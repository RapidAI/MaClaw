package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollapseSearchBodiesKeepsNodeTextAndClaim(t *testing.T) {
	node := SearchResult{
		ResultType: "node",
		NodeID:     "n1",
		Snippet:    "the passage window about patents",
		Score:      1.08,
		Source:     Source{ID: "s1", Title: "Notes"},
	}
	card := SearchResult{
		ResultType: "card",
		NodeID:     "n1",
		CardID:     "c1",
		CardTitle:  "Patent card",
		Claim:      "the family holds twelve patents",
		Score:      2.30,
		Source:     Source{ID: "s1", Title: "Notes"},
	}
	fact := SearchResult{
		ResultType: "fact",
		NodeID:     "n1",
		CardID:     "c1",
		FactID:     "f1",
		Subject:    "family",
		Predicate:  "holds",
		Object:     "twelve patents",
		Claim:      "the family holds twelve patents",
		Score:      1.18,
		Source:     Source{ID: "s1", Title: "Notes"},
	}
	loose := SearchResult{ResultType: "card", CardID: "loose", Claim: "no body", Score: 9}

	got := collapseSearchBodies([]SearchResult{card, fact, node, loose})
	if len(got) != 2 {
		t.Fatalf("bodies = %d, want 2: %#v", len(got), got)
	}
	hit := got[0]
	if got[1].ResultType != "card" || got[1].CardID != "loose" || got[1].Claim != "no body" {
		t.Fatalf("standalone card = %#v", got[1])
	}
	if hit.ResultType != "node" || hit.NodeID != "n1" || hit.CardID != "c1" || hit.FactID != "f1" {
		t.Fatalf("hit = %#v", hit)
	}
	if hit.Snippet != node.Snippet {
		t.Fatalf("body = %q", hit.Snippet)
	}
	if hit.Object != "twelve patents" || hit.Claim != "the family holds twelve patents" {
		t.Fatalf("claim annotation = %#v", hit)
	}
	if hit.Score != 2.30 {
		t.Fatalf("score = %v, want the unmodified relevance score", hit.Score)
	}
	if searchClaimLine(hit) != "family holds twelve patents" {
		t.Fatalf("claim line = %q", searchClaimLine(hit))
	}
	if searchBodyText(hit) != node.Snippet {
		t.Fatalf("presented body = %q", searchBodyText(hit))
	}
}

func TestCollapseSearchBodiesKeepsRowText(t *testing.T) {
	row := SearchResult{
		ResultType: "table_row",
		RowID:      "r1",
		Summary:    "姓名: 张三 | 部门: 法务",
		Snippet:    "法务",
		Claim:      "张三",
		Score:      1.6,
	}
	card := SearchResult{
		ResultType: "card",
		RowID:      "r1",
		CardID:     "c1",
		CardTitle:  "张三",
		Claim:      "姓名: 张三 | 部门: 法务",
		Score:      1.9,
	}
	fact := SearchResult{
		ResultType: "fact",
		RowID:      "r1",
		CardID:     "c1",
		FactID:     "f1",
		Subject:    "张三",
		Predicate:  "部门",
		Object:     "法务",
		Score:      1.78,
	}
	got := collapseSearchBodies([]SearchResult{fact, card, row})
	if len(got) != 1 || got[0].ResultType != "table_row" || got[0].RowID != "r1" {
		t.Fatalf("hit = %#v", got)
	}
	if got[0].Summary != row.Summary || got[0].CardID != "c1" || got[0].FactID != "f1" || got[0].Object != "法务" {
		t.Fatalf("row body = %#v", got[0])
	}
	if searchBodyText(got[0]) != row.Summary {
		t.Fatalf("presented body = %q", searchBodyText(got[0]))
	}
}

func TestSearchLimitCountsDistinctBodies(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-bodies", Kind: SourceKindText, URI: "manual://bodies", Title: "Bodies", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		text := "alphaneedle appears once in a long note. " + strings.Repeat("filler word ", 80) + id
		if id == "a" || id == "b" {
			text = "alphaneedle"
		}
		if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "node-" + id, SourceID: "src-bodies", Type: "paragraph", Title: id, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b"} {
		if err := store.SaveCard(ctx, Card{
			ID:       "card-" + id,
			SourceID: "src-bodies",
			NodeID:   "node-" + id,
			Title:    "alphaneedle card " + id,
			Claim:    "alphaneedle card claim " + id,
			Summary:  "alphaneedle",
		}); err != nil {
			t.Fatal(err)
		}
		tx, err := store.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := insertFact(ctx, tx, Fact{
			ID:        "fact-" + id,
			CardID:    "card-" + id,
			SourceID:  "src-bodies",
			Subject:   "alphaneedle",
			Predicate: "marks",
			Object:    id,
		}); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveCard(ctx, Card{
		ID:       "card-loose",
		SourceID: "src-bodies",
		Title:    "loose",
		Claim:    "loosezebra marker",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.Search(ctx, SearchOptions{Query: "alphaneedle", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3 distinct bodies: %#v", len(results), results)
	}
	seen := map[string]struct{}{}
	hasA, hasB := false, false
	for _, result := range results {
		key := searchBodyKey(result)
		if key == "" || result.ResultType != "node" {
			t.Fatalf("result = %#v", result)
		}
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate body %s", key)
		}
		seen[key] = struct{}{}
		if result.NodeID == "node-a" {
			hasA = true
			if result.CardID != "card-a" || result.FactID != "fact-a" || !strings.Contains(result.Snippet, "alphaneedle") {
				t.Fatalf("node-a body = %#v", result)
			}
		}
		if result.NodeID == "node-b" {
			hasB = true
		}
		if result.CardID == "card-loose" {
			t.Fatalf("loose card entered the default list: %#v", result)
		}
	}
	if !hasA || !hasB {
		t.Fatalf("strong bodies missing: %#v", results)
	}

	var indexed int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards_fts WHERE knowledge_cards_fts MATCH ?`, "loosezebra").Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if indexed != 1 {
		t.Fatalf("loose card fts hits = %d", indexed)
	}
	loose, err := store.Search(ctx, SearchOptions{Query: "loosezebra", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(loose) != 1 || loose[0].CardID != "card-loose" || loose[0].ResultType != "card" {
		t.Fatalf("standalone card search = %#v", loose)
	}
}

func TestSearchBodyTextWindowFollowsQueryMatch(t *testing.T) {
	text := strings.Repeat("无关前缀", 150) + "关键证据在正文中部" + strings.Repeat("无关后缀", 150)
	window := searchBodyTextWindow(text, "关键证据")
	if !strings.Contains(window, "关键证据在正文中部") {
		t.Fatalf("query window = %q", window)
	}
	if strings.HasPrefix(window, "无关前缀") {
		t.Fatalf("query window retained the irrelevant prefix: %q", window)
	}
}

func TestCJKLikeFallbackHonorsFactOnly(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-like-types", Kind: SourceKindText, URI: "memory://like-types", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "like-node", SourceID: "src-like-types", Type: "paragraph", Text: "法务正文"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "like-card", SourceID: "src-like-types", NodeID: "like-node", Claim: "法务卡片"}); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertFact(ctx, tx, Fact{ID: "like-fact", CardID: "like-card", SourceID: "src-like-types", Subject: "张三", Predicate: "部门", Object: "法务"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	results, err := store.searchCJKLikeFallback(ctx, SearchOptions{Query: "法务", ResultTypes: []string{"fact"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ResultType != "fact" || results[0].FactID != "like-fact" {
		t.Fatalf("fact-only LIKE results = %#v", results)
	}
}
