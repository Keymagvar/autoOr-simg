package native

import "fmt"

// ImageDecoder and TextureCompressor are the swappable backend interfaces
// from DESIGN.md §2 — callers in internal/tile depend on these, never on a
// concrete cgo/pure-Go type directly, so the backend can change later
// without touching the tile engine.
type ImageDecoder interface {
	Decode(jpegBytes []byte) (*RGBAImage, error)
}

type TextureCompressor interface {
	CompressBC1(rgba []byte, w, h int) ([]byte, error)
}

// RGBAImage is a minimal decoded-image result: width/height plus tightly
// packed RGBA bytes. Kept separate from Go's image.Image so both the
// stdlib and TurboJPEG decode paths (and the tile compositor) can work
// with plain byte buffers without image.Image's per-pixel interface
// overhead. Not cgo-gated — both decoders need it, including the
// CGO_ENABLED=0 fallback.
type RGBAImage struct {
	Width, Height int
	Pix           []byte // 4 bytes/pixel, row-major
}

// GoDecoder is the CGO_ENABLED=0 fallback (§2) — not the default, but
// always available since it has no cgo dependency.
type GoDecoder struct{}

func (GoDecoder) Decode(jpegBytes []byte) (*RGBAImage, error) {
	img, err := DecodeJPEGGo(jpegBytes)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := &RGBAImage{Width: w, Height: h, Pix: make([]byte, w*h*4)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*w + x) * 4
			out.Pix[i+0] = byte(r >> 8)
			out.Pix[i+1] = byte(g >> 8)
			out.Pix[i+2] = byte(bl >> 8)
			out.Pix[i+3] = byte(a >> 8)
		}
	}
	return out, nil
}

// GoCompressor is the CGO_ENABLED=0 fallback (§2) — not the default, but
// always available since it has no cgo dependency.
type GoCompressor struct{}

func (GoCompressor) CompressBC1(rgba []byte, w, h int) ([]byte, error) {
	if w%4 != 0 || h%4 != 0 {
		return nil, fmt.Errorf("CompressBC1: dimensions must be multiples of 4, got %dx%d", w, h)
	}
	return CompressBC1Go(rgba, w, h), nil
}
