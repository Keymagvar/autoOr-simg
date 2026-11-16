package scenery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Package is one downloadable asset group — either an ortho ("z_...") or
// overlay ("y_...") package, possibly split across multiple part files.
// Mirrors downloader.py's Package class.
type Package struct {
	Name        string
	PkgType     string // "y" or "z"
	DownloadDir string
	InstallDir  string
	RemoteURLs  []string

	Installed  bool
	Downloaded bool

	zip *Zip
	log *slog.Logger
}

func NewPackage(name, pkgType, downloadDir string) *Package {
	log := logFor().With("package", name)
	return &Package{
		Name:        name,
		PkgType:     pkgType,
		DownloadDir: downloadDir,
		zip:         &Zip{Path: filepath.Join(downloadDir, name+".zip"), log: log},
		log:         log,
	}
}

// Download fetches every remote URL into DownloadDir, then assembles
// multi-part zips. An existing destPath is trusted only if its size matches
// what the server reports for that URL (checkExistingSize) — a leftover
// truncated/corrupted file from a previous crashed run is deleted and
// re-fetched rather than silently accepted, which is the one piece of
// "verify what's on disk, clean up just the damaged part" health-checking
// that's possible before the part is assembled into a zip (no server-side
// per-part hash exists to check against — see Zip.Check for the full
// CRC/SHA256 verification that happens after assembly). It reports progress
// via progress, which may be nil. pkgIndex/pkgTotal (1-based) are forwarded
// into Progress purely for the UI's "package 3 of 12" display.
func (p *Package) Download(ctx context.Context, client *http.Client, progress ProgressFunc, pkgIndex, pkgTotal int) error {
	if p.Downloaded {
		p.log.Debug("already downloaded")
		return nil
	}

	if err := os.MkdirAll(p.DownloadDir, 0o755); err != nil {
		return err
	}

	// A multi-part package's individual ".zip.NN" pieces are deleted by
	// Zip.Assemble the moment they're successfully merged into p.zip.Path
	// — so if a previous run finished that merge, there is nothing left
	// on disk for the per-URL loop below to find, and it would otherwise
	// re-download every part from scratch despite the finished file
	// already sitting right there (a real case this hit: a multi-GB
	// overlays package redownloaded entirely after the process was
	// restarted, even though its assembled .zip was already complete).
	// isMultiPart detects this: a single-part package's one URL maps
	// directly to p.zip.Path, so the loop's own checkExistingSize already
	// handles that case correctly without this short-circuit.
	isMultiPart := true
	for _, url := range p.RemoteURLs {
		if filepath.Join(p.DownloadDir, filepath.Base(url)) == p.zip.Path {
			isMultiPart = false
			break
		}
	}
	if isMultiPart {
		if fi, err := os.Stat(p.zip.Path); err == nil && fi.Size() > 0 {
			p.log.Info("assembled zip already present, skipping re-download of its (already-consumed) parts", "path", p.zip.Path, "size", fi.Size())
			p.zip.assembled = true
			p.Downloaded = true
			return nil
		}
	}

	for _, url := range p.RemoteURLs {
		filename := filepath.Base(url)
		destPath := filepath.Join(p.DownloadDir, filename)

		if info, err := os.Stat(destPath); err == nil {
			if ok, verr := checkExistingSize(ctx, client, url, info.Size()); verr != nil {
				p.log.Warn("couldn't verify existing file against remote, trusting it as-is", "path", destPath, "err", verr)
			} else if !ok {
				p.log.Warn("existing file doesn't match remote size, treating as damaged and re-fetching", "path", destPath, "local_size", info.Size())
				if rerr := os.Remove(destPath); rerr != nil {
					return rerr
				}
			} else {
				p.log.Info("already exists, size verified, skip", "path", destPath)
			}
		}

		if _, err := os.Stat(destPath); err != nil {
			if progress != nil {
				progress(Progress{Phase: "downloading", Status: fmt.Sprintf("Downloading %s", url), URL: url, PackageName: p.Name, PackageIndex: pkgIndex, PackageTotal: pkgTotal})
			}
			if err := downloadFile(ctx, client, p.log, url, destPath, progress, p.Name, pkgIndex, pkgTotal); err != nil {
				return fmt.Errorf("downloading %s: %w", url, err)
			}
		}

		if len(destPath) > 7 && destPath[len(destPath)-7:] == ".sha256" {
			p.zip.HashFile = destPath
		} else {
			p.zip.Files = append(p.zip.Files, destPath)
		}
	}

	if progress != nil {
		progress(Progress{Phase: "assembling", Status: fmt.Sprintf("Assembling %s", p.Name), PackageName: p.Name, PackageIndex: pkgIndex, PackageTotal: pkgTotal})
	}
	if err := p.zip.Assemble(); err != nil {
		p.Downloaded = false
		return err
	}
	return nil
}

// checkExistingSize reports whether a local file's size matches what the
// server reports via a HEAD request for url. If the server doesn't answer
// HEAD usefully (no Content-Length, non-200, network error), it returns an
// error rather than a verdict — callers should then trust the existing file
// rather than discard good data over an inconclusive check.
func checkExistingSize(ctx context.Context, client *http.Client, url string, localSize int64) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HEAD %s: unexpected status %d", url, resp.StatusCode)
	}
	if resp.ContentLength <= 0 {
		return false, fmt.Errorf("HEAD %s: no content-length reported", url)
	}
	return resp.ContentLength == localSize, nil
}

// downloadFile fetches url into destPath via a staging ".part" file,
// resuming from wherever a previous attempt's ".part" file left off (a
// crash, a kill -9, the process exiting mid-download) rather than
// re-fetching bytes already safely on disk — the same "don't redo what's
// already good" principle as checkExistingSize above, just for a file
// still in progress rather than one already marked complete.
//
// It asks for the remainder with a Range request; if the server doesn't
// honor it (sends 200 instead of 206 — some CDNs/mirrors don't support
// ranges) it falls back to a full restart rather than corrupting the file
// by blindly appending unrelated bytes. If the local ".part" is already
// as large as the server says the whole file is (416 Range Not
// Satisfiable), that can only mean a stale/corrupt leftover — start over.
// Any resume that somehow still produces bad data is caught by Zip.Check's
// CRC/hash verification later and retried from scratch (see Release.Download's
// maxAttempts loop), so this doesn't need to be airtight on its own.
func downloadFile(ctx context.Context, client *http.Client, log *slog.Logger, url, destPath string, progress ProgressFunc, pkgName string, pkgIndex, pkgTotal int) error {
	tmp := destPath + ".part"
	start := time.Now()

	var resumeFrom int64
	if fi, err := os.Stat(tmp); err == nil {
		resumeFrom = fi.Size()
	}

	doRequest := func(rangeFrom int64) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if rangeFrom > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", rangeFrom))
		}
		return client.Do(req)
	}

	if resumeFrom > 0 {
		log.Info("download start", "url", url, "dest", destPath, "resume_from", resumeFrom)
	} else {
		log.Info("download start", "url", url, "dest", destPath)
	}

	resp, err := doRequest(resumeFrom)
	if err != nil {
		log.Error("download failed", "url", url, "err", err)
		return err
	}
	defer resp.Body.Close()

	// The server answered our Range request with "that start is beyond
	// what I have" — our ".part" is stale/corrupt (bigger than the real
	// file). Drop it and restart from scratch.
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		log.Warn("resume offset rejected by server, restarting file from scratch", "url", url, "resume_from", resumeFrom)
		resp.Body.Close()
		if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
			return err
		}
		resumeFrom = 0
		resp, err = doRequest(0)
		if err != nil {
			log.Error("download failed", "url", url, "err", err)
			return err
		}
		defer resp.Body.Close()
	}

	var out *os.File
	var totalSize int64
	switch resp.StatusCode {
	case http.StatusPartialContent:
		// Server honored the Range request — append to what's there.
		out, err = os.OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0o644)
		totalSize = resumeFrom + resp.ContentLength
	case http.StatusOK:
		// Either we didn't ask for a range, or the server ignored it and
		// is sending the whole file from byte 0 — either way, start the
		// staging file fresh so we don't append the full file onto
		// whatever partial bytes were already there.
		if resumeFrom > 0 {
			log.Warn("server ignored resume request, restarting file from scratch", "url", url)
		}
		resumeFrom = 0
		out, err = os.Create(tmp)
		totalSize = resp.ContentLength
	default:
		log.Error("download failed", "url", url, "status", resp.StatusCode)
		return fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}
	if err != nil {
		return err
	}

	fetched := resumeFrom
	lastHeartbeat := start
	cw := &countingWriter{w: out, onWrite: func(n int) {
		fetched += int64(n)
		elapsed := time.Since(start).Seconds()
		var pcnt, mbps float64
		if totalSize > 0 {
			pcnt = float64(fetched) / float64(totalSize) * 100
		}
		if elapsed > 0 {
			mbps = (float64(fetched-resumeFrom) / 1048576) / elapsed
		}
		if time.Since(lastHeartbeat) >= 10*time.Second {
			log.Info("download progress", "url", url, "percent", pcnt, "mbps", mbps, "bytes", fetched)
			lastHeartbeat = time.Now()
		}
		if progress == nil {
			return
		}
		progress(Progress{
			Phase: "downloading", Status: fmt.Sprintf("Downloading %s", url), URL: url,
			PackageName: pkgName, PackageIndex: pkgIndex, PackageTotal: pkgTotal,
			PercentDone: pcnt, MBps: mbps,
		})
	}}

	_, copyErr := io.Copy(cw, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		if ctx.Err() != nil {
			// Deliberately NOT removing tmp here: a context cancellation
			// (reload/shutdown, not a real failure) should leave the
			// partial file in place so the next attempt can resume from
			// it instead of starting over.
			log.Warn("download cancelled, keeping partial file for resume", "url", url, "err", ctx.Err(), "bytes_so_far", fetched)
		} else {
			log.Error("download failed", "url", url, "err", copyErr)
			os.Remove(tmp)
		}
		return copyErr
	}
	if closeErr != nil {
		log.Error("download failed closing file", "url", url, "err", closeErr)
		os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, destPath); err != nil {
		return err
	}
	log.Info("download done", "url", url, "bytes", fetched, "elapsed_s", time.Since(start).Seconds())
	return nil
}

type countingWriter struct {
	w       io.Writer
	onWrite func(n int)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
	return n, err
}

// Check validates the downloaded zip; on failure it cleans up so a retry
// starts fresh, mirroring downloader.py's Package.check().
func (p *Package) Check(progress ProgressFunc, pkgIndex, pkgTotal int) (bool, error) {
	p.log.Info("verify start", "package", p.Name)
	if progress != nil {
		progress(Progress{Phase: "verifying", Status: fmt.Sprintf("Verifying %s", p.Name), PackageName: p.Name, PackageIndex: pkgIndex, PackageTotal: pkgTotal})
	}
	ok, err := p.zip.Check()
	if err != nil {
		p.log.Error("verify failed", "package", p.Name, "err", err)
		return false, err
	}
	if !ok {
		p.log.Warn("failed validation, cleaning up for retry", "package", p.Name)
		if cerr := p.Cleanup(); cerr != nil {
			return false, cerr
		}
		p.Downloaded = false
		return false, nil
	}
	p.log.Info("verify done", "package", p.Name)
	return ok, nil
}

// Install extracts the package and flattens the single top-level directory
// the zip is expected to contain, matching the Python version's
// "z_*"/"y_*/yOrtho4XP_Overlays" -> InstallDir copy-then-remove dance.
func (p *Package) Install(progress ProgressFunc, pkgIndex, pkgTotal int) error {
	if p.Installed {
		return nil
	}

	if progress != nil {
		progress(Progress{Phase: "extracting", Status: fmt.Sprintf("Extracting %s", p.Name), PackageName: p.Name, PackageIndex: pkgIndex, PackageTotal: pkgTotal})
	}
	p.log.Info("extract start", "package", p.Name, "dir", p.InstallDir)

	// Throttled so a package with thousands of small tiles doesn't flood
	// progress/the WS hub with one update per file.
	lastReport := time.Time{}
	onEntry := func(name string, idx, total int) {
		if progress == nil {
			return
		}
		if idx < total && time.Since(lastReport) < 200*time.Millisecond {
			return
		}
		lastReport = time.Now()
		var pcnt float64
		if total > 0 {
			pcnt = float64(idx) / float64(total) * 100
		}
		progress(Progress{
			Phase: "extracting", Status: fmt.Sprintf("Extracting %s (%d/%d)", p.Name, idx, total),
			PackageName: p.Name, PackageIndex: pkgIndex, PackageTotal: pkgTotal, PercentDone: pcnt,
		})
	}

	if err := p.zip.Extract(p.InstallDir, onEntry); err != nil {
		p.log.Error("extract failed", "package", p.Name, "err", err)
		return err
	}
	p.log.Info("extract done", "package", p.Name)

	var innerDirs []string
	switch p.PkgType {
	case "z":
		innerDirs = []string{filepath.Join(p.InstallDir, p.Name)}
	case "y":
		matches, err := filepath.Glob(filepath.Join(p.InstallDir, "y_*", "yOrtho4XP_Overlays"))
		if err != nil {
			return err
		}
		innerDirs = matches
	}

	for _, dir := range innerDirs {
		if _, err := os.Stat(dir); err != nil {
			continue // nothing to flatten; extraction already landed correctly
		}
		if progress != nil {
			progress(Progress{Phase: "installing", Status: fmt.Sprintf("Installing %s", p.Name), PackageName: p.Name, PackageIndex: pkgIndex, PackageTotal: pkgTotal})
		}
		p.log.Info("flattening", "package", p.Name, "from", dir, "to", p.InstallDir)
		if err := copyTree(dir, p.InstallDir); err != nil {
			p.log.Error("flatten failed", "package", p.Name, "err", err)
			return err
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}

	p.Installed = true
	return nil
}

func (p *Package) Uninstall() error {
	if _, err := os.Stat(p.InstallDir); err == nil {
		p.log.Info("uninstalling", "dir", p.InstallDir)
		if err := os.RemoveAll(p.InstallDir); err != nil {
			return err
		}
	}
	p.Installed = false
	return nil
}

func (p *Package) Cleanup() error {
	return p.zip.Clean()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := d.Info()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
