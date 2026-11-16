#!/usr/bin/env bash
# Builds a portable AppImage: one double-clickable file, no install step,
# no root, no package manager — the Linux equivalent of the Windows exe
# and the macOS .app bundle, carrying its own real icon the same way they
# do. Deliberately NOT a Flatpak: Flatpak's sandbox restricts filesystem
# access, and this app's one job is FUSE-mounting onto whatever Custom
# Scenery path the user picks on their *host* filesystem — fighting that
# sandbox for the app's entire purpose would defeat the point of using
# one. AppImage has no such sandbox, so it doesn't have that problem.
#
# Reuses build.sh's own output (the staged binary + icon + lib) rather
# than rebuilding — run build.sh first, or this re-runs it for you.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
VERSION="${VERSION:-dev}"
DIST_NAME="autoOr-simg-linux-amd64"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/dist}"
STAGE_DIR="$OUT_DIR/$DIST_NAME"
ARCH="x86_64"

if [[ ! -x "$STAGE_DIR/autoOr-simg" ]]; then
	echo "==> $STAGE_DIR/autoOr-simg not found, running build.sh first"
	VERSION="$VERSION" OUT_DIR="$OUT_DIR" bash "$SCRIPT_DIR/build.sh"
fi

APPDIR="$OUT_DIR/autoOr-simg.AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/lib/linux"

cp "$STAGE_DIR/autoOr-simg" "$APPDIR/usr/bin/"
cp "$STAGE_DIR/lib/linux/libispc_texcomp.so" "$APPDIR/usr/lib/linux/"
cp "$STAGE_DIR/autoOr-simg.png" "$APPDIR/autoOr-simg.png"

# AppRun is what actually runs when the AppImage is double-clicked (with
# no arguments) or invoked directly from a terminal (with whatever
# arguments the caller passed) — same bridge role as macOS's
# autoOr-simg-launcher wrapper, since the real binary's own CLI needs an
# explicit "run" subcommand (cmd/autoortho/main.go) that a double-click
# can't supply.
cat > "$APPDIR/AppRun" <<'APPRUN'
#!/usr/bin/env bash
HERE="$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" && pwd)"
export LD_LIBRARY_PATH="$HERE/usr/lib/linux${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
if [ "$#" -eq 0 ]; then
	exec "$HERE/usr/bin/autoOr-simg" run
fi
exec "$HERE/usr/bin/autoOr-simg" "$@"
APPRUN
chmod +x "$APPDIR/AppRun"

cat > "$APPDIR/autoOr-simg.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=autoOr-simg
Comment=Unofficial Go rewrite of kubilus1's AutoOrtho scenery streamer
Exec=AppRun
Icon=autoOr-simg
Terminal=false
Categories=Game;Utility;
StartupWMClass=autoOr-simg
DESKTOP

APPIMAGETOOL="$OUT_DIR/appimagetool-x86_64.AppImage"
if [[ ! -x "$APPIMAGETOOL" ]]; then
	echo "==> downloading appimagetool"
	curl -fsSL -o "$APPIMAGETOOL" \
		"https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-x86_64.AppImage"
	chmod +x "$APPIMAGETOOL"
fi

echo "==> building AppImage"
# --appimage-extract-and-run: appimagetool is itself distributed as an
# AppImage, which normally mounts itself via FUSE to run — most CI
# runners (and plenty of real desktops, now that FUSE2 is disappearing
# from distros) don't have that available. This flag extracts it to a
# temp dir and runs it directly instead, sidestepping FUSE entirely for
# the *build* step. The AppImage this produces still works fine on a
# normal desktop either way: most file managers double-click-mount it
# via FUSE2 same as always, and a system without FUSE2 can still run it
# with the same --appimage-extract-and-run flag the user passes.
ARCH="$ARCH" "$APPIMAGETOOL" --appimage-extract-and-run "$APPDIR" \
	"$OUT_DIR/autoOr-simg-$ARCH.AppImage"

echo "==> done: $OUT_DIR/autoOr-simg-$ARCH.AppImage"
du -h "$OUT_DIR/autoOr-simg-$ARCH.AppImage"
