package main

import (
	"fmt"
	"math"
)

// progressModel is the "progress wheel": shown when an operation is not
// instantaneous. Segments ripen one after the other (clockwise) as the job
// advances; the cream panel in the middle shows the action, the percentage
// and a progress bar. A small × on the peel cancels the job.
//
// When the progress is unknown (a single file and the tool reports nothing,
// e.g. while extracting PDF pages) a bright segment runs around the fruit and
// a short block slides along the bar.

type progressModel struct {
	fruit
	short       string  // short action name, e.g. "Compress"
	index       int     // file being processed (0-based)
	files       int     // number of files in the job
	pct         float64 // 0..1, or -1 when unknown
	done        bool    // finished: show "Done!" for a moment
	phase       int     // animation counter, incremented by the platform timer
	hoverCancel bool    // mouse over the × button
}

const progressSegments = 10

// newProgressModel lays out a progress wheel at the given DPI scale.
func newProgressModel(scale float64) *progressModel {
	p := &progressModel{}
	s := scale
	p.scale = s
	p.fleshR = int(80 * s)
	p.outerR = p.fleshR + int(14*s)
	p.centerR = int(12 * s)
	p.size = 2*p.outerR + int(16*s)
	return p
}

// Update copies the job state into the model.
func (p *progressModel) Update(short string, index, files int, frac float64) {
	p.short, p.index, p.files = short, index, files
	if files <= 1 && frac == 0 {
		p.pct = -1
	} else {
		p.pct = math.Min(1, (float64(index)+frac)/float64(max(files, 1)))
	}
}

// Finish switches to the "Done!" state.
func (p *progressModel) Finish() { p.done, p.pct = true, 1 }

// cancelButton returns the centre and radius of the × button (on the peel, top right).
func (p *progressModel) cancelButton() (int, int, int) {
	s := p.scale
	c := p.size / 2
	a := -45 * math.Pi / 180
	d := float64(p.outerR) - 6*s
	return c + int(d*math.Cos(a)), c + int(d*math.Sin(a)), int(13 * s)
}

// OverCancel reports whether window point (x, y) is on the × button.
func (p *progressModel) OverCancel(x, y int) bool {
	bx, by, br := p.cancelButton()
	return !p.done && math.Hypot(float64(x-bx), float64(y-by)) <= float64(br)+2
}

// Paint draws the progress wheel.
func (p *progressModel) Paint(cv Canvas) {
	s := p.scale
	c := p.size / 2
	px := func(v float64) int { return int(math.Round(v * s)) }
	const n = progressSegments

	p.draw(cv, c, n, func(i int) segStyle {
		switch {
		case p.done:
			return segStyle{col: ripe(i), juice: 60}
		case p.pct < 0: // unknown progress: one bright segment chases around
			switch (i - p.phase/2%n + n) % n {
			case 0:
				return segStyle{col: colFleshHi, pop: 3 * s, juice: 80}
			case n - 1:
				return segStyle{col: ripe(i), juice: 50}
			}
			return segStyle{col: colFleshPale, juice: 35}
		}
		filled := p.pct * n
		switch {
		case float64(i+1) <= filled:
			return segStyle{col: ripe(i), juice: 55}
		case float64(i) < filled: // the segment being filled blends in gradually
			return segStyle{col: lerpColor(colFleshPale, ripe(i), filled-float64(i)), juice: 45}
		}
		return segStyle{col: colFleshPale, juice: 35}
	})

	// central panel: action, percentage, bar
	pw, ph := px(124), px(68)
	x0, y0 := c-pw/2, c-ph/2
	roundRect(cv, x0, y0, pw, ph, 18*s, colCream)
	label := p.short
	if p.files > 1 {
		label = fmt.Sprintf("%s · %d/%d", p.short, min(p.index+1, p.files), p.files)
	}
	cv.Text(label, x0+px(6), y0+px(5), pw-px(12), px(16), 11*s, true, colLabel)
	big := "…"
	switch {
	case p.done:
		big = "Done!"
	case p.pct >= 0:
		big = fmt.Sprintf("%d%%", int(p.pct*100))
	}
	cv.Text(big, x0, y0+px(19), pw, px(28), 21*s, true, colDeep)

	tx, ty, tw, th := c-px(48), y0+ph-px(16), px(96), px(8)
	roundRect(cv, tx, ty, tw, th, 4*s, argb(255, 255, 222, 178))
	if p.done || p.pct >= 0 {
		if fw := int(float64(tw) * math.Max(p.pct, 0)); fw > 0 {
			roundRect(cv, tx, ty, max(fw, th), th, 4*s, colAccent)
		}
	} else { // indeterminate: a block slides along the bar
		chunk := px(30)
		pos := tx - chunk + (p.phase*px(3))%(tw+chunk)
		a, b := max(pos, tx), min(pos+chunk, tx+tw)
		if b-a > th {
			roundRect(cv, a, ty, b-a, th, 4*s, colAccent)
		}
	}

	// cancel button
	if !p.done {
		bx, by, br := p.cancelButton()
		fill, col := colCream, colAccent
		if p.hoverCancel {
			fill, col = colFleshHi, colWhite
		}
		cv.Circle(bx, by+px(1), br+px(1), argb(60, 120, 40, 0))
		cv.Circle(bx, by, br, fill)
		cv.Text("×", bx-br, by-br-px(1), 2*br, 2*br, 17*s, true, col)
	}
}
