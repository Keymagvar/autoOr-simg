package tile

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"autoOr-simg/internal/native"
)

// TestCropScaleUp_BilinearNotBlocky is the regression test for replacing
// cropScaleUp's original nearest-neighbor upscale with real bilinear
// interpolation. A 2x1 source (one black pixel, one white pixel) scaled up
// 8x should produce a smooth gradient across the row — nearest-neighbor
// would instead produce exactly two flat blocks of 8 identical pixels each,
// with one abrupt jump in the middle and no intermediate values anywhere.
// This test fails under the old nearest-neighbor implementation and passes
// under bilinear.
func TestCropScaleUp_BilinearNotBlocky(t *testing.T) {
	src := &native.RGBAImage{
		Width: 2, Height: 1,
		Pix: []byte{
			0, 0, 0, 255, // black
			255, 255, 255, 255, // white
		},
	}

	const scale = 8
	out := cropScaleUp(src, 0, 0, 2, 1, scale)

	if out.Width != 2*scale || out.Height != scale {
		t.Fatalf("output size = %dx%d, want %dx%d", out.Width, out.Height, 2*scale, scale)
	}

	// Collect the red channel (same as every other channel here, since
	// source is grayscale) across the single output row.
	vals := make([]int, out.Width)
	for ox := 0; ox < out.Width; ox++ {
		vals[ox] = int(out.Pix[ox*4])
	}

	// 1. Monotonically non-decreasing left to right (bilinear between a
	// darker-to-lighter source should never get darker moving rightward).
	for i := 1; i < len(vals); i++ {
		if vals[i] < vals[i-1] {
			t.Errorf("row not monotonic at %d: %v", i, vals)
			break
		}
	}

	// 2. The defining difference from nearest-neighbor: at least one
	// distinct intermediate value strictly between the two source
	// extremes must appear. Nearest-neighbor produces only {0, 255}.
	sawIntermediate := false
	for _, v := range vals {
		if v > 0 && v < 255 {
			sawIntermediate = true
			break
		}
	}
	if !sawIntermediate {
		t.Errorf("no intermediate value found in %v — looks like nearest-neighbor (blocky), not bilinear", vals)
	}

	// 3. Still anchored correctly: the leftmost/rightmost output pixels
	// should sit close to their respective source pixel, not swapped or
	// wildly off (sanity check against a reversed or mis-indexed lookup).
	if vals[0] > 64 {
		t.Errorf("leftmost pixel = %d, want close to source black (0)", vals[0])
	}
	if vals[len(vals)-1] < 191 {
		t.Errorf("rightmost pixel = %d, want close to source white (255)", vals[len(vals)-1])
	}
}

// TestCropScaleUp_PreservesSolidColor confirms a uniform source region
// upscales to an exactly uniform result — bilinear interpolation between
// identical neighbors must not introduce any drift or noise.
func TestCropScaleUp_PreservesSolidColor(t *testing.T) {
	src := &native.RGBAImage{
		Width: 4, Height: 4,
		Pix: make([]byte, 4*4*4),
	}
	for i := range src.Pix {
		if i%4 == 3 {
			src.Pix[i] = 255 // alpha
		} else {
			src.Pix[i] = 100
		}
	}

	out := cropScaleUp(src, 0, 0, 4, 4, 4)
	for i, b := range out.Pix {
		want := byte(100)
		if i%4 == 3 {
			want = 255
		}
		if b != want {
			t.Fatalf("pixel byte %d = %d, want %d (uniform source should stay uniform)", i, b, want)
		}
	}
}

// TestFetchComposeCompressed_MatchesOldWholeCompositeApproach is the
// correctness backbone for replacing fetchComposite+one-big-CompressBC1
// with per-chunk compress-and-stitch (fetchComposeCompressed) for mipmap
// 0 — see its own doc comment for why this exists (tonight's Moscow/VKO
// pprof findings showed newRGBA's 64MB-per-tile composite buffers
// accounting for ~1.1-1.3GB in every heap snapshot under heavy load).
//
// The optimization is only worth it if it's actually byte-identical to
// the old approach, not just "close enough" — a subtle bug here would
// show up as visibly corrupted ground textures in X-Plane, a worse
// outcome than the memory problem it's fixing. This relies on BC1/DXT1
// being a genuinely block-local format (stitchBlocks' doc comment), which
// this test verifies empirically rather than just asserting by argument:
// it builds the exact same grid both ways and diffs the resulting bytes.
func TestFetchComposeCompressed_MatchesOldWholeCompositeApproach(t *testing.T) {
	jpegData, err := os.ReadFile("testdata/chunk.jpg")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer srv.Close()

	newTile := func() *Tile {
		return &Tile{
			Row: 0, Col: 0, Zoom: 10,
			Maptype:  "TEST",
			MinZoom:  10,
			CacheDir: t.TempDir(),
			Provider: testProviderFor(srv.URL),
			Client:   &http.Client{},
			MaxWait:  defaultMaxWait,
			DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
			log:      noopLogger{},
		}
	}

	const w, h, zoom = 3, 2, 10 // non-square, multi-chunk grid -- exercises real stitching, not a 1x1 edge case

	oldWay := newTile()
	img, degradedOld := oldWay.fetchComposite(context.Background(), 0, 0, w, h, zoom)
	wantData, err := oldWay.DDS.Compressor.CompressBC1(img.Pix, img.Width, img.Height)
	if err != nil {
		t.Fatalf("old-way compress: %v", err)
	}

	newWay := newTile()
	gotData, degradedNew, _ := newWay.fetchComposeCompressed(context.Background(), 0, 0, w, h, zoom)

	if degradedOld != degradedNew {
		t.Errorf("degraded flag mismatch: old=%v new=%v", degradedOld, degradedNew)
	}
	if len(gotData) != len(wantData) {
		t.Fatalf("compressed output length = %d, want %d", len(gotData), len(wantData))
	}
	if !bytes.Equal(gotData, wantData) {
		diffAt := -1
		for i := range wantData {
			if gotData[i] != wantData[i] {
				diffAt = i
				break
			}
		}
		t.Errorf("fetchComposeCompressed's output differs from the old whole-composite-then-compress approach at byte %d (of %d) — the two paths must be byte-identical", diffAt, len(wantData))
	}
}

// TestFetchComposeCompressed_DegradedMatchesOldApproach is the same
// correctness check as above, but specifically for the all-chunks-missing
// fallback path (flat fallback color, compressed) -- a separate code path
// in fetchComposeCompressed (the pre-fill loop) from the real-chunk
// stitching path above, and needs its own proof of byte-identical output.
func TestFetchComposeCompressed_DegradedMatchesOldApproach(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond) // well past maxWait below -- every chunk times out, nothing cached
	}))
	defer srv.Close()

	newTile := func() *Tile {
		return &Tile{
			Row: 0, Col: 0, Zoom: 10,
			Maptype:  "TEST",
			MinZoom:  10,
			CacheDir: t.TempDir(),
			Provider: testProviderFor(srv.URL),
			Client:   &http.Client{},
			MaxWait:  5 * time.Millisecond,
			DDS:      NewDDS(GridSize*256, GridSize*256, native.DefaultDecoder(), native.DefaultCompressor()),
			log:      noopLogger{},
		}
	}

	const w, h, zoom = 2, 2, 10

	oldWay := newTile()
	img, degradedOld := oldWay.fetchComposite(context.Background(), 0, 0, w, h, zoom)
	wantData, err := oldWay.DDS.Compressor.CompressBC1(img.Pix, img.Width, img.Height)
	if err != nil {
		t.Fatalf("old-way compress: %v", err)
	}
	if !degradedOld {
		t.Fatal("expected the old approach to report degraded=true with every chunk timing out")
	}

	newWay := newTile()
	gotData, degradedNew, _ := newWay.fetchComposeCompressed(context.Background(), 0, 0, w, h, zoom)
	if !degradedNew {
		t.Fatal("expected fetchComposeCompressed to report degraded=true with every chunk timing out")
	}

	if !bytes.Equal(gotData, wantData) {
		t.Error("fetchComposeCompressed's all-fallback output differs from the old whole-composite-then-compress approach")
	}
}
