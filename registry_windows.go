//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// Settings live in the current user's registry:
//   HKCU\Software\Mandarino                          our flags
//   HKCU\Software\Microsoft\Windows\CurrentVersion\Run  "start with Windows"
// Nothing is written outside the current user's profile.

const (
	hkcu       = 0x80000001 // HKEY_CURRENT_USER
	keyRW      = 0x2001F    // KEY_READ | KEY_WRITE
	runKey     = `Software\Microsoft\Windows\CurrentVersion\Run`
	settingKey = `Software\Mandarino`
)

// regOpen opens (creating it if needed) a key under HKCU; 0 on failure.
func regOpen(path string) uintptr {
	var h uintptr
	if r, _, _ := pRegCreateKeyExW.Call(hkcu, up(utf16p(path)), 0, 0, 0, keyRW, 0, uintptr(unsafe.Pointer(&h)), 0); r != 0 {
		return 0
	}
	return h
}

// regGetFlag reads a DWORD flag from our settings key.
func regGetFlag(name string) bool {
	h := regOpen(settingKey)
	if h == 0 {
		return false
	}
	defer pRegCloseKey.Call(h)
	var v, size uint32 = 0, 4
	r, _, _ := pRegQueryValueExW.Call(h, up(utf16p(name)), 0, 0, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size)))
	return r == 0 && v != 0
}

// regSetFlag sets a DWORD flag to 1.
func regSetFlag(name string) {
	h := regOpen(settingKey)
	if h == 0 {
		return
	}
	defer pRegCloseKey.Call(h)
	v := uint32(1)
	pRegSetValueExW.Call(h, up(utf16p(name)), 0, 4 /*REG_DWORD*/, uintptr(unsafe.Pointer(&v)), 4)
}

// autostartEnabled reports whether Mandarino starts with Windows.
func autostartEnabled() bool {
	h := regOpen(runKey)
	if h == 0 {
		return false
	}
	defer pRegCloseKey.Call(h)
	r, _, _ := pRegQueryValueExW.Call(h, up(utf16p(appName)), 0, 0, 0, 0)
	return r == 0
}

// setAutostart adds or removes Mandarino from the programs started at log-in.
func setAutostart(on bool) {
	h := regOpen(runKey)
	if h == 0 {
		return
	}
	defer pRegCloseKey.Call(h)
	if !on {
		pRegDeleteValueW.Call(h, up(utf16p(appName)))
		return
	}
	exe, _ := os.Executable()
	val := syscall.StringToUTF16(`"` + exe + `"`)
	pRegSetValueExW.Call(h, up(utf16p(appName)), 0, 1 /*REG_SZ*/, uintptr(unsafe.Pointer(&val[0])), uintptr(len(val)*2))
}
