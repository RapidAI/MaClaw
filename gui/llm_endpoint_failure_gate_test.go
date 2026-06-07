package main

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLLMEndpointFailureGateBlocksRecentNetworkFailure(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	gate := newLLMEndpointFailureGate(30 * time.Second)
	gate.now = func() time.Time { return now }
	cfg := corelib.MaclawLLMConfig{
		URL:          "https://hub.example.com/v1/",
		Model:        "gpt-test",
		Protocol:     "openai",
		WireAPI:      "chat",
		ProviderName: "hub",
	}

	if _, skip := gate.shouldSkip(cfg); skip {
		t.Fatal("fresh gate should not skip")
	}
	gate.observe(cfg, context.DeadlineExceeded)
	if reason, skip := gate.shouldSkip(cfg); !skip || reason == "" {
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

	gate.observe(cfg, context.DeadlineExceeded)
	now = now.Add(31 * time.Second)
	if _, skip := gate.shouldSkip(cfg); skip {
		t.Fatal("expired network failure should not skip")
	}

	now = now.Add(time.Second)
	gate.observe(cfg, context.DeadlineExceeded)
	gate.observe(cfg, nil)
	if _, skip := gate.shouldSkip(cfg); skip {
		t.Fatal("successful result should clear recent failure")
	}
}

func TestLLMEndpointFailureGateIgnoresNonNetworkFailure(t *testing.T) {
	gate := newLLMEndpointFailureGate(30 * time.Second)
	cfg := corelib.MaclawLLMConfig{URL: "https://hub.example.com/v1", Model: "gpt-test"}

	gate.observe(cfg, errNonNetworkForEndpointGateTest{})
	if _, skip := gate.shouldSkip(cfg); skip {
		t.Fatal("non-network failures should not trip endpoint gate")
	}
}

type errNonNetworkForEndpointGateTest struct{}

func (errNonNetworkForEndpointGateTest) Error() string { return "validation failed" }
