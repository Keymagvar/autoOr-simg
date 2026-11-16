//go:build cgo && linux

package native

// Narrowed from a plain "cgo" tag: dxt_cgo.go/jpeg_turbo.go's #cgo LDFLAGS
// hardcode Linux-only linker syntax and paths (ELF $ORIGIN rpath, a .so
// and a .a vendored only under testdata/lib/linux), so they only exist on
// a "cgo && linux" build. A plain "cgo" build on macOS (ld64, no vendored
// darwin libs yet — DESIGN.md §13) would otherwise try to compile this
// file, which calls DecodeJPEGTurbo/CompressBC1ISPC, and fail with
// "undefined" errors instead of falling back cleanly — see
// defaults_cgo_other.go for that fallback.
import "fmt"

// TurboDecoder is the default ImageDecoder: cgo onto libjpeg-turbo
// (jpeg_turbo.go), per §2's "cgo first" decision, benchmarked ~2.5-2.8x
// faster than the pure-Go fallback on this repo's own test data.
type TurboDecoder struct{}

func (TurboDecoder) Decode(jpegBytes []byte) (*RGBAImage, error) {
	return DecodeJPEGTurbo(jpegBytes)
}

// ISPCCompressor is the default TextureCompressor: cgo onto the same
// libispc_texcomp.so the Python version uses, per §2 — benchmarked
// ~3.6-4.1x faster than the pure-Go fallback.
type ISPCCompressor struct{}

func (ISPCCompressor) CompressBC1(rgba []byte, w, h int) ([]byte, error) {
	if w%4 != 0 || h%4 != 0 {
		return nil, fmt.Errorf("CompressBC1: dimensions must be multiples of 4, got %dx%d", w, h)
	}
	return CompressBC1ISPC(rgba, w, h), nil
}

// DefaultDecoder and DefaultCompressor are the production choices per §2.
func DefaultDecoder() ImageDecoder         { return TurboDecoder{} }
func DefaultCompressor() TextureCompressor { return ISPCCompressor{} }
