package guiapp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// degradedRecoveryTestHooks captures the overridable seams.
type degradedRecoveryTestHooks struct {
	mu           sync.Mutex
	notices      []string
	continuations []degradedRecoveryPlan
	activated    [][]string
}

func (c *degradedRecoveryTestHooks) install(t *testing.T) {
	t.Helper()
	oldNotice := emitDegradedRecoveryNoticeFn
	oldRun := runDegradedTurnContinuationFn
	emitDegradedRecoveryNoticeFn = func(_ *IMMessageHandler, _ , _ , text string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.notices = append(c.notices, text)
	}
	runDegradedTurnContinuationFn = func(_ *IMMessageHandler, plan degradedRecoveryPlan, activated []string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.continuations = append(c.continuations, plan)
		c.activated = append(c.activated, activated)
	}
	t.Cleanup(func() {
		emitDegradedRecoveryNoticeFn = oldNotice
		runDegradedTurnContinuationFn = oldRun
	})
}

func (c *degradedRecoveryTestHooks) noticeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.notices)
}

func (c *degradedRecoveryTestHooks) continuationCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.continuations)
}

func degradedTestLoopContext(requestID string, degraded *intent.ClassificationResult) *LoopContext {
	return &LoopContext{Runtime: RuntimeContext{
		RequestID:      requestID,
		SemanticIntent: degraded,
	}}
}

func degradedUnknownIntent() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
		Reason: "embedding ambiguous; tree classification unavailable (l2=file_download conf=0.75)",
	}
}

func webSearchOnlyTools() []map[string]interface{} {
	return []map[string]interface{}{toolDef("web_search", "search the web", nil, nil)}
}

// The three trigger conditions are conjunctive — drop any one and no
// recovery attempt is recorded.
func TestMaybeRecoverDegradedTurnRequiresAllTriggerConditions(t *testing.T) {
	h := &IMMessageHandler{}
	mgr := h.degradedTurnRecovery()
	hooks := &degradedRecoveryTestHooks{}
	hooks.install(t)
	userID := "user-1"
	text := "保存到知识库：驱动开发笔记"

	// 1) not degraded → no attempt
	h.maybeRecoverDegradedTurn(degradedTestLoopContext("req-1", &intent.ClassificationResult{
		Primary: intent.LabelKnowledgeWrite, Confidence: 0.9, Layer: 3,
	}), userID, text, "req-1", webSearchOnlyTools(), "desktop", "")
	if n := len(mgr.attemptsSnapshot()); n != 0 {
		t.Fatalf("attempts = %d, want 0 for a non-degraded turn", n)
	}

	// 2) degraded but no lexical trigger → no attempt
	h.maybeRecoverDegradedTurn(degradedTestLoopContext("req-2", degradedUnknownIntent()), userID, "你好，看看这段代码", "req-2", webSearchOnlyTools(), "desktop", "")
	if n := len(mgr.attemptsSnapshot()); n != 0 {
		t.Fatalf("attempts = %d, want 0 without a lexical trigger", n)
	}

	// 3) degraded + trigger but the write tool is already on the table → no attempt
	withWrite := append(webSearchOnlyTools(), toolDef("knowledge_save_text", "save text to knowledge base", nil, nil))
	h.maybeRecoverDegradedTurn(degradedTestLoopContext("req-3", degradedUnknownIntent()), userID, text, "req-3", withWrite, "desktop", "")
	if n := len(mgr.attemptsSnapshot()); n != 0 {
		t.Fatalf("attempts = %d, want 0 when the write tool was present", n)
	}

	// all three hold → exactly one attempt; a second call for the same turn
	// is deduped.
	h.maybeRecoverDegradedTurn(degradedTestLoopContext("req-4", degradedUnknownIntent()), userID, text, "req-4", webSearchOnlyTools(), "desktop", "")
	if n := len(mgr.attemptsSnapshot()); n != 1 {
		t.Fatalf("attempts = %d, want 1 when all trigger conditions hold", n)
	}
	h.maybeRecoverDegradedTurn(degradedTestLoopContext("req-4", degradedUnknownIntent()), userID, text, "req-4", webSearchOnlyTools(), "desktop", "")
	if n := len(mgr.attemptsSnapshot()); n != 1 {
		t.Fatalf("attempts = %d, want 1 — one turn re-runs at most once", n)
	}

	// A continuation run must never spawn another recovery.
	continuationReq := degradedRecoveryRequestIDPrefix + "42"
	h.maybeRecoverDegradedTurn(degradedTestLoopContext(continuationReq, degradedUnknownIntent()), userID, text, continuationReq, webSearchOnlyTools(), "desktop", "")
	if n := len(mgr.attemptsSnapshot()); n != 1 {
		t.Fatalf("attempts = %d, want 1 — a continuation run must never spawn another recovery", n)
	}

	// The req-4 attempt scheduled an async recovery on a handler without a
	// classifier; it deterministically emits the unavailable notice. Drain it
	// before returning — under full-suite CPU contention the goroutine can
	// otherwise outlive this test and emit into whichever hooks a later test
	// has installed (test-isolation leak).
	deadline := time.Now().Add(5 * time.Second)
	for hooks.noticeCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := hooks.noticeCount(); n != 1 {
		t.Fatalf("scheduled recovery notices = %d, want 1 (nil classifier → unavailable notice)", n)
	}
}

func (m *degradedTurnRecoveryManager) attemptsSnapshot() map[string]time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]time.Time, len(m.attempts))
	for k, v := range m.attempts {
		out[k] = v
	}
	return out
}

// Successful reclassification → continuation is dispatched and the recovered
// tool table contains the previously missing write tools.
func TestDegradedTurnRecoveryReclassifiesAndContinues(t *testing.T) {
	calls := 0
	uic := intent.New(intent.Config{
		LLMFunc: func(_, _ string) (string, error) {
			calls++
			return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
		},
	})
	h := &IMMessageHandler{unifiedClassifier: uic}
	hooks := &degradedRecoveryTestHooks{}
	hooks.install(t)

	userID := "user-1"
	text := "保存到知识库：驱动开发笔记"
	msg := intent.MessageContext{Text: text, UserID: userID}
	plan := degradedRecoveryPlan{
		userID:     userID,
		userText:   text,
		requestID:  "req-ok",
		platform:   "desktop",
		message:    msg,
		generation: h.degradedTurnRecovery().generationFor(userID),
		turnTools:  webSearchOnlyTools(),
	}
	h.runDegradedTurnRecovery(plan)

	if hooks.continuationCount() != 1 {
		t.Fatalf("continuations = %d, want 1; notices=%v", hooks.continuationCount(), hooks.notices)
	}
	if len(hooks.activated) != 1 || len(hooks.activated[0]) == 0 {
		t.Fatalf("activated = %v, want the missing write tools", hooks.activated)
	}
	found := false
	for _, name := range hooks.activated[0] {
		if strings.HasPrefix(name, "knowledge_save_") {
			found = true
		}
	}
	if !found {
		t.Fatalf("activated = %v, want a knowledge_save_* tool", hooks.activated[0])
	}
	// The fresh verdict must not stay swallowed by the tombstone: a successful
	// recovery lifts it and stores the result, so a resend/adoption hits the
	// cache instead of re-paying the L3 call.
	cached, ok := uic.ClassifyCached(msg)
	if !ok || cached.Degraded || cached.Primary != intent.LabelKnowledgeWrite {
		t.Fatalf("recovered scope must be cacheable again, cached=%+v ok=%v", cached, ok)
	}
	if source, ok := uic.CachedClassificationProvenance(msg); !ok || source != intent.CacheSourceTree {
		t.Fatalf("provenance = %q ok=%v, want tree for the recovered verdict", source, ok)
	}
	if calls != 1 {
		t.Fatalf("llm calls = %d, want 1 (single reclassification)", calls)
	}
	if again := uic.Classify(msg); again.Primary != intent.LabelKnowledgeWrite || calls != 1 {
		t.Fatalf("repeat classify = %+v calls=%d, want warm-cache hit without a new LLM call", again, calls)
	}
}

// A pre-landed authoritative tree verdict is adopted without a new LLM call.
func TestDegradedTurnRecoveryAdoptsPreLandedTreeVerdict(t *testing.T) {
	calls := 0
	uic := intent.New(intent.Config{
		LLMFunc: func(_, _ string) (string, error) {
			calls++
			return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
		},
	})
	msg := intent.MessageContext{Text: "保存到知识库：驱动开发笔记", UserID: "user-1"}
	if res := uic.ClassifyContext(context.Background(), msg); res.Degraded || res.Layer != 3 {
		t.Fatalf("priming classification = %+v, want layer-3 verdict", res)
	}

	h := &IMMessageHandler{unifiedClassifier: uic}
	hooks := &degradedRecoveryTestHooks{}
	hooks.install(t)
	plan := degradedRecoveryPlan{
		userID:     "user-1",
		userText:   msg.Text,
		requestID:  "req-adopt",
		platform:   "desktop",
		message:    msg,
		generation: h.degradedTurnRecovery().generationFor("user-1"),
		turnTools:  webSearchOnlyTools(),
	}
	h.runDegradedTurnRecovery(plan)

	if hooks.continuationCount() != 1 {
		t.Fatalf("continuations = %d, want 1 (pre-landed tree verdict adopted)", hooks.continuationCount())
	}
	if calls != 1 {
		t.Fatalf("llm calls = %d, want 1 — the cached tree verdict must be trusted, not re-sent", calls)
	}
}

// A newer user message while the recovery is pending invalidates it.
func TestDegradedTurnRecoveryInvalidatedByNewMessage(t *testing.T) {
	uic := intent.New(intent.Config{
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
		},
	})
	h := &IMMessageHandler{unifiedClassifier: uic}
	hooks := &degradedRecoveryTestHooks{}
	hooks.install(t)

	userID := "user-1"
	plan := degradedRecoveryPlan{
		userID:     userID,
		userText:   "保存到知识库：驱动开发笔记",
		requestID:  "req-stale",
		platform:   "desktop",
		message:    intent.MessageContext{Text: "保存到知识库：驱动开发笔记", UserID: userID},
		generation: h.degradedTurnRecovery().generationFor(userID),
		turnTools:  webSearchOnlyTools(),
	}
	h.degradedTurnRecovery().bumpGeneration(userID) // user sent a new message
	h.runDegradedTurnRecovery(plan)

	if hooks.continuationCount() != 0 {
		t.Fatalf("continuations = %d, want 0 — pending recovery must be invalidated", hooks.continuationCount())
	}
	if hooks.noticeCount() != 0 {
		t.Fatalf("notices = %d, want 0 — an invalidated recovery stays silent", hooks.noticeCount())
	}
}

// Secret-looking payloads auto-continue like any other turn now that the
// 0b-ii credential gate is live — the execution-layer gate (payload scan +
// confirmation card) is their pause point, so recovery must not block them.
func TestDegradedTurnRecoverySecretPayloadAutoContinues(t *testing.T) {
	uic := intent.New(intent.Config{
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"knowledge_write","score":0.95}]}`, nil
		},
	})
	h := &IMMessageHandler{unifiedClassifier: uic}
	hooks := &degradedRecoveryTestHooks{}
	hooks.install(t)

	userID := "user-1"
	text := "保存到知识库：服务器密码是 sunion123"
	plan := degradedRecoveryPlan{
		userID:     userID,
		userText:   text,
		requestID:  "req-secret",
		platform:   "desktop",
		message:    intent.MessageContext{Text: text, UserID: userID},
		generation: h.degradedTurnRecovery().generationFor(userID),
		turnTools:  webSearchOnlyTools(),
	}
	h.runDegradedTurnRecovery(plan)

	if hooks.continuationCount() != 1 {
		t.Fatalf("continuations = %d, want 1 — the credential gate at execution time guards the write", hooks.continuationCount())
	}
	if hooks.noticeCount() != 0 {
		t.Fatalf("notices = %v, want none — recovery itself stays silent", hooks.notices)
	}
}

// Still-degraded reclassification → explicit unavailable notice.
func TestDegradedTurnRecoveryStillDegradedNotices(t *testing.T) {
	uic := intent.New(intent.Config{
		LLMFunc: func(_, _ string) (string, error) {
			return "", errors.New("endpoint still down")
		},
	})
	h := &IMMessageHandler{unifiedClassifier: uic}
	hooks := &degradedRecoveryTestHooks{}
	hooks.install(t)

	userID := "user-1"
	text := "保存到知识库：驱动开发笔记"
	plan := degradedRecoveryPlan{
		userID:     userID,
		userText:   text,
		requestID:  "req-down",
		platform:   "desktop",
		message:    intent.MessageContext{Text: text, UserID: userID},
		generation: h.degradedTurnRecovery().generationFor(userID),
		turnTools:  webSearchOnlyTools(),
	}
	h.runDegradedTurnRecovery(plan)

	if hooks.continuationCount() != 0 {
		t.Fatalf("continuations = %d, want 0 while the endpoint stays down", hooks.continuationCount())
	}
	if hooks.noticeCount() != 1 || hooks.notices[0] != degradedRecoveryNoticeUnavailable {
		t.Fatalf("notices = %v, want the unavailable notice", hooks.notices)
	}
}

// The non-degraded verdict that does not activate the missing write tools
// (e.g. the endpoint recovered but still mislabels) must not continue.
func TestDegradedTurnRecoveryNonWriteVerdictDoesNotContinue(t *testing.T) {
	uic := intent.New(intent.Config{
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"file_download","score":0.9}]}`, nil
		},
	})
	h := &IMMessageHandler{unifiedClassifier: uic}
	hooks := &degradedRecoveryTestHooks{}
	hooks.install(t)

	userID := "user-1"
	text := "保存到知识库：驱动开发笔记"
	plan := degradedRecoveryPlan{
		userID:     userID,
		userText:   text,
		requestID:  "req-mislabel",
		platform:   "desktop",
		message:    intent.MessageContext{Text: text, UserID: userID},
		generation: h.degradedTurnRecovery().generationFor(userID),
		turnTools:  webSearchOnlyTools(),
	}
	h.runDegradedTurnRecovery(plan)

	if hooks.continuationCount() != 0 {
		t.Fatalf("continuations = %d, want 0 for a non-write verdict", hooks.continuationCount())
	}
	if hooks.noticeCount() != 1 || hooks.notices[0] != degradedRecoveryNoticeUnavailable {
		t.Fatalf("notices = %v, want the unavailable notice", hooks.notices)
	}
}

// A pending recovery is invalidated when any new message for the same user
// enters the handler pipeline.
func TestDegradedRecoveryGenerationBumpOnNewMessage(t *testing.T) {
	h := &IMMessageHandler{}
	mgr := h.degradedTurnRecovery()
	if got := mgr.generationFor("user-1"); got != 0 {
		t.Fatalf("generation = %d, want 0", got)
	}
	if got := mgr.bumpGeneration("user-1"); got != 1 {
		t.Fatalf("generation = %d, want 1", got)
	}
	if got := mgr.generationFor("user-1"); got != 1 {
		t.Fatalf("generation = %d, want 1", got)
	}
	if got := mgr.generationFor("user-2"); got != 0 {
		t.Fatalf("generation leak across users: %d", got)
	}
}

// The dedup slot expires so a later identical resend may recover again.
func TestDegradedRecoveryAttemptDedupExpiry(t *testing.T) {
	mgr := newDegradedTurnRecoveryManager()
	key := degradedRecoveryDedupKey("req-x", "保存到知识库")
	if !mgr.recordAttempt(key) {
		t.Fatal("first attempt must be recorded")
	}
	if mgr.recordAttempt(key) {
		t.Fatal("duplicate attempt within the TTL must be rejected")
	}
	mgr.mu.Lock()
	mgr.attempts[key] = time.Now().Add(-time.Second)
	mgr.mu.Unlock()
	if !mgr.recordAttempt(key) {
		t.Fatal("expired attempt slot must be reclaimable")
	}
}

// The second-generation fence inside runDegradedTurnContinuation must drop a
// stale continuation BEFORE the handler pipeline is entered: a newer user
// message bumping the generation between finishDegradedRecovery's check and
// the dispatch goroutine means the dispatch seam (which calls
// HandleIMMessageWithProgressAndStream) must never run.
func TestDegradedContinuationGenerationFenceDropsStaleDispatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), disableBackgroundEmbeddingForTest: true}
	defer func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	}()
	app.remoteSessions = NewRemoteSessionManager(app)
	app.interactionInfraDone.Store(true)
	handler := app.ensureHubClient().ensureIMHandler()
	mgr := handler.degradedTurnRecovery()

	staleGeneration := mgr.generationFor("user-1") // snapshot before the newer message
	mgr.bumpGeneration("user-1")                   // newer user message lands

	var dispatched int32
	oldDispatch := degradedContinuationDispatchFn
	degradedContinuationDispatchFn = func(_ *IMMessageHandler, _ IMUserMessage, _ func(kind, text string), _, _ string) {
		atomic.AddInt32(&dispatched, 1)
	}
	t.Cleanup(func() { degradedContinuationDispatchFn = oldDispatch })

	app.runDegradedTurnContinuation("user-1", "保存到知识库：驱动开发笔记", "desktop", "", "req-stale", staleGeneration)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&dispatched) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&dispatched); got != 0 {
		t.Fatalf("stale continuation dispatched %d times, want 0 — HandleIMMessage must not run on generation mismatch", got)
	}
}

// Generation still matching means the dispatch seam runs.
func TestDegradedContinuationGenerationFenceAllowsCurrentDispatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), disableBackgroundEmbeddingForTest: true}
	defer func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	}()
	app.remoteSessions = NewRemoteSessionManager(app)
	app.interactionInfraDone.Store(true)
	handler := app.ensureHubClient().ensureIMHandler()
	mgr := handler.degradedTurnRecovery()

	currentGeneration := mgr.generationFor("user-1") // no newer message

	var dispatched int32
	oldDispatch := degradedContinuationDispatchFn
	degradedContinuationDispatchFn = func(_ *IMMessageHandler, _ IMUserMessage, _ func(kind, text string), _, _ string) {
		atomic.AddInt32(&dispatched, 1)
	}
	t.Cleanup(func() { degradedContinuationDispatchFn = oldDispatch })

	app.runDegradedTurnContinuation("user-1", "保存到知识库：驱动开发笔记", "desktop", "", "req-current", currentGeneration)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&dispatched) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&dispatched); got != 1 {
		t.Fatalf("current continuation dispatched %d times, want 1", got)
	}
}
