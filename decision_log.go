package avsb

import (
	"sync"
	"time"

	"github.com/avsbhq/avsb-go/core"
)

// DecisionLog receives one structured record per flag evaluation. Implement it
// to forward decisions to your own logging or analytics pipeline.
//
// This is not the exposure path. Exposures go to A vs B automatically; the
// decision log is for YOUR systems, which is why it is off by default and why
// nothing is sent anywhere without a handler.
type DecisionLog interface {
	// LogDecision is called synchronously on the hot path, so an
	// implementation must not block: write to a buffered channel and drain it
	// elsewhere.
	LogDecision(entry DecisionLogEntry)
}

// DecisionLogEntry is one flag decision.
type DecisionLogEntry struct {
	// FlagKey is the flag that was evaluated.
	FlagKey string `json:"flagKey"`
	// VariationKey is the served variation, empty when none was served.
	VariationKey string `json:"variationKey,omitempty"`
	// Source is why the value was served. See Flag.Source for the ten legal
	// values; the internal spellings "override" and "runtime_override" never
	// appear here.
	Source string `json:"source"`
	// RuleID is the matched rule (or holdout) id, empty when none.
	RuleID string `json:"ruleId,omitempty"`
	// RuleType is the matched rule type, empty when none.
	RuleType string `json:"ruleType,omitempty"`
	// Reasons is the ordered decision trail. Populated only when reasons are
	// on (ServerOptions.IncludeReasons or WithReasons(true)).
	Reasons []string `json:"reasons,omitempty"`
	// EvaluatedAt is the wall-clock time of the decision.
	EvaluatedAt time.Time `json:"evaluatedAt"`
	// DurationMicros is how long the evaluation took, in microseconds.
	DurationMicros int64 `json:"durationMicros"`
	// ContextSummary is the kind/key pairs of the evaluation context.
	// Attributes are deliberately excluded: they may carry personal data, and
	// a decision log is often shipped somewhere with looser access than the
	// request that produced it.
	ContextSummary []core.ContextRef `json:"contextSummary,omitempty"`
}

// noopDecisionLog discards every decision. It is the default.
type noopDecisionLog struct{}

func (noopDecisionLog) LogDecision(_ DecisionLogEntry) {}

// MemoryDecisionLog collects decisions in memory, up to a bound.
//
// It is for tests, local debugging, and a "why did this user get that?"
// endpoint. It is bounded on purpose: an unbounded collector installed by
// default is a slow memory leak in a long-running server.
type MemoryDecisionLog struct {
	mu      sync.Mutex
	entries []DecisionLogEntry
	limit   int
}

// NewMemoryDecisionLog creates a bounded in-memory decision log. A limit of
// zero or less uses 1000. Past the limit the OLDEST entries are dropped, so
// what is kept is always the most recent window.
func NewMemoryDecisionLog(limit int) *MemoryDecisionLog {
	if limit <= 0 {
		limit = 1000
	}
	return &MemoryDecisionLog{limit: limit}
}

// LogDecision implements DecisionLog.
func (m *MemoryDecisionLog) LogDecision(entry DecisionLogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	if len(m.entries) > m.limit {
		m.entries = append([]DecisionLogEntry(nil), m.entries[len(m.entries)-m.limit:]...)
	}
}

// Entries returns a copy of the collected decisions, oldest first.
func (m *MemoryDecisionLog) Entries() []DecisionLogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DecisionLogEntry(nil), m.entries...)
}

// Drain returns the collected decisions and empties the buffer.
func (m *MemoryDecisionLog) Drain() []DecisionLogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.entries
	m.entries = nil
	return out
}
