package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSegmentTextForFTSAddsNoSpaceScriptNGrams(t *testing.T) {
	indexed := segmentTextForFTS("日本語検索")
	if !strings.Contains(indexed, "日本") || !strings.Contains(indexed, "本語") {
		t.Fatalf("indexed text missing Japanese ngrams: %q", indexed)
	}
	query := buildFTSQuerySegmented("한국어검색")
	if !strings.Contains(query, "한국") || !strings.Contains(query, "검색") {
		t.Fatalf("query missing Korean ngrams: %q", query)
	}
}

func TestNormalizeKnowledgeTextCanonicalizesCompatibilityForms(t *testing.T) {
	got := normalizeKnowledgeText("ＡＢＣ\r\nCafe\u0301\u200b")
	if got != "ABC\nCafé" {
		t.Fatalf("normalized = %q", got)
	}
}

func TestDetectKnowledgeLanguageRecognizesJapaneseMixedScript(t *testing.T) {
	info := detectKnowledgeLanguage("日本語の検索手順を確認します")
	if info.language != "ja" || info.script != "Jpan" {
		t.Fatalf("language detection = %#v, want Japanese", info)
	}
}

func TestNormalizeKnowledgeLexicalTextFoldsArabicVariants(t *testing.T) {
	got := normalizeKnowledgeLexicalText("إِدارةُ الهُوية")
	if got != "اداره الهويه" {
		t.Fatalf("Arabic lexical normalization = %q", got)
	}
}

func TestSQLiteStoreSearchesKoreanOriginalNodeWithNoSpaceFallback(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	_, err = store.SaveText(ctx, TextSaveRequest{
		Title: "한국어 안내",
		Text:  "비밀번호 재설정 절차는 보안 메뉴에서 시작합니다.",
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	results, err := store.Search(ctx, SearchOptions{Query: "비밀번호 재설정", ResultTypes: []string{"node"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected Korean query to retrieve original node")
	}
}
