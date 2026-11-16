package webui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFileWithAge(t *testing.T, path string, size int, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	modTime := time.Now().Add(-age)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestCleanCache_RemovesEmptyFilesRegardlessOfSize(t *testing.T) {
	dir := t.TempDir()
	writeFileWithAge(t, filepath.Join(dir, "empty.dds"), 0, time.Hour)
	writeFileWithAge(t, filepath.Join(dir, "real.dds"), 1024, time.Hour)

	removed, _, err := cleanCache(dir, 10)
	if err != nil {
		t.Fatalf("cleanCache: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (just the empty file)", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "real.dds")); err != nil {
		t.Fatalf("real.dds should still exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "empty.dds")); !os.IsNotExist(err) {
		t.Fatalf("empty.dds should have been removed, stat err = %v", err)
	}
}

func TestCleanCache_RemovesOldestFilesFirstWhenOverLimit(t *testing.T) {
	dir := t.TempDir()
	const fileSize = 1 << 20 // 1MB
	// 15 files of 1MB each = 15MB total, well over a 10MB target.
	for i := 0; i < 15; i++ {
		age := time.Duration(15-i) * time.Minute // file 0 is oldest
		writeFileWithAge(t, filepath.Join(dir, fileName(i)), fileSize, age)
	}

	removed, freed, err := cleanCacheToBytes(dir, 10*fileSize)
	if err != nil {
		t.Fatalf("cleanCacheToBytes: %v", err)
	}
	if removed == 0 {
		t.Fatalf("expected some files removed to get under the 10MB target, removed=0")
	}
	if freed == 0 {
		t.Fatalf("expected freed bytes > 0")
	}

	// The oldest file (index 0) must be gone; the newest (index 14) must survive.
	if _, err := os.Stat(filepath.Join(dir, fileName(0))); !os.IsNotExist(err) {
		t.Fatalf("oldest file should have been removed first, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName(14))); err != nil {
		t.Fatalf("newest file should still exist: %v", err)
	}
}

func TestCleanCache_NoopWhenUnderLimit(t *testing.T) {
	dir := t.TempDir()
	writeFileWithAge(t, filepath.Join(dir, "small.dds"), 1024, time.Hour)

	removed, freed, err := cleanCache(dir, 10)
	if err != nil {
		t.Fatalf("cleanCache: %v", err)
	}
	if removed != 0 || freed != 0 {
		t.Fatalf("expected no-op under the size limit, got removed=%d freed=%d", removed, freed)
	}
}

func fileName(i int) string {
	return "tile_" + string(rune('a'+i)) + ".dds"
}
