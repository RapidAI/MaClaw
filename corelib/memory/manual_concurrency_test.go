package memory

import (
	"path/filepath"
	"testing"
)

func TestUpdateManualMemoryIfUnchangedRejectsStaleEdit(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "manual", Content: "opened content", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	opened := store.SearchDirectByID("manual")[0]
	if err := store.Update("manual", "background update", CategoryUserFact, nil); err != nil {
		t.Fatal(err)
	}
	applied, err := store.UpdateManualMemoryIfUnchanged("manual", "stale user edit", CategoryUserFact, nil, opened)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("stale manual edit was applied")
	}
	latest := store.SearchDirectByID("manual")[0]
	if latest.Content != "background update" {
		t.Fatalf("background update was overwritten: %+v", latest)
	}
	applied, err = store.UpdateManualMemoryIfUnchanged("manual", "current user edit", CategoryUserFact, nil, latest)
	if err != nil || !applied {
		t.Fatalf("current manual edit failed: applied=%v err=%v", applied, err)
	}
	if got := store.SearchDirectByID("manual")[0].Content; got != "current user edit" {
		t.Fatalf("content after current edit = %q", got)
	}
}

func TestUpdateBotMemoryIfUnchangedRejectsStaleEdit(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	entry, err := store.SaveBotMemory("desktop-user", "bot_a", "opened bot fact", string(CategoryUserFact), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateBotMemory("desktop-user", "bot_a", entry.ID, "background bot update", string(CategoryUserFact), nil); err != nil {
		t.Fatal(err)
	}
	applied, err := store.UpdateBotMemoryIfUnchanged("desktop-user", "bot_a", entry.ID, "stale bot edit", string(CategoryUserFact), nil, entry)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("stale bot edit was applied")
	}
	entries, err := store.ListBotMemories("desktop-user", "bot_a", "", "")
	if err != nil || len(entries) != 1 || entries[0].Content != "background bot update" {
		t.Fatalf("bot entries after stale edit = %+v, err=%v", entries, err)
	}
}
