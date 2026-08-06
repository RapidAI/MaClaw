package im

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskQueue_EnqueueDequeue(t *testing.T) {
	q := newUserQueue(3)

	task1 := &IMTask{ID: 1, Text: "hello"}
	pos, ok := q.enqueue(task1)
	if !ok {
		t.Fatal("enqueue should succeed")
	}
	if pos != 1 {
		t.Fatalf("expected position 1, got %d", pos)
	}

	task2 := &IMTask{ID: 2, Text: "world"}
	pos, ok = q.enqueue(task2)
	if !ok {
		t.Fatal("enqueue should succeed")
	}
	if pos != 2 {
		t.Fatalf("expected position 2, got %d", pos)
	}

	got := q.dequeue()
	if got == nil || got.ID != 1 {
		t.Fatalf("expected task 1, got %v", got)
	}
	got = q.dequeue()
	if got == nil || got.ID != 2 {
		t.Fatalf("expected task 2, got %v", got)
	}
	got = q.dequeue()
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestTaskQueue_CapacityLimit(t *testing.T) {
	q := newUserQueue(2)

	q.enqueue(&IMTask{ID: 1})
	q.enqueue(&IMTask{ID: 2})
	_, ok := q.enqueue(&IMTask{ID: 3})
	if ok {
		t.Fatal("enqueue should fail when at capacity")
	}
}

func TestTaskDispatcher_BasicFlow(t *testing.T) {
	var executed atomic.Int32
	var delivered sync.Map

	executor := func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
		executed.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate work
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "ok",
			Title:      "Done",
			Body:       fmt.Sprintf("Result for: %s", task.Text),
		}, nil
	}

	delivery := func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse) {
		delivered.Store(resp.Body, true)
	}

	d := NewIMTaskDispatcher(3, executor, delivery)
	defer d.Shutdown()

	// Enqueue first task — should start immediately.
	resp1 := d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "task1"})
	if resp1.StatusCode != 202 {
		t.Fatalf("expected 202, got %d", resp1.StatusCode)
	}
	if resp1.StatusIcon != "busy" {
		t.Fatalf("first task should show processing icon, got %q", resp1.StatusIcon)
	}

	// Give worker time to pick up the task.
	time.Sleep(20 * time.Millisecond)

	// Enqueue second task — should be queued.
	resp2 := d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "task2"})
	if resp2.StatusCode != 202 {
		t.Fatalf("expected 202, got %d", resp2.StatusCode)
	}
	if resp2.StatusIcon != "info" {
		t.Fatalf("second task should show queue icon, got %q", resp2.StatusIcon)
	}

	// Wait for both tasks to complete.
	time.Sleep(300 * time.Millisecond)

	if executed.Load() != 2 {
		t.Fatalf("expected 2 tasks executed, got %d", executed.Load())
	}

	if _, ok := delivered.Load("Result for: task1"); !ok {
		t.Fatal("task1 result not delivered")
	}
	if _, ok := delivered.Load("Result for: task2"); !ok {
		t.Fatal("task2 result not delivered")
	}
}

func TestTaskDispatcherPreservesReplyTarget(t *testing.T) {
	delivered := make(chan string, 1)
	d := NewIMTaskDispatcher(1,
		func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
			return &GenericResponse{StatusCode: 200, Body: "done"}, nil
		},
		func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse) {
			delivered <- replyTarget
		},
	)
	defer d.Shutdown()
	d.Enqueue(&IMTask{UserID: "user-1", PlatformName: "lansenger", PlatformUID: "user-1", ReplyTarget: "group-1", Text: "hello"})
	select {
	case got := <-delivered:
		if got != "group-1" {
			t.Fatalf("reply target = %q, want group-1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task delivery")
	}
}

func TestTaskDispatcher_QueueFull(t *testing.T) {
	executor := func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
		time.Sleep(200 * time.Millisecond)
		return &GenericResponse{StatusCode: 200, Body: "done"}, nil
	}
	delivery := func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse) {
	}

	d := NewIMTaskDispatcher(2, executor, delivery)
	defer d.Shutdown()

	// Fill the queue.
	d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "t1"})
	time.Sleep(20 * time.Millisecond) // let worker pick up t1
	d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "t2"})
	d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "t3"})

	// Fourth task should be rejected.
	resp := d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "t4"})
	if resp.StatusCode != 429 {
		t.Fatalf("expected 429 (queue full), got %d", resp.StatusCode)
	}
}

func TestTaskDispatcher_PerUserIsolation(t *testing.T) {
	var u1Count, u2Count atomic.Int32

	executor := func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
		if task.UserID == "u1" {
			u1Count.Add(1)
		} else {
			u2Count.Add(1)
		}
		time.Sleep(30 * time.Millisecond)
		return &GenericResponse{StatusCode: 200, Body: "ok"}, nil
	}
	delivery := func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse) {
	}

	d := NewIMTaskDispatcher(3, executor, delivery)
	defer d.Shutdown()

	// Both users enqueue tasks — they should run in parallel (separate workers).
	d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "a"})
	d.Enqueue(&IMTask{UserID: "u2", PlatformName: "test", PlatformUID: "p2", Text: "b"})

	time.Sleep(200 * time.Millisecond)

	if u1Count.Load() != 1 {
		t.Fatalf("u1 expected 1 task, got %d", u1Count.Load())
	}
	if u2Count.Load() != 1 {
		t.Fatalf("u2 expected 1 task, got %d", u2Count.Load())
	}
}

func TestTaskDispatcher_PerTenantIsolation(t *testing.T) {
	var maxConcurrent atomic.Int32
	var running atomic.Int32
	release := make(chan struct{})

	executor := func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
		current := running.Add(1)
		for {
			max := maxConcurrent.Load()
			if current <= max || maxConcurrent.CompareAndSwap(max, current) {
				break
			}
		}
		<-release
		running.Add(-1)
		return &GenericResponse{StatusCode: 200, Body: task.TenantID}, nil
	}
	delivery := func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse) {
	}

	d := NewIMTaskDispatcher(3, executor, delivery)
	defer d.Shutdown()

	d.Enqueue(&IMTask{TenantID: "tenant_a", UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "a"})
	d.Enqueue(&IMTask{TenantID: "tenant_b", UserID: "u1", PlatformName: "test", PlatformUID: "p2", Text: "b"})

	time.Sleep(50 * time.Millisecond)
	if maxConcurrent.Load() < 2 {
		t.Fatalf("expected same userID in different tenants to use separate workers, max concurrent=%d", maxConcurrent.Load())
	}
	close(release)
}

func TestTaskDispatcher_HardwareDispatchKeysAreIndependentAndFIFO(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	completed := make(chan string, 3)

	d := NewIMTaskDispatcher(3,
		func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
			started <- task.Text
			<-release
			completed <- task.Text
			return &GenericResponse{StatusCode: 200, Body: task.Text}, nil
		},
		func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse) {
		},
	)
	defer d.Shutdown()

	// Same owner, distinct ESP32 lanes: a1 and b2 may execute together.
	d.Enqueue(&IMTask{TenantID: "tenant-a", UserID: "owner", DispatchKey: "hardware:a1", Text: "a1-first"})
	d.Enqueue(&IMTask{TenantID: "tenant-a", UserID: "owner", DispatchKey: "hardware:a1", Text: "a1-second"})
	d.Enqueue(&IMTask{TenantID: "tenant-a", UserID: "owner", DispatchKey: "hardware:b2", Text: "b2-first"})

	seen := map[string]bool{}
	deadline := time.After(time.Second)
	for len(seen) < 2 {
		select {
		case text := <-started:
			seen[text] = true
		case <-deadline:
			t.Fatalf("expected independent lanes to start a1-first and b2-first, got %#v", seen)
		}
	}
	if !seen["a1-first"] || !seen["b2-first"] || seen["a1-second"] {
		t.Fatalf("unexpected first tasks for hardware lanes: %#v", seen)
	}

	close(release)
	order := make([]string, 0, 3)
	for len(order) < 3 {
		select {
		case text := <-completed:
			order = append(order, text)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for queued hardware tasks; completed=%v", order)
		}
	}
	firstA, secondA := -1, -1
	for i, text := range order {
		if text == "a1-first" {
			firstA = i
		}
		if text == "a1-second" {
			secondA = i
		}
	}
	if firstA == -1 || secondA == -1 || firstA >= secondA {
		t.Fatalf("same hardware lane did not stay FIFO: %v", order)
	}
}

func TestTaskDispatcher_Stats(t *testing.T) {
	executor := func(ctx context.Context, task *IMTask) (*GenericResponse, error) {
		time.Sleep(100 * time.Millisecond)
		return &GenericResponse{StatusCode: 200, Body: "ok"}, nil
	}
	delivery := func(ctx context.Context, userID, platformName, platformUID, replyTarget string, resp *GenericResponse) {
	}

	d := NewIMTaskDispatcher(5, executor, delivery)
	defer d.Shutdown()

	// No queue yet.
	stats := d.Stats("u1")
	if stats.Running || stats.Pending != 0 {
		t.Fatalf("expected idle stats, got %+v", stats)
	}

	d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "t1"})
	time.Sleep(20 * time.Millisecond) // let worker pick up
	d.Enqueue(&IMTask{UserID: "u1", PlatformName: "test", PlatformUID: "p1", Text: "t2"})

	stats = d.Stats("u1")
	if !stats.Running {
		t.Fatal("expected running=true")
	}
	if stats.Pending != 1 {
		t.Fatalf("expected 1 pending, got %d", stats.Pending)
	}
}
