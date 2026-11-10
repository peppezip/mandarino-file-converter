//go:build windows

package main

import (
	"math"
	"unsafe"
)

// GDI+ implementation of Canvas (see ui_canvas.go). GDI+ ships with every
// Windows version and gives us anti-aliased shapes and text with alpha.
//
// Note on floating-point arguments: Go's syscall passes every argument as an
// integer, but on Windows x64 the runtime also copies the first four into the
// XMM registers, and later arguments go on the stack where float32 bits are
// read correctly. So a float32 can be passed as uintptr(math.Float32bits(v)).

var (
	pGdiplusStartup                    = gdiplus.NewProc("GdiplusStartup")
	pGdipCreateBitmapFromScan0         = gdiplus.NewProc("GdipCreateBitmapFromScan0")
	pGdipGetImageGraphicsContext       = gdiplus.NewProc("GdipGetImageGraphicsContext")
	pGdipSetSmoothingMode              = gdiplus.NewProc("GdipSetSmoothingMode")
	pGdipSetTextRenderingHint          = gdiplus.NewProc("GdipSetTextRenderingHint")
	pGdipGraphicsClear                 = gdiplus.NewProc("GdipGraphicsClear")
	pGdipCreateSolidFill               = gdiplus.NewProc("GdipCreateSolidFill")
	pGdipDeleteBrush                   = gdiplus.NewProc("GdipDeleteBrush")
	pGdipFillEllipseI                  = gdiplus.NewProc("GdipFillEllipseI")
	pGdipFillEllipse                   = gdiplus.NewProc("GdipFillEllipse")
	pGdipCreatePen1                    = gdiplus.NewProc("GdipCreatePen1")
	pGdipDrawEllipseI                  = gdiplus.NewProc("GdipDrawEllipseI")
	pGdipDeletePen                     = gdiplus.NewProc("GdipDeletePen")
	pGdipSetPenLineJoin                = gdiplus.NewProc("GdipSetPenLineJoin")
	pGdipFillPolygon                   = gdiplus.NewProc("GdipFillPolygon")
	pGdipDrawPolygon                   = gdiplus.NewProc("GdipDrawPolygon")
	pGdipTranslateWorldTransform       = gdiplus.NewProc("GdipTranslateWorldTransform")
	pGdipRotateWorldTransform          = gdiplus.NewProc("GdipRotateWorldTransform")
	pGdipResetWorldTransform           = gdiplus.NewProc("GdipResetWorldTransform")
	pGdipCreateFontFamilyFromName      = gdiplus.NewProc("GdipCreateFontFamilyFromName")
	pGdipGetGenericFontFamilySansSerif = gdiplus.NewProc("GdipGetGenericFontFamilySansSerif")
	pGdipCreateFont                    = gdiplus.NewProc("GdipCreateFont")
	pGdipDeleteFont                    = gdiplus.NewProc("GdipDeleteFont")
	pGdipCreateStringFormat            = gdiplus.NewProc("GdipCreateStringFormat")
	pGdipSetStringFormatAlign          = gdiplus.NewProc("GdipSetStringFormatAlign")
	pGdipSetStringFormatLineAlign      = gdiplus.NewProc("GdipSetStringFormatLineAlign")
	pGdipSetStringFormatTrimming       = gdiplus.NewProc("GdipSetStringFormatTrimming")
	pGdipDeleteStringFormat            = gdiplus.NewProc("GdipDeleteStringFormat")
	pGdipDrawString                    = gdiplus.NewProc("GdipDrawString")
	pGdipDeleteGraphics                = gdiplus.NewProc("GdipDeleteGraphics")
	pGdipDisposeImage                  = gdiplus.NewProc("GdipDisposeImage")
	pGdipCreateHICONFromBitmap         = gdiplus.NewProc("GdipCreateHICONFromBitmap")
)

const pixelFormat32bppPARGB = 0x000E200B // premultiplied alpha, what UpdateLayeredWindow expects

// gdipStart initializes GDI+ once per process.
func gdipStart() bool {
	type startupInput struct {
		Version                  uint32
		DebugEventCallback       uintptr
		SuppressBackgroundThread int32
		SuppressExternalCodecs   int32
	}
	in := startupInput{Version: 1}
	var token uintptr
	r, _, _ := pGdiplusStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&in)), 0)
	return r == 0
}

// f32 passes a float as a float32 argument (see the note at the top).
func f32(v float64) uintptr { return uintptr(math.Float32bits(float32(v))) }

// gdiCanvas draws into a GDI+ bitmap.
type gdiCanvas struct {
	bmp, g uintptr
}

// newCanvas creates a w×h premultiplied-ARGB bitmap. If bits != 0 the bitmap
// uses that memory (a DIB section), otherwise GDI+ allocates its own.
func newCanvas(w, h int, bits uintptr) *gdiCanvas {
	c := &gdiCanvas{}
	stride := 0
	if bits != 0 {
		stride = w * 4
	}
	pGdipCreateBitmapFromScan0.Call(uintptr(w), uintptr(h), uintptr(stride), pixelFormat32bppPARGB, bits, uintptr(unsafe.Pointer(&c.bmp)))
	if c.bmp == 0 {
		return nil
	}
	pGdipGetImageGraphicsContext.Call(c.bmp, uintptr(unsafe.Pointer(&c.g)))
	pGdipSetSmoothingMode.Call(c.g, 4)     // SmoothingModeAntiAlias
	pGdipSetTextRenderingHint.Call(c.g, 4) // TextRenderingHintAntiAlias (ClearType needs an opaque background)
	pGdipGraphicsClear.Call(c.g, 0)        // fully transparent
	return c
}

// close releases the graphics object and the bitmap (not the DIB memory).
func (c *gdiCanvas) close() {
	if c.g != 0 {
		pGdipDeleteGraphics.Call(c.g)
	}
	if c.bmp != 0 {
		pGdipDisposeImage.Call(c.bmp)
	}
}

// brush runs fn with a temporary solid brush.
func brush(col Color, fn func(br uintptr)) {
	var br uintptr
	pGdipCreateSolidFill.Call(uintptr(col), uintptr(unsafe.Pointer(&br)))
	fn(br)
	pGdipDeleteBrush.Call(br)
}

func (c *gdiCanvas) Circle(cx, cy, r int, col Color) {
	brush(col, func(br uintptr) {
		pGdipFillEllipseI.Call(c.g, br, uintptr(cx-r), uintptr(cy-r), uintptr(2*r), uintptr(2*r))
	})
}

func (c *gdiCanvas) Ring(cx, cy, r int, width float64, col Color) {
	var pen uintptr
	pGdipCreatePen1.Call(uintptr(col), f32(width), 2 /*UnitPixel*/, uintptr(unsafe.Pointer(&pen)))
	pGdipDrawEllipseI.Call(c.g, pen, uintptr(cx-r), uintptr(cy-r), uintptr(2*r), uintptr(2*r))
	pGdipDeletePen.Call(pen)
}

func (c *gdiCanvas) RadialEllipse(cx, cy int, angleDeg, dist, l, w float64, col Color) {
	brush(col, func(br uintptr) {
		// move the origin to the centre and rotate so that "up" points along angleDeg
		pGdipResetWorldTransform.Call(c.g)
		pGdipTranslateWorldTransform.Call(c.g, f32(float64(cx)), f32(float64(cy)), 0)
		pGdipRotateWorldTransform.Call(c.g, f32(angleDeg+90), 0)
		pGdipFillEllipse.Call(c.g, br, f32(-w/2), f32(-dist-l/2), f32(w), f32(l))
		pGdipResetWorldTransform.Call(c.g)
	})
}

func (c *gdiCanvas) RoundedPolygon(pts [][2]float32, r float64, col Color) {
	if len(pts) < 3 {
		return
	}
	// fill the inset polygon, then stroke it with a pen 2r wide and round joins:
	// the outline grows by r and every corner becomes an arc of radius r
	brush(col, func(br uintptr) {
		pGdipFillPolygon.Call(c.g, br, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)), 0)
	})
	var pen uintptr
	pGdipCreatePen1.Call(uintptr(col), f32(2*r), 2 /*UnitPixel*/, uintptr(unsafe.Pointer(&pen)))
	pGdipSetPenLineJoin.Call(pen, 2) // LineJoinRound
	pGdipDrawPolygon.Call(c.g, pen, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	pGdipDeletePen.Call(pen)
}

var fontFamily uintptr

// family returns "Segoe UI" (the Windows UI font), or a generic sans-serif.
func family() uintptr {
	if fontFamily == 0 {
		pGdipCreateFontFamilyFromName.Call(up(utf16p("Segoe UI")), 0, uintptr(unsafe.Pointer(&fontFamily)))
		if fontFamily == 0 {
			pGdipGetGenericFontFamilySansSerif.Call(uintptr(unsafe.Pointer(&fontFamily)))
		}
	}
	return fontFamily
}

func (c *gdiCanvas) Text(s string, x, y, w, h int, px float64, bold bool, col Color) {
	var font uintptr
	style := uintptr(0)
	if bold {
		style = 1 // FontStyleBold
	}
	pGdipCreateFont.Call(family(), f32(px), style, 2 /*UnitPixel*/, uintptr(unsafe.Pointer(&font)))
	if font == 0 {
		return
	}
	defer pGdipDeleteFont.Call(font)
	var sf uintptr
	pGdipCreateStringFormat.Call(0x1000 /*StringFormatFlagsNoWrap*/, 0, uintptr(unsafe.Pointer(&sf)))
	defer pGdipDeleteStringFormat.Call(sf)
	pGdipSetStringFormatAlign.Call(sf, 1)     // centred horizontally
	pGdipSetStringFormatLineAlign.Call(sf, 1) // centred vertically
	pGdipSetStringFormatTrimming.Call(sf, 3)  // "…" when too long
	rect := [4]float32{float32(x), float32(y), float32(w), float32(h)}
	brush(col, func(br uintptr) {
		pGdipDrawString.Call(c.g, up(utf16p(s)), ^uintptr(0) /* -1: NUL-terminated */, font, uintptr(unsafe.Pointer(&rect)), sf, br)
	})
}

// makeIcon draws the wheel icon (see drawIcon) and returns an HICON for the tray.
func makeIcon(size int) uintptr {
	c := newCanvas(size, size, 0)
	if c == nil {
		return 0
	}
	defer c.close()
	drawIcon(c, size)
	var hicon uintptr
	pGdipCreateHICONFromBitmap.Call(c.bmp, uintptr(unsafe.Pointer(&hicon)))
	return hicon
}

// layeredPaint renders paint into a transparent size×size bitmap and shows it
// in the layered window hwnd. pos is the top-left corner on screen
// (nil = leave the window where it is, e.g. after the user dragged it).
func layeredPaint(hwnd uintptr, size int, pos *POINT, paint func(cv Canvas)) {
	hdcScreen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, hdcScreen)
	mem, _, _ := pCreateCompatibleDC.Call(hdcScreen)
	defer pDeleteDC.Call(mem)
	bi := BITMAPINFOHEADER{BiWidth: int32(size), BiHeight: -int32(size), BiPlanes: 1, BiBitCount: 32} // negative height = top-down
	bi.BiSize = uint32(unsafe.Sizeof(bi))
	var bits uintptr
	dib, _, _ := pCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 {
		return
	}
	defer pDeleteObject.Call(dib)
	old, _, _ := pSelectObject.Call(mem, dib)
	defer pSelectObject.Call(mem, old)

	cv := newCanvas(size, size, bits)
	if cv == nil {
		return
	}
	paint(cv)
	cv.close()

	sz := SIZE{int32(size), int32(size)}
	ptSrc := POINT{}
	blend := BLENDFUNCTION{BlendOp: AC_SRC_OVER, SourceConstantAlpha: 255, AlphaFormat: AC_SRC_ALPHA}
	pUpdateLayeredWindow.Call(hwnd, hdcScreen, uintptr(unsafe.Pointer(pos)), uintptr(unsafe.Pointer(&sz)),
		mem, uintptr(unsafe.Pointer(&ptSrc)), 0, uintptr(unsafe.Pointer(&blend)), ULW_ALPHA)
}
