//go:build cgo && linux

package native

import (
	"image"
	"testing"
)

// TestDecodeJPEGTurbo_StaticallyLinkedMatchesPureGo is a real pixel-level
// correctness check for the static-linked libjpeg-turbo (vendored
// testdata/lib/linux/libturbojpeg.a, the same archive the Python original
// ships and statically links in its own aoimage.c) — not just "it didn't crash."
// A broken symbol resolution or ABI mismatch between the vendored archive
// and this build would most likely crash or produce garbage, not silently
// pass, but comparing actual decoded bytes against the pure-Go decoder for
// the same real JPEG fixture is the real test, not an assumption.
func TestDecodeJPEGTurbo_StaticallyLinkedMatchesPureGo(t *testing.T) {
	data := loadFixture(t, "test_tile_small.jpg")

	goResult, err := DecodeJPEGGo(data)
	if err != nil {
		t.Fatalf("pure-Go decode: %v", err)
	}
	turboImg, err := DecodeJPEGTurbo(data)
	if err != nil {
		t.Fatalf("turbojpeg decode: %v", err)
	}

	bounds := goResult.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w != turboImg.Width || h != turboImg.Height {
		t.Fatalf("size mismatch: go=%dx%d turbo=%dx%d", w, h, turboImg.Width, turboImg.Height)
	}
	goRGBA := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			goRGBA.Set(x, y, goResult.At(x, y))
		}
	}

	if len(goRGBA.Pix) != len(turboImg.Pix) {
		t.Fatalf("pixel buffer length mismatch: go=%d turbo=%d", len(goRGBA.Pix), len(turboImg.Pix))
	}

	// JPEG is lossy and the two decoders use different IDCT/upsampling
	// implementations, so exact byte-for-byte equality isn't the right
	// bar — real decoded images should still agree almost everywhere.
	// A broken link (wrong symbols, corrupted archive) would show up as
	// wildly different output, not a handful of off-by-a-few pixels.
	var maxDiff, diffCount int
	for i := range goRGBA.Pix {
		d := int(goRGBA.Pix[i]) - int(turboImg.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > 0 {
			diffCount++
		}
		if d > maxDiff {
			maxDiff = d
		}
	}
	diffFraction := float64(diffCount) / float64(len(goRGBA.Pix))
	if maxDiff > 20 || diffFraction > 0.5 {
		t.Fatalf("decoded output diverges too much between decoders (statically-linked turbojpeg likely broken): maxDiff=%d diffFraction=%.3f", maxDiff, diffFraction)
	}
	t.Logf("decoders agree closely: maxDiff=%d diffFraction=%.4f (expected: small, from real lossy-codec implementation differences)", maxDiff, diffFraction)
}
