// Package native benchmarks the pure-Go vs. cgo backend choice for JPEG
// decode and DXT/BC1 compression (DESIGN.md §2) before committing to one
// as the production ImageDecoder/TextureCompressor implementation. This
// file is the pure-Go JPEG decoder, always available (no cgo required).
package native

import (
	"bytes"
	"image"
	"image/jpeg"
)

// DecodeJPEGGo decodes JPEG bytes using the Go standard library.
func DecodeJPEGGo(data []byte) (image.Image, error) {
	return jpeg.Decode(bytes.NewReader(data))
}
