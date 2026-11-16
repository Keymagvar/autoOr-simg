// Package provider is the imagery-provider registry described in
// ../../DESIGN.md §5: each provider is a URL template plus the per-provider
// politeness knobs (headers, user-agent, concurrency cap) that replace
// getortho.py's hardcoded MAPTYPES dict and its curl-flavored User-Agent.
package provider

import "fmt"

// Provider describes one imagery source. TileURL builds the request URL for
// a given tile; serverIdx lets callers rotate across a provider's
// subdomains (a/b/c/d, t0-3, khms0-3 — the same load-spreading trick
// getortho.py already uses) without the provider needing to track state.
type Provider struct {
	Name       string
	TileURL    func(col, row, zoom, serverIdx int) string
	Headers    map[string]string
	UserAgent  string
	NumServers int // for rotating across serverIdx 0..NumServers-1
	MaxThreads int // per-provider concurrency cap (DESIGN.md §5)
}

// defaultUserAgent deliberately looks like a normal browser/map-client
// request, not a script — see DESIGN.md §5: the Python version's
// "curl/7.68.0" User-Agent is a tell, not camouflage.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

var registry = map[string]Provider{}

// defaultMaxThreadsMultiServer / defaultMaxThreadsSingleServer replace the
// old flat 8-for-everyone default. Found too low by a real flight test
// ("scenery takes 15 minutes to load, patches never
// resolve"): a full tile needs 256 chunk fetches, real measured
// latency to a provider from a real machine was ~0.8s/request, and at the
// old concurrency of 8 that's ~26s to complete a tile — far more than the
// ~9-12s total budget maxwait+repair gave it (tile.go's
// maxRepairAttempts/repairDelay), so tiles permanently gave up with
// visible fallback-color patches baked in forever, and the whole scenery
// load dragged on for 15+ minutes. Raising this is the direct fix: at
// 24-way concurrency the same math is ~8.5s/attempt, which now actually
// fits inside the (also-widened, see tile.go) repair budget. Multi-server
// providers get the higher number since they spread that load across
// 4 subdomains; single-server providers (no load-spreading at all) get a
// more conservative bump to avoid self-inflicted rate-limiting on one
// host — see this session's real-world evidence of ARC (single-server)
// getting flooded with connection resets under even the old, lower load.
const (
	defaultMaxThreadsMultiServer  = 24
	defaultMaxThreadsSingleServer = 12
)

func register(p Provider) {
	if p.UserAgent == "" {
		p.UserAgent = defaultUserAgent
	}
	if p.MaxThreads == 0 {
		if p.NumServers > 1 {
			p.MaxThreads = defaultMaxThreadsMultiServer
		} else {
			p.MaxThreads = defaultMaxThreadsSingleServer
		}
	}
	registry[p.Name] = p
}

// Get returns the named provider (case-sensitive, matching the upstream
// maptype codes: BI, EOX, GO2, ARC, NAIP, USGS, FIREFLY) and whether it's
// registered.
func Get(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

// RegisterForTest registers p in the shared registry — exported only so
// a test in another package (e.g. internal/prefetch's RunCandidates
// test) can exercise the real maptype-string lookup path
// (NewTile -> provider.Get) against a fake/local HTTP server, instead
// of either hitting a real provider's real external host or needing to
// bypass the registry entirely (which would leave the lookup path
// itself unverified). Not meant for production use — nothing stops
// calling it there, but every real provider is already registered via
// this package's own init().
func RegisterForTest(p Provider) {
	register(p)
}

// Names returns every registered provider's name.
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}

func init() {
	register(Provider{
		Name:       "BI",
		NumServers: 4, // t0-t3
		TileURL: func(col, row, zoom, serverIdx int) string {
			q := quadkey(col, row, zoom)
			return fmt.Sprintf("https://ecn.t%d.tiles.virtualearth.net/tiles/a%s.jpeg?g=13816", serverIdx%4, q)
		},
	})

	register(Provider{
		Name:       "EOX",
		NumServers: 4, // a/b/c/d
		Headers:    map[string]string{"Referer": "https://s2maps.eu/"},
		TileURL: func(col, row, zoom, serverIdx int) string {
			servers := []string{"a", "b", "c", "d"}
			server := servers[serverIdx%len(servers)]
			return fmt.Sprintf(
				"https://%s.s2maps-tiles.eu/wmts?layer=s2cloudless-2023_3857&style=default&tilematrixset=g&Service=WMTS&Request=GetTile&Version=1.0.0&Format=image%%2Fjpeg&TileMatrix=%d&TileCol=%d&TileRow=%d",
				server, zoom, col, row,
			)
		},
	})

	register(Provider{
		Name:       "GO2",
		NumServers: 4,
		TileURL: func(col, row, zoom, serverIdx int) string {
			return fmt.Sprintf("http://khms%d.google.com/kh/v=934?x=%d&y=%d&z=%d", serverIdx%4, col, row, zoom)
		},
	})

	register(Provider{
		Name:       "ARC",
		NumServers: 1,
		TileURL: func(col, row, zoom, serverIdx int) string {
			return fmt.Sprintf("http://services.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/%d/%d/%d", zoom, row, col)
		},
	})

	register(Provider{
		Name:       "NAIP",
		NumServers: 1,
		TileURL: func(col, row, zoom, serverIdx int) string {
			return fmt.Sprintf("http://naip.maptiles.arcgis.com/arcgis/rest/services/NAIP/MapServer/tile/%d/%d/%d", zoom, row, col)
		},
	})

	register(Provider{
		Name:       "USGS",
		NumServers: 1,
		TileURL: func(col, row, zoom, serverIdx int) string {
			return fmt.Sprintf("https://basemap.nationalmap.gov/arcgis/rest/services/USGSImageryOnly/MapServer/tile/%d/%d/%d", zoom, row, col)
		},
	})

	register(Provider{
		Name:       "FIREFLY",
		NumServers: 1,
		TileURL: func(col, row, zoom, serverIdx int) string {
			return fmt.Sprintf("https://fly.maptiles.arcgis.com/arcgis/rest/services/World_Imagery_Firefly/MapServer/tile/%d/%d/%d", zoom, row, col)
		},
	})
}

// quadkey translates Google-style (col, row, zoom) tile coordinates to
// Bing's quadkey scheme — direct port of getortho.py's _gtile_to_quadkey.
func quadkey(col, row, zoom int) string {
	var qk []byte
	for step := 1; step <= zoom; step++ {
		size := 1 << (zoom - step)
		a, b := col/size, row/size
		col -= a * size
		row -= b * size
		qk = append(qk, byte('0'+a+2*b))
	}
	return string(qk)
}
