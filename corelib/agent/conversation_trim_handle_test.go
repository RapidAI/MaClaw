package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestTruncateToolContentPreservingHandleKeepsFooter(t *testing.T) {
	footer := toolresult.HandleFooterMarker + "\nid: 20260101T000000_read_document_0123456789abcdef\ntool: read_document\noriginal_bytes: 99999\npreview_bytes: 4096\nhint: 完整结果已落盘。需要细节时用 read_tool_result(id=..., offset, limit) 分段读取；勿要求模型复述全文。\n"
	body := strings.Repeat("文档正文。", 400) // 2000 runes, exceeds 400+200 window
	content := body + "\n\n" + footer

	got := TruncateToolContentPreservingHandle(content, 400, 200)
	if !strings.HasSuffix(got, footer) {
		t.Fatalf("handle footer was cut; truncated content ends with: %q", got[len(got)-80:])
	}
	if !strings.Contains(got, "id: 20260101T000000_read_document_0123456789abcdef") {
		t.Fatal("handle id line lost — spilled result would be orphaned")
	}
	if !strings.HasPrefix(got, body[:100]) {
		t.Fatal("head of the body was not preserved")
	}
}

func TestTruncateToolContentPreservingHandleWithoutFooter(t *testing.T) {
	content := strings.Repeat("x", 2000)
	got := TruncateToolContentPreservingHandle(content, 400, 200)
	if !strings.Contains(got, "…(截断)…") {
		t.Fatal("expected head/tail truncation separator")
	}
	if want := 400 + len([]rune("\n…(截断)…\n")) + 200; len([]rune(got)) != want {
		t.Fatalf("truncated length = %d runes, want %d", len([]rune(got)), want)
	}
}

func TestTruncateToolContentPreservingHandleShortContentUnchanged(t *testing.T) {
	content := strings.Repeat("y", 500)
	if got := TruncateToolContentPreservingHandle(content, 400, 200); got != content {
		t.Fatal("content within the window must be returned unchanged")
	}
}
