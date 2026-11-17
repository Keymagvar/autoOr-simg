package tile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"autoOr-simg/internal/native"
	"autoOr-simg/internal/provider"
)

// TestBuildMipmap_OverBudgetSkipsRealBuild is the test the open-tile
// memory budget fix exists to pass: once the Cacher-shared overBudget func reports true,
// BuildMipmap must not hit the network or allocate a real composite/
// compressed buffer at all — it must leave the mipmap unretrieved and let
// DDS.Read's existing §7 fallback-block path serve it, at near-zero memory
// cost. As soon as overBudget flips back to false, the very next build call
// on the same Tile must proceed normally — no closing/reopening required.
func TestBuildMipmap_OverBudgetSkipsRealBuild(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	testProvider := provider.Provider{
		Name:       "TEST",
		TileURL:    func(col, row, zoom, serverIdx int) string { return srv.URL + "/tile" },
		NumServers: 1,
		MaxThreads: 8,
		UserAgent:  "test",
	}

	var over atomic.Bool
	over.Store(true)

	// Real, bounded fetchPool — the second BuildMipmap call below (once
	// budget clears) does a genuine 256-chunk fetch, same unbounded-
	// goroutine-burst shape that made other tests in this package flake
	// on real windows-latest CI: TempDir's cleanup (registered by
	// t.TempDir() itself, runs when this test function returns) could
	// race the tail end of those chunk-fetch goroutines' disk writes
	// before they'd finished, which Windows' slower async file-close
	// semantics made worse than on Linux. See bc1cache_test.go's
	// TestBuildMipmap_PersistsBC1CacheOnCleanBuild for the full finding.
	pool := NewFetchPool(defaultFetchThreads)
	t.Cleanup(pool.Close)

	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:    "TEST",
		MinZoom:    10,
		CacheDir:   t.TempDir(),
		Provider:   testProvider,
		fetchPool:  pool,
		Client:     newTileHTTPClient(defaultFetchThreads),
		MaxWait:    defaultMaxWait,
		DDS:        NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:        noopLogger{},
		overBudget: over.Load,
	}

	tl.BuildMipmap(context.Background(), 0)

	if requests.Load() != 0 {
		t.Errorf("requests = %d while over budget, want 0 — no network fetch should happen", requests.Load())
	}
	if tl.DDS.Mipmaps[0].Retrieved {
		t.Error("mipmap 0 marked Retrieved after a budget-skipped build, want false")
	}
	if tl.BudgetDegradedCount() != 1 {
		t.Errorf("BudgetDegradedCount() = %d, want 1", tl.BudgetDegradedCount())
	}

	// A read must still return a full, valid-length response (the §7
	// fallback block tiled to fill the gap), never an empty/short buffer.
	data := tl.DDS.Read(ddsHeaderSize, tl.DDS.Mipmaps[0].Length)
	if len(data) != tl.DDS.Mipmaps[0].Length {
		t.Fatalf("Read returned %d bytes while degraded, want %d", len(data), tl.DDS.Mipmaps[0].Length)
	}

	// Budget clears — the next build on the same Tile (no close/reopen)
	// must now do the real fetch and actually populate the mipmap.
	over.Store(false)
	tl.BuildMipmap(context.Background(), 0)

	if requests.Load() == 0 {
		t.Error("requests = 0 after budget cleared, want the real build to have fetched at least one chunk")
	}
	if !tl.DDS.Mipmaps[0].Retrieved {
		t.Error("mipmap 0 should be Retrieved after a real build once budget allows it")
	}
	if tl.BudgetDegradedCount() != 1 {
		t.Errorf("BudgetDegradedCount() = %d after a successful real build, want unchanged at 1", tl.BudgetDegradedCount())
	}

	// BuildMipmap's fetchGrid loop (tile.go) already blocks until every
	// chunk's fetchOne goroutine has sent its result, and each one's disk
	// write (chunk.Fetch) happens-before that send — so by the time
	// BuildMipmap returns, every write has genuinely completed, not just
	// been started. This sleep is for what happens after that: on a real
	// windows-latest CI run, TempDir's own cleanup (registered by
	// t.TempDir() above) still hit "directory not empty" even though every
	// write had finished, because Windows' file-close/directory-entry
	// settling isn't synchronous with the close() syscall returning the
	// way POSIX unlink is — a handle or AV scan can still be settling
	// briefly after. Real margin for that, not a timeout fight.
	time.Sleep(100 * time.Millisecond)
}

// TestCacher_OpenCountTracksOnlyRefsPositive confirms openCount — the
// quantity overBudget checks — reflects exactly "tiles with Refs>0", not
// len(c.tiles) (which also includes idle, already-evicted-eligible tiles),
// across opens, re-opens of the same tile, and closes.
func TestCacher_OpenCountTracksOnlyRefsPositive(t *testing.T) {
	c := NewCacher(t.TempDir(), 25, 0, 0, 2, 0) // maxOpenTiles=2

	if _, ok := c.Open(1, 1, "EOX", 16, 12, 0); !ok {
		t.Fatal("open tile 1")
	}
	if got := c.OpenCount(); got != 1 {
		t.Fatalf("OpenCount() = %d after first open, want 1", got)
	}
	if c.overBudget() {
		t.Error("overBudget() = true at 1/2, want false")
	}

	if _, ok := c.Open(1, 1, "EOX", 16, 12, 0); !ok { // re-open the same tile
		t.Fatal("re-open tile 1")
	}
	if got := c.OpenCount(); got != 1 {
		t.Errorf("OpenCount() = %d after re-opening the same tile, want still 1 (same tile, Refs went 1->2)", got)
	}

	if _, ok := c.Open(2, 2, "EOX", 16, 12, 0); !ok {
		t.Fatal("open tile 2")
	}
	if got := c.OpenCount(); got != 2 {
		t.Fatalf("OpenCount() = %d after opening a second distinct tile, want 2", got)
	}
	if !c.overBudget() {
		t.Error("overBudget() = false at 2/2, want true")
	}

	c.Close(1, 1, "EOX", 16) // tile 1: Refs 2->1, still open
	if got := c.OpenCount(); got != 2 {
		t.Errorf("OpenCount() = %d after one of two refs closed, want still 2 (tile 1 still has Refs=1)", got)
	}

	c.Close(1, 1, "EOX", 16) // tile 1: Refs 1->0, now idle
	if got := c.OpenCount(); got != 1 {
		t.Errorf("OpenCount() = %d after tile 1's last ref closed, want 1 (only tile 2 still open)", got)
	}
	if c.overBudget() {
		t.Error("overBudget() = true at 1/2 after closing tile 1 fully, want false")
	}
}
