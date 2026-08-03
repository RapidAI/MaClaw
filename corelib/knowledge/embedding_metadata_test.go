package knowledge

import (
	"context"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type embeddingMetadataTestEmbedder struct{ model string }

func (e embeddingMetadataTestEmbedder) Embed(string) ([]float32, error) { return []float32{1, 0}, nil }
func (e embeddingMetadataTestEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}
func (e embeddingMetadataTestEmbedder) Dim() int        { return 2 }
func (e embeddingMetadataTestEmbedder) Close()          {}
func (e embeddingMetadataTestEmbedder) ModelID() string { return e.model }

type malformedBatchEmbedder struct {
	embeddingMetadataTestEmbedder
	vectors [][]float32
}

func (e malformedBatchEmbedder) EmbedBatch([]string) ([][]float32, error) { return e.vectors, nil }

func TestEmbeddingSearchRejectsVectorsFromDifferentModelID(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-a"})
	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "Model isolation", Text: "Embedding spaces must not mix."}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a'`).Scan(&count); err != nil || count == 0 {
		t.Fatalf("model-a metadata count = %d, err = %v", count, err)
	}
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-b"})
	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "semantic query", Limit: 5})
	if err != nil {
		t.Fatalf("searchByEmbedding: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("mixed-model results = %#v, want none", results)
	}
}

func TestSetEmbedderRefreshesEmbeddingsForChangedModelID(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-a"})
	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "Refresh", Text: "Embedding metadata should migrate."}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	store.WaitBackground()
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-b"})
	store.WaitBackground()
	var stale int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a'`).Scan(&stale); err != nil {
		t.Fatalf("stale metadata: %v", err)
	}
	if stale != 0 {
		t.Fatalf("stale model-a metadata count = %d", stale)
	}
	var fresh int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-b'`).Scan(&fresh); err != nil || fresh == 0 {
		t.Fatalf("model-b metadata count = %d, err = %v", fresh, err)
	}
}

type blockingEmbeddingMetadataEmbedder struct {
	model   string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *blockingEmbeddingMetadataEmbedder) Embed(text string) ([]float32, error) {
	e.once.Do(func() { close(e.started) })
	<-e.release
	return []float32{1, 0}, nil
}

func (e *blockingEmbeddingMetadataEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.once.Do(func() { close(e.started) })
	<-e.release
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}
func (e *blockingEmbeddingMetadataEmbedder) Dim() int        { return 2 }
func (e *blockingEmbeddingMetadataEmbedder) Close()          {}
func (e *blockingEmbeddingMetadataEmbedder) ModelID() string { return e.model }

func TestSetEmbedderPreventsStaleBackgroundWrites(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "stale refresh", Text: "The second model must own the vector metadata."}); err != nil {
		t.Fatal(err)
	}
	first := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(first)
	select {
	case <-first.started:
	case <-t.Context().Done():
		t.Fatal("first embedding refresh did not start")
	}
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-b"})
	close(first.release)
	store.WaitBackground()
	var stale int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("stale model-a metadata count = %d", stale)
	}
	var fresh int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-b'`).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh == 0 {
		t.Fatal("model-b did not refresh metadata")
	}
}

func TestModelSwitchCancelsStaleFullStoreRefresh(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "refresh cancellation", Text: "Old model work should stop after a model switch."}); err != nil {
		t.Fatal(err)
	}
	first := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(first)
	select {
	case <-first.started:
	case <-t.Context().Done():
		t.Fatal("first embedding refresh did not start")
	}

	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-b"})
	store.embeddingRefreshMu.Lock()
	active := store.embeddingRefreshCancel
	store.embeddingRefreshMu.Unlock()
	if active == nil {
		t.Fatal("new model refresh did not replace the stale cancellation handle")
	}
	close(first.release)
	store.WaitBackground()

	var stale, fresh int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-b'`).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if stale != 0 || fresh == 0 {
		t.Fatalf("metadata after switch: model-a=%d, model-b=%d", stale, fresh)
	}
}

func TestSourceNodeBackfillUsesOneEmbedderGeneration(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "generation-source", Kind: SourceKindText, URI: "manual://generation", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "generation-node", SourceID: "generation-source", Title: "Stable", Text: "The same model must query and write this node."}); err != nil {
		t.Fatal(err)
	}
	first := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(first)
	select {
	case <-first.started:
	case <-t.Context().Done():
		t.Fatal("model-a refresh did not start")
	}
	backfillDone := make(chan error, 1)
	go func() { backfillDone <- store.BackfillNodeEmbeddingsForSources(ctx, []string{"generation-source"}) }()
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-b"})
	close(first.release)
	select {
	case err := <-backfillDone:
		if err != nil {
			t.Fatalf("source backfill: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("source backfill did not finish")
	}
	store.WaitBackground()
	var model string
	if err := store.db.QueryRowContext(ctx, `SELECT model_id FROM knowledge_embedding_metadata WHERE entity_type = 'node' AND entity_id = 'generation-node'`).Scan(&model); err != nil {
		t.Fatal(err)
	}
	if model != "model-b" {
		t.Fatalf("node model = %q, want model-b", model)
	}
}

func TestEmbeddingSearchRejectsQueryWhenModelSwitchesDuringEmbed(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "query-switch-source", Kind: SourceKindText, URI: "manual://query-switch", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-a"})
	if err := store.SaveCard(ctx, Card{ID: "query-switch-card", SourceID: "query-switch-source", Title: "Stable vector", Claim: "Embedding queries must use one model generation.", Embedding: []float32{1, 0}}); err != nil {
		t.Fatal(err)
	}
	blocked := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.embedderMu.Lock()
	store.embedder = blocked
	store.embedderGeneration++
	store.embedderMu.Unlock()
	resultC := make(chan []SearchResult, 1)
	errC := make(chan error, 1)
	go func() {
		results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "switch", Limit: 5})
		resultC <- results
		errC <- err
	}()
	select {
	case <-blocked.started:
	case <-t.Context().Done():
		t.Fatal("query embedding did not start")
	}
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "model-b"})
	close(blocked.release)
	if err := <-errC; err != nil {
		t.Fatalf("searchByEmbedding: %v", err)
	}
	if results := <-resultC; len(results) != 0 {
		t.Fatalf("stale query model returned %#v", results)
	}
}

func TestClosePreventsConcurrentEmbedderRefreshStart(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "shutdown", Text: "A closing store must not start another refresh."}); err != nil {
		t.Fatal(err)
	}
	first := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(first)
	select {
	case <-first.started:
	case <-t.Context().Done():
		t.Fatal("initial embedding refresh did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- store.Close() }()
	for {
		store.embedderMu.RLock()
		closing := store.closed
		store.embedderMu.RUnlock()
		if closing {
			break
		}
		select {
		case <-t.Context().Done():
			t.Fatal("Close did not close the background launch gate")
		default:
		}
		time.Sleep(time.Millisecond)
	}

	second := &blockingEmbeddingMetadataEmbedder{model: "model-b", started: make(chan struct{}), release: make(chan struct{})}
	setDone := make(chan struct{})
	go func() {
		store.SetEmbedder(second)
		close(setDone)
	}()
	select {
	case <-setDone:
		// Close may win the embedder write lock first, in which case SetEmbedder
		// returns immediately without changing the model or launching work.
	case <-time.After(50 * time.Millisecond):
	}

	close(first.release)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("Close did not finish")
	}
	select {
	case <-setDone:
	case <-t.Context().Done():
		t.Fatal("SetEmbedder did not return after Close")
	}
	select {
	case <-second.started:
		t.Fatal("SetEmbedder started a refresh after Close")
	default:
	}
}

func TestSaveCardClearingEmbeddingRemovesModelMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "card-model"})
	if err := store.SaveSource(ctx, Source{ID: "clear-card-source", Kind: SourceKindText, URI: "manual://clear-card", Status: StatusParsed}); err != nil {
		t.Fatalf("save source: %v", err)
	}
	card := Card{ID: "clear-card", SourceID: "clear-card-source", Title: "Vector card", Claim: "A directly supplied vector."}
	card.Embedding = []float32{1, 0}
	if err := store.SaveCard(ctx, card); err != nil {
		t.Fatalf("save embedded card: %v", err)
	}
	var metadata int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'card' AND entity_id = 'clear-card'`).Scan(&metadata); err != nil || metadata != 1 {
		t.Fatalf("initial metadata = %d, err = %v", metadata, err)
	}
	card.Embedding = nil
	if err := store.SaveCard(ctx, card); err != nil {
		t.Fatalf("clear card vector: %v", err)
	}
	var vector []byte
	if err := store.db.QueryRowContext(ctx, `SELECT embedding FROM knowledge_cards WHERE id = 'clear-card'`).Scan(&vector); err != nil {
		t.Fatalf("read cleared vector: %v", err)
	}
	if len(vector) != 0 {
		t.Fatalf("cleared vector length = %d", len(vector))
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'card' AND entity_id = 'clear-card'`).Scan(&metadata); err != nil || metadata != 0 {
		t.Fatalf("remaining metadata = %d, err = %v", metadata, err)
	}
}

func TestSaveCardMismatchedEmbeddingRemovesStaleModelMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetEmbedder(embeddingMetadataTestEmbedder{model: "card-model"})
	if err := store.SaveSource(ctx, Source{ID: "mismatched-card-source", Kind: SourceKindText, URI: "manual://mismatched-card", Status: StatusParsed}); err != nil {
		t.Fatalf("save source: %v", err)
	}
	card := Card{ID: "mismatched-card", SourceID: "mismatched-card-source", Title: "Vector card", Claim: "A directly supplied vector.", Embedding: []float32{1, 0}}
	if err := store.SaveCard(ctx, card); err != nil {
		t.Fatalf("save matching card vector: %v", err)
	}
	card.Embedding = []float32{1, 0, 0}
	if err := store.SaveCard(ctx, card); err != nil {
		t.Fatalf("save mismatched card vector: %v", err)
	}
	var metadata int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'card' AND entity_id = 'mismatched-card'`).Scan(&metadata); err != nil || metadata != 0 {
		t.Fatalf("remaining metadata = %d, err = %v", metadata, err)
	}
	if err := store.backfillCardEmbeddings(ctx); err != nil {
		t.Fatalf("backfill mismatched card: %v", err)
	}
	var vector []byte
	if err := store.db.QueryRowContext(ctx, `SELECT embedding FROM knowledge_cards WHERE id = 'mismatched-card'`).Scan(&vector); err != nil {
		t.Fatalf("read refreshed vector: %v", err)
	}
	if len(vector) != 2*4 {
		t.Fatalf("refreshed vector bytes = %d, want %d", len(vector), 2*4)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'card' AND entity_id = 'mismatched-card' AND model_id = 'card-model' AND dimension = 2`).Scan(&metadata); err != nil || metadata != 1 {
		t.Fatalf("refreshed metadata = %d, err = %v", metadata, err)
	}
}

func TestSaveCardRejectsNonFiniteOrZeroEmbedding(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "invalid-card-source", Kind: SourceKindText, URI: "manual://invalid-card", Status: StatusParsed}); err != nil {
		t.Fatalf("save source: %v", err)
	}
	for _, test := range []struct {
		name   string
		vector []float32
	}{
		{name: "zero", vector: []float32{0, 0}},
		{name: "nan", vector: []float32{float32(math.NaN()), 1}},
		{name: "infinite", vector: []float32{float32(math.Inf(1)), 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := store.SaveCard(ctx, Card{ID: "invalid-card-" + test.name, SourceID: "invalid-card-source", Title: "Invalid vector", Claim: "must not persist", Embedding: test.vector})
			if err == nil {
				t.Fatal("SaveCard accepted invalid vector")
			}
			var count int
			if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE id = ?`, "invalid-card-"+test.name).Scan(&count); err != nil || count != 0 {
				t.Fatalf("persisted invalid card count = %d, err = %v", count, err)
			}
		})
	}
}

func TestEmbeddingBatchValidationRejectsCountAndVectorFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		vectors  [][]float32
		expected int
	}{
		{name: "short", vectors: [][]float32{{1, 0}}, expected: 2},
		{name: "long", vectors: [][]float32{{1, 0}, {1, 0}, {1, 0}}, expected: 2},
		{name: "wrong dimension", vectors: [][]float32{{1, 0}, {1, 0, 0}}, expected: 2},
		{name: "zero", vectors: [][]float32{{1, 0}, {0, 0}}, expected: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateEmbeddingBatchOutput("test", test.vectors, test.expected, 2); err == nil {
				t.Fatal("validateEmbeddingBatchOutput accepted malformed batch")
			}
		})
	}
}

func TestCardBackfillRejectsShortEmbeddingBatchWithoutPartialWrite(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "short-batch-source", Kind: SourceKindText, URI: "manual://short-batch", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"short-batch-card-a", "short-batch-card-b"} {
		if err := store.SaveCard(ctx, Card{ID: id, SourceID: "short-batch-source", Title: id, Claim: "must remain unembedded"}); err != nil {
			t.Fatalf("save card %s: %v", id, err)
		}
	}
	store.SetEmbedder(malformedBatchEmbedder{embeddingMetadataTestEmbedder: embeddingMetadataTestEmbedder{model: "short-batch"}, vectors: [][]float32{{1, 0}}})
	store.WaitBackground()
	if err := store.backfillCardEmbeddings(ctx); err == nil {
		t.Fatal("backfill accepted short embedding batch")
	}
	var vectors int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE source_id = 'short-batch-source' AND embedding IS NOT NULL AND LENGTH(embedding) > 0`).Scan(&vectors); err != nil {
		t.Fatal(err)
	}
	if vectors != 0 {
		t.Fatalf("short batch partially persisted %d card vectors", vectors)
	}
}

func TestCardBackfillDoesNotOverwriteConcurrentCardRewrite(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "card-rewrite-source", Kind: SourceKindText, URI: "manual://card-rewrite", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "card-rewrite", SourceID: "card-rewrite-source", Title: "Before", Claim: "old card claim", Summary: "old"}); err != nil {
		t.Fatal(err)
	}
	blocked := &blockingEmbeddingMetadataEmbedder{model: "rewrite-model", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(blocked)
	select {
	case <-blocked.started:
	case <-t.Context().Done():
		t.Fatal("card backfill did not start")
	}
	if err := store.SaveCard(ctx, Card{ID: "card-rewrite", SourceID: "card-rewrite-source", Title: "After", Claim: "new card claim", Summary: "new"}); err != nil {
		t.Fatal(err)
	}
	close(blocked.release)
	store.WaitBackground()
	var claim string
	var vector []byte
	if err := store.db.QueryRowContext(ctx, `SELECT claim, embedding FROM knowledge_cards WHERE id = 'card-rewrite'`).Scan(&claim, &vector); err != nil {
		t.Fatal(err)
	}
	if claim != "new card claim" {
		t.Fatalf("claim = %q, want rewritten content", claim)
	}
	if len(vector) != 0 {
		t.Fatalf("stale backfill wrote %d vector bytes over rewritten card", len(vector))
	}
	var metadata int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'card' AND entity_id = 'card-rewrite'`).Scan(&metadata); err != nil || metadata != 0 {
		t.Fatalf("stale backfill metadata = %d, err = %v", metadata, err)
	}
}
