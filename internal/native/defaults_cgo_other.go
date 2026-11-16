//go:build cgo && !linux

package native

// DefaultDecoder and DefaultCompressor fall back to the pure-Go
// implementations on a cgo build that isn't Linux (macOS today — Windows
// never builds with cgo at all, see packaging/windows/). dxt_cgo.go/jpeg_turbo.go
// are Linux-only (see defaults_cgo.go's comment): no accelerated codec
// exists for this platform yet, so this is the same fallback
// defaults_nocgo.go uses, just reached via a different build tag.
func DefaultDecoder() ImageDecoder         { return GoDecoder{} }
func DefaultCompressor() TextureCompressor { return GoCompressor{} }
