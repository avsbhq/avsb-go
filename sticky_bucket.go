package avsb

import "github.com/avsbhq/avsb-go/core"

// StickyBucketService is a persistent store of variation assignments, so a
// visitor who was bucketed once keeps that variation for the life of the
// experiment even if traffic weights change underneath them.
//
// It is wired at the CLIENT layer, not inside the evaluator: the evaluator
// stays a pure function of datafile plus context, and the SDK runs the lookup
// before it and the save after it. That is the same pipeline @avsbhq/node uses,
// which is what makes a Node service and a Go service agree on a visitor.
//
// Implementations live in the sticky sub-package
// (github.com/avsbhq/avsb-go/sticky): in-memory by default, with Redis,
// DynamoDB, and Postgres behind build tags.
//
// The pipeline, in order:
//
//  1. runtime override (client)
//  2. sticky Lookup (client)
//  3. datafile override (evaluator)
//  4. holdout (evaluator)
//  5. rules in declared order (evaluator)
//  6. flag default variation (evaluator)
//
// Save happens ONLY when the evaluator returned source "rule" with ruleType
// "ab_test". A rollout is not an assignment, and a holdout, an override, or a
// default must never be written: storing them pins a visitor to a decision
// that was never theirs to keep.
type StickyBucketService interface {
	// Lookup returns the stored assignment for this bucketing id and flag key,
	// or nil. A miss is normal, not an error.
	//
	// It is called on the hot path, so an implementation that talks to a
	// network store should serve from a local cache and refresh in the
	// background rather than blocking the evaluation.
	Lookup(bucketingID, flagKey string) *core.StickyAssignment
	// Save persists an assignment. Fire and forget: it must not block, and it
	// must not return an error the caller has to handle mid-evaluation.
	Save(bucketingID, flagKey string, assignment core.StickyAssignment)
}
