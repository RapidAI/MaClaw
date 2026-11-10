package memory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveActiveEntriesRejectsStaleSnapshot(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "editable", Content: "original", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	snapshot := store.SearchDirectByID("editable")[0]
	if err := store.Update("editable", "edited before archive commit", CategoryUserFact, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveActiveEntries([]Entry{snapshot}); err == nil {
		t.Fatal("archiving a stale snapshot should fail")
	}
	entries := store.SearchDirectByID("editable")
	if len(entries) != 1 || entries[0].Content != "edited before archive commit" {
		t.Fatalf("active entry after rejected archive = %+v", entries)
	}
	if archived := store.archive.EntriesByIDs([]string{"editable"}); len(archived) != 0 {
		t.Fatalf("stale archive copy was not rolled back: %+v", archived)
	}
}

func TestRestoreArchiveDoesNotOverwriteActiveEntry(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "same", Content: "new active memory", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	if err := store.archive.AddDurable(Entry{ID: "same", Content: "old archived memory", Category: CategoryUserFact, UpdatedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreFromArchive("same"); err == nil {
		t.Fatal("restoring over an active entry should fail")
	}
	entries := store.SearchDirectByID("same")
	if len(entries) != 1 || entries[0].Content != "new active memory" {
		t.Fatalf("active entry after rejected restore = %+v", entries)
	}
	if archived := store.archive.EntriesByIDs([]string{"same"}); len(archived) != 1 || archived[0].Content != "old archived memory" {
		t.Fatalf("archive entry after rejected restore = %+v", archived)
	}
}

func TestRestoreArchiveInsertsWhenActiveEntryIsAbsent(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.archive.AddDurable(Entry{ID: "archived", Content: "restored memory", Category: CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreFromArchive("archived"); err != nil {
		t.Fatal(err)
	}
	if entries := store.SearchDirectByID("archived"); len(entries) != 1 || entries[0].Content != "restored memory" {
		t.Fatalf("restored active entry = %+v", entries)
	}
	if archived := store.archive.EntriesByIDs([]string{"archived"}); len(archived) != 0 {
		t.Fatalf("restored entry remains archived: %+v", archived)
	}
}
