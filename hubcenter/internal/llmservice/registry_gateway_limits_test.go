package llmservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestNormalizeProviderGatewayLimitsFillsQueueTimeoutOnly(t *testing.T) {
	p := &llmpool.ProviderConfig{
		ID:             "deepseek",
		MaxConcurrency: 10,
		// MaxQueueWaiters left at 0 on purpose — means unlimited waiters.
	}
	normalizeProviderGatewayLimits(p)
	if p.MaxConcurrency != 10 {
		t.Fatalf("MaxConcurrency changed: got %d", p.MaxConcurrency)
	}
	if p.MaxQueueWaiters != 0 {
		t.Fatalf("MaxQueueWaiters must stay 0 (unlimited), got %d", p.MaxQueueWaiters)
	}
	if p.QueueTimeoutMS != defaultProviderQueueTimeoutMS {
		t.Fatalf("QueueTimeoutMS = %d, want %d", p.QueueTimeoutMS, defaultProviderQueueTimeoutMS)
	}
}

func TestNormalizeProviderGatewayLimitsSkipsUnlimitedProviders(t *testing.T) {
	p := &llmpool.ProviderConfig{ID: "unlimited", MaxConcurrency: 0}
	normalizeProviderGatewayLimits(p)
	if p.QueueTimeoutMS != 0 {
		t.Fatalf("unlimited provider should not get queue timeout default, got %d", p.QueueTimeoutMS)
	}
	if p.MaxQueueWaiters != 0 {
		t.Fatalf("unlimited provider MaxQueueWaiters changed: %d", p.MaxQueueWaiters)
	}
}

func TestNormalizeProviderGatewayLimitsPreservesExplicitTimeout(t *testing.T) {
	p := &llmpool.ProviderConfig{
		ID:              "deepseek",
		MaxConcurrency:  64,
		MaxQueueWaiters: 32,
		QueueTimeoutMS:  5000,
	}
	normalizeProviderGatewayLimits(p)
	if p.MaxQueueWaiters != 32 || p.QueueTimeoutMS != 5000 {
		t.Fatalf("explicit queue settings mutated: waiters=%d timeout=%d", p.MaxQueueWaiters, p.QueueTimeoutMS)
	}
}
