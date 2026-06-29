package device

import (
	"sync"
	"time"
)

// pendingMessage represents a message buffered while a machine was offline.
type pendingMessage struct {
	Msg       any
	EnqueueAt time.Time
}

// PendingMessageQueue buffers messages for machines that are temporarily
// offline (e.g., during Hub redeployment). Messages are drained and delivered
// when the machine reconnects.
//
// Design constraints:
//   - Maximum 50 messages per machine (ring buffer, oldest dropped)
//   - Messages expire after 5 minutes (not delivered if machine takes too long)
//   - Thread-safe: multiple goroutines may enqueue concurrently
type PendingMessageQueue struct {
	mu       sync.Mutex
	queues   map[string][]pendingMessage
	maxSize  int
	maxAge   time.Duration
}

const (
	defaultPendingQueueMaxSize = 50
	defaultPendingQueueMaxAge  = 5 * time.Minute
)

// NewPendingMessageQueue creates a new pending message queue with default limits.
func NewPendingMessageQueue() *PendingMessageQueue {
	return &PendingMessageQueue{
		queues:  make(map[string][]pendingMessage),
		maxSize: defaultPendingQueueMaxSize,
		maxAge:  defaultPendingQueueMaxAge,
	}
}

// Enqueue adds a message to the pending queue for a machine.
// If the queue is full, the oldest message is dropped.
// Messages older than maxAge are evicted on enqueue.
func (q *PendingMessageQueue) Enqueue(machineID string, msg any) {
	if machineID == "" || msg == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	queue := q.evictExpiredLocked(machineID, now)

	// Drop oldest if at capacity
	if len(queue) >= q.maxSize {
		queue = queue[1:]
	}

	queue = append(queue, pendingMessage{Msg: msg, EnqueueAt: now})
	q.queues[machineID] = queue
}

// Drain removes and returns all pending messages for a machine.
// Expired messages are filtered out. Returns nil if no messages are pending.
func (q *PendingMessageQueue) Drain(machineID string) []any {
	if machineID == "" {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	queue, ok := q.queues[machineID]
	if !ok || len(queue) == 0 {
		return nil
	}
	delete(q.queues, machineID)

	now := time.Now()
	var result []any
	for _, pm := range queue {
		if now.Sub(pm.EnqueueAt) <= q.maxAge {
			result = append(result, pm.Msg)
		}
	}
	return result
}

// PendingCount returns the number of pending messages for a machine.
func (q *PendingMessageQueue) PendingCount(machineID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues[machineID])
}

// GC removes empty and fully-expired machine queues from the map.
// Should be called periodically (e.g., every 10 minutes) to prevent
// the map from growing unboundedly for permanently offline machines.
func (q *PendingMessageQueue) GC() {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	for machineID, queue := range q.queues {
		if len(queue) == 0 {
			delete(q.queues, machineID)
			continue
		}
		// Check if all messages are expired
		allExpired := true
		for _, pm := range queue {
			if now.Sub(pm.EnqueueAt) <= q.maxAge {
				allExpired = false
				break
			}
		}
		if allExpired {
			delete(q.queues, machineID)
		}
	}
}

// evictExpiredLocked removes expired messages from a machine's queue.
// Must be called with q.mu held.
func (q *PendingMessageQueue) evictExpiredLocked(machineID string, now time.Time) []pendingMessage {
	queue := q.queues[machineID]
	if len(queue) == 0 {
		return queue
	}
	// Messages are ordered by EnqueueAt (append-only). Find the first
	// non-expired message; everything before it is expired.
	firstValid := -1
	for i, pm := range queue {
		if now.Sub(pm.EnqueueAt) <= q.maxAge {
			firstValid = i
			break
		}
	}
	if firstValid < 0 {
		// All expired
		delete(q.queues, machineID)
		return nil
	}
	if firstValid > 0 {
		queue = queue[firstValid:]
		q.queues[machineID] = queue
	}
	return queue
}
