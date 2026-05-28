// Package core contains the pure flag evaluation engine for the A vs B Go SDK.
//
// Types in this package mirror the §2 JSON wire shapes from the SDK Launch
// Readiness spec. Every exported type is safe to use from multiple goroutines
// as long as the caller does not mutate the datafile after passing it to the
// evaluator.
package core

import "encoding/json"

// Datafile is the §2.15 v2 (and accepted v1) flag datafile.
type Datafile struct {
	Version         int             `json:"version"`
	ProjectType     string          `json:"projectType"`
	SDKKey          string          `json:"sdkKey"`
	EnvironmentKey  string          `json:"environmentKey"`
	PublishedAt     string          `json:"publishedAt"`
	CollectEndpoint string          `json:"collectEndpoint"`
	HeartbeatEndpt  string          `json:"heartbeatEndpoint,omitempty"`
	Flags           []FlagEntry     `json:"flags"`
	Audiences       []Audience      `json:"audiences"`
	Holdouts        []Holdout       `json:"holdouts,omitempty"`
	Bandits         []Bandit        `json:"bandits,omitempty"`
	BigSegments     []BigSegmentRef `json:"bigSegments,omitempty"`
	ContextKinds    []string        `json:"contextKinds,omitempty"`
	// ExposureSampling is a 0–1 fraction controlling what proportion of
	// exposure events are forwarded to the collect endpoint.
	ExposureSampling *float64        `json:"exposureSampling,omitempty"`
	RegAttributes    json.RawMessage `json:"registeredAttributes,omitempty"`
}

// FlagEntry is one flag entry in the datafile.
type FlagEntry struct {
	ID                 string      `json:"id"`
	Key                string      `json:"key"`
	Type               string      `json:"type"`
	Enabled            bool        `json:"enabled"`
	DefaultVariationID string      `json:"defaultVariationId"`
	Variations         []Variation `json:"variations"`
	Overrides          []Override  `json:"overrides"`
	Rules              []Rule      `json:"rules"`
}

// Variation is one possible served value for a flag.
type Variation struct {
	ID    string          `json:"id"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// Override is a datafile-declared user → variation pinning.
type Override struct {
	UserID      string `json:"userId"`
	VariationID string `json:"variationId"`
}

// Rule covers targeted_delivery, ab_test, holdout, bandit per §2.7.
type Rule struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Enabled           bool            `json:"enabled"`
	AudienceIDs       []string        `json:"audienceIds"`
	HashAttribute     string          `json:"hashAttribute"`
	HoldoutID         string          `json:"holdoutId,omitempty"`
	TrafficAllocation *float64        `json:"trafficAllocation"`
	Variations        []RuleVariation `json:"variations"`
	Bandit            *BanditConfig   `json:"bandit,omitempty"`
}

// RuleVariation is the per-rule split of traffic to variations.
type RuleVariation struct {
	VariationID string  `json:"variationId"`
	Percentage  float64 `json:"percentage"`
}

// Audience is a named condition tree.
type Audience struct {
	ID    string         `json:"id"`
	Rules ConditionGroup `json:"rules"`
}

// ConditionGroup is one AND/OR node of an audience tree.
//
// Children are heterogeneous (Condition or ConditionGroup); they round-trip
// via json.RawMessage and are dispatched by the runtime isConditionGroup guard.
type ConditionGroup struct {
	ID         string            `json:"id"`
	Logic      string            `json:"logic"`
	Conditions []json.RawMessage `json:"conditions"`
}

// Condition is one leaf node of an audience tree.
type Condition struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Attribute   string          `json:"attribute,omitempty"`
	Operator    string          `json:"operator"`
	Value       json.RawMessage `json:"value"`
	ContextKind string          `json:"contextKind,omitempty"`
}

// Holdout is §2.8: a cohort held back from all participating flags.
type Holdout struct {
	ID                     string            `json:"id"`
	Key                    string            `json:"key"`
	TrafficAllocation      float64           `json:"trafficAllocation"`
	FlagIDs                []string          `json:"flagIds"`
	HashAttribute          string            `json:"hashAttribute"`
	DefaultVariationByFlag map[string]string `json:"defaultVariationByFlag"`
}

// Bandit is §2.10 — top-level bandit registry.
type Bandit struct {
	ID     string       `json:"id"`
	Key    string       `json:"key"`
	Config BanditConfig `json:"config"`
}

// BanditConfig describes an adaptive-allocation algorithm.
type BanditConfig struct {
	Algorithm       string         `json:"algorithm"`
	ExplorationRate *float64       `json:"explorationRate,omitempty"`
	RewardMetric    string         `json:"rewardMetric"`
	Actions         []BanditAction `json:"actions"`
}

// BanditAction is one available arm.
type BanditAction struct {
	ID                string                 `json:"id"`
	VariationID       string                 `json:"variationId"`
	ContextAttributes map[string]interface{} `json:"contextAttributes,omitempty"`
}

// BigSegmentRef is §2.9 — references to externally-stored segment membership.
type BigSegmentRef struct {
	SegmentKey  string `json:"segmentKey"`
	ContextKind string `json:"contextKind"`
}

// EvalContext is the §2.1 kinded context. Backed by a generic JSON map so
// we accept both SingleContext and MultiContext on the wire.
type EvalContext map[string]json.RawMessage

// FlagEvaluation mirrors the §2.2 Flag<T> evaluation result. The generic
// SDK wraps this in a typed Flag[T] at the API layer.
type FlagEvaluation struct {
	Value       json.RawMessage `json:"value"`
	VariationKey *string        `json:"variationKey"`
	Source      string          `json:"source"`
	RuleID      *string         `json:"ruleId"`
	RuleType    *string         `json:"ruleType"`
	Reasons     []string        `json:"reasons"`
	FlagKey     string          `json:"flagKey"`
}

// StickyAssignment mirrors §2.4.
type StickyAssignment struct {
	VariationID string `json:"variationId"`
	RuleID      string `json:"ruleId"`
	RuleType    string `json:"ruleType"`
	AssignedAt  int64  `json:"assignedAt"`
}

// BanditPerActionStats holds the reward statistics for one bandit arm.
type BanditPerActionStats struct {
	Mean     float64 `json:"mean"`
	Variance float64 `json:"variance"`
	Samples  int     `json:"samples"`
}

// BanditModel is the runtime reward model read by the bandit evaluator.
type BanditModel struct {
	Version         string                                        `json:"version"`
	Algorithm       string                                        `json:"algorithm"`
	PerAction       map[string]BanditPerActionStats               `json:"perAction"`
	ContextualModels map[string]map[string]BanditPerActionStats   `json:"contextualModels,omitempty"`
}

// BanditDecisionEntry is the structured log entry produced by EvaluateBandit.
type BanditDecisionEntry struct {
	FlagKey           string   `json:"flagKey"`
	BanditKey         string   `json:"banditKey"`
	ActionID          string   `json:"actionId"`
	ModelVersion      string   `json:"modelVersion"`
	ActionProbability float64  `json:"actionProbability"`
	OptimalityGap     *float64 `json:"optimalityGap"`
	Source            string   `json:"source"`
	RuleType          string   `json:"ruleType"`
	Reasons           []string `json:"reasons"`
}

// ExposureEvent is the analytics event emitted when a user is bucketed into
// an ab_test or holdout variation.
type ExposureEvent struct {
	VisitorID    string                 `json:"visitorId"`
	ExperimentID string                 `json:"experimentId"`
	VariationID  string                 `json:"variationId"`
	EventName    string                 `json:"eventName"`
	Revenue      *float64               `json:"revenue"`
	Timestamp    int64                  `json:"timestamp"`
	PageURL      string                 `json:"pageUrl"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
	Attributes   map[string]string      `json:"attributes,omitempty"`
}
