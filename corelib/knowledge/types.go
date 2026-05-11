package knowledge

import "time"

const (
	SourceKindURL              = "url"
	SourceKindHTML             = "html"
	SourceKindDOCX             = "docx"
	SourceKindDOC              = "doc"
	SourceKindPDF              = "pdf"
	SourceKindXLSX             = "xlsx"
	SourceKindXLS              = "xls"
	SourceKindCSV              = "csv"
	SourceKindMarkdown         = "markdown"
	SourceKindText             = "text"
	SourceKindConversation     = "conversation"
	SourceKindWorkflowArtifact = "workflow_artifact"

	StatusPending   = "pending"
	StatusParsed    = "parsed"
	StatusDistilled = "distilled"
	StatusFailed    = "failed"
	StatusStale     = "stale"
	StatusDisabled  = "disabled"

	ImportStatusScanned   = "scanned"
	ImportStatusQueued    = "queued"
	ImportStatusRunning   = "running"
	ImportStatusCompleted = "completed"
	ImportStatusFailed    = "failed"
	ImportStatusCancelled = "cancelled"

	ItemStatusQueued           = "queued"
	ItemStatusImported         = "imported"
	ItemStatusSkippedDuplicate = "skipped_duplicate"
	ItemStatusSkippedTooLarge  = "skipped_too_large"
	ItemStatusSkippedHidden    = "skipped_hidden"
	ItemStatusSkippedType      = "skipped_unsupported_type"
	ItemStatusSkippedSymlink   = "skipped_symlink"
	ItemStatusFailed           = "failed"

	SaveScopeSession   = "session"
	SaveScopeProject   = "project"
	SaveScopePersonal  = "personal"
	SaveScopeLocalOnly = "local_only"
)

const DefaultMaxFileBytes int64 = 100 * 1024 * 1024

var DefaultIncludeExts = []string{".docx", ".pdf", ".xlsx", ".csv", ".md", ".txt", ".doc", ".xls"}

// SaveStatusCreated indicates the source was newly created.
const SaveStatusCreated = "created"

// SaveStatusDuplicate indicates the source content already existed and was updated in place.
const SaveStatusDuplicate = "duplicate"

// Source is a persisted original knowledge input: a URL, uploaded file,
// imported local file, or derived conversation/workflow artifact.
type Source struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	URI          string    `json:"uri"`
	CanonicalURI string    `json:"canonical_uri,omitempty"`
	Title        string    `json:"title,omitempty"`
	Author       string    `json:"author,omitempty"`
	SiteName     string    `json:"site_name,omitempty"`
	PublishedAt  time.Time `json:"published_at,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
	ContentHash  string    `json:"content_hash"`
	OwnerID      string    `json:"owner_id,omitempty"`
	TenantID     string    `json:"tenant_id,omitempty"`
	ProjectPath  string    `json:"project_path,omitempty"`
	TopicHint    string    `json:"topic_hint,omitempty"`
	SourceTrust  float64   `json:"source_trust,omitempty"`
	BatchID      string    `json:"batch_id,omitempty"`
	RelativePath string    `json:"relative_path,omitempty"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	NodeCount    int       `json:"node_count,omitempty"`
	CardCount    int       `json:"card_count,omitempty"`
	FactCount    int       `json:"fact_count,omitempty"`
	Labels       []string  `json:"labels,omitempty"`
	// SaveStatus is set by SaveText/SaveURL to indicate whether this was a new
	// creation ("created") or an update of an existing duplicate ("duplicate").
	// It is transient (not persisted) and only meaningful in the return value of save operations.
	SaveStatus string `json:"save_status,omitempty"`
}

type SourceVersion struct {
	ID           string    `json:"id"`
	SourceID     string    `json:"source_id"`
	Kind         string    `json:"kind,omitempty"`
	URI          string    `json:"uri,omitempty"`
	CanonicalURI string    `json:"canonical_uri,omitempty"`
	Title        string    `json:"title,omitempty"`
	ContentHash  string    `json:"content_hash,omitempty"`
	Status       string    `json:"status,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	FetchedAt    time.Time `json:"fetched_at,omitempty"`
	NodeCount    int       `json:"node_count,omitempty"`
	CardCount    int       `json:"card_count,omitempty"`
	FactCount    int       `json:"fact_count,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type SourceTimelineEvent struct {
	ID              string    `json:"id"`
	SourceID        string    `json:"source_id"`
	Kind            string    `json:"kind"`
	Action          string    `json:"action,omitempty"`
	Title           string    `json:"title,omitempty"`
	Detail          string    `json:"detail,omitempty"`
	Status          string    `json:"status,omitempty"`
	Relation        string    `json:"relation,omitempty"`
	RelatedSourceID string    `json:"related_source_id,omitempty"`
	Score           float64   `json:"score,omitempty"`
	Terms           []string  `json:"terms,omitempty"`
	Evidence        []string  `json:"evidence,omitempty"`
	VersionID       string    `json:"version_id,omitempty"`
	ContentHash     string    `json:"content_hash,omitempty"`
	NodeCount       int       `json:"node_count,omitempty"`
	CardCount       int       `json:"card_count,omitempty"`
	FactCount       int       `json:"fact_count,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type SourceTimelineResult struct {
	SourceID    string                `json:"source_id"`
	Source      Source                `json:"source"`
	Count       int                   `json:"count"`
	Limit       int                   `json:"limit"`
	Events      []SourceTimelineEvent `json:"events"`
	Notes       []string              `json:"notes,omitempty"`
	GeneratedAt time.Time             `json:"generated_at"`
}

type SourceDigestResult struct {
	SourceID      string                `json:"source_id"`
	Source        Source                `json:"source"`
	Title         string                `json:"title,omitempty"`
	Labels        []string              `json:"labels,omitempty"`
	Topics        []string              `json:"topics,omitempty"`
	Entities      []string              `json:"entities,omitempty"`
	Tags          []string              `json:"tags,omitempty"`
	NodeCount     int                   `json:"node_count"`
	CardCount     int                   `json:"card_count"`
	FactCount     int                   `json:"fact_count"`
	LinkCount     int                   `json:"link_count"`
	TimelineCount int                   `json:"timeline_count"`
	Nodes         []DocumentNode        `json:"nodes,omitempty"`
	Cards         []Card                `json:"cards,omitempty"`
	Facts         []Fact                `json:"facts,omitempty"`
	Links         []SourceLink          `json:"links,omitempty"`
	Timeline      []SourceTimelineEvent `json:"timeline,omitempty"`
	Notes         []string              `json:"notes,omitempty"`
	GeneratedAt   time.Time             `json:"generated_at"`
}

type SourceLabel struct {
	SourceID  string    `json:"source_id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

type SourceLabelSummary struct {
	Label       string   `json:"label"`
	Count       int      `json:"count"`
	SourceIDs   []string `json:"source_ids,omitempty"`
	SourceNames []string `json:"source_names,omitempty"`
}

// DocumentNode is a structure-preserving parsed node from a Source.
type DocumentNode struct {
	ID         string            `json:"id"`
	SourceID   string            `json:"source_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Type       string            `json:"type"`
	Title      string            `json:"title,omitempty"`
	Text       string            `json:"text,omitempty"`
	Level      int               `json:"level,omitempty"`
	Page       int               `json:"page,omitempty"`
	SheetName  string            `json:"sheet_name,omitempty"`
	RowRange   string            `json:"row_range,omitempty"`
	ColRange   string            `json:"col_range,omitempty"`
	XPath      string            `json:"xpath,omitempty"`
	Offset     int               `json:"offset,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	TokenCount int               `json:"token_count,omitempty"`
}

// Card is the primary recall unit distilled from one or more nodes.
type Card struct {
	ID          string    `json:"id"`
	SourceID    string    `json:"source_id"`
	NodeID      string    `json:"node_id,omitempty"`
	Title       string    `json:"title,omitempty"`
	Claim       string    `json:"claim"`
	Summary     string    `json:"summary,omitempty"`
	Entities    []string  `json:"entities,omitempty"`
	Topics      []string  `json:"topics,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Facts       []Fact    `json:"facts,omitempty"`
	ProjectPath string    `json:"project_path,omitempty"`
	OwnerID     string    `json:"owner_id,omitempty"`
	TenantID    string    `json:"tenant_id,omitempty"`
	ValidAt     time.Time `json:"valid_at,omitempty"`
	InvalidAt   time.Time `json:"invalid_at,omitempty"`
	Confidence  float64   `json:"confidence,omitempty"`
	Importance  float64   `json:"importance,omitempty"`
	SourceTrust float64   `json:"source_trust,omitempty"`
	Embedding   []float32 `json:"embedding,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Fact is an entity-relation fact grounded in a Card and Source.
type Fact struct {
	ID         string    `json:"id"`
	CardID     string    `json:"card_id"`
	SourceID   string    `json:"source_id"`
	Subject    string    `json:"subject"`
	Predicate  string    `json:"predicate"`
	Object     string    `json:"object"`
	Negated    bool      `json:"negated,omitempty"`
	ValidAt    time.Time `json:"valid_at,omitempty"`
	InvalidAt  time.Time `json:"invalid_at,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
}

type FactGraphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Count int    `json:"count,omitempty"`
}

type FactGraphEdge struct {
	ID          string  `json:"id"`
	FactID      string  `json:"fact_id"`
	CardID      string  `json:"card_id,omitempty"`
	SourceID    string  `json:"source_id,omitempty"`
	Subject     string  `json:"subject"`
	Predicate   string  `json:"predicate"`
	Object      string  `json:"object"`
	SourceTitle string  `json:"source_title,omitempty"`
	Citation    string  `json:"citation,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

type FactGraphResult struct {
	Query         string          `json:"query,omitempty"`
	Entity        string          `json:"entity,omitempty"`
	Predicate     string          `json:"predicate,omitempty"`
	Count         int             `json:"count"`
	Nodes         []FactGraphNode `json:"nodes,omitempty"`
	Edges         []FactGraphEdge `json:"edges,omitempty"`
	TopEntities   []FactGraphNode `json:"top_entities,omitempty"`
	TopPredicates []FactGraphNode `json:"top_predicates,omitempty"`
	Notes         []string        `json:"notes,omitempty"`
}

type FactIndexOptions struct {
	SearchOptions
	Kind string `json:"kind,omitempty"`
}

type FactIndexItem struct {
	Label       string   `json:"label"`
	Kind        string   `json:"kind"`
	Count       int      `json:"count"`
	SourceCount int      `json:"source_count,omitempty"`
	CardCount   int      `json:"card_count,omitempty"`
	Predicates  []string `json:"predicates,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

type FactIndexResult struct {
	Query string          `json:"query,omitempty"`
	Kind  string          `json:"kind,omitempty"`
	Count int             `json:"count"`
	Items []FactIndexItem `json:"items,omitempty"`
	Notes []string        `json:"notes,omitempty"`
}

type EntityProfileResult struct {
	Entity          string          `json:"entity"`
	Count           int             `json:"count"`
	Facts           []FactGraphEdge `json:"facts,omitempty"`
	RelatedEntities []FactIndexItem `json:"related_entities,omitempty"`
	Predicates      []FactIndexItem `json:"predicates,omitempty"`
	Citations       []Citation      `json:"citations,omitempty"`
	Notes           []string        `json:"notes,omitempty"`
}

type KnowledgeSuggestOptions struct {
	SearchOptions
	Kinds []string `json:"kinds,omitempty"`
}

type KnowledgeSuggestion struct {
	Label       string   `json:"label"`
	Kind        string   `json:"kind"`
	Count       int      `json:"count,omitempty"`
	SourceID    string   `json:"source_id,omitempty"`
	SourceKind  string   `json:"source_kind,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	SourceLabel string   `json:"source_label,omitempty"`
	URI         string   `json:"uri,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

type KnowledgeSuggestResult struct {
	Query string                `json:"query,omitempty"`
	Count int                   `json:"count"`
	Items []KnowledgeSuggestion `json:"items,omitempty"`
	Notes []string              `json:"notes,omitempty"`
}

type DuplicateCardGroup struct {
	Key         string   `json:"key"`
	Claim       string   `json:"claim"`
	Count       int      `json:"count"`
	CardIDs     []string `json:"card_ids,omitempty"`
	SourceIDs   []string `json:"source_ids,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	OwnerID     string   `json:"owner_id,omitempty"`
	TenantID    string   `json:"tenant_id,omitempty"`
	ProjectPath string   `json:"project_path,omitempty"`
}

type CardSuppression struct {
	CardID       string    `json:"card_id"`
	SourceID     string    `json:"source_id,omitempty"`
	Claim        string    `json:"claim,omitempty"`
	SourceTitle  string    `json:"source_title,omitempty"`
	RelativePath string    `json:"relative_path,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
}

type DuplicateCardSuppressionRequest struct {
	Key         string `json:"key"`
	KeepCardID  string `json:"keep_card_id,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	TenantID    string `json:"tenant_id,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type CardSuppressionResult struct {
	Suppressed int               `json:"suppressed"`
	Restored   int               `json:"restored,omitempty"`
	KeptCardID string            `json:"kept_card_id,omitempty"`
	CardIDs    []string          `json:"card_ids,omitempty"`
	Items      []CardSuppression `json:"items,omitempty"`
}

type SensitiveFinding struct {
	Kind         string `json:"kind"`
	Severity     string `json:"severity"`
	SourceID     string `json:"source_id,omitempty"`
	SourceTitle  string `json:"source_title,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	URI          string `json:"uri,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	CardID       string `json:"card_id,omitempty"`
	Field        string `json:"field,omitempty"`
	Redacted     string `json:"redacted,omitempty"`
	Snippet      string `json:"snippet,omitempty"`
}

type SensitiveScanResult struct {
	Count       int                `json:"count"`
	MaxSeverity string             `json:"max_severity,omitempty"`
	Findings    []SensitiveFinding `json:"findings,omitempty"`
}

type SensitiveIsolationResult struct {
	Scan      SensitiveScanResult      `json:"scan"`
	SourceIDs []string                 `json:"source_ids,omitempty"`
	Update    SourceStatusUpdateResult `json:"update"`
}

type SourceQualityItem struct {
	Source            Source   `json:"source"`
	Score             int      `json:"score"`
	Grade             string   `json:"grade"`
	Signals           []string `json:"signals,omitempty"`
	Actions           []string `json:"actions,omitempty"`
	SensitiveFindings int      `json:"sensitive_findings,omitempty"`
	DuplicateClaims   int      `json:"duplicate_claims,omitempty"`
}

type SourceQualityReport struct {
	Count        int                 `json:"count"`
	AverageScore float64             `json:"average_score,omitempty"`
	Grades       map[string]int      `json:"grades,omitempty"`
	Signals      map[string]int      `json:"signals,omitempty"`
	Actions      map[string]int      `json:"actions,omitempty"`
	Items        []SourceQualityItem `json:"items,omitempty"`
	Notes        []string            `json:"notes,omitempty"`
}

type SourceQualityMaintenanceAction struct {
	Kind        string                 `json:"kind"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Severity    string                 `json:"severity,omitempty"`
	Count       int                    `json:"count"`
	SourceIDs   []string               `json:"source_ids,omitempty"`
	Signals     []string               `json:"signals,omitempty"`
	Tool        string                 `json:"tool,omitempty"`
	Args        map[string]interface{} `json:"args,omitempty"`
}

type SourceQualityMaintenancePlan struct {
	Quality SourceQualityReport              `json:"quality"`
	Count   int                              `json:"count"`
	Actions []SourceQualityMaintenanceAction `json:"actions,omitempty"`
	Notes   []string                         `json:"notes,omitempty"`
}

type SourceQualityMaintenancePolicy struct {
	Name                      string   `json:"name"`
	Title                     string   `json:"title,omitempty"`
	Description               string   `json:"description,omitempty"`
	Actions                   []string `json:"actions,omitempty"`
	DefaultDryRun             bool     `json:"default_dry_run,omitempty"`
	DistillMode               string   `json:"distill_mode,omitempty"`
	MaxSourcesPerAction       int      `json:"max_sources_per_action,omitempty"`
	AllowSensitiveDisable     bool     `json:"allow_sensitive_disable,omitempty"`
	AllowDuplicateSuppression bool     `json:"allow_duplicate_suppression,omitempty"`
	QueryRequiresLLM          bool     `json:"query_requires_llm"`
	MayUseLLMForStructuring   bool     `json:"may_use_llm_for_structuring,omitempty"`
	RequiresExplicitWrite     bool     `json:"requires_explicit_write,omitempty"`
	RequiresExplicitSensitive bool     `json:"requires_explicit_sensitive,omitempty"`
	RequiresExplicitDuplicate bool     `json:"requires_explicit_duplicate,omitempty"`
	Notes                     []string `json:"notes,omitempty"`
}

type SourceQualityMaintenanceExecuteRequest struct {
	Filter                    ListSourcesOptions `json:"filter,omitempty"`
	Policy                    string             `json:"policy,omitempty"`
	Actions                   []string           `json:"actions,omitempty"`
	DryRun                    bool               `json:"dry_run,omitempty"`
	DistillMode               string             `json:"distill_mode,omitempty"`
	MaxSourcesPerAction       int                `json:"max_sources_per_action,omitempty"`
	AllowSensitiveDisable     bool               `json:"allow_sensitive_disable,omitempty"`
	AllowDuplicateSuppression bool               `json:"allow_duplicate_suppression,omitempty"`
}

type SourceQualityMaintenanceActionResult struct {
	Kind      string      `json:"kind"`
	Requested int         `json:"requested"`
	Updated   int         `json:"updated,omitempty"`
	Failed    int         `json:"failed,omitempty"`
	Skipped   int         `json:"skipped,omitempty"`
	DryRun    bool        `json:"dry_run,omitempty"`
	SourceIDs []string    `json:"source_ids,omitempty"`
	Result    interface{} `json:"result,omitempty"`
	Warnings  []string    `json:"warnings,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type SourceQualityMaintenanceExecuteResult struct {
	Plan     SourceQualityMaintenancePlan           `json:"plan"`
	DryRun   bool                                   `json:"dry_run,omitempty"`
	Count    int                                    `json:"count"`
	Results  []SourceQualityMaintenanceActionResult `json:"results,omitempty"`
	Warnings []string                               `json:"warnings,omitempty"`
	Notes    []string                               `json:"notes,omitempty"`
}

// ImportBatch tracks one directory import operation.
type ImportBatch struct {
	ID           string    `json:"id"`
	RootPath     string    `json:"root_path"`
	OwnerID      string    `json:"owner_id,omitempty"`
	TenantID     string    `json:"tenant_id,omitempty"`
	ProjectPath  string    `json:"project_path,omitempty"`
	TopicHint    string    `json:"topic_hint,omitempty"`
	Recursive    bool      `json:"recursive"`
	IncludeExts  []string  `json:"include_exts,omitempty"`
	ExcludeGlobs []string  `json:"exclude_globs,omitempty"`
	MaxFileBytes int64     `json:"max_file_bytes,omitempty"`
	Status       string    `json:"status"`
	TotalFiles   int       `json:"total_files"`
	QueuedFiles  int       `json:"queued_files"`
	Imported     int       `json:"imported_files"`
	Skipped      int       `json:"skipped_files"`
	Failed       int       `json:"failed_files"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ImportItem tracks one file within an import batch.
type ImportItem struct {
	ID           string    `json:"id"`
	BatchID      string    `json:"batch_id"`
	SourceID     string    `json:"source_id,omitempty"`
	FilePath     string    `json:"file_path"`
	RelativePath string    `json:"relative_path,omitempty"`
	FileHash     string    `json:"file_hash,omitempty"`
	FileSize     int64     `json:"file_size"`
	Kind         string    `json:"kind,omitempty"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type DirectoryImportRequest struct {
	RootPath     string   `json:"root_path"`
	OwnerID      string   `json:"owner_id,omitempty"`
	TenantID     string   `json:"tenant_id,omitempty"`
	ProjectPath  string   `json:"project_path,omitempty"`
	TopicHint    string   `json:"topic_hint,omitempty"`
	SaveScope    string   `json:"save_scope,omitempty"`
	DistillMode  string   `json:"distill_mode,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	AutoLabels   bool     `json:"auto_labels,omitempty"`
	Recursive    bool     `json:"recursive"`
	IncludeExts  []string `json:"include_exts,omitempty"`
	ExcludeGlobs []string `json:"exclude_globs,omitempty"`
	MaxFileBytes int64    `json:"max_file_bytes,omitempty"`
	DryRun       bool     `json:"dry_run,omitempty"`
}

type DirectoryImportResult struct {
	BatchID        string       `json:"batch_id,omitempty"`
	Status         string       `json:"status"`
	RootPath       string       `json:"root_path"`
	TotalFiles     int          `json:"total_files"`
	QueuedFiles    int          `json:"queued_files"`
	DuplicateFiles int          `json:"duplicate_files"`
	SkippedFiles   int          `json:"skipped_files"`
	ImportedFiles  int          `json:"imported_files"`
	FailedFiles    int          `json:"failed_files"`
	ProcessedFiles int          `json:"processed_files,omitempty"`
	CurrentFile    string       `json:"current_file,omitempty"`
	CurrentStep    string       `json:"current_step,omitempty"`  // e.g. "parsing", "indexing", "distilling"
	StepProgress   int          `json:"step_progress,omitempty"` // 0-100 within current file
	TotalSteps     int          `json:"total_steps,omitempty"`   // total steps for current file (e.g. 5)
	CurrentStepNum int          `json:"current_step_num,omitempty"` // which step (1-based)
	EstimatedBytes int64        `json:"estimated_bytes"`
	Warnings       []string     `json:"warnings,omitempty"`
	Items          []ImportItem `json:"items,omitempty"`
}

type ImportRetryRequest struct {
	BatchID        string   `json:"batch_id"`
	ItemIDs        []string `json:"item_ids,omitempty"`
	Statuses       []string `json:"statuses,omitempty"`
	IncludeSkipped bool     `json:"include_skipped,omitempty"`
	IncludeExts    []string `json:"include_exts,omitempty"`
	MaxFileBytes   int64    `json:"max_file_bytes,omitempty"`
	TopicHint      string   `json:"topic_hint,omitempty"`
	DistillMode    string   `json:"distill_mode,omitempty"`
}

type ListSourcesOptions struct {
	OwnerID         string   `json:"owner_id,omitempty"`
	TenantID        string   `json:"tenant_id,omitempty"`
	SearchScope     string   `json:"search_scope,omitempty"`
	ProjectPath     string   `json:"project_path,omitempty"`
	SourceIDs       []string `json:"source_ids,omitempty"`
	Status          string   `json:"status,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	SourceKinds     []string `json:"source_kinds,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	Label           string   `json:"label,omitempty"`
	Labels          []string `json:"labels,omitempty"`
	Query           string   `json:"query,omitempty"`
	CoverageFilter  string   `json:"coverage_filter,omitempty"`
	QualityGrade    string   `json:"quality_grade,omitempty"`
	QualityGrades   []string `json:"quality_grades,omitempty"`
	MinQualityScore int      `json:"min_quality_score,omitempty"`
	MaxQualityScore int      `json:"max_quality_score,omitempty"`
	Limit           int      `json:"limit,omitempty"`
}

type URLSaveRequest struct {
	URL         string   `json:"url"`
	OwnerID     string   `json:"owner_id,omitempty"`
	TenantID    string   `json:"tenant_id,omitempty"`
	ProjectPath string   `json:"project_path,omitempty"`
	TopicHint   string   `json:"topic_hint,omitempty"`
	SaveScope   string   `json:"save_scope,omitempty"`
	DistillMode string   `json:"distill_mode,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	AutoLabels  bool     `json:"auto_labels,omitempty"`
	MaxBytes    int64    `json:"max_bytes,omitempty"`
	TimeoutSec  int      `json:"timeout_sec,omitempty"`
}

type URLBatchSaveRequest struct {
	URLs        []string `json:"urls"`
	OwnerID     string   `json:"owner_id,omitempty"`
	TenantID    string   `json:"tenant_id,omitempty"`
	ProjectPath string   `json:"project_path,omitempty"`
	TopicHint   string   `json:"topic_hint,omitempty"`
	SaveScope   string   `json:"save_scope,omitempty"`
	DistillMode string   `json:"distill_mode,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	AutoLabels  bool     `json:"auto_labels,omitempty"`
	MaxBytes    int64    `json:"max_bytes,omitempty"`
	TimeoutSec  int      `json:"timeout_sec,omitempty"`
}

type URLDiscoveryRequest struct {
	Text           string `json:"text"`
	BaseURL        string `json:"base_url,omitempty"`
	SameDomainOnly bool   `json:"same_domain_only,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type URLDiscoveryItem struct {
	URL    string             `json:"url"`
	Host   string             `json:"host,omitempty"`
	Status URLDiscoveryStatus `json:"status"`
	Reason string             `json:"reason,omitempty"`
}

type URLDiscoveryResult struct {
	Requested  int                `json:"requested"`
	Candidates int                `json:"candidates"`
	Rejected   int                `json:"rejected"`
	Skipped    int                `json:"skipped"`
	Items      []URLDiscoveryItem `json:"items,omitempty"`
	URLs       []string           `json:"urls,omitempty"`
}

type URLBatchSaveItem struct {
	URL      string             `json:"url"`
	SourceID string             `json:"source_id,omitempty"`
	Title    string             `json:"title,omitempty"`
	Status   URLBatchSaveStatus `json:"status"`
	Error    string             `json:"error,omitempty"`
}

type URLBatchSaveResult struct {
	Requested  int                `json:"requested"`
	Saved      int                `json:"saved"`
	Duplicates int                `json:"duplicates"`
	Failed     int                `json:"failed"`
	Skipped    int                `json:"skipped"`
	Items      []URLBatchSaveItem `json:"items,omitempty"`
	Sources    []Source           `json:"sources,omitempty"`
}

type URLDomainPolicy struct {
	Domain    string    `json:"domain"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type URLDomainPolicyUpdateRequest struct {
	AllowDomains []string `json:"allow_domains,omitempty"`
	BlockDomains []string `json:"block_domains,omitempty"`
	Replace      bool     `json:"replace,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

type URLDomainPolicyUpdateResult struct {
	Policies []URLDomainPolicy `json:"policies,omitempty"`
	Updated  int               `json:"updated"`
	Deleted  int               `json:"deleted,omitempty"`
}

type URLDomainPolicyCheck struct {
	URL           string           `json:"url,omitempty"`
	Host          string           `json:"host,omitempty"`
	Allowed       bool             `json:"allowed"`
	Reason        string           `json:"reason,omitempty"`
	MatchedPolicy *URLDomainPolicy `json:"matched_policy,omitempty"`
}

type TextSaveRequest struct {
	Text        string   `json:"text"`
	Title       string   `json:"title,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	OwnerID     string   `json:"owner_id,omitempty"`
	TenantID    string   `json:"tenant_id,omitempty"`
	ProjectPath string   `json:"project_path,omitempty"`
	TopicHint   string   `json:"topic_hint,omitempty"`
	SaveScope   string   `json:"save_scope,omitempty"`
	DistillMode string   `json:"distill_mode,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	AutoLabels  bool     `json:"auto_labels,omitempty"`
}

type SourceUpdateRequest struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	TopicHint   string   `json:"topic_hint,omitempty"`
	SourceTrust float64  `json:"source_trust,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

type SourceLabelUpdateRequest struct {
	SourceIDs     []string           `json:"source_ids,omitempty"`
	Filter        ListSourcesOptions `json:"filter,omitempty"`
	AddLabels     []string           `json:"add_labels,omitempty"`
	RemoveLabels  []string           `json:"remove_labels,omitempty"`
	ReplaceLabels []string           `json:"replace_labels,omitempty"`
	RenameFrom    string             `json:"rename_from,omitempty"`
	RenameTo      string             `json:"rename_to,omitempty"`
	ClearLabels   bool               `json:"clear_labels,omitempty"`
	DryRun        bool               `json:"dry_run,omitempty"`
	Limit         int                `json:"limit,omitempty"`
}

type SourceAutoLabelBackfillRequest struct {
	SourceIDs []string           `json:"source_ids,omitempty"`
	Filter    ListSourcesOptions `json:"filter,omitempty"`
	DryRun    bool               `json:"dry_run,omitempty"`
	Limit     int                `json:"limit,omitempty"`
}

type SourceLabelUpdateResult struct {
	Requested    int                     `json:"requested"`
	Updated      int                     `json:"updated"`
	Failed       int                     `json:"failed"`
	DryRun       bool                    `json:"dry_run,omitempty"`
	Mode         string                  `json:"mode,omitempty"`
	SourceIDs    []string                `json:"source_ids,omitempty"`
	Sources      []Source                `json:"sources,omitempty"`
	LabelChanges []SourceLabelChange     `json:"label_changes,omitempty"`
	Failures     []SourceLabelUpdateFail `json:"failures,omitempty"`
}

type SourceLabelChange struct {
	SourceID string   `json:"source_id"`
	Before   []string `json:"before,omitempty"`
	After    []string `json:"after,omitempty"`
}

type SourceLabelUpdateFail struct {
	SourceID string `json:"source_id"`
	Error    string `json:"error"`
}

type SearchOptions struct {
	Query           string   `json:"query"`
	OwnerID         string   `json:"owner_id,omitempty"`
	TenantID        string   `json:"tenant_id,omitempty"`
	ProjectPath     string   `json:"project_path,omitempty"`
	SearchScope     string   `json:"search_scope,omitempty"`
	TopicHint       string   `json:"topic_hint,omitempty"`
	ContextTerms    []string `json:"context_terms,omitempty"`
	ResultTypes     []string `json:"result_types,omitempty"`
	SourceKinds     []string `json:"source_kinds,omitempty"`
	SourceIDs       []string `json:"source_ids,omitempty"`
	Labels          []string `json:"labels,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	Entity          string   `json:"entity,omitempty"`
	Predicate       string   `json:"predicate,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	IncludeDisabled bool     `json:"include_disabled,omitempty"`
}

type SearchResult struct {
	Source     Source  `json:"source"`
	ResultType string  `json:"result_type,omitempty"`
	NodeID     string  `json:"node_id,omitempty"`
	NodeTitle  string  `json:"node_title,omitempty"`
	NodeType   string  `json:"node_type,omitempty"`
	Page       int     `json:"page,omitempty"`
	SheetName  string  `json:"sheet_name,omitempty"`
	RowRange   string  `json:"row_range,omitempty"`
	ColRange   string  `json:"col_range,omitempty"`
	Citation   string  `json:"citation,omitempty"`
	CardID     string  `json:"card_id,omitempty"`
	CardTitle  string  `json:"card_title,omitempty"`
	FactID     string  `json:"fact_id,omitempty"`
	Subject    string  `json:"subject,omitempty"`
	Predicate  string  `json:"predicate,omitempty"`
	Object     string  `json:"object,omitempty"`
	Claim      string  `json:"claim,omitempty"`
	Summary    string  `json:"summary,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

type TopicRelevanceSource struct {
	Source       Source   `json:"source"`
	Score        float64  `json:"score,omitempty"`
	MatchedTerms []string `json:"matched_terms,omitempty"`
	LabelMatches []string `json:"label_matches,omitempty"`
	SourceHits   int      `json:"source_hits,omitempty"`
	CardHits     int      `json:"card_hits,omitempty"`
	FactHits     int      `json:"fact_hits,omitempty"`
	NodeHits     int      `json:"node_hits,omitempty"`
}

type TopicRelevanceReport struct {
	TopicHint string                 `json:"topic_hint,omitempty"`
	Query     string                 `json:"query,omitempty"`
	Terms     []string               `json:"terms,omitempty"`
	Count     int                    `json:"count"`
	Sources   []TopicRelevanceSource `json:"sources,omitempty"`
	Notes     []string               `json:"notes,omitempty"`
}

type SourceLink struct {
	SourceID        string    `json:"source_id"`
	RelatedSourceID string    `json:"related_source_id"`
	Relation        string    `json:"relation"`
	Score           float64   `json:"score,omitempty"`
	Terms           []string  `json:"terms,omitempty"`
	Evidence        []string  `json:"evidence,omitempty"`
	RelatedSource   Source    `json:"related_source,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SourceTopicLinkBuildResult struct {
	SourceID   string       `json:"source_id"`
	Scanned    int          `json:"scanned"`
	Candidates int          `json:"candidates,omitempty"`
	Linked     int          `json:"linked"`
	Skipped    int          `json:"skipped,omitempty"`
	Links      []SourceLink `json:"links,omitempty"`
	Notes      []string     `json:"notes,omitempty"`
}

type SourceUnlinkResult struct {
	SourceID        string   `json:"source_id"`
	RelatedSourceID string   `json:"related_source_id"`
	Relation        string   `json:"relation"`
	Deleted         int      `json:"deleted"`
	Notes           []string `json:"notes,omitempty"`
}

type SourceLinkEvent struct {
	ID              string    `json:"id"`
	SourceID        string    `json:"source_id"`
	RelatedSourceID string    `json:"related_source_id"`
	Relation        string    `json:"relation"`
	Action          string    `json:"action"`
	Score           float64   `json:"score,omitempty"`
	Terms           []string  `json:"terms,omitempty"`
	Evidence        []string  `json:"evidence,omitempty"`
	Note            string    `json:"note,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type SourceGraphNode struct {
	ID           string   `json:"id"`
	Label        string   `json:"label,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Status       string   `json:"status,omitempty"`
	TopicHint    string   `json:"topic_hint,omitempty"`
	ProjectPath  string   `json:"project_path,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	NodeCount    int      `json:"node_count,omitempty"`
	CardCount    int      `json:"card_count,omitempty"`
	FactCount    int      `json:"fact_count,omitempty"`
	Degree       int      `json:"degree,omitempty"`
	ComponentID  int      `json:"component_id,omitempty"`
	SourceTrust  float64  `json:"source_trust,omitempty"`
	RelativePath string   `json:"relative_path,omitempty"`
	URI          string   `json:"uri,omitempty"`
}

type SourceGraphEdge struct {
	ID              string   `json:"id"`
	SourceID        string   `json:"source_id"`
	RelatedSourceID string   `json:"related_source_id"`
	Relation        string   `json:"relation"`
	Score           float64  `json:"score,omitempty"`
	Terms           []string `json:"terms,omitempty"`
	Evidence        []string `json:"evidence,omitempty"`
}

type SourceGraphComponent struct {
	ID            int      `json:"id"`
	Count         int      `json:"count"`
	EdgeCount     int      `json:"edge_count"`
	Density       float64  `json:"density,omitempty"`
	AverageDegree float64  `json:"average_degree,omitempty"`
	TopNodeIDs    []string `json:"top_node_ids,omitempty"`
	TopLabels     []string `json:"top_labels,omitempty"`
	Terms         []string `json:"terms,omitempty"`
	Isolated      bool     `json:"isolated,omitempty"`
}

type SourceGraphResult struct {
	Count                int                    `json:"count"`
	EdgeCount            int                    `json:"edge_count"`
	FocusSourceID        string                 `json:"focus_source_id,omitempty"`
	Depth                int                    `json:"depth,omitempty"`
	ComponentCount       int                    `json:"component_count,omitempty"`
	LargestComponentSize int                    `json:"largest_component_size,omitempty"`
	Density              float64                `json:"density,omitempty"`
	Nodes                []SourceGraphNode      `json:"nodes,omitempty"`
	Edges                []SourceGraphEdge      `json:"edges,omitempty"`
	Components           []SourceGraphComponent `json:"components,omitempty"`
	Isolates             []SourceGraphNode      `json:"isolates,omitempty"`
	Notes                []string               `json:"notes,omitempty"`
}

type SourcePathStep struct {
	FromSourceID string   `json:"from_source_id"`
	ToSourceID   string   `json:"to_source_id"`
	Relation     string   `json:"relation"`
	Score        float64  `json:"score,omitempty"`
	Terms        []string `json:"terms,omitempty"`
	Evidence     []string `json:"evidence,omitempty"`
}

type SourcePathResult struct {
	FromSourceID      string            `json:"from_source_id"`
	ToSourceID        string            `json:"to_source_id"`
	Found             bool              `json:"found"`
	HopCount          int               `json:"hop_count,omitempty"`
	MaxDepth          int               `json:"max_depth"`
	VisitedCount      int               `json:"visited_count,omitempty"`
	SearchedEdgeCount int               `json:"searched_edge_count,omitempty"`
	Truncated         bool              `json:"truncated,omitempty"`
	Nodes             []SourceGraphNode `json:"nodes,omitempty"`
	Steps             []SourcePathStep  `json:"steps,omitempty"`
	Notes             []string          `json:"notes,omitempty"`
}

type Citation struct {
	Label        string  `json:"label"`
	SourceID     string  `json:"source_id,omitempty"`
	SourceTitle  string  `json:"source_title,omitempty"`
	SourceKind   string  `json:"source_kind,omitempty"`
	URI          string  `json:"uri,omitempty"`
	RelativePath string  `json:"relative_path,omitempty"`
	ResultType   string  `json:"result_type,omitempty"`
	NodeID       string  `json:"node_id,omitempty"`
	CardID       string  `json:"card_id,omitempty"`
	FactID       string  `json:"fact_id,omitempty"`
	Page         int     `json:"page,omitempty"`
	SheetName    string  `json:"sheet_name,omitempty"`
	RowRange     string  `json:"row_range,omitempty"`
	ColRange     string  `json:"col_range,omitempty"`
	Snippet      string  `json:"snippet,omitempty"`
	Score        float64 `json:"score,omitempty"`
}

type ExplainResult struct {
	Query     string         `json:"query"`
	Count     int            `json:"count"`
	Results   []SearchResult `json:"results"`
	Citations []Citation     `json:"citations"`
	Notes     []string       `json:"notes,omitempty"`
}

type SearchFacetBucket struct {
	Label      string   `json:"label"`
	Kind       string   `json:"kind,omitempty"`
	Count      int      `json:"count"`
	SourceID   string   `json:"source_id,omitempty"`
	SourceKind string   `json:"source_kind,omitempty"`
	Domain     string   `json:"domain,omitempty"`
	Examples   []string `json:"examples,omitempty"`
}

type SearchFacetsResult struct {
	Query       string              `json:"query"`
	Count       int                 `json:"count"`
	ResultTypes []SearchFacetBucket `json:"result_types,omitempty"`
	SourceKinds []SearchFacetBucket `json:"source_kinds,omitempty"`
	Domains     []SearchFacetBucket `json:"domains,omitempty"`
	Labels      []SearchFacetBucket `json:"labels,omitempty"`
	Sources     []SearchFacetBucket `json:"sources,omitempty"`
	Entities    []SearchFacetBucket `json:"entities,omitempty"`
	Predicates  []SearchFacetBucket `json:"predicates,omitempty"`
	Notes       []string            `json:"notes,omitempty"`
}

type ContextPackOptions struct {
	SearchOptions
	MaxItems int `json:"max_items,omitempty"`
	MaxChars int `json:"max_chars,omitempty"`
}

type ContextPackItem struct {
	Label      string  `json:"label"`
	ResultType string  `json:"result_type,omitempty"`
	Title      string  `json:"title,omitempty"`
	Text       string  `json:"text,omitempty"`
	SourceID   string  `json:"source_id,omitempty"`
	Citation   string  `json:"citation,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

type ContextPackResult struct {
	Query          string            `json:"query"`
	Count          int               `json:"count"`
	CharacterCount int               `json:"character_count"`
	Items          []ContextPackItem `json:"items"`
	Citations      []Citation        `json:"citations"`
	Notes          []string          `json:"notes,omitempty"`
}

type Stats struct {
	Sources             int            `json:"sources"`
	DocumentNodes       int            `json:"document_nodes"`
	Cards               int            `json:"cards"`
	Facts               int            `json:"facts"`
	SourceLinks         int            `json:"source_links,omitempty"`
	SourceLinkEvents    int            `json:"source_link_events,omitempty"`
	Batches             int            `json:"batches"`
	SourcesWithoutNodes int            `json:"sources_without_nodes,omitempty"`
	SourcesWithoutCards int            `json:"sources_without_cards,omitempty"`
	SourcesWithoutFacts int            `json:"sources_without_facts,omitempty"`
	SourcesRebuildCards int            `json:"sources_rebuild_cards,omitempty"`
	SourcesRebuildFacts int            `json:"sources_rebuild_facts,omitempty"`
	SourcesWithoutLinks int            `json:"sources_without_links,omitempty"`
	SourcesByKind       map[string]int `json:"sources_by_kind,omitempty"`
	SourcesByStatus     map[string]int `json:"sources_by_status,omitempty"`
	SourcesByDomain     map[string]int `json:"sources_by_domain,omitempty"`
	SourcesByLabel      map[string]int `json:"sources_by_label,omitempty"`
	LinkEventsByAction  map[string]int `json:"link_events_by_action,omitempty"`
	BatchesByStatus     map[string]int `json:"batches_by_status,omitempty"`
	ImportItemsByStatus map[string]int `json:"import_items_by_status,omitempty"`
}

type DoctorFinding struct {
	Severity  string              `json:"severity"`
	Code      string              `json:"code"`
	Title     string              `json:"title"`
	Detail    string              `json:"detail,omitempty"`
	Count     int                 `json:"count,omitempty"`
	Action    string              `json:"action,omitempty"`
	SourceIDs []string            `json:"source_ids,omitempty"`
	Examples  []string            `json:"examples,omitempty"`
	Filter    *ListSourcesOptions `json:"filter,omitempty"`
}

type DoctorResult struct {
	Status      string          `json:"status"`
	Score       int             `json:"score"`
	Stats       Stats           `json:"stats"`
	Findings    []DoctorFinding `json:"findings,omitempty"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type FormatCapability struct {
	Kind          string   `json:"kind"`
	Extensions    []string `json:"extensions,omitempty"`
	Parser        string   `json:"parser"`
	SearchUnit    string   `json:"search_unit,omitempty"`
	Status        string   `json:"status"`
	Refreshable   bool     `json:"refreshable"`
	DefaultImport bool     `json:"default_import"`
	Notes         string   `json:"notes,omitempty"`
}

type KnowledgeCapabilities struct {
	DefaultIncludeExts []string           `json:"default_include_exts,omitempty"`
	DefaultAutoLabels  bool               `json:"default_auto_labels"`
	AutoLabelRules     []string           `json:"auto_label_rules,omitempty"`
	Formats            []FormatCapability `json:"formats,omitempty"`
	QueryRequiresLLM   bool               `json:"query_requires_llm"`
	WriteLLMOptional   bool               `json:"write_llm_optional"`
	DistillModes       []string           `json:"distill_modes,omitempty"`
	CoverageFilters    []string           `json:"coverage_filters,omitempty"`
	CoverageAliases    map[string]string  `json:"coverage_aliases,omitempty"`
	LocalIndexes       []string           `json:"local_indexes,omitempty"`
	StorageBackend     string             `json:"storage_backend,omitempty"`
	SearchBackend      string             `json:"search_backend,omitempty"`
	GeneratedAt        time.Time          `json:"generated_at"`
}

type MaintenanceResult struct {
	IntegrityOK    bool      `json:"integrity_ok"`
	IntegrityCheck string    `json:"integrity_check,omitempty"`
	OptimizedFTS   []string  `json:"optimized_fts,omitempty"`
	Checkpointed   bool      `json:"checkpointed"`
	Vacuumed       bool      `json:"vacuumed"`
	Warnings       []string  `json:"warnings,omitempty"`
	Errors         []string  `json:"errors,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at"`
}

type ExportOptions struct {
	OutputPath      string   `json:"output_path,omitempty"`
	RedactSensitive bool     `json:"redact_sensitive"`
	SourceIDs       []string `json:"source_ids,omitempty"`
}

type ExportResult struct {
	OutputPath       string    `json:"output_path"`
	Format           string    `json:"format"`
	RedactSensitive  bool      `json:"redact_sensitive"`
	Scoped           bool      `json:"scoped,omitempty"`
	SourceIDs        []string  `json:"source_ids,omitempty"`
	URLPolicies      int       `json:"url_policies,omitempty"`
	Sources          int       `json:"sources"`
	SourceLabels     int       `json:"source_labels,omitempty"`
	SourceVersions   int       `json:"source_versions,omitempty"`
	SourceLinks      int       `json:"source_links,omitempty"`
	SourceLinkEvents int       `json:"source_link_events,omitempty"`
	Nodes            int       `json:"nodes"`
	Cards            int       `json:"cards"`
	Facts            int       `json:"facts"`
	Bytes            int64     `json:"bytes,omitempty"`
	GeneratedAt      time.Time `json:"generated_at"`
}

type SnapshotImportOptions struct {
	InputPath          string `json:"input_path"`
	DryRun             bool   `json:"dry_run,omitempty"`
	Overwrite          bool   `json:"overwrite,omitempty"`
	SkipSafetyBackup   bool   `json:"skip_safety_backup,omitempty"`
	SafetyBackupPath   string `json:"safety_backup_path,omitempty"`
	SafetyBackupRedact bool   `json:"safety_backup_redact,omitempty"`
}

type SnapshotImportFailure struct {
	Line  int    `json:"line,omitempty"`
	Type  string `json:"type,omitempty"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error"`
}

type SnapshotImportConflict struct {
	Line int    `json:"line,omitempty"`
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

type SnapshotImportResult struct {
	InputPath         string                   `json:"input_path"`
	DryRun            bool                     `json:"dry_run,omitempty"`
	Overwrite         bool                     `json:"overwrite,omitempty"`
	SafetyBackupPath  string                   `json:"safety_backup_path,omitempty"`
	SafetyBackup      *ExportResult            `json:"safety_backup,omitempty"`
	Records           int                      `json:"records"`
	WouldImport       int                      `json:"would_import,omitempty"`
	Imported          int                      `json:"imported"`
	Skipped           int                      `json:"skipped"`
	Conflicts         int                      `json:"conflicts,omitempty"`
	UnknownRecords    int                      `json:"unknown_records,omitempty"`
	MissingReferences int                      `json:"missing_references,omitempty"`
	Failed            int                      `json:"failed"`
	URLPolicies       int                      `json:"url_policies,omitempty"`
	Sources           int                      `json:"sources"`
	SourceLabels      int                      `json:"source_labels,omitempty"`
	SourceVersions    int                      `json:"source_versions,omitempty"`
	SourceLinks       int                      `json:"source_links,omitempty"`
	SourceLinkEvents  int                      `json:"source_link_events,omitempty"`
	Nodes             int                      `json:"nodes"`
	Cards             int                      `json:"cards"`
	Facts             int                      `json:"facts"`
	ConflictItems     []SnapshotImportConflict `json:"conflict_items,omitempty"`
	Failures          []SnapshotImportFailure  `json:"failures,omitempty"`
	StartedAt         time.Time                `json:"started_at"`
	CompletedAt       time.Time                `json:"completed_at"`
}

type SourceRefreshFailure struct {
	SourceID string `json:"source_id"`
	Error    string `json:"error"`
}

type SourceRefreshResult struct {
	Requested int                    `json:"requested"`
	Refreshed int                    `json:"refreshed"`
	Failed    int                    `json:"failed"`
	Sources   []Source               `json:"sources,omitempty"`
	Failures  []SourceRefreshFailure `json:"failures,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
}

type SourceRebuildFailure struct {
	SourceID string `json:"source_id"`
	Error    string `json:"error"`
}

type SourceRebuildResult struct {
	Requested int                    `json:"requested"`
	Rebuilt   int                    `json:"rebuilt"`
	Failed    int                    `json:"failed"`
	Sources   []Source               `json:"sources,omitempty"`
	Failures  []SourceRebuildFailure `json:"failures,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
}

type SourceChangeSample struct {
	Kind    string `json:"kind"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type SourceChangePreview struct {
	SourceID        string               `json:"source_id"`
	Source          Source               `json:"source"`
	NextSource      Source               `json:"next_source,omitempty"`
	Refreshable     bool                 `json:"refreshable"`
	Changed         bool                 `json:"changed"`
	HashChanged     bool                 `json:"hash_changed"`
	RequiresRefresh bool                 `json:"requires_refresh"`
	OldHash         string               `json:"old_hash,omitempty"`
	NewHash         string               `json:"new_hash,omitempty"`
	OldStatus       string               `json:"old_status,omitempty"`
	NewStatus       string               `json:"new_status,omitempty"`
	OldNodeCount    int                  `json:"old_node_count"`
	NewNodeCount    int                  `json:"new_node_count"`
	AddedNodes      int                  `json:"added_nodes,omitempty"`
	RemovedNodes    int                  `json:"removed_nodes,omitempty"`
	UnchangedNodes  int                  `json:"unchanged_nodes,omitempty"`
	Error           string               `json:"error,omitempty"`
	Samples         []SourceChangeSample `json:"samples,omitempty"`
	GeneratedAt     time.Time            `json:"generated_at"`
}

type SourceChangePreviewFailure struct {
	SourceID string `json:"source_id"`
	Error    string `json:"error"`
}

type SourceChangePreviewResult struct {
	Requested int                          `json:"requested"`
	Changed   int                          `json:"changed"`
	Unchanged int                          `json:"unchanged"`
	Failed    int                          `json:"failed"`
	Previews  []SourceChangePreview        `json:"previews,omitempty"`
	Failures  []SourceChangePreviewFailure `json:"failures,omitempty"`
}

type ChangedSourceRefreshResult struct {
	Preview   SourceChangePreviewResult `json:"preview"`
	Refresh   SourceRefreshResult       `json:"refresh"`
	SourceIDs []string                  `json:"source_ids,omitempty"`
}

type SourceStatusUpdateFailure struct {
	SourceID string `json:"source_id"`
	Error    string `json:"error"`
}

type SourceStatusUpdateResult struct {
	Requested int                         `json:"requested"`
	Updated   int                         `json:"updated"`
	Failed    int                         `json:"failed"`
	Status    string                      `json:"status,omitempty"`
	Sources   []Source                    `json:"sources,omitempty"`
	Failures  []SourceStatusUpdateFailure `json:"failures,omitempty"`
}
