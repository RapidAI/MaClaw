package guiapp

import (
	"context"
	"testing"
	"time"
)

// ── GossipClient permission tests (Task 13.3, Req 6.4) ─────────────────────

func TestGossipClient_PublishPost_Forbidden(t *testing.T) {
	// Validates: Requirements 6.4 — GossipClient rejects publish when gossip disabled
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{GossipEnabled: false},
	}
	app.hubSecurityCache.mu.Unlock()

	client := NewGossipClient(app)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.PublishPost(ctx, "test content", "gossip")
	if err == nil {
		t.Fatal("expected error when gossip is forbidden")
	}
	if err != errGossipForbidden {
		t.Errorf("expected errGossipForbidden, got: %v", err)
	}
}

func TestGossipClient_AddComment_Forbidden(t *testing.T) {
	// Validates: Requirements 6.4 — GossipClient rejects comment when gossip disabled
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{GossipEnabled: false},
	}
	app.hubSecurityCache.mu.Unlock()

	client := NewGossipClient(app)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.AddComment(ctx, "post-1", "nice", 5)
	if err == nil {
		t.Fatal("expected error when gossip is forbidden")
	}
	if err != errGossipForbidden {
		t.Errorf("expected errGossipForbidden, got: %v", err)
	}
}

func TestGossipClient_RatePost_Forbidden(t *testing.T) {
	// Validates: Requirements 6.4 — GossipClient rejects rate when gossip disabled
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{GossipEnabled: false},
	}
	app.hubSecurityCache.mu.Unlock()

	client := NewGossipClient(app)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.RatePost(ctx, "post-1", 5)
	if err == nil {
		t.Fatal("expected error when gossip is forbidden")
	}
	if err != errGossipForbidden {
		t.Errorf("expected errGossipForbidden, got: %v", err)
	}
}

func TestGossipClient_PublishPost_AllowedWhenCentralizedOff(t *testing.T) {
	// Validates: Requirements 6.5 — gossip allowed when centralized security is off
	// (will fail on network call, but should NOT fail on permission check)
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: false,
		Policy:              &HubEffectivePolicy{GossipEnabled: false},
	}
	app.hubSecurityCache.mu.Unlock()

	client := NewGossipClient(app)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.PublishPost(ctx, "test", "gossip")
	// Should NOT be errGossipForbidden — it should fail on config/network instead
	if err == errGossipForbidden {
		t.Error("expected gossip to be allowed when centralized security is off")
	}
}

// ── AutoPublishTrigger gossip guard tests (Task 13.3, Req 6.3) ──────────────

func TestAutoPublishTrigger_SkipsWhenGossipDisallowed(t *testing.T) {
	// Validates: Requirements 6.3 — AutoPublishTrigger skips when gossip disabled
	app := &App{}
	client := &GossipClient{app: app}

	trigger := NewAutoPublishTrigger(client, func() bool { return true })
	trigger.SetGossipAllowedFn(func() bool { return false })

	// Send enough messages to trigger detection
	for i := 0; i < 5; i++ {
		trigger.OnChatCompleted("hello", "world")
	}

	// Buffer should remain empty because gossip is disallowed
	trigger.mu.Lock()
	bufLen := len(trigger.buffer)
	trigger.mu.Unlock()

	if bufLen != 0 {
		t.Errorf("expected empty buffer when gossip disallowed, got %d entries", bufLen)
	}
}

func TestAutoPublishTrigger_AccumulatesWhenGossipAllowed(t *testing.T) {
	// Validates: Requirements 6.5 — AutoPublishTrigger works normally when gossip allowed
	app := &App{}
	client := &GossipClient{app: app}

	trigger := NewAutoPublishTrigger(client, func() bool { return true })
	trigger.SetGossipAllowedFn(func() bool { return true })

	trigger.OnChatCompleted("hello", "world")

	trigger.mu.Lock()
	bufLen := len(trigger.buffer)
	trigger.mu.Unlock()

	if bufLen != 1 {
		t.Errorf("expected 1 buffer entry when gossip allowed, got %d", bufLen)
	}
}

func TestAutoPublishTrigger_DefaultGossipAllowed(t *testing.T) {
	// Default gossipAllowed function should return true
	app := &App{}
	client := &GossipClient{app: app}

	trigger := NewAutoPublishTrigger(client, func() bool { return true })

	trigger.OnChatCompleted("hello", "world")

	trigger.mu.Lock()
	bufLen := len(trigger.buffer)
	trigger.mu.Unlock()

	if bufLen != 1 {
		t.Errorf("expected 1 buffer entry with default gossipAllowed, got %d", bufLen)
	}
}
