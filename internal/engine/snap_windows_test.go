package engine

import (
	"testing"

	"github.com/BjarkeM/windock-go/internal/win"
)

// What remains here needs no window: everything that created one depended on a
// desktop and DWM composition, neither of which a CI runner reliably has.

func TestSnappableRejectsInvalidHandles(t *testing.T) {
	if snappable(0) {
		t.Error("a null handle must not be snappable")
	}
	if snappable(win.HWND(0xdeadbeef)) {
		t.Error("a bogus handle must not be snappable")
	}
}

// TestAdjustForFrameFallsBackOnBadHandle makes sure a window we cannot query
// is placed at the raw zone rather than at some garbage offset.
func TestAdjustForFrameFallsBackOnBadHandle(t *testing.T) {
	zone := win.RECT{Left: 10, Top: 20, Right: 110, Bottom: 120}
	if got := adjustForFrame(win.HWND(0xdeadbeef), zone); got != zone {
		t.Errorf("unqueryable window should fall back to the raw zone, got %+v", got)
	}
}
