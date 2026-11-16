package scenery

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingResponseWriter tracks how many response body bytes a handler
// actually wrote, so a test can assert a resumed download only transferred
// the remaining bytes instead of the whole file again.
type countingResponseWriter struct {
	http.ResponseWriter
	n *int64
}

func (c *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	atomic.AddInt64(c.n, int64(n))
	return n, err
}

// TestDownloadFile_ResumesPartialFile covers step 5: an existing ".part"
// staging file (left over from a killed/crashed process, or this run's own
// context being cancelled by a WS disconnect) is resumed via a Range
// request instead of being re-fetched from byte 0.
func TestDownloadFile_ResumesPartialFile(t *testing.T) {
	content := strings.Repeat("0123456789", 1000) // 10000 bytes, Range-able
	var bodyBytesWritten int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &countingResponseWriter{ResponseWriter: w, n: &bodyBytesWritten}
		http.ServeContent(cw, r, "z_foo.zip", time.Time{}, strings.NewReader(content))
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "z_foo.zip")
	tmp := destPath + ".part"
	const already = 4000
	if err := os.WriteFile(tmp, []byte(content[:already]), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadFile(context.Background(), srv.Client(), logFor(), srv.URL+"/z_foo.zip", destPath, nil, "z_foo", 1, 1); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("final file wrong: got %d bytes, want %d", len(got), len(content))
	}
	if int(bodyBytesWritten) >= len(content) {
		t.Errorf("expected only the remaining ~%d bytes to be transferred, server sent %d (looks like a full re-download)", len(content)-already, bodyBytesWritten)
	}
}

// TestDownloadFile_FallsBackWhenServerIgnoresRange covers the case where
// the remote doesn't support Range requests (sends 200 instead of 206):
// the partial ".part" must be discarded and overwritten fresh, not
// corrupted by appending the full body onto existing bytes.
func TestDownloadFile_FallsBackWhenServerIgnoresRange(t *testing.T) {
	content := "the-complete-file-content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately ignores any Range header and always sends 200 + the
		// full body, like a mirror/CDN with no range support.
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "z_foo.zip")
	tmp := destPath + ".part"
	if err := os.WriteFile(tmp, []byte("stale-partial-junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadFile(context.Background(), srv.Client(), logFor(), srv.URL+"/z_foo.zip", destPath, nil, "z_foo", 1, 1); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("got %q, want %q (stale partial bytes must not leak into the final file)", got, content)
	}
}

// TestDownloadFile_RestartsWhenResumeOffsetBeyondRemoteSize covers a
// ".part" file larger than what the server actually has (can only be a
// stale/corrupt leftover) — the server's 416 Range Not Satisfiable must
// trigger a clean restart, not a hang or a corrupted result.
func TestDownloadFile_RestartsWhenResumeOffsetBeyondRemoteSize(t *testing.T) {
	content := "short"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "z_foo.zip", time.Time{}, strings.NewReader(content))
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "z_foo.zip")
	tmp := destPath + ".part"
	if err := os.WriteFile(tmp, []byte("this-partial-is-longer-than-the-real-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadFile(context.Background(), srv.Client(), logFor(), srv.URL+"/z_foo.zip", destPath, nil, "z_foo", 1, 1); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("got %q, want %q", got, content)
	}
}

// TestDownloadFile_CancelledContextKeepsPartialFile covers the other half
// of the resume fix: when ctx is cancelled mid-transfer (e.g. a browser
// reload that previously killed the whole install), the ".part" file must
// survive so a later retry can resume from it instead of starting over.
func TestDownloadFile_CancelledContextKeepsPartialFile(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "20")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("0123456789"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(started)
		<-block // hold the response open until the test cancels ctx
	}))
	defer srv.Close()
	defer close(block)

	dir := t.TempDir()
	destPath := filepath.Join(dir, "z_foo.zip")
	tmp := destPath + ".part"

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- downloadFile(ctx, srv.Client(), logFor(), srv.URL+"/z_foo.zip", destPath, nil, "z_foo", 1, 1)
	}()

	<-started
	// Give the client a moment to actually finish receiving/processing the
	// response headers before we cancel — otherwise cancellation can race
	// ahead of client.Do() itself returning, which would fail for an
	// uninteresting reason (no request ever really started) instead of
	// the thing this test wants to exercise (cancellation mid-body-copy).
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-errCh; err == nil {
		t.Fatal("expected downloadFile to return an error after cancellation")
	}

	if _, err := os.Stat(tmp); err != nil {
		t.Errorf("expected .part file to survive cancellation for later resume, but it's gone: %v", err)
	}
	if _, err := os.Stat(destPath); err == nil {
		t.Error("destPath should not exist after a cancelled download")
	}
}
