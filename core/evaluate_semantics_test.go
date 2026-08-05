package core

import (
	"encoding/json"
	"fmt"
	"testing"
)

func float64Ptr(v float64) *float64 { return &v }

// abcFlagDatafile builds a three-variation flag with one ab_test rule, matching
// the shape of the boundary conformance fixtures.
func abcFlagDatafile(flagKey, ruleID string, allocation *float64, split []RuleVariation) *Datafile {
	return &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID:                 "flag_boundary",
				Key:                flagKey,
				Type:               "string",
				Enabled:            true,
				DefaultVariationID: "var_c",
				Variations: []Variation{
					{ID: "var_a", Key: "a", Value: json.RawMessage(`"A"`)},
					{ID: "var_b", Key: "b", Value: json.RawMessage(`"B"`)},
					{ID: "var_c", Key: "c", Value: json.RawMessage(`"C"`)},
				},
				Rules: []Rule{
					{
						ID:                ruleID,
						Type:              "ab_test",
						Enabled:           true,
						HashAttribute:     "user.key",
						TrafficAllocation: allocation,
						Variations:        split,
					},
				},
			},
		},
	}
}

// TestTrafficAllocationThresholdIsAFloatComparison is the truncation detector.
//
// It picks a real bucket, then sets the allocation so the threshold lands
// exactly half a bucket ABOVE it. A float comparison admits the context
// (bucket < bucket+0.5). An implementation that truncates the threshold to a
// uint32, which this SDK used to do, computes bucket >= bucket and EXCLUDES it.
//
// That is one context in ten thousand per rollout deciding differently in Go
// than in TypeScript, Python, Java, and .NET, for the same visitor and the same
// datafile. It is invisible in aggregate and impossible for a customer to
// diagnose, which is why it gets its own test rather than only a fixture.
func TestTrafficAllocationThresholdIsAFloatComparison(t *testing.T) {
	const flagKey = "boundary-allocation-fractional"
	const ruleID = "rule_alloc"
	const userKey = "alloc-detector"

	bucket := MurmurHash3(userKey+flagKey+"__"+ruleID, 0) % 10000
	// The threshold sits half a bucket above the context's own bucket.
	allocation := (float64(bucket) + 0.5) / 10000

	df := abcFlagDatafile(flagKey, ruleID, float64Ptr(allocation), []RuleVariation{
		{VariationID: "var_a", Percentage: 0.5},
		{VariationID: "var_b", Percentage: 0.5},
	})
	ctx := makeUserCtx(userKey, nil)

	result := EvaluateFlag(flagKey, json.RawMessage(`"caller-default"`), ctx, df,
		EvaluateOptions{IncludeReasons: true})

	if result.Source != SourceRule {
		t.Fatalf(
			"bucket %d against threshold %v must be INSIDE the allocation. Got source %q with reasons %v. "+
				"A truncated (integer) threshold is the classic cause",
			bucket, float64(bucket)+0.5, result.Source, result.Reasons)
	}
	if result.RuleType == nil || *result.RuleType != RuleTypeABTest {
		t.Errorf("ruleType: want ab_test, got %v", result.RuleType)
	}
}

// TestTrafficAllocationExclusionReasonCarriesTheFractionalThreshold locks the
// reason string, which is compared exactly by the conformance harness. A
// truncating implementation would print "threshold 1234" where the reference
// prints "threshold 1234.5".
func TestTrafficAllocationExclusionReasonCarriesTheFractionalThreshold(t *testing.T) {
	const flagKey = "boundary-allocation-fractional"
	const ruleID = "rule_alloc"
	const userKey = "alloc-exclusion-detector"

	bucket := MurmurHash3(userKey+flagKey+"__"+ruleID, 0) % 10000
	if bucket == 0 {
		t.Fatalf("this test needs a non-zero bucket to sit below; got 0 for key %q", userKey)
	}
	// Threshold half a bucket BELOW this context's bucket, so it is excluded.
	allocation := (float64(bucket) - 0.5) / 10000

	df := abcFlagDatafile(flagKey, ruleID, float64Ptr(allocation), []RuleVariation{
		{VariationID: "var_a", Percentage: 1},
	})
	ctx := makeUserCtx(userKey, nil)

	result := EvaluateFlag(flagKey, json.RawMessage(`"caller-default"`), ctx, df,
		EvaluateOptions{IncludeReasons: true})

	if result.Source != SourceDefault {
		t.Fatalf("bucket %d must be OUTSIDE the allocation; got source %q", bucket, result.Source)
	}
	wantReason := fmt.Sprintf(
		"rule %q excluded by traffic allocation (hash %d >= threshold %s)",
		ruleID, bucket, formatNumberLikeJS(allocation*10000))
	if len(result.Reasons) == 0 || result.Reasons[0] != wantReason {
		t.Errorf("reason[0]:\n  want %q\n  got  %v", wantReason, result.Reasons)
	}
	if want := "no rules matched, returning default variation"; result.Reasons[len(result.Reasons)-1] != want {
		t.Errorf("final reason: want %q, got %q", want, result.Reasons[len(result.Reasons)-1])
	}
}

// TestSplitBoundaryReasonUsesTheCumulativeFloat covers the second place the
// number format shows up: the cumulative split boundary in a bucketing reason.
// A 0.12345 first slice must read "1234.5", not "1234".
func TestSplitBoundaryReasonUsesTheCumulativeFloat(t *testing.T) {
	const flagKey = "boundary-split-fractional"
	const ruleID = "rule_split"

	df := abcFlagDatafile(flagKey, ruleID, float64Ptr(1), []RuleVariation{
		{VariationID: "var_a", Percentage: 0.12345},
		{VariationID: "var_b", Percentage: 0.5},
		{VariationID: "var_c", Percentage: 0.37655},
	})

	// Find a context that lands in the FIRST slice, so the reason carries the
	// 1234.5 boundary rather than a later one.
	userKey := ""
	for i := 0; i < 100000; i++ {
		candidate := fmt.Sprintf("split-probe-%d", i)
		if MurmurHash3(candidate+flagKey, 0)%10000 < 1234 {
			userKey = candidate
			break
		}
	}
	if userKey == "" {
		t.Fatal("could not find a context inside the first 12.345% slice")
	}

	result := EvaluateFlag(flagKey, json.RawMessage(`"caller-default"`),
		makeUserCtx(userKey, nil), df, EvaluateOptions{IncludeReasons: true})

	if result.VariationKey == nil || *result.VariationKey != "a" {
		t.Fatalf("variationKey: want a, got %v (reasons %v)", result.VariationKey, result.Reasons)
	}
	hash := MurmurHash3(userKey+flagKey, 0) % 10000
	wantReason := fmt.Sprintf(
		"rule %q matched (ab_test), bucketed into variation %q (hash %d/1234.5)",
		ruleID, "a", hash)
	if len(result.Reasons) != 1 || result.Reasons[0] != wantReason {
		t.Errorf("reason:\n  want %q\n  got  %v", wantReason, result.Reasons)
	}
}

// TestUnsupportedRuleTypeIsNamedNotDropped covers the silent-drop ban: a rule
// type this SDK version has never heard of must appear in the decision trail,
// because the reason is the only clue a newer dashboard published something the
// deployed SDK cannot serve.
func TestUnsupportedRuleTypeIsNamedNotDropped(t *testing.T) {
	df := &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID: "flag_future", Key: "edge-unknown-rule-type-feature", Type: "boolean",
				Enabled: true, DefaultVariationID: "var_off",
				Variations: []Variation{
					{ID: "var_off", Key: "off", Value: json.RawMessage(`false`)},
					{ID: "var_on", Key: "on", Value: json.RawMessage(`true`)},
				},
				Rules: []Rule{
					{
						ID: "rule_from_the_future", Type: "multi_armed_rollout", Enabled: true,
						HashAttribute: "user.key",
						Variations:    []RuleVariation{{VariationID: "var_on", Percentage: 1}},
					},
					{
						ID: "rule_known_fallback", Type: "targeted_delivery", Enabled: true,
						HashAttribute: "user.key",
						Variations:    []RuleVariation{{VariationID: "var_on", Percentage: 1}},
					},
				},
			},
		},
	}

	result := EvaluateFlag("edge-unknown-rule-type-feature", json.RawMessage(`false`),
		makeUserCtx("user_unknown_rule_type", nil), df, EvaluateOptions{IncludeReasons: true})

	want := []string{
		`rule "rule_from_the_future" skipped (unsupported rule type "multi_armed_rollout")`,
		`rule "rule_known_fallback" matched (targeted_delivery), serving variation "on"`,
	}
	assertReasons(t, want, result.Reasons)
	if result.Source != SourceRule {
		t.Errorf("source: want rule, got %q", result.Source)
	}
}

// TestNilDatafileIsNotReadyNotNotFound covers the core guard behind B15. Asking
// a datafile-less evaluator about a key cannot answer "that key does not exist",
// because it has nothing to look in.
func TestNilDatafileIsNotReadyNotNotFound(t *testing.T) {
	result := EvaluateFlag("anything", json.RawMessage(`"fallback"`),
		makeUserCtx("u1", nil), nil, EvaluateOptions{IncludeReasons: true})

	if result.Source != SourceNotReady {
		t.Errorf("source: want not_ready, got %q", result.Source)
	}
	if string(result.Value) != `"fallback"` {
		t.Errorf("value: want the caller's default, got %s", result.Value)
	}
	if result.VariationKey != nil {
		t.Errorf("variationKey: want nil, got %q", *result.VariationKey)
	}
	if len(result.Reasons) == 0 {
		t.Error("a not_ready evaluation must say so in its reasons")
	}
}

// TestReasonsAreNeverNil covers the small contract that saves every caller a
// nil check: reasons off returns an empty slice, not nil.
func TestReasonsAreNeverNil(t *testing.T) {
	df := abcFlagDatafile("k", "r", float64Ptr(1), []RuleVariation{{VariationID: "var_a", Percentage: 1}})

	on := EvaluateFlag("k", json.RawMessage(`"d"`), makeUserCtx("u", nil), df,
		EvaluateOptions{IncludeReasons: true})
	if len(on.Reasons) == 0 {
		t.Error("reasons on: expected at least one reason")
	}

	off := EvaluateFlag("k", json.RawMessage(`"d"`), makeUserCtx("u", nil), df,
		EvaluateOptions{IncludeReasons: false})
	if off.Reasons == nil {
		t.Error("reasons off: want an empty slice, got nil")
	}
	if len(off.Reasons) != 0 {
		t.Errorf("reasons off: want empty, got %v", off.Reasons)
	}
}

// TestHoldoutExposureUsesTheHoldoutBucketingKey covers the visitor-id choice on
// a holdout exposure. An org-keyed holdout buckets by organization, so its
// exposure must be attributed to the organization: attributing it to whichever
// member made the request splits one org's traffic across every employee.
func TestHoldoutExposureUsesTheHoldoutBucketingKey(t *testing.T) {
	df := &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID: "flag_org_holdout", Key: "org-held-out", Type: "boolean",
				Enabled: true, DefaultVariationID: "var_off",
				Variations: []Variation{
					{ID: "var_off", Key: "off", Value: json.RawMessage(`false`)},
				},
			},
		},
		Holdouts: []Holdout{
			{
				ID: "holdout_org", Key: "org-holdout", TrafficAllocation: 1,
				FlagIDs: []string{"flag_org_holdout"}, HashAttribute: "organization.key",
				DefaultVariationByFlag: map[string]string{"flag_org_holdout": "var_off"},
			},
		},
	}

	ctx := EvalContext{
		"kind":         mustRaw("multi"),
		"user":         mustRaw(map[string]interface{}{"kind": "user", "key": "u_member"}),
		"organization": mustRaw(map[string]interface{}{"kind": "organization", "key": "acme"}),
	}

	var captured []ExposureEvent
	result := EvaluateFlag("org-held-out", json.RawMessage(`false`), ctx, df, EvaluateOptions{
		IncludeReasons: true,
		OnExposure:     func(e ExposureEvent) { captured = append(captured, e) },
	})

	if result.Source != SourceHoldout {
		t.Fatalf("source: want holdout, got %q (reasons %v)", result.Source, result.Reasons)
	}
	if len(captured) != 1 {
		t.Fatalf("want exactly one exposure, got %d", len(captured))
	}
	if captured[0].VisitorID != "acme" {
		t.Errorf("visitorId: want the organization key \"acme\", got %q", captured[0].VisitorID)
	}
	if captured[0].EventName != "flag_holdout_exposure" {
		t.Errorf("eventName: want flag_holdout_exposure, got %q", captured[0].EventName)
	}
	if captured[0].ExperimentID != "holdout_org" {
		t.Errorf("experimentId: want the holdout id, got %q", captured[0].ExperimentID)
	}
	if captured[0].Revenue != nil {
		t.Error("an exposure carries no revenue")
	}
}

// TestHoldoutThresholdIsAFloatComparison covers the same truncation class in the
// holdout path, which the review's file references did not name but which had
// the identical uint32 cast.
func TestHoldoutThresholdIsAFloatComparison(t *testing.T) {
	const holdoutKey = "float-threshold-holdout"
	const userKey = "holdout-detector"

	bucket := MurmurHash3(holdoutKey+":"+userKey, 0) % 10000
	allocation := (float64(bucket) + 0.5) / 10000

	df := &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID: "flag_h", Key: "held-out-feature", Type: "boolean",
				Enabled: true, DefaultVariationID: "var_off",
				Variations: []Variation{{ID: "var_off", Key: "off", Value: json.RawMessage(`false`)}},
			},
		},
		Holdouts: []Holdout{
			{
				ID: "holdout_float", Key: holdoutKey, TrafficAllocation: allocation,
				FlagIDs: []string{"flag_h"}, HashAttribute: "user.key",
				DefaultVariationByFlag: map[string]string{"flag_h": "var_off"},
			},
		},
	}

	got := EvaluateHoldout(makeUserCtx(userKey, nil), "flag_h", df)
	if !got.HeldOut {
		t.Fatalf("bucket %d against threshold %v must be inside the holdout window",
			bucket, float64(bucket)+0.5)
	}
	if got.BucketingKey != userKey {
		t.Errorf("bucketingKey: want %q, got %q", userKey, got.BucketingKey)
	}
}

// assertReasons compares a reason trail exactly and in order, which is what the
// conformance harness does.
func assertReasons(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("reasons: want %d entries %v, got %d entries %v", len(want), want, len(got), got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("reasons[%d]:\n  want %q\n  got  %q", i, want[i], got[i])
		}
	}
}
