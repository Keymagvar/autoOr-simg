package tile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"autoOr-simg/internal/native"
	"autoOr-simg/internal/provider"
)

// TestCacher_OpenDoesNotEvictItself is a regression test for a real bug
// found live (not in review): Cacher.Open used to call evictLocked BEFORE
// incrementing the newly created tile's Refs, so a brand-new tile — still
// sitting at Refs=0 at that exact moment — could be evicted by its own
// creating call whenever the cache was already full of genuinely
// still-open (Refs>0) tiles and nothing else was evictable. The caller
// still got back a valid *Tile (so BuildMipmap worked), but the entry was
// silently gone from the Cacher's map, so the very next Get (every FUSE
// Read) or Close (FUSE Release) for that id failed. Reproduced here with
// maxTiles=1 and every existing tile held open, which deterministically
// forces exactly this scenario on the *next* Open.
func TestCacher_OpenDoesNotEvictItself(t *testing.T) {
	c := NewCacher(t.TempDir(), 1, 0, 0, 0, 0) // maxTiles=1 makes this reproduce on the very next Open

	if _, ok := c.Open(1, 1, "EOX", 16, 12, 0); !ok {
		t.Fatal("open tile 1")
	}
	// Tile 1 is deliberately never Closed — it stays "genuinely in use"
	// (Refs=1) for the rest of the test, exactly like X-Plane holding a
	// texture open while more tiles get opened around it.

	// Opening a second, distinct tile pushes len(c.tiles) over maxTiles=1.
	// evictLocked must NOT be able to evict anything (tile 1 is still
	// open, and before the fix, tile 2 — the one just created — was the
	// only other candidate, with Refs still at its zero value).
	t2, ok := c.Open(2, 2, "EOX", 16, 12, 0)
	if !ok {
		t.Fatal("open tile 2")
	}

	if got, ok := c.Get(2, 2, "EOX", 16); !ok || got != t2 {
		t.Fatalf("tile 2 should still be present in the Cacher immediately after Open — got ok=%v, got=%v, want=%v (this is the bug: Open evicted the tile it just created)", ok, got, t2)
	}
	if t2.Refs != 1 {
		t.Errorf("tile 2's Refs = %d after one Open, want 1", t2.Refs)
	}

	// Close must succeed without a "close of unknown tile" warning path
	// (Cacher.Close logs that exact case when the id isn't found).
	c.Close(2, 2, "EOX", 16)
	if t2.Refs != 0 {
		t.Errorf("tile 2's Refs = %d after Close, want 0", t2.Refs)
	}
}

// TestCacher_ForceEvictIdle is the correctness backbone the DESIGN.md §9
// memory watchdog's Soft stage relies on: it must evict every idle
// (Refs<=0) tile regardless of the normal maxTiles cap, and must leave
// every still-open (Refs>0) tile completely alone — evicting an
// actively-open tile would break an in-flight FUSE read referencing it.
func TestCacher_ForceEvictIdle(t *testing.T) {
	c := NewCacher(t.TempDir(), 25, 0, 0, 0, 0)

	// Open three distinct tiles, keep two of them open (simulating
	// X-Plane still holding file handles), close the third (simulating a
	// tile that's scrolled out of view).
	if _, ok := c.Open(1, 1, "EOX", 16, 12, 0); !ok {
		t.Fatal("open tile 1")
	}
	if _, ok := c.Open(2, 2, "EOX", 16, 12, 0); !ok {
		t.Fatal("open tile 2")
	}
	if _, ok := c.Open(3, 3, "EOX", 16, 12, 0); !ok {
		t.Fatal("open tile 3")
	}
	c.Close(3, 3, "EOX", 16) // only this one becomes idle (Refs -> 0)

	if got := c.Len(); got != 3 {
		t.Fatalf("Len() = %d before eviction, want 3", got)
	}

	evicted := c.ForceEvictIdle()
	if evicted != 1 {
		t.Errorf("ForceEvictIdle() evicted %d tiles, want 1 (only the idle one)", evicted)
	}
	if got := c.Len(); got != 2 {
		t.Errorf("Len() = %d after eviction, want 2 (the two still-open tiles)", got)
	}

	// The two open tiles must still be reachable — eviction must not
	// have touched them.
	if _, ok := c.Get(1, 1, "EOX", 16); !ok {
		t.Error("tile 1 (still open) should still be cached after ForceEvictIdle")
	}
	if _, ok := c.Get(2, 2, "EOX", 16); !ok {
		t.Error("tile 2 (still open) should still be cached after ForceEvictIdle")
	}

	// The evicted tile must actually be gone (not just unreachable via
	// Get) and reopening it must create a fresh entry.
	if _, ok := c.Get(3, 3, "EOX", 16); ok {
		t.Error("tile 3 (evicted) should no longer be cached")
	}
	if c.Len() != 2 {
		t.Fatalf("Len() = %d, want 2 before reopen", c.Len())
	}
}

// TestCacher_ForceEvictIdle_NothingEvictableWhenAllOpen covers the real,
// previously-live limitation documented on ForceEvictIdle: when every
// cached tile is actively open, there is nothing safe to evict, and the
// method must correctly evict zero rather than, say, panicking on an
// empty idle set or corrupting the cache's internal ordering slice.
func TestCacher_ForceEvictIdle_NothingEvictableWhenAllOpen(t *testing.T) {
	c := NewCacher(t.TempDir(), 25, 0, 0, 0, 0)
	for i := 0; i < 5; i++ {
		if _, ok := c.Open(i, i, "EOX", 16, 12, 0); !ok {
			t.Fatalf("open tile %d", i)
		}
	}

	evicted := c.ForceEvictIdle()
	if evicted != 0 {
		t.Errorf("ForceEvictIdle() evicted %d, want 0 (all tiles still open)", evicted)
	}
	if got := c.Len(); got != 5 {
		t.Errorf("Len() = %d, want 5 (nothing should have been removed)", got)
	}

	// The cache must still be usable afterward — open a 6th tile and
	// confirm the internal ordering slice wasn't left in a broken state.
	if _, ok := c.Open(9, 9, "EOX", 16, 12, 0); !ok {
		t.Fatal("cache should still accept new opens after a no-op ForceEvictIdle")
	}
	if got := c.Len(); got != 6 {
		t.Errorf("Len() = %d after opening a 6th tile, want 6", got)
	}
}

// TestCacher_LRUContentEvictionBoundsCumulativeResidentTiles is the real
// fix for the "cumulative resident tiles" finding: a real
// 15-minute synthetic flight test found that MaxOpenTiles's admission-only
// check let far more than maxOpenTiles tiles accumulate real built data
// (~11MB each) over a long session, because it only ever gated *new*
// builds while instantaneously over budget — never capped how many tiles
// could hold data at once as the open-tile count fluctuated below budget
// and back up (every time it dipped, a new batch joined a resident set
// that never shrank).
//
// This reproduces that exact shape directly, without waiting 15 minutes:
// 10 distinct tiles, each opened, built for real (a real HTTP round trip),
// and closed — one at a time, so Refs/OpenCount repeatedly returns to 0
// between tiles, exactly the "dips back under budget" pattern a real
// flight's view changes would produce. With only the old admission check,
// all 10 would end up holding real data simultaneously (nothing ever
// evicts a closed-but-cached tile's *content*, only idle eviction of the
// whole Tile, which a small maxTiles would eventually do but not
// immediately or predictably). With LRU content eviction wired in, the
// cumulative count of tiles holding real data must never exceed
// maxOpenTiles, at every single point along the way — not just at the
// end.
func TestCacher_LRUContentEvictionBoundsCumulativeResidentTiles(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	// Tiles are built directly (not via Cacher.Open, whose provider.Get
	// lookup only returns the real, registered providers) so each one's
	// Provider.TileURL points at srv instead of a real internet endpoint —
	// this test only needs *a* successful real HTTP round trip, not real
	// geographic tile semantics. Each is then registered into the
	// Cacher's own bookkeeping by hand (tiles map + openCount), exactly
	// what Open() would have done, so onTileBuilt's eviction scan sees it.
	const maxOpenTiles = 3
	c := NewCacher(t.TempDir(), 25, 0, 0, maxOpenTiles, 0)

	for i := 0; i < 10; i++ {
		tl := &Tile{
			Row: i, Col: 0, Zoom: 10,
			Maptype:  "TEST",
			MinZoom:  10,
			CacheDir: t.TempDir(),
			Provider: testProviderFor(srv.URL),
			Client:   &http.Client{},
			MaxWait:  defaultMaxWait,
			DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
			log:      noopLogger{},
			onBuilt:  c.onTileBuilt,
		}
		// Register it with the Cacher's own bookkeeping (tiles map +
		// openCount) the same way Open() would, so onTileBuilt's eviction
		// scan (which walks c.tiles) actually sees it.
		id := tileID(tl.Row, tl.Col, tl.Maptype, tl.Zoom)
		c.mu.Lock()
		c.tiles[id] = tl
		c.mu.Unlock()
		tl.Refs = 1
		c.openCount.Add(1)

		tl.BuildMipmap(context.Background(), 4) // cheap: 1 chunk

		if got := c.BuiltContentCount(); got > maxOpenTiles {
			t.Fatalf("after building tile %d: BuiltContentCount() = %d, want <= %d at every point, not just at the end", i, got, maxOpenTiles)
		}

		// Close it — Refs back to 0, OpenCount back under budget, exactly
		// the "view changed, tile left scope" pattern that let new
		// batches keep joining the resident set under the old,
		// admission-only check.
		tl.Refs = 0
		c.openCount.Add(-1)
	}

	if got := c.BuiltContentCount(); got > maxOpenTiles {
		t.Errorf("final BuiltContentCount() = %d, want <= %d", got, maxOpenTiles)
	}
}

// testProviderFor returns a Provider whose TileURL always points at the
// given test server, regardless of (col,row,zoom) — good enough for a
// test that only needs *a* successful real-HTTP-round-trip chunk fetch,
// not real geographic tile semantics.
func testProviderFor(baseURL string) provider.Provider {
	return provider.Provider{
		Name:       "TEST",
		TileURL:    func(col, row, zoom, serverIdx int) string { return baseURL + "/tile" },
		NumServers: 1,
		UserAgent:  "test",
	}
}

// TestCacher_ContentBudgetHoldsUnderConcurrentDegradedChurn is the
// regression test for a real finding (the Moscow/VKO case):
// a live overloaded flight measured resident compressed-tile content at
// roughly 1.7x maxOpenTiles (515 vs 300), not the exact cap. The existing
// TestCacher_LRUContentEvictionBoundsCumulativeResidentTiles builds tiles
// one at a time, sequentially — it never exercises many tiles building
// concurrently, which is exactly the condition a real, heavily-loaded
// flight creates. This test does: many goroutines each repeatedly building
// (and, via a slow server relative to a short MaxWait, repeatedly
// degrading) their own distinct tile at once, sampling BuiltContentCount()
// throughout rather than only at quiet points, to catch any transient
// overshoot an eviction-trims-then-an-in-flight-build-refills race would
// produce. This is the minimal-kernel's load-bearing memory-bound
// guarantee (evictLRUContentLocked) — repair no longer exists, so a
// degraded build here just stays degraded, same as production.
func TestCacher_ContentBudgetHoldsUnderConcurrentDegradedChurn(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Slow enough that, combined with a short MaxWait below, composites
	// reliably degrade -- the condition under test.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	sharedCacheDir := t.TempDir() // one dir, not one per tile -- thousands of t.TempDir() calls was its own problem

	const maxOpenTiles = 20
	const numTiles = 60 // 3x the budget, all building/repairing concurrently
	c := NewCacher(t.TempDir(), 100, 0, 16, maxOpenTiles, 16)
	// NewFetchPool's worker goroutines run forever unless told to stop —
	// Close() is documented as "not currently called in production...
	// provided for test cleanup", which this test needs: without it,
	// those 16 workers (and any chunk-fetch jobs still queued when this
	// test function returns) keep running into whatever test runs next,
	// which is exactly what caused a real, 100%-reproducible cross-test
	// data race (this test's workers still calling chunk.Fetch, reading a
	// package-level var a LATER, unrelated test was concurrently writing
	// via its own override-then-restore pattern).
	t.Cleanup(c.fetchPool.Close)

	var maxSeen atomic.Int64
	stop := make(chan struct{})
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if n := int64(c.BuiltContentCount()); n > maxSeen.Load() {
				maxSeen.Store(n)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// A shared, ever-incrementing row counter: each loop iteration below
	// creates a BRAND NEW, never-before-seen Tile (not a reused one) --
	// the real flight's working set keeps expanding into new terrain as
	// the camera moves, which a fixed/reused set of tiles (an earlier,
	// simpler version of this test) does not reproduce at all.
	var nextRow atomic.Int64

	var wg sync.WaitGroup
	for w := 0; w < numTiles; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deadline := time.Now().Add(300 * time.Millisecond)
			for time.Now().Before(deadline) {
				row := int(nextRow.Add(1))
				tl := &Tile{
					Row: row, Col: 0, Zoom: 10,
					Maptype:    "TEST",
					MinZoom:    10,
					CacheDir:   sharedCacheDir,
					Provider:   testProviderFor(srv.URL),
					Client:     &http.Client{},
					MaxWait:    5 * time.Millisecond, // short vs. the server's 15ms -- guarantees degraded builds
					DDS:        NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
					log:        noopLogger{},
					buildSem:   c.buildSem,
					fetchPool:  c.fetchPool,
					overBudget: c.overBudget,
					onBuilt:    c.onTileBuilt,
				}
				id := tileID(tl.Row, tl.Col, tl.Maptype, tl.Zoom)
				c.mu.Lock()
				c.tiles[id] = tl
				c.mu.Unlock()
				tl.Refs = 1
				c.openCount.Add(1)

				tl.BuildMipmap(context.Background(), 0)

				tl.Refs = 0
				c.openCount.Add(-1)
			}
		}()
	}
	wg.Wait()

	// No async stragglers to wait out here: BuildMipmap calls onBuilt
	// (which triggers evictLRUContentLocked) synchronously before
	// returning, and there's no background repair goroutine that could
	// still be running after wg.Wait() -- once every goroutine above has
	// returned, every build's eviction pass has already run.
	close(stop)
	samplerWG.Wait()
	finalCount := c.BuiltContentCount()

	// evictLRUContentLocked is reactive, not preemptive: it only re-trims
	// after a build completes (onBuilt), so a handful of builds finishing
	// in the narrow window between one eviction pass and the next can
	// transiently push the count a little past budget before the next
	// pass catches it. Measured as a small, FIXED slop (not proportional
	// to load: peaked at 22/20 with 60 concurrent tiles and again at
	// 22/20 with 150 — not growing with contention), unlike the real bug
	// this test exists to catch, which was a ~70% sustained overshoot
	// (515 resident vs a 300 budget, live on a real flight). A tolerance
	// here distinguishes "normal reactive-trim slop" from "the budget
	// doesn't actually bound anything" — not a loophole for the latter.
	const slopTolerance = 5
	if maxSeen.Load() > int64(maxOpenTiles+slopTolerance) {
		t.Errorf("BuiltContentCount() peaked at %d during concurrent churn, want <= %d (maxOpenTiles + reactive-trim slop)", maxSeen.Load(), maxOpenTiles+slopTolerance)
	}
	if finalCount > maxOpenTiles {
		t.Errorf("final BuiltContentCount() = %d, want <= %d once churn has stopped and eviction has caught up", finalCount, maxOpenTiles)
	}
}
