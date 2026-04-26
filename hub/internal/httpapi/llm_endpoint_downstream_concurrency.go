package httpapi

import (
	"context"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

var globalLLMEndpointDownstreamSemaphore = im.NewLLMSemaphore(im.DefaultLLMProviderDownstreamMaxConcurrency)
var globalLLMEndpointDownstreamCapacity atomic.Int64

func applyLLMEndpointDownstreamConfig(reg *im.LLMProviderRegistry) {
	cap := im.DefaultLLMProviderDownstreamMaxConcurrency
	if reg != nil && reg.DownstreamMaxConcurrency > 0 {
		cap = reg.DownstreamMaxConcurrency
	}
	for {
		old := globalLLMEndpointDownstreamCapacity.Load()
		if old == int64(cap) {
			return
		}
		if globalLLMEndpointDownstreamCapacity.CompareAndSwap(old, int64(cap)) {
			if old != 0 || globalLLMEndpointDownstreamSemaphore.Capacity() != cap {
				globalLLMEndpointDownstreamSemaphore.Resize(cap)
			}
			return
		}
	}
}

func acquireLLMEndpointDownstreamSlot(ctx context.Context, reg *im.LLMProviderRegistry) bool {
	applyLLMEndpointDownstreamConfig(reg)
	return globalLLMEndpointDownstreamSemaphore.Acquire(ctx)
}
