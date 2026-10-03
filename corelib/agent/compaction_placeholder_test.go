package agent

import (
	"strings"
	"testing"
)

func TestTrimHistoryKeepsDroppedUserRequestAndFilePath(t *testing.T) {
	var entries []ConversationEntry
	entries = append(entries,
		ConversationEntry{Role: "user", Content: "请修改 login.go 的会话过期时间"},
		ConversationEntry{Role: "assistant", Content: "先读文件", ToolCalls: []map[string]interface{}{
			{"id": "r1", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"login.go"}`}},
		}},
		ConversationEntry{Role: "tool", Content: "package login", ToolCallID: "r1"},
	)
	for i := 0; i < MaxConversationTurns; i++ {
		entries = append(entries, ConversationEntry{Role: "assistant", Content: "step"})
	}
	entries = append(entries, ConversationEntry{Role: "user", Content: "KEEP_TAIL"})

	trimmed := TrimHistory(entries)
	if len(trimmed) == 0 || trimmed[0].Role != "user" {
		t.Fatalf("expected a leading handoff, got %#v", trimmed)
	}
	text, _ := trimmed[0].Content.(string)
	if !strings.Contains(text, "会话过期时间") || !strings.Contains(text, "login.go") {
		t.Fatalf("handoff missing dropped request or path: %s", text)
	}
	foundTail := false
	for i, entry := range trimmed {
		body, _ := entry.Content.(string)
		if strings.Contains(body, "KEEP_TAIL") {
			foundTail = true
		}
		if entry.Role == "tool" && (i == 0 || (trimmed[i-1].Role != "assistant" && trimmed[i-1].Role != "tool")) {
			t.Fatalf("orphaned tool at %d", i)
		}
	}
	if !foundTail {
		t.Fatal("recent request was dropped")
	}
}

func TestReplaceCompactionPlaceholderSurvivesLaterSave(t *testing.T) {
	cm := NewConversationMemory()
	userID := "user-1"
	placeholder := []ConversationEntry{
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "working"},
		{
			Role:             "system",
			Content:          "[...中间的工具调用和执行细节已省略...]",
			CompactionID:     "cmp-1",
			CompactionSource: "<dropped>\nold work\n</dropped>",
		},
	}
	cm.Save(userID, placeholder)
	if !cm.ReplaceCompactionPlaceholder(userID, "cmp-1", "交接摘要：登录已修好") {
		t.Fatal("expected placeholder replacement")
	}
	stale := append([]ConversationEntry{}, placeholder...)
	stale = append(stale, ConversationEntry{Role: "user", Content: "继续"})
	cm.Save(userID, stale)

	loaded := cm.Load(userID)
	var found string
	for _, entry := range loaded {
		text, _ := entry.Content.(string)
		if strings.Contains(text, "登录已修好") {
			found = text
			if entry.CompactionSource != "" {
				t.Fatal("finished checkpoint should not keep summarizer source")
			}
		}
	}
	if found == "" {
		t.Fatalf("later save dropped the async summary: %+v", loaded)
	}
	if !cm.ReplaceCompactionPlaceholder(userID, "cmp-1", "should not overwrite") {
		return
	}
	t.Fatal("a finished checkpoint must not be replaced again")
}
