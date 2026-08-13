package agent

import (
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestCanInferIntentFromHistoryWithoutClassifierDoesNotUseKeywords(t *testing.T) {
	old := GetUnifiedClassifier()
	SetUnifiedClassifier(nil)
	t.Cleanup(func() { SetUnifiedClassifier(old) })

	entries := []ConversationEntry{{Role: "user", Content: "bug error code review screenshot"}}
	if CanInferIntentFromHistory(entries) {
		t.Fatal("expected false without semantic classifier; local phrase matching must not infer media intent")
	}
}

func TestCanInferIntentFromHistoryDoesNotCallTreeLLM(t *testing.T) {
	old := GetUnifiedClassifier()
	var llmCalls atomic.Int32
	SetUnifiedClassifier(intent.New(intent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls.Add(1)
			return `{"top":[{"skill":"coding","score":0.96}]}`, nil
		},
	}))
	t.Cleanup(func() { SetUnifiedClassifier(old) })

	if CanInferIntentFromHistory([]ConversationEntry{{Role: "user", Content: "fix failing tests"}}) {
		t.Fatal("unavailable embedding should conservatively preserve pending media")
	}
	if got := llmCalls.Load(); got != 0 {
		t.Fatalf("media history inference must not call tree LLM, got %d calls", got)
	}
}
