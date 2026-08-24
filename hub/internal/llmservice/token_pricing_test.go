package llmservice

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestEstimateTokenPricingCreditsSeparatesInputOutputAndAppliesGroupMultiplier(t *testing.T) {
	pricing := llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K:    1,
		OutputCreditsPer10K:   4,
		MinimumRequestCredits: 0.1,
	}}
	if got := EstimateTokenPricingCredits(20_000, 5_000, pricing, 2); got != 8 {
		t.Fatalf("credits = %v, want 8", got)
	}
}

func TestEstimateTokenPricingCreditsMultipliesMinimum(t *testing.T) {
	pricing := llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K:    1,
		OutputCreditsPer10K:   4,
		MinimumRequestCredits: 0.1,
	}}
	if got := EstimateTokenPricingCredits(1, 0, pricing, 2); got != 0.2 {
		t.Fatalf("credits = %v, want 0.2", got)
	}
}

func TestBillingGroupMultiplierDoesNotUseProviderCreditMultiplier(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:                     "official",
		BillingGroupMultiplier: 2,
	}}}
	if got := BillingGroupMultiplier(reg, []string{"official"}); got != 2 {
		t.Fatalf("multiplier = %v, want 2", got)
	}
}

func TestResolveTokenPricingForProviderRouteKeepsUpstreamRoutesDistinct(t *testing.T) {
	pricingOne := llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}
	pricingTwo := llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 4}
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{
		{
			ID: "group-one",
			Models: []ModelServiceModel{{
				Name:        "shared",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID: "provider-a", Model: "upstream-one", BillingMode: llmpool.BillingModePaid, TokenPricing: pricingOne,
				}},
			}},
		},
		{
			ID: "group-two",
			Models: []ModelServiceModel{{
				Name:        "shared",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID: "provider-a", Model: "upstream-two", BillingMode: llmpool.BillingModePaid, TokenPricing: pricingTwo,
				}},
			}},
		},
	}}
	models, _ := buildAuthorizedModels(reg, []string{"group-one", "group-two"})
	if len(models) != 1 {
		t.Fatalf("models = %#v", models)
	}
	model := &models[0]
	when := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	first, ok := ResolveTokenPricingForProviderRoute(model, "provider-a", "upstream-one", when)
	if !ok || first.InputCreditsPer10K != 1 || first.OutputCreditsPer10K != 2 {
		t.Fatalf("upstream-one price = %#v, ok=%v", first, ok)
	}
	second, ok := ResolveTokenPricingForProviderRoute(model, "provider-a", "upstream-two", when)
	if !ok || second.InputCreditsPer10K != 3 || second.OutputCreditsPer10K != 4 {
		t.Fatalf("upstream-two price = %#v, ok=%v", second, ok)
	}
}

func TestIsFreeBillingProviderRouteDoesNotHidePaidSibling(t *testing.T) {
	model := &AuthorizedModel{
		Name: "shared",
		ProviderRouteBilling: map[string]map[string]ProviderRouteBilling{
			"provider-a": {
				"free-upstream": {BillingMode: llmpool.BillingModeFree},
				"paid-upstream": {BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
			},
		},
	}
	if !IsFreeBillingProviderRoute(model, "provider-a", "free-upstream") {
		t.Fatal("free route was not terminal")
	}
	if IsFreeBillingProviderRoute(model, "provider-a", "paid-upstream") {
		t.Fatal("paid sibling was incorrectly classified as free")
	}
}

func TestBillingLedgerRequestIDIsIdempotent(t *testing.T) {
	reg := &Registry{}
	AppendBillingLedgerEntry(reg, BillingLedgerEntry{RequestID: "req-1", DeductedCredits: 2})
	AppendBillingLedgerEntry(reg, BillingLedgerEntry{RequestID: "REQ-1", DeductedCredits: 2})
	if len(reg.BillingLedger) != 1 || !HasBillingRequest(reg, "req-1") {
		t.Fatalf("ledger = %#v", reg.BillingLedger)
	}
}
