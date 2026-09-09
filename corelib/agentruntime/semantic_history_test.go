package agentruntime

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestRewriteExpiredSemanticGrantNamesOnlyRewritesPlaceholder(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "invoke_stale"}}}},
		{Role: "tool", ToolName: PreviousTurnSemanticToolName, Content: map[string]interface{}{"name": PreviousTurnSemanticToolName}},
		{Role: "assistant", ToolCalls: []map[string]interface{}{{"function": map[string]string{"name": PreviousTurnSemanticToolName}}}},
	}
	got := RewriteExpiredSemanticGrantNames(history, map[string]bool{"web_search": true})
	if got[0].ToolCalls.([]llm.ToolCall)[0].Function.Name != "invoke_stale" {
		t.Fatal("historical invoke_* token was unexpectedly rewritten")
	}
	if got[1].ToolName != "web_search" {
		t.Fatalf("placeholder tool name = %q", got[1].ToolName)
	}
	if got[1].Content.(map[string]interface{})["name"] != "web_search" {
		t.Fatalf("placeholder content was not rewritten: %#v", got[1].Content)
	}
	fn := got[2].ToolCalls.([]map[string]interface{})[0]["function"].(map[string]string)
	if fn["name"] != "web_search" {
		t.Fatalf("placeholder map function = %q", fn["name"])
	}
	if history[1].ToolName != PreviousTurnSemanticToolName {
		t.Fatal("rewrite mutated original history")
	}
	if history[1].Content.(map[string]interface{})["name"] != PreviousTurnSemanticToolName {
		t.Fatal("rewrite mutated original content map")
	}
}

func TestRewriteExpiredSemanticGrantNamesNoopWhenLookupNotLive(t *testing.T) {
	history := []agent.ConversationEntry{{Role: "tool", ToolName: PreviousTurnSemanticToolName}}
	got := RewriteExpiredSemanticGrantNames(history, map[string]bool{"generate_pdf": true})
	if len(got) != 1 || &got[0] != &history[0] || got[0].ToolName != PreviousTurnSemanticToolName {
		t.Fatal("non-lookup history should be returned unchanged")
	}
	if gotNil := RewriteExpiredSemanticGrantNames(history, nil); &gotNil[0] != &history[0] {
		t.Fatal("nil live grant set should be a no-op")
	}
}
