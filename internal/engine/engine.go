// The engine package watches for window drags, previews the zone under the cursor,
// and snaps the window on release.
//
// It runs out of process: SetWinEventHook with WINEVENT_OUTOFCONTEXT observes
// drags, SetWindowPos moves windows, nothing is injected.
//
// Threading: every callback here arrives on the single thread running the
// message loop, and background goroutines only PostMessage to it, so the fields
// below need no locking.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

const notifyClassName = "WinDockGoNotify"

// Private messages posted to the notification window.
const (
	wmRefreshDisplays = win.WM_APP + 1
	wmReloadConfig    = win.WM_APP + 2
	wmVerifySnap      = win.WM_APP + 3
	wmShutdown        = win.WM_APP + 4
)

// Class names that must never be docked: the taskbar, the desktop and its
// helpers. The original special-cased the taskbar too.
var excludedClasses = map[string]bool{
	"Shell_TrayWnd":              true,
	"Shell_SecondaryTrayWnd":     true,
	"Progman":                    true,
	"WorkerW":                    true,
	"Windows.UI.Core.CoreWindow": true,
	"ApplicationManager_Immersive_DesktopShellWindow": true,
	"ForegroundStaging":            true,
	"MultitaskingViewFrame":        true,
	"XamlExplorerHostIslandWindow": true,
}

// Engine owns the runtime state.
type Engine struct {
	log     *slog.Logger
	cfgPath string
	cfg     *config.File
	mons    *monitor.Set
	layout  *zones.Layout
	overlay *overlay
	guides  *guides
	slots   []int // palette slot per zone index
	notify  win.HWND
	evHook  win.HWINEVENTHOOK
	mouse   win.HHOOK

	// notifyHandle mirrors notify for Reload, which may be called from another
	// goroutine before or after the window exists.
	notifyHandle atomic.Uintptr

	drag dragState

	// lastCfgMod lets the mtime poller notice hand edits to profile.json.
	lastCfgMod time.Time
}

type dragState struct {
	active bool
	hwnd   win.HWND
	zone   *zones.Zone
	// zoneIdx mirrors zone as an index into the layout, which is what the
	// guide overlay needs in order to highlight one trigger.
	zoneIdx int
}

// current is the process-wide engine: the Win32 callbacks below are plain
// function pointers with no user-data parameter, so they reach it through this.
var current atomic.Pointer[Engine]

// Reload asks the engine to re-read its configuration. Safe from any goroutine:
// it posts to the engine's own thread rather than touching its state.
func (e *Engine) Reload() {
	if hwnd := win.HWND(e.notifyHandle.Load()); hwnd != 0 {
		win.PostMessage(hwnd, wmReloadConfig, 0, 0)
	}
}

// New builds an engine for the given config path.
func New(log *slog.Logger, cfgPath string, cfg *config.File) *Engine {
	return &Engine{log: log, cfgPath: cfgPath, cfg: cfg}
}

// Run installs the hooks and pumps messages until ctx is cancelled. It must be
// called on a thread locked with runtime.LockOSThread.
func (e *Engine) Run(ctx context.Context) error {
	if !current.CompareAndSwap(nil, e) {
		return fmt.Errorf("an engine is already running in this process")
	}
	defer current.Store(nil)

	if err := e.createNotifyWindow(); err != nil {
		return err
	}
	defer func() {
		e.notifyHandle.Store(0)
		win.WTSUnRegisterSessionNotification(e.notify)
		win.DestroyWindow(e.notify)
	}()

	// Subscribing to session changes is what the original never did. Without
	// it, an RDP disconnect silently invalidates all cached monitor geometry.
	if win.WTSRegisterSessionNotification(e.notify, win.NOTIFY_FOR_THIS_SESSION) {
		e.log.Debug("subscribed to session change notifications")
	} else {
		e.log.Warn("could not subscribe to session change notifications; " +
			"display geometry will still refresh on WM_DISPLAYCHANGE")
	}

	initPalette(e.cfg.GlobalSettings.PreviewFill(), e.cfg.GlobalSettings.CategoricalColors())
	defer freePalette()

	e.syncOverlay()
	// Wrapped, because a bare defer would bind whichever overlay exists now and
	// syncOverlay replaces it whenever the setting changes.
	defer func() { e.overlay.destroy() }()

	e.guides = newGuides()
	defer e.guides.destroy()

	e.refreshDisplays("startup")

	e.evHook = win.SetWinEventHook(
		win.EVENT_SYSTEM_MOVESIZESTART,
		win.EVENT_SYSTEM_MOVESIZEEND,
		winEventCallback,
		win.WINEVENT_OUTOFCONTEXT|win.WINEVENT_SKIPOWNPROCESS)
	if e.evHook == 0 {
		return fmt.Errorf("SetWinEventHook failed; cannot observe window drags")
	}
	defer win.UnhookWinEvent(e.evHook)
	defer e.stopMouseHook()

	stopWatch := e.watchConfig(ctx)
	defer stopWatch()

	go func() {
		<-ctx.Done()
		win.PostMessage(e.notify, wmShutdown, 0, 0)
	}()

	e.log.Info("engine running",
		"profile", e.activeProfileName(),
		"zones", e.layout.Len(),
		"monitors", e.mons.Len())

	var msg win.MSG
	for win.GetMessage(&msg) {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
	e.log.Info("engine stopped")
	return nil
}

func (e *Engine) activeProfileName() string {
	if p := e.cfg.ActiveProfileOrNil(); p != nil {
		return p.Name
	}
	return "<none>"
}

// createNotifyWindow makes the hidden top-level window that receives broadcast
// notifications. Not a message-only window: HWND_MESSAGE is excluded from
// broadcasts, so it would never see WM_DISPLAYCHANGE.
func (e *Engine) createNotifyWindow() error {
	hinst := win.GetModuleHandle()
	wc := win.WNDCLASSEX{
		LpfnWndProc:   notifyWndProc,
		HInstance:     hinst,
		LpszClassName: win.UTF16Ptr(notifyClassName),
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if win.RegisterClassEx(&wc) == 0 {
		return fmt.Errorf("registering the notification window class failed")
	}
	hwnd := win.CreateWindowEx(
		win.WS_EX_TOOLWINDOW,
		win.UTF16Ptr(notifyClassName),
		win.UTF16Ptr("WinDock-Go"),
		win.WS_POPUP, // created without WS_VISIBLE, so it never appears
		0, 0, 0, 0,
		0, hinst)
	if hwnd == 0 {
		return fmt.Errorf("creating the notification window failed")
	}
	e.notify = hwnd
	e.notifyHandle.Store(uintptr(hwnd))
	return nil
}

// refreshDisplays rebuilds the monitor set and re-resolves the active profile.
// Every path that could have changed the topology funnels through here.
func (e *Engine) refreshDisplays(reason string) {
	before := e.mons.Fingerprint()
	e.mons = monitor.Enumerate()
	after := e.mons.Fingerprint()

	e.layout = zones.Resolve(e.cfg.ActiveRules(), e.mons, e.cfg.GlobalSettings)
	e.slots = assignSlots(e.mons, e.layout)
	e.guides.rebuild(e.mons, e.layout, e.cfg.GlobalSettings, e.slots)

	if before == after {
		e.log.Debug("display refresh, topology unchanged", "reason", reason)
		return
	}
	e.log.Info("display topology changed",
		"reason", reason,
		"monitors", e.mons.Describe(),
		"zones", e.layout.Len(),
		"skipped_rules", e.layout.Skipped)
	if e.layout.Skipped > 0 {
		e.log.Warn("some rules reference monitors that are not attached; they are inactive",
			"skipped", e.layout.Skipped)
	}
	// A topology change invalidates any drag in progress.
	e.cancelDrag()
}

// scheduleRefresh asks for another refresh later: after an RDP reconnect the
// geometry keeps settling for a second or two, so one read is not enough.
func (e *Engine) scheduleRefresh(delay time.Duration, reason string) {
	hwnd := e.notify
	log := e.log
	go func() {
		time.Sleep(delay)
		log.Debug("scheduled display refresh", "reason", reason, "after", delay)
		win.PostMessage(hwnd, wmRefreshDisplays, 0, 0)
	}()
}

func (e *Engine) onSessionChange(event uintptr) {
	name := sessionEventName(uint32(event))
	e.log.Info("session change", "event", name)
	switch uint32(event) {
	case win.WTS_REMOTE_CONNECT, win.WTS_REMOTE_DISCONNECT,
		win.WTS_CONSOLE_CONNECT, win.WTS_CONSOLE_DISCONNECT,
		win.WTS_SESSION_UNLOCK, win.WTS_SESSION_LOGON:
		// Read now, then again as the display set settles.
		e.refreshDisplays("session:" + name)
		e.scheduleRefresh(750*time.Millisecond, "session settle")
		e.scheduleRefresh(3*time.Second, "session settle")
	}
}

func sessionEventName(e uint32) string {
	switch e {
	case win.WTS_CONSOLE_CONNECT:
		return "console_connect"
	case win.WTS_CONSOLE_DISCONNECT:
		return "console_disconnect"
	case win.WTS_REMOTE_CONNECT:
		return "remote_connect"
	case win.WTS_REMOTE_DISCONNECT:
		return "remote_disconnect"
	case win.WTS_SESSION_LOGON:
		return "logon"
	case win.WTS_SESSION_LOGOFF:
		return "logoff"
	case win.WTS_SESSION_LOCK:
		return "lock"
	case win.WTS_SESSION_UNLOCK:
		return "unlock"
	case win.WTS_SESSION_REMOTE_CONTROL:
		return "remote_control"
	}
	return fmt.Sprintf("unknown(%d)", e)
}

// --- drag lifecycle -------------------------------------------------------

func (e *Engine) onMoveSizeStart(hwnd win.HWND) {
	if !e.cfg.GlobalSettings.DockingEnabled {
		return
	}
	if e.layout.Len() == 0 {
		return
	}
	if !snappable(hwnd) {
		return
	}
	e.drag = dragState{active: true, hwnd: hwnd, zoneIdx: -1}
	e.startMouseHook()
	// Show every trigger as soon as the drag begins, so the targets are
	// visible before the cursor reaches one.
	e.guides.show()
	e.updatePreview(win.GetCursorPos())
}

func (e *Engine) onMoveSizeEnd(hwnd win.HWND) {
	if !e.drag.active {
		return
	}
	zone := e.drag.zone
	target := e.drag.hwnd
	e.stopMouseHook()
	e.guides.hide()
	e.overlay.hide()
	e.drag = dragState{zoneIdx: -1}

	if zone == nil || target != hwnd {
		return
	}
	e.applySnap(target, zone)
}

func (e *Engine) cancelDrag() {
	if !e.drag.active {
		return
	}
	e.stopMouseHook()
	e.guides.hide()
	e.overlay.hide()
	e.drag = dragState{zoneIdx: -1}
}

// updatePreview re-evaluates which zone the cursor is over. Called from the
// mouse hook, so the overlay only moves when the zone actually changes.
func (e *Engine) updatePreview(p win.POINT) {
	if !e.drag.active {
		return
	}
	idx := e.layout.MatchIndex(p)
	if idx == e.drag.zoneIdx {
		return
	}
	e.drag.zoneIdx = idx
	e.guides.setActive(idx)

	if idx < 0 {
		e.drag.zone = nil
		e.overlay.hide()
		return
	}
	zone := &e.layout.Zones()[idx]
	e.drag.zone = zone
	e.overlay.show(zone.Dock, e.slotFor(idx))
}

// slotFor returns the palette slot of a zone, defaulting to the first colour if
// the assignment is somehow out of step with the layout.
func (e *Engine) slotFor(zoneIndex int) int {
	if zoneIndex < 0 || zoneIndex >= len(e.slots) {
		return 0
	}
	return e.slots[zoneIndex]
}

func (e *Engine) startMouseHook() {
	if e.mouse != 0 {
		return
	}
	e.mouse = win.SetWindowsHookEx(win.WH_MOUSE_LL, mouseCallback, win.GetModuleHandle(), 0)
	if e.mouse == 0 {
		e.log.Warn("low-level mouse hook failed; zone preview will not track the cursor")
	}
}

func (e *Engine) stopMouseHook() {
	if e.mouse == 0 {
		return
	}
	win.UnhookWindowsHookEx(e.mouse)
	e.mouse = 0
}

// --- snapping -------------------------------------------------------------

// applySnap positions hwnd so that its visible edges line up with the dock.
func (e *Engine) applySnap(hwnd win.HWND, z *zones.Zone) {
	dock := z.Dock
	if !win.IsWindow(hwnd) {
		return
	}
	if win.IsZoomed(hwnd) {
		// A maximized window ignores SetWindowPos sizing; restore it first.
		win.ShowWindow(hwnd, win.SW_RESTORE)
	}

	target := adjustForFrame(hwnd, dock)

	ok := win.SetWindowPos(hwnd, 0,
		target.Left, target.Top, target.Width(), target.Height(),
		win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_NOOWNERZORDER)
	if !ok {
		e.log.Warn("could not reposition window",
			"class", win.GetClassName(hwnd),
			"hint", "the window may belong to an elevated process; run WinDock elevated to manage it")
		return
	}

	if e.cfg.GlobalSettings.MaximizeFullWindow && e.dockCoversWorkArea(dock, z.WorkArea) {
		win.ShowWindow(hwnd, win.SW_MAXIMIZE)
		return
	}

	// Some applications reposition themselves as their own move loop unwinds.
	// Re-check shortly and reapply once if the window drifted.
	e.scheduleVerify(hwnd, target)
}

func (e *Engine) scheduleVerify(hwnd win.HWND, target win.RECT) {
	notify := e.notify
	pending := &pendingSnap{hwnd: hwnd, target: target}
	verifyPending.Store(pending)
	go func() {
		time.Sleep(60 * time.Millisecond)
		win.PostMessage(notify, wmVerifySnap, 0, 0)
	}()
}

type pendingSnap struct {
	hwnd   win.HWND
	target win.RECT
}

var verifyPending atomic.Pointer[pendingSnap]

func (e *Engine) verifySnap() {
	p := verifyPending.Swap(nil)
	if p == nil || !win.IsWindow(p.hwnd) {
		return
	}
	cur, ok := win.GetWindowRect(p.hwnd)
	if !ok || cur == p.target {
		return
	}
	if win.IsZoomed(p.hwnd) {
		// The app maximized itself; leave it alone.
		return
	}
	e.log.Debug("window drifted after snap, reapplying",
		"class", win.GetClassName(p.hwnd))
	win.SetWindowPos(p.hwnd, 0,
		p.target.Left, p.target.Top, p.target.Width(), p.target.Height(),
		win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_NOOWNERZORDER)
}

func (e *Engine) dockCoversWorkArea(dock win.RECT, useWorkArea bool) bool {
	m := e.mons.FromPoint(win.POINT{X: dock.Left + dock.Width()/2, Y: dock.Top + dock.Height()/2})
	if m == nil {
		return false
	}
	wa := m.Area(useWorkArea)
	const slack = 2
	return abs32(dock.Left-wa.Left) <= slack && abs32(dock.Top-wa.Top) <= slack &&
		abs32(dock.Right-wa.Right) <= slack && abs32(dock.Bottom-wa.Bottom) <= slack
}

// adjustForFrame grows the target by the window's invisible resize border, so
// the visible frame lands on the zone. Since Vista GetWindowRect includes that
// border; DWMWA_EXTENDED_FRAME_BOUNDS gives the real painted bounds.
func adjustForFrame(hwnd win.HWND, dock win.RECT) win.RECT {
	wr, ok1 := win.GetWindowRect(hwnd)
	fb, ok2 := win.DwmGetExtendedFrameBounds(hwnd)
	if !ok1 || !ok2 || fb.IsEmpty() {
		return dock
	}
	left := fb.Left - wr.Left
	top := fb.Top - wr.Top
	right := wr.Right - fb.Right
	bottom := wr.Bottom - fb.Bottom

	// Guard against nonsense from windows with unusual frames.
	const maxPad = 64
	if left < 0 || top < 0 || right < 0 || bottom < 0 ||
		left > maxPad || top > maxPad || right > maxPad || bottom > maxPad {
		return dock
	}
	return win.RECT{
		Left:   dock.Left - left,
		Top:    dock.Top - top,
		Right:  dock.Right + right,
		Bottom: dock.Bottom + bottom,
	}
}

// snappable reports whether a window is something we should manage.
func snappable(hwnd win.HWND) bool {
	if hwnd == 0 || !win.IsWindow(hwnd) || !win.IsWindowVisible(hwnd) {
		return false
	}
	if win.GetAncestor(hwnd, win.GA_ROOT) != hwnd {
		return false // a child or owned window, not the frame being dragged
	}
	style := win.GetWindowLong(hwnd, win.GWL_STYLE)
	if style&win.WS_CHILD != 0 {
		return false
	}
	// A window we can meaningfully place has either a title bar or a resizable
	// frame. This is also what keeps the taskbar and desktop out.
	if style&(win.WS_CAPTION|win.WS_THICKFRAME) == 0 {
		return false
	}
	ex := win.GetWindowLong(hwnd, win.GWL_EXSTYLE)
	if ex&win.WS_EX_TOOLWINDOW != 0 {
		return false
	}
	if excludedClasses[win.GetClassName(hwnd)] {
		return false
	}
	if win.IsCloaked(hwnd) {
		return false // a UWP ghost window
	}
	return true
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// --- config reloading -----------------------------------------------------

// watchConfig polls the config file's modification time so hand edits to
// profile.json take effect without a restart.
func (e *Engine) watchConfig(ctx context.Context) func() {
	if fi, err := os.Stat(e.cfgPath); err == nil {
		e.lastCfgMod = fi.ModTime()
	}
	done := make(chan struct{})
	notify := e.notify
	path := e.cfgPath
	last := e.lastCfgMod

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
				fi, err := os.Stat(path)
				if err != nil {
					continue
				}
				if fi.ModTime().After(last) {
					last = fi.ModTime()
					win.PostMessage(notify, wmReloadConfig, 0, 0)
				}
			}
		}
	}()
	return func() { close(done) }
}

func (e *Engine) reloadConfig() {
	cfg, err := config.Load(e.cfgPath)
	if err != nil {
		e.log.Warn("could not reload configuration; keeping the previous one", "error", err)
		return
	}
	e.cfg = cfg
	initPalette(e.cfg.GlobalSettings.PreviewFill(), e.cfg.GlobalSettings.CategoricalColors())
	e.layout = zones.Resolve(e.cfg.ActiveRules(), e.mons, e.cfg.GlobalSettings)
	e.slots = assignSlots(e.mons, e.layout)
	e.guides.rebuild(e.mons, e.layout, e.cfg.GlobalSettings, e.slots)
	e.cancelDrag()
	e.syncOverlay()
	e.log.Info("configuration reloaded",
		"profile", e.activeProfileName(),
		"zones", e.layout.Len(),
		"docking_enabled", e.cfg.GlobalSettings.DockingEnabled)
}

// syncOverlay brings the preview window into line with the configuration. The
// setting can be turned either way while the engine runs, and the opacity can
// change under it, so this is called on startup and on every reload rather than
// only once.
func (e *Engine) syncOverlay() {
	if !e.cfg.GlobalSettings.PreviewEnabled() {
		e.overlay.destroy()
		e.overlay = nil
		return
	}
	if e.overlay != nil {
		e.overlay.setAlpha(e.cfg.GlobalSettings.PreviewOpacity())
		return
	}
	e.overlay = newOverlay(e.cfg.GlobalSettings.PreviewOpacity())
	if e.overlay == nil {
		e.log.Warn("preview overlay unavailable; snapping will work without a preview")
	}
}

// --- Win32 callbacks ------------------------------------------------------

var winEventCallback = syscall.NewCallback(func(
	hook win.HWINEVENTHOOK, event uint32, hwnd win.HWND,
	idObject, idChild int32, threadID, timestamp uint32,
) uintptr {
	e := current.Load()
	if e == nil {
		return 0
	}
	if idObject != win.OBJID_WINDOW || idChild != win.CHILDID_SELF {
		return 0
	}
	switch event {
	case win.EVENT_SYSTEM_MOVESIZESTART:
		e.onMoveSizeStart(hwnd)
	case win.EVENT_SYSTEM_MOVESIZEEND:
		e.onMoveSizeEnd(hwnd)
	}
	return 0
})

var mouseCallback = syscall.NewCallback(func(code int32, wparam, lparam uintptr) uintptr {
	e := current.Load()
	if e == nil {
		return 0
	}
	if code >= 0 && wparam == win.WM_MOUSEMOVE {
		// The hook fires with the cursor already at the new position, so this
		// avoids reinterpreting lparam as a pointer.
		e.updatePreview(win.GetCursorPos())
	}
	return win.CallNextHookEx(e.mouse, int(code), wparam, lparam)
})

var notifyWndProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	e := current.Load()
	if e == nil {
		return win.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case win.WM_DISPLAYCHANGE:
		e.refreshDisplays("WM_DISPLAYCHANGE")
		e.scheduleRefresh(750*time.Millisecond, "display settle")

	case win.WM_DPICHANGED:
		e.refreshDisplays("WM_DPICHANGED")

	case win.WM_SETTINGCHANGE:
		// SPI_SETWORKAREA arrives this way when the taskbar moves or resizes.
		e.refreshDisplays("WM_SETTINGCHANGE")

	case win.WM_WTSSESSION_CHANGE:
		e.onSessionChange(wparam)

	case wmRefreshDisplays:
		e.refreshDisplays("scheduled")

	case wmReloadConfig:
		e.reloadConfig()

	case wmVerifySnap:
		e.verifySnap()

	case wmShutdown, win.WM_CLOSE:
		win.PostQuitMessage(0)

	case win.WM_DESTROY:
		win.PostQuitMessage(0)

	default:
		return win.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	return 0
})
