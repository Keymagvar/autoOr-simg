package tile

import (
	"fmt"
	"os"
	"path/filepath"
)

// bc1CachePath returns the on-disk path for this tile's mipmap idx's
// persisted, already-*compressed* BC1 output — DESIGN.md §12's "persist
// compressed DXT mip output across sessions, not just the raw JPEG
// chunks": compression, not the network fetch, is the real CPU hot spot
// (every pprof heap snapshot this project has taken shows CompressBC1ISPC
// as the dominant cost, not chunk download). Today, even a tile whose raw
// JPEG chunks are all warm in Chunk's own disk cache still redoes the
// full decode+composite+compress pipeline from scratch on every rebuild
// (a process restart, or an evicted tile being reopened) — this cache
// skips that entirely on a hit.
//
// Named distinctly from Chunk's own cache files (col_row_zoom_maptype.jpg)
// so the two can never collide, and tagged with the concrete compressor
// type (ISPCCompressor vs. GoCompressor — see internal/native/
// defaults_*.go) rather than a version number: the two backends are not
// guaranteed to produce byte-identical output for the same input, and
// TextureCompressor has no method to ask an implementation its own name,
// so %T is the simplest correct way to never silently reuse one backend's
// output under a different one (e.g. a cgo build's cache being picked up
// by a later CGO_ENABLED=0 run).
func (t *Tile) bc1CachePath(idx int) string {
	return filepath.Join(t.CacheDir, fmt.Sprintf("bc1_%d_%d_%d_%s_mm%d_%T.bin",
		t.Row, t.Col, t.Zoom, t.Maptype, idx, t.DDS.Compressor))
}

// loadBC1Cache returns mipmap idx's persisted compressed bytes if a
// correctly-sized cache file exists, without touching the network or
// doing any decode/compress work. The length check (against
// DDS.Mipmaps[idx].Length, the exact byte count this mipmap's BC1 data
// must be) is what actually matters here, not the scheme above — it's
// what catches a truncated write from a prior crashed process, since
// file content, unlike Chunk's own JPEG cache, has no independent magic
// bytes of its own to validate against.
func (t *Tile) loadBC1Cache(idx int) ([]byte, bool) {
	if t.CacheDir == "" {
		return nil, false
	}
	data, err := os.ReadFile(t.bc1CachePath(idx))
	if err != nil || len(data) != t.DDS.Mipmaps[idx].Length {
		return nil, false
	}
	return data, true
}

// saveBC1Cache persists mipmap idx's compressed bytes for reuse by a
// future rebuild of this same tile (this session or a later one) — see
// bc1CachePath's doc comment for the full rationale. Callers must only
// ever call this for a build that is both not degraded and allFresh (see
// fetchGrid's doc comment on that distinction): persisting a build that
// quietly includes a confirmed-no-coverage cell or a blurry backup
// stand-in would freeze that imperfection in place across restarts,
// permanently defeating this project's existing repair/self-heal
// mechanism for exactly the tiles that need it most. A write failure is
// non-fatal, same convention as Chunk.Fetch's own disk-cache write.
func (t *Tile) saveBC1Cache(idx int, data []byte) {
	if t.CacheDir == "" {
		return
	}
	if err := os.MkdirAll(t.CacheDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(t.bc1CachePath(idx), data, 0o644)
}
