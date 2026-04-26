package httpapi

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

type providerResilienceErrorKind string

const (
	providerResilienceCircuitOpen providerResilienceErrorKind = "circuit_open"
	providerResilienceBackingOff  providerResilienceErrorKind = "backing_off"
)

type providerResilienceError struct {
	ProviderID string
	Kind       providerResilienceErrorKind
	RetryAfter time.Duration
}

func (e *providerResilienceError) Error() string {
	if e == nil {
		return ""
	}
	retryAfter := e.RetryAfter.Round(time.Millisecond)
	switch e.Kind {
	case providerResilienceCircuitOpen:
		return fmt.Sprintf("provider %q circuit is open for %s", e.ProviderID, retryAfter)
	default:
		return fmt.Sprintf("provider %q is backing off for %s", e.ProviderID, retryAfter)
	}
}

type providerResilienceSnapshot struct {
	CircuitBreakerThreshold  int
	CircuitBreakerCooldownMS int
	FailureBackoffBaseMS     int
	FailureBackoffMaxMS      int
	ConsecutiveFailures      int
	CircuitOpen              bool
	CircuitOpenUntil         time.Time
	BackoffUntil             time.Time
}

type providerResilienceState struct {
	providerID               string
	circuitBreakerThreshold  int
	circuitBreakerCooldownMS int
	failureBackoffBaseMS     int
	failureBackoffMaxMS      int
	consecutiveFailures      int
	circuitOpenUntil         time.Time
	backoffUntil             time.Time
}

type providerResilienceController struct {
	mu     sync.Mutex
	states map[string]*providerResilienceState
}

var globalProviderResilience = newProviderResilienceController()

func newProviderResilienceController() *providerResilienceController {
	return &providerResilienceController{states: map[string]*providerResilienceState{}}
}

func (c *providerResilienceController) beforeAttempt(p *im.LLMProvider) error {
	if p == nil {
		return nil
	}
	state := c.stateForProvider(p)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !state.circuitOpenUntil.IsZero() && now.Before(state.circuitOpenUntil) {
		return &providerResilienceError{ProviderID: state.providerID, Kind: providerResilienceCircuitOpen, RetryAfter: time.Until(state.circuitOpenUntil)}
	}
	if !state.backoffUntil.IsZero() && now.Before(state.backoffUntil) {
		return &providerResilienceError{ProviderID: state.providerID, Kind: providerResilienceBackingOff, RetryAfter: time.Until(state.backoffUntil)}
	}
	return nil
}

func (c *providerResilienceController) recordSuccess(providerID string) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil {
		return
	}
	state.consecutiveFailures = 0
	state.circuitOpenUntil = time.Time{}
	state.backoffUntil = time.Time{}
}

func (c *providerResilienceController) recordFailure(p *im.LLMProvider) {
	if p == nil {
		return
	}
	state := c.stateForProvider(p)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	state.consecutiveFailures++
	if state.failureBackoffBaseMS > 0 {
		delay := time.Duration(state.failureBackoffBaseMS) * time.Millisecond
		for i := 1; i < state.consecutiveFailures; i++ {
			delay *= 2
			maxDelay := time.Duration(state.failureBackoffMaxMS) * time.Millisecond
			if maxDelay > 0 && delay >= maxDelay {
				delay = maxDelay
				break
			}
		}
		if delay > 0 {
			state.backoffUntil = now.Add(delay)
		}
	}
	if state.circuitBreakerThreshold > 0 && state.consecutiveFailures >= state.circuitBreakerThreshold {
		cooldown := time.Duration(state.circuitBreakerCooldownMS) * time.Millisecond
		if cooldown > 0 {
			state.circuitOpenUntil = now.Add(cooldown)
		}
	}
}

func (c *providerResilienceController) snapshot(p *im.LLMProvider) providerResilienceSnapshot {
	if p == nil {
		return providerResilienceSnapshot{}
	}
	state := c.stateForProvider(p)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	return providerResilienceSnapshot{
		CircuitBreakerThreshold:  state.circuitBreakerThreshold,
		CircuitBreakerCooldownMS: state.circuitBreakerCooldownMS,
		FailureBackoffBaseMS:     state.failureBackoffBaseMS,
		FailureBackoffMaxMS:      state.failureBackoffMaxMS,
		ConsecutiveFailures:      state.consecutiveFailures,
		CircuitOpen:              !state.circuitOpenUntil.IsZero() && now.Before(state.circuitOpenUntil),
		CircuitOpenUntil:         state.circuitOpenUntil,
		BackoffUntil:             state.backoffUntil,
	}
}

func (c *providerResilienceController) reset() {
	c.mu.Lock()
	c.states = map[string]*providerResilienceState{}
	c.mu.Unlock()
}

func (c *providerResilienceController) stateForProvider(p *im.LLMProvider) *providerResilienceState {
	providerID := strings.TrimSpace(p.ID)
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil {
		state = &providerResilienceState{providerID: providerID}
		c.states[providerID] = state
	}
	state.circuitBreakerThreshold = p.CircuitBreakerThreshold
	state.circuitBreakerCooldownMS = p.CircuitBreakerCooldownMS
	state.failureBackoffBaseMS = p.FailureBackoffBaseMS
	state.failureBackoffMaxMS = p.FailureBackoffMaxMS
	if state.failureBackoffMaxMS < state.failureBackoffBaseMS {
		state.failureBackoffMaxMS = state.failureBackoffBaseMS
	}
	return state
}

func shouldCountProviderFailure(statusCode int, err error) bool {
	if err != nil {
		return true
	}
	switch statusCode {
	case 0:
		return false
	case 401, 403, 404, 408, 429:
		return true
	default:
		return statusCode >= 500
	}
}
