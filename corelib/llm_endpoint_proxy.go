package corelib

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type LLMEndpointProvider struct {
	ID                       string
	Name                     string
	APIURL                   string
	APIKey                   string
	Model                    string
	Protocol                 string
	WireAPI                  string
	AgentType                string
	UpstreamTimeoutSec       int
	MaxConcurrency           int
	MaxQueueWaiters          int
	QueueTimeoutMS           int
	CircuitBreakerThreshold  int
	CircuitBreakerCooldownMS int
	FailureBackoffBaseMS     int
	FailureBackoffMaxMS      int
}

type LLMEndpointForwardResult struct {
	Body       []byte
	StatusCode int
	ProviderID string
}

type LLMEndpointProxy struct {
	Concurrency *LLMProviderConcurrencyController
	Resilience  *LLMProviderResilienceController
	Client      func(MaclawLLMConfig) *http.Client
}

func NewLLMEndpointProxy() *LLMEndpointProxy {
	return &LLMEndpointProxy{
		Concurrency: NewLLMProviderConcurrencyController(),
		Resilience:  NewLLMProviderResilienceController(),
		Client:      NewLLMEndpointHTTPClient,
	}
}

func (p *LLMEndpointProxy) ForwardProviderRequest(ctx context.Context, provider LLMEndpointProvider, body map[string]any, responseModel string) (LLMEndpointForwardResult, error) {
	if strings.TrimSpace(provider.ID) == "" {
		return LLMEndpointForwardResult{}, fmt.Errorf("provider id is required")
	}
	if p == nil {
		p = NewLLMEndpointProxy()
	}
	if p.Resilience != nil {
		if err := p.Resilience.BeforeAttempt(provider); err != nil {
			return LLMEndpointForwardResult{}, err
		}
	}
	release := func() {}
	if p.Concurrency != nil {
		var err error
		release, err = p.Concurrency.Acquire(ctx, provider.ID, provider.MaxConcurrency, provider.MaxQueueWaiters, provider.QueueTimeoutMS)
		if err != nil {
			return LLMEndpointForwardResult{}, err
		}
	}
	defer release()

	cfg := provider.MaclawLLMConfig()
	clientFactory := p.Client
	if clientFactory == nil {
		clientFactory = NewLLMEndpointHTTPClient
	}
	fwd := make(map[string]any, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	respBody, statusCode, err := ForwardOpenAICompatRequest(ctx, cfg, fwd, clientFactory(cfg), responseModel)
	if p.Resilience != nil {
		if ShouldCountLLMProviderFailure(statusCode, err) {
			p.Resilience.RecordFailure(provider)
		} else {
			p.Resilience.RecordSuccess(provider.ID)
		}
	}
	if err != nil {
		return LLMEndpointForwardResult{StatusCode: statusCode, ProviderID: provider.ID}, err
	}
	return LLMEndpointForwardResult{Body: respBody, StatusCode: statusCode, ProviderID: provider.ID}, nil
}

func ForwardLLMEndpointProviderRequest(ctx context.Context, provider LLMEndpointProvider, body map[string]any, client *http.Client, responseModel string) ([]byte, int, error) {
	cfg := provider.MaclawLLMConfig()
	if client == nil {
		client = NewLLMEndpointHTTPClient(cfg)
	}
	fwd := make(map[string]any, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	return ForwardOpenAICompatRequest(ctx, cfg, fwd, client, responseModel)
}

func (p LLMEndpointProvider) MaclawLLMConfig() MaclawLLMConfig {
	return MaclawLLMConfig{
		URL:        p.APIURL,
		Key:        p.APIKey,
		Model:      p.Model,
		Protocol:   NormalizeLLMProviderProtocol(p.Protocol),
		WireAPI:    NormalizeLLMProviderWireAPI(p.WireAPI),
		AgentType:  strings.TrimSpace(p.AgentType),
		TimeoutSec: p.UpstreamTimeoutSec,
	}
}

func NewLLMEndpointHTTPClient(cfg MaclawLLMConfig) *http.Client {
	return &http.Client{Timeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second}
}

func NormalizeLLMProviderProtocol(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

func NormalizeLLMProviderWireAPI(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "responses", "responses-ws":
		return v
	default:
		return "chat"
	}
}

type LLMProviderConcurrencySnapshot struct {
	MaxConcurrency  int
	MaxQueueWaiters int
	QueueTimeoutMS  int
	InFlight        int
	QueueWaiters    int
}

type llmProviderConcurrencyState struct {
	maxConcurrency  int
	maxQueueWaiters int
	queueTimeoutMS  int
	sema            chan struct{}
	inFlight        int
	queueWaiters    int
}

type LLMProviderConcurrencyErrorKind string

const (
	LLMProviderConcurrencyQueueFull     LLMProviderConcurrencyErrorKind = "queue_full"
	LLMProviderConcurrencyQueueTimeout  LLMProviderConcurrencyErrorKind = "queue_timeout"
	LLMProviderConcurrencyQueueCanceled LLMProviderConcurrencyErrorKind = "queue_canceled"
)

type LLMProviderConcurrencyError struct {
	ProviderID string
	Kind       LLMProviderConcurrencyErrorKind
	Err        error
}

func (e *LLMProviderConcurrencyError) Error() string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case LLMProviderConcurrencyQueueFull:
		return fmt.Sprintf("provider %q queue is full", e.ProviderID)
	case LLMProviderConcurrencyQueueTimeout:
		return fmt.Sprintf("provider %q queue wait timed out", e.ProviderID)
	default:
		if e.Err != nil {
			return fmt.Sprintf("provider %q queue wait canceled: %v", e.ProviderID, e.Err)
		}
		return fmt.Sprintf("provider %q queue wait canceled", e.ProviderID)
	}
}

type LLMProviderConcurrencyController struct {
	mu     sync.Mutex
	states map[string]*llmProviderConcurrencyState
}

func NewLLMProviderConcurrencyController() *LLMProviderConcurrencyController {
	return &LLMProviderConcurrencyController{states: map[string]*llmProviderConcurrencyState{}}
}

func (c *LLMProviderConcurrencyController) Acquire(ctx context.Context, providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) (func(), error) {
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
		return nil, &LLMProviderConcurrencyError{ProviderID: providerID, Kind: LLMProviderConcurrencyQueueFull}
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
			return nil, &LLMProviderConcurrencyError{ProviderID: providerID, Kind: LLMProviderConcurrencyQueueCanceled, Err: ctx.Err()}
		}
		return nil, &LLMProviderConcurrencyError{ProviderID: providerID, Kind: LLMProviderConcurrencyQueueTimeout}
	}
}

func (c *LLMProviderConcurrencyController) Snapshot(providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) LLMProviderConcurrencySnapshot {
	if maxConcurrency <= 0 {
		return LLMProviderConcurrencySnapshot{}
	}
	state := c.stateForProvider(providerID, maxConcurrency, maxQueueWaiters, queueTimeoutMS)
	c.mu.Lock()
	defer c.mu.Unlock()
	return LLMProviderConcurrencySnapshot{
		MaxConcurrency:  state.maxConcurrency,
		MaxQueueWaiters: state.maxQueueWaiters,
		QueueTimeoutMS:  state.queueTimeoutMS,
		InFlight:        state.inFlight,
		QueueWaiters:    state.queueWaiters,
	}
}

func (c *LLMProviderConcurrencyController) Reset() {
	c.mu.Lock()
	c.states = map[string]*llmProviderConcurrencyState{}
	c.mu.Unlock()
}

func (c *LLMProviderConcurrencyController) releaseFunc(state *llmProviderConcurrencyState) func() {
	return func() {
		<-state.sema
		c.mu.Lock()
		if state.inFlight > 0 {
			state.inFlight--
		}
		c.mu.Unlock()
	}
}

func (c *LLMProviderConcurrencyController) stateForProvider(providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) *llmProviderConcurrencyState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil || state.maxConcurrency != maxConcurrency || state.maxQueueWaiters != maxQueueWaiters || state.queueTimeoutMS != queueTimeoutMS {
		state = &llmProviderConcurrencyState{
			maxConcurrency:  maxConcurrency,
			maxQueueWaiters: maxQueueWaiters,
			queueTimeoutMS:  queueTimeoutMS,
			sema:            make(chan struct{}, maxConcurrency),
		}
		c.states[providerID] = state
	}
	return state
}

type LLMProviderResilienceErrorKind string

const (
	LLMProviderResilienceCircuitOpen LLMProviderResilienceErrorKind = "circuit_open"
	LLMProviderResilienceBackingOff  LLMProviderResilienceErrorKind = "backing_off"
)

type LLMProviderResilienceError struct {
	ProviderID string
	Kind       LLMProviderResilienceErrorKind
	RetryAfter time.Duration
}

func (e *LLMProviderResilienceError) Error() string {
	if e == nil {
		return ""
	}
	retryAfter := e.RetryAfter.Round(time.Millisecond)
	switch e.Kind {
	case LLMProviderResilienceCircuitOpen:
		return fmt.Sprintf("provider %q circuit is open for %s", e.ProviderID, retryAfter)
	default:
		return fmt.Sprintf("provider %q is backing off for %s", e.ProviderID, retryAfter)
	}
}

type LLMProviderResilienceSnapshot struct {
	CircuitBreakerThreshold  int
	CircuitBreakerCooldownMS int
	FailureBackoffBaseMS     int
	FailureBackoffMaxMS      int
	ConsecutiveFailures      int
	CircuitOpen              bool
	CircuitOpenUntil         time.Time
	BackoffUntil             time.Time
}

type llmProviderResilienceState struct {
	providerID               string
	circuitBreakerThreshold  int
	circuitBreakerCooldownMS int
	failureBackoffBaseMS     int
	failureBackoffMaxMS      int
	consecutiveFailures      int
	circuitOpenUntil         time.Time
	backoffUntil             time.Time
}

type LLMProviderResilienceController struct {
	mu     sync.Mutex
	states map[string]*llmProviderResilienceState
}

func NewLLMProviderResilienceController() *LLMProviderResilienceController {
	return &LLMProviderResilienceController{states: map[string]*llmProviderResilienceState{}}
}

func (c *LLMProviderResilienceController) BeforeAttempt(p LLMEndpointProvider) error {
	if strings.TrimSpace(p.ID) == "" {
		return nil
	}
	state := c.stateForProvider(p)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !state.circuitOpenUntil.IsZero() && now.Before(state.circuitOpenUntil) {
		return &LLMProviderResilienceError{ProviderID: state.providerID, Kind: LLMProviderResilienceCircuitOpen, RetryAfter: time.Until(state.circuitOpenUntil)}
	}
	if !state.backoffUntil.IsZero() && now.Before(state.backoffUntil) {
		return &LLMProviderResilienceError{ProviderID: state.providerID, Kind: LLMProviderResilienceBackingOff, RetryAfter: time.Until(state.backoffUntil)}
	}
	return nil
}

func (c *LLMProviderResilienceController) RecordSuccess(providerID string) {
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

func (c *LLMProviderResilienceController) RecordFailure(p LLMEndpointProvider) {
	if strings.TrimSpace(p.ID) == "" {
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

func (c *LLMProviderResilienceController) Snapshot(p LLMEndpointProvider) LLMProviderResilienceSnapshot {
	if strings.TrimSpace(p.ID) == "" {
		return LLMProviderResilienceSnapshot{}
	}
	state := c.stateForProvider(p)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	return LLMProviderResilienceSnapshot{
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

func (c *LLMProviderResilienceController) Reset() {
	c.mu.Lock()
	c.states = map[string]*llmProviderResilienceState{}
	c.mu.Unlock()
}

func (c *LLMProviderResilienceController) stateForProvider(p LLMEndpointProvider) *llmProviderResilienceState {
	providerID := strings.TrimSpace(p.ID)
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[providerID]
	if state == nil {
		state = &llmProviderResilienceState{providerID: providerID}
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

func ShouldCountLLMProviderFailure(statusCode int, err error) bool {
	if err != nil {
		return true
	}
	switch statusCode {
	case 0:
		return false
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	default:
		return statusCode >= 500
	}
}
