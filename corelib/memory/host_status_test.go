package memory

import (
	"strings"
	"testing"
	"time"
)

func TestHostStatusProjectionSortsCategories(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if err := store.SaveManualMemory("one", CategoryUserFact, nil); err != nil {
		t.Fatalf("SaveManualMemory one: %v", err)
	}
	if err := store.SaveManualMemory("two", CategoryInstruction, nil); err != nil {
		t.Fatalf("SaveManualMemory two: %v", err)
	}
	if err := store.SaveManualMemory("three", CategoryInstruction, nil); err != nil {
		t.Fatalf("SaveManualMemory three: %v", err)
	}

	status := store.StatusForHost()
	if status.TotalEntries != 3 || len(status.Categories) != 2 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Categories[0].Category != string(CategoryInstruction) || status.Categories[0].Count != 2 {
		t.Fatalf("categories should sort by count desc: %+v", status.Categories)
	}
}

func TestHostListAndHealthProjectionsHandleNil(t *testing.T) {
	var store *Store
	if got := store.ListEntriesForHost("", ""); got != nil {
		t.Fatalf("nil ListEntriesForHost = %+v", got)
	}
	if got := store.ListArchiveEntriesForHost("", ""); got != nil {
		t.Fatalf("nil ListArchiveEntriesForHost = %+v", got)
	}
	if got := store.HealthReportForHost(); got == nil {
		t.Fatal("nil HealthReportForHost should return empty report")
	}
	if got := store.StatusForHost(); got == nil || got.MaxCapacity != 2000 || len(got.Categories) != 0 {
		t.Fatalf("nil StatusForHost = %+v", got)
	}
}

func TestRecentArtifactTitlesForHostFiltersAndFormats(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	now := time.Now().UTC()
	longContent := strings.Repeat("a", 55)
	entries := []Entry{
		{Content: "user fact", Category: CategoryUserFact, CreatedAt: now, UpdatedAt: now},
		{Title: "artifact title", Content: "artifact body", Category: CategoryTaskArtifact, CreatedAt: now, UpdatedAt: now},
		{Content: longContent, Category: CategoryTaskArtifact, CreatedAt: now, UpdatedAt: now},
		{Title: "old artifact", Category: CategoryTaskArtifact, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got := store.RecentArtifactTitlesForHost(now.Add(-24*time.Hour), 3)
	if len(got) != 2 {
		t.Fatalf("RecentArtifactTitlesForHost len = %d, got %#v", len(got), got)
	}
	if got[0] != "artifact title" {
		t.Fatalf("first title = %q", got[0])
	}
	if got[1] != strings.Repeat("a", 50)+"..." {
		t.Fatalf("truncated title = %q", got[1])
	}
	if nilStore := (*Store)(nil); nilStore.RecentArtifactTitlesForHost(now, 3) != nil {
		t.Fatalf("nil store should return nil")
	}
}
