package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

var codingKnowledgeAppTestOutcomeSequence uint64

func recordCodingKnowledgeAppTestOutcome(t *testing.T, app *App, experienceID string, succeeded bool) error {
	t.Helper()
	ledger := app.ensureCodingRuntimeStore()
	if ledger == nil {
		return fmt.Errorf("runtime ledger unavailable")
	}
	sequence := atomic.AddUint64(&codingKnowledgeAppTestOutcomeSequence, 1)
	taskID := fmt.Sprintf("knowledge-outcome-%d", sequence)
	now := time.Now().UTC()
	task, err := ledger.CreateTask(codingruntime.Task{TaskID: taskID, ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		return err
	}
	attempt, err := ledger.StartAttempt(task.TaskID, "test", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		return err
	}
	if _, err := ledger.AppendEvent(attempt.AttemptID, "test", "verification", fmt.Sprintf("sha256:knowledge-outcome-%d", sequence), now); err != nil {
		return err
	}
	if _, err := ledger.FinishAttempt(attempt.AttemptID, "test", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectConfirmed}, now); err != nil {
		return err
	}
	provenance, err := codingruntime.ResolveExperienceProvenance(ledger, task.TaskID)
	if err != nil {
		return err
	}
	return app.CodingKnowledgeRecordRecallOutcome(experienceID, knowledge.RecallOutcome{
		RuntimeTaskID: provenance.TaskID, RuntimeAttemptID: provenance.AttemptID, EvidenceDigest: provenance.EvidenceDigest, TaskSucceeded: succeeded,
	})
}

func closeCodingKnowledgeStore(t *testing.T, app *App) {
	t.Helper()
	codingKnowledgeStoreMu.Lock()
	defer codingKnowledgeStoreMu.Unlock()
	if app != nil && app.codingKnowledgeStore != nil {
		_ = app.codingKnowledgeStore.Close()
		app.codingKnowledgeStore = nil
	}
	if app != nil {
		app.closeCodingRuntimeStore()
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
	if saved.CreatedBy != "manual" || saved.Status != knowledge.CodingStatusCandidate || !saved.LastReviewedAt.IsZero() {
		t.Fatalf("manual new experience must be a locally authored unreviewed candidate: %+v", saved)
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

func TestCodingKnowledgeManualSaveRejectsRuntimeOriginOrProvenance(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })

	base := knowledge.CodingExperience{
		Title: "manual origin guard", Scope: knowledge.CodingScopeUniversal,
		TriggerCondition: "origin guard", Content: "manual input remains manual",
	}
	for name, exp := range map[string]knowledge.CodingExperience{
		"runtime provenance": func() knowledge.CodingExperience {
			candidate := base
			candidate.SourceRuntimeTaskID = "task"
			candidate.SourceRuntimeAttemptID = "attempt"
			candidate.EvidenceDigest = "sha256:evidence"
			return candidate
		}(),
		"runtime origin": func() knowledge.CodingExperience {
			candidate := base
			candidate.CreatedBy = "runtime"
			return candidate
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := app.CodingKnowledgeSave(exp); err == nil {
				t.Fatal("manual Wails save must reject Runtime authority")
			}
		})
	}

	saved, err := app.CodingKnowledgeSave(base)
	if err != nil {
		t.Fatalf("manual save: %v", err)
	}
	if saved.CreatedBy != "manual" {
		t.Fatalf("manual origin=%q, want manual", saved.CreatedBy)
	}
	if saved.Status != knowledge.CodingStatusCandidate {
		t.Fatalf("manual default status=%q, want candidate", saved.Status)
	}
	if _, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "forged verified", Scope: knowledge.CodingScopeUniversal,
		TriggerCondition: "initial status", Content: "must use recall evidence", Status: knowledge.CodingStatusVerified,
	}); err == nil {
		t.Fatal("manual Wails save must not create a verified experience directly")
	}
}

func TestCodingKnowledgeConfirmRevalidatesRuntimeProvenance(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		closeCodingKnowledgeStore(t, app)
		app.closeCodingRuntimeStore()
	})
	ledger := app.ensureCodingRuntimeStore()
	if ledger == nil {
		t.Fatal("runtime ledger unavailable")
	}
	now := time.Now().UTC()
	task, err := ledger.CreateTask(codingruntime.Task{TaskID: "knowledge-review", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ledger.StartAttempt(task.TaskID, "test", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AppendEvent(attempt.AttemptID, "test", "verification", "sha256:verified", now); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.FinishAttempt(attempt.AttemptID, "test", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectConfirmed}, now); err != nil {
		t.Fatal(err)
	}
	provenance, err := codingruntime.ResolveExperienceProvenance(ledger, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	store := app.ensureCodingKnowledgeStore()
	if store == nil {
		t.Fatal("coding knowledge store unavailable")
	}
	saved, err := store.SaveRuntimeExperience(context.Background(), knowledge.CodingExperience{
		Title: "runtime reviewed", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "ledger review", Content: "bound to a verified Runtime attempt",
		SourceRuntimeTaskID: provenance.TaskID, SourceRuntimeAttemptID: provenance.AttemptID, EvidenceDigest: provenance.EvidenceDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CodingKnowledgeConfirm(saved.ID); err != nil {
		t.Fatalf("runtime-backed confirmation: %v", err)
	}

	tampered, err := store.SaveRuntimeExperience(context.Background(), knowledge.CodingExperience{
		Title: "runtime tampered", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "ledger mismatch", Content: "must remain candidate",
		SourceRuntimeTaskID: provenance.TaskID, SourceRuntimeAttemptID: provenance.AttemptID, EvidenceDigest: "sha256:tampered",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CodingKnowledgeConfirm(tampered.ID); err == nil {
		t.Fatal("tampered runtime evidence must not confirm")
	}
	got, err := app.CodingKnowledgeGet(tampered.ID)
	if err != nil || got.Status != knowledge.CodingStatusCandidate {
		t.Fatalf("tampered candidate=%+v err=%v", got, err)
	}
}

func TestCodingKnowledgeRecordRecallOutcomeRevalidatesRuntimeEvidence(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		closeCodingKnowledgeStore(t, app)
		app.closeCodingRuntimeStore()
	})
	ledger := app.ensureCodingRuntimeStore()
	if ledger == nil {
		t.Fatal("runtime ledger unavailable")
	}
	now := time.Now().UTC()
	task, err := ledger.CreateTask(codingruntime.Task{TaskID: "recall-outcome", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ledger.StartAttempt(task.TaskID, "test", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AppendEvent(attempt.AttemptID, "test", "verification", "sha256:recall", now); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.FinishAttempt(attempt.AttemptID, "test", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectConfirmed}, now); err != nil {
		t.Fatal(err)
	}
	provenance, err := codingruntime.ResolveExperienceProvenance(ledger, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "recall evidence binding", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "record outcome", Content: "must use a verified runtime outcome", Status: knowledge.CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CodingKnowledgeRecordRecallOutcome(saved.ID, knowledge.RecallOutcome{RuntimeTaskID: provenance.TaskID, RuntimeAttemptID: provenance.AttemptID, EvidenceDigest: provenance.EvidenceDigest, TaskSucceeded: true}); err != nil {
		t.Fatalf("record verified runtime outcome: %v", err)
	}
	if err := app.CodingKnowledgeRecordRecallOutcome(saved.ID, knowledge.RecallOutcome{RuntimeTaskID: provenance.TaskID, RuntimeAttemptID: provenance.AttemptID, EvidenceDigest: "sha256:tampered", TaskSucceeded: true}); err == nil {
		t.Fatal("tampered outcome digest must be rejected")
	}
	got, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil || got.RecallCount != 1 || got.SuccessCount != 1 {
		t.Fatalf("recall evidence result=%+v err=%v", got, err)
	}
}

func TestCodingKnowledgeUpdateCannotRewriteRuntimeProvenance(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	store := app.ensureCodingKnowledgeStore()
	if store == nil {
		t.Fatal("coding knowledge store unavailable")
	}
	saved, err := store.SaveRuntimeExperience(context.Background(), knowledge.CodingExperience{
		Title: "runtime provenance ui edit", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "ui audit", Content: "original",
		SourceRuntimeTaskID: "task-1", SourceRuntimeAttemptID: "attempt-1", EvidenceDigest: "sha256:original",
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil {
		t.Fatal(err)
	}

	payload := baseline
	payload.Content = "edited through Wails binding"
	payload.SourceRuntimeTaskID = "task-2"
	if err := app.CodingKnowledgeUpdate(payload); err == nil {
		t.Fatal("UI update must reject a rewritten runtime task ID")
	}
	got, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != baseline.Content || got.SourceRuntimeTaskID != "task-1" || got.SourceRuntimeAttemptID != "attempt-1" || got.EvidenceDigest != "sha256:original" {
		t.Fatalf("rejected UI update changed persisted audit record: %+v", got)
	}
}

func TestCodingKnowledgeImportStagesForeignExperienceForReview(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	pack := CodingKnowledgeExportPack{
		Version: "1.0",
		Experiences: []knowledge.CodingExperience{{
			ID: "foreign-id", Title: "foreign runtime experience", Scope: knowledge.CodingScopeUniversal,
			TriggerCondition: "foreign provenance", Content: "must be reviewed locally", Status: knowledge.CodingStatusVerified,
			SourceRuntimeTaskID: "foreign-task", SourceRuntimeAttemptID: "foreign-attempt", EvidenceDigest: "sha256:foreign",
			ParentExperienceID: "foreign-parent", LifecycleEvents: []knowledge.CodingExperienceLifecycleEvent{{Action: "candidate_confirmed", Reason: "foreign audit", OccurredAt: time.Now().UTC()}},
			Confidence: 9.5, RecallCount: 8, SuccessCount: 7, FailureCount: 1, LastRecalledAt: time.Now().UTC(), LastReviewedAt: time.Now().UTC(),
		}},
	}
	data, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(t.TempDir(), "coding-knowledge.json")
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := app.CodingKnowledgeImportFromFile(filePath)
	if err != nil || count != 1 {
		t.Fatalf("import result count=%d err=%v", count, err)
	}
	all, err := app.CodingKnowledgeList(knowledge.CodingListFilter{Limit: 10})
	if err != nil || len(all) != 1 {
		t.Fatalf("list imported=%+v err=%v", all, err)
	}
	got, err := app.CodingKnowledgeGet(all[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != knowledge.CodingStatusCandidate || got.SourceRuntimeTaskID != "" || got.SourceRuntimeAttemptID != "" || got.EvidenceDigest != "" || got.CreatedBy != "import" || !got.LastReviewedAt.IsZero() || got.ParentExperienceID != "" || len(got.LifecycleEvents) != 0 || got.Confidence != knowledge.CodingConfidenceInitial || got.RecallCount != 0 || got.SuccessCount != 0 || got.FailureCount != 0 || !got.LastRecalledAt.IsZero() {
		t.Fatalf("foreign import must be a provenance-free candidate: %+v", got)
	}
	if err := app.CodingKnowledgeConfirm(got.ID); err != nil {
		t.Fatalf("local review of import should use ordinary candidate path: %v", err)
	}
}

func TestCodingKnowledgeExportStripsLocalRuntimeAndReviewAuthority(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	store := app.ensureCodingKnowledgeStore()
	if store == nil {
		t.Fatal("coding knowledge store unavailable")
	}
	saved, err := store.SaveRuntimeExperience(context.Background(), knowledge.CodingExperience{
		Title: "export runtime authority", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "export runtime", Content: "must become a local candidate after import",
		SourceRuntimeTaskID: "task-export", SourceRuntimeAttemptID: "attempt-export", EvidenceDigest: "sha256:export",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmCandidate(context.Background(), saved.ID, func(context.Context, knowledge.CodingExperience) error { return nil }); err != nil {
		t.Fatal(err)
	}
	pack, err := app.CodingKnowledgeExport()
	if err != nil {
		t.Fatal(err)
	}
	if pack.Count != 1 || len(pack.Experiences) != 1 {
		t.Fatalf("exported pack=%+v", pack)
	}
	exported := pack.Experiences[0]
	if exported.ID != "" || exported.Status != knowledge.CodingStatusCandidate || exported.CreatedBy != "" || !exported.LastReviewedAt.IsZero() || exported.SourceRuntimeTaskID != "" || exported.SourceRuntimeAttemptID != "" || exported.EvidenceDigest != "" || exported.ParentExperienceID != "" || len(exported.LifecycleEvents) != 0 || exported.Confidence != knowledge.CodingConfidenceInitial || exported.RecallCount != 0 || exported.SuccessCount != 0 || exported.FailureCount != 0 || !exported.CreatedAt.IsZero() || !exported.UpdatedAt.IsZero() || !exported.LastRecalledAt.IsZero() {
		t.Fatalf("export retained local authority: %+v", exported)
	}
	if exported.Content == "" {
		t.Fatal("export must retain the reviewable content proposal")
	}
}

func TestCodingKnowledgeConflictBindingRetiresAndAuditsExperience(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	saved, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "conflict binding", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "binding conflict", Content: "conflicted rule",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CodingKnowledgeMarkConflict(saved.ID, "related-experience", "contradicted by current repository evidence"); err != nil {
		t.Fatal(err)
	}
	got, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil || got.Status != knowledge.CodingStatusDeprecated {
		t.Fatalf("conflict retirement exp=%+v err=%v", got, err)
	}
	events, err := app.CodingKnowledgeLifecycle(saved.ID)
	if err != nil || len(events) != 1 || events[0].Action != "conflict_marked" || events[0].RelatedID != "related-experience" {
		t.Fatalf("conflict lifecycle=%+v err=%v", events, err)
	}
}

func TestCodingKnowledgeRevisionCandidateBindingDoesNotReactivateConflict(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	parent, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "revision binding", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "revision binding", Content: "old guidance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CodingKnowledgeMarkConflict(parent.ID, "newer-evidence", "needs revision"); err != nil {
		t.Fatal(err)
	}
	candidate, err := app.CodingKnowledgeCreateRevisionCandidate(parent.ID, "review current behavior")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != knowledge.CodingStatusCandidate || candidate.ParentExperienceID != parent.ID {
		t.Fatalf("revision candidate=%+v", candidate)
	}
	got, err := app.CodingKnowledgeGet(parent.ID)
	if err != nil || got.Status != knowledge.CodingStatusDeprecated {
		t.Fatalf("parent must remain deprecated: %+v err=%v", got, err)
	}
}

func TestCodingKnowledgeGraduateToSteeringRetiresVerifiedExperienceWithoutOverwriting(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	store := app.ensureCodingKnowledgeStore()
	if store == nil {
		t.Fatal("coding knowledge store unavailable")
	}
	saved, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "Safe steering rule", Scope: knowledge.CodingScopeUniversal, TriggerCondition: "graduate tested rule", Content: "graduated guidance", Status: knowledge.CodingStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < knowledge.CodingMinRecallsForVerified; i++ {
		if err := recordCodingKnowledgeAppTestOutcome(t, app, saved.ID, true); err != nil {
			t.Fatal(err)
		}
	}

	steeringDir := filepath.Join(app.getMaclawBaseDir(), "steering")
	if err := os.MkdirAll(steeringDir, 0o755); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(steeringDir, buildSteeringFilename(saved))
	if err := os.WriteFile(occupied, []byte("user-owned steering content"), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := app.CodingKnowledgeGraduateToSteering(saved.ID)
	if err != nil {
		t.Fatalf("graduate: %v", err)
	}
	if path == occupied || filepath.Base(path) != "coding-exp-safe-steering-rule-2.md" {
		t.Fatalf("graduation must choose a new non-conflicting file, got %q", path)
	}
	oldContent, err := os.ReadFile(occupied)
	if err != nil || string(oldContent) != "user-owned steering content" {
		t.Fatalf("existing steering file was overwritten: content=%q err=%v", oldContent, err)
	}
	graduatedContent, err := os.ReadFile(path)
	if err != nil || len(graduatedContent) == 0 {
		t.Fatalf("graduated steering file missing: content=%q err=%v", graduatedContent, err)
	}

	got, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil || got.Status != knowledge.CodingStatusDeprecated {

		t.Fatalf("graduated experience must retire: exp=%+v err=%v", got, err)
	}
	events, err := app.CodingKnowledgeLifecycle(saved.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("graduation audit=%+v err=%v", events, err)
	}
	last := events[len(events)-1]
	if last.Action != "graduated_to_steering" || last.RelatedID != filepath.Base(path) {
		t.Fatalf("graduation audit event=%+v", last)
	}
}

func TestCodingKnowledgeCapacityAndEvict(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })

	// Seed more project-scoped experiences than the tiny limit.
	for i := 0; i < 5; i++ {
		status := knowledge.CodingStatusCandidate
		if i == 0 {
			status = knowledge.CodingStatusActive
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
	seeded, err := app.CodingKnowledgeList(knowledge.CodingListFilter{Limit: 10})
	if err != nil || len(seeded) != 5 {
		t.Fatalf("list seeded knowledge: %v %+v", err, seeded)
	}
	store := app.ensureCodingKnowledgeStore()
	if store == nil {
		t.Fatal("coding knowledge store unavailable")
	}
	var verifiedID string
	for _, exp := range seeded {
		if exp.Title == "proj exp A" {
			verifiedID = exp.ID
			break
		}
	}
	if verifiedID == "" {
		t.Fatal("verified seed missing")
	}
	for i := 0; i < knowledge.CodingMinRecallsForVerified; i++ {
		if err := recordCodingKnowledgeAppTestOutcome(t, app, verifiedID, true); err != nil {
			t.Fatalf("verify capacity seed: %v", err)
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

func TestCodingKnowledgeConfirmEnforcesReviewedProjectBudget(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodingKnowledgeMaxReviewedPerProject = 1
	cfg.CodingKnowledgeMaxReviewedTokensPerProject = 1_000
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project")
	first, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "first project review", Scope: knowledge.CodingScopeProject, ProjectPath: project,
		TriggerCondition: "review budget", Content: "first project guidance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CodingKnowledgeConfirm(first.ID); err != nil {
		t.Fatalf("confirm first candidate: %v", err)
	}
	second, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "second project review", Scope: knowledge.CodingScopeProject, ProjectPath: project,
		TriggerCondition: "review budget", Content: "second project guidance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.CodingKnowledgeConfirm(second.ID); err == nil || !strings.Contains(err.Error(), "count budget exceeded") {
		t.Fatalf("app confirmation must enforce reviewed budget, err=%v", err)
	}
	got, err := app.CodingKnowledgeGet(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != knowledge.CodingStatusCandidate {
		t.Fatalf("budget rejection must leave candidate unreviewed: %+v", got)
	}
}

func TestCodingKnowledgeSaveEnforcesReviewedProjectBudgetForActiveManualRecord(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodingKnowledgeMaxReviewedPerProject = 1
	cfg.CodingKnowledgeMaxReviewedTokensPerProject = 1_000
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "manual-project")
	if _, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "first manual active record", Scope: knowledge.CodingScopeProject, ProjectPath: project,
		TriggerCondition: "manual budget", Content: "first manual active record", Status: knowledge.CodingStatusActive,
	}); err != nil {
		t.Fatalf("save first active manual record: %v", err)
	}
	if _, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "second manual active record", Scope: knowledge.CodingScopeProject, ProjectPath: project,
		TriggerCondition: "manual budget", Content: "second manual active record", Status: knowledge.CodingStatusActive,
	}); err == nil || !strings.Contains(err.Error(), "count budget exceeded") {
		t.Fatalf("second active manual record must obey reviewed project budget, err=%v", err)
	}
}

func TestCodingKnowledgeUpdateEnforcesReviewedProjectTokenBudget(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodingKnowledgeMaxReviewedPerProject = 10
	cfg.CodingKnowledgeMaxReviewedTokensPerProject = 20
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "manual-token-project")
	saved, err := app.CodingKnowledgeSave(knowledge.CodingExperience{
		Title: "editable active record", Scope: knowledge.CodingScopeProject, ProjectPath: project,
		TriggerCondition: "manual token budget", Content: "small", Status: knowledge.CodingStatusActive,
	})
	if err != nil {
		t.Fatalf("save active manual record: %v", err)
	}
	editable, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	editable.Content = strings.Repeat("expanded reviewed guidance ", 30)
	if err := app.CodingKnowledgeUpdate(editable); err == nil || !strings.Contains(err.Error(), "token budget exceeded") {
		t.Fatalf("oversized reviewed edit must be rejected, err=%v", err)
	}
	after, err := app.CodingKnowledgeGet(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after.Content, "expanded reviewed guidance") || !strings.Contains(after.Content, "small") {
		t.Fatalf("rejected edit changed persisted content: %q", after.Content)
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
