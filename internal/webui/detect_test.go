package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// setTestHome points os.UserHomeDir() at dir for the duration of the test.
// Setting HOME alone only works on Unix — Go's os.UserHomeDir() reads
// %USERPROFILE% on Windows instead and ignores HOME entirely, so a test
// that only set HOME was silently testing against the real CI runner's
// actual home directory on Windows, not its own temp dir (confirmed the
// hard way: this detect-xplane suite found zero candidates on a real
// windows-latest run for exactly this reason). Setting both is harmless
// cross-platform — Go only ever reads the one that matches the current
// OS — so this one helper works everywhere instead of needing a
// runtime.GOOS branch at every call site.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func mkXPlaneInstall(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "Custom Scenery"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func detectXPlane(t *testing.T, srv *Server) []xplaneCandidate {
	t.Helper()
	resp, err := http.Get("http://" + srv.Addr() + "/api/detect-xplane")
	if err != nil {
		t.Fatalf("GET /api/detect-xplane: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Candidates []xplaneCandidate `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return out.Candidates
}

func TestAPIDetectXPlane_FindsDefaultSteamLibraryInstall(t *testing.T) {
	srv, _, _ := newTestServer(t)
	home := t.TempDir()
	setTestHome(t, home)

	xp := filepath.Join(home, ".local", "share", "Steam", "steamapps", "common", "X-Plane 12")
	mkXPlaneInstall(t, xp)

	candidates := detectXPlane(t, srv)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly 1", candidates)
	}
	if candidates[0].Path != xp {
		t.Errorf("Path = %q, want %q", candidates[0].Path, xp)
	}
	if candidates[0].Label != "X-Plane 12" {
		t.Errorf("Label = %q, want %q", candidates[0].Label, "X-Plane 12")
	}
}

func TestAPIDetectXPlane_IgnoresFolderMissingCustomScenery(t *testing.T) {
	srv, _, _ := newTestServer(t)
	home := t.TempDir()
	setTestHome(t, home)

	// A folder that merely starts with "X-Plane" but isn't a real install
	// (no Custom Scenery, no Resources) must not be reported as a hit.
	fake := filepath.Join(home, ".local", "share", "Steam", "steamapps", "common", "X-Plane Demo Videos")
	if err := os.MkdirAll(fake, 0o755); err != nil {
		t.Fatal(err)
	}

	if candidates := detectXPlane(t, srv); len(candidates) != 0 {
		t.Errorf("candidates = %+v, want none", candidates)
	}
}

func TestAPIDetectXPlane_FindsInstallInSecondarySteamLibrary(t *testing.T) {
	srv, _, _ := newTestServer(t)
	home := t.TempDir()
	setTestHome(t, home)

	secondLibrary := t.TempDir()
	xp := filepath.Join(secondLibrary, "steamapps", "common", "X-Plane 11")
	mkXPlaneInstall(t, xp)

	vdfDir := filepath.Join(home, ".local", "share", "Steam", "steamapps")
	if err := os.MkdirAll(vdfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	vdf := `"libraryfolders"
{
	"0"
	{
		"path"		"` + secondLibrary + `"
	}
}
`
	if err := os.WriteFile(filepath.Join(vdfDir, "libraryfolders.vdf"), []byte(vdf), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates := detectXPlane(t, srv)
	if len(candidates) != 1 || candidates[0].Path != xp {
		t.Fatalf("candidates = %+v, want exactly [%s]", candidates, xp)
	}
}

func TestAPIDetectXPlane_NoCandidatesWhenNothingFound(t *testing.T) {
	srv, _, _ := newTestServer(t)
	setTestHome(t, t.TempDir())

	resp, err := http.Get("http://" + srv.Addr() + "/api/detect-xplane")
	if err != nil {
		t.Fatalf("GET /api/detect-xplane: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Candidates []xplaneCandidate `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if out.Candidates == nil {
		t.Error("candidates should serialize as [], not null, when empty")
	}
	if len(out.Candidates) != 0 {
		t.Errorf("candidates = %+v, want none", out.Candidates)
	}
}
