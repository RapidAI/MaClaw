package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

const (
	// tokenBankPeerHopHeader and tokenBankPeerSecretHeader authenticate a
	// one-hop proxy between hubcenter nodes. Every token bank operation that
	// has to be decided on one particular node uses them — a gift claim on the
	// link's origin node, a withdrawal on the clearing node — because they tell
	// the receiving node "routing already happened, execute here".
	//
	// The wire values keep the older "Claim" wording on purpose. A three-node
	// cluster is upgraded one node at a time, so for a while nodes running
	// different builds have to interoperate: a renamed header would be silently
	// ignored by an older node, which would then execute locally and defeat the
	// routing it was supposed to honour.
	tokenBankPeerHopHeader     = "X-Token-Bank-Claim-Hop"
	tokenBankPeerSecretHeader  = "X-Token-Bank-Claim-Peer"
	tokenBankClaimRetryMessage = "链接暂时无法领取，请稍后重试"
	// tokenBankWithdrawRetryMessage is the withdrawal counterpart: a pull that
	// cannot reach the node that must decide it is refused, not delegated.
	// Losing a top-up is recoverable; over-issuing credits is not.
	tokenBankWithdrawRetryMessage = "提取暂时无法处理，请稍后重试"
)

// tokenBankOriginDirectory is the HA view a claim needs. AccessPeer answers
// where the link's origin node is and whether it answered the last probe.
// ClusterSecret authenticates the one-hop proxy so a client cannot set the hop
// header and force this replica to claim locally.
type tokenBankOriginDirectory interface {
	AccessPeer(nodeID string) (internalURL string, reachable bool, rttMs int64)
	ClusterSecret() string
}

func (h *SkillMarketHandlers) SetTokenBankOrigin(dir tokenBankOriginDirectory) {
	if h == nil {
		return
	}
	h.tokenBankOrigin = dir
}

// claimMustProxy reports whether this request has to be executed on another
// node. A trusted hop means the origin already received it and must run the
// conditional UPDATE here. An empty origin, or this node's own id, is local.
// The bool is true when the response has already been written.
func (h *SkillMarketHandlers) claimMustProxy(w http.ResponseWriter, r *http.Request, repo tokenBankRepoView, code string) bool {
	if h.tokenBankHopTrusted(r) {
		return false
	}
	link, err := repo.GiftLinkByCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, sqlite.ErrGiftLinkNotFound) {
			tbError(w, http.StatusNotFound, "not_found", "share link not found")
			return true
		}
		tbError(w, http.StatusInternalServerError, "claim_failed", err.Error())
		return true
	}
	origin := strings.TrimSpace(link.OriginNodeID)
	if origin == "" || strings.EqualFold(origin, h.tokenBankNodeID()) {
		return false
	}
	dir := h.tokenBankOrigin
	if dir == nil {
		tbError(w, http.StatusServiceUnavailable, "origin_unreachable", tokenBankClaimRetryMessage)
		return true
	}
	base, reachable, _ := dir.AccessPeer(origin)
	if !reachable || strings.TrimSpace(base) == "" {
		tbError(w, http.StatusServiceUnavailable, "origin_unreachable", tokenBankClaimRetryMessage)
		return true
	}
	h.proxyGiftClaim(w, r, strings.TrimRight(strings.TrimSpace(base), "/"), code)
	return true
}

// tokenBankHopTrusted reports whether this request arrived from a peer node
// that already routed it, in which case it must be executed here.
//
// It is shared by every routed token bank operation. The secret check is what
// stops a client from setting the hop header itself and forcing a replica to
// decide locally — which is precisely the concurrency bug routing exists to
// prevent.
func (h *SkillMarketHandlers) tokenBankHopTrusted(r *http.Request) bool {
	if h == nil || h.tokenBankOrigin == nil || r == nil {
		return false
	}
	if strings.TrimSpace(r.Header.Get(tokenBankPeerHopHeader)) != "1" {
		return false
	}
	secret := h.tokenBankOrigin.ClusterSecret()
	if secret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.Header.Get(tokenBankPeerSecretHeader)), []byte(secret)) == 1
}

func (h *SkillMarketHandlers) proxyGiftClaim(w http.ResponseWriter, r *http.Request, base, code string) {
	target := base + "/api/v1/credits/share-links/" + url.PathEscape(code) + "/claim"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, nil)
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "origin_unreachable", tokenBankClaimRetryMessage)
		return
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set(tokenBankPeerHopHeader, "1")
	if h != nil && h.tokenBankOrigin != nil {
		if secret := h.tokenBankOrigin.ClusterSecret(); secret != "" {
			req.Header.Set(tokenBankPeerSecretHeader, secret)
		}
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "origin_unreachable", tokenBankClaimRetryMessage)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "origin_unreachable", tokenBankClaimRetryMessage)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

var (
	errTokenBankGiftUnavailable = errors.New("gift settlement is not available")
	errTokenBankGiftNotYours    = errors.New("gift link was not claimed by this account")
)

type tokenBankGiftSettler interface {
	GiftLinkByID(ctx context.Context, id string) (*sqlite.TokenBankGiftLink, error)
	SettleClaimedGift(ctx context.Context, linkID string, now time.Time) (bool, error)
}

// settleGiftForWithdraw moves a claimed gift before the receiver's withdrawal.
// Settle is idempotent, so a retry after a lost response settles nothing the
// second time and the withdrawal's own request id stays the debit key.
func (h *SkillMarketHandlers) settleGiftForWithdraw(ctx context.Context, userID, linkID string, now time.Time) error {
	if h == nil || h.tokenBank == nil {
		return errTokenBankGiftUnavailable
	}
	settler, ok := h.tokenBank.(tokenBankGiftSettler)
	if !ok {
		return errTokenBankGiftUnavailable
	}
	link, err := settler.GiftLinkByID(ctx, linkID)
	if err != nil {
		return err
	}
	switch link.Status {
	case sqlite.TokenBankGiftStatusClaimed, sqlite.TokenBankGiftStatusSettled:
	default:
		return sqlite.ErrGiftLinkNotClaimed
	}
	if link.ClaimedByUser != userID {
		return errTokenBankGiftNotYours
	}
	_, err = settler.SettleClaimedGift(ctx, link.ID, now)
	return err
}

func writeTokenBankGiftSettleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, errTokenBankGiftUnavailable):
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "gift settlement is not available on this node")
	case errors.Is(err, errTokenBankGiftNotYours):
		tbError(w, http.StatusForbidden, "gift_not_claimed_by_user", "this share link was not claimed by this account")
	case errors.Is(err, sqlite.ErrGiftLinkNotFound):
		tbError(w, http.StatusNotFound, "not_found", "share link not found")
	case errors.Is(err, sqlite.ErrGiftLinkNotClaimed):
		tbError(w, http.StatusConflict, "gift_not_claimed", "share link is not claimed")
	default:
		tbError(w, http.StatusInternalServerError, "gift_settle_failed", err.Error())
	}
	return true
}

type tokenBankGiftExpirer interface {
	ExpireGiftLinks(ctx context.Context, now time.Time) (int, error)
}

// RunTokenBankGiftExpiry releases freezes on a timer. One pass runs immediately
// so a restart does not wait a full interval to return expired credits.
func RunTokenBankGiftExpiry(ctx context.Context, repo tokenBankGiftExpirer, interval time.Duration) {
	if repo == nil || interval <= 0 {
		return
	}
	expire := func(now time.Time) {
		if _, err := repo.ExpireGiftLinks(ctx, now.UTC()); err != nil && ctx.Err() == nil {
			log.Printf("[token-bank] expire gift links: %v", err)
		}
	}
	expire(time.Now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			expire(now)
		}
	}
}
