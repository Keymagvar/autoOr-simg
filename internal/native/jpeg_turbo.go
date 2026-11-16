//go:build cgo && linux

package native

/*
#cgo LDFLAGS: ${SRCDIR}/testdata/lib/linux/libturbojpeg.a
#include <turbojpeg.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// DecodeJPEGTurbo decodes JPEG bytes via libjpeg-turbo's TurboJPEG API —
// the same library autoortho/autoortho/aoimage/aoimage.c already links
// against, just called directly here rather than through that wrapper, to
// isolate decode throughput from the rest of AoImage's responsibilities.
func DecodeJPEGTurbo(data []byte) (*RGBAImage, error) {
	handle := C.tjInitDecompress()
	if handle == nil {
		return nil, fmt.Errorf("tjInitDecompress failed")
	}
	defer C.tjDestroy(handle)

	var width, height, subsamp, colorspace C.int
	if len(data) == 0 {
		return nil, fmt.Errorf("empty jpeg data")
	}
	srcPtr := (*C.uchar)(unsafe.Pointer(&data[0]))
	if C.tjDecompressHeader3(handle, srcPtr, C.ulong(len(data)), &width, &height, &subsamp, &colorspace) != 0 {
		return nil, fmt.Errorf("tjDecompressHeader3: %s", C.GoString(C.tjGetErrorStr2(handle)))
	}

	w, h := int(width), int(height)
	out := &RGBAImage{Width: w, Height: h, Pix: make([]byte, w*h*4)}
	dstPtr := (*C.uchar)(unsafe.Pointer(&out.Pix[0]))

	ret := C.tjDecompress2(handle, srcPtr, C.ulong(len(data)), dstPtr, C.int(w), 0, C.int(h), C.TJPF_RGBA, 0)
	if ret != 0 {
		return nil, fmt.Errorf("tjDecompress2: %s", C.GoString(C.tjGetErrorStr2(handle)))
	}
	return out, nil
}
