package zones

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
)

// fakeSet builds a monitor.Set without touching the OS, so geometry can be
// tested on a machine with any display configuration.
func fakeSet(t *testing.T, rects ...win.RECT) *monitor.Set {
	t.Helper()
	mons := make([]monitor.Monitor, len(rects))
	for i, r := range rects {
		mons[i] = monitor.Monitor{
			Index:    i,
			Bounds:   r,
			WorkArea: r,
			Device:   "\\\\.\\DISPLAY" + string(rune('1'+i)),
			Primary:  i == 0,
		}
	}
	return monitor.NewSetForTest(mons)
}

// fullScreen marks every dock as measuring against the whole screen rather than
// the work area. The fixtures below use monitors with no taskbar, so this only
// states the assumption; TestDockStopsAtTheTaskbar is where it bites.
func fullScreen(rules []config.Rule) []config.Rule {
	out := make([]config.Rule, len(rules))
	for i, r := range rules {
		f := false
		r.Dock.UseWorkArea = &f
		out[i] = r
	}
	return out
}

const (
	uwWidth  = 3440
	uwHeight = 1440
)

func ultrawide(t *testing.T) *monitor.Set {
	return fakeSet(t, win.RECT{Left: 0, Top: 0, Right: uwWidth, Bottom: uwHeight})
}

func TestEdgeTriggerAndDockGeometry(t *testing.T) {
	// Rule taken verbatim from the shipped 21:9 profile: the left edge, upper
	// half, docks a window into the leftmost 20% x 50% of the screen.
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 20, 50}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 50}},
	}}

	l := Resolve(fullScreen(rules), ultrawide(t), config.Global{})
	if l.Len() != 1 {
		t.Fatalf("got %d zones (skipped %d), want 1", l.Len(), l.Skipped)
	}
	z := l.Zones()[0]

	wantDock := win.RECT{Left: 0, Top: 0, Right: 688, Bottom: 720}
	if z.Dock != wantDock {
		t.Errorf("dock = %+v, want %+v", z.Dock, wantDock)
	}

	// The hit region hugs the left edge and spans the top half.
	wantHit := win.RECT{Left: 0, Top: 0, Right: config.DefaultEdgeThicknessPx, Bottom: 720}
	if z.Hit != wantHit {
		t.Errorf("hit = %+v, want %+v", z.Hit, wantHit)
	}

	// Cursor on the edge in the top half matches; the bottom half does not.
	if got := l.Match(win.POINT{X: 0, Y: 100}); got == nil {
		t.Error("cursor at left edge, top half should match")
	}
	if got := l.Match(win.POINT{X: 0, Y: 900}); got != nil {
		t.Error("cursor at left edge, bottom half should not match")
	}
	// Cursor away from the edge does not match.
	if got := l.Match(win.POINT{X: 400, Y: 100}); got != nil {
		t.Error("cursor away from the edge should not match")
	}
}

func TestCenterBandRoundsToNearest(t *testing.T) {
	// The 21:9 profile docks the top edge to a centred band of 12%..87%.
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{12, 0, 87, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosTop, Type: config.TypeEdge, Values: []float64{0, 100}},
	}}
	l := Resolve(fullScreen(rules), ultrawide(t), config.Global{})
	if l.Len() != 1 {
		t.Fatalf("got %d zones, want 1", l.Len())
	}
	// 3440 * 0.12 = 412.8 -> 413; 3440 * 0.87 = 2992.8 -> 2993.
	want := win.RECT{Left: 413, Top: 0, Right: 2993, Bottom: uwHeight}
	if got := l.Zones()[0].Dock; got != want {
		t.Errorf("dock = %+v, want %+v", got, want)
	}
}

func TestAdjacentZonesMeetWithoutSeam(t *testing.T) {
	// The three bottom-edge rules of the 21:9 profile tile the screen in
	// thirds. Rounding must make them share exact boundaries.
	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 33, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosBottom, Type: config.TypeEdge, Values: []float64{0, 33}},
		},
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{33, 0, 67, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosBottom, Type: config.TypeEdge, Values: []float64{33, 67}},
		},
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{67, 0, 100, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosBottom, Type: config.TypeEdge, Values: []float64{67, 100}},
		},
	}
	l := Resolve(fullScreen(rules), ultrawide(t), config.Global{})
	if l.Len() != 3 {
		t.Fatalf("got %d zones, want 3", l.Len())
	}
	z := l.Zones()
	if z[0].Dock.Right != z[1].Dock.Left {
		t.Errorf("seam between zone 0 and 1: %d != %d", z[0].Dock.Right, z[1].Dock.Left)
	}
	if z[1].Dock.Right != z[2].Dock.Left {
		t.Errorf("seam between zone 1 and 2: %d != %d", z[1].Dock.Right, z[2].Dock.Left)
	}
	if z[2].Dock.Right != uwWidth {
		t.Errorf("last zone ends at %d, want %d", z[2].Dock.Right, uwWidth)
	}
}

func TestCornerBeatsEdgeRegardlessOfOrder(t *testing.T) {
	// The README is explicit: a corner trigger wins over an edge trigger even
	// when the edge rule is listed first.
	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 50}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosTopLeft, Type: config.TypeCorner},
		},
	}
	l := Resolve(fullScreen(rules), ultrawide(t), config.Global{})
	if l.Len() != 2 {
		t.Fatalf("got %d zones, want 2", l.Len())
	}
	// Top-left pixel lies in both hit regions; the corner must win.
	m := l.Match(win.POINT{X: 0, Y: 0})
	if m == nil {
		t.Fatal("top-left corner should match something")
	}
	if m.RuleIndex != 1 {
		t.Errorf("matched rule %d, want the corner rule (1)", m.RuleIndex)
	}
	// Further down the left edge, only the edge rule applies.
	m = l.Match(win.POINT{X: 0, Y: 800})
	if m == nil || m.RuleIndex != 0 {
		t.Errorf("mid left edge should match the edge rule, got %+v", m)
	}
}

func TestEarlierRuleWinsWithinSamePriority(t *testing.T) {
	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 25, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 50}},
		},
	}
	l := Resolve(fullScreen(rules), ultrawide(t), config.Global{})
	m := l.Match(win.POINT{X: 0, Y: 100})
	if m == nil || m.RuleIndex != 0 {
		t.Errorf("overlapping edges should resolve to the first rule, got %+v", m)
	}
}

// TestRulesForMissingMonitorAreSkipped covers the RDP regression directly: when
// a display goes away, its rules must stop producing zones rather than
// resolving against whatever monitor happens to be left.
func TestRulesForMissingMonitorAreSkipped(t *testing.T) {
	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{
			Dock:    config.Dock{Monitor: 1, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 1, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
	}

	two := fakeSet(t,
		win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		win.RECT{Left: 1920, Top: 0, Right: 3840, Bottom: 1080},
	)
	if l := Resolve(fullScreen(rules), two, config.Global{}); l.Len() != 2 || l.Skipped != 0 {
		t.Fatalf("two monitors: got %d zones / %d skipped, want 2/0", l.Len(), l.Skipped)
	}

	one := fakeSet(t, win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080})
	l := Resolve(fullScreen(rules), one, config.Global{})
	if l.Len() != 1 {
		t.Errorf("one monitor: got %d zones, want 1", l.Len())
	}
	if l.Skipped != 1 {
		t.Errorf("one monitor: got %d skipped, want 1", l.Skipped)
	}
	for _, z := range l.Zones() {
		if z.DockMonitor != 0 {
			t.Errorf("zone docked to monitor %d, which is not attached", z.DockMonitor)
		}
	}
}

func TestNoMonitorsResolvesToNothing(t *testing.T) {
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
	}}
	l := Resolve(rules, monitor.NewSetForTest(nil), config.Global{})
	if l.Len() != 0 {
		t.Errorf("got %d zones with no monitors attached, want 0", l.Len())
	}
	if m := l.Match(win.POINT{X: 0, Y: 0}); m != nil {
		t.Error("nothing should match with no monitors attached")
	}
}

func TestInvalidRulesAreSkipped(t *testing.T) {
	rules := []config.Rule{
		{ // inverted dock
			Dock:    config.Dock{Monitor: 0, Values: []float64{50, 0, 10, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{ // unknown position
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: "sideways", Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{ // missing dock values
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
	}
	l := Resolve(fullScreen(rules), ultrawide(t), config.Global{})
	if l.Len() != 0 || l.Skipped != 3 {
		t.Errorf("got %d zones / %d skipped, want 0/3", l.Len(), l.Skipped)
	}
}

// TestShippedProfileResolves loads the real 21:9 profile from the repository
// root and checks every rule resolves on an ultrawide display.
func TestShippedProfileResolves(t *testing.T) {
	path := filepath.Join("..", "..", "21-9-profile.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("sample profile not present: %v", err)
	}
	var p config.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if p.Name != "21:9 Profile" {
		t.Errorf("profile name = %q", p.Name)
	}
	if len(p.Rules) != 8 {
		t.Fatalf("got %d rules, want 8", len(p.Rules))
	}

	l := Resolve(fullScreen(p.Rules), ultrawide(t), config.Global{})
	if l.Skipped != 0 {
		t.Errorf("%d rules skipped, want 0", l.Skipped)
	}
	if l.Len() != 8 {
		t.Fatalf("got %d zones, want 8", l.Len())
	}
	for _, z := range l.Zones() {
		if z.Dock.IsEmpty() {
			t.Errorf("rule %d produced an empty dock", z.RuleIndex)
		}
		if z.Dock.Left < 0 || z.Dock.Right > uwWidth {
			t.Errorf("rule %d dock %+v escapes the monitor", z.RuleIndex, z.Dock)
		}
	}
}

func TestDockUsesWorkAreaWhenEnabled(t *testing.T) {
	mons := monitor.NewSetForTest([]monitor.Monitor{{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		WorkArea: win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040}, // 40px taskbar
		Primary:  true,
	}})
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 100, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosTop, Type: config.TypeEdge, Values: []float64{0, 100}},
	}}

	l := Resolve(rules, mons, config.Global{}) // UseWorkArea defaults to true
	if got := l.Zones()[0].Dock.Bottom; got != 1040 {
		t.Errorf("dock bottom = %d, want 1040 (above the taskbar)", got)
	}
	// The trigger still reaches the physical bottom edge of the screen.
	l2 := Resolve([]config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 100, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosBottom, Type: config.TypeEdge, Values: []float64{0, 100}},
	}}, mons, config.Global{})
	if got := l2.Zones()[0].Hit.Bottom; got != 1080 {
		t.Errorf("trigger bottom = %d, want 1080 (the physical edge)", got)
	}
}

// withTaskbar is a 3440x1440 monitor with a 48px taskbar along the bottom,
// matching the display this was reported on.
func withTaskbar() *monitor.Set {
	return monitor.NewSetForTest([]monitor.Monitor{{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: uwWidth, Bottom: uwHeight},
		WorkArea: win.RECT{Left: 0, Top: 0, Right: uwWidth, Bottom: uwHeight - 48},
		Primary:  true,
	}})
}

// TestBottomTriggerReachesOverTheTaskbar is the regression test for bottom-edge
// triggers being unreachable.
//
// A strip measured only from the physical edge lies entirely underneath the
// taskbar. Applications that draw their own title bar, such as anything built
// on Electron, clamp a dragged window to the work area, so the window stops
// dead at the top of the taskbar while the trigger is still 48px further down.
// The trigger has to span from the work-area edge out to the physical edge so
// that it fires either way.
func TestBottomTriggerReachesOverTheTaskbar(t *testing.T) {
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 33, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosBottom, Type: config.TypeEdge, Values: []float64{0, 33}},
	}}
	l := Resolve(rules, withTaskbar(), config.Global{})
	if l.Len() != 1 {
		t.Fatalf("got %d zones, want 1", l.Len())
	}
	hit := l.Zones()[0].Hit

	const workBottom = uwHeight - 48 // 1392
	if hit.Bottom != uwHeight {
		t.Errorf("trigger should extend to the physical edge %d, got %d", uwHeight, hit.Bottom)
	}
	if hit.Top >= workBottom {
		t.Errorf("trigger starts at y=%d, below the work-area edge %d; a window "+
			"clamped to the work area could never reach it", hit.Top, workBottom)
	}

	// A pointer stopping where a clamped window stops must fire the trigger.
	if l.Match(win.POINT{X: 500, Y: workBottom - 1}) == nil {
		t.Error("pointer just above the taskbar should activate the bottom trigger")
	}
	// So must a pointer slammed all the way into the physical edge.
	if l.Match(win.POINT{X: 500, Y: uwHeight - 1}) == nil {
		t.Error("pointer at the physical bottom edge should activate the bottom trigger")
	}
	// Well above the taskbar it must not.
	if l.Match(win.POINT{X: 500, Y: workBottom - 200}) != nil {
		t.Error("pointer far from the edge should not activate the bottom trigger")
	}
}

// TestEdgeTriggersUnchangedWithoutAnAppbar pins the common case: where no
// appbar occupies an edge, the work area and the bounds coincide and the
// trigger is exactly edgeThicknessPx deep, as before.
func TestEdgeTriggersUnchangedWithoutAnAppbar(t *testing.T) {
	for _, pos := range []string{config.PosLeft, config.PosRight, config.PosTop, config.PosBottom} {
		t.Run(pos, func(t *testing.T) {
			rules := []config.Rule{{
				Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 50}},
				Trigger: config.Trigger{Monitor: 0, Pos: pos, Type: config.TypeEdge, Values: []float64{0, 100}},
			}}
			l := Resolve(rules, ultrawide(t), config.Global{})
			if l.Len() != 1 {
				t.Fatalf("got %d zones, want 1", l.Len())
			}
			hit := l.Zones()[0].Hit
			depth := hit.Width()
			if pos == config.PosTop || pos == config.PosBottom {
				depth = hit.Height()
			}
			if depth != config.DefaultEdgeThicknessPx {
				t.Errorf("%s trigger is %dpx deep, want %d when no appbar occupies that edge",
					pos, depth, config.DefaultEdgeThicknessPx)
			}
		})
	}
}

// TestSideTaskbarExtendsTheMatchingEdge covers a taskbar docked to the left.
func TestSideTaskbarExtendsTheMatchingEdge(t *testing.T) {
	mons := monitor.NewSetForTest([]monitor.Monitor{{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		WorkArea: win.RECT{Left: 60, Top: 0, Right: 1920, Bottom: 1080},
		Primary:  true,
	}})
	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{50, 0, 100, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosRight, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
	}
	l := Resolve(rules, mons, config.Global{})
	left, right := l.Zones()[0].Hit, l.Zones()[1].Hit

	if left.Left != 0 {
		t.Errorf("left trigger should still reach the physical edge, got Left=%d", left.Left)
	}
	if left.Right <= 60 {
		t.Errorf("left trigger should extend past the 60px taskbar, got Right=%d", left.Right)
	}
	if l.Match(win.POINT{X: 61, Y: 500}) == nil {
		t.Error("pointer just inside the work area should activate the left trigger")
	}
	// The opposite edge has no appbar and must be unaffected.
	if got := right.Right - right.Left; got != config.DefaultEdgeThicknessPx {
		t.Errorf("right trigger is %dpx deep, want %d", got, config.DefaultEdgeThicknessPx)
	}
}

// TestStarterTemplatesResolveOnOneScreen checks the shipped layouts end to end.
// A rule that fails to resolve is skipped silently, so a broken template would
// present to a new user as "the program does nothing" - and a first run is
// exactly where nobody knows to look in the log.
func TestStarterTemplatesResolveOnOneScreen(t *testing.T) {
	// 1920x1080 with a 40px taskbar along the bottom.
	mons := monitor.NewSetForTest([]monitor.Monitor{{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		WorkArea: win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040},
		Device:   `\.\DISPLAY1`,
		Primary:  true,
	}})

	for _, tpl := range config.Templates() {
		t.Run(tpl.Name, func(t *testing.T) {
			l := Resolve(tpl.Rules, mons, config.Global{})
			if l.Skipped != 0 {
				t.Errorf("%d of %d rules were skipped", l.Skipped, len(tpl.Rules))
			}
			if l.Len() != len(tpl.Rules) {
				t.Fatalf("resolved %d zones from %d rules", l.Len(), len(tpl.Rules))
			}
			for i, z := range l.Zones() {
				if z.Dock.IsEmpty() {
					t.Errorf("rule %d docks to an empty rectangle", i)
				}
				if z.Hit.IsEmpty() {
					t.Errorf("rule %d has an unreachable trigger", i)
				}
			}
		})
	}
}

// TestStarterTemplatesAreReachable checks that every trigger in a shipped
// layout can actually be hit with the cursor, and that hitting it selects the
// rule it belongs to rather than one that overlaps it.
func TestStarterTemplatesAreReachable(t *testing.T) {
	mons := monitor.NewSetForTest([]monitor.Monitor{{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		WorkArea: win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040},
		Device:   `\.\DISPLAY1`,
		Primary:  true,
	}})

	for _, tpl := range config.Templates() {
		t.Run(tpl.Name, func(t *testing.T) {
			l := Resolve(tpl.Rules, mons, config.Global{})
			for i, z := range l.Zones() {
				mid := win.POINT{
					X: z.Hit.Left + z.Hit.Width()/2,
					Y: z.Hit.Top + z.Hit.Height()/2,
				}
				hit := l.MatchIndex(mid)
				if hit < 0 {
					t.Errorf("rule %d: the centre of its own trigger matches nothing", i)
					continue
				}
				if hit != i {
					t.Logf("rule %d is shadowed by rule %d at the centre of its trigger", i, hit)
				}
			}
		})
	}
}

// TestWorkAreaIsPerZone is the point of the setting: two zones on one screen,
// one stopping at the taskbar and one running under it.
func TestWorkAreaIsPerZone(t *testing.T) {
	mons := monitor.NewSetForTest([]monitor.Monitor{{
		Index:    0,
		Bounds:   win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		WorkArea: win.RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1040}, // 40px taskbar
		Primary:  true,
	}})
	no := false
	rules := []config.Rule{
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
		{
			Dock:    config.Dock{Monitor: 0, Values: []float64{50, 0, 100, 100}, UseWorkArea: &no},
			Trigger: config.Trigger{Monitor: 0, Pos: config.PosRight, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
	}

	l := Resolve(rules, mons, config.Global{})
	if l.Len() != 2 {
		t.Fatalf("got %d zones (skipped %d), want 2", l.Len(), l.Skipped)
	}
	byRule := map[int]Zone{}
	for _, z := range l.Zones() {
		byRule[z.RuleIndex] = z
	}
	if got := byRule[0].Dock.Bottom; got != 1040 {
		t.Errorf("the default zone ends at %d, want 1040 (above the taskbar)", got)
	}
	if got := byRule[1].Dock.Bottom; got != 1080 {
		t.Errorf("the opted-out zone ends at %d, want 1080 (the physical edge)", got)
	}
	if !byRule[0].WorkArea || byRule[1].WorkArea {
		t.Errorf("Zone.WorkArea = %v and %v, want true and false",
			byRule[0].WorkArea, byRule[1].WorkArea)
	}
}

// --- matching, which is now what reveals the guides -------------------------

// The guides come up on a hit, so a drag through open space must match nothing.
func TestMatchIndexIgnoresTheMiddleOfTheScreen(t *testing.T) {
	rules := fullScreen([]config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
	}})
	l := Resolve(rules, ultrawide(t), config.Global{})

	center := win.POINT{X: uwWidth / 2, Y: uwHeight / 2}
	if i := l.MatchIndex(center); i >= 0 {
		t.Errorf("a cursor in the middle of the screen matched zone %d", i)
	}
}

// A layout with no zones, and a nil one, must answer rather than panic: the
// mouse hook calls this on every move.
func TestMatchIndexHandlesAnEmptyLayout(t *testing.T) {
	var nilLayout *Layout
	if i := nilLayout.MatchIndex(win.POINT{}); i >= 0 {
		t.Errorf("a nil layout matched zone %d", i)
	}
	empty := Resolve(nil, ultrawide(t), config.Global{})
	if i := empty.MatchIndex(win.POINT{}); i >= 0 {
		t.Errorf("an empty layout matched zone %d", i)
	}
}
