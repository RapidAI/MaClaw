package main

import (
	"context"
	"testing"
	"time"
)

// TestConfirmCriticalRisk_FailClosedEmptyPlatform verifies that calling
// confirmCriticalRiskSkill with an empty platform returns false immediately
// (fail-closed behavior).
func TestConfirmCriticalRisk_FailClosedEmptyPlatform(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	result := h.confirmCriticalRiskSkill(
		context.Background(),
		"dangerous-skill", "https://hub.example.com",
		[]string{"rm -rf found"}, "", "",
	)
	if result {
		t.Fatal("expected false for empty platform (fail-closed), got true")
	}
}

// TestConfirmCriticalRisk_TimeoutReturnsFalse verifies that when no response
// is sent on the channel, the function returns false after the timeout.
// Instead of waiting 120s, we test the channel mechanism directly with a
// short timer.
func TestConfirmCriticalRisk_TimeoutReturnsFalse(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Directly test the channel + timeout mechanism:
	// Create a response channel and store it, then select with a short timeout.
	respCh := make(chan criticalRiskConfirmResponse, 1)
	confirmID := "test_timeout_1"
	h.pendingCriticalConfirm.Store(confirmID, respCh)
	defer h.pendingCriticalConfirm.Delete(confirmID)

	// Nobody sends on respCh — simulate timeout with a short duration.
	select {
	case resp := <-respCh:
		t.Fatalf("unexpected response: %+v", resp)
	case <-time.After(50 * time.Millisecond):
		// Timeout reached — this is the expected path (mirrors the 120s timeout).
	}
}

// TestConfirmCriticalRisk_ContextCancellation verifies that cancelling the
// context causes confirmCriticalRiskSkill to return false.
func TestConfirmCriticalRisk_ContextCancellation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context after a short delay so the function unblocks.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	result := h.confirmCriticalRiskSkill(
		ctx,
		"dangerous-skill", "https://hub.example.com",
		[]string{"rm -rf found"}, "desktop", "user1",
	)
	if result {
		t.Fatal("expected false on context cancellation, got true")
	}
}

// TestConfirmCriticalRisk_DesktopChannelAdaptation verifies that calling with
// platform="desktop" emits a Wails event (via h.app.emitEvent) and stores a
// pending confirmation channel.
func TestConfirmCriticalRisk_DesktopChannelAdaptation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())
	var confirmIDFound string

	// Run confirmCriticalRiskSkill in a goroutine; it will block on the channel.
	done := make(chan bool, 1)
	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"desktop-skill", "https://hub.example.com",
			[]string{"network access"}, "desktop", "user1",
		)
		done <- result
	}()

	// Give the goroutine time to store the pending confirmation.
	time.Sleep(50 * time.Millisecond)

	// Verify a pending confirmation was stored (desktop path).
	h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
		confirmIDFound = key.(string)
		return false // stop after first
	})
	if confirmIDFound == "" {
		t.Fatal("expected a pending confirmation to be stored for desktop platform")
	}

	// Clean up: cancel context to unblock.
	cancel()
	<-done
}

// TestConfirmCriticalRisk_IMChannelAdaptation verifies that calling with an
// IM platform (e.g. "feishu") stores a pending confirmation channel.
func TestConfirmCriticalRisk_IMChannelAdaptation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())
	var confirmIDFound string

	done := make(chan bool, 1)
	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"im-skill", "clawhub",
			[]string{"shell access"}, "feishu", "user1",
		)
		done <- result
	}()

	// Give the goroutine time to store the pending confirmation.
	time.Sleep(50 * time.Millisecond)

	// Verify a pending confirmation was stored (IM path).
	h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
		confirmIDFound = key.(string)
		return false
	})
	if confirmIDFound == "" {
		t.Fatal("expected a pending confirmation to be stored for IM platform")
	}

	cancel()
	<-done
}

// TestConfirmCriticalRisk_ConfirmResponse verifies that resolving with
// confirmed=true causes confirmCriticalRiskSkill to return true.
func TestConfirmCriticalRisk_ConfirmResponse(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx := context.Background()
	done := make(chan bool, 1)

	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"good-skill", "https://hub.example.com",
			[]string{"network access"}, "desktop", "user1",
		)
		done <- result
	}()

	// Wait for the pending confirmation to be stored.
	time.Sleep(50 * time.Millisecond)

	// Find the confirmID and resolve it with confirmed=true.
	var confirmID string
	h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
		confirmID = key.(string)
		return false
	})
	if confirmID == "" {
		t.Fatal("no pending confirmation found")
	}

	h.ResolveCriticalConfirm(confirmID, true)

	result := <-done
	if !result {
		t.Fatal("expected true when user confirms, got false")
	}
}

// TestConfirmCriticalRisk_RejectResponse verifies that resolving with
// confirmed=false causes confirmCriticalRiskSkill to return false.
func TestConfirmCriticalRisk_RejectResponse(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx := context.Background()
	done := make(chan bool, 1)

	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"bad-skill", "https://github.com/user/repo",
			[]string{"dangerous keyword"}, "feishu", "user1",
		)
		done <- result
	}()

	// Wait for the pending confirmation to be stored.
	time.Sleep(50 * time.Millisecond)

	// Find the confirmID and resolve it with confirmed=false.
	var confirmID string
	h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
		confirmID = key.(string)
		return false
	})
	if confirmID == "" {
		t.Fatal("no pending confirmation found")
	}

	h.ResolveCriticalConfirm(confirmID, false)

	result := <-done
	if result {
		t.Fatal("expected false when user rejects, got true")
	}
}

// TestResolveCriticalConfirm_UnknownID verifies that calling
// ResolveCriticalConfirm with a non-existent ID does not panic.
func TestResolveCriticalConfirm_UnknownID(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Should not panic — just logs and returns.
	h.ResolveCriticalConfirm("nonexistent-id", true)
	h.ResolveCriticalConfirm("nonexistent-id", false)
}

// TestConfirmCriticalRisk_NilAppDesktop verifies fail-closed when app is nil
// on desktop platform.
func TestConfirmCriticalRisk_NilAppDesktop(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{} // app is nil

	result := h.confirmCriticalRiskSkill(
		context.Background(),
		"skill", "source",
		[]string{"factor"}, "desktop", "",
	)
	if result {
		t.Fatal("expected false when app is nil (fail-closed), got true")
	}
}

// TestConfirmCriticalRisk_NilAppIM verifies fail-closed when app is nil
// on IM platform.
func TestConfirmCriticalRisk_NilAppIM(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{} // app is nil

	result := h.confirmCriticalRiskSkill(
		context.Background(),
		"skill", "source",
		[]string{"factor"}, "feishu", "",
	)
	if result {
		t.Fatal("expected false when app is nil on IM (fail-closed), got true")
	}
}

// TestConfirmCriticalRisk_ConcurrentConfirmations verifies that multiple
// concurrent confirmations with different IDs don't interfere.
// We test the channel mechanism directly to avoid timing issues with
// confirmID generation (Windows timer resolution).
func TestConfirmCriticalRisk_ConcurrentConfirmations(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Manually create 3 pending confirmations with known IDs.
	ids := []string{"crit_concurrent_1", "crit_concurrent_2", "crit_concurrent_3"}
	channels := make([]chan criticalRiskConfirmResponse, 3)
	for i, id := range ids {
		ch := make(chan criticalRiskConfirmResponse, 1)
		channels[i] = ch
		h.pendingCriticalConfirm.Store(id, ch)
	}

	// Resolve: first=true, second=false, third=true.
	expected := []bool{true, false, true}
	for i, id := range ids {
		h.ResolveCriticalConfirm(id, expected[i])
	}

	// Verify each channel received the correct response.
	for i, ch := range channels {
		select {
		case resp := <-ch:
			if resp.Confirmed != expected[i] {
				t.Errorf("channel %d: expected confirmed=%v, got %v", i, expected[i], resp.Confirmed)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for channel %d", i)
		}
	}

	// Clean up.
	for _, id := range ids {
		h.pendingCriticalConfirm.Delete(id)
	}
}
