package agent

import "testing"

func TestDeduplicateAdjacentAssistantEntries(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "check server"},
		{Role: "assistant", Content: "server is healthy"},
		{Role: "assistant", Content: " server is healthy\n"},
		{Role: "user", Content: "weather"},
		{Role: "assistant", Content: "sunny"},
	}

	got := DeduplicateAdjacentAssistantEntries(entries)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[2].Role != "user" || got[2].Content != "weather" {
		t.Fatalf("unexpected entry after dedup: %#v", got[2])
	}
}

func TestDeduplicateAdjacentAssistantEntriesKeepsToolCallMessages(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "assistant", Content: "calling tool", ToolCalls: []interface{}{map[string]interface{}{"id": "call-1"}}},
		{Role: "assistant", Content: "calling tool"},
	}

	got := DeduplicateAdjacentAssistantEntries(entries)
	if len(got) != 2 {
		t.Fatalf("tool-call assistant entries must be preserved, len = %d", len(got))
	}
}
