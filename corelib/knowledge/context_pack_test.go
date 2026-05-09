package knowledge

import (
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
