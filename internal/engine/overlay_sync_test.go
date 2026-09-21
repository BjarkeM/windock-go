package engine

import (
	"io"
	"log/slog"
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/win"
)

// These cover the disabled direction only. Switching the preview back on
// creates a real layered window, which a CI runner has no desktop for.

func testEngine(t *testing.T, showPreview bool) *Engine {
	t.Helper()
	cfg := &config.File{GlobalSettings: config.Global{ShowPreview: &showPreview}}
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), "", cfg)
}

// TestSyncOverlayDropsThePreviewWhenTurnedOff is the regression: the setting
// used to be read once at startup, so unchecking "Show zone preview" left the
// overlay up until the process restarted.
func TestSyncOverlayDropsThePreviewWhenTurnedOff(t *testing.T) {
	e := testEngine(t, true)
	// Stand in for a window the engine created before the setting changed.
	// hwnd stays 0, so destroy touches nothing.
	e.overlay = &overlay{visible: true}

	off := false
	e.cfg.GlobalSettings.ShowPreview = &off
	e.syncOverlay()

	if e.overlay != nil {
		t.Error("the overlay survived the setting being turned off")
	}
}

// With the preview already off, a reload must not build one.
func TestSyncOverlayStaysNilWhileDisabled(t *testing.T) {
	e := testEngine(t, false)
	e.syncOverlay()
	if e.overlay != nil {
		t.Error("syncOverlay created a preview window with the setting off")
	}
	// A second pass must be just as quiet.
	e.syncOverlay()
	if e.overlay != nil {
		t.Error("a repeat call created one")
	}
}

// show and hide are called from the hook whatever the setting says, so they
// have to tolerate the overlay being gone.
func TestOverlayCallsAreSafeWhenDisabled(t *testing.T) {
	var o *overlay
	o.show(win.RECT{Right: 100, Bottom: 100}, 0)
	o.hide()
	o.setAlpha(120)
	o.destroy()
}
