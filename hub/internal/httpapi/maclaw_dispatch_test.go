package httpapi

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestMaClawModuleGlobalAccessors(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	SetMaClawModule(nil)
	if got := GetMaClawAccessControl(); got != nil {
		t.Fatalf("access control with nil module = %#v", got)
	}
	body, status, err := ForwardViaMaClaw(context.Background(), nil, "tenant_default")
	if err != nil || status != http.StatusServiceUnavailable {
		t.Fatalf("forward without module status=%d err=%v", status, err)
	}
	if !strings.Contains(string(body), "not configured") {
		t.Fatalf("forward without module body=%s", string(body))
	}
	streamResp, err := ForwardStreamViaMaClaw(context.Background(), nil, "tenant_default")
	if err != nil || streamResp == nil || streamResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stream forward without module resp=%#v err=%v", streamResp, err)
	}
	streamBody, _ := io.ReadAll(streamResp.Body)
	_ = streamResp.Body.Close()
	if !strings.Contains(string(streamBody), "not configured") {
		t.Fatalf("stream forward without module body=%s", string(streamBody))
	}

	module := &llmservice.MaClawModule{AccessCtrl: llmservice.NewTenantLLMAccessControl(nil)}
	SetMaClawModule(module)
	if got := GetMaClawModule(); got != module {
		t.Fatalf("module getter returned %#v", got)
	}
	if got := GetMaClawAccessControl(); got != module.AccessCtrl {
		t.Fatalf("access control getter returned %#v", got)
	}
}

func TestHubCenterServiceGroupIDsTranslatesOnlyVEVirtualGroup(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "virtual employee group", in: []string{"ve-service"}, want: []string{"system-free"}},
		{name: "case and whitespace", in: []string{" VE-Service ", "redeem"}, want: []string{"system-free", "redeem"}},
		{name: "ordinary groups unchanged", in: []string{"redeem", "enterprise"}, want: []string{"redeem", "enterprise"}},
		{name: "official hub entry is not remapped to redeem", in: []string{llmpool.HubOfficialServiceGroupID, "redeem"}, want: []string{llmpool.HubOfficialServiceGroupID, "redeem"}},
		{name: "nil remains nil", in: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hubCenterServiceGroupIDs(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("groups = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("groups = %#v, want %#v", got, tt.want)
				}
			}
			if len(tt.in) > 0 && &got[0] == &tt.in[0] {
				t.Fatal("translation must not mutate the caller slice")
			}
		})
	}
}

func TestNoteOfficialStreamHeadersPrefersTrailers(t *testing.T) {
	ctx := withLLMBillingState(context.Background(), time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC))
	resp := &http.Response{
		Header:  make(http.Header),
		Trailer: make(http.Header),
		Body:    io.NopCloser(strings.NewReader("ok")),
	}
	resp.Header.Set(llmpool.CreditMultiplierHeader, "1")
	resp.Header.Set(llmpool.ProviderIDHeader, "openai")
	resp.Trailer.Set(llmpool.CreditMultiplierHeader, "0.5")
	resp.Trailer.Set(llmpool.ProviderIDHeader, "deepseek")
	resp = noteOfficialStreamHeaders(ctx, resp)
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	state := llmBillingStateFrom(ctx)
	if state == nil {
		t.Fatal("missing billing state")
	}
	if state.applied != 0.5 {
		t.Fatalf("applied = %v, want trailer 0.5", state.applied)
	}
	if state.officialProviderID != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", state.officialProviderID)
	}
}

func TestNoteOfficialStreamHeadersAppliesTrailersOnClose(t *testing.T) {
	ctx := withLLMBillingState(context.Background(), time.Now())
	resp := &http.Response{
		Header:  make(http.Header),
		Trailer: make(http.Header),
		Body:    io.NopCloser(strings.NewReader("partial")),
	}
	resp.Header.Set(llmpool.CreditMultiplierHeader, "1")
	resp.Trailer.Set(llmpool.CreditMultiplierHeader, "0.4")
	resp.Trailer.Set(llmpool.ProviderIDHeader, "deepseek")
	resp = noteOfficialStreamHeaders(ctx, resp)
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	state := llmBillingStateFrom(ctx)
	if state.applied != 0.4 || state.officialProviderID != "deepseek" {
		t.Fatalf("close trailers applied=%v provider=%q", state.applied, state.officialProviderID)
	}
}

func TestNoteOfficialStreamHeadersAppliesTrailerRMBPricingSnapshot(t *testing.T) {
	ctx := withLLMBillingState(context.Background(), time.Now())
	resp := &http.Response{
		Header:  make(http.Header),
		Trailer: make(http.Header),
		Body:    io.NopCloser(strings.NewReader("ok")),
	}
	resp.Trailer.Set(llmpool.ProviderIDHeader, "official-provider-a")
	resp.Trailer.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
		ProviderID: "official-provider-a", InputTokens: 1_000_000, OutputTokens: 500_000,
		Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K: 1, OutputCreditsPer10K: 4,
			InputRMBPer10K: 0.02, OutputRMBPer10K: 0.06,
		}},
	}))
	resp = noteOfficialStreamHeaders(ctx, resp)
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	usage := authoritativeLLMUsageForAccessLog(ctx, llmservice.MaClawOfficialProviderID, corelib.TokenUsageStat{})
	if usage.InputCostRMB != 2 || usage.OutputCostRMB != 3 || usage.TotalCostRMB != 5 {
		t.Fatalf("stream trailer RMB costs = input %v, output %v, total %v; want 2, 3, 5", usage.InputCostRMB, usage.OutputCostRMB, usage.TotalCostRMB)
	}
}

func TestSnapshotOfficialBillingRoundTripsToAnotherContext(t *testing.T) {
	src := withLLMBillingState(context.Background(), time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC))
	dst := withLLMBillingState(context.Background(), time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC))
	noteOfficialBilling(src, 0.5, "deepseek")
	multiplier, providerID := snapshotOfficialBilling(src)
	noteOfficialBilling(dst, multiplier, providerID)
	got := resolveBillableCreditMultiplier(dst, &llmservice.AuthorizedModel{CreditMultiplier: 2}, llmservice.MaClawOfficialProviderID, nil)
	if got != 0.5 {
		t.Fatalf("copied billing multiplier = %v, want 0.5 without local route remultiply", got)
	}
}

func TestOfficialBillingPropagatesToSingleflightWaiter(t *testing.T) {
	started := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)
	leaderCtx := withLLMBillingState(context.Background(), started)
	waiterCtx := withLLMBillingState(context.Background(), started)
	g := &authorizedModelRequestFlightGroup{}
	leaderFnStarted := make(chan struct{})
	releaseLeader := make(chan struct{})
	leaderDone := make(chan error, 1)
	waiterDone := make(chan error, 1)

	go func() {
		_, err := g.do(leaderCtx, "official-cache", time.Second, func(ctx context.Context) (authorizedModelForwardResult, error) {
			close(leaderFnStarted)
			<-releaseLeader
			noteOfficialBilling(ctx, 0.5, "deepseek")
			noteOfficialTokenPricing(ctx, &llmpool.TokenPricingSnapshot{
				ProviderID: "deepseek", InputTokens: 12_000, OutputTokens: 3_000,
				Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4}},
			})
			return authorizedModelForwardResult{statusCode: http.StatusOK, providerID: llmservice.MaClawOfficialProviderID}, nil
		})
		leaderDone <- err
	}()
	<-leaderFnStarted

	go func() {
		_, err := g.do(waiterCtx, "official-cache", time.Second, func(context.Context) (authorizedModelForwardResult, error) {
			t.Error("waiter must not execute the forward function")
			return authorizedModelForwardResult{}, nil
		})
		waiterDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		g.mu.Lock()
		call := g.calls["official-cache"]
		waiters := int32(0)
		if call != nil {
			waiters = call.waiters.Load()
		}
		g.mu.Unlock()
		if waiters > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter did not attach to in-flight official request")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseLeader)

	if err := <-leaderDone; err != nil {
		t.Fatalf("leader err = %v", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter err = %v", err)
	}
	applied, providerID := snapshotOfficialBilling(waiterCtx)
	if applied != 0.5 || providerID != "deepseek" {
		t.Fatalf("waiter billing = %v %q, want 0.5 deepseek", applied, providerID)
	}
	got := resolveBillableCreditMultiplier(waiterCtx, &llmservice.AuthorizedModel{CreditMultiplier: 2}, llmservice.MaClawOfficialProviderID, nil)
	if got != 0.5 {
		t.Fatalf("waiter billable multiplier = %v, want 0.5 without local route remultiply", got)
	}
	snapshot := snapshotOfficialTokenPricing(waiterCtx)
	if snapshot == nil || snapshot.ProviderID != "deepseek" || snapshot.InputTokens != 12_000 || snapshot.OutputTokens != 3_000 || snapshot.Pricing.OutputCreditsPer10K != 4 {
		t.Fatalf("waiter token-pricing snapshot = %#v", snapshot)
	}
	credits, multiplier := computeLLMRequestBilling(waiterCtx, &llmservice.AuthorizedModel{}, llmservice.MaClawOfficialProviderID, nil, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "official", BillingGroupMultiplier: 2}}}, []string{"official"}, corelib.TokenUsageStat{Requests: 1}, llmservice.DefaultTokensPerCredit)
	if credits != 4.8 || multiplier != 2 {
		t.Fatalf("waiter directional billing = credits=%v multiplier=%v, want 4.8 and 2", credits, multiplier)
	}
}

func TestNoteOfficialBillingIgnoresNonFiniteMultiplier(t *testing.T) {
	ctx := withLLMBillingState(context.Background(), time.Now())
	noteOfficialBilling(ctx, math.Inf(1), "deepseek")
	applied, providerID := snapshotOfficialBilling(ctx)
	if applied != 0 {
		t.Fatalf("applied Inf = %v, want ignored", applied)
	}
	if providerID != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", providerID)
	}
}

func TestAuthorizedModelRequestFlightRecoversLeaderPanic(t *testing.T) {
	g := &authorizedModelRequestFlightGroup{}
	_, err := g.do(context.Background(), "panic-key", time.Second, func(context.Context) (authorizedModelForwardResult, error) {
		panic("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("leader err = %v, want panic wrapper", err)
	}
	g.mu.Lock()
	_, stillTracked := g.calls["panic-key"]
	g.mu.Unlock()
	if stillTracked {
		t.Fatal("panicking leader left a stuck singleflight key")
	}
	result, err := g.do(context.Background(), "panic-key", time.Second, func(context.Context) (authorizedModelForwardResult, error) {
		return authorizedModelForwardResult{statusCode: http.StatusOK}, nil
	})
	if err != nil || result.statusCode != http.StatusOK {
		t.Fatalf("retry after panic status=%d err=%v", result.statusCode, err)
	}
}
