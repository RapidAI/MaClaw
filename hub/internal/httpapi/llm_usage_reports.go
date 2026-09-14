package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	llmUsageReportsKey      = "llm_usage_reports_v1"
	llmUsageReportsVersion  = 1
	llmUsageReportsKeepDays = 366
)

// llmUsageProviderMultiplier is a settled request-pricing fact retained with
// usage statistics. A directional HubCenter route records both the provider
// multiplier and the Hub service-group multiplier. It is never inferred again
// from current settings.
type llmUsageProviderMultiplier struct {
	ProviderID       string  `json:"provider_id"`
	ProviderName     string  `json:"provider_name,omitempty"`
	Multiplier       float64 `json:"multiplier"`
	MultiplierSource string  `json:"multiplier_source"`
}

// llmUsageProviderPricing is the frozen HubCenter base price that contributed
// to a settled request. Retaining it lets the tooltip show the provider's
// configured price separately from the final, multiplied rate.
type llmUsageProviderPricing struct {
	ProviderID              string  `json:"provider_id"`
	ProviderName            string  `json:"provider_name,omitempty"`
	InputCreditsPer10K      float64 `json:"input_credits_per_10k"`
	OutputCreditsPer10K     float64 `json:"output_credits_per_10k"`
	CacheReadCreditsPer10K  float64 `json:"cache_read_credits_per_10k"`
	CacheWriteCreditsPer10K float64 `json:"cache_write_credits_per_10k"`
	InputRMBPer10K          float64 `json:"input_rmb_per_10k"`
	OutputRMBPer10K         float64 `json:"output_rmb_per_10k"`
	CacheReadRMBPer10K      float64 `json:"cache_read_rmb_per_10k"`
	CacheWriteRMBPer10K     float64 `json:"cache_write_rmb_per_10k"`
}

type llmUsageReportsStore struct {
	Version int                           `json:"version"`
	Days    map[string]*llmUsageReportDay `json:"days,omitempty"`
}

type llmUsageReportDay struct {
	Totals        llmUsageCounters                `json:"totals"`
	Users         map[string]*llmUsageReportEntry `json:"users,omitempty"`
	Groups        map[string]*llmUsageReportEntry `json:"groups,omitempty"`
	Providers     map[string]*llmUsageReportEntry `json:"providers,omitempty"`
	ServiceGroups map[string]*llmUsageReportEntry `json:"service_groups,omitempty"`
}

type llmUsageReportEntry struct {
	Totals llmUsageCounters   `json:"totals"`
	Hours  []llmUsageCounters `json:"hours,omitempty"`
}

type llmUsageCounters struct {
	InputTokens       int64            `json:"input_tokens"`
	OutputTokens      int64            `json:"output_tokens"`
	TotalTokens       int64            `json:"total_tokens"`
	CachedInputTokens int64            `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64            `json:"cache_write_tokens,omitempty"`
	CacheUsageSources map[string]int64 `json:"cache_usage_sources,omitempty"`
	PricingSources    map[string]int64 `json:"pricing_sources,omitempty"`
	UsageAnomalyCount int64            `json:"usage_anomaly_count,omitempty"`
	InputCostRMB      float64          `json:"input_cost_rmb,omitempty"`
	OutputCostRMB     float64          `json:"output_cost_rmb,omitempty"`
	CacheReadCostRMB  float64          `json:"cache_read_cost_rmb,omitempty"`
	CacheWriteCostRMB float64          `json:"cache_write_cost_rmb,omitempty"`
	TotalCostRMB      float64          `json:"total_cost_rmb,omitempty"`
	// LegacyTotalCostRMB carries aggregate-only RMB totals from report records
	// written before directional RMB components existed. It is kept as a
	// separate component so merging old and new records never erases either
	// side; the persisted/displayed TotalCostRMB is always the four directional
	// components plus this legacy component.
	LegacyTotalCostRMB float64 `json:"legacy_total_cost_rmb,omitempty"`
	// RMBPriced* identifies the settled requests for which Hub retained a
	// directional RMB pricing snapshot.  A report can contain older Credits
	// without one, so these fields keep a small reference-cost total from being
	// misread as the RMB value of every Credit in the same row.
	RMBPricedInputTokens  int64 `json:"rmb_priced_input_tokens,omitempty"`
	RMBPricedOutputTokens int64 `json:"rmb_priced_output_tokens,omitempty"`
	// Priced*Tokens are the directional denominators for credit components that
	// have a frozen provider/local-service price snapshot. They deliberately do
	// not include legacy token-only usage: mixing the two is what previously
	// produced a fictitious "credits per 10K" rate in Usage Stats.
	PricedNormalInputTokens    int64   `json:"priced_normal_input_tokens,omitempty"`
	PricedCacheReadTokens      int64   `json:"priced_cache_read_tokens,omitempty"`
	PricedCacheWriteTokens     int64   `json:"priced_cache_write_tokens,omitempty"`
	PricedOutputTokens         int64   `json:"priced_output_tokens,omitempty"`
	RMBPricedCredits           float64 `json:"rmb_priced_credits,omitempty"`
	RMBPricedRequests          int64   `json:"rmb_priced_requests,omitempty"`
	RMBPricingSnapshotRequests int64   `json:"rmb_pricing_snapshot_requests,omitempty"`
	Requests                   int64   `json:"requests"`
	CachedRequests             int64   `json:"cached_requests,omitempty"`
	Credits                    float64 `json:"credits,omitempty"`
	CreditInputComponent       float64 `json:"credit_input_component,omitempty"`
	CreditNormalInputComponent float64 `json:"credit_normal_input_component,omitempty"`
	CreditCacheReadComponent   float64 `json:"credit_cache_read_component,omitempty"`
	CreditCacheWriteComponent  float64 `json:"credit_cache_write_component,omitempty"`
	CreditOutputComponent      float64 `json:"credit_output_component,omitempty"`
	CreditMinimumAdjustment    float64 `json:"credit_minimum_adjustment,omitempty"`
	CreditRoundingAdjustment   float64 `json:"credit_rounding_adjustment,omitempty"`
	CreditUnitemizedComponent  float64 `json:"credit_unitemized_component,omitempty"`
	// Unitemized* scope the legacy token-count settlements whose Credits land
	// in CreditUnitemizedComponent (no frozen price snapshot). They let the
	// Usage Stats tooltip reconcile the itemized directional legs with the row
	// totals instead of leaving the legacy share unexplainable.
	UnitemizedRequests          int64                        `json:"unitemized_requests,omitempty"`
	UnitemizedInputTokens       int64                        `json:"unitemized_input_tokens,omitempty"`
	UnitemizedCachedInputTokens int64                        `json:"unitemized_cached_input_tokens,omitempty"`
	UnitemizedCacheWriteTokens  int64                        `json:"unitemized_cache_write_tokens,omitempty"`
	UnitemizedOutputTokens      int64                        `json:"unitemized_output_tokens,omitempty"`
	ProviderMultipliers         []llmUsageProviderMultiplier `json:"provider_multipliers,omitempty"`
	ProviderPricing             []llmUsageProviderPricing    `json:"provider_pricing,omitempty"`
}

// llmUsageCreditBreakdown preserves the settled components of a request's
// credit charge. A report may span price changes and service-group multipliers,
// so these values cannot be reconstructed from aggregate Tokens alone.
type llmUsageCreditBreakdown struct {
	InputComponent       float64
	NormalInputComponent float64
	CacheReadComponent   float64
	CacheWriteComponent  float64
	OutputComponent      float64
	MinimumAdjustment    float64
	// UnitemizedComponent is used only when a legacy token-count request has
	// no directional token price to split into input/output. It still carries
	// the exact settled debit and multiplier provenance for the audit trail.
	UnitemizedComponent float64
	// RoundingAdjustment includes the fixed-point rounding residual and any
	// post-settlement difference when a balance or period limit permits less
	// than the calculated request amount to be deducted.
	RoundingAdjustment      float64
	ProviderID              string
	ProviderMultiplier      float64
	ServiceGroupMultiplier  float64
	InputCreditsPer10K      float64
	OutputCreditsPer10K     float64
	CacheReadCreditsPer10K  float64
	CacheWriteCreditsPer10K float64
	InputRMBPer10K          float64
	OutputRMBPer10K         float64
	CacheReadRMBPer10K      float64
	CacheWriteRMBPer10K     float64
	// RMBPricingRecorded distinguishes a real, possibly zero-priced, RMB
	// snapshot from legacy token accounting that never retained RMB pricing.
	RMBPricingRecorded bool
	PricingSource      string
	// ReportServiceGroupIDs are HubCenter catalog group IDs used to line Hub
	// official-route usage up against HubCenter's per-group ledger. Local
	// third-party usage is not recorded here.
	ReportServiceGroupIDs []string
}

type llmUsageReportEntityOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type llmUsageReportRow struct {
	ID                          string                       `json:"id"`
	Name                        string                       `json:"name"`
	InputTokens                 int64                        `json:"input_tokens"`
	OutputTokens                int64                        `json:"output_tokens"`
	TotalTokens                 int64                        `json:"total_tokens"`
	CachedInputTokens           int64                        `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens            int64                        `json:"cache_write_tokens,omitempty"`
	CacheUsageSources           map[string]int64             `json:"cache_usage_sources,omitempty"`
	PricingSources              map[string]int64             `json:"pricing_sources,omitempty"`
	UsageAnomalyCount           int64                        `json:"usage_anomaly_count,omitempty"`
	InputCostRMB                float64                      `json:"input_cost_rmb,omitempty"`
	OutputCostRMB               float64                      `json:"output_cost_rmb,omitempty"`
	CacheReadCostRMB            float64                      `json:"cache_read_cost_rmb,omitempty"`
	CacheWriteCostRMB           float64                      `json:"cache_write_cost_rmb,omitempty"`
	TotalCostRMB                float64                      `json:"total_cost_rmb,omitempty"`
	RMBPricedInputTokens        int64                        `json:"rmb_priced_input_tokens,omitempty"`
	RMBPricedOutputTokens       int64                        `json:"rmb_priced_output_tokens,omitempty"`
	PricedNormalInputTokens     int64                        `json:"priced_normal_input_tokens,omitempty"`
	PricedCacheReadTokens       int64                        `json:"priced_cache_read_tokens,omitempty"`
	PricedCacheWriteTokens      int64                        `json:"priced_cache_write_tokens,omitempty"`
	PricedOutputTokens          int64                        `json:"priced_output_tokens,omitempty"`
	RMBPricedCredits            float64                      `json:"rmb_priced_credits,omitempty"`
	RMBPricedRequests           int64                        `json:"rmb_priced_requests,omitempty"`
	RMBPricingSnapshotRequests  int64                        `json:"rmb_pricing_snapshot_requests,omitempty"`
	Requests                    int64                        `json:"requests"`
	CachedRequests              int64                        `json:"cached_requests,omitempty"`
	Credits                     float64                      `json:"credits"`
	CreditInputComponent        float64                      `json:"credit_input_component,omitempty"`
	CreditNormalInputComponent  float64                      `json:"credit_normal_input_component,omitempty"`
	CreditCacheReadComponent    float64                      `json:"credit_cache_read_component,omitempty"`
	CreditCacheWriteComponent   float64                      `json:"credit_cache_write_component,omitempty"`
	CreditOutputComponent       float64                      `json:"credit_output_component,omitempty"`
	CreditMinimumAdjustment     float64                      `json:"credit_minimum_adjustment,omitempty"`
	CreditRoundingAdjustment    float64                      `json:"credit_rounding_adjustment,omitempty"`
	CreditUnitemizedComponent   float64                      `json:"credit_unitemized_component,omitempty"`
	UnitemizedRequests          int64                        `json:"unitemized_requests,omitempty"`
	UnitemizedInputTokens       int64                        `json:"unitemized_input_tokens,omitempty"`
	UnitemizedCachedInputTokens int64                        `json:"unitemized_cached_input_tokens,omitempty"`
	UnitemizedCacheWriteTokens  int64                        `json:"unitemized_cache_write_tokens,omitempty"`
	UnitemizedOutputTokens      int64                        `json:"unitemized_output_tokens,omitempty"`
	ProviderMultipliers         []llmUsageProviderMultiplier `json:"provider_multipliers,omitempty"`
	ProviderPricing             []llmUsageProviderPricing    `json:"provider_pricing,omitempty"`
	Hours                       []llmUsageCounters           `json:"hours,omitempty"`
}

type llmUsageReportResponse struct {
	Scope           string                       `json:"scope"`
	Period          string                       `json:"period"`
	Date            string                       `json:"date,omitempty"`
	Month           string                       `json:"month,omitempty"`
	SelectedEntity  string                       `json:"selected_entity,omitempty"`
	Summary         llmUsageCounters             `json:"summary"`
	Trend           []llmUsageCounters           `json:"trend,omitempty"`
	Rows            []llmUsageReportRow          `json:"rows"`
	Entities        []llmUsageReportEntityOption `json:"entities,omitempty"`
	AvailableGroups []llmUsageReportEntityOption `json:"available_groups,omitempty"`
	SystemUser      *llmUsageReportRow           `json:"system_user,omitempty"`
	GeneratedAt     time.Time                    `json:"generated_at"`
}

type llmUsageReconciliationDifference struct {
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	CachedInputTokens int64   `json:"cached_input_tokens"`
	CacheWriteTokens  int64   `json:"cache_write_tokens"`
	Requests          int64   `json:"requests"`
	Credits           float64 `json:"credits"`
	UpstreamCredits   float64 `json:"upstream_credits,omitempty"`
	// UpstreamCreditsComparable is true only when Hub's settled group
	// multiplier is uniform and every credit in the row was directional.
	// Zero upstream_credits is then a real match, not "could not compare".
	UpstreamCreditsComparable bool `json:"upstream_credits_comparable,omitempty"`
}

type llmUsageReconciliationGroup struct {
	ServiceGroupID string                            `json:"service_group_id"`
	Hub            llmUsageCounters                  `json:"hub"`
	HubCenter      *llmservice.OfficialUsageSummary  `json:"hubcenter,omitempty"`
	Difference     *llmUsageReconciliationDifference `json:"difference,omitempty"`
	Status         string                            `json:"status"`
}

type llmUsageReconciliationResponse struct {
	Date          string                            `json:"date"`
	Timezone      string                            `json:"timezone"`
	Hub           llmUsageCounters                  `json:"hub"`
	HubCenter     *llmservice.OfficialUsageSummary  `json:"hubcenter,omitempty"`
	Difference    *llmUsageReconciliationDifference `json:"difference,omitempty"`
	ServiceGroups []llmUsageReconciliationGroup     `json:"service_groups,omitempty"`
	Status        string                            `json:"status"`
	Message       string                            `json:"message,omitempty"`
	CheckedAt     time.Time                         `json:"checked_at"`
}

func (s *llmUsageReportsStore) ensureDay(day string) *llmUsageReportDay {
	if s.Days == nil {
		s.Days = map[string]*llmUsageReportDay{}
	}
	entry := s.Days[day]
	if entry == nil {
		entry = &llmUsageReportDay{}
		s.Days[day] = entry
	}
	return entry
}

func (d *llmUsageReportDay) ensureUser(email string) *llmUsageReportEntry {
	if d.Users == nil {
		d.Users = map[string]*llmUsageReportEntry{}
	}
	entry := d.Users[email]
	if entry == nil {
		entry = &llmUsageReportEntry{}
		d.Users[email] = entry
	}
	ensureHourlyCounters(entry)
	return entry
}

func (d *llmUsageReportDay) ensureGroup(groupID string) *llmUsageReportEntry {
	if d.Groups == nil {
		d.Groups = map[string]*llmUsageReportEntry{}
	}
	entry := d.Groups[groupID]
	if entry == nil {
		entry = &llmUsageReportEntry{}
		d.Groups[groupID] = entry
	}
	ensureHourlyCounters(entry)
	return entry
}

func (d *llmUsageReportDay) ensureProvider(providerID string) *llmUsageReportEntry {
	if d.Providers == nil {
		d.Providers = map[string]*llmUsageReportEntry{}
	}
	entry := d.Providers[providerID]
	if entry == nil {
		entry = &llmUsageReportEntry{}
		d.Providers[providerID] = entry
	}
	ensureHourlyCounters(entry)
	return entry
}

func (d *llmUsageReportDay) ensureServiceGroup(groupID string) *llmUsageReportEntry {
	if d.ServiceGroups == nil {
		d.ServiceGroups = map[string]*llmUsageReportEntry{}
	}
	entry := d.ServiceGroups[groupID]
	if entry == nil {
		entry = &llmUsageReportEntry{}
		d.ServiceGroups[groupID] = entry
	}
	ensureHourlyCounters(entry)
	return entry
}

func hubOfficialUsageCounters(day *llmUsageReportDay) llmUsageCounters {
	if day == nil {
		return llmUsageCounters{}
	}
	var official llmUsageCounters
	foundOfficial := false
	for id, entry := range day.Providers {
		if entry == nil || !strings.EqualFold(strings.TrimSpace(id), llmservice.MaClawOfficialProviderID) {
			continue
		}
		foundOfficial = true
		addUsageCountersFromTotals(&official, entry.Totals)
	}
	if foundOfficial {
		return official
	}
	var sum llmUsageCounters
	found := false
	for _, entry := range day.ServiceGroups {
		if entry == nil {
			continue
		}
		found = true
		addUsageCountersFromTotals(&sum, entry.Totals)
	}
	if found {
		return sum
	}
	return day.Totals
}

// usageReportServiceGroupIDs keeps one HubCenter catalog group for official-route
// reconciliation. A request may be charged to several Hub groups, but it landed
// on a single upstream group; repeating the tokens would inflate Hub's side.
func usageReportServiceGroupIDs(ids []string) []string {
	for _, id := range normalizeUsageStringSlice(ids) {
		return []string{id}
	}
	return nil
}

func ensureHourlyCounters(entry *llmUsageReportEntry) {
	if entry == nil {
		return
	}
	if len(entry.Hours) == 24 {
		return
	}
	hours := make([]llmUsageCounters, 24)
	copy(hours, entry.Hours)
	entry.Hours = hours
}

func addUsageCounters(dst *llmUsageCounters, usage corelib.TokenUsageStat, credits float64, breakdowns ...*llmUsageCreditBreakdown) {
	if dst == nil {
		return
	}
	requests := usage.Requests
	if requests <= 0 {
		requests = 1
	}
	totalTokens := usage.TotalTokens
	if totalTokens <= 0 && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
		totalTokens = usage.InputTokens + usage.OutputTokens
	}
	dst.InputTokens += usage.InputTokens
	dst.OutputTokens += usage.OutputTokens
	dst.TotalTokens += totalTokens
	dst.CachedInputTokens += usage.CachedInputTokens
	dst.CacheWriteTokens += usage.CacheWriteTokens
	if source := strings.TrimSpace(usage.CacheUsageSource); source != "" {
		if dst.CacheUsageSources == nil {
			dst.CacheUsageSources = map[string]int64{}
		}
		dst.CacheUsageSources[source] += requests
	}
	if strings.TrimSpace(usage.UsageAnomaly) != "" {
		dst.UsageAnomalyCount += requests
	}
	dst.Requests += requests
	dst.CachedRequests += usage.CachedRequests
	dst.Credits += credits
	if len(breakdowns) > 0 && breakdowns[0] != nil {
		breakdown := breakdowns[0]
		pricingSource := strings.TrimSpace(usage.PricingSource)
		if strings.TrimSpace(breakdown.PricingSource) != "" {
			pricingSource = strings.TrimSpace(breakdown.PricingSource)
		}
		if pricingSource != "" {
			if dst.PricingSources == nil {
				dst.PricingSources = map[string]int64{}
			}
			dst.PricingSources[pricingSource] += requests
		}
		dst.CreditInputComponent += breakdown.InputComponent
		dst.CreditNormalInputComponent += breakdown.NormalInputComponent
		dst.CreditCacheReadComponent += breakdown.CacheReadComponent
		dst.CreditCacheWriteComponent += breakdown.CacheWriteComponent
		dst.CreditOutputComponent += breakdown.OutputComponent
		dst.CreditMinimumAdjustment += breakdown.MinimumAdjustment
		dst.CreditRoundingAdjustment += breakdown.RoundingAdjustment
		dst.CreditUnitemizedComponent += breakdown.UnitemizedComponent
		if breakdown.UnitemizedComponent > 0 {
			// Legacy token-count settlement: scope its tokens so the tooltip can
			// reconcile itemized legs with the row totals.
			dst.UnitemizedRequests += requests
			dst.UnitemizedInputTokens += usage.InputTokens
			dst.UnitemizedCachedInputTokens += usage.CachedInputTokens
			dst.UnitemizedCacheWriteTokens += usage.CacheWriteTokens
			dst.UnitemizedOutputTokens += usage.OutputTokens
		}
		addUsageProviderMultiplier(&dst.ProviderMultipliers, breakdown.ProviderID, breakdown.ProviderMultiplier, "provider")
		addUsageProviderMultiplier(&dst.ProviderMultipliers, breakdown.ProviderID, breakdown.ServiceGroupMultiplier, "service_group")
		addUsageProviderPricing(&dst.ProviderPricing, breakdown)
		if breakdown.RMBPricingRecorded {
			// RMB is a frozen settlement reference, not a value inferred from
			// Credits. Keep its numerator in the same provenance set as the
			// coverage counters below; otherwise one legacy record carrying an old
			// non-zero cost could silently inflate a "recorded" RMB total.
			dst.InputCostRMB += usage.InputCostRMB
			dst.OutputCostRMB += usage.OutputCostRMB
			dst.CacheReadCostRMB += usage.CacheReadCostRMB
			dst.CacheWriteCostRMB += usage.CacheWriteCostRMB
			// Never accumulate a separately supplied total: the four frozen
			// components plus any legacy aggregate component are the accounting
			// source of truth. This also prevents stale totals from older report
			// records from drifting from the directional breakdown shown to
			// administrators.
			dst.TotalCostRMB = llmUsageCountersRMBCostTotal(*dst)
			dst.RMBPricedInputTokens += usage.InputTokens
			dst.RMBPricedOutputTokens += usage.OutputTokens
			normalInput, cacheRead, cacheWrite, output := settledUsagePricedTokenCounts(usage)
			dst.PricedNormalInputTokens += normalInput
			dst.PricedCacheReadTokens += cacheRead
			dst.PricedCacheWriteTokens += cacheWrite
			dst.PricedOutputTokens += output
			dst.RMBPricedCredits += credits
			dst.RMBPricedRequests += requests
			dst.RMBPricingSnapshotRequests += requests
		}
	} else {
		// Backward-compatible aggregate-only RMB records may contain only
		// TotalCostRMB. Preserve that value in the separate legacy component so
		// mixing legacy and directional records never erases either side;
		// records with directional fields are intentionally excluded because
		// they are not tied to a frozen price snapshot.
		if usage.InputCostRMB == 0 && usage.OutputCostRMB == 0 && usage.CacheReadCostRMB == 0 && usage.CacheWriteCostRMB == 0 {
			dst.LegacyTotalCostRMB += usage.TotalCostRMB
			dst.TotalCostRMB = llmUsageCountersRMBCostTotal(*dst)
		}
		if source := strings.TrimSpace(usage.PricingSource); source != "" {
			if dst.PricingSources == nil {
				dst.PricingSources = map[string]int64{}
			}
			dst.PricingSources[source] += requests
		}
		dst.CreditUnitemizedComponent += credits
		if credits != 0 {
			// Records without any breakdown (imported or recovered legacy
			// aggregates) settle as unitemized too; scope them likewise.
			dst.UnitemizedRequests += requests
			dst.UnitemizedInputTokens += usage.InputTokens
			dst.UnitemizedCachedInputTokens += usage.CachedInputTokens
			dst.UnitemizedCacheWriteTokens += usage.CacheWriteTokens
			dst.UnitemizedOutputTokens += usage.OutputTokens
		}
	}
}

func llmUsageCountersRMBCostTotal(c llmUsageCounters) float64 {
	return c.InputCostRMB + c.CacheReadCostRMB + c.CacheWriteCostRMB + c.OutputCostRMB + c.LegacyTotalCostRMB
}

func normalizeLLMUsageCountersRMBCostTotal(c *llmUsageCounters) {
	if c == nil {
		return
	}
	// New records always carry the four directional components. A report
	// record written before cache-v1 carries only an aggregate TotalCostRMB;
	// move it into the legacy component so later merges with directional
	// records cannot erase it, then keep the total as the sum of both parts.
	directional := c.InputCostRMB + c.CacheReadCostRMB + c.CacheWriteCostRMB + c.OutputCostRMB
	if c.LegacyTotalCostRMB == 0 && directional == 0 && math.Abs(c.TotalCostRMB) > 0.000000000001 {
		c.LegacyTotalCostRMB = c.TotalCostRMB
	}
	c.TotalCostRMB = directional + c.LegacyTotalCostRMB
}

// settledUsagePricedTokenCounts mirrors the billing boundary in
// TokenPricingCreditComponentsDetailedWithCache: cache read/write usage is a
// subset of input, and ordinary input is only what remains after both have
// been removed. Keep these denominators with the frozen price snapshot so the
// reporting UI never divides a priced component by unrelated legacy usage.
func settledUsagePricedTokenCounts(usage corelib.TokenUsageStat) (normalInput, cacheRead, cacheWrite, output int64) {
	input := usage.InputTokens
	if input < 0 {
		input = 0
	}
	cacheRead = usage.CachedInputTokens
	if cacheRead < 0 {
		cacheRead = 0
	}
	if cacheRead > input {
		cacheRead = input
	}
	cacheWrite = usage.CacheWriteTokens
	if cacheWrite < 0 {
		cacheWrite = 0
	}
	if cacheWrite > input-cacheRead {
		cacheWrite = input - cacheRead
	}
	normalInput = input - cacheRead - cacheWrite
	output = usage.OutputTokens
	if output < 0 {
		output = 0
	}
	return normalInput, cacheRead, cacheWrite, output
}

func addUsageProviderMultiplier(items *[]llmUsageProviderMultiplier, providerID string, multiplier float64, source string) {
	if items == nil || strings.TrimSpace(providerID) == "" || multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return
	}
	source = strings.TrimSpace(source)
	for i := range *items {
		item := &(*items)[i]
		if item.ProviderID == providerID && item.Multiplier == multiplier && item.MultiplierSource == source {
			return
		}
	}
	*items = append(*items, llmUsageProviderMultiplier{ProviderID: providerID, Multiplier: multiplier, MultiplierSource: source})
}

func addUsageProviderMultipliers(dst *[]llmUsageProviderMultiplier, src []llmUsageProviderMultiplier) {
	for _, item := range src {
		addUsageProviderMultiplier(dst, item.ProviderID, item.Multiplier, item.MultiplierSource)
	}
}

func addUsageProviderPricing(items *[]llmUsageProviderPricing, breakdown *llmUsageCreditBreakdown) {
	// A legacy debit can retain the provider identity and multiplier without a
	// directional token-pricing snapshot. Do not turn its zero-value fields
	// into a misleading "settled provider price" in the UI. A real zero-priced
	// route still has RMBPricingRecorded set and is retained faithfully.
	if items == nil || breakdown == nil || !breakdown.RMBPricingRecorded || strings.TrimSpace(breakdown.ProviderID) == "" {
		return
	}
	pricing := llmUsageProviderPricing{
		ProviderID:              strings.TrimSpace(breakdown.ProviderID),
		InputCreditsPer10K:      breakdown.InputCreditsPer10K,
		OutputCreditsPer10K:     breakdown.OutputCreditsPer10K,
		CacheReadCreditsPer10K:  breakdown.CacheReadCreditsPer10K,
		CacheWriteCreditsPer10K: breakdown.CacheWriteCreditsPer10K,
		InputRMBPer10K:          breakdown.InputRMBPer10K,
		OutputRMBPer10K:         breakdown.OutputRMBPer10K,
		CacheReadRMBPer10K:      breakdown.CacheReadRMBPer10K,
		CacheWriteRMBPer10K:     breakdown.CacheWriteRMBPer10K,
	}
	for _, existing := range *items {
		if existing == pricing {
			return
		}
	}
	*items = append(*items, pricing)
}

func cloneUsageProviderPricingItems(items []llmUsageProviderPricing) []llmUsageProviderPricing {
	if len(items) == 0 {
		return nil
	}
	return append([]llmUsageProviderPricing(nil), items...)
}

func addUsageProviderPricingItems(dst *[]llmUsageProviderPricing, src []llmUsageProviderPricing) {
	for _, pricing := range src {
		addUsageProviderPricing(dst, &llmUsageCreditBreakdown{
			ProviderID:              pricing.ProviderID,
			InputCreditsPer10K:      pricing.InputCreditsPer10K,
			OutputCreditsPer10K:     pricing.OutputCreditsPer10K,
			CacheReadCreditsPer10K:  pricing.CacheReadCreditsPer10K,
			CacheWriteCreditsPer10K: pricing.CacheWriteCreditsPer10K,
			InputRMBPer10K:          pricing.InputRMBPer10K,
			OutputRMBPer10K:         pricing.OutputRMBPer10K,
			CacheReadRMBPer10K:      pricing.CacheReadRMBPer10K,
			CacheWriteRMBPer10K:     pricing.CacheWriteRMBPer10K,
			RMBPricingRecorded:      true,
		})
	}
}

func usageProviderPricingWithNames(items []llmUsageProviderPricing, names map[string]string) []llmUsageProviderPricing {
	if len(items) == 0 {
		return nil
	}
	out := append([]llmUsageProviderPricing(nil), items...)
	for i := range out {
		if name := strings.TrimSpace(names[out[i].ProviderID]); name != "" {
			out[i].ProviderName = name
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.ProviderName != right.ProviderName {
			return left.ProviderName < right.ProviderName
		}
		if left.ProviderID != right.ProviderID {
			return left.ProviderID < right.ProviderID
		}
		if left.InputCreditsPer10K != right.InputCreditsPer10K {
			return left.InputCreditsPer10K < right.InputCreditsPer10K
		}
		return left.OutputCreditsPer10K < right.OutputCreditsPer10K
	})
	return out
}

func usageProviderMultipliersWithNames(items []llmUsageProviderMultiplier, names map[string]string) []llmUsageProviderMultiplier {
	if len(items) == 0 {
		return nil
	}
	out := append([]llmUsageProviderMultiplier(nil), items...)
	for i := range out {
		if name := strings.TrimSpace(names[out[i].ProviderID]); name != "" {
			out[i].ProviderName = name
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderName != out[j].ProviderName {
			return out[i].ProviderName < out[j].ProviderName
		}
		if out[i].ProviderID != out[j].ProviderID {
			return out[i].ProviderID < out[j].ProviderID
		}
		return out[i].Multiplier < out[j].Multiplier
	})
	return out
}

func addUsageCountersFromTotals(dst *llmUsageCounters, src llmUsageCounters) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.TotalTokens += src.TotalTokens
	dst.CachedInputTokens += src.CachedInputTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.UsageAnomalyCount += src.UsageAnomalyCount
	if len(src.CacheUsageSources) > 0 {
		if dst.CacheUsageSources == nil {
			dst.CacheUsageSources = map[string]int64{}
		}
		for source, count := range src.CacheUsageSources {
			dst.CacheUsageSources[source] += count
		}
	}
	if len(src.PricingSources) > 0 {
		if dst.PricingSources == nil {
			dst.PricingSources = map[string]int64{}
		}
		for source, count := range src.PricingSources {
			dst.PricingSources[source] += count
		}
	}
	dst.InputCostRMB += src.InputCostRMB
	dst.OutputCostRMB += src.OutputCostRMB
	dst.CacheReadCostRMB += src.CacheReadCostRMB
	dst.CacheWriteCostRMB += src.CacheWriteCostRMB
	// Merge the RMB totals by component so neither side is lost: a legacy
	// record carries only an aggregate total, a cache-v1 record carries the
	// four directional components (and possibly its own legacy component from
	// an earlier merge). The displayed total is always the sum of both parts.
	srcDirectional := src.InputCostRMB + src.CacheReadCostRMB + src.CacheWriteCostRMB + src.OutputCostRMB
	dst.LegacyTotalCostRMB += src.LegacyTotalCostRMB
	if src.LegacyTotalCostRMB == 0 && srcDirectional == 0 && math.Abs(src.TotalCostRMB) > 0.000000000001 {
		dst.LegacyTotalCostRMB += src.TotalCostRMB
	}
	dst.TotalCostRMB = llmUsageCountersRMBCostTotal(*dst)
	dst.RMBPricedInputTokens += src.RMBPricedInputTokens
	dst.RMBPricedOutputTokens += src.RMBPricedOutputTokens
	dst.PricedNormalInputTokens += src.PricedNormalInputTokens
	dst.PricedCacheReadTokens += src.PricedCacheReadTokens
	dst.PricedCacheWriteTokens += src.PricedCacheWriteTokens
	dst.PricedOutputTokens += src.PricedOutputTokens
	dst.RMBPricedCredits += src.RMBPricedCredits
	dst.RMBPricedRequests += src.RMBPricedRequests
	dst.RMBPricingSnapshotRequests += src.RMBPricingSnapshotRequests
	dst.Requests += src.Requests
	dst.CachedRequests += src.CachedRequests
	dst.Credits += src.Credits
	dst.CreditInputComponent += src.CreditInputComponent
	dst.CreditNormalInputComponent += src.CreditNormalInputComponent
	dst.CreditCacheReadComponent += src.CreditCacheReadComponent
	dst.CreditCacheWriteComponent += src.CreditCacheWriteComponent
	dst.CreditOutputComponent += src.CreditOutputComponent
	dst.CreditMinimumAdjustment += src.CreditMinimumAdjustment
	dst.CreditRoundingAdjustment += src.CreditRoundingAdjustment
	dst.CreditUnitemizedComponent += src.CreditUnitemizedComponent
	dst.UnitemizedRequests += src.UnitemizedRequests
	dst.UnitemizedInputTokens += src.UnitemizedInputTokens
	dst.UnitemizedCachedInputTokens += src.UnitemizedCachedInputTokens
	dst.UnitemizedCacheWriteTokens += src.UnitemizedCacheWriteTokens
	dst.UnitemizedOutputTokens += src.UnitemizedOutputTokens
	addUsageProviderMultipliers(&dst.ProviderMultipliers, src.ProviderMultipliers)
	addUsageProviderPricingItems(&dst.ProviderPricing, src.ProviderPricing)
}

func cloneUsageCountersSlice(items []llmUsageCounters) []llmUsageCounters {
	if len(items) == 0 {
		return nil
	}
	out := make([]llmUsageCounters, len(items))
	copy(out, items)
	for i := range out {
		out[i].CacheUsageSources = cloneUsageSourceCounts(items[i].CacheUsageSources)
		out[i].PricingSources = cloneUsageSourceCounts(items[i].PricingSources)
		out[i].ProviderMultipliers = append([]llmUsageProviderMultiplier(nil), items[i].ProviderMultipliers...)
		out[i].ProviderPricing = cloneUsageProviderPricingItems(items[i].ProviderPricing)
	}
	return out
}

func cloneUsageSourceCounts(src map[string]int64) map[string]int64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int64, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func normalizeUsageCreditComponents(counters *llmUsageCounters) {
	if counters == nil {
		return
	}
	// Detailed components introduced after the original aggregate-only report
	// format can outlive their directional token denominators.  Bound each
	// component by the highest frozen provider-route price represented in this
	// row; any excess remains visible as unitemized settlement credits instead
	// of being rendered as an impossible per-10K rate.
	boundUsageCreditComponents(counters)
	itemized := counters.CreditInputComponent + counters.CreditOutputComponent + counters.CreditMinimumAdjustment + counters.CreditRoundingAdjustment + counters.CreditUnitemizedComponent
	if difference := counters.Credits - itemized; math.Abs(difference) > 0.000000001 {
		// Earlier report versions only retained Credits. Carry that exact legacy
		// amount as an unitemized component so the displayed calculation still
		// reconciles to the reported total after an upgrade.
		counters.CreditUnitemizedComponent += difference
	}
	// Older report rows only retained aggregate Credits and token counters.
	// Once directional pricing is added, the legacy share must still expose the
	// token scope that is not covered by a frozen snapshot; otherwise the UI
	// shows a large unitemized debit with "0 tokens" beside a non-zero row.
	if counters.CreditUnitemizedComponent > 0 {
		pricedInput := counters.PricedNormalInputTokens + counters.PricedCacheReadTokens + counters.PricedCacheWriteTokens
		legacyInput := counters.InputTokens - pricedInput
		if legacyInput < 0 {
			legacyInput = 0
		}
		legacyOutput := counters.OutputTokens - counters.PricedOutputTokens
		if legacyOutput < 0 {
			legacyOutput = 0
		}
		if counters.UnitemizedInputTokens == 0 && legacyInput > 0 {
			counters.UnitemizedInputTokens = legacyInput
		}
		legacyCacheRead := counters.CachedInputTokens - counters.PricedCacheReadTokens
		if legacyCacheRead < 0 {
			legacyCacheRead = 0
		}
		if counters.UnitemizedCachedInputTokens == 0 && legacyCacheRead > 0 {
			counters.UnitemizedCachedInputTokens = legacyCacheRead
		}
		legacyCacheWrite := counters.CacheWriteTokens - counters.PricedCacheWriteTokens
		if legacyCacheWrite < 0 {
			legacyCacheWrite = 0
		}
		if counters.UnitemizedCacheWriteTokens == 0 && legacyCacheWrite > 0 {
			counters.UnitemizedCacheWriteTokens = legacyCacheWrite
		}
		if counters.UnitemizedOutputTokens == 0 && legacyOutput > 0 {
			counters.UnitemizedOutputTokens = legacyOutput
		}
		if counters.UnitemizedRequests == 0 && counters.Requests > counters.RMBPricedRequests {
			counters.UnitemizedRequests = counters.Requests - counters.RMBPricedRequests
		}
	}
}

func boundUsageCreditComponents(c *llmUsageCounters) {
	if c == nil || len(c.ProviderPricing) == 0 {
		return
	}
	maxRate := func(which string) float64 {
		best := 0.0
		for _, p := range c.ProviderPricing {
			rate := p.InputCreditsPer10K
			switch which {
			case "read":
				rate = p.CacheReadCreditsPer10K
			case "write":
				rate = p.CacheWriteCreditsPer10K
			case "output":
				rate = p.OutputCreditsPer10K
			}
			if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
				continue
			}
			provider, group := 1.0, 1.0
			for _, m := range c.ProviderMultipliers {
				if m.ProviderID != p.ProviderID {
					continue
				}
				if m.MultiplierSource == "provider" {
					provider = m.Multiplier
				}
				if m.MultiplierSource == "service_group" {
					group = m.Multiplier
				}
			}
			rate *= provider * group
			if rate > best {
				best = rate
			}
		}
		return best
	}
	capComponent := func(component *float64, tokens int64, rate float64) {
		if component == nil || *component <= 0 || tokens <= 0 || rate <= 0 {
			return
		}
		limit := float64(tokens) * rate / 10000
		if *component > limit {
			c.CreditUnitemizedComponent += *component - limit
			*component = limit
		}
	}
	normal, read, write, output := settledUsagePricedTokenCounts(corelib.TokenUsageStat{InputTokens: c.PricedNormalInputTokens + c.PricedCacheReadTokens + c.PricedCacheWriteTokens, OutputTokens: c.PricedOutputTokens, CachedInputTokens: c.PricedCacheReadTokens, CacheWriteTokens: c.PricedCacheWriteTokens})
	capComponent(&c.CreditNormalInputComponent, normal, maxRate("input"))
	capComponent(&c.CreditCacheReadComponent, read, maxRate("read"))
	capComponent(&c.CreditCacheWriteComponent, write, maxRate("write"))
	capComponent(&c.CreditOutputComponent, output, maxRate("output"))
	c.CreditInputComponent = c.CreditNormalInputComponent + c.CreditCacheReadComponent + c.CreditCacheWriteComponent
}

func normalizeLLMUsageReportCreditComponents(rep *llmUsageReportsStore) {
	if rep == nil {
		return
	}
	for _, day := range rep.Days {
		if day == nil {
			continue
		}
		normalizeUsageCreditComponents(&day.Totals)
		normalizeLLMUsageCountersRMBCostTotal(&day.Totals)
		for _, entries := range []map[string]*llmUsageReportEntry{day.Users, day.Groups, day.Providers, day.ServiceGroups} {
			for _, entry := range entries {
				if entry == nil {
					continue
				}
				normalizeUsageCreditComponents(&entry.Totals)
				normalizeLLMUsageCountersRMBCostTotal(&entry.Totals)
				for i := range entry.Hours {
					normalizeUsageCreditComponents(&entry.Hours[i])
					normalizeLLMUsageCountersRMBCostTotal(&entry.Hours[i])
				}
			}
		}
	}
}

func (s *llmUsageReportsStore) addUsage(ts time.Time, email string, userGroupIDs []string, usage corelib.TokenUsageStat, credits float64, providerIDs ...string) {
	s.addUsageWithCreditBreakdown(ts, email, userGroupIDs, usage, credits, nil, providerIDs...)
}

func (s *llmUsageReportsStore) addUsageWithCreditBreakdown(ts time.Time, email string, userGroupIDs []string, usage corelib.TokenUsageStat, credits float64, breakdown *llmUsageCreditBreakdown, providerIDs ...string) {
	if s == nil {
		return
	}
	dayKey := ts.Format("2006-01-02")
	hour := ts.Hour()
	day := s.ensureDay(dayKey)
	addUsageCounters(&day.Totals, usage, credits, breakdown)
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" {
		entry := day.ensureUser(email)
		addUsageCounters(&entry.Totals, usage, credits, breakdown)
		addUsageCounters(&entry.Hours[hour], usage, credits, breakdown)
	}
	for _, groupID := range normalizeUsageStringSlice(userGroupIDs) {
		entry := day.ensureGroup(groupID)
		addUsageCounters(&entry.Totals, usage, credits, breakdown)
		addUsageCounters(&entry.Hours[hour], usage, credits, breakdown)
	}
	providerID := ""
	if len(providerIDs) > 0 {
		providerID = strings.TrimSpace(providerIDs[0])
	}
	if providerID != "" {
		entry := day.ensureProvider(providerID)
		addUsageCounters(&entry.Totals, usage, credits, breakdown)
		addUsageCounters(&entry.Hours[hour], usage, credits, breakdown)
	}
	if breakdown != nil {
		for _, groupID := range usageReportServiceGroupIDs(breakdown.ReportServiceGroupIDs) {
			entry := day.ensureServiceGroup(groupID)
			addUsageCounters(&entry.Totals, usage, credits, breakdown)
			addUsageCounters(&entry.Hours[hour], usage, credits, breakdown)
		}
	}
}

// addSettledCreditAdjustment reconciles a request's initially calculated
// amount with the amount actually deducted by the durable credit ledger. It
// intentionally changes only Credits and its settlement/rounding component:
// the observed token usage and request count have not changed. The caller also
// supplies whether this exact request retained an RMB pricing snapshot; using
// an aggregate counter for that decision would incorrectly classify a legacy
// adjustment when one earlier request on the same row happened to be priced.
func (s *llmUsageReportsStore) addSettledCreditAdjustment(ts time.Time, email string, userGroupIDs []string, providerID string, delta float64, rmbPricingRecorded bool, serviceGroupIDs ...string) {
	if s == nil || math.Abs(delta) <= 0.000000001 {
		return
	}
	add := func(counters *llmUsageCounters) {
		counters.Credits += delta
		counters.CreditRoundingAdjustment += delta
		if rmbPricingRecorded {
			counters.RMBPricedCredits += delta
		}
	}
	day := s.ensureDay(ts.Format("2006-01-02"))
	add(&day.Totals)
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" {
		entry := day.ensureUser(email)
		add(&entry.Totals)
		add(&entry.Hours[ts.Hour()])
	}
	for _, groupID := range normalizeUsageStringSlice(userGroupIDs) {
		entry := day.ensureGroup(groupID)
		add(&entry.Totals)
		add(&entry.Hours[ts.Hour()])
	}
	if providerID = strings.TrimSpace(providerID); providerID != "" {
		entry := day.ensureProvider(providerID)
		add(&entry.Totals)
		add(&entry.Hours[ts.Hour()])
	}
	for _, groupID := range usageReportServiceGroupIDs(serviceGroupIDs) {
		entry := day.ensureServiceGroup(groupID)
		add(&entry.Totals)
		add(&entry.Hours[ts.Hour()])
	}
}

func mergeLLMUsageReports(dst *llmUsageReportsStore, src *llmUsageReportsStore) {
	if dst == nil || src == nil {
		return
	}
	for dayKey, srcDay := range src.Days {
		if srcDay == nil {
			continue
		}
		dstDay := dst.ensureDay(dayKey)
		addUsageCountersFromTotals(&dstDay.Totals, srcDay.Totals)
		for email, srcEntry := range srcDay.Users {
			if srcEntry == nil {
				continue
			}
			dstEntry := dstDay.ensureUser(email)
			addUsageCountersFromTotals(&dstEntry.Totals, srcEntry.Totals)
			for i := 0; i < len(srcEntry.Hours) && i < 24; i++ {
				addUsageCountersFromTotals(&dstEntry.Hours[i], srcEntry.Hours[i])
			}
		}
		for groupID, srcEntry := range srcDay.Groups {
			if srcEntry == nil {
				continue
			}
			dstEntry := dstDay.ensureGroup(groupID)
			addUsageCountersFromTotals(&dstEntry.Totals, srcEntry.Totals)
			for i := 0; i < len(srcEntry.Hours) && i < 24; i++ {
				addUsageCountersFromTotals(&dstEntry.Hours[i], srcEntry.Hours[i])
			}
		}
		for providerID, srcEntry := range srcDay.Providers {
			if srcEntry == nil {
				continue
			}
			dstEntry := dstDay.ensureProvider(providerID)
			addUsageCountersFromTotals(&dstEntry.Totals, srcEntry.Totals)
			for i := 0; i < len(srcEntry.Hours) && i < 24; i++ {
				addUsageCountersFromTotals(&dstEntry.Hours[i], srcEntry.Hours[i])
			}
		}
		for groupID, srcEntry := range srcDay.ServiceGroups {
			if srcEntry == nil {
				continue
			}
			dstEntry := dstDay.ensureServiceGroup(groupID)
			addUsageCountersFromTotals(&dstEntry.Totals, srcEntry.Totals)
			for i := 0; i < len(srcEntry.Hours) && i < 24; i++ {
				addUsageCountersFromTotals(&dstEntry.Hours[i], srcEntry.Hours[i])
			}
		}
	}
	pruneLLMUsageReports(dst, time.Now())
}

func pruneLLMUsageReports(rep *llmUsageReportsStore, now time.Time) {
	if rep == nil || len(rep.Days) == 0 {
		return
	}
	cutoff := now.AddDate(0, 0, -llmUsageReportsKeepDays).Format("2006-01-02")
	for dayKey := range rep.Days {
		if dayKey < cutoff {
			delete(rep.Days, dayKey)
		}
	}
}

func loadLLMUsageReports(ctx context.Context, system store.SystemSettingsRepository) (*llmUsageReportsStore, error) {
	if system == nil {
		return &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}, nil
	}
	raw, err := system.Get(ctx, llmUsageReportsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}, nil
	}
	var rep llmUsageReportsStore
	if err := json.Unmarshal([]byte(raw), &rep); err != nil {
		return nil, err
	}
	if rep.Days == nil {
		rep.Days = map[string]*llmUsageReportDay{}
	}
	if rep.Version == 0 {
		rep.Version = llmUsageReportsVersion
	}
	// Move pre-cache-v1 aggregate-only RMB totals into the legacy component at
	// load time, before any merge recomputes totals from directional parts.
	for _, day := range rep.Days {
		if day == nil {
			continue
		}
		normalizeLLMUsageCountersRMBCostTotal(&day.Totals)
		for _, entries := range []map[string]*llmUsageReportEntry{day.Users, day.Groups, day.Providers, day.ServiceGroups} {
			for _, entry := range entries {
				if entry == nil {
					continue
				}
				normalizeLLMUsageCountersRMBCostTotal(&entry.Totals)
				for i := range entry.Hours {
					normalizeLLMUsageCountersRMBCostTotal(&entry.Hours[i])
				}
			}
		}
	}
	return &rep, nil
}

func llmUsageTotalsForUser(ctx context.Context, system store.SystemSettingsRepository, email string) (llmUsageCounters, error) {
	rep, err := loadLLMUsageReports(ctx, system)
	if err != nil {
		return llmUsageCounters{}, err
	}
	var totals llmUsageCounters
	email = strings.ToLower(strings.TrimSpace(email))
	if rep == nil || email == "" {
		return totals, nil
	}
	for _, day := range rep.Days {
		if day == nil || day.Users == nil {
			continue
		}
		if entry := day.Users[email]; entry != nil {
			addUsageCountersFromTotals(&totals, entry.Totals)
		}
	}
	return totals, nil
}

func saveLLMUsageReports(ctx context.Context, system store.SystemSettingsRepository, rep *llmUsageReportsStore) error {
	if system == nil {
		return nil
	}
	if rep == nil {
		rep = &llmUsageReportsStore{}
	}
	rep.Version = llmUsageReportsVersion
	if rep.Days == nil {
		rep.Days = map[string]*llmUsageReportDay{}
	}
	pruneLLMUsageReports(rep, time.Now())
	data, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	return system.Set(ctx, llmUsageReportsKey, string(data))
}

func flushLLMUsageReports(ctx context.Context, system store.SystemSettingsRepository, pending *llmUsageReportsStore) error {
	if pending == nil || len(pending.Days) == 0 {
		return nil
	}
	rep, err := loadLLMUsageReports(ctx, system)
	if err != nil {
		return err
	}
	mergeLLMUsageReports(rep, pending)
	return saveLLMUsageReports(ctx, system, rep)
}

func flattenSecurityGroups(node *security.GroupTreeNode, path string, out *[]llmUsageReportEntityOption) {
	if node == nil {
		return
	}
	name := node.Name
	if path != "" {
		name = path + " / " + node.Name
	}
	*out = append(*out, llmUsageReportEntityOption{ID: node.ID, Name: name})
	for _, child := range node.Children {
		flattenSecurityGroups(child, name, out)
	}
}

func groupNameMap(ctx context.Context, securitySvc *security.SecurityService) map[string]string {
	out := map[string]string{}
	if securitySvc == nil {
		return out
	}
	tree, err := securitySvc.GetGroupTree(ctx)
	if err != nil || tree == nil {
		return out
	}
	items := make([]llmUsageReportEntityOption, 0)
	flattenSecurityGroups(tree, "", &items)
	for _, item := range items {
		out[item.ID] = item.Name
	}
	return out
}

func listAvailableGroups(ctx context.Context, securitySvc *security.SecurityService) []llmUsageReportEntityOption {
	if securitySvc == nil {
		return nil
	}
	tree, err := securitySvc.GetGroupTree(ctx)
	if err != nil || tree == nil {
		return nil
	}
	items := make([]llmUsageReportEntityOption, 0)
	flattenSecurityGroups(tree, "", &items)
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

func providerNameMap(ctx context.Context, system store.SystemSettingsRepository) map[string]string {
	out := map[string]string{}
	if system == nil {
		return out
	}
	reg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil || reg == nil {
		return out
	}
	for _, provider := range reg.Providers {
		id := strings.TrimSpace(provider.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = id
		}
		out[id] = name
	}
	return out
}

func normalizeUsageScope(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "group":
		return "group"
	case "provider", "llm_provider", "llm-provider":
		return "provider"
	}
	return "user"
}

func normalizeUsagePeriod(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "monthly") || strings.EqualFold(strings.TrimSpace(v), "month") {
		return "monthly"
	}
	return "daily"
}

func parseUsageDay(v string, now time.Time) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return now.Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return now.Format("2006-01-02")
	}
	return v
}

func parseUsageMonth(v string, now time.Time) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return now.Format("2006-01")
	}
	if _, err := time.Parse("2006-01", v); err != nil {
		return now.Format("2006-01")
	}
	return v
}

func collectSystemLLMUserUsage(rep *llmUsageReportsStore, period, dayKey, monthKey string) (llmUsageCounters, []llmUsageCounters) {
	var totals llmUsageCounters
	if rep == nil || rep.Days == nil {
		return totals, nil
	}
	entryFor := func(day *llmUsageReportDay) *llmUsageReportEntry {
		if day == nil || day.Users == nil {
			return nil
		}
		return day.Users[llmservice.SystemLLMUserEmail]
	}
	if period == "daily" {
		if entry := entryFor(rep.Days[dayKey]); entry != nil {
			return entry.Totals, entry.Hours
		}
		return totals, nil
	}
	for date, day := range rep.Days {
		if day == nil || !strings.HasPrefix(date, monthKey+"-") {
			continue
		}
		if entry := entryFor(day); entry != nil {
			addUsageCountersFromTotals(&totals, entry.Totals)
		}
	}
	return totals, nil
}

func buildLLMUsageReportResponse(ctx context.Context, rep *llmUsageReportsStore, securitySvc *security.SecurityService, scope, period, dayKey, monthKey, entity string, now time.Time, providerNameMaps ...map[string]string) llmUsageReportResponse {
	normalizeLLMUsageReportCreditComponents(rep)
	resp := llmUsageReportResponse{
		Scope:           scope,
		Period:          period,
		SelectedEntity:  strings.TrimSpace(entity),
		Rows:            []llmUsageReportRow{},
		Trend:           []llmUsageCounters{},
		AvailableGroups: listAvailableGroups(ctx, securitySvc),
		GeneratedAt:     now,
	}
	groupNames := groupNameMap(ctx, securitySvc)
	providerNames := map[string]string{}
	if len(providerNameMaps) > 0 && providerNameMaps[0] != nil {
		providerNames = providerNameMaps[0]
	}
	entityOptions := map[string]string{}
	makeRow := func(id string, totals llmUsageCounters, hours []llmUsageCounters) llmUsageReportRow {
		normalizeLLMUsageCountersRMBCostTotal(&totals)
		name := id
		if scope == "group" {
			if display := groupNames[id]; display != "" {
				name = display
			}
		} else if scope == "provider" {
			if display := providerNames[id]; display != "" {
				name = display
			}
		} else if scope == "user" && llmservice.IsSystemLLMUser("", id) {
			name = llmservice.SystemLLMUserEmail
		}
		rowHours := cloneUsageCountersSlice(hours)
		for i := range rowHours {
			normalizeLLMUsageCountersRMBCostTotal(&rowHours[i])
		}
		return llmUsageReportRow{
			ID:                          id,
			Name:                        name,
			InputTokens:                 totals.InputTokens,
			OutputTokens:                totals.OutputTokens,
			TotalTokens:                 totals.TotalTokens,
			CachedInputTokens:           totals.CachedInputTokens,
			CacheWriteTokens:            totals.CacheWriteTokens,
			CacheUsageSources:           cloneUsageSourceCounts(totals.CacheUsageSources),
			PricingSources:              cloneUsageSourceCounts(totals.PricingSources),
			UsageAnomalyCount:           totals.UsageAnomalyCount,
			InputCostRMB:                totals.InputCostRMB,
			OutputCostRMB:               totals.OutputCostRMB,
			CacheReadCostRMB:            totals.CacheReadCostRMB,
			CacheWriteCostRMB:           totals.CacheWriteCostRMB,
			TotalCostRMB:                totals.TotalCostRMB,
			RMBPricedInputTokens:        totals.RMBPricedInputTokens,
			RMBPricedOutputTokens:       totals.RMBPricedOutputTokens,
			PricedNormalInputTokens:     totals.PricedNormalInputTokens,
			PricedCacheReadTokens:       totals.PricedCacheReadTokens,
			PricedCacheWriteTokens:      totals.PricedCacheWriteTokens,
			PricedOutputTokens:          totals.PricedOutputTokens,
			RMBPricedCredits:            totals.RMBPricedCredits,
			RMBPricedRequests:           totals.RMBPricedRequests,
			RMBPricingSnapshotRequests:  totals.RMBPricingSnapshotRequests,
			Requests:                    totals.Requests,
			CachedRequests:              totals.CachedRequests,
			Credits:                     totals.Credits,
			CreditInputComponent:        totals.CreditInputComponent,
			CreditNormalInputComponent:  totals.CreditNormalInputComponent,
			CreditCacheReadComponent:    totals.CreditCacheReadComponent,
			CreditCacheWriteComponent:   totals.CreditCacheWriteComponent,
			CreditOutputComponent:       totals.CreditOutputComponent,
			CreditMinimumAdjustment:     totals.CreditMinimumAdjustment,
			CreditRoundingAdjustment:    totals.CreditRoundingAdjustment,
			CreditUnitemizedComponent:   totals.CreditUnitemizedComponent,
			UnitemizedRequests:          totals.UnitemizedRequests,
			UnitemizedInputTokens:       totals.UnitemizedInputTokens,
			UnitemizedCachedInputTokens: totals.UnitemizedCachedInputTokens,
			UnitemizedCacheWriteTokens:  totals.UnitemizedCacheWriteTokens,
			UnitemizedOutputTokens:      totals.UnitemizedOutputTokens,
			ProviderMultipliers:         usageProviderMultipliersWithNames(totals.ProviderMultipliers, providerNames),
			ProviderPricing:             usageProviderPricingWithNames(totals.ProviderPricing, providerNames),
			Hours:                       rowHours,
		}
	}
	addRow := func(id string, totals llmUsageCounters, hours []llmUsageCounters) {
		if strings.TrimSpace(id) == "" {
			return
		}
		row := makeRow(id, totals, hours)
		resp.Rows = append(resp.Rows, row)
		entityOptions[id] = row.Name
	}
	pinSystemUser := func() {
		if scope != "user" {
			return
		}
		totals, hours := collectSystemLLMUserUsage(rep, period, dayKey, monthKey)
		row := makeRow(llmservice.SystemLLMUserEmail, totals, hours)
		resp.SystemUser = &row
		entityOptions[llmservice.SystemLLMUserEmail] = llmservice.SystemLLMUserEmail
		if entity != "" && llmservice.IsSystemLLMUser("", entity) {
			return
		}
		kept := make([]llmUsageReportRow, 0, len(resp.Rows))
		for i := range resp.Rows {
			if llmservice.IsSystemLLMUser("", resp.Rows[i].ID) {
				continue
			}
			kept = append(kept, resp.Rows[i])
		}
		resp.Rows = kept
	}
	if rep != nil {
		if period == "daily" {
			resp.Date = dayKey
			day := rep.Days[dayKey]
			if day != nil {
				if entity != "" {
					if scope == "group" {
						if entry := day.Groups[entity]; entry != nil {
							resp.Summary = entry.Totals
							resp.Trend = cloneUsageCountersSlice(entry.Hours)
							addRow(entity, entry.Totals, entry.Hours)
						}
					} else if scope == "provider" {
						if entry := day.Providers[entity]; entry != nil {
							resp.Summary = entry.Totals
							resp.Trend = cloneUsageCountersSlice(entry.Hours)
							addRow(entity, entry.Totals, entry.Hours)
						}
					} else if entry := day.Users[strings.ToLower(entity)]; entry != nil {
						resp.Summary = entry.Totals
						resp.Trend = cloneUsageCountersSlice(entry.Hours)
						addRow(strings.ToLower(entity), entry.Totals, entry.Hours)
					}
				} else {
					if scope != "provider" {
						resp.Summary = day.Totals
					}
					resp.Trend = make([]llmUsageCounters, 24)
					if scope == "group" {
						for id, entry := range day.Groups {
							if entry == nil {
								continue
							}
							addRow(id, entry.Totals, entry.Hours)
							for i := 0; i < len(entry.Hours) && i < 24; i++ {
								addUsageCountersFromTotals(&resp.Trend[i], entry.Hours[i])
							}
						}
					} else if scope == "provider" {
						for id, entry := range day.Providers {
							if entry == nil {
								continue
							}
							addUsageCountersFromTotals(&resp.Summary, entry.Totals)
							addRow(id, entry.Totals, entry.Hours)
							for i := 0; i < len(entry.Hours) && i < 24; i++ {
								addUsageCountersFromTotals(&resp.Trend[i], entry.Hours[i])
							}
						}
					} else {
						for id, entry := range day.Users {
							if entry == nil {
								continue
							}
							addRow(id, entry.Totals, entry.Hours)
							for i := 0; i < len(entry.Hours) && i < 24; i++ {
								addUsageCountersFromTotals(&resp.Trend[i], entry.Hours[i])
							}
						}
					}
				}
			}
		} else {
			resp.Month = monthKey
			monthly := map[string]llmUsageCounters{}
			for date, day := range rep.Days {
				if day == nil || !strings.HasPrefix(date, monthKey+"-") {
					continue
				}
				if entity == "" && scope != "provider" {
					addUsageCountersFromTotals(&resp.Summary, day.Totals)
				}
				if scope == "group" {
					for id, entry := range day.Groups {
						if entry == nil {
							continue
						}
						curr := monthly[id]
						addUsageCountersFromTotals(&curr, entry.Totals)
						monthly[id] = curr
					}
				} else if scope == "provider" {
					for id, entry := range day.Providers {
						if entry == nil {
							continue
						}
						if entity == "" {
							addUsageCountersFromTotals(&resp.Summary, entry.Totals)
						}
						curr := monthly[id]
						addUsageCountersFromTotals(&curr, entry.Totals)
						monthly[id] = curr
					}
				} else {
					for id, entry := range day.Users {
						if entry == nil {
							continue
						}
						curr := monthly[id]
						addUsageCountersFromTotals(&curr, entry.Totals)
						monthly[id] = curr
					}
				}
			}
			if entity != "" {
				if totals, ok := monthly[entity]; ok {
					resp.Summary = totals
					addRow(entity, totals, nil)
				}
			} else {
				for id, totals := range monthly {
					addRow(id, totals, nil)
				}
			}
		}
	}
	normalizeLLMUsageCountersRMBCostTotal(&resp.Summary)
	for i := range resp.Trend {
		normalizeLLMUsageCountersRMBCostTotal(&resp.Trend[i])
	}
	sort.Slice(resp.Rows, func(i, j int) bool {
		if resp.Rows[i].TotalTokens == resp.Rows[j].TotalTokens {
			return strings.ToLower(resp.Rows[i].Name) < strings.ToLower(resp.Rows[j].Name)
		}
		return resp.Rows[i].TotalTokens > resp.Rows[j].TotalTokens
	})
	pinSystemUser()
	resp.Entities = make([]llmUsageReportEntityOption, 0, len(entityOptions))
	for id, name := range entityOptions {
		resp.Entities = append(resp.Entities, llmUsageReportEntityOption{ID: id, Name: name})
	}
	sort.Slice(resp.Entities, func(i, j int) bool {
		return strings.ToLower(resp.Entities[i].Name) < strings.ToLower(resp.Entities[j].Name)
	})
	resp.Summary.ProviderMultipliers = usageProviderMultipliersWithNames(resp.Summary.ProviderMultipliers, providerNames)
	resp.Summary.ProviderPricing = usageProviderPricingWithNames(resp.Summary.ProviderPricing, providerNames)
	for i := range resp.Trend {
		// Summary and rows are built from value copies, but a daily trend is a
		// clone of the persisted counters. Ensure name decoration never mutates
		// the report cache: provider display names can change between requests,
		// while persisted IDs and pricing facts must remain canonical.
		resp.Trend[i].ProviderMultipliers = append([]llmUsageProviderMultiplier(nil), resp.Trend[i].ProviderMultipliers...)
		resp.Trend[i].ProviderPricing = cloneUsageProviderPricingItems(resp.Trend[i].ProviderPricing)
		resp.Trend[i].ProviderMultipliers = usageProviderMultipliersWithNames(resp.Trend[i].ProviderMultipliers, providerNames)
		resp.Trend[i].ProviderPricing = usageProviderPricingWithNames(resp.Trend[i].ProviderPricing, providerNames)
	}
	return resp
}

func GetLLMUsageReportHandler(system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		rep, err := loadLLMUsageReports(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_USAGE_REPORT_LOAD_FAILED", err.Error())
			return
		}
		now := time.Now()
		scope := normalizeUsageScope(r.URL.Query().Get("scope"))
		period := normalizeUsagePeriod(r.URL.Query().Get("period"))
		dayKey := parseUsageDay(r.URL.Query().Get("date"), now)
		monthKey := parseUsageMonth(r.URL.Query().Get("month"), now)
		entity := strings.TrimSpace(r.URL.Query().Get("entity"))
		if scope == "user" {
			entity = strings.ToLower(entity)
		}
		resp := buildLLMUsageReportResponse(r.Context(), rep, securitySvc, scope, period, dayKey, monthKey, entity, now, providerNameMap(r.Context(), system))
		writeJSON(w, http.StatusOK, resp)
	}
}

// GetLLMUsageReconciliationHandler compares the Hub's report with the
// HubCenter ledger using the same tenant/date boundary. It is deliberately a
// separate on-demand endpoint: a remote HubCenter read must never make the
// normal Usage Stats page unavailable.
func GetLLMUsageReconciliationHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		now := time.Now()
		// HubCenter buckets its ledger days in Asia/Shanghai, so interpret the
		// requested date in that same timezone. Note the Hub report's day
		// buckets are still cut in the Hub server's local zone at write time;
		// on a host outside +08:00 the two calendars can disagree around
		// midnight and surface as a boundary mismatch. If the host lacks tzdata
		// the date falls back to local time as well.
		const timezone = "Asia/Shanghai"
		if loc, err := time.LoadLocation(timezone); err == nil {
			now = now.In(loc)
		}
		date := parseUsageDay(r.URL.Query().Get("date"), now)
		response := llmUsageReconciliationResponse{
			Date: date, Timezone: timezone, Status: "unavailable", CheckedAt: now.UTC(),
		}
		reports, err := loadLLMUsageReports(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_USAGE_REPORT_LOAD_FAILED", err.Error())
			return
		}
		var hubServiceGroups map[string]*llmUsageReportEntry
		if reports != nil && reports.Days[date] != nil {
			day := reports.Days[date]
			// HubCenter reconciliation is scoped to the official route. The
			// report's global total may also contain local providers, which would
			// create a false mismatch even though the upstream ledger is exact.
			response.Hub = hubOfficialUsageCounters(day)
			hubServiceGroups = day.ServiceGroups
		}
		tenantID := store.TenantIDFromContext(r.Context())
		upstream, status, err := ReconcileMaClawUsage(r.Context(), tenantID, date, timezone)
		if err != nil {
			response.Message = err.Error()
			writeJSON(w, http.StatusOK, response)
			return
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			response.Message = fmt.Sprintf("HubCenter returned HTTP %d", status)
			writeJSON(w, http.StatusOK, response)
			return
		}
		response.HubCenter = &upstream.Upstream
		response.Difference = usageReconciliationDifference(response.Hub, upstream.Upstream)
		response.ServiceGroups = usageReconciliationServiceGroups(hubServiceGroups, upstream.ServiceGroups)
		response.Status = usageReconciliationDayStatus(response.Difference)
		writeJSON(w, http.StatusOK, response)
	}
}

func usageReconciliationHubRequests(hub llmUsageCounters) int64 {
	// Local full-response cache hits are served without contacting the
	// upstream, so HubCenter's ledger never sees them. Exclude them from the
	// request-count comparison; every other recorded request did reach the
	// official route upstream.
	cached := hub.CacheUsageSources["local_cache"]
	if cached <= 0 {
		return hub.Requests
	}
	if cached >= hub.Requests {
		return 0
	}
	return hub.Requests - cached
}

func usageReconciliationDayStatus(diff *llmUsageReconciliationDifference) string {
	if usageReconciliationTokenMismatch(diff) {
		return "mismatch"
	}
	return "matched"
}

func usageReconciliationUpstreamCredits(hub llmUsageCounters) (float64, bool) {
	if hub.UnitemizedRequests > 0 || math.Abs(hub.CreditUnitemizedComponent) > 0.000000001 {
		return 0, false
	}
	groupMul := 0.0
	for _, item := range hub.ProviderMultipliers {
		if strings.TrimSpace(item.MultiplierSource) != "service_group" {
			continue
		}
		mul := llmpool.NormalizeCreditMultiplier(item.Multiplier)
		if mul <= 0 {
			return 0, false
		}
		if groupMul == 0 {
			groupMul = mul
			continue
		}
		if math.Abs(groupMul-mul) > 0.000000001 {
			return 0, false
		}
	}
	if groupMul <= 0 {
		return 0, false
	}
	return hub.Credits / groupMul, true
}

func usageReconciliationDifference(hub llmUsageCounters, upstream llmservice.OfficialUsageSummary) *llmUsageReconciliationDifference {
	diff := &llmUsageReconciliationDifference{
		InputTokens:       hub.InputTokens - upstream.InputTokens,
		OutputTokens:      hub.OutputTokens - upstream.OutputTokens,
		CachedInputTokens: hub.CachedInputTokens - upstream.CachedInputTokens,
		CacheWriteTokens:  hub.CacheWriteTokens - upstream.CacheWriteTokens,
		Requests:          usageReconciliationHubRequests(hub) - upstream.TotalRequests,
		Credits:           hub.Credits - upstream.TotalCredits,
	}
	if upstreamCredits, ok := usageReconciliationUpstreamCredits(hub); ok {
		diff.UpstreamCredits = upstreamCredits - upstream.TotalCredits
		diff.UpstreamCreditsComparable = true
	}
	return diff
}

func usageReconciliationTokenMismatch(diff *llmUsageReconciliationDifference) bool {
	if diff == nil {
		return false
	}
	return diff.InputTokens != 0 || diff.OutputTokens != 0 || diff.CachedInputTokens != 0 || diff.CacheWriteTokens != 0 || diff.Requests != 0
}

func usageReconciliationCreditMismatch(diff *llmUsageReconciliationDifference) bool {
	return diff != nil && diff.UpstreamCreditsComparable && math.Abs(diff.UpstreamCredits) > 0.0005
}

func usageReconciliationServiceGroups(hubGroups map[string]*llmUsageReportEntry, upstream []llmservice.OfficialUsageSummary) []llmUsageReconciliationGroup {
	if len(hubGroups) == 0 && len(upstream) == 0 {
		return nil
	}
	type pair struct {
		id       string
		hub      llmUsageCounters
		upstream *llmservice.OfficialUsageSummary
	}
	byID := map[string]*pair{}
	order := make([]string, 0)
	add := func(id string) *pair {
		id = strings.TrimSpace(id)
		key := strings.ToLower(id)
		if cur, ok := byID[key]; ok {
			return cur
		}
		cur := &pair{id: id}
		byID[key] = cur
		order = append(order, key)
		return cur
	}
	hubHasGroups := false
	for id, entry := range hubGroups {
		if entry == nil {
			continue
		}
		hubHasGroups = true
		cur := add(id)
		addUsageCountersFromTotals(&cur.hub, entry.Totals)
		if cur.id == "" {
			cur.id = strings.TrimSpace(id)
		}
	}
	for i := range upstream {
		row := upstream[i]
		cur := add(row.ServiceGroupID)
		if cur.upstream == nil {
			copyRow := row
			cur.upstream = &copyRow
		} else {
			addOfficialUsageSummary(cur.upstream, row)
		}
		if cur.id == "" {
			cur.id = strings.TrimSpace(row.ServiceGroupID)
		}
	}
	sort.Strings(order)
	out := make([]llmUsageReconciliationGroup, 0, len(order))
	for _, key := range order {
		cur := byID[key]
		if cur == nil {
			continue
		}
		group := llmUsageReconciliationGroup{ServiceGroupID: cur.id, Hub: cur.hub, Status: "matched"}
		if cur.upstream != nil {
			group.HubCenter = cur.upstream
		}
		if !hubHasGroups {
			// Older Hub report days have no per-catalog-group buckets. Surface
			// HubCenter's split without treating Hub=0 as a ledger miss.
			group.Status = "unavailable"
			out = append(out, group)
			continue
		}
		upstream := llmservice.OfficialUsageSummary{}
		if cur.upstream != nil {
			upstream = *cur.upstream
		}
		group.Difference = usageReconciliationDifference(cur.hub, upstream)
		if usageReconciliationTokenMismatch(group.Difference) || usageReconciliationCreditMismatch(group.Difference) {
			group.Status = "mismatch"
		}
		out = append(out, group)
	}
	return out
}

func addOfficialUsageSummary(dst *llmservice.OfficialUsageSummary, src llmservice.OfficialUsageSummary) {
	if dst == nil {
		return
	}
	if dst.HubID == "" {
		dst.HubID = src.HubID
	}
	if dst.TenantID == "" {
		dst.TenantID = src.TenantID
	}
	if dst.Period == "" {
		dst.Period = src.Period
	}
	if dst.PeriodStart == "" {
		dst.PeriodStart = src.PeriodStart
	}
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CachedInputTokens += src.CachedInputTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.TotalCredits += src.TotalCredits
	dst.TotalRequests += src.TotalRequests
	dst.CacheHits += src.CacheHits
	if dst.TotalRequests > 0 {
		dst.CacheHitRate = float64(dst.CacheHits) / float64(dst.TotalRequests)
	}
}

func formatUsageReportError(scope, period string) error {
	return fmt.Errorf("invalid usage report request: scope=%s period=%s", scope, period)
}
