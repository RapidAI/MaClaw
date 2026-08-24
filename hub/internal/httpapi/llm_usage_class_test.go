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
		"invalid pricing": func(a *llmservice.OfficialBillingAttempt) { a.PricingSnapshot.Pricing.InputCreditsPer10K = -1 },
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
	charge := &pendingCreditCharge{userID: "u1", email: "user@example.com", serviceGroupIDs: []string{"paid"}, credits: 2, requestID: "req-1", providerID: "p1", usage: corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 5}}
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
	enqueueLLMUsageRecordWithBilling(system, "p", corelib.TokenUsageStat{Requests: 1}, "u", "u@example.com", []string{"paid"}, nil, 0, llmservice.OfficialForwardMeta{}, "req-enqueue-zero", 2, nil)
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

func TestComputeLLMRequestBillingUsesOfficialBasePriceAndHubGroupMultiplierOnce(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC), "req-official-price")
	header := make(http.Header)
	header.Set(llmpool.ProviderIDHeader, "official-provider-a")
	header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID:    "official-provider-a",
		UpstreamModel: "opencode-1",
		InputTokens:   20_000,
		OutputTokens:  5_000,
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
	if multiplier != 2 || credits != 8 {
		t.Fatalf("official billing = credits=%v multiplier=%v, want 8 and 2", credits, multiplier)
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
		Pricing:   llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
		ExpiresAt: time.Now().UTC().Add(time.Minute),
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
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
	}))
	noteOfficialCreditMultiplierFromHeader(ctx, header)
	credits, multiplier := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"official-group"}, corelib.TokenUsageStat{}, llmservice.DefaultTokensPerCredit)
	if credits != 2.8 || multiplier != 2 {
		t.Fatalf("frozen official multiplier billing = credits=%v multiplier=%v, want 2.8 and 2", credits, multiplier)
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
		ProviderID:    "hubcenter-provider",
		UpstreamModel: "upstream-model",
		Pricing:       llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
		ExpiresAt:     now.Add(time.Minute),
	}
	if err := rememberOfficialPricingQuote(ctx, reg, model, quote, []string{"official-group"}, 1_000, 2_000); err != nil {
		t.Fatalf("remember official quote: %v", err)
	}
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok || stored.LogicalModel != model.Name || stored.UpstreamModel != quote.UpstreamModel || stored.BillingGroupMultiplier != 2 {
		t.Fatalf("stored quote = %#v, ok=%v", stored, ok)
	}
	credits, multiplier := computeLLMRequestBilling(ctx, model, llmservice.MaClawOfficialProviderID, nil, reg, []string{"official-group"}, corelib.TokenUsageStat{InputTokens: 10_000, OutputTokens: 1_000}, llmservice.DefaultTokensPerCredit)
	if credits != 2.8 || multiplier != 2 {
		t.Fatalf("official quote billing = credits=%v multiplier=%v, want 2.8 and 2", credits, multiplier)
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
