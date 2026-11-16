package scenery

// ghAsset and ghRelease mirror the subset of the GitHub Releases API JSON
// that downloader.py actually reads.
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	DownloadCount      int    `json:"download_count"`
}

type ghRelease struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	TagName    string    `json:"tag_name"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

// regionInfo mirrors the "<region>_info.json" asset shape (see
// autoortho/autoortho/testfiles/infofiles/*_info.json): {"id": "sa", "name":
// "South America Ortho Set", "ortho_prefix": ..., "ver": "0.0.53",
// "ortho_dirs": [...]}.
type regionInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	OrthoPrefix   string   `json:"ortho_prefix"`
	OverlayPrefix string   `json:"overlay_prefix"`
	Ver           string   `json:"ver"`
	OrthoDirs     []string `json:"ortho_dirs"`
	InfoVer       string   `json:"info_ver,omitempty"`
}

// Progress describes an in-flight package install, for the web UI's live
// progress display (DESIGN.md §3) — the Go equivalent of downloader.py's
// module-level cur_activity dict, but passed explicitly instead of global
// mutable state.
//
// PercentDone is meaningful only during Phase "downloading" (it's
// byte-granularity progress of one file within one package); the other
// phases report 0 but still fire so the UI has something other than a
// frozen percentage to show while verify/assemble/extract run (these can
// take minutes on a large multi-GB region with no byte-level progress to
// report).
type Progress struct {
	Phase        string // "downloading" | "verifying" | "assembling" | "extracting" | "installing"
	Status       string
	URL          string
	PackageName  string
	PackageIndex int // 1-based index of the current package within the release
	PackageTotal int
	PercentDone  float64
	MBps         float64
}

// ProgressFunc is called periodically during a download. Implementations
// must not block for long — they're called from the download loop.
type ProgressFunc func(Progress)
