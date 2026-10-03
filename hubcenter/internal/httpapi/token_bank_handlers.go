package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// Token Bank client API (§6.1). Every handler here resolves the acting user
// from the SkillMarket session and never from the request body: these
// endpoints move money, and a caller-controlled user id would let anybody
// withdraw somebody else's credits.

const (
	tokenBankDefaultListLimit = 100
	tokenBankMaxListLimit     = 500
	// Unfinished gift grants only. This is not the history page size: a page of
	// ordinary withdrawals must not be able to hide a gift debit that still
	// needs its original request id.
	tokenBankUnboundGiftLimit = 100
)

// tokenBankRepoView is the slice of the ledger repository these handlers need.
// Declaring it as an interface keeps the handler testable without a live
// SQLite file, and it documents exactly which store methods the HTTP surface
// depends on.
type tokenBankRepoView interface {
	Balance(ctx context.Context, userID string) (sqlite.TokenBankBalance, error)
	AvailableMicro(ctx context.Context, userID string) (int64, error)
	Withdraw(ctx context.Context, req sqlite.TokenBankWithdrawRequest) (*sqlite.TokenBankWithdrawal, bool, error)
	ListWithdrawals(ctx context.Context, userID, hubID string, limit int) ([]sqlite.TokenBankWithdrawal, error)
	CreateGiftLink(ctx context.Context, link sqlite.TokenBankGiftLink, policy sqlite.GiftLinkPolicy, now time.Time) (*sqlite.TokenBankGiftLink, error)
	ClaimGiftLink(ctx context.Context, code, claimerUserID, claimerEmail string, now time.Time) (*sqlite.TokenBankGiftLink, error)
	RevokeGiftLink(ctx context.Context, linkID, senderUserID string, now time.Time) error
	ExpireGiftLinks(ctx context.Context, now time.Time) (int, error)
	ListGiftLinks(ctx context.Context, senderUserID, status string, limit int) ([]sqlite.TokenBankGiftLink, error)
	GiftLinkByCode(ctx context.Context, code string) (*sqlite.TokenBankGiftLink, error)
	CountGiftLinksSince(ctx context.Context, senderUserID string, since time.Time) (int, error)
	CountUserHubs(ctx context.Context, userID string) (int, error)
	UsageDaily(ctx context.Context, ownerID string, days int) ([]sqlite.TokenBankUsageDay, []sqlite.TokenBankUsageModel, error)
	UsageExport(ctx context.Context, ownerID string, days int) ([]sqlite.TokenBankUsageExportRow, error)
}

// --- GET /api/v1/token-bank/summary ---

// TokenBankSummary is the "my credits" card. All values are microcredits so the
// client can render them at whatever precision it wants; the GUI divides by
// 1e6. Available excludes frozen on purpose (C1): showing a number the user
// cannot actually withdraw is the fastest way to a support ticket.
type TokenBankSummary struct {
	EarnedMicro            int64 `json:"earned_micro"`
	ReceivedMicro          int64 `json:"received_micro"`
	WithdrawnMicro         int64 `json:"withdrawn_micro"`
	GrantedMicro           int64 `json:"granted_micro"`
	FrozenMicro            int64 `json:"frozen_micro"`
	AvailableMicro         int64 `json:"available_micro"`
	GiftShareCapMicro      int64 `json:"gift_share_cap_micro"`
	HubCount               int   `json:"hub_count"`
	AutoWithdrawLimitMicro int64 `json:"auto_withdraw_limit_micro"`
	GiftSharePercent       int   `json:"gift_share_percent"`
	GiftLinkTTLSeconds     int64 `json:"gift_link_ttl_seconds"`
	// MinShareCredits and ShareCreatesToday let the GUI disable the share button
	// with a reason ("below the 1 credit floor", "you have used today's 10")
	// instead of letting the user submit and collect a 400. Both are advisory:
	// the create endpoint re-checks them under a transaction.
	MinShareCredits   float64 `json:"min_share_credits"`
	ShareCreatesToday int     `json:"share_creates_today"`
	// Rank is 1-based among sharers with settled usage. 0 means none yet.
	Rank          int    `json:"rank"`
	RankBadge     string `json:"rank_badge"`
	LifetimeBadge string `json:"lifetime_badge"`
}

func (h *SkillMarketHandlers) TokenBankSummary(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	balance, err := repo.Balance(r.Context(), user.ID)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	hubCount, err := repo.CountUserHubs(r.Context(), user.ID)
	if err != nil {
		// The hub count only shapes the automatic-withdrawal hint. Failing the
		// whole card because the hub inventory is unreadable would hide the
		// balance the user actually came for.
		hubCount = 0
	}
	available := balance.AvailableMicro()
	// The summary must advertise the *same* cap the create endpoint enforces.
	// Reporting a hardcoded 50% while the admin had configured 25% would let the
	// GUI block a legal amount or, worse, offer one the server then refuses.
	settings, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		// Same reasoning as the hub count: a settings read failure must not hide
		// the balance. Falling back to the documented defaults is exactly what
		// the create path would do with an unreadable blob.
		settings = sqlite.DefaultTokenBankSettings()
	}
	policy := giftLinkPolicyFromSettings(settings)
	summary := TokenBankSummary{
		EarnedMicro:            balance.EarnedMicro,
		ReceivedMicro:          balance.ReceivedMicro,
		WithdrawnMicro:         balance.WithdrawnMicro,
		GrantedMicro:           balance.GrantedMicro,
		FrozenMicro:            balance.FrozenMicro,
		AvailableMicro:         available,
		GiftShareCapMicro:      sqlite.GiftShareCapMicro(available, policy),
		HubCount:               hubCount,
		AutoWithdrawLimitMicro: sqlite.AutoWithdrawLimitMicro(available, hubCount),
		GiftSharePercent:       giftSharePercent(settings.CreditShareMaxRatio),
		GiftLinkTTLSeconds:     int64(giftLinkTTL(settings) / time.Second),
		MinShareCredits:        settings.CreditShareMinCredits,
		ShareCreatesToday:      shareCreatesToday(r.Context(), repo, user.ID),
	}
	if p2, ok := h.tokenBank.(tokenBankP2Store); ok && p2 != nil {
		earned, rank, standErr := p2.SharerStanding(r.Context(), user.ID)
		if standErr == nil {
			summary.Rank = rank
			summary.RankBadge, summary.LifetimeBadge = sqlite.TokenBankBadges(rank, earned)
		}
	}
	writeJSON(w, http.StatusOK, summary)
}

// giftSharePercent renders a ratio as the whole-number percentage the summary
// has always reported (0.5 -> 50). It rounds down so the number shown is never
// larger than the cap actually applied.
func giftSharePercent(ratio float64) int {
	if ratio <= 0 {
		return sqlite.TokenBankGiftSharePercent
	}
	return int(ratio * 100)
}

// shareCreatesToday counts links made since UTC midnight for the daily-limit
// hint. A failure here is reported as 0 rather than failing the card: the count
// is advisory, and the create endpoint enforces the limit authoritatively.
func shareCreatesToday(ctx context.Context, repo tokenBankRepoView, userID string) int {
	n, err := repo.CountGiftLinksSince(ctx, userID, sqlite.StartOfUTCDay(time.Now().UTC()))
	if err != nil {
		return 0
	}
	return n
}

// --- POST /api/v1/token-bank/credits/withdraw ---

type tokenBankWithdrawRequest struct {
	RequestID string `json:"request_id"`
	// AmountMicro is microcredits. Zero (or absent) means "as much as this
	// caller is allowed", which is what an automatic low-balance top-up wants.
	// amount (whole credits) is accepted as a convenience for scripted callers,
	// but the two must not both be sent: a caller that sends both is confused
	// about the unit and would silently get 30 instead of 30,000,000.
	AmountMicro int64  `json:"amount_micro"`
	Amount      *int64 `json:"amount"`
	HubID       string `json:"hub_id"`
	Kind        string `json:"kind"`
	LinkID      string `json:"link_id"`
	// Manual distinguishes a person pressing "withdraw" (may take the whole
	// available balance) from an automatic top-up (capped at 1/N so one machine
	// cannot drain a user who owns several hubs, E6).
	//
	// It defaults to false — the capped mode — because the default has to be
	// the safe one: an old client that never heard of this field must not be
	// able to take everything, and HubCenter cannot tell a person from a cron
	// job by looking at the request.
	Manual bool `json:"manual"`
}

func (h *SkillMarketHandlers) TokenBankWithdrawCredits(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	// Before the body is decoded: a withdrawal has to be decided on the
	// clearing node, or two nodes can each authorise the same credits against
	// their own stale copy of the ledger.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	var req tokenBankWithdrawRequest
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
		return
	}
	if req.Amount != nil && req.AmountMicro != 0 {
		tbError(w, http.StatusBadRequest, "invalid_amount", "send either amount_micro or amount, not both")
		return
	}
	amountMicro := req.AmountMicro
	if req.Amount != nil {
		converted, ok := creditsToMicro(*req.Amount)
		if !ok {
			tbError(w, http.StatusBadRequest, "invalid_amount", "amount must be positive and within range")
			return
		}
		amountMicro = converted
	}
	if amountMicro < 0 {
		// Zero is meaningful: it asks for the whole allowed allowance and is the
		// automatic top-up's normal request. Only a negative figure is nonsense.
		tbError(w, http.StatusBadRequest, "invalid_amount", "amount_micro must not be negative")
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		// Without an idempotency key a hub that retries after a crash is
		// debited twice. Refusing is the only safe answer: the caller has to
		// supply a value it can reproduce.
		tbError(w, http.StatusBadRequest, "missing_request_id", "request_id is required: it is the idempotency key for the debit")
		return
	}
	hubID := strings.TrimSpace(req.HubID)
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "self"
	}
	if kind != "self" && kind != "gift" {
		tbError(w, http.StatusBadRequest, "invalid_kind", "kind must be self or gift")
		return
	}
	if kind == "gift" && strings.TrimSpace(req.LinkID) == "" {
		tbError(w, http.StatusBadRequest, "missing_link_id", "link_id is required when kind is gift")
		return
	}
	// A manual withdrawal that names a hub is the authorization for "提取到本机":
	// the hub pull cannot set Manual (R7), so it replays this row and only then
	// writes the grant. Refuse before gift settlement and before the debit when
	// that hub is not linked. Otherwise the credits leave the account and the
	// pull stays 403, with no grant and no way to replay onto another hub.
	// An empty hub id is unchanged: it does not promise a grant to a machine.
	if req.Manual && hubID != "" && !h.tokenBankHubMayActFor(w, r, hubID, user.ID) {
		return
	}
	if kind == "gift" && writeTokenBankGiftSettleError(w, h.settleGiftForWithdraw(r.Context(), user.ID, strings.TrimSpace(req.LinkID), time.Now().UTC())) {
		return
	}

	withdrawal, created, err := repo.Withdraw(r.Context(), sqlite.TokenBankWithdrawRequest{
		RequestID: requestID,
		UserID:    user.ID,
		HubID:     hubID,
		// Zero means "as much as allowed": for an automatic top-up that is 1/N of
		// the remainder, for a manual one it is the whole available balance. A
		// caller that names a figure gets exactly that figure, and is refused
		// rather than truncated if it exceeds what the mode allows.
		AmountMicro: amountMicro,
		Manual:      req.Manual,
		Kind:        kind,
		LinkID:      strings.TrimSpace(req.LinkID),
		Now:         time.Now().UTC(),
	})
	if err != nil {
		writeTokenBankWithdrawError(w, err)
		return
	}
	// Replay and a fresh debit return the same body apart from `created`.
	// The same account and hub use that to tell a retry from the first debit.
	// Another account or hub is refused and does not receive this amount.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"created":       created,
		"withdrawal_id": withdrawal.ID,
		"request_id":    withdrawal.RequestID,
		"amount_micro":  withdrawal.AmountMicro,
		"hub_id":        withdrawal.HubID,
		"kind":          withdrawal.Kind,
		"link_id":       withdrawal.LinkID,
		"grant_id":      withdrawal.GrantID,
		"state":         withdrawal.Status,
		"created_at":    withdrawal.Created.UTC().Format(time.RFC3339),
	})
}

// --- GET /api/v1/token-bank/credits/withdrawals ---

func (h *SkillMarketHandlers) TokenBankListWithdrawals(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	// Withdrawal rows are only written on the clearing node and are not
	// replicated, so reading them anywhere else returns an empty history.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	limit := tokenBankListLimit(r)
	hubID := strings.TrimSpace(r.URL.Query().Get("hub_id"))
	items, err := repo.ListWithdrawals(r.Context(), user.ID, hubID, limit)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	// The page above is newest-first. An older gift debit with an empty grant
	// still has to be on this response: the desktop finishes it by replaying
	// that request id, and a freshly minted id is refused without a grant.
	if lister, ok := repo.(tokenBankUnboundGiftWithdrawalLister); ok {
		pending, err := lister.ListUnboundGiftWithdrawals(r.Context(), user.ID, hubID, tokenBankUnboundGiftLimit)
		if err != nil {
			tbError(w, http.StatusInternalServerError, "list_failed", err.Error())
			return
		}
		items = appendUnboundGiftWithdrawals(items, pending)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":           item.ID,
			"request_id":   item.RequestID,
			"hub_id":       item.HubID,
			"amount_micro": item.AmountMicro,
			"grant_id":     item.GrantID,
			"kind":         item.Kind,
			"link_id":      item.LinkID,
			"state":        item.Status,
			"created_at":   item.Created.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"withdrawals": out})
}

// --- POST /api/v1/credits/share-links ---

type giftLinkCreateRequest struct {
	// Credits is whole credits. Gift links are created by a human tapping a
	// button, so the honest unit is the one on screen; the handler converts.
	Credits *int64 `json:"credits"`
	// CreditsMicro is the precise form, for callers that already work in micro.
	CreditsMicro int64 `json:"credits_micro"`
}

// microCreditsPerCredit is the fixed-point scale. Every credit figure crossing
// this boundary is converted with an explicit overflow check: the multiplication
// is performed on a user-supplied int64, and a silent wrap would turn "share a
// huge amount" into "share a negative one" or a wildly wrong positive one.
const microCreditsPerCredit = 1_000_000

// creditsToMicro converts whole credits to microcredits, refusing anything that
// would not fit in an int64.
func creditsToMicro(credits int64) (int64, bool) {
	if credits <= 0 {
		return 0, false
	}
	const maxCredits = math.MaxInt64 / microCreditsPerCredit
	if credits > maxCredits {
		return 0, false
	}
	return credits * microCreditsPerCredit, true
}

func (h *SkillMarketHandlers) TokenBankCreateGiftLink(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	// Before the body is decoded: the freeze this creates is not replicated, so
	// it has to be written where the settlement will later look for it.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	var req giftLinkCreateRequest
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
		return
	}
	// Same rule as the withdrawal endpoint: sending both units is a caller bug,
	// and silently preferring one of them hands the user a different amount than
	// they wrote.
	if req.Credits != nil && req.CreditsMicro != 0 {
		tbError(w, http.StatusBadRequest, "invalid_amount", "send either credits or credits_micro, not both")
		return
	}
	var amountMicro int64
	if req.Credits != nil {
		converted, ok := creditsToMicro(*req.Credits)
		if !ok {
			tbError(w, http.StatusBadRequest, "invalid_amount", "credits must be positive and within range")
			return
		}
		amountMicro = converted
	} else {
		amountMicro = req.CreditsMicro
	}
	if amountMicro <= 0 {
		tbError(w, http.StatusBadRequest, "invalid_amount", "credits_micro must be positive")
		return
	}

	// The anti-abuse limits (§5) are admin-configurable and previously only
	// stored. Loading them here is what makes `credit_share_daily_limit`,
	// `credit_share_min_credits` and `credit_share_link_ttl_hours` real
	// settings rather than decorative ones.
	settings, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}

	code, err := newGiftLinkCode()
	if err != nil {
		tbError(w, http.StatusInternalServerError, "code_generation_failed", err.Error())
		return
	}
	now := time.Now().UTC()
	link, err := repo.CreateGiftLink(r.Context(), sqlite.TokenBankGiftLink{
		ID:           "gift_" + code,
		Code:         code,
		SenderUserID: user.ID,
		SenderEmail:  user.Email,
		CreditsMicro: amountMicro,
		// The link is pinned to this node because the claim is a conditional
		// UPDATE here. Two nodes each running it locally would both win and then
		// overwrite each other during HA sync, paying the credits out twice.
		OriginNodeID: h.tokenBankNodeID(),
		ExpiresAt:    now.Add(giftLinkTTL(settings)),
	}, giftLinkPolicyFromSettings(settings), now)
	if err != nil {
		switch {
		case errors.Is(err, sqlite.ErrGiftLinkNoBalance):
			tbError(w, http.StatusPaymentRequired, "insufficient_credits", err.Error())
		case errors.Is(err, sqlite.ErrGiftLinkOverCap):
			tbError(w, http.StatusPaymentRequired, "over_cap", err.Error())
		case errors.Is(err, sqlite.ErrGiftLinkBelowFloor):
			tbError(w, http.StatusBadRequest, "below_minimum", err.Error())
		case errors.Is(err, sqlite.ErrGiftLinkRateLimited):
			tbError(w, http.StatusTooManyRequests, "daily_limit_reached", err.Error())
		default:
			tbError(w, http.StatusInternalServerError, "create_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, giftLinkPayload(*link, giftLinkView{includeCode: true}))
}

// giftLinkTTL is the configured link lifetime, falling back to the built-in
// default when the stored blob predates the field or carries a zero.
func giftLinkTTL(settings sqlite.TokenBankSettings) time.Duration {
	hours := settings.CreditShareLinkTTLHours
	if hours <= 0 {
		return sqlite.TokenBankGiftLinkTTL
	}
	return time.Duration(hours) * time.Hour
}

// giftLinkPolicyFromSettings projects the settings blob onto the store-layer
// policy value.
//
// The two types are kept separate on purpose: the store must be usable (and
// testable) without a settings table, and a clamp added to the admin form must
// not silently change what the store enforces. `CreditShareMinCredits` is
// credits while the store speaks microcredits, so the conversion happens here,
// at the one boundary that knows both units.
func giftLinkPolicyFromSettings(settings sqlite.TokenBankSettings) sqlite.GiftLinkPolicy {
	policy := sqlite.GiftLinkPolicy{
		MaxRatio:   settings.CreditShareMaxRatio,
		DailyLimit: settings.CreditShareDailyLimit,
		TTL:        giftLinkTTL(settings),
	}
	if settings.CreditShareMinCredits > 0 {
		policy.MinMicro = int64(settings.CreditShareMinCredits * float64(sqlite.TokenBankMicrocreditsPerCredit))
	}
	return policy
}

// --- GET /api/v1/credits/share-links ---

func (h *SkillMarketHandlers) TokenBankListGiftLinks(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	// A link lives only on the node that created it, which is the clearing
	// node. Listing locally would show the user an empty page right after
	// they created one.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	limit := tokenBankListLimit(r)
	links, err := repo.ListGiftLinks(r.Context(), user.ID, strings.TrimSpace(r.URL.Query().Get("status")), limit)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(links))
	for _, link := range links {
		out = append(out, giftLinkPayload(link, giftLinkView{revealClaimer: true}))
	}
	// claimed_links is how the receiver finishes a gift after leaving the page
	// that claimed it. Without it the only control was in-memory, so a later
	// visit saw "already claimed" while the sender stayed frozen.
	claimedOut := []map[string]any{}
	if lister, ok := repo.(tokenBankClaimedGiftLister); ok {
		claimed, err := lister.ListClaimedGiftLinks(r.Context(), user.ID, time.Now().UTC(), limit)
		if err != nil {
			tbError(w, http.StatusInternalServerError, "list_failed", err.Error())
			return
		}
		for _, link := range claimed {
			item := giftLinkPayload(link, giftLinkView{})
			item["sender_masked"] = maskEmail(link.SenderEmail)
			claimedOut = append(claimedOut, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"share_links": out, "claimed_links": claimedOut})
}

// --- POST /api/v1/credits/share-links/{id}/revoke ---

func (h *SkillMarketHandlers) TokenBankRevokeGiftLink(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	// Revoking releases the freeze, so it has to happen where the link is.
	// Reached locally it would answer not-found and leave the credits frozen.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	linkID := strings.TrimSpace(r.PathValue("id"))
	if linkID == "" {
		tbError(w, http.StatusBadRequest, "missing_link_id", "link id is required")
		return
	}
	// RevokeGiftLink takes the sender id and reports ErrGiftLinkNotFound when it
	// does not match, so the ownership test lives in the store where it cannot
	// be forgotten by a future caller.
	if err := repo.RevokeGiftLink(r.Context(), linkID, user.ID, time.Now().UTC()); err != nil {
		switch {
		case errors.Is(err, sqlite.ErrGiftLinkNotFound):
			tbError(w, http.StatusNotFound, "not_found", "share link not found")
		case errors.Is(err, sqlite.ErrGiftLinkNotActive):
			tbError(w, http.StatusConflict, "not_active", "share link is no longer active")
		default:
			tbError(w, http.StatusInternalServerError, "revoke_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "id": linkID})
}

// --- GET /api/v1/credits/share-links/{code}/preview ---

// TokenBankPreviewGiftLink is deliberately public and deliberately thin. The
// sender is masked, the claimer is never named, and the response says nothing
// about the sender's balance: a stranger holding a leaked code learns only what
// they could read off the link itself.
func (h *SkillMarketHandlers) TokenBankPreviewGiftLink(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	code := strings.TrimSpace(r.PathValue("code"))
	if code == "" {
		tbError(w, http.StatusBadRequest, "missing_code", "share code is required")
		return
	}
	// The preview is the first thing a recipient sees. Read locally it 404s
	// on every node but the clearing one, which reads to the user as a dead
	// link — they never get as far as claiming.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	link, err := repo.GiftLinkByCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, sqlite.ErrGiftLinkNotFound) {
			tbError(w, http.StatusNotFound, "not_found", "share link not found")
			return
		}
		tbError(w, http.StatusInternalServerError, "preview_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, giftPreviewPayload(link, time.Now().UTC(), h.tokenBankOptionalUser(r)))
}

// --- POST /api/v1/credits/share-links/{code}/claim ---

func (h *SkillMarketHandlers) TokenBankClaimGiftLink(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	code := strings.TrimSpace(r.PathValue("code"))
	if code == "" {
		tbError(w, http.StatusBadRequest, "missing_code", "share code is required")
		return
	}
	// Claiming moves no money until the receiver withdraws, but it still needs a
	// verified account: gifting is exactly the shape an unverified-account farm
	// would use to launder credits between throwaway logins.
	if !strings.EqualFold(strings.TrimSpace(user.Status), "verified") {
		tbError(w, http.StatusForbidden, "unverified_account", "verify your account before claiming shared credits")
		return
	}
	if h.claimMustProxy(w, r, repo, code) {
		return
	}
	link, err := repo.ClaimGiftLink(r.Context(), code, user.ID, user.Email, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, sqlite.ErrGiftLinkNotFound):
			tbError(w, http.StatusNotFound, "not_found", "share link not found")
		case errors.Is(err, sqlite.ErrGiftLinkOwnLink):
			tbError(w, http.StatusForbidden, "own_link", "you cannot claim your own share link")
		case errors.Is(err, sqlite.ErrGiftLinkExpired):
			tbError(w, http.StatusGone, "expired", "share link has expired")
		case errors.Is(err, sqlite.ErrGiftLinkNotActive):
			// The same account claiming again is not a conflict. The withdraw
			// button lived only on the page that first claimed, so a retry has
			// to hand that link back or the sender stays frozen at "未提取".
			if h.writeOwnGiftClaimResume(w, r, repo, code, user.ID) {
				return
			}
			tbError(w, http.StatusConflict, "already_claimed", "share link has already been claimed")
		default:
			tbError(w, http.StatusInternalServerError, "claim_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, giftLinkPayload(*link, giftLinkView{includeCode: true}))
}

// --- helpers ---

// SetTokenBankRepo wires the Token Bank repository after construction. It is a
// setter rather than a config field because the repository is created by the
// LLM module, which is initialised after this handler set (SkillMarket must
// survive an LLM persistence failure, so it cannot depend on it).
//
// Calling it again replaces the previous repository, which is what a test wants
// and a running deployment never does.
//
// # Why the parameter is the narrow view
//
// It documents exactly which store methods the client API needs, and it lets a
// test pass a fake. The cost is a runtime type assert in tokenBankAdminRepo():
// the§6.2 surface needs a strictly larger method set, so a value that satisfies
// only tokenBankRepoView is accepted here and then silently 503s every admin
// endpoint. To keep that from being a silent wiring bug, the concrete
// production repository is asserted below so the failure shows up at startup
// rather than as a mystery 503 on the admin tab.
func (h *SkillMarketHandlers) SetTokenBankRepo(repo tokenBankRepoView, nodeID string) {
	if h == nil {
		return
	}
	h.tokenBank = repo
	h.nodeID = strings.TrimSpace(nodeID)
	if repo == nil {
		return
	}
	if _, ok := repo.(tokenBankAdminRepoView); !ok {
		// Not fatal: the client API still works, and a deployment may genuinely
		// run with a client-only repository. Logged so an operator can tell
		// "Token Bank admin is unavailable on purpose" from "someone changed
		// the wrapper and broke it".
		log.Printf("[token-bank] repository does not implement the admin view; §6.2 endpoints will report 503")
	}
}

func (h *SkillMarketHandlers) tokenBankRepo() tokenBankRepoView {
	if h == nil || h.tokenBank == nil {
		return nil
	}
	return h.tokenBank
}

// SetTokenBankPublisher wires the registry half of Token Bank (§3.2). It is a
// separate setter from SetTokenBankRepo because the two live in different
// modules: the ledger repository is created by the SQLite provider, while the
// registry publisher is the LLM service. A node that initialises the store but
// not the LLM module keeps working and reports 503 on the share endpoints.
func (h *SkillMarketHandlers) SetTokenBankPublisher(publisher tokenBankPublishView) {
	if h == nil {
		return
	}
	h.tokenBankLLM = publisher
	if publisher == nil {
		h.tokenBankGroups = nil
		return
	}
	if checker, ok := publisher.(tokenBankGroupChecker); ok {
		h.tokenBankGroups = checker
	}
}

func (h *SkillMarketHandlers) tokenBankNodeID() string {
	if h == nil {
		return ""
	}
	return strings.TrimSpace(h.nodeID)
}

// tokenBankSessionUser resolves the caller the same way the Credits wallet
// does. Separate from creditSessionUser only so the error codes stay in the
// token-bank vocabulary the client already handles.
// tokenBankClaimedGiftLister is optional. The client list still returns
// share_links when a test double does not implement it.
type tokenBankClaimedGiftLister interface {
	ListClaimedGiftLinks(ctx context.Context, claimerUserID string, now time.Time, limit int) ([]sqlite.TokenBankGiftLink, error)
}

// tokenBankUnboundGiftWithdrawalLister is optional. A test double that only
// implements the history list still returns that page.
type tokenBankUnboundGiftWithdrawalLister interface {
	ListUnboundGiftWithdrawals(ctx context.Context, userID, hubID string, limit int) ([]sqlite.TokenBankWithdrawal, error)
}

// appendUnboundGiftWithdrawals keeps a gift debit that the history page
// already dropped. Rows already on the page stay where they are.
func appendUnboundGiftWithdrawals(items, pending []sqlite.TokenBankWithdrawal) []sqlite.TokenBankWithdrawal {
	if len(pending) == 0 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.RequestID); id != "" {
			seen[id] = struct{}{}
		}
	}
	for _, item := range pending {
		id := strings.TrimSpace(item.RequestID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		items = append(items, item)
	}
	return items
}

// tokenBankOptionalUser reads a session when one was sent. A missing or bad
// token is not an error: the gift preview stays public either way.
func (h *SkillMarketHandlers) tokenBankOptionalUser(r *http.Request) *skillmarket.SkillMarketUser {
	if h == nil || h.authSvc == nil || r == nil {
		return nil
	}
	token := strings.TrimSpace(extractSessionToken(r))
	if token == "" {
		return nil
	}
	user, err := h.authSvc.CurrentUser(r.Context(), token)
	if err != nil || user == nil {
		return nil
	}
	return user
}

// giftPreviewPayload is the public preview plus, only for the account that
// already claimed it, the fields that let that account withdraw. Strangers
// still learn nothing but the masked sender, the amount, and the status.
func giftPreviewPayload(link *sqlite.TokenBankGiftLink, now time.Time, user *skillmarket.SkillMarketUser) map[string]any {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	open := link.ExpiresAt.IsZero() || link.ExpiresAt.After(now)
	out := map[string]any{
		"code":              link.Code,
		"sender_masked":     maskEmail(link.SenderEmail),
		"credits_micro":     link.CreditsMicro,
		"status":            link.Status,
		"claimable":         link.Status == sqlite.TokenBankGiftStatusActive && open,
		"expires_at":        formatOptionalTime(link.ExpiresAt),
		"remaining_seconds": remainingSeconds(link.ExpiresAt, now),
	}
	if user == nil || strings.TrimSpace(user.ID) == "" || user.ID != link.ClaimedByUser {
		return out
	}
	switch link.Status {
	case sqlite.TokenBankGiftStatusClaimed:
		// Still claimed after the return time means the sweeper has not
		// returned the credits. The claimer can withdraw until that happens.
		out["id"] = link.ID
		out["withdrawable"] = true
	case sqlite.TokenBankGiftStatusSettled:
		out["withdrawn"] = true
	}
	return out
}

// writeOwnGiftClaimResume answers a repeat claim from the account that already
// holds the link. It returns true when it has written the response. A settled
// link is not resumed: settling again is a no-op, and a new withdrawal would
// debit the rest of that account's balance. A revoked link is answered for
// every caller, because "already claimed" would send them back to withdraw.
func (h *SkillMarketHandlers) writeOwnGiftClaimResume(w http.ResponseWriter, r *http.Request, repo tokenBankRepoView, code, userID string) bool {
	current, err := repo.GiftLinkByCode(r.Context(), code)
	if err != nil {
		return false
	}
	// Revoke is allowed after a claim. A retry must not say "already claimed":
	// that is the resume path, and it would offer a withdraw that cannot succeed.
	if current.Status == sqlite.TokenBankGiftStatusRevoked {
		tbError(w, http.StatusConflict, "gift_revoked", "the sender revoked this gift")
		return true
	}
	if current.ClaimedByUser != userID {
		return false
	}
	switch current.Status {
	case sqlite.TokenBankGiftStatusClaimed:
		payload := giftLinkPayload(*current, giftLinkView{includeCode: true})
		payload["resume"] = true
		writeJSON(w, http.StatusOK, payload)
		return true
	case sqlite.TokenBankGiftStatusSettled:
		tbError(w, http.StatusConflict, "already_withdrawn", "you already withdrew this gift to your machine")
		return true
	default:
		return false
	}
}

func (h *SkillMarketHandlers) tokenBankSessionUser(w http.ResponseWriter, r *http.Request) (*skillmarket.SkillMarketUser, bool) {
	if h == nil || h.authSvc == nil {
		tbError(w, http.StatusServiceUnavailable, "auth_unavailable", "token bank authentication unavailable")
		return nil, false
	}
	user, err := h.authSvc.CurrentUser(r.Context(), extractSessionToken(r))
	if err != nil {
		tbError(w, http.StatusUnauthorized, "unauthorized", "session expired or invalid")
		return nil, false
	}
	return user, true
}

type tokenBankAudienceCatalog interface {
	ListShareAudiences(ctx context.Context, userID string) (sqlite.TokenBankShareAudiences, error)
}

// TokenBankListShareAudiences returns the hubs and tenants the signed-in
// account may grant on a private share.
//
// GET /api/v1/token-bank/audiences
func (h *SkillMarketHandlers) TokenBankListShareAudiences(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	catalog, ok := h.tokenBank.(tokenBankAudienceCatalog)
	if !ok || catalog == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	items, err := catalog.ListShareAudiences(r.Context(), user.ID)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// giftLinkView chooses what one client response may reveal. The two switches
// are named so a call cannot swap "show the code" with "show who claimed".
type giftLinkView struct {
	includeCode   bool
	revealClaimer bool
}

// giftLinkPayload is the client view of one share link.
//
// revealClaimer is only for the sender's own list. That list is where they see
// who claimed a link. Preview and the claim response keep the mask so holding
// a code is not an address book. status "claimed" means the credits are still
// frozen; "settled" means the receiver has withdrawn them.
func giftLinkPayload(link sqlite.TokenBankGiftLink, view giftLinkView) map[string]any {
	email := maskEmail(link.ClaimedByEmail)
	if view.revealClaimer {
		email = strings.TrimSpace(link.ClaimedByEmail)
	}
	out := map[string]any{
		"id":               link.ID,
		"credits_micro":    link.CreditsMicro,
		"status":           link.Status,
		"created_at":       formatOptionalTime(link.CreatedAt),
		"expires_at":       formatOptionalTime(link.ExpiresAt),
		"claimed_at":       formatOptionalTime(link.ClaimedAt),
		"revoked_at":       formatOptionalTime(link.RevokedAt),
		"claimed":          link.ClaimedByUser != "",
		"claimed_by_email": email,
	}
	if view.revealClaimer {
		out["claimed_by_user_id"] = link.ClaimedByUser
	}
	if view.includeCode {
		out["code"] = link.Code
	}
	return out
}

func tokenBankListLimit(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return tokenBankDefaultListLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return tokenBankDefaultListLimit
	}
	if n > tokenBankMaxListLimit {
		return tokenBankMaxListLimit
	}
	return n
}

// giftLinkCodeEncoding is RFC 4648 base32 without padding. The standard
// alphabet is A-Z plus 2-7, which already excludes the 0/O and 1/I pairs that
// make a code read aloud over the phone ambiguous — so there is nothing to gain
// from a custom alphabet, and a custom one is a silent source of collisions if
// it is not exactly 32 symbols. Base32 requires a 32-byte alphabet and panics
// in init() otherwise.
var giftLinkCodeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newGiftLinkCode mints a 10-character share code (50 bits). The store treats a
// duplicate id as an insert conflict rather than a lookup, so guessing the code
// space is not a path to somebody else's credits, but 50 bits still puts the
// birthday bound far beyond any realistic grant count.
func newGiftLinkCode() (string, error) {
	// 7 bytes round up to 12 base32 symbols; take the first 10 (50 bits).
	var buf [7]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	encoded := giftLinkCodeEncoding.EncodeToString(buf[:])
	if len(encoded) < 10 {
		return "", errors.New("gift link code encoding produced a short value")
	}
	return encoded[:10], nil
}

// maskEmail keeps the first character and the domain so a claimer can recognise
// "yes, that is the person who sent me this" without the preview endpoint
// becoming an email harvester.
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		if len(email) <= 1 {
			return "*"
		}
		return email[:1] + "***"
	}
	local, domain := email[:at], email[at+1:]
	if len(local) <= 1 {
		return "*@" + domain
	}
	return local[:1] + "***@" + domain
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func remainingSeconds(expiresAt, now time.Time) int64 {
	if expiresAt.IsZero() {
		return 0
	}
	d := expiresAt.Sub(now)
	if d <= 0 {
		return 0
	}
	return int64(d / time.Second)
}

func tbError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "code": code, "message": message})
}
