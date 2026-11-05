package memory

import "strings"

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
