// Package webui is the Go equivalent of config_ui.py (the retired
// PySimpleGUI desktop window) and flighttrack.py's map/stats routes,
// rebuilt as DESIGN.md §3 specifies: one page, one net/http server, one
// WebSocket per browser tab, server-authoritative state. See AppState
// (state.go) for the single source of truth and Hub (hub.go) for the
// broadcaster every connected tab shares.
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"autoOr-simg/internal/tile"

	"github.com/gorilla/websocket"

	"autoOr-simg/internal/config"
	"autoOr-simg/internal/logging"
	"autoOr-simg/internal/scenery"
)

//go:embed web
var webFS embed.FS

// Server is the net/http + WebSocket server DESIGN.md §3 describes,
// bound to loopback only by default (§10).
type Server struct {
	cfg        *config.Config
	engine     Engine
	sceneryMgr *scenery.Manager
	state      *AppState
	log        *slog.Logger

	httpSrv  *http.Server
	upgrader websocket.Upgrader

	// baseCtx is the server/app-lifetime context (set from Run's ctx), used
	// for work that must outlive any single WebSocket connection — e.g. a
	// scenery install shouldn't abort just because the browser tab that
	// started it was reloaded or closed. Using a per-request r.Context()
	// for that would cancel it the moment the WS connection drops.
	baseCtx context.Context

	shutdownRequested chan struct{}
	restartRequested  chan struct{}
}

// New builds a Server. confFile is the path config.Config was loaded from
// (needed again here so Save-from-the-UI writes back to the same file).
// sceneryMgr may be nil if no scenery.Manager could be constructed (the
// Scenery block then reports no regions rather than failing startup).
func New(cfg *config.Config, engine Engine, sceneryMgr *scenery.Manager) *Server {
	state := NewAppState(cfg, engine)
	logging.AddSink(state)

	return &Server{
		cfg:        cfg,
		engine:     engine,
		sceneryMgr: sceneryMgr,
		state:      state,
		log:        logging.For("webui"),
		upgrader:   websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024},
		// Buffered so a handler never blocks trying to signal shutdown.
		shutdownRequested: make(chan struct{}, 1),
		restartRequested:  make(chan struct{}, 1),
	}
}

// ShutdownRequested fires once if the page's Shutdown action is used — the
// real entrypoint (cmd/autoortho/main.go's runCmd) selects on this
// alongside ctx.Done() and the memory watchdog's Critical channel, so a
// web-UI-triggered shutdown runs through the exact same guaranteed
// unmount-then-exit sequence as a SIGINT (DESIGN.md §4c/§8).
func (s *Server) ShutdownRequested() <-chan struct{} {
	return s.shutdownRequested
}

// RestartRequested fires once if the page's Restart action is used — same
// select-alongside-ctx.Done()/Critical pattern as ShutdownRequested, so a
// web-UI-triggered restart runs through the exact same guaranteed
// unmount-then-release-lock sequence, except runCmd re-execs the binary
// afterward instead of letting the process exit. See cmd/autoortho's
// reExecSelf.
func (s *Server) RestartRequested() <-chan struct{} {
	return s.restartRequested
}

// Addr is "127.0.0.1:<port>" — DESIGN.md §10: bind to loopback by default,
// never 0.0.0.0, so this isn't reachable from the rest of the LAN without
// the user deliberately choosing to expose it some other way.
func (s *Server) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.cfg.FlightData.WebUIPort)
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts
// the server down with a bounded grace period. Intended to run in its own
// goroutine from runCmd.
func (s *Server) Run(ctx context.Context) error {
	s.baseCtx = ctx

	mux := http.NewServeMux()
	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("webui: embedding static assets: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webRoot)))
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/state", s.handleAPIState)
	mux.HandleFunc("/api/browse", s.handleAPIBrowse)
	mux.HandleFunc("/api/browse/native", s.handleAPIBrowseNative)
	mux.HandleFunc("/api/detect-xplane", s.handleAPIDetectXPlane)

	s.httpSrv = &http.Server{Addr: s.Addr(), Handler: mux}

	s.refreshRegions(ctx) // best-effort initial Scenery-block population

	ln, err := net.Listen("tcp", s.Addr())
	if err != nil {
		return fmt.Errorf("webui: listening on %s: %w", s.Addr(), err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- s.httpSrv.Serve(ln) }()

	// Periodic stats_tick (DESIGN.md §3's live stats counters) — cheap
	// enough to just always run; the page only renders it if it's on
	// screen.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.httpSrv.Shutdown(shutdownCtx)
			return nil
		case err := <-errCh:
			if err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("webui: server exited: %w", err)
			}
			return nil
		case <-ticker.C:
			s.state.broadcastStats()
		}
	}
}

// URL is the address to open a browser to / show in the startup toast.
func (s *Server) URL() string {
	return "http://" + s.Addr()
}

func (s *Server) handleAPIState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.state.Snapshot())
}

// handleWS upgrades to a WebSocket and runs DESIGN.md §3's exact protocol:
// a full Snapshot first, then incremental events, with every inbound
// message treated as a command the server acts on and then broadcasts the
// result of — never a client-side state mutation.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("websocket upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	ch := s.state.hub.subscribe()
	defer s.state.hub.unsubscribe(ch)

	if err := conn.WriteMessage(websocket.TextMessage, snapshotEvent(s.state.Snapshot())); err != nil {
		return
	}

	writeErr := make(chan struct{})
	go func() {
		defer close(writeErr)
		for msg := range ch {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			s.log.Info("websocket disconnected", "err", err)
			<-writeErr
			return
		}
		var cmd command
		if err := json.Unmarshal(data, &cmd); err != nil {
			s.state.broadcastError(fmt.Sprintf("bad command: %v", err))
			continue
		}
		s.handleCommand(r.Context(), cmd)
	}
}

type command struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// handleCommand is DESIGN.md §3's "every control sends a command, not a
// local state change" — each case performs the real action, mutates
// AppState, then broadcasts; it never writes a response back to just the
// caller.
func (s *Server) handleCommand(ctx context.Context, cmd command) {
	switch cmd.Action {
	case "mount":
		s.doMount(ctx)
	case "stop":
		s.doStop()
	case "shutdown":
		s.doShutdown()
	case "restart":
		s.doRestart()
	case "save_config":
		s.doSaveConfig(cmd.Data)
	case "clean_cache":
		s.doCleanCache()
	case "install_region":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(cmd.Data, &payload); err != nil || payload.ID == "" {
			s.state.broadcastError("install_region: missing region id")
			return
		}
		// Use baseCtx, not the WS request's ctx: this goroutine must
		// survive the browser tab reloading or closing (see baseCtx's
		// doc comment).
		go s.doInstallRegion(s.baseCtx, payload.ID)
	case "refresh_regions":
		go s.refreshRegions(s.baseCtx)
	case "verify_region":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(cmd.Data, &payload); err != nil || payload.ID == "" {
			s.state.broadcastError("verify_region: missing region id")
			return
		}
		go s.doVerifyRegion(payload.ID)
	case "mount_region":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(cmd.Data, &payload); err != nil || payload.ID == "" {
			s.state.broadcastError("mount_region: missing region id")
			return
		}
		go s.doMountRegion(s.baseCtx, payload.ID)
	case "unmount_region":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(cmd.Data, &payload); err != nil || payload.ID == "" {
			s.state.broadcastError("unmount_region: missing region id")
			return
		}
		go s.doUnmountRegion(payload.ID)
	default:
		s.state.broadcastError(fmt.Sprintf("unknown action %q", cmd.Action))
	}
}

func (s *Server) doMount(ctx context.Context) {
	if _, err := s.engine.Mount(ctx); err != nil {
		s.log.Error("mount requested from web UI failed", "err", err)
		s.state.broadcastError(fmt.Sprintf("mount failed: %v", err))
	}
	s.state.broadcastMountState()
	s.state.refreshRegionMountFlags(s.engine)
}

func (s *Server) doStop() {
	s.engine.Stop()
	s.state.broadcastMountState()
	s.state.refreshRegionMountFlags(s.engine)
}

// sceneryMountFor finds the one config.SceneryMount matching a region id,
// by the z_ao_<id> directory-naming convention internal/scenery's
// installer uses for both its own staging dir (Root) and the X-Plane
// Custom Scenery mountpoint (Mount) — see internal/scenery's zInstallDir
// and config.ComputeDerived. Returns ok=false if the region isn't
// installed (no matching entry in cfg.SceneryMounts yet).
func sceneryMountFor(cfg *config.Config, id string) (config.SceneryMount, bool) {
	dirName := "z_ao_" + id
	for _, sm := range cfg.SceneryMounts {
		if filepath.Base(sm.Root) == dirName {
			return sm, true
		}
	}
	return config.SceneryMount{}, false
}

// doMountRegion is the Scenery block's per-region "Mount" toggle (the
// other half of doUnmountRegion) — mounts exactly one already-installed
// region without touching any other currently-mounted one, unlike the
// global doMount above.
func (s *Server) doMountRegion(ctx context.Context, id string) {
	s.state.mu.Lock()
	sm, ok := sceneryMountFor(s.cfg, id)
	s.state.mu.Unlock()
	if !ok {
		s.state.broadcastError(fmt.Sprintf("mount_region: region %q is not installed", id))
		return
	}

	if _, err := s.engine.MountOne(ctx, sm.Root, sm.Mount); err != nil {
		s.log.Error("mount_region failed", "region", id, "err", err)
		s.state.broadcastError(fmt.Sprintf("mounting %s failed: %v", id, err))
		return
	}
	s.state.broadcastMountState()
	s.state.refreshRegionMountFlags(s.engine)
}

// doUnmountRegion is doMountRegion's sibling: unmounts exactly one region,
// leaving every other currently-mounted region untouched — unlike the
// global doStop above, which tears down everything.
func (s *Server) doUnmountRegion(id string) {
	s.state.mu.Lock()
	sm, ok := sceneryMountFor(s.cfg, id)
	s.state.mu.Unlock()
	if !ok {
		s.state.broadcastError(fmt.Sprintf("unmount_region: region %q is not installed", id))
		return
	}

	if err := s.engine.UnmountOne(sm.Mount); err != nil {
		s.log.Error("unmount_region failed", "region", id, "err", err)
		s.state.broadcastError(fmt.Sprintf("unmounting %s failed: %v", id, err))
		return
	}
	s.state.broadcastMountState()
	s.state.refreshRegionMountFlags(s.engine)
}

// RequestStop runs the same Stop action the web UI's own Stop button
// triggers (doStop above) — exported so another UI surface (the systray
// menu, internal/tray) can reach it without duplicating the logic.
func (s *Server) RequestStop() {
	s.doStop()
}

// RequestShutdown runs the same Shutdown action the web UI's own Shutdown
// button triggers (doShutdown below) — exported for internal/tray's
// Shutdown menu item, same reasoning as RequestStop.
func (s *Server) RequestShutdown() {
	s.doShutdown()
}

// doShutdown is DESIGN.md §8's Shutdown: Stop, then signal runCmd to run
// the rest of the guaranteed sequence (release the lock, exit). It
// deliberately does not release the lock or exit itself — that stays
// owned by the one guaranteed-shutdown path in cmd/autoortho/main.go so
// there is exactly one place that sequence lives, matching §4(c).
func (s *Server) doShutdown() {
	s.engine.Stop()
	s.state.broadcastMountState()
	select {
	case s.shutdownRequested <- struct{}{}:
	default:
	}
}

// RequestRestart runs the same Restart action the web UI's own Restart
// button triggers (doRestart below) — exported for parity with
// RequestStop/RequestShutdown, in case another UI surface wants it later.
func (s *Server) RequestRestart() {
	s.doRestart()
}

// doRestart is doShutdown's sibling: identical Stop-then-signal sequence,
// but signals restartRequested instead — runCmd runs the exact same
// unmount-then-release-lock cleanup either way, then re-execs the binary
// instead of letting the process exit. Deliberately does not re-exec
// itself, same reasoning as doShutdown not releasing the lock itself: the
// guaranteed sequence lives in exactly one place (cmd/autoortho/main.go).
func (s *Server) doRestart() {
	s.engine.Stop()
	s.state.broadcastMountState()
	select {
	case s.restartRequested <- struct{}{}:
	default:
	}
}

func (s *Server) doSaveConfig(data json.RawMessage) {
	var in ConfigView
	if err := json.Unmarshal(data, &in); err != nil {
		s.state.broadcastError(fmt.Sprintf("save_config: %v", err))
		return
	}

	s.state.mu.Lock()
	cfg := s.cfg
	cfg.Paths.XplanePath = in.XplanePath
	cfg.Paths.SceneryPath = in.SceneryPath
	cfg.Paths.CacheDir = in.CacheDir
	cfg.Paths.DownloadDir = in.DownloadDir
	cfg.Cache.FileCacheSizeGB = in.FileCacheSizeGB
	cfg.AutoOrtho.MaptypeOverride = in.MaptypeOverride
	cfg.AutoOrtho.MinZoom = in.MinZoom
	cfg.AutoOrtho.MaxZoom = in.MaxZoom
	cfg.AutoOrtho.MaxWait = in.MaxWait
	cfg.AutoOrtho.MaxConcurrentBuilds = in.MaxConcurrentBuilds
	// FetchThreads, like MaxConcurrentBuilds, sizes a fixed-capacity
	// pool/channel created once when the Cacher starts (internal/tile's
	// FetchPool / buildSem) — saving here persists it for the *next*
	// restart, not live.
	cfg.AutoOrtho.FetchThreads = in.FetchThreads
	cfg.AutoOrtho.MaxOpenTiles = in.MaxOpenTiles
	cfg.AutoOrtho.PerformanceMode = in.PerformanceMode
	cfg.AutoOrtho.DisableCoarseFallback = in.DisableCoarseFallback
	cfg.General.AutoOpenUI = in.AutoOpenUI
	s.state.mu.Unlock()

	if err := cfg.Save(); err != nil {
		s.state.broadcastError(fmt.Sprintf("saving config: %v", err))
		return
	}
	// Apply live, not just on next restart: zoomDropForLoad reads its thresholds on
	// every call, not once at Tile-creation time, so the very next
	// saturated build picks up a changed performance mode immediately —
	// no restart needed, unlike MaxConcurrentBuilds/FetchThreads above.
	tile.SetPerformanceMode(cfg.AutoOrtho.PerformanceMode)
	// Also live, not just on next restart — getBestCachedChunk reads
	// disableCoarseFallback on every call, not once at Tile-creation time.
	tile.SetDisableCoarseFallback(cfg.AutoOrtho.DisableCoarseFallback)
	s.state.broadcastConfigSaved()
}

// doCleanCache is config_ui.py's "Clean Cache" button: trims the on-disk
// tile cache down to the configured size limit, oldest files first.
func (s *Server) doCleanCache() {
	s.state.mu.Lock()
	cacheDir := s.cfg.Paths.CacheDir
	sizeGB := s.cfg.Cache.FileCacheSizeGB
	s.state.mu.Unlock()

	go func() {
		n, freed, err := cleanCache(cacheDir, sizeGB)
		if err != nil {
			s.state.broadcastError(fmt.Sprintf("clean cache: %v", err))
			return
		}
		s.log.Info("cache cleaned from web UI", "files_removed", n, "bytes_freed", freed)
		s.state.hub.broadcast(event{Type: "cache_cleaned", Data: map[string]any{
			"files_removed": n,
			"bytes_freed":   freed,
		}})
	}()
}

// refreshRegions re-scans the scenery downloader's known regions (the Go
// equivalent of config_ui.py's refresh_scenery()) and populates the
// Scenery block.
func (s *Server) refreshRegions(ctx context.Context) {
	if s.sceneryMgr == nil {
		return
	}
	if err := s.sceneryMgr.FindRegions(ctx); err != nil {
		s.log.Warn("refreshing scenery region list failed", "err", err)
		s.state.broadcastError(fmt.Sprintf("refreshing scenery list: %v", err))
		return
	}

	views := make([]RegionView, 0, len(s.sceneryMgr.Regions))
	for id, region := range s.sceneryMgr.Regions {
		latest := region.GetLatestRelease()
		if latest == nil {
			continue
		}
		parseErr := latest.Parse(ctx, s.sceneryMgr.Client)
		views = append(views, regionViewFrom(id, region, latest, parseErr))
	}
	s.state.setRegions(views)
	s.state.refreshRegionMountFlags(s.engine)
}

// regionViewFrom builds one Scenery-block row from a region's already-known
// local-install state plus whatever remote metadata Parse managed to fetch
// (parseErr non-nil if it didn't). Split out from refreshRegions as a pure
// function specifically so the real bug this once had — losing a region's
// already-known Installed/LocalVersion the instant the *unrelated* remote
// Parse() call failed (a GitHub rate limit, a timeout, a transient CDN
// hiccup — nothing to do with whether the region is installed, which is a
// purely local file read done once when the Region was constructed) — has
// a regression test that doesn't depend on forcing a real network failure.
func regionViewFrom(id string, region *scenery.Region, latest *scenery.Release, parseErr error) RegionView {
	v := RegionView{ID: id, Name: id}
	if region.LocalRel != nil {
		v.Installed = true
		v.LocalVersion = region.LocalRel.Ver
		v.UpToDate = region.LocalRel.Ver == latest.Ver
	}

	if parseErr != nil {
		v.Error = parseErr.Error()
		return v
	}

	v.Name = latest.DisplayName
	if v.Name == "" {
		v.Name = id
	}
	v.LatestVersion = latest.Ver
	v.SizeMB = float64(latest.TotalSize) / 1048576
	v.UpToDate = region.LocalRel != nil && region.LocalRel.Ver == latest.Ver
	return v
}

// doInstallRegion is config_ui.py's per-region "Install" button: downloads
// and installs (or updates) one region, pushing live progress to every
// connected tab as it goes.
func (s *Server) doInstallRegion(ctx context.Context, id string) {
	if s.sceneryMgr == nil {
		s.state.broadcastError("install_region: no scenery manager available")
		return
	}
	region, ok := s.sceneryMgr.Regions[id]
	if !ok {
		s.state.broadcastError(fmt.Sprintf("install_region: unknown region %q", id))
		return
	}

	s.log.Info("scenery install starting", "region", id)

	v := s.state.region(id)
	v.Downloading = true
	v.Error = ""
	s.state.updateRegion(v)

	ok2, err := region.InstallRelease(ctx, s.sceneryMgr.Client, "", func(p scenery.Progress) {
		v := s.state.region(id)
		v.Downloading = true
		v.DownloadPercent = p.PercentDone
		v.DownloadStatus = p.Status
		v.Phase = p.Phase
		v.PackageIndex = p.PackageIndex
		v.PackageTotal = p.PackageTotal
		s.state.updateRegion(v)
	})

	v.Downloading = false
	if err != nil || !ok2 {
		msg := "install failed"
		if err != nil {
			msg = err.Error()
		}
		v.Error = msg
		v.Phase = "failed"
		s.state.updateRegion(v)
		if ctx.Err() != nil {
			s.log.Warn("scenery install cancelled", "region", id, "ctx_err", ctx.Err(), "err", err)
		} else {
			s.log.Error("scenery install failed", "region", id, "err", err)
		}
		return
	}

	v.Phase = "done"
	latest := region.GetLatestRelease()
	if latest != nil {
		v.Installed = true
		v.LocalVersion = latest.Ver
		v.UpToDate = true
	}
	s.state.updateRegion(v)

	// Without this, a region downloaded mid-session never becomes
	// mountable: runEngine.Mount loops over cfg.SceneryMounts, which was
	// only ever computed once at process startup from whatever scenery
	// existed then. Clicking Run afterward would silently do nothing —
	// zero configured mounts, no error — even though the files are right
	// there on disk. See ComputeDerived's own doc comment.
	if err := s.cfg.ComputeDerived(); err != nil {
		s.log.Error("failed to refresh scenery mounts after install", "region", id, "err", err)
	}

	// First-install plumbing a user would otherwise have to do by hand:
	// make sure the mountpoint directory actually exists under X-Plane's
	// Custom Scenery (it's also created lazily on actual mount, by
	// internal/fuse's ensureMountpoint,
	// but a user shouldn't have to mount first just to see the folder is
	// there), and register it in scenery_packs.ini so X-Plane picks it up
	// without a manual edit. Both are idempotent — safe on every install,
	// not just the first; neither failure should block reporting the
	// install itself as successful, since the scenery is genuinely
	// installed either way.
	s.state.mu.Lock()
	sm, haveMount := sceneryMountFor(s.cfg, id)
	customSceneryPath := s.cfg.XplaneCustomSceneryPath
	s.state.mu.Unlock()
	if haveMount {
		if err := os.MkdirAll(sm.Mount, 0o755); err != nil {
			s.log.Error("failed to create Custom Scenery mount directory after install", "region", id, "path", sm.Mount, "err", err)
		}
		if err := scenery.EnsureSceneryPackRegistered(customSceneryPath, filepath.Base(sm.Root)); err != nil {
			s.log.Error("failed to register scenery pack in scenery_packs.ini", "region", id, "err", err)
		}
	}

	s.log.Info("scenery installed from web UI", "region", id)
}

// doVerifyRegion is the "Verify" button: a point-in-time check that an
// already-installed region's scenery directories are actually still on
// disk. It's deliberately not a byte-level integrity check — the
// downloaded zip is deleted after a successful install (Release.Cleanup),
// so there's nothing left locally to checksum against; this only catches
// "did something delete/move/empty out what I installed", not silent
// in-place file corruption.
func (s *Server) doVerifyRegion(id string) {
	if s.sceneryMgr == nil {
		s.state.broadcastError("verify_region: no scenery manager available")
		return
	}
	region, ok := s.sceneryMgr.Regions[id]
	if !ok {
		s.state.broadcastError(fmt.Sprintf("verify_region: unknown region %q", id))
		return
	}
	if region.LocalRel == nil {
		s.state.broadcastError(fmt.Sprintf("verify_region: %q is not installed", id))
		return
	}

	v := s.state.region(id)
	v.Verifying = true
	s.state.updateRegion(v)

	ok2, message, err := region.LocalRel.VerifyInstalled()

	v.Verifying = false
	s.state.updateRegion(v)

	name := v.Name
	if name == "" {
		name = id
	}
	if err != nil {
		s.log.Error("scenery verify errored", "region", id, "err", err)
		s.state.broadcastVerifyResult(id, name, false, err.Error())
		return
	}
	s.log.Info("scenery verified", "region", id, "ok", ok2, "message", message)
	s.state.broadcastVerifyResult(id, name, ok2, message)
}
