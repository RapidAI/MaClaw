package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestRebuildFTSSegmentationPagesAndDropsOrphans(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "fts-source", Kind: SourceKindText, URI: "manual://fts", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	nodeCount := ftsRebuildPageSize + 1
	for i := 0; i < nodeCount; i++ {
		id := fmt.Sprintf("node-%04d", i)
		if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text) VALUES (?, 'fts-source', 'paragraph', ?, ?)`, id, "标题"+id, "正文"+id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_cards(id, source_id, claim, created_at, updated_at) VALUES ('card-1', 'fts-source', '卡片主张', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_facts(id, card_id, source_id, subject, predicate, object) VALUES ('fact-1', 'card-1', 'fts-source', '主体', '关系', '客体')`); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO document_nodes_fts(node_id, title, text) VALUES ('orphan-node', 'STALE', 'STALE')`,
		`INSERT INTO knowledge_cards_fts(card_id, title, claim, summary) VALUES ('orphan-card', 'STALE', 'STALE', 'STALE')`,
		`INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object) VALUES ('orphan-fact', 'STALE', 'STALE', 'STALE')`,
	} {
		if _, err := store.db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.RebuildFTSSegmentation(ctx); err != nil {
		t.Fatal(err)
	}

	var nodes, cards, facts int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_nodes_fts`).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards_fts`).Scan(&cards); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_facts_fts`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if nodes != nodeCount || cards != 1 || facts != 1 {
		t.Fatalf("fts counts nodes=%d cards=%d facts=%d, want %d, 1, 1", nodes, cards, facts, nodeCount)
	}
	var stale int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_nodes_fts WHERE node_id = 'orphan-node'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("orphan node fts rows = %d", stale)
	}
}

func TestRebuildFTSSegmentationClearsIndexWhenSourcesAreEmpty(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES ('orphan-node', 'STALE', 'STALE')`); err != nil {
		t.Fatal(err)
	}
	if err := store.RebuildFTSSegmentation(ctx); err != nil {
		t.Fatal(err)
	}
	var nodes int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_nodes_fts`).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if nodes != 0 {
		t.Fatalf("node fts rows = %d, want 0", nodes)
	}
}
