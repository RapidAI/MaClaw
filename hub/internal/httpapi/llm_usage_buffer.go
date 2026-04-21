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

func enqueueLLMUsage(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64) {
	if system == nil {
		return
	}
	globalLLMUsageAccumulator.start()
	globalLLMUsageAccumulator.enqueue(system, providerID, usage, email, serviceGroupIDs, userGroupIDs, credits)
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

func (a *llmUsageAccumulator) enqueue(system store.SystemSettingsRepository, providerID string, usage corelib.TokenUsageStat, email string, serviceGroupIDs []string, userGroupIDs []string, credits float64) {
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
		curr.Requests += usage.Requests
		curr.CachedRequests += usage.CachedRequests
		buf.providerUsage[providerID] = curr
	}
	serviceGroupIDs = normalizeUsageStringSlice(serviceGroupIDs)
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" && len(serviceGroupIDs) > 0 && credits > 0 {
		key := email + "|" + strings.Join(serviceGroupIDs, ",")
		charge := buf.creditCharges[key]
		if charge == nil {
			charge = &pendingCreditCharge{email: email, serviceGroupIDs: append([]string(nil), serviceGroupIDs...)}
			buf.creditCharges[key] = charge
		}
		charge.credits += credits
	}
	if buf.reports == nil {
		buf.reports = &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	}
	buf.reports.addUsage(time.Now(), email, userGroupIDs, usage, credits)
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
			current.creditCharges[key] = &pendingCreditCharge{email: charge.email, serviceGroupIDs: append([]string(nil), charge.serviceGroupIDs...), credits: charge.credits}
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
		stat.Requests += usage.Requests
		stat.CachedRequests += usage.CachedRequests
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, reg); err != nil {
		return err
	}
	return syncLegacyHubLLMConfig(ctx, system, reg)
}

func flushCreditCharges(ctx context.Context, system store.SystemSettingsRepository, chargeMap map[string]*pendingCreditCharge) error {
	if len(chargeMap) == 0 {
		return nil
	}
	reg, err := llmservice.LoadRegistry(ctx, system)
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
		llmservice.ApplyCreditUsageToRegistry(reg, charge.email, charge.serviceGroupIDs, charge.credits, now)
	}
	return llmservice.SaveRegistry(ctx, system, reg)
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
