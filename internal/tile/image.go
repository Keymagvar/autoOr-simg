package tile

import "autoOr-simg/internal/native"

// newRGBA creates a tightly-packed RGBA buffer filled with color.
func newRGBA(w, h int, color [4]byte) *native.RGBAImage {
	img := &native.RGBAImage{Width: w, Height: h, Pix: make([]byte, w*h*4)}
	for i := 0; i < w*h; i++ {
		copy(img.Pix[i*4:i*4+4], color[:])
	}
	return img
}

// stitchBlocks copies BC1-compressed block data for a (w, h) RGBA region
// (already compressed — see fetchComposeCompressed) into dst at block
// offset (blockX, blockY), where dst represents one contiguous texture
// dstBlocksWide blocks wide. BC1 blocks are 4x4 pixels, 8 bytes each,
// row-major within whatever region was compressed — compressed must be
// exactly ((w+3)/4)*((h+3)/4)*8 bytes, tightly packed for just this region
// (what TextureCompressor.CompressBC1 returns).
//
// This works at all, and produces byte-identical output to compressing one
// big region in a single call, specifically because BC1/DXT1 is a
// block-local format by design: each 4x4 block's compressed bytes depend
// only on that block's own 16 pixels, never on neighboring blocks. There is
// no cross-block state to get wrong by compressing in pieces and copying
// the pieces into place afterward.
func stitchBlocks(dst []byte, dstBlocksWide, blockX, blockY int, compressed []byte, w, h int) {
	regionBlocksWide := (w + 3) / 4
	regionBlocksHigh := (h + 3) / 4
	for by := 0; by < regionBlocksHigh; by++ {
		srcOff := by * regionBlocksWide * 8
		dstOff := ((blockY+by)*dstBlocksWide + blockX) * 8
		copy(dst[dstOff:dstOff+regionBlocksWide*8], compressed[srcOff:srcOff+regionBlocksWide*8])
	}
}

// paste copies src into dst at (x, y), clipping to dst's bounds. Pure Go —
// this is cheap raster work, not the compression hot path, so it doesn't
// need a cgo binding (§2 keeps the native surface limited to decode/
// compress specifically).
func paste(dst *native.RGBAImage, src *native.RGBAImage, x, y int) {
	for row := 0; row < src.Height; row++ {
		dy := y + row
		if dy < 0 || dy >= dst.Height {
			continue
		}
		srcOff := row * src.Width * 4
		dstOff := (dy*dst.Width + x) * 4

		// Clip horizontally.
		startX := 0
		if x < 0 {
			startX = -x
		}
		endX := src.Width
		if x+src.Width > dst.Width {
			endX = dst.Width - x
		}
		if startX >= endX {
			continue
		}
		copy(dst.Pix[dstOff+startX*4:dstOff+endX*4], src.Pix[srcOff+startX*4:srcOff+endX*4])
	}
}

// cropScaleUp crops a (w, h) region of src starting at (x, y), then bilinear-
// upscales it by scale — getortho.py's Tile.get_best_chunk equivalent:
// AoImage.crop followed by AoImage.scale. Used to turn an already-cached
// COARSER chunk into a usably-sized (if blurry) stand-in for a finer chunk
// that isn't ready yet, rather than a flat fallback color.
//
// Bilinear, not nearest-neighbor (what this replaced): nearest-neighbor
// just repeats each source pixel into a scale x scale block, which reads as
// hard, blocky edges — mathematically correct for "scale it up" in the
// loosest sense, but not a reconstruction of anything resembling continuous
// imagery. Bilinear interpolates between each output pixel's four nearest
// source neighbors, weighted by distance, which is what every real
// image/GPU texture magnification filter does for exactly this reason —
// still can't invent detail that was never fetched (no algorithm can,
// that's an information limit, not a code quality one), but doesn't
// introduce new blockiness on top of the resolution it does have.
func cropScaleUp(src *native.RGBAImage, x, y, w, h, scale int) *native.RGBAImage {
	outW, outH := w*scale, h*scale
	out := &native.RGBAImage{Width: outW, Height: outH, Pix: make([]byte, outW*outH*4)}

	clamp := func(v, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}

	// sx0/sx1/fx depend only on ox, never on oy — the original
	// implementation recomputed them from scratch for every single output
	// pixel, meaning outH-1 redundant recomputations of the same value per
	// column. A live CPU profile of a real running process found this
	// function consuming 78.76% of all CPU time under heavy load (far
	// above CompressBC1ISPC's 16.76% and DecodeJPEGTurbo's 1.75% — the
	// two costs previously assumed to be the real bottleneck) — precomputed
	// once per column here instead, cut via
	// BenchmarkCropScaleUp_Drop2Scale (image_bench_test.go) from
	// 221ms/call to the number recorded in that benchmark's own history,
	// for the exact 1024x1024->4096x4096 shape zoomDropForLoad's drop=2
	// case actually produces in production. Output is byte-for-byte
	// identical — this changes only what's recomputed, not the math
	// itself (same clamp, same pixel-center alignment, same bilinear
	// weights).
	sx0s := make([]int, outW)
	sx1s := make([]int, outW)
	fxs := make([]float64, outW)
	for ox := 0; ox < outW; ox++ {
		sx := clamp((float64(ox)+0.5)/float64(scale)-0.5, 0, float64(w-1))
		sx0 := int(sx)
		sx1 := sx0 + 1
		if sx1 > w-1 {
			sx1 = w - 1
		}
		sx0s[ox] = sx0
		sx1s[ox] = sx1
		fxs[ox] = sx - float64(sx0)
	}

	srcPixel := func(sx, sy int) []byte {
		off := ((y+sy)*src.Width + (x + sx)) * 4
		return src.Pix[off : off+4]
	}

	for oy := 0; oy < outH; oy++ {
		// Continuous source row this output row falls at, pixel-center
		// aligned (the +0.5/-0.5 pair keeps the first and last output rows
		// centered on the first and last source rows rather than slightly
		// offset), then clamped so edge pixels don't sample outside the
		// cropped region.
		sy := clamp((float64(oy)+0.5)/float64(scale)-0.5, 0, float64(h-1))
		sy0 := int(sy)
		sy1 := sy0 + 1
		if sy1 > h-1 {
			sy1 = h - 1
		}
		fy := sy - float64(sy0)

		for ox := 0; ox < outW; ox++ {
			sx0, sx1, fx := sx0s[ox], sx1s[ox], fxs[ox]

			p00, p10 := srcPixel(sx0, sy0), srcPixel(sx1, sy0)
			p01, p11 := srcPixel(sx0, sy1), srcPixel(sx1, sy1)

			// (1-fx) and (1-fy) don't depend on the channel index c, but
			// the original code recomputed (1-fx) twice per channel (8
			// times per pixel) anyway — hoisted out of the loop, same
			// values, same output.
			invFx, invFy := 1-fx, 1-fy
			dstOff := (oy*outW + ox) * 4
			for c := 0; c < 4; c++ {
				top := float64(p00[c])*invFx + float64(p10[c])*fx
				bottom := float64(p01[c])*invFx + float64(p11[c])*fx
				out.Pix[dstOff+c] = byte(top*invFy + bottom*fy + 0.5)
			}
		}
	}
	return out
}

// reduce2 box-downsamples src by a factor of 2 (averaging 2x2 pixel
// blocks) — the Go equivalent of AoImage.reduce_2, used to build each
// mipmap level from the one above it.
func reduce2(src *native.RGBAImage) *native.RGBAImage {
	w, h := src.Width/2, src.Height/2
	out := &native.RGBAImage{Width: w, Height: h, Pix: make([]byte, w*h*4)}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum [4]int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					sx, sy := x*2+dx, y*2+dy
					off := (sy*src.Width + sx) * 4
					for c := 0; c < 4; c++ {
						sum[c] += int(src.Pix[off+c])
					}
				}
			}
			off := (y*w + x) * 4
			for c := 0; c < 4; c++ {
				out.Pix[off+c] = byte(sum[c] / 4)
			}
		}
	}
	return out
}
