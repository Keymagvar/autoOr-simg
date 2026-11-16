package fuse

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAutoOrtho_PanicInReadIsRecovered is a real end-to-end test, not a
// mock: it mounts an AutoOrtho filesystem with a nil *tile.Cacher (so
// opening/reading a synthesized .dds path genuinely nil-pointer-panics
// inside Cacher.Open/Get — not a simulated panic() call standing in for
// one), drives it through a real FUSE mount, and verifies the mount
// survives: the triggering request fails with an I/O error instead of
// taking the process down, PanicCount reflects it, and the mount keeps
// serving *other* requests correctly afterward (proving one bad request
// doesn't wedge or poison the whole filesystem).
func TestAutoOrtho_PanicInReadIsRecovered(t *testing.T) {
	root := t.TempDir()
	// A real scenery root always has a real "textures" dir on disk (even
	// if the .dds files inside are synthesized, not real) — path
	// resolution for "/textures/<file>.dds" requires the kernel to
	// successfully look up the "textures" directory component first, so
	// without this the test would fail at that lookup (ENOENT) before
	// ever reaching the dds branch, never exercising the panic path at
	// all. Mirrors the real `aus_pac` layout used in prior sessions.
	if err := os.MkdirAll(filepath.Join(root, "textures"), 0o755); err != nil {
		t.Fatalf("seeding textures dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("still here\n"), 0o644); err != nil {
		t.Fatalf("seeding passthrough file: %v", err)
	}

	mountpoint := t.TempDir()
	fs := NewAutoOrtho(root, nil /* cacher: deliberately nil to force a real panic */, 14, 0, "")

	ctrl, err := Mount(mountpoint, fs)
	if err != nil {
		t.Fatalf("mounting: %v", err)
	}
	if err := ctrl.WaitMounted(2 * time.Second); err != nil {
		t.Fatalf("mount did not come up: %v", err)
	}
	defer func() {
		if err := ctrl.Unmount(); err != nil {
			t.Errorf("unmount: %v", err)
		}
	}()

	ddsPath := filepath.Join(mountpoint, "textures", "100000_222560_BI18.dds")

	// Open alone should panic inside Cacher.Open(nil receiver) and be
	// recovered, surfacing as a normal open(2) failure to the caller —
	// not a hung or crashed mount.
	f, err := os.Open(ddsPath)
	if err == nil {
		f.Close()
		t.Fatal("expected Open on a nil-Cacher-backed dds path to fail with an I/O error, got success")
	}

	if got := fs.PanicCount(); got < 1 {
		t.Errorf("PanicCount() = %d, want at least 1 after the panicking open", got)
	}

	// The mount must still be alive and correctly serving *other*
	// requests — this is the real point of the test: one recovered
	// panic must not wedge or poison the whole filesystem.
	data, err := os.ReadFile(filepath.Join(mountpoint, "plain.txt"))
	if err != nil {
		t.Fatalf("passthrough read after a recovered panic should still work, got: %v", err)
	}
	if string(data) != "still here\n" {
		t.Errorf("passthrough content = %q, want %q", data, "still here\n")
	}

	// A second panicking request should also be handled cleanly (proves
	// recovery isn't a one-shot fluke tied to process/goroutine state).
	if f2, err := os.Open(ddsPath); err == nil {
		f2.Close()
		t.Fatal("expected second Open on the nil-Cacher path to also fail")
	}
	if got := fs.PanicCount(); got < 2 {
		t.Errorf("PanicCount() = %d, want at least 2 after a second panicking open", got)
	}
}
