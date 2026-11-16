//go:build windows

package fuse

import (
	"fmt"
	"os"
)

// ensureMountpoint mirrors autoortho/winsetup.py's setup_winfsp_mount:
// WinFsp requires the mountpoint NOT exist at Mount() time (the opposite
// of Linux/macOS — see mountpoint_unix.go), so if path already exists it
// gets renamed out of the way to "<path>_bkp" rather than left for
// Mount() to fail against. If "<path>_bkp" is also already taken, this
// gives up and asks for manual cleanup rather than guessing which stale
// directory is safe to delete.
func ensureMountpoint(path string) error {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	backup := path + "_bkp"
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("mountpoint %s already exists and backup location %s is also taken; manual cleanup required", path, backup)
	} else if !os.IsNotExist(err) {
		return err
	}

	return os.Rename(path, backup)
}
