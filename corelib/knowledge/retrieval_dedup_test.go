package knowledge

import "testing"

func TestRRFFuseDeduplicatesExactEvidenceAcrossIndexes(t *testing.T) {
	source := Source{ID: "source-a", ContentHash: "hash-a"}
	text := "The deployment endpoint is api.example.test and the port is 18099."
	fts := []SearchResult{{ResultType: "card", CardID: "card-a", Source: source, Claim: text, Score: 2}}
	emb := []SearchResult{{ResultType: "node", NodeID: "node-a", Source: source, Snippet: "  THE deployment endpoint is api.example.test and the port is 18099. ", Score: 3}}
	got := rrfFuse(fts, emb, 10)
	if len(got) != 1 {
		t.Fatalf("exact evidence should merge, got %d: %+v", len(got), got)
	}
	if got[0].Score < 3 {
		t.Fatalf("merged evidence lost best score: %+v", got[0])
	}
}

func TestRRFFuseKeepsDistinctFactsFromSameSource(t *testing.T) {
	source := Source{ID: "source-a", ContentHash: "hash-a"}
	fts := []SearchResult{{ResultType: "fact", FactID: "fact-a", Source: source, Subject: "service", Predicate: "port", Object: "18099", Claim: "The service port is 18099 and uses HTTPS."}}
	emb := []SearchResult{{ResultType: "fact", FactID: "fact-b", Source: source, Subject: "service", Predicate: "model", Object: "GLM", Claim: "The service model is GLM and supports tool calls."}}
	got := rrfFuse(fts, emb, 10)
	if len(got) != 2 {
		t.Fatalf("distinct evidence was collapsed: %+v", got)
	}
}

func TestRRFFuseKeepsRepeatedEvidenceAtDifferentLocations(t *testing.T) {
	source := Source{ID: "source-a", ContentHash: "hash-a"}
	text := "This legal disclaimer is intentionally repeated on every signed page."
	fts := []SearchResult{{ResultType: "node", NodeID: "node-1", Source: source, Page: 1, Snippet: text}}
	emb := []SearchResult{{ResultType: "node", NodeID: "node-2", Source: source, Page: 2, Snippet: text}}
	got := rrfFuse(fts, emb, 10)
	if len(got) != 2 {
		t.Fatalf("evidence from different locations was collapsed: %+v", got)
	}
}

func TestRRFFuseDoesNotMergeDifferentSourcesBySharedCanonicalURI(t *testing.T) {
	text := "The archived release record contains this exact long deployment statement."
	fts := []SearchResult{{
		ResultType: "node",
		NodeID:     "node-a",
		Source:     Source{CanonicalURI: "https://example.test/releases", OwnerID: "owner-a"},
		Snippet:    text,
	}}
	emb := []SearchResult{{
		ResultType: "node",
		NodeID:     "node-b",
		Source:     Source{CanonicalURI: "https://example.test/releases", OwnerID: "owner-b"},
		Snippet:    text,
	}}
	got := rrfFuse(fts, emb, 10)
	if len(got) != 2 {
		t.Fatalf("results without persisted source identity must not merge by URI: %+v", got)
	}
}
