package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestSubAgentCompactionIncludesReadFilesAndKeepsEarlierModified(t *testing.T) {
	compactor := NewSubAgentCompactor(8000,
		func() []string { return []string{"new.go"} },
		func() []string { return nil },
		func() []string { return []string{"fresh_read.go"} },
		nil,
	)
	task := "实现功能\n\n## 已读取文件\n- earlier_read.go\n\n## 已修改文件\n- earlier.go\n"
	summary := compactor.buildCompactionSummary(nil, task)
	for _, want := range []string{"earlier_read.go", "fresh_read.go", "earlier.go", "new.go"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %s:\n%s", want, summary)
		}
	}
}

func TestSubAgentCompactionSplitsOversizedTurn(t *testing.T) {
	compactor := NewSubAgentCompactor(8000, nil, nil, nil, nil)
	prefix := strings.Repeat("output ", 8000)
	suffix := strings.Repeat("kept ", 200)
	conversation := []interface{}{
		map[string]interface{}{"role": "system", "content": "sys"},
		map[string]interface{}{"role": "user", "content": "do the task"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "working",
			"tool_calls": []llm.ToolCall{
				{ID: "1", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"one"}`}},
				{ID: "2", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"two"}`}},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "1", "content": prefix},
		map[string]interface{}{"role": "tool", "tool_call_id": "2", "content": suffix},
	}
	if !compactor.ShouldCompact(conversation) {
		t.Fatal("expected oversized turn to trigger compaction")
	}
	compacted := compactor.Compact(conversation)
	if len(compacted) >= len(conversation) {
		t.Fatalf("expected a shorter conversation, got %d -> %d", len(conversation), len(compacted))
	}
	foundKept := false
	for _, msg := range compacted {
		m, ok := msg.(map[string]interface{})
		if !ok || m["role"] != "tool" {
			continue
		}
		if m["tool_call_id"] == "2" {
			foundKept = true
		}
		if m["tool_call_id"] == "1" {
			t.Fatal("prefix tool result should have been summarized, not kept")
		}
	}
	if !foundKept {
		t.Fatal("trailing tool result should stay in the kept suffix")
	}
}
