package memory

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRecallForProject_GraphExpand verifies that 1-hop graph expansion
// brings in related entries that weren't in the initial top-N.
func TestRecallForProject_GraphExpand(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Use deterministic embedder so we can control similarity.
	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.98, 0.1, 0, 0}  // high cosine with A
	vecC := []float32{0, 1, 0, 0}       // orthogonal to A and B
	vecQ := []float32{0.95, 0.05, 0, 0} // query: similar to A and B

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"golang concurrency patterns": vecA,
			"go goroutine best practices": vecB,
			"unrelated cooking topic":     vecC,
			"golang parallel programming": vecQ,
		},
	}
	store.embedder = emb

	// Save entry A — will be a direct match for the query.
	if err := store.Save(Entry{
		Content:   "golang concurrency patterns",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}

	// Save entry C — unrelated, won't match query.
	if err := store.Save(Entry{
		Content:   "unrelated cooking topic",
		Category:  CategoryProjectKnowledge,
		Embedding: vecC,
	}); err != nil {
		t.Fatal(err)
	}

	// Save entry B — similar to A, autoLink will create a graph edge A↔B.
	if err := store.Save(Entry{
		Content:   "go goroutine best practices",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	// Verify graph edge exists between A and B.
	store.mu.RLock()
	var idA, idB string
	for _, e := range store.entries {
		switch e.Content {
		case "golang concurrency patterns":
			idA = e.ID
		case "go goroutine best practices":
			idB = e.ID
		}
	}
	store.mu.RUnlock()

	if idA == "" || idB == "" {
		t.Fatal("entries not found")
	}

	neighbors := store.graph.neighborsOf(idA)
	if _, ok := neighbors[idB]; !ok {
		t.Fatalf("expected graph edge A→B, got neighbors: %v", neighbors)
	}

	// Recall with a query similar to A — should get both A and B via graph expansion.
	results := store.RecallForProject("golang parallel programming", "")

	foundA, foundB := false, false
	for _, e := range results {
		if e.ID == idA {
			foundA = true
		}
		if e.ID == idB {
			foundB = true
		}
	}

	if !foundA {
		t.Error("expected entry A in recall results")
	}
	if !foundB {
		t.Error("expected entry B in recall results (via graph expansion)")
	}
}

// TestRecallDynamic_GraphExpand verifies graph expansion in RecallDynamic.
func TestRecallDynamic_GraphExpand(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.98, 0.1, 0, 0}
	vecC := []float32{0, 1, 0, 0}
	vecQ := []float32{0.95, 0.05, 0, 0}

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"memory management in go":   vecA,
			"garbage collection tuning": vecB,
			"unrelated music theory":    vecC,
			"go memory allocation":      vecQ,
		},
	}
	store.embedder = emb

	if err := store.Save(Entry{
		Content:   "memory management in go",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Entry{
		Content:   "unrelated music theory",
		Category:  CategoryProjectKnowledge,
		Embedding: vecC,
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Entry{
		Content:   "garbage collection tuning",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	// Verify graph edge.
	store.mu.RLock()
	var idA, idB string
	for _, e := range store.entries {
		switch e.Content {
		case "memory management in go":
			idA = e.ID
		case "garbage collection tuning":
			idB = e.ID
		}
	}
	store.mu.RUnlock()

	if idA == "" || idB == "" {
		t.Fatal("entries not found")
	}

	neighbors := store.graph.neighborsOf(idA)
	if _, ok := neighbors[idB]; !ok {
		t.Fatalf("expected graph edge A→B, got neighbors: %v", neighbors)
	}

	results := store.RecallDynamic("go memory allocation", "", "")

	foundA, foundB := false, false
	for _, e := range results {
		if e.ID == idA {
			foundA = true
		}
		if e.ID == idB {
			foundB = true
		}
	}

	if !foundA {
		t.Error("expected entry A in RecallDynamic results")
	}
	if !foundB {
		t.Error("expected entry B in RecallDynamic results (via graph expansion)")
	}
}

// TestGraphExpand_EmptyCandidates verifies graphExpand handles empty input.
func TestGraphExpand_EmptyCandidates(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	store.mu.RLock()
	result := store.graphExpand(nil, 5)
	store.mu.RUnlock()

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

// TestGraphExpand_NoGraphEdges verifies graphExpand is a no-op when graph is empty.
func TestGraphExpand_NoGraphEdges(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Now()
	entry := Entry{
		ID:        "test-1",
		Content:   "some content",
		Category:  CategoryProjectKnowledge,
		CreatedAt: now,
		UpdatedAt: now,
		Strength:  1.0,
	}

	candidates := []recallScored{{entry: entry, score: 5.0}}

	store.mu.RLock()
	result := store.graphExpand(candidates, 5)
	store.mu.RUnlock()

	if len(result) != 1 {
		t.Errorf("expected 1 candidate (unchanged), got %d", len(result))
	}
}

// TestGraphExpand_Deduplication verifies expanded entries don't duplicate existing candidates.
func TestGraphExpand_Deduplication(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.98, 0.1, 0, 0}

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"topic alpha": vecA,
			"topic beta":  vecB,
		},
	}
	store.embedder = emb

	// Save both entries — autoLink will create edge.
	if err := store.Save(Entry{
		Content:   "topic alpha",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:   "topic beta",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	var entryA, entryB Entry
	for _, e := range store.entries {
		switch e.Content {
		case "topic alpha":
			entryA = e
		case "topic beta":
			entryB = e
		}
	}

	// Both A and B are already in candidates — expansion should NOT duplicate B.
	candidates := []recallScored{
		{entry: entryA, score: 5.0},
		{entry: entryB, score: 4.0},
	}

	result := store.graphExpand(candidates, 5)
	store.mu.RUnlock()

	// Count occurrences of B.
	countB := 0
	for _, c := range result {
		if c.entry.ID == entryB.ID {
			countB++
		}
	}
	if countB != 1 {
		t.Errorf("expected entry B exactly once, got %d", countB)
	}
}

func TestRecallForProject_GraphExpandPreservesProjectScope(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	alphaProject := `D:\workprj\alpha`
	betaProject := `D:\workprj\beta`
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "alpha-seed", Content: "alpha visible needle config", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{alphaProject}, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "beta-neighbor", Content: "beta hidden neighbor config", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{betaProject}, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()
	store.graph.link("alpha-seed", "beta-neighbor", 1)

	results := store.RecallForProject("alpha visible needle", alphaProject)
	for _, entry := range results {
		if entry.ID == "beta-neighbor" {
			t.Fatalf("graph expansion leaked a different project into alpha recall: %+v", results)
		}
	}
}

func TestRecallForProject_GraphExpandDoesNotBypassDedicatedTypeQuotas(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	project := `D:\workprj\alpha`
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "project-seed", Content: "alpha visible project config", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{project}, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "user-fact", Content: "alpha dedicated user fact", Category: CategoryUserFact, Scope: ScopeGlobal, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()
	store.graph.link("project-seed", "user-fact", 1)

	results := store.RecallForProject("alpha visible project", project)
	count := 0
	for _, entry := range results {
		if entry.ID == "user-fact" {
			count++
		}
	}
	if count > 1 {
		t.Fatalf("graph expansion should not duplicate dedicated user facts through the general tier, count=%d results=%+v", count, results)
	}
}

func TestRecallDynamic_PreservesProjectScopeBeforeAndAfterGraphExpand(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	alphaProject := `D:\workprj\alpha`
	betaProject := `D:\workprj\beta`
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "alpha-dynamic", Content: "shared dynamic scope needle", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{alphaProject}, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "beta-direct", Content: "shared dynamic scope needle", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{betaProject}, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "beta-expanded", Content: "hidden expanded beta scope", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{betaProject}, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()
	store.graph.link("alpha-dynamic", "beta-expanded", 1)

	results := store.RecallDynamic("shared dynamic scope needle", "", alphaProject)
	for _, entry := range results {
		if entry.ID == "beta-direct" || entry.ID == "beta-expanded" {
			t.Fatalf("RecallDynamic leaked beta project entry into alpha recall: %+v", results)
		}
	}
}

func TestRecallDynamic_GraphExpandPreservesRequestedCategory(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "instruction-seed", Content: "alpha command policy", Category: CategoryInstruction, Scope: ScopeGlobal, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "project-neighbor", Content: "alpha project neighbor", Category: CategoryProjectKnowledge, Scope: ScopeGlobal, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()
	store.graph.link("instruction-seed", "project-neighbor", 1)

	results := store.RecallDynamic("alpha command policy", CategoryInstruction, "")
	for _, entry := range results {
		if entry.Category != CategoryInstruction {
			t.Fatalf("RecallDynamic graph expansion ignored requested category: %+v", results)
		}
	}
}

func TestRecallWithBFS_GraphExpansionPreservesProjectScope(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	alphaProject := `D:\workprj\alpha`
	betaProject := `D:\workprj\beta`
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "bfs-alpha-seed", Content: "bfs scoped alpha seed", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{alphaProject}, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "bfs-beta-neighbor", Content: "bfs scoped beta neighbor", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{betaProject}, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()
	store.graph.link("bfs-alpha-seed", "bfs-beta-neighbor", 1)

	results := store.RecallWithBFS("bfs scoped alpha seed", "", alphaProject, 1)
	for _, entry := range results {
		if entry.ID == "bfs-beta-neighbor" {
			t.Fatalf("BFS graph expansion leaked beta project entry into alpha recall: %+v", results)
		}
	}
}

func TestRecallWithBFS_GraphExpansionPreservesCategoryAndOwner(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "bfs-owner-seed", Content: "bfs instruction policy seed", Category: CategoryInstruction, Scope: ScopeGlobal, OwnerID: "userA", CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "bfs-wrong-category", Content: "bfs project neighbor", Category: CategoryProjectKnowledge, Scope: ScopeGlobal, OwnerID: "userA", CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "bfs-wrong-owner", Content: "bfs instruction foreign owner", Category: CategoryInstruction, Scope: ScopeGlobal, OwnerID: "userB", CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()
	store.graph.link("bfs-owner-seed", "bfs-wrong-category", 1)
	store.graph.link("bfs-owner-seed", "bfs-wrong-owner", 1)

	results := store.RecallWithBFS("bfs instruction policy seed", CategoryInstruction, "", 1, "userA")
	for _, entry := range results {
		if entry.ID == "bfs-wrong-category" || entry.ID == "bfs-wrong-owner" {
			t.Fatalf("BFS graph expansion bypassed category/owner visibility: %+v", results)
		}
	}
}

func TestRecallDynamicProjectScopeNormalizesPathSeparators(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "normalized-project", Content: "normalized project separator target", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\workprj\alpha\`}, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "other-project", Content: "normalized project separator target", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\workprj\beta`}, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()

	results := store.RecallDynamic("normalized project separator target", "", `d:/workprj/alpha`)
	found := false
	for _, entry := range results {
		if entry.ID == "normalized-project" {
			found = true
		}
		if entry.ID == "other-project" {
			t.Fatalf("normalized project matching leaked another project: %+v", results)
		}
	}
	if !found {
		t.Fatalf("normalized project path should match equivalent Windows separators, got %+v", results)
	}
}

func TestSearchByModeProjectScopeNormalizesPathSeparators(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "direct-normalized", Content: "direct normalized", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\workprj\alpha\child`}, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()

	got := store.SearchByMode("direct-normalized", SearchDirect, CategoryProject, `d:/workprj/alpha`, 1)
	if len(got) != 1 || got[0].ID != "direct-normalized" {
		t.Fatalf("direct search should match normalized ancestor project path, got %+v", got)
	}
}

func TestProjectScopeKeepsDriveRootAsPath(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "drive-root", Content: "drive root scoped memory", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\`}, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{ID: "other-drive", Content: "drive root scoped memory", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`E:\`}, CreatedAt: now, UpdatedAt: now, Strength: 1},
	})
	store.mu.Unlock()

	results := store.RecallDynamic("drive root scoped memory", "", `D:\workprj\alpha`)
	found := false
	for _, entry := range results {
		if entry.ID == "drive-root" {
			found = true
		}
		if entry.ID == "other-drive" {
			t.Fatalf("drive-root project matching leaked another drive: %+v", results)
		}
	}
	if !found {
		t.Fatalf("drive root tag should remain a path ancestor, got %+v", results)
	}
}

func TestSemanticProjectPathMatchesPreservesFilesystemRoots(t *testing.T) {
	if !semanticProjectPathMatches(`D:\`, `d:/workprj/alpha`) {
		t.Fatal("drive root should match descendants after normalization")
	}
	if !semanticProjectPathMatches(`/`, `/workprj/alpha`) {
		t.Fatal("unix root should match descendants after normalization")
	}
	if semanticProjectPathMatches(`E:\`, `d:/workprj/alpha`) {
		t.Fatal("different drive roots should not match")
	}
}
