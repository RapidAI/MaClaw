package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestComputeRemovedEntriesIndexesToolCallsAndMultimodalText(t *testing.T) {
	call := llm.ToolCall{
		ID: "call-index",
		Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"D:/work/unique-index-target.go","content":"package main"}`,
		},
	}
	assistant := agent.ConversationEntry{Role: "assistant", ToolCalls: []llm.ToolCall{call}}
	got := computeRemovedEntries([]agent.ConversationEntry{assistant}, nil)
	if len(got) != 1 || !strings.Contains(got[0].Content, "unique-index-target.go") || !strings.Contains(got[0].Content, "write_file") {
		t.Fatalf("tool-only assistant entry was not indexed: %#v", got)
	}
	if kept := computeRemovedEntries([]agent.ConversationEntry{assistant}, []agent.ConversationEntry{assistant}); len(kept) != 0 {
		t.Fatalf("an unchanged tool call was indexed as removed: %#v", kept)
	}

	multimodal := agent.ConversationEntry{Role: "user", Content: []interface{}{
		map[string]interface{}{"type": "text", "text": "多模态消息里的唯一约束"},
		map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,BASE64_MUST_NOT_BE_INDEXED"}},
	}}
	got = computeRemovedEntries([]agent.ConversationEntry{multimodal}, nil)
	if len(got) != 1 || !strings.Contains(got[0].Content, "多模态消息里的唯一约束") {
		t.Fatalf("multimodal text was not indexed: %#v", got)
	}
	if strings.Contains(got[0].Content, "BASE64_MUST_NOT_BE_INDEXED") {
		t.Fatal("binary image payload leaked into the page index")
	}
}

func TestComputeRemovedEntriesDistinguishesSamePrefixAndRole(t *testing.T) {
	prefix := strings.Repeat("共同前缀", 30)
	removed := prefix + "被压缩条目的唯一结尾"
	kept := prefix + "仍保留条目的不同结尾"
	original := []agent.ConversationEntry{
		{Role: "assistant", Content: removed},
		{Role: "assistant", Content: kept},
	}
	got := computeRemovedEntries(original, original[1:])
	if len(got) != 1 || got[0].Content != removed || got[0].Category != memory.Category("conversation_assistant") {
		t.Fatalf("same-prefix removal was not indexed exactly: %#v", got)
	}

	got = computeRemovedEntries(
		[]agent.ConversationEntry{{Role: "user", Content: kept}},
		[]agent.ConversationEntry{{Role: "assistant", Content: kept}},
	)
	if len(got) != 1 || got[0].Category != memory.Category("conversation_user") {
		t.Fatalf("equal text in another role hid a removed entry: %#v", got)
	}
}
