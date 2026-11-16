//go:build windows

package scenery

import "golang.org/x/sys/windows"

// freeBytes implements the Windows half of DESIGN.md §11's disk-space
// preflight check via GetDiskFreeSpaceExW — the direct Win32 equivalent of
// linux/darwin's statfs(2) call in diskspace_unix.go. Pure syscall, no cgo,
// so it needs no C toolchain to build (unlike the native DXT/JPEG cgo
// code, which does).
func freeBytes(path string) (uint64, bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false, err
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &totalBytes, &totalFree); err != nil {
		return 0, false, err
	}
	return freeAvail, true, nil
}
