package guiapp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestCodingExperienceEvidenceDigestBindsMaterialLedgerEvents(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "task-experience", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy-digest"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy-digest", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "verification", "sha256:verification", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}
	digest, err := codingExperienceEvidenceDigest(store, task, attempt)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
	// Changing any material Ledger fact must change the provenance digest.
	store2 := codingruntime.NewMemoryStore()
	task2, _ := store2.CreateTask(*task)
	attempt2, _ := store2.StartAttempt(task2.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy-digest", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	_, _ = store2.AppendEvent(attempt2.AttemptID, "runner", "verification", "sha256:different", now)
	_, _ = store2.FinishAttempt(attempt2.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now)
	changed, err := codingExperienceEvidenceDigest(store2, task2, attempt2)
	if err != nil || digest == changed {
		t.Fatalf("digest=%q changed=%q err=%v", digest, changed, err)
	}
}

func TestCodingExperienceEvidenceDigestRejectsLifecycleOnlyAttempt(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "task-lifecycle-only", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "attempt_started", "policy", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := codingExperienceEvidenceDigest(store, task, attempt); err == nil || !strings.Contains(err.Error(), "no material execution evidence") {
		t.Fatalf("expected lifecycle-only evidence rejection, err=%v", err)
	}
}

func TestCodingExperienceEvidenceDigestRejectsUnknownEvidenceEvent(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "task-unknown-evidence", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "future_adapter_claim", "sha256:unreviewed", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := codingExperienceEvidenceDigest(store, task, attempt); err == nil || !strings.Contains(err.Error(), "no material execution evidence") {
		t.Fatalf("expected unknown evidence rejection, err=%v", err)
	}
}

func TestCodingExperienceShouldExtract(t *testing.T) {
	passed := &CodingSubAgentResult{Status: TaskExecPassed}
	failed := &CodingSubAgentResult{Status: TaskExecFailed}
	horizon := &CodingSubAgentResult{Status: TaskExecPassed, HorizonOwned: true}

	if codingExperienceShouldExtract(ExperienceSaveModeOff, ExperienceStrategyOnSuccess, passed, false) {
		t.Fatal("off mode must not extract")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyOff, passed, false) {
		t.Fatal("off strategy must not extract")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeAuto, ExperienceStrategyOnSuccess, horizon, false) {
		t.Fatal("horizon-owned results must not extract")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyOnRetry, passed, false) {
		t.Fatal("on_retry_success must wait for a retry")
	}
	if !codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyOnRetry, passed, true) {
		t.Fatal("on_retry_success should extract after a retry")
	}
	if !codingExperienceShouldExtract(ExperienceSaveModeAuto, ExperienceStrategyOnSuccess, passed, false) {
		t.Fatal("auto/on_success should extract a passing task")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeAuto, ExperienceStrategyOnSuccess, failed, false) {
		t.Fatal("on_success must not extract a failed task")
	}
	if !codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyAlways, failed, false) {
		t.Fatal("always should extract failures")
	}
}

func TestCodingResultLooksLikeRetrySuccess(t *testing.T) {
	if codingResultLooksLikeRetrySuccess(&CodingSubAgentResult{
		CommandsRun: []CodingSubAgentCommandResult{{Command: "go test", Succeeded: true}},
	}) {
		t.Fatal("first-try success is not a retry")
	}
	if !codingResultLooksLikeRetrySuccess(&CodingSubAgentResult{
		CommandsRun: []CodingSubAgentCommandResult{
			{Command: "go test", Succeeded: false},
			{Command: "go test", Succeeded: true},
		},
	}) {
		t.Fatal("failed then succeeded commands should count as retry success")
	}
}

func TestIsSimilarExperienceUsesContentWhenTriggerMissing(t *testing.T) {
	body := strings.Repeat("always set http client timeouts in production. ", 3)
	existing := knowledge.CodingExperience{Title: "timeouts", Content: body}
	dup := knowledge.CodingExperience{Title: "http timeouts", Content: body}
	if !isSimilarExperience(existing, dup) {
		t.Fatal("identical content should be treated as a duplicate")
	}
	if isSimilarExperience(existing, knowledge.CodingExperience{Title: "other", Content: "unrelated guidance about maps"}) {
		t.Fatal("unrelated content must not match")
	}
}

func TestInferRemoteCodingLanguageDoesNotHardcodePython(t *testing.T) {
	if got := inferRemoteCodingLanguage(&remoteCodingCallbacks{}); got != "" {
		t.Fatalf("empty files should leave language unset, got %q", got)
	}
	if got := inferRemoteCodingLanguage(&remoteCodingCallbacks{filesModified: []string{"pkg/foo.go"}}); got != "go" {
		t.Fatalf("go files should infer go, got %q", got)
	}
}

func TestFinishCodingKnowledgeSearchPreservesLocalError(t *testing.T) {
	localErr := fmt.Errorf("db closed")
	_, err := finishCodingKnowledgeSearch(nil, context.Background(), nil, localErr, "query", "", "", 5)
	if err != localErr {
		t.Fatalf("nil app should keep local search error, got %v", err)
	}
	_, err = finishCodingKnowledgeSearch(&App{}, context.Background(), nil, localErr, "query", "", "", 5)
	if err == nil || !strings.Contains(err.Error(), "db closed") {
		t.Fatalf("empty enterprise merge should not hide local error, got %v", err)
	}
}

func TestRemoteResultAsCodingSubAgentResultMapsSuccess(t *testing.T) {
	got := remoteResultAsCodingSubAgentResult(&RemoteCodingSubAgentResult{
		Status:                "success",
		Summary:               "done",
		RuntimeTaskID:         "task-1",
		RecalledExperienceIDs: []string{"exp-1"},
	})
	if got.Status != TaskExecPassed || got.RuntimeTaskID != "task-1" || got.RecalledExperienceIDs[0] != "exp-1" {
		t.Fatalf("mapped result = %+v", got)
	}
}

func TestCodingExperienceKnowledgeTerminalStatus(t *testing.T) {
	for _, status := range []codingruntime.TaskStatus{codingruntime.TaskCompleted, codingruntime.TaskFailed, codingruntime.TaskBlocked} {
		if !codingExperienceKnowledgeTerminalStatus(status) {
			t.Fatalf("status %q should be knowledge eligible", status)
		}
	}
	for _, status := range []codingruntime.TaskStatus{codingruntime.TaskCancelled, codingruntime.TaskInterrupted, codingruntime.TaskRunning} {
		if codingExperienceKnowledgeTerminalStatus(status) {
			t.Fatalf("status %q should not be knowledge eligible", status)
		}
	}
}
