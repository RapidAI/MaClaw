package httpapi

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

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
