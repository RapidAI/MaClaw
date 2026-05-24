package lifecycle

import (
	"sync"
	"time"
)

type EventSink interface {
	RecordExperienceEvent(Event)
}

type EventSinkFunc func(Event)

func (f EventSinkFunc) RecordExperienceEvent(event Event) {
	if f != nil {
		f(event)
	}
}

type NoopEventSink struct{}

func (NoopEventSink) RecordExperienceEvent(Event) {}

type EventTrail struct {
	mu     sync.RWMutex
	limit  int
	now    func() time.Time
	events []Event
}

func NewEventTrail(limit int) *EventTrail {
	if limit <= 0 {
		limit = 256
	}
	return &EventTrail{limit: limit, now: time.Now}
}

func (t *EventTrail) RecordExperienceEvent(event Event) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	event = event.WithDefaults(t.now)
	t.events = append(t.events, event)
	if len(t.events) > t.limit {
		excess := len(t.events) - t.limit
		t.events = t.events[excess:]
	}
}

func (t *EventTrail) List() []Event {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Event, len(t.events))
	for i, event := range t.events {
		out[i] = cloneEvent(event)
	}
	return out
}

func (t *EventTrail) Latest(limit int) []Event {
	if t == nil || limit == 0 {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if limit < 0 || limit > len(t.events) {
		limit = len(t.events)
	}
	start := len(t.events) - limit
	out := make([]Event, 0, limit)
	for _, event := range t.events[start:] {
		out = append(out, cloneEvent(event))
	}
	return out
}

func cloneEvent(event Event) Event {
	event.EntryIDs = append([]string(nil), event.EntryIDs...)
	return event
}
