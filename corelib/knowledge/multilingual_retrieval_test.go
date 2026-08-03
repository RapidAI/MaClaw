package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

type multilingualRouteEmbedder struct {
	calls int
}

func (e *multilingualRouteEmbedder) Embed(text string) ([]float32, error) {
	e.calls++
	if containsNoSpaceScriptRunes(text) {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func (e *multilingualRouteEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i], _ = e.Embed(text)
	}
	return vectors, nil
}

func (*multilingualRouteEmbedder) Dim() int { return 2 }
func (*multilingualRouteEmbedder) Close()   {}

func TestSearchRunsEmbeddingRouteForNoSpaceScriptEvenWithLexicalHit(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	emb := &multilingualRouteEmbedder{}
	store.SetEmbedder(emb)
	_, err = store.SaveText(ctx, TextSaveRequest{
		Title: "한국어 안내",
		Text:  "비밀번호 재설정 절차는 보안 메뉴에서 시작합니다.",
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	// Ignore calls made for card/node insertion. A Korean lexical match must
	// still invoke query embedding so hybrid retrieval remains available.
	emb.calls = 0
	results, err := store.Search(ctx, SearchOptions{Query: "비밀번호 재설정", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected lexical result")
	}
	if emb.calls == 0 {
		t.Fatal("expected semantic route for Korean query despite lexical hit")
	}
}
