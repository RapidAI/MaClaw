package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTriggerNowRejectsDuplicateManualRun(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	manager.SetExecutor(func(ctx context.Context, _ *ScheduledTask) (string, error) {
		started <- struct{}{}
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	id, err := manager.Add(ScheduledTask{
		Name:       "daily report",
		Action:     "send report",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := manager.TriggerNow(id); err != nil {
		t.Fatalf("first TriggerNow() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first manual run did not start")
	}
	if err := manager.TriggerNow(id); err == nil {
		t.Fatal("second TriggerNow() error = nil, want already running error")
	}
	close(release)
	// Wait for the async execution to finish writing its state before TempDir
	// cleanup removes the scheduler's persistence directory on Windows.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task := manager.Get(id)
		if task != nil && task.RunCount == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("manual run did not complete")
}

func TestScheduledRunSkipsTaskClaimedByManualRun(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	manager.SetExecutor(func(ctx context.Context, _ *ScheduledTask) (string, error) {
		started <- struct{}{}
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	id, err := manager.Add(ScheduledTask{
		Name:       "due report",
		Action:     "send report",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := manager.TriggerNow(id); err != nil {
		t.Fatalf("TriggerNow() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual run did not start")
	}

	manager.mu.RLock()
	executor := manager.executor
	manager.mu.RUnlock()
	go manager.fireByID(id, executor)
	time.Sleep(30 * time.Millisecond)
	select {
	case <-started:
		t.Fatal("scheduled path started a duplicate task while manual run was active")
	default:
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task := manager.Get(id)
		if task != nil && task.RunCount == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("manual run did not complete")
}

func TestDeleteRunningTaskCancelsBeforeRemoval(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)

	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	manager.SetExecutor(func(ctx context.Context, _ *ScheduledTask) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return "", ctx.Err()
	})
	id, err := manager.Add(ScheduledTask{
		Name:       "running report",
		Action:     "send report",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := manager.TriggerNow(id); err != nil {
		t.Fatalf("TriggerNow() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual run did not start")
	}
	if err := manager.Delete(id); err == nil {
		t.Fatal("Delete() error = nil, want running-task cancellation error")
	}
	if task := manager.Get(id); task == nil {
		t.Fatal("Delete() removed a task while its executor was still running")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Delete() did not cancel the running task")
	}
}

func TestStopKeepsExecutionLeaseUntilRunFinishes(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	manager.SetExecutor(func(ctx context.Context, _ *ScheduledTask) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return "", ctx.Err()
	})
	id, err := manager.Add(ScheduledTask{
		Name:       "stoppable report",
		Action:     "send report",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := manager.TriggerNow(id); err != nil {
		t.Fatalf("TriggerNow() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual run did not start")
	}
	manager.Stop()
	if err := manager.TriggerNow(id); err == nil {
		t.Fatal("TriggerNow() after Stop while run unwinds = nil, want already running error")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not cancel the running task")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		manager.mu.RLock()
		_, running := manager.runningTasks[id]
		manager.mu.RUnlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("execution lease was not released after cancelled run finished")
}

func TestPauseRunningTaskCancelsAndKeepsTaskPaused(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)

	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	manager.SetExecutor(func(ctx context.Context, _ *ScheduledTask) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return "", ctx.Err()
	})
	id, err := manager.Add(ScheduledTask{
		Name:       "pausable report",
		Action:     "send report",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := manager.TriggerNow(id); err != nil {
		t.Fatalf("TriggerNow() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual run did not start")
	}
	if err := manager.Pause(id); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Pause() did not cancel the running task")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task := manager.Get(id)
		if task != nil && task.RunCount == 1 {
			if task.Status != "paused" || task.NextRunAt != nil {
				t.Fatalf("task after paused execution = %#v, want paused with no next run", task)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("cancelled run did not persist completion state")
}
