//go:build linux || darwin

package scenery

import "syscall"

func freeBytes(path string) (uint64, bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false, err
	}
	// Bavail is blocks available to an unprivileged user; Bsize is the
	// filesystem block size. Both fields exist (with these names) on both
	// linux and darwin's syscall.Statfs_t.
	return uint64(st.Bavail) * uint64(st.Bsize), true, nil
}
