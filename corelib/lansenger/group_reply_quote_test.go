package lansenger

import (
	"strings"
	"testing"
)

func TestFormatGroupReplyWithQuote(t *testing.T) {
	got := FormatGroupReplyWithQuote("staff-123", "今天天气怎么样？", "今天晴。")
	want := "staff-123问：今天天气怎么样？\n\n今天晴。"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatGroupReplyWithQuoteWithoutSender(t *testing.T) {
	got := FormatGroupReplyWithQuote("", "问题", "回答")
	want := "有人问：问题\n\n回答"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatGroupReplyWithQuoteMultiline(t *testing.T) {
	got := FormatGroupReplyWithQuote("u1", "第一行\n第二行", "答")
	want := "u1问：第一行\n第二行\n\n答"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatGroupReplyWithQuoteSkipsEmptyQuestion(t *testing.T) {
	if got := FormatGroupReplyWithQuote("staff-1", "  ", "回答"); got != "回答" {
		t.Fatalf("empty question should not quote, got %q", got)
	}
}

func TestFormatGroupReplyWithQuoteTruncates(t *testing.T) {
	long := strings.Repeat("问", MaxGroupReplyQuoteRunes+20)
	got := FormatGroupReplyWithQuote("A", long, "答")
	// Extract the quoted question after "A问：".
	const prefix = "A问："
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("missing prefix: %q", got)
	}
	rest := strings.TrimPrefix(got, prefix)
	quoted := strings.SplitN(rest, "\n\n", 2)[0]
	if strings.Count(quoted, "问") != MaxGroupReplyQuoteRunes-1 || !strings.HasSuffix(quoted, "…") {
		t.Fatalf("truncated quote = %q (runes=%d)", quoted, len([]rune(quoted)))
	}
}

func TestMaybeFormatGroupReplyWithQuote(t *testing.T) {
	if got := MaybeFormatGroupReplyWithQuote(false, "staff-1", "问", "答"); got != "答" {
		t.Fatalf("private reply must stay plain, got %q", got)
	}
	if got := MaybeFormatGroupReplyWithQuote(true, "staff-1", "问", "答"); !strings.HasPrefix(got, "staff-1问：") {
		t.Fatalf("group reply must be quoted with staff id, got %q", got)
	}
}

func TestFormatGroupReplyWithQuoteIsIdempotent(t *testing.T) {
	once := FormatGroupReplyWithQuote("staff-1", "问题", "回答")
	twice := FormatGroupReplyWithQuote("staff-2", "另一问", once)
	if twice != once {
		t.Fatalf("re-quoting should be a no-op:\n once=%q\ntwice=%q", once, twice)
	}
}
