#!/usr/bin/env bash
# Installs this already-extracted autoOr-simg distribution for the current
# user. No root required, no package manager involved — copies two files
# into a per-user directory and registers a desktop launcher.
#
# Run this from inside the extracted tarball directory:
#   tar xzf autoOr-simg-linux-amd64.tar.gz
#   cd autoOr-simg-linux-amd64
#   ./install.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${AUTOORTHO_INSTALL_DIR:-$HOME/.local/opt/autoOr-simg}"
BIN_DIR="${AUTOORTHO_BIN_DIR:-$HOME/.local/bin}"
APPS_DIR="${AUTOORTHO_APPS_DIR:-$HOME/.local/share/applications}"

if ! command -v fusermount3 >/dev/null 2>&1 && ! command -v fusermount >/dev/null 2>&1; then
	echo "warning: no fusermount/fusermount3 found on PATH." >&2
	echo "         autoOr-simg needs libfuse3 installed to mount scenery (this is a" >&2
	echo "         one-time system package, e.g. 'libfuse3-3' / 'fuse3' depending" >&2
	echo "         on your distro) - install it before running 'autoOr-simg run'." >&2
fi

echo "==> installing to $INSTALL_DIR"
mkdir -p "$INSTALL_DIR/lib/linux" "$BIN_DIR" "$APPS_DIR"
cp "$SCRIPT_DIR/autoOr-simg" "$INSTALL_DIR/"
cp "$SCRIPT_DIR/lib/linux/libispc_texcomp.so" "$INSTALL_DIR/lib/linux/"
cp "$SCRIPT_DIR/autoOr-simg.png" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/autoOr-simg"

ln -sf "$INSTALL_DIR/autoOr-simg" "$BIN_DIR/autoOr-simg"

sed "s#%{INSTALL_DIR}#$INSTALL_DIR#g" "$SCRIPT_DIR/autoOr-simg.desktop" > "$APPS_DIR/autoOr-simg.desktop"

# Without this, the launcher is on disk and technically valid, but most
# desktop environments cache their "installed applications" list and won't
# show the new entry (or its icon) in the app menu/search until the next
# login. update-desktop-database isn't on every distro by default (it's
# part of desktop-file-utils), so skip it quietly rather than fail the
# install over a cosmetic refresh.
if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database "$APPS_DIR" >/dev/null 2>&1 || true
fi

echo "==> installed."
echo "    binary:  $INSTALL_DIR/autoOr-simg"
echo "    symlink: $BIN_DIR/autoOr-simg (make sure $BIN_DIR is on your PATH)"
echo "    launcher: $APPS_DIR/autoOr-simg.desktop"
echo
echo "Run 'autoOr-simg run' (or use the autoOr-simg desktop launcher) to start."
