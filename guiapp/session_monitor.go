package guiapp

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// SessionMonitor — lightweight session status poller (no LLM, pure Go)
// ---------------------------------------------------------------------------

// SessionMonitor periodically polls the status of busy coding sessions and
// pushes StatusEvents when the status changes (e.g. busy → waiting_input,
// busy → exited). It does NOT use LLM inference — all checks are direct
// field reads on RemoteSession.
//
// Division of labor with StallDetector:
//   - StallDetector: detects stalled sessions (no output), sends nudge
//   - SessionMonitor: detects status transitions, notifies user
type SessionMonitor struct {
	mu       sync.Mutex
	watches  map[string]*sessionWatch // sessionID -> watch
	manager  *RemoteSessionManager
	statusC  chan StatusEvent
	interval time.Duration // polling interval (default 20s)
}

// sessionWatch tracks the monitoring state for a single session.
type sessionWatch struct {
	sessionID  string
	loopID     string // associated background loop (if any)
	lastStatus SessionStatus
	cancelCh   chan struct{}
}

// NewSessionMonitor creates a SessionMonitor with the given polling interval.
// If interval <= 0, defaults to 20 seconds.
func NewSessionMonitor(manager *RemoteSessionManager, statusC chan StatusEvent, interval time.Duration) *SessionMonitor {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	return &SessionMonitor{
		watches:  make(map[string]*sessionWatch),
		manager:  manager,
		statusC:  statusC,
		interval: interval,
	}
}

// StartWatching begins polling a session's status. If the session is already
// being watched, the existing watch is stopped and replaced.
func (m *SessionMonitor) StartWatching(sessionID string, loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing watch if any
	if existing, ok := m.watches[sessionID]; ok {
		m.stopWatchLocked(existing)
	}

	// Determine initial status
	var initialStatus SessionStatus
	if m.manager != nil {
		if s, ok := m.manager.Get(sessionID); ok {
			s.mu.RLock()
			initialStatus = s.Status
			s.mu.RUnlock()
		}
	}

	w := &sessionWatch{
		sessionID:  sessionID,
		loopID:     loopID,
		lastStatus: initialStatus,
		cancelCh:   make(chan struct{}),
	}
	m.watches[sessionID] = w

	go m.pollLoop(w)
}

// StopWatching stops polling for a session.
func (m *SessionMonitor) StopWatching(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if w, ok := m.watches[sessionID]; ok {
		m.stopWatchLocked(w)
		delete(m.watches, sessionID)
	}
}

// IsWatching returns true if the session is currently being monitored.
func (m *SessionMonitor) IsWatching(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watches[sessionID]
	return ok
}

// WatchCount returns the number of sessions being monitored.
func (m *SessionMonitor) WatchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.watches)
}

// Close stops all watches and releases resources.
func (m *SessionMonitor) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, w := range m.watches {
		m.stopWatchLocked(w)
		delete(m.watches, id)
	}
}

// stopWatchLocked stops a single watch. Caller must hold m.mu.
func (m *SessionMonitor) stopWatchLocked(w *sessionWatch) {
	select {
	case <-w.cancelCh:
		// already closed
	default:
		close(w.cancelCh)
	}
}

// pollLoop is the per-session goroutine that periodically checks status.
func (m *SessionMonitor) pollLoop(w *sessionWatch) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[session-monitor-panic] session=%s recovered: %v\n", w.sessionID, r)
		}
	}()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.cancelCh:
			return
		case <-ticker.C:
			m.checkStatus(w)
		}
	}
}

// checkStatus reads the session's current status and emits events on transitions.
func (m *SessionMonitor) checkStatus(w *sessionWatch) {
	if m.manager == nil {
		return
	}

	s, ok := m.manager.Get(w.sessionID)
	if !ok {
		// Session has been cleaned up — stop watching and notify
		m.emitEvent(StatusEvent{
			Type:      StatusEventSessionFailed,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   fmt.Sprintf("会话 %s 已被清理，监控停止", w.sessionID),
		})
		m.StopWatching(w.sessionID)
		return
	}

	s.mu.RLock()
	currentStatus := s.Status
	s.mu.RUnlock()

	if currentStatus == w.lastStatus {
		return // no change
	}

	oldStatus := w.lastStatus
	w.lastStatus = currentStatus

	// Detect meaningful transitions
	switch {
	case isBusyStatus(oldStatus) && currentStatus == SessionWaitingInput:
		// busy → waiting_input: session completed a task
		m.emitEvent(StatusEvent{
			Type:      StatusEventSessionCompleted,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   fmt.Sprintf("编程会话 %s 已完成（等待输入）", w.sessionID),
		})

	case isBusyStatus(oldStatus) && (currentStatus == SessionExited || currentStatus == SessionError):
		// busy → exited/error: session ended
		evtType := StatusEventSessionCompleted
		msg := fmt.Sprintf("编程会话 %s 已退出", w.sessionID)
		if currentStatus == SessionError {
			evtType = StatusEventSessionFailed
			msg = fmt.Sprintf("编程会话 %s 出错退出", w.sessionID)
		}
		m.emitEvent(StatusEvent{
			Type:      evtType,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   msg,
		})
		// Auto-stop watching for terminal states
		m.StopWatching(w.sessionID)

	case currentStatus == SessionExited || currentStatus == SessionError:
		// Any transition to terminal state
		m.emitEvent(StatusEvent{
			Type:      StatusEventStopped,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   fmt.Sprintf("会话 %s 状态变为 %s", w.sessionID, string(currentStatus)),
		})
		m.StopWatching(w.sessionID)
	}
}

// emitEvent sends a StatusEvent to the status channel (non-blocking).
func (m *SessionMonitor) emitEvent(evt StatusEvent) {
	select {
	case m.statusC <- evt:
	default:
		// Channel full — log and drop
		fmt.Fprintf(os.Stderr, "[session-monitor] status channel full, dropping event for session=%s\n", evt.SessionID)
	}
}

// isBusyStatus returns true for statuses that indicate the session is actively working.
func isBusyStatus(s SessionStatus) bool {
	return s == SessionBusy || s == SessionRunning
}
