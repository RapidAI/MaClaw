package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestGUILoopCancellationPersistsRuntimeCancellationAndDiscardsLateResult(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	loop := NewLoopContext("coding-runtime-cancel", 3, nil)
	ctx, cancel := loop.Context()
	defer cancel()
	started := make(chan codingruntime.ExecutionRequest, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	var (
		result  *CodingSubAgentResult
		attempt *codingruntime.Attempt
		runErr  error
	)
	go func() {
		result, attempt, runErr = runGUICodingTaskWithLedgerWithStart(ctx, store, "gui:test", "workflow", "phase", "D:/repo", "change", nil, func(request codingruntime.ExecutionRequest) {
			started <- request
		}, func() *CodingSubAgentResult {
			<-release
			return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "late completion"}
		})
		close(done)
	}()
	request := <-started
	loop.RegisterCancelHook(func() { _, _ = store.CancelTask(request.Task.TaskID, request.Attempt.StartedAt.Add(time.Second)) })
	loop.Cancel()
	close(release)
	<-done
	if runErr != nil || result == nil || result.Status != TaskExecInterrupted || attempt == nil || attempt.Status != codingruntime.TaskCancelled {
		t.Fatalf("result=%#v attempt=%#v err=%v", result, attempt, runErr)
	}
	task, err := store.GetTask(request.Task.TaskID)
	if err != nil || task.Status != codingruntime.TaskCancelled {
		t.Fatalf("task=%#v err=%v", task, err)
	}
	events, err := store.ListEvents(request.Attempt.AttemptID)
	if err != nil || len(events) < 3 || events[len(events)-2].Type != "task_cancelled" || events[len(events)-1].Type != "stale_callback_discarded" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestLoopContextCancellationHookRunsOnceAndCanBeUnregistered(t *testing.T) {
	loop := NewLoopContext("cancel-hook", 1, nil)
	calls := 0
	remove := loop.RegisterCancelHook(func() { calls++ })
	remove()
	loop.RegisterCancelHook(func() { calls++ })
	loop.Cancel()
	loop.Cancel()
	if calls != 1 {
		t.Fatalf("cancel hook calls=%d, want 1", calls)
	}
	calledAfterCancellation := false
	loop.RegisterCancelHook(func() { calledAfterCancellation = true })
	if !calledAfterCancellation {
		t.Fatal("hook registered after cancellation must execute immediately")
	}
}

func TestRunGUICodingTaskWithLedger_WaitsForScopeApprovalBeforeExecutor(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	calls := 0
	result, attempt, err := runGUICodingTaskWithLedger(
		context.Background(), store, "gui:test", "workflow", "phase", "D:/repo", "edit external file",
		codingRuntimeApprovalGate(func() string { return "external project path requires approval" }),
		func() *CodingSubAgentResult {
			calls++
			return &CodingSubAgentResult{Status: TaskExecPassed}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempt != nil || calls != 0 {
		t.Fatalf("attempt=%+v executor_calls=%d; executor must not start before approval", attempt, calls)
	}
	if result == nil || result.Status != TaskExecWaitingApproval {
		t.Fatalf("result=%+v; want waiting approval", result)
	}
}

func TestGUILocalWorkspaceProberUsesReadOnlyGitBaseline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The fixture is intentionally not a git repository: the prober must fail
	// safely and never create metadata or write a baseline by itself.
	prober := newGUILocalWorkspaceProber(dir)
	if prober == nil {
		t.Fatal("expected local prober")
	}
	if _, err := prober.ProbeWorkspace(context.Background(), codingruntime.Task{ProjectRef: dir}, codingruntime.Attempt{}); err == nil {
		t.Fatal("expected non-git probe failure")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("read-only probe unexpectedly created .git: %v", err)
	}
}

func TestRunGUIRemoteCodingTaskWithLedgerCapturesRemoteBaselineThroughAdapter(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	proberCalls := 0
	result, attempt, err := runGUIRemoteCodingTaskWithLedger(
		context.Background(), store, "owner", "workflow", "phase", "remote-target", "/srv/repo", "fix",
		codingruntime.WorkspaceProberFunc(func(context.Context, codingruntime.Task, codingruntime.Attempt) (*codingruntime.WorkspaceProbe, error) {
			proberCalls++
			return &codingruntime.WorkspaceProbe{ProjectRef: "/srv/repo", Head: "abc", HostKey: "remote-target", WorkDir: "/srv/repo"}, nil
		}),
		func() *RemoteCodingSubAgentResult {
			return &RemoteCodingSubAgentResult{Status: "success", Summary: "done"}
		},
	)
	// A remote implementation task now requires the same pre/post read-only
	// workspace evidence as other writers. An unchanged workspace without a
	// quality-gated no-change marker is blocked rather than falsely completed.
	if err != nil || result == nil || attempt == nil || proberCalls != 2 {
		t.Fatalf("result=%#v attempt=%#v calls=%d err=%v", result, attempt, proberCalls, err)
	}
	if attempt.Status != codingruntime.TaskBlocked || attempt.ErrorCode != "final_workspace_unchanged" || attempt.WorkspaceBefore == nil || attempt.WorkspaceBefore.Head != "abc" || result.RuntimeTaskID == "" {
		t.Fatalf("missing remote baseline/runtime ID: attempt=%#v result=%#v", attempt, result)
	}
}

func TestRunGUIRemoteCodingTaskWithLedgerAcceptsVerifiedNoChangeEvidence(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	proberCalls := 0
	result, attempt, err := runGUIRemoteCodingTaskWithLedger(
		context.Background(), store, "owner", "workflow", "phase", "remote-target", "/srv/repo", "fix",
		codingruntime.WorkspaceProberFunc(func(context.Context, codingruntime.Task, codingruntime.Attempt) (*codingruntime.WorkspaceProbe, error) {
			proberCalls++
			return &codingruntime.WorkspaceProbe{ProjectRef: "/srv/repo", Head: "abc", HostKey: "remote-target", WorkDir: "/srv/repo"}, nil
		}),
		func() *RemoteCodingSubAgentResult {
			return &RemoteCodingSubAgentResult{Status: "success", Summary: "checked remote target\n[verified no-change acceptance] remote acceptance command and clean workspace inspection confirm the requested behavior already exists."}
		},
	)
	if err != nil || result == nil || attempt == nil || proberCalls != 2 || attempt.Status != codingruntime.TaskCompleted || result.RuntimeTaskID == "" {
		t.Fatalf("result=%#v attempt=%#v calls=%d err=%v", result, attempt, proberCalls, err)
	}
}

func TestRunGUICodingTaskWithLedgerReportsWaitingChildWithoutOverwritingLease(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	result, attempt, err := runGUICodingTaskWithLedgerWithStart(
		context.Background(), store, "owner", "workflow", "phase", "D:/repo", "inspect in children", nil,
		func(request codingruntime.ExecutionRequest) {
			_, admissionErr := (codingruntime.ChildTaskService{Store: store}).AdmitReadOnlyChild(request.Attempt.AttemptID, "owner", codingruntime.ChildTaskSpec{Name: "explorer", RequestedWork: "inspect", ProjectRef: "D:/repo", Mode: "local"}, codingruntime.PolicySnapshot{ProjectRoot: "D:/repo", Mode: "local", ReadOnly: true})
			if admissionErr != nil {
				t.Fatalf("admit child: %v", admissionErr)
			}
		},
		func() *CodingSubAgentResult {
			return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "old parent result"}
		},
	)
	if err != nil || result == nil || result.Status != TaskExecWaitingChild || attempt == nil || attempt.Status != codingruntime.TaskWaitingChild {
		t.Fatalf("result=%#v attempt=%#v err=%v", result, attempt, err)
	}
}

func TestRunGUICodingTaskWithLedgerStartsFreshAttemptForExplicitChildReview(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	result, parentAttempt, err := runGUICodingTaskWithLedgerWithStart(
		context.Background(), store, "owner", "workflow", "phase", "D:/repo", "inspect in child", nil,
		func(request codingruntime.ExecutionRequest) {
			_, admissionErr := (codingruntime.ChildTaskService{Store: store}).AdmitReadOnlyChild(request.Attempt.AttemptID, "owner", codingruntime.ChildTaskSpec{Name: "explorer", RequestedWork: "inspect", ProjectRef: "D:/repo", Mode: "local"}, codingruntime.PolicySnapshot{ProjectRoot: "D:/repo", Mode: "local", ReadOnly: true})
			if admissionErr != nil {
				t.Fatalf("admit child: %v", admissionErr)
			}
		},
		func() *CodingSubAgentResult { return &CodingSubAgentResult{Status: TaskExecPassed} },
	)
	if err != nil || result == nil || parentAttempt == nil || result.RuntimeTaskID == "" {
		t.Fatalf("handoff result=%#v attempt=%#v err=%v", result, parentAttempt, err)
	}
	children, err := store.ListAttempts(result.RuntimeTaskID)
	if err != nil || len(children) != 1 {
		t.Fatalf("parent attempts=%#v err=%v", children, err)
	}
	service := codingruntime.ChildTaskService{Store: store}
	handles, err := store.ListChildTasks(result.RuntimeTaskID)
	if err != nil || len(handles) != 1 {
		t.Fatalf("child tasks=%#v err=%v", handles, err)
	}
	childRunner := codingruntime.Runner{Store: store, LeaseOwner: "child", LeaseDuration: time.Minute}
	if _, _, _, err := service.RunReadOnlyChild(context.Background(), childRunner, handles[0].TaskID, codingruntime.PolicySnapshot{ProjectRoot: "D:/repo", Mode: "local", ReadOnly: true}, codingruntimeReadOnlyChildExecutorFunc(func(context.Context, codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCompleted, Summary: "bounded child finding", EvidenceDigest: "sha256:child"}
	})); err != nil {
		t.Fatalf("deliver child result: %v", err)
	}
	continuation, err := service.PrepareParentContinuation(result.RuntimeTaskID)
	if err != nil || continuation == nil {
		t.Fatalf("prepare continuation=%#v err=%v", continuation, err)
	}
	review, reviewAttempt, err := runGUICodingTaskWithLedgerWithOptions(context.Background(), store, "owner", "workflow", "phase", "D:/repo", "review only bounded child result", nil, nil, &guiCodingRuntimeOptions{ExistingTaskID: continuation.Task.TaskID, ParentContinuationAttemptID: continuation.ParentAttemptID}, func() *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "reviewed child evidence"}
	})
	if err != nil || review == nil || reviewAttempt == nil || reviewAttempt.AttemptID == parentAttempt.AttemptID || reviewAttempt.AttemptNo != 2 || review.RuntimeTaskID != result.RuntimeTaskID {
		t.Fatalf("review=%#v attempt=%#v parent=%#v err=%v", review, reviewAttempt, parentAttempt, err)
	}
}

func TestRunGUICodingTaskWithLedgerConsumesChildHandoffOnlyOnce(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	result, _, err := runGUICodingTaskWithLedgerWithStart(context.Background(), store, "owner", "workflow", "phase", "D:/repo", "inspect in child", nil, func(request codingruntime.ExecutionRequest) {
		_, err := (codingruntime.ChildTaskService{Store: store}).AdmitReadOnlyChild(request.Attempt.AttemptID, "owner", codingruntime.ChildTaskSpec{Name: "explorer", RequestedWork: "inspect", ProjectRef: "D:/repo", Mode: "local"}, codingruntime.PolicySnapshot{ProjectRoot: "D:/repo", Mode: "local", ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
	}, func() *CodingSubAgentResult { return &CodingSubAgentResult{Status: TaskExecPassed} })
	if err != nil || result == nil {
		t.Fatalf("handoff result=%#v err=%v", result, err)
	}
	children, err := store.ListChildTasks(result.RuntimeTaskID)
	if err != nil || len(children) != 1 {
		t.Fatalf("children=%#v err=%v", children, err)
	}
	service := codingruntime.ChildTaskService{Store: store}
	if _, _, _, err := service.RunReadOnlyChild(context.Background(), codingruntime.Runner{Store: store, LeaseOwner: "child", LeaseDuration: time.Minute}, children[0].TaskID, codingruntime.PolicySnapshot{ProjectRoot: "D:/repo", Mode: "local", ReadOnly: true}, codingruntimeReadOnlyChildExecutorFunc(func(context.Context, codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCompleted, Summary: "finding", EvidenceDigest: "sha256:child"}
	})); err != nil {
		t.Fatal(err)
	}
	continuation, err := service.PrepareParentContinuation(result.RuntimeTaskID)
	if err != nil {
		t.Fatal(err)
	}
	first, firstAttempt, err := runGUICodingTaskWithLedgerWithOptions(context.Background(), store, "owner", "workflow", "phase", "D:/repo", "fresh review", nil, nil, &guiCodingRuntimeOptions{ExistingTaskID: continuation.Task.TaskID, ParentContinuationAttemptID: continuation.ParentAttemptID}, func() *CodingSubAgentResult { return &CodingSubAgentResult{Status: TaskExecPassed} })
	if err != nil || first == nil || firstAttempt == nil || first.Status != TaskExecPassed {
		t.Fatalf("first=%#v attempt=%#v err=%v", first, firstAttempt, err)
	}
	calls := 0
	second, secondAttempt, err := runGUICodingTaskWithLedgerWithOptions(context.Background(), store, "owner", "workflow", "phase", "D:/repo", "duplicate review", nil, nil, &guiCodingRuntimeOptions{ExistingTaskID: continuation.Task.TaskID, ParentContinuationAttemptID: continuation.ParentAttemptID}, func() *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	if err != nil || second == nil || second.Error != "runtime_child_review_consumed" || calls != 0 || secondAttempt != nil {
		t.Fatalf("second=%#v attempt=%#v calls=%d err=%v", second, secondAttempt, calls, err)
	}
}

func TestRunGUICodingTaskWithLedgerRejectsConcurrentChildReviewWithoutSecondAttempt(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "parent", WorkflowID: "workflow", PhaseID: "phase", ProjectRef: "D:/repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	policy := codingruntime.PolicySnapshot{ProjectRoot: "D:/repo", Mode: "local"}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Digest = digest
	if _, err := store.StartAttempt(task.TaskID, "active-review", time.Minute, policy, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, attempt, err := runGUICodingTaskWithLedgerWithOptions(context.Background(), store, "second-review", "workflow", "phase", "D:/repo", "review", nil, nil, &guiCodingRuntimeOptions{ExistingTaskID: task.TaskID}, func() *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecPassed}
	})
	if err != nil || result == nil || result.Error != "runtime_child_review_in_progress" || !result.RuntimeHandoff || calls != 0 || attempt != nil {
		t.Fatalf("result=%#v attempt=%#v calls=%d err=%v", result, attempt, calls, err)
	}
	attempts, err := store.ListAttempts(task.TaskID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%#v err=%v", attempts, err)
	}
}

type codingruntimeReadOnlyChildExecutorFunc func(context.Context, codingruntime.ExecutionRequest) codingruntime.ChildTaskResult

func (f codingruntimeReadOnlyChildExecutorFunc) ExecuteReadOnlyChild(ctx context.Context, request codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
	return f(ctx, request)
}

func TestRunGUIRemoteCodingTaskWithLedgerReportsWaitingChildWithoutFalseSuccess(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	result, attempt, err := runGUIRemoteCodingTaskWithLedgerWithStart(
		context.Background(), store, "owner", "workflow", "phase", "remote-target", "/srv/repo", "inspect in children", nil,
		func(request codingruntime.ExecutionRequest) {
			_, admissionErr := (codingruntime.ChildTaskService{Store: store}).AdmitReadOnlyChild(request.Attempt.AttemptID, "owner", codingruntime.ChildTaskSpec{Name: "explorer", RequestedWork: "inspect", ProjectRef: "/srv/repo", Mode: "remote"}, codingruntime.PolicySnapshot{ProjectRoot: "/srv/repo", RemoteTarget: "remote-target", Mode: "remote", ReadOnly: true})
			if admissionErr != nil {
				t.Fatalf("admit child: %v", admissionErr)
			}
		},
		func() *RemoteCodingSubAgentResult {
			return &RemoteCodingSubAgentResult{Status: "success", Summary: "obsolete parent completion"}
		},
	)
	if err != nil || result == nil || result.Status != "waiting_child" || attempt == nil || attempt.Status != codingruntime.TaskWaitingChild {
		t.Fatalf("result=%#v attempt=%#v err=%v", result, attempt, err)
	}
}

func TestRunGUIRemoteCodingTaskWithLedgerStartsFreshAttemptForExplicitChildReview(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	result, parentAttempt, err := runGUIRemoteCodingTaskWithLedgerWithStart(
		context.Background(), store, "owner", "workflow", "phase", "remote-target", "/srv/repo", "inspect in child", nil,
		func(request codingruntime.ExecutionRequest) {
			_, admissionErr := (codingruntime.ChildTaskService{Store: store}).AdmitReadOnlyChild(request.Attempt.AttemptID, "owner", codingruntime.ChildTaskSpec{Name: "explorer", RequestedWork: "inspect", ProjectRef: "/srv/repo", Mode: "remote"}, codingruntime.PolicySnapshot{ProjectRoot: "/srv/repo", RemoteTarget: "remote-target", Mode: "remote", ReadOnly: true})
			if admissionErr != nil {
				t.Fatalf("admit child: %v", admissionErr)
			}
		},
		func() *RemoteCodingSubAgentResult { return &RemoteCodingSubAgentResult{Status: "success"} },
	)
	if err != nil || result == nil || parentAttempt == nil || result.RuntimeTaskID == "" {
		t.Fatalf("handoff result=%#v attempt=%#v err=%v", result, parentAttempt, err)
	}
	children, err := store.ListChildTasks(result.RuntimeTaskID)
	if err != nil || len(children) != 1 {
		t.Fatalf("child tasks=%#v err=%v", children, err)
	}
	service := codingruntime.ChildTaskService{Store: store}
	childRunner := codingruntime.Runner{Store: store, LeaseOwner: "child", LeaseDuration: time.Minute}
	if _, _, _, err := service.RunReadOnlyChild(context.Background(), childRunner, children[0].TaskID, codingruntime.PolicySnapshot{ProjectRoot: "/srv/repo", RemoteTarget: "remote-target", Mode: "remote", ReadOnly: true}, codingruntimeReadOnlyChildExecutorFunc(func(context.Context, codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCompleted, Summary: "bounded remote child finding", EvidenceDigest: "sha256:remote-child"}
	})); err != nil {
		t.Fatalf("deliver child result: %v", err)
	}
	continuation, err := service.PrepareParentContinuation(result.RuntimeTaskID)
	if err != nil || continuation == nil {
		t.Fatalf("prepare continuation=%#v err=%v", continuation, err)
	}
	review, reviewAttempt, err := runGUIRemoteCodingTaskWithStartAndContinuation(context.Background(), store, "owner", "workflow", "phase", "remote-target", "/srv/repo", "review only bounded remote child result", nil, continuation.Task.TaskID, continuation.ParentAttemptID, nil, func() *RemoteCodingSubAgentResult {
		return &RemoteCodingSubAgentResult{Status: "success", Summary: "reviewed remote child evidence"}
	})
	if err != nil || review == nil || reviewAttempt == nil || reviewAttempt.AttemptID == parentAttempt.AttemptID || reviewAttempt.AttemptNo != 2 || review.RuntimeTaskID != result.RuntimeTaskID {
		t.Fatalf("review=%#v attempt=%#v parent=%#v err=%v", review, reviewAttempt, parentAttempt, err)
	}
}

func TestRunGUIRemoteCodingTaskWithLedgerRejectsConcurrentChildReviewWithoutSecondAttempt(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "parent", WorkflowID: "workflow", PhaseID: "phase", ProjectRef: "/srv/repo", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	policy := codingruntime.PolicySnapshot{ProjectRoot: "/srv/repo", RemoteTarget: "remote-target", Mode: "remote"}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Digest = digest
	if _, err := store.StartAttempt(task.TaskID, "active-review", time.Minute, policy, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, attempt, err := runGUIRemoteCodingTaskWithStartAndContinuation(context.Background(), store, "second-review", "workflow", "phase", "remote-target", "/srv/repo", "review", nil, task.TaskID, "", nil, func() *RemoteCodingSubAgentResult {
		calls++
		return &RemoteCodingSubAgentResult{Status: "success"}
	})
	if err != nil || result == nil || result.Error != "runtime_child_review_in_progress" || !result.RuntimeHandoff || calls != 0 || attempt != nil {
		t.Fatalf("result=%#v attempt=%#v calls=%d err=%v", result, attempt, calls, err)
	}
	attempts, err := store.ListAttempts(task.TaskID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%#v err=%v", attempts, err)
	}
}

func TestRunGUICodingTaskWithLedgerBlocksIsolatedWriterWhenMergeGateFails(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	gateCalls := 0
	result, attempt, err := runGUICodingTaskWithLedgerWithOptions(
		context.Background(), store, "gui:test", "workflow", "phase", "D:/repo", "change a.go", nil, nil,
		&guiCodingRuntimeOptions{
			DeclaredWrites:       []string{"a.go"},
			PolicyProjectRoot:    "D:/primary-repo",
			WorkspaceIsolated:    true,
			RequireFinalDiffGate: true,
			FinalizeWriter: func(*CodingSubAgentResult) (bool, error) {
				gateCalls++
				return false, fmt.Errorf("controlled cherry-pick conflict")
			},
		},
		func() *CodingSubAgentResult {
			return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "agent changed a.go"}
		},
	)
	if err != nil || result == nil || attempt == nil {
		t.Fatalf("result=%#v attempt=%#v err=%v", result, attempt, err)
	}
	if gateCalls != 1 || result.Status != TaskExecSkipped || attempt.Status != codingruntime.TaskBlocked || attempt.ErrorCode != "final_diff_gate_failed" {
		t.Fatalf("gate=%d result=%#v attempt=%#v", gateCalls, result, attempt)
	}
}

func TestRunGUICodingTaskWithLedgerPersistsIsolatedWriterGateSuccess(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	result, attempt, err := runGUICodingTaskWithLedgerWithOptions(
		context.Background(), store, "gui:test", "workflow", "phase", "D:/repo", "change a.go", nil, nil,
		&guiCodingRuntimeOptions{
			DeclaredWrites:       []string{"a.go"},
			PolicyProjectRoot:    "D:/primary-repo",
			WorkspaceIsolated:    true,
			RequireFinalDiffGate: true,
			FinalizeWriter:       func(*CodingSubAgentResult) (bool, error) { return true, nil },
		},
		func() *CodingSubAgentResult {
			return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "agent changed a.go"}
		},
	)
	if err != nil || result == nil || attempt == nil {
		t.Fatalf("result=%#v attempt=%#v err=%v", result, attempt, err)
	}
	if result.Status != TaskExecPassed || attempt.Status != codingruntime.TaskCompleted || !attempt.Policy.FinalDiffGateRequired || !attempt.Policy.WorkspaceIsolated || len(attempt.Policy.WriteSet.Claims) != 1 || filepath.Clean(attempt.Policy.ProjectRoot) != filepath.Clean("D:/primary-repo") {
		t.Fatalf("result=%#v attempt=%#v", result, attempt)
	}
}

func TestGUICodingRuntimeVerifiedNoChangeEvidencePassesFinalWorkspaceGate(t *testing.T) {
	adapter := &guiCodingRuntimeAdapter{run: func() *CodingSubAgentResult {
		return &CodingSubAgentResult{
			Status:             TaskExecPassed,
			ExplorationStatus:  codingSubAgentQualityPassed,
			ExplorationSummary: "read target implementation",
			QualityStatus:      codingSubAgentQualityPassed,
			QualitySummary:     "inspection confirms requested behavior already exists",
		}
	}}
	out := adapter.Execute(context.Background(), codingruntime.ExecutionRequest{})
	if out.Status != codingruntime.TaskCompleted || out.NoWorkspaceChangeEvidenceDigest == "" {
		t.Fatalf("execution result=%#v", out)
	}
}
