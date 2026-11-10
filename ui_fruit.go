package main

import "math"

// fruit is the geometry of a mandarin cut in half (the wheel), in physical pixels.
// It is shared by the wheel and by the progress window.
type fruit struct {
	size    int     // side of the (square) window
	radius  int     // distance of the labels from the centre (0 = no labels)
	fleshR  int     // radius of the flesh (the segments)
	centerR int     // radius of the small white heart in the middle
	outerR  int     // outer radius (peel)
	scale   float64 // DPI scale factor (1 = 96 dpi)
}

// segStyle describes how to paint one segment.
type segStyle struct {
	col   Color   // fill colour
	pop   float64 // how far the segment sticks out (pixels), used to highlight
	juice uint8   // opacity of the little juice drops
}

// Wheel palette.
var (
	colPeel      = argb(255, 239, 108, 0)   // deep orange peel
	colPith      = argb(255, 255, 244, 226) // white "albedo" under the peel
	colPithDark  = argb(255, 255, 228, 190) // centre of the heart
	colFlesh     = argb(255, 255, 145, 0)   // segment
	colFleshAlt  = argb(255, 255, 156, 18)  // every other segment, slightly lighter
	colFleshHi   = argb(255, 255, 190, 60)  // highlighted segment
	colFleshPale = argb(255, 255, 224, 182) // "unripe" segment (progress not reached)
	colCream     = argb(255, 255, 246, 232) // panels and buttons
	colAccent    = argb(255, 232, 102, 0)   // text and borders on cream
	colDeep      = argb(255, 110, 45, 0)    // big numbers
	colLabel     = argb(255, 205, 110, 0)   // small captions on cream
	colWhite     = argb(255, 255, 255, 255)
)

// ripe returns the normal colour of segment i (alternating shades).
func ripe(i int) Color {
	if i%2 == 1 {
		return colFleshAlt
	}
	return colFlesh
}

// draw paints the peel, the white pith, n segments (coloured by style) with
// their juice drops, and the heart in the middle. c is the window centre.
func (f *fruit) draw(cv Canvas, c, n int, style func(i int) segStyle) {
	s := f.scale
	step := 360 / float64(n)
	rind := int(9 * s)

	// soft shadow under the fruit
	for k := 4; k >= 1; k-- {
		cv.Circle(c, c+int(3*s), f.outerR+int(float64(k)*1.5*s), argb(16, 0, 0, 0))
	}
	// peel, with a thin highlight on the edge and darker oil glands
	cv.Circle(c, c, f.outerR, colPeel)
	cv.Ring(c, c, f.outerR-int(2*s), 1.5*s, argb(70, 255, 255, 255))
	for k := 0; k < n*7; k++ {
		a := float64(k)*(360/float64(n*7)) + float64(k%3)*4
		cv.RadialEllipse(c, c, a, float64(f.outerR)-float64(rind)*0.55, 2.2*s, 2.2*s, argb(60, 190, 70, 0))
	}
	// white pith (albedo); it also shows through the gaps between segments
	cv.Circle(c, c, f.outerR-rind, colPith)

	// rounded segments separated by a thin gap
	styles := make([]segStyle, n)
	corner := 6 * s
	for i := 0; i < n; i++ {
		styles[i] = style(i)
		shape := segmentShape(c, -90+float64(i)*step, step, float64(f.fleshR)+styles[i].pop, corner, 3.5*s)
		cv.RoundedPolygon(shape, corner, styles[i].col)
	}
	// juice drops: small, light, elongated ellipses pointing outwards
	sides := []float64{-0.3, 0.3, -0.18, 0.22, -0.32, 0.12, 0.3, -0.22, 0.05}
	for i := 0; i < n; i++ {
		mid := -90 + float64(i)*step
		for k, side := range sides {
			t := float64(k) / 8
			dist := float64(f.centerR) + 14*s + t*(float64(f.fleshR-f.centerR)-26*s)
			// leave room for the label in the middle of the segment
			if f.radius > 0 && math.Abs(dist-float64(f.radius)) < 16*s && math.Abs(side) < 0.25 {
				continue
			}
			cv.RadialEllipse(c, c, mid+side*step, dist, 11*s, 3.6*s, argb(styles[i].juice, 255, 255, 235))
		}
	}
	// heart
	cv.Circle(c, c, f.centerR, colPith)
	cv.Circle(c, c, int(float64(f.centerR)*0.45), colPithDark)
}

// segmentShape returns the outline of one segment: centre c, direction midDeg,
// opening stepDeg, radius R. The outline is inset by corner+gap/2 so that,
// once its corners are rounded by "corner", neighbours are gap pixels apart.
func segmentShape(c int, midDeg, stepDeg, R, corner, gap float64) [][2]float32 {
	half := stepDeg / 2 * math.Pi / 180
	m := midDeg * math.Pi / 180
	off := corner + gap/2
	cx, cy := float64(c), float64(c)
	apex := off / math.Sin(half) // the tip moves outwards so both sides stay parallel to the original
	pts := [][2]float32{{float32(cx + apex*math.Cos(m)), float32(cy + apex*math.Sin(m))}}
	ri := R - corner
	d := math.Asin(math.Min(1, off/ri))
	a0, a1 := m-half+d, m+half-d
	const steps = 24
	for j := 0; j <= steps; j++ {
		a := a0 + (a1-a0)*float64(j)/steps
		pts = append(pts, [2]float32{float32(cx + ri*math.Cos(a)), float32(cy + ri*math.Sin(a))})
	}
	return pts
}
