//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
	"unsafe"
)

// The actions of the Ctrl wheel on Windows.
//
//   - "path": the full path(s) as text on the clipboard;
//   - "copy": the files on the clipboard exactly like Ctrl+C in File Explorer,
//     so Ctrl+V pastes them in any folder, e-mail or chat;
//   - "move": moved with the standard Windows file operation (progress window,
//     "replace or skip" questions, undo with Ctrl+Z in Explorer);
//   - "bt":   handed to the Windows Bluetooth file transfer wizard, which asks
//     which device to send them to (like "Send to > Bluetooth device"); without
//     Bluetooth support, the Bluetooth settings open instead.

var (
	pOpenClipboard    = user32.NewProc("OpenClipboard")
	pCloseClipboard   = user32.NewProc("CloseClipboard")
	pEmptyClipboard   = user32.NewProc("EmptyClipboard")
	pSetClipboardData = user32.NewProc("SetClipboardData")
	pSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

// runFileOp is called by the wheel. The work is queued on the UI thread, so it
// never runs inside the OLE drop callback (File Explorer is waiting for it).
func runFileOp(op string, paths []string, dest string) {
	runOnUI(func() {
		switch op {
		case "path":
			text := strings.Join(paths, "\r\n")
			if err := setClipboard(map[uintptr][]byte{13 /*CF_UNICODETEXT*/ : utf16z(text)}); err != nil {
				balloon("Could not copy the path", err.Error(), true)
				return
			}
			balloon("Path copied", text, false)
		case "copy":
			if err := copyFilesToClipboard(paths); err != nil {
				balloon("Could not copy", err.Error(), true)
				return
			}
			balloon("Copied", fmt.Sprintf("%s: paste with Ctrl+V wherever you like.", countItems(paths)), false)
		case "move":
			go moveFiles(paths, dest)
		case "bt":
			sendBluetooth(paths)
		}
	})
}

// countItems returns "photo.jpg" or "3 items".
func countItems(paths []string) string {
	if len(paths) == 1 {
		return filepath.Base(paths[0])
	}
	return fmt.Sprintf("%d items", len(paths))
}

// utf16z encodes s as NUL-terminated UTF-16 bytes.
func utf16z(s string) []byte {
	u := utf16.Encode([]rune(s + "\x00"))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

// setClipboard replaces the clipboard content with the given formats.
func setClipboard(formats map[uintptr][]byte) error {
	opened := false
	for i := 0; i < 10 && !opened; i++ { // another program may be holding it for a moment
		r, _, _ := pOpenClipboard.Call(app.hwnd)
		opened = r != 0
		if !opened {
			sleepMs(30)
		}
	}
	if !opened {
		return fmt.Errorf("the clipboard is busy")
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	for format, data := range formats {
		h, _, _ := pGlobalAlloc.Call(0x0002 /*GMEM_MOVEABLE*/, uintptr(len(data)))
		if h == 0 {
			return fmt.Errorf("out of memory")
		}
		p, _, _ := pGlobalLock.Call(h)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(data)), data)
		pGlobalUnlock.Call(h)
		if r, _, _ := pSetClipboardData.Call(format, h); r == 0 {
			pGlobalFree.Call(h) // the clipboard did not take ownership
		}
	}
	return nil
}

// copyFilesToClipboard puts the files on the clipboard as CF_HDROP
// (struct DROPFILES followed by a double-NUL-terminated list of paths).
func copyFilesToClipboard(paths []string) error {
	hdrop := make([]byte, 20) // DROPFILES { DWORD pFiles; POINT pt; BOOL fNC; BOOL fWide; }
	binary.LittleEndian.PutUint32(hdrop[0:], 20)
	binary.LittleEndian.PutUint32(hdrop[16:], 1) // wide characters
	for _, p := range paths {
		hdrop = append(hdrop, utf16z(p)...)
	}
	hdrop = append(hdrop, 0, 0)
	effect := make([]byte, 4)
	binary.LittleEndian.PutUint32(effect, dropEffectCopy) // paste = copy, not move
	cfEffect, _, _ := pRegisterClipboardFormatW.Call(up(utf16p("Preferred DropEffect")))
	return setClipboard(map[uintptr][]byte{15 /*CF_HDROP*/ : hdrop, cfEffect: effect})
}

// doubleNul builds the "a\0b\0\0" lists used by SHFileOperation.
func doubleNul(list []string) *uint16 {
	var u []uint16
	for _, s := range list {
		u = append(u, utf16.Encode([]rune(s))...)
		u = append(u, 0)
	}
	u = append(u, 0)
	return &u[0]
}

// moveFiles moves paths into dest with the standard Windows dialogs. It runs
// on its own thread because the operation (and its questions) can take a while.
func moveFiles(paths []string, dest string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pOleInitialize.Call(0)
	type shFileOpStruct struct {
		Hwnd                  uintptr
		WFunc                 uint32
		PFrom                 *uint16
		PTo                   *uint16
		FFlags                uint16
		FAnyOperationsAborted int32
		HNameMappings         uintptr
		LpszProgressTitle     *uint16
	}
	op := shFileOpStruct{
		WFunc:  1, // FO_MOVE
		PFrom:  doubleNul(paths),
		PTo:    doubleNul([]string{dest}),
		FFlags: 0x0040 | 0x0200, // FOF_ALLOWUNDO | FOF_NOCONFIRMMKDIR
	}
	r, _, _ := pSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	runOnUI(func() {
		switch {
		case r != 0:
			balloon("Could not move", fmt.Sprintf("%s could not be moved to %s.", countItems(paths), displayName(dest)), true)
		case op.FAnyOperationsAborted != 0:
			balloon("Move cancelled", "Some items were not moved.", true)
		default:
			app.lastOutput = filepath.Join(dest, filepath.Base(paths[len(paths)-1]))
			balloon("Moved", fmt.Sprintf("%s moved to %s.\nClick to open the folder.", countItems(paths), displayName(dest)), false)
		}
	})
}

// sendBluetooth opens the Bluetooth file transfer wizard with the files.
func sendBluetooth(paths []string) {
	fsquirt := filepath.Join(os.Getenv("SystemRoot"), "System32", "fsquirt.exe")
	if _, err := os.Stat(fsquirt); err == nil {
		// same as dropping the files on "Send to > Bluetooth device"
		if exec.Command(fsquirt, paths...).Start() == nil {
			return
		}
	}
	// no Bluetooth file transfer on this PC: open the Bluetooth settings
	pShellExecuteW.Call(0, up(utf16p("open")), up(utf16p("ms-settings:bluetooth")), 0, 0, 1)
	balloon("Bluetooth", "Turn on Bluetooth and pair your device, then try again.", false)
}
