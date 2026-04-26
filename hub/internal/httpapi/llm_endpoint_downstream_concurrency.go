package httpapi

import (
	"context"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

var globalLLMEndpointDownstreamSemaphore = im.NewLLMSemaphore(im.DefaultLLMProviderDownstreamMaxConcurrency)

func applyLLMEndpointDownstreamConfig(reg *im.LLMProviderRegistry) {
	cap := im.DefaultLLMProviderDownstreamMaxConcurrency
	if reg != nil && reg.DownstreamMaxConcurrency > 0 {
		cap = reg.DownstreamMaxConcurrency
	}
	globalLLMEndpointDownstreamSemaphore.Resize(cap)
	globalLLMEndpointDownstreamSemaphore.AcquireTimeout = 10 * time.Second
}

func acquireLLMEndpointDownstreamSlot(ctx context.Context, reg *im.LLMProviderRegistry) bool {
	applyLLMEndpointDownstreamConfig(reg)
	return globalLLMEndpointDownstreamSemaphore.Acquire(ctx)
}
