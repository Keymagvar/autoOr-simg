package scenery

import "testing"

func TestFreeBytes(t *testing.T) {
	free, ok, err := FreeBytes(t.TempDir())
	if err != nil {
		t.Fatalf("FreeBytes error: %v", err)
	}
	if !ok {
		t.Skip("free-space check not implemented on this platform (DESIGN.md §11 TODO)")
	}
	if free == 0 {
		t.Error("expected nonzero free space on a real filesystem")
	}
}
