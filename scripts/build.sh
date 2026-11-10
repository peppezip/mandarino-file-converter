#!/usr/bin/env bash
# Builds Mandarino and packages ready-to-use archives in dist/.
#
#   dist/Mandarino-<version>-windows-x64.zip     Mandarino.exe + ffmpeg.exe + pdfium.dll + docs
#   dist/mandarino-<version>-linux-x64.tar.gz    mandarino + ffmpeg + libpdfium.so + install.sh + docs
#
# Usage: scripts/build.sh [version] [windows|linux|all]
# Run scripts/fetch-deps.sh first to get ffmpeg and pdfium (without them only
# the bare binaries are built). The Linux build needs cgo and the development
# packages of X11, Xext and cairo (Debian/Ubuntu: libx11-dev libxext-dev libcairo2-dev).
set -euo pipefail

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"
TARGET="${2:-all}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
cd "$ROOT"
mkdir -p "$DIST"

build_windows() {
  local stage="$DIST/Mandarino"
  rm -rf "$stage" && mkdir -p "$stage/licenses"
  echo "Building Windows x64..."
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
    -ldflags "-H=windowsgui -s -w -X main.appVersion=$VERSION" -o "$stage/Mandarino.exe" .
  sed "s/\$/\r/" packaging/windows/README.txt > "$stage/README.txt"   # Windows line endings for Notepad
  cp LICENSE "$stage/licenses/Mandarino-LICENSE.txt"
  cp THIRD_PARTY_NOTICES.md "$stage/licenses/"
  if [ -f third_party/windows/ffmpeg.exe ]; then
    cp third_party/windows/ffmpeg.exe third_party/windows/pdfium.dll "$stage/"
    cp third_party/windows/PDFium-LICENSE.txt "$stage/licenses/" 2>/dev/null || true
  else
    echo "  (third_party/windows missing: run scripts/fetch-deps.sh windows)"
  fi
  (cd "$DIST" && rm -f "Mandarino-$VERSION-windows-x64.zip" && zip -q -9 -r "Mandarino-$VERSION-windows-x64.zip" Mandarino)
  rm -rf "$stage"
  echo "  -> dist/Mandarino-$VERSION-windows-x64.zip"
}

build_linux() {
  local stage="$DIST/mandarino"
  rm -rf "$stage" && mkdir -p "$stage/licenses"
  echo "Building Linux x64..."
  CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.appVersion=$VERSION" -o "$stage/mandarino" .
  cp packaging/linux/install.sh packaging/linux/README.txt "$stage/"
  chmod +x "$stage/install.sh"
  cp LICENSE "$stage/licenses/Mandarino-LICENSE.txt"
  cp THIRD_PARTY_NOTICES.md "$stage/licenses/"
  if [ -f third_party/linux/ffmpeg ]; then
    cp third_party/linux/ffmpeg third_party/linux/libpdfium.so "$stage/"
    cp third_party/linux/PDFium-LICENSE.txt "$stage/licenses/" 2>/dev/null || true
  else
    echo "  (third_party/linux missing: run scripts/fetch-deps.sh linux)"
  fi
  tar -C "$DIST" -czf "$DIST/mandarino-$VERSION-linux-x64.tar.gz" mandarino
  rm -rf "$stage"
  echo "  -> dist/mandarino-$VERSION-linux-x64.tar.gz"
}

case "$TARGET" in
  windows) build_windows ;;
  linux) build_linux ;;
  all) build_windows; build_linux ;;
  *) echo "usage: $0 [version] [windows|linux|all]" >&2; exit 2 ;;
esac
