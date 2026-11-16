autoOr-simg (Go rewrite) — Linux distribution
==============================================

This directory is self-contained: the autoOr-simg binary and its one real
runtime dependency (lib/linux/libispc_texcomp.so, the ISPC BC1 texture
compressor) sit side by side and the binary finds the library relative to
its own location — no absolute paths, no install step required to just run
it from wherever you extracted it:

    ./autoOr-simg run

Optional: run ./install.sh to copy this into ~/.local/opt/autoOr-simg, add
an `autoOr-simg` command to ~/.local/bin, and register a desktop launcher.
No root is needed for any of this.

Prefer a single portable file instead of a tarball? Download
autoOr-simg-x86_64.AppImage from the same release instead — one
double-clickable file with its own real icon, no install step at all, same
philosophy as the Windows exe and macOS .app:

    chmod +x autoOr-simg-x86_64.AppImage
    ./autoOr-simg-x86_64.AppImage

Most file managers double-click-mount an AppImage via FUSE2 automatically.
If yours doesn't have FUSE2 (increasingly common — many current distros
ship FUSE3 only), run it with --appimage-extract-and-run instead:

    ./autoOr-simg-x86_64.AppImage --appimage-extract-and-run run

Either way it's the same binary with the same libfuse3 requirement below
for autoOr-simg's own scenery mount — FUSE2 (if needed at all) is only ever
about the AppImage container unpacking itself, not autoOr-simg's own
functionality.

One real system dependency this package does NOT and cannot bundle: libfuse3
(the Linux FUSE userspace library). autoOr-simg mounts a virtual filesystem for
X-Plane to read scenery through, and that requires libfuse3 to be installed
once via your distro's package manager — e.g.:

    Debian/Ubuntu:  sudo apt install fuse3
    Fedora:         sudo dnf install fuse3
    Arch:           sudo pacman -S fuse3

This is identical to what the original Python version of AutoOrtho already
requires — no FUSE-based tool in any language can avoid it.

The taskbar tray icon (Open/Stop/Shutdown) needs GTK3 + libappindicator (or
its modern ayatana-appindicator fork), which a normal desktop install already
has — nothing extra to install on KDE Plasma, GNOME, or Cinnamon. On a
GNOME desktop specifically, tray icons only show with the AppIndicator
extension enabled (Ubuntu ships this on by default; a from-scratch GNOME
install may need `gnome-shell-extension-appindicator`). If no graphical
session is detected at all (e.g. a true headless box), the tray is skipped
automatically — autoOr-simg still runs fine, just without it.

First run: `autoOr-simg run` creates ~/.autoortho (config) and walks you
through the setup web UI at http://127.0.0.1:5000.
