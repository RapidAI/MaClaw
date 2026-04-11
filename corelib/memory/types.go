package memory

import "time"

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

	// Claude-style four-type taxonomy (inspired by Claude Code memdir).
	// These map to the original categories but provide a cleaner semantic model:
	//   user     — user role, goals, knowledge (maps to user_fact)
	//   feedback — corrections and confirmations on approach (maps to instruction)
	//   project  — non-derivable project context, decisions, deadlines (maps to project_knowledge)
	//   reference — pointers to external systems (maps to project_knowledge)
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
	StatusActive     Status = ""           // default — participates in recall
	StatusSuperseded Status = "superseded" // replaced by a newer conflicting entry
	StatusDormant    Status = "dormant"    // forgotten — below strength threshold
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
	LinkRelated    LinkType = ""           // default — generic relatedness
	LinkReferences LinkType = "references" // A references B
	LinkSupersedes LinkType = "supersedes" // A supersedes B
	LinkDerivedFrom LinkType = "derived_from" // A was derived from B
	LinkConflicts  LinkType = "conflicts"  // A conflicts with B
)

// VersionSnapshot records a previous version of an entry's content.
type VersionSnapshot struct {
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Entry represents a single memory record.
type Entry struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`
	Category    Category  `json:"category"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	AccessCount int       `json:"access_count"`
	// --- F1: Vector embedding ---
	Embedding []float32 `json:"embedding,omitempty"`
	// --- F3: Memory graph ---
	RelatedIDs []string `json:"related_ids,omitempty"`
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
	// --- Source provenance (inspired by GBrain) ---
	SourceURL  string `json:"source_url,omitempty"`
	SourceType string `json:"source_type,omitempty"` // e.g. "conversation", "web", "meeting", "manual"
	// --- Content hash for idempotent import (inspired by GBrain) ---
	ContentHash string `json:"content_hash,omitempty"`
	// --- Version history: last 3 snapshots (inspired by GBrain page_versions) ---
	Versions []VersionSnapshot `json:"versions,omitempty"`
	// --- Stale flag: set by dream cycle when newer conflicting entry exists ---
	Stale bool `json:"stale,omitempty"`
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
	OrphanEntries    int            `json:"orphan_entries"`    // no graph edges
	NoEmbedding      int            `json:"no_embedding"`      // missing vector
	NoHash           int            `json:"no_hash"`           // missing content hash
	PinnedEntries    int            `json:"pinned_entries"`
	EmbedderActive   bool           `json:"embedder_active"`
	CategoryCounts   map[string]int `json:"category_counts"`
	AvgAccessCount   float64        `json:"avg_access_count"`
	OldestEntry      string         `json:"oldest_entry,omitempty"`      // RFC3339
	NewestEntry      string         `json:"newest_entry,omitempty"`      // RFC3339
	VersionedEntries int            `json:"versioned_entries"` // entries with version history
}
