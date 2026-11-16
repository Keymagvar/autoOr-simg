#!/usr/bin/env bash
# Builds the macOS distributable as a plain zip (same reasoning as
# packaging/windows/build.sh: a single static Go binary needs none of the
# Python original's packaging machinery). Produces a universal-ish
# amd64+arm64 pair of binaries? No — see below.
#
# MUST run natively on a real macOS machine (or a macos-latest CI runner):
# unlike Windows, cgofuse requires real cgo on darwin (confirmed by trying
# both ways from Linux), and there is no osxcross
# toolchain available to cross-compile that cgo with. There is currently no
# accelerated native codec for macOS either (internal/native's ISPC/
# libjpeg-turbo libs are vendored for Linux and Windows only, not macOS —
# DESIGN.md §13), so this build uses the same pure-Go DXT/JPEG fallback
# Windows currently does (internal/native/defaults_cgo_other.go), not
# because CGO_ENABLED=0 was chosen here (it's =1, for cgofuse's own
# darwin cgo requirement) but because no native macOS codec library exists
# yet to link against.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
VERSION="${VERSION:-dev}"
ARCH="$(uname -m)"
case "$ARCH" in
	arm64) GOARCH=arm64 ;;
	x86_64) GOARCH=amd64 ;;
	*) echo "unrecognized macOS arch: $ARCH" >&2; exit 1 ;;
esac
DIST_NAME="autoOr-simg-macos-$GOARCH"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/dist}"
STAGE_DIR="$OUT_DIR/$DIST_NAME"

if [[ "$(uname)" != "Darwin" ]]; then
	echo "this script must run on real macOS (cgofuse needs cgo there, and this" >&2
	echo "repo has no osxcross cross-compile toolchain set up) — see the header" >&2
	echo "comment above." >&2
	exit 1
fi

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"

echo "==> building autoOr-simg $VERSION (darwin/$GOARCH, CGO_ENABLED=1)"
cd "$REPO_ROOT"
CGO_ENABLED=1 GOARCH="$GOARCH" go build -trimpath -ldflags "-X main.version=$VERSION" \
	-o "$STAGE_DIR/autoOr-simg" ./cmd/autoortho
chmod +x "$STAGE_DIR/autoOr-simg"

cp "$SCRIPT_DIR/README.txt" "$STAGE_DIR/"

# Wrap the raw binary in a real .app bundle — without this, double-
# clicking it from Finder does nothing visible (it's a bare CLI binary
# that prints usage and exits with zero args, and Finder doesn't attach a
# terminal to show that). A bundle also gives the menu-bar tray icon a
# proper NSApplication context to run in, and gives the app a real Dock/
# Launchpad icon instead of a generic Unix-executable one. Entirely done
# with tools already on every macOS system (sips, iconutil) — no new
# dependency.
APP_NAME="autoOr-simg"
APP_DIR="$STAGE_DIR/$APP_NAME.app"
CONTENTS="$APP_DIR/Contents"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"

echo "==> building $APP_NAME.icns from internal/tray/icon.png"
# Source art is only 60x60 (internal/tray/icon.png, the same square logo
# the systray icon uses) — sips upscales it for the larger iconset sizes,
# which will look soft at 512/1024px. Swap in a higher-resolution source
# here if one ever gets made; the rest of this pipeline doesn't change.
ICONSET="$OUT_DIR/autoOr-simg.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
	sips -z "$size" "$size" "$REPO_ROOT/internal/tray/icon.png" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	sips -z "$double" "$double" "$REPO_ROOT/internal/tray/icon.png" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$CONTENTS/Resources/AppIcon.icns"
rm -rf "$ICONSET"

cat > "$CONTENTS/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>$APP_NAME</string>
	<key>CFBundleDisplayName</key>
	<string>autoOr-simg</string>
	<key>CFBundleIdentifier</key>
	<string>io.github.keymagvar.autoor-simg</string>
	<key>CFBundleVersion</key>
	<string>$VERSION</string>
	<key>CFBundleShortVersionString</key>
	<string>$VERSION</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleExecutable</key>
	<string>autoOr-simg-launcher</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
	<!-- Menu-bar-only app (the tray icon IS the UI entry point, same as
	     the Linux/Windows tray) — no Dock icon, no Cmd+Tab entry, matching
	     how the tray already behaves on the other two platforms. -->
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
PLIST

cp "$STAGE_DIR/autoOr-simg" "$CONTENTS/MacOS/autoOr-simg"
# Finder launches CFBundleExecutable with no arguments, but the binary's
# own CLI needs an explicit "run" subcommand (see cmd/autoortho/main.go) —
# this tiny wrapper is the bridge, so double-clicking the .app behaves
# like `autoOr-simg run` while the raw binary's own argument handling
# stays untouched for terminal/script use.
cat > "$CONTENTS/MacOS/autoOr-simg-launcher" <<'LAUNCHER'
#!/usr/bin/env bash
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$DIR/autoOr-simg" run "$@"
LAUNCHER
chmod +x "$CONTENTS/MacOS/autoOr-simg" "$CONTENTS/MacOS/autoOr-simg-launcher"

# The bare binary stays at the top level too (alongside the .app), for
# anyone who wants `./autoOr-simg run --headless` directly without
# Finder/the bundle at all.
echo "==> zipping"
python3 - "$OUT_DIR" "$DIST_NAME" <<'PYEOF'
import os, sys, zipfile
out_dir, dist_name = sys.argv[1], sys.argv[2]
stage = os.path.join(out_dir, dist_name)
zip_path = os.path.join(out_dir, dist_name + ".zip")
with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zf:
	for root, _, files in os.walk(stage):
		for name in files:
			full = os.path.join(root, name)
			info = zipfile.ZipInfo(os.path.join(dist_name, os.path.relpath(full, stage)))
			info.external_attr = (0o755 if os.access(full, os.X_OK) else 0o644) << 16
			with open(full, "rb") as fh:
				zf.writestr(info, fh.read())
PYEOF

echo "==> done: $OUT_DIR/$DIST_NAME.zip"
du -h "$OUT_DIR/$DIST_NAME.zip"
