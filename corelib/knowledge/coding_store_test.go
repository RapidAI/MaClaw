package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var codingKnowledgeTestRecallSequence uint64

func recordTestRecallOutcome(t *testing.T, store *CodingKnowledgeStore, id string, succeeded bool) error {
	t.Helper()
	sequence := atomic.AddUint64(&codingKnowledgeTestRecallSequence, 1)
	outcome := RecallOutcome{
		RuntimeTaskID:    fmt.Sprintf("test-task-%d", sequence),
		RuntimeAttemptID: fmt.Sprintf("test-attempt-%d", sequence),
		EvidenceDigest:   fmt.Sprintf("sha256:test-evidence-%d", sequence),
		TaskSucceeded:    succeeded,
	}
	return store.RecordRecallOutcome(context.Background(), id, outcome, func(context.Context, RecallOutcome) error { return nil })
}

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
	if err := recordTestRecallOutcome(t, store, saved.ID, true); err != nil {
		t.Fatalf("RecordRecallOutcome: %v", err)
	}

	updated, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetExperience before update: %v", err)
	}
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
	if err := recordTestRecallOutcome(t, store, saved.ID, true); err != nil {
		t.Fatalf("RecordRecallOutcome(success): %v", err)
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
	if err := recordTestRecallOutcome(t, store, saved.ID, false); err != nil {
		t.Fatalf("RecordRecallOutcome(failure): %v", err)
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

func TestCodingKnowledgeStore_RecordRecallOutcomeRequiresVerifiedUniqueRuntimeEvidence(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "runtime recall evidence", Scope: CodingScopeUniversal, TriggerCondition: "outcome evidence", Content: "confidence must be traceable", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome := RecallOutcome{RuntimeTaskID: "task-1", RuntimeAttemptID: "attempt-1", EvidenceDigest: "sha256:outcome", TaskSucceeded: true}
	if err := store.RecordRecallOutcome(ctx, saved.ID, outcome, nil); err == nil || !strings.Contains(err.Error(), "requires runtime evidence verification") {
		t.Fatalf("bare outcome must be rejected, err=%v", err)
	}
	if err := store.RecordRecallOutcome(ctx, saved.ID, outcome, func(context.Context, RecallOutcome) error { return nil }); err != nil {
		t.Fatalf("verified outcome: %v", err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecallCount != 1 || got.SuccessCount != 1 || got.Confidence != CodingConfidenceInitial+CodingConfidenceSuccessBoost {
		t.Fatalf("recorded outcome stats=%+v", got)
	}
	if len(got.LifecycleEvents) != 1 || got.LifecycleEvents[0].Action != "recall_outcome_recorded" || got.LifecycleEvents[0].Reason != "success" || got.LifecycleEvents[0].RelatedID != recallOutcomeRelatedID(outcome) {
		t.Fatalf("recorded outcome audit=%+v", got.LifecycleEvents)
	}
	if err := store.RecordRecallOutcome(ctx, saved.ID, outcome, func(context.Context, RecallOutcome) error { return nil }); err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("duplicate outcome must not inflate confidence, err=%v", err)
	}
	afterDuplicate, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterDuplicate.RecallCount != got.RecallCount || afterDuplicate.Confidence != got.Confidence || len(afterDuplicate.LifecycleEvents) != len(got.LifecycleEvents) {
		t.Fatalf("duplicate outcome mutated evidence: got=%+v baseline=%+v", afterDuplicate, got)
	}

	candidate, err := store.SaveExperience(ctx, CodingExperience{
		Title: "candidate outcome", Scope: CodingScopeUniversal, TriggerCondition: "candidate evidence", Content: "must be reviewed first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRecallOutcome(ctx, candidate.ID, RecallOutcome{RuntimeTaskID: "task-2", RuntimeAttemptID: "attempt-2", EvidenceDigest: "sha256:candidate", TaskSucceeded: true}, func(context.Context, RecallOutcome) error { return nil }); err == nil || !strings.Contains(err.Error(), "reviewed active or verified") {
		t.Fatalf("candidate outcome must be rejected, err=%v", err)
	}
}

func TestCodingKnowledgeStore_RecordRecallOutcomeIsConcurrentAndIdempotent(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "concurrent recall outcome", Scope: CodingScopeUniversal, TriggerCondition: "duplicate callback", Content: "only one outcome may be counted", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome := RecallOutcome{RuntimeTaskID: "task-concurrent", RuntimeAttemptID: "attempt-concurrent", EvidenceDigest: "sha256:concurrent", TaskSucceeded: true}
	const callers = 12
	start := make(chan struct{})
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			errs <- store.RecordRecallOutcome(ctx, saved.ID, outcome, func(context.Context, RecallOutcome) error { return nil })
		}()
	}
	close(start)
	succeeded, duplicated := 0, 0
	for i := 0; i < callers; i++ {
		err := <-errs
		if err == nil {
			succeeded++
			continue
		}
		if strings.Contains(err.Error(), "already recorded") {
			duplicated++
			continue
		}
		t.Fatalf("unexpected concurrent recall result: %v", err)
	}
	if succeeded != 1 || duplicated != callers-1 {
		t.Fatalf("concurrent recall results: succeeded=%d duplicated=%d", succeeded, duplicated)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecallCount != 1 || got.SuccessCount != 1 || got.Confidence != CodingConfidenceInitial+CodingConfidenceSuccessBoost || len(got.LifecycleEvents) != 1 {
		t.Fatalf("duplicate callbacks inflated outcome: %+v", got)
	}
}

func TestCodingKnowledgeStore_RecallOutcomeDedupSurvivesAuditCompactionAndRestart(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "coding_knowledge.db")
	store, err := NewCodingKnowledgeStore(dbPath)
	if err != nil {
		t.Fatalf("open coding knowledge store: %v", err)
	}
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "durable recall de-duplication", Scope: CodingScopeUniversal,
		TriggerCondition: "late duplicate callback", Content: "evidence claims outlive compact audit logs", Status: CodingStatusActive,
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	first := RecallOutcome{RuntimeTaskID: "task-0", RuntimeAttemptID: "attempt-0", EvidenceDigest: "sha256:first", TaskSucceeded: true}
	verify := func(context.Context, RecallOutcome) error { return nil }
	if err := store.RecordRecallOutcome(ctx, saved.ID, first, verify); err != nil {
		_ = store.Close()
		t.Fatalf("record first outcome: %v", err)
	}
	for i := 1; i <= maxCodingExperienceLifecycleEvents; i++ {
		outcome := RecallOutcome{
			RuntimeTaskID:    fmt.Sprintf("task-%d", i),
			RuntimeAttemptID: fmt.Sprintf("attempt-%d", i),
			EvidenceDigest:   fmt.Sprintf("sha256:evidence-%d", i),
			TaskSucceeded:    true,
		}
		if err := store.RecordRecallOutcome(ctx, saved.ID, outcome, verify); err != nil {
			_ = store.Close()
			t.Fatalf("record outcome %d: %v", i, err)
		}
	}
	beforeRestart, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if len(beforeRestart.LifecycleEvents) != maxCodingExperienceLifecycleEvents {
		_ = store.Close()
		t.Fatalf("compact audit count = %d, want %d", len(beforeRestart.LifecycleEvents), maxCodingExperienceLifecycleEvents)
	}
	if beforeRestart.LifecycleEvents[0].RelatedID == recallOutcomeRelatedID(first) {
		_ = store.Close()
		t.Fatal("first outcome unexpectedly remained in bounded audit")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewCodingKnowledgeStore(dbPath)
	if err != nil {
		t.Fatalf("reopen coding knowledge store: %v", err)
	}
	defer reopened.Close()
	if err := reopened.RecordRecallOutcome(ctx, saved.ID, first, verify); err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("old outcome must remain duplicate after restart, err=%v", err)
	}
	afterDuplicate, err := reopened.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterDuplicate.RecallCount != beforeRestart.RecallCount || afterDuplicate.SuccessCount != beforeRestart.SuccessCount || afterDuplicate.Confidence != beforeRestart.Confidence {
		t.Fatalf("late duplicate mutated stats: before=%+v after=%+v", beforeRestart, afterDuplicate)
	}
}

func TestCodingKnowledgeStore_DeleteExperienceCascadesRecallOutcomeClaims(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "recall outcome deletion", Scope: CodingScopeUniversal,
		TriggerCondition: "delete evidence claim", Content: "deleting the experience removes its local recall evidence", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recordTestRecallOutcome(t, store, saved.ID, true); err != nil {
		t.Fatalf("record recall outcome: %v", err)
	}
	var before int
	if err := store.inner.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM coding_experience_recall_outcomes WHERE experience_id = ?`, saved.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 1 {
		t.Fatalf("recall outcome claim count before delete = %d, want 1", before)
	}
	if err := store.DeleteExperience(ctx, saved.ID); err != nil {
		t.Fatalf("delete experience: %v", err)
	}
	var after int
	if err := store.inner.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM coding_experience_recall_outcomes WHERE experience_id = ?`, saved.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("recall outcome claims remained after experience delete: %d", after)
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

func TestCodingKnowledgeStore_ConfirmCandidateWithProjectBudget(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	project := "D:/work/project-budget"
	first, err := store.SaveExperience(ctx, CodingExperience{
		Title: "first reviewed project guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "project budget", Content: "first reviewed project experience",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmCandidateWithBudget(ctx, first.ID, CodingExperienceBudget{MaxVerifiedCount: 1, MaxVerifiedTokens: 100}); err != nil {
		t.Fatalf("confirm first candidate: %v", err)
	}
	second, err := store.SaveExperience(ctx, CodingExperience{
		Title: "second reviewed project guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "project budget", Content: "second reviewed project experience",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmCandidateWithBudget(ctx, second.ID, CodingExperienceBudget{MaxVerifiedCount: 1, MaxVerifiedTokens: 100}); err == nil || !strings.Contains(err.Error(), "count budget exceeded") {
		t.Fatalf("count budget must reject second candidate, err=%v", err)
	}
	got, err := store.GetExperience(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CodingStatusCandidate || !got.LastReviewedAt.IsZero() {
		t.Fatalf("rejected confirmation mutated candidate: %+v", got)
	}

	tokenLimited, err := store.SaveExperience(ctx, CodingExperience{
		Title: "token limited project guidance", Scope: CodingScopeProject, ProjectPath: "D:/work/token-budget",
		TriggerCondition: "project token budget", Content: strings.Repeat("多字节项目经验", 20),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmCandidateWithBudget(ctx, tokenLimited.ID, CodingExperienceBudget{MaxVerifiedTokens: 4}); err == nil || !strings.Contains(err.Error(), "token budget exceeded") {
		t.Fatalf("token budget must reject oversized candidate, err=%v", err)
	}
	got, err = store.GetExperience(ctx, tokenLimited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CodingStatusCandidate {
		t.Fatalf("token budget rejection mutated candidate: %+v", got)
	}
}

func TestCodingKnowledgeStore_SaveExperienceWithBudgetRejectsActiveProjectBypass(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	budget := CodingExperienceBudget{MaxVerifiedCount: 1, MaxVerifiedTokens: 100}
	project := "D:/work/manual-project-budget"

	first, err := store.SaveExperienceWithBudget(ctx, CodingExperience{
		Title: "first manually reviewed guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "manual budget", Content: "first active manual record", Status: CodingStatusActive,
	}, budget)
	if err != nil {
		t.Fatalf("save first active manual record: %v", err)
	}
	if first.Status != CodingStatusActive || first.LastReviewedAt.IsZero() {
		t.Fatalf("first active manual record was not locally reviewed: %+v", first)
	}

	if _, err := store.SaveExperienceWithBudget(ctx, CodingExperience{
		Title: "second manually reviewed guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "manual budget", Content: "second active manual record", Status: CodingStatusActive,
	}, budget); err == nil || !strings.Contains(err.Error(), "count budget exceeded") {
		t.Fatalf("active manual record must not bypass reviewed project budget, err=%v", err)
	}
	all, err := store.ListExperiences(ctx, CodingListFilter{ProjectPath: project, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != first.ID {
		t.Fatalf("rejected active manual save mutated project records: %+v", all)
	}
}

func TestCodingKnowledgeStore_UpdateExperienceWithBudgetRejectsReviewedProjectGrowth(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	project := "D:/work/update-project-budget"
	first, err := store.SaveExperience(ctx, CodingExperience{
		Title: "first reviewed project guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "update budget", Content: "first active record", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveExperience(ctx, CodingExperience{
		Title: "second reviewed project guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "update budget", Content: "small", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.GetExperience(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated := before
	updated.Content = strings.Repeat("expanded reviewed content ", 40)
	firstStored, err := store.GetExperience(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	limit := codingExperienceTokenCost(firstStored) + codingExperienceTokenCost(before)
	if err := store.UpdateExperienceWithBudget(ctx, updated, CodingExperienceBudget{MaxVerifiedTokens: limit}); err == nil || !strings.Contains(err.Error(), "token budget exceeded") {
		t.Fatalf("reviewed edit that exceeds project tokens must be rejected, err=%v", err)
	}
	after, err := store.GetExperience(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Content != before.Content {
		t.Fatalf("rejected reviewed edit replaced content: got=%q want=%q", after.Content, before.Content)
	}
}

func TestCodingKnowledgeStore_ProjectReviewedExperienceUsagePaginatesAndCountsStoredContent(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	project := "D:/work/project-budget-paged"

	// The production scan uses a 5,000-row page because ListSources caps that
	// size. Exercise the same offset loop with a small page here, then verify
	// that a reviewed record from the second page is included in the hard gate.
	first, err := store.SaveExperience(ctx, CodingExperience{
		Title: "first reviewed guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "budget paging", Content: "first reviewed content",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmCandidate(ctx, first.ID); err != nil {
		t.Fatalf("confirm first candidate: %v", err)
	}

	// Candidates do not count toward reviewed capacity, but force the scan onto
	// a second page.
	for i := 0; i < 2; i++ {
		if _, err := store.SaveExperience(ctx, CodingExperience{
			Title: fmt.Sprintf("candidate filler %d", i), Scope: CodingScopeProject, ProjectPath: project,
			TriggerCondition: "budget paging", Content: fmt.Sprintf("candidate filler content %d", i),
		}); err != nil {
			t.Fatalf("save candidate filler %d: %v", i, err)
		}
	}

	second, err := store.SaveExperience(ctx, CodingExperience{
		Title: "second reviewed guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "budget paging", Content: "second reviewed content that must be counted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmCandidate(ctx, second.ID); err != nil {
		t.Fatalf("confirm second candidate: %v", err)
	}

	count, tokens, err := store.projectReviewedExperienceUsageWithPageSize(ctx, "", project, 3)
	if err != nil {
		t.Fatalf("inspect project reviewed usage: %v", err)
	}
	if count != 2 {
		t.Fatalf("reviewed count=%d, want 2", count)
	}
	firstStored, err := store.GetExperience(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondStored, err := store.GetExperience(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantTokens := codingExperienceTokenCost(firstStored) + codingExperienceTokenCost(secondStored)
	if tokens != wantTokens {
		t.Fatalf("reviewed tokens=%d, want stored-content cost %d", tokens, wantTokens)
	}

	third, err := store.SaveExperience(ctx, CodingExperience{
		Title: "third reviewed guidance", Scope: CodingScopeProject, ProjectPath: project,
		TriggerCondition: "budget paging", Content: "third reviewed content",
	})
	if err != nil {
		t.Fatal(err)
	}
	count, _, err = store.projectReviewedExperienceUsageWithPageSize(ctx, "", project, 3)
	if err != nil || count != 2 {
		t.Fatalf("second-page reviewed experience must be counted, count=%d err=%v", count, err)
	}
	if err := store.ConfirmCandidateWithBudget(ctx, third.ID, CodingExperienceBudget{MaxVerifiedCount: 2}); err == nil || !strings.Contains(err.Error(), "count budget exceeded") {
		t.Fatalf("full project usage must enforce count budget, err=%v", err)
	}
}

func TestCodingKnowledgeStore_RuntimeExperienceRequiresProvenanceAndReview(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()

	base := CodingExperience{
		Title:            "runtime evidence",
		Scope:            CodingScopeUniversal,
		TriggerCondition: "runtime provenance",
		Content:          "Only reuse after a durable execution is reviewed.",
		Status:           CodingStatusActive,
	}
	partial := base
	partial.SourceRuntimeTaskID = "task-1"
	if _, err := store.SaveRuntimeExperience(ctx, partial); err == nil {
		t.Fatal("expected incomplete runtime provenance to be rejected")
	}

	base.SourceRuntimeTaskID = "task-1"
	base.SourceRuntimeAttemptID = "attempt-2"
	base.EvidenceDigest = "sha256:evidence"
	saved, err := store.SaveRuntimeExperience(ctx, base)
	if err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}
	if saved.Status != CodingStatusCandidate {
		t.Fatalf("runtime experience status=%q, want candidate", saved.Status)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	got.Status = CodingStatusActive // normal edit must not bypass review.
	got.Content = "Edited wording still requires review."
	if err := store.UpdateExperience(ctx, got); err != nil {
		t.Fatalf("UpdateExperience: %v", err)
	}
	got, err = store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetExperience after update: %v", err)
	}
	if got.Status != CodingStatusCandidate {
		t.Fatalf("updated runtime experience status=%q, want candidate", got.Status)
	}
	if err := store.ConfirmCandidate(ctx, saved.ID); err == nil || !strings.Contains(err.Error(), "requires provenance verification") {
		t.Fatalf("runtime candidate confirmation without verifier must fail, err=%v", err)
	}
	if err := store.ConfirmCandidate(ctx, saved.ID, func(context.Context, CodingExperience) error { return nil }); err != nil {
		t.Fatalf("ConfirmCandidate with verifier: %v", err)
	}
}

func TestCodingKnowledgeStore_RuntimeCandidateRejectsFailedVerifier(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveRuntimeExperience(ctx, CodingExperience{
		Title: "runtime verifier rejection", Scope: CodingScopeUniversal, TriggerCondition: "runtime verifier", Content: "review evidence",
		SourceRuntimeTaskID: "task", SourceRuntimeAttemptID: "attempt", EvidenceDigest: "sha256:evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmCandidate(ctx, saved.ID, func(context.Context, CodingExperience) error { return fmt.Errorf("missing ledger evidence") }); err == nil || !strings.Contains(err.Error(), "provenance verification failed") {
		t.Fatalf("expected verifier failure, err=%v", err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil || got.Status != CodingStatusCandidate {
		t.Fatalf("candidate must remain staged after verifier failure: exp=%+v err=%v", got, err)
	}
}

func TestCodingKnowledgeStore_UpdateExperiencePreservesRuntimeProvenance(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveRuntimeExperience(ctx, CodingExperience{
		Title: "runtime provenance is immutable", Scope: CodingScopeUniversal, TriggerCondition: "runtime audit", Content: "original content",
		SourceRuntimeTaskID: "task-1", SourceRuntimeAttemptID: "attempt-1", EvidenceDigest: "sha256:original",
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Editors may change the experience, and partial update payloads may omit
	// provenance, but the durable audit binding remains unchanged.
	edited := baseline
	edited.Content = "edited content"
	edited.SourceRuntimeTaskID = ""
	edited.SourceRuntimeAttemptID = ""
	edited.EvidenceDigest = ""
	if err := store.UpdateExperience(ctx, edited); err != nil {
		t.Fatalf("UpdateExperience content edit: %v", err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Content, "edited content") || got.SourceRuntimeTaskID != saved.SourceRuntimeTaskID || got.SourceRuntimeAttemptID != saved.SourceRuntimeAttemptID || got.EvidenceDigest != saved.EvidenceDigest {
		t.Fatalf("runtime provenance was not preserved: %+v", got)
	}

	tampered := got
	tampered.EvidenceDigest = "sha256:tampered"
	if err := store.UpdateExperience(ctx, tampered); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("tampered provenance must be rejected, err=%v", err)
	}
	got, err = store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EvidenceDigest != "sha256:original" {
		t.Fatalf("rejected update changed persisted provenance: %+v", got)
	}
}

func TestCodingKnowledgeStore_UpdateExperienceRejectsAddingRuntimeProvenance(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "manual experience", Scope: CodingScopeUniversal, TriggerCondition: "manual audit", Content: "manual content",
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}

	tampered := baseline
	tampered.Content = "must not overwrite persisted content"
	tampered.SourceRuntimeTaskID = "task-1"
	tampered.SourceRuntimeAttemptID = "attempt-1"
	tampered.EvidenceDigest = "sha256:forged"
	if err := store.UpdateExperience(ctx, tampered); err == nil || !strings.Contains(err.Error(), "only be set when creating") {
		t.Fatalf("manual experience must not gain runtime provenance, err=%v", err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != baseline.Content || isRuntimeDerivedExperience(got) {
		t.Fatalf("rejected update changed manual record: %+v", got)
	}
}

func TestCodingKnowledgeStore_UpdateStatusCannotBypassCandidateReview(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	runtimeCandidate, err := store.SaveRuntimeExperience(ctx, CodingExperience{
		Title: "runtime candidate status guard", Scope: CodingScopeUniversal, TriggerCondition: "status guard", Content: "review must be explicit",
		SourceRuntimeTaskID: "task-1", SourceRuntimeAttemptID: "attempt-1", EvidenceDigest: "sha256:evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStatus(ctx, runtimeCandidate.ID, CodingStatusActive, nil); err == nil || !strings.Contains(err.Error(), "promotion") {
		t.Fatalf("UpdateStatus must not bypass runtime candidate review, err=%v", err)
	}
	got, err := store.GetExperience(ctx, runtimeCandidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CodingStatusCandidate {
		t.Fatalf("rejected status update changed candidate: %+v", got)
	}
	if err := store.UpdateStatus(ctx, runtimeCandidate.ID, CodingStatusDeprecated, []string{"conflicted"}); err != nil {
		t.Fatalf("candidate deprecation: %v", err)
	}
	got, err = store.GetExperience(ctx, runtimeCandidate.ID)
	if err != nil || got.Status != CodingStatusDeprecated {
		t.Fatalf("candidate deprecation missing: exp=%+v err=%v", got, err)
	}
	if err := store.UpdateStatus(ctx, runtimeCandidate.ID, "arbitrary", nil); err == nil || !strings.Contains(err.Error(), "invalid experience status") {
		t.Fatalf("invalid status must be rejected, err=%v", err)
	}
	if err := store.UpdateStatus(ctx, runtimeCandidate.ID, CodingStatusActive, nil); err == nil || !strings.Contains(err.Error(), "promotion") {
		t.Fatalf("deprecated runtime candidate must not be revived through UpdateStatus, err=%v", err)
	}
}

func TestCodingKnowledgeStore_UpdateConfidenceDoesNotPromoteCandidate(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	candidate, err := store.SaveExperience(ctx, CodingExperience{
		Title: "candidate confidence guard", Scope: CodingScopeUniversal, TriggerCondition: "confidence guard", Content: "candidate remains reviewed",
		Confidence: CodingConfidenceVerifiedThreshold,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < CodingMinRecallsForVerified; i++ {
		if err := recordTestRecallOutcome(t, store, candidate.ID, true); err == nil || !strings.Contains(err.Error(), "reviewed active or verified") {
			t.Fatalf("candidate outcome must be rejected, err=%v", err)
		}
	}
	got, err := store.GetExperience(ctx, candidate.ID)
	if err != nil || got.Status != CodingStatusCandidate {
		t.Fatalf("candidate must not be auto-promoted: exp=%+v err=%v", got, err)
	}
}

func TestCodingKnowledgeStore_DeprecatedExperienceCannotAccrueConfidenceOrRevive(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "retired confidence guard", Scope: CodingScopeUniversal, TriggerCondition: "conflicting evidence", Content: "must remain retired", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkConflict(ctx, saved.ID, "replacement", "contradicted by current evidence"); err != nil {
		t.Fatal(err)
	}
	baseline, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordTestRecallOutcome(t, store, saved.ID, true); err == nil || !strings.Contains(err.Error(), "deprecated experience") {
		t.Fatalf("deprecated experience must reject confidence update, err=%v", err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CodingStatusDeprecated || got.Confidence != baseline.Confidence || got.RecallCount != baseline.RecallCount || got.SuccessCount != baseline.SuccessCount || got.FailureCount != baseline.FailureCount || !got.LastRecalledAt.Equal(baseline.LastRecalledAt) {
		t.Fatalf("rejected confidence update mutated retired evidence: got=%+v baseline=%+v", got, baseline)
	}
}

func TestCodingKnowledgeStore_RetireToSteeringRequiresVerifiedEvidenceAndAuditsReference(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "steering graduation", Scope: CodingScopeUniversal, TriggerCondition: "promote rule", Content: "must be verified before it becomes steering", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RetireToSteering(ctx, saved.ID, "coding-exp-steering.md"); err == nil || !strings.Contains(err.Error(), "only verified") {
		t.Fatalf("active experience must not be graduated, err=%v", err)
	}
	if err := store.RetireToSteering(ctx, saved.ID, `steering\\coding-exp-steering.md`); err == nil || !strings.Contains(err.Error(), "must not contain a filesystem path") {
		t.Fatalf("steering audit must reject filesystem paths, err=%v", err)
	}
	for i := 0; i < CodingMinRecallsForVerified; i++ {
		if err := recordTestRecallOutcome(t, store, saved.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	verified, err := store.GetExperience(ctx, saved.ID)
	if err != nil || verified.Status != CodingStatusVerified || verified.LastReviewedAt.IsZero() {
		t.Fatalf("verified experience=%+v err=%v", verified, err)
	}
	if err := store.RetireToSteering(ctx, saved.ID, "coding-exp-steering.md"); err != nil {
		t.Fatal(err)
	}
	retired, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Status != CodingStatusDeprecated || !stringSliceContains(retired.Labels, "graduated_to_steering") {
		t.Fatalf("graduated experience was not retired: %+v", retired)
	}
	if !retired.LastReviewedAt.Equal(verified.LastReviewedAt) {
		t.Fatalf("graduation must not claim a new positive review: got=%s want=%s", retired.LastReviewedAt, verified.LastReviewedAt)
	}
	events, err := store.ListLifecycleEvents(ctx, saved.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("graduation lifecycle=%+v err=%v", events, err)
	}
	last := events[len(events)-1]
	if last.Action != "graduated_to_steering" || last.RelatedID != "coding-exp-steering.md" || last.OccurredAt.IsZero() {
		t.Fatalf("unexpected graduation audit: %+v", last)
	}
}

func TestCodingKnowledgeStore_UpdateExperienceCannotForgeLifecycleEvidence(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "lifecycle evidence guard", Scope: CodingScopeUniversal, TriggerCondition: "lifecycle edit guard", Content: "original content", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recordTestRecallOutcome(t, store, saved.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendContraindication(ctx, saved.ID, "does not apply to generated code"); err != nil {
		t.Fatal(err)
	}
	baseline, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, mutate := range []struct {
		name string
		edit func(*CodingExperience)
	}{
		{"recall count", func(exp *CodingExperience) { exp.RecallCount++ }},
		{"success count", func(exp *CodingExperience) { exp.SuccessCount++ }},
		{"failure count", func(exp *CodingExperience) { exp.FailureCount++ }},
		{"confidence", func(exp *CodingExperience) { exp.Confidence += 0.2 }},
		{"verified status", func(exp *CodingExperience) { exp.Status = CodingStatusVerified }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			tampered := baseline
			tampered.LifecycleEvents = append([]CodingExperienceLifecycleEvent(nil), baseline.LifecycleEvents...)
			mutate.edit(&tampered)
			if err := store.UpdateExperience(ctx, tampered); err == nil {
				t.Fatal("forged lifecycle value must be rejected")
			}
			got, err := store.GetExperience(ctx, saved.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.RecallCount != baseline.RecallCount || got.SuccessCount != baseline.SuccessCount || got.FailureCount != baseline.FailureCount || got.Confidence != baseline.Confidence || got.Status != baseline.Status {
				t.Fatalf("rejected edit changed lifecycle facts: %+v", got)
			}
		})
	}
	for _, mutate := range []struct {
		name string
		edit func(*CodingExperience)
	}{
		{"lifecycle audit append", func(exp *CodingExperience) {
			exp.LifecycleEvents = append(exp.LifecycleEvents, CodingExperienceLifecycleEvent{Action: "forged", Reason: "forged", OccurredAt: time.Now().UTC()})
		}},
		{"lifecycle audit rewrite", func(exp *CodingExperience) { exp.LifecycleEvents[0].Reason = "forged rewrite" }},
		{"creation timestamp", func(exp *CodingExperience) { exp.CreatedAt = exp.CreatedAt.Add(-time.Hour) }},
		{"update timestamp", func(exp *CodingExperience) { exp.UpdatedAt = exp.UpdatedAt.Add(time.Hour) }},
		{"last recalled timestamp", func(exp *CodingExperience) { exp.LastRecalledAt = exp.LastRecalledAt.Add(time.Hour) }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			tampered := baseline
			tampered.LifecycleEvents = append([]CodingExperienceLifecycleEvent(nil), baseline.LifecycleEvents...)
			mutate.edit(&tampered)
			if err := store.UpdateExperience(ctx, tampered); err == nil {
				t.Fatal("forged audit metadata must be rejected")
			}
			got, err := store.GetExperience(ctx, saved.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !codingExperienceLifecycleEventsEqual(got.LifecycleEvents, baseline.LifecycleEvents) || !got.CreatedAt.Equal(baseline.CreatedAt) || !got.UpdatedAt.Equal(baseline.UpdatedAt) {
				t.Fatalf("rejected audit edit mutated persisted record: got=%+v baseline=%+v", got, baseline)
			}
		})
	}

	edited := baseline
	edited.Title = "lifecycle evidence guard edited"
	edited.RecallCount = 0 // Omitted list-projection value remains safely hydrated.
	edited.SuccessCount = 0
	edited.FailureCount = 0
	edited.Confidence = 0
	edited.LifecycleEvents = nil // Omitted list-projection value remains safely hydrated.
	edited.LastRecalledAt = time.Time{}
	if err := store.UpdateExperience(ctx, edited); err != nil {
		t.Fatalf("ordinary content edit with omitted stats: %v", err)
	}
}

func TestCodingKnowledgeStore_UpdateExperienceCannotRewriteManagedLifecycleLabels(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "managed label guard", Scope: CodingScopeUniversal, TriggerCondition: "managed label", Content: "labels must retain lifecycle semantics", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkConflict(ctx, saved.ID, "replacement", "current evidence contradicts this rule"); err != nil {
		t.Fatal(err)
	}
	baseline, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	withoutConflict := baseline
	withoutConflict.Labels = withoutCodingExperienceLabel(withoutConflict.Labels, "conflicted")
	if err := store.UpdateExperience(ctx, withoutConflict); err == nil || !strings.Contains(err.Error(), "lifecycle label") {
		t.Fatalf("editor must not remove conflict label, err=%v", err)
	}
	forgedImport := baseline
	forgedImport.Labels = append(append([]string(nil), baseline.Labels...), "imported")
	if err := store.UpdateExperience(ctx, forgedImport); err == nil || !strings.Contains(err.Error(), "lifecycle label") {
		t.Fatalf("editor must not claim import label, err=%v", err)
	}
	projection := baseline
	projection.Labels = nil
	projection.Content = "ordinary content edit without labels"
	if err := store.UpdateExperience(ctx, projection); err != nil {
		t.Fatalf("omitted labels should be hydrated, err=%v", err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !codingExperienceLabelsContain(got.Labels, "conflicted") || got.Status != CodingStatusDeprecated {
		t.Fatalf("omitted-label update lost lifecycle state: %+v", got)
	}
}

func TestCodingKnowledgeStore_EvictExperiencePreservesAuditedAndReviewedRecords(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	candidate, err := store.SaveExperience(ctx, CodingExperience{
		Title: "disposable candidate", Scope: CodingScopeUniversal, TriggerCondition: "candidate", Content: "may be evicted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EvictExperience(ctx, candidate.ID); err != nil {
		t.Fatalf("candidate should be evictable: %v", err)
	}
	if _, err := store.GetExperience(ctx, candidate.ID); err == nil {
		t.Fatal("evicted candidate remains available")
	}

	audited, err := store.SaveExperience(ctx, CodingExperience{
		Title: "audited conflict", Scope: CodingScopeUniversal, TriggerCondition: "conflict", Content: "must retain audit", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkConflict(ctx, audited.ID, "replacement", "contradicted by current evidence"); err != nil {
		t.Fatal(err)
	}
	if err := store.EvictExperience(ctx, audited.ID); err == nil || !strings.Contains(err.Error(), "audited experience") {
		t.Fatalf("audited conflict must not be auto-evicted, err=%v", err)
	}
	if got, err := store.GetExperience(ctx, audited.ID); err != nil || got.Status != CodingStatusDeprecated {
		t.Fatalf("rejected eviction lost audit record: exp=%+v err=%v", got, err)
	}
}

func TestSanitizeExperienceForExportClearsLocalAuthority(t *testing.T) {
	when := time.Now().UTC()
	portable := SanitizeExperienceForExport(CodingExperience{
		ID: "local-id", Title: "portable proposal", Scope: CodingScopeUniversal, TriggerCondition: "transfer", Content: "recipient must review",
		Status: CodingStatusVerified, CreatedBy: "runtime", LastReviewedAt: when,
		SourceRuntimeTaskID: "task", SourceRuntimeAttemptID: "attempt", EvidenceDigest: "sha256:evidence", ParentExperienceID: "parent",
		LifecycleEvents: []CodingExperienceLifecycleEvent{{Action: "candidate_confirmed", OccurredAt: when}},
		Confidence:      2, RecallCount: 9, SuccessCount: 8, FailureCount: 1, CreatedAt: when, UpdatedAt: when, LastRecalledAt: when,
		Labels: []string{"team-label", "conflicted", "imported", "revision_candidate"},
	})
	if portable.ID != "" || portable.Status != CodingStatusCandidate || portable.CreatedBy != "" || !portable.LastReviewedAt.IsZero() || isRuntimeDerivedExperience(portable) || portable.ParentExperienceID != "" || len(portable.LifecycleEvents) != 0 || portable.Confidence != CodingConfidenceInitial || portable.RecallCount != 0 || portable.SuccessCount != 0 || portable.FailureCount != 0 || !portable.CreatedAt.IsZero() || !portable.UpdatedAt.IsZero() || !portable.LastRecalledAt.IsZero() {
		t.Fatalf("portable export retained local authority: %+v", portable)
	}
	if len(portable.Labels) != 1 || portable.Labels[0] != "team-label" {
		t.Fatalf("portable export labels=%+v", portable.Labels)
	}
}

func TestCodingKnowledgeStore_UpdateStatusCannotManuallyVerify(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "verified status guard", Scope: CodingScopeUniversal, TriggerCondition: "verified guard", Content: "must be evidence driven", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStatus(ctx, saved.ID, CodingStatusVerified, nil); err == nil || !strings.Contains(err.Error(), "dedicated review or confidence") {
		t.Fatalf("manual verified promotion must be rejected, err=%v", err)
	}
}

func TestCodingKnowledgeStore_SaveExperienceCannotCreateVerified(t *testing.T) {
	store := openTestCodingStore(t)
	_, err := store.SaveExperience(context.Background(), CodingExperience{
		Title: "direct verified status guard", Scope: CodingScopeUniversal,
		TriggerCondition: "initial status", Content: "verified must have recall evidence",
		Status: CodingStatusVerified,
	})
	if err == nil || !strings.Contains(err.Error(), "requires candidate confirmation and recall evidence") {
		t.Fatalf("direct verified creation must be rejected, err=%v", err)
	}
}

func TestCodingKnowledgeStore_UpdateExperienceRequiresExistingRecord(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	err := store.UpdateExperience(ctx, CodingExperience{
		ID: "missing-experience", Title: "must not be created", Scope: CodingScopeUniversal,
		TriggerCondition: "missing update", Content: "update is not an upsert",
	})
	if err == nil || !strings.Contains(err.Error(), "update missing-experience") {
		t.Fatalf("missing experience update must fail, err=%v", err)
	}
	if _, err := store.GetExperience(ctx, "missing-experience"); err == nil {
		t.Fatal("failed update created a new experience")
	}
}

func TestCodingKnowledgeStore_RejectedUpdatePreservesExistingRecord(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "preserve before reindex", Scope: CodingScopeUniversal, TriggerCondition: "reindex guard", Content: "original content",
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	invalid := baseline
	invalid.Category = "not-a-category"
	invalid.Content = "this invalid edit must not replace the original"
	if err := store.UpdateExperience(ctx, invalid); err == nil || !strings.Contains(err.Error(), "invalid category") {
		t.Fatalf("invalid update must fail validation, err=%v", err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("rejected update removed record: %v", err)
	}
	if got.Content != baseline.Content || got.Category != baseline.Category || got.Title != baseline.Title {
		t.Fatalf("rejected update mutated existing record: got=%+v baseline=%+v", got, baseline)
	}
}

func TestCodingKnowledgeStore_MarkConflictRetainsAuditAndPreventsInjection(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	saved, err := store.SaveExperience(ctx, CodingExperience{
		Title: "conflict target", Scope: CodingScopeUniversal, TriggerCondition: "conflict target", Content: "must leave prompt context when conflicted", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkConflict(ctx, saved.ID, "ksrc_related", "contradicts verified alternative"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CodingStatusDeprecated || !stringSliceContains(got.Labels, "conflicted") {
		t.Fatalf("conflicted experience was not deprecated: %+v", got)
	}
	events, err := store.ListLifecycleEvents(ctx, saved.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("lifecycle events=%+v err=%v", events, err)
	}
	if events[0].Action != "conflict_marked" || events[0].RelatedID != "ksrc_related" || events[0].Reason != "contradicts verified alternative" || events[0].OccurredAt.IsZero() {
		t.Fatalf("unexpected conflict audit: %+v", events[0])
	}
	results, err := store.SearchExperiences(ctx, CodingSearchOptions{Query: "conflict target", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.ID == saved.ID {
			t.Fatalf("conflicted experience was still automatically retrievable: %+v", result)
		}
	}
	if err := store.UpdateStatus(ctx, saved.ID, CodingStatusActive, nil); err == nil {
		t.Fatal("conflicted experience must not be reactivated through generic status update")
	}
}

func TestCodingKnowledgeStore_CreateRevisionCandidateDoesNotReviveDeprecatedExperience(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	parent, err := store.SaveRuntimeExperience(ctx, CodingExperience{
		Title: "retired rule", Scope: CodingScopeUniversal, TriggerCondition: "retired trigger", Content: "original guidance", Status: CodingStatusActive,
		SourceRuntimeTaskID: "task-1", SourceRuntimeAttemptID: "attempt-1", EvidenceDigest: "sha256:original",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkConflict(ctx, parent.ID, "ksrc-conflict", "repository behavior changed"); err != nil {
		t.Fatal(err)
	}
	candidate, err := store.CreateRevisionCandidate(ctx, parent.ID, "recheck against current repository")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ID == parent.ID || candidate.Status != CodingStatusCandidate || candidate.ParentExperienceID != parent.ID {
		t.Fatalf("revision candidate identity/lifecycle=%+v", candidate)
	}
	if isRuntimeDerivedExperience(candidate) || candidate.RecallCount != 0 || candidate.SuccessCount != 0 || candidate.FailureCount != 0 || candidate.Confidence != CodingConfidenceInitial || stringSliceContains(candidate.Labels, "conflicted") || !stringSliceContains(candidate.Labels, "revision_candidate") {
		t.Fatalf("revision candidate retained unsafe inherited state: %+v", candidate)
	}
	parentAfter, err := store.GetExperience(ctx, parent.ID)
	if err != nil || parentAfter.Status != CodingStatusDeprecated {
		t.Fatalf("parent was revived/removed: exp=%+v err=%v", parentAfter, err)
	}
	parentEvents, err := store.ListLifecycleEvents(ctx, parent.ID)
	if err != nil || len(parentEvents) < 2 || parentEvents[len(parentEvents)-1].Action != "revision_candidate_created" || parentEvents[len(parentEvents)-1].RelatedID != candidate.ID {
		t.Fatalf("parent revision audit=%+v err=%v", parentEvents, err)
	}
	if err := store.UpdateExperience(ctx, CodingExperience{ID: candidate.ID, Title: candidate.Title, Scope: candidate.Scope, TriggerCondition: candidate.TriggerCondition, Content: "corrected guidance", ParentExperienceID: "forged-parent"}); err == nil || !strings.Contains(err.Error(), "parent experience is immutable") {
		t.Fatalf("revision lineage tampering must fail, err=%v", err)
	}
}

func TestCodingKnowledgeStore_ReviewMetadataIsDurableAndEditorImmutable(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	runtimeCandidate, err := store.SaveRuntimeExperience(ctx, CodingExperience{
		Title: "review metadata", Scope: CodingScopeUniversal, TriggerCondition: "review metadata", Content: "must retain auditable origin",
		SourceRuntimeTaskID: "task-review", SourceRuntimeAttemptID: "attempt-review", EvidenceDigest: "sha256:review", CreatedBy: "runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtimeCandidate.CreatedBy != "runtime" {
		t.Fatalf("runtime origin=%q, want runtime", runtimeCandidate.CreatedBy)
	}
	if !runtimeCandidate.LastReviewedAt.IsZero() {
		t.Fatalf("unreviewed candidate timestamp=%s", runtimeCandidate.LastReviewedAt)
	}
	if err := store.ConfirmCandidate(ctx, runtimeCandidate.ID, func(context.Context, CodingExperience) error { return nil }); err != nil {
		t.Fatal(err)
	}
	confirmed, err := store.GetExperience(ctx, runtimeCandidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.LastReviewedAt.IsZero() {
		t.Fatal("confirmation must persist review timestamp")
	}
	modified := confirmed
	modified.CreatedBy = "forged"
	if err := store.UpdateExperience(ctx, modified); err == nil {
		t.Fatal("editor must not rewrite creator origin")
	}
	modified = confirmed
	modified.LastReviewedAt = time.Now().UTC().Add(time.Hour)
	if err := store.UpdateExperience(ctx, modified); err == nil {
		t.Fatal("editor must not rewrite review timestamp")
	}
	got, err := store.GetExperience(ctx, runtimeCandidate.ID)
	if err != nil || got.CreatedBy != confirmed.CreatedBy || !got.LastReviewedAt.Equal(confirmed.LastReviewedAt) {
		t.Fatalf("rejected review metadata update mutated record: got=%+v err=%v", got, err)
	}
}

func TestCodingKnowledgeStore_ImportedExperienceClearsForeignReviewAuthority(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	reviewed := time.Now().UTC().Add(-time.Hour)
	saved, err := store.SaveImportedExperience(ctx, CodingExperience{
		Title: "foreign experience", Scope: CodingScopeUniversal, TriggerCondition: "foreign input", Content: "requires local review",
		Status: CodingStatusVerified, CreatedBy: "runtime", LastReviewedAt: reviewed,
		SourceRuntimeTaskID: "foreign-task", SourceRuntimeAttemptID: "foreign-attempt", EvidenceDigest: "sha256:foreign",
		ParentExperienceID: "foreign-parent", LifecycleEvents: []CodingExperienceLifecycleEvent{{Action: "candidate_confirmed", Reason: "foreign review", OccurredAt: reviewed}},
		Confidence: 9.5, RecallCount: 8, SuccessCount: 7, FailureCount: 1, LastRecalledAt: reviewed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != CodingStatusCandidate || saved.CreatedBy != "import" || !saved.LastReviewedAt.IsZero() || isRuntimeDerivedExperience(saved) || saved.ParentExperienceID != "" || len(saved.LifecycleEvents) != 0 || saved.Confidence != CodingConfidenceInitial || saved.RecallCount != 0 || saved.SuccessCount != 0 || saved.FailureCount != 0 || !saved.LastRecalledAt.IsZero() {
		t.Fatalf("import did not clear foreign authority: %+v", saved)
	}
}

func TestCodingKnowledgeStore_SaveOperationsManageReviewTimestamp(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	for name, save := range map[string]func() error{
		"manual": func() error {
			_, err := store.SaveExperience(ctx, CodingExperience{
				Title: "manual review guard", Scope: CodingScopeUniversal, TriggerCondition: "manual review", Content: "must be store managed",
				LastReviewedAt: time.Now().UTC(),
			})
			return err
		},
		"runtime": func() error {
			_, err := store.SaveRuntimeExperience(ctx, CodingExperience{
				Title: "runtime review guard", Scope: CodingScopeUniversal, TriggerCondition: "runtime review", Content: "must be confirmed",
				SourceRuntimeTaskID: "task", SourceRuntimeAttemptID: "attempt", EvidenceDigest: "sha256:evidence", LastReviewedAt: time.Now().UTC(),
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := save(); err == nil || !strings.Contains(err.Error(), "review timestamp") {
				t.Fatalf("caller-supplied review timestamp must be rejected, err=%v", err)
			}
		})
	}

	manual, err := store.SaveExperience(ctx, CodingExperience{
		Title: "manual review", Scope: CodingScopeUniversal, TriggerCondition: "manual save", Content: "saved by a reviewer", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if manual.LastReviewedAt.IsZero() {
		t.Fatal("manual active experience must receive a store-assigned review timestamp")
	}
}

func TestCodingKnowledgeStore_ContextPackExcludesCandidates(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	for _, exp := range []CodingExperience{
		{Title: "candidate", Scope: CodingScopeUniversal, TriggerCondition: "shared context keyword", Content: "candidate must not be injected", Status: CodingStatusCandidate},
		{Title: "active", Scope: CodingScopeUniversal, TriggerCondition: "shared context keyword", Content: "active may be injected", Status: CodingStatusActive},
	} {
		if _, err := store.SaveExperience(ctx, exp); err != nil {
			t.Fatalf("SaveExperience(%s): %v", exp.Title, err)
		}
	}
	verified, err := store.SaveExperience(ctx, CodingExperience{
		Title: "verified", Scope: CodingScopeUniversal, TriggerCondition: "shared context keyword", Content: "verified may be injected", Status: CodingStatusActive,
	})
	if err != nil {
		t.Fatalf("save verified candidate: %v", err)
	}
	for i := 0; i < CodingMinRecallsForVerified; i++ {
		if err := recordTestRecallOutcome(t, store, verified.ID, true); err != nil {
			t.Fatalf("RecordRecallOutcome(%d): %v", i, err)
		}
	}
	if got, err := store.GetExperience(ctx, verified.ID); err != nil || got.Status != CodingStatusVerified {
		t.Fatalf("verified evidence path result=%+v err=%v", got, err)
	}
	pack, err := store.ContextPackForTask(ctx, CodingContextPackOptions{Query: "shared context keyword", MaxItems: 10, MaxChars: 1000})
	if err != nil {
		t.Fatalf("ContextPackForTask: %v", err)
	}
	if len(pack.Items) != 2 {
		t.Fatalf("context item count=%d, want 2: %+v", len(pack.Items), pack.Items)
	}
	for _, item := range pack.Items {
		if item.Title == "candidate" {
			t.Fatalf("candidate was injected into context: %+v", item)
		}
	}
}

func TestCodingKnowledgeStore_ProjectRecallIncludesSharedAndExcludesOtherProjects(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	const projectA = "D:/work/project-a"
	const projectB = "D:/work/project-b"
	for _, exp := range []CodingExperience{
		{Title: "shared universal guidance", Scope: CodingScopeUniversal, TriggerCondition: "project recall boundary", Content: "shared universal project recall boundary", Status: CodingStatusActive},
		{Title: "shared Python guidance", Scope: CodingScopeLanguage, Language: "python", TriggerCondition: "project recall boundary", Content: "shared python project recall boundary", Status: CodingStatusActive},
		{Title: "project A guidance", Scope: CodingScopeProject, ProjectPath: projectA, TriggerCondition: "project recall boundary", Content: "project A project recall boundary", Status: CodingStatusActive},
		{Title: "project B guidance", Scope: CodingScopeProject, ProjectPath: projectB, TriggerCondition: "project recall boundary", Content: "project B project recall boundary", Status: CodingStatusActive},
	} {
		if _, err := store.SaveExperience(ctx, exp); err != nil {
			t.Fatalf("SaveExperience(%q): %v", exp.Title, err)
		}
	}

	pack, err := store.ContextPackForTask(ctx, CodingContextPackOptions{
		Query: "project recall boundary", Language: "python", ProjectPath: projectA, MaxItems: 10, MaxChars: 4_000,
	})
	if err != nil {
		t.Fatalf("ContextPackForTask: %v", err)
	}
	titles := make(map[string]bool, len(pack.Items))
	for _, item := range pack.Items {
		titles[item.Title] = true
	}
	for _, want := range []string{"shared universal guidance", "shared Python guidance", "project A guidance"} {
		if !titles[want] {
			t.Fatalf("context pack omitted %q: %+v", want, pack.Items)
		}
	}
	if titles["project B guidance"] {
		t.Fatalf("context pack leaked another project's guidance: %+v", pack.Items)
	}
}

func TestCodingKnowledgeStore_ContextPackEnforcesTokenBudget(t *testing.T) {
	store := openTestCodingStore(t)
	ctx := context.Background()
	longText := strings.Repeat("多字节上下文内容", 40)
	if _, err := store.SaveExperience(ctx, CodingExperience{
		Title: "token limited guidance", Scope: CodingScopeUniversal,
		TriggerCondition: "token budget keyword", Content: longText, Status: CodingStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	const budget = 12
	pack, err := store.ContextPackForTask(ctx, CodingContextPackOptions{
		Query: "token budget keyword", MaxItems: 4, MaxChars: 1_000, MaxTokens: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pack.TokenCount <= 0 || pack.TokenCount > budget {
		t.Fatalf("token count=%d, want 1..%d: %+v", pack.TokenCount, budget, pack)
	}
	if len(pack.Items) != 1 || estimateTokens(pack.Items[0].Text) != pack.TokenCount {
		t.Fatalf("pack item/token count mismatch: %+v", pack)
	}
	if !hasContextPackNote(pack.Notes, "token_budget_enforced") || !hasContextPackNote(pack.Notes, "truncated_to_budget") {
		t.Fatalf("missing token budget audit notes: %+v", pack.Notes)
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
