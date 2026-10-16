package llmservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// fakeSettler records what the proxy asked the store to settle, so the test can
// assert on the wiring (which calls settle, with what numbers) without a
// database. The store's own arithmetic is covered by its package tests.
type fakeSettler struct {
	lookups   []string
	settled   []TokenBankSettlementInput
	view      TokenBankShareSettlementView
	found     bool
	lookupErr error
}

func (f *fakeSettler) TokenBankShareForPublish(_ context.Context, shareID, model string) (TokenBankShareSettlementView, bool, error) {
	f.lookups = append(f.lookups, shareID+"/"+model)
	if f.lookupErr != nil {
		return TokenBankShareSettlementView{}, false, f.lookupErr
	}
	return f.view, f.found, nil
}

func (f *fakeSettler) SettleTokenBankUsage(_ context.Context, in TokenBankSettlementInput) (TokenBankSettlementOutcome, error) {
	f.settled = append(f.settled, in)
	return TokenBankSettlementOutcome{Applied: true, NetMicro: 1, GrossMicro: 2, FeeMicro: 1}, nil
}

func TestSettleTokenBankUsageIgnoresNonMemberProvider(t *testing.T) {
	// The overwhelming majority of providers are not Token Bank members. The
	// hook runs on every completed call, so a non-member must cost one prefix
	// check and nothing else — in particular no store lookup.
	settler := &fakeSettler{found: true, view: TokenBankShareSettlementView{OwnerUserID: "u1"}}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1", HubID: "h1", TenantID: "t1"}

	settleTokenBankUsageForProvider(context.Background(), cfg, req, "openai-prod", "gpt-4o", TokenBankSettlementInput{}, 5_000_000, nil)

	if len(settler.lookups) != 0 || len(settler.settled) != 0 {
		t.Fatalf("non-member provider caused lookups=%v settled=%v, want none", settler.lookups, settler.settled)
	}
}

func TestSettleTokenBankUsageCreditsMemberProvider(t *testing.T) {
	settler := &fakeSettler{
		found: true,
		view: TokenBankShareSettlementView{
			ShareID: "share-1", OwnerUserID: "owner-1", ShareDisplayName: "my llama",
			Model: "llama-3.3-70b", Tier: "high", TierMultiplier: 1.5, FeeRate: 0.1,
			UnitInputPer10K: 2, UnitOutputPer10K: 4, PriceBookID: "pb-1",
		},
	}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1", HubID: "h1", TenantID: "t1"}
	providerID := TokenBankMemberID("share-1", "llama-3.3-70b")

	settleTokenBankUsageForProvider(context.Background(), cfg, req, providerID, "llama-3.3-70b", TokenBankSettlementInput{
		InputTokens: 1000, OutputTokens: 500,
	}, 7_000_000, nil)

	if len(settler.settled) != 1 {
		t.Fatalf("settled calls = %d, want 1", len(settler.settled))
	}
	got := settler.settled[0]
	if got.RequestID != "r1" || got.ShareID != "share-1" || got.Model != "llama-3.3-70b" {
		t.Fatalf("settled key = %s/%s/%s, want r1/share-1/llama-3.3-70b", got.RequestID, got.ShareID, got.Model)
	}
	// The owner comes from the share lookup, never from the request.
	if got.OwnerID != "owner-1" {
		t.Fatalf("OwnerID = %q, want owner-1 (resolved from the share)", got.OwnerID)
	}
	if got.ShareDisplayName != "my llama" {
		t.Fatalf("ShareDisplayName = %q, want the snapshot from the share", got.ShareDisplayName)
	}
	if got.Tier != "high" || got.TierMultiplier != 1.5 || got.FeeRate != 0.1 {
		t.Fatalf("tier/multiplier/fee = %q/%v/%v, want high/1.5/0.1", got.Tier, got.TierMultiplier, got.FeeRate)
	}
	// The owner's units come from the price book (via the view), not from the
	// consumer's route pricing: §5 keeps the two sides independent.
	if got.UnitInputPer10K != 2 || got.UnitOutputPer10K != 4 || got.PriceBookID != "pb-1" {
		t.Fatalf("units/book = %v/%v/%q, want 2/4/pb-1 from the price book",
			got.UnitInputPer10K, got.UnitOutputPer10K, got.PriceBookID)
	}
	if got.ChargedMicro != 7_000_000 {
		t.Fatalf("ChargedMicro = %d, want 7000000", got.ChargedMicro)
	}
	if got.ConsumerHubID != "h1" || got.ConsumerTenantID != "t1" {
		t.Fatalf("consumer = %s/%s, want h1/t1", got.ConsumerHubID, got.ConsumerTenantID)
	}
	if got.InputTokens != 1000 || got.OutputTokens != 500 {
		t.Fatalf("tokens = %d/%d, want 1000/500", got.InputTokens, got.OutputTokens)
	}
}

func TestSettleTokenBankUsageUsesModelFromMemberID(t *testing.T) {
	// An array route can carry a logical model name that differs from the
	// share's own model (that is what makes a tier array work). The share's own
	// model is what the owner is paid for, so the member id wins.
	settler := &fakeSettler{found: true, view: TokenBankShareSettlementView{OwnerUserID: "owner-1", Model: "actual-share-model"}}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1"}
	providerID := TokenBankMemberID("share-1", "actual-share-model")

	settleTokenBankUsageForProvider(context.Background(), cfg, req, providerID, "logical-array-name", TokenBankSettlementInput{}, 0, nil)

	if len(settler.settled) != 1 {
		t.Fatalf("settled calls = %d, want 1", len(settler.settled))
	}
	if got := settler.settled[0].Model; got != "actual-share-model" {
		t.Fatalf("Model = %q, want actual-share-model (from the member id)", got)
	}
}

func TestSettleTokenBankUsageSkipsWithdrawnShare(t *testing.T) {
	// The share was taken out between dispatch and settle. That is not an error
	// and must not produce a settlement: there is no owner to pay.
	settler := &fakeSettler{found: false}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1"}
	providerID := TokenBankMemberID("share-gone", "llama")

	settleTokenBankUsageForProvider(context.Background(), cfg, req, providerID, "llama", TokenBankSettlementInput{}, 1_000_000, nil)

	if len(settler.settled) != 0 {
		t.Fatalf("settled calls = %d, want 0 for a withdrawn share", len(settler.settled))
	}
}

func TestSettleTokenBankUsageNilSettlerIsNoOp(t *testing.T) {
	// A deployment without Token Bank configured installs no settler. The hook
	// must be a no-op rather than a nil dereference on every proxied request.
	req := &ProxyRequest{RequestID: "r1"}
	providerID := TokenBankMemberID("share-1", "llama")

	settleTokenBankUsageForProvider(context.Background(), &ProxyConfig{}, req, providerID, "llama", TokenBankSettlementInput{}, 0, nil)
	settleTokenBankUsageForProvider(context.Background(), nil, req, providerID, "llama", TokenBankSettlementInput{}, 0, nil)
}

func TestSettleTokenBankUsageOwnerUnitsNotConsumerPricing(t *testing.T) {
	// §5: the consumer keeps their service group's price and never sees the
	// tier; the owner is paid from the platform price book. The two sides are
	// deliberately independent, so a caller that tries to hand in its own units
	// must not be able to overwrite the book's.
	settler := &fakeSettler{
		found: true,
		view: TokenBankShareSettlementView{
			OwnerUserID: "owner-1", Model: "llama", Tier: "mid", TierMultiplier: 1, FeeRate: 0.2,
			UnitInputPer10K: 1, UnitOutputPer10K: 2, PriceBookID: "book",
		},
	}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1"}
	providerID := TokenBankMemberID("share-1", "llama")

	settleTokenBankUsageForProvider(context.Background(), cfg, req, providerID, "llama", TokenBankSettlementInput{
		// A caller that tries to supply its own owner, units or tier must not
		// be able to influence the settlement: every one of these is resolved
		// from the share.
		OwnerID: "attacker", UnitInputPer10K: 999, UnitOutputPer10K: 999, PriceBookID: "consumer-route",
		Tier: "high", TierMultiplier: 99, FeeRate: 0.99,
	}, 0, nil)

	if len(settler.settled) != 1 {
		t.Fatalf("settled calls = %d, want 1", len(settler.settled))
	}
	got := settler.settled[0]
	if got.UnitInputPer10K != 1 || got.UnitOutputPer10K != 2 {
		t.Fatalf("units = %v/%v, want 1/2 from the price book", got.UnitInputPer10K, got.UnitOutputPer10K)
	}
	if got.PriceBookID != "book" {
		t.Fatalf("PriceBookID = %q, want book (the price book's rule)", got.PriceBookID)
	}
	if got.OwnerID != "owner-1" {
		t.Fatalf("OwnerID = %q, want owner-1 (never the caller's)", got.OwnerID)
	}
	if got.Tier != "mid" || got.TierMultiplier != 1 || got.FeeRate != 0.2 {
		t.Fatalf("tier/multiplier/fee = %q/%v/%v, want mid/1/0.2 from the share",
			got.Tier, got.TierMultiplier, got.FeeRate)
	}
}

type ownerAwareSettler struct {
	*fakeSettler
	self bool
	err  error
}

func (s *ownerAwareSettler) TokenBankConsumerIsOwner(context.Context, string, string) (bool, error) {
	return s.self, s.err
}

func TestSettleTokenBankUsageRecordsSelfUseOnBothSides(t *testing.T) {
	settler := &ownerAwareSettler{
		fakeSettler: &fakeSettler{found: true, view: TokenBankShareSettlementView{OwnerUserID: "owner-1", Model: "llama", TierMultiplier: 1}},
		self:        true,
	}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1", HubID: "hub-owner"}
	settleTokenBankUsageForProvider(context.Background(), cfg, req, TokenBankMemberID("share-1", "llama"), "llama", TokenBankSettlementInput{}, 1_000_000, nil)
	if len(settler.settled) != 1 {
		t.Fatalf("settled = %d, want 1 when the owner uses their own share", len(settler.settled))
	}
	got := settler.settled[0]
	if !got.SelfUse {
		t.Fatal("SelfUse = false, want the call marked so the earning stays distinguishable from outside use")
	}
	if got.ChargedMicro != 1_000_000 {
		t.Fatalf("ChargedMicro = %d, want the consumer charge kept on the same settlement", got.ChargedMicro)
	}
	if got.OwnerID != "owner-1" {
		t.Fatalf("OwnerID = %q, want the sharer credited on their own call", got.OwnerID)
	}
}

func TestSettleTokenBankUsagePaysWhenSelfUseLookupFails(t *testing.T) {
	settler := &ownerAwareSettler{
		fakeSettler: &fakeSettler{found: true, view: TokenBankShareSettlementView{OwnerUserID: "owner-1", Model: "llama", TierMultiplier: 1}},
		err:         errors.New("link lookup failed"),
	}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1", HubID: "hub-owner"}
	settleTokenBankUsageForProvider(context.Background(), cfg, req, TokenBankMemberID("share-1", "llama"), "llama", TokenBankSettlementInput{}, 1_000_000, nil)
	if len(settler.settled) != 1 {
		t.Fatalf("settled = %d, want 1 when the self-use lookup fails", len(settler.settled))
	}
}

type snapshotPricer struct {
	*fakeSettler
	priceErr error
}

func (s *snapshotPricer) TokenBankFallbackPrice(context.Context, string) (TokenBankShareSettlementView, bool, error) {
	if s.priceErr != nil {
		return TokenBankShareSettlementView{}, false, s.priceErr
	}
	return TokenBankShareSettlementView{
		FeeRate: 0.1, UnitInputPer10K: 3, UnitOutputPer10K: 6, PriceBookID: "fallback-book",
	}, true, nil
}

func TestSettleTokenBankUsageUsesInFlightSnapshotWhenShareIsGone(t *testing.T) {
	settler := &snapshotPricer{fakeSettler: &fakeSettler{found: false}}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r-inflight", HubID: "hub-other"}
	memberID := TokenBankMemberID("share-gone", "llama")
	served := &llmpool.ProviderConfig{
		ID:                        memberID,
		TokenBankOwnerUserID:      "owner-1",
		TokenBankTier:             "high",
		TokenBankTierMultiplier:   1.2,
		TokenBankShareDisplayName: "kept name",
	}
	settleTokenBankUsageForProvider(context.Background(), cfg, req, memberID, "llama", TokenBankSettlementInput{
		InputTokens: 10,
	}, 1_000_000, served)
	if len(settler.settled) != 1 {
		t.Fatalf("settled = %d, want 1 from the in-flight snapshot", len(settler.settled))
	}
	got := settler.settled[0]
	if got.OwnerID != "owner-1" || got.Tier != "high" || got.TierMultiplier != 1.2 || got.ShareDisplayName != "kept name" {
		t.Fatalf("snapshot owner/tier = %+v", got)
	}
	if got.UnitInputPer10K != 3 || got.UnitOutputPer10K != 6 || got.PriceBookID != "fallback-book" || got.FeeRate != 0.1 {
		t.Fatalf("fallback price = %+v", got)
	}
}

func TestSettleTokenBankUsageDropsInFlightWhenFallbackPriceFails(t *testing.T) {
	settler := &snapshotPricer{fakeSettler: &fakeSettler{found: false}, priceErr: errors.New("price book down")}
	cfg := &ProxyConfig{TokenBank: settler}
	memberID := TokenBankMemberID("share-gone", "llama")
	served := &llmpool.ProviderConfig{ID: memberID, TokenBankOwnerUserID: "owner-1", TokenBankTierMultiplier: 1}
	settleTokenBankUsageForProvider(context.Background(), cfg, &ProxyRequest{RequestID: "r1"}, memberID, "llama", TokenBankSettlementInput{}, 1, served)
	if len(settler.settled) != 0 {
		t.Fatalf("settled = %d, want 0 when the fallback price fails", len(settler.settled))
	}
}

type capPauser struct {
	*fakeSettler
	applied bool
	cap     string
	pauses  []string
}

func (s *capPauser) SettleTokenBankUsage(_ context.Context, in TokenBankSettlementInput) (TokenBankSettlementOutcome, error) {
	s.settled = append(s.settled, in)
	return TokenBankSettlementOutcome{Applied: s.applied, CapHit: s.cap, NetMicro: 1}, nil
}

func (s *capPauser) PauseTokenBankShare(_ context.Context, shareID, reason string) error {
	s.pauses = append(s.pauses, shareID+"/"+reason)
	return nil
}

func TestSettleTokenBankUsagePausesOnCapHitOnce(t *testing.T) {
	settler := &capPauser{
		fakeSettler: &fakeSettler{found: true, view: TokenBankShareSettlementView{OwnerUserID: "owner-1", Model: "llama", TierMultiplier: 1}},
		applied:     true,
		cap:         "daily",
	}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r1", HubID: "other-hub"}
	id := TokenBankMemberID("share-1", "llama")
	settleTokenBankUsageForProvider(context.Background(), cfg, req, id, "llama", TokenBankSettlementInput{}, 1, nil)
	if len(settler.pauses) != 1 || settler.pauses[0] != "share-1/daily" {
		t.Fatalf("pauses = %v, want share-1/daily", settler.pauses)
	}
	settler.applied = false
	settler.cap = "daily"
	settleTokenBankUsageForProvider(context.Background(), cfg, req, id, "llama", TokenBankSettlementInput{}, 1, nil)
	if len(settler.pauses) != 1 {
		t.Fatalf("replay pauses = %v, want the first pause only", settler.pauses)
	}
}

// Stream usage records store a lowercased provider id. Settlement must keep
// the member id's original spelling, because the model is case-sensitive base64.
func TestRecordProxyStreamUsageSettlesOriginalMemberModel(t *testing.T) {
	memberID := TokenBankMemberID("share-1", "gpt-4o-mini")
	if memberID == strings.ToLower(memberID) {
		t.Fatal("fixture member id has no uppercase base64")
	}
	settler := &fakeSettler{found: true, view: TokenBankShareSettlementView{OwnerUserID: "owner-1", TierMultiplier: 1}}
	member := &llmpool.ProviderConfig{
		ID: memberID, TokenBankOwnerUserID: "owner-1", TokenBankTierMultiplier: 1, TokenBankShareDisplayName: "my-share",
	}
	cfg := &ProxyConfig{TokenBank: settler}
	req := &ProxyRequest{RequestID: "r-stream", HubID: "hub-1", TenantID: "tenant-1"}
	dispatch := &proxyDispatch{model: "gpt-4o-mini", provider: member, logicalProviderID: TokenBankArrayMid}
	recordProxyStreamUsage(context.Background(), cfg, req, dispatch, TokenBankArrayMid, memberID, &providerStreamResult{
		statusCode: 200, inputTokens: 3, outputTokens: 4, inputTokensObserved: true, outputTokensObserved: true,
	}, member)
	if len(settler.settled) != 1 {
		t.Fatalf("settled calls = %d, want 1", len(settler.settled))
	}
	got := settler.settled[0]
	if got.ShareID != "share-1" || got.Model != "gpt-4o-mini" || got.InputTokens != 3 || got.OutputTokens != 4 {
		t.Fatalf("stream settlement = %+v", got)
	}
}
