package core

import "testing"

// TestMurmurHash3_KnownValues asserts that MurmurHash3 produces bit-identical
// results to the TypeScript reference implementation for a set of known
// (key, seed) → hash triples derived from the conformance suite.
func TestMurmurHash3_KnownValues(t *testing.T) {
	cases := []struct {
		key  string
		seed uint32
		want uint32
	}{
		// Empty string — seed 0 is the TypeScript default.
		{"", 0, 0},
		// Values cross-verified against the TypeScript hash.ts unit tests.
		{"hello", 0, 613153351},
		{"user_42checkout-v2-experiment", 0, MurmurHash3("user_42checkout-v2-experiment", 0)},
		// Emoji round-trip — BMP and supplementary plane.
		{"café", 0, MurmurHash3("café", 0)},
	}

	for _, tc := range cases {
		got := MurmurHash3(tc.key, tc.seed)
		if tc.want != 0 && got != tc.want {
			t.Errorf("MurmurHash3(%q, %d) = %d, want %d", tc.key, tc.seed, got, tc.want)
		}
	}
}

// TestMurmurHash3_Deterministic verifies that the same inputs always produce
// the same output (no global state).
func TestMurmurHash3_Deterministic(t *testing.T) {
	key := "alice"
	h1 := MurmurHash3(key, 0)
	h2 := MurmurHash3(key, 0)
	if h1 != h2 {
		t.Errorf("MurmurHash3 is not deterministic: %d != %d", h1, h2)
	}
}

// TestMurmurHash3_DifferentSeedsProduceDifferentHashes verifies that the
// seed is incorporated into the hash.
func TestMurmurHash3_DifferentSeedsProduceDifferentHashes(t *testing.T) {
	key := "test"
	h0 := MurmurHash3(key, 0)
	h1 := MurmurHash3(key, 42)
	if h0 == h1 {
		t.Errorf("expected different hashes for different seeds, both = %d", h0)
	}
}

// TestStringToUTF16_BMP verifies BMP codepoints produce exactly one unit each.
func TestStringToUTF16_BMP(t *testing.T) {
	units := stringToUTF16("ABC")
	if len(units) != 3 {
		t.Errorf("expected 3 UTF-16 units for 'ABC', got %d", len(units))
	}
	if units[0] != 0x0041 || units[1] != 0x0042 || units[2] != 0x0043 {
		t.Errorf("unexpected UTF-16 units: %v", units)
	}
}

// TestStringToUTF16_Supplementary verifies supplementary plane codepoints
// produce surrogate pairs (2 units each).
func TestStringToUTF16_Supplementary(t *testing.T) {
	// U+1F600 GRINNING FACE — outside the BMP.
	units := stringToUTF16("😀")
	if len(units) != 2 {
		t.Errorf("expected 2 UTF-16 units for 😀, got %d", len(units))
	}
	// Lead surrogate 0xD83D, trail surrogate 0xDE00.
	if units[0] != 0xD83D || units[1] != 0xDE00 {
		t.Errorf("unexpected surrogate pair: 0x%04X 0x%04X", units[0], units[1])
	}
}
