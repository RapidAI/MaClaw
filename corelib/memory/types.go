package memory

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Category represents the category of a memory entry.
type Category string

const (
	CategorySelfIdentity        Category = "self_identity"
	CategoryUserFact            Category = "user_fact"
	CategoryPreference          Category = "preference"
	CategoryProjectKnowledge    Category = "project_knowledge"
	CategoryInstruction         Category = "instruction"
	CategoryConversationSummary Category = "conversation_summary"
	CategorySessionCheckpoint   Category = "session_checkpoint"
	CategoryTaskArtifact        Category = "task_artifact" // workflow phase output summaries (requirements, design, task list)
	CategoryProfile             Category = "profile"

	// Claude-style four-type taxonomy (inspired by Claude Code memdir).
	// These map to the original categories but provide a cleaner semantic model:
	//   user      - user role, goals, knowledge (maps to user_fact)
	//   feedback  - corrections and confirmations on approach (maps to instruction)
	//   project   - non-derivable project context, decisions, deadlines (maps to project_knowledge)
	//   reference - pointers to external systems (maps to project_knowledge)
	CategoryUser      Category = "user"
	CategoryFeedback  Category = "feedback"
	CategoryProject   Category = "project"
	CategoryReference Category = "reference"
)

// ClaudeStyleCategories returns the four Claude-style category constants.
func ClaudeStyleCategories() []Category {
	return []Category{CategoryUser, CategoryFeedback, CategoryProject, CategoryReference}
}

// MapToCanonical maps a Claude-style category to the canonical internal
// category used by scoring, scope inference, and protection checks.
// Legacy categories pass through unchanged.
func MapToCanonical(c Category) Category {
	switch c {
	case CategoryUser:
		return CategoryUserFact
	case CategoryFeedback:
		return CategoryInstruction
	case CategoryProject, CategoryReference:
		return CategoryProjectKnowledge
	default:
		return c
	}
}

// IsClaudeStyle returns true if the category is one of the four Claude-style types.
func (c Category) IsClaudeStyle() bool {
	switch c {
	case CategoryUser, CategoryFeedback, CategoryProject, CategoryReference:
		return true
	}
	return false
}

// IsProtected returns true for categories that must never be evicted or compressed.
func (c Category) IsProtected() bool {
	return c == CategorySelfIdentity
}

// Scope controls cross-project visibility of a memory entry.
type Scope string

const (
	ScopeGlobal  Scope = "global"  // visible in all projects
	ScopeProject Scope = "project" // visible only when project path matches
)

// Status tracks the lifecycle state of a memory entry.
type Status string

const (
	StatusActive     Status = ""           // default - participates in recall
	StatusSuperseded Status = "superseded" // replaced by a newer conflicting entry
	StatusDormant    Status = "dormant"    // forgotten - below strength threshold
)

// InferScope returns the default scope for a given category.
func InferScope(c Category) Scope {
	// Map Claude-style categories to canonical for scope inference.
	canonical := MapToCanonical(c)
	switch canonical {
	case CategorySelfIdentity, CategoryUserFact, CategoryPreference, CategoryInstruction:
		return ScopeGlobal
	default:
		return ScopeProject
	}
}

// MemoryTier classifies categories into the MemGPT-style hierarchy.
type MemoryTier int

const (
	TierSemantic MemoryTier = iota // abstract knowledge (user_fact, preference, instruction, self_identity)
	TierEpisodic                   // event records (conversation_summary, session_checkpoint)
)

// Tier returns the memory tier for a category.
func (c Category) Tier() MemoryTier {
	// Map Claude-style categories to canonical for tier classification.
	canonical := MapToCanonical(c)
	switch canonical {
	case CategoryConversationSummary, CategorySessionCheckpoint:
		return TierEpisodic
	default:
		return TierSemantic
	}
}

// LinkType describes the semantic relationship between two memory entries.
type LinkType string

const (
	LinkRelated     LinkType = ""             // default - generic relatedness
	LinkReferences  LinkType = "references"   // A references B
	LinkSupersedes  LinkType = "supersedes"   // A supersedes B
	LinkDerivedFrom LinkType = "derived_from" // A was derived from B
	LinkConflicts   LinkType = "conflicts"    // A conflicts with B
)

// RelatedEdge is the persistent representation of a memory graph edge.
// RelatedIDs is kept for backward compatibility and quick legacy inspection;
// RelatedEdges preserves the relationship semantics needed for graph-aware recall.
type RelatedEdge struct {
	ID        string    `json:"id"`
	Strength  float64   `json:"strength,omitempty"`
	LinkType  LinkType  `json:"link_type,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// VersionSnapshot records a previous version of an entry's content.
type VersionSnapshot struct {
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// TemporalLevel represents the hierarchy level in the temporal memory tree.
type TemporalLevel int

const (
	LevelNone TemporalLevel = iota
	LevelSegment
	LevelSession
	LevelDay
	LevelWeek
	LevelProfile
)

func (l TemporalLevel) String() string {
	switch l {
	case LevelSegment:
		return "segment"
	case LevelSession:
		return "session"
	case LevelDay:
		return "day"
	case LevelWeek:
		return "week"
	case LevelProfile:
		return "profile"
	default:
		return "none"
	}
}

// TimeInterval describes a closed time range for consolidated memories.
type TimeInterval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (ti TimeInterval) Contains(other TimeInterval) bool {
	return !other.Start.Before(ti.Start) && !other.End.After(ti.End)
}

func (ti TimeInterval) Overlaps(other TimeInterval) bool {
	return !ti.End.Before(other.Start) && !other.End.Before(ti.Start)
}

// QueryComplexity classifies a user query for hierarchical recall depth.
type QueryComplexity int

const (
	ComplexitySimple  QueryComplexity = iota // factual lookup -> L1-L3
	ComplexityHybrid                         // moderate reasoning -> L1-L4
	ComplexityComplex                        // deep analysis -> L1-L5
)

func (c QueryComplexity) String() string {
	switch c {
	case ComplexitySimple:
		return "simple"
	case ComplexityHybrid:
		return "hybrid"
	case ComplexityComplex:
		return "complex"
	default:
		return "unknown"
	}
}

// RecallLevels returns which temporal levels should be searched for this complexity.
func (c QueryComplexity) RecallLevels() []TemporalLevel {
	switch c {
	case ComplexitySimple:
		return []TemporalLevel{LevelSegment, LevelSession, LevelDay}
	case ComplexityHybrid:
		return []TemporalLevel{LevelSegment, LevelSession, LevelDay, LevelWeek}
	case ComplexityComplex:
		return []TemporalLevel{LevelSegment, LevelSession, LevelDay, LevelWeek, LevelProfile}
	default:
		return []TemporalLevel{LevelSegment, LevelSession, LevelDay}
	}
}

// ConsolidationResult summarizes a consolidation run.
type ConsolidationResult struct {
	Level          TemporalLevel `json:"level"`
	NodesCreated   int           `json:"nodes_created"`
	ChildrenMerged int           `json:"children_merged"`
	Duration       string        `json:"duration,omitempty"`
}

// Entry represents a single memory record.
type Entry struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	// Title is a short human-readable label for this entry, set by the writer
	// at creation time. Used by ProjectIndex for task list display names.
	// When empty, ProjectIndex falls back to extracting a title from Content.
	Title       string    `json:"title,omitempty"`
	Category    Category  `json:"category"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	AccessCount int       `json:"access_count"`
	// --- F1: Vector embedding ---
	Embedding []float32 `json:"embedding,omitempty"`
	// --- F3: Memory graph ---
	RelatedIDs   []string      `json:"related_ids,omitempty"`
	RelatedEdges []RelatedEdge `json:"related_edges,omitempty"`
	// --- F5: Forgetting curve ---
	Strength float64 `json:"strength,omitempty"`
	// --- F6: Conflict detection ---
	Status Status `json:"status,omitempty"`
	// --- F7: Cross-project scope ---
	Scope Scope `json:"scope,omitempty"`
	// --- Pin mechanism ---
	Pinned bool `json:"pinned,omitempty"`
	// --- Compact form for context injection ---
	CompactForm string `json:"compact_form,omitempty"`
	// --- Temporal memory tree metadata ---
	Level    TemporalLevel `json:"level,omitempty"`
	Interval *TimeInterval `json:"interval,omitempty"`
	ParentID string        `json:"parent_id,omitempty"`
	ChildIDs []string      `json:"child_ids,omitempty"`
	// --- Source provenance (inspired by GBrain) ---
	SourceURL  string `json:"source_url,omitempty"`
	SourceType string `json:"source_type,omitempty"` // e.g. "conversation", "web", "meeting", "manual"
	// --- Content hash for idempotent import (inspired by GBrain) ---
	ContentHash string `json:"content_hash,omitempty"`
	// --- Version history: last 3 snapshots (inspired by GBrain page_versions) ---
	Versions []VersionSnapshot `json:"versions,omitempty"`
	// --- Stale flag: set by dream cycle when newer conflicting entry exists ---
	Stale bool `json:"stale,omitempty"`
	// --- Bi-temporal model (inspired by Graphiti/Zep) ---
	// ValidAt is when this fact became true in the real world.
	// nil means "unknown" or "always true" (e.g. user preferences without a start date).
	// Extracted by the online extraction pipeline from conversation context.
	ValidAt *time.Time `json:"valid_at,omitempty"`
	// InvalidAt is when this fact stopped being true in the real world.
	// nil means "still true" or "unknown".
	// Set when a contradicting fact is ingested (four-operation update: DELETE/UPDATE).
	InvalidAt *time.Time `json:"invalid_at,omitempty"`
	// --- Entity-relation triples (inspired by Mem0^g / Graphiti) ---
	// Entities extracted from this entry's content, stored as structured tags.
	// Format: ["entity:Alice", "entity:Shanghai", "relation:lives_in"]
	// Used by tag-based recall and entity index for multi-hop reasoning.
	Entities []string `json:"entities,omitempty"`
	// --- Multi-tenant ownership (maclawsrv only) ---
	// OwnerID identifies the user who owns this memory entry.
	// Empty string means "shared" - visible to all users.
	// In GUI/TUI (single-user): always empty, all memories belong to the same user.
	// In maclawsrv (multi-tenant): set to the IM user ID (e.g. feishu_ou_xxx).
	OwnerID string `json:"owner_id,omitempty"`
}

// IsActive returns true if the entry participates in normal recall.
func (e *Entry) IsActive() bool {
	return e.Status == StatusActive
}

// BackupInfo describes a single memory backup snapshot.
type BackupInfo struct {
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	SizeBytes  int64  `json:"size_bytes"`
	EntryCount int    `json:"entry_count"`
}

// CompressResult holds the outcome of a compression run.
type CompressResult struct {
	BackupName      string `json:"backup_name"`
	TotalEntries    int    `json:"total_entries"`
	DedupCount      int    `json:"dedup_count"`
	MergedCount     int    `json:"merged_count"`
	CompressedCount int    `json:"compressed_count"`
	SkippedCount    int    `json:"skipped_count"`
	ErrorCount      int    `json:"error_count"`
	SavedChars      int    `json:"saved_chars"`
}

// CompressorStatus is returned by the status query.
type CompressorStatus struct {
	Running    bool            `json:"running"`
	LastRun    string          `json:"last_run,omitempty"`
	LastResult *CompressResult `json:"last_result,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
}

// GCResult records the outcome of an intelligent GC cycle.
type GCResult struct {
	ArchivedCount int `json:"archived_count"`
	RevivedCount  int `json:"revived_count"`
	ActiveBefore  int `json:"active_before"`
	ActiveAfter   int `json:"active_after"`
	SkippedPinned int `json:"skipped_pinned"`
}

// DreamCycleResult records the outcome of a dream cycle (background self-healing).
type DreamCycleResult struct {
	StaleDetected    int `json:"stale_detected"`
	LinksDiscovered  int `json:"links_discovered"`
	HashesBackfilled int `json:"hashes_backfilled"`
	TagsBackfilled   int `json:"tags_backfilled"`
}

// SearchMode controls which retrieval strategy to use.
type SearchMode int

const (
	SearchHybrid      SearchMode = iota // BM25 + vector + RRF (default)
	SearchKeywordOnly                   // BM25 only, no vector
	SearchDirect                        // exact ID lookup
)

// HealthReport provides an aggregated view of memory system health.
// Inspired by GBrain's `gbrain health` / `gbrain doctor` commands.
type HealthReport struct {
	ActiveEntries    int            `json:"active_entries"`
	MaxCapacity      int            `json:"max_capacity"`
	CapacityPercent  float64        `json:"capacity_percent"`
	ArchivedEntries  int            `json:"archived_entries"`
	StaleEntries     int            `json:"stale_entries"`
	OrphanEntries    int            `json:"orphan_entries"` // no graph edges
	NoEmbedding      int            `json:"no_embedding"`   // missing vector
	NoHash           int            `json:"no_hash"`        // missing content hash
	PinnedEntries    int            `json:"pinned_entries"`
	EmbedderActive   bool           `json:"embedder_active"`
	CategoryCounts   map[string]int `json:"category_counts"`
	AvgAccessCount   float64        `json:"avg_access_count"`
	OldestEntry      string         `json:"oldest_entry,omitempty"` // RFC3339
	NewestEntry      string         `json:"newest_entry,omitempty"` // RFC3339
	VersionedEntries int            `json:"versioned_entries"`      // entries with version history
}

// ---------------------------------------------------------------------------
// Token estimation (CJK-aware)
// ---------------------------------------------------------------------------

// EstimateTextTokens delegates to corelib.EstimateTextTokens.
// Package-level alias so callers within memory/ use a consistent function name.
func EstimateTextTokens(text string) int {
	return corelib.EstimateTextTokens(text)
}

// ---------------------------------------------------------------------------
// Mem0-style four-operation memory management
// ---------------------------------------------------------------------------

// MemoryOperation represents the action to take when integrating a new fact
// into the memory store. Inspired by Mem0's extraction/update pipeline.
type MemoryOperation string

const (
	// OpAdd creates a new memory entry (no semantically equivalent memory exists).
	OpAdd MemoryOperation = "add"
	// OpUpdate augments an existing memory with complementary information.
	OpUpdate MemoryOperation = "update"
	// OpDelete removes/invalidates a memory contradicted by new information.
	OpDelete MemoryOperation = "delete"
	// OpNoop indicates the candidate fact requires no modification to the store.
	OpNoop MemoryOperation = "noop"
)

// OnlineExtractionResult holds the outcome of one online extraction cycle.
type OnlineExtractionResult struct {
	ExtractedFacts int `json:"extracted_facts"`
	Added          int `json:"added"`
	Updated        int `json:"updated"`
	Deleted        int `json:"deleted"`
	Noops          int `json:"noops"`
	Errors         int `json:"errors"`
}

// ExtractedFact represents a single fact extracted from conversation by the
// online extraction pipeline, with optional temporal and entity annotations.
type ExtractedFact struct {
	Content     string          `json:"content"`
	Category    string          `json:"category"`             // "user_fact", "project_knowledge", "preference", "instruction"
	RawEntities json.RawMessage `json:"entities,omitempty"`   // tolerates flat ["a","b"] and nested [["a","b"]]
	ValidAt     string          `json:"valid_at,omitempty"`   // ISO 8601 datetime or empty
	InvalidAt   string          `json:"invalid_at,omitempty"` // ISO 8601 datetime or empty
}

// ParsedEntities returns the entities as a flat []string, tolerating three
// formats that LLMs produce:
//   - Flat array:   ["entity:X", "relation:Y", "entity:Z"]
//   - Nested array: [["entity:X", "relation:Y", "entity:Z"]]
//   - Single string: "entity:X"
func (f ExtractedFact) ParsedEntities() []string {
	if len(f.RawEntities) == 0 {
		return nil
	}
	// Try flat array first (most common).
	var flat []string
	if err := json.Unmarshal(f.RawEntities, &flat); err == nil {
		return canonicalizeExtractedEntities(flat)
	}
	// Try nested array: LLM wraps each triple in its own array.
	var nested [][]string
	if err := json.Unmarshal(f.RawEntities, &nested); err == nil {
		var result []string
		for _, arr := range nested {
			result = append(result, arr...)
		}
		return canonicalizeExtractedEntities(result)
	}
	// Try single string.
	var single string
	if err := json.Unmarshal(f.RawEntities, &single); err == nil && single != "" {
		return canonicalizeExtractedEntities([]string{single})
	}
	return nil
}

func canonicalizeExtractedEntities(raw []string) []string {
	if len(raw) == 0 {
		return raw
	}
	out := make([]string, 0, len(raw))
	for i := 0; i < len(raw); i += 3 {
		end := i + 3
		if end > len(raw) {
			end = len(raw)
		}
		chunk := canonicalizeExtractedEntityChunk(raw[i:end])
		out = append(out, chunk...)
	}
	return out
}

func canonicalizeExtractedEntityChunk(raw []string) []string {
	if len(raw) == 3 {
		subj, subjOK := canonicalEntityToken(raw[0])
		rel, reverse, relOK := canonicalRelationToken(raw[1])
		obj, objOK := canonicalEntityToken(raw[2])
		if subjOK && relOK && objOK {
			if reverse {
				subj, obj = obj, subj
			}
			return []string{subj, rel, obj}
		}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if ent, ok := canonicalEntityToken(item); ok {
			out = append(out, ent)
			continue
		}
		if rel, _, ok := canonicalRelationToken(item); ok {
			out = append(out, rel)
			continue
		}
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func canonicalEntityToken(item string) (string, bool) {
	trimmed := strings.TrimSpace(item)
	if !strings.HasPrefix(strings.ToLower(trimmed), "entity:") {
		return "", false
	}
	name := strings.TrimSpace(trimmed[len("entity:"):])
	if name == "" {
		return "", false
	}
	return "entity:" + name, true
}

func canonicalRelationToken(item string) (string, bool, bool) {
	trimmed := strings.TrimSpace(item)
	if !strings.HasPrefix(strings.ToLower(trimmed), "relation:") {
		return "", false, false
	}
	rel, reverse := normalizeRelationNameWithDirection(strings.TrimSpace(trimmed[len("relation:"):]))
	if rel == "" {
		return "", false, false
	}
	return "relation:" + rel, reverse, true
}

// ClassifiedOperation is the LLM's decision on how to integrate a new fact.
type ClassifiedOperation struct {
	Operation  MemoryOperation `json:"operation"`   // add, update, delete, noop
	TargetID   string          `json:"target_id"`   // entry ID for update/delete
	MergedText string          `json:"merged_text"` // merged content for update
	Reason     string          `json:"reason"`      // brief explanation
}
