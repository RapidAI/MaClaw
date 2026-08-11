package enterpriseknowledge

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestMergeSearchResultsTagAndDedup(t *testing.T) {
	personal := []knowledge.SearchResult{
		{Source: knowledge.Source{ID: "p1", Title: "P"}, Snippet: "a"},
		{Source: knowledge.Source{ID: "shared", Title: "S"}, Snippet: "b"},
	}
	enterprise := []knowledge.SearchResult{
		{Source: knowledge.Source{ID: "shared", Title: "S"}, Snippet: "b"},
		{Source: knowledge.Source{ID: "e1", Title: "Policy"}, Snippet: "c"},
	}
	got := MergeSearchResults(personal, enterprise, 5, true)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[2].Source.ID != "e1" {
		t.Fatalf("want e1 last, got %+v", got)
	}
	if got[2].Source.Title != "[企业] Policy" {
		t.Fatalf("want tagged title, got %q", got[2].Source.Title)
	}
}
