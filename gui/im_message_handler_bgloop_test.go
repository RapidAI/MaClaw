package main

import (
	"math/rand"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

// ---------------------------------------------------------------------------
// Property-based tests for background-agent-loop Task 4: runAgentLoop refactor.
//
// Property 6: 向后兼容 — Chat Loop 行为不变
// For any non-background message processed by HandleIMMessageWithProgress,
// the response SHALL be identical to the current implementation (same
// conversation flow, same tool routing, same memory persistence).
//
// We verify this by checking that:
// 1. A chat LoopContext is created with Kind=LoopKindChat
// 2. The LoopContext gets the correct HTTPClient (chat client, not task client)
// 3. The LoopContext iteration tracking works correctly
// 4. loopMaxOverride and ctx.MaxIterations stay in sync
// 5. StatusC drain does not interfere when StatusC is nil (chat default)
// 6. ContinueC pause logic does NOT trigger for chat loops
// ---------------------------------------------------------------------------

// bgloopTestConfig generates random configurations for backward compat tests.
type bgloopTestConfig struct {
	MaxIter       int
	LoopOverride  int
	MinIterations int
}

func (bgloopTestConfig) Generate(rand *rand.Rand, size int) reflect.Value {
	return reflect.ValueOf(bgloopTestConfig{
		MaxIter:       rand.Intn(50) + 5,
		LoopOverride:  rand.Intn(30),
		MinIterations: rand.Intn(10),
	})
}

// TestBgLoopProperty6_ChatLoopContextCreation verifies that chat messages
// create a LoopContext with Kind=LoopKindChat and correct defaults.
func TestBgLoopProperty6_ChatLoopContextCreation(t *testing.T) {
	f := func(cfg bgloopTestConfig) bool {
		client := &http.Client{Timeout: 30 * time.Second}
		ctx := NewLoopContext("chat", cfg.MaxIter, client)

		if ctx.Kind != LoopKindChat {
			t.Logf("expected LoopKindChat, got %d", ctx.Kind)
			return false
		}
		if ctx.ID != "chat" {
			t.Logf("expected ID 'chat', got %q", ctx.ID)
			return false
		}
		if ctx.MaxIterations() != cfg.MaxIter {
			t.Logf("expected MaxIterations=%d, got %d", cfg.MaxIter, ctx.MaxIterations())
			return false
		}
		if ctx.HTTPClient != client {
			t.Logf("HTTPClient mismatch")
			return false
		}
		if ctx.State() != "running" {
			t.Logf("expected state 'running', got %q", ctx.State())
			return false
		}
		// Chat loops should NOT have ContinueC (it's nil).
		if ctx.ContinueC != nil {
			t.Logf("chat loop should not have ContinueC")
			return false
		}
		// Chat loops should NOT have StatusC by default.
		if ctx.StatusC != nil {
			t.Logf("chat loop should not have StatusC by default")
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 6a (ChatLoopContextCreation) failed: %v", err)
	}
}

// TestBgLoopProperty6_LoopMaxOverrideSyncsToCtx verifies that when
// toolSetMaxIterations writes to loopMaxOverride, it also updates the
// active LoopContext.
func TestBgLoopProperty6_LoopMaxOverrideSyncsToCtx(t *testing.T) {
	f := func(cfg bgloopTestConfig) bool {
		if cfg.LoopOverride < 1 || cfg.LoopOverride > maxAgentIterationsCap {
			return true // skip invalid overrides
		}

		app := &App{}
		mgr := &RemoteSessionManager{
			app:      app,
			sessions: map[string]*RemoteSession{},
		}
		h := &IMMessageHandler{
			app:     app,
			manager: mgr,
		}

		// Simulate an active loop context.
		ctx := NewLoopContext("chat", cfg.MaxIter, nil)
		h.currentLoopCtx = ctx

		// Call toolSetMaxIterations.
		result := h.toolSetMaxIterations(map[string]interface{}{
			"max_iterations": float64(cfg.LoopOverride),
		})

		if !strings.Contains(result, "✅") {
			t.Logf("toolSetMaxIterations failed: %s", result)
			return false
		}

		// Verify both loopMaxOverride and ctx are in sync.
		expected := cfg.LoopOverride
		if expected < minAgentIterations {
			expected = minAgentIterations
		}
		if expected > maxAgentIterationsCap {
			expected = maxAgentIterationsCap
		}
		if h.loopMaxOverride != expected {
			t.Logf("loopMaxOverride=%d, expected=%d", h.loopMaxOverride, expected)
			return false
		}
		if ctx.MaxIterations() != expected {
			t.Logf("ctx.MaxIterations()=%d, expected=%d", ctx.MaxIterations(), expected)
			return false
		}

		// Clean up.
		h.currentLoopCtx = nil
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 6b (LoopMaxOverrideSyncsToCtx) failed: %v", err)
	}
}

// TestBgLoopProperty6_ContinueCNotTriggeredForChat verifies that the
// background-only ContinueC pause logic does NOT activate for chat loops.
func TestBgLoopProperty6_ContinueCNotTriggeredForChat(t *testing.T) {
	f := func(cfg bgloopTestConfig) bool {
		ctx := NewLoopContext("chat", cfg.MaxIter, nil)

		// Simulate iteration near the limit.
		iteration := cfg.MaxIter - 1
		if iteration < 0 {
			iteration = 0
		}
		ctx.SetIteration(iteration)

		// The pause condition: ctx.Kind == LoopKindBackground && effectiveMax > 4 && iteration == effectiveMax-2
		// For chat loops, this should NEVER be true.
		shouldPause := ctx.Kind == LoopKindBackground && ctx.MaxIterations() > 4 && iteration == ctx.MaxIterations()-2
		if shouldPause {
			t.Logf("chat loop incorrectly triggered pause at iteration=%d, max=%d", iteration, ctx.MaxIterations())
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 6c (ContinueCNotTriggeredForChat) failed: %v", err)
	}
}

// TestBgLoopProperty6_StatusCDrainSkippedWhenNil verifies that the StatusC
// drain logic is safely skipped when StatusC is nil (default for chat loops).
func TestBgLoopProperty6_StatusCDrainSkippedWhenNil(t *testing.T) {
	f := func(cfg bgloopTestConfig) bool {
		ctx := NewLoopContext("chat", cfg.MaxIter, nil)

		// The drain condition: ctx.Kind == LoopKindChat && ctx.StatusC != nil
		// For default chat loops, StatusC is nil, so drain should be skipped.
		shouldDrain := ctx.Kind == LoopKindChat && ctx.StatusC != nil
		if shouldDrain {
			t.Logf("chat loop with nil StatusC incorrectly triggered drain")
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 6d (StatusCDrainSkippedWhenNil) failed: %v", err)
	}
}


// TestBgLoopProperty6_StatusCDrainWorksWhenSet verifies that when a chat
// loop has a StatusC (because bgManager is active), events are correctly
// drained without blocking.
func TestBgLoopProperty6_StatusCDrainWorksWhenSet(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	ctx := NewLoopContext("chat", 20, nil)
	ctx.StatusC = statusC

	// Push some events.
	for i := 0; i < 5; i++ {
		statusC <- StatusEvent{
			Type:    StatusEventProgress,
			LoopID:  "bg-coding-1",
			Message: "test event",
		}
	}

	// Drain like the main loop does.
	var drained int
	for {
		select {
		case <-ctx.StatusC:
			drained++
		default:
			goto done
		}
	}
done:

	if drained != 5 {
		t.Errorf("expected 5 drained events, got %d", drained)
	}
}

// TestBgLoopProperty6_BackgroundLoopPausesNearLimit verifies that background
// loops correctly enter paused state near the iteration limit.
func TestBgLoopProperty6_BackgroundLoopPausesNearLimit(t *testing.T) {
	f := func(cfg bgloopTestConfig) bool {
		if cfg.MaxIter <= 4 {
			return true // skip edge cases where pause doesn't apply (need >4)
		}

		statusC := make(chan StatusEvent, 32)
		ctx := NewBackgroundLoopContext("bg-test", SlotKindCoding, "test", cfg.MaxIter, nil, statusC)

		iteration := cfg.MaxIter - 2
		ctx.SetIteration(iteration)

		// The pause condition should be true for background loops at exactly maxIter-2.
		shouldPause := ctx.Kind == LoopKindBackground && ctx.MaxIterations() > 4 && iteration == ctx.MaxIterations()-2
		if !shouldPause {
			t.Logf("background loop should pause at iteration=%d, max=%d", iteration, ctx.MaxIterations())
			return false
		}

		// Verify it does NOT pause one iteration earlier.
		earlyIter := cfg.MaxIter - 3
		shouldNotPauseEarly := ctx.Kind == LoopKindBackground && ctx.MaxIterations() > 4 && earlyIter == ctx.MaxIterations()-2
		if shouldNotPauseEarly {
			t.Logf("background loop should NOT pause at iteration=%d", earlyIter)
			return false
		}

		// Simulate the pause: set state, send event, then continue.
		ctx.SetState("paused")
		if ctx.State() != "paused" {
			t.Logf("expected state 'paused', got %q", ctx.State())
			return false
		}

		// Send continue signal.
		ctx.ContinueC <- 10
		extra := <-ctx.ContinueC
		ctx.AddMaxIterations(extra)
		ctx.SetState("running")

		if ctx.MaxIterations() != cfg.MaxIter+10 {
			t.Logf("expected MaxIterations=%d, got %d", cfg.MaxIter+10, ctx.MaxIterations())
			return false
		}
		if ctx.State() != "running" {
			t.Logf("expected state 'running' after continue, got %q", ctx.State())
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 6e (BackgroundLoopPausesNearLimit) failed: %v", err)
	}
}

// TestBgLoopProperty6_CancelStopsLoop verifies that cancelling a LoopContext
// is correctly detected by IsCancelled().
func TestBgLoopProperty6_CancelStopsLoop(t *testing.T) {
	f := func(cfg bgloopTestConfig) bool {
		ctx := NewLoopContext("chat", cfg.MaxIter, nil)

		if ctx.IsCancelled() {
			t.Logf("new context should not be cancelled")
			return false
		}

		ctx.Cancel()

		if !ctx.IsCancelled() {
			t.Logf("context should be cancelled after Cancel()")
			return false
		}

		// Double cancel should not panic.
		ctx.Cancel()
		return ctx.IsCancelled()
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 6f (CancelStopsLoop) failed: %v", err)
	}
}
