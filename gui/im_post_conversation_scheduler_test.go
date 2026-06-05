package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestPostConversationSchedulerCancelIsOwnerScoped(t *testing.T) {
	s := newPostConversationScheduler(&IMMessageHandler{})
	started := make(chan string, 2)
	ownerADone := make(chan struct{})
	ownerBDone := make(chan struct{})

	s.runTask = func(ctx context.Context, task postConversationTask) {
		started <- task.UserID
		switch task.UserID {
		case "owner-a":
			<-ctx.Done()
			close(ownerADone)
		case "owner-b":
			select {
			case <-ctx.Done():
				t.Fatal("owner-b was canceled by owner-a")
			case <-time.After(40 * time.Millisecond):
				close(ownerBDone)
			}
		}
	}

	s.Enqueue(postConversationTask{UserID: "owner-a", History: []agent.ConversationEntry{{Role: "user", Content: "a"}}})
	s.Enqueue(postConversationTask{UserID: "owner-b", History: []agent.ConversationEntry{{Role: "user", Content: "b"}}})
	waitForPostSchedulerStarts(t, started, map[string]bool{"owner-a": true, "owner-b": true})

	if !s.CancelOwner("owner-a", "test") {
		t.Fatal("expected owner-a cancel to hit active task")
	}

	select {
	case <-ownerADone:
	case <-time.After(time.Second):
		t.Fatal("owner-a task was not canceled")
	}
	select {
	case <-ownerBDone:
	case <-time.After(time.Second):
		t.Fatal("owner-b task did not finish independently")
	}
}

func TestPostConversationSchedulerSerializesSameOwner(t *testing.T) {
	s := newPostConversationScheduler(&IMMessageHandler{})
	var mu sync.Mutex
	active := 0
	maxActive := 0
	started := make(chan struct{}, 1)
	done := make(chan struct{}, 2)

	s.runTask = func(ctx context.Context, task postConversationTask) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}

		select {
		case <-ctx.Done():
		case <-time.After(20 * time.Millisecond):
		}

		mu.Lock()
		active--
		mu.Unlock()
		done <- struct{}{}
	}

	s.Enqueue(postConversationTask{UserID: "owner-a"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first same-owner task did not start")
	}
	s.Enqueue(postConversationTask{UserID: "owner-a"})

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for same-owner tasks")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if maxActive != 1 {
		t.Fatalf("same owner ran concurrently: maxActive=%d", maxActive)
	}
}

func TestPostConversationSchedulerCancelDropsPendingForOwner(t *testing.T) {
	s := newPostConversationScheduler(&IMMessageHandler{})
	started := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan string, 2)

	s.runTask = func(ctx context.Context, task postConversationTask) {
		started <- task.UserID
		if task.UserID == "owner-a" {
			<-release
		}
		done <- task.UserID
	}

	s.Enqueue(postConversationTask{UserID: "owner-a", History: []agent.ConversationEntry{{Role: "user", Content: "active"}}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active owner-a task did not start")
	}
	s.Enqueue(postConversationTask{UserID: "owner-a", History: []agent.ConversationEntry{{Role: "user", Content: "pending"}}})

	if !s.CancelOwner("owner-a", "foreground") {
		t.Fatal("expected cancel to drop pending owner-a task")
	}
	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active owner-a task did not finish")
	}
	select {
	case got := <-started:
		t.Fatalf("pending task started after cancel: %s", got)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestForegroundStartCancelsOnlySameOwnerPostConversation(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	h := &IMMessageHandler{}
	app := &App{imHandler: h}
	h.app = app
	s := newPostConversationScheduler(h)
	h.postConversationScheduler = s

	started := make(chan string, 2)
	ownerADone := make(chan struct{})
	ownerBDone := make(chan struct{})
	s.runTask = func(ctx context.Context, task postConversationTask) {
		started <- task.UserID
		switch task.UserID {
		case "owner-a":
			<-ctx.Done()
			close(ownerADone)
		case "owner-b":
			select {
			case <-ctx.Done():
				t.Fatal("owner-b was canceled by foreground start for owner-a")
			case <-time.After(80 * time.Millisecond):
				close(ownerBDone)
			}
		}
	}

	s.Enqueue(postConversationTask{UserID: "owner-a"})
	s.Enqueue(postConversationTask{UserID: "owner-b"})
	waitForPostSchedulerStarts(t, started, map[string]bool{"owner-a": true, "owner-b": true})

	cleanup := app.beginForegroundAgentLoop("owner-a", "req-a", "chat")
	defer cleanup()

	select {
	case <-ownerADone:
	case <-time.After(time.Second):
		t.Fatal("owner-a post conversation was not canceled")
	}
	select {
	case <-ownerBDone:
	case <-time.After(time.Second):
		t.Fatal("owner-b post conversation did not finish independently")
	}
}

func waitForPostSchedulerStarts(t *testing.T, started <-chan string, want map[string]bool) {
	t.Helper()
	deadline := time.After(time.Second)
	for len(want) > 0 {
		select {
		case got := <-started:
			delete(want, got)
		case <-deadline:
			t.Fatalf("timed out waiting for starts: remaining=%v", want)
		}
	}
}
