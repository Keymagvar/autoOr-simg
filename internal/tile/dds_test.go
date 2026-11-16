package tile

import (
	"bytes"
	"testing"

	"autoOr-simg/internal/native"
)

func testDDS(t *testing.T) *DDS {
	t.Helper()
	return NewDDS(4096, 4096, native.GoDecoder{}, native.GoCompressor{})
}

// TestMipmapChainStructure locks in the byte-layout math against the known
// values from the Python version (pydds.py), so a refactor here can't
// silently drift the DDS file size X-Plane is told to expect.
func TestMipmapChainStructure(t *testing.T) {
	d := testDDS(t)

	// 4096x4096 BC1: mipmap 0 is (4096*4096)>>4 * 8 bytes.
	wantMM0Len := (4096 * 4096 >> 4) * 8
	if got := d.Mipmaps[0].Length; got != wantMM0Len {
		t.Errorf("mipmap 0 length = %d, want %d", got, wantMM0Len)
	}
	if d.Mipmaps[0].StartPos != ddsHeaderSize {
		t.Errorf("mipmap 0 StartPos = %d, want %d (right after header)", d.Mipmaps[0].StartPos, ddsHeaderSize)
	}

	// Chain must continue in powers of two down to 1x1: log2(4096)+1 = 13 levels.
	if len(d.Mipmaps) != 13 {
		t.Fatalf("mipmap count = %d, want 13 for a 4096x4096 texture", len(d.Mipmaps))
	}

	// BC1 (11,184,952 in autoortho_fuse.py's getattr) is the known-good
	// total size X-Plane is told via st_size for the BC1 case.
	const wantTotalSize = 11184952
	if d.TotalSize != wantTotalSize {
		t.Errorf("TotalSize = %d, want %d (must match autoortho_fuse.py's hardcoded st_size for BC1)", d.TotalSize, wantTotalSize)
	}
}

// TestMipmapChainAlwaysValidAfterSmallestReal is the explicit invariant
// check DESIGN.md §2a calls for: once the last real (>=4x4 block)
// mipmap is compressed, every remaining declared level down to 1x1 must
// have real, non-nil, correctly retrieved data — never left empty or
// malformed, which is the structural bug behind "tile disappears when
// zoomed out."
func TestMipmapChainAlwaysValidAfterSmallestReal(t *testing.T) {
	d := testDDS(t)

	fakeBlock := bytes.Repeat([]byte{0xAB}, bc1BlockSize)
	d.SetMipmapData(d.SmallestRealMipmap, fakeBlock)

	for i := d.SmallestRealMipmap; i < len(d.Mipmaps); i++ {
		mm := d.Mipmaps[i]
		if !mm.Retrieved {
			t.Errorf("mipmap %d: Retrieved = false, want true (every level from SmallestRealMipmap down to 1x1 must be populated)", i)
		}
		if mm.Data == nil {
			t.Errorf("mipmap %d: Data is nil, want the replicated smallest-real-block bytes", i)
			continue
		}
		if !bytes.Equal(mm.Data, fakeBlock) {
			t.Errorf("mipmap %d: Data = %x, want replicated %x", i, mm.Data, fakeBlock)
		}
	}

	// Levels *before* SmallestRealMipmap must NOT have been touched by
	// this call — only the actual fetch path populates those.
	if d.SmallestRealMipmap > 0 {
		if d.Mipmaps[d.SmallestRealMipmap-1].Retrieved {
			t.Error("a mipmap before SmallestRealMipmap was marked retrieved by SetMipmapData at SmallestRealMipmap — replication range is wrong")
		}
	}
}

func TestHeaderSizeAndMagic(t *testing.T) {
	d := testDDS(t)
	h := d.Header()
	if len(h) != ddsHeaderSize {
		t.Fatalf("header length = %d, want %d", len(h), ddsHeaderSize)
	}
	if string(h[0:4]) != "DDS " {
		t.Errorf("magic = %q, want %q", h[0:4], "DDS ")
	}
	if string(h[84:88]) != "DXT1" {
		t.Errorf("fourCC = %q, want %q", h[84:88], "DXT1")
	}
}

func TestReadHeaderThenFallbackForUnretrievedMipmap(t *testing.T) {
	d := testDDS(t)

	// Reading the header (offset 0) must exactly match Header().
	got := d.Read(0, ddsHeaderSize)
	want := d.Header()
	if !bytes.Equal(got, want) {
		t.Fatalf("Read(0, header) did not match Header()")
	}

	// Reading into mipmap 0, which has never been compressed, must return
	// the tiled fallback block — never zero bytes, never 0x88 filler
	// (§7's fix for the Python version's behavior).
	data := d.Read(d.Mipmaps[0].StartPos, 64)
	if len(data) != 64 {
		t.Fatalf("got %d bytes, want 64", len(data))
	}
	allSame := true
	for _, b := range data {
		if b != data[0] {
			allSame = false
		}
	}
	// The fallback block for a flat color is NOT a single repeated byte
	// (it's a real BC1-encoded block), so if every byte came back
	// identical, something degenerate happened instead of real fallback
	// tiling.
	if allSame {
		t.Errorf("fallback read looks degenerate (all bytes = %#x) — expected tiled BC1 fallback block bytes", data[0])
	}
	if bytes.Equal(data, bytes.Repeat([]byte{0x88}, 64)) {
		t.Error("fallback read returned literal 0x88 filler bytes — this is exactly the bug §7 exists to fix")
	}
}
