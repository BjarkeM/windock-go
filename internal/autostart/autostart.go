// The autostart package registers windock to start on user logon.
//
// A per-user startup entry rather than a service: a service runs in session 0,
// where SetWinEventHook observes nothing and SetWindowPos has no windows to
// move.
package autostart

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// runKey is the per-user Run key. HKCU rather than HKLM: no elevation needed,
// and the entry belongs to the user whose session the program manages.
const (
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName = "WinDock-Go"
)

// Command is the command line registered for logon.
func Command() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	// Not %q: that is Go quoting, which escapes the separators and yields
	// C:\\dir\\windock.exe. Windows collapses the doubled separators
	// when it launches, so it works, but the entry reads wrong and any
	// comparison against it is wrong too.
	return `"` + exe + `" tray`, nil
}

// Enabled reports whether windock starts at logon by either mechanism, and
// what that entry points at. The scheduled task wins when both exist, because
// it is the one that decides how windock starts.
func Enabled() (bool, string, error) {
	if on, cmd, err := ElevatedEnabled(); err == nil && on {
		return true, cmd, nil
	}
	return runEntry()
}

// runEntry reads the plain, non-elevated Run key entry.
func runEntry() (bool, string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, "", nil
		}
		return false, "", err
	}
	defer k.Close()

	v, _, err := k.GetStringValue(valueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, "", nil
		}
		return false, "", err
	}
	return true, v, nil
}

// Enable registers the current executable to start at logon, replacing any
// entry left behind by a copy that has since moved.
func Enable() error {
	cmd, err := Command()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(valueName, cmd)
}

// Disable removes the logon entry. Removing one that is not there is not an
// error, so callers can use this to guarantee a state.
func Disable() error {
	if err := disableRunEntry(); err != nil {
		return err
	}
	return DisableElevated()
}

func disableRunEntry() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer k.Close()

	if err := k.DeleteValue(valueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// Set applies the requested state. Turning it on when the elevated task is
// already registered leaves that alone: adding a Run entry beside it would
// start a second copy at logon, which the single-instance lock would then have
// to turn away.
func Set(on bool) error {
	if !on {
		return Disable()
	}
	if elevated, _, err := ElevatedEnabled(); err == nil && elevated {
		return nil
	}
	return Enable()
}

// IsStale reports whether the entry points somewhere other than the executable
// running now. Re-enabling fixes it.
func IsStale() (bool, error) {
	// Never refresh a competing Run entry when the elevated task exists.
	if elevated, _, err := ElevatedEnabled(); err != nil || elevated {
		return false, err
	}
	// The Run entry only. A stale task is repointed by reinstalling, which is
	// the only context that has the rights to rewrite it.
	on, registered, err := runEntry()
	if err != nil || !on {
		return false, err
	}
	want, err := Command()
	if err != nil {
		return false, err
	}
	return !strings.EqualFold(strings.TrimSpace(registered), strings.TrimSpace(want)), nil
}
