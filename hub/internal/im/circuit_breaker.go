package im

import (
	"sync"
	"time"
)

// CircuitBreaker protects LLM calls with a simple consecutive-failure circuit
// breaker. After `threshold` consecutive failures the breaker opens for
// `cooldown` duration, during which all calls are rejected. After cooldown
// one probe call is allowed; if it succeeds the breaker closes, otherwise
// it re-opens.
type CircuitBreaker struct {
	mu               sync.Mutex
	consecutiveFails int
	openUntil        time.Time
	threshold        int
	cooldown         time.Duration
}

// NewCircuitBreaker creates a breaker with the given threshold and cooldown.
func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// DefaultCircuitBreaker returns a breaker with 3-failure threshold and 5min cooldown.
func DefaultCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreaker(3, 5*time.Minute)
}

// Allow returns true if a call is permitted (breaker closed or cooldown expired).
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.consecutiveFails < cb.threshold {
		return true
	}
	// Breaker is open — check if cooldown has elapsed.
	return time.Now().After(cb.openUntil)
}

// RecordSuccess resets the failure counter (closes the breaker).
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	cb.consecutiveFails = 0
	cb.openUntil = time.Time{}
	cb.mu.Unlock()
}

// RecordFailure increments the failure counter. When the threshold is reached
// the breaker opens for the configured cooldown duration.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	cb.consecutiveFails++
	if cb.consecutiveFails >= cb.threshold {
		cb.openUntil = time.Now().Add(cb.cooldown)
	}
	cb.mu.Unlock()
}

// IsOpen returns true if the breaker is currently in the open state and
// the cooldown has NOT yet elapsed.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.consecutiveFails < cb.threshold {
		return false
	}
	return time.Now().Before(cb.openUntil)
}

// Status returns a human-readable status: "normal" or "circuit_open".
func (cb *CircuitBreaker) Status() string {
	if cb.IsOpen() {
		return "circuit_open"
	}
	return "normal"
}
