package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// tokenBankProxySettler binds the SQLite Token Bank repository to the proxy's
// settlement interface (§5, P0-3).
//
// The interface exists because llmservice must not import the sqlite package:
// the proxy is linked into binaries that never open the database, and an import
// would pull the whole store layer in behind it. The adapter is the only place
// the two halves meet, and it lives here because app is already the package that
// knows about both.
//
// It is deliberately thin. Every rule about owner, tier and fee is applied by
// the store's SettleTokenBankUsage, which is the transaction boundary — putting
// a second copy of those rules here would create a way for the two to disagree
// about money.
type tokenBankProxySettler struct {
	repo     *sqlite.TokenBankRepo
	settings store.SystemSettingsRepository
	llm      *llmservice.Service
}

// newTokenBankProxySettler returns nil when the repository is missing, so the
// caller can install the result unconditionally: a nil settler leaves the proxy
// hook a no-op, which is the correct behaviour on a node without Token Bank.
func newTokenBankProxySettler(repo *sqlite.TokenBankRepo, settings store.SystemSettingsRepository) llmservice.TokenBankSettler {
	if repo == nil {
		return nil
	}
	return &tokenBankProxySettler{repo: repo, settings: settings}
}

// TokenBankShareForPublish resolves the owner, name and tier of one shared
// model.
//
// The model lookup is by name because that is what a member id carries. A share
// whose model row was removed by a re-sync (the provider stopped offering it) is
// reported as not found, which turns an in-flight settlement into a no-op rather
// than a credit to a model that no longer exists.
func (s *tokenBankProxySettler) TokenBankShareForPublish(ctx context.Context, shareID, model string) (llmservice.TokenBankShareSettlementView, bool, error) {
	var view llmservice.TokenBankShareSettlementView
	if s == nil || s.repo == nil {
		return view, false, nil
	}
	shareID = strings.TrimSpace(shareID)
	model = strings.TrimSpace(model)
	if shareID == "" || model == "" {
		return view, false, nil
	}

	// scopeOwner is empty: settlement runs inside the proxy, where there is no
	// session user. Ownership is irrelevant here because nobody is authorising
	// anything — the credit goes to whoever the share says owns it, and the
	// proxy has already decided to route to this member.
	share, err := s.repo.LoadShare(ctx, shareID, "")
	if err != nil {
		if errors.Is(err, sqlite.ErrTokenBankShareNotFound) {
			return view, false, nil
		}
		return view, false, err
	}
	if share == nil {
		return view, false, nil
	}
	// A paused share is not routable, so a settlement for one is a race with the
	// pause. Pay it anyway: the work happened, and withholding earned credits
	// because an unrelated admin action landed mid-request would be a bug the
	// owner cannot appeal.
	models, err := s.repo.ListModels(ctx, shareID)
	if err != nil {
		return view, false, err
	}
	var matched *sqlite.TokenBankShareModel
	for i := range models {
		if strings.EqualFold(strings.TrimSpace(models[i].ModelName), model) {
			matched = &models[i]
			break
		}
	}
	if matched == nil {
		return view, false, nil
	}

	view.ShareID = share.ID
	view.OwnerUserID = share.OwnerUserID
	view.ShareDisplayName = share.DisplayName
	view.Model = matched.ModelName
	view.Tier = matched.Tier
	view.TierMultiplier = matched.TierMultiplier

	// §5: the owner is paid from the platform price book, NOT from the price
	// the consumer's service group charged. The two sides are deliberately
	// independent — that independence is what makes the anti-inversion clamp
	// load-bearing rather than decorative — so the units are resolved here from
	// the book, and fall back to the settings defaults when no rule matches.
	settings := s.currentSettings(ctx)
	view.FeeRate = settings.FeeRate
	view.UnitInputPer10K = settings.DefaultUnitInputPer10K
	view.UnitOutputPer10K = settings.DefaultUnitOutputPer10K
	view.UnitCachedReadPer10K = settings.DefaultUnitCachedReadPer10K
	view.UnitCacheWritePer10K = settings.DefaultUnitCacheWritePer10K
	rule, ok, err := s.repo.ResolvePrice(ctx, matched.ModelName)
	if err != nil {
		// A failed lookup must not settle at the defaults. The usage row is
		// idempotent, so the wrong units would stick.
		return llmservice.TokenBankShareSettlementView{}, false, err
	}
	if ok {
		applyTokenBankPriceRule(&view, rule)
	}
	return view, true, nil
}

// SettleTokenBankUsage forwards to the store, mapping the two shape-identical
// structs. The mapping is explicit rather than an embedded type so a new field
// on one side is a compile error here instead of a silently dropped value.
func (s *tokenBankProxySettler) SettleTokenBankUsage(ctx context.Context, in llmservice.TokenBankSettlementInput) (llmservice.TokenBankSettlementOutcome, error) {
	var out llmservice.TokenBankSettlementOutcome
	if s == nil || s.repo == nil {
		return out, nil
	}
	res, err := s.repo.SettleTokenBankUsage(ctx, sqlite.TokenBankSettlement{
		RequestID:            in.RequestID,
		ShareID:              in.ShareID,
		OwnerID:              in.OwnerID,
		ShareDisplayName:     in.ShareDisplayName,
		ModelName:            in.Model,
		ConsumerHubID:        in.ConsumerHubID,
		ConsumerTenantID:     in.ConsumerTenantID,
		InputTokens:          in.InputTokens,
		OutputTokens:         in.OutputTokens,
		CachedInputTokens:    in.CachedInputTokens,
		CacheWriteTokens:     in.CacheWriteTokens,
		Tier:                 in.Tier,
		TierMultiplier:       in.TierMultiplier,
		FeeRate:              in.FeeRate,
		ChargedMicro:         in.ChargedMicro,
		UnitInputPer10K:      in.UnitInputPer10K,
		UnitOutputPer10K:     in.UnitOutputPer10K,
		UnitCachedReadPer10K: in.UnitCachedReadPer10K,
		UnitCacheWritePer10K: in.UnitCacheWritePer10K,
		PriceBookID:          in.PriceBookID,
		CreatedAt:            in.At,
		SelfUse:              in.SelfUse,
	})
	if err != nil {
		return out, err
	}
	out.GrossMicro = res.GrossMicro
	out.FeeMicro = res.FeeMicro
	out.NetMicro = res.NetMicro
	out.NetClamped = res.NetClamped
	out.Applied = res.Applied
	out.CapHit = res.CapHit
	return out, nil
}

func (s *tokenBankProxySettler) TokenBankConsumerIsOwner(ctx context.Context, ownerUserID, consumerHubID string) (bool, error) {
	if s == nil || s.repo == nil {
		return false, nil
	}
	return s.repo.HubIsExclusiveToUser(ctx, consumerHubID, ownerUserID)
}

func (s *tokenBankProxySettler) TokenBankFallbackPrice(ctx context.Context, model string) (llmservice.TokenBankShareSettlementView, bool, error) {
	var view llmservice.TokenBankShareSettlementView
	if s == nil || s.repo == nil {
		return view, false, nil
	}
	settings := s.currentSettings(ctx)
	view.FeeRate = settings.FeeRate
	view.UnitInputPer10K = settings.DefaultUnitInputPer10K
	view.UnitOutputPer10K = settings.DefaultUnitOutputPer10K
	view.UnitCachedReadPer10K = settings.DefaultUnitCachedReadPer10K
	view.UnitCacheWritePer10K = settings.DefaultUnitCacheWritePer10K
	rule, ok, err := s.repo.ResolvePrice(ctx, model)
	if err != nil {
		return view, false, err
	}
	if ok {
		applyTokenBankPriceRule(&view, rule)
	}
	return view, true, nil
}

// applyTokenBankPriceRule copies a matching price-book rule onto a view that
// already holds the settings defaults. Input and output of 0 are real prices:
// a free model. A cache unit of 0 is a blank field stored as 0, so it leaves
// the default cache price in place. A positive cache unit replaces that
// default. Settlement prices that positive unit on its own leg, and a resolved
// cache unit that is still 0 on the input rate.
func applyTokenBankPriceRule(view *llmservice.TokenBankShareSettlementView, rule sqlite.TokenBankPriceRule) {
	view.UnitInputPer10K = rule.UnitInputPer10K
	view.UnitOutputPer10K = rule.UnitOutputPer10K
	if rule.UnitCachedReadPer10K > 0 {
		view.UnitCachedReadPer10K = rule.UnitCachedReadPer10K
	}
	if rule.UnitCacheWritePer10K > 0 {
		view.UnitCacheWritePer10K = rule.UnitCacheWritePer10K
	}
	view.PriceBookID = rule.ID
}

func (s *tokenBankProxySettler) PauseTokenBankShare(ctx context.Context, shareID, reason string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	// The usage-spike brake is retired. Ignore a stale "anomaly" hit so a
	// share cannot be paused for it again.
	if strings.EqualFold(strings.TrimSpace(reason), "anomaly") {
		return nil
	}
	note := "token cap: " + strings.TrimSpace(reason)
	now := time.Now().UTC()
	return llmservice.WithTokenBankRouteLock(func() error {
		if err := s.repo.SetSharePaused(ctx, shareID, "", note, true, now); err != nil {
			return err
		}
		_ = s.repo.NoteShareModelError(ctx, shareID, "", note)
		if s.llm == nil {
			return nil
		}
		_, err := s.llm.SetTokenBankSharePaused(ctx, shareID, true)
		return err
	})
}

// resumeSharesPausedForUsageSpike makes shares retired for a usage spike
// routable again. Other pause reasons stay paused.
//
// Members are resumed before the row, inside the route lock. A republish of an
// active row keeps a member pause, so clearing the row first and then failing
// the member update would leave the share looking available while it stays
// dark. The row stays paused until the member update succeeds, and the next
// start retries it. The spike note stays on the row after it becomes active,
// so a later registry snapshot that pauses the members is undone the next time
// this runs.
func resumeSharesPausedForUsageSpike(ctx context.Context, bank *sqlite.TokenBankRepo, svc *llmservice.Service) {
	if bank == nil {
		return
	}
	ids, err := bank.ListUsageSpikePausedShareIDs(ctx)
	if err != nil {
		log.Printf("[token-bank] list usage-spike pauses: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, id := range ids {
		if err := resumeOneUsageSpikePause(ctx, bank, svc, id, now); err != nil {
			log.Printf("[token-bank] resume usage-spike pause %s: %v", id, err)
		}
	}
}

func resumeOneUsageSpikePause(ctx context.Context, bank *sqlite.TokenBankRepo, svc *llmservice.Service, shareID string, now time.Time) error {
	if svc == nil {
		released, err := bank.ReleaseUsageSpikePause(ctx, shareID, now)
		if err != nil {
			return err
		}
		if released {
			log.Printf("[token-bank] resumed share %s after retiring the usage-spike pause", shareID)
		}
		return nil
	}
	return llmservice.WithTokenBankRouteLock(func() error {
		marked, rowPaused, err := bank.UsageSpikePauseHeld(ctx, shareID)
		if err != nil || !marked {
			return err
		}
		if _, err := svc.SetTokenBankSharePaused(ctx, shareID, false); err != nil {
			return err
		}
		if !rowPaused {
			return nil
		}
		released, err := bank.ReleaseUsageSpikePause(ctx, shareID, now)
		if err != nil || !released {
			if _, revErr := svc.SetTokenBankSharePaused(ctx, shareID, true); revErr != nil {
				log.Printf("[token-bank] share %s member resume revert failed: %v", shareID, revErr)
			}
			return err
		}
		log.Printf("[token-bank] resumed share %s after retiring the usage-spike pause", shareID)
		return nil
	})
}

// scheduleUsageSpikeResume runs the spike resume after a replicated registry
// lands. One run at a time; an apply that arrives during a run schedules
// another pass so that snapshot is not left in place.
func scheduleUsageSpikeResume(ctx context.Context, bank *sqlite.TokenBankRepo, svc *llmservice.Service) {
	if bank == nil || svc == nil {
		return
	}
	usageSpikeResumeMu.Lock()
	usageSpikeResumeWaiting = true
	if usageSpikeResumeRunning {
		usageSpikeResumeMu.Unlock()
		return
	}
	usageSpikeResumeRunning = true
	usageSpikeResumeMu.Unlock()
	go func() {
		for {
			usageSpikeResumeMu.Lock()
			if !usageSpikeResumeWaiting {
				usageSpikeResumeRunning = false
				usageSpikeResumeMu.Unlock()
				return
			}
			usageSpikeResumeWaiting = false
			usageSpikeResumeMu.Unlock()
			resumeSharesPausedForUsageSpike(ctx, bank, svc)
		}
	}()
}

var (
	usageSpikeResumeMu      sync.Mutex
	usageSpikeResumeRunning bool
	usageSpikeResumeWaiting bool
)

// currentSettings reads the platform Token Bank settings blob. Settlement must
// agree with what the admin tab shows; both read the same blob through this
// helper. A missing or corrupt blob yields the documented defaults, because a
// zero fee rate would silently give the platform's cut to the sharer.
func (s *tokenBankProxySettler) currentSettings(ctx context.Context) sqlite.TokenBankSettings {
	defaults := sqlite.DefaultTokenBankSettings()
	if s == nil || s.settings == nil {
		return defaults
	}
	raw, err := s.settings.Get(ctx, sqlite.TokenBankSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return defaults
	}
	return sqlite.ParseTokenBankSettings(raw)
}
