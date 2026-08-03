package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	CodingConfidenceInitial           = 1.0
	CodingConfidenceMax               = 2.0
	CodingConfidenceMin               = 0.0
	CodingConfidenceSuccessBoost      = 0.15
	CodingConfidenceFailurePenalty    = 0.25
	CodingConfidenceVerifiedThreshold = 1.5
	CodingConfidenceDeprecatedThreshold = 0.3
	CodingMinRecallsForVerified       = 5
)

// CodingExperience is the primary data structure for coding knowledge entries.
type CodingExperience struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Category         string   `json:"category"`          // pattern/decision/pitfall/convention
	Scope            string   `json:"scope"`             // universal/language/project
	Language         string   `json:"language,omitempty"` // Required when Scope=language
	Frameworks       []string `json:"frameworks,omitempty"`
	TriggerCondition string   `json:"trigger_condition"` // Short keyword combo for recall matching
	Content          string   `json:"content"`           // Full experience (problem + solution + context)
	CodeSnippet      string   `json:"code_snippet,omitempty"`
	FailedAttempts   []string `json:"failed_attempts,omitempty"`
	Contraindications []string `json:"contraindications,omitempty"` // Scenarios where this does NOT apply

	// Metadata
	Labels          []string `json:"labels,omitempty"`
	ProjectPath     string   `json:"project_path,omitempty"` // Required when Scope=project
	SourceTaskTitle string   `json:"source_task_title,omitempty"`
	LanguageVersion string   `json:"language_version,omitempty"` // e.g. "go1.21"
	ValidUntil      string   `json:"valid_until,omitempty"`      // e.g. "go1.22" means deprecated after 1.22

	// Confidence & statistics
	Confidence  float64 `json:"confidence"`
	RecallCount int     `json:"recall_count"`
	SuccessCount int    `json:"success_count"`
	FailureCount int    `json:"failure_count"`

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
	Category          string   `json:"category"`
	Scope             string   `json:"scope"`
	Language          string   `json:"language,omitempty"`
	Frameworks        []string `json:"frameworks,omitempty"`
	TriggerCondition  string   `json:"trigger_condition"`
	CodeSnippet       string   `json:"code_snippet,omitempty"`
	FailedAttempts    []string `json:"failed_attempts,omitempty"`
	Contraindications []string `json:"contraindications,omitempty"`
	SourceTaskTitle   string   `json:"source_task_title,omitempty"`
	LanguageVersion   string   `json:"language_version,omitempty"`
	ValidUntil        string   `json:"valid_until,omitempty"`
	Confidence        float64  `json:"confidence"`
	RecallCount       int      `json:"recall_count"`
	SuccessCount      int      `json:"success_count"`
	FailureCount      int      `json:"failure_count"`
	Status            string   `json:"status"`
	LastRecalledAt    string   `json:"last_recalled_at,omitempty"`
}

// CodingKnowledgeStore wraps a dedicated SQLiteStore for coding experiences.
type CodingKnowledgeStore struct {
	inner *SQLiteStore
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

// SaveExperience persists a new coding experience into the knowledge base.
func (s *CodingKnowledgeStore) SaveExperience(ctx context.Context, exp CodingExperience) (CodingExperience, error) {
	if err := validateExperience(exp); err != nil {
		return CodingExperience{}, err
	}

	// Set defaults
	if exp.Confidence == 0 {
		exp.Confidence = CodingConfidenceInitial
	}
	if exp.Status == "" {
		exp.Status = CodingStatusCandidate
	}
	now := time.Now().UTC()
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

// UpdateConfidence updates the confidence score after a recall event.
func (s *CodingKnowledgeStore) UpdateConfidence(ctx context.Context, id string, taskSucceeded bool) error {
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
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

	// Status transitions
	if exp.Confidence >= CodingConfidenceVerifiedThreshold && exp.RecallCount >= CodingMinRecallsForVerified {
		exp.Status = CodingStatusVerified
	}
	if exp.Confidence <= CodingConfidenceDeprecatedThreshold {
		exp.Status = CodingStatusDeprecated
	}

	return s.updateExperienceMetadata(ctx, exp)
}

// UpdateExperience updates an existing experience (for manual edits).
// This re-indexes the FTS content if title/content/trigger changed while
// preserving the experience ID so UI links and recall stats stay stable.
func (s *CodingKnowledgeStore) UpdateExperience(ctx context.Context, exp CodingExperience) error {
	if exp.ID == "" {
		return fmt.Errorf("coding knowledge: experience ID is required for update")
	}

	// Hydrate from the existing record when the caller only changed metadata
	// (ListExperiences does not populate Content).
	existing, existingErr := s.GetExperience(ctx, exp.ID)
	if existingErr == nil {
		if strings.TrimSpace(exp.Content) == "" {
			exp.Content = existing.Content
		}
		if exp.CreatedAt.IsZero() {
			exp.CreatedAt = existing.CreatedAt
		}
		// Preserve counters unless the editor explicitly set them.
		if exp.RecallCount == 0 && existing.RecallCount > 0 {
			exp.RecallCount = existing.RecallCount
		}
		if exp.SuccessCount == 0 && existing.SuccessCount > 0 {
			exp.SuccessCount = existing.SuccessCount
		}
		if exp.FailureCount == 0 && existing.FailureCount > 0 {
			exp.FailureCount = existing.FailureCount
		}
		if exp.Confidence == 0 && existing.Confidence > 0 {
			exp.Confidence = existing.Confidence
		}
		if exp.Status == "" {
			exp.Status = existing.Status
		}
		if exp.LastRecalledAt.IsZero() {
			exp.LastRecalledAt = existing.LastRecalledAt
		}
	} else if strings.TrimSpace(exp.Content) == "" {
		nodes, err := s.inner.ListNodesBySource(ctx, exp.ID, 1)
		if err == nil && len(nodes) > 0 && nodes[0].Text != "" {
			exp.Content = nodes[0].Text
		}
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

	// Delete old and re-save with the same ID so FTS + metadata stay consistent.
	if err := s.inner.DeleteSource(ctx, forceID); err != nil {
		return fmt.Errorf("coding knowledge: delete for re-save: %w", err)
	}

	if exp.Confidence == 0 {
		exp.Confidence = CodingConfidenceInitial
	}
	if exp.Status == "" {
		exp.Status = CodingStatusCandidate
	}

	indexText := buildExperienceIndexText(exp)
	meta := experienceToMetadata(exp)
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("coding knowledge: marshal metadata: %w", err)
	}
	labels := buildExperienceLabels(exp)

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
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	exp.Contraindications = append(exp.Contraindications, strings.TrimSpace(scenario))
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
}

// ConfirmCandidate promotes a candidate experience to active status.
func (s *CodingKnowledgeStore) ConfirmCandidate(ctx context.Context, id string) error {
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	if exp.Status != CodingStatusCandidate {
		return fmt.Errorf("coding knowledge: experience %s is not a candidate (status=%s)", id, exp.Status)
	}
	exp.Status = CodingStatusActive
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
}

// UpdateStatus updates only the status and labels of an experience (no FTS re-index).
// Used for lifecycle transitions (graduate, deprecate) where content doesn't change.
func (s *CodingKnowledgeStore) UpdateStatus(ctx context.Context, id string, status string, addLabels []string) error {
	exp, err := s.GetExperience(ctx, id)
	if err != nil {
		return err
	}
	exp.Status = status
	exp.Labels = append(exp.Labels, addLabels...)
	exp.UpdatedAt = time.Now().UTC()
	return s.updateExperienceMetadata(ctx, exp)
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

	// Search with a larger candidate pool, then filter by scope/language/status
	searchOpts := SearchOptions{
		Query:       opts.Query,
		ProjectPath: opts.ProjectPath,
		Labels:      opts.Labels,
		Limit:       opts.Limit * 3, // Over-fetch for post-filtering
	}

	results, err := s.inner.Search(ctx, searchOpts)
	if err != nil {
		return nil, fmt.Errorf("coding knowledge: search: %w", err)
	}

	experiences := make([]CodingExperience, 0, opts.Limit)
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

		experiences = append(experiences, exp)
		if len(experiences) >= opts.Limit {
			break
		}
	}

	// Sort by weighted score (scope affinity + confidence), descending
	sort.Slice(experiences, func(i, j int) bool {
		si := weightedScore(experiences[i], 1.0, opts)
		sj := weightedScore(experiences[j], 1.0, opts)
		return si > sj
	})

	return experiences, nil
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
func (s *CodingKnowledgeStore) DeleteExperience(ctx context.Context, id string) error {
	return s.inner.DeleteSource(ctx, id)
}

// DeleteByScope removes all experiences matching the given scope (and optional language).
func (s *CodingKnowledgeStore) DeleteByScope(ctx context.Context, scope, language string) (int, error) {
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
	searchOpts := SearchOptions{
		Query:       opts.Query,
		ProjectPath: opts.ProjectPath,
		Labels:      opts.Labels,
		Limit:       20,
	}

	packOpts := ContextPackOptions{
		SearchOptions: searchOpts,
		MaxItems:      opts.MaxItems,
		MaxChars:      opts.MaxChars,
	}
	if packOpts.MaxItems <= 0 {
		packOpts.MaxItems = 4
	}
	if packOpts.MaxChars <= 0 {
		packOpts.MaxChars = 1500
	}

	return s.inner.ContextPack(ctx, packOpts)
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
		Category:          exp.Category,
		Scope:             exp.Scope,
		Language:          exp.Language,
		Frameworks:        exp.Frameworks,
		TriggerCondition:  exp.TriggerCondition,
		CodeSnippet:       exp.CodeSnippet,
		FailedAttempts:    exp.FailedAttempts,
		Contraindications: exp.Contraindications,
		SourceTaskTitle:   exp.SourceTaskTitle,
		LanguageVersion:   exp.LanguageVersion,
		ValidUntil:        exp.ValidUntil,
		Confidence:        exp.Confidence,
		RecallCount:       exp.RecallCount,
		SuccessCount:      exp.SuccessCount,
		FailureCount:      exp.FailureCount,
		Status:            exp.Status,
		LastRecalledAt:    lastRecalled,
	}
}

func sourceToExperience(src Source) (CodingExperience, error) {
	exp := CodingExperience{
		ID:          src.ID,
		Title:       src.Title,
		ProjectPath: src.ProjectPath,
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

	return exp, nil
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
	})
	return err
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
