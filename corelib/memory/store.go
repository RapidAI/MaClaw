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
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
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
	eventSink        lifecycle.EventSink          // shared experience lifecycle sink

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

	// --- Multi-page recall components ---
	cursorPaginator  *CursorPaginator      // cursor-based pagination state
	scrollSessions   *ScrollSessionManager  // per-loop scroll-through recall sessions
	pageIndex        *PageIndex             // cross-page context retrieval index
	aliasIndex       *AliasIndex            // bidirectional alias mappings for query expansion

	// lastRebuildDone is closed when the most recent background index rebuild
	// (from replaceEntriesAndRebuildAsync) completes. Used by WaitRebuild and
	// Stop to drain in-flight goroutines before closing the backend.
	lastRebuildDone chan struct{}

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

// SetExperienceEventSink connects memory recall to the shared experience
// lifecycle. Recall remains fully functional without a sink; wiring one lets
// tools, memory, and workflow attribution share the same event stream.
func (s *Store) SetExperienceEventSink(sink lifecycle.EventSink) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.eventSink = sink
	s.mu.Unlock()
}

// Paginator returns the store's CursorPaginator for cursor-based pagination.
// Used by HandleTool to dispatch paginated recall requests.
func (s *Store) Paginator() *CursorPaginator {
	if s == nil {
		return nil
	}
	return s.cursorPaginator
}

// ScrollSessions returns the store's ScrollSessionManager for scroll-through
// recall within agent loops. Hosts call Destroy(loopID) on agent loop exit.
func (s *Store) ScrollSessions() *ScrollSessionManager {
	if s == nil {
		return nil
	}
	return s.scrollSessions
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
		cursorPaginator: NewCursorPaginator(),
		scrollSessions:  NewScrollSessionManager(),
		pageIndex:       NewPageIndex(),
		aliasIndex:      NewAliasIndex(),
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
	if removed, err := s.ReconcileArchiveDuplicates(); err != nil {
		log.Printf("[memory_store] WARNING: reconcile archive duplicates: %v", err)
	} else if removed > 0 {
		log.Printf("[memory_store] reconciled %d active/archive duplicate entries", removed)
	}

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
			// Register aliases: each entity from contextHint is a potential alias
			// for other entities in the same context. This bridges the write-recall
			// semantic gap (e.g., user says "4090服务器" in context while saving
			// entry about "api.rapidai.tech").
			if s.aliasIndex != nil && len(ctxExpanded.Entities) > 1 {
				for _, entity := range ctxExpanded.Entities {
					// Register this entity with all other entities as aliases.
					var others []string
					for _, other := range ctxExpanded.Entities {
						if normalize(other) != normalize(entity) {
							others = append(others, other)
						}
					}
					if len(others) > 0 {
						s.aliasIndex.Register(entity, others)
					}
				}
			}
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

	now := time.Now()

	// Idempotent: check by content hash first (O(n) but fast string compare).
	// Multi-tenant isolation: only dedup within the same owner (or shared entries).
	s.mu.RLock()
	for i := range s.entries {
		if s.entries[i].ContentHash == hash || s.entries[i].Content == entry.Content {
			// Multi-tenant isolation: skip entries from different users.
			// Empty OwnerID (shared) can match with any user.
			existingOwner := s.entries[i].OwnerID
			if entry.OwnerID != "" && existingOwner != "" && existingOwner != entry.OwnerID {
				continue
			}
			updated := s.entries[i]
			updated.UpdatedAt = now
			updated.AccessCount++
			updated.Tags = mergeTags(updated.Tags, entry.Tags)
			updated.Entities = mergeStringSlice(updated.Entities, entry.Entities)
			if updated.ContentHash == "" {
				updated.ContentHash = hash
			}
			s.mu.RUnlock()
			if err := s.updateMetadataEntriesByID([]Entry{updated}); err != nil {
				return fmt.Errorf("memory_store: persist updated entry: %w", err)
			}
			return nil
		}
	}
	s.mu.RUnlock()

	// Substring dedup: check if the new content is a substring of (or contains)
	// a recent existing entry. This catches semantically duplicate entries that
	// differ in wording (e.g. KnowledgeExtractor extracts similar knowledge
	// points across sessions). Only scan the most recent 50 entries to bound
	// write latency. When a match is found, merge tags into the existing entry
	// instead of creating a duplicate.
	// Multi-tenant isolation: only dedup within the same owner (or shared entries).
	s.mu.RLock()
	if substringDupIdx := s.findSubstringDuplicateForEntry(entry); substringDupIdx >= 0 {
		updated := s.entries[substringDupIdx]
		updated.UpdatedAt = now
		updated.AccessCount++
		updated.Tags = mergeTags(updated.Tags, entry.Tags)
		updated.Entities = mergeStringSlice(updated.Entities, entry.Entities)
		// If the new content is a superset (contains the existing content),
		// update to the longer version to preserve more information.
		existingLen := len([]rune(updated.Content))
		newLen := len([]rune(entry.Content))
		if newLen > existingLen {
			updated.Content = entry.Content
			updated.CompactForm = ""
			updated.ContentHash = hash
			if len(entry.Embedding) > 0 {
				updated.Embedding = append([]float32(nil), entry.Embedding...)
			}
		}
		s.mu.RUnlock()
		if err := s.UpdateEntriesByID([]Entry{updated}); err != nil {
			return fmt.Errorf("memory_store: persist merged duplicate: %w", err)
		}
		log.Printf("[memory_store] merged substring duplicate into entry %s (kept longer: %v)", updated.ID, newLen > existingLen)
		return nil
	}
	s.mu.RUnlock()

	// Assign ID early so it's available for pending dedup tracking.
	if entry.ID == "" {
		entry.ID = generateID()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.insertPreparedEntryLocked(entry, hash, now, true)
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

	s.mu.RLock()
	duplicateID := ""
	var updated Entry
	found := false
	for _, e := range s.entries {
		if e.ID != id && e.Content == content {
			duplicateID = e.ID
			break
		}
	}

	if duplicateID == "" {
		for _, e := range s.entries {
			if e.ID == id {
				updated = e
				found = true
				break
			}
		}
	}
	s.mu.RUnlock()
	if duplicateID != "" {
		return fmt.Errorf("memory_store: duplicate content (matches entry %q)", duplicateID)
	}
	if !found {
		return fmt.Errorf("memory_store: entry %q not found", id)
	}
	updated.Content = content
	updated.Category = category
	updated.Tags = append([]string(nil), tags...)
	updated.CompactForm = "" // invalidate: content changed
	updated.ContentHash = computeContentHash(content)
	updated.Stale = false // content just updated, clear stale flag
	if err := s.UpdateEntriesByID([]Entry{updated}); err != nil {
		return fmt.Errorf("memory_store: persist updated entry: %w", err)
	}
	return nil
}

// UpdateEntriesByID updates several existing entries as one store-level batch.
// Validation is completed before any in-memory entry is changed. Backends that
// implement BatchStorageBackend persist the batch in one transaction.
func (s *Store) UpdateEntriesByID(entries []Entry) error {
	return s.upsertEntriesByID(entries, true, false)
}

// UpsertEntriesByID creates or updates several ID-addressed entries as one
// store-level batch. It is intended for governed state transitions where a new
// audit entry and existing source entries must persist together.
func (s *Store) UpsertEntriesByID(entries []Entry) error {
	return s.upsertEntriesByID(entries, false, false)
}

func (s *Store) updateMetadataEntriesByID(entries []Entry) error {
	return s.upsertEntriesByID(entries, true, true)
}

// UpdateEntriesAndDeleteIDs updates entries and removes other entries as one
// store-level mutation. SQLite backends persist the whole mutation in one
// transaction, which is required for merge flows that keep one memory and retire
// another.
func (s *Store) UpdateEntriesAndDeleteIDs(entries []Entry, deleteIDs []string) error {
	if s == nil || (len(entries) == 0 && len(deleteIDs) == 0) {
		return nil
	}
	if len(deleteIDs) == 0 {
		return s.UpdateEntriesByID(entries)
	}
	now := time.Now()
	desiredByID := make(map[string]Entry, len(entries))
	orderedIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Content = strings.TrimSpace(entry.Content)
		if entry.ID == "" {
			return fmt.Errorf("memory_store: entry id must not be empty")
		}
		if entry.Content == "" {
			return fmt.Errorf("memory_store: content must not be empty")
		}
		if err := ScanForInjection(entry.Content); err != nil {
			return fmt.Errorf("memory_store: rejected: %w", err)
		}
		entry.Content = redactSecretsInMemory(entry.Content)
		if _, exists := desiredByID[entry.ID]; !exists {
			orderedIDs = append(orderedIDs, entry.ID)
		}
		desiredByID[entry.ID] = entry
	}
	deleteSet := make(map[string]struct{}, len(deleteIDs))
	orderedDeletes := make([]string, 0, len(deleteIDs))
	for _, id := range deleteIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := desiredByID[id]; exists {
			return fmt.Errorf("memory_store: entry %q cannot be updated and deleted in the same batch", id)
		}
		if _, exists := deleteSet[id]; !exists {
			deleteSet[id] = struct{}{}
			orderedDeletes = append(orderedDeletes, id)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	indices := make([]int, 0, len(desiredByID))
	updated := make([]Entry, 0, len(desiredByID))
	updatedByID := make(map[string]int, len(desiredByID))
	for _, id := range orderedIDs {
		desired := desiredByID[id]
		idx := s.findEntryIndexByIDLocked(id)
		if idx < 0 {
			return fmt.Errorf("memory_store: entry %q not found", id)
		}
		current := s.entries[idx]
		if current.Content != desired.Content {
			snap := VersionSnapshot{Content: current.Content, Timestamp: current.UpdatedAt}
			versions := append([]VersionSnapshot(nil), current.Versions...)
			if len(versions) > 2 {
				versions = versions[len(versions)-2:]
			}
			current.Versions = append(versions, snap)
		}
		applyEntryMutationFields(&current, desired, now)
		current, _ = stripDeletedRelations(current, deleteSet)
		updatedByID[current.ID] = len(updated)
		indices = append(indices, idx)
		updated = append(updated, current)
	}
	for _, id := range orderedDeletes {
		if s.findEntryIndexByIDLocked(id) < 0 {
			return fmt.Errorf("memory_store: entry %q not found", id)
		}
	}
	for idx, current := range s.entries {
		if _, deleting := deleteSet[current.ID]; deleting {
			continue
		}
		if _, alreadyUpdated := updatedByID[current.ID]; alreadyUpdated {
			continue
		}
		cleaned, changed := stripDeletedRelations(current, deleteSet)
		if !changed {
			continue
		}
		updatedByID[cleaned.ID] = len(updated)
		indices = append(indices, idx)
		updated = append(updated, cleaned)
	}

	if batchBackend, ok := s.backend.(BatchMutationStorageBackend); ok {
		ptrs := make([]*Entry, len(updated))
		for i := range updated {
			ptrs[i] = &updated[i]
		}
		if err := batchBackend.UpdateEntriesAndDeleteIDs(ptrs, orderedDeletes); err != nil {
			return fmt.Errorf("memory_store: persist entry mutation batch: %w", err)
		}
	} else if s.backend != nil {
		return fmt.Errorf("memory_store: backend does not support atomic update/delete batch")
	}

	for i, idx := range indices {
		s.entries[idx] = updated[i]
	}
	if len(deleteSet) > 0 {
		kept := s.entries[:0]
		for _, entry := range s.entries {
			if _, deleting := deleteSet[entry.ID]; deleting {
				continue
			}
			kept = append(kept, entry)
		}
		s.entries = kept
	}
	s.rebuildDerivedIndexesLocked(false)
	if s.backend == nil {
		s.markDirtyLocked()
	}
	return nil
}

func (s *Store) upsertEntriesByID(entries []Entry, requireExisting bool, preserveContent bool) error {
	if s == nil || len(entries) == 0 {
		return nil
	}
	now := time.Now()
	desiredByID := make(map[string]Entry, len(entries))
	orderedIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry.ID = strings.TrimSpace(entry.ID)
		if !preserveContent {
			entry.Content = strings.TrimSpace(entry.Content)
		}
		if entry.ID == "" {
			return fmt.Errorf("memory_store: entry id must not be empty")
		}
		if !preserveContent && entry.Content == "" {
			return fmt.Errorf("memory_store: content must not be empty")
		}
		if !preserveContent {
			if err := ScanForInjection(entry.Content); err != nil {
				return fmt.Errorf("memory_store: rejected: %w", err)
			}
			entry.Content = redactSecretsInMemory(entry.Content)
		}
		if _, exists := desiredByID[entry.ID]; !exists {
			orderedIDs = append(orderedIDs, entry.ID)
		}
		desiredByID[entry.ID] = entry
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	indices := make([]int, 0, len(desiredByID))
	updated := make([]Entry, 0, len(desiredByID))
	for _, id := range orderedIDs {
		desired := desiredByID[id]
		idx := -1
		for i := range s.entries {
			if s.entries[i].ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			if requireExisting {
				return fmt.Errorf("memory_store: entry %q not found", id)
			}
			if desired.Category == "" {
				desired.Category = CategoryProjectKnowledge
			}
			if desired.Scope == "" {
				desired.Scope = InferScope(desired.Category)
			}
			if desired.CreatedAt.IsZero() {
				desired.CreatedAt = now
			}
			desired.UpdatedAt = now
			desired.ContentHash = computeContentHash(desired.Content)
			if desired.AccessCount == 0 {
				desired.AccessCount = 1
			}
			if desired.Strength == 0 {
				desired.Strength = 1
			}
			indices = append(indices, -1)
			updated = append(updated, desired)
			continue
		}
		current := s.entries[idx]
		if current.Content != desired.Content {
			snap := VersionSnapshot{Content: current.Content, Timestamp: current.UpdatedAt}
			versions := append([]VersionSnapshot(nil), current.Versions...)
			if len(versions) > 2 {
				versions = versions[len(versions)-2:]
			}
			current.Versions = append(versions, snap)
		}
		applyEntryMutationFields(&current, desired, now)
		indices = append(indices, idx)
		updated = append(updated, current)
	}

	if batchBackend, ok := s.backend.(BatchStorageBackend); ok {
		ptrs := make([]*Entry, len(updated))
		for i := range updated {
			ptrs[i] = &updated[i]
		}
		if err := batchBackend.UpdateEntries(ptrs); err != nil {
			return fmt.Errorf("memory_store: persist updated entry batch: %w", err)
		}
	} else if s.backend != nil {
		return fmt.Errorf("memory_store: backend does not support atomic batch update")
	}

	for i, idx := range indices {
		if idx < 0 {
			s.entries = append(s.entries, updated[i])
		} else {
			s.entries[idx] = updated[i]
		}
	}
	s.rebuildDerivedIndexesLocked(false)
	if s.backend == nil {
		s.markDirtyLocked()
	}
	return nil
}

// Delete removes the entry with the given ID.
func (s *Store) Delete(id string) error {
	if err := s.UpdateEntriesAndDeleteIDs(nil, []string{id}); err != nil {
		return fmt.Errorf("memory_store: delete entry: %w", err)
	}
	return nil
}

// ProjectIndex returns the project index for search and listing.
// Returns nil if the store has not been initialized.
func (s *Store) ProjectIndex() *ProjectIndex {
	return s.projIndex
}

// PageIdx returns the PageIndex for cross-page context retrieval.
// Hosts use this to call IndexCompactedPage on compaction and Clear on session reset.
func (s *Store) PageIdx() *PageIndex {
	return s.pageIndex
}

// AliasIdx returns the AliasIndex for write-recall semantic gap bridging.
func (s *Store) AliasIdx() *AliasIndex {
	return s.aliasIndex
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
	start := time.Now()
	result := s.recallForProjectCandidates(userMessage, projectPath)
	s.touchRecallResultsAsync(result)
	s.logRecallIfEnabled("project", userMessage, "", projectPath, nil, start, result)
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

	// Temporal demotion: entries marked stale or temporally invalidated are
	// demoted in score so they rank lower but remain discoverable. Applied
	// after graphExpand so expanded entries are also subject to demotion.
	// Implements the Dreaming V3 "stay current over time" principle using the
	// existing Stale flag (set by DetectStale/DreamCycle) and InvalidAt field
	// (set by OnlineExtractor OpDelete / SupersedeEntryByID).
	applyTemporalDemotion(others, now)
	// Re-sort after demotion to push stale/invalidated entries down.
	sort.SliceStable(others, func(i, j int) bool {
		if others[i].score != others[j].score {
			return others[i].score > others[j].score
		}
		return others[i].entry.AccessCount > others[j].entry.AccessCount
	})

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
	return semanticProjectAllowed(e.Scope, e.Tags, projectLower) && recallBoundaryAllowed(e, projectLower, "")
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

	s.mu.RLock()
	updates := make([]Entry, 0, len(idSet))
	for i := range s.entries {
		if _, ok := idSet[s.entries[i].ID]; ok {
			updated := s.entries[i]
			updated.AccessCount++
			// Boost forgetting curve strength so recalled memories don't decay.
			boostStrength(&updated, now)
			updates = append(updates, updated)
		}
	}
	s.mu.RUnlock()

	if len(updates) > 0 {
		if err := s.updateMetadataEntriesByID(updates); err != nil {
			log.Printf("[memory_store] persist access touches: %v", err)
		}
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
			tagLower := strings.ToLower(tag)
			if tagLower == entityLower {
				boost += 5.0 // strong boost for exact tag match
				break
			}
			// Containment boost: entity is a substring of a tag. This bridges
			// the gap between stored compound tags ("api2服务器", "api2.maclaw.top")
			// and shorter extracted entities ("api2", "SSH").
			// Only single-direction: entity ⊂ tag (entity is the query fragment,
			// tag is the stored label). Reverse direction (tag ⊂ entity) would
			// cause short tags like "go"/"ai" to spuriously match long entities.
			// Only apply for entities ≥ 3 runes to avoid noise from short tokens.
			//
			// For structured tags ("key:value" format like "tool:ssh"), only match
			// against the value part — matching the key ("tool") would cause massive
			// false positives on entries with many structured tags.
			if len([]rune(entityLower)) >= 3 {
				matchTarget := tagLower
				if colonIdx := strings.IndexByte(tagLower, ':'); colonIdx >= 0 {
					matchTarget = tagLower[colonIdx+1:]
				}
				if matchTarget != "" && strings.Contains(matchTarget, entityLower) {
					boost += 3.0 // moderate boost for containment match
					break
				}
			}
		}
	}
	// Tag specificity normalization: entries with many tags (e.g. 30+ tags on
	// an aggregated "adaptive retry" entry) match almost any query by chance.
	// Entries with few, precise tags (e.g. 3 tags on an SSH credential entry)
	// are much more discriminative when they match. Divide by sqrt(tagCount)
	// to reward specificity — this is analogous to IDF but for tag cardinality.
	if tagCount := len(entry.Tags); tagCount > 5 {
		boost /= math.Sqrt(float64(tagCount) / 5.0)
	}
	// Cap to prevent a single entry from dominating.
	if boost > 15.0 {
		boost = 15.0
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

// applyTemporalDemotion demotes stale and temporally-invalidated entries in a
// scored candidate list. Entries with InvalidAt in the past get score × 0.2;
// entries marked Stale get score × 0.3. This keeps them discoverable but
// pushes them below fresh entries in ranking.
//
// Implements the Dreaming V3 "stay current over time" principle.
// Call this after all scoring/expansion is complete, before final sort/truncation.
func applyTemporalDemotion(candidates []recallScored, now time.Time) {
	for i := range candidates {
		e := &candidates[i].entry
		if e.InvalidAt != nil && e.InvalidAt.Before(now) {
			candidates[i].score *= 0.2
		} else if e.Stale {
			candidates[i].score *= 0.3
		}
	}
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

// syncGraphLinksLocked is an internal reconstruction helper: it mirrors the
// in-memory graph onto each entry's persisted relationship fields after a graph
// rebuild. Public write paths should stage relationship changes through
// UpdateEntriesByID or UpdateEntriesAndDeleteIDs so persistence remains atomic.
// Caller MUST hold s.mu write lock.
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

func stripDeletedRelations(entry Entry, deleteSet map[string]struct{}) (Entry, bool) {
	if len(deleteSet) == 0 || (len(entry.RelatedIDs) == 0 && len(entry.RelatedEdges) == 0) {
		return entry, false
	}
	changed := false
	if len(entry.RelatedIDs) > 0 {
		kept := make([]string, 0, len(entry.RelatedIDs))
		for _, id := range entry.RelatedIDs {
			if _, deleting := deleteSet[id]; deleting {
				changed = true
				continue
			}
			kept = append(kept, id)
		}
		entry.RelatedIDs = kept
	}
	if len(entry.RelatedEdges) > 0 {
		kept := make([]RelatedEdge, 0, len(entry.RelatedEdges))
		for _, edge := range entry.RelatedEdges {
			if _, deleting := deleteSet[edge.ID]; deleting {
				changed = true
				continue
			}
			kept = append(kept, edge)
		}
		entry.RelatedEdges = kept
	}
	return entry, changed
}

// rebuildDerivedIndexesLocked rebuilds every index derived from s.entries.
// Caller MUST hold s.mu write lock, or be in Store construction before sharing.
// For incremental updates (single entry insert/delete), prefer the targeted
// addEntry/IndexEntry paths on individual sub-indexes. For the async batch sync
// path, use replaceEntriesAndRebuildAsync which releases s.mu before the
// expensive rebuild work.
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
	if s.aliasIndex != nil {
		s.aliasIndex.Rebuild(s.entries)
	}
	s.rebuildThemeLayerLocked()
	return graphLinksChanged
}

// rebuildDerivedIndexesOutsideLock rebuilds every sub-index that carries its
// own internal mutex using the provided snapshot of entries. This must NOT be
// called while s.mu is held — each sub-index serialises access internally, so
// holding the store-level write lock would create unnecessary contention.
//
// Called as a goroutine from replaceEntriesAndRebuildAsync after the batch
// sync applies its changes to s.entries under s.mu. The brief window between
// the lock release and the goroutine finishing means readers see the new
// entries immediately but slightly-stale index scores; this is acceptable
// because the same race window existed before (all rebuilds happened under
// the write lock anyway, blocking readers for just as long).
func (s *Store) rebuildDerivedIndexesOutsideLock(snapshot []Entry, syncGraphLinks bool) {
	s.bm25.rebuild(snapshot)
	s.vecIndex.rebuild(snapshot)
	s.graph.rebuild(snapshot)

	// syncGraphLinksLocked reads s.graph (just rebuilt from snapshot) and
	// writes RelatedIDs/RelatedEdges back onto s.entries. We must restrict
	// the write to entries that were in the snapshot: s.entries may have
	// grown since (concurrent Save + autoLink) and those newer entries have
	// their own correct graph links that must not be clobbered.
	//
	// syncGraphLinksLocked internally builds a map[string]struct{} from its
	// variadic ids argument. We build the slice here outside s.mu so the
	// allocation cost is not paid while holding the write lock.
	if syncGraphLinks {
		snapshotIDs := make([]string, 0, len(snapshot))
		for i := range snapshot {
			if id := snapshot[i].ID; id != "" {
				snapshotIDs = append(snapshotIDs, id)
			}
		}
		s.mu.Lock()
		s.syncGraphLinksLocked(snapshotIDs...)
		s.mu.Unlock()
	}

	if s.tmt != nil {
		s.tmt.Rebuild(snapshot)
	}
	if s.entityIndex != nil {
		s.entityIndex.Rebuild(snapshot)
	}
	if s.projIndex != nil {
		s.projIndex.Rebuild(snapshot)
	}
	if s.aliasIndex != nil {
		s.aliasIndex.Rebuild(snapshot)
	}
	if s.semanticGraph != nil {
		s.semanticGraph.Rebuild(snapshot)
		// NewInferenceEngine is pure allocation — no Store lock needed here.
		engine := NewInferenceEngine(s.semanticGraph, nil)
		s.mu.Lock()
		s.inferenceEngine = engine
		s.rebuildThemeLayerLocked()
		s.mu.Unlock()
	} else {
		// rebuildThemeLayerLocked only sets a dirty flag; it is very fast.
		s.mu.Lock()
		s.rebuildThemeLayerLocked()
		s.mu.Unlock()
	}
}

// rebuildThemeLayerLocked keeps the xMemory-style theme layer in sync with the
// authoritative store entries. Caller MUST hold s.mu write lock, or be in Store
// construction before sharing.
//
// rebuildThemeLayerLocked marks the theme layer as needing a rebuild.
// The actual rebuild happens lazily via ThemeManager.EnsureUpToDate()
// when themes are queried (adaptive recall, memory tool, diversity rerank).
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

// SupersedeEntryByID invalidates a fact while preserving history. It stages the
// lifecycle transition through the store batch updater so persistence and all
// derived indexes follow the same path as other governed mutations.
func (s *Store) SupersedeEntryByID(id string, invalidAt time.Time) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("memory_store: not initialized")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false, fmt.Errorf("memory_store: missing entry id")
	}

	s.mu.RLock()
	var updated Entry
	found := false
	for _, entry := range s.entries {
		if entry.ID != id {
			continue
		}
		updated = entry
		found = true
		break
	}
	s.mu.RUnlock()
	if !found {
		return false, fmt.Errorf("memory_store: entry %q not found", id)
	}

	changed := false
	if updated.Status != StatusSuperseded {
		updated.Status = StatusSuperseded
		changed = true
	}
	if !updated.Stale {
		updated.Stale = true
		changed = true
	}
	if updated.InvalidAt == nil {
		t := invalidAt
		if t.IsZero() {
			t = time.Now()
		}
		if !updated.CreatedAt.IsZero() && !t.After(updated.CreatedAt) {
			t = updated.CreatedAt.Add(time.Nanosecond)
		}
		updated.InvalidAt = &t
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := s.updateMetadataEntriesByID([]Entry{updated}); err != nil {
		return false, err
	}
	return true, nil
}

// RecallDynamic is the default flat recall engine for simple/direct queries.
// It combines BM25, vector, semantic graph, entity/tag, recency, importance,
// stability, and graph-expansion signals, then filters dormant/superseded
// entries. It excludes internal summary/identity categories from general recall
// unless a category is explicitly requested.
//
// projectPath is the current workspace/task path. Project-scoped entries and
// derived-memory Boundary.ProjectPath values are compared against it to avoid
// cross-project contamination.
//
// ownerID is optional. When provided and non-empty, only entries with matching
// OwnerID or empty OwnerID (shared) are returned, and Boundary.OwnerID is also
// enforced. In GUI/TUI single-user mode, omit ownerID or pass empty string.
func (s *Store) RecallDynamic(query string, category Category, projectPath string, ownerID ...string) []Entry {
	return s.recallDynamicWithEventContext(query, category, projectPath, lifecycle.EventContext{}, ownerID...)
}

// RecallDynamicForTool is the recall entry point for the memory tool's recall
// action. Unlike RecallDynamic (used by proactive recall in system prompts),
// this method uses a minimal exclusion list when category is empty—only
// session_checkpoint and conversation_summary are excluded. user_fact and
// self_identity are legitimate recall targets when the user asks about
// personal info.
//
// Root cause: the exclusion policy belongs to the caller, not the data layer.
// Proactive recall excludes user_fact because it's already injected via the
// frozen UserFactSummary. Tool recall has no such redundancy, so it should
// not exclude user_fact.
func (s *Store) RecallDynamicForTool(query string, category Category, projectPath string, ownerID ...string) []Entry {
	start := time.Now()
	results := s.recallDynamicCoreWithOptions(query, category, projectPath, recallFilterOptions{
		strictProject:         false,
		excludeWhenNoCategory: toolRecallExcludeCategories,
	}, ownerID...)
	s.recordRecallExperienceEvent("dynamic_tool", query, results, lifecycle.EventContext{})
	s.logRecallIfEnabled("dynamic_tool", query, category, projectPath, ownerID, start, results)
	return results
}

func (s *Store) recallDynamicWithEventContext(query string, category Category, projectPath string, eventContext lifecycle.EventContext, ownerID ...string) []Entry {
	start := time.Now()
	results := s.recallDynamicCoreWithOptions(query, category, projectPath, recallFilterOptions{
		strictProject:         false,
		excludeWhenNoCategory: proactiveRecallExcludeCategories,
	}, ownerID...)
	s.recordRecallExperienceEvent("dynamic", query, results, eventContext)
	s.logRecallIfEnabled("dynamic", query, category, projectPath, ownerID, start, results)
	return results
}

// recallFilterOptions controls the entry filtering behavior of recallDynamicCoreWithOptions.
// This is the mechanism that separates "what to exclude" from "how to recall"—
// the exclusion policy belongs to the caller, not the recall engine.
type recallFilterOptions struct {
	strictProject         bool
	excludeWhenNoCategory []Category // categories to exclude when caller passes category=""
}

// proactiveRecallExcludeCategories is the exclusion list for system prompt
// proactive recall. user_fact is excluded because it's already injected via
// the frozen UserFactSummary snapshot. self_identity is excluded because it's
// injected separately. session_checkpoint and conversation_summary are internal
// bookkeeping not useful for LLM context.
var proactiveRecallExcludeCategories = []Category{
	CategoryUserFact,
	CategorySelfIdentity,
	CategorySessionCheckpoint,
	CategoryConversationSummary,
}

// toolRecallExcludeCategories is the exclusion list for the memory tool's
// recall action. Only internal bookkeeping categories are excluded. user_fact
// and self_identity are legitimate recall targets.
var toolRecallExcludeCategories = []Category{
	CategorySessionCheckpoint,
	CategoryConversationSummary,
}

// recallDynamicCore is the shared implementation for RecallDynamic and RecallDynamicStrict.
// When strictProject=true: ScopeProject entries must have tags matching current projectPath;
// other projects' project_knowledge is excluded; ScopeGlobal + user_fact + preference always allowed.
// When strictProject=false: default behavior (soft project filtering) unchanged.
func (s *Store) recallDynamicCore(query string, category Category, projectPath string, strictProject bool, ownerID ...string) []Entry {
	return s.recallDynamicCoreWithOptions(query, category, projectPath, recallFilterOptions{
		strictProject:         strictProject,
		excludeWhenNoCategory: proactiveRecallExcludeCategories,
	}, ownerID...)
}

// recallDynamicCoreWithOptions is the unified recall engine. The exclusion
// policy is passed in via opts.excludeWhenNoCategory, making the engine
// agnostic to caller-specific filtering needs.
func (s *Store) recallDynamicCoreWithOptions(query string, category Category, projectPath string, opts recallFilterOptions, ownerID ...string) []Entry {
	// Single time reference for the entire recall operation — ensures consistent
	// temporal scoring, demotion, and recency calculations across all stages.
	now := time.Now()

	// Query Expand: extract entities for multi-query BM25 + tokens for tag matching.
	expanded := ExpandQuery(query)

	// Alias expansion: augment entities with known aliases from the AliasIndex.
	// This bridges the write-recall semantic gap by adding alternative terms
	// that were registered during SaveWithContext.
	aliasExpanded := expanded.Entities
	if s.aliasIndex != nil && len(expanded.Entities) > 0 {
		aliases := s.aliasIndex.Expand(expanded.Entities)
		if len(aliases) > 0 {
			aliasExpanded = append(append([]string(nil), expanded.Entities...), aliases...)
		}
	}

	bm25Scores := s.multiQueryBM25(query, aliasExpanded)
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(query))
	semanticScores := map[string]float64{}
	semanticHitDebug := map[string]SemanticSearchHit{}
	if s.semanticGraph != nil {
		temporalMode, asOf := semanticTemporalOptionsFromQuery(query)
		for _, hit := range s.semanticGraph.SearchWithOptions(expanded.Entities, SemanticSearchOptions{
			Now:             now,
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
	// Note: s.inferenceEngine is read without s.mu 闁?same pattern as s.semanticGraph
	// above. Pointer read is safe on 64-bit (atomic at hardware level). Worst case
	// is reading a stale engine (gives slightly outdated results) or nil (no results).
	var derivedFacts []DerivedFact
	if s.inferenceEngine != nil && len(expanded.Entities) > 0 {
		derivedFacts = s.inferenceEngine.Infer(expanded.Entities, InferenceOptions{
			Now:             now,
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
		if opts.strictProject && projectLower != "" {
			// Strict project mode: use recallStrictProjectEntryAllowed for
			// ScopeProject entries, and allow ScopeGlobal + user_fact + preference.
			if !recallDynamicEntryAllowedStrict(e, category, projectLower, filterOwner) {
				continue
			}
		} else {
			if !recallDynamicEntryAllowedWithExclusions(e, category, projectLower, filterOwner, opts.excludeWhenNoCategory) {
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

	// Alias match boost: when alias expansion produced additional entities
	// that match an entry's tag, apply a moderate boost (+2.0). This is
	// below tagExactMatchBoost (+5.0) but above baseline.
	if s.aliasIndex != nil && len(aliasExpanded) > len(expanded.Entities) {
		// Only the alias-derived entities (not the original ones).
		aliasOnly := aliasExpanded[len(expanded.Entities):]
		for i := range candidates {
			boost := tagExactMatchBoost(candidates[i].entry, aliasOnly)
			if boost > 0 {
				// Cap to AliasMatchBoost per entry (below tag boost).
				if boost > AliasMatchBoost {
					boost = AliasMatchBoost
				}
				candidates[i].score += boost
			}
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
	preExpandLen := len(candidates)
	candidates = s.graphExpand(candidates, graphExpandSeeds)

	// Apply tag/alias boost to newly expanded entries — they only have a
	// graph-derived score and missed the initial boost passes.
	if len(candidates) > preExpandLen {
		// Pre-compute alias slice once outside loop.
		var aliasOnly []string
		if s.aliasIndex != nil && len(aliasExpanded) > len(expanded.Entities) {
			aliasOnly = aliasExpanded[len(expanded.Entities):]
		}
		for i := preExpandLen; i < len(candidates); i++ {
			if len(expanded.Entities) > 0 {
				candidates[i].score += tagExactMatchBoost(candidates[i].entry, expanded.Entities)
			}
			if len(aliasOnly) > 0 {
				aliasBoost := tagExactMatchBoost(candidates[i].entry, aliasOnly)
				if aliasBoost > AliasMatchBoost {
					aliasBoost = AliasMatchBoost
				}
				candidates[i].score += aliasBoost
			}
			candidates[i].score += candidates[i].entry.Stability.StabilityBoost()
		}
	}

	// Re-apply the full dynamic visibility contract after graph expansion: graph
	// edges can cross owner, project, or category boundaries that the seed set had
	// already filtered out.
	if opts.strictProject && projectLower != "" {
		candidates = filterRecallDynamicCandidatesStrict(candidates, category, projectLower, filterOwner)
	} else {
		candidates = filterRecallDynamicCandidatesWithExclusions(candidates, category, projectLower, filterOwner, opts.excludeWhenNoCategory)
	}
	if ClassifyComplexity(query, expanded.Entities, nil) != ComplexitySimple && s.themeManager != nil {
		candidates = themeAwareDiversityRerank(candidates, s.themeManager.Themes(), graphExpandSeeds)
	}

	// Temporal demotion: entries marked stale or temporally invalidated are
	// demoted in score so they rank lower but remain discoverable. Applied
	// after graphExpand so expanded entries are also subject to demotion.
	// Implements the Dreaming V3 "stay current over time" principle.
	applyTemporalDemotion(candidates, now)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

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

// recallScoredForPagination runs the full multi-signal scoring pipeline
// (BM25 + Vector + Semantic Graph + Tag matching + Memory Stream Score)
// but returns the complete sorted []recallScored list instead of applying
// entry/token limits. Used by CursorPaginator to cache the full candidate
// set for subsequent page slicing.
func (s *Store) recallScoredForPagination(query string, category Category, projectPath string, ownerID string) []recallScored {
	// Single time reference for the entire operation.
	now := time.Now()

	// Query Expand: extract entities for multi-query BM25 + tokens for tag matching.
	expanded := ExpandQuery(query)

	// Alias expansion: augment entities with known aliases from the AliasIndex.
	// Mirrors recallDynamicCoreWithOptions to bridge the write-recall semantic gap.
	aliasExpanded := expanded.Entities
	if s.aliasIndex != nil && len(expanded.Entities) > 0 {
		aliases := s.aliasIndex.Expand(expanded.Entities)
		if len(aliases) > 0 {
			aliasExpanded = append(append([]string(nil), expanded.Entities...), aliases...)
		}
	}

	bm25Scores := s.multiQueryBM25(query, aliasExpanded)
	vecScores := s.vecIndex.score(s.queryEmbeddingCached(query))
	semanticScores := map[string]float64{}
	if s.semanticGraph != nil {
		temporalMode, asOf := semanticTemporalOptionsFromQuery(query)
		for _, hit := range s.semanticGraph.SearchWithOptions(expanded.Entities, SemanticSearchOptions{
			Now:             now,
			AsOf:            asOf,
			OwnerID:         ownerID,
			ProjectPath:     projectPath,
			RelationHints:   semanticRelationHintsFromQuery(query, expanded),
			SeedWeights:     semanticSeedWeightsFromEntities(expanded.Entities),
			MaxHits:         30,
			MaxVisitedFacts: 500,
			TemporalMode:    temporalMode,
		}) {
			semanticScores[hit.EntryID] = hit.Score
		}
	}

	// Multi-hop inference: derive implicit facts.
	if s.inferenceEngine != nil && len(expanded.Entities) > 0 {
		derivedFacts := s.inferenceEngine.Infer(expanded.Entities, InferenceOptions{
			Now:             now,
			OwnerID:         ownerID,
			ProjectPath:     projectPath,
			MaxDerived:      10,
			MinConfidence:   0.50,
			MaxVisitedFacts: 200,
		})
		for _, df := range derivedFacts {
			for _, sf := range df.SourceFacts {
				if sf.EntryID != "" {
					semanticScores[sf.EntryID] += df.Confidence * 1.5
				}
			}
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	projectLower := semanticNormalizeProjectPath(projectPath)

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
		if !recallDynamicEntryAllowedWithExclusions(e, category, projectLower, ownerID, proactiveRecallExcludeCategories) {
			continue
		}
		b := bm25Scores[e.ID]
		v := 0.0
		if vs, ok := vecScores[e.ID]; ok {
			v = vs
		}
		raw = append(raw, rawCandidate{entry: e, bm25: b, vec: v, sem: semanticScores[e.ID]})
	}

	// Three-way RRF fusion.
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

	// Tag exact match boost.
	if len(expanded.Entities) > 0 {
		for i := range candidates {
			boost := tagExactMatchBoost(candidates[i].entry, expanded.Entities)
			candidates[i].score += boost
		}
	}

	// Alias match boost: when alias expansion produced additional entities
	// that match an entry's tag, apply a moderate boost (+2.0).
	if s.aliasIndex != nil && len(aliasExpanded) > len(expanded.Entities) {
		aliasOnly := aliasExpanded[len(expanded.Entities):]
		for i := range candidates {
			boost := tagExactMatchBoost(candidates[i].entry, aliasOnly)
			if boost > 0 {
				if boost > AliasMatchBoost {
					boost = AliasMatchBoost
				}
				candidates[i].score += boost
			}
		}
	}

	// Stability boost/penalty.
	for i := range candidates {
		candidates[i].score += candidates[i].entry.Stability.StabilityBoost()
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// 1-hop graph expansion.
	preExpandLen := len(candidates)
	candidates = s.graphExpand(candidates, graphExpandSeeds)

	// Apply tag/alias/stability boost to newly expanded entries.
	if len(candidates) > preExpandLen {
		// Pre-compute alias slice once outside loop.
		var aliasOnly []string
		if s.aliasIndex != nil && len(aliasExpanded) > len(expanded.Entities) {
			aliasOnly = aliasExpanded[len(expanded.Entities):]
		}
		for i := preExpandLen; i < len(candidates); i++ {
			if len(expanded.Entities) > 0 {
				candidates[i].score += tagExactMatchBoost(candidates[i].entry, expanded.Entities)
			}
			if len(aliasOnly) > 0 {
				aliasBoost := tagExactMatchBoost(candidates[i].entry, aliasOnly)
				if aliasBoost > AliasMatchBoost {
					aliasBoost = AliasMatchBoost
				}
				candidates[i].score += aliasBoost
			}
			candidates[i].score += candidates[i].entry.Stability.StabilityBoost()
		}
	}

	// Re-apply visibility filters after graph expansion.
	candidates = filterRecallDynamicCandidatesWithExclusions(candidates, category, projectLower, ownerID, proactiveRecallExcludeCategories)

	// Temporal demotion: stale/invalidated entries rank lower.
	applyTemporalDemotion(candidates, now)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return candidates
}

// recallScoredForScroll executes the full multi-signal scoring pipeline and
// returns scored candidates for scroll session caching. Uses the tool recall
// exclusion list (less restrictive than proactive recall). Accepts variadic
// ownerID to match RecallDynamic's signature.
func (s *Store) recallScoredForScroll(query string, category Category, projectPath string, ownerID ...string) []recallScored {
	filterOwner := firstOwnerID(ownerID...)
	return s.recallScoredForPagination(query, category, projectPath, filterOwner)
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
// results after strict filtering 闁?the second pass uses a broader query to
// backfill the budget.
func (s *Store) RecallDynamicStrict(query string, category Category, projectPath string, ownerID ...string) []Entry {
	return s.recallDynamicStrictWithEventContext(query, category, projectPath, lifecycle.EventContext{}, ownerID...)
}

func (s *Store) recallDynamicStrictWithEventContext(query string, category Category, projectPath string, eventContext lifecycle.EventContext, ownerID ...string) []Entry {
	start := time.Now()
	results := s.recallDynamicCore(query, category, projectPath, true, ownerID...)
	if projectPath == "" {
		s.recordRecallExperienceEvent("dynamic_strict", query, results, eventContext)
		s.logRecallIfEnabled("dynamic_strict", query, category, projectPath, ownerID, start, results)
		return results
	}
	projectLower := semanticNormalizeProjectPath(projectPath)
	if projectLower == "" {
		s.recordRecallExperienceEvent("dynamic_strict", query, results, eventContext)
		s.logRecallIfEnabled("dynamic_strict", query, category, projectPath, ownerID, start, results)
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
	s.recordRecallExperienceEvent("dynamic_strict", query, results, eventContext)
	s.logRecallIfEnabled("dynamic_strict", query, category, projectPath, ownerID, start, results)
	return results
}

// logRecallIfEnabled is a shared helper that logs recall operations when the
// memory recall log is enabled. Avoids duplicating the ExpandQuery + LogRecallOperation
// pattern at every return site.
func (s *Store) logRecallIfEnabled(op, query string, category Category, projectPath string, ownerID []string, start time.Time, results []Entry) {
	if !IsMemoryRecallLogEnabled() {
		return
	}
	expanded := ExpandQuery(query)
	totalEntries := s.ActiveCount()
	LogRecallOperation(op, query, category, projectPath, firstOwnerID(ownerID...), time.Since(start), results, totalEntries, &expanded)
}

func (s *Store) recordRecallExperienceEvent(mode string, query string, entries []Entry, eventContext ...lifecycle.EventContext) {
	s.recordMemoryExperienceEvent(lifecycle.EventExperienceRetrieved, mode, query, entries, 0, firstEventContext(eventContext))
}

func (s *Store) recordInjectedExperienceEvent(mode string, query string, entries []Entry, tokenCost int, eventContext ...lifecycle.EventContext) {
	s.recordMemoryExperienceEvent(lifecycle.EventExperienceInjected, mode, query, entries, tokenCost, firstEventContext(eventContext))
}

func (s *Store) recordCandidateExperienceEvent(eventType lifecycle.EventType, mode string, query string, candidates []lifecycle.Candidate, tokenCost int, eventContext lifecycle.EventContext) {
	if s == nil {
		return
	}
	s.mu.RLock()
	sink := s.eventSink
	s.mu.RUnlock()
	if sink == nil {
		return
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Entry.ID != "" {
			ids = append(ids, candidate.Entry.ID)
		}
	}
	sink.RecordExperienceEvent(eventContext.Apply(lifecycle.Event{
		EventType: eventType,
		EntryIDs:  ids,
		Query:     query,
		Reason:    mode,
		TokenCost: tokenCost,
		Outcome:   fmt.Sprintf("entries:%d", len(candidates)),
	}))
}

func (s *Store) recordRetrievalDecisionEvent(decision lifecycle.RetrievalDecision, eventContext lifecycle.EventContext) {
	if s == nil {
		return
	}
	s.mu.RLock()
	sink := s.eventSink
	s.mu.RUnlock()
	if sink == nil {
		return
	}
	outcome := "skip"
	if decision.ShouldRetrieve {
		outcome = "retrieve"
	}
	sink.RecordExperienceEvent(eventContext.Apply(lifecycle.Event{
		EventType: lifecycle.EventRetrievalDecided,
		Query:     decision.Query,
		Reason:    decision.Reason,
		TokenCost: decision.Budget.MaxTokens,
		Outcome:   outcome,
	}))
}

func firstEventContext(values []lifecycle.EventContext) lifecycle.EventContext {
	if len(values) == 0 {
		return lifecycle.EventContext{}
	}
	return values[0]
}

func (s *Store) recordMemoryExperienceEvent(eventType lifecycle.EventType, mode string, query string, entries []Entry, tokenCost int, eventContext lifecycle.EventContext) {
	if s == nil {
		return
	}
	s.mu.RLock()
	sink := s.eventSink
	s.mu.RUnlock()
	if sink == nil {
		return
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.ID != "" {
			ids = append(ids, entry.ID)
		}
	}
	sink.RecordExperienceEvent(eventContext.Apply(lifecycle.Event{
		EventType: eventType,
		EntryIDs:  ids,
		Query:     query,
		Reason:    mode,
		TokenCost: tokenCost,
		Outcome:   fmt.Sprintf("entries:%d", len(entries)),
	}))
}

// recallStrictProjectEntryAllowed implements the strict project filtering rule:
//   - Scope != ScopeProject 闁?always allowed (global knowledge, user_fact, preference, self_identity)
//   - Scope == ScopeProject AND tags contain projectPath 闁?allowed (belongs to this project)
//   - Scope == ScopeProject AND tags do NOT contain projectPath 闁?excluded (belongs to another project)
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

// SearchByMode is the low-level search selector used by legacy/diagnostic
// callers. SearchDirect is exact ID lookup, SearchKeywordOnly is BM25-only, and
// SearchHybrid delegates to RecallDynamic. New user-facing recall should prefer
// RecallByMode so empty mode can default to auto and adaptive/lightmem debug
// plans remain available. Pass ownerID for tenant isolation.
func (s *Store) SearchByMode(query string, mode SearchMode, category Category, projectPath string, limit int, ownerID ...string) []Entry {
	switch mode {
	case SearchDirect:
		return s.SearchDirectByIDForProject(query, category, projectPath, ownerID...)
	case SearchKeywordOnly:
		return s.SearchKeywordForProject(query, category, projectPath, limit, ownerID...)
	default:
		return limitSearchResults(s.RecallDynamic(query, category, projectPath, ownerID...), limit)
	}
}

func recallDynamicEntryAllowed(e Entry, category Category, projectLower, filterOwner string) bool {
	return recallDynamicEntryAllowedWithExclusions(e, category, projectLower, filterOwner, proactiveRecallExcludeCategories)
}

// recallDynamicEntryAllowedWithExclusions is the single parameterized entry
// filter for recall. The exclusion list determines which categories are
// filtered out when the caller does not specify a category. This is the
// mechanism that separates "what to exclude" (caller policy) from "how to
// filter" (data layer logic).
func recallDynamicEntryAllowedWithExclusions(e Entry, category Category, projectLower, filterOwner string, excludeWhenNoCategory []Category) bool {
	if !e.IsActive() || !recallProjectEntryAllowed(e, projectLower) {
		return false
	}
	if !recallBoundaryAllowed(e, projectLower, filterOwner) {
		return false
	}
	if filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner {
		return false
	}
	if category != "" {
		return recallCategoryMatches(e.Category, category)
	}
	// No category specified: apply caller's exclusion policy.
	for _, excluded := range excludeWhenNoCategory {
		if e.Category == excluded {
			return false
		}
	}
	return true
}

func filterRecallDynamicCandidates(candidates []recallScored, category Category, projectLower, filterOwner string) []recallScored {
	return filterRecallDynamicCandidatesWithExclusions(candidates, category, projectLower, filterOwner, proactiveRecallExcludeCategories)
}

func filterRecallDynamicCandidatesWithExclusions(candidates []recallScored, category Category, projectLower, filterOwner string, excludeWhenNoCategory []Category) []recallScored {
	if len(candidates) == 0 {
		return candidates
	}
	filtered := candidates[:0]
	for _, c := range candidates {
		if recallDynamicEntryAllowedWithExclusions(c.entry, category, projectLower, filterOwner, excludeWhenNoCategory) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// recallDynamicEntryAllowedStrict implements strict project filtering for Project Tab:
//   - ScopeProject + tags match current projectPath 闁?allowed
//   - ScopeProject + tags don't match current projectPath 闁?excluded (other projects' knowledge)
//   - ScopeGlobal 闁?allowed (archived experience, universal knowledge)
//   - user_fact / preference 闁?allowed (user preferences always available)
//   - Other projects' project_knowledge 闁?excluded
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
	if !recallBoundaryAllowed(e, projectLower, filterOwner) {
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
	// If entry has project tags but none match 闁?exclude (belongs to another project).
	// If entry has no project tags 闁?allow (generic knowledge).
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

func recallBoundaryAllowed(e Entry, projectLower, filterOwner string) bool {
	if e.Boundary == nil {
		return true
	}
	boundary := e.Boundary
	if filterOwner != "" && boundary.OwnerID != "" && boundary.OwnerID != filterOwner {
		return false
	}
	if projectLower != "" && boundary.ProjectPath != "" {
		boundaryProject := semanticNormalizeProjectPath(boundary.ProjectPath)
		if boundaryProject != "" && !semanticProjectPathMatches(boundaryProject, projectLower) {
			return false
		}
	}
	return true
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

func recallDirectEntryAllowed(e Entry, category Category, projectLower, filterOwner string) bool {
	if !e.IsActive() || !recallProjectEntryAllowed(e, projectLower) {
		return false
	}
	if !recallBoundaryAllowed(e, projectLower, filterOwner) {
		return false
	}
	if filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner {
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
func (s *Store) SearchDirectByIDForProject(id string, category Category, projectPath string, ownerID ...string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projectLower := semanticNormalizeProjectPath(projectPath)
	filterOwner := firstOwnerID(ownerID...)
	for _, e := range s.entries {
		if e.ID == id && recallDirectEntryAllowed(e, category, projectLower, filterOwner) {
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
func (s *Store) SearchKeywordForProject(query string, category Category, projectPath string, limit int, ownerID ...string) []Entry {
	if limit <= 0 {
		limit = 15
	}
	scores := s.bm25.score(query)
	projectLower := semanticNormalizeProjectPath(projectPath)
	filterOwner := firstOwnerID(ownerID...)

	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		entry Entry
		score float64
	}
	var candidates []scored
	for _, e := range s.entries {
		if !recallDynamicEntryAllowed(e, category, projectLower, filterOwner) {
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
	s.mu.RLock()

	n := len(s.entries)
	if n < 2 {
		s.mu.RUnlock()
		return 0
	}

	updates := make([]Entry, 0)
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
				updated := s.entries[i]
				updated.Stale = true
				updates = append(updates, updated)
			}
			break
		}
	}
	s.mu.RUnlock()

	if len(updates) > 0 {
		if err := s.UpdateEntriesByID(updates); err != nil {
			log.Printf("[memory_dream] persist stale flags: %v", err)
			return 0
		}
	}
	return len(updates)
}

// ClearStale removes the stale flag from all entries. Returns count cleared.
func (s *Store) ClearStale() int {
	s.mu.RLock()
	updates := make([]Entry, 0)
	for i := range s.entries {
		if s.entries[i].Stale {
			updated := s.entries[i]
			updated.Stale = false
			updates = append(updates, updated)
		}
	}
	s.mu.RUnlock()
	if len(updates) > 0 {
		if err := s.UpdateEntriesByID(updates); err != nil {
			log.Printf("[memory_dream] persist cleared stale flags: %v", err)
			return 0
		}
	}
	return len(updates)
}

// detectTemporallyExpired marks active entries as stale when their temporal
// validity has passed. This catches time-bound memories (plans, events, projects)
// that become outdated without a newer contradicting entry existing.
//
// Two detection rules:
//   - Boundary.Until expired: entry explicitly declares an end time.
//   - ValidAt + staleness window: entry has a real-world validity timestamp
//     older than 30 days and has not been accessed/updated in 14 days.
//
// Pinned, protected-category, and already-stale entries are skipped.
func (s *Store) detectTemporallyExpired() int {
	now := time.Now()
	const (
		validAtStalenessWindow = 30 * 24 * time.Hour // 30 days since ValidAt
		recentActivityWindow   = 14 * 24 * time.Hour // 14 days of inactivity
	)

	s.mu.RLock()
	var updates []Entry
	for i := range s.entries {
		e := &s.entries[i]
		if !e.IsActive() || e.Stale || e.Pinned || e.Category.IsProtected() {
			continue
		}

		shouldMark := false

		// Rule 1: Boundary.Until explicitly expired.
		if e.Boundary != nil && e.Boundary.Until != nil && !e.Boundary.Until.IsZero() && e.Boundary.Until.Before(now) {
			shouldMark = true
		}

		// Rule 2: ValidAt is older than 30 days and entry has not been
		// updated or accessed recently (14 days). This avoids marking
		// actively-referenced entries that happen to have an old ValidAt.
		//
		// Skip stable-fact categories (user_fact, preference, instruction):
		// these represent facts true until explicitly contradicted, not
		// time-bound events. Their ValidAt means "when stated", not "expires after".
		if !shouldMark && e.ValidAt != nil && !isStableFactCategory(e.Category) && now.Sub(*e.ValidAt) > validAtStalenessWindow {
			lastActivity := e.UpdatedAt
			if e.CreatedAt.After(lastActivity) {
				lastActivity = e.CreatedAt
			}
			if now.Sub(lastActivity) > recentActivityWindow {
				shouldMark = true
			}
		}

		if shouldMark {
			updated := *e
			updated.Stale = true
			updates = append(updates, updated)
		}
	}
	s.mu.RUnlock()

	if len(updates) > 0 {
		if err := s.UpdateEntriesByID(updates); err != nil {
			log.Printf("[memory_dream] persist temporal expired flags: %v", err)
			return 0
		}
	}
	return len(updates)
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
// 1. Stale detection (tag-overlap based)
// 1b. Temporal expiry detection (time-based, Dreaming V3 "stay current")
// 2. Auto-link discovery for unlinked but related entries
// 3. Content hash backfill for entries missing hashes
// ---------------------------------------------------------------------------

// DreamCycle performs a background self-healing pass over all entries.
// Safe to call from the Compressor's periodic loop.
func (s *Store) DreamCycle() *DreamCycleResult {
	result := &DreamCycleResult{}

	// Phase 1: Stale detection (tag-overlap based).
	result.StaleDetected = s.DetectStale()

	// Phase 1b: Temporal expiry detection — marks entries whose ValidAt or
	// Boundary.Until has passed as stale. Implements the Dreaming V3
	// "stay current over time" principle without requiring a new contradicting
	// entry to trigger invalidation.
	result.TemporalExpired = s.detectTemporallyExpired()

	// Phase 2: Auto-link discovery; find high-BM25 pairs that aren't linked.
	result.LinksDiscovered = s.discoverMissingLinks()

	// Phase 3: Content hash backfill.
	result.HashesBackfilled = s.backfillContentHashes()

	// Phase 4: Tag backfill; enrich old entries that have poor tags.
	result.TagsBackfilled = s.backfillTags()

	if result.StaleDetected > 0 || result.TemporalExpired > 0 || result.LinksDiscovered > 0 || result.HashesBackfilled > 0 || result.TagsBackfilled > 0 {
		log.Printf("[memory_dream] stale=%d temporal_expired=%d links=%d hashes=%d tags=%d",
			result.StaleDetected, result.TemporalExpired, result.LinksDiscovered, result.HashesBackfilled, result.TagsBackfilled)
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
		var updates []Entry
		if s.syncGraphLinksLocked() {
			for _, entry := range s.entries {
				if len(entry.RelatedIDs) > 0 || len(entry.RelatedEdges) > 0 {
					updates = append(updates, entry)
				}
			}
		}
		s.mu.Unlock()
		if len(updates) > 0 {
			if err := s.UpdateEntriesByID(updates); err != nil {
				log.Printf("[memory_dream] persist discovered graph links: %v", err)
			}
		}
	}
	return created
}

// backfillContentHashes computes SHA-256 hashes for entries missing them.
func (s *Store) backfillContentHashes() int {
	s.mu.RLock()
	updates := make([]Entry, 0)
	for i := range s.entries {
		if s.entries[i].ContentHash == "" && s.entries[i].Content != "" {
			updated := s.entries[i]
			updated.ContentHash = computeContentHash(updated.Content)
			updates = append(updates, updated)
		}
	}
	s.mu.RUnlock()
	if len(updates) > 0 {
		if err := s.UpdateEntriesByID(updates); err != nil {
			log.Printf("[memory_dream] persist content hash backfill: %v", err)
			return 0
		}
	}
	return len(updates)
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

	// Stage enrichments from current entries, matching by ID.
	s.mu.RLock()
	updates := make([]Entry, 0, len(enrichByID))
	for i := range s.entries {
		if newTags, ok := enrichByID[s.entries[i].ID]; ok {
			updated := s.entries[i]
			updated.Tags = mergeTags(updated.Tags, newTags)
			updates = append(updates, updated)
		}
	}
	s.mu.RUnlock()
	if len(updates) > 0 {
		if err := s.UpdateEntriesByID(updates); err != nil {
			log.Printf("[memory_dream] persist tag backfill: %v", err)
			return 0
		}
	}
	return len(updates)
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

		// Wait for any in-flight async index rebuild goroutine to complete.
		// replaceEntriesAndRebuildAsync launches a goroutine that accesses
		// s.entries and calls s.mu.Lock internally. We must drain it before
		// closing the backend, otherwise the goroutine's s.mu.Lock acquire or
		// s.entries read would race with the backend close.
		//
		// We cannot use WaitRebuild() here because it short-circuits on
		// s.stopCh (already closed above). Instead we wait directly on the
		// done channel without the stopCh escape.
		s.mu.RLock()
		rebuildDone := s.lastRebuildDone
		s.mu.RUnlock()
		if rebuildDone != nil {
			<-rebuildDone
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

// ReconcileArchiveDuplicates removes cold archive copies for IDs that are live
// in active memory. Archive transitions are intentionally archive-first; this
// repair step closes any duplicate left by a crash or backend failure.
func (s *Store) ReconcileArchiveDuplicates() (int, error) {
	if s == nil || s.archive == nil {
		return 0, nil
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.entries))
	seen := make(map[string]struct{}, len(s.entries))
	for _, entry := range s.entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	if len(ids) == 0 {
		return 0, nil
	}
	removed, err := s.archive.RemoveIDsDurable(ids)
	if err != nil {
		return 0, err
	}
	return len(removed), nil
}

// ArchiveActiveEntries moves active entries to cold storage and tombstones them
// from the active backend in one store-level mutation. The active tombstones are
// emitted through the same sync stream as other deletes.
func (s *Store) ArchiveActiveEntries(entries []Entry) error {
	if s == nil || len(entries) == 0 {
		return nil
	}
	if s.archive == nil {
		return fmt.Errorf("memory_store: archive not initialized")
	}
	ids := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return fmt.Errorf("memory_store: archived entry id must not be empty")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if err := s.archive.AddDurable(entries...); err != nil {
		return fmt.Errorf("memory_store: archive active entries: %w", err)
	}
	if err := s.UpdateEntriesAndDeleteIDs(nil, ids); err != nil {
		return err
	}
	return nil
}

// ReviveArchivedEntries removes entries from cold storage and restores them to
// active memory through the normal ID-addressed batch path.
func (s *Store) ReviveArchivedEntries(ids []string) ([]Entry, error) {
	if s == nil || len(ids) == 0 {
		return nil, nil
	}
	if s.archive == nil {
		return nil, fmt.Errorf("memory_store: archive not initialized")
	}
	now := time.Now()
	seen := make(map[string]struct{}, len(ids))
	removeIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		removeIDs = append(removeIDs, id)
	}
	revived := s.archive.EntriesByIDs(removeIDs)
	for i := range revived {
		revived[i].UpdatedAt = now
		normalizeEntryTimestamp(&revived[i], now)
		revived[i].AccessCount = 1
	}
	if len(revived) == 0 {
		return nil, nil
	}
	if err := s.upsertEntriesByID(revived, false, true); err != nil {
		return nil, err
	}
	removed, err := s.archive.RemoveIDsDurable(removeIDs)
	if err != nil {
		return revived, fmt.Errorf("memory_store: remove revived archive entries: %w", err)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	return revived, nil
}

// RestoreFromArchive removes an entry from the archive and adds it back to
// active memory with UpdatedAt=now and AccessCount=1. If active memory is
// full, evictLRU runs first (which archives the lowest priority entry).
func (s *Store) RestoreFromArchive(id string) error {
	revived, err := s.ReviveArchivedEntries([]string{id})
	if err != nil {
		return fmt.Errorf("memory_store: %w", err)
	}
	if len(revived) == 0 {
		return fmt.Errorf("memory_store: archive_store: entry %q not found", id)
	}
	s.mu.Lock()
	s.evictLRU()
	s.mu.Unlock()
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
	s.replaceEntriesAndRebuildLocked(entries, false)
}

// RestoreEntriesSnapshot replaces active memory with an explicit backup
// snapshot. For sync-capable backends, restored rows and tombstones for rows
// absent from the snapshot are committed in one backend mutation before the
// in-memory view changes.
func (s *Store) RestoreEntriesSnapshot(entries []Entry) error {
	if s == nil {
		return fmt.Errorf("memory_store: not initialized")
	}
	restored := normalizeRestoredSnapshot(entries)
	if s.backend != nil && s.backend.SupportsSync() {
		batchBackend, ok := s.backend.(BatchMutationStorageBackend)
		if !ok {
			return fmt.Errorf("memory_store: backend does not support atomic snapshot restore")
		}
		s.mu.RLock()
		deleteIDs := restoreSnapshotDeleteIDsLocked(s.entries, restored)
		s.mu.RUnlock()
		ptrs := make([]*Entry, len(restored))
		for i := range restored {
			ptrs[i] = &restored[i]
		}
		if err := batchBackend.UpdateEntriesAndDeleteIDs(ptrs, deleteIDs); err != nil {
			return fmt.Errorf("memory_store: persist restored snapshot: %w", err)
		}
		s.mu.Lock()
		s.replaceEntriesAndRebuildLocked(restored, false)
		s.dirty = false
		if s.sync != nil {
			if maxV, err := s.backend.MaxVersion(); err == nil {
				s.sync.lastVersion = maxV
			}
		}
		s.mu.Unlock()
		return nil
	}

	s.mu.Lock()
	previous := append([]Entry(nil), s.entries...)
	s.replaceEntriesAndRebuildLocked(restored, false)
	s.markDirtyLocked()
	s.mu.Unlock()
	if err := s.flush(); err != nil {
		s.mu.Lock()
		s.replaceEntriesAndRebuildLocked(previous, false)
		s.markDirtyLocked()
		s.mu.Unlock()
		return fmt.Errorf("memory_store: flush restored snapshot: %w", err)
	}
	return nil
}

func (s *Store) replaceEntriesAndRebuildLocked(entries []Entry, syncGraphLinks bool) {
	s.entries = entries
	s.rebuildDerivedIndexesLocked(syncGraphLinks)
}

// replaceEntriesAndRebuildAsync is the non-blocking variant used by syncOnce.
// It updates s.entries immediately (under s.mu, held by the caller), then
// rebuilds every derived index in a background goroutine. This reduces the
// s.mu write-lock hold time from O(rebuild) to O(n copy), eliminating the
// 5-8 second contention window that starved concurrent RecallDynamic/Save.
//
// Only one rebuild goroutine runs at a time: if a previous rebuild is still
// in progress when a new sync batch arrives, the new goroutine waits for the
// old one to finish before starting its own rebuild. This prevents concurrent
// rebuilds from racing on syncGraphLinksLocked.
func (s *Store) replaceEntriesAndRebuildAsync(entries []Entry, syncGraphLinks bool) {
	s.entries = entries
	snapshot := append([]Entry(nil), entries...)

	// Capture the previous done channel (may be nil on first call or already
	// closed from a previous rebuild). The new goroutine waits on it so that
	// at most one rebuild is active at any time, preventing concurrent calls
	// to syncGraphLinksLocked from corrupting s.entries.
	prevDone := s.lastRebuildDone
	done := make(chan struct{})
	s.lastRebuildDone = done
	go func() {
		// Drain the previous rebuild first. Under a 3-second sync interval
		// this path is only taken when a rebuild takes longer than the interval.
		if prevDone != nil {
			select {
			case <-prevDone:
			case <-s.stopCh:
				close(done)
				return
			}
		}
		s.rebuildDerivedIndexesOutsideLock(snapshot, syncGraphLinks)
		close(done)
	}()
}

// WaitRebuild blocks until the most recent background index rebuild triggered
// by replaceEntriesAndRebuildAsync completes. Used in tests and by code paths
// that need consistent index state immediately after a sync operation.
func (s *Store) WaitRebuild() {
	s.mu.RLock()
	done := s.lastRebuildDone
	s.mu.RUnlock()
	if done != nil {
		select {
		case <-done:
		case <-s.stopCh:
		}
	}
}

func normalizeRestoredSnapshot(entries []Entry) []Entry {
	restored := make([]Entry, 0, len(entries))
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" {
			entry.ID = generateID()
		}
		if idx, ok := seen[entry.ID]; ok {
			restored[idx] = entry
			continue
		}
		seen[entry.ID] = len(restored)
		restored = append(restored, entry)
	}
	normalizeEntryTimestamps(restored)
	return restored
}

func restoreSnapshotDeleteIDsLocked(current []Entry, restored []Entry) []string {
	restoredIDs := make(map[string]struct{}, len(restored))
	for _, entry := range restored {
		restoredIDs[entry.ID] = struct{}{}
	}
	deleteIDs := make([]string, 0)
	for _, entry := range current {
		if entry.ID == "" {
			continue
		}
		if _, ok := restoredIDs[entry.ID]; ok {
			continue
		}
		deleteIDs = append(deleteIDs, entry.ID)
	}
	return deleteIDs
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

// insertPreparedEntryLocked appends an already sanitized entry and refreshes all indexes.
// Caller MUST hold s.mu.Lock. The caller owns identity/dedup policy; this
// helper owns the common insert mechanics.
func (s *Store) insertPreparedEntryLocked(entry Entry, hash string, now time.Time, enqueueSemanticDedup bool) error {
	if entry.ID == "" {
		entry.ID = generateID()
	}
	if enqueueSemanticDedup && len(entry.Embedding) > 0 {
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
	s.autoLink(entry)
	s.evictLRU()
	s.markDirtyLocked()
	if s.findEntryIndexByIDLocked(entry.ID) >= 0 {
		if s.projIndex != nil {
			s.projIndex.IndexEntry(&entry)
		}
		if s.entityIndex != nil {
			s.entityIndex.IndexEntry(&entry)
		}
		if s.semanticGraph != nil {
			s.semanticGraph.IndexEntry(&entry)
		}
	}
	s.rebuildThemeLayerLocked()
	return nil
}

func (s *Store) markDirtyLocked() {
	s.dirty = true
	s.signalSave()
}

func (s *Store) persistInsertedEntryLocked(entry *Entry) error {
	if s.backend != nil {
		return s.backend.SaveEntry(entry)
	}
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

// WarmQueryEmbedding asynchronously pre-computes and caches the embedding for
// the given query text. This is a fire-and-forget operation — the result is
// stored in the query embedding cache so that subsequent calls to
// queryEmbeddingCached (via RecallDynamic/proactive recall) get a cache hit
// instead of blocking on model inference.
//
// This eliminates the cold-start embedding latency from the proactive recall
// critical path: the host calls WarmQueryEmbedding as soon as the user message
// arrives, overlapping embedding inference with other prompt-building work.
// When proactive recall runs ~500ms later, the cache is warm.
func (s *Store) WarmQueryEmbedding(query string) {
	if s == nil {
		return
	}
	go s.queryEmbeddingCached(query)
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

	updates := make([]Entry, 0, len(todo))
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

		s.mu.RLock()
		if s.embedderGen != gen {
			s.mu.RUnlock()
			return
		}
		for i := range s.entries {
			if s.entries[i].ID == p.id && len(s.entries[i].Embedding) == 0 {
				updated := s.entries[i]
				updated.Embedding = append([]float32(nil), embVec...)
				updates = append(updates, updated)
				break
			}
		}
		s.mu.RUnlock()
	}

	if len(updates) > 0 {
		if err := s.UpdateEntriesByID(updates); err != nil {
			log.Printf("[memory_embed] persist embedding backfill: %v", err)
		}
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
func (s *Store) OnlineExtractor() *OnlineExtractor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.onlineExtractor
}

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
			if !recallDirectEntryAllowed(e, category, projectLower, ownerID) {
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
	return s.setPinnedByID(id, true)
}

// UnpinEntry sets Pinned=false for the entry with the given ID.
func (s *Store) UnpinEntry(id string) error {
	return s.setPinnedByID(id, false)
}

func (s *Store) setPinnedByID(id string, pinned bool) error {
	if s == nil {
		return fmt.Errorf("memory_store: not initialized")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("memory_store: missing entry id")
	}
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
	s.mu.RUnlock()
	if !found {
		return fmt.Errorf("entry %q not found", id)
	}
	if updated.Pinned == pinned {
		return nil
	}
	updated.Pinned = pinned
	return s.updateMetadataEntriesByID([]Entry{updated})
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

	if len(evicted) == 0 {
		return
	}
	if s.archive != nil {
		if err := s.archive.AddDurable(evicted...); err != nil {
			log.Printf("[memory_store] WARNING: failed to durably archive evicted entries: %v", err)
			return
		}
	}
	deleteIDs := make([]string, 0, len(evicted))
	for _, entry := range evicted {
		deleteIDs = append(deleteIDs, entry.ID)
	}
	if batchBackend, ok := s.backend.(BatchMutationStorageBackend); ok {
		if err := batchBackend.UpdateEntriesAndDeleteIDs(nil, deleteIDs); err != nil {
			log.Printf("[memory_store] WARNING: failed to tombstone evicted entries: %v", err)
			return
		}
	} else if s.backend != nil {
		log.Printf("[memory_store] WARNING: backend does not support batch eviction tombstones")
		return
	}

	s.replaceEntriesAndRebuildLocked(kept, false)
	if s.backend == nil {
		s.markDirtyLocked()
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
			s.replaceEntriesAndRebuildLocked(entries, false)
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
		s.replaceEntriesAndRebuildLocked(make([]Entry, 0), false)
		return nil
	}
	s.replaceEntriesAndRebuildLocked(entries, false)

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
