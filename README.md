<p align="center">
  <img src="docs/logo.png" alt="autoOr-simg logo" width="160">
</p>

<h1 align="center">autoOr-simg</h1>

<p align="center">
  An unofficial Go reimplementation of <a href="https://github.com/kubilus1/autoortho">kubilus1's AutoOrtho</a> —
  streams free satellite imagery into X-Plane as scenery, built on demand as you fly.
</p>

<p align="center">
  <a href="https://github.com/Keymagvar/autoOr-simg/actions/workflows/test.yml"><img src="https://github.com/Keymagvar/autoOr-simg/actions/workflows/test.yml/badge.svg" alt="Tests"></a>
  <a href="https://github.com/Keymagvar/autoOr-simg/actions/workflows/release.yml"><img src="https://github.com/Keymagvar/autoOr-simg/actions/workflows/release.yml/badge.svg" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>
  <img src="https://img.shields.io/badge/go-1.27%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.27+">
  <img src="https://img.shields.io/badge/platforms-Linux%20%7C%20Windows%20%7C%20macOS-lightgrey" alt="Platforms">
</p>

---

autoOr-simg mounts a virtual filesystem (FUSE) that X-Plane reads scenery
textures through, fetching and building each tile from free satellite
imagery the moment X-Plane asks for it — no multi-hundred-gigabyte
pre-rendered scenery pack to download up front. This is a from-scratch
Go rewrite of that idea, not a fork: a single binary per platform, a
browser-based control panel instead of a desktop GUI toolkit, and a few
features the original doesn't have.

## Contents

- [Features](#features)
- [Install](#install)
  - [Linux](#linux)
  - [Windows](#windows)
  - [macOS](#macos)
  - [Build from source](#build-from-source)
- [First run](#first-run)
- [Configuration](#configuration)
- [Status](#status)
- [Development](#development)
- [Credits & license](#credits--license)

## Features

- **Portable on every platform** — one file, no install step: a single
  `.exe` on Windows, a relocatable `.app` on macOS, an AppImage on Linux
- **Browser control panel** — one page at `http://127.0.0.1:5000`, no desktop
  toolkit dependency, works the same on every platform
- **10 languages** — English, Spanish, Russian, Portuguese, Greek, Arabic
  (full RTL), Chinese, French, Italian, German
- **Per-region scenery mount/unmount** — bring a region online or take it
  offline individually, without touching any other mounted region
- **Native system tray icon** on all three platforms (Open / Stop / Restart /
  Shutdown) — real Win32 notification-area / macOS menu-bar / Linux
  AppIndicator integration, not a shared lowest-common-denominator shim
- **Performance mode** (Quality / Balanced / Speed) — trade sharpness for
  load smoothness under heavy demand (dense cities, wide views), tunable
  live
- **Max-zoom cap** — put a ceiling on the sharpest imagery ever fetched to
  save disk space and bandwidth, independent of the per-provider floor
- **Memory watchdog** — staged response (soft eviction → warn → safe
  shutdown) to real process memory growth, so an overloaded session
  degrades gracefully instead of getting OOM-killed
- **Automatic `scenery_packs.ini` registration** — installing a region
  creates its `Custom Scenery` folder and adds the right line for you, no
  manual file editing
- **A desktop mascot named Natalia**, because why not

See [DESIGN.md](DESIGN.md) for the architecture and the reasoning behind
each of these.

## Install

Each platform's package is built by [`.github/workflows/release.yml`](.github/workflows/release.yml)
and attached to the [latest release](https://github.com/Keymagvar/autoOr-simg/releases/latest).

### Linux

Portable, no install step — download `autoOr-simg-x86_64.AppImage`, carries
its own icon:

```bash
chmod +x autoOr-simg-x86_64.AppImage
./autoOr-simg-x86_64.AppImage
```

(If your file manager double-click doesn't mount it — some current distros
only ship FUSE3, not the FUSE2 an AppImage container itself needs to
self-mount — run it with `./autoOr-simg-x86_64.AppImage --appimage-extract-and-run run` instead.)

Or the tarball, with an optional installer that registers a desktop
launcher:

```bash
tar xzf autoOr-simg-linux-amd64.tar.gz
cd autoOr-simg-linux-amd64
./install.sh
```

Either way, needs `fuse3` installed once for autoOr-simg's own scenery
mount — `sudo apt install fuse3` (Debian/Ubuntu), `sudo pacman -S fuse3`
(Arch), or your distro's equivalent.

### Windows

```powershell
# unzip autoOr-simg-windows-amd64.zip, then from inside that folder:
.\install.ps1
```

No admin rights required. Installs to `%LOCALAPPDATA%\Programs\autoOr-simg`
and creates both a Start Menu and a Desktop shortcut with the real app
icon. Needs [WinFsp](https://winfsp.dev/rel/) installed once — autoOr-simg's
scenery mounts won't work without it.

Or just run it directly without installing: `autoOr-simg.exe run`.

### macOS

Not currently published as a release download — build it yourself:

```bash
VERSION=local bash packaging/macos/build.sh
```

Drag the resulting `autoOr-simg.app` (under `dist/`) to `/Applications`
(or just double-click it from wherever it was built). Needs
[macFUSE](https://macfuse.github.io) installed once. This build isn't
notarized, so the first launch needs right-click → Open instead of a
plain double-click, or:

```bash
xattr -dr com.apple.quarantine autoOr-simg.app
```

### Build from source

Needs [Go](https://go.dev/) (see [`go.mod`](go.mod) for the minimum
version) and, on Linux, a GTK AppIndicator dev package for the tray icon
(`libayatana-appindicator3-dev`, or `-tags legacy_appindicator` for the
older `libappindicator3-dev`).

```bash
go build -o autoOr-simg ./cmd/autoortho
./autoOr-simg run
```

## First run

`autoOr-simg run` opens a browser tab at `http://127.0.0.1:5000` automatically
(pass `-H`/`--headless` to skip that). From there:

1. Point the **Setup** block at your X-Plane install folder and a dedicated
   scenery folder.
2. Download whichever regions you fly over in the **Scenery** panel.
3. Mount them with each region's own **Mount** toggle, or the global
   **Run** button for everything at once.
4. Fly.

## Configuration

Settings live in `~/.autoortho` (an INI file, auto-created on first run)
and are editable live from the web UI — nothing needs hand-editing for
normal use. See [DESIGN.md](DESIGN.md) for what each setting actually
does and why its default is what it is.

## Status

Functional and flyable, actively developed. Linux is the best-tested
platform, with CI running the real binary (FUSE mount included) on every
push. Windows cross-compiles and is also run for real in CI against an
actual WinFsp install. macOS is not currently covered by CI (see
`packaging/macos/build.sh` if you want to build it yourself); a real
scenery mount there hasn't been verified either way — macFUSE needs a
one-time interactive security approval CI can't automate. Not affiliated
with or endorsed by the original AutoOrtho project.

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

`.github/workflows/test.yml` runs the same checks on Linux and Windows on
every push, plus a real end-to-end run (web UI + FUSE mount, Linux via
the actual AppImage) on both.

## Credits & license

All credit for the original design goes to
[kubilus1's AutoOrtho](https://github.com/kubilus1/autoortho) — this is
an independent reimplementation, not a fork, and not affiliated with the
original project. 

This is a cookie-cutter readme for now. I will update it on future PRs.

Licensed under the [Apache License 2.0](LICENSE).
