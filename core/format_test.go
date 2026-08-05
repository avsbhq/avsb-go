package core

import "testing"

// TestFormatNumberLikeJS locks the reason-string number format against the
// JavaScript reference. These exact strings appear inside conformance reason
// assertions, which are compared byte for byte, so a formatting slip is a
// cross-language conformance failure and not a cosmetic one.
func TestFormatNumberLikeJS(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{
			name: "integer threshold prints with no decimal point",
			in:   0.1 * 10000,
			want: "1000",
		},
		{
			name: "fractional threshold keeps its fraction",
			in:   0.12345 * 10000,
			want: "1234.5",
		},
		{
			name: "0.333 allocation is exactly 3330 in float64",
			in:   0.333 * 10000,
			want: "3330",
		},
		{
			name: "full allocation",
			in:   1.0 * 10000,
			want: "10000",
		},
		{
			name: "cumulative split boundary from the boundary fixtures",
			in:   0.12345*10000 + 0.5*10000,
			want: "6234.5",
		},
		{
			name: "thirds split adds up to a whole number",
			in:   0.3333*10000 + 0.3333*10000 + 0.3334*10000,
			want: "10000",
		},
		{
			name: "zero",
			in:   0,
			want: "0",
		},
		{
			name: "a value that needs every digit to round-trip",
			in:   0.6234499999 * 10000,
			want: "6234.499999000001",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatNumberLikeJS(tc.in); got != tc.want {
				t.Errorf("formatNumberLikeJS(%v): want %q, got %q", tc.in, tc.want, got)
			}
		})
	}
}
