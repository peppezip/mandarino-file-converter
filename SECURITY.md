# Security and privacy

## What Mandarino does with your data

- **Nothing leaves your computer.** Mandarino has no network code at all: no
  update checks, no telemetry, no accounts. Files are converted locally by the
  bundled `ffmpeg` and `pdfium`. (Bluetooth sends files only to the device you
  pick in the system's own Bluetooth dialog.)
- **Conversions never modify the originals.** The new file is written next to
  them, with a name that does not overwrite anything.
- **Files are moved only with "Move to…".** For every other drop on the
  tangerine, Mandarino tells File Explorer that nothing happened.

## Why the Windows version watches the mouse and the keyboard

To recognize "dragging files + Shift/Ctrl", Mandarino installs *low-level mouse
and keyboard hooks* (`hooks_windows.go`). Some antivirus programs flag any
program that does this. The hooks:

- only look at the left mouse button, the pointer position (to tell a drag
  from a click), Shift and Ctrl; while a tangerine is open also at Esc, clicks
  outside it and, in the page picker / folder browser, the few keys they use;
- never store, log or send anything;
- let every event through to the other programs, except those keys and the
  mouse wheel over the page picker while it is open.

The Linux version installs no hooks: the file manager starts it with the
selected files.

You can verify all of this in the source code, and build the program yourself.

## Reporting a vulnerability

Please open a private security advisory on GitHub ("Security" tab >
"Report a vulnerability") instead of a public issue.
