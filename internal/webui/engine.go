package webui

import "context"

// MountStatus describes one active scenery mount, for the state snapshot's
// Scenery/Run blocks.
type MountStatus struct {
	Root  string `json:"root"`
	Mount string `json:"mount"`
}

// Engine is the thin seam between the web UI and the real mount/cache
// machinery in cmd/autoortho/main.go (internal/lifecycle.Manager +
// internal/tile.Cacher). Kept as an interface, not a concrete dependency
// on those packages, so this package stays about HTTP/WebSocket plumbing
// and state broadcasting, not FUSE lifecycle itself.
type Engine interface {
	// Mount mounts every currently configured scenery region. Safe to call
	// again after Stop — DESIGN.md §8's "Run" after a "Stop".
	Mount(ctx context.Context) ([]MountStatus, error)
	// Stop unmounts everything but leaves the process (and the tile cache)
	// alive — DESIGN.md §8's "Stop", distinct from Shutdown.
	Stop()
	// MountOne mounts a single scenery region (root -> mount), independent
	// of every other mount — the Scenery block's per-region mount toggle,
	// as opposed to Mount's all-at-once startup behavior. Safe to call
	// again after UnmountOne for the same region.
	MountOne(ctx context.Context, root, mount string) (MountStatus, error)
	// UnmountOne unmounts a single region previously mounted via MountOne
	// (or Mount) without touching any other mount. Returns an error if
	// mount isn't currently mounted.
	UnmountOne(mount string) error
	// MountStatuses reports whatever is currently mounted.
	MountStatuses() []MountStatus
	// CachedTiles reports the tile cache's current size, for the stats
	// block (DESIGN.md §9's Cacher.Len(), already exposed for exactly
	// this kind of reporting).
	CachedTiles() int
	// OpenTiles reports how many cached tiles currently have Refs>0 —
	// Cacher.OpenCount(), the quantity the MaxOpenTiles budget
	// actually bounds (distinct from CachedTiles, which also counts idle,
	// evictable tiles). Added so the Stats block can show this real,
	// already-wired counter instead of it being invisible outside logs.
	OpenTiles() int
}
