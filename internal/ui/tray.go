// The ui package is the notification-area interface: a tray icon with a menu, and a
// settings window.
//
// The engine and the UI each own a thread with its own message loop - hooks are
// bound to the installing thread and walk expects the main one - and the two
// talk by posting messages rather than sharing state.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"

	"github.com/BjarkeM/windock-go/internal/autostart"
	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/engine"
	"github.com/BjarkeM/windock-go/internal/ipc"
)

// App is the tray application.
type App struct {
	log     *slog.Logger
	cfgPath string

	mu  sync.Mutex
	cfg *config.File

	window *walk.MainWindow
	icon   *walk.NotifyIcon

	enabledAction   *walk.Action
	autostartAction *walk.Action
	profileMenu     *walk.Menu

	eng     *engine.Engine
	stopEng context.CancelFunc
	engDone chan struct{}

	settings      *SettingsWindow
	closing       bool
	engineStopped bool
}

// Run shows the tray icon, starts the engine and pumps messages until the user
// exits. It must be called on the main goroutine. openSettings opens the editor
// straight away, which is what "windock settings" does.
func Run(log *slog.Logger, cfgPath string, cfg *config.File, openSettings bool) error {
	a := &App{log: log, cfgPath: cfgPath, cfg: cfg}

	// walk needs a window to own the notification icon, but this one is never
	// shown; the settings window is created separately and on demand.
	w, err := walk.NewMainWindow()
	if err != nil {
		return fmt.Errorf("creating the tray host window: %w", err)
	}
	a.window = w
	defer w.Dispose()
	w.Closing().Attach(a.onClosing)

	// An installer asks the running copy to go before it replaces the
	// executable. Close is what the Exit menu item does, so this shuts down the
	// same way rather than losing a staged settings edit to a kill.
	listener, err := ipc.Listen(func() {
		w.Synchronize(func() { w.Close() })
	})
	if err != nil {
		log.Warn("could not listen for an exit request", "error", err)
	} else {
		defer listener.Close()
	}

	ni, err := walk.NewNotifyIcon(w)
	if err != nil {
		return fmt.Errorf("creating the tray icon: %w", err)
	}
	defer ni.Dispose()
	a.icon = ni

	if err := a.buildMenu(); err != nil {
		return err
	}
	a.refreshIcon()

	// A double-click on the tray icon opens the settings, matching the
	// original, whose tray handler was named notifyIcon_windock_DoubleClick.
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.ShowSettings()
		}
	})

	if err := ni.SetVisible(true); err != nil {
		return err
	}

	a.startEngine()
	defer a.stopEngine()

	if openSettings {
		a.ShowSettings()
	}

	a.window.Run()
	return nil
}

func (a *App) buildMenu() error {
	ni := a.icon

	enabled := walk.NewAction()
	if err := enabled.SetText("&Docking enabled"); err != nil {
		return err
	}
	enabled.SetCheckable(true)
	enabled.Triggered().Attach(func() { a.setDockingEnabled(enabled.Checked()) })
	a.enabledAction = enabled
	if err := ni.ContextMenu().Actions().Add(enabled); err != nil {
		return err
	}

	if err := ni.ContextMenu().Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}

	// Profiles are rebuilt whenever the configuration changes, so they live in
	// a submenu this code owns entirely.
	profiles, err := walk.NewMenu()
	if err != nil {
		return err
	}
	profilesAction, err := ni.ContextMenu().Actions().AddMenu(profiles)
	if err != nil {
		return err
	}
	if err := profilesAction.SetText("&Profiles"); err != nil {
		return err
	}
	a.profileMenu = profiles

	if err := ni.ContextMenu().Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}

	settings := walk.NewAction()
	if err := settings.SetText("&Settings..."); err != nil {
		return err
	}
	settings.Triggered().Attach(a.ShowSettings)
	if err := ni.ContextMenu().Actions().Add(settings); err != nil {
		return err
	}

	auto := walk.NewAction()
	if err := auto.SetText("&Run at logon"); err != nil {
		return err
	}
	auto.SetCheckable(true)
	auto.Triggered().Attach(func() { a.setAutostart(auto.Checked()) })
	a.autostartAction = auto
	if err := ni.ContextMenu().Actions().Add(auto); err != nil {
		return err
	}

	about := walk.NewAction()
	if err := about.SetText("&About"); err != nil {
		return err
	}
	about.Triggered().Attach(a.showAbout)
	if err := ni.ContextMenu().Actions().Add(about); err != nil {
		return err
	}

	if err := ni.ContextMenu().Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}

	exit := walk.NewAction()
	if err := exit.SetText("E&xit"); err != nil {
		return err
	}
	exit.Triggered().Attach(func() { a.window.Close() })
	if err := ni.ContextMenu().Actions().Add(exit); err != nil {
		return err
	}

	a.syncMenu()
	return nil
}

// syncMenu brings the menu into line with the configuration on disk.
func (a *App) syncMenu() {
	cfg := a.Config()

	a.enabledAction.SetChecked(cfg.GlobalSettings.DockingEnabled)

	on, _, err := autostart.Enabled()
	if err == nil {
		a.autostartAction.SetChecked(on)
	}

	a.rebuildProfileMenu()
	a.refreshIcon()
	a.updateToolTip()
}

func (a *App) updateToolTip() {
	cfg := a.Config()
	name := "<no profile>"
	if p := cfg.ActiveProfileOrNil(); p != nil {
		name = p.Name
	}
	state := "on"
	if !cfg.GlobalSettings.DockingEnabled {
		state = "off"
	}
	// The tray tooltip is limited to 127 characters.
	a.icon.SetToolTip(fmt.Sprintf("WinDock - %s (docking %s)", name, state))
}

// refreshIcon puts the icon on the tray and on every window the program owns.
func (a *App) refreshIcon() {
	cfg := a.Config()
	icon := appIcon(cfg.GlobalSettings.DockingEnabled)

	if err := a.icon.SetIcon(icon); err != nil {
		a.log.Warn("could not set the tray icon", "error", err)
	}
	// A greyed-out title bar would read as an inactive window, not as docking
	// being off, so windows always get the lit icon.
	live := appIcon(true)
	if a.window != nil {
		a.window.SetIcon(live)
	}
	if a.settings != nil && a.settings.Form != nil {
		a.settings.Form.SetIcon(live)
	}
}

// windowIcon is what dialogs and windows wear, whatever the docking state.
func (a *App) windowIcon() walk.Image {
	return appIcon(true)
}

// Config returns the current configuration. Callers must not mutate it; use
// Update instead.
func (a *App) Config() *config.File {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// Update applies a change, saves it and tells the engine to pick it up. Every
// mutation goes through here so neither step can be forgotten.
func (a *App) Update(mutate func(*config.File)) error {
	a.mu.Lock()
	mutate(a.cfg)
	cfg := a.cfg
	a.mu.Unlock()

	if err := config.Save(a.cfgPath, cfg); err != nil {
		return err
	}
	if a.eng != nil {
		a.eng.Reload()
	}
	a.syncMenu()
	return nil
}

// Commit replaces the live configuration with an edited copy, saves it and
// hands it to the engine. The settings window calls this on Apply and OK.
func (a *App) Commit(draft *config.File) error {
	next := draft.Clone()

	if err := config.Save(a.cfgPath, next); err != nil {
		return err
	}

	a.mu.Lock()
	a.cfg = next
	a.mu.Unlock()

	if a.eng != nil {
		a.eng.Reload()
	}
	a.syncMenu()
	return nil
}

func (a *App) setDockingEnabled(on bool) {
	if err := a.Update(func(c *config.File) {
		c.GlobalSettings.DockingEnabled = on
	}); err != nil {
		a.errorBox("Could not save the setting", err)
	}
}

func (a *App) setAutostart(on bool) {
	if err := autostart.Set(on); err != nil {
		a.errorBox("Could not change the logon setting", err)
		// Put the tick back where it was.
		if actual, _, err := autostart.Enabled(); err == nil {
			a.autostartAction.SetChecked(actual)
		}
	}
}

// SetActiveProfile switches profile from the tray menu. Menu actions apply
// straight away; only the settings window stages its edits.
func (a *App) SetActiveProfile(i int) {
	if err := a.Update(func(c *config.File) {
		if i >= 0 && i < len(c.Profiles) {
			c.GlobalSettings.ActiveProfile = i
		}
	}); err != nil {
		a.errorBox("Could not switch profile", err)
	}
}

func (a *App) errorBox(title string, err error) {
	a.log.Error(title, "error", err)
	owner := walk.Form(a.window)
	if a.settings != nil && a.settings.Form != nil {
		owner = a.settings.Form
	}
	walk.MsgBox(owner, "WinDock", title+":\n\n"+err.Error(), walk.MsgBoxIconError)
}

// showAbout is a dialog rather than a message box so the icon can be shown and
// the configuration path selected and copied.
func (a *App) showAbout() {
	owner := walk.Form(a.window)
	if a.settings != nil && a.settings.Form != nil {
		owner = a.settings.Form
	}
	cfg := a.Config()

	var (
		dlg   *walk.Dialog
		close *walk.PushButton
	)
	err := d.Dialog{
		AssignTo:      &dlg,
		Title:         "About WinDock-Go",
		Icon:          a.windowIcon(),
		MinSize:       d.Size{Width: 430, Height: 0},
		DefaultButton: &close,
		CancelButton:  &close,
		Layout:        d.VBox{},
		Children: []d.Widget{
			d.Composite{
				Layout: d.HBox{MarginsZero: true, Spacing: 14},
				Children: []d.Widget{
					d.ImageView{
						Image:   appIconSized(true, 48),
						MinSize: d.Size{Width: 48, Height: 48},
						MaxSize: d.Size{Width: 48, Height: 48},
					},
					d.Composite{
						Layout: d.VBox{MarginsZero: true},
						Children: []d.Widget{
							d.Label{Text: "WinDock-Go", Font: d.Font{Family: "Segoe UI", PointSize: 12, Bold: true}},
							d.Label{Text: "A Go reimplementation of WinDock (Ivan Yu)."},
							d.VSpacer{Size: 6},
							d.Label{Text: "It works out of process, so sandboxed windows are visible to it,"},
							d.Label{Text: "and it re-reads the monitor layout on every change, so an RDP"},
							d.Label{Text: "session cannot leave it snapping to screens that moved."},
							d.VSpacer{},
						},
					},
				},
			},
			d.Composite{
				Layout: d.Grid{Columns: 2, MarginsZero: true},
				Children: []d.Widget{
					d.Label{Text: "Profiles:"},
					d.Label{Text: fmt.Sprintf("%d", len(cfg.Profiles))},
					d.Label{Text: "Configuration:"},
					d.LineEdit{Text: a.cfgPath, ReadOnly: true},
				},
			},
			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.HSpacer{},
					d.PushButton{AssignTo: &close, Text: "Close", MaxSize: d.Size{Width: 90},
						OnClicked: func() { dlg.Accept() }},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		a.log.Warn("could not open the About dialog", "error", err)
		return
	}
	dlg.Run()
}

func (a *App) onClosing(canceled *bool, _ walk.CloseReason) {
	if !a.engineStopped && a.stopEng != nil {
		*canceled = true
		a.requestExit()
	}
}

// requestExit keeps the UI pumping while the engine restores Windows settings.
// SystemParametersInfo broadcasts synchronously to this thread's windows, so
// waiting for engDone after closing the host can deadlock (especially with the
// settings window open). All close paths go through the host's Closing handler.
func (a *App) requestExit() {
	if a.closing {
		return
	}
	a.closing = true
	a.stopEng()
	go func() {
		<-a.engDone
		a.window.Synchronize(func() {
			a.engineStopped = true
			if a.settings != nil && a.settings.Form != nil {
				a.settings.Form.Dispose()
			}
			a.window.Close()
		})
	}()
}

// --- engine lifecycle -----------------------------------------------------

// startEngine runs the engine on its own OS thread: its hooks are bound to the
// installing thread, which must keep pumping messages.
func (a *App) startEngine() {
	ctx, cancel := context.WithCancel(context.Background())
	a.stopEng = cancel
	a.engDone = make(chan struct{})

	eng := engine.New(a.log, a.cfgPath, a.Config())
	a.eng = eng

	go func() {
		defer close(a.engDone)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := eng.Run(ctx); err != nil {
			a.log.Error("engine stopped", "error", err)
		}
	}()
}

func (a *App) stopEngine() {
	if a.stopEng == nil {
		return
	}
	a.stopEng()
	<-a.engDone
}

// rebuildProfileMenu repopulates the profile submenu from scratch.
func (a *App) rebuildProfileMenu() {
	actions := a.profileMenu.Actions()
	for actions.Len() > 0 {
		actions.RemoveAt(0)
	}

	cfg := a.Config()
	for i, p := range cfg.Profiles {
		i, name := i, p.Name
		act := walk.NewAction()
		if err := act.SetText(fmt.Sprintf("%s	%d rules", name, len(p.Rules))); err != nil {
			continue
		}
		act.SetCheckable(true)
		act.SetChecked(i == cfg.GlobalSettings.ActiveProfile)
		act.Triggered().Attach(func() { a.SetActiveProfile(i) })
		actions.Add(act)
	}

	if len(cfg.Profiles) == 0 {
		act := walk.NewAction()
		act.SetText("(no profiles)")
		act.SetEnabled(false)
		actions.Add(act)
	}
}

// configDir is used by the file dialogs as a sensible starting point.
func (a *App) configDir() string {
	d := filepath.Dir(a.cfgPath)
	if _, err := os.Stat(d); err != nil {
		return ""
	}
	return d
}
