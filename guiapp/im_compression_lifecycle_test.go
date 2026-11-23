package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestApplyPendingContextCompressionKeepsDroppedHistoryBehindHandle(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	h := &IMMessageHandler{}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "build the project"},
		{Role: "assistant", Content: "decision: preserve the parser interface"},
		{Role: "assistant", Content: "read parser.go"},
		{Role: "tool", ToolCallID: "read-1", Content: "package parser"},
		{Role: "assistant", Content: "run tests"},
		{Role: "tool", ToolCallID: "test-1", Content: "all tests passed"},
		{Role: "assistant", Content: "compress now", ToolCalls: []map[string]interface{}{{"id": "compress-1", "type": "function"}}},
		{Role: "tool", ToolCallID: "compress-1", Content: "compression queued"},
	}
	h.pendingContextCompression.Store("desktop-user", &contextCompressionRequest{
		Summary:       "Parser interface preserved; tests passed.",
		PreserveLastN: 4,
	})
	var lastSummary string
	compressed := h.applyPendingContextCompression("desktop-user", history, &lastSummary)
	if lastSummary == "" {
		t.Fatal("successful lossless compression did not record its summary")
	}
	var checkpoint string
	for _, entry := range compressed {
		text, _ := entry.Content.(string)
		if strings.Contains(text, toolresult.HandleFooterMarker) {
			checkpoint = text
			break
		}
	}
	if checkpoint == "" {
		t.Fatalf("compressed history has no lossless handle: %#v", compressed)
	}
	id := ""
	for _, line := range strings.Split(checkpoint, "\n") {
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
			break
		}
	}
	if id == "" {
		t.Fatalf("checkpoint has no handle id: %s", checkpoint)
	}
	res, err := toolresult.Read(toolresult.ReadOptions{
		ID:         id,
		SessionKey: "desktop-user",
		Query:      "preserve the parser interface",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "preserve the parser interface") || !strings.Contains(res.Content, "package parser") {
		t.Fatalf("dropped history is not recoverable: %s", res.Content)
	}
}

func TestApplyPendingContextCompressionAlsoShrinksLiveConversation(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	h := &IMMessageHandler{}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "build the project"},
		{Role: "assistant", Content: "old decision"},
		{Role: "assistant", Content: "read parser.go"},
		{Role: "tool", ToolCallID: "read-1", Content: "package parser"},
		{Role: "assistant", Content: "run tests"},
		{Role: "tool", ToolCallID: "test-1", Content: "all tests passed"},
		{Role: "assistant", Content: "compress now", ToolCalls: []map[string]interface{}{{"id": "compress-1", "type": "function"}}},
		{Role: "tool", ToolCallID: "compress-1", Content: "compression queued"},
	}
	conversation := []interface{}{map[string]string{"role": "system", "content": "policy"}}
	for _, entry := range history {
		conversation = append(conversation, entry.ToMessage())
	}
	h.pendingContextCompression.Store("desktop-user", &contextCompressionRequest{Summary: "Tests passed.", PreserveLastN: 4})
	_, live := h.applyPendingContextCompressionWithConversation("desktop-user", history, conversation, nil)
	if len(live) >= len(conversation) {
		t.Fatalf("live conversation did not shrink: %d -> %d", len(conversation), len(live))
	}
	foundHandle := false
	for _, msg := range live {
		_, content := agent.ExtractRoleContent(msg)
		foundHandle = foundHandle || strings.Contains(content, toolresult.HandleFooterMarker)
	}
	if !foundHandle {
		t.Fatalf("live summary lost the shared recovery handle: %#v", live)
	}
	_, lastContent := agent.ExtractRoleContent(live[len(live)-1])
	if lastContent != "compression queued" {
		t.Fatalf("recent tool result was not preserved: %q", lastContent)
	}
}

func TestToolCompressContextRejectsOversizedSummary(t *testing.T) {
	h := &IMMessageHandler{}
	result := h.toolCompressContext(map[string]interface{}{
		"summary": strings.Repeat("x", compactionSummaryRuneCap+1),
	})
	if !strings.Contains(result, "5000") {
		t.Fatalf("oversized summary was not rejected: %s", result)
	}
	if _, ok := h.pendingContextCompression.Load("desktop-user"); ok {
		t.Fatal("oversized summary was queued")
	}
}
