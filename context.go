package avsb

import (
	"context"
	"encoding/json"

	"github.com/avsbhq/avsb-go/core"
)

// EvalContext is who the flag is being evaluated for: an identity, a kind, and
// any attributes your targeting rules read. It is an alias for
// core.EvalContext, so callers never import the core package directly.
//
// Build one with UserContext, OrgContext, SingleContext, or MultiContext:
//
//	evalCtx := avsb.UserContext("u_123", map[string]any{"plan": "pro"})
type EvalContext = core.EvalContext

// UserContext builds a "user" context with the supplied key and attributes.
// Pass nil attributes when there are none.
//
//	evalCtx := avsb.UserContext("u_123", map[string]any{"plan": "pro"})
func UserContext(key string, attrs map[string]any) EvalContext {
	return buildSingleContext("user", key, attrs)
}

// OrgContext builds an "organization" context. Use it when a rule buckets by
// account rather than by person, so every member of an account sees the same
// thing.
func OrgContext(key string, attrs map[string]any) EvalContext {
	return buildSingleContext("organization", key, attrs)
}

// SingleContext builds a context of any kind, for the kinds your project
// declares beyond user and organization ("device", "workspace", "tenant").
func SingleContext(kind, key string, attrs map[string]any) EvalContext {
	return buildSingleContext(kind, key, attrs)
}

// MultiContext combines single-kind contexts so one evaluation can target on
// several identities at once. A rule with hashAttribute "organization.key" then
// buckets by account while a rule with "user.key" buckets by person, from the
// same call.
//
//	evalCtx := avsb.MultiContext(
//	    avsb.UserContext("u_123", nil),
//	    avsb.OrgContext("acme-corp", map[string]any{"tier": "enterprise"}),
//	)
//
// Each argument must be a single-kind context; nesting a multi-context is
// ignored.
func MultiContext(sub ...EvalContext) EvalContext {
	m := EvalContext{}
	kindRaw, _ := json.Marshal("multi")
	m["kind"] = kindRaw

	for _, s := range sub {
		raw := toRawMessage(s)
		single := core.ParseSingle(raw)
		if single == nil || single.Kind == "" || single.Kind == "multi" {
			continue
		}
		m[single.Kind] = raw
	}
	return m
}

// BucketingKey returns the key the given kind buckets on, or "" when that kind
// is absent. Pass "" for kind to read the user key.
func BucketingKey(ctx EvalContext, kind string) string {
	return core.BucketingKey(ctx, kind)
}

// ContextRefs summarises a context as kind/key pairs, which is what the
// decision log records. Attributes are deliberately excluded.
func ContextRefs(ctx EvalContext) []core.ContextRef {
	return core.ContextRefs(ctx)
}

// evalContextKey is the private key an EvalContext is stored under on a Go
// context.Context. A struct{} type means no other package can collide with it.
type evalContextKey struct{}

// ContextWithEvalContext attaches an EvalContext to a Go context.
//
// This is the seam the HTTP middleware uses: it resolves the identity once per
// request, attaches it here, and then every handler downstream can call
// server.GetBoolFlag(r.Context(), "flag", false) with no plumbing. Before this
// existed the middleware attached a context that the evaluator then discarded,
// so it looked wired and silently bucketed every request as anonymous.
//
// An explicit WithContext option on the call still wins.
func ContextWithEvalContext(ctx context.Context, evalCtx EvalContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, evalContextKey{}, evalCtx)
}

// EvalContextFrom reads the EvalContext attached to a Go context, or nil when
// there is none.
func EvalContextFrom(ctx context.Context) EvalContext {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(evalContextKey{})
	if v == nil {
		return nil
	}
	evalCtx, _ := v.(EvalContext)
	return evalCtx
}

// buildSingleContext constructs the JSON map for one single-kind context.
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
			// An attribute that cannot be serialised (a channel, a func, a
			// cyclic struct) is skipped rather than taking the whole context
			// down. It cannot be sent on the wire either way.
			continue
		}
		ctx[k] = raw
	}
	return ctx
}

// toRawMessage marshals an EvalContext for embedding inside a multi-context.
func toRawMessage(ctx EvalContext) json.RawMessage {
	b, _ := json.Marshal(ctx)
	return b
}
