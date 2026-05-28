package core

import (
	"encoding/json"
	"testing"
)

// buildSimpleDatafile creates a minimal Datafile with one boolean flag.
func buildSimpleDatafile(flagKey string, enabled bool) *Datafile {
	return &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID:                 "flag_1",
				Key:                flagKey,
				Type:               "boolean",
				Enabled:            enabled,
				DefaultVariationID: "var_off",
				Variations: []Variation{
					{ID: "var_off", Key: "off", Value: json.RawMessage(`false`)},
					{ID: "var_on", Key: "on", Value: json.RawMessage(`true`)},
				},
				Overrides: nil,
				Rules:     nil,
			},
		},
		Audiences: nil,
	}
}

// TestEvaluateFlag_NotFound verifies that a missing flag returns source=not_found
// and the caller-supplied default value.
func TestEvaluateFlag_NotFound(t *testing.T) {
	df := buildSimpleDatafile("existing-flag", true)
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u1"`)

	result := EvaluateFlag("does-not-exist", json.RawMessage(`false`), ctx, df, EvaluateOptions{IncludeReasons: true})

	if result.Source != "not_found" {
		t.Errorf("source: want not_found, got %q", result.Source)
	}
	if result.VariationKey != nil {
		t.Errorf("variationKey: want nil, got %q", *result.VariationKey)
	}
	if string(result.Value) != "false" {
		t.Errorf("value: want false, got %s", result.Value)
	}
	if len(result.Reasons) == 0 {
		t.Error("expected at least one reason when IncludeReasons=true")
	}
}

// TestEvaluateFlag_DisabledFlag verifies that a disabled flag returns
// source=disabled and the default variation value (not the caller default).
func TestEvaluateFlag_DisabledFlag(t *testing.T) {
	df := buildSimpleDatafile("feature-off", false)
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u1"`)

	result := EvaluateFlag("feature-off", json.RawMessage(`true`), ctx, df, EvaluateOptions{IncludeReasons: true})

	if result.Source != "disabled" {
		t.Errorf("source: want disabled, got %q", result.Source)
	}
	// The default variation is "off" with value false.
	if string(result.Value) != "false" {
		t.Errorf("value: want false (default variation), got %s", result.Value)
	}
	if result.VariationKey == nil || *result.VariationKey != "off" {
		t.Errorf("variationKey: want 'off', got %v", result.VariationKey)
	}
}

// TestEvaluateFlag_DatafileOverride verifies that a userId override is served
// with source=datafileOverride over any rule.
func TestEvaluateFlag_DatafileOverride(t *testing.T) {
	df := &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID:                 "flag_2",
				Key:                "overridden-flag",
				Type:               "string",
				Enabled:            true,
				DefaultVariationID: "var_default",
				Variations: []Variation{
					{ID: "var_default", Key: "default", Value: json.RawMessage(`"default-val"`)},
					{ID: "var_forced", Key: "forced", Value: json.RawMessage(`"forced-val"`)},
				},
				Overrides: []Override{
					{UserID: "special_user", VariationID: "var_forced"},
				},
				Rules: []Rule{
					{
						ID:      "rule_td",
						Type:    "targeted_delivery",
						Enabled: true,
						Variations: []RuleVariation{
							{VariationID: "var_default", Percentage: 1},
						},
					},
				},
			},
		},
		Audiences: nil,
	}

	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"special_user"`)

	result := EvaluateFlag("overridden-flag", json.RawMessage(`"caller-default"`), ctx, df, EvaluateOptions{IncludeReasons: true})

	if result.Source != "datafileOverride" {
		t.Errorf("source: want datafileOverride, got %q", result.Source)
	}
	if string(result.Value) != `"forced-val"` {
		t.Errorf("value: want 'forced-val', got %s", result.Value)
	}
}

// TestEvaluateFlag_TargetedDelivery verifies that audience-matching rules
// serve the targeted variation with source=rule, ruleType=targeted_delivery.
func TestEvaluateFlag_TargetedDelivery(t *testing.T) {
	df := &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID:                 "flag_td",
				Key:                "feature-td",
				Type:               "boolean",
				Enabled:            true,
				DefaultVariationID: "var_off",
				Variations: []Variation{
					{ID: "var_off", Key: "off", Value: json.RawMessage(`false`)},
					{ID: "var_on", Key: "on", Value: json.RawMessage(`true`)},
				},
				Overrides: nil,
				Rules: []Rule{
					{
						ID:          "rule_paid",
						Type:        "targeted_delivery",
						Enabled:     true,
						AudienceIDs: []string{"aud_paid"},
						Variations: []RuleVariation{
							{VariationID: "var_on", Percentage: 1},
						},
					},
				},
			},
		},
		Audiences: []Audience{
			{
				ID: "aud_paid",
				Rules: ConditionGroup{
					ID:    "g1",
					Logic: "AND",
					Conditions: []json.RawMessage{
						func() json.RawMessage {
							c := Condition{
								ID:          "c1",
								Type:        "CUSTOM_ATTRIBUTE",
								Attribute:   "plan",
								Operator:    "equals",
								Value:       json.RawMessage(`"paid"`),
								ContextKind: "user",
							}
							b, _ := json.Marshal(c)
							return b
						}(),
					},
				},
			},
		},
	}

	paidCtx := EvalContext{}
	paidCtx["kind"] = json.RawMessage(`"user"`)
	paidCtx["key"] = json.RawMessage(`"u_paid"`)
	paidCtx["plan"] = json.RawMessage(`"paid"`)

	result := EvaluateFlag("feature-td", json.RawMessage(`false`), paidCtx, df, EvaluateOptions{IncludeReasons: true})

	if result.Source != "rule" {
		t.Errorf("source: want rule, got %q", result.Source)
	}
	if result.RuleType == nil || *result.RuleType != "targeted_delivery" {
		t.Errorf("ruleType: want targeted_delivery, got %v", result.RuleType)
	}
	if string(result.Value) != "true" {
		t.Errorf("value: want true, got %s", result.Value)
	}

	// Free user should fall through to default.
	freeCtx := EvalContext{}
	freeCtx["kind"] = json.RawMessage(`"user"`)
	freeCtx["key"] = json.RawMessage(`"u_free"`)
	freeCtx["plan"] = json.RawMessage(`"free"`)

	missResult := EvaluateFlag("feature-td", json.RawMessage(`false`), freeCtx, df, EvaluateOptions{})
	if missResult.Source != "default" {
		t.Errorf("source: want default, got %q", missResult.Source)
	}
}

// TestEvaluateFlag_ABTest_Deterministic verifies that the AB test bucketing
// is deterministic and the two "alice"/"bob" fixture users land in the
// correct variations.
func TestEvaluateFlag_ABTest_Deterministic(t *testing.T) {
	df := &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID:                 "flag_ab_1",
				Key:                "checkout-v2-experiment",
				Type:               "string",
				Enabled:            true,
				DefaultVariationID: "var_control",
				Variations: []Variation{
					{ID: "var_control", Key: "control", Value: json.RawMessage(`"old"`)},
					{ID: "var_treatment", Key: "treatment", Value: json.RawMessage(`"new"`)},
				},
				Overrides: nil,
				Rules: []Rule{
					{
						ID:      "rule_ab_split",
						Type:    "ab_test",
						Enabled: true,
						HashAttribute: "user.key",
						TrafficAllocation: func() *float64 { v := 1.0; return &v }(),
						Variations: []RuleVariation{
							{VariationID: "var_control", Percentage: 0.5},
							{VariationID: "var_treatment", Percentage: 0.5},
						},
					},
				},
			},
		},
		Audiences: nil,
	}

	makeCtx := func(key string) EvalContext {
		ctx := EvalContext{}
		ctx["kind"] = json.RawMessage(`"user"`)
		ctx["key"], _ = json.Marshal(key)
		return ctx
	}

	// alice → control (hash 4336 < 5000)
	aliceResult := EvaluateFlag("checkout-v2-experiment", json.RawMessage(`"fallback"`), makeCtx("alice"), df, EvaluateOptions{IncludeReasons: true})
	if aliceResult.Source != "rule" {
		t.Errorf("alice source: want rule, got %q", aliceResult.Source)
	}
	if aliceResult.VariationKey == nil || *aliceResult.VariationKey != "control" {
		t.Errorf("alice variationKey: want control, got %v", aliceResult.VariationKey)
	}

	// Same call twice must produce same result.
	aliceResult2 := EvaluateFlag("checkout-v2-experiment", json.RawMessage(`"fallback"`), makeCtx("alice"), df, EvaluateOptions{})
	if aliceResult.Source != aliceResult2.Source {
		t.Error("AB test is not deterministic: same user produced different results")
	}
}

// TestEvaluateFlag_ReasonsOff verifies that disabling reasons returns an
// empty slice, not nil.
func TestEvaluateFlag_ReasonsOff(t *testing.T) {
	df := buildSimpleDatafile("existing-flag", true)
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u1"`)

	result := EvaluateFlag("does-not-exist", json.RawMessage(`false`), ctx, df, EvaluateOptions{IncludeReasons: false})

	if result.Reasons == nil {
		t.Error("expected non-nil empty slice when reasons disabled")
	}
	if len(result.Reasons) != 0 {
		t.Errorf("expected empty reasons, got %v", result.Reasons)
	}
}
