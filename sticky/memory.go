// Package sticky provides StickyBucketService implementations for the A vs B
// Go SDK. Import this package and pass an instance via ServerOptions to
// enable persistent variation bucketing.
package sticky

import (
	"sync"

	"github.com/avsbhq/avsb-go/core"
)

// assignmentKey is the composite lookup key for a sticky assignment.
type assignmentKey struct {
	bucketingID string
	flagKey     string
	ruleID      string
}

// InMemoryService is a thread-safe in-memory StickyBucketService suitable for
// tests and single-instance deployments. Assignments are lost on process restart.
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

// GetAssignment implements core.StickyBucketService.
func (s *InMemoryService) GetAssignment(bucketingID, flagKey, ruleID string) *core.StickyAssignment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k := assignmentKey{bucketingID, flagKey, ruleID}
	a, ok := s.assignments[k]
	if !ok {
		return nil
	}
	cp := a
	return &cp
}

// SaveAssignment implements core.StickyBucketService.
func (s *InMemoryService) SaveAssignment(bucketingID, flagKey, ruleID string, assignment core.StickyAssignment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assignments[assignmentKey{bucketingID, flagKey, ruleID}] = assignment
}

// Len returns the total number of stored assignments. Useful in tests.
func (s *InMemoryService) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.assignments)
}
