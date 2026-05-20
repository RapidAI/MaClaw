package memory

import (
	"testing"
	"time"
)

func TestExperienceProtectionSnapshotForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if err := store.SaveManualMemory("regular memory", CategoryUserFact, nil); err != nil {
		t.Fatalf("SaveManualMemory regular: %v", err)
	}
	if err := store.SaveManualMemory("important pinned instruction", CategoryInstruction, []string{"policy"}); err != nil {
		t.Fatalf("SaveManualMemory protected: %v", err)
	}
	entries := store.List(CategoryInstruction, "important")
	if len(entries) != 1 {
		t.Fatalf("expected instruction entry, got %+v", entries)
	}
	entries[0].Pinned = true
	if _, err := store.UpsertEntryByID(entries[0]); err != nil {
		t.Fatalf("UpsertEntryByID: %v", err)
	}

	snapshot := store.ExperienceProtectionSnapshotForHost()
	if snapshot.DistillResult.ScannedEntries != 2 || snapshot.DistillResult.ActiveEntries != 2 {
		t.Fatalf("unexpected distill counts: %+v", snapshot.DistillResult)
	}
	if snapshot.DistillResult.ProtectedCandidates == 0 || len(snapshot.Candidates) == 0 {
		t.Fatalf("expected protected candidates: %+v", snapshot)
	}
	if snapshot.Candidates[0].ID == "" || snapshot.Candidates[0].Reason == "" {
		t.Fatalf("candidate should be bounded and classified: %+v", snapshot.Candidates[0])
	}
}

func TestExperienceSnapshotFacadesForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	now := time.Now().UTC()
	entries := []Entry{
		{ID: "old", Content: "old trace", Category: CategoryProjectKnowledge, Tags: []string{"tool_recovery_pattern"}, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), Status: StatusActive},
		{ID: "new", Content: "new trace", Category: CategoryProjectKnowledge, Tags: []string{"tool_recovery_pattern", "new"}, CreatedAt: now, UpdatedAt: now, Status: StatusActive},
		{ID: "other", Content: "other trace", Category: CategoryUserFact, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), Status: StatusActive},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	distill := store.ExperienceDistillForHost(12)
	if distill.ScannedEntries != 3 || distill.ActiveEntries != 3 {
		t.Fatalf("ExperienceDistillForHost = %+v", distill)
	}
	traceEntries := store.ExperienceTraceEntriesForHost()
	if len(traceEntries) != 3 || traceEntries[0].ID != "new" {
		t.Fatalf("ExperienceTraceEntriesForHost = %+v", traceEntries)
	}
	tagged := store.EntriesWithTagForHost("tool_recovery_pattern")
	if len(tagged) != 2 || tagged[0].ID != "new" || tagged[1].ID != "old" {
		t.Fatalf("EntriesWithTagForHost = %+v", tagged)
	}
}
