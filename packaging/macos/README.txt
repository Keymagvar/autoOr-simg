autoOr-simg (Go rewrite) — macOS distribution
==============================================

This zip contains autoOr-simg.app (drag it to /Applications, or just
double-click it from wherever you extracted it) plus the bare
autoOr-simg binary alongside it for terminal/script use.

This build isn't notarized yet, so Gatekeeper will refuse to open
autoOr-simg.app the first time — right-click it -> Open (not a plain
double-click) to get the "Open anyway" prompt, or remove the quarantine
flag yourself first:

    xattr -dr com.apple.quarantine autoOr-simg.app

Once open, it runs as a menu-bar-only app (no Dock icon — see "menu-bar
icon" below) with its own real icon, not a generic one.

To use the bare binary directly from a terminal instead:

    ./autoOr-simg run

This build uses autoOr-simg's pure-Go image codecs rather than an
accelerated native one — there is no macOS build of the ISPC/
libjpeg-turbo libraries the Linux/Windows builds use yet. Fully
functional, just not yet as fast on very large tiles.

One real system dependency this package does NOT and cannot bundle:
macFUSE (https://osxfuse.github.io) or FUSE-T, the FUSE-compatible
filesystem driver autoOr-simg needs to mount scenery for X-Plane to read.
Install one of them once before running `./autoOr-simg run` — this is a
one-time system install, identical to what every FUSE-based tool on
macOS requires in any language.

First run: `./autoOr-simg run` creates a config directory under your
home directory and walks you through the setup web UI at
http://127.0.0.1:5000.

A real macOS menu-bar icon (Open autoOr-simg / Stop / Restart / Shutdown)
appears automatically unless you pass -H/--headless — the exact same
internal/tray code the Linux/Windows builds use. autoOr-simg.app is set
LSUIElement (menu-bar-only, no Dock icon/Cmd+Tab entry), matching that
tray icon's own role as the app's actual UI entry point. Like the rest of
this build, it hasn't been run on real macOS yet.
