package avsb

import (
	"context"
	"encoding/json"

	"github.com/avsbhq/avsb-go/core"
)

// ---------------------------------------------------------------------------
// Typed getters
// ---------------------------------------------------------------------------

// GetBoolFlag evaluates a boolean flag. defaultValue is returned when the flag
// is missing, disabled, the SDK is not ready, or the served value is not a
// boolean.
func (s *Server) GetBoolFlag(ctx context.Context, key string, defaultValue bool, opts ...Option) Flag[bool] {
	raw, _ := json.Marshal(defaultValue)
	eval, meta := s.evaluate(ctx, key, raw, opts...)
	value := defaultValue
	if err := json.Unmarshal(eval.Value, &value); err != nil {
		s.warnTypeMismatch(key, "bool", eval.Value)
		value = defaultValue
	}
	return buildFlag(value, eval, meta)
}

// GetStringFlag evaluates a string flag.
func (s *Server) GetStringFlag(ctx context.Context, key string, defaultValue string, opts ...Option) Flag[string] {
	raw, _ := json.Marshal(defaultValue)
	eval, meta := s.evaluate(ctx, key, raw, opts...)
	value := defaultValue
	if err := json.Unmarshal(eval.Value, &value); err != nil {
		s.warnTypeMismatch(key, "string", eval.Value)
		value = defaultValue
	}
	return buildFlag(value, eval, meta)
}

// GetNumberFlag evaluates a numeric flag. Numbers are float64 because that is
// what JSON carries; a flag holding 3 comes back as 3.0.
func (s *Server) GetNumberFlag(ctx context.Context, key string, defaultValue float64, opts ...Option) Flag[float64] {
	raw, _ := json.Marshal(defaultValue)
	eval, meta := s.evaluate(ctx, key, raw, opts...)
	value := defaultValue
	if err := json.Unmarshal(eval.Value, &value); err != nil {
		s.warnTypeMismatch(key, "number", eval.Value)
		value = defaultValue
	}
	return buildFlag(value, eval, meta)
}

// GetJSONFlag evaluates a flag and returns its raw JSON value. Unmarshal it
// into your own type:
//
//	type Config struct{ Retries int `json:"retries"` }
//	flag := server.GetJSONFlag(ctx, "checkout_config", []byte(`{"retries":3}`))
//	var cfg Config
//	if err := json.Unmarshal(flag.Value(), &cfg); err != nil { /* ... */ }
func (s *Server) GetJSONFlag(ctx context.Context, key string, defaultValue json.RawMessage, opts ...Option) Flag[json.RawMessage] {
	eval, meta := s.evaluate(ctx, key, defaultValue, opts...)
	return buildFlag(eval.Value, eval, meta)
}

// GetAllFlags evaluates every flag in the datafile against one context, keyed
// by flag key. It is what a server-rendered page needs: one pass instead of one
// call per flag.
//
// Exposures ARE recorded, because every returned flag is one the page can act
// on. Pass WithoutExposure() when you are only inspecting state.
func (s *Server) GetAllFlags(ctx context.Context, opts ...Option) map[string]Flag[json.RawMessage] {
	df := s.datafile()
	out := map[string]Flag[json.RawMessage]{}
	if df == nil {
		return out
	}
	for i := range df.Flags {
		key := df.Flags[i].Key
		out[key] = s.GetJSONFlag(ctx, key, json.RawMessage(`null`), opts...)
	}
	return out
}

// buildFlag converts a core.FlagEvaluation into a typed Flag[T].
func buildFlag[T any](value T, eval core.FlagEvaluation, meta evalMeta) Flag[T] {
	return Flag[T]{
		value:          value,
		variationKey:   eval.VariationKey,
		source:         eval.Source,
		ruleID:         eval.RuleID,
		ruleType:       eval.RuleType,
		reasons:        eval.Reasons,
		flagKey:        eval.FlagKey,
		evaluatedAt:    meta.evaluatedAt,
		durationMicros: meta.durationMicros,
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
