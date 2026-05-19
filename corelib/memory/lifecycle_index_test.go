package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConflictSupersedeSynchronizesSemanticGraph(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entry := Entry{
		ID:        "old-config",
		Content:   "alpha ssh port is 22",
		Category:  CategoryProjectKnowledge,
		Entities:  []string{"entity:alpha", "relation:config_of", "entity:ssh-port-22"},
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	if err := store.Save(entry); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	before := store.SemanticGraph().SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: now})
	if len(before) == 0 || before[0].EntryID != "old-config" {
		t.Fatalf("expected active semantic fact before supersede, got %+v", before)
	}

	NewConflictDetector(store, nil, nil).Supersede("old-config")

	after := store.SemanticGraph().SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: time.Now()})
	for _, hit := range after {
		if hit.EntryID == "old-config" {
			t.Fatalf("superseded fact should not be visible in current semantic recall: %+v", after)
		}
	}
	if got := store.FindByEntity("alpha"); len(got) != 0 {
		t.Fatalf("superseded entry should be removed from active entity index, got %+v", got)
	}

	store.mu.RLock()
	var invalidAt *time.Time
	for i := range store.entries {
		if store.entries[i].ID == "old-config" {
			invalidAt = store.entries[i].InvalidAt
			break
		}
	}
	store.mu.RUnlock()
	if invalidAt == nil {
		t.Fatal("superseded fact should record InvalidAt for as-of semantics")
	}
	asOfBefore := invalidAt.Add(-time.Nanosecond)
	historical := store.SemanticGraph().SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{
		Now:          asOfBefore,
		AsOf:         &asOfBefore,
		TemporalMode: SemanticTemporalAsOf,
	})
	foundHistorical := false
	for _, hit := range historical {
		if hit.EntryID == "old-config" {
			foundHistorical = true
			break
		}
	}
	if !foundHistorical {
		t.Fatalf("as-of recall before supersede should still see old fact, got %+v", historical)
	}
}

func TestSupersedeEnsuresNonEmptyValidityWindow(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	created := time.Now()
	store.SetEntries([]Entry{{
		ID:        "instant-old",
		Content:   "alpha port is 22",
		Category:  CategoryProjectKnowledge,
		CreatedAt: created,
		UpdatedAt: created,
		Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-22"},
	}})
	store.mu.Lock()
	changed := store.supersedeEntryLocked("instant-old", created)
	store.mu.Unlock()
	if !changed {
		t.Fatal("expected supersede to change entry")
	}
	store.mu.RLock()
	invalidAt := store.entries[0].InvalidAt
	store.mu.RUnlock()
	if invalidAt == nil || !invalidAt.After(created) {
		t.Fatalf("supersede should create a non-empty validity window, created=%v invalid=%v", created, invalidAt)
	}
}
func TestPipelineDormantRebuildsDerivedIndexes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	old := time.Now().Add(-90 * 24 * time.Hour)
	store.SetEntries([]Entry{
		{
			ID:           "dormant-candidate",
			Content:      "alpha project config",
			Category:     CategoryProjectKnowledge,
			SourceURL:    "D:\\workprj\\alpha\\readme.md",
			Entities:     []string{"entity:alpha", "relation:config_of", "entity:project-config"},
			Embedding:    []float32{1, 0, 0, 0},
			RelatedIDs:   []string{"active-neighbor"},
			RelatedEdges: []RelatedEdge{{ID: "active-neighbor", Strength: 0.9}},
			Strength:     0.02,
			CreatedAt:    old,
			UpdatedAt:    old,
		},
		{
			ID:        "active-neighbor",
			Content:   "active neighbor",
			Category:  CategoryProjectKnowledge,
			Strength:  1,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	})
	if store.ProjectIndex().Count() != 1 {
		t.Fatalf("expected project index before decay, got %d", store.ProjectIndex().Count())
	}
	if got := store.FindByEntity("alpha"); len(got) != 1 {
		t.Fatalf("expected entity index before decay, got %+v", got)
	}

	pipeline := NewPipeline(store, nil, nil, nil, nil)
	result := pipeline.RunOnce(context.Background())
	if result.Dormant != 1 {
		t.Fatalf("expected one entry to become dormant, got %d", result.Dormant)
	}
	if store.ProjectIndex().Count() != 0 {
		t.Fatalf("dormant project entry should be removed from project index, got %d", store.ProjectIndex().Count())
	}
	if scores := store.vecIndex.score([]float32{1, 0, 0, 0}); scores["dormant-candidate"] != 0 {
		t.Fatalf("dormant entry should be removed from vector index, got %v", scores)
	}
	if neighbors := store.GraphNeighbors("dormant-candidate"); len(neighbors) != 0 {
		t.Fatalf("dormant entry should be removed from legacy graph, got %v", neighbors)
	}
	if got := store.FindByEntity("alpha"); len(got) != 0 {
		t.Fatalf("dormant entry should be removed from active entity index, got %+v", got)
	}
	hits := store.SemanticGraph().SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: time.Now()})
	for _, hit := range hits {
		if hit.EntryID == "dormant-candidate" {
			t.Fatalf("dormant entry should not be visible in current semantic graph, got %+v", hits)
		}
	}
}

func TestDerivedIndexRebuildsSkipInactiveEntries(t *testing.T) {
	active := Entry{
		ID:           "active",
		Content:      "active alpha endpoint",
		Category:     CategoryProjectKnowledge,
		Embedding:    []float32{1, 0, 0, 0},
		RelatedIDs:   []string{"superseded"},
		RelatedEdges: []RelatedEdge{{ID: "superseded", Strength: 0.9}},
	}
	superseded := Entry{
		ID:           "superseded",
		Content:      "old inactive alpha endpoint",
		Category:     CategoryProjectKnowledge,
		Status:       StatusSuperseded,
		Embedding:    []float32{1, 0, 0, 0},
		RelatedIDs:   []string{"active"},
		RelatedEdges: []RelatedEdge{{ID: "active", Strength: 0.9}},
	}
	dormant := Entry{
		ID:        "dormant",
		Content:   "dormant alpha endpoint",
		Category:  CategoryProjectKnowledge,
		Status:    StatusDormant,
		Embedding: []float32{1, 0, 0, 0},
	}
	entries := []Entry{active, superseded, dormant}

	bm := newBM25Index()
	bm.rebuild(entries)
	if scores := bm.score("inactive dormant"); scores["superseded"] != 0 || scores["dormant"] != 0 {
		t.Fatalf("inactive entries should not be in BM25 rebuild, got %v", scores)
	}
	if scores := bm.score("active alpha"); scores["active"] == 0 {
		t.Fatalf("active entry should remain searchable in BM25, got %v", scores)
	}

	vec := newVectorIndex()
	vec.rebuild(entries)
	if scores := vec.score([]float32{1, 0, 0, 0}); scores["superseded"] != 0 || scores["dormant"] != 0 || scores["active"] == 0 {
		t.Fatalf("vector rebuild should only index active embeddings, got %v", scores)
	}

	graph := newMemoryGraph()
	graph.rebuild(entries)
	if neighbors := graph.neighborsOf("active"); len(neighbors) != 0 {
		t.Fatalf("graph rebuild should drop edges to inactive entries, got %v", neighbors)
	}
	if neighbors := graph.neighborsOf("superseded"); len(neighbors) != 0 {
		t.Fatalf("graph rebuild should not create inactive nodes, got %v", neighbors)
	}
}

func TestSupersedeRemovesEntryFromVectorAndLegacyGraph(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	store.SetEntries([]Entry{
		{ID: "active", Content: "active alpha", Category: CategoryProjectKnowledge, Embedding: []float32{1, 0, 0, 0}, RelatedIDs: []string{"old"}, RelatedEdges: []RelatedEdge{{ID: "old", Strength: 0.9}}},
		{ID: "old", Content: "old alpha", Category: CategoryProjectKnowledge, Embedding: []float32{1, 0, 0, 0}, RelatedIDs: []string{"active"}, RelatedEdges: []RelatedEdge{{ID: "active", Strength: 0.9}}},
	})
	if len(store.GraphNeighbors("active")) == 0 {
		t.Fatal("expected graph edge before supersede")
	}

	NewConflictDetector(store, nil, nil).Supersede("old")

	if scores := store.vecIndex.score([]float32{1, 0, 0, 0}); scores["old"] != 0 {
		t.Fatalf("superseded entry should be removed from vector index, got %v", scores)
	}
	if neighbors := store.GraphNeighbors("active"); len(neighbors) != 0 {
		t.Fatalf("superseded graph neighbor should be removed, got %v", neighbors)
	}
}

func TestStoreUpdateRebuildsProjectIndexWhenProjectTagChanges(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "project-note",
		Content:  "project note",
		Category: CategoryProjectKnowledge,
		Tags:     []string{`D:\workprj\oldproj`},
	}); err != nil {
		t.Fatal(err)
	}
	if store.ProjectIndex().Get(`D:\workprj\oldproj`) == nil {
		t.Fatal("expected old project before update")
	}

	if err := store.Update("project-note", "project note moved", CategoryProjectKnowledge, []string{`D:\workprj
ewproj`}); err != nil {
		t.Fatal(err)
	}
	if rec := store.ProjectIndex().Get(`D:\workprj\oldproj`); rec != nil {
		t.Fatalf("old project should be removed after project tag update, got %+v", rec)
	}
	if rec := store.ProjectIndex().Get(`D:\workprj
ewproj`); rec == nil || rec.EntryCount != 1 {
		t.Fatalf("new project should be indexed after update, got %+v", rec)
	}
}

func TestCompressorDedupKeepsDifferentOwnersSeparate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	store.SetEntries([]Entry{
		{ID: "user-a", Content: "same preference for editor", Category: CategoryPreference, OwnerID: "userA"},
		{ID: "user-b", Content: "same preference for editor", Category: CategoryPreference, OwnerID: "userB"},
	})
	compressor := NewCompressor(store, nil, nil)
	if removed := compressor.dedup(); removed != 0 {
		t.Fatalf("dedup should not merge different non-empty owners, removed=%d", removed)
	}
	if entries := store.List("", ""); len(entries) != 2 {
		t.Fatalf("expected both owner-scoped entries to remain, got %+v", entries)
	}
}

func TestRebuildDerivedIndexesRefreshesTemporalTree(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Now()
	interval := TimeInterval{Start: now, End: now.Add(time.Minute)}
	store.SetEntries([]Entry{{
		ID:       "temporal-entry",
		Content:  "temporal entry",
		Category: CategoryConversationSummary,
		Level:    LevelSegment,
		Interval: &interval,
	}})
	if !store.TMT().Has("temporal-entry") {
		t.Fatal("expected temporal entry in TMT after SetEntries")
	}

	store.Lock()
	store.entries = nil
	store.rebuildDerivedIndexesLocked(false)
	store.Unlock()
	if store.TMT().Has("temporal-entry") {
		t.Fatal("temporal tree should be rebuilt after entries replacement")
	}
}

func TestSaveDedupMergesEntitiesIntoDerivedIndexes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "same-content",
		Content:  "alpha config note",
		Category: CategoryProjectKnowledge,
		Entities: []string{"entity:alpha", "relation:about", "entity:config"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:  "alpha config note",
		Category: CategoryProjectKnowledge,
		Entities: []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
	}); err != nil {
		t.Fatal(err)
	}

	if entries := store.List("", ""); len(entries) != 1 {
		t.Fatalf("expected hash dedup to keep one entry, got %+v", entries)
	}
	if got := store.FindByEntity("port-2222"); len(got) != 1 || got[0].ID != "same-content" {
		t.Fatalf("dedup should merge new entities into entity index, got %+v", got)
	}
	hits := store.SemanticGraph().SearchWithOptions([]string{"port-2222"}, SemanticSearchOptions{Now: time.Now()})
	if len(hits) == 0 || hits[0].EntryID != "same-content" {
		t.Fatalf("dedup should merge new entities into semantic graph, got %+v", hits)
	}
}

func TestSubstringDedupUpdatesEmbeddingAndEntities(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:        "short",
		Content:   "alpha endpoint",
		Category:  CategoryProjectKnowledge,
		Embedding: []float32{1, 0, 0, 0},
		Entities:  []string{"entity:alpha"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:   "alpha endpoint uses port 2222 and token auth",
		Category:  CategoryProjectKnowledge,
		Embedding: []float32{0, 1, 0, 0},
		Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
	}); err != nil {
		t.Fatal(err)
	}

	entries := store.List("", "")
	if len(entries) != 1 || entries[0].ID != "short" || entries[0].Content != "alpha endpoint uses port 2222 and token auth" {
		t.Fatalf("expected substring dedup to retain updated short entry, got %+v", entries)
	}
	if scores := store.vecIndex.score([]float32{0, 1, 0, 0}); scores["short"] == 0 {
		t.Fatalf("substring dedup should update vector embedding, got %v", scores)
	}
	if got := store.FindByEntity("port-2222"); len(got) != 1 || got[0].ID != "short" {
		t.Fatalf("substring dedup should merge entities, got %+v", got)
	}
}

func TestProfileUpdateRebuildsTemporalTree(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	interval := TimeInterval{Start: time.Now(), End: time.Now()}
	store.SetEntries([]Entry{{
		ID:       "profile",
		Content:  "old profile",
		Category: CategoryProfile,
		Level:    LevelSegment,
		Interval: &interval,
	}})
	pc := NewProfileConsolidator(store, store.TMT(), nil)
	if err := pc.upsertProfile("new profile", ""); err != nil {
		t.Fatal(err)
	}
	level, _, ok := store.TMT().NodeInfo("profile")
	if !ok || level != LevelProfile {
		t.Fatalf("profile update should rebuild TMT with LevelProfile, level=%v ok=%v", level, ok)
	}
}

func TestProfileUpdateStoresEvidenceBoundary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	evidence := []Entry{
		{ID: "week-1", Content: "weekly summary", Category: CategoryConversationSummary, Level: LevelWeek, OwnerID: "owner-1", SourceType: "summary", Status: StatusActive},
		{ID: "insight-1", Content: "prefers concise replies", Category: CategoryPreference, Tags: []string{"reflection"}, OwnerID: "owner-1", SourceType: "schema_consolidation", Status: StatusActive},
	}
	pc := NewProfileConsolidator(store, store.TMT(), nil)
	if err := pc.upsertProfile("new profile", "owner-1", evidence); err != nil {
		t.Fatal(err)
	}
	entries := store.List(CategoryProfile, "new profile")
	if len(entries) != 1 {
		t.Fatalf("expected profile entry, got %+v", entries)
	}
	got := entries[0]
	if got.DerivedKind != "profile" || got.SourceType != "profile_consolidation" {
		t.Fatalf("missing profile metadata: %+v", got)
	}
	if got.Level != LevelProfile || got.Interval == nil {
		t.Fatalf("profile should keep temporal profile metadata: level=%v interval=%+v", got.Level, got.Interval)
	}
	if strings.Join(got.EvidenceIDs, ",") != "week-1,insight-1" {
		t.Fatalf("unexpected evidence ids: %+v", got.EvidenceIDs)
	}
	if got.Boundary == nil || got.Boundary.OwnerID != "owner-1" {
		t.Fatalf("unexpected boundary: %+v", got.Boundary)
	}
}

func TestSetEntriesNormalizesMissingTimestamps(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	updated := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.SetEntries([]Entry{{
		ID:        "missing-created",
		Content:   "alpha port is 2222",
		Category:  CategoryProjectKnowledge,
		UpdatedAt: updated,
		Entities:  []string{"entity:alpha", "relation:config_of", "entity:port-2222"},
	}})

	store.mu.RLock()
	entry := store.entries[0]
	store.mu.RUnlock()
	if !entry.CreatedAt.Equal(updated) || !entry.UpdatedAt.Equal(updated) {
		t.Fatalf("SetEntries should normalize timestamps from UpdatedAt, got created=%v updated=%v", entry.CreatedAt, entry.UpdatedAt)
	}
	asOfBefore := updated.Add(-time.Nanosecond)
	if hits := store.SemanticGraph().SearchWithOptions([]string{"alpha"}, SemanticSearchOptions{Now: asOfBefore, AsOf: &asOfBefore, TemporalMode: SemanticTemporalAsOf}); len(hits) != 0 {
		t.Fatalf("normalized created timestamp should keep future facts out of as-of recall, got %+v", hits)
	}
}

func TestProfileConsolidatorGateSkipsSingleEvidence(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:         "week-1",
		Content:    "single weekly summary should not form a durable profile yet",
		Category:   CategoryConversationSummary,
		Level:      LevelWeek,
		OwnerID:    "owner-1",
		SourceType: "summary",
		Status:     StatusActive,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	llm := &recordingExperienceProtectionLLM{response: "profile text"}
	pc := NewProfileConsolidator(store, store.TMT(), llm)
	result, err := pc.ConsolidateForOwner(context.Background(), "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.NodesCreated != 0 {
		t.Fatalf("NodesCreated = %d, want 0", result.NodesCreated)
	}
	if len(llm.messages) != 0 {
		t.Fatalf("profile LLM was called despite insufficient evidence: %+v", llm.messages)
	}
	if entries := store.List(CategoryProfile, "profile text"); len(entries) != 0 {
		t.Fatalf("unexpected profile entry: %+v", entries)
	}
}
