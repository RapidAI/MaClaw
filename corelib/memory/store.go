package memory

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
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
	bm25     *bm25Index
	vecIndex *vectorIndex
	graph    *memoryGraph
	embedder embedding.Embedder // nil until SetEmbedder is called
	archive  *ArchiveStore      // cold storage for evicted entries
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
		bm25:     newBM25Index(),
		vecIndex: newVectorIndex(),
		graph:    newMemoryGraph(),
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	// Build indices from loaded entries.
	s.bm25.rebuild(s.entries)
	s.vecIndex.rebuild(s.entries)
	s.graph.rebuild(s.entries)

	// Initialize archive store in the same directory.
	archivePath := filepath.Join(filepath.Dir(absPath), "archive.json")
	archive, err := NewArchiveStore(archivePath)
	if err != nil {
		return nil, fmt.Errorf("memory_store: init archive: %w", err)
	}
	s.archive = archive

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
			s.bm25.updateEntry(s.entries[i])
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
	if entry.Strength == 0 {
		entry.Strength = 1.0
	}
	if entry.Scope == "" {
		entry.Scope = InferScope(entry.Category)
	}

	s.entries = append(s.entries, entry)
	s.bm25.addEntry(entry)
	s.vecIndex.add(entry.ID, entry.Embedding)

	// Auto-link: find related entries and create graph edges.
	s.autoLink(entry)

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
			s.entries[i].CompactForm = "" // invalidate: content changed
			s.entries[i].UpdatedAt = time.Now()
			s.bm25.updateEntry(s.entries[i])
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
			s.bm25.removeEntry(id)
			s.vecIndex.remove(id)
			s.graph.remove(id)
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
// message, with optional project path affinity boosting. Uses Memory Stream
// scoring (Recency + Importance + BM25+Vector Relevance) for the general tier.
// Filters out dormant and superseded entries. Respects Scope for project filtering.
// Performs 1-hop graph expansion on top matches.
func (s *Store) RecallForProject(userMessage, projectPath string) []Entry {
	// Compute BM25 scores outside the store lock to avoid nested locking.
	bm25Scores := s.bm25.score(userMessage)

	// Compute vector scores if available.
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(userMessage))

	s.mu.RLock()
	defer s.mu.RUnlock()

	const maxEntries = 20
	const maxTokens = 2000

	projectLower := strings.ToLower(projectPath)
	now := time.Now()

	var selfIdentity []Entry
	var userFacts []Entry
	var others []recallScored

	for _, e := range s.entries {
		// Skip inactive entries.
		if !e.IsActive() {
			continue
		}
		// Scope filtering: project-scoped entries only match their project.
		if e.Scope == ScopeProject && projectLower != "" {
			matched := false
			for _, tag := range e.Tags {
				if strings.ToLower(tag) == projectLower {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		if e.Category == CategorySelfIdentity {
			selfIdentity = append(selfIdentity, e)
			continue
		}
		if e.Category == CategoryUserFact {
			userFacts = append(userFacts, e)
			continue
		}

		// Fused relevance: 0.4×BM25 + 0.6×cosine + project affinity.
		bm25 := bm25Scores[e.ID]
		cosine := 0.0
		if vs, ok := vecScores[e.ID]; ok {
			cosine = vs
		}
		fusedRelevance := 0.4*bm25 + 0.6*cosine
		if projectLower != "" {
			for _, tag := range e.Tags {
				if strings.ToLower(tag) == projectLower {
					fusedRelevance += 3.0
					break
				}
			}
		}

		sc := memoryStreamScore(e, fusedRelevance, "", now)
		others = append(others, recallScored{entry: e, score: sc})
	}

	sort.SliceStable(others, func(i, j int) bool {
		if others[i].score != others[j].score {
			return others[i].score > others[j].score
		}
		return others[i].entry.AccessCount > others[j].entry.AccessCount
	})

	// 1-hop graph expansion: expand top candidates to discover related entries.
	others = s.graphExpand(others, graphExpandSeeds)

	var result []Entry
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

	for _, sc := range others {
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
	return s.categorySummary(CategorySelfIdentity, maxRunes)
}

// UserFactSummary returns a compressed one-line summary of all user_fact
// entries. The summary is capped at maxRunes runes to keep system prompt
// overhead predictable (~200 tokens). Original entries are NOT modified.
func (s *Store) UserFactSummary(maxRunes int) string {
	return s.categorySummary(CategoryUserFact, maxRunes)
}

// DisplayContent returns CompactForm if available, otherwise Content.
// Use this when rendering memory entries for LLM context injection.
func DisplayContent(e Entry) string {
	if e.CompactForm != "" {
		return e.CompactForm
	}
	return e.Content
}

// categorySummary joins all entries of the given category into a pipe-separated
// string, capped at maxRunes. Prefers CompactForm when available.
func (s *Store) categorySummary(cat Category, maxRunes int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxRunes <= 0 {
		maxRunes = 400
	}

	var parts []string
	for _, e := range s.entries {
		if e.Category == cat {
			text := strings.TrimSpace(e.CompactForm)
			if text == "" {
				text = strings.TrimSpace(e.Content)
			}
			parts = append(parts, text)
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
// Relevance:  BM25 score against query + project affinity boost.
// ---------------------------------------------------------------------------

const (
	msDecay       = 0.005 // recency decay rate per hour
	msWRecency    = 1.0
	msWImportance = 1.0
	msWRelevance  = 1.0
)

// graphExpandSeeds is the number of top-scored entries used as seeds for
// 1-hop graph expansion during Recall.
const graphExpandSeeds = 5

// recallScored pairs an entry with its computed recall score.
type recallScored struct {
	entry Entry
	score float64
}

// CategoryImportanceWeight returns a base importance weight for each category.
func CategoryImportanceWeight(c Category) float64 {
	switch c {
	case CategorySelfIdentity:
		return 4.0
	case CategoryInstruction:
		return 3.0
	case CategoryPreference:
		return 2.0
	case CategoryProjectKnowledge:
		return 2.0
	case CategorySessionCheckpoint:
		return 1.5
	case CategoryConversationSummary:
		return 1.0
	default:
		return 1.0
	}
}

// memoryStreamScore computes the three-dimensional score for a memory entry.
func memoryStreamScore(e Entry, bm25Score float64, projectLower string, now time.Time) float64 {
	// --- Recency ---
	hours := now.Sub(e.UpdatedAt).Hours()
	if hours < 0 {
		hours = 0
	}
	recency := math.Exp(-msDecay * hours)

	// --- Importance ---
	importance := CategoryImportanceWeight(e.Category) + math.Log1p(float64(e.AccessCount))

	// --- Relevance (BM25 + project affinity) ---
	relevance := bm25Score
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

// graphExpand performs 1-hop graph expansion on the top-scored entries.
// It takes the top `seedCount` entries as seeds, expands via the memory graph,
// and merges any newly discovered entries (with derived scores) back into the
// candidate list. Already-present entries are not duplicated.
// Caller MUST hold s.mu.RLock.
func (s *Store) graphExpand(candidates []recallScored, seedCount int) []recallScored {
	if len(candidates) == 0 {
		return candidates
	}
	if seedCount > len(candidates) {
		seedCount = len(candidates)
	}

	// Collect seed IDs and their scores.
	seedIDs := make([]string, seedCount)
	seedScores := make(map[string]float64, seedCount)
	for i := 0; i < seedCount; i++ {
		seedIDs[i] = candidates[i].entry.ID
		seedScores[candidates[i].entry.ID] = candidates[i].score
	}

	// 1-hop BFS expansion. expand() returns neighbor → decayed edge weight.
	expanded := s.graph.expand(seedIDs, 1)
	if len(expanded) == 0 {
		return candidates
	}

	// Build set of IDs already in candidates for deduplication.
	existing := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		existing[c.entry.ID] = true
	}

	// Build entry lookup for quick access.
	entryByID := make(map[string]*Entry, len(s.entries))
	for i := range s.entries {
		entryByID[s.entries[i].ID] = &s.entries[i]
	}

	// Find the best seed score among seeds that link to each expanded neighbor.
	// Use the maximum seed score as the base for the derived score.
	// expandedWeight already includes the 0.5 decay factor from graph.expand().
	for neighborID, expandWeight := range expanded {
		if existing[neighborID] {
			continue
		}
		e, ok := entryByID[neighborID]
		if !ok || !e.IsActive() {
			continue
		}

		// Derive score: best seed score × expanded weight (which is edge_strength × 0.5).
		bestSeed := 0.0
		for _, sid := range seedIDs {
			if sc, ok := seedScores[sid]; ok && sc > bestSeed {
				bestSeed = sc
			}
		}
		derivedScore := bestSeed * expandWeight

		candidates = append(candidates, recallScored{entry: *e, score: derivedScore})
		existing[neighborID] = true
	}

	// Re-sort after merging expanded entries.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return candidates
}

// RecallDynamic retrieves memory entries matching the given query, excluding
// user_fact entries (which are injected separately as a compressed summary).
// Uses Memory Stream scoring with BM25+Vector fused relevance.
// Filters out dormant and superseded entries.
func (s *Store) RecallDynamic(query string, category Category, projectPath string) []Entry {
	bm25Scores := s.bm25.score(query)
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(query))

	s.mu.RLock()
	defer s.mu.RUnlock()

	const maxEntries = 15
	const maxTokens = 1500

	projectLower := strings.ToLower(projectPath)
	now := time.Now()

	var candidates []recallScored

	for _, e := range s.entries {
		if !e.IsActive() {
			continue
		}
		if e.Category == CategoryUserFact {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		bm25 := bm25Scores[e.ID]
		cosine := 0.0
		if vs, ok := vecScores[e.ID]; ok {
			cosine = vs
		}
		fusedRelevance := 0.4*bm25 + 0.6*cosine
		if projectLower != "" {
			for _, tag := range e.Tags {
				if strings.ToLower(tag) == projectLower {
					fusedRelevance += 3.0
					break
				}
			}
		}
		sc := memoryStreamScore(e, fusedRelevance, "", now)
		candidates = append(candidates, recallScored{entry: e, score: sc})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// 1-hop graph expansion: expand top candidates to discover related entries.
	candidates = s.graphExpand(candidates, graphExpandSeeds)

	var result []Entry
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

// Stop gracefully shuts down the persistence loop and the archive store.
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

		if s.archive != nil {
			s.archive.Stop()
		}
	})
}

// Archive returns the ArchiveStore for direct access.
func (s *Store) Archive() *ArchiveStore { return s.archive }

// ListArchive returns archived entries filtered by category and keyword.
func (s *Store) ListArchive(category Category, keyword string) []Entry {
	if s.archive == nil {
		return nil
	}
	return s.archive.List(category, keyword)
}

// RestoreFromArchive removes an entry from the archive and adds it back to
// active memory with UpdatedAt=now and AccessCount=1. If active memory is
// full, evictLRU runs first (which archives the lowest priority entry).
func (s *Store) RestoreFromArchive(id string) error {
	if s.archive == nil {
		return fmt.Errorf("memory_store: archive not initialized")
	}

	entry, err := s.archive.Remove(id)
	if err != nil {
		return fmt.Errorf("memory_store: %w", err)
	}

	now := time.Now()
	entry.UpdatedAt = now
	entry.AccessCount = 1

	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, *entry)
	s.bm25.addEntry(*entry)
	s.vecIndex.add(entry.ID, entry.Embedding)
	s.evictLRU()
	s.dirty = true
	s.signalSave()
	return nil
}

// ---------------------------------------------------------------------------
// Exported accessors for external compressors (e.g. GUI MemoryCompressor)
// that need low-level store access without importing unexported fields.
// ---------------------------------------------------------------------------

// RLock acquires a read lock on the store.
func (s *Store) RLock() { s.mu.RLock() }

// RUnlock releases the read lock.
func (s *Store) RUnlock() { s.mu.RUnlock() }

// Lock acquires a write lock on the store.
func (s *Store) Lock() { s.mu.Lock() }

// Unlock releases the write lock.
func (s *Store) Unlock() { s.mu.Unlock() }

// Entries returns a direct reference to the internal entries slice.
// Caller MUST hold the appropriate lock.
func (s *Store) Entries() []Entry { return s.entries }

// SetEntries replaces the internal entries slice. Caller MUST hold the write lock.
func (s *Store) SetEntries(entries []Entry) {
	s.entries = entries
	s.bm25.rebuild(entries)
	s.vecIndex.rebuild(entries)
	s.graph.rebuild(entries)
}

// MarkDirty marks the store as needing a flush.
// Caller MUST hold the write lock.
func (s *Store) MarkDirty() {
	s.dirty = true
}

// SignalSave triggers an async persist. Safe to call without lock.
func (s *Store) SignalSave() { s.signalSave() }

// Flush writes current entries to disk immediately.
func (s *Store) Flush() error { return s.flush() }

// Path returns the file path of the store.
func (s *Store) Path() string { return s.path }

// queryEmbeddingCached returns the embedding for a query string.
// Returns nil if no embedder is configured (graceful degradation).
func (s *Store) queryEmbeddingCached(query string) []float32 {
	if s.embedder == nil || embedding.IsNoop(s.embedder) {
		return nil
	}
	emb, err := s.embedder.Embed(query)
	if err != nil {
		return nil
	}
	return emb
}

// autoLinkThreshold is the minimum cosine similarity required to create a graph link.
const autoLinkThreshold = 0.7

// autoLinkTopK is the maximum number of related entries to link per save.
const autoLinkTopK = 3

// autoLink finds related entries for the newly saved entry and creates
// bidirectional graph edges. It uses BM25 scores and, when an embedder is
// available, cosine similarity to rank candidates. Only candidates above
// autoLinkThreshold are linked. The entry's RelatedIDs field is updated
// to reflect the new graph neighbors.
//
// Caller MUST hold s.mu write lock.
func (s *Store) autoLink(entry Entry) {
	if len(s.entries) <= 1 {
		return
	}

	// Gather BM25 scores for the new entry's content.
	bm25Scores := s.bm25.score(entry.Content)

	// Gather cosine similarity scores if embedding is available.
	var vecScores map[string]float64
	if len(entry.Embedding) > 0 {
		vecScores = s.vecIndex.score(entry.Embedding)
	}

	// Fuse scores: 0.4×BM25 + 0.6×cosine (same weights as Recall).
	type candidate struct {
		id    string
		score float64
	}
	var candidates []candidate
	seen := make(map[string]bool)

	// Collect all candidate IDs from both sources.
	for id := range bm25Scores {
		if id != entry.ID {
			seen[id] = true
		}
	}
	for id := range vecScores {
		if id != entry.ID {
			seen[id] = true
		}
	}

	for id := range seen {
		bm25 := bm25Scores[id]
		cosine := 0.0
		if vecScores != nil {
			cosine = vecScores[id]
		}

		var fused float64
		if vecScores != nil {
			// Both signals available: fuse them.
			fused = 0.4*bm25 + 0.6*cosine
		} else {
			// BM25 only fallback — use raw BM25 as the score.
			fused = bm25
		}

		// Apply threshold: when embedder is available, require cosine > threshold.
		// When BM25-only, require a positive BM25 score as a basic filter.
		if vecScores != nil {
			if cosine < autoLinkThreshold {
				continue
			}
		} else {
			if bm25 <= 0 {
				continue
			}
		}

		candidates = append(candidates, candidate{id: id, score: fused})
	}

	if len(candidates) == 0 {
		return
	}

	// Sort by fused score descending, take top-K.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > autoLinkTopK {
		candidates = candidates[:autoLinkTopK]
	}

	// Create graph links and determine edge strength.
	for _, c := range candidates {
		strength := c.score
		// When cosine is available, use it directly as edge strength.
		if vecScores != nil {
			if cs, ok := vecScores[c.id]; ok {
				strength = cs
			}
		}
		s.graph.link(entry.ID, c.id, strength)
	}

	// Update RelatedIDs on the new entry.
	relIDs := s.graph.relatedIDsFor(entry.ID)
	for i := range s.entries {
		if s.entries[i].ID == entry.ID {
			s.entries[i].RelatedIDs = relIDs
			break
		}
	}

	// Also update RelatedIDs on the linked entries (bidirectional).
	for _, c := range candidates {
		neighborRels := s.graph.relatedIDsFor(c.id)
		for i := range s.entries {
			if s.entries[i].ID == c.id {
				s.entries[i].RelatedIDs = neighborRels
				break
			}
		}
	}
}

// SetEmbedder wires an Embedder into the store. If the embedder is real
// (not NoopEmbedder), a background goroutine is launched to compute
// embeddings for any existing entries that are missing them.
// Safe to call at most once after NewStore.
func (s *Store) SetEmbedder(e embedding.Embedder) {
	s.embedder = e
	if e == nil || embedding.IsNoop(e) {
		return
	}
	go s.backfillEmbeddings()
}

// EmbedderActive returns true if a real (non-noop) embedder is loaded.
func (s *Store) EmbedderActive() bool {
	return s.embedder != nil && !embedding.IsNoop(s.embedder)
}

// EmbedderDim returns the embedding dimension, or 0 if no embedder is active.
func (s *Store) EmbedderDim() int {
	if s.embedder == nil {
		return 0
	}
	return s.embedder.Dim()
}

// backfillEmbeddings scans entries missing embeddings and computes them
// in the background. It processes entries one at a time to avoid blocking
// the store for extended periods.
func (s *Store) backfillEmbeddings() {
	// Collect IDs and content of entries that need embeddings.
	type pending struct {
		id      string
		content string
	}

	s.mu.RLock()
	var todo []pending
	for _, e := range s.entries {
		if len(e.Embedding) == 0 && e.Content != "" {
			todo = append(todo, pending{id: e.ID, content: e.Content})
		}
	}
	s.mu.RUnlock()

	if len(todo) == 0 {
		return
	}

	updated := 0
	for _, p := range todo {
		// Check if store is shutting down.
		select {
		case <-s.stopCh:
			return
		default:
		}

		emb, err := s.embedder.Embed(p.content)
		if err != nil || len(emb) == 0 {
			continue
		}

		s.mu.Lock()
		for i := range s.entries {
			if s.entries[i].ID == p.id && len(s.entries[i].Embedding) == 0 {
				s.entries[i].Embedding = emb
				s.vecIndex.add(p.id, emb)
				updated++
				break
			}
		}
		s.mu.Unlock()
	}

	if updated > 0 {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		s.signalSave()
	}
}

// Graph returns the memory graph for external access (e.g. CLI commands).
func (s *Store) Graph() *memoryGraph { return s.graph }

// Embedder returns the configured embedder (may be nil).
func (s *Store) Embedder() embedding.Embedder { return s.embedder }

// GraphNeighbors returns the direct neighbors and edge weights for the given entry ID.
func (s *Store) GraphNeighbors(id string) map[string]float64 {
	return s.graph.neighborsOf(id)
}


// PinEntry sets Pinned=true for the entry with the given ID.
func (s *Store) PinEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, e := range s.entries {
		if e.ID == id {
			s.entries[i].Pinned = true
			s.entries[i].UpdatedAt = time.Now()
			s.dirty = true
			s.signalSave()
			return nil
		}
	}
	return fmt.Errorf("entry %q not found", id)
}

// UnpinEntry sets Pinned=false for the entry with the given ID.
func (s *Store) UnpinEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, e := range s.entries {
		if e.ID == id {
			s.entries[i].Pinned = false
			s.entries[i].UpdatedAt = time.Now()
			s.dirty = true
			s.signalSave()
			return nil
		}
	}
	return fmt.Errorf("entry %q not found", id)
}

// ActiveCount returns the number of active entries in the store.
func (s *Store) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func (s *Store) evictLRU() {
	if len(s.entries) <= s.maxItems {
		return
	}

	// Separate protected (self_identity) and pinned entries — they are never evicted.
	var protectedEntries []Entry
	var evictable []Entry
	for _, e := range s.entries {
		if e.Category.IsProtected() || e.Pinned {
			protectedEntries = append(protectedEntries, e)
		} else {
			evictable = append(evictable, e)
		}
	}

	target := s.maxItems - len(protectedEntries)
	if target < 0 {
		// Protected entries alone exceed maxItems — nothing else can be kept.
		log.Printf("[memory_store] WARNING: %d protected entries exceed maxItems (%d)", len(protectedEntries), s.maxItems)
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

	// Collect evicted entries for archiving.
	var evicted []Entry
	kept := make([]Entry, 0, s.maxItems)
	kept = append(kept, protectedEntries...)
	for i, e := range evictable {
		if _, ok := remove[i]; ok {
			evicted = append(evicted, e)
		} else {
			kept = append(kept, e)
		}
	}
	s.entries = kept
	s.bm25.rebuild(kept)
	s.vecIndex.rebuild(kept)
	s.graph.rebuild(kept)

	// Archive evicted entries instead of discarding them.
	if s.archive != nil && len(evicted) > 0 {
		_ = s.archive.Add(evicted...)
	}
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

// MergeTags combines two tag slices, removing duplicates.
func MergeTags(existing, incoming []string) []string {
	return mergeTags(existing, incoming)
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
