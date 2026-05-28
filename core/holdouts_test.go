package core

import (
	"encoding/json"
	"testing"
)

// buildHoldoutDatafile creates a minimal Datafile with a holdout.
func buildHoldoutDatafile(trafficAllocation float64) *Datafile {
	return &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID:                 "flag_holdout_target",
				Key:                "feature-with-holdout",
				Type:               "boolean",
				Enabled:            true,
				DefaultVariationID: "var_off",
				Variations: []Variation{
					{ID: "var_off", Key: "off", Value: json.RawMessage(`false`)},
					{ID: "var_on", Key: "on", Value: json.RawMessage(`true`)},
				},
				Rules: []Rule{
					{
						ID:      "rule_full_rollout",
						Type:    "targeted_delivery",
						Enabled: true,
						Variations: []RuleVariation{
							{VariationID: "var_on", Percentage: 1},
						},
					},
				},
			},
		},
		Holdouts: []Holdout{
			{
				ID:                "holdout_q2",
				Key:               "q2-2026-holdout",
				TrafficAllocation: trafficAllocation,
				FlagIDs:           []string{"flag_holdout_target"},
				HashAttribute:     "user.key",
				DefaultVariationByFlag: map[string]string{
					"flag_holdout_target": "var_off",
				},
			},
		},
	}
}

// TestEvaluateHoldout_HeldOut verifies that a user with key "holdout_subject_1"
// is held out by a 99.9% holdout — matching the conformance fixture.
func TestEvaluateHoldout_HeldOut(t *testing.T) {
	df := buildHoldoutDatafile(0.999)
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"holdout_subject_1"`)

	result := EvaluateHoldout(ctx, "flag_holdout_target", df)
	if !result.HeldOut {
		t.Error("expected user 'holdout_subject_1' to be held out by 99.9% holdout")
	}
	if result.Holdout == nil {
		t.Fatal("expected non-nil Holdout")
	}
	if result.Holdout.Key != "q2-2026-holdout" {
		t.Errorf("holdout key: want q2-2026-holdout, got %q", result.Holdout.Key)
	}
	if result.VariationID != "var_off" {
		t.Errorf("variationID: want var_off, got %q", result.VariationID)
	}
}

// TestEvaluateHoldout_ZeroAllocation verifies that a holdout with 0%
// allocation never holds out any user.
func TestEvaluateHoldout_ZeroAllocation(t *testing.T) {
	df := buildHoldoutDatafile(0)
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"holdout_subject_1"`)

	result := EvaluateHoldout(ctx, "flag_holdout_target", df)
	if result.HeldOut {
		t.Error("expected no holdout for 0% traffic allocation")
	}
}

// TestEvaluateHoldout_FlagNotParticipating verifies that a flag not listed
// in the holdout's flagIds is never held out.
func TestEvaluateHoldout_FlagNotParticipating(t *testing.T) {
	df := buildHoldoutDatafile(0.999)
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"holdout_subject_1"`)

	// Use a flag ID not listed in the holdout.
	result := EvaluateHoldout(ctx, "some-other-flag-id", df)
	if result.HeldOut {
		t.Error("expected no holdout for a flag not listed in holdout.flagIds")
	}
}

// TestEvaluateHoldout_EmptyDatafileHoldouts verifies no panic on empty
// holdouts slice.
func TestEvaluateHoldout_EmptyDatafileHoldouts(t *testing.T) {
	df := &Datafile{Version: 2}
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u1"`)

	result := EvaluateHoldout(ctx, "flag_holdout_target", df)
	if result.HeldOut {
		t.Error("expected no holdout for datafile with no holdouts")
	}
}
