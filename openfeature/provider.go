// Package openfeature is the OpenFeature provider for the A vs B Go SDK.
//
// It is its own Go module, so `go get github.com/avsbhq/avsb-go` does not drag
// the OpenFeature SDK into the build graph of everyone who just wants flags.
// Install it when you want it:
//
//	go get github.com/avsbhq/avsb-go/openfeature
//
// Usage:
//
//	import (
//	    avsb "github.com/avsbhq/avsb-go"
//	    avsbof "github.com/avsbhq/avsb-go/openfeature"
//	    of "github.com/open-feature/go-sdk/openfeature"
//	)
//
//	server, _ := avsb.NewServer(avsb.ServerOptions{SDKKey: os.Getenv("AVSB_SDK_KEY")})
//	_ = server.OnReady(ctx)
//
//	of.SetProvider(avsbof.NewProvider(server))
//	client := of.NewClient("my-app")
//
//	value, _ := client.BooleanValue(ctx, "checkout-v2", false, of.EvaluationContext{
//	    TargetingKey: "u_123",
//	    Attributes: map[string]any{
//	        "plan":             "pro",
//	        "organization.key":  "acme-corp",
//	        "organization.tier": "enterprise",
//	    },
//	})
//
// Multi-kind targeting works through dotted attribute names, as the example
// above shows: "organization.key" builds an organization sub-context, so a rule
// with hashAttribute "organization.key" buckets by account. OpenFeature has no
// multi-context concept of its own, and the alternative (collapsing everything
// onto the user) silently loses account-level bucketing, which is A vs B's
// headline targeting feature.
package openfeature

import (
	"context"
	"encoding/json"
	"strings"

	of "github.com/open-feature/go-sdk/openfeature"

	avsb "github.com/avsbhq/avsb-go"
)

// targetingKeyAttribute is the flattened-context entry OpenFeature puts the
// targeting key under. Spelled out rather than referenced from the OF SDK so
// this file does not depend on which release exported the constant.
const targetingKeyAttribute = "targetingKey"

// Provider adapts an A vs B Server to the OpenFeature FeatureProvider
// interface.
type Provider struct {
	server *avsb.Server
	meta   of.Metadata
}

// NewProvider returns an OpenFeature provider backed by server.
func NewProvider(server *avsb.Server) *Provider {
	return &Provider{
		server: server,
		meta:   of.Metadata{Name: "avsb"},
	}
}

// Metadata implements of.FeatureProvider.
func (p *Provider) Metadata() of.Metadata {
	return p.meta
}

// BooleanEvaluation implements of.FeatureProvider.
func (p *Provider) BooleanEvaluation(
	ctx context.Context,
	flag string,
	defaultValue bool,
	evalCtx of.FlattenedContext,
) of.BoolResolutionDetail {
	f := p.server.GetBoolFlag(ctx, flag, defaultValue, avsb.WithContext(toEvalContext(evalCtx)))
	return of.BoolResolutionDetail{
		Value:                    f.Value(),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// StringEvaluation implements of.FeatureProvider.
func (p *Provider) StringEvaluation(
	ctx context.Context,
	flag string,
	defaultValue string,
	evalCtx of.FlattenedContext,
) of.StringResolutionDetail {
	f := p.server.GetStringFlag(ctx, flag, defaultValue, avsb.WithContext(toEvalContext(evalCtx)))
	return of.StringResolutionDetail{
		Value:                    f.Value(),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// FloatEvaluation implements of.FeatureProvider.
func (p *Provider) FloatEvaluation(
	ctx context.Context,
	flag string,
	defaultValue float64,
	evalCtx of.FlattenedContext,
) of.FloatResolutionDetail {
	f := p.server.GetNumberFlag(ctx, flag, defaultValue, avsb.WithContext(toEvalContext(evalCtx)))
	return of.FloatResolutionDetail{
		Value:                    f.Value(),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// IntEvaluation implements of.FeatureProvider. A vs B numbers are JSON numbers,
// so this evaluates as a float and truncates.
func (p *Provider) IntEvaluation(
	ctx context.Context,
	flag string,
	defaultValue int64,
	evalCtx of.FlattenedContext,
) of.IntResolutionDetail {
	f := p.server.GetNumberFlag(ctx, flag, float64(defaultValue), avsb.WithContext(toEvalContext(evalCtx)))
	return of.IntResolutionDetail{
		Value:                    int64(f.Value()),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// ObjectEvaluation implements of.FeatureProvider.
func (p *Provider) ObjectEvaluation(
	ctx context.Context,
	flag string,
	defaultValue interface{},
	evalCtx of.FlattenedContext,
) of.InterfaceResolutionDetail {
	encodedDefault, err := json.Marshal(defaultValue)
	if err != nil {
		encodedDefault = []byte("null")
	}
	f := p.server.GetJSONFlag(ctx, flag, encodedDefault, avsb.WithContext(toEvalContext(evalCtx)))

	var out interface{}
	if err := json.Unmarshal(f.Value(), &out); err != nil {
		out = defaultValue
	}
	return of.InterfaceResolutionDetail{
		Value:                    out,
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// Hooks implements of.FeatureProvider. This provider ships no hooks: A vs B
// evaluation is local and synchronous, so there is nothing a hook would
// usefully wrap that the OpenFeature client does not already expose.
func (p *Provider) Hooks() []of.Hook { return nil }

// toDetail maps an A vs B source onto an OpenFeature resolution reason.
//
// The mapping is deliberate rather than mechanical:
//
//   - a rule or bandit match is TARGETING_MATCH, the reason OpenFeature means
//     for "a rule picked this"
//   - overrides, sticky, and holdout are STATIC: the value was decided by
//     something outside this request
//   - default and disabled are DEFAULT
//   - not_found is an ERROR with FLAG_NOT_FOUND, because asking for a key that
//     is not there is a mistake worth surfacing
//   - not_ready is an ERROR with PROVIDER_NOT_READY, which is the distinction
//     OpenFeature clients branch on and which this SDK used to collapse into
//     "flag not found"
func toDetail(source, variationKey string) of.ProviderResolutionDetail {
	detail := of.ProviderResolutionDetail{Variant: variationKey}

	switch source {
	case "rule", "bandit":
		detail.Reason = of.TargetingMatchReason
	case "datafileOverride", "runtimeOverride", "sticky", "holdout":
		detail.Reason = of.StaticReason
	case "default", "disabled":
		detail.Reason = of.DefaultReason
	case "not_found":
		detail.Reason = of.ErrorReason
		detail.ResolutionError = of.NewFlagNotFoundResolutionError(
			"avsb: the flag key is not in the datafile")
	case "not_ready":
		detail.Reason = of.ErrorReason
		detail.ResolutionError = of.NewProviderNotReadyResolutionError(
			"avsb: no datafile is loaded yet. Await server.OnReady(ctx) before evaluating")
	default:
		detail.Reason = of.UnknownReason
	}
	return detail
}

// toEvalContext converts an OpenFeature flattened context into an A vs B
// EvalContext.
//
// targetingKey becomes the user key. A dotted attribute name ("organization.key",
// "organization.tier") builds that kind's sub-context, and everything else stays
// on the user, so a caller who never uses dots gets exactly the single-context
// behaviour they expect.
func toEvalContext(flat of.FlattenedContext) avsb.EvalContext {
	userKey := ""
	userAttrs := map[string]any{}
	// kind -> attribute -> value, for the dotted names.
	kinds := map[string]map[string]any{}

	for k, v := range flat {
		if k == targetingKeyAttribute {
			if s, ok := v.(string); ok {
				userKey = s
			}
			continue
		}
		dot := strings.IndexByte(k, '.')
		if dot <= 0 || dot == len(k)-1 {
			userAttrs[k] = v
			continue
		}
		kind := k[:dot]
		attr := k[dot+1:]
		if kinds[kind] == nil {
			kinds[kind] = map[string]any{}
		}
		kinds[kind][attr] = v
	}

	user := avsb.UserContext(userKey, userAttrs)
	if len(kinds) == 0 {
		return user
	}

	subContexts := make([]avsb.EvalContext, 0, len(kinds)+1)
	subContexts = append(subContexts, user)
	for kind, attrs := range kinds {
		key := ""
		if raw, ok := attrs["key"]; ok {
			if s, ok := raw.(string); ok {
				key = s
			}
			delete(attrs, "key")
		}
		subContexts = append(subContexts, avsb.SingleContext(kind, key, attrs))
	}
	return avsb.MultiContext(subContexts...)
}
