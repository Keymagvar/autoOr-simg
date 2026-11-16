package scenery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestInstalledOrthoDirs covers the helper Release.Install uses to compute
// OrthoDirs from the actual package list: distinct, sorted, no duplicates
// even though every "z" package shares one InstallDir.
func TestInstalledOrthoDirs(t *testing.T) {
	pkgs := []*Package{
		{Name: "z_asi_00", InstallDir: "/x/z_ao_asi"},
		{Name: "z_asi_01", InstallDir: "/x/z_ao_asi"},
		{Name: "y_asi_overlays", InstallDir: "/x/yAutoOrtho_Overlays"},
	}
	got := installedOrthoDirs(pkgs)
	want := []string{"/x/yAutoOrtho_Overlays", "/x/z_ao_asi"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

// TestRelease_Load_BackfillsMissingOrthoDirs is a regression test for the
// real bug: this port's Parse() only ever read ortho_prefix/overlay_prefix
// from the upstream region_info.json (never ortho_dirs — that field isn't
// even present there), so OrthoDirs was never populated and every
// previously-saved info.json on disk has "ortho_dirs": null. That silently
// broke both Release.Uninstall (a no-op with nothing to iterate) and the
// "Verify" button. Load must reconstruct OrthoDirs from the fixed
// install-layout convention and persist the fix.
func TestRelease_Load_BackfillsMissingOrthoDirs(t *testing.T) {
	installDir := t.TempDir()
	region := "asi"

	zDir := zInstallDir(installDir, region)
	yDir := yInstallDir(installDir)
	if err := os.MkdirAll(zDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(yDir, 0o755); err != nil {
		t.Fatal(err)
	}

	infoPath := filepath.Join(installDir, "z_autoortho", region+"_info.json")
	if err := os.MkdirAll(filepath.Dir(infoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Mirrors a real on-disk info.json from before this fix: ortho_dirs
	// explicitly null.
	raw := `{"name":"asi","display_name":"Asia Ortho Set","ver":"0.0.44-1","info_ver":"v2","ortho_dirs":null,"download_count":42352,"installed":true,"downloaded":true}`
	if err := os.WriteFile(infoPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRelease(region, installDir, t.TempDir())
	if err := r.Load(infoPath); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(r.OrthoDirs) != 2 {
		t.Fatalf("expected 2 backfilled OrthoDirs, got %v", r.OrthoDirs)
	}

	// The fix must also be persisted, not just held in memory, so a
	// future Uninstall (a fresh process, Region.findExisting -> Load
	// again) still has it.
	data, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["ortho_dirs"] == nil {
		t.Error("expected backfilled ortho_dirs to be persisted back to info.json, still null")
	}
}

// TestRelease_Load_NoBackfillWhenDirsDontExist ensures a region whose
// convention directories genuinely don't exist (e.g. a differently-laid-
// out legacy install, or metadata for a region never actually extracted)
// doesn't get fabricated paths that would make Verify report false
// problems about directories that were never meant to exist.
func TestRelease_Load_NoBackfillWhenDirsDontExist(t *testing.T) {
	installDir := t.TempDir()
	infoPath := filepath.Join(installDir, "sa_info.json")
	raw := `{"name":"sa","ver":"1.0","info_ver":"v2","ortho_dirs":null,"installed":true,"downloaded":true}`
	if err := os.WriteFile(infoPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRelease("sa", installDir, t.TempDir())
	if err := r.Load(infoPath); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(r.OrthoDirs) != 0 {
		t.Errorf("expected no backfilled dirs when none exist on disk, got %v", r.OrthoDirs)
	}
}
