//go:build windows

package engine

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// Windows side of the pdfium bindings: pdfium.dll is loaded with LoadLibrary
// (syscall.LazyDLL) and called through syscall, no cgo needed.

const libName = "pdfium.dll"

var (
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	rtlMoveMemory = kernel32.NewProc("RtlMoveMemory")
	globalAlloc   = kernel32.NewProc("GlobalAlloc")
	globalFree    = kernel32.NewProc("GlobalFree")

	winCallbackOnce sync.Once
	winCallback     uintptr
	winLib          *winNative // library used by the write callback
)

type winNative struct {
	procs map[string]*syscall.LazyProc
}

func nativeLib() native { return &winNative{} }

func (n *winNative) open(path string, funcs []string) error {
	dll := syscall.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return err
	}
	n.procs = map[string]*syscall.LazyProc{}
	for _, f := range funcs {
		p := dll.NewProc(f)
		if err := p.Find(); err != nil {
			return fmt.Errorf("missing function %s", f)
		}
		n.procs[f] = p
	}
	winLib = n
	return nil
}

func (n *winNative) call(fn string, args ...uintptr) uintptr {
	r, _, _ := n.procs[fn].Call(args...)
	return r
}

func (n *winNative) alloc(size int) uintptr {
	r, _, _ := globalAlloc.Call(0 /* GMEM_FIXED */, uintptr(size))
	return r
}

func (n *winNative) free(p uintptr) {
	if p != 0 {
		globalFree.Call(p)
	}
}

func (n *winNative) copyIn(dst uintptr, src []byte) {
	if len(src) > 0 {
		rtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)))
	}
}

func (n *winNative) copyOut(src uintptr, size int) []byte {
	if size <= 0 {
		return nil
	}
	b := make([]byte, size)
	rtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), src, uintptr(size))
	return b
}

func (n *winNative) writeCallback() uintptr {
	winCallbackOnce.Do(func() {
		// int WriteBlock(FPDF_FILEWRITE* pThis, const void* pData, unsigned long size)
		// "unsigned long" is 32 bits on Windows, so the upper half of size is garbage.
		winCallback = syscall.NewCallback(func(this, data, size uintptr) uintptr {
			onWriteBlock(winLib, data, int(uint32(size)))
			return 1
		})
	})
	return winCallback
}
