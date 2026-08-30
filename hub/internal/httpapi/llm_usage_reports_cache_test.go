package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
