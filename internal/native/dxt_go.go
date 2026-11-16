package native

// CompressBC1Go is a pure-Go BC1/DXT1 encoder — a straightforward "range
// fit" per-4x4-block compressor (min/max corner colors, 4-color palette,
// nearest-color pixel assignment). It is not tuned for maximum throughput;
// it exists to give a realistic pure-Go baseline to compare against the
// cgo-bound ISPC compressor (dxt_cgo.go) per DESIGN.md §2 — "don't take
// upstream micro-benchmarks as given, measure this workload."
//
// width and height must be multiples of 4. pix is tightly packed RGBA,
// width*height*4 bytes.
func CompressBC1Go(pix []byte, width, height int) []byte {
	blocksWide := width / 4
	blocksHigh := height / 4
	out := make([]byte, blocksWide*blocksHigh*8)

	var block [16 * 4]byte // 16 pixels * RGBA, scratch buffer reused per block

	oi := 0
	for by := 0; by < blocksHigh; by++ {
		for bx := 0; bx < blocksWide; bx++ {
			extractBlock(pix, width, bx*4, by*4, block[:])
			encodeBC1Block(block[:], out[oi:oi+8])
			oi += 8
		}
	}
	return out
}

func extractBlock(pix []byte, width, x0, y0 int, dst []byte) {
	for row := 0; row < 4; row++ {
		srcOff := ((y0+row)*width + x0) * 4
		copy(dst[row*16:row*16+16], pix[srcOff:srcOff+16])
	}
}

func encodeBC1Block(block []byte, dst []byte) {
	// Find min/max corners of the bounding box in RGB space across the 16
	// pixels — the classic cheap-and-fast BC1 heuristic.
	var minR, minG, minB byte = 255, 255, 255
	var maxR, maxG, maxB byte = 0, 0, 0
	for i := 0; i < 16; i++ {
		r, g, b := block[i*4], block[i*4+1], block[i*4+2]
		if r < minR {
			minR = r
		}
		if g < minG {
			minG = g
		}
		if b < minB {
			minB = b
		}
		if r > maxR {
			maxR = r
		}
		if g > maxG {
			maxG = g
		}
		if b > maxB {
			maxB = b
		}
	}

	c0 := rgb565(maxR, maxG, maxB)
	c1 := rgb565(minR, minG, minB)
	// BC1 opaque mode requires c0 > c1 numerically to select the 4-color
	// (non-alpha) palette rather than the 3-color+transparent one.
	if c0 < c1 {
		c0, c1 = c1, c0
		minR, maxR = maxR, minR
		minG, maxG = maxG, minG
		minB, maxB = maxB, minB
	} else if c0 == c1 {
		// Degenerate (flat) block: nudge so the two endpoints differ,
		// avoiding the 3-color palette branch for a uniform area.
		if c0 < 0xFFFF {
			c0++
		} else {
			c1--
		}
	}

	dst[0] = byte(c0)
	dst[1] = byte(c0 >> 8)
	dst[2] = byte(c1)
	dst[3] = byte(c1 >> 8)

	// Palette: color0, color1, and two interpolated thirds.
	pr := [4]int{int(maxR), int(minR), 0, 0}
	pg := [4]int{int(maxG), int(minG), 0, 0}
	pb := [4]int{int(maxB), int(minB), 0, 0}
	pr[2] = (2*pr[0] + pr[1]) / 3
	pg[2] = (2*pg[0] + pg[1]) / 3
	pb[2] = (2*pb[0] + pb[1]) / 3
	pr[3] = (pr[0] + 2*pr[1]) / 3
	pg[3] = (pg[0] + 2*pg[1]) / 3
	pb[3] = (pb[0] + 2*pb[1]) / 3

	var indices uint32
	for i := 0; i < 16; i++ {
		r, g, b := int(block[i*4]), int(block[i*4+1]), int(block[i*4+2])
		best, bestDist := 0, 1<<30
		for p := 0; p < 4; p++ {
			dr, dg, db := r-pr[p], g-pg[p], b-pb[p]
			dist := dr*dr + dg*dg + db*db
			if dist < bestDist {
				bestDist = dist
				best = p
			}
		}
		indices |= uint32(best) << uint(i*2)
	}
	dst[4] = byte(indices)
	dst[5] = byte(indices >> 8)
	dst[6] = byte(indices >> 16)
	dst[7] = byte(indices >> 24)
}

func rgb565(r, g, b byte) uint16 {
	return (uint16(r)>>3)<<11 | (uint16(g)>>2)<<5 | uint16(b)>>3
}
