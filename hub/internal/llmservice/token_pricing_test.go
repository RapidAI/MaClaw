package llmservice

import (
	"fmt"
	"math"
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

func TestEstimateTokenPricingCreditsFallbackClampsLegsLikeFixedPoint(t *testing.T) {
	pricing := llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}}
	// A NaN multiplier forces the float fallback path.
	fallback := EstimateTokenPricingCreditsWithCache(10_000, 5_000, 99_999, 5_000, pricing, math.NaN())
	// Cache legs are bounded by the input total exactly like the fixed-point
	// path: read=10000, write=0, normal=0 -> 0.1 + 2 Credits.
	fixed, ok := llmpool.EstimateTokenPricingMicrocreditsWithCache(10_000, 5_000, 99_999, 5_000, pricing, 1)
	if !ok || fallback != 2.1 || fallback != llmpool.MicrocreditsToCredits(fixed) {
		t.Fatalf("fallback = %v, fixed = %d ok=%v, want 2.1", fallback, fixed, ok)
	}
	// Negative totals clamp before the cache legs: nothing billable remains.
	if got := EstimateTokenPricingCreditsWithCache(-100, -50, 30, 20, pricing, math.NaN()); got != 0 {
		t.Fatalf("negative-leg fallback = %v, want 0", got)
	}
	// A negative price in the fallback path must never produce a negative debit.
	negativePrice := llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: -1, OutputCreditsPer10K: 4}}
	if got := EstimateTokenPricingCreditsWithCache(10_000, 5_000, 0, 0, negativePrice, math.NaN()); got < 0 || math.IsNaN(got) {
		t.Fatalf("negative-price fallback = %v, want a non-negative debit", got)
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
					ProviderID: "provider-a", Model: "upstream-one", BillingMode: llmpool.BillingModePaid, TokenPricingOverride: true, TokenPricing: pricingOne,
				}},
			}},
		},
		{
			ID: "group-two",
			Models: []ModelServiceModel{{
				Name:        "shared",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID: "provider-a", Model: "upstream-two", BillingMode: llmpool.BillingModePaid, TokenPricingOverride: true, TokenPricing: pricingTwo,
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

func TestPricingSourceForProviderRouteHonorsExplicitOverride(t *testing.T) {
	model := &AuthorizedModel{
		ProviderRouteBilling: map[string]map[string]ProviderRouteBilling{
			"provider-a": {
				"upstream": {TokenPricingOverride: true, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
				"other":    {},
			},
		},
	}
	if got := PricingSourceForProviderRoute(model, "provider-a", "upstream"); got != llmpool.PricingSourceServiceGroupOverride {
		t.Fatalf("override source = %q", got)
	}
	if got := PricingSourceForProviderRoute(model, "provider-a", "other"); got != llmpool.PricingSourceProvider {
		t.Fatalf("provider source = %q", got)
	}
}

// TestRoutePricingWithoutOverrideFallsBackToProviderPrice pins the shared
// EffectiveRouteTokenPricing semantics on the Hub side: a route price without
// an explicit override flag is not a service-group price — the provider base
// price applies and the audit label stays "provider".
func TestRoutePricingWithoutOverrideFallsBackToProviderPrice(t *testing.T) {
	model := &AuthorizedModel{
		Name:                 "shared",
		ProviderTokenPricing: map[string]llmpool.TokenPricing{"provider-a": {InputCreditsPer10K: 9, OutputCreditsPer10K: 9}},
		ProviderRouteBilling: map[string]map[string]ProviderRouteBilling{
			"provider-a": {
				"upstream": {BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
			},
		},
	}
	resolved, ok := ResolveTokenPricingForProviderRoute(model, "provider-a", "upstream", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	if !ok || resolved.InputCreditsPer10K != 9 || resolved.OutputCreditsPer10K != 9 {
		t.Fatalf("non-override route price = %#v, ok=%v; want provider price 9/9", resolved, ok)
	}
	if got := PricingSourceForProviderRoute(model, "provider-a", "upstream"); got != llmpool.PricingSourceProvider {
		t.Fatalf("non-override route source = %q, want %q", got, llmpool.PricingSourceProvider)
	}
	// An override flag without a usable price cannot supply or claim the price.
	model.ProviderRouteBilling["provider-a"]["upstream"] = ProviderRouteBilling{TokenPricingOverride: true}
	if _, ok := ResolveTokenPricingForProviderRoute(model, "provider-a", "upstream", time.Now()); !ok {
		t.Fatal("override without price must fall through to the provider price")
	}
	if got := PricingSourceForProviderRoute(model, "provider-a", "upstream"); got != llmpool.PricingSourceProvider {
		t.Fatalf("priceless override source = %q, want %q", got, llmpool.PricingSourceProvider)
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

func TestTrimBillingLedgerForRegistryKeepsNewestEntries(t *testing.T) {
	reg := &Registry{}
	for i := 0; i < BillingLedgerRegistryKeep+500; i++ {
		AppendBillingLedgerEntry(reg, BillingLedgerEntry{RequestID: fmt.Sprintf("req-%d", i), DeductedCredits: 1})
	}
	if len(reg.BillingLedger) != BillingLedgerRegistryKeep {
		t.Fatalf("ledger len = %d, want %d", len(reg.BillingLedger), BillingLedgerRegistryKeep)
	}
	if !HasBillingRequest(reg, fmt.Sprintf("req-%d", BillingLedgerRegistryKeep+499)) {
		t.Fatal("newest entry missing after trim")
	}
	if HasBillingRequest(reg, "req-0") {
		t.Fatal("oldest entry should have been trimmed")
	}
	// Idempotency still works within the retained window.
	before := len(reg.BillingLedger)
	AppendBillingLedgerEntry(reg, BillingLedgerEntry{RequestID: fmt.Sprintf("req-%d", BillingLedgerRegistryKeep+499), DeductedCredits: 1})
	if len(reg.BillingLedger) != before {
		t.Fatal("duplicate request ID must not append")
	}
}

func TestTrimBillingLedgerForRegistryNoOpUnderLimit(t *testing.T) {
	reg := &Registry{}
	AppendBillingLedgerEntry(reg, BillingLedgerEntry{RequestID: "req-a", DeductedCredits: 1})
	TrimBillingLedgerForRegistry(reg)
	if len(reg.BillingLedger) != 1 {
		t.Fatalf("ledger len = %d, want 1", len(reg.BillingLedger))
	}
}
