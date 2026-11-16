//go:build linux

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
	stat.Bsize = uint64(sfs.Bsize)
	stat.Frsize = uint64(sfs.Bsize)
	stat.Blocks = sfs.Blocks
	stat.Bfree = sfs.Bfree
	stat.Bavail = sfs.Bavail
	stat.Files = sfs.Files
	stat.Ffree = sfs.Ffree
	stat.Namemax = uint64(sfs.Namelen)
	return 0
}

// enrichStatFromSys adds the uid/gid/nlink/atim/ctim fields os.FileInfo
// doesn't expose directly, from the Linux *syscall.Stat_t info.Sys()
// returns. See statfs_darwin.go/statfs_windows.go for the other
// platforms' field layouts (they differ: darwin uses Atimespec/Ctimespec,
// Windows' Sys() isn't a syscall.Stat_t at all).
func enrichStatFromSys(stat *fuse.Stat_t, info os.FileInfo) {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	stat.Uid = sys.Uid
	stat.Gid = sys.Gid
	stat.Nlink = uint32(sys.Nlink)
	stat.Atim = fuse.Timespec{Sec: sys.Atim.Sec, Nsec: sys.Atim.Nsec}
	stat.Ctim = fuse.Timespec{Sec: sys.Ctim.Sec, Nsec: sys.Ctim.Nsec}
}
