package ve

import (
	"sync"
	"time"
)

// SessionFIFORelay guarantees FIFO (First-In-First-Out) message ordering
// within a single A2A session. When the Hub relays messages between participants,
// messages for the same session are delivered in the order they were received.
//
// This is implemented using per-session sequential delivery: each session has
// its own message queue, and messages are delivered one at a time in order.
type SessionFIFORelay struct {
	mu       sync.Mutex
	sessions map[string]*sessionQueue
}

// sessionQueue maintains ordered message delivery for a single session.
type sessionQueue struct {
	SessionID    string
	messages     []queuedMessage
	delivering   bool
	lastActivity time.Time
}

// queuedMessage represents a message waiting to be delivered.
type queuedMessage struct {
	ID        string
	SessionID string
	FromID    string
	Content   []byte // serialized message payload
	EnqueueAt time.Time
	DeliverFn func([]byte) error // delivery callback
}

// NewSessionFIFORelay creates a new FIFO relay.
func NewSessionFIFORelay() *SessionFIFORelay {
	return &SessionFIFORelay{
		sessions: make(map[string]*sessionQueue),
	}
}

// Enqueue adds a message to the session's FIFO queue and triggers delivery.
// The deliverFn is called with the serialized message payload when it's the
// message's turn to be delivered. Messages within the same session are
// guaranteed to be delivered in enqueue order.
func (r *SessionFIFORelay) Enqueue(sessionID, messageID, fromID string, payload []byte, deliverFn func([]byte) error) {
	r.mu.Lock()
	q, ok := r.sessions[sessionID]
	if !ok {
		q = &sessionQueue{
			SessionID:    sessionID,
			messages:     make([]queuedMessage, 0, 8),
			lastActivity: time.Now(),
		}
		r.sessions[sessionID] = q
	}
	q.messages = append(q.messages, queuedMessage{
		ID:        messageID,
		SessionID: sessionID,
		FromID:    fromID,
		Content:   payload,
		EnqueueAt: time.Now(),
		DeliverFn: deliverFn,
	})
	q.lastActivity = time.Now()

	// If not currently delivering, start delivery
	shouldDeliver := !q.delivering
	if shouldDeliver {
		q.delivering = true
	}
	r.mu.Unlock()

	if shouldDeliver {
		go r.deliverLoop(sessionID, q)
	}
}

// deliverLoop processes messages for a session in FIFO order.
func (r *SessionFIFORelay) deliverLoop(sessionID string, q *sessionQueue) {
	for {
		r.mu.Lock()
		current, ok := r.sessions[sessionID]
		if !ok || current != q {
			r.mu.Unlock()
			return
		}
		if len(current.messages) == 0 {
			current.delivering = false
			r.mu.Unlock()
			return
		}
		msg := current.messages[0]
		current.messages = current.messages[1:]
		r.mu.Unlock()

		if msg.DeliverFn != nil {
			_ = msg.DeliverFn(msg.Content)
		}
	}
}

// SessionMessageCount returns the number of pending messages for a session.
func (r *SessionFIFORelay) SessionMessageCount(sessionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	q, ok := r.sessions[sessionID]
	if !ok {
		return 0
	}
	return len(q.messages)
}

// CloseSession removes a session's queue and discards any pending messages.
func (r *SessionFIFORelay) CloseSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionID)
}

// ActiveSessionCount returns the number of sessions with active queues.
func (r *SessionFIFORelay) ActiveSessionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

// CleanupStale removes sessions that have been inactive for longer than maxAge.
func (r *SessionFIFORelay) CleanupStale(maxAge time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	removed := 0
	for id, q := range r.sessions {
		if now.Sub(q.lastActivity) > maxAge && len(q.messages) == 0 {
			delete(r.sessions, id)
			removed++
		}
	}
	return removed
}
