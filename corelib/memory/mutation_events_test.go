package memory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMutationEventsCoverSemanticWritesButNotAccessTouches(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	var events []MutationEvent
	unsubscribe := store.SubscribeMutations(func(event MutationEvent) {
		events = append(events, event)
	})
	defer unsubscribe()

	entry := Entry{Content: "User prefers concise Chinese answers in technical discussions", Category: CategoryPreference, SourceType: "test"}
	if err := store.Save(entry); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != MutationCreate || events[0].After == nil {
		t.Fatalf("create events = %+v", events)
	}
	entryID := events[0].After.ID
	store.TouchAccess([]string{entryID})
	if len(events) != 1 {
		t.Fatalf("access touch emitted a semantic event: %+v", events)
	}

	stored := store.SearchDirectByID(entryID)[0]
	stored.Content = "User prefers detailed Chinese answers with examples for technical discussions"
	if err := store.UpdateEntriesByID([]Entry{stored}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Kind != MutationUpdate || events[1].Before == nil || events[1].After == nil {
		t.Fatalf("update events = %+v", events)
	}
	if events[1].Before.Content == events[1].After.Content {
		t.Fatal("update event did not preserve before/after content")
	}

	ok, err := store.SupersedeEntryByID(entryID, time.Now())
	if err != nil || !ok {
		t.Fatalf("supersede = %v, %v", ok, err)
	}
	if len(events) != 3 || events[2].Kind != MutationSupersede {
		t.Fatalf("supersede events = %+v", events)
	}
	if err := store.Delete(entryID); err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[3].Kind != MutationDelete || events[3].After != nil {
		t.Fatalf("delete events = %+v", events)
	}
}

func TestRestorePublishesResetAfterCommit(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: "old memory", Category: CategoryProjectKnowledge}); err != nil {
		t.Fatal(err)
	}
	var events []MutationEvent
	unsubscribe := store.SubscribeMutations(func(event MutationEvent) {
		if event.Kind == MutationReset {
			if got := store.List(CategoryProjectKnowledge, "restored"); len(got) != 1 {
				t.Errorf("reset published before restored view was visible: %+v", got)
			}
		}
		events = append(events, event)
	})
	defer unsubscribe()
	if err := store.RestoreEntriesSnapshot([]Entry{{ID: "restored", Content: "restored memory", Category: CategoryProjectKnowledge}}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != MutationReset {
		t.Fatalf("restore events = %+v", events)
	}
}

func TestUpsertPublishesCreateAfterReleasingStoreLock(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	var events []MutationEvent
	unsubscribe := store.SubscribeMutations(func(event MutationEvent) {
		if got := store.List(CategoryConversationSummary, "committed"); len(got) != 1 {
			t.Errorf("event arrived before committed upsert was visible: %+v", got)
		}
		events = append(events, event)
	})
	defer unsubscribe()
	_, err = store.UpsertConversationSummary(ConversationSummaryUpsertOptions{
		Content:          "committed conversation summary",
		Tags:             []string{"summary", "owner", "2026-01-01"},
		IdentityTagCount: 3,
		OwnerID:          "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != MutationCreate {
		t.Fatalf("upsert events = %+v", events)
	}
}
