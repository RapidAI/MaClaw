package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDeleteFragmentRemovesCardNodeAndDerivedEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-card", Kind: SourceKindMarkdown, URI: "manual://card", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "node-card",
		SourceID: "src-card",
		Type:     "heading",
		Title:    "Authorized patents",
		Text:     "Google Patents lists the authorized Chinese invention patents.",
	}); err != nil {
		t.Fatalf("SaveDocumentNode: %v", err)
	}
	if err := store.SaveCard(ctx, Card{
		ID:       "card-patents",
		SourceID: "src-card",
		NodeID:   "node-card",
		Title:    "Authorized patents",
		Claim:    "Google Patents lists the authorized Chinese invention patents.",
		Summary:  "patent evidence",
	}); err != nil {
		t.Fatalf("SaveCard: %v", err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertFact(ctx, tx, Fact{ID: "fact-patents", CardID: "card-patents", SourceID: "src-card", Subject: "patents", Predicate: "listed_in", Object: "Google Patents"}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insertFact: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteFragment(ctx, SearchResult{ResultType: "card", NodeID: "node-card", CardID: "card-patents"}); err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	for _, check := range []struct {
		name  string
		query string
	}{
		{"nodes", `SELECT COUNT(*) FROM document_nodes WHERE id = 'node-card'`},
		{"cards", `SELECT COUNT(*) FROM knowledge_cards WHERE id = 'card-patents'`},
		{"facts", `SELECT COUNT(*) FROM knowledge_facts WHERE id = 'fact-patents'`},
		{"card fts", `SELECT COUNT(*) FROM knowledge_cards_fts WHERE card_id = 'card-patents'`},
		{"fact fts", `SELECT COUNT(*) FROM knowledge_facts_fts WHERE fact_id = 'fact-patents'`},
		{"node fts", `SELECT COUNT(*) FROM document_nodes_fts WHERE node_id = 'node-card'`},
	} {
		var count int
		if err := store.db.QueryRowContext(ctx, check.query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", check.name, count)
		}
	}
	results, err := store.Search(ctx, SearchOptions{Query: "patents", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("deleted fragment still searchable: %#v", results)
	}
}

func TestDeleteFragmentCardWithoutNodeLeavesOtherCards(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-orphan", Kind: SourceKindText, URI: "manual://orphan", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "keep-card", SourceID: "src-orphan", Title: "Keep", Claim: "keep this claim", Summary: "keep"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "drop-card", SourceID: "src-orphan", Title: "Drop", Claim: "drop this claim", Summary: "drop"}); err != nil {
		t.Fatal(err)
	}

	got, err := store.DeleteFragment(ctx, SearchResult{ResultType: "card", CardID: "drop-card"})
	if err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("card delete node IDs = %#v, want empty slice", got)
	}
	var kept, dropped int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE id = 'keep-card'`).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("kept card = %d, err = %v", kept, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE id = 'drop-card'`).Scan(&dropped); err != nil || dropped != 0 {
		t.Fatalf("dropped card = %d, err = %v", dropped, err)
	}
}

func TestDeleteFragmentFactOnly(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-fact", Kind: SourceKindText, URI: "manual://fact", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "fact-card", SourceID: "src-fact", Title: "Card", Claim: "card claim", Summary: "summary"}); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertFact(ctx, tx, Fact{ID: "drop-fact", CardID: "fact-card", SourceID: "src-fact", Subject: "alpha", Predicate: "uses", Object: "sqlite"}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteFragment(ctx, SearchResult{ResultType: "fact", FactID: "drop-fact", CardID: "fact-card"}); err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	var facts, cards int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_facts WHERE id = 'drop-fact'`).Scan(&facts); err != nil || facts != 0 {
		t.Fatalf("facts = %d, err = %v", facts, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE id = 'fact-card'`).Scan(&cards); err != nil || cards != 1 {
		t.Fatalf("cards = %d, err = %v", cards, err)
	}
}

func TestDeleteFragmentTableRow(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	var rowID string
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM kb_rows ORDER BY row_index ASC LIMIT 1`).Scan(&rowID); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if _, err := store.DeleteFragment(ctx, SearchResult{ResultType: "table_row", RowID: rowID}); err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	var rows, cells, cards int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_rows WHERE id = ?`, rowID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows = %d, err = %v", rows, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_cells WHERE row_id = ?`, rowID).Scan(&cells); err != nil || cells != 0 {
		t.Fatalf("cells = %d, err = %v", cells, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_cards WHERE row_id = ?`, rowID).Scan(&cards); err != nil || cards != 0 {
		t.Fatalf("cards = %d, err = %v", cards, err)
	}
	var remaining int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_rows`).Scan(&remaining); err != nil || remaining == 0 {
		t.Fatalf("remaining rows = %d, err = %v", remaining, err)
	}
}

func TestDeleteFragmentRemovesOrphanCardWhenNodeMissing(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-orphan-node", Kind: SourceKindText, URI: "manual://orphan-node", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{
		ID:       "orphan-card",
		SourceID: "src-orphan-node",
		NodeID:   "missing-node",
		Title:    "Orphan",
		Claim:    "stale node pointer still searchable",
		Summary:  "orphan",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteFragment(ctx, SearchResult{ResultType: "card", NodeID: "missing-node", CardID: "orphan-card"}); err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	var cards int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE id = 'orphan-card'`).Scan(&cards); err != nil || cards != 0 {
		t.Fatalf("orphan card = %d, err = %v", cards, err)
	}
}

func TestDeleteFragmentTableCardRemovesRow(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	var cardID, rowID, tableID string
	var beforeCount int
	if err := store.db.QueryRowContext(ctx, `SELECT c.id, c.row_id, r.table_id FROM kb_cards c JOIN kb_rows r ON r.id = c.row_id LIMIT 1`).Scan(&cardID, &rowID, &tableID); err != nil {
		t.Fatalf("read table card: %v", err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT row_count FROM kb_tables WHERE id = ?`, tableID).Scan(&beforeCount); err != nil {
		t.Fatalf("read row_count: %v", err)
	}
	if _, err := store.DeleteFragment(ctx, SearchResult{ResultType: "card", CardID: cardID, RowID: rowID}); err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	var rows, cards, afterCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_rows WHERE id = ?`, rowID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows = %d, err = %v", rows, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_cards WHERE id = ?`, cardID).Scan(&cards); err != nil || cards != 0 {
		t.Fatalf("cards = %d, err = %v", cards, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT row_count FROM kb_tables WHERE id = ?`, tableID).Scan(&afterCount); err != nil {
		t.Fatalf("read row_count after: %v", err)
	}
	if afterCount != beforeCount-1 {
		t.Fatalf("row_count = %d, want %d", afterCount, beforeCount-1)
	}
}

func TestDeleteFragmentReturnsDescendantNodeIDs(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-tree", Kind: SourceKindMarkdown, URI: "manual://tree", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "parent-node", SourceID: "src-tree", Type: "heading", Title: "Section", Text: "parent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, parent_id, type, title, text) VALUES ('child-node', 'src-tree', 'parent-node', 'paragraph', 'Child', 'child text')`); err != nil {
		t.Fatal(err)
	}

	got, err := store.DeleteFragment(ctx, SearchResult{ResultType: "node", NodeID: "parent-node"})
	if err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	want := map[string]bool{"parent-node": true, "child-node": true}
	if len(got) != 2 {
		t.Fatalf("node IDs = %#v, want parent and child", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected node id %q in %#v", id, got)
		}
	}
	var remaining int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_nodes WHERE id IN ('parent-node', 'child-node')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("remaining nodes = %d, err = %v", remaining, err)
	}
}

func TestDeleteFragmentRemovesDanglingTableFactWhenRowMissing(t *testing.T) {
	ctx := context.Background()
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	var factID, rowID string
	if err := store.db.QueryRowContext(ctx, `SELECT id, row_id FROM kb_facts LIMIT 1`).Scan(&factID, &rowID); err != nil {
		t.Fatalf("read table fact: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM kb_rows WHERE id = ?`, rowID); err != nil {
		t.Fatalf("delete row: %v", err)
	}
	if _, err := store.DeleteFragment(ctx, SearchResult{ResultType: "fact", FactID: factID, RowID: rowID}); err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	var facts int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_facts WHERE id = ?`, factID).Scan(&facts); err != nil || facts != 0 {
		t.Fatalf("dangling fact = %d, err = %v", facts, err)
	}
}

func TestDeleteFragmentEmptyTypeStillRemovesOrphanCard(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-empty-type", Kind: SourceKindText, URI: "manual://empty-type", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{
		ID:       "empty-type-card",
		SourceID: "src-empty-type",
		NodeID:   "missing-node",
		Title:    "Orphan",
		Claim:    "missing type still deletes the card",
		Summary:  "orphan",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteFragment(ctx, SearchResult{NodeID: "missing-node", CardID: "empty-type-card"}); err != nil {
		t.Fatalf("DeleteFragment: %v", err)
	}
	var cards int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE id = 'empty-type-card'`).Scan(&cards); err != nil || cards != 0 {
		t.Fatalf("orphan card = %d, err = %v", cards, err)
	}
}

func TestDeleteFragmentRequiresIdentity(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DeleteFragment(context.Background(), SearchResult{}); err == nil {
		t.Fatal("expected error for empty fragment")
	}
}

func TestResolveFragmentDeleteTarget(t *testing.T) {
	kind, id, err := resolveFragmentDeleteTarget(SearchResult{ResultType: "card", NodeID: "n1", CardID: "c1"})
	if err != nil || kind != "node" || id != "n1" {
		t.Fatalf("card with node = %s %s %v", kind, id, err)
	}
	kind, id, err = resolveFragmentDeleteTarget(SearchResult{ResultType: "card", CardID: "c1"})
	if err != nil || kind != "card" || id != "c1" {
		t.Fatalf("card without node = %s %s %v", kind, id, err)
	}
	kind, id, err = resolveFragmentDeleteTarget(SearchResult{ResultType: "fact", FactID: "f1", CardID: "c1", NodeID: "n1"})
	if err != nil || kind != "fact" || id != "f1" {
		t.Fatalf("fact = %s %s %v", kind, id, err)
	}
	kind, id, err = resolveFragmentDeleteTarget(SearchResult{ResultType: "card", CardID: "c1", RowID: "r1"})
	if err != nil || kind != "table_row" || id != "r1" {
		t.Fatalf("table card = %s %s %v", kind, id, err)
	}
	kind, id, err = resolveFragmentDeleteTarget(SearchResult{ResultType: "fact", FactID: "f1", RowID: "r1"})
	if err != nil || kind != "table_row" || id != "r1" {
		t.Fatalf("table fact = %s %s %v", kind, id, err)
	}
	kind, id, err = resolveFragmentDeleteTarget(SearchResult{ResultType: "table_row", RowID: "r1"})
	if err != nil || kind != "table_row" || id != "r1" {
		t.Fatalf("row = %s %s %v", kind, id, err)
	}
	kind, id, err = resolveFragmentDeleteTarget(SearchResult{NodeID: "n1", CardID: "c1"})
	if err != nil || kind != "node" || id != "n1" {
		t.Fatalf("empty type with node = %s %s %v", kind, id, err)
	}
}
