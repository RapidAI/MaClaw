package knowledge

import (
	"context"
	"strings"
	"testing"
)

type structuredRowEmbedder struct{ model string }

func (e structuredRowEmbedder) Embed(text string) ([]float32, error) {
	text = strings.ToLower(text)
	if strings.Contains(text, "semantic employee") || strings.Contains(text, "bob") || strings.Contains(text, "engineering") {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func (e structuredRowEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vector, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		vectors[i] = vector
	}
	return vectors, nil
}

func (e structuredRowEmbedder) Dim() int        { return 2 }
func (e structuredRowEmbedder) Close()          {}
func (e structuredRowEmbedder) ModelID() string { return e.model }

func TestSearchStructuredUsesTableRowEmbeddingsWithModelIsolation(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	store.SetEmbedder(structuredRowEmbedder{model: "structured-a"})
	store.WaitBackground()

	results, err := store.SearchStructured(ctx, StructuredSearchOptions{Query: "semantic employee", Limit: 5})
	if err != nil {
		t.Fatalf("SearchStructured: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 || results[0].Claim != "Bob" {
		t.Fatalf("semantic table-row results = %#v", results)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE knowledge_embedding_metadata SET model_id = 'other-model' WHERE entity_type = 'table_row'`); err != nil {
		t.Fatalf("mark row vectors stale: %v", err)
	}
	results, err = store.SearchStructured(ctx, StructuredSearchOptions{Query: "semantic employee", Limit: 5})
	if err != nil {
		t.Fatalf("SearchStructured with stale model: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("mixed-model table-row results = %#v, want none", results)
	}

	store.SetEmbedder(structuredRowEmbedder{model: "structured-b"})
	store.WaitBackground()
	var fresh int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'table_row' AND model_id = 'structured-b'`).Scan(&fresh); err != nil {
		t.Fatalf("query refreshed row metadata: %v", err)
	}
	if fresh != 2 {
		t.Fatalf("refreshed table-row metadata = %d, want 2", fresh)
	}
}

func TestRRFFuseKeepsDistinctTableRows(t *testing.T) {
	results := rrfFuse(
		[]SearchResult{{ResultType: "table_row", RowID: "row-a"}, {ResultType: "table_row", RowID: "row-b"}},
		nil,
		10,
	)
	if len(results) != 2 {
		t.Fatalf("RRF fused %d table rows, want 2: %#v", len(results), results)
	}
}

func TestRRFFuseKeepsFactsDistinctFromTheirParentCard(t *testing.T) {
	results := rrfFuse([]SearchResult{
		{ResultType: "card", CardID: "card-1"},
		{ResultType: "fact", FactID: "fact-1", CardID: "card-1"},
		{ResultType: "fact", FactID: "fact-2", CardID: "card-1"},
	}, nil, 10)
	if len(results) != 3 {
		t.Fatalf("RRF fused %d results, want card plus two facts: %#v", len(results), results)
	}
	if results[0].ResultType != "card" || results[1].FactID != "fact-1" || results[2].FactID != "fact-2" {
		t.Fatalf("RRF entity identity/order = %#v", results)
	}
}

func TestRRFFuseOrdersTiesDeterministically(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		results := rrfFuse(
			[]SearchResult{{ResultType: "table_row", RowID: "row-b"}, {ResultType: "table_row", RowID: "row-a"}},
			nil,
			10,
		)
		if len(results) != 2 || results[0].RowID != "row-b" || results[1].RowID != "row-a" {
			t.Fatalf("attempt %d RRF tie order = %#v", attempt, results)
		}
	}
}
