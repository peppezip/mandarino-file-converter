//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"mandarino/engine"
)

// The actions of the actions wheel on Linux ("mandarino --actions FILE...").
//
//   - "path": the full path(s) as text on the clipboard;
//   - "copy": the files on the clipboard (text/uri-list and the GNOME format),
//     so Paste in the file manager copies them;
//   - "move": moved into the chosen folder (never overwriting: "name (1)");
//   - "bt":   sent with the desktop's Bluetooth tool (bluetooth-sendto,
//     blueman-sendto), or the Bluetooth settings open if there is none.
//
// X11 has no clipboard daemon of its own: the program that copies keeps the
// data and hands it out on request. So after "path" and "copy" Mandarino
// stays in the background (no window) until something else is copied.

func runFileOp(d displayPtr, op string, paths []string, dest string) int {
	switch op {
	case "path":
		text := strings.Join(paths, "\n")
		notify("Path copied", text, false)
		serveClipboard(d, text, nil)
	case "copy":
		notify("Copied", countItems(paths)+": paste it in any folder.", false)
		serveClipboard(d, strings.Join(paths, "\n"), paths)
	case "move":
		moved, err := moveAll(paths, dest)
		if err != nil {
			notify("Could not move", err.Error(), true)
			return 1
		}
		notify("Moved", fmt.Sprintf("%s moved to %s.", countItems(moved), displayName(dest)), false)
	case "bt":
		return sendBluetooth(paths)
	}
	return 0
}

// countItems returns "photo.jpg" or "3 items".
func countItems(paths []string) string {
	if len(paths) == 1 {
		return filepath.Base(paths[0])
	}
	return fmt.Sprintf("%d items", len(paths))
}

// fileURIs converts paths to file:// URIs.
func fileURIs(paths []string) []string {
	var out []string
	for _, p := range paths {
		out = append(out, (&url.URL{Scheme: "file", Path: p}).String())
	}
	return out
}

// moveAll moves every path into dest and returns the new paths.
func moveAll(paths []string, dest string) ([]string, error) {
	var moved []string
	var errs []error
	for _, p := range paths {
		target := engine.UniquePath(filepath.Join(dest, filepath.Base(p)))
		err := os.Rename(p, target)
		if errors.Is(err, syscall.EXDEV) { // another drive: copy, then delete
			if err = copyTree(p, target); err == nil {
				err = os.RemoveAll(p)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(p), err))
			continue
		}
		moved = append(moved, target)
	}
	if len(moved) == 0 {
		return nil, errors.Join(errs...)
	}
	return moved, nil
}

// copyTree copies a file or a whole folder, keeping permissions.
func copyTree(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		link, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(link, dst)
	case st.IsDir():
		if err := os.MkdirAll(dst, st.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// sendBluetooth uses the desktop's Bluetooth sender, or opens the settings.
func sendBluetooth(paths []string) int {
	for _, tool := range []string{"bluetooth-sendto", "blueman-sendto"} {
		if bin, err := exec.LookPath(tool); err == nil {
			if exec.Command(bin, paths...).Start() == nil {
				return 0
			}
		}
	}
	for _, settings := range [][]string{
		{"gnome-control-center", "bluetooth"},
		{"systemsettings", "kcm_bluetooth"},
		{"blueman-manager"},
	} {
		if bin, err := exec.LookPath(settings[0]); err == nil {
			if exec.Command(bin, settings[1:]...).Start() == nil {
				notify("Bluetooth", "Pair your device in the Bluetooth settings, then try again.", false)
				return 0
			}
		}
	}
	notify("Bluetooth", "No Bluetooth tool found (install gnome-bluetooth or blueman).", true)
	return 1
}
