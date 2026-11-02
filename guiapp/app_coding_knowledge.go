package guiapp

// app_coding_knowledge.go provides Wails bindings for the coding knowledge
// management panel in the Programming Tools settings.

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var codingKnowledgeStoreMu sync.Mutex

// ensureCodingKnowledgeStore lazily initializes the coding knowledge store.
func (a *App) ensureCodingKnowledgeStore() *knowledge.CodingKnowledgeStore {
	if a == nil {
		return nil
	}
	codingKnowledgeStoreMu.Lock()
	defer codingKnowledgeStoreMu.Unlock()

	if a.codingKnowledgeStore != nil {
		return a.codingKnowledgeStore
	}

	dbPath := filepath.Join(a.GetDataDir(), "coding_knowledge.db")
	store, err := knowledge.NewCodingKnowledgeStore(dbPath)
	if err != nil {
		log.Printf("[coding-knowledge] failed to open store: %v", err)
		return nil
	}
	a.codingKnowledgeStore = store
	// Share the active semantic embedder when one is available. SetEmbedder
	// starts a background vector refresh for existing experiences; recall stays
	// lexical until that finishes.
	if emb := a.knowledgeEmbedder(); emb != nil {
		store.Inner().SetEmbedder(emb)
	}
	log.Printf("[coding-knowledge] store opened at %s", dbPath)
	return a.codingKnowledgeStore
}

// ---------------------------------------------------------------------------
// Wails bindings (called from frontend)
// ---------------------------------------------------------------------------

// CodingKnowledgeStats returns aggregate statistics for the management panel.
func (a *App) CodingKnowledgeStats() (knowledge.CodingKnowledgeStats, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return knowledge.CodingKnowledgeStats{}, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.Stats(ctx)
}

// CodingKnowledgeList returns experiences matching the given filter.
func (a *App) CodingKnowledgeList(filter knowledge.CodingListFilter) ([]knowledge.CodingExperience, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return nil, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	filter.OmitContent = true
	items, err := store.ListExperiences(ctx, filter)
	if err != nil {
		return nil, err
	}
	return codingExperienceUIList(items), nil
}

// CodingKnowledgeGet retrieves a single experience by ID.
func (a *App) CodingKnowledgeGet(id string) (knowledge.CodingExperience, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return knowledge.CodingExperience{}, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.GetExperience(ctx, id)
}

// CodingKnowledgeUpdate updates an existing experience (manual edit from UI).
func (a *App) CodingKnowledgeUpdate(exp knowledge.CodingExperience) error {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, _ := a.LoadConfig()
	return store.UpdateExperienceWithBudget(ctx, exp, codingKnowledgeReviewedProjectBudget(cfg))
}

// CodingKnowledgeConfirm promotes a candidate experience to active.
func (a *App) CodingKnowledgeConfirm(id string) error {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, _ := a.LoadConfig()
	return store.ConfirmCandidateWithBudget(ctx, id, codingKnowledgeReviewedProjectBudget(cfg), a.verifyRuntimeCodingExperience)
}

const maxCodingKnowledgeBulkConfirm = 200

// CodingKnowledgeConfirmFailure is one rejected ID in a bulk review action.
type CodingKnowledgeConfirmFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// CodingKnowledgeConfirmManyResult reports per-ID outcomes so a partial bulk
// review does not hide stale IDs, already-active records or budget failures.
type CodingKnowledgeConfirmManyResult struct {
	Confirmed []string                        `json:"confirmed"`
	Failed    []CodingKnowledgeConfirmFailure `json:"failed,omitempty"`
}

// CodingKnowledgeConfirmMany promotes selected candidates one by one. Each
// confirmation keeps the same provenance and reviewed-project budget checks as
// the single-item action; failures are reported instead of aborting the batch.
func (a *App) CodingKnowledgeConfirmMany(ids []string) (CodingKnowledgeConfirmManyResult, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return CodingKnowledgeConfirmManyResult{}, fmt.Errorf("coding knowledge store not available")
	}
	if len(ids) == 0 {
		return CodingKnowledgeConfirmManyResult{}, fmt.Errorf("no experience IDs provided")
	}
	if len(ids) > maxCodingKnowledgeBulkConfirm {
		return CodingKnowledgeConfirmManyResult{}, fmt.Errorf("too many experience IDs (max %d)", maxCodingKnowledgeBulkConfirm)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, _ := a.LoadConfig()
	budget := codingKnowledgeReviewedProjectBudget(cfg)
	result := CodingKnowledgeConfirmManyResult{Confirmed: make([]string, 0, len(ids))}
	seen := make(map[string]struct{}, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := store.ConfirmCandidateWithBudget(ctx, id, budget, a.verifyRuntimeCodingExperience); err != nil {
			result.Failed = append(result.Failed, CodingKnowledgeConfirmFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Confirmed = append(result.Confirmed, id)
	}
	return result, nil
}

// CodingKnowledgeRecordRecallOutcome records a locally verified Runtime task
// outcome for an already reviewed experience. It is intentionally separate
// from editing and confirmation: every confidence change must be traceable to
// one unique durable Runtime attempt.
func (a *App) CodingKnowledgeRecordRecallOutcome(id string, outcome knowledge.RecallOutcome) error {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.RecordRecallOutcome(ctx, id, outcome, a.verifyCodingKnowledgeRecallOutcome)
}

// CodingKnowledgeMarkConflict retires an experience from automatic recall
// while retaining a bounded audit record for human reconciliation.
func (a *App) CodingKnowledgeMarkConflict(id, relatedID, reason string) error {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.MarkConflict(ctx, id, relatedID, reason)
}

// CodingKnowledgeLifecycle returns the bounded lifecycle audit for review.
func (a *App) CodingKnowledgeLifecycle(id string) ([]knowledge.CodingExperienceLifecycleEvent, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return nil, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ListLifecycleEvents(ctx, id)
}

// CodingKnowledgeCreateRevisionCandidate creates a review-gated replacement
// for a deprecated experience; it never reactivates the retired record.
func (a *App) CodingKnowledgeCreateRevisionCandidate(id, reason string) (knowledge.CodingExperience, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return knowledge.CodingExperience{}, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.CreateRevisionCandidate(ctx, id, reason)
}

// verifyRuntimeCodingExperience recomputes provenance from the live durable
// Ledger before an automatically extracted candidate becomes active. It is
// deliberately invoked only at confirmation time: manual knowledge has no
// Runtime reference and remains eligible for the ordinary review flow.
func (a *App) verifyRuntimeCodingExperience(ctx context.Context, exp knowledge.CodingExperience) error {
	if a == nil {
		return fmt.Errorf("coding runtime application is unavailable")
	}
	taskID := strings.TrimSpace(exp.SourceRuntimeTaskID)
	if strings.HasPrefix(taskID, horizonExperienceProvenancePrefix) {
		return a.verifyHorizonCodingExperience(exp)
	}
	if strings.HasPrefix(taskID, knowledge.CodingTurnEvidenceTaskPrefix) {
		store := a.ensureCodingKnowledgeStore()
		if store == nil {
			return fmt.Errorf("coding knowledge store is unavailable")
		}
		if err := store.VerifyTurnRecallEvidence(ctx, taskID, exp.SourceRuntimeAttemptID, exp.EvidenceDigest); err != nil {
			return fmt.Errorf("main-agent turn provenance no longer matches the candidate evidence: %w", err)
		}
		return nil
	}
	if err := codingruntime.VerifyExperienceProvenance(a.ensureCodingRuntimeStore(), exp.SourceRuntimeTaskID, exp.SourceRuntimeAttemptID, exp.EvidenceDigest); err != nil {
		return fmt.Errorf("runtime provenance no longer matches the candidate evidence: %w", err)
	}
	return nil
}

func (a *App) verifyCodingKnowledgeRecallOutcome(ctx context.Context, outcome knowledge.RecallOutcome) error {
	if a == nil {
		return fmt.Errorf("coding runtime application is unavailable")
	}
	if strings.HasPrefix(strings.TrimSpace(outcome.RuntimeTaskID), knowledge.CodingTurnEvidenceTaskPrefix) {
		store := a.ensureCodingKnowledgeStore()
		if store == nil {
			return fmt.Errorf("coding knowledge store is unavailable")
		}
		if err := store.VerifyTurnRecallEvidence(ctx, outcome.RuntimeTaskID, outcome.RuntimeAttemptID, outcome.EvidenceDigest); err != nil {
			return fmt.Errorf("main-agent turn recall outcome no longer matches durable evidence: %w", err)
		}
		return nil
	}
	if err := codingruntime.VerifyExperienceProvenance(a.ensureCodingRuntimeStore(), outcome.RuntimeTaskID, outcome.RuntimeAttemptID, outcome.EvidenceDigest); err != nil {
		return fmt.Errorf("runtime recall outcome no longer matches durable evidence: %w", err)
	}
	return nil
}

// CodingKnowledgeDelete removes a single experience.
func (a *App) CodingKnowledgeDelete(id string) error {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.DeleteExperience(ctx, id)
}

// CodingKnowledgeDeleteByScope removes all experiences of a given scope/language.
func (a *App) CodingKnowledgeDeleteByScope(scope, language string) (int, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return 0, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return store.DeleteByScope(ctx, scope, language)
}

// CodingKnowledgeReset clears all coding knowledge (nuclear option for recovery).
func (a *App) CodingKnowledgeReset() error {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Reset(ctx); err != nil {
		return err
	}
	log.Printf("[coding-knowledge] store reset (all experiences deleted)")
	return nil
}

// CodingKnowledgeResetFile completely removes the database file and recreates.
// This is the most thorough recovery option.
func (a *App) CodingKnowledgeResetFile() error {
	codingKnowledgeStoreMu.Lock()
	defer codingKnowledgeStoreMu.Unlock()

	if a.codingKnowledgeStore != nil {
		_ = a.codingKnowledgeStore.Close()
		a.codingKnowledgeStore = nil
	}

	dbPath := filepath.Join(a.GetDataDir(), "coding_knowledge.db")
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(dbPath + suffix)
	}
	log.Printf("[coding-knowledge] database file removed: %s", dbPath)
	return nil
}

// CodingKnowledgeSave manually saves a new experience (from UI or main Agent).
// New records are staged as candidates; verified remains an evidence-derived
// state enforced by the Store.
func (a *App) CodingKnowledgeSave(exp knowledge.CodingExperience) (knowledge.CodingExperience, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return knowledge.CodingExperience{}, fmt.Errorf("coding knowledge store not available")
	}
	// A missing status means this is a newly proposed rule, so keep it out of
	// automatic prompt injection until an explicit review confirms it.
	if exp.Status == "" {
		exp.Status = knowledge.CodingStatusCandidate
	}
	if exp.CreatedBy != "" && exp.CreatedBy != "manual" {
		return knowledge.CodingExperience{}, fmt.Errorf("coding knowledge: manual save cannot set creator origin")
	}
	exp.CreatedBy = "manual"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, _ := a.LoadConfig()
	return store.SaveExperienceWithBudget(ctx, exp, codingKnowledgeReviewedProjectBudget(cfg))
}

// CodingKnowledgeSearch searches experiences (for the search box in the panel).
func (a *App) CodingKnowledgeSearch(query string, limit int, filter knowledge.CodingListFilter) ([]knowledge.CodingExperience, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return nil, fmt.Errorf("coding knowledge store not available")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("coding knowledge: query is required")
	}
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := store.SearchExperiences(ctx, knowledge.CodingSearchOptions{
		Query:       query,
		Limit:       limit,
		Scope:       filter.Scope,
		Language:    filter.Language,
		Status:      []string{knowledge.CodingStatusCandidate, knowledge.CodingStatusActive, knowledge.CodingStatusVerified, knowledge.CodingStatusDeprecated},
		OmitContent: true,
	})
	if err != nil {
		return nil, err
	}
	return codingExperienceUIList(items), nil
}

func codingExperienceUIList(items []knowledge.CodingExperience) []knowledge.CodingExperience {
	if len(items) == 0 {
		return items
	}
	out := make([]knowledge.CodingExperience, len(items))
	for i, exp := range items {
		exp.Content = ""
		exp.CodeSnippet = ""
		exp.FailedAttempts = nil
		exp.Contraindications = nil
		exp.LifecycleEvents = nil
		// The list only needs the count for the observation progress tag; the
		// underlying durable task IDs stay in the detail/audit API.
		exp.ObservationTaskIDs = nil
		out[i] = exp
	}
	return out
}

// SelectCodingKnowledgeExportPath opens a save dialog for coding experience packs.
func (a *App) SelectCodingKnowledgeExportPath() string {
	if a == nil || a.ctx == nil {
		return ""
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Coding Knowledge Pack",
		DefaultFilename: fmt.Sprintf("maclaw-coding-knowledge-%s.json", stamp),
		Filters: []runtime.FileFilter{
			{DisplayName: "Coding Knowledge Pack (*.json)", Pattern: "*.json"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return ""
	}
	return savePath
}

// SelectCodingKnowledgeImportFile opens a file picker for coding experience packs.
func (a *App) SelectCodingKnowledgeImportFile() string {
	if a == nil || a.ctx == nil {
		return ""
	}
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import Coding Knowledge Pack",
		Filters: []runtime.FileFilter{
			{DisplayName: "Coding Knowledge Pack (*.json)", Pattern: "*.json"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return ""
	}
	return selection
}
