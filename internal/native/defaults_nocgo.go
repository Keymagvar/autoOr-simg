//go:build !cgo

package native

// DefaultDecoder and DefaultCompressor fall back to the pure-Go
// implementations when built with CGO_ENABLED=0 (§2) — slower, but the
// only option available without a C toolchain.
func DefaultDecoder() ImageDecoder         { return GoDecoder{} }
func DefaultCompressor() TextureCompressor { return GoCompressor{} }
