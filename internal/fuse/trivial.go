// Package fuse hosts the cgofuse-based filesystem. This file is the
// migration-order step-3 proof-of-concept from autoortho/AGENTS.md: a
// trivial, fixed, in-memory filesystem exercising mount/read/unmount end to
// end, before any tile-fetching logic exists (DESIGN.md §1). It exposes a
// single read-only file, "hello.txt", with fixed content.
package fuse

import (
	"log/slog"
	"sync"

	"github.com/winfsp/cgofuse/fuse"

	"autoOr-simg/internal/logging"
)

const (
	trivialFileName    = "/hello.txt"
	trivialFileContent = "autoOr-simg FUSE proof-of-concept: mount/read/unmount works.\n"
)

// Trivial is the smallest useful cgofuse.FileSystemInterface implementation:
// one fixed file, read-only, no real backing store. It exists purely to
// validate the mount/unmount lifecycle (including clean shutdown) before
// the real tile engine (internal/tile, not yet implemented) is wired in.
type Trivial struct {
	fuse.FileSystemBase
	log *slog.Logger

	mu    sync.Mutex
	opens int // outstanding open file handles, for shutdown diagnostics
}

func NewTrivial() *Trivial {
	return &Trivial{log: logging.For("fuse")}
}

func (t *Trivial) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	now := fuse.Now()
	switch path {
	case "/":
		stat.Mode = fuse.S_IFDIR | 0o755
		stat.Nlink = 2
		stat.Mtim, stat.Atim, stat.Ctim = now, now, now
		return 0
	case trivialFileName:
		stat.Mode = fuse.S_IFREG | 0o444
		stat.Nlink = 1
		stat.Size = int64(len(trivialFileContent))
		stat.Mtim, stat.Atim, stat.Ctim = now, now, now
		return 0
	default:
		return -fuse.ENOENT
	}
}

func (t *Trivial) Open(path string, flags int) (int, uint64) {
	if path != trivialFileName {
		return -fuse.ENOENT, ^uint64(0)
	}
	t.mu.Lock()
	t.opens++
	n := t.opens
	t.mu.Unlock()
	t.log.Debug("open", "path", path, "outstanding_opens", n)
	return 0, 0
}

func (t *Trivial) Read(path string, buff []byte, ofst int64, fh uint64) int {
	if path != trivialFileName {
		return -fuse.ENOENT
	}
	content := trivialFileContent
	if ofst >= int64(len(content)) {
		return 0
	}
	n := copy(buff, content[ofst:])
	return n
}

func (t *Trivial) Release(path string, fh uint64) int {
	if path == trivialFileName {
		t.mu.Lock()
		t.opens--
		n := t.opens
		t.mu.Unlock()
		t.log.Debug("release", "path", path, "outstanding_opens", n)
	}
	return 0
}

func (t *Trivial) Opendir(path string) (int, uint64) {
	if path != "/" {
		return -fuse.ENOENT, ^uint64(0)
	}
	return 0, 0
}

func (t *Trivial) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	if path != "/" {
		return -fuse.ENOENT
	}
	fill(".", nil, 0)
	fill("..", nil, 0)
	fill(trivialFileName[1:], nil, 0)
	return 0
}

func (t *Trivial) Statfs(path string, stat *fuse.Statfs_t) int {
	stat.Bsize = 4096
	stat.Frsize = 4096
	stat.Blocks = 1024
	stat.Bfree = 1024
	stat.Bavail = 1024
	stat.Files = 2
	stat.Ffree = 1000
	return 0
}

func (t *Trivial) Init() {
	t.log.Info("filesystem init")
}

func (t *Trivial) Destroy() {
	t.log.Info("filesystem destroy")
}

// OutstandingOpens reports how many file handles are currently open — used
// by the shutdown path (DESIGN.md §8) to log whether unmount happened with
// handles still outstanding, which would indicate X-Plane (or whatever
// client) didn't release cleanly.
func (t *Trivial) OutstandingOpens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.opens
}
