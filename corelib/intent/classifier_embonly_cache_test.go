package intent

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// embOnlyTestEmbedder maps texts containing a marker phrase to one axis and
// counts Embed calls, so tests can assert cache hit/miss behavior.
type embOnlyTestEmbedder struct {
	hit   string
	calls atomic.Int64
}

func (e *embOnlyTestEmbedder) Embed(text string) ([]float32, error) {
	e.calls.Add(1)
	if strings.Contains(text, e.hit) {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func (e *embOnlyTestEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := e.Embed(t)
		out[i] = v
	}
	return out, nil
}

func (e *embOnlyTestEmbedder) Dim() int { return 2 }
func (e *embOnlyTestEmbedder) Close()   {}

func waitUICReadyForTest(t *testing.T, uic *UnifiedIntentClassifier) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !uic.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("UIC anchor warmup timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClassifyEmbeddingOnlyCachesPerText(t *testing.T) {
	emb := &embOnlyTestEmbedder{hit: "桌面"}
	uic := New(Config{Embedder: emb})
	waitUICReadyForTest(t, uic)

	before := emb.calls.Load()
	r1 := uic.ClassifyEmbeddingOnly(MessageContext{Text: "打开桌面上的记事本"})
	r2 := uic.ClassifyEmbeddingOnly(MessageContext{Text: "打开桌面上的记事本"})
	if r1.Primary != r2.Primary {
		t.Fatalf("inconsistent results: %s vs %s", r1.Primary, r2.Primary)
	}
	if got := emb.calls.Load() - before; got != 1 {
		t.Fatalf("repeated text should embed once, got %d", got)
	}

	uic.ClassifyEmbeddingOnly(MessageContext{Text: "今天天气怎么样"})
	if got := emb.calls.Load() - before; got != 2 {
		t.Fatalf("new text should embed once more, total %d", got)
	}

	uic.InvalidateCache()
	uic.ClassifyEmbeddingOnly(MessageContext{Text: "打开桌面上的记事本"})
	if got := emb.calls.Load() - before; got != 3 {
		t.Fatalf("InvalidateCache should force recompute, total %d", got)
	}
}

func TestClassifyEmbeddingOnlyDoesNotCacheDegraded(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	for i := 0; i < 3; i++ {
		if res := uic.ClassifyEmbeddingOnly(MessageContext{Text: "anything"}); !res.Degraded {
			t.Fatal("expected degraded result with noop embedder")
		}
	}
	if uic.embOnlyCount.Load() != 0 {
		t.Fatal("degraded results must not be cached")
	}
}

func TestClassifyEmbeddingOnlyCacheBounded(t *testing.T) {
	emb := &embOnlyTestEmbedder{hit: "桌面"}
	uic := New(Config{Embedder: emb})
	waitUICReadyForTest(t, uic)
	for i := 0; i < embOnlyCacheMaxEntries+50; i++ {
		uic.ClassifyEmbeddingOnly(MessageContext{Text: fmt.Sprintf("unique text %d", i)})
	}
	if got := uic.embOnlyCount.Load(); got > embOnlyCacheMaxEntries {
		t.Fatalf("cache count %d exceeds bound %d", got, embOnlyCacheMaxEntries)
	}
}
