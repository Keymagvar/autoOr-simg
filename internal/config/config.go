// Package config is the Go equivalent of aoconfig.py: a typed config
// struct (instead of Python's dynamic SectionParser), loaded from and saved
// to ~/.autoortho in an INI-compatible format so an existing Python install
// can be picked up without losing settings.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type General struct {
	GUI        bool // show the one-shot startup toast / auto-open browser
	ShowConfig bool
	Debug      bool
	AutoOpenUI bool // DESIGN.md §3: open the browser to :5000 on startup
	Headless   bool // DESIGN.md §3: no toast, no browser launch
	EnableTray bool // native taskbar/menu-bar icon (internal/tray); ignored under Headless
}

type Paths struct {
	XplanePath  string
	SceneryPath string
	CacheDir    string
	DownloadDir string
	LogFile     string
}

// AutoOrtho holds the quality/performance tradeoffs. MinZoom is the field
// DESIGN.md calls out explicitly: it exists in the Python config file today
// but was never exposed in the GUI. It's a floor on how far a tile is
// allowed to degrade under load, not a ceiling on max quality.
type AutoOrtho struct {
	MaptypeOverride string
	MinZoom         int
	// MaxZoom (0 = disabled, no cap — the default, matching every prior
	// release's behavior) is new in the Go port, not present in the
	// Python original: a user-requested ceiling on the highest zoom ever
	// fetched, trading texture sharpness for real disk/bandwidth savings
	// — e.g. capping at 15 even when installed scenery references
	// zoom-18 tiles. See internal/tile.NewTile's doc comment for exactly
	// how it's applied (row/col are shifted along with zoom, not just
	// the zoom number).
	MaxZoom  int
	MaxWait  float64
	Maptypes []string

	// FetchThreads sizes the single, shared, process-wide chunk-fetch
	// worker pool (internal/tile.FetchPool) every tile's every chunk
	// request competes through equally — the Go port's equivalent of
	// getortho.py's ChunkGetter (which uses the exact same default, 32).
	// Previously present but unused (a leftover from an earlier per-tile
	// concurrency design); wired up for real after a real finding
	// (the "Berlin" case): a per-tile/per-build concurrency split let a
	// handful of tiles monopolize fetch concurrency while hundreds of
	// others, under heavy simultaneous demand, got none at all.
	FetchThreads int

	// MaxConcurrentBuilds bounds how many tiles may simultaneously be
	// running BuildMipmap's composite/compress work — originally added in
	// direct response to a real finding: X-Plane can open many tiles at
	// once, and with no cap here each one started a full build (~67MB
	// transient RGBA composite) simultaneously, driving RSS to ~20GB on a
	// real test flight. See DESIGN.md §9/§2's notes and internal/tile's
	// buildSem.
	//
	// Raised from an original 8 to 32 after a second real finding
	// (the "Berlin" case): FetchThreads, not this field, is what
	// bounds chunk-fetch *concurrency and fairness* process-wide now (a
	// shared pool every tile's chunks compete through equally) — this
	// field only bounds simultaneous transient composite-buffer memory, a
	// purely different concern.
	//
	// Raised again, from 32 to 128, after the ~67MB-per-build premise
	// above went stale: a later architecture rewrite
	// (fetchComposeCompressed, internal/tile/tile.go) eliminated that
	// buffer for the common case — a mip0 build's real transient cost is
	// now ~9-10MB (the compressed output plus a few in-flight chunk
	// buffers), not 67MB. At 32 this field was gating the *cheap* path
	// far more tightly than its own math required, which — confirmed by a
	// live CPU profile and a direct before/after stress-tool comparison
	// of a real dense-view burst — pushed tiles into
	// zoomDropForLoad's *degraded* path (internal/tile/tile.go) more
	// often than necessary. That path, unlike the common case, still
	// allocates a genuine 64MB RGBA buffer (cropScaleUp's upscale output)
	// and spends the bulk of its CPU time there — so the old cap was
	// perversely *causing* more of the original 67MB-class memory
	// pressure it existed to prevent, not less. Measured: 32→128 took a
	// 300-tile synthetic burst from zoom_dropped_total=914 and a 8.2GB
	// RSS peak to zoom_dropped_total=0 and a 3.8GB peak — fewer
	// degradations *and* lower memory, not a tradeoff between them. The
	// real, independent safety net for resident (not transient) memory —
	// MaxOpenTiles' admission control plus LRU content eviction — is
	// unaffected by this field either way.
	MaxConcurrentBuilds int

	// MaxOpenTiles bounds how many tiles the Cacher will build at full
	// quality while simultaneously open (Refs>0) — distinct from both
	// MaxConcurrentBuilds (which only throttles active builds, not
	// resident ones already finished) and the Cacher's maxTiles cap
	// (which can only evict idle tiles, never ones X-Plane is actually
	// holding open). Loading a large area can leave
	// far more tiles open at once than either of those caps ever
	// constrains, and each fully-built tile holds ~11MB of compressed
	// mipmap data resident for as long as it's open — unbounded tile
	// count times that is exactly what drove a real OOM. Once open-tile
	// count exceeds this, newly-opened tiles are served the cheap,
	// near-zero-memory solid-color fallback (DESIGN.md §7) instead of a
	// real build, until enough tiles close to drop back under budget.
	// Default 300 ≈ 3.3GB of full-quality tiles resident at once, chosen
	// to leave headroom under an 8GB system alongside X-Plane itself and
	// MaxConcurrentBuilds' transient build memory.
	MaxOpenTiles int

	// ChunkConcurrency is now a no-op, kept only for config/API
	// backward-compat — see internal/tile.NewTile's own note on
	// chunkConcurrencyOverride. It originally bounded chunk-fetch
	// concurrency *within* a single tile's build, but
	// the "Berlin" finding replaced that per-tile model with FetchThreads'
	// shared, process-wide FetchPool (fairer under heavy simultaneous
	// demand). Deliberately not reused for anything else and deliberately
	// removed from the web UI — showing a control that silently does
	// nothing would violate the "UI reflects the real backend" rule.
	// Setting this to anything other than 0 has no effect.
	ChunkConcurrency int

	// PerformanceMode is "quality" (default), "balanced", or "speed" —
	// picks how aggressively internal/tile.zoomDropForLoad steps down to
	// a coarser, cheaper mip0 fetch under heavy concurrent-build load,
	// via SetPerformanceMode. Added after a real flight found a dense,
	// wide-view cold load pegging every CPU core on decode+compress work
	// (not network — confirmed no new downloads were happening at the
	// time); a closed-source competitor (XPME) independently ships an
	// analogous memory/quality toggle, which is real evidence this exact
	// tradeoff is something users want direct control over, not just
	// internal auto-tuning. An invalid/unrecognized value is treated as
	// "quality" (SetPerformanceMode's own fallback), not an error — a
	// hand-edited typo should degrade safely, not break startup.
	PerformanceMode string

	// DisableCoarseFallback, when true, turns off getBestCachedChunk's
	// blurry-but-geographically-correct coarser-zoom stand-in for a
	// slow/missing chunk (internal/tile.SetDisableCoarseFallback), falling
	// back to the flat fallbackColor patch instead. Default false (the
	// stand-in stays on) — it was ported from getortho.py's Tile.get_best_chunk
	// specifically because a flat patch reads as a glitch, while the blurry
	// stand-in reads as normal progressive loading. Exposed as a real
	// setting because a real flight found it reads as a soft-focus effect
	// over land but a washed-out "dirty" look over water, where low-zoom
	// provider imagery skews algae-tinted blue-green — land-heavy flying
	// should probably leave this off, water-heavy flying may prefer it on.
	DisableCoarseFallback bool
}

// Compression is the Go name for the Python config's [pydds] section.
type Compression struct {
	Compressor string // "ISPC" or "STB"
	Format     string // "BC1" or "BC3"
}

type Scenery struct {
	NoClean bool
}

type Fuse struct {
	Threading bool
}

type FlightData struct {
	WebUIPort     int
	XPlaneUDPPort int
}

type Cache struct {
	FileCacheSizeGB int
}

// Memory holds the DESIGN.md §9 watchdog's staged RSS thresholds, each a
// fraction of total system memory (not an absolute size) so one default
// works sensibly across an 8GB laptop and a 64GB desktop alike. Soft
// triggers aggressive-but-safe eviction of idle tiles; Hard is a loud
// warning; Critical runs the same guaranteed shutdown sequence DESIGN.md
// §4(c)/§8 already provides, to get ahead of the OS OOM-killer rather than
// wait for it.
type Memory struct {
	SoftPercent     float64
	HardPercent     float64
	CriticalPercent float64
}

// SceneryMount is a derived (not persisted) root/mount pair, computed the
// same way aoconfig.py's get_config() scans ao_scenery_path.
type SceneryMount struct {
	Root  string
	Mount string
}

type Config struct {
	General     General
	Paths       Paths
	AutoOrtho   AutoOrtho
	Compression Compression
	Scenery     Scenery
	Fuse        Fuse
	FlightData  FlightData
	Cache       Cache
	Memory      Memory

	// Derived, computed on Load(), not written back on Save().
	AOSceneryPath           string
	XplaneCustomSceneryPath string
	SceneryMounts           []SceneryMount

	confFile string
}

// DefaultConfigPath mirrors aoconfig.py: os.path.expanduser("~/.autoortho").
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".autoortho"), nil
}

func defaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".autoortho-data"), nil
}

// Default returns the same defaults as aoconfig.py's _defaults template.
func Default() (*Config, error) {
	dataDir, err := defaultDataDir()
	if err != nil {
		return nil, err
	}

	return &Config{
		General: General{
			GUI:        true,
			ShowConfig: true,
			Debug:      false,
			AutoOpenUI: true,
			Headless:   false,
			EnableTray: true,
		},
		Paths: Paths{
			XplanePath:  "",
			SceneryPath: "",
			CacheDir:    filepath.Join(dataDir, "cache"),
			DownloadDir: filepath.Join(dataDir, "downloads"),
			LogFile:     filepath.Join(dataDir, "logs", "autoortho.log"),
		},
		AutoOrtho: AutoOrtho{
			MaptypeOverride:       "",
			MinZoom:               12,
			MaxZoom:               0, // disabled by default — no behavior change for existing installs
			MaxWait:               0.5,
			Maptypes:              []string{"Null", "BI", "NAIP", "EOX", "USGS", "Firefly"},
			FetchThreads:          32,
			MaxConcurrentBuilds:   128, // see the field's own doc comment — 32 was stale, this is measured
			MaxOpenTiles:          300,
			ChunkConcurrency:      0,         // 0 = use provider.go's per-provider default (24 multi-server / 12 single-server)
			PerformanceMode:       "quality", // today's original zoomDropForLoad thresholds — no behavior change for existing installs
			DisableCoarseFallback: false,     // blurry stand-in stays on by default — no behavior change for existing installs
		},
		Compression: Compression{
			Compressor: "ISPC",
			Format:     "BC1",
		},
		Scenery: Scenery{NoClean: false},
		Fuse:    Fuse{Threading: true},
		FlightData: FlightData{
			WebUIPort:     5000,
			XPlaneUDPPort: 49000,
		},
		Cache: Cache{FileCacheSizeGB: 30},
		Memory: Memory{
			SoftPercent:     0.50,
			HardPercent:     0.70,
			CriticalPercent: 0.85,
		},
	}, nil
}

// Load reads confFile (if it exists) over top of Default(), then computes
// derived fields and ensures the directories it needs exist — same overall
// shape as aoconfig.py's load()+get_config().
func Load(confFile string) (*Config, error) {
	cfg, err := Default()
	if err != nil {
		return nil, err
	}
	cfg.confFile = confFile

	if data, err := os.ReadFile(confFile); err == nil {
		doc, err := parseIni(strings.NewReader(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", confFile, err)
		}
		cfg.applyIni(doc)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := cfg.ComputeDerived(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyIni(doc *iniDoc) {
	g := doc.section("general")
	setBool(&c.General.GUI, g, "gui")
	setBool(&c.General.ShowConfig, g, "showconfig")
	setBool(&c.General.Debug, g, "debug")
	setBool(&c.General.AutoOpenUI, g, "auto_open_ui")
	setBool(&c.General.EnableTray, g, "enable_tray")

	p := doc.section("paths")
	setStr(&c.Paths.XplanePath, p, "xplane_path")
	setStr(&c.Paths.SceneryPath, p, "scenery_path")
	setStr(&c.Paths.CacheDir, p, "cache_dir")
	setStr(&c.Paths.DownloadDir, p, "download_dir")
	setStr(&c.Paths.LogFile, p, "log_file")

	a := doc.section("autoortho")
	setStr(&c.AutoOrtho.MaptypeOverride, a, "maptype_override")
	setInt(&c.AutoOrtho.MinZoom, a, "min_zoom")
	setInt(&c.AutoOrtho.MaxZoom, a, "max_zoom")
	setFloat(&c.AutoOrtho.MaxWait, a, "maxwait")
	setInt(&c.AutoOrtho.FetchThreads, a, "fetch_threads")
	setInt(&c.AutoOrtho.MaxConcurrentBuilds, a, "max_concurrent_builds")
	setInt(&c.AutoOrtho.MaxOpenTiles, a, "max_open_tiles")
	setInt(&c.AutoOrtho.ChunkConcurrency, a, "chunk_concurrency")
	setStr(&c.AutoOrtho.PerformanceMode, a, "performance_mode")
	setBool(&c.AutoOrtho.DisableCoarseFallback, a, "disable_coarse_fallback")
	if v, ok := a.get("maptypes"); ok {
		c.AutoOrtho.Maptypes = parseStringList(v)
	}

	pd := doc.section("pydds")
	setStr(&c.Compression.Compressor, pd, "compressor")
	setStr(&c.Compression.Format, pd, "format")

	sc := doc.section("scenery")
	setBool(&c.Scenery.NoClean, sc, "noclean")

	fu := doc.section("fuse")
	setBool(&c.Fuse.Threading, fu, "threading")

	fd := doc.section("flightdata")
	setInt(&c.FlightData.WebUIPort, fd, "webui_port")
	setInt(&c.FlightData.XPlaneUDPPort, fd, "xplane_udp_port")

	ca := doc.section("cache")
	setInt(&c.Cache.FileCacheSizeGB, ca, "file_cache_size")

	me := doc.section("memory")
	setFloat(&c.Memory.SoftPercent, me, "soft_percent")
	setFloat(&c.Memory.HardPercent, me, "hard_percent")
	setFloat(&c.Memory.CriticalPercent, me, "critical_percent")
}

// ComputeDerived (re)derives every path/list that depends on the raw
// XplanePath/SceneryPath fields — AOSceneryPath, XplaneCustomSceneryPath,
// and SceneryMounts (scanned fresh from disk each call). Exported, not just
// an internal Load() step, because a scenery region installed through the
// web UI mid-session (doInstallRegion) needs this rerun afterward: without
// it, SceneryMounts stays frozen at whatever existed when the process
// started, so a freshly downloaded region never becomes mountable until a
// full restart — the web UI's Run button would silently do nothing (zero
// configured mounts to loop over, no error) even though the scenery really
// is there on disk.
func (c *Config) ComputeDerived() error {
	c.AOSceneryPath = filepath.Join(c.Paths.SceneryPath, "z_autoortho", "scenery")

	abs, err := filepath.Abs(filepath.Join(c.Paths.XplanePath, "Custom Scenery"))
	if err != nil {
		return err
	}
	c.XplaneCustomSceneryPath = abs

	c.SceneryMounts = nil
	entries, err := os.ReadDir(c.AOSceneryPath)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			c.SceneryMounts = append(c.SceneryMounts, SceneryMount{
				Root:  filepath.Join(c.AOSceneryPath, e.Name()),
				Mount: filepath.Join(c.XplaneCustomSceneryPath, e.Name()),
			})
		}
	}

	for _, dir := range []string{c.Paths.CacheDir, c.Paths.DownloadDir, filepath.Dir(c.Paths.LogFile), c.AOSceneryPath} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	return nil
}

// Save writes the config back to its confFile in the same INI-compatible
// format it was loaded from.
func (c *Config) Save() error {
	doc := newIniDoc()

	g := doc.section("general")
	g.set("gui", boolStr(c.General.GUI))
	g.set("showconfig", boolStr(c.General.ShowConfig))
	g.set("debug", boolStr(c.General.Debug))
	g.set("auto_open_ui", boolStr(c.General.AutoOpenUI))
	g.set("enable_tray", boolStr(c.General.EnableTray))

	p := doc.section("paths")
	p.set("xplane_path", c.Paths.XplanePath)
	p.set("scenery_path", c.Paths.SceneryPath)
	p.set("cache_dir", c.Paths.CacheDir)
	p.set("download_dir", c.Paths.DownloadDir)
	p.set("log_file", c.Paths.LogFile)

	a := doc.section("autoortho")
	a.set("maptype_override", c.AutoOrtho.MaptypeOverride)
	a.set("min_zoom", strconv.Itoa(c.AutoOrtho.MinZoom))
	a.set("max_zoom", strconv.Itoa(c.AutoOrtho.MaxZoom))
	a.set("maxwait", strconv.FormatFloat(c.AutoOrtho.MaxWait, 'f', -1, 64))
	a.set("fetch_threads", strconv.Itoa(c.AutoOrtho.FetchThreads))
	a.set("max_concurrent_builds", strconv.Itoa(c.AutoOrtho.MaxConcurrentBuilds))
	a.set("max_open_tiles", strconv.Itoa(c.AutoOrtho.MaxOpenTiles))
	a.set("chunk_concurrency", strconv.Itoa(c.AutoOrtho.ChunkConcurrency))
	a.set("performance_mode", c.AutoOrtho.PerformanceMode)
	a.set("disable_coarse_fallback", boolStr(c.AutoOrtho.DisableCoarseFallback))
	a.set("maptypes", "["+strings.Join(quoteAll(c.AutoOrtho.Maptypes), ", ")+"]")

	pd := doc.section("pydds")
	pd.set("compressor", c.Compression.Compressor)
	pd.set("format", c.Compression.Format)

	sc := doc.section("scenery")
	sc.set("noclean", boolStr(c.Scenery.NoClean))

	fu := doc.section("fuse")
	fu.set("threading", boolStr(c.Fuse.Threading))

	fd := doc.section("flightdata")
	fd.set("webui_port", strconv.Itoa(c.FlightData.WebUIPort))
	fd.set("xplane_udp_port", strconv.Itoa(c.FlightData.XPlaneUDPPort))

	ca := doc.section("cache")
	ca.set("file_cache_size", strconv.Itoa(c.Cache.FileCacheSizeGB))

	me := doc.section("memory")
	me.set("soft_percent", strconv.FormatFloat(c.Memory.SoftPercent, 'f', -1, 64))
	me.set("hard_percent", strconv.FormatFloat(c.Memory.HardPercent, 'f', -1, 64))
	me.set("critical_percent", strconv.FormatFloat(c.Memory.CriticalPercent, 'f', -1, 64))

	f, err := os.Create(c.confFile)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeIni(f, doc)
}

// --- small helpers -------------------------------------------------------

func setStr(dst *string, s *iniSection, key string) {
	if v, ok := s.get(key); ok {
		*dst = v
	}
}

func setBool(dst *bool, s *iniSection, key string) {
	v, ok := s.get(key)
	if !ok {
		return
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes", "on":
		*dst = true
	case "false", "0", "no", "off":
		*dst = false
	}
}

func setInt(dst *int, s *iniSection, key string) {
	v, ok := s.get(key)
	if !ok {
		return
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		*dst = n
	}
}

func setFloat(dst *float64, s *iniSection, key string) {
	v, ok := s.get(key)
	if !ok {
		return
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
		*dst = f
	}
}

func boolStr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// parseStringList parses Python-literal-list-looking strings like
// "['Null', 'BI', 'EOX']" written by the original config file.
func parseStringList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "'\"")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func quoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = "'" + s + "'"
	}
	return out
}
