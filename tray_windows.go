//go:build windows

package main

import (
	"unsafe"
)

// The orange icon in the notification area (next to the clock): its tooltip
// shows the progress, its menu holds the few settings, and it is also used to
// show notifications (Windows 10/11 turns them into toasts).

const nin_BALLOONUSERCLICK = WM_USER + 5 // the user clicked our notification

func trayData() NOTIFYICONDATAW {
	nid := NOTIFYICONDATAW{HWnd: app.hwnd, UID: 1}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	return nid
}

func trayAdd() {
	nid := trayData()
	nid.UFlags = 0x1 | 0x2 | 0x4 // NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = msgTray
	nid.HIcon = app.icon
	copyUTF16(nid.SzTip[:], appName+" – drag files, then Shift or Ctrl")
	pShellNotifyIconW.Call(0 /*NIM_ADD*/, uintptr(unsafe.Pointer(&nid)))
}

func trayTip(tip string) {
	nid := trayData()
	nid.UFlags = 0x4 // NIF_TIP
	copyUTF16(nid.SzTip[:], tip)
	pShellNotifyIconW.Call(1 /*NIM_MODIFY*/, uintptr(unsafe.Pointer(&nid)))
}

// balloon shows a notification.
func balloon(title, text string, isError bool) {
	nid := trayData()
	nid.UFlags = 0x10 // NIF_INFO
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], text)
	nid.DwInfoFlags = 0x1 // NIIF_INFO
	if isError {
		nid.DwInfoFlags = 0x3 // NIIF_ERROR
	}
	pShellNotifyIconW.Call(1 /*NIM_MODIFY*/, uintptr(unsafe.Pointer(&nid)))
}

func trayRemove() {
	nid := trayData()
	pShellNotifyIconW.Call(2 /*NIM_DELETE*/, uintptr(unsafe.Pointer(&nid)))
}

// onTrayEvent handles clicks on the icon and on notifications.
func onTrayEvent(lparam uintptr) {
	switch loword(lparam) {
	case WM_RBUTTONUP, WM_LBUTTONUP:
		trayMenu()
	case nin_BALLOONUSERCLICK:
		if app.lastOutput != "" {
			showInFolder(app.lastOutput)
		}
	}
}

const helpText = `How to use Mandarino

In File Explorer (or on the desktop) start dragging one or more files and,
while still holding the mouse button:

• press Shift to CONVERT them: the tangerine shows the formats;
• press Ctrl for ACTIONS: Bluetooth, Copy, Path (copy the path), Move to…

Move onto a slice (it lights up) and release the mouse button.
Press the other key to switch tangerine. Release anywhere else for a normal
drag & drop, or press Esc to cancel.

Move to…: rest on it and the tangerine shows your folders. Rest on a folder to
open it, "Up" goes back; drop on a folder (or on "Move here" in the middle).

Conversions never touch the originals: the new file appears next to them.
Everything happens on your PC, without internet.

Formats:
• Images: JPG, PNG, WebP, BMP, TIFF, GIF, ICO, PDF, 50%, Compress
  (HEIC and AVIF can be opened too)
• Video: MP4, MKV, MOV, WebM, AVI, animated GIF, extract MP3, Compress, Mute
• Audio: MP3, WAV, FLAC, M4A, OGG, Opus
• PDF: pages to PNG or JPG, Merge several PDFs, Split (choose the pages)`

func trayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(menu)
	add := func(id uintptr, text string, flags uintptr) {
		pAppendMenuW.Call(menu, flags, id, up(utf16p(text)))
	}
	add(0, appName+" "+appVersion+" – drag files, then Shift or Ctrl", MF_GRAYED)
	pAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	add(1, "How to use", MF_STRING)
	auto := uintptr(MF_STRING)
	if autostartEnabled() {
		auto |= MF_CHECKED
	}
	add(2, "Start with Windows", auto)
	if n := len(app.jobs); n > 0 {
		add(3, "Cancel running conversions ("+itoa(n)+")", MF_STRING)
	}
	if app.lastOutput != "" {
		add(4, "Show the last file created", MF_STRING)
	}
	pAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	add(9, "Quit", MF_STRING)

	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(app.hwnd) // required, or the menu does not close when clicking elsewhere
	cmd, _, _ := pTrackPopupMenu.Call(menu, TPM_RIGHTBUTTON|TPM_RETURNCMD|TPM_BOTTOMALIGN, uintptr(pt.X), uintptr(pt.Y), 0, app.hwnd, 0)
	pPostMessageW.Call(app.hwnd, 0, 0, 0) // WM_NULL, part of the same workaround
	switch cmd {
	case 1:
		messageBox(helpText, appName, MB_ICONINFO)
	case 2:
		setAutostart(!autostartEnabled())
	case 3:
		cancelAll()
	case 4:
		showInFolder(app.lastOutput)
	case 9:
		quit()
	}
}
