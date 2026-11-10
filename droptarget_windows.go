//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// The wheel window is an OLE drop target. While the user drags files from
// File Explorer, Windows asks the window under the pointer what would happen
// on drop (DragEnter / DragOver), and finally calls Drop on it. Because the
// wheel opens right under the pointer, the drag "enters" it immediately:
//
//   - DragEnter gives us the dragged files (so the wheel can show the right formats);
//   - DragOver gives the pointer position (to highlight the segment under it);
//   - Drop tells us the segment chosen.
//
// We always answer DROPEFFECT_NONE to the actual drop, so File Explorer never
// moves or copies anything: the files stay where they are and Mandarino
// creates the converted copy next to them.
//
// IDropTarget is implemented in Go as a static COM object: a struct whose
// first field points to a table of callbacks created with syscall.NewCallback.

const (
	dropEffectNone = 0
	dropEffectCopy = 1
	dropEffectMove = 2
)

type dropTarget struct {
	vtbl *[7]uintptr // QueryInterface, AddRef, Release, DragEnter, DragOver, DragLeave, Drop
	refs int32
}

var (
	dropVtbl = [7]uintptr{
		syscall.NewCallback(dtQueryInterface),
		syscall.NewCallback(dtAddRef),
		syscall.NewCallback(dtRelease),
		syscall.NewCallback(dtDragEnter),
		syscall.NewCallback(dtDragOver),
		syscall.NewCallback(dtDragLeave),
		syscall.NewCallback(dtDrop),
	}
	// a global variable: the object must never move nor be collected
	theDropTarget = &dropTarget{vtbl: &dropVtbl, refs: 1}

	// IDropTargetHelper draws Explorer's drag image (the file thumbnails)
	// over our window too; without it the image would vanish over the wheel.
	dropHelper uintptr

	// the data object of the drag in progress (kept between DragEnter and
	// DragLeave/Drop to update the text under the drag image)
	dragData          uintptr
	lastDesc          string
	cfDropDescription uintptr
)

// registerDropTarget makes hwnd accept drag & drop (OleInitialize must have been called).
func registerDropTarget(hwnd uintptr) {
	pRegisterDragDrop.Call(hwnd, uintptr(unsafe.Pointer(theDropTarget)))
	pCoCreateInstance.Call(uintptr(unsafe.Pointer(&CLSID_DragDropHelper)), 0, 1, /*CLSCTX_INPROC_SERVER*/
		uintptr(unsafe.Pointer(&IID_IDropTargetHelper)), uintptr(unsafe.Pointer(&dropHelper)))
}

func revokeDropTarget(hwnd uintptr) {
	pRevokeDragDrop.Call(hwnd)
	comRelease(dropHelper)
	dropHelper = 0
}

// setDropDescription changes the label Explorer shows under the dragged files
// ("Convert to PNG" instead of the generic "Copy"). An empty text restores the default.
func setDropDescription(text string, move bool) {
	key := text
	if move {
		key += "\x00move"
	}
	if dragData == 0 || key == lastDesc {
		return
	}
	lastDesc = key
	if cfDropDescription == 0 {
		cfDropDescription, _, _ = pRegisterClipboardFormatW.Call(up(utf16p("DropDescription")))
	}
	// struct DROPDESCRIPTION { int type; WCHAR szMessage[260]; WCHAR szInsert[260]; }
	type dropDescription struct {
		Type    int32
		Message [260]uint16
		Insert  [260]uint16
	}
	var dd dropDescription
	dd.Type = -1 // DROPIMAGE_INVALID: back to the default label
	if text != "" {
		dd.Type = 1 // DROPIMAGE_COPY (the "+" badge)
		if move {
			dd.Type = 2 // DROPIMAGE_MOVE (the arrow badge)
		}
		copyUTF16(dd.Message[:], "%1")
		copyUTF16(dd.Insert[:], text) // the inserted part is highlighted
	}
	size := unsafe.Sizeof(dd)
	h, _, _ := pGlobalAlloc.Call(0x0002 /*GMEM_MOVEABLE*/, size)
	if h == 0 {
		return
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), size), unsafe.Slice((*byte)(unsafe.Pointer(&dd)), size))
	pGlobalUnlock.Call(h)
	fe := FORMATETC{CfFormat: uint16(cfDropDescription), DwAspect: 1, Lindex: -1, Tymed: 1 /*TYMED_HGLOBAL*/}
	med := STGMEDIUM{Tymed: 1, Data: h}
	// IDataObject::SetData(fe, medium, fRelease = TRUE): the data object now owns h
	if failed(comCall(dragData, 7, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med)), 1)) {
		pGlobalFree.Call(h)
	}
}

// endDrag clears the label and releases the data object.
func endDrag() {
	if dragData != 0 {
		setDropDescription("", false)
		comRelease(dragData)
		dragData = 0
	}
	lastDesc = ""
}

// pointFromPOINTL unpacks a POINTL passed by value (x in the low 32 bits, y in the high 32 bits).
func pointFromPOINTL(pt uintptr) (int32, int32) {
	return int32(uint32(pt)), int32(uint32(pt >> 32))
}

func setEffect(p uintptr, e uint32) {
	if p != 0 {
		*(*uint32)(unsafe.Pointer(p)) = e
	}
}

func dtQueryInterface(this, riid, ppv uintptr) uintptr {
	iid := *(*GUID)(unsafe.Pointer(riid))
	if iid == IID_IUnknown || iid == IID_IDropTarget {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		dtAddRef(this)
		return 0 // S_OK
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return 0x80004002 // E_NOINTERFACE
}

func dtAddRef(this uintptr) uintptr {
	t := (*dropTarget)(unsafe.Pointer(this))
	t.refs++
	return uintptr(t.refs)
}

func dtRelease(this uintptr) uintptr {
	t := (*dropTarget)(unsafe.Pointer(this))
	if t.refs > 1 { // static object: never freed
		t.refs--
	}
	return uintptr(t.refs)
}

// HRESULT DragEnter(IDataObject* data, DWORD keys, POINTL pt, DWORD* effect)
func dtDragEnter(this, data, keys, pt, effect uintptr) uintptr {
	x, y := pointFromPOINTL(pt)
	endDrag()
	if data != 0 {
		comCall(data, 1) // AddRef: kept until DragLeave/Drop
		dragData = data
	}
	e := uint32(dropEffectNone)
	if app.wheel.visible {
		e = app.wheel.dragEnter(dataObjectPaths(data), int(x), int(y))
		setDropDescription(app.wheel.hoverDesc(), e == dropEffectMove)
	}
	setEffect(effect, e)
	if dropHelper != 0 {
		p := POINT{x, y}
		comCall(dropHelper, 3 /*DragEnter*/, app.wheel.hwnd, data, uintptr(unsafe.Pointer(&p)), uintptr(e))
	}
	return 0
}

// HRESULT DragOver(DWORD keys, POINTL pt, DWORD* effect)
func dtDragOver(this, keys, pt, effect uintptr) uintptr {
	x, y := pointFromPOINTL(pt)
	e := uint32(dropEffectNone)
	if app.wheel.visible {
		e = app.wheel.dragOver(int(x), int(y))
		setDropDescription(app.wheel.hoverDesc(), e == dropEffectMove)
	}
	setEffect(effect, e)
	if dropHelper != 0 {
		p := POINT{x, y}
		comCall(dropHelper, 5 /*DragOver*/, uintptr(unsafe.Pointer(&p)), uintptr(e))
	}
	return 0
}

// HRESULT DragLeave()
func dtDragLeave(this uintptr) uintptr {
	if app.wheel.visible {
		app.wheel.dragLeave()
	}
	endDrag()
	if dropHelper != 0 {
		comCall(dropHelper, 4 /*DragLeave*/)
	}
	return 0
}

// HRESULT Drop(IDataObject* data, DWORD keys, POINTL pt, DWORD* effect)
func dtDrop(this, data, keys, pt, effect uintptr) uintptr {
	x, y := pointFromPOINTL(pt)
	if dropHelper != 0 {
		p := POINT{x, y}
		comCall(dropHelper, 6 /*Drop*/, data, uintptr(unsafe.Pointer(&p)), dropEffectNone)
	}
	setEffect(effect, dropEffectNone) // File Explorer must not move or copy anything
	endDrag()
	if app.wheel.visible {
		app.wheel.drop(dataObjectPaths(data), int(x), int(y))
	}
	return 0
}
