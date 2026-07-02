package httpapi

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	if system == nil {
		return
	}
	if isRemoteCodingToolUsageProviderID(providerID) {
		log.Printf("[llm-usage] ignoring remote coding tool provider %q; remote tool tokens are session diagnostics, not Hub LLM usage", providerID)
		return
	}
	globalLLMUsageAccumulator.start()
	charge := globalLLMUsageAccumulator.enqueue(system, providerID, usage, userID, email, serviceGroupIDs, userGroupIDs, credits)
	if charge == nil {
		return
	}
	if err := flushCreditCharges(context.Background(), system, map[string]*pendingCreditCharge{"immediate": charge}); err != nil {
		log.Printf("[llm-usage] immediate credit charge failed: %v", err)
		globalLLMUsageAccumulator.requeue(system, &pendingSystemUsage{creditCharges: map[string]*pendingCreditCharge{"immediate": charge}})
	}
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

func (a *llmUsageAccumulator) enqueue(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, userID string, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64) *pendingCreditCharge {
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
	if (userID != "" || email != "") && len(serviceGroupIDs) > 0 && credits > 0 {
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
			current.creditCharges[key] = &pendingCreditCharge{userID: charge.userID, email: charge.email, serviceGroupIDs: append([]string(nil), charge.serviceGroupIDs...), credits: charge.credits}
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
	if len(chargeMap) == 0 {
		return nil
	}
	llmCreditChargeMu.Lock()
	defer llmCreditChargeMu.Unlock()
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	keys := make([]string, 0, len(chargeMap))
	for key := range chargeMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		charge := chargeMap[key]
		if charge == nil || charge.credits <= 0 {
			continue
		}
		llmservice.ApplyCreditUsageToRegistryForUserID(reg, charge.userID, charge.email, charge.serviceGroupIDs, charge.credits, now)
	}
	if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
		return err
	}
	invalidateLLMRuntimeCaches(system)
	return nil
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
