package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The folder browser of "Move to…": the wheel shows one folder per
// segment, so the user can walk through the file system without leaving the
// gesture.
//
//   - resting on a folder for a moment opens it (spring-loaded, also while dragging);
//   - "Up" goes to the parent folder, "More…" shows the next folders;
//   - the cream centre means "here": dropping (or clicking) on it moves the
//     files into the folder being shown; dropping on a folder moves them into
//     that folder directly;
//   - the first level lists the usual places (Desktop, Documents, drives...).

type folderEntryKind int

const (
	entFolder folderEntryKind = iota
	entUp
	entMore
	entNone // empty filler segment (a wheel needs at least 3 segments)
)

type folderEntry struct {
	name string
	path string
	kind folderEntryKind
}

type folderState struct {
	dir  string        // folder being shown; "" = the list of places
	all  []folderEntry // its subfolders (or the places)
	page int
	view []folderEntry // segments currently shown
}

const foldersPerPage = 8

// Platform hooks: the list of places and how to recognise hidden folders.
var (
	placesFn = defaultPlaces
	hiddenFn = func(path, name string) bool { return strings.HasPrefix(name, ".") }
)

func defaultPlaces() []folderEntry {
	home, _ := os.UserHomeDir()
	return []folderEntry{{name: "Home", path: home}}
}

// folderCenterR is the radius of the "Move here" centre.
func (w *wheelModel) folderCenterR() int { return int(46 * w.scale) }

// enterFolders switches to the folder browser, starting from the places.
func (w *wheelModel) enterFolders() Outcome {
	w.mode = modeFolders
	w.openDir("")
	return Outcome{Resize: true}
}

// openDir lists dir ("" = places) and lays out the segments.
func (w *wheelModel) openDir(dir string) {
	w.fd = folderState{dir: dir}
	if dir == "" {
		for _, p := range placesFn() {
			if st, err := os.Stat(p.path); err == nil && st.IsDir() && !w.isDragged(p.path) {
				w.fd.all = append(w.fd.all, p)
			}
		}
	} else if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if !isDirEntry(p, e) || hiddenFn(p, e.Name()) || w.isDragged(p) {
				continue
			}
			w.fd.all = append(w.fd.all, folderEntry{name: e.Name(), path: p})
		}
		sort.Slice(w.fd.all, func(i, j int) bool {
			return strings.ToLower(w.fd.all[i].name) < strings.ToLower(w.fd.all[j].name)
		})
	}
	w.buildView()
}

// isDirEntry follows symbolic links / junctions to see whether p is a folder.
func isDirEntry(p string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink != 0 {
		st, err := os.Stat(p)
		return err == nil && st.IsDir()
	}
	return false
}

// isDragged reports whether p is one of the moved items (a folder cannot be moved into itself).
func (w *wheelModel) isDragged(p string) bool {
	for _, d := range w.paths {
		if strings.EqualFold(filepath.Clean(d), filepath.Clean(p)) {
			return true
		}
	}
	return false
}

// buildView picks the segments for the current page.
func (w *wheelModel) buildView() {
	f := &w.fd
	f.view = nil
	if f.dir != "" {
		f.view = append(f.view, folderEntry{name: "Up", kind: entUp})
	}
	room := foldersPerPage + 1 // without "More…" one more folder fits
	if len(f.all) > room {
		room = foldersPerPage
	}
	start := f.page * room
	if start >= len(f.all) {
		start, f.page = 0, 0
	}
	end := min(start+room, len(f.all))
	f.view = append(f.view, f.all[start:end]...)
	if len(f.all) > room {
		f.view = append(f.view, folderEntry{name: "More…", kind: entMore})
	}
	for len(f.view) < 3 {
		f.view = append(f.view, folderEntry{kind: entNone})
	}
	w.sel = -1
	w.layout(len(f.view))
}

// folderOpen navigates according to segment i (folder, Up or More…).
func (w *wheelModel) folderOpen(i int) Outcome {
	if i < 0 || i >= len(w.fd.view) {
		return Outcome{}
	}
	e := w.fd.view[i]
	switch e.kind {
	case entFolder:
		w.openDir(e.path)
	case entUp:
		parent := filepath.Dir(w.fd.dir)
		if parent == w.fd.dir || w.fd.dir == "" { // a drive or "/" root: back to the places
			parent = ""
		}
		w.openDir(parent)
	case entMore:
		w.fd.page++
		w.buildView()
	default:
		return Outcome{}
	}
	return Outcome{Resize: true}
}

// folderHover highlights the segment (or the centre) under the pointer.
func (w *wheelModel) folderHover(x, y int) Outcome {
	h := w.hit(x, y)
	if h >= 0 && w.fd.view[h].kind == entNone {
		h = -1
	}
	if h == w.sel {
		return Outcome{}
	}
	w.sel = h
	return Outcome{Redraw: true, Dwell: h >= 0} // resting on a folder, Up or More… opens it
}

// moveHere is the action of the centre.
func (w *wheelModel) moveHere() Outcome {
	if w.fd.dir == "" {
		return Outcome{} // the places list is not a folder
	}
	return Outcome{Close: true, Op: "move", Paths: w.paths, Dest: w.fd.dir}
}

// folderClick: centre = move here, segments = navigate.
func (w *wheelModel) folderClick(x, y int) Outcome {
	switch h := w.hit(x, y); {
	case h == -2:
		return w.moveHere()
	case h >= 0:
		return w.folderOpen(h)
	}
	return Outcome{}
}

// folderDrop: centre = move here, folder = move into it, Up/More… = navigate
// (the browser then stays open and works with clicks).
func (w *wheelModel) folderDrop(x, y int) Outcome {
	switch h := w.hit(x, y); {
	case h == -2:
		if w.fd.dir == "" {
			return Outcome{Redraw: true}
		}
		return w.moveHere()
	case h >= 0 && w.fd.view[h].kind == entFolder:
		return Outcome{Close: true, Op: "move", Paths: w.paths, Dest: w.fd.view[h].path}
	case h >= 0:
		return w.folderOpen(h)
	}
	return Outcome{Redraw: true}
}

func (w *wheelModel) folderKey(k Key) Outcome {
	n := len(w.fd.view)
	switch k {
	case KeyEsc:
		return Outcome{Close: true}
	case KeyBackspace:
		if w.fd.dir != "" {
			return w.folderOpen(0) // "Up" is always the first segment
		}
	case KeyRight, KeyDown, KeyTab:
		w.sel = (max(w.sel, -1) + 1) % n
		return Outcome{Redraw: true}
	case KeyLeft, KeyUp:
		if w.sel < 0 {
			w.sel = n - 1
		} else {
			w.sel = (w.sel - 1 + n) % n
		}
		return Outcome{Redraw: true}
	case KeyEnter:
		if w.sel >= 0 {
			return w.folderOpen(w.sel)
		}
		return w.moveHere()
	case KeySpace:
		return w.moveHere()
	}
	return Outcome{}
}

func (w *wheelModel) folderHoverInfo() (string, bool, bool) {
	switch {
	case w.sel == -2 && w.fd.dir != "":
		return "Move to " + displayName(w.fd.dir), true, true
	case w.sel >= 0 && w.fd.view[w.sel].kind == entFolder:
		return "Move to " + w.fd.view[w.sel].name, true, true
	}
	return "", false, false
}

// displayName is the last element of a path ("C:\" stays "C:").
func displayName(p string) string {
	b := filepath.Base(p)
	if b == "." || b == string(filepath.Separator) || strings.HasSuffix(b, ":\\") || b == "" {
		return strings.TrimRight(p, `\/`)
	}
	return b
}

// paintFolders draws the folder browser.
func (w *wheelModel) paintFolders(cv Canvas, c int) {
	s := w.scale
	labels := make([]string, len(w.fd.view))
	for i, e := range w.fd.view {
		labels[i] = e.name
	}
	w.paintSlices(cv, c, labels, func(i int) segStyle {
		e := w.fd.view[i]
		switch {
		case e.kind == entNone:
			return segStyle{col: colFleshPale, juice: 30}
		case i == w.sel:
			return segStyle{col: colFleshHi, pop: 4 * s, juice: 80}
		case e.kind != entFolder: // Up / More…
			return segStyle{col: argb(255, 255, 172, 64), juice: 50}
		}
		return segStyle{col: ripe(i), juice: 50}
	})

	// the centre: "Move here" + the folder name
	r := w.folderCenterR()
	fill := colCream
	if w.sel == -2 && w.fd.dir != "" {
		fill = argb(255, 255, 226, 170)
	}
	cv.Circle(c, c+int(1.5*s), r+int(1.5*s), argb(50, 120, 40, 0))
	cv.Circle(c, c, r, fill)
	tw := int(float64(r) * 1.75)
	if w.fd.dir == "" {
		cv.Text("Choose a", c-tw/2, c-int(17*s), tw, int(16*s), 11*s, true, colLabel)
		cv.Text("folder", c-tw/2, c+int(1*s), tw, int(16*s), 11*s, true, colLabel)
		return
	}
	cv.Text("Move here", c-tw/2, c-int(19*s), tw, int(16*s), 11*s, true, colAccent)
	cv.Text(displayName(w.fd.dir), c-tw/2, c-int(1*s), tw, int(18*s), 12*s, true, colDeep)
	if n := len(w.fd.all); n == 0 {
		cv.Text("no folders", c-tw/2, c+int(16*s), tw, int(14*s), 9.5*s, false, colLabel)
	}
}
