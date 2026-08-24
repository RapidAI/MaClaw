package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func liveGemmaIntentClassifier(t *testing.T) (*intent.UnifiedIntentClassifier, *atomic.Int32) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	modelPath := filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf")
	if _, err := os.Stat(modelPath); err != nil {
		t.Skip("no gemma embedding model found")
	}
	emb, err := embedding.NewGemmaEmbedder(modelPath, embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatalf("load embedder: %v", err)
	}
	t.Cleanup(func() { emb.Close() })

	var llmCalls atomic.Int32
	uic := intent.New(intent.Config{
		Embedder: emb,
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls.Add(1)
			return "", fmt.Errorf("tree unavailable")
		},
		LLMTimeout:         30 * time.Second,
		FusionTreeDeadline: 5 * time.Second,
	})
	deadline := time.Now().Add(90 * time.Second)
	for !uic.Ready() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !uic.Ready() {
		t.Fatal("intent anchor warmup timed out")
	}
	return uic, &llmCalls
}

func TestLiveEmbeddingDesktopWeatherRegression(t *testing.T) {
	uic, llmCalls := liveGemmaIntentClassifier(t)
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: uic}
	registerSemanticCurrentWebLookup(t, h, "ok")

	t.Run("typo falls through without waiting on tree", func(t *testing.T) {
		started := time.Now()
		before := llmCalls.Load()
		classified := uic.Classify(intent.MessageContext{Text: "北京天所"})
		elapsed := time.Since(started)
		t.Logf("beijing-typo primary=%s conf=%.2f degraded=%v layer=%d reason=%q elapsed=%s llm=%d",
			classified.Primary, classified.Confidence, classified.Degraded, classified.Layer, classified.Reason, elapsed, llmCalls.Load()-before)
		if elapsed > 2*time.Second {
			t.Fatalf("classification took %s; short unconfirmed lookup must not wait on L3", elapsed)
		}
		if llmCalls.Load() != before {
			t.Fatalf("LLM calls=%d, want 0", llmCalls.Load()-before)
		}
		if classified.Primary != intent.LabelLiveData && classified.Primary != intent.LabelSearch {
			t.Fatalf("primary=%s, want lookup hint so routing can chat without HostReject", classified.Primary)
		}
		if !classified.Degraded {
			t.Fatal("typo lookup must stay a degraded hint")
		}
		_, surface, handled, err := h.semanticCallSurfaceForSharedTurn("desktop-user", "北京天所", "desktop")
		if err != nil || handled || surface != nil {
			t.Fatalf("must fall through to chat: handled=%v surface=%v err=%v", handled, surface != nil, err)
		}
	})

	t.Run("weather requires semantic confirmation", func(t *testing.T) {
		classified := uic.Classify(intent.MessageContext{Text: "北京天气"})
		t.Logf("beijing-weather primary=%s conf=%.2f degraded=%v layer=%d reason=%q",
			classified.Primary, classified.Confidence, classified.Degraded, classified.Layer, classified.Reason)
		defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("desktop-user", "北京天气", "desktop")
		if classified.Primary == intent.LabelLiveData || classified.Primary == intent.LabelSearch {
			if err != nil || !handled || surface == nil || len(defs) == 0 {
				t.Fatalf("semantic lookup must plan: defs=%d handled=%v err=%v", len(defs), handled, err)
			}
			return
		}
		if err != nil || handled || surface != nil || len(defs) != 0 {
			t.Fatalf("unconfirmed weather must not gain lookup from wording: defs=%d handled=%v surface=%v err=%v", len(defs), handled, surface != nil, err)
		}
	})

	t.Run("weather pdf requires semantic composite", func(t *testing.T) {
		text := "杭州天气，生成pdf报告"
		classified := uic.Classify(intent.MessageContext{Text: text})
		t.Logf("weather-pdf primary=%s conf=%.2f degraded=%v layer=%d secondary=%v reason=%q",
			classified.Primary, classified.Confidence, classified.Degraded, classified.Layer, classified.Secondary, classified.Reason)
		if classificationHasLabel(classified, intent.LabelDocumentGenerate) {
			if classified.Primary != intent.LabelLiveData && classified.Primary != intent.LabelSearch {
				t.Fatalf("declared generate composite missing semantic lookup anchor: %+v", classified)
			}
			return
		}
		if classified.Primary != intent.LabelUnknown || !classified.Degraded {
			t.Fatalf("tree failure must remain an unconfirmed semantic result: %+v", classified)
		}
	})
}
