package agent

import "testing"

func TestResolveAssistantFinishReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		finishReason string
		hasToolCalls bool
		want         string
	}{
		{name: "preserve stop", finishReason: "stop", want: "stop"},
		{name: "preserve tool_calls", finishReason: "tool_calls", hasToolCalls: true, want: "tool_calls"},
		{name: "preserve length", finishReason: "length", want: "length"},
		{name: "trim whitespace", finishReason: "  stop  ", want: "stop"},
		{name: "default stop", finishReason: "", want: "stop"},
		{name: "default tool_calls", finishReason: "", hasToolCalls: true, want: "tool_calls"},
		{name: "whitespace only with tools", finishReason: "   ", hasToolCalls: true, want: "tool_calls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveAssistantFinishReason(tc.finishReason, tc.hasToolCalls)
			if got != tc.want {
				t.Fatalf("ResolveAssistantFinishReason(%q, %v) = %q, want %q",
					tc.finishReason, tc.hasToolCalls, got, tc.want)
			}
		})
	}
}

func TestConversationEntryToMessageOmitsFinishReason(t *testing.T) {
	t.Parallel()
	entry := ConversationEntry{
		Role:         "assistant",
		Content:      "hello",
		FinishReason: "stop",
	}
	msg, ok := entry.ToMessage().(map[string]interface{})
	if !ok {
		t.Fatalf("ToMessage type = %T", entry.ToMessage())
	}
	if _, exists := msg["finish_reason"]; exists {
		t.Fatalf("ToMessage must not include finish_reason for the LLM API: %#v", msg)
	}
}
