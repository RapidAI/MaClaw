package intent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestCacheTombstoneDropsStores verifies that a tombstoned scope stops
// caching: the current entry is dropped and subsequent stores (including the
// reclassification's own) are suppressed, so every call re-runs.
func TestCacheTombstoneDropsStores(t *testing.T) {
	var calls atomic.Int32
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			calls.Add(1)
			return `{"top":[{"skill":"coding","score":0.95}]}`, nil
		},
		LLMTimeout: time.Second,
	})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：驱动开发笔记"}

	first := uic.Classify(msg)
	if first.Primary != LabelCoding || first.Degraded {
		t.Fatalf("first classification = %+v, want authoritative coding", first)
	}
	if calls.Load() != 1 {
		t.Fatalf("llm calls = %d, want 1", calls.Load())
	}
	if _, ok := uic.ClassifyCached(msg); !ok {
		t.Fatal("warm classification must be cached before the tombstone")
	}

	uic.AddCacheTombstone(msg)
	if _, ok := uic.ClassifyCached(msg); ok {
		t.Fatal("tombstone must drop the current cache entry")
	}

	second := uic.Classify(msg)
	if second.Primary != LabelCoding || calls.Load() != 2 {
		t.Fatalf("reclassification after tombstone = %+v calls=%d, want fresh LLM call", second, calls.Load())
	}
	if _, ok := uic.ClassifyCached(msg); ok {
		t.Fatal("store during the tombstone window must be suppressed")
	}

	third := uic.Classify(msg)
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3 — suppressed stores must not warm the cache", calls.Load())
	}
	_ = third
}

// TestCacheTombstoneExcludesEpoch verifies the tombstone follows the message
// scope across epoch bumps (unlike cache keys).
func TestCacheTombstoneExcludesEpoch(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{UserID: "u-1", Text: "记住这台服务器的地址"}
	uic.AddCacheTombstone(msg)

	cacheKey := classificationCacheKey(uic.cacheEpoch.Load(), msg)
	uic.cacheEpoch.Add(1)
	if !uic.cacheStoreSuppressed(cacheKey) {
		t.Fatal("tombstone must survive an epoch bump for the same message scope")
	}
	other := MessageContext{UserID: "u-2", Text: msg.Text}
	if uic.cacheStoreSuppressed(classificationCacheKey(uic.cacheEpoch.Load(), other)) {
		t.Fatal("tombstone must not leak across users")
	}
}

// TestCacheTombstoneExpires verifies an expired tombstone stops suppressing.
func TestCacheTombstoneExpires(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库"}
	uic.AddCacheTombstone(msg)

	cacheKey := classificationCacheKey(uic.cacheEpoch.Load(), msg)
	scope := cacheKeyScope(cacheKey)
	uic.cacheTombstones.Store(scope, CacheTombstoneToken{expiry: time.Now().Add(-time.Second)})
	if uic.cacheStoreSuppressed(cacheKey) {
		t.Fatal("expired tombstone must stop suppressing stores")
	}
}

// TestRemoveCacheTombstoneRestoresStores verifies a lifted tombstone lets the
// scope cache again immediately (P0-1 recovery success path) — no TTL wait.
func TestRemoveCacheTombstoneRestoresStores(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：驱动开发笔记"}
	token := uic.AddCacheTombstone(msg)
	cacheKey := classificationCacheKey(uic.cacheEpoch.Load(), msg)
	if !uic.cacheStoreSuppressed(cacheKey) {
		t.Fatal("fresh tombstone must suppress stores")
	}

	uic.RemoveCacheTombstone(msg, token)
	if uic.cacheStoreSuppressed(cacheKey) {
		t.Fatal("removed tombstone must stop suppressing stores")
	}

	// The recovery stores its fresh verdict explicitly; the scope must then
	// satisfy ClassifyCached and later classifications without re-derivation.
	recovered := ClassificationResult{
		Primary: LabelKnowledgeWrite, Confidence: 0.95, Layer: 3,
		Reason:    "tree-after-embedding: knowledge_write (0.950)",
		ToolNames: []string{"knowledge_save_text"},
	}
	uic.StoreRecoveredClassification(msg, recovered)
	got, ok := uic.ClassifyCached(msg)
	if !ok || got.Primary != LabelKnowledgeWrite {
		t.Fatalf("ClassifyCached = %+v ok=%v, want the stored recovered verdict", got, ok)
	}
	if source, ok := uic.CachedClassificationProvenance(msg); !ok || source != CacheSourceTree {
		t.Fatalf("provenance = %q ok=%v, want tree", source, ok)
	}

	// A still-armed tombstone must win over a direct store: a newer recovery
	// pass (or an in-flight race) re-raising the guard is not fought.
	other := MessageContext{UserID: "u-2", Text: msg.Text}
	uic.AddCacheTombstone(other)
	uic.StoreRecoveredClassification(other, recovered)
	if _, ok := uic.ClassifyCached(other); ok {
		t.Fatal("store into a tombstoned scope must stay suppressed")
	}
}

// TestRemoveCacheTombstoneTokenIsolation verifies the disarm-token contract:
// two recoveries arming the same scope concurrently each hold their own
// token; recovery A removing with its token must NOT drop recovery B's newer
// tombstone — B's guard keeps suppressing stores (otherwise a stale in-flight
// late verdict could land in the cache, exactly what the tombstone exists to
// prevent).
func TestRemoveCacheTombstoneTokenIsolation(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：驱动开发笔记"}
	tokenA := uic.AddCacheTombstone(msg)
	tokenB := uic.AddCacheTombstone(msg) // concurrent recovery B re-arms the scope
	cacheKey := classificationCacheKey(uic.cacheEpoch.Load(), msg)

	uic.RemoveCacheTombstone(msg, tokenA)
	if !uic.cacheStoreSuppressed(cacheKey) {
		t.Fatal("recovery A's remove must not disarm recovery B's tombstone")
	}

	// B's guard still drops stores, including a stale late verdict.
	stale := ClassificationResult{
		Primary: LabelFileDownload, Confidence: 0.9, Layer: 3,
		Reason: "late tree verdict after fusion timeout",
	}
	uic.cacheAndLog(cacheKey, msg.Text, &stale)
	if _, ok := uic.ClassifyCached(msg); ok {
		t.Fatal("stale late verdict must stay suppressed while B's tombstone is armed")
	}

	// Only B's own token disarms it.
	uic.RemoveCacheTombstone(msg, tokenB)
	if uic.cacheStoreSuppressed(cacheKey) {
		t.Fatal("recovery B's own token must disarm its tombstone")
	}
}

// TestRemoveCacheTombstoneConcurrentStress arms and disarms the same scope
// from many goroutines (as concurrent recoveries for identical text do):
// every remove carries only its own token, so the last armed tombstone must
// still be suppressing when the dust settles.
func TestRemoveCacheTombstoneConcurrentStress(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：驱动开发笔记"}
	cacheKey := classificationCacheKey(uic.cacheEpoch.Load(), msg)

	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := uic.AddCacheTombstone(msg)
			uic.RemoveCacheTombstone(msg, token)
		}()
	}
	wg.Wait()

	// Final arm: no worker's disarm may have leaked a stale-token delete that
	// defeats this guard.
	final := uic.AddCacheTombstone(msg)
	_ = final
	if !uic.cacheStoreSuppressed(cacheKey) {
		t.Fatal("concurrent add/remove cycles must leave a freshly armed tombstone suppressing")
	}
}

// TestStoreRecoveredClassificationSkipsDegraded verifies degraded results are
// never written as recovered knowledge.
func TestStoreRecoveredClassificationSkipsDegraded(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库"}
	uic.StoreRecoveredClassification(msg, ClassificationResult{
		Primary: LabelUnknown, Confidence: 0.30, Layer: 0, Degraded: true,
	})
	if _, ok := uic.ClassifyCached(msg); ok {
		t.Fatal("degraded result must not be stored as recovered knowledge")
	}
}

// TestCacheProvenanceMarksTreeAndL2 verifies the stored provenance marker:
// Layer>=3 entries are "tree", local-only entries are "l2".
func TestCacheProvenanceMarksTreeAndL2(t *testing.T) {
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
		},
		LLMTimeout: time.Second,
	})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：root 密码"}
	res := uic.Classify(msg)
	if res.Layer != 3 || res.Degraded {
		t.Fatalf("result = %+v, want layer-3 authoritative verdict", res)
	}
	source, ok := uic.CachedClassificationProvenance(msg)
	if !ok || source != CacheSourceTree {
		t.Fatalf("provenance = %q ok=%v, want tree", source, ok)
	}

	// A local-only (Layer 2) store is marked l2.
	l2Msg := MessageContext{UserID: "u-1", Text: "short l2 only"}
	l2 := ClassificationResult{Primary: LabelSearch, Confidence: 0.9, Layer: 2, Reason: "test"}
	uic.cacheAndLog(classificationCacheKey(uic.cacheEpoch.Load(), l2Msg), l2Msg.Text, &l2)
	source, ok = uic.CachedClassificationProvenance(l2Msg)
	if !ok || source != CacheSourceL2 {
		t.Fatalf("l2 provenance = %q ok=%v, want l2", source, ok)
	}

	// Degraded results are never cached, so they carry no provenance.
	if _, ok := uic.CachedClassificationProvenance(MessageContext{UserID: "u-1", Text: "never classified"}); ok {
		t.Fatal("uncached scope must report no provenance")
	}
}

// TestTombstoneBlocksLateVerdictStore models the recovery race: a degraded
// turn schedules a background late verdict; the tombstone written before it
// lands must drop its store.
func TestTombstoneBlocksLateVerdictStore(t *testing.T) {
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMContextFunc: func(ctx context.Context, _ context.Context, _, _ string) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(50 * time.Millisecond):
				return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
			}
		},
		LLMTimeout: 10 * time.Millisecond, // forces the sync path to time out
	})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：驱动开发笔记"}

	degraded := uic.Classify(msg)
	if !degraded.Degraded {
		t.Fatalf("result = %+v, want degraded after tree timeout", degraded)
	}
	// Recovery tombstones the scope before the late verdict goroutine lands.
	uic.AddCacheTombstone(msg)
	time.Sleep(200 * time.Millisecond) // let the late verdict attempt its store

	if _, ok := uic.ClassifyCached(msg); ok {
		t.Fatal("late verdict store during the tombstone window must be dropped")
	}
	if source, ok := uic.CachedClassificationProvenance(msg); ok {
		t.Fatalf("provenance = %q, want none — the late verdict must not be cached", source)
	}
}

// TestReclassificationAfterFailureRecovers exercises the full recovery shape:
// degraded turn, tombstone, then a healthy endpoint re-classifies.
func TestReclassificationAfterFailureRecovers(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	uic := New(Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			if fail.Load() {
				calls.Add(1)
				return "", errors.New("endpoint down")
			}
			calls.Add(1)
			return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
		},
		LLMTimeout: time.Second,
	})
	msg := MessageContext{UserID: "u-1", Text: "保存到知识库：驱动开发笔记"}

	fail.Store(true)
	degraded := uic.Classify(msg)
	if !degraded.Degraded {
		t.Fatalf("result = %+v, want degraded while endpoint is down", degraded)
	}

	uic.AddCacheTombstone(msg)
	fail.Store(false)
	recovered := uic.Classify(msg)
	if recovered.Degraded || recovered.Layer != 3 || recovered.Primary != LabelKnowledgeWrite {
		t.Fatalf("recovered = %+v, want layer-3 knowledge_write", recovered)
	}
	found := false
	for _, name := range recovered.ToolNames {
		if name == "knowledge_save_text" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recovered ToolNames = %v, want knowledge_save_text", recovered.ToolNames)
	}
}

func TestExplicitCapabilityRequestTrigger(t *testing.T) {
	positive := []string{
		"保存到知识库",
		"请将以下内容存入知识库：驱动开发笔记",
		"记入备忘录：明天检查服务器",
		"记住我的服务器地址是 10.0.0.1",
		"把这篇文章收藏到知识库",
	}
	for _, text := range positive {
		if !ExplicitCapabilityRequestTrigger(text) {
			t.Fatalf("trigger(%q) = false, want true", text)
		}
	}
	negative := []string{
		"",
		"你好，帮我看看这段代码",
		"知识库里有关于驱动的资料吗", // read, no save verb
		"保存这个文件到桌面",     // save verb without knowledge destination
		"存档并退出",         // 存档 is not a knowledge-write verb
		"记账本里记一笔",       // 记下 not present; 记账 unrelated
	}
	for _, text := range negative {
		if ExplicitCapabilityRequestTrigger(text) {
			t.Fatalf("trigger(%q) = true, want false", text)
		}
	}
}
