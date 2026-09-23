// The win package holds the Win32 bindings WinDock needs. Everything is
// out-of-process: other applications' windows are observed and repositioned
// from our own, and no DLL is ever injected.
package win

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	wtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")

	procSetWinEventHook            = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent             = user32.NewProc("UnhookWinEvent")
	procSetWindowsHookExW          = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx        = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx             = user32.NewProc("CallNextHookEx")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procGetWindowLongW             = user32.NewProc("GetWindowLongW")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procIsWindow                   = user32.NewProc("IsWindow")
	procIsZoomed                   = user32.NewProc("IsZoomed")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procGetCursorPos               = user32.NewProc("GetCursorPos")
	procEnumDisplayMonitors        = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetAncestor                = user32.NewProc("GetAncestor")
	procSystemParametersInfoW      = user32.NewProc("SystemParametersInfoW")
	procSetProcessDpiAwarenessCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetThreadDpiAwarenessCtx   = user32.NewProc("GetThreadDpiAwarenessContext")
	procGetAwarenessFromDpiCtx     = user32.NewProc("GetAwarenessFromDpiAwarenessContext")
	procCreateSolidBrush           = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject               = gdi32.NewProc("DeleteObject")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procFillRect                   = user32.NewProc("FillRect")
	procFrameRect                  = user32.NewProc("FrameRect")
	procGetClientRect              = user32.NewProc("GetClientRect")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	procFreeConsole                = kernel32.NewProc("FreeConsole")
	procGetConsoleWindow           = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList      = kernel32.NewProc("GetConsoleProcessList")
	procDwmGetWindowAttribute      = dwmapi.NewProc("DwmGetWindowAttribute")
	procWTSRegisterSession         = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procWTSUnRegisterSession       = wtsapi32.NewProc("WTSUnRegisterSessionNotification")
)

type (
	HWND          uintptr
	HMONITOR      uintptr
	HHOOK         uintptr
	HWINEVENTHOOK uintptr
)

// RECT mirrors the Win32 RECT. Right and Bottom are exclusive.
type RECT struct {
	Left, Top, Right, Bottom int32
}

func (r RECT) Width() int32  { return r.Right - r.Left }
func (r RECT) Height() int32 { return r.Bottom - r.Top }

func (r RECT) Contains(p POINT) bool {
	return p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

func (r RECT) IsEmpty() bool { return r.Right <= r.Left || r.Bottom <= r.Top }

// POINT mirrors the Win32 POINT.
type POINT struct {
	X, Y int32
}

// MSG mirrors the Win32 MSG.
type MSG struct {
	Hwnd    HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// MONITORINFOEX mirrors the Win32 MONITORINFOEXW.
type MONITORINFOEX struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
	SzDevice  [32]uint16
}

// Device returns the adapter-relative device name, e.g. `\\.\DISPLAY1`.
func (m MONITORINFOEX) Device() string {
	return windows.UTF16ToString(m.SzDevice[:])
}

// WNDCLASSEX mirrors the Win32 WNDCLASSEXW.
type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// MSLLHOOKSTRUCT mirrors the Win32 low-level mouse hook payload.
type MSLLHOOKSTRUCT struct {
	Pt          POINT
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// PAINTSTRUCT mirrors the Win32 PAINTSTRUCT.
type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

// Window messages.
const (
	WM_DESTROY           = 0x0002
	WM_CLOSE             = 0x0010
	WM_QUIT              = 0x0012
	WM_SETTINGCHANGE     = 0x001A
	WM_DISPLAYCHANGE     = 0x007E
	WM_WTSSESSION_CHANGE = 0x02B1
	WM_DPICHANGED        = 0x02E0
	WM_PAINT             = 0x000F
	WM_ERASEBKGND        = 0x0014
	WM_MOUSEMOVE         = 0x0200
	WM_APP               = 0x8000
)

// WTS session-change notifications, which the original never subscribed to -
// hence its stale geometry after an RDP disconnect.
const (
	NOTIFY_FOR_THIS_SESSION = 0

	WTS_CONSOLE_CONNECT        = 0x1
	WTS_CONSOLE_DISCONNECT     = 0x2
	WTS_REMOTE_CONNECT         = 0x3
	WTS_REMOTE_DISCONNECT      = 0x4
	WTS_SESSION_LOGON          = 0x5
	WTS_SESSION_LOGOFF         = 0x6
	WTS_SESSION_LOCK           = 0x7
	WTS_SESSION_UNLOCK         = 0x8
	WTS_SESSION_REMOTE_CONTROL = 0x9
)

// WinEvent constants.
const (
	EVENT_SYSTEM_MOVESIZESTART = 0x000A
	EVENT_SYSTEM_MOVESIZEEND   = 0x000B

	WINEVENT_OUTOFCONTEXT   = 0x0000
	WINEVENT_SKIPOWNPROCESS = 0x0002

	OBJID_WINDOW = 0
	CHILDID_SELF = 0
)

// Hook types.
const (
	WH_MOUSE_LL = 14
)

// Window styles.
const (
	GWL_STYLE   = -16
	GWL_EXSTYLE = -20

	WS_CHILD       = 0x40000000
	WS_CAPTION     = 0x00C00000
	WS_THICKFRAME  = 0x00040000
	WS_POPUP       = 0x80000000
	WS_SYSMENU     = 0x00080000
	WS_MINIMIZEBOX = 0x00020000
	WS_MAXIMIZEBOX = 0x00010000

	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_TOPMOST     = 0x00000008
	WS_EX_LAYERED     = 0x00080000
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_NOACTIVATE  = 0x08000000
)

// SetWindowPos flags.
const (
	SWP_NOSIZE        = 0x0001
	SWP_NOMOVE        = 0x0002
	SWP_NOZORDER      = 0x0004
	SWP_NOACTIVATE    = 0x0010
	SWP_SHOWWINDOW    = 0x0040
	SWP_HIDEWINDOW    = 0x0080
	SWP_NOOWNERZORDER = 0x0200
)

// ShowWindow commands.
const (
	SW_MAXIMIZE = 3
	SW_RESTORE  = 9
)

// Z-order sentinels for SetWindowPos.
const (
	HWND_TOPMOST = ^HWND(0) // (HWND)-1
)

// Misc.
const (
	MONITORINFOF_PRIMARY = 1

	GA_ROOT = 2

	LWA_ALPHA    = 0x02
	LWA_COLORKEY = 0x01

	DWMWA_EXTENDED_FRAME_BOUNDS = 9
	DWMWA_CLOAKED               = 14

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is (HANDLE)-4.
	DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = ^uintptr(0) - 3

	DPI_AWARENESS_PER_MONITOR_AWARE = 2
)

// SetProcessDpiAwarenessContextV2 opts into per-monitor DPI awareness v2, so
// GetWindowRect and SetWindowPos speak physical pixels on mixed-DPI setups.
//
// It reports success if the process ends up aware, whether by this call or by
// the manifest: setting it twice fails, and a bare return value would report a
// false problem on a manifested build.
func SetProcessDpiAwarenessContextV2() bool {
	if err := procSetProcessDpiAwarenessCtx.Find(); err != nil {
		return false
	}
	if r, _, _ := procSetProcessDpiAwarenessCtx.Call(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2); r != 0 {
		return true
	}
	return IsPerMonitorDpiAware()
}

// IsPerMonitorDpiAware reports the awareness the process actually has.
func IsPerMonitorDpiAware() bool {
	if err := procGetThreadDpiAwarenessCtx.Find(); err != nil {
		return false
	}
	if err := procGetAwarenessFromDpiCtx.Find(); err != nil {
		return false
	}
	ctx, _, _ := procGetThreadDpiAwarenessCtx.Call()
	a, _, _ := procGetAwarenessFromDpiCtx.Call(ctx)
	return int32(a) == DPI_AWARENESS_PER_MONITOR_AWARE
}

// OwnsItsConsole reports whether this process is the only one attached to its
// console, as when launched from Explorer rather than a shell.
func OwnsItsConsole() bool {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return false // no console at all
	}
	var pids [4]uint32
	n, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// FreeConsole detaches from the console, closing the window if it was ours.
func FreeConsole() {
	procFreeConsole.Call()
}

func GetModuleHandle() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func SetWinEventHook(min, max uint32, callback uintptr, flags uint32) HWINEVENTHOOK {
	h, _, _ := procSetWinEventHook.Call(
		uintptr(min), uintptr(max), 0, callback, 0, 0, uintptr(flags))
	return HWINEVENTHOOK(h)
}

func UnhookWinEvent(h HWINEVENTHOOK) {
	if h != 0 {
		procUnhookWinEvent.Call(uintptr(h))
	}
}

func SetWindowsHookEx(idHook int, callback uintptr, hmod uintptr, threadID uint32) HHOOK {
	h, _, _ := procSetWindowsHookExW.Call(uintptr(idHook), callback, hmod, uintptr(threadID))
	return HHOOK(h)
}

func UnhookWindowsHookEx(h HHOOK) {
	if h != 0 {
		procUnhookWindowsHookEx.Call(uintptr(h))
	}
}

func CallNextHookEx(h HHOOK, code int, wparam, lparam uintptr) uintptr {
	r, _, _ := procCallNextHookEx.Call(uintptr(h), uintptr(code), wparam, lparam)
	return r
}

// GetMessage returns false when WM_QUIT is received or the call fails.
func GetMessage(msg *MSG) bool {
	r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(msg)), 0, 0, 0)
	return int32(r) > 0
}

func TranslateMessage(msg *MSG) {
	procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
}

func DispatchMessage(msg *MSG) {
	procDispatchMessageW.Call(uintptr(unsafe.Pointer(msg)))
}

func PostQuitMessage(code int) {
	procPostQuitMessage.Call(uintptr(code))
}

// PostMessage queues a message to a window. It is safe to call from any
// goroutine, which is how background timers nudge the message loop.
func PostMessage(hwnd HWND, msg uint32, wparam, lparam uintptr) bool {
	r, _, _ := procPostMessageW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam)
	return r != 0
}

func RegisterClassEx(wc *WNDCLASSEX) uint16 {
	r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc)))
	return uint16(r)
}

func CreateWindowEx(exStyle uint32, class, title *uint16, style uint32, x, y, w, h int32, parent HWND, hinst uintptr) HWND {
	r, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(title)),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(parent), 0, hinst, 0)
	return HWND(r)
}

func DestroyWindow(hwnd HWND) {
	if hwnd != 0 {
		procDestroyWindow.Call(uintptr(hwnd))
	}
}

func DefWindowProc(hwnd HWND, msg uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam)
	return r
}

func SetWindowPos(hwnd, after HWND, x, y, cx, cy int32, flags uint32) bool {
	r, _, _ := procSetWindowPos.Call(
		uintptr(hwnd), uintptr(after),
		uintptr(x), uintptr(y), uintptr(cx), uintptr(cy),
		uintptr(flags))
	return r != 0
}

func GetWindowRect(hwnd HWND) (RECT, bool) {
	var r RECT
	ok, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func GetWindowLong(hwnd HWND, index int32) uint32 {
	r, _, _ := procGetWindowLongW.Call(uintptr(hwnd), uintptr(index))
	return uint32(r)
}

func GetClassName(hwnd HWND) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func IsWindow(hwnd HWND) bool {
	r, _, _ := procIsWindow.Call(uintptr(hwnd))
	return r != 0
}

func IsWindowVisible(hwnd HWND) bool {
	r, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	return r != 0
}

func IsZoomed(hwnd HWND) bool {
	r, _, _ := procIsZoomed.Call(uintptr(hwnd))
	return r != 0
}

func ShowWindow(hwnd HWND, cmd int32) {
	procShowWindow.Call(uintptr(hwnd), uintptr(cmd))
}

// --- window arrangement (Windows' own snapping) ---------------------------

// SPI_*WINARRANGING is the per-user "Snap windows" setting: whether dragging a
// window to a screen edge makes Windows preview and then snap it. It is the
// same bit as HKCU\Control Panel\Desktop\WindowArrangementActive, but going
// through SystemParametersInfo is what applies it to the running session.
const (
	SPI_GETWINARRANGING = 0x0082
	SPI_SETWINARRANGING = 0x0083

	SPIF_UPDATEINIFILE = 0x01
	SPIF_SENDCHANGE    = 0x02
)

// WindowArranging reports whether Windows' own drag-to-edge snapping is on.
// The second result is false if the setting could not be read at all, which is
// not the same as it being off.
func WindowArranging() (bool, bool) {
	var on int32 // a Win32 BOOL
	r, _, _ := procSystemParametersInfoW.Call(
		SPI_GETWINARRANGING, 0, uintptr(unsafe.Pointer(&on)), 0)
	if r == 0 {
		return false, false
	}
	return on != 0, true
}

// SetWindowArranging turns Windows' own drag-to-edge snapping on or off for
// this session.
//
// Note the asymmetry with the getter, which is how the API is defined: the get
// takes a pointer to a BOOL, the set takes the boolean itself in the pvParam
// slot.
//
// SPIF_UPDATEINIFILE is deliberately not passed, so the change is never
// written to the registry: if we exit without restoring it, the user's setting
// is back at their next sign-in rather than silently left off.
func SetWindowArranging(on bool) bool {
	var v uintptr
	if on {
		v = 1
	}
	r, _, _ := procSystemParametersInfoW.Call(
		SPI_SETWINARRANGING, 0, v, SPIF_SENDCHANGE)
	return r != 0
}

func GetCursorPos() POINT {
	var p POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func GetAncestor(hwnd HWND, flags uint32) HWND {
	r, _, _ := procGetAncestor.Call(uintptr(hwnd), uintptr(flags))
	return HWND(r)
}

func GetMonitorInfo(h HMONITOR) (MONITORINFOEX, bool) {
	var mi MONITORINFOEX
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	r, _, _ := procGetMonitorInfoW.Call(uintptr(h), uintptr(unsafe.Pointer(&mi)))
	return mi, r != 0
}

func SetLayeredWindowAttributes(hwnd HWND, colorKey uint32, alpha byte, flags uint32) bool {
	r, _, _ := procSetLayeredWindowAttributes.Call(uintptr(hwnd), uintptr(colorKey), uintptr(alpha), uintptr(flags))
	return r != 0
}

func CreateSolidBrush(color uint32) uintptr {
	r, _, _ := procCreateSolidBrush.Call(uintptr(color))
	return r
}

func DeleteObject(h uintptr) {
	if h != 0 {
		procDeleteObject.Call(h)
	}
}

func BeginPaint(hwnd HWND, ps *PAINTSTRUCT) uintptr {
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(ps)))
	return hdc
}

func EndPaint(hwnd HWND, ps *PAINTSTRUCT) {
	procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(ps)))
}

func FillRect(hdc uintptr, r *RECT, brush uintptr) {
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(r)), brush)
}

// FrameRect draws a one-pixel border just inside r.
func FrameRect(hdc uintptr, r *RECT, brush uintptr) {
	procFrameRect.Call(hdc, uintptr(unsafe.Pointer(r)), brush)
}

func GetClientRect(hwnd HWND) RECT {
	var r RECT
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

// InvalidateRect marks the whole window for repainting.
func InvalidateRect(hwnd HWND, erase bool) {
	var e uintptr
	if erase {
		e = 1
	}
	procInvalidateRect.Call(uintptr(hwnd), 0, e)
}

// DwmGetExtendedFrameBounds returns the window's visible bounds. GetWindowRect
// includes an invisible resize border, so snapping to it leaves a gap at the
// screen edge. The original fixed this the same way in v1.2.2.
func DwmGetExtendedFrameBounds(hwnd HWND) (RECT, bool) {
	var r RECT
	hr, _, _ := procDwmGetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(DWMWA_EXTENDED_FRAME_BOUNDS),
		uintptr(unsafe.Pointer(&r)),
		unsafe.Sizeof(r))
	return r, hr == 0
}

// IsCloaked reports whether the window is DWM-cloaked. UWP apps keep hidden
// ghost windows around that pass IsWindowVisible but are cloaked.
func IsCloaked(hwnd HWND) bool {
	var cloaked uint32
	hr, _, _ := procDwmGetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(DWMWA_CLOAKED),
		uintptr(unsafe.Pointer(&cloaked)),
		unsafe.Sizeof(cloaked))
	return hr == 0 && cloaked != 0
}

func WTSRegisterSessionNotification(hwnd HWND, flags uint32) bool {
	if err := procWTSRegisterSession.Find(); err != nil {
		return false
	}
	r, _, _ := procWTSRegisterSession.Call(uintptr(hwnd), uintptr(flags))
	return r != 0
}

func WTSUnRegisterSessionNotification(hwnd HWND) {
	if err := procWTSUnRegisterSession.Find(); err != nil {
		return
	}
	procWTSUnRegisterSession.Call(uintptr(hwnd))
}

// EnumDisplayMonitors walks the current monitor set. Callers re-run this on
// every display change rather than caching it for the life of the process.
func EnumDisplayMonitors() []HMONITOR {
	var mons []HMONITOR
	cb := syscall.NewCallback(func(hmon HMONITOR, hdc uintptr, lprc uintptr, data uintptr) uintptr {
		mons = append(mons, hmon)
		return 1 // keep enumerating
	})
	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return mons
}

// UTF16Ptr converts a Go string for Win32. Our call sites pass literals, so the
// embedded-NUL error case cannot occur.
func UTF16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}
