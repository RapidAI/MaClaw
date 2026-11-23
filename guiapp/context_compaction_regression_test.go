package guiapp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestCalibratedAgentLoopTokenLimitComparesCompleteRequestSurface(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{ContextLength: 10000}
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": strings.Repeat("x", 400)},
	}
	const toolsTokens = 2000
	estimated := estimateConversationTokens(conversation) + toolsTokens
	lastInput := estimated * 12 / 10
	limit, gotEstimate := calibratedAgentLoopTokenLimit(cfg, conversation, toolsTokens, lastInput, 1)
	if gotEstimate != estimated {
		t.Fatalf("calibration estimate = %d, want complete surface %d", gotEstimate, estimated)
	}
	want := cfg.EffectiveContextTokens() * 10 / 12
	if limit < want-10 || limit > want+10 {
		t.Fatalf("calibrated limit = %d, want approximately %d", limit, want)
	}
}

func TestTrimHistoryCheckpointProvidesEntryHysteresis(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "build the project"},
		{Role: "assistant", Content: "I will inspect it first"},
	}
	for i := 0; i < 12; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "assistant", Content: "step"})
	}
	const limit = 8
	first := trimHistoryWithSummary(entries, nil, nil, limit, 0)
	foundCheckpoint := false
	for _, entry := range first {
		if entry.CompactionID != "" {
			foundCheckpoint = true
			break
		}
	}
	if !foundCheckpoint {
		t.Fatalf("initial compaction did not create a checkpoint: %#v", first)
	}

	next := append(append([]agent.ConversationEntry(nil), first...),
		agent.ConversationEntry{Role: "user", Content: "continue"},
		agent.ConversationEntry{Role: "assistant", Content: "done"},
	)
	if len(next) <= limit {
		t.Fatalf("fixture must exceed the original entry limit: %d", len(next))
	}
	second := trimHistoryWithSummary(next, nil, nil, limit, 0)
	if !reflect.DeepEqual(second, next) {
		t.Fatalf("a small append immediately rewrote the checkpoint:\nbefore=%#v\nafter=%#v", next, second)
	}

	for len(next) <= historyEntryCompactionTrigger(next, limit) {
		next = append(next, agent.ConversationEntry{Role: "assistant", Content: "more work"})
	}
	third := trimHistoryWithSummary(next, nil, nil, limit, 0)
	if reflect.DeepEqual(third, next) {
		t.Fatal("crossing the high-water mark did not compact again")
	}
}
