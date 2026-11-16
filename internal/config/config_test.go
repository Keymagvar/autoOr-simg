package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// pythonWrittenSample is a real ~/.autoortho file as written by the
// original Python app (aoconfig.py) — lowercase booleans, aligned '=',
// the double z_autoortho nesting the Python app itself produces. Verifies
// the Go port reads an existing Python install's config without loss.
// pythonWrittenSampleTemplate has %s placeholders for a writable temp dir,
// substituted in at test time (the real file has absolute paths, but we
// can't hardcode a path this test environment can actually mkdir).
const pythonWrittenSampleTemplate = `[general]
gui        = false
showconfig = false
hide       = true
debug      = false

[paths]
xplane_path  = %[1]s/xplane
scenery_path = %[1]s/xplane/Custom Scenery/z_autoortho
cache_dir    = %[1]s/cache
download_dir = %[1]s/downloads
log_file     = %[1]s/logs/autoortho.log

[autoortho]
maptype_override = BI
min_zoom         = 14
maxwait          = 0.5
fetch_threads    = 32
maptypes         = ['Null', 'BI', 'EOX', 'GO2', 'ARC', 'NAIP', 'USGS', 'Firefly']

[pydds]
compressor = ISPC
format     = BC1

[scenery]
noclean = false

[fuse]
threading = true

[flightdata]
webui_port      = 5000
xplane_udp_port = 49000

[cache]
file_cache_size = 30
`

func TestLoadPythonWrittenConfig(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, ".autoortho")
	sample := fmt.Sprintf(pythonWrittenSampleTemplate, dir)
	if err := os.WriteFile(confPath, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(confPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantXplane := dir + "/xplane"
	if cfg.Paths.XplanePath != wantXplane {
		t.Errorf("XplanePath = %q, want %q", cfg.Paths.XplanePath, wantXplane)
	}
	if cfg.AutoOrtho.MinZoom != 14 {
		t.Errorf("MinZoom = %d, want 14 (lowercase-bool/aligned-INI parsing regression)", cfg.AutoOrtho.MinZoom)
	}
	if cfg.AutoOrtho.MaptypeOverride != "BI" {
		t.Errorf("MaptypeOverride = %q", cfg.AutoOrtho.MaptypeOverride)
	}
	if len(cfg.AutoOrtho.Maptypes) != 8 {
		t.Errorf("Maptypes = %v, want 8 entries", cfg.AutoOrtho.Maptypes)
	}
	if cfg.General.ShowConfig {
		t.Error("ShowConfig should be false per the sample file")
	}
	if cfg.Cache.FileCacheSizeGB != 30 {
		t.Errorf("FileCacheSizeGB = %d", cfg.Cache.FileCacheSizeGB)
	}

	// The double z_autoortho nesting is what the real Python app's own
	// get_config() produces from this exact scenery_path value — bug-
	// compatible on purpose, not a bug in the Go port.
	wantAO := filepath.Join(dir, "xplane", "Custom Scenery", "z_autoortho", "z_autoortho", "scenery")
	if cfg.AOSceneryPath != wantAO {
		t.Errorf("AOSceneryPath = %q, want %q", cfg.AOSceneryPath, wantAO)
	}
}

// TestComputeDerived_PicksUpRegionInstalledAfterInitialLoad is the
// regression test for a real web-UI bug: downloading a new scenery region
// mid-session (the Scenery panel's install button) used to never become
// mountable without restarting the whole process, because SceneryMounts
// was only ever scanned once, at initial config load. The fix
// (internal/webui/server.go's doInstallRegion) calls ComputeDerived again
// right after a successful install — this test proves ComputeDerived
// itself actually picks up a region directory that didn't exist on the
// first call, which is the mechanism that fix relies on.
func TestComputeDerived_PicksUpRegionInstalledAfterInitialLoad(t *testing.T) {
	dir := t.TempDir()

	cfg, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Paths.XplanePath = dir
	cfg.Paths.SceneryPath = dir
	if err := cfg.ComputeDerived(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.SceneryMounts) != 0 {
		t.Fatalf("SceneryMounts = %v before any region is installed, want empty", cfg.SceneryMounts)
	}

	// Simulate a region finishing installation (scenery.Package's real
	// extract/flatten leaves exactly this shape: one directory per region
	// under AOSceneryPath).
	if err := os.MkdirAll(filepath.Join(cfg.AOSceneryPath, "z_ao_eur"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := cfg.ComputeDerived(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.SceneryMounts) != 1 {
		t.Fatalf("SceneryMounts after install = %v, want exactly 1 entry for z_ao_eur", cfg.SceneryMounts)
	}
	if got := cfg.SceneryMounts[0].Root; got != filepath.Join(cfg.AOSceneryPath, "z_ao_eur") {
		t.Errorf("SceneryMounts[0].Root = %q, want the z_ao_eur dir", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, ".autoortho")

	cfg, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.confFile = confPath
	cfg.Paths.XplanePath = dir
	cfg.Paths.SceneryPath = dir
	cfg.AutoOrtho.MinZoom = 15
	cfg.AutoOrtho.PerformanceMode = "speed" // non-default, so a save that silently dropped it would read back as "quality" instead
	if err := cfg.ComputeDerived(); err != nil {
		t.Fatal(err)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(confPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.AutoOrtho.MinZoom != 15 {
		t.Errorf("MinZoom after round trip = %d, want 15", reloaded.AutoOrtho.MinZoom)
	}
	if reloaded.AutoOrtho.PerformanceMode != "speed" {
		t.Errorf("PerformanceMode after round trip = %q, want %q", reloaded.AutoOrtho.PerformanceMode, "speed")
	}
	if len(reloaded.AutoOrtho.Maptypes) != len(cfg.AutoOrtho.Maptypes) {
		t.Errorf("Maptypes lost in round trip: got %v", reloaded.AutoOrtho.Maptypes)
	}

	// internal/memwatch's watchdog (DESIGN.md §9) depends on these
	// surviving a save/reload intact — a silent loss here would make the
	// watchdog quietly fall back to defaults instead of the user's
	// configured thresholds.
	if reloaded.Memory != cfg.Memory {
		t.Errorf("Memory thresholds lost in round trip: got %+v, want %+v", reloaded.Memory, cfg.Memory)
	}
}

func TestMemoryThresholdsCustomRoundTrip(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, ".autoortho")

	cfg, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.confFile = confPath
	cfg.Paths.XplanePath = dir
	cfg.Paths.SceneryPath = dir
	cfg.Memory = Memory{SoftPercent: 0.3, HardPercent: 0.6, CriticalPercent: 0.9}
	if err := cfg.ComputeDerived(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(confPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Memory != cfg.Memory {
		t.Errorf("custom Memory thresholds = %+v after round trip, want %+v", reloaded.Memory, cfg.Memory)
	}
}
