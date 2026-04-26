package httpapi

import (
	"context"

	"github.com/RapidAI/CodeClaw/corelib"
)

type providerConcurrencySnapshot = corelib.LLMProviderConcurrencySnapshot
type providerConcurrencyErrorKind = corelib.LLMProviderConcurrencyErrorKind
type providerConcurrencyError = corelib.LLMProviderConcurrencyError

const (
	providerConcurrencyQueueFull     providerConcurrencyErrorKind = corelib.LLMProviderConcurrencyQueueFull
	providerConcurrencyQueueTimeout  providerConcurrencyErrorKind = corelib.LLMProviderConcurrencyQueueTimeout
	providerConcurrencyQueueCanceled providerConcurrencyErrorKind = corelib.LLMProviderConcurrencyQueueCanceled
)

type providerConcurrencyController struct {
	inner *corelib.LLMProviderConcurrencyController
}

func newProviderConcurrencyController() *providerConcurrencyController {
	return &providerConcurrencyController{inner: corelib.NewLLMProviderConcurrencyController()}
}

var globalProviderConcurrency = newProviderConcurrencyController()

func (c *providerConcurrencyController) acquire(ctx context.Context, providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) (func(), error) {
	return c.inner.Acquire(ctx, providerID, maxConcurrency, maxQueueWaiters, queueTimeoutMS)
}

func (c *providerConcurrencyController) snapshot(providerID string, maxConcurrency int, maxQueueWaiters int, queueTimeoutMS int) providerConcurrencySnapshot {
	return c.inner.Snapshot(providerID, maxConcurrency, maxQueueWaiters, queueTimeoutMS)
}

func (c *providerConcurrencyController) reset() {
	c.inner.Reset()
}
