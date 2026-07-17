package meetingminutes

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSplitPlainTextSinglePass(t *testing.T) {
	text := "开场寒暄。\n\n讨论项目进度。"
	chunks := SplitPlainText(text, 4000, 3500, 10)
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d want 1", len(chunks))
	}
	if chunks[0].Text != text {
		t.Fatalf("text mismatch")
	}
}

func TestSplitPlainTextMapReduce(t *testing.T) {
	// Many short paragraphs → multiple chunks under a tight budget.
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("段落")
		b.WriteString(strings.Repeat("内容讨论决议待办", 20))
		b.WriteString("\n\n")
	}
	text := b.String()
	chunks := SplitPlainText(text, 200, 100, 8)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d (tokens=%d)", len(chunks), corelib.EstimateTextTokens(text))
	}
	if len(chunks) > 8 {
		t.Fatalf("exceeded max chunks: %d", len(chunks))
	}
	var joined int
	for i, ch := range chunks {
		if ch.Index != i {
			t.Fatalf("index %d != %d", ch.Index, i)
		}
		if strings.TrimSpace(ch.Text) == "" {
			t.Fatalf("empty chunk %d", i)
		}
		joined += len([]rune(ch.Text))
	}
	if joined < len([]rune(text))/2 {
		t.Fatalf("too much content dropped: joined_runes=%d src=%d", joined, len([]rune(text)))
	}
}

func TestExtractiveDraftContainsStructure(t *testing.T) {
	body := strings.Repeat("会议讨论内容。", 200)
	draft := ExtractiveDraft("周会", "项目同步", body)
	for _, needle := range []string{"# 周会", "摘要", "决议", "待办", "transcript_file"} {
		if !strings.Contains(draft, needle) {
			t.Fatalf("missing %q in draft:\n%s", needle, draft[:min(300, len(draft))])
		}
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	text := strings.Repeat("abcdefghij", 500)
	got := TruncateToTokenBudget(text, 50)
	if corelib.EstimateTextTokens(got) > 60 { // small slack
		t.Fatalf("still too large: tokens=%d", corelib.EstimateTextTokens(got))
	}
	if !strings.HasSuffix(text, got[len(got)/2:]) && !strings.Contains(text, got) {
		// Tail of original should contain the truncated result as a suffix-ish slice.
		if !strings.HasSuffix(text, strings.TrimSpace(got)) {
			// Accept that we keep tail: last chars of original appear in got.
			srcTail := text[len(text)-20:]
			if !strings.Contains(got, srcTail) {
				t.Fatalf("expected tail preservation")
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
