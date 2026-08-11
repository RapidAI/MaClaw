package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Coding Experience Knowledge Store
//
// Independent SQLite-backed store for coding experiences accumulated by
// CodingSubAgent. Uses a separate database file (coding_knowledge.db) to
// avoid polluting the general-purpose knowledge base.
//
// The store wraps an inner SQLiteStore and exposes a CodingExperience-oriented
// API. Under the hood, each experience is stored as a Source (kind=text) with
// structured JSON metadata in TopicHint, plus derived Cards/Facts via the
// standard distill pipeline.
// ---------------------------------------------------------------------------

// Scope constants for coding experiences.
const (
	CodingScopeUniversal = "universal" // Language-agnostic experience
	CodingScopeLanguage  = "language"  // Language-specific experience
	CodingScopeProject   = "project"   // Project-specific experience
)

// Category constants for coding experiences.
const (
	CodingCategoryPattern    = "pattern"    // Solution patterns / algorithms
	CodingCategoryDecision   = "decision"   // Technology selection decisions
	CodingCategoryPitfall    = "pitfall"    // Traps / anti-patterns
	CodingCategoryConvention = "convention" // Project or team conventions
)

// Status constants for coding experiences.
const (
	CodingStatusCandidate  = "candidate"  // Awaiting user confirmation
	CodingStatusActive     = "active"     // Participates in auto-recall
	CodingStatusVerified   = "verified"   // High confidence, frequently validated
	CodingStatusDeprecated = "deprecated" // Low confidence, auto-retired
)

// Confidence tuning constants.
const (
	CodingConfidenceInitial             = 1.0
	CodingConfidenceMax                 = 2.0
	CodingConfidenceMin                 = 0.0
	CodingConfidenceSuccessBoost        = 0.15
	CodingConfidenceFailurePenalty      = 0.25
	CodingConfidenceVerifiedThreshold   = 1.5
	CodingConfidenceDeprecatedThreshold = 0.3
	CodingMinRecallsForVerified         = 5
)

// CodingExperienceBudget is the project-level retention budget for reviewed
// guidance. It is enforced at candidate confirmation, before an experience can
// enter automatic recall. The store keeps it host-configurable because hosts
// own the configuration surface, but all counting and token estimation stays
// here so GUI, TUI and service hosts cannot reinterpret the rule.
type CodingExperienceBudget struct {
	MaxVerifiedCount  int
	MaxVerifiedTokens int
}

// CodingExperience is the primary data structure for coding knowledge entries.
type CodingExperience struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Category          string   `json:"category"`           // pattern/decision/pitfall/convention
	Scope             string   `json:"scope"`              // universal/language/project
	Language          string   `json:"language,omitempty"` // Required when Scope=language
	Frameworks        []string `json:"frameworks,omitempty"`
	TriggerCondition  string   `json:"trigger_condition"` // Short keyword combo for recall matching
	Content           string   `json:"content"`           // Full experience (problem + solution + context)
	CodeSnippet       string   `json:"code_snippet,omitempty"`
	FailedAttempts    []string `json:"failed_attempts,omitempty"`
	Contraindications []string `json:"contraindications,omitempty"` // Scenarios where this does NOT apply

	// Metadata
	Labels          []string `json:"labels,omitempty"`
	ProjectPath     string   `json:"project_path,omitempty"` // Required when Scope=project
	SourceTaskTitle string   `json:"source_task_title,omitempty"`
	LanguageVersion string   `json:"language_version,omitempty"` // e.g. "go1.21"
	ValidUntil      string   `json:"valid_until,omitempty"`      // e.g. "go1.22" means deprecated after 1.22
	// Provenance binds an automatically extracted experience to a durable
	// coding-runtime execution fact. It is deliberately opaque: prompts, raw
	// commands and transcripts are never copied into the knowledge store.
	SourceRuntimeTaskID    string                           `json:"source_runtime_task_id,omitempty"`
	SourceRuntimeAttemptID string                           `json:"source_runtime_attempt_id,omitempty"`
	EvidenceDigest         string                           `json:"evidence_digest,omitempty"`
	ParentExperienceID     string                           `json:"parent_experience_id,omitempty"`
	LifecycleEvents        []CodingExperienceLifecycleEvent `json:"lifecycle_events,omitempty"`
	// CreatedBy is a bounded origin label (manual/runtime/import/revision), not
	// an identity credential. It makes the review queue explainable without
	// copying a user prompt or executor transcript into the knowledge store.
	CreatedBy      string    `json:"created_by,omitempty"`
	LastReviewedAt time.Time `json:"last_reviewed_at,omitempty"`

	// Confidence & statistics
	Confidence   float64 `json:"confidence"`
	RecallCount  int     `json:"recall_count"`
	SuccessCount int     `json:"success_count"`
	FailureCount int     `json:"failure_count"`

	// Lifecycle
	Status         string    `json:"status"` // candidate/active/verified/deprecated
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastRecalledAt time.Time `json:"last_recalled_at,omitempty"`
}

// CodingExperienceMetadata is the structured metadata stored in Source.TopicHint as JSON.
// This allows the standard SQLiteStore to persist all experience-specific fields
// without schema changes.
type CodingExperienceMetadata struct {
	Category               string                           `json:"category"`
	Scope                  string                           `json:"scope"`
	Language               string                           `json:"language,omitempty"`
	Frameworks             []string                         `json:"frameworks,omitempty"`
	TriggerCondition       string                           `json:"trigger_condition"`
	CodeSnippet            string                           `json:"code_snippet,omitempty"`
	FailedAttempts         []string                         `json:"failed_attempts,omitempty"`
	Contraindications      []string                         `json:"contraindications,omitempty"`
	SourceTaskTitle        string                           `json:"source_task_title,omitempty"`
	LanguageVersion        string                           `json:"language_version,omitempty"`
	ValidUntil             string                           `json:"valid_until,omitempty"`
	SourceRuntimeTaskID    string                           `json:"source_runtime_task_id,omitempty"`
	SourceRuntimeAttemptID string                           `json:"source_runtime_attempt_id,omitempty"`
	EvidenceDigest         string                           `json:"evidence_digest,omitempty"`
	ParentExperienceID     string                           `json:"parent_experience_id,omitempty"`
	LifecycleEvents        []CodingExperienceLifecycleEvent `json:"lifecycle_events,omitempty"`
	CreatedBy              string                           `json:"created_by,omitempty"`
	LastReviewedAt         string                           `json:"last_reviewed_at,omitempty"`
	Confidence             float64                          `json:"confidence"`
	RecallCount            int                              `json:"recall_count"`
	SuccessCount           int                              `json:"success_count"`
	FailureCount           int                              `json:"failure_count"`
	Status                 string                           `json:"status"`
	LastRecalledAt         string                           `json:"last_recalled_at,omitempty"`
}

// CodingExperienceLifecycleEvent is a compact, append-only audit entry for a
// knowledge lifecycle decision. It intentionally stores only an operation,
// bounded opaque related ID, and a short reason: Runtime evidence itself stays
// in the execution Ledger.
type CodingExperienceLifecycleEvent struct {
	Action     string    `json:"action"`
	Reason     string    `json:"reason,omitempty"`
	RelatedID  string    `json:"related_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// CodingKnowledgeStore wraps a dedicated SQLiteStore for coding experiences.
type CodingKnowledgeStore struct {
	inner            *SQLiteStore
	lifecycleWriteMu sync.Mutex // serializes read-modify-write lifecycle evidence updates
}

// RuntimeProvenanceVerifier revalidates the opaque Runtime reference carried
// by an automatically extracted experience before it becomes active. The
// knowledge package deliberately does not import codingruntime: hosts own the
// ledger connection and supply this narrow verifier at review time.
type RuntimeProvenanceVerifier func(context.Context, CodingExperience) error

// RecallEvidenceVerifier verifies the immutable Runtime outcome that is being
// used to adjust an experience's confidence. The knowledge package keeps this
// callback host-owned so it does not depend on codingruntime, while every
// production caller can still bind recall evidence to the durable Ledger.
type RecallEvidenceVerifier func(context.Context, RecallOutcome) error

// RecallOutcome is one bounded, idempotent evidence unit for a recalled
// experience. RuntimeTaskID/RuntimeAttemptID/EvidenceDigest must identify the
// task outcome that judged the recalled guidance useful or harmful.
type RecallOutcome struct {
	RuntimeTaskID    string `json:"runtime_task_id"`
	RuntimeAttemptID string `json:"runtime_attempt_id"`
	EvidenceDigest   string `json:"evidence_digest"`
	TaskSucceeded    bool   `json:"task_succeeded"`
}

// NewCodingKnowledgeStore opens (or creates) the coding knowledge database.
func NewCodingKnowledgeStore(dbPath string) (*CodingKnowledgeStore, error) {
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("coding knowledge store: %w", err)
	}
	return &CodingKnowledgeStore{inner: store}, nil
}

// Close releases the database connection.
func (s *CodingKnowledgeStore) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

// Inner returns the underlying SQLiteStore for advanced operations.
// Use sparingly — prefer the CodingExperience-level API.
func (s *CodingKnowledgeStore) Inner() *SQLiteStore {
	if s == nil {
		return nil
	}
	return s.inner
}

// SetCardDistiller configures the LLM distiller for card extraction.
func (s *CodingKnowledgeStore) SetCardDistiller(distiller CardDistiller) {
	if s != nil && s.inner != nil {
		s.inner.SetCardDistiller(distiller)
	}
}

// ---------------------------------------------------------------------------
// Write operations
// ---------------------------------------------------------------------------

// SaveExperience persists a manually authored coding experience. Origin is
// assigned by the store rather than accepted from the caller, so a UI payload
// cannot claim Runtime/import/revision authority.
func (s *CodingKnowledgeStore) SaveExperience(ctx context.Context, exp CodingExperience) (CodingExperience, error) {
	return s.SaveExperienceWithBudget(ctx, exp, CodingExperienceBudget{})
}

// SaveExperienceWithBudget persists manually authored guidance while enforcing
// the same reviewed-project retention boundary used by candidate confirmation.
// A human reviewer may intentionally create an active manual record, but that
// must not become a side door around the active/verified count and token
// budgets. A zero budget preserves the legacy unrestricted API for hosts that
// have not yet supplied their configuration.
func (s *CodingKnowledgeStore) SaveExperienceWithBudget(ctx context.Context, exp CodingExperience, budget CodingExperienceBudget) (CodingExperience, error) {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	if isRuntimeDerivedExperience(exp) {
		return CodingExperience{}, fmt.Errorf("coding knowledge: runtime-derived experience must use the runtime save operation")
	}
	if !exp.LastReviewedAt.IsZero() {
		return CodingExperience{}, fmt.Errorf("coding knowledge: review timestamp is assigned by the store")
	}
	// Verified is evidence-derived, not an author-selected initial state. A
	// manually authored record may be explicitly saved as active by a reviewer,
	// but it must earn verified status through the bounded recall-evidence path.
	if exp.Status == CodingStatusVerified {
		return CodingExperience{}, fmt.Errorf("coding knowledge: verified status requires candidate confirmation and recall evidence")
	}
	if exp.Status == CodingStatusActive {
		if err := s.validateExperiencePromotionBudget(ctx, exp, budget); err != nil {
			return CodingExperience{}, err
		}
	}
	return s.saveExperience(ctx, exp, false, "manual")
}

// SaveRuntimeExperience persists an automatic Runtime-derived candidate. The
// immutable provenance fields are validated by the common store path, while
// the origin is assigned here so host adapters cannot accidentally label an
// arbitrary manual record as Runtime evidence.
func (s *CodingKnowledgeStore) SaveRuntimeExperience(ctx context.Context, exp CodingExperience) (CodingExperience, error) {
	if !isRuntimeDerivedExperience(exp) {
		return CodingExperience{}, fmt.Errorf("coding knowledge: runtime save requires task, attempt and evidence digest")
	}
	if !exp.LastReviewedAt.IsZero() {
		return CodingExperience{}, fmt.Errorf("coding knowledge: review timestamp is assigned by candidate confirmation")
	}
	exp.Status = CodingStatusCandidate
	return s.saveExperience(ctx, exp, false, "runtime")
}

// SaveImportedExperience stages an external record for local review. Nothing
// that represents foreign authority or usage evidence may cross the boundary:
// provenance, lineage, lifecycle audit, review state and recall statistics are
// all local facts and are reset before persistence.
func (s *CodingKnowledgeStore) SaveImportedExperience(ctx context.Context, exp CodingExperience) (CodingExperience, error) {
	exp.Status = CodingStatusCandidate
	exp.CreatedBy = "import"
	exp.SourceRuntimeTaskID = ""
	exp.SourceRuntimeAttemptID = ""
	exp.EvidenceDigest = ""
	exp.ParentExperienceID = ""
	exp.LifecycleEvents = nil
	exp.LastReviewedAt = time.Time{}
	exp.Confidence = CodingConfidenceInitial
	exp.RecallCount = 0
	exp.SuccessCount = 0
	exp.FailureCount = 0
	exp.LastRecalledAt = time.Time{}
	exp.CreatedAt = time.Time{}
	exp.UpdatedAt = time.Time{}
	return s.saveExperience(ctx, exp, false, "import")
}

func (s *CodingKnowledgeStore) saveExperience(ctx context.Context, exp CodingExperience, allowParent bool, requiredOrigin string) (CodingExperience, error) {
	if strings.TrimSpace(exp.ParentExperienceID) != "" && !allowParent {
		return CodingExperience{}, fmt.Errorf("coding knowledge: parent experience can only be set by a revision operation")
	}
	if err := validateExperience(exp); err != nil {
		return CodingExperience{}, err
	}

	// Set defaults
	if exp.Confidence == 0 {
		exp.Confidence = CodingConfidenceInitial
	}
	// Runtime-derived knowledge is always staged. Auto-saving directly to
	// active would make an LLM extraction globally prompt-injectable without a
	// reviewer ever inspecting its execution provenance.
	if exp.SourceRuntimeTaskID != "" || exp.SourceRuntimeAttemptID != "" || exp.EvidenceDigest != "" {
		if strings.TrimSpace(exp.SourceRuntimeTaskID) == "" || strings.TrimSpace(exp.SourceRuntimeAttemptID) == "" || strings.TrimSpace(exp.EvidenceDigest) == "" {
			return CodingExperience{}, fmt.Errorf("coding knowledge: runtime-derived experience requires task, attempt and evidence digest")
		}
		exp.Status = CodingStatusCandidate
	} else if exp.Status == "" {
		exp.Status = CodingStatusCandidate
	}
	requiredOrigin = strings.TrimSpace(requiredOrigin)
	if requiredOrigin != "" {
		if strings.TrimSpace(exp.CreatedBy) != "" && exp.CreatedBy != requiredOrigin {
			return CodingExperience{}, fmt.Errorf("coding knowledge: creator origin must be %q for this operation", requiredOrigin)
		}
		exp.CreatedBy = requiredOrigin
	} else if strings.TrimSpace(exp.CreatedBy) == "" {
		exp.CreatedBy = "manual"
	}
	now := time.Now().UTC()
	if requiredOrigin == "manual" && (exp.Status == CodingStatusActive || exp.Status == CodingStatusVerified) {
		// A manual authoring action is itself a local review. The timestamp is
		// assigned here rather than accepted from a UI payload.
		exp.LastReviewedAt = now
	}
	if exp.CreatedAt.IsZero() {
		exp.CreatedAt = now
	}
	exp.UpdatedAt = now

	// Build the text content that will be indexed by FTS
	indexText := buildExperienceIndexText(exp)

	// Encode metadata as JSON in TopicHint
	meta := experienceToMetadata(exp)
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return CodingExperience{}, fmt.Errorf("coding knowledge: marshal metadata: %w", err)
	}

	// Build labels for the source
	labels := buildExperienceLabels(exp)

	source, err := s.inner.SaveText(ctx, TextSaveRequest{
		Text:        indexText,
		Title:       exp.Title,
		Kind:        SourceKindText,
		TopicHint:   string(metaJSON),
		ProjectPath: exp.ProjectPath,
		Labels:      labels,
		DistillMode: "off", // Don't distill coding experiences into cards (we use raw text search)
	})
	if err != nil {
		return CodingExperience{}, fmt.Errorf("coding knowledge: save: %w", err)
	}

	exp.ID = source.ID
	return exp, nil
}

// UpdateConfidence is deprecated and deliberately refuses bare boolean
// outcomes. Production code must call RecordRecallOutcome with durable Runtime
// evidence; accepting this value at an application boundary would let a caller
// mint verified status without an auditable task outcome.
func (s *CodingKnowledgeStore) UpdateConfidence(ctx context.Context, id string, taskSucceeded bool) error {
	return fmt.Errorf("coding knowledge: bare confidence update is not allowed; record a verified runtime recall outcome instead")
}

// RecordRecallOutcome records one verified Runtime result for an active or
// verified experience. The Runtime attempt/digest pair is single-use per
// experience, so retries or duplicate callbacks cannot inflate confidence.
func (s *CodingKnowledgeStore) RecordRecallOutcome(ctx context.Context, id string, outcome RecallOutcome, verify RecallEvidenceVerifier) error {
	outcome.RuntimeTaskID = strings.TrimSpace(outcome.RuntimeTaskID)
	outcome.RuntimeAttemptID = strings.TrimSpace(outcome.RuntimeAttemptID)
	outcome.EvidenceDigest = strings.TrimSpace(outcome.EvidenceDigest)
	if outcome.RuntimeTaskID == "" || outcome.RuntimeAttemptID == "" || outcome.EvidenceDigest == "" {
		return fmt.Errorf("coding knowledge: recall outcome requires task, attempt and evidence digest")
	}
	if verify == nil {
		return fmt.Errorf("coding knowledge: recall outcome requires runtime evidence verification")
	}
	if err := verify(ctx, outcome); err != nil {
		return fmt.Errorf("coding knowledge: recall outcome verification failed: %w", err)
	}
	return s.updateConfidence(ctx, id, outcome.TaskSucceeded, recallOutcomeRelatedID(outcome))
}

func (s *CodingKnowledgeStore) updateConfidence(ctx context.Context, id string, taskSucceeded bool, recallEvidenceID string) error {
	// updateExperienceMetadata stores a whole metadata document. Serialize this
	// read-modify-write path so two callbacks cannot both observe the same
	// missing evidence ID and then overwrite each other's audit/stat update.
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	// Retired/conflicted experience is audit evidence, not a live rule. It must
	// not collect new recall evidence that could later make the historical entry
	// look validated or accidentally revive it through status derivation.
	if exp.Status == CodingStatusDeprecated {
		return fmt.Errorf("coding knowledge: cannot update confidence for deprecated experience %s; create a revision candidate instead", id)
	}
	if recallEvidenceID != "" {
		if exp.Status != CodingStatusActive && exp.Status != CodingStatusVerified {
			return fmt.Errorf("coding knowledge: recall outcome requires a reviewed active or verified experience %s (status=%s)", id, exp.Status)
		}
	}

	exp.RecallCount++
	exp.LastRecalledAt = time.Now().UTC()

	if taskSucceeded {
		exp.SuccessCount++
		exp.Confidence += CodingConfidenceSuccessBoost
		if exp.Confidence > CodingConfidenceMax {
			exp.Confidence = CodingConfidenceMax
		}
	} else {
		exp.FailureCount++
		exp.Confidence -= CodingConfidenceFailurePenalty
		if exp.Confidence < CodingConfidenceMin {
			exp.Confidence = CodingConfidenceMin
		}
	}

	// Candidate records have not passed human review and are never eligible for
	// confidence-driven promotion. Only an explicitly confirmed active record
	// can earn verified status through recall evidence.
	if exp.Status == CodingStatusActive && exp.Confidence >= CodingConfidenceVerifiedThreshold && exp.RecallCount >= CodingMinRecallsForVerified {
		exp.Status = CodingStatusVerified
	}
	if exp.Confidence <= CodingConfidenceDeprecatedThreshold {
		exp.Status = CodingStatusDeprecated
	}
	if recallEvidenceID != "" {
		result := "failure"
		if taskSucceeded {
			result = "success"
		}
		exp.LifecycleEvents = appendCodingExperienceLifecycleEvent(exp.LifecycleEvents, "recall_outcome_recorded", result, recallEvidenceID)
	}

	if recallEvidenceID == "" {
		return s.updateExperienceMetadata(ctx, exp)
	}
	claimed, err := s.updateExperienceMetadataWithRecallOutcome(ctx, exp, recallEvidenceID, taskSucceeded)
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("coding knowledge: recall outcome was already recorded for experience %s", id)
	}
	return nil
}

// UpdateExperience updates an existing experience (for manual edits).
// This re-indexes the FTS content if title/content/trigger changed while
// preserving the experience ID so UI links and recall stats stay stable.
func (s *CodingKnowledgeStore) UpdateExperience(ctx context.Context, exp CodingExperience) error {
	return s.UpdateExperienceWithBudget(ctx, exp, CodingExperienceBudget{})
}

// UpdateExperienceWithBudget updates an experience without allowing an active
// or verified project record to grow past the reviewed retention budget. The
// candidate's own ID is excluded from current usage, so this validates the
// proposed replacement content rather than double-counting the old version.
// A zero budget preserves the legacy API for hosts that have not yet exposed
// reviewed-knowledge settings.
func (s *CodingKnowledgeStore) UpdateExperienceWithBudget(ctx context.Context, exp CodingExperience, budget CodingExperienceBudget) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	if exp.ID == "" {
		return fmt.Errorf("coding knowledge: experience ID is required for update")
	}

	// Hydrate from the existing record when the caller only changed metadata
	// (ListExperiences does not populate Content).
	existing, err := s.GetExperience(ctx, exp.ID)
	if err != nil {
		return fmt.Errorf("coding knowledge: update %s: %w", exp.ID, err)
	}
	if strings.TrimSpace(exp.Content) == "" {
		exp.Content = existing.Content
	}
	if exp.CreatedAt.IsZero() {
		exp.CreatedAt = existing.CreatedAt
	}
	if err := preserveExperienceLifecycleOnUpdate(&exp, existing); err != nil {
		return err
	}
	if exp.LastRecalledAt.IsZero() {
		exp.LastRecalledAt = existing.LastRecalledAt
	}
	if err := preserveRuntimeProvenanceOnUpdate(&exp, existing); err != nil {
		return err
	}
	if err := preserveExperienceLineageOnUpdate(&exp, existing); err != nil {
		return err
	}
	if err := preserveExperienceLifecycleAuditOnUpdate(&exp, existing); err != nil {
		return err
	}
	if err := preserveExperienceLabelsOnUpdate(&exp, existing); err != nil {
		return err
	}
	if err := preserveExperienceReviewMetadataOnUpdate(&exp, existing); err != nil {
		return err
	}

	if err := validateExperience(exp); err != nil {
		return err
	}
	forceID := exp.ID
	forceCreatedAt := exp.CreatedAt
	exp.UpdatedAt = time.Now().UTC()
	if exp.CreatedAt.IsZero() {
		exp.CreatedAt = exp.UpdatedAt
		forceCreatedAt = exp.CreatedAt
	}

	if exp.Confidence == 0 {
		exp.Confidence = CodingConfidenceInitial
	}
	if isRuntimeDerivedExperience(exp) {
		if strings.TrimSpace(exp.SourceRuntimeTaskID) == "" || strings.TrimSpace(exp.SourceRuntimeAttemptID) == "" || strings.TrimSpace(exp.EvidenceDigest) == "" {
			return fmt.Errorf("coding knowledge: runtime-derived experience requires task, attempt and evidence digest")
		}
	}
	if exp.Status == "" {
		exp.Status = CodingStatusCandidate
	}
	if exp.Status == CodingStatusActive || exp.Status == CodingStatusVerified {
		if err := s.validateExperiencePromotionBudget(ctx, exp, budget); err != nil {
			return err
		}
	}

	// Complete every validation and serialization step before deleting the old
	// source. Update is implemented as re-indexing, but a malformed edit must
	// never turn a validation error into loss of the prior experience.
	indexText := buildExperienceIndexText(exp)
	meta := experienceToMetadata(exp)
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("coding knowledge: marshal metadata: %w", err)
	}
	labels := buildExperienceLabels(exp)

	// Delete old and re-save with the same ID so FTS + metadata stay consistent.
	if err := s.inner.DeleteSource(ctx, forceID); err != nil {
		return fmt.Errorf("coding knowledge: delete for re-save: %w", err)
	}

	source, err := s.inner.SaveText(ctx, TextSaveRequest{
		Text:           indexText,
		Title:          exp.Title,
		Kind:           SourceKindText,
		TopicHint:      string(metaJSON),
		ProjectPath:    exp.ProjectPath,
		Labels:         labels,
		DistillMode:    "off",
		ForceID:        forceID,
		ForceCreatedAt: forceCreatedAt,
	})
	if err != nil {
		return fmt.Errorf("coding knowledge: re-save: %w", err)
	}
	if source.ID != forceID {
		return fmt.Errorf("coding knowledge: update changed id %q -> %q", forceID, source.ID)
	}
	return nil
}

// AppendContraindication adds a "does not apply" scenario to an experience.
func (s *CodingKnowledgeStore) AppendContraindication(ctx context.Context, id string, scenario string) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	scenario = strings.TrimSpace(scenario)
	if scenario == "" {
		return fmt.Errorf("coding knowledge: contraindication is required")
	}
	exp.Contraindications = append(exp.Contraindications, scenario)
	exp.LifecycleEvents = appendCodingExperienceLifecycleEvent(exp.LifecycleEvents, "contraindication_added", scenario, "")
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
}

// ConfirmCandidate promotes a candidate experience to active status. Runtime-
// derived candidates require a verifier so a stale, deleted, or tampered
// Task/Attempt/EvidenceDigest cannot become prompt-injectable knowledge.
func (s *CodingKnowledgeStore) ConfirmCandidate(ctx context.Context, id string, verifyRuntime ...RuntimeProvenanceVerifier) error {
	return s.ConfirmCandidateWithBudget(ctx, id, CodingExperienceBudget{}, verifyRuntime...)
}

// ConfirmCandidateWithBudget promotes a candidate only if the applicable
// project-level reviewed-knowledge budget still has room. A zero budget field
// is unlimited, preserving the legacy confirmation behavior for hosts that do
// not yet expose an explicit budget configuration.
func (s *CodingKnowledgeStore) ConfirmCandidateWithBudget(ctx context.Context, id string, budget CodingExperienceBudget, verifyRuntime ...RuntimeProvenanceVerifier) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	if exp.Status != CodingStatusCandidate {
		return fmt.Errorf("coding knowledge: experience %s is not a candidate (status=%s)", id, exp.Status)
	}
	if isRuntimeDerivedExperience(exp) && (strings.TrimSpace(exp.SourceRuntimeTaskID) == "" || strings.TrimSpace(exp.SourceRuntimeAttemptID) == "" || strings.TrimSpace(exp.EvidenceDigest) == "") {
		return fmt.Errorf("coding knowledge: runtime-derived candidate %s has incomplete provenance", id)
	}
	if isRuntimeDerivedExperience(exp) {
		if len(verifyRuntime) == 0 || verifyRuntime[0] == nil {
			return fmt.Errorf("coding knowledge: runtime-derived candidate %s requires provenance verification before confirmation", id)
		}
		if err := verifyRuntime[0](ctx, exp); err != nil {
			return fmt.Errorf("coding knowledge: runtime-derived candidate %s provenance verification failed: %w", id, err)
		}
	}
	if err := s.validateExperiencePromotionBudget(ctx, exp, budget); err != nil {
		return err
	}
	exp.Status = CodingStatusActive
	exp.LastReviewedAt = time.Now().UTC()
	exp.LifecycleEvents = appendCodingExperienceLifecycleEvent(exp.LifecycleEvents, "candidate_confirmed", "review confirmed candidate", "")
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
}

// validateExperiencePromotionBudget evaluates retention capacity before the
// state transition. It intentionally counts both active and verified entries:
// both are eligible for automatic injection, and allowing unlimited active
// entries would let a caller evade a "verified project experience" budget by
// confirming more candidates before recall evidence promotes them.
func (s *CodingKnowledgeStore) validateExperiencePromotionBudget(ctx context.Context, candidate CodingExperience, budget CodingExperienceBudget) error {
	if budget.MaxVerifiedCount <= 0 && budget.MaxVerifiedTokens <= 0 {
		return nil
	}
	if candidate.Scope != CodingScopeProject || strings.TrimSpace(candidate.ProjectPath) == "" {
		return nil
	}
	count, tokens, err := s.projectReviewedExperienceUsage(ctx, candidate.ID, candidate.ProjectPath)
	if err != nil {
		return fmt.Errorf("coding knowledge: inspect project experience budget: %w", err)
	}
	candidateTokens := codingExperienceTokenCost(candidate)
	if budget.MaxVerifiedCount > 0 && count+1 > budget.MaxVerifiedCount {
		return fmt.Errorf("coding knowledge: project reviewed experience count budget exceeded for %q (%d/%d)", candidate.ProjectPath, count+1, budget.MaxVerifiedCount)
	}
	if budget.MaxVerifiedTokens > 0 && tokens+candidateTokens > budget.MaxVerifiedTokens {
		return fmt.Errorf("coding knowledge: project reviewed experience token budget exceeded for %q (%d/%d)", candidate.ProjectPath, tokens+candidateTokens, budget.MaxVerifiedTokens)
	}
	return nil
}

const codingExperienceBudgetPageSize = 5000

// projectReviewedExperienceUsage returns the exact reviewed usage for one
// project. ListSources intentionally caps a single page at 5,000 rows for
// general-purpose callers, so a retention gate must page instead of relying on
// an oversized limit. It also hydrates the stored node text before estimating
// tokens: ListExperiences is a lightweight metadata projection whose empty
// Content field would otherwise undercount every existing experience.
func (s *CodingKnowledgeStore) projectReviewedExperienceUsage(ctx context.Context, excludeID, projectPath string) (int, int, error) {
	return s.projectReviewedExperienceUsageWithPageSize(ctx, excludeID, projectPath, codingExperienceBudgetPageSize)
}

// projectReviewedExperienceUsageWithPageSize is split out to keep pagination
// deterministic and directly testable without creating thousands of records.
func (s *CodingKnowledgeStore) projectReviewedExperienceUsageWithPageSize(ctx context.Context, excludeID, projectPath string, pageSize int) (int, int, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return 0, 0, nil
	}
	if pageSize <= 0 || pageSize > codingExperienceBudgetPageSize {
		pageSize = codingExperienceBudgetPageSize
	}

	count, tokens := 0, 0
	for offset := 0; ; offset += pageSize {
		sources, err := s.inner.ListSources(ctx, ListSourcesOptions{
			ProjectPath:     projectPath,
			Limit:           pageSize,
			Offset:          offset,
			IncludeDisabled: true,
		})
		if err != nil {
			return 0, 0, err
		}
		for _, source := range sources {
			exp, err := sourceToExperience(source)
			if err != nil || exp.ID == excludeID || exp.Scope != CodingScopeProject || exp.ProjectPath != projectPath {
				continue
			}
			if exp.Status != CodingStatusActive && exp.Status != CodingStatusVerified {
				continue
			}
			// Coding experience text is stored in its first document node. A
			// missing node is tolerated for legacy/corrupt records and falls
			// back to TriggerCondition, matching codingExperienceTokenCost.
			nodes, err := s.inner.ListNodesBySource(ctx, exp.ID, 1)
			if err != nil {
				return 0, 0, err
			}
			if len(nodes) > 0 && strings.TrimSpace(nodes[0].Text) != "" {
				exp.Content = nodes[0].Text
			}
			count++
			tokens += codingExperienceTokenCost(exp)
		}
		if len(sources) < pageSize {
			return count, tokens, nil
		}
	}
}

func codingExperienceTokenCost(exp CodingExperience) int {
	text := strings.TrimSpace(exp.Content)
	if text == "" {
		text = strings.TrimSpace(exp.TriggerCondition)
	}
	return estimateTokens(text)
}

// UpdateStatus updates only a non-promotion lifecycle transition and labels of
// an experience (no FTS re-index). Promotion or restoration is deliberately
// excluded: callers must use the dedicated review path so Runtime provenance is
// checked against the durable ledger before a candidate becomes injectable.
func (s *CodingKnowledgeStore) UpdateStatus(ctx context.Context, id string, status string, addLabels []string) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	if err := validateStatusTransition(exp.Status, status); err != nil {
		return err
	}
	previous := exp.Status
	exp.Status = status
	exp.Labels = append(exp.Labels, addLabels...)
	exp.LifecycleEvents = appendCodingExperienceLifecycleEvent(exp.LifecycleEvents, "status_changed", previous+" -> "+status, "")
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
}

// MarkConflict deprecates an experience because it conflicts with another
// known experience or evidence. The target is retained for audit and manual
// correction; it is never silently deleted nor automatically reactivated.
func (s *CodingKnowledgeStore) MarkConflict(ctx context.Context, id, relatedID, reason string) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("coding knowledge: conflict reason is required")
	}
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	if exp.Status != CodingStatusDeprecated {
		exp.Status = CodingStatusDeprecated
	}
	exp.Labels = append(exp.Labels, "conflicted")
	exp.LifecycleEvents = appendCodingExperienceLifecycleEvent(exp.LifecycleEvents, "conflict_marked", reason, relatedID)
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
}

// RetireToSteering retires a verified experience after its guidance has been
// materialized as a steering rule. The steering reference is deliberately a
// basename or logical ID rather than an absolute path so the bounded audit
// does not expose installation-specific filesystem details.
//
// This is intentionally a dedicated operation instead of a generic status
// update: graduating a rule is an external side effect that must leave an
// explainable lifecycle event and may only start from evidence-earned
// verified knowledge.
func (s *CodingKnowledgeStore) RetireToSteering(ctx context.Context, id, steeringReference string) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	steeringReference = strings.TrimSpace(steeringReference)
	if steeringReference == "" {
		return fmt.Errorf("coding knowledge: steering reference is required")
	}
	if strings.ContainsAny(steeringReference, `/\\`) {
		return fmt.Errorf("coding knowledge: steering reference must not contain a filesystem path")
	}
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	if exp.Status != CodingStatusVerified {
		return fmt.Errorf("coding knowledge: only verified experiences can be graduated (status=%s)", exp.Status)
	}
	exp.Status = CodingStatusDeprecated
	exp.Labels = append(exp.Labels, "graduated_to_steering")
	// LastReviewedAt means the last positive local review. Graduation retires
	// a record; it is not a new validation of the old guidance.
	exp.LifecycleEvents = appendCodingExperienceLifecycleEvent(exp.LifecycleEvents, "graduated_to_steering", "graduated after verified review", steeringReference)
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
}

// CreateRevisionCandidate is the rollback-safe replacement for reviving a
// deprecated experience. It creates a new, isolated candidate that a reviewer
// may edit and confirm, while the old conflicting/deprecated record remains
// retired and auditable. Runtime provenance is intentionally not copied: it
// proves the original observation, not the revised guidance.
func (s *CodingKnowledgeStore) CreateRevisionCandidate(ctx context.Context, id, reason string) (CodingExperience, error) {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return CodingExperience{}, fmt.Errorf("coding knowledge: revision reason is required")
	}
	parent, err := s.GetExperience(ctx, id)
	if err != nil {
		return CodingExperience{}, err
	}
	if parent.Status != CodingStatusDeprecated {
		return CodingExperience{}, fmt.Errorf("coding knowledge: only deprecated experiences can create a revision candidate (status=%s)", parent.Status)
	}

	candidate := parent
	candidate.ID = ""
	candidate.ParentExperienceID = parent.ID
	candidate.CreatedBy = "revision"
	candidate.LastReviewedAt = time.Time{}
	candidate.SourceRuntimeTaskID = ""
	candidate.SourceRuntimeAttemptID = ""
	candidate.EvidenceDigest = ""
	candidate.Status = CodingStatusCandidate
	candidate.Confidence = CodingConfidenceInitial
	candidate.RecallCount = 0
	candidate.SuccessCount = 0
	candidate.FailureCount = 0
	candidate.CreatedAt = time.Time{}
	candidate.UpdatedAt = time.Time{}
	candidate.LastRecalledAt = time.Time{}
	candidate.Labels = withoutCodingExperienceLabel(candidate.Labels, "conflicted")
	candidate.Labels = append(candidate.Labels, "revision_candidate")
	candidate.LifecycleEvents = []CodingExperienceLifecycleEvent{{
		Action:     "revision_candidate_created",
		Reason:     truncateCodingExperienceAuditText(reason, 512),
		RelatedID:  parent.ID,
		OccurredAt: time.Now().UTC(),
	}}
	saved, err := s.saveExperience(ctx, candidate, true, "revision")
	if err != nil {
		return CodingExperience{}, err
	}

	parent.LifecycleEvents = appendCodingExperienceLifecycleEvent(parent.LifecycleEvents, "revision_candidate_created", reason, saved.ID)
	parent.UpdatedAt = time.Now().UTC()
	if err := s.updateExperienceMetadata(ctx, parent); err != nil {
		return CodingExperience{}, fmt.Errorf("coding knowledge: revision candidate %s created but parent audit update failed: %w", saved.ID, err)
	}
	return saved, nil
}

// ListLifecycleEvents returns a defensive copy of the bounded lifecycle audit
// attached to an experience. It is sufficient for review without exposing raw
// commands, model transcripts, or Ledger event payloads.
func (s *CodingKnowledgeStore) ListLifecycleEvents(ctx context.Context, id string) ([]CodingExperienceLifecycleEvent, error) {
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return nil, err
	}
	return append([]CodingExperienceLifecycleEvent(nil), exp.LifecycleEvents...), nil
}

// ---------------------------------------------------------------------------
// Read operations
// ---------------------------------------------------------------------------

// GetExperience retrieves a single experience by ID.
func (s *CodingKnowledgeStore) GetExperience(ctx context.Context, id string) (CodingExperience, error) {
	source, err := s.inner.GetSource(ctx, id)
	if err != nil {
		return CodingExperience{}, fmt.Errorf("coding knowledge: get %s: %w", id, err)
	}
	exp, err := sourceToExperience(source)
	if err != nil {
		return CodingExperience{}, err
	}
	// Hydrate content from document nodes (full text stored there by SaveText)
	nodes, nodeErr := s.inner.ListNodesBySource(ctx, id, 1)
	if nodeErr == nil && len(nodes) > 0 && nodes[0].Text != "" {
		exp.Content = nodes[0].Text
	}
	return exp, nil
}

// SearchExperiences searches the coding knowledge base with scope-aware filtering.
func (s *CodingKnowledgeStore) SearchExperiences(ctx context.Context, opts CodingSearchOptions) ([]CodingExperience, error) {
	if opts.Query == "" {
		return nil, fmt.Errorf("coding knowledge: query is required")
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Search a bounded but deliberately broad candidate pool, then apply coding
	// scope semantics below. Passing ProjectPath to the generic store would turn
	// this into an exact source filter and hide universal/language guidance. The
	// coding layer instead admits shared guidance plus the current project's
	// entries, while rejecting project-scoped records from other projects.
	candidateLimit := opts.Limit * 8
	if candidateLimit < 30 {
		candidateLimit = 30
	}
	if candidateLimit > 100 {
		candidateLimit = 100
	}
	searchOpts := SearchOptions{
		Query:  opts.Query,
		Labels: append([]string{"coding_experience"}, opts.Labels...),
		Limit:  candidateLimit,
	}

	results, err := s.inner.Search(ctx, searchOpts)
	if err != nil {
		return nil, fmt.Errorf("coding knowledge: search: %w", err)
	}

	type scoredExperience struct {
		experience CodingExperience
		score      float64
	}
	candidates := make(map[string]scoredExperience, opts.Limit)
	for _, r := range results {
		exp, err := sourceToExperience(r.Source)
		if err != nil {
			continue // Skip entries with bad metadata
		}
		exp.ID = r.Source.ID

		// Populate Content from search snippet when not hydrated from nodes.
		if exp.Content == "" {
			if r.Snippet != "" {
				exp.Content = r.Snippet
			} else if r.Claim != "" {
				exp.Content = r.Claim
			} else if r.Summary != "" {
				exp.Content = r.Summary
			}
		}

		// Post-filter by scope/language/status
		if !matchesCodingSearchFilter(exp, opts) {
			continue
		}

		// Search can return more than one evidence type for the same source.
		// Keep its best relevance score while making a coding experience appear
		// only once in the context pack.
		if existing, ok := candidates[exp.ID]; !ok || r.Score > existing.score {
			candidates[exp.ID] = scoredExperience{experience: exp, score: r.Score}
		}
	}

	experiences := make([]scoredExperience, 0, len(candidates))
	for _, candidate := range candidates {
		experiences = append(experiences, candidate)
	}
	// Preserve retrieval relevance, then apply scope affinity and confidence.
	sort.Slice(experiences, func(i, j int) bool {
		si := weightedScore(experiences[i].experience, experiences[i].score, opts)
		sj := weightedScore(experiences[j].experience, experiences[j].score, opts)
		if si != sj {
			return si > sj
		}
		if experiences[i].experience.Title != experiences[j].experience.Title {
			return experiences[i].experience.Title < experiences[j].experience.Title
		}
		return experiences[i].experience.ID < experiences[j].experience.ID
	})

	if len(experiences) > opts.Limit {
		experiences = experiences[:opts.Limit]
	}
	out := make([]CodingExperience, 0, len(experiences))
	for _, candidate := range experiences {
		out = append(out, candidate.experience)
	}
	return out, nil
}

// ListExperiences returns all experiences matching the given filter.
func (s *CodingKnowledgeStore) ListExperiences(ctx context.Context, filter CodingListFilter) ([]CodingExperience, error) {
	listOpts := ListSourcesOptions{
		ProjectPath:     filter.ProjectPath,
		Labels:          filter.Labels,
		Limit:           filter.Limit,
		IncludeDisabled: true,
	}
	if listOpts.Limit <= 0 {
		listOpts.Limit = 100
	}

	sources, err := s.inner.ListSources(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("coding knowledge: list: %w", err)
	}

	experiences := make([]CodingExperience, 0, len(sources))
	for _, src := range sources {
		exp, err := sourceToExperience(src)
		if err != nil {
			continue
		}
		// Post-filter by scope/language/status/category
		if filter.Scope != "" && exp.Scope != filter.Scope {
			continue
		}
		if filter.Language != "" && exp.Language != filter.Language {
			continue
		}
		if filter.Status != "" && exp.Status != filter.Status {
			continue
		}
		if filter.Category != "" && exp.Category != filter.Category {
			continue
		}
		experiences = append(experiences, exp)
	}

	return experiences, nil
}

// Stats returns aggregate statistics about the coding knowledge base.
func (s *CodingKnowledgeStore) Stats(ctx context.Context) (CodingKnowledgeStats, error) {
	sources, err := s.inner.ListSources(ctx, ListSourcesOptions{Limit: 10000, IncludeDisabled: true})
	if err != nil {
		return CodingKnowledgeStats{}, err
	}

	stats := CodingKnowledgeStats{
		ByProject:  make(map[string]int),
		ByCategory: make(map[string]int),
		ByLanguage: make(map[string]int),
	}

	var totalConf float64
	for _, src := range sources {
		exp, err := sourceToExperience(src)
		if err != nil {
			continue
		}
		stats.TotalCount++
		totalConf += exp.Confidence

		switch exp.Status {
		case CodingStatusActive:
			stats.ActiveCount++
		case CodingStatusVerified:
			stats.VerifiedCount++
		case CodingStatusCandidate:
			stats.CandidateCount++
		case CodingStatusDeprecated:
			stats.DeprecatedCount++
		}

		if exp.ProjectPath != "" {
			stats.ByProject[exp.ProjectPath]++
		}
		if exp.Category != "" {
			stats.ByCategory[exp.Category]++
		}
		if exp.Language != "" {
			stats.ByLanguage[exp.Language]++
		}
	}

	if stats.TotalCount > 0 {
		stats.AvgConfidence = totalConf / float64(stats.TotalCount)
	}

	return stats, nil
}

// ---------------------------------------------------------------------------
// Delete operations
// ---------------------------------------------------------------------------

// DeleteExperience removes a single experience by ID.
//
// This is an explicit user-directed deletion primitive. Automated capacity
// management must use EvictExperience so it cannot silently discard a record
// whose lifecycle audit is needed to explain a conflict, review, or steering
// graduation.
func (s *CodingKnowledgeStore) DeleteExperience(ctx context.Context, id string) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	return s.inner.DeleteSource(ctx, id)
}

// EvictExperience deletes only disposable, locally unreviewed knowledge. It
// deliberately refuses active/verified guidance and every record with a
// lifecycle audit: those entries are evidence or review history, not cache
// entries. Explicit user deletion remains available through DeleteExperience.
func (s *CodingKnowledgeStore) EvictExperience(ctx context.Context, id string) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	if len(exp.LifecycleEvents) > 0 {
		return fmt.Errorf("coding knowledge: cannot automatically evict audited experience %s", id)
	}
	if exp.Status != CodingStatusCandidate && exp.Status != CodingStatusDeprecated {
		return fmt.Errorf("coding knowledge: cannot automatically evict reviewed experience %s (status=%s)", id, exp.Status)
	}
	return s.inner.DeleteSource(ctx, id)
}

// SanitizeExperienceForExport converts an experience into a portable content
// proposal. Runtime IDs, lifecycle/audit history and usage statistics are
// installation-local facts; a recipient must establish its own provenance and
// review rather than receiving them as transferable authority.
func SanitizeExperienceForExport(exp CodingExperience) CodingExperience {
	exp.ID = ""
	exp.Status = CodingStatusCandidate
	exp.SourceRuntimeTaskID = ""
	exp.SourceRuntimeAttemptID = ""
	exp.EvidenceDigest = ""
	exp.ParentExperienceID = ""
	exp.LifecycleEvents = nil
	exp.CreatedBy = ""
	exp.LastReviewedAt = time.Time{}
	exp.Confidence = CodingConfidenceInitial
	exp.RecallCount = 0
	exp.SuccessCount = 0
	exp.FailureCount = 0
	exp.CreatedAt = time.Time{}
	exp.UpdatedAt = time.Time{}
	exp.LastRecalledAt = time.Time{}
	exp.Labels = removeManagedCodingExperienceLabels(exp.Labels)
	return exp
}

// DeleteByScope removes all experiences matching the given scope (and optional language).
func (s *CodingKnowledgeStore) DeleteByScope(ctx context.Context, scope, language string) (int, error) {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	sources, err := s.inner.ListSources(ctx, ListSourcesOptions{Limit: 10000, IncludeDisabled: true})
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, src := range sources {
		exp, err := sourceToExperience(src)
		if err != nil {
			continue
		}
		if exp.Scope != scope {
			continue
		}
		if language != "" && exp.Language != language {
			continue
		}
		if err := s.inner.DeleteSource(ctx, src.ID); err != nil {
			continue
		}
		deleted++
	}
	return deleted, nil
}

// Reset removes all coding knowledge by deleting and recreating the database.
// The caller should close this store and create a new one after calling Reset.
func (s *CodingKnowledgeStore) Reset(ctx context.Context) error {
	s.lifecycleWriteMu.Lock()
	defer s.lifecycleWriteMu.Unlock()
	sources, err := s.inner.ListSources(ctx, ListSourcesOptions{Limit: 100000, IncludeDisabled: true})
	if err != nil {
		return err
	}
	for _, src := range sources {
		_ = s.inner.DeleteSource(ctx, src.ID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Context Pack (for system prompt injection)
// ---------------------------------------------------------------------------

// ContextPackForTask builds a token-budgeted context pack for a coding task.
func (s *CodingKnowledgeStore) ContextPackForTask(ctx context.Context, opts CodingContextPackOptions) (ContextPackResult, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return ContextPackResult{Query: opts.Query}, nil
	}
	maxItems, maxChars, maxTokens := opts.MaxItems, opts.MaxChars, opts.MaxTokens
	if maxItems <= 0 {
		maxItems = 4
	}
	if maxChars <= 0 {
		maxChars = 1500
	}
	if maxTokens <= 0 {
		// Keep the legacy 1,500-character default while making the prompt budget
		// explicit and observable. estimateTokens is CJK-aware enough for this
		// local safety boundary and is used consistently by knowledge indexing.
		maxTokens = estimateTokens(strings.Repeat("x", maxChars))
	}
	experiences, err := s.SearchExperiences(ctx, CodingSearchOptions{
		Query:       opts.Query,
		Language:    opts.Language,
		Frameworks:  opts.Frameworks,
		ProjectPath: opts.ProjectPath,
		Labels:      opts.Labels,
		// Candidate/deprecated experiences must never enter automatic prompt
		// context. Manual search can still request them explicitly.
		Status: []string{CodingStatusActive, CodingStatusVerified},
		Limit:  maxItems,
	})
	if err != nil {
		return ContextPackResult{}, err
	}
	pack := ContextPackResult{Query: opts.Query, Items: make([]ContextPackItem, 0, len(experiences)), Notes: []string{"coding_experiences_active_or_verified_only", "runtime_candidates_excluded", "token_budget_enforced"}}
	for _, exp := range experiences {
		if len(pack.Items) >= maxItems || pack.CharacterCount >= maxChars || pack.TokenCount >= maxTokens {
			break
		}
		text := strings.TrimSpace(exp.Content)
		if text == "" {
			text = strings.TrimSpace(exp.TriggerCondition)
		}
		if text == "" {
			continue
		}
		remainingChars := maxChars - pack.CharacterCount
		remainingTokens := maxTokens - pack.TokenCount
		text, truncatedByChars := truncateContextText(text, remainingChars)
		text, truncatedByTokens := truncateCodingContextTextToTokenBudget(text, remainingTokens)
		if text == "" {
			break
		}
		if truncatedByChars || truncatedByTokens {
			pack.Notes = append(pack.Notes, "truncated_to_budget")
		}
		label := fmt.Sprintf("K%d", len(pack.Items)+1)
		pack.Items = append(pack.Items, ContextPackItem{Label: label, ResultType: "coding_experience", Title: exp.Title, Text: text, SourceID: exp.ID, Citation: "coding experience " + exp.ID, Score: exp.Confidence})
		pack.CharacterCount += len([]rune(text))
		pack.TokenCount += estimateTokens(text)
	}
	pack.Count = len(pack.Items)
	return pack, nil
}

// truncateCodingContextTextToTokenBudget applies the store's deterministic
// CJK-aware token estimator without relying on byte offsets. Unlike the generic
// character truncator it does not append a marker, because a marker itself can
// exceed a one-token remainder and would weaken the hard budget guarantee.
func truncateCodingContextTextToTokenBudget(text string, maxTokens int) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || maxTokens <= 0 {
		return "", text != ""
	}
	if estimateTokens(text) <= maxTokens {
		return text, false
	}
	runes := []rune(text)
	low, high := 0, len(runes)
	for low < high {
		mid := low + (high-low+1)/2
		candidate := strings.TrimSpace(string(runes[:mid]))
		if candidate != "" && estimateTokens(candidate) <= maxTokens {
			low = mid
			continue
		}
		high = mid - 1
	}
	if low == 0 {
		return "", true
	}
	return strings.TrimSpace(string(runes[:low])), true
}

// ---------------------------------------------------------------------------
// Supporting types
// ---------------------------------------------------------------------------

// CodingSearchOptions controls how coding experiences are searched.
type CodingSearchOptions struct {
	Query       string   // Full-text search query
	Scope       string   // Filter by scope (empty = all scopes)
	Language    string   // Filter by language (used when Scope=language or as affinity)
	Frameworks  []string // Framework affinity (optional)
	ProjectPath string   // Filter/boost by project path
	Labels      []string // Additional label filters
	Status      []string // Filter by status (empty = active + verified only)
	Limit       int
}

// CodingListFilter controls listing/enumeration of experiences.
type CodingListFilter struct {
	Scope       string   `json:"scope,omitempty"`
	Language    string   `json:"language,omitempty"`
	Category    string   `json:"category,omitempty"`
	Status      string   `json:"status,omitempty"`
	ProjectPath string   `json:"project_path,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Limit       int      `json:"limit,omitempty"`
}

// CodingContextPackOptions controls context pack generation for SubAgent injection.
type CodingContextPackOptions struct {
	Query       string
	Language    string
	Frameworks  []string
	ProjectPath string
	Labels      []string
	MaxItems    int
	MaxChars    int
	// MaxTokens is a deterministic, CJK-aware hard limit for the injected text.
	// When omitted, the legacy MaxChars default is converted to the same local
	// estimate used by the knowledge store.
	MaxTokens int
}

// CodingKnowledgeStats provides aggregate statistics.
type CodingKnowledgeStats struct {
	TotalCount      int            `json:"total_count"`
	ActiveCount     int            `json:"active_count"`
	VerifiedCount   int            `json:"verified_count"`
	CandidateCount  int            `json:"candidate_count"`
	DeprecatedCount int            `json:"deprecated_count"`
	ByProject       map[string]int `json:"by_project"`
	ByCategory      map[string]int `json:"by_category"`
	ByLanguage      map[string]int `json:"by_language"`
	AvgConfidence   float64        `json:"avg_confidence"`
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func validateExperience(exp CodingExperience) error {
	if strings.TrimSpace(exp.Title) == "" {
		return fmt.Errorf("coding knowledge: title is required")
	}
	if strings.TrimSpace(exp.Content) == "" {
		return fmt.Errorf("coding knowledge: content is required")
	}
	switch exp.Scope {
	case CodingScopeUniversal, CodingScopeLanguage, CodingScopeProject, "":
		// ok
	default:
		return fmt.Errorf("coding knowledge: invalid scope %q", exp.Scope)
	}
	if exp.Scope == CodingScopeLanguage && strings.TrimSpace(exp.Language) == "" {
		return fmt.Errorf("coding knowledge: language is required when scope=language")
	}
	if exp.Scope == CodingScopeProject && strings.TrimSpace(exp.ProjectPath) == "" {
		return fmt.Errorf("coding knowledge: project_path is required when scope=project")
	}
	switch exp.Category {
	case CodingCategoryPattern, CodingCategoryDecision, CodingCategoryPitfall, CodingCategoryConvention, "":
		// ok
	default:
		return fmt.Errorf("coding knowledge: invalid category %q", exp.Category)
	}
	return nil
}

// buildExperienceIndexText constructs the FTS-indexed text from an experience.
// This includes trigger_condition prominently (for recall matching) plus content.
func buildExperienceIndexText(exp CodingExperience) string {
	var parts []string
	if exp.TriggerCondition != "" {
		// Repeat trigger condition for higher FTS weight
		parts = append(parts, exp.TriggerCondition)
		parts = append(parts, exp.TriggerCondition)
	}
	parts = append(parts, exp.Title)
	parts = append(parts, exp.Content)
	if exp.CodeSnippet != "" {
		parts = append(parts, exp.CodeSnippet)
	}
	for _, fa := range exp.FailedAttempts {
		parts = append(parts, fa)
	}
	return strings.Join(parts, "\n\n")
}

func buildExperienceLabels(exp CodingExperience) []string {
	labels := make([]string, 0, len(exp.Labels)+4)
	labels = append(labels, "coding_experience")
	if exp.Scope != "" {
		labels = append(labels, "scope:"+exp.Scope)
	}
	if exp.Category != "" {
		labels = append(labels, "category:"+exp.Category)
	}
	if exp.Language != "" {
		labels = append(labels, "lang:"+exp.Language)
	}
	for _, fw := range exp.Frameworks {
		if fw != "" {
			labels = append(labels, "fw:"+fw)
		}
	}
	labels = append(labels, exp.Labels...)
	return labels
}

func experienceToMetadata(exp CodingExperience) CodingExperienceMetadata {
	lastRecalled := ""
	if !exp.LastRecalledAt.IsZero() {
		lastRecalled = exp.LastRecalledAt.Format(time.RFC3339)
	}
	return CodingExperienceMetadata{
		Category:               exp.Category,
		Scope:                  exp.Scope,
		Language:               exp.Language,
		Frameworks:             exp.Frameworks,
		TriggerCondition:       exp.TriggerCondition,
		CodeSnippet:            exp.CodeSnippet,
		FailedAttempts:         exp.FailedAttempts,
		Contraindications:      exp.Contraindications,
		SourceTaskTitle:        exp.SourceTaskTitle,
		LanguageVersion:        exp.LanguageVersion,
		ValidUntil:             exp.ValidUntil,
		SourceRuntimeTaskID:    exp.SourceRuntimeTaskID,
		SourceRuntimeAttemptID: exp.SourceRuntimeAttemptID,
		EvidenceDigest:         exp.EvidenceDigest,
		ParentExperienceID:     exp.ParentExperienceID,
		LifecycleEvents:        append([]CodingExperienceLifecycleEvent(nil), exp.LifecycleEvents...),
		CreatedBy:              exp.CreatedBy,
		LastReviewedAt:         formatCodingExperienceTimestamp(exp.LastReviewedAt),
		Confidence:             exp.Confidence,
		RecallCount:            exp.RecallCount,
		SuccessCount:           exp.SuccessCount,
		FailureCount:           exp.FailureCount,
		Status:                 exp.Status,
		LastRecalledAt:         lastRecalled,
	}
}

func formatCodingExperienceTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseCodingExperienceTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func sourceToExperience(src Source) (CodingExperience, error) {
	exp := CodingExperience{
		ID:          src.ID,
		Title:       src.Title,
		ProjectPath: src.ProjectPath,
		Labels:      append([]string(nil), src.Labels...),
		CreatedAt:   src.CreatedAt,
		UpdatedAt:   src.UpdatedAt,
	}

	// Parse metadata from TopicHint
	if src.TopicHint != "" {
		var meta CodingExperienceMetadata
		if err := json.Unmarshal([]byte(src.TopicHint), &meta); err != nil {
			// Not a coding experience or corrupted metadata
			return CodingExperience{}, fmt.Errorf("coding knowledge: parse metadata for %s: %w", src.ID, err)
		}
		exp.Category = meta.Category
		exp.Scope = meta.Scope
		exp.Language = meta.Language
		exp.Frameworks = meta.Frameworks
		exp.TriggerCondition = meta.TriggerCondition
		exp.CodeSnippet = meta.CodeSnippet
		exp.FailedAttempts = meta.FailedAttempts
		exp.Contraindications = meta.Contraindications
		exp.SourceTaskTitle = meta.SourceTaskTitle
		exp.LanguageVersion = meta.LanguageVersion
		exp.ValidUntil = meta.ValidUntil
		exp.SourceRuntimeTaskID = meta.SourceRuntimeTaskID
		exp.SourceRuntimeAttemptID = meta.SourceRuntimeAttemptID
		exp.EvidenceDigest = meta.EvidenceDigest
		exp.ParentExperienceID = meta.ParentExperienceID
		exp.LifecycleEvents = append([]CodingExperienceLifecycleEvent(nil), meta.LifecycleEvents...)
		exp.CreatedBy = meta.CreatedBy
		exp.LastReviewedAt = parseCodingExperienceTimestamp(meta.LastReviewedAt)
		exp.Confidence = meta.Confidence
		exp.RecallCount = meta.RecallCount
		exp.SuccessCount = meta.SuccessCount
		exp.FailureCount = meta.FailureCount
		exp.Status = meta.Status
		if meta.LastRecalledAt != "" {
			if t, err := time.Parse(time.RFC3339, meta.LastRecalledAt); err == nil {
				exp.LastRecalledAt = t
			}
		}
	}

	// Content is not directly available from Source (stored in document_nodes).
	// For GetExperience, the caller should read nodes separately if full content
	// is needed. For search results, content comes from SearchResult.Snippet.
	// We store a placeholder here; the metadata fields carry the structured data.
	exp.Content = "" // Populated by caller or from search snippet

	// Apply default confidence if zero (legacy or initial)
	if exp.Confidence == 0 {
		exp.Confidence = CodingConfidenceInitial
	}
	if exp.Status == "" {
		exp.Status = CodingStatusCandidate
	}
	if strings.TrimSpace(exp.CreatedBy) == "" {
		exp.CreatedBy = "legacy"
	}

	return exp, nil
}

const maxCodingExperienceLifecycleEvents = 32

func appendCodingExperienceLifecycleEvent(events []CodingExperienceLifecycleEvent, action, reason, relatedID string) []CodingExperienceLifecycleEvent {
	event := CodingExperienceLifecycleEvent{
		Action:     strings.TrimSpace(action),
		Reason:     truncateCodingExperienceAuditText(reason, 512),
		RelatedID:  truncateCodingExperienceAuditText(relatedID, 160),
		OccurredAt: time.Now().UTC(),
	}
	if event.Action == "" {
		return append([]CodingExperienceLifecycleEvent(nil), events...)
	}
	result := append(append([]CodingExperienceLifecycleEvent(nil), events...), event)
	if len(result) > maxCodingExperienceLifecycleEvents {
		result = result[len(result)-maxCodingExperienceLifecycleEvents:]
	}
	return result
}

func recallOutcomeRelatedID(outcome RecallOutcome) string {
	return outcome.RuntimeTaskID + ":" + outcome.RuntimeAttemptID + ":" + outcome.EvidenceDigest
}

func truncateCodingExperienceAuditText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func withoutCodingExperienceLabel(labels []string, removed string) []string {
	removed = strings.TrimSpace(removed)
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label) != removed {
			result = append(result, label)
		}
	}
	return result
}

func removeManagedCodingExperienceLabels(labels []string) []string {
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		if !codingExperienceLabelsContain(managedCodingExperienceLabels, strings.TrimSpace(label)) {
			result = append(result, label)
		}
	}
	return result
}

func isRuntimeDerivedExperience(exp CodingExperience) bool {
	return strings.TrimSpace(exp.SourceRuntimeTaskID) != "" || strings.TrimSpace(exp.SourceRuntimeAttemptID) != "" || strings.TrimSpace(exp.EvidenceDigest) != ""
}

func validateStatusTransition(from, to string) error {
	switch to {
	case CodingStatusCandidate, CodingStatusActive, CodingStatusVerified, CodingStatusDeprecated:
	default:
		return fmt.Errorf("coding knowledge: invalid experience status %q", to)
	}
	if from == to {
		return nil
	}
	if to == CodingStatusActive || to == CodingStatusVerified {
		return fmt.Errorf("coding knowledge: promotion requires its dedicated review or confidence operation")
	}
	return nil
}

// preserveExperienceLifecycleOnUpdate makes a content/metadata edit unable to
// forge recall statistics or bypass the review and confidence state machine.
// Callers that only know an old list projection often submit zero values, so
// zero is treated as omitted for positive counters/confidence; a conflicting
// non-zero value is rejected rather than trusted.
func preserveExperienceLifecycleOnUpdate(updated *CodingExperience, existing CodingExperience) error {
	if updated == nil {
		return fmt.Errorf("coding knowledge: update experience is required")
	}
	for _, field := range []struct {
		name     string
		provided int
		existing int
		assign   func(int)
	}{
		{"recall count", updated.RecallCount, existing.RecallCount, func(v int) { updated.RecallCount = v }},
		{"success count", updated.SuccessCount, existing.SuccessCount, func(v int) { updated.SuccessCount = v }},
		{"failure count", updated.FailureCount, existing.FailureCount, func(v int) { updated.FailureCount = v }},
	} {
		if field.provided != 0 && field.provided != field.existing {
			return fmt.Errorf("coding knowledge: %s is managed by recall evidence", field.name)
		}
		field.assign(field.existing)
	}
	if updated.Confidence != 0 && updated.Confidence != existing.Confidence {
		return fmt.Errorf("coding knowledge: confidence is managed by recall evidence")
	}
	updated.Confidence = existing.Confidence
	if updated.Status != "" && updated.Status != existing.Status {
		// A stale UI form may still contain an optimistic candidate → active or
		// candidate → verified value. Preserve the candidate rather than making
		// that presentation race a destructive edit, but reject all other
		// lifecycle rewrites before re-indexing.
		if existing.Status != CodingStatusCandidate || (updated.Status != CodingStatusActive && updated.Status != CodingStatusVerified) {
			return fmt.Errorf("coding knowledge: lifecycle status must be changed through its dedicated review operation")
		}
	}
	updated.Status = existing.Status
	return nil
}

// preserveRuntimeProvenanceOnUpdate keeps Runtime provenance immutable after
// creation. It is an audit fact that identifies the exact durable execution,
// rather than editor-controlled experience metadata. Runtime-derived records
// may still have their content edited, while ordinary records cannot be
// relabeled as Runtime-derived through an update.
func preserveRuntimeProvenanceOnUpdate(updated *CodingExperience, existing CodingExperience) error {
	if updated == nil {
		return fmt.Errorf("coding knowledge: update experience is required")
	}
	if !isRuntimeDerivedExperience(existing) {
		if isRuntimeDerivedExperience(*updated) {
			return fmt.Errorf("coding knowledge: runtime provenance can only be set when creating an experience")
		}
		return nil
	}

	for _, field := range []struct {
		name     string
		provided string
		existing string
	}{
		{"runtime task ID", updated.SourceRuntimeTaskID, existing.SourceRuntimeTaskID},
		{"runtime attempt ID", updated.SourceRuntimeAttemptID, existing.SourceRuntimeAttemptID},
		{"evidence digest", updated.EvidenceDigest, existing.EvidenceDigest},
	} {
		if strings.TrimSpace(field.provided) != "" && field.provided != field.existing {
			return fmt.Errorf("coding knowledge: runtime %s is immutable", field.name)
		}
	}

	updated.SourceRuntimeTaskID = existing.SourceRuntimeTaskID
	updated.SourceRuntimeAttemptID = existing.SourceRuntimeAttemptID
	updated.EvidenceDigest = existing.EvidenceDigest
	return nil
}

func preserveExperienceLineageOnUpdate(updated *CodingExperience, existing CodingExperience) error {
	if updated == nil {
		return fmt.Errorf("coding knowledge: update experience is required")
	}
	if strings.TrimSpace(existing.ParentExperienceID) == "" {
		if strings.TrimSpace(updated.ParentExperienceID) != "" {
			return fmt.Errorf("coding knowledge: parent experience is immutable and can only be set by a revision operation")
		}
		return nil
	}
	if strings.TrimSpace(updated.ParentExperienceID) != "" && updated.ParentExperienceID != existing.ParentExperienceID {
		return fmt.Errorf("coding knowledge: parent experience is immutable")
	}
	updated.ParentExperienceID = existing.ParentExperienceID
	return nil
}

// preserveExperienceLifecycleAuditOnUpdate keeps lifecycle evidence append-only
// and store-owned. An editor can change the guidance itself, but must neither
// inject a new audit event nor erase an earlier conflict/review history while
// the record is re-indexed.
func preserveExperienceLifecycleAuditOnUpdate(updated *CodingExperience, existing CodingExperience) error {
	if updated == nil {
		return fmt.Errorf("coding knowledge: update experience is required")
	}
	if len(updated.LifecycleEvents) > 0 && !codingExperienceLifecycleEventsEqual(updated.LifecycleEvents, existing.LifecycleEvents) {
		return fmt.Errorf("coding knowledge: lifecycle audit is managed by dedicated operations")
	}
	updated.LifecycleEvents = append([]CodingExperienceLifecycleEvent(nil), existing.LifecycleEvents...)
	return nil
}

func codingExperienceLifecycleEventsEqual(left, right []CodingExperienceLifecycleEvent) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Action != right[index].Action || left[index].Reason != right[index].Reason || left[index].RelatedID != right[index].RelatedID || !left[index].OccurredAt.Equal(right[index].OccurredAt) {
			return false
		}
	}
	return true
}

// preserveExperienceLabelsOnUpdate retains lifecycle labels when a caller is
// editing a list projection that omits labels, and prevents editors from
// claiming or removing labels that carry lifecycle meaning. Ordinary labels
// remain editable through the same API.
func preserveExperienceLabelsOnUpdate(updated *CodingExperience, existing CodingExperience) error {
	if updated == nil {
		return fmt.Errorf("coding knowledge: update experience is required")
	}
	if updated.Labels == nil {
		updated.Labels = append([]string(nil), existing.Labels...)
		return nil
	}
	for _, label := range managedCodingExperienceLabels {
		if codingExperienceLabelsContain(updated.Labels, label) != codingExperienceLabelsContain(existing.Labels, label) {
			return fmt.Errorf("coding knowledge: lifecycle label %q is managed by dedicated operations", label)
		}
	}
	return nil
}

var managedCodingExperienceLabels = []string{
	"conflicted",
	"graduated_to_steering",
	"revision_candidate",
	"imported",
	"import_requires_review",
}

func codingExperienceLabelsContain(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func preserveExperienceReviewMetadataOnUpdate(updated *CodingExperience, existing CodingExperience) error {
	if updated == nil {
		return fmt.Errorf("coding knowledge: update experience is required")
	}
	if strings.TrimSpace(updated.CreatedBy) != "" && updated.CreatedBy != existing.CreatedBy {
		return fmt.Errorf("coding knowledge: creator origin is immutable")
	}
	if !updated.LastReviewedAt.IsZero() && !updated.LastReviewedAt.Equal(existing.LastReviewedAt) {
		return fmt.Errorf("coding knowledge: review timestamp is managed by candidate confirmation")
	}
	if !updated.CreatedAt.IsZero() && !updated.CreatedAt.Equal(existing.CreatedAt) {
		return fmt.Errorf("coding knowledge: creation timestamp is immutable")
	}
	if !updated.UpdatedAt.IsZero() && !updated.UpdatedAt.Equal(existing.UpdatedAt) {
		return fmt.Errorf("coding knowledge: update timestamp is managed by the store")
	}
	if !updated.LastRecalledAt.IsZero() && !updated.LastRecalledAt.Equal(existing.LastRecalledAt) {
		return fmt.Errorf("coding knowledge: last recalled timestamp is managed by recall evidence")
	}
	updated.CreatedBy = existing.CreatedBy
	updated.LastReviewedAt = existing.LastReviewedAt
	updated.CreatedAt = existing.CreatedAt
	updated.UpdatedAt = existing.UpdatedAt
	updated.LastRecalledAt = existing.LastRecalledAt
	return nil
}

// updateExperienceMetadata updates the TopicHint JSON for an existing source.
func (s *CodingKnowledgeStore) updateExperienceMetadata(ctx context.Context, exp CodingExperience) error {
	meta := experienceToMetadata(exp)
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("coding knowledge: marshal metadata: %w", err)
	}

	_, err = s.inner.UpdateSourceMetadata(ctx, SourceUpdateRequest{
		ID:        exp.ID,
		TopicHint: string(metaJSON),
		Labels:    buildExperienceLabels(exp),
	})
	return err
}

// updateExperienceMetadataWithRecallOutcome stores the confidence/audit change
// together with a permanent outcome claim. LifecycleEvents remains a bounded
// human-readable audit, while SQLite enforces idempotency across compaction and
// process restarts.
func (s *CodingKnowledgeStore) updateExperienceMetadataWithRecallOutcome(ctx context.Context, exp CodingExperience, recallEvidenceID string, taskSucceeded bool) (bool, error) {
	meta := experienceToMetadata(exp)
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return false, fmt.Errorf("coding knowledge: marshal metadata: %w", err)
	}
	return s.inner.recordCodingExperienceRecallOutcome(ctx, exp.ID, recallEvidenceID, taskSucceeded, string(metaJSON), buildExperienceLabels(exp))
}

// matchesCodingSearchFilter checks if an experience matches the post-search filter.
func matchesCodingSearchFilter(exp CodingExperience, opts CodingSearchOptions) bool {
	// Status filter (default: active + verified only)
	allowedStatuses := opts.Status
	if len(allowedStatuses) == 0 {
		allowedStatuses = []string{CodingStatusActive, CodingStatusVerified}
	}
	statusOK := false
	for _, st := range allowedStatuses {
		if exp.Status == st {
			statusOK = true
			break
		}
	}
	if !statusOK {
		return false
	}

	// Scope/language filter
	if opts.Scope != "" && exp.Scope != opts.Scope {
		return false
	}
	// When language is specified, exclude language-scoped experiences of OTHER languages
	if opts.Language != "" && exp.Scope == CodingScopeLanguage && exp.Language != opts.Language {
		return false
	}
	// Project-scoped guidance is valid only for the exact project when a caller
	// provides project context. An empty ProjectPath denotes an administrative
	// listing/search, which intentionally remains able to inspect every project.
	// Universal and language-scoped guidance remains reusable and ranks below a
	// matching project record.
	if exp.Scope == CodingScopeProject && strings.TrimSpace(opts.ProjectPath) != "" && exp.ProjectPath != strings.TrimSpace(opts.ProjectPath) {
		return false
	}

	return true
}

// weightedScore applies scope-based weighting to the search score.
func weightedScore(exp CodingExperience, baseScore float64, opts CodingSearchOptions) float64 {
	weight := 1.0
	switch exp.Scope {
	case CodingScopeProject:
		if opts.ProjectPath != "" && exp.ProjectPath == opts.ProjectPath {
			weight = 2.5
		}
	case CodingScopeLanguage:
		if opts.Language != "" && exp.Language == opts.Language {
			weight = 1.8
			// Framework bonus
			if len(opts.Frameworks) > 0 && len(exp.Frameworks) > 0 {
				for _, of := range opts.Frameworks {
					for _, ef := range exp.Frameworks {
						if strings.EqualFold(of, ef) {
							weight *= 1.2
							break
						}
					}
				}
			}
		}
	case CodingScopeUniversal:
		weight = 1.0
	}

	return baseScore * weight * exp.Confidence
}
