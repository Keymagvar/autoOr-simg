package tile

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"autoOr-simg/internal/logging"
)

// Cacher is a process-wide registry of open Tiles, refcounted by FUSE
// open/close (once wired up — AGENTS.md migration step 6, not yet done).
// Mirrors getortho.py's TileCacher, with a simplified eviction policy for
// this first pass: a fixed tile-count cap rather than Python's RSS-based
// eviction loop — DESIGN.md §9's memory watchdog is the intended long-term
// answer to real memory pressure; this is just a basic bound so an
// unbounded cache isn't the starting point.
type Cacher struct {
	mu            sync.Mutex
	tiles         map[string]*Tile
	order         []string // insertion order, oldest first, for eviction
	maxTiles      int
	CacheDir      string
	DefaultClient *http.Client
	MaxWait       time.Duration
	log           slogLogger

	// buildSem bounds how many Tiles created by this Cacher may be
	// simultaneously running BuildMipmap's actual fetch/decode/composite/
	// compress work — see tile.go's doc comment on Tile.buildSem for why
	// this exists: a real test flight drove RSS to ~20GB because nothing
	// capped how many tiles X-Plane could cause to build at once. Shared
	// by reference across every Tile this Cacher creates (set once here,
	// not per-Tile), so the limit is process-wide for this Cacher, not
	// per-tile.
	//
	// Its job narrowed once fetchPool (below) took over bounding chunk
	// *fetch* concurrency: buildSem now only bounds how many ~67MB
	// transient RGBA composite buffers can exist at once, a pure memory
	// concern, not a network-fairness one — which is why its default
	// could be raised well above the original 8 (see
	// defaultMaxConcurrentBuilds) now that the periodic GC/FreeOSMemory
	// reclaim (cmd/autoortho/main.go's Soft-stage callback) makes a higher
	// transient peak safe to absorb.
	buildSem chan struct{}

	// fetchPool is the shared, process-wide chunk-fetch worker pool
	// (fetchpool.go) — the Go port's equivalent of getortho.py's
	// ChunkGetter. Created once here, shared by reference across every
	// Tile this Cacher creates (set in Open, like buildSem), sized by
	// config.AutoOrtho.FetchThreads. This — not buildSem, not a per-tile
	// semaphore — is what actually bounds how many chunk fetches run at
	// once process-wide, and does so fairly across every tile currently
	// building, not just whichever ones buildSem let through. See
	// the "Berlin" finding for the failure this replaced.
	fetchPool *FetchPool

	// openCount is the number of tiles currently open (Refs>0), tracked
	// independently of len(c.tiles) (which also includes idle, evictable
	// tiles). This is the quantity that actually drove
	// the real OOM finding — neither maxTiles nor buildSem bounds it.
	// Updated atomically on every Refs 0<->positive transition in
	// Open/Close (cheap enough to check on every FUSE read via
	// Tile.overBudget, unlike a scan over c.tiles under c.mu).
	openCount atomic.Int64

	// maxOpenTiles is the budget openCount is checked against — see
	// config.AutoOrtho.MaxOpenTiles's doc comment for the full rationale.
	// Shared by reference (via the overBudget closure) across every Tile
	// this Cacher creates, exactly like buildSem.
	maxOpenTiles int
}

type slogLogger = interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

// defaultMaxConcurrentBuilds is used when NewCacher is given <= 0.
// Raised from an original 8 to 32 once fetchPool (not buildSem) became
// what actually bounds chunk-fetch concurrency and fairness — buildSem's
// remaining job is purely capping simultaneous ~67MB transient composite
// buffers, and the periodic GC/FreeOSMemory reclaim makes a higher
// transient peak safe. config.AutoOrtho.MaxConcurrentBuilds is the real,
// tunable knob in production; this is only the fallback when that's left
// unset (e.g. in tests, or direct NewCacher use).
const defaultMaxConcurrentBuilds = 32

// defaultMaxOpenTiles mirrors config.AutoOrtho.MaxOpenTiles' default — used
// when NewCacher is given <= 0 (e.g. tests, or direct NewCacher use outside
// the configured production path).
const defaultMaxOpenTiles = 300

// defaultFetchThreads mirrors config.AutoOrtho.FetchThreads' own default —
// used when NewCacher is given <= 0.
const defaultFetchThreads = 32

func NewCacher(cacheDir string, maxTiles int, maxWait time.Duration, maxConcurrentBuilds, maxOpenTiles, fetchThreads int) *Cacher {
	if maxTiles <= 0 {
		maxTiles = 25 // matches getortho.py's default cache_tile_lim
	}
	if maxConcurrentBuilds <= 0 {
		maxConcurrentBuilds = defaultMaxConcurrentBuilds
	}
	if maxOpenTiles <= 0 {
		maxOpenTiles = defaultMaxOpenTiles
	}
	if fetchThreads <= 0 {
		fetchThreads = defaultFetchThreads
	}
	return &Cacher{
		tiles:         map[string]*Tile{},
		maxTiles:      maxTiles,
		CacheDir:      cacheDir,
		fetchPool:     NewFetchPool(fetchThreads),
		DefaultClient: &http.Client{Timeout: 0}, // see DESIGN.md's http.Client.Timeout lesson — no blanket deadline on what may be a slow tile fetch
		MaxWait:       maxWait,
		log:           logging.For("tilecache"),
		buildSem:      make(chan struct{}, maxConcurrentBuilds),
		maxOpenTiles:  maxOpenTiles,
	}
}

// overBudget reports whether the number of currently-open tiles is at or
// over this Cacher's maxOpenTiles budget — shared by reference with every
// Tile this Cacher creates (set in Open, like buildSem) so buildMipmap can
// cheaply re-check it on every call without touching c.mu.
func (c *Cacher) overBudget() bool {
	return c.openCount.Load() >= int64(c.maxOpenTiles)
}

func tileID(row, col int, maptype string, zoom int) string {
	return fmt.Sprintf("%d_%d_%s_%d", row, col, maptype, zoom)
}

// Open returns the Tile for (row, col, maptype, zoom), creating it if
// necessary, and increments its refcount — mirrors TileCacher._open_tile.
// The cache key is always the raw (row, col, zoom) FUSE actually parsed
// from the requested path, never the maxZoom-capped values NewTile may
// internally substitute — Get/Close (called later for the same open file
// handle) look up by that same raw key, so it must stay stable regardless
// of any capping NewTile does underneath.
func (c *Cacher) Open(row, col int, maptype string, zoom, minZoom, maxZoom int) (*Tile, bool) {
	id := tileID(row, col, maptype, zoom)

	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tiles[id]
	if !ok {
		var created bool
		t, created = NewTile(row, col, zoom, maptype, minZoom, maxZoom, c.CacheDir, c.DefaultClient, c.MaxWait)
		if !created {
			return nil, false
		}
		t.buildSem = c.buildSem
		t.fetchPool = c.fetchPool
		t.overBudget = c.overBudget
		t.onBuilt = c.onTileBuilt
		c.tiles[id] = t
		c.order = append(c.order, id)
		// Increment Refs BEFORE evictLocked, not after: evictLocked scans
		// for any tile with Refs<=0, and the tile just inserted above is
		// sitting right there in c.order/c.tiles with Refs still at its
		// zero value until the increment below runs. Under real load this
		// isn't theoretical — found live via a real-mount stress test with
		// more concurrently-open tiles than maxTiles: when every older
		// tile is genuinely still in use (Refs>0, nothing else evictable),
		// evictLocked's scan reaches the end of c.order and evicts the
		// brand-new tile it just walked past, right after Open() created
		// it and before Open() ever got to protect it with a ref — the
		// caller still gets back a valid *Tile (so BuildMipmap works
		// fine), but it's silently no longer in the Cacher's map, so the
		// very next Cacher.Get (every FUSE Read call) or Cacher.Close
		// (FUSE Release) for that id fails ("close of unknown tile",
		// "read: dds tile not open") even though the caller's handle is
		// still technically "open" from its own point of view.
		t.Refs++
		c.openCount.Add(1)
		c.evictLocked()
		return t, true
	}
	if t.Refs == 0 {
		c.openCount.Add(1)
	}
	t.Refs++
	return t, true
}

// Get returns the already-open Tile for (row, col, maptype, zoom) without
// creating it or touching its refcount — mirrors TileCacher._get_tile, used
// by the FUSE read() path (Open already called Open above and holds the
// ref; Read just needs the existing *Tile on every call).
func (c *Cacher) Get(row, col int, maptype string, zoom int) (*Tile, bool) {
	id := tileID(row, col, maptype, zoom)

	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tiles[id]
	return t, ok
}

// Close decrements the tile's refcount. Mirrors TileCacher._close_tile;
// unlike the Python version's cache-enabled delayed close, tiles stay
// resident until the eviction pass removes them (simplification noted
// above), so Close here only does the refcount bookkeeping.
func (c *Cacher) Close(row, col int, maptype string, zoom int) {
	id := tileID(row, col, maptype, zoom)

	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tiles[id]
	if !ok {
		c.log.Warn("close of unknown tile", "id", id)
		return
	}
	t.Refs--
	if t.Refs == 0 {
		c.openCount.Add(-1)
	}
}

// OpenCount reports how many tiles are currently open (Refs>0) — the
// quantity the open-tile memory budget fix bounds via maxOpenTiles. Diagnostic/stats
// use, same purpose as Len() but for the number that actually matters for
// memory (an idle cached tile costs nothing extra once evictable).
func (c *Cacher) OpenCount() int {
	return int(c.openCount.Load())
}

// BuiltContentCount reports how many currently-cached tiles hold real
// built mipmap data right now (DDS.HasAnyData()) — the quantity
// evictLRUContentLocked actually bounds at maxOpenTiles, distinct from
// both Len() (includes idle/empty tiles) and OpenCount() (an open tile
// may not have finished building yet, or may have had its content
// reclaimed while still open). Diagnostic/stats use, and the basis for
// this package's own tests proving the cap holds over many build/evict
// cycles.
func (c *Cacher) BuiltContentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.tiles {
		if t.DDS.HasAnyData() {
			n++
		}
	}
	return n
}

// evictLocked drops the oldest tiles with no outstanding refs once the
// cache exceeds maxTiles. Caller must hold c.mu.
func (c *Cacher) evictLocked() {
	for len(c.tiles) > c.maxTiles {
		evicted := false
		for i, id := range c.order {
			t, ok := c.tiles[id]
			if !ok || t.Refs > 0 {
				continue
			}
			delete(c.tiles, id)
			c.order = append(c.order[:i], c.order[i+1:]...)
			evicted = true
			break
		}
		if !evicted {
			return // everything still referenced; nothing more to evict
		}
	}
}

// ForceEvictIdle drops every currently cached tile with no outstanding
// refs, regardless of maxTiles — DESIGN.md §9's "soft" memory-pressure
// response (triggered by the watchdog in internal/memwatch once RSS
// crosses the soft threshold): aggressive, immediate, silent eviction of
// anything safe to drop right now, not just the lazy trim-to-cap that
// evictLocked does opportunistically on the next Open. Returns how many
// tiles were actually evicted, so the caller can log whether this
// provided real relief.
//
// Important limitation, worth knowing rather than discovering live: this
// can only evict tiles with Refs<=0. If every cached tile is currently
// open (a real scenario, confirmed by a real flight test — X-Plane can
// hold many tiles open simultaneously while rendering a complex area),
// this legitimately evicts nothing, because none of them are safe to drop
// without breaking an in-flight FUSE read. The watchdog's Hard and
// Critical thresholds exist specifically to cover the case where Soft
// eviction alone isn't enough.
func (c *Cacher) ForceEvictIdle() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := 0
	kept := c.order[:0]
	for _, id := range c.order {
		t, ok := c.tiles[id]
		if ok && t.Refs <= 0 {
			delete(c.tiles, id)
			evicted++
			continue
		}
		kept = append(kept, id)
	}
	c.order = kept
	return evicted
}

// Len reports how many tiles are currently cached (open or idle) —
// diagnostic use (logging alongside ForceEvictIdle's result, stats).
func (c *Cacher) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tiles)
}

// onTileBuilt is called (via the onBuilt closure set on every Tile this
// Cacher creates) right after a real, successful mipmap build — the
// trigger point for LRU content eviction, not just a periodic sweep, so
// the cap is enforced continuously as new tiles join the resident set,
// never transiently exceeded by more than the one tile that just
// triggered the check.
func (c *Cacher) onTileBuilt() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictLRUContentLocked()
}

// evictLRUContentLocked is the real fix for the "cumulative
// resident tiles" finding: MaxOpenTiles (via overBudget) only ever gated
// *new* builds while instantaneously over budget — it never capped how
// many tiles could accumulate real built data (~11MB each) over a long
// session as the open-tile count fluctuated below budget and back up,
// repeatedly letting new batches join a resident set that never shrank
// (idle eviction only removes tiles with Refs<=0, powerless when most
// tiles stay genuinely open/referenced for most of a long dense flight).
// A real 15-minute synthetic test proved this concretely: RSS plateaued
// safely in that test only because tiles were never closed, so OpenCount
// never dropped back under budget to let new ones in — an artifact of the
// test harness, not a property a real flight (which does close/reopen
// tiles as the view changes) would have.
//
// This closes that gap directly: cap the number of tiles *currently
// holding real built data* (DDS.HasAnyData(), independent of Refs/open
// status) at maxOpenTiles, evicting the least-recently-*read* (not
// least-recently-built — see Tile.lastRead's doc comment) built tile's
// content first when over. Evicting content (DDS.ClearAllData()) is NOT
// the same as evicting the Tile from the Cacher: the Tile stays tracked,
// stays "open" if X-Plane still holds it, and will simply serve the
// cheap §7 fallback block until it's rebuilt fresh on next read — exactly
// the same graceful-degradation shape MaxOpenTiles already used for new
// tiles, just now also applied retroactively to old ones under pressure.
// Caller must hold c.mu.
func (c *Cacher) evictLRUContentLocked() {
	if c.maxOpenTiles <= 0 {
		return
	}

	type builtTile struct {
		t        *Tile
		lastRead int64
	}
	var built []builtTile
	for _, t := range c.tiles {
		if t.DDS.HasAnyData() {
			built = append(built, builtTile{t, t.lastRead.Load()})
		}
	}
	if len(built) <= c.maxOpenTiles {
		return
	}

	sort.Slice(built, func(i, j int) bool { return built[i].lastRead < built[j].lastRead })
	toEvict := len(built) - c.maxOpenTiles
	for i := 0; i < toEvict; i++ {
		built[i].t.DDS.ClearAllData()
	}
}
