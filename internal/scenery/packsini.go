package scenery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sceneryPackLine builds the exact SCENERY_PACK line X-Plane expects for a
// package directory name directly under Custom Scenery — always
// forward-slash-separated and trailing-slash-terminated, regardless of
// host OS, since this is X-Plane's own file format, not a filesystem path.
func sceneryPackLine(packDirName string) string {
	return "SCENERY_PACK Custom Scenery/" + packDirName + "/"
}

// EnsureSceneryPackRegistered idempotently makes sure X-Plane's
// scenery_packs.ini (at <customSceneryPath>/scenery_packs.ini) lists
// packDirName (e.g. "z_ao_eur") as an active scenery pack, adding one line
// if it's missing. This is HANDOFF_NEXT_SESSION.md's documented gap: per
// its own notes, without this, a freshly installed region sits there fully
// downloaded and mountable but invisible to X-Plane until a user manually
// edits this file — easy to forget, and easy to get the line format or
// position wrong by hand.
//
// The new line is inserted directly above the first existing
// "Global Scenery"-named entry (X-Plane's own default/low-res base
// scenery, when it's listed at all) so ortho imagery still overrides that,
// but below every other already-listed pack — payware airports/landmark
// packages keep their own ground textures and exclusion zones taking
// priority over the raw satellite imagery underneath, exactly as
// HANDOFF_NEXT_SESSION.md's manual instructions describe. If no such
// marker line exists, the new line is simply appended at the end. If the
// file doesn't exist yet at all (a X-Plane install that has literally
// never been launched), a minimal valid one is created from scratch.
//
// Already-registered (a line for this exact packDirName already present,
// active or disabled) is a no-op — safe to call on every install, not just
// the first.
func EnsureSceneryPackRegistered(customSceneryPath, packDirName string) error {
	iniPath := filepath.Join(customSceneryPath, "scenery_packs.ini")
	marker := "Custom Scenery/" + packDirName + "/"

	existing, err := os.ReadFile(iniPath)
	var lines []string
	if err == nil {
		lines = strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	} else if os.IsNotExist(err) {
		lines = []string{"I", "1000 Version", "SCENERY", ""}
	} else {
		return fmt.Errorf("reading %s: %w", iniPath, err)
	}

	for _, line := range lines {
		if strings.Contains(line, marker) {
			return nil // already registered, active or disabled — nothing to do
		}
	}

	newLine := sceneryPackLine(packDirName)
	insertAt := len(lines)
	for i, line := range lines {
		if strings.Contains(line, "Global Scenery") {
			insertAt = i
			break
		}
	}
	lines = append(lines[:insertAt], append([]string{newLine}, lines[insertAt:]...)...)

	out := strings.Join(lines, "\n") + "\n"
	tmp := iniPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, iniPath); err != nil {
		return fmt.Errorf("replacing %s: %w", iniPath, err)
	}
	return nil
}
