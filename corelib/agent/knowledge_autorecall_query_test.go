package agent

import (
	"strings"
	"testing"
)

func TestPriorUserMessagesFromHistory(t *testing.T) {
	t.Parallel()
	history := []ConversationEntry{
		{Role: "user", Content: "transformer attention 机制"},
		{Role: "assistant", Content: "attention 是..."},
		{Role: "user", Content: "好的"},
		{Role: "assistant", Content: "还有问题吗"},
		{Role: "user", Content: "多头注意力怎么算"},
	}
	// Last turn is current message; history for prior is everything before it.
	priorHistory := history[:len(history)-1]
	got := PriorUserMessagesFromHistory(priorHistory, KnowledgeAutoRecallPriorUserTurns)
	// "好的" is low-signal and skipped; only the transformer turn remains.
	if len(got) != 1 || got[0] != "transformer attention 机制" {
		t.Fatalf("prior = %#v, want single substantive turn", got)
	}
}

func TestExpandKnowledgeAutoRecallQueryPrefersCurrent(t *testing.T) {
	t.Parallel()
	current := "多头注意力公式"
	prior := []string{"transformer attention 机制"}
	got := ExpandKnowledgeAutoRecallQuery(current, prior)
	if !strings.Contains(got, current) {
		t.Fatalf("query must keep current message: %q", got)
	}
	if !strings.Contains(got, "transformer") {
		t.Fatalf("query should include prior signal: %q", got)
	}

	runes := make([]rune, KnowledgeAutoRecallMaxQueryRunes+40)
	for i := range runes {
		runes[i] = 'a'
	}
	long := string(runes)
	got = ExpandKnowledgeAutoRecallQuery(long, prior)
	if len([]rune(got)) != KnowledgeAutoRecallMaxQueryRunes {
		t.Fatalf("long current length = %d, want %d", len([]rune(got)), KnowledgeAutoRecallMaxQueryRunes)
	}
}

func TestExpandKnowledgeAutoRecallQuerySkipsLowSignalPrior(t *testing.T) {
	t.Parallel()
	got := ExpandKnowledgeAutoRecallQuery("BERT 预训练", []string{"ok", "嗯", "继续"})
	if got != "BERT 预训练" {
		t.Fatalf("low-signal priors should be ignored, got %q", got)
	}
}
