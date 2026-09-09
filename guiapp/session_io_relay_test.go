package guiapp

import (
	"sync"
	"testing"
	"time"
)

func TestNewSessionIORelay(t *testing.T) {
	r := NewSessionIORelay()
	if r == nil {
		t.Fatal("NewSessionIORelay returned nil")
	}
	if r.listeners == nil {
		t.Fatal("listeners map not initialized")
	}
}

func TestSubscribeAndListenerCount(t *testing.T) {
	r := NewSessionIORelay()

	ch := r.Subscribe("s1", "d1")
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}
	if r.ListenerCount("s1") != 1 {
		t.Fatalf("expected 1 listener, got %d", r.ListenerCount("s1"))
	}

	r.Subscribe("s1", "d2")
	if r.ListenerCount("s1") != 2 {
		t.Fatalf("expected 2 listeners, got %d", r.ListenerCount("s1"))
	}

	// Different session
	if r.ListenerCount("s2") != 0 {
		t.Fatalf("expected 0 listeners for s2, got %d", r.ListenerCount("s2"))
	}
}

func TestUnsubscribe(t *testing.T) {
	r := NewSessionIORelay()

	ch := r.Subscribe("s1", "d1")
	r.Subscribe("s1", "d2")
	r.Unsubscribe("s1", "d1")

	if r.ListenerCount("s1") != 1 {
		t.Fatalf("expected 1 listener after unsubscribe, got %d", r.ListenerCount("s1"))
	}

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}

	// Unsubscribe last device cleans up session entry
	r.Unsubscribe("s1", "d2")
	if r.ListenerCount("s1") != 0 {
		t.Fatalf("expected 0 listeners, got %d", r.ListenerCount("s1"))
	}
}

func TestUnsubscribeNonExistent(t *testing.T) {
	r := NewSessionIORelay()
	// Should not panic
	r.Unsubscribe("no-session", "no-device")
	r.Subscribe("s1", "d1")
	r.Unsubscribe("s1", "no-device")
}

func TestBroadcastOutput(t *testing.T) {
	r := NewSessionIORelay()

	ch1 := r.Subscribe("s1", "d1")
	ch2 := r.Subscribe("s1", "d2")

	r.BroadcastOutput("s1", "hello")

	select {
	case msg := <-ch1:
		if msg != "hello" {
			t.Fatalf("d1 got %q, want %q", msg, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("d1 did not receive broadcast")
	}

	select {
	case msg := <-ch2:
		if msg != "hello" {
			t.Fatalf("d2 got %q, want %q", msg, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("d2 did not receive broadcast")
	}
}

func TestBroadcastOutputSkipsFullChannel(t *testing.T) {
	r := NewSessionIORelay()
	ch := r.Subscribe("s1", "d1")

	// Fill the channel buffer (capacity 64)
	for i := 0; i < 64; i++ {
		r.BroadcastOutput("s1", "fill")
	}

	// This should not block — the full channel is skipped
	done := make(chan struct{})
	go func() {
		r.BroadcastOutput("s1", "overflow")
		close(done)
	}()

	select {
	case <-done:
		// good, didn't block
	case <-time.After(time.Second):
		t.Fatal("BroadcastOutput blocked on full channel")
	}

	// Drain and verify we got 64 "fill" messages
	count := 0
	for range 64 {
		<-ch
		count++
	}
	if count != 64 {
		t.Fatalf("expected 64 messages, got %d", count)
	}
}

func TestBroadcastOutputNoListeners(t *testing.T) {
	r := NewSessionIORelay()
	// Should not panic
	r.BroadcastOutput("no-session", "hello")
}

func TestForwardInput(t *testing.T) {
	r := NewSessionIORelay()
	result := r.ForwardInput("s1", "d1", "some input")
	if result != "some input" {
		t.Fatalf("ForwardInput returned %q, want %q", result, "some input")
	}
}

func TestSubscribeReplacesExisting(t *testing.T) {
	r := NewSessionIORelay()

	oldCh := r.Subscribe("s1", "d1")
	newCh := r.Subscribe("s1", "d1")

	// Old channel should be closed
	_, ok := <-oldCh
	if ok {
		t.Fatal("expected old channel to be closed")
	}

	// New channel should work
	r.BroadcastOutput("s1", "test")
	select {
	case msg := <-newCh:
		if msg != "test" {
			t.Fatalf("got %q, want %q", msg, "test")
		}
	case <-time.After(time.Second):
		t.Fatal("new channel did not receive broadcast")
	}

	// Listener count should still be 1
	if r.ListenerCount("s1") != 1 {
		t.Fatalf("expected 1 listener, got %d", r.ListenerCount("s1"))
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewSessionIORelay()
	var wg sync.WaitGroup

	// Concurrent subscribes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			deviceID := "d" + string(rune('0'+id))
			r.Subscribe("s1", deviceID)
		}(i)
	}
	wg.Wait()

	if r.ListenerCount("s1") != 10 {
		t.Fatalf("expected 10 listeners, got %d", r.ListenerCount("s1"))
	}

	// Concurrent broadcasts
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.BroadcastOutput("s1", "concurrent")
		}()
	}
	wg.Wait()
}
