package engine

import (
	"io"
	"log/slog"
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

// These exercise the reveal-on-hit mode without a desktop: e.guides stays nil,
// which show() and setActive() already tolerate, so only the decision to
// reveal is under test.

func revealEngine(t *testing.T, onHit bool) *Engine {
	t.Helper()
	m := uw()
	set := monitor.NewSetForTest([]monitor.Monitor{m})
	rules := []config.Rule{{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
	}}
	cfg := &config.File{
		GlobalSettings: config.Global{
			DockingEnabled: true,
			GuidesOnHit:    &onHit,
		},
		Profiles: []config.Profile{{Name: "test", Rules: rules}},
	}
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)), "", cfg)
	e.mons = set
	e.layout = zones.Resolve(rules, set, cfg.GlobalSettings)
	e.slots = assignSlots(set, e.layout)
	e.drag = dragState{active: true, zoneIdx: -1}
	return e
}

// TestGuidesStayDownForADragThroughTheMiddle is the feature: a window dragged
// across open space must not light up the edges.
func TestGuidesStayDownForADragThroughTheMiddle(t *testing.T) {
	e := revealEngine(t, true)

	for x := int32(1500); x < 2500; x += 100 {
		e.updatePreview(win.POINT{X: x, Y: 700})
	}
	if e.drag.guidesUp {
		t.Error("the guides came up for a drag that never reached a trigger")
	}
}

// Coming near a trigger is no longer enough; only entering one reveals them,
// and they then stay up - which is what stops them flickering as the cursor
// wanders back out.
func TestHittingATriggerRevealsTheGuidesForTheRestOfTheDrag(t *testing.T) {
	e := revealEngine(t, true)

	e.updatePreview(win.POINT{X: 120, Y: 700})
	if e.drag.guidesUp {
		t.Fatal("the guides came up 120px short of the trigger, without hitting it")
	}

	e.updatePreview(win.POINT{X: 0, Y: 700})
	if !e.drag.guidesUp {
		t.Fatal("the guides did not come up with the cursor on the left edge")
	}

	e.updatePreview(win.POINT{X: 1700, Y: 700})
	if !e.drag.guidesUp {
		t.Error("the guides went down again when the cursor left the trigger; " +
			"one hit is meant to keep them up for the whole drag")
	}
}

// With the setting off, nothing waits for a hit: onMoveSizeStart shows the
// guides itself, and a move through the middle leaves them up.
func TestWithoutHitModeTheMiddleOfTheScreenChangesNothing(t *testing.T) {
	e := revealEngine(t, false)
	e.guides.show() // no-op without a desktop; mirrors onMoveSizeStart
	e.drag.guidesUp = true

	e.updatePreview(win.POINT{X: 1700, Y: 700})
	if !e.drag.guidesUp {
		t.Error("the guides were taken down mid-drag with hit mode off")
	}
}

// The preview and the snap target must be unaffected: holding the guides back
// is presentation only.
func TestHitModeStillSnaps(t *testing.T) {
	e := revealEngine(t, true)

	e.updatePreview(win.POINT{X: 0, Y: 700})
	if e.drag.zone == nil {
		t.Fatal("the cursor on the trigger selected no zone")
	}
	if !e.drag.guidesUp {
		t.Error("the cursor inside a trigger did not reveal the guides")
	}
}

// A drag that starts with the cursor already on a trigger reveals them at once,
// on the first update rather than after the first mouse move.
func TestADragStartingOnATriggerRevealsImmediately(t *testing.T) {
	e := revealEngine(t, true)
	e.updatePreview(win.POINT{X: 1, Y: 700})
	if !e.drag.guidesUp {
		t.Error("a drag begun on the trigger left the guides hidden")
	}
}

// The reveal must not depend on the zone having changed: a drag whose first
// update lands on the same zone index it already holds still brings them up.
func TestRevealDoesNotDependOnTheZoneChanging(t *testing.T) {
	e := revealEngine(t, true)
	e.updatePreview(win.POINT{X: 0, Y: 700})
	e.drag.guidesUp = false // as if the reveal had been missed

	e.updatePreview(win.POINT{X: 0, Y: 720}) // same zone, still inside it
	if !e.drag.guidesUp {
		t.Error("staying inside a trigger never revealed the guides")
	}
}
