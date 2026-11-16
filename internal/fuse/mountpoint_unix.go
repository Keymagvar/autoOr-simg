//go:build linux || darwin

package fuse

import (
	"fmt"
	"os"
)

// ensureMountpoint handles the platform-specific mountpoint lifecycle
// quirks noted in autoortho/AGENTS.md (setupmount()): on Linux/macOS the
// directory just needs to exist (and be empty, so a real mount doesn't
// silently shadow leftover files). See mountpoint_windows.go for WinFsp's
// opposite requirement.
func ensureMountpoint(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("mountpoint %s is not empty", path)
	}
	return nil
}
