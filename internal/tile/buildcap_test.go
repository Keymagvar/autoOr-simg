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

// TestBuildMipmap_ConcurrentBuildCap is the real-mechanism test for the
// concurrent-build cap added in direct response to the first live test
// flight driving RSS to ~20GB: it does NOT just check that buildSem exists
// or that BuildMipmap compiles with it — it launches far more simultaneous
// BuildMipmap calls than the configured cap, across distinct real Tile
// objects sharing one buildSem (exactly as Cacher.Open wires every Tile it
// creates), against a real slow HTTP server instrumented to report actual
// peak concurrency, and asserts that peak never exceeds the cap.
func TestBuildMipmap_ConcurrentBuildCap(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	const buildCap = 2
	const numTiles = 6
	const reqDelay = 120 * time.Millisecond

	var inFlight, peak atomic.Int64
	var mu sync.Mutex
	var totalRequests atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalRequests.Add(1)
		n := inFlight.Add(1)
		mu.Lock()
		if n > peak.Load() {
			peak.Store(n)
		}
		mu.Unlock()
		time.Sleep(reqDelay)
		inFlight.Add(-1)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	buildSem := make(chan struct{}, buildCap)

	testProvider := provider.Provider{
		Name:       "TEST",
		TileURL:    func(col, row, zoom, serverIdx int) string { return srv.URL + "/tile" },
		NumServers: 1,
		MaxThreads: 8,
		UserAgent:  "test",
	}

	// Each tile uses idx=4 (mipmap 4), which per quickZoom's maxFetchDepth
	// logic resolves to exactly a 1x1 chunk grid — one HTTP request per
	// BuildMipmap call, keeping this test fast while still exercising the
	// real fetch path (not a shortcut). Row/Col are spaced by 20 (not 1)
	// specifically so each tile's quickZoom(4) shift (>>4, i.e. /16)
	// still lands on a *distinct* underlying chunk coordinate — with a
	// spacing of 1, all 6 tiles would collapse to the same (col>>4,
	// row>>4) cell, and after the first genuinely fetches it, the rest
	// would be served from the (correctly) warm disk cache instead of
	// the network, which would undercount real concurrent HTTP traffic
	// and silently defeat this test's whole premise. Each tile also gets
	// its own CacheDir, belt-and-suspenders against any other collision.
	tiles := make([]*Tile, numTiles)
	for i := 0; i < numTiles; i++ {
		tiles[i] = &Tile{
			Row: i * 20, Col: i * 20, Zoom: 16,
			Maptype:  "TEST",
			MinZoom:  12,
			CacheDir: t.TempDir(),
			Provider: testProvider,
			Client:   &http.Client{},
			MaxWait:  time.Second, // generous — this test is about concurrency, not deadlines
			DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
			log:      noopLogger{},
			buildSem: buildSem,
		}
	}

	var wg sync.WaitGroup
	start := time.Now()
	for _, tl := range tiles {
		wg.Add(1)
		go func(tl *Tile) {
			defer wg.Done()
			tl.BuildMipmap(context.Background(), 4)
		}(tl)
	}
	wg.Wait()
	elapsed := time.Since(start)

	if totalRequests.Load() != numTiles {
		t.Errorf("total requests = %d, want %d (every tile's build should have completed, none skipped/lost)", totalRequests.Load(), numTiles)
	}
	if peak.Load() > buildCap {
		t.Errorf("peak concurrent builds = %d, want <= %d (the configured cap) — buildSem did not actually throttle", peak.Load(), buildCap)
	}
	if peak.Load() < 2 {
		t.Errorf("peak concurrent builds = %d, want >= 2 — if the cap is never actually reached, this test isn't proving throttling happened, just that requests complete eventually", peak.Load())
	}

	// With numTiles=6 and cap=2, throttling means at least 3 sequential
	// batches of reqDelay each — if everything ran unthrottled this would
	// finish in ~1 batch (~reqDelay). This is the real behavioral proof
	// that requests were actually queued, not just a peak-counter
	// coincidence.
	minExpected := time.Duration(numTiles/buildCap) * reqDelay
	if elapsed < minExpected {
		t.Errorf("elapsed = %s, want >= %s — completed too fast for the cap to have actually queued anything", elapsed, minExpected)
	}

	for i, tl := range tiles {
		if !tl.DDS.Mipmaps[4].Retrieved {
			t.Errorf("tile %d: mipmap 4 should be marked Retrieved after a successful build", i)
		}
	}
}

// TestBuildMipmap_NilBuildSemIsUnbounded confirms the documented nil-means-
// unlimited behavior: a Tile with no buildSem (the state every other test
// in this package already relies on, and what direct NewTile/struct-literal
// construction outside a Cacher produces) must not block at all on the
// semaphore path.
func TestBuildMipmap_NilBuildSemIsUnbounded(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	tl := &Tile{
		Row: 0, Col: 0, Zoom: 16,
		Maptype:  "TEST",
		MinZoom:  12,
		CacheDir: t.TempDir(),
		Provider: provider.Provider{
			Name:       "TEST",
			TileURL:    func(col, row, zoom, serverIdx int) string { return srv.URL + "/tile" },
			NumServers: 1,
			MaxThreads: 8,
			UserAgent:  "test",
		},
		Client:  &http.Client{},
		MaxWait: time.Second,
		DDS:     NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:     noopLogger{},
		// buildSem intentionally left nil
	}

	done := make(chan struct{})
	go func() {
		tl.BuildMipmap(context.Background(), 4)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BuildMipmap with a nil buildSem hung — it should skip the semaphore entirely, not block on a nil channel")
	}

	if !tl.DDS.Mipmaps[4].Retrieved {
		t.Error("mipmap 4 should be marked Retrieved")
	}
}
