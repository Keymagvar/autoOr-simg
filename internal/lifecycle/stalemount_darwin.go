//go:build darwin

package lifecycle

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"autoOr-simg/internal/logging"
)

// IsMounted reports whether something is currently mounted at exactly
// path — the macOS half of DESIGN.md §4(b)'s stale-mount check. macOS has
// no /proc/mounts (that's Linux-specific), so this shells out to the
// system `mount` command instead, same style as ForceUnmount below
// already does on every platform. Each line looks like
// "<device> on <path> (<type>, <options>)"; we match the path between
// " on " and the trailing " (".
func IsMounted(path string) (bool, error) {
	want := cleanMountPath(path)

	out, err := exec.Command("mount").Output()
	if err != nil {
		return false, fmt.Errorf("running mount: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		onIdx := strings.Index(line, " on ")
		if onIdx < 0 {
			continue
		}
		rest := line[onIdx+len(" on "):]
		parenIdx := strings.LastIndex(rest, " (")
		if parenIdx < 0 {
			continue
		}
		if cleanMountPath(rest[:parenIdx]) == want {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func cleanMountPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// ForceUnmount tears down whatever is mounted at path using external OS
// tools directly — the macOS equivalent of the Linux
// fusermount/fusermount3/umount sequence in stalemount_linux.go. macFUSE
// mounts respond to a plain `umount`; `diskutil unmount force` is tried as
// a fallback for the rarer case of a wedged mount umount alone won't
// release, mirroring DESIGN.md §4(b)'s "shell out as a fallback for a
// truly wedged mount."
func ForceUnmount(ctx context.Context, path string) error {
	log := logging.For("lifecycle")

	attempts := [][]string{
		{"umount", path},
		{"diskutil", "unmount", "force", path},
	}

	var lastErr error
	for _, args := range attempts {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := exec.CommandContext(cctx, args[0], args[1:]...).CombinedOutput()
		cancel()
		if err == nil {
			log.Info("force-unmounted stale mount", "path", path, "tool", args[0])
			return nil
		}
		lastErr = fmt.Errorf("%s: %w (%s)", args[0], err, strings.TrimSpace(string(out)))
		log.Debug("unmount attempt failed, trying next strategy", "path", path, "tool", args[0], "err", lastErr)
	}
	return fmt.Errorf("force-unmounting %s: all strategies failed, last error: %w", path, lastErr)
}
