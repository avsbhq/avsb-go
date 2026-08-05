package avsb

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// Flag is the typed result of one flag evaluation.
//
// The zero value is a safe "nothing was evaluated" result: Value returns T's
// zero value, IsEnabled and Exists both return false, and Source returns "".
type Flag[T any] struct {
	value          T
	variationKey   *string
	source         string
	ruleID         *string
	ruleType       *string
	reasons        []string
	flagKey        string
	evaluatedAt    time.Time
	durationMicros int64
}

// Value returns the evaluated value, or the default supplied to the Get*Flag
// call when no variation was served.
func (f Flag[T]) Value() T { return f.value }

// VariationKey returns the served variation's key, or "" when none was served.
func (f Flag[T]) VariationKey() string {
	if f.variationKey == nil {
		return ""
	}
	return *f.variationKey
}

// Source returns why this value was served. It is one of exactly ten values,
// and the core package exports each as a constant:
//
//	"datafileOverride"  a datafile-declared user override matched
//	"runtimeOverride"   an override set on this client matched
//	"sticky"            a stored sticky assignment was served
//	"rule"              a targeted_delivery or ab_test rule matched
//	"holdout"           the context is in a holdout cohort
//	"bandit"            a bandit rule selected an action
//	"default"           no rule matched, the flag's default was served
//	"not_found"         the datafile loaded and this key is not in it
//	"disabled"          the flag exists but is switched off
//	"not_ready"         there was no datafile to look in yet
//
// "not_found" and "not_ready" are different answers and this SDK no longer
// conflates them: "not_found" means the key is absent from a datafile it has,
// "not_ready" means it has no datafile, so nothing about the key is knowable.
//
// The internal evaluator spellings "override" and "runtime_override" never
// reach here.
func (f Flag[T]) Source() string { return f.source }

// RuleID returns the matched rule id (or holdout id), or "" when none.
func (f Flag[T]) RuleID() string {
	if f.ruleID == nil {
		return ""
	}
	return *f.ruleID
}

// RuleType returns the matched rule type: "ab_test", "targeted_delivery",
// "holdout", "bandit", or "" when none.
func (f Flag[T]) RuleType() string {
	if f.ruleType == nil {
		return ""
	}
	return *f.ruleType
}

// Reasons returns the ordered decision trail, never nil. It is empty unless
// reasons are on, via ServerOptions.IncludeReasons or WithReasons(true).
func (f Flag[T]) Reasons() []string {
	if f.reasons == nil {
		return []string{}
	}
	return f.reasons
}

// FlagKey returns the key this result was evaluated for.
func (f Flag[T]) FlagKey() string { return f.flagKey }

// EvaluatedAt returns when the decision was made.
func (f Flag[T]) EvaluatedAt() time.Time { return f.evaluatedAt }

// DurationMicros returns how long the evaluation took, in microseconds.
func (f Flag[T]) DurationMicros() int64 { return f.durationMicros }

// decidedSources are the six sources that mean a real decision produced this
// value. Everything outside this set is a fallback, however truthy its value.
var decidedSources = map[string]struct{}{
	core.SourceRule:             {},
	core.SourceHoldout:          {},
	core.SourceBandit:           {},
	core.SourceRuntimeOverride:  {},
	core.SourceDatafileOverride: {},
	core.SourceSticky:           {},
}

// IsEnabled reports whether a real decision produced a truthy value.
//
// The definition is identical in every A vs B SDK and is locked by the
// conformance corpus: true when Source is one of "rule", "holdout", "bandit",
// "runtimeOverride", "datafileOverride", or "sticky" AND the value is truthy.
// False otherwise, with no exceptions.
//
// The trap worth naming: a flag serving its DEFAULT variation is not enabled,
// however truthy that default is. "Enabled" means something decided to serve
// it, not that the value happens to look on. Likewise a not-ready SDK never
// reports a feature enabled, even when the caller's default is true.
//
// Truthiness follows the JavaScript rule, because the reference engine is
// TypeScript and a cross-language helper that answers differently per language
// is not a helper: false, 0, -0, NaN, "" and null are falsy, and EVERYTHING
// else is truthy. Note what that means for the surprising cases: the STRING
// "false" is truthy (it is not parsed), the STRING "0" is truthy, and an empty
// JSON array or object is truthy.
func (f Flag[T]) IsEnabled() bool {
	if _, decided := decidedSources[f.source]; !decided {
		return false
	}
	return isTruthy(f.value)
}

// Exists reports whether the flag key is present in the datafile.
//
// False for "not_found" and false for "not_ready": with no datafile loaded,
// whether the key exists is unknowable, so the honest answer is false rather
// than a guess. A DISABLED flag exists: the customer switched it off, which is
// not the same as it being absent.
func (f Flag[T]) Exists() bool {
	return f.source != "" &&
		f.source != core.SourceNotFound &&
		f.source != core.SourceNotReady
}

// isTruthy applies the JavaScript truthiness rule to a Go value.
//
// Numeric zero, false, the empty string, and nil are falsy, and so is NaN. A
// json.RawMessage is inspected as JSON rather than as bytes, so a raw "null" is
// falsy while a raw "[]" is truthy. Anything else (a struct, a map, a non-nil
// pointer) is truthy, matching "every object is truthy".
func isTruthy[T any](v T) bool {
	switch x := any(v).(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.RawMessage:
		return jsonRawTruthy(x)
	case []byte:
		return jsonRawTruthy(x)
	case int:
		return x != 0
	case int8:
		return x != 0
	case int16:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case uint:
		return x != 0
	case uint8:
		return x != 0
	case uint16:
		return x != 0
	case uint32:
		return x != 0
	case uint64:
		return x != 0
	case float32:
		return x != 0 && !math.IsNaN(float64(x))
	case float64:
		return x != 0 && !math.IsNaN(x)
	default:
		return true
	}
}

// jsonRawTruthy applies the JavaScript truthiness rule to a raw JSON value,
// which is what a json-typed flag serves.
func jsonRawTruthy(raw []byte) bool {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "", "null", "false", `""`:
		return false
	}
	// A number, including one written so it is still zero (0, -0, 0.0, 0e5).
	if n, ok := parseJSONNumber(trimmed); ok {
		return n != 0 && !math.IsNaN(n)
	}
	return true
}

// parseJSONNumber reports whether trimmed is a JSON number, and its value.
func parseJSONNumber(trimmed string) (float64, bool) {
	if trimmed == "" {
		return 0, false
	}
	c := trimmed[0]
	if c != '-' && (c < '0' || c > '9') {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal([]byte(trimmed), &n); err != nil {
		return 0, false
	}
	return n, true
}
