package lansenger

import (
	"strings"
	"testing"
)

func TestGroupReplyDisplayNameAndSenderLabel(t *testing.T) {
	if got := GroupReplyDisplayName(IncomingMessage{FromUserID: "staff-1", SenderName: "张三"}); got != "张三" {
		t.Fatalf("display name, got %q", got)
	}
	if got := GroupReplyDisplayName(IncomingMessage{FromUserID: "staff-1"}); got != "" {
		t.Fatalf("no name => empty display, got %q", got)
	}
	// Platform sometimes echoes staffId into senderName.
	if got := GroupReplyDisplayName(IncomingMessage{FromUserID: "staff-1", SenderName: "staff-1"}); got != "" {
		t.Fatalf("echoed staffId is not a display name, got %q", got)
	}
	if got := GroupReplySenderLabel(IncomingMessage{FromUserID: "staff-1", SenderName: "staff-1"}); got != "staff-1" {
		t.Fatalf("label falls back to staffId, got %q", got)
	}
	if got := GroupReplySenderLabel(IncomingMessage{FromUserID: "staff-1", SenderName: "张三"}); got != "张三" {
		t.Fatalf("prefer display name, got %q", got)
	}
	if got := GroupReplySenderLabel(IncomingMessage{FromUserID: "staff-1"}); got != "staff-1" {
		t.Fatalf("fallback to staffId, got %q", got)
	}
	if got := GroupReplySenderLabel(IncomingMessage{SenderName: "  Alice   Bob \n junk"}); got != "Alice Bob" {
		t.Fatalf("collapse whitespace + first line only, got %q", got)
	}
	if got := GroupReplySenderLabel(IncomingMessage{SenderName: "张\u200b三"}); got != "张三" {
		t.Fatalf("strip zero-width chars, got %q", got)
	}
	// Labels must not retain "问：" or the header becomes ambiguous.
	if got := GroupReplySenderLabel(IncomingMessage{SenderName: "顾问问：甲"}); got != "顾问甲" {
		t.Fatalf("strip 问： from label, got %q", got)
	}
	if got := GroupReplySenderLabel(IncomingMessage{}); got != "" {
		t.Fatalf("empty, got %q", got)
	}
	long := strings.Repeat("名", MaxGroupReplySenderRunes+8)
	got := GroupReplySenderLabel(IncomingMessage{SenderName: long})
	if utf8Count := len([]rune(got)); utf8Count != MaxGroupReplySenderRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("name should truncate to %d runes, got %q (runes=%d)", MaxGroupReplySenderRunes, got, utf8Count)
	}
}

func TestFormatGroupReplyWithQuote(t *testing.T) {
	got := FormatGroupReplyWithQuote("张三", "今天天气怎么样？", "今天晴。")
	want := "张三问：今天天气怎么样？\n\n今天晴。"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatGroupReplyWithQuoteFromMessage(t *testing.T) {
	msg := IncomingMessage{FromUserID: "staff-abc", SenderName: "张三"}
	got := FormatGroupReplyWithQuoteFromMessage(msg, "今天天气怎么样？", "今天晴。")
	want := "张三问：今天天气怎么样？\n\n今天晴。"
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
	got := FormatGroupReplyWithQuote("王五", "第一行\n第二行", "答")
	want := "王五问：第一行\n第二行\n\n答"
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
	if got := MaybeFormatGroupReplyWithQuote(false, "张三", "问", "答"); got != "答" {
		t.Fatalf("private reply must stay plain, got %q", got)
	}
	if got := MaybeFormatGroupReplyWithQuote(true, "张三", "问", "答"); !strings.HasPrefix(got, "张三问：") {
		t.Fatalf("group reply must be quoted with display name, got %q", got)
	}
	msg := IncomingMessage{FromUserID: "staff-1", SenderName: "李四"}
	if got := MaybeFormatGroupReplyWithQuoteFromMessage(true, msg, "问", "答"); !strings.HasPrefix(got, "李四问：") {
		t.Fatalf("FromMessage must prefer display name, got %q", got)
	}
}

func TestFormatGroupReplyWithQuoteIsIdempotent(t *testing.T) {
	once := FormatGroupReplyWithQuote("张三", "问题", "回答")
	twice := FormatGroupReplyWithQuote("李四", "另一问", once)
	if twice != once {
		t.Fatalf("re-quoting should be a no-op:\n once=%q\ntwice=%q", once, twice)
	}
	// Display names with spaces must still be treated as already-quoted.
	spaced := FormatGroupReplyWithQuote("Alice Bob", "问题", "回答")
	if again := FormatGroupReplyWithQuote("Carol", "另一问", spaced); again != spaced {
		t.Fatalf("spaced name re-quote should be a no-op:\n once=%q\ntwice=%q", spaced, again)
	}
}

func TestAlreadyHasGroupReplyQuoteRejectsCommonFalsePositives(t *testing.T) {
	// Single-line "请问：" must not look like our header (no blank line).
	if alreadyHasGroupReplyQuote("请问：今天天气怎么样？") {
		t.Fatal("single-line 请问 must not match")
	}
	// Multi-line answers starting with 请问 should not suppress real quoting.
	if alreadyHasGroupReplyQuote("请问：今天天气怎么样？\n\n补充说明") {
		t.Fatal("请问 multi-line free text must not match")
	}
	// Long prose containing 问： should not match (label too long).
	longLabel := strings.Repeat("啊", MaxGroupReplySenderRunes+1) + "问：内容\n\n答案"
	if alreadyHasGroupReplyQuote(longLabel) {
		t.Fatal("overlong label must not match")
	}
	// Header block itself over size bound (long question before blank line).
	// max header runes = MaxGroupReplySenderRunes + 2 + MaxGroupReplyQuoteRunes + 16
	// Use a label at the cap plus an oversize question body.
	hugeQ := strings.Repeat("问", maxGroupReplyHeaderRunes)
	hugeHeader := "A问：" + hugeQ + "\n\n答案"
	if alreadyHasGroupReplyQuote(hugeHeader) {
		t.Fatal("overlong header block must not match")
	}
	// Rare real name ending with 请 must still be treated as our header when formatted.
	named := FormatGroupReplyWithQuote("赵请", "问题", "回答")
	if !alreadyHasGroupReplyQuote(named) {
		t.Fatalf("name ending with 请 must still match: %q", named)
	}
	// Our real header shape must still match.
	real := FormatGroupReplyWithQuote("张三", "问题", "回答")
	if !alreadyHasGroupReplyQuote(real) {
		t.Fatalf("real header must match: %q", real)
	}
	// Spaced display names remain idempotent.
	spaced := FormatGroupReplyWithQuote("Alice Bob", "问题", "回答")
	if !alreadyHasGroupReplyQuote(spaced) {
		t.Fatalf("spaced name header must match: %q", spaced)
	}
}
