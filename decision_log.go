package avsb

import "time"

// DecisionLog receives structured log entries for every flag evaluation the
// server performs. Implement this interface to forward decisions to your
// analytics pipeline.
type DecisionLog interface {
	// LogDecision is called synchronously on the hot path. Implementations
	// must be non-blocking (e.g. write to a buffered channel and drain
	// asynchronously).
	LogDecision(entry DecisionLogEntry)
}

// DecisionLogEntry is one structured flag decision record.
type DecisionLogEntry struct {
	// FlagKey is the flag that was evaluated.
	FlagKey string `json:"flagKey"`
	// VariationKey is the variation that was served, empty when not found.
	VariationKey string `json:"variationKey,omitempty"`
	// Source is the evaluation source: "rule", "override", "holdout",
	// "disabled", "default", "not_found".
	Source string `json:"source"`
	// RuleID is the matched rule id, empty when none.
	RuleID string `json:"ruleId,omitempty"`
	// RuleType is the matched rule type, empty when none.
	RuleType string `json:"ruleType,omitempty"`
	// Reasons is the human-readable audit trail (only populated when
	// IncludeReasons is enabled).
	Reasons []string `json:"reasons,omitempty"`
	// EvaluatedAt is the wall-clock time of the evaluation.
	EvaluatedAt time.Time `json:"evaluatedAt"`
	// DurationMicros is the evaluation duration in microseconds.
	DurationMicros int64 `json:"durationMicros"`
	// ContextSummary is a short representation of the eval context.
	ContextSummary string `json:"contextSummary,omitempty"`
}

// noopDecisionLog silently discards all decisions.
type noopDecisionLog struct{}

func (noopDecisionLog) LogDecision(_ DecisionLogEntry) {}
