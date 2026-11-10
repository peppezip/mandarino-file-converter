package main

import (
	"fmt"
	"math"
	"strconv"

	"mandarino/engine"
)

// The "Split" picker for a single PDF. Inside the wheel the user chooses
// "from page" / "to page" (with − and + buttons, the mouse wheel, the arrow
// keys or by typing digits), then:
//
//   - Extract        -> one PDF with those pages   ("pdf:extract:A-B")
//   - One per page   -> one PDF for every page     ("pdf:split:A-B")

type rangeState struct {
	pages    int    // pages in the document
	from, to int    // chosen range (1-based, inclusive)
	field    int    // field being edited: 0 = from, 1 = to
	typed    string // digits typed into the active field, not yet applied
	hover    string // zone under the mouse (see zones)
}

// zone is a clickable rectangle of the picker, in window coordinates.
type zone struct {
	id             string
	x0, y0, x1, y1 int
}

// enterRange switches the wheel to the picker, enlarging the fruit if needed.
func (w *wheelModel) enterRange() Outcome {
	n, err := engine.PageCount(w.files[0])
	if err != nil {
		return Outcome{Close: true, Error: err.Error()}
	}
	if n < 2 {
		return Outcome{Close: true, Error: "This PDF has a single page: there is nothing to split."}
	}
	w.mode = modeRange
	w.rg = rangeState{pages: n, from: 1, to: n}
	out := Outcome{Redraw: true}
	if minFlesh := int(132 * w.scale); w.fleshR < minFlesh {
		w.fleshR = minFlesh
		w.outerR = w.fleshR + int(14*w.scale)
		w.size = 2*w.outerR + int(16*w.scale)
		out.Resize = true // the platform keeps the same centre on screen
	}
	return out
}

// px converts a design size (in pixels at 96 dpi) to physical pixels.
func (w *wheelModel) px(v float64) int { return int(math.Round(v * w.scale)) }

// zones lists the clickable areas: the two fields (each with − and +) and the two buttons.
func (w *wheelModel) zones() []zone {
	c, px := w.size/2, w.px
	var zs []zone
	for f, cx := range []int{c - px(56), c + px(56)} {
		name := []string{"from", "to"}[f]
		top, bot := c-px(54), c+px(10)
		l, r := cx-px(52), cx+px(52)
		zs = append(zs,
			zone{name + "-", l, top, l + px(30), bot},
			zone{name + "+", r - px(30), top, r, bot},
			zone{name, l + px(30), top, r - px(30), bot},
		)
	}
	return append(zs,
		zone{"extract", c - px(108), c + px(22), c - px(4), c + px(58)},
		zone{"split", c + px(4), c + px(22), c + px(108), c + px(58)},
	)
}

func (w *wheelModel) zoneAt(x, y int) string {
	for _, z := range w.zones() {
		if x >= z.x0 && x < z.x1 && y >= z.y0 && y < z.y1 {
			return z.id
		}
	}
	return ""
}

func (w *wheelModel) rangeHover(x, y int) Outcome {
	if h := w.zoneAt(x, y); h != w.rg.hover {
		w.rg.hover = h
		return Outcome{Redraw: true}
	}
	return Outcome{}
}

// commit applies the typed digits and keeps 1 <= from <= to <= pages.
func (w *wheelModel) commit() {
	r := &w.rg
	if r.typed != "" {
		v, _ := strconv.Atoi(r.typed)
		r.typed = ""
		if r.field == 0 {
			r.from = v
		} else {
			r.to = v
		}
	}
	r.from = min(max(r.from, 1), r.pages)
	r.to = min(max(r.to, 1), r.pages)
	if r.from > r.to { // the field being edited wins
		if r.field == 0 {
			r.to = r.from
		} else {
			r.from = r.to
		}
	}
}

// bump adds delta to a field and makes it the active one.
func (w *wheelModel) bump(field, delta int) Outcome {
	w.commit()
	w.rg.field = field
	if field == 0 {
		w.rg.from += delta
	} else {
		w.rg.to += delta
	}
	w.commit()
	return Outcome{Redraw: true}
}

// finishRange returns the action for the chosen range.
func (w *wheelModel) finishRange(split bool) Outcome {
	w.commit()
	r := w.rg
	a := engine.Action{Label: "Split",
		ID:   fmt.Sprintf("pdf:extract:%d-%d", r.from, r.to),
		Desc: fmt.Sprintf("Extract pages %d-%d", r.from, r.to)}
	if r.from == r.to {
		a.Desc = fmt.Sprintf("Extract page %d", r.from)
	}
	if split {
		a.ID = fmt.Sprintf("pdf:split:%d-%d", r.from, r.to)
		a.Desc = fmt.Sprintf("Split pages %d-%d", r.from, r.to)
	}
	return Outcome{Close: true, Pick: &a}
}

func (w *wheelModel) rangeClick(x, y int) Outcome {
	switch z := w.zoneAt(x, y); z {
	case "from-":
		return w.bump(0, -1)
	case "from+":
		return w.bump(0, +1)
	case "to-":
		return w.bump(1, -1)
	case "to+":
		return w.bump(1, +1)
	case "from", "to":
		w.commit()
		w.rg.field = map[string]int{"from": 0, "to": 1}[z]
		return Outcome{Redraw: true}
	case "extract":
		return w.finishRange(false)
	case "split":
		return w.finishRange(true)
	}
	return Outcome{}
}

// rangeScroll changes the field under the mouse (or the active one).
func (w *wheelModel) rangeScroll(up bool) Outcome {
	d := -1
	if up {
		d = 1
	}
	f := w.rg.field
	switch w.rg.hover {
	case "from", "from-", "from+":
		f = 0
	case "to", "to-", "to+":
		f = 1
	}
	return w.bump(f, d)
}

func (w *wheelModel) rangeKey(k Key) Outcome {
	r := &w.rg
	if d, ok := k.digit(); ok {
		if len(r.typed) < len(strconv.Itoa(r.pages)) {
			r.typed += strconv.Itoa(d)
		}
		return Outcome{Redraw: true}
	}
	switch k {
	case KeyBackspace:
		if r.typed != "" {
			r.typed = r.typed[:len(r.typed)-1]
		}
		return Outcome{Redraw: true}
	case KeyLeft, KeyRight, KeyTab:
		w.commit()
		switch k {
		case KeyLeft:
			r.field = 0
		case KeyRight:
			r.field = 1
		default:
			r.field = 1 - r.field
		}
		return Outcome{Redraw: true}
	case KeyUp:
		return w.bump(r.field, +1)
	case KeyDown:
		return w.bump(r.field, -1)
	case KeyEnter:
		return w.finishRange(false)
	case KeyEsc:
		return Outcome{Close: true}
	}
	return Outcome{}
}

// paintRange draws the picker on top of a wheel with 8 unlabelled segments.
func (w *wheelModel) paintRange(cv Canvas, c int) {
	s, px := w.scale, w.px
	w.draw(cv, c, 8, func(i int) segStyle { return segStyle{col: ripe(i), juice: 50} })
	r := w.rg

	shadowText(cv, "Split PDF", c-px(90), c-px(112), px(180), px(26), 17*s, s, true, colWhite)
	shadowText(cv, fmt.Sprintf("%d pages", r.pages), c-px(90), c-px(86), px(180), px(20), 12*s, s, false, colWhite)

	// the two fields
	for f, cx := range []int{c - px(56), c + px(56)} {
		name := []string{"from", "to"}[f]
		txt := strconv.Itoa([]int{r.from, r.to}[f])
		if f == r.field && r.typed != "" {
			txt = r.typed
		}
		x, y, wd, h := cx-px(52), c-px(54), px(104), px(64)
		if f == r.field { // orange frame around the active field
			roundRect(cv, x-px(3), y-px(3), wd+px(6), h+px(6), 19*s, colAccent)
		}
		roundRect(cv, x, y, wd, h, 16*s, colCream)
		cv.Text([]string{"from page", "to page"}[f], x, y+px(4), wd, px(16), 10.5*s, true, colLabel)
		cv.Text(txt, x+px(26), y+px(18), wd-px(52), px(40), 24*s, true, colDeep)
		for k, sym := range []string{"−", "+"} {
			bx := x + px(15)
			if k == 1 {
				bx = x + wd - px(15)
			}
			by := y + px(38)
			if r.hover == name+[]string{"-", "+"}[k] {
				cv.Circle(bx, by, px(12), argb(255, 255, 214, 160))
			}
			cv.Text(sym, bx-px(14), by-px(14), px(28), px(28), 20*s, true, colAccent)
		}
	}

	// the two buttons
	for k, b := range []struct{ id, label string }{{"extract", "Extract"}, {"split", "One per page"}} {
		x := c - px(108)
		if k == 1 {
			x = c + px(4)
		}
		y, wd, h := c+px(22), px(104), px(36)
		fill, col := colCream, colAccent
		if r.hover == b.id {
			fill, col = colFleshHi, colWhite
		}
		roundRect(cv, x, y, wd, h, 18*s, fill)
		size := 13.5 * s
		if len([]rune(b.label)) > 8 {
			size = 11.5 * s
		}
		cv.Text(b.label, x, y, wd, h, size, true, col)
	}

	// hint under the buttons, explaining what the hovered button does
	hint := "Enter to extract · Esc to cancel"
	switch r.hover {
	case "extract":
		hint = fmt.Sprintf("One PDF with pages %d-%d", r.from, r.to)
		if r.from == r.to {
			hint = fmt.Sprintf("One PDF with page %d", r.from)
		}
	case "split":
		hint = fmt.Sprintf("%d PDFs, one for each page", r.to-r.from+1)
	}
	shadowText(cv, hint, c-px(100), c+px(64), px(200), px(20), 11*s, s, false, colWhite)
}
