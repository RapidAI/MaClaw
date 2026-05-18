package bm25

import "testing"

func TestTokenizeAddsCJKFallbackNgrams(t *testing.T) {
	tokens := Tokenize("记忆证据导航")
	if !containsToken(tokens, "证据") || !containsToken(tokens, "导航") || !containsToken(tokens, "证据导") {
		t.Fatalf("expected CJK fallback ngrams in %v", tokens)
	}
}

func TestScoreMatchesCJKSubstringFallback(t *testing.T) {
	idx := New()
	idx.Add(Doc{ID: "scene", Text: "任务侧栏现在支持记忆证据导航和产物来源回查"})
	idx.Add(Doc{ID: "other", Text: "普通英文 deployment notes"})

	scores := idx.Score("证据导航")
	if scores["scene"] <= 0 {
		t.Fatalf("expected scene doc to match by CJK fallback ngrams, got %v", scores)
	}
	if scores["other"] > 0 {
		t.Fatalf("unrelated doc should not match CJK query, got %v", scores)
	}
}

func containsToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}

func TestScoreSubsetMatchesScoreForAllowedIDs(t *testing.T) {
	idx := New()
	idx.Rebuild([]Doc{
		{ID: "a", Text: "证据导航 source evidence"},
		{ID: "b", Text: "unrelated deployment note"},
		{ID: "c", Text: "证据导航 read file source"},
	})
	full := idx.Score("证据导航")
	allowed := map[string]struct{}{"a": {}, "c": {}}
	subset := idx.ScoreSubset("证据导航", allowed)
	if len(subset) != 2 {
		t.Fatalf("expected two subset hits, got %#v", subset)
	}
	for id, got := range subset {
		if want := full[id]; got != want {
			t.Fatalf("score mismatch for %s: got %v want %v", id, got, want)
		}
	}
	if _, ok := subset["b"]; ok {
		t.Fatalf("subset should not score disallowed id b: %#v", subset)
	}
}
