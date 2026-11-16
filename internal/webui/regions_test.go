package webui

import (
	"errors"
	"testing"

	"autoOr-simg/internal/scenery"
)

// TestRegionViewFrom_PreservesInstalledStatusWhenParseFails is the
// regression test for a real bug: refreshRegions used to build a region's
// RegionView entirely *after* a successful scenery.Release.Parse() call —
// so the one remote network request in the whole function (fetching the
// latest release's own metadata) failing for any reason (rate limit,
// timeout, a transient CDN hiccup) silently discarded Region.LocalRel,
// which is purely local, no-network information already known before
// Parse() was ever called. The observed symptom: a region the user had
// actually installed (aus_pac) showed up in the web UI as available to
// install, the instant that one unrelated network call hiccuped.
func TestRegionViewFrom_PreservesInstalledStatusWhenParseFails(t *testing.T) {
	region := &scenery.Region{
		ID: "aus_pac",
		LocalRel: &scenery.Release{
			Name: "aus_pac",
			Ver:  "0.0.42-1",
		},
	}
	latest := scenery.NewRelease("aus_pac", "", "")
	latest.Ver = "0.0.42-1"

	v := regionViewFrom("aus_pac", region, latest, errors.New("github: rate limited"))

	if !v.Installed {
		t.Fatalf("Installed = false, want true — LocalRel was known locally and must survive a remote Parse() failure")
	}
	if v.LocalVersion != "0.0.42-1" {
		t.Fatalf("LocalVersion = %q, want %q", v.LocalVersion, "0.0.42-1")
	}
	if v.Error == "" {
		t.Fatalf("Error should still be reported so the UI can show the fetch problem")
	}
}

// TestRegionViewFrom_NotInstalledAndParseFails proves the fix doesn't
// invent an install that doesn't exist — a region with no LocalRel stays
// not-installed even when Parse also fails.
func TestRegionViewFrom_NotInstalledAndParseFails(t *testing.T) {
	region := &scenery.Region{ID: "eur"}
	latest := scenery.NewRelease("eur", "", "")
	latest.Ver = "0.0.51"

	v := regionViewFrom("eur", region, latest, errors.New("network unreachable"))

	if v.Installed {
		t.Fatalf("Installed = true, want false — this region was never locally installed")
	}
}

// TestRegionViewFrom_SuccessfulParsePopulatesRemoteFields proves the happy
// path still works: when Parse succeeds, the remote-only fields (display
// name, size, latest version) come through too, not just install status.
func TestRegionViewFrom_SuccessfulParsePopulatesRemoteFields(t *testing.T) {
	region := &scenery.Region{
		ID: "aus_pac",
		LocalRel: &scenery.Release{
			Name: "aus_pac",
			Ver:  "0.0.42-1",
		},
	}
	latest := scenery.NewRelease("aus_pac", "", "")
	latest.Ver = "0.0.42-1"
	latest.DisplayName = "Australia Pacific Ortho Set"
	latest.TotalSize = 1048576 * 10 // 10MB

	v := regionViewFrom("aus_pac", region, latest, nil)

	if !v.Installed || !v.UpToDate {
		t.Fatalf("Installed/UpToDate = %v/%v, want true/true", v.Installed, v.UpToDate)
	}
	if v.Name != "Australia Pacific Ortho Set" {
		t.Fatalf("Name = %q, want the parsed display name", v.Name)
	}
	if v.SizeMB != 10 {
		t.Fatalf("SizeMB = %v, want 10", v.SizeMB)
	}
	if v.Error != "" {
		t.Fatalf("Error = %q, want empty on a successful parse", v.Error)
	}
}
