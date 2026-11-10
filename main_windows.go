//go:build windows

// Mandarino for Windows: start dragging files in File Explorer, press Shift
// (convert) or Ctrl (Bluetooth, copy, copy path, move to a folder) and drop
// them on a segment of the wheel. Everything runs locally.
//
// Architecture (all files ending in _windows.go):
//
//   - main_windows.go      start-up, the hidden main window and its message loop, jobs
//   - hooks_windows.go     low-level keyboard/mouse hooks (drag + Shift) on their own thread
//   - droptarget_windows.go the wheel as an OLE drop target (dragged files, hover, drop)
//   - com_windows.go       minimal COM helpers
//   - wheel_windows.go     the wheel window (logic and drawing in ui_wheel.go / ui_range.go)
//   - progress_windows.go  the progress wheel (logic and drawing in ui_progress.go)
//   - fileops_windows.go   the Ctrl actions: clipboard, move, Bluetooth
//   - places_windows.go    the places shown by the folder browser
//   - tray_windows.go      notification-area icon, menu and notifications
//   - registry_windows.go  "start with Windows" and settings
//   - gdip_windows.go      GDI+ drawing (Canvas implementation)
//   - win32_windows.go     Win32 declarations
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"mandarino/engine"
)

const appName = "Mandarino"

// appVersion is set at build time: go build -ldflags "-X main.appVersion=1.2.0"
var appVersion = "dev"

// Messages posted to the main window (from hooks and worker goroutines).
const (
	msgTray        = WM_APP + 1 // notification-area icon events
	msgDragKey     = WM_APP + 2 // Shift/Ctrl pressed while dragging files (wParam = wheel kind, lParam = source window)
	msgDragRelease = WM_APP + 7 // mouse released while the wheel was open for a drag (wParam = 1 if on the wheel)
	msgRunUI       = WM_APP + 8 // run the functions queued by runOnUI
	msgKey         = WM_APP + 3 // key for the open wheel (wParam = virtual key)
	msgClose       = WM_APP + 4 // close the wheel (click outside, other key)
	msgJobDone     = WM_APP + 5 // a job finished (see app.doneQueue)
	msgScroll      = WM_APP + 6 // mouse wheel over the page picker (wParam = 1 for up)
)

// Timers of the main window.
const (
	timerWatch     = 1 // closes the wheel if another window comes to the foreground
	timerTooltip   = 2 // updates the tray tooltip while jobs run
	timerShowProg  = 3 // shows the progress wheel if a job is not instantaneous
	timerDropWatch = 4 // closes the wheel if a release on it was not followed by a Drop
	timerDwell     = 5 // the pointer rested on "Move to…" or a folder: open it
)

// Windows (the UI) and COM objects must live on one OS thread.
func init() { runtime.LockOSThread() }

var app struct {
	hinst, hwnd, icon uintptr
	taskbarCreated    uintptr // message sent when Explorer restarts (re-add the tray icon)
	wheel             *wheelWin
	progress          *progressWin

	jobs       map[int]*job // running jobs, only touched on the UI thread
	nextJob    int
	lastOutput string // last file created, opened when a notification is clicked
	wg         sync.WaitGroup

	doneMu    sync.Mutex
	doneQueue []*job // finished jobs waiting to be handled on the UI thread

	uiMu    sync.Mutex
	uiQueue []func() // work to run on the UI thread (see runOnUI)
}

// runOnUI queues fn to run on the UI thread (safe from any goroutine).
func runOnUI(fn func()) {
	app.uiMu.Lock()
	app.uiQueue = append(app.uiQueue, fn)
	app.uiMu.Unlock()
	post(msgRunUI, 0, 0)
}

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

var mainWndProcCB = syscall.NewCallback(mainWndProc)

func main() {
	// a single instance at a time
	if _, _, e := pCreateMutexW.Call(0, 0, up(utf16p(`Local\MandarinoFileConverter`))); e == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		messageBox("Mandarino is already running: look for the orange icon next to the clock.", appName, MB_ICONINFO)
		return
	}

	// crisp rendering on high-DPI screens (must happen before creating windows)
	if pSetProcessDpiAwarenessContext.Find() == nil {
		pSetProcessDpiAwarenessContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	} else {
		pSetProcessDPIAware.Call()
	}
	pOleInitialize.Call(0) // COM + OLE drag & drop on this (UI) thread

	exe, _ := os.Executable()
	engine.ToolDir = filepath.Dir(exe) // ffmpeg.exe and pdfium.dll live next to Mandarino.exe

	if !gdipStart() {
		messageBox("Could not initialize graphics (GDI+).", appName, MB_ICONWARNING)
		return
	}
	app.jobs = map[int]*job{}
	app.hinst, _, _ = pGetModuleHandleW.Call(0)
	app.icon = makeIcon(32)

	// hidden window that receives tray events, timers and our own messages
	cls := utf16p("MandarinoMain")
	wc := WNDCLASSEXW{LpfnWndProc: mainWndProcCB, HInstance: app.hinst, LpszClassName: cls, HIcon: app.icon, HIconSm: app.icon}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	app.hwnd, _, _ = pCreateWindowExW.Call(0, up(cls), up(utf16p(appName)), 0, 0, 0, 0, 0, 0, 0, app.hinst, 0)
	app.taskbarCreated, _, _ = pRegisterWindowMessageW.Call(up(utf16p("TaskbarCreated")))

	app.wheel = newWheelWin(app.hinst)
	app.wheel.onPick = launchJob
	app.wheel.onOp = runFileOp
	app.wheel.onError = func(msg string) {
		pKillTimer.Call(app.hwnd, timerWatch)
		balloon(appName, msg, true)
	}
	app.progress = newProgressWin(app.hinst)

	trayAdd()

	if _, err := engine.FFmpegPath(); err != nil {
		messageBox("ffmpeg.exe was not found in Mandarino's folder.\n\nWithout it only PDF conversions and some image formats work. "+
			"Extract the whole zip into a folder (right click > Extract All) and start Mandarino.exe from there.", appName, MB_ICONWARNING)
	} else if !regGetFlag("Configured") {
		// first real start (not from inside the zip): start with Windows from now on
		regSetFlag("Configured")
		setAutostart(true)
		balloon("Mandarino is running", "Drag files in File Explorer and press Shift to convert them, or Ctrl for more actions.", false)
	}

	go hookThread()

	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// post sends a message to the main window from any thread.
func post(msg, wparam, lparam uintptr) { pPostMessageW.Call(app.hwnd, msg, wparam, lparam) }

func mainWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case msgDragKey:
		onDragKey(wheelKind(wparam), lparam)
		return 0
	case msgDragRelease:
		if wparam == 1 {
			// released on the wheel: OLE will call Drop in a moment; if it does not
			// (it was not a real file drag), close the wheel anyway
			pSetTimer.Call(app.hwnd, timerDropWatch, 700, 0)
		} else {
			closeWheel()
		}
		return 0
	case msgKey:
		app.wheel.key(wparam)
		if !app.wheel.visible {
			pKillTimer.Call(app.hwnd, timerWatch)
		}
		return 0
	case msgScroll:
		app.wheel.scroll(wparam == 1)
		return 0
	case msgClose:
		closeWheel()
		return 0
	case msgJobDone:
		onJobsDone()
		return 0
	case msgRunUI:
		app.uiMu.Lock()
		queue := app.uiQueue
		app.uiQueue = nil
		app.uiMu.Unlock()
		for _, fn := range queue {
			fn()
		}
		return 0
	case msgTray:
		onTrayEvent(lparam)
		return 0
	case WM_TIMER:
		switch wparam {
		case timerWatch:
			if fg, _, _ := pGetForegroundWindow.Call(); !app.wheel.visible || fg != app.wheel.fg {
				closeWheel()
			}
		case timerTooltip:
			updateTooltip()
		case timerDwell:
			pKillTimer.Call(app.hwnd, timerDwell)
			app.wheel.dwell()
		case timerDropWatch:
			pKillTimer.Call(app.hwnd, timerDropWatch)
			if app.wheel.dragMode {
				closeWheel()
			}
		case timerShowProg:
			pKillTimer.Call(app.hwnd, timerShowProg)
			if !app.progress.visible {
				showNextProgress()
			}
		}
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	if msg == app.taskbarCreated && msg != 0 {
		trayAdd() // Explorer restarted: the tray icon must be added again
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func closeWheel() {
	app.wheel.hide()
	pKillTimer.Call(app.hwnd, timerWatch)
}

// onDragKey opens (or switches) the wheel near the pointer: the user is
// dragging files from File Explorer (or the desktop) and pressed Shift or Ctrl.
func onDragKey(kind wheelKind, source uintptr) {
	if app.wheel.visible && !app.wheel.dragMode {
		return // the page picker or the folder browser is open
	}
	fg, _, _ := pGetForegroundWindow.Call()
	if fg == 0 {
		fg = source
	}
	app.wheel.showForDrag(kind, fg)
	pSetTimer.Call(app.hwnd, timerWatch, 250, 0)
}

// ---------- jobs ----------

// launchJob starts the chosen action; called by the wheel.
func launchJob(a engine.Action, files []string, cx, cy int) {
	pKillTimer.Call(app.hwnd, timerWatch)
	app.nextJob++
	app.wg.Add(1)
	j := startJob(app.nextJob, a, files, cx, cy, func(j *job) {
		defer app.wg.Done()
		app.doneMu.Lock()
		app.doneQueue = append(app.doneQueue, j)
		app.doneMu.Unlock()
		post(msgJobDone, 0, 0) // handled on the UI thread
	})
	app.jobs[j.id] = j
	pSetTimer.Call(app.hwnd, timerTooltip, 500, 0)
	pSetTimer.Call(app.hwnd, timerShowProg, 400, 0) // instantaneous jobs never show the progress wheel
}

// showNextProgress attaches the progress wheel to the newest running job.
func showNextProgress() {
	var newest *job
	for _, j := range app.jobs {
		if newest == nil || j.id > newest.id {
			newest = j
		}
	}
	if newest != nil {
		app.progress.attach(newest)
	}
}

func onJobsDone() {
	app.doneMu.Lock()
	done := app.doneQueue
	app.doneQueue = nil
	app.doneMu.Unlock()
	for _, j := range done {
		delete(app.jobs, j.id)
		app.progress.finished(j)
		if out := j.lastOutput(); out != "" {
			app.lastOutput = out
		}
		title, text, isErr := j.summary()
		if !isErr {
			text += "\nClick to show it in its folder."
		}
		balloon(title, text, isErr)
	}
	updateTooltip()
}

// updateTooltip shows the progress in the tray icon's tooltip.
func updateTooltip() {
	tip := appName + " – drag files, then Shift or Ctrl"
	switch len(app.jobs) {
	case 0:
		pKillTimer.Call(app.hwnd, timerTooltip)
	case 1:
		for _, j := range app.jobs {
			tip = appName + ": " + j.action.Desc + "… " + itoa(j.percent()) + "%"
		}
	default:
		tip = appName + ": " + itoa(len(app.jobs)) + " conversions running"
	}
	trayTip(tip)
}

func itoa(n int) string { return strconv.Itoa(n) }

func cancelAll() {
	for _, j := range app.jobs {
		j.cancel()
	}
}

// showInFolder opens File Explorer with path selected.
func showInFolder(path string) {
	pShellExecuteW.Call(0, up(utf16p("open")), up(utf16p("explorer.exe")), up(utf16p(`/select,"`+path+`"`)), 0, 1)
}

// quit cancels running jobs (waiting up to 3 s for ffmpeg to stop) and exits.
func quit() {
	if len(app.jobs) > 0 {
		cancelAll()
		done := make(chan struct{})
		go func() { app.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
	app.progress.hide()
	revokeDropTarget(app.wheel.hwnd)
	trayRemove()
	pPostQuitMessage.Call(0)
}
