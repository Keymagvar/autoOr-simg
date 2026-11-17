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

// countingDecoder wraps a real ImageDecoder and counts every call —
// used to prove fetchOne skips the decode step entirely for a chunk whose
// fetchComposite call has already given up waiting on it.
type countingDecoder struct {
	inner native.ImageDecoder
	calls atomic.Int64
}

func (d *countingDecoder) Decode(jpegBytes []byte) (*native.RGBAImage, error) {
	d.calls.Add(1)
	return d.inner.Decode(jpegBytes)
}

// TestAbandonedFetchSkipsDecode is the regression test for the real OOM
// crash in the Berlin case: a pprof heap profile taken
// moments before a kernel OOM-kill showed internal/native.DecodeJPEGTurbo
// alone holding ~30GB of inuse_space (87% of the whole heap), fed entirely
// by chunk fetches whose fetchComposite caller had already timed out and
// moved on. The disk cache is warmed by chunk.Fetch regardless (see
// TestMaxWaitDeadline) — decoding into a full RGBA buffer for a result
// nobody will ever read was pure waste. This test fails without the
// `abandoned` check in fetchOne (tile.go).
func TestAbandonedFetchSkipsDecode(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	const slowDelay = 1 * time.Second
	const maxWait = 200 * time.Millisecond // must be well under slowDelay
	// Widened from 60ms/300ms: this test only has 2 chunks total (not the
	// 256-way burst the fetchPool/connection-pooling fixes elsewhere in
	// this package address), so it was never a concurrency bottleneck —
	// a real windows-latest CI run still failed here because 60ms isn't
	// enough margin for one real HTTP round-trip + JPEG decode on a
	// loaded CI VM. The actual invariant under test (fast chunk decodes
	// before maxWait, slow chunk doesn't) doesn't care about the absolute
	// values, just that maxWait stays comfortably under slowDelay.

	var slowReqSeen sync.WaitGroup
	slowReqSeen.Add(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("c") == "slow" {
			slowReqSeen.Done()
			time.Sleep(slowDelay)
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
			if col == 1 && row == 0 {
				c = "slow"
			}
			return srv.URL + "/tile?c=" + c
		},
		NumServers: 1,
		MaxThreads: 8,
		UserAgent:  "test",
	}

	decoder := &countingDecoder{inner: native.DefaultDecoder()}

	tl := &Tile{
		Row: 0, Col: 0, Zoom: 10,
		Maptype:  "TEST",
		MinZoom:  10,
		CacheDir: cacheDir,
		Provider: testProvider,
		Client:   &http.Client{},
		MaxWait:  maxWait,
		DDS:      NewDDS(GridSize*256, GridSize*256, decoder, native.DefaultCompressor()),
		log:      noopLogger{},
	}

	// 2x1 grid: col 0 is fast (decoded before the deadline), col 1 is the
	// slow chunk fetchComposite will give up waiting on.
	_, degraded := tl.fetchComposite(context.Background(), 0, 0, 2, 1, 10)
	if !degraded {
		t.Fatal("fetchComposite reported degraded=false, want true")
	}

	callsAtReturn := decoder.calls.Load()
	if callsAtReturn != 1 {
		t.Fatalf("decode calls right after fetchComposite returned = %d, want 1 (only the fast chunk)", callsAtReturn)
	}

	// Let the slow chunk's background fetch actually finish, well past
	// slowDelay, then confirm decode was never called for it.
	slowReqSeen.Wait()
	time.Sleep(2 * slowDelay)

	if got := decoder.calls.Load(); got != 1 {
		t.Errorf("decode calls after the abandoned slow fetch finished = %d, want 1 — the abandoned chunk was decoded anyway", got)
	}
}
