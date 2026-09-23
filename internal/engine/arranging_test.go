package engine

import (
	"io"
	"log/slog"
	"testing"

	"github.com/BjarkeM/windock-go/internal/config"
)

// fakeArranging stands in for the real setting, so none of this touches the
// machine it runs on.
type fakeArranging struct {
	on       bool
	readable bool
	writable bool
	writes   int
}

func (f *fakeArranging) install(t *testing.T) {
	t.Helper()
	oldRead, oldWrite := readArranging, writeArranging
	t.Cleanup(func() { readArranging, writeArranging = oldRead, oldWrite })

	readArranging = func() (bool, bool) {
		if !f.readable {
			return false, false
		}
		return f.on, true
	}
	writeArranging = func(on bool) bool {
		if !f.writable {
			return false
		}
		f.writes++
		f.on = on
		return true
	}
}

func snapEngine(t *testing.T, docking bool) (*Engine, *fakeArranging) {
	t.Helper()
	f := &fakeArranging{on: true, readable: true, writable: true}
	f.install(t)
	cfg := &config.File{GlobalSettings: config.Global{DockingEnabled: docking}}
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), "", cfg), f
}

// The feature: docking on means Windows' own preview and snap are out of the
// way, and shutting down hands the setting back.
func TestDockingSuppressesWindowsSnapAndRestoresIt(t *testing.T) {
	e, f := snapEngine(t, true)

	e.syncWindowsSnap()
	if f.on {
		t.Fatal("docking was enabled but Windows' snapping was left on")
	}

	e.restoreWindowsSnap()
	if !f.on {
		t.Error("Windows' snapping was not restored on shutdown")
	}
}

// With docking off we are not drawing previews of our own, so the user's
// setting is none of our business.
func TestDockingDisabledLeavesWindowsSnapAlone(t *testing.T) {
	e, f := snapEngine(t, false)

	e.syncWindowsSnap()
	if !f.on || f.writes != 0 {
		t.Errorf("the setting was touched with docking disabled: on=%v writes=%d", f.on, f.writes)
	}
}

// Turning docking off mid-session hands the setting back without waiting for
// shutdown; turning it on again takes it away once more.
func TestTogglingDockingFollowsTheSetting(t *testing.T) {
	e, f := snapEngine(t, true)
	e.syncWindowsSnap()

	e.cfg.GlobalSettings.DockingEnabled = false
	e.syncWindowsSnap()
	if !f.on {
		t.Fatal("disabling docking did not restore Windows' snapping")
	}

	e.cfg.GlobalSettings.DockingEnabled = true
	e.syncWindowsSnap()
	if f.on {
		t.Error("re-enabling docking did not turn Windows' snapping off again")
	}
}

// Repeated reloads must not keep rewriting a setting that is already where we
// want it - reloadConfig calls this on every hand edit of profile.json.
func TestRepeatedSyncsWriteOnce(t *testing.T) {
	e, f := snapEngine(t, true)

	for i := 0; i < 5; i++ {
		e.syncWindowsSnap()
	}
	if f.writes != 1 {
		t.Errorf("five syncs wrote the setting %d times, want 1", f.writes)
	}
}

// A setting the user had already turned off is not ours: we must not hand it
// back turned on.
func TestAnAlreadyDisabledSettingIsNotRestoredOn(t *testing.T) {
	e, f := snapEngine(t, true)
	f.on = false

	e.syncWindowsSnap()
	e.restoreWindowsSnap()

	if f.on {
		t.Error("we turned on a setting the user had turned off themselves")
	}
	if f.writes != 0 {
		t.Errorf("a setting that was already off was written %d times", f.writes)
	}
}

// If the setting cannot be read, nothing is written and nothing is claimed as
// ours to restore.
func TestAnUnreadableSettingIsLeftAlone(t *testing.T) {
	e, f := snapEngine(t, true)
	f.readable = false

	e.syncWindowsSnap()
	if f.writes != 0 {
		t.Errorf("the setting was written %d times despite being unreadable", f.writes)
	}
	if e.winSnapSuppressed {
		t.Error("an unread setting was recorded as ours to restore")
	}
}

// A write that fails leaves us owing nothing, so shutdown does not turn on a
// setting we never managed to turn off.
func TestAFailedWriteIsNotRecordedAsOurs(t *testing.T) {
	e, f := snapEngine(t, true)
	f.writable = false

	e.syncWindowsSnap()
	if e.winSnapSuppressed {
		t.Fatal("a failed write was recorded as ours to restore")
	}

	f.writable = true
	e.restoreWindowsSnap()
	if f.writes != 0 {
		t.Errorf("shutdown wrote the setting %d times after a failed suppress", f.writes)
	}
}
