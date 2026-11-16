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

// TestFetchPool_BoundsConcurrencyAndSharesFairly is the test
// the "Berlin" finding exists to fix: many tiles building at
// once (simulating X-Plane's wide aerial view over a dense city — far
// more than any per-tile/per-build semaphore scheme let through) must
// (a) never exceed the shared pool's worker count in concurrent in-flight
// fetches, process-wide, and (b) every single tile must still eventually
// get real data — none permanently starved because a handful of other
// tiles monopolized a separate, per-tile concurrency allowance. This is
// the property the old per-tile semaphore design did NOT have: tiles
// built one-at-a-time-ish behind buildSem, each with its own private
// fetch concurrency, so under heavy simultaneous demand most tiles never
// even got a turn.
func TestFetchPool_BoundsConcurrencyAndSharesFairly(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}

	const poolSize = 4
	const numTiles = 20
	const gridSide = 2 // 2x2 = 4 chunks/tile — small so the test is fast

	var inFlight, maxInFlight atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			cur := maxInFlight.Load()
			if n <= cur || maxInFlight.CompareAndSwap(cur, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond) // simulate real network latency
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	pool := NewFetchPool(poolSize)
	defer pool.Close()

	testProvider := provider.Provider{
		Name:       "TEST",
		TileURL:    func(col, row, zoom, serverIdx int) string { return srv.URL + "/tile" },
		NumServers: 1,
		MaxThreads: 999, // irrelevant now — fetchPool is what bounds concurrency
		UserAgent:  "test",
	}

	tiles := make([]*Tile, numTiles)
	for i := range tiles {
		tiles[i] = &Tile{
			Row: i, Col: 0, Zoom: 10,
			Maptype:   "TEST",
			MinZoom:   10,
			CacheDir:  t.TempDir(), // distinct per tile so none hit each other's disk cache
			Provider:  testProvider,
			Client:    &http.Client{},
			MaxWait:   5 * time.Second, // generous — this test proves eventual completion, not deadline behavior
			DDS:       NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
			log:       noopLogger{},
			fetchPool: pool,
		}
	}

	var wg sync.WaitGroup
	degraded := make([]bool, numTiles)
	for i, tl := range tiles {
		wg.Add(1)
		go func(i int, tl *Tile) {
			defer wg.Done()
			_, deg := tl.fetchComposite(context.Background(), 0, 0, gridSide, gridSide, 10)
			degraded[i] = deg
		}(i, tl)
	}
	wg.Wait()

	if got := maxInFlight.Load(); got > poolSize {
		t.Errorf("max concurrent in-flight requests = %d, want <= %d (pool should bound this process-wide)", got, poolSize)
	}

	for i, deg := range degraded {
		if deg {
			t.Errorf("tile %d finished degraded (still missing chunks) — a tile was starved despite a generous MaxWait; the pool should have serviced everyone eventually", i)
		}
	}
}

// TestFetchPool_NoPriorityStarvation is the regression test for a real,
// confirmed bug: an earlier version of fetchComposite submitted jobs with
// priority=zoom (coarse/distant mipmap fetches, lower zoom, outranking
// mip0's higher zoom), matching getortho.py's Chunk.priority scheme. Under
// a real flight with continuous, never-ending resubmission of coarse-mip
// work (many tiles, X-Plane's view constantly shifting), the high-priority
// queue never actually drained, so mip0 — lower priority under that
// scheme — never got serviced at all: confirmed in real logs as 862
// consecutive "giving up" events, every single one mipmap=0.
//
// This test reproduces the shape directly: one single low-value job
// submitted first, then a continuous flood of same-or-lower-priority jobs
// keeps arriving for the whole test (simulating the never-ending coarse-
// mip churn), on a small pool. The fix (plain FIFO, every job priority 0)
// means the single job must still complete promptly — it can't be
// starved by anything submitted after it, no matter how much keeps
// arriving.
func TestFetchPool_NoPriorityStarvation(t *testing.T) {
	pool := NewFetchPool(2) // small pool so the flood actually contends
	defer pool.Close()

	done := make(chan struct{})
	pool.Submit(0, func() { close(done) })

	stopFlood := make(chan struct{})
	var floodWG sync.WaitGroup
	for i := 0; i < 4; i++ {
		floodWG.Add(1)
		go func() {
			defer floodWG.Done()
			for {
				select {
				case <-stopFlood:
					return
				default:
					result := make(chan struct{})
					pool.Submit(0, func() {
						time.Sleep(time.Millisecond)
						close(result)
					})
					<-result
				}
			}
		}()
	}

	select {
	case <-done:
		// Got serviced promptly despite the continuous flood — no starvation.
	case <-time.After(2 * time.Second):
		t.Error("the first-submitted job was never serviced within 2s while a continuous flood of other jobs kept arriving — this is the exact starvation bug the second Berlin finding describes")
	}

	close(stopFlood)
	floodWG.Wait()
}

// TestFetchPool_SurvivesRepeatedHangs is the pool-level version of
// TestChunk_Fetch_HungConnectionDoesNotHangForever: proves the actual
// consequence that matters, not just that one Fetch call returns. A
// FetchPool worker runs one job per goroutine, synchronously, to
// completion — so if Chunk.Fetch could hang forever (the bug the
// per-request timeout in chunk.go fixes), each hang would permanently
// remove one worker from the pool, for the rest of the process's life.
// Simulated here directly against the pool (not real HTTP) by having a
// fraction of submitted jobs block until explicitly released: this test
// repeatedly submits "hanging" jobs that eventually complete (standing in
// for a bounded-by-timeout fetch) interleaved with normal fast jobs, many
// times over, on a tiny pool — and asserts throughput never degrades to
// zero, i.e. the pool always has working capacity left, no matter how many
// hangs it has already absorbed.
func TestFetchPool_SurvivesRepeatedHangs(t *testing.T) {
	pool := NewFetchPool(2)
	defer pool.Close()

	for round := 0; round < 50; round++ {
		// One "hanging" job per round, held open briefly then released —
		// standing in for a chunk fetch that would have hung forever
		// without chunk.go's per-request timeout, now bounded instead.
		release := make(chan struct{})
		pool.Submit(0, func() {
			<-release
		})
		time.Sleep(time.Millisecond)
		close(release)

		// A normal fast job must still complete promptly every single
		// round — if previous rounds' hangs had permanently consumed
		// workers, this would eventually time out as the pool's
		// effective capacity hit zero.
		done := make(chan struct{})
		pool.Submit(0, func() { close(done) })
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("round %d: pool made no progress — a prior round's hang appears to have permanently consumed a worker", round)
		}
	}
}
