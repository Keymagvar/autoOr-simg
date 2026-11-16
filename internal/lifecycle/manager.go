package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/winfsp/cgofuse/fuse"

	aofuse "autoOr-simg/internal/fuse"
	"autoOr-simg/internal/logging"
)

// Manager owns the single-instance lock and every currently-mounted FUSE
// filesystem, and provides the one guaranteed shutdown sequence from
// DESIGN.md §4(c)/§8: unmount everything (each bounded so one wedged mount
// can't hang the others), THEN release the lock — in that order, so a
// shutdown interrupted partway through still reads as "running" to the
// next startup's stale-mount check instead of lying about being clean.
type Manager struct {
	log  *slog.Logger
	lock *InstanceLock

	mu     sync.Mutex
	mounts map[string]*aofuse.Controller // mountpoint -> controller

	shutdownOnce sync.Once
}

// NewManager acquires the single-instance lock at lockPath — the one
// required precondition before mounting anything (DESIGN.md §4(a)).
// Returns *ErrAlreadyRunning (via AcquireInstanceLock) if another live
// instance already holds it; callers should treat that as "refuse to
// start, don't try to mount anyway."
func NewManager(lockPath string) (*Manager, error) {
	lock, err := AcquireInstanceLock(lockPath)
	if err != nil {
		return nil, err
	}
	return &Manager{
		log:    logging.For("lifecycle"),
		lock:   lock,
		mounts: map[string]*aofuse.Controller{},
	}, nil
}

// MountFresh clears mountpoint first if DESIGN.md §4(b)'s stale-mount
// check finds something already mounted there (a leftover from a previous
// crash), then mounts fsop and tracks it for Shutdown.
func (m *Manager) MountFresh(ctx context.Context, mountpoint string, fsop fuse.FileSystemInterface) error {
	mounted, err := IsMounted(mountpoint)
	if err != nil {
		m.log.Warn("stale-mount check failed, proceeding to mount anyway", "mountpoint", mountpoint, "err", err)
	} else if mounted {
		m.log.Warn("stale mount found at startup, forcing it down before remounting", "mountpoint", mountpoint)
		if err := ForceUnmount(ctx, mountpoint); err != nil {
			return fmt.Errorf("clearing stale mount at %s: %w", mountpoint, err)
		}
	}

	ctrl, err := aofuse.Mount(mountpoint, fsop)
	if err != nil {
		return fmt.Errorf("mounting %s: %w", mountpoint, err)
	}
	if err := ctrl.WaitMounted(2 * time.Second); err != nil {
		return fmt.Errorf("mount of %s did not come up cleanly: %w", mountpoint, err)
	}

	m.mu.Lock()
	m.mounts[mountpoint] = ctrl
	m.mu.Unlock()
	m.log.Info("mounted", "mountpoint", mountpoint)
	return nil
}

// StopMounts unmounts every currently tracked mount and waits for all of
// them, but — unlike Shutdown — does NOT release the single-instance lock.
// This is DESIGN.md §8's "Stop" action: the process (and the web UI's HTTP
// server) stays alive so the user can change config and MountFresh again
// without restarting the whole program. Safe to call repeatedly (e.g.
// Stop, change config, mount again, Stop again) since it's not guarded by
// shutdownOnce — only Shutdown is a one-way door.
func (m *Manager) StopMounts() {
	m.mu.Lock()
	mounts := m.mounts
	m.mounts = map[string]*aofuse.Controller{}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for mountpoint, ctrl := range mounts {
		wg.Add(1)
		go func(mountpoint string, ctrl *aofuse.Controller) {
			defer wg.Done()
			if err := ctrl.Unmount(); err != nil {
				m.log.Error("unmount failed during stop", "mountpoint", mountpoint, "err", err)
			}
		}(mountpoint, ctrl)
	}
	wg.Wait()
	m.log.Info("all mounts stopped (instance lock retained)")
}

// UnmountOne unmounts exactly one currently-tracked mount (added via
// MountFresh) without touching any other — the per-region "Unmount" action,
// as opposed to StopMounts' all-at-once teardown. Returns an error if
// mountpoint isn't currently tracked, so a caller can tell "nothing to do"
// apart from a real unmount failure.
func (m *Manager) UnmountOne(mountpoint string) error {
	m.mu.Lock()
	ctrl, ok := m.mounts[mountpoint]
	if ok {
		delete(m.mounts, mountpoint)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("not mounted: %s", mountpoint)
	}
	if err := ctrl.Unmount(); err != nil {
		return fmt.Errorf("unmounting %s: %w", mountpoint, err)
	}
	m.log.Info("unmounted", "mountpoint", mountpoint)
	return nil
}

// Mountpoints returns the mountpoints currently tracked as mounted, for
// status reporting (e.g. the web UI's state snapshot).
func (m *Manager) Mountpoints() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.mounts))
	for mountpoint := range m.mounts {
		out = append(out, mountpoint)
	}
	return out
}

// Shutdown runs the guaranteed sequence exactly once (a second call is a
// no-op, via sync.Once — safe to call from both a normal exit path and a
// recover()-triggered panic path without double-unmounting or
// double-releasing): StopMounts (unmount everything), then release the
// single-instance lock last — DESIGN.md §8's "Shutdown" is exactly "Stop,
// then also release the lock and let the caller exit the process."
func (m *Manager) Shutdown() {
	m.shutdownOnce.Do(func() {
		m.StopMounts()

		if err := m.lock.Release(); err != nil {
			m.log.Error("releasing instance lock failed", "err", err)
			return
		}
		m.log.Info("shutdown complete, instance lock released")
	})
}
