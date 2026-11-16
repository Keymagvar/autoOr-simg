package tile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"autoOr-simg/internal/native"
	"autoOr-simg/internal/provider"
)

// TestGetBestCachedChunk_UsedOnHardFailure is the regression test for a
// real quality gap found by comparing against getortho.py (the original
// Python implementation): a chunk that fails outright (network error, bad
// response) used to leave its region as a flat fallback color for the rest
// of that build attempt, producing visibly blocky tile boundaries during
// heavy load. Python's Tile.get_best_chunk instead searches already-cached
// coarser zoom levels and shows a blurry-but-correctly-positioned stand-in
// — this test proves the Go port's getBestCachedChunk equivalent does the
// same, by pre-seeding the disk cache with a coarser chunk and making the
// finer one fail outright.
func TestGetBestCachedChunk_UsedOnHardFailure(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("c") == "fail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	const zoom = 10
	const failCol, failRow = 1, 0 // grid cell (c=1, r=0)

	// Pre-seed the disk cache with a coarser (zoom-1) chunk covering the
	// same ground area as the failing fine chunk, so getBestCachedChunk
	// has something to find.
	coarsePath := filepath.Join(cacheDir, "0_0_9_TEST.jpg") // (failCol>>1, failRow>>1, zoom-1)
	if err := os.WriteFile(coarsePath, jpegData, 0o644); err != nil {
		t.Fatal(err)
	}

	testProvider := provider.Provider{
		Name: "TEST",
		TileURL: func(col, row, z, serverIdx int) string {
			c := "fast"
			if col == failCol && row == failRow {
				c = "fail"
			}
			return srv.URL + "/tile?c=" + c
		},
		NumServers: 1,
		MaxThreads: 8,
		UserAgent:  "test",
	}

	tl := &Tile{
		Row: 0, Col: 0, Zoom: zoom,
		Maptype:  "TEST",
		MinZoom:  5,
		CacheDir: cacheDir,
		Provider: testProvider,
		Client:   &http.Client{},
		MaxWait:  2 * time.Second, // generous: this test is about the hard-failure path, not the timeout path
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
	}

	composite, _ := tl.fetchComposite(context.Background(), 0, 0, 2, 1, zoom)

	// The failing chunk's region (grid cell c=1,r=0 -> pixels x in [256,512)) should NOT
	// be the flat fallback color -- getBestCachedChunk should have filled it
	// with a cropped/scaled stand-in instead.
	off := (0*composite.Width + 256) * 4
	got := composite.Pix[off : off+4]
	if got[0] == fallbackColor[0] && got[1] == fallbackColor[1] && got[2] == fallbackColor[2] {
		t.Errorf("failing chunk's region is still flat fallback color %v, want a cached-chunk stand-in", fallbackColor)
	}
}

// TestGetBestCachedChunk_NoCacheAvailable confirms the fallback color is
// still used (not a crash, not garbage) when no coarser cached chunk exists
// at all -- getBestCachedChunk must return ok=false cleanly rather than
// panicking or looping forever.
func TestGetBestCachedChunk_NoCacheAvailable(t *testing.T) {
	cacheDir := t.TempDir()

	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:  "TEST",
		MinZoom:  5,
		CacheDir: cacheDir,
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
	}

	img, ok := tl.getBestCachedChunk(1, 0, 10)
	if ok {
		t.Errorf("getBestCachedChunk returned ok=true with no cache present, img=%v", img)
	}
}
