package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveDocumentNodeAppliesMultilingualChunkingAndMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "manual-node-source", Kind: SourceKindText, URI: "manual://node", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("跨语言检索需要保留分块上下文。", 900)
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "manual-node", SourceID: "manual-node-source", Type: "paragraph", Title: "\ufeff多语言节点", Text: text}); err != nil {
		t.Fatalf("SaveDocumentNode: %v", err)
	}
	var parentText, parentVersion string
	if err := store.db.QueryRowContext(ctx, `SELECT text, json_extract(metadata_json, '$.chunker_version') FROM document_nodes WHERE id = 'manual-node'`).Scan(&parentText, &parentVersion); err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if parentText != "" || parentVersion != chunkerVersion {
		t.Fatalf("parent = text %q version %q", parentText, parentVersion)
	}
	var childCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_nodes WHERE parent_id = 'manual-node' AND json_extract(metadata_json, '$.language') = 'zh' AND json_extract(metadata_json, '$.script') = 'Hans'`).Scan(&childCount); err != nil {
		t.Fatalf("read children: %v", err)
	}
	if childCount < 2 {
		t.Fatalf("chunked children = %d, want >= 2", childCount)
	}
}

func TestSaveDocumentNodeRewriteRemovesOldTreeAndDerivedEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "rewrite-source", Kind: SourceKindText, URI: "manual://rewrite", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "rewrite-root",
		SourceID: "rewrite-source",
		Type:     "paragraph",
		Title:    "Old evidence",
		Text:     strings.Repeat("obsolete retrieval evidence ", 1000),
	}); err != nil {
		t.Fatalf("save long node: %v", err)
	}
	var childID string
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM document_nodes WHERE parent_id = 'rewrite-root' LIMIT 1`).Scan(&childID); err != nil {
		t.Fatalf("read old child: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_embedding_metadata(entity_type, entity_id, model_id, dimension, updated_at) VALUES ('node', ?, 'old-model', 2, 'now')`, childID); err != nil {
		t.Fatalf("insert node metadata: %v", err)
	}
	if err := store.SaveCard(ctx, Card{ID: "rewrite-card", SourceID: "rewrite-source", NodeID: childID, Title: "Old card", Claim: "obsolete retrieval evidence", Summary: "old"}); err != nil {
		t.Fatalf("save card: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_embedding_metadata(entity_type, entity_id, model_id, dimension, updated_at) VALUES ('card', 'rewrite-card', 'old-model', 2, 'now')`); err != nil {
		t.Fatalf("insert card metadata: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, insertFactSQL, "rewrite-fact", "rewrite-card", "rewrite-source", "obsolete", "states", "evidence", false, "", "", 0.8); err != nil {
		t.Fatalf("insert fact: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object) VALUES ('rewrite-fact', 'obsolete', 'states', 'evidence')`); err != nil {
		t.Fatalf("insert fact fts: %v", err)
	}

	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "rewrite-root", SourceID: "rewrite-source", Type: "paragraph", Title: "Fresh evidence", Text: "replacement retrieval evidence"}); err != nil {
		t.Fatalf("rewrite node: %v", err)
	}
	for _, check := range []struct {
		table string
		query string
	}{
		{"nodes", `SELECT COUNT(*) FROM document_nodes`},
		{"node fts", `SELECT COUNT(*) FROM document_nodes_fts`},
		{"cards", `SELECT COUNT(*) FROM knowledge_cards`},
		{"cards fts", `SELECT COUNT(*) FROM knowledge_cards_fts`},
		{"facts", `SELECT COUNT(*) FROM knowledge_facts`},
		{"facts fts", `SELECT COUNT(*) FROM knowledge_facts_fts`},
		{"embedding metadata", `SELECT COUNT(*) FROM knowledge_embedding_metadata`},
	} {
		var count int
		if err := store.db.QueryRowContext(ctx, check.query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.table, err)
		}
		want := 0
		if check.table == "nodes" || check.table == "node fts" {
			want = 1
		}
		if count != want {
			t.Fatalf("%s count = %d, want %d", check.table, count, want)
		}
	}
	var text string
	if err := store.db.QueryRowContext(ctx, `SELECT text FROM document_nodes WHERE id = 'rewrite-root'`).Scan(&text); err != nil || text != "replacement retrieval evidence" {
		t.Fatalf("replacement node text = %q, err = %v", text, err)
	}
	oldResults, err := store.Search(ctx, SearchOptions{Query: "obsolete", ResultTypes: []string{"node"}, Limit: 5})
	if err != nil {
		t.Fatalf("search old content: %v", err)
	}
	if len(oldResults) != 0 {
		t.Fatalf("old node remains searchable: %#v", oldResults)
	}
	newResults, err := store.Search(ctx, SearchOptions{Query: "replacement", ResultTypes: []string{"node"}, Limit: 5})
	if err != nil || len(newResults) != 1 || newResults[0].NodeID != "rewrite-root" {
		t.Fatalf("new node search = %#v, err = %v", newResults, err)
	}
}
