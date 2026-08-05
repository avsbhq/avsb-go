// Package avsb_test exercises the SDK the way a consumer does: through the
// exported API only, with no access to internals. It is the check that the public
// surface is usable as documented, separately from the internal tests that
// verify how it works.
package avsb_test

import (
	"context"
	"encoding/json"
	"testing"

	avsb "github.com/avsbhq/avsb-go"
	"github.com/avsbhq/avsb-go/core"
)

// quickstartDatafile is the datafile the README quickstart implies, plus a
// string, a JSON, a number, and a switched-off flag so every getter and every
// fallback source is reachable.
const quickstartDatafile = `{
  "version": 2,
  "projectType": "FEATURE_FLAG",
  "sdkKey": "sdk_production_abc123",
  "environmentKey": "production",
  "publishedAt": "2026-05-29T00:00:00.000Z",
  "collectEndpoint": "https://ingest.avsb.cloud",
  "flags": [
    {
      "id": "flag_checkout", "key": "checkout-v2", "type": "boolean", "enabled": true,
      "defaultVariationId": "var_off",
      "variations": [
        {"id": "var_off", "key": "off", "value": false},
        {"id": "var_on", "key": "on", "value": true}
      ],
      "overrides": [],
      "rules": [{
        "id": "rule_full", "type": "targeted_delivery", "enabled": true, "audienceIds": [],
        "hashAttribute": "user.key", "trafficAllocation": null,
        "variations": [{"variationId": "var_on", "percentage": 1}]
      }]
    },
    {
      "id": "flag_copy", "key": "hero-copy", "type": "string", "enabled": true,
      "defaultVariationId": "var_control",
      "variations": [
        {"id": "var_control", "key": "control", "value": "Sign up"},
        {"id": "var_bold", "key": "bold", "value": "Start free"}
      ],
      "overrides": [{"userId": "u_forced", "variationId": "var_bold"}],
      "rules": []
    },
    {
      "id": "flag_config", "key": "checkout-config", "type": "json", "enabled": true,
      "defaultVariationId": "var_default",
      "variations": [{"id": "var_default", "key": "default", "value": {"retries": 3, "timeoutMs": 250}}],
      "overrides": [],
      "rules": []
    },
    {
      "id": "flag_limit", "key": "upload-limit", "type": "number", "enabled": true,
      "defaultVariationId": "var_ten",
      "variations": [{"id": "var_ten", "key": "ten", "value": 10}],
      "overrides": [],
      "rules": []
    },
    {
      "id": "flag_off", "key": "switched-off", "type": "boolean", "enabled": false,
      "defaultVariationId": "var_off",
      "variations": [{"id": "var_off", "key": "off", "value": false}],
      "overrides": [],
      "rules": []
    }
  ],
  "audiences": []
}`

func newQuickstartServer(t *testing.T) *avsb.Server {
	t.Helper()
	server, err := avsb.NewTestServer([]byte(quickstartDatafile))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	t.Cleanup(server.Close)
	return server
}

// TestQuickstartPathWorks is the shape of the README's first example. If this
// fails, the documented first five minutes fail.
func TestQuickstartPathWorks(t *testing.T) {
	server := newQuickstartServer(t)
	ctx := context.Background()
	evalCtx := avsb.UserContext("u_123", map[string]any{"plan": "pro"})

	flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))

	if !flag.IsEnabled() {
		t.Errorf("the full-rollout rule serves on, so IsEnabled is true; reasons %v", flag.Reasons())
	}
	if !flag.Value() {
		t.Error("value: want true")
	}
	if flag.VariationKey() != "on" {
		t.Errorf("variationKey: want on, got %q", flag.VariationKey())
	}
	if flag.Source() != core.SourceRule {
		t.Errorf("source: want rule, got %q", flag.Source())
	}
	if flag.RuleID() != "rule_full" {
		t.Errorf("ruleId: want rule_full, got %q", flag.RuleID())
	}
	if flag.RuleType() != core.RuleTypeTargetedDelivery {
		t.Errorf("ruleType: want targeted_delivery, got %q", flag.RuleType())
	}
	if flag.FlagKey() != "checkout-v2" {
		t.Errorf("flagKey: got %q", flag.FlagKey())
	}
	if flag.EvaluatedAt().IsZero() {
		t.Error("evaluatedAt must be set")
	}
	if !flag.Exists() {
		t.Error("the flag is in the datafile, so Exists is true")
	}
}

// TestEveryTypedGetter covers the four getters against their matching flag types.
func TestEveryTypedGetter(t *testing.T) {
	server := newQuickstartServer(t)
	ctx := context.Background()
	evalCtx := avsb.UserContext("u_123", nil)

	t.Run("bool", func(t *testing.T) {
		flag := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))
		if !flag.Value() {
			t.Error("want true")
		}
	})

	t.Run("string", func(t *testing.T) {
		flag := server.GetStringFlag(ctx, "hero-copy", "fallback", avsb.WithContext(evalCtx))
		if flag.Value() != "Sign up" {
			t.Errorf("want the default variation's value, got %q", flag.Value())
		}
		if flag.Source() != core.SourceDefault {
			t.Errorf("no rules on this flag, so source is default; got %q", flag.Source())
		}
		if flag.IsEnabled() {
			t.Error("a default decision is not enabled, however truthy the value")
		}
	})

	t.Run("number", func(t *testing.T) {
		flag := server.GetNumberFlag(ctx, "upload-limit", 1, avsb.WithContext(evalCtx))
		if flag.Value() != 10 {
			t.Errorf("want 10, got %v", flag.Value())
		}
	})

	t.Run("json", func(t *testing.T) {
		flag := server.GetJSONFlag(ctx, "checkout-config",
			json.RawMessage(`{"retries":0}`), avsb.WithContext(evalCtx))

		var config struct {
			Retries   int `json:"retries"`
			TimeoutMs int `json:"timeoutMs"`
		}
		if err := json.Unmarshal(flag.Value(), &config); err != nil {
			t.Fatalf("the JSON value must unmarshal into a caller type: %v", err)
		}
		if config.Retries != 3 || config.TimeoutMs != 250 {
			t.Errorf("want retries 3 and timeoutMs 250, got %+v", config)
		}
	})
}

// TestSourcesAConsumerBranchesOn covers the fallback sources by name, because
// Source is part of the public contract and code branches on it.
func TestSourcesAConsumerBranchesOn(t *testing.T) {
	server := newQuickstartServer(t)
	ctx := context.Background()

	cases := []struct {
		name       string
		flagKey    string
		userKey    string
		wantSource string
		wantExists bool
	}{
		{
			name: "no rules matched", flagKey: "hero-copy", userKey: "u_normal",
			wantSource: core.SourceDefault, wantExists: true,
		},
		{
			name: "a switched-off flag still exists", flagKey: "switched-off", userKey: "u_normal",
			wantSource: core.SourceDisabled, wantExists: true,
		},
		{
			name: "a key that is not in the datafile", flagKey: "never-created", userKey: "u_normal",
			wantSource: core.SourceNotFound, wantExists: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flag := server.GetStringFlag(ctx, tc.flagKey, "caller-default",
				avsb.WithContext(avsb.UserContext(tc.userKey, nil)))
			if flag.Source() != tc.wantSource {
				t.Errorf("source: want %q, got %q (reasons %v)", tc.wantSource, flag.Source(), flag.Reasons())
			}
			if flag.Exists() != tc.wantExists {
				t.Errorf("exists: want %v, got %v", tc.wantExists, flag.Exists())
			}
			if flag.IsEnabled() {
				t.Errorf("%q is a fallback, not a decision, so IsEnabled must be false", tc.wantSource)
			}
		})
	}
}

// TestOverrideBeatsEverythingElse covers the highest-precedence datafile step,
// which is what makes a support override work immediately.
func TestOverrideBeatsEverythingElse(t *testing.T) {
	server := newQuickstartServer(t)
	flag := server.GetStringFlag(context.Background(), "hero-copy", "caller-default",
		avsb.WithContext(avsb.UserContext("u_forced", nil)))

	if flag.Value() != "Start free" {
		t.Errorf("want the overridden variation, got %q", flag.Value())
	}
	if flag.Source() != core.SourceDatafileOverride {
		t.Errorf("source: want datafileOverride, got %q", flag.Source())
	}
	if !flag.IsEnabled() {
		t.Error("an override is a real decision, so a truthy value is enabled")
	}
}

// TestMissingFlagReturnsTheCallerDefaultOfEveryType covers the safety posture: a
// default is required on every call and it comes back untouched, type intact.
func TestMissingFlagReturnsTheCallerDefaultOfEveryType(t *testing.T) {
	server := newQuickstartServer(t)
	ctx := context.Background()
	evalCtx := avsb.UserContext("u_1", nil)

	if got := server.GetBoolFlag(ctx, "nope", true, avsb.WithContext(evalCtx)); !got.Value() {
		t.Error("bool: want the caller's true back")
	}
	if got := server.GetStringFlag(ctx, "nope", "mine", avsb.WithContext(evalCtx)); got.Value() != "mine" {
		t.Errorf("string: want \"mine\", got %q", got.Value())
	}
	if got := server.GetNumberFlag(ctx, "nope", -1, avsb.WithContext(evalCtx)); got.Value() != -1 {
		t.Errorf("number: want -1, got %v", got.Value())
	}
	got := server.GetJSONFlag(ctx, "nope", json.RawMessage(`{"mine":true}`), avsb.WithContext(evalCtx))
	if string(got.Value()) != `{"mine":true}` {
		t.Errorf("json: want the caller's object back, got %s", got.Value())
	}
}

// TestMultiContextTargetsOnAccount covers the headline targeting feature from the
// consumer's side: one call, two identities, an organization-keyed rule, and the
// "is" alias the dashboard writes.
func TestMultiContextTargetsOnAccount(t *testing.T) {
	datafile := `{
	  "version": 2, "sdkKey": "sdk_production_abc123", "publishedAt": "x",
	  "collectEndpoint": "https://ingest.avsb.cloud",
	  "flags": [{
	    "id": "flag_ent", "key": "enterprise-feature", "type": "boolean", "enabled": true,
	    "defaultVariationId": "var_off",
	    "variations": [
	      {"id": "var_off", "key": "off", "value": false},
	      {"id": "var_on", "key": "on", "value": true}
	    ],
	    "overrides": [],
	    "rules": [{
	      "id": "rule_ent", "type": "targeted_delivery", "enabled": true,
	      "audienceIds": ["aud_ent"], "hashAttribute": "organization.key",
	      "trafficAllocation": null,
	      "variations": [{"variationId": "var_on", "percentage": 1}]
	    }]
	  }],
	  "audiences": [{
	    "id": "aud_ent",
	    "rules": {
	      "id": "g1", "logic": "AND",
	      "conditions": [{
	        "id": "c1", "type": "CUSTOM_ATTRIBUTE", "contextKind": "organization",
	        "attribute": "tier", "operator": "is", "value": "enterprise"
	      }]
	    }
	  }]
	}`
	server, err := avsb.NewTestServer([]byte(datafile))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	defer server.Close()

	ctx := context.Background()

	enterprise := avsb.MultiContext(
		avsb.UserContext("u_1", nil),
		avsb.OrgContext("acme-corp", map[string]any{"tier": "enterprise"}),
	)
	if flag := server.GetBoolFlag(ctx, "enterprise-feature", false, avsb.WithContext(enterprise)); !flag.Value() {
		t.Errorf("an enterprise organization matches; reasons %v", flag.Reasons())
	}

	startup := avsb.MultiContext(
		avsb.UserContext("u_2", nil),
		avsb.OrgContext("tiny-co", map[string]any{"tier": "free"}),
	)
	if flag := server.GetBoolFlag(ctx, "enterprise-feature", false, avsb.WithContext(startup)); flag.Value() {
		t.Error("a free organization must not match")
	}
}

// TestReasonsAreOffByDefaultAndOnPerCall covers the debugging switch. Reasons are
// built eagerly, so they are off unless asked for, and asking per call is the
// cheapest way to debug one flag in production.
func TestReasonsAreOffByDefaultAndOnPerCall(t *testing.T) {
	server, err := avsb.NewServer(avsb.ServerOptions{
		Bootstrap:       []byte(quickstartDatafile),
		PollingDisabled: true,
		Events:          avsb.EventOptions{Disabled: true},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	ctx := context.Background()
	evalCtx := avsb.UserContext("u_1", nil)

	off := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx))
	if off.Reasons() == nil {
		t.Error("Reasons is never nil, even when collection is off")
	}
	if len(off.Reasons()) != 0 {
		t.Errorf("reasons are off by default, got %v", off.Reasons())
	}

	on := server.GetBoolFlag(ctx, "checkout-v2", false, avsb.WithContext(evalCtx), avsb.WithReasons(true))
	if len(on.Reasons()) == 0 {
		t.Fatal("WithReasons(true) must populate the trail")
	}
	if on.Reasons()[0] != `rule "rule_full" matched (targeted_delivery), serving variation "on"` {
		t.Errorf("the reason names the rule and the variation, got %q", on.Reasons()[0])
	}
}

// TestStickyBucketServiceIsImplementableFromOutside is the check that the
// interface can be satisfied by a customer's own type, which is the real test of
// whether the signature is usable rather than only internally consistent.
func TestStickyBucketServiceIsImplementableFromOutside(t *testing.T) {
	var service avsb.StickyBucketService = newConsumerSticky()

	if got := service.Lookup("u_1", "checkout-v2"); got != nil {
		t.Errorf("a fresh store misses, got %+v", got)
	}
	service.Save("u_1", "checkout-v2", core.StickyAssignment{
		VariationID: "var_on", RuleID: "rule_1", RuleType: "ab_test", AssignedAt: 1780000000000,
	})
	got := service.Lookup("u_1", "checkout-v2")
	if got == nil || got.VariationID != "var_on" {
		t.Errorf("want the stored assignment back, got %+v", got)
	}
}

// consumerSticky is a StickyBucketService written the way a customer would write
// one, with no access to package internals.
type consumerSticky struct {
	stored map[string]core.StickyAssignment
}

func newConsumerSticky() *consumerSticky {
	return &consumerSticky{stored: map[string]core.StickyAssignment{}}
}

func (c *consumerSticky) Lookup(bucketingID, flagKey string) *core.StickyAssignment {
	a, ok := c.stored[bucketingID+"|"+flagKey]
	if !ok {
		return nil
	}
	return &a
}

func (c *consumerSticky) Save(bucketingID, flagKey string, assignment core.StickyAssignment) {
	c.stored[bucketingID+"|"+flagKey] = assignment
}

// TestDecisionLogIsUsableFromOutside covers the interface a customer implements
// to forward decisions to their own pipeline.
func TestDecisionLogIsUsableFromOutside(t *testing.T) {
	server := newQuickstartServer(t)

	var recorded []avsb.DecisionLogEntry
	server.SetDecisionLog(collectingLog(func(entry avsb.DecisionLogEntry) {
		recorded = append(recorded, entry)
	}))

	_ = server.GetBoolFlag(context.Background(), "checkout-v2", false,
		avsb.WithContext(avsb.UserContext("u_logged", nil)))

	if len(recorded) != 1 {
		t.Fatalf("want one decision recorded, got %d", len(recorded))
	}
	entry := recorded[0]
	if entry.FlagKey != "checkout-v2" || entry.Source != core.SourceRule {
		t.Errorf("want checkout-v2 / rule, got %q / %q", entry.FlagKey, entry.Source)
	}
	if len(entry.ContextSummary) != 1 || entry.ContextSummary[0].Key != "u_logged" {
		t.Errorf("want the identity in the summary, got %+v", entry.ContextSummary)
	}
	if entry.DurationMicros < 0 {
		t.Errorf("durationMicros must not be negative, got %d", entry.DurationMicros)
	}
}

// collectingLog adapts a function to the DecisionLog interface.
type collectingLog func(entry avsb.DecisionLogEntry)

func (c collectingLog) LogDecision(entry avsb.DecisionLogEntry) { c(entry) }
