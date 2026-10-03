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
	if status.TotalEntries != 3 || status.RecallableEntries != 3 || len(status.Categories) != 2 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Categories[0].Category != string(CategoryInstruction) || status.Categories[0].Count != 2 {
		t.Fatalf("categories should sort by count desc: %+v", status.Categories)
	}
}

func TestListActiveForOwnerSkipsInactiveAndOtherOwners(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	for _, entry := range []Entry{
		{Content: "desktop fact", Category: CategoryUserFact, OwnerID: "desktop-user"},
		{Content: "shared fact", Category: CategoryUserFact},
		{Content: "other fact", Category: CategoryUserFact, OwnerID: "other-user"},
		{Content: "old desktop fact", Category: CategoryUserFact, OwnerID: "desktop-user", Status: StatusSuperseded},
		{Content: "dormant desktop fact", Category: CategoryUserFact, OwnerID: "desktop-user", Status: StatusDormant},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	if got := store.List("", ""); len(got) != 5 {
		t.Fatalf("Store.List = %d entries, want 5", len(got))
	}
	visible := store.ListActiveForOwner("", "", "desktop-user", false)
	if len(visible) != 2 {
		t.Fatalf("desktop visible = %+v", visible)
	}
	for _, entry := range visible {
		if entry.Content == "other fact" || entry.Content == "old desktop fact" || entry.Content == "dormant desktop fact" {
			t.Fatalf("visible entry %q", entry.Content)
		}
	}
	strict := store.ListActiveForOwner("", "", "desktop-user", true)
	if len(strict) != 1 || strict[0].Content != "desktop fact" {
		t.Fatalf("strict visible = %+v", strict)
	}
	status := store.StatusForHost()
	if status.TotalEntries != 5 || status.RecallableEntries != 3 || status.DormantEntries != 1 || status.SupersededEntries != 1 {
		t.Fatalf("status split = %+v", status)
	}
	if len(status.Categories) != 1 || status.Categories[0].Category != string(CategoryUserFact) || status.Categories[0].Count != 3 {
		t.Fatalf("status categories should count recallable rows only: %+v", status.Categories)
	}
}

func TestEntryExpiredAtIncludesTheInstant(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if entryExpiredAt(nil, now) {
		t.Fatal("nil InvalidAt is not expired")
	}
	future := now.Add(time.Second)
	if entryExpiredAt(&future, now) {
		t.Fatal("future InvalidAt is not expired")
	}
	if !entryExpiredAt(&now, now) {
		t.Fatal("InvalidAt equal to now is expired")
	}
	past := now.Add(-time.Second)
	if !entryExpiredAt(&past, now) {
		t.Fatal("past InvalidAt is expired")
	}
}

func TestHealthReportStaleAndPinnedIgnoreInactiveRows(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()
	for _, entry := range []Entry{
		{Content: "live stale", Category: CategoryUserFact, Stale: true, Pinned: true},
		{Content: "old stale", Category: CategoryUserFact, Status: StatusSuperseded, Stale: true, Pinned: true},
		{Content: "dormant stale", Category: CategoryUserFact, Status: StatusDormant, Stale: true},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	status := store.StatusForHost()
	if status.StaleEntries != 1 || status.PinnedEntries != 1 || status.RecallableEntries != 1 {
		t.Fatalf("status = %+v", status)
	}
}

func TestHealthReportExpiredActiveRowStaysRecallable(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()
	expired := time.Now().Add(-time.Hour)
	if err := store.Save(Entry{Content: "still true", Category: CategoryUserFact, OwnerID: "desktop-user"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "no longer true", Category: CategoryUserFact, OwnerID: "desktop-user", InvalidAt: &expired}); err != nil {
		t.Fatal(err)
	}
	status := store.StatusForHost()
	if status.TotalEntries != 2 || status.RecallableEntries != 2 || status.InvalidEntries != 1 {
		t.Fatalf("status = %+v", status)
	}
	if len(status.Categories) != 1 || status.Categories[0].Count != 2 {
		t.Fatalf("categories = %+v", status.Categories)
	}
	recalled := store.RecallDynamic("no longer true", CategoryUserFact, "")
	found := false
	for _, entry := range recalled {
		if entry.Content == "no longer true" {
			found = true
		}
	}
	if !found {
		t.Fatal("dynamic recall dropped an expired active row")
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
