package ui

import (
	"strings"
	"testing"

	"github.com/lxn/walk"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
)

// These cover the arithmetic behind the layout map. The widget itself needs a
// desktop, but where a drag lands, and what rule it produces, is ordinary
// geometry and is exactly the part that would be wrong.

func testMons() *monitor.Set {
	return monitor.NewSetForTest([]monitor.Monitor{{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: 3440, Bottom: 1440},
		WorkArea: win.RECT{Left: 0, Top: 0, Right: 3440, Bottom: 1400},
		Device:   `\\.\DISPLAY1`,
		Primary:  true,
	}})
}

func testView() canvasView {
	src := win.RECT{Left: 0, Top: 0, Right: 3440, Bottom: 1440}
	client := walk.Rectangle{X: 0, Y: 0, Width: 700, Height: 400}
	w := float64(client.Width - 2*canvasPad)
	h := float64(client.Height - 2*canvasPad)
	sx, sy := w/float64(src.Width()), h/float64(src.Height())
	s := sx
	if sy < s {
		s = sy
	}
	return canvasView{
		src:   src,
		scale: s,
		offX:  (float64(client.Width) - float64(src.Width())*s) / 2,
		offY:  (float64(client.Height) - float64(src.Height())*s) / 2,
		ok:    true,
	}
}

func TestViewKeepsAspect(t *testing.T) {
	v := testView()
	r := v.rect(win.RECT{Left: 0, Top: 0, Right: 3440, Bottom: 1440})
	want := 3440.0 / 1440.0
	got := float64(r.Width) / float64(r.Height)
	if got < want*0.97 || got > want*1.03 {
		t.Errorf("a 21:9 screen drew at %.2f:1, want %.2f:1", got, want)
	}
}

func TestViewIsEmptyWithNoScreens(t *testing.T) {
	c := newZoneCanvas()
	if v := c.view(); v.ok {
		t.Error("a canvas with no monitors reported a usable view")
	}
}

// --- rules produced by dragging -------------------------------------------

func TestEdgeDragProducesAUsableRule(t *testing.T) {
	for _, pos := range []string{config.PosLeft, config.PosRight, config.PosTop, config.PosBottom} {
		t.Run(pos, func(t *testing.T) {
			r := edgeRuleFor(0, pos, 0, 50)
			if err := r.Validate(); err != nil {
				t.Fatalf("dragging the %s edge produced an invalid rule: %v", pos, err)
			}
			if r.Trigger.Type != config.TypeEdge || r.Trigger.Pos != pos {
				t.Errorf("got trigger %+v", r.Trigger)
			}
			if len(r.Trigger.Values) != 2 || r.Trigger.Values[0] != 0 || r.Trigger.Values[1] != 50 {
				t.Errorf("trigger span is %v, want [0 50]", r.Trigger.Values)
			}
			if len(r.Dock.Values) != 4 {
				t.Fatalf("dock is %v", r.Dock.Values)
			}
		})
	}
}

// TestEdgeDragDocksBesideTheEdge checks the defaults are the obvious ones: a
// side edge gives the half beside it, a top or bottom edge gives a column.
func TestEdgeDragDocksBesideTheEdge(t *testing.T) {
	for _, tc := range []struct {
		pos  string
		want []float64
	}{
		{config.PosLeft, []float64{0, 10, 50, 60}},
		{config.PosRight, []float64{50, 10, 100, 60}},
		{config.PosTop, []float64{10, 0, 60, 100}},
		{config.PosBottom, []float64{10, 0, 60, 100}},
	} {
		got := edgeRuleFor(0, tc.pos, 10, 60).Dock.Values
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v", tc.pos, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: dock %v, want %v", tc.pos, got, tc.want)
				break
			}
		}
	}
}

func TestEdgeDragCarriesTheMonitor(t *testing.T) {
	r := edgeRuleFor(2, config.PosLeft, 0, 100)
	if r.Trigger.Monitor != 2 || r.Dock.Monitor != 2 {
		t.Errorf("a zone drawn on screen 2 was written for %d/%d",
			r.Trigger.Monitor, r.Dock.Monitor)
	}
}

func TestAreaDragProducesAUsableRule(t *testing.T) {
	c := newZoneCanvas()
	c.mons = testMons()
	v := testView()

	// A box roughly in the middle of the screen.
	plate := v.rect(c.mons.ByIndex(0).Bounds)
	box := walk.Rectangle{
		X:      plate.X + plate.Width/4,
		Y:      plate.Y + plate.Height/4,
		Width:  plate.Width / 4,
		Height: plate.Height / 4,
	}
	r, ok := c.areaRuleFor(v, box)
	if !ok {
		t.Fatal("drawing a box in the middle of the screen produced nothing")
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("the drawn rule does not validate: %v", err)
	}
	if r.Trigger.Type != config.TypeArea {
		t.Errorf("trigger type is %q, want area", r.Trigger.Type)
	}
	// The drawn box is both where you drop the window and where it goes.
	for i := range r.Dock.Values {
		if r.Dock.Values[i] != r.Trigger.Values[i] {
			t.Errorf("dock %v does not match trigger %v", r.Dock.Values, r.Trigger.Values)
			break
		}
	}
	if r.Trigger.Values[0] < 20 || r.Trigger.Values[0] > 30 {
		t.Errorf("a box a quarter across the screen started at %.1f%%", r.Trigger.Values[0])
	}
}

func TestAreaDragOffScreenIsRejected(t *testing.T) {
	c := newZoneCanvas()
	c.mons = testMons()
	v := testView()
	// Far outside any monitor.
	box := walk.Rectangle{X: 5000, Y: 5000, Width: 40, Height: 40}
	if _, ok := c.areaRuleFor(v, box); ok {
		t.Error("a box drawn off every screen produced a rule")
	}
}

// --- drawing helpers ------------------------------------------------------

func TestTaskbarStripIsTheWorkAreaDifference(t *testing.T) {
	bounds := win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	work := win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040}
	strips := taskbarStrips(bounds, work)
	if len(strips) != 1 {
		t.Fatalf("got %d strips, want 1", len(strips))
	}
	if strips[0].Top != 1040 || strips[0].Bottom != 1080 {
		t.Errorf("strip is %+v, want the bottom 40px", strips[0])
	}
}

func TestNoTaskbarStripWhenTheWorkAreaIsTheWholeScreen(t *testing.T) {
	r := win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	if got := taskbarStrips(r, r); len(got) != 0 {
		t.Errorf("got %d strips for a screen with no appbar", len(got))
	}
}

func TestFrameRectsCoverTheOutlineOnly(t *testing.T) {
	r := walk.Rectangle{X: 10, Y: 20, Width: 100, Height: 60}
	bars := frameRects(r, 2)
	if len(bars) != 4 {
		t.Fatalf("got %d bars, want 4", len(bars))
	}
	for _, b := range bars {
		if b.X < r.X || b.Y < r.Y ||
			b.X+b.Width > r.X+r.Width || b.Y+b.Height > r.Y+r.Height {
			t.Errorf("bar %+v escapes the rectangle %+v", b, r)
		}
	}
}

// TestFrameRectsDegradeToASolidFill guards a rectangle too small to have an
// inside, which a drag can easily produce.
func TestFrameRectsDegradeToASolidFill(t *testing.T) {
	r := walk.Rectangle{X: 0, Y: 0, Width: 3, Height: 3}
	if got := frameRects(r, 2); len(got) != 1 {
		t.Errorf("got %d bars for a 3x3 frame, want a single fill", len(got))
	}
}

func TestOrderedPctSortsADragInEitherDirection(t *testing.T) {
	if a, b := orderedPct(80, 20); a != 20 || b != 80 {
		t.Errorf("got %v..%v, want 20..80; dragging upwards must work", a, b)
	}
	if a, b := orderedPct(20, 80); a != 20 || b != 80 {
		t.Errorf("got %v..%v", a, b)
	}
}

func TestRoundPctKeepsOneDecimal(t *testing.T) {
	if got := roundPct(33.33333); got != 33.3 {
		t.Errorf("got %v, want 33.3", got)
	}
	if got := roundPct(66.66666); got != 66.7 {
		t.Errorf("got %v, want 66.7", got)
	}
}

func TestNormalizedHandlesEveryDragDirection(t *testing.T) {
	want := walk.Rectangle{X: 10, Y: 20, Width: 30, Height: 40}
	corners := [][2]walk.Point{
		{{X: 10, Y: 20}, {X: 40, Y: 60}},
		{{X: 40, Y: 60}, {X: 10, Y: 20}},
		{{X: 40, Y: 20}, {X: 10, Y: 60}},
		{{X: 10, Y: 60}, {X: 40, Y: 20}},
	}
	for i, c := range corners {
		if got := normalized(c[0], c[1]); got != want {
			t.Errorf("drag %d gave %+v, want %+v", i, got, want)
		}
	}
}

func TestPadValuesNeverPanicsOnAShortProfile(t *testing.T) {
	for _, in := range [][]float64{nil, {1}, {1, 2, 3}, {1, 2, 3, 4, 5}} {
		if got := padValues(in, 4); len(got) != 4 {
			t.Errorf("padValues(%v) gave %d values", in, len(got))
		}
	}
}

func TestClampIndexSurvivesAnEmptyCombo(t *testing.T) {
	if got := clampIndex(-1, 0); got != 0 {
		t.Errorf("got %d", got)
	}
	if got := clampIndex(9, 4); got != 3 {
		t.Errorf("got %d, want the last item", got)
	}
}

func TestInspectorHintFlagsARuleTheEngineWouldSkip(t *testing.T) {
	bad := config.Rule{
		Dock:    config.Dock{Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Type: config.TypeEdge, Pos: config.PosLeft}, // no span
	}
	got := inspectorHint(bad)
	if !strings.Contains(got, "ignored") {
		t.Errorf("hint for an invalid rule was %q; it must say the rule will not fire", got)
	}

	good := config.Rule{
		Dock:    config.Dock{Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Type: config.TypeEdge, Pos: config.PosLeft, Values: []float64{0, 100}},
	}
	if h := inspectorHint(good); strings.Contains(h, "ignored") {
		t.Errorf("a valid rule was reported as ignored: %q", h)
	}
}

func TestUniqueProfileNameAvoidsCollisions(t *testing.T) {
	existing := []config.Profile{{Name: "Halves"}, {Name: "Halves (2)"}}
	if got := uniqueProfileName(existing, "Halves"); got != "Halves (3)" {
		t.Errorf("got %q, want Halves (3)", got)
	}
	if got := uniqueProfileName(existing, "Thirds"); got != "Thirds" {
		t.Errorf("an unused name was changed to %q", got)
	}
}

func TestTriggerTypesMatchTheComboOrder(t *testing.T) {
	// The combo shows three fixed labels and the index maps straight into this
	// slice, so the two must stay the same length and order.
	if len(triggerTypes) != 3 {
		t.Fatalf("got %d trigger types", len(triggerTypes))
	}
	want := []string{config.TypeEdge, config.TypeCorner, config.TypeArea}
	for i := range want {
		if triggerTypes[i] != want[i] {
			t.Errorf("position %d is %q, want %q", i, triggerTypes[i], want[i])
		}
	}
}

// TestInspectorHintNamesBothScreens covers the unusual zone: one that fires on
// one screen and sends the window to another. The two dropdowns can be set to
// disagree on purpose, and if they do the hint has to say so - otherwise the
// only way to notice is to drag a window and watch it leave.
func TestInspectorHintNamesBothScreens(t *testing.T) {
	r := config.Rule{
		Dock:    config.Dock{Monitor: 1, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Type: config.TypeEdge, Pos: config.PosLeft, Values: []float64{0, 100}},
	}
	got := inspectorHint(r)
	for _, want := range []string{"screen 0", "screen 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("hint %q does not mention %q", got, want)
		}
	}
}

// TestInspectorHintStaysShortForOneScreen: naming the screen twice on every
// ordinary zone would bury the one case where it matters.
func TestInspectorHintStaysShortForOneScreen(t *testing.T) {
	r := config.Rule{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Type: config.TypeEdge, Pos: config.PosLeft, Values: []float64{0, 100}},
	}
	if got := inspectorHint(r); strings.Contains(got, "screen") {
		t.Errorf("a single-screen zone named its screen: %q", got)
	}
}

// TestBoxStraddlingTheBottomEdgeStillMakesAZone is the case that was reported:
// drawing a box around the bottom line of a screen put half of it below the
// screen, so its centre fell outside every monitor and the drag produced
// nothing at all - with no message saying so.
func TestBoxStraddlingTheBottomEdgeStillMakesAZone(t *testing.T) {
	c := newZoneCanvas()
	c.mons = testMons()
	v := testView()
	plate := v.rect(c.mons.ByIndex(0).Bounds)

	// A wide, short box centred on the bottom border of the screen.
	h := 40
	box := walk.Rectangle{
		X:      plate.X + plate.Width/4,
		Y:      plate.Y + plate.Height - h/2,
		Width:  plate.Width / 2,
		Height: h,
	}
	r, ok := c.areaRuleFor(v, box)
	if !ok {
		t.Fatal("a box drawn across the bottom edge produced no zone")
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("it produced an unusable zone: %v", err)
	}
	// Clamped to the screen, so it ends at the bottom rather than past it.
	if got := r.Trigger.Values[3]; got != 100 {
		t.Errorf("the zone ends at %.1f%%, want 100%% - it should stop at the screen edge", got)
	}
	if r.Trigger.Values[1] >= 100 {
		t.Errorf("the zone starts at %.1f%%, leaving it no height", r.Trigger.Values[1])
	}
}

func TestBoxWhollyOffScreenStillProducesNothing(t *testing.T) {
	c := newZoneCanvas()
	c.mons = testMons()
	v := testView()
	plate := v.rect(c.mons.ByIndex(0).Bounds)
	// Well below the screen, touching nothing.
	box := walk.Rectangle{X: plate.X, Y: plate.Y + plate.Height + 40, Width: 60, Height: 30}
	if _, ok := c.areaRuleFor(v, box); ok {
		t.Error("a box drawn entirely off the screen produced a zone")
	}
}

func TestIntersectRects(t *testing.T) {
	a := walk.Rectangle{X: 0, Y: 0, Width: 100, Height: 100}
	b := walk.Rectangle{X: 50, Y: 50, Width: 100, Height: 100}
	if got := intersectRects(a, b); got != (walk.Rectangle{X: 50, Y: 50, Width: 50, Height: 50}) {
		t.Errorf("got %+v", got)
	}
	apart := walk.Rectangle{X: 500, Y: 500, Width: 10, Height: 10}
	if got := intersectRects(a, apart); got.Width != 0 || got.Height != 0 {
		t.Errorf("disjoint rectangles overlapped: %+v", got)
	}
	// Touching edges are not an overlap.
	touching := walk.Rectangle{X: 100, Y: 0, Width: 10, Height: 100}
	if got := intersectRects(a, touching); got.Width != 0 {
		t.Errorf("touching rectangles reported an overlap: %+v", got)
	}
}

// TestMonitorUnderPicksTheScreenTheBoxIsMostlyOn matters on a multi-screen
// desktop: a box dragged across the join belongs to the screen it covers more
// of, not to whichever one happens to be first.
func TestMonitorUnderPicksTheScreenTheBoxIsMostlyOn(t *testing.T) {
	c := newZoneCanvas()
	c.mons = monitor.NewSetForTest([]monitor.Monitor{
		{Index: 0, Bounds: win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
			WorkArea: win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080}, Primary: true, Device: `\.\DISPLAY1`},
		{Index: 1, Bounds: win.RECT{Left: 1920, Top: 0, Right: 3840, Bottom: 1080},
			WorkArea: win.RECT{Left: 1920, Top: 0, Right: 3840, Bottom: 1080}, Device: `\.\DISPLAY2`},
	})
	src := win.RECT{Left: 0, Top: 0, Right: 3840, Bottom: 1080}
	v := canvasView{src: src, scale: 0.1, offX: 0, offY: 0, ok: true}

	// Mostly on the right-hand screen.
	box := walk.Rectangle{X: 180, Y: 20, Width: 100, Height: 40}
	m := c.monitorUnder(v, box)
	if m == nil {
		t.Fatal("no screen chosen")
	}
	if m.Index != 1 {
		t.Errorf("chose screen %d, want 1", m.Index)
	}
}
