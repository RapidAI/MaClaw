package codingruntime

import (
	"context"
	"testing"
	"time"
)

func TestRecoveryRequiresProbeAndConfirmationBeforeNewAttempt(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	task, err := store.CreateTask(Task{TaskID: "task-1", ProjectRef: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "gui:old", time.Minute, PolicySnapshot{Digest: "policy", ProjectRoot: "repo"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "gui:old", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain, WorkspaceAfter: &WorkspaceProbe{ProjectRef: "repo", Head: "old", StatusHash: "before"}}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	recovery := RecoveryService{Store: store, Now: func() time.Time { return now.Add(2 * time.Second) }}
	plan, err := recovery.PrepareRecovery(attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(task.TaskID, "gui:new", time.Minute, PolicySnapshot{Digest: "policy"}, now.Add(3*time.Second)); err == nil {
		t.Fatal("new attempt started before a recovery confirmation")
	}
	if err := recovery.ConfirmContinuation(plan, PolicySnapshot{Digest: "policy"}, true); err != ErrRecoveryNotReady {
		t.Fatalf("confirm before probe = %v, want ErrRecoveryNotReady", err)
	}
	plan, err = recovery.ProbeWorkspace(context.Background(), plan, WorkspaceProberFunc(func(context.Context, Task, Attempt) (*WorkspaceProbe, error) {
		return &WorkspaceProbe{ProjectRef: "repo", Head: "new", StatusHash: "after"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := recovery.PresentRecoveryDiff(plan)
	if err != nil || summary == "" || !plan.WorkspaceChanged {
		t.Fatalf("summary=%q plan=%+v err=%v", summary, plan, err)
	}
	if err := recovery.ConfirmContinuation(plan, PolicySnapshot{Digest: "policy"}, true); err != nil {
		t.Fatal(err)
	}
	resumed, err := store.StartAttempt(task.TaskID, "gui:new", time.Minute, PolicySnapshot{Digest: "policy"}, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.AttemptNo != 2 || resumed.AttemptID == attempt.AttemptID {
		t.Fatalf("resumed=%+v prior=%+v", resumed, attempt)
	}
}

func TestRecoveryRejectsPolicyMismatchWithoutMakingTaskRunnable(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	task, _ := store.CreateTask(Task{ProjectRef: "repo"})
	attempt, _ := store.StartAttempt(task.TaskID, "gui:old", time.Minute, PolicySnapshot{Digest: "policy-a"}, now)
	_, _ = store.FinishAttempt(attempt.AttemptID, "gui:old", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now)
	recovery := RecoveryService{Store: store}
	plan, err := recovery.PrepareRecovery(attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = recovery.ProbeWorkspace(context.Background(), plan, WorkspaceProberFunc(func(context.Context, Task, Attempt) (*WorkspaceProbe, error) {
		return &WorkspaceProbe{ProjectRef: "repo"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.ConfirmContinuation(plan, PolicySnapshot{Digest: "policy-b"}, true); err != ErrPolicyMismatch {
		t.Fatalf("ConfirmContinuation()=%v, want ErrPolicyMismatch", err)
	}
	if _, err := store.StartAttempt(task.TaskID, "gui:new", time.Minute, PolicySnapshot{Digest: "policy-a"}, now.Add(time.Second)); err == nil {
		t.Fatal("policy mismatch unexpectedly made task runnable")
	}
}

func TestPrepareRecoveryForTaskSelectsLatestInterruptedAttempt(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	task, err := store.CreateTask(Task{ProjectRef: "repo", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.StartAttempt(task.TaskID, "owner", time.Minute, PolicySnapshot{Digest: "policy"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(first.AttemptID, "owner", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.MarkTaskReadyForRecovery(task.TaskID, now); err != nil {
		t.Fatal(err)
	}
	second, err := store.StartAttempt(task.TaskID, "owner", time.Minute, PolicySnapshot{Digest: "policy"}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(second.AttemptID, "owner", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	plan, err := (RecoveryService{Store: store, Now: func() time.Time { return now }}).PrepareRecoveryForTask(task.TaskID)
	if err != nil || plan.Interrupted.AttemptID != second.AttemptID {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}
