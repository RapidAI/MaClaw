package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
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
