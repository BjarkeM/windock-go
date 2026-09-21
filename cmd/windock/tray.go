package main

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"github.com/BjarkeM/windock-go/internal/autostart"
	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/ui"
	"github.com/BjarkeM/windock-go/internal/win"
)

// cmdTray is the normal way to run windock (as a service, instead of a one-shot binary).
// Walk owns the message loop on this thread, so it must be the main one;
// the engine takes its own, because hooks are bound to the installing thread and it must keep pumping messages.
func cmdTray(log *slog.Logger, path string, openSettings bool) error {
	runtime.LockOSThread()

	release, err := claimSingleInstance()
	if err != nil {
		return err
	}
	defer release()

	if !win.SetProcessDpiAwarenessContextV2() {
		log.Warn("per-monitor DPI awareness unavailable; zones may be off on mixed-DPI setups")
	}

	cfg, source, err := loadConfig(path)
	if err != nil {
		return err
	}
	log.Info("configuration loaded", "path", path, "source", source)

	if cfg.IsExportedProfile() {
		return fmt.Errorf(
			"%s is a single exported profile, not a configuration; the settings "+
				"window would have nowhere to save changes. Run 'windock import %s' first",
			path, path)
	}
	if source != "config" {
		if err := config.Save(path, cfg); err != nil {
			log.Warn("could not write the initial configuration", "path", path, "error", err)
		} else {
			log.Info("wrote initial configuration", "path", path)
		}
	}

	// A logon entry left pointing at an older copy of the executable would
	// silently start the wrong binary next time.
	if stale, err := autostart.IsStale(); err == nil && stale {
		if err := autostart.Enable(); err == nil {
			log.Info("logon entry refreshed to point at this executable")
		}
	}

	// Only ours to close when launched from Explorer or the logon entry; from
	// a shell the console belongs to that shell.
	if win.OwnsItsConsole() {
		win.FreeConsole()
	}

	return ui.Run(log, path, cfg, openSettings)
}

// cmdAutostart manages the logon entry from the command line, for anyone who
// would rather not go through the tray menu.
func cmdAutostart(args []string) error {
	if len(args) == 0 {
		return printAutostart()
	}

	switch strings.ToLower(args[0]) {
	case "on", "enable", "true":
		if err := autostart.Set(true); err != nil {
			return err
		}
		cmd, _ := autostart.Command()
		fmt.Printf("windock will start at logon:\n  %s\n", cmd)
	case "elevated", "admin":
		if err := autostart.EnableElevated(); err != nil {
			return fmt.Errorf("%w\n\nRegistering the elevated logon entry needs administrator "+
				"rights; run this from an elevated prompt", err)
		}
		cmd, _ := autostart.Command()
		fmt.Printf("windock will start elevated at logon:\n  %s\n", cmd)
	case "off", "disable", "false":
		if err := autostart.Disable(); err != nil {
			return err
		}
		fmt.Println("windock will no longer start at logon")
	default:
		return errors.New("usage: windock autostart [on|elevated|off]")
	}
	return nil
}

// printAutostart reports which mechanism is registered.
// Apps running as admin require elevation to move/reposition.
func printAutostart() error {
	elevated, taskCmd, err := autostart.ElevatedEnabled()
	if err != nil {
		return err
	}
	if elevated {
		fmt.Printf("windock starts elevated at logon, from a scheduled task:\n  %s\n", taskCmd)
		return nil
	}

	on, cmd, err := autostart.Enabled()
	if err != nil {
		return err
	}
	if !on {
		fmt.Println("windock does not start at logon")
		return nil
	}

	fmt.Printf("windock starts at logon:\n  %s\n", cmd)
	fmt.Println()
	fmt.Println("This starts it without elevation, so it cannot move windows belonging to")
	fmt.Println("programs running as administrator. 'windock autostart elevated', from an")
	fmt.Println("elevated prompt, registers a scheduled task that can.")

	if stale, err := autostart.IsStale(); err == nil && stale {
		want, _ := autostart.Command()
		fmt.Printf("\nThis points somewhere other than the executable running now:\n  %s\n", want)
		fmt.Println("Run 'windock autostart on' to update it.")
	}
	return nil
}
