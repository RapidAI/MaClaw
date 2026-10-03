package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

type admissionPullRecorder struct {
	calls  int
	groups []string
}

func (p *admissionPullRecorder) PullTokenBankForAdmissionShortfall(_ context.Context, _, _ string, chargedGroupIDs []string, _ int64) (bool, error) {
	p.calls++
	p.groups = append([]string(nil), chargedGroupIDs...)
	return true, nil
}

func TestPullTokenBankForQuoteShortfallPullsWhenTheCardCannotCoverTheFloor(t *testing.T) {
	now := time.Now().UTC()
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "card", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 5.34, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 15.042, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want one pull when 15.042 exceeds the 5.34 card", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenTheRestoredCeilingExceedsTheCard(t *testing.T) {
	now := time.Now().UTC()
	quote, ok := llmpool.NewPricingQuoteSnapshot(
		"req-restored", "req-restored:1", "maclaw",
		llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 4.75,
		}},
		1, 1, 100, 65_536, now.Add(time.Minute),
	)
	if !ok {
		t.Fatal("quote")
	}
	one, oneOK := requoteAtOutputLimit(quote, quote.InputTokenEstimate, 1)
	if !oneOK {
		t.Fatal("requote")
	}
	const card = 0.409
	if llmpool.MicrocreditsToCredits(one.ReservedMicrocredits) > card {
		t.Fatalf("floor = %.3f, want the card to cover one output token", llmpool.MicrocreditsToCredits(one.ReservedMicrocredits))
	}
	if llmpool.MicrocreditsToCredits(quote.ReservedMicrocredits) <= card {
		t.Fatalf("reserved = %.3f, want the restored ceiling above the card", llmpool.MicrocreditsToCredits(quote.ReservedMicrocredits))
	}
	ctx := withLLMBillingState(context.Background(), now, "req-restored")
	rememberLLMPricingQuote(ctx, quote)
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(card), "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", llmpool.MicrocreditsToCredits(one.ReservedMicrocredits), []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when the restored ceiling exceeds %.3f and one token does not", pulled, puller.calls, card)
	}
}

func TestPullTokenBankForQuoteShortfallWaitsWhenAHoldHidesTheCardUnderARestoredCeiling(t *testing.T) {
	now := time.Now().UTC()
	quote, ok := llmpool.NewPricingQuoteSnapshot(
		"req-held", "req-held:1", "maclaw",
		llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 4.75,
		}},
		1, 1, 100, 65_536, now.Add(time.Minute),
	)
	if !ok {
		t.Fatal("quote")
	}
	one, oneOK := requoteAtOutputLimit(quote, quote.InputTokenEstimate, 1)
	if !oneOK {
		t.Fatal("requote")
	}
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: now.Add(time.Hour),
	}}
	ctx := withLLMBillingState(context.Background(), now, "req-held")
	rememberLLMPricingQuote(ctx, quote)
	puller := &admissionPullRecorder{}
	floor := llmpool.MicrocreditsToCredits(one.ReservedMicrocredits)
	pulled, err := pullTokenBankForQuoteShortfall(ctx, reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", floor, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when a hold clamps the 5.34 card and the floor %.3f still fits it", pulled, puller.calls, floor)
	}
}

func TestPullTokenBankForQuoteShortfallLeavesACardThatCoversTheFloor(t *testing.T) {
	now := time.Now().UTC()
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "card", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 5.34, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 3.5, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when the card covers the floor", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallSkipsUnlimitedAndPeriodLimits(t *testing.T) {
	now := time.Now().UTC()
	unlimited := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "open", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), unlimited, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 15, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("unlimited grant pulled=%v calls=%d", pulled, puller.calls)
	}
	metered := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "card", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 1, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), metered, "u1", "user@example.com", "LLM_SERVICE_PERIOD_LIMITED", 15, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("period limit pulled=%v calls=%d", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenAHoldExceedsTheCard(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 6, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when a 10 credit hold clamps available to 0 but the 5.34 card cannot pay 6", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallWaitsWhenSpendableCoversTheFloor(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 1.49, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 4, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when the 5.34 card can pay 4 after the hold releases", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallKeepsThePricedFloor(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(context.Background(), now, "req")
	rememberLLMPricingQuote(ctx, llmpool.PricingQuoteSnapshot{
		ProviderID: "local", LogicalModel: "mid", ReservedMicrocredits: 1000,
	})
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(5.34), "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 15.042, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want the 15.042 floor to pull even when a stored quote reserves 0.001", pulled, puller.calls)
	}
	idle := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(5.34), "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, idle)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || idle.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when the denial has no floor and the stored 0.001 quote fits the card", pulled, idle.calls)
	}
}

func TestPullTokenBankForQuoteShortfallIgnoresAnUnrequotableReservation(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(context.Background(), now, "req")
	rememberLLMPricingQuote(ctx, llmpool.PricingQuoteSnapshot{
		ProviderID: "local", LogicalModel: "mid", ReservedMicrocredits: 20 * 1_000_000,
	})
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(5.34), "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when the quote cannot be priced at one output token and the card still has 5.34", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallWaitsWhenHoldsHideAnUnpricedCard(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when holds hide a 5.34 card and the denial has no floor", pulled, puller.calls)
	}

	empty := paidCardRegistry(1)
	empty.Grants[0].CreditsUsed = 1
	emptyPuller := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), empty, "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, emptyPuller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || emptyPuller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when the card itself is empty", pulled, emptyPuller.calls)
	}
}

func TestCoverFilteredModelsPullsOnlyTheShortModel(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "other", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		Grants: []llmservice.Grant{
			paidGrant("paid-card", "paid", 5),
			paidGrant("rich-card", "other", 100),
		},
	}
	models := []llmservice.AuthorizedModel{
		{Name: "rich", ChargedServiceGroupIDs: []string{"other"}},
		{Name: "poor", ChargedServiceGroupIDs: []string{"paid"}},
	}
	denied := map[string]llmBillingDenial{
		"rich": {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 1},
		"poor": {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["rich"])
	if puller.calls != 1 || !sameGroups(puller.seen[0], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want one pull for the paid card only", puller.calls, puller.seen)
	}
}

func TestCoverFilteredModelsDoesNotAddProviderWallets(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "other", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		Grants: []llmservice.Grant{
			paidGrant("paid-card", "paid", 5),
			paidGrant("rich-card", "other", 100),
		},
	}
	models := []llmservice.AuthorizedModel{{
		Name:        "split",
		ProviderIDs: []string{"a", "b"},
		ProviderServiceGroups: map[string][]string{
			"a": {"paid"},
			"b": {"other"},
		},
	}}
	denied := map[string]llmBillingDenial{
		"split": {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["split"])
	if puller.calls != 1 || !sameGroups(puller.seen[0], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want one pull for the short provider wallet", puller.calls, puller.seen)
	}
}

func TestCoverFilteredModelsSkipsAGroupTheBankDoesNotPay(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "other", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		Grants: []llmservice.Grant{paidGrant("paid-card", "paid", 5)},
	}
	models := []llmservice.AuthorizedModel{
		{Name: "other-model", ChargedServiceGroupIDs: []string{"other"}},
		{Name: "paid-model", ChargedServiceGroupIDs: []string{"paid"}},
	}
	denied := map[string]llmBillingDenial{
		"other-model": {Code: "LLM_SERVICE_CREDITS_EXHAUSTED"},
		"paid-model":  {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		if sameGroups(groups, []string{"other"}) {
			return false, llmservice.ErrTokenBankGroupNotCharged
		}
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["other-model"])
	if puller.calls != 2 || !sameGroups(puller.seen[0], []string{"other"}) || !sameGroups(puller.seen[1], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want the uncharged group skipped and the paid card tried next", puller.calls, puller.seen)
	}
}

type scriptedAdmissionPuller struct {
	calls  int
	seen   [][]string
	decide func(groups []string) (bool, error)
}

func (p *scriptedAdmissionPuller) PullTokenBankForAdmissionShortfall(_ context.Context, _, _ string, chargedGroupIDs []string, _ int64) (bool, error) {
	p.calls++
	copied := append([]string(nil), chargedGroupIDs...)
	p.seen = append(p.seen, copied)
	if p.decide != nil {
		return p.decide(copied)
	}
	return false, nil
}

func paidCardRegistry(credits float64) *llmservice.Registry {
	return &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{paidGrant("card", "paid", credits)},
	}
}

func paidGrant(id, group string, credits float64) llmservice.Grant {
	now := time.Now().UTC()
	return llmservice.Grant{
		ID: id, UserID: "u1", Email: "user@example.com", ServiceGroupID: group,
		CreditsTotal: credits, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
	}
}

func TestPrepareAndReserveRefitsAStaleHandlerRegistryBeforeWithdrawing(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		writeOfficialTestQuote(w, "quote-stale", 2)
	})
	ctx, system, handler, model, body := admissionReserveFixture(t, "req-stale-registry", 10_000, 4)
	puller := &admissionPullRecorder{}
	_, denial, err := prepareAndReserveLLMPricing(ctx, system, &im.LLMProviderRegistry{}, handler, model, body, "u1", "user@example.com", puller)
	if err != nil || denial.Code != "" {
		t.Fatalf("reserve after refit: code=%s err=%v", denial.Code, err)
	}
	if puller.calls != 0 {
		t.Fatalf("pulls=%d, want none when the loaded card can still start the call", puller.calls)
	}
	got, ok := llmQuotePositiveInt64(body["max_tokens"])
	if !ok || got <= 0 || got >= 65_536 {
		t.Fatalf("max_tokens=%d ok=%v, want a ceiling fitted to the loaded 4 credit card", got, ok)
	}
}

func TestPrepareAndReservePullsWhenTheFreshCardCannotCoverTheFloor(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		writeAdmissionOfficialQuote(w, "quote-floor", 1_000_000, 1_000_000)
	})
	ctx, system, handler, model, body := admissionReserveFixture(t, "req-floor-pull", 4, 4)
	puller := &admissionPullRecorder{}
	_, denial, err := prepareAndReserveLLMPricing(ctx, system, &im.LLMProviderRegistry{}, handler, model, body, "u1", "user@example.com", puller)
	if err == nil || denial.Code != "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST" {
		t.Fatalf("code=%s err=%v, want the floor to stay denied", denial.Code, err)
	}
	if puller.calls != 1 || !sameGroups(puller.groups, []string{"official-group"}) {
		t.Fatalf("pulls=%d groups=%v, want one pull from official-group", puller.calls, puller.groups)
	}
}

func admissionReserveFixture(t *testing.T, requestID string, handlerCredits, storedCredits float64) (context.Context, *testLLMServiceSystemSettings, *llmservice.Registry, *llmservice.AuthorizedModel, map[string]any) {
	t.Helper()
	now := time.Now().UTC()
	ctx := officialFitContext(t, requestID, now)
	system := newTestLLMServiceSystemSettings()
	invalidateLLMRuntimeCaches(system)
	if err := llmservice.SaveRegistry(ctx, system, officialFitRegistry(now, storedCredits)); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	return ctx, system, officialFitRegistry(now, handlerCredits), officialFitModel(), map[string]any{"model": "official-mid", "max_tokens": 65_536}
}

func writeAdmissionOfficialQuote(w http.ResponseWriter, token string, inputPer10k, outputPer10k float64) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token": token,
		"quote": map[string]any{
			"provider_id":         "agnes",
			"upstream_model":      "upstream-model",
			"service_group_id":    "official-group",
			"provider_multiplier": 1,
			"expires_at":          time.Now().UTC().Add(time.Minute),
			"pricing": map[string]any{
				"input_credits_per_10k":  inputPer10k,
				"output_credits_per_10k": outputPer10k,
			},
		},
	})
}

func sameGroups(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
