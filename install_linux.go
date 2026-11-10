//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"mandarino/engine"
)

// "mandarino --install" adds Mandarino to the file managers of the current
// user. Everything goes into the home folder (no root needed) and
// "mandarino --uninstall" removes exactly the same files.
//
//	~/.local/share/applications/mandarino.desktop          "Open With" in every file manager
//	~/.local/share/icons/hicolor/256x256/apps/mandarino.png icon
//	~/.local/share/nautilus/scripts/Mandarino[ Actions]     GNOME Files: right click > Scripts
//	~/.config/nautilus/scripts-accels                       GNOME Files: Ctrl+Shift+M / Ctrl+Alt+M
//	~/.local/share/nemo/actions/mandarino*.nemo_action      Nemo (Cinnamon)
//	~/.config/caja/scripts/Mandarino[ Actions]              Caja (MATE)
//	~/.local/share/kio/servicemenus/mandarino*.desktop      Dolphin (KDE 6 / Plasma 5.85+)
//	~/.local/share/kservices5/ServiceMenus/mandarino*.desktop Dolphin (older KDE 5)
//	~/.config/Thunar/uca.xml                                Thunar (Xfce) custom actions
//
// Every file manager gets two entries: "Mandarino" (the convert wheel) and
// "Mandarino Actions" (Bluetooth, Copy, Path, Move to…), which also works on folders.

// supported extensions, used by the menus that filter by extension
var supportedExts = []string{
	"jpg", "jpeg", "png", "webp", "bmp", "tif", "tiff", "gif", "ico", "heic", "heif", "avif", "tga",
	"mp4", "m4v", "mov", "mkv", "webm", "avi", "wmv", "flv", "mpg", "mpeg", "3gp", "ts", "mts", "m2ts",
	"mp3", "wav", "flac", "m4a", "aac", "ogg", "oga", "opus", "wma", "aiff", "aif", "amr", "pdf",
}

// supported MIME types, used by "Open With" and Dolphin
const mimeTypes = "image/jpeg;image/png;image/webp;image/bmp;image/tiff;image/gif;image/x-icon;" +
	"image/vnd.microsoft.icon;image/heic;image/heif;image/avif;image/x-tga;" +
	"video/mp4;video/quicktime;video/x-matroska;video/webm;video/x-msvideo;video/x-ms-wmv;" +
	"video/x-flv;video/mpeg;video/3gpp;video/mp2t;" +
	"audio/mpeg;audio/wav;audio/x-wav;audio/flac;audio/mp4;audio/x-m4a;audio/aac;audio/ogg;" +
	"audio/opus;audio/x-ms-wma;audio/x-aiff;audio/amr;application/pdf;"

const (
	thunarID        = "mandarino-file-converter"
	thunarActionsID = "mandarino-file-actions"
)

type installFile struct {
	path    string
	content string
	exec    bool // make it executable
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local", "share")
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	return filepath.Join(home(), ".config")
}

// shellQuote quotes a path for a POSIX shell script.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// desktopQuote quotes a path for the Exec key of .desktop files.
func desktopQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", "$", `\\$`)
	return `"` + r.Replace(s) + `"`
}

// installFiles lists every file written by --install.
func installFiles(exe string) []installFile {
	script := "#!/bin/sh\n# Added by Mandarino (mandarino --uninstall removes it)\nexec " + shellQuote(exe) + " \"$@\"\n"
	actionsScript := "#!/bin/sh\n# Added by Mandarino (mandarino --uninstall removes it)\nexec " + shellQuote(exe) + " --actions \"$@\"\n"
	exts := strings.Join(supportedExts, ";") + ";"
	return []installFile{
		{path: filepath.Join(dataHome(), "applications", "mandarino.desktop"), content: `[Desktop Entry]
Type=Application
Name=Mandarino
GenericName=File converter
Comment=Convert images, videos, audio and PDFs offline
Exec=` + desktopQuote(exe) + ` %F
Icon=mandarino
Terminal=false
StartupNotify=false
Categories=Utility;AudioVideo;Graphics;
Keywords=convert;converter;compress;pdf;video;audio;image;
MimeType=` + mimeTypes + "\n"},
		{path: filepath.Join(dataHome(), "nautilus", "scripts", "Mandarino"), content: script, exec: true},
		{path: filepath.Join(dataHome(), "nautilus", "scripts", "Mandarino Actions"), content: actionsScript, exec: true},
		{path: filepath.Join(configHome(), "caja", "scripts", "Mandarino"), content: script, exec: true},
		{path: filepath.Join(configHome(), "caja", "scripts", "Mandarino Actions"), content: actionsScript, exec: true},
		{path: filepath.Join(dataHome(), "nemo", "actions", "mandarino-actions.nemo_action"), content: `[Nemo Action]
Name=Mandarino Actions (Bluetooth, copy, move…)
Comment=Send, copy or move the selection with Mandarino
Exec=` + desktopQuote(exe) + ` --actions %F
Icon-Name=mandarino
Selection=notnone
Extensions=any;
Quote=double
`},
		{path: filepath.Join(dataHome(), "nemo", "actions", "mandarino.nemo_action"), content: `[Nemo Action]
Name=Convert with Mandarino
Comment=Convert the selected files with Mandarino
Exec=` + desktopQuote(exe) + ` %F
Icon-Name=mandarino
Selection=notnone
Extensions=` + exts + `
Quote=double
`},
		{path: filepath.Join(dataHome(), "kio", "servicemenus", "mandarino.desktop"), content: dolphinMenu(exe), exec: true},
		{path: filepath.Join(dataHome(), "kservices5", "ServiceMenus", "mandarino.desktop"), content: dolphinMenu(exe)},
		{path: filepath.Join(dataHome(), "kio", "servicemenus", "mandarino-actions.desktop"), content: dolphinActionsMenu(exe), exec: true},
		{path: filepath.Join(dataHome(), "kservices5", "ServiceMenus", "mandarino-actions.desktop"), content: dolphinActionsMenu(exe)},
	}
}

func dolphinMenu(exe string) string {
	return `[Desktop Entry]
Type=Service
ServiceTypes=KonqPopupMenu/Plugin
X-KDE-ServiceTypes=KonqPopupMenu/Plugin
MimeType=` + mimeTypes + `
Actions=mandarino

[Desktop Action mandarino]
Name=Convert with Mandarino
Icon=mandarino
Exec=` + desktopQuote(exe) + ` %F
`
}

func dolphinActionsMenu(exe string) string {
	return `[Desktop Entry]
Type=Service
ServiceTypes=KonqPopupMenu/Plugin
X-KDE-ServiceTypes=KonqPopupMenu/Plugin
MimeType=all/all;
Actions=mandarinoActions

[Desktop Action mandarinoActions]
Name=Mandarino Actions (Bluetooth, copy, move…)
Icon=mandarino
Exec=` + desktopQuote(exe) + ` --actions %F
`
}

func iconPath() string {
	return filepath.Join(dataHome(), "icons", "hicolor", "256x256", "apps", "mandarino.png")
}

func install(exe string) error {
	if _, err := engine.FFmpegPath(); err != nil {
		fmt.Println("Warning:", err)
	}
	var done []string
	for _, f := range installFiles(exe) {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.exec {
			mode = 0o755
		}
		if err := os.WriteFile(f.path, []byte(f.content), mode); err != nil {
			return err
		}
		os.Chmod(f.path, mode) // WriteFile keeps the old mode of existing files
		done = append(done, f.path)
	}
	if err := os.MkdirAll(filepath.Dir(iconPath()), 0o755); err == nil {
		if err := renderPNG(iconPath(), 256, func(cv Canvas) { drawIcon(cv, 256) }); err == nil {
			done = append(done, iconPath())
		}
	}
	if err := setNautilusShortcut(true); err == nil {
		done = append(done, filepath.Join(configHome(), "nautilus", "scripts-accels")+" (Ctrl+Shift+M, Ctrl+Alt+M)")
	}
	if p, err := thunarAction(exe, true); err == nil && p != "" {
		done = append(done, p)
	}
	refreshCaches()

	fmt.Println("Mandarino is installed:")
	for _, p := range done {
		fmt.Println("  ", p)
	}
	fmt.Println(`
How to use it: select files in your file manager, then
  GNOME Files:  right click > Scripts > Mandarino          (or Ctrl+Shift+M)
                right click > Scripts > Mandarino Actions  (or Ctrl+Alt+M)
  Nemo, Dolphin, Thunar: right click > Convert with Mandarino / Mandarino Actions
  any file manager:      right click > Open With > Mandarino
You may need to close and reopen the file manager once (nautilus -q, thunar -q).
Do not move this folder, or run "mandarino --install" again after moving it.`)
	return nil
}

func uninstall() error {
	var errs []error
	for _, f := range installFiles("") {
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if err := os.Remove(iconPath()); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	setNautilusShortcut(false)
	thunarAction("", false)
	refreshCaches()
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	fmt.Println("Mandarino was removed from your file managers. You can now delete its folder.")
	return nil
}

// setNautilusShortcut adds (or removes) our two lines in scripts-accels.
func setNautilusShortcut(add bool) error {
	path := filepath.Join(configHome(), "nautilus", "scripts-accels")
	ours := []string{"<Control><Shift>m Mandarino", "<Control><Alt>m Mandarino Actions"}
	data, _ := os.ReadFile(path)
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasSuffix(t, " Mandarino") && !strings.HasSuffix(t, " Mandarino Actions") {
			lines = append(lines, l)
		}
	}
	if add {
		lines = append(lines, ours...)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// thunarAction adds (or removes) a custom action in Thunar's uca.xml.
func thunarAction(exe string, add bool) (string, error) {
	path := filepath.Join(configHome(), "Thunar", "uca.xml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if os.IsNotExist(err) {
		if !add {
			return "", nil
		}
		if _, statErr := os.Stat(filepath.Dir(path)); statErr != nil {
			return "", nil // Thunar was never used: nothing to do
		}
		data = []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<actions>\n</actions>\n")
	}
	// remove any previous Mandarino actions (from the end, so offsets stay valid)
	s := string(data)
	re := regexp.MustCompile(`(?s)\s*<action>.*?</action>`)
	locs := re.FindAllStringIndex(s, -1)
	for i := len(locs) - 1; i >= 0; i-- {
		block := s[locs[i][0]:locs[i][1]]
		if strings.Contains(block, "<unique-id>"+thunarID+"</unique-id>") ||
			strings.Contains(block, "<unique-id>"+thunarActionsID+"</unique-id>") {
			s = s[:locs[i][0]] + s[locs[i][1]:]
		}
	}
	if add {
		var pats []string
		for _, e := range supportedExts {
			pats = append(pats, "*."+e)
		}
		action := `
<action>
	<icon>mandarino</icon>
	<name>Convert with Mandarino</name>
	<submenu></submenu>
	<unique-id>` + thunarID + `</unique-id>
	<command>` + xmlEscape(shellQuote(exe)) + ` %F</command>
	<description>Convert the selected files with Mandarino</description>
	<range>*</range>
	<patterns>` + strings.Join(pats, ";") + `</patterns>
	<image-files/>
	<audio-files/>
	<video-files/>
	<other-files/>
</action>
<action>
	<icon>mandarino</icon>
	<name>Mandarino Actions (Bluetooth, copy, move…)</name>
	<submenu></submenu>
	<unique-id>` + thunarActionsID + `</unique-id>
	<command>` + xmlEscape(shellQuote(exe)) + ` --actions %F</command>
	<description>Send, copy or move the selection with Mandarino</description>
	<range>*</range>
	<patterns>*</patterns>
	<directories/>
	<image-files/>
	<audio-files/>
	<video-files/>
	<text-files/>
	<other-files/>
</action>`
		i := strings.LastIndex(s, "</actions>")
		if i < 0 {
			return "", errors.New("unexpected format of " + path)
		}
		s = s[:i] + strings.TrimLeft(action, "\n") + "\n" + s[i:]
	}
	return path, os.WriteFile(path, []byte(s), 0o644)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// refreshCaches asks the desktop to notice the new .desktop file and icon (best effort).
func refreshCaches() {
	if bin, err := exec.LookPath("update-desktop-database"); err == nil {
		exec.Command(bin, filepath.Join(dataHome(), "applications")).Run()
	}
	if bin, err := exec.LookPath("gtk-update-icon-cache"); err == nil {
		exec.Command(bin, "-f", "-t", filepath.Join(dataHome(), "icons", "hicolor")).Run()
	}
}
