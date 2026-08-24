package intent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestDefaultLLMTimeoutAllowsRemoteClassifier(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	if uic.llmTimeout < 30*time.Second {
		t.Fatalf("llmTimeout = %s, want >= 30s", uic.llmTimeout)
	}
}

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

func TestSetLLMFuncInvalidatesDegradedCache(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{Text: "fix failing tests"}

	before := uic.Classify(msg)
	if before.Primary != LabelUnknown {
		t.Fatalf("before SetLLMFunc primary = %s, want %s", before.Primary, LabelUnknown)
	}

	uic.SetLLMFunc(func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"coding","score":0.95,"reason":"requires code changes","workflow_type":"coding"}]}`, nil
	})

	after := uic.Classify(msg)
	if after.Primary != LabelCoding {
		t.Fatalf("after SetLLMFunc primary = %s, want %s; cache may be stale", after.Primary, LabelCoding)
	}
	if after.Layer != 3 {
		t.Fatalf("after SetLLMFunc layer = %d, want 3", after.Layer)
	}
}

func TestClassifierIgnoresStaleCacheFromPreviousEpoch(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{Text: "fix failing tests"}
	stale := &ClassificationResult{Primary: LabelUnknown, Confidence: 0.30, Layer: 0, Reason: "stale degraded result", Degraded: true}
	uic.cache.Store(classificationCacheKey(uic.cacheEpoch.Load(), msg), stale)

	uic.SetLLMFunc(func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"coding","score":0.95,"reason":"requires code changes","workflow_type":"coding"}]}`, nil
	})
	uic.cache.Store(classificationCacheKey(uic.cacheEpoch.Load()-1, msg), stale)

	result := uic.Classify(msg)
	if result.Primary != LabelCoding {
		t.Fatalf("primary = %s, want %s; stale cache from previous epoch was reused", result.Primary, LabelCoding)
	}
}

func TestLLMOnlyPathUsesConfiguredTimeout(t *testing.T) {
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			time.Sleep(60 * time.Millisecond)
			return `{"top":[{"skill":"coding","score":0.95}]}`, nil
		},
		LLMTimeout: 10 * time.Millisecond,
	})

	started := time.Now()
	result := uic.Classify(MessageContext{Text: "fix failing tests"})
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("Classify took %s, want bounded by configured timeout", elapsed)
	}
	if !result.Degraded || result.Primary != LabelUnknown {
		t.Fatalf("result = primary=%s degraded=%v, want degraded unknown on timeout", result.Primary, result.Degraded)
	}
}

func TestClassifyContextCancelsTreeAndDoesNotCacheDegradedResult(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMContextFunc: func(ctx context.Context, _, _ string) (string, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return "", ctx.Err()
			}
			return `{"top":[{"skill":"coding","score":0.95}]}`, nil
		},
		LLMTimeout: time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan ClassificationResult, 1)
	go func() { resultCh <- uic.ClassifyContext(ctx, MessageContext{UserID: "u-1", Text: "fix failing tests"}) }()
	select {
	case <-started:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("tree classification did not start")
	}
	select {
	case result := <-resultCh:
		if !result.Degraded {
			t.Fatalf("cancelled classification = %+v, want degraded", result)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled tree classification did not return")
	}

	result := uic.Classify(MessageContext{UserID: "u-1", Text: "fix failing tests"})
	if result.Primary != LabelCoding || result.Degraded || calls.Load() != 2 {
		t.Fatalf("fresh classification = %+v calls=%d, want authoritative uncached result", result, calls.Load())
	}
}

func TestClassifyContextAlreadyCancelledDoesNotUseCacheOrStartTree(t *testing.T) {
	var calls atomic.Int32
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			calls.Add(1)
			return `{"top":[{"skill":"coding","score":0.95}]}`, nil
		},
	})
	msg := MessageContext{UserID: "u-1", Text: "fix failing tests"}
	if got := uic.Classify(msg); got.Primary != LabelCoding || calls.Load() != 1 {
		t.Fatalf("initial classification = %+v calls=%d", got, calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := uic.ClassifyContext(ctx, msg)
	if !got.Degraded || got.Primary != LabelUnknown || calls.Load() != 1 {
		t.Fatalf("cancelled classification = %+v calls=%d, want uncached degraded result", got, calls.Load())
	}
}

func TestClassificationCacheIsPrincipalScopedAndCollisionSafe(t *testing.T) {
	var calls atomic.Int32
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			calls.Add(1)
			return `{"top":[{"skill":"coding","score":0.95}]}`, nil
		},
	})

	// These inputs collided under the former delimiter-only cache key.
	left := MessageContext{UserID: "tenant-a", Text: "x\x00y", RecentHistory: []string{"z"}}
	right := MessageContext{UserID: "tenant-a\x00x", Text: "y", RecentHistory: []string{"z"}}
	if classificationCacheKey(1, left) == classificationCacheKey(1, right) {
		t.Fatal("distinct semantic inputs must not share a cache key")
	}

	first := uic.Classify(MessageContext{UserID: "alice", Text: "fix failing tests"})
	second := uic.Classify(MessageContext{UserID: "bob", Text: "fix failing tests"})
	if first.Primary != LabelCoding || second.Primary != LabelCoding || calls.Load() != 2 {
		t.Fatalf("principal-scoped calls=%d first=%+v second=%+v, want two independent classifications", calls.Load(), first, second)
	}
}
