package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestEntityQueryRequiresRealNameSpans(t *testing.T) {
	query := buildFTSQuerySegmented("中国人奇强")
	if strings.Contains(query, " OR ") {
		t.Fatalf("name query fell back to fragment OR: %s", query)
	}
	if !strings.Contains(query, "奇强") && !strings.Contains(query, "中国人奇强") && !strings.Contains(query, "人奇强") {
		t.Fatalf("name query lost the distinctive span: %s", query)
	}
	if strings.Contains(query, `"奇"`) || strings.Contains(query, `"强"`) {
		t.Fatalf("single-character fragments leaked into query: %s", query)
	}
	question := buildFTSQuerySegmented("马勇出版过书籍吗")
	if strings.Contains(question, " AND ") {
		t.Fatalf("question query should keep recall OR: %s", question)
	}
	word := buildFTSQuerySegmented("学历")
	if strings.Contains(word, " OR ") || strings.Contains(word, `"学"`) {
		t.Fatalf("dictionary word should be one term, got %s", word)
	}
}

func TestSegmentedSnippetDoesNotJoinUnrelatedHan(t *testing.T) {
	anchors, strict := bm25.QueryAnchors("中国人奇强")
	if !strict || len(anchors) == 0 {
		t.Fatalf("expected strict anchors, got %v strict=%v", anchors, strict)
	}
	bio := SearchResult{Snippet: "中国 人 奇 安信 马勇"}
	if evidenceHasAnchors(bio, anchors) {
		t.Fatalf("segmented biography satisfied anchors %v", anchors)
	}
	hit := SearchResult{Snippet: "中国 人 奇 强 只是测试称呼"}
	if !evidenceHasAnchors(hit, anchors) {
		t.Fatalf("segmented true mention missed anchors %v", anchors)
	}
	broken := SearchResult{Snippet: "国籍为中国人\n单位奇\n强相关说明"}
	if evidenceHasAnchors(broken, anchors) {
		t.Fatal("a line break joined unrelated Han into the queried name")
	}
}

func TestAnchorDoesNotFormAcrossEvidenceFields(t *testing.T) {
	anchors, strict := bm25.QueryAnchors("中国人奇强")
	if !strict {
		t.Fatal("expected a strict name query")
	}
	split := SearchResult{Claim: "国籍为中国人，单位奇", Summary: "强相关的其他说明"}
	if evidenceHasAnchors(split, anchors) {
		t.Fatalf("anchor formed across fields: %v", anchors)
	}
	card := SearchResult{ResultType: "card", Claim: "中国人奇强只是测试称呼"}
	node := SearchResult{ResultType: "node", Snippet: "中国人奇强只是测试称呼"}
	if strictAnchorsAlreadyCovered("中国人奇强", []SearchResult{card}) {
		t.Fatal("a card hit must not skip the original-node scan")
	}
	if !strictAnchorsAlreadyCovered("中国人奇强", []SearchResult{node}) {
		t.Fatal("a covering node should skip a redundant LIKE scan")
	}
}

func TestSearchDoesNotBindSharedHanFragments(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title: "公开身份调研",
		Text:  "高置信身份锚点。中文名马勇，英文名 Yong Ma。国籍为中国人，工作单位奇安信科技集团，教育机构北京理工大学。",
	}); err != nil {
		t.Fatalf("save biography: %v", err)
	}
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title: "测试称呼",
		Text:  "中国人奇强只是一个测试称呼，不是上面那份调研里的人。",
	}); err != nil {
		t.Fatalf("save mention: %v", err)
	}

	results, err := store.Search(ctx, SearchOptions{Query: "中国人奇强", Limit: 8})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	foundMention := false
	for _, result := range results {
		text := result.Claim + "\n" + result.Summary + "\n" + result.Snippet + "\n" + result.CardTitle + "\n" + result.NodeTitle
		if strings.Contains(text, "马勇") && !strings.Contains(text, "中国人奇强") {
			t.Fatalf("fragment overlap returned the other person: %#v", result)
		}
		if strings.Contains(text, "测试称呼") || strings.Contains(text, "中国人奇强") {
			foundMention = true
		}
	}
	if !foundMention {
		t.Fatalf("expected the note that contains the queried name, got %#v", results)
	}

	bio, err := store.Search(ctx, SearchOptions{Query: "马勇", Limit: 8})
	if err != nil {
		t.Fatalf("Search 马勇: %v", err)
	}
	if len(bio) == 0 {
		t.Fatal("expected the biography to remain retrievable by its real name")
	}
}

type countingEmbedder struct{ calls int }

func (e *countingEmbedder) Embed(string) ([]float32, error) {
	e.calls++
	return []float32{1, 0}, nil
}

func (e *countingEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vector, err := e.Embed(texts[i])
		if err != nil {
			return nil, err
		}
		vectors[i] = vector
	}
	return vectors, nil
}

func (e *countingEmbedder) Dim() int        { return 2 }
func (e *countingEmbedder) Close()          {}
func (e *countingEmbedder) ModelID() string { return "counting" }

var _ embedding.Embedder = (*countingEmbedder)(nil)

func TestStrictNameSearchSkipsEmbedding(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	emb := &countingEmbedder{}
	store.SetEmbedder(emb)
	store.WaitBackground()
	emb.calls = 0

	ctx := context.Background()
	if _, err := store.Search(ctx, SearchOptions{Query: "中国人奇强", Limit: 5}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := store.SearchStructured(ctx, StructuredSearchOptions{Query: "中国人奇强", Limit: 5}); err != nil {
		t.Fatalf("SearchStructured: %v", err)
	}
	if _, err := store.SearchImages(ctx, ImageSearchOptions{SearchOptions: SearchOptions{Query: "中国人奇强", Limit: 5}}); err != nil {
		t.Fatalf("SearchImages: %v", err)
	}
	if emb.calls != 0 {
		t.Fatalf("strict name search embedded %d times", emb.calls)
	}

	if _, err := store.Search(ctx, SearchOptions{Query: "学历", Limit: 5}); err != nil {
		t.Fatalf("Search dictionary word: %v", err)
	}
	if emb.calls == 0 {
		t.Fatal("a single dictionary word should still use semantic recall")
	}
}
