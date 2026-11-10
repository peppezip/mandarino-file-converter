#!/usr/bin/env bash
# Downloads the helper programs bundled with Mandarino releases into third_party/:
#
#   third_party/windows/ffmpeg.exe    FFmpeg (GPL build by BtbN)   https://github.com/BtbN/FFmpeg-Builds
#   third_party/windows/pdfium.dll    PDFium (bblanchon)           https://github.com/bblanchon/pdfium-binaries
#   third_party/linux/ffmpeg          static FFmpeg for Linux x86-64
#   third_party/linux/libpdfium.so    PDFium for Linux x86-64
#
# Usage: scripts/fetch-deps.sh [windows|linux|all]   (default: all)
set -euo pipefail

FFMPEG_BASE="https://github.com/BtbN/FFmpeg-Builds/releases/download/latest"
PDFIUM_BASE="https://github.com/bblanchon/pdfium-binaries/releases/latest/download"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch() { echo "  downloading $1"; curl -fsSL --retry 3 -o "$2" "$1"; }

windows() {
  local out="$ROOT/third_party/windows"
  mkdir -p "$out"
  echo "Windows dependencies -> $out"
  fetch "$FFMPEG_BASE/ffmpeg-master-latest-win64-gpl.zip" "$TMP/ffmpeg-win.zip"
  unzip -q -o "$TMP/ffmpeg-win.zip" -d "$TMP/ffmpeg-win"
  cp "$(find "$TMP/ffmpeg-win" -name ffmpeg.exe | head -1)" "$out/ffmpeg.exe"
  fetch "$PDFIUM_BASE/pdfium-win-x64.tgz" "$TMP/pdfium-win.tgz"
  mkdir -p "$TMP/pdfium-win" && tar -xzf "$TMP/pdfium-win.tgz" -C "$TMP/pdfium-win"
  cp "$TMP/pdfium-win/bin/pdfium.dll" "$out/pdfium.dll"
  cp "$TMP/pdfium-win/LICENSE" "$out/PDFium-LICENSE.txt" 2>/dev/null || true
}

linux() {
  local out="$ROOT/third_party/linux"
  mkdir -p "$out"
  echo "Linux dependencies -> $out"
  fetch "$FFMPEG_BASE/ffmpeg-master-latest-linux64-gpl.tar.xz" "$TMP/ffmpeg-linux.tar.xz"
  mkdir -p "$TMP/ffmpeg-linux" && tar -xJf "$TMP/ffmpeg-linux.tar.xz" -C "$TMP/ffmpeg-linux"
  cp "$(find "$TMP/ffmpeg-linux" -type f -name ffmpeg | head -1)" "$out/ffmpeg"
  chmod +x "$out/ffmpeg"
  fetch "$PDFIUM_BASE/pdfium-linux-x64.tgz" "$TMP/pdfium-linux.tgz"
  mkdir -p "$TMP/pdfium-linux" && tar -xzf "$TMP/pdfium-linux.tgz" -C "$TMP/pdfium-linux"
  cp "$TMP/pdfium-linux/lib/libpdfium.so" "$out/libpdfium.so"
  cp "$TMP/pdfium-linux/LICENSE" "$out/PDFium-LICENSE.txt" 2>/dev/null || true
}

case "${1:-all}" in
  windows) windows ;;
  linux) linux ;;
  all) windows; linux ;;
  *) echo "usage: $0 [windows|linux|all]" >&2; exit 2 ;;
esac
echo "Done."
