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
)

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
	switch c {
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
	switch c {
	case CategoryConversationSummary, CategorySessionCheckpoint:
		return TierEpisodic
	default:
		return TierSemantic
	}
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
