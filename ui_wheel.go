package main

import (
	"math"
	"os"
	"strings"

	"mandarino/engine"
)

// wheelModel is the platform-independent part of the wheel: layout,
// hit testing, mouse/keyboard handling and painting. The platform code creates
// a window of wheelModel.size pixels, forwards input events and carries out
// what the returned Outcome asks for.
//
// There are two wheels:
//
//   - the convert wheel (Shift): formats for the selected files (engine.ActionsFor);
//   - the actions wheel (Ctrl): Bluetooth, Copy, Copy path, Move to…
//
// and three modes: the wheel itself, the page picker for one PDF
// (ui_range.go) and the folder browser of "Move to…" (ui_folders.go).
//
// On Windows the wheel opens while the user is still dragging the files,
// before Windows says which files they are. Until then it is "pending": a
// pale wheel without labels; SetPaths fills it in.

// Outcome tells the platform what to do after an input event.
type Outcome struct {
	Redraw bool           // repaint the window
	Resize bool           // the window size changed (size field), repaint too
	Close  bool           // hide the wheel
	Pick   *engine.Action // run this conversion on the files (implies Close)
	Op     string         // run this file action: "bt", "copy", "path" or "move" (implies Close)
	Paths  []string       // ...on these paths
	Dest   string         // destination folder for "move"
	Error  string         // show this message to the user (implies Close)
	Dwell  bool           // the pointer rests on something that opens after a moment: (re)start the dwell timer
}

// dwellMillis is how long the pointer must rest on "Move to…" or on a folder
// before it opens (like spring-loaded folders).
const dwellMillis = 650

type wheelKind int

const (
	kindConvert wheelKind = iota // Shift: conversions
	kindActions                  // Ctrl: file actions
)

const (
	modeWheel   = iota // choosing an entry
	modeRange          // "Split" page picker for a single PDF (ui_range.go)
	modeFolders        // folder browser for "Move to…" (ui_folders.go)
)

type wheelModel struct {
	fruit
	kind     wheelKind
	acts     []engine.Action
	paths    []string // everything selected or dragged (files and folders)
	files    []string // the regular files among paths (used by conversions)
	sel      int      // highlighted entry; -1 = none, -2 = the centre (folder browser)
	mode     int
	rg       rangeState
	fd       folderState
	hint     string // message in the middle of a wheel without entries
	Dragging bool   // set by the platform while the files are still being dragged
}

// fileActions are the entries of the Ctrl wheel.
func fileActions() []engine.Action {
	return []engine.Action{
		{ID: "op:bt", Label: "Bluetooth", Desc: "Send with Bluetooth"},
		{ID: "op:copy", Label: "Copy", Desc: "Copy (paste it anywhere)"},
		{ID: "op:path", Label: "Path", Desc: "Copy the path"},
		{ID: "op:move", Label: "Move to…", Desc: "Move to a folder"},
	}
}

// newWheelModel builds a wheel of the given kind for paths (nil = pending).
func newWheelModel(kind wheelKind, paths []string, scale float64) *wheelModel {
	w := &wheelModel{kind: kind, sel: -1}
	w.scale = scale
	if paths != nil {
		w.setPaths(paths)
	}
	w.layout(len(w.acts))
	return w
}

// setPaths computes the entries for paths.
func (w *wheelModel) setPaths(paths []string) {
	w.paths, w.files, w.acts, w.hint = paths, nil, nil, ""
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			w.files = append(w.files, p)
		}
	}
	switch {
	case len(paths) == 0:
		w.hint = "Nothing selected"
	case w.kind == kindActions:
		w.acts = fileActions()
	case len(w.files) == 0:
		w.hint = "Folders can't be converted"
	default:
		acts, err := engine.ActionsFor(w.files)
		w.acts = acts
		if err != nil {
			w.hint = shortError(err)
		}
	}
}

// layout sets the geometry for n slices.
func (w *wheelModel) layout(n int) {
	s := w.scale
	// each label needs about 72 px of arc inside its segment
	w.radius = int(math.Max(88*s, 11.5*float64(n)*s))
	w.fleshR = w.radius + int(42*s)
	w.outerR = w.fleshR + int(14*s) // white pith + peel
	w.centerR = int(16 * s)
	w.size = 2*w.outerR + int(16*s) // room for the shadow
}

// Pending reports whether the wheel is still waiting for the dragged files.
func (w *wheelModel) Pending() bool { return w.paths == nil && w.hint == "" }

// SetPaths fills a pending wheel. The size may change, so it asks for a resize.
func (w *wheelModel) SetPaths(paths []string) Outcome {
	if paths == nil {
		paths = []string{}
	}
	w.setPaths(paths)
	w.sel = -1
	w.layout(len(w.acts))
	return Outcome{Resize: true}
}

// SetKind switches between the convert and the actions wheel (Shift / Ctrl
// pressed again while dragging).
func (w *wheelModel) SetKind(kind wheelKind) Outcome {
	if kind == w.kind && w.mode == modeWheel {
		return Outcome{}
	}
	w.kind, w.mode = kind, modeWheel
	if w.paths != nil {
		return w.SetPaths(w.paths)
	}
	return Outcome{Redraw: true}
}

// shortError turns engine errors into a few words that fit inside the wheel.
func shortError(err error) string {
	switch err {
	case engine.ErrMixed:
		return "Mixed file types"
	case engine.ErrNoFiles:
		return "Folders can't be converted"
	}
	return "Not supported"
}

// slices is the number of segments in the current mode.
func (w *wheelModel) slices() int {
	if w.mode == modeFolders {
		return len(w.fd.view)
	}
	return len(w.acts)
}

// Contains reports whether window point (x, y) is on the fruit.
func (w *wheelModel) Contains(x, y int) bool {
	c := float64(w.size / 2)
	return math.Hypot(float64(x)-c, float64(y)-c) <= float64(w.outerR)
}

// hit returns the segment under window point (x, y), -2 for the centre of the
// folder browser, or -1 for nothing.
func (w *wheelModel) hit(x, y int) int {
	c := float64(w.size / 2)
	dx, dy := float64(x)-c, float64(y)-c
	d := math.Hypot(dx, dy)
	n := w.slices()
	if w.mode == modeFolders && d < float64(w.folderCenterR()) {
		return -2
	}
	if d < float64(w.centerR) || d > float64(w.outerR) || n == 0 {
		return -1
	}
	step := 2 * math.Pi / float64(n)
	a := math.Atan2(dy, dx) + math.Pi/2 // 0 = straight up, clockwise
	if a < 0 {
		a += 2 * math.Pi
	}
	return int(math.Round(a/step)) % n
}

// pick runs entry i of the wheel.
func (w *wheelModel) pick(i int) Outcome {
	if i < 0 || i >= len(w.acts) {
		return Outcome{}
	}
	a := w.acts[i]
	switch {
	case a.ID == "pdf:split" && len(w.files) == 1:
		return w.enterRange() // one PDF: ask from which page to which page
	case a.ID == "op:move":
		return w.enterFolders()
	case strings.HasPrefix(a.ID, "op:"):
		return Outcome{Close: true, Op: strings.TrimPrefix(a.ID, "op:"), Paths: w.paths}
	}
	return Outcome{Close: true, Pick: &a}
}

// Move handles the pointer moving over the window (also during a drag).
func (w *wheelModel) Move(x, y int) Outcome {
	switch w.mode {
	case modeRange:
		return w.rangeHover(x, y)
	case modeFolders:
		return w.folderHover(x, y)
	}
	if h := w.hit(x, y); h >= 0 && h != w.sel {
		w.sel = h
		// resting on "Move to…" opens the folder browser
		return Outcome{Redraw: true, Dwell: w.acts[h].ID == "op:move"}
	}
	return Outcome{}
}

// DwellFire is called when the pointer has rested on the same entry for dwellMillis.
func (w *wheelModel) DwellFire() Outcome {
	switch w.mode {
	case modeWheel:
		if w.sel >= 0 && w.sel < len(w.acts) && w.acts[w.sel].ID == "op:move" {
			return w.enterFolders()
		}
	case modeFolders:
		return w.folderOpen(w.sel)
	}
	return Outcome{}
}

// Leave handles the pointer leaving the window.
func (w *wheelModel) Leave() Outcome {
	switch {
	case w.mode == modeRange && w.rg.hover != "":
		w.rg.hover = ""
		return Outcome{Redraw: true}
	case w.mode != modeRange && w.sel != -1:
		w.sel = -1
		return Outcome{Redraw: true}
	}
	return Outcome{}
}

// Click handles a left click (button released) at window point (x, y).
// Clicking the heart in the middle of the wheel cancels.
func (w *wheelModel) Click(x, y int) Outcome {
	switch w.mode {
	case modeRange:
		return w.rangeClick(x, y)
	case modeFolders:
		return w.folderClick(x, y)
	}
	if h := w.hit(x, y); h >= 0 {
		return w.pick(h)
	}
	c := float64(w.size / 2)
	if math.Hypot(float64(x)-c, float64(y)-c) < float64(w.centerR) {
		return Outcome{Close: true}
	}
	return Outcome{}
}

// Drop handles files dropped at window point (x, y) at the end of a drag.
// Unlike a click, dropping on a folder moves the files into it.
func (w *wheelModel) Drop(x, y int) Outcome {
	w.Dragging = false
	switch w.mode {
	case modeFolders:
		return w.folderDrop(x, y)
	case modeRange:
		return Outcome{}
	}
	h := w.hit(x, y)
	if h < 0 {
		return Outcome{Close: true}
	}
	return w.pick(h) // "Move to…" stays open as a clickable folder browser
}

// Scroll handles the mouse wheel (page picker only).
func (w *wheelModel) Scroll(up bool) Outcome {
	if w.mode == modeRange {
		return w.rangeScroll(up)
	}
	return Outcome{}
}

// Key handles a key press. Arrows/Tab move the highlight, Enter/Space run it,
// digits 1-9 pick an entry directly, Esc closes.
func (w *wheelModel) Key(k Key) Outcome {
	switch w.mode {
	case modeRange:
		return w.rangeKey(k)
	case modeFolders:
		return w.folderKey(k)
	}
	n := len(w.acts)
	if n == 0 || k == KeyEsc {
		return Outcome{Close: true}
	}
	switch k {
	case KeyRight, KeyDown, KeyTab:
		w.sel = (w.sel + 1 + n) % n
		return Outcome{Redraw: true}
	case KeyLeft, KeyUp:
		if w.sel < 0 {
			w.sel = n - 1
		} else {
			w.sel = (w.sel - 1 + n) % n
		}
		return Outcome{Redraw: true}
	case KeyEnter, KeySpace:
		return w.pick(w.sel)
	}
	if d, ok := k.digit(); ok && d >= 1 {
		return w.pick(d - 1)
	}
	return Outcome{}
}

// HoverInfo describes what a drop would do right now: the text shown under
// the dragged files, whether a drop is possible, and whether it moves the files.
func (w *wheelModel) HoverInfo() (desc string, ok, move bool) {
	switch w.mode {
	case modeWheel:
		if w.sel >= 0 && w.sel < len(w.acts) {
			return w.acts[w.sel].Desc, true, false
		}
	case modeFolders:
		return w.folderHoverInfo()
	}
	return "", false, false
}

// Paint draws the current mode.
func (w *wheelModel) Paint(cv Canvas) {
	c := w.size / 2
	switch w.mode {
	case modeRange:
		w.paintRange(cv, c)
		return
	case modeFolders:
		w.paintFolders(cv, c)
		return
	}
	s := w.scale
	labels := make([]string, len(w.acts))
	for i, a := range w.acts {
		labels[i] = a.Label
	}
	if len(labels) == 0 { // pending, or nothing to offer
		w.draw(cv, c, 8, func(i int) segStyle { return segStyle{col: colFleshPale, juice: 35} })
		if w.hint != "" {
			pw, ph := int(176*s), int(36*s)
			roundRect(cv, c-pw/2, c-ph/2, pw, ph, 18*s, colCream)
			cv.Text(w.hint, c-pw/2+int(8*s), c-ph/2, pw-int(16*s), ph, 13*s, true, colAccent)
		}
		return
	}
	w.paintSlices(cv, c, labels, func(i int) segStyle {
		if i == w.sel {
			return segStyle{col: colFleshHi, pop: 4 * s, juice: 80}
		}
		return segStyle{col: ripe(i), juice: 50}
	})
}

// paintSlices draws the fruit with one label per segment.
func (w *wheelModel) paintSlices(cv Canvas, c int, labels []string, style func(i int) segStyle) {
	s := w.scale
	n := len(labels)
	step := 360 / float64(n)
	w.draw(cv, c, n, style)
	arc := 2 * math.Pi * float64(w.radius) / float64(n)
	lw := int(math.Min(arc-6*s, 92*s))
	lh := int(30 * s)
	for i, label := range labels {
		if label == "" {
			continue
		}
		a := (-90 + float64(i)*step) * math.Pi / 180
		lx := c + int(math.Round(float64(w.radius)*math.Cos(a)))
		ly := c + int(math.Round(float64(w.radius)*math.Sin(a)))
		px := 15.5 * s
		if len([]rune(label)) > 5 {
			px = 12.5 * s
		}
		shadowText(cv, label, lx-lw/2, ly-lh/2, lw, lh, px, s, true, colWhite)
	}
}
