package knowledge

import "testing"

func TestEscapeLikePatternKeepsUserTermsLiteral(t *testing.T) {
	got := escapeLikePattern("版本_2% \\ 中文")
	if want := "版本\\_2\\% \\\\ 中文"; got != want {
		t.Fatalf("escapeLikePattern() = %q, want %q", got, want)
	}
}

func TestSearchResultIDMergeNamespacesCallerSuppliedIDs(t *testing.T) {
	seen := make(map[string]struct{})
	markSearchResultIDs(seen, SearchResult{ResultType: "card", CardID: "shared-id"})
	if searchResultIDsSeen(seen, SearchResult{ResultType: "fact", FactID: "shared-id", CardID: "other-card"}) {
		t.Fatal("a card ID must not suppress a fact with the same caller-supplied ID")
	}
	markSearchResultIDs(seen, SearchResult{ResultType: "fact", FactID: "shared-id", CardID: "other-card"})
	if !searchResultIDsSeen(seen, SearchResult{ResultType: "fact", FactID: "shared-id", CardID: "another-card"}) {
		t.Fatal("the same fact ID must remain deduplicated")
	}
}
