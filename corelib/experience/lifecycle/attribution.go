package lifecycle

import (
	"context"
	"strings"
	"sync"
	"time"
)

type ProviderResolver func() Provider

type AttributingEventSink struct {
	Sink     EventSink
	Provider Provider
	Resolve  ProviderResolver
	Window   int

	mu         sync.Mutex
	attributed map[string]struct{}
}

func (s *AttributingEventSink) RecordExperienceEvent(event Event) {
	if s == nil {
		return
	}
	event = event.WithDefaults(time.Now)
	downstream := s.Sink
	if downstream != nil {
		downstream.RecordExperienceEvent(event)
	}
	updates := s.utilityUpdatesForEvent(event)
	if len(updates) == 0 {
		return
	}
	provider := s.utilityProvider()
	if provider == nil {
		return
	}
	for _, update := range updates {
		_ = provider.UpdateUtility(context.Background(), update)
	}
}

func (s *AttributingEventSink) utilityProvider() Provider {
	if s.Resolve != nil {
		return s.Resolve()
	}
	return s.Provider
}

func (s *AttributingEventSink) utilityUpdatesForEvent(event Event) []UtilityUpdate {
	if strings.TrimSpace(event.TraceID) == "" {
		return nil
	}
	if !isAttributionOutcomeEvent(event) {
		return nil
	}
	injected, ok := s.lastInjectedEvent(event)
	if !ok || len(injected.EntryIDs) == 0 {
		return nil
	}
	key := attributionKey(event, injected)
	s.mu.Lock()
	if s.attributed == nil {
		s.attributed = make(map[string]struct{})
	}
	if _, exists := s.attributed[key]; exists {
		s.mu.Unlock()
		return nil
	}
	s.attributed[key] = struct{}{}
	s.mu.Unlock()

	helpful, harmful, success := attributionOutcomeFlags(event)
	if !helpful && !harmful && !success {
		return nil
	}
	updates := make([]UtilityUpdate, 0, len(injected.EntryIDs))
	seen := map[string]struct{}{}
	for _, entryID := range injected.EntryIDs {
		entryID = strings.TrimSpace(entryID)
		if entryID == "" {
			continue
		}
		if _, exists := seen[entryID]; exists {
			continue
		}
		seen[entryID] = struct{}{}
		updates = append(updates, UtilityUpdate{
			EntryID:      entryID,
			TraceID:      event.TraceID,
			Helpful:      helpful,
			Harmful:      harmful,
			Success:      success,
			TokenDelta:   -injected.TokenCost,
			Reason:       "outcome_attribution:" + string(event.EventType),
			EvidenceType: string(event.EventType),
		})
	}
	return updates
}

func (s *AttributingEventSink) lastInjectedEvent(outcome Event) (Event, bool) {
	trail, ok := s.Sink.(*EventTrail)
	if !ok || trail == nil {
		return Event{}, false
	}
	window := s.Window
	if window <= 0 {
		window = 128
	}
	events := trail.Latest(window)
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.EventType != EventExperienceInjected || event.TraceID != outcome.TraceID || len(event.EntryIDs) == 0 {
			continue
		}
		if !outcome.CreatedAt.IsZero() && !event.CreatedAt.IsZero() && event.CreatedAt.After(outcome.CreatedAt) {
			continue
		}
		return event, true
	}
	return Event{}, false
}

func isAttributionOutcomeEvent(event Event) bool {
	switch event.EventType {
	case EventTaskSucceeded, EventTaskFailed, EventToolCallFinished, EventWorkflowPhaseCompleted, EventUserFeedbackReceived:
		return true
	default:
		return false
	}
}

func attributionOutcomeFlags(event Event) (helpful bool, harmful bool, success bool) {
	switch event.EventType {
	case EventTaskSucceeded, EventWorkflowPhaseCompleted:
		return true, false, true
	case EventTaskFailed:
		return false, true, false
	case EventUserFeedbackReceived:
		outcome := strings.ToLower(strings.TrimSpace(event.Outcome))
		switch outcome {
		case "confirm", "confirmed", "approve", "approved":
			return true, false, true
		case "supplement", "modify", "revision_requested", "cancel", "cancelled", "switch_task":
			return false, true, false
		default:
			return false, false, false
		}
	case EventToolCallFinished:
		outcome := strings.ToLower(strings.TrimSpace(event.Outcome))
		if outcome == "success" || outcome == "succeeded" || outcome == "ok" || outcome == "passed" {
			return true, false, true
		}
		if outcome == "failure" || outcome == "failed" || outcome == "error" || event.ErrorClass != "" {
			return false, true, false
		}
	}
	return false, false, false
}

func attributionKey(outcome Event, injected Event) string {
	return strings.Join([]string{
		outcome.TraceID,
		string(outcome.EventType),
		outcome.CreatedAt.Format(time.RFC3339Nano),
		injected.CreatedAt.Format(time.RFC3339Nano),
		strings.Join(injected.EntryIDs, ","),
	}, "|")
}
