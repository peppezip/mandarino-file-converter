package main

// dragDetector recognizes the Mandarino gesture: the user starts dragging
// files with the left mouse button and, while still dragging, presses Shift
// (convert wheel) or Ctrl (actions wheel).
// It is fed by the low-level mouse and keyboard hooks (hooks_windows.go) and
// contains no Windows code, so it can be unit-tested anywhere.
//
// A drag counts only if
//   - the button went down on a window that holds files (File Explorer or the
//     desktop), so that Shift+drag to select text in other programs is left alone;
//   - the mouse then moved farther than the system drag threshold (Windows
//     itself starts a drag & drop only after a few pixels).
//
// Pressing the other key while the wheel is open switches wheel.
type dragDetector struct {
	threshold int // pixels (SM_CXDRAG, usually 4)

	down       bool
	fromFiles  bool // the button went down on File Explorer or the desktop
	dragging   bool // moved past the threshold while the button is down
	downX      int
	downY      int
	sourceRoot uintptr // top-level window where the drag started
}

// ButtonDown records a left-button press at screen point (x, y).
func (d *dragDetector) ButtonDown(x, y int, fromFiles bool, root uintptr) {
	*d = dragDetector{threshold: d.threshold, down: true, fromFiles: fromFiles, downX: x, downY: y, sourceRoot: root}
}

// Move records a mouse move; it turns a press into a drag past the threshold.
func (d *dragDetector) Move(x, y int) {
	if d.down && !d.dragging {
		t := max(d.threshold, 1)
		if abs(x-d.downX) > t || abs(y-d.downY) > t {
			d.dragging = true
		}
	}
}

// ButtonUp ends the drag. It reports whether a drag was in progress.
func (d *dragDetector) ButtonUp() bool {
	was := d.dragging
	d.down, d.dragging = false, false
	return was
}

// Trigger must be called when Shift or Ctrl goes down. It returns true when
// a drag of files is in progress, i.e. when the wheel should open (or switch).
func (d *dragDetector) Trigger() bool { return d.Dragging() }

// Dragging reports whether a drag from a file window is in progress.
func (d *dragDetector) Dragging() bool { return d.down && d.dragging && d.fromFiles }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
