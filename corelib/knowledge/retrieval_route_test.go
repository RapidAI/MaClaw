package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type callCountEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (e *callCountEmbedder) Embed(text string) ([]float32, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	if strings.Contains(text, "SEMANTIC") || text == "\u0301" {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func (e *callCountEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i], _ = e.Embed(text)
	}
	return vectors, nil
}

func (e *callCountEmbedder) Dim() int { return 2 }
func (e *callCountEmbedder) Close()   {}

func (e *callCountEmbedder) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *callCountEmbedder) Reset() {
	e.mu.Lock()
	e.calls = 0
	e.mu.Unlock()
}

var _ embedding.Embedder = (*callCountEmbedder)(nil)

func TestKnowledgeRetrievalPlanKeepsAutoHeuristic(t *testing.T) {
	latinHit := []SearchResult{{ResultType: "card", Score: 3}}
	if plan := knowledgeRetrievalPlan(SearchOptions{Retrieval: knowledgeRetrievalAuto, Query: "AlphaToken"}, latinHit, true); plan.runEmbedding {
		t.Fatal("auto with a high non-node score should stay lexical")
	}
	low := []SearchResult{{ResultType: "card", Score: 1}}
	if plan := knowledgeRetrievalPlan(SearchOptions{Retrieval: knowledgeRetrievalAuto, Query: "AlphaToken"}, low, true); !plan.runEmbedding || !plan.fuse {
		t.Fatalf("auto below 2.0 = %#v", plan)
	}
	cjk := knowledgeRetrievalPlan(SearchOptions{Retrieval: knowledgeRetrievalAuto, Query: "园林"}, latinHit, true)
	if !cjk.runEmbedding || !cjk.fuse {
		t.Fatalf("auto CJK = %#v", cjk)
	}
	if plan := knowledgeRetrievalPlan(SearchOptions{Retrieval: knowledgeRetrievalFTS, Query: "园林"}, nil, true); plan.runEmbedding {
		t.Fatal("fts must not embed")
	}
	vector := knowledgeRetrievalPlan(SearchOptions{Retrieval: knowledgeRetrievalVector, Query: "AlphaToken"}, latinHit, true)
	if !vector.runEmbedding || vector.fuse {
		t.Fatalf("vector = %#v", vector)
	}
	hybrid := knowledgeRetrievalPlan(SearchOptions{Retrieval: knowledgeRetrievalHybrid, Query: "AlphaToken"}, latinHit, true)
	if !hybrid.runEmbedding || !hybrid.fuse {
		t.Fatalf("hybrid = %#v", hybrid)
	}
	if plan := knowledgeRetrievalPlan(SearchOptions{Retrieval: knowledgeRetrievalVector, Query: "中国人奇强"}, nil, true); plan.runEmbedding {
		t.Fatal("a strict name suppresses vector retrieval")
	}
	if _, err := canonicalKnowledgeRetrieval("nope"); err == nil {
		t.Fatal("expected unknown retrieval to fail")
	}
}

func TestSearchRetrievalRoutes(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	emb := &callCountEmbedder{}
	store.UseEmbedder(emb)
	if err := store.SaveSource(ctx, Source{ID: "route-source", Kind: SourceKindText, URI: "manual://route", Status: StatusParsed, Title: "notes"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "lexical-card", SourceID: "route-source", Title: "UniqueLexicalToken", Claim: "UniqueLexicalToken"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{
		ID:        "semantic-card",
		SourceID:  "route-source",
		Title:     "Semantic",
		Claim:     "SEMANTIC neighbor of the saved note.",
		Embedding: []float32{1, 0},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCard(ctx, Card{ID: "cjk-card", SourceID: "route-source", Title: "园林笔记", Claim: "这是一本关于园林的笔记"}); err != nil {
		t.Fatal(err)
	}
	// A larger corpus raises the IDF of the rare Latin token so its lexical
	// score clears the auto route's 2.0 cutoff.
	for i := 0; i < 24; i++ {
		if err := store.SaveCard(ctx, Card{
			ID:       fmt.Sprintf("filler-%02d", i),
			SourceID: "route-source",
			Title:    fmt.Sprintf("filler %d", i),
			Claim:    "unrelated garden notes about irrigation schedules and soil",
		}); err != nil {
			t.Fatal(err)
		}
	}

	emb.Reset()
	autoHits, err := store.Search(ctx, SearchOptions{Query: "UniqueLexicalToken", Retrieval: knowledgeRetrievalAuto, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHitsContainCard(autoHits, "lexical-card") {
		t.Fatalf("auto hits = %#v", autoHits)
	}
	if emb.Calls() != 0 {
		t.Fatalf("auto embedded the query %d times; hits=%#v", emb.Calls(), autoHits)
	}

	emb.Reset()
	ftsHits, err := store.Search(ctx, SearchOptions{Query: "园林", Retrieval: knowledgeRetrievalFTS, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !searchHitsContainCard(ftsHits, "cjk-card") {
		t.Fatalf("fts hits = %#v", ftsHits)
	}
	if emb.Calls() != 0 {
		t.Fatalf("fts embedded the query %d times", emb.Calls())
	}

	emb.Reset()
	hybridHits, err := store.Search(ctx, SearchOptions{Query: "UniqueLexicalToken", Retrieval: knowledgeRetrievalHybrid, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if emb.Calls() == 0 {
		t.Fatal("hybrid did not embed the query")
	}
	if !searchHitsContainCard(hybridHits, "lexical-card") {
		t.Fatalf("hybrid dropped the lexical card: %#v", hybridHits)
	}

	emb.Reset()
	vectorHits, err := store.Search(ctx, SearchOptions{Query: "UniqueLexicalToken", Retrieval: knowledgeRetrievalVector, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if emb.Calls() == 0 {
		t.Fatal("vector did not embed the query")
	}
	if searchHitsContainCard(vectorHits, "lexical-card") {
		t.Fatalf("vector mixed in an FTS-only card: %#v", vectorHits)
	}

	emb.Reset()
	markHits, err := store.Search(ctx, SearchOptions{Query: "\u0301", Retrieval: knowledgeRetrievalVector, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if emb.Calls() == 0 {
		t.Fatal("vector returned before embedding a query with an empty FTS expression")
	}
	if !searchHitsContainCard(markHits, "semantic-card") {
		t.Fatalf("vector hits = %#v", markHits)
	}

	emb.Reset()
	if _, err := store.Search(ctx, SearchOptions{Query: "中国人奇强", Retrieval: knowledgeRetrievalVector, Limit: 5}); err != nil {
		t.Fatal(err)
	}
	if emb.Calls() != 0 {
		t.Fatalf("strict name embedded %d times", emb.Calls())
	}
	if _, err := store.Search(ctx, SearchOptions{Query: "AlphaToken", Retrieval: "sideways"}); err == nil {
		t.Fatal("expected unknown retrieval to fail")
	}
}

func searchHitsContainCard(hits []SearchResult, cardID string) bool {
	for _, hit := range hits {
		if hit.CardID == cardID {
			return true
		}
	}
	return false
}
