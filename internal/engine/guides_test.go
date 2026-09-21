package engine

import (
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

func uw() monitor.Monitor {
	return monitor.Monitor{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: 3440, Bottom: 1440},
		WorkArea: win.RECT{Left: 0, Top: 0, Right: 3440, Bottom: 1392},
		Device:   "\\\\.\\DISPLAY1",
		Primary:  true,
	}
}

func layoutFor(t *testing.T, rules []config.Rule, m monitor.Monitor) *zones.Layout {
	t.Helper()
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	return zones.Resolve(rules, set, config.Global{})
}

// guideShapesFor resolves rules on a single monitor and builds the guide shapes
// for it, including the palette-slot assignment.
func guideShapesFor(t *testing.T, m monitor.Monitor, rules []config.Rule, thickness int32) []guideShape {
	t.Helper()
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	l := zones.Resolve(rules, set, config.Global{})
	return shapesForMonitor(m, l, assignSlots(set, l), thickness)
}

// TestEdgeGuideIsThickerThanTheTrigger is the whole point of the feature: the
// region that actually triggers is only a few pixels deep, so the guide has to
// be drawn thicker to be visible at all.
func TestEdgeGuideIsThickerThanTheTrigger(t *testing.T) {
	m := uw()
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 20, 50}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 50}},
	}}
	l := layoutFor(t, rules, m)

	const thickness = 10
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	shapes := shapesForMonitor(m, l, assignSlots(set, l), thickness)
	if len(shapes) != 1 {
		t.Fatalf("got %d shapes, want 1", len(shapes))
	}
	band := shapes[0].rects[0]

	if got := band.Width(); got != thickness {
		t.Errorf("guide band is %dpx deep, want %d", got, thickness)
	}
	if band.Width() <= l.Zones()[0].Hit.Width() {
		t.Error("the guide must be drawn thicker than the trigger it represents")
	}
	if band.Left != 0 {
		t.Errorf("a left-edge guide should start at the screen edge, got Left=%d", band.Left)
	}
	// It should span the trigger's half of the screen, less the separating gap.
	if band.Top < 0 || band.Bottom > 720 {
		t.Errorf("guide %+v escapes the trigger's span (0..720)", band)
	}
}

func TestAdjacentEdgeGuidesAreVisuallySeparated(t *testing.T) {
	m := uw()
	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 20, 50}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 50}},
		},
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 50, 20, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{50, 100}},
		},
	}
	shapes := guideShapesFor(t, m, rules, 10)
	if len(shapes) != 2 {
		t.Fatalf("got %d shapes, want 2", len(shapes))
	}
	first, second := shapes[0].rects[0], shapes[1].rects[0]
	if first.Bottom >= second.Top {
		t.Errorf("guides touch or overlap (%d >= %d); adjacent segments must read as separate targets",
			first.Bottom, second.Top)
	}
}

func TestEdgeGuidesGrowInwardFromTheirOwnEdge(t *testing.T) {
	m := uw()
	for _, tc := range []struct {
		pos   string
		check func(t *testing.T, r win.RECT)
	}{
		{config.PosLeft, func(t *testing.T, r win.RECT) {
			if r.Left != 0 {
				t.Errorf("left guide should hug x=0, got %+v", r)
			}
		}},
		{config.PosRight, func(t *testing.T, r win.RECT) {
			if r.Right != 3440 {
				t.Errorf("right guide should hug x=3440, got %+v", r)
			}
		}},
		{config.PosTop, func(t *testing.T, r win.RECT) {
			if r.Top != 0 {
				t.Errorf("top guide should hug y=0, got %+v", r)
			}
		}},
		{config.PosBottom, func(t *testing.T, r win.RECT) {
			// The work area ends at 1392; the taskbar owns 1392..1440. The
			// guide belongs on the visible side of it, which is also where a
			// dragged window comes to rest.
			if r.Bottom != 1392 {
				t.Errorf("bottom guide should sit on the work-area edge y=1392, got %+v", r)
			}
		}},
	} {
		t.Run(tc.pos, func(t *testing.T) {
			rules := []config.Rule{{
				Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 50}},
				Trigger: config.Trigger{Monitor: 0, Pos: tc.pos, Type: config.TypeEdge, Values: []float64{0, 100}},
			}}
			shapes := guideShapesFor(t, m, rules, 10)
			if len(shapes) != 1 {
				t.Fatalf("got %d shapes, want 1", len(shapes))
			}
			tc.check(t, shapes[0].rects[0])
		})
	}
}

func TestCornerGuideIsAnLAtTheCorner(t *testing.T) {
	m := uw()
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 50}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosTopLeft, Type: config.TypeCorner},
	}}
	shapes := guideShapesFor(t, m, rules, 10)
	if len(shapes) != 1 {
		t.Fatalf("got %d shapes, want 1", len(shapes))
	}
	arms := shapes[0].rects
	if len(arms) != 2 {
		t.Fatalf("a corner guide should have 2 arms, got %d", len(arms))
	}
	for _, a := range arms {
		if a.Left != 0 || a.Top != 0 {
			t.Errorf("both arms should meet at the corner, got %+v", a)
		}
	}
	// One arm runs horizontally, the other vertically.
	if !(arms[0].Width() > arms[0].Height() && arms[1].Height() > arms[1].Width()) {
		t.Errorf("expected one horizontal and one vertical arm, got %+v and %+v", arms[0], arms[1])
	}
}

func TestAreaGuideIsAFrame(t *testing.T) {
	m := uw()
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 50}},
		Trigger: config.Trigger{Monitor: 0, Type: config.TypeArea, Values: []float64{25, 25, 75, 75}},
	}}
	shapes := guideShapesFor(t, m, rules, 10)
	if len(shapes) != 1 {
		t.Fatalf("got %d shapes, want 1", len(shapes))
	}
	if n := len(shapes[0].rects); n != 4 {
		t.Errorf("an area guide should be drawn as 4 sides, got %d", n)
	}
}

func TestGuidesOnlyCoverTheirOwnMonitor(t *testing.T) {
	left := monitor.Monitor{Index: 0, Bounds: win.RECT{Right: 1920, Bottom: 1080},
		WorkArea: win.RECT{Right: 1920, Bottom: 1080}, Primary: true}
	right := monitor.Monitor{Index: 1,
		Bounds:   win.RECT{Left: 1920, Right: 3840, Bottom: 1080},
		WorkArea: win.RECT{Left: 1920, Right: 3840, Bottom: 1080}}
	set := monitor.NewSetForTest([]monitor.Monitor{left, right})

	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{
			Dock:    config.Dock{Monitor: 1, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 1, Pos: config.PosRight, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
	}
	l := zones.Resolve(rules, set, config.Global{})

	for _, m := range []monitor.Monitor{left, right} {
		shapes := shapesForMonitor(m, l, assignSlots(set, l), 10)
		if len(shapes) != 1 {
			t.Fatalf("monitor %d: got %d shapes, want 1", m.Index, len(shapes))
		}
		// Shapes are in client coordinates, so they must fit inside the
		// monitor's own size regardless of where it sits on the desktop.
		for _, r := range shapes[0].rects {
			if r.Left < 0 || r.Top < 0 || r.Right > m.Bounds.Width() || r.Bottom > m.Bounds.Height() {
				t.Errorf("monitor %d: shape %+v escapes client bounds %dx%d",
					m.Index, r, m.Bounds.Width(), m.Bounds.Height())
			}
		}
	}
}

func TestGuidesDisabledProduceNoWindows(t *testing.T) {
	off := false
	cfg := config.Global{ShowTriggerGuides: &off}
	m := uw()
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 20, 50}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 50}},
	}}
	set := monitor.NewSetForTest([]monitor.Monitor{m})

	g := newGuides()
	t.Cleanup(g.destroy)
	lay := zones.Resolve(rules, set, cfg)
	g.rebuild(set, lay, cfg, assignSlots(set, lay))

	if len(g.windows) != 0 {
		t.Errorf("guides are disabled but %d windows were created", len(g.windows))
	}
	g.show() // must be a harmless no-op
	if g.shown {
		t.Error("disabled guides should never report themselves as shown")
	}
}
