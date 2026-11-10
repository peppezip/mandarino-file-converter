package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Output files are always created next to the original and never overwrite
// anything: if "photo.png" exists, the next one is "photo (1).png", and so on.
//
// Several conversions can run at the same time, so a name is "reserved" the
// moment it is chosen, before the file exists on disk. Otherwise two jobs
// could both decide that "photo (1).png" is free.

var (
	reserveMu sync.Mutex
	reserved  = map[string]bool{} // lower-cased paths (Windows paths are case-insensitive)
)

// baseNoExt returns the path without its extension: "/a/b.txt" -> "/a/b".
func baseNoExt(p string) string { return strings.TrimSuffix(p, filepath.Ext(p)) }

// replaceExt swaps the extension: ("/a/b.png", "jpg") -> "/a/b.jpg".
func replaceExt(p, ext string) string { return baseNoExt(p) + "." + ext }

// withSuffix adds a suffix before a new extension:
// ("/a/clip.mov", " (compressed)", "mp4") -> "/a/clip (compressed).mp4".
func withSuffix(p, suffix, ext string) string { return baseNoExt(p) + suffix + "." + ext }

// UniquePath returns p if nothing exists there, otherwise "name (1).ext",
// "name (2).ext"... The returned name stays reserved until release is called.
func UniquePath(p string) string {
	reserveMu.Lock()
	defer reserveMu.Unlock()
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	cand := p
	for i := 1; ; i++ {
		_, err := os.Stat(cand)
		if os.IsNotExist(err) && !reserved[strings.ToLower(cand)] {
			reserved[strings.ToLower(cand)] = true
			return cand
		}
		cand = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
}

// release frees a reserved name once the file has been written (or discarded).
func release(p string) {
	reserveMu.Lock()
	delete(reserved, strings.ToLower(p))
	reserveMu.Unlock()
}
