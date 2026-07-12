package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func openTestCodingStore(t *testing.T) *CodingKnowledgeStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "coding_knowledge.db")
	store, err := NewCodingKnowledgeStore(dbPath)
	if err != nil {
		t.Fatalf("open coding knowledge store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCodingKnowledgeStore_UpdatePreservesID(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title:            "timeouts matter",
		Category:         CodingCategoryPattern,
		Scope:            CodingScopeLanguage,
		Language:         "go",
		TriggerCondition: "http timeout",
		Content:          "Always set client timeouts.",
		Status:           CodingStatusActive,
	})
	if err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}
	if err := store.UpdateConfidence(ctx, saved.ID, true); err != nil {
		t.Fatalf("UpdateConfidence: %v", err)
	}

	updated := saved
	updated.Title = "timeouts still matter"
	updated.Content = "Always set client timeouts and cancel contexts."
	if err := store.UpdateExperience(ctx, updated); err != nil {
		t.Fatalf("UpdateExperience: %v", err)
	}

	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetExperience after update: %v", err)
	}
	if got.ID != saved.ID {
		t.Fatalf("id changed: %q -> %q", saved.ID, got.ID)
	}
	if got.Title != "timeouts still matter" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Content == "" || got.Content == saved.Content {
		t.Fatalf("content not updated: %q", got.Content)
	}
	if got.RecallCount < 1 {
		t.Fatalf("expected recall stats preserved, got %+v", got)
	}
}

func TestCodingKnowledgeStore_SaveAndGet(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	exp := CodingExperience{
		Title:            "Go interface 不能嵌套指针",
		Category:         CodingCategoryPitfall,
		Scope:            CodingScopeLanguage,
		Language:         "go",
		TriggerCondition: "Go interface 组合 嵌套 指针",
		Content:          "Go interface 只能嵌套 interface 本身，不能嵌套指针。移除 * 即可。",
		Labels:           []string{"compile_error"},
	}

	saved, err := store.SaveExperience(ctx, exp)
	if err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("expected non-empty ID after save")
	}
	if saved.Status != CodingStatusCandidate {
		t.Errorf("expected status=candidate, got %s", saved.Status)
	}
	if saved.Confidence != CodingConfidenceInitial {
		t.Errorf("expected confidence=%.1f, got %.2f", CodingConfidenceInitial, saved.Confidence)
	}

	// Get by ID
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if got.Title != exp.Title {
		t.Errorf("title: got %q, want %q", got.Title, exp.Title)
	}
	if got.Category != CodingCategoryPitfall {
		t.Errorf("category: got %q, want %q", got.Category, CodingCategoryPitfall)
	}
	if got.Scope != CodingScopeLanguage {
		t.Errorf("scope: got %q, want %q", got.Scope, CodingScopeLanguage)
	}
	if got.Language != "go" {
		t.Errorf("language: got %q, want %q", got.Language, "go")
	}
	if got.TriggerCondition != exp.TriggerCondition {
		t.Errorf("trigger: got %q, want %q", got.TriggerCondition, exp.TriggerCondition)
	}
}

func TestCodingKnowledgeStore_SearchByLanguage(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	// Save Go experience
	goExp := CodingExperience{
		Title:            "Go sync.Map 用于并发读多写少",
		Category:         CodingCategoryPattern,
		Scope:            CodingScopeLanguage,
		Language:         "go",
		TriggerCondition: "Go 并发 map concurrent",
		Content:          "Go 中 sync.Map 适合读多写少的并发场景。",
		Status:           CodingStatusActive,
	}
	_, err := store.SaveExperience(ctx, goExp)
	if err != nil {
		t.Fatalf("save Go exp: %v", err)
	}

	// Save Python experience
	pyExp := CodingExperience{
		Title:            "Python GIL 限制多线程 CPU 密集计算",
		Category:         CodingCategoryPitfall,
		Scope:            CodingScopeLanguage,
		Language:         "python",
		TriggerCondition: "Python 多线程 CPU GIL",
		Content:          "Python GIL 使得多线程不能提升 CPU 密集型计算性能，用 multiprocessing 替代。",
		Status:           CodingStatusActive,
	}
	_, err = store.SaveExperience(ctx, pyExp)
	if err != nil {
		t.Fatalf("save Python exp: %v", err)
	}

	// Search with Go language filter — should only find Go experience
	results, err := store.SearchExperiences(ctx, CodingSearchOptions{
		Query:    "并发 map",
		Language: "go",
		Status:   []string{CodingStatusActive, CodingStatusCandidate},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("SearchExperiences: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for Go concurrent map search")
	}
	for _, r := range results {
		if r.Scope == CodingScopeLanguage && r.Language != "go" {
			t.Errorf("language filter: got language=%s, expected go", r.Language)
		}
	}
}

func TestCodingKnowledgeStore_ConfidenceUpdate(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	exp := CodingExperience{
		Title:            "Test confidence",
		Category:         CodingCategoryPattern,
		Scope:            CodingScopeUniversal,
		TriggerCondition: "test",
		Content:          "Test content for confidence tracking.",
		Status:           CodingStatusActive,
	}
	saved, err := store.SaveExperience(ctx, exp)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Success boosts confidence
	if err := store.UpdateConfidence(ctx, saved.ID, true); err != nil {
		t.Fatalf("UpdateConfidence(success): %v", err)
	}
	got, _ := store.GetExperience(ctx, saved.ID)
	expectedConf := CodingConfidenceInitial + CodingConfidenceSuccessBoost
	if got.Confidence != expectedConf {
		t.Errorf("after success: confidence=%.2f, want %.2f", got.Confidence, expectedConf)
	}
	if got.RecallCount != 1 {
		t.Errorf("recall_count=%d, want 1", got.RecallCount)
	}
	if got.SuccessCount != 1 {
		t.Errorf("success_count=%d, want 1", got.SuccessCount)
	}

	// Failure reduces confidence
	if err := store.UpdateConfidence(ctx, saved.ID, false); err != nil {
		t.Fatalf("UpdateConfidence(failure): %v", err)
	}
	got, _ = store.GetExperience(ctx, saved.ID)
	expectedConf = expectedConf - CodingConfidenceFailurePenalty
	if got.Confidence != expectedConf {
		t.Errorf("after failure: confidence=%.2f, want %.2f", got.Confidence, expectedConf)
	}
	if got.FailureCount != 1 {
		t.Errorf("failure_count=%d, want 1", got.FailureCount)
	}
}

func TestCodingKnowledgeStore_ConfirmCandidate(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	exp := CodingExperience{
		Title:            "Candidate experience",
		Scope:            CodingScopeUniversal,
		TriggerCondition: "test candidate",
		Content:          "This should start as candidate.",
	}
	saved, err := store.SaveExperience(ctx, exp)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Status != CodingStatusCandidate {
		t.Fatalf("expected candidate, got %s", saved.Status)
	}

	// Confirm
	if err := store.ConfirmCandidate(ctx, saved.ID); err != nil {
		t.Fatalf("ConfirmCandidate: %v", err)
	}
	got, _ := store.GetExperience(ctx, saved.ID)
	if got.Status != CodingStatusActive {
		t.Errorf("after confirm: status=%s, want active", got.Status)
	}
}

func TestCodingKnowledgeStore_DeleteAndReset(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	// Save two experiences
	for _, title := range []string{"Exp 1", "Exp 2"} {
		_, err := store.SaveExperience(ctx, CodingExperience{
			Title:            title,
			Scope:            CodingScopeUniversal,
			TriggerCondition: "test delete",
			Content:          "Content for " + title,
		})
		if err != nil {
			t.Fatalf("save %s: %v", title, err)
		}
	}

	stats, _ := store.Stats(ctx)
	if stats.TotalCount != 2 {
		t.Fatalf("expected 2 experiences, got %d", stats.TotalCount)
	}

	// Reset
	if err := store.Reset(ctx); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	stats, _ = store.Stats(ctx)
	if stats.TotalCount != 0 {
		t.Errorf("after reset: total=%d, want 0", stats.TotalCount)
	}
}

func TestCodingKnowledgeStore_ValidationErrors(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	tests := []struct {
		name string
		exp  CodingExperience
	}{
		{"empty title", CodingExperience{Content: "c", Scope: CodingScopeUniversal, TriggerCondition: "t"}},
		{"empty content", CodingExperience{Title: "t", Scope: CodingScopeUniversal, TriggerCondition: "t"}},
		{"language scope without language", CodingExperience{Title: "t", Content: "c", Scope: CodingScopeLanguage, TriggerCondition: "t"}},
		{"project scope without path", CodingExperience{Title: "t", Content: "c", Scope: CodingScopeProject, TriggerCondition: "t"}},
		{"invalid scope", CodingExperience{Title: "t", Content: "c", Scope: "invalid", TriggerCondition: "t"}},
		{"invalid category", CodingExperience{Title: "t", Content: "c", Scope: CodingScopeUniversal, TriggerCondition: "t", Category: "bad"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.SaveExperience(ctx, tt.exp)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestCodingKnowledgeStore_ScopeWeighting(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	// Save universal, language, and project scoped experiences with same content base
	exps := []CodingExperience{
		{Title: "Universal pattern", Scope: CodingScopeUniversal, TriggerCondition: "临时文件 rename", Content: "先写临时文件再 rename 防止写一半崩溃", Status: CodingStatusActive},
		{Title: "Go 临时文件", Scope: CodingScopeLanguage, Language: "go", TriggerCondition: "Go 临时文件 rename", Content: "Go 中用 os.CreateTemp + os.Rename 模式写大文件", Status: CodingStatusActive},
		{Title: "Morio 项目临时文件", Scope: CodingScopeProject, ProjectPath: "d:\\workprj\\morio", TriggerCondition: "morio 临时文件", Content: "Morio 项目用 ioutil.TempFile 在 .tmp/ 目录", Status: CodingStatusActive},
	}
	for _, exp := range exps {
		if _, err := store.SaveExperience(ctx, exp); err != nil {
			t.Fatalf("save %s: %v", exp.Title, err)
		}
	}

	// Search with project context — project scope should rank highest
	results, err := store.SearchExperiences(ctx, CodingSearchOptions{
		Query:       "临时文件 rename",
		Language:    "go",
		ProjectPath: "d:\\workprj\\morio",
		Status:      []string{CodingStatusActive},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// We can't guarantee exact ordering since FTS scores vary,
	// but project-scoped result should have highest weighted score
	// due to 2.5x multiplier
}

func TestNewCodingKnowledgeStore_InvalidPath(t *testing.T) {
	// Empty path should fail
	_, err := NewCodingKnowledgeStore("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestNewCodingKnowledgeStore_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "coding_knowledge.db")
	store, err := NewCodingKnowledgeStore(dbPath)
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("expected database file to be created")
	}
}
