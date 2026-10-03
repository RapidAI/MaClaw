package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	// quotes freezes each logical model's provider price at admission time.
	// The key is the model plus the provider: several models can share one
	// provider and still keep distinct ceilings. An operator changing a
	// time-of-day price while an upstream call is in flight cannot change
	// that request's debit.
	quotes          map[string]llmpool.PricingQuoteSnapshot
	heldQuote       llmpool.PricingQuoteSnapshot
	heldQuoteSet    bool
	reservationHeld bool
	// officialAdmissionGroupFactor is the service-group multiplier without the
	// capability band. A fallback tier is quoted again, and its band coefficient
	// has to be combined with this same group factor so the new ceiling is priced
	// the way settlement will price it. The registry is not on the forward path.
	officialAdmissionGroupFactor    float64
	officialAdmissionGroupFactorSet bool
	upstreamSent                    bool
	settlementQueued                bool
	noUpstreamDispatch              bool
	// dispatchObserved sticks once any attempt was sent or its transport result
	// was ambiguous. A later local refusal must not prove the whole request
	// never left Hub.
	dispatchObserved bool
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

// noteOfficialDispatchObserved records that one attempt left Hub, or that its
// transport result can no longer prove it did not. The mark sticks for the
// rest of the request, including availability fallbacks, and it withdraws any
// earlier local no-dispatch proof.
func noteOfficialDispatchObserved(ctx context.Context) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	state.dispatchObserved = true
	state.noUpstreamDispatch = false
	state.mu.Unlock()
}

// noteOfficialNoUpstreamDispatch records a narrow, local proof that Hub never
// sent the request to HubCenter. It must never be set for HTTP or transport
// failures, which remain ambiguous after dispatch begins. One fallback tier
// refused before forward is not that proof: an earlier attempt may already
// have been sent, and a later tier may still be sent.
func noteOfficialNoUpstreamDispatch(ctx context.Context, noUpstreamDispatch bool) {
	if !noUpstreamDispatch {
		return
	}
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		if !state.dispatchObserved {
			state.noUpstreamDispatch = true
		}
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

func llmPricingQuoteKey(logicalModel, providerID string) string {
	return strings.ToLower(strings.TrimSpace(logicalModel)) + "\x00" + strings.ToLower(strings.TrimSpace(providerID))
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
	state.quotes[llmPricingQuoteKey(quote.LogicalModel, quote.ProviderID)] = quote
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
	// HubCenter resolves and freezes the provider/time-of-use multiplier; Hub
	// owns the service-group multiplier. Both must be included in the admission
	// reservation so a request cannot pass preflight at an understated price.
	providerMultiplier := llmpool.NormalizeCreditMultiplier(quote.ProviderMultiplier)
	groupMultiplier := officialGroupMultiplier(ctx, serviceReg, serviceGroupIDs, quote.CapabilityMultiplier)
	_ = startedAt // retained as the pricing-start ownership boundary for callers.
	frozen, ok := llmpool.NewPricingQuoteSnapshot(requestID, requestID+":"+llmservice.MaClawOfficialProviderID, llmservice.MaClawOfficialProviderID, quote.Pricing, providerMultiplier, groupMultiplier, inputEstimate, outputLimit, quote.ExpiresAt)
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
	frozen.PricingSource = strings.TrimSpace(quote.PricingSource)
	if frozen.PricingSource == "" {
		frozen.PricingSource = llmpool.PricingSourceProvider
	}
	if frozen.PricingSource != llmpool.PricingSourceProvider && frozen.PricingSource != llmpool.PricingSourceServiceGroupOverride {
		return fmt.Errorf("official pricing quote has invalid pricing source")
	}
	frozen.ServiceGroupIDs = append([]string(nil), serviceGroupIDs...)
	rememberLLMPricingQuote(ctx, frozen)
	// Keep the group factor apart from the capability band. Fallback requotes
	// combine it with the band HubCenter returns for the tier that actually runs.
	groupFactor := llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
	state.mu.Lock()
	copyQuote := quote
	state.officialQuote = &copyQuote
	state.officialAdmissionGroupFactor = groupFactor
	state.officialAdmissionGroupFactorSet = true
	state.mu.Unlock()
	return nil
}

// prepareOfficialLLMRequestPricingQuote locks HubCenter's concrete provider and
// time-of-use base price before Hub reserves the user's Credits. If an older
// HubCenter does not support quotes, forwarding retains its compatibility path.
// A ceiling the balance cannot cover is written onto the forwarded body before
// the token Hub will attach: HubCenter binds that token to the exact JSON bytes.
func prepareOfficialLLMRequestPricingQuote(ctx context.Context, serviceReg *llmservice.Registry, providerReg *im.LLMProviderRegistry, model *llmservice.AuthorizedModel, body map[string]any, userID, email string) error {
	if model == nil || providerReg == nil {
		return nil
	}
	for _, item := range orderAuthorizedProviders(body, model, providerReg) {
		providerID := strings.TrimSpace(item.Route.ProviderID)
		if !IsMaClawProviderRequest(providerID) {
			continue
		}
		if llmservice.IsFreeBillingProviderRoute(model, providerID, billingUpstreamModel(model, providerID)) {
			continue
		}
		// Official input/cache/output prices come from HubCenter. Quote even
		// when Hub has no local directional price.
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
		groups := llmservice.ChargedServiceGroupIDs(model, providerID)
		inputEstimate := estimateLLMQuoteInputTokens(forwardBody)
		outputLimit := llmQuoteOutputTokenLimit(forwardBody)
		if strings.TrimSpace(userID) != "" || strings.TrimSpace(email) != "" {
			allowed, _, _, _, available, _, _ := llmservice.BillingEligibilityForServiceGroupsForUserID(serviceReg, userID, email, groups, time.Now().UTC())
			if allowed && available > 0 {
				quote, inputEstimate, outputLimit = fitOfficialForwardToBalance(ctx, serviceReg, groups, forwardGroupIDs, available, quote, payload, inputEstimate, outputLimit, forwardBody, nil, body, forwardBody)
			}
		}
		if err := rememberOfficialPricingQuote(ctx, serviceReg, model, quote, groups, inputEstimate, outputLimit); err != nil {
			return err
		}
		alignLLMPricingQuotesToForwardedCeiling(ctx, body, model.Name)
		return nil
	}
	return nil
}

// provisionalOfficialAdmissionQuote is the reservation Hub would freeze for
// this HubCenter quote at the given token counts. It uses the same multiplier
// split as rememberOfficialPricingQuote.
func provisionalOfficialAdmissionQuote(ctx context.Context, serviceReg *llmservice.Registry, groups []string, quote llmservice.OfficialPricingQuote, inputEstimate, outputLimit int64, billingGroupMultiplier float64) (llmpool.PricingQuoteSnapshot, bool) {
	groupMultiplier := billingGroupMultiplier
	if groupMultiplier <= 0 {
		groupMultiplier = officialGroupMultiplier(ctx, serviceReg, groups, quote.CapabilityMultiplier)
	}
	requestID := llmBillingRequestID(ctx)
	return llmpool.NewPricingQuoteSnapshot(
		requestID,
		requestID+":"+llmservice.MaClawOfficialProviderID,
		llmservice.MaClawOfficialProviderID,
		quote.Pricing,
		llmpool.NormalizeCreditMultiplier(quote.ProviderMultiplier),
		groupMultiplier,
		inputEstimate,
		outputLimit,
		quote.ExpiresAt,
	)
}

// fitOfficialForwardToBalance lowers the output ceiling when the quoted worst
// case does not fit in availableCredits, then asks HubCenter to price that
// exact body. The token from the larger body is not returned: HubCenter
// rejects it once the JSON changes. The ceiling is refit from the caller's
// original fields whenever the new quote's unit price differs, so a cheaper
// member can raise the ceiling and a more expensive one can lower it. A
// failed re-quote keeps the last token that still covers its body, or
// restores the original ceiling when no fitted token does. estimateBody is
// the rewritten body that will be marshaled again at forward time, and it
// must be one of bodies so the ceiling lands on those bytes.
func fitOfficialForwardToBalance(ctx context.Context, serviceReg *llmservice.Registry, chargedGroups, forwardGroupIDs []string, availableCredits float64, quote llmservice.OfficialPricingQuote, quotedPayload []byte, inputEstimate, outputLimit int64, estimateBody map[string]any, billingGroup func(llmservice.OfficialPricingQuote) float64, bodies ...map[string]any) (llmservice.OfficialPricingQuote, int64, int64) {
	groupOf := func(q llmservice.OfficialPricingQuote) float64 {
		if billingGroup == nil {
			return 0
		}
		return billingGroup(q)
	}
	provisional, ok := provisionalOfficialAdmissionQuote(ctx, serviceReg, chargedGroups, quote, inputEstimate, outputLimit, groupOf(quote))
	if !ok || provisional.ReservedMicrocredits <= creditsToMicrocredits(availableCredits) {
		return quote, inputEstimate, outputLimit
	}
	availableMicro := creditsToMicrocredits(availableCredits)
	saved := make([]map[string]any, len(bodies))
	for i, body := range bodies {
		saved[i] = snapshotOutputLimitFields(body)
	}
	restore := func() {
		for i, body := range bodies {
			restoreOutputLimitFields(body, saved[i])
		}
	}
	applyCeiling := func(ceiling int64) {
		for _, body := range bodies {
			applyLLMQuoteOutputCeiling(body, ceiling)
		}
	}
	working := provisional
	matchedQuote := quote
	matchedPayload := quotedPayload
	var bestQuote llmservice.OfficialPricingQuote
	var bestInput, bestLimit int64
	haveBest := false
	keepBest := func() (llmservice.OfficialPricingQuote, int64, int64) {
		if !haveBest {
			restore()
			return quote, inputEstimate, outputLimit
		}
		restore()
		applyCeiling(bestLimit)
		return bestQuote, bestInput, bestLimit
	}
	// Two passes cover one price change: fit at the quote in hand, then fit
	// again at the price HubCenter returns for that body. Each pass starts
	// from the caller's fields, because a ceiling written for a more expensive
	// quote must be allowed to rise when the next quote is cheaper.
	for attempt := 0; attempt < 2; attempt++ {
		restore()
		// working may already describe a ceiling fitted at an older price.
		// Searching from that ceiling treats a quote that fits as done, so a
		// cheaper rate can never buy the caller's original output back.
		// Rebase onto the caller's token counts before fitting.
		if rebased, rebaseOK := requoteAtOutputLimit(working, inputEstimate, outputLimit); rebaseOK {
			working = rebased
		}
		fitted, fitOK := fitLLMQuoteToAvailableBalance(working, availableCredits, estimateBody, bodies...)
		if !fitOK {
			return keepBest()
		}
		payload, err := json.Marshal(estimateBody)
		if err != nil {
			log.Printf("[llm-billing] official output-ceiling requote failed; restoring the quoted body: %v", err)
			return keepBest()
		}
		if bytes.Equal(payload, matchedPayload) {
			covered, coverOK := provisionalOfficialAdmissionQuote(ctx, serviceReg, chargedGroups, matchedQuote, fitted.InputTokenEstimate, fitted.OutputTokenLimit, groupOf(matchedQuote))
			if coverOK && covered.ReservedMicrocredits <= availableMicro {
				return matchedQuote, fitted.InputTokenEstimate, fitted.OutputTokenLimit
			}
			return keepBest()
		}
		next, err := QuoteViaMaClaw(ctx, payload, store.TenantIDFromContext(ctx), forwardGroupIDs...)
		if err != nil {
			log.Printf("[llm-billing] official output-ceiling requote failed; restoring the quoted body: %v", err)
			return keepBest()
		}
		nextSnap, snapOK := provisionalOfficialAdmissionQuote(ctx, serviceReg, chargedGroups, next, fitted.InputTokenEstimate, fitted.OutputTokenLimit, groupOf(next))
		if !snapOK {
			log.Printf("[llm-billing] official output-ceiling requote could not be priced; restoring the quoted body")
			return keepBest()
		}
		if nextSnap.ReservedMicrocredits <= availableMicro {
			bestQuote = next
			bestInput = fitted.InputTokenEstimate
			bestLimit = fitted.OutputTokenLimit
			haveBest = true
			if sameOfficialAdmissionPrice(working, nextSnap) {
				return next, fitted.InputTokenEstimate, fitted.OutputTokenLimit
			}
		}
		working = nextSnap
		matchedQuote = next
		matchedPayload = payload
	}
	if !haveBest {
		log.Printf("[llm-billing] official output ceiling still exceeds the balance after requote; restoring the quoted body")
	}
	return keepBest()
}

// sameOfficialAdmissionPrice reports whether two frozen quotes reserve Credits
// at the same unit rates. The output ceiling fitted against one quote is not
// the maximum the balance can pay once the other quote's rate differs.
func sameOfficialAdmissionPrice(left, right llmpool.PricingQuoteSnapshot) bool {
	if left.ProviderMultiplier != right.ProviderMultiplier || left.BillingGroupMultiplier != right.BillingGroupMultiplier {
		return false
	}
	a := left.Pricing.TokenPricing
	b := right.Pricing.TokenPricing
	return a.InputCreditsPer10K == b.InputCreditsPer10K &&
		a.OutputCreditsPer10K == b.OutputCreditsPer10K &&
		a.MinimumRequestCredits == b.MinimumRequestCredits &&
		sameOptionalTokenPrice(a.CacheReadCreditsPer10K, b.CacheReadCreditsPer10K) &&
		sameOptionalTokenPrice(a.CacheWriteCreditsPer10K, b.CacheWriteCreditsPer10K)
}

func sameOptionalTokenPrice(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// officialAdmissionHold is the credit amount already reserved for this request
// and the service-group factor frozen with it. availableGrantCredits subtracts
// that hold, so a fallback tier cannot fit against the leftover balance: the
// hold is the budget this request may still spend.
func officialAdmissionHold(ctx context.Context) (heldMicro int64, groupFactor, admissionCombined float64, ok bool) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return 0, 0, 0, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.reservationHeld || !state.heldQuoteSet || state.heldQuote.ReservedMicrocredits <= 0 || !state.officialAdmissionGroupFactorSet {
		return 0, 0, 0, false
	}
	return state.heldQuote.ReservedMicrocredits, state.officialAdmissionGroupFactor, state.heldQuote.BillingGroupMultiplier, true
}

// fallbackBillingGroupMultiplier prices a fallback quote with the admission
// group factor and the band coefficient on that quote. A quote that omits the
// band keeps the multiplier the hold already used.
func fallbackBillingGroupMultiplier(groupFactor, admissionCombined float64, quote llmservice.OfficialPricingQuote) float64 {
	capability := quote.CapabilityMultiplier
	if capability <= 0 || math.IsNaN(capability) || math.IsInf(capability, 0) {
		if admissionCombined > 0 {
			return admissionCombined
		}
		return groupFactor
	}
	return llmpool.CombineCreditMultipliers(groupFactor, capability)
}

// refitOfficialFallbackBodyToHold lowers a fallback body's output ceiling until
// the quote HubCenter just gave fits inside the credits already held. The
// selected tier's ceiling is not a safe ceiling for a more expensive fallback:
// mid admits the largest output its own price can pay, then availability
// fallback can run high at that same ceiling. No hold returns the quote
// unchanged so an unlimited grant still forwards. An unaffordable tier returns
// an error and leaves the body at the caller's ceiling; the caller must not
// forward it.
func refitOfficialFallbackBodyToHold(ctx context.Context, body map[string]any, tenantID string, forwardGroupIDs []string) (llmservice.OfficialPricingQuote, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return llmservice.OfficialPricingQuote{}, fmt.Errorf("marshal official fallback quote: %w", err)
	}
	quote, err := QuoteViaMaClaw(ctx, payload, tenantID, forwardGroupIDs...)
	if err != nil {
		return llmservice.OfficialPricingQuote{}, err
	}
	heldMicro, groupFactor, admissionCombined, ok := officialAdmissionHold(ctx)
	if !ok {
		return quote, nil
	}
	groupFor := func(q llmservice.OfficialPricingQuote) float64 {
		return fallbackBillingGroupMultiplier(groupFactor, admissionCombined, q)
	}
	inputEstimate := estimateLLMQuoteInputTokens(body)
	outputLimit := llmQuoteOutputTokenLimit(body)
	fitted, fittedInput, fittedOutput := fitOfficialForwardToBalance(ctx, nil, nil, forwardGroupIDs, llmpool.MicrocreditsToCredits(heldMicro), quote, payload, inputEstimate, outputLimit, body, groupFor, body)
	check, checkOK := provisionalOfficialAdmissionQuote(ctx, nil, nil, fitted, fittedInput, fittedOutput, groupFor(fitted))
	if !checkOK || check.ReservedMicrocredits > heldMicro {
		log.Printf("[llm-billing] official fallback output ceiling exceeds the admission hold; not forwarding this tier")
		return llmservice.OfficialPricingQuote{}, fmt.Errorf("official fallback output ceiling exceeds the admission hold")
	}
	return fitted, nil
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

func officialAdmissionQuoteApplies(ctx context.Context) bool {
	resolved := strings.TrimSpace(llmservice.OfficialForwardMetaFrom(ctx).ResolvedModel)
	if resolved == "" {
		return true
	}
	local, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok {
		return true
	}
	return llmPricingQuoteAppliesToModel(local, &llmservice.AuthorizedModel{Name: resolved})
}

func rememberOfficialForwardQuoteForResolved(ctx context.Context, quote llmservice.OfficialPricingQuote) {
	if strings.TrimSpace(quote.ProviderID) == "" {
		return
	}
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return
	}
	resolved := strings.TrimSpace(llmservice.OfficialForwardMetaFrom(ctx).ResolvedModel)
	state.mu.Lock()
	defer state.mu.Unlock()
	copyQuote := quote
	state.officialQuote = &copyQuote
	if state.quotes == nil {
		state.quotes = map[string]llmpool.PricingQuoteSnapshot{}
	}
	// One official admission quote can be retargeted when fallback resolves a
	// different tier. Several official quotes must not be collapsed onto the
	// resolved tier: that would bill one model at another's frozen price.
	key, snap, ok := officialPricingSnapshotLocked(state, resolved)
	if !ok {
		key = llmPricingQuoteKey(resolved, llmservice.MaClawOfficialProviderID)
	}
	if resolved != "" {
		snap.LogicalModel = resolved
	}
	if strings.TrimSpace(snap.ProviderID) == "" {
		snap.ProviderID = llmservice.MaClawOfficialProviderID
	}
	snap.UpstreamModel = strings.TrimSpace(quote.UpstreamModel)
	snap.Pricing = quote.Pricing
	if quote.ProviderMultiplier > 0 {
		snap.ProviderMultiplier = llmpool.NormalizeCreditMultiplier(quote.ProviderMultiplier)
	}
	if src := strings.TrimSpace(quote.PricingSource); src != "" {
		snap.PricingSource = src
	}
	newKey := llmPricingQuoteKey(snap.LogicalModel, snap.ProviderID)
	if ok && key != newKey {
		delete(state.quotes, key)
	}
	state.quotes[newKey] = snap
}

func officialPricingSnapshotLocked(state *llmBillingState, resolved string) (string, llmpool.PricingQuoteSnapshot, bool) {
	var keys []string
	for key, quote := range state.quotes {
		if strings.EqualFold(strings.TrimSpace(quote.ProviderID), llmservice.MaClawOfficialProviderID) {
			keys = append(keys, key)
		}
	}
	if resolved != "" {
		for _, key := range keys {
			if strings.EqualFold(strings.TrimSpace(state.quotes[key].LogicalModel), resolved) {
				return key, state.quotes[key], true
			}
		}
	}
	if len(keys) == 1 {
		return keys[0], state.quotes[keys[0]], true
	}
	return "", llmpool.PricingQuoteSnapshot{}, false
}

func llmPricingQuoteAppliesToModel(quote llmpool.PricingQuoteSnapshot, model *llmservice.AuthorizedModel) bool {
	quoted := strings.TrimSpace(quote.LogicalModel)
	if quoted == "" {
		return true
	}
	if model == nil || strings.TrimSpace(model.Name) == "" {
		return true
	}
	return strings.EqualFold(quoted, strings.TrimSpace(model.Name))
}

func snapshotLLMPricingQuote(ctx context.Context, providerID string) (llmpool.PricingQuoteSnapshot, bool) {
	return snapshotLLMPricingQuoteForModel(ctx, providerID, nil)
}

// snapshotLLMPricingQuoteForModel returns the admission quote for one provider
// on one logical model. A provider shared by several models has several
// quotes; looking up the provider alone must not price one model with another
// model's frozen rate. A provider with exactly one quote still resolves
// without a model, which is the historical lookup.
func snapshotLLMPricingQuoteForModel(ctx context.Context, providerID string, model *llmservice.AuthorizedModel) (llmpool.PricingQuoteSnapshot, bool) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return llmpool.PricingQuoteSnapshot{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	matches := pricingQuotesForProviderLocked(state.quotes, providerID)
	if model != nil && strings.TrimSpace(model.Name) != "" {
		fitted := make([]llmpool.PricingQuoteSnapshot, 0, len(matches))
		for _, quote := range matches {
			if llmPricingQuoteAppliesToModel(quote, model) {
				fitted = append(fitted, quote)
			}
		}
		matches = fitted
	}
	if len(matches) != 1 {
		return llmpool.PricingQuoteSnapshot{}, false
	}
	return matches[0], true
}

func pricingQuotesForProviderLocked(quotes map[string]llmpool.PricingQuoteSnapshot, providerID string) []llmpool.PricingQuoteSnapshot {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if providerID == "" || len(quotes) == 0 {
		return nil
	}
	matches := make([]llmpool.PricingQuoteSnapshot, 0, 1)
	for _, quote := range quotes {
		if strings.ToLower(strings.TrimSpace(quote.ProviderID)) == providerID {
			matches = append(matches, quote)
		}
	}
	return matches
}

// prepareLLMPricingQuote freezes a provider/model price before any upstream
// bytes are sent. A quoted paid route is admitted when the current metered
// balance covers the prompt plus at least one output token. A requested
// output ceiling the balance cannot cover is lowered, on the quote and on
// the forwarded body together, so work continues until the balance is gone.
// Unlimited grants still bypass a balance reservation, as they have no
// finite wallet value to compare here.
func prepareLLMPricingQuote(ctx context.Context, reg *llmservice.Registry, providerReg *im.LLMProviderRegistry, userID, email string, model *llmservice.AuthorizedModel, providerID string, body map[string]any, now time.Time) (llmBillingDenial, error) {
	groups := llmservice.ChargedServiceGroupIDs(model, providerID)
	allowed, _, code, message, available, _, _ := llmservice.BillingEligibilityForServiceGroupsForUserID(reg, userID, email, groups, now)
	if !allowed {
		return llmBillingDenial{Code: code, Message: message}, fmt.Errorf("%s", message)
	}
	if llmservice.IsFreeBillingProviderRoute(model, providerID, billingUpstreamModel(model, providerID)) {
		return llmBillingDenial{}, nil
	}
	if IsMaClawProviderRequest(providerID) {
		// Official input/cache/output prices belong to the HubCenter provider.
		// HubCenter quote/snapshot is the admission and settlement source.
		return llmBillingDenial{}, nil
	}
	// Quote the same concrete upstream route that settlement will use. A
	// provider can serve several routes with different prices, so the
	// provider-only compatibility projection is not safe for admission.
	pricing, ok := llmservice.ResolveTokenPricingForProviderRoute(model, providerID, billingUpstreamModel(model, providerID), now)
	// The quote's pricing source must name the branch that actually supplied
	// the price, not a re-evaluation of the configuration: an override flag
	// without a usable price falls through to the provider price.
	pricingSource := llmservice.PricingSourceForProviderRoute(model, providerID, billingUpstreamModel(model, providerID))
	if !ok {
		// A local third-party provider can own its directional price directly in
		// the provider registry (no service-group route override). Settlement
		// honors that price, so admission must reserve against it too; otherwise
		// a finite balance could be silently overdrawn at settlement.
		pricing, ok = resolveLocalProviderTokenPricing(providerReg, providerID, now)
		pricingSource = llmpool.PricingSourceProvider
	}
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
	quote, ok := llmpool.NewPricingQuoteSnapshot(requestID, requestID+":"+strings.TrimSpace(providerID), providerID, pricing, 1, multiplier, inputEstimate, outputLimit, now.Add(15*time.Minute))
	if !ok {
		return llmBillingDenial{Code: "LLM_PRICING_QUOTE_INVALID", Message: "unable to calculate the configured token price"}, fmt.Errorf("unable to calculate the configured token price")
	}
	quote.ServiceGroupIDs = append([]string(nil), groups...)
	quote.LogicalModel = strings.TrimSpace(model.Name)
	quote.PricingSource = pricingSource
	// A zero available amount represents an unlimited entitlement in the
	// existing registry model. Only finite positive wallet balances participate
	// in the preflight check.
	if available > 0 && quote.ReservedMicrocredits > creditsToMicrocredits(available) {
		fitted, ok := fitLLMQuoteToAvailableBalance(quote, available, body, body)
		if !ok {
			floor := quote
			if one, oneOK := requoteAtOutputLimit(quote, quote.InputTokenEstimate, 1); oneOK {
				floor = one
			}
			held := insufficientCreditsHeld(reg, userID, email, groups, now)
			need := llmpool.MicrocreditsToCredits(floor.ReservedMicrocredits)
			return llmBillingDenial{
				Code:        "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST",
				Message:     insufficientCreditsMessage(need, available, held),
				HeldCredits: held,
				NeedCredits: need,
			}, fmt.Errorf("insufficient credits for quoted request")
		}
		quote = fitted
	}
	if llmBillingRequestID(ctx) != "" {
		rememberLLMPricingQuote(ctx, quote)
	}
	return llmBillingDenial{}, nil
}

// insufficientCreditsMessage builds the admission denial message. When
// in-flight reservations hold part of the balance, the held amount is
// included so the denial explains why available sits below the visible
// period window remaining.
func insufficientCreditsMessage(need, available, held float64) string {
	message := fmt.Sprintf("insufficient credits for this request: need %.3f credits, available %.3f", need, available)
	if held > 0 {
		message = fmt.Sprintf("%s (%.3f held by in-flight requests)", message, held)
	}
	return message
}

// tokenBankAdmissionPullLimit bounds how many automatic 1/N shares one
// request may withdraw. The first share often covers the shortfall. Further
// shares stop once the card can start the call, the bank is empty, or the
// balance does not rise.
const tokenBankAdmissionPullLimit = 4

// tokenBankAdmissionPuller withdraws one automatic share into a charged group.
// *center.Service implements it. A nil puller leaves admission unchanged.
// callerSpendMicro is the charged card the caller already judged too small.
type tokenBankAdmissionPuller interface {
	PullTokenBankForAdmissionShortfall(ctx context.Context, userID, email string, chargedGroupIDs []string, callerSpendMicro int64) (bool, error)
}

func tokenBankPullerFrom(sources []any) tokenBankAdmissionPuller {
	for _, source := range sources {
		puller, ok := source.(tokenBankAdmissionPuller)
		if ok && puller != nil {
			return puller
		}
	}
	return nil
}

// prepareAndReserveLLMPricing freezes the official quote and holds credits.
// When the card cannot pay for the prompt plus one output token, and the
// token bank can still fund a group this request charges, it withdraws
// automatic shares and admits again. Caller output fields are restored on
// every attempt so a ceiling fitted to the old balance cannot stick.
func prepareAndReserveLLMPricing(ctx context.Context, system store.SystemSettingsRepository, providerReg *im.LLMProviderRegistry, serviceReg *llmservice.Registry, model *llmservice.AuthorizedModel, body map[string]any, userID, email string, puller tokenBankAdmissionPuller) (*llmservice.Registry, llmBillingDenial, error) {
	callerCeiling := snapshotOutputLimitFields(body)
	var lastDenial llmBillingDenial
	var lastErr error
	// The handler's registry can still show a balance reserve no longer sees.
	// Reprice the official quote against the loaded wallet once before any
	// withdraw; a later share still resets that quote.
	repriced := false
	for pulls := 0; pulls <= tokenBankAdmissionPullLimit; {
		restoreOutputLimitFields(body, callerCeiling)
		if repriced || pulls > 0 {
			resetOfficialAdmissionQuote(ctx)
		}
		applySelectedModelOutputCeiling(ctx, body, model)
		if err := prepareOfficialLLMRequestPricingQuote(ctx, serviceReg, providerReg, model, body, userID, email); err != nil {
			return serviceReg, llmBillingDenial{}, err
		}
		denial, err := reserveLLMRequestPricing(ctx, system, userID, email, model)
		if err == nil {
			return serviceReg, llmBillingDenial{}, nil
		}
		lastDenial, lastErr = denial, err
		if !llmDenialCodeIs(denial.Code, "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST") {
			break
		}
		groups := admissionChargedGroups(ctx, model)
		beforeSpend := grossSpendableMicro(serviceReg, userID, email, groups)
		beforeAvailable := admissionAvailableMicro(serviceReg, userID, email, groups)
		if system != nil {
			if fresh, loadErr := loadCachedLLMServiceRegistry(ctx, system); loadErr == nil && fresh != nil {
				serviceReg = fresh
			}
		}
		walletMoved := grossSpendableMicro(serviceReg, userID, email, groups) != beforeSpend || admissionAvailableMicro(serviceReg, userID, email, groups) != beforeAvailable
		if !repriced && walletMoved {
			repriced = true
			continue
		}
		if puller == nil || pulls == tokenBankAdmissionPullLimit {
			break
		}
		before := grossSpendableMicro(serviceReg, userID, email, groups)
		pulled, pullErr := pullTokenBankForQuoteShortfall(ctx, serviceReg, userID, email, denial.Code, denial.NeedCredits, groups, model, puller)
		if pullErr != nil {
			log.Printf("[llm-billing] token bank auto withdraw for admission: %v", pullErr)
		}
		if !pulled {
			break
		}
		invalidateLLMRuntimeCaches(system)
		if fresh, loadErr := loadCachedLLMServiceRegistry(ctx, system); loadErr == nil && fresh != nil {
			serviceReg = fresh
		}
		if grossSpendableMicro(serviceReg, userID, email, groups) <= before {
			break
		}
		repriced = true
		pulls++
	}
	return serviceReg, lastDenial, lastErr
}

// coverFilteredModelsFromTokenBank pulls when catalog admission rejected every
// model because the charged card cannot pay, then filters again. Each denied
// model is judged on its own groups, and providers that bill different groups
// are judged apart: a rich sibling must not hide a short card. A group the
// bank does not pay does not stop a later wallet the bank does pay. One
// successful share is applied before the next filter. Official routes pass
// this filter and settle the same shortfall in prepareAndReserve.
func coverFilteredModelsFromTokenBank(ctx context.Context, system store.SystemSettingsRepository, puller tokenBankAdmissionPuller, userID, email string, body map[string]any, providerReg *im.LLMProviderRegistry, models []llmservice.AuthorizedModel, serviceReg *llmservice.Registry, billable []llmservice.AuthorizedModel, denied map[string]llmBillingDenial, first llmBillingDenial) ([]llmservice.AuthorizedModel, map[string]llmBillingDenial, llmBillingDenial, *llmservice.Registry) {
	if puller == nil || len(billable) > 0 || serviceReg == nil {
		return billable, denied, first, serviceReg
	}
	for attempt := 0; attempt < tokenBankAdmissionPullLimit; attempt++ {
		if len(billable) > 0 {
			break
		}
		advanced := false
		for i := range models {
			key := strings.ToLower(strings.TrimSpace(models[i].Name))
			denial, ok := denied[key]
			if !ok || !creditShortfallDenial(denial.Code) {
				continue
			}
			for _, groups := range chargedGroupSetsForModel(&models[i]) {
				if !quoteShortfallNeedsPull(ctx, serviceReg, userID, email, denial.Code, denial.NeedCredits, groups, &models[i]) {
					continue
				}
				before := grossSpendableMicro(serviceReg, userID, email, groups)
				pulled, pullErr := pullTokenBankForQuoteShortfall(ctx, serviceReg, userID, email, denial.Code, denial.NeedCredits, groups, &models[i], puller)
				if errors.Is(pullErr, llmservice.ErrTokenBankGroupNotCharged) {
					continue
				}
				if pullErr != nil {
					log.Printf("[llm-billing] token bank auto withdraw for admission: %v", pullErr)
				}
				if !pulled {
					return billable, denied, first, serviceReg
				}
				invalidateLLMRuntimeCaches(system)
				if fresh, loadErr := loadCachedLLMServiceRegistry(ctx, system); loadErr == nil && fresh != nil {
					serviceReg = fresh
				}
				if grossSpendableMicro(serviceReg, userID, email, groups) <= before {
					return billable, denied, first, serviceReg
				}
				advanced = true
				break
			}
			if advanced {
				break
			}
		}
		if !advanced {
			break
		}
		billable, denied, first = filterAuthorizedModelsByBillingEligibility(ctx, serviceReg, providerReg, userID, email, body, models)
	}
	return billable, denied, first, serviceReg
}

func creditShortfallDenial(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", "LLM_SERVICE_CREDITS_EXHAUSTED", "LLM_SERVICE_CREDITS_REQUIRED":
		return true
	default:
		return false
	}
}

func llmDenialCodeIs(code, want string) bool {
	return strings.EqualFold(strings.TrimSpace(code), want)
}

// pullTokenBankForQuoteShortfall withdraws when the charged card cannot start
// the request. An unlimited grant reports available 0 and must not pull. A
// period window, a queued grant, or an expired grant is not a bank problem.
// The card is its spendable balance before holds. A floor that balance can
// pay waits for the holds instead of withdrawing more.
func pullTokenBankForQuoteShortfall(ctx context.Context, reg *llmservice.Registry, userID, email, code string, needCredits float64, groups []string, model *llmservice.AuthorizedModel, puller tokenBankAdmissionPuller) (bool, error) {
	if puller == nil || !quoteShortfallNeedsPull(ctx, reg, userID, email, code, needCredits, groups, model) {
		return false, nil
	}
	return puller.PullTokenBankForAdmissionShortfall(ctx, userID, email, groups, grossSpendableMicro(reg, userID, email, groups))
}

func quoteShortfallNeedsPull(ctx context.Context, reg *llmservice.Registry, userID, email, code string, needCredits float64, groups []string, model *llmservice.AuthorizedModel) bool {
	if reg == nil || len(groups) == 0 {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "LLM_SERVICE_PERIOD_LIMITED", "LLM_SERVICE_GRANT_QUEUED", "LLM_SERVICE_GRANT_EXPIRED":
		return false
	}
	now := time.Now().UTC()
	allowed, _, eligibilityCode, _, available, _, _ := llmservice.BillingEligibilityForServiceGroupsForUserID(reg, userID, email, groups, now)
	if allowed && available == 0 {
		return false
	}
	if !allowed {
		switch strings.ToUpper(strings.TrimSpace(eligibilityCode)) {
		case "LLM_SERVICE_PERIOD_LIMITED", "LLM_SERVICE_GRANT_QUEUED", "LLM_SERVICE_GRANT_EXPIRED":
			return false
		}
	}
	// A denial that priced a floor already requoted at one output token. A
	// stored quote fills that in only when it can be requoted the same way.
	// Its full reservation is not a floor by itself: using an unpriced number
	// withdraws while the card can still pay for one output token.
	floorMicro := creditsToMicrocredits(needCredits)
	if floorMicro <= 0 {
		if quoted, ok := admissionFloorMicro(ctx, model); ok {
			floorMicro = quoted
		}
	}
	gross := grossSpendableMicro(reg, userID, email, groups)
	if floorMicro > 0 {
		if floorMicro > gross {
			return true
		}
		// The one-token price fits, but reserve holds the stored quote. The
		// output ceiling is restored to that quote when the capped body cannot
		// be priced, so the request still cannot start. Withdraw only when
		// that priced hold is above the whole card and some balance is still
		// free. A hold that clamps available credits to zero leaves the
		// spendable card in place; releasing it can pay the floor, and
		// draining the bank is not required.
		reserved, reservedOK := admissionPricedReservationMicro(ctx, model)
		if reservedOK && reserved > gross && admissionAvailableMicro(reg, userID, email, groups) > 0 {
			return true
		}
		return false
	}
	// No priced floor. Withdraw only when this card is empty before holds.
	// A hold that clamps available credits to zero still leaves the balance
	// on the card, and draining the bank would not be required to start.
	return !allowed && gross <= 0
}

// grossSpendableMicro is the charged card before in-flight holds. Available
// credits clamp at zero, so adding holds onto that number treats an over-hold
// as extra balance and skips a pull the card can never fund.
func grossSpendableMicro(reg *llmservice.Registry, userID, email string, groups []string) int64 {
	if reg == nil || len(groups) == 0 {
		return 0
	}
	return creditsToMicrocredits(llmservice.SpendableCreditsForServiceGroupsForUserID(reg, userID, email, groups, time.Now().UTC()))
}

// admissionAvailableMicro is the post-hold balance the output ceiling was
// fitted to. It can move while pre-hold spendable stays put, when another
// request takes or releases a hold after the handler loaded its registry.
func admissionAvailableMicro(reg *llmservice.Registry, userID, email string, groups []string) int64 {
	if reg == nil || len(groups) == 0 {
		return 0
	}
	return creditsToMicrocredits(llmservice.AvailableCreditsForServiceGroupsForUserID(reg, userID, email, groups, time.Now().UTC()))
}

func admissionFloorMicro(ctx context.Context, model *llmservice.AuthorizedModel) (int64, bool) {
	chosen, ok := largestAdmissionQuote(ctx, model)
	if !ok {
		return 0, false
	}
	one, oneOK := requoteAtOutputLimit(chosen, chosen.InputTokenEstimate, 1)
	if !oneOK || one.ReservedMicrocredits <= 0 {
		return 0, false
	}
	return one.ReservedMicrocredits, true
}

// admissionPricedReservationMicro is the hold reserve already tried to take.
// It is returned only when the same quote can be requoted at one output token,
// so a bare reserved number cannot withdraw by itself.
func admissionPricedReservationMicro(ctx context.Context, model *llmservice.AuthorizedModel) (int64, bool) {
	chosen, ok := largestAdmissionQuote(ctx, model)
	if !ok || chosen.ReservedMicrocredits <= 0 {
		return 0, false
	}
	if _, oneOK := requoteAtOutputLimit(chosen, chosen.InputTokenEstimate, 1); !oneOK {
		return 0, false
	}
	return chosen.ReservedMicrocredits, true
}

func admissionChargedGroups(ctx context.Context, model *llmservice.AuthorizedModel) []string {
	if chosen, ok := largestAdmissionQuote(ctx, model); ok && len(chosen.ServiceGroupIDs) > 0 {
		return append([]string(nil), chosen.ServiceGroupIDs...)
	}
	return chargedGroupsForModel(model)
}

func largestAdmissionQuote(ctx context.Context, model *llmservice.AuthorizedModel) (llmpool.PricingQuoteSnapshot, bool) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return llmpool.PricingQuoteSnapshot{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	name := ""
	if model != nil {
		name = strings.TrimSpace(model.Name)
	}
	var chosen llmpool.PricingQuoteSnapshot
	found := false
	for _, quote := range state.quotes {
		if name != "" && !strings.EqualFold(strings.TrimSpace(quote.LogicalModel), name) {
			continue
		}
		if !found || quote.ReservedMicrocredits > chosen.ReservedMicrocredits {
			chosen = quote
			found = true
		}
	}
	if !found || chosen.ReservedMicrocredits <= 0 {
		return llmpool.PricingQuoteSnapshot{}, false
	}
	return chosen, true
}

// chargedGroupSetsForModel lists each provider wallet separately. A model
// whose providers bill different groups must not add those balances together:
// one provider cannot spend the other provider's card.
func chargedGroupSetsForModel(model *llmservice.AuthorizedModel) [][]string {
	if model == nil {
		return nil
	}
	if len(model.ChargedServiceGroupIDs) > 0 || len(model.ProviderIDs) == 0 {
		groups := chargedGroupsForModel(model)
		if len(groups) == 0 {
			return nil
		}
		return [][]string{groups}
	}
	seen := map[string]struct{}{}
	sets := make([][]string, 0, len(model.ProviderIDs))
	for _, providerID := range model.ProviderIDs {
		groups := llmservice.ChargedServiceGroupIDs(model, providerID)
		key := groupSetKey(groups)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sets = append(sets, groups)
	}
	return sets
}

func groupSetKey(groups []string) string {
	var b strings.Builder
	for _, id := range groups {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(0)
		}
		b.WriteString(id)
	}
	return b.String()
}

func chargedGroupsForModel(model *llmservice.AuthorizedModel) []string {
	if model == nil {
		return nil
	}
	providers := model.ProviderIDs
	if len(providers) == 0 {
		providers = []string{""}
	}
	seen := map[string]struct{}{}
	groups := make([]string, 0, len(providers))
	for _, providerID := range providers {
		for _, groupID := range llmservice.ChargedServiceGroupIDs(model, providerID) {
			key := strings.ToLower(strings.TrimSpace(groupID))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			groups = append(groups, strings.TrimSpace(groupID))
		}
	}
	return groups
}

func resetOfficialAdmissionQuote(ctx context.Context) {
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.reservationHeld {
		return
	}
	for key, quote := range state.quotes {
		if strings.EqualFold(strings.TrimSpace(quote.ProviderID), llmservice.MaClawOfficialProviderID) {
			delete(state.quotes, key)
		}
	}
	state.officialQuote = nil
	state.officialAdmissionGroupFactor = 0
	state.officialAdmissionGroupFactorSet = false
	if strings.EqualFold(strings.TrimSpace(state.heldQuote.ProviderID), llmservice.MaClawOfficialProviderID) {
		state.heldQuote = llmpool.PricingQuoteSnapshot{}
		state.heldQuoteSet = false
	}
}

func insufficientCreditsHeld(reg *llmservice.Registry, userID, email string, serviceGroupIDs []string, now time.Time) float64 {
	if reg == nil {
		return 0
	}
	return llmservice.HeldBillingCreditsForServiceGroupsForUserID(reg, userID, email, serviceGroupIDs, now)
}

// reserveLLMRequestPricing turns the selected request's conservative quote
// into one durable hold. It chooses the largest eligible provider quote, so a
// local failover cannot consume more than admission reserved. The immutable
// final ledger later charges actual usage and releases this hold in the same
// registry save.
func reserveLLMRequestPricing(ctx context.Context, system store.SystemSettingsRepository, userID, email string, model *llmservice.AuthorizedModel) (llmBillingDenial, error) {
	if _, ok := llmEndpointAPIKeyAuthFromContext(ctx); ok {
		return llmBillingDenial{}, nil
	}
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
	llmservice.LockServiceRegistryMutation()
	defer llmservice.UnlockServiceRegistryMutation()
	reg, err := loadCachedLLMServiceRegistryLocked(ctx, system)
	if err != nil {
		return llmBillingDenial{Code: "LLM_BILLING_RESERVATION_FAILED", Message: "unable to reserve credits for this request"}, err
	}
	now := time.Now().UTC()
	reserved, ok := llmservice.ReserveBillingCreditsForUserID(reg, userID, email, chosen.ServiceGroupIDs, requestID, llmpool.MicrocreditsToCredits(chosen.ReservedMicrocredits), chosen.ExpiresAt, now)
	if !ok {
		available := llmservice.AvailableCreditsForServiceGroupsForUserID(reg, userID, email, chosen.ServiceGroupIDs, now)
		held := insufficientCreditsHeld(reg, userID, email, chosen.ServiceGroupIDs, now)
		floorMicro := chosen.ReservedMicrocredits
		if one, oneOK := requoteAtOutputLimit(chosen, chosen.InputTokenEstimate, 1); oneOK {
			floorMicro = one.ReservedMicrocredits
		}
		// The message keeps the amount reserve tried to hold. NeedCredits is the
		// prompt-plus-one-token floor, which is what a token-bank pull has to cover.
		return llmBillingDenial{
			Code:        "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST",
			Message:     insufficientCreditsMessage(llmpool.MicrocreditsToCredits(chosen.ReservedMicrocredits), available, held),
			HeldCredits: held,
			NeedCredits: llmpool.MicrocreditsToCredits(floorMicro),
		}, fmt.Errorf("insufficient credits for quoted request")
	}
	if reserved > 0 {
		if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
			return llmBillingDenial{Code: "LLM_BILLING_RESERVATION_FAILED", Message: "unable to reserve credits for this request"}, err
		}
		invalidateLLMRuntimeCaches(system)
	}
	state.mu.Lock()
	state.reservationHeld = reserved > 0
	if reserved > 0 {
		state.heldQuote = chosen
		state.heldQuoteSet = true
	}
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
	llmservice.LockServiceRegistryMutation()
	defer llmservice.UnlockServiceRegistryMutation()
	reg, err := loadCachedLLMServiceRegistryLocked(context.Background(), system)
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
	llmservice.LockServiceRegistryMutation()
	defer llmservice.UnlockServiceRegistryMutation()
	reg, err := loadCachedLLMServiceRegistryLocked(context.Background(), system)
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
	quote := state.heldQuote
	if !state.heldQuoteSet {
		for _, candidate := range state.quotes {
			if IsMaClawProviderRequest(candidate.ProviderID) {
				quote = candidate
				break
			}
			if candidate.ReservedMicrocredits > quote.ReservedMicrocredits {
				quote = candidate
			}
		}
	}
	state.mu.Unlock()
	if !held || requestID == "" {
		return nil
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	llmservice.LockServiceRegistryMutation()
	defer llmservice.UnlockServiceRegistryMutation()
	reg, err := loadCachedLLMServiceRegistryLocked(context.Background(), system)
	if err != nil {
		return err
	}
	if !llmservice.MarkBillingReservationSent(reg, requestID, time.Now().UTC()) {
		return fmt.Errorf("billing reservation %q is unavailable", requestID)
	}
	if quote.ProviderID != "" && !llmservice.SetBillingReservationBillingDetails(reg, requestID, quote.ProviderID, quote.ProviderMultiplier, quote.BillingGroupMultiplier) {
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
	Scanned  int
	Settled  int
	Pending  int
	Failed   int
	Released int
}

// officialBillingAttemptNotFoundGrace bounds how long a sent reservation may
// be held before a HubCenter "attempt not found" answer is trusted. An
// upstream call can legitimately outlive its quote TTL while streaming, so a
// 404 is proof of "no billable work" only once this dispatch window has
// passed; younger holds stay pending and are re-checked on a later pass.
const officialBillingAttemptNotFoundGrace = 15 * time.Minute

// markReconciledNotFoundBillingReservation releases a hold whose upstream
// attempt HubCenter definitively does not know, retaining the row as terminal
// usage_unresolved evidence. The ledger is rechecked under the billing lock so
// a concurrent online settlement is never undone.
func markReconciledNotFoundBillingReservation(ctx context.Context, system store.SystemSettingsRepository, requestID string) bool {
	if system == nil || strings.TrimSpace(requestID) == "" {
		return false
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	llmservice.LockServiceRegistryMutation()
	defer llmservice.UnlockServiceRegistryMutation()
	reg, err := loadCachedLLMServiceRegistryLocked(ctx, system)
	if err != nil || llmservice.HasBillingRequest(reg, requestID) {
		return false
	}
	if !llmservice.MarkBillingReservationUsageUnresolved(reg, requestID, time.Now().UTC()) {
		return false
	}
	if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
		return false
	}
	invalidateLLMRuntimeCaches(system)
	return true
}

// reconcileOfficialBillingReservations settles sent official requests whose
// response/trailer did not reach Hub. HubCenter's authenticated attempt
// endpoint is the sole source of recovered usage. Missing or unknown attempts
// retain their reservations, except that a definitive not-found answer past
// the dispatch grace window releases the hold (kept as usage_unresolved
// evidence): HubCenter records every billable attempt durably on completion,
// so by then it proves the request never consumed billable upstream work.
func reconcileOfficialBillingReservations(ctx context.Context, system store.SystemSettingsRepository, reservations []llmservice.BillingReservation) OfficialBillingReconciliationResult {
	result := OfficialBillingReconciliationResult{}
	if system == nil {
		return result
	}
	now := time.Now().UTC()
	for _, reservation := range reservations {
		if !IsMaClawProviderRequest(reservation.ProviderID) {
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
		// HubCenter durably records every billable attempt only after the
		// upstream request completed. A not-found answer past the dispatch
		// grace window therefore proves no billable work occurred; releasing
		// the hold unblocks balances that lost-response holds would otherwise
		// squat on indefinitely. This recovery deliberately runs before the
		// BillingGroupMultiplier gate below: the frozen multiplier is only
		// needed to settle a real charge, while rows that crashed between
		// mark-sent and detail-freeze (or predate the details feature) would
		// otherwise hold credits forever yet never reach this scan's cleanup.
		if status == http.StatusNotFound {
			if now.Sub(reservation.SentAt) >= officialBillingAttemptNotFoundGrace {
				if markReconciledNotFoundBillingReservation(ctx, system, reservation.RequestID) {
					log.Printf("[llm-billing] released reservation %s as usage_unresolved after HubCenter reported no billing attempt (sent_at=%s, credits=%.3f)", reservation.RequestID, reservation.SentAt.UTC().Format(time.RFC3339), reservation.Credits)
					result.Released++
				} else {
					result.Pending++
				}
			} else {
				result.Pending++
			}
			continue
		}
		if err != nil {
			result.Failed++
			continue
		}
		// Settlement needs the frozen service-group multiplier recorded at
		// dispatch; without it the debit cannot be reconstructed, so the hold
		// stays for a later pass instead of guessing a price.
		if reservation.BillingGroupMultiplier <= 0 || status < http.StatusOK || status >= http.StatusMultipleChoices || !validOfficialBillingAttempt(attempt) {
			result.Pending++
			continue
		}
		// The reconciliation response is the same authenticated pricing fact as
		// the online path. Apply it here as well so recovered Usage Stats retain
		// the displayed RMB cost rather than showing a misleading zero price.
		usage := applyOfficialTokenPricingUsageSnapshot(corelib.TokenUsageStat{Requests: 1}, &attempt.PricingSnapshot)
		// The reservation holds the admission quote's provider multiplier. It is
		// the durable fallback for an older HubCenter reconciliation response that
		// did not yet include ProviderMultiplier in its snapshot.
		providerMultiplier := llmpool.NormalizeCreditMultiplier(reservation.ProviderMultiplier)
		if attempt.PricingSnapshot.ProviderMultiplier > 0 {
			providerMultiplier = llmpool.NormalizeCreditMultiplier(attempt.PricingSnapshot.ProviderMultiplier)
		}
		effectiveMultiplier := llmpool.CombineCreditMultipliers(providerMultiplier, reservation.BillingGroupMultiplier)
		// The snapshot normally carries the same frozen provider factor as the
		// reservation. An older response can omit it, however, so finish from the
		// actual immutable settlement factors rather than trusting its display
		// value alone.
		applyUsageRMBMultiplier(&usage, effectiveMultiplier/llmpool.NormalizeCreditMultiplier(attempt.PricingSnapshot.ProviderMultiplier))
		credits := 0.0
		if hasBillableTokenLeg(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens) {
			credits = llmservice.EstimateTokenPricingCreditsWithCache(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, attempt.PricingSnapshot.Pricing, effectiveMultiplier)
		}
		pricing := attempt.PricingSnapshot.Pricing
		charge := &pendingCreditCharge{userID: reservation.UserID, email: reservation.Email, serviceGroupIDs: reservation.ServiceGroupIDs, credits: credits, requestID: reservation.RequestID, providerID: llmservice.MaClawOfficialProviderID, usage: usage, multiplier: effectiveMultiplier, providerMultiplier: providerMultiplier, serviceGroupMultiplier: reservation.BillingGroupMultiplier, pricing: &pricing}
		if settled, err := flushCreditChargesDetailed(ctx, system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge}); err != nil {
			result.Failed++
		} else if settled[creditChargeKey(charge)] {
			// A lost response never entered the normal request accumulator. Queue
			// the recovered fact separately only after its debit is durable; this
			// preserves the exact ledger amount without risking a second charge.
			// The concrete HubCenter provider is tooltip provenance, while the
			// aggregate remains under Hub's logical official route.
			reportedAt := attempt.CompletedAt
			if reportedAt.IsZero() {
				reportedAt = time.Now().UTC()
			}
			globalLLMUsageAccumulator.enqueueRecoveredUsageReport(system, llmservice.MaClawOfficialProviderID, attempt.PricingSnapshot.ProviderID, usage, reservation.Email, reportedAt, charge.credits, providerMultiplier, reservation.BillingGroupMultiplier, &pricing, reservation.ServiceGroupIDs...)
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
	if strings.TrimSpace(snapshot.ProviderID) == "" || snapshot.InputTokens < 0 || snapshot.OutputTokens < 0 || snapshot.ProviderMultiplier < 0 || math.IsNaN(snapshot.ProviderMultiplier) || math.IsInf(snapshot.ProviderMultiplier, 0) {
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

// fitLLMQuoteToAvailableBalance lowers the output ceiling to the largest
// value the balance can still pay. The forwarded bodies are capped to that
// same ceiling. estimateBody is the payload whose token estimate feeds the
// quote; it must be one of bodies so each candidate is priced after its own
// digits are written. A false result leaves those bodies unchanged and means
// the balance cannot pay for the prompt plus one output token.
func fitLLMQuoteToAvailableBalance(quote llmpool.PricingQuoteSnapshot, availableCredits float64, estimateBody map[string]any, bodies ...map[string]any) (llmpool.PricingQuoteSnapshot, bool) {
	if availableCredits <= 0 || quote.ReservedMicrocredits <= creditsToMicrocredits(availableCredits) {
		return quote, true
	}
	availableMicro := creditsToMicrocredits(availableCredits)
	requested := quote.OutputTokenLimit
	saved := make([]map[string]any, len(bodies))
	for i, body := range bodies {
		saved[i] = snapshotOutputLimitFields(body)
	}
	restore := func() {
		for i, body := range bodies {
			restoreOutputLimitFields(body, saved[i])
		}
	}
	// A shorter ceiling is a shorter decimal in the JSON, so it changes the
	// prompt estimate it is priced against. Searching with the original
	// estimate stops at the first number that fits and apply cannot raise it
	// again, which leaves output tokens the balance could still pay for.
	// Restore the caller's fields before every probe so a higher candidate
	// can be written, and price that candidate against the body it produces.
	price := func(limit int64) (llmpool.PricingQuoteSnapshot, bool) {
		inputEstimate := quote.InputTokenEstimate
		if estimateBody != nil {
			restore()
			for _, body := range bodies {
				applyLLMQuoteOutputCeiling(body, limit)
			}
			inputEstimate = estimateLLMQuoteInputTokens(estimateBody)
		}
		return requoteAtOutputLimit(quote, inputEstimate, limit)
	}
	limit, fitted, ok := maxAffordableOutputTokens(quote, availableMicro, price)
	if !ok {
		restore()
		return quote, false
	}
	restore()
	for _, body := range bodies {
		applyLLMQuoteOutputCeiling(body, limit)
	}
	if estimateBody != nil {
		// The winning probe and the final write must describe the same
		// payload. A drift here would reserve a different prompt than the
		// one about to be forwarded.
		finalEstimate := estimateLLMQuoteInputTokens(estimateBody)
		if finalEstimate != fitted.InputTokenEstimate {
			refit, refitOK := requoteAtOutputLimit(quote, finalEstimate, limit)
			if !refitOK || refit.ReservedMicrocredits > availableMicro {
				restore()
				return quote, false
			}
			fitted = refit
		}
	}
	if limit < requested {
		log.Printf("[llm-billing] fit output ceiling provider=%s from=%d to=%d reserved=%.3f available=%.3f", quote.ProviderID, requested, limit, llmpool.MicrocreditsToCredits(fitted.ReservedMicrocredits), availableCredits)
	}
	return fitted, true
}

func snapshotOutputLimitFields(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}
	saved := map[string]any{}
	for _, key := range []string{"max_completion_tokens", "max_tokens", "max_output_tokens"} {
		if value, ok := body[key]; ok {
			saved[key] = value
		}
	}
	return saved
}

func restoreOutputLimitFields(body map[string]any, saved map[string]any) {
	if body == nil {
		return
	}
	for _, key := range []string{"max_completion_tokens", "max_tokens", "max_output_tokens"} {
		if value, ok := saved[key]; ok {
			body[key] = value
		} else {
			delete(body, key)
		}
	}
}

// maxAffordableOutputTokens is the largest output count at or below the
// requested ceiling for which price reports a quote inside availableMicro.
// price must be non-decreasing in the limit: a higher ceiling writes an
// equal or longer decimal, so both the output hold and the prompt estimate
// move the same direction. One token is the smallest completion that still
// counts as continued work.
func maxAffordableOutputTokens(sample llmpool.PricingQuoteSnapshot, availableMicro int64, price func(limit int64) (llmpool.PricingQuoteSnapshot, bool)) (int64, llmpool.PricingQuoteSnapshot, bool) {
	one, ok := price(1)
	if !ok || one.ReservedMicrocredits > availableMicro {
		return 0, llmpool.PricingQuoteSnapshot{}, false
	}
	requested := sample.OutputTokenLimit
	if requested < 1 {
		requested = 1
	}
	if requested == 1 {
		return 1, one, true
	}
	if full, fullOK := price(requested); fullOK && full.ReservedMicrocredits <= availableMicro {
		return requested, full, true
	}
	lo, hi := int64(1), requested
	best := int64(1)
	bestQuote := one
	for lo <= hi {
		mid := lo + (hi-lo)/2
		q, qOK := price(mid)
		if qOK && q.ReservedMicrocredits <= availableMicro {
			best = mid
			bestQuote = q
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best, bestQuote, true
}

func requoteAtOutputLimit(quote llmpool.PricingQuoteSnapshot, inputEstimate, outputLimit int64) (llmpool.PricingQuoteSnapshot, bool) {
	next, ok := llmpool.NewPricingQuoteSnapshot(quote.RequestID, quote.AttemptID, quote.ProviderID, quote.Pricing, quote.ProviderMultiplier, quote.BillingGroupMultiplier, inputEstimate, outputLimit, quote.ExpiresAt)
	if !ok {
		return llmpool.PricingQuoteSnapshot{}, false
	}
	next.TenantID = quote.TenantID
	next.LogicalModel = quote.LogicalModel
	next.UpstreamModel = quote.UpstreamModel
	if src := strings.TrimSpace(quote.PricingSource); src != "" {
		next.PricingSource = src
	}
	next.ServiceGroupIDs = append([]string(nil), quote.ServiceGroupIDs...)
	return next, true
}

// applyLLMQuoteOutputCeiling caps every output-limit field to the reserved
// ceiling. A body that omitted all of them gains max_tokens so the forwarded
// request cannot keep a higher provider default than the hold.
func applyLLMQuoteOutputCeiling(body map[string]any, ceiling int64) {
	if body == nil || ceiling <= 0 {
		return
	}
	keys := []string{"max_completion_tokens", "max_tokens", "max_output_tokens"}
	wrote := false
	for _, key := range keys {
		if value, ok := llmQuotePositiveInt64(body[key]); ok {
			wrote = true
			if value > ceiling {
				body[key] = ceiling
			}
		}
	}
	if !wrote {
		body["max_tokens"] = ceiling
	}
}

// capResponsesPayloadToAdmittedCeiling copies the output ceiling admission
// already wrote onto the chat-shaped body onto the original Responses payload.
// Responses-wire providers forward that payload, not the chat copy. A hold
// fitted on the chat copy would otherwise leave max_output_tokens at the
// caller's full ceiling. Responses APIs enforce max_output_tokens; max_tokens
// alone does not cap that wire. A smaller max_output_tokens is left as the
// caller set it. When neither body named an output field, the shared default
// ceiling is left uninjected.
func capResponsesPayloadToAdmittedCeiling(responsesBody, admittedBody map[string]any) {
	if responsesBody == nil || admittedBody == nil {
		return
	}
	admitted, admittedSet := explicitLLMQuoteOutputTokenLimit(admittedBody)
	current, currentSet := explicitLLMQuoteOutputTokenLimit(responsesBody)
	if !admittedSet {
		admitted = defaultLLMPricingQuoteOutputTokenLimit
	}
	if !currentSet {
		current = defaultLLMPricingQuoteOutputTokenLimit
	}
	if current > admitted {
		applyLLMQuoteOutputCeiling(responsesBody, admitted)
		current = admitted
	}
	if _, ok := llmQuotePositiveInt64(responsesBody["max_output_tokens"]); ok {
		return
	}
	if !admittedSet && !currentSet {
		return
	}
	responsesBody["max_output_tokens"] = current
}

// alignLLMPricingQuotesToForwardedCeiling rebuilds any remembered quote for
// logicalModel whose output ceiling is above the body that will actually be
// forwarded. A cheaper provider of this model quoted earlier must not keep a
// hold for tokens the shared body no longer allows. Quotes for other models
// keep the ceiling fitted from the caller's original fields; this model's
// ceiling must not pin theirs.
func alignLLMPricingQuotesToForwardedCeiling(ctx context.Context, body map[string]any, logicalModel string) {
	state := llmBillingStateFrom(ctx)
	if state == nil || body == nil {
		return
	}
	logicalModel = strings.TrimSpace(logicalModel)
	ceiling := llmQuoteOutputTokenLimit(body)
	state.mu.Lock()
	defer state.mu.Unlock()
	for id, quote := range state.quotes {
		if !strings.EqualFold(strings.TrimSpace(quote.LogicalModel), logicalModel) {
			continue
		}
		if quote.OutputTokenLimit <= ceiling {
			continue
		}
		next, ok := requoteAtOutputLimit(quote, quote.InputTokenEstimate, ceiling)
		if !ok {
			continue
		}
		state.quotes[id] = next
	}
}

// applySelectedModelOutputCeiling writes the selected model's fitted output
// ceiling onto the body that will be forwarded. Catalog admission fits every
// eligible model from the caller's original fields and then restores those
// fields, so an unselected model cannot pin the ceiling. Within the selected
// model the ceiling stays the most restrictive eligible provider, which keeps
// a failover inside the hold.
func applySelectedModelOutputCeiling(ctx context.Context, body map[string]any, model *llmservice.AuthorizedModel) {
	if body == nil || model == nil {
		return
	}
	state := llmBillingStateFrom(ctx)
	if state == nil {
		return
	}
	name := strings.TrimSpace(model.Name)
	state.mu.Lock()
	var ceiling int64
	found := false
	for _, quote := range state.quotes {
		if !strings.EqualFold(strings.TrimSpace(quote.LogicalModel), name) {
			continue
		}
		if quote.OutputTokenLimit <= 0 {
			continue
		}
		if !found || quote.OutputTokenLimit < ceiling {
			ceiling = quote.OutputTokenLimit
			found = true
		}
	}
	state.mu.Unlock()
	if !found || ceiling >= llmQuoteOutputTokenLimit(body) {
		return
	}
	applyLLMQuoteOutputCeiling(body, ceiling)
	alignLLMPricingQuotesToForwardedCeiling(ctx, body, name)
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

// llmQuoteOutputTokenLimit is the largest output cap the forwarded body can
// still ask for. A smaller sibling only over-reserves; a larger one would be
// forwarded above the hold, so the reservation follows the maximum.
func llmQuoteOutputTokenLimit(body map[string]any) int64 {
	if limit, found := explicitLLMQuoteOutputTokenLimit(body); found {
		return limit
	}
	return defaultLLMPricingQuoteOutputTokenLimit
}

func explicitLLMQuoteOutputTokenLimit(body map[string]any) (int64, bool) {
	var limit int64
	found := false
	for _, key := range []string{"max_completion_tokens", "max_tokens", "max_output_tokens"} {
		value, ok := llmQuotePositiveInt64(body[key])
		if !ok {
			continue
		}
		if !found || value > limit {
			limit = value
			found = true
		}
	}
	return limit, found
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
	// Directional pricing uses both HubCenter's request-frozen provider/time-of-
	// use multiplier and Hub's service-group multiplier.
	// HubCenter returns the exact frozen snapshot used for the forwarded official
	// request. Prefer it over the admission quote because a compatibility retry
	// may legitimately need a new quote for a sanitized payload.
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil && IsMaClawProviderRequest(providerID) {
		providerMultiplier, groupMultiplier := officialRouteMultipliers(ctx, providerID, serviceReg, serviceGroupIDs)
		multiplier = llmpool.CombineCreditMultipliers(providerMultiplier, groupMultiplier)
		if !hasBillableTokenLeg(snapshot.InputTokens, snapshot.OutputTokens, snapshot.CachedInputTokens, snapshot.CacheWriteTokens) {
			return 0, multiplier
		}
		credits = llmservice.EstimateTokenPricingCreditsWithCache(snapshot.InputTokens, snapshot.OutputTokens, snapshot.CachedInputTokens, snapshot.CacheWriteTokens, snapshot.Pricing, multiplier)
		return credits, multiplier
	}
	if quote, ok := snapshotLLMPricingQuoteForModel(ctx, providerID, model); ok && llmPricingQuoteAppliesToModel(quote, model) {
		multiplier = llmpool.CombineCreditMultipliers(quote.ProviderMultiplier, quote.BillingGroupMultiplier)
		if !hasBillableTokenLeg(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens) {
			return 0, multiplier
		}
		credits = llmservice.EstimateTokenPricingCreditsWithCache(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, quote.Pricing, multiplier)
		return credits, multiplier
	}
	if IsMaClawProviderRequest(providerID) {
		providerMultiplier, groupMultiplier := officialRouteMultipliers(ctx, providerID, serviceReg, serviceGroupIDs)
		multiplier = llmpool.CombineCreditMultipliers(providerMultiplier, groupMultiplier)
		if hasBillableTokenLeg(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens) || usage.TotalTokens > 0 {
			log.Printf("[llm-billing] official usage missing HubCenter directional price provider=%s input=%d cache_read=%d cache_write=%d output=%d total=%d", providerID, usage.InputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, usage.OutputTokens, usage.TotalTokens)
		}
		return 0, multiplier
	}
	if pricing, ok := llmservice.ResolveTokenPricingForProviderRoute(model, providerID, billingUpstreamModel(model, providerID), startedAt); ok {
		groupMultiplier := llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
		multiplier = groupMultiplier
		if !hasBillableTokenLeg(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens) {
			return 0, multiplier
		}
		credits = llmservice.EstimateTokenPricingCreditsWithCache(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, pricing, multiplier)
		return credits, multiplier
	}
	// Local provider pricing is stored in the provider registry when no
	// service-group route override exists. Honor that provider-owned price
	// before falling back to the legacy tokens-per-credit formula.
	if pricing, ok := resolveLocalProviderTokenPricing(providerReg, providerID, startedAt); ok {
		groupMultiplier := llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
		multiplier = groupMultiplier
		if !hasBillableTokenLeg(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens) {
			return 0, multiplier
		}
		credits = llmservice.EstimateTokenPricingCreditsWithCache(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, pricing, multiplier)
		return credits, multiplier
	}
	multiplier = resolveBillableCreditMultiplier(ctx, model, providerID, providerReg)
	if !hasBillableTokenLeg(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens) && usage.TotalTokens <= 0 {
		return 0, multiplier
	}
	if usage.CachedInputTokens > 0 || usage.CacheWriteTokens > 0 {
		log.Printf("[llm-billing] cache-aware usage billed via legacy tokens-per-credit formula provider=%s input=%d cache_read=%d cache_write=%d output=%d total=%d", providerID, usage.InputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, usage.OutputTokens, usage.TotalTokens)
	}
	credits = llmservice.EstimateCreditsWithFloor(usage.TotalTokens, multiplier, tokensPerCredit)
	return credits, multiplier
}

// hasBillableTokenLeg distinguishes an explicit empty/zero-usage response from
// a real request. Minimum-request pricing is only meaningful after at least
// one directional token leg (normal input, cache read/write, or output) exists.
func hasBillableTokenLeg(inputTokens, outputTokens, cachedInputTokens, cacheWriteTokens int64) bool {
	return inputTokens > 0 || outputTokens > 0 || cachedInputTokens > 0 || cacheWriteTokens > 0
}

func resolveLocalProviderTokenPricing(providerReg *im.LLMProviderRegistry, providerID string, startedAt time.Time) (llmpool.ResolvedTokenPricing, bool) {
	if providerReg == nil || IsMaClawProviderRequest(providerID) {
		return llmpool.ResolvedTokenPricing{}, false
	}
	provider := providerReg.FindProvider(providerID)
	if provider == nil || !provider.TokenPricing.HasCreditPricing() {
		return llmpool.ResolvedTokenPricing{}, false
	}
	return llmpool.ResolveTokenPricing(provider.TokenPricing, startedAt)
}

func chargeLoggedLLMEndpointUsage(ctx context.Context, system store.SystemSettingsRepository, securitySvc *security.SecurityService, userID, email, providerID string, model *llmservice.AuthorizedModel, providerReg *im.LLMProviderRegistry, serviceReg *llmservice.Registry, usage corelib.TokenUsageStat, serviceGroupIDs []string) (credits, multiplier float64) {
	if strings.TrimSpace(providerID) == "" {
		return 0, 0
	}
	officialSnapshot := snapshotOfficialTokenPricing(ctx)
	hasOfficialDirectionalSnapshot := officialSnapshot != nil && IsMaClawProviderRequest(providerID)
	// HubCenter's authenticated snapshot is the authoritative final usage for
	// official routes. OpenAI-compatible bodies may omit `usage` entirely, so
	// carrying the body-parsed zero value into the ledger would make a correctly
	// charged request appear to have consumed zero Tokens in audits and reports.
	if hasOfficialDirectionalSnapshot {
		usage = applyOfficialTokenPricingUsageSnapshot(usage, officialSnapshot)
		if usage.Requests <= 0 {
			usage.Requests = 1
		}
	}
	tokensPerCredit := 0
	if serviceReg != nil {
		tokensPerCredit = serviceReg.TokensPerCredit
	}
	credits, multiplier = computeLLMRequestBilling(ctx, model, providerID, providerReg, serviceReg, serviceGroupIDs, usage, tokensPerCredit)
	providerMultiplier, serviceGroupMultiplier := llmUsageReportMultipliers(ctx, providerID, serviceReg, serviceGroupIDs, multiplier)
	localProviderPricing := false
	if hasOfficialDirectionalSnapshot {
		// Recompute RMB from the upstream snapshot with the same provider ×
		// service-group factors used for Credits. Do not scale the already
		// provider-weighted display rates; that ratio drifts when the snapshot
		// omitted ProviderMultiplier and settlement used the quote factor.
		usage = applyResolvedTokenPricingUsageSnapshot(usage, officialSnapshot.Pricing, providerMultiplier, serviceGroupMultiplier)
	}
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
	if hasOfficialDirectionalSnapshot {
		copyPricing := officialSnapshot.Pricing
		pricing = &copyPricing
	} else if quote, ok := snapshotLLMPricingQuoteForModel(ctx, providerID, model); ok && llmPricingQuoteAppliesToModel(quote, model) {
		copyPricing := quote.Pricing
		pricing = &copyPricing
	} else if !IsMaClawProviderRequest(providerID) {
		if resolved, ok := llmservice.ResolveTokenPricingForProviderRoute(model, providerID, billingUpstreamModel(model, providerID), pricingStartedAt); ok {
			pricing = &resolved
		} else if resolved, ok := resolveLocalProviderTokenPricing(providerReg, providerID, pricingStartedAt); ok {
			pricing = &resolved
			localProviderPricing = true
		}
	}
	if localProviderPricing {
		// The local provider registry owns the base directional price; only the
		// Hub service-group multiplier is applied to the user debit.
		providerMultiplier = 1
		serviceGroupMultiplier = llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
	}
	if pricing != nil && !hasOfficialDirectionalSnapshot {
		if localProviderPricing {
			// The price came from the provider registry after the route
			// resolution failed (for example an override whose time windows
			// currently resolve to no billable price). The static route-source
			// probe cannot see that dynamic failure, so label the actual
			// supplier here instead of re-deriving it from the configuration.
			usage.PricingSource = llmpool.PricingSourceProvider
		} else if quote, ok := snapshotLLMPricingQuoteForModel(ctx, providerID, model); ok && llmPricingQuoteAppliesToModel(quote, model) && strings.TrimSpace(quote.PricingSource) != "" {
			usage.PricingSource = strings.TrimSpace(quote.PricingSource)
		} else {
			usage.PricingSource = llmservice.PricingSourceForProviderRoute(model, providerID, billingUpstreamModel(model, providerID))
		}
		// Local directional routes are debited from the exact resolved route
		// price (or its admission quote), not from the provider-wide display
		// price which may describe a different route. Freeze that same RMB
		// fact in the usage report, including every multiplier in the debit.
		usage = applyResolvedTokenPricingUsageSnapshot(usage, *pricing, providerMultiplier, serviceGroupMultiplier)
	}
	if IsMaClawProviderRequest(providerID) && pricing == nil && (hasBillableTokenLeg(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens) || usage.TotalTokens > 0) {
		// Keep any sent reservation for HubCenter reconciliation. A zero ledger
		// debit would look settled and block recovery of the upstream price.
		clearUsageRMBCosts(&usage)
		publishLoggedLLMUsage(ctx, system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, 0, meta, "", multiplier, providerMultiplier, serviceGroupMultiplier, nil)
		return 0, multiplier
	}
	markLLMBillingSettlementQueued(ctx)
	publishLoggedLLMUsage(ctx, system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, meta, llmBillingRequestID(ctx), multiplier, providerMultiplier, serviceGroupMultiplier, pricing)
	return credits, multiplier
}

func publishLoggedLLMUsage(ctx context.Context, system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID, email string, serviceGroupIDs, userGroupIDs []string, credits float64, meta llmservice.OfficialForwardMeta, requestID string, multiplier, providerMultiplier, serviceGroupMultiplier float64, pricing *llmpool.ResolvedTokenPricing) {
	enqueueLLMUsageRecordWithBilling(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, meta, requestID, multiplier, providerMultiplier, serviceGroupMultiplier, pricing, officialUsageReportServiceGroupIDs(ctx, providerID, serviceGroupIDs), usageReportBillingProviderID(ctx, providerID))
	recordLLMClassTraffic(system, serviceGroupIDs, meta, usage, meta.Preview)
	recordLLMClassHeadSample(system, serviceGroupIDs, meta)
}

func officialUsageReportServiceGroupIDs(ctx context.Context, providerID string, charged []string) []string {
	if !IsMaClawProviderRequest(providerID) {
		return nil
	}
	if quote, ok := snapshotOfficialForwardQuote(ctx); ok {
		if id := strings.TrimSpace(quote.ServiceGroupID); id != "" {
			return []string{id}
		}
	}
	return usageReportServiceGroupIDs(charged)
}

// llmUsageReportMultipliers returns the actual factors used in this request's
// debit. Directional official pricing gets the provider/time-of-use factor
// from HubCenter's authenticated snapshot; legacy billing has one factor.
func llmUsageReportMultipliers(ctx context.Context, providerID string, serviceReg *llmservice.Registry, serviceGroupIDs []string, effectiveMultiplier float64) (providerMultiplier, serviceGroupMultiplier float64) {
	if IsMaClawProviderRequest(providerID) {
		return officialRouteMultipliers(ctx, providerID, serviceReg, serviceGroupIDs)
	}
	// Local directional routes have no HubCenter-owned provider factor: the
	// whole effective multiplier is the Hub service-group factor.
	return 1, llmpool.NormalizeCreditMultiplier(effectiveMultiplier)
}

func officialGroupMultiplier(ctx context.Context, serviceReg *llmservice.Registry, serviceGroupIDs []string, quotedCapability float64) float64 {
	groupMultiplier := llmservice.BillingGroupMultiplier(serviceReg, serviceGroupIDs)
	capability := quotedCapability
	if capability <= 0 || math.IsNaN(capability) || math.IsInf(capability, 0) {
		clientModel := llmservice.OfficialForwardMetaFrom(ctx).ClientModel
		capability = llmservice.CapabilityBillingMultiplierForGroups(serviceReg, serviceGroupIDs, clientModel)
	}
	return llmpool.CombineCreditMultipliers(groupMultiplier, capability)
}

func officialRouteMultipliers(ctx context.Context, providerID string, serviceReg *llmservice.Registry, serviceGroupIDs []string) (providerMultiplier, serviceGroupMultiplier float64) {
	serviceGroupMultiplier = officialGroupMultiplier(ctx, serviceReg, serviceGroupIDs, 0)
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil {
		providerMultiplier = llmpool.NormalizeCreditMultiplier(snapshot.ProviderMultiplier)
		if quote, ok := snapshotLLMPricingQuote(ctx, providerID); ok && quote.BillingGroupMultiplier > 0 {
			serviceGroupMultiplier = quote.BillingGroupMultiplier
			if snapshot.ProviderMultiplier <= 0 {
				providerMultiplier = llmpool.NormalizeCreditMultiplier(quote.ProviderMultiplier)
			}
		}
		return providerMultiplier, serviceGroupMultiplier
	}
	if quote, ok := snapshotLLMPricingQuote(ctx, providerID); ok {
		return llmpool.NormalizeCreditMultiplier(quote.ProviderMultiplier), llmpool.NormalizeCreditMultiplier(quote.BillingGroupMultiplier)
	}
	startedAt := time.Now()
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		if !state.started.IsZero() {
			startedAt = state.started
		}
		state.mu.Unlock()
	}
	return officialProviderMultiplierFallback(ctx, startedAt), serviceGroupMultiplier
}

// officialProviderMultiplierFallback resolves a HubCenter provider's multiplier
// only for the compatibility path where the response has no immutable pricing
// snapshot or admission quote.  The concrete provider ID still comes from the
// authenticated response header, while its published policy is evaluated at
// the same request-start instant as HubCenter.  New requests always prefer the
// snapshot above, so a later config change cannot alter an audited settlement.
func officialProviderMultiplierFallback(ctx context.Context, startedAt time.Time) float64 {
	providerID := ""
	if state := llmBillingStateFrom(ctx); state != nil {
		state.mu.Lock()
		providerID = strings.TrimSpace(state.officialProviderID)
		state.mu.Unlock()
	}
	if providerID == "" {
		return 1
	}
	if access := GetMaClawAccessControl(); access != nil {
		if policy, ok := llmpool.FindProviderBillingPolicy(access.OfficialProviderBilling(), providerID); ok {
			return llmpool.ResolveCreditMultiplier(policy, startedAt)
		}
	}
	return 1
}

// usageReportBillingProviderID returns the concrete provider behind an
// official logical route. It is used only as multiplier provenance in the
// usage-report tooltip; provider-scoped aggregation remains keyed by the Hub
// route so existing filters and historical series do not change.
func usageReportBillingProviderID(ctx context.Context, providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if IsMaClawProviderRequest(providerID) {
		if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil {
			if resolved := strings.TrimSpace(snapshot.ProviderID); resolved != "" {
				return resolved
			}
		}
		if state := llmBillingStateFrom(ctx); state != nil {
			state.mu.Lock()
			resolved := strings.TrimSpace(state.officialProviderID)
			state.mu.Unlock()
			if resolved != "" {
				return resolved
			}
		}
	}
	return providerID
}

// authoritativeLLMUsageForAccessLog applies the authenticated official
// snapshot to the handler's access-log copy as well as to the billing copy.
// The final credit ledger and the user-visible access log must describe the
// same input/output usage.
func authoritativeLLMUsageForAccessLog(ctx context.Context, providerID string, usage corelib.TokenUsageStat) corelib.TokenUsageStat {
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil && IsMaClawProviderRequest(providerID) {
		usage = applyOfficialTokenPricingUsageSnapshot(usage, snapshot)
		if usage.Requests <= 0 {
			usage.Requests = 1
		}
	}
	return usage
}

// recordLocalCacheHitLLMUsage records a local full-response cache hit in Usage
// Stats without any charge. A cache hit never contacts the upstream provider,
// so there is no real token usage (design §2.2): the token legs stay zero, the
// request itself still counts, and the cache_usage_source "local_cache" marker
// keeps it separate from provider-reported prompt-cache reads.
func recordLocalCacheHitLLMUsage(ctx context.Context, system store.SystemSettingsRepository, securitySvc *security.SecurityService, userID, email, providerID string, serviceGroupIDs []string) {
	if system == nil || strings.TrimSpace(providerID) == "" {
		return
	}
	userGroupIDs := []string(nil)
	if securitySvc != nil {
		if resolved, resolveErr := securitySvc.ResolveUserGroupChain(ctx, email); resolveErr == nil {
			userGroupIDs = resolved
		}
	}
	usage := corelib.TokenUsageStat{Requests: 1, CacheUsageSource: "local_cache"}
	enqueueLLMUsageRecord(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, 0, llmservice.OfficialForwardMetaFrom(ctx))
}

// applyOfficialTokenPricingUsageSnapshot applies HubCenter's authenticated
// directional usage and RMB display pricing to a Hub-side usage record. Its
// base RMB price is weighted by HubCenter's frozen provider multiplier. Hub's
// service-group multiplier is applied later, where that independently-owned
// factor is available. Credit settlement remains based on the pricing
// snapshot's credit fields elsewhere.
func applyOfficialTokenPricingUsageSnapshot(usage corelib.TokenUsageStat, snapshot *llmpool.TokenPricingSnapshot) corelib.TokenUsageStat {
	if snapshot == nil {
		return usage
	}
	usage.InputTokens = snapshot.InputTokens
	usage.OutputTokens = snapshot.OutputTokens
	usage.CachedInputTokens = snapshot.CachedInputTokens
	usage.CacheWriteTokens = snapshot.CacheWriteTokens
	usage.PricingSource = strings.TrimSpace(snapshot.PricingSource)
	if usage.PricingSource == "" {
		usage.PricingSource = llmpool.PricingSourceProvider
	}
	usage.TotalTokens = snapshot.InputTokens + snapshot.OutputTokens
	providerMultiplier := llmpool.NormalizeCreditMultiplier(snapshot.ProviderMultiplier)
	p := snapshot.Pricing.TokenPricing.WithCachePricingDefaults()
	usage.InputPricePerMTokensRMB = p.InputRMBPer10K * 100 * providerMultiplier
	usage.OutputPricePerMTokensRMB = p.OutputRMBPer10K * 100 * providerMultiplier
	usage.CacheReadPricePerMTokensRMB = llmpool.OptionalTokenPriceValue(p.CacheReadRMBPer10K) * 100 * providerMultiplier
	usage.CacheWritePricePerMTokensRMB = llmpool.OptionalTokenPriceValue(p.CacheWriteRMBPer10K) * 100 * providerMultiplier
	usage.InputCostRMB, usage.OutputCostRMB, usage.CacheReadCostRMB, usage.CacheWriteCostRMB = calculateLLMCostRMBWithCache(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, usage.InputPricePerMTokensRMB, usage.OutputPricePerMTokensRMB, usage.CacheReadPricePerMTokensRMB, usage.CacheWritePricePerMTokensRMB)
	usage.TotalCostRMB = usage.InputCostRMB + usage.CacheReadCostRMB + usage.CacheWriteCostRMB + usage.OutputCostRMB
	return usage
}

func calculateLLMCostRMBWithCache(input, output, cached, written int64, inputRate, outputRate, cacheReadRate, cacheWriteRate float64) (inputCost, outputCost, cacheReadCost, cacheWriteCost float64) {
	if input < 0 {
		input = 0
	}
	if output < 0 {
		output = 0
	}
	if cached < 0 {
		cached = 0
	}
	if written < 0 {
		written = 0
	}
	if cached > input {
		cached = input
	}
	if written > input-cached {
		written = input - cached
	}
	normal := input - cached - written
	inputCost = float64(normal) * inputRate / 1_000_000
	cacheReadCost = float64(cached) * cacheReadRate / 1_000_000
	cacheWriteCost = float64(written) * cacheWriteRate / 1_000_000
	outputCost = float64(output) * outputRate / 1_000_000
	return
}

// applyUsageRMBMultiplier adds a settlement multiplier to already-resolved RMB
// usage. It intentionally operates only on the display/equivalent amount;
// Credits are always calculated from their own directional price fields.
func applyUsageRMBMultiplier(usage *corelib.TokenUsageStat, multiplier float64) {
	if usage == nil {
		return
	}
	multiplier = llmpool.NormalizeCreditMultiplier(multiplier)
	usage.InputPricePerMTokensRMB *= multiplier
	usage.OutputPricePerMTokensRMB *= multiplier
	usage.CacheReadPricePerMTokensRMB *= multiplier
	usage.CacheWritePricePerMTokensRMB *= multiplier
	usage.InputCostRMB, usage.OutputCostRMB, usage.CacheReadCostRMB, usage.CacheWriteCostRMB = calculateLLMCostRMBWithCache(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, usage.InputPricePerMTokensRMB, usage.OutputPricePerMTokensRMB, usage.CacheReadPricePerMTokensRMB, usage.CacheWritePricePerMTokensRMB)
	usage.TotalCostRMB = usage.InputCostRMB + usage.CacheReadCostRMB + usage.CacheWriteCostRMB + usage.OutputCostRMB
}

func clearUsageRMBCosts(usage *corelib.TokenUsageStat) {
	if usage == nil {
		return
	}
	usage.InputPricePerMTokensRMB = 0
	usage.OutputPricePerMTokensRMB = 0
	usage.CacheReadPricePerMTokensRMB = 0
	usage.CacheWritePricePerMTokensRMB = 0
	usage.InputCostRMB = 0
	usage.OutputCostRMB = 0
	usage.CacheReadCostRMB = 0
	usage.CacheWriteCostRMB = 0
	usage.TotalCostRMB = 0
}

// applyResolvedTokenPricingUsageSnapshot records the exact directional RMB
// price used for a local request. Unlike the legacy provider-wide display
// price, this value has already resolved the route and time-of-use window at
// request start. The caller applies the same provider and service-group
// multipliers used by the settled Credits debit.
func applyResolvedTokenPricingUsageSnapshot(usage corelib.TokenUsageStat, pricing llmpool.ResolvedTokenPricing, providerMultiplier, serviceGroupMultiplier float64) corelib.TokenUsageStat {
	p := pricing.TokenPricing.WithCachePricingDefaults()
	usage.InputPricePerMTokensRMB = p.InputRMBPer10K * 100
	usage.OutputPricePerMTokensRMB = p.OutputRMBPer10K * 100
	usage.CacheReadPricePerMTokensRMB = llmpool.OptionalTokenPriceValue(p.CacheReadRMBPer10K) * 100
	usage.CacheWritePricePerMTokensRMB = llmpool.OptionalTokenPriceValue(p.CacheWriteRMBPer10K) * 100
	usage.InputCostRMB, usage.OutputCostRMB, usage.CacheReadCostRMB, usage.CacheWriteCostRMB = calculateLLMCostRMBWithCache(usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, usage.InputPricePerMTokensRMB, usage.OutputPricePerMTokensRMB, usage.CacheReadPricePerMTokensRMB, usage.CacheWritePricePerMTokensRMB)
	usage.TotalCostRMB = usage.InputCostRMB + usage.CacheReadCostRMB + usage.CacheWriteCostRMB + usage.OutputCostRMB
	applyUsageRMBMultiplier(&usage, llmpool.CombineCreditMultipliers(providerMultiplier, serviceGroupMultiplier))
	return usage
}
