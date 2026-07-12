package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestClassifyIMExecutionProfileUsesEmbeddingOnlyNotFullFusion(t *testing.T) {
	// Build a UIC with an LLM func that would hang if full Classify/fusion ran.
	// Execution-profile routing must use ClassifyEmbeddingOnly and return quickly.
	uic := intent.New(intent.Config{
		Embedder: nil, // embedding unavailable → fast degraded unknown
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			time.Sleep(3 * time.Second)
			return `[{"label":"live_data","score":0.99}]`, nil
		},
		LLMTimeout: 50 * time.Millisecond,
	})

	h := &IMMessageHandler{unifiedClassifier: uic}
	start := time.Now()
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{
		Text: "兰州天气",
	}, false, false)
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("execution-profile classify took %v; full tree fusion should not block", elapsed)
	}
	// Without ready embedder, embedding-only is degraded → full profile.
	if profile.IsLight() || profile.IsDirect() {
		t.Fatalf("profile = %+v, want full when embedding degraded", profile)
	}
	if semantic == nil {
		t.Fatal("expected semantic result from embedding-only path")
	}
	if semantic.Layer == 3 {
		t.Fatalf("expected embedding-only (not tree layer 3), got %+v", semantic)
	}
}

func TestExecutePreparedIMEntryParallelHistoryLoadPattern(t *testing.T) {
	cm := agent.NewConversationMemory()
	cm.Save(desktopUserID, []agent.ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})

	h := &IMMessageHandler{memory: cm}
	type historyLoadResult struct {
		entries []agent.ConversationEntry
		elapsed time.Duration
	}
	historyCh := make(chan historyLoadResult, 1)
	go func() {
		start := time.Now()
		historyCh <- historyLoadResult{entries: h.memory.Load(desktopUserID), elapsed: time.Since(start)}
	}()
	// Early progress is emitted before join in executePreparedIMEntry.
	if imEarlyProgressText == "" {
		t.Fatal("imEarlyProgressText must be non-empty")
	}
	hist := <-historyCh
	if len(hist.entries) != 2 {
		t.Fatalf("history len = %d, want 2", len(hist.entries))
	}
}
