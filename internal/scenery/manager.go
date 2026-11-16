package scenery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const releasesURL = "https://api.github.com/repos/kubilus1/autoortho-scenery/releases"

// metadataTimeout bounds small JSON calls (release lists, info.json) only.
// It is applied per-call via context, never as http.Client.Timeout — that
// field covers a request's *entire* lifetime including reading the body,
// which would also cap large package downloads at the same duration. That
// was a real bug here: a 30s Client.Timeout killed every multi-GB scenery
// download partway through, every time, regardless of actual progress.
const metadataTimeout = 30 * time.Second

// newHTTPClient returns a client with no overall request-duration cap (so
// large downloads can run as long as they need), but with transport-level
// timeouts so a genuinely stuck/dead connection still gets caught instead
// of hanging forever.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

func fetchURL(ctx context.Context, client *http.Client, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// fetchMetadata is fetchURL with a bounded deadline, for small JSON calls
// only (never for package file downloads — see metadataTimeout above).
func fetchMetadata(ctx context.Context, client *http.Client, url string, headers map[string]string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	return fetchURL(ctx, client, url, headers)
}

// Region groups all known Releases for one scenery region (e.g. "sa"),
// tracking whichever one is actually installed locally. Mirrors
// downloader.py's Region class.
type Region struct {
	ID          string
	InstallDir  string
	DownloadDir string
	Releases    map[string]*Release
	LocalRel    *Release
}

func newRegion(id, installDir, downloadDir string) *Region {
	r := &Region{
		ID:          id,
		InstallDir:  installDir,
		DownloadDir: downloadDir,
		Releases:    map[string]*Release{},
	}
	r.findExisting()
	return r
}

var infoJSONNameRe = regexp.MustCompile(`^(.*)_info\.json$`)

func (r *Region) findExisting() {
	pattern := filepath.Join(r.InstallDir, "z_autoortho", r.ID+"_info.json")
	matches, _ := filepath.Glob(pattern)
	for _, path := range matches {
		rel := NewRelease(r.ID, r.InstallDir, r.DownloadDir)
		if err := rel.Load(path); err != nil {
			continue
		}
		if _, ok := r.Releases[rel.Ver]; !ok {
			r.Releases[rel.Ver] = rel
		}
		r.LocalRel = rel
	}
}

// GetLatestRelease returns the highest-versioned known Release for this
// region (downloaded-and-parsed or just discovered from the GitHub API).
func (r *Region) GetLatestRelease() *Release {
	vers := make([]string, 0, len(r.Releases))
	for v := range r.Releases {
		vers = append(vers, v)
	}
	sort.Slice(vers, func(i, j int) bool { return compareVersions(vers[i], vers[j]) > 0 })
	if len(vers) == 0 {
		return nil
	}
	return r.Releases[vers[0]]
}

// InstallRelease downloads and installs ver (or the latest known release
// if ver is empty), uninstalling whatever's currently installed first if
// it differs. Mirrors downloader.py's Region.install_release().
func (r *Region) InstallRelease(ctx context.Context, client *http.Client, ver string, progress ProgressFunc) (bool, error) {
	var rel *Release
	if ver == "" {
		rel = r.GetLatestRelease()
	} else {
		rel = r.Releases[ver]
	}
	if rel == nil {
		return false, fmt.Errorf("no release %q known for region %s", ver, r.ID)
	}

	if r.LocalRel != nil && r.LocalRel.Ver == rel.Ver {
		return true, nil // already installed at this version
	}
	if r.LocalRel != nil {
		if err := r.LocalRel.Uninstall(); err != nil {
			return false, err
		}
		delete(r.Releases, r.LocalRel.Ver)
		r.LocalRel = nil
	}

	if err := rel.Parse(ctx, client); err != nil {
		return false, err
	}
	if ok, err := rel.Download(ctx, client, progress); err != nil || !ok {
		return false, err
	}
	if ok, err := rel.Install(progress); err != nil || !ok {
		return false, err
	}
	r.LocalRel = rel
	return true, nil
}

// Manager is the Go equivalent of downloader.py's OrthoManager: it talks to
// the GitHub Releases API (with a 1-hour on-disk cache) and groups results
// into Regions.
type Manager struct {
	Client      *http.Client
	DownloadDir string
	ExtractDir  string
	InfoCache   string

	Regions map[string]*Region
}

func NewManager(extractDir, downloadDir, infoCache string) (*Manager, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{
		Client:      newHTTPClient(),
		DownloadDir: downloadDir,
		ExtractDir:  extractDir,
		InfoCache:   infoCache,
		Regions:     map[string]*Region{},
	}, nil
}

func (m *Manager) getReleaseData(ctx context.Context) ([]ghRelease, error) {
	var stale bool
	if st, err := os.Stat(m.InfoCache); err == nil {
		stale = time.Since(st.ModTime()) >= time.Hour
	} else {
		stale = true
	}

	if stale {
		data, err := fetchMetadata(ctx, m.Client, releasesURL, map[string]string{"Accept": "application/vnd.github+json"})
		if err == nil {
			if werr := os.WriteFile(m.InfoCache, data, 0o644); werr == nil {
				var releases []ghRelease
				if jerr := json.Unmarshal(data, &releases); jerr == nil {
					return releases, nil
				}
			}
		}
		logFor().Warn("couldn't refresh release info, falling back to cache if present", "err", err)
	}

	data, err := os.ReadFile(m.InfoCache)
	if err != nil {
		return nil, fmt.Errorf("no cached release info and refresh failed: %w", err)
	}
	var releases []ghRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// FindRegions populates m.Regions from the GitHub release list, same
// grouping/skip-prerelease logic as downloader.py's find_regions().
func (m *Manager) FindRegions(ctx context.Context) error {
	releases, err := m.getReleaseData(ctx)
	if err != nil {
		return err
	}

	for i := range releases {
		rel := &releases[i]

		var regionName string
		for _, a := range rel.Assets {
			if mm := infoJSONNameRe.FindStringSubmatch(a.Name); mm != nil {
				regionName = mm[1]
				break
			}
		}
		if regionName == "" {
			continue
		}
		if rel.Prerelease {
			continue
		}

		region, ok := m.Regions[regionName]
		if !ok {
			region = newRegion(regionName, m.ExtractDir, m.DownloadDir)
			m.Regions[regionName] = region
		}

		verName := rel.Name
		if _, ok := region.Releases[verName]; !ok {
			r := NewRelease(regionName, m.ExtractDir, m.DownloadDir)
			r.Ver = verName
			r.URL = fmt.Sprintf("%s/%d", releasesURL, rel.ID)
			r.ghRelease = rel
			region.Releases[verName] = r
		}
	}
	return nil
}
