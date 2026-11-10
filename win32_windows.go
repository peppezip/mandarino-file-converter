//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Direct Win32 calls through the standard library's syscall package: no cgo and
// no third-party modules, so the Windows build is a plain "go build".
//
// Only what Mandarino needs is declared here: window creation and messages,
// low-level keyboard/mouse hooks, layered (per-pixel alpha) windows, the
// notification-area icon, the registry (start with Windows) and a few helpers.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	gdiplus  = syscall.NewLazyDLL("gdiplus.dll")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pSetWindowsHookExW             = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx           = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx                = user32.NewProc("CallNextHookEx")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pGetClassNameW                 = user32.NewProc("GetClassNameW")
	pGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	pGetGUIThreadInfo              = user32.NewProc("GetGUIThreadInfo")
	pFindWindowExW                 = user32.NewProc("FindWindowExW")
	pGetAncestor                   = user32.NewProc("GetAncestor")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pMonitorFromPoint              = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pUpdateLayeredWindow           = user32.NewProc("UpdateLayeredWindow")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                   = user32.NewProc("AppendMenuW")
	pTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	pDestroyMenu                   = user32.NewProc("DestroyMenu")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pMessageBoxW                   = user32.NewProc("MessageBoxW")
	pRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	pSetTimer                      = user32.NewProc("SetTimer")
	pKillTimer                     = user32.NewProc("KillTimer")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pTrackMouseEvent               = user32.NewProc("TrackMouseEvent")
	pGetWindowRect                 = user32.NewProc("GetWindowRect")
	pWindowFromPoint               = user32.NewProc("WindowFromPoint")
	pGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	pSetCursorPos                  = user32.NewProc("SetCursorPos")
	pRegisterClipboardFormatW      = user32.NewProc("RegisterClipboardFormatW")

	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pDeleteDC           = gdi32.NewProc("DeleteDC")

	pCreateMutexW     = kernel32.NewProc("CreateMutexW")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pGetTickCount     = kernel32.NewProc("GetTickCount")
	pGlobalLock       = kernel32.NewProc("GlobalLock")
	pGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	pGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	pGlobalFree       = kernel32.NewProc("GlobalFree")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW    = shell32.NewProc("ShellExecuteW")
	pDragQueryFileW   = shell32.NewProc("DragQueryFileW")

	pOleInitialize    = ole32.NewProc("OleInitialize")
	pRegisterDragDrop = ole32.NewProc("RegisterDragDrop")
	pRevokeDragDrop   = ole32.NewProc("RevokeDragDrop")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	pReleaseStgMedium = ole32.NewProc("ReleaseStgMedium")

	pRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	pRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	pRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey      = advapi32.NewProc("RegCloseKey")

	pGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")
)

// Win32 constants (names as in the Windows SDK).
const (
	WM_DESTROY         = 0x0002
	WM_TIMER           = 0x0113
	WM_COMMAND         = 0x0111
	WM_MOUSEMOVE       = 0x0200
	WM_LBUTTONDOWN     = 0x0201
	WM_LBUTTONUP       = 0x0202
	WM_RBUTTONDOWN     = 0x0204
	WM_RBUTTONUP       = 0x0205
	WM_MBUTTONDOWN     = 0x0207
	WM_XBUTTONDOWN     = 0x020B
	WM_LBUTTONDBLCLK   = 0x0203
	WM_MOUSELEAVE      = 0x02A3
	WM_KEYDOWN         = 0x0100
	WM_KEYUP           = 0x0101
	WM_SYSKEYDOWN      = 0x0104
	WM_SYSKEYUP        = 0x0105
	WM_MOUSEACTIVATE   = 0x0021
	WM_MOUSEWHEEL      = 0x020A
	WM_NCHITTEST       = 0x0084
	WM_NCMOUSEMOVE     = 0x00A0
	WM_NCLBUTTONDBLCLK = 0x00A3
	WM_NCRBUTTONUP     = 0x00A5
	WM_MOVING          = 0x0216
	HTCLIENT           = 1
	HTCAPTION          = 2
	MA_NOACTIVATE      = 3
	WM_APP             = 0x8000
	WM_USER            = 0x0400

	WS_POPUP          = 0x80000000
	WS_EX_LAYERED     = 0x00080000
	WS_EX_TOPMOST     = 0x00000008
	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_NOACTIVATE  = 0x08000000
	SW_HIDE           = 0
	SW_SHOWNOACTIVATE = 4
	HWND_TOPMOST      = ^uintptr(0) // (HWND)-1
	SWP_NOSIZE        = 0x0001
	SWP_NOMOVE        = 0x0002
	SWP_NOACTIVATE    = 0x0010
	SWP_SHOWWINDOW    = 0x0040

	WH_KEYBOARD_LL = 13
	WH_MOUSE_LL    = 14
	LLKHF_INJECTED = 0x10
	LLMHF_INJECTED = 0x01

	VK_SHIFT    = 0x10
	VK_LSHIFT   = 0xA0
	VK_RSHIFT   = 0xA1
	VK_CONTROL  = 0x11
	VK_LCONTROL = 0xA2
	VK_RCONTROL = 0xA3
	VK_RETURN   = 0x0D
	VK_BACK     = 0x08
	VK_ESCAPE   = 0x1B
	VK_TAB      = 0x09
	VK_SPACE    = 0x20
	VK_LEFT     = 0x25
	VK_UP       = 0x26
	VK_RIGHT    = 0x27
	VK_DOWN     = 0x28

	MF_STRING       = 0x0000
	MF_GRAYED       = 0x0001
	MF_CHECKED      = 0x0008
	MF_SEPARATOR    = 0x0800
	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100
	TPM_BOTTOMALIGN = 0x0020

	MB_OK            = 0x0
	MB_ICONINFO      = 0x40
	MB_ICONWARNING   = 0x30
	MB_TOPMOST       = 0x40000
	MB_SETFOREGROUND = 0x10000

	ULW_ALPHA    = 2
	AC_SRC_OVER  = 0
	AC_SRC_ALPHA = 1
	TME_LEAVE    = 2
)

// Win32 structures. Field order and types match the C definitions so that the
// Go layout (including padding on 64-bit) is identical.
type POINT struct{ X, Y int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }
type SIZE struct{ CX, CY int32 }
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type WNDCLASSEXW struct {
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

type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type MSLLHOOKSTRUCT struct {
	Pt          POINT
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type GUITHREADINFO struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcCaret       RECT
}

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type BLENDFUNCTION struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         GUID
	HBalloonIcon     uintptr
}

// utf16p converts a Go string to a NUL-terminated UTF-16 pointer.
func utf16p(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("?")
	}
	return p
}

// up turns a *uint16 into a uintptr argument.
func up(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }

// copyUTF16 copies s into a fixed-size UTF-16 buffer, truncating if needed.
func copyUTF16(dst []uint16, s string) {
	u := syscall.StringToUTF16(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
	for i := len(u); i < len(dst); i++ {
		dst[i] = 0
	}
}

// className returns the window class name of hwnd (e.g. "CabinetWClass" for File Explorer).
func className(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := pGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

// loword returns the low 16 bits of a message parameter.
func loword(x uintptr) uint32 { return uint32(x) & 0xFFFF }

// messageBox shows a standard message box on top of everything.
func messageBox(text, title string, flags uintptr) {
	pMessageBoxW.Call(0, up(utf16p(text)), up(utf16p(title)), flags|MB_TOPMOST|MB_SETFOREGROUND)
}

// tick returns the milliseconds since boot (GetTickCount).
func tick() uint32 {
	r, _, _ := pGetTickCount.Call()
	return uint32(r)
}
