//go:build linux

package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"

	"mandarino/engine"
)

// "mandarino --demo DIR" renders the animations used in the README
// (docs/demo/*.gif and *.mp4) and the GitHub social preview image.
//
// The scenes are illustrations: a simple file window, a mouse pointer and the
// dragged file are drawn here, while the wheels are the real ones
// (wheelModel, progressModel) driven exactly as the Windows app drives them.
// Frames are written as PNG files and assembled with ffmpeg.

const (
	demoW, demoH = 1280, 720
	demoFPS      = 30
)

// shifted draws on another Canvas with an offset (to place a wheel in the scene).
type shifted struct {
	Canvas
	dx, dy int
}

func (s shifted) Circle(cx, cy, r int, c Color) { s.Canvas.Circle(cx+s.dx, cy+s.dy, r, c) }
func (s shifted) Ring(cx, cy, r int, w float64, c Color) {
	s.Canvas.Ring(cx+s.dx, cy+s.dy, r, w, c)
}
func (s shifted) RadialEllipse(cx, cy int, a, d, l, w float64, c Color) {
	s.Canvas.RadialEllipse(cx+s.dx, cy+s.dy, a, d, l, w, c)
}
func (s shifted) RoundedPolygon(pts [][2]float32, r float64, c Color) {
	moved := make([][2]float32, len(pts))
	for i, p := range pts {
		moved[i] = [2]float32{p[0] + float32(s.dx), p[1] + float32(s.dy)}
	}
	s.Canvas.RoundedPolygon(moved, r, c)
}
func (s shifted) Text(t string, x, y, w, h int, px float64, bold bool, c Color) {
	s.Canvas.Text(t, x+s.dx, y+s.dy, w, h, px, bold, c)
}

// ---------- scene elements ----------

type demoFile struct {
	name, ext string
	x, y      int // top-left of the icon
	selected  bool
	hidden    bool
}

type demoScene struct {
	caption  string
	files    []demoFile
	cursorX  int
	cursorY  int
	pressed  bool
	ghost    *demoFile // file being dragged (drawn next to the pointer)
	label    string    // text under the dragged file ("+ Convert to MP4")
	key      string    // key shown in the keycap ("Shift", "Ctrl"), "" = none
	keyDown  bool
	wheel    *wheelModel
	progress *progressModel
	cx, cy   int // centre of the wheel on screen
	toastT   string
	toastB   string
}

var (
	colDesk   = argb(255, 34, 52, 82)
	colWindow = argb(255, 250, 250, 252)
	colBar    = argb(255, 238, 240, 244)
	colInk    = argb(255, 40, 44, 52)
	colMuted  = argb(255, 120, 126, 138)
	colSelect = argb(255, 204, 228, 255)
)

func extColor(ext string) Color {
	switch ext {
	case "MOV", "MP4":
		return argb(255, 124, 77, 255)
	case "PDF":
		return argb(255, 229, 57, 53)
	case "JPG", "PNG":
		return argb(255, 46, 160, 90)
	}
	return argb(255, 96, 125, 139)
}

// drawFileIcon draws a document icon with a coloured band showing its type.
func drawFileIcon(cv Canvas, x, y int, ext string, alpha uint8) {
	a := func(c Color) Color { return Color(uint32(alpha)<<24 | uint32(c)&0xFFFFFF) }
	roundRect(cv, x, y, 64, 80, 7, a(argb(255, 200, 205, 214)))
	roundRect(cv, x+2, y+2, 60, 76, 6, a(colWindow))
	for i := 0; i < 4; i++ { // "text lines"
		roundRect(cv, x+12, y+12+i*9, 40-(i%2)*12, 4, 2, a(argb(255, 218, 222, 230)))
	}
	roundRect(cv, x+2, y+52, 60, 22, 4, a(extColor(ext)))
	cv.Text(ext, x+2, y+52, 60, 22, 13, true, a(colWhite))
}

func drawWindow(cv Canvas, files []demoFile) {
	x, y, w, h := 70, 80, 600, 470
	roundRect(cv, x+4, y+8, w, h, 14, argb(70, 0, 0, 0)) // shadow
	roundRect(cv, x, y, w, h, 14, colWindow)
	roundRect(cv, x, y, w, 48, 14, colBar)
	roundRect(cv, x, y+30, w, 18, 0.5, colBar)
	cv.Text("Documents", x+20, y+12, 200, 24, 16, true, colInk)
	// minimize, maximize, close (drawn, so they do not depend on the font)
	roundRect(cv, x+w-128, y+24, 14, 2, 0.5, colMuted)
	roundRect(cv, x+w-84, y+17, 14, 14, 2, colMuted)
	roundRect(cv, x+w-82, y+19, 10, 10, 1, colBar)
	cv.Text("×", x+w-48, y+10, 24, 28, 22, false, colMuted)
	roundRect(cv, x, y+48, w, 1, 0.4, argb(255, 225, 228, 234))
	for _, f := range files {
		if f.hidden {
			continue
		}
		if f.selected {
			roundRect(cv, f.x-24, f.y-10, 112, 132, 8, colSelect)
		}
		drawFileIcon(cv, f.x, f.y, f.ext, 255)
		cv.Text(f.name, f.x-30, f.y+86, 124, 22, 13, false, colInk)
	}
}

// drawCursor draws a mouse arrow with its tip at (x, y): a black arrow with a white border.
func drawCursor(cv Canvas, x, y int, pressed bool) {
	k := float32(1.15)
	if pressed {
		k = 1.05
	}
	shape := [][2]float32{{0, 0}, {0, 22}, {6, 17}, {10, 26}, {14, 24}, {10, 15.5}, {17, 15.5}}
	pts := make([][2]float32, len(shape))
	for i, p := range shape {
		pts[i] = [2]float32{float32(x) + p[0]*k, float32(y) + p[1]*k}
	}
	cv.RoundedPolygon(pts, 2.2, colWhite)              // border
	cv.RoundedPolygon(pts, 0.3, argb(255, 20, 20, 24)) // arrow
}

func drawScene(cv Canvas, st *demoScene) {
	// desktop
	roundRect(cv, -20, -20, demoW+40, demoH+40, 0.5, colDesk)
	cv.Circle(1150, 120, 320, argb(28, 255, 255, 255))
	cv.Circle(120, 700, 260, argb(20, 255, 255, 255))
	drawWindow(cv, st.files)

	if st.caption != "" {
		roundRect(cv, demoW/2-300, 18, 600, 46, 23, argb(200, 15, 20, 30))
		cv.Text(st.caption, demoW/2-290, 18, 580, 46, 20, true, colWhite)
	}
	if st.wheel != nil {
		st.wheel.Paint(shifted{cv, st.cx - st.wheel.size/2, st.cy - st.wheel.size/2})
	}
	if st.progress != nil {
		st.progress.Paint(shifted{cv, st.cx - st.progress.size/2, st.cy - st.progress.size/2})
	}
	if st.ghost != nil {
		drawFileIcon(cv, st.cursorX+8, st.cursorY+10, st.ghost.ext, 170)
		if st.label != "" {
			w := 26 + 9*len([]rune(st.label))
			roundRect(cv, st.cursorX+6, st.cursorY+96, w, 30, 6, argb(255, 255, 255, 255))
			roundRect(cv, st.cursorX+6, st.cursorY+96, w, 30, 6, argb(40, 0, 90, 200))
			cv.Text(st.label, st.cursorX+10, st.cursorY+96, w-8, 30, 14, true, argb(255, 0, 84, 180))
		}
	}
	if st.key != "" { // keycap at the bottom, for the viewer
		roundRect(cv, demoW/2-95, demoH-92, 190, 64, 16, argb(190, 15, 20, 30))
		fill, ink := colWhite, colInk
		dy := 0
		if st.keyDown {
			fill, ink, dy = colFleshHi, colWhite, 3
		}
		roundRect(cv, demoW/2-70, demoH-82+dy, 140, 44, 9, argb(255, 160, 165, 175))
		roundRect(cv, demoW/2-70, demoH-84+dy, 140, 42, 9, fill)
		cv.Text(st.key, demoW/2-70, demoH-84+dy, 140, 42, 19, true, ink)
	}
	if st.toastT != "" {
		x, y := demoW-400, demoH-120
		roundRect(cv, x+3, y+5, 370, 92, 12, argb(80, 0, 0, 0))
		roundRect(cv, x, y, 370, 92, 12, argb(255, 40, 42, 48))
		drawIcon(shifted{cv, x + 16, y + 24}, 44)
		cv.Text(st.toastT, x+72, y+16, 280, 26, 16, true, colWhite)
		cv.Text(st.toastB, x+72, y+44, 280, 26, 14, false, argb(255, 200, 204, 212))
	}
	drawCursor(cv, st.cursorX, st.cursorY, st.pressed)
}

// ---------- timeline helpers ----------

func ease(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// tween returns the eased position between a and b for time t in [t0, t1].
func tween(t, t0, t1 float64, a, b [2]int) (int, int) {
	k := ease((t - t0) / (t1 - t0))
	return a[0] + int(float64(b[0]-a[0])*k), a[1] + int(float64(b[1]-a[1])*k)
}

// slicePoint returns the screen point in the middle of slice i (label radius).
func slicePoint(m *wheelModel, cx, cy, i int) [2]int {
	n := m.slices()
	a := (-90 + float64(i)*360/float64(n)) * math.Pi / 180
	r := float64(m.radius)
	return [2]int{cx + int(r*math.Cos(a)), cy + int(r*math.Sin(a))}
}

// hover moves the pointer over the wheel and updates its highlight.
func hover(st *demoScene) {
	if st.wheel != nil {
		st.wheel.Move(st.cursorX-(st.cx-st.wheel.size/2), st.cursorY-(st.cy-st.wheel.size/2))
		st.label = ""
		if d, ok, move := st.wheel.HoverInfo(); ok {
			if move {
				st.label = "→ " + d
			} else {
				st.label = "+ " + d
			}
		}
	}
}

func indexOf(acts []engine.Action, id string) int {
	for i, a := range acts {
		if a.ID == id {
			return i
		}
	}
	return 0
}

func folderIndex(m *wheelModel, name string) int {
	for i, e := range m.fd.view {
		if e.name == name {
			return i
		}
	}
	return 0
}

// ---------- the two scenes ----------

// sceneConvert: drag a video, press Shift, drop on MP4, watch the progress.
func sceneConvert(tmp string) (float64, func(t float64) *demoScene) {
	video := filepath.Join(tmp, "holiday.mov")
	os.WriteFile(video, []byte("x"), 0o644)
	files := []demoFile{
		{name: "holiday.mov", ext: "MOV", x: 130, y: 170},
		{name: "invoice.pdf", ext: "PDF", x: 270, y: 170},
		{name: "beach.jpg", ext: "JPG", x: 410, y: 170},
		{name: "holiday.mp4", ext: "MP4", x: 130, y: 340, hidden: true},
	}
	start, onFile, dropAt := [2]int{1050, 640}, [2]int{162, 210}, [2]int{900, 380}
	cx, cy := 900, 380
	wheel := newWheelModel(kindConvert, []string{video}, 1)
	mp4 := slicePoint(wheel, cx, cy, indexOf(wheel.acts, "to:mp4"))
	prog := newProgressModel(1)

	return 7.6, func(t float64) *demoScene {
		st := &demoScene{files: append([]demoFile(nil), files...), cx: cx, cy: cy}
		switch {
		case t < 0.7:
			st.caption = "1 · Start dragging a file"
			st.cursorX, st.cursorY = tween(t, 0, 0.7, start, onFile)
		case t < 1.7:
			st.caption = "1 · Start dragging a file"
			st.pressed, st.files[0].selected = true, true
			st.cursorX, st.cursorY = tween(t, 0.9, 1.7, onFile, dropAt)
			if t > 0.9 {
				st.ghost = &files[0]
			}
		case t < 3.2:
			st.caption = "2 · Press Shift: the wheel opens"
			st.pressed, st.files[0].selected, st.ghost = true, true, &files[0]
			st.key, st.keyDown = "Shift", t < 2.3
			st.wheel = wheel
			st.cursorX, st.cursorY = tween(t, 2.0, 2.8, dropAt, mp4)
			if t > 2.4 {
				st.caption = "3 · Drop it on a format"
			}
			hover(st)
		case t < 5.9:
			st.caption = "Converting… the slices ripen as it goes"
			st.files[0].selected = true
			st.progress = prog
			st.cursorX, st.cursorY = tween(t, 3.2, 4.0, mp4, [2]int{1040, 560})
			prog.Update("MP4", 0, 1, ease((t-3.3)/2.0))
			if t > 5.4 {
				prog.Finish()
			}
			prog.phase = int(t * 16)
		default:
			st.caption = "The new file appears next to the original"
			st.files[0].selected = true
			st.files[3].hidden = false
			st.cursorX, st.cursorY = 1040, 560
			st.toastT, st.toastB = "Done!", "Created holiday.mp4"
		}
		return st
	}
}

// sceneActions: drag a PDF, press Ctrl, rest on Move to…, open Documents, drop on Work.
func sceneActions(tmp string) (float64, func(t float64) *demoScene) {
	pdf := filepath.Join(tmp, "report.pdf")
	os.WriteFile(pdf, []byte("x"), 0o644)
	places := []folderEntry{}
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Videos", "Music", "C:"} {
		p := filepath.Join(tmp, "places", name)
		os.MkdirAll(p, 0o755)
		places = append(places, folderEntry{name: name, path: p})
	}
	for _, sub := range []string{"Holidays", "Invoices", "Projects", "Recipes", "School", "Work"} {
		os.MkdirAll(filepath.Join(tmp, "places", "Documents", sub), 0o755)
	}
	placesFn = func() []folderEntry { return places }

	files := []demoFile{
		{name: "report.pdf", ext: "PDF", x: 130, y: 170},
		{name: "beach.jpg", ext: "JPG", x: 270, y: 170},
		{name: "notes.txt", ext: "TXT", x: 410, y: 170},
	}
	start, onFile, dropAt := [2]int{1050, 640}, [2]int{162, 210}, [2]int{900, 380}
	cx, cy := 900, 380

	// the wheel goes through three states; build each one once
	actions := newWheelModel(kindActions, []string{pdf}, 1)
	moveTo := slicePoint(actions, cx, cy, indexOf(actions.acts, "op:move"))
	placesWheel := newWheelModel(kindActions, []string{pdf}, 1)
	placesWheel.enterFolders()
	docs := slicePoint(placesWheel, cx, cy, folderIndex(placesWheel, "Documents"))
	docsWheel := newWheelModel(kindActions, []string{pdf}, 1)
	docsWheel.enterFolders()
	docsWheel.openDir(filepath.Join(tmp, "places", "Documents"))
	work := slicePoint(docsWheel, cx, cy, folderIndex(docsWheel, "Work"))

	return 8.4, func(t float64) *demoScene {
		st := &demoScene{files: append([]demoFile(nil), files...), cx: cx, cy: cy}
		switch {
		case t < 0.7:
			st.caption = "1 · Start dragging a file"
			st.cursorX, st.cursorY = tween(t, 0, 0.7, start, onFile)
		case t < 1.6:
			st.caption = "1 · Start dragging a file"
			st.pressed, st.files[0].selected = true, true
			st.cursorX, st.cursorY = tween(t, 0.8, 1.6, onFile, dropAt)
			if t > 0.8 {
				st.ghost = &files[0]
			}
		case t < 5.9:
			st.pressed, st.files[0].selected, st.ghost = true, true, &files[0]
			st.key, st.keyDown = "Ctrl", t < 2.2
			switch {
			case t < 3.25:
				st.caption = "2 · Press Ctrl: Bluetooth, Copy, Path, Move to…"
				st.wheel = actions
				st.cursorX, st.cursorY = tween(t, 1.9, 2.6, dropAt, moveTo)
				if t > 2.6 {
					st.caption = "3 · Rest on Move to… to see your folders"
				}
			case t < 4.75:
				st.caption = "4 · Rest on a folder to open it"
				st.wheel = placesWheel
				st.cursorX, st.cursorY = tween(t, 3.4, 4.1, moveTo, docs)
			default:
				st.caption = "5 · Drop it on a folder (or on Move here)"
				st.wheel = docsWheel
				st.cursorX, st.cursorY = tween(t, 4.9, 5.6, docs, work)
			}
			hover(st)
		default:
			st.caption = "Moved, without opening any window"
			st.files[0].hidden = true
			st.cursorX, st.cursorY = tween(t, 5.9, 6.6, work, [2]int{1040, 560})
			st.toastT, st.toastB = "Moved", "report.pdf moved to Work"
		}
		return st
	}
}

// renderScene writes the frames of a scene and encodes them to MP4 and GIF.
func renderScene(dir, name string, length float64, frame func(t float64) *demoScene) error {
	frames := filepath.Join(dir, name+"-frames")
	os.RemoveAll(frames)
	if err := os.MkdirAll(frames, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(frames)
	n := int(length * demoFPS)
	for i := 0; i < n; i++ {
		st := frame(float64(i) / demoFPS)
		if err := renderPNGSize(filepath.Join(frames, fmt.Sprintf("f%04d.png", i)), demoW, demoH,
			func(cv Canvas) { drawScene(cv, st) }); err != nil {
			return err
		}
	}
	bin, err := engine.FFmpegPath()
	if err != nil {
		return err
	}
	in := filepath.Join(frames, "f%04d.png")
	run := func(args ...string) error {
		out, err := exec.Command(bin, append([]string{"-v", "error", "-y", "-framerate", fmt.Sprint(demoFPS), "-i", in}, args...)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg: %v %s", err, out)
		}
		return nil
	}
	if err := run("-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "20", "-movflags", "+faststart", filepath.Join(dir, name+".mp4")); err != nil {
		return err
	}
	return run("-vf", "fps=15,scale=800:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=160:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=3:diff_mode=rectangle",
		"-loop", "0", filepath.Join(dir, name+".gif"))
}

// socialPreview renders the 1280×640 image GitHub shows when the repository is shared.
func socialPreview(path, tmp string) error {
	video := filepath.Join(tmp, "clip.mp4")
	os.WriteFile(video, []byte("x"), 0o644)
	m := newWheelModel(kindConvert, []string{video}, 1.25)
	m.sel = 1
	return renderPNGSize(path, 1280, 640, func(cv Canvas) {
		roundRect(cv, -20, -20, 1320, 680, 0.5, colDesk)
		cv.Circle(1180, 80, 300, argb(28, 255, 255, 255))
		m.Paint(shifted{cv, 900 - m.size/2, 320 - m.size/2})
		drawIcon(shifted{cv, 352, 128}, 96)
		cv.Text("Mandarino", 80, 230, 640, 90, 78, true, colWhite)
		cv.Text("Drag files · press Shift or Ctrl · drop", 80, 320, 640, 44, 30, false, argb(255, 255, 196, 120))
		cv.Text("Offline file converter for Windows and Linux", 80, 380, 640, 40, 24, false, argb(255, 205, 212, 225))
		cv.Text("images · video · audio · PDF · Bluetooth · move", 80, 424, 640, 40, 22, false, argb(255, 150, 162, 182))
	})
}

// demo renders everything into dir.
func demo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "mandarino-demo")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	length, frame := sceneConvert(tmp)
	if err := renderScene(dir, "convert", length, frame); err != nil {
		return err
	}
	length, frame = sceneActions(tmp)
	if err := renderScene(dir, "actions", length, frame); err != nil {
		return err
	}
	return socialPreview(filepath.Join(dir, "social-preview.png"), tmp)
}
