package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const defaultLLMPricingQuoteOutputTokenLimit int64 = 65_536

type llmBillingStateKey struct{}

type llmBillingState struct {
	mu                 sync.Mutex
	started            time.Time
	requestID          string
	applied            float64
	officialProviderID string
	officialPricing    *llmpool.TokenPricingSnapshot
	officialQuote      *llmservice.OfficialPricingQuote
	// quotes freezes the local provider price selected at admission time. It is
	// deliberately request-scoped: an operator changing a time-of-day price
	// while an upstream call is in flight cannot change that request's debit.
	quotes             map[string]llmpool.PricingQuoteSnapshot
	reservationHeld    bool
	upstreamSent       bool
	settlementQueued   bool
	noUpstreamDispatch bool
}

func withLLMBillingState(ctx context.Context, startedAt time.Time, requestIDs ...string) context.Context {
	requestID := ""
	if len(requestIDs) > 0 {
		requestID = requestIDs[0]
	}
	return context.WithValue(ctx, llmBillingStateKey{}, &llmBillingState{started: startedAt, requestID: strings.TrimSpace(requestID), quotes: map[string]llmpool.PricingQuoteSnapshot{}})
}

func llmBillingRequestID(ctx context.Context) string {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.requestID
}

func llmBillingStateFrom(ctx context.Context) *llmBillingState {
	state, _ := ctx.Value(llmBillingStateKey{}).(*llmBillingState)
	return state
}

func noteOfficialBilling(ctx context.Context, value float64, providerID string) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		state.applied = value
	}
	if id := strings.TrimSpace(providerID); id != "" {
		state.officialProviderID = id
	}
	state.mu.Unlock()
}

func noteOfficialCreditMultiplierFromHeader(ctx context.Context, header http.Header) {
	if header == nil {
		return
	}
	noteOfficialBilling(ctx, llmpool.ParseCreditMultiplierHeader(header.Get(llmpool.CreditMultiplierHeader)), header.Get(llmpool.ProviderIDHeader))
	if snapshot, ok := llmpool.DecodeTokenPricingSnapshot(header.Get(llmpool.TokenPricingSnapshotHeader)); ok && validOfficialTokenPricingSnapshot(snapshot) {
		noteOfficialTokenPricing(ctx, &snapshot)
	}
}

func snapshotOfficialBilling(ctx context.Context) (multiplier float64, providerID string) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return 0, ""
	}
	state.mu.Lock()
	multiplier = state.applied
	providerID = state.officialProviderID
	state.mu.Unlock()
	return multiplier, providerID
}

func snapshotOfficialTokenPricing(ctx context.Context) *llmpool.TokenPricingSnapshot {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.officialPricing == nil {
		return nil
	}
	copySnapshot := *state.officialPricing
	return &copySnapshot
}

// noteOfficialTokenPricing copies HubCenter's authenticated final directional
// pricing and usage into this request's billing state. It is kept separate
// from the legacy multiplier so a singleflight follower can settle from the
// same immutable fact as the request that performed the upstream call.
func noteOfficialTokenPricing(ctx context.Context, snapshot *llmpool.TokenPricingSnapshot) {
	if snapshot == nil {
		return
	}
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	copySnapshot := *snapshot
	state.officialPricing = &copySnapshot
	state.mu.Unlock()
}

// noteOfficialNoUpstreamDispatch records a narrow, local proof that Hub never
// sent the request to HubCenter. It must never be set for HTTP or transport
// failures, which remain ambiguous after dispatch begins.
func noteOfficialNoUpstreamDispatch(ctx context.Context, noUpstreamDispatch bool) {
	if !noUpstreamDispatch {
		return
	}
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		state.noUpstreamDispatch = true
		state.mu.Unlock()
	}
}

func snapshotOfficialNoUpstreamDispatch(ctx context.Context) bool {
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		defer state.mu.Unlock()
		return state.noUpstreamDispatch
	}
	return false
}

func rememberLLMPricingQuote(ctx context.Context, quote llmpool.PricingQuoteSnapshot) {
	state := llmBillingStateFrom(ctx)
	if state == nil || strings.TrimSpace(quote.ProviderID) == "" {
		return
	}
	state.mu.Lock()
	if state.quotes == nil {
		state.quotes = map[string]llmpool.PricingQuoteSnapshot{}
	}
	state.quotes[strings.ToLower(strings.TrimSpace(quote.ProviderID))] = quote
	state.mu.Unlock()
}

func rememberOfficialPricingQuote(ctx context.Context, serviceReg *llmservice.Registry, model *llmservice.AuthorizedModel, quote llmservice.OfficialPricingQuote, serviceGroupIDs []string, inputEstimate, outputLimit int64) error {
	if !quote.ExpiresAt.After(time.Now().UTC()) || strings.TrimSpace(quote.ProviderID) == "" {
		return fmt.Errorf("official pricing quote is invalid or expired")
	}
	requestID := llmBillingRequestID(ctx)
	if requestID == "" {
		return fmt.Errorf("official pricing quote requires request id")
	}
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return fmt.Errorf("official pricing quote requires billing state")
	}
	state.mu.Lock()
	startedAt := state.started
	state.mu.Unlock()
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	// Hub is the sole owner of the group multiplier. HubCenter's quote contains
	// only the official provider base price.
	multiplier := llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
	_ = startedAt // retained as the pricing-start ownership boundary for callers.
	frozen, ok := llmpool.NewPricingQuoteSnapshot(requestID, requestID+":"+llmservice.MaClawOfficialProviderID, llmservice.MaClawOfficialProviderID, quote.Pricing, multiplier, inputEstimate, outputLimit, quote.ExpiresAt)
	if !ok {
		return fmt.Errorf("unable to freeze official pricing quote")
	}
	frozen.TenantID = ""
	// Admission is selected by Hub's logical model, while UpstreamModel identifies
	// the concrete HubCenter route. These can differ for official providers.
	if model != nil {
		frozen.LogicalModel = strings.TrimSpace(model.Name)
	}
	if frozen.LogicalModel == "" {
		frozen.LogicalModel = quote.UpstreamModel
	}
	frozen.UpstreamModel = quote.UpstreamModel
	frozen.ServiceGroupIDs = append([]string(nil), serviceGroupIDs...)
	rememberLLMPricingQuote(ctx, frozen)
	state.mu.Lock()
	copyQuote := quote
	state.officialQuote = &copyQuote
	state.mu.Unlock()
	return nil
}

// prepareOfficialLLMRequestPricingQuote locks HubCenter's concrete provider and
// time-of-use base price before Hub reserves the user's Credits. If an older
// HubCenter does not support quotes, forwarding retains its compatibility path.
func prepareOfficialLLMRequestPricingQuote(ctx context.Context, serviceReg *llmservice.Registry, providerReg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any) error {
	if model == nil || providerReg == nil {
		return nil
	}
	for _, item := range orderAuthorizedProviders(body, model, providerReg) {
		providerID := strings.TrimSpace(item.Route.ProviderID)
		if !IsMaClawProviderRequest(providerID) {
			continue
		}
		if !officialRouteHasDirectionalPricing(model, providerID) {
			// A legacy official route without directional pricing continues through
			// the historical forward path. It cannot safely be quote-reserved yet.
			continue
		}
		forwardBody := rewriteOfficialForwardBody(body, model, providerID)
		payload, err := json.Marshal(forwardBody)
		if err != nil {
			return fmt.Errorf("marshal official pricing quote request: %w", err)
		}
		forwardGroupIDs := officialForwardServiceGroupIDs(model, providerID)
		quote, err := QuoteViaMaClaw(ctx, payload, store.TenantIDFromContext(ctx), forwardGroupIDs...)
		if err != nil {
			// Quote support and any request validation remain a forwarding concern.
			// This admission optimisation must never alter the established upstream
			// error/retry contract (notably 400 validation and local test stubs).
			return nil
		}
		return rememberOfficialPricingQuote(ctx, serviceReg, model, quote, llmservice.ChargedServiceGroupIDs(model, providerID), estimateLLMQuoteInputTokens(forwardBody), llmQuoteOutputTokenLimit(forwardBody))
	}
	return nil
}

func officialRouteHasDirectionalPricing(model *llmservice.AuthorizedModel, providerID string) bool {
	if model == nil {
		return false
	}
	_, ok := llmservice.ResolveTokenPricingForProviderRoute(model, providerID, billingUpstreamModel(model, providerID), time.Now())
	return ok
}

func billingUpstreamModel(model *llmservice.AuthorizedModel, providerID string) string {
	if model == nil {
		return ""
	}
	return llmservice.OfficialUpstreamModelForLogicalModel(model, providerID, model.Name)
}

func snapshotOfficialForwardQuote(ctx context.Context) (llmservice.OfficialPricingQuote, bool) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return llmservice.OfficialPricingQuote{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.officialQuote == nil {
		return llmservice.OfficialPricingQuote{}, false
	}
	return *state.officialQuote, true
}

func snapshotLLMPricingQuote(ctx context.Context, providerID string) (llmpool.PricingQuoteSnapshot, bool) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return llmpool.PricingQuoteSnapshot{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	quote, ok := state.quotes[strings.ToLower(strings.TrimSpace(providerID))]
	return quote, ok
}

// prepareLLMPricingQuote freezes a provider/model price before any upstream
// bytes are sent. A quoted paid route is admitted only when the current
// metered balance covers the request's conservative input+maximum-output
// amount. Unlimited grants still bypass a balance reservation, as they have no
// finite wallet value to compare here.
func prepareLLMPricingQuote(ctx context.Context, reg *llmservice.Registry, userID, email string, model *llmservice.AuthorizedModel, providerID string, body map[string]any, now time.Time) (llmBillingDenial, error) {
	groups := llmservice.ChargedServiceGroupIDs(model, providerID)
	allowed, _, code, message, available, _, _ := llmservice.BillingEligibilityForServiceGroupsForUserID(reg, userID, email, groups, now)
	if !allowed {
		return llmBillingDenial{Code: code, Message: message}, fmt.Errorf("%s", message)
	}
	if llmservice.IsFreeBillingProviderRoute(model, providerID, billingUpstreamModel(model, providerID)) {
		return llmBillingDenial{}, nil
	}
	pricing, ok := llmservice.ResolveTokenPricingForProvider(model, providerID, now)
	if !ok {
		// Legacy routes retain their existing entitlement behavior until their
		// owner assigns directional pricing. They cannot safely be reserved by
		// the new token-pricing path yet.
		return llmBillingDenial{}, nil
	}
	inputEstimate := estimateLLMQuoteInputTokens(body)
	outputLimit := llmQuoteOutputTokenLimit(body)
	multiplier := llmservice.BillingGroupMultiplier(reg, groups)
	requestID := llmBillingRequestID(ctx)
	if requestID == "" {
		requestID = "pricing-check"
	}
	quote, ok := llmpool.NewPricingQuoteSnapshot(requestID, requestID+":"+strings.TrimSpace(providerID), providerID, pricing, multiplier, inputEstimate, outputLimit, now.Add(15*time.Minute))
	if !ok {
		return llmBillingDenial{Code: "LLM_PRICING_QUOTE_INVALID", Message: "unable to calculate the configured token price"}, fmt.Errorf("unable to calculate the configured token price")
	}
	quote.ServiceGroupIDs = append([]string(nil), groups...)
	quote.LogicalModel = strings.TrimSpace(model.Name)
	// A zero available amount represents an unlimited entitlement in the
	// existing registry model. Only finite positive wallet balances participate
	// in the preflight check.
	if available > 0 && quote.ReservedMicrocredits > creditsToMicrocredits(available) {
		return llmBillingDenial{
			Code:    "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST",
			Message: fmt.Sprintf("insufficient credits for this request: need %.3f credits, available %.3f", llmpool.MicrocreditsToCredits(quote.ReservedMicrocredits), available),
		}, fmt.Errorf("insufficient credits for quoted request")
	}
	if llmBillingRequestID(ctx) != "" {
		rememberLLMPricingQuote(ctx, quote)
	}
	return llmBillingDenial{}, nil
}

// reserveLLMRequestPricing turns the selected request's conservative quote
// into one durable hold. It chooses the largest eligible provider quote, so a
// local failover cannot consume more than admission reserved. The immutable
// final ledger later charges actual usage and releases this hold in the same
// registry save.
func reserveLLMRequestPricing(ctx context.Context, system store.SystemSettingsRepository, userID, email string, model *llmservice.AuthorizedModel) (llmBillingDenial, error) {
	state := llmBillingStateFrom(ctx)
	if state == nil || system == nil {
		return llmBillingDenial{}, nil
	}
	state.mu.Lock()
	if state.reservationHeld || state.requestID == "" {
		state.mu.Unlock()
		return llmBillingDenial{}, nil
	}
	var chosen llmpool.PricingQuoteSnapshot
	for _, quote := range state.quotes {
		if model != nil && !strings.EqualFold(strings.TrimSpace(quote.LogicalModel), strings.TrimSpace(model.Name)) {
			continue
		}
		if quote.ReservedMicrocredits > chosen.ReservedMicrocredits {
			chosen = quote
		}
	}
	requestID := state.requestID
	state.mu.Unlock()
	if chosen.ReservedMicrocredits <= 0 || len(chosen.ServiceGroupIDs) == 0 {
		return llmBillingDenial{}, nil
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		return llmBillingDenial{Code: "LLM_BILLING_RESERVATION_FAILED", Message: "unable to reserve credits for this request"}, err
	}
	now := time.Now().UTC()
	reserved, ok := llmservice.ReserveBillingCreditsForUserID(reg, userID, email, chosen.ServiceGroupIDs, requestID, llmpool.MicrocreditsToCredits(chosen.ReservedMicrocredits), chosen.ExpiresAt, now)
	if !ok {
		available := llmservice.AvailableCreditsForServiceGroupsForUserID(reg, userID, email, chosen.ServiceGroupIDs, now)
		return llmBillingDenial{Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", Message: fmt.Sprintf("insufficient credits for this request: need %.3f credits, available %.3f", llmpool.MicrocreditsToCredits(chosen.ReservedMicrocredits), available)}, fmt.Errorf("insufficient credits for quoted request")
	}
	if reserved > 0 {
		if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
			return llmBillingDenial{Code: "LLM_BILLING_RESERVATION_FAILED", Message: "unable to reserve credits for this request"}, err
		}
		invalidateLLMRuntimeCaches(system)
	}
	state.mu.Lock()
	state.reservationHeld = reserved > 0
	state.mu.Unlock()
	return llmBillingDenial{}, nil
}

func markLLMBillingSettlementQueued(ctx context.Context) {
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		state.settlementQueued = true
		state.mu.Unlock()
	}
}

func releaseUnsettledLLMBillingReservation(ctx context.Context, system store.SystemSettingsRepository) {
	state := llmBillingStateFrom(ctx)
	if state == nil || system == nil {
		return
	}
	state.mu.Lock()
	requestID, release := state.requestID, state.reservationHeld && !state.settlementQueued && (!state.upstreamSent || state.noUpstreamDispatch)
	state.mu.Unlock()
	if !release || requestID == "" {
		return
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	reg, err := loadCachedLLMServiceRegistry(context.Background(), system)
	if err != nil || !llmservice.ReleaseBillingReservation(reg, requestID, time.Now().UTC()) {
		return
	}
	if err := llmservice.SaveRegistry(context.Background(), system, reg); err == nil {
		invalidateLLMRuntimeCaches(system)
	}
}

// releaseKnownUnbilledLLMBillingReservation clears a conservative hold when
// Hub can prove no billable upstream dispatch occurred (currently a local
// prompt-cache hit). markLLMBillingReservationSent runs before provider/cache
// dispatch to protect ambiguous network outcomes, so this explicit evidence is
// the only safe way to undo that sent marker without waiting for reconciliation.
func releaseKnownUnbilledLLMBillingReservation(ctx context.Context, system store.SystemSettingsRepository) {
	state := llmBillingStateFrom(ctx)
	if state == nil || system == nil {
		return
	}
	state.mu.Lock()
	requestID, held := state.requestID, state.reservationHeld
	state.mu.Unlock()
	if !held || requestID == "" {
		return
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	reg, err := loadCachedLLMServiceRegistry(context.Background(), system)
	if err != nil || !llmservice.ReleaseBillingReservation(reg, requestID, time.Now().UTC()) {
		return
	}
	if err := llmservice.SaveRegistry(context.Background(), system, reg); err != nil {
		return
	}
	invalidateLLMRuntimeCaches(system)
	state.mu.Lock()
	state.reservationHeld = false
	state.settlementQueued = true
	state.mu.Unlock()
}

// markLLMBillingReservationSent durably records the dispatch boundary before
// an upstream call. Do not release this hold merely because the price quote
// expires: a timeout after dispatch has an unknown billable outcome.
func markLLMBillingReservationSent(ctx context.Context, system store.SystemSettingsRepository) error {
	state := llmBillingStateFrom(ctx)
	if state == nil || system == nil {
		return nil
	}
	state.mu.Lock()
	requestID, held := state.requestID, state.reservationHeld
	var quote llmpool.PricingQuoteSnapshot
	for _, candidate := range state.quotes {
		if IsMaClawProviderRequest(candidate.ProviderID) {
			quote = candidate
			break
		}
		if candidate.ReservedMicrocredits > quote.ReservedMicrocredits {
			quote = candidate
		}
	}
	state.mu.Unlock()
	if !held || requestID == "" {
		return nil
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	reg, err := loadCachedLLMServiceRegistry(context.Background(), system)
	if err != nil {
		return err
	}
	if !llmservice.MarkBillingReservationSent(reg, requestID, time.Now().UTC()) {
		return fmt.Errorf("billing reservation %q is unavailable", requestID)
	}
	if quote.ProviderID != "" && !llmservice.SetBillingReservationBillingDetails(reg, requestID, quote.ProviderID, quote.BillingGroupMultiplier) {
		return fmt.Errorf("billing reservation %q pricing details are unavailable", requestID)
	}
	if err := llmservice.SaveRegistry(context.Background(), system, reg); err != nil {
		return err
	}
	invalidateLLMRuntimeCaches(system)
	state.mu.Lock()
	state.upstreamSent = true
	state.mu.Unlock()
	return nil
}

// OfficialBillingReconciliationResult makes background recovery observable
// without exposing request content or pricing quote tokens.
type OfficialBillingReconciliationResult struct {
	Scanned int
	Settled int
	Pending int
	Failed  int
}

// reconcileOfficialBillingReservations settles sent official requests whose
// response/trailer did not reach Hub. HubCenter's authenticated attempt
// endpoint is the sole source of recovered usage; missing or unknown attempts
// deliberately retain their reservations.
func reconcileOfficialBillingReservations(ctx context.Context, system store.SystemSettingsRepository, reservations []llmservice.BillingReservation) OfficialBillingReconciliationResult {
	result := OfficialBillingReconciliationResult{}
	if system == nil {
		return result
	}
	for _, reservation := range reservations {
		if !IsMaClawProviderRequest(reservation.ProviderID) || reservation.BillingGroupMultiplier <= 0 {
			continue
		}
		// A completed request may already have been settled by the online path
		// after this scan loaded its snapshot. Avoid an unnecessary HubCenter
		// lookup; flushCreditChargesDetailed remains the final idempotency guard.
		if reg, err := loadCachedLLMServiceRegistry(ctx, system); err == nil && llmservice.HasBillingRequest(reg, reservation.RequestID) {
			result.Settled++
			continue
		}
		result.Scanned++
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		attempt, status, err := ReconcileMaClawBillingAttempt(attemptCtx, store.TenantIDFromContext(ctx), reservation.RequestID)
		cancel()
		if err != nil {
			result.Failed++
			continue
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices || !validOfficialBillingAttempt(attempt) {
			result.Pending++
			continue
		}
		usage := corelib.TokenUsageStat{InputTokens: attempt.PricingSnapshot.InputTokens, OutputTokens: attempt.PricingSnapshot.OutputTokens, TotalTokens: attempt.PricingSnapshot.InputTokens + attempt.PricingSnapshot.OutputTokens, Requests: 1}
		credits := llmservice.EstimateTokenPricingCredits(usage.InputTokens, usage.OutputTokens, attempt.PricingSnapshot.Pricing, reservation.BillingGroupMultiplier)
		pricing := attempt.PricingSnapshot.Pricing
		charge := &pendingCreditCharge{userID: reservation.UserID, email: reservation.Email, serviceGroupIDs: reservation.ServiceGroupIDs, credits: credits, requestID: reservation.RequestID, providerID: llmservice.MaClawOfficialProviderID, usage: usage, multiplier: reservation.BillingGroupMultiplier, pricing: &pricing}
		if settled, err := flushCreditChargesDetailed(ctx, system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge}); err != nil {
			result.Failed++
		} else if settled[creditChargeKey(charge)] {
			result.Settled++
		} else {
			// A concurrent handler may already have completed the same request.
			// The ledger request-id guard makes this a successful idempotent scan.
			result.Settled++
		}
	}
	return result
}

// validOfficialBillingAttempt is deliberately conservative: reconciliation is
// a recovery path after Hub lost the original response, so an incomplete or
// malformed remote fact must leave the sent reservation intact for a later
// retry instead of guessing a charge or releasing it. The provider response is
// authenticated by HubCenter, but these checks also defend against rolling
// deployments and corrupted in-memory state.
func validOfficialBillingAttempt(attempt llmservice.OfficialBillingAttempt) bool {
	snapshot := attempt.PricingSnapshot
	if attempt.StatusCode < http.StatusOK || attempt.StatusCode >= http.StatusBadRequest {
		return false
	}
	return validOfficialTokenPricingSnapshot(snapshot)
}

// validOfficialTokenPricingSnapshot is shared by the online response path and
// delayed reconciliation. A HubCenter snapshot is authenticated, but it still
// crosses a network/version boundary: malformed counts must not enter usage
// logs or be silently normalized into a different debit.
func validOfficialTokenPricingSnapshot(snapshot llmpool.TokenPricingSnapshot) bool {
	if strings.TrimSpace(snapshot.ProviderID) == "" || snapshot.InputTokens < 0 || snapshot.OutputTokens < 0 {
		return false
	}
	// Avoid an int64 overflow when constructing TotalTokens for the ledger.
	if snapshot.InputTokens > math.MaxInt64-snapshot.OutputTokens {
		return false
	}
	return llmpool.ValidateResolvedTokenPricing(snapshot.Pricing)
}

// reconcilePendingOfficialBillingReservations is the request-path form of
// recovery. It scopes the scan to the current user to avoid unnecessary
// HubCenter calls on every LLM request.
func reconcilePendingOfficialBillingReservations(ctx context.Context, system store.SystemSettingsRepository, userID, email string) {
	if system == nil {
		return
	}
	// Older HubCenter versions do not expose reconciliation. Avoid probing on
	// every request unless a prior sent official reservation actually exists.
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		return
	}
	_ = reconcileOfficialBillingReservations(ctx, system, llmservice.SentBillingReservationsForUserID(reg, userID, email))
}

// ReconcileSentOfficialBillingReservations performs tenant-scoped background
// recovery. It ensures a user who never sends another request is still charged
// (or released for a confirmed zero-cost attempt) after a lost Hub response.
func ReconcileSentOfficialBillingReservations(ctx context.Context, system store.SystemSettingsRepository) (OfficialBillingReconciliationResult, error) {
	if system == nil {
		return OfficialBillingReconciliationResult{}, nil
	}
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		return OfficialBillingReconciliationResult{}, err
	}
	return reconcileOfficialBillingReservations(ctx, system, llmservice.SentBillingReservations(reg)), nil
}

func estimateLLMQuoteInputTokens(body map[string]any) int64 {
	if len(body) == 0 {
		return 0
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return 0
	}
	return int64(corelib.EstimateTextTokens(string(payload)))
}

func llmQuoteOutputTokenLimit(body map[string]any) int64 {
	for _, key := range []string{"max_completion_tokens", "max_tokens", "max_output_tokens"} {
		if value, ok := llmQuotePositiveInt64(body[key]); ok {
			return value
		}
	}
	return defaultLLMPricingQuoteOutputTokenLimit
}

func llmQuotePositiveInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return int64(v), true
		}
	case int64:
		if v > 0 {
			return v, true
		}
	case float64:
		if v > 0 && v == math.Trunc(v) && v <= float64(math.MaxInt64) {
			return int64(v), true
		}
	case json.Number:
		if v, err := v.Int64(); err == nil && v > 0 {
			return v, true
		}
	}
	return 0, false
}

func resolveBillableCreditMultiplier(ctx context.Context, model *llmservice.AuthorizedModel, providerID string, providerReg *im.LLMProviderRegistry) float64 {
	startedAt := time.Now()
	applied := 0.0
	officialProviderID := ""
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		if !state.started.IsZero() {
			startedAt = state.started
		}
		applied = state.applied
		officialProviderID = state.officialProviderID
		state.mu.Unlock()
	}
	var local *llmpool.ProviderBillingPolicy
	if providerReg != nil {
		if provider := providerReg.FindProvider(providerID); provider != nil {
			policy := provider.BillingPolicy()
			local = &policy
		}
	}
	var official []llmpool.ProviderBillingPolicy
	if ac := GetMaClawAccessControl(); ac != nil {
		official = ac.OfficialProviderBilling()
	}
	return llmservice.BillableCreditMultiplier(model, providerID, startedAt, local, official, applied, officialProviderID)
}

func computeLLMRequestBilling(ctx context.Context, model *llmservice.AuthorizedModel, providerID string, providerReg *im.LLMProviderRegistry, serviceReg *llmservice.Registry, serviceGroupIDs []string, usage corelib.TokenUsageStat, tokensPerCredit int) (credits, multiplier float64) {
	startedAt := time.Now()
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		if !state.started.IsZero() {
			startedAt = state.started
		}
		state.mu.Unlock()
	}
	// Explicitly free routes are terminal: they must not fall through to the
	// legacy CreditMultiplier calculation merely because they have no pricing
	// snapshot. This preserves a provider's intentional free-route setting.
	if llmservice.IsFreeBillingProviderRoute(model, providerID, billingUpstreamModel(model, providerID)) {
		return 0, 1
	}
	// New route pricing intentionally bypasses the legacy provider/model
	// CreditMultiplier. The only user-facing markup is the charged group's
	// BillingGroupMultiplier.
	// HubCenter returns the exact frozen snapshot used for the forwarded official
	// request. Prefer it over the admission quote because a compatibility retry
	// may legitimately need a new quote for a sanitized payload.
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil && IsMaClawProviderRequest(providerID) {
		multiplier = llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
		// Keep the customer-facing multiplier frozen at admission. The response
		// snapshot supplies actual provider price and usage, not a new group price.
		if quote, ok := snapshotLLMPricingQuote(ctx, providerID); ok && quote.BillingGroupMultiplier > 0 {
			multiplier = quote.BillingGroupMultiplier
		}
		credits = llmservice.EstimateTokenPricingCredits(snapshot.InputTokens, snapshot.OutputTokens, snapshot.Pricing, multiplier)
		return credits, multiplier
	}
	if quote, ok := snapshotLLMPricingQuote(ctx, providerID); ok {
		multiplier = quote.BillingGroupMultiplier
		credits = llmservice.EstimateTokenPricingCredits(usage.InputTokens, usage.OutputTokens, quote.Pricing, multiplier)
		return credits, multiplier
	}
	if pricing, ok := llmservice.ResolveTokenPricingForProviderRoute(model, providerID, billingUpstreamModel(model, providerID), startedAt); ok {
		multiplier = llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
		credits = llmservice.EstimateTokenPricingCredits(usage.InputTokens, usage.OutputTokens, pricing, multiplier)
		return credits, multiplier
	}
	multiplier = resolveBillableCreditMultiplier(ctx, model, providerID, providerReg)
	credits = llmservice.EstimateCreditsWithFloor(usage.TotalTokens, multiplier, tokensPerCredit)
	return credits, multiplier
}

func chargeLoggedLLMEndpointUsage(ctx context.Context, system store.SystemSettingsRepository, securitySvc *security.SecurityService, userID, email, providerID string, model *llmservice.AuthorizedModel, providerReg *im.LLMProviderRegistry, serviceReg *llmservice.Registry, usage corelib.TokenUsageStat, serviceGroupIDs []string) (credits, multiplier float64) {
	if strings.TrimSpace(providerID) == "" {
		return 0, 0
	}
	// HubCenter's authenticated snapshot is the authoritative final usage for
	// official routes. OpenAI-compatible bodies may omit `usage` entirely, so
	// carrying the body-parsed zero value into the ledger would make a correctly
	// charged request appear to have consumed zero Tokens in audits and reports.
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil && IsMaClawProviderRequest(providerID) {
		usage.InputTokens = snapshot.InputTokens
		usage.OutputTokens = snapshot.OutputTokens
		usage.TotalTokens = snapshot.InputTokens + snapshot.OutputTokens
		if usage.Requests <= 0 {
			usage.Requests = 1
		}
	}
	tokensPerCredit := 0
	if serviceReg != nil {
		tokensPerCredit = serviceReg.TokensPerCredit
	}
	credits, multiplier = computeLLMRequestBilling(ctx, model, providerID, providerReg, serviceReg, serviceGroupIDs, usage, tokensPerCredit)
	userGroupIDs := []string(nil)
	if securitySvc != nil {
		if resolved, resolveErr := securitySvc.ResolveUserGroupChain(ctx, email); resolveErr == nil {
			userGroupIDs = resolved
		}
	}
	meta := llmservice.OfficialForwardMetaFrom(ctx)
	pricingStartedAt := time.Now()
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		if !state.started.IsZero() {
			pricingStartedAt = state.started
		}
		state.mu.Unlock()
	}
	var pricing *llmpool.ResolvedTokenPricing
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil && IsMaClawProviderRequest(providerID) {
		copyPricing := snapshot.Pricing
		pricing = &copyPricing
	} else if quote, ok := snapshotLLMPricingQuote(ctx, providerID); ok {
		copyPricing := quote.Pricing
		pricing = &copyPricing
	} else if resolved, ok := llmservice.ResolveTokenPricingForProviderRoute(model, providerID, billingUpstreamModel(model, providerID), pricingStartedAt); ok {
		pricing = &resolved
	}
	markLLMBillingSettlementQueued(ctx)
	enqueueLLMUsageRecordWithBilling(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, meta, llmBillingRequestID(ctx), multiplier, pricing)
	recordLLMClassTraffic(system, serviceGroupIDs, meta, usage, meta.Preview)
	recordLLMClassHeadSample(system, serviceGroupIDs, meta)
	return credits, multiplier
}

// authoritativeLLMUsageForAccessLog applies the authenticated official
// snapshot to the handler's access-log copy as well as to the billing copy.
// The final credit ledger and the user-visible access log must describe the
// same input/output usage.
func authoritativeLLMUsageForAccessLog(ctx context.Context, providerID string, usage corelib.TokenUsageStat) corelib.TokenUsageStat {
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil && IsMaClawProviderRequest(providerID) {
		usage.InputTokens = snapshot.InputTokens
		usage.OutputTokens = snapshot.OutputTokens
		usage.TotalTokens = snapshot.InputTokens + snapshot.OutputTokens
		if usage.Requests <= 0 {
			usage.Requests = 1
		}
	}
	return usage
}
