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
