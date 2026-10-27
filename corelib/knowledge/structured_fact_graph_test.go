package knowledge

import (
	"path/filepath"
	"testing"
)

func TestFactGraphIncludesStructuredTableFacts(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()
	seedLegacyStructuredTableFact(t, store)

	graph, err := store.FactGraph(t.Context(), SearchOptions{Entity: "张三", Limit: 20})
	if err != nil {
		t.Fatalf("FactGraph: %v", err)
	}
	if !hasFactEdge(graph.Edges, "张三", "部门", "法务") {
		t.Fatalf("expected structured edge 张三 部门 法务, got %#v", graph.Edges)
	}
}

func TestEntityProfileIncludesStructuredTableFacts(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()
	seedLegacyStructuredTableFact(t, store)

	profile, err := store.EntityProfile(t.Context(), SearchOptions{Entity: "张三", Limit: 20})
	if err != nil {
		t.Fatalf("EntityProfile: %v", err)
	}
	if !hasFactEdge(profile.Facts, "张三", "部门", "法务") {
		t.Fatalf("expected structured profile fact 张三 部门 法务, got %#v", profile.Facts)
	}
}

func TestFactGraphMatchesCJKQueryThroughNGrams(t *testing.T) {
	ctx := t.Context()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "fg-src", Kind: SourceKindText, URI: "memory://fg", Status: StatusParsed}); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if err := store.SaveCard(ctx, Card{ID: "fg-card", SourceID: "fg-src", Title: "文件系统", Claim: "文件系统负责管理磁盘", Summary: "文件系统"}); err != nil {
		t.Fatalf("SaveCard: %v", err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := insertFact(ctx, tx, Fact{ID: "fg-fact", CardID: "fg-card", SourceID: "fg-src", Subject: "文件系统", Predicate: "职责", Object: "管理磁盘"}); err != nil {
		t.Fatalf("insertFact: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// Simulate the dictionary split observed in older indexes: the exact span
	// token is absent, only its pieces and the overlapping bigrams are indexed.
	if _, err := store.db.ExecContext(ctx, `UPDATE knowledge_facts_fts SET subject = ? WHERE fact_id = ?`,
		"文件 系统 文件 件系 系统", "fg-fact"); err != nil {
		t.Fatalf("rewrite fts row: %v", err)
	}

	graph, err := store.FactGraph(ctx, SearchOptions{Query: "文件系统", Limit: 10})
	if err != nil {
		t.Fatalf("FactGraph: %v", err)
	}
	if !hasFactEdge(graph.Edges, "文件系统", "职责", "管理磁盘") {
		t.Fatalf("CJK fact query missed the segmented row: %#v", graph.Edges)
	}
}

func hasFactEdge(edges []FactGraphEdge, subject, predicate, object string) bool {
	for _, edge := range edges {
		if edge.Subject == subject && edge.Predicate == predicate && edge.Object == object {
			return true
		}
	}
	return false
}
