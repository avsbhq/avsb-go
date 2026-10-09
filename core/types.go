// Package core contains the pure flag evaluation engine for the A vs B Go SDK.
//
// It is a port of the TypeScript reference engine in
// packages/avsb-core/src/evaluate.ts and is locked to it by the shared
// conformance corpus (packages/avsb-conformance/fixtures). Values, variation
// keys, sources, rule ids, rule types AND the ordered reason strings are all
// compared exactly, so any change here must land with a matching change to the
// reference and the fixtures.
//
// Every exported type is safe to use from multiple goroutines as long as the
// caller does not mutate the datafile after passing it to the evaluator.
package core

import "encoding/json"

// Datafile is the published flag configuration an SDK evaluates against.
type Datafile struct {
	// Version is the datafile schema version (2 is current; 1 is accepted).
	Version int `json:"version"`
	// ProjectType is "FEATURE_FLAG" or "WEB_EXPERIMENT".
	ProjectType string `json:"projectType"`
	// SDKKey is the environment key this datafile was published for.
	SDKKey string `json:"sdkKey"`
	// EnvironmentKey is the environment name, e.g. "production".
	EnvironmentKey string `json:"environmentKey"`
	// PublishedAt is the RFC3339 publish timestamp. Change detection for
	// polling compares this field.
	PublishedAt string `json:"publishedAt"`
	// CollectEndpoint is the server-controlled ingestion origin. May be an
	// origin or a full batch URL; normalise it through CollectOriginFrom.
	CollectEndpoint string `json:"collectEndpoint"`
	// HeartbeatEndpoint is the server-controlled SDK-liveness endpoint. Empty
	// means do not send heartbeats.
	HeartbeatEndpoint string `json:"heartbeatEndpoint,omitempty"`
	// StreamEndpoint is the server-controlled SSE ticket endpoint. Empty means
	// streaming is unavailable; no SDK may guess a host.
	StreamEndpoint string `json:"streamEndpoint,omitempty"`
	// Flags is every flag published to this environment.
	Flags []FlagEntry `json:"flags"`
	// Audiences is the audience registry rules join against by id.
	Audiences []Audience `json:"audiences"`
	// Holdouts is the holdout registry, checked before per-rule iteration.
	Holdouts []Holdout `json:"holdouts,omitempty"`
	// Bandits is the shared-bandit registry a rule joins via Rule.BanditID.
	Bandits []Bandit `json:"bandits,omitempty"`
	// ContextKinds is the project's declared context kinds.
	ContextKinds []string `json:"contextKinds,omitempty"`
	// ExposureSampling is a 0-1 fraction controlling what proportion of
	// exposure events are forwarded to the collect endpoint.
	ExposureSampling *float64 `json:"exposureSampling,omitempty"`
	// CurrencyExponent is the project's money exponent (2 for USD/EUR, 0 for
	// JPY). Commerce audience conditions carry money in minor units and
	// convert with it. Absent means 2.
	CurrencyExponent *int `json:"currencyExponent,omitempty"`
	// RegAttributes is the registered-attribute registry. Exposure events
	// carry only these attribute keys; anything else is dropped server-side.
	RegAttributes json.RawMessage `json:"registeredAttributes,omitempty"`
}

// FlagEntry is one flag entry in the datafile.
type FlagEntry struct {
	// ID is the flag's stable identifier.
	ID string `json:"id"`
	// Key is the flag key callers pass to Get*Flag.
	Key string `json:"key"`
	// Type is the declared value type: "boolean", "string", "number", "json".
	Type string `json:"type"`
	// Enabled is false when the flag is switched off; a disabled flag serves
	// its default variation with source "disabled".
	Enabled bool `json:"enabled"`
	// DefaultVariationID is served when no rule matches.
	DefaultVariationID string `json:"defaultVariationId"`
	// Variations is every value this flag can serve.
	Variations []Variation `json:"variations"`
	// Overrides pins named users to a variation from the datafile itself.
	Overrides []Override `json:"overrides"`
	// Rules is evaluated in declared order; first match wins.
	Rules []Rule `json:"rules"`
}

// Variation is one possible served value for a flag.
type Variation struct {
	// ID is the variation's stable identifier. Sticky assignments store this,
	// never the key.
	ID string `json:"id"`
	// Key is the human-readable variation name.
	Key string `json:"key"`
	// Value is the raw JSON value served for this variation.
	Value json.RawMessage `json:"value"`
}

// Override is a datafile-declared user to variation pinning. It resolves to
// source "datafileOverride".
type Override struct {
	// UserID is compared against the primary context's key.
	UserID string `json:"userId"`
	// VariationID is the variation served on a match.
	VariationID string `json:"variationId"`
}

// Rule is one targeting rule: "targeted_delivery", "ab_test", or "bandit".
type Rule struct {
	// ID is the rule's stable identifier. It is also the experiment id on
	// exposure events.
	ID string `json:"id"`
	// Type is "targeted_delivery", "ab_test", or "bandit". An unrecognised
	// type is skipped with a named reason, never silently.
	Type string `json:"type"`
	// Enabled is false when the rule is paused.
	Enabled bool `json:"enabled"`
	// AudienceIDs must all resolve; the rule matches when ANY of them does.
	AudienceIDs []string `json:"audienceIds"`
	// HashAttribute is the dotted context path bucketing reads, e.g.
	// "user.key" or "organization.tier". Empty means the legacy "userId".
	HashAttribute string `json:"hashAttribute"`
	// HoldoutID links a holdout-typed rule to the holdout registry.
	HoldoutID string `json:"holdoutId,omitempty"`
	// TrafficAllocation is the 0-1 fraction of contexts the rule admits: the
	// experiment's traffic on an ab_test rule, the rollout on a
	// targeted_delivery rule. nil means everyone. Compared as a FLOAT: see
	// allocationGate.
	TrafficAllocation *float64 `json:"trafficAllocation"`
	// Variations is the per-rule split of traffic to variations.
	Variations []RuleVariation `json:"variations"`
	// Bandit is an inline bandit config for a bandit-typed rule. The rule's
	// own id addresses the model store when this is used.
	Bandit *BanditConfig `json:"bandit,omitempty"`
	// BanditID joins a bandit-typed rule to Datafile.Bandits. A shared bandit
	// wins over an inline Bandit, and the shared entry's Key is what
	// addresses the model store.
	BanditID string `json:"banditId,omitempty"`
}

// RuleVariation is the per-rule split of traffic to variations.
type RuleVariation struct {
	// VariationID names the variation this slice of traffic receives.
	VariationID string `json:"variationId"`
	// Percentage is the 0-1 share of admitted traffic.
	Percentage float64 `json:"percentage"`
}

// Audience is a named condition tree.
type Audience struct {
	// ID is what Rule.AudienceIDs joins against.
	ID string `json:"id"`
	// Rules is the root condition group. An empty group matches everyone.
	Rules ConditionGroup `json:"rules"`
}

// ConditionGroup is one AND/OR node of an audience tree.
//
// Children are heterogeneous (Condition or ConditionGroup); they round-trip
// via json.RawMessage and are dispatched by the runtime isConditionGroup guard.
type ConditionGroup struct {
	// ID is the group's identifier (unused by evaluation, kept for logs).
	ID string `json:"id"`
	// Logic is "AND" or "OR". Anything else behaves as "OR".
	Logic string `json:"logic"`
	// Conditions are the child nodes, each a Condition or a ConditionGroup.
	Conditions []json.RawMessage `json:"conditions"`
}

// Condition is one leaf node of an audience tree.
type Condition struct {
	// ID is the condition's identifier (unused by evaluation, kept for logs).
	ID string `json:"id"`
	// Type is the condition family: "CUSTOM_ATTRIBUTE", "CART_VALUE",
	// "PRODUCTS_VIEWED", "PURCHASE_HISTORY", or a browser-only family.
	Type string `json:"type"`
	// Attribute is the context attribute a CUSTOM_ATTRIBUTE condition reads.
	Attribute string `json:"attribute,omitempty"`
	// Operator is the comparison, including the dashboard's alias spellings
	// ("is", "is_not", "does_not_contain", "does_not_exist").
	Operator string `json:"operator"`
	// Value is the comparison target: a scalar, or an array for list and
	// range operators.
	Value json.RawMessage `json:"value"`
	// ContextKind scopes the lookup to one sub-context. When set and that
	// kind is absent from the context, the condition is a hard no-match.
	ContextKind string `json:"contextKind,omitempty"`
	// Match selects which viewed-products list a PRODUCTS_VIEWED condition
	// reads: "category" reads viewedCategories, anything else viewedSkus.
	Match string `json:"match,omitempty"`
	// DatasetSlug marks a PRODUCTS_VIEWED condition as dataset-backed.
	// Dataset windows live in the browser, so a server evaluation of such a
	// condition is false rather than a full-traffic match.
	DatasetSlug string `json:"datasetSlug,omitempty"`
}

// Holdout is a cohort held back from every flag it participates in.
type Holdout struct {
	// ID is the holdout's identifier. It is the experiment id on holdout
	// exposure events.
	ID string `json:"id"`
	// Key is the human-readable holdout name.
	Key string `json:"key"`
	// TrafficAllocation is the 0-1 share of contexts held out. Zero or less
	// disables the holdout entirely.
	TrafficAllocation float64 `json:"trafficAllocation"`
	// FlagIDs is every flag this holdout carves out.
	FlagIDs []string `json:"flagIds"`
	// HashAttribute is the dotted context path holdout bucketing reads.
	// Empty means "user.key".
	HashAttribute string `json:"hashAttribute"`
	// DefaultVariationByFlag maps a flag id to the variation held-out
	// contexts receive. A missing entry serves the flag's own default.
	DefaultVariationByFlag map[string]string `json:"defaultVariationByFlag"`
}

// Bandit is one entry in the shared-bandit registry.
type Bandit struct {
	// ID is what Rule.BanditID joins against.
	ID string `json:"id"`
	// Key addresses the model store and is the name decisions are
	// attributed to.
	Key string `json:"key"`
	// Config is the algorithm and action set.
	Config BanditConfig `json:"config"`
}

// BanditConfig describes an adaptive-allocation algorithm.
type BanditConfig struct {
	// Algorithm is "epsilon-greedy", "thompson-sampling", or "ucb1".
	Algorithm string `json:"algorithm"`
	// ExplorationRate is epsilon for epsilon-greedy. nil means 0.1. Zero is
	// a real value (exploration off), not "unset".
	ExplorationRate *float64 `json:"explorationRate,omitempty"`
	// RewardMetric is the metric key the offline trainer optimises.
	RewardMetric string `json:"rewardMetric"`
	// Actions is the available arm set. An empty set is a loud skip.
	Actions []BanditAction `json:"actions"`
}

// BanditAction is one available arm.
type BanditAction struct {
	// ID is the arm identifier. Tie-breaks are alphabetical on this value,
	// which is why cold start serves the lowest id.
	ID string `json:"id"`
	// VariationID is the flag variation this arm serves.
	VariationID string `json:"variationId"`
	// ContextAttributes declares the keys a contextual model buckets on.
	ContextAttributes map[string]interface{} `json:"contextAttributes,omitempty"`
}

// EvalContext is the kinded evaluation context, backed by a raw JSON map so
// both single-context and multi-context shapes decode into one type.
type EvalContext map[string]json.RawMessage

// ContextRef is one kind/key pair in a decision log's context summary.
// Attributes are deliberately excluded: they may carry personal data.
type ContextRef struct {
	// Kind is the context kind, e.g. "user" or "organization".
	Kind string `json:"kind"`
	// Key is that sub-context's key.
	Key string `json:"key"`
}

// FlagEvaluation is the evaluator's result for one (flag, context) pair. The
// SDK layer wraps it in a typed Flag[T].
type FlagEvaluation struct {
	// Value is the served value, or the caller's default.
	Value json.RawMessage `json:"value"`
	// VariationKey is the served variation's key, nil when none was served.
	VariationKey *string `json:"variationKey"`
	// Source is the public evaluation source. See the Source constants.
	Source string `json:"source"`
	// RuleID is the matched rule (or holdout) id, nil when none.
	RuleID *string `json:"ruleId"`
	// RuleType is the matched rule type, nil when none.
	RuleType *string `json:"ruleType"`
	// Reasons is the ordered decision trail. Never nil.
	Reasons []string `json:"reasons"`
	// FlagKey is the key that was evaluated.
	FlagKey string `json:"flagKey"`
}

// The public evaluation sources. This is the ENTIRE legal set; the internal
// spellings "override" and "runtime_override" must never reach a caller
// (locked by the conformance semantics fixture family).
const (
	// SourceDatafileOverride means a datafile-declared user override matched.
	SourceDatafileOverride = "datafileOverride"
	// SourceRuntimeOverride means a runtime override set on the client matched.
	SourceRuntimeOverride = "runtimeOverride"
	// SourceSticky means a stored sticky assignment was served.
	SourceSticky = "sticky"
	// SourceRule means a targeted_delivery or ab_test rule matched.
	SourceRule = "rule"
	// SourceHoldout means the context is in a holdout cohort.
	SourceHoldout = "holdout"
	// SourceBandit means a bandit rule selected an action.
	SourceBandit = "bandit"
	// SourceDefault means no rule matched and the flag's default was served.
	SourceDefault = "default"
	// SourceNotFound means the datafile loaded and the key is not in it.
	SourceNotFound = "not_found"
	// SourceDisabled means the flag exists but is switched off.
	SourceDisabled = "disabled"
	// SourceNotReady means there was no datafile to look in: init has not
	// finished, it failed with no bootstrap, or the client was closed.
	SourceNotReady = "not_ready"
)

// The rule types a decision can be attributed to.
const (
	// RuleTypeTargetedDelivery is a rollout to an audience.
	RuleTypeTargetedDelivery = "targeted_delivery"
	// RuleTypeABTest is a randomised split.
	RuleTypeABTest = "ab_test"
	// RuleTypeHoldout is the synthetic type on a holdout decision.
	RuleTypeHoldout = "holdout"
	// RuleTypeBandit is an adaptive-allocation rule.
	RuleTypeBandit = "bandit"
)

// StickyAssignment is one persisted variation assignment.
//
// Sticky bucketing is wired at the CLIENT layer, not inside the evaluator:
// the evaluator stays a pure function of datafile plus context.
type StickyAssignment struct {
	// VariationID is the variation's ID, never its key. Saving the key
	// corrupts the store because lookup resolves by id.
	VariationID string `json:"variationId"`
	// RuleID is the id of the ab_test rule that made the assignment.
	RuleID string `json:"ruleId"`
	// RuleType is always "ab_test": no other decision is ever saved.
	RuleType string `json:"ruleType"`
	// AssignedAt is milliseconds since the Unix epoch.
	AssignedAt int64 `json:"assignedAt"`
}

// BanditPerActionStats holds the reward statistics for one bandit arm.
type BanditPerActionStats struct {
	// Mean is the observed reward mean.
	Mean float64 `json:"mean"`
	// Variance is the observed reward variance.
	Variance float64 `json:"variance"`
	// Samples is the observation count. Zero means unseen (cold start).
	Samples int `json:"samples"`
}

// BanditModel is the runtime reward model read by the bandit evaluator.
// Models are learned, not published in the datafile, so an SDK with no store
// evaluates against a deterministic zero-observation cold-start model.
type BanditModel struct {
	// Version identifies the trained model; "cold-start" for the derived one.
	Version string `json:"version"`
	// Algorithm is the algorithm the model was trained for.
	Algorithm string `json:"algorithm"`
	// PerAction is the top-level per-arm statistics.
	PerAction map[string]BanditPerActionStats `json:"perAction"`
	// ContextualModels are per-context-bucket overrides of PerAction, keyed
	// by the joined contextAttributes bucket string.
	ContextualModels map[string]map[string]BanditPerActionStats `json:"contextualModels,omitempty"`
}

// BanditDecisionEntry is the structured record produced by EvaluateBandit.
type BanditDecisionEntry struct {
	// FlagKey is the bandit key the decision is attributed to.
	FlagKey string `json:"flagKey"`
	// BanditKey is the resolved bandit key.
	BanditKey string `json:"banditKey"`
	// ActionID is the selected arm.
	ActionID string `json:"actionId"`
	// ModelVersion is the model the decision came from.
	ModelVersion string `json:"modelVersion"`
	// ActionProbability is the probability the policy assigned this arm.
	ActionProbability float64 `json:"actionProbability"`
	// OptimalityGap is set when the explore branch fired.
	OptimalityGap *float64 `json:"optimalityGap"`
	// Source is always "bandit".
	Source string `json:"source"`
	// RuleType is always "bandit".
	RuleType string `json:"ruleType"`
	// Reasons is the decision trail for this selection.
	Reasons []string `json:"reasons"`
}
