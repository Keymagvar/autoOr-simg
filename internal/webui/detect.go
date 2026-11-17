package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// xplaneCandidate is one detected install offered by the "Find X-Plane…"
// button next to the Setup block's X-Plane path field.
type xplaneCandidate struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// handleAPIDetectXPlane scans the places a real X-Plane install actually
// lives — Steam's default library, every additional Steam library folder
// (read from libraryfolders.vdf, since a large install commonly lives on a
// separate drive the default library doesn't cover), and a handful of
// common manual-install locations — and returns every directory found that
// genuinely looks like an X-Plane install (see looksLikeXPlaneDir), not
// just anything named "X-Plane *". This is a convenience, best-effort scan,
// same loopback-only trust model as /api/browse (§10): a user with an
// install somewhere this doesn't look still has the "Browse…" button.
func (s *Server) handleAPIDetectXPlane(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeBrowseError(w, http.StatusInternalServerError, "could not determine home directory: "+err.Error())
		return
	}

	roots := []string{
		// Linux
		filepath.Join(home, ".local", "share", "Steam", "steamapps", "common"),
		filepath.Join(home, ".steam", "steam", "steamapps", "common"),
		filepath.Join(home, ".steam", "root", "steamapps", "common"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam", "steamapps", "common"),
		// macOS
		filepath.Join(home, "Library", "Application Support", "Steam", "steamapps", "common"),
	}
	// Windows — not home-relative at all (Steam's own default install
	// location, independent of which user is logged in). Guarded on the
	// env var actually being set, unlike the paths above: filepath.Join
	// silently drops an empty first element rather than erroring, so an
	// unguarded join here would resolve to a bare relative "Steam/..."  —
	// which os.ReadDir would then happily check against the *working*
	// directory on a platform where this variable doesn't exist, a wrong
	// answer instead of the clean "doesn't exist, skip it" every other
	// root gets.
	for _, envVar := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if dir := os.Getenv(envVar); dir != "" {
			roots = append(roots, filepath.Join(dir, "Steam", "steamapps", "common"))
		}
	}
	roots = append(roots, steamLibraryCommonDirs(home)...)

	seen := map[string]bool{}
	var candidates []xplaneCandidate
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "X-Plane") {
				continue
			}
			path := filepath.Join(root, e.Name())
			if seen[path] || !looksLikeXPlaneDir(path) {
				continue
			}
			seen[path] = true
			candidates = append(candidates, xplaneCandidate{Path: path, Label: e.Name()})
		}
	}

	// A handful of common non-Steam, manual-install locations too.
	manualDirs := []string{
		filepath.Join(home, "X-Plane 12"),
		filepath.Join(home, "X-Plane 11"),
		filepath.Join(home, "Games", "X-Plane 12"),
		filepath.Join(home, "Games", "X-Plane 11"),
		"/opt/X-Plane 12",
		"/opt/X-Plane 11",
	}
	for _, envVar := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if dir := os.Getenv(envVar); dir != "" {
			manualDirs = append(manualDirs,
				filepath.Join(dir, "X-Plane 12"),
				filepath.Join(dir, "X-Plane 11"))
		}
	}
	for _, manual := range manualDirs {
		if seen[manual] {
			continue
		}
		if looksLikeXPlaneDir(manual) {
			seen[manual] = true
			candidates = append(candidates, xplaneCandidate{Path: manual, Label: filepath.Base(manual)})
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	if candidates == nil {
		candidates = []xplaneCandidate{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"candidates": candidates})
}

// looksLikeXPlaneDir checks for the two things that actually distinguish a
// real X-Plane install from an arbitrarily-named or empty folder: its own
// Custom Scenery directory (required — this is exactly what AutoOrtho
// itself reads/writes under, via config.XplaneCustomSceneryPath) and
// either a platform launcher binary or the Resources folder every real
// install ships (on every platform, including inside X-Plane.app on
// macOS, which is itself a directory), so a renamed-but-empty folder
// doesn't false-positive.
func looksLikeXPlaneDir(path string) bool {
	if info, err := os.Stat(filepath.Join(path, "Custom Scenery")); err != nil || !info.IsDir() {
		return false
	}
	for _, launcher := range []string{"X-Plane-x86_64", "X-Plane.exe", "X-Plane.app"} {
		if _, err := os.Stat(filepath.Join(path, launcher)); err == nil {
			return true
		}
	}
	if info, err := os.Stat(filepath.Join(path, "Resources")); err == nil && info.IsDir() {
		return true
	}
	return false
}

var steamVDFPathRe = regexp.MustCompile(`"path"\s*"([^"]+)"`)

// steamLibraryCommonDirs does a best-effort read of Steam's own
// libraryfolders.vdf (checked at every known default Steam location, not
// just one) for additional library paths beyond the default steamapps dir.
// Not a full VDF parser — just the one key this needs — so a future Steam
// VDF format change degrades to "finds nothing extra", not a crash.
func steamLibraryCommonDirs(home string) []string {
	vdfPaths := []string{
		// Linux
		filepath.Join(home, ".local", "share", "Steam", "steamapps", "libraryfolders.vdf"),
		filepath.Join(home, ".steam", "steam", "steamapps", "libraryfolders.vdf"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam", "steamapps", "libraryfolders.vdf"),
		// macOS
		filepath.Join(home, "Library", "Application Support", "Steam", "steamapps", "libraryfolders.vdf"),
	}
	for _, envVar := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if dir := os.Getenv(envVar); dir != "" {
			vdfPaths = append(vdfPaths, filepath.Join(dir, "Steam", "steamapps", "libraryfolders.vdf"))
		}
	}

	var dirs []string
	for _, vdf := range vdfPaths {
		data, err := os.ReadFile(vdf)
		if err != nil {
			continue
		}
		for _, m := range steamVDFPathRe.FindAllStringSubmatch(string(data), -1) {
			dirs = append(dirs, filepath.Join(m[1], "steamapps", "common"))
		}
	}
	return dirs
}
