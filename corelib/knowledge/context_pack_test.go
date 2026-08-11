package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextPackFactTextDeduplicatesSnippet(t *testing.T) {
	result := SearchResult{
		ResultType: "fact",
		Subject:    "知识库接口",
		Predicate:  "提供",
		Object:     "来源摘要",
		Snippet:    "知识库接口 提供 来源摘要",
	}

	text := contextPackText(result)
	if strings.Count(text, "知识库接口 提供 来源摘要") != 1 {
		t.Fatalf("expected deduplicated fact text, got %q", text)
	}
}

func TestContextPackTitleDoesNotExposeImagePathMetadata(t *testing.T) {
	privatePath := `C:\\private\\knowledge_assets\\diagram.png`
	result := SearchResult{
		ResultType: "node",
		NodeType:   NodeTypeImage,
		NodeTitle:  privatePath,
		Source: Source{
			ID:           "safe-image-id",
			Kind:         SourceKindImage,
			Title:        privatePath,
			RelativePath: privatePath,
			CanonicalURI: "file://" + privatePath,
			URI:          privatePath,
		},
	}
	if got := contextPackTitle(result); got != "safe-image-id" {
		t.Fatalf("contextPackTitle = %q, want safe image ID", got)
	}
}

func TestContextPackDoesNotExposeImagePathThroughCitations(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	privatePath := `C:\\private\\knowledge_assets\\diagram.png`
	if err := store.SaveSource(ctx, Source{
		ID:           "safe-image-id",
		Kind:         SourceKindImage,
		URI:          privatePath,
		CanonicalURI: "file://" + privatePath,
		RelativePath: privatePath,
		Title:        privatePath,
		Status:       StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       "safe-image-node",
		SourceID: "safe-image-id",
		Type:     NodeTypeImage,
		Title:    privatePath,
		Text:     "gateway architecture image evidence",
	}); err != nil {
		t.Fatal(err)
	}
	pack, err := store.ContextPack(ctx, ContextPackOptions{SearchOptions: SearchOptions{Query: "gateway architecture", Limit: 5}, MaxItems: 1, MaxChars: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Items) != 1 || len(pack.Citations) != 1 {
		t.Fatalf("context pack = %#v", pack)
	}
	item, citation := pack.Items[0], pack.Citations[0]
	for _, value := range []string{item.Title, item.Citation, citation.Label, citation.SourceTitle, citation.URI, citation.RelativePath} {
		if strings.Contains(value, privatePath) || strings.Contains(value, "file://") {
			t.Fatalf("context pack leaked image path in %q", value)
		}
	}
	if item.Title != "safe-image-id" || citation.SourceID != "safe-image-id" {
		t.Fatalf("context pack lost safe image identity: item=%#v citation=%#v", item, citation)
	}
}

func TestFormatResultCitationIncludesFactEvidence(t *testing.T) {
	result := SearchResult{
		Source:     Source{ID: "s1", Title: "中文知识结构化"},
		ResultType: "fact",
		Subject:    "知识库接口",
		Predicate:  "提供",
		Object:     "来源摘要",
	}

	citation := formatResultCitation(result)
	if !strings.Contains(citation, "中文知识结构化") || !strings.Contains(citation, "fact: 知识库接口 提供 来源摘要") {
		t.Fatalf("expected fact evidence in citation, got %q", citation)
	}
}

func TestSortSearchResultsPrefersCardsWhenScoresAreClose(t *testing.T) {
	results := []SearchResult{
		{ResultType: "fact", Citation: "b", Score: 1.20},
		{ResultType: "card", Citation: "a", Score: 1.00},
		{ResultType: "node", Citation: "c", Score: 0.98},
	}

	sortSearchResults(results)
	if results[0].ResultType != "card" || results[1].ResultType != "fact" {
		t.Fatalf("expected card before close-scored fact: %#v", results)
	}
	results = []SearchResult{
		{ResultType: "fact", Citation: "b", Score: 1.80},
		{ResultType: "card", Citation: "a", Score: 1.00},
	}
	sortSearchResults(results)
	if results[0].ResultType != "fact" {
		t.Fatalf("expected high-scored fact to stay first: %#v", results)
	}
}

func TestBuildFTSQueryQuotesChinesePhraseAndSplitsSpacedTerms(t *testing.T) {
	if got := buildFTSQuery("来源摘要"); got != `"来源摘要"` {
		t.Fatalf("unexpected Chinese phrase query: %q", got)
	}
	if got := buildFTSQuery("知识库 来源摘要"); got != `"知识库" AND "来源摘要"` {
		t.Fatalf("unexpected spaced Chinese query: %q", got)
	}
}
