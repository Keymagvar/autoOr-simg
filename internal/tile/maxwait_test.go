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

// TestMaxWaitDeadline is the test this whole change exists to pass: one
// slow chunk in an otherwise-fast grid must not make fetchComposite block
// past MaxWait, the composite must fall back to the flat color for the
// still-missing chunk, AND the slow fetch must keep running in the
// background and still land in the disk cache afterward — the Go
// equivalent of getortho.py's ChunkGetter worker continuing past a
// caller's wait() timeout.
func TestMaxWaitDeadline(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	const slowDelay = 300 * time.Millisecond
	const maxWait = 60 * time.Millisecond // must be well under slowDelay

	var slowRequests, fastRequests atomic.Int64
	var slowReqSeen sync.WaitGroup
	slowReqSeen.Add(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Identify "the slow chunk" by its column query param.
		if r.URL.Query().Get("c") == "slow" {
			slowRequests.Add(1)
			slowReqSeen.Done()
			time.Sleep(slowDelay)
		} else {
			fastRequests.Add(1)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()

	testProvider := provider.Provider{
		Name: "TEST",
		TileURL: func(col, row, zoom, serverIdx int) string {
			c := "fast"
			if col == 1 && row == 0 { // mark exactly one grid cell as the slow one
				c = "slow"
			}
			return srv.URL + "/tile?c=" + c
		},
		NumServers: 1,
		MaxThreads: 8,
		UserAgent:  "test",
	}

	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:  "TEST",
		MinZoom:  10,
		CacheDir: cacheDir,
		Provider: testProvider,
		Client:   &http.Client{},
		MaxWait:  maxWait,
		DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
		log:      noopLogger{},
	}

	// A small 2x2-chunk grid so the test is fast: col 0-1, row 0.
	start := time.Now()
	composite, degraded := tl.fetchComposite(context.Background(), 0, 0, 2, 1, 10)
	elapsed := time.Since(start)

	if !degraded {
		t.Error("fetchComposite reported degraded=false, want true — a chunk missed the deadline")
	}

	if elapsed > maxWait+100*time.Millisecond {
		t.Errorf("fetchComposite took %s, want close to MaxWait (%s) — it waited for the slow chunk instead of giving up", elapsed, maxWait)
	}
	if elapsed >= slowDelay {
		t.Errorf("fetchComposite took %s, which is >= the slow chunk's %s delay — the deadline didn't fire at all", elapsed, slowDelay)
	}

	if tl.FallbackChunkCount() == 0 {
		t.Error("FallbackChunkCount() = 0, want >0 — the deadline path should have recorded a fallback event")
	}

	// The slow chunk's region (grid cell c=1,r=0 -> pixels x in [256,512), y in [0,256))
	// should still be the flat fallback color, since its fetch hadn't
	// completed when we stopped waiting.
	off := (0*composite.Width + 256) * 4
	if composite.Pix[off] != fallbackColor[0] || composite.Pix[off+1] != fallbackColor[1] || composite.Pix[off+2] != fallbackColor[2] {
		t.Errorf("slow chunk's region = %v, want fallback color %v", composite.Pix[off:off+4], fallbackColor)
	}

	// Wait for the background fetch to actually finish (it wasn't
	// cancelled), then confirm it landed in the disk cache — proving the
	// "don't cancel, just stop waiting" design actually works end to end.
	slowReqSeen.Wait()
	deadline := time.Now().Add(2 * slowDelay)
	cachePath := cacheDir + "/1_0_10_TEST.jpg" // col=0+1, row=0+0
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cachePath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected background fetch to eventually cache %s, but it never appeared: %v", cachePath, err)
	}

	if slowRequests.Load() != 1 {
		t.Errorf("slow endpoint hit %d times, want exactly 1", slowRequests.Load())
	}
}

type noopLogger struct{}

func (noopLogger) Debug(msg string, args ...any) {}
func (noopLogger) Info(msg string, args ...any)  {}
func (noopLogger) Warn(msg string, args ...any)  {}
