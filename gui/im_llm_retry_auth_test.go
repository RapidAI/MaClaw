package main

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMaybeRefreshLLMAuthForRetryWithoutApp(t *testing.T) {
	h := &IMMessageHandler{}
	cfg := corelib.MaclawLLMConfig{Key: "old-key", ProviderID: "xai-grok"}
	err := errors.New("The OAuth2 access token could not be validated.")
	did := false
	out, rotated := h.maybeRefreshLLMAuthForRetry(context.Background(), cfg, err, &did)
	if !did {
		t.Fatal("expected auth-refresh attempt to be marked done")
	}
	if rotated {
		t.Fatal("no app: cannot rotate a credential")
	}
	if out.Key != "old-key" {
		t.Fatalf("key = %q, want old-key", out.Key)
	}
	out2, rotated2 := h.maybeRefreshLLMAuthForRetry(context.Background(), out, err, &did)
	if rotated2 || out2.Key != "old-key" {
		t.Fatal("second call should be a no-op")
	}
}

func TestMaybeRefreshLLMAuthForRetryIgnoresNonTokenErrors(t *testing.T) {
	h := &IMMessageHandler{}
	cfg := corelib.MaclawLLMConfig{Key: "old-key"}
	did := false
	_, rotated := h.maybeRefreshLLMAuthForRetry(context.Background(), cfg, errors.New("HTTP 503: service unavailable"), &did)
	if did || rotated {
		t.Fatal("503 should not trigger OAuth force-refresh")
	}
}

func TestApplyRefreshedProviderKeyKeepsBoundProvider(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "loop-key", ProviderID: "xai-grok"}
	got := applyRefreshedProviderKey(cfg, "", "", false, corelib.MaclawLLMConfig{Key: "assistant-other"})
	if got.Key != "loop-key" {
		t.Fatalf("bound provider must keep its key, got %q", got.Key)
	}
	got = applyRefreshedProviderKey(cfg, "fresh-key", "oauth", true, corelib.MaclawLLMConfig{Key: "assistant-other"})
	if got.Key != "fresh-key" || got.AuthType != "oauth" {
		t.Fatalf("got %+v", got)
	}
}

func TestApplyRefreshedProviderKeyFallbackWithoutProviderID(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "loop-key"}
	got := applyRefreshedProviderKey(cfg, "", "", false, corelib.MaclawLLMConfig{Key: "assistant"})
	if got.Key != "assistant" {
		t.Fatalf("unbound config should take fallback key, got %q", got.Key)
	}
}

func TestLLMAuthKeyRotated(t *testing.T) {
	prev := corelib.MaclawLLMConfig{Key: "abc"}
	if llmAuthKeyRotated(prev, corelib.MaclawLLMConfig{Key: "abc"}) {
		t.Fatal("same key is not a rotation")
	}
	if !llmAuthKeyRotated(prev, corelib.MaclawLLMConfig{Key: "xyz"}) {
		t.Fatal("different key should count as rotation")
	}
	if llmAuthKeyRotated(prev, corelib.MaclawLLMConfig{}) {
		t.Fatal("empty next key is not a rotation")
	}
}
