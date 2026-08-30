package llmpool

import (
	"testing"
	"time"
)

func ptr(v float64) *float64 { return &v }

func TestResolveTokenPricingUsesInputOutputTimeWindow(t *testing.T) {
	pricing := TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 4,
		Timezone:            "Asia/Shanghai",
		Version:             "v1",
		PriceSchedule: []TokenPriceWindow{{
			ID:                  "night",
			Days:                []int{6},
			Start:               "00:00",
			End:                 "08:00",
			InputCreditsPer10K:  ptr(0.5),
			OutputCreditsPer10K: ptr(2),
		}},
	}
	started := time.Date(2026, 8, 21, 17, 30, 0, 0, time.UTC) // Sat 01:30 +08
	got, ok := ResolveTokenPricing(pricing, started)
	if !ok {
		t.Fatal("expected pricing to resolve")
	}
	if got.WindowID != "night" || got.InputCreditsPer10K != 0.5 || got.OutputCreditsPer10K != 2 {
		t.Fatalf("unexpected resolved pricing: %#v", got)
	}
	if len(got.PriceSchedule) != 0 {
		t.Fatalf("resolved price retained mutable schedule: %#v", got.PriceSchedule)
	}
}

func TestResolveTokenPricingRejectsEmptyCreditPrice(t *testing.T) {
	if _, ok := ResolveTokenPricing(TokenPricing{InputCreditsPer10K: 0, OutputCreditsPer10K: 0}, time.Now()); ok {
		t.Fatal("zero-priced paid route must not resolve as billable pricing")
	}
}

func TestEffectiveRouteTokenPricingPrefersConfiguredProviderPrice(t *testing.T) {
	provider := ProviderConfig{TokenPricing: TokenPricing{
		InputCreditsPer10K:  2,
		OutputCreditsPer10K: 8,
		InputRMBPer10K:      0.02,
		OutputRMBPer10K:     0.06,
	}}
	route := ModelProviderConfig{TokenPricing: TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 4,
		InputRMBPer10K:      0.01,
		OutputRMBPer10K:     0.03,
	}}
	got := EffectiveRouteTokenPricing(route, provider)
	if got.InputCreditsPer10K != provider.TokenPricing.InputCreditsPer10K ||
		got.OutputCreditsPer10K != provider.TokenPricing.OutputCreditsPer10K ||
		got.InputRMBPer10K != provider.TokenPricing.InputRMBPer10K ||
		got.OutputRMBPer10K != provider.TokenPricing.OutputRMBPer10K {
		t.Fatalf("effective route pricing = %#v", got)
	}

	provider.TokenPricing = TokenPricing{}
	got = EffectiveRouteTokenPricing(route, provider)
	if got.InputCreditsPer10K != route.TokenPricing.InputCreditsPer10K ||
		got.OutputCreditsPer10K != route.TokenPricing.OutputCreditsPer10K ||
		got.InputRMBPer10K != route.TokenPricing.InputRMBPer10K ||
		got.OutputRMBPer10K != route.TokenPricing.OutputRMBPer10K {
		t.Fatalf("legacy route fallback pricing = %#v", got)
	}
}

func TestTokenPricingSnapshotRoundTrip(t *testing.T) {
	snapshot := TokenPricingSnapshot{
		ProviderID:    "official-a",
		UpstreamModel: "opencode-1",
		Pricing: ResolvedTokenPricing{TokenPricing: TokenPricing{
			InputCreditsPer10K:  0.5,
			OutputCreditsPer10K: 2,
			Timezone:            "Asia/Shanghai",
			Version:             "v1",
		}},
		InputTokens:  20_000,
		OutputTokens: 5_000,
	}
	raw, ok := EncodeTokenPricingSnapshot(snapshot)
	if !ok {
		t.Fatal("expected snapshot to encode")
	}
	got, ok := DecodeTokenPricingSnapshot(raw)
	if !ok || got.ProviderID != snapshot.ProviderID || got.OutputTokens != 5_000 || got.Pricing.OutputCreditsPer10K != 2 {
		t.Fatalf("snapshot round trip failed: %#v", got)
	}
}

func TestValidateResolvedTokenPricingDoesNotReapplyTimeSchedule(t *testing.T) {
	base := TokenPricing{
		InputCreditsPer10K:  0.5,
		OutputCreditsPer10K: 2,
		Timezone:            "Asia/Shanghai",
		PriceSchedule: []TokenPriceWindow{{
			ID: "day", Start: "08:00", End: "20:00",
			InputCreditsPer10K:  ptr(1),
			OutputCreditsPer10K: ptr(4),
		}},
	}
	pricing, ok := ResolveTokenPricing(base, time.Date(2026, 8, 23, 13, 0, 0, 0, time.UTC)) // 21:00 +08, outside day window
	if !ok {
		t.Fatal("failed to resolve frozen price")
	}
	if !ValidateResolvedTokenPricing(pricing) {
		t.Fatal("frozen directional price was rejected")
	}
	// ResolveTokenPricing is deliberately a different operation: it evaluates
	// schedule windows using a caller-supplied clock, whereas a snapshot must
	// preserve the already-resolved 0.5/2 price above.
	if pricing.InputCreditsPer10K != 0.5 || pricing.OutputCreditsPer10K != 2 || len(pricing.PriceSchedule) != 0 {
		t.Fatalf("frozen price mutated: %#v", pricing)
	}
	if ValidateResolvedTokenPricing(ResolvedTokenPricing{TokenPricing: base, WindowID: "night"}) {
		t.Fatal("snapshot with mutable schedule was accepted")
	}
}

func TestValidateRouteBillingRejectsOverlappingWindows(t *testing.T) {
	pricing := TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 2,
		Timezone:            "Asia/Shanghai",
		PriceSchedule: []TokenPriceWindow{
			{ID: "morning", Days: []int{1}, Start: "08:00", End: "10:00"},
			{ID: "overlap", Days: []int{1}, Start: "09:00", End: "11:00"},
		},
	}
	if err := ValidateRouteBilling(BillingModePaid, pricing); err == nil {
		t.Fatal("expected overlapping schedules to fail")
	}
}

func TestValidateRouteBillingRequiresExplicitPriceForPaid(t *testing.T) {
	if err := ValidateRouteBilling(BillingModePaid, TokenPricing{}); err == nil {
		t.Fatal("expected paid route without price to fail")
	}
	if err := ValidateRouteBilling(BillingModeFree, TokenPricing{}); err != nil {
		t.Fatalf("free route should be accepted: %v", err)
	}
}

func TestEstimateTokenPricingMicrocreditsSeparatesDirectionsAndRoundsOnce(t *testing.T) {
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 4,
	}}
	got, ok := EstimateTokenPricingMicrocredits(20_000, 5_000, pricing, 2)
	if !ok || got != 8*MicrocreditsPerCredit {
		t.Fatalf("amount = %d ok=%v, want %d", got, ok, 8*MicrocreditsPerCredit)
	}

	// 5 input tokens at 1 Credit/10k is exactly 0.0005 Credit; this
	// verifies the configured half-up 0.001 Credit request rounding.
	got, ok = EstimateTokenPricingMicrocredits(5, 0, pricing, 1)
	if !ok || got != BillingRoundMicrocredits {
		t.Fatalf("half-up amount = %d ok=%v, want %d", got, ok, BillingRoundMicrocredits)
	}
}

func TestEstimateTokenPricingMicrocreditsAppliesMinimumAfterMultiplier(t *testing.T) {
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{
		InputCreditsPer10K:    1,
		OutputCreditsPer10K:   4,
		MinimumRequestCredits: 0.1,
	}}
	got, ok := EstimateTokenPricingMicrocredits(1, 0, pricing, 2)
	if !ok || got != 200_000 {
		t.Fatalf("minimum amount = %d ok=%v, want 200000", got, ok)
	}
}

func TestTokenPricingCreditComponentsMatchFixedPointDebit(t *testing.T) {
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{
		InputCreditsPer10K:    1.2345,
		OutputCreditsPer10K:   4.5678,
		MinimumRequestCredits: 0.3333,
	}}
	input, output, minimum, ok := TokenPricingCreditComponents(17, 9, pricing, 1.17)
	if !ok {
		t.Fatal("expected fixed-point components")
	}
	if input <= 0 || output <= 0 || minimum <= 0 {
		t.Fatalf("components = input=%v output=%v minimum=%v, want positive values", input, output, minimum)
	}
	debit, ok := EstimateTokenPricingMicrocredits(17, 9, pricing, 1.17)
	if !ok {
		t.Fatal("expected fixed-point debit")
	}
	if got := input + output + minimum; got > MicrocreditsToCredits(debit)+0.0005 {
		t.Fatalf("unrounded components %v exceed rounded debit %v", got, MicrocreditsToCredits(debit))
	}
}

func TestNewPricingQuoteSnapshotFreezesMaximumDebit(t *testing.T) {
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 4,
	}}
	expiresAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	quote, ok := NewPricingQuoteSnapshot("req-1", "attempt-1", "provider-1", pricing, 1, 2, 20_000, 5_000, expiresAt)
	if !ok {
		t.Fatal("expected quote")
	}
	if quote.ReservedMicrocredits != 8*MicrocreditsPerCredit || !quote.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected quote: %#v", quote)
	}
	if _, ok := NewPricingQuoteSnapshot("", "attempt-1", "provider-1", pricing, 1, 1, 1, 1, expiresAt); ok {
		t.Fatal("quote without request ID must be rejected")
	}
}
