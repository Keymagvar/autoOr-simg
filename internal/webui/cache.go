package webui

import (
	"os"
	"path/filepath"
	"sort"
)

// cleanCache is the Go port of config_ui.py's ConfigUI.clean_cache(): walk
// cacheDir, remove every zero-byte file (a leftover from an interrupted
// build), then — if still over the target size — remove whole files
// oldest-first until under targetGB (floored at 10, same as the Python
// original). Returns how many files were removed and how many bytes were
// freed.
func cleanCache(cacheDir string, targetGB int) (filesRemoved int, bytesFreed int64, err error) {
	if targetGB < 10 {
		targetGB = 10 // same floor as the Python original
	}
	return cleanCacheToBytes(cacheDir, int64(targetGB)*(1<<30))
}

// cleanCacheToBytes is cleanCache's actual implementation, taking a raw
// byte target instead of a GB count floored at 10 — split out so tests can
// exercise realistic small-scale eviction without needing multi-GB
// fixtures on disk.
func cleanCacheToBytes(cacheDir string, targetBytes int64) (filesRemoved int, bytesFreed int64, err error) {
	type fileInfo struct {
		path    string
		size    int64
		modTime int64
	}
	var files []fileInfo

	walkErr := filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		files = append(files, fileInfo{path: path, size: info.Size(), modTime: info.ModTime().UnixNano()})
		return nil
	})
	if walkErr != nil {
		return 0, 0, walkErr
	}
	if len(files) == 0 {
		return 0, 0, nil
	}

	var totalBytes int64
	var kept []fileInfo
	for _, f := range files {
		if f.size == 0 {
			if rmErr := os.Remove(f.path); rmErr == nil {
				filesRemoved++
			}
			continue
		}
		totalBytes += f.size
		kept = append(kept, f)
	}

	if totalBytes <= targetBytes || len(kept) == 0 {
		return filesRemoved, bytesFreed, nil
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].modTime < kept[j].modTime })

	avgSize := totalBytes / int64(len(kept))
	if avgSize == 0 {
		avgSize = 1
	}
	toDelete := int((totalBytes - targetBytes) / avgSize)
	if toDelete > len(kept) {
		toDelete = len(kept)
	}

	for _, f := range kept[:toDelete] {
		if rmErr := os.Remove(f.path); rmErr == nil {
			filesRemoved++
			bytesFreed += f.size
		}
	}
	return filesRemoved, bytesFreed, nil
}
