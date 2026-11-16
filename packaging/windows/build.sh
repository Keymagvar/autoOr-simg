#!/usr/bin/env bash
# Builds the Windows distributable as a plain zip: just autoOr-simg.exe
# plus a README, no installer — a single statically-linked Go binary needs
# none of the Python original's PyInstaller/NSIS machinery.
#
# CGO_ENABLED=0 is deliberate, not a shortcut: cgofuse needs no cgo at all
# on Windows (it talks to WinFsp's DLL via plain syscalls, unlike its
# Linux/macOS builds, which do need cgo — confirmed by actually building
# both ways), so this produces a genuine cross-compiled
# Windows binary from Linux with zero C toolchain. The one real cost: the
# accelerated cgo codec path (internal/native's ISPC/libjpeg-turbo) isn't
# available under CGO_ENABLED=0, so this binary uses the pure-Go DXT/JPEG
# fallback (internal/native/dxt_go.go, jpeg_go.go) instead — slower, but
# fully functional, and proven end-to-end under Wine (real network tile
# fetch + decode + compress + DDS write) before this script was written.
# Wiring up the accelerated path needs mingw-w64 (or a windows-latest CI
# runner) plus vendoring a Windows libjpeg-turbo static lib that doesn't
# exist in this repo yet (DESIGN.md §13) — not done by this script.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
VERSION="${VERSION:-dev}"
DIST_NAME="autoOr-simg-windows-amd64"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/dist}"
STAGE_DIR="$OUT_DIR/$DIST_NAME"

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"

cd "$REPO_ROOT"

# Embed internal/tray/icon.ico as the .exe's own resource icon (what
# Explorer, the taskbar, and a pinned shortcut all actually show) — without
# this, the binary gets Go's generic default icon. rsrc is pure Go (no
# windres/mingw needed), so this works from the same CGO_ENABLED=0,
# zero-C-toolchain cross-compile this script already does. The generated
# .syso is picked up automatically by `go build` for any package in the
# same directory when GOOS=windows — see cmd/autoortho's own .gitignore
# entry, it's build output, not checked in.
echo "==> embedding icon.ico into the exe resource"
SYSO="cmd/autoortho/rsrc_windows_amd64.syso"
go run github.com/akavel/rsrc@v0.10.2 -ico internal/tray/icon.ico -o "$SYSO" -arch amd64
trap 'rm -f "$REPO_ROOT/$SYSO"' EXIT

echo "==> building autoOr-simg $VERSION (windows/amd64, CGO_ENABLED=0)"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$VERSION" \
	-o "$STAGE_DIR/autoOr-simg.exe" ./cmd/autoortho

file "$STAGE_DIR/autoOr-simg.exe" | grep -q "PE32+" || {
	echo "FAIL: output is not a PE32+ Windows binary" >&2
	exit 1
}

cp "$SCRIPT_DIR/README.txt" "$SCRIPT_DIR/install.ps1" "$STAGE_DIR/"

echo "==> zipping"
# python3's zipfile instead of the `zip` CLI — not every build host (this
# sandbox included) has `zip` installed, but a Go toolchain always implies
# at least some scripting runtime, and python3 is standard on every CI
# runner this will ever build on (linux/windows/macos alike).
python3 - "$OUT_DIR" "$DIST_NAME" <<'PYEOF'
import os, sys, zipfile
out_dir, dist_name = sys.argv[1], sys.argv[2]
stage = os.path.join(out_dir, dist_name)
zip_path = os.path.join(out_dir, dist_name + ".zip")
with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zf:
	for root, _, files in os.walk(stage):
		for name in files:
			full = os.path.join(root, name)
			zf.write(full, os.path.join(dist_name, os.path.relpath(full, stage)))
PYEOF

echo "==> done: $OUT_DIR/$DIST_NAME.zip"
du -h "$OUT_DIR/$DIST_NAME.zip"
