//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// The first level of the "Move to…" folder browser on Windows: the user's
// known folders (they may live in OneDrive, so they are asked to Windows) and
// the drives.

var (
	pSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
	pGetLogicalDrives     = kernel32.NewProc("GetLogicalDrives")
	pGetFileAttributesW   = kernel32.NewProc("GetFileAttributesW")
)

func init() {
	placesFn = windowsPlaces
	hiddenFn = func(path, name string) bool {
		if strings.HasPrefix(name, "$") || strings.HasPrefix(name, ".") {
			return true // $Recycle.Bin, .git...
		}
		a, _, _ := pGetFileAttributesW.Call(up(utf16p(path)))
		const hidden, system, invalid = 0x2, 0x4, 0xFFFFFFFF
		return uint32(a) != invalid && uint32(a)&(hidden|system) != 0
	}
}

// knownFolder returns the path of a known folder (Desktop, Documents...).
func knownFolder(id string) string {
	g := mustGUID(id)
	var p *uint16
	if r, _, _ := pSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&g)), 0, 0, uintptr(unsafe.Pointer(&p))); r != 0 || p == nil {
		return ""
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return utf16PtrToString(p)
}

func windowsPlaces() []folderEntry {
	var list []folderEntry
	for _, k := range []struct{ name, id string }{
		{"Desktop", "B4BFCC3A-DB2C-424C-B029-7FE99A87C641"},
		{"Documents", "FDD39AD0-238F-46AF-ADB4-6C85480369C7"},
		{"Downloads", "374DE290-123F-4565-9164-39C4925E467B"},
		{"Pictures", "33E28130-4E1E-4676-835A-98395C3BC3BB"},
		{"Videos", "18989B1D-99B5-455B-841C-AB7C74E4DDFC"},
		{"Music", "4BD8D571-6D19-48D3-BE97-422220080E43"},
	} {
		if p := knownFolder(k.id); p != "" {
			list = append(list, folderEntry{name: k.name, path: p})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		list = append(list, folderEntry{name: "Home", path: home})
	}
	mask, _, _ := pGetLogicalDrives.Call()
	for i := 0; i < 26; i++ {
		if mask&(1<<i) != 0 {
			letter := string(rune('A' + i))
			list = append(list, folderEntry{name: letter + ":", path: letter + `:\`})
		}
	}
	return list
}

// utf16PtrToString reads a NUL-terminated UTF-16 string allocated by Windows.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	var s []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 2) {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		s = append(s, c)
	}
	return syscall.UTF16ToString(s)
}
