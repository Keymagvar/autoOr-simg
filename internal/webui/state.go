package webui

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sync"

	"autoOr-simg/internal/config"
	"autoOr-simg/internal/memwatch"
)

const logRingSize = 500

// RegionView is one entry in the Scenery block — install/update state for
// one downloadable region, the Go equivalent of config_ui.py's per-region
// "Install"/"Updated!" row.
type RegionView struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	LatestVersion   string  `json:"latest_version"`
	LocalVersion    string  `json:"local_version,omitempty"`
	SizeMB          float64 `json:"size_mb"`
	UpToDate        bool    `json:"up_to_date"`
	Installed       bool    `json:"installed"`
	Downloading     bool    `json:"downloading"`
	DownloadPercent float64 `json:"download_percent"`
	DownloadStatus  string  `json:"download_status,omitempty"`
	// Phase/PackageIndex/PackageTotal mirror scenery.Progress, so the UI can
	// show e.g. "verifying package 3 of 12" instead of freezing at "100%"
	// once bytes stop moving while CRC-check/assemble/extract still run.
	Phase        string `json:"phase,omitempty"`
	PackageIndex int    `json:"package_index,omitempty"`
	PackageTotal int    `json:"package_total,omitempty"`
	Error        string `json:"error,omitempty"`
	// Verifying is true while a "Verify" click is checking an installed
	// region's scenery directories are still on disk (see
	// scenery.Release.VerifyInstalled) — the result itself isn't stored
	// here, it's a one-off region_verified event (see broadcastVerifyResult),
	// since it's a point-in-time check result, not ongoing state.
	Verifying bool `json:"verifying,omitempty"`
	// Mounted reflects whether this region's z_ao_<id> directory is
	// currently live-mounted under X-Plane's Custom Scenery — distinct
	// from Installed (on disk in AutoOrtho's own scenery staging area).
	// Recomputed by refreshRegionMountFlags after every mount/unmount
	// action, global or per-region, so a row never shows a stale state.
	Mounted bool `json:"mounted,omitempty"`
}

// ConfigView is the subset of config.Config the page can read and write —
// DESIGN.md §3's Setup/Zoom & quality blocks. Paths are editable text,
// MinZoom/MaxWait/Maptypes are the "newly exposed" fields the design calls
// out explicitly as previously ini-file-only.
type ConfigView struct {
	XplanePath            string   `json:"xplane_path"`
	SceneryPath           string   `json:"scenery_path"`
	CacheDir              string   `json:"cache_dir"`
	DownloadDir           string   `json:"download_dir"`
	FileCacheSizeGB       int      `json:"file_cache_size_gb"`
	MaptypeOverride       string   `json:"maptype_override"`
	MinZoom               int      `json:"min_zoom"`
	MaxZoom               int      `json:"max_zoom"`
	MaxWait               float64  `json:"maxwait"`
	Maptypes              []string `json:"maptypes"`
	MaxConcurrentBuilds   int      `json:"max_concurrent_builds"`
	FetchThreads          int      `json:"fetch_threads"`
	MaxOpenTiles          int      `json:"max_open_tiles"`
	PerformanceMode       string   `json:"performance_mode"`
	DisableCoarseFallback bool     `json:"disable_coarse_fallback"`
	AutoOpenUI            bool     `json:"auto_open_ui"`
}

func configViewFrom(cfg *config.Config) ConfigView {
	return ConfigView{
		XplanePath:            cfg.Paths.XplanePath,
		SceneryPath:           cfg.Paths.SceneryPath,
		CacheDir:              cfg.Paths.CacheDir,
		DownloadDir:           cfg.Paths.DownloadDir,
		FileCacheSizeGB:       cfg.Cache.FileCacheSizeGB,
		MaptypeOverride:       cfg.AutoOrtho.MaptypeOverride,
		MinZoom:               cfg.AutoOrtho.MinZoom,
		MaxZoom:               cfg.AutoOrtho.MaxZoom,
		MaxWait:               cfg.AutoOrtho.MaxWait,
		Maptypes:              cfg.AutoOrtho.Maptypes,
		MaxConcurrentBuilds:   cfg.AutoOrtho.MaxConcurrentBuilds,
		FetchThreads:          cfg.AutoOrtho.FetchThreads,
		MaxOpenTiles:          cfg.AutoOrtho.MaxOpenTiles,
		PerformanceMode:       cfg.AutoOrtho.PerformanceMode,
		DisableCoarseFallback: cfg.AutoOrtho.DisableCoarseFallback,
		AutoOpenUI:            cfg.General.AutoOpenUI,
	}
}

// Stats is the stats block's live counters. Deliberately small: only what
// is actually wired up today (Cacher.Len(), process RSS) — there's no
// general metrics pipeline yet. Not inventing numbers nothing in this
// codebase tracks.
type Stats struct {
	CachedTiles    int    `json:"cached_tiles"`
	OpenTiles      int    `json:"open_tiles"`
	ProcessRSS     uint64 `json:"process_rss_bytes"`
	TotalSystemMem uint64 `json:"total_system_mem_bytes"`
}

// Snapshot is the full state sent as the first message on every new
// WebSocket connection (DESIGN.md §3: "renders the real current state
// immediately, never a default/blank one").
type Snapshot struct {
	Running bool          `json:"running"`
	Mounts  []MountStatus `json:"mounts"`
	Config  ConfigView    `json:"config"`
	Regions []RegionView  `json:"regions"`
	Stats   Stats         `json:"stats"`
	Logs    []string      `json:"logs"`
}

// AppState is the single in-process source of truth DESIGN.md §3 requires:
// one mutex-guarded struct, with every control sending a command that
// mutates it server-side and then broadcasts the result — never a
// client-side-only state flip. Implements io.Writer so it can register
// itself via logging.AddSink and receive every log line as it's written.
type AppState struct {
	mu sync.Mutex

	cfg     *config.Config
	engine  Engine
	regions map[string]RegionView

	logRing []string

	hub *Hub
}

func NewAppState(cfg *config.Config, engine Engine) *AppState {
	return &AppState{
		cfg:     cfg,
		engine:  engine,
		regions: map[string]RegionView{},
		hub:     newHub(),
	}
}

// Write implements io.Writer for logging.AddSink: every log line gets
// appended to the ring buffer and pushed to connected clients as a
// log_line event, with no separate polling/tailing mechanism (DESIGN.md
// §3's "Logs" paragraph).
func (s *AppState) Write(p []byte) (int, error) {
	line := string(bytes.TrimRight(p, "\n"))
	if line == "" {
		return len(p), nil
	}
	s.mu.Lock()
	s.logRing = append(s.logRing, line)
	if len(s.logRing) > logRingSize {
		s.logRing = s.logRing[len(s.logRing)-logRingSize:]
	}
	s.mu.Unlock()

	s.hub.broadcast(event{Type: "log_line", Data: line})
	return len(p), nil
}

// Snapshot renders the current state for a newly connected client or a
// REST poll (GET /api/state).
func (s *AppState) Snapshot() Snapshot {
	s.mu.Lock()
	cfgView := configViewFrom(s.cfg)
	regions := make([]RegionView, 0, len(s.regions))
	for _, r := range s.regions {
		regions = append(regions, r)
	}
	logs := make([]string, len(s.logRing))
	copy(logs, s.logRing)
	s.mu.Unlock()

	mounts := s.engine.MountStatuses()
	return Snapshot{
		Running: len(mounts) > 0,
		Mounts:  mounts,
		Config:  cfgView,
		Regions: regions,
		Stats:   s.currentStats(),
		Logs:    logs,
	}
}

func (s *AppState) currentStats() Stats {
	st := Stats{CachedTiles: s.engine.CachedTiles(), OpenTiles: s.engine.OpenTiles()}
	if rss, err := memwatch.ReadRSSBytes(); err == nil {
		st.ProcessRSS = rss
	}
	if total, err := memwatch.TotalSystemMemoryBytes(); err == nil {
		st.TotalSystemMem = total
	}
	return st
}

// setRegions replaces the whole Scenery-block region list (called after a
// scenery.Manager.FindRegions refresh) and broadcasts the change.
func (s *AppState) setRegions(views []RegionView) {
	s.mu.Lock()
	s.regions = make(map[string]RegionView, len(views))
	for _, v := range views {
		s.regions[v.ID] = v
	}
	regions := make([]RegionView, 0, len(views))
	for _, r := range s.regions {
		regions = append(regions, r)
	}
	s.mu.Unlock()
	s.hub.broadcast(event{Type: "regions_changed", Data: regions})
}

// region returns a copy of one region's current view (zero value if
// unknown), for a caller about to patch a few fields and updateRegion it
// back — avoids reaching into AppState's map without the lock.
func (s *AppState) region(id string) RegionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.regions[id]
}

// updateRegion patches one region's view (install progress/result) and
// broadcasts just that one, so an in-flight download can update its own
// row repeatedly without resending the whole list each time.
func (s *AppState) updateRegion(v RegionView) {
	s.mu.Lock()
	s.regions[v.ID] = v
	s.mu.Unlock()
	s.hub.broadcast(event{Type: "region_changed", Data: v})
}

// broadcastMountState re-reads mount status from the engine and fans it
// out — called after every mount/stop command so every connected tab
// (including the one that clicked) learns the new state from the server,
// never from its own click handler (DESIGN.md §3).
func (s *AppState) broadcastMountState() {
	mounts := s.engine.MountStatuses()
	s.hub.broadcast(event{Type: "mount_state_changed", Data: map[string]any{
		"running": len(mounts) > 0,
		"mounts":  mounts,
	}})
}

// refreshRegionMountFlags recomputes every known region's Mounted field
// from the engine's live mount status (by the z_ao_<id> directory-naming
// convention internal/scenery's installer uses) and broadcasts the whole
// list if anything actually changed. Called after any mount/unmount
// action — global (doMount/doStop/doShutdown/doRestart) or per-region
// (doMountRegion/doUnmountRegion) — so every connected tab's Scenery rows
// stay accurate no matter which control changed the mount set.
func (s *AppState) refreshRegionMountFlags(engine Engine) {
	mountedDirs := map[string]bool{}
	for _, m := range engine.MountStatuses() {
		mountedDirs[filepath.Base(m.Mount)] = true
	}

	s.mu.Lock()
	changed := false
	for id, v := range s.regions {
		isMounted := mountedDirs["z_ao_"+id]
		if v.Mounted != isMounted {
			v.Mounted = isMounted
			s.regions[id] = v
			changed = true
		}
	}
	regions := make([]RegionView, 0, len(s.regions))
	for _, r := range s.regions {
		regions = append(regions, r)
	}
	s.mu.Unlock()

	if changed {
		s.hub.broadcast(event{Type: "regions_changed", Data: regions})
	}
}

func (s *AppState) broadcastStats() {
	s.hub.broadcast(event{Type: "stats_tick", Data: s.currentStats()})
}

func (s *AppState) broadcastConfigSaved() {
	s.mu.Lock()
	cfgView := configViewFrom(s.cfg)
	s.mu.Unlock()
	s.hub.broadcast(event{Type: "config_saved", Data: cfgView})
}

func (s *AppState) broadcastError(msg string) {
	s.hub.broadcast(event{Type: "error", Data: msg})
}

// broadcastVerifyResult reports the outcome of a "Verify" click — a
// point-in-time check, not state to persist on the region row itself (the
// row's own Error field is reserved for an actual install/download
// failure).
func (s *AppState) broadcastVerifyResult(id, name string, ok bool, message string) {
	s.hub.broadcast(event{Type: "region_verified", Data: map[string]any{
		"id": id, "name": name, "ok": ok, "message": message,
	}})
}

type event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

func (e event) marshal() []byte {
	b, _ := json.Marshal(e)
	return b
}

// snapshotEvent wraps a Snapshot the same way other events are wrapped, so
// the client's one WebSocket message handler can switch on "type" for
// every message including the very first one.
func snapshotEvent(snap Snapshot) []byte {
	b, _ := json.Marshal(event{Type: "snapshot", Data: snap})
	return b
}
