package core

// MurmurHash3 implements the MurmurHash3 x86 32-bit hash algorithm in a way
// that produces byte-identical output to the TypeScript reference
// implementation in packages/avsb-core/src/hash.ts.
//
// The TypeScript implementation iterates UTF-16 code units (via charCodeAt)
// and masks each with 0xff before packing into a 32-bit block. We replicate
// that behaviour by ranging over the input string with utf16-aware indexing.
//
// Constants and finalization steps are copied verbatim from the TypeScript
// reference so cross-runtime hash collisions stay in lockstep.
func MurmurHash3(key string, seed uint32) uint32 {
	const c1 uint32 = 0xcc9e2d51
	const c2 uint32 = 0x1b873593

	codeUnits := stringToUTF16(key)
	length := len(codeUnits)
	nblocks := length >> 2
	h1 := seed

	// Body — process complete 4-code-unit blocks.
	for i := 0; i < nblocks; i++ {
		i4 := i * 4
		var k1 uint32 = uint32(codeUnits[i4]&0xff) |
			(uint32(codeUnits[i4+1]&0xff) << 8) |
			(uint32(codeUnits[i4+2]&0xff) << 16) |
			(uint32(codeUnits[i4+3]&0xff) << 24)

		k1 = imul(k1, c1)
		k1 = (k1 << 15) | (k1 >> 17)
		k1 = imul(k1, c2)

		h1 ^= k1
		h1 = (h1 << 13) | (h1 >> 19)
		h1 = imul(h1, 5) + 0xe6546b64
	}

	// Tail — handle the final 0..3 remaining code units.
	tail := nblocks * 4
	var k1 uint32 = 0
	switch length & 3 {
	case 3:
		k1 ^= uint32(codeUnits[tail+2]&0xff) << 16
		fallthrough
	case 2:
		k1 ^= uint32(codeUnits[tail+1]&0xff) << 8
		fallthrough
	case 1:
		k1 ^= uint32(codeUnits[tail] & 0xff)
		k1 = imul(k1, c1)
		k1 = (k1 << 15) | (k1 >> 17)
		k1 = imul(k1, c2)
		h1 ^= k1
	}

	// Finalization mix — same constants as the TypeScript reference.
	h1 ^= uint32(length)
	h1 ^= h1 >> 16
	h1 = imul(h1, 0x85ebca6b)
	h1 ^= h1 >> 13
	h1 = imul(h1, 0xc2b2ae35)
	h1 ^= h1 >> 16

	return h1
}

// imul performs Math.imul-equivalent 32-bit multiplication.
// Go's uint32 arithmetic already wraps modulo 2^32.
func imul(a, b uint32) uint32 {
	return a * b
}

// stringToUTF16 converts a UTF-8 Go string to its UTF-16 code unit sequence
// so MurmurHash3 reads the same code units as the JavaScript
// String.prototype.charCodeAt API. Code points in the BMP take one unit;
// supplementary plane code points split into a surrogate pair.
func stringToUTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r <= 0xffff {
			out = append(out, uint16(r))
			continue
		}
		// Encode supplementary code point as a UTF-16 surrogate pair.
		r -= 0x10000
		hi := 0xd800 + uint16((r>>10)&0x3ff)
		lo := 0xdc00 + uint16(r&0x3ff)
		out = append(out, hi, lo)
	}
	return out
}
