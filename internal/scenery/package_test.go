package scenery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestPackageDownload_RefetchesDamagedExistingFile covers the "verify
// what's on disk, clean up just the damaged piece, continue" behavior:
// a destPath file that already exists but doesn't match the remote's
// reported size (e.g. left over truncated/corrupted from a prior crashed
// run) must be deleted and re-fetched, not silently trusted.
func TestPackageDownload_RefetchesDamagedExistingFile(t *testing.T) {
	const want = "the-real-full-content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(want)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(want))
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "z_foo.zip")
	if err := os.WriteFile(destPath, []byte("truncated-garbage"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewPackage("z_foo", "z", dir)
	p.RemoteURLs = []string{srv.URL + "/z_foo.zip"}

	if err := p.Download(context.Background(), srv.Client(), nil, 1, 1); err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("expected damaged file to be replaced with %q, got %q", want, got)
	}
}

// TestPackageDownload_SkipsAlreadyAssembledMultiPartZip is a regression
// test for a real-world bug: a multi-part package (parts named
// "name.zip.00", "name.zip.01", ...) whose parts were already merged into
// the final "name.zip" by a previous run's Assemble (which deletes each
// part once it's consumed) must NOT be re-downloaded from scratch just
// because the individual parts are gone — the merged file's own presence
// is proof the download phase already finished.
func TestPackageDownload_SkipsAlreadyAssembledMultiPartZip(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Errorf("unexpected request for already-assembled package: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	dir := t.TempDir()
	p := NewPackage("y_asi_overlays", "y", dir)
	p.RemoteURLs = []string{
		srv.URL + "/y_asi_overlays.zip.00",
		srv.URL + "/y_asi_overlays.zip.01",
	}

	// Simulate a previous run that finished: the merged zip exists, its
	// consumed parts don't.
	if err := os.WriteFile(p.zip.Path, []byte("a-fully-merged-zip-worth-of-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := p.Download(context.Background(), srv.Client(), nil, 1, 1); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if called {
		t.Error("expected no HTTP requests for a package whose merged zip already exists")
	}
	if !p.Downloaded {
		t.Error("expected Downloaded=true")
	}
	got, err := os.ReadFile(p.zip.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a-fully-merged-zip-worth-of-bytes" {
		t.Error("the already-assembled zip must be left untouched, not truncated/rewritten")
	}
}

// TestPackageDownload_TrustsMatchingExistingFile ensures a correctly-sized
// existing file is skipped rather than re-downloaded.
func TestPackageDownload_TrustsMatchingExistingFile(t *testing.T) {
	const content = "already-here-and-correct"
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(content)))
		if r.Method == http.MethodHead {
			return
		}
		called = true
		_, _ = w.Write([]byte(content))
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "z_foo.zip")
	if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewPackage("z_foo", "z", dir)
	p.RemoteURLs = []string{srv.URL + "/z_foo.zip"}

	if err := p.Download(context.Background(), srv.Client(), nil, 1, 1); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if called {
		t.Error("expected matching existing file to be skipped, but GET was issued")
	}
}
