package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestValidOfficialBillingAttemptRejectsIncompleteOrUnsafeFacts(t *testing.T) {
	valid := llmservice.OfficialBillingAttempt{
		StatusCode: http.StatusOK,
		PricingSnapshot: llmpool.TokenPricingSnapshot{
			ProviderID: "official-provider",
			Pricing:    llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
		},
	}
	if !validOfficialBillingAttempt(valid) {
		t.Fatal("valid billing attempt was rejected")
	}
	for name, mutate := range map[string]func(*llmservice.OfficialBillingAttempt){
		"failed upstream status": func(a *llmservice.OfficialBillingAttempt) { a.StatusCode = http.StatusBadRequest },
		"missing provider":       func(a *llmservice.OfficialBillingAttempt) { a.PricingSnapshot.ProviderID = "" },
		"negative input":         func(a *llmservice.OfficialBillingAttempt) { a.PricingSnapshot.InputTokens = -1 },
		"token overflow": func(a *llmservice.OfficialBillingAttempt) {
			a.PricingSnapshot.InputTokens, a.PricingSnapshot.OutputTokens = math.MaxInt64, 1
		},
		"invalid pricing":     func(a *llmservice.OfficialBillingAttempt) { a.PricingSnapshot.Pricing.InputCreditsPer10K = -1 },
		"negative multiplier": func(a *llmservice.OfficialBillingAttempt) { a.PricingSnapshot.ProviderMultiplier = -1 },
		"infinite multiplier": func(a *llmservice.OfficialBillingAttempt) { a.PricingSnapshot.ProviderMultiplier = math.Inf(1) },
		"not-a-number multiplier": func(a *llmservice.OfficialBillingAttempt) {
			a.PricingSnapshot.ProviderMultiplier = math.NaN()
		},
	} {
		t.Run(name, func(t *testing.T) {
			attempt := valid
			mutate(&attempt)
			if validOfficialBillingAttempt(attempt) {
				t.Fatal("unsafe billing attempt was accepted")
			}
		})
	}
}

func TestSettledUsageCreditBreakdownUsesActualPricing(t *testing.T) {
	breakdown := settledUsageCreditBreakdown(
		corelib.TokenUsageStat{InputTokens: 20_000, OutputTokens: 5_000},
		8,
		1,
		2,
		&llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:    1,
			OutputCreditsPer10K:   4,
			MinimumRequestCredits: 0.1,
		}},
	)
	if breakdown == nil || breakdown.InputComponent != 4 || breakdown.OutputComponent != 4 || breakdown.MinimumAdjustment != 0 || breakdown.RoundingAdjustment != 0 {
		t.Fatalf("settled breakdown = %#v, want input/output/minimum/rounding 4/4/0/0", breakdown)
	}
}

func TestSettledUsageCreditBreakdownIncludesServiceGroupMultiplier(t *testing.T) {
	breakdown := settledUsageCreditBreakdown(
		corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 5_000},
		4,
		1,
		2,
		&llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 2,
		}},
	)
	if breakdown == nil || breakdown.InputComponent != 2 || breakdown.OutputComponent != 2 || breakdown.RoundingAdjustment != 0 {
		t.Fatalf("settled breakdown = %#v, want input/output/rounding 2/2/0", breakdown)
	}
}

func TestSettledUsageCreditBreakdownMaterializesLegacyCacheDiscount(t *testing.T) {
	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K: 1, OutputCreditsPer10K: 2, Version: "legacy-v1",
	}}
	credits := llmservice.EstimateTokenPricingCreditsWithCache(10_000, 0, 10_000, 0, *pricing, 1)
	breakdown := settledUsageCreditBreakdown(corelib.TokenUsageStat{InputTokens: 10_000, CachedInputTokens: 10_000}, credits, 1, 1, pricing)
	if breakdown == nil || breakdown.CacheReadCreditsPer10K != 0.1 || breakdown.CacheReadComponent != 0.1 {
		t.Fatalf("legacy cache breakdown = %#v, want read price/component 0.1", breakdown)
	}
}

func TestSettledUsageCreditBreakdownAlwaysReconcilesToSettledCredits(t *testing.T) {
	pricing := llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K:    1.2345,
		OutputCreditsPer10K:   4.5678,
		MinimumRequestCredits: 0.3333,
	}}
	credits := llmservice.EstimateTokenPricingCredits(17, 9, pricing, 1.17)
	breakdown := settledUsageCreditBreakdown(corelib.TokenUsageStat{InputTokens: 17, OutputTokens: 9}, credits, 1, 1.17, &pricing)
	if breakdown == nil {
		t.Fatal("expected settled breakdown")
	}
	got := breakdown.InputComponent + breakdown.OutputComponent + breakdown.MinimumAdjustment + breakdown.RoundingAdjustment
	if math.Abs(got-credits) > 0.000000001 {
		t.Fatalf("breakdown = %.12f, settled credits = %.12f", got, credits)
	}
}

func TestReleaseUnsettledBillingReservationRequiresProofAfterSent(t *testing.T) {
	state := &llmBillingState{reservationHeld: true, upstreamSent: true}
	ctx := context.WithValue(context.Background(), llmBillingStateKey{}, state)
	if snapshotOfficialNoUpstreamDispatch(ctx) {
		t.Fatal("fresh sent billing state unexpectedly has no-dispatch proof")
	}
	noteOfficialNoUpstreamDispatch(ctx, true)
	if !snapshotOfficialNoUpstreamDispatch(ctx) {
		t.Fatal("no-dispatch proof was not recorded")
	}
	state.mu.Lock()
	release := state.reservationHeld && !state.settlementQueued && (!state.upstreamSent || state.noUpstreamDispatch)
	state.mu.Unlock()
	if !release {
		t.Fatal("confirmed pre-dispatch failure should release a sent reservation")
	}

	state = &llmBillingState{reservationHeld: true, upstreamSent: true}
	ctx = context.WithValue(context.Background(), llmBillingStateKey{}, state)
	state.mu.Lock()
	release = state.reservationHeld && !state.settlementQueued && (!state.upstreamSent || state.noUpstreamDispatch)
	state.mu.Unlock()
	if release {
		t.Fatal("ambiguous sent request must retain its reservation")
	}
}

func TestOfficialHTTPErrorNeverBecomesNoDispatchProof(t *testing.T) {
	state := &llmBillingState{reservationHeld: true, upstreamSent: true}
	ctx := context.WithValue(context.Background(), llmBillingStateKey{}, state)
	allProviderDispatchesProvenAbsent := true
	lastBody := []byte(`{"error":{"message":"invalid request"}}`)
	lastStatus := http.StatusBadRequest
	if lastBody != nil && lastStatus > 0 && allProviderDispatchesProvenAbsent {
		allProviderDispatchesProvenAbsent = false
	}
	if allProviderDispatchesProvenAbsent {
		noteOfficialNoUpstreamDispatch(ctx, true)
	}
	if snapshotOfficialNoUpstreamDispatch(ctx) {
		t.Fatal("HubCenter HTTP error must remain a dispatched, reconcilable attempt")
	}
}

func TestOfficialDispatchClearsNoUpstreamProof(t *testing.T) {
	state := &llmBillingState{reservationHeld: true, upstreamSent: true}
	ctx := context.WithValue(context.Background(), llmBillingStateKey{}, state)
	noteOfficialNoUpstreamDispatch(ctx, true)
	if !snapshotOfficialNoUpstreamDispatch(ctx) {
		t.Fatal("local refusal should record no-dispatch proof before any attempt leaves Hub")
	}
	noteOfficialDispatchObserved(ctx)
	if snapshotOfficialNoUpstreamDispatch(ctx) {
		t.Fatal("an observed dispatch must clear a local no-dispatch proof")
	}
	noteOfficialNoUpstreamDispatch(ctx, true)
	if snapshotOfficialNoUpstreamDispatch(ctx) {
		t.Fatal("a later local refusal must not erase an observed dispatch")
	}
	state.mu.Lock()
	release := state.reservationHeld && !state.settlementQueued && (!state.upstreamSent || state.noUpstreamDispatch)
	state.mu.Unlock()
	if release {
		t.Fatal("sent request with an observed dispatch must keep its reservation")
	}
}

func TestOfficialBillingHeaderRejectsUnsafeTokenPricingSnapshot(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "unsafe-official-snapshot")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "official-provider", InputTokens: -1,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	if snapshot := snapshotOfficialTokenPricing(ctx); snapshot != nil {
		t.Fatalf("unsafe online snapshot was retained: %#v", snapshot)
	}
}

func TestEnqueueLLMUsageRecordWritesSQLClassColumns(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-usage-class.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, usage: st.LLMUsage, billing: st.LLMBillingLedger}

	enqueueLLMUsageRecord(system, "maclaw_official", corelib.TokenUsageStat{InputTokens: 20, OutputTokens: 5, TotalTokens: 25, Requests: 1}, "u1", "user@example.com", []string{"coding-auto", "writing-auto"}, nil, 2, llmservice.OfficialForwardMeta{
		WorkloadClass: llmpool.WorkloadClassPlan,
		ClassSource:   llmpool.ClassSourceHint,
		ResolvedModel: llmpool.OfficialTierHigh,
		Preview:       "write a product plan",
	})

	coding, err := st.LLMUsage.ListByGroupClass(t.Context(), store.DefaultTenantID, "coding-auto", llmpool.WorkloadClassPlan)
	if err != nil {
		t.Fatal(err)
	}
	writing, err := st.LLMUsage.ListByGroupClass(t.Context(), store.DefaultTenantID, "writing-auto", llmpool.WorkloadClassPlan)
	if err != nil {
		t.Fatal(err)
	}
	if len(coding) != 1 || len(writing) != 1 {
		t.Fatalf("rows coding=%d writing=%d", len(coding), len(writing))
	}
	if coding[0].WorkloadClass != llmpool.WorkloadClassPlan || coding[0].ServiceGroupID != "coding-auto" || coding[0].Model != llmpool.OfficialTierHigh {
		t.Fatalf("coding row: %#v", coding[0])
	}
	if writing[0].ServiceGroupID != "writing-auto" || writing[0].ClassSource != llmpool.ClassSourceHint {
		t.Fatalf("writing row: %#v", writing[0])
	}
}

func TestFlushCreditChargesPersistsRequestLedgerAndIsIdempotent(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, usage: st.LLMUsage, billing: st.LLMBillingLedger}
	now := time.Now().UTC()
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, Grants: []llmservice.Grant{{ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid", CreditsTotal: 10, Permanent: true, StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0)}}}
	if err := llmservice.SaveRegistry(t.Context(), system, reg); err != nil {
		t.Fatal(err)
	}
	charge := &pendingCreditCharge{userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2, requestID: "req-1", providerID: "p1", providerMultiplier: 0.5, serviceGroupMultiplier: 2, usage: corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 5}}
	for range 2 {
		if err := flushCreditCharges(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BillingLedger) != 1 || got.Grants[0].CreditsUsed != 2 {
		t.Fatalf("ledger=%#v grant=%#v", got.BillingLedger, got.Grants[0])
	}
	if got.BillingLedger[0].RequestedMicrocredits != 2*llmpool.MicrocreditsPerCredit || got.BillingLedger[0].DeductedMicrocredits != 2*llmpool.MicrocreditsPerCredit {
		t.Fatalf("ledger microcredits=%#v", got.BillingLedger[0])
	}
	var sqlRows int
	if err := provider.Read.QueryRow(`SELECT COUNT(*) FROM llm_billing_ledger WHERE tenant_id = ? AND request_id = ?`, store.DefaultTenantID, "req-1").Scan(&sqlRows); err != nil {
		t.Fatal(err)
	}
	if sqlRows != 1 {
		t.Fatalf("SQL ledger rows=%d, want 1", sqlRows)
	}
	if got.BillingLedger[0].ProviderMultiplier != 0.5 || got.BillingLedger[0].BillingGroupMultiplier != 2 {
		t.Fatalf("ledger multipliers=%#v", got.BillingLedger[0])
	}
	var providerMultiplier, groupMultiplier float64
	if err := provider.Read.QueryRow(`SELECT provider_multiplier, billing_group_multiplier FROM llm_billing_ledger WHERE tenant_id = ? AND request_id = ?`, store.DefaultTenantID, "req-1").Scan(&providerMultiplier, &groupMultiplier); err != nil {
		t.Fatal(err)
	}
	if providerMultiplier != 0.5 || groupMultiplier != 2 {
		t.Fatalf("SQL ledger multipliers=(%v, %v), want (0.5, 2)", providerMultiplier, groupMultiplier)
	}
}

func TestFlushCreditChargesReplayRestoresFrozenPricingForUsageReporting(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-ledger-pricing-replay.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, usage: st.LLMUsage, billing: st.LLMBillingLedger}
	now := time.Now().UTC()
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, Grants: []llmservice.Grant{{ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid", CreditsTotal: 10, Permanent: true, StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0)}}}
	if err := llmservice.SaveRegistry(t.Context(), system, reg); err != nil {
		t.Fatal(err)
	}
	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2, InputRMBPer10K: 0.02, OutputRMBPer10K: 0.06}}
	first := &pendingCreditCharge{userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2, requestID: "req-priced-replay", providerID: "p1", usage: corelib.TokenUsageStat{InputTokens: 120, OutputTokens: 80, TotalTokens: 200}, providerMultiplier: 1.5, serviceGroupMultiplier: 2, pricing: pricing}
	if err := flushCreditCharges(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(first): first}); err != nil {
		t.Fatal(err)
	}
	// Simulate process recovery: the replayed response-path charge no longer has
	// its in-memory billing provenance, but the ledger has the immutable
	// settlement snapshot.
	replay := &pendingCreditCharge{userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2, requestID: "req-priced-replay"}
	if err := flushCreditCharges(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(replay): replay}); err != nil {
		t.Fatal(err)
	}
	if replay.pricing == nil || replay.pricing.InputRMBPer10K != 0.02 || replay.pricing.OutputRMBPer10K != 0.06 {
		t.Fatalf("replayed charge pricing = %#v, want frozen ledger pricing", replay.pricing)
	}
	if replay.providerID != "p1" || replay.providerMultiplier != 1.5 || replay.serviceGroupMultiplier != 2 {
		t.Fatalf("replayed charge billing provenance = %#v, want ledger provider and multipliers", replay)
	}
	if replay.usage.InputTokens != 120 || replay.usage.OutputTokens != 80 || replay.usage.TotalTokens != 200 {
		t.Fatalf("replayed charge usage = %#v, want frozen ledger token totals", replay.usage)
	}
}

func TestFlushCreditChargesExposesActualDeductedCredits(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-ledger-partial.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, usage: st.LLMUsage, billing: st.LLMBillingLedger}
	now := time.Now().UTC()
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, Grants: []llmservice.Grant{{ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid", CreditsTotal: 1, Permanent: true, StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0)}}}
	if err := llmservice.SaveRegistry(t.Context(), system, reg); err != nil {
		t.Fatal(err)
	}
	charge := &pendingCreditCharge{userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2, requestID: "req-partial", providerID: "p1"}
	if err := flushCreditCharges(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge}); err != nil {
		t.Fatal(err)
	}
	if charge.credits != 1 {
		t.Fatalf("charge credits = %v, want actual debit 1", charge.credits)
	}
	got, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BillingLedger) != 1 || got.BillingLedger[0].RequestedCredits != 2 || got.BillingLedger[0].DeductedCredits != 1 {
		t.Fatalf("ledger = %#v", got.BillingLedger)
	}
}

func TestFlushCreditChargesFinalizesAndReleasesReservation(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-reservation.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, billing: st.LLMBillingLedger}
	now := time.Now().UTC()
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, Grants: []llmservice.Grant{{ID: "g", UserID: "u", Email: "u@example.com", ServiceGroupID: "paid", CreditsTotal: 10, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}}
	if _, ok := llmservice.ReserveBillingCreditsForUserID(reg, "u", "u@example.com", []string{"paid"}, "req", 8, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if err := llmservice.SaveRegistry(t.Context(), system, reg); err != nil {
		t.Fatal(err)
	}
	charge := &pendingCreditCharge{userID: "u", email: "u@example.com", serviceGroupIDs: []string{"paid"}, credits: 2, requestID: "req", providerID: "p"}
	if err := flushCreditCharges(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge}); err != nil {
		t.Fatal(err)
	}
	got, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BillingReservations) != 0 || got.Grants[0].CreditsUsed != 2 || len(got.BillingLedger) != 1 {
		t.Fatalf("finalized registry=%#v", got)
	}
}

func TestFlushCreditChargesFinalizesZeroUsageReservation(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-zero-reservation.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, billing: st.LLMBillingLedger}
	now := time.Now().UTC()
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, Grants: []llmservice.Grant{{ID: "g", UserID: "u", Email: "u@example.com", ServiceGroupID: "paid", CreditsTotal: 10, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}}
	if _, ok := llmservice.ReserveBillingCreditsForUserID(reg, "u", "u@example.com", []string{"paid"}, "req-zero", 8, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if err := llmservice.SaveRegistry(t.Context(), system, reg); err != nil {
		t.Fatal(err)
	}
	charge := &pendingCreditCharge{userID: "u", email: "u@example.com", serviceGroupIDs: []string{"paid"}, requestID: "req-zero", providerID: "p"}
	if err := flushCreditCharges(t.Context(), system, map[string]*pendingCreditCharge{creditChargeKey(charge): charge}); err != nil {
		t.Fatal(err)
	}
	got, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BillingReservations) != 0 || got.Grants[0].CreditsUsed != 0 || len(got.BillingLedger) != 1 || got.BillingLedger[0].DeductedCredits != 0 {
		t.Fatalf("zero usage must be finalized, registry=%#v", got)
	}
}

func TestEnqueueLLMUsageRecordFinalizesZeroUsageRequestReservation(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-enqueue-zero-reservation.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, billing: st.LLMBillingLedger}
	now := time.Now().UTC()
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, Grants: []llmservice.Grant{{ID: "g", UserID: "u", Email: "u@example.com", ServiceGroupID: "paid", CreditsTotal: 10, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}}
	if _, ok := llmservice.ReserveBillingCreditsForUserID(reg, "u", "u@example.com", []string{"paid"}, "req-enqueue-zero", 8, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if err := llmservice.SaveRegistry(t.Context(), system, reg); err != nil {
		t.Fatal(err)
	}
	enqueueLLMUsageRecordWithBilling(system, "p", corelib.TokenUsageStat{Requests: 1}, "u", "u@example.com", []string{"paid"}, nil, 0, llmservice.OfficialForwardMeta{}, "req-enqueue-zero", 2, 2, 1, nil, nil)
	got, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BillingReservations) != 0 || len(got.BillingLedger) != 1 || got.BillingLedger[0].RequestedCredits != 0 {
		t.Fatalf("zero request settlement must release reservation, registry=%#v", got)
	}
}

func TestComputeLLMRequestBillingDoesNotChargeExplicitFreeRoute(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		CreditMultiplier: 9, // A legacy multiplier must not revive billing.
		ProviderBillingModes: map[string]string{
			"provider-free": llmpool.BillingModeFree,
		},
	}
	credits, multiplier := computeLLMRequestBilling(
		t.Context(), model, "provider-free", nil, nil, nil,
		corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 10_000, TotalTokens: 20_000},
		llmservice.DefaultTokensPerCredit,
	)
	if credits != 0 || multiplier != 1 {
		t.Fatalf("free route billing = credits=%v multiplier=%v, want 0 and 1", credits, multiplier)
	}
}

func TestComputeLLMRequestBillingUsesOfficialProviderAndHubGroupMultipliers(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC), "req-official-price")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider-a")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID:         "official-provider-a",
		UpstreamModel:      "opencode-1",
		ProviderMultiplier: 0.5,
		InputTokens:        20_000,
		OutputTokens:       5_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 4,
		}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	serviceReg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID:                     "official-group",
		BillingGroupMultiplier: 2,
	}}}
	credits, multiplier := computeLLMRequestBilling(
		ctx,
		&llmservice.AuthorizedModel{},
		llmservice.MaClawOfficialProviderID,
		nil,
		serviceReg,
		[]string{"official-group"},
		corelib.TokenUsageStat{InputTokens: 20_000, OutputTokens: 5_000, TotalTokens: 25_000},
		llmservice.DefaultTokensPerCredit,
	)
	if multiplier != 1 || credits != 4 {
		t.Fatalf("official billing = credits=%v multiplier=%v, want 4 and 1", credits, multiplier)
	}
}

func TestComputeLLMRequestBillingUsesSyncedHubCenterMultiplierWithoutSnapshot(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)
	access := llmservice.NewTenantLLMAccessControl(nil)
	access.UpdateOfficialProviderBilling([]llmpool.ProviderBillingPolicy{{
		ProviderID:       "hubcenter-provider-a",
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1.5,
	}})
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: access})

	ctx := withLLMBillingState(t.Context(), time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC), "req-official-legacy-snapshot")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "hubcenter-provider-a")
	// Provider ID without a HubCenter price snapshot: keep the synced
	// multiplier, but do not invent input/cache/output prices on Hub.
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	serviceReg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID:                     "official-group",
		BillingGroupMultiplier: 4,
	}}}
	model := &llmservice.AuthorizedModel{
		Name:                   "official",
		ProviderTokenPricing:   map[string]llmpool.TokenPricing{llmservice.MaClawOfficialProviderID: {InputCreditsPer10K: 2, OutputCreditsPer10K: 8}},
		ProviderUpstreamModels: map[string]string{llmservice.MaClawOfficialProviderID: "opencode-1"},
	}
	credits, multiplier := computeLLMRequestBilling(
		ctx, model, llmservice.MaClawOfficialProviderID, nil, serviceReg, []string{"official-group"},
		corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 1_000, TotalTokens: 11_000}, llmservice.DefaultTokensPerCredit,
	)
	if credits != 0 || multiplier != 6 {
		t.Fatalf("official billing without HubCenter price = credits=%v multiplier=%v, want 0 and 6", credits, multiplier)
	}
	providerMultiplier, groupMultiplier := llmUsageReportMultipliers(ctx, llmservice.MaClawOfficialProviderID, serviceReg, []string{"official-group"}, multiplier)
	if providerMultiplier != 1.5 || groupMultiplier != 4 {
		t.Fatalf("legacy official fallback usage multipliers = provider %v group %v, want 1.5 and 4", providerMultiplier, groupMultiplier)
	}
	if got := usageReportBillingProviderID(ctx, llmservice.MaClawOfficialProviderID); got != "hubcenter-provider-a" {
		t.Fatalf("legacy official fallback tooltip provider = %q, want hubcenter-provider-a", got)
	}
}

func TestComputeLLMRequestBillingDoesNotUseHubLocalOfficialPrices(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-hub-local-price")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", BillingGroupMultiplier: 1}}}
	model := &llmservice.AuthorizedModel{
		Name: "official",
		ProviderTokenPricing: map[string]llmpool.TokenPricing{
			llmservice.MaClawOfficialProviderID: {InputCreditsPer10K: 1, OutputCreditsPer10K: 2},
		},
	}
	usage := corelib.TokenUsageStat{InputTokens: 10_000, CachedInputTokens: 8_000, OutputTokens: 2_000, TotalTokens: 12_000}
	credits, _ := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"official-group"}, usage, llmservice.DefaultTokensPerCredit)
	if credits != 0 {
		t.Fatalf("official debit = %v; Hub-local prices and tokens-per-credit must not replace upstream provider input/cache/output prices", credits)
	}
}

func TestChargeLoggedOfficialUsageWithoutUpstreamPriceKeepsReservation(t *testing.T) {
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: filepath.Join(t.TempDir(), "llm-official-no-price.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	system := userReferralMetricSystemSettings{SystemSettingsRepository: st.System, billing: st.LLMBillingLedger}
	now := time.Now().UTC()
	requestID := "req-official-no-price"
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		Grants: []llmservice.Grant{{
			ID: "g", UserID: "u", Email: "u@example.com", ServiceGroupID: "official-group",
			CreditsTotal: 10, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		}},
	}
	if _, ok := llmservice.ReserveBillingCreditsForUserID(reg, "u", "u@example.com", []string{"official-group"}, requestID, 8, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if !llmservice.MarkBillingReservationSent(reg, requestID, now) {
		t.Fatal("mark sent")
	}
	if !llmservice.SetBillingReservationBillingDetails(reg, requestID, llmservice.MaClawOfficialProviderID, 1, 1) {
		t.Fatal("billing details")
	}
	if err := llmservice.SaveRegistry(t.Context(), system, reg); err != nil {
		t.Fatal(err)
	}
	ctx := withLLMBillingState(t.Context(), now, requestID)
	credits, _ := chargeLoggedLLMEndpointUsage(ctx, system, nil, "u", "u@example.com", llmservice.MaClawOfficialProviderID, &llmservice.AuthorizedModel{}, nil, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", BillingGroupMultiplier: 1}}}, corelib.TokenUsageStat{InputTokens: 10_000, CachedInputTokens: 8_000, OutputTokens: 2_000, TotalTokens: 12_000, Requests: 1}, []string{"official-group"})
	if credits != 0 {
		t.Fatalf("credits=%v, want 0 without upstream provider price", credits)
	}
	got, err := llmservice.LoadRegistry(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	if llmservice.HasBillingRequest(got, requestID) {
		t.Fatalf("zero debit must not settle and block HubCenter reconciliation: %#v", got.BillingLedger)
	}
	if len(got.BillingReservations) != 1 {
		t.Fatalf("sent official reservation = %#v, want kept for reconciliation", got.BillingReservations)
	}
}

func TestComputeLLMRequestBillingKeepsOfficialGroupMultiplierFrozenAfterAdmission(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC), "req-official-frozen-multiplier")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", BillingGroupMultiplier: 2}}}
	model := &llmservice.AuthorizedModel{Name: "official", ProviderTokenPricing: map[string]llmpool.TokenPricing{
		llmservice.MaClawOfficialProviderID: {InputCreditsPer10K: 1, OutputCreditsPer10K: 4},
	}}
	quote := llmservice.OfficialPricingQuote{
		ProviderID: "official-provider-a", UpstreamModel: "opencode-1",
		Pricing:            llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
		ProviderMultiplier: 0.5,
		ExpiresAt:          time.Now().UTC().Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, model, quote, []string{"official-group"}, 1_000, 2_000); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	// A configuration change during the upstream call affects only new requests.
	reg.ModelServiceGroups[0].BillingGroupMultiplier = 9
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider-a")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "official-provider-a", UpstreamModel: "opencode-1", InputTokens: 10_000, OutputTokens: 1_000,
		ProviderMultiplier: 0.5,
		Pricing:            llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	credits, multiplier := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"official-group"}, corelib.TokenUsageStat{}, llmservice.DefaultTokensPerCredit)
	if credits != 1.4 || multiplier != 1 {
		t.Fatalf("frozen official multiplier billing = credits=%v multiplier=%v, want 1.4 and 1", credits, multiplier)
	}
}

func TestComputeLLMRequestBillingUsesFinalOfficialProviderMultiplier(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC), "req-official-final-provider-multiplier")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", BillingGroupMultiplier: 2}}}
	model := &llmservice.AuthorizedModel{Name: "official", ProviderTokenPricing: map[string]llmpool.TokenPricing{
		llmservice.MaClawOfficialProviderID: {InputCreditsPer10K: 1, OutputCreditsPer10K: 4},
	}}
	quote := llmservice.OfficialPricingQuote{
		ProviderID: "official-provider-a", UpstreamModel: "opencode-1",
		Pricing:            llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
		ProviderMultiplier: 0.5,
		ExpiresAt:          time.Now().UTC().Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, model, quote, []string{"official-group"}, 1_000, 2_000); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider-a")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "official-provider-a", UpstreamModel: "opencode-1", InputTokens: 10_000, OutputTokens: 1_000,
		// HubCenter may apply a new quoted provider factor for a sanitized
		// compatibility retry. Final settlement must honor this authenticated fact.
		ProviderMultiplier: 1.5,
		Pricing:            llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	credits, multiplier := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"official-group"}, corelib.TokenUsageStat{}, llmservice.DefaultTokensPerCredit)
	if credits != 4.2 || multiplier != 3 {
		t.Fatalf("final official multiplier billing = credits=%v multiplier=%v, want 4.2 and 3", credits, multiplier)
	}
	providerMultiplier, groupMultiplier := llmUsageReportMultipliers(ctx, llmservice.MaClawOfficialProviderID, reg, []string{"official-group"}, multiplier)
	if providerMultiplier != 1.5 || groupMultiplier != 2 {
		t.Fatalf("usage report multipliers = provider %v group %v, want 1.5 and 2", providerMultiplier, groupMultiplier)
	}
}

func TestChargeLoggedOfficialUsageUsesAuthenticatedSnapshotInsteadOfResponseBodyUsage(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-usage-snapshot")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider-a")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "official-provider-a", InputTokens: 12_000, OutputTokens: 3_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	credits, multiplier := chargeLoggedLLMEndpointUsage(ctx, nil, nil, "u", "u@example.com", llmservice.MaClawOfficialProviderID, &llmservice.AuthorizedModel{}, nil, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official", BillingGroupMultiplier: 2}}}, corelib.TokenUsageStat{Requests: 1}, []string{"official"})
	if credits != 4.8 || multiplier != 2 {
		t.Fatalf("official billing = credits=%v multiplier=%v, want 4.8 and 2", credits, multiplier)
	}
}

func TestUsageReportBillingProviderUsesAuthenticatedOfficialSnapshot(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-tooltip-provider")
	header := make(http.Header)
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "hubcenter-provider-a", InputTokens: 12, OutputTokens: 3,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	if got := usageReportBillingProviderID(ctx, llmservice.MaClawOfficialProviderID); got != "hubcenter-provider-a" {
		t.Fatalf("tooltip billing provider = %q, want authenticated HubCenter provider", got)
	}
	if got := usageReportBillingProviderID(ctx, "local-provider"); got != "local-provider" {
		t.Fatalf("non-official tooltip billing provider = %q, want local provider", got)
	}
}

func TestEnqueueRecoveredUsageReportKeepsLedgerAmountAndHubCenterProvider(t *testing.T) {
	system := &testSystemSettingsRepo{}
	accumulator := &llmUsageAccumulator{pending: map[store.SystemSettingsRepository]*pendingSystemUsage{}}
	reportedAt := time.Date(2026, 8, 26, 9, 30, 0, 0, time.UTC)
	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 2,
		InputRMBPer10K:      0.02,
		OutputRMBPer10K:     0.06,
	}}
	usage := applyOfficialTokenPricingUsageSnapshot(corelib.TokenUsageStat{Requests: 1}, &llmpool.TokenPricingSnapshot{
		ProviderID: "hubcenter-provider-a", InputTokens: 10_000, OutputTokens: 5_000, Pricing: *pricing,
	})
	// A period limit allowed only part of the calculated 4-credit request. The
	// report must use the durable 3-credit debit and retain its actual provider.
	accumulator.enqueueRecoveredUsageReport(system, llmservice.MaClawOfficialProviderID, "hubcenter-provider-a", usage, "user@example.com", reportedAt, 3, 1, 2, pricing)
	pending := accumulator.pending[system]
	if pending == nil || pending.reports == nil {
		t.Fatal("recovered usage report was not queued")
	}
	resp := buildLLMUsageReportResponse(t.Context(), pending.reports, nil, "user", "daily", "2026-08-26", "2026-08", "", reportedAt)
	if resp.Summary.Credits != 3 || resp.Summary.TotalCostRMB != 0.05 || resp.Summary.RMBPricedInputTokens != 10_000 || resp.Summary.RMBPricedOutputTokens != 5_000 || resp.Summary.RMBPricedCredits != 3 || resp.Summary.RMBPricedRequests != 1 || len(resp.Summary.ProviderMultipliers) != 2 || len(resp.Summary.ProviderPricing) != 1 {
		t.Fatalf("recovered usage summary = %+v", resp.Summary)
	}
	if multiplier := resp.Summary.ProviderMultipliers[0]; multiplier.ProviderID != "hubcenter-provider-a" || multiplier.Multiplier != 1 || multiplier.MultiplierSource != "provider" {
		t.Fatalf("recovered provider multiplier = %#v", multiplier)
	}
	if multiplier := resp.Summary.ProviderMultipliers[1]; multiplier.ProviderID != "hubcenter-provider-a" || multiplier.Multiplier != 2 || multiplier.MultiplierSource != "service_group" {
		t.Fatalf("recovered service-group multiplier = %#v", multiplier)
	}
	if pricingFact := resp.Summary.ProviderPricing[0]; pricingFact.ProviderID != "hubcenter-provider-a" || pricingFact.InputRMBPer10K != 0.02 || pricingFact.OutputRMBPer10K != 0.06 {
		t.Fatalf("recovered provider pricing = %#v", pricingFact)
	}
}

func TestUsageReportSeparatesRMBReferenceCoverageFromHistoricalCredits(t *testing.T) {
	reports := &llmUsageReportsStore{Days: map[string]*llmUsageReportDay{}}
	reportedAt := time.Date(2026, 8, 26, 9, 30, 0, 0, time.UTC)
	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 2,
		InputRMBPer10K:      0.02,
		OutputRMBPer10K:     0.06,
	}}
	pricedUsage := applyOfficialTokenPricingUsageSnapshot(corelib.TokenUsageStat{Requests: 1}, &llmpool.TokenPricingSnapshot{
		ProviderID: "hubcenter-provider-a", InputTokens: 10_000, OutputTokens: 5_000, Pricing: *pricing,
	})
	breakdown := settledUsageCreditBreakdown(pricedUsage, 3, 1, 1, pricing)
	breakdown.ProviderID = "hubcenter-provider-a"
	reports.addUsageWithCreditBreakdown(reportedAt, "user@example.com", nil, pricedUsage, 3, breakdown, llmservice.MaClawOfficialProviderID)

	// The legacy record has a real settled debit but no frozen RMB price. It
	// must remain excluded from RMB reference-cost coverage rather than being
	// retroactively valued with today's HubCenter configuration.
	reports.addUsage(reportedAt, "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000_000, Requests: 1}, 1_000, llmservice.MaClawOfficialProviderID)

	resp := buildLLMUsageReportResponse(t.Context(), reports, nil, "user", "daily", "2026-08-26", "2026-08", "", reportedAt)
	summary := resp.Summary
	if summary.Credits != 1_003 || summary.TotalCostRMB != 0.05 {
		t.Fatalf("report totals = credits %v, RMB %v; want 1003 and 0.05", summary.Credits, summary.TotalCostRMB)
	}
	if summary.RMBPricedRequests != 1 || summary.RMBPricedCredits != 3 || summary.RMBPricedInputTokens != 10_000 || summary.RMBPricedOutputTokens != 5_000 {
		t.Fatalf("RMB coverage = %+v; want only the frozen-price request", summary)
	}
}

func TestUsageReportSummaryPricingUsesHubCenterProviderName(t *testing.T) {
	reports := &llmUsageReportsStore{Days: map[string]*llmUsageReportDay{}}
	pricing := &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
		InputRMBPer10K: 0.02, OutputRMBPer10K: 0.06,
	}}
	breakdown := settledUsageCreditBreakdown(corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 5_000}, 2, 1, 1, pricing)
	breakdown.ProviderID = "hubcenter-provider-a"
	reports.addUsageWithCreditBreakdown(time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC), "user@example.com", nil, corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 5_000, TotalTokens: 15_000, Requests: 1}, 2, breakdown, llmservice.MaClawOfficialProviderID)
	resp := buildLLMUsageReportResponse(t.Context(), reports, nil, "user", "daily", "2026-08-26", "2026-08", "", time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC), map[string]string{"hubcenter-provider-a": "HubCenter Provider A"})
	if len(resp.Summary.ProviderPricing) != 1 || resp.Summary.ProviderPricing[0].ProviderName != "HubCenter Provider A" {
		t.Fatalf("summary provider pricing = %#v", resp.Summary.ProviderPricing)
	}
	if len(resp.Trend) != 24 || len(resp.Trend[9].ProviderPricing) != 1 || resp.Trend[9].ProviderPricing[0].ProviderName != "HubCenter Provider A" {
		t.Fatalf("trend provider pricing = %#v", resp.Trend[9].ProviderPricing)
	}
}

func TestAuthoritativeOfficialUsageAppliesHubCenterRMBPricingSnapshot(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-rmb-pricing-snapshot")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider-a")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "official-provider-a", InputTokens: 1_000_000, OutputTokens: 500_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 4,
			InputRMBPer10K: 0.02, OutputRMBPer10K: 0.06,
		}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	usage := authoritativeLLMUsageForAccessLog(ctx, llmservice.MaClawOfficialProviderID, corelib.TokenUsageStat{})
	if usage.InputPricePerMTokensRMB != 2 || usage.OutputPricePerMTokensRMB != 6 {
		t.Fatalf("RMB prices per million = input %v, output %v; want 2, 6", usage.InputPricePerMTokensRMB, usage.OutputPricePerMTokensRMB)
	}
	if usage.InputCostRMB != 2 || usage.OutputCostRMB != 3 || usage.TotalCostRMB != 5 {
		t.Fatalf("RMB costs = input %v, output %v, total %v; want 2, 3, 5", usage.InputCostRMB, usage.OutputCostRMB, usage.TotalCostRMB)
	}
}

func TestAuthoritativeOfficialUsageWeightsHubCenterRMBPriceByProviderAndGroup(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-rmb-multipliers")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider-a")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "official-provider-a", ProviderMultiplier: 1.5, InputTokens: 1_000_000, OutputTokens: 500_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 4,
			InputRMBPer10K: 0.02, OutputRMBPer10K: 0.06,
		}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	usage := authoritativeLLMUsageForAccessLog(ctx, llmservice.MaClawOfficialProviderID, corelib.TokenUsageStat{})
	applyUsageRMBMultiplier(&usage, 2)
	if usage.InputPricePerMTokensRMB != 6 || usage.OutputPricePerMTokensRMB != 18 {
		t.Fatalf("weighted RMB prices per million = input %v, output %v; want 6, 18", usage.InputPricePerMTokensRMB, usage.OutputPricePerMTokensRMB)
	}
	if usage.InputCostRMB != 6 || usage.OutputCostRMB != 9 || usage.TotalCostRMB != 15 {
		t.Fatalf("weighted RMB costs = input %v, output %v, total %v; want 6, 9, 15", usage.InputCostRMB, usage.OutputCostRMB, usage.TotalCostRMB)
	}
}

func TestResolvedRouteUsageRMBPricingOverridesProviderDisplayPrice(t *testing.T) {
	usage := corelib.TokenUsageStat{
		InputTokens:              1_000_000,
		OutputTokens:             500_000,
		TotalTokens:              1_500_000,
		InputPricePerMTokensRMB:  0.01, // mutable provider-wide display price
		OutputPricePerMTokensRMB: 0.02,
	}
	pricing := llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputRMBPer10K:  0.02,
		OutputRMBPer10K: 0.06,
	}}

	priced := applyResolvedTokenPricingUsageSnapshot(usage, pricing, 1.5, 2)
	if priced.InputPricePerMTokensRMB != 6 || priced.OutputPricePerMTokensRMB != 18 {
		t.Fatalf("route RMB prices per million = input %v, output %v; want 6, 18", priced.InputPricePerMTokensRMB, priced.OutputPricePerMTokensRMB)
	}
	if priced.InputCostRMB != 6 || priced.OutputCostRMB != 9 || priced.TotalCostRMB != 15 {
		t.Fatalf("route RMB costs = input %v, output %v, total %v; want 6, 9, 15", priced.InputCostRMB, priced.OutputCostRMB, priced.TotalCostRMB)
	}
}

func TestResolvedRouteUsageRMBCostSeparatesCacheReadAndWrite(t *testing.T) {
	usage := corelib.TokenUsageStat{
		InputTokens:       1_000_000,
		CachedInputTokens: 200_000,
		CacheWriteTokens:  100_000,
		OutputTokens:      500_000,
		TotalTokens:       1_500_000,
	}
	pricing := llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
		InputRMBPer10K:      0.02,
		CacheReadRMBPer10K:  cacheRMBPrice(0.002),
		CacheWriteRMBPer10K: cacheRMBPrice(0.04),
		OutputRMBPer10K:     0.06,
	}}

	priced := applyResolvedTokenPricingUsageSnapshot(usage, pricing, 1.5, 2)
	if math.Abs(priced.InputCostRMB-4.2) > 1e-12 || math.Abs(priced.CacheReadCostRMB-0.12) > 1e-12 || math.Abs(priced.CacheWriteCostRMB-1.2) > 1e-12 || math.Abs(priced.OutputCostRMB-9) > 1e-12 {
		t.Fatalf("RMB components = input %.12f cache-read %.12f cache-write %.12f output %.12f; want 4.2/0.12/1.2/9", priced.InputCostRMB, priced.CacheReadCostRMB, priced.CacheWriteCostRMB, priced.OutputCostRMB)
	}
	if got, want := priced.TotalCostRMB, priced.InputCostRMB+priced.CacheReadCostRMB+priced.CacheWriteCostRMB+priced.OutputCostRMB; math.Abs(got-want) > 1e-12 {
		t.Fatalf("RMB total = %.12f, want component sum %.12f", got, want)
	}
}

func TestApplyOfficialTokenPricingUsageSnapshotDefaultsCacheRMBFromUpstreamInput(t *testing.T) {
	usage := applyOfficialTokenPricingUsageSnapshot(corelib.TokenUsageStat{Requests: 1}, &llmpool.TokenPricingSnapshot{
		ProviderID: "agnes", InputTokens: 10_000, CachedInputTokens: 8_000, OutputTokens: 2_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
			InputRMBPer10K: 0.01, OutputRMBPer10K: 0.02,
		}},
	})
	// Upstream cache RMB defaults: read = input/10, write = input.
	// Per 1M: input ¥1, cache read ¥0.1, output ¥2.
	if math.Abs(usage.InputCostRMB-0.002) > 1e-12 || math.Abs(usage.CacheReadCostRMB-0.0008) > 1e-12 || math.Abs(usage.CacheWriteCostRMB) > 1e-12 || math.Abs(usage.OutputCostRMB-0.004) > 1e-12 {
		t.Fatalf("official cache RMB = input %.12f read %.12f write %.12f output %.12f; want 0.002/0.0008/0/0.004", usage.InputCostRMB, usage.CacheReadCostRMB, usage.CacheWriteCostRMB, usage.OutputCostRMB)
	}
	if math.Abs(usage.TotalCostRMB-0.0068) > 1e-12 {
		t.Fatalf("official cache RMB total = %.12f, want 0.0068", usage.TotalCostRMB)
	}
}

func TestChargeLoggedOfficialSnapshotAppliesUpstreamCacheRMB(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-snapshot-rmb-cache")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "agnes")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "agnes", ProviderMultiplier: 1,
		InputTokens: 10_000, CachedInputTokens: 8_000, OutputTokens: 2_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
			InputRMBPer10K: 0.01, OutputRMBPer10K: 0.02,
		}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	system := &testSystemSettingsRepo{}
	t.Cleanup(func() {
		globalLLMUsageAccumulator.mu.Lock()
		delete(globalLLMUsageAccumulator.pending, system)
		globalLLMUsageAccumulator.mu.Unlock()
	})
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official", BillingGroupMultiplier: 2}}}
	credits, _ := chargeLoggedLLMEndpointUsage(ctx, system, nil, "u", "u@example.com", llmservice.MaClawOfficialProviderID, &llmservice.AuthorizedModel{}, nil, reg, corelib.TokenUsageStat{Requests: 1}, []string{"official"})
	if credits != 1.36 {
		t.Fatalf("official cache credits = %v, want 1.36 from upstream prices × service-group 2", credits)
	}
	summary := officialPendingUsageSummary(t, system)
	if math.Abs(summary.InputCostRMB-0.004) > 1e-12 || math.Abs(summary.CacheReadCostRMB-0.0016) > 1e-12 || math.Abs(summary.OutputCostRMB-0.008) > 1e-12 || math.Abs(summary.TotalCostRMB-0.0136) > 1e-12 {
		t.Fatalf("official snapshot RMB = %+v; want input 0.004 cache-read 0.0016 output 0.008 total 0.0136", summary)
	}
	if summary.RMBPricedRequests != 1 || summary.CreditUnitemizedComponent != 0 {
		t.Fatalf("official snapshot must keep RMB coverage and stay itemized: %+v", summary)
	}
}

func TestChargeLoggedOfficialSnapshotUsesQuoteProviderMultiplierWhenSnapshotOmitsIt(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(t.Context(), now, "req-official-snapshot-quote-mul")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official", BillingGroupMultiplier: 2}}}
	quote := llmservice.OfficialPricingQuote{
		ProviderID: "agnes",
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
			InputRMBPer10K: 0.01, OutputRMBPer10K: 0.02,
		}},
		ProviderMultiplier: 1.5,
		ExpiresAt:          now.Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}, quote, []string{"official"}, 10_000, 2_000); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	reg.ModelServiceGroups[0].BillingGroupMultiplier = 9
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "agnes")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "agnes", InputTokens: 10_000, OutputTokens: 2_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
			InputRMBPer10K: 0.01, OutputRMBPer10K: 0.02,
		}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	system := &testSystemSettingsRepo{}
	t.Cleanup(func() {
		globalLLMUsageAccumulator.mu.Lock()
		delete(globalLLMUsageAccumulator.pending, system)
		globalLLMUsageAccumulator.mu.Unlock()
	})
	credits, multiplier := chargeLoggedLLMEndpointUsage(ctx, system, nil, "u", "u@example.com", llmservice.MaClawOfficialProviderID, &llmservice.AuthorizedModel{}, nil, reg, corelib.TokenUsageStat{Requests: 1}, []string{"official"})
	if credits != 4.2 || multiplier != 3 {
		t.Fatalf("official settlement = credits=%v multiplier=%v, want 4.2 and 3 from quote 1.5 × frozen group 2", credits, multiplier)
	}
	summary := officialPendingUsageSummary(t, system)
	if math.Abs(summary.InputCostRMB-0.03) > 1e-12 || math.Abs(summary.OutputCostRMB-0.012) > 1e-12 || math.Abs(summary.TotalCostRMB-0.042) > 1e-12 {
		t.Fatalf("official RMB = %+v; want input 0.03 output 0.012 total 0.042", summary)
	}
}

func TestChargeLoggedOfficialQuoteAppliesUpstreamCacheRMBWithoutSnapshot(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(t.Context(), now, "req-official-quote-rmb-cache")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official", BillingGroupMultiplier: 1}}}
	quote := llmservice.OfficialPricingQuote{
		ProviderID: "agnes",
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
			InputRMBPer10K: 0.01, OutputRMBPer10K: 0.02,
		}},
		ProviderMultiplier: 1,
		ExpiresAt:          now.Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}, quote, []string{"official"}, 10_000, 2_000); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	system := &testSystemSettingsRepo{}
	t.Cleanup(func() {
		globalLLMUsageAccumulator.mu.Lock()
		delete(globalLLMUsageAccumulator.pending, system)
		globalLLMUsageAccumulator.mu.Unlock()
	})
	credits, _ := chargeLoggedLLMEndpointUsage(ctx, system, nil, "u", "u@example.com", llmservice.MaClawOfficialProviderID, &llmservice.AuthorizedModel{}, nil, reg, corelib.TokenUsageStat{InputTokens: 10_000, CachedInputTokens: 8_000, OutputTokens: 2_000, TotalTokens: 12_000, Requests: 1}, []string{"official"})
	if credits != 0.68 {
		t.Fatalf("official quote cache credits = %v, want 0.68", credits)
	}
	summary := officialPendingUsageSummary(t, system)
	if math.Abs(summary.InputCostRMB-0.002) > 1e-12 || math.Abs(summary.CacheReadCostRMB-0.0008) > 1e-12 || math.Abs(summary.OutputCostRMB-0.004) > 1e-12 || math.Abs(summary.TotalCostRMB-0.0068) > 1e-12 {
		t.Fatalf("official quote RMB = %+v; want input 0.002 cache-read 0.0008 output 0.004 total 0.0068", summary)
	}
}

func officialPendingUsageSummary(t *testing.T, system store.SystemSettingsRepository) llmUsageCounters {
	t.Helper()
	globalLLMUsageAccumulator.mu.Lock()
	pending := globalLLMUsageAccumulator.pending[system]
	globalLLMUsageAccumulator.mu.Unlock()
	if pending == nil || pending.reports == nil || len(pending.reports.Days) == 0 {
		t.Fatal("usage report was not queued")
	}
	var day string
	for key := range pending.reports.Days {
		day = key
		break
	}
	month := day
	if len(day) >= 7 {
		month = day[:7]
	}
	return buildLLMUsageReportResponse(t.Context(), pending.reports, nil, "user", "daily", day, month, "", time.Now()).Summary
}

func TestPrepareLLMPricingQuoteUsesConcreteUpstreamRoutePrice(t *testing.T) {
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
	}}, Grants: []llmservice.Grant{{
		ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
		CreditsTotal: 10, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
	}}}
	model := &llmservice.AuthorizedModel{
		Name:                   "logical-model",
		ProviderIDs:            []string{"p1"},
		ProviderServiceGroups:  map[string][]string{"p1": {"paid"}},
		ProviderBillingModes:   map[string]string{"p1": llmpool.BillingModePaid},
		ProviderUpstreamModels: map[string]string{"p1": "expensive-default"},
		ProviderTokenPricing:   map[string]llmpool.TokenPricing{"p1": {InputCreditsPer10K: 99, OutputCreditsPer10K: 99}},
		ProviderRouteBilling: map[string]map[string]llmservice.ProviderRouteBilling{"p1": {
			"expensive-default": {BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 9, OutputCreditsPer10K: 9}},
			"cheap-route":       {BillingMode: llmpool.BillingModePaid, TokenPricingOverride: true, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
		}},
		ProviderUpstreamRouteModels: map[string]map[string]string{"p1": {"logical-model": "cheap-route"}},
	}
	ctx := withLLMBillingState(t.Context(), now, "req-route-quote")
	denial, err := prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p1", map[string]any{"max_tokens": 1_000}, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("quote failed: denial=%#v err=%v", denial, err)
	}
	quote, ok := snapshotLLMPricingQuote(ctx, "p1")
	if !ok || quote.Pricing.InputCreditsPer10K != 1 || quote.Pricing.OutputCreditsPer10K != 2 {
		t.Fatalf("route quote=%#v ok=%v; want concrete cheap-route price", quote, ok)
	}
}

func TestPrepareLLMPricingQuoteRejectsInsufficientMaximumAndFreezesRoutePrice(t *testing.T) {
	now := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:                     "paid",
			AccessPolicy:           llmservice.AccessPolicyGrantRequired,
			BillingGroupMultiplier: 2,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 1, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	model := &llmservice.AuthorizedModel{
		ProviderIDs:           []string{"p1"},
		ProviderServiceGroups: map[string][]string{"p1": {"paid"}},
		ProviderBillingModes:  map[string]string{"p1": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"p1": {InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
	}
	ctx := withLLMBillingState(t.Context(), now, "req-quote")
	// The requested ceiling does not fit in 1 Credit, but the prompt does.
	// Admission lowers the forwarded ceiling instead of stopping the call.
	tightBody := map[string]any{"max_tokens": 2_000}
	denial, err := prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p1", tightBody, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("tight balance should keep working: denial=%#v err=%v", denial, err)
	}
	tightQuote, ok := snapshotLLMPricingQuote(ctx, "p1")
	if !ok || tightQuote.OutputTokenLimit <= 0 || tightQuote.OutputTokenLimit >= 2_000 {
		t.Fatalf("fitted ceiling = %#v ok=%v, want 1..1999", tightQuote, ok)
	}
	if tightQuote.ReservedMicrocredits > creditsToMicrocredits(1) {
		t.Fatalf("fitted reserve = %d microcredits, above the 1 Credit balance", tightQuote.ReservedMicrocredits)
	}
	if got, gotOK := llmQuotePositiveInt64(tightBody["max_tokens"]); !gotOK || got != tightQuote.OutputTokenLimit {
		t.Fatalf("forwarded max_tokens = %d ok=%v, want the fitted ceiling %d", got, gotOK, tightQuote.OutputTokenLimit)
	}
	// A prompt the balance cannot pay for is still refused, and its ceiling
	// stays at the caller's value because nothing will be forwarded.
	hugeBody := map[string]any{"max_tokens": 2_000, "messages": []any{map[string]any{"role": "user", "content": strings.Repeat("token ", 8000)}}}
	hugeCtx := withLLMBillingState(t.Context(), now, "req-quote-huge")
	denial, err = prepareLLMPricingQuote(hugeCtx, reg, nil, "u1", "user@example.com", model, "p1", hugeBody, now)
	if err == nil || denial.Code != "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST" {
		t.Fatalf("denial=%#v err=%v, want insufficient quote", denial, err)
	}
	if got, gotOK := llmQuotePositiveInt64(hugeBody["max_tokens"]); !gotOK || got != 2_000 {
		t.Fatalf("denied max_tokens = %d ok=%v, want the caller's 2000", got, gotOK)
	}

	reg.Grants[0].CreditsTotal = 10
	denial, err = prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p1", map[string]any{"max_tokens": 2_000}, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("quote failed: denial=%#v err=%v", denial, err)
	}
	quote, ok := snapshotLLMPricingQuote(ctx, "p1")
	if !ok || quote.OutputTokenLimit != 2_000 || quote.BillingGroupMultiplier != 2 {
		t.Fatalf("quote=%#v ok=%v", quote, ok)
	}
	// Mutating the live configuration after admission cannot alter settlement.
	model.ProviderTokenPricing["p1"] = llmpool.TokenPricing{InputCreditsPer10K: 99, OutputCreditsPer10K: 99}
	credits, multiplier := computeLLMRequestBilling(ctx, model, "p1", nil, reg, []string{"paid"}, corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 1_000}, llmservice.DefaultTokensPerCredit)
	if credits != 2.8 || multiplier != 2 {
		t.Fatalf("frozen quote billing = credits=%v multiplier=%v, want 2.8 and 2", credits, multiplier)
	}
}

func TestRememberOfficialPricingQuoteUsesLogicalModelAndFreezesDirectionalPrice(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(t.Context(), now, "req-official-quote")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", BillingGroupMultiplier: 2}}}
	model := &llmservice.AuthorizedModel{Name: "hub-public-model", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}
	quote := llmservice.OfficialPricingQuote{
		ProviderID:         "hubcenter-provider",
		UpstreamModel:      "upstream-model",
		Pricing:            llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
		ProviderMultiplier: 0.5,
		ExpiresAt:          now.Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, model, quote, []string{"official-group"}, 1_000, 2_000); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok || stored.LogicalModel != model.Name || stored.UpstreamModel != quote.UpstreamModel || stored.ProviderMultiplier != 0.5 || stored.BillingGroupMultiplier != 2 {
		t.Fatalf("stored quote = %#v, ok=%v", stored, ok)
	}
	credits, multiplier := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"official-group"}, corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 1_000}, llmservice.DefaultTokensPerCredit)
	if credits != 1.4 || multiplier != 1 {
		t.Fatalf("official quote billing = credits=%v multiplier=%v, want 1.4 and 1", credits, multiplier)
	}
}

func TestComputeLLMRequestBillingUsesOfficialQuoteCacheRatesWithoutSnapshot(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(t.Context(), now, "req-official-cache-quote")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", BillingGroupMultiplier: 1}}}
	model := &llmservice.AuthorizedModel{Name: "hub-public-model", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}
	quote := llmservice.OfficialPricingQuote{
		ProviderID: "agnes",
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 2,
		}},
		ProviderMultiplier: 1,
		ExpiresAt:          now.Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, model, quote, []string{"official-group"}, 10_000, 2_000); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	usage := corelib.TokenUsageStat{InputTokens: 10_000, CachedInputTokens: 8_000, OutputTokens: 2_000, TotalTokens: 12_000}
	credits, multiplier := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"official-group"}, usage, llmservice.DefaultTokensPerCredit)
	// Directional: (2000*1 + 8000*0.1 + 2000*2) / 10000 = 0.68
	// Legacy tokens-per-credit would charge 12_000/10000 = 1.2.
	if credits != 0.68 || multiplier != 1 {
		t.Fatalf("official cache quote billing = credits=%v multiplier=%v, want 0.68 and 1", credits, multiplier)
	}
	breakdown := settledUsageCreditBreakdown(usage, credits, 1, 1, &quote.Pricing)
	if breakdown == nil || breakdown.UnitemizedComponent != 0 {
		t.Fatalf("cache quote settlement must stay itemized, breakdown=%#v", breakdown)
	}
	if breakdown.NormalInputComponent != 0.2 || breakdown.CacheReadComponent != 0.08 || breakdown.OutputComponent != 0.4 {
		t.Fatalf("cache quote components = %#v, want normal 0.2 cache-read 0.08 output 0.4", breakdown)
	}
}

func TestComputeLLMRequestBillingSettlesAutoFromLegacyAdmissionQuote(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(t.Context(), now, "req-auto-legacy-quote")
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "maclaw_official_group", BillingGroupMultiplier: 1}}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}
	cacheRate := 1.0
	cacheWriteRate := 1.0
	quote := llmservice.OfficialPricingQuote{
		ProviderID:    "member-without-directional-price",
		UpstreamModel: "upstream-member",
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:      1,
			OutputCreditsPer10K:     1,
			CacheReadCreditsPer10K:  &cacheRate,
			CacheWriteCreditsPer10K: &cacheWriteRate,
			MinimumRequestCredits:   0.1,
			Version:                 "cache-v1",
		}},
		PricingSource:        llmpool.PricingSourceProvider,
		ProviderMultiplier:   1,
		CapabilityMultiplier: 1,
		ExpiresAt:            now.Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, model, quote, []string{"maclaw_official_group"}, 5891, 508); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	// No response snapshot is present. An auto admission quote whose logical
	// model is still "auto" must settle from the body counts. Cache is priced
	// at the same 1 Credit / 10k as ordinary input, so 256 cached tokens do
	// not reduce the 0.640 debit.
	usage := corelib.TokenUsageStat{InputTokens: 5891, CachedInputTokens: 256, OutputTokens: 508, TotalTokens: 6399}
	credits, multiplier := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"maclaw_official_group"}, usage, llmservice.DefaultTokensPerCredit)
	if math.Abs(credits-0.64) > 1e-9 || multiplier != 1 {
		t.Fatalf("auto legacy quote billing = credits=%v multiplier=%v, want 0.64 and 1", credits, multiplier)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteWithoutLocalDirectionalPrice(t *testing.T) {
	var quoted bool
	hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/llm/v1/quotes") {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-MaClaw-Request-ID") == "" || r.Header.Get("X-Hub-ID") == "" {
			http.Error(w, "missing quote identity", http.StatusBadRequest)
			return
		}
		quoted = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "quote-token",
			"quote": map[string]any{
				"provider_id":         "agnes",
				"upstream_model":      "upstream-model",
				"service_group_id":    "redeem",
				"provider_multiplier": 1,
				"expires_at":          time.Now().UTC().Add(time.Minute),
				"pricing": map[string]any{
					"input_credits_per_10k":  1,
					"output_credits_per_10k": 2,
				},
			},
		})
	}))
	defer hubCenter.Close()

	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: hubCenter.URL,
		HubID:        "hub-1",
		MachineToken: "hub-secret",
	})})
	t.Cleanup(func() { SetMaClawModule(previous) })

	ctx := store.WithTenant(withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-no-local-price"), "tenant-a")
	ctx = llmservice.WithOfficialForwardMeta(ctx, llmservice.OfficialForwardMeta{RequestID: "req-official-no-local-price"})
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
	}
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official-group", BillingGroupMultiplier: 1}}}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, reg, &im.LLMProviderRegistry{}, model, map[string]any{"model": "auto"}, "", ""); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if !quoted {
		t.Fatal("official quote must be requested even when Hub has no local directional price")
	}
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok || stored.Pricing.InputCreditsPer10K != 1 || stored.Pricing.OutputCreditsPer10K != 2 {
		t.Fatalf("stored official quote = %#v ok=%v, want HubCenter 1/2 price", stored, ok)
	}
	officialQuote, ok := snapshotOfficialForwardQuote(ctx)
	if !ok || officialQuote.ServiceGroupID != "redeem" {
		t.Fatalf("official quote service group = %#v ok=%v, want HubCenter matched redeem", officialQuote, ok)
	}
	if got := officialUsageReportServiceGroupIDs(ctx, llmservice.MaClawOfficialProviderID, []string{"coding-auto"}); len(got) != 1 || got[0] != "redeem" {
		t.Fatalf("report groups = %#v, want HubCenter matched redeem", got)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteSkipsFreeRoute(t *testing.T) {
	quoted := false
	hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		quoted = true
		http.Error(w, "quote must not be requested for a free official route", http.StatusInternalServerError)
	}))
	defer hubCenter.Close()

	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: hubCenter.URL,
		HubID:        "hub-1",
		MachineToken: "hub-secret",
	})})
	t.Cleanup(func() { SetMaClawModule(previous) })

	ctx := store.WithTenant(withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-free"), "tenant-a")
	ctx = llmservice.WithOfficialForwardMeta(ctx, llmservice.OfficialForwardMeta{RequestID: "req-official-free"})
	model := &llmservice.AuthorizedModel{
		Name:                 "auto",
		ProviderIDs:          []string{llmservice.MaClawOfficialProviderID},
		ProviderBillingModes: map[string]string{llmservice.MaClawOfficialProviderID: llmpool.BillingModeFree},
	}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, &llmservice.Registry{}, &im.LLMProviderRegistry{}, model, map[string]any{"model": "auto"}, "", ""); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if quoted {
		t.Fatal("free official route must not request a HubCenter pricing quote")
	}
	if _, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID); ok {
		t.Fatal("free official route must not freeze a pricing quote")
	}
}

type officialQuoteCapture struct {
	bodies [][]byte
}

func (c *officialQuoteCapture) serve(t *testing.T, respond func(call int, body []byte, w http.ResponseWriter)) {
	t.Helper()
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/llm/v1/quotes") {
			http.NotFound(w, r)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read quote body", http.StatusBadRequest)
			return
		}
		copied := append([]byte(nil), raw...)
		c.bodies = append(c.bodies, copied)
		call++
		respond(call, copied, w)
	}))
	t.Cleanup(server.Close)
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: server.URL,
		HubID:        "hub-1",
		MachineToken: "hub-secret",
	})})
	t.Cleanup(func() { SetMaClawModule(previous) })
}

func writeOfficialTestQuote(w http.ResponseWriter, token string, outputPer10k float64) {
	writeOfficialTestQuoteWithCapability(w, token, outputPer10k, 0)
}

func writeOfficialTestQuoteWithCapability(w http.ResponseWriter, token string, outputPer10k, capability float64) {
	w.Header().Set("Content-Type", "application/json")
	quote := map[string]any{
		"provider_id":         "agnes",
		"upstream_model":      "upstream-model",
		"service_group_id":    "official-group",
		"provider_multiplier": 1,
		"expires_at":          time.Now().UTC().Add(time.Minute),
		"pricing": map[string]any{
			"input_credits_per_10k":  1,
			"output_credits_per_10k": outputPer10k,
		},
	}
	if capability > 0 {
		quote["capability_multiplier"] = capability
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token": token,
		"quote": quote,
	})
}

func officialFitModel() *llmservice.AuthorizedModel {
	return &llmservice.AuthorizedModel{
		Name:                   "official-mid",
		ProviderIDs:            []string{llmservice.MaClawOfficialProviderID},
		ChargedServiceGroupIDs: []string{"official-group"},
	}
}

func officialFitRegistry(now time.Time, credits float64) *llmservice.Registry {
	return &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "official-group", AccessPolicy: llmservice.AccessPolicyGrantRequired, BillingGroupMultiplier: 1,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "official-group",
			CreditsTotal: credits, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		}},
	}
}

func officialFitContext(t *testing.T, requestID string, now time.Time) context.Context {
	t.Helper()
	ctx := store.WithTenant(withLLMBillingState(t.Context(), now, requestID), "tenant-a")
	return llmservice.WithOfficialForwardMeta(ctx, llmservice.OfficialForwardMeta{RequestID: requestID})
}

func assertQuotedBodyIsForwarded(t *testing.T, model *llmservice.AuthorizedModel, body map[string]any, quoted []byte) {
	t.Helper()
	forwarded, err := json.Marshal(rewriteOfficialForwardBody(body, model, llmservice.MaClawOfficialProviderID))
	if err != nil {
		t.Fatalf("marshal forwarded body: %v", err)
	}
	if !bytes.Equal(forwarded, quoted) {
		t.Fatalf("quoted body = %s\nforwarded body = %s", quoted, forwarded)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteFitsOutputCeilingToBalance(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		writeOfficialTestQuote(w, "quote-"+string(rune('0'+call)), 2)
	})
	now := time.Now().UTC()
	ctx := officialFitContext(t, "req-official-fit", now)
	model := officialFitModel()
	body := map[string]any{"model": "official-mid", "max_tokens": 65_536}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, officialFitRegistry(now, 1), &im.LLMProviderRegistry{}, model, body, "u1", "user@example.com"); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if len(quoted.bodies) != 2 {
		t.Fatalf("official quote calls = %d, want 2 (price the uncapped body, then the fitted body)", len(quoted.bodies))
	}
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok || stored.OutputTokenLimit <= 0 || stored.OutputTokenLimit >= 65_536 {
		t.Fatalf("official fitted ceiling = %#v ok=%v, want a ceiling below 65536", stored, ok)
	}
	if stored.ReservedMicrocredits > creditsToMicrocredits(1) {
		t.Fatalf("official fitted reserve = %d microcredits, above the 1 Credit balance", stored.ReservedMicrocredits)
	}
	if got, gotOK := llmQuotePositiveInt64(body["max_tokens"]); !gotOK || got != stored.OutputTokenLimit {
		t.Fatalf("forwarded max_tokens = %d ok=%v, want %d", got, gotOK, stored.OutputTokenLimit)
	}
	assertQuotedBodyIsForwarded(t, model, body, quoted.bodies[len(quoted.bodies)-1])
	forwardQuote, ok := snapshotOfficialForwardQuote(ctx)
	if !ok || forwardQuote.Token != "quote-2" {
		t.Fatalf("forward quote token = %q ok=%v, want the requote token quote-2", forwardQuote.Token, ok)
	}
	var priced map[string]any
	if err := json.Unmarshal(quoted.bodies[0], &priced); err != nil {
		t.Fatalf("decode first quote body: %v", err)
	}
	if got, gotOK := llmQuotePositiveInt64(priced["max_tokens"]); !gotOK || got != 65_536 {
		t.Fatalf("first quote max_tokens = %d ok=%v, want the uncapped 65536", got, gotOK)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteSkipsRequoteWhenBalanceCoversCeiling(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		writeOfficialTestQuote(w, "quote-"+string(rune('0'+call)), 2)
	})
	now := time.Now().UTC()
	ctx := officialFitContext(t, "req-official-cover", now)
	model := officialFitModel()
	body := map[string]any{"model": "official-mid", "max_tokens": 65_536}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, officialFitRegistry(now, 10_000), &im.LLMProviderRegistry{}, model, body, "u1", "user@example.com"); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if len(quoted.bodies) != 1 {
		t.Fatalf("official quote calls = %d, want 1 when the balance covers 65536", len(quoted.bodies))
	}
	if got, gotOK := llmQuotePositiveInt64(body["max_tokens"]); !gotOK || got != 65_536 {
		t.Fatalf("max_tokens = %d ok=%v, want the requested 65536", got, gotOK)
	}
	assertQuotedBodyIsForwarded(t, model, body, quoted.bodies[0])
	forwardQuote, ok := snapshotOfficialForwardQuote(ctx)
	if !ok || forwardQuote.Token != "quote-1" {
		t.Fatalf("forward quote token = %q ok=%v, want quote-1", forwardQuote.Token, ok)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteRestoresBodyWhenRequoteFails(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		if call > 1 {
			http.Error(w, "quote unavailable", http.StatusInternalServerError)
			return
		}
		writeOfficialTestQuote(w, "quote-1", 2)
	})
	now := time.Now().UTC()
	ctx := officialFitContext(t, "req-official-requote-fail", now)
	model := officialFitModel()
	body := map[string]any{"model": "official-mid", "max_tokens": 65_536}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, officialFitRegistry(now, 1), &im.LLMProviderRegistry{}, model, body, "u1", "user@example.com"); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if len(quoted.bodies) != 2 {
		t.Fatalf("official quote calls = %d, want the capped requote to be attempted", len(quoted.bodies))
	}
	if got, gotOK := llmQuotePositiveInt64(body["max_tokens"]); !gotOK || got != 65_536 {
		t.Fatalf("max_tokens = %d ok=%v, want the original 65536 after the requote failed", got, gotOK)
	}
	assertQuotedBodyIsForwarded(t, model, body, quoted.bodies[0])
	forwardQuote, ok := snapshotOfficialForwardQuote(ctx)
	if !ok || forwardQuote.Token != "quote-1" {
		t.Fatalf("forward quote token = %q ok=%v, want the original token quote-1", forwardQuote.Token, ok)
	}
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok || stored.OutputTokenLimit != 65_536 {
		t.Fatalf("stored ceiling = %#v ok=%v, want the original 65536 so reserve denies", stored, ok)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteRestoresBodyWhenRequoteCannotBePriced(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		if call == 1 {
			writeOfficialTestQuote(w, "quote-1", 2)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "quote-2",
			"quote": map[string]any{
				"provider_id":         "agnes",
				"upstream_model":      "upstream-model",
				"service_group_id":    "official-group",
				"provider_multiplier": 1,
				"expires_at":          time.Now().UTC().Add(time.Minute),
				"pricing": map[string]any{
					"input_credits_per_10k":      1,
					"output_credits_per_10k":     2,
					"cache_read_credits_per_10k": -1,
				},
			},
		})
	})
	now := time.Now().UTC()
	ctx := officialFitContext(t, "req-official-reprice-invalid", now)
	model := officialFitModel()
	body := map[string]any{"model": "official-mid", "max_tokens": 65_536}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, officialFitRegistry(now, 1), &im.LLMProviderRegistry{}, model, body, "u1", "user@example.com"); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if len(quoted.bodies) != 2 {
		t.Fatalf("official quote calls = %d, want the capped requote to be attempted", len(quoted.bodies))
	}
	if got, gotOK := llmQuotePositiveInt64(body["max_tokens"]); !gotOK || got != 65_536 {
		t.Fatalf("max_tokens = %d ok=%v, want the original 65536 after the requote could not be priced", got, gotOK)
	}
	assertQuotedBodyIsForwarded(t, model, body, quoted.bodies[0])
	forwardQuote, ok := snapshotOfficialForwardQuote(ctx)
	if !ok || forwardQuote.Token != "quote-1" {
		t.Fatalf("forward quote token = %q ok=%v, want the original token quote-1", forwardQuote.Token, ok)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteRefitsWhenRequotePriceRises(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		output := 2.0
		if call > 1 {
			output = 200
		}
		writeOfficialTestQuote(w, "quote-"+string(rune('0'+call)), output)
	})
	now := time.Now().UTC()
	ctx := officialFitContext(t, "req-official-reprice", now)
	model := officialFitModel()
	body := map[string]any{"model": "official-mid", "max_tokens": 65_536}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, officialFitRegistry(now, 1), &im.LLMProviderRegistry{}, model, body, "u1", "user@example.com"); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if len(quoted.bodies) != 3 {
		t.Fatalf("official quote calls = %d, want 3 (uncapped, first ceiling, refit after the higher price)", len(quoted.bodies))
	}
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok || stored.OutputTokenLimit <= 0 || stored.OutputTokenLimit >= 65_536 {
		t.Fatalf("official refitted ceiling = %#v ok=%v, want a ceiling below 65536", stored, ok)
	}
	if stored.ReservedMicrocredits > creditsToMicrocredits(1) {
		t.Fatalf("official refitted reserve = %d microcredits, above the 1 Credit balance", stored.ReservedMicrocredits)
	}
	assertQuotedBodyIsForwarded(t, model, body, quoted.bodies[len(quoted.bodies)-1])
	forwardQuote, ok := snapshotOfficialForwardQuote(ctx)
	if !ok || forwardQuote.Token != "quote-3" {
		t.Fatalf("forward quote token = %q ok=%v, want quote-3", forwardQuote.Token, ok)
	}
}

func TestPrepareOfficialLLMRequestPricingQuoteRaisesCeilingWhenRequotePriceDrops(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		output := 2.0
		if call == 1 {
			output = 200
		}
		writeOfficialTestQuote(w, "quote-"+string(rune('0'+call)), output)
	})
	now := time.Now().UTC()
	ctx := officialFitContext(t, "req-official-reprice-drop", now)
	model := officialFitModel()
	body := map[string]any{"model": "official-mid", "max_tokens": 65_536}
	if err := prepareOfficialLLMRequestPricingQuote(ctx, officialFitRegistry(now, 1), &im.LLMProviderRegistry{}, model, body, "u1", "user@example.com"); err != nil {
		t.Fatalf("prepare official quote: %v", err)
	}
	if len(quoted.bodies) != 3 {
		t.Fatalf("official quote calls = %d, want 3 (expensive uncapped, cheap refit, confirm the raised ceiling)", len(quoted.bodies))
	}
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	// 100 output tokens at 200 Credits/10k already cost 2 Credits, so a ceiling
	// that stayed on the first quote's price cannot clear this bar.
	if !ok || stored.OutputTokenLimit <= 100 || stored.OutputTokenLimit >= 65_536 {
		t.Fatalf("official raised ceiling = %#v ok=%v, want a ceiling the cheaper quote can pay and the expensive quote cannot", stored, ok)
	}
	if stored.ReservedMicrocredits > creditsToMicrocredits(1) {
		t.Fatalf("official raised reserve = %d microcredits, above the 1 Credit balance", stored.ReservedMicrocredits)
	}
	firstFit := map[string]any{}
	if err := json.Unmarshal(quoted.bodies[1], &firstFit); err != nil {
		t.Fatalf("decode first fitted quote: %v", err)
	}
	firstCeiling, firstOK := llmQuotePositiveInt64(firstFit["max_tokens"])
	if !firstOK || stored.OutputTokenLimit <= firstCeiling {
		t.Fatalf("raised ceiling %d did not exceed the expensive-price ceiling %d", stored.OutputTokenLimit, firstCeiling)
	}
	assertQuotedBodyIsForwarded(t, model, body, quoted.bodies[len(quoted.bodies)-1])
	forwardQuote, ok := snapshotOfficialForwardQuote(ctx)
	if !ok || forwardQuote.Token != "quote-3" {
		t.Fatalf("forward quote token = %q ok=%v, want quote-3", forwardQuote.Token, ok)
	}
}

func TestOfficialFallbackRefitsOutputCeilingToAdmissionHold(t *testing.T) {
	now := time.Now().UTC()
	admitFallback := func(t *testing.T) context.Context {
		t.Helper()
		ctx := officialFitContext(t, "req-official-fallback", now)
		ctx = llmservice.WithOfficialForwardMeta(ctx, llmservice.OfficialForwardMeta{
			RequestID:     "req-official-fallback",
			ResolvedModel: llmpool.OfficialTierHigh,
			ClientModel:   llmpool.OfficialTierMid,
		})
		admitted := &llmservice.AuthorizedModel{
			Name:                   llmpool.OfficialTierMid,
			ProviderIDs:            []string{llmservice.MaClawOfficialProviderID},
			ChargedServiceGroupIDs: []string{"official-group"},
		}
		quote := llmservice.OfficialPricingQuote{
			ProviderID:    "agnes",
			UpstreamModel: "upstream-model",
			Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
				InputCreditsPer10K:  1,
				OutputCreditsPer10K: 2,
				Version:             "cache-v1",
			}},
			PricingSource:        llmpool.PricingSourceProvider,
			ProviderMultiplier:   1,
			CapabilityMultiplier: 1,
			ExpiresAt:            now.Add(time.Minute),
		}
		if err := rememberOfficialPricingQuote(ctx, officialFitRegistry(now, 1), admitted, quote, []string{"official-group"}, 10, 100); err != nil {
			t.Fatalf("remember admission quote: %v", err)
		}
		state := llmBillingStateFrom(ctx)
		state.mu.Lock()
		state.reservationHeld = true
		state.heldQuoteSet = true
		state.heldQuote = state.quotes[llmPricingQuoteKey(llmpool.OfficialTierMid, llmservice.MaClawOfficialProviderID)]
		state.heldQuote.ReservedMicrocredits = creditsToMicrocredits(1)
		state.mu.Unlock()
		return ctx
	}

	t.Run("higher tier", func(t *testing.T) {
		var quoted officialQuoteCapture
		quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
			writeOfficialTestQuoteWithCapability(w, "quote-"+string(rune('0'+call)), 2, 2)
		})
		ctx := admitFallback(t)
		body := map[string]any{"model": llmpool.OfficialTierHigh, "max_tokens": int64(65_536)}
		_, _, _, _ = forwardMaClawOfficialRequestWithCompatRetry(ctx, body, "tenant-a", []string{"official-group"})
		limit, ok := llmQuotePositiveInt64(body["max_tokens"])
		if !ok || limit <= 1 || limit >= 65_536 {
			t.Fatalf("fallback ceiling = %d ok=%v, want a ceiling inside the 1 credit hold", limit, ok)
		}
		if len(quoted.bodies) < 2 {
			t.Fatalf("fallback quotes = %d, want the uncapped tier and the ceiling fitted to the hold", len(quoted.bodies))
		}
		var last map[string]any
		if err := json.Unmarshal(quoted.bodies[len(quoted.bodies)-1], &last); err != nil {
			t.Fatalf("decode fitted fallback quote: %v", err)
		}
		quotedLimit, quotedOK := llmQuotePositiveInt64(last["max_tokens"])
		if !quotedOK || quotedLimit != limit {
			t.Fatalf("quoted ceiling = %d ok=%v, body ceiling = %d", quotedLimit, quotedOK, limit)
		}
		forwardQuote, ok := snapshotOfficialForwardQuote(ctx)
		if !ok || forwardQuote.Token != "quote-2" {
			t.Fatalf("fallback quote token = %q ok=%v, want quote-2", forwardQuote.Token, ok)
		}
		snap, snapOK := provisionalOfficialAdmissionQuote(ctx, nil, nil, forwardQuote, estimateLLMQuoteInputTokens(body), limit, 2)
		if !snapOK || snap.ReservedMicrocredits > creditsToMicrocredits(1) {
			t.Fatalf("fallback reserve = %d ok=%v, above the 1 credit hold", snap.ReservedMicrocredits, snapOK)
		}
	})

	t.Run("unaffordable tier restores", func(t *testing.T) {
		var quoted officialQuoteCapture
		quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
			writeOfficialTestQuoteWithCapability(w, "quote-"+string(rune('0'+call)), 1_000_000, 2)
		})
		ctx := admitFallback(t)
		body := map[string]any{"model": llmpool.OfficialTierHigh, "max_tokens": int64(65_536)}
		_, _, _, err := forwardMaClawOfficialRequestWithCompatRetry(ctx, body, "tenant-a", []string{"official-group"})
		if err == nil || !strings.Contains(err.Error(), "exceeds the admission hold") {
			t.Fatalf("fallback err = %v, want the unaffordable tier to stop before forward", err)
		}
		limit, ok := llmQuotePositiveInt64(body["max_tokens"])
		if !ok || limit != 65_536 {
			t.Fatalf("restored ceiling = %d ok=%v, want the caller ceiling", limit, ok)
		}
		if snapshotOfficialNoUpstreamDispatch(ctx) {
			t.Fatal("refusing one fallback tier must not prove the whole request never left Hub")
		}
	})

	t.Run("unaffordable stream keeps dispatch proof open", func(t *testing.T) {
		var quoted officialQuoteCapture
		quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
			writeOfficialTestQuoteWithCapability(w, "quote-"+string(rune('0'+call)), 1_000_000, 2)
		})
		ctx := admitFallback(t)
		state := llmBillingStateFrom(ctx)
		state.mu.Lock()
		state.upstreamSent = true
		state.mu.Unlock()
		body := map[string]any{"model": llmpool.OfficialTierHigh, "max_tokens": int64(65_536)}
		req, err := http.NewRequest(http.MethodPost, "http://hub.local/v1/chat/completions", nil)
		if err != nil {
			t.Fatal(err)
		}
		req = req.WithContext(ctx)
		_, provenAbsent, err := openMaClawOfficialStreamRequest(req, body, []string{"official-group"})
		if err == nil || !strings.Contains(err.Error(), "exceeds the admission hold") {
			t.Fatalf("stream fallback err = %v, want the unaffordable tier to stop before forward", err)
		}
		if !provenAbsent {
			t.Fatal("stream fallback err = dispatched, want a local refusal")
		}
		if snapshotOfficialNoUpstreamDispatch(ctx) {
			t.Fatal("refusing one stream tier must not prove the whole request never left Hub")
		}
		noteOfficialDispatchObserved(ctx)
		noteOfficialNoUpstreamDispatch(ctx, true)
		if snapshotOfficialNoUpstreamDispatch(ctx) {
			t.Fatal("a dispatch observed elsewhere in the chain must stay reconcilable")
		}
	})
}

func TestFilterAuthorizedModelFitsSharedOutputCeilingToBalance(t *testing.T) {
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 1, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	model := &llmservice.AuthorizedModel{
		Name:                  "logical-model",
		ProviderIDs:           []string{"cheap", "expensive"},
		ProviderServiceGroups: map[string][]string{"cheap": {"paid"}, "expensive": {"paid"}},
		ProviderBillingModes:  map[string]string{"cheap": llmpool.BillingModePaid, "expensive": llmpool.BillingModePaid},
		ProviderTokenPricing: map[string]llmpool.TokenPricing{
			"cheap":     {InputCreditsPer10K: 1, OutputCreditsPer10K: 1},
			"expensive": {InputCreditsPer10K: 1, OutputCreditsPer10K: 20},
		},
	}
	body := map[string]any{"max_tokens": 20_000}
	ctx := withLLMBillingState(t.Context(), now, "req-shared-ceiling")
	got, denial, err := filterAuthorizedModelByBillingEligibility(ctx, reg, nil, "u1", "user@example.com", body, model)
	if err != nil || denial.Code != "" || got == nil {
		t.Fatalf("shared ceiling admission failed: model=%v denial=%#v err=%v", got, denial, err)
	}
	ceiling, ok := llmQuotePositiveInt64(body["max_tokens"])
	if !ok || ceiling <= 0 || ceiling >= 20_000 {
		t.Fatalf("shared ceiling = %d ok=%v, want a value below 20000", ceiling, ok)
	}
	for _, providerID := range []string{"cheap", "expensive"} {
		quote, quoteOK := snapshotLLMPricingQuote(ctx, providerID)
		if !quoteOK || quote.OutputTokenLimit != ceiling || quote.ReservedMicrocredits > creditsToMicrocredits(1) {
			t.Fatalf("provider %s quote = %#v ok=%v, want ceiling %d within 1 Credit", providerID, quote, quoteOK, ceiling)
		}
	}
}

func TestFilterAuthorizedModelsDoNotLetAnotherModelPinTheOutputCeiling(t *testing.T) {
	now := time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 1, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	expensive := llmservice.AuthorizedModel{
		Name:                  "high",
		ProviderIDs:           []string{"expensive"},
		ProviderServiceGroups: map[string][]string{"expensive": {"paid"}},
		ProviderBillingModes:  map[string]string{"expensive": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"expensive": {InputCreditsPer10K: 1, OutputCreditsPer10K: 100}},
	}
	cheap := llmservice.AuthorizedModel{
		Name:                  "mid",
		ProviderIDs:           []string{"cheap"},
		ProviderServiceGroups: map[string][]string{"cheap": {"paid"}},
		ProviderBillingModes:  map[string]string{"cheap": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"cheap": {InputCreditsPer10K: 1, OutputCreditsPer10K: 1}},
	}
	// The expensive model is admitted first. Its fitted ceiling must not become
	// the body the cheap model searches from, because that search cannot raise.
	body := map[string]any{"max_completion_tokens": 50, "max_tokens": 20_000}
	ctx := withLLMBillingState(t.Context(), now, "req-cross-model-ceiling")
	filtered, denied, first := filterAuthorizedModelsByBillingEligibility(ctx, reg, nil, "u1", "user@example.com", body, []llmservice.AuthorizedModel{expensive, cheap})
	if len(filtered) != 2 || len(denied) != 0 || first.Code != "" {
		t.Fatalf("catalog admission = models:%d denied:%v first:%#v", len(filtered), denied, first)
	}
	if got, ok := llmQuotePositiveInt64(body["max_tokens"]); !ok || got != 20_000 {
		t.Fatalf("catalog max_tokens = %d ok=%v, want the caller's 20000 until a model is selected", got, ok)
	}
	if got, ok := llmQuotePositiveInt64(body["max_completion_tokens"]); !ok || got != 50 {
		t.Fatalf("catalog max_completion_tokens = %d ok=%v, want the caller's 50", got, ok)
	}
	highQuote, highOK := snapshotLLMPricingQuote(ctx, "expensive")
	midQuote, midOK := snapshotLLMPricingQuote(ctx, "cheap")
	if !highOK || !midOK || highQuote.OutputTokenLimit <= 50 || highQuote.OutputTokenLimit >= midQuote.OutputTokenLimit || midQuote.OutputTokenLimit >= 20_000 {
		t.Fatalf("ceilings high=%#v ok=%v mid=%#v ok=%v, want the cheap model fitted from 20000", highQuote, highOK, midQuote, midOK)
	}
	if highQuote.ReservedMicrocredits > creditsToMicrocredits(1) || midQuote.ReservedMicrocredits > creditsToMicrocredits(1) {
		t.Fatalf("reserves high=%d mid=%d, want both inside 1 Credit", highQuote.ReservedMicrocredits, midQuote.ReservedMicrocredits)
	}
	applySelectedModelOutputCeiling(ctx, body, &cheap)
	if got, ok := llmQuotePositiveInt64(body["max_tokens"]); !ok || got != midQuote.OutputTokenLimit {
		t.Fatalf("selected mid max_tokens = %d ok=%v, want %d", got, ok, midQuote.OutputTokenLimit)
	}
	if got, ok := llmQuotePositiveInt64(body["max_completion_tokens"]); !ok || got != 50 {
		t.Fatalf("selected mid max_completion_tokens = %d ok=%v, want the caller's 50", got, ok)
	}
	body["max_completion_tokens"] = 50
	body["max_tokens"] = 20_000
	applySelectedModelOutputCeiling(ctx, body, &expensive)
	if got, ok := llmQuotePositiveInt64(body["max_tokens"]); !ok || got != highQuote.OutputTokenLimit {
		t.Fatalf("selected high max_tokens = %d ok=%v, want %d", got, ok, highQuote.OutputTokenLimit)
	}
	if got, ok := llmQuotePositiveInt64(body["max_completion_tokens"]); !ok || got != 50 {
		t.Fatalf("selected high max_completion_tokens = %d ok=%v, want the caller's 50", got, ok)
	}
}

func TestFilterAuthorizedModelsKeepSeparateCeilingsForASharedProvider(t *testing.T) {
	now := time.Date(2026, 8, 24, 3, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 1, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	// The cheap model is admitted second. A provider-only quote slot would keep
	// its ceiling and drop the expensive model's quote, so selecting high would
	// forward the caller's full max_tokens with no hold.
	expensive := llmservice.AuthorizedModel{
		Name:                  "high",
		ProviderIDs:           []string{"shared"},
		ProviderServiceGroups: map[string][]string{"shared": {"paid"}},
		ProviderBillingModes:  map[string]string{"shared": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"shared": {InputCreditsPer10K: 1, OutputCreditsPer10K: 100}},
	}
	cheap := llmservice.AuthorizedModel{
		Name:                  "mid",
		ProviderIDs:           []string{"shared"},
		ProviderServiceGroups: map[string][]string{"shared": {"paid"}},
		ProviderBillingModes:  map[string]string{"shared": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"shared": {InputCreditsPer10K: 1, OutputCreditsPer10K: 1}},
	}
	body := map[string]any{"max_completion_tokens": 50, "max_tokens": 20_000}
	ctx := withLLMBillingState(t.Context(), now, "req-shared-provider-ceiling")
	filtered, denied, first := filterAuthorizedModelsByBillingEligibility(ctx, reg, nil, "u1", "user@example.com", body, []llmservice.AuthorizedModel{expensive, cheap})
	if len(filtered) != 2 || len(denied) != 0 || first.Code != "" {
		t.Fatalf("catalog admission = models:%d denied:%v first:%#v", len(filtered), denied, first)
	}
	if _, ok := snapshotLLMPricingQuote(ctx, "shared"); ok {
		t.Fatal("provider-only lookup returned a quote shared by two models")
	}
	highQuote, highOK := snapshotLLMPricingQuoteForModel(ctx, "shared", &expensive)
	midQuote, midOK := snapshotLLMPricingQuoteForModel(ctx, "shared", &cheap)
	if !highOK || !midOK || highQuote.OutputTokenLimit <= 50 || highQuote.OutputTokenLimit >= midQuote.OutputTokenLimit || midQuote.OutputTokenLimit >= 20_000 {
		t.Fatalf("ceilings high=%#v ok=%v mid=%#v ok=%v, want both models to keep a fitted ceiling", highQuote, highOK, midQuote, midOK)
	}
	if highQuote.ReservedMicrocredits > creditsToMicrocredits(1) || midQuote.ReservedMicrocredits > creditsToMicrocredits(1) {
		t.Fatalf("reserves high=%d mid=%d, want both inside 1 Credit", highQuote.ReservedMicrocredits, midQuote.ReservedMicrocredits)
	}
	applySelectedModelOutputCeiling(ctx, body, &expensive)
	if got, ok := llmQuotePositiveInt64(body["max_tokens"]); !ok || got != highQuote.OutputTokenLimit {
		t.Fatalf("selected high max_tokens = %d ok=%v, want %d", got, ok, highQuote.OutputTokenLimit)
	}
	if got, ok := llmQuotePositiveInt64(body["max_completion_tokens"]); !ok || got != 50 {
		t.Fatalf("selected high max_completion_tokens = %d ok=%v, want the caller's 50", got, ok)
	}
	body["max_completion_tokens"] = 50
	body["max_tokens"] = 20_000
	applySelectedModelOutputCeiling(ctx, body, &cheap)
	if got, ok := llmQuotePositiveInt64(body["max_tokens"]); !ok || got != midQuote.OutputTokenLimit {
		t.Fatalf("selected mid max_tokens = %d ok=%v, want %d", got, ok, midQuote.OutputTokenLimit)
	}
	if got, ok := llmQuotePositiveInt64(body["max_completion_tokens"]); !ok || got != 50 {
		t.Fatalf("selected mid max_completion_tokens = %d ok=%v, want the caller's 50", got, ok)
	}
}

func TestPrepareLLMPricingQuoteCapsLargerOutputSibling(t *testing.T) {
	now := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired, BillingGroupMultiplier: 2,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 1, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	model := &llmservice.AuthorizedModel{
		Name:                  "logical-model",
		ProviderIDs:           []string{"p1"},
		ProviderServiceGroups: map[string][]string{"p1": {"paid"}},
		ProviderBillingModes:  map[string]string{"p1": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"p1": {InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
	}
	// max_completion_tokens alone fits in 1 Credit. The larger max_tokens must
	// still be reserved and lowered, or the forwarded request can outrun the hold.
	body := map[string]any{"max_completion_tokens": 50, "max_tokens": 20_000}
	ctx := withLLMBillingState(t.Context(), now, "req-output-sibling")
	denial, err := prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p1", body, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("sibling ceiling admission failed: denial=%#v err=%v", denial, err)
	}
	quote, ok := snapshotLLMPricingQuote(ctx, "p1")
	if !ok || quote.OutputTokenLimit <= 50 || quote.OutputTokenLimit >= 20_000 || quote.ReservedMicrocredits > creditsToMicrocredits(1) {
		t.Fatalf("sibling quote = %#v ok=%v, want a ceiling between the two fields and inside 1 Credit", quote, ok)
	}
	if got, gotOK := llmQuotePositiveInt64(body["max_tokens"]); !gotOK || got != quote.OutputTokenLimit {
		t.Fatalf("max_tokens = %d ok=%v, want the reserved ceiling %d", got, gotOK, quote.OutputTokenLimit)
	}
	if got, gotOK := llmQuotePositiveInt64(body["max_completion_tokens"]); !gotOK || got != 50 {
		t.Fatalf("max_completion_tokens = %d ok=%v, want the caller's 50 left in place", got, gotOK)
	}
}

func TestPrepareLLMPricingQuoteFitsCeilingAfterItsDigitsShrink(t *testing.T) {
	now := time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC)
	const available = 1.0
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: available, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	model := &llmservice.AuthorizedModel{
		Name:                  "logical-model",
		ProviderIDs:           []string{"p1"},
		ProviderServiceGroups: map[string][]string{"p1": {"paid"}},
		ProviderBillingModes:  map[string]string{"p1": llmpool.BillingModePaid},
		// One input token costs the same as 1000 output tokens. Dropping a
		// digit in max_tokens frees a whole input token, which the balance
		// can spend on more output.
		ProviderTokenPricing: map[string]llmpool.TokenPricing{"p1": {InputCreditsPer10K: 1000, OutputCreditsPer10K: 1}},
	}
	// "xy" places the 5-digit ceiling on a token boundary, so a 4-digit
	// ceiling estimates one input token cheaper.
	body := map[string]any{"max_tokens": 65536, "p": "xy"}
	originalEstimate := estimateLLMQuoteInputTokens(body)
	ctx := withLLMBillingState(t.Context(), now, "req-ceiling-digits")
	denial, err := prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p1", body, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("digit-shrink admission failed: denial=%#v err=%v", denial, err)
	}
	quote, ok := snapshotLLMPricingQuote(ctx, "p1")
	ceiling, ceilingOK := llmQuotePositiveInt64(body["max_tokens"])
	if !ok || !ceilingOK || ceiling != quote.OutputTokenLimit || ceiling <= 0 || ceiling >= 65536 {
		t.Fatalf("fitted ceiling = body %d quote %#v ok=%v, want the reserved ceiling below 65536", ceiling, quote, ok)
	}
	if quote.ReservedMicrocredits > creditsToMicrocredits(available) {
		t.Fatalf("fitted reserve = %d microcredits, above the balance", quote.ReservedMicrocredits)
	}
	if quote.InputTokenEstimate != estimateLLMQuoteInputTokens(body) {
		t.Fatalf("reserved input = %d, body estimates %d", quote.InputTokenEstimate, estimateLLMQuoteInputTokens(body))
	}
	availableMicro := creditsToMicrocredits(available)
	frozen := int64(0)
	for lo, hi := int64(1), int64(65536); lo <= hi; {
		mid := lo + (hi-lo)/2
		probed, probedOK := requoteAtOutputLimit(quote, originalEstimate, mid)
		if probedOK && probed.ReservedMicrocredits <= availableMicro {
			frozen = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if ceiling <= frozen {
		t.Fatalf("ceiling %d does not spend the input tokens freed by the shorter number (frozen search %d)", ceiling, frozen)
	}
	raised := map[string]any{"max_tokens": ceiling + 1, "p": "xy"}
	next, nextOK := requoteAtOutputLimit(quote, estimateLLMQuoteInputTokens(raised), ceiling+1)
	if !nextOK || next.ReservedMicrocredits <= availableMicro {
		t.Fatalf("ceiling+1 quote = %#v ok=%v, want it above the balance", next, nextOK)
	}
}

func TestPrepareLLMPricingQuoteFitsInjectedCeilingAfterDigitsShrink(t *testing.T) {
	now := time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC)
	const available = 1.5
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: available, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	model := &llmservice.AuthorizedModel{
		Name:                  "logical-model",
		ProviderIDs:           []string{"p1"},
		ProviderServiceGroups: map[string][]string{"p1": {"paid"}},
		ProviderBillingModes:  map[string]string{"p1": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"p1": {InputCreditsPer10K: 1000, OutputCreditsPer10K: 1}},
	}
	// No output field. Writing max_tokens raises the prompt estimate, and a
	// 4-digit ceiling is one token cheaper than a 5-digit one.
	body := map[string]any{"p": "xy"}
	ctx := withLLMBillingState(t.Context(), now, "req-ceiling-inject")
	denial, err := prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p1", body, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("injected ceiling admission failed: denial=%#v err=%v", denial, err)
	}
	quote, ok := snapshotLLMPricingQuote(ctx, "p1")
	ceiling, ceilingOK := llmQuotePositiveInt64(body["max_tokens"])
	if !ok || !ceilingOK || ceiling != quote.OutputTokenLimit || ceiling <= 0 || ceiling >= 65536 {
		t.Fatalf("injected ceiling = body %d quote %#v ok=%v, want the reserved ceiling below 65536", ceiling, quote, ok)
	}
	availableMicro := creditsToMicrocredits(available)
	if quote.ReservedMicrocredits > availableMicro {
		t.Fatalf("fitted reserve = %d microcredits, above the balance", quote.ReservedMicrocredits)
	}
	if quote.InputTokenEstimate != estimateLLMQuoteInputTokens(body) {
		t.Fatalf("reserved input = %d, body estimates %d", quote.InputTokenEstimate, estimateLLMQuoteInputTokens(body))
	}
	raised := map[string]any{"max_tokens": ceiling + 1, "p": "xy"}
	next, nextOK := requoteAtOutputLimit(quote, estimateLLMQuoteInputTokens(raised), ceiling+1)
	if !nextOK || next.ReservedMicrocredits <= availableMicro {
		t.Fatalf("ceiling+1 quote = %#v ok=%v, want it above the balance", next, nextOK)
	}
}

func TestCapResponsesPayloadToAdmittedCeiling(t *testing.T) {
	t.Run("lowers max_output_tokens to the admitted chat ceiling", func(t *testing.T) {
		responses := map[string]any{"model": "m", "input": "hi", "max_output_tokens": 65_536, "max_tokens": 80}
		chat := map[string]any{"model": "m", "max_tokens": 1200}
		capResponsesPayloadToAdmittedCeiling(responses, chat)
		if got, ok := llmQuotePositiveInt64(responses["max_output_tokens"]); !ok || got != 1200 {
			t.Fatalf("max_output_tokens = %d ok=%v, want 1200", got, ok)
		}
		if got, ok := llmQuotePositiveInt64(responses["max_tokens"]); !ok || got != 80 {
			t.Fatalf("max_tokens = %d ok=%v, want the caller's smaller 80", got, ok)
		}
	})
	t.Run("writes max_output_tokens when the caller only set max_tokens", func(t *testing.T) {
		responses := map[string]any{"model": "m", "input": "hi", "max_tokens": 65_536}
		chat := map[string]any{"model": "m", "max_tokens": 1200}
		capResponsesPayloadToAdmittedCeiling(responses, chat)
		if got, ok := llmQuotePositiveInt64(responses["max_output_tokens"]); !ok || got != 1200 {
			t.Fatalf("max_output_tokens = %d ok=%v, want the admitted 1200", got, ok)
		}
		if got, ok := llmQuotePositiveInt64(responses["max_tokens"]); !ok || got != 1200 {
			t.Fatalf("max_tokens = %d ok=%v, want the admitted 1200", got, ok)
		}
	})
	t.Run("enforces a caller max_tokens cap on the responses wire", func(t *testing.T) {
		responses := map[string]any{"model": "m", "input": "hi", "max_tokens": 80}
		chat := map[string]any{"model": "m", "max_tokens": 80}
		capResponsesPayloadToAdmittedCeiling(responses, chat)
		if got, ok := llmQuotePositiveInt64(responses["max_output_tokens"]); !ok || got != 80 {
			t.Fatalf("max_output_tokens = %d ok=%v, want 80", got, ok)
		}
		if got, ok := llmQuotePositiveInt64(responses["max_tokens"]); !ok || got != 80 {
			t.Fatalf("max_tokens = %d ok=%v, want 80", got, ok)
		}
	})
	t.Run("does not invent a ceiling when both bodies omitted it", func(t *testing.T) {
		responses := map[string]any{"model": "m", "input": "hi"}
		chat := map[string]any{"model": "m", "messages": []any{}}
		capResponsesPayloadToAdmittedCeiling(responses, chat)
		if _, ok := responses["max_output_tokens"]; ok {
			t.Fatalf("max_output_tokens = %#v, want it omitted", responses["max_output_tokens"])
		}
		if _, ok := responses["max_tokens"]; ok {
			t.Fatalf("max_tokens = %#v, want it omitted", responses["max_tokens"])
		}
	})
}

func TestComputeLLMRequestBillingUsesLocalProviderPricing(t *testing.T) {
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "local", TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 4, OutputCreditsPer10K: 8}}}}
	serviceReg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", BillingGroupMultiplier: 1}}}
	credits, multiplier := computeLLMRequestBilling(context.Background(), &llmservice.AuthorizedModel{}, "local", providerReg, serviceReg, []string{"paid"}, corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 5_000}, llmservice.DefaultTokensPerCredit)
	if credits != 8 || multiplier != 1 {
		t.Fatalf("local provider pricing billing = credits=%v multiplier=%v, want 8 and 1", credits, multiplier)
	}
}

func TestPrepareLLMPricingQuoteLabelsServiceGroupOverrideSource(t *testing.T) {
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
	}}, Grants: []llmservice.Grant{{
		ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
		CreditsTotal: 100, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
	}}}
	model := &llmservice.AuthorizedModel{
		Name:                   "logical-model",
		ProviderIDs:            []string{"p1", "p2"},
		ProviderServiceGroups:  map[string][]string{"p1": {"paid"}, "p2": {"paid"}},
		ProviderBillingModes:   map[string]string{"p1": llmpool.BillingModePaid, "p2": llmpool.BillingModePaid},
		ProviderUpstreamModels: map[string]string{"p1": "upstream-a", "p2": "upstream-b"},
		ProviderTokenPricing: map[string]llmpool.TokenPricing{
			"p1": {InputCreditsPer10K: 1, OutputCreditsPer10K: 2},
			"p2": {InputCreditsPer10K: 3, OutputCreditsPer10K: 4},
		},
		ProviderRouteBilling: map[string]map[string]llmservice.ProviderRouteBilling{"p1": {
			// An explicit per-route price is a service-group override and must be
			// reported as such, never silently merged into the provider price.
			"upstream-a": {BillingMode: llmpool.BillingModePaid, TokenPricingOverride: true, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 5, OutputCreditsPer10K: 6}},
		}},
	}
	ctx := withLLMBillingState(t.Context(), now, "req-override-source")
	denial, err := prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p1", map[string]any{"max_tokens": 100}, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("override quote failed: denial=%#v err=%v", denial, err)
	}
	quote, ok := snapshotLLMPricingQuote(ctx, "p1")
	if !ok || quote.PricingSource != llmpool.PricingSourceServiceGroupOverride {
		t.Fatalf("override quote source = %q ok=%v, want %q", quote.PricingSource, ok, llmpool.PricingSourceServiceGroupOverride)
	}

	ctx = withLLMBillingState(t.Context(), now, "req-provider-source")
	denial, err = prepareLLMPricingQuote(ctx, reg, nil, "u1", "user@example.com", model, "p2", map[string]any{"max_tokens": 100}, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("provider quote failed: denial=%#v err=%v", denial, err)
	}
	quote, ok = snapshotLLMPricingQuote(ctx, "p2")
	if !ok || quote.PricingSource != llmpool.PricingSourceProvider {
		t.Fatalf("provider quote source = %q ok=%v, want %q", quote.PricingSource, ok, llmpool.PricingSourceProvider)
	}
}

func TestLLMUsageReportMultipliersSlotsLocalRouteFactorUnderServiceGroup(t *testing.T) {
	// A non-official directional route has no HubCenter-owned provider factor.
	// Its whole effective multiplier belongs to the service-group slot so the
	// usage report labels provider_multiplier=1 and
	// service_group_multiplier=<actual>, and their product still equals the
	// multiplier used by the debit.
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", BillingGroupMultiplier: 2.5}}}
	providerMultiplier, groupMultiplier := llmUsageReportMultipliers(context.Background(), "third-party", reg, []string{"paid"}, 2.5)
	if providerMultiplier != 1 || groupMultiplier != 2.5 {
		t.Fatalf("local route multipliers = provider %v group %v, want 1 and 2.5", providerMultiplier, groupMultiplier)
	}
	if providerMultiplier*groupMultiplier != 2.5 {
		t.Fatalf("multiplier product %v no longer matches the debit factor 2.5", providerMultiplier*groupMultiplier)
	}
}

func TestPrepareLLMPricingQuoteReservesLocalProviderPricing(t *testing.T) {
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	// The provider owns its directional price in the local provider registry;
	// the model has no service-group route price for it. Admission must still
	// reserve the worst-case quote against this price (design §8), matching the
	// settlement priority chain in computeLLMRequestBilling.
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "local", TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 4, OutputCreditsPer10K: 8}}}}
	model := &llmservice.AuthorizedModel{
		Name:                  "logical-model",
		ProviderIDs:           []string{"local"},
		ProviderServiceGroups: map[string][]string{"local": {"paid"}},
	}
	newReg := func(creditsTotal float64) *llmservice.Registry {
		return &llmservice.Registry{
			ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
			Grants: []llmservice.Grant{{
				ID: "g1", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
				CreditsTotal: creditsTotal, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
			}},
		}
	}

	// Sufficient balance: the quote is frozen from the provider-owned price.
	ctx := withLLMBillingState(t.Context(), now, "req-local-quote")
	denial, err := prepareLLMPricingQuote(ctx, newReg(100), providerReg, "u1", "user@example.com", model, "local", map[string]any{"max_tokens": 100}, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("local provider quote failed: denial=%#v err=%v", denial, err)
	}
	quote, ok := snapshotLLMPricingQuote(ctx, "local")
	if !ok || quote.Pricing.InputCreditsPer10K != 4 || quote.Pricing.OutputCreditsPer10K != 8 {
		t.Fatalf("local provider quote = %#v ok=%v, want provider-owned 4/8 price", quote, ok)
	}
	if quote.PricingSource != llmpool.PricingSourceProvider {
		t.Fatalf("local provider quote source = %q, want %q", quote.PricingSource, llmpool.PricingSourceProvider)
	}

	// A finite balance below the caller's max_tokens still admits the request
	// after the ceiling is lowered to what that balance can reserve.
	shortBody := map[string]any{"max_tokens": 100}
	ctx = withLLMBillingState(t.Context(), now, "req-local-short")
	denial, err = prepareLLMPricingQuote(ctx, newReg(0.01), providerReg, "u1", "user@example.com", model, "local", shortBody, now)
	if err != nil || denial.Code != "" {
		t.Fatalf("short balance should fit a smaller ceiling: denial=%#v err=%v", denial, err)
	}
	shortQuote, ok := snapshotLLMPricingQuote(ctx, "local")
	if !ok || shortQuote.OutputTokenLimit <= 0 || shortQuote.OutputTokenLimit >= 100 || shortQuote.ReservedMicrocredits > creditsToMicrocredits(0.01) {
		t.Fatalf("short quote = %#v ok=%v, want a ceiling below 100 that fits 0.01 credits", shortQuote, ok)
	}
	// The prompt itself costs more than 0.01 credits, so there is no ceiling
	// left to continue with.
	hugeBody := map[string]any{"max_tokens": 100, "messages": []any{map[string]any{"role": "user", "content": strings.Repeat("a", 400)}}}
	ctx = withLLMBillingState(t.Context(), now, "req-local-insufficient")
	denial, err = prepareLLMPricingQuote(ctx, newReg(0.01), providerReg, "u1", "user@example.com", model, "local", hugeBody, now)
	if err == nil || denial.Code != "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST" {
		t.Fatalf("denial=%#v err=%v, want insufficient quote denial", denial, err)
	}
}

func mustEncodeTokenPricingSnapshot(t *testing.T, snapshot llmpool.TokenPricingSnapshot) string {
	t.Helper()
	encoded, ok := llmpool.EncodeTokenPricingSnapshot(snapshot)
	if !ok {
		t.Fatal("encode token pricing snapshot")
	}
	return encoded
}

func TestOfficialUsageReportPrefersHubCenterMatchedServiceGroup(t *testing.T) {
	ctx := withLLMBillingState(context.Background(), time.Now().UTC(), "req-recon")
	state := llmBillingStateFrom(ctx)
	state.officialQuote = &llmservice.OfficialPricingQuote{ServiceGroupID: "redeem"}
	got := officialUsageReportServiceGroupIDs(ctx, llmservice.MaClawOfficialProviderID, []string{"coding-auto"})
	if len(got) != 1 || got[0] != "redeem" {
		t.Fatalf("official report groups = %#v, want HubCenter matched redeem", got)
	}
	if got := officialUsageReportServiceGroupIDs(context.Background(), llmservice.MaClawOfficialProviderID, []string{"coding-auto", "second"}); len(got) != 1 || got[0] != "coding-auto" {
		t.Fatalf("fallback charged groups = %#v, want a single catalog group", got)
	}
	if got := officialUsageReportServiceGroupIDs(ctx, "local-provider", []string{"coding-auto"}); got != nil {
		t.Fatalf("local provider report groups = %#v, want omitted from HubCenter reconciliation", got)
	}
}

func TestUsageReconciliationServiceGroupsAlignCreditsAfterHubMarkup(t *testing.T) {
	hub := &llmUsageReportEntry{Totals: llmUsageCounters{
		InputTokens: 100, OutputTokens: 20, CachedInputTokens: 10, Requests: 2, Credits: 2.4,
		ProviderMultipliers: []llmUsageProviderMultiplier{{ProviderID: "agnes", Multiplier: 2, MultiplierSource: "service_group"}},
	}}
	groups := usageReconciliationServiceGroups(map[string]*llmUsageReportEntry{"redeem": hub}, []llmservice.OfficialUsageSummary{{
		ServiceGroupID: "redeem", InputTokens: 100, OutputTokens: 20, CachedInputTokens: 10, TotalRequests: 2, TotalCredits: 1.2,
	}})
	if len(groups) != 1 {
		t.Fatalf("groups = %#v", groups)
	}
	if groups[0].Status != "matched" {
		t.Fatalf("status = %s difference=%#v", groups[0].Status, groups[0].Difference)
	}
	if groups[0].Difference.Credits != 1.2 || !groups[0].Difference.UpstreamCreditsComparable || math.Abs(groups[0].Difference.UpstreamCredits) > 0.0005 {
		t.Fatalf("credit difference = %#v, want Hub markup 1.2 and stripped upstream 0", groups[0].Difference)
	}
	dayDiff := usageReconciliationDifference(hub.Totals, llmservice.OfficialUsageSummary{
		InputTokens: 100, OutputTokens: 20, CachedInputTokens: 10, TotalRequests: 2, TotalCredits: 1.2,
	})
	if usageReconciliationTokenMismatch(dayDiff) {
		t.Fatalf("tenant tokens should match: %#v", dayDiff)
	}
	if dayDiff.Credits != 1.2 {
		t.Fatalf("raw credit delta should keep Hub markup visible: %#v", dayDiff)
	}
	if !dayDiff.UpstreamCreditsComparable || math.Abs(dayDiff.UpstreamCredits) > 0.0005 {
		t.Fatalf("stripped upstream credits should match HubCenter: %#v", dayDiff)
	}
	if usageReconciliationDayStatus(dayDiff) != "matched" {
		t.Fatalf("day status = %s, want matched from tokens even when raw credits differ", usageReconciliationDayStatus(dayDiff))
	}
}

func TestUsageReconciliationDayStatusIgnoresGroupCreditMismatch(t *testing.T) {
	diff := usageReconciliationDifference(llmUsageCounters{
		InputTokens: 10, Requests: 1, Credits: 2,
		ProviderMultipliers: []llmUsageProviderMultiplier{{Multiplier: 2, MultiplierSource: "service_group"}},
	}, llmservice.OfficialUsageSummary{InputTokens: 10, TotalRequests: 1, TotalCredits: 2})
	if !usageReconciliationCreditMismatch(diff) {
		t.Fatalf("stripped 1 vs HubCenter 2 should be a credit mismatch: %#v", diff)
	}
	if usageReconciliationDayStatus(diff) != "matched" {
		t.Fatalf("day status = %s, want matched; group credit diffs stay on the group row", usageReconciliationDayStatus(diff))
	}
}

func TestUsageReconciliationSkipsCreditsWhenUnitemizedOrMixedMarkup(t *testing.T) {
	legacy := llmUsageCounters{InputTokens: 10, Requests: 1, Credits: 5, UnitemizedRequests: 1, CreditUnitemizedComponent: 5,
		ProviderMultipliers: []llmUsageProviderMultiplier{{Multiplier: 2, MultiplierSource: "service_group"}}}
	if _, ok := usageReconciliationUpstreamCredits(legacy); ok {
		t.Fatal("unitemized credits must not be divided by a later group multiplier")
	}
	mixed := llmUsageCounters{Credits: 9, ProviderMultipliers: []llmUsageProviderMultiplier{
		{Multiplier: 2, MultiplierSource: "service_group"},
		{Multiplier: 3, MultiplierSource: "service_group"},
	}}
	if _, ok := usageReconciliationUpstreamCredits(mixed); ok {
		t.Fatal("mixed Hub markups must not invent a single upstream credit total")
	}
	groups := usageReconciliationServiceGroups(nil, []llmservice.OfficialUsageSummary{{
		ServiceGroupID: "redeem", InputTokens: 10, TotalRequests: 1, TotalCredits: 1,
	}})
	if len(groups) != 1 || groups[0].Status != "unavailable" || groups[0].Difference != nil {
		t.Fatalf("legacy Hub day groups = %#v", groups)
	}
}

func TestUsageReconciliationServiceGroupCreditMismatch(t *testing.T) {
	hub := &llmUsageReportEntry{Totals: llmUsageCounters{
		InputTokens: 100, Requests: 1, Credits: 2.4,
		ProviderMultipliers: []llmUsageProviderMultiplier{{ProviderID: "agnes", Multiplier: 2, MultiplierSource: "service_group"}},
	}}
	groups := usageReconciliationServiceGroups(map[string]*llmUsageReportEntry{"redeem": hub}, []llmservice.OfficialUsageSummary{{
		ServiceGroupID: "redeem", InputTokens: 100, TotalRequests: 1, TotalCredits: 2,
	}})
	if len(groups) != 1 || groups[0].Status != "mismatch" || !usageReconciliationCreditMismatch(groups[0].Difference) {
		t.Fatalf("stripped upstream credits 1.2 vs HubCenter 2 should mismatch: %#v", groups)
	}
	if usageReconciliationTokenMismatch(groups[0].Difference) {
		t.Fatalf("tokens matched, credit-only mismatch: %#v", groups[0].Difference)
	}
}

func TestAddUsageRecordsOfficialServiceGroupsForReconciliation(t *testing.T) {
	reports := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	reports.addUsageWithCreditBreakdown(ts, "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 4, Requests: 1}, 1.2, &llmUsageCreditBreakdown{
		ServiceGroupMultiplier: 2,
		ReportServiceGroupIDs:  []string{"redeem", "coding-auto"},
		RMBPricingRecorded:     true,
	}, llmservice.MaClawOfficialProviderID)
	day := reports.Days["2026-09-01"]
	if day == nil || day.ServiceGroups["redeem"] == nil {
		t.Fatalf("service groups = %#v", day)
	}
	if day.ServiceGroups["redeem"].Totals.InputTokens != 10 || day.ServiceGroups["redeem"].Totals.Credits != 1.2 {
		t.Fatalf("redeem totals = %#v", day.ServiceGroups["redeem"].Totals)
	}
	if _, ok := day.ServiceGroups["coding-auto"]; ok {
		t.Fatal("one request must not be copied onto a second HubCenter catalog group")
	}
	if _, ok := day.Groups["engineering"]; !ok {
		t.Fatal("security-group bucket missing")
	}
}

func TestHubOfficialUsageCountersFallsBackToServiceGroups(t *testing.T) {
	day := &llmUsageReportDay{
		Totals: llmUsageCounters{InputTokens: 99, Requests: 9},
		ServiceGroups: map[string]*llmUsageReportEntry{
			"redeem":          {Totals: llmUsageCounters{InputTokens: 10, Requests: 1, Credits: 1.2}},
			"maclaw-official": {Totals: llmUsageCounters{InputTokens: 5, Requests: 1, Credits: 0.4}},
		},
	}
	got := hubOfficialUsageCounters(day)
	if got.InputTokens != 15 || got.Requests != 2 || got.Credits != 1.6 {
		t.Fatalf("service-group fallback = %#v, want official-only 15/2/1.6 not day totals", got)
	}
	day.Providers = map[string]*llmUsageReportEntry{
		llmservice.MaClawOfficialProviderID: {Totals: llmUsageCounters{InputTokens: 8, Requests: 1}},
	}
	got = hubOfficialUsageCounters(day)
	if got.InputTokens != 8 || got.Requests != 1 {
		t.Fatalf("official provider bucket = %#v", got)
	}
	day.Providers["MACLAW_OFFICIAL"] = &llmUsageReportEntry{Totals: llmUsageCounters{InputTokens: 2, Requests: 1}}
	got = hubOfficialUsageCounters(day)
	if got.InputTokens != 10 || got.Requests != 2 {
		t.Fatalf("case-insensitive official provider sum = %#v", got)
	}
}

func TestUsageReconciliationServiceGroupsMergesCaseVariants(t *testing.T) {
	groups := usageReconciliationServiceGroups(map[string]*llmUsageReportEntry{
		"Redeem": {Totals: llmUsageCounters{InputTokens: 4, Requests: 1, Credits: 0.4}},
		"redeem": {Totals: llmUsageCounters{InputTokens: 6, Requests: 1, Credits: 0.8}},
	}, []llmservice.OfficialUsageSummary{
		{ServiceGroupID: "REDEEM", InputTokens: 3, TotalRequests: 1, TotalCredits: 0.3},
		{ServiceGroupID: "redeem", InputTokens: 7, TotalRequests: 1, TotalCredits: 0.9},
	})
	if len(groups) != 1 {
		t.Fatalf("groups = %#v", groups)
	}
	if groups[0].Hub.InputTokens != 10 || math.Abs(groups[0].Hub.Credits-1.2) > 0.000000001 {
		t.Fatalf("hub merge = %#v", groups[0].Hub)
	}
	if groups[0].HubCenter == nil || groups[0].HubCenter.InputTokens != 10 || groups[0].HubCenter.TotalCredits != 1.2 {
		t.Fatalf("hubcenter merge = %#v", groups[0].HubCenter)
	}
}

func TestRememberOfficialPricingQuoteAppliesCapabilityMultiplier(t *testing.T) {
	now := time.Now().UTC()
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID: "official-group", BillingGroupMultiplier: 2,
		Models: []llmservice.ModelServiceModel{
			{Name: "auto"},
			{Name: llmpool.OfficialTierLow, BillingMultiplier: 0.5},
			{Name: llmpool.OfficialTierHigh, BillingMultiplier: 2},
		},
	}}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}
	quote := func(capability float64) llmservice.OfficialPricingQuote {
		return llmservice.OfficialPricingQuote{
			ProviderID:           "hubcenter-provider",
			UpstreamModel:        "upstream-model",
			Pricing:              llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 1}},
			ProviderMultiplier:   1,
			CapabilityMultiplier: capability,
			ExpiresAt:            now.Add(time.Minute),
		}
	}
	remember := func(clientModel string, capability float64) float64 {
		ctx := llmservice.WithOfficialForwardMeta(withLLMBillingState(t.Context(), now, "req-"+clientModel), llmservice.OfficialForwardMeta{ClientModel: clientModel})
		if err := rememberOfficialPricingQuote(ctx, reg, model, quote(capability), []string{"official-group"}, 1_000, 1_000); err != nil {
			t.Fatalf("remember %s: %v", clientModel, err)
		}
		stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
		if !ok {
			t.Fatalf("missing stored quote for %s", clientModel)
		}
		return stored.BillingGroupMultiplier
	}
	if got := remember("auto", 0); got != 2 {
		t.Fatalf("auto capability = %v, want group 2 x 1", got)
	}
	if got := remember("low", 0); got != 1 {
		t.Fatalf("low capability = %v, want group 2 x 0.5", got)
	}
	if got := remember("high", 0); got != 4 {
		t.Fatalf("high capability = %v, want group 2 x 2", got)
	}
	if got := remember("auto", 3); got != 6 {
		t.Fatalf("quoted capability = %v, want group 2 x 3", got)
	}
}
