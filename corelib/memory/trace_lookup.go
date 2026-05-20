package memory

import (
	"fmt"
	"strings"
)

// EntryByIDForHost returns a single entry by exact ID for host runtime code that
// needs to update stateful generated memories while keeping exact-ID lookup
// semantics owned by corelib/memory.
func (s *Store) EntryByIDForHost(id string) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Entry{}, false
	}
	entries := s.SearchDirectByID(id)
	if len(entries) == 0 {
		return Entry{}, false
	}
	return entries[0], true
}

// EntryByMemoryTraceID resolves host-facing trace IDs of the form "memory:<id>"
// to a memory entry. Hosts use this instead of scanning the full store when
// reviewing or following up memory-backed experience traces.
func (s *Store) EntryByMemoryTraceID(traceID string) (Entry, error) {
	if s == nil {
		return Entry{}, fmt.Errorf("memory store not initialized")
	}
	traceID = strings.TrimSpace(traceID)
	if !strings.HasPrefix(traceID, "memory:") {
		return Entry{}, fmt.Errorf("only memory-backed experience traces are supported")
	}
	memoryID := strings.TrimSpace(strings.TrimPrefix(traceID, "memory:"))
	if memoryID == "" {
		return Entry{}, fmt.Errorf("memory trace id is empty")
	}
	entries := s.SearchDirectByID(memoryID)
	if len(entries) == 0 {
		return Entry{}, fmt.Errorf("experience trace %q not found", traceID)
	}
	return entries[0], nil
}
