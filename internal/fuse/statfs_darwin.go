//go:build darwin

package fuse

import (
	"os"
	"syscall"

	"github.com/winfsp/cgofuse/fuse"
)

func (a *AutoOrtho) Statfs(path string, stat *fuse.Statfs_t) (errc int) {
	defer a.recoverPanic(&errc, "Statfs", path)

	full := a.fullPath(path)
	var sfs syscall.Statfs_t
	if err := syscall.Statfs(full, &sfs); err != nil {
		stat.Bsize, stat.Frsize = 4096, 4096
		stat.Blocks, stat.Bfree, stat.Bavail = 1, 1, 1
		stat.Files, stat.Ffree = 1, 1
		return 0
	}
	// darwin's syscall.Statfs_t has no Namelen field (unlike linux); 255
	// is the real HFS+/APFS filename limit, same value macOS itself
	// reports via pathconf(_PC_NAME_MAX).
	stat.Bsize = uint64(sfs.Bsize)
	stat.Frsize = uint64(sfs.Bsize)
	stat.Blocks = sfs.Blocks
	stat.Bfree = sfs.Bfree
	stat.Bavail = sfs.Bavail
	stat.Files = sfs.Files
	stat.Ffree = sfs.Ffree
	stat.Namemax = 255
	return 0
}

// enrichStatFromSys adds the uid/gid/nlink/atim/ctim fields os.FileInfo
// doesn't expose directly, from the darwin *syscall.Stat_t info.Sys()
// returns — field names differ from Linux's (Atimespec/Ctimespec, not
// Atim/Ctim). See statfs_linux.go/statfs_windows.go for the other
// platforms.
func enrichStatFromSys(stat *fuse.Stat_t, info os.FileInfo) {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	stat.Uid = sys.Uid
	stat.Gid = sys.Gid
	stat.Nlink = uint32(sys.Nlink)
	stat.Atim = fuse.Timespec{Sec: sys.Atimespec.Sec, Nsec: sys.Atimespec.Nsec}
	stat.Ctim = fuse.Timespec{Sec: sys.Ctimespec.Sec, Nsec: sys.Ctimespec.Nsec}
}
