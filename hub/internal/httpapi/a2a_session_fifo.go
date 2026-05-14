package httpapi

import (
	"sync"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
)

// SessionMessageQueue provides per-session FIFO ordering guarantee for message relay.
// The Hub relay guarantees that messages within the same session are delivered
// in the order they were received (FIFO). This is achieved by serializing
// message processing per session using per-session mutexes.
type SessionMessageQueue struct {
	mu       sync.Mutex
	sessions map[string]*sessionQueue
}

// sessionQueue holds the per-session state for FIFO ordering.
type sessionQueue struct {
	mu          sync.Mutex
	lastSeqNum  int64
	lastMsgTime time.Time
}

// NewSessionMessageQueue creates a new session message queue.
func NewSessionMessageQueue() *SessionMessageQueue {
	return &SessionMessageQueue{
		sessions: make(map[string]*sessionQueue),
	}
}

// AcquireSession returns the per-session mutex for FIFO ordering.
// Callers must call the returned unlock function after processing the message.
// This ensures that messages within the same session are processed sequentially.
func (q *SessionMessageQueue) AcquireSession(sessionID string) func() {
	q.mu.Lock()
	sq, ok := q.sessions[sessionID]
	if !ok {
		sq = &sessionQueue{}
		q.sessions[sessionID] = sq
	}
	q.mu.Unlock()

	sq.mu.Lock()
	sq.lastSeqNum++
	sq.lastMsgTime = time.Now()
	return sq.mu.Unlock
}

// EnqueueMessage assigns a sequence number to a message within a session,
// guaranteeing FIFO delivery order. Returns the assigned sequence number.
func (q *SessionMessageQueue) EnqueueMessage(sessionID string, msg *corea2a.GroupDiscussionMessage) int64 {
	q.mu.Lock()
	sq, ok := q.sessions[sessionID]
	if !ok {
		sq = &sessionQueue{}
		q.sessions[sessionID] = sq
	}
	q.mu.Unlock()

	sq.mu.Lock()
	defer sq.mu.Unlock()
	sq.lastSeqNum++
	sq.lastMsgTime = time.Now()
	return sq.lastSeqNum
}

// RemoveSession removes a session's queue state (called when session is closed).
func (q *SessionMessageQueue) RemoveSession(sessionID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.sessions, sessionID)
}

// SessionCount returns the number of tracked sessions.
func (q *SessionMessageQueue) SessionCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.sessions)
}

// CleanupStale removes session queues that have been inactive for the given duration.
func (q *SessionMessageQueue) CleanupStale(maxAge time.Duration) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, sq := range q.sessions {
		sq.mu.Lock()
		stale := sq.lastMsgTime.Before(cutoff)
		sq.mu.Unlock()
		if stale {
			delete(q.sessions, id)
			removed++
		}
	}
	return removed
}
