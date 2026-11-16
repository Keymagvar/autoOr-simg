//go:build linux

package lifecycle

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"autoOr-simg/internal/logging"
)

// IsMounted reports whether something is currently mounted at exactly
// path, by scanning /proc/mounts — the Linux half of DESIGN.md §4(b)'s
// stale-mount check. Deliberately answers "is anything mounted here", not
// "is an AutoOrtho mount here": whatever's occupying the mountpoint has to
// come down before a fresh mount can go up, regardless of what left it
// there.
func IsMounted(path string) (bool, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false, fmt.Errorf("reading /proc/mounts: %w", err)
	}
	defer f.Close()

	want := cleanMountPath(path)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if unescapeMountField(fields[1]) == want {
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

// unescapeMountField decodes the octal escapes (\040 for space, \011 tab,
// \012 newline, \134 backslash) the kernel uses for whitespace/backslash
// in /proc/mounts fields, so paths containing spaces compare correctly.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if code, err := strconv.ParseUint(s[i+1:i+4], 8, 16); err == nil {
				b.WriteByte(byte(code))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ForceUnmount tears down whatever is mounted at path using external OS
// tools directly, not the cgofuse Controller API — this is specifically
// for a *stale* mount left behind by a previous, no-longer-running
// process, so this process has no fuse.Controller for it to politely
// unmount through. Tries fusermount/fusermount3 first (the normal path for
// a user-space FUSE mount on Linux), then falls back to umount — mirrors
// DESIGN.md §4(b)'s "a direct fusermount -u/umount shell-out as a fallback
// for a truly wedged mount."
func ForceUnmount(ctx context.Context, path string) error {
	log := logging.For("lifecycle")

	attempts := [][]string{
		{"fusermount", "-u", path},
		{"fusermount3", "-u", path},
		{"umount", path},
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
