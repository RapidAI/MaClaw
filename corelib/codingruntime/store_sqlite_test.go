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

func TestSQLiteSameTaskExpiredLeaseIsRecordedAndRefused(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 24, 10, 26, 0, 0, time.UTC)
	task, err := store.CreateTask(Task{ProjectRef: `E:\prog-test1`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	policy := PolicySnapshot{ProjectRoot: `E:\prog-test1`, Mode: "local"}
	attempt, err := store.StartAttempt(task.TaskID, "holder", time.Minute, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	markLocalLiveWriter(attempt.AttemptID)
	t.Cleanup(func() { unmarkLocalLiveWriter(attempt.AttemptID) })
	if _, err := store.StartAttempt(task.TaskID, "again", time.Minute, policy, now.Add(2*time.Minute)); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("local executor error = %v, want ErrLeaseHeld", err)
	}
	held, err := store.GetTask(task.TaskID)
	if err != nil || held.Status != TaskRunning {
		t.Fatalf("local executor was interrupted: %+v %v", held, err)
	}
	unmarkLocalLiveWriter(attempt.AttemptID)
	if _, err := store.StartAttempt(task.TaskID, "again", time.Minute, policy, now.Add(2*time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("dead same-task lease error = %v, want ErrInvalidTransition", err)
	}
	leaseEnd := now.Add(time.Minute)
	closed, err := store.GetTask(task.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("task=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
	}
	attempts, err := store.ListAttempts(task.TaskID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != TaskInterrupted || !attempts[0].FinishedAt.Equal(leaseEnd) {
		t.Fatalf("attempt=%+v err=%v", attempts, err)
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

func TestSQLiteStoreListActiveTaskStatusesByProjectRef(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 18, 22, 0, 0, 0, time.UTC)
	// Running remote run on /home/testprj-2.
	remote, err := store.CreateTask(Task{TaskID: "remote-run", ProjectRef: "/home/testprj-2", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(remote.TaskID, "worker", leaseStillOpen(now), PolicySnapshot{ProjectRoot: "/home/testprj-2", Mode: "remote"}, now); err != nil {
		t.Fatal(err)
	}
	// Terminal local run on F:\test-prog must not surface.
	done, err := store.CreateTask(Task{TaskID: "done-run", ProjectRef: `F:\test-prog`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(done.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: `F:\test-prog`, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "worker", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectObserved}, now); err != nil {
		t.Fatal(err)
	}
	// A blocked run on another ref surfaces under its own status.
	blocked, err := store.CreateTask(Task{TaskID: "blocked-run", ProjectRef: "/opt/app", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	blockedAttempt, err := store.StartAttempt(blocked.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: "/opt/app", Mode: "remote"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(blockedAttempt.AttemptID, "worker", FinishInput{Status: TaskBlocked, SideEffectState: SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}

	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active["/home/testprj-2"]; got != string(TaskRunning) {
		t.Fatalf("remote ref status=%q", got)
	}
	if got := active["/opt/app"]; got != string(TaskBlocked) {
		t.Fatalf("blocked ref status=%q", got)
	}
	invisible, err := store.CreateTask(Task{TaskID: "invisible-edit", ProjectRef: "/home/prj8", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	invisibleAttempt, err := store.StartAttempt(invisible.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: "/home/prj8", Mode: "remote"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(invisibleAttempt.AttemptID, "worker", "remote_file_activity", "sha256:edited-untracked", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(invisibleAttempt.AttemptID, "worker", FinishInput{Status: TaskBlocked, SideEffectState: SideEffectObserved, ErrorCode: "final_workspace_unchanged", ErrorSummary: "porcelain unchanged"}, now); err != nil {
		t.Fatal(err)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active["/home/prj8"]; got != string(TaskCompleted) {
		t.Fatalf("untracked content edit status=%q, want completed", got)
	}
	if _, ok := active[`F:\test-prog`]; ok {
		t.Fatalf("completed ref surfaced: %v", active)
	}

	// A failed turn is not a completed write. The task list has no other
	// record of that outcome, so the newest failed row must stay visible.
	failed, err := store.CreateTask(Task{TaskID: "failed-run", ProjectRef: `F:\goaltest`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	failedAttempt, err := store.StartAttempt(failed.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: `F:\goaltest`, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(failedAttempt.AttemptID, "worker", FinishInput{Status: TaskFailed, SideEffectState: SideEffectObserved}, now); err != nil {
		t.Fatal(err)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[`F:\goaltest`]; got != string(TaskFailed) {
		t.Fatalf("failed ref status=%q, want failed", got)
	}

	// A child that finishes after the parent failed shares the ref. Dropping
	// that completion must not drop the parent's failure with it.
	childDone, err := store.CreateTask(Task{TaskID: "child-done", ParentTaskID: failed.TaskID, ProjectRef: `F:\goaltest`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	childDoneAttempt, err := store.StartAttempt(childDone.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: `F:\goaltest`, Mode: "local"}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(childDoneAttempt.AttemptID, "worker", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectObserved}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[`F:\goaltest`]; got != string(TaskFailed) {
		t.Fatalf("completed child hid the parent failure: %q", got)
	}

	// A lease expiry marks the child interrupted and bumps its timestamp.
	// That row is still not the task outcome.
	childStopped, err := store.CreateTask(Task{TaskID: "child-stopped", ParentTaskID: failed.TaskID, ProjectRef: `F:\goaltest`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	childStoppedAttempt, err := store.StartAttempt(childStopped.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: `F:\goaltest`, Mode: "local"}, now.Add(1500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(childStoppedAttempt.AttemptID, "worker", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, now.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTask(Task{TaskID: "child-queued", ParentTaskID: failed.TaskID, ProjectRef: `F:\goaltest`, Mode: "local", CreatedAt: now.Add(1800 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[`F:\goaltest`]; got != string(TaskFailed) {
		t.Fatalf("interrupted or queued child hid the parent failure: %q", got)
	}

	// A later completed run is the outcome. The older failure must not stick.
	later, err := store.CreateTask(Task{TaskID: "later-ok", ProjectRef: `F:\goaltest`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	laterAttempt, err := store.StartAttempt(later.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: `F:\goaltest`, Mode: "local"}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(laterAttempt.AttemptID, "worker", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectObserved}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// A child that fails after the parent finished shares the ref and must not
	// put the task back into the failed bucket.
	child, err := store.CreateTask(Task{TaskID: "child-fail", ParentTaskID: later.TaskID, ProjectRef: `F:\goaltest`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	childAttempt, err := store.StartAttempt(child.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: `F:\goaltest`, Mode: "local"}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(childAttempt.AttemptID, "worker", FinishInput{Status: TaskFailed, SideEffectState: SideEffectNone}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := active[`F:\goaltest`]; ok {
		t.Fatalf("later success or failed child surfaced: %v", active)
	}
}

func TestSQLiteStoreListActiveTaskStatusesPreferNewestOutcome(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	older := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	finishBlocked := func(taskID, ref string, at time.Time, errorCode, eventType string) {
		t.Helper()
		task, err := store.CreateTask(Task{TaskID: taskID, ProjectRef: ref, Mode: "remote", CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := store.StartAttempt(task.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: ref, Mode: "remote"}, at)
		if err != nil {
			t.Fatal(err)
		}
		if eventType != "" {
			if _, err := store.AppendEvent(attempt.AttemptID, "worker", eventType, "sha256:edited-untracked", at); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.FinishAttempt(attempt.AttemptID, "worker", FinishInput{Status: TaskBlocked, SideEffectState: SideEffectObserved, ErrorCode: errorCode, ErrorSummary: errorCode}, at); err != nil {
			t.Fatal(err)
		}
	}

	finishBlocked("old-probe", "/home/mix", older, "workspace_before_probe_failed", "")
	finishBlocked("new-edit", "/home/mix", newer, "final_workspace_unchanged", "remote_file_activity")
	finishBlocked("old-edit", "/home/fresh", older, "final_workspace_unchanged", "remote_file_activity")
	finishBlocked("new-probe", "/home/fresh", newer, "final_workspace_probe_failed", "")

	live, err := store.CreateTask(Task{TaskID: "live-run", ProjectRef: "/home/live", Mode: "remote", CreatedAt: older})
	if err != nil {
		t.Fatal(err)
	}
	// One minute is already over by the later edit, and long over by the time
	// the list is read. An abandoned running row must not stay in progress.
	if _, err := store.StartAttempt(live.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: "/home/live", Mode: "remote"}, older); err != nil {
		t.Fatal(err)
	}
	finishBlocked("later-edit", "/home/live", newer, "final_workspace_unchanged", "remote_file_activity")

	if _, err := store.CreateTask(Task{TaskID: "stale-queue", ProjectRef: "/home/queue", Mode: "remote", CreatedAt: older}); err != nil {
		t.Fatal(err)
	}
	finishBlocked("queue-later-edit", "/home/queue", newer, "final_workspace_unchanged", "remote_file_activity")

	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active["/home/mix"]; got != string(TaskCompleted) {
		t.Fatalf("newer untracked edit status=%q, want completed", got)
	}
	if got := active["/home/fresh"]; got != string(TaskBlocked) {
		t.Fatalf("newer probe failure status=%q, want blocked", got)
	}
	if got := active["/home/live"]; got != string(TaskCompleted) {
		t.Fatalf("expired running row status=%q, want the later edit", got)
	}
	if got := active["/home/queue"]; got != string(TaskCompleted) {
		t.Fatalf("stale queue status=%q, want completed", got)
	}
}

// leaseStillOpen keeps a historical attempt's lease valid when the task list
// is read against the wall clock. A one-minute lease from a fixed test date
// has already expired, and an expired running row is not still in progress.
func leaseStillOpen(started time.Time) time.Duration {
	open := time.Since(started) + time.Hour
	if open < time.Minute {
		return time.Minute
	}
	return open
}

func TestSQLiteStoreExpiredRunningDoesNotHideLaterFailure(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := `E:\prog-test1`
	policy := PolicySnapshot{ProjectRoot: root, Mode: "local"}
	staleAt := time.Now().UTC().Add(-2 * time.Hour)
	failedAt := staleAt.Add(time.Hour)

	stale, err := store.CreateTask(Task{TaskID: "stale-run", ProjectRef: root, Mode: "local", CreatedAt: staleAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(stale.TaskID, "gui:chat", time.Minute, policy, staleAt); err != nil {
		t.Fatal(err)
	}
	failed, err := store.CreateTask(Task{TaskID: "later-fail", ProjectRef: root, Mode: "local", CreatedAt: failedAt})
	if err != nil {
		t.Fatal(err)
	}
	failedAttempt, err := store.StartAttempt(failed.TaskID, "gui:chat", time.Minute, policy, failedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(failedAttempt.AttemptID, "gui:chat", FinishInput{Status: TaskFailed, SideEffectState: SideEffectObserved, ErrorCode: "executor_failed", ErrorSummary: "模型连续返回空响应，任务中断"}, failedAt); err != nil {
		t.Fatal(err)
	}

	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[root]; got != string(TaskFailed) {
		t.Fatalf("expired running row hid the later failure: %q", got)
	}
	// The later admission records the dead holder at its lease end. A sweep
	// long after that must not move the corpse in front of the failure.
	leaseEnd := staleAt.Add(time.Minute)
	staleTask, err := store.GetTask(stale.TaskID)
	if err != nil || staleTask.Status != TaskInterrupted || !staleTask.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("stale task=%+v err=%v, want interrupted at %s", staleTask, err, leaseEnd)
	}
	if _, err := store.ExpireLeases(time.Now().UTC().Add(48 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	staleTask, err = store.GetTask(stale.TaskID)
	if err != nil || !staleTask.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("sweep moved the corpse to %+v, want %s", staleTask, leaseEnd)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[root]; got != string(TaskFailed) {
		t.Fatalf("late sweep status=%q, want failed", got)
	}

	liveRoot := `E:\still-going`
	livePolicy := PolicySnapshot{ProjectRoot: liveRoot, Mode: "local"}
	liveAt := time.Now().UTC().Add(-time.Minute)
	live, err := store.CreateTask(Task{TaskID: "live-run", ProjectRef: liveRoot, Mode: "local", CreatedAt: liveAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(live.TaskID, "gui:chat", time.Hour, livePolicy, liveAt); err != nil {
		t.Fatal(err)
	}
	later, err := store.CreateTask(Task{TaskID: "later-fail-live", ProjectRef: liveRoot, Mode: "local", CreatedAt: liveAt.Add(30 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	// Read-only so admission allows this turn beside the open write lease.
	laterPolicy := livePolicy
	laterPolicy.ReadOnly = true
	laterAttempt, err := store.StartAttempt(later.TaskID, "gui:chat", time.Minute, laterPolicy, liveAt.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(laterAttempt.AttemptID, "gui:chat", FinishInput{Status: TaskFailed, SideEffectState: SideEffectNone}, liveAt.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[liveRoot]; got != string(TaskRunning) {
		t.Fatalf("open lease status=%q, want running", got)
	}

	abandoned := `E:\abandoned`
	abandonedAt := time.Now().UTC().Add(-3 * time.Hour)
	alone, err := store.CreateTask(Task{TaskID: "alone", ProjectRef: abandoned, Mode: "local", CreatedAt: abandonedAt})
	if err != nil {
		t.Fatal(err)
	}
	aloneAttempt, err := store.StartAttempt(alone.TaskID, "gui:chat", time.Minute, PolicySnapshot{ProjectRoot: abandoned, Mode: "local"}, abandonedAt)
	if err != nil {
		t.Fatal(err)
	}
	// This process is still inside the attempt, so an expired clock stays live.
	markLocalLiveWriter(aloneAttempt.AttemptID)
	t.Cleanup(func() { unmarkLocalLiveWriter(aloneAttempt.AttemptID) })
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[abandoned]; got != string(TaskRunning) {
		t.Fatalf("local executor status=%q, want running", got)
	}
	unmarkLocalLiveWriter(aloneAttempt.AttemptID)
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[abandoned]; got != string(TaskInterrupted) {
		t.Fatalf("expired running row status=%q, want interrupted", got)
	}
	if got := active[root]; got != string(TaskFailed) {
		t.Fatalf("later failure status=%q, want failed", got)
	}
}

func TestSQLiteStoreListRecoveryCandidatesExcludesEndedTasks(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 6, 20, 0, 0, 0, time.UTC)

	interrupt := func(taskID string, at time.Time) {
		t.Helper()
		attempt, err := store.StartAttempt(taskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: "repo"}, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.FinishAttempt(attempt.AttemptID, "worker", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, at); err != nil {
			t.Fatal(err)
		}
	}

	declined, err := store.CreateTask(Task{TaskID: "declined", ProjectRef: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	interrupt(declined.TaskID, now)
	if _, err := store.CancelTask(declined.TaskID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	// A recovered task that later completed must retire its old uncertain
	// attempt as a recovery candidate too.
	recovered, err := store.CreateTask(Task{TaskID: "recovered", ProjectRef: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	interrupt(recovered.TaskID, now)
	if _, err := store.MarkTaskReadyForRecovery(recovered.TaskID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(recovered.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: "repo"}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "worker", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectObserved}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}

	candidates, err := store.ListRecoveryCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("ended tasks still offered as recovery candidates: %v", candidates)
	}
}

func TestSQLiteStoreListActiveTaskStatusesSuppressesStaleInterrupted(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	older := time.Date(2026, 10, 6, 19, 38, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	interruptRun := func(taskID, ref string, at time.Time) {
		t.Helper()
		task, err := store.CreateTask(Task{TaskID: taskID, ProjectRef: ref, Mode: "remote", CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.StartAttempt(task.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: ref, Mode: "remote"}, at); err != nil {
			t.Fatal(err)
		}
		// The production path: a restart marks the abandoned lease interrupted.
		if _, err := store.ExpireLeases(at.Add(2 * time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	finishRun := func(taskID, ref string, at time.Time) {
		t.Helper()
		task, err := store.CreateTask(Task{TaskID: taskID, ProjectRef: ref, Mode: "remote", CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := store.StartAttempt(task.TaskID, "worker", time.Minute, PolicySnapshot{ProjectRoot: ref, Mode: "remote"}, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.FinishAttempt(attempt.AttemptID, "worker", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectObserved}, at); err != nil {
			t.Fatal(err)
		}
	}

	// An interrupted row older than a later completed write must not surface
	// as the ref's active status forever.
	interruptRun("crashed-run", "/home/recovered", older)
	finishRun("later-run", "/home/recovered", newer)
	// The newest interrupted row is still the ref's live recovery reminder.
	finishRun("done-run", "/home/latest-broke", older)
	interruptRun("crashed-later", "/home/latest-broke", newer)

	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := active["/home/recovered"]; ok {
		t.Fatalf("stale interrupted ref surfaced after a later completed run: %v", active)
	}
	if got := active["/home/latest-broke"]; got != string(TaskInterrupted) {
		t.Fatalf("newest interrupted run status=%q, want interrupted", got)
	}
}

func TestSQLiteExpiredRunningWriterDoesNotBlockWithoutLocalExecutor(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 13, 5, 38, 0, 0, time.UTC)
	root := filepath.Join("D:", "shared")
	first, err := store.CreateTask(Task{ProjectRef: root, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateTask(Task{ProjectRef: root, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	policy := PolicySnapshot{ProjectRoot: root, Mode: "local"}
	attempt, err := store.StartAttempt(first.TaskID, "holder", time.Minute, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	markLocalLiveWriter(attempt.AttemptID)
	t.Cleanup(func() { unmarkLocalLiveWriter(attempt.AttemptID) })
	if _, err := store.StartAttempt(second.TaskID, "waiter", time.Minute, policy, now.Add(2*time.Minute)); !errors.Is(err, ErrWriterConflict) {
		t.Fatalf("local executor was admitted beside an expired lease: %v", err)
	}
	held, err := store.GetTask(first.TaskID)
	if err != nil || held.Status != TaskRunning {
		t.Fatalf("local executor was interrupted while still running: %+v %v", held, err)
	}
	unmarkLocalLiveWriter(attempt.AttemptID)
	if _, err := store.StartAttempt(second.TaskID, "waiter", time.Minute, policy, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("dead expired writer still locked the directory: %v", err)
	}
	leaseEnd := now.Add(time.Minute)
	closed, err := store.GetTask(first.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("dead writer task=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
	}
	attempts, err := store.ListAttempts(first.TaskID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != TaskInterrupted || !attempts[0].FinishedAt.Equal(leaseEnd) || !attempts[0].LeaseUntil.IsZero() || attempts[0].SideEffectState != SideEffectUncertain {
		t.Fatalf("dead writer attempt=%+v err=%v", attempts, err)
	}
	events, err := store.ListEvents(attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	foundDeath := false
	for _, event := range events {
		if event.Type == "lease_expired" && event.CreatedAt.Equal(leaseEnd) {
			foundDeath = true
		}
	}
	if !foundDeath {
		t.Fatalf("lease death was not recorded at %s: %+v", leaseEnd, events)
	}
}

func TestSQLiteExpireLeasesSkipsLocalWriterAndStampsLeaseEnd(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 24, 10, 26, 0, 0, time.UTC)
	task, err := store.CreateTask(Task{ProjectRef: `E:\prog-test1`, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "gui:chat", time.Minute, PolicySnapshot{ProjectRoot: `E:\prog-test1`, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	markLocalLiveWriter(attempt.AttemptID)
	t.Cleanup(func() { unmarkLocalLiveWriter(attempt.AttemptID) })
	expired, err := store.ExpireLeases(now.Add(48 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 0 {
		t.Fatalf("local executor was expired: %+v", expired)
	}
	held, err := store.GetTask(task.TaskID)
	if err != nil || held.Status != TaskRunning {
		t.Fatalf("task=%+v err=%v, want running", held, err)
	}
	unmarkLocalLiveWriter(attempt.AttemptID)
	expired, err = store.ExpireLeases(now.Add(48 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	leaseEnd := now.Add(time.Minute)
	if len(expired) != 1 || !expired[0].FinishedAt.Equal(leaseEnd) {
		t.Fatalf("expired=%+v, want finished at %s", expired, leaseEnd)
	}
	closed, err := store.GetTask(task.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("task=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
	}
}

func TestSQLiteExpireLeasesDoesNotOutrankLaterFailure(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := `E:\prog-test1`
	started := time.Date(2026, 10, 24, 10, 26, 0, 0, time.UTC)
	leaseEnd := started.Add(time.Hour)
	policy := PolicySnapshot{ProjectRoot: root, Mode: "local"}
	stale, err := store.CreateTask(Task{TaskID: "stale-run", ProjectRef: root, Mode: "local", CreatedAt: started})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(stale.TaskID, "gui:chat", time.Hour, policy, started); err != nil {
		t.Fatal(err)
	}
	// Admitted while the write lease was still open, so this start must not
	// be what closes the stale row. Read-only does not conflict with it.
	admitAt := started.Add(10 * time.Minute)
	failedAt := leaseEnd.Add(time.Hour)
	failed, err := store.CreateTask(Task{TaskID: "later-fail", ProjectRef: root, Mode: "local", CreatedAt: admitAt})
	if err != nil {
		t.Fatal(err)
	}
	readOnly := policy
	readOnly.ReadOnly = true
	failedAttempt, err := store.StartAttempt(failed.TaskID, "gui:chat", time.Minute, readOnly, admitAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(failedAttempt.AttemptID, "gui:chat", FinishInput{Status: TaskFailed, SideEffectState: SideEffectObserved, ErrorCode: "executor_failed"}, failedAt); err != nil {
		t.Fatal(err)
	}
	still, err := store.GetTask(stale.TaskID)
	if err != nil || still.Status != TaskRunning {
		t.Fatalf("open-lease holder was closed by the later admission: %+v %v", still, err)
	}
	expired, err := store.ExpireLeases(failedAt.Add(48 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || !expired[0].FinishedAt.Equal(leaseEnd) {
		t.Fatalf("expired=%+v, want finished at %s", expired, leaseEnd)
	}
	closed, err := store.GetTask(stale.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("stale task=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
	}
	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[root]; got != string(TaskFailed) {
		t.Fatalf("late sweep status=%q, want failed", got)
	}
}

func TestSQLiteListAndSweepRankDeadLeaseAtItsEnd(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := `E:\prog-test1`
	started := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	leaseEnd := started.Add(time.Hour)
	policy := PolicySnapshot{ProjectRoot: root, Mode: "local"}
	holder, err := store.CreateTask(Task{TaskID: "holder", ProjectRef: root, Mode: "local", CreatedAt: started})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(holder.TaskID, "gui:chat", time.Hour, policy, started); err != nil {
		t.Fatal(err)
	}
	// The failure lands while the write lease is still open, so admission does
	// not close the holder. After the lease ends, both the projection and the
	// sweep must rank the corpse at the lease end, ahead of this failure.
	failedAt := started.Add(20 * time.Minute)
	failed, err := store.CreateTask(Task{TaskID: "mid-fail", ProjectRef: root, Mode: "local", CreatedAt: failedAt})
	if err != nil {
		t.Fatal(err)
	}
	readOnly := policy
	readOnly.ReadOnly = true
	failedAttempt, err := store.StartAttempt(failed.TaskID, "gui:chat", time.Minute, readOnly, failedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(failedAttempt.AttemptID, "gui:chat", FinishInput{Status: TaskFailed, SideEffectState: SideEffectNone}, failedAt); err != nil {
		t.Fatal(err)
	}
	openList, stale, err := store.ReadTaskList(started.Add(30 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got := openList[root]; got != string(TaskRunning) || stale {
		t.Fatalf("open lease status=%q stale=%v, want running and not stale", got, stale)
	}
	deadList, stale, err := store.ReadTaskList(leaseEnd.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := deadList[root]; got != string(TaskInterrupted) || !stale {
		t.Fatalf("dead lease status=%q stale=%v, want interrupted and stale", got, stale)
	}
	unread, err := store.GetTask(holder.TaskID)
	if err != nil || unread.Status != TaskRunning {
		t.Fatalf("read wrote the holder: %+v %v", unread, err)
	}
	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[root]; got != string(TaskInterrupted) {
		t.Fatalf("before sweep status=%q, want interrupted", got)
	}
	still, err := store.GetTask(holder.TaskID)
	if err != nil || still.Status != TaskRunning {
		t.Fatalf("projection wrote the holder: %+v %v", still, err)
	}
	if _, err := store.ExpireLeases(leaseEnd.Add(48 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	closed, err := store.GetTask(holder.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("holder=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
	}
	active, err = store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[root]; got != string(TaskInterrupted) {
		t.Fatalf("after sweep status=%q, want interrupted", got)
	}
}

func TestSQLiteReadTaskListRanksWaitingParentAtDeadChildLeaseEnd(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := "repo"
	started := time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	leaseEnd := started.Add(time.Minute)
	policy := PolicySnapshot{ProjectRoot: root, Mode: "local"}
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: root, Mode: "local", CreatedAt: started})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, policy, started)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return started }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", ChildTaskSpec{Name: "inspect", RequestedWork: "inspect", ProjectRef: root, Mode: "local"}, PolicySnapshot{ProjectRoot: root, Mode: "local", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(handle.TaskID, "child", time.Minute, PolicySnapshot{ProjectRoot: root, Mode: "local", ReadOnly: true}, started); err != nil {
		t.Fatal(err)
	}
	openList, stale, err := store.ReadTaskList(started.Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got := openList[root]; got != string(TaskRunning) || stale {
		t.Fatalf("live child status=%q stale=%v, want running and not stale", got, stale)
	}
	// The failure lands while the child lease is still open. After that lease
	// ends, the waiting parent is the corpse and outranks this failure.
	failedAt := started.Add(20 * time.Second)
	failed, err := store.CreateTask(Task{TaskID: "mid-fail", ProjectRef: root, Mode: "local", CreatedAt: failedAt})
	if err != nil {
		t.Fatal(err)
	}
	readOnly := policy
	readOnly.ReadOnly = true
	failedAttempt, err := store.StartAttempt(failed.TaskID, "gui:chat", time.Minute, readOnly, failedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(failedAttempt.AttemptID, "gui:chat", FinishInput{Status: TaskFailed, SideEffectState: SideEffectNone}, failedAt); err != nil {
		t.Fatal(err)
	}
	deadList, stale, err := store.ReadTaskList(leaseEnd.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := deadList[root]; got != string(TaskInterrupted) || !stale {
		t.Fatalf("dead child status=%q stale=%v, want interrupted and stale", got, stale)
	}
	waiting, err := store.GetTask(parent.TaskID)
	if err != nil || waiting.Status != TaskWaitingChild {
		t.Fatalf("read wrote the parent: %+v %v", waiting, err)
	}
	if _, err := store.ExpireLeases(leaseEnd.Add(48 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	closed, err := store.GetTask(parent.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("parent=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
	}
	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[root]; got != string(TaskInterrupted) {
		t.Fatalf("after sweep status=%q, want interrupted", got)
	}
}

func TestSQLiteEarlierChildDeathOutranksLaterParentStamp(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := "repo"
	started := time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	earlyEnd := started.Add(time.Minute)
	laterEnd := started.Add(2 * time.Minute)
	readOnly := PolicySnapshot{ProjectRoot: root, Mode: "local", ReadOnly: true}
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: root, Mode: "local", CreatedAt: started})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: root, Mode: "local"}, started)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return started }}
	handles, err := service.AdmitReadOnlyChildren(parentAttempt.AttemptID, "parent", []ChildTaskSpec{
		{Name: "early", RequestedWork: "inspect early", ProjectRef: root, Mode: "local"},
		{Name: "later", RequestedWork: "inspect later", ProjectRef: root, Mode: "local"},
	}, readOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(handles[0].TaskID, "child", time.Minute, readOnly, started); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(handles[1].TaskID, "child", 2*time.Minute, readOnly, started); err != nil {
		t.Fatal(err)
	}
	// The failure is recorded while both leases are open, and it finishes
	// between the two lease ends.
	failedAt := started.Add(90 * time.Second)
	failed, err := store.CreateTask(Task{TaskID: "mid-fail", ProjectRef: root, Mode: "local", CreatedAt: started.Add(30 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	failedAttempt, err := store.StartAttempt(failed.TaskID, "gui:chat", time.Minute, readOnly, started.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(failedAttempt.AttemptID, "gui:chat", FinishInput{Status: TaskFailed, SideEffectState: SideEffectNone}, failedAt); err != nil {
		t.Fatal(err)
	}
	// Interrupting the later child first stamps the parent at the later lease
	// end and leaves the earlier child running.
	if _, err := store.StartAttempt(handles[1].TaskID, "again", time.Minute, readOnly, started.Add(3*time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("later child error = %v, want ErrInvalidTransition", err)
	}
	stamped, err := store.GetTask(parent.TaskID)
	if err != nil || stamped.Status != TaskInterrupted || !stamped.UpdatedAt.Equal(laterEnd) {
		t.Fatalf("parent=%+v err=%v, want interrupted at %s", stamped, err, laterEnd)
	}
	listed, stale, err := store.ReadTaskList(started.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got := listed[root]; got != string(TaskFailed) || !stale {
		t.Fatalf("before sweep status=%q stale=%v, want failed and stale", got, stale)
	}
	if _, err := store.ExpireLeases(started.Add(51 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	moved, err := store.GetTask(parent.TaskID)
	if err != nil || moved.Status != TaskInterrupted || !moved.UpdatedAt.Equal(earlyEnd) {
		t.Fatalf("parent=%+v err=%v, want interrupted at %s", moved, err, earlyEnd)
	}
	active, err := store.ListActiveTaskStatusesByProjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := active[root]; got != string(TaskFailed) {
		t.Fatalf("after sweep status=%q, want failed", got)
	}
}

func TestSQLiteDeadChildClosesQueuedSiblings(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := "repo"
	started := time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	earlyEnd := started.Add(time.Minute)
	readOnly := PolicySnapshot{ProjectRoot: root, Mode: "local", ReadOnly: true}
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: root, Mode: "local", CreatedAt: started})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: root, Mode: "local"}, started)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return started }}
	handles, err := service.AdmitReadOnlyChildren(parentAttempt.AttemptID, "parent", []ChildTaskSpec{
		{Name: "dead", RequestedWork: "inspect dead", ProjectRef: root, Mode: "local"},
		{Name: "live", RequestedWork: "inspect live", ProjectRef: root, Mode: "local"},
		{Name: "queued", RequestedWork: "inspect queued", ProjectRef: root, Mode: "local"},
	}, readOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(handles[0].TaskID, "child", time.Minute, readOnly, started); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(handles[1].TaskID, "child", 2*time.Minute, readOnly, started); err != nil {
		t.Fatal(err)
	}
	listed, stale, err := store.ReadTaskList(earlyEnd)
	if err != nil {
		t.Fatal(err)
	}
	if got := listed[root]; got != string(TaskRunning) || !stale {
		t.Fatalf("before sweep status=%q stale=%v, want running and stale", got, stale)
	}
	if _, err := store.ExpireLeases(earlyEnd); err != nil {
		t.Fatal(err)
	}
	parentTask, err := store.GetTask(parent.TaskID)
	if err != nil || parentTask.Status != TaskInterrupted || !parentTask.UpdatedAt.Equal(earlyEnd) {
		t.Fatalf("parent=%+v err=%v", parentTask, err)
	}
	dead, err := store.GetTask(handles[0].TaskID)
	if err != nil || dead.Status != TaskInterrupted {
		t.Fatalf("dead child=%+v err=%v", dead, err)
	}
	live, err := store.GetTask(handles[1].TaskID)
	if err != nil || live.Status != TaskRunning {
		t.Fatalf("live child=%+v err=%v", live, err)
	}
	queued, err := store.GetTask(handles[2].TaskID)
	if err != nil || queued.Status != TaskInterrupted || !queued.UpdatedAt.Equal(earlyEnd) {
		t.Fatalf("queued child=%+v err=%v, want interrupted at %s", queued, err, earlyEnd)
	}
	if _, err := store.StartAttempt(handles[2].TaskID, "late", time.Minute, readOnly, earlyEnd); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("queued sibling error = %v, want ErrInvalidTransition", err)
	}
	listed, stale, err = store.ReadTaskList(earlyEnd)
	if err != nil {
		t.Fatal(err)
	}
	if got := listed[root]; got != string(TaskRunning) || stale {
		t.Fatalf("after sweep status=%q stale=%v, want running while the sibling lease is open", got, stale)
	}
}
