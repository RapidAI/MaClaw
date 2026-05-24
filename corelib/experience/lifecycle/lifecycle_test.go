package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEntryTypeValid(t *testing.T) {
	if !EntryTypeComparativeSkill.Valid() {
		t.Fatal("comparative skill should be a first-class experience type")
	}
	if NormalizeEntryType("unknown") != "" {
		t.Fatal("unknown entry type should not normalize to a valid type")
	}
}

func TestEventTrailRecordsBoundedCopies(t *testing.T) {
	trail := NewEventTrail(2)
	trail.now = func() time.Time { return time.Unix(123, 0).UTC() }

	trail.RecordExperienceEvent(Event{EventType: EventExperienceRetrieved, EntryIDs: []string{"a", "a", "b"}})
	trail.RecordExperienceEvent(Event{EventType: EventExperienceInjected, EntryIDs: []string{"c"}})
	trail.RecordExperienceEvent(Event{EventType: EventTaskSucceeded})

	events := trail.List()
	if len(events) != 2 {
		t.Fatalf("expected bounded trail length 2, got %d", len(events))
	}
	if events[0].EventType != EventExperienceInjected || events[1].EventType != EventTaskSucceeded {
		t.Fatalf("unexpected retained events: %+v", events)
	}
	if events[0].CreatedAt.IsZero() || !events[1].CreatedAt.Equal(time.Unix(123, 0).UTC()) {
		t.Fatalf("expected default timestamps, got %+v", events)
	}
	events[0].EntryIDs[0] = "mutated"
	if trail.List()[0].EntryIDs[0] == "mutated" {
		t.Fatal("List should return copies")
	}
}

func TestEventContextAppliesOnlyMissingIDs(t *testing.T) {
	ctx := EventContext{TraceID: "trace-a", TaskID: "task-a"}
	event := ctx.Apply(Event{EventType: EventExperienceInjected})
	if event.TraceID != "trace-a" || event.TaskID != "task-a" {
		t.Fatalf("expected context ids, got %+v", event)
	}
	preserved := ctx.Apply(Event{TraceID: "trace-b", TaskID: "task-b", EventType: EventExperienceRetrieved})
	if preserved.TraceID != "trace-b" || preserved.TaskID != "task-b" {
		t.Fatalf("expected event ids to win, got %+v", preserved)
	}
}

func TestDefaultRetrievalPolicyDecidesFromGoal(t *testing.T) {
	decision := DefaultRetrievalPolicy{}.Decide(context.Background(), RetrievalPolicyInput{
		TraceID:        "trace-1",
		TaskID:         "task-1",
		CurrentGoal:    " use project memory ",
		TokenBudget:    512,
		MissingSignals: []string{"max_entries:3"},
	})
	if !decision.ShouldRetrieve || decision.Query != "use project memory" || decision.Mode != RetrievalModeAuto {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if decision.Budget.MaxEntries != 3 || decision.Budget.MaxTokens != 512 {
		t.Fatalf("unexpected budget: %+v", decision.Budget)
	}
	if len(decision.Types) != 5 || decision.Budget.Quotas[EntryTypeComparativeSkill] != 1 {
		t.Fatalf("expected balanced default types/quotas, got %+v", decision)
	}

	skip := DefaultRetrievalPolicy{}.Decide(context.Background(), RetrievalPolicyInput{})
	if skip.ShouldRetrieve || skip.Reason != "empty_goal" {
		t.Fatalf("expected empty goal to skip retrieval, got %+v", skip)
	}
}

func TestSelectBalancedCandidatesUsesQuotaRerankAndRedundancy(t *testing.T) {
	candidates := []Candidate{
		{Entry: Entry{ID: "fact-low", EntryType: EntryTypeFactual, Content: "API endpoint is https://api.example.com"}, Relevance: 0.9, PriorityScore: 0.1, TokenCost: 20},
		{Entry: Entry{ID: "fact-high", EntryType: EntryTypeFactual, Content: "API endpoint is https://api.example.com"}, Relevance: 0.9, PriorityScore: 1.0, TokenCost: 20},
		{Entry: Entry{ID: "failure", EntryType: EntryTypeFailureSkill, Content: "Avoid deploy retry after timeout"}, Relevance: 0.7, PriorityScore: 0.2, TokenCost: 20},
		{Entry: Entry{ID: "backup", EntryType: EntryTypeFactual, Content: "PostgreSQL backup window is Sunday"}, Relevance: 0.6, TokenCost: 20},
	}
	decision := RetrievalDecision{
		ShouldRetrieve: true,
		Types:          []EntryType{EntryTypeFailureSkill, EntryTypeFactual},
		Budget: RetrievalBudget{MaxEntries: 3, Quotas: map[EntryType]int{
			EntryTypeFailureSkill: 1,
			EntryTypeFactual:      2,
		}},
	}

	selected := SelectBalancedCandidates(candidates, decision)
	if len(selected) != 3 {
		t.Fatalf("expected 3 selected candidates, got %+v", selected)
	}
	if selected[0].Entry.ID != "failure" || selected[1].Entry.ID != "fact-high" || selected[2].Entry.ID != "backup" {
		t.Fatalf("unexpected selected order: %+v", selected)
	}
}

type testProvider struct {
	entries    []Entry
	candidates []Candidate
	err        error
	updates    int
	updateLog  []UtilityUpdate
}

func (p *testProvider) ListExperience(context.Context, Scope) ([]Entry, error) {
	return p.entries, p.err
}

func (p *testProvider) SearchExperience(context.Context, Query) ([]Candidate, error) {
	return p.candidates, p.err
}

func (p *testProvider) UpdateUtility(_ context.Context, update UtilityUpdate) error {
	p.updates++
	p.updateLog = append(p.updateLog, update)
	return p.err
}

func TestCompositeProviderAggregatesProviders(t *testing.T) {
	a := &testProvider{entries: []Entry{{ID: "a"}}, candidates: []Candidate{{Entry: Entry{ID: "ca"}}}}
	b := &testProvider{entries: []Entry{{ID: "b"}}, candidates: []Candidate{{Entry: Entry{ID: "cb"}}}}
	provider := NewCompositeProvider(a, nil, b)

	entries, err := provider.ListExperience(context.Background(), Scope{})
	if err != nil || len(entries) != 2 || entries[0].ID != "a" || entries[1].ID != "b" {
		t.Fatalf("unexpected list result entries=%+v err=%v", entries, err)
	}
	candidates, err := provider.SearchExperience(context.Background(), Query{})
	if err != nil || len(candidates) != 2 || candidates[0].Entry.ID != "ca" || candidates[1].Entry.ID != "cb" {
		t.Fatalf("unexpected search result candidates=%+v err=%v", candidates, err)
	}
	if err := provider.UpdateUtility(context.Background(), UtilityUpdate{EntryID: "x"}); err != nil {
		t.Fatalf("unexpected update err: %v", err)
	}
	if a.updates != 1 || b.updates != 1 {
		t.Fatalf("expected update fanout, got a=%d b=%d", a.updates, b.updates)
	}
}

func TestCompositeProviderDoesNotTruncateSearchBeforeSelection(t *testing.T) {
	provider := NewCompositeProvider(
		&testProvider{candidates: []Candidate{{Entry: Entry{ID: "memory-a"}}, {Entry: Entry{ID: "memory-b"}}}},
		&testProvider{candidates: []Candidate{{Entry: Entry{ID: "skill-a"}}}},
	)
	candidates, err := provider.SearchExperience(context.Background(), Query{Limit: 1})
	if err != nil {
		t.Fatalf("unexpected search err: %v", err)
	}
	if len(candidates) != 3 || candidates[2].Entry.ID != "skill-a" {
		t.Fatalf("expected composite search to preserve downstream provider candidates, got %+v", candidates)
	}
}

func TestCompositeProviderReturnsPartialResultsWithError(t *testing.T) {
	boom := errors.New("boom")
	provider := NewCompositeProvider(
		&testProvider{entries: []Entry{{ID: "ok"}}, candidates: []Candidate{{Entry: Entry{ID: "ok"}}}},
		&testProvider{err: boom},
	)
	entries, err := provider.ListExperience(context.Background(), Scope{})
	if err == nil || len(entries) != 1 || entries[0].ID != "ok" {
		t.Fatalf("expected partial list with error, entries=%+v err=%v", entries, err)
	}
}

func TestAttributingEventSinkUpdatesInjectedEntriesOnSuccess(t *testing.T) {
	trail := NewEventTrail(8)
	provider := &testProvider{}
	sink := &AttributingEventSink{Sink: trail, Provider: provider}

	sink.RecordExperienceEvent(Event{TraceID: "trace-1", EventType: EventExperienceInjected, EntryIDs: []string{"a", "b", "a"}, Query: "verify", TokenCost: 20, CreatedAt: time.Unix(10, 0)})
	sink.RecordExperienceEvent(Event{TraceID: "trace-1", EventType: EventTaskSucceeded, Outcome: "success", CreatedAt: time.Unix(12, 0)})

	if provider.updates != 2 {
		t.Fatalf("expected one utility update per unique injected entry, got %d %+v", provider.updates, provider.updateLog)
	}
	if provider.updateLog[0].EntryID != "a" || !provider.updateLog[0].Helpful || !provider.updateLog[0].Success || provider.updateLog[0].TokenDelta != -20 {
		t.Fatalf("unexpected first update: %+v", provider.updateLog[0])
	}
	if provider.updateLog[1].EntryID != "b" || provider.updateLog[1].Reason != "outcome_attribution:task_succeeded" {
		t.Fatalf("unexpected second update: %+v", provider.updateLog[1])
	}
}

func TestAttributingEventSinkRequiresTraceMatch(t *testing.T) {
	trail := NewEventTrail(8)
	provider := &testProvider{}
	sink := &AttributingEventSink{Sink: trail, Provider: provider}

	sink.RecordExperienceEvent(Event{TraceID: "trace-1", EventType: EventExperienceInjected, EntryIDs: []string{"a"}, CreatedAt: time.Unix(10, 0)})
	sink.RecordExperienceEvent(Event{TraceID: "trace-2", EventType: EventTaskFailed, Outcome: "failure", CreatedAt: time.Unix(12, 0)})
	sink.RecordExperienceEvent(Event{EventType: EventTaskSucceeded, Outcome: "success", CreatedAt: time.Unix(13, 0)})

	if provider.updates != 0 {
		t.Fatalf("expected no cross-trace or empty-trace attribution, got %+v", provider.updateLog)
	}
}

func TestAttributingEventSinkUpdatesInjectedEntriesOnToolFailure(t *testing.T) {
	trail := NewEventTrail(8)
	provider := &testProvider{}
	sink := &AttributingEventSink{Sink: trail, Provider: provider}

	sink.RecordExperienceEvent(Event{TraceID: "trace-1", EventType: EventExperienceInjected, EntryIDs: []string{"failure-skill"}, CreatedAt: time.Unix(10, 0)})
	sink.RecordExperienceEvent(Event{TraceID: "trace-1", EventType: EventToolCallFinished, Outcome: "failure", ErrorClass: "timeout", CreatedAt: time.Unix(11, 0)})

	if provider.updates != 1 || !provider.updateLog[0].Harmful || provider.updateLog[0].Success {
		t.Fatalf("expected harmful tool failure attribution, got %+v", provider.updateLog)
	}
}

func TestAttributingEventSinkUpdatesInjectedEntriesOnReviewFeedback(t *testing.T) {
	trail := NewEventTrail(8)
	provider := &testProvider{}
	sink := &AttributingEventSink{Sink: trail, Provider: provider}

	sink.RecordExperienceEvent(Event{TraceID: "trace-1", EventType: EventExperienceInjected, EntryIDs: []string{"workflow-tip"}, CreatedAt: time.Unix(10, 0)})
	sink.RecordExperienceEvent(Event{TraceID: "trace-1", EventType: EventUserFeedbackReceived, Outcome: "supplement", Reason: "requirements", CreatedAt: time.Unix(11, 0)})

	if provider.updates != 1 || !provider.updateLog[0].Harmful || provider.updateLog[0].Helpful || provider.updateLog[0].Success {
		t.Fatalf("expected harmful review supplement attribution, got %+v", provider.updateLog)
	}
	if provider.updateLog[0].Reason != "outcome_attribution:user_feedback_received" {
		t.Fatalf("unexpected attribution reason: %+v", provider.updateLog[0])
	}
}
