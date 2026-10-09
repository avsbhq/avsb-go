package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// Targeted delivery rollouts (N1-1, production QA 2026-10-01).
//
// A TARGETED_DELIVERY rule with trafficAllocation 0.3 used to serve 100
// percent of its audience in every SDK, because the evaluator only applied
// the allocation to ab_test rules. These tests lock the fix: the same gate,
// hash input and float threshold as an ab_test rule. They mirror
// packages/avsb-core/src/evaluate.rollout.test.ts, and the exact counts were
// computed by the TypeScript reference (@avsbhq/core 1.6.0) over the same
// keys.

const (
	rolloutFlagKey = "rollout_feature"
	rolloutRuleID  = "rule_rollout"
	rolloutUsers   = 10000
)

// tdRuleJSON builds one targeted_delivery rule. allocation is a JSON literal:
// "0.3", "1" or "null".
func tdRuleJSON(id, allocation, variationID string) string {
	return fmt.Sprintf(`{"id":%q,"type":"targeted_delivery","enabled":true,"audienceIds":[],`+
		`"hashAttribute":"user.key","trafficAllocation":%s,`+
		`"variations":[{"variationId":%q,"percentage":1}]}`, id, allocation, variationID)
}

func rolloutDatafile(t *testing.T, rules, audiences string) *Datafile {
	t.Helper()
	raw := `{"version":2,"flags":[{"id":"flag_rollout","key":"rollout_feature","type":"boolean",` +
		`"enabled":true,"defaultVariationId":"var_off","variations":[` +
		`{"id":"var_off","key":"off","value":false},{"id":"var_on","key":"on","value":true},` +
		`{"id":"var_fallback","key":"fallback","value":true}],` +
		`"overrides":[],"rules":[` + rules + `]}],"audiences":[` + audiences + `]}`
	var df Datafile
	if err := json.Unmarshal([]byte(raw), &df); err != nil {
		t.Fatalf("parse rollout datafile: %v", err)
	}
	return &df
}

func rolloutUser(t *testing.T, key string, attrs map[string]string) EvalContext {
	t.Helper()
	ctx := EvalContext{"kind": json.RawMessage(`"user"`)}
	encoded, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("encode key: %v", err)
	}
	ctx["key"] = encoded
	for name, value := range attrs {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encode attribute: %v", err)
		}
		ctx[name] = raw
	}
	return ctx
}

func evaluateRollout(t *testing.T, df *Datafile, key string, reasons bool) FlagEvaluation {
	t.Helper()
	return EvaluateFlag(rolloutFlagKey, json.RawMessage(`false`), rolloutUser(t, key, nil), df,
		EvaluateOptions{IncludeReasons: reasons})
}

func servedRule(f FlagEvaluation) string {
	if f.RuleID == nil {
		return ""
	}
	return *f.RuleID
}

func rolloutServedCount(t *testing.T, df *Datafile, count int) int {
	t.Helper()
	served := 0
	for i := 0; i < count; i++ {
		if servedRule(evaluateRollout(t, df, fmt.Sprintf("user-%d", i), false)) == rolloutRuleID {
			served++
		}
	}
	return served
}

func rolloutBucket(key string) uint32 {
	return MurmurHash3(key+rolloutFlagKey+"__"+rolloutRuleID, 0) % 10000
}

func keyForRolloutBucket(t *testing.T, bucket uint32, prefix string) string {
	t.Helper()
	for i := 0; i < 200000; i++ {
		candidate := fmt.Sprintf("%s-%d", prefix, i)
		if rolloutBucket(candidate) == bucket {
			return candidate
		}
	}
	t.Fatalf("no key hashed to allocation bucket %d", bucket)
	return ""
}

func TestRollout_ThirtyPercentServesTheReferenceShareOf10000Users(t *testing.T) {
	df := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "0.3", "var_on"), "")
	// The TypeScript reference serves exactly 2,984 of these keys.
	if got := rolloutServedCount(t, df, rolloutUsers); got != 2984 {
		t.Fatalf("30 percent rollout served %d of %d, want 2984", got, rolloutUsers)
	}
}

func TestRollout_FiveAndFiftyPercentMatchTheReferenceCounts(t *testing.T) {
	cases := map[string]int{"0.05": 536, "0.5": 4939}
	for allocation, want := range cases {
		df := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, allocation, "var_on"), "")
		if got := rolloutServedCount(t, df, rolloutUsers); got != want {
			t.Errorf("allocation %s served %d, want %d", allocation, got, want)
		}
	}
}

func TestRollout_NullServesEveryoneExactlyAsBeforeTheFix(t *testing.T) {
	df := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "null", "var_on"), "")
	if got := rolloutServedCount(t, df, 2000); got != 2000 {
		t.Fatalf("null allocation served %d of 2000, want all", got)
	}
}

func TestRollout_FullServesEveryoneAndZeroServesNoOne(t *testing.T) {
	full := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "1", "var_on"), "")
	if got := rolloutServedCount(t, full, 2000); got != 2000 {
		t.Errorf("1.0 allocation served %d of 2000, want all", got)
	}
	zero := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "0", "var_on"), "")
	if got := rolloutServedCount(t, zero, 2000); got != 0 {
		t.Errorf("0 allocation served %d of 2000, want none", got)
	}
}

func TestRollout_GrowingKeepsEveryoneAlreadyInside(t *testing.T) {
	small := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "0.05", "var_on"), "")
	large := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "0.3", "var_on"), "")
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("user-%d", i)
		if servedRule(evaluateRollout(t, small, key, false)) != rolloutRuleID {
			continue
		}
		if servedRule(evaluateRollout(t, large, key, false)) != rolloutRuleID {
			t.Fatalf("%s was in the 5 percent rollout but not the 30 percent one", key)
		}
	}
}

func TestRollout_AbTestHashInputDecidesMembershipForBothRuleTypes(t *testing.T) {
	asTargeted := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "0.3", "var_on"), "")
	abRule := `{"id":"rule_rollout","type":"ab_test","enabled":true,"audienceIds":[],` +
		`"hashAttribute":"user.key","trafficAllocation":0.3,` +
		`"variations":[{"variationId":"var_on","percentage":1}]}`
	asAbTest := rolloutDatafile(t, abRule, "")
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("user-%d", i)
		inRollout := rolloutBucket(key) < 3000
		if got := servedRule(evaluateRollout(t, asTargeted, key, false)) == rolloutRuleID; got != inRollout {
			t.Fatalf("targeted delivery: %s served=%v, want %v", key, got, inRollout)
		}
		if got := servedRule(evaluateRollout(t, asAbTest, key, false)) == rolloutRuleID; got != inRollout {
			t.Fatalf("ab_test: %s served=%v, want %v", key, got, inRollout)
		}
	}
}

func TestRollout_FirstUsersBucketExactlyAsTheReference(t *testing.T) {
	want := []uint32{5278, 8498, 4278, 4172, 1088}
	for i, bucket := range want {
		if got := rolloutBucket(fmt.Sprintf("user-%d", i)); got != bucket {
			t.Errorf("user-%d: allocation bucket %d, want %d", i, got, bucket)
		}
	}
}

func TestRollout_ThresholdIsTheRawFloatSoBucket1234IsInside(t *testing.T) {
	df := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "0.12345", "var_on"), "")
	inside := keyForRolloutBucket(t, 1234, "td-in")
	outside := keyForRolloutBucket(t, 1235, "td-out")
	// The same keys the TypeScript search settles on.
	if inside != "td-in-3679" || outside != "td-out-19194" {
		t.Fatalf("searched keys %q and %q, want td-in-3679 and td-out-19194", inside, outside)
	}

	if got := servedRule(evaluateRollout(t, df, inside, false)); got != rolloutRuleID {
		t.Fatalf("bucket 1234 against threshold 1234.5: served %q, want %q", got, rolloutRuleID)
	}

	result := evaluateRollout(t, df, outside, true)
	if result.Source != SourceDefault {
		t.Fatalf("bucket 1235: source %q, want default", result.Source)
	}
	wantReasons := []string{
		`rule "rule_rollout" excluded by traffic allocation (hash 1235 >= threshold 1234.5)`,
		"no rules matched, returning default variation",
	}
	if !reflect.DeepEqual(result.Reasons, wantReasons) {
		t.Fatalf("reasons:\n got %q\nwant %q", result.Reasons, wantReasons)
	}
}

func TestRollout_VisitorOutsideFallsThroughToTheNextRule(t *testing.T) {
	rules := tdRuleJSON(rolloutRuleID, "0.3", "var_on") + "," + tdRuleJSON("rule_everyone", "null", "var_fallback")
	df := rolloutDatafile(t, rules, "")
	outside := keyForRolloutBucket(t, 9000, "td-next")

	result := evaluateRollout(t, df, outside, false)
	if servedRule(result) != "rule_everyone" {
		t.Fatalf("rule: got %q, want rule_everyone", servedRule(result))
	}
	if result.VariationKey == nil || *result.VariationKey != "fallback" {
		t.Fatalf("variationKey: got %v, want fallback", result.VariationKey)
	}
}

func TestRollout_AudienceMissStillSkipsBeforeTheRollout(t *testing.T) {
	paid := `{"id":"aud_paid","rules":{"id":"g_root","logic":"AND","conditions":[` +
		`{"id":"c_plan","type":"CUSTOM_ATTRIBUTE","attribute":"plan","operator":"equals",` +
		`"value":"paid","contextKind":"user"}]}}`
	rule := `{"id":"rule_rollout","type":"targeted_delivery","enabled":true,"audienceIds":["aud_paid"],` +
		`"hashAttribute":"user.key","trafficAllocation":1,` +
		`"variations":[{"variationId":"var_on","percentage":1}]}`
	df := rolloutDatafile(t, rule, paid)

	result := EvaluateFlag(rolloutFlagKey, json.RawMessage(`false`),
		rolloutUser(t, "free-user", map[string]string{"plan": "free"}), df,
		EvaluateOptions{IncludeReasons: true})
	if result.Source != SourceDefault {
		t.Fatalf("source %q, want default", result.Source)
	}
	if len(result.Reasons) == 0 || result.Reasons[0] != `rule "rule_rollout" skipped (audience not matched)` {
		t.Fatalf("first reason %q, want the audience miss", result.Reasons)
	}
}

func TestRollout_NoVariationsStaysASilentSkipWhateverTheAllocation(t *testing.T) {
	rule := `{"id":"rule_rollout","type":"targeted_delivery","enabled":true,"audienceIds":[],` +
		`"hashAttribute":"user.key","trafficAllocation":0,"variations":[]}`
	df := rolloutDatafile(t, rule, "")

	result := evaluateRollout(t, df, "u1", true)
	want := []string{"no rules matched, returning default variation"}
	if !reflect.DeepEqual(result.Reasons, want) {
		t.Fatalf("reasons %q, want %q", result.Reasons, want)
	}
}

func TestRollout_TargetedDeliveryFiresNoExposureRolloutOrNot(t *testing.T) {
	df := rolloutDatafile(t, tdRuleJSON(rolloutRuleID, "0.3", "var_on"), "")
	inside := keyForRolloutBucket(t, 10, "td-expo")
	exposures := 0

	result := EvaluateFlag(rolloutFlagKey, json.RawMessage(`false`), rolloutUser(t, inside, nil), df,
		EvaluateOptions{OnExposure: func(ExposureEvent) { exposures++ }})
	if result.RuleType == nil || *result.RuleType != RuleTypeTargetedDelivery {
		t.Fatalf("ruleType %v, want targeted_delivery", result.RuleType)
	}
	if exposures != 0 {
		t.Fatalf("targeted delivery fired %d exposures, want none", exposures)
	}
}
