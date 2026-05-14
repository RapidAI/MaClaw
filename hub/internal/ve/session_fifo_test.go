package ve

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionFIFORelay_BasicFIFOOrdering(t *testing.T) {
	relay := NewSessionFIFORelay()

	var delivered []string
	var mu sync.Mutex

	deliverFn := func(payload []byte) error {
		mu.Lock()
		delivered = append(delivered, string(payload))
		mu.Unlock()
		return nil
	}

	// Enqueue 5 messages for the same session
	for i := 0; i < 5; i++ {
		relay.Enqueue("session-1", fmt.Sprintf("msg-%d", i), "user-a",
			[]byte(fmt.Sprintf("message-%d", i)), deliverFn)
	}

	// Wait for delivery
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(delivered) != 5 {
		t.Fatalf("expected 5 delivered messages, got %d", len(delivered))
	}

	// Verify FIFO order
	for i, msg := range delivered {
		expected := fmt.Sprintf("message-%d", i)
		if msg != expected {
			t.Errorf("message %d: expected %q, got %q", i, expected, msg)
		}
	}
}

func TestSessionFIFORelay_DifferentSessionsIndependent(t *testing.T) {
	relay := NewSessionFIFORelay()

	var session1Msgs []string
	var session2Msgs []string
	var mu sync.Mutex

	deliverFn1 := func(payload []byte) error {
		mu.Lock()
		session1Msgs = append(session1Msgs, string(payload))
		mu.Unlock()
		return nil
	}
	deliverFn2 := func(payload []byte) error {
		mu.Lock()
		session2Msgs = append(session2Msgs, string(payload))
		mu.Unlock()
		return nil
	}

	// Enqueue messages for two different sessions
	relay.Enqueue("session-1", "msg-1a", "user-a", []byte("s1-msg-1"), deliverFn1)
	relay.Enqueue("session-2", "msg-2a", "user-b", []byte("s2-msg-1"), deliverFn2)
	relay.Enqueue("session-1", "msg-1b", "user-a", []byte("s1-msg-2"), deliverFn1)
	relay.Enqueue("session-2", "msg-2b", "user-b", []byte("s2-msg-2"), deliverFn2)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(session1Msgs) != 2 {
		t.Fatalf("session-1: expected 2 messages, got %d", len(session1Msgs))
	}
	if len(session2Msgs) != 2 {
		t.Fatalf("session-2: expected 2 messages, got %d", len(session2Msgs))
	}

	// Each session maintains its own FIFO order
	if session1Msgs[0] != "s1-msg-1" || session1Msgs[1] != "s1-msg-2" {
		t.Errorf("session-1 order wrong: %v", session1Msgs)
	}
	if session2Msgs[0] != "s2-msg-1" || session2Msgs[1] != "s2-msg-2" {
		t.Errorf("session-2 order wrong: %v", session2Msgs)
	}
}

func TestSessionFIFORelay_CloseSession(t *testing.T) {
	relay := NewSessionFIFORelay()

	relay.Enqueue("session-1", "msg-1", "user-a", []byte("hello"), func([]byte) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	if relay.ActiveSessionCount() != 1 {
		t.Fatalf("expected 1 active session, got %d", relay.ActiveSessionCount())
	}

	relay.CloseSession("session-1")

	if relay.ActiveSessionCount() != 0 {
		t.Fatalf("expected 0 active sessions after close, got %d", relay.ActiveSessionCount())
	}
}

func TestSessionFIFORelay_CleanupStale(t *testing.T) {
	relay := NewSessionFIFORelay()

	relay.Enqueue("session-old", "msg-1", "user-a", []byte("old"), func([]byte) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	// Cleanup with very short maxAge should remove the session
	removed := relay.CleanupStale(10 * time.Millisecond)
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}

	if relay.ActiveSessionCount() != 0 {
		t.Fatalf("expected 0 active sessions after cleanup, got %d", relay.ActiveSessionCount())
	}
}

func TestSessionFIFORelay_ConcurrentEnqueue(t *testing.T) {
	relay := NewSessionFIFORelay()

	var deliveredCount int64

	deliverFn := func(payload []byte) error {
		atomic.AddInt64(&deliveredCount, 1)
		return nil
	}

	// Concurrent enqueue from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			relay.Enqueue("session-1", fmt.Sprintf("msg-%d", idx), "user-a",
				[]byte(fmt.Sprintf("payload-%d", idx)), deliverFn)
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	count := atomic.LoadInt64(&deliveredCount)
	if count != 10 {
		t.Fatalf("expected 10 delivered messages, got %d", count)
	}
}

func TestSessionFIFORelay_CloseAndRecreateSessionDoesNotLetOldLoopStealNewQueue(t *testing.T) {
	relay := NewSessionFIFORelay()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondDelivered := make(chan struct{}, 1)

	relay.Enqueue("session-1", "msg-1", "user-a", []byte("first"), func([]byte) error {
		close(firstStarted)
		<-releaseFirst
		return nil
	})

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first delivery did not start")
	}

	relay.CloseSession("session-1")
	relay.Enqueue("session-1", "msg-2", "user-a", []byte("second"), func([]byte) error {
		secondDelivered <- struct{}{}
		return nil
	})

	select {
	case <-secondDelivered:
	case <-time.After(time.Second):
		t.Fatal("new session queue was not delivered")
	}

	close(releaseFirst)
}
func TestSessionFIFORelay_DeliveryErrorDoesNotBlockQueue(t *testing.T) {
	relay := NewSessionFIFORelay()

	var delivered []string
	var mu sync.Mutex

	// First message returns error, second should still be delivered
	callCount := 0
	deliverFn := func(payload []byte) error {
		mu.Lock()
		callCount++
		n := callCount
		mu.Unlock()

		if n == 1 {
			return fmt.Errorf("delivery failed")
		}
		mu.Lock()
		delivered = append(delivered, string(payload))
		mu.Unlock()
		return nil
	}

	relay.Enqueue("session-1", "msg-1", "user-a", []byte("first"), deliverFn)
	relay.Enqueue("session-1", "msg-2", "user-a", []byte("second"), deliverFn)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(delivered) != 1 {
		t.Fatalf("expected 1 delivered message (second), got %d", len(delivered))
	}
	if delivered[0] != "second" {
		t.Errorf("expected 'second', got %q", delivered[0])
	}
}
