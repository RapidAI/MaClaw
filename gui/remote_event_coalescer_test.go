package main

import (
	"sync"
	"testing"
	"time"
)

func TestCoalescerNonToolUseFlushesImmediately(t *testing.T) {
	var mu sync.Mutex
	var got []ImportantEvent
	c := NewEventCoalescer(200*time.Millisecond, func(events []ImportantEvent) {
		mu.Lock()
		got = append(got, events...)
		mu.Unlock()
	})
	defer c.Close()

	evt := ImportantEvent{EventID: "e1", Type: "file_edit", Title: "edited main.go"}
	c.Enqueue(evt)

	mu.Lock()
	count := len(got)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 event flushed immediately, got %d", count)
	}
	if got[0].EventID != "e1" {
		t.Fatalf("expected event e1, got %s", got[0].EventID)
	}
}

func TestCoalescerToolUseBufferedThenFlushed(t *testing.T) {
	var mu sync.Mutex
	var got []ImportantEvent
	c := NewEventCoalescer(100*time.Millisecond, func(events []ImportantEvent) {
		mu.Lock()
		got = append(got, events...)
		mu.Unlock()
	})
	defer c.Close()

	evt := ImportantEvent{EventID: "e2", Type: "tool_use", Title: "Read", Summary: "reading file"}
	c.Enqueue(evt)

	// Should not be flushed yet.
	mu.Lock()
	count := len(got)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 events before window, got %d", count)
	}

	// Wait for window to expire.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count = len(got)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 event after window, got %d", count)
	}
	if got[0].Grouped {
		t.Fatal("event should not be grouped when flushed by timer")
	}
}

func TestCoalescerCompleteToolCallMerges(t *testing.T) {
	var mu sync.Mutex
	var got []ImportantEvent
	c := NewEventCoalescer(500*time.Millisecond, func(events []ImportantEvent) {
		mu.Lock()
		got = append(got, events...)
		mu.Unlock()
	})
	defer c.Close()

	evt := ImportantEvent{EventID: "e3", Type: "tool_use", Title: "Write", Summary: "writing file"}
	c.Enqueue(evt)

	// Complete before window expires.
	c.CompleteToolCall("e3")

	mu.Lock()
	count := len(got)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 merged event, got %d", count)
	}
	if !got[0].Grouped {
		t.Fatal("expected event to be marked as grouped")
	}
	if got[0].Summary != "writing file [done]" {
		t.Fatalf("expected merged summary, got %q", got[0].Summary)
	}
}

func TestCoalescerCompleteAfterTimerIsNoop(t *testing.T) {
	var mu sync.Mutex
	var got []ImportantEvent
	c := NewEventCoalescer(50*time.Millisecond, func(events []ImportantEvent) {
		mu.Lock()
		got = append(got, events...)
		mu.Unlock()
	})
	defer c.Close()

	evt := ImportantEvent{EventID: "e4", Type: "tool_use", Title: "Bash", Summary: "running cmd"}
	c.Enqueue(evt)

	// Wait for timer to flush.
	time.Sleep(100 * time.Millisecond)

	// Late complete — should not produce a second event.
	c.CompleteToolCall("e4")

	mu.Lock()
	count := len(got)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected exactly 1 event, got %d", count)
	}
}

func TestCoalescerFlushDrainsAll(t *testing.T) {
	var mu sync.Mutex
	var got []ImportantEvent
	c := NewEventCoalescer(10*time.Second, func(events []ImportantEvent) {
		mu.Lock()
		got = append(got, events...)
		mu.Unlock()
	})

	c.Enqueue(ImportantEvent{EventID: "a", Type: "tool_use", Title: "A"})
	c.Enqueue(ImportantEvent{EventID: "b", Type: "tool_use", Title: "B"})

	c.Flush()

	mu.Lock()
	count := len(got)
	mu.Unlock()
	if count != 2 {
		t.Fatalf("expected 2 events after Flush, got %d", count)
	}
}
