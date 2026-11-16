package tile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"autoOr-simg/internal/native"
)

// TestChunk_Fetch_NonJPEGResponseIsNilNilNotError is the regression test
// for a real divergence from getortho.py found by flying this fix's
// predecessor: Chunk.get() there treats a 200 response that isn't a real
// JPEG (a provider's "no imagery at this zoom here" placeholder, e.g.
// Bing's small PNG) as a confirmed, final, non-error outcome — sets empty
// data, still returns success. The Go port used to return an error
// instead, which made every such chunk count as "missing" (a MaxWait
// timeout miss, not a confirmed empty result). Fetch must return (nil,
// nil), not (nil, err), for this case.
func TestChunk_Fetch_NonJPEGResponseIsNilNilNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a jpeg, e.g. a provider's no-imagery placeholder PNG"))
	}))
	defer srv.Close()

	c := &Chunk{Col: 1, Row: 1, Zoom: 10, Maptype: "TEST", CacheDir: t.TempDir()}
	p := testProviderFor(srv.URL)

	data, err := c.Fetch(context.Background(), &http.Client{}, p, 0)
	if err != nil {
		t.Fatalf("Fetch returned err=%v, want nil (a non-JPEG 200 response is a confirmed outcome, not a failure)", err)
	}
	if data != nil {
		t.Fatalf("Fetch returned data=%v, want nil", data)
	}

	// Must not be written to disk cache — matches getortho.py's
	// save_cache, which no-ops on empty/falsy data. A provider's coverage
	// can change, so this isn't memoized as permanent.
	if _, ok := c.GetCached(); ok {
		t.Error("GetCached() found a cached entry after a non-JPEG response, want none written")
	}
}

// TestBuildMipmap_AllChunksNonJPEGIsNotDegraded is the end-to-end proof:
// when every chunk in a tile's grid resolves to the provider's confirmed
// "no data here" response, the build must settle as clean (not degraded)
// with the composite's pre-filled fallback color left in place. Confirmed
// live on a real flight, not just theorized.
func TestBuildMipmap_AllChunksNonJPEGIsNotDegraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("placeholder, not a jpeg"))
	}))
	defer srv.Close()

	// mipmap 4 collapses the fetch grid to a single chunk (quickZoom's
	// maxFetchDepth), keeping this test fast and focused on one chunk's
	// outcome rather than 256.
	const mipmapIdx = maxFetchDepth
	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10 + maxFetchDepth,
		Maptype:  "TEST",
		MinZoom:  10,
		CacheDir: t.TempDir(),
		Provider: testProviderFor(srv.URL),
		Client:   &http.Client{},
		MaxWait:  200 * time.Millisecond, // generous -- this test is about the non-degraded outcome, not timing
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
	}

	tl.BuildMipmap(context.Background(), mipmapIdx)

	if tl.FallbackChunkCount() != 0 {
		t.Errorf("FallbackChunkCount() = %d, want 0 -- a confirmed no-data response must not count as a MaxWait timeout miss", tl.FallbackChunkCount())
	}
	if !tl.DDS.IsRetrieved(mipmapIdx) {
		t.Error("mipmap should be marked Retrieved -- a confirmed-empty result is still a settled, final build")
	}
}
