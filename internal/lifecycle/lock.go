// Package lifecycle implements DESIGN.md §4's error-proofing trio: a
// single-instance lock (this file), stale-mount detection/forced cleanup
// (stalemount.go), and the guaranteed unmount-then-release shutdown
// sequence that ties them together with the real mounts (manager.go).
package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofrs/flock"
)

// InstanceLock is the DESIGN.md §4(a) single-instance guard: an exclusive
// OS-level file lock (flock(2) on Unix, LockFileEx on Windows, both via
// gofrs/flock) on a well-known lock file, with the current PID written
// inside it so a second instance can report *who* is already running, not
// just that something is.
type InstanceLock struct {
	fl   *flock.Flock
	path string
}

// ErrAlreadyRunning is returned by AcquireInstanceLock when another live
// process already holds the lock. HolderPID is the PID recorded inside the
// lock file by that process, when it could be read — 0 if the file
// couldn't be read or didn't contain a valid PID.
type ErrAlreadyRunning struct {
	Path      string
	HolderPID int
}

func (e *ErrAlreadyRunning) Error() string {
	if e.HolderPID > 0 {
		return fmt.Sprintf("autoortho is already running (pid %d), lock held at %s", e.HolderPID, e.Path)
	}
	return fmt.Sprintf("autoortho is already running, lock held at %s", e.Path)
}

// AcquireInstanceLock tries to take the exclusive lock at path, creating
// the file (and its parent dir) if needed, and writes the current PID into
// it on success. Returns *ErrAlreadyRunning (not a generic error, so
// callers can errors.As against it specifically) if another live process
// holds it already.
func AcquireInstanceLock(path string) (*InstanceLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating lock file dir: %w", err)
	}

	fl := flock.New(path)
	ok, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	if !ok {
		return nil, &ErrAlreadyRunning{Path: path, HolderPID: readLockPID(path)}
	}

	// Truncate+write happens after the lock is held, so a reader never
	// sees a half-written PID from a process that's mid-startup.
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		_ = fl.Unlock()
		return nil, fmt.Errorf("writing pid to lock file: %w", err)
	}

	return &InstanceLock{fl: fl, path: path}, nil
}

func readLockPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// Release gives up the lock. Idempotent — a second call is a harmless
// no-op (gofrs/flock's Unlock() returns nil if not currently locked). Per
// DESIGN.md §4(c), this must be the LAST step of shutdown: called only
// once every mount is confirmed down, so a half-finished shutdown still
// looks "running" to the next startup's stale-mount check instead of
// lying about having cleaned up.
func (l *InstanceLock) Release() error {
	return l.fl.Unlock()
}
