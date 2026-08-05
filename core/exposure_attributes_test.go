package core

import (
	"encoding/json"
	"testing"
)

// TestBuildExposureAttributes covers the registered-attribute filter. Sending an
// unregistered key is not an error but it is a waste: the ingestion worker drops
// it, so a customer who never registers an attribute sees an empty column and
// has no way to tell whether the SDK sent it.
func TestBuildExposureAttributes(t *testing.T) {
	t.Run("no registry means send nothing", func(t *testing.T) {
		df := &Datafile{Version: 2}
		got := BuildExposureAttributes(makeUserCtx("u1", map[string]interface{}{"plan": "pro"}), df)
		if got != nil {
			t.Errorf("want nil, got %v", got)
		}
	})

	t.Run("an empty registry means send nothing", func(t *testing.T) {
		df := &Datafile{Version: 2, RegAttributes: json.RawMessage(`[]`)}
		got := BuildExposureAttributes(makeUserCtx("u1", map[string]interface{}{"plan": "pro"}), df)
		if got != nil {
			t.Errorf("want nil, got %v", got)
		}
	})

	t.Run("only registered keys survive", func(t *testing.T) {
		df := &Datafile{Version: 2, RegAttributes: json.RawMessage(`["plan","country"]`)}
		ctx := makeUserCtx("u1", map[string]interface{}{
			"plan":   "pro",
			"secret": "do-not-send",
		})
		got := BuildExposureAttributes(ctx, df)
		if got["plan"] != "pro" {
			t.Errorf("plan: want pro, got %q", got["plan"])
		}
		if _, present := got["secret"]; present {
			t.Error("an unregistered attribute must not be sent")
		}
		if len(got) != 1 {
			t.Errorf("want exactly one attribute, got %v", got)
		}
	})

	t.Run("the object registry shape is accepted too", func(t *testing.T) {
		df := &Datafile{
			Version:       2,
			RegAttributes: json.RawMessage(`[{"key":"plan","type":"string"}]`),
		}
		got := BuildExposureAttributes(makeUserCtx("u1", map[string]interface{}{"plan": "pro"}), df)
		if got["plan"] != "pro" {
			t.Errorf("plan: want pro, got %q", got["plan"])
		}
	})

	t.Run("values are stringified for a string map column", func(t *testing.T) {
		df := &Datafile{
			Version:       2,
			RegAttributes: json.RawMessage(`["seats","beta","tags","userId"]`),
		}
		ctx := makeUserCtx("u_str", map[string]interface{}{
			"seats": 12,
			"beta":  true,
			"tags":  []string{"a", "b"},
		})
		got := BuildExposureAttributes(ctx, df)
		if got["seats"] != "12" {
			t.Errorf("seats: want \"12\", got %q", got["seats"])
		}
		if got["beta"] != "true" {
			t.Errorf("beta: want \"true\", got %q", got["beta"])
		}
		if got["tags"] != `["a","b"]` {
			t.Errorf("tags: want the JSON text, got %q", got["tags"])
		}
		if got["userId"] != "u_str" {
			t.Errorf("userId: want the primary key, got %q", got["userId"])
		}
	})

	t.Run("a null attribute is skipped rather than sent as the text null", func(t *testing.T) {
		df := &Datafile{Version: 2, RegAttributes: json.RawMessage(`["plan"]`)}
		ctx := makeUserCtx("u1", map[string]interface{}{"plan": nil})
		if got := BuildExposureAttributes(ctx, df); got != nil {
			t.Errorf("want nil when nothing survives, got %v", got)
		}
	})

	t.Run("multi-context sub-contexts contribute dotted keys", func(t *testing.T) {
		df := &Datafile{
			Version:       2,
			RegAttributes: json.RawMessage(`["organization.key","organization.tier","userId"]`),
		}
		ctx := EvalContext{
			"kind": mustRaw("multi"),
			"user": mustRaw(map[string]interface{}{"kind": "user", "key": "u_member"}),
			"organization": mustRaw(map[string]interface{}{
				"kind": "organization", "key": "acme", "tier": "enterprise",
			}),
		}
		got := BuildExposureAttributes(ctx, df)
		if got["organization.key"] != "acme" {
			t.Errorf("organization.key: want acme, got %q", got["organization.key"])
		}
		if got["organization.tier"] != "enterprise" {
			t.Errorf("organization.tier: want enterprise, got %q", got["organization.tier"])
		}
		if got["userId"] != "u_member" {
			t.Errorf("userId: want u_member, got %q", got["userId"])
		}
	})

	t.Run("a nil datafile is safe", func(t *testing.T) {
		if got := BuildExposureAttributes(makeUserCtx("u1", nil), nil); got != nil {
			t.Errorf("want nil, got %v", got)
		}
	})
}

// TestContextRefs covers the decision log's context summary: kind/key pairs
// only, stable order, and never nil.
func TestContextRefs(t *testing.T) {
	t.Run("single context yields one ref", func(t *testing.T) {
		got := ContextRefs(makeUserCtx("u1", map[string]interface{}{"plan": "pro"}))
		if len(got) != 1 {
			t.Fatalf("want one ref, got %v", got)
		}
		if got[0].Kind != "user" || got[0].Key != "u1" {
			t.Errorf("want user/u1, got %+v", got[0])
		}
	})

	t.Run("multi context is sorted by kind so a log entry is diffable", func(t *testing.T) {
		ctx := EvalContext{
			"kind":         mustRaw("multi"),
			"user":         mustRaw(map[string]interface{}{"kind": "user", "key": "u1"}),
			"organization": mustRaw(map[string]interface{}{"kind": "organization", "key": "acme"}),
			"device":       mustRaw(map[string]interface{}{"kind": "device", "key": "d1"}),
		}
		// Run it repeatedly: Go randomises map iteration, so an unsorted
		// implementation passes once and fails on a later run.
		for i := 0; i < 20; i++ {
			got := ContextRefs(ctx)
			if len(got) != 3 {
				t.Fatalf("want three refs, got %v", got)
			}
			if got[0].Kind != "device" || got[1].Kind != "organization" || got[2].Kind != "user" {
				t.Fatalf("want kinds sorted device, organization, user; got %+v", got)
			}
		}
	})

	t.Run("attributes are never included", func(t *testing.T) {
		got := ContextRefs(makeUserCtx("u1", map[string]interface{}{"email": "a@b.com"}))
		if len(got) != 1 {
			t.Fatalf("want one ref, got %v", got)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != `[{"kind":"user","key":"u1"}]` {
			t.Errorf("a context summary carries kind and key only, got %s", encoded)
		}
	})
}
