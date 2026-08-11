package codingruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type executorFunc func(context.Context, ExecutionRequest) ExecutionResult

func (f executorFunc) Execute(ctx context.Context, request ExecutionRequest) ExecutionResult {
	return f(ctx, request)
}

type approvalGateFunc func(Task, PolicySnapshot) error

func (f approvalGateFunc) Check(task Task, policy PolicySnapshot) error { return f(task, policy) }

func TestRunnerPersistsBoundedEvidenceAndTerminalResult(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	runner := Runner{Store: store, LeaseOwner: "gui:test", Now: func() time.Time { return now }}
	task, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo", RequestedWork: "fix"}, PolicySnapshot{Digest: "policy"}, executorFunc(func(ctx context.Context, request ExecutionRequest) ExecutionResult {
		if request.Attempt.AttemptNo != 1 || request.Attempt.Status != TaskRunning {
			t.Fatalf("request=%+v", request)
		}
		return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectConfirmed, Evidence: []Evidence{{Type: "verification", Digest: "sha256:test"}}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskCompleted || attempt.Status != TaskCompleted {
		t.Fatalf("task=%+v attempt=%+v", task, attempt)
	}
}

func TestRunnerCancellationNeverCommitsAutomaticRetrySafeResult(t *testing.T) {
	store := NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := Runner{Store: store, LeaseOwner: "tui:test"}
	_, attempt, err := runner.Run(ctx, Task{}, PolicySnapshot{}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != TaskInterrupted || attempt.SideEffectState != SideEffectUncertain {
		t.Fatalf("attempt=%+v", attempt)
	}
}

func TestRunnerApprovalGateDoesNotStartExecutor(t *testing.T) {
	store := NewMemoryStore()
	calls := 0
	runner := Runner{Store: store, LeaseOwner: "gui:test", ApprovalGate: approvalGateFunc(func(Task, PolicySnapshot) error {
		return ApprovalRequiredError{Summary: "external project path"}
	})}
	task, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo"}, PolicySnapshot{}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		calls++
		return ExecutionResult{}
	}))
	if err == nil || attempt != nil || task.Status != TaskWaitingApproval || calls != 0 {
		t.Fatalf("task=%+v attempt=%+v err=%v calls=%d", task, attempt, err, calls)
	}
}

func TestRunnerRejectsChangedPolicyForExistingTask(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.CreateTask(Task{TaskID: "stable-task", ProjectRef: "repo", PolicyDigest: "policy-a"})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: store, LeaseOwner: "gui:test"}
	_, attempt, err := runner.Run(context.Background(), Task{TaskID: "stable-task"}, PolicySnapshot{Digest: "policy-b"}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		t.Fatal("executor must not run under a changed policy")
		return ExecutionResult{}
	}))
	if err != ErrPolicyMismatch || attempt != nil {
		t.Fatalf("attempt=%+v err=%v", attempt, err)
	}
}

func TestRunnerContinuationCannotCreateUnknownOrNewTask(t *testing.T) {
	store := NewMemoryStore()
	runner := Runner{Store: store, LeaseOwner: "review"}
	calls := 0
	executor := executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		calls++
		return ExecutionResult{Status: TaskCompleted}
	})
	if task, attempt, err := runner.RunWithContinuation(context.Background(), Task{TaskID: "unknown-parent"}, PolicySnapshot{}, ContinuationReview{ParentAttemptID: "waiting-parent-attempt"}, executor); !errors.Is(err, ErrNotFound) || task != nil || attempt != nil {
		t.Fatalf("unknown continuation task=%#v attempt=%#v err=%v", task, attempt, err)
	}
	if task, attempt, err := runner.RunWithContinuation(context.Background(), Task{ProjectRef: "repo"}, PolicySnapshot{}, ContinuationReview{ParentAttemptID: "waiting-parent-attempt"}, executor); !errors.Is(err, ErrInvalidTransition) || task != nil || attempt != nil {
		t.Fatalf("new continuation task=%#v attempt=%#v err=%v", task, attempt, err)
	}
	if calls != 0 {
		t.Fatalf("executor calls=%d", calls)
	}
	if _, err := store.GetTask("unknown-parent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown task was persisted: %v", err)
	}
	if attempts, err := store.ListRecoveryCandidates(); err != nil || len(attempts) != 0 {
		t.Fatalf("unexpected persisted continuation state attempts=%#v err=%v", attempts, err)
	}
}

func TestRunnerRejectsChangedFrozenWritePolicyEvenWhenDigestIsReused(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	const legacyDigest = "host-supplied-legacy-digest"
	task, err := store.CreateTask(Task{TaskID: "stable-write-task", ProjectRef: "repo", Mode: "local", PolicyDigest: legacyDigest})
	if err != nil {
		t.Fatal(err)
	}
	firstPolicy := PolicySnapshot{
		Digest: legacyDigest, ProjectRoot: "repo", Mode: "local",
		WorkspaceIsolated: true, FinalDiffGateRequired: true,
		WriteSet: WriteSet{Claims: []WriteClaim{{Path: "internal/auth.go"}}},
	}
	first, err := store.StartAttempt(task.TaskID, "old", time.Minute, firstPolicy, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(first.AttemptID, "old", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTaskReadyForRecovery(task.TaskID, now); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: store, LeaseOwner: "new", Now: func() time.Time { return now.Add(time.Second) }}
	_, attempt, err := runner.Run(context.Background(), Task{TaskID: task.TaskID}, PolicySnapshot{
		Digest: legacyDigest, ProjectRoot: "repo", Mode: "local",
		WorkspaceIsolated: true, FinalDiffGateRequired: true,
		WriteSet: WriteSet{Claims: []WriteClaim{{Path: "internal/billing.go"}}},
	}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		t.Fatal("executor must not run under a changed write declaration")
		return ExecutionResult{}
	}))
	if err != ErrPolicyMismatch || attempt != nil {
		t.Fatalf("attempt=%+v err=%v", attempt, err)
	}
}

func TestRunnerCapturesReadOnlyWorkspaceBaselineBeforeExecutor(t *testing.T) {
	store := NewMemoryStore()
	proberCalls := 0
	runner := Runner{
		Store: store, LeaseOwner: "gui:test",
		WorkspaceProber: WorkspaceProberFunc(func(context.Context, Task, Attempt) (*WorkspaceProbe, error) {
			proberCalls++
			return &WorkspaceProbe{ProjectRef: "repo", Head: "abc", StatusHash: "clean"}, nil
		}),
	}
	_, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo"}, PolicySnapshot{}, executorFunc(func(_ context.Context, request ExecutionRequest) ExecutionResult {
		if request.Attempt.WorkspaceBefore == nil || request.Attempt.WorkspaceBefore.Head != "abc" {
			t.Fatalf("executor did not receive baseline: %#v", request.Attempt.WorkspaceBefore)
		}
		return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}
	}))
	// Completed attempts capture both the recovery baseline and a final
	// read-only observation. The final probe remains useful even when the
	// host did not opt into the strict final-workspace completion gate.
	if err != nil || proberCalls != 2 || attempt.WorkspaceBefore == nil || attempt.WorkspaceAfter == nil {
		t.Fatalf("attempt=%#v proberCalls=%d err=%v", attempt, proberCalls, err)
	}
}

func TestRunnerDoesNotOverwriteParentAfterChildAdmission(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	runner := Runner{Store: store, LeaseOwner: "gui:parent", Now: func() time.Time { return now }}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	task, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, executorFunc(func(_ context.Context, request ExecutionRequest) ExecutionResult {
		if _, err := service.AdmitReadOnlyChild(request.Attempt.AttemptID, "gui:parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}); err != nil {
			t.Fatal(err)
		}
		// The host may naturally return its normal loop result after child
		// admission; Runner must preserve waiting_child instead.
		return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}
	}))
	if err != nil || task.Status != TaskWaitingChild || attempt.Status != TaskWaitingChild || !attempt.LeaseUntil.IsZero() {
		t.Fatalf("task=%#v attempt=%#v err=%v", task, attempt, err)
	}
}

func TestRunnerFinalWorkspaceGateRequiresChangedReadOnlyProbe(t *testing.T) {
	store := NewMemoryStore()
	probes := []*WorkspaceProbe{
		{ProjectRef: "repo", Head: "abc", StatusHash: "clean"},
		{ProjectRef: "repo", Head: "abc", StatusHash: "clean"},
	}
	runner := Runner{Store: store, LeaseOwner: "tui:test", WorkspaceProber: WorkspaceProberFunc(func(context.Context, Task, Attempt) (*WorkspaceProbe, error) {
		probe := probes[0]
		probes = probes[1:]
		return probe, nil
	})}
	task, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", FinalWorkspaceGateRequired: true}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectObserved}
	}))
	if err != nil || task.Status != TaskBlocked || attempt.Status != TaskBlocked || attempt.ErrorCode != "final_workspace_unchanged" || attempt.WorkspaceAfter == nil {
		t.Fatalf("task=%#v attempt=%#v err=%v", task, attempt, err)
	}
}

func TestRunnerFinalWorkspaceGateAcceptsChangedReadOnlyProbe(t *testing.T) {
	store := NewMemoryStore()
	probes := []*WorkspaceProbe{
		{ProjectRef: "repo", Head: "abc", StatusHash: "clean"},
		{ProjectRef: "repo", Head: "abc", StatusHash: "dirty"},
	}
	runner := Runner{Store: store, LeaseOwner: "srv:test", WorkspaceProber: WorkspaceProberFunc(func(context.Context, Task, Attempt) (*WorkspaceProbe, error) {
		probe := probes[0]
		probes = probes[1:]
		return probe, nil
	})}
	task, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", FinalWorkspaceGateRequired: true}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectObserved}
	}))
	if err != nil || task.Status != TaskCompleted || attempt.Status != TaskCompleted || attempt.WorkspaceAfter == nil {
		t.Fatalf("task=%#v attempt=%#v err=%v", task, attempt, err)
	}
}

func TestRunnerFinalWorkspaceGateAcceptsExplicitVerifiedNoChangeEvidence(t *testing.T) {
	store := NewMemoryStore()
	probes := []*WorkspaceProbe{
		{ProjectRef: "repo", Head: "abc", StatusHash: "clean"},
		{ProjectRef: "repo", Head: "abc", StatusHash: "clean"},
	}
	runner := Runner{Store: store, LeaseOwner: "gui:test", WorkspaceProber: WorkspaceProberFunc(func(context.Context, Task, Attempt) (*WorkspaceProbe, error) {
		probe := probes[0]
		probes = probes[1:]
		return probe, nil
	})}
	task, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", FinalWorkspaceGateRequired: true}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		return ExecutionResult{
			Status:                          TaskCompleted,
			SideEffectState:                 SideEffectNone,
			NoWorkspaceChangeEvidenceDigest: "sha256:verified-no-change",
			Evidence:                        []Evidence{{Type: "verified_no_change", Digest: "sha256:verified-no-change"}},
		}
	}))
	if err != nil || task.Status != TaskCompleted || attempt.Status != TaskCompleted || attempt.WorkspaceAfter == nil {
		t.Fatalf("task=%#v attempt=%#v err=%v", task, attempt, err)
	}
}

func TestRunnerFinalWorkspaceGateRejectsUnpairedNoChangeDigest(t *testing.T) {
	store := NewMemoryStore()
	probes := []*WorkspaceProbe{
		{ProjectRef: "repo", Head: "abc", StatusHash: "clean"},
		{ProjectRef: "repo", Head: "abc", StatusHash: "clean"},
	}
	runner := Runner{Store: store, LeaseOwner: "host:test", WorkspaceProber: WorkspaceProberFunc(func(context.Context, Task, Attempt) (*WorkspaceProbe, error) {
		probe := probes[0]
		probes = probes[1:]
		return probe, nil
	})}
	task, attempt, err := runner.Run(context.Background(), Task{ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", FinalWorkspaceGateRequired: true}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
		return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectNone, NoWorkspaceChangeEvidenceDigest: "sha256:unpaired"}
	}))
	if err != nil || task.Status != TaskBlocked || attempt.Status != TaskBlocked || attempt.ErrorCode != "verified_no_change_evidence_missing" {
		t.Fatalf("task=%#v attempt=%#v err=%v", task, attempt, err)
	}
}

func TestRunnerDiscardsLateExecutorResultAfterAttemptWasClosed(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var (
		gotTask    *Task
		gotAttempt *Attempt
		gotErr     error
	)
	runner := Runner{Store: store, LeaseOwner: "owner", LeaseDuration: time.Hour, Now: func() time.Time { return now }}
	go func() {
		gotTask, gotAttempt, gotErr = runner.Run(context.Background(), Task{TaskID: "late-callback"}, PolicySnapshot{}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
			close(started)
			<-release
			return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}
		}))
		close(done)
	}()
	<-started
	attempts, err := store.ListAttempts("late-callback")
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	if _, err := store.FinishAttempt(attempts[0].AttemptID, "owner", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTaskReadyForRecovery("late-callback", now); err != nil {
		t.Fatal(err)
	}
	newAttempt, err := store.StartAttempt("late-callback", "new-owner", time.Hour, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if !errors.Is(gotErr, ErrStaleAttempt) || gotAttempt == nil || gotAttempt.Status != TaskInterrupted || gotTask == nil || gotTask.Status != TaskRunning {
		t.Fatalf("result task=%#v attempt=%#v err=%v", gotTask, gotAttempt, gotErr)
	}
	if current, err := store.GetAttempt(newAttempt.AttemptID); err != nil || current.Status != TaskRunning {
		t.Fatalf("new attempt=%#v err=%v", current, err)
	}
	events, err := store.ListEvents(attempts[0].AttemptID)
	if err != nil || len(events) != 2 || events[1].Type != "stale_callback_discarded" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestRunnerDiscardsLateExecutorResultAfterExplicitCancellation(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var gotErr error
	runner := Runner{Store: store, LeaseOwner: "owner", LeaseDuration: time.Hour, Now: func() time.Time { return now }}
	go func() {
		_, _, gotErr = runner.Run(context.Background(), Task{TaskID: "cancel-late"}, PolicySnapshot{}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
			close(started)
			<-release
			return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}
		}))
		close(done)
	}()
	<-started
	if _, err := store.CancelTask("cancel-late", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if !errors.Is(gotErr, ErrStaleAttempt) {
		t.Fatalf("runner error=%v, want stale callback", gotErr)
	}
	task, err := store.GetTask("cancel-late")
	if err != nil || task.Status != TaskCancelled {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	attempts, err := store.ListAttempts("cancel-late")
	if err != nil || len(attempts) != 1 || attempts[0].Status != TaskCancelled {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	events, err := store.ListEvents(attempts[0].AttemptID)
	if err != nil || len(events) != 3 || events[0].Type != "attempt_started" || events[1].Type != "task_cancelled" || events[2].Type != "stale_callback_discarded" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}
