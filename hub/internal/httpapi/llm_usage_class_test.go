package httpapi

import (
	"context"
	"math"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
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
	enqueueLLMUsageRecordWithBilling(system, "p", corelib.TokenUsageStat{Requests: 1}, "u", "u@example.com", []string{"paid"}, nil, 0, llmservice.OfficialForwardMeta{}, "req-enqueue-zero", 2, 2, 1, nil)
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
	// Simulate an older HubCenter that sent its concrete provider ID but not the
	// pricing snapshot. Hub must still include the synced provider multiplier
	// rather than charging only the Hub service-group multiplier.
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	serviceReg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID:                     "official-group",
		BillingGroupMultiplier: 4,
	}}}
	model := &llmservice.AuthorizedModel{
		Name: "official",
		ProviderRouteBilling: map[string]map[string]llmservice.ProviderRouteBilling{
			llmservice.MaClawOfficialProviderID: {
				"opencode-1": {BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 2, OutputCreditsPer10K: 8}},
			},
		},
		ProviderUpstreamModels: map[string]string{llmservice.MaClawOfficialProviderID: "opencode-1"},
	}
	credits, multiplier := computeLLMRequestBilling(
		ctx, model, llmservice.MaClawOfficialProviderID, nil, serviceReg, []string{"official-group"},
		corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 1_000, TotalTokens: 11_000}, llmservice.DefaultTokensPerCredit,
	)
	if credits != 16.8 || multiplier != 6 {
		t.Fatalf("legacy official fallback billing = credits=%v multiplier=%v, want 16.8 and 6", credits, multiplier)
	}
	providerMultiplier, groupMultiplier := llmUsageReportMultipliers(ctx, llmservice.MaClawOfficialProviderID, serviceReg, []string{"official-group"}, multiplier)
	if providerMultiplier != 1.5 || groupMultiplier != 4 {
		t.Fatalf("legacy official fallback usage multipliers = provider %v group %v, want 1.5 and 4", providerMultiplier, groupMultiplier)
	}
	if got := usageReportBillingProviderID(ctx, llmservice.MaClawOfficialProviderID); got != "hubcenter-provider-a" {
		t.Fatalf("legacy official fallback tooltip provider = %q, want hubcenter-provider-a", got)
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
			"cheap-route":       {BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
		}},
		ProviderUpstreamRouteModels: map[string]map[string]string{"p1": {"logical-model": "cheap-route"}},
	}
	ctx := withLLMBillingState(t.Context(), now, "req-route-quote")
	denial, err := prepareLLMPricingQuote(ctx, reg, "u1", "user@example.com", model, "p1", map[string]any{"max_tokens": 1_000}, now)
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
	denial, err := prepareLLMPricingQuote(ctx, reg, "u1", "user@example.com", model, "p1", map[string]any{"max_tokens": 2_000}, now)
	if err == nil || denial.Code != "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST" {
		t.Fatalf("denial=%#v err=%v, want insufficient quote", denial, err)
	}

	reg.Grants[0].CreditsTotal = 10
	denial, err = prepareLLMPricingQuote(ctx, reg, "u1", "user@example.com", model, "p1", map[string]any{"max_tokens": 2_000}, now)
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

func mustEncodeTokenPricingSnapshot(t *testing.T, snapshot llmpool.TokenPricingSnapshot) string {
	t.Helper()
	encoded, ok := llmpool.EncodeTokenPricingSnapshot(snapshot)
	if !ok {
		t.Fatal("encode token pricing snapshot")
	}
	return encoded
}
