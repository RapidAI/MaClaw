package mcp

import (
	"errors"
	"strings"
	"testing"
)

func TestToolCallContentProjectsTextAndRejectsToolErrors(t *testing.T) {
	envelope := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"张学友资料"},{"type":"image","data":"abc"},{"type":"text","text":"第二段"}]}}`
	got, err := ToolCallContent(envelope)
	if err != nil || got != "张学友资料\n第二段" {
		t.Fatalf("envelope content=%q err=%v", got, err)
	}
	if strings.Contains(got, "jsonrpc") || strings.Contains(got, "web-search-prime") {
		t.Fatalf("protocol identity leaked: %q", got)
	}
	bare, err := ToolCallContent(`{"content":[{"type":"text","text":"本地正文"}]}`)
	if err != nil || bare != "本地正文" {
		t.Fatalf("bare content=%q err=%v", bare, err)
	}
	plain, err := ToolCallContent(`{"content":"纯文本"}`)
	if err != nil || plain != "纯文本" {
		t.Fatalf("string content=%q err=%v", plain, err)
	}
	quoted, err := ToolCallContent(`{"result":"直接文本"}`)
	if err != nil || quoted != "直接文本" {
		t.Fatalf("result string=%q err=%v", quoted, err)
	}
	imageOnly, err := ToolCallContent(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"image","data":"abc"}]}}`)
	if err != nil || strings.Contains(imageOnly, "jsonrpc") || !strings.Contains(imageOnly, `"type":"image"`) {
		t.Fatalf("image fallback=%q err=%v", imageOnly, err)
	}
	raw, err := ToolCallContent("not json")
	if err != nil || raw != "not json" {
		t.Fatalf("plain=%q err=%v", raw, err)
	}

	_, err = ToolCallContent(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"quota exceeded"}]}}`)
	var toolErr *ToolCallError
	if !errors.As(err, &toolErr) || toolErr.Error() != "quota exceeded" {
		t.Fatalf("tool error=%v", err)
	}
	_, err = ToolCallContent(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"session expired"}}`)
	if !errors.As(err, &toolErr) || toolErr.Error() != "session expired" {
		t.Fatalf("rpc error=%v", err)
	}
	_, err = ToolCallContent(`{"isError":true,"content":[]}`)
	if !errors.As(err, &toolErr) || toolErr.Error() != "unknown tool error" {
		t.Fatalf("empty tool error=%v", err)
	}
}
