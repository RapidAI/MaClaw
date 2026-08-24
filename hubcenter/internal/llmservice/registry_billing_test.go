package llmservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestAddProviderNormalizesVendorBillingSchedule(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "DeepSeek",
		APIURL: "https://api.deepseek.com",
		CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
			Days:       []int{1, 1, 2, 3, 4, 5, 99},
			Start:      "0:30",
			End:        "8:30",
			Multiplier: 0.5,
		}},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	billing := svc.ListProviderBilling(ctx)
	if len(billing) != 1 {
		t.Fatalf("billing len = %d, want 1", len(billing))
	}
	got := billing[0]
	if got.Timezone != llmpool.DefaultCreditMultiplierTimezone {
		t.Fatalf("timezone = %q, want %q", got.Timezone, llmpool.DefaultCreditMultiplierTimezone)
	}
	if got.CreditMultiplier != 1 {
		t.Fatalf("default multiplier = %v, want 1", got.CreditMultiplier)
	}
	if len(got.CreditMultiplierSchedule) != 1 {
		t.Fatalf("windows = %#v", got.CreditMultiplierSchedule)
	}
	window := got.CreditMultiplierSchedule[0]
	if window.Start != "00:30" || window.End != "08:30" || window.Multiplier != 0.5 {
		t.Fatalf("window = %#v", window)
	}
	if len(window.Days) != 5 {
		t.Fatalf("days = %#v, want weekdays", window.Days)
	}
}

func TestUpdateProviderPreservesResilienceWhenSavingBilling(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:                       "deepseek",
		Name:                     "DeepSeek",
		APIURL:                   "https://api.deepseek.com",
		WireAPI:                  "responses",
		ResolutionTier:           2,
		MaxQueueWaiters:          8,
		QueueTimeoutMS:           1500,
		CircuitBreakerThreshold:  4,
		CircuitBreakerCooldownMS: 12000,
		FailureBackoffBaseMS:     250,
		FailureBackoffMaxMS:      8000,
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:               "deepseek",
		Name:             "DeepSeek",
		APIURL:           "https://api.deepseek.com",
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1,
		CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
			Days:       []int{1, 2, 3, 4, 5},
			Start:      "00:30",
			End:        "08:30",
			Multiplier: 0.5,
		}},
	}); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil {
		t.Fatalf("GetProvider: %#v err=%v", got, err)
	}
	if got.WireAPI != "responses" || got.ResolutionTier != 2 || got.MaxQueueWaiters != 8 || got.QueueTimeoutMS != 1500 {
		t.Fatalf("queue fields = %#v", got)
	}
	if got.CircuitBreakerThreshold != 4 || got.CircuitBreakerCooldownMS != 12000 || got.FailureBackoffBaseMS != 250 || got.FailureBackoffMaxMS != 8000 {
		t.Fatalf("breaker fields = %#v", got)
	}
	if got.Timezone != "Asia/Shanghai" || len(got.CreditMultiplierSchedule) != 1 || got.CreditMultiplierSchedule[0].Multiplier != 0.5 {
		t.Fatalf("billing = %#v", got)
	}
}

func TestListProviderBillingIncludesPausedFlag(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "DeepSeek",
		APIURL: "https://api.deepseek.com",
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	if err := svc.SetProviderPaused(ctx, "deepseek", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	billing := svc.ListProviderBilling(ctx)
	if len(billing) != 1 || !billing[0].Paused || billing[0].ProviderID != "deepseek" {
		t.Fatalf("billing = %#v, want paused deepseek", billing)
	}
}
