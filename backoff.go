package avsb

import (
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// Exponential backoff with jitter, honouring a server-supplied Retry-After.
//
// Port of packages/avsb-node/src/reconnectBackoff.ts, kept identical on
// purpose. Retry loops used to back off at a fixed cadence, which is a textbook
// thundering herd: every connected SDK hits a struggling worker again at the
// same instant, preventing it from recovering.
//
// Curve: 1s, 2s, 4s, 8s, 16s, 32s, 60s (capped), with plus or minus 20% jitter
// so SDKs that started together drift apart on each retry. When the server sent
// a Retry-After, that wins for the one attempt, and the attempt counter still
// climbs so the next miss moves up the curve.

const (
	backoffBaseDelay = time.Second
	backoffMaxDelay  = 60 * time.Second
	backoffMinDelay  = 500 * time.Millisecond
	backoffJitter    = 0.2
)

// backoffState is one position on the retry curve. The zero value is the
// "never retried yet" state, so a fresh state needs no constructor.
type backoffState struct {
	attempt int
	delay   time.Duration
}

// next advances the backoff. retryAfter, when positive, replaces the computed
// delay for this attempt only.
func (b backoffState) next(retryAfter time.Duration) backoffState {
	attempt := b.attempt + 1
	if retryAfter > 0 {
		return backoffState{attempt: attempt, delay: retryAfter}
	}

	base := backoffBaseDelay << uint(attempt-1)
	if base > backoffMaxDelay || base <= 0 {
		base = backoffMaxDelay
	}
	// Symmetric jitter in [-20%, +20%).
	jitter := float64(base) * backoffJitter * (rand.Float64()*2 - 1) //nolint:gosec // jitter, not a secret
	delay := time.Duration(float64(base) + jitter)
	if delay < backoffMinDelay {
		delay = backoffMinDelay
	}
	return backoffState{attempt: attempt, delay: delay}
}

// reset returns the zero state, called after a success so the next failure
// starts at one second again rather than wherever the last outage ended.
func (b backoffState) reset() backoffState {
	return backoffState{}
}

// parseRetryAfter reads an HTTP Retry-After header value. Only the numeric
// seconds form is supported, which is what the workers emit; an HTTP-date or
// anything unparseable returns 0, meaning "use the curve".
func parseRetryAfter(headerValue string) time.Duration {
	trimmed := strings.TrimSpace(headerValue)
	if trimmed == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

// jitteredInterval spreads a fixed polling interval by plus or minus 20%, so a
// fleet that deployed together does not hit the CDN in lockstep.
func jitteredInterval(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	jitter := float64(base) * backoffJitter * (rand.Float64()*2 - 1) //nolint:gosec // jitter, not a secret
	out := time.Duration(float64(base) + jitter)
	if out < time.Second {
		out = time.Second
	}
	return out
}
