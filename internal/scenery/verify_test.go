package scenery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRelease_VerifyInstalled(t *testing.T) {
	t.Run("all present and non-empty", func(t *testing.T) {
		base := t.TempDir()
		dirA := filepath.Join(base, "z_ao_sa")
		dirB := filepath.Join(base, "yAutoOrtho_Overlays")
		for _, d := range []string{dirA, dirB} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "tile.dsf"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		r := NewRelease("sa", base, base)
		r.OrthoDirs = []string{dirA, dirB}

		ok, msg, err := r.VerifyInstalled()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Errorf("expected ok=true, got false (msg: %s)", msg)
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		base := t.TempDir()
		dirA := filepath.Join(base, "z_ao_sa")
		if err := os.MkdirAll(dirA, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dirA, "tile.dsf"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		missingDir := filepath.Join(base, "does_not_exist")

		r := NewRelease("sa", base, base)
		r.OrthoDirs = []string{dirA, missingDir}

		ok, msg, err := r.VerifyInstalled()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected ok=false when a directory is missing")
		}
		if msg == "" {
			t.Error("expected a non-empty explanatory message")
		}
	})

	t.Run("empty directory counts as missing", func(t *testing.T) {
		base := t.TempDir()
		emptyDir := filepath.Join(base, "z_ao_sa")
		if err := os.MkdirAll(emptyDir, 0o755); err != nil {
			t.Fatal(err)
		}

		r := NewRelease("sa", base, base)
		r.OrthoDirs = []string{emptyDir}

		ok, _, err := r.VerifyInstalled()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected ok=false for an empty scenery directory")
		}
	})

	t.Run("no known ortho dirs", func(t *testing.T) {
		r := NewRelease("sa", t.TempDir(), t.TempDir())
		ok, msg, err := r.VerifyInstalled()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected ok=false with no OrthoDirs recorded")
		}
		if msg == "" {
			t.Error("expected an explanatory message")
		}
	})
}
