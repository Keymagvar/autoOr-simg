package tile

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"autoOr-simg/internal/native"
)

// TestBuildMipmap_PersistsBC1CacheOnCleanBuild is the regression test for
// DESIGN.md §12's "persist compressed DXT mip output across sessions, not
// just the raw JPEG chunks" — a clean (non-degraded, every chunk a
// genuine fresh fetch) mip0 build must write a BC1 cache file, so a later
// rebuild of the same tile (this session or a fresh process) can skip the
// decode+compress pipeline entirely, not just the network fetch.
func TestBuildMipmap_PersistsBC1CacheOnCleanBuild(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	// A real fetchPool, same as every Tile actually gets from Cacher —
	// not the nil fetchPool a bare struct literal leaves you with. That
	// nil case falls back to firing every chunk fetch as its own
	// unbounded goroutine (tile.go's fetchGrid), which is not what
	// production ever does (Cacher always bounds this via fetchPool) and
	// turned out not to be free on Windows CI either: even after tuning
	// the HTTP client's idle-connection pool (newTileHTTPClient, below),
	// a real windows-latest run still only completed 223-256 of a
	// 256-chunk unbounded burst inside a 5s MaxWait — the bottleneck
	// wasn't just connection reuse, it was genuinely firing 256
	// concurrent goroutines/syscalls at once on a resource-constrained CI
	// VM. Bounding it to defaultFetchThreads workers matches what real
	// usage actually looks like and is the real fix, not another
	// timeout bump.
	pool := NewFetchPool(defaultFetchThreads)
	t.Cleanup(pool.Close)

	cacheDir := t.TempDir()
	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:   "TEST",
		MinZoom:   10,
		CacheDir:  cacheDir,
		Provider:  testProviderFor(srv.URL),
		fetchPool: pool,
		// See newTileHTTPClient's own doc comment (cache.go): Go's
		// http.DefaultTransport only keeps 2 idle connections per host,
		// so a bare &http.Client{} churns through fresh TCP connections
		// instead of reusing them — this mirrors the same tuning
		// production's shared Cacher.DefaultClient gets.
		Client:  newTileHTTPClient(defaultFetchThreads),
		MaxWait: 5 * time.Second,
		DDS:     NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:     noopLogger{},
	}

	tl.BuildMipmap(context.Background(), 0)

	cached, ok := tl.loadBC1Cache(0)
	if !ok {
		t.Fatal("loadBC1Cache(0) = false after a clean build, want a persisted cache entry")
	}
	tl.DDS.dataMu.RLock()
	built := append([]byte(nil), tl.DDS.Mipmaps[0].Data...)
	tl.DDS.dataMu.RUnlock()
	if string(cached) != string(built) {
		t.Error("persisted BC1 cache bytes differ from what was actually built and served")
	}
}

// TestBuildMipmap_LoadsBC1CacheWithoutNetwork is the real end-to-end
// proof: a second, brand-new Tile for the same (row,col,zoom,maptype) —
// standing in for a process restart, or this same tile being evicted and
// reopened — must load mip0 entirely from the persisted BC1 cache, with
// zero real HTTP requests, zero decode, zero compress.
func TestBuildMipmap_LoadsBC1CacheWithoutNetwork(t *testing.T) {
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

	cacheDir := t.TempDir()
	provider := testProviderFor(srv.URL)
	newTile := func() *Tile {
		// Real, bounded fetchPool — see TestBuildMipmap_
		// PersistsBC1CacheOnCleanBuild's pool comment above for why a nil
		// fetchPool (unbounded 256-goroutine burst) isn't just a test
		// artifact, it's genuinely slower on Windows CI.
		pool := NewFetchPool(defaultFetchThreads)
		t.Cleanup(pool.Close)
		return &Tile{
			Row: 0, Col: 0, Zoom: 10,
			Maptype:   "TEST",
			MinZoom:   10,
			CacheDir:  cacheDir,
			Provider:  provider,
			fetchPool: pool,
			Client:    newTileHTTPClient(defaultFetchThreads),
			MaxWait:   5 * time.Second,
			DDS:       NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
			log:       noopLogger{},
		}
	}

	first := newTile()
	first.BuildMipmap(context.Background(), 0)
	firstReqCount := requests.Load()
	if firstReqCount == 0 {
		t.Fatal("first build made 0 requests, want > 0 — the test setup itself is broken")
	}
	first.DDS.dataMu.RLock()
	firstData := append([]byte(nil), first.DDS.Mipmaps[0].Data...)
	first.DDS.dataMu.RUnlock()

	second := newTile() // fresh Tile, fresh DDS — simulates a restart/reopen, not a reused object
	second.BuildMipmap(context.Background(), 0)

	if got := requests.Load(); got != firstReqCount {
		t.Errorf("second build made %d total requests (vs %d after the first build), want unchanged — it should have loaded entirely from the BC1 cache, no network at all", got, firstReqCount)
	}
	second.DDS.dataMu.RLock()
	secondData := append([]byte(nil), second.DDS.Mipmaps[0].Data...)
	second.DDS.dataMu.RUnlock()
	if string(firstData) != string(secondData) {
		t.Error("second (cache-loaded) build's mip0 data differs from the first (freshly-built) one")
	}
	if !second.DDS.IsRetrieved(0) {
		t.Error("mipmap 0 should be marked Retrieved after a cache-loaded build")
	}
}

// TestBuildMipmap_DoesNotPersistDegradedBuild confirms a build with a
// genuinely missing chunk (MaxWait timeout, no backup available) never
// writes a BC1 cache entry — reusing it later would permanently serve a
// build that stays degraded for this Tile's whole lifetime (no background
// repair exists — see BuildMipmap's doc comment).
func TestBuildMipmap_DoesNotPersistDegradedBuild(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Blocked while `blocked` is true, guaranteeing every chunk times out
	// with nothing to fall back on (fresh cache dir, nothing else cached
	// at any zoom) — then serves real data once unblocked. served counts
	// every handler invocation so the test can deterministically wait for
	// all 256 chunk requests to actually finish (server-side) before
	// returning — without a background repair goroutine to poll for
	// completion (see this test's own doc comment), nothing else gives
	// the in-flight chunk-fetch goroutines (never cancelled by MaxWait —
	// see fetchComposite's doc comment) a chance to finish their disk
	// writes before t.TempDir()'s cleanup runs and races them.
	//
	// A target-count poll (below), not a sync.WaitGroup: fetchGrid
	// (tile.go) launches all 256 chunk fetches as unbounded `go
	// fetchOne(...)` goroutines with nothing synchronizing their actual
	// start, so there's no guarantee the Go scheduler has run every one
	// of them — let alone had it reach Add(1) — before this test moves
	// past a WaitGroup.Wait() call. `go test -race` caught this directly:
	// a goroutine that hadn't started yet called Add(1) after Wait() had
	// already returned, a genuine "WaitGroup reused before Wait returned"
	// panic. Polling for the real target (totalChunks) instead has no
	// such race — it simply keeps waiting, for however long it actually
	// takes, until every one of the 256 has genuinely been counted.
	const totalChunks = GridSize * GridSize
	var blocked atomic.Bool
	blocked.Store(true)
	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for blocked.Load() {
			time.Sleep(5 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
		served.Add(1)
	}))
	defer srv.Close()

	// Bounded fetchPool — the real root cause, found via the log line
	// enabled below: a real windows-latest CI run showed "connectex: ...
	// actively refused it" for dozens of the 256 chunks. That's not
	// slowness, it's Windows' TCP listen backlog (much smaller by default
	// than Linux's) overflowing when 256 raw connections hit this single
	// local listener at once — excess SYNs get refused outright, not
	// queued, so no amount of waiting ever recovers them (confirmed: the
	// served count plateaus below 256 permanently, it doesn't climb
	// slowly). A bounded pool keeps concurrent connection attempts at
	// defaultFetchThreads (32) instead of all 256 at once, well within
	// any reasonable backlog — matching what production always does
	// anyway (Cacher never lets a Tile bypass fetchPool). An earlier
	// attempt at this same bounded pool was reverted because it broke a
	// since-replaced sync.WaitGroup-based wait below (queued-but-
	// not-yet-dispatched jobs raced that WaitGroup's own Wait() call) —
	// the target-count poll that replaced it has no such race regardless
	// of how the 256 jobs get dispatched, so it's safe to combine with
	// bounding now.
	pool := NewFetchPool(defaultFetchThreads)
	t.Cleanup(pool.Close)

	cacheDir := t.TempDir()
	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:   "TEST",
		MinZoom:   10,
		CacheDir:  cacheDir,
		Provider:  testProviderFor(srv.URL),
		fetchPool: pool,
		Client:    newTileHTTPClient(defaultFetchThreads),
		// The server stays blocked (gated by `blocked`, independent of
		// MaxWait) for this whole call, so BuildMipmap always times out
		// and reports degraded regardless of MaxWait's value — the chunk
		// fetches fetchGrid (tile.go) dispatches through fetchPool above
		// keep running in the background afterward either way, and the
		// target-count poll below (not MaxWait) is what makes waiting for
		// all of them actually safe.
		MaxWait: 30 * time.Millisecond,
		DDS:     NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		// Not noopLogger: a real windows-latest CI run hit this test's own
		// 10s backstop below with only 202/256 ever served, and noopLogger
		// silently swallows the exact Warn("chunk fetch failed...", "err",
		// err) call (tile.go) that would say WHY the other 54 never
		// reached the handler — a dropped/errored connection, a real
		// per-request timeout, or something else entirely. A real logger
		// here means the next failure (if there is one) is diagnosable
		// from the CI log directly instead of requiring another guess.
		log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	tl.BuildMipmap(context.Background(), 0)

	if _, ok := tl.loadBC1Cache(0); ok {
		t.Error("loadBC1Cache(0) = true after a degraded (all-missing) build, want no cache entry written")
	}

	blocked.Store(false)

	// Poll for the real target instead of a WaitGroup: see served's own
	// comment above for why a WaitGroup here is an actual race, not just
	// a style preference. deadline is a correctness backstop — the actual
	// fix for what used to trip it (202/256, then 246/256 served) is the
	// bounded fetchPool above; the real logger it took to find that
	// ("connectex: ... actively refused it" — Windows' TCP listen backlog
	// overflowing under 256 simultaneous raw connections, not slowness)
	// confirmed those requests were refused outright, not just slow, so
	// no deadline could ever have fixed it by itself. 45s stays as margin
	// for genuine CI slowness, well past perRequestTimeout's own 30s
	// per-chunk bound (chunk.go) so a chunk that's going to fail outright
	// has had time to actually do so.
	deadline := time.Now().Add(45 * time.Second)
	for served.Load() < totalChunks {
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d chunk requests were ever served after unblocking — a real regression, not a timing fluke", served.Load(), totalChunks)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// served.Load() == totalChunks only confirms the server finished
	// writing each response — each client goroutine still has to read the
	// body and save_cache() to disk afterward, and then Windows itself
	// has to finish settling that close before the directory genuinely
	// looks empty (file-close/directory-entry semantics more asynchronous
	// than POSIX unlink — a real, known platform difference). No
	// deterministic signal to wait on for that part, so this remaining
	// sleep is an honest margin for genuinely nondeterministic platform
	// behavior, not a guess padded indefinitely.
	time.Sleep(750 * time.Millisecond)
}

// TestBuildMipmap_DoesNotPersistBuildWithConfirmedEmptyChunk is the most
// important case: a build where every chunk resolves cleanly (so
// degraded=false, per tonight's earlier chunk-fetch fix) but via the
// provider's confirmed "no data at this zoom" response, not a real fetch.
// Without the allFresh distinction (fetchGrid's second return value),
// this would look identical to a genuinely complete build and get
// persisted — permanently freezing a possibly-temporary coverage gap into
// the BC1 cache, where no future repair or coverage change could ever
// correct it (unlike the normal in-memory case, where a later read
// re-checks nothing persists it at all).
func TestBuildMipmap_DoesNotPersistBuildWithConfirmedEmptyChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a jpeg, e.g. a provider's no-imagery placeholder"))
	}))
	defer srv.Close()

	// Same risk as the clean-build tests above: a confirmed-empty response
	// still means every one of the 256 chunks completes quickly, so this
	// is the same unbounded-burst-vs-instant-server shape a nil fetchPool
	// made slow on real Windows CI — see TestBuildMipmap_
	// PersistsBC1CacheOnCleanBuild's pool comment.
	pool := NewFetchPool(defaultFetchThreads)
	t.Cleanup(pool.Close)

	cacheDir := t.TempDir()
	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:   "TEST",
		MinZoom:   10,
		CacheDir:  cacheDir,
		Provider:  testProviderFor(srv.URL),
		fetchPool: pool,
		Client:    newTileHTTPClient(defaultFetchThreads),
		MaxWait:   2 * time.Second,
		DDS:       NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:       noopLogger{},
	}

	tl.BuildMipmap(context.Background(), 0)

	if _, ok := tl.loadBC1Cache(0); ok {
		t.Error("loadBC1Cache(0) = true after a confirmed-empty-but-not-degraded build, want no cache entry written — allFresh must have been false")
	}
}
