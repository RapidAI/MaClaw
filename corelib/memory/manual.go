package memory

import (
	"fmt"
	"strings"
)

// ManualMemorySourceType marks memories explicitly created or edited by the user.
const ManualMemorySourceType = "manual"

// SaveManualMemory stores a user-authored memory entry without generated-memory
// tag upsert semantics. Explicit manual creates should remain distinct unless
// Store.Save's lower-level content deduplication applies.
func (s *Store) SaveManualMemory(content string, category Category, tags []string) error {
	if s == nil {
		return nil
	}
	entry := Entry{
		Content:    content,
		Category:   category,
		Tags:       tags,
		SourceType: ManualMemorySourceType,
	}
	return s.Save(entry)
}

// SaveManualMemoryForOwner stores a user-authored memory entry under an
// explicit owner and boundary. Bot-scoped memories use this so the write stays
// on the reviewed manual helper instead of calling Store.Save directly.
func (s *Store) SaveManualMemoryForOwner(content string, category Category, tags []string, owner string, boundary *MemoryBoundary) error {
	if s == nil {
		return nil
	}
	entry := Entry{
		Content:    content,
		Category:   category,
		Tags:       tags,
		SourceType: ManualMemorySourceType,
		OwnerID:    owner,
		Boundary:   boundary,
	}
	return s.Save(entry)
}

// UpdateManualMemory updates a user-authored memory entry by ID while reusing
// the same metadata-preserving update path used by generated-memory upserts.
func (s *Store) UpdateManualMemory(id, content string, category Category, tags []string) error {
	if s == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return s.Update(id, content, category, tags)
	}
	return s.updateEntryFromUpsert(id, Entry{
		Content:    content,
		Category:   category,
		Tags:       tags,
		SourceType: ManualMemorySourceType,
	})
}

// UpdateManualMemoryIfUnchanged applies a manual edit only when the entry still matches expected.
func (s *Store) UpdateManualMemoryIfUnchanged(id, content string, category Category, tags []string, expected Entry) (bool, error) {
	return s.updateManualMemoryForOwnerIfUnchanged(id, content, category, tags, "", nil, expected)
}

func (s *Store) updateManualMemoryForOwnerIfUnchanged(id, content string, category Category, tags []string, owner string, boundary *MemoryBoundary, expected Entry) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("memory_store: not initialized")
	}
	id = strings.TrimSpace(id)
	content = strings.TrimSpace(content)
	if id == "" {
		return false, fmt.Errorf("memory_store: entry id must not be empty")
	}
	if content == "" {
		return false, fmt.Errorf("memory_store: content must not be empty")
	}
	if err := ScanForInjection(content); err != nil {
		return false, fmt.Errorf("memory_store: rejected: %w", err)
	}
	content = redactSecretsInMemory(content)

	s.mu.RLock()
	var updated Entry
	found := false
	for _, entry := range s.entries {
		if entry.ID == id {
			updated = entry
			found = true
			break
		}
	}
	if !found {
		s.mu.RUnlock()
		return false, fmt.Errorf("memory_store: entry %q not found", id)
	}
	if !entryMatchesExpectation(updated, map[string]Entry{id: expected}, id) {
		s.mu.RUnlock()
		return false, nil
	}
	ownerForDup := updated.OwnerID
	if owner != "" {
		ownerForDup = owner
	}
	duplicateID := ""
	for _, entry := range s.entries {
		if !entry.IsActive() || entry.ID == id || strings.TrimSpace(entry.Content) != content {
			continue
		}
		if category != "" && MapToCanonical(entry.Category) != MapToCanonical(category) {
			continue
		}
		if !memoryOwnersEqual(entry.OwnerID, ownerForDup) || distinctProjectRecords(entry, updated) {
			continue
		}
		duplicateID = entry.ID
		break
	}
	s.mu.RUnlock()
	if duplicateID != "" {
		return false, fmt.Errorf("memory_store: duplicate content (matches entry %q)", duplicateID)
	}

	contentChanged := updated.Content != content
	if contentChanged {
		updated.InvalidAt = nil
		updated.Embedding = nil
	}
	updated.Content = content
	updated.Category = category
	updated.Tags = append([]string(nil), tags...)
	updated.SourceType = ManualMemorySourceType
	updated.CompactForm = ""
	updated.ContentHash = computeContentHash(content)
	updated.Stale = false
	if owner != "" {
		updated.OwnerID = owner
	}
	if boundary != nil {
		updated.Boundary = cloneMemoryBoundary(boundary)
	}
	applied, err := s.UpdateEntriesAndDeleteIDsIfUnchanged([]Entry{updated}, nil, map[string]Entry{id: expected})
	if err != nil || !applied {
		return applied, err
	}
	if contentChanged {
		s.scheduleStoredEntryEmbedding(id)
	}
	return true, nil
}

// FormatMemorySavedForTool returns the shared short success message used by
// GUI/TUI/server memory management surfaces after a durable manual save.
func FormatMemorySavedForTool(content string) string {
	return "Memory saved: " + summarizeMemorySaveContent(content)
}

// FormatMemoryCandidateSavedForTool returns the shared short success message
// used when tool governance quarantines an uncertain memory candidate.
func FormatMemoryCandidateSavedForTool(content string) string {
	return "Memory saved as candidate: " + summarizeMemorySaveContent(content)
}

func summarizeMemorySaveContent(content string) string {
	summary := strings.ReplaceAll(content, "\n", " ")
	if len([]rune(summary)) > 50 {
		summary = string([]rune(summary)[:50]) + "..."
	}
	return summary
}
