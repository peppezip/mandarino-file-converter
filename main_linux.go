//go:build linux

// Mandarino for Linux.
//
// On Linux there is no reliable way to read the selection of "the" file
// manager (there are many, and Wayland forbids global keyboard hooks), so
// Mandarino integrates with the file managers instead:
//
//	mandarino --install          adds "Mandarino" (convert) and "Mandarino Actions"
//	                             to the right-click menu of Nautilus (GNOME Files, with
//	                             Ctrl+Shift+M / Ctrl+Alt+M), Nemo, Caja, Dolphin, Thunar,
//	                             and "Mandarino" to "Open With" everywhere
//	mandarino FILE...            the convert wheel under the mouse for FILE...
//	mandarino --actions PATH...  the actions wheel: Bluetooth, Copy, Path, Move to…
//
// The wheel, the page picker and the progress wheel are the same as on
// Windows (ui_*.go); this file only runs them in an X11 window (x11_linux.go),
// which also works on Wayland desktops through XWayland.
package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mandarino/engine"
)

const appName = "Mandarino"

// appVersion is set at build time: go build -ldflags "-X main.appVersion=1.2.0"
var appVersion = "dev"

const usage = `Mandarino %s – offline file converter

Usage:
  mandarino FILE...            convert: open the wheel for the files
  mandarino --actions PATH...  actions: Bluetooth, Copy, Path, Move to…
  mandarino --install          add Mandarino to your file manager's right-click menu
  mandarino --uninstall        remove it again
  mandarino --version          print the version

After --install: select files, right-click > Scripts / Actions > Mandarino
(in GNOME Files you can also press Ctrl+Shift+M to convert, Ctrl+Alt+M for actions).
`

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func main() {
	exe, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	engine.ToolDir = filepath.Dir(exe) // bundled ffmpeg and libpdfium.so live next to the binary

	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Printf(usage, appVersion)
		notify("Mandarino", "Select files in your file manager, then right-click > Mandarino.", false)
		return
	}
	switch args[0] {
	case "-h", "--help":
		fmt.Printf(usage, appVersion)
		return
	case "--version":
		fmt.Println(appName, appVersion)
		return
	case "--install":
		exitOn(install(exe))
		return
	case "--uninstall":
		exitOn(uninstall())
		return
	case "--screenshots":
		// developer helper: renders the UI to PNG files (used for the README)
		dir := "."
		if len(args) > 1 {
			dir = args[1]
		}
		exitOn(screenshots(dir))
		return
	case "--demo":
		// developer helper: renders the README animations and the social preview
		dir := "docs/demo"
		if len(args) > 1 {
			dir = args[1]
		}
		exitOn(demo(dir))
		return
	case "--actions":
		os.Exit(runWheel(kindActions, args[1:]))
	}
	os.Exit(runWheel(kindConvert, args))
}

func exitOn(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mandarino:", err)
		os.Exit(1)
	}
}

// selectedPaths turns the arguments into absolute paths of existing files and
// folders (file managers sometimes pass relative names or file:// URIs).
func selectedPaths(args []string) []string {
	var paths []string
	for _, a := range args {
		if strings.HasPrefix(a, "file://") {
			if u, err := url.Parse(a); err == nil {
				a = u.Path
			}
		}
		if abs, err := filepath.Abs(a); err == nil {
			a = abs
		}
		if _, err := os.Stat(a); err == nil {
			paths = append(paths, a)
		}
	}
	return paths
}

// runWheel shows a wheel for the paths, runs what the user picks and reports the result.
func runWheel(kind wheelKind, args []string) int {
	paths := selectedPaths(args)
	d, err := openDisplay()
	if err != nil {
		notify(appName, err.Error(), true)
		return 1
	}
	m := newWheelModel(kind, paths, screenScale(d))
	if len(m.acts) == 0 { // nothing to offer: say why instead of showing an empty wheel
		notify(appName, m.hint, true)
		return 1
	}

	// 1. the wheel
	px, py := pointerPos(d)
	w := newXWin(d, m.size, px, py)
	w.setShape(m.outerR)
	w.show()
	w.paint(m.Paint)
	w.grab()
	out := wheelLoop(w, m)
	w.ungrab()
	cx, cy := w.x+w.size/2, w.y+w.size/2
	w.destroy()
	switch {
	case out.Error != "":
		notify(appName, out.Error, true)
		return 1
	case out.Op != "":
		return runFileOp(d, out.Op, out.Paths, out.Dest)
	case out.Pick == nil:
		return 0
	}
	action, files := out.Pick, m.files

	// 2. the job, with the progress wheel if it takes more than a moment
	finished := make(chan struct{})
	j := startJob(1, *action, files, cx, cy, func(*job) { close(finished) })
	select {
	case <-finished:
	case <-time.After(400 * time.Millisecond):
		progressLoop(d, j, m.scale, finished)
	}

	// 3. the notification
	title, text, isErr := j.summary()
	notify(title, text, isErr)
	if isErr {
		return 1
	}
	return 0
}

// wheelLoop handles events until the user picks something or closes the wheel,
// and returns that final Outcome (Pick, Op, Error, or just Close).
func wheelLoop(w *xwin, m *wheelModel) Outcome {
	hadFocus := false
	var dwellAt time.Time              // when the dwell timer fires (zero = not running)
	handle := func(out Outcome) bool { // true = the wheel is done
		if out.Redraw || out.Resize {
			dwellAt = time.Time{}
		}
		if out.Dwell {
			dwellAt = time.Now().Add(dwellMillis * time.Millisecond)
		}
		switch {
		case out.Pick != nil || out.Op != "" || out.Error != "" || out.Close:
			return true
		case out.Resize:
			w.resize(m.size)
			w.setShape(m.outerR)
			w.paint(m.Paint)
		case out.Redraw:
			w.paint(m.Paint)
		}
		return false
	}
	for {
		for w.pending() {
			e := w.next()
			var out Outcome
			switch e.kind {
			case evExpose:
				out.Redraw = true
			case evMap:
				if w.managed {
					w.focus() // Wayland: keys arrive through the focus instead of a grab
				}
			case evFocusIn:
				hadFocus = true
			case evFocusOut:
				if w.managed && hadFocus {
					out.Close = true // clicked elsewhere: same as a click outside on X11
				}
			case evMotion:
				out = m.Move(e.x, e.y)
			case evLeave:
				out = m.Leave()
			case evPress:
				switch {
				case e.button == 4 || e.button == 5: // mouse wheel
					out = m.Scroll(e.button == 4)
				case e.button == 3: // right click
					out.Close = true
				case !m.Contains(e.x, e.y): // click outside the fruit
					out.Close = true
				}
			case evRelease:
				if e.button == 1 {
					out = m.Click(e.x, e.y)
				}
			case evKey:
				out = m.Key(e.key)
			}
			if handle(out) {
				return out
			}
		}
		if !dwellAt.IsZero() && time.Now().After(dwellAt) {
			dwellAt = time.Time{}
			if out := m.DwellFire(); handle(out) {
				return out
			}
		}
		sleepMs(10)
	}
}

// progressLoop shows the progress wheel until the job ends. The window can
// be dragged, the × cancels the job and a right click hides it.
func progressLoop(d displayPtr, j *job, scale float64, finished <-chan struct{}) {
	m := newProgressModel(scale)
	w := newXWin(d, m.size, j.cx, j.cy)
	w.setShape(m.outerR)
	update := func() {
		i, f, _ := j.state()
		m.Update(j.action.Label, i, len(j.files), f)
	}
	update()
	w.show()
	w.paint(m.Paint)

	visible, dragging := true, false
	var offX, offY int
	lastFrame := time.Now()
	for {
		for w.pending() {
			e := w.next()
			switch e.kind {
			case evExpose:
				w.paint(m.Paint)
			case evMotion:
				if dragging {
					w.moveTo(e.xRoot-offX, e.yRoot-offY)
				} else if h := m.OverCancel(e.x, e.y); h != m.hoverCancel {
					m.hoverCancel = h
					w.paint(m.Paint)
				}
			case evPress:
				switch {
				case e.button == 1 && m.OverCancel(e.x, e.y):
					j.cancel()
				case e.button == 1:
					if !w.startMove(e.xRoot, e.yRoot) { // Wayland: the compositor moves it
						dragging, offX, offY = true, e.x, e.y
					}
				case e.button == 3:
					w.unmap()
					visible = false
				}
			case evRelease:
				dragging = false
			}
		}
		select {
		case <-finished:
			if visible && j.succeeded() {
				m.Finish()
				w.paint(m.Paint)
				sleepMs(800)
			}
			w.destroy()
			return
		default:
		}
		if visible && time.Since(lastFrame) >= 60*time.Millisecond {
			lastFrame = time.Now()
			m.phase++
			update()
			w.paint(m.Paint)
		}
		sleepMs(15)
	}
}

// notify shows a desktop notification (through notify-send, part of libnotify)
// and always prints the message too.
func notify(title, text string, isError bool) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, text)
	bin, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}
	icon := "mandarino"
	if isError {
		icon = "dialog-error"
	}
	_ = exec.Command(bin, "-a", appName, "-i", icon, title, text).Run()
}

// screenshots renders the wheels, the page picker, the folder browser and the
// progress wheel to PNG files (used for the README).
func screenshots(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "mandarino-shots")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	video := filepath.Join(tmp, "clip.mp4")
	os.WriteFile(video, []byte("x"), 0o644)
	pdf := filepath.Join(tmp, "doc.pdf")
	os.WriteFile(pdf, []byte("x"), 0o644)
	for _, sub := range []string{"Holidays", "Invoices", "Music", "Projects", "Recipes", "Screenshots", "Work"} {
		os.MkdirAll(filepath.Join(tmp, "Documents", sub), 0o755)
	}

	shots := []struct {
		name string
		m    *wheelModel
	}{}
	m := newWheelModel(kindConvert, []string{video}, 2)
	m.sel = 1
	shots = append(shots, struct {
		name string
		m    *wheelModel
	}{"wheel.png", m})

	a := newWheelModel(kindActions, []string{video}, 2)
	a.sel = 3
	shots = append(shots, struct {
		name string
		m    *wheelModel
	}{"actions.png", a})

	f := newWheelModel(kindActions, []string{video}, 2)
	f.mode = modeFolders
	f.openDir(filepath.Join(tmp, "Documents"))
	f.sel = 3
	shots = append(shots, struct {
		name string
		m    *wheelModel
	}{"folders.png", f})

	r := newWheelModel(kindConvert, []string{pdf}, 2)
	r.mode, r.rg = modeRange, rangeState{pages: 12, from: 3, to: 7, hover: "extract"}
	r.fleshR = int(132 * r.scale)
	r.outerR = r.fleshR + int(14*r.scale)
	r.size = 2*r.outerR + int(16*r.scale)
	shots = append(shots, struct {
		name string
		m    *wheelModel
	}{"split.png", r})

	for _, sh := range shots {
		if err := renderPNG(filepath.Join(dir, sh.name), sh.m.size, sh.m.Paint); err != nil {
			return err
		}
	}
	p := newProgressModel(2)
	p.Update("Compress", 0, 1, 0.42)
	if err := renderPNG(filepath.Join(dir, "progress.png"), p.size, p.Paint); err != nil {
		return err
	}
	return renderPNG(filepath.Join(dir, "icon.png"), 256, func(cv Canvas) { drawIcon(cv, 256) })
}
