package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dirEntry is one subdirectory offered to the "Browse" dialog next to a
// Setup-block path field.
type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// browseResponse is what GET /api/browse returns: the directory actually
// listed (after resolving/validating the requested one), its parent (empty
// at a filesystem root, so the client knows to disable "Up"), and every
// subdirectory inside it. Only directories are listed — this dialog picks
// a folder, never a file.
type browseResponse struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent,omitempty"`
	Entries []dirEntry `json:"entries"`
}

// handleAPIBrowse backs the directory-picker "Browse" buttons next to the
// Setup block's path fields (X-Plane/scenery/cache/download dirs). This
// only makes sense because the web UI is loopback-bound by design (§10):
// the server and the browser hitting it are the same machine, so listing
// server-side directories is listing the same filesystem the user would
// otherwise be typing a path into blind. Deliberately does NOT hide
// dotfile directories — real X-Plane installs commonly live under one
// (e.g. `~/.local/share/Steam/...`), so filtering them out would make the
// picker useless for the most common real path.
func (s *Server) handleAPIBrowse(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeBrowseError(w, http.StatusInternalServerError, "could not determine a starting directory: "+err.Error())
			return
		}
		reqPath = home
	}

	reqPath = filepath.Clean(reqPath)
	info, err := os.Stat(reqPath)
	if err != nil {
		writeBrowseError(w, http.StatusBadRequest, "can't read "+reqPath+": "+err.Error())
		return
	}
	if !info.IsDir() {
		reqPath = filepath.Dir(reqPath)
	}

	entries, err := os.ReadDir(reqPath)
	if err != nil {
		writeBrowseError(w, http.StatusBadRequest, "can't list "+reqPath+": "+err.Error())
		return
	}

	resp := browseResponse{Path: reqPath, Entries: []dirEntry{}}
	if parent := filepath.Dir(reqPath); parent != reqPath {
		resp.Parent = parent
	}
	for _, e := range entries {
		// Resolve symlinks-to-directories too (e.g. a symlinked Steam
		// library folder) — Entry.IsDir() alone misses those.
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if target, err := os.Stat(filepath.Join(reqPath, e.Name())); err == nil {
				isDir = target.IsDir()
			}
		}
		if !isDir {
			continue
		}
		resp.Entries = append(resp.Entries, dirEntry{
			Name: e.Name(),
			Path: filepath.Join(reqPath, e.Name()),
		})
	}
	sort.Slice(resp.Entries, func(i, j int) bool {
		return strings.ToLower(resp.Entries[i].Name) < strings.ToLower(resp.Entries[j].Name)
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAPIBrowseNative backs the Browse buttons' preferred path: a real OS
// folder-picker dialog (zenity/kdialog, the .NET FolderBrowserDialog, or
// AppleScript's "choose folder" — see native_picker.go), spawned
// server-side and shown on the same screen the browser tab is already on
// (loopback-only, same machine, see handleAPIBrowse's doc comment). The
// client falls back to the plain in-page browser (GET /api/browse) when
// this comes back 501 — e.g. a minimal Linux install with neither zenity
// nor kdialog.
func (s *Server) handleAPIBrowseNative(w http.ResponseWriter, r *http.Request) {
	startDir := r.URL.Query().Get("path")
	if startDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeBrowseError(w, http.StatusInternalServerError, "could not determine a starting directory: "+err.Error())
			return
		}
		startDir = home
	}

	path, err := pickFolderNative(startDir)
	if err == errPickerUnavailable {
		writeBrowseError(w, http.StatusNotImplemented, err.Error())
		return
	}
	if err != nil {
		writeBrowseError(w, http.StatusInternalServerError, "folder picker failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
}

func writeBrowseError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
