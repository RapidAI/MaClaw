package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDeleteSourcesByIDsRemovesStructuredRowsAndEmbeddingMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetEmbedder(structuredRowEmbedder{model: "cleanup-model"})
	store.WaitBackground()

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(parseTime("2026-01-01T00:00:00Z"))
	source := Source{ID: "cleanup-source", Kind: SourceKindCSV, URI: "cleanup.csv", Status: StatusParsed}
	if err := insertKBSource(ctx, tx, source); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_tables(id, source_id, sheet_name, created_at, updated_at) VALUES ('cleanup-table', ?, 'Sheet1', ?, ?)`, source.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_rows(id, table_id, source_id, row_index, primary_key_text, row_text, embedding, created_at, updated_at) VALUES ('cleanup-row', 'cleanup-table', ?, 1, 'Bob', 'name: Bob', ?, ?, ?)`, source.ID, float32SliceToBytes([]float32{1, 0}), now, now); err != nil {
		t.Fatal(err)
	}
	if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityRow, "cleanup-row", "cleanup-model", 2); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	tx, err = store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteSourcesByIDsTx(ctx, tx, []string{source.ID}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"kb_sources", "kb_tables", "kb_rows", "knowledge_embedding_metadata"} {
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
}

func TestBackfillTableRowEmbeddingsForSourcesBatchesLargeSourceLists(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetEmbedder(structuredRowEmbedder{model: "batch-model"})
	store.WaitBackground()

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(parseTime("2026-01-01T00:00:00Z"))
	ids := make([]string, 0, 401)
	for i := 0; i < 401; i++ {
		id := NewID("batch-source")
		ids = append(ids, id)
		if err := insertKBSource(ctx, tx, Source{ID: id, Kind: SourceKindCSV, URI: id + ".csv", Status: StatusParsed}); err != nil {
			t.Fatal(err)
		}
	}
	lastSource := ids[len(ids)-1]
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_tables(id, source_id, sheet_name, created_at, updated_at) VALUES ('batch-table', ?, 'Sheet1', ?, ?)`, lastSource, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_rows(id, table_id, source_id, row_index, primary_key_text, row_text, created_at, updated_at) VALUES ('batch-row', 'batch-table', ?, 1, 'Bob', 'name: Bob | department: Engineering', ?, ?)`, lastSource, now, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := store.BackfillTableRowEmbeddingsForSources(ctx, ids); err != nil {
		t.Fatalf("BackfillTableRowEmbeddingsForSources: %v", err)
	}
	var metadataCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_embedding_metadata WHERE entity_type = 'table_row' AND entity_id = 'batch-row' AND model_id = 'batch-model'`).Scan(&metadataCount); err != nil {
		t.Fatal(err)
	}
	if metadataCount != 1 {
		t.Fatalf("embedded row metadata = %d, want 1", metadataCount)
	}
}
