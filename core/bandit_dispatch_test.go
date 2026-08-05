package core

import (
	"encoding/json"
	"fmt"
	"testing"
)

// banditFlagDatafile builds the three-variation flag the bandit conformance
// fixtures use, with one bandit-typed rule.
func banditFlagDatafile(flagKey string, rule Rule, bandits []Bandit) *Datafile {
	return &Datafile{
		Version: 2,
		Flags: []FlagEntry{
			{
				ID:                 "flag_bandit_1",
				Key:                flagKey,
				Type:               "string",
				Enabled:            true,
				DefaultVariationID: "var_control",
				Variations: []Variation{
					{ID: "var_control", Key: "control", Value: json.RawMessage(`"control-copy"`)},
					{ID: "var_bold", Key: "bold", Value: json.RawMessage(`"bold-copy"`)},
					{ID: "var_plain", Key: "plain", Value: json.RawMessage(`"plain-copy"`)},
				},
				Rules: []Rule{rule},
			},
		},
		Bandits: bandits,
	}
}

// TestBanditRuleDispatches covers the whole bandit family: a rule of type
// "bandit" used to have no case in this evaluator at all, so a configured bandit
// behaved exactly like "no rules matched" and logged nothing. Every path below
// either serves a variation or names why it did not.
func TestBanditRuleDispatches(t *testing.T) {
	epsilonOff := float64Ptr(0)

	cases := []struct {
		name             string
		flagKey          string
		rule             Rule
		bandits          []Bandit
		wantValue        string
		wantVariationKey string
		wantSource       string
		wantRuleType     string
		wantReasons      []string
	}{
		{
			name:    "an inline epsilon-greedy config dispatches and exploits the best mean",
			flagKey: "bandit-inline-feature",
			rule: Rule{
				ID: "rule_bandit_eg", Type: "bandit", Enabled: true, HashAttribute: "user.key",
				Bandit: &BanditConfig{
					Algorithm:       "epsilon-greedy",
					ExplorationRate: epsilonOff,
					RewardMetric:    "checkout_completed",
					Actions: []BanditAction{
						{ID: "action_a_bold", VariationID: "var_bold"},
						{ID: "action_b_plain", VariationID: "var_plain"},
					},
				},
			},
			wantValue:        `"bold-copy"`,
			wantVariationKey: "bold",
			wantSource:       SourceBandit,
			wantRuleType:     RuleTypeBandit,
			wantReasons: []string{
				`rule "rule_bandit_eg" matched (bandit "rule_bandit_eg"), served action "action_a_bold" into variation "bold"`,
			},
		},
		{
			name:    "ucb1 cold start serves the lowest action id, not the first declared",
			flagKey: "bandit-inline-feature",
			rule: Rule{
				ID: "rule_bandit_ucb1", Type: "bandit", Enabled: true, HashAttribute: "user.key",
				Bandit: &BanditConfig{
					Algorithm:    "ucb1",
					RewardMetric: "checkout_completed",
					Actions: []BanditAction{
						{ID: "action_c_plain", VariationID: "var_plain"},
						{ID: "action_a_bold", VariationID: "var_bold"},
					},
				},
			},
			wantValue:        `"bold-copy"`,
			wantVariationKey: "bold",
			wantSource:       SourceBandit,
			wantRuleType:     RuleTypeBandit,
			wantReasons: []string{
				`rule "rule_bandit_ucb1" matched (bandit "rule_bandit_ucb1"), served action "action_a_bold" into variation "bold"`,
			},
		},
		{
			name:    "a shared bandit resolves through banditId and is attributed to its key",
			flagKey: "bandit-shared-feature",
			rule: Rule{
				ID: "rule_bandit_shared", Type: "bandit", Enabled: true, HashAttribute: "user.key",
				BanditID: "bandit_checkout",
			},
			bandits: []Bandit{
				{
					ID: "bandit_checkout", Key: "checkout-copy-bandit",
					Config: BanditConfig{
						Algorithm:    "ucb1",
						RewardMetric: "checkout_completed",
						Actions: []BanditAction{
							{ID: "action_c_plain", VariationID: "var_plain"},
							{ID: "action_a_bold", VariationID: "var_bold"},
						},
					},
				},
			},
			wantValue:        `"bold-copy"`,
			wantVariationKey: "bold",
			wantSource:       SourceBandit,
			wantRuleType:     RuleTypeBandit,
			wantReasons: []string{
				`rule "rule_bandit_shared" matched (bandit "checkout-copy-bandit"), served action "action_a_bold" into variation "bold"`,
			},
		},
		{
			name:    "a banditId with no matching entry is skipped loudly",
			flagKey: "bandit-shared-feature",
			rule: Rule{
				ID: "rule_bandit_ghost_id", Type: "bandit", Enabled: true, HashAttribute: "user.key",
				BanditID: "bandit_does_not_exist",
			},
			wantValue:        `"control-copy"`,
			wantVariationKey: "control",
			wantSource:       SourceDefault,
			wantReasons: []string{
				`rule "rule_bandit_ghost_id" skipped (banditId "bandit_does_not_exist" is not in datafile.bandits)`,
				"no rules matched, returning default variation",
			},
		},
		{
			name:    "a bandit rule with no config at all is skipped loudly",
			flagKey: "bandit-inline-feature",
			rule: Rule{
				ID: "rule_bandit_no_config", Type: "bandit", Enabled: true, HashAttribute: "user.key",
			},
			wantValue:        `"control-copy"`,
			wantVariationKey: "control",
			wantSource:       SourceDefault,
			wantReasons: []string{
				`rule "rule_bandit_no_config" skipped (bandit rule carries no bandit config)`,
				"no rules matched, returning default variation",
			},
		},
		{
			name:    "a bandit with an empty action set is skipped loudly",
			flagKey: "bandit-inline-feature",
			rule: Rule{
				ID: "rule_bandit_no_actions", Type: "bandit", Enabled: true, HashAttribute: "user.key",
				Bandit: &BanditConfig{
					Algorithm: "ucb1", RewardMetric: "checkout_completed", Actions: []BanditAction{},
				},
			},
			wantValue:        `"control-copy"`,
			wantVariationKey: "control",
			wantSource:       SourceDefault,
			wantReasons: []string{
				`rule "rule_bandit_no_actions" skipped (bandit "rule_bandit_no_actions" declares no actions)`,
				"no rules matched, returning default variation",
			},
		},
		{
			name:    "an action pointing at a deleted variation is skipped loudly",
			flagKey: "bandit-inline-feature",
			rule: Rule{
				ID: "rule_bandit_ghost_variation", Type: "bandit", Enabled: true, HashAttribute: "user.key",
				Bandit: &BanditConfig{
					Algorithm: "ucb1", RewardMetric: "checkout_completed",
					Actions: []BanditAction{{ID: "action_ghost", VariationID: "var_deleted"}},
				},
			},
			wantValue:        `"control-copy"`,
			wantVariationKey: "control",
			wantSource:       SourceDefault,
			wantReasons: []string{
				`rule "rule_bandit_ghost_variation" skipped (bandit "rule_bandit_ghost_variation" action "action_ghost" targets unknown variation "var_deleted")`,
				"no rules matched, returning default variation",
			},
		},
		{
			name:    "a disabled bandit rule is skipped before dispatch",
			flagKey: "bandit-inline-feature",
			rule: Rule{
				ID: "rule_bandit_disabled", Type: "bandit", Enabled: false, HashAttribute: "user.key",
				Bandit: &BanditConfig{
					Algorithm: "ucb1", RewardMetric: "checkout_completed",
					Actions: []BanditAction{{ID: "action_a_bold", VariationID: "var_bold"}},
				},
			},
			wantValue:        `"control-copy"`,
			wantVariationKey: "control",
			wantSource:       SourceDefault,
			wantReasons: []string{
				`rule "rule_bandit_disabled" skipped (disabled)`,
				"no rules matched, returning default variation",
			},
		},
		{
			name:    "an unknown algorithm is named rather than blamed on a missing model",
			flagKey: "bandit-inline-feature",
			rule: Rule{
				ID: "rule_bandit_future_algo", Type: "bandit", Enabled: true, HashAttribute: "user.key",
				Bandit: &BanditConfig{
					Algorithm: "quantum-annealing", RewardMetric: "checkout_completed",
					Actions: []BanditAction{{ID: "action_a_bold", VariationID: "var_bold"}},
				},
			},
			wantValue:        `"control-copy"`,
			wantVariationKey: "control",
			wantSource:       SourceDefault,
			wantReasons: []string{
				`rule "rule_bandit_future_algo" skipped (bandit "rule_bandit_future_algo" could not select an action: ` +
					`unknown bandit algorithm "quantum-annealing". Known algorithms: epsilon-greedy, thompson-sampling, ucb1)`,
				"no rules matched, returning default variation",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			df := banditFlagDatafile(tc.flagKey, tc.rule, tc.bandits)
			result := EvaluateFlag(tc.flagKey, json.RawMessage(`"caller-default"`),
				makeUserCtx("user_bandit", nil), df, EvaluateOptions{IncludeReasons: true})

			if string(result.Value) != tc.wantValue {
				t.Errorf("value: want %s, got %s", tc.wantValue, result.Value)
			}
			if result.VariationKey == nil || *result.VariationKey != tc.wantVariationKey {
				t.Errorf("variationKey: want %q, got %v", tc.wantVariationKey, result.VariationKey)
			}
			if result.Source != tc.wantSource {
				t.Errorf("source: want %q, got %q", tc.wantSource, result.Source)
			}
			if tc.wantRuleType == "" {
				if result.RuleType != nil {
					t.Errorf("ruleType: want nil, got %q", *result.RuleType)
				}
				if result.RuleID != nil {
					t.Errorf("ruleId: want nil, got %q", *result.RuleID)
				}
			} else {
				if result.RuleType == nil || *result.RuleType != tc.wantRuleType {
					t.Errorf("ruleType: want %q, got %v", tc.wantRuleType, result.RuleType)
				}
				if result.RuleID == nil || *result.RuleID != tc.rule.ID {
					t.Errorf("ruleId: want %q, got %v", tc.rule.ID, result.RuleID)
				}
			}
			assertReasons(t, tc.wantReasons, result.Reasons)
		})
	}
}

// TestBanditDispatchEmitsAnExposure covers the analytics half: a bandit decision
// is an impression and has to be recorded, carrying the bandit key and the
// action so the offline trainer can attribute reward.
func TestBanditDispatchEmitsAnExposure(t *testing.T) {
	rule := Rule{
		ID: "rule_bandit_eg", Type: "bandit", Enabled: true, HashAttribute: "user.key",
		Bandit: &BanditConfig{
			Algorithm: "ucb1", RewardMetric: "checkout_completed",
			Actions: []BanditAction{{ID: "action_a_bold", VariationID: "var_bold"}},
		},
	}
	df := banditFlagDatafile("bandit-inline-feature", rule, nil)

	var captured []ExposureEvent
	result := EvaluateFlag("bandit-inline-feature", json.RawMessage(`"caller-default"`),
		makeUserCtx("user_bandit_exposure", nil), df, EvaluateOptions{
			IncludeReasons: true,
			OnExposure:     func(e ExposureEvent) { captured = append(captured, e) },
		})

	if result.Source != SourceBandit {
		t.Fatalf("source: want bandit, got %q (reasons %v)", result.Source, result.Reasons)
	}
	if len(captured) != 1 {
		t.Fatalf("want exactly one exposure, got %d", len(captured))
	}
	event := captured[0]
	if event.VisitorID != "user_bandit_exposure" {
		t.Errorf("visitorId: want user_bandit_exposure, got %q", event.VisitorID)
	}
	if event.ExperimentID != "rule_bandit_eg" {
		t.Errorf("experimentId: want the rule id, got %q", event.ExperimentID)
	}
	if event.VariationID != "var_bold" {
		t.Errorf("variationId: want var_bold, got %q", event.VariationID)
	}
	if event.EventName != "flag_exposure" {
		t.Errorf("eventName: want flag_exposure, got %q", event.EventName)
	}
	if got := event.Properties["banditKey"]; got != "rule_bandit_eg" {
		t.Errorf("properties.banditKey: want rule_bandit_eg, got %v", got)
	}
	if got := event.Properties["actionId"]; got != "action_a_bold" {
		t.Errorf("properties.actionId: want action_a_bold, got %v", got)
	}
}

// TestBanditHashBucketUsesTheBucketingKey pins the request-bucket seed exactly.
//
// It used to be derived from a context SUMMARY string ("user:u_1"), which made
// every Go bandit decision differ from every other language's for the same
// visitor while still looking perfectly deterministic.
//
// The trick that makes this exact rather than statistical: with the exploration
// rate at 1 the explore branch always fires and picks sorted[bucket % N], so
// with 10000 zero-padded actions the SELECTED ACTION ID IS the bucket. Any other
// seed derivation names a different action.
func TestBanditHashBucketUsesTheBucketingKey(t *testing.T) {
	const banditKey = "probe"
	const userKey = "u_bandit_seed"

	actions := make([]BanditAction, maxBucket)
	for i := range actions {
		actions[i] = BanditAction{
			ID:          fmt.Sprintf("action_%04d", i),
			VariationID: "var_a",
		}
	}
	config := BanditConfig{
		Algorithm:       "epsilon-greedy",
		ExplorationRate: float64Ptr(1),
		RewardMetric:    "checkout_completed",
		Actions:         actions,
	}
	store := NewInMemoryBanditModelStore(map[string]*BanditModel{
		banditKey: {Version: "v1", Algorithm: "epsilon-greedy", PerAction: map[string]BanditPerActionStats{}},
	})

	wantBucket := MurmurHash3(banditKey+":"+userKey, 0) % maxBucket
	wantAction := fmt.Sprintf("action_%04d", wantBucket)

	got, err := EvaluateBandit(banditKey, config, makeUserCtx(userKey, nil), store)
	if err != nil {
		t.Fatalf("EvaluateBandit: %v", err)
	}
	if got == nil {
		t.Fatal("expected a decision from a store that has a model")
	}
	if got.Action.ID != wantAction {
		summaryBucket := MurmurHash3(banditKey+":user:"+userKey, 0) % maxBucket
		t.Errorf("action: want %q (bucket %d from banditKey+\":\"+bucketingKey), got %q. "+
			"A context-summary seed would have produced bucket %d",
			wantAction, wantBucket, got.Action.ID, summaryBucket)
	}
	if got.Entry.ModelVersion != "v1" {
		t.Errorf("modelVersion: want v1, got %q", got.Entry.ModelVersion)
	}
	if got.Entry.Source != SourceBandit || got.Entry.RuleType != RuleTypeBandit {
		t.Errorf("entry source/ruleType: want bandit/bandit, got %q/%q",
			got.Entry.Source, got.Entry.RuleType)
	}
}

// TestEvaluateBanditWithNoModelReturnsNilNotAnError separates the two failure
// shapes the evaluator reports differently: "no model in this store" is a
// nil decision, and a config it cannot serve is an error.
func TestEvaluateBanditWithNoModelReturnsNilNotAnError(t *testing.T) {
	config := BanditConfig{
		Algorithm:    "ucb1",
		RewardMetric: "checkout_completed",
		Actions:      []BanditAction{{ID: "action_a", VariationID: "var_a"}},
	}
	empty := NewInMemoryBanditModelStore(nil)

	got, err := EvaluateBandit("unseen", config, makeUserCtx("u", nil), empty)
	if err != nil {
		t.Errorf("a missing model is not an error, got %v", err)
	}
	if got != nil {
		t.Errorf("a missing model returns no decision, got %+v", got)
	}
}

// TestColdStartStoreServesEveryBanditKey covers the fallback the corpus locks:
// with no store, a bandit still runs its documented cold-start policy instead of
// the rule being skipped.
func TestColdStartStoreServesEveryBanditKey(t *testing.T) {
	store := coldStartBanditStore(BanditConfig{Algorithm: "ucb1"})

	for _, key := range []string{"anything", "another", ""} {
		model := store.GetModel(key)
		if model == nil {
			t.Fatalf("cold-start store returned no model for %q", key)
		}
		if model.Version != coldStartModelVersion {
			t.Errorf("version: want %q, got %q", coldStartModelVersion, model.Version)
		}
		if len(model.PerAction) != 0 {
			t.Errorf("a cold-start model has no observations, got %v", model.PerAction)
		}
	}

	// UpdateModel is a no-op rather than a panic: the model is derived.
	store.UpdateModel("anything", &BanditModel{Version: "should-not-stick"})
	if store.GetModel("anything").Version != coldStartModelVersion {
		t.Error("the cold-start model must not be replaceable")
	}
}
