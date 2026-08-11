package codingruntime

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStorePersistsInterruptedRecoveryCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coding_runtime.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	task, err := store.CreateTask(Task{ProjectRef: "project-a", RequestedWork: "write"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "worker-a", time.Second, PolicySnapshot{ProjectRoot: "project-a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "worker-a", "tool_started", "digest", now); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	expired, err := reopened.ExpireLeases(now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].Status != TaskInterrupted || expired[0].SideEffectState != SideEffectUncertain {
		t.Fatalf("expired=%+v", expired)
	}
	candidates, err := reopened.ListRecoveryCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].AttemptID != attempt.AttemptID {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestSQLiteStoreLeaseAndTerminalOwnership(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task, err := store.CreateTask(Task{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Hour, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(task.TaskID, "other", time.Hour, PolicySnapshot{}, now); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("lease error=%v", err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "other", FinishInput{Status: TaskFailed}, now); !errors.Is(err, ErrLeaseOwnerMismatch) {
		t.Fatalf("owner error=%v", err)
	}
	finished, err := store.FinishAttempt(attempt.AttemptID, "owner", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}, now)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != TaskCompleted {
		t.Fatalf("finished=%+v", finished)
	}
}

func TestSQLiteStoreCancelTaskPropagatesToChildSubtree(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	handles, err := (ChildTaskService{Store: store, Now: func() time.Time { return now }}).AdmitReadOnlyChildren(parentAttempt.AttemptID, "parent", []ChildTaskSpec{{Name: "inspect", RequestedWork: "inspect", ProjectRef: "repo", Mode: "local"}}, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	childAttempt, err := store.StartAttempt(handles[0].TaskID, "child", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelTask(parent.TaskID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{parent.TaskID, handles[0].TaskID} {
		task, getErr := store.GetTask(id)
		if getErr != nil || task.Status != TaskCancelled {
			t.Fatalf("task %s=%+v err=%v", id, task, getErr)
		}
	}
	child, err := store.GetAttempt(childAttempt.AttemptID)
	if err != nil || child.Status != TaskCancelled || child.SideEffectState != SideEffectUncertain {
		t.Fatalf("child attempt=%+v err=%v", child, err)
	}
	events, err := store.ListEvents(childAttempt.AttemptID)
	if err != nil || len(events) != 1 || events[0].Type != "task_cancelled" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestSQLiteStoreRecordsLateCallbackWithoutChangingTerminalAttempt(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task, err := store.CreateTask(Task{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Hour, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "owner", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	event, err := store.RecordStaleCallback(attempt.AttemptID, "sha256:late", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "stale_callback_discarded" || event.Sequence != 1 {
		t.Fatalf("event=%+v", event)
	}
	current, err := store.GetAttempt(attempt.AttemptID)
	if err != nil || current.Status != TaskInterrupted {
		t.Fatalf("attempt=%+v err=%v", current, err)
	}
	events, err := store.ListEvents(attempt.AttemptID)
	if err != nil || len(events) != 1 || events[0].Type != "stale_callback_discarded" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestSQLiteStoreWriterAdmissionSurvivesSeparateStoreHandles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "runtime.db")
	first, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	a, err := first.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.StartAttempt(a.TaskID, "first", time.Minute, PolicySnapshot{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := second.StartAttempt(b.TaskID, "second", time.Minute, PolicySnapshot{}, now); !errors.Is(err, ErrWriterConflict) {
		t.Fatalf("err=%v, want writer conflict", err)
	}
}

func TestSQLiteStoreWriterAdmissionAllowsGuardedDisjointAcrossSeparateHandles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "runtime.db")
	first, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	a, err := first.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.CreateTask(Task{ProjectRef: filepath.Join("D:", "repo"), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	left := PolicySnapshot{ProjectRoot: filepath.Join("D:", "repo"), Mode: "local", WorkspaceIsolated: true, FinalDiffGateRequired: true, WriteSet: WriteSet{Claims: []WriteClaim{{Path: "a.go"}}}}
	right := left
	right.WriteSet.Claims = []WriteClaim{{Path: "b.go"}}
	if _, err := first.StartAttempt(a.TaskID, "first", time.Minute, left, now); err != nil {
		t.Fatal(err)
	}
	if _, err := second.StartAttempt(b.TaskID, "second", time.Minute, right, now); err != nil {
		t.Fatalf("guarded disjoint cross-handle writer err=%v", err)
	}
}

func TestSQLiteStoreListAttemptsOrdersByAttemptNumber(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task, err := store.CreateTask(Task{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first, err := store.StartAttempt(task.TaskID, "owner", time.Hour, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(first.AttemptID, "owner", FinishInput{Status: TaskInterrupted}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTaskReadyForRecovery(task.TaskID, now); err != nil {
		t.Fatal(err)
	}
	second, err := store.StartAttempt(task.TaskID, "owner", time.Hour, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := store.ListAttempts(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].AttemptID != first.AttemptID || attempts[1].AttemptID != second.AttemptID {
		t.Fatalf("attempts=%+v", attempts)
	}
}

func TestSQLiteStoreExpiredChildInterruptsWaitingParent(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)
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
	children, err := store.ListChildTasks(parent.TaskID)
	if err != nil || len(children) != 1 || children[0].Status != TaskInterrupted {
		t.Fatalf("children=%+v err=%v", children, err)
	}
	// A recovery plan is inspect-only and cannot accidentally run the expired child.
	plan, err := (RecoveryService{Store: store, Now: func() time.Time { return now }}).PrepareRecoveryForTask(parent.TaskID)
	if err != nil || len(plan.Children) != 1 || plan.Children[0].Status != TaskInterrupted {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestSQLiteStoreUnstartedChildInterruptsWaitingParent(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 10, 16, 0, 0, 0, time.UTC)
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
}
