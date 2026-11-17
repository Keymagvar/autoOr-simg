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

// TestZoomDropForLoad is a table test for the pure threshold function
// driving the coverage-aware mip0 degradation (the VKO throughput
// finding). The key invariant it protects: active is always a
// buffered channel's len(), which can never exceed buildCap, so every
// bucket above the lowest must actually be reachable — a threshold written
// in terms of multiples of buildCap greater than 1x is dead code that would
// never fire in production.
func TestZoomDropForLoad(t *testing.T) {
	cases := []struct {
		name         string
		active, cap  int
		wantZoomDrop int
	}{
		{"nil buildSem (cap=0) never throttles", 0, 0, 0},
		{"empty", 0, 32, 0},
		{"just under half", 15, 32, 0},
		{"half", 16, 32, 1},
		{"just under three-quarters", 23, 32, 1},
		{"three-quarters", 24, 32, 2},
		{"full", 32, 32, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := zoomDropForLoad(c.active, c.cap); got != c.wantZoomDrop {
				t.Errorf("zoomDropForLoad(%d, %d) = %d, want %d", c.active, c.cap, got, c.wantZoomDrop)
			}
		})
	}
}

// TestSetPerformanceMode confirms each preset actually changes
// zoomDropForLoad's behavior at the same (active, buildCap), and that an
// unrecognized mode falls back to "quality" — including restoring
// quality's thresholds afterward (a global, so leaking a non-default mode
// into whatever test runs next would be a real, silent bug of its own).
func TestSetPerformanceMode(t *testing.T) {
	origT1, origT2 := zoomDropThreshold1, zoomDropThreshold2
	defer func() { zoomDropThreshold1, zoomDropThreshold2 = origT1, origT2 }()

	const buildCap = 32
	const active = 10 // 10/32 = 0.3125 -- lands differently depending on mode

	SetPerformanceMode("quality")
	if got := zoomDropForLoad(active, buildCap); got != 0 {
		t.Errorf("quality: zoomDropForLoad(%d, %d) = %d, want 0 (0.3125 is under quality's 0.5 first threshold)", active, buildCap, got)
	}

	SetPerformanceMode("balanced")
	if got := zoomDropForLoad(active, buildCap); got != 0 {
		t.Errorf("balanced: zoomDropForLoad(%d, %d) = %d, want 0 (0.3125 is under balanced's 0.35 first threshold)", active, buildCap, got)
	}

	SetPerformanceMode("speed")
	if got := zoomDropForLoad(active, buildCap); got != 1 {
		t.Errorf("speed: zoomDropForLoad(%d, %d) = %d, want 1 (0.3125 is over speed's 0.2 first threshold)", active, buildCap, got)
	}

	SetPerformanceMode("quality") // leave a non-default mode active, then confirm an unrecognized name resets to quality, not "whatever was set last"
	SetPerformanceMode("not-a-real-mode")
	if got := zoomDropForLoad(active, buildCap); got != 0 {
		t.Errorf("unrecognized mode: zoomDropForLoad(%d, %d) = %d, want 0 (must fall back to quality's thresholds)", active, buildCap, got)
	}
}

// TestBuildMipmap_Mip0DegradesUnderSaturatedBuildSem is the end-to-end proof
// that a saturated buildSem makes a new mip0 build fetch a far smaller grid
// (fewer real HTTP requests) instead of either paying full cost or hitting
// overBudget's all-or-nothing fallback-color cliff — and that the result is
// still a correctly-sized, valid mip0 buffer. This degraded build stays at
// the reduced zoom for this Tile's lifetime (no background repair exists —
// see BuildMipmap's doc comment).
func TestBuildMipmap_Mip0DegradesUnderSaturatedBuildSem(t *testing.T) {
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

	const buildCap = 8
	buildSem := make(chan struct{}, buildCap)
	// Pre-fill to 7/8 (just under full): once BuildMipmap acquires its own
	// slot below, len(buildSem) reads 8/8 — "full", zoomDropForLoad's top
	// bucket (drop=2, a 16x reduction: 256 chunks -> 16).
	for i := 0; i < buildCap-1; i++ {
		buildSem <- struct{}{}
	}

	tl := &Tile{
		Row: 0, Col: 0, Zoom: 16,
		Maptype:  "TEST",
		MinZoom:  10, // enough room below Zoom=16 for a drop of 2
		CacheDir: t.TempDir(),
		Provider: testProvider,
		Client:   &http.Client{},
		MaxWait:  5 * time.Second,
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
		buildSem: buildSem,
	}

	tl.BuildMipmap(context.Background(), 0)

	// drop=2 means a 4x4 chunk grid (16 chunks), not the full 16x16 (256).
	if got := requests.Load(); got != 16 {
		t.Errorf("requests = %d, want 16 (4x4 grid at drop=2), not the full 256", got)
	}
	if !tl.DDS.Mipmaps[0].Retrieved {
		t.Error("mipmap 0 should be marked Retrieved even though degraded — same convention as every other degraded build")
	}
	data := tl.DDS.Read(ddsHeaderSize, tl.DDS.Mipmaps[0].Length)
	if len(data) != tl.DDS.Mipmaps[0].Length {
		t.Fatalf("Read returned %d bytes, want %d (upscaled composite must compress back to mip0's real size)", len(data), tl.DDS.Mipmaps[0].Length)
	}
}

// TestBuildMipmap_Mip0FullZoomWhenBuildSemNotSaturated is the control: with
// buildSem nowhere near full, mip0 must build at full native zoom exactly
// as before this feature existed — the real fetch path (256 requests), not
// degraded.
func TestBuildMipmap_Mip0FullZoomWhenBuildSemNotSaturated(t *testing.T) {
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
		MaxThreads: 32,
		UserAgent:  "test",
	}

	buildSem := make(chan struct{}, 32) // empty — nowhere near saturated

	tl := &Tile{
		Row: 0, Col: 0, Zoom: 16,
		Maptype:  "TEST",
		MinZoom:  10,
		CacheDir: t.TempDir(),
		Provider: testProvider,
		// Not a bare &http.Client{} — see newTileHTTPClient's doc comment
		// (cache.go) and bc1cache_test.go's TestBuildMipmap_
		// PersistsBC1CacheOnCleanBuild for the real finding: Go's default
		// Transport only keeps 2 idle connections per host, which turns a
		// 256-concurrent-fetch burst into 256 fresh TCP handshakes instead
		// of connection reuse — cheap on Linux, measurably expensive on
		// Windows (confirmed on a real windows-latest CI run).
		Client:   newTileHTTPClient(GridSize * GridSize),
		MaxWait:  5 * time.Second,
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
		buildSem: buildSem,
	}

	tl.BuildMipmap(context.Background(), 0)

	if got := requests.Load(); got != GridSize*GridSize {
		t.Errorf("requests = %d, want %d (full 16x16 grid, no degradation)", got, GridSize*GridSize)
	}
}
