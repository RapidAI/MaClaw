package browser

import (
	"strings"
	"testing"
)

func TestParagraphize(t *testing.T) {
	in := "## 标题\n第一段。\n第二段。\n- a\n- b\n```\nx\ny\n```\n| a | b |\n| - | - |\n结尾"
	got := Paragraphize(in)
	for _, want := range []string{"## 标题\n\n第一段。\n\n第二段。\n\n- a\n- b\n\n```\nx\ny\n```\n\n| a | b |\n| - | - |\n\n结尾"} {
		if got != want {
			t.Fatalf("got %q\nwant %q", got, want)
		}
	}
	if Paragraphize("a\n\nb") != "a\n\nb" {
		t.Fatal("blank lines must stay single")
	}
}

func TestParagraphProblem(t *testing.T) {
	wall := strings.Repeat("这是一句很长的话。", 60)
	if ParagraphProblem(wall) == "" {
		t.Fatal("a 540-char single block must be refused")
	}
	if ParagraphProblem(wall+"\n\n"+wall) != "" || ParagraphProblem("短文") != "" {
		t.Fatal("paragraphed or short bodies pass")
	}
}

func TestRichPlainKeepsBlankLines(t *testing.T) {
	d := parseRichBody("## 一\n第一段 **重点**。\n第二段。", 0)
	if !strings.Contains(d.plain, "第一段 重点。\n\n第二段。") {
		t.Fatalf("plain=%q", d.plain)
	}
}
