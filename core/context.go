package core

import (
	"encoding/json"
	"strings"
)

// reservedContextKeys mirrors the TypeScript set of keys excluded from
// attribute lookup so we never collapse identity fields into targeting
// attributes.
var reservedContextKeys = map[string]struct{}{
	"kind":  {},
	"key":   {},
	"name":  {},
	"_meta": {},
}

// SingleContext is the parsed form of one kind:key:attrs envelope.
type SingleContext struct {
	Kind       string
	Key        string
	Name       string
	Attributes map[string]json.RawMessage
}

// IsMultiContext reports whether ctx is a multi-context wrapper
// (i.e. kind == "multi").
func IsMultiContext(ctx EvalContext) bool {
	raw, ok := ctx["kind"]
	if !ok {
		return false
	}
	var kind string
	if err := json.Unmarshal(raw, &kind); err != nil {
		return false
	}
	return kind == "multi"
}

// ParseSingle reads a SingleContext from raw JSON bytes.
// Returns nil when the bytes do not decode into a SingleContext shape.
func ParseSingle(raw json.RawMessage) *SingleContext {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	sc := &SingleContext{Attributes: map[string]json.RawMessage{}}
	if v, ok := m["kind"]; ok {
		_ = json.Unmarshal(v, &sc.Kind)
	}
	if v, ok := m["key"]; ok {
		_ = json.Unmarshal(v, &sc.Key)
	}
	if v, ok := m["name"]; ok {
		_ = json.Unmarshal(v, &sc.Name)
	}
	for k, v := range m {
		if _, reserved := reservedContextKeys[k]; reserved {
			continue
		}
		sc.Attributes[k] = v
	}
	return sc
}

// AsSingle interprets the whole EvalContext as a single-context object.
func (ctx EvalContext) AsSingle() *SingleContext {
	b, err := json.Marshal(ctx)
	if err != nil {
		return nil
	}
	return ParseSingle(b)
}

// ContextByKind returns the sub-context for the supplied kind. For a
// single-context, returns the receiver when kinds match. For a multi-context,
// dereferences the sibling sub-context.
func ContextByKind(ctx EvalContext, kind string) *SingleContext {
	if IsMultiContext(ctx) {
		raw, ok := ctx[kind]
		if !ok {
			return nil
		}
		sub := ParseSingle(raw)
		if sub == nil || sub.Kind != kind {
			return nil
		}
		return sub
	}
	single := ctx.AsSingle()
	if single == nil || single.Kind != kind {
		return nil
	}
	return single
}

// BucketingKey returns the canonical bucketing identifier for the given kind,
// defaulting to "user". Returns the empty string when no key is available.
func BucketingKey(ctx EvalContext, kind string) string {
	if kind == "" {
		kind = "user"
	}
	sub := ContextByKind(ctx, kind)
	if sub == nil {
		return ""
	}
	return sub.Key
}

// PrimaryContext returns the primary single-context for an EvalContext.
// For a multi-context the 'user' sub-context is preferred; otherwise the
// first non-meta entry is returned. For a single-context it is returned
// directly.
func PrimaryContext(ctx EvalContext) *SingleContext {
	if !IsMultiContext(ctx) {
		return ctx.AsSingle()
	}
	// prefer user
	if sub := ContextByKind(ctx, "user"); sub != nil {
		return sub
	}
	// fall back to first non-meta kind
	for k, v := range ctx {
		if k == "kind" {
			continue
		}
		if sub := ParseSingle(v); sub != nil {
			return sub
		}
	}
	return nil
}

// ResolveDottedAttribute resolves a dotted path against an EvalContext.
//
//   - "user.key"           → key of the 'user' sub-context
//   - "organization.tier"  → tier attribute on the 'organization' sub-context
//   - "userId" (no dot)    → legacy fallback: primary context's key
//   - any other bare path  → attribute from the primary context's bag
//
// Returns nil when the path cannot be resolved.
func ResolveDottedAttribute(ctx EvalContext, dottedPath string) json.RawMessage {
	if dottedPath == "" {
		return nil
	}
	dotIdx := strings.IndexByte(dottedPath, '.')
	if dotIdx > 0 {
		kind := dottedPath[:dotIdx]
		attr := dottedPath[dotIdx+1:]
		sub := ContextByKind(ctx, kind)
		if sub == nil {
			return nil
		}
		return readSubAttribute(sub, attr)
	}
	// Non-dotted legacy path.
	primary := PrimaryContext(ctx)
	if primary == nil {
		return nil
	}
	if dottedPath == "userId" {
		return jsonString(primary.Key)
	}
	return readSubAttribute(primary, dottedPath)
}

// ResolveAttribute is the original kinded lookup used by the audience
// evaluator. It mirrors the TypeScript resolveAttribute behaviour.
func ResolveAttribute(ctx EvalContext, contextKind, attribute string) json.RawMessage {
	if contextKind != "" {
		sub := ContextByKind(ctx, contextKind)
		if sub == nil {
			return nil
		}
		return readSubAttribute(sub, attribute)
	}
	var primary *SingleContext
	if IsMultiContext(ctx) {
		primary = ContextByKind(ctx, "user")
	} else {
		primary = ctx.AsSingle()
	}
	if primary == nil {
		return nil
	}
	if attribute == "userId" {
		return jsonString(primary.Key)
	}
	return readSubAttribute(primary, attribute)
}

func readSubAttribute(sub *SingleContext, attribute string) json.RawMessage {
	if attribute == "key" {
		return jsonString(sub.Key)
	}
	if attribute == "name" {
		return jsonString(sub.Name)
	}
	if _, reserved := reservedContextKeys[attribute]; reserved {
		return nil
	}
	v, ok := sub.Attributes[attribute]
	if !ok {
		return nil
	}
	return v
}

// LegacyAttribute returns the resolved hash value for the given hash
// attribute path (e.g. "user.key", "userId"). Returned as a string
// for direct use in MurmurHash3 calls.
func LegacyAttribute(ctx EvalContext, attribute string) string {
	if attribute == "" {
		attribute = "userId"
	}
	raw := ResolveDottedAttribute(ctx, attribute)
	if raw == nil {
		// Flat bag fallback — legacy single-context.
		single := ctx.AsSingle()
		if single == nil {
			return ""
		}
		if v, ok := single.Attributes[attribute]; ok {
			return RawToString(v)
		}
		return ""
	}
	return RawToString(raw)
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// RawToString converts a json.RawMessage to its string representation.
// JSON string literals are unwrapped; numbers are returned as-is.
func RawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	return string(raw)
}

// ContextSummary returns a compact representation of the context for logging.
func ContextSummary(ctx EvalContext) string {
	if IsMultiContext(ctx) {
		return "multi"
	}
	single := ctx.AsSingle()
	if single == nil {
		return "unknown"
	}
	return single.Kind + ":" + single.Key
}
