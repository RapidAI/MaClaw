package knowledge

import (
	"context"
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
