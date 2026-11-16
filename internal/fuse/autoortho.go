// This file is the migration-order step-6 piece from autoortho/AGENTS.md:
// the real filesystem, replacing Trivial (trivial.go) now that
// internal/tile is proven against real provider data. It ports
// autoortho_fuse.py's AutoOrtho Operations class: a regex classifies every
// path as a synthesized .dds tile or a real on-disk file (.dsf/.ter and
// everything else under the scenery root), and only .dds paths are routed
// into the tile engine.
package fuse

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/winfsp/cgofuse/fuse"

	"autoOr-simg/internal/logging"
	"autoOr-simg/internal/tile"
)

// ddsPathRe mirrors autoortho_fuse.py's dds_re:
// r".*/(\d+)[-_](\d+)[-_]((?!ZL)\S*)(\d{2}).dds"
// Go's RE2 engine has no negative lookahead, so the "(?!ZL)" exclusion
// (never observed to matter for any real maptype code — BI/EOX/GO2/ARC/
// NAIP/USGS/FIREFLY, none start with "ZL") is applied as a plain prefix
// check on the captured maptype group after matching, in isDDSPath below,
// rather than folded into the pattern.
var ddsPathRe = regexp.MustCompile(`^.*/(\d+)[-_](\d+)[-_](\S*)(\d{2})\.dds$`)

// parseDDSPath extracts (row, col, maptype, zoom) from a FUSE path, or
// ok=false if path isn't a synthesized tile path.
func parseDDSPath(path string) (row, col int, maptype string, zoom int, ok bool) {
	m := ddsPathRe.FindStringSubmatch(path)
	if m == nil {
		return 0, 0, "", 0, false
	}
	if len(m[3]) >= 2 && m[3][:2] == "ZL" {
		return 0, 0, "", 0, false
	}
	row, err1 := strconv.Atoi(m[1])
	col, err2 := strconv.Atoi(m[2])
	zoom, err3 := strconv.Atoi(m[4])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, "", 0, false
	}
	return row, col, m[3], zoom, true
}

// AutoOrtho is the real cgofuse filesystem: synthesized .dds tiles backed
// by internal/tile's Cacher, everything else passed straight through to
// root — mirrors autoortho_fuse.py's AutoOrtho(Operations) class.
type AutoOrtho struct {
	fuse.FileSystemBase
	log *slog.Logger

	root            string
	cacher          *tile.Cacher
	minZoom         int
	maxZoom         int
	maptypeOverride string
	ddsSize         int64

	mu      sync.Mutex
	handles map[uint64]*os.File
	nextFH  uint64

	opens  atomic.Int64 // outstanding opens of any kind, for shutdown diagnostics
	panics atomic.Int64 // recovered panics, see recoverPanic — should stay 0 in a healthy run
}

// NewAutoOrtho builds the real filesystem rooted at root (the real scenery
// directory X-Plane's Custom Scenery entry points at), serving synthesized
// tiles from cacher. minZoom mirrors aoconfig.py's autoortho.min_zoom,
// passed through to every Cacher.Open call. maxZoom (0 = no cap) is new in
// the Go port, not present in the Python original: a ceiling on the
// highest zoom ever fetched, trading texture sharpness for less network/
// disk use — see internal/tile.NewTile's doc comment for how it's applied.
// maptypeOverride, when non-empty, replaces whatever provider code is baked
// into each requested tile's filename (config.AutoOrtho.MaptypeOverride) —
// see resolveMaptype.
func NewAutoOrtho(root string, cacher *tile.Cacher, minZoom, maxZoom int, maptypeOverride string) *AutoOrtho {
	return &AutoOrtho{
		log:             logging.For("fuse"),
		root:            root,
		cacher:          cacher,
		minZoom:         minZoom,
		maxZoom:         maxZoom,
		maptypeOverride: maptypeOverride,
		ddsSize:         int64(tile.DDSSize()),
		handles:         map[uint64]*os.File{},
	}
}

// resolveMaptype applies maptypeOverride (the web UI's "Imagery provider
// (anular)" setting) over whatever maptype code parseDDSPath found baked
// into the requested tile's own filename. Every installed scenery package
// bakes a maptype into its .ter files at the time it was authored/
// downloaded — without this, config.AutoOrtho.MaptypeOverride was read
// into config/state but never actually consulted anywhere in the real
// tile-serving path, making it a silently no-op setting.
func (a *AutoOrtho) resolveMaptype(parsed string) string {
	if a.maptypeOverride != "" {
		return a.maptypeOverride
	}
	return parsed
}

func (a *AutoOrtho) fullPath(path string) string {
	return filepath.Join(a.root, filepath.FromSlash(path))
}

func (a *AutoOrtho) Init() {
	a.log.Info("filesystem init", "root", a.root)
}

func (a *AutoOrtho) Destroy() {
	a.log.Info("filesystem destroy")
}

// OutstandingOpens reports open file handles (real passthrough files only
// — synthesized tiles are refcounted inside the Cacher itself, see
// Cacher.Open/Close) — used by the shutdown path exactly as Trivial's
// equivalent is.
func (a *AutoOrtho) OutstandingOpens() int {
	return int(a.opens.Load())
}

// PanicCount reports how many FUSE callback panics have been recovered
// over this filesystem's lifetime — see recoverPanic. Should read 0 in a
// healthy run; any nonzero value means something in the tile engine (most
// likely a cgo call fed genuinely malformed real-world data) hit a case
// recoverPanic turned into a logged EIO instead of taking the whole mount
// down, and is worth investigating even though the mount itself survived.
func (a *AutoOrtho) PanicCount() int64 {
	return a.panics.Load()
}

// recoverPanic is deferred at the top of every FUSE callback that touches
// the tile engine or the native cgo decode/compress path underneath it.
// Go's runtime treats an unrecovered panic that unwinds across a cgo call
// boundary as fatal — it does not become a catchable error on either side,
// it crashes the whole process outright. cgofuse invokes these methods
// directly from its own dispatch goroutine(s), not from the goroutine
// running runCmd's top-level recover() in cmd/autoortho/main.go, so that
// outer recover cannot and does not protect this path — it is a different
// goroutine entirely. A panic reaching all the way up through one of these
// methods would silently kill the mount (and very possibly the whole
// process) mid-flight with no unmount, no log explaining why, nothing.
// Recovering here, locally, before control returns to cgofuse's C-side
// caller, is what actually prevents that: errc is set to -fuse.EIO (the
// request that triggered it fails, X-Plane sees a read error on one
// texture) instead of the mount or process going down.
func (a *AutoOrtho) recoverPanic(errc *int, op, path string) {
	if r := recover(); r != nil {
		a.panics.Add(1)
		a.log.Error("panic recovered in FUSE callback — serving an error instead of crashing the mount",
			"op", op, "path", path, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		*errc = -fuse.EIO
	}
}

func (a *AutoOrtho) Getattr(path string, stat *fuse.Stat_t, fh uint64) (errc int) {
	defer a.recoverPanic(&errc, "Getattr", path)

	if _, _, _, _, ok := parseDDSPath(path); ok {
		now := fuse.Now()
		stat.Mode = fuse.S_IFREG | 0o444
		stat.Nlink = 1
		stat.Size = a.ddsSize
		stat.Mtim, stat.Atim, stat.Ctim = now, now, now
		return 0
	}

	// .AOISWORKING is a zero-byte stub autoortho_fuse.py's getattr fakes
	// directly (path.endswith('AOISWORKING')) rather than looking up on
	// disk — it exists so the fake Readdir entries below (which list it
	// for /textures and /terrain) resolve to *something* real instead of
	// ENOENT, without ever touching the real backing directory.
	if strings.HasSuffix(path, "AOISWORKING") {
		now := fuse.Now()
		stat.Mode = fuse.S_IFREG | 0o444
		stat.Nlink = 1
		stat.Size = 0
		stat.Mtim, stat.Atim, stat.Ctim = now, now, now
		return 0
	}

	full := a.fullPath(path)
	info, err := os.Lstat(full)
	if err != nil {
		return -fuse.ENOENT
	}
	fillStatFromFileInfo(stat, info)
	return 0
}

func (a *AutoOrtho) Open(path string, flags int) (errc int, fh uint64) {
	fh = ^uint64(0)
	defer func() {
		if r := recover(); r != nil {
			a.panics.Add(1)
			a.log.Error("panic recovered in FUSE callback — serving an error instead of crashing the mount",
				"op", "Open", "path", path, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			errc, fh = -fuse.EIO, ^uint64(0)
		}
	}()

	if row, col, maptype, zoom, ok := parseDDSPath(path); ok {
		maptype = a.resolveMaptype(maptype)
		if _, ok := a.cacher.Open(row, col, maptype, zoom, a.minZoom, a.maxZoom); !ok {
			a.log.Warn("open: unknown maptype", "path", path, "maptype", maptype)
			return -fuse.ENOENT, ^uint64(0)
		}
		a.opens.Add(1)
		a.log.Debug("open dds", "path", path, "row", row, "col", col, "maptype", maptype, "zoom", zoom)
		return 0, 0
	}

	full := a.fullPath(path)
	f, err := os.OpenFile(full, flags, 0o644)
	if err != nil {
		a.log.Debug("open passthrough failed", "path", path, "err", err)
		return -fuse.ENOENT, ^uint64(0)
	}

	a.mu.Lock()
	a.nextFH++
	h := a.nextFH
	a.handles[h] = f
	a.mu.Unlock()
	a.opens.Add(1)
	return 0, h
}

// Read is the hot path, called repeatedly by X-Plane as it streams in a
// texture. It intentionally re-parses path on every call (rather than
// stashing row/col/maptype/zoom on open) because fh is always 0 for dds
// paths — matching autoortho_fuse.py's read(), which re-matches dds_re
// here too instead of carrying state from open(). It's also the path most
// exposed to recoverPanic: every mipmap build here ends in a cgo call
// (native JPEG decode / BC1 compress) fed bytes that came off the network,
// which is exactly the kind of input that occasionally finds an edge case
// a controlled Go error wouldn't.
func (a *AutoOrtho) Read(path string, buff []byte, ofst int64, fh uint64) (n int) {
	defer a.recoverPanic(&n, "Read", path)

	if row, col, maptype, zoom, ok := parseDDSPath(path); ok {
		maptype = a.resolveMaptype(maptype)
		t, ok := a.cacher.Get(row, col, maptype, zoom)
		if !ok {
			a.log.Warn("read: dds tile not open", "path", path)
			return -fuse.EIO
		}
		data := t.ReadDDSBytes(context.Background(), int(ofst), len(buff))
		return copy(buff, data)
	}

	a.mu.Lock()
	f, ok := a.handles[fh]
	a.mu.Unlock()
	if !ok {
		return -fuse.EIO
	}
	got, err := f.ReadAt(buff, ofst)
	if err != nil && got == 0 {
		return 0
	}
	return got
}

func (a *AutoOrtho) Release(path string, fh uint64) (errc int) {
	defer a.recoverPanic(&errc, "Release", path)

	if row, col, maptype, zoom, ok := parseDDSPath(path); ok {
		maptype = a.resolveMaptype(maptype)
		a.cacher.Close(row, col, maptype, zoom)
		a.opens.Add(-1)
		return 0
	}

	a.mu.Lock()
	f, ok := a.handles[fh]
	delete(a.handles, fh)
	a.mu.Unlock()
	if !ok {
		return -fuse.EIO
	}
	a.opens.Add(-1)
	if err := f.Close(); err != nil {
		return -fuse.EIO
	}
	return 0
}

func (a *AutoOrtho) Opendir(path string) (errc int, fh uint64) {
	fh = ^uint64(0)
	defer func() {
		if r := recover(); r != nil {
			a.panics.Add(1)
			a.log.Error("panic recovered in FUSE callback — serving an error instead of crashing the mount",
				"op", "Opendir", "path", path, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			errc, fh = -fuse.EIO, ^uint64(0)
		}
	}()

	full := a.fullPath(path)
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		return -fuse.ENOENT, ^uint64(0)
	}
	return 0, 0
}

// Readdir mirrors autoortho_fuse.py's readdir special case for /textures
// and /terrain: X-Plane never needs a real directory listing to open a
// specific .dds/.ter/.dsf file by name (it already has the exact filename
// from the DSF scenery data it loaded separately) — readdir on these two
// directories is essentially vestigial for X-Plane's own access pattern,
// but naively honoring it is NOT free: aus_pac's real "terrain" directory
// alone holds 276,198 files, and a real os.ReadDir of it measured ~5
// seconds wall clock. Python sidesteps this entirely with a hardcoded
// stub list instead of ever scanning the real directory; this does the
// same, rather than a cache on top of the real (still expensive-at-least-
// once) scan. Every other directory still gets a real listing.
func (a *AutoOrtho) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) (errc int) {
	defer a.recoverPanic(&errc, "Readdir", path)

	switch path {
	case "/textures":
		fill(".", nil, 0)
		fill("..", nil, 0)
		fill(".AOISWORKING", nil, 0)
		fill("24832_12416_BI16.dds", nil, 0)
		return 0
	case "/terrain":
		fill(".", nil, 0)
		fill("..", nil, 0)
		fill(".AOISWORKING", nil, 0)
		return 0
	}

	full := a.fullPath(path)
	entries, err := os.ReadDir(full)
	if err != nil {
		return -fuse.ENOENT
	}
	fill(".", nil, 0)
	fill("..", nil, 0)
	for _, e := range entries {
		fill(e.Name(), nil, 0)
	}
	return 0
}

// Statfs and fillStatFromFileInfo's uid/gid/nlink enrichment are
// platform-specific (statfs_linux.go/statfs_darwin.go/statfs_windows.go):
// the underlying statfs(2)/stat(2) struct fields differ, and Windows has
// neither call at all. fillStatFromFileInfo itself stays here since the
// mode/size/mtime portion is identical everywhere.

// fillStatFromFileInfo copies real on-disk metadata into a FUSE Stat_t for
// passthrough paths — mirrors autoortho_fuse.py's getattr() building its
// attrs dict from os.lstat() for anything that isn't a synthesized tile.
func fillStatFromFileInfo(stat *fuse.Stat_t, info os.FileInfo) {
	mode := uint32(info.Mode().Perm())
	if info.IsDir() {
		mode |= fuse.S_IFDIR
	} else {
		mode |= fuse.S_IFREG
	}
	stat.Mode = mode
	stat.Size = info.Size()
	now := fuse.NewTimespec(info.ModTime())
	stat.Mtim, stat.Atim, stat.Ctim = now, now, now
	stat.Nlink = 1
	enrichStatFromSys(stat, info)
}
