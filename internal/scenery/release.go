package scenery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Release is one versioned release of a region's orthophoto/overlay
// packages. Mirrors downloader.py's Release class, including the on-disk
// "<region>_info.json" format, so a Go install can recognize (and be
// recognized by) a prior Python install.
type Release struct {
	Name        string // region id, e.g. "sa"
	DisplayName string // human name from info.json, e.g. "South America Ortho Set"
	InstallDir  string
	DownloadDir string
	URL         string
	Ver         string
	TotalSize   int64
	InfoVer     string
	Prerelease  bool
	Legacy      bool

	Installed  bool
	Downloaded bool
	Cleaned    bool
	Parsed     bool

	OrthoDirs      []string
	DownloadCount  int
	Packages       map[string]*Package

	ghRelease *ghRelease
	log       *slog.Logger
}

func NewRelease(name, installDir, downloadDir string) *Release {
	return &Release{
		Name:        name,
		InstallDir:  installDir,
		DownloadDir: downloadDir,
		InfoVer:     "v2",
		Cleaned:     true,
		Packages:    map[string]*Package{},
		log:         logFor().With("region", name),
	}
}

func (r *Release) InfoPath() string {
	return filepath.Join(r.InstallDir, "z_autoortho", r.Name+"_info.json")
}

// infoFile is the JSON shape written/read at InfoPath — mirrors
// downloader.py's Release.save()/load(), which dump most of the object's
// public attributes (and dynamically absorb whatever's in the upstream
// region_info.json via Python's setattr loop). We keep the superset of
// fields both sides actually use.
type infoFile struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name,omitempty"`
	Ver           string   `json:"ver"`
	InfoVer       string   `json:"info_ver"`
	OrthoDirs     []string `json:"ortho_dirs"`
	DownloadCount int      `json:"download_count"`
	Installed     bool     `json:"installed"`
	Downloaded    bool     `json:"downloaded"`
}

// Load reads a previously-saved info file (from either this Go port or the
// original Python version) and marks the release as already installed.
func (r *Release) Load(infoPath string) error {
	r.log.Info("loading local info", "path", infoPath)
	data, err := os.ReadFile(infoPath)
	if err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var info infoFile
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}
	r.Ver = info.Ver
	r.DisplayName = info.DisplayName
	r.OrthoDirs = info.OrthoDirs
	r.DownloadCount = info.DownloadCount

	r.Installed = true
	r.Downloaded = true
	r.Cleaned = true

	_, hasInfoVer := raw["info_ver"]
	if len(r.OrthoDirs) > 0 && !hasInfoVer {
		r.log.Info("legacy release detected")
		r.InfoVer = "v1"
		r.Ver = "0.0.0"
		r.Legacy = true
	} else if info.InfoVer != "" {
		r.InfoVer = info.InfoVer
	}

	// Backfill for every install this Go port has ever saved: Parse()
	// only ever read ortho_prefix/overlay_prefix from the upstream
	// region_info.json (see regionInfo) without turning them into actual
	// directories, so OrthoDirs was never populated and every saved
	// info.json on disk has "ortho_dirs": null — which silently broke
	// both Uninstall() (iterates OrthoDirs; always a no-op) and the
	// "Verify" button. Reconstruct it from the fixed install-layout
	// convention (same paths Parse()/Install() use below), keeping only
	// ones that actually exist so a region with no overlay package
	// doesn't get a false "missing directory".
	if len(r.OrthoDirs) == 0 {
		if backfilled := existingOrthoDirs(r.InstallDir, r.Name); len(backfilled) > 0 {
			r.log.Info("backfilling ortho_dirs missing from saved info.json", "dirs", backfilled)
			r.OrthoDirs = backfilled
			if err := r.Save(); err != nil {
				r.log.Warn("couldn't persist backfilled ortho_dirs", "err", err)
			}
		}
	}
	return nil
}

func (r *Release) Save() error {
	r.log.Info("saving info", "path", r.InfoPath())
	if err := os.MkdirAll(filepath.Dir(r.InfoPath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(infoFile{
		Name:          r.Name,
		DisplayName:   r.DisplayName,
		Ver:           r.Ver,
		InfoVer:       r.InfoVer,
		OrthoDirs:     r.OrthoDirs,
		DownloadCount: r.DownloadCount,
		Installed:     r.Installed,
		Downloaded:    r.Downloaded,
	}, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.InfoPath(), data, 0o644)
}

var assetNameRe = regexp.MustCompile(`^([yz])_(.*)\.zip\.?(\d*)$`)

// zInstallDir/yInstallDir are the one fixed install-layout convention this
// port uses for every region's "z" (ortho) and "y" (overlay) packages —
// shared by Parse (setting each Package.InstallDir) and by the OrthoDirs
// backfill/compute logic below, so there's exactly one place that layout
// is spelled out.
func zInstallDir(installDir, region string) string {
	return filepath.Join(installDir, "z_autoortho", "scenery", "z_ao_"+region)
}

func yInstallDir(installDir string) string {
	return filepath.Join(installDir, "yAutoOrtho_Overlays")
}

// installedOrthoDirs collects the distinct InstallDir values among pkgs —
// in practice at most two (one shared by every "z" package, one for "y"),
// since Parse sets every package of a given type to the same directory.
// Deduplicating + sorting here rather than trusting map iteration order
// keeps a freshly-installed release's OrthoDirs stable/deterministic.
func installedOrthoDirs(pkgs []*Package) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, pkg := range pkgs {
		if pkg.InstallDir == "" || seen[pkg.InstallDir] {
			continue
		}
		seen[pkg.InstallDir] = true
		dirs = append(dirs, pkg.InstallDir)
	}
	sort.Strings(dirs)
	return dirs
}

// existingOrthoDirs returns whichever of the two convention directories
// for region actually exist on disk right now — used to reconstruct
// OrthoDirs for an install whose saved info.json predates it being
// populated (see Load), where there's no package list to consult.
func existingOrthoDirs(installDir, region string) []string {
	var dirs []string
	for _, d := range []string{zInstallDir(installDir, region), yInstallDir(installDir)} {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// Parse reads the release's assets: fetches the "<region>_info.json" asset
// for display metadata, and groups the y_/z_ zip (or zip-part) assets into
// Packages, same grouping logic as downloader.py's Release.parse().
func (r *Release) Parse(ctx context.Context, client *http.Client) error {
	if r.Parsed || r.ghRelease == nil {
		return nil
	}

	r.Prerelease = r.ghRelease.Prerelease

	for _, a := range r.ghRelease.Assets {
		if len(a.Name) > len("_info.json") && a.Name[len(a.Name)-len("_info.json"):] == "_info.json" {
			data, err := fetchMetadata(ctx, client, a.BrowserDownloadURL, nil)
			if err != nil {
				return fmt.Errorf("fetching %s: %w", a.Name, err)
			}
			var info regionInfo
			if err := json.Unmarshal(data, &info); err != nil {
				return fmt.Errorf("parsing %s: %w", a.Name, err)
			}
			r.DisplayName = info.Name
			if info.Ver != "" {
				r.Ver = info.Ver
			}
			r.OrthoDirs = info.OrthoDirs
		}
	}

	var counts []int
	for _, a := range r.ghRelease.Assets {
		r.TotalSize += a.Size

		m := assetNameRe.FindStringSubmatch(a.Name)
		if m == nil {
			continue
		}
		pkgType, pkgName := m[1], m[2]
		key := pkgType + "_" + pkgName

		p, ok := r.Packages[key]
		if !ok {
			p = NewPackage(key, pkgType, r.DownloadDir)
			r.Packages[key] = p
		}
		switch pkgType {
		case "y":
			p.InstallDir = yInstallDir(r.InstallDir)
		case "z":
			p.InstallDir = zInstallDir(r.InstallDir, r.Name)
		}
		p.RemoteURLs = append(p.RemoteURLs, a.BrowserDownloadURL)
		counts = append(counts, a.DownloadCount)
	}

	for _, c := range counts {
		if c > r.DownloadCount {
			r.DownloadCount = c
		}
	}
	r.Parsed = true
	return nil
}

// Download fetches every package, after a disk-space preflight (DESIGN.md
// §11) against the download directory — failing fast with a clear message
// instead of partway through a multi-GB transfer.
func (r *Release) Download(ctx context.Context, client *http.Client, progress ProgressFunc) (bool, error) {
	if r.Downloaded {
		r.log.Info("already downloaded")
		return true, nil
	}

	if free, ok, err := FreeBytes(r.DownloadDir); err == nil && ok {
		// Require space for the download itself plus roughly the same
		// again for extraction, with a small safety margin.
		needed := uint64(float64(r.TotalSize) * 2.2)
		if free < needed {
			return false, fmt.Errorf(
				"not enough disk space in %s: need ~%.2f GB, have %.2f GB free",
				r.DownloadDir, float64(needed)/1e9, float64(free)/1e9,
			)
		}
	}

	r.Cleaned = false
	packages := sortedPackages(r.Packages)

	// A package that fails Check() gets cleaned up and re-downloaded, same
	// "verify what's on disk, discard the damaged package, try again"
	// health-check loop torrent clients run per-piece — here it's whole
	// packages, since that's the finest grain a CRC/SHA256 check on the
	// assembled zip can condemn (see Package.Download's checkExistingSize
	// for the lighter per-file check that runs before assembly).
	const maxAttempts = 3
	for i, pkg := range packages {
		var ok bool
		var err error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			r.log.Info("downloading package", "package", pkg.Name, "index", i+1, "total", len(packages), "attempt", attempt)
			if err = pkg.Download(ctx, client, progress, i+1, len(packages)); err != nil {
				if ctx.Err() != nil {
					r.log.Warn("download cancelled", "package", pkg.Name, "err", ctx.Err())
				}
				return false, err
			}
			ok, err = pkg.Check(progress, i+1, len(packages))
			if err != nil {
				return false, err
			}
			if ok {
				break
			}
			r.log.Warn("package failed validation, discarding and retrying", "package", pkg.Name, "attempt", attempt, "max_attempts", maxAttempts)
		}
		if !ok {
			return false, fmt.Errorf("package %s failed validation after %d attempts", pkg.Name, maxAttempts)
		}
	}
	r.Downloaded = true
	return true, nil
}

func (r *Release) Install(progress ProgressFunc) (bool, error) {
	if r.Installed {
		r.log.Info("already installed")
		return true, nil
	}

	for _, pkg := range sortedPackages(r.Packages) {
		if pkg.PkgType == "z" {
			if err := pkg.Uninstall(); err != nil {
				return false, err
			}
		}
	}

	installPackages := sortedPackages(r.Packages)
	for i, pkg := range installPackages {
		ok, err := pkg.Check(progress, i+1, len(installPackages))
		if err != nil {
			return false, err
		}
		if !ok {
			r.log.Warn("package fails checks", "package", pkg.Name)
			r.Downloaded = false
			return false, nil
		}
		r.log.Info("installing package", "package", pkg.Name, "index", i+1, "total", len(installPackages))
		if err := pkg.Install(progress, i+1, len(installPackages)); err != nil {
			r.log.Error("install failed", "package", pkg.Name, "err", err)
			return false, err
		}
	}

	// Computed from the actual package list rather than existingOrthoDirs'
	// disk-existence guess (Load's fallback for installs that predate this
	// field) — this is the authoritative source now that we know exactly
	// which package types this release actually has.
	r.OrthoDirs = installedOrthoDirs(installPackages)

	// Set Installed before Save so the persisted info.json actually
	// reflects success. The Python original saves first then flips the
	// flag — same ordering bug — but since Load() always treats a found
	// info.json as fully installed regardless of this field's stored
	// value, it's purely cosmetic there and free to fix here.
	r.Installed = true
	if err := r.Save(); err != nil {
		return false, err
	}
	return true, r.Cleanup()
}

func (r *Release) Cleanup() error {
	if r.Cleaned {
		return nil
	}
	for _, pkg := range r.Packages {
		if err := pkg.Cleanup(); err != nil {
			return err
		}
	}
	r.Cleaned = true
	return nil
}

// VerifyInstalled is a point-in-time "is it actually still there" check
// for an already-installed release — the "Verify" button's web UI command.
// It's deliberately NOT a byte-level integrity check: the downloaded zip
// is removed after a successful install (see Cleanup, called from
// Install), so there's no hash/CRC left locally to check extracted files
// against, and the upstream GitHub release only ever published one hash
// for the whole zip anyway, never per-tile. What this *can* check — every
// directory OrthoDirs says should exist actually exists and isn't empty —
// still catches the real-world failure modes that matter here: a user (or
// another tool) deleting/moving/emptying out an installed region's folder
// without going through this app's own Uninstall.
func (r *Release) VerifyInstalled() (bool, string, error) {
	if len(r.OrthoDirs) == 0 {
		return false, "no known scenery directories recorded for this install (older/legacy install metadata?)", nil
	}

	var missing []string
	var totalEntries int
	for _, dir := range r.OrthoDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			r.log.Warn("verify: scenery directory missing or unreadable", "dir", dir, "err", err)
			missing = append(missing, dir)
			continue
		}
		if len(entries) == 0 {
			r.log.Warn("verify: scenery directory is empty", "dir", dir)
			missing = append(missing, dir)
			continue
		}
		totalEntries += len(entries)
	}

	if len(missing) > 0 {
		return false, fmt.Sprintf("%d of %d scenery director%s missing or empty: %s",
			len(missing), len(r.OrthoDirs), pluralIES(len(r.OrthoDirs)), strings.Join(missing, ", ")), nil
	}
	return true, fmt.Sprintf("%d scenery director%s present (%d entries total)",
		len(r.OrthoDirs), pluralIES(len(r.OrthoDirs)), totalEntries), nil
}

func pluralIES(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func (r *Release) Uninstall() error {
	for _, dir := range r.OrthoDirs {
		if _, err := os.Stat(dir); err == nil {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	r.Installed = false
	return nil
}

// sortedPackages returns packages in a deterministic order (Go map
// iteration is randomized, so sorted *keys* alone wouldn't help — we need
// an ordered slice). Install/uninstall order shouldn't matter functionally,
// but stable logs make debugging much easier.
func sortedPackages(m map[string]*Package) []*Package {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*Package, 0, len(m))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
