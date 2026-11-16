//go:build !linux && !darwin && !windows

package scenery

// Windows is now covered by diskspace_windows.go; this stub is left for
// any genuinely unsupported build target (DESIGN.md §11) — skip the
// preflight check rather than guess.
func freeBytes(path string) (uint64, bool, error) {
	return 0, false, nil
}
