package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	aofuse "autoOr-simg/internal/fuse"
)

// TestManager_MountFresh_ClearsStaleMount is a real end-to-end test, not a
// mock: it actually mounts a filesystem via cgofuse and abandons the
// Controller without unmounting (simulating a crashed previous process —
// the exact scenario DESIGN.md §4(b) exists for), confirms IsMounted sees
// it, then verifies Manager.MountFresh force-unmounts the stale mount and
// successfully mounts a fresh filesystem in its place, and that the fresh
// filesystem is actually readable afterward — not just that MountFresh
// returned nil.
func TestManager_MountFresh_ClearsStaleMount(t *testing.T) {
	if _, err := exec.LookPath("fusermount"); err != nil {
		t.Skip("fusermount not available in this environment")
	}

	mountpoint := t.TempDir()
	ctx := context.Background()

	staleCtrl, err := aofuse.Mount(mountpoint, aofuse.NewTrivial())
	if err != nil {
		t.Fatalf("mounting stale filesystem: %v", err)
	}
	if err := staleCtrl.WaitMounted(2 * time.Second); err != nil {
		t.Fatalf("stale mount did not come up: %v", err)
	}
	// Deliberately NOT calling staleCtrl.Unmount() here — a crashed
	// process never gets the chance to either, which is the scenario
	// under test.

	mounted, err := IsMounted(mountpoint)
	if err != nil {
		t.Fatalf("IsMounted: %v", err)
	}
	if !mounted {
		t.Fatal("expected the abandoned mount to be visible in /proc/mounts")
	}

	lockPath := filepath.Join(t.TempDir(), "autoortho.lock")
	mgr, err := NewManager(lockPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	if err := mgr.MountFresh(ctx, mountpoint, aofuse.NewTrivial()); err != nil {
		t.Fatalf("MountFresh should clear the stale mount and mount fresh, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(mountpoint, "hello.txt"))
	if err != nil {
		t.Fatalf("reading through the fresh mount: %v", err)
	}
	if string(data) == "" {
		t.Error("fresh mount served empty content — not actually the Trivial filesystem")
	}

	mgr.Shutdown()

	mounted, err = IsMounted(mountpoint)
	if err != nil {
		t.Fatalf("IsMounted after Shutdown: %v", err)
	}
	if mounted {
		t.Error("expected the mount to be gone after Shutdown")
	}
}

// TestManager_SecondInstanceRefused proves the DESIGN.md §4(a) guarantee
// end to end through the Manager, not just the lower-level lock: a second
// Manager pointed at the same lock file must fail to construct while the
// first is alive, with the specific error type identifying the error.
func TestManager_SecondInstanceRefused(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "autoortho.lock")

	first, err := NewManager(lockPath)
	if err != nil {
		t.Fatalf("first NewManager: %v", err)
	}
	defer first.Shutdown()

	_, err = NewManager(lockPath)
	if err == nil {
		t.Fatal("second NewManager should have been refused while the first is still running")
	}
	if _, ok := err.(*ErrAlreadyRunning); !ok {
		t.Errorf("expected *ErrAlreadyRunning, got %T: %v", err, err)
	}

	first.Shutdown()

	second, err := NewManager(lockPath)
	if err != nil {
		t.Fatalf("NewManager after first Shutdown should succeed, got: %v", err)
	}
	second.Shutdown()
}
