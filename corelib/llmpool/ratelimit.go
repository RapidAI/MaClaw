package llmpool

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Concurrency Control
// ---------------------------------------------------------------------------

// ConcurrencyError is returned when a provider's concurrency limit is exceeded.
type ConcurrencyError struct {
	ProviderID  string
	Reason      string
	QueueFull   bool
	QueueExpiry bool
}

func (e *ConcurrencyError) Error() string {
	return fmt.Sprintf("llmpool: concurrency limit for provider %s: %s", e.ProviderID, e.Reason)
}

// ConcurrencySnapshot captures the instantaneous state of a provider's concurrency.
type ConcurrencySnapshot struct {
	Active         int `json:"active"`
	MaxConcurrency int `json:"max_concurrency"`
	QueueWaiting   int `json:"queue_waiting"`
	MaxQueueSize   int `json:"max_queue_size"`
}

// ConcurrencyController manages per-provider concurrency limits with a
// semaphore-style approach: requests beyond MaxConcurrency wait in a queue;
// if the queue exceeds MaxQueueWaiters, the request is rejected immediately.
type ConcurrencyController struct {
	mu     sync.Mutex
	states map[string]*concurrencyState
}

type concurrencyState struct {
	active  int
	ch      chan struct{}
	waiters int
}

// NewConcurrencyController creates a new per-provider concurrency controller.
func NewConcurrencyController() *ConcurrencyController {
	return &ConcurrencyController{states: map[string]*concurrencyState{}}
}

// Acquire blocks until a slot is available for the given provider, or returns
// an error if the queue is full or times out. On success, returns a release
// function that must be called when the request completes.
func (c *ConcurrencyController) Acquire(ctx context.Context, providerID string, maxConcurrency, maxQueueWaiters, queueTimeoutMS int) (func(), error) {
	if maxConcurrency <= 0 {
		return func() {}, nil
	}

	c.mu.Lock()
	state := c.stateFor(providerID, maxConcurrency)

	// Fast path: slot available
	if state.active < maxConcurrency {
		state.active++
		c.mu.Unlock()
		return c.releaseFunc(state), nil
	}

	// Queue full?
	if maxQueueWaiters > 0 && state.waiters >= maxQueueWaiters {
		c.mu.Unlock()
		return nil, &ConcurrencyError{ProviderID: providerID, Reason: "queue full", QueueFull: true}
	}

	state.waiters++
	c.mu.Unlock()

	// Wait for a slot
	timeout := time.Duration(queueTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		c.mu.Lock()
		state.waiters--
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-timer.C:
		c.mu.Lock()
		state.waiters--
		c.mu.Unlock()
		return nil, &ConcurrencyError{ProviderID: providerID, Reason: "queue timeout", QueueExpiry: true}
	case <-state.ch:
		c.mu.Lock()
		state.waiters--
		state.active++
		c.mu.Unlock()
		return c.releaseFunc(state), nil
	}
}

// TryAcquire takes a concurrency slot immediately. maxConcurrency <= 0 means
// unlimited. If the provider is already at its limit, it returns a
// ConcurrencyError without waiting so the caller can fail over.
func (c *ConcurrencyController) TryAcquire(providerID string, maxConcurrency int) (func(), error) {
	if c == nil || maxConcurrency <= 0 {
		return func() {}, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.stateFor(providerID, maxConcurrency)
	if state.active >= maxConcurrency {
		return nil, &ConcurrencyError{ProviderID: providerID, Reason: "busy", QueueFull: true}
	}
	state.active++
	return c.releaseFunc(state), nil
}

// Snapshot returns the current concurrency state for a provider.
func (c *ConcurrencyController) Snapshot(providerID string, maxConcurrency, maxQueueWaiters, queueTimeoutMS int) ConcurrencySnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.stateFor(providerID, maxConcurrency)
	return ConcurrencySnapshot{
		Active:         state.active,
		MaxConcurrency: maxConcurrency,
		QueueWaiting:   state.waiters,
		MaxQueueSize:   maxQueueWaiters,
	}
}

// Reset clears all concurrency state (for testing).
func (c *ConcurrencyController) Reset() {
	c.mu.Lock()
	c.states = map[string]*concurrencyState{}
	c.mu.Unlock()
}

func (c *ConcurrencyController) releaseFunc(state *concurrencyState) func() {
	return func() {
		c.mu.Lock()
		state.active--
		// Signal one waiter if available
		select {
		case state.ch <- struct{}{}:
		default:
		}
		c.mu.Unlock()
	}
}

func (c *ConcurrencyController) stateFor(providerID string, maxConcurrency int) *concurrencyState {
	state := c.states[providerID]
	if state == nil {
		bufSize := maxConcurrency
		if bufSize <= 0 {
			bufSize = 1
		}
		state = &concurrencyState{ch: make(chan struct{}, bufSize)}
		c.states[providerID] = state
	}
	return state
}

// ---------------------------------------------------------------------------
// Resilience (Circuit Breaker)
// ---------------------------------------------------------------------------

// ResilienceError is returned when a provider is in circuit-open state.
type ResilienceError struct {
	ProviderID   string
	State        string // "open" / "backoff"
	CooldownLeft time.Duration
}

func (e *ResilienceError) Error() string {
	return fmt.Sprintf("llmpool: provider %s circuit %s (cooldown %v)", e.ProviderID, e.State, e.CooldownLeft)
}

// ResilienceSnapshot captures the circuit breaker state.
type ResilienceSnapshot struct {
	State          string    `json:"state"` // "closed" / "open" / "half_open"
	ConsecFailures int       `json:"consec_failures"`
	Threshold      int       `json:"threshold"`
	LastFailureAt  time.Time `json:"last_failure_at,omitempty"`
	CooldownEndsAt time.Time `json:"cooldown_ends_at,omitempty"`
}

// ResilienceController implements a per-provider circuit breaker with
// configurable failure thresholds and cooldown periods.
type ResilienceController struct {
	mu     sync.Mutex
	states map[string]*resilienceState
}

type resilienceState struct {
	consecFailures int
	lastFailureAt  time.Time
	cooldownEndsAt time.Time
	halfOpen       bool
	probeInFlight  bool
}

const (
	defaultResilienceCooldown   = 10 * time.Second
	defaultResilienceMaxBackoff = 5 * time.Minute
	maxResilienceBackoffShift   = 8
)

// NewResilienceController creates a new circuit breaker controller.
func NewResilienceController() *ResilienceController {
	return &ResilienceController{states: map[string]*resilienceState{}}
}

// BeforeAttempt checks if a provider is available. Returns ResilienceError
// if the circuit is open and the cooldown hasn't expired. After cooldown, only
// one in-flight probe is allowed so a recovering provider is not stampeded.
func (c *ResilienceController) BeforeAttempt(providerID string, threshold, cooldownMS int) error {
	if c == nil || threshold <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil {
		return nil
	}
	if state.consecFailures < threshold {
		return nil
	}
	now := time.Now()
	if now.Before(state.cooldownEndsAt) {
		return &ResilienceError{
			ProviderID:   providerID,
			State:        "open",
			CooldownLeft: state.cooldownEndsAt.Sub(now),
		}
	}
	if state.probeInFlight {
		return &ResilienceError{
			ProviderID: providerID,
			State:      "probe",
		}
	}
	state.halfOpen = true
	state.probeInFlight = true
	return nil
}

// AbortProbe releases a half-open probe slot without counting a failure.
// Use when the attempt never reached the provider (for example concurrency skip).
func (c *ResilienceController) AbortProbe(providerID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil || !state.probeInFlight {
		return
	}
	state.probeInFlight = false
}

// RecordSuccess resets the circuit breaker for a provider.
func (c *ResilienceController) RecordSuccess(providerID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil {
		return
	}
	state.consecFailures = 0
	state.halfOpen = false
	state.probeInFlight = false
	state.cooldownEndsAt = time.Time{}
}

// RecordFailure increments the failure counter and potentially opens the circuit.
func (c *ResilienceController) RecordFailure(providerID string, threshold, cooldownMS int) {
	c.RecordFailureBackoff(providerID, threshold, cooldownMS, 0)
}

// RecordFailureBackoff is RecordFailure with an explicit exponential cooldown cap.
func (c *ResilienceController) RecordFailureBackoff(providerID string, threshold, baseMS, maxMS int) {
	if c == nil || threshold <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil {
		state = &resilienceState{}
		c.states[providerID] = state
	}
	state.consecFailures++
	state.lastFailureAt = time.Now()
	state.halfOpen = false
	state.probeInFlight = false
	if state.consecFailures < threshold {
		return
	}
	opens := state.consecFailures - threshold + 1
	state.cooldownEndsAt = state.lastFailureAt.Add(resilienceCooldown(opens, baseMS, maxMS))
}

func resilienceCooldown(opens, baseMS, maxMS int) time.Duration {
	base := time.Duration(baseMS) * time.Millisecond
	if base <= 0 {
		base = defaultResilienceCooldown
	}
	max := time.Duration(maxMS) * time.Millisecond
	if max <= 0 {
		max = defaultResilienceMaxBackoff
	}
	if max < base {
		max = base
	}
	shift := opens - 1
	if shift < 0 {
		shift = 0
	}
	if shift > maxResilienceBackoffShift {
		shift = maxResilienceBackoffShift
	}
	cooldown := base << shift
	if cooldown > max || cooldown < base {
		return max
	}
	return cooldown
}

// Snapshot returns the current resilience state.
func (c *ResilienceController) Snapshot(providerID string, threshold int) ResilienceSnapshot {
	if c == nil {
		return ResilienceSnapshot{Threshold: threshold, State: "closed"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	snap := ResilienceSnapshot{Threshold: threshold}
	if state == nil {
		snap.State = "closed"
		return snap
	}
	snap.ConsecFailures = state.consecFailures
	snap.LastFailureAt = state.lastFailureAt
	snap.CooldownEndsAt = state.cooldownEndsAt
	if threshold > 0 && state.consecFailures < threshold {
		snap.State = "closed"
	} else if !state.cooldownEndsAt.IsZero() && time.Now().Before(state.cooldownEndsAt) {
		snap.State = "open"
	} else if state.probeInFlight || state.halfOpen {
		snap.State = "half_open"
	} else if threshold > 0 && state.consecFailures >= threshold {
		snap.State = "half_open"
	} else {
		snap.State = "closed"
	}
	return snap
}

// Reset clears all resilience state (for testing).
func (c *ResilienceController) Reset() {
	c.mu.Lock()
	c.states = map[string]*resilienceState{}
	c.mu.Unlock()
}
