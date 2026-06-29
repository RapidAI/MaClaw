package device

import (
	"testing"
	"time"
)

func TestPendingMessageQueue_EnqueueAndDrain(t *testing.T) {
	q := NewPendingMessageQueue()

	q.Enqueue("machine-a", "msg1")
	q.Enqueue("machine-a", "msg2")
	q.Enqueue("machine-b", "msg3")

	if got := q.PendingCount("machine-a"); got != 2 {
		t.Fatalf("machine-a pending = %d, want 2", got)
	}
	if got := q.PendingCount("machine-b"); got != 1 {
		t.Fatalf("machine-b pending = %d, want 1", got)
	}

	drained := q.Drain("machine-a")
	if len(drained) != 2 {
		t.Fatalf("drained = %d, want 2", len(drained))
	}
	if drained[0] != "msg1" || drained[1] != "msg2" {
		t.Fatalf("drained = %v, want [msg1, msg2]", drained)
	}

	// Second drain should return nil (already consumed)
	if got := q.Drain("machine-a"); got != nil {
		t.Fatalf("second drain = %v, want nil", got)
	}

	// machine-b unaffected
	drained = q.Drain("machine-b")
	if len(drained) != 1 || drained[0] != "msg3" {
		t.Fatalf("machine-b drained = %v, want [msg3]", drained)
	}
}

func TestPendingMessageQueue_MaxSize(t *testing.T) {
	q := &PendingMessageQueue{
		queues:  make(map[string][]pendingMessage),
		maxSize: 3,
		maxAge:  5 * time.Minute,
	}

	q.Enqueue("m", "a")
	q.Enqueue("m", "b")
	q.Enqueue("m", "c")
	q.Enqueue("m", "d") // should drop "a"

	drained := q.Drain("m")
	if len(drained) != 3 {
		t.Fatalf("drained = %d, want 3", len(drained))
	}
	if drained[0] != "b" || drained[1] != "c" || drained[2] != "d" {
		t.Fatalf("drained = %v, want [b, c, d]", drained)
	}
}

func TestPendingMessageQueue_Expiry(t *testing.T) {
	q := &PendingMessageQueue{
		queues:  make(map[string][]pendingMessage),
		maxSize: 50,
		maxAge:  50 * time.Millisecond,
	}

	q.Enqueue("m", "old")
	time.Sleep(100 * time.Millisecond)
	q.Enqueue("m", "new")

	drained := q.Drain("m")
	if len(drained) != 1 || drained[0] != "new" {
		t.Fatalf("drained = %v, want [new] (old should be expired)", drained)
	}
}

func TestPendingMessageQueue_EmptyMachineID(t *testing.T) {
	q := NewPendingMessageQueue()
	q.Enqueue("", "msg")
	if got := q.Drain(""); got != nil {
		t.Fatalf("empty machine_id drain = %v, want nil", got)
	}
}

func TestPendingMessageQueue_NilMessage(t *testing.T) {
	q := NewPendingMessageQueue()
	q.Enqueue("m", nil)
	if got := q.PendingCount("m"); got != 0 {
		t.Fatalf("nil msg count = %d, want 0", got)
	}
}
