package openfeature_test

import (
	"context"
	"testing"

	of "github.com/open-feature/go-sdk/openfeature"

	avsb "github.com/avsbhq/avsb-go"
	avsbof "github.com/avsbhq/avsb-go/openfeature"
)

// datafile carries a boolean flag on a full rollout, a string flag with no
// rules, and an organization-targeted flag so multi-kind mapping is provable.
const datafile = `{
  "version": 2, "sdkKey": "sdk_production_abc123", "publishedAt": "x",
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
      "variations": [{"id": "var_control", "key": "control", "value": "Sign up"}],
      "overrides": [], "rules": []
    },
    {
      "id": "flag_limit", "key": "upload-limit", "type": "number", "enabled": true,
      "defaultVariationId": "var_ten",
      "variations": [{"id": "var_ten", "key": "ten", "value": 10}],
      "overrides": [], "rules": []
    },
    {
      "id": "flag_config", "key": "checkout-config", "type": "json", "enabled": true,
      "defaultVariationId": "var_default",
      "variations": [{"id": "var_default", "key": "default", "value": {"retries": 3}}],
      "overrides": [], "rules": []
    },
    {
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
    }
  ],
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

func newProvider(t *testing.T) *avsbof.Provider {
	t.Helper()
	server, err := avsb.NewTestServer([]byte(datafile))
	if err != nil {
		t.Fatalf("NewTestServer: %v", err)
	}
	t.Cleanup(server.Close)
	return avsbof.NewProvider(server)
}

// TestProviderSatisfiesTheOpenFeatureInterface is the compile-time check that
// matters most: a provider that does not satisfy the interface fails at the
// customer's registration call, not ours.
func TestProviderSatisfiesTheOpenFeatureInterface(t *testing.T) {
	var provider of.FeatureProvider = newProvider(t)
	if provider.Metadata().Name != "avsb" {
		t.Errorf("metadata name: want avsb, got %q", provider.Metadata().Name)
	}
	if provider.Hooks() != nil && len(provider.Hooks()) != 0 {
		t.Errorf("this provider ships no hooks, got %v", provider.Hooks())
	}
}

// TestEveryEvaluationKind covers the five methods the OpenFeature client calls.
func TestEveryEvaluationKind(t *testing.T) {
	provider := newProvider(t)
	ctx := context.Background()
	flat := of.FlattenedContext{"targetingKey": "u_123"}

	t.Run("boolean", func(t *testing.T) {
		got := provider.BooleanEvaluation(ctx, "checkout-v2", false, flat)
		if !got.Value {
			t.Error("want true from the full-rollout rule")
		}
		if got.Reason != of.TargetingMatchReason {
			t.Errorf("a rule match is TARGETING_MATCH, got %q", got.Reason)
		}
		if got.Variant != "on" {
			t.Errorf("variant: want on, got %q", got.Variant)
		}
	})

	t.Run("string", func(t *testing.T) {
		got := provider.StringEvaluation(ctx, "hero-copy", "fallback", flat)
		if got.Value != "Sign up" {
			t.Errorf("want the default variation value, got %q", got.Value)
		}
		if got.Reason != of.DefaultReason {
			t.Errorf("a default decision is DEFAULT, got %q", got.Reason)
		}
	})

	t.Run("float", func(t *testing.T) {
		got := provider.FloatEvaluation(ctx, "upload-limit", 1, flat)
		if got.Value != 10 {
			t.Errorf("want 10, got %v", got.Value)
		}
	})

	t.Run("int truncates the JSON number", func(t *testing.T) {
		got := provider.IntEvaluation(ctx, "upload-limit", 1, flat)
		if got.Value != 10 {
			t.Errorf("want 10, got %d", got.Value)
		}
	})

	t.Run("object", func(t *testing.T) {
		got := provider.ObjectEvaluation(ctx, "checkout-config", map[string]any{}, flat)
		asMap, ok := got.Value.(map[string]any)
		if !ok {
			t.Fatalf("want a decoded object, got %T", got.Value)
		}
		if asMap["retries"] != float64(3) {
			t.Errorf("want retries 3, got %v", asMap["retries"])
		}
	})
}

// TestNotFoundAndNotReadyAreDifferentErrors is the OpenFeature half of B15.
// Clients branch on these codes, and reporting an uninitialised provider as
// FLAG_NOT_FOUND sends a customer looking for a flag they already created.
func TestNotFoundAndNotReadyAreDifferentErrors(t *testing.T) {
	provider := newProvider(t)
	ctx := context.Background()
	flat := of.FlattenedContext{"targetingKey": "u_123"}

	missing := provider.BooleanEvaluation(ctx, "never-created", true, flat)
	if missing.Reason != of.ErrorReason {
		t.Errorf("a missing key is an error reason, got %q", missing.Reason)
	}
	if missing.ResolutionError.Error() == "" {
		t.Error("a missing key must carry a resolution error")
	}
	if !missing.Value {
		t.Error("the caller's default comes back regardless")
	}

	// A server with no datafile at all.
	notReady, err := avsb.NewServer(avsb.ServerOptions{
		SDKKey:          "sdk_production_abc123",
		PollingDisabled: true,
		Events:          avsb.EventOptions{Disabled: true},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer notReady.Close()

	got := avsbof.NewProvider(notReady).BooleanEvaluation(ctx, "checkout-v2", true, flat)
	if got.Reason != of.ErrorReason {
		t.Errorf("a not-ready provider is an error reason, got %q", got.Reason)
	}
	if got.ResolutionError.Error() == missing.ResolutionError.Error() {
		t.Error("not_ready and not_found must not produce the same resolution error")
	}
	if !got.Value {
		t.Error("the caller's default comes back regardless")
	}
}

// TestMultiKindContextMapping is the targeting feature OpenFeature has no native
// shape for. Collapsing everything onto the user context silently loses
// account-level bucketing, so dotted attribute names build the sub-contexts.
func TestMultiKindContextMapping(t *testing.T) {
	provider := newProvider(t)
	ctx := context.Background()

	enterprise := of.FlattenedContext{
		"targetingKey":      "u_1",
		"plan":              "pro",
		"organization.key":  "acme-corp",
		"organization.tier": "enterprise",
	}
	if got := provider.BooleanEvaluation(ctx, "enterprise-feature", false, enterprise); !got.Value {
		t.Error("an enterprise organization must match the organization-keyed rule")
	}

	free := of.FlattenedContext{
		"targetingKey":      "u_2",
		"organization.key":  "tiny-co",
		"organization.tier": "free",
	}
	if got := provider.BooleanEvaluation(ctx, "enterprise-feature", false, free); got.Value {
		t.Error("a free organization must not match")
	}

	// With no dotted attributes the mapping stays a plain single context, so a
	// caller who never uses dots gets exactly what they expect.
	plain := of.FlattenedContext{"targetingKey": "u_3", "plan": "pro"}
	if got := provider.BooleanEvaluation(ctx, "checkout-v2", false, plain); !got.Value {
		t.Error("a plain single context must still evaluate")
	}
}

// TestEvaluationWithNoTargetingKey covers the anonymous case: it must not panic,
// and it must return something usable.
func TestEvaluationWithNoTargetingKey(t *testing.T) {
	provider := newProvider(t)
	got := provider.BooleanEvaluation(context.Background(), "checkout-v2", false, of.FlattenedContext{})
	if !got.Value {
		t.Error("a full rollout serves everyone, including an anonymous context")
	}
}
