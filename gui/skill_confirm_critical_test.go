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
// We test the channel-close mechanism directly with a short timer to avoid
// waiting 120s.
func TestConfirmCriticalRisk_TimeoutReturnsFalse(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Create a pending entry (same type as confirmCriticalRiskSkill uses).
	entry := &pendingCriticalConfirmEntry{
		Ch: make(chan criticalRiskConfirmResponse, 1),
	}
	confirmID := "test_timeout_1"
	h.pendingCriticalConfirm.Store(confirmID, entry)

	// Simulate cleanup goroutine closing the channel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		if entry.tryResolve() {
			h.pendingCriticalConfirm.Delete(confirmID)
			close(entry.Ch)
		}
	}()

	// Read from channel — should get ok=false (closed).
	select {
	case _, ok := <-entry.Ch:
		if ok {
			t.Fatal("expected channel close (ok=false), got a value")
		}
		// ok=false — timeout path, correct.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel close")
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
// platform="desktop" stores a pending confirmation entry.
func TestConfirmCriticalRisk_DesktopChannelAdaptation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"desktop-skill", "https://hub.example.com",
			[]string{"network access"}, "desktop", "user1",
		)
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)

	var confirmIDFound string
	h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
		confirmIDFound = key.(string)
		return false
	})
	if confirmIDFound == "" {
		t.Fatal("expected a pending confirmation to be stored for desktop platform")
	}

	cancel()
	<-done
}

// TestConfirmCriticalRisk_IMChannelAdaptation verifies that calling with an
// IM platform stores a pending confirmation entry.
func TestConfirmCriticalRisk_IMChannelAdaptation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"im-skill", "clawhub",
			[]string{"shell access"}, "feishu", "user1",
		)
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)

	var confirmIDFound string
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

	time.Sleep(50 * time.Millisecond)

	var confirmID string
	h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
		confirmID = key.(string)
		return false
	})
	if confirmID == "" {
		t.Fatal("no pending confirmation found")
	}

	if err := h.ResolveCriticalConfirm(confirmID, true); err != nil {
		t.Fatalf("ResolveCriticalConfirm returned error: %v", err)
	}

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

	time.Sleep(50 * time.Millisecond)

	var confirmID string
	h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
		confirmID = key.(string)
		return false
	})
	if confirmID == "" {
		t.Fatal("no pending confirmation found")
	}

	if err := h.ResolveCriticalConfirm(confirmID, false); err != nil {
		t.Fatalf("ResolveCriticalConfirm returned error: %v", err)
	}

	result := <-done
	if result {
		t.Fatal("expected false when user rejects, got true")
	}
}

// TestResolveCriticalConfirm_UnknownID verifies that calling
// ResolveCriticalConfirm with a non-existent ID returns an error.
func TestResolveCriticalConfirm_UnknownID(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	err := h.ResolveCriticalConfirm("nonexistent-id", true)
	if err == nil {
		t.Fatal("expected error for unknown confirmID, got nil")
	}
	err = h.ResolveCriticalConfirm("nonexistent-id", false)
	if err == nil {
		t.Fatal("expected error for unknown confirmID, got nil")
	}
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
func TestConfirmCriticalRisk_ConcurrentConfirmations(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Create 3 pending entries with known IDs.
	ids := []string{"crit_concurrent_1", "crit_concurrent_2", "crit_concurrent_3"}
	entries := make([]*pendingCriticalConfirmEntry, 3)
	for i, id := range ids {
		entry := &pendingCriticalConfirmEntry{
			Ch: make(chan criticalRiskConfirmResponse, 1),
		}
		entries[i] = entry
		h.pendingCriticalConfirm.Store(id, entry)
	}

	// Resolve: first=true, second=false, third=true.
	expected := []bool{true, false, true}
	for i, id := range ids {
		if err := h.ResolveCriticalConfirm(id, expected[i]); err != nil {
			t.Fatalf("ResolveCriticalConfirm(%s) returned error: %v", id, err)
		}
	}

	// Verify each channel received the correct response.
	for i, entry := range entries {
		select {
		case resp := <-entry.Ch:
			if resp.Confirmed != expected[i] {
				t.Errorf("entry %d: expected confirmed=%v, got %v", i, expected[i], resp.Confirmed)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for entry %d", i)
		}
	}
}

// TestResolveCriticalConfirm_DoubleResolve verifies that resolving the same
// confirmID twice returns an error on the second call (CAS prevents double-send).
func TestResolveCriticalConfirm_DoubleResolve(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	entry := &pendingCriticalConfirmEntry{
		Ch: make(chan criticalRiskConfirmResponse, 1),
	}
	confirmID := "test_double_resolve"
	h.pendingCriticalConfirm.Store(confirmID, entry)

	// First resolve should succeed.
	if err := h.ResolveCriticalConfirm(confirmID, true); err != nil {
		t.Fatalf("first resolve returned error: %v", err)
	}

	// Second resolve should fail — entry was deleted by the first resolve.
	err := h.ResolveCriticalConfirm(confirmID, true)
	if err == nil {
		t.Fatal("expected error on second resolve, got nil")
	}
}
