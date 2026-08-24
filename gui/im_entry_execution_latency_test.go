package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestClassifyIMExecutionProfileUsesAuthoritativeSemanticFusion(t *testing.T) {
	// A capability-managed result is used again by the materializer, so the
	// entry point must preserve the full UIC decision rather than retain an
	// ambiguous embedding-only hint and fall back to legacy name routing.
	calls := 0
	uic := intent.New(intent.Config{
		Embedder: nil, // force the tree classifier for this test
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			calls++
			return `{"top":[{"skill":"screenshot","score":0.99}]}`, nil
		},
		LLMTimeout: time.Second,
	})

	h := &IMMessageHandler{unifiedClassifier: uic}
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{
		Text: "截取当前桌面",
	}, false, false)
	if calls != 1 {
		t.Fatalf("UIC tree calls = %d, want 1", calls)
	}
	if semantic == nil {
		t.Fatal("expected semantic result")
	}
	if semantic.Primary != intent.LabelScreenshot || semantic.Layer != 3 {
		t.Fatalf("semantic = %+v, want authoritative screenshot tree result", semantic)
	}
	if profile.IsLight() || profile.IsDirect() || (profile.Reason != "semantic capability-managed intent" && profile.Reason != "semantic capability-managed mutating intent") {
		t.Fatalf("profile = %+v, want governed non-legacy screenshot route", profile)
	}
}

func TestClassifyIMExecutionProfileUsesSemanticWeatherPDFComposite(t *testing.T) {
	uic := intent.New(intent.Config{
		Embedder: nil,
		LLMFunc: func(_, userText string) (string, error) {
			if userText != "北京天气，输出 格式化pdf报告" {
				t.Fatalf("userText=%q", userText)
			}
			return `{"top":[{"skill":"live_data","score":0.95},{"skill":"document_generate","score":0.90},{"skill":"non_coding","score":0.40}]}`, nil
		},
		LLMTimeout: time.Second,
	})

	h := &IMMessageHandler{unifiedClassifier: uic}
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: "北京天气，输出 格式化pdf报告"}, false, false)
	if semantic == nil || semantic.Primary != intent.LabelLiveData || len(semantic.Secondary) != 1 || semantic.Secondary[0] != intent.LabelDocumentGenerate {
		t.Fatalf("semantic=%+v, want live_data + document_generate", semantic)
	}
	if profile.IsLight() || profile.Reason != "semantic capability-managed mutating intent" {
		t.Fatalf("profile=%+v, want full governed composite", profile)
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
