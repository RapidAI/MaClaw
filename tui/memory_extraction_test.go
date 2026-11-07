package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestTUIHistoryConversionUsesStableExtractionIDs(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "first", Timestamp: 123},
		{Role: "assistant", Content: "answer", Timestamp: 124},
	}
	first := convertTUIHistoryToMessages(history, 10)
	second := convertTUIHistoryToMessages(history, 10)
	for i := range first {
		if first[i].ID == "" || first[i].ID != second[i].ID {
			t.Fatalf("unstable extraction IDs: %q vs %q", first[i].ID, second[i].ID)
		}
	}
	withID := convertTUIHistoryToMessages([]agent.ConversationEntry{{ID: "entry-id", Role: "user", Content: "hello"}}, 10)
	if len(withID) != 1 || withID[0].ID != "entry-id" {
		t.Fatalf("existing entry ID not preserved: %+v", withID)
	}
}
