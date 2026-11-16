package tile

// Read returns length bytes starting at offset from the DDS file — header
// for offset<128, otherwise compressed mipmap data, falling back to a
// tiled solid-color block (never raw filler bytes, per §7) for any portion
// of a mipmap that hasn't been compressed yet.
func (d *DDS) Read(offset, length int) []byte {
	out := make([]byte, 0, length)
	pos := offset
	remaining := length

	if pos < ddsHeaderSize {
		h := d.Header()
		n := min(ddsHeaderSize-pos, remaining)
		out = append(out, h[pos:pos+n]...)
		pos += n
		remaining -= n
	}

	for remaining > 0 {
		mm := d.FindMipmap(pos)
		mmPos := pos - mm.StartPos
		avail := mm.EndPos - pos
		if avail <= 0 {
			break // past the end of the declared chain
		}
		want := min(avail, remaining)

		// mm.Data/Retrieved can be mutated concurrently by a background
		// repair (DESIGN.md §7 item 2) well after this mipmap was first
		// marked Retrieved — see dataMu's doc comment on DDS. Snapshot
		// both under the lock, then work with the snapshot.
		d.dataMu.RLock()
		retrieved, data := mm.Retrieved, mm.Data
		d.dataMu.RUnlock()

		got := 0
		if retrieved && data != nil && mmPos < len(data) {
			end := min(mmPos+want, len(data))
			out = append(out, data[mmPos:end]...)
			got = end - mmPos
		}

		if got < want {
			// Not enough real data for this stretch — fill the remainder
			// from the fallback block instead of leaving a gap or
			// emitting raw filler bytes (§7).
			out = append(out, tileFallback(want-got)...)
		}

		pos += want
		remaining -= want
	}

	return out
}

// tileFallback returns n bytes built by repeating the cached fallback BC1
// block, used for any portion of a mipmap not yet compressed.
func tileFallback(n int) []byte {
	block := currentFallbackBlock()
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = block[i%len(block)]
	}
	return out
}
