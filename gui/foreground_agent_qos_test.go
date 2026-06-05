package main

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func resetForegroundAgentOwnersForTest() {
	foregroundAgentOwners.mu.Lock()
	defer foregroundAgentOwners.mu.Unlock()
	foregroundAgentOwners.counts = make(map[string]int)
	foregroundAgentWork.Store(0)
	foregroundAgentLastDoneUnixNano.Store(0)
}

func TestForegroundAgentOwnerNestingCountsOwnerOnce(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	active, first := beginForegroundAgentOwner("owner-a")
	if active != 1 || !first {
		t.Fatalf("first begin active=%d first=%v, want 1 true", active, first)
	}
	active, first = beginForegroundAgentOwner("owner-a")
	if active != 1 || first {
		t.Fatalf("nested begin active=%d first=%v, want 1 false", active, first)
	}
	active, done := endForegroundAgentOwner("owner-a")
	if active != 1 || done {
		t.Fatalf("nested end active=%d done=%v, want 1 false", active, done)
	}
	active, done = endForegroundAgentOwner("owner-a")
	if active != 0 || !done {
		t.Fatalf("final end active=%d done=%v, want 0 true", active, done)
	}
}

func TestForegroundAgentOwnerCountsIndependentOwners(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	if active, _ := beginForegroundAgentOwner("owner-a"); active != 1 {
		t.Fatalf("owner-a active=%d, want 1", active)
	}
	if active, _ := beginForegroundAgentOwner("owner-b"); active != 2 {
		t.Fatalf("owner-b active=%d, want 2", active)
	}
	if active, _ := endForegroundAgentOwner("owner-a"); active != 1 {
		t.Fatalf("end owner-a active=%d, want 1", active)
	}
	if active, _ := endForegroundAgentOwner("owner-b"); active != 0 {
		t.Fatalf("end owner-b active=%d, want 0", active)
	}
}

func TestForegroundAgentLoopCleanupIsIdempotent(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	app := &App{}
	cleanup := app.beginForegroundAgentLoop("owner-a", "req-a", "chat")
	if got := activeForegroundAgentWork(); got != 1 {
		t.Fatalf("activeForegroundAgentWork after begin = %d, want 1", got)
	}
	cleanup()
	cleanup()
	if got := activeForegroundAgentWork(); got != 0 {
		t.Fatalf("activeForegroundAgentWork after duplicate cleanup = %d, want 0", got)
	}
	if got := app.activeForegroundAgentLoops(); got != 0 {
		t.Fatalf("activeForegroundAgentLoops after duplicate cleanup = %d, want 0", got)
	}
}

func TestForegroundAgentStartCancelsActiveMemoryPipeline(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	app := &App{}
	defer func() {
		app.memoryPipelineScheduleMu.Lock()
		if app.memoryPipelineTimer != nil {
			app.memoryPipelineTimer.Stop()
			app.memoryPipelineTimer = nil
		}
		app.memoryPipelineRunActive = false
		app.memoryPipelineRunSeq = 0
		app.memoryPipelineScheduleMu.Unlock()
	}()
	store, err := memory.NewStoreWithMode(t.TempDir(), memory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()
	app.memPipeline = memory.NewMaintenance(store, nil, nil).Pipeline()
	cancelled := make(chan struct{})
	app.memoryPipelineScheduleMu.Lock()
	app.memoryPipelineScheduleSeq = 7
	app.memoryPipelineRunCancel = func() { close(cancelled) }
	app.memoryPipelineRunSeq = 7
	app.memoryPipelineRunActive = true
	app.memoryPipelineScheduleMu.Unlock()

	cleanup := app.beginForegroundAgentLoop("owner-a", "req-a", "chat")
	defer cleanup()

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("foreground start did not cancel active memory pipeline")
	}
	app.memoryPipelineScheduleMu.Lock()
	defer app.memoryPipelineScheduleMu.Unlock()
	if app.memoryPipelineScheduleSeq <= 8 {
		t.Fatalf("memory pipeline was not rescheduled after cancel; seq=%d", app.memoryPipelineScheduleSeq)
	}
	if app.memoryPipelineTimer == nil {
		t.Fatal("memory pipeline cancel did not schedule a future idle retry")
	}
	if !app.memoryPipelineRunActive {
		t.Fatal("memory pipeline active marker should stay until cancelled run exits")
	}
}

func TestStopMemoryPipelineScheduleInvalidatesRunAndTimer(t *testing.T) {
	app := &App{}
	cancelled := make(chan struct{})
	app.memoryPipelineScheduleMu.Lock()
	app.memoryPipelineScheduleSeq = 3
	app.memoryPipelineRunSeq = 3
	app.memoryPipelineRunActive = true
	app.memoryPipelineRunCancel = func() { close(cancelled) }
	app.memoryPipelineTimer = time.AfterFunc(time.Hour, func() {})
	app.memoryPipelineScheduleMu.Unlock()

	app.stopMemoryPipelineSchedule("test")

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel active memory pipeline")
	}
	app.memoryPipelineScheduleMu.Lock()
	defer app.memoryPipelineScheduleMu.Unlock()
	if app.memoryPipelineRunActive || app.memoryPipelineRunSeq != 0 || app.memoryPipelineRunCancel != nil {
		t.Fatalf("run state not cleared: active=%v seq=%d cancel=%v", app.memoryPipelineRunActive, app.memoryPipelineRunSeq, app.memoryPipelineRunCancel != nil)
	}
	if app.memoryPipelineTimer != nil {
		t.Fatal("timer not cleared")
	}
	if app.memoryPipelineScheduleSeq != 4 {
		t.Fatalf("schedule seq = %d, want 4", app.memoryPipelineScheduleSeq)
	}
}

func TestWaitForForegroundAgentIdleScopesToOwner(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	app := &App{}
	cleanupA := app.beginForegroundAgentLoop("owner-a", "req-a", "chat")
	defer cleanupA()
	cleanupB := app.beginForegroundAgentLoop("owner-b", "req-b", "chat")
	defer cleanupB()

	ownerADone := make(chan bool, 1)
	go func() {
		ownerADone <- app.waitForForegroundAgentIdle(context.Background(), "post-conversation", "owner-a")
	}()

	select {
	case <-ownerADone:
		t.Fatal("owner-specific wait returned while owner-a was still active")
	case <-time.After(100 * time.Millisecond):
	}

	cleanupA()
	select {
	case ok := <-ownerADone:
		if !ok {
			t.Fatal("owner-specific wait returned false")
		}
	case <-time.After(time.Second):
		t.Fatal("owner-specific wait stayed blocked by another owner")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if app.waitForForegroundAgentIdle(ctx, "memory-maintenance", "") {
		t.Fatal("global wait returned while owner-b was still active")
	}
}

func TestBackgroundAgentRuntimeDoesNotCountAsForegroundWork(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	app := &App{}
	h := NewIMMessageHandler(app, nil)
	ctx := NewBackgroundLoopContext("bg-scheduled-1", SlotKindScheduled, "scheduled", 3, nil, nil)
	ctx.Runtime.RequestID = "req-bg-1"

	cleanup := h.beginAgentLoopRuntime(ctx, "scheduled_task", "task", "scheduler")
	if got := activeForegroundAgentWork(); got != 0 {
		cleanup()
		t.Fatalf("activeForegroundAgentWork = %d, want 0 for background loop", got)
	}
	cleanup()
	if got := activeForegroundAgentWork(); got != 0 {
		t.Fatalf("activeForegroundAgentWork after cleanup = %d, want 0", got)
	}
}

func TestBackgroundAgentRuntimeTraceUsesBackgroundCaller(t *testing.T) {
	app := &App{}
	h := NewIMMessageHandler(app, nil)
	ctx := NewBackgroundLoopContext("bg-scheduled-2", SlotKindScheduled, "scheduled", 3, nil, nil)
	ctx.Runtime.RequestID = "req-bg-2"

	state := h.beginAgentLoopRuntimeState(ctx, "scheduled_task", "task", nil, nil, newAgentLoopTelemetry())
	defer state.Cleanup()
	trace, ok := llm.RequestTraceFromContext(state.RequestContext)
	if !ok {
		t.Fatal("missing request trace")
	}
	if trace.Caller != "background_agent_loop" || trace.OwnerID != "scheduled_task" || trace.RequestID != "req-bg-2" || trace.LoopID != "bg-scheduled-2" {
		t.Fatalf("unexpected trace: %+v", trace)
	}
	if priority := classifyLLMRequestPriority(trace); priority != llmPriorityBackground {
		t.Fatalf("priority = %s, want %s", priority, llmPriorityBackground)
	}
}

func TestAgentLoopLLMRoundPreservesBackgroundCaller(t *testing.T) {
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "background_agent_loop"})
	if got := agentLoopLLMCallerFromContext(ctx); got != "background_agent_loop" {
		t.Fatalf("caller = %q, want background_agent_loop", got)
	}
	if got := agentLoopLLMCallerFromContext(context.Background()); got != "agent_loop" {
		t.Fatalf("default caller = %q, want agent_loop", got)
	}
}
