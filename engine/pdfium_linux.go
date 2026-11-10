//go:build linux && cgo

package engine

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// Generic trampoline: every pdfium function we use takes only integer or
// pointer arguments (at most 8), so on x86-64 and arm64 it can be called
// through a prototype with 8 uintptr_t parameters; extra arguments are ignored.
typedef uintptr_t (*fn8)(uintptr_t, uintptr_t, uintptr_t, uintptr_t,
                         uintptr_t, uintptr_t, uintptr_t, uintptr_t);

static uintptr_t mandarino_call(void *f, uintptr_t a0, uintptr_t a1, uintptr_t a2, uintptr_t a3,
                                uintptr_t a4, uintptr_t a5, uintptr_t a6, uintptr_t a7) {
	return ((fn8)f)(a0, a1, a2, a3, a4, a5, a6, a7);
}

// Called by pdfium while saving; forwards to Go (see goWriteBlock below).
extern int goWriteBlock(void *data, unsigned long size);
static int mandarino_write_block(void *self, const void *data, unsigned long size) {
	(void)self;
	return goWriteBlock((void *)data, size);
}
static uintptr_t mandarino_write_block_addr(void) { return (uintptr_t)&mandarino_write_block; }
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// Linux side of the pdfium bindings: libpdfium.so is opened with dlopen and
// its functions are called through a small C trampoline.

const libName = "libpdfium.so"

var linuxLib *linuxNative // library used by the write callback

type linuxNative struct {
	handle unsafe.Pointer
	funcs  map[string]unsafe.Pointer
}

func nativeLib() native { return &linuxNative{} }

func (n *linuxNative) open(path string, funcs []string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	h := C.dlopen(cpath, C.RTLD_NOW|C.RTLD_LOCAL)
	if h == nil {
		return errors.New(C.GoString(C.dlerror()))
	}
	n.handle = h
	n.funcs = map[string]unsafe.Pointer{}
	for _, f := range funcs {
		cf := C.CString(f)
		sym := C.dlsym(h, cf)
		C.free(unsafe.Pointer(cf))
		if sym == nil {
			return fmt.Errorf("missing function %s", f)
		}
		n.funcs[f] = sym
	}
	linuxLib = n
	return nil
}

func (n *linuxNative) call(fn string, args ...uintptr) uintptr {
	var a [8]C.uintptr_t
	for i, v := range args {
		a[i] = C.uintptr_t(v)
	}
	return uintptr(C.mandarino_call(n.funcs[fn], a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7]))
}

func (n *linuxNative) alloc(size int) uintptr { return uintptr(C.calloc(1, C.size_t(size))) }

func (n *linuxNative) free(p uintptr) {
	if p != 0 {
		C.free(unsafe.Pointer(p))
	}
}

func (n *linuxNative) copyIn(dst uintptr, src []byte) {
	if len(src) > 0 {
		C.memcpy(unsafe.Pointer(dst), unsafe.Pointer(&src[0]), C.size_t(len(src)))
	}
}

func (n *linuxNative) copyOut(src uintptr, size int) []byte {
	if size <= 0 {
		return nil
	}
	return C.GoBytes(unsafe.Pointer(src), C.int(size))
}

func (n *linuxNative) writeCallback() uintptr { return uintptr(C.mandarino_write_block_addr()) }

//export goWriteBlock
func goWriteBlock(data unsafe.Pointer, size C.ulong) C.int {
	onWriteBlock(linuxLib, uintptr(data), int(size))
	return 1
}
