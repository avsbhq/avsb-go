package avsb

import (
	"testing"
	"time"
)

// TestBackoffClimbsAndCaps covers the curve. A fixed retry cadence is a
// thundering herd: every connected SDK hits a struggling worker at the same
// instant, which is what stops it recovering.
func TestBackoffClimbsAndCaps(t *testing.T) {
	var state backoffState

	// Base delays double from 1s and cap at 60s. Jitter is plus or minus 20%,
	// so each attempt is asserted as a range rather than a value.
	wantBase := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second,
		60 * time.Second,
	}

	for i, base := range wantBase {
		state = state.next(0)
		if state.attempt != i+1 {
			t.Errorf("attempt %d: want counter %d, got %d", i, i+1, state.attempt)
		}
		low := time.Duration(float64(base) * 0.8)
		high := time.Duration(float64(base) * 1.2)
		if low < backoffMinDelay {
			low = backoffMinDelay
		}
		if state.delay < low || state.delay > high {
			t.Errorf("attempt %d: want a delay in [%v, %v] around base %v, got %v",
				i+1, low, high, base, state.delay)
		}
	}
}

// TestBackoffJitterActuallyVaries proves the jitter is real. Without it, SDKs
// that started together stay in lockstep forever.
func TestBackoffJitterActuallyVaries(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		// Attempt 4 has a 8s base, so the jitter window is wide enough that
		// identical values would mean no jitter at all.
		state := backoffState{attempt: 3}
		seen[state.next(0).delay] = true
	}
	if len(seen) < 5 {
		t.Errorf("want jittered delays to differ across calls, got %d distinct values", len(seen))
	}
}

// TestBackoffHonoursRetryAfter covers the server's own instruction, which beats
// the curve for that one attempt while the counter still climbs.
func TestBackoffHonoursRetryAfter(t *testing.T) {
	state := backoffState{}.next(3 * time.Second)
	if state.delay != 3*time.Second {
		t.Errorf("want the requested 3s exactly, got %v", state.delay)
	}
	if state.attempt != 1 {
		t.Errorf("the attempt counter still climbs, got %d", state.attempt)
	}

	// The next miss with no header goes back to the curve, one step further on.
	next := state.next(0)
	if next.attempt != 2 {
		t.Errorf("want attempt 2, got %d", next.attempt)
	}
	if next.delay < time.Duration(1.6*float64(time.Second)) {
		t.Errorf("attempt 2 should sit near a 2s base, got %v", next.delay)
	}
}

// TestBackoffResetsAfterSuccess covers the bug that left a stream reconnecting
// once a minute forever after one bad hour: the curve has to come back down.
func TestBackoffResetsAfterSuccess(t *testing.T) {
	state := backoffState{}
	for i := 0; i < 8; i++ {
		state = state.next(0)
	}
	if state.delay < 30*time.Second {
		t.Fatalf("expected the curve to have climbed, got %v", state.delay)
	}

	state = state.reset()
	if state.attempt != 0 {
		t.Errorf("reset must clear the attempt counter, got %d", state.attempt)
	}

	first := state.next(0)
	if first.delay > 2*time.Second {
		t.Errorf("after a reset the next delay is back near one second, got %v", first.delay)
	}
}

// TestParseRetryAfter covers the header shapes. Only the numeric seconds form is
// supported, which is what the workers emit; anything else falls back to the
// curve rather than guessing.
func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{name: "whole seconds", header: "5", want: 5 * time.Second},
		{name: "padded", header: "  2  ", want: 2 * time.Second},
		{name: "fractional", header: "0.5", want: 500 * time.Millisecond},
		{name: "zero means no wait requested", header: "0", want: 0},
		{name: "absent", header: "", want: 0},
		{name: "negative is nonsense", header: "-3", want: 0},
		{name: "an HTTP date is unsupported", header: "Wed, 21 Oct 2026 07:28:00 GMT", want: 0},
		{name: "not a number", header: "soon", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRetryAfter(tc.header); got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// TestJitteredIntervalStaysUseful covers the polling spread: it must vary, stay
// near the requested interval, and never drop below a second.
func TestJitteredIntervalStaysUseful(t *testing.T) {
	const base = 30 * time.Second
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		got := jitteredInterval(base)
		if got < 24*time.Second || got > 36*time.Second {
			t.Fatalf("want within 20%% of %v, got %v", base, got)
		}
		seen[got] = true
	}
	if len(seen) < 5 {
		t.Errorf("want the interval to vary, got %d distinct values", len(seen))
	}

	if got := jitteredInterval(0); got != 0 {
		t.Errorf("a zero interval stays zero, got %v", got)
	}
	if got := jitteredInterval(time.Millisecond); got < time.Second {
		t.Errorf("want a one second floor, got %v", got)
	}
}
