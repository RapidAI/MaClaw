package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type consolidatorMetadataLLM struct {
	response string
}

func (l consolidatorMetadataLLM) ChatCall(messages []map[string]string) (string, error) {
	return l.response, nil
}

func (l consolidatorMetadataLLM) IsConfigured() bool { return true }

func TestConsolidatorSegmentStoresDerivedBoundary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	turnTime := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)
	consolidator := NewConsolidator(store, store.TMT(), consolidatorMetadataLLM{response: "User asked to keep memory summaries evidence-first."})
	result, err := consolidator.ConsolidateSegment(context.Background(), "keep evidence", "will do", turnTime, "owner-a")
	if err != nil {
		t.Fatalf("ConsolidateSegment: %v", err)
	}
	if result.NodesCreated != 1 {
		t.Fatalf("expected one segment, got %+v", result)
	}

	entries := store.List(CategoryConversationSummary, "")
	if len(entries) != 1 {
		t.Fatalf("expected one segment entry, got %+v", entries)
	}
	entry := entries[0]
	if entry.DerivedKind != "tmt:segment" || entry.SourceType != "tmt_consolidation" {
		t.Fatalf("unexpected derived metadata: %+v", entry)
	}
	if entry.Boundary == nil || entry.Boundary.OwnerID != "owner-a" || entry.Boundary.SourceScope != "conversation" {
		t.Fatalf("unexpected boundary: %+v", entry.Boundary)
	}
	if entry.Boundary.Since == nil || !entry.Boundary.Since.Equal(turnTime) || entry.Boundary.Until == nil || !entry.Boundary.Until.Equal(turnTime) {
		t.Fatalf("unexpected boundary time window: %+v", entry.Boundary)
	}
}

func TestConsolidatorLevelStoresEvidenceAndBoundary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	window := TimeInterval{Start: time.Date(2026, 5, 19, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)}
	seg1 := TimeInterval{Start: window.Start.Add(5 * time.Minute), End: window.Start.Add(10 * time.Minute)}
	seg2 := TimeInterval{Start: window.Start.Add(15 * time.Minute), End: window.Start.Add(20 * time.Minute)}
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "seg-1", Content: "first segment", Category: CategoryConversationSummary, Level: LevelSegment, Interval: &seg1, OwnerID: "owner-a", SourceType: "conversation", Status: StatusActive},
		{ID: "seg-2", Content: "second segment", Category: CategoryConversationSummary, Level: LevelSegment, Interval: &seg2, OwnerID: "owner-a", SourceType: "conversation", Status: StatusActive},
	})
	store.mu.Unlock()

	consolidator := NewConsolidator(store, store.TMT(), consolidatorMetadataLLM{response: "session summary"})
	result, err := consolidator.ConsolidateLevel(context.Background(), LevelSession, window, "owner-a")
	if err != nil {
		t.Fatalf("ConsolidateLevel: %v", err)
	}
	if result.NodesCreated != 1 || result.ChildrenMerged != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}

	entries := store.List(CategoryConversationSummary, "session summary")
	if len(entries) != 1 {
		t.Fatalf("expected session summary, got %+v", entries)
	}
	entry := entries[0]
	if entry.Level != LevelSession || entry.DerivedKind != "tmt:session" || entry.SourceType != "tmt_consolidation" {
		t.Fatalf("unexpected derived session metadata: %+v", entry)
	}
	if strings.Join(entry.EvidenceIDs, ",") != "seg-1,seg-2" || strings.Join(entry.RelatedIDs, ",") != "seg-1,seg-2" {
		t.Fatalf("unexpected evidence links: evidence=%v related=%v", entry.EvidenceIDs, entry.RelatedIDs)
	}
	if entry.Boundary == nil || entry.Boundary.OwnerID != "owner-a" || entry.Boundary.SourceScope != "conversation" {
		t.Fatalf("unexpected boundary: %+v", entry.Boundary)
	}
	if entry.Boundary.Since == nil || !entry.Boundary.Since.Equal(window.Start) || entry.Boundary.Until == nil || !entry.Boundary.Until.Equal(window.End) {
		t.Fatalf("unexpected boundary time window: %+v", entry.Boundary)
	}
}
