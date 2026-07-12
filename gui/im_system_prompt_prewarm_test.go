package main

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestWarmFrozenMemorySnapshot_PrecomputesStaticSection(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Seed a user fact so the static section is non-empty.
	if err := store.Save(corememory.Entry{
		Category: corememory.CategoryUserFact,
		Content:  "User prefers concise answers in Chinese.",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := &IMMessageHandler{memoryStore: store}
	h.WarmFrozenMemorySnapshot(desktopUserID)

	initialized, ok := h.snapshotInitialized.Load(desktopUserID)
	if !ok || initialized != true {
		t.Fatal("expected snapshotInitialized for desktop user")
	}
	raw, ok := h.frozenMemorySnapshots.Load(desktopUserID)
	if !ok {
		t.Fatal("expected frozenMemorySnapshots entry")
	}
	snapshot, _ := raw.(string)
	if snapshot == "" {
		t.Fatal("expected non-empty prewarmed snapshot")
	}

	// Second call is a no-op (does not clear / rewrite under race).
	before := snapshot
	h.WarmFrozenMemorySnapshot(desktopUserID)
	raw2, _ := h.frozenMemorySnapshots.Load(desktopUserID)
	if raw2.(string) != before {
		t.Fatal("second warm should keep existing snapshot")
	}
}

func TestWarmFrozenMemorySnapshot_NilSafe(t *testing.T) {
	var h *IMMessageHandler
	h.WarmFrozenMemorySnapshot("")
	(&IMMessageHandler{}).WarmFrozenMemorySnapshot(desktopUserID)
}

func TestIsVisibleAIAssistantProgressText_EarlyPreLoopAck(t *testing.T) {
	if !isVisibleAIAssistantProgressText(imEarlyProgressText) {
		t.Fatalf("isVisibleAIAssistantProgressText(%q) = false; early pre-loop ack must reach desktop UI", imEarlyProgressText)
	}
}

func TestLoadOrBuildStaticMemorySnapshot_CoalescesConcurrentBuilders(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Save(corememory.Entry{
		Category: corememory.CategoryUserFact,
		Content:  "User prefers concise answers in Chinese.",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := &IMMessageHandler{memoryStore: store}
	var builds atomic.Int32
	// Wrap by calling loadOrBuild from many goroutines; built=true should be rare (ideally 1).
	const n = 16
	var wg sync.WaitGroup
	results := make([]string, n)
	builtFlags := make([]bool, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			text, built := h.loadOrBuildStaticMemorySnapshot(desktopUserID)
			results[i] = text
			builtFlags[i] = built
			if built {
				builds.Add(1)
			}
		}()
	}
	wg.Wait()

	if builds.Load() != 1 {
		t.Fatalf("built count = %d, want exactly 1 singleflight builder", builds.Load())
	}
	base := results[0]
	if base == "" {
		t.Fatal("empty snapshot")
	}
	for i, text := range results {
		if text != base {
			t.Fatalf("worker %d got different snapshot (len %d vs %d)", i, len(text), len(base))
		}
	}
	// Stable after concurrent build.
	time.Sleep(10 * time.Millisecond)
	again, built := h.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if built || again != base {
		t.Fatalf("follow-up load built=%v len=%d want cache hit of %d", built, len(again), len(base))
	}
}

func TestRefreshMemorySnapshot_DoesNotOrphanSingleflightWaiters(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Save(corememory.Entry{
		Category: corememory.CategoryUserFact,
		Content:  "User prefers concise answers in Chinese.",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	h := &IMMessageHandler{memoryStore: store}

	// Start a slow-ish concurrent build by racing warm + refresh.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.WarmFrozenMemorySnapshot(desktopUserID)
	}()
	// Give the warm a head start to claim singleflight.
	time.Sleep(5 * time.Millisecond)
	h.RefreshMemorySnapshot(desktopUserID)
	wg.Wait()

	// After refresh, cache must be empty (or re-built only if warm finished after).
	// Either empty or non-empty is OK as long as we don't hang; require no panic and
	// a subsequent load succeeds.
	text, _ := h.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if text == "" {
		t.Fatal("expected snapshot rebuild after refresh")
	}
}

func TestRefreshMemorySnapshot_InvalidatesInFlightBuild(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Save(corememory.Entry{
		Category: corememory.CategoryUserFact,
		Content:  "fact-before-refresh",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	h := &IMMessageHandler{memoryStore: store}

	// Claim singleflight and hold it open so we control publish timing.
	doneCh := make(chan struct{})
	h.snapshotWarmInflight.Store(desktopUserID, doneCh)
	gen := h.snapshotGeneration(desktopUserID)

	// Refresh bumps generation and clears cache while "build" is still open.
	h.bumpSnapshotGeneration(desktopUserID)
	h.frozenMemorySnapshots.Delete(desktopUserID)
	h.snapshotInitialized.Delete(desktopUserID)

	// Stale build under old gen must not stick.
	text, built := h.buildAndStoreStaticMemorySnapshot(desktopUserID, gen)
	if built || text != "" {
		t.Fatalf("stale build published after refresh: built=%v len=%d", built, len(text))
	}
	if h.cachedStaticMemorySnapshot(desktopUserID) != "" {
		t.Fatal("cache should remain empty after discarded stale build")
	}
	close(doneCh)
	h.snapshotWarmInflight.Delete(desktopUserID)

	// Fresh build under current gen succeeds.
	fresh, built := h.loadOrBuildStaticMemorySnapshot(desktopUserID)
	if !built || fresh == "" {
		t.Fatalf("fresh build failed: built=%v len=%d", built, len(fresh))
	}
}

func TestAppendMemorySectionReusesPrewarmOnFirstTurn(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Save(corememory.Entry{
		Category: corememory.CategoryUserFact,
		Content:  "User prefers concise answers in Chinese.",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := &IMMessageHandler{memoryStore: store}
	h.WarmFrozenMemorySnapshot(desktopUserID)

	rawBefore, ok := h.frozenMemorySnapshots.Load(desktopUserID)
	if !ok {
		t.Fatal("expected prewarmed snapshot")
	}
	before := rawBefore.(string)

	// First-turn path must reuse prewarm (the whole point of prewarm).
	var b strings.Builder
	h.appendMemorySection(&b, true /* isFirstTurn / includeMemoryGuide */, desktopUserID, lifecycle.EventContext{}, "hello")
	out := b.String()
	if out == "" {
		t.Fatal("expected non-empty memory section")
	}
	if !strings.Contains(out, before) && !strings.HasPrefix(out, before) {
		// Proactive recall may append after static; static must still be present.
		t.Fatalf("first-turn section does not include prewarmed static snapshot (static_len=%d out_len=%d)", len(before), len(out))
	}
	rawAfter, ok := h.frozenMemorySnapshots.Load(desktopUserID)
	if !ok {
		t.Fatal("snapshot missing after first-turn append")
	}
	after := rawAfter.(string)
	if after != before {
		t.Fatalf("first-turn append regenerated snapshot (prewarm was ignored); before_len=%d after_len=%d", len(before), len(after))
	}
}
