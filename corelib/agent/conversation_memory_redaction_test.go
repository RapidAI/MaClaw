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
			Role:             "assistant",
			Content:          "safe answer\nBrowser: SECRET_BROWSER_CONTENT",
			ReasoningContent: "thinking\nTool: SECRET_REASONING_CONTENT",
		},
	})
	cm.Stop()

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read persisted memory: %v", err)
	}
	serialized := string(data)
	for _, secret := range []string{"Browser:", "Tool:", "SECRET_BROWSER_CONTENT", "SECRET_REASONING_CONTENT"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted conversation leaked role-prefixed content %q: %s", secret, serialized)
		}
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
