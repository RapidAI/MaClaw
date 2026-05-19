package httpapi

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

func TestLLMEndpointDownstreamConfigTenantSemaphoreDoesNotResizeGlobal(t *testing.T) {
	originalCapacity := globalLLMEndpointDownstreamSemaphore.Capacity()
	defer globalLLMEndpointDownstreamSemaphore.Resize(originalCapacity)
	applyLLMEndpointDownstreamConfig(&im.LLMProviderRegistry{DownstreamMaxConcurrency: 17})

	sem, ok := acquireLLMEndpointDownstreamSlot(t.Context(), "tenant_a", &im.LLMProviderRegistry{DownstreamMaxConcurrency: 1})
	if !ok {
		t.Fatal("tenant semaphore acquire failed")
	}
	defer sem.Release()
	if got := sem.Capacity(); got != 1 {
		t.Fatalf("tenant capacity = %d, want 1", got)
	}
	if got := globalLLMEndpointDownstreamSemaphore.Capacity(); got != 17 {
		t.Fatalf("tenant config resized global capacity = %d, want 17", got)
	}
}
func TestApplyLLMEndpointDownstreamConfigUsesDefaultAndOverride(t *testing.T) {
	originalCapacity := globalLLMEndpointDownstreamSemaphore.Capacity()
	originalTimeout := globalLLMEndpointDownstreamSemaphore.AcquireTimeout
	defer func() {
		globalLLMEndpointDownstreamSemaphore.Resize(originalCapacity)
		globalLLMEndpointDownstreamSemaphore.AcquireTimeout = originalTimeout
	}()

	applyLLMEndpointDownstreamConfig(nil)
	if got := globalLLMEndpointDownstreamSemaphore.Capacity(); got != im.DefaultLLMProviderDownstreamMaxConcurrency {
		t.Fatalf("default capacity = %d, want %d", got, im.DefaultLLMProviderDownstreamMaxConcurrency)
	}
	if globalLLMEndpointDownstreamSemaphore.AcquireTimeout != 10*time.Second {
		t.Fatalf("default acquire timeout = %v, want %v", globalLLMEndpointDownstreamSemaphore.AcquireTimeout, 10*time.Second)
	}

	applyLLMEndpointDownstreamConfig(&im.LLMProviderRegistry{DownstreamMaxConcurrency: 23})
	if got := globalLLMEndpointDownstreamSemaphore.Capacity(); got != 23 {
		t.Fatalf("override capacity = %d, want %d", got, 23)
	}

	applyLLMEndpointDownstreamConfig(&im.LLMProviderRegistry{DownstreamMaxConcurrency: 0})
	if got := globalLLMEndpointDownstreamSemaphore.Capacity(); got != im.DefaultLLMProviderDownstreamMaxConcurrency {
		t.Fatalf("fallback capacity = %d, want %d", got, im.DefaultLLMProviderDownstreamMaxConcurrency)
	}
}
