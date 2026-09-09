package llmpool

import (
	"encoding/json"
	"math"
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

func TestEffectiveRouteTokenPricingPrefersExplicitRouteOverride(t *testing.T) {
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
	}, TokenPricingOverride: true}
	got := EffectiveRouteTokenPricing(route, provider)
	if got.InputCreditsPer10K != route.TokenPricing.InputCreditsPer10K ||
		got.OutputCreditsPer10K != route.TokenPricing.OutputCreditsPer10K ||
		got.InputRMBPer10K != route.TokenPricing.InputRMBPer10K ||
		got.OutputRMBPer10K != route.TokenPricing.OutputRMBPer10K {
		t.Fatalf("effective route override pricing = %#v", got)
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

func TestCacheV1MissingCacheRatesUseDefaults(t *testing.T) {
	pricing, ok := ResolveTokenPricing(TokenPricing{
		InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
		InputRMBPer10K: 0.01, OutputRMBPer10K: 0.02,
		Version: "cache-v1",
	}, time.Now())
	if !ok {
		t.Fatal("cache-v1 pricing should resolve")
	}
	if OptionalTokenPriceValue(pricing.CacheReadCreditsPer10K) != 0.1 || OptionalTokenPriceValue(pricing.CacheWriteCreditsPer10K) != 1 {
		t.Fatalf("missing cache-v1 rates did not default: %#v", pricing)
	}
	credits, ok := EstimateTokenPricingMicrocreditsWithCache(10_000, 0, 10_000, 0, pricing, 1)
	if !ok || credits != 100000 {
		t.Fatalf("default cache read should use 10%% input price, credits=%d ok=%v", credits, ok)
	}
}

func TestTokenPricingSnapshotRoundTrip(t *testing.T) {
	snapshot := TokenPricingSnapshot{
		ProviderID:    "official-a",
		UpstreamModel: "opencode-1",
		PricingSource: PricingSourceServiceGroupOverride,
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
	if !ok || got.ProviderID != snapshot.ProviderID || got.OutputTokens != 5_000 || got.Pricing.OutputCreditsPer10K != 2 || got.PricingSource != PricingSourceServiceGroupOverride {
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

func TestEstimateTokenPricingMicrocreditsWithCacheUsesIndependentRates(t *testing.T) {
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{
		InputCreditsPer10K: 1, OutputCreditsPer10K: 4,
		CacheReadCreditsPer10K: ptr(0.1), CacheWriteCreditsPer10K: ptr(1),
	}}
	got, ok := EstimateTokenPricingMicrocreditsWithCache(10_000, 5_000, 8_000, 0, pricing, 1)
	if !ok || got != 2_280_000 {
		t.Fatalf("cache amount = %d ok=%v, want 2280000", got, ok)
	}
	// Cache counts are bounded to the input total rather than producing a
	// negative normal-input component: input=10, cached=9, write=9 clamps write
	// to 1 and normal to 0, leaving 9×0.1/10K + 1×1/10K = 0.00019 Credits,
	// which rounds down to zero at the 0.001-Credit quantum.
	bounded, ok := EstimateTokenPricingMicrocreditsWithCache(10, 0, 9, 9, pricing, 1)
	if !ok || bounded != 0 {
		t.Fatalf("bounded cache amount = %d ok=%v, want 0", bounded, ok)
	}
	bNormal, bRead, bWrite, bOutput, _, ok := TokenPricingCreditComponentsDetailedWithCache(10, 0, 9, 9, pricing, 1)
	if !ok || bNormal != 0 || bRead != 0.00009 || bWrite != 0.0001 || bOutput != 0 {
		t.Fatalf("bounded directional components = %v/%v/%v/%v, want 0/0.00009/0.0001/0", bNormal, bRead, bWrite, bOutput)
	}
	normal, read, write, output, _, ok := TokenPricingCreditComponentsDetailedWithCache(10_000, 5_000, 8_000, 0, pricing, 1)
	if !ok || normal != 0.2 || read != 0.08 || write != 0 || output != 2 {
		t.Fatalf("directional cache components = %v/%v/%v/%v", normal, read, write, output)
	}
}

func TestResolveTokenPricingRejectsInvalidOptionalCachePrices(t *testing.T) {
	if _, ok := ResolveTokenPricing(TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2, CacheReadCreditsPer10K: ptr(-0.5)}, time.Now()); ok {
		t.Fatal("negative cache read Credits price must not resolve")
	}
	nan := math.NaN()
	if _, ok := ResolveTokenPricing(TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2, CacheWriteCreditsPer10K: &nan}, time.Now()); ok {
		t.Fatal("NaN cache write Credits price must not resolve")
	}
	if _, ok := ResolveTokenPricing(TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2, CacheReadRMBPer10K: ptr(-1)}, time.Now()); ok {
		t.Fatal("negative cache RMB price must not resolve")
	}
}

func TestNewPricingQuoteSnapshotRejectsInvalidCachePrices(t *testing.T) {
	expiresAt := time.Now().Add(time.Minute)
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4, CacheReadRMBPer10K: ptr(-0.01)}}
	if _, ok := NewPricingQuoteSnapshot("req-bad", "attempt-bad", "provider-1", pricing, 1, 1, 100, 10, expiresAt); ok {
		t.Fatal("negative cache RMB price must be rejected")
	}
	nan := math.NaN()
	pricing.CacheReadRMBPer10K = nil
	pricing.CacheWriteCreditsPer10K = &nan
	if _, ok := NewPricingQuoteSnapshot("req-nan", "attempt-nan", "provider-1", pricing, 1, 1, 100, 10, expiresAt); ok {
		t.Fatal("NaN cache Credits price must be rejected")
	}
}

func TestResolveTokenPricingDefaultsAndExplicitZeroCacheRates(t *testing.T) {
	resolved, ok := ResolveTokenPricing(TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}, time.Now())
	if !ok || resolved.Version != "cache-v1" ||
		OptionalTokenPriceValue(resolved.CacheReadCreditsPer10K) != 0.1 || OptionalTokenPriceValue(resolved.CacheWriteCreditsPer10K) != 1 {
		t.Fatalf("default cache pricing = %#v", resolved)
	}
	// An explicit zero is a deliberate free cache direction. It must survive
	// resolution unchanged instead of being rewritten to the default.
	explicit, ok := ResolveTokenPricing(TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4, Version: "cache-v1", CacheReadCreditsPer10K: ptr(0), CacheWriteCreditsPer10K: ptr(0)}, time.Now())
	if !ok || explicit.CacheReadCreditsPer10K == nil || *explicit.CacheReadCreditsPer10K != 0 ||
		explicit.CacheWriteCreditsPer10K == nil || *explicit.CacheWriteCreditsPer10K != 0 {
		t.Fatalf("explicit zero cache pricing was rewritten: %#v", explicit)
	}
	credits, ok := EstimateTokenPricingMicrocreditsWithCache(10_000, 0, 10_000, 0, explicit, 1)
	if !ok || credits != 0 {
		t.Fatalf("explicit zero cache read should be free, credits=%d ok=%v", credits, ok)
	}
}

// TestTokenPricingCachePriceJSONRoundTripPinsPresence locks the wire contract:
// an unset cache price stays absent, while an explicit zero is serialized as 0
// and decodes back as a non-nil zero rather than an unset field.
func TestTokenPricingCachePriceJSONRoundTripPinsPresence(t *testing.T) {
	encoded, err := json.Marshal(TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4, CacheReadCreditsPer10K: ptr(0)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, present := raw["cache_write_credits_per_10k"]; present {
		t.Fatalf("unset cache write price was serialized: %s", encoded)
	}
	if value, present := raw["cache_read_credits_per_10k"]; !present || value != float64(0) {
		t.Fatalf("explicit zero cache read price lost in JSON: %s", encoded)
	}
	var decoded TokenPricing
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.CacheReadCreditsPer10K == nil || *decoded.CacheReadCreditsPer10K != 0 {
		t.Fatalf("explicit zero did not survive round trip: %#v", decoded.CacheReadCreditsPer10K)
	}
	if decoded.CacheWriteCreditsPer10K != nil {
		t.Fatalf("unset cache write price materialized: %#v", decoded.CacheWriteCreditsPer10K)
	}
	// Legacy JSON produced before cache-v1 carries numbers or omits the keys;
	// both shapes must still decode.
	var legacy TokenPricing
	if err := json.Unmarshal([]byte(`{"input_credits_per_10k":2,"cache_read_credits_per_10k":0.2}`), &legacy); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if legacy.CacheReadCreditsPer10K == nil || *legacy.CacheReadCreditsPer10K != 0.2 || legacy.CacheWriteCreditsPer10K != nil {
		t.Fatalf("legacy JSON did not decode: %#v", legacy)
	}
}

func TestResolveTokenPricingDerivesCacheDefaultsFromActiveWindow(t *testing.T) {
	input := 4.0
	pricing := TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4, PriceSchedule: []TokenPriceWindow{{ID: "peak", Start: "00:00", End: "23:59", InputCreditsPer10K: &input}}}
	resolved, ok := ResolveTokenPricing(pricing, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	if !ok || resolved.InputCreditsPer10K != 4 ||
		OptionalTokenPriceValue(resolved.CacheReadCreditsPer10K) != 0.4 || OptionalTokenPriceValue(resolved.CacheWriteCreditsPer10K) != 4 {
		t.Fatalf("resolved cache defaults = %+v, want input 4/read .4/write 4", resolved)
	}
}

func TestLegacyCachePricingUsesCacheReadDiscount(t *testing.T) {
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{InputCreditsPer10K: 2, OutputCreditsPer10K: 0, Version: "legacy-v1"}}
	got, ok := EstimateTokenPricingMicrocreditsWithCache(10_000, 0, 10_000, 0, pricing, 1)
	if !ok || MicrocreditsToCredits(got) != 0.2 {
		t.Fatalf("legacy cached input charge = %v, want 0.2", MicrocreditsToCredits(got))
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

func TestNewPricingQuoteSnapshotReservesMostExpensiveCacheDirection(t *testing.T) {
	pricing := ResolvedTokenPricing{TokenPricing: TokenPricing{
		InputCreditsPer10K: 1, OutputCreditsPer10K: 4,
		CacheReadCreditsPer10K: ptr(2), CacheWriteCreditsPer10K: ptr(3),
	}}
	quote, ok := NewPricingQuoteSnapshot("req-cache", "attempt-cache", "provider-1", pricing, 1, 1, 20_000, 5_000, time.Now().Add(time.Minute))
	if !ok {
		t.Fatal("expected quote")
	}
	// Worst case: all 20k input tokens are Cache Write (3 Credits/10k),
	// plus 5k output at 4 Credits/10k.
	if got, want := quote.ReservedMicrocredits, int64(8*MicrocreditsPerCredit); got != want {
		t.Fatalf("reserved=%d, want worst-case %d", got, want)
	}
}

// TestTokenPricingSnapshotWireRoundTripPreservesExplicitZeroCachePrices covers
// the HubCenter -> Hub snapshot header boundary: an explicitly configured zero
// cache price (a deliberate free direction) must survive base64/JSON encoding
// and decoding, and settlement must then charge that direction at zero while
// still deriving defaults for the unset direction.
func TestTokenPricingSnapshotWireRoundTripPreservesExplicitZeroCachePrices(t *testing.T) {
	snapshot := TokenPricingSnapshot{
		ProviderID:    "official-1",
		PricingSource: PricingSourceProvider,
		Pricing: ResolvedTokenPricing{TokenPricing: TokenPricing{
			Version:             "cache-v1",
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 4,
			// Cache Read is explicitly free; Cache Write is left unset and
			// must receive the input-price default after decoding.
			CacheReadCreditsPer10K: ptr(0),
		}},
		InputTokens:       10_000,
		OutputTokens:      1_000,
		CachedInputTokens: 5_000,
	}
	encoded, ok := EncodeTokenPricingSnapshot(snapshot)
	if !ok {
		t.Fatal("encode failed")
	}
	decoded, ok := DecodeTokenPricingSnapshot(encoded)
	if !ok {
		t.Fatal("decode failed")
	}
	if decoded.Pricing.CacheReadCreditsPer10K == nil || *decoded.Pricing.CacheReadCreditsPer10K != 0 {
		t.Fatalf("explicit zero cache-read price lost across the wire: %#v", decoded.Pricing.CacheReadCreditsPer10K)
	}
	// Settlement on the Hub side: 5k normal input at 1 + 5k cache read at 0 +
	// 1k output at 4 = 0.5 + 0 + 0.4 = 0.9 Credits.
	amount, ok := EstimateTokenPricingMicrocreditsWithCache(decoded.InputTokens, decoded.OutputTokens, decoded.CachedInputTokens, decoded.CacheWriteTokens, decoded.Pricing, 1)
	if !ok {
		t.Fatal("settlement failed")
	}
	if want := int64(900_000); amount != want {
		t.Fatalf("settled microcredits = %d, want %d (cache read must be free)", amount, want)
	}
}
