package httpapi

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

type providerResilienceErrorKind = corelib.LLMProviderResilienceErrorKind

type providerResilienceError = corelib.LLMProviderResilienceError

type providerResilienceSnapshot = corelib.LLMProviderResilienceSnapshot

const (
	providerResilienceCircuitOpen providerResilienceErrorKind = corelib.LLMProviderResilienceCircuitOpen
	providerResilienceBackingOff  providerResilienceErrorKind = corelib.LLMProviderResilienceBackingOff
)

type providerResilienceController struct {
	inner *corelib.LLMProviderResilienceController
}

var globalProviderResilience = newProviderResilienceController()

func newProviderResilienceController() *providerResilienceController {
	return &providerResilienceController{inner: corelib.NewLLMProviderResilienceController()}
}

func (c *providerResilienceController) beforeAttempt(p *im.LLMProvider) error {
	if p == nil {
		return nil
	}
	return c.inner.BeforeAttempt(toCoreLLMEndpointProvider(p))
}

func (c *providerResilienceController) recordSuccess(providerID string) {
	c.inner.RecordSuccess(providerID)
}

func (c *providerResilienceController) recordFailure(p *im.LLMProvider) {
	if p == nil {
		return
	}
	c.inner.RecordFailure(toCoreLLMEndpointProvider(p))
}

func (c *providerResilienceController) snapshot(p *im.LLMProvider) providerResilienceSnapshot {
	if p == nil {
		return providerResilienceSnapshot{}
	}
	return c.inner.Snapshot(toCoreLLMEndpointProvider(p))
}

func (c *providerResilienceController) reset() {
	c.inner.Reset()
}

func shouldCountProviderFailure(statusCode int, err error) bool {
	return corelib.ShouldCountLLMProviderFailure(statusCode, err)
}
