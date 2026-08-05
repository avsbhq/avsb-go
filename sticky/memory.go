// Package sticky provides StickyBucketService implementations for the A vs B
// Go SDK. Pass one via ServerOptions.StickyBucketService to keep a visitor in
// the variation they were first bucketed into, even when traffic weights change
// underneath them.
//
// The in-memory service needs no dependencies. Redis, DynamoDB, and Postgres
// live behind build tags so the root module stays dependency free:
//
//	go build -tags redis ./...
//	go build -tags dynamodb ./...
//	go build -tags postgres ./...
//
// Pick a shared store for anything running more than one process. An in-memory
// store on three replicas gives a visitor three different sticky assignments,
// one per replica, which is worse than no sticky bucketing at all.
package sticky

import (
	"sync"

	"github.com/avsbhq/avsb-go/core"
)

// assignmentKey is the composite lookup key: one assignment per (identity,
// flag). It deliberately does NOT include the rule id, because a lookup happens
// before the evaluator has picked a rule; the stored record carries the rule it
// came from.
type assignmentKey struct {
	bucketingID string
	flagKey     string
}

// InMemoryService keeps assignments in this process's memory. Assignments are
// lost on restart and are not shared between replicas, so it suits tests and
// single-instance deployments.
type InMemoryService struct {
	mu          sync.RWMutex
	assignments map[assignmentKey]core.StickyAssignment
}

// NewInMemoryService creates an empty InMemoryService.
func NewInMemoryService() *InMemoryService {
	return &InMemoryService{
		assignments: make(map[assignmentKey]core.StickyAssignment),
	}
}

// Lookup implements avsb.StickyBucketService.
func (s *InMemoryService) Lookup(bucketingID, flagKey string) *core.StickyAssignment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.assignments[assignmentKey{bucketingID, flagKey}]
	if !ok {
		return nil
	}
	// A copy, so a caller cannot mutate the stored record through the pointer.
	cp := a
	return &cp
}

// Save implements avsb.StickyBucketService.
func (s *InMemoryService) Save(bucketingID, flagKey string, assignment core.StickyAssignment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assignments[assignmentKey{bucketingID, flagKey}] = assignment
}

// Len returns how many assignments are stored. Useful in tests.
func (s *InMemoryService) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.assignments)
}
