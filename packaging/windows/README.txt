autoOr-simg (Go rewrite) — Windows distribution
================================================

This zip contains autoOr-simg.exe, README.txt, and install.ps1. No
runtime dependencies to unpack — you can run the exe directly from
wherever you extracted it:

    autoOr-simg.exe run

Optional: run install.ps1 (right-click -> Run with PowerShell, or
`powershell -ExecutionPolicy Bypass -File install.ps1`) to copy it into
%LOCALAPPDATA%\Programs\autoOr-simg and add a shortcut to both the Start
Menu and the Desktop — no admin rights needed for any of this. The exe has
its own icon baked in (not the generic Go default), so both the taskbar
and the shortcuts show the real autoOr-simg logo.

This build uses autoOr-simg's pure-Go image codecs rather than the
ISPC/libjpeg-turbo-accelerated ones Linux ships with — fully functional,
just not yet as fast on very large tiles. Bringing the accelerated path to
Windows is tracked separately (needs a Windows libjpeg-turbo build this
repo doesn't vendor yet).

One real system dependency this package does NOT and cannot bundle:
WinFsp (https://winfsp.dev), the Windows FUSE-compatible filesystem
driver autoOr-simg needs to mount scenery for X-Plane to read. Install it
once before running `autoOr-simg.exe run` — this is a one-time system
install, identical to what the original Python version of AutoOrtho
already requires (winsetup.py only ever detected an installed WinFsp, it
never installed one either).

First run: `autoOr-simg.exe run` creates a config directory under your
user profile and walks you through the setup web UI at
http://127.0.0.1:5000.

A real Windows system tray icon (Open autoOr-simg / Stop / Restart /
Shutdown) appears automatically unless you pass -H/--headless — the exact
same internal/tray code as the Linux/macOS builds, via a cross-platform
library that talks to the Win32 notification-area API directly (no GTK
dependency like Linux's AppIndicator needs, so there's nothing extra to
install for it specifically). CI now actually runs this build on a real
Windows VM on every push (not just Wine) and confirms the web UI and the
WinFsp mount both come up for real.
