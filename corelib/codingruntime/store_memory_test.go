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
