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

	cacheDir := t.TempDir()
	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:  "TEST",
		MinZoom:  10,
		CacheDir: cacheDir,
		Provider: testProviderFor(srv.URL),
		Client:   &http.Client{},
		// Generous on purpose: this Tile has no fetchPool (direct
		// construction, not via Cacher), so all 256 chunk fetches fire as
		// unbounded concurrent goroutines straight at the local httptest
		// server. A real windows-latest CI run took >2s but <10s to
		// finish that burst — Windows' loopback TCP stack handles a
		// sudden spike of concurrent short-lived connections measurably
		// slower than Linux's — and this test's actual assertion
		// (allFresh ends up true, BC1 cache gets written) silently
		// degrades to false on any timeout, so a too-tight budget here
		// was failing the test without the build logic itself being
		// wrong.
		MaxWait: 10 * time.Second,
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
		return &Tile{
			Row: 0, Col: 0, Zoom: 10,
			Maptype:  "TEST",
			MinZoom:  10,
			CacheDir: cacheDir,
			Provider: provider,
			Client:   &http.Client{},
			MaxWait:  10 * time.Second, // generous on purpose — see TestBuildMipmap_PersistsBC1CacheOnCleanBuild's own MaxWait comment
			DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
			log:      noopLogger{},
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
	// at any zoom) — then serves real data once unblocked. inFlight tracks
	// every handler invocation so the test can deterministically wait for
	// all 256 chunk requests to actually finish (server-side) before
	// returning — without a background repair goroutine to poll for
	// completion (see this test's own doc comment), nothing else gives
	// the in-flight chunk-fetch goroutines (never cancelled by MaxWait —
	// see fetchComposite's doc comment) a chance to finish their disk
	// writes before t.TempDir()'s cleanup runs and races them.
	var blocked atomic.Bool
	blocked.Store(true)
	var inFlight sync.WaitGroup
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlight.Add(1)
		defer inFlight.Done()
		for blocked.Load() {
			time.Sleep(5 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:  "TEST",
		MinZoom:  10,
		CacheDir: cacheDir,
		Provider: testProviderFor(srv.URL),
		Client:   &http.Client{},
		MaxWait:  30 * time.Millisecond,
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
	}

	tl.BuildMipmap(context.Background(), 0)

	if _, ok := tl.loadBC1Cache(0); ok {
		t.Error("loadBC1Cache(0) = true after a degraded (all-missing) build, want no cache entry written")
	}

	blocked.Store(false)
	inFlight.Wait() // every handler has now written its response
	// inFlight.Wait() only confirms the server finished writing each
	// response — each client goroutine still has to read the body and
	// save_cache() to disk afterward. A short settle window for that
	// client-side tail, same order of magnitude as this file's own
	// polling intervals, so TempDir's cleanup doesn't race those last
	// writes.
	time.Sleep(50 * time.Millisecond)
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

	cacheDir := t.TempDir()
	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:  "TEST",
		MinZoom:  10,
		CacheDir: cacheDir,
		Provider: testProviderFor(srv.URL),
		Client:   &http.Client{},
		MaxWait:  200 * time.Millisecond,
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
	}

	tl.BuildMipmap(context.Background(), 0)

	if _, ok := tl.loadBC1Cache(0); ok {
		t.Error("loadBC1Cache(0) = true after a confirmed-empty-but-not-degraded build, want no cache entry written — allFresh must have been false")
	}
}
