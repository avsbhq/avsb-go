package core

// StickyBucketService is the interface that sticky-bucket backends must
// satisfy. When provided to EvaluateOptions, the evaluator will read
// existing assignments before evaluating ab_test rules and persist new
// assignments on first bucket.
//
// Implementations are in the top-level sticky sub-package.
type StickyBucketService interface {
	// GetAssignment returns the persisted variation assignment for the
	// supplied (bucketingID, flagKey, ruleID) triple, or nil when none
	// exists yet.
	GetAssignment(bucketingID, flagKey, ruleID string) *StickyAssignment
	// SaveAssignment persists a new assignment. Called on the hot path so
	// implementations should be non-blocking (fire-and-forget).
	SaveAssignment(bucketingID, flagKey, ruleID string, assignment StickyAssignment)
}
