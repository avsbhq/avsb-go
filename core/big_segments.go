package core

import "sync"

// BigSegmentStore is the interface that big-segment membership backends must
// satisfy. Big segments are externally-stored collections too large to embed
// in the datafile (spec §2.9).
type BigSegmentStore interface {
	// IsMember returns true when contextKey is a member of segmentKey.
	// Returns (false, nil) when the answer is definitively "not a member".
	// Returns (false, error) when the membership cannot be determined.
	IsMember(segmentKey, contextKind, contextKey string) (bool, error)
}

// InMemoryBigSegmentStore is a thread-safe in-memory BigSegmentStore suitable
// for tests and environments that load segment membership up-front.
type InMemoryBigSegmentStore struct {
	mu      sync.RWMutex
	members map[string]map[string]struct{} // segmentKey → set of contextKey
}

// NewInMemoryBigSegmentStore creates an empty store.
func NewInMemoryBigSegmentStore() *InMemoryBigSegmentStore {
	return &InMemoryBigSegmentStore{
		members: make(map[string]map[string]struct{}),
	}
}

// Add registers contextKey as a member of segmentKey. contextKind is accepted
// for interface symmetry but is not stored separately: membership is keyed
// only by segmentKey + contextKey.
func (s *InMemoryBigSegmentStore) Add(segmentKey, _ string, contextKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.members[segmentKey]; !ok {
		s.members[segmentKey] = make(map[string]struct{})
	}
	s.members[segmentKey][contextKey] = struct{}{}
}

// IsMember implements BigSegmentStore.
func (s *InMemoryBigSegmentStore) IsMember(segmentKey, _ string, contextKey string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.members[segmentKey]; ok {
		_, member := m[contextKey]
		return member, nil
	}
	return false, nil
}
