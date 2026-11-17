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

# Not $STAGE_DIR/autoOr-simg.png (that's internal/webui/web/logo.png, a
# 508x676 non-square marketing/mascot image) — linuxdeploy's icon
# deployment step requires an exact standard square resolution (256x256
# here) and rejects anything else outright, so a non-square source fails
# the whole build. internal/tray/icon.png is the real small square app
# icon (60x60) already used as the upscale source for the macOS .icns
# (packaging/macos/build.sh) — same approach here, just via ImageMagick
# instead of sips since this runs on Linux.
convert "$REPO_ROOT/internal/tray/icon.png" -resize 256x256 "$APPDIR/autoOr-simg.png"

# The tray feature (internal/tray) cgo-links GTK3 + libappindicator/
# libayatana-appindicator at build time, which pulls in ~60 transitive
# shared libraries (confirmed via `ldd` on a real built binary — GTK3,
# Pango, Cairo, GLib, X11/Wayland client libs, the lot). None of that
# used to get bundled here — only libispc_texcomp.so (this repo's own
# native lib) did — so the AppImage ran fine in CI, where the exact same
# ubuntu-latest box that built it already has every one of those libs
# installed system-wide, and then failed immediately for any real user
# whose machine doesn't ("error while loading shared libraries:
# libayatana-appindicator3.so.1: cannot open shared object file"),
# defeating the entire point of a portable AppImage. CI's own AppImage
# test never caught this because it runs on the same machine that just
# built it, not a clean one.

# Exec names the real binary under usr/bin, NOT AppRun — AppRun (written
# further below) is the separate top-level entry point every AppImage
# runtime always invokes directly on double-click/run regardless of this
# file. linuxdeploy's own desktop-file validation resolves Exec against
# usr/bin and fails the build if it doesn't find a match there, which an
# Exec=AppRun value never will (AppRun lives at the AppDir root, not
# usr/bin).
cat > "$APPDIR/autoOr-simg.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=autoOr-simg
Comment=Unofficial Go rewrite of kubilus1's AutoOrtho scenery streamer
Exec=autoOr-simg
Icon=autoOr-simg
Terminal=false
Categories=Game;Utility;
StartupWMClass=autoOr-simg
DESKTOP

LINUXDEPLOY="$OUT_DIR/linuxdeploy-x86_64.AppImage"
if [[ ! -x "$LINUXDEPLOY" ]]; then
	echo "==> downloading linuxdeploy"
	curl -fsSL -o "$LINUXDEPLOY" \
		"https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage"
	chmod +x "$LINUXDEPLOY"
fi
LINUXDEPLOY_GTK="$OUT_DIR/linuxdeploy-plugin-gtk.sh"
if [[ ! -x "$LINUXDEPLOY_GTK" ]]; then
	echo "==> downloading linuxdeploy-plugin-gtk"
	# Not a GitHub Release asset (the repo has none) — it's distributed as
	# a raw script straight from the default branch, same as its own
	# install instructions document.
	curl -fsSL -o "$LINUXDEPLOY_GTK" \
		"https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk.sh"
	chmod +x "$LINUXDEPLOY_GTK"
fi

echo "==> bundling GTK3/appindicator shared libraries + runtime assets (linuxdeploy)"
# --plugin gtk needs its plugin script found as literally
# "linuxdeploy-plugin-gtk.sh" on PATH — not a linuxdeploy flag, that's how
# linuxdeploy's own plugin discovery works. --appimage-extract-and-run:
# same FUSE-less-CI reasoning as appimagetool below. This bundles every
# ldd-visible shared library (same mechanism as the single lib we used to
# hand-copy above) AND the GTK-specific files ldd can't see at all,
# because GTK loads them at runtime via dlopen/discovery, not regular
# dynamic linking: icon themes, gdk-pixbuf loaders, GSettings schemas,
# input method modules. It also writes apprun-hooks/linuxdeploy-plugin-
# gtk.sh, which our own AppRun below sources to point the bundled GTK
# stack at those bundled assets instead of (absent, on a real user's
# machine) system ones.
PATH="$OUT_DIR:$PATH" "$LINUXDEPLOY" --appimage-extract-and-run \
	--appdir "$APPDIR" \
	--executable "$APPDIR/usr/bin/autoOr-simg" \
	--desktop-file "$APPDIR/autoOr-simg.desktop" \
	--icon-file "$APPDIR/autoOr-simg.png" \
	--plugin gtk

# AppRun is what actually runs when the AppImage is double-clicked (with
# no arguments) or invoked directly from a terminal (with whatever
# arguments the caller passed) — same bridge role as macOS's
# autoOr-simg-launcher wrapper, since the real binary's own CLI needs an
# explicit "run" subcommand (cmd/autoortho/main.go) that a double-click
# can't supply. Written after linuxdeploy runs, deliberately overwriting
# whatever generic AppRun it generated — ours still needs the
# subcommand-default logic above, which a stock AppRun doesn't have.
cat > "$APPDIR/AppRun" <<'APPRUN'
#!/usr/bin/env bash
HERE="$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" && pwd)"
export LD_LIBRARY_PATH="$HERE/usr/lib/linux:$HERE/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
# Sourced hooks (if linuxdeploy's gtk plugin ran) point GTK at its own
# bundled icon theme/pixbuf loaders/schemas instead of a real user's
# system ones, which may not exist at all or may mismatch this bundle's
# GTK version.
if [ -d "$HERE/apprun-hooks" ]; then
	for hook in "$HERE"/apprun-hooks/*.sh; do
		[ -f "$hook" ] && . "$hook"
	done
fi
if [ "$#" -eq 0 ]; then
	exec "$HERE/usr/bin/autoOr-simg" run
fi
exec "$HERE/usr/bin/autoOr-simg" "$@"
APPRUN
chmod +x "$APPDIR/AppRun"

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
