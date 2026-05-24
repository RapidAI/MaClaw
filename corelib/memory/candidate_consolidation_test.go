package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestConsolidateMemoryCandidatesPromotesDurableCandidate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entry := Entry{
		Content:    "Project API endpoint is https://api.example.com and the build command is pnpm test",
		Category:   CategoryProjectKnowledge,
		Tags:       []string{memoryCandidateTag, "api"},
		Status:     StatusDormant,
		SourceType: memoryCandidateTag,
	}
	if err := store.Save(entry); err != nil {
		t.Fatal(err)
	}
	if got := store.RecallDynamic("api endpoint pnpm test", CategoryProjectKnowledge, ""); len(got) != 0 {
		t.Fatalf("dormant candidate should not be recalled before consolidation, got %+v", got)
	}

	result := store.ConsolidateMemoryCandidates(context.Background())
	if result.Scanned != 1 || result.Promoted != 1 {
		t.Fatalf("expected one promoted candidate, got %+v", result)
	}

	entries := store.List(CategoryProjectKnowledge, "api.example.com")
	if len(entries) != 1 {
		t.Fatalf("expected one entry after promotion, got %+v", entries)
	}
	if entries[0].Status != StatusActive || hasTag(entries[0].Tags, memoryCandidateTag) {
		t.Fatalf("candidate should be active and untagged after promotion, got %+v", entries[0])
	}
	if got := store.RecallDynamic("api endpoint pnpm test", CategoryProjectKnowledge, ""); len(got) == 0 {
		t.Fatalf("promoted candidate should be recallable")
	}
}

func TestConsolidateMemoryCandidatesMarksRejectedStale(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:  "thanks",
		Category: CategoryProjectKnowledge,
		Tags:     []string{memoryCandidateTag},
		Status:   StatusDormant,
	}); err != nil {
		t.Fatal(err)
	}

	result := store.ConsolidateMemoryCandidates(context.Background())
	if result.Scanned != 1 || result.Rejected != 1 {
		t.Fatalf("expected one rejected candidate, got %+v", result)
	}
	entries := store.List(CategoryProjectKnowledge, "thanks")
	if len(entries) != 1 || entries[0].Status != StatusDormant || !entries[0].Stale {
		t.Fatalf("rejected candidate should remain dormant and stale, got %+v", entries)
	}
	if got := store.RecallDynamic("thanks", CategoryProjectKnowledge, ""); len(got) != 0 {
		t.Fatalf("rejected candidate should not be recalled, got %+v", got)
	}
}

func TestConsolidateMemoryCandidatesPersistsPromoteRejectThroughSQLiteBatch(t *testing.T) {
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

	if err := store.Save(Entry{ID: "candidate-promote", Content: "Project API endpoint is https://api.example.com and build command is pnpm test", Category: CategoryProjectKnowledge, Tags: []string{memoryCandidateTag, "api"}, Status: StatusDormant, SourceType: memoryCandidateTag}); err != nil {
		t.Fatalf("save promote: %v", err)
	}
	if err := store.Save(Entry{ID: "candidate-reject", Content: "thanks", Category: CategoryProjectKnowledge, Tags: []string{memoryCandidateTag}, Status: StatusDormant}); err != nil {
		t.Fatalf("save reject: %v", err)
	}

	result := store.ConsolidateMemoryCandidates(context.Background())
	if result.Scanned != 2 || result.Promoted != 1 || result.Rejected != 1 || len(result.Errors) != 0 {
		t.Fatalf("unexpected consolidation result: %+v", result)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	seen := map[string]Entry{}
	for _, entry := range loaded {
		seen[entry.ID] = entry
	}
	promoted := seen["candidate-promote"]
	if promoted.Status != StatusActive || hasTag(promoted.Tags, memoryCandidateTag) || promoted.SourceType != "memory_governed" {
		t.Fatalf("promoted candidate state not persisted: %+v", promoted)
	}
	rejected := seen["candidate-reject"]
	if rejected.Status != StatusDormant || !rejected.Stale {
		t.Fatalf("rejected candidate state not persisted: %+v", rejected)
	}
}

func TestConsolidateMemoryCandidatesMergesDuplicateIntoActive(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	active := Entry{
		ID:        "active-api",
		Content:   "Project API endpoint is https://api.example.com and build command is pnpm test",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"api"},
		Status:    StatusActive,
		Strength:  1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	candidate := Entry{
		ID:        "candidate-api",
		Content:   "API endpoint is https://api.example.com and build command is pnpm test",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{memoryCandidateTag, "endpoint"},
		Status:    StatusDormant,
		Strength:  1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	insertTestEntries(store, active, candidate)

	result := store.ConsolidateMemoryCandidates(context.Background())
	if result.Scanned != 1 || result.Merged != 1 {
		t.Fatalf("expected duplicate candidate merge, got %+v", result)
	}
	entries := store.List(CategoryProjectKnowledge, "api.example.com")
	if len(entries) != 1 {
		t.Fatalf("expected only active entry after merge, got %+v", entries)
	}
	if entries[0].ID != "active-api" || !hasTag(entries[0].Tags, "endpoint") || hasTag(entries[0].Tags, memoryCandidateTag) {
		t.Fatalf("active entry should absorb non-candidate tags, got %+v", entries[0])
	}
}

func TestConsolidateMemoryCandidatesDuplicateMergePersistsUpdateAndDeleteThroughSQLiteBatch(t *testing.T) {
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
	active := Entry{
		ID:        "active-api-sqlite",
		Content:   "Project API endpoint is https://api.example.com and build command is pnpm test",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"api"},
		Status:    StatusActive,
		Strength:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	candidate := Entry{
		ID:        "candidate-api-sqlite",
		Content:   "API endpoint is https://api.example.com and build command is pnpm test",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{memoryCandidateTag, "endpoint"},
		Status:    StatusDormant,
		Strength:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	insertTestEntries(store, active, candidate)
	if err := backend.SaveEntry(&active); err != nil {
		t.Fatalf("persist active: %v", err)
	}
	if err := backend.SaveEntry(&candidate); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}

	result := store.ConsolidateMemoryCandidates(context.Background())
	if result.Scanned != 1 || result.Merged != 1 || len(result.Errors) != 0 {
		t.Fatalf("expected duplicate candidate merge, got %+v", result)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	seen := map[string]Entry{}
	for _, entry := range loaded {
		seen[entry.ID] = entry
	}
	merged := seen["active-api-sqlite"]
	if merged.ID == "" || !hasTag(merged.Tags, "endpoint") || hasTag(merged.Tags, memoryCandidateTag) {
		t.Fatalf("active entry should persist absorbed candidate tags, got %+v", merged)
	}
	if _, ok := seen["candidate-api-sqlite"]; ok {
		t.Fatalf("merged candidate should be absent from LoadAll: %+v", seen["candidate-api-sqlite"])
	}
	modified, deleted, err := backend.Since(0)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "candidate-api-sqlite" {
		t.Fatalf("candidate delete should be visible in same sync stream, modified=%d deleted=%v", len(modified), deleted)
	}
}

func TestMemoryCandidateHealthCountsGovernanceActions(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	insertTestEntries(store,
		Entry{ID: "accept", Content: "Project API endpoint is https://api.example.com and build command is pnpm test", Category: CategoryProjectKnowledge, Tags: []string{memoryCandidateTag}, Status: StatusDormant},
		Entry{ID: "quarantine", Content: "I just ran the current task and it is probably okay", Category: CategoryProjectKnowledge, Tags: []string{memoryCandidateTag}, Status: StatusDormant},
		Entry{ID: "reject", Content: "thanks", Category: CategoryProjectKnowledge, Tags: []string{memoryCandidateTag}, Status: StatusDormant, Stale: true},
	)

	health := store.MemoryCandidateHealth()
	if health.Total != 3 || health.Accept != 1 || health.Quarantine != 1 || health.Reject != 1 || health.Stale != 1 {
		t.Fatalf("unexpected candidate health: %+v", health)
	}
}

func TestPipelineRunOnceIncludesCandidateConsolidation(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:    "Project database config uses postgres 16 and migration command is pnpm db:migrate",
		Category:   CategoryProjectKnowledge,
		Tags:       []string{memoryCandidateTag, "postgres"},
		Status:     StatusDormant,
		SourceType: memoryCandidateTag,
	}); err != nil {
		t.Fatal(err)
	}

	pipeline := NewPipeline(store, nil, nil, nil, nil)
	result := pipeline.RunOnce(context.Background())
	if result.Candidates == nil || result.Candidates.Promoted != 1 {
		t.Fatalf("expected pipeline candidate promotion result, got %+v", result.Candidates)
	}
}

func insertTestEntries(store *Store, entries ...Entry) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries = append(store.entries, entries...)
	store.rebuildDerivedIndexesLocked(true)
}
