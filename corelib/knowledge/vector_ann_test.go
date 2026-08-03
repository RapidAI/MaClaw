package knowledge

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestApproximateVectorSearchIsOptInAndKeepsTopKParity(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetEmbedder(directionalKnowledgeEmbedder{})
	now := time.Now().UTC()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "ann-source", Kind: SourceKindText, URI: "ann://source", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := insertSource(ctx, tx, source); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		vector := []float32{0, 1}
		if i == 299 {
			vector = []float32{1, 0}
		}
		id := fmt.Sprintf("ann-card-%03d", i)
		if _, err := tx.ExecContext(ctx, insertCardSQL, id, source.ID, nil, id, id, "", "[]", "[]", "[]", "", "", "", "", "", .8, 1, .8, float32SliceToBytes(vector), formatTime(now), formatTime(now)); err != nil {
			t.Fatal(err)
		}
		if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityCard, id, embeddingModelIdentifier(directionalKnowledgeEmbedder{}), 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	exact, err := store.searchByEmbedding(ctx, SearchOptions{Query: "target semantic query", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if store.approximateVectorSearchEnabled() {
		t.Fatal("ANN must be disabled by default")
	}
	store.SetApproximateVectorSearch(true)
	approximate, err := store.searchByEmbedding(ctx, SearchOptions{Query: "target semantic query", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) == 0 || len(approximate) == 0 || exact[0].CardID != "ann-card-299" || approximate[0].CardID != exact[0].CardID {
		t.Fatalf("exact=%#v approximate=%#v", exact, approximate)
	}
}

func TestVectorANNCandidatesFallBackToExactForSparseBucket(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetApproximateVectorSearch(true)
	values := make([]vectorANNVector, 0, 300)
	for i := 0; i < 299; i++ {
		values = append(values, vectorANNVector{key: fmt.Sprintf("noise-%03d", i), vector: []float32{0, 1}})
	}
	values = append(values, vectorANNVector{key: "target", vector: []float32{1, 0}})
	indexes := store.vectorANNCandidates("card:model", 0, values, []float32{1, 0}, 3)
	if len(indexes) == 0 || indexes[0] != 299 {
		t.Fatalf("candidate indexes = %v, expected exact fallback target first", indexes)
	}
}

func TestVectorANNCandidatesCachesOnlyIdenticalCandidateSpaces(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetApproximateVectorSearch(true)
	values := make([]vectorANNVector, 0, 256)
	for i := 0; i < 256; i++ {
		vector := []float32{0, 1}
		if i == 255 {
			vector = []float32{1, 0}
		}
		values = append(values, vectorANNVector{key: fmt.Sprintf("row-%03d", i), vector: vector})
	}
	_ = store.vectorANNCandidates("row:model", 7, values, []float32{1, 0}, 3)
	_ = store.vectorANNCandidates("row:model", 7, values, []float32{1, 0}, 3)
	store.vectorANN.mu.RLock()
	cached := len(store.vectorANN.spaces)
	store.vectorANN.mu.RUnlock()
	if cached != 1 {
		t.Fatalf("cached spaces = %d, want 1", cached)
	}
	values[0] = vectorANNVector{key: "row-replaced", vector: []float32{0, 1}}
	_ = store.vectorANNCandidates("row:model", 7, values, []float32{1, 0}, 3)
	store.vectorANN.mu.RLock()
	cached = len(store.vectorANN.spaces)
	store.vectorANN.mu.RUnlock()
	if cached != 2 {
		t.Fatalf("cached spaces after candidate change = %d, want 2", cached)
	}
}

func TestVectorANNSpaceKeySeparatesDelimiterContainingCallerIDs(t *testing.T) {
	// These two inputs produced the same delimiter-only key before the cache key
	// used length-prefixed fields. Source/card IDs may be supplied by callers, so
	// they cannot be assumed to exclude punctuation used by an internal key.
	first := vectorANNSpaceKey("scope|7|entry", 1, []vectorANNVector{{key: "id", vector: []float32{1, 0}}})
	second := vectorANNSpaceKey("scope", 7, []vectorANNVector{{key: "entry|1|id", vector: []float32{1, 0}}})
	if first == second {
		t.Fatalf("distinct caller-controlled scope identities share cache key %q", first)
	}
}

func TestVectorANNCacheIsBoundedAcrossCandidateSpaces(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetApproximateVectorSearch(true)
	values := make([]vectorANNVector, 256)
	for i := range values {
		values[i] = vectorANNVector{key: fmt.Sprintf("row-%03d", i), vector: []float32{1, 0}}
	}
	for space := 0; space < vectorANNMaxCachedSpaces+3; space++ {
		values[0].key = fmt.Sprintf("scope-%d", space)
		_ = store.vectorANNCandidates("row:model", 1, values, []float32{1, 0}, 3)
	}
	store.vectorANN.mu.RLock()
	cached := len(store.vectorANN.spaces)
	store.vectorANN.mu.RUnlock()
	if cached > vectorANNMaxCachedSpaces {
		t.Fatalf("cached spaces = %d, limit = %d", cached, vectorANNMaxCachedSpaces)
	}
}

func TestVectorANNCacheEvictsLeastRecentlyUsedSpace(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetApproximateVectorSearch(true)
	values := make([]vectorANNVector, 256)
	for i := range values {
		values[i] = vectorANNVector{key: fmt.Sprintf("row-%03d", i), vector: []float32{1, 0}}
	}
	for space := 0; space < vectorANNMaxCachedSpaces; space++ {
		values[0].key = fmt.Sprintf("scope-%d", space)
		_ = store.vectorANNCandidates("row:model", 1, values, []float32{1, 0}, 3)
	}
	values[0].key = "scope-0"
	_ = store.vectorANNCandidates("row:model", 1, values, []float32{1, 0}, 3)
	values[0].key = "scope-new"
	_ = store.vectorANNCandidates("row:model", 1, values, []float32{1, 0}, 3)

	keyOneValues := append([]vectorANNVector(nil), values...)
	keyOneValues[0].key = "scope-1"
	keyZeroValues := append([]vectorANNVector(nil), values...)
	keyZeroValues[0].key = "scope-0"
	store.vectorANN.mu.RLock()
	_, evicted := store.vectorANN.spaces[vectorANNSpaceKey("row:model", 1, keyOneValues)]
	_, retained := store.vectorANN.spaces[vectorANNSpaceKey("row:model", 1, keyZeroValues)]
	store.vectorANN.mu.RUnlock()
	if evicted || !retained {
		t.Fatalf("LRU cache states: scope-1 present=%v, scope-0 present=%v", evicted, retained)
	}
}

func TestVectorANNCacheSeparatesEmbedderGenerations(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetApproximateVectorSearch(true)
	values := make([]vectorANNVector, 256)
	for i := range values {
		values[i] = vectorANNVector{key: fmt.Sprintf("row-%03d", i), vector: []float32{1, 0}}
	}
	_ = store.vectorANNCandidates("row:model", 3, values, []float32{1, 0}, 3)
	_ = store.vectorANNCandidates("row:model", 4, values, []float32{1, 0}, 3)
	store.vectorANN.mu.RLock()
	cached := len(store.vectorANN.spaces)
	store.vectorANN.mu.RUnlock()
	if cached != 2 {
		t.Fatalf("cached generation spaces = %d, want 2", cached)
	}
}

func TestVectorIndexStatsReportsOptInMode(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if stats := store.VectorIndexStats(); stats.Enabled || stats.Backend != "exact_cosine" || stats.Fallback != "exact_cosine" {
		t.Fatalf("default stats = %#v", stats)
	}
	store.SetApproximateVectorSearch(true)
	if stats := store.VectorIndexStats(); !stats.Enabled || stats.Backend != "lsh_candidate" || stats.Fallback != "exact_cosine" {
		t.Fatalf("enabled stats = %#v", stats)
	}
}

func TestVectorANNCandidatesRejectMalformedVectors(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetApproximateVectorSearch(true)
	values := []vectorANNVector{
		{key: "valid", vector: []float32{1, 0}},
		{key: "nan", vector: []float32{float32(math.NaN()), 1}},
		{key: "zero", vector: []float32{0, 0}},
	}
	indexes := store.vectorANNCandidates("card:model", 0, values, []float32{1, 0}, 3)
	if len(indexes) != 1 || indexes[0] != 0 {
		t.Fatalf("candidate indexes = %v, want only valid vector", indexes)
	}
	if got := cosineSimilarity([]float32{1, 0}, []float32{float32(math.NaN()), 1}); got != 0 {
		t.Fatalf("non-finite cosine = %v, want 0", got)
	}
}

func TestVectorANNCandidatesFallBackWhenBucketContainsOnlyMalformedVectors(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetApproximateVectorSearch(true)

	// +Inf hashes into the query's bucket but is not a scoreable embedding.
	// The valid target hashes elsewhere, so the exact fallback is required to
	// avoid treating a large corrupt bucket as evidence of sufficient recall.
	values := make([]vectorANNVector, 0, 256)
	for i := 0; i < 255; i++ {
		values = append(values, vectorANNVector{key: fmt.Sprintf("invalid-%03d", i), vector: []float32{float32(math.Inf(1)), 0}})
	}
	values = append(values, vectorANNVector{key: "valid-target", vector: []float32{-1, 0}})
	indexes := store.vectorANNCandidates("card:model", 0, values, []float32{1, 0}, 3)
	if len(indexes) != 1 || indexes[0] != 255 {
		t.Fatalf("candidate indexes = %v, want exact fallback valid target", indexes)
	}
}

func TestStatsExposeMultilingualAndVectorIndexObservability(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "observed-source", Kind: SourceKindText, URI: "stats://observed", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "observed-node", SourceID: "observed-source", Type: "paragraph", Text: "日本語の検索品質を確認する", Metadata: map[string]string{"language": "ja", "script": "Jpan"}}); err != nil {
		t.Fatal(err)
	}
	store.SetApproximateVectorSearch(true)
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Languages["ja"] != 1 || stats.Scripts["Jpan"] != 1 || !stats.VectorIndex.Enabled || stats.VectorIndex.Backend != "lsh_candidate" {
		t.Fatalf("stats = %#v", stats)
	}
}
