package scenery

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Zip is a downloaded archive that may be assembled from one or more parts
// before it's a valid zip file, optionally verified against a sibling
// .sha256 hash file. Mirrors downloader.py's Zip class.
type Zip struct {
	Path      string
	Files     []string
	HashFile  string
	assembled bool

	log *slog.Logger
}

var hashLineRe = regexp.MustCompile(`^([0-9A-Fa-f]+)\s+(\S.*)$`)

// Check validates the zip: by hash if HashFile is set, otherwise by
// actually reading every entry (Go's archive/zip verifies CRC32 on read,
// which is the equivalent of Python's zf.testzip()).
func (z *Zip) Check() (bool, error) {
	z.log.Debug("check start", "path", z.Path, "by_hash", z.HashFile != "")
	if _, err := os.Stat(z.Path); err != nil {
		z.log.Debug("check: file missing", "path", z.Path)
		return false, nil
	}

	if z.HashFile != "" {
		f, err := os.Open(z.Path)
		if err != nil {
			return false, err
		}
		defer f.Close()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return false, err
		}
		sum := hex.EncodeToString(h.Sum(nil))

		data, err := os.ReadFile(z.HashFile)
		if err != nil {
			return false, err
		}
		m := hashLineRe.FindStringSubmatch(strings.TrimSpace(string(data)))
		if m == nil || !strings.EqualFold(m[1], sum) {
			z.log.Warn("check: hash mismatch", "path", z.Path)
			return false, nil
		}
		return true, nil
	}

	r, err := zip.OpenReader(z.Path)
	if err != nil {
		z.log.Warn("check: can't open zip", "path", z.Path, "err", err)
		return false, nil
	}
	defer r.Close()

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			z.log.Warn("check: can't open entry", "path", z.Path, "entry", f.Name, "err", err)
			return false, nil
		}
		_, err = io.Copy(io.Discard, rc)
		rc.Close()
		if err != nil {
			// CRC mismatch or truncated entry surfaces here.
			z.log.Warn("check: CRC/read failure", "path", z.Path, "entry", f.Name, "err", err)
			return false, nil
		}
	}
	return true, nil
}

// Assemble concatenates multi-part downloads into Path, in sorted filename
// order, removing the parts as it goes. No-op if any Files entry already
// ends in ".zip" (nothing to assemble) or if already assembled.
func (z *Zip) Assemble() error {
	if z.assembled {
		return nil
	}
	for _, f := range z.Files {
		if strings.HasSuffix(f, ".zip") {
			return nil
		}
	}

	z.log.Info("assemble start", "path", z.Path, "parts", len(z.Files))
	files := append([]string(nil), z.Files...)
	sort.Strings(files)

	out, err := os.Create(z.Path)
	if err != nil {
		return err
	}
	defer out.Close()

	for _, f := range files {
		in, err := os.Open(f)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		if copyErr != nil {
			z.log.Error("assemble failed", "path", z.Path, "part", f, "err", copyErr)
			return copyErr
		}
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	z.assembled = true
	z.log.Info("assemble done", "path", z.Path)
	return nil
}

// ExtractEntryFunc is called after each non-directory entry is written
// during Extract, with a 1-based index among the zip's file (non-dir)
// entries — so a caller can show "extracting file 1234 of 5000" instead of
// nothing at all for however long a large package's unzip takes. May be
// nil.
type ExtractEntryFunc func(name string, index, total int)

// Extract unpacks Path into dest, guarding against zip-slip path traversal
// (not a concern the Python version checks for, but cheap to do right).
func (z *Zip) Extract(dest string, onEntry ExtractEntryFunc) error {
	z.log.Info("extract start", "path", z.Path, "dest", dest)
	r, err := zip.OpenReader(z.Path)
	if err != nil {
		z.log.Error("extract: can't open zip", "path", z.Path, "err", err)
		return err
	}
	defer r.Close()

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	totalFiles := 0
	for _, f := range r.File {
		if !f.FileInfo().IsDir() {
			totalFiles++
		}
	}

	fileIndex := 0
	for _, f := range r.File {
		target := filepath.Join(destAbs, f.Name)
		if !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) && target != destAbs {
			return fmt.Errorf("zip entry %q escapes destination %q", f.Name, dest)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			z.log.Error("extract failed", "path", z.Path, "entry", f.Name, "err", copyErr)
			return copyErr
		}

		fileIndex++
		if onEntry != nil {
			onEntry(f.Name, fileIndex, totalFiles)
		}
	}
	z.log.Info("extract done", "path", z.Path, "dest", dest, "files", totalFiles)
	return nil
}

// Clean removes the assembled zip, any leftover part files, and the hash
// file, then resets state so this Zip can be reused for a fresh download.
func (z *Zip) Clean() error {
	for _, p := range append([]string{z.Path, z.HashFile}, z.Files...) {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	z.Files = nil
	z.assembled = false
	return nil
}
