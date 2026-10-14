package core

import "testing"

// TestFormatNumberLikeJS locks the reason-string number format against the
// JavaScript reference. These exact strings appear inside conformance reason
// assertions, which are compared byte for byte, so a formatting slip is a
// cross-language conformance failure and not a cosmetic one.
//
// Every input goes through scaled, which multiplies at run time in float64.
// Writing the product as a literal (0.6234499999 * 10000) would not: Go folds
// untyped constant arithmetic with exact precision at compile time, so the test
// would format 6234.499999 while JavaScript, and the evaluator at run time,
// computes 6234.499999000001. Each expectation is what node prints for
// String(p * 10000) with the same inputs.
func TestFormatNumberLikeJS(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{
			name: "integer threshold prints with no decimal point",
			in:   scaled(0.1),
			want: "1000",
		},
		{
			name: "fractional threshold keeps its fraction",
			in:   scaled(0.12345),
			want: "1234.5",
		},
		{
			name: "0.333 allocation is exactly 3330 in float64",
			in:   scaled(0.333),
			want: "3330",
		},
		{
			name: "full allocation",
			in:   scaled(1.0),
			want: "10000",
		},
		{
			name: "cumulative split boundary from the boundary fixtures",
			in:   scaled(0.12345) + scaled(0.5),
			want: "6234.5",
		},
		{
			name: "thirds split adds up to a whole number",
			in:   scaled(0.3333) + scaled(0.3333) + scaled(0.3334),
			want: "10000",
		},
		{
			name: "zero",
			in:   0,
			want: "0",
		},
		{
			name: "a run-time product that needs every digit to round-trip, as in JS",
			in:   scaled(0.6234499999),
			want: "6234.499999000001",
		},
		{
			name: "a cumulative boundary that rounds each product before the add, as in JS",
			in:   scaled(0.0003) + scaled(0.7988),
			want: "7991",
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

// scaled is the evaluator's percentage-to-bucket conversion, done at run time.
// The float64 conversion rounds the product before any add that follows it, the
// same guard core/evaluate.go puts on the cumulative split.
//
//go:noinline
func scaled(p float64) float64 {
	return float64(p * 10000)
}
