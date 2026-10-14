package avsb

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/avsbhq/avsb-go/core"
)

// The isEnabled and exists tables below are transcribed from the conformance
// corpus' semantics family (packages/avsb-conformance/fixtures/semantics/
// manifest.json). They cannot be expressed as a datafile plus a context, so no
// generated fixture covers them: each SDK asserts them directly, and this is
// this SDK's copy.

// TestIsEnabledMatchesTheSemanticsContract locks the sixteen-row table.
//
// The trap the table exists for: a flag serving its DEFAULT variation is not
// enabled, however truthy the default is. This SDK used to count only source
// "rule", so an overridden-on flag reported enabled in Python and disabled in Go
// for the same visitor.
func TestIsEnabledMatchesTheSemanticsContract(t *testing.T) {
	type row struct {
		name   string
		source string
		// exactly one of these carries the value under test
		boolValue   *bool
		stringValue *string
		numValue    *float64
		jsonValue   json.RawMessage
		want        bool
	}

	boolPtr := func(v bool) *bool { return &v }
	strPtr := func(v string) *string { return &v }
	numPtr := func(v float64) *float64 { return &v }

	rows := []row{
		{name: "enabled_rule_truthy", source: core.SourceRule, boolValue: boolPtr(true), want: true},
		{name: "enabled_rule_falsy", source: core.SourceRule, boolValue: boolPtr(false), want: false},
		{name: "enabled_holdout_truthy", source: core.SourceHoldout, boolValue: boolPtr(true), want: true},
		{name: "enabled_holdout_falsy", source: core.SourceHoldout, boolValue: boolPtr(false), want: false},
		{name: "enabled_bandit_truthy", source: core.SourceBandit, stringValue: strPtr("bold-copy"), want: true},
		{name: "enabled_bandit_falsy", source: core.SourceBandit, stringValue: strPtr(""), want: false},
		{name: "enabled_runtime_override_truthy", source: core.SourceRuntimeOverride, boolValue: boolPtr(true), want: true},
		{name: "enabled_runtime_override_falsy", source: core.SourceRuntimeOverride, numValue: numPtr(0), want: false},
		{name: "enabled_datafile_override_truthy", source: core.SourceDatafileOverride, stringValue: strPtr("forced-on"), want: true},
		{name: "enabled_datafile_override_falsy", source: core.SourceDatafileOverride, jsonValue: json.RawMessage(`null`), want: false},
		{name: "enabled_sticky_truthy", source: core.SourceSticky, boolValue: boolPtr(true), want: true},
		{name: "enabled_sticky_falsy", source: core.SourceSticky, boolValue: boolPtr(false), want: false},
		{name: "enabled_default_truthy_value", source: core.SourceDefault, boolValue: boolPtr(true), want: false},
		{name: "enabled_not_found_truthy_value", source: core.SourceNotFound, boolValue: boolPtr(true), want: false},
		{name: "enabled_disabled_truthy_value", source: core.SourceDisabled, boolValue: boolPtr(true), want: false},
		{name: "enabled_not_ready_truthy_value", source: core.SourceNotReady, boolValue: boolPtr(true), want: false},
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			switch {
			case tc.boolValue != nil:
				got = Flag[bool]{value: *tc.boolValue, source: tc.source}.IsEnabled()
			case tc.stringValue != nil:
				got = Flag[string]{value: *tc.stringValue, source: tc.source}.IsEnabled()
			case tc.numValue != nil:
				got = Flag[float64]{value: *tc.numValue, source: tc.source}.IsEnabled()
			default:
				got = Flag[json.RawMessage]{value: tc.jsonValue, source: tc.source}.IsEnabled()
			}
			if got != tc.want {
				t.Errorf("IsEnabled with source %q: want %v, got %v", tc.source, tc.want, got)
			}
		})
	}
}

// TestExistsMatchesTheSemanticsContract locks the five-row table. A DISABLED
// flag exists: the customer switched it off, which is not the same as it being
// absent. A not-ready SDK cannot know either way, so it answers false.
func TestExistsMatchesTheSemanticsContract(t *testing.T) {
	cases := []struct {
		source string
		want   bool
	}{
		{source: core.SourceRule, want: true},
		{source: core.SourceDefault, want: true},
		{source: core.SourceDisabled, want: true},
		{source: core.SourceNotFound, want: false},
		{source: core.SourceNotReady, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			got := Flag[bool]{value: true, source: tc.source}.Exists()
			if got != tc.want {
				t.Errorf("Exists with source %q: want %v, got %v", tc.source, tc.want, got)
			}
		})
	}

	t.Run("the zero Flag exists nowhere", func(t *testing.T) {
		var zero Flag[bool]
		if zero.Exists() {
			t.Error("a Flag with no source must not claim to exist")
		}
		if zero.IsEnabled() {
			t.Error("a Flag with no source must not claim to be enabled")
		}
	})
}

// TestTruthinessFollowsTheJavaScriptRule locks the twelve-row truthiness table.
//
// The rule is JavaScript's, not Go's and not Python's, because the reference
// engine is TypeScript and a helper that answers differently per language is not
// a portable helper. The rows that surprise people are the ones that matter: the
// STRING "false" is truthy, the STRING "0" is truthy, and an empty array or
// object is truthy.
func TestTruthinessFollowsTheJavaScriptRule(t *testing.T) {
	t.Run("falsy", func(t *testing.T) {
		if isTruthy(false) {
			t.Error("false is falsy")
		}
		if isTruthy(0.0) {
			t.Error("0 is falsy")
		}
		if isTruthy(-0.0) {
			t.Error("-0 is falsy")
		}
		if isTruthy("") {
			t.Error("the empty string is falsy")
		}
		if isTruthy(json.RawMessage(`null`)) {
			t.Error("an explicit JSON null is falsy")
		}
		if isTruthy(json.RawMessage(`false`)) {
			t.Error("a raw JSON false is falsy")
		}
		if isTruthy(json.RawMessage(`0`)) {
			t.Error("a raw JSON 0 is falsy")
		}
		if isTruthy(json.RawMessage(`-0`)) {
			t.Error("a raw JSON -0 is falsy")
		}
		if isTruthy(json.RawMessage(`0.0`)) {
			t.Error("a raw JSON 0.0 is falsy")
		}
		if isTruthy(json.RawMessage(`""`)) {
			t.Error("a raw JSON empty string is falsy")
		}
	})

	t.Run("truthy", func(t *testing.T) {
		if !isTruthy(true) {
			t.Error("true is truthy")
		}
		if !isTruthy(1.0) {
			t.Error("1 is truthy")
		}
		if !isTruthy(-1.0) {
			t.Error("negative numbers are truthy")
		}
		if !isTruthy("off") {
			t.Error("a non-empty string is truthy whatever it says")
		}
		if !isTruthy("false") {
			t.Error(`the STRING "false" is truthy: it is not parsed`)
		}
		if !isTruthy("0") {
			t.Error(`the STRING "0" is truthy: PHP would disagree`)
		}
		if !isTruthy(json.RawMessage(`"false"`)) {
			t.Error(`a raw JSON string "false" is truthy`)
		}
		if !isTruthy(json.RawMessage(`"0"`)) {
			t.Error(`a raw JSON string "0" is truthy`)
		}
		if !isTruthy(json.RawMessage(`[]`)) {
			t.Error("an empty array is truthy: Python would disagree")
		}
		if !isTruthy(json.RawMessage(`{}`)) {
			t.Error("an empty object is truthy: Python would disagree")
		}
		if !isTruthy(json.RawMessage(`{"a":1}`)) {
			t.Error("a populated object is truthy")
		}
	})

	t.Run("NaN is falsy", func(t *testing.T) {
		if isTruthy(math.NaN()) {
			t.Error("NaN is falsy in JavaScript and must be here too")
		}
		if isTruthy(float32(math.NaN())) {
			t.Error("a float32 NaN is falsy too")
		}
	})
}

// TestSourceSetIsPublicOnly proves the internal evaluator spellings cannot reach
// a caller: the six "decided" sources are the public names, and the banned
// spellings are not in the set at all.
func TestSourceSetIsPublicOnly(t *testing.T) {
	wantDecided := []string{
		"rule", "holdout", "bandit", "runtimeOverride", "datafileOverride", "sticky",
	}
	if len(decidedSources) != len(wantDecided) {
		t.Fatalf("want %d decided sources, got %d: %v", len(wantDecided), len(decidedSources), decidedSources)
	}
	for _, source := range wantDecided {
		if _, ok := decidedSources[source]; !ok {
			t.Errorf("%q must count as a real decision", source)
		}
	}

	banned := []string{"override", "runtime_override", "not_started", "not_initialized", "uninitialized", "error"}
	for _, source := range banned {
		if _, ok := decidedSources[source]; ok {
			t.Errorf("%q is an internal or invented spelling and must never reach a caller", source)
		}
		if (Flag[bool]{value: true, source: source}).IsEnabled() {
			t.Errorf("a flag carrying the banned source %q must not report enabled", source)
		}
	}
}

// TestFlagAccessorsAreSafeOnTheZeroValue covers the accessors' nil handling,
// which is what stops a caller from panicking on a flag that was never served.
func TestFlagAccessorsAreSafeOnTheZeroValue(t *testing.T) {
	var zero Flag[string]
	if zero.VariationKey() != "" {
		t.Error("VariationKey must be empty, not a nil dereference")
	}
	if zero.RuleID() != "" {
		t.Error("RuleID must be empty")
	}
	if zero.RuleType() != "" {
		t.Error("RuleType must be empty")
	}
	if zero.Reasons() == nil {
		t.Error("Reasons must be an empty slice, never nil")
	}
	if len(zero.Reasons()) != 0 {
		t.Error("Reasons on a zero flag must be empty")
	}
	if zero.Source() != "" {
		t.Error("Source must be empty")
	}
	if zero.FlagKey() != "" {
		t.Error("FlagKey must be empty")
	}
}
