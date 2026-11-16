package native

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// loadFixture reads a file from testdata/, the location set by DESIGN.md's
// "benchmark against real chunks" instruction for this exact decision —
// not synthetic data.
func loadFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("loading fixture %s: %v", name, err)
	}
	return data
}

// --- JPEG decode: a real 256x256 chunk (the actual per-provider-tile unit) ---

func BenchmarkJPEGDecode_Go_Chunk256(b *testing.B) {
	data := loadFixture(b, "test_tile_small.jpg")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeJPEGGo(data); err != nil {
			b.Fatal(err)
		}
	}
}

// --- JPEG decode: a full 4096x4096 image, for a large-decode comparison ---

func BenchmarkJPEGDecode_Go_Full4096(b *testing.B) {
	data := loadFixture(b, "test_tile2.jpg")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeJPEGGo(data); err != nil {
			b.Fatal(err)
		}
	}
}

// --- BC1/DXT1 compression: a full 4096x4096 RGBA tile (the real per-tile
// compression unit — Tile.width*256 = 16*256 = 4096 in getortho.py) ---

func loadRGBATile(t testing.TB) (pix []byte, w, h int) {
	t.Helper()
	data := loadFixture(t, "test_tile.png")
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding test_tile.png: %v", err)
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	return rgba.Pix, b.Dx(), b.Dy()
}

func BenchmarkDXT1Compress_Go_Tile4096(b *testing.B) {
	pix, w, h := loadRGBATile(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CompressBC1Go(pix, w, h)
	}
}

// --- Also benchmark DXT1 at 256x256 (one chunk's worth) since that's the
// granularity the partial-mipmap-compression path (AGENTS.md's getortho.py
// notes) actually operates at for mipmap 0 reads. ---

func BenchmarkDXT1Compress_Go_Chunk256(b *testing.B) {
	pix, _, _ := loadRGBATile(b)
	pix256 := pix[:256*256*4]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CompressBC1Go(pix256, 256, 256)
	}
}
