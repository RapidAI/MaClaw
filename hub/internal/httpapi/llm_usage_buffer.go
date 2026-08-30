package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type pendingSystemUsage struct {
	providerUsage map[string]corelib.TokenUsageStat
	creditCharges map[string]*pendingCreditCharge
	reports       *llmUsageReportsStore
}

type pendingCreditCharge struct {
	userID                 string
	email                  string
	serviceGroupIDs        []string
	userGroupIDs           []string
	credits                float64
	reportedCredits        float64
	reportedAt             time.Time
	requestID              string
	providerID             string
	usage                  corelib.TokenUsageStat
	multiplier             float64
	providerMultiplier     float64
	serviceGroupMultiplier float64
	pricing                *llmpool.ResolvedTokenPricing
	meta                   llmservice.OfficialForwardMeta
}

type llmUsageAccumulator struct {
	mu       sync.Mutex
	once     sync.Once
	pending  map[store.SystemSettingsRepository]*pendingSystemUsage
	interval time.Duration
}

var globalLLMUsageAccumulator = &llmUsageAccumulator{
	pending:  map[store.SystemSettingsRepository]*pendingSystemUsage{},
	interval: 20 * time.Second,
}

var llmCreditChargeMu sync.Mutex

func enqueueLLMUsage(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64) {
	enqueueLLMUsageForUserID(system, providerID, usage, "", email, serviceGroupIDs, userGroupIDs, credits)
}

func enqueueLLMUsageForUserID(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID string, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64) {
	enqueueLLMUsageRecord(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, llmservice.OfficialForwardMeta{})
}

func enqueueLLMUsageRecord(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID string, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64, meta llmservice.OfficialForwardMeta) {
	enqueueLLMUsageRecordWithBilling(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, meta, "", 0, 0, 0, nil)
}

func enqueueLLMUsageRecordWithBilling(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID string, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64, meta llmservice.OfficialForwardMeta, requestID string, multiplier, providerMultiplier, serviceGroupMultiplier float64, pricing *llmpool.ResolvedTokenPricing, billingProviderIDs ...string) {
	if system == nil {
		return
	}
	if isRemoteCodingToolUsageProviderID(providerID) {
		log.Printf("[llm-usage] ignoring remote coding tool provider %q; remote tool tokens are session diagnostics, not Hub LLM usage", providerID)
		return
	}
	// A request-scoped record is persisted only after the request's debit and
	// its idempotency marker are durable in the same registry save. Legacy
	// callers keep the existing eager usage-record behavior.
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		persistLLMUsageRecords(system, providerID, usage, userID, email, serviceGroupIDs, credits, meta)
	}
	globalLLMUsageAccumulator.start()
	if pricing == nil {
		// Legacy token-count billing has only one provider/model multiplier.
		providerMultiplier = multiplier
		serviceGroupMultiplier = 0
	}
	breakdown := settledUsageCreditBreakdown(usage, credits, providerMultiplier, serviceGroupMultiplier, pricing)
	if breakdown == nil {
		// Legacy token-count billing has no directional price, but it is still a
		// settled and fully attributable debit. Keep it separate from imported
		// historical records so the Usage Stats tooltip can show the provider
		// multiplier that was actually used.
		breakdown = &llmUsageCreditBreakdown{UnitemizedComponent: credits}
	}
	// Keep the aggregate keyed by providerID, while retaining an optional
	// concrete upstream provider as the billing provenance shown in the tooltip.
	// This is needed for the official HubCenter route, whose logical Hub ID and
	// final selected provider can legitimately differ.
	billingProviderID := strings.TrimSpace(providerID)
	if len(billingProviderIDs) > 0 {
		if resolved := strings.TrimSpace(billingProviderIDs[0]); resolved != "" {
			billingProviderID = resolved
		}
	}
	breakdown.ProviderID = billingProviderID
	breakdown.ProviderMultiplier = llmpool.NormalizeCreditMultiplier(providerMultiplier)
	if serviceGroupMultiplier > 0 {
		breakdown.ServiceGroupMultiplier = llmpool.NormalizeCreditMultiplier(serviceGroupMultiplier)
	}
	charge := globalLLMUsageAccumulator.enqueue(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, requestID, breakdown)
	if charge == nil {
		return
	}
	charge.requestID = strings.TrimSpace(requestID)
	charge.providerID = strings.TrimSpace(providerID)
	charge.usage = usage
	charge.multiplier = multiplier
	charge.providerMultiplier = providerMultiplier
	charge.serviceGroupMultiplier = serviceGroupMultiplier
	charge.meta = meta
	if pricing != nil {
		copyPricing := *pricing
		charge.pricing = &copyPricing
	}
	// Apply the charge immediately so a successful response cannot leave a
	// short-lived window in which concurrent requests all pass the same limit.
	// Keep the full route identity as the key: requeueing different routes under
	// one generic key previously merged their credits and charged them against
	// whichever group happened to be retained first.
	chargeKey := creditChargeKey(charge)
	_, err := flushCreditChargesDetailed(context.Background(), system, map[string]*pendingCreditCharge{chargeKey: charge})
	if err != nil {
		log.Printf("[llm-usage] immediate credit charge failed: %v", err)
		globalLLMUsageAccumulator.requeue(system, &pendingSystemUsage{creditCharges: map[string]*pendingCreditCharge{chargeKey: charge}})
		return
	}
	globalLLMUsageAccumulator.applySettledCreditAdjustment(system, charge)
}

func settledUsageCreditBreakdown(usage corelib.TokenUsageStat, credits, providerMultiplier, serviceGroupMultiplier float64, pricing *llmpool.ResolvedTokenPricing) *llmUsageCreditBreakdown {
	if pricing == nil {
		return nil
	}
	// The display components must follow exactly the same decimal arithmetic as
	// the debit.  Do not derive the multiplier through binary float math here:
	// a configuration such as 1.1 × 1.2 can otherwise leave a visible residual
	// even before the request-level microcredit rounding is applied.
	multiplier := llmpool.CombineCreditMultipliers(providerMultiplier, serviceGroupMultiplier)
	input, output, minimumAdjustment, ok := llmpool.TokenPricingCreditComponents(
		usage.InputTokens,
		usage.OutputTokens,
		*pricing,
		multiplier,
	)
	if !ok {
		return nil
	}
	// The final debit is rounded per request. Preserve the tiny residual so the
	// displayed components always add up to the exact settled Credits value.
	roundingAdjustment := credits - input - output - minimumAdjustment
	return &llmUsageCreditBreakdown{
		InputComponent:      input,
		OutputComponent:     output,
		MinimumAdjustment:   minimumAdjustment,
		RoundingAdjustment:  roundingAdjustment,
		InputCreditsPer10K:  pricing.InputCreditsPer10K,
		OutputCreditsPer10K: pricing.OutputCreditsPer10K,
		InputRMBPer10K:      pricing.InputRMBPer10K,
		OutputRMBPer10K:     pricing.OutputRMBPer10K,
		RMBPricingRecorded:  true,
	}
}

func creditChargeKey(charge *pendingCreditCharge) string {
	if charge == nil {
		return ""
	}
	userID := strings.TrimSpace(charge.userID)
	email := strings.ToLower(strings.TrimSpace(charge.email))
	groups := normalizeUsageStringSlice(charge.serviceGroupIDs)
	if requestID := strings.TrimSpace(charge.requestID); requestID != "" {
		return "request\n" + requestID
	}
	return userID + "\n" + email + "\n" + strings.Join(groups, "\x1f")
}

func (a *llmUsageAccumulator) start() {
	a.once.Do(func() {
		go func() {
			ticker := time.NewTicker(a.interval)
			defer ticker.Stop()
			for range ticker.C {
				a.flush(context.Background())
			}
		}()
	})
}

func (a *llmUsageAccumulator) enqueue(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID string, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64, requestID string, breakdown *llmUsageCreditBreakdown) *pendingCreditCharge {
	a.mu.Lock()
	defer a.mu.Unlock()
	buf := a.pending[system]
	if buf == nil {
		buf = &pendingSystemUsage{providerUsage: map[string]corelib.TokenUsageStat{}, creditCharges: map[string]*pendingCreditCharge{}, reports: &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}}
		a.pending[system] = buf
	}
	providerID = strings.TrimSpace(providerID)
	if providerID != "" {
		curr := buf.providerUsage[providerID]
		curr.InputTokens += usage.InputTokens
		curr.OutputTokens += usage.OutputTokens
		curr.TotalTokens += usage.TotalTokens
		curr.CachedInputTokens += usage.CachedInputTokens
		curr.CacheWriteTokens += usage.CacheWriteTokens
		curr.InputCostRMB += usage.InputCostRMB
		curr.OutputCostRMB += usage.OutputCostRMB
		curr.TotalCostRMB += usage.TotalCostRMB
		curr.Requests += usage.Requests
		curr.CachedRequests += usage.CachedRequests
		buf.providerUsage[providerID] = curr
	}
	serviceGroupIDs = normalizeUsageStringSlice(serviceGroupIDs)
	userID = strings.TrimSpace(userID)
	email = strings.ToLower(strings.TrimSpace(email))
	var charge *pendingCreditCharge
	// Request-scoped settlements must flow through the immediate ledger path
	// even when a legitimate provider response has zero billable usage. Legacy
	// aggregate usage continues to omit zero-credit no-op entries.
	if (userID != "" || email != "") && len(serviceGroupIDs) > 0 && (credits > 0 || strings.TrimSpace(requestID) != "") {
		charge = &pendingCreditCharge{
			userID:          userID,
			email:           email,
			serviceGroupIDs: append([]string(nil), serviceGroupIDs...),
			userGroupIDs:    append([]string(nil), userGroupIDs...),
			credits:         credits,
			reportedCredits: credits,
			reportedAt:      time.Now(),
		}
	}
	if buf.reports == nil {
		buf.reports = &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	}
	reportedAt := time.Now()
	if charge != nil && !charge.reportedAt.IsZero() {
		reportedAt = charge.reportedAt
	}
	buf.reports.addUsageWithCreditBreakdown(reportedAt, email, userGroupIDs, usage, credits, breakdown, providerID)
	return charge
}

// enqueueRecoveredUsageReport records an authenticated official reconciliation
// without creating another credit charge. The recovered request already has a
// durable ledger entry, but it never passed through the normal response-path
// accumulator. billingProviderID is only the concrete pricing provenance shown
// in the tooltip; providerID keeps provider-scoped report series stable.
func (a *llmUsageAccumulator) enqueueRecoveredUsageReport(system store.SystemSettingsRepository, providerID, billingProviderID string, usage corelib.TokenUsageStat, email string, reportedAt time.Time, credits, providerMultiplier, serviceGroupMultiplier float64, pricing *llmpool.ResolvedTokenPricing) {
	if a == nil || system == nil {
		return
	}
	if reportedAt.IsZero() {
		reportedAt = time.Now().UTC()
	}
	breakdown := settledUsageCreditBreakdown(usage, credits, providerMultiplier, serviceGroupMultiplier, pricing)
	if breakdown == nil {
		breakdown = &llmUsageCreditBreakdown{UnitemizedComponent: credits}
	}
	breakdown.ProviderID = strings.TrimSpace(billingProviderID)
	if breakdown.ProviderID == "" {
		breakdown.ProviderID = strings.TrimSpace(providerID)
	}
	breakdown.ProviderMultiplier = llmpool.NormalizeCreditMultiplier(providerMultiplier)
	breakdown.ServiceGroupMultiplier = llmpool.NormalizeCreditMultiplier(serviceGroupMultiplier)

	a.mu.Lock()
	defer a.mu.Unlock()
	buf := a.pending[system]
	if buf == nil {
		buf = &pendingSystemUsage{providerUsage: map[string]corelib.TokenUsageStat{}, creditCharges: map[string]*pendingCreditCharge{}, reports: &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}}
		a.pending[system] = buf
	}
	providerID = strings.TrimSpace(providerID)
	if providerID != "" {
		curr := buf.providerUsage[providerID]
		curr.InputTokens += usage.InputTokens
		curr.OutputTokens += usage.OutputTokens
		curr.TotalTokens += usage.TotalTokens
		curr.CachedInputTokens += usage.CachedInputTokens
		curr.CacheWriteTokens += usage.CacheWriteTokens
		curr.InputCostRMB += usage.InputCostRMB
		curr.OutputCostRMB += usage.OutputCostRMB
		curr.TotalCostRMB += usage.TotalCostRMB
		curr.Requests += usage.Requests
		curr.CachedRequests += usage.CachedRequests
		buf.providerUsage[providerID] = curr
	}
	if buf.reports == nil {
		buf.reports = &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	}
	// A reservation does not retain user-group membership. Do not infer a
	// historical group from today's directory state; user and provider totals
	// remain exact, and the main report total is still reconciled to the ledger.
	buf.reports.addUsageWithCreditBreakdown(reportedAt, email, nil, usage, credits, breakdown, providerID)
}

func (a *llmUsageAccumulator) flush(ctx context.Context) {
	a.mu.Lock()
	snapshot := a.pending
	a.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
	a.mu.Unlock()
	for system, buf := range snapshot {
		if system == nil || buf == nil {
			continue
		}
		if err := flushProviderUsage(ctx, system, buf.providerUsage); err != nil {
			log.Printf("[llm-usage] flush provider usage failed: %v", err)
			a.requeue(system, buf)
			continue
		}
		if err := flushCreditCharges(ctx, system, buf.creditCharges); err != nil {
			log.Printf("[llm-usage] flush credit charges failed: %v", err)
			// The report is a projection of the actual ledger debit. Do not persist
			// its requested-Credits version when settlement failed or will be retried.
			// Requeue both facts together so their later adjustment is applied once.
			a.requeue(system, &pendingSystemUsage{creditCharges: buf.creditCharges, reports: buf.reports})
			continue
		}
		applySettledCreditAdjustments(buf.reports, buf.creditCharges)
		if err := flushLLMUsageReports(ctx, system, buf.reports); err != nil {
			log.Printf("[llm-usage] flush usage reports failed: %v", err)
			a.requeue(system, &pendingSystemUsage{reports: buf.reports})
		}
	}
}

func applySettledCreditAdjustments(reports *llmUsageReportsStore, charges map[string]*pendingCreditCharge) {
	if reports == nil {
		return
	}
	for _, charge := range charges {
		if charge == nil || strings.TrimSpace(charge.requestID) == "" {
			continue
		}
		delta := charge.credits - charge.reportedCredits
		if math.Abs(delta) <= 0.000000001 {
			continue
		}
		reportedAt := charge.reportedAt
		if reportedAt.IsZero() {
			reportedAt = time.Now()
		}
		reports.addSettledCreditAdjustment(reportedAt, charge.email, charge.userGroupIDs, charge.providerID, delta, charge.pricing != nil)
		charge.reportedCredits = charge.credits
	}
}

func (a *llmUsageAccumulator) applySettledCreditAdjustment(system store.SystemSettingsRepository, charge *pendingCreditCharge) {
	if system == nil || charge == nil || strings.TrimSpace(charge.requestID) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	buf := a.pending[system]
	if buf == nil {
		buf = &pendingSystemUsage{providerUsage: map[string]corelib.TokenUsageStat{}, creditCharges: map[string]*pendingCreditCharge{}, reports: &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}}
		a.pending[system] = buf
	}
	if buf.reports == nil {
		buf.reports = &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	}
	applySettledCreditAdjustments(buf.reports, map[string]*pendingCreditCharge{creditChargeKey(charge): charge})
}

func (a *llmUsageAccumulator) requeue(system store.SystemSettingsRepository, buf *pendingSystemUsage) {
	if system == nil || buf == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.pending[system]
	if current == nil {
		current = &pendingSystemUsage{providerUsage: map[string]corelib.TokenUsageStat{}, creditCharges: map[string]*pendingCreditCharge{}, reports: &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}}
		a.pending[system] = current
	}
	for providerID, usage := range buf.providerUsage {
		curr := current.providerUsage[providerID]
		curr.InputTokens += usage.InputTokens
		curr.OutputTokens += usage.OutputTokens
		curr.TotalTokens += usage.TotalTokens
		curr.CachedInputTokens += usage.CachedInputTokens
		curr.CacheWriteTokens += usage.CacheWriteTokens
		curr.InputCostRMB += usage.InputCostRMB
		curr.OutputCostRMB += usage.OutputCostRMB
		curr.TotalCostRMB += usage.TotalCostRMB
		curr.Requests += usage.Requests
		curr.CachedRequests += usage.CachedRequests
		current.providerUsage[providerID] = curr
	}
	for key, charge := range buf.creditCharges {
		if charge == nil {
			continue
		}
		curr := current.creditCharges[key]
		if curr == nil {
			copyCharge := *charge
			copyCharge.serviceGroupIDs = append([]string(nil), charge.serviceGroupIDs...)
			copyCharge.userGroupIDs = append([]string(nil), charge.userGroupIDs...)
			if charge.pricing != nil {
				copyPricing := *charge.pricing
				copyCharge.pricing = &copyPricing
			}
			copyCharge.meta = charge.meta
			current.creditCharges[key] = &copyCharge
			continue
		}
		if strings.TrimSpace(curr.requestID) != "" || strings.TrimSpace(charge.requestID) != "" {
			// Request-scoped charges are idempotent ledger entries and must never
			// be merged with another request while retrying a failed save.
			continue
		}
		curr.credits += charge.credits
	}
	if buf.reports != nil {
		if current.reports == nil {
			current.reports = &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
		}
		mergeLLMUsageReports(current.reports, buf.reports)
	}
}

func flushProviderUsage(ctx context.Context, system store.SystemSettingsRepository, usageMap map[string]corelib.TokenUsageStat) error {
	if len(usageMap) == 0 {
		return nil
	}
	reg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		return err
	}
	if reg.TokenUsage == nil {
		reg.TokenUsage = map[string]*corelib.TokenUsageStat{}
	}
	for providerID, usage := range usageMap {
		if strings.TrimSpace(providerID) == "" {
			continue
		}
		if isRemoteCodingToolUsageProviderID(providerID) {
			continue
		}
		stat := reg.TokenUsage[providerID]
		if stat == nil {
			stat = &corelib.TokenUsageStat{}
			reg.TokenUsage[providerID] = stat
		}
		stat.InputTokens += usage.InputTokens
		stat.OutputTokens += usage.OutputTokens
		stat.TotalTokens += usage.TotalTokens
		stat.CachedInputTokens += usage.CachedInputTokens
		stat.CacheWriteTokens += usage.CacheWriteTokens
		stat.InputPricePerMTokensRMB = usage.InputPricePerMTokensRMB
		stat.OutputPricePerMTokensRMB = usage.OutputPricePerMTokensRMB
		stat.InputCostRMB += usage.InputCostRMB
		stat.OutputCostRMB += usage.OutputCostRMB
		stat.TotalCostRMB += usage.TotalCostRMB
		stat.Requests += usage.Requests
		stat.CachedRequests += usage.CachedRequests
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, reg); err != nil {
		return err
	}
	invalidateLLMRuntimeCaches(system)
	// The provider registry above is the durable source of truth for this
	// accounting update. The legacy config is a compatibility projection; do
	// not make a successful usage write retryable solely because refreshing that
	// projection failed, or the next flush would add the same provider usage
	// again. A later successful registry update will refresh the projection.
	if err := syncLegacyHubLLMConfig(ctx, system, reg); err != nil {
		log.Printf("[llm-usage] sync legacy provider config failed after usage was saved: %v", err)
	}
	return nil
}

func flushCreditCharges(ctx context.Context, system store.SystemSettingsRepository, chargeMap map[string]*pendingCreditCharge) error {
	_, err := flushCreditChargesDetailed(ctx, system, chargeMap)
	return err
}

func flushCreditChargesDetailed(ctx context.Context, system store.SystemSettingsRepository, chargeMap map[string]*pendingCreditCharge) (map[string]bool, error) {
	settled := map[string]bool{}
	settledCredits := map[string]float64{}
	if len(chargeMap) == 0 {
		return settled, nil
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		return settled, err
	}
	referralUsageBefore := userReferralGrantUsageSnapshot(reg)
	now := time.Now().UTC()
	keys := make([]string, 0, len(chargeMap))
	for key := range chargeMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		charge := chargeMap[key]
		// A request-scoped zero charge is still a completed billing fact: release
		// its admission hold and write an immutable zero-debit ledger entry.
		// Only non-request aggregate no-ops remain skippable.
		if charge == nil || charge.credits < 0 || (charge.credits == 0 && strings.TrimSpace(charge.requestID) == "") {
			continue
		}
		if strings.TrimSpace(charge.requestID) != "" && llmservice.HasBillingRequest(reg, charge.requestID) {
			if entry, ok := llmservice.BillingLedgerEntryForRequest(reg, charge.requestID); ok {
				if err := persistLLMBillingLedger(ctx, system, entry); err != nil {
					return map[string]bool{}, err
				}
				settledCredits[key] = entry.DeductedCredits
				// The immutable ledger fact is also the only safe source for a replayed
				// request's reporting provenance. A process can resume after the debit
				// was saved but before this response-path charge was adjusted, so retain
				// every frozen pricing input rather than mixing the original debit with
				// a stale route, multiplier, or token count from the retry.
				if providerID := strings.TrimSpace(entry.ProviderID); providerID != "" {
					charge.providerID = providerID
				}
				charge.providerMultiplier = entry.ProviderMultiplier
				charge.serviceGroupMultiplier = entry.BillingGroupMultiplier
				charge.usage.InputTokens = entry.InputTokens
				charge.usage.OutputTokens = entry.OutputTokens
				charge.usage.TotalTokens = entry.InputTokens + entry.OutputTokens
				if entry.Pricing != nil {
					pricing := *entry.Pricing
					charge.pricing = &pricing
				}
			}
			continue
		}
		// Release the request's conservative admission hold in the same durable
		// registry write as its final actual-token debit. This prevents a failed
		// process between completion and settlement from permanently shrinking the
		// user's available balance, while preserving idempotency on replay.
		if strings.TrimSpace(charge.requestID) != "" {
			llmservice.ReleaseBillingReservation(reg, charge.requestID, now)
		}
		applied := llmservice.ApplyCreditUsageToRegistryForUserID(reg, charge.userID, charge.email, charge.serviceGroupIDs, charge.credits, now)
		if strings.TrimSpace(charge.requestID) != "" {
			entry := llmservice.BillingLedgerEntry{
				RequestID:              charge.requestID,
				UserID:                 charge.userID,
				Email:                  charge.email,
				ProviderID:             charge.providerID,
				ServiceGroupIDs:        charge.serviceGroupIDs,
				InputTokens:            charge.usage.InputTokens,
				OutputTokens:           charge.usage.OutputTokens,
				RequestedCredits:       charge.credits,
				DeductedCredits:        applied,
				RequestedMicrocredits:  creditsToMicrocredits(charge.credits),
				DeductedMicrocredits:   creditsToMicrocredits(applied),
				ProviderMultiplier:     charge.providerMultiplier,
				BillingGroupMultiplier: charge.serviceGroupMultiplier,
				Pricing:                charge.pricing,
				CreatedAt:              now,
			}
			llmservice.AppendBillingLedgerEntry(reg, entry)
		}
		settled[key] = true
		// Do not replace the calculated amount until the registry write below is
		// durable. A failed save must retry the same requested debit.
		settledCredits[key] = applied
	}
	if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
		// The cached registry is a mutable clone. A failed write must never leave
		// its unapplied debit in memory: a retry would otherwise see a finalized
		// request or exhausted grant that was never durable, and could project an
		// incorrect zero/partial settlement into Usage Stats.
		invalidateLLMRuntimeCaches(system)
		return map[string]bool{}, err
	}
	for key, credits := range settledCredits {
		if charge := chargeMap[key]; charge != nil {
			charge.credits = credits
		}
	}
	// The registry remains the balance authority during migration. Write the
	// SQL audit mirror only after that debit is durable; if this mirror write
	// fails, retrying the request will find the same immutable registry entry
	// and safely repair the SQLite row through INSERT OR IGNORE.
	for _, charge := range chargeMap {
		if charge == nil || strings.TrimSpace(charge.requestID) == "" {
			continue
		}
		if entry, ok := llmservice.BillingLedgerEntryForRequest(reg, charge.requestID); ok {
			if err := persistLLMBillingLedger(ctx, system, entry); err != nil {
				return map[string]bool{}, err
			}
		}
	}
	// Credit charges are already serialized and batched here. Record referral
	// consumption after the ledger save so a failed save cannot produce a metric
	// for usage that was not durable. One event per grant keeps the funnel
	// aggregate lightweight and retry-safe without writes in request handlers.
	recordReferralRewardUsageMetrics(ctx, system, reg, referralUsageBefore, now)
	for key := range settled {
		charge := chargeMap[key]
		if charge == nil || strings.TrimSpace(charge.requestID) == "" {
			continue
		}
		persistLLMUsageRecords(system, charge.providerID, charge.usage, charge.userID, charge.email, charge.serviceGroupIDs, charge.credits, charge.meta)
	}
	invalidateLLMRuntimeCaches(system)
	return settled, nil
}

type llmBillingLedgerRepositoryProvider interface {
	LLMBillingLedgerRepository() store.LLMBillingLedgerRepository
}

func persistLLMBillingLedger(ctx context.Context, system store.SystemSettingsRepository, entry llmservice.BillingLedgerEntry) error {
	provider, ok := system.(llmBillingLedgerRepositoryProvider)
	if !ok || provider == nil || provider.LLMBillingLedgerRepository() == nil {
		return nil
	}
	groups, err := json.Marshal(entry.ServiceGroupIDs)
	if err != nil {
		return err
	}
	pricing := []byte(nil)
	if entry.Pricing != nil {
		pricing, err = json.Marshal(entry.Pricing)
		if err != nil {
			return err
		}
	}
	_, err = provider.LLMBillingLedgerRepository().RecordSettlement(ctx, &store.LLMBillingSettlement{
		TenantID:               tenantIDForSystemSettings(system),
		RequestID:              entry.RequestID,
		UserID:                 entry.UserID,
		Email:                  entry.Email,
		ProviderID:             entry.ProviderID,
		ServiceGroupIDsJSON:    string(groups),
		InputTokens:            entry.InputTokens,
		OutputTokens:           entry.OutputTokens,
		RequestedMicrocredits:  entry.RequestedMicrocredits,
		DeductedMicrocredits:   entry.DeductedMicrocredits,
		ProviderMultiplier:     entry.ProviderMultiplier,
		BillingGroupMultiplier: entry.BillingGroupMultiplier,
		PricingJSON:            string(pricing),
		CreatedAt:              entry.CreatedAt,
	})
	return err
}

func creditsToMicrocredits(credits float64) int64 {
	if credits <= 0 {
		return 0
	}
	return int64(math.Round(credits * float64(llmpool.MicrocreditsPerCredit)))
}

func userReferralGrantUsageSnapshot(reg *llmservice.Registry) map[string]float64 {
	if reg == nil {
		return nil
	}
	result := make(map[string]float64)
	for _, grant := range reg.Grants {
		if grant.Source == "user_referral" && !grant.Frozen && strings.TrimSpace(grant.ID) != "" {
			result[grant.ID] = grant.CreditsUsed
		}
	}
	return result
}

func recordReferralRewardUsageMetrics(ctx context.Context, system store.SystemSettingsRepository, reg *llmservice.Registry, before map[string]float64, occurredAt time.Time) {
	if len(before) == 0 || reg == nil {
		return
	}
	repo, ok := userReferralMetricRepository(system)
	if !ok || repo == nil {
		return
	}
	tenantID := tenantIDForSystemSettings(system)
	for _, grant := range reg.Grants {
		if grant.Source != "user_referral" || grant.Frozen || strings.TrimSpace(grant.ID) == "" || grant.CreditsUsed <= before[grant.ID] {
			continue
		}
		if _, err := repo.RecordRewardMetricEvent(ctx, tenantID, grant.ID, userReferralMetricRewardUsed, occurredAt); err != nil {
			log.Printf("[user-referral] record reward usage metric: %v", err)
		}
	}
}

type userReferralMetricRepositoryProvider interface {
	UserReferralMetricRepository() store.UserReferralRepository
}

type tenantScopedSettings interface {
	TenantID() string
}

func userReferralMetricRepository(system store.SystemSettingsRepository) (store.UserReferralRepository, bool) {
	provider, ok := system.(userReferralMetricRepositoryProvider)
	if !ok {
		return nil, false
	}
	return provider.UserReferralMetricRepository(), true
}

func tenantIDForSystemSettings(system store.SystemSettingsRepository) string {
	if scoped, ok := system.(tenantScopedSettings); ok && strings.TrimSpace(scoped.TenantID()) != "" {
		return scoped.TenantID()
	}
	return store.DefaultTenantID
}

type llmUsageRepositoryProvider interface {
	LLMUsageRepository() store.LLMUsageRepository
}

func llmUsageRepository(system store.SystemSettingsRepository) (store.LLMUsageRepository, bool) {
	provider, ok := system.(llmUsageRepositoryProvider)
	if !ok || provider == nil {
		return nil, false
	}
	repo := provider.LLMUsageRepository()
	return repo, repo != nil
}

func persistLLMUsageRecords(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID, email string, serviceGroupIDs []string, credits float64, meta llmservice.OfficialForwardMeta) {
	repo, ok := llmUsageRepository(system)
	if !ok {
		return
	}
	ids := normalizeUsageStringSlice(serviceGroupIDs)
	if len(ids) == 0 {
		return
	}
	class := strings.TrimSpace(meta.WorkloadClass)
	if class == "" {
		class = llmpool.WorkloadUnclassified
	}
	classSource := strings.TrimSpace(meta.ClassSource)
	model := strings.TrimSpace(meta.ResolvedModel)
	preview := strings.TrimSpace(meta.Preview)
	now := time.Now().UTC()
	for _, groupID := range ids {
		rec := &store.LLMUsageRecord{
			TenantID:       tenantIDForSystemSettings(system),
			UserID:         strings.TrimSpace(userID),
			Email:          email,
			ProviderID:     strings.TrimSpace(providerID),
			Model:          model,
			ServiceGroupID: groupID,
			WorkloadClass:  class,
			ClassSource:    classSource,
			Preview:        preview,
			InputTokens:    usage.InputTokens,
			OutputTokens:   usage.OutputTokens,
			TotalTokens:    usage.TotalTokens,
			Credits:        credits,
			CreatedAt:      now,
		}
		if err := repo.Insert(context.Background(), rec); err != nil {
			log.Printf("[llm-usage] persist usage record failed: %v", err)
		}
	}
}

func normalizeUsageStringSlice(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
