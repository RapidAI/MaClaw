package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type mockLLMForDedup struct {
	response string
}

func (m mockLLMForDedup) ChatCall(messages []map[string]string) (string, error) {
	return m.response, nil
}

func (m mockLLMForDedup) IsConfigured() bool { return true }

func TestProcessPendingDedupMergeRebuildsAllDerivedIndexes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Now()
	candidate := Entry{
		ID:           "candidate",
		Content:      "alpha endpoint is https://old.example.test",
		Category:     CategoryProjectKnowledge,
		SourceURL:    `D:\workprjlpha\old.md`,
		Tags:         []string{`D:\workprjlpha`},
		Entities:     []string{"entity:alpha", "relation:config_of", "entity:https://old.example.test"},
		Embedding:    []float32{1, 0, 0, 0},
		RelatedIDs:   []string{"new-entry"},
		RelatedEdges: []RelatedEdge{{ID: "new-entry", Strength: 0.9}},
		UpdatedAt:    now.Add(-time.Hour),
	}
	newEntry := Entry{
		ID:       "new-entry",
		Content:  "alpha endpoint is https://new.example.test",
		Category: CategoryProjectKnowledge,
		SourceURL: `D:\workprjeta
ew.md`,
		Tags:         []string{`D:\workprjeta`},
		Entities:     []string{"entity:alpha", "relation:config_of", "entity:https://new.example.test"},
		Embedding:    []float32{1, 0, 0, 0},
		RelatedIDs:   []string{"candidate"},
		RelatedEdges: []RelatedEdge{{ID: "candidate", Strength: 0.9}},
		UpdatedAt:    now,
	}
	store.SetEntries([]Entry{candidate, newEntry})
	store.SetLLMDedup(mockLLMForDedup{response: `{"decision":"merge","merged":"alpha endpoint is https://new.example.test","reason":"same endpoint fact"}`})
	store.mu.Lock()
	store.pendingDedup = []pendingDedupPair{{NewEntryID: "new-entry", CandidateEntryID: "candidate", CreatedAt: now}}
	store.mu.Unlock()

	if merged := store.ProcessPendingDedup(context.Background()); merged != 1 {
		t.Fatalf("expected one merge, got %d", merged)
	}

	if got := store.SearchDirectByID("new-entry"); len(got) != 0 {
		t.Fatalf("new entry should be removed from store, got %+v", got)
	}
	if scores := store.vecIndex.score([]float32{1, 0, 0, 0}); scores["new-entry"] != 0 {
		t.Fatalf("new entry should be removed from vector index, got scores=%v", scores)
	}
	if neighbors := store.GraphNeighbors("candidate"); len(neighbors) != 0 {
		t.Fatalf("candidate graph edges should drop removed entry, got %v", neighbors)
	}
	if rec := store.ProjectIndex().Get(`D:\workprjeta`); rec != nil {
		t.Fatalf("removed entry project should not remain indexed, got %+v", rec)
	}
	if got := store.FindByEntity("https://new.example.test"); len(got) != 1 || got[0].ID != "candidate" {
		t.Fatalf("merged candidate should carry new fact in entity index, got %+v", got)
	}
	hits := store.SemanticGraph().SearchWithOptions([]string{"https://new.example.test"}, SemanticSearchOptions{Now: time.Now()})
	if len(hits) == 0 || hits[0].EntryID != "candidate" {
		t.Fatalf("semantic graph should point merged fact at candidate only, got %+v", hits)
	}
}

func TestSemanticDedupCandidateSkipsInactiveEntries(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	store.SetEntries([]Entry{
		{ID: "old", Content: "old duplicate", Category: CategoryProjectKnowledge, Status: StatusSuperseded, Embedding: []float32{1, 0, 0, 0}},
		{ID: "dormant", Content: "dormant duplicate", Category: CategoryProjectKnowledge, Status: StatusDormant, Embedding: []float32{1, 0, 0, 0}},
	})
	store.vecIndex.add("old", []float32{1, 0, 0, 0})
	store.vecIndex.add("dormant", []float32{1, 0, 0, 0})

	store.mu.RLock()
	candidate := store.findSemanticDupCandidate([]float32{1, 0, 0, 0}, CategoryProjectKnowledge, "")
	store.mu.RUnlock()
	if candidate != nil {
		t.Fatalf("inactive entries should not become semantic dedup candidates, got %+v", candidate)
	}
}
