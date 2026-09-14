package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type failOnceUsageReportSettingsRepo struct {
	testSystemSettingsRepo
	failRegistry bool
}

func (r *failOnceUsageReportSettingsRepo) Set(ctx context.Context, key, valueJSON string) error {
	if r.failRegistry && key == llmservice.RegistryKey {
		r.failRegistry = false
		return errors.New("registry save failed")
	}
	return r.testSystemSettingsRepo.Set(ctx, key, valueJSON)
}

type failOnceUsageProviderLegacySyncRepo struct {
	testSystemSettingsRepo
	failLegacySync bool
}

func (r *failOnceUsageProviderLegacySyncRepo) Set(ctx context.Context, key, valueJSON string) error {
	if r.failLegacySync && key == legacyHubLLMConfigKey {
		r.failLegacySync = false
		return errors.New("legacy config sync failed")
	}
	return r.testSystemSettingsRepo.Set(ctx, key, valueJSON)
}

func TestLLMUsageReportIncludesPromptCacheCounters(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	rep.addUsage(ts, "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:       100,
		OutputTokens:      20,
		TotalTokens:       120,
		CachedInputTokens: 40,
		CacheWriteTokens:  8,
		Requests:          1,
		CachedRequests:    1,
	}, 0.5)
	rep.addUsage(ts.Add(time.Hour), "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:       300,
		OutputTokens:      60,
		TotalTokens:       360,
		CachedInputTokens: 0,
		CacheWriteTokens:  4,
		Requests:          1,
		CachedRequests:    0,
	}, 1.5)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if resp.Summary.InputTokens != 400 || resp.Summary.TotalTokens != 480 {
		t.Fatalf("summary tokens = input %d total %d", resp.Summary.InputTokens, resp.Summary.TotalTokens)
	}
	if resp.Summary.CachedInputTokens != 40 {
		t.Fatalf("summary cached input = %d, want 40", resp.Summary.CachedInputTokens)
	}
	if resp.Summary.CacheWriteTokens != 12 {
		t.Fatalf("summary cache write = %d, want 12", resp.Summary.CacheWriteTokens)
	}
	if resp.Summary.CachedRequests != 1 || resp.Summary.Requests != 2 {
		t.Fatalf("summary cached requests = %d/%d, want 1/2", resp.Summary.CachedRequests, resp.Summary.Requests)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(resp.Rows))
	}
	row := resp.Rows[0]
	if row.ID != "user@example.com" {
		t.Fatalf("row id = %q", row.ID)
	}
	if row.CachedInputTokens != 40 || row.CacheWriteTokens != 12 {
		t.Fatalf("row cache tokens = read %d write %d", row.CachedInputTokens, row.CacheWriteTokens)
	}
	if row.CachedRequests != 1 || row.Requests != 2 {
		t.Fatalf("row cached requests = %d/%d", row.CachedRequests, row.Requests)
	}
	if len(resp.Trend) != 24 {
		t.Fatalf("trend len = %d, want 24", len(resp.Trend))
	}
	if resp.Trend[9].CachedInputTokens != 40 || resp.Trend[10].CacheWriteTokens != 4 {
		t.Fatalf("trend cache counters not preserved: hour9=%+v hour10=%+v", resp.Trend[9], resp.Trend[10])
	}
}

func TestLLMUsageReportIncludesRMBCostCounters(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	breakdown := &llmUsageCreditBreakdown{RMBPricingRecorded: true}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:   1_000_000,
		OutputTokens:  500_000,
		TotalTokens:   1_500_000,
		InputCostRMB:  3,
		OutputCostRMB: 3,
		TotalCostRMB:  6,
		Requests:      1,
	}, 0, breakdown)
	rep.addUsageWithCreditBreakdown(ts.Add(time.Hour), "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:   250_000,
		OutputTokens:  250_000,
		TotalTokens:   500_000,
		InputCostRMB:  0.25,
		OutputCostRMB: 0.5,
		TotalCostRMB:  0.75,
		Requests:      1,
	}, 0, breakdown)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if resp.Summary.InputCostRMB != 3.25 || resp.Summary.OutputCostRMB != 3.5 || resp.Summary.TotalCostRMB != 6.75 {
		t.Fatalf("summary cost = input %.4f output %.4f total %.4f, want 3.25/3.5/6.75", resp.Summary.InputCostRMB, resp.Summary.OutputCostRMB, resp.Summary.TotalCostRMB)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].TotalCostRMB != 6.75 {
		t.Fatalf("row cost not preserved: %#v", resp.Rows)
	}
	if resp.Trend[9].TotalCostRMB != 6 || resp.Trend[10].TotalCostRMB != 0.75 {
		t.Fatalf("trend cost not preserved: hour9=%+v hour10=%+v", resp.Trend[9], resp.Trend[10])
	}
}

func TestLLMUsageReportRMBCostTotalIsSummedFromDirectionalComponents(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	// TotalCostRMB deliberately contains a stale value. Reports must expose the
	// sum of the four frozen directional amounts, never this independently
	// supplied aggregate.
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens:       1_000,
		CachedInputTokens: 200,
		CacheWriteTokens:  100,
		OutputTokens:      500,
		TotalTokens:       1_500,
		InputCostRMB:      0.1048,
		CacheReadCostRMB:  0.006,
		CacheWriteCostRMB: 0.009,
		OutputCostRMB:     0.0402,
		TotalCostRMB:      999,
		Requests:          1,
	}, 0, &llmUsageCreditBreakdown{RMBPricingRecorded: true})

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if got, want := resp.Summary.TotalCostRMB, 0.16; math.Abs(got-want) > 1e-12 {
		t.Fatalf("summary RMB total = %.12f, want %.12f", got, want)
	}
	if len(resp.Rows) != 1 || math.Abs(resp.Rows[0].TotalCostRMB-0.16) > 1e-12 {
		t.Fatalf("row RMB total must equal its components: %#v", resp.Rows)
	}
	if len(resp.Trend) != 24 || math.Abs(resp.Trend[9].TotalCostRMB-0.16) > 1e-12 {
		t.Fatalf("trend RMB total must equal its components: %#v", resp.Trend[9])
	}
}

func TestLLMUsageReportExcludesLegacyRMBWithoutFrozenPricing(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	pricedUsage := corelib.TokenUsageStat{InputTokens: 10_000, TotalTokens: 10_000, InputCostRMB: 0.02, TotalCostRMB: 0.02, Requests: 1}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, pricedUsage, 1, &llmUsageCreditBreakdown{RMBPricingRecorded: true}, "provider-a")
	// Old reports may include non-zero displayed RMB costs without a directional
	// frozen price snapshot. They are not trustworthy enough to mix into the
	// new reference-cost total.
	legacyUsage := corelib.TokenUsageStat{InputTokens: 10_000_000, TotalTokens: 10_000_000, InputCostRMB: 12, TotalCostRMB: 12, Requests: 1}
	rep.addUsage(ts, "user@example.com", nil, legacyUsage, 1_000, "provider-a")

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if resp.Summary.TotalCostRMB != 0.02 || resp.Summary.InputCostRMB != 0.02 || resp.Summary.RMBPricedInputTokens != 10_000 || resp.Summary.RMBPricedCredits != 1 {
		t.Fatalf("legacy RMB leaked into frozen reference cost: %+v", resp.Summary)
	}
}

func TestLLMUsageReportPricingSourceCountedOnce(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	usage := corelib.TokenUsageStat{InputTokens: 10, Requests: 1, PricingSource: "provider"}
	breakdown := &llmUsageCreditBreakdown{PricingSource: "provider", RMBPricingRecorded: true}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, usage, 1, breakdown, "provider-a")
	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if got := resp.Summary.PricingSources["provider"]; got != 1 {
		t.Fatalf("pricing source count = %d, want 1", got)
	}
}

func TestLLMUsageReportDoesNotLabelLegacyDebitAsFrozenProviderPricing(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	legacy := &llmUsageCreditBreakdown{
		UnitemizedComponent: 1,
		ProviderID:          "provider-a",
		ProviderMultiplier:  1.5,
		InputCreditsPer10K:  99,
		OutputCreditsPer10K: 199,
		InputRMBPer10K:      9,
		OutputRMBPer10K:     19,
		RMBPricingRecorded:  false,
	}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000, Requests: 1}, 1, legacy, "provider-a")

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if len(resp.Summary.ProviderPricing) != 0 || len(resp.Rows) != 1 || len(resp.Rows[0].ProviderPricing) != 0 {
		t.Fatalf("legacy request exposed invented provider pricing: summary=%#v rows=%#v", resp.Summary.ProviderPricing, resp.Rows)
	}
	if len(resp.Summary.ProviderMultipliers) != 1 || resp.Summary.ProviderMultipliers[0].Multiplier != 1.5 {
		t.Fatalf("legacy provider multiplier should remain auditable: %#v", resp.Summary.ProviderMultipliers)
	}
}

func TestLLMUsageReportDoesNotPersistDisplayNamesIntoHourlyPricingFacts(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	breakdown := &llmUsageCreditBreakdown{
		ProviderID:         "provider-a",
		InputCreditsPer10K: 1,
		InputRMBPer10K:     0.02,
		RMBPricingRecorded: true,
	}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000, Requests: 1}, 1, breakdown, "provider-a")

	first := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts, map[string]string{"provider-a": "Provider A"})
	if len(first.Trend) != 24 || len(first.Trend[9].ProviderPricing) != 1 || first.Trend[9].ProviderPricing[0].ProviderName != "Provider A" {
		t.Fatalf("first hourly price name = %#v", first.Trend)
	}
	second := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts, map[string]string{"provider-a": "Renamed Provider"})
	if len(second.Trend) != 24 || len(second.Trend[9].ProviderPricing) != 1 || second.Trend[9].ProviderPricing[0].ProviderName != "Renamed Provider" {
		t.Fatalf("hourly pricing retained stale display name: %#v", second.Trend)
	}
	entry := rep.Days["2026-04-21"].Users["user@example.com"]
	if entry.Hours[9].ProviderPricing[0].ProviderName != "" {
		t.Fatalf("persisted hourly pricing was mutated with a display name: %#v", entry.Hours[9].ProviderPricing)
	}
}

func TestLLMUsageReportIncludesSettledCreditCalculationComponents(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	breakdown := &llmUsageCreditBreakdown{
		InputComponent:     1.2,
		OutputComponent:    3.6,
		MinimumAdjustment:  0.2,
		RoundingAdjustment: 0.1,
	}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  10_000,
		OutputTokens: 5_000,
		TotalTokens:  15_000,
		Requests:     1,
	}, 5.1, breakdown)
	rep.addUsage(ts.Add(time.Hour), "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		Requests:     1,
	}, 1)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if resp.Summary.Credits != 6.1 || resp.Summary.CreditInputComponent != 1.2 || resp.Summary.CreditOutputComponent != 3.6 || resp.Summary.CreditMinimumAdjustment != 0.2 || resp.Summary.CreditRoundingAdjustment != 0.1 || resp.Summary.CreditUnitemizedComponent != 1 {
		t.Fatalf("summary credit breakdown = %+v", resp.Summary)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].Credits != 6.1 || resp.Rows[0].CreditInputComponent != 1.2 || resp.Rows[0].CreditOutputComponent != 3.6 || resp.Rows[0].CreditMinimumAdjustment != 0.2 || resp.Rows[0].CreditRoundingAdjustment != 0.1 || resp.Rows[0].CreditUnitemizedComponent != 1 {
		t.Fatalf("row credit breakdown = %#v", resp.Rows)
	}
}

func TestLLMUsageReportBoundsStaleDirectionalComponents(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 10_000, Requests: 1}, 20, &llmUsageCreditBreakdown{
		InputComponent: 10, NormalInputComponent: 10, OutputComponent: 10,
		ProviderID: "p", RMBPricingRecorded: true,
	}, "p")
	// No provider pricing means there is no safe upper bound; preserve the
	// frozen component exactly rather than guessing from current settings.
	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-08-26", "2026-08", "", ts)
	if resp.Summary.CreditOutputComponent != 10 || resp.Summary.CreditUnitemizedComponent != 0 {
		t.Fatalf("component without pricing was rewritten: %+v", resp.Summary)
	}
}

func TestBoundUsageCreditComponentsMovesImpossibleExcessToUnitemized(t *testing.T) {
	c := &llmUsageCounters{
		PricedNormalInputTokens: 10_000, PricedOutputTokens: 10_000,
		CreditNormalInputComponent: 10, CreditOutputComponent: 10,
		ProviderPricing:     []llmUsageProviderPricing{{ProviderID: "p", InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
		ProviderMultipliers: []llmUsageProviderMultiplier{{ProviderID: "p", Multiplier: 1, MultiplierSource: "provider"}},
	}
	boundUsageCreditComponents(c)
	if c.CreditNormalInputComponent != 1 || c.CreditOutputComponent != 2 || c.CreditUnitemizedComponent != 17 {
		t.Fatalf("bounded components = %+v", c)
	}
}

func TestLLMUsageReportUsesDirectionalPricedTokenDenominators(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	priced := corelib.TokenUsageStat{
		InputTokens:       3_972,
		CachedInputTokens: 256,
		CacheWriteTokens:  128,
		OutputTokens:      463,
		TotalTokens:       4_435,
		Requests:          1,
	}
	breakdown := &llmUsageCreditBreakdown{
		NormalInputComponent: 0.3588,
		CacheReadComponent:   0.00256,
		CacheWriteComponent:  0.0128,
		OutputComponent:      0.0926,
		RMBPricingRecorded:   true,
	}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, priced, 0.46676, breakdown, "provider-a")
	// A legacy debit must remain in the total but must not be used as the
	// denominator for a frozen directional price.
	rep.addUsage(ts, "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens: 3_000_000, OutputTokens: 15_000, TotalTokens: 3_015_000, Requests: 100,
	}, 324.279, "provider-a")

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	got := resp.Summary
	if got.PricedNormalInputTokens != 3_588 || got.PricedCacheReadTokens != 256 || got.PricedCacheWriteTokens != 128 || got.PricedOutputTokens != 463 {
		t.Fatalf("directional priced token denominators = %+v", got)
	}
	if got.InputTokens != 3_003_972 || got.CachedInputTokens != 256 {
		t.Fatalf("all usage must still be retained separately: %+v", got)
	}
}

func TestLLMUsageReportRetainsSettledProviderMultiplier(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000, Requests: 1}, 2, &llmUsageCreditBreakdown{
		InputComponent:         2,
		ProviderID:             "provider-a",
		ProviderMultiplier:     1,
		ServiceGroupMultiplier: 2,
	}, "provider-a")

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts, map[string]string{"provider-a": "Provider A"})
	if len(resp.Summary.ProviderMultipliers) != 2 {
		t.Fatalf("provider multipliers = %#v", resp.Summary.ProviderMultipliers)
	}
	if got := resp.Summary.ProviderMultipliers[0]; got.ProviderID != "provider-a" || got.ProviderName != "Provider A" || got.Multiplier != 1 || got.MultiplierSource != "provider" {
		t.Fatalf("provider multiplier = %#v", got)
	}
	if got := resp.Summary.ProviderMultipliers[1]; got.ProviderID != "provider-a" || got.Multiplier != 2 || got.MultiplierSource != "service_group" {
		t.Fatalf("service-group multiplier = %#v", got)
	}
	if len(resp.Rows) != 1 || len(resp.Rows[0].ProviderMultipliers) != 2 || resp.Rows[0].ProviderMultipliers[1].Multiplier != 2 {
		t.Fatalf("row provider multipliers = %#v", resp.Rows)
	}
}

func TestLLMUsageReportRetainsLegacyProviderMultiplier(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	legacy := &llmUsageCreditBreakdown{
		UnitemizedComponent:    1.5,
		ProviderID:             "provider-a",
		ProviderMultiplier:     1.5,
		ServiceGroupMultiplier: 0,
	}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{TotalTokens: 1_000, Requests: 1}, 1.5, legacy, "provider-a")

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts, map[string]string{"provider-a": "Provider A"})
	if resp.Summary.CreditUnitemizedComponent != 1.5 || len(resp.Summary.ProviderMultipliers) != 1 {
		t.Fatalf("legacy summary = %+v", resp.Summary)
	}
	got := resp.Summary.ProviderMultipliers[0]
	if got.ProviderName != "Provider A" || got.Multiplier != 1.5 || got.MultiplierSource != "provider" {
		t.Fatalf("legacy provider multiplier = %#v", got)
	}
}

func TestLLMUsageReportSettlesCreditsToActualLedgerDeduction(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", []string{"team-a"}, corelib.TokenUsageStat{
		InputTokens: 10_000,
		Requests:    1,
	}, 5, &llmUsageCreditBreakdown{InputComponent: 5}, "provider-a")

	// A grant or period limit can permit only part of a calculated request
	// amount. The Usage Stats tooltip must reconcile to the ledger's actual
	// debit, without inflating its token or request counts.
	rep.addSettledCreditAdjustment(ts, "user@example.com", []string{"team-a"}, "provider-a", -2, false)
	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if resp.Summary.Credits != 3 || resp.Summary.CreditInputComponent != 5 || resp.Summary.CreditRoundingAdjustment != -2 || resp.Summary.Requests != 1 || resp.Summary.InputTokens != 10_000 {
		t.Fatalf("settled usage summary = %+v", resp.Summary)
	}
}

func TestSettledCreditAdjustmentUsesTheChargeReportTimestampAndScope(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 23, 30, 0, 0, time.UTC)
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", []string{"team-a"}, corelib.TokenUsageStat{InputTokens: 10_000, Requests: 1}, 5, &llmUsageCreditBreakdown{InputComponent: 5}, "provider-a")
	charge := &pendingCreditCharge{
		email:           "user@example.com",
		userGroupIDs:    []string{"team-a"},
		providerID:      "provider-a",
		credits:         3,
		reportedCredits: 5,
		reportedAt:      ts,
		requestID:       "request-1",
	}
	applySettledCreditAdjustments(rep, map[string]*pendingCreditCharge{"request-1": charge})

	for _, tc := range []struct {
		scope  string
		entity string
	}{
		{scope: "user", entity: "user@example.com"},
		{scope: "group", entity: "team-a"},
		{scope: "provider", entity: "provider-a"},
	} {
		resp := buildLLMUsageReportResponse(context.Background(), rep, nil, tc.scope, "daily", "2026-04-21", "2026-04", tc.entity, ts)
		if resp.Summary.Credits != 3 || resp.Summary.CreditRoundingAdjustment != -2 || len(resp.Trend) != 24 || resp.Trend[23].Credits != 3 {
			t.Fatalf("%s settlement adjustment was not kept in the request scope: %+v", tc.scope, resp)
		}
	}
	if charge.reportedCredits != 3 {
		t.Fatalf("reported credits = %v, want 3 to prevent duplicate adjustment", charge.reportedCredits)
	}
}

func TestSettledCreditAdjustmentKeepsRMBCoverageAlignedWithActualDebit(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	breakdown := &llmUsageCreditBreakdown{
		InputComponent:     5,
		RMBPricingRecorded: true,
	}
	// The Credits cap can reduce the user's final debit, but it does not undo
	// the completed upstream request. Its frozen RMB reference cost therefore
	// stays attached to the observed token usage.
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  10_000,
		InputCostRMB: 0.02,
		TotalCostRMB: 0.02,
		Requests:     1,
	}, 5, breakdown, "provider-a")
	rep.addSettledCreditAdjustment(ts, "user@example.com", nil, "provider-a", -2, true)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if resp.Summary.Credits != 3 || resp.Summary.RMBPricedCredits != 3 || resp.Summary.RMBPricedRequests != 1 || resp.Summary.InputCostRMB != 0.02 || resp.Summary.TotalCostRMB != 0.02 {
		t.Fatalf("settled RMB coverage = %+v", resp.Summary)
	}
}

func TestAccumulatorDefersUsageReportUntilCreditSettlementSucceeds(t *testing.T) {
	system := &failOnceUsageReportSettingsRepo{}
	now := time.Now().UTC().Truncate(time.Second)
	registry := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		Grants: []llmservice.Grant{{
			ID: "grant-1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 3, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		}},
	}
	if err := llmservice.SaveRegistry(t.Context(), system, registry); err != nil {
		t.Fatal(err)
	}
	invalidateLLMRuntimeCaches(system)

	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, InputRMBPer10K: 0.02}}
	usage := corelib.TokenUsageStat{InputTokens: 10_000, TotalTokens: 10_000, InputCostRMB: 0.02, TotalCostRMB: 0.02, Requests: 1}
	breakdown := &llmUsageCreditBreakdown{InputComponent: 5, RMBPricingRecorded: true}
	reports := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	reports.addUsageWithCreditBreakdown(now, "user@example.com", nil, usage, 5, breakdown, "provider-a")
	charge := &pendingCreditCharge{
		userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 5, reportedCredits: 5,
		reportedAt: now, requestID: "request-1", providerID: "provider-a", usage: usage, pricing: pricing,
	}
	accumulator := &llmUsageAccumulator{pending: map[store.SystemSettingsRepository]*pendingSystemUsage{
		system: {creditCharges: map[string]*pendingCreditCharge{creditChargeKey(charge): charge}, reports: reports},
	}}
	system.failRegistry = true

	// The first registry write fails. The requested five Credits must not be
	// written into the report before the durable ledger determines that only
	// three Credits are available to deduct.
	accumulator.flush(t.Context())
	stored, err := loadLLMUsageReports(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Days) != 0 {
		t.Fatalf("usage report persisted before settlement: %+v", stored)
	}

	accumulator.flush(t.Context())
	stored, err = loadLLMUsageReports(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	resp := buildLLMUsageReportResponse(t.Context(), stored, nil, "user", "daily", now.Format("2006-01-02"), now.Format("2006-01"), "user@example.com", now)
	if resp.Summary.Credits != 3 || resp.Summary.RMBPricedCredits != 3 || resp.Summary.CreditRoundingAdjustment != -2 {
		t.Fatalf("settled report = %+v; want actual 3-Credit debit", resp.Summary)
	}
}

func TestFlushCreditChargesFreezesCacheLegsAndDirectionalAmounts(t *testing.T) {
	system := &testSystemSettingsRepo{}
	now := time.Now().UTC().Truncate(time.Second)
	registry := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		Grants: []llmservice.Grant{{
			ID: "grant-1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 10, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		}},
	}
	if err := llmservice.SaveRegistry(t.Context(), system, registry); err != nil {
		t.Fatal(err)
	}
	invalidateLLMRuntimeCaches(system)

	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K: 1, OutputCreditsPer10K: 4, InputRMBPer10K: 0.02, OutputRMBPer10K: 0.08,
	}}
	usage := corelib.TokenUsageStat{
		InputTokens: 10_000, CachedInputTokens: 8_000, CacheWriteTokens: 1_000, OutputTokens: 5_000, TotalTokens: 15_000,
		InputCostRMB: 0.002, CacheReadCostRMB: 0.00016, CacheWriteCostRMB: 0.002, OutputCostRMB: 0.04, TotalCostRMB: 0.04416,
		Requests: 1, PricingSource: llmpool.PricingSourceServiceGroupOverride,
	}
	charge := &pendingCreditCharge{
		userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2.28,
		requestID: "request-cache-1", providerID: "provider-a", usage: usage,
		providerMultiplier: 1, serviceGroupMultiplier: 1, pricing: pricing,
	}
	settled, err := flushCreditChargesDetailed(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge})
	if err != nil || !settled[creditChargeKey(charge)] {
		t.Fatalf("flush: settled=%v err=%v", settled, err)
	}

	stored, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := llmservice.BillingLedgerEntryForRequest(stored, "request-cache-1")
	if !ok {
		t.Fatal("ledger entry missing after settlement")
	}
	if entry.CachedInputTokens != 8_000 || entry.CacheWriteTokens != 1_000 {
		t.Fatalf("cache legs not frozen in ledger: %+v", entry)
	}
	// The directional amounts are the already-computed settlement facts:
	// normal 1000×1/10k=0.1, read 8000×0.1/10k=0.08, write 1000×1/10k=0.1,
	// output 5000×4/10k=2 (defaults derived at resolution: read=input×0.1,
	// write=input).
	assertFloat := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
	assertFloat("normal_input_credits", entry.NormalInputCredits, 0.1)
	assertFloat("cache_read_credits", entry.CacheReadCredits, 0.08)
	assertFloat("cache_write_credits", entry.CacheWriteCredits, 0.1)
	assertFloat("output_credits", entry.OutputCredits, 2)
	assertFloat("normal_input_cost_rmb", entry.NormalInputCostRMB, 0.002)
	assertFloat("cache_read_cost_rmb", entry.CacheReadCostRMB, 0.00016)
	assertFloat("cache_write_cost_rmb", entry.CacheWriteCostRMB, 0.002)
	assertFloat("output_cost_rmb", entry.OutputCostRMB, 0.04)
	if entry.PricingSource != llmpool.PricingSourceServiceGroupOverride {
		t.Fatalf("pricing source not frozen in ledger: %q", entry.PricingSource)
	}

	// A replayed request must rebuild its reporting provenance from the ledger,
	// including the cache legs, the frozen directional RMB amounts, and the
	// pricing source, instead of the retry's stale or cache-less usage.
	replay := &pendingCreditCharge{
		userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 15,
		requestID: "request-cache-1", providerID: "provider-a",
		usage: corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 5_000, TotalTokens: 15_000, Requests: 1, InputCostRMB: 9.9, TotalCostRMB: 9.9},
	}
	settled, err = flushCreditChargesDetailed(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(replay): replay})
	if err != nil {
		t.Fatalf("replay flush: %v", err)
	}
	if settled[creditChargeKey(replay)] {
		t.Fatal("replayed request was debited a second time")
	}
	if replay.usage.CachedInputTokens != 8_000 || replay.usage.CacheWriteTokens != 1_000 {
		t.Fatalf("replay lost cache legs: %+v", replay.usage)
	}
	assertFloat("replay input_cost_rmb", replay.usage.InputCostRMB, 0.002)
	assertFloat("replay cache_read_cost_rmb", replay.usage.CacheReadCostRMB, 0.00016)
	assertFloat("replay cache_write_cost_rmb", replay.usage.CacheWriteCostRMB, 0.002)
	assertFloat("replay output_cost_rmb", replay.usage.OutputCostRMB, 0.04)
	assertFloat("replay total_cost_rmb", replay.usage.TotalCostRMB, 0.04416)
	if replay.usage.PricingSource != llmpool.PricingSourceServiceGroupOverride {
		t.Fatalf("replay lost frozen pricing source: %q", replay.usage.PricingSource)
	}
	if replay.pricing == nil || replay.pricing.InputCreditsPer10K != 1 {
		t.Fatalf("replay lost frozen pricing: %+v", replay.pricing)
	}
	if replay.credits != 2.28 {
		t.Fatalf("replay credits = %v, want ledger deduction 2.28", replay.credits)
	}
}

func TestRecordLocalCacheHitLLMUsageCountsRequestWithoutCharge(t *testing.T) {
	system := &testSystemSettingsRepo{}
	// A local full-response cache hit never reaches upstream: zero tokens, zero
	// Credits, but the request must appear in Usage Stats with the local_cache
	// marker, separate from provider prompt-cache reads (design §2.2).
	recordLocalCacheHitLLMUsage(t.Context(), system, nil, "u1", "user@example.com", "provider-a", []string{"paid"})
	globalLLMUsageAccumulator.flush(t.Context())
	stored, err := loadLLMUsageReports(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	// The report is bucketed by the local day; derive the key from the stored
	// record so the assertion survives timezone and midnight boundaries.
	if len(stored.Days) != 1 {
		t.Fatalf("stored report days = %d, want 1", len(stored.Days))
	}
	dayKey := ""
	for key := range stored.Days {
		dayKey = key
	}
	resp := buildLLMUsageReportResponse(t.Context(), stored, nil, "user", "daily", dayKey, dayKey[:7], "user@example.com", time.Now())
	if resp.Summary.Requests != 1 || resp.Summary.Credits != 0 {
		t.Fatalf("local cache hit summary = %+v, want 1 request and 0 credits", resp.Summary)
	}
	if resp.Summary.InputTokens != 0 || resp.Summary.OutputTokens != 0 || resp.Summary.CachedInputTokens != 0 || resp.Summary.CacheWriteTokens != 0 {
		t.Fatalf("local cache hit recorded phantom tokens: %+v", resp.Summary)
	}
	if resp.Summary.CachedRequests != 0 {
		t.Fatalf("local cache hit must not count as a prompt-cache request: %+v", resp.Summary)
	}
	if got := resp.Summary.CacheUsageSources["local_cache"]; got != 1 {
		t.Fatalf("cache_usage_sources = %+v, want local_cache=1", resp.Summary.CacheUsageSources)
	}
	// No charge and no ledger entry may be created for a free cache hit.
	reg, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.BillingLedger) != 0 {
		t.Fatalf("local cache hit created a ledger entry: %#v", reg.BillingLedger)
	}
}

func TestBillingLedgerEntryJSONKeepsLegacyShapeCompatible(t *testing.T) {
	// Entries written before the cache-v1 ledger extension have no cache legs
	// and no directional amounts; they must still decode, and zero-valued new
	// fields must not appear when re-serialized.
	var legacy llmservice.BillingLedgerEntry
	if err := json.Unmarshal([]byte(`{"request_id":"r1","input_tokens":100,"output_tokens":50,"requested_credits":1,"deducted_credits":1,"billing_group_multiplier":1,"created_at":"2026-08-24T01:00:00Z"}`), &legacy); err != nil {
		t.Fatalf("legacy entry decode: %v", err)
	}
	if legacy.CachedInputTokens != 0 || legacy.CacheReadCredits != 0 || legacy.NormalInputCostRMB != 0 {
		t.Fatalf("legacy entry gained phantom cache facts: %+v", legacy)
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"cached_input_tokens", "cache_write_tokens", "normal_input_credits", "cache_read_credits", "cache_write_credits", "output_credits", "normal_input_cost_rmb", "cache_read_cost_rmb", "cache_write_cost_rmb", "output_cost_rmb"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("zero-valued %s leaked into legacy JSON: %s", key, encoded)
		}
	}
}

func TestLegacySettlementAdjustmentDoesNotChangeRMBCoverage(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	pricedBreakdown := &llmUsageCreditBreakdown{
		InputComponent:     5,
		RMBPricingRecorded: true,
	}
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000, Requests: 1}, 5, pricedBreakdown, "provider-a")
	// A second request was settled under legacy billing. Its partial debit must
	// not be counted as RMB-priced merely because the same row also contains the
	// first request above.
	rep.addUsage(ts, "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000, Requests: 1}, 5, "provider-a")
	rep.addSettledCreditAdjustment(ts, "user@example.com", nil, "provider-a", -2, false)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if resp.Summary.Credits != 8 || resp.Summary.RMBPricedCredits != 5 || resp.Summary.RMBPricedRequests != 1 {
		t.Fatalf("legacy settlement adjustment must not expand RMB coverage: %+v", resp.Summary)
	}
}

func TestRequeueCopiesUserGroupsForLaterSettlementAdjustment(t *testing.T) {
	accumulator := &llmUsageAccumulator{pending: map[store.SystemSettingsRepository]*pendingSystemUsage{}}
	system := &testSystemSettingsRepo{}
	charge := &pendingCreditCharge{
		email:           "user@example.com",
		userGroupIDs:    []string{"team-a"},
		serviceGroupIDs: []string{"paid"},
		credits:         3,
		reportedCredits: 5,
		reportedAt:      time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC),
		requestID:       "request-1",
		providerID:      "provider-a",
	}
	accumulator.requeue(system, &pendingSystemUsage{creditCharges: map[string]*pendingCreditCharge{"request-1": charge}})

	// The retry buffer must not retain caller-owned slices. A later settlement
	// adjustment still has to update the original user-group report.
	charge.userGroupIDs[0] = "mutated"
	copied := accumulator.pending[system].creditCharges["request-1"]
	if len(copied.userGroupIDs) != 1 || copied.userGroupIDs[0] != "team-a" {
		t.Fatalf("requeued user groups = %#v, want independent team-a copy", copied.userGroupIDs)
	}
}

func TestLLMUsageReportNormalizesLegacyCreditTotalsAsUnitemized(t *testing.T) {
	var rep llmUsageReportsStore
	if err := json.Unmarshal([]byte(`{
        "version": 1,
        "days": {
            "2026-04-21": {
                "totals": {"credits": 3.5},
                "users": {
                    "user@example.com": {
                        "totals": {"credits": 3.5},
                        "hours": [{"credits": 3.5}]
                    }
                }
            }
        }
    }`), &rep); err != nil {
		t.Fatalf("unmarshal legacy report: %v", err)
	}

	resp := buildLLMUsageReportResponse(context.Background(), &rep, nil, "user", "daily", "2026-04-21", "2026-04", "", time.Now())
	if resp.Summary.CreditUnitemizedComponent != 3.5 || len(resp.Rows) != 1 || resp.Rows[0].CreditUnitemizedComponent != 3.5 || len(resp.Trend) != 24 || resp.Trend[0].CreditUnitemizedComponent != 3.5 {
		t.Fatalf("legacy credits must reconcile as unitemized components: %+v", resp)
	}
}

func TestLLMUsageReportScopesResidualLegacyTokens(t *testing.T) {
	// Simulate a row formed by a mix of new directionally-priced requests and
	// older aggregate-only settlements. The residual debit must not be shown as
	// an unexplained amount with zero token scope.
	c := &llmUsageCounters{
		InputTokens: 2_738_212, OutputTokens: 18_541, CachedInputTokens: 1_902_080,
		Requests: 130, Credits: 278.42,
		CreditInputComponent: 3.375, CreditMinimumAdjustment: 0.0103,
		CreditRoundingAdjustment: 0.00014,
		PricedNormalInputTokens:  25_726, PricedCacheReadTokens: 1_536,
		PricedOutputTokens: 3_883, RMBPricedRequests: 12,
	}
	normalizeUsageCreditComponents(c)
	if c.CreditUnitemizedComponent < 275 {
		t.Fatalf("unitemized credits = %v, want residual legacy debit", c.CreditUnitemizedComponent)
	}
	if c.UnitemizedRequests != 118 || c.UnitemizedInputTokens != 2_710_950 || c.UnitemizedCachedInputTokens != 1_902_080-1_536 || c.UnitemizedOutputTokens != 14_658 {
		t.Fatalf("unitemized scope = %+v", c)
	}
	// Normalization is also used when rendering an already-normalized report;
	// repeating it must not grow the residual scope or debit.
	normalizeUsageCreditComponents(c)
	if c.CreditUnitemizedComponent < 275 || c.UnitemizedRequests != 118 || c.UnitemizedInputTokens != 2_710_950 || c.UnitemizedCachedInputTokens != 1_902_080-1_536 || c.UnitemizedOutputTokens != 14_658 {
		t.Fatalf("normalization is not idempotent: %+v", c)
	}
}

func TestLLMUsageReportSupportsProviderScope(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	rep.addUsage(ts, "alice@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  100,
		OutputTokens: 20,
		TotalTokens:  120,
		Requests:     1,
	}, 0.12, "provider-a")
	rep.addUsage(ts.Add(time.Hour), "bob@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  300,
		OutputTokens: 60,
		TotalTokens:  360,
		Requests:     1,
	}, 0.36, "provider-b")
	rep.addUsage(ts.Add(2*time.Hour), "alice@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  40,
		OutputTokens: 10,
		TotalTokens:  50,
		Requests:     1,
	}, 0.05, "provider-a")
	rep.addUsage(ts.Add(3*time.Hour), "legacy@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  900,
		OutputTokens: 99,
		TotalTokens:  999,
		Requests:     1,
	}, 0.99)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "provider", "daily", "2026-04-21", "2026-04", "", ts, map[string]string{
		"provider-a": "Provider A",
		"provider-b": "Provider B",
	})
	if resp.Summary.TotalTokens != 530 || len(resp.Rows) != 2 {
		t.Fatalf("provider daily summary=%+v rows=%#v", resp.Summary, resp.Rows)
	}
	if resp.Rows[0].ID != "provider-b" || resp.Rows[0].Name != "Provider B" || resp.Rows[0].TotalTokens != 360 {
		t.Fatalf("provider rows not sorted/named by usage: %#v", resp.Rows)
	}
	if len(resp.Trend) != 24 || resp.Trend[9].TotalTokens != 120 || resp.Trend[10].TotalTokens != 360 || resp.Trend[11].TotalTokens != 50 {
		t.Fatalf("provider trend not aggregated: %#v", resp.Trend)
	}

	filtered := buildLLMUsageReportResponse(context.Background(), rep, nil, "provider", "monthly", "", "2026-04", "provider-a", ts, map[string]string{
		"provider-a": "Provider A",
	})
	if filtered.Summary.TotalTokens != 170 || len(filtered.Rows) != 1 || filtered.Rows[0].Name != "Provider A" {
		t.Fatalf("provider monthly filtered summary=%+v rows=%#v", filtered.Summary, filtered.Rows)
	}
}

func TestMonthlyUsageReportEntitySummaryDoesNotLeakGlobalTotals(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	rep.addUsage(time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC), "alice@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		TotalCostRMB: 1.5,
		Requests:     1,
	}, 0.015)
	rep.addUsage(time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC), "bob@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  200,
		OutputTokens: 100,
		TotalTokens:  300,
		TotalCostRMB: 3,
		Requests:     1,
	}, 0.03)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "monthly", "", "2026-04", "alice@example.com", now)
	if resp.Summary.TotalTokens != 150 || resp.Summary.TotalCostRMB != 1.5 || resp.Summary.Credits != 0.015 {
		t.Fatalf("alice monthly summary leaked global totals: %+v", resp.Summary)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].ID != "alice@example.com" || resp.Rows[0].TotalTokens != 150 {
		t.Fatalf("alice monthly rows = %#v", resp.Rows)
	}

	missing := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "monthly", "", "2026-04", "nobody@example.com", now)
	if missing.Summary.TotalTokens != 0 || missing.Summary.TotalCostRMB != 0 || len(missing.Rows) != 0 {
		t.Fatalf("missing entity should be empty, got summary=%+v rows=%#v", missing.Summary, missing.Rows)
	}
}

func TestLLMUsageReportHandlerUsesTenantScopedSettings(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)

	globalRep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	globalRep.addUsage(ts, "global@example.com", nil, corelib.TokenUsageStat{TotalTokens: 900, Requests: 1}, 0)
	if err := saveLLMUsageReports(context.Background(), system, globalRep); err != nil {
		t.Fatalf("save global usage report: %v", err)
	}

	tenantRep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	tenantRep.addUsage(ts, "tenant@example.com", nil, corelib.TokenUsageStat{TotalTokens: 123, Requests: 1}, 0)
	if err := saveLLMUsageReports(context.Background(), scopedSystemSettingsForTenant("tenant_a", system), tenantRep); err != nil {
		t.Fatalf("save tenant usage report: %v", err)
	}

	handler := GetLLMUsageReportHandler(system, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/usage-report?period=monthly&month=2026-04", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant_a"}))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp llmUsageReportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Summary.TotalTokens != 123 || len(resp.Rows) != 1 || resp.Rows[0].ID != "tenant@example.com" {
		t.Fatalf("tenant report should not read global settings, summary=%+v rows=%#v", resp.Summary, resp.Rows)
	}

	defaultReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/usage-report?period=monthly&month=2026-04", nil)
	defaultReq = defaultReq.WithContext(context.WithValue(defaultReq.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: store.DefaultTenantID}))
	defaultRec := httptest.NewRecorder()
	handler(defaultRec, defaultReq)
	if defaultRec.Code != http.StatusOK {
		t.Fatalf("default tenant status = %d body=%s", defaultRec.Code, defaultRec.Body.String())
	}
	var defaultResp llmUsageReportResponse
	if err := json.Unmarshal(defaultRec.Body.Bytes(), &defaultResp); err != nil {
		t.Fatalf("decode default tenant response: %v", err)
	}
	if defaultResp.Summary.TotalTokens != 900 || len(defaultResp.Rows) != 1 || defaultResp.Rows[0].ID != "global@example.com" {
		t.Fatalf("default tenant report inherited a previous tenant scope, summary=%+v rows=%#v", defaultResp.Summary, defaultResp.Rows)
	}
}

func TestLLMUsageAccumulatorFlushKeepsTenantUsageSeparated(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	tenantA := scopedSystemSettingsForTenant("tenant_a", system)
	tenantB := scopedSystemSettingsForTenant("tenant_b", system)
	accumulator := &llmUsageAccumulator{
		pending:  map[store.SystemSettingsRepository]*pendingSystemUsage{},
		interval: time.Hour,
	}

	accumulator.enqueue(tenantA, "provider-a", corelib.TokenUsageStat{TotalTokens: 123, Requests: 1}, "user-a", "a@example.com", nil, nil, 0, "", nil)
	accumulator.enqueue(tenantB, "provider-b", corelib.TokenUsageStat{TotalTokens: 456, Requests: 1}, "user-b", "b@example.com", nil, nil, 0, "", nil)
	accumulator.flush(ctx)

	for _, tc := range []struct {
		name   string
		system store.SystemSettingsRepository
		email  string
		tokens int64
	}{
		{name: "tenant a", system: tenantA, email: "a@example.com", tokens: 123},
		{name: "tenant b", system: tenantB, email: "b@example.com", tokens: 456},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := loadLLMUsageReports(ctx, tc.system)
			if err != nil {
				t.Fatalf("load usage reports: %v", err)
			}
			var actual int64
			for _, day := range report.Days {
				if entry := day.Users[tc.email]; entry != nil {
					actual += entry.Totals.TotalTokens
				}
			}
			if actual != tc.tokens {
				t.Fatalf("tokens for %s = %d, want %d; reports=%#v", tc.email, actual, tc.tokens, report)
			}
		})
	}

	globalReport, err := loadLLMUsageReports(ctx, system)
	if err != nil {
		t.Fatalf("load default tenant usage reports: %v", err)
	}
	if len(globalReport.Days) != 0 {
		t.Fatalf("default tenant received tenant usage: %#v", globalReport)
	}
}

func TestLLMUsageAccumulatorDoesNotReplayProviderUsageAfterLegacySyncFailure(t *testing.T) {
	ctx := context.Background()
	system := &failOnceUsageProviderLegacySyncRepo{failLegacySync: true}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	accumulator := &llmUsageAccumulator{
		pending:  map[store.SystemSettingsRepository]*pendingSystemUsage{},
		interval: time.Hour,
	}
	accumulator.enqueue(system, "provider-a", corelib.TokenUsageStat{InputTokens: 6, OutputTokens: 3, TotalTokens: 9, Requests: 1}, "user-a", "user@example.com", nil, nil, 0, "", nil)

	// The registry write succeeds while the compatibility projection fails. The
	// successful usage mutation must not be requeued and counted again later.
	accumulator.flush(ctx)
	accumulator.flush(ctx)

	registry, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	stat := registry.TokenUsage["provider-a"]
	if stat == nil || stat.InputTokens != 6 || stat.OutputTokens != 3 || stat.TotalTokens != 9 || stat.Requests != 1 {
		t.Fatalf("provider usage replayed after legacy sync failure: %#v", stat)
	}
}

func TestFlushCreditChargesSettlesLateResponseAfterUsageUnresolved(t *testing.T) {
	system := &testSystemSettingsRepo{}
	now := time.Now().UTC().Truncate(time.Second)
	registry := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		Grants: []llmservice.Grant{{
			ID: "grant-1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 10, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		}},
		// A sent local reservation whose response was lost past the recovery
		// window: its hold no longer counts against the balance, and the late
		// response must still settle the real usage exactly once.
		BillingReservations: []llmservice.BillingReservation{{
			RequestID: "late-request", UserID: "u1", Email: "user@example.com",
			ServiceGroupIDs: []string{"paid"}, Credits: 7,
			ProviderID: "third-party",
			SentAt:     now.Add(-llmservice.SentLocalBillingReservationMaxAge - time.Minute),
			ExpiresAt:  now.Add(-llmservice.SentLocalBillingReservationMaxAge),
			CreatedAt:  now.Add(-llmservice.SentLocalBillingReservationMaxAge - time.Minute),
		}},
	}
	if err := llmservice.SaveRegistry(t.Context(), system, registry); err != nil {
		t.Fatal(err)
	}
	invalidateLLMRuntimeCaches(system)
	if got := llmservice.AvailableCreditsForServiceGroupsForUserID(registry, "u1", "user@example.com", []string{"paid"}, now); got != 10 {
		t.Fatalf("aged sent reservation still holds balance: available=%v, want 10", got)
	}

	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K: 1, OutputCreditsPer10K: 4,
	}}
	charge := &pendingCreditCharge{
		userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2.28,
		requestID: "late-request", providerID: "third-party",
		usage:              corelib.TokenUsageStat{InputTokens: 10_000, CachedInputTokens: 8_000, CacheWriteTokens: 1_000, OutputTokens: 5_000, TotalTokens: 15_000, Requests: 1},
		providerMultiplier: 1, serviceGroupMultiplier: 1, pricing: pricing,
	}
	settled, err := flushCreditChargesDetailed(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge})
	if err != nil || !settled[creditChargeKey(charge)] {
		t.Fatalf("late settlement flush: settled=%v err=%v", settled, err)
	}

	stored, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := llmservice.BillingLedgerEntryForRequest(stored, "late-request")
	if !ok {
		t.Fatal("late settlement did not write the ledger entry")
	}
	if entry.DeductedCredits != 2.28 {
		t.Fatalf("late settlement deducted %v, want the real usage 2.28", entry.DeductedCredits)
	}
	for _, reservation := range stored.BillingReservations {
		if strings.EqualFold(reservation.RequestID, "late-request") {
			t.Fatalf("reservation row survived settlement: %#v", reservation)
		}
	}
	if got := llmservice.AvailableCreditsForServiceGroupsForUserID(stored, "u1", "user@example.com", []string{"paid"}, now); got != 7.72 {
		t.Fatalf("available after late settlement = %v, want 7.72 (10 - 2.28)", got)
	}

	// Replaying the same request must not debit a second time.
	replay := &pendingCreditCharge{
		userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2.28,
		requestID: "late-request", providerID: "third-party",
		usage:   corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 5_000, TotalTokens: 15_000, Requests: 1},
		pricing: pricing, providerMultiplier: 1, serviceGroupMultiplier: 1,
	}
	settled, err = flushCreditChargesDetailed(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(replay): replay})
	if err != nil {
		t.Fatalf("replay flush: %v", err)
	}
	if settled[creditChargeKey(replay)] {
		t.Fatal("replayed late request was debited a second time")
	}
	stored, err = llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if got := llmservice.AvailableCreditsForServiceGroupsForUserID(stored, "u1", "user@example.com", []string{"paid"}, now); got != 7.72 {
		t.Fatalf("available after replay = %v, want unchanged 7.72", got)
	}
}

func TestLLMUsageReportScopesUnitemizedLegacySettlements(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	// One directionally priced request and one legacy token-count settlement in
	// the same row: the tooltip must show the legacy share's own token scope so
	// the itemized legs plus the unitemized scope reconcile with the row totals.
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens: 10_000, OutputTokens: 2_000, TotalTokens: 12_000, Requests: 1,
	}, 5, &llmUsageCreditBreakdown{InputComponent: 1, OutputComponent: 4, RMBPricingRecorded: true}, "provider-a")
	rep.addUsageWithCreditBreakdown(ts, "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens: 90_000, OutputTokens: 8_000, TotalTokens: 98_000, Requests: 1,
	}, 3, &llmUsageCreditBreakdown{UnitemizedComponent: 3, ProviderID: "provider-a", ProviderMultiplier: 1}, "provider-a")

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if resp.Summary.CreditUnitemizedComponent != 3 {
		t.Fatalf("unitemized credits = %v, want 3", resp.Summary.CreditUnitemizedComponent)
	}
	if resp.Summary.UnitemizedRequests != 1 || resp.Summary.UnitemizedInputTokens != 90_000 || resp.Summary.UnitemizedOutputTokens != 8_000 {
		t.Fatalf("unitemized scope = %+v, want 1 request with 90000/8000 tokens", resp.Summary)
	}
	// The priced request must not leak into the unitemized scope, and the row
	// totals still cover both requests.
	if resp.Summary.UnitemizedInputTokens+resp.Summary.PricedNormalInputTokens > resp.Summary.InputTokens {
		t.Fatalf("scopes exceed row totals: %+v", resp.Summary)
	}
	if resp.Summary.InputTokens != 100_000 || resp.Summary.OutputTokens != 10_000 || resp.Summary.Requests != 2 {
		t.Fatalf("row totals = %+v, want 100000/10000 over 2 requests", resp.Summary)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].UnitemizedRequests != 1 || resp.Rows[0].UnitemizedInputTokens != 90_000 {
		t.Fatalf("row unitemized scope = %+v", resp.Rows)
	}
}

func usageReportHasEntity(resp llmUsageReportResponse, id string) bool {
	for _, item := range resp.Entities {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestLLMUsageReportPinsSystemUserOutsideRanking(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	rep.addUsage(ts, "user@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  100,
		OutputTokens: 20,
		TotalTokens:  120,
		Requests:     1,
	}, 0.5)
	rep.addUsage(ts, llmservice.SystemLLMUserEmail, nil, corelib.TokenUsageStat{
		InputTokens:  40,
		OutputTokens: 10,
		TotalTokens:  50,
		Requests:     2,
	}, 0.2)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if resp.SystemUser == nil {
		t.Fatal("system_user missing")
	}
	if resp.SystemUser.ID != llmservice.SystemLLMUserEmail || resp.SystemUser.Name != llmservice.SystemLLMUserEmail {
		t.Fatalf("system_user identity = %#v", resp.SystemUser)
	}
	if resp.SystemUser.TotalTokens != 50 || resp.SystemUser.Requests != 2 || resp.SystemUser.Credits != 0.2 {
		t.Fatalf("system_user totals = %#v", resp.SystemUser)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].ID != "user@example.com" {
		t.Fatalf("ranking should exclude sys_user: %#v", resp.Rows)
	}
	if !usageReportHasEntity(resp, llmservice.SystemLLMUserEmail) {
		t.Fatalf("entities missing sys_user: %#v", resp.Entities)
	}

	empty := buildLLMUsageReportResponse(context.Background(), &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if empty.SystemUser == nil || empty.SystemUser.ID != llmservice.SystemLLMUserEmail || empty.SystemUser.TotalTokens != 0 {
		t.Fatalf("empty day should still pin sys_user zeros: %#v", empty.SystemUser)
	}
	if len(empty.Rows) != 0 {
		t.Fatalf("empty ranking = %#v", empty.Rows)
	}
	if !usageReportHasEntity(empty, llmservice.SystemLLMUserEmail) {
		t.Fatalf("empty day entities missing sys_user: %#v", empty.Entities)
	}

	nilStore := buildLLMUsageReportResponse(context.Background(), nil, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if nilStore.SystemUser == nil || nilStore.SystemUser.ID != llmservice.SystemLLMUserEmail || nilStore.SystemUser.TotalTokens != 0 {
		t.Fatalf("nil store should still pin sys_user zeros: %#v", nilStore.SystemUser)
	}
	if !usageReportHasEntity(nilStore, llmservice.SystemLLMUserEmail) {
		t.Fatalf("nil store entities missing sys_user: %#v", nilStore.Entities)
	}

	nilDays := buildLLMUsageReportResponse(context.Background(), &llmUsageReportsStore{Version: llmUsageReportsVersion}, nil, "user", "monthly", "", "2026-04", "", ts)
	if nilDays.SystemUser == nil || nilDays.SystemUser.TotalTokens != 0 {
		t.Fatalf("nil Days should not panic and should pin zeros: %#v", nilDays.SystemUser)
	}

	lookalikeRep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	lookalikeRep.addUsage(ts, "sys_user_bot@example.com", nil, corelib.TokenUsageStat{TotalTokens: 9, Requests: 1}, 0)
	lookalike := buildLLMUsageReportResponse(context.Background(), lookalikeRep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if lookalike.SystemUser == nil || lookalike.SystemUser.TotalTokens != 0 {
		t.Fatalf("lookalike email must not fill sys_user: %#v", lookalike.SystemUser)
	}
	if len(lookalike.Rows) != 1 || lookalike.Rows[0].ID != "sys_user_bot@example.com" {
		t.Fatalf("lookalike email should stay in ranking: %#v", lookalike.Rows)
	}

	filtered := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "user@example.com", ts)
	if filtered.SystemUser == nil || filtered.SystemUser.TotalTokens != 50 {
		t.Fatalf("filtered user view should still report sys_user: %#v", filtered.SystemUser)
	}
	if len(filtered.Rows) != 1 || filtered.Rows[0].ID != "user@example.com" {
		t.Fatalf("filtered ranking = %#v", filtered.Rows)
	}

	self := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", llmservice.SystemLLMUserEmail, ts)
	if self.SystemUser == nil || self.SystemUser.TotalTokens != 50 {
		t.Fatalf("sys_user filter missing pinned row: %#v", self.SystemUser)
	}
	if len(self.Rows) != 1 || self.Rows[0].ID != llmservice.SystemLLMUserEmail || self.Rows[0].TotalTokens != 50 {
		t.Fatalf("sys_user filter ranking = %#v", self.Rows)
	}

	group := buildLLMUsageReportResponse(context.Background(), rep, nil, "group", "daily", "2026-04-21", "2026-04", "", ts)
	if group.SystemUser != nil {
		t.Fatalf("group scope should not pin sys_user: %#v", group.SystemUser)
	}

	monthly := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "monthly", "", "2026-04", "", ts)
	if monthly.SystemUser == nil || monthly.SystemUser.TotalTokens != 50 {
		t.Fatalf("monthly sys_user = %#v", monthly.SystemUser)
	}
	if len(monthly.Rows) != 1 || monthly.Rows[0].ID != "user@example.com" {
		t.Fatalf("monthly ranking should exclude sys_user: %#v", monthly.Rows)
	}
}
