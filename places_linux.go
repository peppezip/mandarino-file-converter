//go:build linux

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// The first level of the "Move to…" folder browser on Linux: the XDG user
// folders (localized names come from ~/.config/user-dirs.dirs), the home
// folder, removable drives and the root of the file system.

func init() { placesFn = linuxPlaces }

// xdgUserDirs reads ~/.config/user-dirs.dirs (XDG_DESKTOP_DIR="$HOME/Desktop"...).
func xdgUserDirs(home string) map[string]string {
	dirs := map[string]string{}
	f, err := os.Open(filepath.Join(configHome(), "user-dirs.dirs"))
	if err != nil {
		return dirs
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		v = strings.ReplaceAll(strings.Trim(v, `"`), "$HOME", home)
		dirs[k] = v
	}
	return dirs
}

func linuxPlaces() []folderEntry {
	home, _ := os.UserHomeDir()
	xdg := xdgUserDirs(home)
	var list []folderEntry
	for _, k := range []struct{ name, key, def string }{
		{"Desktop", "XDG_DESKTOP_DIR", "Desktop"},
		{"Documents", "XDG_DOCUMENTS_DIR", "Documents"},
		{"Downloads", "XDG_DOWNLOAD_DIR", "Downloads"},
		{"Pictures", "XDG_PICTURES_DIR", "Pictures"},
		{"Videos", "XDG_VIDEOS_DIR", "Videos"},
		{"Music", "XDG_MUSIC_DIR", "Music"},
	} {
		p := xdg[k.key]
		if p == "" {
			p = filepath.Join(home, k.def)
		}
		if p != home { // unset XDG folders point to $HOME
			list = append(list, folderEntry{name: k.name, path: p})
		}
	}
	list = append(list, folderEntry{name: "Home", path: home})
	// USB sticks and other drives mounted by the desktop
	user := filepath.Base(home)
	for _, base := range []string{filepath.Join("/media", user), filepath.Join("/run/media", user)} {
		if entries, err := os.ReadDir(base); err == nil {
			for _, e := range entries {
				list = append(list, folderEntry{name: e.Name(), path: filepath.Join(base, e.Name())})
			}
		}
	}
	return append(list, folderEntry{name: "Computer", path: "/"})
}
