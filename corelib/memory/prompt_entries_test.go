package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

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
