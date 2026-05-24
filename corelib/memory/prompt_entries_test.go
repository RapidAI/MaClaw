package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

type promptTestProvider struct {
	candidates []lifecycle.Candidate
}

func (p promptTestProvider) ListExperience(context.Context, lifecycle.Scope) ([]lifecycle.Entry, error) {
	return nil, nil
}

func (p promptTestProvider) SearchExperience(context.Context, lifecycle.Query) ([]lifecycle.Candidate, error) {
	return p.candidates, nil
}

func (p promptTestProvider) UpdateUtility(context.Context, lifecycle.UtilityUpdate) error {
	return nil
}

func TestFormatRecallEntriesForPrompt(t *testing.T) {
	out := FormatRecallEntriesForPrompt([]Entry{{Content: "Project uses pnpm test", Category: CategoryProjectKnowledge}}, RecallEntriesPromptOptions{
		Header: "## Memory",
		Intro:  "Use these entries.",
		Footer: "Call memory(action: recall) for more.",
	})
	for _, want := range []string{"## Memory", "Use these entries.", "project_knowledge", "pnpm test", "Call memory(action: recall)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted prompt missing %q in %q", want, out)
		}
	}
}

func TestFormatRecallEntriesForPromptEmpty(t *testing.T) {
	if got := FormatRecallEntriesForPrompt(nil, RecallEntriesPromptOptions{Header: "x"}); got != "" {
		t.Fatalf("expected empty output for no entries, got %q", got)
	}
}

func TestProactiveContextForPrompt(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "Project uses pnpm test for verification", Category: CategoryProjectKnowledge, Tags: []string{"pnpm"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	out, recalled := store.ProactiveContextForPrompt("How do I verify this project with pnpm?", ProactivePromptOptions{
		IncludeMemoryIndex: true,
		MemoryIndexLabel:   "[Index] ",
		Recall:             ProactiveRecallOptions{MaxEntries: 4, EntityLimit: 1},
		RecallEntries: RecallEntriesPromptOptions{
			Header: "## Recall",
		},
	})
	if len(recalled) == 0 {
		t.Fatalf("expected proactive recall entries")
	}
	for _, want := range []string{"[Index]", "## Recall", "pnpm test"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in proactive prompt: %q", want, out)
		}
	}
}

func TestProactiveContextForPromptEmitsInjectedEvent(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	trail := lifecycle.NewEventTrail(8)
	store.SetExperienceEventSink(trail)

	if err := store.Save(Entry{Content: "Project uses pnpm test for verification", Category: CategoryProjectKnowledge, Tags: []string{"pnpm"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	_, recalled := store.ProactiveContextForPrompt("How do I verify this project with pnpm?", ProactivePromptOptions{
		EventContext: lifecycle.EventContext{TraceID: "trace-123", TaskID: "task-456"},
		Recall:       ProactiveRecallOptions{MaxEntries: 4, EntityLimit: 1},
		RecallEntries: RecallEntriesPromptOptions{
			Header: "## Recall",
		},
	})
	if len(recalled) == 0 {
		t.Fatal("expected recalled entries")
	}

	var injected lifecycle.Event
	var decided lifecycle.Event
	for _, event := range trail.List() {
		if event.EventType == lifecycle.EventRetrievalDecided {
			decided = event
		}
		if event.EventType == lifecycle.EventExperienceInjected {
			injected = event
		}
	}
	if decided.EventType != lifecycle.EventRetrievalDecided || decided.Outcome != "retrieve" {
		t.Fatalf("expected retrieval decision event, got %+v from %+v", decided, trail.List())
	}
	if injected.EventType != lifecycle.EventExperienceInjected {
		t.Fatalf("expected injected event, got %+v", trail.List())
	}
	if injected.Reason != "proactive_prompt:auto" || injected.Query == "" || injected.TokenCost <= 0 {
		t.Fatalf("unexpected injected event metadata: %+v", injected)
	}
	if injected.TraceID != "trace-123" || injected.TaskID != "task-456" {
		t.Fatalf("expected lifecycle trace/task context, got %+v", injected)
	}
	if len(injected.EntryIDs) == 0 || injected.EntryIDs[0] != recalled[0].ID {
		t.Fatalf("expected injected entry id, got event=%+v recalled=%+v", injected, recalled[0].ID)
	}
}

func TestProactiveContextForPromptHonorsRetrievalPolicy(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: "Project uses pnpm test for verification", Category: CategoryProjectKnowledge, Tags: []string{"pnpm"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	section, recalled := store.ProactiveContextForPrompt("pnpm verification", ProactivePromptOptions{
		EventContext: lifecycle.EventContext{TraceID: "trace-skip", TaskID: "task-skip"},
		Policy: lifecycle.RetrievalPolicyFunc(func(_ context.Context, input lifecycle.RetrievalPolicyInput) lifecycle.RetrievalDecision {
			return lifecycle.RetrievalDecision{ShouldRetrieve: false, Query: input.CurrentGoal, Reason: "already_known", Mode: lifecycle.RetrievalModeAuto}
		}),
		Recall: ProactiveRecallOptions{MaxEntries: 4, EntityLimit: 1},
		RecallEntries: RecallEntriesPromptOptions{
			Header: "## Recall",
		},
	})
	if section != "" || len(recalled) != 0 {
		t.Fatalf("expected policy to suppress recall, section=%q recalled=%d", section, len(recalled))
	}
}

func TestProactiveContextForPromptRendersProviderCandidates(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	section, recalled := store.ProactiveContextForPrompt("recover playwright snapshot", ProactivePromptOptions{
		Recall: ProactiveRecallOptions{
			MaxEntries: 2,
			Provider: promptTestProvider{candidates: []lifecycle.Candidate{{
				Entry: lifecycle.Entry{
					ID:        "skill-provider-1",
					EntryType: lifecycle.EntryTypeFailureSkill,
					WhenToUse: "when browser verification stalls",
					Content:   "Restart local dev server before rerunning Playwright snapshot checks.",
				},
				Relevance:     1,
				PriorityScore: 1,
			}}},
		},
		RecallEntries: RecallEntriesPromptOptions{Header: "## Recall"},
	})
	if len(recalled) != 0 {
		t.Fatalf("expected no memory entries for external provider candidate, got %+v", recalled)
	}
	for _, want := range []string{"## Recall", "failure_skill", "Restart local dev server", "when browser verification stalls"} {
		if !strings.Contains(section, want) {
			t.Fatalf("expected provider candidate text %q in %q", want, section)
		}
	}
}
