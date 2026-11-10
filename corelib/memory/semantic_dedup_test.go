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
	newEntry.SourceURL = candidate.SourceURL
	newEntry.Tags = append([]string(nil), candidate.Tags...)
	store.SetEntries([]Entry{candidate, newEntry})
	store.SetLLMDedup(mockLLMForDedup{response: `{"decision":"merge","merged":"alpha endpoint is https://new.example.test","reason":"same endpoint fact"}`})
	store.mu.Lock()
	store.pendingDedup = []pendingDedupPair{{NewEntryID: "new-entry", CandidateEntryID: "candidate", CreatedAt: now}}
	store.mu.Unlock()

	if merged := store.ProcessPendingDedup(context.Background()); merged != 1 {
		t.Fatalf("expected one merge, got %d", merged)
	}

	store.WaitRebuild()
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
	candidate := store.findSemanticDupCandidate([]float32{1, 0, 0, 0}, CategoryProjectKnowledge, "", "")
	store.mu.RUnlock()
	if candidate != nil {
		t.Fatalf("inactive entries should not become semantic dedup candidates, got %+v", candidate)
	}
}

func TestSemanticDedupCandidateStaysInsideOwner(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	emb := []float32{1, 0, 0, 0}
	store.SetEntries([]Entry{
		{ID: "user-b", Content: "user b fact", Category: CategoryProjectKnowledge, Status: StatusActive, OwnerID: "user-b", Embedding: emb},
		{ID: "shared", Content: "shared fact", Category: CategoryProjectKnowledge, Status: StatusActive, Embedding: emb},
		{ID: "user-a", Content: "user a fact", Category: CategoryProjectKnowledge, Status: StatusActive, OwnerID: "user-a", Embedding: emb},
	})
	store.vecIndex.add("user-b", emb)
	store.vecIndex.add("shared", emb)
	store.vecIndex.add("user-a", emb)

	store.mu.RLock()
	defer store.mu.RUnlock()
	if got := store.findSemanticDupCandidate(emb, CategoryProjectKnowledge, "user-a", ""); got == nil || got.EntryID != "user-a" {
		t.Fatalf("user-a candidate = %+v", got)
	}
	if got := store.findSemanticDupCandidate(emb, CategoryProjectKnowledge, "", ""); got == nil || got.EntryID != "shared" {
		t.Fatalf("shared candidate = %+v", got)
	}
}

func TestProcessPendingDedupDoesNotMergeAcrossOwners(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	store.SetEntries([]Entry{
		{ID: "private", Content: "user a private fact", Category: CategoryProjectKnowledge, Status: StatusActive, OwnerID: "user-a"},
		{ID: "shared", Content: "shared fact", Category: CategoryProjectKnowledge, Status: StatusActive},
	})
	store.SetLLMDedup(mockLLMForDedup{response: `{"decision":"merge","merged":"leaked","reason":"same"}`})
	store.mu.Lock()
	store.pendingDedup = []pendingDedupPair{{NewEntryID: "shared", CandidateEntryID: "private", CreatedAt: time.Now()}}
	store.mu.Unlock()
	if merged := store.ProcessPendingDedup(context.Background()); merged != 0 {
		t.Fatalf("merged = %d", merged)
	}
	if got := store.SearchDirectByID("private"); len(got) != 1 || got[0].Content != "user a private fact" {
		t.Fatalf("private entry = %+v", got)
	}
	if got := store.SearchDirectByID("shared"); len(got) != 1 {
		t.Fatalf("shared entry = %+v", got)
	}
}

func TestSemanticDedupCandidateStaysInsideProject(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	emb := []float32{1, 0, 0, 0}
	projectA := &MemoryBoundary{ProjectPath: `D:\work\project-a`}
	projectB := &MemoryBoundary{ProjectPath: `D:\work\project-b`}
	store.SetEntries([]Entry{
		{ID: "project-b", Content: "project B fact", Category: CategoryProjectKnowledge, OwnerID: "desktop-user", Embedding: emb, Boundary: projectB},
		{ID: "project-a", Content: "project A fact", Category: CategoryProjectKnowledge, OwnerID: "desktop-user", Embedding: emb, Boundary: projectA},
	})
	store.vecIndex.add("project-b", emb)
	store.vecIndex.add("project-a", emb)
	store.mu.RLock()
	defer store.mu.RUnlock()
	candidate := store.findSemanticDupCandidate(emb, CategoryProjectKnowledge, "desktop-user", compressionProjectKey(Entry{Boundary: projectA}))
	if candidate == nil || candidate.EntryID != "project-a" {
		t.Fatalf("project A candidate = %+v", candidate)
	}
}

func TestProcessPendingDedupDoesNotMergeAcrossProjects(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	store.SetEntries([]Entry{
		{ID: "project-a", Content: "project A fact", Category: CategoryProjectKnowledge, OwnerID: "desktop-user", Boundary: &MemoryBoundary{ProjectPath: `D:\work\project-a`}},
		{ID: "project-b", Content: "project B fact", Category: CategoryProjectKnowledge, OwnerID: "desktop-user", Boundary: &MemoryBoundary{ProjectPath: `D:\work\project-b`}},
	})
	store.SetLLMDedup(mockLLMForDedup{response: `{"decision":"merge","merged":"cross-project","reason":"same"}`})
	store.pendingDedup = []pendingDedupPair{{NewEntryID: "project-a", CandidateEntryID: "project-b", CreatedAt: time.Now()}}
	if merged := store.ProcessPendingDedup(context.Background()); merged != 0 {
		t.Fatalf("merged = %d", merged)
	}
	if len(store.List("", "")) != 2 {
		t.Fatal("cross-project dedup changed the entries")
	}
}

type blockingDedupLLM struct {
	started chan struct{}
	release chan struct{}
}

func (l *blockingDedupLLM) IsConfigured() bool { return true }
func (l *blockingDedupLLM) ChatCall(messages []map[string]string) (string, error) {
	close(l.started)
	<-l.release
	return `{"decision":"merge","merged":"merged memory","reason":"same"}`, nil
}

func TestProcessPendingDedupSkipsEntriesChangedDuringJudgment(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	store.SetEntries([]Entry{
		{ID: "new", Content: "new fact", Category: CategoryProjectKnowledge},
		{ID: "candidate", Content: "candidate fact", Category: CategoryProjectKnowledge},
	})
	llm := &blockingDedupLLM{started: make(chan struct{}), release: make(chan struct{})}
	store.SetLLMDedup(llm)
	store.pendingDedup = []pendingDedupPair{{NewEntryID: "new", CandidateEntryID: "candidate", CreatedAt: time.Now()}}
	done := make(chan int, 1)
	go func() { done <- store.ProcessPendingDedup(context.Background()) }()
	select {
	case <-llm.started:
	case <-time.After(3 * time.Second):
		t.Fatal("dedup did not reach the LLM")
	}
	if err := store.Update("new", "user edited during dedup", CategoryProjectKnowledge, nil); err != nil {
		t.Fatal(err)
	}
	close(llm.release)
	if merged := <-done; merged != 0 {
		t.Fatalf("merged = %d", merged)
	}
	if got := store.SearchDirectByID("new"); len(got) != 1 || got[0].Content != "user edited during dedup" {
		t.Fatalf("new entry = %+v", got)
	}
	if got := store.SearchDirectByID("candidate"); len(got) != 1 || got[0].Content != "candidate fact" {
		t.Fatalf("candidate entry = %+v", got)
	}
}

func TestProcessPendingDedupRejectsChangedLifecycleEligibility(t *testing.T) {
	tests := []struct {
		name      string
		newEntry  Entry
		candidate Entry
	}{
		{
			name:      "different categories",
			newEntry:  Entry{ID: "new", Content: "new fact", Category: CategoryUserFact},
			candidate: Entry{ID: "candidate", Content: "candidate fact", Category: CategoryPreference},
		},
		{
			name:      "pinned candidate",
			newEntry:  Entry{ID: "new", Content: "new fact", Category: CategoryProjectKnowledge},
			candidate: Entry{ID: "candidate", Content: "candidate fact", Category: CategoryProjectKnowledge, Pinned: true},
		},
		{
			name:      "protected pair",
			newEntry:  Entry{ID: "new", Content: "new identity", Category: CategorySelfIdentity},
			candidate: Entry{ID: "candidate", Content: "candidate identity", Category: CategorySelfIdentity},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Stop()
			store.SetEntries([]Entry{test.candidate, test.newEntry})
			store.SetLLMDedup(mockLLMForDedup{response: `{"decision":"merge","merged":"merged","reason":"same"}`})
			store.pendingDedup = []pendingDedupPair{{NewEntryID: test.newEntry.ID, CandidateEntryID: test.candidate.ID, CreatedAt: time.Now()}}
			if merged := store.ProcessPendingDedup(context.Background()); merged != 0 {
				t.Fatalf("merged = %d", merged)
			}
			if entries := store.List("", ""); len(entries) != 2 {
				t.Fatalf("entries after rejected merge = %+v", entries)
			}
		})
	}
}

func TestProcessPendingDedupDropsStaleEmbeddingAfterMerge(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	invalidAt := time.Now().Add(-time.Hour)
	store.SetEntries([]Entry{
		{ID: "candidate", Content: "candidate fact", Category: CategoryProjectKnowledge, Embedding: []float32{1, 0}, InvalidAt: &invalidAt, Stale: true},
		{ID: "new", Content: "new fact", Category: CategoryProjectKnowledge, Embedding: []float32{0, 1}},
	})
	store.SetLLMDedup(mockLLMForDedup{response: `{"decision":"merge","merged":"merged fact","reason":"same"}`})
	store.pendingDedup = []pendingDedupPair{{NewEntryID: "new", CandidateEntryID: "candidate", CreatedAt: time.Now()}}
	if merged := store.ProcessPendingDedup(context.Background()); merged != 1 {
		t.Fatalf("merged = %d", merged)
	}
	entries := store.SearchDirectByID("candidate")
	if len(entries) != 1 || entries[0].Content != "merged fact" {
		t.Fatalf("candidate after merge = %+v", entries)
	}
	if len(entries[0].Embedding) != 0 || entries[0].InvalidAt != nil || entries[0].Stale {
		t.Fatalf("stale metadata survived content merge: %+v", entries[0])
	}
}
