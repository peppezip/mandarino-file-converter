#!/bin/sh
# Installs Mandarino for the current user (no root needed):
# copies this folder to ~/.local/share/mandarino, links the command in
# ~/.local/bin and adds Mandarino to the file managers' right-click menus.
# To remove it: ~/.local/share/mandarino/mandarino --uninstall, then delete that folder.
set -e
SRC="$(cd "$(dirname "$0")" && pwd)"
DEST="${XDG_DATA_HOME:-$HOME/.local/share}/mandarino"

if [ "$SRC" != "$DEST" ]; then
  mkdir -p "$DEST"
  cp -R "$SRC"/. "$DEST"/
fi
chmod +x "$DEST/mandarino" "$DEST/ffmpeg" 2>/dev/null || true

mkdir -p "$HOME/.local/bin"
ln -sf "$DEST/mandarino" "$HOME/.local/bin/mandarino"

"$DEST/mandarino" --install
