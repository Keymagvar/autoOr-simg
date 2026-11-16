// Command autoortho is the entrypoint for the Go rewrite. For now it only
// wires up config + logging + the scenery downloader (migration step 1,
// per autoortho/AGENTS.md); FUSE/tile/web-UI pieces land in later steps.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"autoOr-simg/internal/config"
	aofuse "autoOr-simg/internal/fuse"
	"autoOr-simg/internal/lifecycle"
	"autoOr-simg/internal/logging"
	"autoOr-simg/internal/memwatch"
	"autoOr-simg/internal/scenery"
	"autoOr-simg/internal/tile"
	"autoOr-simg/internal/tray"
	"autoOr-simg/internal/webui"
)

// version is set at build time via -ldflags "-X main.version=...";
// "dev" when built with a plain `go build`.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	confPath, err := config.DefaultConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(confPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config defaults: %w", err)
	}

	// Apply the process-wide tile-package overrides from config before any
	// Tile is ever created — see tile.SetChunkConcurrencyOverride's doc
	// comment. Done here, once, for every subcommand (not just `run`), so
	// `autoortho tile fetch`/`tile stress` exercise the same configured
	// values a real `run` would.
	tile.SetChunkConcurrencyOverride(cfg.AutoOrtho.ChunkConcurrency)
	tile.SetPerformanceMode(cfg.AutoOrtho.PerformanceMode)
	tile.SetDisableCoarseFallback(cfg.AutoOrtho.DisableCoarseFallback)

	if _, err := logging.Init(cfg.Paths.LogFile); err != nil {
		return fmt.Errorf("initializing logging: %w", err)
	}
	if cfg.General.Debug {
		logging.Level.Set(slog.LevelDebug)
	}
	defer logging.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Diagnostic-only pprof listener, localhost-bound. Added specifically
	// to settle a real big-map OOM investigation with real evidence instead
	// of more guessing: every real crash tonight has landed in the same
	// ~21-24GB range regardless of which of several real, independently
	// verified bugs got fixed in between — a consistency that suggests
	// the dominant leak is somewhere none of those fixes touched. A heap
	// profile taken during the next live reproduction (`go tool pprof
	// http://127.0.0.1:6061/debug/pprof/heap`) shows the actual
	// allocation site holding the growing memory, which code-reading
	// alone has not been reliable enough to find tonight.
	go func() {
		srv := &http.Server{Addr: "127.0.0.1:6061", Handler: nil}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.For("pprof").Warn("pprof listener failed", "err", err)
		}
	}()

	if len(os.Args) < 2 {
		printUsage()
		return nil
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("autoOr-simg %s\n", version)
		return nil
	case "run":
		return runWithOptionalTray(ctx, cfg, os.Args[2:])
	case "scenery":
		return sceneryCmd(ctx, cfg, os.Args[2:])
	case "fuse":
		return fuseCmd(ctx, cfg, os.Args[2:])
	case "tile":
		return tileCmd(ctx, cfg, os.Args[2:])
	case "config":
		fmt.Printf("Config file: %s\n", confPath)
		fmt.Printf("X-Plane path: %s\n", cfg.Paths.XplanePath)
		fmt.Printf("Scenery path: %s\n", cfg.Paths.SceneryPath)
		fmt.Printf("Cache dir: %s\n", cfg.Paths.CacheDir)
		fmt.Printf("Download dir: %s\n", cfg.Paths.DownloadDir)
		fmt.Printf("Min zoom: %d\n", cfg.AutoOrtho.MinZoom)
		fmt.Printf("Scenery mounts: %d configured\n", len(cfg.SceneryMounts))
		for _, m := range cfg.SceneryMounts {
			fmt.Printf("  %s -> %s\n", m.Root, m.Mount)
		}
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}

func printUsage() {
	fmt.Println(`autoOr-simg - early scaffolding (scenery downloader + config + FUSE PoC)

Usage:
  autoortho run [-H|--headless]      The real entrypoint: acquire the
                                       single-instance lock (DESIGN.md §4a),
                                       clear any stale mount left by a
                                       previous crash (§4b), mount every
                                       configured scenery region, and start
                                       the web UI (§3) at http://127.0.0.1:
                                       <webui_port> (firing a startup toast
                                       and opening a browser tab there,
                                       unless -H/--headless or
                                       general.auto_open_ui=false). Runs
                                       until interrupted (Ctrl+C, the memory
                                       watchdog's Critical stage, or the web
                                       UI's own Shutdown button), unmounting
                                       everything and releasing the lock on
                                       the way out (§4c) — including on a
                                       panic.
  autoortho config                   Show the active config and derived paths
  autoortho scenery list              List available/installed scenery regions
  autoortho scenery fetch <region>    Download and install a region
  autoortho fuse testmount <path>     Mount the trivial test filesystem at
                                       path until interrupted (Ctrl+C), then
                                       cleanly unmount. Proves the cgofuse
                                       mount/unmount lifecycle end to end
                                       (DESIGN.md §1, §4, §8) before any
                                       real tile-serving logic exists.
  autoortho fuse mount <root> <mountpoint>
                                       Mount the real filesystem: <root> is
                                       a real scenery dir (its .dsf/.ter
                                       files pass through), <mountpoint> is
                                       where X-Plane (or you, by hand) reads
                                       synthesized .dds tiles from. Runs
                                       until interrupted (Ctrl+C), then
                                       cleanly unmounts.
  autoortho tile fetch <row> <col> <maptype> <zoom> <mipmap> <out.dds>
                                       Build one real tile's DDS data (real
                                       HTTP fetch + decode + composite +
                                       BC1 compress) down through the
                                       requested mipmap level and write it
                                       to out.dds. Exercises the real
                                       engine end to end (migration step 5).
  autoortho tile stress <maptype> <native_zoom> <max_zoom_cap> <num_tiles> <duration_seconds> <base_row> <base_col> [mipmap (-1=mixed mip0/mip4)]
                                       Big-map OOM diagnostic: opens
                                       num_tiles native-zoom tiles (real
                                       HTTP fetches, never closed, simulating
                                       X-Plane holding many textures open at
                                       once) with max_zoom_cap applied (0 =
                                       no cap), printing Cacher.Len()/
                                       OpenCount(), real process RSS, and
                                       budget-degraded count every 2s for
                                       duration_seconds.`)
}

func tileCmd(ctx context.Context, cfg *config.Config, args []string) error {
	if len(args) >= 1 && args[0] == "stress" {
		return tileStressCmd(ctx, cfg, args[1:])
	}

	// args: "fetch" <row> <col> <maptype> <zoom> <mipmap> <out.dds> — 7 total.
	if len(args) != 7 || args[0] != "fetch" {
		printUsage()
		return fmt.Errorf("tile: usage: autoortho tile fetch <row> <col> <maptype> <zoom> <mipmap> <out.dds>")
	}
	var row, col, zoom, mipmap int
	if _, err := fmt.Sscanf(args[1], "%d", &row); err != nil {
		return fmt.Errorf("bad row: %w", err)
	}
	if _, err := fmt.Sscanf(args[2], "%d", &col); err != nil {
		return fmt.Errorf("bad col: %w", err)
	}
	maptype := args[3]
	if _, err := fmt.Sscanf(args[4], "%d", &zoom); err != nil {
		return fmt.Errorf("bad zoom: %w", err)
	}
	if _, err := fmt.Sscanf(args[5], "%d", &mipmap); err != nil {
		return fmt.Errorf("bad mipmap: %w", err)
	}
	outPath := args[6]

	maxWait := time.Duration(cfg.AutoOrtho.MaxWait * float64(time.Second))
	cacher := tile.NewCacher(cfg.Paths.CacheDir, cfg.AutoOrtho.MaxOpenTiles, maxWait, cfg.AutoOrtho.MaxConcurrentBuilds, cfg.AutoOrtho.MaxOpenTiles, cfg.AutoOrtho.FetchThreads)
	t, ok := cacher.Open(row, col, maptype, zoom, cfg.AutoOrtho.MinZoom, cfg.AutoOrtho.MaxZoom)
	if !ok {
		return fmt.Errorf("unknown maptype %q", maptype)
	}
	defer cacher.Close(row, col, maptype, zoom)

	start := time.Now()
	fmt.Printf("Building tile row=%d col=%d maptype=%s zoom=%d down to mipmap %d ...\n", row, col, maptype, zoom, mipmap)
	t.BuildMipmap(ctx, mipmap)
	fmt.Printf("Done in %s (maxwait=%s, fallback-triggered count=%d)\n", time.Since(start), maxWait, t.FallbackChunkCount())

	full := t.DDS.Read(0, t.DDS.TotalSize)
	if err := os.WriteFile(outPath, full, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	for _, m := range t.DDS.Mipmaps {
		fmt.Printf("  mipmap %2d: %dx%d retrieved=%v\n", m.Idx, t.DDS.Width>>m.Idx, t.DDS.Height>>m.Idx, m.Retrieved)
	}
	fmt.Printf("Wrote %d bytes to %s\n", len(full), outPath)
	return nil
}

// tileStressCmd is a diagnostic tool for the big-map OOM investigation: it simulates
// X-Plane opening a burst of tiles and holding every one of them open
// (never calling Cacher.Close, exactly like X-Plane holding texture file
// handles while rendering a large area) — the one real-world condition
// that the MaxOpenTiles budget fix (internal/tile) and MaxZoom's row/col
// collapsing (internal/tile.NewTile) both exist to handle. It opens
// numTiles distinct *native*-zoom (row, col) requests spread across a
// grid, so that when maxZoomCap is set below nativeZoom, adjacent
// requests collapse onto the same lower-zoom Tile — directly observable
// here via how many distinct *Tile pointers Cacher.Open actually returns,
// not just how many requests were made. Every opened tile's BuildMipmap(0)
// runs for real (real HTTP fetches against the real provider, bounded by
// the configured MaxConcurrentBuilds, exactly like production), and every
// durationSeconds-bounded tick prints Cacher.Len()/OpenCount(), real
// process RSS, and aggregate budget-degraded count, so a run's output is a
// real, reproducible before/after data point — not a simulated one.
func tileStressCmd(ctx context.Context, cfg *config.Config, args []string) error {
	if len(args) != 7 && len(args) != 8 {
		return fmt.Errorf("tile stress: usage: autoortho tile stress <maptype> <native_zoom> <max_zoom_cap> <num_tiles> <duration_seconds> <base_row> <base_col> [mipmap (-1=mixed mip0/mip4)]")
	}
	maptype := args[0]
	var nativeZoom, maxZoomCap, numTiles, durationSeconds, baseRow, baseCol int
	if _, err := fmt.Sscanf(args[1], "%d", &nativeZoom); err != nil {
		return fmt.Errorf("bad native_zoom: %w", err)
	}
	if _, err := fmt.Sscanf(args[2], "%d", &maxZoomCap); err != nil {
		return fmt.Errorf("bad max_zoom_cap: %w", err)
	}
	if _, err := fmt.Sscanf(args[3], "%d", &numTiles); err != nil {
		return fmt.Errorf("bad num_tiles: %w", err)
	}
	if _, err := fmt.Sscanf(args[4], "%d", &durationSeconds); err != nil {
		return fmt.Errorf("bad duration_seconds: %w", err)
	}
	if _, err := fmt.Sscanf(args[5], "%d", &baseRow); err != nil {
		return fmt.Errorf("bad base_row: %w", err)
	}
	if _, err := fmt.Sscanf(args[6], "%d", &baseCol); err != nil {
		return fmt.Errorf("bad base_col: %w", err)
	}
	// mipmap defaults to 0 (full native-res fetch, 256 chunks/tile — realistic
	// but expensive at high num_tiles); pass a higher mipmap (e.g. 4, a single
	// coarse chunk/tile) to exercise the Open/budget machinery at large
	// num_tiles without hammering the real provider with tens of thousands of
	// chunk requests — the budget check in buildMipmap doesn't care which
	// mipmap is being built.
	mipmap := 0
	if len(args) == 8 {
		if _, err := fmt.Sscanf(args[7], "%d", &mipmap); err != nil {
			return fmt.Errorf("bad mipmap: %w", err)
		}
	}

	maxWait := time.Duration(cfg.AutoOrtho.MaxWait * float64(time.Second))
	cacher := tile.NewCacher(cfg.Paths.CacheDir, cfg.AutoOrtho.MaxOpenTiles, maxWait, cfg.AutoOrtho.MaxConcurrentBuilds, cfg.AutoOrtho.MaxOpenTiles, cfg.AutoOrtho.FetchThreads)

	fmt.Printf("Stress test: maptype=%s native_zoom=%d max_zoom_cap=%d num_tiles=%d duration=%ds\n", maptype, nativeZoom, maxZoomCap, numTiles, durationSeconds)
	fmt.Printf("Config: MinZoom=%d MaxConcurrentBuilds=%d MaxOpenTiles=%d\n", cfg.AutoOrtho.MinZoom, cfg.AutoOrtho.MaxConcurrentBuilds, cfg.AutoOrtho.MaxOpenTiles)

	side := 1
	for side*side < numTiles {
		side++
	}

	// mipmap=-1 means "mixed": alternate mip0 (full native-res, 256
	// chunks) and mip4 (coarse/distant, 1 chunk) across tiles — directly
	// reproducing the real bug this verifies the fix for (the
	// second "Berlin" finding): a shared pool prioritizing coarse fetches
	// over mip0 starved mip0 completely once enough coarse-mip churn kept
	// arriving continuously. Real network, not a mock, so this exercises
	// actual latency/jitter, not just the logic in isolation.
	mixed := mipmap < 0

	// Opened gradually over the full duration (a few per 2s tick, below),
	// not all at once — the "sustained vs. burst" question: every earlier
	// synthetic test opened all its tiles in one upfront
	// burst and then sat idle, which never reproduced the real crash
	// (settled at 3.37GB vs. a real flight's 24GB at a similar tile
	// count). A real flight never does one burst — X-Plane keeps
	// requesting new tiles continuously for as long as it flies, AND it
	// keeps re-reading already-open tiles repeatedly (LOD/distance
	// changes), not just opening each one once and never touching it
	// again. Both are reproduced here: gradual opens across the whole
	// run, and each opened tile gets a long-running goroutine that keeps
	// re-calling BuildMipmap every few seconds for as long as the test
	// runs, instead of a single fire-and-forget call.
	var mu sync.Mutex
	opened := make([]*tile.Tile, 0, numTiles)
	targetMipmap := make([]int, 0, numTiles)
	seen := map[*tile.Tile]bool{}
	openNext := func(i int) {
		row := baseRow + i/side
		col := baseCol + i%side
		t, ok := cacher.Open(row, col, maptype, nativeZoom, cfg.AutoOrtho.MinZoom, maxZoomCap)
		if !ok {
			return
		}
		mu.Lock()
		alreadySeen := seen[t]
		if !alreadySeen {
			seen[t] = true
		}
		mu.Unlock()
		if alreadySeen {
			return
		}
		tileMipmap := mipmap
		if mixed {
			if len(opened)%2 == 0 {
				tileMipmap = 0
			} else {
				tileMipmap = 4
			}
		}
		mu.Lock()
		opened = append(opened, t)
		targetMipmap = append(targetMipmap, tileMipmap)
		mu.Unlock()
		go func() {
			// Re-reads the same tile every few seconds for the rest of
			// the run — standing in for X-Plane repeatedly touching a
			// texture that stays in view — but also, ~30% of the time,
			// closes and reopens it first: standing in for a tile
			// genuinely leaving and re-entering view as a real flight's
			// camera moves, which is exactly the "OpenCount dips back
			// under budget, letting a new batch join the resident set"
			// pattern the cumulative-resident-tiles finding
			// depends on — a tile that's simply never closed (as every
			// earlier version of this tool did) can't exercise it at all.
			for {
				if rand.Intn(10) < 3 {
					cacher.Close(row, col, maptype, nativeZoom)
					cacher.Open(row, col, maptype, nativeZoom, cfg.AutoOrtho.MinZoom, maxZoomCap)
				}
				t.BuildMipmap(ctx, tileMipmap)
				select {
				case <-ctx.Done():
					return
				case <-time.After(4 * time.Second):
				}
			}
		}()
	}
	fmt.Printf("Will open %d native-zoom tiles gradually over %ds (not all at once) -> up to that many distinct Tile(s) after zoom-cap collapsing\n", numTiles, durationSeconds)

	// Wire the real memwatch.Watchdog — same production code path as
	// runWithOptionalTray — rather than hand-replicating its Soft-stage
	// action here, so this test proves the actual fix end to end: a
	// genuinely crossed Soft threshold must trigger ForceEvictIdle +
	// GC + FreeOSMemory on its own, with no manual intervention from this
	// test. Soft is set artificially low (half this process's own current
	// RSS) so it reliably fires during a short test run regardless of the
	// real configured percentage.
	startRSS, _ := memwatch.ReadRSSBytes()
	totalMem, err := memwatch.TotalSystemMemoryBytes()
	if err == nil && startRSS > 0 {
		thresholds := memwatch.Thresholds{
			Soft:     startRSS / 2,
			Hard:     totalMem, // effectively disabled for this test
			Critical: totalMem, // effectively disabled for this test
		}
		wd := memwatch.New(thresholds, totalMem, 2*time.Second,
			func() {
				n := cacher.ForceEvictIdle()
				fmt.Printf("--- memwatch Soft fired: ForceEvictIdle()=%d, now GC()+FreeOSMemory() ---\n", n)
				runtime.GC()
				debug.FreeOSMemory()
			},
			nil, nil,
		)
		go wd.Run(ctx)
	} else {
		fmt.Println("(could not read RSS/total memory — skipping real memwatch wiring for this run)")
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.Now().Add(time.Duration(durationSeconds) * time.Second)
	start := time.Now()

	// Spread opens across roughly the first 80% of the run (a real flight
	// doesn't usually keep discovering brand-new ground right up to the
	// last second) at a few per tick, rather than one upfront burst.
	totalTicks := durationSeconds / 2
	openWindowTicks := totalTicks * 8 / 10
	if openWindowTicks < 1 {
		openWindowTicks = 1
	}
	tilesPerTick := (numTiles + openWindowTicks - 1) / openWindowTicks
	nextToOpen := 0

	for tick := 0; time.Now().Before(deadline); tick++ {
		<-ticker.C
		for i := 0; i < tilesPerTick && nextToOpen < numTiles; i++ {
			openNext(nextToOpen)
			nextToOpen++
		}

		mu.Lock()
		snapshotTiles := append([]*tile.Tile(nil), opened...)
		snapshotMipmap := append([]int(nil), targetMipmap...)
		mu.Unlock()

		var degraded, zoomDropped int64
		var built, builtMip0 int
		for i, t := range snapshotTiles {
			degraded += t.BudgetDegradedCount()
			zoomDropped += t.ZoomDropCount()
			if t.DDS.Mipmaps[snapshotMipmap[i]].Retrieved {
				built++
				if snapshotMipmap[i] == 0 {
					builtMip0++
				}
			}
		}
		rss, _ := memwatch.ReadRSSBytes()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		fmt.Printf("[%6s] opened=%d/%d Cacher.Len()=%d OpenCount()=%d BuiltContentCount()=%d rss=%dMB heapAlloc=%dMB heapSys=%dMB heapReleased=%dMB numGC=%d built=%d/%d builtMip0=%d budget_degraded_total=%d zoom_dropped_total=%d\n",
			time.Since(start).Round(time.Second), len(snapshotTiles), numTiles, cacher.Len(), cacher.OpenCount(), cacher.BuiltContentCount(), rss/1024/1024,
			ms.HeapAlloc/1024/1024, ms.HeapSys/1024/1024, ms.HeapReleased/1024/1024, ms.NumGC,
			built, len(snapshotTiles), builtMip0, degraded, zoomDropped)
	}

	fmt.Println("Done. Tiles stay open (never Closed) for the duration of this process, matching X-Plane's real behavior.")
	return nil
}

// wantsTray reports whether this invocation should run under
// internal/tray's native taskbar/menu-bar icon — never under -H/
// --headless (no GUI at all in that mode, same reasoning as skipping the
// startup toast/browser-open), with general.enable_tray=false, or when
// tray.Available() says there's no GUI session to put an icon in (a
// headless Linux box where the user forgot -H — confirmed this actually
// crashes GTK's init instead of failing gracefully, see
// internal/tray/available_linux.go). runWithOptionalTray and runCmd both
// call this with the exact same inputs so their tray decisions can never
// drift apart.
func wantsTray(cfg *config.Config, args []string) bool {
	if !cfg.General.EnableTray {
		return false
	}
	if cfg.General.Headless {
		return false
	}
	for _, a := range args {
		if a == "-H" || a == "--headless" {
			return false
		}
	}
	return tray.Available()
}

// runWithOptionalTray wraps runCmd in internal/tray.Run when wantsTray
// says to. systray requires the OS thread that first called into it (here,
// main()'s own goroutine, via this function) for its native event loop —
// see tray.Run's own doc comment — so runCmd's actual work has to happen
// inside the callback tray.Run hands it, not the other way around.
func runWithOptionalTray(ctx context.Context, cfg *config.Config, args []string) error {
	if !wantsTray(cfg, args) {
		return runCmd(ctx, cfg, args)
	}
	var runErr error
	tray.Run(func() {
		runErr = runCmd(ctx, cfg, args)
		tray.Quit()
	})
	return runErr
}

// runCmd is the real entrypoint: DESIGN.md §4's single-instance lock +
// stale-mount cleanup + guaranteed shutdown (internal/lifecycle), driving
// a real AutoOrtho filesystem per configured scenery region. A panic
// anywhere in the mount loop or wait is converted into a logged error
// after running the same guaranteed shutdown a clean exit would (§4c: "a
// panic still runs this same shutdown sequence instead of leaving a mount
// dangling").
func runCmd(ctx context.Context, cfg *config.Config, args []string) (retErr error) {
	headless := cfg.General.Headless
	for _, a := range args {
		if a == "-H" || a == "--headless" {
			headless = true
		}
	}

	lockPath := filepath.Join(filepath.Dir(cfg.Paths.CacheDir), "autoortho.lock")
	mgr, err := lifecycle.NewManager(lockPath)
	if err != nil {
		var already *lifecycle.ErrAlreadyRunning
		if errors.As(err, &already) {
			return fmt.Errorf("not starting: %w", err)
		}
		return fmt.Errorf("acquiring instance lock: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			mgr.Shutdown()
			retErr = fmt.Errorf("recovered from panic, shut down cleanly: %v", r)
		}
	}()

	// maxTiles (NewCacher's 2nd arg) used to be hardcoded to 25 here,
	// completely disconnected from the user's configured MaxOpenTiles —
	// a real bug found live on a Moscow/VKO flight: evictLocked (whole-
	// tile eviction, bounded by maxTiles) skips any tile with a pending
	// repair, and under sustained heavy overload nearly every tile has
	// one, so it had almost nothing left it could safely evict — the
	// *whole-tile* map ballooned to ~622 resident tiles against a
	// hardcoded cap of 25 (and even past MaxOpenTiles=300, since
	// repairSem alone, sized to MaxOpenTiles, was never the bottleneck
	// here — this smaller, unrelated cap was). Tying it to MaxOpenTiles
	// means the whole-tile cap is never *tighter* than the open-content
	// budget the user actually configured.
	maxWait := time.Duration(cfg.AutoOrtho.MaxWait * float64(time.Second))
	cacher := tile.NewCacher(cfg.Paths.CacheDir, cfg.AutoOrtho.MaxOpenTiles, maxWait, cfg.AutoOrtho.MaxConcurrentBuilds, cfg.AutoOrtho.MaxOpenTiles, cfg.AutoOrtho.FetchThreads)

	eng := &runEngine{cfg: cfg, mgr: mgr, cacher: cacher}

	if len(cfg.SceneryMounts) == 0 {
		fmt.Printf("warning: no scenery installed under %s — the FUSE engine has nothing to mount yet. The web UI's Run button will retry once something is installed via `autoortho scenery fetch <region>` (or the Scenery block).\n", cfg.AOSceneryPath)
	} else if _, err := eng.Mount(ctx); err != nil {
		mgr.Shutdown()
		return fmt.Errorf("mounting scenery: %w", err)
	}

	// DESIGN.md §3: the web UI replaces config_ui.py entirely — server-
	// authoritative state over one WebSocket per tab, absorbing
	// flighttrack.py's conceptual stats block. Scenery manager failures
	// (e.g. no network) degrade the Scenery block to "no regions", not a
	// startup failure — the FUSE/tile engine above is already running.
	infoCache := filepath.Join(filepath.Dir(cfg.Paths.CacheDir), ".release_info")
	sceneryMgr, sErr := scenery.NewManager(cfg.Paths.SceneryPath, cfg.Paths.DownloadDir, infoCache)
	if sErr != nil {
		logging.For("webui").Warn("scenery manager unavailable, Scenery block will be empty", "err", sErr)
		sceneryMgr = nil
	}
	webSrv := webui.New(cfg, eng, sceneryMgr)
	webErrCh := make(chan error, 1)
	go func() { webErrCh <- webSrv.Run(ctx) }()

	if headless {
		fmt.Printf("autoOr-simg web UI listening at %s (headless: no toast, no browser launch)\n", webSrv.URL())
	} else {
		if err := webui.NotifyStartup(webSrv.URL()); err != nil {
			logging.For("webui").Warn("startup notification failed (continuing without it)", "err", err)
		}
		fmt.Printf("autoOr-simg web UI listening at %s\n", webSrv.URL())
		if cfg.General.AutoOpenUI {
			if err := webui.OpenBrowser(webSrv.URL()); err != nil {
				logging.For("webui").Warn("opening browser failed (continuing without it)", "err", err)
			}
		}
		if wantsTray(cfg, args) {
			// Same actions the web UI's own Open/Stop/Shutdown controls
			// trigger (DESIGN.md §8) — the tray adds no new behavior, just
			// another way to reach what's already there.
			tray.SetCallbacks(tray.Callbacks{
				Open: func() {
					if err := webui.OpenBrowser(webSrv.URL()); err != nil {
						logging.For("webui").Warn("opening browser from tray failed", "err", err)
					}
				},
				Stop:     webSrv.RequestStop,
				Restart:  webSrv.RequestRestart,
				Shutdown: webSrv.RequestShutdown,
			})
		}
	}

	// DESIGN.md §9 memory watchdog — not a formality, a real finding: the
	// first live test flight drove RSS to ~20GB within ~10 minutes of
	// normal scenery loading at one airport, because
	// Cacher can only evict tiles with no outstanding refs, and X-Plane
	// was legitimately holding many tiles open at once. Soft-stage
	// eviction is a real but partial mitigation (see
	// Cacher.ForceEvictIdle's doc comment on when it can't help); Critical
	// is the actual safety net — a clean, voluntary shutdown instead of
	// waiting for the OS OOM-killer to pick a victim process.
	totalMem, err := memwatch.TotalSystemMemoryBytes()
	critical := make(chan string, 1)
	if err != nil {
		logging.For("memwatch").Warn("could not read total system memory, memory watchdog disabled for this run", "err", err)
	} else {
		thresholds := memwatch.NewThresholdsFromPercent(totalMem, cfg.Memory.SoftPercent, cfg.Memory.HardPercent, cfg.Memory.CriticalPercent)
		wd := memwatch.New(thresholds, totalMem, 5*time.Second,
			func() { // Soft: evict every idle tile right now, not just on the next Open.
				if n := cacher.ForceEvictIdle(); n > 0 {
					logging.For("memwatch").Info("soft threshold: evicted idle tiles", "evicted", n, "remaining_cached", cacher.Len())
				}
				// Also force a GC + return freed pages to the OS. Measured,
				// not theoretical (autoortho tile stress testing):
				// after building 9 tiles, RSS sat at ~1.1GB and stayed
				// there, flat, for 40+ idle seconds — Go's default GC/
				// scavenger pacing does not promptly return a build
				// burst's transient garbage (~67MB/tile composite +
				// decode buffers) to the OS on its own. One
				// runtime.GC()+debug.FreeOSMemory() call dropped that same
				// RSS to ~105MB immediately. Without this, the RSS-based
				// watchdog itself (this package) can see — and act on, up
				// to and including Critical's shutdown — memory that is
				// pure reclaimable garbage, not anything actually needed.
				runtime.GC()
				debug.FreeOSMemory()
			},
			func(rssBytes, totalBytes uint64) { // Hard: loud warning (toast deferred, see DESIGN.md §9/§4a scope-cut note)
				logging.For("memwatch").Error("memory usage is high", "rss_bytes", rssBytes, "total_bytes", totalBytes, "cached_tiles", cacher.Len())
			},
			func(rssBytes, totalBytes uint64) { // Critical: trigger the one guaranteed shutdown sequence, don't act twice.
				select {
				case critical <- fmt.Sprintf("rss=%d total=%d cached_tiles=%d", rssBytes, totalBytes, cacher.Len()):
				default:
				}
			},
		)
		go wd.Run(ctx)
	}

	fmt.Printf("autoOr-simg running. Press Ctrl+C or use Shutdown at %s to exit.\n", webSrv.URL())
	restarting := false
	select {
	case <-ctx.Done():
		fmt.Println("\nShutdown requested, unmounting everything...")
	case reason := <-critical:
		fmt.Printf("\nCRITICAL memory threshold crossed (%s) — shutting down automatically to avoid an uncontrolled OOM.\n", reason)
	case <-webSrv.ShutdownRequested():
		fmt.Println("\nShutdown requested from the web UI, unmounting everything...")
	case <-webSrv.RestartRequested():
		fmt.Println("\nRestart requested from the web UI, unmounting everything...")
		restarting = true
	case err := <-webErrCh:
		if err != nil {
			fmt.Printf("\nweb UI server exited unexpectedly (%v), shutting down...\n", err)
		}
	}
	mgr.Shutdown()
	if restarting {
		fmt.Println("Restarting...")
		// mgr.Shutdown() already released the instance lock (its own doc
		// comment: "Shutdown is exactly Stop, then also release the lock")
		// — the freshly re-exec'd process can acquire it again immediately,
		// same as any other fresh launch. reExecSelf replaces this process
		// image outright (same PID on Linux/macOS; a new PID with this one
		// exiting right after on Windows, cgofuse/WinFsp doesn't support a
		// true in-place exec) — it only returns on failure, never on
		// success.
		if err := reExecSelf(); err != nil {
			return fmt.Errorf("restarting: %w", err)
		}
	}
	fmt.Println("Shutdown complete.")
	return nil
}

// runEngine adapts internal/lifecycle.Manager + internal/tile.Cacher to
// webui.Engine — the thin seam DESIGN.md §3's web UI needs to actually
// mount/stop scenery and report live stats, without the webui package
// importing FUSE/lifecycle machinery directly.
type runEngine struct {
	cfg    *config.Config
	mgr    *lifecycle.Manager
	cacher *tile.Cacher
}

func (e *runEngine) Mount(ctx context.Context) ([]webui.MountStatus, error) {
	var statuses []webui.MountStatus
	for _, sm := range e.cfg.SceneryMounts {
		fs := aofuse.NewAutoOrtho(sm.Root, e.cacher, e.cfg.AutoOrtho.MinZoom, e.cfg.AutoOrtho.MaxZoom, e.cfg.AutoOrtho.MaptypeOverride)
		if err := e.mgr.MountFresh(ctx, sm.Mount, fs); err != nil {
			return statuses, fmt.Errorf("mounting %s: %w", sm.Mount, err)
		}
		statuses = append(statuses, webui.MountStatus{Root: sm.Root, Mount: sm.Mount})
	}
	return statuses, nil
}

func (e *runEngine) Stop() {
	e.mgr.StopMounts()
}

func (e *runEngine) MountOne(ctx context.Context, root, mount string) (webui.MountStatus, error) {
	fs := aofuse.NewAutoOrtho(root, e.cacher, e.cfg.AutoOrtho.MinZoom, e.cfg.AutoOrtho.MaxZoom, e.cfg.AutoOrtho.MaptypeOverride)
	if err := e.mgr.MountFresh(ctx, mount, fs); err != nil {
		return webui.MountStatus{}, fmt.Errorf("mounting %s: %w", mount, err)
	}
	return webui.MountStatus{Root: root, Mount: mount}, nil
}

func (e *runEngine) UnmountOne(mount string) error {
	return e.mgr.UnmountOne(mount)
}

func (e *runEngine) MountStatuses() []webui.MountStatus {
	mountpoints := e.mgr.Mountpoints()
	statuses := make([]webui.MountStatus, 0, len(mountpoints))
	rootByMount := map[string]string{}
	for _, sm := range e.cfg.SceneryMounts {
		rootByMount[sm.Mount] = sm.Root
	}
	for _, mp := range mountpoints {
		statuses = append(statuses, webui.MountStatus{Root: rootByMount[mp], Mount: mp})
	}
	return statuses
}

func (e *runEngine) CachedTiles() int {
	return e.cacher.Len()
}

func (e *runEngine) OpenTiles() int {
	return e.cacher.OpenCount()
}

// fuseCmd mounts the trivial proof-of-concept filesystem (internal/fuse)
// and holds it until the context is cancelled (SIGINT/SIGTERM), then runs
// the same guaranteed-unmount sequence a real Stop/Shutdown action would
// use (DESIGN.md §4(c), §8): request unmount, wait for the serve loop to
// actually exit, log whether any file handles were still outstanding.
func fuseCmd(ctx context.Context, cfg *config.Config, args []string) error {
	if len(args) < 2 {
		printUsage()
		return fmt.Errorf("fuse: usage: autoortho fuse testmount <path> | autoortho fuse mount <root> <mountpoint>")
	}

	switch args[0] {
	case "testmount":
		mountpoint := args[1]

		fs := aofuse.NewTrivial()
		ctrl, err := aofuse.Mount(mountpoint, fs)
		if err != nil {
			return fmt.Errorf("mounting at %s: %w", mountpoint, err)
		}

		if err := ctrl.WaitMounted(2 * time.Second); err != nil {
			return fmt.Errorf("mount did not come up cleanly: %w", err)
		}
		fmt.Printf("Mounted trivial test filesystem at %s — try:\n  cat %s/hello.txt\nPress Ctrl+C to unmount and exit.\n", mountpoint, mountpoint)

		<-ctx.Done()
		fmt.Println("\nShutdown requested, unmounting...")

		if err := ctrl.Unmount(); err != nil {
			return fmt.Errorf("unmount failed: %w", err)
		}
		if n := fs.OutstandingOpens(); n != 0 {
			fmt.Printf("warning: %d file handle(s) were still open at unmount\n", n)
		}
		fmt.Println("Unmounted cleanly.")
		return nil

	case "mount":
		if len(args) < 3 {
			printUsage()
			return fmt.Errorf("fuse mount: usage: autoortho fuse mount <root> <mountpoint>")
		}
		root, mountpoint := args[1], args[2]

		maxWait := time.Duration(cfg.AutoOrtho.MaxWait * float64(time.Second))
		cacher := tile.NewCacher(cfg.Paths.CacheDir, cfg.AutoOrtho.MaxOpenTiles, maxWait, cfg.AutoOrtho.MaxConcurrentBuilds, cfg.AutoOrtho.MaxOpenTiles, cfg.AutoOrtho.FetchThreads)
		fs := aofuse.NewAutoOrtho(root, cacher, cfg.AutoOrtho.MinZoom, cfg.AutoOrtho.MaxZoom, cfg.AutoOrtho.MaptypeOverride)

		ctrl, err := aofuse.Mount(mountpoint, fs)
		if err != nil {
			return fmt.Errorf("mounting at %s: %w", mountpoint, err)
		}
		if err := ctrl.WaitMounted(2 * time.Second); err != nil {
			return fmt.Errorf("mount did not come up cleanly: %w", err)
		}
		fmt.Printf("Mounted %s at %s. Press Ctrl+C to unmount and exit.\n", root, mountpoint)

		<-ctx.Done()
		fmt.Println("\nShutdown requested, unmounting...")

		if err := ctrl.Unmount(); err != nil {
			return fmt.Errorf("unmount failed: %w", err)
		}
		if n := fs.OutstandingOpens(); n != 0 {
			fmt.Printf("warning: %d handle(s) were still open at unmount\n", n)
		}
		fmt.Println("Unmounted cleanly.")
		return nil

	default:
		printUsage()
		return fmt.Errorf("fuse: unknown subcommand %q", args[0])
	}
}

func sceneryCmd(ctx context.Context, cfg *config.Config, args []string) error {
	infoCache := filepath.Join(filepath.Dir(cfg.Paths.CacheDir), ".release_info")
	mgr, err := scenery.NewManager(cfg.Paths.SceneryPath, cfg.Paths.DownloadDir, infoCache)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("scenery: missing subcommand")
	}

	switch args[0] {
	case "list":
		if err := mgr.FindRegions(ctx); err != nil {
			return err
		}
		for id, region := range mgr.Regions {
			latest := region.GetLatestRelease()
			if latest == nil {
				continue
			}
			if err := latest.Parse(ctx, mgr.Client); err != nil {
				fmt.Printf("%-6s error parsing latest release: %v\n", id, err)
				continue
			}
			localInfo := ""
			if region.LocalRel != nil {
				localInfo = fmt.Sprintf(" (installed: %s)", region.LocalRel.Ver)
			}
			fmt.Printf("%-6s latest: %-10s size: %6.1f MB%s\n",
				id, latest.Ver, float64(latest.TotalSize)/1048576, localInfo)
		}
		return nil

	case "fetch":
		if len(args) < 2 {
			return fmt.Errorf("scenery fetch: missing region id")
		}
		if err := mgr.FindRegions(ctx); err != nil {
			return err
		}
		region, ok := mgr.Regions[args[1]]
		if !ok {
			return fmt.Errorf("unknown region %q", args[1])
		}
		ok2, err := region.InstallRelease(ctx, mgr.Client, "", func(p scenery.Progress) {
			fmt.Printf("\r%-60s %5.1f%% %6.2f MBps", p.Status, p.PercentDone, p.MBps)
		})
		fmt.Println()
		if err != nil {
			return err
		}
		if !ok2 {
			return fmt.Errorf("install failed for region %q", args[1])
		}
		fmt.Printf("Installed %s\n", args[1])
		return nil

	default:
		printUsage()
		return fmt.Errorf("scenery: unknown subcommand %q", args[0])
	}
}
