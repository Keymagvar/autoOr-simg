# autoOr-simg — Design Decisions

A from-scratch Go reimplementation of [kubilus1's AutoOrtho](https://github.com/kubilus1/autoortho)
(the Python original). This file locks in the design decisions behind the
port so implementation can proceed consistently — it's the source of truth
for *why* the code is shaped the way it is. Update this file (not just a
commit message) whenever one of these decisions changes.

## 1. FUSE backend: `cgofuse` (winfsp/cgofuse)

**Known upstream issue, found under `-race` (not blocking, documented here so it isn't
re-discovered from scratch):** `cgofuse`'s own `FileSystemHost.fuse` field is written by
its internal `hostDestroy` (on the mount's serve-loop goroutine, as part of normal
teardown) and read by `FileSystemHost.Unmount()` (called from whatever goroutine
requests the unmount — our `Controller.Unmount()`) with no synchronization between
them. This reproduces as a genuine `-race` data race on **every single unmount**,
independent of load — confirmed identical under a single trivial read and under a
heavy 12-way-concurrent multi-tile stress test. It is entirely inside `cgofuse`'s own
unexported state, not reachable or fixable from caller code without forking the
library. In every mount/unmount cycle exercised so far (this session and prior), it
has not produced any observed incorrect behavior — unmount has always completed
correctly — but it is real, undefined-behavior-under-the-Go-memory-model, and worth
reporting upstream or revisiting if `-race` testing becomes part of CI. Not treated as
a blocker for now because the caller-side mitigation (adding our own lock around a
field we don't own) isn't actually possible.

**Decision:** use [`github.com/winfsp/cgofuse`](https://github.com/winfsp/cgofuse) as
the single cross-platform FUSE abstraction, implementing one `fuse.FileSystemInterface`
that's mounted identically on every desktop target.

| Platform | Backend | Runtime requirement |
|---|---|---|
| Windows | WinFsp | user installs WinFsp (same as today) |
| macOS | macFUSE (or FUSE-T for a non-kext install) | user installs macFUSE/FUSE-T |
| Linux | libfuse2 or libfuse3 | `libfuse-dev`/`libfuse3-dev` at build time, libfuse at runtime |
| FreeBSD/NetBSD/OpenBSD | fusefs-libs | not a current target, but cgofuse covers it if ever needed |

This directly replaces the manual WinFsp-vs-Dokan ctypes juggling in `winsetup.py` —
`cgofuse` already wraps exactly that duality (plus macOS and BSD) behind one Go API,
so `autoortho.py`'s `setupmount()` platform-specific branching collapses into mostly
one code path, with the Windows "mountpoint must not pre-exist" quirk still needing a
small platform-specific check before `Mount()`.

### Android: explicitly **not supported**, and not realistically portable

This needs to be said plainly rather than quietly scoped out: **unprivileged Android
apps cannot open `/dev/fuse` or mount a FUSE filesystem.** FUSE mounting on stock
Android is done only by system processes (MediaProvider's in-process `libfuse_jni`
for `/sdcard`), and third-party apps get no equivalent entrypoint — the kernel node
itself is permission-gated to system UIDs. The only way to get a real FUSE mount as a
regular app is root (Magisk) plus `su`, which isn't a baseline we can require.
`cgofuse` itself has no Android target at all — it's desktop/server-OS only.

Practically this is moot anyway: X-Plane (the whole reason this project exists) isn't
a thing you run on Android with custom scenery packs mounted into a `Custom Scenery`
folder the way desktop X-Plane is. There's no actual use case being given up here.

If "Android" later turns out to mean something narrower — e.g. a companion app that
just *browses/manages* scenery packs or shows the map/stats page, not one that mounts
a filesystem — that's a different, much smaller feature (a mobile-friendly view of the
same `:5000` web UI, no FUSE involved) and can be revisited on its own terms. Don't
build toward Android FUSE support; it isn't there to build toward.

## 2. Image decode + DXT compression: cgo first, behind swappable interfaces

**Concurrent-build cap added** (`Tile.buildSem`, `Cacher.buildSem`,
`config.AutoOrtho.MaxConcurrentBuilds`, default 8) — the actual fix for the §9 memory
finding, not just the watchdog's reaction to it. Nothing previously bounded how many
tiles could be simultaneously running the fetch/decode/composite/compress pipeline
below; `BuildMipmap` now waits for a semaphore slot (shared across every `Tile` one
`Cacher` creates) before doing any of that work, skipping the wait entirely when
`buildSem` is nil (direct `NewTile` construction outside a `Cacher`, which every
existing test in this package already relied on). **Proven two ways**: a real-HTTP
unit test (`TestBuildMipmap_ConcurrentBuildCap`) launching far more simultaneous builds
than the cap across distinct real `Tile` objects, asserting measured peak concurrency
never exceeds it; and a real `aus_pac` mount hammered with 129-150 concurrently-opened
real tiles (held open 8s each, the same pattern that caused the original 20GB spike) —
peak RSS stayed in the **1.5-2.5GB range**, roughly a 10x reduction under comparable
(if anything, higher-fanout) load.

**A second, independent real bug was found while verifying this**, not related to the
build cap itself: `Cacher.Open` incremented a newly created tile's `Refs` *after*
calling `evictLocked`, so when the cache was already full of genuinely still-open
tiles (nothing else evictable) — which is exactly what this same burst-load test
produces — `evictLocked`'s scan could reach the tile `Open` had just inserted,
still sitting at `Refs=0` at that exact moment, and evict it before `Open` ever got
the chance to protect it. The caller still received a valid `*Tile` (so the current
request worked), but the entry was silently gone from the map, so the *next* `Get`
(every FUSE `Read`) or `Close` (FUSE `Release`) for that id failed. Reproduced live —
157 "close of unknown tile" warnings across 129 opened tiles before the fix, zero
after — and covered by a deterministic regression test
(`TestCacher_OpenDoesNotEvictItself`) confirmed red on the old ordering, green on the
fix. Moving the `Refs++` to before `evictLocked` closes it.

**Decision:** start by reusing proven native code via cgo — the same families of
library already vendored in `autoortho/autoortho/aoimage/` and `autoortho/autoortho/lib/`
(libjpeg-turbo for JPEG decode, Intel ISPC `texcomp` for BC1/BC3 encode, with `stb_dxt`
as the slower fallback) — rather than reimplementing decode/compress in pure Go right
away. Reasoning: this is the hot path that gates texture pop-in latency, these libs are
already battle-tested at exactly this job, and matching today's performance is the
baseline the rewrite must not regress on. "Not chained by Python" means we're free to
pick *better* native code too (see below), not that cgo itself is off the table — Go's
FFI cost is much lower than ctypes', and this is squarely a case for it.

Wrap both responsibilities behind small interfaces so the backend is swappable later
without touching caller code:

```go
type ImageDecoder interface {
    Decode(jpegBytes []byte) (*Image, error) // Image = raw RGBA + dims
}

type TextureCompressor interface {
    CompressBC1(rgba []byte, w, h int) ([]byte, error)
    CompressBC3(rgba []byte, w, h int) ([]byte, error)
}
```

Initial implementations:
- `ImageDecoder`: cgo binding onto libjpeg-turbo (reuse the existing build, add a
  macOS `.dylib` target alongside the current linux `.so`/windows `.dll` — that's
  currently missing and blocks a from-scratch macOS build). Go's stdlib `image/jpeg`
  is 3–9x slower in published benchmarks against libjpeg-turbo bindings; keep it only
  as a `CGO_ENABLED=0` fallback build tag, not the default.
- `TextureCompressor`: cgo binding onto Intel ISPC `texcomp` (`CompressBlocksBC1`/
  `CompressBlocksBC3`), i.e. a straight port of what `pydds.py` already calls. This is
  the safest first step since it's a drop-in behavioral match with the Python version.

**Worth prototyping once the ISPC port is correct and benchmarked as a baseline:**
[Goofy](https://github.com/SergeyMakeev/Goofy) (SergeyMakeev), a header-only C++
real-time BC1/ETC1 encoder claiming ~1429 MP/s vs. ~24–43 MP/s for `icbc`/`ryg`-style
encoders — a large potential win over even ISPC if it holds up on real tile data and
at the quality level X-Plane's 3D rendering actually needs. It's C++, so still a cgo
wrapper (a thin `extern "C"` shim), not a pure-Go rewrite — don't let "faster" get
confused with "no native dependency." Benchmark it against real chunks from
`autoortho/autoortho/testfiles/` before swapping it in; don't take the upstream
micro-benchmark numbers as given for this workload.

### Benchmarked (this workload, not upstream numbers): decision confirmed

Built `internal/native/` (cgo-gated via `//go:build cgo` where applicable) with four
real implementations — `DecodeJPEGGo` (stdlib), `DecodeJPEGTurbo` (cgo onto the system
`libturbojpeg`), `CompressBC1Go` (a straightforward pure-Go range-fit BC1 encoder),
`CompressBC1ISPC` (cgo onto the actual vendored `libispc_texcomp.so` from
`autoortho/autoortho/lib/linux/`, same ABI `pydds.py` calls) — and benchmarked all four
against the real fixtures in `autoortho/autoortho/testfiles/`: `test_tile_small.jpg`
(a genuine 256×256 provider chunk) and `test_tile2.jpg`/`test_tile.png` (genuine
4096×4096 full tiles — `Tile.width*256 = 16*256 = 4096` in `getortho.py`, so this is the
actual per-tile compression unit, not an arbitrary size). Results (13th Gen i5-13600KF,
`go test -bench=. -benchmem`):

| Benchmark | Go (pure) | cgo (turbo/ISPC) | Speedup |
|---|---|---|---|
| JPEG decode, 256×256 chunk | 393.6 µs/op | 157.6 µs/op | **2.50×** |
| JPEG decode, 4096×4096 | 88.8 ms/op | 31.5 ms/op | **2.82×** |
| BC1 compress, 256×256 | 582.8 µs/op | 141.8 µs/op | **4.11×** |
| BC1 compress, 4096×4096 (full tile) | 132.6 ms/op | 37.0 ms/op | **3.59×** |

Composited, for the actual real workload of fully building one mipmap-0 tile (decode
256 chunks + one full 4096² BC1 compress, ignoring network fetch time): **~233 ms
pure-Go vs. ~77 ms cgo — about 3× faster end to end.** That's a measured result on this
repo's own data, not an extrapolation from someone else's benchmark, and it confirms
the cgo-first decision above rather than just asserting it.

One caveat worth being honest about: Go's stdlib `jpeg.Decode` returns `*image.YCbCr`
for a baseline JPEG, not RGBA — the benchmark above times the decode call as-is, but the
real pipeline needs RGBA for compositing (`AoImage.paste`), so a correct pure-Go path
would need an additional YCbCr→RGBA conversion pass not captured in that number. The
true pure-Go gap is very likely larger than 2.5–2.8× in practice, not smaller. The BC1
pure-Go encoder here is also a simple, uncomplicated range-fit implementation — a
better-optimized pure-Go encoder could close some of that specific gap, but matching
hand-tuned ISPC SPMD code without significant disproportionate effort is unlikely.

The benchmark harness (`internal/native/bench_test.go`) stays in the tree — re-run it
(`go test ./internal/native/... -bench=. -benchmem`) on real target hardware before
shipping, and it's the natural place to drop in a Goofy comparison later.

Pure-Go decode/compress (stdlib `image/jpeg` + a Go BC1/BC3 encoder) stays a documented
option behind a build tag for anyone who wants a zero-cgo build, but is *not* the
default — do not spend effort tuning it unless profiling on real usage says the cgo
path is actually the bottleneck somewhere pure Go would help.

### 2a. Critical correctness detail: the full mipmap chain must always be valid (likely cause of "tile vanishes when zoomed out" in a from-scratch attempt)

This is a different failure mode from §7's "deadline missed, serves garbage" — this one
is about **structural validity of the DDS mipmap chain itself**, independent of network
timing, and it's the kind of bug that makes a texture disappear entirely rather than
look degraded, which matches "fully loaded up close, then gone when zoomed out/farther
away" much better than a fetch-timing issue would.

**The trap:** DXT/BC block compression operates on 4×4 pixel blocks — it has no
representation for a 2×2 or 1×1 image. But DDS mipmap chains conventionally go all the
way down to 1×1, and X-Plane's loader expects to find that full chain, not one that
stops early. A naive mipmap generator that just keeps halving width/height and tries to
DXT-compress each level will hit a wall below 4×4 and either produce zero/garbage data
or a chain that's shorter than the header declares. GPU drivers and engines are often
strict about this: if a requested mip level has no valid data, the whole texture can get
treated as invalid rather than falling back gracefully to the next mip up — which looks
exactly like "the tile disappeared" once the camera is far enough that the renderer
actually requests one of those small, broken levels. Close up, only the big early
mipmaps get sampled, so everything looks fine; zoom out (or just move farther away) and
suddenly the levels that were never validly populated get requested, and the whole
texture drops out.

**How the existing code avoids this** (`pydds.py`): it computes `smallest_mm = len(mipmap_list) - 3`
— the last mipmap level that's still a real, DXT-compressible 4×4 block — and in
`gen_mipmaps`, once compression reaches that level, it explicitly **replicates that
same compressed buffer into every remaining, smaller, theoretical mipmap slot** down to
the 1×1 X-Plane expects, marking them all `retrieved = True`. So the DDS file X-Plane
reads always has a fully populated, structurally valid mipmap chain end to end — the
smallest few levels are redundant (same bytes repeated), not generated, but they are
never empty or malformed. That's the whole trick, and it's a few lines of code doing
something very easy to omit by accident.

**Decision:** the Go port's `TextureCompressor`/DDS-writer must reproduce this exact
behavior deliberately, not rediscover it the hard way — compute the real
DXT-compressible floor explicitly, generate real data down to that level, then replicate
that smallest real level's compressed bytes into every remaining declared mipmap slot
down to 1×1. Add an explicit **test**: construct a DDS for a known tile size, assert
every mipmap level in `mipmap_list` has a non-nil, correctly-sized buffer before writing
a single file — a structural invariant check that would have caught this class of bug
immediately in a from-scratch attempt, instead of surfacing as "distance-dependent
disappearing tiles" with no obvious cause.

## 3. Config/setup UI: fully web-based at `http://localhost:5000`, server-authoritative

**Decision:** `config_ui.py` (the PySimpleGUI desktop window) is retired entirely. Its
responsibilities move into the same Go `net/http` server that will also absorb
`flighttrack.py`'s existing map/stats/metrics routes — as **one single page**, laid out
in visually distinct blocks (not tabs), not a multi-page app:

- **Setup block**: scenery/X-Plane/cache/download paths, maptype override, cache size +
  a "Clean Cache" action — same fields as today's Setup tab.
- **Zoom & quality block** (new — see below): exposes `autoortho.min_zoom`,
  `autoortho.maxwait`, and the provider list (`autoortho.maptypes`), which today only
  exist in the ini file and were never in the GUI at all.
- **Scenery block**: list of regions from the downloader, install/update buttons, live
  download progress.
- **Run block**: a single Run/Stop control reflecting actual mount state (see state
  model below), not a client-side toggle.
- **Log block**: a live-scrolling log pane, in the same page, not a separate view.
- **Map/stats blocks**: carried over conceptually from `flighttrack.py`.

### Config fields newly exposed: zoom level and friends

`autoortho.min_zoom` (default 12) is real today but buried in the ini file — add it to
the Zoom & quality block as a slider/number input, with help text matching its actual
semantics: it's a *floor* on how far AutoOrtho will degrade a tile under load, **not** a
ceiling that raises max quality (that's capped by what the imagery provider serves at
that location/zoom). Expose it alongside `maxwait` (the per-chunk fetch deadline that
trades stutter for quality) and the provider list, since all three are different levers
on the same quality-vs-performance tradeoff and make more sense presented together than
scattered.

### State model: the server is the single source of truth

This is the piece that makes the UI reload-proof and multi-tab-safe. The failure mode
being designed against: a client-side-only "Run" → "Running" label flip, which desyncs
the instant you reload the page, open a second tab, or the mount dies in the background.

- One in-process `AppState` struct on the server (mount status per scenery root,
  per-region download progress, a bounded ring buffer of recent log lines, live stats
  counters, the current config) guarded by a mutex, with a pub/sub broadcaster that
  fans out state changes to every connected client.
- **One WebSocket connection per browser tab**, opened on page load. First message from
  the server on connect is always a full state snapshot — so a reload (or a second tab)
  renders the real current state immediately, never a default/blank one. After that,
  only incremental events stream down (`mount_state_changed`, `log_line`,
  `download_progress`, `stats_tick`).
- **Every control sends a command, not a local state change.** Clicking Run sends
  `{action: "mount"}` over the socket (or a POST, mirrored into the same broadcast path);
  the server performs the actual mount, updates `AppState`, and *then* broadcasts the new
  state to all connected tabs, including the one that clicked. The button's rendered
  label/disabled-state is purely a function of the last state broadcast received — it
  never flips itself in the click handler. This is what guarantees two tabs (or a tab
  plus a reload mid-action) never disagree with the server or with each other.
- **Logs**: the ring buffer in `AppState` is what gets sent as backfill on connect; new
  lines appended by the logger are pushed as `log_line` events over the same socket. No
  separate polling/tailing mechanism needed once this exists.
- REST endpoints (`GET /api/state`, `POST /api/config`, etc.) can still exist for
  curl/scripting convenience, but the live page itself should run entirely over the one
  WebSocket so there's a single code path to keep correct, not two (REST polling +
  WebSocket) that can drift from each other.

**On startup, instead of opening any window:** fire one native OS notification via a
cross-platform toast library (e.g. `github.com/gen2brain/beeep`, which covers Windows
toast, macOS Notification Center, and Linux D-Bus/`notify-send`) reading something
like *"AutoOrtho is running — configure it at http://localhost:5000"*. Pair it with
opening the user's default browser to that URL automatically (a small `xdg-open`/
`open`/`rundll32 url.dll,FileProtocolHandler`-style helper — several tiny Go packages
do exactly this, e.g. `pkg/browser`), gated by a config flag (`general.auto_open_ui`,
default true) so it's skippable for headless/server-style runs (`-H`/`--headless`
already exists as a concept in `autoortho.py` and should gain exactly this meaning: no
toast, no browser launch, server still comes up on `:5000` either way since the FUSE
engine and X-Plane-facing pieces always need it reachable for diagnostics).

No persistent native GUI process, no window to keep alive — the toast is fire-and-forget,
the browser tab is just a normal tab the user can close, and the actual program keeps
running as a background process (console app on Linux/macOS, maybe a tray icon later if
wanted, but that's a nice-to-have, not a requirement here).

## 4. Error-proofing: single instance, stale-mount cleanup, guaranteed unmount on exit

**Implemented** in `internal/lifecycle` (`lock.go`, `stalemount.go`, `manager.go`),
driven by `autoortho run` in `cmd/autoortho/main.go`. Proven for real, not just
unit-tested: ran against the actual installed `aus_pac` mount, `kill -9`'d the process
to leave a genuinely stale FUSE mount + dead-PID lock file, restarted, and watched it
auto-detect and force-clear the stale mount before remounting. One deliberate scope
cut from the design below: the native OS toast notification (`beeep`) on
"already running" is not wired up — it logs/exits instead, since no code anywhere
yet triggers a GUI context to notify through (that
lands together with DESIGN.md §3/§9 when the web UI and memory watchdog give toast
notifications an actual reason to exist, instead of being the first and only caller of
a new dependency for one message).

This directly addresses "I want it error proof and cleaning after shutdown, so we don't
have several fuse mounts of the same program." Three mechanisms, all needed together:

**a. Single-instance lock.** On startup, acquire an exclusive OS-level file lock (e.g.
`github.com/gofrs/flock`, which covers `flock(2)` on Unix and `LockFileEx` on Windows
correctly) on a lock file under `~/.autoortho-data/autoortho.lock`, storing the current
PID inside it. If the lock is already held by a live process, refuse to start a second
instance: log it, fire the same toast notification saying AutoOrtho is already running
at `:5000`, and exit non-zero rather than racing another instance for the same mounts.

**b. Stale-mount detection and forced cleanup on startup.** Before mounting anything,
for every configured scenery mount point: check whether something is already mounted
there (platform-appropriate check — e.g. parsing `/proc/mounts` on Linux, or just
probing whether a sentinel file read behaves like a live AutoOrtho mount vs. a plain
directory) and if so, force an unmount first (`cgofuse`'s unmount call, or a direct
`fusermount -u`/`umount` shell-out as a fallback for a truly wedged mount) before
re-mounting fresh. This is what makes a crash-and-restart safe instead of accumulating
dangling mounts — today's Python code has no equivalent of this and relies on the
`.poison`-file-polling unmount always having run cleanly, which it may not have after a
crash.

**c. Guaranteed unmount + cleanup on every exit path.** One shutdown path triggered by:
OS signal (SIGINT/SIGTERM on Unix, console control handler on Windows), the web UI's
own "Quit" action, or a FUSE error bubbling up. That path must, in order: cancel a
root `context.Context` that every long-running goroutine (HTTP server, each mount's
serve loop, the chunk-fetch workers) selects on; call `Unmount()` on every active FUSE
host with a bounded timeout; restore the placeholder directory structure (mirroring
`setupmount()`'s cleanup today); release the single-instance lock *last*, only once
every mount is confirmed down — so a half-finished shutdown still shows up as "still
running" to the stale-mount check on the next launch, rather than lying about having
cleaned up. Wrap `main()`'s body so a panic still runs this same shutdown sequence
(`recover()` + best-effort unmount) instead of leaving a mount dangling because of an
unhandled error — this is the Go-side answer to `.poison`-file polling, and should be
both more reliable and much simpler to reason about than that mechanism.

## 5. Imagery providers: pluggable registry, politeness tuning per provider

**Context:** every provider in `getortho.py`'s `MAPTYPES` dict (Bing/`BI`, Google/`GO2`,
EOX/`EOX`, Esri/`ARC`, NAIP, USGS, Firefly) is a public "slippy map" tile endpoint — the
same backend the provider's own consumer-facing web map viewer hits, not a keyed
developer API. No key is needed because the key-gated products are a separate thing
from the raw tile CDN. Any client sending requests that look like normal map-viewer
traffic can pull tiles the same way. This is how every tool in this space (AutoOrtho
included, and by all indication tools like X-Plane Map Enhancement) gets imagery
without a subscription — and it's also the whole source of ban/rate-limit risk, since
none of Bing/Google/Esri/NAIP/USGS license this kind of third-party bulk use. EOX
(Sentinel-2 cloudless, Copernicus/ESA data) is the one provider in the list that's
actually openly licensed for this; the rest carry a standing, un-fixable risk of being
rate-limited or IP-blocked no matter how polite the traffic looks.

**Decision — provider registry.** Define a small interface so adding, removing, or
reconfiguring a provider is a config/one-function change, not a core-code change:

```go
type Provider struct {
    Name       string
    TileURL    func(col, row, zoom int) string
    Headers    map[string]string // e.g. Referer for EOX
    UserAgent  string            // per-provider override; see below
    MaxThreads int               // per-provider concurrency cap; see below
}
```

A registry (`map[string]Provider`) replaces the hardcoded `MAPTYPES` dict; `maptypes`
and `maptype_override` in config select from it by name. Adding a keyed provider
(Mapbox, MapTiler, a self-hosted tile server) later is the same shape, just with an
`APIKey` field folded into the URL template — worth supporting as an opt-in "no ban
risk" alternative even if not the default.

**Decision — stop announcing ourselves as a scraper.** The current code sends
`User-Agent: curl/7.68.0`, which is a tell, not camouflage — it signals "this is a
script" rather than "this is a normal map client." Default to a realistic browser-like
UA (matching what the provider's own web map viewer actually sends) unless a specific
provider is known to behave better with something else. Make it per-provider and
configurable, not hardcoded once globally.

**Decision — treat concurrency as a politeness knob, not just a performance knob.**
`fetch_threads=32` as one global number means sustained, heavy, round-the-clock-during-
a-flight parallelism against a single provider's CDN from one IP — closer to a scrape
pattern than normal browser bursts. Give each `Provider` its own `MaxThreads` (and
reuse the existing subdomain-rotation trick — `a/b/c/d`, `t0-3`, `khms0-3` — which
already helps spread load the way a real browser's parallel tile fetches do), so this
can be tuned per provider instead of one setting straining the most rate-limit-sensitive
one.

**Carry forward as-is** (already reasonable in the Python version, keep the same
behavior): aggressive disk caching so a tile is never re-fetched once cached — this is
the single biggest thing keeping request volume down — retry-with-backoff, and
error-rate monitoring that warns past a high failure rate (useful signal that a
provider may be throttling).

**Not solved by any of this:** politeness reduces risk, it doesn't eliminate it. Being
rate-limited or blocked on the non-EOX providers is a standing possibility independent
of how well-behaved the client is, since this traffic was never officially sanctioned
for third-party bulk use. Users who want a zero-risk option should be pointed at EOX or
a keyed provider; that's a usage choice, not something the Go rewrite can code its way
out of.

## 6. Logging and cgo memory-safety instrumentation

Two distinct concerns, both real for this codebase specifically, and easy to
under-invest in because Go doesn't hand you either one by default the way some other
ecosystems do: Go's stdlib has no de-facto structured/leveled logger the way a JVM
stack has slf4j/logback as an unquestioned default, and `cgo` gives you none of Go's
memory safety for anything on the C side of the boundary — a leak or a use-after-free
in the native JPEG/DXT code is a silent, gradual process-memory-growth bug, exactly the
kind that's painful to diagnose after the fact. Build the instrumentation in from the
start rather than bolting it on once something looks wrong in the field.

### Logging

- **Library: stdlib `log/slog`** (structured, leveled, zero extra dependency). Every
  log call carries structured attributes, not just an interpolated string — e.g.
  `slog.Info("chunk fetched", "provider", p.Name, "col", c, "row", r, "zoom", z, "bytes", n)`
  — so logs are filterable/greppable by field, and the same structured record can feed
  the on-disk log file, the in-memory ring buffer behind the web UI's Log block (§3),
  and `stdout` simultaneously via `slog`'s multi-handler composition, instead of three
  separate ad hoc logging paths.
- **Per-component tagging**, mirroring the Python version's one-`log = logging.getLogger(__name__)`-per-module
  pattern: every subsystem (`fuse`, `tile`, `chunk`, `provider`, `downloader`, `cgo`,
  `websocket`) logs through a `slog.Logger` built with `.With("component", "...")`, so
  the log block can filter by subsystem and a flood from one area (e.g. chunk retries
  during a network hiccup) doesn't bury unrelated errors.
- **Levels** map onto the existing `general.debug` config knob plus the usual
  debug/info/warn/error — default to info in normal operation, debug opt-in from the
  web UI (a live level change, not just a startup flag, since this is exactly the kind
  of thing you want to crank up *while* chasing a live issue without restarting a mount).
- **Every cgo call boundary gets a log line**, not just failures: debug-level
  enter/exit around each native call (decode, compress, free) including size/pointer
  info, error-level on any non-zero/failure return from the C side with full context
  (which tile/chunk/provider/offset triggered it). This is deliberately heavier than
  typical Go logging hygiene — the payoff is being able to correlate "memory climbing"
  with "which code path" after the fact from the log file alone.

### cgo / native memory-safety instrumentation

The C/C++ layer (libjpeg-turbo decode, ISPC/stb_dxt compress, and whatever the Goofy
prototype becomes) is the one place in this codebase where Go's own memory safety
doesn't apply — a forgotten `free()`, a double-free, or a buffer-size mismatch across
the cgo boundary is a real crash or leak, not a caught Go error, and it's exactly the
class of bug this project's own C code (`aoimage.c`, the DXT libs) already has to be
careful about today.

- **Every native allocation is owned by exactly one Go type with an explicit `Close()`/
  `Free()`**, called via `defer` at the allocation site — never rely on a Go
  finalizer as the primary release mechanism (finalizers run on GC's schedule, not
  deterministically, which is too loose for buffers this large and this frequently
  allocated). Finalizers are still worth attaching as a **last-resort safety net** that
  logs an error-level "finalized without explicit Close — leak in caller" message if it
  ever fires, since that's a signal a real bug exists upstream, not a mechanism to lean on.
- **Global outstanding-allocation counters** (`atomic.Int64` per allocation kind:
  decoded-image buffers, compressed-block buffers, etc.), incremented on alloc,
  decremented on free, exposed as regular stats fields in the same `AppState`/stats
  pipeline already planned for the web UI (§3). A leak then shows up as a number that
  only ever climbs on the stats block/live page, visible during normal use — not
  something you need a profiler attached to notice.
- **Defensive validation before every call into C**: check buffer length against the
  width/height/format you're about to tell the C side to assume *before* handing over a
  pointer, in Go, where a mismatch is a normal recoverable error — because once that
  pointer is on the C side, a size mismatch is a buffer overrun, not a Go panic you can
  recover from. A true segfault in the native code still takes the whole process down;
  `recover()` cannot and will not catch that, so prevention on the Go side of the
  boundary is the only real mitigation, not error handling after the fact.
- **Tooling, not just discipline**: run the cgo test suite under `-race` routinely;
  for the native side specifically, build a debug variant with AddressSanitizer/
  LeakSanitizer enabled via cgo `CFLAGS`/`LDFLAGS` (`-fsanitize=address`) for local/CI
  runs against the decode/compress paths — this is the actual tool for catching native
  leaks and overruns before they ship, not something Go's own tooling substitutes for.
  Valgrind as an occasional deeper Linux-only check is a reasonable supplement, not a
  replacement for ASan-in-CI.
- **Fuzz the decode path** specifically: chunk JPEG bytes come from the network and
  will occasionally be truncated, corrupted, or simply wrong (today's Python code
  already special-cases "fetched bytes don't start with the JPEG magic"), so the native
  decoder needs to be fed malformed/truncated input under `go test -fuzz` (driving the
  cgo call, not just the Go-side wrapper) as a standing part of the test suite, not a
  one-time check.

## 7. Self-healing: bound failure severity, don't pretend deadlines never get missed

**Context.** This is a known, reproducible failure class, not a one-off bug: textures
occasionally render as corrupted/"missing texture"-looking blocks, especially noticeable
at specific camera distances. Root-caused in the current Python code: when a requested
mipmap hasn't actually been compressed yet (fetch missed `maxwait`, a chunk never
arrived, a cache-eviction race), `pydds.py`'s `DDS.read()` fabricates literal `0x88`
filler bytes and serves them to X-Plane as if they were real DXT-compressed pixel data
(`getortho.py`/`pydds.py`, the "No buffer for {mipmap.idx}!" path). The GPU then decodes
that noise as a texture block. Distant terrain samples from the *highest* mipmap
numbers, which is exactly the tier most exposed to this — `Tile.max_mipmap=4` makes
those levels dependent on a single upstream compress finishing, and any miss there
serves garbage instead of something safe. A milder version: a freshly composited tile
starts filled with an arbitrary solid color (`(66,77,55)`) wherever a chunk hasn't
landed yet, served as final if time runs out.

**Why this is architectural, not a language problem.** This is the generic failure mode
of an eventually-consistent, best-effort async cache being read by a synchronous
consumer with a hard per-frame deadline — X-Plane reads `offset, length` and expects an
answer now, it doesn't wait. Porting this logic to Go verbatim carries the exact same
hole forward; the fix has to be a deliberate invariant, not a side effect of using a
different language. The actually-fixable problem isn't "a fetch sometimes misses its
deadline" (inherent to streaming imagery over a network with a real-time consumer) — 
it's "and therefore the user sees corruption," which is a separate, solvable problem.

**Decisions:**

1. **Never serve literal filler noise as if it were real compressed data.** Replace the
   `0x88`-fill-on-missing-buffer path with a precomputed, valid, solid-color DXT1/DXT5
   block (computed once at process startup — trivial, fixed cost) as the guaranteed
   fallback whenever a mipmap region isn't ready. Worst case becomes a flat,
   slightly-wrong-colored patch (reads as "briefly lower quality") — never a
   corrupted-looking block.
2. **A background repair queue, not just a one-shot deadline.** Today a chunk fetch
   failure gets resubmitted, but there's no mipmap-level guarantee that "this was served
   as a fallback" leads to the data actually getting finished. Any time a fallback is
   served for a given tile/mipmap, enqueue it for background completion unconditionally
   (not only if something reads it again), so real data exists well before the camera
   is actually at the distance where that mipmap matters for more than an instant.
   **Done in the Go port** (`internal/tile/tile.go`'s `scheduleRepair`/`recordRepairOutcome`):
   every degraded build (any `fetchComposite` that missed `MaxWait`) schedules a
   background rebuild after a fixed `repairDelay`, keyed by the actual fetch level
   (`fetchIdx`/mipmap 0), bounded by `maxRepairAttempts` so a permanently-broken chunk
   doesn't retry forever. Proven end to end with a real (if local) HTTP round trip: a
   slow chunk's fetch keeps running in the background (never cancelled — see
   `fetchComposite`'s doc comment), lands in the disk cache, and the scheduled repair
   picks it up from there and rebuilds cleanly, confirmed via a byte-for-byte diff of
   the mipmap's compressed data before/after and a request-count assertion that the
   repair didn't re-hit the network. This also surfaced and fixed a real, previously
   latent data race: once a mipmap's compressed bytes could be rewritten well after
   being first marked `Retrieved`, `DDS.Read` (the FUSE read path) needed its own lock
   (`DDS.dataMu`) around `Mipmap.Data`/`Retrieved`, since it runs with no synchronization
   against a concurrent `SetMipmapData` call from a repair goroutine — caught by `-race`
   while writing this feature's tests, not by inspection.
3. **Treat every fallback-serve as a first-class, observable event.** Structured log
   line (tile id, mipmap, offset — ties into §6's heavy logging) plus a running counter
   (`fallback_serves_total`) exposed through the same stats pipeline feeding the web
   UI's stats block. Today this is invisible — a user sees a weird texture with zero
   explanation. Make it countable and correlatable with network conditions instead of
   "mysterious."
4. **Eviction must be aware of in-flight repair state.** `TileCacher.clean()`'s eviction
   today only checks `refs <= 0`, with no notion of "a repair for this tile is still
   pending." Evicting mid-repair throws away the in-flight work and forces a cold
   re-fetch on next open. Add an explicit "don't evict while a repair is outstanding"
   rule to the Go port's cache. **Done**: `Tile.HasPendingRepair()` backs a check in
   both `Cacher.evictLocked` and `Cacher.ForceEvictIdle` (`internal/tile/cache.go`) —
   a tile with a scheduled-or-running repair is treated as not safe to drop even when
   `Refs<=0`.
5. **An active mount-level watchdog**, independent of the tile-serving logic — some of
   "weird FUSE mounting issues" may be the mount itself silently going stale rather than
   a texture problem at all. On top of §4's clean mount/unmount lifecycle, periodically
   stat a sentinel path *through* the live mount (not just trust that `Mount()` returning
   success means it's still healthy minutes later); on failure, log loudly and attempt a
   controlled remount instead of silently continuing to serve from a dead mount.

**What this does not promise:** a missed fetch deadline under real network conditions is
not something any of this eliminates — it's inherent to the approach. The goal is that a
miss degrades to "momentarily lower quality, logged and self-correcting," never to
"visibly broken with no trail to explain why."

## 8. Lifecycle controls: Stop vs. Shutdown are different actions

The web UI needs two distinct controls, not one, because they already differ in the
Python version's `Run`/`Quit` split:

- **Stop**: unmount every active FUSE mount (the §4(c) guaranteed-unmount path), but
  keep the HTTP server and process alive — lets the user change config (paths, maptype,
  zoom/quality settings) and remount without restarting the whole program.
- **Shutdown**: Stop, then also tear down the HTTP server, release every outstanding
  native (cgo) buffer, release the single-instance lock, and exit the process. This is
  the actual answer to "a safe shutdown button that unmounts fuse, shuts down the
  server, and frees the memory" — all three of those are explicitly sequenced steps in
  this path, not implied side effects of just killing the process.

Both reuse the same underlying shutdown sequence from §4(c); Shutdown is that sequence
plus process exit. Both are disruptive if a mount is actively in use (X-Plane could be
mid-flight), so both require an explicit confirm step in the UI — no single-click
Shutdown.

On Shutdown specifically, log the final value of every §6 outstanding-native-allocation
counter before exiting. They should all read zero. If one doesn't, that's a leak that
happened during this run — log it loudly as an error, since this is the one moment
(every mount confirmed closed) where that number is actually meaningful, far easier to
catch here than as an unexplained "memory was high last time."

## 9. Memory watchdog: staged response, independent of the web UI being open

**Implemented** in `internal/memwatch` (`watchdog.go`), wired into `autoortho run` in
`cmd/autoortho/main.go`. Not implemented ahead of need — built directly in response to
a real, reproduced finding: the first live test flight over the installed `aus_pac`
region drove process RSS to **~20GB within about ten minutes** of normal X-Plane
scenery loading at one airport (62.6% of a 31GB machine's memory, with under 500MB free
at the worst point). Root cause confirmed: `internal/tile`'s `Cacher` only evicts tiles
with no outstanding refs, and X-Plane was legitimately holding many tiles open
simultaneously while rendering the area around the airport — so the existing 25-tile
cap never engaged at all, because nothing was ever idle enough to evict. The fix was
`MaxOpenTiles`, a hard cap on concurrently-*open* tiles (distinct from the idle-only
25-tile cache cap and the concurrent-*build* cap in §2) — see `internal/tile/cache.go`'s
`overBudget`/`evictLRUContentLocked` and `config.AutoOrtho.MaxOpenTiles`'s own doc comment.

Thresholds are `config.Memory`'s `SoftPercent`/`HardPercent`/`CriticalPercent` (defaults
0.50/0.70/0.85) — a fraction of total system memory read once at startup via
`memwatch.TotalSystemMemoryBytes()` (`/proc/meminfo`), rather than an absolute number,
so one default works across an 8GB laptop and a 64GB desktop. Soft calls
`Cacher.ForceEvictIdle()` (new: an on-demand sweep of every idle tile, independent of
the lazy trim-to-cap `evictLocked` only runs opportunistically on the next `Open`).
Critical sends on a buffered channel that `runCmd`'s main `select` (alongside
`ctx.Done()`) reacts to by running the exact same `lifecycle.Manager.Shutdown()`
sequence a SIGINT would — one guaranteed shutdown path, not two.

**Honest limitation, not glossed over**: Soft-stage eviction is a real but *partial*
mitigation. It can only evict tiles with `Refs<=0` — in the exact scenario that caused
the 20GB spike (X-Plane holding everything open at once), `ForceEvictIdle` legitimately
evicts nothing, because nothing is idle. This was proven directly:
`TestCacher_ForceEvictIdle_NothingEvictableWhenAllOpen` in `internal/tile/cache_test.go`
asserts exactly zero evictions in that state, rather than silently assuming Soft always
helps. **Critical is the actual safety net for that case** — turning "uncontrolled OOM,
possibly taking down something else on the machine" into "this process shuts down
cleanly on its own terms, logged and voluntary." The deeper architectural fix — bounding
how many tiles can be *built* concurrently in the first place (§2's `MaxConcurrentBuilds`),
and separately bounding how many can stay cumulatively *open* (`MaxOpenTiles`, above) —
is what actually solves this; the watchdog alone is a last-resort safety net, not the
primary fix.

**Proven end-to-end, not just unit-tested**: with `config.Memory`'s percentages
temporarily set to near-zero via the real `~/.autoortho` file, a real `autoortho run`
crossed Critical within one 5-second tick of starting and ran the full real shutdown
sequence (unmount, filesystem destroy, lock release, clean process exit code 0) —
confirmed via the actual log output and `mount`/`ps` afterward, not inferred.

No native OS toast (`beeep`) on Hard — same documented scope-cut as §4(a)'s
"already running" case: no GUI context exists yet to make a toast meaningful, so this
logs at `ERROR` level instead; both land together once §3's web UI exists.

---

"Too much memory, shutting down before X-Plane crashes" needs to work even with no
browser tab open — so it cannot be a web UI banner; it's the same native OS toast
(`beeep`, already planned for the startup notification in §3) plus an automatic action,
not just a warning. Staged thresholds, not one trigger:

- **Soft** (process RSS approaching the configured cache limit): trigger aggressive
  tile-cache eviction immediately, silently — no user-visible action yet, this is normal
  operation under load.
- **Hard** (RSS well past that, regardless of which subsystem is responsible): fire the
  native toast warning the user memory usage is high.
- **Critical** (close to a level that risks the OS OOM-killer taking out X-Plane or the
  whole system): automatically run the exact same safe-shutdown path from §8, not just
  warn — the whole point is avoiding an uncontrolled crash, not just notifying about one
  on the way.

Important: this watchdog samples **total process RSS at the top level**, independent of
any one subsystem's own bookkeeping. `TileCacher.clean()`'s Python equivalent only
watches memory to decide whether to evict *its own* cached tiles — it has no concept of
"the whole process is dangerously large for some other reason" (a cgo leak, a download
buffer, anything outside the tile cache). The watchdog's job is to catch that regardless
of cause; §6's per-allocation-kind counters are what help pinpoint *which* subsystem
once the watchdog has already flagged that something's wrong.

## 10. Network exposure: bind to loopback by default

The Python version binds Flask to `0.0.0.0:5000` — reachable by anything on the same
network, no auth. That was low-risk for a read-only map/stats page; it stops being
low-risk the moment the web UI can edit config, install scenery, and trigger Shutdown
(§8) — any device on the same Wi-Fi could now do all of that with nothing to stop it.

**Decision:** bind `127.0.0.1` only by default. Add an opt-in config toggle for LAN
visibility for the one legitimate use case this gives up — checking the moving map/stats
page from a tablet while flying — but keep it **read-only on LAN regardless of the
toggle**: config edits, scenery install/uninstall, and Stop/Shutdown stay restricted to
loopback origin unconditionally, never exposed over the LAN toggle.

## 11. Disk-space preflight check before scenery downloads

Scenery zips can run tens of GB; today's downloader (`downloader.py`) has no free-space
check and just fails partway through a large multi-part download. Add a preflight
check against the download directory's free space before starting an install, and
surface a clear warning in the web UI (not a failure 40% through a multi-GB transfer)
if there isn't enough room. Same category of problem as §9's memory watchdog —
check before committing to an operation that's expensive to fail out of midway.

## 12. Post-v1 enhancement: predictive prefetch using live flight data

**Not required for a working first version** — the tile engine works purely reactively
(fetch starts when X-Plane's `read()` asks) in both the Python version and the planned
Go port, and that's a legitimate, working baseline. This is a genuine improvement worth
making once the core engine exists, not a gap in v1.

**The opportunity:** X-Plane's own UDP dataref feed (the Go port's `xp_udp.py`
equivalent) already delivers live aircraft heading and groundspeed, pulled in today
only to drive the map display. That's a real, already-available signal going unused for
the one thing that would matter most to texture pop-in: predicting which tiles/mip
levels will be needed in the next several seconds and fetching them *ahead of* the
aircraft, instead of waiting for X-Plane's read() to trigger the fetch and then racing
the `maxwait` deadline. Done well, this directly reduces how often §7's fallback path
(solid-color patch while real data is in flight) ever triggers in normal cruise flight —
most of the time the needed tile would already be warm in cache by the time it's
actually requested.

Two smaller, related wins to bundle into the same post-v1 pass:
- **Persist compressed DXT mip output across sessions**, not just the raw JPEG chunks.
  Today (both versions) a restart recomputes DXT compression for terrain already flown
  over in a prior session, even though nothing about that imagery changed. Compression
  is the actual CPU hot spot (§2/§2a), so caching its *output* keyed by tile+mip trades
  disk space for CPU that doesn't need to be spent twice.
- **Hysteresis at mip-level transition boundaries**, so an aircraft holding altitude
  near a threshold that flips between two mip levels doesn't thrash — redoing a fetch/
  recompute every time it crosses back and forth across the line.

**Decision:** design the tile engine's fetch-triggering path as a distinct, swappable
piece from day one (not fused directly into the FUSE `read()` handler) specifically so
"reactive only" can become "reactive + predictive prefetch" later without a rework —
same spirit as the provider registry (§5) and the `ImageDecoder`/`TextureCompressor`
interfaces (§2): get a correct reactive baseline working and measured first, then layer
prediction on top once there's a real system to benchmark it against.

## 13. Distribution/packaging: single-file-plus-sidecar per platform, FUSE driver stays external

**Implemented and packaged for Linux** (internal/native/{dxt_cgo.go,jpeg_turbo.go} for the
relocatable/static-link groundwork; `packaging/linux/` — `build.sh`/`install.sh`/
`autoOr-simg.desktop`/`README.txt` — for the actual distributable tarball, which `build.sh`
itself verifies before packaging: asserts `$ORIGIN/lib/linux` is the first RUNPATH entry and that
`libturbojpeg` is absent from `ldd`, so a build that regresses either check fails loudly instead of
silently shipping a broken tarball); see `.github/workflows/release.yml` and
`packaging/windows/`, `packaging/macos/` for the other two platforms. Decided in
direct response to the question "single exe, no runtime dependencies, like Python's
deployment story" answered by actually checking, not assuming — `readelf -d` on a plain `go build` output
showed the ISPC compressor's `RUNPATH` baked in as an absolute path into the dev checkout, i.e.
not portable at all as it stood.

**Decision — match (and where possible improve on) exactly what the Python original already
ships**, not invent a new distribution model: Python vendors `lib/linux/libispc_texcomp.so` /
`lib/windows/ispc_texcomp.dll` in the repo itself, loaded via a path relative to the app's own
install location (not a system package) — the Go side now does the same thing via a relocatable
rpath (`-Wl,-rpath,$ORIGIN/lib/linux`, with the dev-tree absolute path kept as a second fallback
entry so `go test`'s own temp-dir binaries keep working). Python's `aoimage/lib/libturbojpeg.a` is
already a **static** archive — the Go side now statically links that exact same file instead of
dynamically linking the system's `libturbojpeg.so.0`, which is strictly better than Python's own
model (one fewer runtime dependency, not just parity).

**What this does NOT eliminate, and can't**: `libfuse3` (Linux) / WinFsp (Windows) / macFUSE
(macOS) are real, unavoidable one-time system installs on every platform, for both the Python
original and this rewrite equally — `cgofuse` `dlopen()`s the FUSE library at runtime (confirmed
in its own source), and Python's own `winsetup.py` only *detects* an installed
WinFsp/Dokan, it doesn't install one either. No FUSE-based tool in any language can code its way
out of this — it's the one thing every distribution format below still requires the user to have
installed, same as today's Python releases already require.

**Linux distribution format, decided and built**: a plain tarball (`packaging/linux/build.sh`
produces it) — binary + `lib/linux/libispc_texcomp.so` + a `.desktop` entry (using
`internal/webui/web/logo.png`) in a folder, `install.sh` to place it and register the desktop
entry, no package manager, no root, works on literally any distro. Proven by extracting the real
built tarball into `/tmp` (outside the repo) and running a real `tile fetch` against the live EOX
provider from there, and by running `install.sh` against a throwaway `$HOME`. Considered and
explicitly deprioritized for now: `.deb` (nicer `apt install` experience
with automatic `libfuse3` dependency resolution, but Debian/Ubuntu-only — would need a second
format like an AUR/RPM package to cover everything else, not worth the maintenance burden before
there's demand for it) and AppImage (closest to "truly one file," but has a known rough edge
running a FUSE-mounting app from inside AppImage's own FUSE-based runtime — worth revisiting once
there's a reason to actually test it, not before). Flatpak/Snap ruled out entirely: their sandboxing
makes mounting a real filesystem into another app's (X-Plane's) actual directory tree exactly the
kind of thing they're designed to restrict.

**Windows: now built and packaged too, via a real, surprising discovery this session
overturned the previous session's assumption on.** The prior handoff said "this sandbox
cannot cross-compile" for Windows — true for a cgo build (confirmed again: no
mingw-w64 here), but nobody had actually tried a `CGO_ENABLED=0` build, and it turns
out `cgofuse` needs **no cgo at all on Windows** — it talks to WinFsp's DLL through
plain `golang.org/x/sys/windows` syscalls, unlike its Linux/macOS backends, which
really do need cgo (confirmed experimentally both ways, not assumed). That means a
genuine, fully-functional Windows binary cross-compiles from this plain Linux sandbox
with zero C toolchain, using `internal/native`'s existing pure-Go DXT/JPEG fallback
codecs in place of the cgo-accelerated ones. `packaging/windows/build.sh` builds it;
**proven for real**, not just compiled: the cross-built `.exe` was run under Wine,
`autoortho.exe config` showed correct Windows-native paths
(`C:\users\...\.autoortho`), and `autoortho.exe tile fetch` did a real network fetch
against both the Bing (BI) and EOX providers, decoded real JPEG chunks, BC1-compressed
them via the pure-Go path, and wrote a real multi-mipmap `.dds` — then the packaged zip
itself was extracted outside the repo and re-run under Wine with the same result,
exactly the "verify for real" bar every other session in this file has held to.

Three real gaps this surfaced and fixed along the way (all previously undocumented
precisely — DESIGN.md had only flagged diskspace and `IsMounted`, not the other two):
1. `internal/scenery/diskspace_other.go`'s Windows branch — now implemented via
   `GetDiskFreeSpaceEx` in `diskspace_windows.go` (pure syscall, no cgo).
2. `internal/lifecycle.IsMounted`/`ForceUnmount` — split into
   `stalemount_linux.go` (unchanged), `stalemount_darwin.go` (new: shells out to
   `mount`/`umount`/`diskutil unmount`, macOS has no `/proc/mounts`), and
   `stalemount_windows.go` (new: since AutoOrtho's Windows mountpoints are always a
   directory, not a drive letter — matching `autoortho/winsetup.py`'s
   `setup_winfsp_mount` — "mounted" is approximated as "the path exists", and
   force-unmount is a plain directory removal, safe because a live mount from this
   process never reaches this path; see the file's own comment for the reasoning).
3. **A real, previously-unflagged compile blocker**: `internal/fuse/autoortho.go`'s
   `Statfs` and `fillStatFromFileInfo` used `syscall.Statfs_t`/`Stat_t` directly with
   no build tag at all — Linux-only in practice (darwin's `Stat_t` has different field
   names, `Statfs_t` lacks `Namelen`; Windows has neither type). This was never
   Windows/macOS-only — it would have failed on darwin too. Split into
   `statfs_linux.go`/`statfs_darwin.go`/`statfs_windows.go`; the Windows one now
   derives `Statfs` from `GetDiskFreeSpaceEx` and skips uid/gid enrichment entirely
   (meaningless on Windows).
4. Also found and fixed: `internal/native/dxt_cgo.go`/`jpeg_turbo.go`/
   `defaults_cgo.go` were tagged bare `//go:build cgo`, not `cgo && linux`, even
   though their `#cgo LDFLAGS` hardcode ELF/Linux-only rpath syntax and
   Linux-only vendored lib paths. A future cgo-enabled macOS build would have hit
   "undefined: DecodeJPEGTurbo" instead of falling back cleanly. Narrowed the tag to
   `cgo && linux` and added `defaults_cgo_other.go` (`cgo && !linux`) so any future
   cgo build on a platform without a vendored accelerated codec still gets the
   pure-Go fallback instead of a broken build.
5. `ensureMountpoint` (WinFsp's "mountpoint must not pre-exist" requirement, flagged
   as a TODO in `internal/fuse/mount.go` since an earlier session) is now
   implemented too, mirroring `winsetup.py`'s `setup_winfsp_mount` exactly: split into
   `mountpoint_unix.go` (unchanged mkdir+must-be-empty logic) and
   `mountpoint_windows.go` (rename the pre-existing path to `<path>_bkp`, or fail
   asking for manual cleanup if that's also taken).

**Still explicitly NOT done for Windows**: the accelerated cgo codec path. Python's
`lib/windows/ispc_texcomp.dll`/`stb_dxt.dll` exist and are ready to reuse the same way
Linux's libs were, but there is **no Windows build of libjpeg-turbo vendored anywhere
in this repo** (checked directly — Python's Windows JPEG decode goes through a
monolithic prebuilt `aoimage.dll`, not a reusable standalone turbojpeg lib), so that
half needs sourcing before a `dxt_cgo_windows.go`/`jpeg_turbo_windows.go` pair can even
be attempted — and actually building/testing that cgo still needs mingw-w64 or a real
`windows-latest` CI run, neither available to this session.

**macOS: still genuinely unstarted, but narrowed to exactly one blocker.** Unlike
Windows, `cgofuse` really does require cgo on darwin (confirmed experimentally:
`GOOS=darwin CGO_ENABLED=0` fails with the same "undefined: c_struct_fuse" errors as
`CGO_ENABLED=1` without a compiler) — there is no syscall-only path like Windows got.
After fix #4 above, every other package in this repo now builds cleanly for
`GOOS=darwin CGO_ENABLED=1` **using only Go's own compiler** (no C code reached at
all, since the accelerated codecs are Linux-only and the fallback is pure Go); the
*only* remaining failure is `runtime/cgo` itself needing a real
macOS-targeting C compiler ("unsupported option '-arch' for target
'x86_64-pc-linux-gnu'" — confirmed precisely, not a guess). That compiler can only
come from running on real macOS (or a `macos-latest` CI runner) — no osxcross setup
exists in this sandbox and building one was out of scope this session.
`packaging/macos/build.sh` is written and ready (same pure-Go-codec-fallback approach
as Windows, since no native macOS codec libs exist either), but **has not been run or
proven** — it needs a real Mac or `.github/workflows/release.yml`'s `macos-latest` job
to execute even once before it can be trusted.

**CI now does the actual native building this sandbox can't**:
`.github/workflows/release.yml` builds Linux and Windows on `ubuntu-latest` (both are
genuine cross-compiles, proven working) and macOS on `macos-latest` (the one target
that needs a real native runner, for the reason above) — `.github/workflows/test.yml`
runs the equivalent build+vet+test on every push/PR. Neither workflow has been run for
real yet (this repo has no `.git`/remote in this sandbox to push and trigger it); both
need their first real run checked once this code reaches an actual GitHub repo.

## 14. System tray / menu-bar icon: `getlantern/systray`, wired to the exact actions the web UI already has

**Decision, matching what was scoped out earlier (see "Still open" below for the pre-decision
reasoning)**: `github.com/getlantern/systray` wraps Windows `Shell_NotifyIcon`, macOS
`NSStatusItem` (Cocoa), and Linux's AppIndicator/StatusNotifierItem protocol behind one API — no
new control-flow needed, its three menu items (Open AutoOrtho / Stop / Shutdown) just call
`webui.Server`'s newly-exported `RequestStop`/`RequestShutdown` (thin wrappers around the
existing `doStop`/`doShutdown` the web UI's own buttons already use) and `webui.OpenBrowser`.
`internal/tray` owns the systray wiring; `cmd/autoortho/main.go`'s `wantsTray`/
`runWithOptionalTray` decide whether to engage it at all.

**Proven live on the user's own real desktop, not just compiled**: this sandbox turned out to
have an actual running KDE Plasma (Wayland) session — the user's own desktop environment,
confirmed by `XDG_CURRENT_DESKTOP=KDE` and real `plasmashell`/`kwin_wayland` processes, not a
mock. Built with `-tags legacy_appindicator` (this sandbox has `libappindicator` but not the
newer `libayatana-appindicator`; see below), ran the real binary, and confirmed via direct D-Bus
calls — not guessing from a screenshot — that: `org.kde.StatusNotifierWatcher`'s
`RegisteredStatusNotifierItems` listed our exact PID under
`/org/ayatana/NotificationItem/systray`; the `Title` property read back "AutoOrtho"; the
DBusMenu `GetLayout` call showed exactly the three expected items ("Open AutoOrtho", a separator,
"Stop", "Shutdown"); and sending a real DBusMenu `Event("clicked")` to item id 4 ("Stop") produced
the real `lifecycle` log line "all mounts stopped (instance lock retained)", then clicking id 5
("Shutdown") produced the exact same guaranteed-shutdown sequence a SIGINT or the web UI's own
Shutdown button triggers ("Shutdown requested from the web UI, unmounting everything...", "all
mounts stopped", "shutdown complete, instance lock released", process actually exits). Re-ran the
same sequence against the real packaged `packaging/linux/build.sh` tarball extracted outside the
repo, same result.

**A real crash this session found and fixed, not a hypothetical**: initially, running `autoortho
run` without `-H` on a terminal with no `$DISPLAY`/`$WAYLAND_DISPLAY` set (confirmed by actually
unsetting them and running it) crashed the whole process — GTK's init aborts hard
("Gtk-WARNING: cannot open display") instead of returning an error, unlike the already-existing
toast/browser-open calls, which just log a warning and continue. `internal/tray.Available()`
(Linux: checks those two env vars; Windows/macOS: always true, no equivalent cheap check and no
real headless-desktop use case there) now gates `wantsTray` so a headless box with no GUI session
degrades to "no tray, works fine" instead of crashing — confirmed by re-running the same
no-display case after the fix: clean skip, normal operation, no crash.

**Linux build-time dependency, new as of this feature**: `internal/tray` needs an AppIndicator
dev package to compile at all — `ayatana-appindicator3-0.1` (the modern one; current
Ubuntu/Mint/Arch all package it) by default, or `-tags legacy_appindicator` for the older
`appindicator3-0.1` this sandbox actually has installed (confirmed via `pkg-config`).
`packaging/linux/build.sh` auto-detects which one is available and fails loudly with install
instructions if neither is. **Runtime** dependency is just GTK3, already present on any desktop
Linux install with a GUI — not a new ask for the actual target distros (KDE Plasma — the user's
own setup — Cinnamon, GNOME all either support the AppIndicator protocol natively or, for GNOME,
via the AppIndicator extension Ubuntu ships enabled by default).

**Windows/macOS: code written and cross-compiles, but unproven beyond that.** `systray`'s Windows
backend is pure `golang.org/x/sys/windows` syscalls — no cgo, consistent with the WinFsp discovery
in §13 — so it didn't break the existing `CGO_ENABLED=0` Windows cross-build at all (confirmed:
still builds clean). Ran the cross-built `.exe` under Wine; it stayed alive for the full test
window without crashing (only Wine's own known-incomplete shell/OLE integration logged warnings),
which is reassuring but **not** the same as confirming a tray icon actually renders in a real
Windows taskbar — that still needs a real Windows machine or CI run. macOS's backend needs cgo +
the Cocoa/WebKit frameworks (bundled with Xcode, no extra install), consistent with §13's existing
macOS blocker (no osxcross here) — no new blocker introduced, but also not newly solved; still
needs a real Mac or the `macos-latest` CI job to prove anything.

## Still open / revisit later

- ~~Exact wire protocol for live scenery-download progress in the web UI~~ — **resolved**: plain
  WebSocket, one connection per tab, per DESIGN.md §3's full implementation.
- ~~System-tray/menu-bar icon (Open/Stop/Shutdown shortcuts)~~ — **resolved**: built, and proven
  live on the user's own real KDE Plasma desktop. See §14.
- Revisit the Goofy-vs-ISPC compressor choice with real benchmark numbers once a
  correctness baseline (ISPC port) exists — don't block initial development on this.
