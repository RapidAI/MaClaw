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

	if withoutContext.Primary != LabelUnknown || withContext.Primary != LabelUnknown {
		t.Fatalf("expected conservative unknown labels without semantic classifiers, got without=%s with=%s", withoutContext.Primary, withContext.Primary)
	}
	if withoutContext.Reason != "semantic classifiers unavailable" {
		t.Fatalf("unexpected reason without context: %q", withoutContext.Reason)
	}
	if withContext.Reason != "semantic classifiers unavailable" {
		t.Fatalf("unexpected reason with context: %q", withContext.Reason)
	}
}
