package memory

import (
	"path/filepath"
	"testing"
)

func TestRecallProactiveFiltersProfileByDefault(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entries := []Entry{
		{Content: "User prefers concise answers about React migration", Category: CategoryUserFact, Tags: []string{"react", "migration"}, Status: StatusActive},
		{Content: "React migration project uses pnpm test for validation", Category: CategoryProjectKnowledge, Tags: []string{"react", "migration", "pnpm"}, Status: StatusActive},
		{Content: "Conversation summary about React migration", Category: CategoryConversationSummary, Tags: []string{"react"}, Status: StatusActive},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	got := store.RecallProactive("react migration pnpm", ProactiveRecallOptions{MaxEntries: 10})
	for _, entry := range got {
		if MapToCanonical(entry.Category) == CategoryUserFact || MapToCanonical(entry.Category) == CategoryConversationSummary {
			t.Fatalf("unexpected filtered category in proactive recall: %+v", entry)
		}
	}
	if len(got) == 0 {
		t.Fatal("expected project memory")
	}
}

func TestRecallProactiveCanIncludeProfile(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "User prefers concise answers about Kubernetes", Category: CategoryUserFact, Tags: []string{"kubernetes", "concise"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	got := store.RecallProactive("kubernetes concise preference", ProactiveRecallOptions{IncludeUserProfile: true, MaxEntries: 5})
	if len(got) == 0 {
		t.Fatal("expected profile memory when IncludeUserProfile is true")
	}
}
