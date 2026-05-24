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

func TestProcessPendingDedupMergePersistsUpdateAndDeleteThroughSQLiteBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	defer store.Stop()

	now := time.Now().UTC()
	candidate := Entry{
		ID:           "candidate-sqlite",
		Content:      "alpha endpoint is https://old.example.test",
		Category:     CategoryProjectKnowledge,
		Tags:         []string{"alpha"},
		Entities:     []string{"entity:alpha", "entity:https://old.example.test"},
		Embedding:    []float32{1, 0, 0, 0},
		RelatedIDs:   []string{"new-entry-sqlite"},
		RelatedEdges: []RelatedEdge{{ID: "new-entry-sqlite", Strength: 0.9}},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	newEntry := Entry{
		ID:           "new-entry-sqlite",
		Content:      "alpha endpoint is https://new.example.test",
		Category:     CategoryProjectKnowledge,
		Tags:         []string{"beta"},
		Entities:     []string{"entity:alpha", "entity:https://new.example.test"},
		Embedding:    []float32{1, 0, 0, 0},
		RelatedIDs:   []string{"candidate-sqlite"},
		RelatedEdges: []RelatedEdge{{ID: "candidate-sqlite", Strength: 0.9}},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.Save(candidate); err != nil {
		t.Fatalf("save candidate: %v", err)
	}
	if err := store.Save(newEntry); err != nil {
		t.Fatalf("save new entry: %v", err)
	}
	store.SetLLMDedup(mockLLMForDedup{response: `{"decision":"merge","merged":"alpha endpoint is https://new.example.test","reason":"same endpoint fact"}`})
	store.mu.Lock()
	store.pendingDedup = []pendingDedupPair{{NewEntryID: "new-entry-sqlite", CandidateEntryID: "candidate-sqlite", CreatedAt: now}}
	store.mu.Unlock()

	if merged := store.ProcessPendingDedup(context.Background()); merged != 1 {
		t.Fatalf("expected one merge, got %d", merged)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	seen := map[string]Entry{}
	for _, entry := range loaded {
		seen[entry.ID] = entry
	}
	mergedEntry := seen["candidate-sqlite"]
	if mergedEntry.Content != "alpha endpoint is https://new.example.test" || !hasTag(mergedEntry.Tags, "beta") {
		t.Fatalf("candidate update not persisted: %+v", mergedEntry)
	}
	if len(mergedEntry.RelatedIDs) != 0 || len(mergedEntry.RelatedEdges) != 0 {
		t.Fatalf("candidate should drop edges to deleted entry: related=%v edges=%v", mergedEntry.RelatedIDs, mergedEntry.RelatedEdges)
	}
	if _, ok := seen["new-entry-sqlite"]; ok {
		t.Fatalf("merged new entry should be absent from LoadAll: %+v", seen["new-entry-sqlite"])
	}
	_, deleted, err := backend.Since(0)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "new-entry-sqlite" {
		t.Fatalf("new entry delete should be visible in sync stream, got %v", deleted)
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
