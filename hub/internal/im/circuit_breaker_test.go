package im

import (
	"testing"
	"time"
)

func TestCircuitBreaker_AllowWhenClosed(t *testing.T) {
	cb := DefaultCircuitBreaker()
	if !cb.Allow() {
		t.Fatal("expected Allow()=true for fresh breaker")
	}
	if cb.IsOpen() {
		t.Fatal("expected IsOpen()=false for fresh breaker")
	}
	if s := cb.Status(); s != "normal" {
		t.Fatalf("expected status=normal, got %s", s)
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := DefaultCircuitBreaker() // threshold=3, cooldown=5min
	cb.RecordFailure()
	cb.RecordFailure()
	if !cb.Allow() {
		t.Fatal("expected Allow()=true after 2 failures (below threshold)")
	}
	cb.RecordFailure() // 3rd failure → opens
	if !cb.IsOpen() {
		t.Fatal("expected IsOpen()=true after 3 consecutive failures")
	}
	if cb.Allow() {
		t.Fatal("expected Allow()=false when breaker is open")
	}
	if s := cb.Status(); s != "circuit_open" {
		t.Fatalf("expected status=circuit_open, got %s", s)
	}
}

func TestCircuitBreaker_RecordSuccessResets(t *testing.T) {
	cb := DefaultCircuitBreaker()
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // reset
	cb.RecordFailure()
	cb.RecordFailure()
	// Only 2 failures since last success — should still be closed.
	if cb.IsOpen() {
		t.Fatal("expected breaker closed after success reset + 2 failures")
	}
	if !cb.Allow() {
		t.Fatal("expected Allow()=true")
	}
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	// Use a very short cooldown for testing.
	cb := NewCircuitBreaker(3, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if !cb.IsOpen() {
		t.Fatal("expected open after 3 failures")
	}
	// Wait for cooldown.
	time.Sleep(80 * time.Millisecond)
	// After cooldown, Allow should return true (half-open probe).
	if !cb.Allow() {
		t.Fatal("expected Allow()=true after cooldown (half-open)")
	}
}

func TestCircuitBreaker_HalfOpenProbeSuccess(t *testing.T) {
	cb := NewCircuitBreaker(3, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(80 * time.Millisecond)
	// Probe succeeds.
	cb.RecordSuccess()
	if cb.IsOpen() {
		t.Fatal("expected breaker closed after successful probe")
	}
	if s := cb.Status(); s != "normal" {
		t.Fatalf("expected status=normal after probe success, got %s", s)
	}
}

func TestCircuitBreaker_HalfOpenProbeFailure(t *testing.T) {
	cb := NewCircuitBreaker(3, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(80 * time.Millisecond)
	// Probe fails — breaker re-opens.
	cb.RecordFailure()
	if !cb.IsOpen() {
		t.Fatal("expected breaker re-opened after failed probe")
	}
}
