package knowledge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/rerank"
)

type countingReranker struct {
	mu    sync.Mutex
	calls int
	lastN int
	fn    func([]rerank.Passage) ([]float64, error)
}

func (c *countingReranker) Rerank(_ context.Context, _ string, passages []rerank.Passage) ([]float64, error) {
	c.mu.Lock()
	c.calls++
	c.lastN = len(passages)
	c.mu.Unlock()
	if c.fn != nil {
		return c.fn(passages)
	}
	scores := make([]float64, len(passages))
	return scores, nil
}

func (c *countingReranker) Calls() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.lastN
}

func TestKnowledgeRerankProjectBonusDoesNotOverturnGap(t *testing.T) {
	opts := SearchOptions{ProjectPath: "D:/project"}
	results := []SearchResult{
		{CardID: "far", ResultType: "card", Score: 1, Source: Source{ProjectPath: "D:/other"}},
		{CardID: "near", ResultType: "card", Score: 9, Source: Source{ProjectPath: "D:/project", SourceTrust: 1}},
	}
	// Relevance gap is 0.2. Project +0.05 and trust +0.05 must not flip it.
	ordered := orderKnowledgeResultsByRerank(opts, results, []float64{1, 0.8})
	if ordered[0].CardID != "far" {
		t.Fatalf("project bonus overturned a 0.2 relevance gap: %#v", ordered)
	}

	// Min-max stretches the ends to 0 and 1. The 0.01 neighbor stays in the
	// card's band, so type priority keeps the card ahead of the fact.
	near := []SearchResult{
		{CardID: "anchor", ResultType: "card", Score: 1},
		{CardID: "card", ResultType: "card", Score: 1},
		{FactID: "fact", ResultType: "fact", Score: 1},
	}
	banded := orderKnowledgeResultsByRerank(SearchOptions{}, near, []float64{1, 0, 0.01})
	if banded[0].CardID != "anchor" || banded[1].CardID != "card" || banded[2].FactID != "fact" {
		t.Fatalf("near blend should keep card priority, got %#v", banded)
	}
}

func TestKnowledgePassageTextDoesNotRepeatFact(t *testing.T) {
	text := knowledgePassageText(SearchResult{
		Subject:   "policy",
		Predicate: "requires",
		Object:    "review",
		Snippet:   "policy requires review",
		Claim:     "policy requires review before release",
	}, "policy")
	if strings.Count(text, "policy") != 2 {
		t.Fatalf("passage = %q", text)
	}
	card := knowledgePassageText(SearchResult{
		CardTitle: "policy",
		Claim:     "the policy GOLDNEEDLE is the important part",
		Snippet:   "policy GOLDNEEDLE",
	}, "policy")
	if strings.Count(card, "GOLDNEEDLE") != 1 {
		t.Fatalf("card passage = %q", card)
	}
}

func TestKnowledgePassageKeepsMatchPastTheCap(t *testing.T) {
	// Vector cards and LIKE nodes set Snippet to the whole field. The hit
	// sits past the prefix, so the passage has to follow the query.
	claim := strings.Repeat("甲", rerank.MaxPassageRunes) + " GOLDNEEDLE"
	text := knowledgePassageText(SearchResult{
		CardTitle: "policy",
		Claim:     claim,
		Snippet:   claim,
	}, "GOLDNEEDLE")
	if !strings.Contains(text, "GOLDNEEDLE") {
		t.Fatalf("match window dropped: %q", text)
	}
	if utf8.RuneCountInString(text) > rerank.MaxPassageRunes {
		t.Fatalf("passage runes = %d", utf8.RuneCountInString(text))
	}

	window := knowledgePassageText(SearchResult{
		Claim:   strings.Repeat("甲", rerank.MaxPassageRunes+40),
		Snippet: "... GOLDNEEDLE ...",
	}, "GOLDNEEDLE")
	if !strings.Contains(window, "GOLDNEEDLE") || utf8.RuneCountInString(window) > rerank.MaxPassageRunes {
		t.Fatalf("ellipsis window = %q (%d runes)", window, utf8.RuneCountInString(window))
	}

	long := strings.Repeat("甲", rerank.MaxPassageRunes+40)
	same := knowledgePassageText(SearchResult{Claim: long, Snippet: long}, "GOLDNEEDLE")
	if utf8.RuneCountInString(same) != rerank.MaxPassageRunes || strings.Count(same, "甲") != rerank.MaxPassageRunes {
		t.Fatalf("long duplicate snippet = %d runes", utf8.RuneCountInString(same))
	}
}

func TestKnowledgeQuestionAndStrictGate(t *testing.T) {
	if !knowledgeQueryIsQuestion("why does policy matter") || !knowledgeQueryIsQuestion("这是为什么？") {
		t.Fatal("question queries should open the gate")
	}
	if knowledgeQueryIsQuestion("policy token") || knowledgeQueryIsQuestion("show the policy") {
		t.Fatal("a keyword query is not a question")
	}
	if !knowledgeQueryIsQuestion("how does policy work") {
		t.Fatal("how should be a question word")
	}
	strict := SearchOptions{Query: "奇安信网神", Limit: 5}
	if !QuerySuppressesEmbedding(strict.Query) {
		t.Fatal("expected a strict name")
	}
	if knowledgeRerankPreGate(strict) {
		t.Fatal("strict names stay off the rerank gate")
	}
	if knowledgeRerankPreGate(SearchOptions{Query: "policy", Limit: knowledgeRerankPool}) {
		t.Fatal("a pool that is already the final limit has no headroom")
	}
}

func TestKnowledgeScanLimitNeverShrinks(t *testing.T) {
	if got := knowledgeScanLimit(20, false); got != 60 {
		t.Fatalf("historical limit = %d, want 60", got)
	}
	if got := knowledgeScanLimit(20, true); got != 60 {
		t.Fatalf("widening a limit of 20 must keep the historical 60, got %d", got)
	}
	if got := knowledgeScanLimit(5, false); got != 15 {
		t.Fatalf("narrow limit = %d, want 15", got)
	}
	if got := knowledgeScanLimit(5, true); got != knowledgeRerankPool {
		t.Fatalf("short scan = %d, want %d", got, knowledgeRerankPool)
	}
	if got := knowledgeScanLimit(40, true); got != 100 {
		t.Fatalf("capped scan = %d, want 100", got)
	}
}

func TestKnowledgeRerankRetrievalKeepsExplicitRoutes(t *testing.T) {
	strong := []SearchResult{{ResultType: "card", Score: 5}}
	fts := knowledgeRerankRetrieval(SearchOptions{Retrieval: knowledgeRetrievalFTS, Query: "policy?"}, strong, true)
	if fts.runEmbedding || fts.fuse {
		t.Fatalf("fts route = %+v", fts)
	}
	vector := knowledgeRerankRetrieval(SearchOptions{Retrieval: knowledgeRetrievalVector, Query: "policy?"}, strong, true)
	if !vector.runEmbedding || vector.fuse {
		t.Fatalf("vector route = %+v", vector)
	}
	auto := knowledgeRerankRetrieval(SearchOptions{Retrieval: knowledgeRetrievalAuto, Query: "policy?"}, strong, true)
	if !auto.runEmbedding || !auto.fuse {
		t.Fatalf("auto wide pool = %+v", auto)
	}
	hybrid := knowledgeRerankRetrieval(SearchOptions{Retrieval: knowledgeRetrievalHybrid, Query: "policy?"}, strong, true)
	if !hybrid.runEmbedding || !hybrid.fuse {
		t.Fatalf("hybrid route = %+v", hybrid)
	}
	if plan := knowledgeRerankRetrieval(SearchOptions{Query: "policy?"}, strong, false); plan.runEmbedding || plan.fuse {
		t.Fatalf("missing embedder = %+v", plan)
	}
	strict := knowledgeRerankRetrieval(SearchOptions{Retrieval: knowledgeRetrievalAuto, Query: "奇安信网神"}, strong, true)
	if strict.runEmbedding || strict.fuse {
		t.Fatalf("strict name = %+v", strict)
	}
}

func TestSearchRerankGateAndWidePool(t *testing.T) {
	ctx := context.Background()
	store := newRerankFixtureStore(t)
	defer store.Close()

	exact := &countingReranker{fn: func([]rerank.Passage) ([]float64, error) {
		t.Fatal("strict name search called the reranker")
		return nil, nil
	}}
	store.SetReranker(exact)
	without, err := searchCardIDs(store, SearchOptions{Query: "奇安信网神", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	store.SetReranker(nil)
	baseline, err := searchCardIDs(store, SearchOptions{Query: "奇安信网神", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	store.SetReranker(exact)
	again, err := searchCardIDs(store, SearchOptions{Query: "奇安信网神", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(again, ",") != strings.Join(baseline, ",") || strings.Join(without, ",") != strings.Join(baseline, ",") {
		t.Fatalf("strict name order changed: base %v with %v", baseline, again)
	}
	if calls, _ := exact.Calls(); calls != 0 {
		t.Fatalf("strict name calls = %d", calls)
	}

	strong := &countingReranker{fn: func([]rerank.Passage) ([]float64, error) {
		t.Fatal("high lexical score called the reranker")
		return nil, nil
	}}
	store.SetReranker(strong)
	strongHits, err := store.Search(ctx, SearchOptions{Query: "TaxAlphaUniqueToken", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(strongHits) == 0 || strongHits[0].Score < 2 {
		t.Fatalf("expected a lexical score >= 2, got %#v", strongHits)
	}
	if calls, _ := strong.Calls(); calls != 0 {
		t.Fatalf("high-score calls = %d", calls)
	}

	promote := &countingReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		scores := make([]float64, len(passages))
		for i, passage := range passages {
			if strings.Contains(passage.Text, "GOLDNEEDLE") {
				scores[i] = 10
			}
		}
		return scores, nil
	}}
	store.SetReranker(promote)
	promoted, err := store.Search(ctx, SearchOptions{Query: "policy?", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	calls, pool := promote.Calls()
	if calls != 1 {
		t.Fatalf("question calls = %d", calls)
	}
	if pool < 30 || pool > knowledgeRerankPool {
		t.Fatalf("rerank pool = %d, want 30..%d", pool, knowledgeRerankPool)
	}
	if len(promoted) != 5 {
		t.Fatalf("promoted len = %d", len(promoted))
	}
	if !searchHasCard(promoted, "kcard_gold") {
		t.Fatalf("gold card missing from top limit: %#v", cardIDs(promoted))
	}

	failed := &countingReranker{fn: func([]rerank.Passage) ([]float64, error) {
		return nil, errors.New("down")
	}}
	store.SetReranker(failed)
	fallback, err := store.Search(ctx, SearchOptions{Query: "policy?", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if searchHasCard(fallback, "kcard_gold") {
		t.Fatalf("error fallback kept the gold card inside the limit: %#v", cardIDs(fallback))
	}
	if len(fallback) != 5 {
		t.Fatalf("fallback len = %d", len(fallback))
	}

	explained, err := store.Explain(ctx, SearchOptions{Query: "policy?", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceContains(explained.Notes, "local_fts_no_llm") || !stringSliceContains(explained.Notes, "rerank_skipped") {
		t.Fatalf("explain notes = %#v", explained.Notes)
	}
	store.SetReranker(nil)
	off, err := store.Explain(ctx, SearchOptions{Query: "policy?", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceContains(off.Notes, "rerank_off") {
		t.Fatalf("nil reranker notes = %#v", off.Notes)
	}
	store.SetReranker(promote)
	on, err := store.Explain(ctx, SearchOptions{Query: "policy?", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceContains(on.Notes, "rerank_applied") {
		t.Fatalf("applied notes = %#v", on.Notes)
	}
}

func TestSearchRerankWidensCJKLikePool(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err := store.SaveSource(ctx, Source{
		ID: "ksrc_cjk", Kind: SourceKindText, URI: "knowledge://text/cjk", Title: "书籍",
		ContentHash: "cjk", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	// Rows live in the node table and not in FTS, which is the LIKE fallback
	// case. Page order would keep the gold node outside a final limit of 3.
	for i := 0; i < 20; i++ {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, page) VALUES (?, 'ksrc_cjk', 'paragraph', '条目', '书籍目录填充', ?)`, "cjk_"+rerankItoa(i), i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, page) VALUES ('cjk_gold', 'ksrc_cjk', 'paragraph', '条目', '书籍目录 GOLDNEEDLE', 100)`); err != nil {
		t.Fatal(err)
	}

	narrow, err := store.Search(ctx, SearchOptions{Query: "书?", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHasNode(narrow, "cjk_gold") {
		t.Fatalf("page-ordered LIKE pool discarded the relevant tail node: %#v", narrow)
	}

	promote := &countingReranker{fn: func(passages []rerank.Passage) ([]float64, error) {
		scores := make([]float64, len(passages))
		for i, passage := range passages {
			if strings.Contains(passage.Text, "GOLDNEEDLE") {
				scores[i] = 10
			}
		}
		return scores, nil
	}}
	store.SetReranker(promote)
	promoted, err := store.Search(ctx, SearchOptions{Query: "书?", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	calls, pool := promote.Calls()
	if calls != 1 || pool <= 3 {
		t.Fatalf("LIKE pool calls=%d pool=%d", calls, pool)
	}
	if !searchHasNode(promoted, "cjk_gold") {
		t.Fatalf("widened LIKE pool did not promote the gold node: %#v", promoted)
	}
}

func searchHasNode(results []SearchResult, id string) bool {
	for _, result := range results {
		if result.NodeID == id {
			return true
		}
	}
	return false
}

func newRerankFixtureStore(t *testing.T) *SQLiteStore {
	t.Helper()
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sources := []Source{
		{ID: "ksrc_name", Kind: SourceKindText, URI: "knowledge://text/name", Title: "名称", ContentHash: "name", ProjectPath: "D:/project", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_strong", Kind: SourceKindText, URI: "knowledge://text/strong", Title: "强命中", ContentHash: "strong", ProjectPath: "D:/project", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "ksrc_policy", Kind: SourceKindText, URI: "knowledge://text/policy", Title: "policy", ContentHash: "policy", ProjectPath: "D:/project", Status: StatusDistilled, FetchedAt: now, CreatedAt: now, UpdatedAt: now},
	}
	for _, source := range sources {
		if err := store.SaveSource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	saveSearchableCard(t, store, Card{ID: "kcard_name", SourceID: "ksrc_name", Title: "奇安信网神", Claim: "奇安信网神驻留在名称卡片里", ProjectPath: "D:/project"})
	saveSearchableCard(t, store, Card{ID: "kcard_strong", SourceID: "ksrc_strong", Title: "TaxAlphaUniqueToken", Claim: "TaxAlphaUniqueToken TaxAlphaUniqueToken TaxAlphaUniqueToken", ProjectPath: "D:/project"})
	for i := 0; i < 35; i++ {
		id := "kcard_policy_" + rerankItoa(i)
		claim := strings.Repeat("policy ", 12) + "filler card " + rerankItoa(i)
		saveSearchableCard(t, store, Card{ID: id, SourceID: "ksrc_policy", Title: "policy card " + rerankItoa(i), Claim: claim, ProjectPath: "D:/project"})
	}
	saveSearchableCard(t, store, Card{ID: "kcard_gold", SourceID: "ksrc_policy", Title: "policy tail", Claim: "policy GOLDNEEDLE", ProjectPath: "D:/project"})
	return store
}

// saveSearchableCard stores the claim on its own document node. Default search
// returns that body and still carries the card id.
func saveSearchableCard(t *testing.T, store *SQLiteStore, card Card) {
	t.Helper()
	ctx := context.Background()
	if strings.TrimSpace(card.ID) == "" {
		t.Fatal("card id is required")
	}
	if strings.TrimSpace(card.NodeID) == "" {
		card.NodeID = "knode_" + card.ID
	}
	text := strings.TrimSpace(card.Claim)
	if text == "" {
		text = strings.TrimSpace(card.Title)
	}
	title := strings.TrimSpace(card.Title)
	if title == "" {
		title = text
	}
	if err := store.SaveDocumentNode(ctx, DocumentNode{
		ID:       card.NodeID,
		SourceID: card.SourceID,
		Type:     "paragraph",
		Title:    title,
		Text:     text,
	}); err != nil {
		t.Fatalf("SaveDocumentNode %s: %v", card.ID, err)
	}
	if err := store.SaveCard(ctx, card); err != nil {
		t.Fatalf("SaveCard %s: %v", card.ID, err)
	}
}

func searchCardIDs(store *SQLiteStore, opts SearchOptions) ([]string, error) {
	results, err := store.Search(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	return cardIDs(results), nil
}

func cardIDs(results []SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		if result.CardID != "" {
			ids = append(ids, result.CardID)
		}
	}
	return ids
}

func searchHasCard(results []SearchResult, id string) bool {
	for _, result := range results {
		if result.CardID == id {
			return true
		}
	}
	return false
}

func rerankItoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
