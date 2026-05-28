package avsb

import (
	"context"
	"encoding/json"

	of "github.com/open-feature/go-sdk/openfeature"
)

// OpenFeatureProvider adapts an A vs B [Server] to the OpenFeature
// go-sdk-contrib Provider interface (openfeature.dev/go-sdk).
//
// Usage:
//
//	provider := server.OpenFeatureProvider()
//	openfeature.SetProvider(provider)
//	client := openfeature.NewClient("my-app")
type OpenFeatureProvider struct {
	server *Server
	meta   of.Metadata
}

// OpenFeatureProvider returns an of.FeatureProvider backed by this server.
func (s *Server) OpenFeatureProvider() *OpenFeatureProvider {
	return &OpenFeatureProvider{
		server: s,
		meta:   of.Metadata{Name: "avsb"},
	}
}

// Metadata implements of.FeatureProvider.
func (p *OpenFeatureProvider) Metadata() of.Metadata {
	return p.meta
}

// BooleanEvaluation implements of.FeatureProvider.
func (p *OpenFeatureProvider) BooleanEvaluation(ctx context.Context, flag string, defaultValue bool, evalCtx of.FlattenedContext) of.BoolResolutionDetail {
	ec := flattenedToEvalContext(evalCtx)
	f := p.server.GetBoolFlag(ctx, flag, defaultValue, WithContext(ec))
	return of.BoolResolutionDetail{
		Value: f.Value(),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// StringEvaluation implements of.FeatureProvider.
func (p *OpenFeatureProvider) StringEvaluation(ctx context.Context, flag string, defaultValue string, evalCtx of.FlattenedContext) of.StringResolutionDetail {
	ec := flattenedToEvalContext(evalCtx)
	f := p.server.GetStringFlag(ctx, flag, defaultValue, WithContext(ec))
	return of.StringResolutionDetail{
		Value: f.Value(),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// FloatEvaluation implements of.FeatureProvider.
func (p *OpenFeatureProvider) FloatEvaluation(ctx context.Context, flag string, defaultValue float64, evalCtx of.FlattenedContext) of.FloatResolutionDetail {
	ec := flattenedToEvalContext(evalCtx)
	f := p.server.GetNumberFlag(ctx, flag, defaultValue, WithContext(ec))
	return of.FloatResolutionDetail{
		Value: f.Value(),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// IntEvaluation implements of.FeatureProvider. Evaluates as float64 then
// truncates to int64.
func (p *OpenFeatureProvider) IntEvaluation(ctx context.Context, flag string, defaultValue int64, evalCtx of.FlattenedContext) of.IntResolutionDetail {
	ec := flattenedToEvalContext(evalCtx)
	f := p.server.GetNumberFlag(ctx, flag, float64(defaultValue), WithContext(ec))
	return of.IntResolutionDetail{
		Value: int64(f.Value()),
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// ObjectEvaluation implements of.FeatureProvider.
func (p *OpenFeatureProvider) ObjectEvaluation(ctx context.Context, flag string, defaultValue interface{}, evalCtx of.FlattenedContext) of.InterfaceResolutionDetail {
	ec := flattenedToEvalContext(evalCtx)
	dv, _ := json.Marshal(defaultValue)
	f := p.server.GetJSONFlag(ctx, flag, dv, WithContext(ec))

	var out interface{}
	_ = json.Unmarshal(f.Value(), &out)

	return of.InterfaceResolutionDetail{
		Value: out,
		ProviderResolutionDetail: toDetail(f.Source(), f.VariationKey()),
	}
}

// Hooks implements of.FeatureProvider. Returns no hooks.
func (p *OpenFeatureProvider) Hooks() []of.Hook { return nil }

// toDetail maps an avsb source string to an OpenFeature ResolutionDetail.
func toDetail(source, variationKey string) of.ProviderResolutionDetail {
	var reason of.Reason
	switch source {
	case "rule":
		reason = of.TargetingMatchReason
	case "override", "datafileOverride", "holdout":
		reason = of.StaticReason
	case "disabled", "default":
		reason = of.DefaultReason
	case "not_found":
		reason = of.ErrorReason
	default:
		reason = of.UnknownReason
	}
	d := of.ProviderResolutionDetail{
		Reason:  reason,
		Variant: variationKey,
	}
	if source == "not_found" {
		d.ResolutionError = of.NewFlagNotFoundResolutionError("flag not found")
	}
	return d
}

// flattenedToEvalContext converts an OpenFeature FlattenedContext into an
// avsb EvalContext. The "targetingKey" is mapped to the user key.
func flattenedToEvalContext(flat of.FlattenedContext) EvalContext {
	attrs := make(map[string]any, len(flat))
	key := ""
	for k, v := range flat {
		if k == "targetingKey" {
			if s, ok := v.(string); ok {
				key = s
			}
			continue
		}
		attrs[k] = v
	}
	return UserContext(key, attrs)
}
