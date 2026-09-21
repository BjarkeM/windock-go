package engine

import (
	"math"
	"sort"
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/palette"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

func edge(pos string, from, to float64) config.Rule {
	return config.Rule{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 50}},
		Trigger: config.Trigger{Monitor: 0, Pos: pos, Type: config.TypeEdge, Values: []float64{from, to}},
	}
}

// shippedProfileRules mirrors the 21:9 profile: triggers on all four edges,
// which is exactly the case that made a single-colour guide render as one
// featureless frame around the monitor.
func shippedProfileRules() []config.Rule {
	return []config.Rule{
		edge(config.PosLeft, 0, 50),
		edge(config.PosLeft, 50, 100),
		edge(config.PosTop, 0, 100),
		edge(config.PosRight, 0, 50),
		edge(config.PosRight, 50, 100),
		edge(config.PosBottom, 0, 33),
		edge(config.PosBottom, 33, 67),
		edge(config.PosBottom, 67, 100),
	}
}

func TestEveryTriggerGetsItsOwnColourWhenTheyFit(t *testing.T) {
	m := uw()
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	l := zones.Resolve(shippedProfileRules(), set, config.Global{})
	if l.Len() != 8 {
		t.Fatalf("got %d zones, want 8", l.Len())
	}

	slots := assignSlots(set, l)
	seen := map[uint32]int{}
	for i := range slots {
		seen[palette.Color(slots[i], true, 0)]++
	}
	if len(seen) != 8 {
		t.Errorf("8 triggers produced only %d distinct colours: %v", len(seen), seen)
	}
}

// TestSlotsFollowThePerimeter is the guarantee the palette depends on. The
// palette is validated for ADJACENT slots, so triggers that sit next to each
// other on screen must receive slots that sit next to each other in the palette.
func TestSlotsFollowThePerimeter(t *testing.T) {
	m := uw()
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	l := zones.Resolve(shippedProfileRules(), set, config.Global{})
	slots := assignSlots(set, l)

	// Walk the zones in the same clockwise order the assignment uses and check
	// the slots come out strictly increasing.
	type pair struct {
		angle float64
		slot  int
	}
	var ring []pair
	cx := float64(m.Bounds.Left+m.Bounds.Right) / 2
	cy := float64(m.Bounds.Top+m.Bounds.Bottom) / 2
	for i, z := range l.Zones() {
		px := float64(z.Hit.Left+z.Hit.Right) / 2
		py := float64(z.Hit.Top+z.Hit.Bottom) / 2
		ring = append(ring, pair{palette.ClockwiseFromTop(px-cx, py-cy), slots[i]})
	}
	for i := range ring {
		for j := i + 1; j < len(ring); j++ {
			if ring[i].angle < ring[j].angle && ring[i].slot >= ring[j].slot {
				t.Errorf("slots do not follow the perimeter: angle %.2f got slot %d, "+
					"but later angle %.2f got slot %d",
					ring[i].angle, ring[i].slot, ring[j].angle, ring[j].slot)
			}
		}
	}
}

func TestClockwiseFromTopOrdersTheCompass(t *testing.T) {
	up := palette.ClockwiseFromTop(0, -1)
	right := palette.ClockwiseFromTop(1, 0)
	down := palette.ClockwiseFromTop(0, 1)
	left := palette.ClockwiseFromTop(-1, 0)

	if math.Abs(up) > 1e-9 {
		t.Errorf("straight up should be angle 0, got %v", up)
	}
	if !(up < right && right < down && down < left) {
		t.Errorf("expected clockwise ordering up<right<down<left, got %v %v %v %v",
			up, right, down, left)
	}
	if left >= 2*math.Pi {
		t.Errorf("angles must be normalised below 2pi, got %v", left)
	}
}

// TestColoursOnlyRepeatFarApart covers a profile with more triggers than the
// palette has colours: a repeat is unavoidable, but the two must not end up
// beside each other.
func TestColoursOnlyRepeatFarApart(t *testing.T) {
	m := uw()
	// Twelve triggers around the perimeter, more than the eight-colour palette.
	var rules []config.Rule
	for i := 0; i < 3; i++ {
		lo, hi := float64(i)*33, float64(i+1)*33
		rules = append(rules,
			edge(config.PosTop, lo, hi),
			edge(config.PosRight, lo, hi),
			edge(config.PosBottom, lo, hi),
			edge(config.PosLeft, lo, hi),
		)
	}
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	l := zones.Resolve(rules, set, config.Global{})
	if l.Len() != 12 {
		t.Fatalf("got %d zones, want 12", l.Len())
	}
	slots := assignSlots(set, l)

	n := len(paletteColors)
	byColour := map[uint32][]int{}
	for i := range slots {
		c := palette.Color(slots[i], true, 0)
		byColour[c] = append(byColour[c], slots[i])
	}
	for c, used := range byColour {
		if len(used) < 2 {
			continue
		}
		// Compare in perimeter order, which is the order the slot numbers
		// themselves encode.
		sort.Ints(used)
		for i := 1; i < len(used); i++ {
			if gap := used[i] - used[i-1]; gap < n {
				t.Errorf("colour %#06x reused only %d positions apart around the "+
					"perimeter; the palette has %d entries so a repeat should be "+
					"a full cycle away", c, gap, n)
			}
		}
	}
}

func TestSlotsAreStableAcrossRebuilds(t *testing.T) {
	m := uw()
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	l := zones.Resolve(shippedProfileRules(), set, config.Global{})

	first := assignSlots(set, l)
	second := assignSlots(set, l)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("zone %d moved from slot %d to %d between identical rebuilds",
				i, first[i], second[i])
		}
	}
}

func TestEachMonitorGetsItsOwnColourRun(t *testing.T) {
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
			Trigger: config.Trigger{Monitor: 1, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
		},
	}
	l := zones.Resolve(rules, set, config.Global{})
	slots := assignSlots(set, l)

	// Each monitor starts its own run, so the sole trigger on each takes slot 0.
	for i, s := range slots {
		if s != 0 {
			t.Errorf("zone %d got slot %d; each monitor should start its run at 0", i, s)
		}
	}
}

func TestSingleColourModeCollapsesThePalette(t *testing.T) {
	const custom = 0x123456
	if got := palette.Color(3, false, custom); got != custom {
		t.Errorf("single-colour mode returned %#06x, want %#06x", got, custom)
	}
	if got := palette.Color(3, true, custom); got != paletteColors[3] {
		t.Errorf("categorical mode returned %#06x, want slot 3 %#06x", got, paletteColors[3])
	}
}

func TestBrushesWrapForOversizedProfiles(t *testing.T) {
	initPalette(0x0078D7, true)
	t.Cleanup(freePalette)

	n := len(paletteColors)
	if brushesFor(0) != brushesFor(n) {
		t.Error("slot n should wrap onto slot 0")
	}
	if brushesFor(-1) != brushesFor(n-1) {
		t.Error("a negative slot should wrap onto the last entry, not panic")
	}
	for i := 0; i < n; i++ {
		b := brushesFor(i)
		if b.fill == 0 || b.border == 0 {
			t.Errorf("slot %d has an uninitialised brush", i)
		}
	}
}

func TestBrushesAreSafeBeforeInitialisation(t *testing.T) {
	freePalette()
	if b := brushesFor(0); b.fill != 0 || b.border != 0 {
		t.Error("an uninitialised palette should hand back zero brushes, not stale ones")
	}
}

// TestPaletteIsTheValidatedSet guards the hexes against a careless edit. The
// order is the colourblind-safety mechanism, so it is pinned too.
func TestPaletteIsTheValidatedSet(t *testing.T) {
	want := []uint32{0x2A78D6, 0xEB6834, 0x1BAF7A, 0xEDA100, 0xE87BA4, 0x008300, 0x4A3AA7, 0xE34948}
	if len(paletteColors) != len(want) {
		t.Fatalf("palette has %d colours, want %d", len(paletteColors), len(want))
	}
	for i := range want {
		if paletteColors[i] != want[i] {
			t.Errorf("slot %d is %#06x, want %#06x; the order was validated for "+
				"adjacent-pair separation and must not be reshuffled casually",
				i, paletteColors[i], want[i])
		}
	}
}
