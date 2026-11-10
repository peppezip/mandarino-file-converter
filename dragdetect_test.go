package main

import "testing"

func TestDragDetector(t *testing.T) {
	d := &dragDetector{threshold: 4}

	// a modifier without dragging does nothing
	if d.Trigger() {
		t.Fatal("no drag in progress")
	}
	// press, small jitter: not a drag yet
	d.ButtonDown(100, 100, true, 1)
	d.Move(102, 101)
	if d.Trigger() {
		t.Fatal("below the drag threshold")
	}
	// real drag, then Shift or Ctrl
	d.Move(130, 110)
	if !d.Trigger() {
		t.Fatal("drag + modifier must open the wheel")
	}
	if !d.ButtonUp() {
		t.Fatal("ButtonUp should report the drag")
	}
	if d.Trigger() {
		t.Fatal("no drag after the button is released")
	}
	// dragging inside another program (e.g. selecting text) is ignored
	d.ButtonDown(10, 10, false, 2)
	d.Move(80, 10)
	if d.Trigger() {
		t.Fatal("drags outside File Explorer must be ignored")
	}
}
