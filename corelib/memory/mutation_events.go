package memory

import (
	"reflect"
	"sync"
	"time"
)

type MutationKind string

const (
	MutationCreate    MutationKind = "create"
	MutationUpdate    MutationKind = "update"
	MutationSupersede MutationKind = "supersede"
	MutationDelete    MutationKind = "delete"
	MutationReset     MutationKind = "reset"
)

type MutationEvent struct {
	MutationID string
	OwnerID    string
	Kind       MutationKind
	Origin     string
	Before     *Entry
	After      *Entry
}

type mutationSubscriberSet struct {
	mu      sync.RWMutex
	nextID  uint64
	entries map[uint64]func(MutationEvent)
}

func (s *Store) SubscribeMutations(subscriber func(MutationEvent)) func() {
	if s == nil || subscriber == nil {
		return func() {}
	}
	s.mu.Lock()
	if s.mutationSubs == nil {
		s.mutationSubs = &mutationSubscriberSet{entries: make(map[uint64]func(MutationEvent))}
	}
	subs := s.mutationSubs
	s.mu.Unlock()

	subs.mu.Lock()
	subs.nextID++
	id := subs.nextID
	subs.entries[id] = subscriber
	subs.mu.Unlock()
	return func() {
		subs.mu.Lock()
		delete(subs.entries, id)
		subs.mu.Unlock()
	}
}

func mutationEventFor(before, after *Entry, origin string) (MutationEvent, bool) {
	if before == nil && after == nil {
		return MutationEvent{}, false
	}
	if before != nil && after != nil && memoryViewEntriesEqual(*before, *after) {
		return MutationEvent{}, false
	}
	event := MutationEvent{MutationID: generateID(), Origin: origin}
	if before != nil {
		beforeCopy := cloneMutationEntry(*before)
		event.Before = &beforeCopy
		event.OwnerID = before.OwnerID
		if event.Origin == "" {
			event.Origin = before.SourceType
		}
	}
	if after != nil {
		afterCopy := cloneMutationEntry(*after)
		event.After = &afterCopy
		event.OwnerID = after.OwnerID
		if event.Origin == "" {
			event.Origin = after.SourceType
		}
	}
	switch {
	case before == nil:
		event.Kind = MutationCreate
	case after == nil:
		event.Kind = MutationDelete
	case before.Status != StatusSuperseded && after.Status == StatusSuperseded:
		event.Kind = MutationSupersede
	default:
		event.Kind = MutationUpdate
	}
	return event, true
}

func memoryViewEntriesEqual(a, b Entry) bool {
	return reflect.DeepEqual(memoryViewProjection(a), memoryViewProjection(b))
}

func memoryViewProjection(entry Entry) Entry {
	entry.Embedding = nil
	entry.AccessCount = 0
	entry.Strength = 0
	entry.UpdatedAt = time.Time{}
	entry.Versions = nil
	entry.Version = 0
	return entry
}

func cloneMutationEntry(entry Entry) Entry {
	entry.Tags = append([]string(nil), entry.Tags...)
	entry.RelatedIDs = append([]string(nil), entry.RelatedIDs...)
	entry.RelatedEdges = append([]RelatedEdge(nil), entry.RelatedEdges...)
	entry.EvidenceIDs = append([]string(nil), entry.EvidenceIDs...)
	entry.Entities = append([]string(nil), entry.Entities...)
	entry.ChildIDs = append([]string(nil), entry.ChildIDs...)
	entry.Versions = append([]VersionSnapshot(nil), entry.Versions...)
	entry.Boundary = cloneMemoryBoundary(entry.Boundary)
	entry.Interval = cloneTimeInterval(entry.Interval)
	entry.ValidAt = cloneTimePtr(entry.ValidAt)
	entry.InvalidAt = cloneTimePtr(entry.InvalidAt)
	entry.Stability = cloneStabilityMeta(entry.Stability)
	return entry
}

func (s *Store) publishMutations(events []MutationEvent) {
	if s == nil || len(events) == 0 {
		return
	}
	s.mu.RLock()
	subs := s.mutationSubs
	s.mu.RUnlock()
	if subs == nil {
		return
	}
	subs.mu.RLock()
	subscribers := make([]func(MutationEvent), 0, len(subs.entries))
	for _, subscriber := range subs.entries {
		subscribers = append(subscribers, subscriber)
	}
	subs.mu.RUnlock()
	for _, event := range events {
		for _, subscriber := range subscribers {
			subscriber(event)
		}
	}
}

func (s *Store) publishMutation(before, after *Entry, origin string) {
	if event, ok := mutationEventFor(before, after, origin); ok {
		s.publishMutations([]MutationEvent{event})
	}
}

func (s *Store) publishInsertedEntry(entry Entry) {
	if stored := s.SearchDirectByID(entry.ID); len(stored) == 1 {
		s.publishMutation(nil, &stored[0], entry.SourceType)
	}
}

func (s *Store) publishReset(origin string) {
	s.publishMutations([]MutationEvent{{
		MutationID: generateID(),
		Kind:       MutationReset,
		Origin:     origin,
	}})
}
