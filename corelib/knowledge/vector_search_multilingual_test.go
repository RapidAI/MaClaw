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

func TestSearchByEmbeddingDoesNotDropNodeBeforeScoring(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(directionalKnowledgeEmbedder{})
	if err := store.EnsureNodeEmbeddingColumn(); err != nil {
		t.Fatalf("EnsureNodeEmbeddingColumn: %v", err)
	}

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
	// These nodes would fill an importance-ordered candidate cap if scoring truncated first.
	for i := 0; i < 500; i++ {
		nodeID := "node_noise_" + fmt.Sprint(i)
		if _, err := tx.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, embedding) VALUES (?, ?, 'paragraph', 'noise', 'irrelevant', ?)`,
			nodeID, source.ID, float32SliceToBytes([]float32{0, 1})); err != nil {
			t.Fatalf("noise node %d: %v", i, err)
		}
		if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityNode, nodeID, embeddingModelIdentifier(directionalKnowledgeEmbedder{}), 2); err != nil {
			t.Fatalf("noise metadata %d: %v", i, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, embedding) VALUES ('node_target', ?, 'paragraph', 'target', 'relevant', ?)`,
		source.ID, float32SliceToBytes([]float32{1, 0})); err != nil {
		t.Fatalf("target node: %v", err)
	}
	if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityNode, "node_target", embeddingModelIdentifier(directionalKnowledgeEmbedder{}), 2); err != nil {
		t.Fatalf("target metadata: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "target semantic query", Limit: 1})
	if err != nil {
		t.Fatalf("searchByEmbedding: %v", err)
	}
	if len(results) != 1 || results[0].NodeID != "node_target" {
		t.Fatalf("results = %#v, want the matching node", results)
	}
}
