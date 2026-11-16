package tile

import (
	"sync"

	"autoOr-simg/internal/native"
)

// fallbackColor is the solid color used when a mipmap region hasn't been
// compressed yet by the time a read needs it — a dark, neutral green-gray,
// matching the compositing background getortho.py already uses
// ((66,77,55) in get_img) so a fallback read looks like "slightly flat
// ground," not an alarming color.
var fallbackColor = [4]byte{66, 77, 55, 255}

var (
	fallbackOnce  sync.Once
	fallbackBlock []byte
)

// currentFallbackBlock returns one precomputed, valid BC1-compressed 4x4
// solid-color block (DESIGN.md §7): the fix for the Python version serving
// literal 0x88 filler bytes as if they were real compressed pixel data
// whenever a mipmap wasn't ready in time. Tiling this real, valid block
// instead means the worst case ever served is "briefly flat-colored,"
// never GPU-decoded noise.
func currentFallbackBlock() []byte {
	fallbackOnce.Do(func() {
		pix := make([]byte, 4*4*4)
		for i := 0; i < 16; i++ {
			copy(pix[i*4:i*4+4], fallbackColor[:])
		}
		block, err := native.DefaultCompressor().CompressBC1(pix, 4, 4)
		if err != nil {
			// A 4x4 solid-color block is the simplest possible input;
			// failure here means the compressor backend is broken, not a
			// recoverable runtime condition.
			panic("tile: failed to precompute fallback BC1 block: " + err.Error())
		}
		fallbackBlock = block
	})
	return fallbackBlock
}
