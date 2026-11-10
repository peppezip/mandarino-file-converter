//go:build windows

package main

import (
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Minimal COM helpers. COM objects are called directly through their vtables
// (see comCall), so no COM library or code generator is needed. Vtable
// indexes come from the Windows SDK headers (objidl.h, oleidl.h, shobjidl_core.h).

// mustGUID parses "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX" into a GUID.
func mustGUID(s string) GUID {
	s = strings.Trim(s, "{}")
	p := strings.Split(s, "-")
	h := func(x string) uint64 { v, _ := strconv.ParseUint(x, 16, 64); return v }
	g := GUID{Data1: uint32(h(p[0])), Data2: uint16(h(p[1])), Data3: uint16(h(p[2]))}
	tail := p[3] + p[4]
	for i := 0; i < 8; i++ {
		g.Data4[i] = byte(h(tail[i*2 : i*2+2]))
	}
	return g
}

var (
	IID_IUnknown          = mustGUID("00000000-0000-0000-C000-000000000046")
	IID_IDropTarget       = mustGUID("00000122-0000-0000-C000-000000000046")
	CLSID_DragDropHelper  = mustGUID("4657278A-411B-11D2-839A-00C04FD918D0")
	IID_IDropTargetHelper = mustGUID("4657278B-411B-11D2-839A-00C04FD918D0")
)

// FORMATETC and STGMEDIUM describe data exchanged through IDataObject.
type FORMATETC struct {
	CfFormat uint16
	Ptd      uintptr
	DwAspect uint32
	Lindex   int32
	Tymed    uint32
}

type STGMEDIUM struct {
	Tymed          uint32
	Data           uintptr
	PUnkForRelease uintptr
}

// comCall calls method number idx of the vtable of COM object obj
// (0-2 are QueryInterface, AddRef and Release on every interface).
func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

// failed reports whether an HRESULT is an error (negative).
func failed(hr uintptr) bool { return int32(hr) < 0 }

// comRelease releases a COM pointer (nil-safe).
func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2)
	}
}

// dataObjectPaths reads the files and folders carried by a drag & drop
// (the CF_HDROP format of the IDataObject).
func dataObjectPaths(dobj uintptr) []string {
	if dobj == 0 {
		return nil
	}
	fe := FORMATETC{CfFormat: 15 /*CF_HDROP*/, DwAspect: 1 /*DVASPECT_CONTENT*/, Lindex: -1, Tymed: 1 /*TYMED_HGLOBAL*/}
	var med STGMEDIUM
	if failed(comCall(dobj, 3 /*IDataObject::GetData*/, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med)))) {
		return nil // not files (text, a link from a browser...)
	}
	defer pReleaseStgMedium.Call(uintptr(unsafe.Pointer(&med)))
	if p, _, _ := pGlobalLock.Call(med.Data); p == 0 {
		return nil
	}
	defer pGlobalUnlock.Call(med.Data)
	n, _, _ := pDragQueryFileW.Call(med.Data, 0xFFFFFFFF, 0, 0) // number of files
	var files []string
	buf := make([]uint16, 32768)
	for i := uintptr(0); i < n; i++ {
		l, _, _ := pDragQueryFileW.Call(med.Data, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if l == 0 {
			continue
		}
		files = append(files, syscall.UTF16ToString(buf[:l]))
	}
	return files
}
