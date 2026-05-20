package memory

import (
	"context"
	"testing"
	"time"
)

func TestToolInspectionFacadesForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "tool-host", Content: "tool host recall target", Category: CategoryProjectKnowledge, Tags: []string{"tool-host"}, Status: StatusActive}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_ = store.MemoryCandidatesForHost(context.Background(), "tool", 5, false)
	_ = store.MemoryThemesForHost(ToolThemesOptions{Limit: 5})
	_ = store.EvaluateRecallStrategiesForHost([]RecallEvalCase{{Name: "case", Query: "tool host recall", ExpectedContains: []string{"target"}}}, 5)
	_ = store.EvaluateRecallStrategiesWithMaintenanceForHost([]RecallEvalCase{{Name: "case", Query: "tool host recall", ExpectedContains: []string{"target"}}}, 5, 2, 1)
	_ = store.EmbedStatusForHost()
	_ = store.GraphNeighborsForHost("tool-host")
	_ = store.StrengthForHost(time.Now())
	_ = store.InferForHost("tool host recall", InferenceOptions{MaxDerived: 2})
	out := store.HandleToolForHost(map[string]interface{}{"action": "recall", "query": "tool host recall"}, ToolOptions{})
	if out == "" {
		t.Fatalf("HandleToolForHost returned empty output")
	}
	if err := store.DeleteEntryForHost("tool-host"); err != nil {
		t.Fatalf("DeleteEntryForHost: %v", err)
	}
	if _, ok := store.EntryByIDForHost("tool-host"); ok {
		t.Fatalf("DeleteEntryForHost did not delete entry")
	}

	var nilStore *Store
	if got := nilStore.MemoryCandidatesForHost(context.Background(), "", 0, false); len(got.Candidates) != 0 {
		t.Fatalf("nil MemoryCandidatesForHost = %+v", got)
	}
	if out := nilStore.HandleToolForHost(map[string]interface{}{"action": "recall"}, ToolOptions{}); out == "" {
		t.Fatalf("nil HandleToolForHost returned empty output")
	}
	if err := nilStore.DeleteEntryForHost("missing"); err != nil {
		t.Fatalf("nil DeleteEntryForHost = %v", err)
	}
}
