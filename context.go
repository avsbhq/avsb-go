package avsb

import (
	"encoding/json"

	"github.com/avsbhq/avsb-go/core"
)

// EvalContext is a type alias for core.EvalContext so callers never need to
// import the core sub-package directly.
type EvalContext = core.EvalContext

// UserContext builds a single-kind "user" context with the supplied key and
// optional attribute map.
//
//	evalCtx := avsb.UserContext("u_1", map[string]any{"plan": "pro"})
func UserContext(key string, attrs map[string]any) EvalContext {
	return buildSingleContext("user", key, attrs)
}

// OrgContext builds a single-kind "organization" context.
func OrgContext(key string, attrs map[string]any) EvalContext {
	return buildSingleContext("organization", key, attrs)
}

// SingleContext builds a single-kind context for any context kind.
func SingleContext(kind, key string, attrs map[string]any) EvalContext {
	return buildSingleContext(kind, key, attrs)
}

// MultiContext builds a multi-context wrapper from the supplied sub-contexts.
// Each sub-context must be a single-kind context (not itself a multi-context).
//
//	evalCtx := avsb.MultiContext(
//	    avsb.UserContext("u_1", nil),
//	    avsb.OrgContext("acme-corp", map[string]any{"tier": "enterprise"}),
//	)
func MultiContext(sub ...EvalContext) EvalContext {
	m := EvalContext{}
	kindRaw, _ := json.Marshal("multi")
	m["kind"] = kindRaw

	for _, s := range sub {
		single := core.ParseSingle(toRawMessage(s))
		if single == nil {
			continue
		}
		m[single.Kind] = toRawMessage(s)
	}
	return m
}

// BucketingKey returns the primary bucketing key from an EvalContext.
// For a user context this is the user's key; for a multi-context the user
// sub-context key is preferred.
func BucketingKey(ctx EvalContext, kind string) string {
	return core.BucketingKey(ctx, kind)
}

// ContextSummary returns a short human-readable summary of the context for
// logging. Example: "user:u_1", "multi".
func ContextSummary(ctx EvalContext) string {
	return core.ContextSummary(ctx)
}

// buildSingleContext constructs the EvalContext JSON map for a single-kind
// context with the given kind, key, and optional attributes.
func buildSingleContext(kind, key string, attrs map[string]any) EvalContext {
	ctx := EvalContext{}
	if kind == "" {
		kind = "user"
	}
	kindRaw, _ := json.Marshal(kind)
	keyRaw, _ := json.Marshal(key)
	ctx["kind"] = kindRaw
	ctx["key"] = keyRaw

	for k, v := range attrs {
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		ctx[k] = raw
	}
	return ctx
}

// toRawMessage marshals an EvalContext back to json.RawMessage for embedding
// inside a multi-context.
func toRawMessage(ctx EvalContext) json.RawMessage {
	b, _ := json.Marshal(ctx)
	return b
}
