package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestProviderConcurrencyControllerAcquireAndRelease(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	snap := controller.snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 1 || snap.QueueWaiters != 0 || snap.MaxConcurrency != 1 || snap.MaxQueueWaiters != 2 {
		t.Fatalf("unexpected snapshot after acquire: %+v", snap)
	}
	release()
	snap = controller.snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 0 || snap.QueueWaiters != 0 {
		t.Fatalf("unexpected snapshot after release: %+v", snap)
	}
}

func TestProviderConcurrencyControllerQueuesUntilReleased(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
		if err != nil {
			return
		}
		release2()
		close(acquired)
	}()

	time.Sleep(50 * time.Millisecond)
	snap := controller.snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 1 || snap.QueueWaiters != 1 {
		t.Fatalf("unexpected snapshot while queued: %+v", snap)
	}
	release()
	select {
	case <-acquired:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second acquire did not complete after release")
	}
}

func TestProviderConcurrencyControllerAcquireCanceledWhileQueued(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := controller.acquire(ctx, "provider-a", 1, 2, 0); err == nil {
		t.Fatal("expected queued acquire to fail on timeout")
	} else if queueErr, ok := err.(*providerConcurrencyError); !ok || queueErr.Kind != providerConcurrencyQueueCanceled {
		t.Fatalf("unexpected error kind: %v", err)
	}
	snap := controller.snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 1 || snap.QueueWaiters != 0 {
		t.Fatalf("unexpected snapshot after canceled wait: %+v", snap)
	}
}

func TestProviderConcurrencyControllerRejectsWhenQueueFull(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 1, 0)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	queued := make(chan struct{})
	go func() {
		_, _ = controller.acquire(context.Background(), "provider-a", 1, 1, 0)
		close(queued)
	}()
	select {
	case <-queued:
		t.Fatal("expected second acquire to remain queued")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := controller.acquire(context.Background(), "provider-a", 1, 1, 0); err == nil {
		t.Fatal("expected queue full error")
	} else if queueErr, ok := err.(*providerConcurrencyError); !ok || queueErr.Kind != providerConcurrencyQueueFull {
		t.Fatalf("unexpected error kind: %v", err)
	}
}

func TestShouldSkipFullAuthorizedProviderWhenSiblingHasCapacity(t *testing.T) {
	globalProviderConcurrency.reset()
	t.Cleanup(globalProviderConcurrency.reset)
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "a", MaxConcurrency: 1},
		{ID: "b", MaxConcurrency: 1},
	}}
	release, err := globalProviderConcurrency.acquire(context.Background(), "a", 1, 0, 0)
	if err != nil {
		t.Fatalf("hold a: %v", err)
	}
	defer release()
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "b"}, BandKey: "x1|s0|t1"},
	}
	if !shouldSkipFullAuthorizedProvider(routes, 0, reg) {
		t.Fatal("full a should skip when sibling b has capacity")
	}
	holdB, err := globalProviderConcurrency.acquire(context.Background(), "b", 1, 0, 0)
	if err != nil {
		t.Fatalf("hold b: %v", err)
	}
	defer holdB()
	if shouldSkipFullAuthorizedProvider(routes, 0, reg) {
		t.Fatal("WRR winner should queue when the whole band is full")
	}
	if !shouldSkipFullAuthorizedProvider(routes, 1, reg) {
		t.Fatal("failover member should not queue")
	}
}

func TestShouldSkipFullAuthorizedProviderAllowsCircuitOpenProbe(t *testing.T) {
	globalProviderConcurrency.reset()
	globalProviderResilience.reset()
	t.Cleanup(func() {
		globalProviderConcurrency.reset()
		globalProviderResilience.reset()
	})
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "a", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
		{ID: "b", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
	}}
	globalProviderResilience.recordFailure(reg.FindProvider("b"))
	if !globalProviderResilience.snapshot(reg.FindProvider("b")).CircuitOpen {
		t.Fatal("expected b circuit to be open")
	}
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "b"}, BandKey: "x1|s0|t1", SkipWRR: true},
	}
	if shouldSkipFullAuthorizedProvider(routes, 1, reg) {
		t.Fatal("circuit-open failover must still be tried so BeforeAttempt can probe")
	}
}

func TestApplyLiveHubSkipWRRMarksAllCircuitOpenMembers(t *testing.T) {
	globalProviderResilience.reset()
	t.Cleanup(globalProviderResilience.reset)
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "a", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
		{ID: "b", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
	}}
	globalProviderResilience.recordFailure(reg.FindProvider("a"))
	globalProviderResilience.recordFailure(reg.FindProvider("b"))
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "b"}, BandKey: "x1|s0|t1"},
	}
	applyLiveHubSkipWRR(routes, reg)
	if !routes[0].SkipWRR || !routes[1].SkipWRR {
		t.Fatalf("live circuit-open must mark every member SkipWRR, got %#v", routes)
	}
}

func TestShouldSkipFullAuthorizedProviderAllowsLiveCircuitOpenProbe(t *testing.T) {
	globalProviderConcurrency.reset()
	globalProviderResilience.reset()
	t.Cleanup(func() {
		globalProviderConcurrency.reset()
		globalProviderResilience.reset()
	})
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "a", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
		{ID: "b", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
	}}
	globalProviderResilience.recordFailure(reg.FindProvider("b"))
	if !globalProviderResilience.snapshot(reg.FindProvider("b")).CircuitOpen {
		t.Fatal("expected b circuit to be open")
	}
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "b"}, BandKey: "x1|s0|t1"},
	}
	if shouldSkipFullAuthorizedProvider(routes, 1, reg) {
		t.Fatal("live circuit-open failover must still be tried even if Balance snapshot missed SkipWRR")
	}
}

func TestShouldSkipFullAuthorizedProviderDoesNotQueueCircuitOpenAtCapacity(t *testing.T) {
	globalProviderConcurrency.reset()
	globalProviderResilience.reset()
	t.Cleanup(func() {
		globalProviderConcurrency.reset()
		globalProviderResilience.reset()
	})
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "a", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
		{ID: "b", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
	}}
	globalProviderResilience.recordFailure(reg.FindProvider("b"))
	if !globalProviderResilience.snapshot(reg.FindProvider("b")).CircuitOpen {
		t.Fatal("expected b circuit to be open")
	}
	release, err := globalProviderConcurrency.acquire(context.Background(), "b", 1, 0, 0)
	if err != nil {
		t.Fatalf("hold b: %v", err)
	}
	defer release()
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "b"}, BandKey: "x1|s0|t1", SkipWRR: true},
	}
	if !shouldSkipFullAuthorizedProvider(routes, 1, reg) {
		t.Fatal("circuit-open failover at concurrency limit must not queue on acquire")
	}
}

func TestShouldSkipFullAuthorizedProviderQueuesWhenSiblingCircuitOpen(t *testing.T) {
	globalProviderConcurrency.reset()
	globalProviderResilience.reset()
	t.Cleanup(func() {
		globalProviderConcurrency.reset()
		globalProviderResilience.reset()
	})
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "a", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
		{ID: "b", MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
	}}
	globalProviderResilience.recordFailure(reg.FindProvider("b"))
	if !globalProviderResilience.snapshot(reg.FindProvider("b")).CircuitOpen {
		t.Fatal("expected sibling circuit to be open")
	}
	release, err := globalProviderConcurrency.acquire(context.Background(), "a", 1, 0, 0)
	if err != nil {
		t.Fatalf("hold a: %v", err)
	}
	defer release()
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "b"}, BandKey: "x1|s0|t1"},
	}
	if shouldSkipFullAuthorizedProvider(routes, 0, reg) {
		t.Fatal("full WRR winner should queue when the only sibling is circuit-open")
	}
}

func TestShouldSkipFullAuthorizedProviderFailsOverToMaClaw(t *testing.T) {
	globalProviderConcurrency.reset()
	t.Cleanup(globalProviderConcurrency.reset)
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "chat-a", MaxConcurrency: 1},
	}}
	release, err := globalProviderConcurrency.acquire(context.Background(), "chat-a", 1, 0, 0)
	if err != nil {
		t.Fatalf("hold chat-a: %v", err)
	}
	defer release()
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "chat-a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: llmservice.MaClawOfficialProviderID}, BandKey: "x1|s0|t1"},
	}
	if !shouldSkipFullAuthorizedProvider(routes, 0, reg) {
		t.Fatal("full local winner should fail over to MaClaw official")
	}
	if shouldSkipFullAuthorizedProvider(routes, 1, reg) {
		t.Fatal("MaClaw official must not look unavailable just because it is not a local registry card")
	}
}

func TestOrderAuthorizedProvidersKeepsMaClawInWRRWhenMissingFromRegistry(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "chat-a", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 10},
	}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"chat-a", llmservice.MaClawOfficialProviderID}}
	got := orderAuthorizedProviders(nil, model, reg)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, item := range got {
		if item.Route.ProviderID == llmservice.MaClawOfficialProviderID && item.SkipWRR {
			t.Fatal("MaClaw official must stay eligible for WRR when it is not a local registry card")
		}
	}
}

func TestOrderAuthorizedProvidersSkipsUnknownFromWRR(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "chat-a", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 10},
		{ID: "chat-b", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 10},
	}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"chat-a", "ghost", "chat-b"}}
	first := orderAuthorizedProviders(nil, model, reg)
	second := orderAuthorizedProviders(nil, model, reg)
	if len(first) != 3 || first[0].Route.ProviderID != "chat-a" {
		t.Fatalf("first = %#v, want chat-a", first)
	}
	var ghost llmpool.BalancedRoute
	for _, item := range first {
		if item.Route.ProviderID == "ghost" {
			ghost = item
			break
		}
	}
	if ghost.Route.ProviderID == "" || !ghost.SkipWRR || ghost.FirstInBand {
		t.Fatalf("ghost should stay in failover and SkipWRR, got %#v", first)
	}
	if len(second) < 1 || second[0].Route.ProviderID != "chat-b" {
		t.Fatalf("second = %s, want chat-b (ghost must not take a WRR slot)", second[0].Route.ProviderID)
	}
}

func TestShouldSkipFullUsesLiveChatStreamSkipWRR(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	globalProviderConcurrency.reset()
	t.Cleanup(globalProviderConcurrency.reset)
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "chat-a", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 1},
		{ID: "resp", Protocol: "openai", WireAPI: "responses", MaxConcurrency: 10},
	}}
	release, err := globalProviderConcurrency.acquire(context.Background(), "chat-a", 1, 0, 0)
	if err != nil {
		t.Fatalf("hold chat-a: %v", err)
	}
	defer release()
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"chat-a", "resp"}}
	routes := orderAuthorizedChatStreamProviders(nil, model, reg)
	if len(routes) != 2 || routes[0].Route.ProviderID != "chat-a" || !routes[1].SkipWRR {
		t.Fatalf("routes = %#v, want chat-a then SkipWRR resp", routes)
	}
	if shouldSkipFullAuthorizedProvider(routes, 0, reg) {
		t.Fatal("full chat-stream winner should queue when live Balance only has a SkipWRR sibling")
	}
}

func TestOrderAuthorizedChatStreamProvidersSkipsNonStreamWRR(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "chat-a", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 10},
		{ID: "resp", Protocol: "openai", WireAPI: "responses", MaxConcurrency: 10},
		{ID: "chat-b", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 10},
	}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"chat-a", "resp", "chat-b"}}
	first := orderAuthorizedChatStreamProviders(nil, model, reg)
	second := orderAuthorizedChatStreamProviders(nil, model, reg)
	if len(first) < 1 || first[0].Route.ProviderID != "chat-a" {
		t.Fatalf("first = %#v, want chat-a", first)
	}
	if len(second) < 1 || second[0].Route.ProviderID != "chat-b" {
		t.Fatalf("second = %s, want chat-b (responses must not take a chat-stream WRR slot)", second[0].Route.ProviderID)
	}
}

func TestOrderAuthorizedChatStreamPoolDoesNotResetNonStreamWRR(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "chat-a", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 10},
		{ID: "resp", Protocol: "openai", WireAPI: "responses", MaxConcurrency: 10},
		{ID: "chat-b", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 10},
	}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"chat-a", "resp", "chat-b"}}
	if got := orderAuthorizedProviders(nil, model, reg); len(got) < 1 || got[0].Route.ProviderID != "chat-a" {
		t.Fatalf("non-stream first = %#v, want chat-a", got)
	}
	if got := orderAuthorizedChatStreamProviders(nil, model, reg); len(got) < 1 || got[0].Route.ProviderID != "chat-a" {
		t.Fatalf("stream first = %#v, want chat-a in its own pool", got)
	}
	if got := orderAuthorizedProviders(nil, model, reg); len(got) < 1 || got[0].Route.ProviderID != "resp" {
		t.Fatalf("non-stream second = %s, want resp (chat-stream must not reset this pool)", got[0].Route.ProviderID)
	}
	if got := orderAuthorizedChatStreamProviders(nil, model, reg); len(got) < 1 || got[0].Route.ProviderID != "chat-b" {
		t.Fatalf("stream second = %s, want chat-b", got[0].Route.ProviderID)
	}
}

func TestShouldSkipFullAuthorizedProviderQueuesWhenSiblingCannotStream(t *testing.T) {
	globalProviderConcurrency.reset()
	t.Cleanup(globalProviderConcurrency.reset)
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "chat-a", Protocol: "openai", WireAPI: "chat", MaxConcurrency: 1},
		{ID: "resp", Protocol: "openai", WireAPI: "responses", MaxConcurrency: 10},
	}}
	release, err := globalProviderConcurrency.acquire(context.Background(), "chat-a", 1, 0, 0)
	if err != nil {
		t.Fatalf("hold chat-a: %v", err)
	}
	defer release()
	routes := []llmpool.BalancedRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "chat-a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "resp"}, BandKey: "x1|s0|t1", SkipWRR: true},
	}
	if shouldSkipFullAuthorizedProvider(routes, 0, reg) {
		t.Fatal("full chat-stream winner should queue when the only sibling cannot stream")
	}
}

func TestHubProviderCannotAcceptMissingProvider(t *testing.T) {
	if !hubProviderCannotAccept(nil) {
		t.Fatal("missing sibling must not count as available capacity")
	}
}

func TestHubProviderSkipWRRIsCircuitOnly(t *testing.T) {
	globalProviderResilience.reset()
	t.Cleanup(globalProviderResilience.reset)
	provider := &im.LLMProvider{
		ID:                       "flaky",
		CircuitBreakerThreshold:  3,
		CircuitBreakerCooldownMS: 60_000,
		FailureBackoffBaseMS:     5_000,
		FailureBackoffMaxMS:      5_000,
	}
	globalProviderResilience.recordFailure(provider)
	snap := globalProviderResilience.snapshot(provider)
	if snap.CircuitOpen {
		t.Fatal("one failure must not open the circuit")
	}
	if hubProviderSkipWRR(provider) {
		t.Fatal("short backoff must not remove the provider from WRR")
	}
	if !hubProviderCannotAccept(provider) {
		t.Fatal("backing-off sibling must not count as available capacity")
	}
}

func TestProviderConcurrencyControllerQueueTimeout(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 30)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	if _, err := controller.acquire(context.Background(), "provider-a", 1, 2, 30); err == nil {
		t.Fatal("expected queue timeout error")
	} else if queueErr, ok := err.(*providerConcurrencyError); !ok || queueErr.Kind != providerConcurrencyQueueTimeout {
		t.Fatalf("unexpected error kind: %v", err)
	}
}
