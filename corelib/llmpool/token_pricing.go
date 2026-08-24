package llmpool

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"
)

const TokenPricingSnapshotHeader = "X-MaClaw-Pricing-Snapshot"

// PricingQuoteHeader carries a short-lived opaque quote credential between a
// Hub and HubCenter. It is deliberately a header (never a model request
// field), so it cannot be forwarded to a third-party provider or persisted in
// user-facing billing records.
const PricingQuoteHeader = "X-MaClaw-Pricing-Quote"

const (
	// MicrocreditsPerCredit is the ledger precision. Configuration is accepted
	// as decimal Credits, while request billing is calculated in this integer
	// unit to avoid float drift between retries and reports.
	MicrocreditsPerCredit int64 = 1_000_000
	// BillingRoundMicrocredits is 0.001 Credit. A request is rounded only once,
	// after input, output and the minimum charge have been combined.
	BillingRoundMicrocredits int64 = 1_000
)

const (
	BillingModePaid = "paid"
	BillingModeFree = "free"
)

// TokenPricing describes the base, provider-owned token price for one
// provider/model route. Credits are per 10,000 tokens. RMB fields are display
// only and must never be used to derive a credit charge.
type TokenPricing struct {
	InputCreditsPer10K    float64            `json:"input_credits_per_10k,omitempty"`
	OutputCreditsPer10K   float64            `json:"output_credits_per_10k,omitempty"`
	InputRMBPer10K        float64            `json:"input_rmb_per_10k,omitempty"`
	OutputRMBPer10K       float64            `json:"output_rmb_per_10k,omitempty"`
	MinimumRequestCredits float64            `json:"minimum_request_credits,omitempty"`
	Timezone              string             `json:"timezone,omitempty"`
	PriceSchedule         []TokenPriceWindow `json:"price_schedule,omitempty"`
	Version               string             `json:"version,omitempty"`
}

// TokenPriceWindow overrides selected fields of TokenPricing during a local
// provider time window. Nil values inherit the route default, allowing an
// explicit zero price without confusing it with an omitted field.
type TokenPriceWindow struct {
	ID                    string   `json:"id,omitempty"`
	Days                  []int    `json:"days,omitempty"`
	Start                 string   `json:"start"`
	End                   string   `json:"end"`
	InputCreditsPer10K    *float64 `json:"input_credits_per_10k,omitempty"`
	OutputCreditsPer10K   *float64 `json:"output_credits_per_10k,omitempty"`
	InputRMBPer10K        *float64 `json:"input_rmb_per_10k,omitempty"`
	OutputRMBPer10K       *float64 `json:"output_rmb_per_10k,omitempty"`
	MinimumRequestCredits *float64 `json:"minimum_request_credits,omitempty"`
}

// ResolvedTokenPricing is the immutable price snapshot used for one request.
type ResolvedTokenPricing struct {
	TokenPricing
	WindowID string `json:"window_id,omitempty"`
}

// TokenPricingSnapshot is the authenticated HubCenter-to-Hub fact used to
// bill an official provider. It deliberately contains base pricing only: Hub
// adds its own service-group multiplier exactly once.
type TokenPricingSnapshot struct {
	ProviderID    string               `json:"provider_id"`
	UpstreamModel string               `json:"upstream_model"`
	Pricing       ResolvedTokenPricing `json:"pricing"`
	InputTokens   int64                `json:"input_tokens"`
	OutputTokens  int64                `json:"output_tokens"`
}

// PricingQuoteSnapshot is the immutable, non-secret financial envelope for a
// single upstream attempt.  It is intentionally separate from the eventual
// usage record: input/output token counts below are limits used before a
// request is sent, while TokenPricingSnapshot contains actual usage after it
// completes.  A transport credential used to authorize a quote must never be
// embedded in this value or persisted in a ledger.
//
// Hub owns the group multiplier. Providers (including HubCenter official
// providers) own Pricing.  Keeping both snapshots together makes a later
// final settlement independent of price-schedule/config changes.
type PricingQuoteSnapshot struct {
	RequestID              string               `json:"request_id"`
	AttemptID              string               `json:"attempt_id"`
	TenantID               string               `json:"tenant_id,omitempty"`
	LogicalModel           string               `json:"logical_model,omitempty"`
	ProviderID             string               `json:"provider_id"`
	UpstreamModel          string               `json:"upstream_model,omitempty"`
	Pricing                ResolvedTokenPricing `json:"pricing"`
	ServiceGroupIDs        []string             `json:"service_group_ids,omitempty"`
	BillingGroupMultiplier float64              `json:"billing_group_multiplier"`
	InputTokenEstimate     int64                `json:"input_token_estimate"`
	OutputTokenLimit       int64                `json:"output_token_limit"`
	ReservedMicrocredits   int64                `json:"reserved_microcredits"`
	ExpiresAt              time.Time            `json:"expires_at"`
}

// NewPricingQuoteSnapshot validates and freezes a quote using the same
// fixed-point calculation as final billing.  It accepts an input estimate and
// output ceiling rather than actual usage, so callers can reject requests
// whose maximum possible debit cannot be covered before contacting upstream.
func NewPricingQuoteSnapshot(requestID, attemptID, providerID string, pricing ResolvedTokenPricing, billingGroupMultiplier float64, inputTokenEstimate, outputTokenLimit int64, expiresAt time.Time) (PricingQuoteSnapshot, bool) {
	requestID = strings.TrimSpace(requestID)
	attemptID = strings.TrimSpace(attemptID)
	providerID = strings.TrimSpace(providerID)
	if requestID == "" || attemptID == "" || providerID == "" || expiresAt.IsZero() {
		return PricingQuoteSnapshot{}, false
	}
	reserved, ok := EstimateTokenPricingMicrocredits(inputTokenEstimate, outputTokenLimit, pricing, billingGroupMultiplier)
	if !ok {
		return PricingQuoteSnapshot{}, false
	}
	return PricingQuoteSnapshot{
		RequestID:              requestID,
		AttemptID:              attemptID,
		ProviderID:             providerID,
		Pricing:                pricing,
		BillingGroupMultiplier: billingGroupMultiplier,
		InputTokenEstimate:     maxTokenCount(inputTokenEstimate),
		OutputTokenLimit:       maxTokenCount(outputTokenLimit),
		ReservedMicrocredits:   reserved,
		ExpiresAt:              expiresAt.UTC(),
	}, true
}

func EncodeTokenPricingSnapshot(snapshot TokenPricingSnapshot) (string, bool) {
	if !ValidateResolvedTokenPricing(snapshot.Pricing) {
		return "", false
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || len(encoded) > 4096 {
		return "", false
	}
	return base64.RawURLEncoding.EncodeToString(encoded), true
}

func DecodeTokenPricingSnapshot(raw string) (TokenPricingSnapshot, bool) {
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(encoded) == 0 || len(encoded) > 4096 {
		return TokenPricingSnapshot{}, false
	}
	var snapshot TokenPricingSnapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil || strings.TrimSpace(snapshot.ProviderID) == "" {
		return TokenPricingSnapshot{}, false
	}
	if !ValidateResolvedTokenPricing(snapshot.Pricing) {
		return TokenPricingSnapshot{}, false
	}
	return snapshot, true
}

// ValidateResolvedTokenPricing validates the already-frozen price values in a
// request snapshot. Unlike ResolveTokenPricing, it intentionally does not
// inspect PriceSchedule or the current clock: applying a new time window while
// decoding an old snapshot would turn a historical billing fact back into a
// live configuration lookup.
func ValidateResolvedTokenPricing(pricing ResolvedTokenPricing) bool {
	p := pricing.TokenPricing
	return len(p.PriceSchedule) == 0 && p.HasCreditPricing() && validNonNegative(p.MinimumRequestCredits) &&
		validNonNegative(p.InputRMBPer10K) && validNonNegative(p.OutputRMBPer10K)
}

// HasCreditPricing reports whether the route has an explicit Credits price.
func (p TokenPricing) HasCreditPricing() bool {
	return validNonNegative(p.InputCreditsPer10K) && validNonNegative(p.OutputCreditsPer10K) &&
		(p.InputCreditsPer10K > 0 || p.OutputCreditsPer10K > 0 || p.MinimumRequestCredits > 0)
}

// EffectiveRouteTokenPricing resolves the billable base price for one provider
// route. Route-level pricing wins; a route without its own Credits price
// inherits the provider-wide default. The result may still carry no pricing —
// callers must gate billing on HasCreditPricing.
func EffectiveRouteTokenPricing(route ModelProviderConfig, provider ProviderConfig) TokenPricing {
	if route.TokenPricing.HasCreditPricing() {
		return route.TokenPricing
	}
	return provider.TokenPricing
}

// ResolveTokenPricing freezes the route's price at startedAt. Invalid or
// overlapping windows are ignored here; configuration validation must reject
// them before a route is enabled.
func ResolveTokenPricing(p TokenPricing, startedAt time.Time) (ResolvedTokenPricing, bool) {
	if !p.HasCreditPricing() || !validNonNegative(p.MinimumRequestCredits) ||
		!validNonNegative(p.InputRMBPer10K) || !validNonNegative(p.OutputRMBPer10K) {
		return ResolvedTokenPricing{}, false
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	if strings.TrimSpace(p.Timezone) == "" {
		p.Timezone = DefaultCreditMultiplierTimezone
	}
	if strings.TrimSpace(p.Version) == "" {
		p.Version = "legacy-v1"
	}
	// A request snapshot contains final, time-of-use values only. Leaving the
	// live schedule attached would make an immutable billing fact ambiguous when
	// it crosses a time boundary or configuration update.
	schedule := p.PriceSchedule
	p.PriceSchedule = nil
	resolved := ResolvedTokenPricing{TokenPricing: p}
	local := startedAt.In(loadTokenPricingLocation(p.Timezone))
	weekday, minute := int(local.Weekday()), local.Hour()*60+local.Minute()
	for _, window := range schedule {
		if !tokenPriceWindowMatches(window, weekday, minute) {
			continue
		}
		if !applyTokenPriceWindow(&resolved.TokenPricing, window) {
			return ResolvedTokenPricing{}, false
		}
		resolved.WindowID = strings.TrimSpace(window.ID)
		break
	}
	return resolved, true
}

func applyTokenPriceWindow(p *TokenPricing, window TokenPriceWindow) bool {
	if p == nil {
		return false
	}
	apply := func(target *float64, value *float64) bool {
		if value == nil {
			return true
		}
		if !validNonNegative(*value) {
			return false
		}
		*target = *value
		return true
	}
	return apply(&p.InputCreditsPer10K, window.InputCreditsPer10K) &&
		apply(&p.OutputCreditsPer10K, window.OutputCreditsPer10K) &&
		apply(&p.InputRMBPer10K, window.InputRMBPer10K) &&
		apply(&p.OutputRMBPer10K, window.OutputRMBPer10K) &&
		apply(&p.MinimumRequestCredits, window.MinimumRequestCredits) &&
		p.HasCreditPricing()
}

func validNonNegative(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

// EstimateTokenPricingMicrocredits calculates a request amount without using
// binary floating-point arithmetic. Price and multiplier floats are only
// configuration input; they are first converted through their decimal form.
// The result is rounded half-up to BillingRoundMicrocredits exactly once.
func EstimateTokenPricingMicrocredits(inputTokens, outputTokens int64, pricing ResolvedTokenPricing, billingGroupMultiplier float64) (int64, bool) {
	if !validNonNegative(billingGroupMultiplier) || billingGroupMultiplier <= 0 {
		return 0, false
	}
	inputPrice, ok := decimalCreditsToMicrocredits(pricing.InputCreditsPer10K)
	if !ok {
		return 0, false
	}
	outputPrice, ok := decimalCreditsToMicrocredits(pricing.OutputCreditsPer10K)
	if !ok {
		return 0, false
	}
	minimum, ok := decimalCreditsToMicrocredits(pricing.MinimumRequestCredits)
	if !ok {
		return 0, false
	}
	multiplier, ok := decimalToRat(billingGroupMultiplier)
	if !ok || multiplier.Sign() <= 0 {
		return 0, false
	}
	inputTokens = maxTokenCount(inputTokens)
	outputTokens = maxTokenCount(outputTokens)

	// ((inputTokens * inputPrice) + (outputTokens * outputPrice)) / 10,000
	// yields microcredits before the service-group multiplier.
	raw := new(big.Int).Mul(big.NewInt(inputTokens), big.NewInt(inputPrice))
	raw.Add(raw, new(big.Int).Mul(big.NewInt(outputTokens), big.NewInt(outputPrice)))
	amount := new(big.Rat).SetInt(raw)
	amount.Quo(amount, big.NewRat(10_000, 1))
	amount.Mul(amount, multiplier)

	minimumAmount := new(big.Rat).SetInt64(minimum)
	minimumAmount.Mul(minimumAmount, multiplier)
	if amount.Cmp(minimumAmount) < 0 {
		amount = minimumAmount
	}
	return roundRatHalfUpToQuantum(amount, BillingRoundMicrocredits)
}

// MicrocreditsToCredits is intended only for JSON/UI compatibility. Ledger
// writes and equality comparisons must retain the integer amount.
func MicrocreditsToCredits(value int64) float64 {
	return float64(value) / float64(MicrocreditsPerCredit)
}

func decimalCreditsToMicrocredits(value float64) (int64, bool) {
	rat, ok := decimalToRat(value)
	if !ok || rat.Sign() < 0 {
		return 0, false
	}
	rat.Mul(rat, big.NewRat(MicrocreditsPerCredit, 1))
	return roundRatHalfUpToQuantum(rat, 1)
}

func decimalToRat(value float64) (*big.Rat, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, false
	}
	// 'f' avoids scientific notation and preserves the JSON-style decimal the
	// operator configured as far as a float64 value can represent it.
	raw := strconv.FormatFloat(value, 'f', -1, 64)
	rat, ok := new(big.Rat).SetString(raw)
	return rat, ok
}

func roundRatHalfUpToQuantum(value *big.Rat, quantum int64) (int64, bool) {
	if value == nil || quantum <= 0 || value.Sign() < 0 {
		return 0, false
	}
	denominator := new(big.Int).Mul(value.Denom(), big.NewInt(quantum))
	numerator := value.Num()
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	// half-up for non-negative monetary values.
	if new(big.Int).Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	quotient.Mul(quotient, big.NewInt(quantum))
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}

func maxTokenCount(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

// NormalizeBillingMode preserves an empty value for legacy configurations.
// New configuration must use either paid or free and is validated by
// ValidateRouteBilling.
func NormalizeBillingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case BillingModePaid:
		return BillingModePaid
	case BillingModeFree:
		return BillingModeFree
	default:
		return ""
	}
}

// ValidateRouteBilling validates the provider-owned token price before it is
// stored. A route can be legacy (no mode and no token price), but a new paid
// route must have an explicit usable Credits price. RMB is display-only.
func ValidateRouteBilling(mode string, pricing TokenPricing) error {
	rawMode := strings.TrimSpace(mode)
	mode = NormalizeBillingMode(mode)
	if rawMode != "" && mode == "" {
		return fmt.Errorf("billing_mode must be %q or %q", BillingModePaid, BillingModeFree)
	}
	if mode == BillingModeFree {
		return validateTokenPricingShape(pricing, false)
	}
	if mode == BillingModePaid || pricing.HasCreditPricing() {
		if !pricing.HasCreditPricing() {
			return fmt.Errorf("paid route requires a non-zero Credits price or minimum")
		}
		return validateTokenPricingShape(pricing, true)
	}
	return validateTokenPricingShape(pricing, false)
}

func validateTokenPricingShape(pricing TokenPricing, requirePrice bool) error {
	if !validNonNegative(pricing.InputCreditsPer10K) || !validNonNegative(pricing.OutputCreditsPer10K) ||
		!validNonNegative(pricing.InputRMBPer10K) || !validNonNegative(pricing.OutputRMBPer10K) ||
		!validNonNegative(pricing.MinimumRequestCredits) {
		return fmt.Errorf("token prices must be finite non-negative numbers")
	}
	if requirePrice && !pricing.HasCreditPricing() {
		return fmt.Errorf("token Credits price is required")
	}
	if len(pricing.PriceSchedule) == 0 {
		return nil
	}
	if strings.TrimSpace(pricing.Timezone) == "" {
		return fmt.Errorf("timezone is required when price_schedule is configured")
	}
	if _, err := time.LoadLocation(strings.TrimSpace(pricing.Timezone)); err != nil {
		return fmt.Errorf("invalid token-pricing timezone %q", pricing.Timezone)
	}
	occupied := [7][24 * 60]bool{}
	for i, window := range pricing.PriceSchedule {
		if strings.TrimSpace(window.ID) == "" {
			return fmt.Errorf("price_schedule[%d] requires an id", i)
		}
		start, ok := parseTokenPricingClock(window.Start)
		if !ok {
			return fmt.Errorf("price_schedule[%d] has invalid start", i)
		}
		end, ok := parseTokenPricingClock(window.End)
		if !ok || start == end {
			return fmt.Errorf("price_schedule[%d] has invalid end", i)
		}
		for _, day := range window.Days {
			if day < 0 || day > 6 {
				return fmt.Errorf("price_schedule[%d] has invalid weekday", i)
			}
		}
		for _, value := range []*float64{window.InputCreditsPer10K, window.OutputCreditsPer10K, window.InputRMBPer10K, window.OutputRMBPer10K, window.MinimumRequestCredits} {
			if value != nil && !validNonNegative(*value) {
				return fmt.Errorf("price_schedule[%d] has invalid override", i)
			}
		}
		days := window.Days
		if len(days) == 0 {
			days = []int{0, 1, 2, 3, 4, 5, 6}
		}
		duration := end - start
		if duration <= 0 {
			duration += 24 * 60
		}
		for _, day := range days {
			for offset := 0; offset < duration; offset++ {
				absolute := start + offset
				weekday := (day + absolute/(24*60)) % 7
				minute := absolute % (24 * 60)
				if occupied[weekday][minute] {
					return fmt.Errorf("price_schedule[%d] overlaps another window", i)
				}
				occupied[weekday][minute] = true
			}
		}
	}
	return nil
}

var tokenPricingLocations sync.Map // string -> *time.Location

func loadTokenPricingLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultCreditMultiplierTimezone
	}
	if cached, ok := tokenPricingLocations.Load(name); ok {
		if loc, _ := cached.(*time.Location); loc != nil {
			return loc
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc = time.UTC
	}
	tokenPricingLocations.Store(name, loc)
	return loc
}

func tokenPriceWindowMatches(window TokenPriceWindow, weekday, minute int) bool {
	start, ok := parseTokenPricingClock(window.Start)
	if !ok {
		return false
	}
	end, ok := parseTokenPricingClock(window.End)
	if !ok || start == end {
		return false
	}
	if start < end {
		return tokenPricingWeekdayMatches(window.Days, weekday) && minute >= start && minute < end
	}
	if minute >= start {
		return tokenPricingWeekdayMatches(window.Days, weekday)
	}
	previous := (weekday + 6) % 7
	return minute < end && (tokenPricingWeekdayMatches(window.Days, weekday) || tokenPricingWeekdayMatches(window.Days, previous))
}

func tokenPricingWeekdayMatches(days []int, weekday int) bool {
	if len(days) == 0 {
		return true
	}
	for _, day := range days {
		if day == weekday {
			return true
		}
	}
	return false
}

func parseTokenPricingClock(raw string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}
