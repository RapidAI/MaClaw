package guiapp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestLLMConcurrencySchedulerClassifiesBackgroundCallers(t *testing.T) {
	for _, trace := range []llm.RequestTrace{
		{Caller: "memory-maintenance"},
		{Caller: "session-start-extraction"},
		{Caller: "post-conversation-llm", OwnerID: "desktop-user"},
		{Caller: "pending-reply-prompt", OwnerID: "desktop-user"},
		{Caller: "experience-extraction"},
		{Caller: "knowledge-card"},
		{Caller: "gossip-auto"},
		{Caller: "vision-probe"},
		{Caller: "provider-test"},
		{Caller: "unified-intent-classifier-late"},
	} {
		if got := classifyLLMRequestPriority(trace); got != llmPriorityBackground {
			t.Fatalf("classifyLLMRequestPriority(%+v) = %s, want background", trace, got)
		}
	}
	if got := classifyLLMRequestPriority(llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user"}); got != llmPriorityForeground {
		t.Fatalf("agent_loop priority = %s, want foreground", got)
	}
	if got := classifyLLMRequestPriority(llm.RequestTrace{Caller: "simple_llm"}); got != llmPriorityForeground {
		t.Fatalf("untraced simple_llm priority = %s, want foreground", got)
	}
	if got := classifyLLMRequestPriority(llm.RequestTrace{Caller: "pending-reply-answer-fast", OwnerID: "desktop-user"}); got != llmPriorityForeground {
		t.Fatalf("fast pending-reply answer priority = %s, want foreground", got)
	}
	if got := classifyLLMRequestPriority(llm.RequestTrace{Caller: "confirmation-intent-fast", OwnerID: "desktop-user"}); got != llmPriorityForeground {
		t.Fatalf("fast confirmation intent priority = %s, want foreground", got)
	}
	if got := classifyLLMRequestPriority(llm.RequestTrace{Caller: "unified-intent-classifier"}); got != llmPriorityForeground {
		t.Fatalf("synchronous intent tree priority = %s, want foreground", got)
	}
}

func TestWaitLateTreeForForegroundIdleWaitsOutTurn(t *testing.T) {
	var busy atomic.Bool
	done := make(chan struct{})
	go func() {
		waitLateTreeForForegroundIdle(busy.Load, nil, 300*time.Millisecond, time.Second, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	busy.Store(true)
	select {
	case <-done:
		t.Fatal("late tree started while the foreground turn was active")
	case <-time.After(40 * time.Millisecond):
	}
	busy.Store(false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("late tree did not start after the foreground turn ended")
	}
}

func TestWaitLateTreeForForegroundIdleStartsWhenNoTurnAppears(t *testing.T) {
	start := time.Now()
	waitLateTreeForForegroundIdle(func() bool { return false }, nil, 40*time.Millisecond, time.Second, 5*time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 20*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("no-turn wait took %s", elapsed)
	}
}

func TestWaitLateTreeForForegroundIdleHoldsPastAppearDeadline(t *testing.T) {
	var busy atomic.Bool
	busy.Store(true)
	done := make(chan struct{})
	go func() {
		waitLateTreeForForegroundIdle(busy.Load, nil, 30*time.Millisecond, 500*time.Millisecond, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(80 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("released at the appear deadline while the turn was still active")
	default:
	}
	busy.Store(false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not release after the turn ended")
	}
}

func TestWaitLateTreeForForegroundIdleSurvivesGapAfterOtherTurn(t *testing.T) {
	// 0: some other turn is already on the lane. 1: quiet gap. 2: this turn.
	var phase atomic.Int32
	done := make(chan struct{})
	go func() {
		waitLateTreeForForegroundIdle(func() bool {
			switch phase.Load() {
			case 0, 2:
				return true
			default:
				return false
			}
		}, nil, 300*time.Millisecond, 2*time.Second, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	phase.Store(1)
	time.Sleep(40 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("late tree started in the gap before this turn claimed the lane")
	default:
	}
	phase.Store(2)
	time.Sleep(40 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("late tree started while this turn still held the lane")
	default:
	}
	phase.Store(3)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("late tree did not start after this turn released the lane")
	}
}

func TestWaitLateTreeForForegroundIdleCapsWhileTurnStaysBusy(t *testing.T) {
	start := time.Now()
	waitLateTreeForForegroundIdle(func() bool { return true }, nil, time.Second, 50*time.Millisecond, 5*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("capped wait took %s", elapsed)
	}
}

func TestWaitLateTreeForForegroundIdleIgnoresClassifierHTTP(t *testing.T) {
	// Production order: the late tree is scheduled inside the first
	// ClassifyContext, the lane is idle, then a task-context ClassifyContext
	// holds a foreground slot longer than the appear window, and only after
	// that HTTP ends does the agent loop claim foreground work.
	var loop atomic.Bool
	var http atomic.Bool
	done := make(chan struct{})
	go func() {
		waitLateTreeForForegroundIdle(loop.Load, http.Load, 80*time.Millisecond, 2*time.Second, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(25 * time.Millisecond)
	http.Store(true)
	time.Sleep(120 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("appear window expired during classifier HTTP")
	default:
	}
	http.Store(false)
	time.Sleep(25 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("late tree started in the gap after classifier HTTP and before the agent loop")
	default:
	}
	loop.Store(true)
	time.Sleep(40 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("late tree started while the agent loop held the lane")
	default:
	}
	// Raise HTTP before dropping the loop so one poll cannot see both idle.
	http.Store(true)
	loop.Store(false)
	time.Sleep(30 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("late tree started while a foreground HTTP was still in flight")
	default:
	}
	http.Store(false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("late tree did not start after the loop and classifier HTTP were both idle")
	}
}

func TestWaitLateTreeForForegroundIdleAppearSlidesPastClassifierHTTP(t *testing.T) {
	var http atomic.Bool
	http.Store(true)
	done := make(chan struct{})
	go func() {
		waitLateTreeForForegroundIdle(func() bool { return false }, http.Load, 50*time.Millisecond, time.Second, 5*time.Millisecond)
		close(done)
	}()
	time.Sleep(120 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("appear window expired during classifier HTTP")
	default:
	}
	cleared := time.Now()
	http.Store(false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not start after classifier HTTP with no agent loop")
	}
	if waited := time.Since(cleared); waited < 30*time.Millisecond {
		t.Fatalf("started %s after classifier HTTP; want a fresh appear window", waited)
	}
}

func TestForegroundLaneBusyCoversLoopGapAndQueuedForeground(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	var work atomic.Int64
	s.foregroundWork = func() int64 { return work.Load() }
	if s.foregroundLaneBusy() {
		t.Fatal("idle lane reported busy")
	}
	work.Store(1)
	if !s.foregroundLaneBusy() || !s.foregroundLoopBusy() || s.foregroundHTTPBusy() {
		t.Fatal("agent loop between model rounds must keep the lane busy without an HTTP lease")
	}
	work.Store(0)
	if s.foregroundLaneBusy() || s.foregroundLoopBusy() || s.foregroundHTTPBusy() {
		t.Fatal("lane stayed busy after the agent loop finished")
	}

	lease, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user"})
	if err != nil {
		t.Fatalf("acquire foreground: %v", err)
	}
	if !s.foregroundLaneBusy() || s.foregroundLoopBusy() || !s.foregroundHTTPBusy() {
		t.Fatal("in-flight foreground request must keep the lane busy without an agent loop")
	}
	lease.Release()
	if s.foregroundLaneBusy() || s.foregroundLoopBusy() || s.foregroundHTTPBusy() {
		t.Fatal("lane stayed busy after the foreground lease was released")
	}

	// A foreground waiter can be visible before dispatch grants it. The late
	// tree must treat that waiter as the user turn, or it acquires a slot and
	// the grant cancels it.
	s.mu.Lock()
	s.queue = append(s.queue, &llmSchedulerWaiter{priority: llmPriorityForeground})
	s.mu.Unlock()
	if !s.foregroundLaneBusy() || s.foregroundLoopBusy() || !s.foregroundHTTPBusy() {
		t.Fatal("queued foreground request must keep the lane busy without an agent loop")
	}
	s.mu.Lock()
	s.queue = nil
	s.mu.Unlock()
	if s.foregroundLaneBusy() || s.foregroundLoopBusy() || s.foregroundHTTPBusy() {
		t.Fatal("lane stayed busy after the queued foreground request was dropped")
	}
}

func TestLLMConcurrencySchedulerDoesNotSelfBlockUntracedSimpleLLM(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 1 }
	lease, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "simple_llm"})
	if err != nil {
		t.Fatalf("untraced simple_llm should not wait for foreground idle: %v", err)
	}
	lease.Release()
}

func TestLLMConcurrencySchedulerBlocksBackgroundWhileForegroundActive(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	fg, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user"})
	if err != nil {
		t.Fatalf("acquire foreground: %v", err)
	}
	defer fg.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "memory-maintenance"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("background acquire err = %v, want deadline", err)
	}
}

func TestLLMConcurrencySchedulerIsForegroundDegradedOnlyOnThrottleErrors(t *testing.T) {
	// 429/overloaded → fg degraded
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	if s.IsForegroundDegraded() {
		t.Fatal("should not be degraded initially")
	}
	s.ObserveResult(llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-a"}, errors.New("HTTP 429 too many requests"))
	if !s.IsForegroundDegraded() {
		t.Fatal("should be fg-degraded after 429 on foreground caller")
	}

	// 503 on background caller → only bg paused, NOT fg degraded
	s2 := newLLMConcurrencyScheduler()
	s2.foregroundWork = func() int64 { return 0 }
	s2.ObserveResult(llm.RequestTrace{Caller: "memory-maintenance"}, errors.New("HTTP 503 service unavailable"))
	if s2.IsForegroundDegraded() {
		t.Fatal("bg-only 503 should not cause fg degraded mode")
	}
}

func TestLLMConcurrencySchedulerBackgroundRunsWhenForegroundAgentIdleNoLLMActive(t *testing.T) {
	// foregroundWork > 0 (a tab has an active agent loop) but no foreground LLM
	// request is in-flight. Background LLM should be dispatched immediately.
	// Previously this was incorrectly blocked; the agent-loop activity counter
	// must not gate LLM slot dispatch — only in-flight LLM slot usage matters.
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 1 } // agent loop active, but no LLM in-flight
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	lease, err := s.Acquire(ctx, llm.RequestTrace{Caller: "memory-maintenance"})
	if err != nil {
		t.Fatalf("background LLM should not be blocked by foreground agent work alone: %v", err)
	}
	lease.Release()
}

func TestLLMConcurrencySchedulerDegradedLimitsForegroundToOne(t *testing.T) {
	// Under 429 pressure, foreground is capped at degradedForegroundLLMConcurrency (2).
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	s.ObserveResult(llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user"}, errors.New("HTTP 429 too many requests"))

	leases := make([]*llmSchedulerLease, degradedForegroundLLMConcurrency)
	for i := range leases {
		lease, err := s.Acquire(context.Background(), llm.RequestTrace{
			Caller:  "agent_loop",
			OwnerID: "desktop-user-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatalf("foreground acquire %d: %v", i, err)
		}
		leases[i] = lease
	}
	defer func() {
		for _, lease := range leases {
			lease.Release()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user:overflow"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overflow foreground acquire err = %v, want deadline at degraded cap %d", err, degradedForegroundLLMConcurrency)
	}
}

func TestLLMConcurrencySchedulerBackgroundPressurePausesOnlyBackground(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	s.ObserveResult(llm.RequestTrace{Caller: "memory-maintenance"}, errors.New("HTTP 503 service unavailable"))

	first, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-a"})
	if err != nil {
		t.Fatalf("first foreground acquire: %v", err)
	}
	defer first.Release()
	second, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-b"})
	if err != nil {
		t.Fatalf("second foreground acquire should not be limited by background pressure: %v", err)
	}
	defer second.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "memory-maintenance"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("background acquire err = %v, want deadline while background paused", err)
	}
}

func TestLLMConcurrencySchedulerForegroundPressureStillLimitsForeground(t *testing.T) {
	// 429 on a foreground caller degrades fg slots to degradedForegroundLLMConcurrency.
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	s.ObserveResult(llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-a"}, errors.New("HTTP 429 too many requests"))

	leases := make([]*llmSchedulerLease, degradedForegroundLLMConcurrency)
	for i := range leases {
		lease, err := s.Acquire(context.Background(), llm.RequestTrace{
			Caller:  "agent_loop",
			OwnerID: "owner-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatalf("foreground acquire %d: %v", i, err)
		}
		leases[i] = lease
	}
	defer func() {
		for _, lease := range leases {
			lease.Release()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-overflow"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overflow foreground acquire err = %v, want deadline at degraded cap %d", err, degradedForegroundLLMConcurrency)
	}
}

func TestLLMConcurrencySchedulerForegroundServiceUnavailablePausesOnlyBackground(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	s.ObserveResult(llm.RequestTrace{Caller: "task-context", OwnerID: "owner-a"}, errors.New("HTTP 503 service unavailable"))

	first, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-a"})
	if err != nil {
		t.Fatalf("first foreground acquire: %v", err)
	}
	defer first.Release()
	second, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-b"})
	if err != nil {
		t.Fatalf("second foreground acquire should not be limited by HTTP 503: %v", err)
	}
	defer second.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "memory-maintenance"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("background acquire err = %v, want deadline while background paused", err)
	}
}

func TestProviderPressureDoesNotTreatLocalDeadlineAsCapacityPressure(t *testing.T) {
	if isProviderPressureLLMError(context.DeadlineExceeded) {
		t.Fatal("context deadline should not force global degraded concurrency")
	}
	if isProviderPressureLLMError(errors.New("Post https://llm.test: context deadline exceeded")) {
		t.Fatal("deadline text should not force global degraded concurrency")
	}
	for _, err := range []error{
		errors.New("HTTP 429: too many requests"),
		errors.New("HTTP 503: service unavailable"),
		errors.New("server is overloaded"),
	} {
		if !isProviderPressureLLMError(err) {
			t.Fatalf("%v should be provider pressure", err)
		}
	}
}

func TestForegroundThrottleErrorClassification(t *testing.T) {
	if !isForegroundLLMThrottleError(errors.New("HTTP 429: too many requests")) {
		t.Fatal("HTTP 429 should throttle foreground concurrency")
	}
	if !isForegroundLLMThrottleError(errors.New("server overloaded")) {
		t.Fatal("overloaded should throttle foreground concurrency")
	}
	if isForegroundLLMThrottleError(errors.New("HTTP 503: service unavailable")) {
		t.Fatal("HTTP 503 should pause background only, not serialize foreground agents")
	}
	if isForegroundLLMThrottleError(errors.New("HTTP 504: gateway timeout")) {
		t.Fatal("HTTP 504 should pause background only, not serialize foreground agents")
	}
}

func TestLLMConcurrencySchedulerQueuedForegroundBeatsBackground(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	first, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-a"})
	if err != nil {
		t.Fatalf("first foreground acquire: %v", err)
	}
	second, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-b"})
	if err != nil {
		t.Fatalf("second foreground acquire: %v", err)
	}

	bgReady := make(chan struct{})
	bgRelease := make(chan func(), 1)
	go func() {
		lease, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "memory-maintenance"})
		if err == nil {
			bgRelease <- lease.Release
			close(bgReady)
		}
	}()
	fgReady := make(chan struct{})
	fgRelease := make(chan func(), 1)
	go func() {
		lease, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-c"})
		if err == nil {
			fgRelease <- lease.Release
			close(fgReady)
		}
	}()

	first.Release()
	select {
	case <-fgReady:
	case <-time.After(time.Second):
		t.Fatal("queued foreground was not dispatched")
	}
	select {
	case <-bgReady:
		t.Fatal("background dispatched while foreground active/queued")
	default:
	}
	(<-fgRelease)()
	second.Release()
	select {
	case <-bgReady:
		(<-bgRelease)()
	case <-time.After(time.Second):
		t.Fatal("background did not dispatch after foreground drained")
	}
}

func TestLLMConcurrencySchedulerFourForegroundTabsCanRunConcurrently(t *testing.T) {
	// defaultForegroundLLMConcurrency covers multiple desktop tabs (currently 6).
	// Fill every slot, then the next request must wait.
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }

	leases := make([]*llmSchedulerLease, defaultForegroundLLMConcurrency)
	for i := range leases {
		lease, err := s.Acquire(context.Background(), llm.RequestTrace{
			Caller:  "agent_loop",
			OwnerID: "tab-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatalf("tab-%d acquire err = %v, all %d slots should acquire immediately", i, err, defaultForegroundLLMConcurrency)
		}
		leases[i] = lease
	}

	ctxOverflow, cancelOverflow := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelOverflow()
	if _, err := s.Acquire(ctxOverflow, llm.RequestTrace{Caller: "agent_loop", OwnerID: "tab-overflow"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overflow foreground acquire should block when all %d slots are occupied", defaultForegroundLLMConcurrency)
	}

	for _, l := range leases {
		l.Release()
	}
}

func TestLLMConcurrencySchedulerBackgroundDispatchedWhenNoFGInFlight(t *testing.T) {
	// Core new behavior: BG runs when activeFG == 0, regardless of foregroundWork.
	// This is the scenario that was previously broken: agent loop active in tab
	// but between LLM calls (no in-flight FG LLM).
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 2 } // two agent loops active, no LLM in-flight

	bgGot := make(chan struct{})
	go func() {
		lease, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "memory-maintenance"})
		if err == nil {
			close(bgGot)
			lease.Release()
		}
	}()

	select {
	case <-bgGot:
		// correct: BG runs even though foregroundWork > 0
	case <-time.After(200 * time.Millisecond):
		t.Fatal("background LLM was blocked by agent-loop activity counter (no FG LLM in-flight)")
	}
}

func TestLLMConcurrencySchedulerBGPreemptedWhenFGEnqueuesAfterBGDispatched(t *testing.T) {
	// BG gets dispatched (activeFG==0, no queued FG). Then a FG request arrives
	// and preempts the active BG via cancel. After BG releases, FG gets the slot.
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }

	bg, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "memory-maintenance"})
	if err != nil {
		t.Fatalf("bg acquire: %v", err)
	}
	bgCancelled := make(chan struct{}, 1)
	bg.SetCancel(func() { close(bgCancelled) })

	// FG arrives after BG is active — should preempt BG.
	fgReady := make(chan *llmSchedulerLease, 1)
	go func() {
		l, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "tab-a"})
		if err == nil {
			fgReady <- l
		}
	}()

	select {
	case <-bgCancelled:
	case <-time.After(time.Second):
		t.Fatal("FG did not preempt active BG")
	}

	// BG releases its slot (cancel was called, simulating HTTP abort completing).
	bg.Release()

	select {
	case l := <-fgReady:
		l.Release()
	case <-time.After(time.Second):
		t.Fatal("FG was not dispatched after BG released")
	}
}

func TestLLMConcurrencySchedulerForegroundPreemptsActiveBackground(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	bg, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "memory-maintenance"})
	if err != nil {
		t.Fatalf("background acquire: %v", err)
	}
	defer bg.Release()
	cancelled := make(chan struct{})
	bg.SetCancel(func() { close(cancelled) })

	fg, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user"})
	if err != nil {
		t.Fatalf("foreground acquire: %v", err)
	}
	defer fg.Release()

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("foreground did not cancel active background lease")
	}
}
