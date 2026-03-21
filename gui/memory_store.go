package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryCategory represents the category of a memory entry.
type MemoryCategory string

const (
	MemCategorySelfIdentity         MemoryCategory = "self_identity"
	MemCategoryUserFact             MemoryCategory = "user_fact"
	MemCategoryPreference           MemoryCategory = "preference"
	MemCategoryProjectKnowledge     MemoryCategory = "project_knowledge"
	MemCategoryInstruction          MemoryCategory = "instruction"
	MemCategoryConversationSummary  MemoryCategory = "conversation_summary"
)

// IsProtected returns true for categories that must never be evicted.
func (c MemoryCategory) IsProtected() bool {
	return c == MemCategorySelfIdentity
}

// MemoryEntry represents a single memory record.
type MemoryEntry struct {
	ID          string         `json:"id"`
	Content     string         `json:"content"`
	Category    MemoryCategory `json:"category"`
	Tags        []string       `json:"tags"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	AccessCount int            `json:"access_count"`
}

// MemoryStore provides persistent long-term memory storage.
type MemoryStore struct {
	mu       sync.RWMutex
	entries  []MemoryEntry
	path     string
	dirty    bool
	saveCh   chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	maxItems int
}

// NewMemoryStore creates a MemoryStore that persists to the given path.
// It loads existing entries from disk if the file exists.
func NewMemoryStore(path string) (*MemoryStore, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("memory_store: resolve path: %w", err)
	}

	s := &MemoryStore{
		entries:  make([]MemoryEntry, 0),
		path:     absPath,
		saveCh:   make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		maxItems: 500,
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	go s.persistLoop()

	return s, nil
}

// generateID produces a unique ID from the current timestamp and a random suffix.
func generateID() string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%04x", time.Now().UnixNano(), int(buf[0])<<8|int(buf[1]))
}

// Save stores a memory entry. If an entry with identical content already exists,
// it updates that entry's UpdatedAt and increments AccessCount instead of
// creating a duplicate.
func (s *MemoryStore) Save(entry MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Dedup: look for an existing entry with the same content.
	for i := range s.entries {
		if s.entries[i].Content == entry.Content {
			s.entries[i].UpdatedAt = now
			s.entries[i].AccessCount++
			// Merge tags if the caller supplied new ones.
			s.entries[i].Tags = mergeTags(s.entries[i].Tags, entry.Tags)
			s.dirty = true
			s.signalSave()
			return nil
		}
	}

	// New entry.
	if entry.ID == "" {
		entry.ID = generateID()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	if entry.AccessCount == 0 {
		entry.AccessCount = 1
	}

	s.entries = append(s.entries, entry)
	s.evictLRU()
	s.dirty = true
	s.signalSave()
	return nil
}

// Update modifies an existing entry identified by ID. Only Content, Category
// and Tags are updated; timestamps are refreshed automatically.
// Returns an error if content is empty, the ID is not found, or the new
// content collides with another existing entry (dedup).
func (s *MemoryStore) Update(id string, content string, category MemoryCategory, tags []string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("memory_store: content must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Dedup check: reject if another entry already has the same content.
	for _, e := range s.entries {
		if e.ID != id && e.Content == content {
			return fmt.Errorf("memory_store: duplicate content (matches entry %q)", e.ID)
		}
	}

	for i, e := range s.entries {
		if e.ID == id {
			s.entries[i].Content = content
			s.entries[i].Category = category
			s.entries[i].Tags = tags
			s.entries[i].UpdatedAt = time.Now()
			s.dirty = true
			s.signalSave()
			return nil
		}
	}
	return fmt.Errorf("memory_store: entry %q not found", id)
}

// Delete removes the entry with the given ID. Returns an error if not found.
func (s *MemoryStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			s.dirty = true
			s.signalSave()
			return nil
		}
	}
	return fmt.Errorf("memory_store: entry %q not found", id)
}

// List returns entries filtered by category and keyword.
// Pass empty strings to skip a filter.
func (s *MemoryStore) List(category MemoryCategory, keyword string) []MemoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	kw := strings.ToLower(keyword)
	var result []MemoryEntry
	for _, e := range s.entries {
		if category != "" && e.Category != category {
			continue
		}
		if kw != "" && !containsKeyword(e, kw) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// Search returns entries filtered by category and keyword, returning at most
// limit results. It behaves like List but with a cap on the result count.
func (s *MemoryStore) Search(category MemoryCategory, keyword string, limit int) []MemoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	kw := strings.ToLower(keyword)
	var result []MemoryEntry
	for _, e := range s.entries {
		if category != "" && e.Category != category {
			continue
		}
		if kw != "" && !containsKeyword(e, kw) {
			continue
		}
		result = append(result, e)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

// Recall retrieves memory entries relevant to the given user message for
// injection into the system prompt. Delegates to RecallForProject with
// no project affinity.
func (s *MemoryStore) Recall(userMessage string) []MemoryEntry {
	return s.RecallForProject(userMessage, "")
}

// RecallForProject retrieves memory entries relevant to the given user
// message, with optional project path affinity boosting.
//
// Rules:
//   - Always include ALL entries with category == "user_fact"
//   - Score remaining entries by keyword relevance, project affinity (+3),
//     and time decay (-1 per week after 7 days)
//   - Session checkpoints within 24h get a +2 recency boost
//   - Skip entries with negative score (too old, no match)
//   - Return at most 20 entries total
//   - Total estimated tokens (len(content)/4 per entry) must not exceed 2000
func (s *MemoryStore) RecallForProject(userMessage, projectPath string) []MemoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const maxEntries = 20
	const maxTokens = 2000

	words := strings.Fields(strings.ToLower(userMessage))
	projectLower := strings.ToLower(projectPath)
	now := time.Now()

	var selfIdentity []MemoryEntry
	var userFacts []MemoryEntry
	type scored struct {
		entry MemoryEntry
		score int
	}
	var others []scored

	for _, e := range s.entries {
		if e.Category == MemCategorySelfIdentity {
			selfIdentity = append(selfIdentity, e)
			continue
		}
		if e.Category == MemCategoryUserFact {
			userFacts = append(userFacts, e)
			continue
		}
		sc := relevanceScore(e, words)

		// Project affinity: boost entries tagged with the same project.
		if projectLower != "" {
			for _, tag := range e.Tags {
				if strings.ToLower(tag) == projectLower {
					sc += 3
					break
				}
			}
		}

		// Time decay: entries older than 7 days lose 1 point per week.
		age := now.Sub(e.UpdatedAt)
		if age > 7*24*time.Hour {
			weeks := int(age.Hours() / (24 * 7))
			sc -= weeks
		}

		// Session checkpoints get a recency boost.
		if e.Category == MemCategorySessionCheckpoint && age < 24*time.Hour {
			sc += 2
		}

		others = append(others, scored{entry: e, score: sc})
	}

	sort.SliceStable(others, func(i, j int) bool {
		if others[i].score != others[j].score {
			return others[i].score > others[j].score
		}
		return others[i].entry.AccessCount > others[j].entry.AccessCount
	})

	var result []MemoryEntry
	tokenBudget := maxTokens

	// Self-identity memories are always recalled first — highest priority.
	for _, e := range selfIdentity {
		tokens := len(e.Content) / 4
		tokenBudget -= tokens
		result = append(result, e)
	}

	for _, e := range userFacts {
		if len(result) >= maxEntries {
			break
		}
		tokens := len(e.Content) / 4
		if tokens > tokenBudget {
			continue
		}
		tokenBudget -= tokens
		result = append(result, e)
	}

	for _, s := range others {
		if len(result) >= maxEntries {
			break
		}
		if s.score < 0 {
			continue
		}
		tokens := len(s.entry.Content) / 4
		if tokens > tokenBudget {
			continue
		}
		tokenBudget -= tokens
		result = append(result, s.entry)
	}

	return result
}

// TouchAccess increments access_count for all entries whose ID is in ids.
func (s *MemoryStore) TouchAccess(ids []string) {
	if len(ids) == 0 {
		return
	}

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	touched := false
	for i := range s.entries {
		if _, ok := idSet[s.entries[i].ID]; ok {
			s.entries[i].AccessCount++
			touched = true
		}
	}

	if touched {
		s.dirty = true
		s.signalSave()
	}
}

// UserFactSummary returns a compressed one-line summary of all user_fact
// entries. The summary is capped at maxRunes runes to keep system prompt
// overhead predictable (~200 tokens). Original entries are NOT modified.
func (s *MemoryStore) UserFactSummary(maxRunes int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxRunes <= 0 {
		maxRunes = 400 // ~200 tokens for CJK
	}

	var parts []string
	for _, e := range s.entries {
		if e.Category == MemCategoryUserFact {
			parts = append(parts, strings.TrimSpace(e.Content))
		}
	}
	if len(parts) == 0 {
		return ""
	}

	summary := strings.Join(parts, " | ")
	runes := []rune(summary)
	if len(runes) > maxRunes {
		summary = string(runes[:maxRunes]) + "…"
	}
	return summary
}

// SelfIdentitySummary returns a concatenated summary of all self_identity
// memory entries. Returns empty string if none exist.
func (s *MemoryStore) SelfIdentitySummary(maxRunes int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxRunes <= 0 {
		maxRunes = 400
	}

	var parts []string
	for _, e := range s.entries {
		if e.Category == MemCategorySelfIdentity {
			parts = append(parts, strings.TrimSpace(e.Content))
		}
	}
	if len(parts) == 0 {
		return ""
	}

	summary := strings.Join(parts, " | ")
	runes := []rune(summary)
	if len(runes) > maxRunes {
		summary = string(runes[:maxRunes]) + "…"
	}
	return summary
}

// ---------------------------------------------------------------------------
// Memory Stream scoring (inspired by Stanford "Generative Agents")
//
//   Score = w1·Recency + w2·Importance + w3·Relevance
//
// Recency:    exponential decay based on hours since last update.
// Importance: category weight + log(1 + accessCount).
// Relevance:  keyword match count (content + tags) with project affinity boost.
// ---------------------------------------------------------------------------

const (
	msDecay       = 0.005 // recency decay rate per hour
	msWRecency    = 1.0
	msWImportance = 1.0
	msWRelevance  = 1.0
)

// categoryImportanceWeight returns a base importance weight for each category.
// Higher means the category is inherently more important.
func categoryImportanceWeight(c MemoryCategory) float64 {
	switch c {
	case MemCategorySelfIdentity:
		return 4.0 // self-identity — highest, never lose it
	case MemCategoryInstruction:
		return 3.0 // explicit user rules
	case MemCategoryPreference:
		return 2.0
	case MemCategoryProjectKnowledge:
		return 2.0
	case MemCategorySessionCheckpoint:
		return 1.5
	case MemCategoryConversationSummary:
		return 1.0
	default:
		return 1.0
	}
}

// memoryStreamScore computes the three-dimensional score for a memory entry.
func memoryStreamScore(e MemoryEntry, words []string, projectLower string, now time.Time) float64 {
	// --- Recency ---
	hours := now.Sub(e.UpdatedAt).Hours()
	if hours < 0 {
		hours = 0
	}
	recency := math.Exp(-msDecay * hours)

	// --- Importance ---
	importance := categoryImportanceWeight(e.Category) + math.Log1p(float64(e.AccessCount))

	// --- Relevance ---
	relevance := float64(keywordMatchCount(e, words))
	if projectLower != "" {
		for _, tag := range e.Tags {
			if strings.ToLower(tag) == projectLower {
				relevance += 3.0
				break
			}
		}
	}

	return msWRecency*recency + msWImportance*importance + msWRelevance*relevance
}

// keywordMatchCount counts how many query words appear in the entry's
// content or tags. Used as the Relevance component.
func keywordMatchCount(e MemoryEntry, words []string) int {
	score := 0
	contentLower := strings.ToLower(e.Content)
	tagsLower := make([]string, len(e.Tags))
	for i, t := range e.Tags {
		tagsLower[i] = strings.ToLower(t)
	}
	for _, w := range words {
		if w == "" {
			continue
		}
		if strings.Contains(contentLower, w) {
			score++
		}
		for _, tag := range tagsLower {
			if strings.Contains(tag, w) {
				score++
				break
			}
		}
	}
	return score
}

// RecallDynamic retrieves memory entries matching the given query, excluding
// user_fact entries (which are injected separately as a compressed summary).
// It uses the Memory Stream scoring algorithm (Recency + Importance + Relevance)
// and supports optional category filtering with a token budget.
func (s *MemoryStore) RecallDynamic(query string, category MemoryCategory, projectPath string) []MemoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const maxEntries = 15
	const maxTokens = 1500

	words := strings.Fields(strings.ToLower(query))
	projectLower := strings.ToLower(projectPath)
	now := time.Now()

	type scored struct {
		entry MemoryEntry
		score float64
	}
	var candidates []scored

	for _, e := range s.entries {
		if e.Category == MemCategoryUserFact {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		sc := memoryStreamScore(e, words, projectLower, now)
		candidates = append(candidates, scored{entry: e, score: sc})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	var result []MemoryEntry
	tokenBudget := maxTokens
	for _, sc := range candidates {
		if len(result) >= maxEntries {
			break
		}
		tokens := len(sc.entry.Content) / 4
		if tokens > tokenBudget {
			continue
		}
		tokenBudget -= tokens
		result = append(result, sc.entry)
	}
	return result
}

// relevanceScore is kept for backward compatibility with RecallForProject.
// It counts how many words from the query appear in the entry's content/tags.
func relevanceScore(e MemoryEntry, words []string) int {
	return keywordMatchCount(e, words)
}

// evictLRU removes entries that exceed maxItems. It evicts entries with the
// lowest access_count first, breaking ties by oldest updated_at.
// Must be called with s.mu held.
func (s *MemoryStore) evictLRU() {
	if len(s.entries) <= s.maxItems {
		return
	}

	// Separate protected (self_identity) entries — they are never evicted.
	var protectedEntries []MemoryEntry
	var evictable []MemoryEntry
	for _, e := range s.entries {
		if e.Category.IsProtected() {
			protectedEntries = append(protectedEntries, e)
		} else {
			evictable = append(evictable, e)
		}
	}

	target := s.maxItems - len(protectedEntries)
	if target < 0 {
		target = 0
	}

	if len(evictable) <= target {
		return
	}

	excess := len(evictable) - target

	// Build an index sorted by eviction priority: lowest access_count first,
	// then oldest updated_at.
	indices := make([]int, len(evictable))
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(a, b int) bool {
		ea, eb := evictable[indices[a]], evictable[indices[b]]
		if ea.AccessCount != eb.AccessCount {
			return ea.AccessCount < eb.AccessCount
		}
		return ea.UpdatedAt.Before(eb.UpdatedAt)
	})

	// Mark the first `excess` indices for removal.
	remove := make(map[int]struct{}, excess)
	for i := 0; i < excess; i++ {
		remove[indices[i]] = struct{}{}
	}

	// Rebuild entries: protected first, then surviving evictable.
	kept := make([]MemoryEntry, 0, s.maxItems)
	kept = append(kept, protectedEntries...)
	for i, e := range evictable {
		if _, ok := remove[i]; !ok {
			kept = append(kept, e)
		}
	}
	s.entries = kept
}

// persistLoop runs as a background goroutine. It waits for a save signal,
// then debounces for 5 seconds before flushing to disk. This coalesces
// rapid successive writes into a single IO operation.
func (s *MemoryStore) persistLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.saveCh:
			// Debounce: wait 5 seconds, draining any additional signals.
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-s.stopCh:
				timer.Stop()
				return
			case <-timer.C:
			}
			// Drain any signals that arrived during the debounce window.
			select {
			case <-s.saveCh:
			default:
			}
			_ = s.flush()
		}
	}
}

// Stop gracefully shuts down the persistence loop. It flushes any dirty
// data to disk, then signals the persistLoop goroutine to exit.
// Safe to call multiple times.
func (s *MemoryStore) Stop() {
	s.stopOnce.Do(func() {
		s.mu.RLock()
		dirty := s.dirty
		s.mu.RUnlock()

		if dirty {
			_ = s.flush()
			s.mu.Lock()
			s.dirty = false
			s.mu.Unlock()
		}

		close(s.stopCh)
	})
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// load reads entries from the JSON file on disk. If the file does not exist,
// it starts with an empty slice (not an error).
func (s *MemoryStore) load() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("memory_store: create dir: %w", err)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // first run, nothing to load
		}
		return fmt.Errorf("memory_store: read file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var entries []MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Memory file is corrupted — back it up and start fresh so the
		// application can still launch with an empty memory store instead
		// of failing entirely.
		backupPath := s.path + ".corrupt." + time.Now().Format("20060102_150405")
		_ = os.WriteFile(backupPath, data, 0o644)
		fmt.Printf("[memory_store] WARNING: corrupted memory file backed up to %s, starting with empty memory\n", backupPath)
		s.entries = make([]MemoryEntry, 0)
		return nil
	}
	s.entries = entries
	return nil
}

// flush writes the current entries to disk as JSON.
func (s *MemoryStore) flush() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.entries, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("memory_store: marshal: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o644); err != nil {
		return fmt.Errorf("memory_store: write file: %w", err)
	}
	s.mu.Lock()
	s.dirty = false
	s.mu.Unlock()
	return nil
}

// signalSave sends a non-blocking signal to the save channel.
func (s *MemoryStore) signalSave() {
	select {
	case s.saveCh <- struct{}{}:
	default:
	}
}

// containsKeyword checks if the entry's content or tags contain the keyword.
func containsKeyword(e MemoryEntry, kw string) bool {
	if strings.Contains(strings.ToLower(e.Content), kw) {
		return true
	}
	for _, tag := range e.Tags {
		if strings.Contains(strings.ToLower(tag), kw) {
			return true
		}
	}
	return false
}

// mergeTags combines two tag slices, removing duplicates.
func mergeTags(existing, incoming []string) []string {
	set := make(map[string]struct{}, len(existing)+len(incoming))
	for _, t := range existing {
		set[t] = struct{}{}
	}
	for _, t := range incoming {
		set[t] = struct{}{}
	}
	merged := make([]string, 0, len(set))
	for t := range set {
		merged = append(merged, t)
	}
	sort.Strings(merged)
	return merged
}
