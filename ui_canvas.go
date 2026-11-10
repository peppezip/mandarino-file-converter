package main

import "math"

// The drawing code of Mandarino (the wheels, the page picker, the
// folder browser and the progress wheel) is shared by Windows and Linux.
// It only talks to this small Canvas interface, implemented with GDI+ on
// Windows (gdip_windows.go) and with cairo on Linux (x11_linux.go).
//
// All coordinates are in physical pixels with (0,0) at the top-left corner of
// the window; angles are in degrees, clockwise, 0 = pointing right.

// Color is a 32-bit ARGB colour (0xAARRGGBB), the format GDI+ uses natively.
type Color uint32

// argb builds a Color from its components.
func argb(a, r, g, b uint8) Color {
	return Color(uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b))
}

// Components returns alpha, red, green and blue as 0..1 floats (used by cairo on Linux).
func (c Color) Components() (a, r, g, b float64) {
	return float64(c>>24&0xFF) / 255, float64(c>>16&0xFF) / 255, float64(c>>8&0xFF) / 255, float64(c&0xFF) / 255
}

// lerpColor mixes a and b: t = 0 gives a, t = 1 gives b.
func lerpColor(a, b Color, t float64) Color {
	t = math.Max(0, math.Min(1, t))
	ch := func(sh uint) Color {
		x, y := float64((a>>sh)&0xFF), float64((b>>sh)&0xFF)
		return Color(x+(y-x)*t+0.5) << sh
	}
	return ch(24) | ch(16) | ch(8) | ch(0)
}

// Canvas is everything the shared drawing code needs from a platform.
type Canvas interface {
	// Circle fills a disc of radius r centred on (cx, cy).
	Circle(cx, cy, r int, c Color)
	// Ring strokes a circle outline of the given line width.
	Ring(cx, cy, r int, width float64, c Color)
	// RadialEllipse fills an ellipse that points away from (cx, cy) in direction
	// angleDeg, centred at distance dist, l long (radially) and w wide.
	RadialEllipse(cx, cy int, angleDeg, dist, l, w float64, c Color)
	// RoundedPolygon fills a polygon whose corners are rounded with radius r.
	// The points must already be inset by r (the outline grows by r).
	RoundedPolygon(pts [][2]float32, r float64, c Color)
	// Text draws s centred inside the rectangle (x, y, w, h), shortened with
	// an ellipsis if it does not fit. px is the font size in pixels.
	Text(s string, x, y, w, h int, px float64, bold bool, c Color)
}

// roundRect fills a rectangle with rounded corners (radius r) using RoundedPolygon.
func roundRect(cv Canvas, x, y, w, h int, r float64, c Color) {
	r = math.Min(r, math.Min(float64(w), float64(h))/2-0.5)
	x0, y0 := float32(float64(x)+r), float32(float64(y)+r)
	x1, y1 := float32(float64(x+w)-r), float32(float64(y+h)-r)
	cv.RoundedPolygon([][2]float32{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}, r, c)
}

// shadowText draws white-ish text with a soft brown shadow, readable on orange.
func shadowText(cv Canvas, s string, x, y, w, h int, px, scale float64, bold bool, c Color) {
	cv.Text(s, x+int(math.Round(scale)), y+int(math.Round(1.5*scale)), w, h, px, bold, argb(110, 140, 50, 0))
	cv.Text(s, x, y, w, h, px, bold, c)
}

// Key is a platform-neutral key code delivered to the shared UI models.
type Key int

const (
	KeyNone Key = iota
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyTab
	KeyEnter
	KeySpace
	KeyEsc
	KeyBackspace
	KeyDigit0 // KeyDigit0 + n is the digit n (0..9)
)

// digit returns n and true if k is a digit key.
func (k Key) digit() (int, bool) {
	if k >= KeyDigit0 && k <= KeyDigit0+9 {
		return int(k - KeyDigit0), true
	}
	return 0, false
}
