package memory

import (
	"strings"
	"testing"
)

func TestPromptFacadesForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if err := store.Save(Entry{ID: "self", Content: "I prefer concise answers", Category: CategorySelfIdentity, Status: StatusActive}); err != nil {
		t.Fatalf("Save self: %v", err)
	}
	if err := store.Save(Entry{ID: "fact", Content: "User works on maclaw memory", Category: CategoryUserFact, Status: StatusActive}); err != nil {
		t.Fatalf("Save fact: %v", err)
	}
	if err := store.Save(Entry{ID: "proj", Content: "Project prompt recall target", Category: CategoryProjectKnowledge, Tags: []string{"prompt", "project"}, Scope: ScopeProject, Status: StatusActive}); err != nil {
		t.Fatalf("Save project: %v", err)
	}

	if got := store.SelfIdentitySummaryForHost(200); !strings.Contains(got, "concise") {
		t.Fatalf("SelfIdentitySummaryForHost = %q", got)
	}
	if got := store.UserFactSummaryForHost(UserFactSummaryPromptOptions{Header: "Facts"}); !strings.Contains(got, "Facts") || !strings.Contains(got, "maclaw") {
		t.Fatalf("UserFactSummaryForHost = %q", got)
	}
	if got := store.StaticMemorySectionForHost(StaticMemoryPromptOptions{UserFacts: UserFactSummaryPromptOptions{Header: "Facts"}}); !strings.Contains(got, "Facts") || !strings.Contains(got, "maclaw") {
		t.Fatalf("StaticMemorySectionForHost = %q", got)
	}
	section, entries := store.ProactiveContextForHost("prompt recall target", ProactivePromptOptions{Recall: ProactiveRecallOptions{}, RecallEntries: RecallEntriesPromptOptions{Header: "Recall"}})
	if !strings.Contains(section, "Recall") || len(entries) == 0 {
		t.Fatalf("ProactiveContextForHost section=%q entries=%+v", section, entries)
	}
	recall, err := store.RecallByModeForHost("prompt recall target", "", "auto", "", 5)
	if err != nil || len(recall.Entries) == 0 {
		t.Fatalf("RecallByModeForHost = %+v, %v", recall, err)
	}

	var nilStore *Store
	if got := nilStore.SelfIdentitySummaryForHost(200); got != "" {
		t.Fatalf("nil SelfIdentitySummaryForHost = %q", got)
	}
	if section, entries := nilStore.ProactiveContextForHost("x", ProactivePromptOptions{}); section != "" || entries != nil {
		t.Fatalf("nil ProactiveContextForHost section=%q entries=%+v", section, entries)
	}
}
