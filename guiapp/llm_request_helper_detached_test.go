package guiapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// slowSimpleLLMServer returns a test server that answers every request after
// the given delay with a fixed OpenAI chat payload, counting upstream hits.
// The body is drained first, like a real LLM endpoint, so client-side
// cancellations are observed on the server immediately.
func slowSimpleLLMServer(delay time.Duration, hits *atomic.Int32, payload string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
}

const detachedTestPayload = `{"choices":[{"message":{"role":"assistant","content":"adopted-content"},"finish_reason":"stop"}]}`

func waitForDetachedCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// resetDetachedSimpleLLMReadsForTest clears the detached-read registry. The
// completed-entry retention window outlives a test, and httptest servers
// reuse ports within a process — without the reset, a later test can adopt a
// retained entry keyed by a recycled host:port and see phantom successes.
func resetDetachedSimpleLLMReadsForTest(t *testing.T) {
	t.Helper()
	detachedSimpleLLMReads.Range(func(k, _ interface{}) bool {
		detachedSimpleLLMReads.Delete(k)
		return true
	})
}

// TestDetachedReadAdoptedWithinGrace models the 30s-budget / >35s-latency
// acceptance shape at unit scale: the endpoint answers after the scheduling
// budget fired but inside the grace window (min(2×budget, 60s, keepalive)).
// The retry (late-verdict re-run) must adopt the in-flight detached read —
// the upstream request count stays 1.
func TestDetachedReadAdoptedWithinGrace(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	// budget 200ms → grace 400ms; latency 350ms lands inside the window.
	srv := slowSimpleLLMServer(350*time.Millisecond, &hits, detachedTestPayload)
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	_, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 200*time.Millisecond)
	if err == nil {
		t.Fatal("want budget error from the slow endpoint")
	}
	if !isLLMBudgetFiredError(err) {
		t.Fatalf("err = %v, want llmBudgetFiredError", err)
	}

	resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 200*time.Millisecond)
	if err != nil {
		t.Fatalf("retry should adopt the detached read: %v", err)
	}
	if resp.Content != "adopted-content" {
		t.Fatalf("content = %q, want adopted-content", resp.Content)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1 (no double-send)", got)
	}
}

// TestDetachedReadBeyondGraceFallsBackToFreshRequest: an endpoint slower than
// the grace window cannot be adopted; the retry falls through to a genuine
// re-send (the legacy late-verdict path) instead of hanging forever.
func TestDetachedReadBeyondGraceFallsBackToFreshRequest(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	// budget 100ms → grace 200ms; latency 600ms outlives the window.
	srv := slowSimpleLLMServer(600*time.Millisecond, &hits, detachedTestPayload)
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	_, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 100*time.Millisecond)
	if !isLLMBudgetFiredError(err) {
		t.Fatalf("err = %v, want llmBudgetFiredError", err)
	}

	resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("retry past grace should send a fresh request: %v", err)
	}
	if resp.Content != "adopted-content" {
		t.Fatalf("content = %q, want adopted-content", resp.Content)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream hits = %d, want 2 (grace expired → fresh request)", got)
	}
}

// TestDetachedReadCompletedEntryRetainedForLateAdoption: a duplicate that
// arrives AFTER the detached read finished (response already landed, entry
// still within its completed-retention window) must adopt the completed
// result — not pay a second upstream request. This closes the window where
// a late-verdict goroutine delayed by scheduler load past the response
// arrival would otherwise double-send.
func TestDetachedReadCompletedEntryRetainedForLateAdoption(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	// budget 200ms → grace 400ms; latency 300ms completes the read at ~300ms,
	// inside the grace window.
	srv := slowSimpleLLMServer(300*time.Millisecond, &hits, detachedTestPayload)
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	if _, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 200*time.Millisecond); !isLLMBudgetFiredError(err) {
		t.Fatalf("first call err = %v, want llmBudgetFiredError", err)
	}
	// The response lands at ~300ms and the entry enters its completed-
	// retention window; arriving well after completion must still adopt.
	time.Sleep(700 * time.Millisecond)

	resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("late duplicate should adopt the completed read: %v", err)
	}
	if resp.Content != "adopted-content" {
		t.Fatalf("content = %q, want adopted-content", resp.Content)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1 (completed-entry retention, no double-send)", got)
	}
}

// TestDetachedReadAbortsWhenCallerContextCancelled: user cancellation during
// the detached window must tear the connection down (the detached read
// derives from the caller's parent context, not the dead budget context).
func TestDetachedReadAbortsWhenCallerContextCancelled(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	aborted := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
			aborted <- struct{}{}
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	parent, cancel := context.WithCancel(context.Background())
	_, err := doSimpleLLMRequestWithOptions(parent, cfg, msgs, srv.Client(), 100*time.Millisecond, simpleLLMRequestOptions{})
	if !isLLMBudgetFiredError(err) {
		t.Fatalf("err = %v, want llmBudgetFiredError", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}

	// User cancels while the detached read is still in flight.
	cancel()
	select {
	case <-aborted:
	case <-time.After(2 * time.Second):
		t.Fatal("detached read must abort when the caller context is cancelled")
	}

	// The registry entry must resolve promptly so later calls send fresh;
	// removal follows after the (test-shortened) retention window.
	// Resolution is the semantic assertion — deletion timing is retention
	// policy — so the resolution wait uses the standard window while the
	// deletion wait gets a generous deadline for loaded CI machines.
	key := detachedSimpleLLMReadKey(cfg, msgs, false)
	waitForDetachedCondition(t, "registry entry resolution", func() bool {
		v, ok := detachedSimpleLLMReads.Load(key)
		if !ok {
			return true // already removed
		}
		entry := v.(*detachedSimpleLLMRead)
		select {
		case <-entry.done:
			return true
		default:
			return false
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, ok := detachedSimpleLLMReads.Load(key); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("registry entry not removed after resolution + retention window")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestDetachedReadReleasesForegroundSlot: once the budget fires, the detached
// read must not keep holding its scheduler foreground slot.
func TestDetachedReadReleasesForegroundSlot(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	// budget 100ms → grace 200ms; latency 180ms resolves inside the window.
	srv := slowSimpleLLMServer(180*time.Millisecond, &hits, detachedTestPayload)
	defer srv.Close()

	schedulerActiveFG := func() int {
		globalLLMScheduler.mu.Lock()
		defer globalLLMScheduler.mu.Unlock()
		return globalLLMScheduler.activeFG
	}
	baseline := schedulerActiveFG()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	_, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 100*time.Millisecond)
	if !isLLMBudgetFiredError(err) {
		t.Fatalf("err = %v, want llmBudgetFiredError", err)
	}
	waitForDetachedCondition(t, "foreground slot release", func() bool {
		return schedulerActiveFG() <= baseline
	})
}

// TestDetachedReadSuccessClearsEndpointGate: a detached read that completes in
// the background feeds the endpoint gate a positive health signal. Via the
// production buildUICLLMContextFunc wiring, a sibling lightweight ban on the
// same endpoint prefix is shortened to the 2s grace window; a direct call
// clears the uic-tree category's own ban.
func TestDetachedReadSuccessClearsEndpointGate(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	// budget 100ms → grace 200ms; latency 150ms resolves inside the window.
	srv := slowSimpleLLMServer(150*time.Millisecond, &hits, detachedTestPayload)
	defer srv.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "test", Name: "Test", URL: srv.URL, Key: "test-key", Model: "test-model",
		}},
		MaclawLLMCurrentProvider: "Test",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cfg := app.GetMaclawLLMConfig()
	dialErr := fmt.Errorf("dial tcp 127.0.0.1:1: connectex: connection refused")

	// Direct unit: the positive-health signal clears the category's own ban.
	app.observeLLMEndpointResult(cfg, llmEndpointCategoryUICTree, false, dialErr)
	if _, skip := app.shouldSkipLightweightLLM(cfg, llmEndpointCategoryUICTree); !skip {
		t.Fatal("precondition: uic-tree ban must be active")
	}
	app.observeLLMEndpointLightweightSuccess(cfg, llmEndpointCategoryUICTree)
	if _, skip := app.shouldSkipLightweightLLM(cfg, llmEndpointCategoryUICTree); skip {
		t.Fatal("detached success must clear the uic-tree ban")
	}

	// End-to-end via the production callback wiring: while a sibling
	// lightweight ban is live, a detached uic-tree success shortens it to the
	// 2s grace window (the uic-tree category itself is not banned, so the
	// callback proceeds).
	app.observeLLMEndpointResult(cfg, llmEndpointCategoryLightweightClassify, false, dialErr)
	fn := app.buildUICLLMContextFunc()
	budgetCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := fn(budgetCtx, context.Background(), "classify", "北京天气，输出格式化PDF报告"); !isLLMBudgetFiredError(err) {
		t.Fatalf("err = %v, want llmBudgetFiredError", err)
	}

	waitForDetachedCondition(t, "sibling lightweight ban shortened by detached success", func() bool {
		gate := app.getLLMEndpointFailureGate()
		gate.mu.Lock()
		defer gate.mu.Unlock()
		entry, ok := gate.entries[llmEndpointFailureKey(cfg, llmEndpointCategoryLightweightClassify)]
		if !ok {
			return true // already expired/cleared
		}
		return time.Until(entry.expiresAt) <= 5*time.Second
	})
}

// TestDetachedUICFusionAdoptsSingleUpstreamRequest is the end-to-end P0-3
// acceptance: a slow endpoint (latency > fusion budget, < grace window)
// degrades the live turn, the late-verdict re-run adopts the detached read,
// and the Layer-3 verdict lands in cache with upstream request count == 1.
func TestDetachedUICFusionAdoptsSingleUpstreamRequest(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	// fusion budget 200ms; helper budget 30s → grace 60s; latency 400ms.
	srv := slowSimpleLLMServer(400*time.Millisecond, &hits, `{"choices":[{"message":{"role":"assistant","content":"{\"top\":[{\"skill\":\"live_data\",\"score\":0.95,\"workflow_type\":\"\"}]}"},"finish_reason":"stop"}]}`)
	defer srv.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "test", Name: "Test", URL: srv.URL, Key: "test-key", Model: "test-model",
		}},
		MaclawLLMCurrentProvider: "Test",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	uic := intent.New(intent.Config{
		Embedder:       &detachedTestEmbedder{vec: []float32{1, 0}},
		LLMContextFunc: app.buildUICLLMContextFunc(),
	})
	uic.SetFusionTreeDeadline(200 * time.Millisecond)
	waitForDetachedCondition(t, "anchor warmup", uic.Ready)

	msg := intent.MessageContext{UserID: "u-1", Text: "请帮我查询明天的航班"}
	result := uic.ClassifyContext(context.Background(), msg)
	if !result.Degraded {
		t.Fatalf("live turn must degrade on fusion timeout: %+v", result)
	}

	waitForDetachedCondition(t, "late verdict adoption", func() bool {
		cached, ok := uic.ClassifyCached(msg)
		return ok && cached.Layer == 3 && !cached.Degraded
	})
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1 (detached adoption, no double-send)", got)
	}
}

// TestWSVariantDoesNotDetach: the Responses-WebSocket variant keeps the legacy
// to-point-of-budget behaviour — no detached read is registered and the
// request is torn down at the budget.
func TestWSVariantDoesNotDetach(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	srv := slowSimpleLLMServer(2*time.Second, &hits, `{}`)
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", WireAPI: "responses-ws"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	start := time.Now()
	_, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 100*time.Millisecond)
	if err == nil {
		t.Fatal("want error from the slow WS endpoint")
	}
	// Regression guard (2026-08-25 incident pattern): the budget fire must
	// surface as llmBudgetFiredError, never a bare deadline-exceeded that the
	// endpoint gate could misclassify as a network failure.
	if !isLLMBudgetFiredError(err) {
		t.Fatalf("err = %v, want llmBudgetFiredError", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("WS variant waited %s, want teardown at the 100ms budget", elapsed)
	}
	if _, ok := detachedSimpleLLMReads.Load(detachedSimpleLLMReadKey(cfg, msgs, false)); ok {
		t.Fatal("WS variant must not register a detached read")
	}
}

// TestDetachedReadConcurrentDuplicateAdoptsAndRespectsCallerCtx covers the
// LoadOrStore loaded branch: three concurrent identical requests with
// staggered budgets, started behind a gate after the detacher's request is
// already in flight (it is deterministically server hit #1). The first
// budget fires first (the detacher, handed the budget-fired error and owning
// the registry entry); the later budgets take the loaded branch and wait on
// that entry. The waiter that is still live adopts the successful detached
// read (delivering the positive health signal); the waiter whose own ctx is
// cancelled returns ctx.Err() promptly instead of blocking on the existing
// entry's grace window.
func TestDetachedReadConcurrentDuplicateAdoptsAndRespectsCallerCtx(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	// Every request answers after 500ms, so all three callers miss their
	// budgets regardless of arrival order and reach the detach point; the
	// detacher's entry (budget 200ms → grace 400ms → would end at ~600ms)
	// resolves with the successful body at ~500ms, comfortably inside the
	// grace window. Margins: cancel lands ~350ms, adoption lands ~500ms
	// (≥150ms apart) so CI jitter cannot flip the outcome roles.
	srv := slowSimpleLLMServer(500*time.Millisecond, &hits, detachedTestPayload)
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	type outcome struct {
		resp *llmSimpleResponse
		err  error
	}
	results := make(chan outcome, 3)

	// The detacher gets a head start so its request is deterministically the
	// slow hit #1; the two waiters start together behind the gate.
	go func() {
		resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 200*time.Millisecond)
		results <- outcome{resp, err}
	}()
	time.Sleep(50 * time.Millisecond)
	gate := make(chan struct{})
	go func() {
		<-gate
		resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 250*time.Millisecond)
		results <- outcome{resp, err}
	}()
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-gate
		resp, err := doSimpleLLMRequest(cancelCtx, cfg, msgs, srv.Client(), 250*time.Millisecond)
		results <- outcome{resp, err}
	}()
	close(gate)
	// Cancel the third caller while it waits on the existing entry: well after
	// its own budget fired (~300ms) and well before the entry resolves with
	// the successful body (~500ms) — ≥150ms margin on both sides.
	time.Sleep(300 * time.Millisecond)
	cancel()

	var budgetFired, adopted, cancelled int
	for i := 0; i < 3; i++ {
		select {
		case o := <-results:
			switch {
			case isLLMBudgetFiredError(o.err):
				budgetFired++
			case errors.Is(o.err, context.Canceled):
				cancelled++
			case o.err == nil && o.resp != nil && o.resp.Content == "adopted-content":
				adopted++
			default:
				t.Fatalf("unexpected outcome: resp=%v err=%v", o.resp, o.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for concurrent outcomes")
		}
	}
	if budgetFired != 1 || adopted != 1 || cancelled != 1 {
		t.Fatalf("outcomes = budgetFired:%d adopted:%d cancelled:%d, want 1/1/1", budgetFired, adopted, cancelled)
	}
	// All three callers sent their own foreground request before any of them
	// detached — the registry only deduplicates the sequential
	// retry-adoption path (upstream count 1 there), not concurrent sends.
	if got := hits.Load(); got != 3 {
		t.Fatalf("upstream hits = %d, want 3 (three concurrent foreground sends)", got)
	}
}

// TestDetachedReadLoadedBranchFallsThroughOnExistingFailure: when the entry
// the loaded branch waits on fails (its grace expired), the loser must fall
// through to a fresh request and succeed — the P0-3 rescue semantics — while
// a loser whose own ctx is cancelled still returns ctx.Err() promptly.
func TestDetachedReadLoadedBranchFallsThroughOnExistingFailure(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if n == 1 {
			// The detacher's request: outlive the grace window (budget 150ms →
			// grace 300ms → ends at 450ms) so its entry resolves with an error.
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		if n <= 3 {
			// The waiters' duplicate reads: slow enough that their budgets
			// fire before a response arrives.
			select {
			case <-r.Context().Done():
				return
			case <-time.After(400 * time.Millisecond):
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(detachedTestPayload))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	type outcome struct {
		name string
		resp *llmSimpleResponse
		err  error
	}
	results := make(chan outcome, 3)

	// Detacher first (deterministic hit #1), then the two waiters behind the
	// gate with staggered budgets.
	go func() {
		resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 150*time.Millisecond)
		results <- outcome{"detacher", resp, err}
	}()
	time.Sleep(50 * time.Millisecond)
	gate := make(chan struct{})
	go func() {
		<-gate
		resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 250*time.Millisecond)
		results <- outcome{"waiter", resp, err}
	}()
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-gate
		resp, err := doSimpleLLMRequest(cancelCtx, cfg, msgs, srv.Client(), 300*time.Millisecond)
		results <- outcome{"cancelled-waiter", resp, err}
	}()
	close(gate)
	// Cancel the second waiter while it waits on the existing entry: after
	// its own budget fired (~350ms) and a full 100ms before the entry's grace
	// expires (~450ms), so CI jitter cannot flip the cancelled/fall-through
	// roles.
	time.Sleep(300 * time.Millisecond)
	cancel()

	outcomes := map[string]error{}
	for i := 0; i < 3; i++ {
		select {
		case o := <-results:
			outcomes[o.name] = o.err
			if o.name == "waiter" && (o.err != nil || o.resp == nil || o.resp.Content != "adopted-content") {
				t.Fatalf("waiter outcome = resp:%v err:%v, want adopted fresh-request success", o.resp, o.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for concurrent outcomes")
		}
	}
	if !isLLMBudgetFiredError(outcomes["detacher"]) {
		t.Fatalf("detacher err = %v, want llmBudgetFiredError", outcomes["detacher"])
	}
	if !errors.Is(outcomes["cancelled-waiter"], context.Canceled) {
		t.Fatalf("cancelled-waiter err = %v, want context.Canceled", outcomes["cancelled-waiter"])
	}
	// Three concurrent foreground sends plus the waiter's fall-through fresh
	// request; the cancelled waiter never re-sends.
	if got := hits.Load(); got != 4 {
		t.Fatalf("upstream hits = %d, want 4 (3 concurrent sends + 1 fall-through)", got)
	}
}

// TestDetachedReadGraceExpiryFallsThroughToFreshRequest: when the existing
// entry's grace window expires first, the waiter must fall through to a fresh
// request (rescue semantics) instead of propagating the grace-expired error —
// the late-verdict goroutine would otherwise exit without re-sending.
func TestDetachedReadGraceExpiryFallsThroughToFreshRequest(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if n == 1 {
			// First request: outlive the grace window (budget 100ms → grace
			// 200ms) so the detached entry resolves with an error.
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(detachedTestPayload))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	msgs := []interface{}{map[string]string{"role": "user", "content": "classify this"}}

	// First call detaches; its entry's grace expires while still registered.
	_, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 100*time.Millisecond)
	if !isLLMBudgetFiredError(err) {
		t.Fatalf("err = %v, want llmBudgetFiredError", err)
	}

	// Second call arrives while the entry is still registered: it waits,
	// observes the grace-expired failure, and falls through to a fresh
	// request instead of propagating the error.
	resp, err := doSimpleLLMRequest(context.Background(), cfg, msgs, srv.Client(), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("waiter must fall through to a fresh request after grace expiry: %v", err)
	}
	if resp.Content != "adopted-content" {
		t.Fatalf("content = %q, want adopted-content", resp.Content)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream hits = %d, want 2 (hung first request + fresh fall-through send)", got)
	}
}

// detachedTestEmbedder is a minimal non-noop embedder whose identical vectors
// make every label equally (un)confident, forcing L2 ambiguity → L3
// escalation without short-lookup policy skips.
type detachedTestEmbedder struct{ vec []float32 }

func (e *detachedTestEmbedder) Embed(text string) ([]float32, error) { return e.vec, nil }
func (e *detachedTestEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = e.vec
	}
	return out, nil
}
func (e *detachedTestEmbedder) Dim() int          { return len(e.vec) }
func (e *detachedTestEmbedder) Close()            {}
func (e *detachedTestEmbedder) ModelPath() string { return "" }

var _ embedding.Embedder = (*detachedTestEmbedder)(nil)
