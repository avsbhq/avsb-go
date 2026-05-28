package avsb

// Flag[T] is the typed result of a flag evaluation.
//
// The zero value is a safe "flag not found" result — IsEnabled() returns
// false, Exists() returns false, and Value() returns the default supplied
// to the Get*Flag call.
type Flag[T any] struct {
	value        T
	variationKey *string
	source       string
	ruleID       *string
	ruleType     *string
	reasons      []string
	flagKey      string
}

// Value returns the evaluated flag value (or the default when not found).
func (f Flag[T]) Value() T { return f.value }

// VariationKey returns the matched variation key, or empty string when none.
func (f Flag[T]) VariationKey() string {
	if f.variationKey == nil {
		return ""
	}
	return *f.variationKey
}

// Source returns the evaluation source:
// "rule", "override", "holdout", "disabled", "default", "not_found".
func (f Flag[T]) Source() string { return f.source }

// RuleID returns the matched rule id, or empty string when none.
func (f Flag[T]) RuleID() string {
	if f.ruleID == nil {
		return ""
	}
	return *f.ruleID
}

// RuleType returns the matched rule type ("ab_test", "targeted_delivery",
// "holdout") or empty string when none.
func (f Flag[T]) RuleType() string {
	if f.ruleType == nil {
		return ""
	}
	return *f.ruleType
}

// Reasons returns the human-readable audit trail. Empty unless WithReasons
// or ServerOptions.IncludeReasons is set to true.
func (f Flag[T]) Reasons() []string { return f.reasons }

// FlagKey returns the flag key this result was evaluated for.
func (f Flag[T]) FlagKey() string { return f.flagKey }

// IsEnabled reports true when the flag was matched via a rule and its value
// is truthy (non-zero, non-empty, non-null).
func (f Flag[T]) IsEnabled() bool {
	return f.source == "rule" && isTruthy(f.value)
}

// Exists reports true when the flag key was found in the datafile.
func (f Flag[T]) Exists() bool {
	return f.source != "" && f.source != "not_found"
}

// isTruthy returns false for Go zero values and empty strings. Any other
// value is considered truthy, matching the JavaScript truthiness rules used
// by the TypeScript evaluator for Flag.isEnabled.
func isTruthy[T any](v T) bool {
	switch x := any(v).(type) {
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case float32:
		return x != 0
	case nil:
		return false
	default:
		return true
	}
}
