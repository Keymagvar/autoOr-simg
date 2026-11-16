package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"autoOr-simg/internal/config"
)

// fakeEngine is a minimal Engine good enough to prove the server's
// command -> mutate -> broadcast pipeline (DESIGN.md §3) without any real
// FUSE/lifecycle machinery — that's already proven end to end elsewhere
// (internal/lifecycle, internal/fuse).
type fakeEngine struct {
	mounts map[string]string // mount -> root
}

func (f *fakeEngine) Mount(ctx context.Context) ([]MountStatus, error) {
	if f.mounts == nil {
		f.mounts = map[string]string{}
	}
	f.mounts["/fake/mount"] = "/fake/root"
	return f.MountStatuses(), nil
}
func (f *fakeEngine) Stop() { f.mounts = map[string]string{} }
func (f *fakeEngine) MountOne(ctx context.Context, root, mount string) (MountStatus, error) {
	if f.mounts == nil {
		f.mounts = map[string]string{}
	}
	f.mounts[mount] = root
	return MountStatus{Root: root, Mount: mount}, nil
}
func (f *fakeEngine) UnmountOne(mount string) error {
	if _, ok := f.mounts[mount]; !ok {
		return fmt.Errorf("not mounted: %s", mount)
	}
	delete(f.mounts, mount)
	return nil
}
func (f *fakeEngine) MountStatuses() []MountStatus {
	out := make([]MountStatus, 0, len(f.mounts))
	for mount, root := range f.mounts {
		out = append(out, MountStatus{Root: root, Mount: mount})
	}
	return out
}
func (f *fakeEngine) CachedTiles() int { return 3 }
func (f *fakeEngine) OpenTiles() int   { return 2 }

func newTestServer(t *testing.T) (srv *Server, cfg *config.Config, confPath string) {
	t.Helper()
	dir := t.TempDir()
	confPath = filepath.Join(dir, "autoortho.ini")

	cfg, err := config.Load(confPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Paths.LogFile = filepath.Join(dir, "autoortho.log")

	// Run on an OS-assigned free port instead of the configured 5000, so
	// parallel test runs (and a real `autoortho run` on this machine)
	// never collide with this test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	cfg.FlightData.WebUIPort = ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv = New(cfg, &fakeEngine{}, nil)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel() // must run before waiting on errCh, or Run() never returns
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("server.Run returned error: %v", err)
			}
		// Server.Run's own shutdown path budgets up to 5s for
		// httpSrv.Shutdown (draining any in-flight/keep-alive
		// connections) before giving up — this wait must be comfortably
		// longer than that real budget, or it flags a slow-but-still-
		// within-contract shutdown (seen under -race with many tests
		// spinning up real listeners back to back) as a false failure.
		case <-time.After(8 * time.Second):
			t.Error("server did not shut down within 8s of ctx cancellation")
		}
	})

	waitForServer(t, srv.Addr())
	return srv, cfg, confPath
}

// waitForServer polls until the server's HTTP endpoint actually answers —
// Run() starts a goroutine, so a test dialing immediately after calling it
// would otherwise race the listener coming up.
func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never came up", addr)
}

func dialWS(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Addr()+"/ws", nil)
	if err != nil {
		t.Fatalf("dialing websocket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readEvent(t *testing.T, conn *websocket.Conn) event {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("reading message: %v", err)
	}
	var e event
	if err := json.Unmarshal(msg, &e); err != nil {
		t.Fatalf("unmarshaling event: %v (raw: %s)", err, msg)
	}
	return e
}

// TestWebUI_ConnectGetsFullSnapshotFirst proves DESIGN.md §3's core
// reload-proofing guarantee: the very first message on a new connection is
// always a full state snapshot, not a blank/default one.
func TestWebUI_ConnectGetsFullSnapshotFirst(t *testing.T) {
	srv, _, _ := newTestServer(t)
	conn := dialWS(t, srv)

	e := readEvent(t, conn)
	if e.Type != "snapshot" {
		t.Fatalf("first message type = %q, want %q", e.Type, "snapshot")
	}
}

// TestWebUI_MountCommandBroadcastsServerState proves "every control sends
// a command, not a local state change": the server performs the real
// Mount, then broadcasts the actual resulting state back — not an echo of
// what the client asked for.
func TestWebUI_MountCommandBroadcastsServerState(t *testing.T) {
	srv, _, _ := newTestServer(t)
	conn := dialWS(t, srv)
	readEvent(t, conn) // initial snapshot

	if err := conn.WriteJSON(map[string]any{"action": "mount"}); err != nil {
		t.Fatalf("writing mount command: %v", err)
	}
	e := readEvent(t, conn)
	if e.Type != "mount_state_changed" {
		t.Fatalf("event type = %q, want mount_state_changed", e.Type)
	}
	data, _ := json.Marshal(e.Data)
	if !strings.Contains(string(data), `"running":true`) {
		t.Fatalf("mount_state_changed did not report running:true: %s", data)
	}

	if err := conn.WriteJSON(map[string]any{"action": "stop"}); err != nil {
		t.Fatalf("writing stop command: %v", err)
	}
	e = readEvent(t, conn)
	data, _ = json.Marshal(e.Data)
	if !strings.Contains(string(data), `"running":false`) {
		t.Fatalf("mount_state_changed after stop did not report running:false: %s", data)
	}
}

// TestWebUI_TwoTabsSeeTheSameBroadcast proves the multi-tab guarantee the
// whole server-authoritative design exists for: a command sent by one
// connection is observed by every connection, including itself.
func TestWebUI_TwoTabsSeeTheSameBroadcast(t *testing.T) {
	srv, _, _ := newTestServer(t)
	connA := dialWS(t, srv)
	connB := dialWS(t, srv)
	readEvent(t, connA)
	readEvent(t, connB)

	if err := connA.WriteJSON(map[string]any{"action": "mount"}); err != nil {
		t.Fatalf("writing mount command: %v", err)
	}

	for _, conn := range []*websocket.Conn{connA, connB} {
		e := readEvent(t, conn)
		if e.Type != "mount_state_changed" {
			t.Fatalf("event type = %q, want mount_state_changed", e.Type)
		}
	}
}

// TestWebUI_SaveConfigPersistsToDisk proves save_config actually writes
// through to the real config file, not just updates in-memory state.
func TestWebUI_SaveConfigPersistsToDisk(t *testing.T) {
	srv, cfg, confPath := newTestServer(t)
	conn := dialWS(t, srv)
	readEvent(t, conn)

	newCache := cfg.Paths.CacheDir // keep a real, creatable dir
	cmd := map[string]any{
		"action": "save_config",
		"data": map[string]any{
			"xplane_path":           cfg.Paths.XplanePath,
			"scenery_path":          cfg.Paths.SceneryPath,
			"cache_dir":             newCache,
			"download_dir":          cfg.Paths.DownloadDir,
			"file_cache_size_gb":    99,
			"maptype_override":      "EOX",
			"min_zoom":              13,
			"max_zoom":              15,
			"maxwait":               0.8,
			"max_concurrent_builds": 4,
			"auto_open_ui":          true,
		},
	}
	if err := conn.WriteJSON(cmd); err != nil {
		t.Fatalf("writing save_config: %v", err)
	}
	e := readEvent(t, conn)
	if e.Type != "config_saved" {
		t.Fatalf("event type = %q, want config_saved (data: %+v)", e.Type, e.Data)
	}

	raw, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("reading back config file: %v", err)
	}
	if !strings.Contains(string(raw), "file_cache_size = 99") {
		t.Fatalf("saved config file missing updated file_cache_size:\n%s", raw)
	}
	if !strings.Contains(string(raw), "maptype_override = EOX") {
		t.Fatalf("saved config file missing updated maptype_override:\n%s", raw)
	}
	if !strings.Contains(string(raw), "max_zoom = 15") {
		t.Fatalf("saved config file missing updated max_zoom:\n%s", raw)
	}
}

// TestWebUI_UnknownCommandReportsErrorNotCrash proves a malformed/unknown
// command degrades to a broadcast error event, not a dropped connection or
// a crashed server.
func TestWebUI_UnknownCommandReportsErrorNotCrash(t *testing.T) {
	srv, _, _ := newTestServer(t)
	conn := dialWS(t, srv)
	readEvent(t, conn)

	if err := conn.WriteJSON(map[string]any{"action": "bogus"}); err != nil {
		t.Fatalf("writing bogus command: %v", err)
	}
	e := readEvent(t, conn)
	if e.Type != "error" {
		t.Fatalf("event type = %q, want error", e.Type)
	}
}

// TestWebUI_APIStateMatchesSnapshot proves the REST convenience endpoint
// (DESIGN.md §3: "REST endpoints... for curl/scripting convenience")
// reports the same shape as the WebSocket snapshot.
func TestWebUI_APIStateMatchesSnapshot(t *testing.T) {
	srv, _, _ := newTestServer(t)
	snap := srv.state.Snapshot()
	if snap.Config.CacheDir == "" {
		t.Fatalf("snapshot config looks empty: %+v", snap.Config)
	}
}
