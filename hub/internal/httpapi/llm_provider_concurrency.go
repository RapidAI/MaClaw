package httpapi

import (
	"context"
	"fmt"
	"sync"
)

type providerConcurrencySnapshot struct {
	MaxConcurrency int
	InFlight       int
	QueueWaiters   int
}

type providerConcurrencyState struct {
	maxConcurrency int
	sema           chan struct{}
	inFlight       int
	queueWaiters   int
}

type providerConcurrencyController struct {
	mu     sync.Mutex
	states map[string]*providerConcurrencyState
}

func newProviderConcurrencyController() *providerConcurrencyController {
	return &providerConcurrencyController{states: map[string]*providerConcurrencyState{}}
}

var globalProviderConcurrency = newProviderConcurrencyController()

func (c *providerConcurrencyController) acquire(ctx context.Context, providerID string, maxConcurrency int) (func(), error) {
	if maxConcurrency <= 0 {
		return func() {}, nil
	}
	state := c.stateForProvider(providerID, maxConcurrency)
	c.mu.Lock()
	state.queueWaiters++
	c.mu.Unlock()

	select {
	case state.sema <- struct{}{}:
		c.mu.Lock()
		if state.queueWaiters > 0 {
			state.queueWaiters--
		}
		state.inFlight++
		c.mu.Unlock()
		return func() {
			<-state.sema
			c.mu.Lock()
			if state.inFlight > 0 {
				state.inFlight--
			}
			c.mu.Unlock()
		}, nil
	case <-ctx.Done():
		c.mu.Lock()
		if state.queueWaiters > 0 {
			state.queueWaiters--
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("provider %q queue wait canceled: %w", providerID, ctx.Err())
	}
}

func (c *providerConcurrencyController) snapshot(providerID string, maxConcurrency int) providerConcurrencySnapshot {
	if maxConcurrency <= 0 {
		return providerConcurrencySnapshot{}
	}
	state := c.stateForProvider(providerID, maxConcurrency)
	c.mu.Lock()
	defer c.mu.Unlock()
	return providerConcurrencySnapshot{
		MaxConcurrency: state.maxConcurrency,
		InFlight:       state.inFlight,
		QueueWaiters:   state.queueWaiters,
	}
}

func (c *providerConcurrencyController) stateForProvider(providerID string, maxConcurrency int) *providerConcurrencyState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil || state.maxConcurrency != maxConcurrency {
		state = &providerConcurrencyState{
			maxConcurrency: maxConcurrency,
			sema:           make(chan struct{}, maxConcurrency),
		}
		c.states[providerID] = state
	}
	return state
}
