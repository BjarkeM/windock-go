package autostart

import (
	"golang.org/x/sys/windows/registry"
	"strings"
	"testing"
)

// These tests read and restore the real registry entry, so they leave the
// user's configuration exactly as they found it.

func TestEnableDisableRoundTrip(t *testing.T) {
	wasOn, original, err := runEntry()
	if err != nil {
		t.Fatalf("reading the current state: %v", err)
	}
	t.Cleanup(func() {
		if wasOn {
			k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
			if err == nil {
				k.SetStringValue(valueName, original)
				k.Close()
			}
			return
		}
		disableRunEntry()
	})

	if err := Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	on, cmd, err := runEntry()
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Fatal("Enable did not register a logon entry")
	}
	// Not asserting the executable name: under "go test" the running binary is
	// the test harness, not windock.exe.
	if !strings.HasSuffix(cmd, `" tray`) {
		t.Errorf("registered command %q should start the tray, not a console mode", cmd)
	}
	// The path must be quoted: Program Files and the like contain spaces.
	if !strings.HasPrefix(cmd, `"`) {
		t.Errorf("registered command %q must quote the executable path", cmd)
	}

	if stale, err := IsStale(); err != nil {
		t.Fatal(err)
	} else if stale {
		t.Error("a freshly written entry should not be reported as stale")
	}

	if err := disableRunEntry(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if on, _, _ := runEntry(); on {
		t.Error("Disable left the logon entry in place")
	}
}

func TestDisableIsIdempotent(t *testing.T) {
	wasOn, original, err := runEntry()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasOn {
			k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
			if err == nil {
				k.SetStringValue(valueName, original)
				k.Close()
			}
		}
	})

	if err := disableRunEntry(); err != nil {
		t.Fatalf("first Disable: %v", err)
	}
	if err := disableRunEntry(); err != nil {
		t.Fatalf("Disable on an absent entry must not fail: %v", err)
	}
}

func TestCommandIsAbsoluteAndQuoted(t *testing.T) {
	cmd, err := Command()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cmd, `"`) || !strings.Contains(cmd, `" `) {
		t.Errorf("command %q should be a quoted path followed by arguments", cmd)
	}
	if strings.HasPrefix(cmd, `".\`) || strings.HasPrefix(cmd, `"..`) {
		t.Errorf("command %q must be absolute; a relative path would break at logon", cmd)
	}
}

// The logon entry is a Windows command line, not a Go string literal. %q would
// double every separator; Windows collapses them on launch, so the mistake is
// invisible until you read the entry or compare against it.
func TestCommandDoesNotEscapeSeparators(t *testing.T) {
	cmd, err := Command()
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if strings.Contains(cmd, `\\`) {
		t.Errorf("Command contains doubled separators: %s", cmd)
	}
	if !strings.HasPrefix(cmd, `"`) || !strings.HasSuffix(cmd, `" tray`) {
		t.Errorf("Command is not a quoted path followed by the subcommand: %s", cmd)
	}
}
