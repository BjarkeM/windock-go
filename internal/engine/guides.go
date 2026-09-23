package engine

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

const guideClassName = "WinDockGoGuides"

// guideKeyColor is made transparent by the layered-window colour key, so only
// the trigger shapes are painted.
const guideKeyColor = 0x010203

// cornerArmMax caps how far a corner guide extends along each edge; the whole
// 120px box filled would dominate the screen, so it is drawn as an L.
const cornerArmMax = 96

// activeGrowth is how much thicker the trigger under the cursor is drawn. It
// keeps its own colour, which is what ties it to the preview now filling.
const activeGrowth = 8

// guideShape is one trigger as filled rectangles in its monitor window's client
// coordinates. Both sizes are precomputed so highlighting is only a repaint.
type guideShape struct {
	zoneIndex  int
	slot       int
	rects      []win.RECT
	activeRect []win.RECT
}

// monitorGuides is the paint state for one monitor's guide window, reached from
// the window procedure through guideState below.
type monitorGuides struct {
	shapes   []guideShape
	active   int // zone index currently under the cursor, or -1
	keyBrush uintptr
}

var (
	guideState     = map[win.HWND]*monitorGuides{}
	guideClassOnce sync.Once
	guideClassOK   bool
)

var guideWndProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case win.WM_ERASEBKGND:
		return 1

	case win.WM_PAINT:
		st := guideState[hwnd]
		var ps win.PAINTSTRUCT
		hdc := win.BeginPaint(hwnd, &ps)
		rc := win.GetClientRect(hwnd)
		if st != nil {
			// Everything starts transparent; only the shapes are drawn.
			win.FillRect(hdc, &rc, st.keyBrush)
			for _, sh := range st.shapes {
				active := sh.zoneIndex == st.active
				rects := sh.rects
				keyline := keylineDark
				if active {
					rects = sh.activeRect
					keyline = keylineLight
				}
				fill := brushesFor(sh.slot).fill
				for i := range rects {
					win.FillRect(hdc, &rects[i], fill)
					// The keyline is what keeps a band legible over an
					// arbitrary wallpaper, whatever colour it happens to be.
					if keyline != 0 {
						win.FrameRect(hdc, &rects[i], keyline)
					}
				}
			}
		}
		win.EndPaint(hwnd, &ps)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wparam, lparam)
})

// guides draws every trigger region while a window is dragged, highlighting the
// one under the cursor. Drawn thicker than the region that actually fires.
type guides struct {
	windows []win.HWND
	shown   bool
	active  int
}

func newGuides() *guides { return &guides{active: -1} }

// rebuild recreates one window per monitor and recomputes the shapes. It is
// called whenever the layout or the display topology changes.
//
// Tolerates a nil receiver, like the overlay does: the notification window
// exists before Run builds the guides, so a WM_DISPLAYCHANGE or
// WM_SETTINGCHANGE arriving in that gap reaches refreshDisplays with nothing
// to rebuild yet. Run rebuilds them itself a moment later.
func (g *guides) rebuild(mons *monitor.Set, layout *zones.Layout, cfg config.Global, slots []int) {
	if g == nil {
		return
	}
	g.destroy()
	g.active = -1

	if !cfg.GuidesEnabled() || layout.Len() == 0 || mons.Len() == 0 {
		return
	}

	hinst := win.GetModuleHandle()
	guideClassOnce.Do(func() {
		wc := win.WNDCLASSEX{
			LpfnWndProc:   guideWndProc,
			HInstance:     hinst,
			LpszClassName: win.UTF16Ptr(guideClassName),
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		guideClassOK = win.RegisterClassEx(&wc) != 0
	})
	if !guideClassOK {
		return
	}

	thickness := int32(cfg.GuideThickness())

	for _, m := range mons.All() {
		shapes := shapesForMonitor(m, layout, slots, thickness)
		if len(shapes) == 0 {
			continue
		}

		b := m.Bounds
		hwnd := win.CreateWindowEx(
			win.WS_EX_LAYERED|win.WS_EX_TRANSPARENT|win.WS_EX_NOACTIVATE|
				win.WS_EX_TOOLWINDOW|win.WS_EX_TOPMOST,
			win.UTF16Ptr(guideClassName),
			win.UTF16Ptr(""),
			win.WS_POPUP,
			b.Left, b.Top, b.Width(), b.Height(),
			0, hinst)
		if hwnd == 0 {
			continue
		}

		guideState[hwnd] = &monitorGuides{
			shapes:   shapes,
			active:   -1,
			keyBrush: win.CreateSolidBrush(colorRef(guideKeyColor)),
		}
		// LWA_COLORKEY hides the background entirely; LWA_ALPHA then applies to
		// what remains, so the guides sit lightly over the desktop.
		win.SetLayeredWindowAttributes(hwnd, colorRef(guideKeyColor),
			config.DefaultGuideAlpha, win.LWA_COLORKEY|win.LWA_ALPHA)

		g.windows = append(g.windows, hwnd)
	}
}

// shapesForMonitor builds the drawable shapes for the zones that trigger on m.
func shapesForMonitor(m monitor.Monitor, layout *zones.Layout, slots []int, thickness int32) []guideShape {
	var out []guideShape
	for i, z := range layout.Zones() {
		if z.TriggerMonitor != m.Index {
			continue
		}
		slot := 0
		if i < len(slots) {
			slot = slots[i]
		}
		// Guide windows cover the monitor, so shapes are drawn in client
		// coordinates relative to its top-left corner.
		hit := offsetRect(z.Hit, -m.Bounds.Left, -m.Bounds.Top)
		bounds := offsetRect(m.Bounds, -m.Bounds.Left, -m.Bounds.Top)
		work := offsetRect(m.WorkArea, -m.Bounds.Left, -m.Bounds.Top)

		build := func(th int32) []win.RECT {
			switch z.TriggerType {
			case config.TypeEdge:
				return []win.RECT{edgeBand(hit, bounds, work, z.TriggerPos, th)}
			case config.TypeCorner:
				return cornerArms(hit, z.TriggerPos, th)
			case config.TypeArea:
				return frame(hit, th)
			}
			return nil
		}
		rects := build(thickness)
		if len(rects) == 0 {
			continue
		}
		out = append(out, guideShape{
			zoneIndex:  i,
			slot:       slot,
			rects:      rects,
			activeRect: build(thickness + activeGrowth),
		})
	}
	return out
}

// edgeBand thickens a trigger strip and insets the ends, so neighbouring
// segments read as separate targets.
//
// Drawn against the work-area edge, not the physical one: the taskbar is itself
// topmost and would cover anything painted under it, and the work-area edge is
// where a dragged window comes to rest anyway.
func edgeBand(hit, bounds, work win.RECT, pos string, thickness int32) win.RECT {
	const gap = 2
	r := hit
	switch pos {
	case config.PosLeft:
		r.Left = work.Left
		r.Right = work.Left + thickness
		r.Top += gap
		r.Bottom -= gap
	case config.PosRight:
		r.Left = work.Right - thickness
		r.Right = work.Right
		r.Top += gap
		r.Bottom -= gap
	case config.PosTop:
		r.Top = work.Top
		r.Bottom = work.Top + thickness
		r.Left += gap
		r.Right -= gap
	case config.PosBottom:
		r.Top = work.Bottom - thickness
		r.Bottom = work.Bottom
		r.Left += gap
		r.Right -= gap
	}
	return clipTo(r, bounds)
}

// cornerArms draws a corner trigger as an L tracing the two outer edges of its
// hit box, so its real extent is visible without filling the whole square.
func cornerArms(hit win.RECT, pos string, thickness int32) []win.RECT {
	armX := minInt32(hit.Width(), cornerArmMax)
	armY := minInt32(hit.Height(), cornerArmMax)
	if armX <= 0 || armY <= 0 {
		return nil
	}

	switch pos {
	case config.PosTopLeft:
		return []win.RECT{
			{Left: hit.Left, Top: hit.Top, Right: hit.Left + armX, Bottom: hit.Top + thickness},
			{Left: hit.Left, Top: hit.Top, Right: hit.Left + thickness, Bottom: hit.Top + armY},
		}
	case config.PosTopRight:
		return []win.RECT{
			{Left: hit.Right - armX, Top: hit.Top, Right: hit.Right, Bottom: hit.Top + thickness},
			{Left: hit.Right - thickness, Top: hit.Top, Right: hit.Right, Bottom: hit.Top + armY},
		}
	case config.PosBottomLeft:
		return []win.RECT{
			{Left: hit.Left, Top: hit.Bottom - thickness, Right: hit.Left + armX, Bottom: hit.Bottom},
			{Left: hit.Left, Top: hit.Bottom - armY, Right: hit.Left + thickness, Bottom: hit.Bottom},
		}
	case config.PosBottomRight:
		return []win.RECT{
			{Left: hit.Right - armX, Top: hit.Bottom - thickness, Right: hit.Right, Bottom: hit.Bottom},
			{Left: hit.Right - thickness, Top: hit.Bottom - armY, Right: hit.Right, Bottom: hit.Bottom},
		}
	}
	return nil
}

// frame draws an area trigger as an outline of the region it covers.
func frame(r win.RECT, thickness int32) []win.RECT {
	if r.Width() <= 2*thickness || r.Height() <= 2*thickness {
		return []win.RECT{r}
	}
	return []win.RECT{
		{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Top + thickness},
		{Left: r.Left, Top: r.Bottom - thickness, Right: r.Right, Bottom: r.Bottom},
		{Left: r.Left, Top: r.Top + thickness, Right: r.Left + thickness, Bottom: r.Bottom - thickness},
		{Left: r.Right - thickness, Top: r.Top + thickness, Right: r.Right, Bottom: r.Bottom - thickness},
	}
}

func offsetRect(r win.RECT, dx, dy int32) win.RECT {
	return win.RECT{Left: r.Left + dx, Top: r.Top + dy, Right: r.Right + dx, Bottom: r.Bottom + dy}
}

func clipTo(r, bounds win.RECT) win.RECT {
	if r.Left < bounds.Left {
		r.Left = bounds.Left
	}
	if r.Top < bounds.Top {
		r.Top = bounds.Top
	}
	if r.Right > bounds.Right {
		r.Right = bounds.Right
	}
	if r.Bottom > bounds.Bottom {
		r.Bottom = bounds.Bottom
	}
	return r
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// show reveals the guides for the duration of a drag.
func (g *guides) show() {
	if g == nil || g.shown {
		return
	}
	for _, hwnd := range g.windows {
		win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0,
			win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	}
	g.shown = len(g.windows) > 0
}

func (g *guides) hide() {
	if g == nil || !g.shown {
		return
	}
	for _, hwnd := range g.windows {
		win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0,
			win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE|win.SWP_HIDEWINDOW)
	}
	g.shown = false
	g.setActive(-1)
}

// setActive highlights one trigger. Only the windows whose appearance actually
// changes are repainted, since this runs from the mouse hook.
func (g *guides) setActive(zoneIndex int) {
	if g == nil || g.active == zoneIndex {
		return
	}
	previous := g.active
	g.active = zoneIndex

	for _, hwnd := range g.windows {
		st := guideState[hwnd]
		if st == nil {
			continue
		}
		if !st.holds(previous) && !st.holds(zoneIndex) {
			continue
		}
		st.active = zoneIndex
		win.InvalidateRect(hwnd, false)
	}
}

func (m *monitorGuides) holds(zoneIndex int) bool {
	if zoneIndex < 0 {
		return false
	}
	for _, sh := range m.shapes {
		if sh.zoneIndex == zoneIndex {
			return true
		}
	}
	return false
}

func (g *guides) destroy() {
	if g == nil {
		return
	}
	for _, hwnd := range g.windows {
		if st := guideState[hwnd]; st != nil {
			win.DeleteObject(st.keyBrush)
			delete(guideState, hwnd)
		}
		win.DestroyWindow(hwnd)
	}
	g.windows = nil
	g.shown = false
	g.active = -1
}
