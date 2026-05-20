package memory

// SaveManualMemoryForHost stores a user-authored memory entry from host
// management surfaces while keeping validation and generated-memory ownership in
// corelib/memory.
func (s *Store) SaveManualMemoryForHost(content string, category Category, tags []string) error {
	if s == nil {
		return nil
	}
	return s.SaveManualMemory(content, category, tags)
}

// UpdateManualMemoryForHost updates a user-authored memory entry from host
// management surfaces by delegating to the shared corelib write path.
func (s *Store) UpdateManualMemoryForHost(id, content string, category Category, tags []string) error {
	if s == nil {
		return nil
	}
	return s.UpdateManualMemory(id, content, category, tags)
}

// RestoreFromArchiveForHost restores an archived memory entry for host
// management UIs without exposing the archive store directly.
func (s *Store) RestoreFromArchiveForHost(id string) error {
	if s == nil {
		return nil
	}
	return s.RestoreFromArchive(id)
}

// PinEntryForHost marks a host-visible memory entry as pinned/protected.
func (s *Store) PinEntryForHost(id string) error {
	if s == nil {
		return nil
	}
	return s.PinEntry(id)
}

// UnpinEntryForHost clears the pinned/protected flag for a host-visible entry.
func (s *Store) UnpinEntryForHost(id string) error {
	if s == nil {
		return nil
	}
	return s.UnpinEntry(id)
}
