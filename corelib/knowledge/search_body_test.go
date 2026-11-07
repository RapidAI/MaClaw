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

func TestSearchReplacesFTSSnippetWithStoredNodeText(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-stored", Kind: SourceKindText, URI: "manual://stored", Title: "API2", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	// Longer than the 32-token FTS highlight, still inside one chunk.
	text := "api2hostrecord " + strings.Repeat("detail ", 40) + "TAILMARKER end of the stored note"
	if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "node-stored", SourceID: "src-stored", Type: "paragraph", Title: "API2", Text: text}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{
		ID:       "card-stored",
		SourceID: "src-stored",
		NodeID:   "node-stored",
		Title:    "API2",
		Claim:    "api2hostrecord claimmarker ssh port stays on the hit",
	}); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "api2hostrecord", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	got := results[0]
	if !strings.Contains(got.Snippet, "TAILMARKER end of the stored note") {
		t.Fatalf("stored tail missing from snippet: %q", got.Snippet)
	}
	if strings.HasSuffix(strings.TrimSpace(got.Snippet), "...") {
		t.Fatalf("FTS highlight kept: %q", got.Snippet)
	}
	evidence := SearchEvidenceText(got)
	if !strings.Contains(evidence, "TAILMARKER") || !strings.Contains(evidence, "claimmarker ssh port stays on the hit") {
		t.Fatalf("evidence = %q", evidence)
	}
}

func TestSearchAttachesChunkNeighbors(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-neighbors", Kind: SourceKindText, URI: "manual://neighbors", Title: "API2", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	// Longer than the context-pack title cut. The model read must keep it whole.
	parentTitle := strings.Repeat("甲", 200)
	parent := DocumentNode{ID: "parent-neighbors", SourceID: "src-neighbors", Type: "section", Title: parentTitle, Text: ""}
	if err := store.SaveDocumentNode(ctx, parent); err != nil {
		t.Fatal(err)
	}
	children := []DocumentNode{
		{ID: "neighbor-0", SourceID: "src-neighbors", ParentID: parent.ID, Type: "paragraph", Offset: 0, Text: strings.Repeat("前段填充 ", 80) + "HEADMARKER starts the previous chunk."},
		{ID: "neighbor-1", SourceID: "src-neighbors", ParentID: parent.ID, Type: "paragraph", Offset: 1, Text: "api2hostrecord lives in the first chunk."},
		// A blank sibling sits between the hit and the next stored chunk.
		{ID: "neighbor-gap", SourceID: "src-neighbors", ParentID: parent.ID, Type: "image", Offset: 2, Text: ""},
		{ID: "neighbor-2", SourceID: "src-neighbors", ParentID: parent.ID, Type: "paragraph", Offset: 3, Text: strings.Repeat("邻块填充 ", 80) + "TAILMARKER is past the short neighbor window."},
	}
	for _, child := range children {
		if err := store.SaveDocumentNode(ctx, child); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.Search(ctx, SearchOptions{Query: "api2hostrecord", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no hit")
	}
	for _, result := range results {
		if strings.Contains(result.Snippet, "[following]") || strings.Contains(result.Snippet, "[preceding]") {
			t.Fatalf("header search gained neighbor markers: %q", result.Snippet)
		}
	}
	expanded, err := store.AttachSearchNodeContext(ctx, "api2hostrecord", results)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, result := range expanded {
		snippet := result.Snippet
		head := strings.Index(snippet, "HEADMARKER starts the previous chunk.")
		hit := strings.Index(snippet, "api2hostrecord lives in the first chunk.")
		tail := strings.Index(snippet, "TAILMARKER is past the short neighbor window.")
		if head < 0 || hit < 0 || tail < 0 || head > hit || hit > tail {
			continue
		}
		if !strings.Contains(snippet, parentTitle) || strings.Contains(snippet, "...") {
			t.Fatalf("stored record was cut: %q", snippet)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("chunk neighbor missing: %#v", expanded)
	}
}

func TestSearchReadsEmptyChunkParentFromChildren(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(ctx, Source{ID: "src-parent", Kind: SourceKindText, URI: "manual://parent", Title: "API2", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	parentTitle := "api2parenttitle " + strings.Repeat("甲", 200)
	parent := DocumentNode{ID: "parent-empty", SourceID: "src-parent", Type: "section", Title: parentTitle, Text: ""}
	if err := store.SaveDocumentNode(ctx, parent); err != nil {
		t.Fatal(err)
	}
	children := []DocumentNode{
		{ID: "parent-child-0", SourceID: "src-parent", ParentID: parent.ID, Type: "paragraph", Offset: 0, Text: "HEADMARKER is the first stored chunk."},
		{ID: "parent-child-gap", SourceID: "src-parent", ParentID: parent.ID, Type: "image", Offset: 1, Text: ""},
		{ID: "parent-child-1", SourceID: "src-parent", ParentID: parent.ID, Type: "paragraph", Offset: 2, Text: "TAILMARKER is the next stored chunk."},
	}
	for _, child := range children {
		if err := store.SaveDocumentNode(ctx, child); err != nil {
			t.Fatal(err)
		}
	}
	results, err := store.Search(ctx, SearchOptions{Query: "api2parenttitle", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no hit")
	}
	for _, result := range results {
		if strings.Contains(result.Snippet, "[following]") || strings.Contains(result.Snippet, "[preceding]") {
			t.Fatalf("header search gained neighbor markers: %q", result.Snippet)
		}
	}
	expanded, err := store.AttachSearchNodeContext(ctx, "api2parenttitle", results)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, result := range expanded {
		if result.NodeID != parent.ID {
			continue
		}
		snippet := result.Snippet
		head := strings.Index(snippet, "HEADMARKER is the first stored chunk.")
		tail := strings.Index(snippet, "TAILMARKER is the next stored chunk.")
		if head < 0 || tail < 0 || head > tail || !strings.Contains(snippet, parentTitle) || strings.Contains(snippet, "...") {
			t.Fatalf("empty parent kept a cut highlight: %q", snippet)
		}
		found = true
	}
	if !found {
		t.Fatalf("empty parent missing: %#v", expanded)
	}
}

func TestSearchEvidenceSkipsCutClaimAlreadyInBody(t *testing.T) {
	body := "api2hostrecord\n\n" + strings.Repeat("主机记录保持完整 ", 40) + "TAILMARKER"
	claim := truncateRunes(normalizeWhitespace(body), cardClaimRunes)
	if !strings.HasSuffix(claim, "...") {
		t.Fatalf("fixture claim was not cut: %q", claim)
	}
	evidence := SearchEvidenceText(SearchResult{ResultType: "node", Snippet: body, Claim: claim})
	if strings.Contains(evidence, "...") || !strings.Contains(evidence, "TAILMARKER") {
		t.Fatalf("cut claim repeated after the stored body: %q", evidence)
	}
	otherClaim := truncateRunes("独立结论 "+strings.Repeat("不在正文里 ", 40), cardClaimRunes)
	other := SearchEvidenceText(SearchResult{ResultType: "node", Snippet: body, Claim: otherClaim})
	if !strings.Contains(other, "独立结论") || strings.Contains(other, "...") {
		t.Fatalf("claim outside the body = %q", other)
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
