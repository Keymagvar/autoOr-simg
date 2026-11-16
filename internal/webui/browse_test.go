package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestAPIBrowse_ListsOnlyDirectories(t *testing.T) {
	srv, _, _ := newTestServer(t)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "scenery"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".hidden_but_real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not_a_dir.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + srv.Addr() + "/api/browse?path=" + dir)
	if err != nil {
		t.Fatalf("GET /api/browse: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var br browseResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	names := map[string]bool{}
	for _, e := range br.Entries {
		names[e.Name] = true
	}
	if !names["scenery"] {
		t.Errorf("entries missing real subdirectory %q: %+v", "scenery", br.Entries)
	}
	// Real X-Plane installs commonly live under a dotfile directory
	// (~/.local/share/Steam/...) — the picker must not hide those.
	if !names[".hidden_but_real"] {
		t.Errorf("entries missing dotfile subdirectory, got: %+v", br.Entries)
	}
	if names["not_a_dir.txt"] {
		t.Errorf("entries should not include plain files, got: %+v", br.Entries)
	}
	if br.Path != dir {
		t.Errorf("Path = %q, want %q", br.Path, dir)
	}
	if br.Parent != filepath.Dir(dir) {
		t.Errorf("Parent = %q, want %q", br.Parent, filepath.Dir(dir))
	}
}

func TestAPIBrowse_DefaultsToHomeDirWhenPathOmitted(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp, err := http.Get("http://" + srv.Addr() + "/api/browse")
	if err != nil {
		t.Fatalf("GET /api/browse: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available in this environment")
	}
	var br browseResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if br.Path != home {
		t.Errorf("Path = %q, want home dir %q", br.Path, home)
	}
}

func TestAPIBrowse_RejectsNonexistentPath(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp, err := http.Get("http://" + srv.Addr() + "/api/browse?path=/this/does/not/exist/at/all")
	if err != nil {
		t.Fatalf("GET /api/browse: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAPIBrowse_FileArgumentFallsBackToItsDirectory(t *testing.T) {
	srv, _, _ := newTestServer(t)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "somefile.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + srv.Addr() + "/api/browse?path=" + filePath)
	if err != nil {
		t.Fatalf("GET /api/browse: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var br browseResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if br.Path != dir {
		t.Errorf("Path = %q, want the file's containing directory %q", br.Path, dir)
	}
}
