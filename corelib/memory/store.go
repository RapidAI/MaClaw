package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
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
	tmt      *TemporalTree
	gating   *RecallGating
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
		tmt:      NewTemporalTree(),
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	// Build indices from loaded entries.
	s.bm25.rebuild(s.entries)
	s.vecIndex.rebuild(s.entries)
	s.graph.rebuild(s.entries)
	if s.tmt != nil {
		s.tmt.Rebuild(s.entries)
	}

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

// computeContentHash returns the SHA-256 hex digest of content.
func computeContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// Save stores a memory entry. If an entry with identical content hash already
// exists, it updates that entry instead of creating a duplicate.
// Content is scanned for prompt injection patterns before saving.
func (s *Store) Save(entry Entry) error {
	if err := ScanForInjection(entry.Content); err != nil {
		return fmt.Errorf("memory_store: rejected: %w", err)
	}

	hash := computeContentHash(entry.Content)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Idempotent: check by content hash first (O(n) but fast string compare).
	for i := range s.entries {
		if s.entries[i].ContentHash == hash || s.entries[i].Content == entry.Content {
			s.entries[i].UpdatedAt = now
			s.entries[i].AccessCount++
			s.entries[i].Tags = mergeTags(s.entries[i].Tags, entry.Tags)
			if s.entries[i].ContentHash == "" {
				s.entries[i].ContentHash = hash
			}
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
	entry.ContentHash = hash
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
// Content is scanned for prompt injection patterns before updating.
func (s *Store) Update(id string, content string, category Category, tags []string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("memory_store: content must not be empty")
	}
	if err := ScanForInjection(content); err != nil {
		return fmt.Errorf("memory_store: rejected: %w", err)
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
			// Save version snapshot (keep last 3).
			if e.Content != content {
				snap := VersionSnapshot{Content: e.Content, Timestamp: e.UpdatedAt}
				prev := s.entries[i].Versions
				versions := make([]VersionSnapshot, 0, 3)
				// Keep at most the last 2 existing + the new one = 3 total.
				start := 0
				if len(prev) > 2 {
					start = len(prev) - 2
				}
				versions = append(versions, prev[start:]...)
				versions = append(versions, snap)
				s.entries[i].Versions = versions
			}
			s.entries[i].Content = content
			s.entries[i].Category = category
			s.entries[i].Tags = tags
			s.entries[i].CompactForm = "" // invalidate: content changed
			s.entries[i].ContentHash = computeContentHash(content)
			s.entries[i].UpdatedAt = time.Now()
			s.entries[i].Stale = false // content just updated, clear stale flag
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
// message, with optional project path affinity boosting. Uses RRF (Reciprocal
// Rank Fusion) to combine BM25 and vector search rankings, then applies Memory
// Stream scoring (Recency + Importance + RRF Relevance) for the general tier.
// Filters out dormant and superseded entries. Respects Scope for project filtering.
// Performs 1-hop graph expansion on top matches.
func (s *Store) RecallForProject(userMessage, projectPath string) []Entry {
	// === Phase 1: Query Understanding ===
	expanded := ExpandQuery(userMessage)

	// === Phase 2: Multi-Query BM25 ===
	bm25Scores := s.multiQueryBM25(userMessage, expanded.Entities)

	// Compute vector scores if available (use original message — embeddings understand semantics).
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(userMessage))

	// Hold RLock for the scoring/assembly phase, then release before TouchAccess.
	result := s.recallForProjectLocked(userMessage, bm25Scores, vecScores, expanded.QueryTokens, projectPath)

	// Touch access counts outside the lock to avoid RLock→Lock deadlock.
	if len(result) > 0 {
		ids := make([]string, len(result))
		for i, e := range result {
			ids[i] = e.ID
		}
		go s.TouchAccess(ids)
	}

	return result
}

// recallForProjectLocked performs the scoring and assembly phase of RecallForProject.
// Caller must NOT hold any lock — this method acquires RLock internally.
func (s *Store) recallForProjectLocked(query string, bm25Scores map[string]float64, vecScores map[string]float64, queryTokens []string, projectPath string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// === Phase 3: Dynamic Budget ===
	activeCount := s.activeCountLocked()
	maxTokens, maxEntries := dynamicBudget(activeCount)

	projectLower := strings.ToLower(projectPath)
	now := time.Now()

	var selfIdentity []Entry
	var userFacts []Entry

	// Collect candidate IDs for RRF ranking.
	type candidate struct {
		entry Entry
		bm25  float64
		vec   float64
	}
	var candidates []candidate

	for _, e := range s.entries {
		// Skip inactive entries.
		if !e.IsActive() {
			continue
		}
		// Scope filtering: project-scoped entries are only excluded when they
		// are explicitly bound to a DIFFERENT project. An entry is considered
		// bound to a project if it has a tag that looks like a directory path
		// (starts with / or X:\). Entries without any path-like tags are
		// treated as globally visible — they may be project_knowledge but
		// not tied to a specific project (e.g. server credentials).
		if e.Scope == ScopeProject && projectLower != "" {
			boundToOtherProject := false
			for _, tag := range e.Tags {
				tl := strings.ToLower(tag)
				// Heuristic: a tag is a project path if it starts with / or drive letter
				isPath := (len(tl) > 1 && tl[0] == '/') ||
					(len(tl) > 2 && tl[1] == ':' && (tl[2] == '/' || tl[2] == '\\'))
				if isPath {
					if tl != projectLower && !strings.HasPrefix(projectLower, tl) {
						boundToOtherProject = true
					} else {
						// Matches current project — not bound to other
						boundToOtherProject = false
						break
					}
				}
			}
			if boundToOtherProject {
				continue
			}
		}

		if e.Category == CategorySelfIdentity {
			selfIdentity = append(selfIdentity, e)
			continue
		}
		canonical := MapToCanonical(e.Category)
		if e.Category == CategoryUserFact || canonical == CategoryUserFact {
			userFacts = append(userFacts, e)
			continue
		}

		b := bm25Scores[e.ID]
		v := 0.0
		if vs, ok := vecScores[e.ID]; ok {
			v = vs
		}
		candidates = append(candidates, candidate{entry: e, bm25: b, vec: v})
	}

	// === Phase 4: Three-way RRF fusion (BM25 + Vec + Tag) ===
	bm25Arr := make([]float64, len(candidates))
	vecArr := make([]float64, len(candidates))
	entryArr := make([]Entry, len(candidates))
	for i, c := range candidates {
		bm25Arr[i] = c.bm25
		vecArr[i] = c.vec
		entryArr[i] = c.entry
	}
	rrfScores := rrfFuseScores(bm25Arr, vecArr, entryArr, projectLower, queryTokens)

	var others []recallScored
	for i, c := range candidates {
		fusedRelevance := rrfScores[i]
		sc := memoryStreamScore(c.entry, fusedRelevance, "", now)
		others = append(others, recallScored{entry: c.entry, score: sc})
	}

	sort.SliceStable(others, func(i, j int) bool {
		if others[i].score != others[j].score {
			return others[i].score > others[j].score
		}
		return others[i].entry.AccessCount > others[j].entry.AccessCount
	})

	// 1-hop graph expansion: expand top candidates to discover related entries.
	others = s.graphExpand(others, graphExpandSeeds)

	// Recall gating: LLM-based post-retrieval filtering.
	if s.gating != nil {
		others = s.gating.Filter(query, others)
	}

	// === Phase 5: Type-quota assembly ===
	var result []Entry
	tokenBudget := maxTokens
	userFactBudgetCap := int(float64(maxTokens) * 0.6) // user_fact gets at most 60%

	// Self-identity memories are always recalled first — highest priority.
	for _, e := range selfIdentity {
		tokens := len(e.Content) / 4
		tokenBudget -= tokens
		result = append(result, e)
	}

	// user_fact: capped at 60% of total budget.
	userFactUsed := 0
	for _, e := range userFacts {
		if len(result) >= maxEntries {
			break
		}
		tokens := len(e.Content) / 4
		if userFactUsed+tokens > userFactBudgetCap {
			continue
		}
		if tokens > tokenBudget {
			continue
		}
		userFactUsed += tokens
		tokenBudget -= tokens
		result = append(result, e)
	}

	// Other types (project_knowledge, instruction, etc.): remaining budget.
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

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	touched := false
	for i := range s.entries {
		if _, ok := idSet[s.entries[i].ID]; ok {
			s.entries[i].AccessCount++
			// Boost forgetting curve strength so recalled memories don't decay.
			boostStrength(&s.entries[i], now)
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
// Stale entries are prefixed with [可能过时] to alert the LLM.
func DisplayContent(e Entry) string {
	text := e.CompactForm
	if text == "" {
		text = e.Content
	}
	if e.Stale {
		text = "[可能过时] " + text
	}
	return text
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
// RRF (Reciprocal Rank Fusion) — inspired by GBrain's hybrid search
//
// Instead of linearly combining BM25 and vector scores (which requires
// careful weight tuning and is sensitive to score scale differences),
// RRF ranks candidates independently by each signal, then fuses:
//
//   RRF_score = 1/(k + rank_bm25) + 1/(k + rank_vec) + project_boost
//
// k=60 is the standard constant from the original RRF paper (Cormack et al.).
// This is more robust than weighted sum because it only depends on rank order,
// not absolute score magnitudes.
// ---------------------------------------------------------------------------

// multiQueryBM25 computes BM25 scores using the original message plus
// extracted entities as independent queries. For each entry, the maximum
// score across all queries is kept. This prevents noise words in the
// original message from diluting the match score of key entities.
func (s *Store) multiQueryBM25(userMessage string, entities []string) map[string]float64 {
	primary := s.bm25.score(userMessage)
	if len(entities) == 0 {
		return primary
	}

	merged := make(map[string]float64, len(primary))
	for id, score := range primary {
		merged[id] = score
	}
	for _, entity := range entities {
		entityScores := s.bm25.score(entity)
		for id, score := range entityScores {
			if score > merged[id] {
				merged[id] = score
			}
		}
	}
	return merged
}

// dynamicBudget computes token budget and max entries based on active memory count.
func dynamicBudget(activeCount int) (maxTokens, maxEntries int) {
	switch {
	case activeCount <= 30:
		return 2000, 20
	case activeCount <= 100:
		return 3000, 25
	case activeCount <= 250:
		return 4000, 28
	default:
		return 5000, 30
	}
}

// activeCountLocked returns the number of active entries. Must be called
// while holding at least s.mu.RLock().
func (s *Store) activeCountLocked() int {
	count := 0
	for _, e := range s.entries {
		if e.IsActive() {
			count++
		}
	}
	return count
}

const rrfK = 60 // RRF smoothing constant

// rrfFuseScores computes RRF scores from parallel slices of BM25 and vector
// scores. Returns a parallel slice of fused relevance scores. Includes project
// affinity boost when projectLower is non-empty and entries have matching tags.
// When queryTokens is non-nil, adds a third tag cross-matching signal.
func rrfFuseScores(bm25Scores, vecScores []float64, entries []Entry, projectLower string, queryTokens []string) []float64 {
	n := len(bm25Scores)
	if n == 0 {
		return nil
	}

	// Build index arrays sorted by each signal descending.
	bm25Order := make([]int, n)
	vecOrder := make([]int, n)
	for i := range bm25Order {
		bm25Order[i] = i
		vecOrder[i] = i
	}

	sort.SliceStable(bm25Order, func(a, b int) bool {
		return bm25Scores[bm25Order[a]] > bm25Scores[bm25Order[b]]
	})
	sort.SliceStable(vecOrder, func(a, b int) bool {
		return vecScores[vecOrder[a]] > vecScores[vecOrder[b]]
	})

	// Assign ranks (1-based).
	bm25Rank := make([]int, n)
	vecRank := make([]int, n)
	for rank, idx := range bm25Order {
		bm25Rank[idx] = rank + 1
	}
	for rank, idx := range vecOrder {
		vecRank[idx] = rank + 1
	}

	// Tag cross-matching: compute tag scores and ranks.
	var tagRank []int
	hasTagSignal := len(queryTokens) > 0
	if hasTagSignal {
		tagScores := make([]float64, n)
		for i := range entries {
			tagScores[i] = tagCrossScore(entries[i], queryTokens)
		}
		tagOrder := make([]int, n)
		for i := range tagOrder {
			tagOrder[i] = i
		}
		sort.SliceStable(tagOrder, func(a, b int) bool {
			return tagScores[tagOrder[a]] > tagScores[tagOrder[b]]
		})
		tagRank = make([]int, n)
		for rank, idx := range tagOrder {
			tagRank[idx] = rank + 1
		}
	}

	// Compute RRF scores.
	const tagAlpha = 0.8 // tag signal weight (slightly lower than BM25/Vec)
	scores := make([]float64, n)
	for i := range scores {
		rrf := 1.0/float64(rrfK+bm25Rank[i]) + 1.0/float64(rrfK+vecRank[i])
		if hasTagSignal {
			rrf += tagAlpha / float64(rrfK+tagRank[i])
		}
		// Project affinity boost.
		if projectLower != "" {
			for _, tag := range entries[i].Tags {
				if strings.ToLower(tag) == projectLower {
					rrf += 3.0
					break
				}
			}
		}
		scores[i] = rrf
	}
	return scores
}

// tagCrossScore computes the cross-match score between user message tokens
// and a memory entry's tags.
//   - Exact match (case-insensitive): +2.0
//   - Containment match (tag contains token or vice versa, min 3 runes): +1.0
//   - Cap: 6.0
func tagCrossScore(entry Entry, queryTokens []string) float64 {
	if len(entry.Tags) == 0 || len(queryTokens) == 0 {
		return 0
	}
	score := 0.0
	for _, tag := range entry.Tags {
		tagLower := strings.ToLower(tag)
		if len([]rune(tagLower)) < 2 {
			continue
		}
		for _, token := range queryTokens {
			if token == tagLower {
				score += 2.0
				break // one match per tag
			}
			// Containment: only if both sides are ≥3 runes to avoid
			// spurious matches like tag "测" matching token "测试环境配置".
			if len([]rune(token)) >= 3 && len([]rune(tagLower)) >= 3 {
				if strings.Contains(tagLower, token) || strings.Contains(token, tagLower) {
					score += 1.0
					break
				}
			}
		}
		if score >= 6.0 {
			return 6.0
		}
	}
	return score
}

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
	// Map Claude-style categories to canonical for scoring.
	canonical := MapToCanonical(c)
	switch canonical {
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
// Uses RRF (Reciprocal Rank Fusion) with Memory Stream scoring.
// Filters out dormant and superseded entries.
func (s *Store) RecallDynamic(query string, category Category, projectPath string) []Entry {
	// Query Expand: extract entities for multi-query BM25 + tokens for tag matching.
	expanded := ExpandQuery(query)
	bm25Scores := s.multiQueryBM25(query, expanded.Entities)
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(query))

	s.mu.RLock()
	defer s.mu.RUnlock()

	const maxEntries = 15
	const maxTokens = 1500

	projectLower := strings.ToLower(projectPath)
	now := time.Now()

	type rawCandidate struct {
		entry Entry
		bm25  float64
		vec   float64
	}
	var raw []rawCandidate

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
		b := bm25Scores[e.ID]
		v := 0.0
		if vs, ok := vecScores[e.ID]; ok {
			v = vs
		}
		raw = append(raw, rawCandidate{entry: e, bm25: b, vec: v})
	}

	// Three-way RRF fusion (BM25 + Vec + Tag).
	bm25Arr := make([]float64, len(raw))
	vecArr := make([]float64, len(raw))
	entryArr := make([]Entry, len(raw))
	for i, c := range raw {
		bm25Arr[i] = c.bm25
		vecArr[i] = c.vec
		entryArr[i] = c.entry
	}
	rrfScores := rrfFuseScores(bm25Arr, vecArr, entryArr, projectLower, expanded.QueryTokens)

	var candidates []recallScored
	for i, c := range raw {
		sc := memoryStreamScore(c.entry, rrfScores[i], "", now)
		candidates = append(candidates, recallScored{entry: c.entry, score: sc})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// 1-hop graph expansion: expand top candidates to discover related entries.
	candidates = s.graphExpand(candidates, graphExpandSeeds)

	// Recall gating: LLM-based post-retrieval filtering.
	if s.gating != nil {
		candidates = s.gating.Filter(query, candidates)
	}

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

// ---------------------------------------------------------------------------
// Three-layer search (inspired by GBrain's keyword / hybrid / direct modes)
// ---------------------------------------------------------------------------

// SearchByMode dispatches to the appropriate search strategy based on mode.
func (s *Store) SearchByMode(query string, mode SearchMode, category Category, projectPath string, limit int) []Entry {
	switch mode {
	case SearchDirect:
		return s.SearchDirectByID(query)
	case SearchKeywordOnly:
		return s.SearchKeyword(query, category, limit)
	default:
		return s.RecallDynamic(query, category, projectPath)
	}
}

// SearchDirectByID returns the entry with the given ID, or nil.
func (s *Store) SearchDirectByID(id string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entries {
		if e.ID == id {
			return []Entry{e}
		}
	}
	return nil
}

// SearchKeyword performs BM25-only search without vector scoring.
func (s *Store) SearchKeyword(query string, category Category, limit int) []Entry {
	if limit <= 0 {
		limit = 15
	}
	scores := s.bm25.score(query)

	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		entry Entry
		score float64
	}
	var candidates []scored
	for _, e := range s.entries {
		if !e.IsActive() {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		sc := scores[e.ID]
		if sc > 0 {
			candidates = append(candidates, scored{entry: e, score: sc})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	var result []Entry
	for _, c := range candidates {
		if len(result) >= limit {
			break
		}
		result = append(result, c.entry)
	}
	return result
}

// ---------------------------------------------------------------------------
// Stale detection (inspired by GBrain's stale alerts)
// ---------------------------------------------------------------------------

// DetectStale scans entries and marks those that may be outdated.
// An entry is considered stale if a newer entry in the same category with
// overlapping tags exists. Returns the number of entries newly marked stale.
// Caller should hold NO lock — this method acquires its own.
func (s *Store) DetectStale() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	staleCount := 0
	n := len(s.entries)
	if n < 2 {
		return 0
	}

	for i := range s.entries {
		if !s.entries[i].IsActive() || s.entries[i].Pinned || s.entries[i].Category.IsProtected() {
			continue
		}
		// Check if a newer entry in the same category with overlapping tags exists.
		for j := range s.entries {
			if i == j || !s.entries[j].IsActive() {
				continue
			}
			if s.entries[j].Category != s.entries[i].Category {
				continue
			}
			if !s.entries[j].UpdatedAt.After(s.entries[i].UpdatedAt) {
				continue
			}
			if !hasOverlappingTags(s.entries[i].Tags, s.entries[j].Tags) {
				continue
			}
			// Newer entry with same category and overlapping tags exists.
			if !s.entries[i].Stale {
				s.entries[i].Stale = true
				staleCount++
			}
			break
		}
	}

	if staleCount > 0 {
		s.dirty = true
		s.signalSave()
	}
	return staleCount
}

// ClearStale removes the stale flag from all entries. Returns count cleared.
func (s *Store) ClearStale() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleared := 0
	for i := range s.entries {
		if s.entries[i].Stale {
			s.entries[i].Stale = false
			cleared++
		}
	}
	if cleared > 0 {
		s.dirty = true
		s.signalSave()
	}
	return cleared
}

func hasOverlappingTags(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		// If either has no tags, consider them potentially overlapping
		// (same category is already a strong signal).
		return true
	}
	set := make(map[string]struct{}, len(a))
	for _, t := range a {
		set[strings.ToLower(t)] = struct{}{}
	}
	for _, t := range b {
		if _, ok := set[strings.ToLower(t)]; ok {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Dream Cycle — background self-healing (inspired by GBrain's dream cycle)
//
// Runs during the Compressor's periodic loop. Performs:
// 1. Stale detection
// 2. Auto-link discovery for unlinked but related entries
// 3. Content hash backfill for entries missing hashes
// ---------------------------------------------------------------------------

// DreamCycle performs a background self-healing pass over all entries.
// Safe to call from the Compressor's periodic loop.
func (s *Store) DreamCycle() *DreamCycleResult {
	result := &DreamCycleResult{}

	// Phase 1: Stale detection.
	result.StaleDetected = s.DetectStale()

	// Phase 2: Auto-link discovery — find high-BM25 pairs that aren't linked.
	result.LinksDiscovered = s.discoverMissingLinks()

	// Phase 3: Content hash backfill.
	result.HashesBackfilled = s.backfillContentHashes()

	// Phase 4: Tag backfill — enrich old entries that have poor tags.
	result.TagsBackfilled = s.backfillTags()

	if result.StaleDetected > 0 || result.LinksDiscovered > 0 || result.HashesBackfilled > 0 || result.TagsBackfilled > 0 {
		log.Printf("[memory_dream] stale=%d links=%d hashes=%d tags=%d",
			result.StaleDetected, result.LinksDiscovered, result.HashesBackfilled, result.TagsBackfilled)
	}

	return result
}

// discoverMissingLinks scans for entry pairs with high BM25 similarity
// that are not yet linked in the graph. Creates links for top candidates.
// Returns the number of new links created.
func (s *Store) discoverMissingLinks() int {
	s.mu.RLock()
	if len(s.entries) < 2 {
		s.mu.RUnlock()
		return 0
	}
	// Sample up to 50 entries to avoid O(n²) on large stores.
	// Copy the sample to avoid holding references to mutable entries.
	src := s.entries
	if len(src) > 50 {
		src = src[len(src)-50:]
	}
	sample := make([]Entry, len(src))
	copy(sample, src)

	// Build a tag→entryIDs index for tag-overlap link discovery.
	tagIndex := make(map[string][]string) // tag (lowered) → entry IDs
	for _, e := range s.entries {
		if !e.IsActive() {
			continue
		}
		for _, tag := range e.Tags {
			tl := strings.ToLower(tag)
			if len([]rune(tl)) >= 2 {
				tagIndex[tl] = append(tagIndex[tl], e.ID)
			}
		}
	}
	s.mu.RUnlock()

	created := 0
	for _, e := range sample {
		if !e.IsActive() || e.Category.IsProtected() {
			continue
		}
		scores := s.bm25.score(e.Content)
		neighbors := s.graph.neighborsOf(e.ID)

		// Find top BM25 candidate not already linked.
		bestID := ""
		bestScore := 0.0
		for id, sc := range scores {
			if id == e.ID || sc <= 0 {
				continue
			}
			if _, linked := neighbors[id]; linked {
				continue
			}
			if sc > bestScore {
				bestScore = sc
				bestID = id
			}
		}
		if bestID != "" && bestScore > 1.0 {
			s.graph.link(bestID, e.ID, bestScore)
			created++
		}

		// Tag-overlap link discovery: entries sharing ≥2 meaningful tags
		// should be linked even if their content text is dissimilar.
		tagOverlapCounts := make(map[string]int) // other entry ID → shared tag count
		for _, tag := range e.Tags {
			tl := strings.ToLower(tag)
			for _, otherID := range tagIndex[tl] {
				if otherID != e.ID {
					tagOverlapCounts[otherID]++
				}
			}
		}
		for otherID, count := range tagOverlapCounts {
			if count >= 2 {
				if _, linked := neighbors[otherID]; !linked {
					s.graph.link(e.ID, otherID, float64(count)*0.3)
					created++
				}
			}
		}
	}

	if created > 0 {
		// Update RelatedIDs on affected entries.
		s.mu.Lock()
		for i := range s.entries {
			newRels := s.graph.relatedIDsFor(s.entries[i].ID)
			if len(newRels) != len(s.entries[i].RelatedIDs) {
				s.entries[i].RelatedIDs = newRels
			}
		}
		s.dirty = true
		s.mu.Unlock()
		s.signalSave()
	}
	return created
}

// backfillContentHashes computes SHA-256 hashes for entries missing them.
func (s *Store) backfillContentHashes() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for i := range s.entries {
		if s.entries[i].ContentHash == "" && s.entries[i].Content != "" {
			s.entries[i].ContentHash = computeContentHash(s.entries[i].Content)
			count++
		}
	}
	if count > 0 {
		s.dirty = true
		s.signalSave()
	}
	return count
}

// backfillTags enriches entries that have poor tags (empty or only generic
// tags like "extracted") by extracting entities from their content using
// ExpandQuery. This ensures old entries benefit from the tag fast lane.
// Processes up to 30 entries per cycle to avoid blocking.
func (s *Store) backfillTags() int {
	// Minimal tag sets that indicate the entry needs enrichment.
	needsEnrichment := func(tags []string) bool {
		if len(tags) == 0 {
			return true
		}
		meaningful := 0
		for _, t := range tags {
			switch t {
			case "extracted", "conversation_summary", "proactive":
				continue // generic tags don't count
			default:
				// Date-like tags (e.g. "2026-04-12") don't count.
				if len(t) == 10 && t[4] == '-' && t[7] == '-' {
					continue
				}
				// UserID-like tags (contain "user" or "-") are often not meaningful.
				if strings.Contains(strings.ToLower(t), "user") {
					continue
				}
				meaningful++
			}
		}
		return meaningful == 0
	}

	// Collect candidates outside the write lock. Store ID, not index.
	s.mu.RLock()
	type pending struct {
		id      string
		content string
	}
	var todo []pending
	for _, e := range s.entries {
		if !e.IsActive() || e.Category.IsProtected() {
			continue
		}
		if len(e.Content) < 10 {
			continue
		}
		if needsEnrichment(e.Tags) {
			todo = append(todo, pending{id: e.ID, content: e.Content})
		}
	}
	s.mu.RUnlock()

	if len(todo) == 0 {
		return 0
	}
	if len(todo) > 30 {
		todo = todo[:30]
	}

	// Extract tags (no lock needed — ExpandQuery is pure computation).
	type enrichment struct {
		id      string
		newTags []string
	}
	var enrichments []enrichment
	for _, p := range todo {
		expanded := ExpandQuery(p.content)
		if len(expanded.Entities) > 0 {
			enrichments = append(enrichments, enrichment{id: p.id, newTags: expanded.Entities})
		}
	}

	if len(enrichments) == 0 {
		return 0
	}

	// Build lookup for fast ID matching.
	enrichByID := make(map[string][]string, len(enrichments))
	for _, e := range enrichments {
		enrichByID[e.id] = e.newTags
	}

	// Apply enrichments under write lock, matching by ID.
	s.mu.Lock()
	count := 0
	for i := range s.entries {
		if newTags, ok := enrichByID[s.entries[i].ID]; ok {
			s.entries[i].Tags = mergeTags(s.entries[i].Tags, newTags)
			s.bm25.updateEntry(s.entries[i])
			count++
		}
	}
	if count > 0 {
		s.dirty = true
	}
	s.mu.Unlock()

	if count > 0 {
		s.signalSave()
	}
	return count
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
// bidirectional graph edges. It uses BM25 scores, cosine similarity (when
// available), and tag overlap to rank candidates. Only candidates above
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

	// Build a set of the new entry's tags (lowered) for overlap checking.
	entryTagSet := make(map[string]bool, len(entry.Tags))
	for _, tag := range entry.Tags {
		tl := strings.ToLower(tag)
		if len([]rune(tl)) >= 2 {
			entryTagSet[tl] = true
		}
	}

	// Build entryByID map and pre-compute tag overlap counts in one pass.
	tagOverlapByID := make(map[string]int)
	entryByID := make(map[string]*Entry, len(s.entries))
	for i := range s.entries {
		e := &s.entries[i]
		entryByID[e.ID] = e
		if e.ID == entry.ID || !e.IsActive() {
			continue
		}
		if len(entryTagSet) > 0 {
			overlap := 0
			for _, tag := range e.Tags {
				if entryTagSet[strings.ToLower(tag)] {
					overlap++
				}
			}
			if overlap > 0 {
				tagOverlapByID[e.ID] = overlap
			}
		}
	}

	// Collect all candidate IDs from BM25, vector, and tag overlap sources.
	seen := make(map[string]bool)
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
	for id := range tagOverlapByID {
		seen[id] = true
	}

	// Score and filter candidates.
	type candidate struct {
		id    string
		score float64
	}
	var candidates []candidate

	for id := range seen {
		bm25 := bm25Scores[id]
		cosine := 0.0
		if vecScores != nil {
			cosine = vecScores[id]
		}
		tagOverlap := tagOverlapByID[id]
		tagBonus := float64(tagOverlap) * 0.15

		var fused float64
		if vecScores != nil {
			fused = 0.4*bm25 + 0.6*cosine + tagBonus
		} else {
			fused = bm25 + tagBonus
		}

		// Threshold: require cosine > threshold OR ≥2 shared tags.
		if vecScores != nil {
			if cosine < autoLinkThreshold && tagOverlap < 2 {
				continue
			}
		} else {
			if bm25 <= 0 && tagOverlap < 2 {
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

	// Create graph links. Use cosine as edge strength when it's strong,
	// otherwise use the fused score (preserves tag-only link strength).
	for _, c := range candidates {
		strength := c.score
		if vecScores != nil {
			if cs, ok := vecScores[c.id]; ok && cs >= autoLinkThreshold {
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
		if e, ok := entryByID[c.id]; ok {
			e.RelatedIDs = neighborRels
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

// CapacityInfo returns the current active entry count and the maximum
// allowed entries. Used by prompt builders to display capacity to the LLM.
func (s *Store) CapacityInfo() (active int, maxItems int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries), s.maxItems
}

// HealthReport computes an aggregated health snapshot of the memory system.
// Inspired by GBrain's `gbrain health` / `gbrain doctor` commands.
// Safe to call from any goroutine.
func (s *Store) HealthReport() *HealthReport {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r := &HealthReport{
		ActiveEntries:  len(s.entries),
		MaxCapacity:    s.maxItems,
		EmbedderActive: s.embedder != nil && !embedding.IsNoop(s.embedder),
		CategoryCounts: make(map[string]int),
	}

	if r.MaxCapacity > 0 {
		r.CapacityPercent = float64(r.ActiveEntries) / float64(r.MaxCapacity) * 100
	}

	if s.archive != nil {
		r.ArchivedEntries = s.archive.Count()
	}

	var totalAccess int
	var oldest, newest time.Time

	for _, e := range s.entries {
		r.CategoryCounts[string(e.Category)]++
		totalAccess += e.AccessCount

		if e.Stale {
			r.StaleEntries++
		}
		if e.Pinned {
			r.PinnedEntries++
		}
		if len(e.Embedding) == 0 {
			r.NoEmbedding++
		}
		if e.ContentHash == "" {
			r.NoHash++
		}
		if len(e.RelatedIDs) == 0 {
			r.OrphanEntries++
		}
		if len(e.Versions) > 0 {
			r.VersionedEntries++
		}

		if oldest.IsZero() || e.CreatedAt.Before(oldest) {
			oldest = e.CreatedAt
		}
		if e.UpdatedAt.After(newest) {
			newest = e.UpdatedAt
		}
	}

	if r.ActiveEntries > 0 {
		r.AvgAccessCount = float64(totalAccess) / float64(r.ActiveEntries)
	}
	if !oldest.IsZero() {
		r.OldestEntry = oldest.Format(time.RFC3339)
	}
	if !newest.IsZero() {
		r.NewestEntry = newest.Format(time.RFC3339)
	}

	return r
}

// HealthSummary returns a one-line human-readable health summary for
// system prompt injection. Example: "Memory: 142/500 (28%), 3 stale, 12 no-embed"
func (s *Store) HealthSummary() string {
	r := s.HealthReport()
	parts := []string{
		fmt.Sprintf("Memory: %d/%d (%.0f%%)", r.ActiveEntries, r.MaxCapacity, r.CapacityPercent),
	}
	if r.StaleEntries > 0 {
		parts = append(parts, fmt.Sprintf("%d stale", r.StaleEntries))
	}
	if r.NoEmbedding > 0 {
		parts = append(parts, fmt.Sprintf("%d no-embed", r.NoEmbedding))
	}
	if r.OrphanEntries > 0 {
		parts = append(parts, fmt.Sprintf("%d orphan", r.OrphanEntries))
	}
	if r.ArchivedEntries > 0 {
		parts = append(parts, fmt.Sprintf("%d archived", r.ArchivedEntries))
	}
	return strings.Join(parts, ", ")
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
	if err := fileutil.AtomicWriteFile(s.path, data, 0o644); err != nil {
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

// ---------------------------------------------------------------------------
// LLM-based relevance filtering (inspired by Claude Code findRelevantMemories)
// ---------------------------------------------------------------------------

// LLMRelevanceFilter selects the most relevant memory entries from a candidate
// set using an LLM sideQuery. This sits on top of BM25+Vector recall and
// provides semantic precision that keyword/embedding fusion alone cannot achieve.
type LLMRelevanceFilter interface {
	// SelectRelevant receives a user query and a list of candidate memory
	// summaries (id + one-line description), and returns the IDs of the
	// most relevant entries (up to maxResults).
	SelectRelevant(query string, candidates []MemoryCandidate, maxResults int) ([]string, error)
	// IsAvailable reports whether the LLM backend is ready for reranking.
	IsAvailable() bool
}

// MemoryCandidate is a lightweight summary of a memory entry for LLM selection.
type MemoryCandidate struct {
	ID          string   `json:"id"`
	Category    Category `json:"category"`
	Description string   `json:"description"` // first 150 chars of content or CompactForm
	Tags        []string `json:"tags"`
}

// RecallWithLLMFilter performs a two-stage recall:
//  1. BM25+Vector fusion via RecallForProject (broad candidate set, up to 20)
//  2. LLM sideQuery to select the top-N most relevant entries (default 5)
//
// If llmFilter is nil or returns an error, falls back to stage-1 results.
// This mirrors Claude Code's findRelevantMemories pattern where Sonnet
// selects from a manifest of memory file headers.
func (s *Store) RecallWithLLMFilter(userMessage, projectPath string, llmFilter LLMRelevanceFilter, maxResults int) []Entry {
	if maxResults <= 0 {
		maxResults = 5
	}

	// Stage 1: broad recall via existing BM25+Vector+Graph pipeline.
	candidates := s.RecallForProject(userMessage, projectPath)
	if len(candidates) == 0 {
		return candidates
	}

	// If no LLM filter or too few candidates, return as-is.
	if llmFilter == nil || len(candidates) <= maxResults {
		if len(candidates) > maxResults {
			return candidates[:maxResults]
		}
		return candidates
	}

	// Build lightweight candidate summaries for the LLM.
	summaries := make([]MemoryCandidate, len(candidates))
	for i, e := range candidates {
		desc := e.CompactForm
		if desc == "" {
			desc = e.Content
		}
		if len([]rune(desc)) > 150 {
			desc = string([]rune(desc)[:150])
		}
		summaries[i] = MemoryCandidate{
			ID:          e.ID,
			Category:    e.Category,
			Description: desc,
			Tags:        e.Tags,
		}
	}

	// Stage 2: LLM selects the most relevant.
	selectedIDs, err := llmFilter.SelectRelevant(userMessage, summaries, maxResults)
	if err != nil || len(selectedIDs) == 0 {
		// Fallback: return top-N from stage 1.
		if len(candidates) > maxResults {
			return candidates[:maxResults]
		}
		return candidates
	}

	// Build result preserving LLM selection order.
	idToEntry := make(map[string]Entry, len(candidates))
	for _, e := range candidates {
		idToEntry[e.ID] = e
	}
	var result []Entry
	for _, id := range selectedIDs {
		if e, ok := idToEntry[id]; ok {
			result = append(result, e)
		}
	}

	// Touch access counts for selected entries.
	if len(result) > 0 {
		ids := make([]string, len(result))
		for i, e := range result {
			ids[i] = e.ID
		}
		go s.TouchAccess(ids)
	}

	return result
}

// RecallSmart is an enhanced recall entry point that integrates Query Expansion,
// Tag Fast Lane, dynamic budget, and optional LLM reranking.
// When llmFilter is nil or unavailable, it is equivalent to RecallForProject.
func (s *Store) RecallSmart(userMessage, projectPath string, llmFilter LLMRelevanceFilter) []Entry {
	candidates := s.RecallForProject(userMessage, projectPath)
	// Note: RecallForProject already called TouchAccess on all candidates.

	if llmFilter == nil || !llmFilter.IsAvailable() || len(candidates) <= 5 {
		return candidates
	}

	// Build lightweight summaries for LLM selection.
	summaries := make([]MemoryCandidate, len(candidates))
	for i, e := range candidates {
		desc := e.CompactForm
		if desc == "" {
			desc = e.Content
		}
		if len([]rune(desc)) > 150 {
			desc = string([]rune(desc)[:150])
		}
		summaries[i] = MemoryCandidate{
			ID:          e.ID,
			Category:    e.Category,
			Description: desc,
			Tags:        e.Tags,
		}
	}

	selectedIDs, err := llmFilter.SelectRelevant(userMessage, summaries, 10)
	if err != nil || len(selectedIDs) == 0 {
		return candidates // graceful degradation
	}

	idToEntry := make(map[string]Entry, len(candidates))
	for _, e := range candidates {
		idToEntry[e.ID] = e
	}
	var result []Entry
	for _, id := range selectedIDs {
		if e, ok := idToEntry[id]; ok {
			result = append(result, e)
		}
	}

	return result
}

// SetRecallGating wires an optional TiMem recall-gating filter into the store.
func (s *Store) SetRecallGating(rg *RecallGating) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gating = rg
}

// TMT returns the store's temporal memory tree instance.
func (s *Store) TMT() *TemporalTree {
	return s.tmt
}
