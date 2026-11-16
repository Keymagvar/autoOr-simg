//go:build windows

package fuse

import (
	"os"

	"github.com/winfsp/cgofuse/fuse"
	"golang.org/x/sys/windows"
)

// blockSize is a fixed stand-in for the real NTFS cluster size: unlike
// linux/darwin's statfs(2), GetDiskFreeSpaceEx reports byte counts
// directly rather than a block size + block counts, so one is picked here
// (same value the Statfs_t error-fallback branch already used on every
// platform) purely to convert those byte counts into the fuse.Statfs_t
// block-count fields WinFsp expects.
const blockSize = 4096

func (a *AutoOrtho) Statfs(path string, stat *fuse.Statfs_t) (errc int) {
	defer a.recoverPanic(&errc, "Statfs", path)

	full := a.fullPath(path)
	p, err := windows.UTF16PtrFromString(full)
	if err != nil {
		stat.Bsize, stat.Frsize = blockSize, blockSize
		stat.Blocks, stat.Bfree, stat.Bavail = 1, 1, 1
		stat.Files, stat.Ffree = 1, 1
		return 0
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &totalBytes, &totalFree); err != nil {
		stat.Bsize, stat.Frsize = blockSize, blockSize
		stat.Blocks, stat.Bfree, stat.Bavail = 1, 1, 1
		stat.Files, stat.Ffree = 1, 1
		return 0
	}
	stat.Bsize = blockSize
	stat.Frsize = blockSize
	stat.Blocks = totalBytes / blockSize
	stat.Bfree = totalFree / blockSize
	stat.Bavail = freeAvail / blockSize
	stat.Namemax = 255
	return 0
}

// enrichStatFromSys is a no-op on Windows: os.FileInfo.Sys() returns
// *syscall.Win32FileAttributeData there, which has no uid/gid/nlink
// concept to copy — fillStatFromFileInfo's generic defaults (Nlink=1,
// mtime for every timestamp) are all WinFsp gets. See
// statfs_linux.go/statfs_darwin.go for the platforms that do have this.
func enrichStatFromSys(stat *fuse.Stat_t, info os.FileInfo) {}
