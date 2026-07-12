package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func closeCodingKnowledgeStore(t *testing.T, app *App) {
	t.Helper()
	codingKnowledgeStoreMu.Lock()
	defer codingKnowledgeStoreMu.Unlock()
	if app != nil && app.codingKnowledgeStore != nil {
		_ = app.codingKnowledgeStore.Close()
		app.codingKnowledgeStore = nil
	}
}

func TestCodingKnowledgeWailsBindingsCRUD(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })

	saved, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title:            "Prefer timeouts on external calls",
		Category:         knowledge.CodingCategoryPattern,
		Scope:            knowledge.CodingScopeLanguage,
		Language:         "go",
		TriggerCondition: "http client timeout",
		Content:          "Always wrap outbound HTTP with context.WithTimeout.",
		Status:           knowledge.CodingStatusCandidate,
	})
	if err != nil {
		t.Fatalf("CodingKnowledgeSave: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("expected saved experience id")
	}

	stats, err := app.CodingKnowledgeStats()
	if err != nil {
		t.Fatalf("CodingKnowledgeStats: %v", err)
	}
	if stats.TotalCount != 1 || stats.CandidateCount != 1 {
		t.Fatalf("stats = %+v, want total=1 candidate=1", stats)
	}

	list, err := app.CodingKnowledgeList(knowledge.CodingListFilter{Limit: 10, Language: "go"})
	if err != nil {
		t.Fatalf("CodingKnowledgeList: %v", err)
	}
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("list = %+v", list)
	}

	if err := app.CodingKnowledgeConfirm(saved.ID); err != nil {
		t.Fatalf("CodingKnowledgeConfirm: %v", err)
	}
	got, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil {
		t.Fatalf("CodingKnowledgeGet: %v", err)
	}
	if got.Status != knowledge.CodingStatusActive {
		t.Fatalf("status after confirm = %q, want active", got.Status)
	}

	found, err := app.CodingKnowledgeSearch("timeout", 10)
	if err != nil {
		t.Fatalf("CodingKnowledgeSearch: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("expected search hit")
	}

	if err := app.CodingKnowledgeDelete(saved.ID); err != nil {
		t.Fatalf("CodingKnowledgeDelete: %v", err)
	}
	stats, err = app.CodingKnowledgeStats()
	if err != nil {
		t.Fatalf("CodingKnowledgeStats after delete: %v", err)
	}
	if stats.TotalCount != 0 {
		t.Fatalf("stats after delete = %+v", stats)
	}
}

func TestCodingKnowledgeCapacityAndEvict(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })

	// Seed more project-scoped experiences than the tiny limit.
	for i := 0; i < 5; i++ {
		status := knowledge.CodingStatusCandidate
		if i == 0 {
			status = knowledge.CodingStatusVerified
		}
		if _, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
			Title:       "proj exp " + string(rune('A'+i)),
			Content:     "content for project experience " + string(rune('A'+i)),
			Category:    knowledge.CodingCategoryPattern,
			Scope:       knowledge.CodingScopeProject,
			ProjectPath: "D:/demo/project",
			Status:      status,
			Confidence:  1.0 + float64(i)*0.1,
		}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	// Force low limits without going through disk config.
	// LoadConfig may return empty defaults; override via SaveConfig if available is heavy.
	// Instead call helpers directly with a synthetic config path through store eviction.
	capBefore := computeCodingKnowledgeCapacity(5, 3, 2, mustListAllCodingExperiences(t, app))
	if capBefore.OverTotal != 2 {
		t.Fatalf("over_total=%d want 2 (%+v)", capBefore.OverTotal, capBefore)
	}
	if capBefore.WouldEvict < 2 {
		t.Fatalf("would_evict=%d want >=2", capBefore.WouldEvict)
	}
	if len(capBefore.ProjectsOver) != 1 || capBefore.ProjectsOver[0].Over != 3 {
		// 5 project items, max 2 → over 3
		t.Fatalf("projects_over=%+v", capBefore.ProjectsOver)
	}

	// Apply limits by patching config file used by LoadConfig.
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.CodingKnowledgeMaxTotal = 3
	cfg.CodingKnowledgeMaxPerProject = 2
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	status, err := app.CodingKnowledgeCapacity()
	if err != nil {
		t.Fatalf("CodingKnowledgeCapacity: %v", err)
	}
	if status.MaxTotal != 3 || status.MaxPerProject != 2 {
		t.Fatalf("limits not applied: %+v", status)
	}

	evicted, err := app.CodingKnowledgeEvict()
	if err != nil {
		t.Fatalf("CodingKnowledgeEvict: %v", err)
	}
	if evicted < 2 {
		t.Fatalf("evicted=%d want >=2", evicted)
	}

	stats, err := app.CodingKnowledgeStats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalCount > 3 {
		t.Fatalf("total after evict=%d want <=3", stats.TotalCount)
	}

	// Verified should be preferred to keep when scores differ.
	list, err := app.CodingKnowledgeList(knowledge.CodingListFilter{Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	hasVerified := false
	for _, exp := range list {
		if exp.Status == knowledge.CodingStatusVerified {
			hasVerified = true
		}
	}
	if !hasVerified {
		t.Fatalf("expected verified experience to survive eviction, got %+v", list)
	}
}

func mustListAllCodingExperiences(t *testing.T, app *App) []knowledge.CodingExperience {
	t.Helper()
	list, err := app.CodingKnowledgeList(knowledge.CodingListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return list
}

func TestCodingKnowledgeResetFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	if _, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title:   "temp",
		Content: "temp content for reset",
		Scope:   knowledge.CodingScopeUniversal,
		Status:  knowledge.CodingStatusActive,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := app.CodingKnowledgeResetFile(); err != nil {
		t.Fatalf("CodingKnowledgeResetFile: %v", err)
	}
	// Store re-opens on next access.
	stats, err := app.CodingKnowledgeStats()
	if err != nil {
		t.Fatalf("stats after reset file: %v", err)
	}
	if stats.TotalCount != 0 {
		t.Fatalf("expected empty store after reset file, got %+v", stats)
	}
}
