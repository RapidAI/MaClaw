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

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
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
	return store.UpdateExperience(ctx, exp)
}

// CodingKnowledgeConfirm promotes a candidate experience to active.
func (a *App) CodingKnowledgeConfirm(id string) error {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.ConfirmCandidate(ctx, id)
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
func (a *App) CodingKnowledgeSave(exp knowledge.CodingExperience) (knowledge.CodingExperience, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return knowledge.CodingExperience{}, fmt.Errorf("coding knowledge store not available")
	}
	// Manual saves go directly to active status
	if exp.Status == "" {
		exp.Status = knowledge.CodingStatusActive
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.SaveExperience(ctx, exp)
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
