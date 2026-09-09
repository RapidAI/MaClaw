package agentruntime

import "testing"

func TestSkillDocPhraseOccursRequiresBoundary(t *testing.T) {
	if !SkillDocPhraseOccurs("使用book-pdf skill", "book-pdf") {
		t.Fatal("hyphenated alias must match")
	}
	if !SkillDocPhraseOccurs("book pdf 已经安装", "book-pdf") {
		t.Fatal("spaced alias must match")
	}
	if !SkillDocPhraseOccurs("使用book\uff0dpdf skill", "book-pdf") {
		t.Fatal("fullwidth hyphen must count as a separator")
	}
	if SkillDocPhraseOccurs("facebook pdf 怎么导出", "book-pdf") {
		t.Fatal("embedded 'book pdf' inside facebook must not match")
	}
	if SkillDocPhraseOccurs("handbook pdf 在哪", "book-pdf") {
		t.Fatal("embedded 'book pdf' inside handbook must not match")
	}
	if SkillDocPhraseOccurs("bookpdf 已经安装", "book-pdf") {
		t.Fatal("concatenated bookpdf must not match book-pdf")
	}
	if SkillDocPhraseOccurs("handbook-pdf 在哪", "book-pdf") {
		t.Fatal("hyphenated handbook-pdf must not substring-match book-pdf")
	}
	if !SkillDocPhraseOccurs("使用 book_pdf 编写", "book-pdf") {
		t.Fatal("underscore must count as a separator")
	}
	if !SkillDocPhraseOccurs("使用book―pdf skill", "book-pdf") {
		t.Fatal("horizontal bar must count as a separator")
	}
	if SkillDocPhraseOccurs(`将 C:\Users\me\output\book-pdf-v2.2.3.pdf 复制到当前工作区`, "book-pdf") {
		t.Fatal("a path basename must not count as naming the skill")
	}
	if SkillDocPhraseOccurs("see /tmp/book-pdf/output.pdf", "book-pdf") {
		t.Fatal("a posix path must not count as naming the skill")
	}
	if SkillDocPhraseOccurs("copy book-pdf-v2.2.3.pdf into the workspace", "book-pdf") {
		t.Fatal("a versioned filename must not count as naming the skill")
	}
	if SkillDocPhraseOccurs("请打开 复制品.pdf", "复制") {
		t.Fatal("a CJK compound filename must not count as naming a shorter skill")
	}
	if !SkillDocPhraseOccurs("使用 book-pdf 写一章", "book-pdf") {
		t.Fatal("naming the skill in prose must still match")
	}
}

func TestSkillDocMatchScorePrefersTriggers(t *testing.T) {
	if got := SkillDocMatchScore("handbook-pdf", []string{"handbook"}, "使用 handbook 处理"); got < 1 {
		t.Fatal("trigger hit must score")
	}
	if got := SkillDocMatchScore("book-pdf", nil, "handbook-pdf 在哪"); got != 0 {
		t.Fatal("substring of a longer name must not score")
	}
}

func TestCountTriggerMatchesSkipsEmpty(t *testing.T) {
	if got := CountTriggerMatches([]string{"", "git"}, "github 怎么用"); got != 0 {
		t.Fatal("substring inside github must not count as trigger git")
	}
	if got := CountTriggerMatches([]string{"git"}, "git 提交代码"); got != 1 {
		t.Fatal("bounded trigger must count")
	}
}
