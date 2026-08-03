package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

type directionalKnowledgeEmbedder struct{}

func (directionalKnowledgeEmbedder) Embed(text string) ([]float32, error) {
	if text == "target semantic query" {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func (directionalKnowledgeEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i], _ = (directionalKnowledgeEmbedder{}).Embed(text)
	}
	return out, nil
}

func (directionalKnowledgeEmbedder) Dim() int { return 2 }
func (directionalKnowledgeEmbedder) Close()   {}

func TestSearchByEmbeddingDoesNotDropLowImportanceCardBeforeScoring(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(directionalKnowledgeEmbedder{})

	now := time.Now().UTC()
	source := Source{
		ID: "source_vector", Kind: SourceKindText, URI: "knowledge://test/vector", Title: "Vector", FetchedAt: now,
		ContentHash: "vector-hash", SourceTrust: 0.8, Status: StatusDistilled, CreatedAt: now, UpdatedAt: now,
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := insertSource(ctx, tx, source); err != nil {
		t.Fatalf("source: %v", err)
	}
	// These cards would fill the old importance-ordered 500-row candidate cap.
	for i := 0; i < 500; i++ {
		cardID := "card_noise_" + fmt.Sprint(i)
		if _, err := tx.ExecContext(ctx, insertCardSQL,
			cardID, source.ID, nil, "noise", "irrelevant", "", "[]", "[]", "[]", "", "", "",
			"", "", 0.8, 100.0, 0.8, float32SliceToBytes([]float32{0, 1}), formatTime(now), formatTime(now)); err != nil {
			t.Fatalf("noise card %d: %v", i, err)
		}
		if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityCard, cardID, embeddingModelIdentifier(directionalKnowledgeEmbedder{}), 2); err != nil {
			t.Fatalf("noise metadata %d: %v", i, err)
		}
	}
	if _, err := tx.ExecContext(ctx, insertCardSQL,
		"card_target", source.ID, nil, "target", "relevant", "", "[]", "[]", "[]", "", "", "",
		"", "", 0.8, 0.01, 0.8, float32SliceToBytes([]float32{1, 0}), formatTime(now), formatTime(now)); err != nil {
		t.Fatalf("target card: %v", err)
	}
	if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityCard, "card_target", embeddingModelIdentifier(directionalKnowledgeEmbedder{}), 2); err != nil {
		t.Fatalf("target metadata: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "target semantic query", Limit: 1})
	if err != nil {
		t.Fatalf("searchByEmbedding: %v", err)
	}
	if len(results) != 1 || results[0].CardID != "card_target" {
		t.Fatalf("results = %#v, want low-importance target", results)
	}
}
