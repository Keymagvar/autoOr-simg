#!/usr/bin/env bash
# Builds the Linux distributable: binary + its one real runtime dependency
# (libispc_texcomp.so, loaded via the $ORIGIN/lib/linux rpath baked into
# internal/native/dxt_cgo.go) laid out side by side, then tarred up.
#
# No root, no package manager, works from any directory on any distro —
# per DESIGN.md §13's decision. libfuse3 itself is NOT bundled (and can't
# be): it's a dlopen'd system dependency the user installs once, same as
# every FUSE-based tool in any language.
#
# New build-time (not runtime) dependency as of the systray session
# (DESIGN.md §14): internal/tray needs an AppIndicator dev package to
# build at all on Linux — ayatana-appindicator3-0.1 (the modern one;
# Ubuntu/Mint/current Arch all ship it) or, via -tags legacy_appindicator,
# the older appindicator3-0.1 (still present on this build machine, and
# what this script actually used for local verification since ayatana
# isn't installed here). Auto-detected below via pkg-config so this
# script keeps working either way.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
VERSION="${VERSION:-dev}"
DIST_NAME="autoOr-simg-linux-amd64"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/dist}"
STAGE_DIR="$OUT_DIR/$DIST_NAME"

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/lib/linux"

TRAY_TAGS=""
if pkg-config --exists ayatana-appindicator3-0.1 2>/dev/null; then
	echo "==> using ayatana-appindicator3-0.1 for the tray icon"
elif pkg-config --exists appindicator3-0.1 2>/dev/null; then
	echo "==> ayatana-appindicator3-0.1 not found, falling back to legacy appindicator3-0.1"
	TRAY_TAGS="-tags legacy_appindicator"
else
	echo "FAIL: neither ayatana-appindicator3-0.1 nor appindicator3-0.1 found via" >&2
	echo "      pkg-config — install libayatana-appindicator3-dev (Debian/Ubuntu)," >&2
	echo "      libayatana-appindicator (Arch), or the legacy libappindicator-gtk3" >&2
	echo "      equivalent before building (DESIGN.md §14)." >&2
	exit 1
fi

echo "==> building autoOr-simg $VERSION"
cd "$REPO_ROOT"
CGO_ENABLED=1 go build -trimpath $TRAY_TAGS -ldflags "-X main.version=$VERSION" -o "$STAGE_DIR/autoOr-simg" ./cmd/autoortho

cp "$REPO_ROOT/internal/native/testdata/lib/linux/libispc_texcomp.so" "$STAGE_DIR/lib/linux/"
cp "$REPO_ROOT/internal/webui/web/logo.png" "$STAGE_DIR/autoOr-simg.png"
cp "$SCRIPT_DIR/autoOr-simg.desktop" "$STAGE_DIR/"
cp "$SCRIPT_DIR/install.sh" "$STAGE_DIR/"
cp "$SCRIPT_DIR/README.txt" "$STAGE_DIR/"
chmod +x "$STAGE_DIR/install.sh" "$STAGE_DIR/autoOr-simg"

echo "==> verifying \$ORIGIN/lib/linux is the first RUNPATH entry (the dev-tree"
echo "    fallback after it is intentional, see internal/native/dxt_cgo.go)"
runpath="$(readelf -d "$STAGE_DIR/autoOr-simg" | grep -i 'RUNPATH\|RPATH')"
if [[ "$runpath" != *'[$ORIGIN/lib/linux'* ]]; then
	echo "FAIL: \$ORIGIN/lib/linux is not the first RUNPATH entry" >&2
	echo "$runpath" >&2
	exit 1
fi
if ldd "$STAGE_DIR/autoOr-simg" | grep -qi "libturbojpeg"; then
	echo "FAIL: binary still dynamically links libturbojpeg (should be static)" >&2
	exit 1
fi
echo "    OK: no dev-tree path in RUNPATH, no dynamic libturbojpeg dependency"

echo "==> tarring"
tar -C "$OUT_DIR" -czf "$OUT_DIR/$DIST_NAME.tar.gz" "$DIST_NAME"

echo "==> done: $OUT_DIR/$DIST_NAME.tar.gz"
du -h "$OUT_DIR/$DIST_NAME.tar.gz"
