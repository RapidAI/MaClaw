package llmservice

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// Token Bank settlement hook.
//
// The proxy already records usage for every completed call. Settlement is the
// second half of that write: when the provider that answered is a Token Bank
// member, the share's owner must be credited for the work. This file is the one
// place that decision is made, and all three proxy usage sites call it through
// settleTokenBankUsage below.
//
// One entry rather than three is the whole point. The three sites (cache hit,
// buffered response, streamed response) run in different functions with
// different locals, and a rule implemented three times is a rule that will
// disagree with itself the first time one of them is edited. The differences
// that genuinely exist between the sites are resolved by the caller and passed
// in; the money rules live here.

// TokenBankSettler is what the proxy needs from the store to settle a call.
//
// It is an interface rather than a concrete *sqlite.TokenBankRepo because
// llmservice must not depend on the sqlite package: the proxy runs on workers
// that never open the store directly, and an import there would drag the whole
// database layer — and its CGO-free driver — into every binary that links the
// proxy. The app layer binds the two (see the TokenBankSettler adapter in
// app/bootstrap.go).
type TokenBankSettler interface {
	// SettleTokenBankUsage records the usage and credits the owner. It must be
	// idempotent on (request, share, model).
	SettleTokenBankUsage(ctx context.Context, in TokenBankSettlementInput) (TokenBankSettlementOutcome, error)

	// TokenBankShareForPublish resolves everything settlement needs to know
	// about a share by id: its owner, its display name, and the tier of the
	// named model. It returns found=false when the share or model no longer
	// exists, which is the normal outcome of a withdrawal that raced a call
	// already in flight.
	TokenBankShareForPublish(ctx context.Context, shareID, model string) (TokenBankShareSettlementView, bool, error)
}

// TokenBankSettlementInput is one call to settle. It carries the token counts
// and the price the consumer was charged; everything about the owner and the
// tier is resolved by the settler from the share id, so the proxy cannot
// mis-attribute a credit by passing the wrong owner.
//
// OwnerID, ShareDisplayName and Tier are filled in by the settler itself
// between the lookup and the write, and are documented here as outputs of that
// step rather than inputs from the proxy. They are fields on the struct, rather
// than a third return value, because the settler passes the same value through
// to the store's own settlement input unchanged.
type TokenBankSettlementInput struct {
	RequestID string
	ShareID   string
	Model     string

	ConsumerHubID    string
	ConsumerTenantID string

	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64

	// ChargedMicro is what the consumer paid, in microcredits, and it bounds
	// the payout (anti-inversion, §5 ⑥). The proxy computes it from the same
	// credit figure it just deducted, so the owner can never be paid more than
	// the platform collected.
	//
	// It is the ONLY thing the consumer's bill contributes to this input. The
	// owner's gross is computed from the Token Bank price book, not from the
	// consumer's route pricing: §5 makes the two sides deliberately independent
	// (the consumer keeps the service group's existing price and never sees the
	// tier), and it is exactly that independence which makes inversion possible
	// and the clamp below meaningful.
	ChargedMicro int64

	// SelfUse is set when the consumer hub is linked only to the share owner.
	// It does not skip settlement. The consumer was already charged, and the
	// owner still earns the net points. A hub other people also use is not
	// self-use: those calls still pay the owner, without this mark.
	SelfUse bool

	// --- resolved by the settler, not by the proxy ---

	OwnerID          string
	ShareDisplayName string
	Tier             string

	// TierMultiplier is the tier rate at settlement time (low 0.5 / mid 1 /
	// high 2). Read live and snapshotted onto the usage row, so a later regrade
	// cannot rewrite what an already-settled call earned.
	TierMultiplier float64

	// FeeRate is the platform's cut of gross. Snapshotted for the same reason.
	FeeRate float64

	// Unit prices come from the Token Bank price book (§5), resolved by the
	// settler. They are not a proxy input: see ChargedMicro.
	UnitInputPer10K      float64
	UnitOutputPer10K     float64
	UnitCachedReadPer10K float64
	UnitCacheWritePer10K float64
	PriceBookID          string

	At time.Time

	// memberSnapshot is copied from the provider that served this request.
	// It is used only when the share row is already gone. A caller outside
	// the proxy cannot set these fields: they are unexported.
	memberSnapshot bool
	memberOwner    string
	memberTier     string
	memberRate     float64
	memberDisplay  string
}

// TokenBankSettlementOutcome is what was applied. The proxy only logs it, so it
// stays small.
type TokenBankSettlementOutcome struct {
	GrossMicro int64
	FeeMicro   int64
	NetMicro   int64
	NetClamped bool
	Applied    bool
	// CapHit is "daily", "monthly", or "anomaly" when this settlement crossed
	// a share limit. Empty on a replay and when no limit applies.
	CapHit string
}

// TokenBankShareSettlementView is the settlement-relevant slice of a share.
//
// It carries both halves of the money: the tier rate (owner's side) and the
// price-book units (owner's side), plus the fee. The consumer's side —
// ChargedMicro — is not here, because it is the proxy's fact, not the share's.
type TokenBankShareSettlementView struct {
	ShareID          string
	OwnerUserID      string
	ShareDisplayName string
	Model            string
	Tier             string
	// TierMultiplier is the tier's rate at settlement time. It is read live and
	// snapshotted onto the usage row, so a later regrade cannot rewrite what an
	// already-settled call earned.
	TierMultiplier float64
	// FeeRate is the platform's cut, resolved from settings when zero.
	FeeRate float64

	// Price-book units (§5 分享者侧). The owner is paid from the platform price
	// book, independently of what the consumer's service group charged — that
	// independence is what makes the anti-inversion clamp meaningful.
	UnitInputPer10K      float64
	UnitOutputPer10K     float64
	UnitCachedReadPer10K float64
	UnitCacheWritePer10K float64
	PriceBookID          string
}

// SetTokenBankSettler installs the settlement dependency. It is a setter rather
// than a ProxyConfig field because the store is constructed after the proxy in
// the bootstrap order; an unset settler yields "not a share" and no settlement,
// which is exactly right when the feature is not configured.
func (c *ProxyConfig) SetTokenBankSettler(settler TokenBankSettler) {
	if c == nil {
		return
	}
	c.TokenBank = settler
}

// settleTokenBankUsage is the single settlement entry for all three proxy usage
// sites.
//
// It is deliberately forgiving: settlement is a side effect of a call that has
// already been served and already been billed to the consumer. Failing the
// request at this point would take back a response the caller already has, so
// every error path here logs and returns. The ledger integrity is protected
// instead by the settler's own idempotency, which means a lost settlement can
// be replayed without doubling a credit.
func settleTokenBankUsage(ctx context.Context, cfg *ProxyConfig, in TokenBankSettlementInput) {
	if cfg == nil || cfg.TokenBank == nil {
		return
	}
	if strings.TrimSpace(in.RequestID) == "" || strings.TrimSpace(in.ShareID) == "" {
		return
	}

	// The owner and tier are read fresh rather than carried in the request: a
	// regrade between dispatch and settle must be reflected, and a withdrawal
	// must turn the settlement into a no-op instead of crediting a ghost.
	view, found, err := cfg.TokenBank.TokenBankShareForPublish(ctx, in.ShareID, in.Model)
	if err != nil {
		log.Printf("[llm-proxy] WARN: token bank settle lookup failed share=%s model=%s request=%s: %v", in.ShareID, in.Model, in.RequestID, err)
		return
	}
	if !found {
		// The share row is gone. Pay from the provider that served THIS request
		// when it still carries the publish-time snapshot. Anything else is a
		// no-op: a crafted settlement input cannot name an owner.
		if !in.memberSnapshot || strings.TrimSpace(in.memberOwner) == "" || in.memberRate <= 0 {
			return
		}
		view = TokenBankShareSettlementView{
			ShareID:          in.ShareID,
			OwnerUserID:      in.memberOwner,
			ShareDisplayName: in.memberDisplay,
			Model:            in.Model,
			Tier:             in.memberTier,
			TierMultiplier:   in.memberRate,
		}
		if pricer, ok := cfg.TokenBank.(tokenBankPriceFallback); ok {
			priced, priceOK, priceErr := pricer.TokenBankFallbackPrice(ctx, in.Model)
			if priceErr != nil {
				log.Printf("[llm-proxy] WARN: token bank fallback price failed share=%s model=%s request=%s: %v", in.ShareID, in.Model, in.RequestID, priceErr)
				return
			}
			if priceOK {
				view.FeeRate = priced.FeeRate
				view.UnitInputPer10K = priced.UnitInputPer10K
				view.UnitOutputPer10K = priced.UnitOutputPer10K
				view.UnitCachedReadPer10K = priced.UnitCachedReadPer10K
				view.UnitCacheWritePer10K = priced.UnitCacheWritePer10K
				view.PriceBookID = priced.PriceBookID
			}
		}
		found = true
	}
	if strings.TrimSpace(view.OwnerUserID) == "" {
		return
	}
	if matcher, ok := cfg.TokenBank.(tokenBankOwnerMatcher); ok && strings.TrimSpace(in.ConsumerHubID) != "" {
		self, matchErr := matcher.TokenBankConsumerIsOwner(ctx, view.OwnerUserID, in.ConsumerHubID)
		if matchErr != nil {
			log.Printf("[llm-proxy] WARN: token bank self-use check failed share=%s request=%s: %v", in.ShareID, in.RequestID, matchErr)
		} else if self {
			// Two lines, not a waiver. The consumer charge already happened
			// upstream of this call. Dropping the earning here left the owner
			// with a debit and no credit, which is what made self-use invisible.
			in.SelfUse = true
		}
	}
	// The lookup result is authoritative over anything the proxy guessed. It
	// carries the owner, the tier rate and the price-book units: the two sides
	// of the money are resolved here, not handed in by the caller.
	in.OwnerID = view.OwnerUserID
	in.ShareDisplayName = view.ShareDisplayName
	if strings.TrimSpace(view.Tier) != "" {
		in.Tier = view.Tier
	}
	in.UnitInputPer10K = view.UnitInputPer10K
	in.UnitOutputPer10K = view.UnitOutputPer10K
	in.UnitCachedReadPer10K = view.UnitCachedReadPer10K
	in.UnitCacheWritePer10K = view.UnitCacheWritePer10K
	in.PriceBookID = view.PriceBookID

	// The tier rate and the fee come from the same fresh read. They are NOT
	// taken from the input: a caller has no business knowing either, and
	// accepting them would let a stale config land on the usage row.
	in.TierMultiplier = view.TierMultiplier
	in.FeeRate = view.FeeRate
	in.At = firstTime(in.At)

	outcome, err := cfg.TokenBank.SettleTokenBankUsage(ctx, in)
	if err != nil {
		log.Printf("[llm-proxy] WARN: token bank settlement failed share=%s model=%s request=%s: %v", in.ShareID, in.Model, in.RequestID, err)
		return
	}
	if outcome.Applied && (outcome.NetMicro > 0 || in.SelfUse) {
		log.Printf("[llm-proxy] token bank settled share=%s model=%s request=%s owner=%s net_micro=%d clamped=%v self_use=%v",
			in.ShareID, in.Model, in.RequestID, view.OwnerUserID, outcome.NetMicro, outcome.NetClamped, in.SelfUse)
	}
	if outcome.Applied && outcome.CapHit != "" {
		if pauser, ok := cfg.TokenBank.(tokenBankSharePauser); ok {
			if err := pauser.PauseTokenBankShare(ctx, in.ShareID, outcome.CapHit); err != nil {
				log.Printf("[llm-proxy] WARN: token bank cap pause failed share=%s reason=%s: %v", in.ShareID, outcome.CapHit, err)
			}
		}
	}
}

// tokenBankOwnerMatcher is optional. A settler that cannot tell reports
// false. True means the consumer hub belongs only to the share owner, which
// marks the call as self-use. It does not skip the earning.
type tokenBankOwnerMatcher interface {
	TokenBankConsumerIsOwner(ctx context.Context, ownerUserID, consumerHubID string) (bool, error)
}

// tokenBankPriceFallback supplies price-book units when the share row is gone.
type tokenBankPriceFallback interface {
	TokenBankFallbackPrice(ctx context.Context, model string) (TokenBankShareSettlementView, bool, error)
}

// tokenBankSharePauser stops routing after a cap or anomaly. Store status
// alone does not stop dispatch.
type tokenBankSharePauser interface {
	PauseTokenBankShare(ctx context.Context, shareID, reason string) error
}

// applyTokenBankMemberSnapshot copies the publish-time owner and tier off the
// provider that served this request. A nil provider, or one that is not a bank
// member, leaves the input unchanged.
func applyTokenBankMemberSnapshot(in *TokenBankSettlementInput, member *llmpool.ProviderConfig) {
	if in == nil || member == nil || !IsTokenBankMemberID(member.ID) {
		return
	}
	if strings.TrimSpace(member.TokenBankOwnerUserID) == "" || member.TokenBankTierMultiplier <= 0 {
		return
	}
	in.memberSnapshot = true
	in.memberOwner = strings.TrimSpace(member.TokenBankOwnerUserID)
	in.memberTier = strings.TrimSpace(member.TokenBankTier)
	in.memberRate = member.TokenBankTierMultiplier
	in.memberDisplay = strings.TrimSpace(member.TokenBankShareDisplayName)
}

// settleTokenBankUsageForProvider is the wrapper the three proxy sites use. It
// is the piece that turns "the provider that answered" into a settlement: a
// provider id that is not a Token Bank member settles nothing, and that check
// is one place rather than three.
//
// chargedMicro is what the consumer was charged for this attempt. Passing the
// recorded credit amount (not the pre-deduction figure) keeps the payout bounded
// by what was actually collected.
// tokenBankSettlementProviderID is the id settlement parses. Usage records
// may store a lowercased provider id. A token-bank member id must not: the
// model is base64, and lowercasing it decodes as a different model, so the
// share lookup misses and the ledger row is filed under that other name.
func tokenBankSettlementProviderID(fallback, memberID string) string {
	if id := strings.TrimSpace(memberID); id != "" {
		return id
	}
	return strings.TrimSpace(fallback)
}

func settleTokenBankUsageForProvider(ctx context.Context, cfg *ProxyConfig, req *ProxyRequest, providerID, logicalModel string, in TokenBankSettlementInput, chargedMicro int64, served *llmpool.ProviderConfig) {
	if req == nil {
		return
	}
	applyTokenBankMemberSnapshot(&in, served)
	shareID, model, ok := ParseTokenBankMemberID(providerID)
	if !ok {
		return
	}
	// The model settled is the one encoded in the member id, not the caller's
	// logical model: an array route can serve a share under a different logical
	// name than the share's own model, and the share's own model is what the
	// owner is paid for.
	if strings.TrimSpace(model) == "" {
		model = strings.TrimSpace(logicalModel)
	}
	in.RequestID = req.RequestID
	in.ShareID = shareID
	in.Model = model
	in.ConsumerHubID = req.HubID
	in.ConsumerTenantID = req.TenantID
	in.ChargedMicro = chargedMicro
	settleTokenBankUsage(ctx, cfg, in)
}

func firstTime(v time.Time) time.Time {
	if v.IsZero() {
		return time.Now().UTC()
	}
	return v.UTC()
}
