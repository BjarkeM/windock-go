package ui

import (
	"image"
	"strings"
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
)

// These cover the parts of the UI that are plain logic: how a rule is described
// in the table, and which choices each trigger type offers. The widgets
// themselves need a desktop and are exercised by running the program.

func TestDescribeTrigger(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger config.Trigger
		want    string
	}{
		{
			"edge",
			config.Trigger{Type: config.TypeEdge, Pos: config.PosLeft, Values: []float64{0, 50}},
			"left edge, 0% to 50%",
		},
		{
			"corner",
			config.Trigger{Type: config.TypeCorner, Pos: config.PosBottomRight},
			"corner bottom right",
		},
		{
			"area",
			config.Trigger{Type: config.TypeArea, Values: []float64{10, 20, 30, 40}},
			"area 10,20 to 30,40",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeTrigger(tc.trigger); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDescribeTriggerSurvivesMalformedRules matters because profile.json can be
// hand-edited: the table must render something rather than panic on a slice
// that is shorter than expected.
func TestDescribeTriggerSurvivesMalformedRules(t *testing.T) {
	cases := []config.Trigger{
		{Type: config.TypeEdge, Pos: config.PosLeft},                       // no values
		{Type: config.TypeEdge, Pos: config.PosLeft, Values: []float64{5}}, // one value
		{Type: config.TypeArea},                                            // no values
		{Type: config.TypeArea, Values: []float64{1, 2}},                   // too few
		{Type: "nonsense"},
		{},
	}
	for i, tr := range cases {
		got := describeTrigger(tr)
		if got == "" {
			t.Errorf("case %d produced an empty description", i)
		}
	}
}

func TestDescribeDock(t *testing.T) {
	// The original writes a dock as a bracketed quadruple, and its General box
	// even labels one such: "Maximize [0,0,100,100]".
	if got := describeDock(config.Dock{Values: []float64{0, 0, 50, 100}}); got != "[0,0,50,100]" {
		t.Errorf("got %q, want %q", got, "[0,0,50,100]")
	}
}

func TestDescribeRuleReadsAsOneLine(t *testing.T) {
	r := config.Rule{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 50}},
	}
	got := describeRule(r)
	for _, want := range []string{"left edge", "0% to 50%", "[0,0,50,100]", "monitor 0"} {
		if !strings.Contains(got, want) {
			t.Errorf("describeRule = %q, missing %q", got, want)
		}
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("describeRule must stay on one line, got %q", got)
	}
}

// TestDescribeRuleNamesBothMonitors covers a rule whose trigger and dock are on
// different screens, which the single-monitor form would hide.
func TestDescribeRuleNamesBothMonitors(t *testing.T) {
	r := config.Rule{
		Dock:    config.Dock{Monitor: 1, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 50}},
	}
	if got := describeRule(r); !strings.Contains(got, "monitor 0 to 1") {
		t.Errorf("describeRule = %q, want it to name both monitors", got)
	}
}

func TestDescribeDockFlagsInvalidValues(t *testing.T) {
	for _, vals := range [][]float64{nil, {1, 2}, {1, 2, 3}} {
		if got := describeDock(config.Dock{Values: vals}); got != "(invalid)" {
			t.Errorf("dock with %d values described as %q, want (invalid)", len(vals), got)
		}
	}
}

func TestPrettyPosCoversEveryPosition(t *testing.T) {
	all := []string{
		config.PosLeft, config.PosRight, config.PosTop, config.PosBottom,
		config.PosTopLeft, config.PosTopRight, config.PosBottomLeft, config.PosBottomRight,
	}
	for _, p := range all {
		got := prettyPos(p)
		if got == "" {
			t.Errorf("%q has no readable form", p)
		}
		if strings.Contains(got, "_") {
			t.Errorf("%q rendered as %q; the underscore should not reach the UI", p, got)
		}
	}
}

// TestSortedPositionsMatchTriggerTypes guards the rule dialog: offering an edge
// position for a corner trigger would write a rule the engine then skips.
func TestSortedPositionsMatchTriggerTypes(t *testing.T) {
	corners := sortedPositions(config.TypeCorner)
	if len(corners) != 4 {
		t.Fatalf("got %d corner positions, want 4", len(corners))
	}
	for _, p := range corners {
		r := config.Rule{
			Dock:    config.Dock{Values: []float64{0, 0, 50, 50}},
			Trigger: config.Trigger{Type: config.TypeCorner, Pos: p},
		}
		if err := r.Validate(); err != nil {
			t.Errorf("corner position %q does not validate: %v", p, err)
		}
	}

	edges := sortedPositions(config.TypeEdge)
	if len(edges) != 4 {
		t.Fatalf("got %d edge positions, want 4", len(edges))
	}
	for _, p := range edges {
		r := config.Rule{
			Dock:    config.Dock{Values: []float64{0, 0, 50, 50}},
			Trigger: config.Trigger{Type: config.TypeEdge, Pos: p, Values: []float64{0, 100}},
		}
		if err := r.Validate(); err != nil {
			t.Errorf("edge position %q does not validate: %v", p, err)
		}
	}

	// The two sets must not overlap, or the dialog could carry a selection
	// across a type change and produce an unusable rule.
	for _, c := range corners {
		for _, e := range edges {
			if c == e {
				t.Errorf("%q appears as both a corner and an edge position", c)
			}
		}
	}
}

func TestDefaultRuleIsValid(t *testing.T) {
	if err := defaultRule().Validate(); err != nil {
		t.Errorf("the rule offered by the Add button is not usable: %v", err)
	}
}

func TestOrDefaultOnlyReplacesZero(t *testing.T) {
	if got := orDefault(0, 100); got != 100 {
		t.Errorf("orDefault(0, 100) = %v, want 100", got)
	}
	if got := orDefault(25, 100); got != 25 {
		t.Errorf("orDefault(25, 100) = %v, want 25", got)
	}
}

func TestMaxIntClampsNegativeComboIndex(t *testing.T) {
	// A walk ComboBox reports -1 when nothing is selected; indexing a slice
	// with that would panic.
	if got := maxInt(-1, 0); got != 0 {
		t.Errorf("maxInt(-1, 0) = %d, want 0", got)
	}
	if got := maxInt(3, 0); got != 3 {
		t.Errorf("maxInt(3, 0) = %d, want 3", got)
	}
}

func TestBoolPtrReturnsDistinctPointers(t *testing.T) {
	// The General checkboxes each store through boolPtr; sharing one address
	// would make every optional setting take the last value written.
	a, b := boolPtr(true), boolPtr(false)
	if a == b {
		t.Fatal("boolPtr returned the same address twice")
	}
	if !*a || *b {
		t.Errorf("boolPtr lost the values: got %v and %v", *a, *b)
	}
}

// --- the program icon ------------------------------------------------------

// 16x16 is the size Windows actually shows, so every size has to draw.
func TestIconRendersAtEverySize(t *testing.T) {
	for _, size := range []int{16, 20, 24, 32, 48, 64, 128, 256} {
		for _, enabled := range []bool{true, false} {
			img := drawIcon(size, enabled)
			b := img.Bounds()
			if b.Dx() != size || b.Dy() != size {
				t.Errorf("at %d: canvas is %dx%d", size, b.Dx(), b.Dy())
			}
			if countOpaque(img) == 0 {
				t.Errorf("at %d (enabled=%v) drew nothing", size, enabled)
			}
		}
	}
}

func countOpaque(img image.Image) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				n++
			}
		}
	}
	return n
}

// Guards against an icon that draws something but is a speck at tray size.
func TestIconFillsEnoughOfTheCanvas(t *testing.T) {
	covered := countOpaque(drawIcon(16, true))
	if covered < 16*16/4 {
		t.Errorf("the icon covers only %d of 256 pixels at 16x16", covered)
	}
}

// The disabled icon is the tray's only signal that docking is off.
func TestDisabledIconIsColourless(t *testing.T) {
	img := drawIcon(64, false)
	bnds := img.Bounds()
	for y := bnds.Min.Y; y < bnds.Max.Y; y++ {
		for x := bnds.Min.X; x < bnds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if r != g || g != b {
				t.Fatalf("coloured pixel at (%d,%d) when disabled: %d,%d,%d",
					x, y, r>>8, g>>8, b>>8)
			}
		}
	}
}
