package eventbus

// Event Bus: typed domain event publish/subscribe system.
// Inspired by OpenHuman's core/event_bus/ — decouples modules via events
// instead of direct function calls and sync.Map state passing.
//
// Usage:
//   bus := eventbus.New()
//   ch := bus.Subscribe("memory")
//   go func() { for evt := range ch { handle(evt) } }()
//   bus.Publish(MemoryEvent{Kind: "saved", EntryID: "..."})
//   bus.Unsubscribe("memory", ch)
//
// Thread-safe. Non-blocking publish (drops events if subscriber is slow).

import (
	"sync"
)

// Event is the interface all domain events must implement.
type Event interface {
	Domain() string // "memory" | "agent" | "workflow" | "tool" | "session"
	Type() string   // "saved" | "recalled" | "started" | "completed" | "failed"
}

// Bus is a simple typed event bus with domain-based routing.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
	bufferSize  int
}

// New creates a new event bus with default buffer size.
func New() *Bus {
	return &Bus{
		subscribers: make(map[string][]chan Event),
		bufferSize:  64,
	}
}

// NewWithBuffer creates a bus with a custom channel buffer size.
func NewWithBuffer(size int) *Bus {
	if size <= 0 {
		size = 64
	}
	return &Bus{
		subscribers: make(map[string][]chan Event),
		bufferSize:  size,
	}
}

// Publish sends an event to all subscribers of its domain.
// Non-blocking: if a subscriber's channel is full, the event is dropped silently.
// This prevents slow subscribers from blocking publishers.
func (b *Bus) Publish(evt Event) {
	if b == nil || evt == nil {
		return
	}
	domain := evt.Domain()

	b.mu.RLock()
	subs := b.subscribers[domain]
	// Also notify wildcard subscribers
	wildcards := b.subscribers["*"]
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			// Drop silently — subscriber is too slow
		}
	}
	for _, ch := range wildcards {
		select {
		case ch <- evt:
		default:
		}
	}
}

// Subscribe creates a channel that receives events for the given domain.
// Use "*" to subscribe to all domains.
func (b *Bus) Subscribe(domain string) <-chan Event {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return ch
	}
	ch := make(chan Event, b.bufferSize)
	b.mu.Lock()
	b.subscribers[domain] = append(b.subscribers[domain], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel from a domain's subscriber list and closes it.
func (b *Bus) Unsubscribe(domain string, ch <-chan Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[domain]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[domain] = append(subs[:i], subs[i+1:]...)
			close(sub)
			return
		}
	}
}

// SubscriberCount returns the number of subscribers for a domain.
func (b *Bus) SubscriberCount(domain string) int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[domain])
}

// Close closes all subscriber channels and clears the bus.
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for domain, subs := range b.subscribers {
		for _, ch := range subs {
			close(ch)
		}
		delete(b.subscribers, domain)
	}
}

// --- Common Event Types ---

// GenericEvent is a simple event implementation for quick prototyping.
type GenericEvent struct {
	EventDomain string
	EventType   string
	Payload     interface{}
}

func (e GenericEvent) Domain() string { return e.EventDomain }
func (e GenericEvent) Type() string   { return e.EventType }

// MemoryEvent represents memory store operations.
type MemoryEvent struct {
	Kind    string // "saved" | "recalled" | "pruned" | "consolidated"
	EntryID string
	Content string
}

func (e MemoryEvent) Domain() string { return "memory" }
func (e MemoryEvent) Type() string   { return e.Kind }

// ToolEvent represents tool execution events.
type ToolEvent struct {
	Kind     string // "executed" | "failed" | "blocked" | "learned"
	ToolName string
	Args     string
	Result   string
}

func (e ToolEvent) Domain() string { return "tool" }
func (e ToolEvent) Type() string   { return e.Kind }

// WorkflowEvent represents workflow state changes.
type WorkflowEvent struct {
	Kind     string // "started" | "phase_changed" | "completed" | "cancelled"
	UserID   string
	Workflow string
	Phase    string
}

func (e WorkflowEvent) Domain() string { return "workflow" }
func (e WorkflowEvent) Type() string   { return e.Kind }

// AgentEvent represents agent loop events.
type AgentEvent struct {
	Kind   string // "loop_started" | "loop_ended" | "drift_detected" | "cost_warning"
	UserID string
	Detail string
}

func (e AgentEvent) Domain() string { return "agent" }
func (e AgentEvent) Type() string   { return e.Kind }
