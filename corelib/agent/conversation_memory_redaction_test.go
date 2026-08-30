package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentConversationMemoryRedactsSecretsBeforeDisk(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.Save("desktop-user", []ConversationEntry{
		{Role: "assistant", Content: "calling ssh", ToolCalls: []interface{}{map[string]interface{}{
			"id":   "call-1",
			"type": "function",
			"function": map[string]interface{}{
				"name":      "ssh",
				"arguments": `{"host":"api2.maclaw.top","password":"secret-password","api_key_secret":"json-secret"}`,
			},
		}}},
		{Role: "tool", Content: "JWT_SECRET=jwt-secret\nAuthorization: Bearer bearer-secret", ToolCallID: "call-1"},
	})
	cm.Stop()

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read persisted memory: %v", err)
	}
	serialized := string(data)
	for _, secret := range []string{"secret-password", "json-secret", "jwt-secret", "bearer-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted conversation leaked %q: %s", secret, serialized)
		}
	}
}

func TestPersistentConversationMemoryRedactsLegacySecretsOnLoad(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	legacy := `{"sessions":{"desktop-user":{"entries":[{"role":"assistant","content":"calling","tool_calls":[{"id":"call-1","type":"function","function":{"name":"ssh","arguments":"{\"password\":\"legacy-password\"}"}}]},{"role":"tool","content":"Cookie: legacy-cookie","tool_call_id":"call-1"}],"last_access":"2026-01-01T00:00:00Z"}}}`
	if err := os.WriteFile(storePath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy memory: %v", err)
	}

	cm := NewPersistentConversationMemory(storePath)
	cm.Stop()

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read rewritten memory: %v", err)
	}
	serialized := string(data)
	for _, secret := range []string{"legacy-cookie", "legacy-password"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("rewritten conversation leaked %q: %s", secret, serialized)
		}
	}
	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	entries := reloaded.Load("desktop-user")
	if len(entries) != 2 || entries[0].Role != "assistant" || entries[1].Role != "tool" || entries[1].ToolCallID != "call-1" {
		t.Fatalf("redaction should preserve valid tool-call group, got %#v", entries)
	}
}

func TestPersistentConversationMemoryStripsRolePrefixLeakBeforeDisk(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.Save("desktop-user", []ConversationEntry{
		{
			Role:    "assistant",
			Content: "safe answer\nBrowser: SECRET_BROWSER_CONTENT",
			// Mid-text "Tool:" lines are legitimate agent planning and must
			// survive persistence; only a leading prefix token is stripped.
			ReasoningContent: "thinking\nTool: bash df -h\n然后根据磁盘占用判断",
		},
		{
			Role:             "assistant",
			Content:          "ok",
			ReasoningContent: "Tool: 先检查状态\n再决定下一步",
		},
	})
	cm.Stop()

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read persisted memory: %v", err)
	}
	serialized := string(data)
	// Content cleaning keeps Case 2: a mid-text role prefix truncates the
	// hallucinated duplicate that follows it.
	for _, secret := range []string{"Browser:", "SECRET_BROWSER_CONTENT"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted conversation leaked role-prefixed content %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(serialized, "safe answer") {
		t.Fatalf("expected visible content prefix to be preserved: %s", serialized)
	}
	// Reasoning must not be truncated at mid-text "Tool:" planning lines.
	if !strings.Contains(serialized, "Tool: bash df -h") || !strings.Contains(serialized, "然后根据磁盘占用判断") {
		t.Fatalf("reasoning mid-text Tool: lines must be preserved: %s", serialized)
	}
	// A leading role-prefix token in reasoning is still stripped.
	if strings.Contains(serialized, "Tool: 先检查状态") {
		t.Fatalf("leading reasoning role prefix must be stripped: %s", serialized)
	}
	if !strings.Contains(serialized, "先检查状态") {
		t.Fatalf("reasoning body after a leading prefix must be preserved: %s", serialized)
	}
}

func TestPersistentConversationMemoryStripsPlainToolCallLeakBeforeDisk(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.Save("desktop-user", []ConversationEntry{
		{
			Role:    "assistant",
			Content: "先执行远程检查\nTOOL_CALL\n{\"function\":\"ssh_execute_command\",\"args\":{\"password\":\"<redacted>\",\"command\":\"df -h\"}}",
		},
	})
	cm.Stop()

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read persisted memory: %v", err)
	}
	serialized := string(data)
	for _, secret := range []string{"TOOL_CALL", "<redacted>", "ssh_execute_command"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted conversation leaked plain tool call %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(serialized, "先执行远程检查") {
		t.Fatalf("expected visible prefix to be preserved: %s", serialized)
	}
}

func TestStripPlainToolCallPersistenceLeakKeepsBareMentions(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bare word in reasoning kept", "I will issue a tool_call to bash and inspect output", "I will issue a tool_call to bash and inspect output"},
		{"bare word with period kept", "the tool_call failed. retrying now", "the tool_call failed. retrying now"},
		{"plural bare word kept", "check the tool_calls field later", "check the tool_calls field later"},
		{"xml tag truncated", "visible\n<tool_call>{\"name\":\"ssh\"}</tool_call>", "visible"},
		{"bare word followed by json truncated", "先执行远程检查\nTOOL_CALL\n{\"function\":\"ssh\"}", "先执行远程检查"},
		{"json key truncated", "result\n{\"tool_call\": {\"name\": \"ssh\"}}", "result\n{"},
		{"sentinel truncated", "answer\n<|tool_call_begin|>x", "answer"},
		{"no occurrence unchanged", "plain text", "plain text"},
		{"later markup after bare mention", "the tool_call word then <tool_call>x", "the tool_call word then"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripPlainToolCallPersistenceLeak(tc.input); got != tc.want {
				t.Fatalf("stripPlainToolCallPersistenceLeak(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
