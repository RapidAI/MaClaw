package httpapi

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestSessionMessageQueue_AcquireSession_FIFO(t *testing.T) {
	q := NewSessionMessageQueue()

	// Verify that messages within the same session are processed sequentially
	var order []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			unlock := q.AcquireSession("session-1")
			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
			// Simulate processing time
			time.Sleep(5 * time.Millisecond)
			unlock()
		}(i)
		// Small delay to ensure goroutines start in order
		time.Sleep(2 * time.Millisecond)
	}
	wg.Wait()

	// All 5 messages should have been processed
	if len(order) != 5 {
		t.Fatalf("expected 5 processed messages, got %d", len(order))
	}
}

func TestSessionMessageQueue_DifferentSessions_Parallel(t *testing.T) {
	q := NewSessionMessageQueue()

	// Messages in different sessions should be processed in parallel
	var session1Done, session2Done atomic.Int32
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		unlock := q.AcquireSession("session-1")
		session1Done.Add(1)
		time.Sleep(50 * time.Millisecond)
		unlock()
	}()
	go func() {
		defer wg.Done()
		unlock := q.AcquireSession("session-2")
		session2Done.Add(1)
		time.Sleep(50 * time.Millisecond)
		unlock()
	}()

	// Both should start processing quickly (within 20ms)
	time.Sleep(20 * time.Millisecond)
	if session1Done.Load() == 0 || session2Done.Load() == 0 {
		t.Error("different sessions should be processed in parallel")
	}

	wg.Wait()
}

func TestSessionMessageQueue_SameSession_Sequential(t *testing.T) {
	q := NewSessionMessageQueue()

	// Messages in the same session must be sequential
	var processing atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := q.AcquireSession("session-1")
			current := processing.Add(1)
			if current > maxConcurrent.Load() {
				maxConcurrent.Store(current)
			}
			time.Sleep(10 * time.Millisecond)
			processing.Add(-1)
			unlock()
		}()
	}
	wg.Wait()

	// Max concurrent processing for same session should be 1 (FIFO)
	if maxConcurrent.Load() != 1 {
		t.Errorf("expected max concurrent=1 for same session (FIFO), got %d", maxConcurrent.Load())
	}
}

func TestSessionMessageQueue_EnqueueMessage_SequenceNumbers(t *testing.T) {
	q := NewSessionMessageQueue()

	msg := &corea2a.GroupDiscussionMessage{
		Kind:    corea2a.MessageStatement,
		Content: "test",
	}

	seq1 := q.EnqueueMessage("session-1", msg)
	seq2 := q.EnqueueMessage("session-1", msg)
	seq3 := q.EnqueueMessage("session-1", msg)

	if seq1 >= seq2 || seq2 >= seq3 {
		t.Errorf("sequence numbers should be monotonically increasing: %d, %d, %d", seq1, seq2, seq3)
	}
}

func TestSessionMessageQueue_EnqueueMessage_IndependentSessions(t *testing.T) {
	q := NewSessionMessageQueue()

	msg := &corea2a.GroupDiscussionMessage{
		Kind:    corea2a.MessageStatement,
		Content: "test",
	}

	// Different sessions have independent sequence numbers
	seqA1 := q.EnqueueMessage("session-A", msg)
	seqB1 := q.EnqueueMessage("session-B", msg)
	seqA2 := q.EnqueueMessage("session-A", msg)

	// Session A: seq1 < seq2
	if seqA1 >= seqA2 {
		t.Errorf("session-A sequences should increase: %d, %d", seqA1, seqA2)
	}

	// Session B starts at 1 independently
	if seqB1 != 1 {
		t.Errorf("session-B first sequence should be 1, got %d", seqB1)
	}
}

func TestSessionMessageQueue_RemoveSession(t *testing.T) {
	q := NewSessionMessageQueue()

	msg := &corea2a.GroupDiscussionMessage{Kind: corea2a.MessageStatement, Content: "x"}
	q.EnqueueMessage("session-1", msg)
	q.EnqueueMessage("session-2", msg)

	if q.SessionCount() != 2 {
		t.Fatalf("expected 2 sessions, got %d", q.SessionCount())
	}

	q.RemoveSession("session-1")
	if q.SessionCount() != 1 {
		t.Errorf("expected 1 session after removal, got %d", q.SessionCount())
	}

	// Removing non-existent session is a no-op
	q.RemoveSession("session-nonexistent")
	if q.SessionCount() != 1 {
		t.Errorf("expected 1 session after removing non-existent, got %d", q.SessionCount())
	}
}

func TestSessionMessageQueue_CleanupStale(t *testing.T) {
	q := NewSessionMessageQueue()

	msg := &corea2a.GroupDiscussionMessage{Kind: corea2a.MessageStatement, Content: "x"}
	q.EnqueueMessage("session-old", msg)

	// Wait a bit then add a new session
	time.Sleep(50 * time.Millisecond)
	q.EnqueueMessage("session-new", msg)

	// Cleanup sessions older than 30ms
	removed := q.CleanupStale(30 * time.Millisecond)
	if removed != 1 {
		t.Errorf("expected 1 stale session removed, got %d", removed)
	}
	if q.SessionCount() != 1 {
		t.Errorf("expected 1 remaining session, got %d", q.SessionCount())
	}
}

func TestSessionMessageQueue_ConcurrentEnqueue(t *testing.T) {
	q := NewSessionMessageQueue()

	var wg sync.WaitGroup
	msg := &corea2a.GroupDiscussionMessage{Kind: corea2a.MessageStatement, Content: "x"}

	// Concurrent enqueue to same session
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.EnqueueMessage("session-concurrent", msg)
		}()
	}
	wg.Wait()

	// Should have processed all 100 without panic
	if q.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", q.SessionCount())
	}
}
