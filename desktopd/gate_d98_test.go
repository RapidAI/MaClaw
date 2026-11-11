package desktopd

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// papers 2026-11-11 10:08-10:12: one bot's long bash held the read gate; a
// waiting writer (Ensure before a screenshot, a bot Stop) froze every other
// bot of the container behind it.
func TestD98WaitingWriterDoesNotQueueReaders(t *testing.T) {
	svc := &Service{}
	release := make(chan struct{})
	longHeld := make(chan struct{})
	go func() {
		_ = svc.withUserRead("t", "u", func() error { close(longHeld); <-release; return nil })
	}()
	<-longHeld
	writerDone := make(chan struct{})
	go func() {
		_ = svc.withUser("t", "u", func() error { return nil })
		close(writerDone)
	}()
	time.Sleep(60 * time.Millisecond) // the writer is now waiting
	readerDone := make(chan struct{})
	go func() {
		_ = svc.withUserRead("t", "u", func() error { return nil })
		close(readerDone)
	}()
	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("a reader queued behind a waiting writer")
	}
	select {
	case <-writerDone:
		t.Fatal("writer ran while a reader held the gate")
	default:
	}
	close(release)
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("writer never got the gate after the readers left")
	}
}

func TestD98WriterStillExcludesReaders(t *testing.T) {
	svc := &Service{}
	var mu sync.Mutex
	inWriter, overlap := false, false
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = svc.withUser("t", "u", func() error {
				mu.Lock()
				inWriter = true
				mu.Unlock()
				time.Sleep(time.Millisecond)
				mu.Lock()
				inWriter = false
				mu.Unlock()
				return nil
			})
		}()
		go func() {
			defer wg.Done()
			_ = svc.withUserRead("t", "u", func() error {
				mu.Lock()
				if inWriter {
					overlap = true
				}
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	if overlap {
		t.Fatal("a reader ran inside the writer")
	}
}

func TestD98EnsureRunningDoesNotWaitForLongBash(t *testing.T) {
	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		if strings.HasPrefix(strings.Join(args, " "), "inspect ") {
			return inspectLine(t, true, "tenant", "alice"), nil
		}
		return "", nil
	}}
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = svc.withUserRead("tenant", "alice", func() error { close(held); <-release; return nil })
	}()
	<-held
	defer close(release)
	done := make(chan error, 1)
	go func() {
		done <- svc.Ensure(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", Memory: "3g", CPUs: "1.5", ShmSize: "1g"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ensure of a running desktop waited for another call's bash")
	}
}

func TestD98StopBotDisplayTakesNoGate(t *testing.T) {
	svc := &Service{Run: func(context.Context, ...string) (string, error) { return "", nil }}
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = svc.withUserRead("tenant", "alice", func() error { close(held); <-release; return nil })
	}()
	<-held
	defer close(release)
	done := make(chan error, 1)
	go func() {
		_, err := svc.Stop(context.Background(), "tenant", "alice::desk::bot_d98")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stopping a bot display waited for the container gate")
	}
}

func TestD98ImagePresentIgnoresCancelledInspect(t *testing.T) {
	calls := 0
	svc := &Service{Run: func(ctx context.Context, args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("docker: context canceled")
		}
		return "sha256:abc", nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !svc.imagePresent(ctx, "maclaw-gui:2") {
		t.Fatal("a cancelled inspect made a present image look missing")
	}
	missing := &Service{Run: func(context.Context, ...string) (string, error) {
		return "Error: No such image: maclaw-gui:2", errors.New("docker: Error: No such image: maclaw-gui:2")
	}}
	if missing.imagePresent(context.Background(), "maclaw-gui:2") {
		t.Fatal("a missing image looked present")
	}
}
