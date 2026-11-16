// Package tile is the tile/chunk fetch engine and DDS mipmap generator —
// the Go equivalent of getortho.py + pydds.py. See ../../DESIGN.md §2, §2a,
// and §7: this is where the mipmap-chain-validity trap and the
// never-serve-filler-noise fallback both have to be handled correctly from
// the start, not retrofitted after a bug report.
package tile

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"

	"autoOr-simg/internal/native"
)

const (
	ddsMagic        = "DDS "
	ddsHeaderSize   = 128 // magic (4) + the fixed-size header struct (124) that follows
	bc1BlockSize    = 8
	ddsFlags        = 0x1 | 0x2 | 0x4 | 0x1000 | 0x20000 | 0x80000 // CAPS|HEIGHT|WIDTH|PIXELFORMAT|MIPMAPCOUNT|LINEARSIZE
	ddsPfFlagsFourCC = 0x4
	ddsCaps         = 0x1000 | 0x400000 // TEXTURE | COMPLEX(mipmap)
)

// Mipmap tracks one level of the chain: its byte range within the DDS
// file, and its compressed data once generated. A nil Data with
// retrieved=false is the normal "not fetched yet" state; nil Data is never
// served directly to a reader — see fallbackBlock in read.go (§7).
type Mipmap struct {
	Idx       int
	StartPos  int
	EndPos    int
	Length    int
	Retrieved bool
	Data      []byte
}

// DDS is an in-memory DXT1(BC1)-compressed texture with a full mipmap
// chain, built and served lazily. It mirrors pydds.py's DDS class,
// including the §2a fix: DXT blocks are 4x4 pixels minimum, but DDS/
// X-Plane expect the chain to continue conceptually down to 1x1, so every
// level below the last real 4x4 block replicates that block's compressed
// bytes rather than being left empty or malformed.
type DDS struct {
	Width, Height int
	Mipmaps       []*Mipmap
	TotalSize     int

	// SmallestRealMipmap is the last level that's still a genuine,
	// DXT-compressible 4x4-or-larger block. Levels beyond this one replicate
	// SmallestRealMipmap's compressed bytes (§2a) rather than being
	// (impossibly) compressed on their own.
	SmallestRealMipmap int

	Decoder    native.ImageDecoder
	Compressor native.TextureCompressor

	// dataMu guards every Mipmap's Data/Retrieved fields. Read() (the FUSE
	// read() path, via Tile.ReadDDSBytes) accesses them with no other
	// synchronization — Tile.mu is released before Read() runs — and
	// SetMipmapData can now be called well after a mipmap was first marked
	// Retrieved, by a background repair goroutine (DESIGN.md §7 item 2)
	// running concurrently with reads of that same, already-"done"
	// mipmap. Before background repair existed, a mipmap's Data was
	// written exactly once and never touched again, so this was
	// theoretically racy but never actually exercised; -race caught it
	// for real the first time a repair could land after a read was
	// already in flight.
	dataMu sync.RWMutex
}

// NewDDS builds the mipmap table (sizes/offsets only — no pixel data yet)
// for a width x height BC1 texture, mirroring pydds.py's DDS.__init__.
func NewDDS(width, height int, decoder native.ImageDecoder, compressor native.TextureCompressor) *DDS {
	d := &DDS{Width: width, Height: height, Decoder: decoder, Compressor: compressor}

	curBytes := ddsHeaderSize
	w, h := width, height
	idx := 0
	for w >= 1 && h >= 1 {
		blockLen := max(1, (w*h)>>4) * bc1BlockSize
		mm := &Mipmap{
			Idx:      idx,
			StartPos: curBytes,
			Length:   blockLen,
			EndPos:   curBytes + blockLen,
		}
		d.Mipmaps = append(d.Mipmaps, mm)
		curBytes += blockLen
		w >>= 1
		h >>= 1
		idx++
	}
	d.TotalSize = curBytes

	// The smallest mipmap level that's still a real, compressible 4x4
	// block. len(Mipmaps)-1 is the 1x1 level X-Plane expects to find; the
	// three levels before it are 2x2 and smaller block-wise impossible
	// sizes too (pydds.py's exact "-3" derivation — matches because DXT
	// requires >=4x4, and the chain continues in powers of two past that
	// down to 1x1, which is exactly 3 levels: 2x2, 1x1... mirrored as-is
	// from the proven Python constant rather than re-derived, since this
	// is the single most important invariant in this whole file).
	d.SmallestRealMipmap = len(d.Mipmaps) - 3
	if d.SmallestRealMipmap < 0 {
		d.SmallestRealMipmap = 0
	}

	return d
}

// DDSSize returns the total byte size of a tile's BC1 DDS file — fixed for
// every tile since every Tile builds a GridSize*256 square DDS (DESIGN.md
// gives no per-tile size variation; FUSE's getattr needs this before any
// chunk is fetched, matching autoortho_fuse.py's hardcoded dds_size
// constant for CFG.pydds.format == "BC1"). Building a throwaway DDS is
// cheap: NewDDS only computes mipmap offsets/lengths, it never touches
// Decoder/Compressor, so both can be nil here.
func DDSSize() int {
	return NewDDS(GridSize*256, GridSize*256, nil, nil).TotalSize
}

// Header returns the 128-byte DDS file header as raw bytes — magic plus
// the fixed-layout fields pydds.py's ctypes Structure serializes directly
// (this does the equivalent with encoding/binary instead of relying on Go
// struct layout matching C, which is not guaranteed).
func (d *DDS) Header() []byte {
	buf := new(bytes.Buffer)
	buf.WriteString(ddsMagic)
	binary.Write(buf, binary.LittleEndian, uint32(124))                  // size
	binary.Write(buf, binary.LittleEndian, uint32(ddsFlags))             // flags
	binary.Write(buf, binary.LittleEndian, uint32(d.Height))             // height
	binary.Write(buf, binary.LittleEndian, uint32(d.Width))              // width
	pitchOrLinearSize := uint32(max(1, (d.Width*d.Height)>>4) * bc1BlockSize)
	binary.Write(buf, binary.LittleEndian, pitchOrLinearSize)
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // depth
	binary.Write(buf, binary.LittleEndian, uint32(len(d.Mipmaps)))       // mipMapCount
	buf.Write(make([]byte, 44))                                          // reserved1
	binary.Write(buf, binary.LittleEndian, uint32(32))                   // pfSize
	binary.Write(buf, binary.LittleEndian, uint32(ddsPfFlagsFourCC))     // pfFlags
	buf.WriteString("DXT1")                                              // fourCC
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // rgbBitCount
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // rBitMask
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // gBitMask
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // bBitMask
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // aBitMask
	binary.Write(buf, binary.LittleEndian, uint32(ddsCaps))              // caps
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // caps2
	buf.Write(make([]byte, 8))                                           // reservedCaps[2]
	binary.Write(buf, binary.LittleEndian, uint32(0))                    // reserved2

	h := buf.Bytes()
	if len(h) != ddsHeaderSize {
		panic(fmt.Sprintf("DDS header is %d bytes, want %d — field list drifted from the format", len(h), ddsHeaderSize))
	}
	return h
}

// FindMipmap returns the mipmap level containing byte offset.
func (d *DDS) FindMipmap(offset int) *Mipmap {
	for _, m := range d.Mipmaps {
		if offset < m.EndPos {
			return m
		}
	}
	return d.Mipmaps[len(d.Mipmaps)-1]
}

// SetMipmapData stores compressed bytes for level idx and, if idx is
// SmallestRealMipmap, replicates them into every remaining smaller level
// (§2a) — the one step that, if skipped, produces a DDS file whose chain
// looks complete by byte-length but is actually hollow at the LODs a
// distant camera view would sample, which is the mechanism behind
// "tile fully loaded, then disappears when zoomed out" (see §2a).
func (d *DDS) SetMipmapData(idx int, data []byte) {
	d.dataMu.Lock()
	defer d.dataMu.Unlock()

	d.Mipmaps[idx].Data = data
	d.Mipmaps[idx].Retrieved = true

	if idx == d.SmallestRealMipmap {
		for i := idx + 1; i < len(d.Mipmaps); i++ {
			d.Mipmaps[i].Data = data
			d.Mipmaps[i].Retrieved = true
		}
	}
}

// IsRetrieved reports whether mipmap level idx is currently marked as
// having real built data — guarded by dataMu like every other access to
// Mipmaps[*].Retrieved/.Data (SetMipmapData, ClearAllData, ReadDDSBytes).
// Added after a real, -race-caught bug: buildMipmap used to read
// Mipmaps[idx].Retrieved directly, protected only by t.mu (the Tile's own
// build-serialization lock) — which provides no mutual exclusion at all
// against ClearAllData's concurrent write under dataMu from a different
// goroutine (Cacher's eviction path, reached via onBuilt while holding
// c.mu, never t.mu). Two different locks guarding the same field from
// different call sites is not actually safe under concurrent access from
// both; this makes every access go through the one lock that actually
// matters.
func (d *DDS) IsRetrieved(idx int) bool {
	d.dataMu.RLock()
	defer d.dataMu.RUnlock()
	return d.Mipmaps[idx].Retrieved
}

// HasAnyData reports whether any mipmap level currently holds real built
// data — i.e. whether this DDS is currently costing real resident memory
// (~11MB once any level is built) rather than being served
// entirely from the near-zero-cost §7 fallback block.
func (d *DDS) HasAnyData() bool {
	d.dataMu.RLock()
	defer d.dataMu.RUnlock()
	for _, m := range d.Mipmaps {
		if m.Data != nil {
			return true
		}
	}
	return false
}

// ClearAllData discards every mipmap's built data and resets Retrieved,
// reclaiming its real resident memory (~11MB) immediately — the Go
// garbage collector frees the underlying byte slices once this is the
// last reference to them, same as any other Go value. The next read of
// this tile serves the §7 fallback block until (and unless) it gets
// rebuilt fresh. This is distinct from evicting the Tile/DDS from the
// Cacher entirely: the Tile stays tracked, still "open" if X-Plane still
// holds it — only its heavy payload is dropped. See evictLRUContentLocked's
// doc comment: this is what actually bounds *cumulative* resident
// tiles over a long session, which MaxOpenTiles's admission-only check
// never did.
func (d *DDS) ClearAllData() {
	d.dataMu.Lock()
	defer d.dataMu.Unlock()
	for _, m := range d.Mipmaps {
		m.Data = nil
		m.Retrieved = false
	}
}
