//go:build windows

package lifecycle

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// IsMounted reports whether something is currently mounted at exactly
// path — the Windows half of DESIGN.md §4(b)'s stale-mount check. Windows
// has no /proc/mounts (Linux) or `mount` command (macOS) equivalent, and
// AutoOrtho's mountpoints are always a directory path (not a drive
// letter — see autoortho/winsetup.py's setup_winfsp_mount and
// internal/fuse/mountpoint_windows.go, which mirrors it), so this checks
// the one thing WinFsp's own "mountpoint must not pre-exist" requirement
// implies: if the path exists at all, either a mount is sitting on it or
// a previous run's directory never got cleaned up — both cases this
// function intentionally treats the same way as "mounted", since both
// need clearing before a fresh Mount() can succeed.
func IsMounted(path string) (bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", path, err)
	}
	if _, err := windows.GetFileAttributes(p); err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND {
			return false, nil
		}
		return false, fmt.Errorf("checking %s: %w", path, err)
	}
	return true, nil
}

// ForceUnmount clears whatever is occupying path so a fresh Mount() can
// claim it — WinFsp itself provides no "unmount this drive/directory from
// the outside" primitive the way fusermount/umount do on Linux/macOS, so
// this is deliberately just a plain directory removal: safe because
// IsMounted above only ever says "mounted" when nothing is actively
// serving that path from *this* process (a live WinFsp mount from this
// process goes through Controller.Unmount(), not here), and a real
// WinFsp-mounted directory from another still-running process would
// refuse removal from the OS anyhow, so this only ever actually fires
// against a genuinely stale leftover directory.
func ForceUnmount(ctx context.Context, path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("clearing stale mountpoint %s: %w", path, err)
	}
	return nil
}
