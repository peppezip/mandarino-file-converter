package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// point returns window coordinates in the middle of segment i of n.
func point(w *wheelModel, i, n int) (int, int) {
	a := (-90 + float64(i)*360/float64(n)) * math.Pi / 180
	c := float64(w.size / 2)
	r := float64(w.radius)
	return int(c + r*math.Cos(a)), int(c + r*math.Sin(a))
}

func TestWheels(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "photo.png")
	os.WriteFile(img, []byte("x"), 0o644)
	sub := filepath.Join(dir, "folder")
	os.Mkdir(sub, 0o755)

	// convert wheel
	w := newWheelModel(kindConvert, []string{img}, 1)
	if len(w.acts) == 0 || w.hint != "" {
		t.Fatalf("convert wheel: %v %q", w.acts, w.hint)
	}
	// folders cannot be converted
	if w := newWheelModel(kindConvert, []string{sub}, 1); w.hint == "" || len(w.acts) != 0 {
		t.Fatal("a folder must show a hint")
	}
	// pending wheel, then the dragged paths arrive
	p := newWheelModel(kindActions, nil, 1)
	if !p.Pending() {
		t.Fatal("expected pending")
	}
	if out := p.SetPaths([]string{img, sub}); !out.Resize || len(p.acts) != 4 {
		t.Fatalf("actions wheel: %+v", p.acts)
	}
	// Shift <-> Ctrl switch while dragging
	p.SetKind(kindConvert)
	if len(p.acts) == 0 || p.acts[0].ID == "op:bt" {
		t.Fatal("switch to convert failed")
	}
	p.SetKind(kindActions)

	// "Copy" returns the file action with every path
	x, y := point(p, 1, 4)
	if out := p.Drop(x, y); out.Op != "copy" || len(out.Paths) != 2 {
		t.Fatalf("copy: %+v", out)
	}
	// resting on "Move to…" opens the folder browser
	p = newWheelModel(kindActions, []string{img}, 1)
	x, y = point(p, 3, 4)
	if out := p.Move(x, y); !out.Dwell {
		t.Fatal("Move to… must ask for a dwell")
	}
	if out := p.DwellFire(); !out.Resize || p.mode != modeFolders || p.fd.dir != "" {
		t.Fatalf("folder browser not opened: %+v", out)
	}
}

func TestFolderBrowser(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "photo.png")
	os.WriteFile(file, []byte("x"), 0o644)
	for i := 0; i < 12; i++ {
		os.Mkdir(filepath.Join(dir, fmt.Sprintf("dir%02d", i)), 0o755)
	}
	os.Mkdir(filepath.Join(dir, ".hidden"), 0o755)

	w := newWheelModel(kindActions, []string{file}, 1)
	w.mode = modeFolders
	w.openDir(dir)
	// Up + 8 folders + More…
	if n := len(w.fd.view); n != 10 || w.fd.view[0].kind != entUp || w.fd.view[9].kind != entMore {
		t.Fatalf("view: %+v", w.fd.view)
	}
	if len(w.fd.all) != 12 {
		t.Fatalf("hidden folders must be skipped: %d", len(w.fd.all))
	}
	n := len(w.fd.view)
	// More… shows the next page
	if out := w.folderOpen(9); !out.Resize || w.fd.view[1].name != "dir08" {
		t.Fatalf("page 2: %+v", w.fd.view)
	}
	// dropping on a folder moves the files into it
	w.openDir(dir)
	x, y := point(w, 1, n)
	out := w.Drop(x, y)
	if out.Op != "move" || out.Dest != filepath.Join(dir, "dir00") {
		t.Fatalf("drop on folder: %+v", out)
	}
	// the centre moves into the folder being shown
	w.openDir(filepath.Join(dir, "dir03"))
	c := w.size / 2
	if out := w.Drop(c, c); out.Op != "move" || out.Dest != filepath.Join(dir, "dir03") {
		t.Fatalf("drop on centre: %+v", out)
	}
	// an empty folder still gets at least 3 segments, Up goes back
	w.openDir(filepath.Join(dir, "dir03"))
	if len(w.fd.view) != 3 || w.fd.view[1].kind != entNone {
		t.Fatalf("empty folder: %+v", w.fd.view)
	}
	w.folderOpen(0)
	if w.fd.dir != dir {
		t.Fatalf("Up: %s", w.fd.dir)
	}
	// clicking a folder opens it instead of moving
	x, y = point(w, 2, len(w.fd.view))
	if out := w.Click(x, y); out.Op != "" || w.fd.dir != filepath.Join(dir, "dir01") {
		t.Fatalf("click: %+v %s", out, w.fd.dir)
	}
}
