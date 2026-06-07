package memory

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStagedRecall_AllStagesComplete(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Save entries with known content for BM25 matching.
	entries := []Entry{
		{Content: "PostgreSQL database configuration and setup guide", Category: CategoryProjectKnowledge, Tags: []string{"postgresql", "database"}, CreatedAt: time.Now()},
		{Content: "Redis caching strategy for session management", Category: CategoryProjectKnowledge, Tags: []string{"redis", "caching"}, CreatedAt: time.Now()},
		{Content: "Docker containerization best practices", Category: CategoryProjectKnowledge, Tags: []string{"docker", "containers"}, CreatedAt: time.Now()},
	}
	for _, e := range entries {
		if err := store.Save(e); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	pipeline := &StagedRecallPipeline{}
	// Use a generous deadline so all stages complete.
	deadline := time.Now().Add(10 * time.Second)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	result := pipeline.Recall(context.Background(), store, "PostgreSQL database", opts, deadline)

	if result.StageReached != StageFull {
		t.Errorf("expected StageReached=%s, got %s", StageFull, result.StageReached)
	}
	if result.Partial {
		t.Error("expected Partial=false when all stages complete")
	}
	if result.Elapsed < 0 {
		t.Error("expected non-negative Elapsed duration")
	}
	if len(result.Entries) == 0 {
		t.Error("expected at least 1 entry in result")
	}
	// The PostgreSQL entry should be present.
	found := false
	for _, e := range result.Entries {
		if strings.Contains(e.Content, "PostgreSQL") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected PostgreSQL entry in results")
	}
}

func TestStagedRecall_BM25OnlyOnTightDeadline(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:   "kubernetes cluster management and scaling",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"kubernetes", "cluster"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	pipeline := &StagedRecallPipeline{}
	// Deadline already in the past — should return BM25-only.
	deadline := time.Now().Add(-1 * time.Second)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	result := pipeline.Recall(context.Background(), store, "kubernetes cluster", opts, deadline)

	if result.StageReached != StageBM25Only {
		t.Errorf("expected StageReached=%s, got %s", StageBM25Only, result.StageReached)
	}
	if !result.Partial {
		t.Error("expected Partial=true when only BM25 stage completed")
	}
	// Should still return BM25 results.
	if len(result.Entries) == 0 {
		t.Error("expected at least 1 entry from BM25 stage")
	}
}

func TestStagedRecall_PartialResultsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:   "machine learning model training pipeline",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"ml", "training"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	pipeline := &StagedRecallPipeline{}
	// Cancel context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deadline := time.Now().Add(10 * time.Second)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	result := pipeline.Recall(ctx, store, "machine learning", opts, deadline)

	// With cancelled context, should stop after BM25 stage.
	if result.StageReached != StageBM25Only {
		t.Errorf("expected StageReached=%s on cancelled context, got %s", StageBM25Only, result.StageReached)
	}
	if !result.Partial {
		t.Error("expected Partial=true on cancelled context")
	}
}

func TestStagedRecall_EmptyStoreReturnsPartial(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	pipeline := &StagedRecallPipeline{}
	deadline := time.Now().Add(10 * time.Second)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	result := pipeline.Recall(context.Background(), store, "anything", opts, deadline)

	// Empty store: all stages complete with no entries.
	if result.StageReached != StageFull {
		t.Errorf("expected StageReached=%s for empty store, got %s", StageFull, result.StageReached)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected 0 entries for empty store, got %d", len(result.Entries))
	}
}

func TestStagedRecall_NilStoreReturnsPartial(t *testing.T) {
	pipeline := &StagedRecallPipeline{}
	deadline := time.Now().Add(10 * time.Second)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	result := pipeline.Recall(context.Background(), nil, "query", opts, deadline)

	if result.StageReached != StageBM25Only {
		t.Errorf("expected StageReached=%s for nil store, got %s", StageBM25Only, result.StageReached)
	}
	if !result.Partial {
		t.Error("expected Partial=true for nil store")
	}
}

func TestStagedRecall_EmptyQueryReturnsPartial(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	pipeline := &StagedRecallPipeline{}
	deadline := time.Now().Add(10 * time.Second)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	result := pipeline.Recall(context.Background(), store, "", opts, deadline)

	if result.StageReached != StageBM25Only {
		t.Errorf("expected StageReached=%s for empty query, got %s", StageBM25Only, result.StageReached)
	}
	if !result.Partial {
		t.Error("expected Partial=true for empty query")
	}
}

func TestStagedRecall_LogsPerfFormat(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:   "testing log output format verification",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"testing"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Capture log output.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	origFlags := log.Flags()
	log.SetFlags(0)
	defer func() {
		log.SetFlags(origFlags)
		log.SetOutput(os.Stderr)
	}()

	pipeline := &StagedRecallPipeline{}
	deadline := time.Now().Add(10 * time.Second)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	pipeline.Recall(context.Background(), store, "testing log", opts, deadline)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "[perf] staged_recall stage=") {
		t.Errorf("expected [perf] staged_recall log line, got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "elapsed=") {
		t.Errorf("expected elapsed= in log output, got: %q", logOutput)
	}
}

func TestStagedRecall_RespectsOwnerIDIsolation(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Save entry owned by user-A.
	if err := store.SaveForUser(Entry{
		Content:   "secret project alpha details",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"alpha", "secret"},
		CreatedAt: time.Now(),
	}, "user-A"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	pipeline := &StagedRecallPipeline{}
	deadline := time.Now().Add(10 * time.Second)

	// Query as user-B should not see user-A's entries.
	opts := ProactiveRecallOptions{MaxEntries: 12, OwnerID: "user-B"}
	result := pipeline.Recall(context.Background(), store, "secret project alpha", opts, deadline)

	for _, e := range result.Entries {
		if strings.Contains(e.Content, "secret project alpha") {
			t.Error("user-B should not see user-A's entries")
		}
	}
}

func TestStagedRecall_BM25StageWithinTimeBudget(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "memories.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Add a moderate number of entries to make timing meaningful.
	for i := 0; i < 50; i++ {
		_ = store.Save(Entry{
			Content:   "entry about various programming topics and frameworks number " + strings.Repeat("x", i%20),
			Category:  CategoryProjectKnowledge,
			Tags:      []string{"programming"},
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}

	pipeline := &StagedRecallPipeline{}
	// Give just enough time for BM25 but force a deadline check before vector.
	deadline := time.Now().Add(stageBM25Budget + 50*time.Millisecond)
	opts := ProactiveRecallOptions{MaxEntries: 12}

	start := time.Now()
	result := pipeline.Recall(context.Background(), store, "programming topics", opts, deadline)
	elapsed := time.Since(start)

	// BM25 stage should complete well within 200ms for 50 entries.
	if elapsed > 200*time.Millisecond {
		t.Errorf("BM25 stage took too long: %v (expected < 200ms)", elapsed)
	}
	// Result should have entries from BM25.
	if len(result.Entries) == 0 {
		t.Error("expected entries from BM25 stage")
	}
	// Stage should be at least bm25_only (might reach further if fast enough).
	validStages := map[string]bool{StageBM25Only: true, StageBM25Vec: true, StageFull: true}
	if !validStages[result.StageReached] {
		t.Errorf("unexpected StageReached: %s", result.StageReached)
	}
}
