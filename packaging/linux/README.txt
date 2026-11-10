MANDARINO - offline file converter for Linux
============================================

INSTALL (no root needed)
    ./install.sh

This copies Mandarino to ~/.local/share/mandarino, adds the "mandarino"
command to ~/.local/bin and adds two entries to your file manager:
"Mandarino" (convert) and "Mandarino Actions" (Bluetooth, Copy, Path, Move to...).
  GNOME Files (Nautilus): right click > Scripts > Mandarino (Ctrl+Shift+M)
                          right click > Scripts > Mandarino Actions (Ctrl+Alt+M)
  Nemo, Caja, Dolphin, Thunar: right click > Convert with Mandarino /
                               Mandarino Actions
  any file manager: right click > Open With > Mandarino
Restart the file manager once (nautilus -q, thunar -q, or log out and in).

Requirements: an X11 or Wayland desktop (Wayland uses XWayland, present by
default on GNOME and KDE), cairo and libX11 (installed on every desktop),
glibc 2.34 or newer (Ubuntu 22.04+, Debian 12+, Fedora 35+ and similar).
On X11 the wheel opens under the mouse; on Wayland in the middle of the screen
(Wayland does not tell applications where the mouse is).
Notifications use notify-send (package libnotify-bin on Debian/Ubuntu).

USE
Select files, open Mandarino from the menu: the wheel appears under the
mouse (moved inwards near the screen edges, never cut). Click a slice (or
arrows + Enter, digits 1-9; Esc cancels).
Convert: the new file is created next to the original. Long operations show
a progress wheel (drag it, x cancels, right click hides it).
Actions: Move to... shows your folders: rest on a folder to open it, click
"Move here" in the middle. Copy / Path put the files or their paths on the
clipboard. Bluetooth uses bluetooth-sendto or blueman-sendto, or opens the
Bluetooth settings.
From a terminal:  mandarino video.mp4    mandarino --actions photo.jpg

UNINSTALL
    mandarino --uninstall
    rm -rf ~/.local/share/mandarino ~/.local/bin/mandarino

FILES
- mandarino       the program
- ffmpeg          audio/video/image engine (static FFmpeg build, GPL)
- libpdfium.so    PDF engine (PDFium, BSD/Apache)
- install.sh      installer
- licenses/       licenses
If ffmpeg is missing, the one installed on the system (sudo apt install ffmpeg) is used.
