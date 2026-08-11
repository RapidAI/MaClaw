package main

// app_coding_knowledge.go provides Wails bindings for the coding knowledge
// management panel in the Programming Tools settings.

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	return store.ListExperiences(ctx, filter)
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
func (a *App) verifyRuntimeCodingExperience(_ context.Context, exp knowledge.CodingExperience) error {
	if a == nil {
		return fmt.Errorf("coding runtime application is unavailable")
	}
	if err := codingruntime.VerifyExperienceProvenance(a.ensureCodingRuntimeStore(), exp.SourceRuntimeTaskID, exp.SourceRuntimeAttemptID, exp.EvidenceDigest); err != nil {
		return fmt.Errorf("runtime provenance no longer matches the candidate evidence: %w", err)
	}
	return nil
}

func (a *App) verifyCodingKnowledgeRecallOutcome(_ context.Context, outcome knowledge.RecallOutcome) error {
	if a == nil {
		return fmt.Errorf("coding runtime application is unavailable")
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
func (a *App) CodingKnowledgeSearch(query string, limit int) ([]knowledge.CodingExperience, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return nil, fmt.Errorf("coding knowledge store not available")
	}
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.SearchExperiences(ctx, knowledge.CodingSearchOptions{
		Query:  query,
		Limit:  limit,
		Status: []string{knowledge.CodingStatusCandidate, knowledge.CodingStatusActive, knowledge.CodingStatusVerified, knowledge.CodingStatusDeprecated},
	})
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
