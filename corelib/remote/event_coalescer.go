package remote

import (
	"sync"
	"time"
)

// EventCoalescer buffers ImportantEvent items for a short window, merging
// tool_use events that complete quickly (tool_use followed by tool_result)
// into a single consolidated event before forwarding to Hub/IM.
type EventCoalescer struct {
	mu      sync.Mutex
	window  time.Duration
	pending map[string]*pendingEvent // keyed by event_id
	flushFn func([]ImportantEvent)
	timers  map[string]*time.Timer
	closed  bool
}

type pendingEvent struct {
	event ImportantEvent
}

// NewEventCoalescer creates a coalescer with the given window duration.
// The flush callback is invoked (possibly from a timer goroutine) with
// the batch of events that should be sent to Hub.
func NewEventCoalescer(window time.Duration, flush func([]ImportantEvent)) *EventCoalescer {
	if window <= 0 {
		window = 300 * time.Millisecond
	}
	return &EventCoalescer{
		window:  window,
		pending: make(map[string]*pendingEvent),
		flushFn: flush,
		timers:  make(map[string]*time.Timer),
	}
}

// Enqueue adds an event to the coalescer. If the event is a tool_use
// type, it is held for up to `window` duration. Any other event type
// is forwarded immediately.
func (c *EventCoalescer) Enqueue(event ImportantEvent) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}

	// Only buffer tool_use events; everything else goes through immediately.
	if event.Type != "tool_use" {
		c.mu.Unlock()
		c.flushFn([]ImportantEvent{event})
		return
	}

	// If there's already a pending event with the same ID, stop its timer.
	if old, exists := c.timers[event.EventID]; exists {
		old.Stop()
	}

	c.pending[event.EventID] = &pendingEvent{
		event: event,
	}

	t := time.AfterFunc(c.window, func() {
		c.flushEvent(event.EventID)
	})
	c.timers[event.EventID] = t
	c.mu.Unlock()
}

// CompleteToolCall is called when a tool_result arrives for a pending
// tool_use event. If the tool_use is still buffered, the event is
// enriched with a completion marker and flushed immediately.
func (c *EventCoalescer) CompleteToolCall(eventID string) {
	c.mu.Lock()
	pe, ok := c.pending[eventID]
	if !ok {
		c.mu.Unlock()
		return
	}

	if t, ok := c.timers[eventID]; ok {
		t.Stop()
		delete(c.timers, eventID)
	}

	pe.event.Grouped = true
	pe.event.Summary = pe.event.Summary + " [done]"
	evt := pe.event
	delete(c.pending, eventID)
	c.mu.Unlock()

	c.flushFn([]ImportantEvent{evt})
}

// flushEvent sends a single pending event after the window expires.
func (c *EventCoalescer) flushEvent(eventID string) {
	c.mu.Lock()
	pe, ok := c.pending[eventID]
	if !ok {
		c.mu.Unlock()
		return
	}
	delete(c.pending, eventID)
	delete(c.timers, eventID)
	evt := pe.event
	c.mu.Unlock()

	c.flushFn([]ImportantEvent{evt})
}

// Flush forces all pending events to be sent immediately.
func (c *EventCoalescer) Flush() {
	c.mu.Lock()
	var batch []ImportantEvent
	for id, pe := range c.pending {
		batch = append(batch, pe.event)
		if t, ok := c.timers[id]; ok {
			t.Stop()
		}
	}
	c.pending = make(map[string]*pendingEvent)
	c.timers = make(map[string]*time.Timer)
	c.mu.Unlock()

	if len(batch) > 0 {
		c.flushFn(batch)
	}
}

// Close stops all timers and flushes remaining events.
func (c *EventCoalescer) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.Flush()
}
