package core

import (
	"encoding/json"
	"testing"
)

func TestIsMultiContext_DetectsMultiKind(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"multi"`)
	ctx["user"] = json.RawMessage(`{"kind":"user","key":"u1"}`)

	if !IsMultiContext(ctx) {
		t.Error("expected IsMultiContext to return true for kind=multi")
	}
}

func TestIsMultiContext_SingleContextReturnsFalse(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u1"`)

	if IsMultiContext(ctx) {
		t.Error("expected IsMultiContext to return false for single context")
	}
}

func TestContextByKind_SingleContextMatchesOwnKind(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u_alice"`)

	sub := ContextByKind(ctx, "user")
	if sub == nil {
		t.Fatal("expected non-nil SingleContext")
	}
	if sub.Key != "u_alice" {
		t.Errorf("key: want u_alice, got %q", sub.Key)
	}
}

func TestContextByKind_MultiContext_ReturnsSubContext(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"multi"`)
	ctx["user"] = json.RawMessage(`{"kind":"user","key":"u1"}`)
	ctx["organization"] = json.RawMessage(`{"kind":"organization","key":"acme","tier":"enterprise"}`)

	orgSub := ContextByKind(ctx, "organization")
	if orgSub == nil {
		t.Fatal("expected non-nil organization sub-context")
	}
	if orgSub.Key != "acme" {
		t.Errorf("org key: want acme, got %q", orgSub.Key)
	}
}

func TestResolveDottedAttribute_UserDotKey(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u_alice"`)

	raw := ResolveDottedAttribute(ctx, "user.key")
	if raw == nil {
		t.Fatal("expected non-nil for user.key")
	}
	got := RawToString(raw)
	if got != "u_alice" {
		t.Errorf("want u_alice, got %q", got)
	}
}

func TestResolveDottedAttribute_OrgTierInMultiContext(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"multi"`)
	ctx["user"] = json.RawMessage(`{"kind":"user","key":"u1"}`)
	ctx["organization"] = json.RawMessage(`{"kind":"organization","key":"acme","tier":"enterprise"}`)

	raw := ResolveDottedAttribute(ctx, "organization.tier")
	if raw == nil {
		t.Fatal("expected non-nil for organization.tier")
	}
	got := RawToString(raw)
	if got != "enterprise" {
		t.Errorf("want enterprise, got %q", got)
	}
}

func TestResolveDottedAttribute_MissingKindReturnsNil(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u1"`)

	raw := ResolveDottedAttribute(ctx, "organization.tier")
	if raw != nil {
		t.Errorf("expected nil for missing kind, got %s", raw)
	}
}

func TestLegacyAttribute_UserIdFallback(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u_legacy"`)

	got := LegacyAttribute(ctx, "userId")
	if got != "u_legacy" {
		t.Errorf("LegacyAttribute userId: want u_legacy, got %q", got)
	}
}

func TestBucketingKey_DefaultsToUser(t *testing.T) {
	ctx := EvalContext{}
	ctx["kind"] = json.RawMessage(`"user"`)
	ctx["key"] = json.RawMessage(`"u_bk"`)

	got := BucketingKey(ctx, "")
	if got != "u_bk" {
		t.Errorf("BucketingKey: want u_bk, got %q", got)
	}
}
