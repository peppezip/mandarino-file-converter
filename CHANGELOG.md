# Changelog

All notable changes are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and versions follow
[Semantic Versioning](https://semver.org/).

## [1.0.0] - 2026-11-09

First public release, for Windows 10/11 (64-bit) and Linux (x86-64).

### Added
- Windows gesture: start dragging files in File Explorer or on the desktop and
  press **Shift** (convert) or **Ctrl** (actions); drop on a slice of the
  tangerine that opens near the pointer. The other key switches tangerine,
  releasing elsewhere is a normal drag & drop, Esc cancels.
- Linux: "Mandarino" and "Mandarino Actions" in the right-click menu of GNOME
  Files (Ctrl+Shift+M / Ctrl+Alt+M), Nemo, Caja, Dolphin and Thunar, plus
  "Open With". X11 and Wayland (XWayland).
- Convert — images: JPG, PNG, WebP, BMP, TIFF, GIF, ICO, PDF, "One PDF", 50%,
  Compress (HEIC/AVIF readable, EXIF rotation applied); video: MP4, MKV, MOV,
  WebM, AVI, animated GIF, extract MP3, Compress, Mute; audio: MP3, WAV, FLAC,
  M4A, OGG, Opus; PDF: pages to PNG/JPG, Merge, Split with a page picker.
- Actions — Bluetooth (system transfer dialog, or Bluetooth settings), Copy
  (files to the clipboard), Path (paths to the clipboard), Move to… with a
  folder browser inside the tangerine (rest on a folder to open it, drop on
  "Move here").
- The tangerine never goes off screen: near the edges it moves inwards.
- Progress tangerine for long conversions (drag, × cancels, right click hides).
- Outputs never overwrite anything; half-written videos are named
  "… - in progress"; video outputs play everywhere (constant frame rate,
  even size, one stereo track).
