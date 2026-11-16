package tile

import "testing"

// TestNewTile_MaxZoomCapsZoomAndShiftsRowCol is the regression test for the
// "max zoom" cap (a user-requested feature not in the Python original,
// trading texture sharpness for real disk/bandwidth savings): when the
// requested zoom exceeds maxZoom, both zoom AND row/col must shift
// together. Slippy-tile row/col numbering scales exactly 2x per zoom
// level, so shifting zoom alone (without also right-shifting row/col by
// the same amount) would silently point at the wrong geographic tile —
// not a theoretical risk, exactly the kind of "looks fine until you
// actually look at the output" bug this project has hit before with zoom
// math (a prior min_zoom reconciliation bug).
func TestNewTile_MaxZoomCapsZoomAndShiftsRowCol(t *testing.T) {
	// Native request: zoom 18, row/col chosen so the shift is easy to
	// verify by hand (row=800 col=400, shift by 3 for an 18->15 cap).
	tile, ok := NewTile(800, 400, 18, "EOX", 0 /* minZoom */, 15 /* maxZoom */, t.TempDir(), nil, 0)
	if !ok {
		t.Fatal("NewTile returned ok=false")
	}
	if tile.Zoom != 15 {
		t.Fatalf("Zoom = %d, want capped to 15", tile.Zoom)
	}
	wantRow, wantCol := 800>>3, 400>>3
	if tile.Row != wantRow || tile.Col != wantCol {
		t.Fatalf("Row/Col = %d/%d, want %d/%d (shifted by the same 3 levels as zoom)", tile.Row, tile.Col, wantRow, wantCol)
	}
}

// TestNewTile_MaxZoomZeroMeansNoCap proves the default (disabled) behaves
// identically to before this feature existed — no existing install's
// behavior should change just because the field now exists in the config
// struct.
func TestNewTile_MaxZoomZeroMeansNoCap(t *testing.T) {
	tile, ok := NewTile(800, 400, 18, "EOX", 0, 0 /* maxZoom disabled */, t.TempDir(), nil, 0)
	if !ok {
		t.Fatal("NewTile returned ok=false")
	}
	if tile.Zoom != 18 || tile.Row != 800 || tile.Col != 400 {
		t.Fatalf("Row/Col/Zoom = %d/%d/%d, want unchanged 800/400/18 when maxZoom=0", tile.Row, tile.Col, tile.Zoom)
	}
}

// TestNewTile_MaxZoomAboveNativeZoomIsNoop proves a maxZoom set higher
// than (or equal to) what's actually requested never raises quality above
// what was asked for — it's a ceiling, never a floor pushing the other
// direction, matching MinZoom's own "never raises quality" framing in
// DESIGN.md §3.
func TestNewTile_MaxZoomAboveNativeZoomIsNoop(t *testing.T) {
	tile, ok := NewTile(800, 400, 15, "EOX", 0, 18 /* maxZoom higher than the 15 actually requested */, t.TempDir(), nil, 0)
	if !ok {
		t.Fatal("NewTile returned ok=false")
	}
	if tile.Zoom != 15 || tile.Row != 800 || tile.Col != 400 {
		t.Fatalf("Row/Col/Zoom = %d/%d/%d, want unchanged 800/400/15 when maxZoom(18) > requested zoom(15)", tile.Row, tile.Col, tile.Zoom)
	}
}
