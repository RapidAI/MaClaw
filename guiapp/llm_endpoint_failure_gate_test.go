package guiapp

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func gateTestConfig() corelib.MaclawLLMConfig {
	return corelib.MaclawLLMConfig{
		URL:          "https://hub.example.com/v1/",
		Model:        "gpt-test",
		Protocol:     "openai",
		WireAPI:      "chat",
		ProviderName: "hub",
	}
}

func TestLLMEndpointFailureGateBlocksRecentNetworkFailure(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	gate := newLLMEndpointFailureGate(30 * time.Second)
	gate.now = func() time.Time { return now }
	cfg := gateTestConfig()

	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("fresh gate should not skip")
	}
	gate.observe(cfg, llmEndpointCategoryLightweightClassify, false, context.DeadlineExceeded)
	if reason, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); !skip || reason == "" {
		t.Fatalf("recent network failure should skip, skip=%v reason=%q", skip, reason)
	}
}

func TestLLMEndpointFailureGateExpiresAndClearsOnSuccess(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	gate := newLLMEndpointFailureGate(30 * time.Second)
	gate.now = func() time.Time { return now }
	cfg := corelib.MaclawLLMConfig{
		URL:      "https://hub.example.com/api/v1",
		Model:    "gpt-test",
		Protocol: "openai",
	}

	gate.observe(cfg, llmEndpointCategoryLightweightClassify, false, context.DeadlineExceeded)
	now = now.Add(11 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("expired network failure should not skip")
	}

	gate.observe(cfg, llmEndpointCategoryLightweightClassify, false, context.DeadlineExceeded)
	gate.observe(cfg, llmEndpointCategoryLightweightClassify, false, nil)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("successful result should clear recent failure")
	}
}

func TestLLMEndpointFailureGateIgnoresNonNetworkFailure(t *testing.T) {
	gate := newLLMEndpointFailureGate(30 * time.Second)
	cfg := corelib.MaclawLLMConfig{URL: "https://hub.example.com/v1", Model: "gpt-test"}

	gate.observe(cfg, llmEndpointCategoryLightweightClassify, false, errNonNetworkForEndpointGateTest{})
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("non-network failures should not trip endpoint gate")
	}
}

// A caller-side budget firing (ctx deadline / user cancel) must never poison
// the gate, even when the error classifies as network (DeadlineExceeded does).
func TestLLMEndpointFailureGateBudgetFiredDoesNotPoison(t *testing.T) {
	gate := newLLMEndpointFailureGate(30 * time.Second)
	cfg := gateTestConfig()

	gate.observe(cfg, llmEndpointCategoryLightweightClassify, true, context.DeadlineExceeded)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("budget-fired deadline should not trip endpoint gate")
	}

	gate.observe(cfg, llmEndpointCategoryUICTree, true, context.Canceled)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); skip {
		t.Fatal("user cancellation should not trip endpoint gate")
	}
}

// Bans are tracked per category: a uic-tree failure must not block
// lightweight-classify calls on the same endpoint.
func TestLLMEndpointFailureGateCategoryIsolation(t *testing.T) {
	gate := newLLMEndpointFailureGate(30 * time.Second)
	cfg := gateTestConfig()

	gate.observe(cfg, llmEndpointCategoryUICTree, false, context.DeadlineExceeded)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); !skip {
		t.Fatal("uic-tree failure should skip uic-tree calls")
	}
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("uic-tree failure must not skip lightweight-classify calls")
	}
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryMainStream); skip {
		t.Fatal("uic-tree failure must not skip main-stream calls")
	}
}

// A successful main-stream call shortens every lightweight ban on the same
// endpoint prefix (URL+protocol+provider, ignoring model/wire) to a 2s grace
// window instead of clearing it outright.
func TestLLMEndpointFailureGateMainStreamSuccessShortensLightweightBans(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	gate := newLLMEndpointFailureGate(30 * time.Second)
	gate.now = func() time.Time { return now }
	cfg := gateTestConfig()

	gate.observe(cfg, llmEndpointCategoryUICTree, false, context.DeadlineExceeded)
	gate.observe(cfg, llmEndpointCategoryLightweightClassify, false, context.DeadlineExceeded)

	// Main-stream succeeds on a different model behind the same endpoint
	// prefix (different wire/model, same URL/protocol/provider).
	mainCfg := cfg
	mainCfg.Model = "gpt-main"
	mainCfg.WireAPI = "responses"
	gate.observe(mainCfg, llmEndpointCategoryMainStream, false, nil)

	// Inside the 2s grace window the lightweight bans still hold...
	now = now.Add(time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); !skip {
		t.Fatal("lightweight ban should still hold inside the 2s grace window")
	}
	// ...and expire right after it instead of living out the full ttl.
	now = now.Add(2 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); skip {
		t.Fatal("main-stream success should shorten lightweight ban to ~2s")
	}
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("main-stream success should shorten lightweight-classify ban to ~2s")
	}
}

// A main-stream success on a different endpoint prefix must not shorten bans.
func TestLLMEndpointFailureGateMainStreamSuccessOtherPrefixUntouched(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	gate := newLLMEndpointFailureGate(30 * time.Second)
	gate.now = func() time.Time { return now }
	cfg := gateTestConfig()

	gate.observe(cfg, llmEndpointCategoryUICTree, false, context.DeadlineExceeded)

	otherCfg := cfg
	otherCfg.URL = "https://other.example.com/v1"
	gate.observe(otherCfg, llmEndpointCategoryMainStream, false, nil)

	now = now.Add(5 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); !skip {
		t.Fatal("main-stream success on another prefix must not shorten this ban")
	}
}

// Lightweight categories use a 10s ban TTL.
func TestLLMEndpointFailureGateLightweightTTLIsTenSeconds(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	gate := newLLMEndpointFailureGate(30 * time.Second)
	gate.now = func() time.Time { return now }
	cfg := gateTestConfig()

	gate.observe(cfg, llmEndpointCategoryLightweightClassify, false, context.DeadlineExceeded)
	now = now.Add(9 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); !skip {
		t.Fatal("lightweight ban should still hold at 9s")
	}
	now = now.Add(2 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryLightweightClassify); skip {
		t.Fatal("lightweight ban should expire after 10s")
	}

	// uic-tree shares the lightweight TTL.
	gate.observe(cfg, llmEndpointCategoryUICTree, false, context.DeadlineExceeded)
	now = now.Add(9 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); !skip {
		t.Fatal("uic-tree ban should still hold at 9s")
	}
	now = now.Add(2 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); skip {
		t.Fatal("uic-tree ban should expire after 10s")
	}
}

// The main-stream category keeps the 30s TTL.
func TestLLMEndpointFailureGateMainStreamTTL(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	gate := newLLMEndpointFailureGate(30 * time.Second)
	gate.now = func() time.Time { return now }
	cfg := gateTestConfig()

	gate.observe(cfg, llmEndpointCategoryMainStream, false, context.DeadlineExceeded)
	now = now.Add(29 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryMainStream); !skip {
		t.Fatal("main-stream ban should still hold at 29s")
	}
	now = now.Add(2 * time.Second)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryMainStream); skip {
		t.Fatal("main-stream ban should expire after 30s")
	}
}

// A success observed while the caller ctx has fired (late response flushed
// after cancellation) still counts as endpoint evidence and clears the ban.
func TestLLMEndpointFailureGateSuccessWithFiredBudgetStillClears(t *testing.T) {
	gate := newLLMEndpointFailureGate(30 * time.Second)
	cfg := gateTestConfig()

	gate.observe(cfg, llmEndpointCategoryUICTree, false, context.DeadlineExceeded)
	gate.observe(cfg, llmEndpointCategoryUICTree, true, nil)
	if _, skip := gate.shouldSkip(cfg, llmEndpointCategoryUICTree); skip {
		t.Fatal("success must clear the ban even when the caller budget fired")
	}
}

type errNonNetworkForEndpointGateTest struct{}

func (errNonNetworkForEndpointGateTest) Error() string { return "validation failed" }
