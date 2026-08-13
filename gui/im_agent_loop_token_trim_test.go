package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestFirstAgentLoopRequestTokenLimitCapsArchivedHistory(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": strings.Repeat("system policy ", 300)},
	}
	for i := 0; i < 24; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "assistant",
			"content": strings.Repeat("older transcript ", 700),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_USER_REQUEST_MUST_SURVIVE"})

	limit := firstAgentLoopRequestTokenLimit(80_000, conversation, nil)
	if limit != firstAgentLoopRequestTargetTokens {
		t.Fatalf("limit = %d, want latency target %d", limit, firstAgentLoopRequestTargetTokens)
	}
	trimmed := trimConversation(conversation, limit, 0, nil)
	if len(trimmed) >= len(conversation) {
		t.Fatalf("archived history was not compacted: before=%d after=%d", len(conversation), len(trimmed))
	}
	last, ok := trimmed[len(trimmed)-1].(map[string]string)
	if !ok || last["content"] != "CURRENT_USER_REQUEST_MUST_SURVIVE" {
		t.Fatalf("current user message lost after first-request trim: %#v", trimmed[len(trimmed)-1])
	}
}

func TestFirstAgentLoopRequestTokenLimitProtectsLargeCurrentUserMessage(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": strings.Repeat("document content ", 8_000)},
	}
	limit := firstAgentLoopRequestTokenLimit(80_000, conversation, nil)
	minimum := estimateSingleMsgTokens(conversation[0]) + estimateSingleMsgTokens(conversation[1]) + firstAgentLoopRequestHistoryReserveTokens
	if limit < minimum {
		t.Fatalf("limit = %d, must preserve protected content plus reserve %d", limit, minimum)
	}
}

func TestFirstAgentLoopRequestTokenLimitNeverExceedsProviderLimit(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": strings.Repeat("policy ", 2_000)},
		map[string]string{"role": "user", "content": strings.Repeat("attachment text ", 8_000)},
	}
	if got := firstAgentLoopRequestTokenLimit(6_000, conversation, nil); got != 6_000 {
		t.Fatalf("limit = %d, want provider limit 6000", got)
	}
}

func TestSharedFirstRequestCompactsArchivedHistoryOnly(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "off")
	h := &IMMessageHandler{}
	conversation := []interface{}{
		map[string]string{"role": "system", "content": strings.Repeat("system policy ", 200)},
	}
	for i := 0; i < 32; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "assistant",
			"content": strings.Repeat("old completed work ", 700),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})

	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "first-request-budget-test",
		llmCfg:  corelib.MaclawLLMConfig{ContextLength: 100_000},
	}
	got := cb.TransformConversation(conversation)
	if got == nil || len(got) >= len(conversation) {
		t.Fatalf("first request must compact archived history: before=%d after=%d", len(conversation), len(got))
	}
	if !cb.firstRequestBudgetApplied {
		t.Fatal("first request budget was not marked applied")
	}
	last, ok := got[len(got)-1].(map[string]string)
	if !ok || last["content"] != "CURRENT_REQUEST" {
		t.Fatalf("current request lost after shared-loop trim: %#v", got[len(got)-1])
	}

	// Later rounds deliberately return to normal model context capacity.
	if next := cb.TransformConversation(got); next != nil {
		t.Fatalf("unchanged later round must not re-trim conversation: %#v", next)
	}
}

func TestLegacyFirstRequestBudgetDoesNotPersistIntoLaterRounds(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "off")
	h := &IMMessageHandler{}
	ctx := NewLoopContext("legacy-first-budget", 3, nil)
	ctx.Kind = LoopKindBackground
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": strings.Repeat("old user context ", 8_000)},
		map[string]string{"role": "assistant", "content": strings.Repeat("old assistant context ", 8_000)},
		map[string]string{"role": "user", "content": strings.Repeat("older history ", 8_000)},
		map[string]string{"role": "assistant", "content": strings.Repeat("previous answer ", 8_000)},
		map[string]string{"role": "user", "content": "CURRENT_REQUEST"},
	}
	cfg := corelib.MaclawLLMConfig{ContextLength: 100_000}
	first := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:                 ctx,
		UserID:                  "legacy-first-budget-test",
		UserText:                "CURRENT_REQUEST",
		Iteration:               0,
		EffectiveMax:            3,
		Config:                  cfg,
		Conversation:            conversation,
		FirstRequest:            true,
		LastOutputTokens:        1,
		DirectModeToolsFiltered: false,
	})
	if first.EffectiveTokenLimit != cfg.EffectiveContextTokens() {
		t.Fatalf("first effective limit = %d, want normal provider limit %d", first.EffectiveTokenLimit, cfg.EffectiveContextTokens())
	}
	if len(first.Conversation) >= len(conversation) {
		t.Fatalf("first request did not compact history: before=%d after=%d", len(conversation), len(first.Conversation))
	}

	second := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:                 ctx,
		UserID:                  "legacy-first-budget-test",
		UserText:                "CURRENT_REQUEST",
		Iteration:               1,
		EffectiveMax:            3,
		Config:                  cfg,
		Conversation:            first.Conversation,
		FirstRequest:            false,
		LastOutputTokens:        1,
		DirectModeToolsFiltered: false,
	})
	if second.EffectiveTokenLimit != cfg.EffectiveContextTokens() {
		t.Fatalf("later effective limit = %d, want restored provider limit %d", second.EffectiveTokenLimit, cfg.EffectiveContextTokens())
	}
}

func TestFirstRequestLatencyBudgetSkipsCheckpointIO(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	for i := 0; i < 16; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": strings.Repeat("archived context ", 1_000),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})

	got := (&IMMessageHandler{}).compactAgentLoopConversation(nil, "first-request-checkpoint", conversation, nil, firstAgentLoopRequestTargetTokens, 0, true)
	if len(got) >= len(conversation) {
		t.Fatalf("first request was not structurally compacted: before=%d after=%d", len(conversation), len(got))
	}
	for _, message := range got {
		if role := msgRole(message); role == "tool" {
			t.Fatalf("unexpected checkpoint reader message: %#v", message)
		}
	}
}

func TestFirstRequestTrimNeverDropsOversizedCurrentUserMessage(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "assistant", "content": strings.Repeat("archived answer ", 6_000)},
		map[string]string{"role": "user", "content": "CURRENT_OVERSIZED_REQUEST " + strings.Repeat("attachment text ", 12_000)},
	}

	trimmed := trimConversation(conversation, 4_000, 0, nil)
	last, ok := trimmed[len(trimmed)-1].(map[string]string)
	if !ok || !strings.HasPrefix(last["content"], "CURRENT_OVERSIZED_REQUEST ") {
		t.Fatalf("current oversized request was lost in fallback: %#v", trimmed)
	}
	archived, ok := conversation[1].(map[string]string)
	if !ok || !strings.HasPrefix(archived["content"], "archived answer ") {
		t.Fatalf("trim mutated the caller's archived history: %#v", conversation)
	}
}
