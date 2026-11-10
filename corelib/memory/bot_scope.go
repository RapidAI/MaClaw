package memory

import (
	"fmt"
	"strings"
)

// Bot memory scope uses OwnerID "<base>:bot:<botID>" so each bot's long-term
// entries stay isolated inside the shared Store while still passing the GUI
// lineage check (prefix base+":"). Tag "bot:<botID>" is kept for search UIs.

const botOwnerInfix = ":bot:"

// FormatBotMemoryOwner builds the OwnerID for one bot under a base owner
// (for example desktop-user). Empty botID is rejected.
func FormatBotMemoryOwner(baseOwner, botID string) (string, error) {
	baseOwner = strings.TrimSpace(baseOwner)
	botID = strings.TrimSpace(botID)
	if baseOwner == "" {
		return "", fmt.Errorf("base owner is required")
	}
	if botID == "" {
		return "", fmt.Errorf("bot id is required")
	}
	if strings.Contains(botID, botOwnerInfix) || strings.Contains(botID, "/") || strings.Contains(botID, "\\") {
		return "", fmt.Errorf("invalid bot id")
	}
	return baseOwner + botOwnerInfix + botID, nil
}

// ParseBotMemoryOwner returns the bot id when owner is a bot-scoped OwnerID
// under baseOwner. ok is false for non-bot owners.
func ParseBotMemoryOwner(baseOwner, owner string) (botID string, ok bool) {
	baseOwner = strings.TrimSpace(baseOwner)
	owner = strings.TrimSpace(owner)
	prefix := baseOwner + botOwnerInfix
	if baseOwner == "" || !strings.HasPrefix(owner, prefix) {
		return "", false
	}
	botID = strings.TrimSpace(strings.TrimPrefix(owner, prefix))
	if botID == "" || strings.Contains(botID, ":") {
		return "", false
	}
	return botID, true
}

// BotMemoryTag is the searchable tag written alongside bot-scoped entries.
func BotMemoryTag(botID string) string {
	return "bot:" + strings.TrimSpace(botID)
}

// ListBotMemories returns active structured long-term entries owned by one bot.
func (s *Store) ListBotMemories(baseOwner, botID, category, keyword string) ([]Entry, error) {
	owner, err := FormatBotMemoryOwner(baseOwner, botID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	listed := s.ListActiveForOwner(Category(category), keyword, owner, true)
	out := make([]Entry, 0, len(listed))
	for _, entry := range listed {
		entry.Embedding = nil
		entry.RelatedEdges = nil
		out = append(out, entry)
	}
	return out, nil
}

// SaveBotMemory creates a user-authored long-term entry scoped to one bot.
func (s *Store) SaveBotMemory(baseOwner, botID, content, category string, tags []string) (Entry, error) {
	owner, err := FormatBotMemoryOwner(baseOwner, botID)
	if err != nil {
		return Entry{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return Entry{}, fmt.Errorf("memory content is required")
	}
	if s == nil {
		return Entry{}, fmt.Errorf("memory store is nil")
	}
	if MapToCanonical(Category(category)).IsProtected() {
		return Entry{}, fmt.Errorf("protected memory cannot be created here")
	}
	tag := BotMemoryTag(botID)
	cleaned := make([]string, 0, len(tags)+1)
	seen := map[string]struct{}{tag: {}}
	cleaned = append(cleaned, tag)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		cleaned = append(cleaned, t)
	}
	if err := s.SaveManualMemoryForOwner(content, Category(category), cleaned, owner, &MemoryBoundary{OwnerID: owner}); err != nil {
		return Entry{}, err
	}
	// Save may rewrite ID/timestamps; re-read by owner+content match preferred.
	for _, got := range s.ListActiveForOwner(Category(category), "", owner, true) {
		if got.Content == content && memoryOwnersEqual(got.OwnerID, owner) {
			got.Embedding = nil
			got.RelatedEdges = nil
			return got, nil
		}
	}
	return Entry{
		Content:    content,
		Category:   Category(category),
		Tags:       cleaned,
		OwnerID:    owner,
		SourceType: ManualMemorySourceType,
		Boundary:   &MemoryBoundary{OwnerID: owner},
	}, nil
}

// UpdateBotMemory edits one bot-scoped entry. Wrong-owner ids are rejected.
func (s *Store) UpdateBotMemory(baseOwner, botID, id, content, category string, tags []string) error {
	owner, err := FormatBotMemoryOwner(baseOwner, botID)
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	content = strings.TrimSpace(content)
	if id == "" {
		return fmt.Errorf("memory id is required")
	}
	if content == "" {
		return fmt.Errorf("memory content is required")
	}
	if s == nil {
		return fmt.Errorf("memory store is nil")
	}
	entry, ok := s.findBotOwnedEntry(owner, id)
	if !ok {
		return fmt.Errorf("memory not found")
	}
	if entry.Category.IsProtected() || MapToCanonical(Category(category)).IsProtected() {
		return fmt.Errorf("protected memory cannot be edited here")
	}
	tag := BotMemoryTag(botID)
	cleaned := make([]string, 0, len(tags)+1)
	seen := map[string]struct{}{tag: {}}
	cleaned = append(cleaned, tag)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		cleaned = append(cleaned, t)
	}
	return s.updateEntryFromUpsert(id, Entry{
		Content:    content,
		Category:   Category(category),
		Tags:       cleaned,
		OwnerID:    owner,
		SourceType: ManualMemorySourceType,
		Boundary:   &MemoryBoundary{OwnerID: owner},
	})
}

// DeleteBotMemory removes one bot-scoped entry. Wrong-owner ids are rejected.
func (s *Store) DeleteBotMemory(baseOwner, botID, id string) error {
	owner, err := FormatBotMemoryOwner(baseOwner, botID)
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("memory id is required")
	}
	if s == nil {
		return fmt.Errorf("memory store is nil")
	}
	entry, ok := s.findBotOwnedEntry(owner, id)
	if !ok {
		return fmt.Errorf("memory not found")
	}
	if entry.Category.IsProtected() {
		return fmt.Errorf("protected memory cannot be deleted here")
	}
	return s.Delete(id)
}

func (s *Store) findBotOwnedEntry(owner, id string) (Entry, bool) {
	for _, entry := range s.List("", "") {
		if entry.ID == id && memoryOwnersEqual(entry.OwnerID, owner) && entry.IsActive() {
			return entry, true
		}
	}
	return Entry{}, false
}

// DeleteBotMemoriesAtomic deletes the complete bot-owned selection or leaves it unchanged.
func (s *Store) DeleteBotMemoriesAtomic(baseOwner, botID string, ids []string) (int, error) {
	owner, err := FormatBotMemoryOwner(baseOwner, botID)
	if err != nil {
		return 0, err
	}
	if s == nil {
		return 0, fmt.Errorf("memory store is nil")
	}
	seen := map[string]bool{}
	deleteIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		deleteIDs = append(deleteIDs, id)
	}
	if len(deleteIDs) == 0 {
		return 0, fmt.Errorf("at least one memory id is required")
	}
	entriesByID := make(map[string]Entry, len(deleteIDs))
	for _, entry := range s.List("", "") {
		if seen[entry.ID] && memoryOwnersEqual(entry.OwnerID, owner) && entry.IsActive() {
			entriesByID[entry.ID] = entry
		}
	}
	for _, id := range deleteIDs {
		entry, ok := entriesByID[id]
		if !ok {
			return 0, fmt.Errorf("memory not found")
		}
		if entry.Category.IsProtected() {
			return 0, fmt.Errorf("protected memory cannot be deleted here")
		}
	}
	applied, err := s.UpdateEntriesAndDeleteIDsIfUnchanged(nil, deleteIDs, entriesByID)
	if err != nil {
		return 0, err
	}
	if !applied {
		return 0, fmt.Errorf("memory batch changed during deletion; retry")
	}
	return len(deleteIDs), nil
}

// DeleteBotMemories removes many bot-scoped entries. Protected or foreign ids
// are skipped and listed in skipped; deleted is the count actually removed.
func (s *Store) DeleteBotMemories(baseOwner, botID string, ids []string) (deleted int, skipped []string, err error) {
	owner, err := FormatBotMemoryOwner(baseOwner, botID)
	if err != nil {
		return 0, nil, err
	}
	if s == nil {
		return 0, nil, fmt.Errorf("memory store is nil")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		entry, ok := s.findBotOwnedEntry(owner, id)
		if !ok {
			skipped = append(skipped, id+":not_found")
			continue
		}
		if entry.Category.IsProtected() {
			skipped = append(skipped, id+":protected")
			continue
		}
		if delErr := s.Delete(id); delErr != nil {
			skipped = append(skipped, id+":"+delErr.Error())
			continue
		}
		deleted++
	}
	return deleted, skipped, nil
}
