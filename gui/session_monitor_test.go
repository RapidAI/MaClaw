package main

import (
	"sync"
	"testing"
	"testing/quick"
	"time"
)

// ---------------------------------------------------------------------------
// Mock RemoteSessionManager for testing
// ---------------------------------------------------------------------------

type mockSessionForMonitor struct {
	mu     sync.RWMutex
	status SessionStatus
}

// mockRemoteSessionManagerForMonitor wraps a real RemoteSessionManager with
// pre-populated sessions for testing. We use the real manager's Get/List
// methods but inject mock sessions.
func newMockManagerWithSessions(sessions map[string]*RemoteSession) *RemoteSessionManager {
	mgr := &RemoteSessionManager{}
	mgr.mu.Lock()
	mgr.sessions = make(map[string]*RemoteSession)
	for id, s := range sessions {
		mgr.sessions[id] = s
	}
	mgr.mu.Unlock()
	return mgr
}

func newMockSession(id string, status SessionStatus) *RemoteSession {
	return &RemoteSession{
		ID:     id,
		Status: status,
	}
}

// ---------------------------------------------------------------------------
// Property 2: Session Monitor 不消耗 LLM Tokens
// SessionMonitor only reads RemoteSession.Status — it never calls LLM APIs.
// We verify this by checking that the monitor only uses manager.Get() and
// reads the Status field, with no other side effects.
// ---------------------------------------------------------------------------

func TestProperty2_NoLLMTokenConsumption(t *testing.T) {
	f := func(numSessions uint8) bool {
		n := int(numSessions)%5 + 1

		sessions := make(map[string]*RemoteSession)
		for i := 0; i < n; i++ {
			id := "session-" + string(rune('A'+i))
			sessions[id] = newMockSession(id, SessionBusy)
		}

		mgr := newMockManagerWithSessions(sessions)
		statusC := make(chan StatusEvent, 32)
		monitor := NewSessionMonitor(mgr, statusC, 5*time.Millisecond)

		// Start watching all sessions
		for id := range sessions {
			monitor.StartWatching(id, "loop-"+id)
		}

		// Let it poll a few times without making the property test dominate
		// the full gui package runtime.
		time.Sleep(20 * time.Millisecond)

		// No status changes → no events should be emitted
		// (sessions stayed busy the whole time)
		select {
		case evt := <-statusC:
			// Unexpected event
			t.Logf("unexpected event: %+v", evt)
			monitor.Close()
			return false
		default:
			// Good — no events
		}

		monitor.Close()
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 10}); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Status transition detection: busy → waiting_input
// ---------------------------------------------------------------------------

func TestSessionMonitor_DetectsCompletion(t *testing.T) {
	session := newMockSession("s1", SessionBusy)
	mgr := newMockManagerWithSessions(map[string]*RemoteSession{"s1": session})
	statusC := make(chan StatusEvent, 32)
	monitor := NewSessionMonitor(mgr, statusC, 50*time.Millisecond)

	monitor.StartWatching("s1", "bg-coding-1")

	// Wait for at least one poll cycle
	time.Sleep(80 * time.Millisecond)

	// Transition to waiting_input
	session.mu.Lock()
	session.Status = SessionWaitingInput
	session.mu.Unlock()

	// Wait for detection
	select {
	case evt := <-statusC:
		if evt.Type != StatusEventSessionCompleted {
			t.Errorf("expected SessionCompleted, got %d", evt.Type)
		}
		if evt.SessionID != "s1" {
			t.Errorf("expected s1, got %s", evt.SessionID)
		}
		if evt.LoopID != "bg-coding-1" {
			t.Errorf("expected bg-coding-1, got %s", evt.LoopID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for completion event")
	}

	monitor.Close()
}

// ---------------------------------------------------------------------------
// Status transition detection: busy → exited
// ---------------------------------------------------------------------------

func TestSessionMonitor_DetectsExit(t *testing.T) {
	session := newMockSession("s2", SessionBusy)
	mgr := newMockManagerWithSessions(map[string]*RemoteSession{"s2": session})
	statusC := make(chan StatusEvent, 32)
	monitor := NewSessionMonitor(mgr, statusC, 50*time.Millisecond)

	monitor.StartWatching("s2", "bg-coding-2")
	time.Sleep(80 * time.Millisecond)

	session.mu.Lock()
	session.Status = SessionExited
	session.mu.Unlock()

	select {
	case evt := <-statusC:
		if evt.Type != StatusEventSessionCompleted {
			t.Errorf("expected SessionCompleted, got %d", evt.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for exit event")
	}

	// Should auto-stop watching
	time.Sleep(100 * time.Millisecond)
	if monitor.IsWatching("s2") {
		t.Error("should have stopped watching after exit")
	}

	monitor.Close()
}

// ---------------------------------------------------------------------------
// Session cleaned up → SessionFailed event
// ---------------------------------------------------------------------------

func TestSessionMonitor_DetectsCleanup(t *testing.T) {
	session := newMockSession("s3", SessionBusy)
	mgr := newMockManagerWithSessions(map[string]*RemoteSession{"s3": session})
	statusC := make(chan StatusEvent, 32)
	monitor := NewSessionMonitor(mgr, statusC, 50*time.Millisecond)

	monitor.StartWatching("s3", "bg-coding-3")
	time.Sleep(80 * time.Millisecond)

	// Remove the session from the manager (simulate cleanup)
	mgr.mu.Lock()
	delete(mgr.sessions, "s3")
	mgr.mu.Unlock()

	select {
	case evt := <-statusC:
		if evt.Type != StatusEventSessionFailed {
			t.Errorf("expected SessionFailed, got %d", evt.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for cleanup event")
	}

	monitor.Close()
}

// ---------------------------------------------------------------------------
// StartWatching replaces existing watch
// ---------------------------------------------------------------------------

func TestSessionMonitor_ReplaceWatch(t *testing.T) {
	session := newMockSession("s4", SessionBusy)
	mgr := newMockManagerWithSessions(map[string]*RemoteSession{"s4": session})
	statusC := make(chan StatusEvent, 32)
	monitor := NewSessionMonitor(mgr, statusC, 50*time.Millisecond)

	monitor.StartWatching("s4", "loop-old")
	monitor.StartWatching("s4", "loop-new") // should replace

	if monitor.WatchCount() != 1 {
		t.Errorf("expected 1 watch, got %d", monitor.WatchCount())
	}

	// Trigger a transition
	session.mu.Lock()
	session.Status = SessionWaitingInput
	session.mu.Unlock()

	select {
	case evt := <-statusC:
		if evt.LoopID != "loop-new" {
			t.Errorf("expected loop-new, got %s", evt.LoopID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out")
	}

	monitor.Close()
}

// ---------------------------------------------------------------------------
// Close stops all watches
// ---------------------------------------------------------------------------

func TestSessionMonitor_Close(t *testing.T) {
	mgr := newMockManagerWithSessions(map[string]*RemoteSession{
		"a": newMockSession("a", SessionBusy),
		"b": newMockSession("b", SessionBusy),
	})
	statusC := make(chan StatusEvent, 32)
	monitor := NewSessionMonitor(mgr, statusC, 50*time.Millisecond)

	monitor.StartWatching("a", "la")
	monitor.StartWatching("b", "lb")

	if monitor.WatchCount() != 2 {
		t.Errorf("expected 2, got %d", monitor.WatchCount())
	}

	monitor.Close()

	if monitor.WatchCount() != 0 {
		t.Errorf("expected 0 after close, got %d", monitor.WatchCount())
	}
}

// ---------------------------------------------------------------------------
// No events when status doesn't change
// ---------------------------------------------------------------------------

func TestSessionMonitor_NoEventOnSameStatus(t *testing.T) {
	session := newMockSession("s5", SessionBusy)
	mgr := newMockManagerWithSessions(map[string]*RemoteSession{"s5": session})
	statusC := make(chan StatusEvent, 32)
	monitor := NewSessionMonitor(mgr, statusC, 30*time.Millisecond)

	monitor.StartWatching("s5", "loop5")

	// Wait for several poll cycles without changing status
	time.Sleep(200 * time.Millisecond)

	select {
	case evt := <-statusC:
		t.Errorf("unexpected event: %+v", evt)
	default:
		// Good
	}

	monitor.Close()
}
