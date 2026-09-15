package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestApplyHubWorkloadSelectionCopiesIncomingHints(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req.Header.Set(llmpool.WorkflowTypeHeader, "coding")
	req.Header.Set(llmpool.PhaseKindHeader, "execution")
	req.Header.Set(llmpool.TaskTypeHeader, "reasoning")
	out := applyHubWorkloadSelection(rec, req, map[string]any{"model": "auto"}, nil, nil, &llmpool.WorkloadDecision{
		Class:  llmpool.WorkloadFallbackBalanced,
		Source: llmpool.ClassSourceFallback,
	})
	meta := llmservice.OfficialForwardMetaFrom(out.Context())
	if meta.WorkflowType != "coding" || meta.PhaseKind != "execution" || meta.TaskType != "reasoning" {
		t.Fatalf("forward meta hints = %+v", meta)
	}
}

func TestOfficialAdmissionQuoteAppliesToResolvedModel(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-l3")
	rememberLLMPricingQuote(ctx, llmpool.PricingQuoteSnapshot{
		ProviderID:   llmservice.MaClawOfficialProviderID,
		LogicalModel: llmpool.OfficialTierHigh,
	})
	highCtx := llmservice.WithOfficialForwardMeta(ctx, llmservice.OfficialForwardMeta{ResolvedModel: llmpool.OfficialTierHigh})
	if !officialAdmissionQuoteApplies(highCtx) {
		t.Fatal("admission quote must apply to official-high")
	}
	midCtx := llmservice.WithOfficialForwardMeta(ctx, llmservice.OfficialForwardMeta{ResolvedModel: llmpool.OfficialTierMid})
	if officialAdmissionQuoteApplies(midCtx) {
		t.Fatal("L3 official-mid fallback must not reuse the official-high admission quote")
	}
}

func TestLLMPricingQuoteAppliesToModel(t *testing.T) {
	quote := llmpool.PricingQuoteSnapshot{LogicalModel: llmpool.OfficialTierHigh}
	high := &llmservice.AuthorizedModel{Name: llmpool.OfficialTierHigh}
	mid := &llmservice.AuthorizedModel{Name: llmpool.OfficialTierMid}
	if !llmPricingQuoteAppliesToModel(quote, high) {
		t.Fatal("high quote must apply to official-high")
	}
	if llmPricingQuoteAppliesToModel(quote, mid) {
		t.Fatal("high quote must not price official-mid L3 usage")
	}
	if !llmPricingQuoteAppliesToModel(llmpool.PricingQuoteSnapshot{}, mid) {
		t.Fatal("quotes without a logical model keep legacy behavior")
	}
	if !llmPricingQuoteAppliesToModel(quote, &llmservice.AuthorizedModel{}) {
		t.Fatal("empty model name keeps the admission quote")
	}
}

func TestRememberOfficialForwardQuoteForResolvedUpdatesLogicalModel(t *testing.T) {
	ctx := withLLMBillingState(t.Context(), time.Now().UTC(), "req-official-l3-requote")
	rememberLLMPricingQuote(ctx, llmpool.PricingQuoteSnapshot{
		ProviderID:   llmservice.MaClawOfficialProviderID,
		LogicalModel: llmpool.OfficialTierHigh,
		Pricing:      llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 9}},
	})
	ctx = llmservice.WithOfficialForwardMeta(ctx, llmservice.OfficialForwardMeta{ResolvedModel: llmpool.OfficialTierMid})
	rememberOfficialForwardQuoteForResolved(ctx, llmservice.OfficialPricingQuote{
		ProviderID:         "agnes",
		UpstreamModel:      llmpool.OfficialTierMid,
		Pricing:            llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 2}},
		ProviderMultiplier: 1.5,
	})
	stored, ok := snapshotLLMPricingQuote(ctx, llmservice.MaClawOfficialProviderID)
	if !ok || stored.LogicalModel != llmpool.OfficialTierMid || stored.UpstreamModel != llmpool.OfficialTierMid || stored.Pricing.InputCreditsPer10K != 2 {
		t.Fatalf("stored quote = %#v, want official-mid pricing snapshot", stored)
	}
	mid := &llmservice.AuthorizedModel{Name: llmpool.OfficialTierMid}
	if !llmPricingQuoteAppliesToModel(stored, mid) {
		t.Fatal("updated snapshot must price official-mid L3 usage")
	}
	if !officialAdmissionQuoteApplies(ctx) {
		t.Fatal("updated snapshot must apply to the resolved official-mid forward")
	}
}

func TestRewriteOfficialForwardBodyDoesNotSendAuto(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name:                   llmpool.OfficialTierMid,
		ChargedServiceGroupIDs: []string{"coding-auto"},
		ProviderUpstreamModels: map[string]string{"maclaw_official": llmpool.OfficialTierMid},
	}
	got := rewriteOfficialForwardBody(map[string]any{"model": "auto", "messages": []any{}}, model, llmservice.MaClawOfficialProviderID)
	if got["model"] != llmpool.OfficialTierMid {
		t.Fatalf("model = %#v, want official-mid", got["model"])
	}
}

func TestOfficialForwardServiceGroupIDsUsesOfficialPool(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name:                   llmpool.OfficialTierHigh,
		ChargedServiceGroupIDs: []string{"coding-auto"},
		ProviderUpstreamModels: map[string]string{"maclaw_official": llmpool.OfficialTierHigh},
		ProviderServiceGroups:  map[string][]string{"maclaw_official": {"coding-auto"}},
	}
	got := officialForwardServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(got) != 1 || got[0] != llmpool.OfficialGroupID {
		t.Fatalf("forward groups = %#v", got)
	}
	charged := llmservice.ChargedServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(charged) != 1 || charged[0] != "coding-auto" {
		t.Fatalf("charged = %#v", charged)
	}
}

func TestOfficialForwardServiceGroupIDsKeepsHubOfficialEntry(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name:                  "auto",
		ServiceGroupIDs:       []string{llmservice.MaClawOfficialServiceGroupID},
		ProviderServiceGroups: map[string][]string{"maclaw_official": {llmservice.MaClawOfficialServiceGroupID}},
	}
	got := officialForwardServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(got) != 1 || got[0] != llmservice.MaClawOfficialServiceGroupID {
		t.Fatalf("forward groups = %#v, want Hub official entry", got)
	}
	charged := llmservice.ChargedServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(charged) != 1 || charged[0] != llmservice.MaClawOfficialServiceGroupID {
		t.Fatalf("charged = %#v, want Hub official entry", charged)
	}
}

func TestAvailabilityFallbackAttemptDowngradesHighToMidOnRetryableFailure(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name: llmpool.OfficialTierHigh,
		AvailabilityFallbacks: []llmservice.AuthorizedModel{
			{Name: llmpool.OfficialTierMid},
			{Name: llmpool.OfficialTierLow},
		},
	}
	fb, body := availabilityFallbackAttempt(model, map[string]any{"model": llmpool.OfficialTierHigh}, http.StatusBadGateway, nil)
	if fb == nil || fb.Name != llmpool.OfficialTierMid {
		t.Fatalf("fallback = %#v, want official-mid", fb)
	}
	if body["model"] != llmpool.OfficialTierMid {
		t.Fatalf("body model = %#v", body["model"])
	}
	if len(fb.AvailabilityFallbacks) != 1 || fb.AvailabilityFallbacks[0].Name != llmpool.OfficialTierLow {
		t.Fatalf("remaining fallbacks = %#v, want official-low", fb.AvailabilityFallbacks)
	}
	if next, _ := availabilityFallbackAttempt(model, nil, http.StatusBadRequest, nil); next != nil {
		t.Fatalf("400 must not availability-fallback, got %#v", next)
	}
}

func TestNextAvailabilityFallbackModelDoesNotAliasRemainingFallbacks(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name: llmpool.OfficialTierHigh,
		AvailabilityFallbacks: []llmservice.AuthorizedModel{
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"mid"}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"low"}},
		},
	}
	fb := nextAvailabilityFallbackModel(model)
	if fb == nil || len(fb.AvailabilityFallbacks) != 1 {
		t.Fatalf("fallback = %#v", fb)
	}
	fb.AvailabilityFallbacks[0].ProviderIDs[0] = "mutated"
	if got := model.AvailabilityFallbacks[1].ProviderIDs[0]; got != "low" {
		t.Fatalf("remaining fallback aliased parent slice: %q", got)
	}
}

func TestBindAvailabilityFallbackMetaUpdatesResolvedModel(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	ctx := llmservice.WithOfficialForwardMeta(req.Context(), llmservice.OfficialForwardMeta{RequestID: "req-1", ResolvedModel: llmpool.OfficialTierHigh})
	req = req.WithContext(ctx)
	ctx, out := bindAvailabilityFallbackMeta(ctx, req, llmpool.OfficialTierMid)
	if got := llmservice.OfficialForwardMetaFrom(ctx).ResolvedModel; got != llmpool.OfficialTierMid {
		t.Fatalf("ctx resolved = %q, want official-mid", got)
	}
	if got := llmservice.OfficialForwardMetaFrom(out.Context()).ResolvedModel; got != llmpool.OfficialTierMid {
		t.Fatalf("request resolved = %q, want official-mid", got)
	}
	if got := llmservice.OfficialForwardMetaFrom(ctx).RequestID; got != "req-1" {
		t.Fatalf("request id = %q, want preserved", got)
	}
}

func TestRewriteAvailabilityFallbackModelUpdatesBody(t *testing.T) {
	got := rewriteAvailabilityFallbackModel(map[string]any{"model": llmpool.OfficialTierHigh, "input": "hi"}, llmpool.OfficialTierMid)
	if got["model"] != llmpool.OfficialTierMid || got["input"] != "hi" {
		t.Fatalf("rewritten body = %#v", got)
	}
	if rewriteAvailabilityFallbackModel(nil, llmpool.OfficialTierLow)["model"] != llmpool.OfficialTierLow {
		t.Fatal("nil body should still carry the fallback model")
	}
}
