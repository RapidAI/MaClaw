package main

import (
	"testing"
)

func TestPruneStaleNoToolTurns_RemovesConsecutiveNoToolAssistant(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "You are a helpful assistant."},
		map[string]string{"role": "user", "content": "继续 docker 备份"},
		// Stale assistant response (no tool_calls)
		map[string]interface{}{"role": "assistant", "content": "收到，已纳入当前任务。"},
		// Recover prompt injected
		map[string]string{"role": "system", "content": "[Recover 阶段]\n连续 1 轮都没有真正调用工具\n[/Recover 阶段]"},
		// Another stale assistant response
		map[string]interface{}{"role": "assistant", "content": "好的，我来继续执行。"},
		// Another recover prompt
		map[string]string{"role": "system", "content": "[执行要求]\n当前任务需要真实执行\n[/执行要求]"},
	}

	result := pruneStaleNoToolTurns(conversation)

	// Should keep only system prompt + user message
	if len(result) != 2 {
		t.Fatalf("expected 2 messages after pruning, got %d", len(result))
	}
	// First should be system prompt
	if msg, ok := result[0].(map[string]string); !ok || msg["role"] != "system" {
		t.Fatal("expected system prompt at index 0")
	}
	// Second should be user message
	if msg, ok := result[1].(map[string]string); !ok || msg["role"] != "user" {
		t.Fatal("expected user message at index 1")
	}
}

func TestPruneStaleNoToolTurns_StopsAtToolCallAssistant(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "system prompt"},
		map[string]string{"role": "user", "content": "do something"},
		// Productive assistant with tool_calls
		map[string]interface{}{
			"role":       "assistant",
			"content":    "Let me check.",
			"tool_calls": []interface{}{map[string]interface{}{"id": "call_1", "function": map[string]interface{}{"name": "ssh"}}},
		},
		map[string]interface{}{"role": "tool", "content": "result", "tool_call_id": "call_1"},
		// Stale assistant (no tool_calls)
		map[string]interface{}{"role": "assistant", "content": "I see the result."},
		// Recover prompt
		map[string]string{"role": "system", "content": "[Recover 阶段]\ntest\n[/Recover 阶段]"},
	}

	result := pruneStaleNoToolTurns(conversation)

	// Should keep system + user + assistant(with tools) + tool result = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 messages after pruning, got %d", len(result))
	}
}

func TestPruneStaleNoToolTurns_StopsAtUserMessage(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "system prompt"},
		map[string]string{"role": "user", "content": "first message"},
		map[string]interface{}{"role": "assistant", "content": "response 1"},
		map[string]string{"role": "user", "content": "second message"},
		// Stale assistant
		map[string]interface{}{"role": "assistant", "content": "stale response"},
		map[string]string{"role": "system", "content": "[Recover 阶段]\ntest\n[/Recover 阶段]"},
	}

	result := pruneStaleNoToolTurns(conversation)

	// Should keep up to and including the second user message = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 messages after pruning, got %d", len(result))
	}
}

func TestPruneStaleNoToolTurns_NoopWhenNothingToPrune(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "system prompt"},
		map[string]string{"role": "user", "content": "hello"},
		map[string]interface{}{
			"role":       "assistant",
			"content":    "calling tool",
			"tool_calls": []interface{}{map[string]interface{}{"id": "call_1"}},
		},
	}

	result := pruneStaleNoToolTurns(conversation)

	if len(result) != len(conversation) {
		t.Fatalf("expected no pruning, got %d messages (was %d)", len(result), len(conversation))
	}
}

func TestPruneStaleNoToolTurns_PreservesNonRecoverSystemMessages(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "system prompt"},
		map[string]string{"role": "user", "content": "do something"},
		// Non-recover system message (e.g., goal anchor)
		map[string]string{"role": "system", "content": "[任务清单]\n- step 1\n[/任务清单]"},
		// Stale assistant after the non-recover system message
		map[string]interface{}{"role": "assistant", "content": "stale"},
	}

	result := pruneStaleNoToolTurns(conversation)

	// Should prune the stale assistant but stop at the non-recover system message
	if len(result) != 3 {
		t.Fatalf("expected 3 messages after pruning, got %d", len(result))
	}
}

func TestPruneStaleNoToolTurns_EmptyConversation(t *testing.T) {
	result := pruneStaleNoToolTurns(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}

	result = pruneStaleNoToolTurns([]interface{}{
		map[string]string{"role": "system", "content": "prompt"},
	})
	if len(result) != 1 {
		t.Fatalf("expected 1 for single-element input, got %d", len(result))
	}
}

func TestPruneStaleNoToolTurns_AlternatingStaleAndRecover(t *testing.T) {
	// This is the actual pattern that causes the positive feedback loop:
	// assistant(no tools) → recover prompt → assistant(no tools) → recover prompt → ...
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "You are a helpful assistant."},
		map[string]interface{}{"role": "user", "content": "继续 docker 备份"},
		// Round 1: stale
		map[string]interface{}{"role": "assistant", "content": "收到，已纳入当前任务。"},
		map[string]string{"role": "system", "content": "[执行要求]\n当前任务需要真实执行\n[/执行要求]"},
		// Round 2: stale
		map[string]interface{}{"role": "assistant", "content": "好的，我来继续。"},
		map[string]string{"role": "system", "content": "[Recover 阶段]\n连续 2 轮没有调用工具\n[/Recover 阶段]"},
		// Round 3: stale
		map[string]interface{}{"role": "assistant", "content": "让我检查一下。"},
		map[string]string{"role": "system", "content": "[Recover 阶段]\n连续 3 轮没有调用工具\n[/Recover 阶段]"},
	}

	result := pruneStaleNoToolTurns(conversation)

	// Should keep only system prompt + user message (indices 0-1)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages after pruning 3 stale rounds, got %d", len(result))
	}
	if msgRole(result[0]) != "system" || msgRole(result[1]) != "user" {
		t.Fatal("expected [system, user] after pruning")
	}
}

func TestPruneStaleNoToolTurns_MapStringStringAssistant(t *testing.T) {
	// Edge case: assistant message stored as map[string]string (no tool_calls possible).
	// This can happen with certain code paths. Should be treated as stale.
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "system prompt"},
		map[string]string{"role": "user", "content": "do something"},
		map[string]string{"role": "assistant", "content": "I'll do it now."},
	}

	result := pruneStaleNoToolTurns(conversation)

	// map[string]string assistant has no tool_calls → stale → pruned
	if len(result) != 2 {
		t.Fatalf("expected 2 messages after pruning map[string]string assistant, got %d", len(result))
	}
}

func TestIsRecoverOrNudgeSystemMessage(t *testing.T) {
	cases := []struct {
		content  string
		expected bool
	}{
		{"[Recover 阶段]\n连续 2 轮都没有真正调用工具\n[/Recover 阶段]", true},
		{"[执行要求]\n当前任务需要真实执行\n[/执行要求]", true},
		{"[任务清单]\n- step 1\n[/任务清单]", false},
		{"You are a helpful assistant.", false},
		{"", false},
		{"[Recover 阶段] partial", true},
	}

	for _, tc := range cases {
		got := isRecoverOrNudgeSystemMessage(tc.content)
		preview := tc.content
		if len(preview) > 50 {
			preview = preview[:50]
		}
		if got != tc.expected {
			t.Errorf("isRecoverOrNudgeSystemMessage(%q) = %v, want %v", preview, got, tc.expected)
		}
	}
}
