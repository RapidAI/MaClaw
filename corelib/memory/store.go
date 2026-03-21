package memory

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store provides persistent long-term memory storage.
type Store struct {
	mu       sync.RWMutex
	entries  []Entry
	path     string
	dirty    bool
	saveCh   chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	maxItems int
}

// NewStore creates a Store that persists to the given path.
func NewStore(path string) (*Store, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("memory_store: resolve path: %w", err)
	}

	s := &Store{
		entries:  make([]Entry, 0),
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

func generateID() string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%04x", time.Now().UnixNano(), int(buf[0])<<8|int(buf[1]))
}

// Save stores a memory entry. If an entry with identical content already
// exists, it updates that entry instead of creating a duplicate.
func (s *Store) Save(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	for i := range s.entries {
		if s.entries[i].Content == entry.Content {
			s.entries[i].UpdatedAt = now
			s.entries[i].AccessCount++
			s.entries[i].Tags = mergeTags(s.entries[i].Tags, entry.Tags)
			s.dirty = true
			s.signalSave()
			return nil
		}
	}

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

// Update modifies an existing entry identified by ID.
func (s *Store) Update(id string, content string, category Category, tags []string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("memory_store: content must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

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

// Delete removes the entry with the given ID.
func (s *Store) Delete(id string) error {
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
func (s *Store) List(category Category, keyword string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	kw := strings.ToLower(keyword)
	var result []Entry
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

// Search returns entries filtered by category and keyword with a limit.
func (s *Store) Search(category Category, keyword string, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	kw := strings.ToLower(keyword)
	var result []Entry
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

// Recall retrieves memory entries relevant to the given user message.
func (s *Store) Recall(userMessage string) []Entry {
	return s.RecallForProject(userMessage, "")
}

// RecallForProject retrieves memory entries relevant to the given user
// message, with optional project path affinity boosting.
func (s *Store) RecallForProject(userMessage, projectPath string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const maxEntries = 20
	const maxTokens = 2000

	words := strings.Fields(strings.ToLower(userMessage))
	projectLower := strings.ToLower(projectPath)
	now := time.Now()

	var selfIdentity []Entry
	var userFacts []Entry
	type scored struct {
		entry Entry
		score int
	}
	var others []scored

	for _, e := range s.entries {
		if e.Category == CategorySelfIdentity {
			selfIdentity = append(selfIdentity, e)
			continue
		}
		if e.Category == CategoryUserFact {
			userFacts = append(userFacts, e)
			continue
		}
		sc := relevanceScore(e, words)

		if projectLower != "" {
			for _, tag := range e.Tags {
				if strings.ToLower(tag) == projectLower {
					sc += 3
					break
				}
			}
		}

		age := now.Sub(e.UpdatedAt)
		if age > 7*24*time.Hour {
			weeks := int(age.Hours() / (24 * 7))
			sc -= weeks
		}

		if e.Category == CategorySessionCheckpoint && age < 24*time.Hour {
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

	var result []Entry
	tokenBudget := maxTokens

	// Self-identity memories are always recalled first — highest priority.
	// They are never skipped due to token budget constraints.
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

	for _, sc := range others {
		if len(result) >= maxEntries {
			break
		}
		if sc.score < 0 {
			continue
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

// TouchAccess increments access_count for all entries whose ID is in ids.
func (s *Store) TouchAccess(ids []string) {
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

// SelfIdentitySummary returns a concatenated summary of all self_identity
// memory entries. Returns empty string if none exist.
func (s *Store) SelfIdentitySummary(maxRunes int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxRunes <= 0 {
		maxRunes = 400
	}

	var parts []string
	for _, e := range s.entries {
		if e.Category == CategorySelfIdentity {
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

// Stop gracefully shuts down the persistence loop.
func (s *Store) Stop() {
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

func relevanceScore(e Entry, words []string) int {
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

func (s *Store) evictLRU() {
	if len(s.entries) <= s.maxItems {
		return
	}

	// Separate protected (self_identity) entries — they are never evicted.
	var protectedEntries []Entry
	var evictable []Entry
	for _, e := range s.entries {
		if e.Category.IsProtected() {
			protectedEntries = append(protectedEntries, e)
		} else {
			evictable = append(evictable, e)
		}
	}

	target := s.maxItems - len(protectedEntries)
	if target < 0 {
		// Protected entries alone exceed maxItems — nothing else can be kept.
		fmt.Printf("[memory_store] WARNING: %d protected entries exceed maxItems (%d)\n", len(protectedEntries), s.maxItems)
		target = 0
	}
	if len(evictable) <= target {
		return
	}

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

	excess := len(evictable) - target
	remove := make(map[int]struct{}, excess)
	for i := 0; i < excess; i++ {
		remove[indices[i]] = struct{}{}
	}

	kept := make([]Entry, 0, s.maxItems)
	kept = append(kept, protectedEntries...)
	for i, e := range evictable {
		if _, ok := remove[i]; !ok {
			kept = append(kept, e)
		}
	}
	s.entries = kept
}

func (s *Store) persistLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.saveCh:
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-s.stopCh:
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case <-s.saveCh:
			default:
			}
			_ = s.flush()
		}
	}
}

func (s *Store) load() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("memory_store: create dir: %w", err)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("memory_store: read file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		backupPath := s.path + ".corrupt." + time.Now().Format("20060102_150405")
		_ = os.WriteFile(backupPath, data, 0o644)
		fmt.Printf("[memory_store] WARNING: corrupted memory file backed up to %s, starting with empty memory\n", backupPath)
		s.entries = make([]Entry, 0)
		return nil
	}
	s.entries = entries
	return nil
}

func (s *Store) flush() error {
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

func (s *Store) signalSave() {
	select {
	case s.saveCh <- struct{}{}:
	default:
	}
}

func containsKeyword(e Entry, kw string) bool {
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
