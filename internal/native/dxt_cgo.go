//go:build cgo && linux

package native

/*
#cgo LDFLAGS: -L${SRCDIR}/testdata/lib/linux -lispc_texcomp -Wl,-rpath,$ORIGIN/lib/linux -Wl,-rpath,${SRCDIR}/testdata/lib/linux
#include <stdint.h>

// No public header ships with libispc_texcomp.so — this struct/prototype
// pair mirrors exactly what pydds.py declares via ctypes (rgba_surface +
// CompressBlocksBC1), since that's the real, proven ABI already in
// production use by the Python version.
typedef struct {
	const uint8_t* data;
	uint32_t width;
	uint32_t height;
	uint32_t stride;
} rgba_surface;

extern void CompressBlocksBC1(const rgba_surface* src, uint8_t* dst);
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// CompressBC1ISPC compresses pix (tightly packed RGBA, width*height*4
// bytes) via the same libispc_texcomp.so the Python version uses by
// default (CFG.pydds.compressor == "ISPC"). width and height must be
// multiples of 4.
func CompressBC1ISPC(pix []byte, width, height int) []byte {
	blocksWide := (width + 3) / 4
	blocksHigh := (height + 3) / 4
	out := make([]byte, blocksWide*blocksHigh*8) // BC1: 8 bytes/block

	// surf.data is a Go pointer (into pix) nested inside a Go struct
	// (surf) that we then pass by address to C — Go 1.21+'s cgo pointer
	// checker requires that nested pointer be pinned for the duration of
	// the call, since a plain pointer-to-struct argument only covers the
	// struct itself, not what its fields point to.
	var pinner runtime.Pinner
	pinner.Pin(&pix[0])
	defer pinner.Unpin()

	surf := C.rgba_surface{
		data:   (*C.uint8_t)(unsafe.Pointer(&pix[0])),
		width:  C.uint32_t(width),
		height: C.uint32_t(height),
		stride: C.uint32_t(width * 4),
	}
	C.CompressBlocksBC1(&surf, (*C.uint8_t)(unsafe.Pointer(&out[0])))
	return out
}
