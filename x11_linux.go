//go:build linux

package main

/*
#cgo pkg-config: x11 xext cairo
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/Xatom.h>
#include <X11/keysym.h>
#include <X11/Xresource.h>
#include <X11/extensions/shape.h>
#include <cairo/cairo.h>
#include <cairo/cairo-xlib.h>

// A flattened view of the X events we care about (Go cannot easily read C unions).
typedef struct {
	int type;            // KeyPress, ButtonPress, ButtonRelease, MotionNotify, LeaveNotify, Expose
	int x, y;            // pointer position relative to our window
	int x_root, y_root;  // pointer position on screen
	unsigned int button; // 1 left, 2 middle, 3 right, 4/5 wheel
	unsigned long keysym;
	char text[8];        // characters produced by a key press (to recognize digits)
	// SelectionRequest: another program asks for our clipboard content
	unsigned long requestor, selection, target, property, time;
} mevent;

static int mandarino_next_event(Display *d, mevent *out) {
	XEvent e;
	XNextEvent(d, &e);
	memset(out, 0, sizeof(*out));
	out->type = e.type;
	switch (e.type) {
	case KeyPress: {
		KeySym ks = 0;
		int n = XLookupString(&e.xkey, out->text, sizeof(out->text) - 1, &ks, NULL);
		out->text[n > 0 ? n : 0] = 0;
		out->keysym = ks;
		break;
	}
	case ButtonPress:
	case ButtonRelease:
		out->x = e.xbutton.x; out->y = e.xbutton.y;
		out->x_root = e.xbutton.x_root; out->y_root = e.xbutton.y_root;
		out->button = e.xbutton.button;
		break;
	case MotionNotify:
		out->x = e.xmotion.x; out->y = e.xmotion.y;
		out->x_root = e.xmotion.x_root; out->y_root = e.xmotion.y_root;
		break;
	case SelectionRequest:
		out->requestor = e.xselectionrequest.requestor;
		out->selection = e.xselectionrequest.selection;
		out->target = e.xselectionrequest.target;
		out->property = e.xselectionrequest.property;
		out->time = e.xselectionrequest.time;
		break;
	}
	return e.type;
}

// ---- clipboard (we own CLIPBOARD and answer the requests ourselves) ----

static Window mandarino_clipboard_window(Display *d) {
	Window w = XCreateSimpleWindow(d, DefaultRootWindow(d), 0, 0, 1, 1, 0, 0, 0);
	Atom clip = XInternAtom(d, "CLIPBOARD", False);
	XSetSelectionOwner(d, clip, w, CurrentTime);
	XFlush(d);
	return XGetSelectionOwner(d, clip) == w ? w : None;
}

// Answers a SelectionRequest: stores the data in the requestor's property
// (type None = refuse) and notifies it.
static void mandarino_reply(Display *d, unsigned long req, unsigned long sel, unsigned long target,
                            unsigned long prop, unsigned long t, unsigned long type, int format,
                            const void *data, int nitems) {
	XEvent ev;
	memset(&ev, 0, sizeof(ev));
	ev.xselection.type = SelectionNotify;
	ev.xselection.display = d;
	ev.xselection.requestor = req;
	ev.xselection.selection = sel;
	ev.xselection.target = target;
	ev.xselection.time = t;
	if (prop == None) prop = target; // obsolete clients
	if (type != None) {
		XChangeProperty(d, req, prop, type, format, PropModeReplace, (const unsigned char *)data, nitems);
		ev.xselection.property = prop;
	} else {
		ev.xselection.property = None;
	}
	XSendEvent(d, req, False, 0, &ev);
	XFlush(d);
}

// Finds a 32-bit visual with an alpha channel (needed for a round, see-through window).
static Visual *mandarino_argb_visual(Display *d, int screen, int *depth) {
	XVisualInfo vi;
	if (XMatchVisualInfo(d, screen, 32, TrueColor, &vi)) { *depth = 32; return vi.visual; }
	*depth = DefaultDepth(d, screen);
	return DefaultVisual(d, screen);
}

static Window mandarino_create_window(Display *d, Visual *v, int depth, int x, int y, int size, int override) {
	int screen = DefaultScreen(d);
	Window root = RootWindow(d, screen);
	XSetWindowAttributes a;
	memset(&a, 0, sizeof(a));
	a.override_redirect = override ? True : False; // True: no title bar, not managed by the window manager
	a.colormap = XCreateColormap(d, root, v, AllocNone);
	a.border_pixel = 0;
	a.background_pixel = 0;
	a.event_mask = ExposureMask | ButtonPressMask | ButtonReleaseMask | PointerMotionMask |
	               KeyPressMask | LeaveWindowMask | StructureNotifyMask | FocusChangeMask;
	return XCreateWindow(d, root, x, y, size, size, 0, depth, InputOutput, v,
	                     CWOverrideRedirect | CWColormap | CWBorderPixel | CWBackPixel | CWEventMask, &a);
}

// Without a compositor, transparent pixels would show as black: clip the window to a circle.
static void mandarino_shape_circle(Display *d, Window w, int size, int cx, int cy, int r) {
	Pixmap mask = XCreatePixmap(d, w, size, size, 1);
	GC gc = XCreateGC(d, mask, 0, NULL);
	XSetForeground(d, gc, 0);
	XFillRectangle(d, mask, gc, 0, 0, size, size);
	XSetForeground(d, gc, 1);
	XFillArc(d, mask, gc, cx - r, cy - r, 2 * r, 2 * r, 0, 360 * 64);
	XShapeCombineMask(d, w, ShapeBounding, 0, 0, mask, ShapeSet);
	XFreeGC(d, gc);
	XFreePixmap(d, mask);
}

static int mandarino_has_compositor(Display *d) {
	char name[32];
	snprintf(name, sizeof(name), "_NET_WM_CM_S%d", DefaultScreen(d));
	return XGetSelectionOwner(d, XInternAtom(d, name, False)) != None;
}

static void mandarino_pointer(Display *d, int *x, int *y) {
	Window root_ret, child; int wx, wy; unsigned int mask;
	*x = *y = 0;
	XQueryPointer(d, DefaultRootWindow(d), &root_ret, &child, x, y, &wx, &wy, &mask);
}

static int mandarino_grab_keyboard(Display *d, Window w) {
	return XGrabKeyboard(d, w, True, GrabModeAsync, GrabModeAsync, CurrentTime) == GrabSuccess;
}

static int mandarino_grab_pointer(Display *d, Window w) {
	return XGrabPointer(d, w, True, ButtonPressMask | ButtonReleaseMask | PointerMotionMask,
	                    GrabModeAsync, GrabModeAsync, None, None, CurrentTime) == GrabSuccess;
}

static void mandarino_set_title(Display *d, Window w, const char *title) {
	XStoreName(d, w, title);
	XClassHint *h = XAllocClassHint();
	if (h) { h->res_name = (char *)"mandarino"; h->res_class = (char *)"Mandarino"; XSetClassHint(d, w, h); XFree(h); }
}

// On Wayland, X11 programs (through XWayland) cannot grab the keyboard or know
// where the mouse is over native windows. There we use a normal, undecorated,
// always-on-top window that the compositor focuses, instead of a grab.
static void mandarino_make_managed(Display *d, Window w, int x, int y, int size) {
	// no decorations (Motif hints: flags = decorations, decorations = 0)
	long motif[5] = {2, 0, 0, 0, 0};
	Atom mh = XInternAtom(d, "_MOTIF_WM_HINTS", False);
	XChangeProperty(d, w, mh, mh, 32, PropModeReplace, (unsigned char *)motif, 5);
	// a dialog that stays above and is not listed in the taskbar
	Atom type = XInternAtom(d, "_NET_WM_WINDOW_TYPE_DIALOG", False);
	XChangeProperty(d, w, XInternAtom(d, "_NET_WM_WINDOW_TYPE", False), XA_ATOM, 32, PropModeReplace, (unsigned char *)&type, 1);
	Atom state[3] = {XInternAtom(d, "_NET_WM_STATE_ABOVE", False), XInternAtom(d, "_NET_WM_STATE_SKIP_TASKBAR", False),
	                 XInternAtom(d, "_NET_WM_STATE_SKIP_PAGER", False)};
	XChangeProperty(d, w, XInternAtom(d, "_NET_WM_STATE", False), XA_ATOM, 32, PropModeReplace, (unsigned char *)state, 3);
	// fixed size and requested position
	XSizeHints *sh = XAllocSizeHints();
	if (sh) {
		sh->flags = PPosition | USPosition | PMinSize | PMaxSize;
		sh->x = x; sh->y = y;
		sh->min_width = sh->max_width = size; sh->min_height = sh->max_height = size;
		XSetWMNormalHints(d, w, sh);
		XFree(sh);
	}
	XWMHints *wh = XAllocWMHints();
	if (wh) { wh->flags = InputHint; wh->input = True; XSetWMHints(d, w, wh); XFree(wh); }
}

static void mandarino_focus(Display *d, Window w) {
	XSetInputFocus(d, w, RevertToParent, CurrentTime);
	XRaiseWindow(d, w);
}

// Asks the window manager to move the window with the mouse (_NET_WM_MOVERESIZE).
static void mandarino_wm_move(Display *d, Window w, int x_root, int y_root) {
	XEvent ev;
	memset(&ev, 0, sizeof(ev));
	XUngrabPointer(d, CurrentTime);
	ev.xclient.type = ClientMessage;
	ev.xclient.window = w;
	ev.xclient.message_type = XInternAtom(d, "_NET_WM_MOVERESIZE", False);
	ev.xclient.format = 32;
	ev.xclient.data.l[0] = x_root;
	ev.xclient.data.l[1] = y_root;
	ev.xclient.data.l[2] = 8; // _NET_WM_MOVERESIZE_MOVE
	ev.xclient.data.l[3] = Button1;
	ev.xclient.data.l[4] = 1; // normal application
	XSendEvent(d, DefaultRootWindow(d), False, SubstructureRedirectMask | SubstructureNotifyMask, &ev);
	XFlush(d);
}

// Some requests (focus on a window that is not mapped yet...) may fail harmlessly:
// do not let Xlib's default handler kill the program.
static int mandarino_ignore_errors(Display *d, XErrorEvent *e) { (void)d; (void)e; return 0; }
static void mandarino_set_error_handler(void) { XSetErrorHandler(mandarino_ignore_errors); }

static int mandarino_screen_w(Display *d) { return DisplayWidth(d, DefaultScreen(d)); }
static int mandarino_screen_h(Display *d) { return DisplayHeight(d, DefaultScreen(d)); }
*/
import "C"

import (
	"errors"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// ---------- cairo implementation of Canvas ----------

// cairoCanvas draws with cairo, which ships with every Linux desktop.
type cairoCanvas struct{ cr *C.cairo_t }

func (c cairoCanvas) setColor(col Color) {
	a, r, g, b := col.Components()
	C.cairo_set_source_rgba(c.cr, C.double(r), C.double(g), C.double(b), C.double(a))
}

func (c cairoCanvas) Circle(cx, cy, r int, col Color) {
	c.setColor(col)
	C.cairo_new_path(c.cr)
	C.cairo_arc(c.cr, C.double(cx), C.double(cy), C.double(r), 0, 2*math.Pi)
	C.cairo_fill(c.cr)
}

func (c cairoCanvas) Ring(cx, cy, r int, width float64, col Color) {
	c.setColor(col)
	C.cairo_new_path(c.cr)
	C.cairo_arc(c.cr, C.double(cx), C.double(cy), C.double(r), 0, 2*math.Pi)
	C.cairo_set_line_width(c.cr, C.double(width))
	C.cairo_stroke(c.cr)
}

func (c cairoCanvas) RadialEllipse(cx, cy int, angleDeg, dist, l, w float64, col Color) {
	c.setColor(col)
	C.cairo_save(c.cr)
	C.cairo_new_path(c.cr)
	C.cairo_translate(c.cr, C.double(cx), C.double(cy))
	C.cairo_rotate(c.cr, C.double((angleDeg+90)*math.Pi/180)) // "up" now points along angleDeg
	C.cairo_translate(c.cr, 0, C.double(-dist))
	C.cairo_scale(c.cr, C.double(w/2), C.double(l/2))
	C.cairo_arc(c.cr, 0, 0, 1, 0, 2*math.Pi) // unit circle, stretched into an ellipse
	C.cairo_restore(c.cr)
	C.cairo_fill(c.cr)
}

func (c cairoCanvas) RoundedPolygon(pts [][2]float32, r float64, col Color) {
	if len(pts) < 3 {
		return
	}
	c.setColor(col)
	C.cairo_new_path(c.cr)
	C.cairo_move_to(c.cr, C.double(pts[0][0]), C.double(pts[0][1]))
	for _, p := range pts[1:] {
		C.cairo_line_to(c.cr, C.double(p[0]), C.double(p[1]))
	}
	C.cairo_close_path(c.cr)
	// fill, then stroke 2r wide with round joins: corners become arcs of radius r
	C.cairo_fill_preserve(c.cr)
	C.cairo_set_line_width(c.cr, C.double(2*r))
	C.cairo_set_line_join(c.cr, C.CAIRO_LINE_JOIN_ROUND)
	C.cairo_stroke(c.cr)
}

var (
	fontSans = C.CString("Sans")
	ellipsis = "…"
)

func (c cairoCanvas) Text(s string, x, y, w, h int, px float64, bold bool, col Color) {
	weight := C.cairo_font_weight_t(C.CAIRO_FONT_WEIGHT_NORMAL)
	if bold {
		weight = C.CAIRO_FONT_WEIGHT_BOLD
	}
	C.cairo_select_font_face(c.cr, fontSans, C.CAIRO_FONT_SLANT_NORMAL, weight)
	C.cairo_set_font_size(c.cr, C.double(px))

	measure := func(t string) (float64, float64) {
		ct := C.CString(t)
		defer C.free(unsafe.Pointer(ct))
		var ext C.cairo_text_extents_t
		C.cairo_text_extents(c.cr, ct, &ext)
		return float64(ext.x_advance), float64(ext.x_bearing)
	}
	// shorten with an ellipsis until it fits
	text := s
	adv, _ := measure(text)
	for adv > float64(w) && utf8.RuneCountInString(text) > 1 {
		r := []rune(text)
		if string(r[len(r)-1:]) == ellipsis {
			r = r[:len(r)-1]
		}
		text = string(r[:len(r)-1]) + ellipsis
		adv, _ = measure(text)
	}
	var fe C.cairo_font_extents_t
	C.cairo_font_extents(c.cr, &fe)
	baseline := float64(y) + float64(h)/2 + (float64(fe.ascent)-float64(fe.descent))/2
	c.setColor(col)
	ct := C.CString(text)
	defer C.free(unsafe.Pointer(ct))
	C.cairo_move_to(c.cr, C.double(float64(x)+(float64(w)-adv)/2), C.double(baseline))
	C.cairo_show_text(c.cr, ct)
	C.cairo_new_path(c.cr)
}

// paintTo clears a cairo surface to transparent and runs paint on it.
func paintTo(surf *C.cairo_surface_t, paint func(cv Canvas)) {
	cr := C.cairo_create(surf)
	defer C.cairo_destroy(cr)
	C.cairo_set_operator(cr, C.CAIRO_OPERATOR_SOURCE)
	C.cairo_set_source_rgba(cr, 0, 0, 0, 0)
	C.cairo_paint(cr)
	C.cairo_set_operator(cr, C.CAIRO_OPERATOR_OVER)
	C.cairo_set_antialias(cr, C.CAIRO_ANTIALIAS_BEST)
	paint(cairoCanvas{cr})
	C.cairo_surface_flush(surf)
}

// renderPNG draws paint into a size×size PNG file (used for screenshots and tests).
func renderPNG(path string, size int, paint func(cv Canvas)) error {
	return renderPNGSize(path, size, size, paint)
}

// renderPNGSize draws paint into a w×h PNG file (used by the demo animations).
func renderPNGSize(path string, w, h int, paint func(cv Canvas)) error {
	surf := C.cairo_image_surface_create(C.CAIRO_FORMAT_ARGB32, C.int(w), C.int(h))
	defer C.cairo_surface_destroy(surf)
	paintTo(surf, paint)
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.cairo_surface_write_to_png(surf, cpath) != C.CAIRO_STATUS_SUCCESS {
		return errors.New("could not write " + path)
	}
	return nil
}

// ---------- clipboard ----------

// serveClipboard puts text (and, if paths is not empty, the files themselves)
// on the clipboard and answers paste requests until another program takes the
// clipboard over. It blocks: call it last.
func serveClipboard(d displayPtr, text string, paths []string) {
	win := C.mandarino_clipboard_window(d)
	if win == 0 {
		notify(appName, "Could not use the clipboard.", true)
		return
	}
	atom := func(name string) C.ulong {
		cs := C.CString(name)
		defer C.free(unsafe.Pointer(cs))
		return C.ulong(C.XInternAtom(d, cs, 0))
	}
	var (
		aTargets  = atom("TARGETS")
		aUTF8     = atom("UTF8_STRING")
		aText     = atom("TEXT")
		aString   = atom("STRING")
		aPlain    = atom("text/plain")
		aPlainU   = atom("text/plain;charset=utf-8")
		aURIs     = atom("text/uri-list")
		aGnome    = atom("x-special/gnome-copied-files")
		aAtomType = atom("ATOM")
	)
	targets := []C.ulong{aTargets, aUTF8, aText, aString, aPlain, aPlainU}
	uris := fileURIs(paths)
	if len(paths) > 0 {
		targets = append(targets, aURIs, aGnome)
	}
	reply := func(e *C.mevent, typ C.ulong, format int, data unsafe.Pointer, n int) {
		C.mandarino_reply(d, e.requestor, e.selection, e.target, e.property, e.time, typ, C.int(format), data, C.int(n))
	}
	sendBytes := func(e *C.mevent, typ C.ulong, b []byte) {
		if len(b) == 0 {
			b = []byte{0}
		}
		cb := C.CBytes(b)
		defer C.free(cb)
		reply(e, typ, 8, cb, len(b))
	}
	for {
		var e C.mevent
		switch C.mandarino_next_event(d, &e) {
		case C.SelectionClear:
			return // something else was copied: our job is done
		case C.SelectionRequest:
			switch t := e.target; {
			case t == aTargets:
				reply(&e, aAtomType, 32, unsafe.Pointer(&targets[0]), len(targets))
			case t == aUTF8 || t == aText || t == aPlainU:
				sendBytes(&e, aUTF8, []byte(text))
			case t == aString || t == aPlain:
				sendBytes(&e, t, []byte(text))
			case t == aURIs && len(paths) > 0:
				sendBytes(&e, aURIs, []byte(strings.Join(uris, "\r\n")+"\r\n"))
			case t == aGnome && len(paths) > 0:
				sendBytes(&e, aGnome, []byte("copy\n"+strings.Join(uris, "\n")))
			default:
				reply(&e, 0, 8, nil, 0) // unknown format: refuse
			}
		}
	}
}

// ---------- X11 window ----------

// xwin is a borderless, round, always-on-top window drawn with cairo.
//
// On X11 it is an "override-redirect" window placed under the mouse, with
// keyboard and pointer grabs. On Wayland (XWayland) it is a "managed" window:
// grabs and the pointer position are not available there, so the window is
// centred on the screen, gets the focus like a dialog and closes when it loses it.
type xwin struct {
	managed    bool
	dpy        *C.Display
	win        C.Window
	vis        *C.Visual
	surf       *C.cairo_surface_t
	size       int
	x, y       int  // top-left corner on screen
	compositor bool // true when transparency works (otherwise the window is clipped to a circle)
}

// displayPtr lets files without cgo hold an X connection.
type displayPtr = *C.Display

// openDisplay connects to the X server (XWayland on Wayland desktops).
func openDisplay() (*C.Display, error) {
	C.mandarino_set_error_handler()
	d := C.XOpenDisplay(nil)
	if d == nil {
		return nil, errors.New("cannot connect to the X display (DISPLAY is not set or XWayland is missing)")
	}
	return d, nil
}

// onWayland reports whether we run inside a Wayland session (through XWayland).
// MANDARINO_X11=1 forces the X11 behaviour (grabs, wheel under the mouse).
func onWayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("MANDARINO_X11") == ""
}

// pointerPos returns the mouse position on screen.
func pointerPos(d *C.Display) (int, int) {
	var x, y C.int
	C.mandarino_pointer(d, &x, &y)
	return int(x), int(y)
}

// screenScale returns the UI scale from Xft.dpi (set by GNOME, KDE, Xfce...),
// or from MANDARINO_SCALE if the user wants to override it.
func screenScale(d *C.Display) float64 {
	if v, err := strconv.ParseFloat(os.Getenv("MANDARINO_SCALE"), 64); err == nil && v >= 0.5 && v <= 4 {
		return v
	}
	if rm := C.XResourceManagerString(d); rm != nil {
		m := regexp.MustCompile(`(?m)^Xft\.dpi:\s*([\d.]+)`).FindStringSubmatch(C.GoString(rm))
		if m != nil {
			if dpi, err := strconv.ParseFloat(m[1], 64); err == nil && dpi >= 48 {
				return dpi / 96
			}
		}
	}
	return 1
}

// clampToScreen keeps a square of the given size, centred on (cx, cy), on screen.
func clampToScreen(d *C.Display, cx, cy, size int) (int, int) {
	w, h := int(C.mandarino_screen_w(d)), int(C.mandarino_screen_h(d))
	half := size / 2
	return min(max(cx, half), max(w-half, half)), min(max(cy, half), max(h-half, half))
}

// newXWin creates (but does not show) a window of the given size centred on (cx, cy).
func newXWin(d *C.Display, size, cx, cy int) *xwin {
	w := &xwin{dpy: d, size: size, managed: onWayland()}
	if w.managed { // the pointer position is unknown over native Wayland windows
		cx, cy = int(C.mandarino_screen_w(d))/2, int(C.mandarino_screen_h(d))/2
	}
	var depth C.int
	w.vis = C.mandarino_argb_visual(d, C.XDefaultScreen(d), &depth)
	w.compositor = depth == 32 && C.mandarino_has_compositor(d) != 0
	cx, cy = clampToScreen(d, cx, cy, size)
	w.x, w.y = cx-size/2, cy-size/2
	override := C.int(1)
	if w.managed {
		override = 0
	}
	w.win = C.mandarino_create_window(d, w.vis, depth, C.int(w.x), C.int(w.y), C.int(size), override)
	if w.managed {
		C.mandarino_make_managed(d, w.win, C.int(w.x), C.int(w.y), C.int(size))
	}
	title := C.CString("Mandarino")
	C.mandarino_set_title(d, w.win, title)
	C.free(unsafe.Pointer(title))
	w.surf = C.cairo_xlib_surface_create(d, C.Drawable(w.win), w.vis, C.int(size), C.int(size))
	return w
}

// setShape clips the window to a circle of radius r when there is no compositor.
func (w *xwin) setShape(r int) {
	if !w.compositor {
		C.mandarino_shape_circle(w.dpy, w.win, C.int(w.size), C.int(w.size/2), C.int(w.size/2), C.int(r+2))
	}
}

func (w *xwin) show() {
	C.XMapRaised(w.dpy, w.win)
	C.XFlush(w.dpy)
}

// unmap hides the window (it can still be destroyed later).
func (w *xwin) unmap() {
	C.XUnmapWindow(w.dpy, w.win)
	C.XFlush(w.dpy)
}

func (w *xwin) paint(fn func(cv Canvas)) {
	paintTo(w.surf, fn)
	C.XFlush(w.dpy)
}

// resize changes the size keeping the same centre.
func (w *xwin) resize(size int) {
	cx, cy := w.x+w.size/2, w.y+w.size/2
	cx, cy = clampToScreen(w.dpy, cx, cy, size)
	w.size = size
	w.x, w.y = cx-size/2, cy-size/2
	if w.managed {
		C.mandarino_make_managed(w.dpy, w.win, C.int(w.x), C.int(w.y), C.int(size))
	}
	C.XMoveResizeWindow(w.dpy, w.win, C.int(w.x), C.int(w.y), C.uint(size), C.uint(size))
	C.cairo_xlib_surface_set_size(w.surf, C.int(size), C.int(size))
}

func (w *xwin) moveTo(x, y int) {
	w.x, w.y = x, y
	C.XMoveWindow(w.dpy, w.win, C.int(x), C.int(y))
	C.XFlush(w.dpy)
}

// grab takes the keyboard and the pointer, so keys reach the wheel and clicks
// outside it can close it. File managers may still hold a grab for a moment
// after their menu closes, so this retries for about a second.
func (w *xwin) grab() bool {
	if w.managed {
		return false // see focus()
	}
	for i := 0; i < 100; i++ {
		if C.mandarino_grab_keyboard(w.dpy, w.win) != 0 {
			C.mandarino_grab_pointer(w.dpy, w.win)
			return true
		}
		sleepMs(10)
	}
	return false
}

// focus asks for the keyboard focus (managed windows, after they are mapped).
func (w *xwin) focus() {
	C.mandarino_focus(w.dpy, w.win)
	C.XFlush(w.dpy)
}

// startMove begins moving the window with the mouse: by hand for override-redirect
// windows (returns false: the caller tracks the motion), through the window
// manager for managed ones (returns true: nothing else to do).
func (w *xwin) startMove(xRoot, yRoot int) bool {
	if !w.managed {
		return false
	}
	C.mandarino_wm_move(w.dpy, w.win, C.int(xRoot), C.int(yRoot))
	return true
}

func (w *xwin) ungrab() {
	C.XUngrabKeyboard(w.dpy, C.CurrentTime)
	C.XUngrabPointer(w.dpy, C.CurrentTime)
	C.XFlush(w.dpy)
}

func (w *xwin) destroy() {
	C.cairo_surface_destroy(w.surf)
	C.XDestroyWindow(w.dpy, w.win)
	C.XFlush(w.dpy)
}

// pending reports whether events are waiting.
func (w *xwin) pending() bool { return C.XPending(w.dpy) > 0 }

// event is a decoded X event.
type event struct {
	kind         int // one of evKey, evPress, evRelease, evMotion, evLeave, evExpose
	x, y         int
	xRoot, yRoot int
	button       int
	key          Key
}

const (
	evOther = iota
	evKey
	evPress
	evRelease
	evMotion
	evLeave
	evExpose
	evMap
	evFocusIn
	evFocusOut
)

func (w *xwin) next() event {
	var m C.mevent
	C.mandarino_next_event(w.dpy, &m)
	e := event{x: int(m.x), y: int(m.y), xRoot: int(m.x_root), yRoot: int(m.y_root), button: int(m.button)}
	switch m._type {
	case C.KeyPress:
		e.kind = evKey
		e.key = keysymToKey(uint64(m.keysym), C.GoString(&m.text[0]))
	case C.ButtonPress:
		e.kind = evPress
	case C.ButtonRelease:
		e.kind = evRelease
	case C.MotionNotify:
		e.kind = evMotion
	case C.LeaveNotify:
		e.kind = evLeave
	case C.Expose:
		e.kind = evExpose
	case C.MapNotify:
		e.kind = evMap
	case C.FocusIn:
		e.kind = evFocusIn
	case C.FocusOut:
		e.kind = evFocusOut
	}
	return e
}

// keysymToKey maps X keysyms to the shared Key codes.
func keysymToKey(ks uint64, text string) Key {
	if len(text) == 1 && text[0] >= '0' && text[0] <= '9' {
		return KeyDigit0 + Key(text[0]-'0')
	}
	switch ks {
	case C.XK_Left, C.XK_KP_Left:
		return KeyLeft
	case C.XK_Right, C.XK_KP_Right:
		return KeyRight
	case C.XK_Up, C.XK_KP_Up:
		return KeyUp
	case C.XK_Down, C.XK_KP_Down:
		return KeyDown
	case C.XK_Tab, C.XK_ISO_Left_Tab:
		return KeyTab
	case C.XK_Return, C.XK_KP_Enter:
		return KeyEnter
	case C.XK_space:
		return KeySpace
	case C.XK_Escape:
		return KeyEsc
	case C.XK_BackSpace:
		return KeyBackspace
	}
	return KeyNone
}
