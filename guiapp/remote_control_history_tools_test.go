package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// A stored tool turn keeps the call name and outcome for the phone, while a
// chat turn never picks up tool_calls (it would drop real replies that happen
// to carry planning echo metadata).
func TestRemoteControlCodingAgentHistoryKeepsToolMetadata(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	userID := desktopUserID + ":" + project
	// Written through the real recorder so the test fails if the stored loop
	// ever stops carrying the tool metadata the phone rows need.
	recordCodingAgentResume(userID, project, []agent.ConversationEntry{
		{Role: "user", Content: "铺满图标"},
		{
			Role:             "assistant",
			Content:          "",
			ReasoningContent: "先看一眼图标文件",
			ToolCalls: []map[string]interface{}{{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "read_file",
					"arguments": `{"path":"E:/workprj/app/icon.dart"}`,
				},
			}},
		},
		{
			Role:        "tool",
			ToolCallID:  "call_1",
			Content:     "E:/workprj/app/icon.dart (12 lines)",
			ToolName:    "read_file",
			ToolOutcome: "failure",
		},
		{Role: "assistant", Content: "图标改好了"},
	}, nil)

	items := remoteControlCodingAgentItems(project)
	if len(items) < 3 {
		t.Fatalf("items = %d, want at least 3", len(items))
	}
	if items[1].ToolCalls == nil {
		t.Fatal("assistant tool turn lost its tool_calls")
	}
	if items[2].ToolName != "read_file" || items[2].ToolOutcome != "failure" {
		t.Fatalf("tool row = %+v, want read_file/failure", items[2])
	}
	for i, item := range items {
		if item.Role == "user" && item.ToolCalls != nil {
			t.Fatalf("item %d: user row carries tool_calls", i)
		}
	}

	// The wire payload the phone decodes keeps the fields.
	encoded, err := json.Marshal(items[1])
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	if _, ok := wire["tool_calls"]; !ok {
		t.Fatalf("wire payload = %v, want tool_calls", wire)
	}
}
