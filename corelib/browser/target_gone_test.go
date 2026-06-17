package browser

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTargetGoneCh_InitiallyOpen(t *testing.T) {
	s := &BrowserAgentSession{
		targetGoneCh: make(chan struct{}),
	}
	if !s.IsTargetAlive() {
		t.Fatal("expected target to be alive initially")
	}
	select {
	case <-s.TargetGone():
		t.Fatal("expected TargetGone channel to be open")
	default:
		// ok
	}
}

func TestTargetGoneCh_ClosedOnTargetDestroyed(t *testing.T) {
	s := &BrowserAgentSession{
		ID:           "test-session",
		TargetID:     "target-abc",
		targetGoneCh: make(chan struct{}),
	}

	// Simulate Target.targetDestroyed event for the active target.
	params, _ := json.Marshal(map[string]string{"targetId": "target-abc"})
	s.handleCDPEvent(CDPEvent{
		Method: "Target.targetDestroyed",
		Params: params,
	})

	if s.IsTargetAlive() {
		t.Fatal("expected target to be dead after destruction")
	}
	select {
	case <-s.TargetGone():
		// ok — channel is closed
	default:
		t.Fatal("expected TargetGone channel to be closed")
	}
}

func TestTargetGoneCh_NotClosedForOtherTarget(t *testing.T) {
	s := &BrowserAgentSession{
		ID:           "test-session",
		TargetID:     "target-abc",
		targetGoneCh: make(chan struct{}),
	}

	// Simulate Target.targetDestroyed event for a DIFFERENT target.
	params, _ := json.Marshal(map[string]string{"targetId": "target-OTHER"})
	s.handleCDPEvent(CDPEvent{
		Method: "Target.targetDestroyed",
		Params: params,
	})

	if !s.IsTargetAlive() {
		t.Fatal("expected target to remain alive when a different target is destroyed")
	}
}

func TestTargetGoneCh_ClosedOnInspectorDetached(t *testing.T) {
	s := &BrowserAgentSession{
		ID:           "test-session",
		TargetID:     "target-abc",
		targetGoneCh: make(chan struct{}),
	}

	s.handleCDPEvent(CDPEvent{
		Method: "Inspector.detached",
		Params: json.RawMessage(`{"reason":"replaced_with_devtools"}`),
	})

	if s.IsTargetAlive() {
		t.Fatal("expected target to be dead after inspector detach")
	}
}

func TestTargetGoneCh_ResetAfterReconnect(t *testing.T) {
	s := &BrowserAgentSession{
		ID:           "test-session",
		TargetID:     "target-abc",
		targetGoneCh: make(chan struct{}),
	}

	// Signal target gone.
	s.mu.Lock()
	s.signalTargetGone()
	s.mu.Unlock()

	if s.IsTargetAlive() {
		t.Fatal("expected dead after signal")
	}

	// Reset (simulates reconnection).
	s.mu.Lock()
	s.resetTargetGone()
	s.mu.Unlock()

	if !s.IsTargetAlive() {
		t.Fatal("expected alive after reset")
	}
}

func TestTargetGoneCh_SignalIdempotent(t *testing.T) {
	s := &BrowserAgentSession{
		ID:           "test-session",
		TargetID:     "target-abc",
		targetGoneCh: make(chan struct{}),
	}

	// Signal multiple times should not panic.
	s.mu.Lock()
	s.signalTargetGone()
	s.signalTargetGone()
	s.signalTargetGone()
	s.mu.Unlock()

	if s.IsTargetAlive() {
		t.Fatal("expected dead after signal")
	}
}

func TestWaitForActionSettle_AbortsOnTargetGone(t *testing.T) {
	// Create a CDPClient with closed channel already closed so Send() returns
	// immediately with "cdp connection closed" — this prevents the goroutine
	// inside waitForActionSettle from panicking on nil conn.
	cdpClosed := make(chan struct{})
	close(cdpClosed)
	s := &BrowserAgentSession{
		ID:           "test-session",
		TargetID:     "target-abc",
		targetGoneCh: make(chan struct{}),
		session:      &Session{client: &CDPClient{closed: cdpClosed}},
	}

	// Signal target gone before settle — the select on TargetGone() should win.
	s.mu.Lock()
	s.signalTargetGone()
	s.mu.Unlock()

	start := time.Now()
	s.waitForActionSettle(5*time.Second, 1*time.Second)
	elapsed := time.Since(start)

	// Should abort almost immediately, not wait 5 seconds.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForActionSettle took %v, expected immediate abort on target gone", elapsed)
	}
}

func TestNilSession_TargetGoneReturnsClosed(t *testing.T) {
	var s *BrowserAgentSession
	ch := s.TargetGone()
	select {
	case <-ch:
		// ok — nil session returns a closed channel (always "gone")
	default:
		t.Fatal("expected nil session TargetGone to return closed channel")
	}
	if s.IsTargetAlive() {
		t.Fatal("expected nil session to report not alive")
	}
}
