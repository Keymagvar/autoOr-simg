package tile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"autoOr-simg/internal/provider"
)

// TestChunk_Fetch_HungConnectionDoesNotHangForever is the regression test
// for a real, severe finding: Cacher.DefaultClient uses
// http.Client{Timeout: 0} deliberately (a genuinely slow-but-progressing
// transfer shouldn't be aborted just because it's slow), but chunk fetches
// now run on a small, fixed-size shared FetchPool (fetchpool.go), one job
// per goroutine, synchronously to completion. A connection that neither
// completes nor errors — a real, documented Starlink failure mode
// (satellite handoffs) — would, without a per-request bound, permanently
// remove that worker from the pool for the rest of the process's life.
// Enough of those over a long enough real flight and the whole pool stops
// making progress while still-running builds pile up unbounded memory
// behind it — plausibly the cause of a real OOM that survived every other
// fix this session. Fetch must return (with an error) within a bounded
// time even against a server that never responds at all.
func TestChunk_Fetch_HungConnectionDoesNotHangForever(t *testing.T) {
	orig := perRequestTimeout
	perRequestTimeout = 50 * time.Millisecond // shrink so the test is fast
	defer func() { perRequestTimeout = orig }()

	neverRespond := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-neverRespond
	}))
	// Order matters: httptest.Server.Close() blocks until every in-flight
	// handler returns, and our handler only returns once neverRespond is
	// closed — so that must happen first. Deferred close(neverRespond) runs
	// after this deferred srv.Close() would, since defers are LIFO; running
	// this one as an explicit statement (not a defer) at the very end
	// instead guarantees the right order without relying on defer ordering.
	defer func() {
		close(neverRespond)
		srv.Close()
	}()

	c := &Chunk{Col: 1, Row: 1, Zoom: 10, Maptype: "TEST", CacheDir: t.TempDir()}
	p := provider.Provider{
		Name:       "TEST",
		TileURL:    func(col, row, zoom, serverIdx int) string { return srv.URL + "/tile" },
		NumServers: 1,
		UserAgent:  "test",
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Fetch(context.Background(), &http.Client{}, p, 0)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Fetch succeeded against a server that never responds — expected a timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Fetch never returned within 2s against a hung connection — this is the exact worker-starvation bug the per-request timeout exists to prevent")
	}
}
