package httpapi

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type providerConcurrencySnapshot struct {
	MaxConcurrency  int
	MaxQueueWaiters int
	QueueTimeoutMS  int
	InFlight        int
	QueueWaiters    int
}

type providerConcurrencyState struct {
	maxConcurrency  int
	maxQueueWaiters int
	queueTimeoutMS  int
	sema            chan struct{}
	inFlight        int
	queueWaiters    int
}

type providerConcurrencyErrorKind string

const (
	providerConcurrencyQueueFull     providerConcurrencyErrorKind = "queue_full"
	providerConcurrencyQueueTimeout  providerConcurrencyErrorKind = "queue_timeout"
	providerConcurrencyQueueCanceled providerConcurrencyErrorKind = "queue_canceled"
)

type providerConcurrencyError struct {
	ProviderID string
	Kind       providerConcurrencyErrorKind
	Err        error
}

func (e *providerConcurrencyError) Error() string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case providerConcurrencyQueueFull:
		return fmt.Sprintf("provider %q queue is full", e.ProviderID)
	case providerConcurrencyQueueTimeout:
		return fmt.Sprintf("provider %q queue wait timed out", e.ProviderID)
	default:
		if e.Err != nil {
			return fmt.Sprintf("provider %q queue wait canceled: %v", e.ProviderID, e.Err)
		}
		return fmt.Sprintf("provider %q queue wait canceled", e.ProviderID)
	}
}

func newProviderConcurrencyController() *providerConcurrencyController {
	return &providerConcurrencyController{states: map[string]*providerConcurrencyState{}}
}

type providerConcurrencyController struct {
	mu     sync.Mutex
	states map[string]*providerConcurrencyState
}

var globalProviderConcurrency = newProviderConcurrencyController()

func (c *providerConcurrencyController) acquire(ctx context.Context, providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) (func(), error) {
	if maxConcurrency <= 0 {
		return func() {}, nil
	}
	state := c.stateForProvider(providerID, maxConcurrency, maxQueueWaiters, queueTimeoutMS)
	c.mu.Lock()
	if len(state.sema) < cap(state.sema) {
		state.sema <- struct{}{}
		state.inFlight++
		c.mu.Unlock()
		return c.releaseFunc(state), nil
	}
	if maxQueueWaiters > 0 && state.queueWaiters >= maxQueueWaiters {
		c.mu.Unlock()
		return nil, &providerConcurrencyError{ProviderID: providerID, Kind: providerConcurrencyQueueFull}
	}
	state.queueWaiters++
	c.mu.Unlock()

	waitCtx := ctx
	cancel := func() {}
	if queueTimeoutMS > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, time.Duration(queueTimeoutMS)*time.Millisecond)
	}
	defer cancel()

	select {
	case state.sema <- struct{}{}:
		c.mu.Lock()
		if state.queueWaiters > 0 {
			state.queueWaiters--
		}
		state.inFlight++
		c.mu.Unlock()
		return c.releaseFunc(state), nil
	case <-waitCtx.Done():
		c.mu.Lock()
		if state.queueWaiters > 0 {
			state.queueWaiters--
		}
		c.mu.Unlock()
		if ctx.Err() != nil {
			return nil, &providerConcurrencyError{ProviderID: providerID, Kind: providerConcurrencyQueueCanceled, Err: ctx.Err()}
		}
		return nil, &providerConcurrencyError{ProviderID: providerID, Kind: providerConcurrencyQueueTimeout}
	}
}

func (c *providerConcurrencyController) releaseFunc(state *providerConcurrencyState) func() {
	return func() {
		<-state.sema
		c.mu.Lock()
		if state.inFlight > 0 {
			state.inFlight--
		}
		c.mu.Unlock()
	}
}

func (c *providerConcurrencyController) snapshot(providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) providerConcurrencySnapshot {
	if maxConcurrency <= 0 {
		return providerConcurrencySnapshot{}
	}
	state := c.stateForProvider(providerID, maxConcurrency, maxQueueWaiters, queueTimeoutMS)
	c.mu.Lock()
	defer c.mu.Unlock()
	return providerConcurrencySnapshot{
		MaxConcurrency:  state.maxConcurrency,
		MaxQueueWaiters: state.maxQueueWaiters,
		QueueTimeoutMS:  state.queueTimeoutMS,
		InFlight:        state.inFlight,
		QueueWaiters:    state.queueWaiters,
	}
}

func (c *providerConcurrencyController) stateForProvider(providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) *providerConcurrencyState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil || state.maxConcurrency != maxConcurrency || state.maxQueueWaiters != maxQueueWaiters || state.queueTimeoutMS != queueTimeoutMS {
		state = &providerConcurrencyState{
			maxConcurrency:  maxConcurrency,
			maxQueueWaiters: maxQueueWaiters,
			queueTimeoutMS:  queueTimeoutMS,
			sema:            make(chan struct{}, maxConcurrency),
		}
		c.states[providerID] = state
	}
	return state
}
