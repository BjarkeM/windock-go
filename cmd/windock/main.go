package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/engine"
	"github.com/BjarkeM/windock-go/internal/ipc"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

func main() {
	var (
		cfgPath = flag.String("config", "", "path to profile.json (default: %LOCALAPPDATA%\\WinDock-Go\\profile.json)")
		verbose = flag.Bool("v", false, "verbose logging")
	)
	flag.Usage = usage
	flag.Parse()

	log := newLogger(*verbose)

	path, err := resolveConfigPath(*cfgPath)
	if err != nil {
		fatal(log, err)
	}

	cmd := "tray"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}

	var runErr error
	switch cmd {
	case "tray":
		runErr = cmdTray(log, path, false)
	case "settings":
		runErr = cmdTray(log, path, true)
	case "run":
		runErr = cmdRun(log, path)
	case "autostart":
		runErr = cmdAutostart(flag.Args()[1:])
	case "diag":
		runErr = cmdDiag(path)
	case "exit":
		runErr = cmdExit()
	case "probe":
		runErr = cmdProbe(path)
	case "profiles":
		runErr = cmdProfiles(path)
	case "use":
		runErr = cmdUse(path, flag.Args()[1:])
	case "import":
		runErr = cmdImport(path, flag.Args()[1:])
	case "export":
		runErr = cmdExport(path, flag.Args()[1:])
	case "enable":
		runErr = cmdSetDocking(path, true)
	case "disable":
		runErr = cmdSetDocking(path, false)
	case "where":
		fmt.Println(path)
	case "help", "-h", "--help":
		usage()
	default:
		runErr = fmt.Errorf("unknown command %q (try: windock help)", cmd)
	}
	if runErr != nil {
		fatal(log, runErr)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `windock - zone snapping for Windows

Usage:
  windock [flags] [command]

Commands:
  tray           Run with a notification-area icon and settings window (default)
  settings       Same as tray, with the settings window already open
  run            Watch for window drags and snap them, without any UI
  diag           Print the current monitors and how each rule resolves
  probe          Follow the cursor live and report which trigger it is inside
  exit           Ask a running copy to shut down
  profiles       List the profiles in the configuration
  use <n|name>   Make a profile active
  import <file>  Add a profile from an exported .json file
  export <n> <file>
                 Write one profile to a file
  enable         Turn docking on
  disable        Turn docking off
  autostart [on|elevated|off]
                 Show or change whether windock starts at logon.
                 "elevated" registers a scheduled task that starts it with
                 administrator rights, which it needs to move windows
                 belonging to elevated programs.
  where          Print the configuration file path

Flags:
  -config path   Use a specific profile.json
  -v             Verbose logging

The configuration format is the same as the original WinDock, so existing
profiles and exported profile files load unchanged.
`)
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func fatal(log *slog.Logger, err error) {
	log.Error(err.Error())
	os.Exit(1)
}

func resolveConfigPath(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	return config.DefaultPath()
}

// loadConfig is the one loader every subcommand uses: the file, then the
// original WinDock's, then built-in defaults.
func loadConfig(path string) (*config.File, string, error) {
	cfg, source, err := config.LoadOrInit(path)
	if err != nil {
		return nil, "", fmt.Errorf("loading %s: %w", path, err)
	}
	return cfg, source, nil
}

// noteSource explains where the settings in play came from when they did not
// come from the configuration file itself.
func noteSource(path, source string) {
	switch {
	case source == "config":
		// Loaded from the configuration file itself; nothing to explain.
	case source == "defaults":
		fmt.Printf("Note:     no configuration yet; showing built-in defaults.\n")
		fmt.Printf("          It will be created at %s on first run.\n", path)
	default:
		fmt.Printf("Note:     no configuration yet; %s.\n", source)
		fmt.Printf("          It will be written to %s on first run.\n", path)
	}
}

// cmdRun is the main mode: install the hooks and pump messages.
func cmdRun(log *slog.Logger, path string) error {
	// The hooks and the windows they feed are bound to one thread, so the
	// message loop must stay on it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	release, err := claimSingleInstance()
	if err != nil {
		return err
	}
	defer release()

	// Per-monitor DPI awareness must be set before any window is created, or
	// Windows virtualises the coordinates we are trying to compute with.
	if !win.SetProcessDpiAwarenessContextV2() {
		log.Warn("per-monitor DPI awareness unavailable; zones may be off on mixed-DPI setups")
	}

	cfg, source, err := loadConfig(path)
	if err != nil {
		return err
	}
	log.Info("configuration loaded", "path", path, "source", source)

	if cfg.IsExportedProfile() {
		log.Info("this file is a single exported profile, not a configuration; "+
			"using it as a one-profile configuration",
			"profile", profileName(cfg),
			"hint", "run 'windock import "+filepath.Base(path)+"' to add it to your real configuration")
	}

	if source != "config" && !cfg.IsExportedProfile() {
		if err := config.Save(path, cfg); err != nil {
			log.Warn("could not write the initial configuration", "path", path, "error", err)
		} else {
			log.Info("wrote initial configuration", "path", path)
		}
	}
	if !cfg.GlobalSettings.DockingEnabled {
		log.Warn("docking is disabled in the configuration; run 'windock enable' to turn it on")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// "windock exit" has to reach this mode too: it holds the single-instance
	// lock, so an installer would otherwise wait out the grace period on it.
	listener, err := ipc.Listen(stop)
	if err != nil {
		log.Warn("could not listen for an exit request", "error", err)
	} else {
		defer listener.Close()
	}

	return engine.New(log, path, cfg).Run(ctx)
}

// claimSingleInstance stops a second copy from fighting the first over window
// placement. It returns a release function.
func claimSingleInstance() (func(), error) {
	name, err := windows.UTF16PtrFromString(ipc.InstanceMutex)
	if err != nil {
		return nil, err
	}
	sa, err := ipc.UserSecurityAttributes()
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(sa, false, name)
	if err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			if h != 0 {
				windows.CloseHandle(h)
			}
			return nil, errors.New("windock is already running")
		}
		return nil, fmt.Errorf("single-instance check failed: %w", err)
	}
	return func() { windows.CloseHandle(h) }, nil
}

// cmdDiag prints the live topology and how every rule resolves against it.
func cmdDiag(path string) error {
	if !win.SetProcessDpiAwarenessContextV2() {
		fmt.Println("note: per-monitor DPI awareness unavailable; coordinates may be virtualised")
	}
	cfg, source, err := loadConfig(path)
	if err != nil {
		return err
	}

	mons := monitor.Enumerate()
	fmt.Printf("Elevated: %v\n", windows.GetCurrentProcessToken().IsElevated())
	fmt.Printf("Config:   %s\n", path)
	fmt.Printf("Docking:  %v\n", cfg.GlobalSettings.DockingEnabled)
	fmt.Printf("Profile:  %s\n", profileName(cfg))
	if cfg.IsExportedProfile() {
		fmt.Println("Note:     this file is a single exported profile, not a configuration file")
	}
	noteSource(path, source)
	fmt.Println()

	fmt.Printf("Monitors (%d), in the stable order rules refer to:\n", mons.Len())
	for _, m := range mons.All() {
		fmt.Printf("  %s\n", m)
		fmt.Printf("      bounds   %s\n", rectStr(m.Bounds))
		fmt.Printf("      workarea %s\n", rectStr(m.WorkArea))
	}
	if mons.Len() == 0 {
		fmt.Println("  (none detected)")
	}
	fmt.Println()

	rules := cfg.ActiveRules()
	layout := zones.Resolve(rules, mons, cfg.GlobalSettings)

	resolved := make(map[int]zones.Zone, layout.Len())
	for _, z := range layout.Zones() {
		resolved[z.RuleIndex] = z
	}

	fmt.Printf("Rules (%d active, %d inactive):\n", layout.Len(), layout.Skipped)
	for i, r := range rules {
		desc := describeRule(r)
		z, ok := resolved[i]
		if !ok {
			reason := "monitor not attached"
			if err := r.Validate(); err != nil {
				reason = err.Error()
			}
			fmt.Printf("  [%2d] %-44s INACTIVE (%s)\n", i, desc, reason)
			continue
		}
		fmt.Printf("  [%2d] %-44s\n", i, desc)
		fmt.Printf("        trigger %s\n", rectStr(z.Hit))
		fmt.Printf("        dock    %s\n", rectStr(z.Dock))
	}
	if len(rules) == 0 {
		fmt.Println("  (the active profile has no rules)")
	}

	fmt.Println()
	fmt.Println("Match order (first hit wins; corners before edges before areas):")
	for n, z := range layout.Zones() {
		fmt.Printf("  %d. rule [%d] %s\n", n+1, z.RuleIndex, describeRule(rules[z.RuleIndex]))
	}
	return nil
}

func describeRule(r config.Rule) string {
	var t string
	switch r.Trigger.Type {
	case config.TypeCorner:
		t = fmt.Sprintf("corner %s", r.Trigger.Pos)
	case config.TypeEdge:
		if len(r.Trigger.Values) == 2 {
			t = fmt.Sprintf("edge %s %g%%-%g%%", r.Trigger.Pos, r.Trigger.Values[0], r.Trigger.Values[1])
		} else {
			t = fmt.Sprintf("edge %s (bad values)", r.Trigger.Pos)
		}
	case config.TypeArea:
		t = "area"
	default:
		t = "unknown trigger " + r.Trigger.Type
	}
	d := "bad dock"
	if len(r.Dock.Values) == 4 {
		v := r.Dock.Values
		d = fmt.Sprintf("%g,%g -> %g,%g", v[0], v[1], v[2], v[3])
		if !r.Dock.WorkArea() {
			d += " full"
		}
	}
	return fmt.Sprintf("mon%d %s => mon%d %s", r.Trigger.Monitor, t, r.Dock.Monitor, d)
}

func rectStr(r win.RECT) string {
	return fmt.Sprintf("(%d,%d)-(%d,%d)  %dx%d", r.Left, r.Top, r.Right, r.Bottom, r.Width(), r.Height())
}

func profileName(cfg *config.File) string {
	if p := cfg.ActiveProfileOrNil(); p != nil {
		return fmt.Sprintf("%s (index %d)", p.Name, cfg.GlobalSettings.ActiveProfile)
	}
	return "<none>"
}

func cmdProfiles(path string) error {
	cfg, source, err := loadConfig(path)
	if err != nil {
		return err
	}
	noteSource(path, source)
	for i, p := range cfg.Profiles {
		marker := " "
		if i == cfg.GlobalSettings.ActiveProfile {
			marker = "*"
		}
		fmt.Printf(" %s [%d] %-28s %d rules\n", marker, i, p.Name, len(p.Rules))
	}
	if len(cfg.Profiles) == 0 {
		fmt.Println("  (no profiles)")
	}
	return nil
}

func cmdUse(path string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: windock use <index|name>")
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		return err
	}

	idx := -1
	if n, err := strconv.Atoi(args[0]); err == nil {
		idx = n
	} else {
		for i, p := range cfg.Profiles {
			if strings.EqualFold(p.Name, args[0]) {
				idx = i
				break
			}
		}
	}
	if idx < 0 || idx >= len(cfg.Profiles) {
		return fmt.Errorf("no such profile: %s", args[0])
	}
	cfg.GlobalSettings.ActiveProfile = idx
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Printf("active profile is now [%d] %s\n", idx, cfg.Profiles[idx].Name)
	return nil
}

func cmdImport(path string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: windock import <file.json>")
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		return err
	}
	p, err := config.LoadProfile(args[0])
	if err != nil {
		return err
	}
	cfg.Profiles = append(cfg.Profiles, *p)
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Printf("imported %q as profile [%d] with %d rules\n", p.Name, len(cfg.Profiles)-1, len(p.Rules))
	return nil
}

func cmdExport(path string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: windock export <index|name> <file.json>")
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		return err
	}
	idx := -1
	if n, err := strconv.Atoi(args[0]); err == nil {
		idx = n
	} else {
		for i, p := range cfg.Profiles {
			if strings.EqualFold(p.Name, args[0]) {
				idx = i
				break
			}
		}
	}
	if idx < 0 || idx >= len(cfg.Profiles) {
		return fmt.Errorf("no such profile: %s", args[0])
	}
	if err := config.SaveProfile(args[1], &cfg.Profiles[idx]); err != nil {
		return err
	}
	fmt.Printf("exported %q to %s\n", cfg.Profiles[idx].Name, args[1])
	return nil
}

func cmdSetDocking(path string, on bool) error {
	cfg, _, err := loadConfig(path)
	if err != nil {
		return err
	}
	cfg.GlobalSettings.DockingEnabled = on
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	state := "disabled"
	if on {
		state = "enabled"
	}
	fmt.Printf("docking %s\n", state)
	return nil
}
