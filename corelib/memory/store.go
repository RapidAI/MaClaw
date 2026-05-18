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
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// Store provides persistent long-term memory storage.
type Store struct {
	mu               sync.RWMutex
	entries          []Entry
	path             string
	dirty            bool
	dirtyGen         uint64
	saveCh           chan struct{}
	stopCh           chan struct{}
	stopOnce         sync.Once
	maxItems         int
	bm25             *bm25Index
	vecIndex         *vectorIndex
	graph            *memoryGraph
	embedder         embedding.Embedder // nil until SetEmbedder is called
	embedderGen      uint64             // increments whenever SetEmbedder changes the embedder
	archive          *ArchiveStore      // cold storage for evicted entries
	tmt              *TemporalTree
	gating           *RecallGating
	partMgr          *partitionManager            // category-based partitioned persistence
	lastSemanticHits map[string]SemanticSearchHit // debug: last semantic recall explanation by entry ID
	lastDerivedFacts []DerivedFact                // debug: last inference engine results
	lastRecallTrace  RecallTrace                  // debug: last RecallDynamic retrieval signals

	// --- Project index ---
	projIndex     *ProjectIndex  // aggregated project metadata for search
	semanticGraph *SemanticGraph // typed Entity/Fact/Memory graph for relation-aware recall

	// --- Entity index (inspired by Graphiti Semantic Entity Subgraph) ---
	entityIndex *EntityIndex // entity name -> entry ID mapping for entity-centric queries

	themeManager *ThemeManager // embedding-aware xMemory-style theme layer

	// --- Online extractor (Mem0-style incremental extraction) ---
	onlineExtractor *OnlineExtractor // real-time per-turn extraction pipeline

	// --- Multi-hop inference engine ---
	inferenceEngine *InferenceEngine // rule-based multi-hop fact reasoning

	// --- Async semantic dedup ---
	// pendingDedup holds (newEntryID, candidateEntryID) pairs that need
	// LLM-based precise dedup judgment. Written by SaveWithContext (under
	// s.mu.Lock), consumed by ProcessPendingDedup (acquires its own lock).
	pendingDedup []pendingDedupPair
	llmDedup     LLMChatCaller // set via SetLLMDedup; nil = async dedup disabled

	// --- Storage backend + cross-instance sync ---
	backend StorageBackend // persistence layer (JSON or SQLite)
	sync    *syncState     // nil if sync is disabled

	queryEmbMu     sync.Mutex
	queryEmbCache  map[string]queryEmbeddingCacheEntry
	queryEmbFlight map[string]*queryEmbeddingFlight
}

type queryEmbeddingCacheEntry struct {
	vec        []float32
	generation uint64
	createdAt  time.Time
	lastUsed   time.Time
}

type queryEmbeddingFlight struct {
	generation uint64
	done       chan struct{}
	vec        []float32
	err        error
}

const maxQueryEmbeddingCacheEntries = 256
const queryEmbeddingCacheTTL = 10 * time.Minute

// NewStore creates a Store that persists to the given path.
func NewStore(path string) (*Store, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("memory_store: resolve path: %w", err)
	}

	s := &Store{
		entries:        make([]Entry, 0),
		path:           absPath,
		saveCh:         make(chan struct{}, 1),
		stopCh:         make(chan struct{}),
		maxItems:       2000,
		bm25:           newBM25Index(),
		vecIndex:       newVectorIndex(),
		graph:          newMemoryGraph(),
		tmt:            NewTemporalTree(),
		partMgr:        newPartitionManager(filepath.Dir(absPath)),
		projIndex:      NewProjectIndex(filepath.Dir(absPath)),
		semanticGraph:  NewSemanticGraph(),
		entityIndex:    NewEntityIndex(),
		themeManager:   NewThemeManager(),
		queryEmbCache:  make(map[string]queryEmbeddingCacheEntry),
		queryEmbFlight: make(map[string]*queryEmbeddingFlight),
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	// Build indices from loaded entries.
	s.rebuildDerivedIndexesLocked(false)

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
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), buf)
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
	return s.SaveWithContext(entry, "")
}

// SaveForUser stores a memory entry owned by the specified user.
// In single-user mode (GUI/TUI), pass empty string for ownerID.
// In multi-tenant mode (maclawsrv), pass the IM user ID.
func (s *Store) SaveForUser(entry Entry, ownerID string) error {
	entry.OwnerID = ownerID
	return s.Save(entry)
}

// SaveWithContext stores a memory entry with additional context for tag enrichment.
// contextHint is surrounding conversation text that provides aliases and related
// terms not present in the entry content itself. When non-empty, entities are
// extracted from contextHint and merged into the entry's tags, improving recall
// when the user later queries with those alias terms.
func (s *Store) SaveWithContext(entry Entry, contextHint string) error {
	if err := ScanForInjection(entry.Content); err != nil {
		return fmt.Errorf("memory_store: rejected: %w", err)
	}

	// Redact sensitive information (API keys, passwords, tokens, private keys)
	// before persisting to long-term memory. This prevents secrets from being
	// recalled and injected into future LLM prompts.
	// Inspired by Codex CLI's redact_secrets() applied at every memory write path.
	entry.Content = redactSecretsInMemory(entry.Content)

	// Enrich tags from conversation context: extract entities from contextHint
	// that are not already present in the entry's content-derived tags.
	if contextHint != "" {
		ctxExpanded := ExpandQuery(contextHint)
		if len(ctxExpanded.Entities) > 0 {
			entry.Tags = mergeTags(entry.Tags, ctxExpanded.Entities)
		}
	}

	hash := computeContentHash(entry.Content)

	// --- Embedding computation (outside lock) ---
	// Compute embedding before acquiring the write lock. The embedder takes
	// 2-10ms for model inference; doing this under s.mu.Lock would block all
	// concurrent reads (RecallDynamic, List, Search).
	//
	// Read s.embedder under RLock to avoid a data race with SetEmbedder.
	// Then use the local copy for the actual Embed() call (no lock held).
	if len(entry.Embedding) == 0 {
		s.mu.RLock()
		emb := s.embedder
		s.mu.RUnlock()
		if emb != nil {
			vec, err := emb.Embed(entry.Content)
			if err == nil && len(vec) > 0 {
				entry.Embedding = vec
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Idempotent: check by content hash first (O(n) but fast string compare).
	// Multi-tenant isolation: only dedup within the same owner (or shared entries).
	for i := range s.entries {
		if s.entries[i].ContentHash == hash || s.entries[i].Content == entry.Content {
			// Multi-tenant isolation: skip entries from different users.
			// Empty OwnerID (shared) can match with any user.
			existingOwner := s.entries[i].OwnerID
			if entry.OwnerID != "" && existingOwner != "" && existingOwner != entry.OwnerID {
				continue
			}
			s.entries[i].UpdatedAt = now
			s.entries[i].AccessCount++
			s.entries[i].Tags = mergeTags(s.entries[i].Tags, entry.Tags)
			s.entries[i].Entities = mergeStringSlice(s.entries[i].Entities, entry.Entities)
			if s.entries[i].ContentHash == "" {
				s.entries[i].ContentHash = hash
			}
			s.bm25.updateEntry(s.entries[i])
			if s.entityIndex != nil {
				s.entityIndex.IndexEntry(&s.entries[i])
			}
			// Tags may change project membership; rebuild because ProjectIndex is an aggregate.
			if s.projIndex != nil {
				s.projIndex.Rebuild(s.entries)
			}
			if s.semanticGraph != nil {
				s.semanticGraph.IndexEntry(&s.entries[i])
			}
			s.rebuildThemeLayerLocked()
			if err := s.persistUpdatedEntryLocked(&s.entries[i]); err != nil {
				return fmt.Errorf("memory_store: persist updated entry: %w", err)
			}
			return nil
		}
	}

	// Substring dedup: check if the new content is a substring of (or contains)
	// a recent existing entry. This catches semantically duplicate entries that
	// differ in wording (e.g. KnowledgeExtractor extracts similar knowledge
	// points across sessions). Only scan the most recent 50 entries to bound
	// write latency. When a match is found, merge tags into the existing entry
	// instead of creating a duplicate.
	// Multi-tenant isolation: only dedup within the same owner (or shared entries).
	if substringDupIdx := s.findSubstringDuplicateForEntry(entry); substringDupIdx >= 0 {
		s.entries[substringDupIdx].UpdatedAt = now
		s.entries[substringDupIdx].AccessCount++
		s.entries[substringDupIdx].Tags = mergeTags(s.entries[substringDupIdx].Tags, entry.Tags)
		s.entries[substringDupIdx].Entities = mergeStringSlice(s.entries[substringDupIdx].Entities, entry.Entities)
		// If the new content is a superset (contains the existing content),
		// update to the longer version to preserve more information.
		existingLen := len([]rune(s.entries[substringDupIdx].Content))
		newLen := len([]rune(entry.Content))
		if newLen > existingLen {
			s.entries[substringDupIdx].Content = entry.Content
			s.entries[substringDupIdx].CompactForm = ""
			s.entries[substringDupIdx].ContentHash = hash
			if len(entry.Embedding) > 0 {
				s.entries[substringDupIdx].Embedding = append([]float32(nil), entry.Embedding...)
			}
		}
		s.bm25.updateEntry(s.entries[substringDupIdx])
		if len(s.entries[substringDupIdx].Embedding) > 0 {
			s.vecIndex.add(s.entries[substringDupIdx].ID, s.entries[substringDupIdx].Embedding)
		}
		if s.entityIndex != nil {
			s.entityIndex.IndexEntry(&s.entries[substringDupIdx])
		}
		// Tags/content may change project membership; rebuild the aggregate index.
		if s.projIndex != nil {
			s.projIndex.Rebuild(s.entries)
		}
		if s.semanticGraph != nil {
			s.semanticGraph.IndexEntry(&s.entries[substringDupIdx])
		}
		s.rebuildThemeLayerLocked()
		if err := s.persistUpdatedEntryLocked(&s.entries[substringDupIdx]); err != nil {
			return fmt.Errorf("memory_store: persist merged duplicate: %w", err)
		}
		log.Printf("[memory_store] merged substring duplicate into entry %s (kept longer: %v)", s.entries[substringDupIdx].ID, newLen > existingLen)
		return nil
	}

	// Assign ID early so it's available for pending dedup tracking.
	if entry.ID == "" {
		entry.ID = generateID()
	}

	// --- Embedding semantic candidate recall (under lock, <1ms) ---
	// The embedding was computed above (outside the lock). Here we only
	// query the vector index for candidates; this is a fast dot-product
	// scan over the in-memory index, not a model inference call.
	if len(entry.Embedding) > 0 {
		if candidate := s.findSemanticDupCandidate(entry.Embedding, entry.Category, entry.OwnerID); candidate != nil {
			s.enqueuePendingDedup(entry.ID, *candidate)
		}
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

	if err := s.persistInsertedEntryLocked(&entry); err != nil {
		return fmt.Errorf("memory_store: persist new entry: %w", err)
	}

	s.entries = append(s.entries, entry)
	s.bm25.addEntry(entry)
	s.vecIndex.add(entry.ID, entry.Embedding)

	// Auto-link: find related entries and create graph edges.
	s.autoLink(entry)

	s.evictLRU()
	s.markDirtyLocked()

	// Update project index. Called under s.mu.Lock; ProjectIndex has its own
	// mutex so this is a nested lock (Store.mu -> ProjectIndex.mu). The lock
	// order is consistent across all call sites, no deadlock risk.
	if s.projIndex != nil {
		s.projIndex.IndexEntry(&entry)
	}

	// Update entity index for entity-centric recall.
	if s.entityIndex != nil {
		s.entityIndex.IndexEntry(&entry)
	}
	if s.semanticGraph != nil {
		s.semanticGraph.IndexEntry(&entry)
	}
	s.rebuildThemeLayerLocked()

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
			if s.entityIndex != nil {
				s.entityIndex.IndexEntry(&s.entries[i])
			}
			if s.projIndex != nil {
				s.projIndex.Rebuild(s.entries)
			}
			if s.semanticGraph != nil {
				s.semanticGraph.IndexEntry(&s.entries[i])
			}
			s.rebuildThemeLayerLocked()
			if err := s.persistUpdatedEntryLocked(&s.entries[i]); err != nil {
				return fmt.Errorf("memory_store: persist updated entry: %w", err)
			}
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
			if err := s.persistDeletedEntryLocked(id); err != nil {
				return fmt.Errorf("memory_store: delete backend entry: %w", err)
			}
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			s.bm25.removeEntry(id)
			s.vecIndex.remove(id)
			s.graph.remove(id)
			s.syncGraphLinksLocked()
			if s.entityIndex != nil {
				s.entityIndex.RemoveEntry(id)
			}
			if s.semanticGraph != nil {
				s.semanticGraph.RemoveEntry(id)
			}
			s.rebuildThemeLayerLocked()
			return nil
		}
	}
	return fmt.Errorf("memory_store: entry %q not found", id)
}

// ProjectIndex returns the project index for search and listing.
// Returns nil if the store has not been initialized.
func (s *Store) ProjectIndex() *ProjectIndex {
	return s.projIndex
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

// CategoryStat holds the count and representative tags for a memory category.
type CategoryStat struct {
	Category Category
	Count    int
	Tags     []string // up to 3 representative tags
}

// CategoryStats returns a summary of all entries grouped by canonical category.
// This is a store-level index; it reflects the full memory contents, not just
// what was recalled for a specific query. Used by the memory index layer to
// give the LLM a "table of contents" of available knowledge.
func (s *Store) CategoryStats() []CategoryStat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.categoryStatsLocked()
}

// CategoryStatsForProject returns category stats filtered to entries relevant
// to the given project path. Uses the same strict filtering as RecallDynamicStrict:
// ScopeProject entries must have tags matching projectPath; non-ScopeProject entries
// (global knowledge, user_fact, preference) are always included.
func (s *Store) CategoryStatsForProject(projectPath string) []CategoryStat {
	s.mu.RLock()
	defer s.mu.RUnlock()

	projectLower := semanticNormalizeProjectPath(projectPath)
	if projectLower == "" {
		return s.categoryStatsLocked()
	}

	type info struct {
		count int
		tags  []string
	}
	catMap := make(map[Category]*info)
	var order []Category

	for _, e := range s.entries {
		canonical := MapToCanonical(e.Category)
		if canonical == CategorySelfIdentity || canonical == CategorySessionCheckpoint || canonical == CategoryConversationSummary {
			continue
		}
		// Strict project filter: exclude other projects' entries.
		if !recallStrictProjectEntryAllowed(e, projectLower) {
			continue
		}
		ci, exists := catMap[canonical]
		if !exists {
			ci = &info{}
			catMap[canonical] = ci
			order = append(order, canonical)
		}
		ci.count++
		if len(ci.tags) < 3 {
			for _, t := range e.Tags {
				if len(t) > 1 && len(t) < 20 && !semanticLooksLikePath(semanticNormalizeProjectPath(t)) {
					dup := false
					for _, existing := range ci.tags {
						if existing == t {
							dup = true
							break
						}
					}
					if !dup {
						ci.tags = append(ci.tags, t)
						if len(ci.tags) >= 3 {
							break
						}
					}
				}
			}
		}
	}

	result := make([]CategoryStat, 0, len(order))
	for _, cat := range order {
		ci := catMap[cat]
		result = append(result, CategoryStat{
			Category: cat,
			Count:    ci.count,
			Tags:     ci.tags,
		})
	}
	return result
}

func (s *Store) categoryStatsLocked() []CategoryStat {
	type info struct {
		count int
		tags  []string
	}
	catMap := make(map[Category]*info)
	var order []Category
	for _, e := range s.entries {
		canonical := MapToCanonical(e.Category)
		if canonical == CategorySelfIdentity || canonical == CategorySessionCheckpoint || canonical == CategoryConversationSummary {
			continue
		}
		ci, exists := catMap[canonical]
		if !exists {
			ci = &info{}
			catMap[canonical] = ci
			order = append(order, canonical)
		}
		ci.count++
		if len(ci.tags) < 3 {
			for _, t := range e.Tags {
				if len(t) > 1 && len(t) < 20 {
					dup := false
					for _, existing := range ci.tags {
						if existing == t {
							dup = true
							break
						}
					}
					if !dup {
						ci.tags = append(ci.tags, t)
						if len(ci.tags) >= 3 {
							break
						}
					}
				}
			}
		}
	}
	result := make([]CategoryStat, 0, len(order))
	for _, cat := range order {
		ci := catMap[cat]
		result = append(result, CategoryStat{Category: cat, Count: ci.count, Tags: ci.tags})
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
	result := s.recallForProjectCandidates(userMessage, projectPath)
	s.touchRecallResultsAsync(result)
	return result
}

func (s *Store) recallForProjectCandidates(userMessage, projectPath string) []Entry {
	// === Phase 1: Query Understanding ===
	expanded := ExpandQuery(userMessage)

	// === Phase 2: Multi-Query BM25 ===
	bm25Scores := s.multiQueryBM25(userMessage, expanded.Entities)

	// Compute vector scores if available (use original message; embeddings understand semantics).
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(userMessage))

	// Hold RLock for the scoring/assembly phase, then release before TouchAccess.
	return s.recallForProjectLocked(userMessage, bm25Scores, vecScores, expanded.QueryTokens, projectPath)
}

func (s *Store) touchRecallResults(result []Entry) {
	ids := recallResultIDs(result)
	if len(ids) == 0 {
		return
	}
	s.TouchAccess(ids)
}

func (s *Store) touchRecallResultsAsync(result []Entry) {
	ids := recallResultIDs(result)
	if len(ids) == 0 {
		return
	}
	go s.TouchAccess(ids)
}

func recallResultIDs(result []Entry) []string {
	if len(result) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(result))
	ids := make([]string, 0, len(result))
	for _, e := range result {
		if e.ID == "" {
			continue
		}
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		ids = append(ids, e.ID)
	}
	return ids
}

// recallForProjectLocked performs the scoring and assembly phase of RecallForProject.
// Caller must NOT hold any lock; this method acquires RLock internally.
func (s *Store) recallForProjectLocked(query string, bm25Scores map[string]float64, vecScores map[string]float64, queryTokens []string, projectPath string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// === Phase 3: Dynamic Budget ===
	activeCount := s.activeCountLocked()
	maxTokens, maxEntries := dynamicBudget(activeCount)

	projectLower := semanticNormalizeProjectPath(projectPath)
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
		if !recallProjectEntryAllowed(e, projectLower) {
			continue
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
		sc := memoryStreamScore(c.entry, fusedRelevance, c.bm25, projectLower, now)
		// OpenHuman-inspired: stability boost/penalty
		sc += c.entry.Stability.StabilityBoost()
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
	others = filterRecallProjectOthers(others, projectLower)
	if ClassifyComplexity(query, queryTokens, nil) != ComplexitySimple && s.themeManager != nil {
		others = themeAwareDiversityRerank(others, s.themeManager.Themes(), graphExpandSeeds)
	}

	// === Phase 5: Type-quota assembly ===
	var result []Entry
	tokenBudget := maxTokens
	userFactBudgetCap := int(float64(maxTokens) * 0.6) // user_fact gets at most 60%

	// Self-identity memories are always recalled first; highest priority.
	for _, e := range selfIdentity {
		tokens := EstimateTextTokens(e.Content)
		tokenBudget -= tokens
		result = append(result, e)
	}

	// user_fact: capped at 60% of total budget.
	userFactUsed := 0
	for _, e := range userFacts {
		if len(result) >= maxEntries {
			break
		}
		tokens := EstimateTextTokens(e.Content)
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
		tokens := EstimateTextTokens(sc.entry.Content)
		if tokens > tokenBudget {
			continue
		}
		tokenBudget -= tokens
		result = append(result, sc.entry)
	}

	return result
}

func recallProjectEntryAllowed(e Entry, projectLower string) bool {
	return semanticProjectAllowed(e.Scope, e.Tags, projectLower)
}

func recallProjectOtherAllowed(e Entry, projectLower string) bool {
	if !e.IsActive() || !recallProjectEntryAllowed(e, projectLower) {
		return false
	}
	canonical := MapToCanonical(e.Category)
	return e.Category != CategorySelfIdentity && e.Category != CategoryUserFact && canonical != CategoryUserFact
}

func filterRecallProjectOthers(candidates []recallScored, projectLower string) []recallScored {
	if len(candidates) == 0 {
		return candidates
	}
	filtered := candidates[:0]
	for _, c := range candidates {
		if recallProjectOtherAllowed(c.entry, projectLower) {
			filtered = append(filtered, c)
		}
	}
	return filtered
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
		// Access touches can happen after every recall, so keep them batched even
		// in SQLite mode. The backend-aware flush path writes the updated entries
		// after the normal debounce or on Stop().
		s.markDirtyLocked()
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
// Stale entries are prefixed with [possibly stale] to alert the LLM.
func DisplayContent(e Entry) string {
	text := e.CompactForm
	if text == "" {
		text = e.Content
	}
	if e.Stale {
		text = "[\u53ef\u80fd\u8fc7\u65f6] " + text
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
		summary = string(runes[:maxRunes]) + "..."
	}
	return summary
}

// ---------------------------------------------------------------------------
// RRF (Reciprocal Rank Fusion) - inspired by GBrain's hybrid search
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

	bm25Rank := positiveSignalRanks(bm25Scores)
	vecRank := positiveSignalRanks(vecScores)

	// Tag cross-matching becomes a rank-fusion channel only when an entry has
	// a positive tag score. Zero-score channels must not create synthetic
	// relevance for every active memory entry.
	var tagRank []int
	hasTagSignal := len(queryTokens) > 0
	if hasTagSignal {
		tagScores := make([]float64, n)
		for i := range entries {
			tagScores[i] = tagCrossScore(entries[i], queryTokens)
		}
		tagRank = positiveSignalRanks(tagScores)
	}

	// Compute RRF scores.
	const tagAlpha = 0.8 // tag signal weight (slightly lower than BM25/Vec)
	scores := make([]float64, n)
	for i := range scores {
		rrf := 0.0
		if bm25Rank[i] > 0 {
			rrf += 1.0 / float64(rrfK+bm25Rank[i])
		}
		if vecRank[i] > 0 {
			rrf += 1.0 / float64(rrfK+vecRank[i])
		}
		if hasTagSignal && tagRank[i] > 0 {
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

func positiveSignalRanks(scores []float64) []int {
	ranks := make([]int, len(scores))
	order := make([]int, 0, len(scores))
	for i, score := range scores {
		if score > 0 {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return scores[order[a]] > scores[order[b]]
	})
	for rank, idx := range order {
		ranks[idx] = rank + 1
	}
	return ranks
}

// tagCrossScore computes the cross-match score between user message tokens
// and a memory entry's tags.
//   - Exact match (case-insensitive): +2.0
//   - Containment match (tag contains token or vice versa, min 3 runes): +1.0
//   - Cap: 6.0
//
// tagExactMatchBoost returns a score boost when any query entity exactly
// matches (case-insensitive) one of the entry's tags. This is a stronger
// signal than tagCrossScore's containment matching; it means the user is
// querying with the exact same term that was stored as a tag (possibly from
// conversation context via SaveWithContext).
func tagExactMatchBoost(entry Entry, entities []string) float64 {
	if len(entry.Tags) == 0 || len(entities) == 0 {
		return 0
	}
	boost := 0.0
	for _, entity := range entities {
		entityLower := strings.ToLower(entity)
		if len([]rune(entityLower)) < 2 {
			continue
		}
		for _, tag := range entry.Tags {
			if strings.ToLower(tag) == entityLower {
				boost += 5.0 // strong boost for exact tag match
				break
			}
		}
	}
	// Cap to prevent a single entry from dominating.
	if boost > 10.0 {
		boost = 10.0
	}
	return boost
}

// tagCrossScore computes the cross-match score between user message tokens
// and a memory entry's tags.
//   - Exact match (case-insensitive): +2.0
//   - Containment match (tag contains token or vice versa, min 3 runes): +1.0
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
			// Containment: only if both sides are at least 3 runes to avoid
			// spurious matches from very short tags or tokens.
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
//   Score = w1*Recency + w2*Importance + w3*Relevance
//
// Recency:    exponential decay based on hours since last update.
// Importance: category weight + log(1 + min(accessCount, 20)).
// Relevance:  RRF-fused BM25+Vec+Tag score + project affinity boost.
//
// Weight tuning rationale (2026-04-21):
//   Relevance (1.5) > Recency (1.0) > Importance (0.8)
//   Relevance is the primary signal; a query-specific BM25 match should
//   outweigh a frequently-accessed but irrelevant entry. Importance is
//   capped and down-weighted to prevent high-accessCount entries from
//   dominating (e.g. GPU server at 104 accesses vs API server at 1).
// ---------------------------------------------------------------------------

const (
	msDecay       = 0.005 // recency decay rate per hour
	msWRecency    = 1.0
	msWImportance = 0.8
	msWRelevance  = 1.5
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
	case CategoryTaskArtifact:
		return 3.0
	case CategorySessionCheckpoint:
		return 1.5
	case CategoryConversationSummary:
		return 1.0
	default:
		return 1.0
	}
}

// memoryStreamScore computes the three-dimensional score for a memory entry.
func memoryStreamScore(e Entry, rrfScore float64, rawBM25 float64, projectLower string, now time.Time) float64 {
	// --- Recency ---
	hours := now.Sub(e.UpdatedAt).Hours()
	if hours < 0 {
		hours = 0
	}
	recency := math.Exp(-msDecay * hours)

	// --- Importance ---
	// Cap the accessCount contribution to prevent high-frequency entries from
	// dominating over more relevant but less-accessed entries.
	cappedAccess := float64(e.AccessCount)
	if cappedAccess > 20 {
		cappedAccess = 20
	}
	importance := CategoryImportanceWeight(e.Category) + math.Log1p(cappedAccess)

	// --- Relevance ---
	// RRF scores are rank-based (~0.01-0.15) and flatten score differences.
	// Raw BM25 scores preserve magnitude differences for exact term matches.
	// Combine both signals:
	// - RRF provides robust multi-signal fusion (BM25 + Vec + Tag)
	// - Raw BM25 preserves the actual term-match strength
	relevance := rrfScore*50.0 + rawBM25
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

	// 1-hop BFS expansion. expand() returns neighbor -> decayed edge weight.
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

	// Find the actual seed that links to each expanded neighbor and use
	// that seed's score as the base for the derived score. This prevents
	// low-relevance seeds from inheriting high scores from unrelated seeds.
	//
	// Pre-compute seed neighbor sets to avoid repeated graph lookups.
	seedNeighbors := make(map[string]map[string]float64, len(seedIDs))
	for _, sid := range seedIDs {
		seedNeighbors[sid] = s.graph.neighborsOf(sid)
	}

	for neighborID, expandWeight := range expanded {
		if existing[neighborID] {
			continue
		}
		e, ok := entryByID[neighborID]
		if !ok || !e.IsActive() {
			continue
		}

		// Derive score: find the best score among seeds that actually link
		// to this neighbor (not the global best seed).
		bestLinkedSeedScore := 0.0
		for _, sid := range seedIDs {
			if _, linked := seedNeighbors[sid][neighborID]; linked {
				if sc := seedScores[sid]; sc > bestLinkedSeedScore {
					bestLinkedSeedScore = sc
				}
			}
		}
		// Fallback: if no direct link found (shouldn't happen), use minimum seed score.
		if bestLinkedSeedScore == 0 {
			for _, sid := range seedIDs {
				if sc, ok := seedScores[sid]; ok {
					if bestLinkedSeedScore == 0 || sc < bestLinkedSeedScore {
						bestLinkedSeedScore = sc
					}
				}
			}
		}
		derivedScore := bestLinkedSeedScore * expandWeight

		candidates = append(candidates, recallScored{entry: *e, score: derivedScore})
		existing[neighborID] = true
	}

	// Re-sort after merging expanded entries.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return candidates
}

// syncGraphLinksLocked mirrors the in-memory graph onto each entry's persisted
// relationship fields. Caller MUST hold s.mu write lock.
func (s *Store) syncGraphLinksLocked(ids ...string) bool {
	if s.graph == nil {
		return false
	}
	filter := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			filter[id] = struct{}{}
		}
	}
	changed := false
	for i := range s.entries {
		if len(filter) > 0 {
			if _, ok := filter[s.entries[i].ID]; !ok {
				continue
			}
		}
		newIDs := s.graph.relatedIDsFor(s.entries[i].ID)
		newEdges := s.graph.relatedEdgesFor(s.entries[i].ID)
		if !sameStringSlice(newIDs, s.entries[i].RelatedIDs) || !sameRelatedEdges(newEdges, s.entries[i].RelatedEdges) {
			s.entries[i].RelatedIDs = newIDs
			s.entries[i].RelatedEdges = newEdges
			changed = true
		}
	}
	return changed
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameRelatedEdges(a, b []RelatedEdge) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Strength != b[i].Strength || a[i].LinkType != b[i].LinkType || !a[i].UpdatedAt.Equal(b[i].UpdatedAt) {
			return false
		}
	}
	return true
}

// rebuildDerivedIndexesLocked rebuilds every index derived from s.entries.
// Caller MUST hold s.mu write lock, or be in Store construction before sharing.
func (s *Store) rebuildDerivedIndexesLocked(syncGraphLinks bool) bool {
	s.bm25.rebuild(s.entries)
	s.vecIndex.rebuild(s.entries)
	s.graph.rebuild(s.entries)
	graphLinksChanged := false
	if syncGraphLinks {
		graphLinksChanged = s.syncGraphLinksLocked()
	}
	if s.tmt != nil {
		s.tmt.Rebuild(s.entries)
	}
	if s.entityIndex != nil {
		s.entityIndex.Rebuild(s.entries)
	}
	if s.semanticGraph != nil {
		s.semanticGraph.Rebuild(s.entries)
		// Rebuild inference engine whenever semantic graph changes.
		s.inferenceEngine = NewInferenceEngine(s.semanticGraph, nil)
	}
	if s.projIndex != nil {
		s.projIndex.Rebuild(s.entries)
	}
	s.rebuildThemeLayerLocked()
	return graphLinksChanged
}

// rebuildThemeLayerLocked keeps the xMemory-style theme layer in sync with the
// authoritative store entries. Caller MUST hold s.mu write lock, or be in Store
// rebuildThemeLayerLocked marks the theme layer as needing a rebuild.
// The actual rebuild happens lazily via ThemeManager.EnsureUpToDate()
// when themes are queried (adaptive recall, memory tool, diversity rerank).
// Caller MUST hold s.mu write lock, or be in Store construction before sharing.
func (s *Store) rebuildThemeLayerLocked() {
	if s.themeManager == nil {
		return
	}
	s.themeManager.MarkDirty()
}

func firstOwnerID(ownerID ...string) string {
	if len(ownerID) == 0 {
		return ""
	}
	return ownerID[0]
}

// supersedeEntryLocked invalidates a fact at the memory lifecycle level and
// synchronizes every derived index that can expose active facts. Caller MUST
// hold s.mu write lock.
func (s *Store) supersedeEntryLocked(id string, invalidAt time.Time) bool {
	for i := range s.entries {
		if s.entries[i].ID != id {
			continue
		}
		changed := false
		if s.entries[i].Status != StatusSuperseded {
			s.entries[i].Status = StatusSuperseded
			changed = true
		}
		if !s.entries[i].Stale {
			s.entries[i].Stale = true
			changed = true
		}
		if s.entries[i].InvalidAt == nil {
			t := invalidAt
			if !s.entries[i].CreatedAt.IsZero() && !t.After(s.entries[i].CreatedAt) {
				t = s.entries[i].CreatedAt.Add(time.Nanosecond)
			}
			s.entries[i].InvalidAt = &t
			changed = true
		}
		if !changed {
			return false
		}
		s.bm25.updateEntry(s.entries[i])
		s.vecIndex.remove(id)
		if s.graph != nil {
			s.graph.remove(id)
			s.syncGraphLinksLocked()
		}
		if s.entityIndex != nil {
			s.entityIndex.RemoveEntry(id)
		}
		if s.semanticGraph != nil {
			s.semanticGraph.IndexEntry(&s.entries[i])
		}
		if s.projIndex != nil {
			s.projIndex.Rebuild(s.entries)
		}
		s.rebuildThemeLayerLocked()
		s.dirty = true
		return true
	}
	return false
}

// RecallDynamic retrieves memory entries matching the given query, excluding
// user_fact entries (which are injected separately as a compressed summary).
// Uses RRF (Reciprocal Rank Fusion) with Memory Stream scoring.
// Filters out dormant and superseded entries.
//
// ownerID is optional (variadic). When provided and non-empty, only entries
// with matching OwnerID or empty OwnerID (shared) are returned. This enables
// multi-tenant isolation in maclawsrv. In GUI/TUI (single-user), omit ownerID
// or pass empty string; all entries are returned.
func (s *Store) RecallDynamic(query string, category Category, projectPath string, ownerID ...string) []Entry {
	return s.recallDynamicCore(query, category, projectPath, false, ownerID...)
}

// recallDynamicCore is the shared implementation for RecallDynamic and RecallDynamicStrict.
// When strictProject=true: ScopeProject entries must have tags matching current projectPath;
// other projects' project_knowledge is excluded; ScopeGlobal + user_fact + preference always allowed.
// When strictProject=false: default behavior (soft project filtering) unchanged.
func (s *Store) recallDynamicCore(query string, category Category, projectPath string, strictProject bool, ownerID ...string) []Entry {
	// Query Expand: extract entities for multi-query BM25 + tokens for tag matching.
	expanded := ExpandQuery(query)
	bm25Scores := s.multiQueryBM25(query, expanded.Entities)
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(query))
	semanticScores := map[string]float64{}
	semanticHitDebug := map[string]SemanticSearchHit{}
	if s.semanticGraph != nil {
		temporalMode, asOf := semanticTemporalOptionsFromQuery(query)
		for _, hit := range s.semanticGraph.SearchWithOptions(expanded.Entities, SemanticSearchOptions{
			Now:             time.Now(),
			AsOf:            asOf,
			OwnerID:         firstOwnerID(ownerID...),
			ProjectPath:     projectPath,
			RelationHints:   semanticRelationHintsFromQuery(query, expanded),
			SeedWeights:     semanticSeedWeightsFromEntities(expanded.Entities),
			MaxHits:         30,
			MaxVisitedFacts: 500,
			TemporalMode:    temporalMode,
		}) {
			semanticScores[hit.EntryID] = hit.Score
			semanticHitDebug[hit.EntryID] = hit
		}
	}
	// Multi-hop inference: derive implicit facts from the semantic graph.
	// Note: s.inferenceEngine is read without s.mu — same pattern as s.semanticGraph
	// above. Pointer read is safe on 64-bit (atomic at hardware level). Worst case
	// is reading a stale engine (gives slightly outdated results) or nil (no results).
	var derivedFacts []DerivedFact
	if s.inferenceEngine != nil && len(expanded.Entities) > 0 {
		derivedFacts = s.inferenceEngine.Infer(expanded.Entities, InferenceOptions{
			Now:             time.Now(),
			OwnerID:         firstOwnerID(ownerID...),
			ProjectPath:     projectPath,
			MaxDerived:      10,
			MinConfidence:   0.50,
			MaxVisitedFacts: 200,
		})
		// Boost source entries of derived facts so they rank higher.
		for _, df := range derivedFacts {
			for _, sf := range df.SourceFacts {
				if sf.EntryID != "" {
					semanticScores[sf.EntryID] += df.Confidence * 1.5
				}
			}
		}
	}
	s.mu.RLock()

	const maxEntries = 15
	const maxTokens = 2500

	projectLower := semanticNormalizeProjectPath(projectPath)
	now := time.Now()

	// Extract optional ownerID for multi-tenant filtering.
	filterOwner := ""
	if len(ownerID) > 0 {
		filterOwner = ownerID[0]
	}

	type rawCandidate struct {
		entry Entry
		bm25  float64
		vec   float64
		sem   float64
	}
	var raw []rawCandidate

	for _, e := range s.entries {
		if !e.IsActive() {
			continue
		}
		if strictProject && projectLower != "" {
			// Strict project mode: use recallStrictProjectEntryAllowed for
			// ScopeProject entries, and allow ScopeGlobal + user_fact + preference.
			if !recallDynamicEntryAllowedStrict(e, category, projectLower, filterOwner) {
				continue
			}
		} else {
			if !recallDynamicEntryAllowed(e, category, projectLower, filterOwner) {
				continue
			}
		}
		b := bm25Scores[e.ID]
		v := 0.0
		if vs, ok := vecScores[e.ID]; ok {
			v = vs
		}
		raw = append(raw, rawCandidate{entry: e, bm25: b, vec: v, sem: semanticScores[e.ID]})
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
		fusedRelevance := rrfScores[i]
		if c.sem > 0 {
			fusedRelevance += c.sem
		}
		sc := memoryStreamScore(c.entry, fusedRelevance, c.bm25, projectLower, now)
		candidates = append(candidates, recallScored{entry: c.entry, score: sc})
	}

	// Tag exact match boost: when a query entity exactly matches an entry's
	// tag, give a significant score boost. This bridges the "write-recall
	// semantic gap: e.g. user saved SSH info with tag "4090-server" from
	// conversation context, and later queries "4090 GPU server".
	// The BM25/Vec channels may miss this because the content doesn't contain
	// "4090", but the tag does.
	if len(expanded.Entities) > 0 {
		for i := range candidates {
			boost := tagExactMatchBoost(candidates[i].entry, expanded.Entities)
			candidates[i].score += boost
		}
	}

	// OpenHuman-inspired: apply stability boost/penalty.
	// Stable knowledge (+2.0) is more reliable; volatile knowledge (-1.0) may be outdated.
	for i := range candidates {
		candidates[i].score += candidates[i].entry.Stability.StabilityBoost()
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// 1-hop graph expansion: expand top candidates to discover related entries.
	candidates = s.graphExpand(candidates, graphExpandSeeds)

	// Re-apply the full dynamic visibility contract after graph expansion: graph
	// edges can cross owner, project, or category boundaries that the seed set had
	// already filtered out.
	if strictProject && projectLower != "" {
		candidates = filterRecallDynamicCandidatesStrict(candidates, category, projectLower, filterOwner)
	} else {
		candidates = filterRecallDynamicCandidates(candidates, category, projectLower, filterOwner)
	}
	if ClassifyComplexity(query, expanded.Entities, nil) != ComplexitySimple && s.themeManager != nil {
		candidates = themeAwareDiversityRerank(candidates, s.themeManager.Themes(), graphExpandSeeds)
	}

	// Recall gating removed from hot path (see memory-simplification-plan.md).
	// Gating is available via RecallAdaptiveHier for precision-sensitive paths.
	var result []Entry
	tokenBudget := maxTokens
	for _, sc := range candidates {
		if len(result) >= maxEntries {
			break
		}
		tokens := EstimateTextTokens(sc.entry.Content)
		if tokens > tokenBudget {
			continue
		}
		tokenBudget -= tokens
		result = append(result, sc.entry)
	}
	s.mu.RUnlock()

	finalSemanticHits := make(map[string]SemanticSearchHit)
	for _, entry := range result {
		if hit, ok := semanticHitDebug[entry.ID]; ok {
			finalSemanticHits[entry.ID] = hit
		}
	}
	s.mu.Lock()
	s.lastSemanticHits = finalSemanticHits
	s.lastDerivedFacts = derivedFacts
	s.lastRecallTrace = newRecallTrace(query, category, projectPath, expanded, bm25Scores, vecScores, semanticScores, candidates, result)
	s.mu.Unlock()
	return result
}

// RecallDynamicStrict performs project-isolated recall for Project Tab scenarios.
// It delegates to RecallDynamic and then applies strict project filtering:
//   - Include entries whose tags contain the given projectPath
//   - Include entries with Scope != ScopeProject (universal knowledge: user_fact, preference, self_identity)
//   - Exclude entries with Scope == ScopeProject whose tags do NOT contain the current projectPath
//
// This ensures that a Project Tab only sees its own project knowledge plus
// universal/global knowledge, never another project's entries.
//
// To compensate for budget slots consumed by entries that will be filtered out,
// this method calls RecallDynamic twice if the first pass yields fewer than 5
// results after strict filtering — the second pass uses a broader query to
// backfill the budget.
func (s *Store) RecallDynamicStrict(query string, category Category, projectPath string, ownerID ...string) []Entry {
	results := s.recallDynamicCore(query, category, projectPath, true, ownerID...)
	if projectPath == "" {
		return results
	}
	projectLower := semanticNormalizeProjectPath(projectPath)
	if projectLower == "" {
		return results
	}
	// If strict filtering (now applied during candidate selection in recallDynamicCore)
	// yielded fewer than 5 results, do a second recall pass with a project-specific
	// query to backfill. This ensures the effective budget isn't starved.
	if len(results) < 5 {
		backfillQuery := query + " " + projectPath
		backfill := s.recallDynamicCore(backfillQuery, category, projectPath, true, ownerID...)
		seen := make(map[string]bool, len(results))
		for _, e := range results {
			seen[e.ID] = true
		}
		for _, e := range backfill {
			if seen[e.ID] {
				continue
			}
			// recallDynamicCore with strictProject=true already filters, but
			// double-check with recallStrictProjectEntryAllowed for safety.
			if recallStrictProjectEntryAllowed(e, projectLower) {
				results = append(results, e)
				seen[e.ID] = true
			}
			if len(results) >= 12 {
				break
			}
		}
	}
	return results
}

// recallStrictProjectEntryAllowed implements the strict project filtering rule:
//   - Scope != ScopeProject → always allowed (global knowledge, user_fact, preference, self_identity)
//   - Scope == ScopeProject AND tags contain projectPath → allowed (belongs to this project)
//   - Scope == ScopeProject AND tags do NOT contain projectPath → excluded (belongs to another project)
func recallStrictProjectEntryAllowed(e Entry, projectLower string) bool {
	if e.Scope != ScopeProject {
		return true
	}
	// ScopeProject entry: must have a tag matching the current project path.
	for _, tag := range e.Tags {
		tl := semanticNormalizeProjectPath(tag)
		if !semanticLooksLikePath(tl) {
			continue
		}
		if semanticProjectPathMatches(tl, projectLower) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Three-layer search (inspired by GBrain's keyword / hybrid / direct modes)
// ---------------------------------------------------------------------------

// SearchByMode dispatches to the appropriate search strategy based on mode.
func (s *Store) SearchByMode(query string, mode SearchMode, category Category, projectPath string, limit int, ownerID ...string) []Entry {
	switch mode {
	case SearchDirect:
		return s.SearchDirectByIDForProject(query, category, projectPath)
	case SearchKeywordOnly:
		return s.SearchKeywordForProject(query, category, projectPath, limit)
	default:
		return limitSearchResults(s.RecallDynamic(query, category, projectPath, ownerID...), limit)
	}
}

func recallDynamicEntryAllowed(e Entry, category Category, projectLower, filterOwner string) bool {
	if !e.IsActive() || !recallProjectEntryAllowed(e, projectLower) {
		return false
	}
	if filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner {
		return false
	}
	if category != "" {
		return recallCategoryMatches(e.Category, category)
	}
	switch e.Category {
	case CategoryUserFact, CategorySelfIdentity, CategorySessionCheckpoint, CategoryConversationSummary:
		return false
	default:
		return true
	}
}

func filterRecallDynamicCandidates(candidates []recallScored, category Category, projectLower, filterOwner string) []recallScored {
	if len(candidates) == 0 {
		return candidates
	}
	filtered := candidates[:0]
	for _, c := range candidates {
		if recallDynamicEntryAllowed(c.entry, category, projectLower, filterOwner) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// recallDynamicEntryAllowedStrict implements strict project filtering for Project Tab:
//   - ScopeProject + tags match current projectPath → allowed
//   - ScopeProject + tags don't match current projectPath → excluded (other projects' knowledge)
//   - ScopeGlobal → allowed (archived experience, universal knowledge)
//   - user_fact / preference → allowed (user preferences always available)
//   - Other projects' project_knowledge → excluded
//
// This is more restrictive than recallDynamicEntryAllowed (soft filter) which allows
// all non-ScopeProject entries through.
func recallDynamicEntryAllowedStrict(e Entry, category Category, projectLower, filterOwner string) bool {
	if !e.IsActive() {
		return false
	}
	// Multi-tenant owner filtering (same as non-strict).
	if filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner {
		return false
	}
	// Category filter (same as non-strict).
	if category != "" {
		if !recallCategoryMatches(e.Category, category) {
			return false
		}
	} else {
		// General recall: exclude internal categories (same as non-strict).
		switch e.Category {
		case CategoryUserFact, CategorySelfIdentity, CategorySessionCheckpoint, CategoryConversationSummary:
			return false
		}
	}
	// Strict project filtering logic:
	// user_fact and preference are always allowed regardless of scope.
	if e.Category == CategoryUserFact || e.Category == CategoryPreference {
		return true
	}
	// ScopeGlobal entries are always allowed (archived experience, universal knowledge).
	if e.Scope == ScopeGlobal {
		return true
	}
	// ScopeProject entries must have tags matching the current project path.
	if e.Scope == ScopeProject {
		return recallStrictProjectEntryAllowed(e, projectLower)
	}
	// Non-scoped entries (empty scope): allow if they have matching project tags
	// or no project-like tags at all (generic knowledge).
	hasProjectTag := false
	for _, tag := range e.Tags {
		tl := semanticNormalizeProjectPath(tag)
		if semanticLooksLikePath(tl) {
			hasProjectTag = true
			if semanticProjectPathMatches(tl, projectLower) {
				return true
			}
		}
	}
	// If entry has project tags but none match → exclude (belongs to another project).
	// If entry has no project tags → allow (generic knowledge).
	return !hasProjectTag
}

// filterRecallDynamicCandidatesStrict re-applies strict project filtering after
// graph expansion (which can pull in entries from other projects via edges).
func filterRecallDynamicCandidatesStrict(candidates []recallScored, category Category, projectLower, filterOwner string) []recallScored {
	if len(candidates) == 0 {
		return candidates
	}
	filtered := candidates[:0]
	for _, c := range candidates {
		if recallDynamicEntryAllowedStrict(c.entry, category, projectLower, filterOwner) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func recallCategoryMatches(entryCategory, requested Category) bool {
	return entryCategory == requested || MapToCanonical(entryCategory) == MapToCanonical(requested)
}

func limitSearchResults(results []Entry, limit int) []Entry {
	if limit <= 0 || len(results) <= limit {
		return results
	}
	return append([]Entry(nil), results[:limit]...)
}

func recallDirectEntryAllowed(e Entry, category Category, projectLower string) bool {
	if !e.IsActive() || !recallProjectEntryAllowed(e, projectLower) {
		return false
	}
	if category != "" {
		return recallCategoryMatches(e.Category, category)
	}
	return true
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

// SearchDirectByIDForProject returns an exact ID match if it is visible in the requested project/category scope.
func (s *Store) SearchDirectByIDForProject(id string, category Category, projectPath string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projectLower := semanticNormalizeProjectPath(projectPath)
	for _, e := range s.entries {
		if e.ID == id && recallDirectEntryAllowed(e, category, projectLower) {
			return []Entry{e}
		}
	}
	return nil
}

// SearchKeyword performs BM25-only search without vector scoring.
func (s *Store) SearchKeyword(query string, category Category, limit int) []Entry {
	return s.SearchKeywordForProject(query, category, "", limit)
}

// SearchKeywordForProject performs BM25-only search within the same project
// visibility contract used by hybrid recall.
func (s *Store) SearchKeywordForProject(query string, category Category, projectPath string, limit int) []Entry {
	if limit <= 0 {
		limit = 15
	}
	scores := s.bm25.score(query)
	projectLower := semanticNormalizeProjectPath(projectPath)

	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		entry Entry
		score float64
	}
	var candidates []scored
	for _, e := range s.entries {
		if !recallDynamicEntryAllowed(e, category, projectLower, "") {
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
// Caller should hold NO lock; this method acquires its own.
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
// Dream Cycle - background self-healing (inspired by GBrain's dream cycle)
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

	// Phase 2: Auto-link discovery; find high-BM25 pairs that aren't linked.
	result.LinksDiscovered = s.discoverMissingLinks()

	// Phase 3: Content hash backfill.
	result.HashesBackfilled = s.backfillContentHashes()

	// Phase 4: Tag backfill; enrich old entries that have poor tags.
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
	// Sample up to 50 entries to avoid O(n^2) on large stores.
	// Copy the sample to avoid holding references to mutable entries.
	src := s.entries
	if len(src) > 50 {
		src = src[len(src)-50:]
	}
	sample := make([]Entry, len(src))
	copy(sample, src)

	// Build a tag-to-entryIDs index for tag-overlap link discovery.
	tagIndex := make(map[string][]string) // tag (lowered) -> entry IDs
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

		// Tag-overlap link discovery: entries sharing at least 2 meaningful tags
		// should be linked even if their content text is dissimilar.
		tagOverlapCounts := make(map[string]int) // other entry ID -> shared tag count
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
		// Update persisted graph links on affected entries.
		s.mu.Lock()
		s.syncGraphLinksLocked()
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

	// Extract tags (no lock needed; ExpandQuery is pure computation).
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
			if s.semanticGraph != nil {
				s.semanticGraph.IndexEntry(&s.entries[i])
			}
			count++
		}
	}
	if count > 0 && s.projIndex != nil {
		s.projIndex.Rebuild(s.entries)
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

		// Wait briefly for syncLoop to exit (it checks s.stopCh on each tick).
		// The syncLoop may be in the middle of syncOnce(); give it time to finish.
		if s.sync != nil {
			// Signal sync stop and wait a short grace period.
			select {
			case <-s.sync.stopCh:
				// Already closed (shouldn't happen, but defensive).
			default:
				close(s.sync.stopCh)
			}
			// Brief sleep to let syncOnce() finish if it's mid-execution.
			// syncOnce holds s.mu.Lock briefly; acquiring it here ensures it's done.
			s.mu.Lock()
			s.mu.Unlock()
		}

		if s.archive != nil {
			s.archive.Stop()
		}

		// Close the storage backend (releases DB connections / file handles).
		// Safe now: syncLoop has exited (stopCh closed + lock fence above).
		if s.backend != nil {
			_ = s.backend.Close()
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
	normalizeEntryTimestamp(entry, now)
	entry.AccessCount = 1

	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, *entry)
	s.bm25.addEntry(*entry)
	s.vecIndex.add(entry.ID, entry.Embedding)
	if s.projIndex != nil {
		s.projIndex.Rebuild(s.entries)
	}
	if s.entityIndex != nil {
		s.entityIndex.IndexEntry(entry)
	}
	if s.semanticGraph != nil {
		s.semanticGraph.IndexEntry(entry)
	}
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
	normalizeEntryTimestamps(entries)
	s.entries = entries
	s.rebuildDerivedIndexesLocked(false)
}

func normalizeEntryTimestamps(entries []Entry) {
	now := time.Now()
	for i := range entries {
		normalizeEntryTimestamp(&entries[i], now)
	}
}

func normalizeEntryTimestamp(e *Entry, fallback time.Time) {
	if e == nil {
		return
	}
	if e.UpdatedAt.IsZero() {
		if !e.CreatedAt.IsZero() {
			e.UpdatedAt = e.CreatedAt
		} else {
			e.UpdatedAt = fallback
		}
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = e.UpdatedAt
	}
}

func (s *Store) markDirtyLocked() {
	s.dirty = true
	s.signalSave()
}

func (s *Store) persistInsertedEntryLocked(entry *Entry) error {
	if s.backend != nil {
		return s.backend.SaveEntry(entry)
	}
	s.markDirtyLocked()
	return nil
}

func (s *Store) persistUpdatedEntryLocked(entry *Entry) error {
	if s.backend != nil {
		return s.backend.UpdateEntry(entry)
	}
	s.markDirtyLocked()
	return nil
}

func (s *Store) persistDeletedEntryLocked(id string) error {
	if s.backend != nil {
		return s.backend.DeleteEntry(id)
	}
	s.markDirtyLocked()
	return nil
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

// SetBackend sets the storage backend and optionally starts the sync loop.
// Must be called before any Save/Update/Delete operations if using SQLite backend.
// If not called, the Store uses its built-in JSON persistence (legacy behavior).
func (s *Store) SetBackend(backend StorageBackend, syncCfg SyncConfig) {
	s.backend = backend
	s.startSyncLoop(syncCfg)
}

// Backend returns the current storage backend, or nil if not set.
func (s *Store) Backend() StorageBackend { return s.backend }

// UniqueOwnerIDs returns a deduplicated list of all OwnerIDs in the store.
// Empty OwnerID (shared entries) is excluded from the result.
// Used by Pipeline to run consolidation per-user in multi-tenant mode.
func (s *Store) UniqueOwnerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	for _, e := range s.entries {
		if e.OwnerID != "" {
			seen[e.OwnerID] = true
		}
	}

	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result
}

// queryEmbeddingCached returns the embedding for a query string.
// Returns nil if no embedder is configured (graceful degradation).
func (s *Store) queryEmbeddingCached(query string) []float32 {
	s.mu.RLock()
	emb := s.embedder
	gen := s.embedderGen
	s.mu.RUnlock()
	if emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	key := strings.TrimSpace(query)
	if key == "" {
		return nil
	}
	now := time.Now()
	s.queryEmbMu.Lock()
	if entry, ok := s.queryEmbCache[key]; ok && entry.generation == gen && now.Sub(entry.createdAt) < queryEmbeddingCacheTTL {
		entry.lastUsed = now
		s.queryEmbCache[key] = entry
		vec := append([]float32(nil), entry.vec...)
		s.queryEmbMu.Unlock()
		return vec
	}
	if flight, ok := s.queryEmbFlight[key]; ok && flight.generation == gen {
		s.queryEmbMu.Unlock()
		<-flight.done
		if flight.err != nil || len(flight.vec) == 0 || s.currentEmbedderGeneration() != gen {
			return nil
		}
		return append([]float32(nil), flight.vec...)
	}
	flight := &queryEmbeddingFlight{generation: gen, done: make(chan struct{})}
	if s.queryEmbFlight == nil {
		s.queryEmbFlight = make(map[string]*queryEmbeddingFlight)
	}
	s.queryEmbFlight[key] = flight
	s.queryEmbMu.Unlock()

	embVec, err := emb.Embed(query)
	vec := append([]float32(nil), embVec...)
	currentGen := s.currentEmbedderGeneration()

	s.queryEmbMu.Lock()
	flight.err = err
	if err == nil && len(vec) > 0 {
		flight.vec = append([]float32(nil), vec...)
		if currentGen == gen {
			if s.queryEmbCache == nil {
				s.queryEmbCache = make(map[string]queryEmbeddingCacheEntry)
			}
			s.queryEmbCache[key] = queryEmbeddingCacheEntry{vec: append([]float32(nil), vec...), generation: gen, createdAt: now, lastUsed: now}
			s.evictQueryEmbeddingCacheLocked(now)
		}
	}
	if s.queryEmbFlight[key] == flight {
		delete(s.queryEmbFlight, key)
	}
	close(flight.done)
	s.queryEmbMu.Unlock()
	if err != nil || len(vec) == 0 || currentGen != gen {
		return nil
	}
	return append([]float32(nil), vec...)
}

func (s *Store) evictQueryEmbeddingCacheLocked(now time.Time) {
	if len(s.queryEmbCache) == 0 {
		return
	}
	for key, entry := range s.queryEmbCache {
		if now.Sub(entry.createdAt) >= queryEmbeddingCacheTTL {
			delete(s.queryEmbCache, key)
		}
	}
	for len(s.queryEmbCache) > maxQueryEmbeddingCacheEntries {
		oldestKey := ""
		oldest := now
		for key, entry := range s.queryEmbCache {
			if oldestKey == "" || entry.lastUsed.Before(oldest) {
				oldestKey = key
				oldest = entry.lastUsed
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.queryEmbCache, oldestKey)
	}
}

func (s *Store) currentEmbedderGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embedderGen
}

func (s *Store) clearQueryEmbeddingCache() {
	s.queryEmbMu.Lock()
	s.queryEmbCache = make(map[string]queryEmbeddingCacheEntry)
	s.queryEmbFlight = make(map[string]*queryEmbeddingFlight)
	s.queryEmbMu.Unlock()
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

	// Pre-compute tag overlap counts in one pass.
	tagOverlapByID := make(map[string]int)
	for i := range s.entries {
		e := &s.entries[i]
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

		// Threshold: require cosine > threshold OR at least 2 shared tags.
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

	// Update persisted graph links on the new entry and linked entries.
	ids := make([]string, 0, len(candidates)+1)
	ids = append(ids, entry.ID)
	for _, c := range candidates {
		ids = append(ids, c.id)
	}
	s.syncGraphLinksLocked(ids...)
}

// SetEmbedder wires an Embedder into the store. If the embedder is real
// (not NoopEmbedder), a background goroutine is launched to compute
// embeddings for any existing entries that are missing them.
// Safe to call repeatedly; changing the embedder invalidates query embedding cache entries.
func (s *Store) SetEmbedder(e embedding.Embedder) {
	s.mu.Lock()
	s.embedder = e
	s.embedderGen++
	gen := s.embedderGen
	s.mu.Unlock()
	s.clearQueryEmbeddingCache()
	if e == nil || embedding.IsNoop(e) {
		return
	}
	go s.backfillEmbeddings(e, gen)
}

// EmbedderActive returns true if a real (non-noop) embedder is loaded.
func (s *Store) EmbedderActive() bool {
	s.mu.RLock()
	emb := s.embedder
	s.mu.RUnlock()
	return emb != nil && !embedding.IsNoop(emb)
}

// EmbedderDim returns the embedding dimension, or 0 if no embedder is active.
func (s *Store) EmbedderDim() int {
	s.mu.RLock()
	emb := s.embedder
	s.mu.RUnlock()
	if emb == nil {
		return 0
	}
	return emb.Dim()
}

// backfillEmbeddings scans entries missing embeddings and computes them
// in the background. It processes entries one at a time to avoid blocking
// the store for extended periods.
func (s *Store) backfillEmbeddings(emb embedding.Embedder, gen uint64) {
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

		embVec, err := emb.Embed(p.content)
		if err != nil || len(embVec) == 0 {
			continue
		}

		s.mu.Lock()
		if s.embedderGen != gen {
			s.mu.Unlock()
			return
		}
		for i := range s.entries {
			if s.entries[i].ID == p.id && len(s.entries[i].Embedding) == 0 {
				s.entries[i].Embedding = embVec
				if s.entries[i].IsActive() {
					s.vecIndex.add(p.id, embVec)
				}
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

// EntityIndex returns the entity index for entity-centric queries.
func (s *Store) EntityIndex() *EntityIndex { return s.entityIndex }

// SemanticGraph returns the typed Entity/Fact/Memory graph for relation-aware recall.
func (s *Store) SemanticGraph() *SemanticGraph { return s.semanticGraph }

// InferenceEngine returns the multi-hop reasoning engine (may be nil if graph is nil).
func (s *Store) InferenceEngine() *InferenceEngine { return s.inferenceEngine }

// LastDerivedFacts returns the derived facts from the most recent RecallDynamic call.
func (s *Store) LastDerivedFacts() []DerivedFact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DerivedFact, len(s.lastDerivedFacts))
	copy(out, s.lastDerivedFacts)
	return out
}

// LastSemanticHits returns the semantic graph explanations from the most recent RecallDynamic call.
func (s *Store) LastSemanticHits() map[string]SemanticSearchHit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]SemanticSearchHit, len(s.lastSemanticHits))
	for id, hit := range s.lastSemanticHits {
		paths := append([]string(nil), hit.Paths...)
		hit.Paths = paths
		out[id] = hit
	}
	return out
}

// SemanticRecallDebug runs only the semantic graph recall layer and returns path explanations.
func (s *Store) SemanticRecallDebug(query string, ownerID string) []SemanticSearchHit {
	return s.SemanticRecallDebugForProject(query, "", ownerID)
}

// SemanticRecallDebugForProject runs the semantic graph recall layer with the same
// project scope used by RecallDynamic.
func (s *Store) SemanticRecallDebugForProject(query string, projectPath string, ownerID string) []SemanticSearchHit {
	if s.semanticGraph == nil {
		return nil
	}
	expanded := ExpandQuery(query)
	temporalMode, asOf := semanticTemporalOptionsFromQuery(query)
	return s.semanticGraph.SearchWithOptions(expanded.Entities, SemanticSearchOptions{
		Now:             time.Now(),
		AsOf:            asOf,
		OwnerID:         ownerID,
		ProjectPath:     projectPath,
		RelationHints:   semanticRelationHintsFromQuery(query, expanded),
		SeedWeights:     semanticSeedWeightsFromEntities(expanded.Entities),
		MaxHits:         30,
		MaxVisitedFacts: 500,
		TemporalMode:    temporalMode,
	})
}

// ThemeManager returns the embedding-aware theme layer used by adaptive recall.
func (s *Store) ThemeManager() *ThemeManager { return s.themeManager }

// ThemeHealth reports coverage and connectivity diagnostics for the theme layer.
func (s *Store) ThemeHealth() ThemeHealth {
	if s == nil || s.themeManager == nil {
		return ThemeHealth{}
	}
	return s.themeManager.Health(s.List("", ""))
}

// ThemeExplanations returns top themes with representative source evidence.
func (s *Store) ThemeExplanations(themeLimit int, evidenceLimit int) []ThemeExplanation {
	if s == nil || s.themeManager == nil {
		return nil
	}
	return s.themeManager.ExplainThemes(s.List("", ""), themeLimit, evidenceLimit)
}

// ThemeDiagnostics returns actionable health issues for the theme layer.
func (s *Store) ThemeDiagnostics(limit int) ThemeDiagnosticReport {
	if s == nil || s.themeManager == nil {
		return ThemeDiagnosticReport{}
	}
	return s.themeManager.DiagnoseThemes(s.List("", ""), limit)
}

// ThemeMaintenancePlan returns a compact, non-destructive maintenance plan.
func (s *Store) ThemeMaintenancePlan(issueLimit int, actionLimit int) ThemeMaintenancePlan {
	return PlanThemeMaintenance(s.ThemeDiagnostics(issueLimit), actionLimit)
}

// OnlineExtractor returns the online extraction pipeline (may be nil).
func (s *Store) OnlineExtractor() *OnlineExtractor { return s.onlineExtractor }

// SetOnlineExtractor wires the Mem0-style online extraction pipeline.
func (s *Store) SetOnlineExtractor(oe *OnlineExtractor) {
	s.mu.Lock()
	s.onlineExtractor = oe
	s.mu.Unlock()
}

// FindByEntity returns all active entries that mention the given entity.
// Uses the entity index for fast lookup, then filters by active status.
func (s *Store) FindByEntity(entityName string) []Entry {
	return s.findByEntity(entityName, "", "", "", false)
}

// FindByEntityForProject returns active entity matches visible in a requested
// project/category/owner scope. Empty category, projectPath, or ownerID means
// that dimension is not restricted.
func (s *Store) FindByEntityForProject(entityName string, category Category, projectPath string, ownerID string) []Entry {
	return s.findByEntity(entityName, category, projectPath, ownerID, true)
}

func (s *Store) findByEntity(entityName string, category Category, projectPath string, ownerID string, scoped bool) []Entry {
	if s.entityIndex == nil {
		return nil
	}
	ids := s.entityIndex.FindByEntity(entityName)
	if len(ids) == 0 {
		return nil
	}

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	projectLower := semanticNormalizeProjectPath(projectPath)

	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Entry
	for _, e := range s.entries {
		if !idSet[e.ID] || !e.IsActive() {
			continue
		}
		if scoped {
			if ownerID != "" && e.OwnerID != "" && e.OwnerID != ownerID {
				continue
			}
			if !recallDirectEntryAllowed(e, category, projectLower) {
				continue
			}
		}
		result = append(result, e)
	}
	return result
}

// RecallWithBFS performs a BFS-based graph traversal search starting from
// seed entries found by the standard recall, expanding to n-hop neighbors.
// Inspired by Graphiti's breadth-first search, which discovers
// contextually similar entries through graph proximity.
func (s *Store) RecallWithBFS(query string, category Category, projectPath string, hops int, ownerID ...string) []Entry {
	// First, get seed entries from standard recall.
	seeds := s.RecallDynamic(query, category, projectPath, ownerID...)
	if len(seeds) == 0 {
		return nil
	}

	// Collect seed IDs.
	seedIDs := make([]string, 0, len(seeds))
	seedSet := make(map[string]bool, len(seeds))
	for _, e := range seeds {
		seedIDs = append(seedIDs, e.ID)
		seedSet[e.ID] = true
	}

	// BFS expand through the graph.
	expanded := s.graph.expand(seedIDs, hops)
	if len(expanded) == 0 {
		return seeds
	}

	// Collect expanded entry IDs (not already in seeds).
	var expandedIDs []string
	for id := range expanded {
		if !seedSet[id] {
			expandedIDs = append(expandedIDs, id)
		}
	}

	if len(expandedIDs) == 0 {
		return seeds
	}

	// Look up expanded entries under the same visibility contract used for seeds.
	filterOwner := ""
	if len(ownerID) > 0 {
		filterOwner = ownerID[0]
	}
	projectLower := semanticNormalizeProjectPath(projectPath)

	s.mu.RLock()
	expandedIDSet := make(map[string]bool, len(expandedIDs))
	for _, id := range expandedIDs {
		expandedIDSet[id] = true
	}
	var bfsEntries []Entry
	for _, e := range s.entries {
		if !expandedIDSet[e.ID] || !recallDynamicEntryAllowed(e, category, projectLower, filterOwner) {
			continue
		}
		bfsEntries = append(bfsEntries, e)
	}
	s.mu.RUnlock()

	// Combine: seeds first, then BFS-expanded entries.
	result := make([]Entry, 0, len(seeds)+len(bfsEntries))
	result = append(result, seeds...)
	result = append(result, bfsEntries...)
	return result
}

// Embedder returns the configured embedder (may be nil).
func (s *Store) Embedder() embedding.Embedder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embedder
}

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
			if s.semanticGraph != nil {
				s.semanticGraph.IndexEntry(&s.entries[i])
			}
			if s.projIndex != nil {
				s.projIndex.Rebuild(s.entries)
			}
			if err := s.persistUpdatedEntryLocked(&s.entries[i]); err != nil {
				return fmt.Errorf("memory_store: persist updated entry: %w", err)
			}
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
			if s.semanticGraph != nil {
				s.semanticGraph.IndexEntry(&s.entries[i])
			}
			if s.projIndex != nil {
				s.projIndex.Rebuild(s.entries)
			}
			if err := s.persistUpdatedEntryLocked(&s.entries[i]); err != nil {
				return fmt.Errorf("memory_store: persist updated entry: %w", err)
			}
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

	// Separate protected (self_identity) and pinned entries; they are never evicted.
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
		// Protected entries alone exceed maxItems; nothing else can be kept.
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
	s.rebuildDerivedIndexesLocked(false)

	if s.backend != nil && len(evicted) > 0 {
		for _, e := range evicted {
			if err := s.backend.DeleteEntry(e.ID); err != nil {
				log.Printf("[memory_store] WARNING: failed to delete evicted entry %s from backend: %v", e.ID, err)
			}
		}
	}

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

	// Try loading from partition files first.
	if s.partMgr != nil {
		if entries, ok := s.partMgr.loadPartitions(); ok {
			s.entries = entries
			s.partMgr.enable()
			log.Printf("[memory_store] loaded %d entries from partition files", len(entries))
			return nil
		}
	}

	// Fall back to legacy single file.
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// No legacy file either; fresh install. Keep using legacy mode
			// (partitions will be enabled on first migration when the store
			// grows large enough).
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

	// Migrate legacy file to partitions when the store is large enough.
	// Small stores (<100 entries) stay as single files; no overhead.
	const migrationThreshold = 100
	if s.partMgr != nil && len(entries) >= migrationThreshold {
		if err := s.partMgr.migrateFromLegacy(entries, s.path); err != nil {
			log.Printf("[memory_store] WARNING: partition migration failed: %v, continuing with legacy mode", err)
		}
	}

	return nil
}

func (s *Store) flush() error {
	s.mu.RLock()
	flushGen := atomic.LoadUint64(&s.dirtyGen)

	if s.backend != nil {
		if !s.dirty {
			s.mu.RUnlock()
			return nil
		}
		entries := make([]Entry, len(s.entries))
		copy(entries, s.entries)
		s.mu.RUnlock()

		for i := range entries {
			if err := s.backend.UpdateEntry(&entries[i]); err != nil {
				return fmt.Errorf("memory_store: backend flush entry %s: %w", entries[i].ID, err)
			}
		}
		s.mu.Lock()
		if atomic.LoadUint64(&s.dirtyGen) == flushGen {
			s.dirty = false
		}
		s.mu.Unlock()
		return nil
	}

	// Partitioned flush: write all partitions when dirty.
	if s.partMgr != nil && s.partMgr.isEnabled() && s.dirty {
		s.partMgr.markAllDirty()
		entries := make([]Entry, len(s.entries))
		copy(entries, s.entries)
		s.mu.RUnlock()

		// flushDirty operates on the copied slice; no lock needed.
		_, err := s.partMgr.flushDirty(entries)
		if err != nil {
			return fmt.Errorf("memory_store: partition flush: %w", err)
		}
		s.mu.Lock()
		if atomic.LoadUint64(&s.dirtyGen) == flushGen {
			s.dirty = false
		}
		s.mu.Unlock()
		return nil
	}

	if !s.dirty {
		s.mu.RUnlock()
		return nil
	}

	// Legacy single-file flush.
	data, err := json.MarshalIndent(s.entries, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("memory_store: marshal: %w", err)
	}
	if err := fileutil.AtomicWriteFile(s.path, data, 0o644); err != nil {
		return fmt.Errorf("memory_store: write file: %w", err)
	}
	s.mu.Lock()
	if atomic.LoadUint64(&s.dirtyGen) == flushGen {
		s.dirty = false
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) signalSave() {
	atomic.AddUint64(&s.dirtyGen, 1)
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

// findSubstringDuplicate checks if the new content is a substring of (or
// contains) a recent existing entry's content. Returns the index of the
// matching entry, or -1 if no match. Only scans the most recent 50 entries
// to bound write latency. Caller MUST hold s.mu.Lock.
//
// Multi-tenant isolation: only matches entries with the same OwnerID or
// shared entries (empty OwnerID). Different users' entries are never
// considered duplicates of each other.
func (s *Store) findSubstringDuplicate(content string, ownerID string) int {
	return s.findSubstringDuplicateForEntry(Entry{Content: content, OwnerID: ownerID})
}

func (s *Store) findSubstringDuplicateForEntry(entry Entry) int {
	lower := strings.ToLower(strings.TrimSpace(entry.Content))
	if len(lower) == 0 {
		return -1
	}

	// Canonical category for isolation check.
	canonicalCat := MapToCanonical(entry.Category)

	// Scan the most recent 50 entries (by slice position, which correlates
	// with creation order since new entries are appended).
	start := len(s.entries) - 50
	if start < 0 {
		start = 0
	}
	for i := start; i < len(s.entries); i++ {
		// Multi-tenant isolation: skip entries from different users.
		// Empty OwnerID (shared) can match with any user.
		existingOwner := s.entries[i].OwnerID
		if entry.OwnerID != "" && existingOwner != "" && existingOwner != entry.OwnerID {
			continue
		}
		// Category isolation: only dedup within the same canonical category.
		// This prevents a project_knowledge entry from being merged into a
		// user_fact entry (or vice versa) just because they share a substring.
		if entry.Category != "" && MapToCanonical(s.entries[i].Category) != canonicalCat {
			continue
		}
		if isDuplicateContentCandidate(lower, entry.Entities, s.entries[i]) {
			return i
		}
	}
	return -1
}

func isDuplicateContentCandidate(lowerContent string, incomingEntities []string, existing Entry) bool {
	existingContent := strings.ToLower(strings.TrimSpace(existing.Content))
	if len(existingContent) == 0 {
		return false
	}
	if len(lowerContent) >= minSubstringLen && len(existingContent) >= minSubstringLen {
		return strings.Contains(existingContent, lowerContent) || strings.Contains(lowerContent, existingContent)
	}
	if !strings.Contains(existingContent, lowerContent) && !strings.Contains(lowerContent, existingContent) {
		return false
	}
	return entityOverlapEvidence(incomingEntities, existing.Entities) >= 1
}

func entityOverlapEvidence(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(a))
	for _, token := range a {
		if name, ok := semanticEntityTokenName(token); ok {
			seen[normalizeEntityName(name)] = struct{}{}
		}
	}
	overlap := 0
	for _, token := range b {
		if name, ok := semanticEntityTokenName(token); ok {
			if _, ok := seen[normalizeEntityName(name)]; ok {
				overlap++
			}
		}
	}
	return overlap
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
//  1. BM25+Vector fusion via the RecallForProject candidate pipeline
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
	candidates := s.recallForProjectCandidates(userMessage, projectPath)
	if len(candidates) == 0 {
		return candidates
	}

	// If no LLM filter or too few candidates, return as-is and touch only the
	// entries that are actually returned.
	if llmFilter == nil || len(candidates) <= maxResults {
		if len(candidates) > maxResults {
			result := candidates[:maxResults]
			s.touchRecallResults(result)
			return result
		}
		s.touchRecallResults(candidates)
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
		// Fallback: return top-N from stage 1 and touch only that fallback set.
		if len(candidates) > maxResults {
			result := candidates[:maxResults]
			s.touchRecallResults(result)
			return result
		}
		s.touchRecallResults(candidates)
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

	s.touchRecallResults(result)
	return result
}

// RecallSmart is an enhanced recall entry point that integrates Query Expansion,
// Tag Fast Lane, dynamic budget, and optional LLM reranking.
// When llmFilter is nil or unavailable, it returns the same candidates as RecallForProject.
func (s *Store) RecallSmart(userMessage, projectPath string, llmFilter LLMRelevanceFilter) []Entry {
	candidates := s.recallForProjectCandidates(userMessage, projectPath)

	if llmFilter == nil || !llmFilter.IsAvailable() || len(candidates) <= 5 {
		s.touchRecallResults(candidates)
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
		s.touchRecallResults(candidates)
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

	s.touchRecallResults(result)
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
