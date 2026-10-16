package counterfactual

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

type recordingProvider struct {
	updates []lifecycle.UtilityUpdate
}

func (p *recordingProvider) ListExperience(context.Context, lifecycle.Scope) ([]lifecycle.Entry, error) {
	return nil, nil
}

func (p *recordingProvider) SearchExperience(context.Context, lifecycle.Query) ([]lifecycle.Candidate, error) {
	return nil, nil
}

func (p *recordingProvider) UpdateUtility(_ context.Context, update lifecycle.UtilityUpdate) error {
	p.updates = append(p.updates, update)
	return nil
}

func pairEvents() []lifecycle.Event {
	at := time.Unix(100, 0).UTC()
	return []lifecycle.Event{
		// Branch A: retrieval injected, task succeeded.
		{TraceID: "trace-a1", TaskID: "task-x", EventType: lifecycle.EventExperienceInjected, EntryIDs: []string{"skill-1"}, TokenCost: 30, CreatedAt: at},
		{TraceID: "trace-a1", TaskID: "task-x", EventType: lifecycle.EventToolCallFinished, ToolName: "read_file", StepIndex: 0, Outcome: "success", CreatedAt: at.Add(time.Second)},
		{TraceID: "trace-a1", TaskID: "task-x", EventType: lifecycle.EventTaskSucceeded, Outcome: "success", CreatedAt: at.Add(2 * time.Second)},
		// Branch B: retrieval suppressed, task failed.
		{TraceID: "trace-b1", TaskID: "task-x", EventType: lifecycle.EventToolCallFinished, ToolName: "read_file", StepIndex: 0, Outcome: "failure", ErrorClass: "not_found", CreatedAt: at.Add(3 * time.Second)},
		{TraceID: "trace-b1", TaskID: "task-x", EventType: lifecycle.EventTaskFailed, Outcome: "failure", ErrorClass: "not_found", CreatedAt: at.Add(4 * time.Second)},
	}
}

func TestEvaluatorHelpfulVerdictWritesBackUtility(t *testing.T) {
	provider := &recordingProvider{}
	trail := lifecycle.NewEventTrail(8)
	evaluator := &Evaluator{
		Opts:     Options{Now: func() time.Time { return time.Unix(200, 0).UTC() }},
		Provider: provider,
		Sink:     trail,
	}

	evidence := evaluator.Evaluate(context.Background(), pairEvents())
	if len(evidence) != 1 {
		t.Fatalf("expected one evidence record, got %+v", evidence)
	}
	ev := evidence[0]
	if ev.Verdict != lifecycle.CounterfactualRetrievalHelpful {
		t.Fatalf("unexpected verdict: %+v", ev)
	}
	if ev.Mode != "offline_event_stream" || ev.TraceID != "trace-a1" || ev.PairKey != "task-x" {
		t.Fatalf("unexpected evidence metadata: %+v", ev)
	}
	if !ev.WithRetrieval.Succeeded || ev.WithRetrieval.Steps != 1 || ev.WithRetrieval.Tokens != 30 {
		t.Fatalf("unexpected with-retrieval outcome: %+v", ev.WithRetrieval)
	}
	if ev.WithoutRetrieval.Succeeded || ev.WithoutRetrieval.ErrorClass != "not_found" {
		t.Fatalf("unexpected without-retrieval outcome: %+v", ev.WithoutRetrieval)
	}
	if len(provider.updates) != 1 || !provider.updates[0].Helpful || !provider.updates[0].Success || provider.updates[0].EntryID != "skill-1" {
		t.Fatalf("expected helpful utility writeback, got %+v", provider.updates)
	}
	if provider.updates[0].EvidenceType != string(lifecycle.EventCounterfactualEvaluated) {
		t.Fatalf("unexpected evidence type: %+v", provider.updates[0])
	}
	events := trail.List()
	if len(events) != 1 || events[0].EventType != lifecycle.EventCounterfactualEvaluated || events[0].Outcome != "retrieval_helpful" {
		t.Fatalf("expected counterfactual_evaluated trail event, got %+v", events)
	}
}

func TestEvaluatorSideEffectTracesDefaultNotEvaluable(t *testing.T) {
	events := pairEvents()
	// Branch A now touches a side-effecting tool; without sandbox/dry-run the
	// pair must be rejected and produce no utility writeback.
	events[1].ToolName = "bash"

	provider := &recordingProvider{}
	evaluator := &Evaluator{Provider: provider}
	evidence := evaluator.Evaluate(context.Background(), events)
	if len(evidence) != 1 || evidence[0].Verdict != lifecycle.CounterfactualNotEvaluable || evidence[0].Reason != "side_effect_without_sandbox" {
		t.Fatalf("expected not_evaluable side-effect gate, got %+v", evidence)
	}
	if len(provider.updates) != 0 {
		t.Fatalf("expected no writeback for non-evaluable pair, got %+v", provider.updates)
	}

	// Sandbox/dry-run assertion lifts the gate.
	sandboxed := &Evaluator{Opts: Options{SandboxDryRun: true}}
	evidence = sandboxed.Evaluate(context.Background(), events)
	if len(evidence) != 1 || evidence[0].Verdict != lifecycle.CounterfactualRetrievalHelpful {
		t.Fatalf("expected sandboxed pair to evaluate, got %+v", evidence)
	}
}

func TestEvaluatorHarmfulAndUnpairedTraces(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	events := []lifecycle.Event{
		// Harmful: injected branch fails, suppressed branch succeeds.
		{TraceID: "trace-a", TaskID: "task-y", EventType: lifecycle.EventExperienceInjected, EntryIDs: []string{"stale-skill"}, CreatedAt: at},
		{TraceID: "trace-a", TaskID: "task-y", EventType: lifecycle.EventTaskFailed, Outcome: "failure", CreatedAt: at.Add(time.Second)},
		{TraceID: "trace-b", TaskID: "task-y", EventType: lifecycle.EventTaskSucceeded, Outcome: "success", CreatedAt: at.Add(2 * time.Second)},
		// Unpaired: only a suppressed trace exists for task-z — no evidence.
		{TraceID: "trace-c", TaskID: "task-z", EventType: lifecycle.EventTaskSucceeded, Outcome: "success", CreatedAt: at.Add(3 * time.Second)},
		// No trace id: skipped.
		{EventType: lifecycle.EventTaskSucceeded, Outcome: "success", CreatedAt: at.Add(4 * time.Second)},
	}
	provider := &recordingProvider{}
	evaluator := &Evaluator{Provider: provider}
	evidence := evaluator.Evaluate(context.Background(), events)
	if len(evidence) != 1 || evidence[0].Verdict != lifecycle.CounterfactualRetrievalHarmful {
		t.Fatalf("expected one harmful verdict, got %+v", evidence)
	}
	if len(provider.updates) != 1 || !provider.updates[0].Harmful || provider.updates[0].Helpful {
		t.Fatalf("expected harmful utility writeback, got %+v", provider.updates)
	}
}

func TestEvaluateCaseChecksExpectedVerdicts(t *testing.T) {
	dc := DatasetCase{
		Name:           "helpful-pair",
		Events:         pairEvents(),
		ExpectVerdicts: map[string]string{"task-x": "retrieval_helpful"},
	}
	if _, err := EvaluateCase(context.Background(), dc); err != nil {
		t.Fatalf("unexpected dataset error: %v", err)
	}
	dc.ExpectVerdicts = map[string]string{"task-x": "retrieval_harmful"}
	if _, err := EvaluateCase(context.Background(), dc); err == nil {
		t.Fatal("expected verdict mismatch error")
	}
}

func TestFilterByTraceID(t *testing.T) {
	events := pairEvents()
	got := FilterByTraceID(events, "trace-a1")
	if len(got) != 3 {
		t.Fatalf("expected 3 events for trace-a1, got %d", len(got))
	}
	if got := FilterByTraceID(events, " "); got != nil {
		t.Fatalf("expected nil for empty trace id, got %+v", got)
	}
}
