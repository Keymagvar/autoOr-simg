package tile

import "container/heap"
import "sync"

// FetchPool is a fixed-size worker pool draining ONE shared priority queue
// of chunk-fetch jobs submitted by every Tile in the process — the Go
// port's equivalent of getortho.py's ChunkGetter: a single global pool of
// workers (sized by config.AutoOrtho.FetchThreads, default 32, matching
// Python's own fetch_threads default), not a per-tile or per-build
// concurrency cap.
//
// Why this exists (the "Berlin" finding): the earlier design
// gave each Tile its own private per-build semaphore for chunk fetches,
// nested inside a separate semaphore bounding how many Tiles could build
// at once. Under light load (one quiet airport) that worked fine. Under
// heavy load — a wide aerial view over a dense city needing hundreds of
// distinct tiles at once — only as many tiles as the outer semaphore
// allowed could make ANY progress at all; the rest sat completely
// starved for the entire load, each blocked on its own FUSE Read() call
// waiting for a build slot that might not open up for many minutes. A
// real flight over Berlin reproduced exactly this: a handful of tiles
// fully resolved, hundreds of others showed nothing but the flat
// fallback pattern, for 20+ minutes.
//
// Python never had this problem because it never had this shape: one
// flat worker pool, shared by every tile's every chunk request, so under
// the same heavy load every tile gets a fair share of the 32 workers
// instead of a few tiles monopolizing all available fetch concurrency
// while everything else starves.
type FetchPool struct {
	mu     sync.Mutex
	cond   *sync.Cond
	heap   fetchJobHeap
	seq    int64
	closed bool

	// wg lets Close block until every worker has actually exited, not
	// just been told to — found necessary after a real, 100%-reproducible
	// cross-test data race: a worker already mid-job (dequeued, running
	// job.run()) when Close is called keeps running that one job to
	// completion regardless, and the old Close returned immediately
	// without waiting for that, so a caller had no way to know the pool
	// was genuinely done. Waiting for wg closes that gap.
	wg sync.WaitGroup
}

// fetchJob is one chunk-fetch task. priority mirrors getortho.py's
// Chunk.priority (the provider zoom level being fetched): lower values
// are serviced first, so coarser/more foundational imagery a tile needs
// early in its mipmap chain doesn't get stuck behind finer detail other
// tiles are fetching — same fairness-by-zoom Python's PriorityQueue gives
// for free. seq is a FIFO tie-breaker for equal-priority jobs.
type fetchJob struct {
	priority int
	seq      int64
	run      func()
}

type fetchJobHeap []*fetchJob

func (h fetchJobHeap) Len() int { return len(h) }
func (h fetchJobHeap) Less(i, j int) bool {
	if h[i].priority != h[j].priority {
		return h[i].priority < h[j].priority
	}
	return h[i].seq < h[j].seq
}
func (h fetchJobHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *fetchJobHeap) Push(x any)   { *h = append(*h, x.(*fetchJob)) }
func (h *fetchJobHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// NewFetchPool starts a pool of `workers` persistent goroutines draining
// a shared priority queue — created once per Cacher (like buildSem
// already was) and shared by reference across every Tile that Cacher
// creates, so fetch concurrency is bounded process-wide, not per-tile.
// workers<=0 falls back to 32, matching config.AutoOrtho.FetchThreads'
// own default.
func NewFetchPool(workers int) *FetchPool {
	if workers <= 0 {
		workers = 32
	}
	p := &FetchPool{}
	p.cond = sync.NewCond(&p.mu)
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go p.workerLoop()
	}
	return p
}

func (p *FetchPool) workerLoop() {
	defer p.wg.Done()
	for {
		p.mu.Lock()
		for len(p.heap) == 0 && !p.closed {
			p.cond.Wait()
		}
		if len(p.heap) == 0 && p.closed {
			p.mu.Unlock()
			return
		}
		job := heap.Pop(&p.heap).(*fetchJob)
		p.mu.Unlock()
		job.run()
	}
}

// Submit enqueues run to be executed on a worker goroutine once one is
// free, ordered by priority (lower first) among everything else
// currently queued from any Tile. Returns immediately — run's completion
// is signalled however the caller chose to (fetchComposite uses a
// results channel, matching its existing pattern).
func (p *FetchPool) Submit(priority int, run func()) {
	p.mu.Lock()
	p.seq++
	heap.Push(&p.heap, &fetchJob{priority: priority, seq: p.seq, run: run})
	p.mu.Unlock()
	p.cond.Signal()
}

// Close stops every worker goroutine once the queue drains, and blocks
// until every one of them has actually exited — not just been signalled —
// before returning. Not currently called in production (the pool lives
// for the process's entire lifetime, like buildSem); exists for test
// cleanup, where "exactly when is this pool truly done" matters: a caller
// that doesn't wait has no way to know a worker isn't still mid-job,
// still capable of touching whatever shared/global state that job reaches
// into, well after Close returns and the next test has already started.
func (p *FetchPool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.cond.Broadcast()
	p.wg.Wait()
}
