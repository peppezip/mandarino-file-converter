//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Low-level keyboard and mouse hooks see every key press and mouse event of
// the session. They run on a dedicated OS thread with its own message loop,
// so a busy UI thread never makes the keyboard or the mouse lag. The
// callbacks only do cheap bookkeeping and post messages to the main window.
//
// What they look for:
//   - the gesture: a drag that started in File Explorer or on the desktop,
//     plus a press of Shift (convert) or Ctrl (actions) while the button is
//     still held (dragDetector);
//   - while the wheel is open: the end of the drag (a drop outside the wheel
//     closes it), Esc, clicks outside, and the keys of the page picker.
//
// Nothing is recorded or sent anywhere.

var (
	kbHookCB    = syscall.NewCallback(kbHookProc)
	mouseHookCB = syscall.NewCallback(mouseHookProc)
	drag        dragDetector // only used on the hook thread
	shiftHeld   bool         // to ignore keyboard auto-repeat
	ctrlHeld    bool
)

func hookThread() {
	runtime.LockOSThread()
	cx, _, _ := pGetSystemMetrics.Call(68) // SM_CXDRAG: how far the mouse must move to start a drag
	drag.threshold = int(cx)
	kb, _, _ := pSetWindowsHookExW.Call(WH_KEYBOARD_LL, kbHookCB, app.hinst, 0)
	ms, _, _ := pSetWindowsHookExW.Call(WH_MOUSE_LL, mouseHookCB, app.hinst, 0)
	if kb == 0 || ms == 0 {
		messageBox("Could not watch the mouse and keyboard: the drag + Shift gesture will not work.", appName, MB_ICONWARNING)
	}
	var m MSG // low-level hooks need a message loop on their thread
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
	}
	pUnhookWindowsHookEx.Call(kb)
	pUnhookWindowsHookEx.Call(ms)
}

// isFileWindow reports whether a top-level window shows files that can be
// dragged: a File Explorer window or the desktop.
func isFileWindow(root uintptr) bool {
	switch className(root) {
	case "CabinetWClass", "ExploreWClass", "Progman", "WorkerW":
		return true
	}
	return false
}

// rootAt returns the top-level window under screen point pt.
func rootAt(pt POINT) uintptr {
	h, _, _ := pWindowFromPoint.Call(uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32)
	if h == 0 {
		return 0
	}
	root, _, _ := pGetAncestor.Call(h, 2 /*GA_ROOT*/)
	return root
}

func kbHookProc(nCode int, wparam, lparam uintptr) uintptr {
	if nCode >= 0 {
		k := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lparam))
		if k.Flags&LLKHF_INJECTED == 0 { // ignore synthetic key presses
			down := wparam == WM_KEYDOWN || wparam == WM_SYSKEYDOWN
			isShift := k.VkCode == VK_LSHIFT || k.VkCode == VK_RSHIFT || k.VkCode == VK_SHIFT
			isCtrl := k.VkCode == VK_LCONTROL || k.VkCode == VK_RCONTROL || k.VkCode == VK_CONTROL

			// the gesture: Shift or Ctrl while dragging files (first press only, no auto-repeat)
			switch {
			case isShift && down && !shiftHeld:
				shiftHeld = true
				if drag.Trigger() {
					post(msgDragKey, uintptr(kindConvert), drag.sourceRoot)
				}
			case isCtrl && down && !ctrlHeld:
				ctrlHeld = true
				if drag.Trigger() {
					post(msgDragKey, uintptr(kindActions), drag.sourceRoot)
				}
			case isShift && !down:
				shiftHeld = false
			case isCtrl && !down:
				ctrlHeld = false
			}

			if !isShift && !isCtrl && wheelIsVisible() {
				if wheelInDrag() {
					// while dragging, keys belong to the drag: Esc cancels both
					if down && k.VkCode == VK_ESCAPE {
						post(msgClose, 0, 0)
					}
				} else if vkToKey(k.VkCode) != KeyNone {
					if down {
						post(msgKey, uintptr(k.VkCode), 0)
					}
					return 1 // the key belongs to the wheel (page picker): do not pass it on
				} else if down {
					post(msgClose, 0, 0) // any other key closes the wheel
				}
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, uintptr(nCode), wparam, lparam)
	return r
}

func mouseHookProc(nCode int, wparam, lparam uintptr) uintptr {
	if nCode >= 0 {
		m := (*MSLLHOOKSTRUCT)(unsafe.Pointer(lparam))
		switch wparam {
		case WM_MOUSEMOVE:
			drag.Move(int(m.Pt.X), int(m.Pt.Y))
		case WM_LBUTTONDOWN:
			root := rootAt(m.Pt)
			drag.ButtonDown(int(m.Pt.X), int(m.Pt.Y), isFileWindow(root), root)
			if wheelIsVisible() && !wheelInDrag() && !wheelContains(m.Pt.X, m.Pt.Y) {
				post(msgClose, 0, 0) // click outside the wheel
			}
		case WM_LBUTTONUP:
			drag.ButtonUp()
			if wheelInDrag() {
				// the files were dropped: on the wheel, its Drop handler runs the action;
				// anywhere else the normal drop happens and the wheel closes
				inside := uintptr(0)
				if wheelContains(m.Pt.X, m.Pt.Y) {
					inside = 1
				}
				post(msgDragRelease, inside, 0)
			}
		case WM_RBUTTONDOWN, WM_MBUTTONDOWN, WM_XBUTTONDOWN:
			if wheelIsVisible() && (wheelInDrag() || !wheelContains(m.Pt.X, m.Pt.Y)) {
				post(msgClose, 0, 0)
			}
		case WM_MOUSEWHEEL:
			if wheelInRange() && wheelContains(m.Pt.X, m.Pt.Y) {
				up := uintptr(0)
				if int16(m.MouseData>>16) > 0 {
					up = 1
				}
				post(msgScroll, up, 0)
				return 1 // the wheel changes the page number instead of scrolling Explorer
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, uintptr(nCode), wparam, lparam)
	return r
}
