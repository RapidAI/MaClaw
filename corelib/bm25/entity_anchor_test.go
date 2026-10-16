package bm25

import (
	"strings"
	"testing"
)

func TestContentAnchorsRejectSharedNameFragments(t *testing.T) {
	if !ShortEntityMention("中国人奇强") {
		t.Fatal("expected a short Han name")
	}
	if ShortEntityMention("马勇出版过书籍吗") {
		t.Fatal("a question must stay on the recall path")
	}
	anchors := ContentAnchors("中国人奇强")
	if len(anchors) == 0 {
		t.Fatal("expected content anchors")
	}
	trueText := "中国人奇强只是一个测试称呼"
	falseText := "国籍为中国人，工作单位是奇安信"
	if anchorsSatisfied(falseText, anchors) {
		t.Fatalf("fragment text satisfied anchors %v", anchors)
	}
	if !anchorsSatisfied(trueText, anchors) {
		t.Fatalf("true mention missed anchors %v", anchors)
	}
}

func TestQueryAnchorsLeaveSingleWordsOpen(t *testing.T) {
	if _, strict := QueryAnchors("中国人奇强"); !strict {
		t.Fatal("composite or unknown name should require its own spans")
	}
	if _, strict := QueryAnchors("学历"); strict {
		anchors, _ := QueryAnchors("学历")
		t.Fatalf("single dictionary word should stay open for semantic recall, anchors=%v", anchors)
	}
	if ShortEntityMention("马勇是谁") || ShortEntityMention("马勇有几本书") {
		t.Fatal("who/count questions stay on the recall path")
	}
	if !ShortEntityMention("几何") {
		t.Fatal("几何 contains 几 but is a noun, not a question")
	}
}

func TestContentAnchorsCoverDictionaryPhrase(t *testing.T) {
	anchors := ContentAnchors("证据导航")
	if !anchorsSatisfied("证据导航面板可以打开最近产物", anchors) {
		t.Fatalf("indexed phrase missed anchors %v", anchors)
	}
}

func anchorsSatisfied(text string, anchors []string) bool {
	for _, anchor := range anchors {
		if !strings.Contains(text, anchor) {
			return false
		}
	}
	return len(anchors) > 0
}
