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
	userID          string
	email           string
	serviceGroupIDs []string
	credits         float64
	requestID       string
	providerID      string
	usage           corelib.TokenUsageStat
	multiplier      float64
	pricing         *llmpool.ResolvedTokenPricing
	meta            llmservice.OfficialForwardMeta
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
	enqueueLLMUsageRecordWithBilling(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, meta, "", 0, nil)
}

func enqueueLLMUsageRecordWithBilling(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID string, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64, meta llmservice.OfficialForwardMeta, requestID string, multiplier float64, pricing *llmpool.ResolvedTokenPricing) {
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
	charge := globalLLMUsageAccumulator.enqueue(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits, requestID)
	if charge == nil {
		return
	}
	charge.requestID = strings.TrimSpace(requestID)
	charge.providerID = strings.TrimSpace(providerID)
	charge.usage = usage
	charge.multiplier = multiplier
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

func (a *llmUsageAccumulator) enqueue(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID string, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64, requestID string) *pendingCreditCharge {
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
		charge = &pendingCreditCharge{userID: userID, email: email, serviceGroupIDs: append([]string(nil), serviceGroupIDs...), credits: credits}
	}
	if buf.reports == nil {
		buf.reports = &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	}
	buf.reports.addUsage(time.Now(), email, userGroupIDs, usage, credits, providerID)
	return charge
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
			a.requeue(system, &pendingSystemUsage{creditCharges: buf.creditCharges})
		}
		if err := flushLLMUsageReports(ctx, system, buf.reports); err != nil {
			log.Printf("[llm-usage] flush usage reports failed: %v", err)
			a.requeue(system, &pendingSystemUsage{reports: buf.reports})
		}
	}
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
	return syncLegacyHubLLMConfig(ctx, system, reg)
}

func flushCreditCharges(ctx context.Context, system store.SystemSettingsRepository, chargeMap map[string]*pendingCreditCharge) error {
	_, err := flushCreditChargesDetailed(ctx, system, chargeMap)
	return err
}

func flushCreditChargesDetailed(ctx context.Context, system store.SystemSettingsRepository, chargeMap map[string]*pendingCreditCharge) (map[string]bool, error) {
	settled := map[string]bool{}
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
				BillingGroupMultiplier: charge.multiplier,
				Pricing:                charge.pricing,
				CreatedAt:              now,
			}
			llmservice.AppendBillingLedgerEntry(reg, entry)
		}
		settled[key] = true
	}
	if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
		return map[string]bool{}, err
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
