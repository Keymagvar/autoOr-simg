package tile

import (
	"testing"

	"autoOr-simg/internal/native"
)

// BenchmarkCropScaleUp_Drop2Scale is the real production shape found to
// dominate CPU under heavy saturation (a live `go tool pprof` CPU profile
// of a real running process showed cropScaleUp at 78.76% of all CPU time,
// far above CompressBC1ISPC's 16.76% and DecodeJPEGTurbo's 1.75% — the
// two costs this project had, until this profile, assumed were the real
// bottleneck): zoomDropForLoad's drop=2 case upscales a 1024x1024
// composite (the 4x4-chunk grid fetched at the reduced zoom) by 4x to
// mip0's full 4096x4096 size. Benchmark this exact shape, not an
// arbitrary one, so an optimization's win is measured against the case
// that actually matters in production.
func BenchmarkCropScaleUp_Drop2Scale(b *testing.B) {
	const srcSize = 1024
	const scale = 4
	src := &native.RGBAImage{
		Width: srcSize, Height: srcSize,
		Pix: make([]byte, srcSize*srcSize*4),
	}
	// Non-uniform content (a simple gradient) -- a flat/solid source could
	// let an optimizer short-circuit in a way real aerial imagery never
	// would, giving a misleadingly rosy benchmark number.
	for y := 0; y < srcSize; y++ {
		for x := 0; x < srcSize; x++ {
			off := (y*srcSize + x) * 4
			src.Pix[off] = byte(x)
			src.Pix[off+1] = byte(y)
			src.Pix[off+2] = byte(x + y)
			src.Pix[off+3] = 255
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cropScaleUp(src, 0, 0, srcSize, srcSize, scale)
	}
}
