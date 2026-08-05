package core

import (
	"encoding/json"
	"strings"
)

// reservedContextKeys are the identity fields excluded from attribute lookup,
// so a context's own kind/key/name never collapse into targeting attributes.
var reservedContextKeys = map[string]struct{}{
	"kind":  {},
	"key":   {},
	"name":  {},
	"_meta": {},
}

// SingleContext is the parsed form of one kind/key/attributes envelope.
type SingleContext struct {
	// Kind is the context kind, e.g. "user" or "organization".
	Kind string
	// Key is the identity this kind buckets on.
	Key string
	// Name is an optional display name.
	Name string
	// Attributes is every non-reserved field, still as raw JSON.
	Attributes map[string]json.RawMessage
}

// IsMultiContext reports whether ctx is a multi-context wrapper (kind "multi").
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

// ParseSingle reads a SingleContext from raw JSON bytes. Returns nil when the
// bytes do not decode into a single-context shape.
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
// single-context it returns the receiver when the kinds match; for a
// multi-context it dereferences the sibling sub-context.
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

// BucketingKey returns the bucketing identifier for the given kind, defaulting
// to "user". Returns the empty string when no key is available, which buckets
// every such context together: that is deterministic, and the SDK layer warns
// about it once per process.
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

// PrimaryContext returns the primary single-context. For a multi-context the
// "user" sub-context is preferred, otherwise the first non-meta entry. For a
// single-context it is returned directly.
func PrimaryContext(ctx EvalContext) *SingleContext {
	if !IsMultiContext(ctx) {
		return ctx.AsSingle()
	}
	if sub := ContextByKind(ctx, "user"); sub != nil {
		return sub
	}
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
//   - "user.key"           key of the "user" sub-context
//   - "organization.tier"  tier attribute on the "organization" sub-context
//   - "userId" (no dot)    legacy fallback: the primary context's key
//   - any other bare path  attribute from the primary context's bag
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
	primary := PrimaryContext(ctx)
	if primary == nil {
		return nil
	}
	if dottedPath == "userId" {
		return rawJSONString(primary.Key)
	}
	return readSubAttribute(primary, dottedPath)
}

// ResolveAttribute is the kinded lookup the audience evaluator uses. When
// contextKind is empty it falls back to the primary context, which also
// exposes the primary key as "userId" for legacy audiences.
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
		return rawJSONString(primary.Key)
	}
	return readSubAttribute(primary, attribute)
}

func readSubAttribute(sub *SingleContext, attribute string) json.RawMessage {
	if attribute == "key" {
		return rawJSONString(sub.Key)
	}
	if attribute == "name" {
		return rawJSONString(sub.Name)
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

// LegacyAttribute returns the resolved hash value for a hash-attribute path
// (for example "user.key" or "userId"), as a string ready for MurmurHash3.
func LegacyAttribute(ctx EvalContext, attribute string) string {
	if attribute == "" {
		attribute = "userId"
	}
	raw := ResolveDottedAttribute(ctx, attribute)
	if raw == nil {
		// Flat-bag fallback for a legacy single-context.
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

// rawJSONString wraps a Go string as a JSON string literal.
func rawJSONString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// RawToString converts raw JSON to its string form. JSON string literals are
// unwrapped; every other shape is returned as its JSON text.
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

// ContextRefs summarises a context as kind/key pairs for the decision log.
// Attributes are deliberately excluded because they may carry personal data.
//
// This replaced a single "user:u_1" string so the Go decision log carries the
// same shape as the Python, .NET, and agent logs: a list a consumer can filter
// on rather than a string it has to parse.
func ContextRefs(ctx EvalContext) []ContextRef {
	if IsMultiContext(ctx) {
		out := make([]ContextRef, 0, len(ctx))
		for kind, raw := range ctx {
			if kind == "kind" {
				continue
			}
			sub := ParseSingle(raw)
			if sub == nil {
				continue
			}
			out = append(out, ContextRef{Kind: sub.Kind, Key: sub.Key})
		}
		sortContextRefs(out)
		return out
	}
	single := ctx.AsSingle()
	if single == nil {
		return []ContextRef{}
	}
	return []ContextRef{{Kind: single.Kind, Key: single.Key}}
}

// sortContextRefs orders refs by kind so a multi-context log entry is stable
// across runs. Go map iteration order is randomised, and a decision log that
// reorders itself between two identical evaluations is unreadable in a diff.
func sortContextRefs(refs []ContextRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && refs[j].Kind < refs[j-1].Kind; j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}
