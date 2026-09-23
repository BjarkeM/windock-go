package engine

import "github.com/BjarkeM/windock-go/internal/win"

// Windows has drag-to-edge snapping of its own: it draws a grey preview over
// the half or quarter it has chosen, and places the window there on release.
// That competes with us for the same drop - two placements for one gesture,
// and a preview that is not ours - so while docking is enabled we turn it off
// and put it back exactly as we found it.
//
// Indirected through these variables so the save-and-restore logic can be
// tested without touching the machine's real setting.
var (
	readArranging  = win.WindowArranging
	writeArranging = win.SetWindowArranging
)

// syncWindowsSnap brings Windows' own snapping into line with our docking
// setting. Called on startup and on every reload, since docking can be turned
// either way while we run.
func (e *Engine) syncWindowsSnap() {
	if e.cfg.GlobalSettings.DockingEnabled {
		e.suppressWindowsSnap()
		return
	}
	e.restoreWindowsSnap()
}

// suppressWindowsSnap turns Windows' snapping off, remembering that it is ours
// to put back. A setting the user had already turned off is left alone and not
// remembered: we only ever restore what we ourselves changed.
func (e *Engine) suppressWindowsSnap() {
	if e.winSnapSuppressed {
		return
	}
	on, ok := readArranging()
	if !ok {
		e.log.Warn("could not read the Windows snap setting; leaving it alone",
			"effect", "Windows may draw its own drag preview alongside ours")
		return
	}
	if !on {
		return // already off, so there is nothing to turn off or restore
	}
	if !writeArranging(false) {
		e.log.Warn("could not turn Windows' own drag-to-edge snapping off",
			"effect", "Windows may draw its own drag preview alongside ours")
		return
	}
	e.winSnapSuppressed = true
	e.log.Info("turned Windows' own drag-to-edge snapping off for this session")
}

// restoreWindowsSnap puts the setting back. Safe to call at any time: it does
// nothing unless we turned the setting off ourselves.
//
// Nothing was written to the registry, so even a crash costs the user only the
// rest of this session: their setting is back at the next sign-in.
func (e *Engine) restoreWindowsSnap() {
	if !e.winSnapSuppressed {
		return
	}
	e.winSnapSuppressed = false
	if !writeArranging(true) {
		e.log.Warn("could not restore Windows' own drag-to-edge snapping",
			"hint", "it is set again at your next sign-in, or from "+
				"Settings > System > Multitasking > Snap windows")
		return
	}
	e.log.Info("restored Windows' own drag-to-edge snapping")
}
