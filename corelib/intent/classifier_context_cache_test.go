package intent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestClassifyCacheIncludesRecentHistory(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})

	withoutContext := uic.Classify(MessageContext{Text: "go ahead"})
	withContext := uic.Classify(MessageContext{
		Text:          "go ahead",
		RecentHistory: []string{"help me develop a snake game"},
	})

	if withoutContext.Primary != LabelContinuation || withContext.Primary != LabelContinuation {
		t.Fatalf("expected continuation labels, got without=%s with=%s", withoutContext.Primary, withContext.Primary)
	}
	if withoutContext.Confidence >= 0.90 {
		t.Fatalf("without context confidence = %.2f, want below contextual threshold", withoutContext.Confidence)
	}
	if withContext.Confidence < 0.90 {
		t.Fatalf("with context confidence = %.2f, want contextual classification not stale cache", withContext.Confidence)
	}
}
