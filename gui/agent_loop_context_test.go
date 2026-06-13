package main

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"testing/quick"
	"time"
)

// ---------------------------------------------------------------------------
// Property 1: Loop Context 隔离
// Modifying one LoopContext's MaxIterations/Iteration does NOT affect others.
// ---------------------------------------------------------------------------

func TestProperty1_LoopContextIsolation(t *testing.T) {
	f := func(n uint8, maxA, maxB uint16, iterA, iterB uint16) bool {
		count := int(n)%5 + 2 // 2..6 contexts
		contexts := make([]*LoopContext, count)
		for i := range contexts {
			contexts[i] = NewLoopContext(
				"ctx-"+string(rune('A'+i)),
				int(maxA)+1,
				nil,
			)
			contexts[i].SetIteration(int(iterA))
		}

		// Mutate only the first context
		contexts[0].SetMaxIterations(int(maxB) + 100)
		contexts[0].SetIteration(int(iterB) + 100)

		// All other contexts must be unchanged
		for i := 1; i < count; i++ {
			if contexts[i].MaxIterations() != int(maxA)+1 {
				return false
			}
			if contexts[i].Iteration() != int(iterA) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Property 3: Continue Signal 正确传递
// Sending N additional rounds via ContinueC increases MaxIterations by N.
// ---------------------------------------------------------------------------

func TestProperty3_ContinueSignal(t *testing.T) {
	f := func(initial uint16, additional uint16) bool {
		initMax := int(initial)%100 + 10
		addRounds := int(additional)%50 + 1

		statusC := make(chan StatusEvent, 32)
		ctx := NewBackgroundLoopContext("bg-test", SlotKindCoding, "test", initMax, nil, statusC)

		// Simulate background loop receiving continue signal
		go func() {
			ctx.ContinueC <- addRounds
		}()

		received := <-ctx.ContinueC
		ctx.AddMaxIterations(received)

		return ctx.MaxIterations() == initMax+addRounds
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Property 4: Graceful Shutdown on Channel Close
// Closing ContinueC does not panic and the loop can detect it.
// ---------------------------------------------------------------------------

func TestProperty4_GracefulShutdownOnClose(t *testing.T) {
	f := func(seed uint8) bool {
		statusC := make(chan StatusEvent, 32)
		ctx := NewBackgroundLoopContext("bg-close", SlotKindScheduled, "test", 20, nil, statusC)

		// Close ContinueC to signal "user declined"
		close(ctx.ContinueC)

		// Reading from closed channel should return zero value immediately
		val, ok := <-ctx.ContinueC
		if ok {
			return false // should be closed
		}
		if val != 0 {
			return false
		}

		// Should be able to set state without panic
		ctx.SetState("stopped")
		return ctx.State() == "stopped"
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Property 5: Timeout Expiry
// If no Continue_Signal within timeout, the loop can detect it via select.
// ---------------------------------------------------------------------------

func TestProperty5_TimeoutExpiry(t *testing.T) {
	f := func(seed uint8) bool {
		statusC := make(chan StatusEvent, 32)
		ctx := NewBackgroundLoopContext("bg-timeout", SlotKindAuto, "test", 20, nil, statusC)

		timeout := 50 * time.Millisecond
		timedOut := false

		select {
		case _, ok := <-ctx.ContinueC:
			if !ok {
				// channel closed — also acceptable
				return true
			}
			// received a signal — not a timeout
			return false
		case <-time.After(timeout):
			timedOut = true
		}

		return timedOut
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access safety for LoopContext fields
// ---------------------------------------------------------------------------

func TestLoopContext_ConcurrentAccess(t *testing.T) {
	ctx := NewLoopContext("concurrent", 100, nil)
	var wg sync.WaitGroup
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ctx.SetMaxIterations(rng.Intn(200))
				_ = ctx.MaxIterations()
				ctx.IncrementIteration()
				_ = ctx.Iteration()
				ctx.SetState("running")
				_ = ctx.State()
			}
		}(i)
	}
	wg.Wait()
	// No race detector failures = pass
}

func TestLoopContext_Cancel(t *testing.T) {
	ctx := NewLoopContext("cancel-test", 10, nil)
	if ctx.IsCancelled() {
		t.Fatal("should not be cancelled initially")
	}
	ctx.Cancel()
	if !ctx.IsCancelled() {
		t.Fatal("should be cancelled after Cancel()")
	}
	// Double cancel should not panic
	ctx.Cancel()
}

func TestLoopContextRequestReplanCancelsOperationWithoutCancellingLoop(t *testing.T) {
	ctx := NewLoopContext("replan-test", 10, nil)
	opCtx, end, revision := ctx.BeginReplannableOperation(context.Background())
	defer end()

	if ctx.ReplanRequestedSince(revision) {
		t.Fatal("replan should not be requested before RequestReplan")
	}
	ctx.RequestReplan()
	if ctx.IsCancelled() {
		t.Fatal("RequestReplan should not cancel the whole loop")
	}
	if !ctx.ReplanRequestedSince(revision) {
		t.Fatal("RequestReplan should advance replan revision")
	}
	select {
	case <-opCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("RequestReplan did not cancel active operation")
	}
}

func TestLoopContextCancelCancelsOperationWithoutAdvancingReplan(t *testing.T) {
	ctx := NewLoopContext("cancel-op-test", 10, nil)
	opCtx, end, revision := ctx.BeginReplannableOperation(context.Background())
	defer end()

	ctx.Cancel()
	if !ctx.IsCancelled() {
		t.Fatal("Cancel should cancel the loop")
	}
	if ctx.ReplanRequestedSince(revision) {
		t.Fatal("Cancel should not advance replan revision")
	}
	select {
	case <-opCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("Cancel did not cancel active operation")
	}
}

func TestNewBackgroundLoopContext_Defaults(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	ctx := NewBackgroundLoopContext("bg-1", SlotKindCoding, "write snake game", 30, nil, statusC)

	if ctx.Kind != LoopKindBackground {
		t.Errorf("expected LoopKindBackground, got %d", ctx.Kind)
	}
	if ctx.SlotKind != SlotKindCoding {
		t.Errorf("expected SlotKindCoding, got %d", ctx.SlotKind)
	}
	if ctx.MaxIterations() != 30 {
		t.Errorf("expected 30, got %d", ctx.MaxIterations())
	}
	if ctx.State() != "running" {
		t.Errorf("expected running, got %s", ctx.State())
	}
	if ctx.Description != "write snake game" {
		t.Errorf("expected description, got %s", ctx.Description)
	}
}

func TestSlotKind_String(t *testing.T) {
	tests := []struct {
		kind SlotKind
		want string
	}{
		{SlotKindCoding, "coding"},
		{SlotKindScheduled, "scheduled"},
		{SlotKindAuto, "auto"},
		{SlotKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("SlotKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
