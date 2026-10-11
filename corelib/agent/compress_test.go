package agent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// hugeContent builds a message body large enough to blow a small context budget.
func hugeContent(filler string, times int) string {
	out := make([]byte, 0, len(filler)*times)
	for i := 0; i < times; i++ {
		out = append(out, filler...)
	}
	return string(out)
}

// TestCompressionHookTrimsOverBudgetConversation proves the hook actually
// shrinks a conversation that has outgrown the window. Without it the loop
// keeps re-sending the whole history on every iteration.
func TestCompressionHookTrimsOverBudgetConversation(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{ContextLength: 4000}
	hook := NewCompressionHook(func() corelib.MaclawLLMConfig { return cfg }, nil)

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "you are a helper"},
	}
	for i := 0; i < 60; i++ {
		conversation = append(conversation,
			map[string]interface{}{"role": "user", "content": hugeContent("x", 2000)},
			map[string]interface{}{"role": "assistant", "content": hugeContent("y", 2000)},
		)
	}
	before := EstimateConversationTokens(conversation)

	out := hook.TransformConversation(conversation)
	if out == nil {
		t.Fatal("hook returned nil: conversation left untrimmed despite exceeding the budget")
	}
	after := EstimateConversationTokens(out)
	if after >= before {
		t.Fatalf("expected the trim to shrink the conversation, got %d -> %d tokens", before, after)
	}
	if len(out) >= len(conversation) {
		t.Fatalf("expected fewer messages, got %d -> %d", len(conversation), len(out))
	}
	if role, _ := ExtractRoleContent(out[0]); role != "system" {
		t.Fatalf("system prompt must survive the trim, first message role is %q", role)
	}
}

// TestCompressionHookLeavesSmallConversationAlone guards the hot path: the hook
// runs before every LLM request, so a conversation inside the budget must not
// be touched or reallocated.
func TestCompressionHookLeavesSmallConversationAlone(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{ContextLength: 200000}
	hook := NewCompressionHook(func() corelib.MaclawLLMConfig { return cfg }, nil)

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "you are a helper"},
		map[string]interface{}{"role": "user", "content": "hello"},
	}
	if out := hook.TransformConversation(conversation); out != nil {
		t.Fatalf("hook must return nil when nothing needs dropping, got %d messages", len(out))
	}
}

// TestCompressionHookKeepsToolCallPairsIntact is the reason the hook trims
// through TrimConversation instead of the ctxcompress round trip: an assistant
// message carrying tool_calls and the tool results that answer it are one group
// and must never be separated, or the provider rejects the request.
func TestCompressionHookKeepsToolCallPairsIntact(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{ContextLength: 4000}
	hook := NewCompressionHook(func() corelib.MaclawLLMConfig { return cfg }, nil)

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "you are a helper"},
	}
	// Enough filler rounds that the trim has to drop from the middle.
	for i := 0; i < 40; i++ {
		conversation = append(conversation,
			map[string]interface{}{"role": "user", "content": hugeContent("x", 2000)},
			map[string]interface{}{"role": "assistant", "content": hugeContent("y", 2000)},
		)
	}
	// The newest round is a tool batch. Trimming must not orphan it.
	conversation = append(conversation,
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []interface{}{
				map[string]interface{}{"id": "call_1", "type": "function"},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "done"},
		map[string]interface{}{"role": "user", "content": "and now?"},
	)

	out := hook.TransformConversation(conversation)
	if out == nil {
		t.Fatal("hook returned nil: nothing was trimmed")
	}
	assertToolPairsIntact(t, out)
}

// assertToolPairsIntact fails if any assistant tool_calls message is not
// followed by a tool result for each of its call ids.
func assertToolPairsIntact(t *testing.T, conversation []interface{}) {
	t.Helper()
	pending := map[string]bool{}
	for _, msg := range conversation {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		switch role {
		case "assistant":
			if m["tool_calls"] == nil {
				continue
			}
			calls, ok := m["tool_calls"].([]interface{})
			if !ok {
				continue
			}
			for _, c := range calls {
				call, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if id, _ := call["id"].(string); id != "" {
					pending[id] = true
				}
			}
		case "tool":
			id, _ := m["tool_call_id"].(string)
			if pending[id] {
				delete(pending, id)
			}
		}
	}
	for id := range pending {
		t.Fatalf("tool call %q survived without its result message", id)
	}
}

// TestCompressionHookHonoursPerRoundConfig checks the hook re-reads the config
// each round, since hosts may escalate the model mid-loop and a bigger window
// changes when trimming is needed.
func TestCompressionHookHonoursPerRoundConfig(t *testing.T) {
	small := corelib.MaclawLLMConfig{ContextLength: 4000}
	large := corelib.MaclawLLMConfig{ContextLength: 4000000}
	current := small
	hook := NewCompressionHook(func() corelib.MaclawLLMConfig { return current }, nil)

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "you are a helper"},
	}
	for i := 0; i < 60; i++ {
		conversation = append(conversation,
			map[string]interface{}{"role": "user", "content": hugeContent("x", 2000)},
			map[string]interface{}{"role": "assistant", "content": hugeContent("y", 2000)},
		)
	}

	if out := hook.TransformConversation(conversation); out == nil {
		t.Fatal("expected a trim under the small window")
	}
	current = large
	if out := hook.TransformConversation(conversation); out != nil {
		t.Fatalf("expected no trim once the window grew, got %d messages", len(out))
	}
}

// TestCompressionHookNilSafe keeps a zero-value or half-built hook inert rather
// than letting it panic inside the loop.
func TestCompressionHookNilSafe(t *testing.T) {
	var hook *CompressionHook
	if out := hook.TransformConversation([]interface{}{map[string]string{"role": "user", "content": "hi"}}); out != nil {
		t.Fatal("nil hook must be inert")
	}
	noConfig := &CompressionHook{}
	if out := noConfig.TransformConversation([]interface{}{map[string]string{"role": "user", "content": "hi"}}); out != nil {
		t.Fatal("hook without a config source must be inert")
	}
}
