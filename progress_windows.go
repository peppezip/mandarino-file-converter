//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// The progress wheel on Windows (drawing and logic are in ui_progress.go).
// It appears where the wheel was, can be dragged anywhere, the × cancels the
// job and a right click hides it (the job keeps running).

const (
	progTimerAnim = 1 // repaint/animation, every 60 ms
	progTimerHide = 2 // hide shortly after "Done!"
)

type progressWin struct {
	hwnd    uintptr
	visible bool
	moved   bool // dragged by the user: keep it where it is
	j       *job
	m       *progressModel
	cx, cy  int
}

var (
	progWndProcCB = syscall.NewCallback(progWndProc)
	theProgress   *progressWin
)

func newProgressWin(hinst uintptr) *progressWin {
	p := &progressWin{}
	cls := utf16p("MandarinoProgress")
	cursor, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	wc := WNDCLASSEXW{LpfnWndProc: progWndProcCB, HInstance: hinst, HCursor: cursor, LpszClassName: cls}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	p.hwnd, _, _ = pCreateWindowExW.Call(
		WS_EX_LAYERED|WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE,
		up(cls), up(utf16p("Mandarino progress")), WS_POPUP,
		0, 0, 10, 10, 0, 0, hinst, 0)
	theProgress = p
	return p
}

// attach shows the progress of job j, centred where its wheel was.
func (p *progressWin) attach(j *job) {
	pKillTimer.Call(p.hwnd, progTimerHide)
	p.j = j
	p.m = newProgressModel(monitorDPI(int32(j.cx), int32(j.cy)))
	p.refresh()
	var pos *POINT
	if !p.visible || !p.moved {
		p.moved = false
		p.cx, p.cy = clampCentre(j.cx, j.cy, p.m.size)
		pos = &POINT{int32(p.cx - p.m.size/2), int32(p.cy - p.m.size/2)}
	}
	layeredPaint(p.hwnd, p.m.size, pos, p.m.Paint)
	if !p.visible {
		pSetWindowPos.Call(p.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE|SWP_SHOWWINDOW)
		pShowWindow.Call(p.hwnd, SW_SHOWNOACTIVATE)
		p.visible = true
	}
	pSetTimer.Call(p.hwnd, progTimerAnim, 60, 0)
}

// refresh copies the job's progress into the model.
func (p *progressWin) refresh() {
	i, f, _ := p.j.state()
	p.m.Update(p.j.action.Label, i, len(p.j.files), f)
}

func (p *progressWin) hide() {
	pKillTimer.Call(p.hwnd, progTimerAnim)
	pKillTimer.Call(p.hwnd, progTimerHide)
	if p.visible {
		pShowWindow.Call(p.hwnd, SW_HIDE)
	}
	p.visible, p.moved, p.j = false, false, nil
}

// finished must be called when job j ends.
func (p *progressWin) finished(j *job) {
	if !p.visible || p.j != j {
		return
	}
	pKillTimer.Call(p.hwnd, progTimerAnim)
	if !j.succeeded() {
		p.hide()
		showNextProgress()
		return
	}
	p.m.Finish()
	layeredPaint(p.hwnd, p.m.size, nil, p.m.Paint)
	pSetTimer.Call(p.hwnd, progTimerHide, 800, 0)
}

func (p *progressWin) setHover(h bool) {
	if p.m != nil && h != p.m.hoverCancel {
		p.m.hoverCancel = h
		layeredPaint(p.hwnd, p.m.size, nil, p.m.Paint)
	}
}

func progWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	p := theProgress
	if p != nil && hwnd == p.hwnd && p.m != nil {
		switch msg {
		case WM_MOUSEACTIVATE:
			return MA_NOACTIVATE
		case WM_NCHITTEST:
			// the × behaves like a button, the rest like a title bar (drag to move)
			var r RECT
			pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			x, y := lparamXY(lparam)
			if p.m.OverCancel(x-int(r.Left), y-int(r.Top)) {
				return HTCLIENT
			}
			return HTCAPTION
		case WM_MOVING:
			p.moved = true
		case WM_MOUSEMOVE:
			x, y := lparamXY(lparam)
			p.setHover(p.m.OverCancel(x, y))
			tme := TRACKMOUSEEVENT{DwFlags: TME_LEAVE, HwndTrack: hwnd}
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			return 0
		case WM_MOUSELEAVE, WM_NCMOUSEMOVE:
			p.setHover(false)
		case WM_LBUTTONUP:
			x, y := lparamXY(lparam)
			if p.m.OverCancel(x, y) && p.j != nil {
				p.j.cancel()
			}
			return 0
		case WM_NCLBUTTONDBLCLK:
			return 0 // no "maximize" on double click
		case WM_NCRBUTTONUP:
			p.hide() // the job keeps running; a notification arrives at the end
			return 0
		case WM_TIMER:
			switch wparam {
			case progTimerAnim:
				if p.j != nil {
					p.m.phase++
					p.refresh()
					layeredPaint(p.hwnd, p.m.size, nil, p.m.Paint)
				}
			case progTimerHide:
				p.hide()
				showNextProgress()
			}
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}
