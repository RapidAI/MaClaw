package progress

import (
	"sync"
	"time"
)

// DefaultMessageQueueCapacity is the per-user pending-message bound from the
// IM dispatch design (§4.4): at most 3 messages wait behind the active task;
// anything beyond that evicts the oldest pending message and notifies the user.
const DefaultMessageQueueCapacity = 3

// QueuedMessage is one inbound message waiting for a user's previous message
// to finish processing. Payload carries the channel-specific delivery context
// (e.g. the gateway's incoming-message struct) and is passed back to Process
// verbatim.
type QueuedMessage struct {
	UserID     string
	Text       string
	Payload    any
	EnqueuedAt time.Time
}

// MessageQueue is an explicit per-user bounded queue with serial worker
// consumption. It replaces the "goroutine blocked on a per-user mutex"
// pattern: a message that arrives while its user's previous message is still
// being processed is enqueued and acknowledged immediately instead of holding
// a goroutine hostage on a lock.
//
// Semantics (IM dispatch design §4.4):
//   - Enqueue returns the 1-based position so the caller can give immediate
//     feedback ("已排队，前面还有 N 条").
//   - When the queue is full, the OLDEST pending message is evicted and
//     reported through the OnDropOldest callback so the user is notified.
//   - A per-user worker goroutine consumes messages serially via Process.
type MessageQueue struct {
	capacity int
	process  func(QueuedMessage)
	onDrop   func(dropped QueuedMessage)

	mu     sync.Mutex
	queues map[string]*pendingMessageLane
	wg     sync.WaitGroup
	closed bool
}

// pendingMessageLane holds one user's FIFO backlog and worker state.
type pendingMessageLane struct {
	msgs    []QueuedMessage
	running bool // a worker goroutine is draining this lane
}

// NewMessageQueue creates a queue. capacity <= 0 falls back to
// DefaultMessageQueueCapacity. process is invoked serially per user and must
// deliver the message through the channel's normal handling path. onDropOldest
// may be nil; when set it is called (outside the queue lock) for every evicted
// message.
func NewMessageQueue(capacity int, process func(QueuedMessage), onDropOldest func(QueuedMessage)) *MessageQueue {
	if capacity <= 0 {
		capacity = DefaultMessageQueueCapacity
	}
	return &MessageQueue{
		capacity: capacity,
		process:  process,
		onDrop:   onDropOldest,
		queues:   make(map[string]*pendingMessageLane),
	}
}

// Enqueue appends msg to the user's lane, spawning the lane worker when idle.
// It returns the 1-based position (1 = processed next) and the evicted oldest
// message when the lane was at capacity. ok is false once the queue has been
// shut down; the caller must then fall back to its legacy path.
func (q *MessageQueue) Enqueue(msg QueuedMessage) (position int, dropped *QueuedMessage, ok bool) {
	if msg.EnqueuedAt.IsZero() {
		msg.EnqueuedAt = time.Now()
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return 0, nil, false
	}
	lane := q.queues[msg.UserID]
	if lane == nil {
		lane = &pendingMessageLane{}
		q.queues[msg.UserID] = lane
	}
	if len(lane.msgs) >= q.capacity {
		evicted := lane.msgs[0]
		dropped = &evicted
		lane.msgs = append(lane.msgs[1:], msg)
	} else {
		lane.msgs = append(lane.msgs, msg)
	}
	position = len(lane.msgs)
	if !lane.running {
		lane.running = true
		q.wg.Add(1)
		go q.runWorker(msg.UserID, lane)
	}
	q.mu.Unlock()

	if dropped != nil && q.onDrop != nil {
		q.onDrop(*dropped)
	}
	return position, dropped, true
}

// Pending returns the number of messages currently waiting in the user's lane.
// Callers use this to keep FIFO order: a new message must enqueue instead of
// grabbing the processing lock directly whenever a backlog exists.
func (q *MessageQueue) Pending(userID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if lane := q.queues[userID]; lane != nil {
		return len(lane.msgs)
	}
	return 0
}

// Shutdown stops accepting new messages and waits for lane workers to finish
// their in-flight Process call. Messages still pending are dropped — shutdown
// is a teardown path, not a drain.
func (q *MessageQueue) Shutdown() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.wg.Wait()
}

// runWorker drains one lane serially and exits when it is empty. All lane
// mutations happen under q.mu so an Enqueue racing worker exit either appends
// to the live lane (worker sees it on the next iteration) or spawns a fresh
// worker after the lane was removed.
func (q *MessageQueue) runWorker(userID string, lane *pendingMessageLane) {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		if len(lane.msgs) == 0 {
			lane.running = false
			// Only remove the lane if it is still the registered one.
			if q.queues[userID] == lane {
				delete(q.queues, userID)
			}
			q.mu.Unlock()
			return
		}
		msg := lane.msgs[0]
		lane.msgs = lane.msgs[1:]
		q.mu.Unlock()

		if q.process != nil {
			q.process(msg)
		}
	}
}
