package llmpool

import (
	"context"
	"strings"
	"testing"
	"time"
)

// openCircuitWithProbeInFlight opens the circuit for p1 with a short cooldown,
// waits for the cooldown to expire, and admits a single probe attempt so the
// controller is left with probeInFlight set.
func openCircuitWithProbeInFlight(t *testing.T, c *ResilienceController, cooldownMS int) {
	t.Helper()
	c.RecordFailureBackoff("p1", 1, cooldownMS, cooldownMS*5)
	if err := c.BeforeAttempt("p1", 1, cooldownMS); err == nil {
		t.Fatal("expected open circuit during cooldown")
	}
	deadline := time.Now().Add(2 * time.Second)
	for c.BeforeAttempt("p1", 1, cooldownMS) != nil {
		if time.Now().After(deadline) {
			t.Fatal("cooldown did not expire")
		}
		time.Sleep(5 * time.Millisecond)
	}
	err := c.BeforeAttempt("p1", 1, cooldownMS)
	re, ok := err.(*ResilienceError)
	if !ok || re.State != "probe" {
		t.Fatalf("expected probe-in-flight rejection for concurrent caller, got %v", err)
	}
}

func TestBeforeAttemptWithProbeWaitSettlesOnSuccess(t *testing.T) {
	c := NewResilienceController()
	openCircuitWithProbeInFlight(t, c, 40)
	go func() {
		time.Sleep(80 * time.Millisecond)
		c.RecordSuccess("p1")
	}()
	start := time.Now()
	if err := c.BeforeAttemptWithProbeWait(context.Background(), "p1", 1, 40, 2*time.Second); err != nil {
		t.Fatalf("wait should settle once the probe succeeds: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wait took %v, expected prompt settle after probe success", elapsed)
	}
}

func TestBeforeAttemptWithProbeWaitSettlesOnFailure(t *testing.T) {
	c := NewResilienceController()
	openCircuitWithProbeInFlight(t, c, 40)
	go func() {
		time.Sleep(80 * time.Millisecond)
		// Re-open with a long cooldown so the settling check observes the
		// open state instead of becoming the next probe admission.
		c.RecordFailureBackoff("p1", 1, 5000, 30000)
	}()
	err := c.BeforeAttemptWithProbeWait(context.Background(), "p1", 1, 40, 2*time.Second)
	if err == nil {
		t.Fatal("expected error after probe failure reopened the circuit")
	}
	re, ok := err.(*ResilienceError)
	if !ok || re.State != "open" || re.CooldownLeft <= 0 {
		t.Fatalf("expected open error with positive cooldown, got %#v", err)
	}
}

func TestBeforeAttemptWithProbeWaitTimeoutKeepsProbeSlot(t *testing.T) {
	c := NewResilienceController()
	openCircuitWithProbeInFlight(t, c, 40)
	start := time.Now()
	err := c.BeforeAttemptWithProbeWait(context.Background(), "p1", 1, 40, 120*time.Millisecond)
	if re, ok := err.(*ResilienceError); !ok || re.State != "probe" {
		t.Fatalf("expected probe error on wait timeout, got %#v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wait exceeded probeWait budget: %v", elapsed)
	}
	// The timed-out waiter must not have consumed the in-flight probe slot.
	if err := c.BeforeAttempt("p1", 1, 40); err == nil {
		t.Fatal("timed-out waiter must leave the probe slot held for the real probe")
	}
}

func TestBeforeAttemptWithProbeWaitDisabledReturnsImmediately(t *testing.T) {
	c := NewResilienceController()
	openCircuitWithProbeInFlight(t, c, 40)
	start := time.Now()
	err := c.BeforeAttemptWithProbeWait(context.Background(), "p1", 1, 40, 0)
	if re, ok := err.(*ResilienceError); !ok || re.State != "probe" {
		t.Fatalf("expected immediate probe error with zero probeWait, got %#v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("zero probeWait should not block, took %v", elapsed)
	}
	c.AbortProbe("p1")
	if err := c.BeforeAttempt("p1", 1, 40); err != nil {
		t.Fatalf("abort should release probe slot: %v", err)
	}
}

func TestResilienceProbeErrorMessage(t *testing.T) {
	c := NewResilienceController()
	openCircuitWithProbeInFlight(t, c, 40)
	err := c.BeforeAttempt("p1", 1, 40)
	if err == nil {
		t.Fatal("expected probe error")
	}
	if msg := err.Error(); !strings.Contains(msg, "probe in flight") {
		t.Fatalf("probe error should explain the in-flight probe, got %q", msg)
	}
	if msg := err.Error(); strings.Contains(msg, "cooldown 0s") {
		t.Fatalf("probe error must not claim a zero cooldown, got %q", msg)
	}
	c.AbortProbe("p1")
}
