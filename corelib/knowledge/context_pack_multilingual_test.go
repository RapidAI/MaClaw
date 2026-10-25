package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchHydratesMultilingualChunkMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-ja", Kind: SourceKindText, URI: "memory://ja", Title: "日本語", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "parent-ja", SourceID: "src-ja", Type: "section", Title: "親見出し", Text: "親の説明", Metadata: map[string]string{"language": "ja", "script": "Jpan"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "child-ja", SourceID: "src-ja", ParentID: "parent-ja", Type: "paragraph", Title: "子", Text: "機械学習の検索精度を改善する", Metadata: map[string]string{"language": "ja", "script": "Jpan", "chunk_index": "1"}}); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "検索精度", ResultTypes: []string{"node"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected Japanese node search result")
	}
	for _, result := range results {
		if result.NodeID != "child-ja" {
			continue
		}
		if result.ParentNodeID != "parent-ja" || result.Language != "ja" || result.Script != "Jpan" {
			t.Fatalf("metadata not hydrated: %#v", result)
		}
		return
	}
	t.Fatalf("child node missing from results: %#v", results)
}

func TestContextPackAddsParentAndNeighborChunkContext(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-chunk", Kind: SourceKindText, URI: "memory://chunk", Title: "多语言指南", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	parent := DocumentNode{ID: "parent-chunk", SourceID: "src-chunk", Type: "section", Title: "部署流程", Text: "章节总览", Metadata: map[string]string{"language": "zh", "script": "Hans"}}
	if err := store.SaveDocumentNode(ctx, parent); err != nil {
		t.Fatal(err)
	}
	children := []DocumentNode{
		{ID: "chunk-1", SourceID: "src-chunk", ParentID: parent.ID, Type: "paragraph", Offset: 1, Text: "前置步骤：准备语料与标注集。", Metadata: map[string]string{"language": "zh", "script": "Hans"}},
		{ID: "chunk-2", SourceID: "src-chunk", ParentID: parent.ID, Type: "paragraph", Offset: 2, Text: "目标内容：检索系统必须支持跨语言召回。", Metadata: map[string]string{"language": "zh", "script": "Hans"}},
		{ID: "chunk-3", SourceID: "src-chunk", ParentID: parent.ID, Type: "paragraph", Offset: 3, Text: "后续步骤：监控召回率和延迟。", Metadata: map[string]string{"language": "zh", "script": "Hans"}},
	}
	for _, child := range children {
		if err := store.SaveDocumentNode(ctx, child); err != nil {
			t.Fatal(err)
		}
	}
	pack, err := store.ContextPack(ctx, ContextPackOptions{SearchOptions: SearchOptions{Query: "跨语言召回", ResultTypes: []string{"node"}, Limit: 5}, MaxItems: 1, MaxChars: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Items) != 1 {
		t.Fatalf("items = %#v", pack.Items)
	}
	text := pack.Items[0].Text
	for _, want := range []string{"部署流程", "前置步骤", "目标内容", "后续步骤"} {
		if !strings.Contains(text, want) {
			t.Fatalf("context lacks %q: %q", want, text)
		}
	}
	if !hasContextPackNote(pack.Notes, "parent_neighbor_context") {
		t.Fatalf("missing parent/neighbor note: %#v", pack.Notes)
	}
}

func TestSelectContextPackResultsDiversifiesSiblingChunks(t *testing.T) {
	results := []SearchResult{
		{ResultType: "node", NodeID: "chunk-1", ParentNodeID: "parent", Score: 10, Source: Source{ID: "one"}, Snippet: "多语言检索的第一段"},
		{ResultType: "node", NodeID: "chunk-2", ParentNodeID: "parent", Score: 9.9, Source: Source{ID: "one"}, Snippet: "多语言检索的第二段"},
		{ResultType: "node", NodeID: "other", Score: 9.5, Source: Source{ID: "two"}, Snippet: "独立文档的跨语言评测"},
	}
	selected := selectContextPackResults(results, 2)
	if len(selected) != 2 || selected[0].NodeID != "chunk-1" || selected[1].NodeID != "other" {
		t.Fatalf("MMR selection = %#v", selected)
	}
}

func TestContextPackUsesAutomaticChunkOrder(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-auto-chunks", Kind: SourceKindText, URI: "memory://auto-chunks", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	var sourceText strings.Builder
	for i := 0; i < 900; i++ {
		fmt.Fprintf(&sourceText, "唯一标记%03d 自动分块顺序测试 ", i)
	}
	nodes := splitNodesToTokenBudget([]DocumentNode{{
		ID: "auto-parent", SourceID: "src-auto-chunks", Type: "section", Title: "自动分块", Offset: 7,
		Text: sourceText.String(),
	}})
	if len(nodes) < 4 {
		t.Fatalf("automatic chunks = %d", len(nodes))
	}
	for _, node := range nodes {
		if err := store.SaveDocumentNode(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	current := nodes[2]
	results := []SearchResult{{ResultType: "node", NodeID: current.ID, ParentNodeID: current.ParentID, Snippet: current.Text}}
	enriched, _, err := store.enrichContextPackNodes(ctx, current.Text, results)
	if err != nil {
		t.Fatal(err)
	}
	text := enriched[0].Snippet
	preceding := "[preceding] " + searchBodyTextWindow(nodes[1].Text, current.Text)
	following := "[following] " + searchBodyTextWindow(nodes[3].Text, current.Text)
	currentPos := strings.Index(text, searchBodyTextWindowLimit(current.Text, current.Text, contextPackNodeWindowRunes))
	precedingPos := strings.Index(text, preceding)
	if currentPos < 0 || precedingPos < 0 || currentPos > precedingPos || !strings.Contains(text, following) {
		t.Fatalf("neighbor order or budget mismatch: %q", text)
	}
}

func TestContextPackToleratesMalformedChunkMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-bad-meta", Kind: SourceKindText, URI: "memory://bad-meta", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	for _, node := range []DocumentNode{
		{ID: "bad-parent", SourceID: "src-bad-meta", Type: "section", Title: "父节点"},
		{ID: "bad-child", SourceID: "src-bad-meta", ParentID: "bad-parent", Type: "paragraph", Text: "正文仍然可读"},
	} {
		if err := store.SaveDocumentNode(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE document_nodes SET metadata_json = '{' WHERE id = 'bad-child'`); err != nil {
		t.Fatal(err)
	}
	results := []SearchResult{{ResultType: "node", NodeID: "bad-child", ParentNodeID: "bad-parent"}}
	if _, _, err := store.enrichContextPackNodes(ctx, "正文", results); err != nil {
		t.Fatalf("malformed metadata blocked context: %v", err)
	}
	if _, err := store.ListNodesBySource(ctx, "src-bad-meta", 10); err != nil {
		t.Fatalf("malformed metadata blocked source nodes: %v", err)
	}
}
