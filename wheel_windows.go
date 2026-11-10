//go:build windows

package main

import (
	"math"
	"sync"
	"syscall"
	"unsafe"

	"mandarino/engine"
)

// The wheel window on Windows: a round, semi-transparent "layered" window that
// appears under the pointer without taking the focus away from File Explorer.
//
// It opens while the user drags files (drag + Shift, see hooks_windows.go) and
// is also an OLE drop target (droptarget_windows.go): the drag enters it,
// segments light up under the pointer and dropping on one runs that action.
// After a drop on "Split" (one PDF) it stays open as a normal clickable
// window for the page picker; keys reach it through the low-level keyboard
// hook, because it never takes the keyboard focus.

type wheelWin struct {
	hwnd     uintptr
	visible  bool
	m        *wheelModel
	fg       uintptr // foreground window when the wheel opened (closing trigger if it changes)
	cx, cy   int     // centre of the wheel on screen
	dragMode bool    // true while the files are still being dragged

	onPick  func(a engine.Action, files []string, cx, cy int)
	onOp    func(op string, paths []string, dest string)
	onError func(msg string)
}

// wheelShared is read by the hook thread to decide whether to swallow keys,
// clicks and mouse-wheel events, so it is protected by a mutex.
var wheelShared struct {
	sync.Mutex
	visible   bool
	rangeMode bool
	dragMode  bool
	cx, cy, r int
}

func wheelIsVisible() bool {
	wheelShared.Lock()
	defer wheelShared.Unlock()
	return wheelShared.visible
}

func wheelInDrag() bool {
	wheelShared.Lock()
	defer wheelShared.Unlock()
	return wheelShared.visible && wheelShared.dragMode
}

func wheelInRange() bool {
	wheelShared.Lock()
	defer wheelShared.Unlock()
	return wheelShared.visible && wheelShared.rangeMode
}

// wheelContains reports whether screen point (x, y) is on the wheel.
func wheelContains(x, y int32) bool {
	wheelShared.Lock()
	defer wheelShared.Unlock()
	return math.Hypot(float64(int(x)-wheelShared.cx), float64(int(y)-wheelShared.cy)) <= float64(wheelShared.r)
}

var (
	wheelWndProcCB = syscall.NewCallback(wheelWndProc)
	theWheel       *wheelWin
)

func newWheelWin(hinst uintptr) *wheelWin {
	w := &wheelWin{}
	cls := utf16p("MandarinoWheel")
	cursor, _, _ := pLoadCursorW.Call(0, 32649) // IDC_HAND
	wc := WNDCLASSEXW{LpfnWndProc: wheelWndProcCB, HInstance: hinst, HCursor: cursor, LpszClassName: cls}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	w.hwnd, _, _ = pCreateWindowExW.Call(
		WS_EX_LAYERED|WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE,
		up(cls), up(utf16p("Mandarino")), WS_POPUP,
		0, 0, 10, 10, 0, 0, hinst, 0)
	theWheel = w
	registerDropTarget(w.hwnd)
	return w
}

// monitorDPI returns the scale factor (1 = 96 dpi) of the monitor at (x, y).
func monitorDPI(x, y int32) float64 {
	pt := uintptr(uint32(x)) | uintptr(uint32(y))<<32 // POINT passed by value in one register
	mon, _, _ := pMonitorFromPoint.Call(pt, 2 /*MONITOR_DEFAULTTONEAREST*/)
	if pGetDpiForMonitor.Find() == nil {
		var dx, dy uint32
		if r, _, _ := pGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx > 0 {
			return float64(dx) / 96
		}
	}
	return 1
}

// workArea returns the usable area (without the taskbar) of the monitor at (x, y).
func workArea(x, y int32) RECT {
	pt := uintptr(uint32(x)) | uintptr(uint32(y))<<32
	mon, _, _ := pMonitorFromPoint.Call(pt, 2)
	mi := MONITORINFO{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	return mi.RcWork
}

// clampCentre keeps a square of the given size fully on the monitor.
func clampCentre(cx, cy, size int) (int, int) {
	wa := workArea(int32(cx), int32(cy))
	half := size / 2
	return min(max(cx, int(wa.Left)+half), int(wa.Right)-half), min(max(cy, int(wa.Top)+half), int(wa.Bottom)-half)
}

// showForDrag opens a pale, "pending" wheel of the given kind near the pointer
// while files are being dragged. The real entries appear as soon as the drag
// enters the wheel (dragEnter), which happens right away (see the nudge below).
// Pressing the other modifier while the wheel is open switches wheel.
func (w *wheelWin) showForDrag(kind wheelKind, fg uintptr) {
	if w.visible && w.dragMode {
		w.apply(w.m.SetKind(kind))
		return
	}
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	w.m = newWheelModel(kind, nil, monitorDPI(pt.X, pt.Y))
	w.m.Dragging = true
	w.fg = fg
	// centred on the pointer, but moved inwards near the edges of the screen:
	// better off-centre than cut
	w.cx, w.cy = clampCentre(int(pt.X), int(pt.Y), w.m.size)
	w.dragMode = true
	w.visible = true
	w.publish()
	w.render()
	pSetWindowPos.Call(w.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE|SWP_SHOWWINDOW)
	pShowWindow.Call(w.hwnd, SW_SHOWNOACTIVATE)
	// A tiny pointer nudge makes Windows re-check what is under the pointer
	// right away, so DragEnter (with the file list) arrives without waiting
	// for the user to move the mouse.
	pSetCursorPos.Call(uintptr(pt.X+1), uintptr(pt.Y))
	pSetCursorPos.Call(uintptr(pt.X), uintptr(pt.Y))
}

// toWindow converts screen coordinates to wheel-window coordinates.
func (w *wheelWin) toWindow(x, y int) (int, int) {
	return x - (w.cx - w.m.size/2), y - (w.cy - w.m.size/2)
}

// dropEffect tells Windows what a drop would do right now: a "+" (copy) badge
// over an entry, a move badge over a folder of "Move to…", nothing elsewhere.
func (w *wheelWin) dropEffect() uint32 {
	_, ok, move := w.m.HoverInfo()
	switch {
	case !ok:
		return dropEffectNone
	case move:
		return dropEffectMove
	}
	return dropEffectCopy
}

// dragEnter receives the dragged paths the first time the drag enters the wheel.
func (w *wheelWin) dragEnter(paths []string, x, y int) uint32 {
	if w.m.Pending() {
		w.apply(w.m.SetPaths(paths)) // now we know the entries: resize and repaint
	}
	return w.dragOver(x, y)
}

// dragOver highlights the entry under the pointer.
func (w *wheelWin) dragOver(x, y int) uint32 {
	wx, wy := w.toWindow(x, y)
	w.apply(w.m.Move(wx, wy))
	return w.dropEffect()
}

// hoverDesc is the text shown under the dragged files ("" = Windows' default).
func (w *wheelWin) hoverDesc() string {
	if w.m == nil {
		return ""
	}
	d, _, _ := w.m.HoverInfo()
	return d
}

// dragLeave: the pointer left the wheel while still dragging. The wheel stays
// open (a drop elsewhere closes it, see hooks_windows.go) but nothing is highlighted.
func (w *wheelWin) dragLeave() {
	pKillTimer.Call(app.hwnd, timerDwell)
	w.apply(w.m.Leave())
}

// drop runs what is under the pointer (see wheelModel.Drop).
func (w *wheelWin) drop(paths []string, x, y int) {
	pKillTimer.Call(app.hwnd, timerDropWatch)
	pKillTimer.Call(app.hwnd, timerDwell)
	w.dragMode = false
	if w.m.Pending() {
		w.m.SetPaths(paths)
	}
	w.publish()
	wx, wy := w.toWindow(x, y)
	w.apply(w.m.Drop(wx, wy))
}

// dwell is called by the dwell timer: open what the pointer rests on.
func (w *wheelWin) dwell() {
	if w.visible {
		w.apply(w.m.DwellFire())
	}
}

// publish copies what the hook thread needs into wheelShared.
func (w *wheelWin) publish() {
	wheelShared.Lock()
	wheelShared.visible = w.visible
	wheelShared.rangeMode = w.visible && w.m != nil && w.m.mode == modeRange
	wheelShared.dragMode = w.visible && w.dragMode
	wheelShared.cx, wheelShared.cy = w.cx, w.cy
	if w.m != nil {
		wheelShared.r = w.m.outerR
	}
	wheelShared.Unlock()
}

func (w *wheelWin) hide() {
	if !w.visible {
		return
	}
	w.visible = false
	w.dragMode = false
	w.publish()
	pShowWindow.Call(w.hwnd, SW_HIDE)
}

// render repaints the wheel centred on (cx, cy).
func (w *wheelWin) render() {
	size := w.m.size
	layeredPaint(w.hwnd, size, &POINT{int32(w.cx - size/2), int32(w.cy - size/2)}, w.m.Paint)
}

// apply carries out what the model asked for.
func (w *wheelWin) apply(out Outcome) {
	if out.Redraw || out.Resize || out.Close || out.Pick != nil || out.Op != "" {
		pKillTimer.Call(app.hwnd, timerDwell) // the pointer moved to something else
	}
	if out.Dwell {
		pSetTimer.Call(app.hwnd, timerDwell, dwellMillis, 0)
	}
	switch {
	case out.Pick != nil:
		files := w.m.files
		w.hide()
		if w.onPick != nil {
			w.onPick(*out.Pick, files, w.cx, w.cy)
		}
	case out.Op != "":
		w.hide()
		if w.onOp != nil {
			w.onOp(out.Op, out.Paths, out.Dest)
		}
	case out.Error != "":
		w.hide()
		if w.onError != nil {
			w.onError(out.Error)
		}
	case out.Close:
		w.hide()
	case out.Resize:
		// keep the whole fruit on screen (it may grow when its entries arrive)
		w.cx, w.cy = clampCentre(w.cx, w.cy, w.m.size)
		w.publish()
		w.render()
	case out.Redraw:
		w.publish()
		w.render()
	}
}

// key handles a key forwarded by the keyboard hook.
func (w *wheelWin) key(vk uintptr) {
	if w.visible {
		w.apply(w.m.Key(vkToKey(uint32(vk))))
	}
}

// scroll handles a mouse-wheel event forwarded by the mouse hook.
func (w *wheelWin) scroll(up bool) {
	if w.visible {
		w.apply(w.m.Scroll(up))
	}
}

// vkToKey maps Windows virtual-key codes to the shared Key codes.
func vkToKey(vk uint32) Key {
	switch vk {
	case VK_LEFT:
		return KeyLeft
	case VK_RIGHT:
		return KeyRight
	case VK_UP:
		return KeyUp
	case VK_DOWN:
		return KeyDown
	case VK_TAB:
		return KeyTab
	case VK_RETURN:
		return KeyEnter
	case VK_SPACE:
		return KeySpace
	case VK_ESCAPE:
		return KeyEsc
	case VK_BACK:
		return KeyBackspace
	}
	switch {
	case vk >= '0' && vk <= '9':
		return KeyDigit0 + Key(vk-'0')
	case vk >= 0x60 && vk <= 0x69: // numeric keypad
		return KeyDigit0 + Key(vk-0x60)
	}
	return KeyNone
}

// lparamXY extracts signed client coordinates from a mouse message.
func lparamXY(l uintptr) (int, int) {
	return int(int16(uint16(l))), int(int16(uint16(l >> 16)))
}

func wheelWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	w := theWheel
	if w != nil && hwnd == w.hwnd && w.visible {
		switch msg {
		case WM_MOUSEACTIVATE:
			return MA_NOACTIVATE // clicking the wheel must not steal the focus
		case WM_MOUSEMOVE:
			x, y := lparamXY(lparam)
			w.apply(w.m.Move(x, y))
			tme := TRACKMOUSEEVENT{DwFlags: TME_LEAVE, HwndTrack: hwnd}
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme))) // ask for WM_MOUSELEAVE
			return 0
		case WM_MOUSELEAVE:
			w.apply(w.m.Leave())
			return 0
		case WM_LBUTTONUP:
			x, y := lparamXY(lparam)
			w.apply(w.m.Click(x, y))
			return 0
		case WM_RBUTTONUP:
			w.hide()
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}
