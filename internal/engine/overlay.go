package engine

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/BjarkeM/windock-go/internal/palette"
	"github.com/BjarkeM/windock-go/internal/win"
)

const overlayClassName = "WinDockGoOverlay"

const borderWidth = 4

// overlaySlot is the palette slot the preview is painted in: the colour of the
// trigger that activated it.
var (
	overlaySlot int

	overlayClassOnce sync.Once
	overlayClassOK   bool
)

var overlayWndProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case win.WM_ERASEBKGND:
		// Painted entirely in WM_PAINT; erasing first would only flicker.
		return 1

	case win.WM_PAINT:
		var ps win.PAINTSTRUCT
		hdc := win.BeginPaint(hwnd, &ps)
		rc := win.GetClientRect(hwnd)
		b := brushesFor(overlaySlot)
		if b.fill != 0 {
			win.FillRect(hdc, &rc, b.fill)
		}
		if b.border != 0 {
			// FrameRect draws one pixel, so inset repeatedly for a thick edge.
			r := rc
			for i := 0; i < borderWidth; i++ {
				if r.IsEmpty() {
					break
				}
				win.FrameRect(hdc, &r, b.border)
				r.Left++
				r.Top++
				r.Right--
				r.Bottom--
			}
		}
		win.EndPaint(hwnd, &ps)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wparam, lparam)
})

// overlay is the translucent rectangle shown over the zone a release would snap
// to.
//
// The original resized the real window live from its injected hook. Out of
// process that is not possible, so the destination is previewed and committed
// on release, similar to what FancyZones does.
type overlay struct {
	hwnd    win.HWND
	visible bool
	last    win.RECT
}

// newOverlay creates the preview window, or nil. Callers treat nil as "previews
// disabled" rather than an error; snapping does not depend on it.
func newOverlay(alpha uint8) *overlay {
	hinst := win.GetModuleHandle()
	overlayClassOnce.Do(func() {
		wc := win.WNDCLASSEX{
			LpfnWndProc:   overlayWndProc,
			HInstance:     hinst,
			LpszClassName: win.UTF16Ptr(overlayClassName),
			// No class background brush: WM_PAINT does all the drawing.
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		overlayClassOK = win.RegisterClassEx(&wc) != 0
	})
	if !overlayClassOK {
		return nil
	}

	hwnd := win.CreateWindowEx(
		win.WS_EX_LAYERED|win.WS_EX_TRANSPARENT|win.WS_EX_NOACTIVATE|
			win.WS_EX_TOOLWINDOW|win.WS_EX_TOPMOST,
		win.UTF16Ptr(overlayClassName),
		win.UTF16Ptr(""),
		win.WS_POPUP,
		0, 0, 0, 0,
		0, hinst)
	if hwnd == 0 {
		return nil
	}
	win.SetLayeredWindowAttributes(hwnd, 0, alpha, win.LWA_ALPHA)

	return &overlay{hwnd: hwnd}
}

// colorRef converts the friendlier 0xRRGGBB used in the config into the Win32
// COLORREF layout, which is 0x00BBGGRR.
func colorRef(c uint32) uint32 {
	r := (c >> 16) & 0xff
	g := (c >> 8) & 0xff
	b := c & 0xff
	return b<<16 | g<<8 | r
}

// lighten blends a colour towards white by f (0..1), used to derive a border
// that stands out against its own fill.
func lighten(c uint32, f float64) uint32 { return palette.Lighten(c, f) }

// show moves the preview over r and paints it in the given palette slot.
// Repeats are no-ops, which matters on the mouse-hook path.
func (o *overlay) show(r win.RECT, slot int) {
	if o == nil || o.hwnd == 0 {
		return
	}
	if o.visible && o.last == r && overlaySlot == slot {
		return
	}
	overlaySlot = slot
	win.SetWindowPos(o.hwnd, win.HWND_TOPMOST,
		r.Left, r.Top, r.Width(), r.Height(),
		win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	// Resizing does not repaint the whole client area on its own, so the
	// border would be drawn at the old size.
	win.InvalidateRect(o.hwnd, false)
	o.last = r
	o.visible = true
}

func (o *overlay) hide() {
	if o == nil || o.hwnd == 0 || !o.visible {
		return
	}
	win.SetWindowPos(o.hwnd, win.HWND_TOPMOST, 0, 0, 0, 0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE|win.SWP_HIDEWINDOW)
	o.visible = false
}

// setAlpha re-applies the opacity, which can change while the engine runs.
func (o *overlay) setAlpha(alpha uint8) {
	if o == nil || o.hwnd == 0 {
		return
	}
	win.SetLayeredWindowAttributes(o.hwnd, 0, alpha, win.LWA_ALPHA)
}

func (o *overlay) destroy() {
	if o == nil {
		return
	}
	win.DestroyWindow(o.hwnd)
	o.hwnd = 0
	o.visible = false
}
