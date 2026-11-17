package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireInstanceLock_SecondAcquireFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autoortho.lock")

	first, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	_, err = AcquireInstanceLock(path)
	if err == nil {
		t.Fatal("second acquire should have failed while first still holds the lock")
	}
	var already *ErrAlreadyRunning
	if !errors.As(err, &already) {
		t.Fatalf("expected *ErrAlreadyRunning, got %T: %v", err, err)
	}
	if already.HolderPID != os.Getpid() {
		t.Errorf("HolderPID = %d, want current pid %d (same process holds it in this test)", already.HolderPID, os.Getpid())
	}
}

func TestAcquireInstanceLock_ReacquireAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autoortho.lock")

	first, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	second, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("reacquire after release should succeed: %v", err)
	}
	defer second.Release()
}

func TestInstanceLock_ReleaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autoortho.lock")

	l, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("second release should be a harmless no-op, got: %v", err)
	}
}

func TestAcquireInstanceLock_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "autoortho.lock")

	l, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("acquire with nonexistent parent dir: %v", err)
	}
	defer l.Release()

	data, err := os.ReadFile(pidPath(path))
	if err != nil {
		t.Fatalf("reading pid file: %v", err)
	}
	if string(data) == "" {
		t.Error("pid file should contain the PID, got empty content")
	}
}
