package codingruntime

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type readOnlyChildExecutorFunc func(context.Context, ExecutionRequest) ChildTaskResult

func (f readOnlyChildExecutorFunc) ExecuteReadOnlyChild(ctx context.Context, request ExecutionRequest) ChildTaskResult {
	return f(ctx, request)
}

type nonComparableStore struct {
	Store
	marker []string
}

func TestChildTaskAdmissionReleasesParentLeaseAndDeliversBoundedResults(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local", Status: TaskQueued, PolicyDigest: "parent-policy"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "gui:parent", time.Minute, PolicySnapshot{Digest: "parent-policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	children, err := (ChildTaskService{Store: store, Now: func() time.Time { return now }}).AdmitReadOnlyChildren(parentAttempt.AttemptID, "gui:parent", []ChildTaskSpec{
		{Name: "map auth", RequestedWork: "map auth", ProjectRef: "repo", Mode: "local"},
		{Name: "find tests", RequestedWork: "find tests", ProjectRef: "repo", Mode: "local"},
	}, PolicySnapshot{Digest: "child-policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil || len(children) != 2 {
		t.Fatalf("children=%#v err=%v", children, err)
	}
	storedParentAttempt, err := store.GetAttempt(parentAttempt.AttemptID)
	if err != nil || storedParentAttempt.Status != TaskWaitingChild || !storedParentAttempt.LeaseUntil.IsZero() {
		t.Fatalf("parent attempt=%#v err=%v", storedParentAttempt, err)
	}
	storedParent, err := store.GetTask(parent.TaskID)
	if err != nil || storedParent.Status != TaskWaitingChild {
		t.Fatalf("parent=%#v err=%v", storedParent, err)
	}

	childResults := make([]ChildTaskResult, 0, len(children))
	for _, handle := range children {
		_, attempt, err := (Runner{Store: store, LeaseOwner: "gui:child:" + handle.TaskID, Now: func() time.Time { return now }}).Run(context.Background(), Task{TaskID: handle.TaskID}, PolicySnapshot{Digest: "child-policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
			return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectNone}
		}))
		if err != nil || attempt == nil || attempt.Status != TaskCompleted {
			t.Fatalf("child attempt=%#v err=%v", attempt, err)
		}
		childResults = append(childResults, ChildTaskResult{TaskID: handle.TaskID, AttemptID: attempt.AttemptID, Summary: strings.Repeat("x", maxChildResultSummaryRunes+100), EvidenceDigest: "sha256:evidence"})
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	if _, err := service.CompleteChildTask(childResults[0].TaskID, childResults[0]); err != nil {
		t.Fatal(err)
	}
	if parent, _ := store.GetTask("parent"); parent.Status != TaskWaitingChild {
		t.Fatalf("parent should wait for all child results: %#v", parent)
	}
	if _, err := service.CompleteChildTask(childResults[1].TaskID, childResults[1]); err != nil {
		t.Fatal(err)
	}
	if parent, _ := store.GetTask("parent"); parent.Status != TaskQueued {
		t.Fatalf("parent must become queued for a new attempt, got %#v", parent)
	}
	results, err := service.ListChildResults("parent")
	if err != nil || len(results) != 2 || len([]rune(results[0].Summary)) != maxChildResultSummaryRunes {
		t.Fatalf("results=%#v err=%v", results, err)
	}
}

func TestChildTaskAdmissionRejectsWritePolicyAndParentScopeChange(t *testing.T) {
	store := NewMemoryStore()
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "remote", Status: TaskQueued})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(parent.TaskID, "owner", time.Minute, PolicySnapshot{Mode: "remote"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store}
	if _, err := service.AdmitReadOnlyChild(attempt.AttemptID, "owner", ChildTaskSpec{Name: "writer", RequestedWork: "change", ProjectRef: "repo", Mode: "remote"}, PolicySnapshot{Mode: "remote"}); err == nil {
		t.Fatal("write-capable child policy must be rejected")
	}
	if _, err := service.AdmitReadOnlyChild(attempt.AttemptID, "owner", ChildTaskSpec{Name: "other project", RequestedWork: "inspect", ProjectRef: "other", Mode: "remote"}, PolicySnapshot{Mode: "remote", ReadOnly: true}); err == nil {
		t.Fatal("child project expansion must be rejected")
	}
}

func TestRunReadOnlyChildUsesFreshAttemptAndCannotExposeWritePolicy(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: store, LeaseOwner: "child", Now: func() time.Time { return now }}
	_, attempt, returnedParent, err := service.RunReadOnlyChild(context.Background(), runner, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, readOnlyChildExecutorFunc(func(_ context.Context, request ExecutionRequest) ChildTaskResult {
		if !request.Attempt.Policy.ReadOnly {
			t.Fatal("child execution must receive read-only policy")
		}
		return ChildTaskResult{Status: TaskCompleted, Summary: "found", EvidenceDigest: "sha256:found"}
	}))
	if err != nil || attempt == nil || attempt.Status != TaskCompleted || returnedParent == nil || returnedParent.Status != TaskQueued {
		t.Fatalf("attempt=%#v parent=%#v err=%v", attempt, returnedParent, err)
	}
	if _, _, _, err := service.RunReadOnlyChild(context.Background(), runner, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult { return ChildTaskResult{} })); err == nil {
		t.Fatal("write-capable run policy must be rejected")
	}
}

func TestRunReadOnlyChildRejectsMismatchedServiceAndRunnerStores(t *testing.T) {
	serviceStore := NewMemoryStore()
	runnerStore := NewMemoryStore()
	calls := 0
	_, attempt, parent, err := (ChildTaskService{Store: serviceStore}).RunReadOnlyChild(context.Background(), Runner{Store: runnerStore, LeaseOwner: "child"}, "unknown-child", PolicySnapshot{ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		calls++
		return ChildTaskResult{Status: TaskCompleted}
	}))
	if !errors.Is(err, ErrInvalidTransition) || attempt != nil || parent != nil || calls != 0 {
		t.Fatalf("attempt=%#v parent=%#v err=%v calls=%d", attempt, parent, err, calls)
	}
}

func TestRunReadOnlyChildRejectsMissingStores(t *testing.T) {
	_, attempt, parent, err := (ChildTaskService{}).RunReadOnlyChild(context.Background(), Runner{LeaseOwner: "child"}, "child", PolicySnapshot{ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		t.Fatal("executor must not run")
		return ChildTaskResult{}
	}))
	if err == nil || attempt != nil || parent != nil {
		t.Fatalf("attempt=%#v parent=%#v err=%v", attempt, parent, err)
	}
}

func TestRunReadOnlyChildRejectsNonComparableStoreWithoutPanic(t *testing.T) {
	base := NewMemoryStore()
	store := nonComparableStore{Store: base, marker: []string{"non-comparable"}}
	calls := 0
	_, attempt, parent, err := (ChildTaskService{Store: store}).RunReadOnlyChild(context.Background(), Runner{Store: store, LeaseOwner: "child"}, "child", PolicySnapshot{ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		calls++
		return ChildTaskResult{Status: TaskCompleted}
	}))
	if !errors.Is(err, ErrInvalidTransition) || attempt != nil || parent != nil || calls != 0 {
		t.Fatalf("attempt=%#v parent=%#v err=%v calls=%d", attempt, parent, err, calls)
	}
}

func TestRunReadOnlyChildRejectsTypedNilStoreWithoutPanic(t *testing.T) {
	var nilStore *MemoryStore
	calls := 0
	_, attempt, parent, err := (ChildTaskService{Store: nilStore}).RunReadOnlyChild(context.Background(), Runner{Store: nilStore, LeaseOwner: "child"}, "child", PolicySnapshot{ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		calls++
		return ChildTaskResult{Status: TaskCompleted}
	}))
	if !errors.Is(err, ErrInvalidTransition) || attempt != nil || parent != nil || calls != 0 {
		t.Fatalf("attempt=%#v parent=%#v err=%v calls=%d", attempt, parent, err, calls)
	}
}

func TestPrepareParentContinuationRequiresDurableChildDelivery(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PrepareParentContinuation(parent.TaskID); err == nil {
		t.Fatal("parent continuation was exposed before child delivery")
	}
	runner := Runner{Store: store, LeaseOwner: "child", Now: func() time.Time { return now }}
	if _, _, _, err := service.RunReadOnlyChild(context.Background(), runner, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		return ChildTaskResult{Status: TaskCompleted, Summary: "located implementation", EvidenceDigest: "sha256:child"}
	})); err != nil {
		t.Fatal(err)
	}
	continuation, err := service.PrepareParentContinuation(parent.TaskID)
	if err != nil {
		t.Fatalf("PrepareParentContinuation: %v", err)
	}
	if continuation.Task.TaskID != parent.TaskID || len(continuation.ChildResults) != 1 || continuation.ChildResults[0].EvidenceDigest != "sha256:child" {
		t.Fatalf("continuation=%+v", continuation)
	}
	// Reading the handoff does not run or reserve the parent. Only the host's
	// later explicit Runner.Run call can create AttemptNo=2.
	attempts, err := store.ListAttempts(parent.TaskID)
	if err != nil || len(attempts) != 1 || attempts[0].AttemptNo != 1 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
}

func TestParentContinuationCarriesWaitingAttemptIdentity(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	policy := PolicySnapshot{ProjectRoot: "repo", Mode: "local"}
	attempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(attempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = service.RunReadOnlyChild(context.Background(), Runner{Store: store, LeaseOwner: "child", LeaseDuration: time.Minute, Now: func() time.Time { return now }}, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		return ChildTaskResult{Status: TaskCompleted, Summary: "finding", EvidenceDigest: "sha256:finding"}
	})); err != nil {
		t.Fatal(err)
	}
	continuation, err := service.PrepareParentContinuation(parent.TaskID)
	if err != nil || continuation == nil || continuation.ParentAttemptID != attempt.AttemptID {
		t.Fatalf("continuation=%#v parent_attempt=%#v err=%v", continuation, attempt, err)
	}
}

func TestPrepareParentContinuationRejectsConsumedHandoffBeforeLoadingResults(t *testing.T) {
	stores := []struct {
		name  string
		store Store
	}{
		{name: "memory", store: NewMemoryStore()},
		{name: "sqlite", store: func() Store {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		}()},
	}
	for _, tt := range stores {
		t.Run(tt.name, func(t *testing.T) {
			if closer, ok := tt.store.(interface{ Close() error }); ok {
				t.Cleanup(func() { _ = closer.Close() })
			}
			now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
			parent, err := tt.store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
			if err != nil {
				t.Fatal(err)
			}
			policy := PolicySnapshot{ProjectRoot: "repo", Mode: "local"}
			parentAttempt, err := tt.store.StartAttempt(parent.TaskID, "parent", time.Minute, policy, now)
			if err != nil {
				t.Fatal(err)
			}
			service := ChildTaskService{Store: tt.store, Now: func() time.Time { return now }}
			handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := service.RunReadOnlyChild(context.Background(), Runner{Store: tt.store, LeaseOwner: "child", LeaseDuration: time.Minute, Now: func() time.Time { return now }}, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
				return ChildTaskResult{Status: TaskCompleted, Summary: "finding", EvidenceDigest: "sha256:finding"}
			})); err != nil {
				t.Fatal(err)
			}
			continuation, err := service.PrepareParentContinuation(parent.TaskID)
			if err != nil || continuation == nil {
				t.Fatalf("continuation=%#v err=%v", continuation, err)
			}
			if _, _, err = (Runner{Store: tt.store, LeaseOwner: "review", LeaseDuration: time.Minute, Now: func() time.Time { return now }}).RunWithContinuation(context.Background(), Task{TaskID: parent.TaskID}, policy, ContinuationReview{ParentAttemptID: continuation.ParentAttemptID}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
				return ExecutionResult{Status: TaskBlocked, SideEffectState: SideEffectNone}
			})); err != nil {
				t.Fatal(err)
			}
			if continuation, err := service.PrepareParentContinuation(parent.TaskID); !errors.Is(err, ErrContinuationConsumed) || continuation != nil {
				t.Fatalf("continuation=%#v err=%v", continuation, err)
			}
		})
	}
}

func TestRunnerRequiresExplicitContinuationForQueuedParentWithChildResults(t *testing.T) {
	stores := []struct {
		name  string
		store Store
	}{
		{name: "memory", store: NewMemoryStore()},
		{name: "sqlite", store: func() Store {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		}()},
	}
	for _, tt := range stores {
		t.Run(tt.name, func(t *testing.T) {
			if closer, ok := tt.store.(interface{ Close() error }); ok {
				t.Cleanup(func() { _ = closer.Close() })
			}
			now := time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC)
			parent, err := tt.store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
			if err != nil {
				t.Fatal(err)
			}
			policy := PolicySnapshot{ProjectRoot: "repo", Mode: "local"}
			parentAttempt, err := tt.store.StartAttempt(parent.TaskID, "parent", time.Minute, policy, now)
			if err != nil {
				t.Fatal(err)
			}
			service := ChildTaskService{Store: tt.store, Now: func() time.Time { return now }}
			handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := service.RunReadOnlyChild(context.Background(), Runner{Store: tt.store, LeaseOwner: "child", LeaseDuration: time.Minute, Now: func() time.Time { return now }}, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
				return ChildTaskResult{Status: TaskCompleted, Summary: "finding", EvidenceDigest: "sha256:finding"}
			})); err != nil {
				t.Fatal(err)
			}
			calls := 0
			runner := Runner{Store: tt.store, LeaseOwner: "review", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
			if _, attempt, err := runner.Run(context.Background(), Task{TaskID: parent.TaskID}, policy, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
				calls++
				return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectNone}
			})); !errors.Is(err, ErrContinuationRequired) || attempt != nil || calls != 0 {
				t.Fatalf("ordinary run must not bypass handoff: attempt=%#v err=%v calls=%d", attempt, err, calls)
			}
			continuation, err := service.PrepareParentContinuation(parent.TaskID)
			if err != nil || continuation == nil {
				t.Fatalf("continuation=%#v err=%v", continuation, err)
			}
			if _, _, err := runner.RunWithContinuation(context.Background(), Task{TaskID: parent.TaskID}, policy, ContinuationReview{ParentAttemptID: continuation.ParentAttemptID}, executorFunc(func(context.Context, ExecutionRequest) ExecutionResult {
				calls++
				return ExecutionResult{Status: TaskCompleted, SideEffectState: SideEffectNone}
			})); err != nil || calls != 1 {
				t.Fatalf("explicit handoff review err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestRunReadOnlyChildRecoversTerminalUndeliveredChildWithoutRerunningExecutor(t *testing.T) {
	stores := []struct {
		name  string
		store Store
	}{
		{name: "memory", store: NewMemoryStore()},
		{name: "sqlite", store: func() Store {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		}()},
	}
	for _, tt := range stores {
		t.Run(tt.name, func(t *testing.T) {
			if closer, ok := tt.store.(interface{ Close() error }); ok {
				t.Cleanup(func() { _ = closer.Close() })
			}
			now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
			parent, err := tt.store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
			if err != nil {
				t.Fatal(err)
			}
			policy := PolicySnapshot{ProjectRoot: "repo", Mode: "local"}
			parentAttempt, err := tt.store.StartAttempt(parent.TaskID, "parent", time.Minute, policy, now)
			if err != nil {
				t.Fatal(err)
			}
			service := ChildTaskService{Store: tt.store, Now: func() time.Time { return now }}
			handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			childPolicy := PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}
			childAttempt, err := tt.store.StartAttempt(handle.TaskID, "child", time.Minute, childPolicy, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tt.store.FinishAttempt(childAttempt.AttemptID, "child", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectNone, ErrorSummary: "completed before delivery"}, now); err != nil {
				t.Fatal(err)
			}
			calls := 0
			child, attempt, recoveredParent, err := service.RunReadOnlyChild(context.Background(), Runner{Store: tt.store, LeaseOwner: "duplicate", LeaseDuration: time.Minute, Now: func() time.Time { return now }}, handle.TaskID, childPolicy, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
				calls++
				return ChildTaskResult{Status: TaskCompleted}
			}))
			if err != nil || child == nil || child.Status != TaskCompleted || attempt != nil || recoveredParent == nil || recoveredParent.Status != TaskQueued || calls != 0 {
				t.Fatalf("child=%#v attempt=%#v parent=%#v err=%v calls=%d", child, attempt, recoveredParent, err, calls)
			}
			results, err := service.ListChildResults(parent.TaskID)
			if err != nil || len(results) != 1 || results[0].AttemptID != childAttempt.AttemptID || results[0].Status != TaskCompleted {
				t.Fatalf("results=%#v err=%v", results, err)
			}
			if _, _, _, err = service.RunReadOnlyChild(context.Background(), Runner{Store: tt.store, LeaseOwner: "duplicate-2", LeaseDuration: time.Minute, Now: func() time.Time { return now }}, handle.TaskID, childPolicy, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
				calls++
				return ChildTaskResult{Status: TaskCompleted}
			})); err != nil || calls != 0 {
				t.Fatalf("duplicate terminal delivery err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestStartReadOnlyChildReturnsBeforeCompletionAndDeliversResult(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	completion := service.StartReadOnlyChild(context.Background(), Runner{Store: store, LeaseOwner: "child", Now: func() time.Time { return now }}, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		close(started)
		<-release
		return ChildTaskResult{Status: TaskCompleted, Summary: "found", EvidenceDigest: "sha256:found"}
	}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("child did not begin asynchronously")
	}
	select {
	case outcome := <-completion:
		t.Fatalf("child completed before release: %+v", outcome)
	default:
	}
	if current, err := store.GetTask(parent.TaskID); err != nil || current.Status != TaskWaitingChild {
		t.Fatalf("parent must wait while child executes: %+v err=%v", current, err)
	}
	close(release)
	select {
	case outcome := <-completion:
		if outcome.Err != nil || outcome.Attempt == nil || outcome.Attempt.Status != TaskCompleted || outcome.Parent == nil || outcome.Parent.Status != TaskQueued {
			t.Fatalf("outcome=%+v", outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("child completion was not delivered")
	}
}

func TestCancelledLiveChildKeepsDurableCancellationAndDiscardsLateCallback(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	var executions ChildExecutionRegistry
	ctx, release := executions.Begin(parent.TaskID, handle.TaskID)
	defer release()
	started := make(chan struct{})
	finished := make(chan struct{})
	completion := service.StartReadOnlyChild(ctx, Runner{Store: store, LeaseOwner: "child", Now: func() time.Time { return now }}, handle.TaskID, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, readOnlyChildExecutorFunc(func(context.Context, ExecutionRequest) ChildTaskResult {
		close(started)
		// Simulate a host callback which cannot be physically aborted at once.
		// Its late success must not overwrite the durable cancellation below.
		<-finished
		return ChildTaskResult{Status: TaskCompleted, Summary: "late success", EvidenceDigest: "sha256:late"}
	}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("child did not begin")
	}

	if _, err := store.CancelTask(parent.TaskID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	executions.CancelParent(parent.TaskID)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("live child context was not cancelled")
	}
	close(finished)
	select {
	case outcome := <-completion:
		if outcome.Err != ErrStaleAttempt {
			t.Fatalf("late child outcome err=%v, want ErrStaleAttempt", outcome.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("late child callback did not return")
	}
	child, err := store.GetTask(handle.TaskID)
	if err != nil || child.Status != TaskCancelled {
		t.Fatalf("child=%+v err=%v", child, err)
	}
	attempts, err := store.ListAttempts(handle.TaskID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("child attempts=%+v err=%v", attempts, err)
	}
	events, err := store.ListEvents(attempts[0].AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	foundStale := false
	for _, event := range events {
		if event.Type == "stale_callback_discarded" {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Fatalf("late child completion was not audited: %+v", events)
	}
}

func TestExpiredChildInterruptsWaitingParentForExplicitRecovery(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(handle.TaskID, "child", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExpireLeases(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	currentParent, err := store.GetTask(parent.TaskID)
	if err != nil || currentParent.Status != TaskInterrupted {
		t.Fatalf("parent=%+v err=%v", currentParent, err)
	}
	continuation, err := service.PrepareParentContinuation(parent.TaskID)
	if err == nil || continuation != nil {
		t.Fatalf("interrupted parent must not expose continuation: %+v err=%v", continuation, err)
	}
	plan, err := (RecoveryService{Store: store, Now: func() time.Time { return now }}).PrepareRecoveryForTask(parent.TaskID)
	if err != nil || len(plan.Children) != 1 || plan.Children[0].TaskID != handle.TaskID || plan.Children[0].Status != TaskInterrupted {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestUnstartedChildInterruptsWaitingParentWithoutDispatch(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	interrupted, err := store.InterruptUnstartedChildren(now)
	if err != nil || len(interrupted) != 1 || interrupted[0].TaskID != parent.TaskID || interrupted[0].Status != TaskInterrupted {
		t.Fatalf("interrupted=%+v err=%v", interrupted, err)
	}
	child, err := store.GetTask(handle.TaskID)
	if err != nil || child.Status != TaskInterrupted {
		t.Fatalf("child=%+v err=%v", child, err)
	}
	if _, err := store.StartAttempt(handle.TaskID, "child", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now); err == nil {
		t.Fatal("reconciled child must not be auto-dispatched")
	}
}

func TestCancelParentPropagatesToRunningAndQueuedChildren(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handles, err := service.AdmitReadOnlyChildren(parentAttempt.AttemptID, "parent", []ChildTaskSpec{
		{Name: "running", RequestedWork: "inspect running", ProjectRef: "repo", Mode: "local"},
		{Name: "queued", RequestedWork: "inspect queued", ProjectRef: "repo", Mode: "local"},
	}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.StartAttempt(handles[0].TaskID, "child", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.CancelTask(parent.TaskID, now.Add(time.Second))
	if err != nil || len(cancelled) != 2 {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	for _, id := range []string{parent.TaskID, handles[0].TaskID, handles[1].TaskID} {
		task, getErr := store.GetTask(id)
		if getErr != nil || task.Status != TaskCancelled {
			t.Fatalf("task %s=%+v err=%v", id, task, getErr)
		}
	}
	if current, getErr := store.GetAttempt(running.AttemptID); getErr != nil || current.Status != TaskCancelled || current.SideEffectState != SideEffectUncertain {
		t.Fatalf("running child attempt=%+v err=%v", current, getErr)
	}
	if current, getErr := store.GetAttempt(parentAttempt.AttemptID); getErr != nil || current.Status != TaskCancelled {
		t.Fatalf("parent attempt=%+v err=%v", current, getErr)
	}
	if _, err = service.PrepareParentContinuation(parent.TaskID); err == nil {
		t.Fatal("cancelled parent must not expose a continuation")
	}
	if _, err = store.StartAttempt(handles[1].TaskID, "late-child", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now.Add(2*time.Second)); err == nil {
		t.Fatal("cancelled queued child must not begin later")
	}
}
