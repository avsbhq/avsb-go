package avsb

// ---------------------------------------------------------------------------
// Runtime overrides
// ---------------------------------------------------------------------------

// SetOverrideForUser forces one identity onto a variation until it is cleared
// or the process restarts. Nothing is persisted and nothing is published: this
// is a local QA and incident tool, not a targeting rule.
//
// contextKind defaults to "user" when empty, so an org-scoped override is
// SetOverrideForUser("checkout-v2", "acme", "on", "organization").
func (s *Server) SetOverrideForUser(flagKey, contextKey, variationKey string, contextKind ...string) {
	kind := "user"
	if len(contextKind) > 0 && contextKind[0] != "" {
		kind = contextKind[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.overrides[flagKey]
	if entry == nil {
		entry = &flagOverrides{perKindKey: map[string]string{}}
		s.overrides[flagKey] = entry
	}
	entry.perKindKey[kind+":"+contextKey] = variationKey
}

// SetGlobalOverride forces every context onto a variation for this flag.
// Per-identity overrides still win over it.
func (s *Server) SetGlobalOverride(flagKey, variationKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.overrides[flagKey]
	if entry == nil {
		entry = &flagOverrides{perKindKey: map[string]string{}}
		s.overrides[flagKey] = entry
	}
	entry.global = variationKey
}

// ClearOverrideForUser removes one identity's override.
func (s *Server) ClearOverrideForUser(flagKey, contextKey string, contextKind ...string) {
	kind := "user"
	if len(contextKind) > 0 && contextKind[0] != "" {
		kind = contextKind[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.overrides[flagKey]
	if entry == nil {
		return
	}
	delete(entry.perKindKey, kind+":"+contextKey)
	if entry.global == "" && len(entry.perKindKey) == 0 {
		delete(s.overrides, flagKey)
	}
}

// ClearGlobalOverride removes a flag's global override.
func (s *Server) ClearGlobalOverride(flagKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.overrides[flagKey]
	if entry == nil {
		return
	}
	entry.global = ""
	if len(entry.perKindKey) == 0 {
		delete(s.overrides, flagKey)
	}
}

// ClearAllOverrides removes every runtime override.
func (s *Server) ClearAllOverrides() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides = map[string]*flagOverrides{}
}
