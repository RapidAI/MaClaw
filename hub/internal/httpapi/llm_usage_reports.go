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
	"github.com/RapidAI/CodeClaw/hub/internal/im"
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
	ProviderID          string  `json:"provider_id"`
	ProviderName        string  `json:"provider_name,omitempty"`
	InputCreditsPer10K  float64 `json:"input_credits_per_10k"`
	OutputCreditsPer10K float64 `json:"output_credits_per_10k"`
	InputRMBPer10K      float64 `json:"input_rmb_per_10k"`
	OutputRMBPer10K     float64 `json:"output_rmb_per_10k"`
}

type llmUsageReportsStore struct {
	Version int                           `json:"version"`
	Days    map[string]*llmUsageReportDay `json:"days,omitempty"`
}

type llmUsageReportDay struct {
	Totals    llmUsageCounters                `json:"totals"`
	Users     map[string]*llmUsageReportEntry `json:"users,omitempty"`
	Groups    map[string]*llmUsageReportEntry `json:"groups,omitempty"`
	Providers map[string]*llmUsageReportEntry `json:"providers,omitempty"`
}

type llmUsageReportEntry struct {
	Totals llmUsageCounters   `json:"totals"`
	Hours  []llmUsageCounters `json:"hours,omitempty"`
}

type llmUsageCounters struct {
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	CachedInputTokens int64   `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64   `json:"cache_write_tokens,omitempty"`
	InputCostRMB      float64 `json:"input_cost_rmb,omitempty"`
	OutputCostRMB     float64 `json:"output_cost_rmb,omitempty"`
	TotalCostRMB      float64 `json:"total_cost_rmb,omitempty"`
	// RMBPriced* identifies the settled requests for which Hub retained a
	// directional RMB pricing snapshot.  A report can contain older Credits
	// without one, so these fields keep a small reference-cost total from being
	// misread as the RMB value of every Credit in the same row.
	RMBPricedInputTokens       int64                        `json:"rmb_priced_input_tokens,omitempty"`
	RMBPricedOutputTokens      int64                        `json:"rmb_priced_output_tokens,omitempty"`
	RMBPricedCredits           float64                      `json:"rmb_priced_credits,omitempty"`
	RMBPricedRequests          int64                        `json:"rmb_priced_requests,omitempty"`
	RMBPricingSnapshotRequests int64                        `json:"rmb_pricing_snapshot_requests,omitempty"`
	Requests                   int64                        `json:"requests"`
	CachedRequests             int64                        `json:"cached_requests,omitempty"`
	Credits                    float64                      `json:"credits,omitempty"`
	CreditInputComponent       float64                      `json:"credit_input_component,omitempty"`
	CreditOutputComponent      float64                      `json:"credit_output_component,omitempty"`
	CreditMinimumAdjustment    float64                      `json:"credit_minimum_adjustment,omitempty"`
	CreditRoundingAdjustment   float64                      `json:"credit_rounding_adjustment,omitempty"`
	CreditUnitemizedComponent  float64                      `json:"credit_unitemized_component,omitempty"`
	ProviderMultipliers        []llmUsageProviderMultiplier `json:"provider_multipliers,omitempty"`
	ProviderPricing            []llmUsageProviderPricing    `json:"provider_pricing,omitempty"`
}

// llmUsageCreditBreakdown preserves the settled components of a request's
// credit charge. A report may span price changes and service-group multipliers,
// so these values cannot be reconstructed from aggregate Tokens alone.
type llmUsageCreditBreakdown struct {
	InputComponent    float64
	OutputComponent   float64
	MinimumAdjustment float64
	// UnitemizedComponent is used only when a legacy token-count request has
	// no directional token price to split into input/output. It still carries
	// the exact settled debit and multiplier provenance for the audit trail.
	UnitemizedComponent float64
	// RoundingAdjustment includes the fixed-point rounding residual and any
	// post-settlement difference when a balance or period limit permits less
	// than the calculated request amount to be deducted.
	RoundingAdjustment     float64
	ProviderID             string
	ProviderMultiplier     float64
	ServiceGroupMultiplier float64
	InputCreditsPer10K     float64
	OutputCreditsPer10K    float64
	InputRMBPer10K         float64
	OutputRMBPer10K        float64
	// RMBPricingRecorded distinguishes a real, possibly zero-priced, RMB
	// snapshot from legacy token accounting that never retained RMB pricing.
	RMBPricingRecorded bool
}

type llmUsageReportEntityOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type llmUsageReportRow struct {
	ID                         string                       `json:"id"`
	Name                       string                       `json:"name"`
	InputTokens                int64                        `json:"input_tokens"`
	OutputTokens               int64                        `json:"output_tokens"`
	TotalTokens                int64                        `json:"total_tokens"`
	CachedInputTokens          int64                        `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens           int64                        `json:"cache_write_tokens,omitempty"`
	InputCostRMB               float64                      `json:"input_cost_rmb,omitempty"`
	OutputCostRMB              float64                      `json:"output_cost_rmb,omitempty"`
	TotalCostRMB               float64                      `json:"total_cost_rmb,omitempty"`
	RMBPricedInputTokens       int64                        `json:"rmb_priced_input_tokens,omitempty"`
	RMBPricedOutputTokens      int64                        `json:"rmb_priced_output_tokens,omitempty"`
	RMBPricedCredits           float64                      `json:"rmb_priced_credits,omitempty"`
	RMBPricedRequests          int64                        `json:"rmb_priced_requests,omitempty"`
	RMBPricingSnapshotRequests int64                        `json:"rmb_pricing_snapshot_requests,omitempty"`
	Requests                   int64                        `json:"requests"`
	CachedRequests             int64                        `json:"cached_requests,omitempty"`
	Credits                    float64                      `json:"credits"`
	CreditInputComponent       float64                      `json:"credit_input_component,omitempty"`
	CreditOutputComponent      float64                      `json:"credit_output_component,omitempty"`
	CreditMinimumAdjustment    float64                      `json:"credit_minimum_adjustment,omitempty"`
	CreditRoundingAdjustment   float64                      `json:"credit_rounding_adjustment,omitempty"`
	CreditUnitemizedComponent  float64                      `json:"credit_unitemized_component,omitempty"`
	ProviderMultipliers        []llmUsageProviderMultiplier `json:"provider_multipliers,omitempty"`
	ProviderPricing            []llmUsageProviderPricing    `json:"provider_pricing,omitempty"`
	Hours                      []llmUsageCounters           `json:"hours,omitempty"`
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
	GeneratedAt     time.Time                    `json:"generated_at"`
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
	dst.InputTokens += usage.InputTokens
	dst.OutputTokens += usage.OutputTokens
	dst.TotalTokens += usage.TotalTokens
	dst.CachedInputTokens += usage.CachedInputTokens
	dst.CacheWriteTokens += usage.CacheWriteTokens
	dst.Requests += requests
	dst.CachedRequests += usage.CachedRequests
	dst.Credits += credits
	if len(breakdowns) > 0 && breakdowns[0] != nil {
		breakdown := breakdowns[0]
		dst.CreditInputComponent += breakdown.InputComponent
		dst.CreditOutputComponent += breakdown.OutputComponent
		dst.CreditMinimumAdjustment += breakdown.MinimumAdjustment
		dst.CreditRoundingAdjustment += breakdown.RoundingAdjustment
		dst.CreditUnitemizedComponent += breakdown.UnitemizedComponent
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
			dst.TotalCostRMB += usage.TotalCostRMB
			dst.RMBPricedInputTokens += usage.InputTokens
			dst.RMBPricedOutputTokens += usage.OutputTokens
			dst.RMBPricedCredits += credits
			dst.RMBPricedRequests += requests
			dst.RMBPricingSnapshotRequests += requests
		}
	} else {
		dst.CreditUnitemizedComponent += credits
	}
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
		ProviderID:          strings.TrimSpace(breakdown.ProviderID),
		InputCreditsPer10K:  breakdown.InputCreditsPer10K,
		OutputCreditsPer10K: breakdown.OutputCreditsPer10K,
		InputRMBPer10K:      breakdown.InputRMBPer10K,
		OutputRMBPer10K:     breakdown.OutputRMBPer10K,
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
			ProviderID:          pricing.ProviderID,
			InputCreditsPer10K:  pricing.InputCreditsPer10K,
			OutputCreditsPer10K: pricing.OutputCreditsPer10K,
			InputRMBPer10K:      pricing.InputRMBPer10K,
			OutputRMBPer10K:     pricing.OutputRMBPer10K,
			RMBPricingRecorded:  true,
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
	dst.InputCostRMB += src.InputCostRMB
	dst.OutputCostRMB += src.OutputCostRMB
	dst.TotalCostRMB += src.TotalCostRMB
	dst.RMBPricedInputTokens += src.RMBPricedInputTokens
	dst.RMBPricedOutputTokens += src.RMBPricedOutputTokens
	dst.RMBPricedCredits += src.RMBPricedCredits
	dst.RMBPricedRequests += src.RMBPricedRequests
	dst.RMBPricingSnapshotRequests += src.RMBPricingSnapshotRequests
	dst.Requests += src.Requests
	dst.CachedRequests += src.CachedRequests
	dst.Credits += src.Credits
	dst.CreditInputComponent += src.CreditInputComponent
	dst.CreditOutputComponent += src.CreditOutputComponent
	dst.CreditMinimumAdjustment += src.CreditMinimumAdjustment
	dst.CreditRoundingAdjustment += src.CreditRoundingAdjustment
	dst.CreditUnitemizedComponent += src.CreditUnitemizedComponent
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
		out[i].ProviderMultipliers = append([]llmUsageProviderMultiplier(nil), items[i].ProviderMultipliers...)
		out[i].ProviderPricing = cloneUsageProviderPricingItems(items[i].ProviderPricing)
	}
	return out
}

func normalizeUsageCreditComponents(counters *llmUsageCounters) {
	if counters == nil {
		return
	}
	itemized := counters.CreditInputComponent + counters.CreditOutputComponent + counters.CreditMinimumAdjustment + counters.CreditRoundingAdjustment + counters.CreditUnitemizedComponent
	if difference := counters.Credits - itemized; math.Abs(difference) > 0.000000001 {
		// Earlier report versions only retained Credits. Carry that exact legacy
		// amount as an unitemized component so the displayed calculation still
		// reconciles to the reported total after an upgrade.
		counters.CreditUnitemizedComponent += difference
	}
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
		for _, entries := range []map[string]*llmUsageReportEntry{day.Users, day.Groups, day.Providers} {
			for _, entry := range entries {
				if entry == nil {
					continue
				}
				normalizeUsageCreditComponents(&entry.Totals)
				for i := range entry.Hours {
					normalizeUsageCreditComponents(&entry.Hours[i])
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
}

// addSettledCreditAdjustment reconciles a request's initially calculated
// amount with the amount actually deducted by the durable credit ledger. It
// intentionally changes only Credits and its settlement/rounding component:
// the observed token usage and request count have not changed. The caller also
// supplies whether this exact request retained an RMB pricing snapshot; using
// an aggregate counter for that decision would incorrectly classify a legacy
// adjustment when one earlier request on the same row happened to be priced.
func (s *llmUsageReportsStore) addSettledCreditAdjustment(ts time.Time, email string, userGroupIDs []string, providerID string, delta float64, rmbPricingRecorded bool) {
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
	if rep == nil {
		return resp
	}
	addRow := func(id string, totals llmUsageCounters, hours []llmUsageCounters) {
		if strings.TrimSpace(id) == "" {
			return
		}
		name := id
		if scope == "group" {
			if display := groupNames[id]; display != "" {
				name = display
			}
		} else if scope == "provider" {
			if display := providerNames[id]; display != "" {
				name = display
			}
		}
		resp.Rows = append(resp.Rows, llmUsageReportRow{
			ID:                         id,
			Name:                       name,
			InputTokens:                totals.InputTokens,
			OutputTokens:               totals.OutputTokens,
			TotalTokens:                totals.TotalTokens,
			CachedInputTokens:          totals.CachedInputTokens,
			CacheWriteTokens:           totals.CacheWriteTokens,
			InputCostRMB:               totals.InputCostRMB,
			OutputCostRMB:              totals.OutputCostRMB,
			TotalCostRMB:               totals.TotalCostRMB,
			RMBPricedInputTokens:       totals.RMBPricedInputTokens,
			RMBPricedOutputTokens:      totals.RMBPricedOutputTokens,
			RMBPricedCredits:           totals.RMBPricedCredits,
			RMBPricedRequests:          totals.RMBPricedRequests,
			RMBPricingSnapshotRequests: totals.RMBPricingSnapshotRequests,
			Requests:                   totals.Requests,
			CachedRequests:             totals.CachedRequests,
			Credits:                    totals.Credits,
			CreditInputComponent:       totals.CreditInputComponent,
			CreditOutputComponent:      totals.CreditOutputComponent,
			CreditMinimumAdjustment:    totals.CreditMinimumAdjustment,
			CreditRoundingAdjustment:   totals.CreditRoundingAdjustment,
			CreditUnitemizedComponent:  totals.CreditUnitemizedComponent,
			ProviderMultipliers:        usageProviderMultipliersWithNames(totals.ProviderMultipliers, providerNames),
			ProviderPricing:            usageProviderPricingWithNames(totals.ProviderPricing, providerNames),
			Hours:                      cloneUsageCountersSlice(hours),
		})
		entityOptions[id] = name
	}
	if period == "daily" {
		resp.Date = dayKey
		day := rep.Days[dayKey]
		if day == nil {
			return resp
		}
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
	sort.Slice(resp.Rows, func(i, j int) bool {
		if resp.Rows[i].TotalTokens == resp.Rows[j].TotalTokens {
			return strings.ToLower(resp.Rows[i].Name) < strings.ToLower(resp.Rows[j].Name)
		}
		return resp.Rows[i].TotalTokens > resp.Rows[j].TotalTokens
	})
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

func formatUsageReportError(scope, period string) error {
	return fmt.Errorf("invalid usage report request: scope=%s period=%s", scope, period)
}
