package tile

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"autoOr-simg/internal/provider"
)

// perRequestTimeout bounds a single chunk's HTTP round trip (dial + headers
// + body). Added after a real, severe finding: Cacher.DefaultClient uses
// http.Client{Timeout: 0} deliberately (DESIGN.md's "no blanket deadline on
// what may be a slow tile fetch" — so a genuinely slow-but-progressing
// transfer isn't aborted just because a Tile's own MaxWait window passed;
// background fetches are meant to keep running and warm the disk cache).
// But with chunk fetches now running on a small, fixed-size shared
// FetchPool (fetchpool.go) rather than one goroutine per chunk, an actual
// hang — not just slowness — is a different problem: a worker pool
// processes one job per goroutine, synchronously, to completion, so a
// single TCP connection that stalls forever (a real, documented Starlink
// failure mode — satellite handoffs can produce a connection that neither
// completes nor errors) permanently removes that worker from the pool for
// the rest of the process's life. Over a short synthetic test this never
// shows up; over a real, sustained flight, each dropout has some chance of
// claiming a worker, and workers lost this way never come back — enough of
// them lost over enough minutes and the whole pool stops making progress,
// while still-running builds pile up unbounded composite-buffer memory
// behind it. 30s is generous headroom over any real chunk fetch (a single
// small JPEG) while still bounding the worst case to something finite.
// A var, not a const, so tests can shrink it instead of waiting out a real
// 30-second hang (matching tile.go's repairDelay/maxRepairAttempts pattern).
var perRequestTimeout = 30 * time.Second

// Chunk is one 256x256 provider tile. Mirrors getortho.py's Chunk: fetched
// over HTTP, cached to disk as a raw JPEG, decoded only when actually
// composited into a Tile.
type Chunk struct {
	Col, Row, Zoom int
	Maptype        string
	CacheDir       string
}

func (c *Chunk) cachePath() string {
	return filepath.Join(c.CacheDir, fmt.Sprintf("%d_%d_%d_%s.jpg", c.Col, c.Row, c.Zoom, c.Maptype))
}

// isJPEG checks the standard JPEG magic bytes — getortho.py does the same
// check (providers occasionally return an HTML error page or empty body
// with a 200 status).
func isJPEG(data []byte) bool {
	return len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
}

// GetCached returns this chunk's raw JPEG bytes if already on disk, without
// ever touching the network — used by getBestCachedChunk to look for a
// coarser, already-fetched stand-in for a chunk that isn't ready yet
// (getortho.py's Tile.get_best_chunk's `c.get_cache()` call).
func (c *Chunk) GetCached() ([]byte, bool) {
	data, err := os.ReadFile(c.cachePath())
	if err != nil || !isJPEG(data) {
		return nil, false
	}
	return data, true
}

// Fetch returns this chunk's raw JPEG bytes, from disk cache if present,
// otherwise over HTTP from the given provider (rotating across its
// serverIdx-th subdomain, per DESIGN.md §5). A (nil, nil) return is a
// distinct, deliberate third outcome from (data, nil)/(nil, err) — see its
// doc comment at the isJPEG check below for what it means and why it's not
// folded into the error case.
func (c *Chunk) Fetch(ctx context.Context, client *http.Client, p provider.Provider, serverIdx int) ([]byte, error) {
	if data, err := os.ReadFile(c.cachePath()); err == nil && isJPEG(data) {
		return data, nil
	}

	ctx, cancel := context.WithTimeout(ctx, perRequestTimeout)
	defer cancel()

	url := p.TileURL(c.Col, c.Row, c.Zoom, serverIdx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.UserAgent)
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chunk fetch %s: status %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !isJPEG(data) {
		// A 200 response that isn't a real JPEG (providers occasionally
		// return a small placeholder/error image with a 200 status, e.g.
		// Bing's "no imagery at this zoom here" tile) is the provider's
		// own confirmed, final answer: this ground has no data at this
		// zoom, not a transient failure to retry. getortho.py's Chunk.get
		// treats it the same way — sets empty data and still returns
		// success (ready.set(); return True) — rather than an error.
		// Returning (nil, nil) here, not an error, is what lets the
		// caller (fetchOne in tile.go) honor that distinction: treated as
		// an error, this would count as "missing" at MaxWait and trigger
		// the background-repair retry loop, re-asking the same provider
		// the same already-answered question every repairDelay for
		// maxRepairAttempts — a real, observed problem (repeated
		// "giving up on background repair" spam for a spot with no
		// coverage at any retry count, confirmed by hand: the same
		// quadkey returns this placeholder regardless of the request's
		// cache-busting parameter). Deliberately NOT written to disk
		// cache below (matching getortho.py's save_cache, which no-ops on
		// falsy/empty data) — a provider's coverage can change, so this
		// isn't memoized as permanent, just not treated as an in-flight
		// failure for this one attempt.
		return nil, nil
	}

	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0o755); err == nil {
			_ = os.WriteFile(c.cachePath(), data, 0o644) // cache write failures are non-fatal
		}
	}
	return data, nil
}
