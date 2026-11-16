package scenery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSceneryPackRegistered_CreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()

	if err := EnsureSceneryPackRegistered(dir, "z_ao_eur"); err != nil {
		t.Fatalf("EnsureSceneryPackRegistered: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "scenery_packs.ini"))
	if err != nil {
		t.Fatalf("reading created ini: %v", err)
	}
	if !strings.Contains(string(got), "SCENERY_PACK Custom Scenery/z_ao_eur/") {
		t.Errorf("created ini missing expected line, got:\n%s", got)
	}
}

func TestEnsureSceneryPackRegistered_AppendsWhenNoGlobalSceneryMarker(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "scenery_packs.ini")
	initial := "I\n1000 Version\nSCENERY\n\nSCENERY_PACK Custom Scenery/KSEA Demo Area/\n"
	if err := os.WriteFile(iniPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSceneryPackRegistered(dir, "z_ao_eur"); err != nil {
		t.Fatalf("EnsureSceneryPackRegistered: %v", err)
	}

	got, err := os.ReadFile(iniPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if lines[len(lines)-1] != "SCENERY_PACK Custom Scenery/z_ao_eur/" {
		t.Errorf("expected new line appended at the end, got:\n%s", got)
	}
}

func TestEnsureSceneryPackRegistered_InsertsBeforeGlobalScenery(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "scenery_packs.ini")
	initial := "I\n1000 Version\nSCENERY\n\n" +
		"SCENERY_PACK Custom Scenery/KSEA Demo Area/\n" +
		"SCENERY_PACK Global Scenery/X-Plane 12 Global Scenery/\n"
	if err := os.WriteFile(iniPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSceneryPackRegistered(dir, "z_ao_eur"); err != nil {
		t.Fatalf("EnsureSceneryPackRegistered: %v", err)
	}

	got, err := os.ReadFile(iniPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	idxNew, idxGlobal := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "z_ao_eur") {
			idxNew = i
		}
		if strings.Contains(l, "Global Scenery") {
			idxGlobal = i
		}
	}
	if idxNew == -1 || idxGlobal == -1 || idxNew >= idxGlobal {
		t.Errorf("expected new line before the Global Scenery entry, got:\n%s", got)
	}
}

func TestEnsureSceneryPackRegistered_IdempotentWhenAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "scenery_packs.ini")
	initial := "I\n1000 Version\nSCENERY\n\nSCENERY_PACK Custom Scenery/z_ao_eur/\n"
	if err := os.WriteFile(iniPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSceneryPackRegistered(dir, "z_ao_eur"); err != nil {
		t.Fatalf("EnsureSceneryPackRegistered: %v", err)
	}

	got, err := os.ReadFile(iniPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != initial {
		t.Errorf("expected file unchanged, got:\n%s", got)
	}
}

func TestEnsureSceneryPackRegistered_IgnoresAlreadyDisabledEntry(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "scenery_packs.ini")
	initial := "I\n1000 Version\nSCENERY\n\nSCENERY_PACK_DISABLED Custom Scenery/z_ao_eur/\n"
	if err := os.WriteFile(iniPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSceneryPackRegistered(dir, "z_ao_eur"); err != nil {
		t.Fatalf("EnsureSceneryPackRegistered: %v", err)
	}

	got, err := os.ReadFile(iniPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != initial {
		t.Errorf("expected a disabled entry to count as already registered, got:\n%s", got)
	}
}
