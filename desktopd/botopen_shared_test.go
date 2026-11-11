package desktopd

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// A sibling bot's long call holds the shared side of the container gate. A
// bot's session open must not wait for a gap that never comes: it opens the
// bot's own screen on the shared side once the container is running.
func TestBotSessionOpensWhileASiblingHoldsTheGate(t *testing.T) {
	old := botOpenExclusiveWait
	botOpenExclusiveWait = 50 * time.Millisecond
	defer func() { botOpenExclusiveWait = old }()
	var mu sync.Mutex
	var calls []string
	base := fakeBotDocker(&calls)
	run := func(ctx context.Context, args ...string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) > 2 && args[0] == "inspect" && args[2] == "{{.State.Running}}" {
			calls = append(calls, strings.Join(args, " "))
			return "true", nil
		}
		return base(ctx, args...)
	}
	svc := &Service{AdvertiseHost: "docker.example", Run: run}
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = svc.withUserRead("tenant", "alice", func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)
	done := make(chan error, 1)
	var session Session
	go func() {
		var err error
		session, err = svc.OpenSession(context.Background(), Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1", Image: "maclaw-gui:2", Memory: "3g", CPUs: "1.5", ShmSize: "1g"})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("open: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bot open waited for the sibling's call")
	}
	botKey, _ := desktop.BotKey("tenant", "alice", "bot_1")
	mu.Lock()
	joined := strings.Join(calls, "\n")
	mu.Unlock()
	if session.Display != ":23" || !strings.Contains(joined, "python3 /desktop_supervisor.py ensure "+botKey) {
		t.Fatalf("session %#v calls:\n%s", session, joined)
	}
}

// With the gate free, the open keeps the exclusive path (create/start).
func TestBotSessionTakesTheExclusivePathWhenFree(t *testing.T) {
	var calls []string
	svc := &Service{AdvertiseHost: "docker.example", Run: fakeBotDocker(&calls)}
	if _, err := svc.OpenSession(context.Background(), Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1", Image: "maclaw-gui:2", Memory: "3g", CPUs: "1.5", ShmSize: "1g"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "inspect --format {{.State.Running}} ") {
			t.Fatalf("free gate used the shared path: %v", calls)
		}
	}
}

// A file write of one bot does not wait for a sibling's long call to end.
func TestFileWriteDoesNotWaitForASiblingsCall(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	base := fakeBotDocker(&calls)
	svc := &Service{AdvertiseHost: "docker.example", Run: func(ctx context.Context, args ...string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return base(ctx, args...)
	}}
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = svc.withUserRead("tenant", "alice", func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)
	done := make(chan error, 1)
	go func() {
		_, err := svc.WriteFile(context.Background(), "tenant", "alice", "/home/desktop/a.txt", "hi")
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("file write waited for the sibling's call")
	}
}
