package ui

import (
	"fmt"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/palette"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

// ZoneCanvas draws the monitor layout with one profile's zones on it, and lets
// rules be created by dragging. It draws from the same resolved zones and the
// same palette the engine puts on the display.
type ZoneCanvas struct {
	widget *walk.CustomWidget

	mons   *monitor.Set
	global config.Global

	// Resolved from the above; rebuilt by SetRules.
	layout *zones.Layout
	slots  []int

	selected int // rule index, or -1

	drag dragOp

	// OnSelect is called when the selected rule changes, including to -1.
	OnSelect func(rule int)

	// OnCreate is called with a rule the user has just drawn.
	OnCreate func(r config.Rule)
}

// dragOp is the gesture in progress.
type dragOp struct {
	kind    dragKind
	monitor int
	pos     string // for an edge drag: which edge
	// from and to are percentages along the edge, or pixel points for an area.
	fromPct float64
	toPct   float64
	start   walk.Point
	cur     walk.Point
	moved   bool
}

type dragKind int

const (
	dragNone dragKind = iota
	dragEdge
	dragArea
)

// Canvas metrics, in widget pixels.
const (
	canvasPad = 12
	// bandPx is how thick a trigger band is drawn. The real thing is a few
	// pixels deep, which at this scale would be less than one.
	bandPx = 8
	// edgeGrabPx is how close to a monitor border a press counts as grabbing
	// that border rather than starting a rectangle.
	edgeGrabPx = 9
	// dragSlopPx separates a click from a drag.
	dragSlopPx = 5
	// cornerPx is the drawn size of a corner trigger.
	cornerPx = 18
)

// Canvas colours. The plate is dark so the palette reads against it as it does
// against a desktop.
const (
	cvsBack     = 0x2C2C2C
	cvsPlate    = 0x4F5356
	cvsTaskbar  = 0x3A3D40
	cvsBorder   = 0x1A1A1A
	cvsLabel    = 0xB9BDC1
	cvsHint     = 0x8A8F94
	cvsDragLine = 0xFFFFFF
)

func newZoneCanvas() *ZoneCanvas {
	return &ZoneCanvas{selected: -1, layout: &zones.Layout{}}
}

// Declarative returns the widget declaration to embed in a layout.
func (c *ZoneCanvas) Declarative() d.CustomWidget {
	return d.CustomWidget{
		AssignTo:            &c.widget,
		PaintPixels:         c.paint,
		PaintMode:           d.PaintBuffered,
		InvalidatesOnResize: true,
		OnMouseDown:         c.onMouseDown,
		OnMouseMove:         c.onMouseMove,
		OnMouseUp:           c.onMouseUp,
		MinSize:             d.Size{Width: 360, Height: 170},
		StretchFactor:       2,
	}
}

// SetRules re-resolves the profile against the current monitors and repaints.
func (c *ZoneCanvas) SetRules(mons *monitor.Set, rules []config.Rule, g config.Global) {
	c.mons = mons
	c.global = g
	c.layout = zones.Resolve(rules, mons, g)
	c.slots = palette.AssignSlots(mons, c.layout)
	c.Invalidate()
}

// SetSelected marks a rule as selected without calling back.
func (c *ZoneCanvas) SetSelected(rule int) {
	if c.selected == rule {
		return
	}
	c.selected = rule
	c.Invalidate()
}

func (c *ZoneCanvas) Invalidate() {
	if c.widget != nil {
		c.widget.Invalidate()
	}
}

// --- coordinate mapping ---------------------------------------------------

// canvasView maps virtual-desktop pixels onto the widget, preserving aspect.
type canvasView struct {
	src   win.RECT
	scale float64
	offX  float64
	offY  float64
	ok    bool
}

func (c *ZoneCanvas) view() canvasView {
	if c.widget == nil || c.mons == nil || c.mons.Len() == 0 {
		return canvasView{}
	}
	src := c.mons.VirtualBounds()
	if src.IsEmpty() {
		return canvasView{}
	}
	cb := c.widget.ClientBoundsPixels()
	w := float64(cb.Width - 2*canvasPad)
	h := float64(cb.Height - 2*canvasPad)
	if w <= 1 || h <= 1 {
		return canvasView{}
	}
	sx := w / float64(src.Width())
	sy := h / float64(src.Height())
	s := sx
	if sy < s {
		s = sy
	}
	return canvasView{
		src:   src,
		scale: s,
		offX:  float64(cb.X) + (float64(cb.Width)-float64(src.Width())*s)/2,
		offY:  float64(cb.Y) + (float64(cb.Height)-float64(src.Height())*s)/2,
		ok:    true,
	}
}

// rect maps a virtual-desktop rectangle to widget pixels.
func (v canvasView) rect(r win.RECT) walk.Rectangle {
	x0 := v.offX + float64(r.Left-v.src.Left)*v.scale
	y0 := v.offY + float64(r.Top-v.src.Top)*v.scale
	x1 := v.offX + float64(r.Right-v.src.Left)*v.scale
	y1 := v.offY + float64(r.Bottom-v.src.Top)*v.scale
	return walk.Rectangle{
		X:      int(x0),
		Y:      int(y0),
		Width:  maxInt(int(x1)-int(x0), 1),
		Height: maxInt(int(y1)-int(y0), 1),
	}
}

// --- painting -------------------------------------------------------------

func (c *ZoneCanvas) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	// Brushes are made per paint rather than cached: this runs on a dialog
	// change, not from a hook, and it removes a class of lifetime bug.
	cb := c.widget.ClientBoundsPixels()
	fill(canvas, cb, cvsBack)

	v := c.view()
	if !v.ok {
		return c.paintNoScreens(canvas, cb)
	}

	for _, m := range c.mons.All() {
		c.paintMonitor(canvas, v, m)
	}
	// Docks first, then every band on top: the selected dock is filled, and
	// painting each zone whole would let that fill hide a neighbour's band.
	zs := c.layout.Zones()
	sel := -1
	for i, z := range zs {
		if z.RuleIndex == c.selected {
			sel = i
			continue
		}
		c.paintDock(canvas, v, i, z, false)
	}
	if sel >= 0 {
		c.paintDock(canvas, v, sel, zs[sel], true)
	}
	for i, z := range zs {
		c.paintBands(canvas, v, i, z, i == sel)
	}
	// Screen numbers last, or a zone over the corner hides them.
	for _, m := range c.mons.All() {
		c.paintMonitorLabel(canvas, v, m)
	}
	c.paintDrag(canvas, v)
	return nil
}

func (c *ZoneCanvas) paintNoScreens(canvas *walk.Canvas, cb walk.Rectangle) error {
	text(canvas, "No screens detected.", cb, cvsHint, walk.TextCenter|walk.TextVCenter)
	return nil
}

func (c *ZoneCanvas) paintMonitor(canvas *walk.Canvas, v canvasView, m monitor.Monitor) {
	r := v.rect(m.Bounds)
	fill(canvas, r, cvsPlate)

	// The taskbar strip, so it is obvious why a zone stops short of the edge.
	if m.WorkArea != m.Bounds && !m.WorkArea.IsEmpty() {
		for _, strip := range taskbarStrips(m.Bounds, m.WorkArea) {
			fill(canvas, v.rect(strip), cvsTaskbar)
		}
	}

	outline(canvas, r, cvsBorder, 1)
}

func (c *ZoneCanvas) paintMonitorLabel(canvas *walk.Canvas, v canvasView, m monitor.Monitor) {
	r := v.rect(m.Bounds)
	label := fmt.Sprintf("Screen %d", m.Index)
	if m.Primary {
		label += " (primary)"
	}
	// Below the top edge, where a trigger band may run.
	box := walk.Rectangle{X: r.X + bandPx + 4, Y: r.Y + bandPx + 3, Width: maxInt(r.Width-2*bandPx-8, 1), Height: 16}
	// Shadowed, since it sits over whatever colour the zone beneath is.
	text(canvas, label, walk.Rectangle{X: box.X + 1, Y: box.Y + 1, Width: box.Width, Height: box.Height},
		cvsBorder, walk.TextLeft)
	text(canvas, label, box, cvsLabel, walk.TextLeft)
}

// taskbarStrips returns the parts of a monitor its work area excludes, at most
// one per side.
func taskbarStrips(bounds, work win.RECT) []win.RECT {
	var out []win.RECT
	if work.Top > bounds.Top {
		out = append(out, win.RECT{Left: bounds.Left, Top: bounds.Top, Right: bounds.Right, Bottom: work.Top})
	}
	if work.Bottom < bounds.Bottom {
		out = append(out, win.RECT{Left: bounds.Left, Top: work.Bottom, Right: bounds.Right, Bottom: bounds.Bottom})
	}
	if work.Left > bounds.Left {
		out = append(out, win.RECT{Left: bounds.Left, Top: work.Top, Right: work.Left, Bottom: work.Bottom})
	}
	if work.Right < bounds.Right {
		out = append(out, win.RECT{Left: work.Right, Top: work.Top, Right: bounds.Right, Bottom: work.Bottom})
	}
	return out
}

// paintDock draws where a window ends up. Only the selected dock is filled:
// zones overlap heavily in a real profile, and filling them all translucently
// stacks the blends into mud. Outlines stay readable however many overlap.
func (c *ZoneCanvas) paintDock(canvas *walk.Canvas, v canvasView, i int, z zones.Zone, sel bool) {
	col := c.colorFor(i)
	dock := v.rect(z.Dock)

	if sel {
		fill(canvas, dock, palette.Blend(col, cvsPlate, 0.62))
		outline(canvas, dock, palette.Lighten(col, 0.4), 3)
		return
	}
	// Coincident edges are normal, so outlines are inset by varying amounts.
	outline(canvas, shrink(dock, 1+(i%3)*3), col, 2)
}

// paintBands draws where the pointer has to go, in the hue of the dock it
// produces.
func (c *ZoneCanvas) paintBands(canvas *walk.Canvas, v canvasView, i int, z zones.Zone, sel bool) {
	col := c.colorFor(i)
	for _, band := range c.triggerShape(v, z) {
		fill(canvas, band, col)
		if sel {
			outline(canvas, band, palette.Lighten(col, 0.6), 2)
		}
	}
}

// shrink insets a rectangle, never past nothing.
func shrink(r walk.Rectangle, by int) walk.Rectangle {
	if r.Width <= 2*by+2 || r.Height <= 2*by+2 {
		return r
	}
	return walk.Rectangle{X: r.X + by, Y: r.Y + by, Width: r.Width - 2*by, Height: r.Height - 2*by}
}

// triggerShape returns the widget rectangles standing for a trigger. Edge
// bands are drawn at a legible thickness rather than to scale: wrong about
// their depth, right about where they are and how far they run.
func (c *ZoneCanvas) triggerShape(v canvasView, z zones.Zone) []walk.Rectangle {
	m := c.mons.ByIndex(z.TriggerMonitor)
	if m == nil {
		return nil
	}
	plate := v.rect(m.Bounds)
	hit := v.rect(z.Hit)

	switch z.TriggerType {
	case config.TypeEdge:
		switch z.TriggerPos {
		case config.PosLeft:
			return []walk.Rectangle{{X: plate.X, Y: hit.Y, Width: bandPx, Height: hit.Height}}
		case config.PosRight:
			return []walk.Rectangle{{X: plate.X + plate.Width - bandPx, Y: hit.Y, Width: bandPx, Height: hit.Height}}
		case config.PosTop:
			return []walk.Rectangle{{X: hit.X, Y: plate.Y, Width: hit.Width, Height: bandPx}}
		case config.PosBottom:
			return []walk.Rectangle{{X: hit.X, Y: plate.Y + plate.Height - bandPx, Width: hit.Width, Height: bandPx}}
		}
		return nil

	case config.TypeCorner:
		// An L, so a corner never looks like a short edge.
		size := minInt(cornerPx, minInt(plate.Width, plate.Height)/3)
		if size < bandPx {
			size = bandPx
		}
		left := plate.X
		top := plate.Y
		if z.TriggerPos == config.PosTopRight || z.TriggerPos == config.PosBottomRight {
			left = plate.X + plate.Width - size
		}
		if z.TriggerPos == config.PosBottomLeft || z.TriggerPos == config.PosBottomRight {
			top = plate.Y + plate.Height - size
		}
		return []walk.Rectangle{
			{X: left, Y: top, Width: size, Height: bandPx},
			{X: left, Y: top, Width: bandPx, Height: size},
		}

	case config.TypeArea:
		// Big enough to draw to scale; a frame keeps it off the dock fill.
		return frameRects(hit, 2)
	}
	return nil
}

// frameRects turns a rectangle into the four bars of its outline.
func frameRects(r walk.Rectangle, w int) []walk.Rectangle {
	if r.Width <= 2*w || r.Height <= 2*w {
		return []walk.Rectangle{r}
	}
	return []walk.Rectangle{
		{X: r.X, Y: r.Y, Width: r.Width, Height: w},
		{X: r.X, Y: r.Y + r.Height - w, Width: r.Width, Height: w},
		{X: r.X, Y: r.Y + w, Width: w, Height: r.Height - 2*w},
		{X: r.X + r.Width - w, Y: r.Y + w, Width: w, Height: r.Height - 2*w},
	}
}

// paintDrag draws the gesture in progress.
func (c *ZoneCanvas) paintDrag(canvas *walk.Canvas, v canvasView) {
	switch c.drag.kind {
	case dragEdge:
		m := c.mons.ByIndex(c.drag.monitor)
		if m == nil {
			return
		}
		lo, hi := orderedPct(c.drag.fromPct, c.drag.toPct)
		band := edgeBandRect(v.rect(m.Bounds), c.drag.pos, lo, hi)
		fill(canvas, band, cvsDragLine)

	case dragArea:
		r := normalized(c.drag.start, c.drag.cur)
		for _, bar := range frameRects(r, 2) {
			fill(canvas, bar, cvsDragLine)
		}
	}
}

// edgeBandRect places a band along one edge of a plate, spanning a percentage
// range of that edge.
func edgeBandRect(plate walk.Rectangle, pos string, from, to float64) walk.Rectangle {
	switch pos {
	case config.PosLeft, config.PosRight:
		y0 := plate.Y + int(float64(plate.Height)*from/100)
		y1 := plate.Y + int(float64(plate.Height)*to/100)
		x := plate.X
		if pos == config.PosRight {
			x = plate.X + plate.Width - bandPx
		}
		return walk.Rectangle{X: x, Y: y0, Width: bandPx, Height: maxInt(y1-y0, 2)}
	default:
		x0 := plate.X + int(float64(plate.Width)*from/100)
		x1 := plate.X + int(float64(plate.Width)*to/100)
		y := plate.Y
		if pos == config.PosBottom {
			y = plate.Y + plate.Height - bandPx
		}
		return walk.Rectangle{X: x0, Y: y, Width: maxInt(x1-x0, 2), Height: bandPx}
	}
}

func (c *ZoneCanvas) colorFor(zoneIndex int) uint32 {
	slot := 0
	if zoneIndex >= 0 && zoneIndex < len(c.slots) {
		slot = c.slots[zoneIndex]
	}
	return palette.Color(slot, c.global.CategoricalColors(), c.global.PreviewFill())
}

// --- interaction ----------------------------------------------------------

func (c *ZoneCanvas) onMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}
	v := c.view()
	if !v.ok {
		return
	}
	p := walk.Point{X: x, Y: y}

	// A press near a screen border grabs that border.
	if mi, pos, ok := c.edgeUnder(v, p); ok {
		pct := c.pctAlongEdge(v, mi, pos, p)
		c.drag = dragOp{kind: dragEdge, monitor: mi, pos: pos, fromPct: pct, toPct: pct, start: p, cur: p}
		c.Invalidate()
		return
	}

	// A press inside an existing zone selects it.
	if rule, ok := c.zoneUnder(v, p); ok {
		c.selectRule(rule)
		return
	}

	// Anything else draws a rectangle. The selection is left alone: beginning
	// to draw is not a reason to lose your place.
	c.drag = dragOp{kind: dragArea, start: p, cur: p}
}

func (c *ZoneCanvas) onMouseMove(x, y int, button walk.MouseButton) {
	if c.drag.kind == dragNone {
		return
	}
	p := walk.Point{X: x, Y: y}
	if absInt(p.X-c.drag.start.X) > dragSlopPx || absInt(p.Y-c.drag.start.Y) > dragSlopPx {
		c.drag.moved = true
	}
	c.drag.cur = p

	if c.drag.kind == dragEdge {
		v := c.view()
		if v.ok {
			c.drag.toPct = c.pctAlongEdge(v, c.drag.monitor, c.drag.pos, p)
		}
	}
	c.Invalidate()
}

func (c *ZoneCanvas) onMouseUp(x, y int, button walk.MouseButton) {
	op := c.drag
	c.drag = dragOp{}
	if op.kind == dragNone {
		return
	}
	c.Invalidate()

	v := c.view()
	if !v.ok {
		return
	}

	switch op.kind {
	case dragEdge:
		from, to := orderedPct(op.fromPct, op.toPct)
		if !op.moved || to-from < 2 {
			// A click, not a drag: offer the whole edge to trim down.
			from, to = 0, 100
		}
		c.emit(edgeRuleFor(op.monitor, op.pos, from, to))

	case dragArea:
		if !op.moved {
			return
		}
		r := normalized(op.start, op.cur)
		if r.Width < dragSlopPx*2 || r.Height < dragSlopPx*2 {
			return
		}
		rule, ok := c.areaRuleFor(v, r)
		if !ok {
			return
		}
		c.emit(rule)
	}
}

func (c *ZoneCanvas) emit(r config.Rule) {
	if c.OnCreate != nil {
		c.OnCreate(r)
	}
}

func (c *ZoneCanvas) selectRule(rule int) {
	if c.selected != rule {
		c.selected = rule
		c.Invalidate()
	}
	if c.OnSelect != nil {
		c.OnSelect(rule)
	}
}

// edgeUnder reports which monitor border a point is grabbing, if any.
func (c *ZoneCanvas) edgeUnder(v canvasView, p walk.Point) (int, string, bool) {
	for _, m := range c.mons.All() {
		r := v.rect(m.Bounds)
		if p.X < r.X-edgeGrabPx || p.X > r.X+r.Width+edgeGrabPx ||
			p.Y < r.Y-edgeGrabPx || p.Y > r.Y+r.Height+edgeGrabPx {
			continue
		}
		// Nearest border wins, so a press in a corner is not order-dependent.
		dl := absInt(p.X - r.X)
		dr := absInt(p.X - (r.X + r.Width))
		dt := absInt(p.Y - r.Y)
		db := absInt(p.Y - (r.Y + r.Height))

		best, pos := dl, config.PosLeft
		if dr < best {
			best, pos = dr, config.PosRight
		}
		if dt < best {
			best, pos = dt, config.PosTop
		}
		if db < best {
			best, pos = db, config.PosBottom
		}
		if best <= edgeGrabPx {
			return m.Index, pos, true
		}
	}
	return 0, "", false
}

// pctAlongEdge converts a widget point into a percentage along one edge of a
// monitor, clamped to that monitor.
func (c *ZoneCanvas) pctAlongEdge(v canvasView, monIndex int, pos string, p walk.Point) float64 {
	m := c.mons.ByIndex(monIndex)
	if m == nil {
		return 0
	}
	r := v.rect(m.Bounds)
	var f float64
	switch pos {
	case config.PosLeft, config.PosRight:
		if r.Height <= 0 {
			return 0
		}
		f = float64(p.Y-r.Y) / float64(r.Height)
	default:
		if r.Width <= 0 {
			return 0
		}
		f = float64(p.X-r.X) / float64(r.Width)
	}
	return roundPct(clampFloat(f*100, 0, 100))
}

// zoneUnder returns the rule index of the smallest zone containing the point,
// so a zone nested inside a full-screen one can still be picked.
func (c *ZoneCanvas) zoneUnder(v canvasView, p walk.Point) (int, bool) {
	best, bestArea := -1, 0
	for _, z := range c.layout.Zones() {
		r := v.rect(z.Dock)
		if p.X < r.X || p.X >= r.X+r.Width || p.Y < r.Y || p.Y >= r.Y+r.Height {
			continue
		}
		a := r.Width * r.Height
		if best < 0 || a < bestArea {
			best, bestArea = z.RuleIndex, a
		}
	}
	return best, best >= 0
}

// areaRuleFor turns a drawn rectangle into a rule. The rectangle becomes both
// the trigger and the dock: drop a window in it and it fills it.
func (c *ZoneCanvas) areaRuleFor(v canvasView, r walk.Rectangle) (config.Rule, bool) {
	m := c.monitorUnder(v, r)
	if m == nil {
		return config.Rule{}, false
	}
	plate := v.rect(m.Bounds)
	if plate.Width <= 0 || plate.Height <= 0 {
		return config.Rule{}, false
	}

	toPctX := func(x int) float64 {
		return roundPct(clampFloat(float64(x-plate.X)/float64(plate.Width)*100, 0, 100))
	}
	toPctY := func(y int) float64 {
		return roundPct(clampFloat(float64(y-plate.Y)/float64(plate.Height)*100, 0, 100))
	}

	l, t := toPctX(r.X), toPctY(r.Y)
	rt, b := toPctX(r.X+r.Width), toPctY(r.Y+r.Height)
	if rt-l < 1 || b-t < 1 {
		return config.Rule{}, false
	}

	vals := []float64{l, t, rt, b}
	return config.Rule{
		Dock:    config.Dock{Monitor: m.Index, Values: append([]float64(nil), vals...)},
		Trigger: config.Trigger{Monitor: m.Index, Type: config.TypeArea, Values: append([]float64(nil), vals...)},
	}, true
}

// monitorUnder picks the screen a drawn box covers most of, not the one its
// centre lands on: a box straddling the bottom edge has its centre off every
// monitor, which used to make that drag produce nothing at all.
func (c *ZoneCanvas) monitorUnder(v canvasView, r walk.Rectangle) *monitor.Monitor {
	mons := c.mons.All()
	best, bestArea := -1, 0
	for i := range mons {
		inter := intersectRects(v.rect(mons[i].Bounds), r)
		if a := inter.Width * inter.Height; a > bestArea {
			best, bestArea = i, a
		}
	}
	if best < 0 {
		return nil
	}
	return &mons[best]
}

// intersectRects returns the overlap of two widget rectangles, empty if none.
func intersectRects(a, b walk.Rectangle) walk.Rectangle {
	x0, y0 := maxInt(a.X, b.X), maxInt(a.Y, b.Y)
	x1 := minInt(a.X+a.Width, b.X+b.Width)
	y1 := minInt(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return walk.Rectangle{}
	}
	return walk.Rectangle{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// edgeRuleFor builds the rule a grabbed edge produces. A side edge docks to
// the half beside it over the dragged span; a top or bottom edge to a column of
// that span. The fields beside the canvas correct it from there.
func edgeRuleFor(mon int, pos string, from, to float64) config.Rule {
	var dock []float64
	switch pos {
	case config.PosLeft:
		dock = []float64{0, from, 50, to}
	case config.PosRight:
		dock = []float64{50, from, 100, to}
	case config.PosTop, config.PosBottom:
		dock = []float64{from, 0, to, 100}
	}
	return config.Rule{
		Dock:    config.Dock{Monitor: mon, Values: dock},
		Trigger: config.Trigger{Monitor: mon, Pos: pos, Type: config.TypeEdge, Values: []float64{from, to}},
	}
}

// --- small helpers --------------------------------------------------------

func fill(canvas *walk.Canvas, r walk.Rectangle, color uint32) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	brush, err := walk.NewSolidColorBrush(walkColor(color))
	if err != nil {
		return
	}
	defer brush.Dispose()
	canvas.FillRectanglePixels(brush, r)
}

func outline(canvas *walk.Canvas, r walk.Rectangle, color uint32, width int) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	for _, bar := range frameRects(r, width) {
		fill(canvas, bar, color)
	}
}

func text(canvas *walk.Canvas, s string, r walk.Rectangle, color uint32, format walk.DrawTextFormat) {
	font, err := walk.NewFont("Segoe UI", 8, 0)
	if err != nil {
		return
	}
	defer font.Dispose()
	canvas.DrawTextPixels(s, font, walkColor(color), r, format|walk.TextSingleLine|walk.TextEndEllipsis)
}

// walkColor converts 0xRRGGBB to walk's COLORREF ordering.
func walkColor(c uint32) walk.Color {
	return walk.RGB(byte(c>>16), byte(c>>8), byte(c))
}

func normalized(a, b walk.Point) walk.Rectangle {
	x0, x1 := minInt(a.X, b.X), maxInt(a.X, b.X)
	y0, y1 := minInt(a.Y, b.Y), maxInt(a.Y, b.Y)
	return walk.Rectangle{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

func orderedPct(a, b float64) (float64, float64) {
	if a <= b {
		return a, b
	}
	return b, a
}

// roundPct keeps drawn values to one decimal; a tenth of a percent is well
// under a pixel.
func roundPct(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
