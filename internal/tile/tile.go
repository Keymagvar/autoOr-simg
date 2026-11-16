package tile

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"autoOr-simg/internal/logging"
	"autoOr-simg/internal/native"
	"autoOr-simg/internal/provider"
)

const (
	// GridSize is 4096px tile / 256px chunk = 16x16 chunks at native
	// zoom. Exported (not just package-internal) so internal/prefetch's
	// candidate-tile generator can align a raw lat/lon-derived tile
	// coordinate to the same 16-tile grid X-Plane's own DSF requests are
	// always aligned to, instead of silently duplicating this constant
	// and risking the two drifting apart.
	GridSize      = 16
	maxFetchDepth = 4 // hard structural limit (see quickZoom doc below), matches getortho.py's Tile.max_mipmap

	// defaultMaxWait mirrors aoconfig.py's autoortho.maxwait default
	// (0.5s) — used only if a Tile is built without an explicit MaxWait
	// (e.g. via NewTile directly in a test); real callers should set it
	// from config.
	defaultMaxWait = 500 * time.Millisecond
)

// chunkConcurrencyOverride, when >0, overrides every provider's own
// MaxThreads default (internal/provider's 24/12 split) for every Tile
// subsequently created via NewTile — set once at startup from
// config.AutoOrtho.ChunkConcurrency via SetChunkConcurrencyOverride, so
// this is a real, visible, user-tunable setting (the web UI's Zoom &
// quality block), not a number only the binary knows about.
var chunkConcurrencyOverride int

// SetChunkConcurrencyOverride sets the process-wide override applied to
// every new Tile's chunk-fetch concurrency (0 disables the override,
// falling back to each provider's own default). Not safe to call
// concurrently with NewTile — intended to be set once at startup, before
// any tiles are created, mirroring how config is loaded once before the
// Cacher starts serving requests.
func SetChunkConcurrencyOverride(n int) {
	chunkConcurrencyOverride = n
}

// Tile is one X-Plane .dds texture's worth of imagery: a GridSize x
// GridSize grid of provider chunks at the tile's native zoom, composited
// and BC1-compressed into a DDS mipmap chain lazily, on read. Mirrors
// getortho.py's Tile class.
type Tile struct {
	Row, Col, Zoom int
	Maptype        string
	MinZoom        int
	CacheDir       string

	DDS *DDS

	Provider provider.Provider
	Client   *http.Client

	// MaxWait bounds how long a single mipmap build will wait on chunk
	// fetches before giving up and compositing with whatever arrived —
	// the Go equivalent of aoconfig.py's autoortho.maxwait (DESIGN.md's
	// "port the maxwait deadline" item). Chunks still in flight when the
	// deadline passes are NOT cancelled: they keep fetching in the
	// background and populate the disk cache for next time, exactly like
	// getortho.py's ChunkGetter worker pool keeps working past a caller's
	// wait() timeout.
	MaxWait time.Duration

	// fetchPool is the shared, process-wide chunk-fetch worker pool (see
	// fetchpool.go) — set by Cacher.Open, like buildSem. nil is valid
	// (every existing test builds a Tile via struct literal, bypassing
	// Cacher entirely) and means "no shared pool" — fetchComposite falls
	// back to spawning a raw goroutine per chunk in that case, unbounded,
	// which is fine for the small grids every such test uses.
	fetchPool *FetchPool

	// lastRead is updated (UnixNano) at the top of every ReadDDSBytes call
	// — a true "last touched by X-Plane" timestamp, used by Cacher's LRU
	// content eviction (see onBuilt/dds.go's HasAnyData/ClearAllData) to
	// pick which built-but-not-recently-read tile to reclaim first. Not
	// "last successfully built" — a tile read often but rarely rebuilt
	// should NOT look evictable just because its last *build* was long
	// ago.
	lastRead atomic.Int64

	// onBuilt is called by buildMipmap after every real (non-degraded,
	// non-budget-skipped) successful write of new mipmap data — set by
	// Cacher.Open, like buildSem/fetchPool/overBudget. Lets the Cacher
	// enforce a cap on *cumulative* resident built tiles immediately when
	// a new one joins, not just periodically — without
	// this, MaxOpenTiles only ever gated new builds while instantaneously
	// over budget, never capped how many tiles could accumulate real
	// (~11MB each) resident data over a long session as the open-tile
	// count fluctuated below budget and back. nil is valid (every
	// existing test) and means "no cap enforced here".
	onBuilt func()

	Refs int // open-file refcount, managed by Cacher (cache.go)

	// fallbackChunkCount is incremented every time MaxWait is exceeded
	// and at least one chunk was still missing — DESIGN.md §7's "treat
	// every fallback-serve as a first-class, observable event". Exposed
	// via FallbackChunkCount(); not yet wired into a stats/web-UI
	// pipeline (that's still future work), but the signal exists now
	// rather than being silently invisible.
	fallbackChunkCount atomic.Int64

	// budgetDegradedCount counts how many times buildMipmap skipped a
	// real build because the Cacher was over its open-tile budget — the
	// memory-pressure counterpart to fallbackChunkCount's network-timing
	// one, exposed the same way (DESIGN.md §7 item 3's "every fallback
	// serve is a first-class, observable event" applies here too, not
	// just to the timeout case it was originally written for).
	budgetDegradedCount atomic.Int64

	// zoomDropCount counts how many times a mip0 build used a reduced
	// zoom (zoomDropForLoad's coverage-aware degradation) instead of this
	// tile's full native zoom, because buildSem was saturated with
	// concurrent builds at the time — the "too much ground wants full
	// detail at once" counterpart to fallbackChunkCount's network-timing
	// one and budgetDegradedCount's memory-pressure one, exposed the same
	// way. Exposed via ZoomDropCount().
	zoomDropCount atomic.Int64

	// buildSem bounds how many Tiles (across the whole Cacher that
	// created this one) may be simultaneously running BuildMipmap's
	// actual fetch/decode/composite/compress work — set by Cacher.Open
	// right after construction, shared by reference across every Tile
	// that Cacher creates. nil is a valid, deliberate state (direct
	// NewTile construction outside a Cacher, as every existing test in
	// this package does — maxwait_test.go even builds a Tile via struct
	// literal, bypassing NewTile entirely) and means "no cap" — see
	// BuildMipmap's acquire/release, which skips the semaphore entirely
	// when this is nil, rather than blocking forever on a nil channel.
	//
	// Why this exists at all: a real test flight (not a hypothetical)
	// drove process RSS to ~20GB within about ten minutes, because
	// nothing bounded how many tiles X-Plane's burst of simultaneous
	// opens could cause to start building at once — each one transiently
	// allocated a ~67MB RGBA composite buffer (4096x4096x4 bytes) plus
	// however many chunk-decode goroutines, and none of that was ever
	// throttled. internal/memwatch's RSS watchdog (DESIGN.md §9) is the
	// safety net for when this still isn't enough; this is the actual
	// fix for the common case, since it stops the growth instead of only
	// reacting to it.
	//
	// That 67MB figure is now stale for the common path specifically —
	// fetchComposeCompressed (below) builds the compressed output
	// directly and never allocates that composite at all, so a cap sized
	// for the old cost (config.AutoOrtho.MaxConcurrentBuilds, see its own
	// doc comment for the measured before/after) throttles the cheap
	// path far more tightly than it needs to, which — confirmed live —
	// pushes tiles into zoomDropForLoad's degraded path more than
	// necessary. That path still builds the full 64MB buffer
	// (cropScaleUp's upscale output), so an overly tight cap here doesn't
	// avoid the original memory risk, it recreates it via a different
	// route while also adding cropScaleUp's own real CPU cost on top.
	buildSem chan struct{}

	// overBudget is set by Cacher.Open to that Cacher's overBudget method
	// (shared by reference across every Tile it creates, like buildSem).
	// buildMipmap checks it before doing any real work:
	// when the Cacher has more open tiles than its configured budget,
	// buildMipmap skips the fetch/decode/composite/compress pipeline
	// entirely rather than adding another ~11MB of resident mipmap data,
	// leaving DDS.Read's existing §7 fallback-block path (already near-
	// zero memory, already network-free) to serve this tile for as long
	// as the budget stays exceeded. nil is valid (direct NewTile
	// construction, every existing test) and means "never over budget".
	overBudget func() bool

	mu  sync.Mutex
	log interface {
		Debug(msg string, args ...any)
		Info(msg string, args ...any)
		Warn(msg string, args ...any)
	}
}

// NewTile builds a Tile for the given native (row, col, zoom) — except
// when maxZoom>0 and zoom exceeds it: a "max zoom" cap (anon-requested
// feature, not in the Python original) that trades texture sharpness for
// less network/disk use by treating the request as if X-Plane had asked
// for a coarser native zoom in the first place. row/col are right-shifted
// by the same amount zoom is lowered — slippy-tile row/col numbering
// scales by exactly 2x per zoom level, so shifting both together keeps
// them pointing at the correct (now larger) geographic area; shifting
// zoom alone without row/col would silently fetch the wrong tile
// entirely. Every other calculation in this package (quickZoom, the
// mipmap chain, DDS sizing) already derives purely from Tile's own
// Row/Col/Zoom fields, so clamping once here — before any of that math
// ever runs — reuses all of it completely unchanged, rather than
// threading a second cap through every call site that already has to
// reconcile MinZoom.
func NewTile(row, col, zoom int, maptype string, minZoom, maxZoom int, cacheDir string, client *http.Client, maxWait time.Duration) (*Tile, bool) {
	p, ok := provider.Get(maptype)
	if !ok {
		return nil, false
	}
	if maxWait <= 0 {
		maxWait = defaultMaxWait
	}
	if maxZoom > 0 && zoom > maxZoom {
		shift := zoom - maxZoom
		row, col, zoom = row>>shift, col>>shift, maxZoom
	}
	// NOTE: chunkConcurrencyOverride/Provider.MaxThreads no longer bound
	// anything — fetchComposite now submits every chunk fetch to the
	// shared, process-wide FetchPool (fetchpool.go), sized by
	// config.AutoOrtho.FetchThreads, not a per-tile semaphore. Kept
	// (rather than deleted) as a documented no-op for config/API
	// backward-compat, not reused for anything else, to avoid a confusing
	// "it's set but silently does nothing different" trap — see
	// the "Berlin" finding for why the per-tile model was
	// replaced.
	t := &Tile{
		Row: row, Col: col, Zoom: zoom,
		Maptype: maptype, MinZoom: minZoom, CacheDir: cacheDir,
		Provider: p,
		Client:   client,
		MaxWait:  maxWait,
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      logging.For("tile"),
	}
	return t, true
}

// FallbackChunkCount returns how many times this tile has served a
// composite with at least one chunk still missing past MaxWait.
func (t *Tile) FallbackChunkCount() int64 {
	return t.fallbackChunkCount.Load()
}

// BudgetDegradedCount returns how many times this tile was served from the
// cheap fallback instead of a real build because the Cacher was over its
// open-tile memory budget.
func (t *Tile) BudgetDegradedCount() int64 {
	return t.budgetDegradedCount.Load()
}

// ZoomDropCount returns how many times this tile's mip0 build used a
// reduced zoom instead of full native zoom because buildSem was saturated
// — see zoomDropForLoad.
func (t *Tile) ZoomDropCount() int64 {
	return t.zoomDropCount.Load()
}

// quickZoom computes which provider zoom level and chunk-grid rectangle to
// fetch for a given mipmap index — a direct port of getortho.py's
// _get_quick_zoom as actually called from get_img (where its return value
// doubles as the *effective* zoom_diff, not simply min(mipmap,4)).
//
// maxFetchDepth=4 is not an arbitrary quality cutoff: at a 16x16 chunk
// grid, a zoom_diff of 5 would require fetching "a quarter of a chunk",
// which provider tile APIs can't serve (they only serve whole tiles at
// integer zoom levels) — 4 is the structural ceiling imposed by
// GridSize=16=2^4. Levels beyond this depth are reached not by fetching
// more, but by locally reducing the mipmap-4 image further (getMipmapChain
// below), exactly as the Python version's gen_mipmaps cascade does.
func (t *Tile) quickZoom(mipmap int) (col, row, w, h, zoom, zoomDiff int) {
	if mipmap == 0 {
		return t.Col, t.Row, GridSize, GridSize, t.Zoom, 0
	}
	maxDiff := min(mipmap, maxFetchDepth)
	minZoomFloor := max(t.Zoom-maxDiff, t.MinZoom)
	quickZoomLevel := max(t.Zoom-mipmap, minZoomFloor)
	zoomDiff = min(t.Zoom-quickZoomLevel, maxFetchDepth)

	return t.Col >> zoomDiff, t.Row >> zoomDiff, GridSize >> zoomDiff, GridSize >> zoomDiff, quickZoomLevel, zoomDiff
}

// zoomDropForLoad maps how full the shared buildSem currently is (active
// concurrent builds vs. its configured capacity — config.AutoOrtho.
// MaxConcurrentBuilds) to how many zoom levels a new mip0 build should drop
// below the tile's native zoom — a distance/coverage-aware zoom cap.
// buildSem occupancy is already, by construction,
// a process-wide measure of how much ground simultaneously wants full
// detail right now: a single close tile never saturates it, but a wide/
// dense view (many tiles opening mip0 at once, e.g. VKO at max_zoom=18)
// does. active/buildCap are read via len()/cap() on a possibly-nil channel
// at the call site, both of which are 0 for a nil channel, so buildCap<=0
// (no Cacher, e.g. every existing test's direct NewTile) always returns 0
// — unthrottled, matching today's behavior exactly.
//
// active is a buffered channel's len(), so it can never exceed buildCap —
// only 3 buckets are reachable, topping out at a drop of 2 (16 chunks, a
// 16x reduction from mip0's full 256, upscaled 4x) once buildSem is
// essentially full. That's deliberately short of maxFetchDepth's 4 (a
// single chunk upscaled 16x) — still recognizably sharp up close, not just
// "technically not a flat fallback color".
// zoomDropThreshold1/2 are the two buildSem-saturation fractions (as a
// ratio of active/buildCap) at which zoomDropForLoad steps down a drop
// level — a user-facing "Speed/Balanced/Quality" performance mode
// (SetPerformanceMode, config.AutoOrtho.PerformanceMode) picks between
// three preset pairs. Defaults (0.5/0.75) are "quality" mode, the exact
// thresholds this mechanism shipped with originally — an existing config
// with no performance_mode set gets byte-for-byte the same behavior as
// before this setting existed.
var (
	zoomDropThreshold1 = 0.5
	zoomDropThreshold2 = 0.75
)

// SetPerformanceMode sets the process-wide zoomDropForLoad thresholds from
// a user-facing preset name — called once at startup from
// config.AutoOrtho.PerformanceMode (cmd/autoortho/main.go), and live from
// the web UI's save handler.
// Unrecognized/empty input falls back to "quality" (today's original
// behavior) rather than leaving whatever the previous call set, so a typo
// in a hand-edited config file degrades safely instead of silently
// keeping a stale, possibly-surprising mode.
//
// "speed" and "balanced" drop to a coarser, cheaper mip0 fetch earlier
// under load (see zoomDropForLoad's own doc comment for what a drop
// actually costs/saves) — a direct, user-visible answer to "I'd rather
// have the view load than stay maximally sharp" for anyone flying dense
// cities or wide views, inspired by XPME (a closed-source competitor)
// shipping an analogous memory/quality toggle, independently confirming
// this exact tradeoff is one real users want control over, not just an
// internal auto-tuning knob.
func SetPerformanceMode(mode string) {
	switch mode {
	case "speed":
		zoomDropThreshold1, zoomDropThreshold2 = 0.2, 0.4
	case "balanced":
		zoomDropThreshold1, zoomDropThreshold2 = 0.35, 0.6
	default: // "quality", "", or anything unrecognized
		zoomDropThreshold1, zoomDropThreshold2 = 0.5, 0.75
	}
}

func zoomDropForLoad(active, buildCap int) int {
	if buildCap <= 0 {
		return 0
	}
	ratio := float64(active) / float64(buildCap)
	switch {
	case ratio < zoomDropThreshold1:
		return 0
	case ratio < zoomDropThreshold2:
		return 1
	default:
		return 2
	}
}

// fetchComposite fetches every chunk in the (col,row,w,h) grid at the
// given provider zoom level and composites them into one RGBA image.
//
// Two distinct fallback mechanisms are both in play here, and it matters
// which one fired: a chunk that errors (bad response, decode failure) is
// simply skipped, leaving its region as the flat fallback color — that's
// permanent for this attempt. A chunk that's merely *slow* is different:
// once MaxWait elapses, fetchComposite stops waiting and returns with
// whatever arrived, but the still-in-flight fetch goroutines are NOT
// cancelled (ctx here is the long-lived caller context, not a child
// scoped to this call) — they keep running and write to the disk cache on
// completion, so a subsequent read of this same tile finds a warm cache
// instead of repeating the slow fetch. This is the direct Go equivalent of
// getortho.py's ChunkGetter worker pool continuing past a caller's wait()
// timeout (DESIGN.md's "port the maxwait deadline" item).
//
// They do NOT, however, keep decoding once abandoned (see fetchOne's
// `abandoned` check): the disk-cache write already happened inside
// chunk.Fetch, so decoding into a full RGBA buffer for a result nobody will
// read is pure waste. Omitting that check is exactly what caused a real OOM
// crash (the Berlin case): under heavy load the fixed-size
// FetchPool's backlog of already-abandoned jobs decoded anyway, and a pprof
// heap profile taken moments before the kill showed DecodeJPEGTurbo alone
// holding ~30GB of the ~24.5GB-RSS process's heap.
func (t *Tile) fetchComposite(ctx context.Context, col, row, w, h, zoom int) (*native.RGBAImage, bool) {
	composite := newRGBA(w*256, h*256, fallbackColor)
	if w == 0 || h == 0 {
		return composite, false
	}
	// allFresh (fetchGrid's second return) isn't needed here — only
	// fetchComposeCompressed's mip0 path feeds the BC1 persistent cache
	// (bc1cache.go), which is what that signal exists for.
	degraded, _ := t.fetchGrid(ctx, col, row, w, h, zoom, func(c, r int, img *native.RGBAImage) {
		if img == nil {
			// Confirmed "no data at this zoom" (see fetchOne's doc
			// comment) — leave composite's pre-filled fallback color
			// for this cell rather than pasting nothing.
			return
		}
		// Disjoint regions of composite.Pix per (c, r): safe to write
		// without a lock even though other goroutines are still running
		// concurrently.
		paste(composite, img, c*256, r*256)
	})
	return composite, degraded
}

// fetchGrid is fetchComposite's shared core, factored out so
// fetchComposeCompressed (below) can reuse the exact same fetch/timeout/
// retry/backup-chunk logic while writing results somewhere other than an
// RGBA composite — every chunk that arrives (real or a getBestCachedChunk
// stand-in), plus every cell fillMissingWithBackups manages to fill after a
// timeout, is handed to onResult instead of being pasted directly. Returns
// whether the build was degraded (anything missing/fallback at the end),
// and separately whether *every* cell came from a genuine, fresh,
// successful network fetch — no backup stand-in, no confirmed-empty
// chunk, no timeout fallback. The two are different questions:
// "not degraded" only means nothing was *missing* at the end (a backup
// stand-in or a confirmed-empty cell both count as present, not missing),
// while "allFresh" is the stronger guarantee buildMipmap's BC1 persistent
// cache (bc1cache.go) needs — caching a build that quietly includes a
// blurry stand-in or a possibly-stale no-coverage result would freeze
// that imperfection in place across restarts too, on top of the fact that
// a degraded build already stays degraded for this Tile's lifetime (no
// background repair exists — see BuildMipmap's doc comment).
func (t *Tile) fetchGrid(ctx context.Context, col, row, w, h, zoom int, onResult func(c, r int, img *native.RGBAImage)) (degraded, allFresh bool) {
	type result struct {
		c, r int
		img  *native.RGBAImage
	}
	total := w * h
	results := make(chan result, total)
	receivedGrid := make([]bool, total)

	// Set once fetchComposite stops waiting (timeout or ctx.Done), so
	// fetchOne calls still running at that point can skip the decode
	// step below — see its own comment for why this matters.
	var abandoned atomic.Bool
	var freshCount atomic.Int64

	fetchOne := func(c, r int) {
		chunkCol, chunkRow := col+c, row+r
		chunk := &Chunk{Col: chunkCol, Row: chunkRow, Zoom: zoom, Maptype: t.Maptype, CacheDir: t.CacheDir}
		data, err := chunk.Fetch(ctx, t.Client, t.Provider, (col+c)%t.Provider.NumServers)
		if err != nil {
			t.log.Warn("chunk fetch failed, trying coarser cached chunk", "col", chunk.Col, "row", chunk.Row, "zoom", chunk.Zoom, "err", err)
			// A hard failure, not a slow-but-in-flight fetch: nothing will
			// warm the cache for this cell later, so try right now rather
			// than leaving flat fallback color for the rest of this
			// attempt (see getBestCachedChunk's doc comment).
			if !abandoned.Load() {
				if backup, ok := t.getBestCachedChunk(chunkCol, chunkRow, zoom); ok {
					results <- result{c, r, backup}
				}
			}
			return
		}
		if data == nil {
			// Chunk.Fetch's confirmed "provider has no data here" outcome
			// (nil, nil — see its own doc comment): resolved, not a
			// failure. Sent through as a nil-image result so this cell
			// counts as received (fetchGrid's receivedGrid, so neither
			// the MaxWait timeout path nor fillMissingWithBackups treats
			// it as missing) without searching for a coarser backup —
			// there's nothing wrong to work around, this ground simply
			// has no imagery at this zoom. onResult callers (fetchComposite/
			// fetchComposeCompressed) leave their pre-filled fallback
			// color in place for a nil image. This is the fix for the
			// repeated "giving up on background repair" spam a confirmed-
			// no-coverage spot used to cause: treating it as an error
			// here made every such cell count toward "degraded" and
			// re-triggered the repair-retry loop, re-asking the provider
			// the same already-answered question every few seconds.
			if !abandoned.Load() {
				results <- result{c, r, nil}
			}
			return
		}
		// chunk.Fetch already wrote the raw bytes to the disk cache
		// above, so a subsequent read of this tile is warm either way.
		// Decoding into a full RGBA buffer is only useful to composite
		// into THIS call's result — if the caller has already given up
		// waiting, nobody will ever read it, so skip the decode
		// entirely. Without this check, a backlog of abandoned jobs
		// under heavy load (the Berlin crash) each still
		// allocate and hold a full decoded chunk for no reason, which is
		// exactly what drove DecodeJPEGTurbo's inuse_space to ~30GB (of
		// a ~24.5GB total RSS) in a pprof heap profile taken moments
		// before a real OOM-kill.
		if abandoned.Load() {
			return
		}
		img, err := t.DDS.Decoder.Decode(data)
		if err != nil {
			t.log.Warn("chunk decode failed, trying coarser cached chunk", "col", chunk.Col, "row", chunk.Row, "err", err)
			if !abandoned.Load() {
				if backup, ok := t.getBestCachedChunk(chunkCol, chunkRow, zoom); ok {
					results <- result{c, r, backup}
				}
			}
			return
		}
		freshCount.Add(1)
		results <- result{c, r, img}
	}

	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			c, r := c, r
			if t.fetchPool != nil {
				// Submitted to the shared, process-wide pool (fetchpool.go)
				// as plain FIFO (priority 0 for every job) — NOT
				// priority-by-zoom, which an earlier version of this code
				// did and which caused a real, confirmed starvation bug
				// (the second "Berlin" finding): coarse/distant
				// mipmap fetches (lower zoom) always outrank mip0 (higher
				// zoom) under that scheme, and under continuous real load
				// — dozens of tiles perpetually resubmitting coarse-mip
				// work as X-Plane's view keeps shifting — the coarse-
				// priority queue never actually empties, so mip0 (what's
				// directly under/near the camera) never gets serviced at
				// all. Confirmed directly in logs: every single
				// "giving up" event was mipmap=0, with near-total 256/256
				// misses, for the entire length of a real flight. Pure
				// FIFO has no such failure mode: every job is served in
				// submission order, so nothing can structurally starve
				// forever, which matters far more here than Python-style
				// zoom prioritization (worth revisiting later with an
				// aging/fairness scheme if coarse-first ordering turns out
				// to matter for quality — not attempted yet).
				t.fetchPool.Submit(0, func() { fetchOne(c, r) })
			} else {
				// No shared pool configured (every existing test builds a
				// Tile via struct literal, bypassing Cacher) — unbounded,
				// fine for the small grids those tests use.
				go fetchOne(c, r)
			}
		}
	}

	timer := time.NewTimer(t.MaxWait)
	defer timer.Stop()

	received := 0
	for received < total {
		select {
		case res := <-results:
			onResult(res.c, res.r, res.img)
			receivedGrid[res.r*w+res.c] = true
			received++
		case <-timer.C:
			missing := total - received
			t.fallbackChunkCount.Add(1)
			t.log.Warn("maxwait exceeded, serving fallback for remaining chunks",
				"missing", missing, "total", total, "maxwait", t.MaxWait)
			abandoned.Store(true)
			t.fillMissingWithBackups(onResult, receivedGrid, col, row, w, h, zoom)
			return true, false
		case <-ctx.Done():
			abandoned.Store(true)
			t.fillMissingWithBackups(onResult, receivedGrid, col, row, w, h, zoom)
			return true, false
		}
	}
	return false, freshCount.Load() == int64(total)
}

// fetchComposeCompressed is fetchComposite's memory-lean sibling, used only
// for mipmap 0 (the full GridSize x GridSize grid — up to 256 chunks,
// 4096x4096 output). Instead of assembling one ~64MB RGBA composite and
// compressing it in a single call at the end, each chunk's decoded RGBA is
// compressed to BC1 (32KB for one 256x256 chunk, not 64MB for the whole
// tile) and stitched directly into its final position the moment it
// arrives — see stitchBlocks's doc comment for why this is byte-identical
// to the old whole-composite-then-compress approach, not an approximation.
//
// This exists because of real, repeated evidence from the Moscow/VKO
// investigation: every pprof heap snapshot taken during
// heavy sustained load showed internal/tile.newRGBA holding ~1.1-1.3GB —
// dozens of full 64MB mip0 composites simultaneously in flight, each held
// for the composite's *entire* fetch duration (which can run seconds under
// contention, since every one of up to 256 chunks must arrive before the
// old code could compress anything). Peak memory per in-flight mip0 build
// drops from ~72MB (64MB RGBA + 8MB BC1 output) to roughly the 8MB BC1
// output plus whatever chunks are actively decoding/compressing at that
// instant — bounded by FetchPool/buildSem concurrency, not by the tile's
// total chunk count.
func (t *Tile) fetchComposeCompressed(ctx context.Context, col, row, w, h, zoom int) (data []byte, degraded, allFresh bool) {
	blocksWide := w * 256 / 4
	blocksHigh := h * 256 / 4
	out := make([]byte, blocksWide*blocksHigh*8)

	// Pre-fill every block with the flat fallback color, compressed once —
	// mirrors fetchComposite's newRGBA(..., fallbackColor) pre-fill, just
	// operating on compressed blocks instead of pixels. Cells that never
	// get a real or backup chunk are left as this.
	fallbackChunk := newRGBA(256, 256, fallbackColor)
	if fallbackCompressed, err := t.DDS.Compressor.CompressBC1(fallbackChunk.Pix, 256, 256); err == nil {
		for r := 0; r < h; r++ {
			for c := 0; c < w; c++ {
				stitchBlocks(out, blocksWide, c*64, r*64, fallbackCompressed, 256, 256)
			}
		}
	}
	if w == 0 || h == 0 {
		return out, false, false
	}

	degraded, allFresh = t.fetchGrid(ctx, col, row, w, h, zoom, func(c, r int, img *native.RGBAImage) {
		if img == nil {
			// Confirmed "no data at this zoom" (see fetchOne's doc
			// comment) — leave out's pre-filled fallback-color block
			// for this cell rather than compressing nothing.
			return
		}
		compressed, err := t.DDS.Compressor.CompressBC1(img.Pix, img.Width, img.Height)
		if err != nil {
			t.log.Warn("compress failed for chunk, leaving fallback color", "col", col+c, "row", row+r, "err", err)
			return
		}
		stitchBlocks(out, blocksWide, c*(img.Width/4), r*(img.Height/4), compressed, img.Width, img.Height)
	})
	return out, degraded, allFresh
}

// disableCoarseFallback, when true, makes getBestCachedChunk always report
// "nothing found" so missing chunks fall through to the flat fallbackColor
// instead of a blurry coarser-zoom stand-in. Real flight observation
// (2026-11-14): the blurry stand-in reads as a pleasant soft-focus effect
// over land, but over water — where low-zoom provider imagery is often a
// washed-out, algae-tinted blue-green at the zoom levels this searches —
// it reads as visibly "dirty" rather than blurry. Exposed as a real,
// user-tunable setting (config.AutoOrtho.DisableCoarseFallback, the web
// UI's Zoom & quality block) rather than removed outright, since the
// blurry stand-in is still the better default for land-heavy flying. A
// var so tests can toggle it.
var disableCoarseFallback bool

// SetDisableCoarseFallback overrides disableCoarseFallback process-wide —
// called once at startup, and live from a web UI save, from
// config.AutoOrtho.DisableCoarseFallback (see disableCoarseFallback's doc
// comment).
func SetDisableCoarseFallback(disabled bool) {
	disableCoarseFallback = disabled
}

// getBestCachedChunk searches progressively coarser provider zoom levels
// (closest first, up to maxFetchDepth) for an already-disk-cached chunk
// covering the same ground area as (col, row) at zoom, and if found, crops
// out the matching sub-region and nearest-neighbor-scales it back up to a
// full chunk — a blurry but geographically-correct stand-in instead of a
// flat fallback color. Never touches the network (Chunk.GetCached only).
// Always reports "not found" when disableCoarseFallback is set — see its
// doc comment.
//
// Mirrors getortho.py's Tile.get_best_chunk, a real quality feature the Go
// port had never ported: without it, any chunk that's merely slow or that
// hard-fails shows as a flat solid-color patch until the next successful
// build, producing visibly blocky tile boundaries during heavy load — the
// Python version instead shows a soft, blurry (but correctly-positioned)
// zoomed-in placeholder, which reads as normal progressive loading instead
// of a glitch.
func (t *Tile) getBestCachedChunk(col, row, zoom int) (*native.RGBAImage, bool) {
	if disableCoarseFallback {
		return nil, false
	}
	for diff := 1; diff <= maxFetchDepth; diff++ {
		zoomP := zoom - diff
		if zoomP < t.MinZoom {
			break
		}
		scale := 1 << diff
		chunk := &Chunk{Col: col >> diff, Row: row >> diff, Zoom: zoomP, Maptype: t.Maptype, CacheDir: t.CacheDir}
		data, ok := chunk.GetCached()
		if !ok {
			continue
		}
		img, err := t.DDS.Decoder.Decode(data)
		if err != nil {
			continue
		}
		w, h := 256/scale, 256/scale
		colOffset, rowOffset := col%scale, row%scale
		return cropScaleUp(img, colOffset*w, rowOffset*h, w, h, scale), true
	}
	return nil, false
}

// fillMissingWithBackups paints every (c, r) cell that never arrived in
// time with getBestCachedChunk's stand-in, if one exists on disk — called
// once, synchronously, right as fetchComposite gives up (timeout or
// ctx.Done), bounded by this call's own (w, h) grid size, not a repeated
// background operation.
func (t *Tile) fillMissingWithBackups(onResult func(c, r int, img *native.RGBAImage), receivedGrid []bool, col, row, w, h, zoom int) {
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			if receivedGrid[r*w+c] {
				continue
			}
			if backup, ok := t.getBestCachedChunk(col+c, row+r, zoom); ok {
				onResult(c, r, backup)
			}
		}
	}
}

// BuildMipmap fetches and compresses the data needed to satisfy a read
// touching mipmap level idx, matching getortho.py's Tile.get_mipmap: mip 0
// is built alone (no cascade — a full-tile fetch is expensive enough that
// cascading further isn't done opportunistically); any other requested
// level triggers a fetch at the structural depth limit (maxFetchDepth)
// followed by a local reduce cascade all the way down to the DDS's
// SmallestRealMipmap, populating every intermediate level with real
// (reduced, not fetched) data along the way — the same thing §2a's
// replication does for the few levels below that.
// BuildMipmap's one short-circuit: once a mipmap is Retrieved — even if
// the build that set it was degraded (fallback color or a blurry stand-in
// baked into the composite before compression, see fetchComposite) — a
// later read of this same open Tile never rebuilds it. No background
// repair exists to bypass that (removed by design): a
// degraded tile stays degraded until this Tile is evicted and reopened
// fresh, same as Python AutoOrtho.
func (t *Tile) BuildMipmap(ctx context.Context, idx int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.DDS.IsRetrieved(idx) {
		return
	}

	// Memory budget (see overBudget's doc comment): if the
	// Cacher has more tiles open right now than it's budgeted for, skip
	// the real build entirely rather than add another ~11MB of resident
	// mipmap data on top of an already-over-budget process. DDS.Read's
	// existing §7 fallback-block path serves this tile in the meantime at
	// near-zero memory cost. Checked on every call (not cached once per
	// Tile), so as soon as enough other tiles close and the budget clears,
	// this same tile's very next read gets a real build instead of needing
	// to be closed and reopened first.
	if t.overBudget != nil && t.overBudget() {
		t.budgetDegradedCount.Add(1)
		// Warn, not Debug: this is the §7 fallback-block path (DDS.Read
		// serves tileFallback for a mipmap that was never built at all) —
		// the proof-flight's degraded-tile count needs this visible at the
		// default log level, same as the maxwait path's own Warn line
		// below. Debug was invisible there.
		t.log.Warn("over open-tile budget, skipping build and serving fallback", "mipmap", idx)
		return
	}

	// Persistent BC1 cache fast path (bc1cache.go, mip0 only for now —
	// see its doc comment): checked after overBudget (a cache hit still
	// allocates the same ~11MB of resident mipmap data as a fresh build,
	// so it must respect the same memory bound) but before buildSem,
	// since a cache hit needs no network/decode/compress work at all and
	// so has no reason to wait for — or occupy — a concurrent-build slot.
	if idx == 0 {
		if cached, ok := t.loadBC1Cache(0); ok {
			t.DDS.SetMipmapData(0, cached)
			if t.onBuilt != nil {
				t.onBuilt()
			}
			return
		}
	}

	// Concurrent-build cap (see buildSem's doc comment): wait for a slot
	// before doing any of the actual fetch/decode/composite/compress
	// work below. A nil buildSem (direct NewTile construction, every
	// existing test in this package) means no cap — skip the wait
	// entirely rather than blocking forever on a nil channel receive.
	// Acquired/released around the whole build, not just fetchComposite,
	// since the compress step also holds real memory (the compressed
	// output plus, until GC reclaims it, the composite it was built
	// from).
	if t.buildSem != nil {
		select {
		case t.buildSem <- struct{}{}:
			defer func() { <-t.buildSem }()
		case <-ctx.Done():
			t.log.Warn("context cancelled while waiting for a build slot", "mipmap", idx)
			return
		}
	}

	if idx == 0 {
		var data []byte
		var degraded, allFresh bool

		if drop := zoomDropForLoad(len(t.buildSem), cap(t.buildSem)); drop <= 0 {
			col, row, w, h, zoom, _ := t.quickZoom(0)
			data, degraded, allFresh = t.fetchComposeCompressed(ctx, col, row, w, h, zoom)
		} else {
			// Coverage-aware degradation (the VKO throughput finding):
			// buildSem is already saturated with
			// concurrent builds, meaning a lot of ground wants full detail
			// at once (a wide/dense view, not a single close tile) — fetch
			// a coarser, far cheaper grid via the same quickZoom math every
			// other mipmap already uses, then upscale it to mip0's full
			// size instead of either succeeding at full cost or hitting
			// overBudget's all-or-nothing fallback-color cliff. This result
			// still gets marked degraded internally (fetchComposite's return
			// value), it just has no retry mechanism anymore — same as any
			// other degraded build, it stays at this reduced zoom until the
			// Tile is evicted and reopened fresh.
			col, row, w, h, zoom, zoomDiff := t.quickZoom(drop)
			img, _ := t.fetchComposite(ctx, col, row, w, h, zoom)
			if zoomDiff > 0 {
				img = cropScaleUp(img, 0, 0, img.Width, img.Height, 1<<zoomDiff)
			}
			compressed, err := t.DDS.Compressor.CompressBC1(img.Pix, img.Width, img.Height)
			if err != nil {
				t.log.Warn("compress failed (reduced-zoom mip0 build)", "err", err)
				return
			}
			data, degraded = compressed, true
			t.zoomDropCount.Add(1)
			t.log.Debug("mip0 build used reduced zoom (coverage-aware degradation)", "drop", drop, "actualZoomDiff", zoomDiff)
		}

		if !degraded && allFresh {
			t.saveBC1Cache(0, data)
		}

		t.DDS.SetMipmapData(0, data)
		if t.onBuilt != nil {
			t.onBuilt()
		}
		return
	}

	fetchIdx := min(idx, maxFetchDepth)
	col, row, w, h, zoom, _ := t.quickZoom(fetchIdx)
	img, _ := t.fetchComposite(ctx, col, row, w, h, zoom)

	// Reconcile composite size against what fetchIdx actually expects: when
	// MinZoom's floor forces quickZoom to settle for a coarser provider
	// zoom than fetchIdx alone would call for (zoomDiff < fetchIdx), the
	// fetched grid comes back *larger* than this mipmap level's pixel
	// size — e.g. mipmap 4 wants 256x256, but a MinZoom clamp can leave us
	// with a 1024x1024 4x4-chunk composite instead. Without reducing that
	// down first, compressing it directly produces a BC1 buffer sized for
	// the wrong dimensions, which reads back as sheared/banded garbage —
	// this is a direct port of gen_mipmaps' "initial reduction" step in
	// pydds.py, which exists for exactly this reconciliation.
	desiredWidth := t.DDS.Width >> fetchIdx
	for img.Width > desiredWidth {
		img = reduce2(img)
	}

	for mm := fetchIdx; mm <= t.DDS.SmallestRealMipmap; mm++ {
		data, err := t.DDS.Compressor.CompressBC1(img.Pix, img.Width, img.Height)
		if err != nil {
			t.log.Warn("compress failed", "mipmap", mm, "err", err)
			return
		}
		t.DDS.SetMipmapData(mm, data) // auto-replicates the rest at SmallestRealMipmap
		if mm == t.DDS.SmallestRealMipmap {
			break
		}
		img = reduce2(img)
	}
	if t.onBuilt != nil {
		t.onBuilt()
	}
}

// ReadDDSBytes is the FUSE read() entry point: ensure the mipmap(s)
// covering [offset, offset+length) are built, then return the bytes.
// Mirrors getortho.py's Tile.read_dds_bytes dispatch logic.
func (t *Tile) ReadDDSBytes(ctx context.Context, offset, length int) []byte {
	t.lastRead.Store(time.Now().UnixNano())

	mm := t.DDS.FindMipmap(offset)

	switch {
	case offset == 0:
		t.BuildMipmap(ctx, 0)
	case offset < 131072: // deep enough into mipmap 0 to just build the whole thing
		t.BuildMipmap(ctx, 0)
	case offset+length < mm.EndPos:
		t.BuildMipmap(ctx, mm.Idx)
	default:
		t.BuildMipmap(ctx, mm.Idx)
		if mm.Idx+1 < len(t.DDS.Mipmaps) {
			t.BuildMipmap(ctx, mm.Idx+1)
		}
	}

	return t.DDS.Read(offset, length)
}
