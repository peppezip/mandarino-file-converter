//go:build windows || (linux && cgo)

package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// PDF support through pdfium, the PDF engine of Google Chrome
// (prebuilt binaries: https://github.com/bblanchon/pdfium-binaries).
//
// The library is loaded at run time from the program folder, so Mandarino
// still starts (without PDF features) if it is missing. Each platform provides
// a tiny "native" layer (pdfium_windows.go, pdfium_linux.go) that can load the
// library, call a function by name with integer/pointer arguments, and manage
// native memory. Everything else lives here and is shared.
//
// Memory passed to pdfium is always allocated outside the Go heap
// (native.alloc), because pdfium keeps pointers (for example to the document
// bytes) after the call returns, and Go's garbage collector must not move or
// free that memory behind its back.

// native is implemented once per platform.
type native interface {
	// open loads the library at path and resolves the given function names.
	open(path string, funcs []string) error
	// call invokes a function with integer/pointer arguments and returns its result register.
	call(fn string, args ...uintptr) uintptr
	// alloc/free manage native memory; copyIn/copyOut move bytes across.
	alloc(n int) uintptr
	free(p uintptr)
	copyIn(dst uintptr, src []byte)
	copyOut(src uintptr, n int) []byte
	// writeCallback returns the address of a C-callable function
	// int WriteBlock(FPDF_FILEWRITE*, const void* data, unsigned long size)
	// that forwards the data to onWriteBlock.
	writeCallback() uintptr
}

// libName is the file name of the library on this platform (set in the native files).
// nativeLib is the platform implementation.

func init() { PDF = &pdfium{} }

type pdfium struct {
	mu      sync.Mutex // pdfium is not thread-safe: one operation at a time
	loaded  bool
	loadErr error
	lib     native
}

// Functions we use. If any is missing the library is considered unusable.
var pdfiumFuncs = []string{
	"FPDF_InitLibrary", "FPDF_LoadMemDocument", "FPDF_GetLastError", "FPDF_GetPageCount",
	"FPDF_GetPageSizeByIndexF", "FPDF_LoadPage", "FPDF_ClosePage", "FPDF_CloseDocument",
	"FPDFBitmap_Create", "FPDFBitmap_FillRect", "FPDF_RenderPageBitmap", "FPDFBitmap_GetBuffer",
	"FPDFBitmap_GetStride", "FPDFBitmap_Destroy", "FPDF_CreateNewDocument", "FPDF_ImportPages",
	"FPDF_ImportPagesByIndex", "FPDF_SaveAsCopy",
}

// writeDst collects the bytes of the document being saved (guarded by pdfium.mu,
// since FPDF_SaveAsCopy calls back synchronously on the calling thread).
var writeDst []byte

// onWriteBlock is called by the platform callback for every chunk pdfium writes.
func onWriteBlock(lib native, data uintptr, n int) {
	writeDst = append(writeDst, lib.copyOut(data, n)...)
}

// load opens the library the first time it is needed.
func (p *pdfium) load() error {
	if p.loaded {
		return p.loadErr
	}
	p.loaded = true
	candidates := []string{filepath.Join(ToolDir, libName)}
	if !isWindows {
		candidates = append(candidates, filepath.Join(ToolDir, "lib", libName), libName) // also try the system one
	}
	p.loadErr = errNoPDF
	for _, path := range candidates {
		if filepath.IsAbs(path) {
			if _, err := os.Stat(path); err != nil {
				continue
			}
		}
		lib := nativeLib()
		if err := lib.open(path, pdfiumFuncs); err != nil {
			p.loadErr = fmt.Errorf("could not load %s: %w", libName, err)
			continue
		}
		p.lib, p.loadErr = lib, nil
		p.lib.call("FPDF_InitLibrary")
		return nil
	}
	return p.loadErr
}

// ok converts a C int/FPDF_BOOL result: only the low 32 bits are meaningful.
func ok(r uintptr) bool { return int32(r) != 0 }

// doc is an open PDF together with the native copy of its bytes.
type doc struct {
	h    uintptr // FPDF_DOCUMENT
	data uintptr // native memory holding the file; must outlive the document
}

// open loads a whole PDF file into native memory and opens it.
func (p *pdfium) open(path string) (*doc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("empty PDF file")
	}
	mem := p.lib.alloc(len(b))
	if mem == 0 {
		return nil, errors.New("not enough memory to open the PDF")
	}
	p.lib.copyIn(mem, b)
	h := p.lib.call("FPDF_LoadMemDocument", mem, uintptr(len(b)), 0)
	if h == 0 {
		p.lib.free(mem)
		switch uint32(p.lib.call("FPDF_GetLastError")) {
		case 4:
			return nil, errors.New("the PDF is password protected")
		case 3:
			return nil, errors.New("the file is not a valid PDF or it is damaged")
		case 5:
			return nil, errors.New("the PDF uses an unsupported security handler")
		default:
			return nil, errors.New("could not open the PDF")
		}
	}
	return &doc{h: h, data: mem}, nil
}

func (p *pdfium) closeDoc(d *doc) {
	if d != nil && d.h != 0 {
		p.lib.call("FPDF_CloseDocument", d.h)
		p.lib.free(d.data)
		d.h, d.data = 0, 0
	}
}

func (p *pdfium) pageCount(d *doc) int { return int(int32(p.lib.call("FPDF_GetPageCount", d.h))) }

// save serializes document h and returns its bytes.
func (p *pdfium) save(h uintptr) ([]byte, error) {
	// struct FPDF_FILEWRITE { int version; int (*WriteBlock)(...); } -> 16 bytes on 64-bit
	fw := p.lib.alloc(16)
	defer p.lib.free(fw)
	var buf [16]byte
	binary.LittleEndian.PutUint32(buf[0:], 1) // version
	binary.LittleEndian.PutUint64(buf[8:], uint64(p.lib.writeCallback()))
	p.lib.copyIn(fw, buf[:])

	writeDst = nil
	res := p.lib.call("FPDF_SaveAsCopy", h, fw, 0)
	out := writeDst
	writeDst = nil
	if !ok(res) {
		return nil, errors.New("could not save the PDF")
	}
	return out, nil
}

// clampRange limits from/to to existing pages (0 = from the start / to the end).
func clampRange(from, to, n int) (int, int, error) {
	if from < 1 {
		from = 1
	}
	if to < 1 || to > n {
		to = n
	}
	if from > n || from > to {
		return 0, 0, fmt.Errorf("the PDF only has %d pages", n)
	}
	return from, to, nil
}

// extract builds a new document with pages a..b (0-based, inclusive) of src.
func (p *pdfium) extract(src uintptr, a, b int) ([]byte, error) {
	dest := p.lib.call("FPDF_CreateNewDocument")
	if dest == 0 {
		return nil, errors.New("could not create the PDF")
	}
	defer p.lib.call("FPDF_CloseDocument", dest)
	n := b - a + 1
	idx := p.lib.alloc(4 * n) // int page_indices[n]
	defer p.lib.free(idx)
	raw := make([]byte, 4*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint32(raw[4*i:], uint32(a+i))
	}
	p.lib.copyIn(idx, raw)
	if !ok(p.lib.call("FPDF_ImportPagesByIndex", dest, src, idx, uintptr(n), 0)) {
		return nil, fmt.Errorf("could not copy pages %d-%d", a+1, b+1)
	}
	return p.save(dest)
}

// withDoc is the common prologue: lock, load the library, open the file.
func (p *pdfium) withDoc(path string, fn func(d *doc, n int) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.load(); err != nil {
		return err
	}
	d, err := p.open(path)
	if err != nil {
		return err
	}
	defer p.closeDoc(d)
	n := p.pageCount(d)
	if n <= 0 {
		return errors.New("the PDF has no pages")
	}
	return fn(d, n)
}

func (p *pdfium) PageCount(path string) (count int, err error) {
	err = p.withDoc(path, func(_ *doc, n int) error { count = n; return nil })
	return count, err
}

func (p *pdfium) Extract(path string, from, to int) (out []byte, err error) {
	err = p.withDoc(path, func(d *doc, n int) error {
		a, b, err := clampRange(from, to, n)
		if err != nil {
			return err
		}
		out, err = p.extract(d.h, a-1, b-1)
		return err
	})
	return out, err
}

func (p *pdfium) Split(path string, from, to int, fn func(page, total int, data []byte) error) error {
	return p.withDoc(path, func(d *doc, n int) error {
		a, b, err := clampRange(from, to, n)
		if err != nil {
			return err
		}
		for i := a - 1; i < b; i++ {
			data, err := p.extract(d.h, i, i)
			if err != nil {
				return err
			}
			if err := fn(i+1, n, data); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *pdfium) Merge(inputs []string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.load(); err != nil {
		return nil, err
	}
	dest := p.lib.call("FPDF_CreateNewDocument")
	if dest == 0 {
		return nil, errors.New("could not create the PDF")
	}
	defer p.lib.call("FPDF_CloseDocument", dest)
	// Sources stay open until the result is saved: imported pages may still
	// reference their resources.
	var srcs []*doc
	defer func() {
		for _, s := range srcs {
			p.closeDoc(s)
		}
	}()
	for _, in := range inputs {
		src, err := p.open(in)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(in), err)
		}
		srcs = append(srcs, src)
		at := uintptr(int32(p.lib.call("FPDF_GetPageCount", dest)))
		if !ok(p.lib.call("FPDF_ImportPages", dest, src.h, 0 /* NULL = all pages */, at)) {
			return nil, fmt.Errorf("%s: could not copy the pages", filepath.Base(in))
		}
	}
	return p.save(dest)
}

func (p *pdfium) Render(path string, dpi int, fn func(page, total int, img *image.RGBA) error) error {
	return p.withDoc(path, func(d *doc, n int) error {
		size := p.lib.alloc(8) // FS_SIZEF { float width; float height; }
		defer p.lib.free(size)
		for i := 0; i < n; i++ {
			if !ok(p.lib.call("FPDF_GetPageSizeByIndexF", d.h, uintptr(i), size)) {
				return fmt.Errorf("page %d cannot be read", i+1)
			}
			raw := p.lib.copyOut(size, 8)
			wPt := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[0:])))
			hPt := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[4:])))
			scale := float64(dpi) / 72 // PDF sizes are in points (1/72 inch)
			// limit huge pages (posters, plans) to about 40 megapixels
			if px := wPt * hPt * scale * scale; px > 40e6 {
				scale *= math.Sqrt(40e6 / px)
			}
			w, h := int(wPt*scale+0.5), int(hPt*scale+0.5)
			if w < 1 || h < 1 {
				return fmt.Errorf("page %d has an invalid size", i+1)
			}
			img, err := p.renderPage(d.h, i, w, h)
			if err != nil {
				return err
			}
			if err := fn(i+1, n, img); err != nil {
				return err
			}
		}
		return nil
	})
}

// renderPage draws page index of document h into a w×h image on a white background.
func (p *pdfium) renderPage(h uintptr, index, w, hh int) (*image.RGBA, error) {
	page := p.lib.call("FPDF_LoadPage", h, uintptr(index))
	if page == 0 {
		return nil, fmt.Errorf("page %d cannot be read", index+1)
	}
	defer p.lib.call("FPDF_ClosePage", page)
	bmp := p.lib.call("FPDFBitmap_Create", uintptr(w), uintptr(hh), 0) // 0 = no alpha (BGRx)
	if bmp == 0 {
		return nil, errors.New("not enough memory to render the page")
	}
	defer p.lib.call("FPDFBitmap_Destroy", bmp)
	p.lib.call("FPDFBitmap_FillRect", bmp, 0, 0, uintptr(w), uintptr(hh), 0xFFFFFFFF)
	const fpdfAnnot = 0x01 // also draw annotations (form fields, stamps...)
	p.lib.call("FPDF_RenderPageBitmap", bmp, page, 0, 0, uintptr(w), uintptr(hh), 0, fpdfAnnot)
	buf := p.lib.call("FPDFBitmap_GetBuffer", bmp)
	stride := int(int32(p.lib.call("FPDFBitmap_GetStride", bmp)))
	if buf == 0 || stride < w*4 {
		return nil, errors.New("could not render the page")
	}
	raw := p.lib.copyOut(buf, stride*hh)
	img := image.NewRGBA(image.Rect(0, 0, w, hh))
	for y := 0; y < hh; y++ {
		s := raw[y*stride : y*stride+w*4]
		d := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w*4; x += 4 {
			d[x], d[x+1], d[x+2], d[x+3] = s[x+2], s[x+1], s[x], 255 // BGRx -> RGBA
		}
	}
	return img, nil
}
