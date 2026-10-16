package knowledge

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestUseEmbedderDoesNotStartRefresh(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	emb := &blockingEmbeddingMetadataEmbedder{model: "query-only", started: make(chan struct{}), release: make(chan struct{})}
	store.UseEmbedder(emb)
	select {
	case <-emb.started:
		t.Fatal("UseEmbedder started a library refresh")
	case <-time.After(50 * time.Millisecond):
	}
	store.embeddingRefreshMu.Lock()
	active := store.embeddingRefreshCancel
	store.embeddingRefreshMu.Unlock()
	if active != nil {
		t.Fatal("UseEmbedder installed a refresh cancellation handle")
	}
}

func TestSetEmbedderSameModelDoesNotRestartRefresh(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "same-model-source", Kind: SourceKindText, URI: "manual://same-model", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "same-model-card", SourceID: "same-model-source", Title: "Stay", Claim: "The running refresh must keep this card."}); err != nil {
		t.Fatal(err)
	}
	first := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(first)
	select {
	case <-first.started:
	case <-t.Context().Done():
		t.Fatal("refresh did not start")
	}
	_, generation := store.currentEmbedderSnapshot()
	second := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(second)
	select {
	case <-second.started:
		t.Fatal("same model restarted the library refresh")
	case <-time.After(50 * time.Millisecond):
	}
	if _, got := store.currentEmbedderSnapshot(); got != generation {
		t.Fatalf("generation = %d, want %d", got, generation)
	}
	close(first.release)
	store.WaitBackground()
}

func TestSetEmbedderSameModelResumesWhenIdle(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "resume-source", Kind: SourceKindText, URI: "manual://resume", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "resume-card-1", SourceID: "resume-source", Title: "First", Claim: "already embedded"}); err != nil {
		t.Fatal(err)
	}
	emb := &recordingBatchEmbedder{model: "model-a"}
	store.SetEmbedder(emb)
	store.WaitBackground()
	_, generation := store.currentEmbedderSnapshot()
	if err := store.SaveCard(ctx, Card{ID: "resume-card-2", SourceID: "resume-source", Title: "Second", Claim: "filled after the first pass"}); err != nil {
		t.Fatal(err)
	}
	store.SetEmbedder(emb)
	store.WaitBackground()
	if _, got := store.currentEmbedderSnapshot(); got != generation {
		t.Fatalf("generation = %d, want %d", got, generation)
	}
	var fresh int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a' AND entity_type = 'card' AND entity_id = 'resume-card-2'`).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh != 1 {
		t.Fatal("idle same-model refresh did not fill the new card")
	}
	if store.EmbeddingRefreshIncomplete() {
		t.Fatal("successful refresh stayed incomplete")
	}
}

type errBatchEmbedder struct{ model string }

func (e errBatchEmbedder) Embed(string) ([]float32, error) {
	return nil, errBatchEmbedderFailure{}
}
func (e errBatchEmbedder) EmbedBatch([]string) ([][]float32, error) {
	return nil, errBatchEmbedderFailure{}
}
func (e errBatchEmbedder) Dim() int           { return 2 }
func (e errBatchEmbedder) Close()             {}
func (e errBatchEmbedder) ModelID() string    { return e.model }
func (errBatchEmbedderFailure) Error() string { return "embed failed" }

type errBatchEmbedderFailure struct{}

func TestEmbeddingRefreshIncompleteResumesOnNextAttach(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "fail-source", Kind: SourceKindText, URI: "manual://fail", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "fail-card", SourceID: "fail-source", Title: "Retry", Claim: "this card survives a failed embed"}); err != nil {
		t.Fatal(err)
	}
	store.SetEmbedder(errBatchEmbedder{model: "model-a"})
	store.WaitBackground()
	if !store.EmbeddingRefreshIncomplete() {
		t.Fatal("failed refresh was not marked incomplete")
	}
	if store.EmbeddingRefreshResumeDue() {
		t.Fatal("failed refresh resumed on the next search immediately")
	}
	_, generation := store.currentEmbedderSnapshot()
	again := &recordingBatchEmbedder{model: "model-a"}
	store.SetEmbedder(again)
	store.WaitBackground()
	if _, got := store.currentEmbedderSnapshot(); got != generation {
		t.Fatalf("generation = %d, want %d", got, generation)
	}
	if store.EmbeddingRefreshIncomplete() {
		t.Fatal("resume left the refresh incomplete")
	}
	var fresh int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a' AND entity_type = 'card' AND entity_id = 'fail-card'`).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh != 1 {
		t.Fatal("resume did not embed the card that failed")
	}
}

func TestLongDocumentNodeEmbeddingPersists(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureNodeEmbeddingColumn(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, Source{ID: "long-node-source", Kind: SourceKindText, URI: "manual://long-node", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("甲", 9000)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text) VALUES ('long-node', 'long-node-source', 'paragraph', '北京', ?)`, body); err != nil {
		t.Fatal(err)
	}
	emb := &recordingBatchEmbedder{model: "model-a"}
	store.SetEmbedder(emb)
	store.WaitBackground()
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'node' AND entity_id = 'long-node' AND model_id = 'model-a'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("long document node was not persisted")
	}
	emb.mu.Lock()
	defer emb.mu.Unlock()
	joined := strings.Join(emb.texts, "\n")
	if !strings.Contains(joined, strings.Repeat("甲", 100)) {
		t.Fatal("embedder did not see the document prefix")
	}
	if strings.Contains(joined, strings.Repeat("甲", 8001)) {
		t.Fatal("embedder received more than the document prefix")
	}
}

func TestEmbeddingRefreshAllowsNullTitles(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureNodeEmbeddingColumn(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, Source{ID: "null-title-source", Kind: SourceKindText, URI: "manual://null-title", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "null-title-card", SourceID: "null-title-source", Title: "temporary", Claim: "card with no title"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "titled-card", SourceID: "null-title-source", Title: "Kept", Claim: "card after the null title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE knowledge_cards SET title = NULL, summary = NULL WHERE id = 'null-title-card'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, text) VALUES ('null-title-node', 'null-title-source', 'paragraph', '正文没有标题')`); err != nil {
		t.Fatal(err)
	}
	store.SetEmbedder(&recordingBatchEmbedder{model: "model-a"})
	store.WaitBackground()
	if store.EmbeddingRefreshIncomplete() {
		t.Fatal("null titles failed the library refresh")
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a' AND entity_id IN ('null-title-card', 'titled-card', 'null-title-node')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("embedded rows = %d, want 3", n)
	}
}

func TestNodeEmbeddingSearchLoadsTextForHitsOnly(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureNodeEmbeddingColumn(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, Source{ID: "node-source", Kind: SourceKindText, URI: "manual://nodes", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	emb := &recordingBatchEmbedder{model: "model-a"}
	store.UseEmbedder(emb)
	blob := float32SliceToBytes([]float32{1, 0})
	for _, node := range []struct{ id, text string }{
		{"node-a", "命中的全文要带回"},
		{"node-b", "没有进入结果的长文不应该在打分时被读出来"},
	} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, embedding) VALUES (?, 'node-source', 'paragraph', '北京', ?, ?)`, node.id, node.text, blob); err != nil {
			t.Fatal(err)
		}
		if err := store.upsertEmbeddingMetadata(ctx, embeddingEntityNode, node.id, "model-a", 2); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "北京", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].NodeID != "node-a" || results[0].Snippet != "命中的全文要带回" {
		t.Fatalf("hit = %+v", results[0])
	}
}

func TestEmbeddingSearchAllowsNullTextColumns(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureNodeEmbeddingColumn(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, Source{ID: "null-search-source", Kind: SourceKindText, URI: "manual://null-search", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE knowledge_sources SET title = NULL, author = NULL, canonical_uri = NULL, source_trust = NULL WHERE id = 'null-search-source'`); err != nil {
		t.Fatal(err)
	}
	emb := &recordingBatchEmbedder{model: "model-a"}
	store.UseEmbedder(emb)
	if err := store.SaveCard(ctx, Card{ID: "null-search-card", SourceID: "null-search-source", Title: "temporary", Claim: "北京卡片", Embedding: []float32{1, 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE knowledge_cards SET title = NULL, summary = NULL WHERE id = 'null-search-card'`); err != nil {
		t.Fatal(err)
	}
	blob := float32SliceToBytes([]float32{1, 0})
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, text, embedding) VALUES ('null-search-node', 'null-search-source', 'paragraph', '北京节点正文', ?)`, blob); err != nil {
		t.Fatal(err)
	}
	if err := store.upsertEmbeddingMetadata(ctx, embeddingEntityNode, "null-search-node", "model-a", 2); err != nil {
		t.Fatal(err)
	}
	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "北京", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	var sawCard, sawNode bool
	for _, result := range results {
		if result.CardID == "null-search-card" {
			sawCard = true
		}
		if result.NodeID == "null-search-node" && result.Snippet == "北京节点正文" {
			sawNode = true
		}
	}
	if !sawCard || !sawNode {
		t.Fatalf("card=%v node=%v results=%+v", sawCard, sawNode, results)
	}
}

func TestCardRefreshKeepsPersistedBatchWhenCancelled(t *testing.T) {
	previousBatch := embeddingBackfillBatchSize
	embeddingBackfillBatchSize = 1
	t.Cleanup(func() { embeddingBackfillBatchSize = previousBatch })

	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(ctx, Source{ID: "batch-source", Kind: SourceKindText, URI: "manual://batch", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	for _, card := range []Card{
		{ID: "batch-card-1", SourceID: "batch-source", Title: "One", Claim: "first card claim stays"},
		{ID: "batch-card-2", SourceID: "batch-source", Title: "Two", Claim: "second card claim resumes"},
	} {
		if err := store.SaveCard(ctx, card); err != nil {
			t.Fatal(err)
		}
	}
	emb := &gatedBatchEmbedder{model: "model-a", release: make(chan struct{})}
	store.SetEmbedder(emb)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a' AND entity_type = 'card'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first card batch was not persisted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- store.Close() }()
	cancelDeadline := time.Now().Add(2 * time.Second)
	for {
		store.embedderMu.RLock()
		closing := store.closed
		store.embedderMu.RUnlock()
		store.embeddingRefreshMu.Lock()
		active := store.embeddingRefreshCancel
		store.embeddingRefreshMu.Unlock()
		if closing && active == nil {
			break
		}
		if time.Now().After(cancelDeadline) {
			t.Fatal("Close did not cancel the refresh")
		}
		time.Sleep(time.Millisecond)
	}
	close(emb.release)
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}

	resumed, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	var kept int
	if err := resumed.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a' AND entity_type = 'card'`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatalf("persisted cards after cancel = %d, want 1", kept)
	}
	again := &recordingBatchEmbedder{model: "model-a"}
	resumed.SetEmbedder(again)
	resumed.WaitBackground()
	var fresh int
	if err := resumed.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE model_id = 'model-a' AND entity_type = 'card'`).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh != 2 {
		t.Fatalf("cards after resume = %d, want 2", fresh)
	}
	again.mu.Lock()
	texts := append([]string(nil), again.texts...)
	again.mu.Unlock()
	joined := strings.Join(texts, "\n")
	if strings.Contains(joined, "first card claim stays") && strings.Contains(joined, "second card claim resumes") {
		t.Fatalf("resume embedded both cards: %q", joined)
	}
	if !strings.Contains(joined, "first card claim stays") && !strings.Contains(joined, "second card claim resumes") {
		t.Fatalf("resume embedded neither remaining card: %q", joined)
	}
}

func TestRestartEmbeddingRefreshReplacesSameModelPass(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "restart-source", Kind: SourceKindText, URI: "manual://restart", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "restart-card", SourceID: "restart-source", Title: "Hold", Claim: "the first pass blocks in the model"}); err != nil {
		t.Fatal(err)
	}
	emb := &countingBlockEmbedder{blockingEmbeddingMetadataEmbedder: blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}}
	store.SetEmbedder(emb)
	select {
	case <-emb.started:
	case <-t.Context().Done():
		t.Fatal("refresh did not start")
	}
	if emb.calls.Load() != 1 {
		t.Fatalf("embed batches = %d, want the first pass", emb.calls.Load())
	}
	store.RestartEmbeddingRefresh()
	deadline := time.Now().Add(2 * time.Second)
	for emb.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if emb.calls.Load() < 2 {
		t.Fatalf("embed batches = %d, want the replacement pass to call the model", emb.calls.Load())
	}
	close(emb.release)
	store.WaitBackground()
}

type countingBlockEmbedder struct {
	blockingEmbeddingMetadataEmbedder
	calls atomic.Int32
}

func (e *countingBlockEmbedder) Embed(text string) ([]float32, error) {
	vectors, err := e.EmbedBatch([]string{text})
	if err != nil || len(vectors) == 0 {
		return nil, err
	}
	return vectors[0], nil
}

func (e *countingBlockEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.calls.Add(1)
	e.once.Do(func() { close(e.started) })
	<-e.release
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}

func TestLibraryRefreshReportsActiveUntilItFinishes(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "active-source", Kind: SourceKindText, URI: "manual://active", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "active-card", SourceID: "active-source", Title: "Hold", Claim: "Refresh stays active while this embeds."}); err != nil {
		t.Fatal(err)
	}
	emb := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	store.SetEmbedder(emb)
	select {
	case <-emb.started:
	case <-t.Context().Done():
		t.Fatal("refresh did not start")
	}
	if !LibraryEmbeddingRefreshActive() {
		t.Fatal("library refresh was not reported active")
	}
	close(emb.release)
	store.WaitBackground()
	if LibraryEmbeddingRefreshActive() {
		t.Fatal("library refresh stayed active after it finished")
	}
}

func TestSearchSkipsQueryEmbeddingWhileRebuildRuns(t *testing.T) {
	ctx := context.Background()
	refresh, err := NewSQLiteStore(filepath.Join(t.TempDir(), "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer refresh.Close()
	if err := refresh.SaveSource(ctx, Source{ID: "refresh-source", Kind: SourceKindText, URI: "manual://refresh", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := refresh.SaveCard(ctx, Card{ID: "refresh-card", SourceID: "refresh-source", Title: "Hold", Claim: "blocks the library rebuild"}); err != nil {
		t.Fatal(err)
	}
	blocking := &blockingEmbeddingMetadataEmbedder{model: "model-a", started: make(chan struct{}), release: make(chan struct{})}
	refresh.SetEmbedder(blocking)
	select {
	case <-blocking.started:
	case <-t.Context().Done():
		t.Fatal("refresh did not start")
	}
	released := false
	defer func() {
		if !released {
			close(blocking.release)
		}
		refresh.WaitBackground()
	}()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "skip-running-source", Kind: SourceKindText, URI: "manual://skip-running", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	emb := &recordingBatchEmbedder{model: "model-a"}
	store.UseEmbedder(emb)
	if err := store.SaveCard(ctx, Card{ID: "skip-running-card", SourceID: "skip-running-source", Title: "北京", Claim: "已有向量", Embedding: []float32{1, 0}}); err != nil {
		t.Fatal(err)
	}
	// Node and row metadata make image and structured search eligible to embed.
	// The image row is required as well: image search does not embed when the
	// library has no image nodes.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, sheet_name, row_range, col_range) VALUES ('skip-running-node', 'skip-running-source', ?, '北京', '图片说明', '', '', '')`, NodeTypeImage); err != nil {
		t.Fatal(err)
	}
	if err := store.upsertEmbeddingMetadata(ctx, embeddingEntityNode, "skip-running-node", emb.model, emb.Dim()); err != nil {
		t.Fatal(err)
	}
	if err := store.upsertEmbeddingMetadata(ctx, embeddingEntityRow, "skip-running-row", emb.model, emb.Dim()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.searchByEmbedding(SkipQueryEmbedding(ctx), SearchOptions{Query: "北京", Limit: 5}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	skipped := emb.embeds
	emb.mu.Unlock()
	if skipped != 0 {
		t.Fatalf("skipped search embedded %d times", skipped)
	}
	if _, err := store.searchTableRowsByEmbedding(SkipQueryEmbedding(ctx), StructuredSearchOptions{Query: "北京"}, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SearchImages(SkipQueryEmbedding(ctx), ImageSearchOptions{SearchOptions: SearchOptions{Query: "北京", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	skipped = emb.embeds
	emb.mu.Unlock()
	if skipped != 0 {
		t.Fatalf("image and structured search embedded %d times during rebuild", skipped)
	}
	if _, err := store.searchByEmbedding(ctx, SearchOptions{Query: "北京", Limit: 5}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	during := emb.embeds
	emb.mu.Unlock()
	if during == 0 {
		t.Fatal("search without the skip mark did not embed the query")
	}

	close(blocking.release)
	released = true
	refresh.WaitBackground()
	if _, err := store.searchByEmbedding(SkipQueryEmbedding(ctx), SearchOptions{Query: "北京", Limit: 5}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	afterRebuild := emb.embeds
	emb.mu.Unlock()
	if afterRebuild == during {
		t.Fatal("search kept skipping after the rebuild finished")
	}
	if _, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "北京", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	defer emb.mu.Unlock()
	if emb.embeds <= afterRebuild {
		t.Fatal("image search did not embed after the rebuild finished")
	}
}

func TestSearchSkipsEmbedWhenModelHasNoVectors(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "skip-source", Kind: SourceKindText, URI: "manual://skip", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "skip-card", SourceID: "skip-source", Title: "北京", Claim: "北京天气还没向量"}); err != nil {
		t.Fatal(err)
	}
	emb := &recordingBatchEmbedder{model: "model-new"}
	store.UseEmbedder(emb)
	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "北京", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want none", results)
	}
	if _, err := store.searchTableRowsByEmbedding(ctx, StructuredSearchOptions{Query: "北京"}, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "北京", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	defer emb.mu.Unlock()
	if emb.embeds != 0 || len(emb.texts) != 0 {
		t.Fatalf("embed calls = %d texts = %q, want no embedding", emb.embeds, emb.texts)
	}
}

func TestSearchImagesSkipsEmbedUntilAnImageVectorExists(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "image-gate-source", Kind: SourceKindText, URI: "manual://image-gate", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	emb := &recordingBatchEmbedder{model: "model-a"}
	store.UseEmbedder(emb)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, sheet_name, row_range, col_range) VALUES
		('paragraph-node', 'image-gate-source', 'paragraph', '北京', '段落', '', '', ''),
		('image-node', 'image-gate-source', ?, '北京', '图片说明', '', '', '')`, NodeTypeImage); err != nil {
		t.Fatal(err)
	}
	if err := store.upsertEmbeddingMetadata(ctx, embeddingEntityNode, "paragraph-node", emb.model, emb.Dim()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "北京", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	before := emb.embeds
	emb.mu.Unlock()
	if before != 0 {
		t.Fatalf("image search embedded %d times before an image vector existed", before)
	}
	if err := store.upsertEmbeddingMetadata(ctx, embeddingEntityNode, "image-node", emb.model, emb.Dim()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "北京", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	emb.mu.Lock()
	defer emb.mu.Unlock()
	if emb.embeds != 1 {
		t.Fatalf("image search embeds = %d, want 1", emb.embeds)
	}
}

type gatedBatchEmbedder struct {
	model   string
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func (e *gatedBatchEmbedder) Embed(text string) ([]float32, error) {
	vectors, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (e *gatedBatchEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	call := e.calls
	e.mu.Unlock()
	if call > 1 {
		<-e.release
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}

func (e *gatedBatchEmbedder) Dim() int        { return 2 }
func (e *gatedBatchEmbedder) Close()          {}
func (e *gatedBatchEmbedder) ModelID() string { return e.model }

type recordingBatchEmbedder struct {
	model  string
	mu     sync.Mutex
	embeds int
	texts  []string
}

func (e *recordingBatchEmbedder) Embed(text string) ([]float32, error) {
	vectors, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (e *recordingBatchEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.embeds++
	e.texts = append(e.texts, texts...)
	e.mu.Unlock()
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}

func (e *recordingBatchEmbedder) Dim() int        { return 2 }
func (e *recordingBatchEmbedder) Close()          {}
func (e *recordingBatchEmbedder) ModelID() string { return e.model }
