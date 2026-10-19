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

func TestMergeSearchResultsKeepsDistinctPersonalEvidence(t *testing.T) {
	personal := []knowledge.SearchResult{
		{ResultType: "card", CardID: "c1", Source: knowledge.Source{ID: "doc", Title: "Doc"}, Snippet: "card"},
		{ResultType: "fact", FactID: "f1", CardID: "c1", Source: knowledge.Source{ID: "doc", Title: "Doc"}, Snippet: "fact one"},
		{ResultType: "fact", FactID: "f1", CardID: "c1", Source: knowledge.Source{ID: "doc", Title: "Doc"}, Snippet: "same fact"},
		{ResultType: "table_row", RowID: "r1", Source: knowledge.Source{ID: "doc", Title: "Doc"}, Snippet: "row"},
	}
	enterprise := []knowledge.SearchResult{
		{ResultType: "card", CardID: "c9", Source: knowledge.Source{ID: "doc", Title: "Doc"}, Snippet: "enterprise copy"},
		{ResultType: "fact", FactID: "ef", Source: knowledge.Source{ID: "ent", Title: "Policy"}, Snippet: "remote"},
		{ResultType: "node", NodeID: "n2", Source: knowledge.Source{ID: "ent", Title: "Policy"}, Snippet: "second chunk"},
	}
	got := MergeSearchResults(personal, enterprise, 8, true)
	if len(got) != 4 {
		t.Fatalf("want card, fact, row, and one new enterprise source, got %+v", got)
	}
	if got[0].CardID != "c1" || got[1].FactID != "f1" || got[2].RowID != "r1" || got[3].Source.ID != "ent" {
		t.Fatalf("order = %+v", got)
	}
	if got[3].Source.Title != "[企业] Policy" {
		t.Fatalf("title = %q", got[3].Source.Title)
	}
}

func TestMergeSearchResultsKeepsRoomForANewEnterpriseSource(t *testing.T) {
	personal := []knowledge.SearchResult{
		{ResultType: "card", CardID: "c1", Source: knowledge.Source{ID: "doc"}, Snippet: "card"},
		{ResultType: "fact", FactID: "f1", CardID: "c1", Source: knowledge.Source{ID: "doc"}, Snippet: "fact"},
		{ResultType: "table_row", RowID: "r1", Source: knowledge.Source{ID: "doc"}, Snippet: "row"},
	}
	enterprise := []knowledge.SearchResult{
		{ResultType: "card", CardID: "ec", Source: knowledge.Source{ID: "ent", Title: "Policy"}, Snippet: "remote"},
	}
	tight := MergeSearchResults(personal, enterprise, 2, true)
	if len(tight) != 2 || tight[0].CardID != "c1" || tight[1].Source.ID != "ent" || tight[1].Source.Title != "[企业] Policy" {
		t.Fatalf("tight page = %+v", tight)
	}
	wide := MergeSearchResults(personal, enterprise, 4, true)
	if len(wide) != 4 || wide[0].CardID != "c1" || wide[1].FactID != "f1" || wide[2].RowID != "r1" || wide[3].Source.ID != "ent" {
		t.Fatalf("wide page = %+v", wide)
	}
}
