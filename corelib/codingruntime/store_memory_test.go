package codingruntime

import (
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreLeasePreventsConcurrentWriters(t *testing.T) {
	store := NewMemoryStore()
	task, err := store.CreateTask(Task{ProjectRef: "project-a"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	first, err := store.StartAttempt(task.TaskID, "worker-a", time.Minute, PolicySnapshot{ProjectRoot: "project-a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAttempt(task.TaskID, "worker-b", time.Minute, PolicySnapshot{}, now); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second writer error = %v, want ErrLeaseHeld", err)
	}
	if _, err := store.FinishAttempt(first.AttemptID, "worker-b", FinishInput{Status: TaskFailed}, now); !errors.Is(err, ErrLeaseOwnerMismatch) {
		t.Fatalf("stale owner error = %v, want ErrLeaseOwnerMismatch", err)
	}
}

func TestMemorySameTaskExpiredLeaseIsRecordedAndRefused(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 10, 24, 10, 26, 0, 0, time.UTC)
	task, err := store.CreateTask(Task{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "holder", time.Minute, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	markLocalLiveWriter(attempt.AttemptID)
	t.Cleanup(func() { unmarkLocalLiveWriter(attempt.AttemptID) })
	if _, err := store.StartAttempt(task.TaskID, "again", time.Minute, PolicySnapshot{}, now.Add(2*time.Minute)); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("local executor error = %v, want ErrLeaseHeld", err)
	}
	held, err := store.GetTask(task.TaskID)
	if err != nil || held.Status != TaskRunning {
		t.Fatalf("local executor was interrupted: %+v %v", held, err)
	}
	unmarkLocalLiveWriter(attempt.AttemptID)
	if _, err := store.StartAttempt(task.TaskID, "again", time.Minute, PolicySnapshot{}, now.Add(2*time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("dead same-task lease error = %v, want ErrInvalidTransition", err)
	}
	leaseEnd := now.Add(time.Minute)
	closed, err := store.GetTask(task.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("task=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
	}
}

func TestMemoryStoreExpiredLeaseRequiresRecovery(t *testing.T) {
	store := NewMemoryStore()
	task, err := store.CreateTask(Task{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	attempt, err := store.StartAttempt(task.TaskID, "worker", time.Minute, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "worker", "tool_started", "digest", now); err != nil {
		t.Fatal(err)
	}
	expired, err := store.ExpireLeases(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].Status != TaskInterrupted || expired[0].SideEffectState != SideEffectUncertain {
		t.Fatalf("expired=%+v", expired)
	}
	candidates, err := store.ListRecoveryCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].AttemptID != attempt.AttemptID {
		t.Fatalf("candidates=%+v", candidates)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "worker", FinishInput{Status: TaskCompleted}, now.Add(time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("finish interrupted error = %v, want ErrInvalidTransition", err)
	}
}

func TestMemoryExpireLeasesSkipsLocalWriterAndStampsLeaseEnd(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 10, 24, 10, 26, 0, 0, time.UTC)
	task, err := store.CreateTask(Task{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "worker", time.Minute, PolicySnapshot{}, now)
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
	if len(expired) != 1 || expired[0].Status != TaskInterrupted || !expired[0].FinishedAt.Equal(leaseEnd) {
		t.Fatalf("expired=%+v, want finished at %s", expired, leaseEnd)
	}
	closed, err := store.GetTask(task.TaskID)
	if err != nil || closed.Status != TaskInterrupted || !closed.UpdatedAt.Equal(leaseEnd) {
		t.Fatalf("task=%+v err=%v, want interrupted at %s", closed, err, leaseEnd)
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

func TestMemoryStoreFinishAndEventsAreLeaseOwned(t *testing.T) {
	store := NewMemoryStore()
	task, err := store.CreateTask(Task{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Hour, PolicySnapshot{}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.AppendEvent(attempt.AttemptID, "owner", "probe", "a", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AppendEvent(attempt.AttemptID, "owner", "tool", "b", now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequence = %d, %d", first.Sequence, second.Sequence)
	}
	finished, err := store.FinishAttempt(attempt.AttemptID, "owner", FinishInput{Status: TaskCompleted, SideEffectState: SideEffectConfirmed}, now)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != TaskCompleted || finished.SideEffectState != SideEffectConfirmed {
		t.Fatalf("finished=%+v", finished)
	}
	current, err := store.GetTask(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != TaskCompleted {
		t.Fatalf("task status=%s", current.Status)
	}
}

func TestMemoryStoreRecordsLateCallbackWithoutChangingTerminalAttempt(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStoreListAttemptsOrdersByAttemptNumber(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryEarlierChildDeathPullsParentStampBack(t *testing.T) {
	store := NewMemoryStore()
	started := time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	earlyEnd := started.Add(time.Minute)
	laterEnd := started.Add(2 * time.Minute)
	readOnly := PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, started)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return started }}
	handles, err := service.AdmitReadOnlyChildren(parentAttempt.AttemptID, "parent", []ChildTaskSpec{
		{Name: "early", RequestedWork: "inspect early", ProjectRef: "repo", Mode: "local"},
		{Name: "later", RequestedWork: "inspect later", ProjectRef: "repo", Mode: "local"},
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
	if _, err := store.StartAttempt(handles[1].TaskID, "again", time.Minute, readOnly, started.Add(3*time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("later child error = %v, want ErrInvalidTransition", err)
	}
	stamped, err := store.GetTask(parent.TaskID)
	if err != nil || stamped.Status != TaskInterrupted || !stamped.UpdatedAt.Equal(laterEnd) {
		t.Fatalf("parent=%+v err=%v, want interrupted at %s", stamped, err, laterEnd)
	}
	if _, err := store.ExpireLeases(started.Add(3 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	moved, err := store.GetTask(parent.TaskID)
	if err != nil || moved.Status != TaskInterrupted || !moved.UpdatedAt.Equal(earlyEnd) {
		t.Fatalf("parent=%+v err=%v, want interrupted at %s", moved, err, earlyEnd)
	}
}

func TestMemoryDeadChildClosesQueuedSiblings(t *testing.T) {
	store := NewMemoryStore()
	started := time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	earlyEnd := started.Add(time.Minute)
	readOnly := PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}
	parent, err := store.CreateTask(Task{TaskID: "parent", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, started)
	if err != nil {
		t.Fatal(err)
	}
	service := ChildTaskService{Store: store, Now: func() time.Time { return started }}
	handles, err := service.AdmitReadOnlyChildren(parentAttempt.AttemptID, "parent", []ChildTaskSpec{
		{Name: "dead", RequestedWork: "inspect dead", ProjectRef: "repo", Mode: "local"},
		{Name: "live", RequestedWork: "inspect live", ProjectRef: "repo", Mode: "local"},
		{Name: "queued", RequestedWork: "inspect queued", ProjectRef: "repo", Mode: "local"},
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
}
