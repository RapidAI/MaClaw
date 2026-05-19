package httpapi

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var globalLLMEndpointDownstreamSemaphore = im.NewLLMSemaphore(im.DefaultLLMProviderDownstreamMaxConcurrency)
var globalLLMEndpointDownstreamCapacity atomic.Int64

var tenantLLMEndpointDownstreamSemaphores = struct {
	sync.Mutex
	items map[string]*im.LLMSemaphore
}{items: map[string]*im.LLMSemaphore{}}

func applyLLMEndpointDownstreamConfig(reg *im.LLMProviderRegistry) {
	cap := llmEndpointDownstreamCapacityFromRegistry(reg)
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

func llmEndpointDownstreamCapacityFromRegistry(reg *im.LLMProviderRegistry) int {
	cap := im.DefaultLLMProviderDownstreamMaxConcurrency
	if reg != nil && reg.DownstreamMaxConcurrency > 0 {
		cap = reg.DownstreamMaxConcurrency
	}
	return cap
}

func acquireLLMEndpointDownstreamSlot(ctx context.Context, tenantID string, reg *im.LLMProviderRegistry) (*im.LLMSemaphore, bool) {
	sem := llmEndpointDownstreamSemaphoreForTenant(tenantID, reg)
	return sem, sem.Acquire(ctx)
}

func llmEndpointDownstreamSemaphoreForTenant(tenantID string, reg *im.LLMProviderRegistry) *im.LLMSemaphore {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		applyLLMEndpointDownstreamConfig(reg)
		return globalLLMEndpointDownstreamSemaphore
	}
	cap := llmEndpointDownstreamCapacityFromRegistry(reg)
	tenantLLMEndpointDownstreamSemaphores.Lock()
	defer tenantLLMEndpointDownstreamSemaphores.Unlock()
	sem := tenantLLMEndpointDownstreamSemaphores.items[tenantID]
	if sem == nil {
		sem = im.NewLLMSemaphore(cap)
		tenantLLMEndpointDownstreamSemaphores.items[tenantID] = sem
		return sem
	}
	if sem.Capacity() != cap {
		sem.Resize(cap)
	}
	return sem
}
