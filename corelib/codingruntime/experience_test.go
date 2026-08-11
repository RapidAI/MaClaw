package codingruntime

import (
	"strings"
	"testing"
	"time"
)

func TestExperienceEvidenceDigestBindsMaterialLedgerEvents(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	task, err := store.CreateTask(Task{TaskID: "experience-task", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "verification", "sha256:verification", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}
	digest, err := ExperienceEvidenceDigest(store, task, attempt)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
}

func TestExperienceEvidenceDigestRejectsLifecycleAndUnknownEvents(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	for _, eventType := range []string{"attempt_started", "future_adapter_claim"} {
		t.Run(eventType, func(t *testing.T) {
			store := NewMemoryStore()
			task, err := store.CreateTask(Task{TaskID: "experience-" + eventType, ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.AppendEvent(attempt.AttemptID, "runner", eventType, "sha256:event", now); err != nil {
				t.Fatal(err)
			}
			if _, err := store.FinishAttempt(attempt.AttemptID, "runner", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectNone}, now); err != nil {
				t.Fatal(err)
			}
			if _, err := ExperienceEvidenceDigest(store, task, attempt); err == nil || !strings.Contains(err.Error(), "no material execution evidence") {
				t.Fatalf("expected evidence rejection, err=%v", err)
			}
		})
	}
}

func TestMaterialExperienceEvidenceEvent(t *testing.T) {
	if !MaterialExperienceEvidenceEvent("verification") {
		t.Fatal("verification should be material evidence")
	}
	if MaterialExperienceEvidenceEvent("attempt_terminal_completed") || MaterialExperienceEvidenceEvent("unknown") {
		t.Fatal("lifecycle and unknown events must not authorize experience extraction")
	}
}

func TestKnowledgeEligibleTerminalStatus(t *testing.T) {
	for _, status := range []TaskStatus{TaskCompleted, TaskFailed, TaskBlocked} {
		if !KnowledgeEligibleTerminalStatus(status) {
			t.Fatalf("status %q should be knowledge eligible", status)
		}
	}
	for _, status := range []TaskStatus{TaskCancelled, TaskInterrupted, TaskRunning} {
		if KnowledgeEligibleTerminalStatus(status) {
			t.Fatalf("status %q should not be knowledge eligible", status)
		}
	}
}

func TestResolveExperienceProvenanceUsesMatchingTerminalEvidence(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	task, err := store.CreateTask(Task{TaskID: "resolve-experience", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "verification", "sha256:verified", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", FinishInput{Status: TaskFailed, SideEffectState: SideEffectObserved}, now); err != nil {
		t.Fatal(err)
	}
	provenance, err := ResolveExperienceProvenance(store, task.TaskID)
	if err != nil || provenance.TaskID != task.TaskID || provenance.AttemptID != attempt.AttemptID || !strings.HasPrefix(provenance.EvidenceDigest, "sha256:") {
		t.Fatalf("provenance=%+v err=%v", provenance, err)
	}
}

func TestVerifyExperienceProvenanceRejectsMismatchedAttemptOrDigest(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	task, err := store.CreateTask(Task{TaskID: "verify-experience", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "owner", "verification", "sha256:verified", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "owner", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}, now); err != nil {
		t.Fatal(err)
	}
	provenance, err := ResolveExperienceProvenance(store, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyExperienceProvenance(store, task.TaskID, provenance.AttemptID, provenance.EvidenceDigest); err != nil {
		t.Fatalf("verify matching provenance: %v", err)
	}
	if err := VerifyExperienceProvenance(store, task.TaskID, "other-attempt", provenance.EvidenceDigest); err == nil {
		t.Fatal("mismatched attempt must be rejected")
	}
	if err := VerifyExperienceProvenance(store, task.TaskID, provenance.AttemptID, "sha256:tampered"); err == nil {
		t.Fatal("mismatched digest must be rejected")
	}
}

func TestResolveExperienceProvenanceRejectsInterruptedTask(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	task, err := store.CreateTask(Task{TaskID: "interrupted-experience", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "verification", "sha256:verified", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExperienceProvenance(store, task.TaskID); err == nil || !strings.Contains(err.Error(), "knowledge-eligible") {
		t.Fatalf("expected interrupted task rejection, err=%v", err)
	}
}
