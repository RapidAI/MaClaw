package main

import (
	"context"
	"errors"
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

func TestLLMConcurrencySchedulerBlocksBackgroundWhileForegroundAgentWorkActive(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 1 }
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "memory-maintenance"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("background acquire err = %v, want deadline while foreground agent work active", err)
	}
}

func TestLLMConcurrencySchedulerDegradedLimitsForegroundToOne(t *testing.T) {
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	s.ObserveResult(llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user"}, errors.New("HTTP 503 service unavailable"))

	first, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user"})
	if err != nil {
		t.Fatalf("first foreground acquire: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "agent_loop", OwnerID: "desktop-user:task"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second foreground acquire err = %v, want deadline", err)
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
	s := newLLMConcurrencyScheduler()
	s.foregroundWork = func() int64 { return 0 }
	s.ObserveResult(llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-a"}, errors.New("HTTP 503 service unavailable"))

	first, err := s.Acquire(context.Background(), llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-a"})
	if err != nil {
		t.Fatalf("first foreground acquire: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, llm.RequestTrace{Caller: "agent_loop", OwnerID: "owner-b"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second foreground acquire err = %v, want deadline", err)
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
