# Contributing to Mandarino

Thanks for helping! Bug reports, translations, new formats, actions and
file-manager integrations are all welcome.

## Project layout

| Path | What it is |
|------|------------|
| `engine/` | All conversions, no user interface. Testable on any OS: `go test ./engine` |
| `ui_wheel.go`, `ui_range.go`, `ui_folders.go`, `ui_progress.go`, `ui_fruit.go` | The tangerines (convert, actions, page picker, folder browser, progress): logic and drawing, shared by both platforms |
| `ui_canvas.go` | The small `Canvas` interface the drawing code uses (GDI+ on Windows, cairo on Linux) |
| `dragdetect.go` | Recognizes "dragging files + Shift/Ctrl" (pure logic, unit-tested) |
| `jobs.go` | Runs a conversion in the background and summarizes the result |
| **Windows** | |
| `hooks_windows.go` | Low-level mouse/keyboard hooks feeding the drag detector |
| `droptarget_windows.go` | The tangerine as an OLE drop target: dragged files, hover, drop, labels under the drag image |
| `wheel_windows.go`, `progress_windows.go` | The two layered (round, transparent) windows |
| `fileops_windows.go`, `places_windows.go` | The Ctrl actions (clipboard, move, Bluetooth) and the places of the folder browser |
| `gdip_windows.go` | GDI+ implementation of `Canvas` |
| `main_windows.go`, `tray_windows.go`, `registry_windows.go`, `com_windows.go`, `win32_windows.go` | Start-up, tray icon, settings, COM and Win32 declarations |
| **Linux** | |
| `main_linux.go` | Command line, event loops |
| `x11_linux.go` | X11 window, cairo `Canvas`, clipboard owner (cgo) |
| `fileops_linux.go`, `places_linux.go` | The actions and the places of the folder browser |
| `install_linux.go` | `--install` / `--uninstall` for Nautilus, Nemo, Caja, Dolphin, Thunar |
| `demo_linux.go` | `--demo`: renders the README animations and the social preview |
| `scripts/` | `fetch-deps.sh` (ffmpeg/pdfium), `build.sh` (release archives), `mkrsrc.py` (Windows icon/manifest) |
| `packaging/` | Files shipped inside the release archives |

There are **no third-party Go modules**: only the standard library (plus cgo
with X11/cairo on Linux). The Windows build needs no C compiler.

## Building

```sh
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o Mandarino.exe .   # from any OS
sudo apt install libx11-dev libxext-dev libcairo2-dev && go build -o mandarino .  # Linux
scripts/fetch-deps.sh && scripts/build.sh 1.0.0                                    # release archives
```

## Tests

```sh
sudo apt install ffmpeg            # the engine tests call ffmpeg
scripts/fetch-deps.sh linux        # enables the real PDF tests
MANDARINO_TOOLS=$PWD/third_party/linux go test ./...
go vet -unsafeptr=false ./... && GOOS=windows go vet -unsafeptr=false ./...
make screenshots                   # re-renders docs/screenshots with the real drawing code (Linux)
make demo                          # re-renders the README animations and the social preview (Linux)
```

`-unsafeptr=false` is needed because talking to Win32, COM, X11 and pdfium
requires converting between `uintptr` and pointers on purpose.

## Adding a conversion

1. Add the action to `ActionsFor` in `engine/catalog.go` (ID, short label, description).
2. Handle the ID in `runOne` (`engine/run.go`); for ffmpeg-based actions add the
   arguments in `mediaArgs` (`engine/ffmpeg.go`).
3. Add a test in `engine/engine_test.go`.

## Adding a file action

1. Add it to `fileActions()` in `ui_wheel.go` (ID `op:<name>`).
2. Implement `<name>` in `runFileOp` in `fileops_windows.go` and `fileops_linux.go`.

The tangerine adapts automatically to the number of slices (keep it under ~12).

## Pull requests

- Run `gofmt`, the tests and `go vet` (both platforms) before opening a PR.
- Keep comments in English and explain *why*, not only *what*.
- One topic per PR; screenshots are welcome for UI changes.
