package engine

import (
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
)

// TestRebuildBeforeTheGuidesExist covers the gap in startup: the notification
// window is created before Run builds the guides, so a WM_DISPLAYCHANGE or
// WM_SETTINGCHANGE arriving in between reaches refreshDisplays with g still
// nil. That dereference took the whole process down - the engine panics on its
// own goroutine, which is fatal - and the crash looked like windock refusing to
// start.
func TestRebuildBeforeTheGuidesExist(t *testing.T) {
	var g *guides

	m := uw()
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
	}}
	l := layoutFor(t, rules, m)

	// Would panic before rebuild tolerated a nil receiver.
	g.rebuild(set, l, config.Global{}, assignSlots(set, l))
}
