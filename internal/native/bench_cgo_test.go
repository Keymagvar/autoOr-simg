//go:build cgo

package native

import "testing"

// Split from bench_test.go: these benchmark the cgo-only accelerated
// paths (libjpeg-turbo, ISPC), which don't exist in a CGO_ENABLED=0 build
// (e.g. the current pure-Go Windows build — see packaging/windows/). Keeping them
// in a `cgo`-tagged file instead of bench_test.go lets `go test`/`go vet`
// run clean on that build without losing these benchmarks where cgo is
// available.

func BenchmarkJPEGDecode_Turbo_Chunk256(b *testing.B) {
	data := loadFixture(b, "test_tile_small.jpg")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeJPEGTurbo(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJPEGDecode_Turbo_Full4096(b *testing.B) {
	data := loadFixture(b, "test_tile2.jpg")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeJPEGTurbo(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDXT1Compress_ISPC_Tile4096(b *testing.B) {
	pix, w, h := loadRGBATile(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CompressBC1ISPC(pix, w, h)
	}
}

func BenchmarkDXT1Compress_ISPC_Chunk256(b *testing.B) {
	pix, _, _ := loadRGBATile(b)
	pix256 := pix[:256*256*4]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CompressBC1ISPC(pix256, 256, 256)
	}
}
