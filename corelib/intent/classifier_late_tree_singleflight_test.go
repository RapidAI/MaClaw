package intent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestLateTreeVerdictSingleFlightIgnoresEpoch verifies the P0-3 functional
// in-flight key: the single-flight claim for a background tree verdict is the
// epoch-agnostic scope (UserID + Text + history), so a late-verdict re-run and
// a reclassification racing across an epoch bump cannot double-send the same
// tree call. The verdict still stores only under its captured epoch-bound
// cache key.
func TestLateTreeVerdictSingleFlightIgnoresEpoch(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
			calls.Add(1)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-release:
				return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
			}
		},
		LLMTimeout: time.Second,
	})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：驱动开发笔记"}
	keyEpoch0 := classificationCacheKey(uic.cacheEpoch.Load(), msg)

	uic.scheduleLateTreeVerdict(keyEpoch0, msg.Text)
	// Wait until the first verdict actually occupies the single-flight claim.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("tree calls = %d, want 1 after first schedule", got)
	}
	// A reclassification (or epoch bump) racing the first verdict must not
	// schedule a second tree call for the same scope.
	uic.cacheEpoch.Add(1)
	keyEpoch1 := classificationCacheKey(uic.cacheEpoch.Load(), msg)
	uic.scheduleLateTreeVerdict(keyEpoch1, msg.Text)

	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("tree calls = %d, want 1 (single-flight across epoch bump)", got)
	}
	close(release)
	for time.Now().Before(deadline) {
		if _, ok := uic.cache.Load(keyEpoch0); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("tree calls after release = %d, want 1", got)
	}
	// The verdict stores under the epoch-0 key it was captured with; the
	// epoch-1 scope must not see it (stale verdicts never satisfy a newer
	// configuration).
	if _, ok := uic.ClassifyCached(msg); ok {
		t.Fatal("stale epoch verdict must not satisfy the bumped-epoch scope")
	}
}
