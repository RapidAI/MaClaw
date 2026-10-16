package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// Hub-secret Token Bank pull. The desktop must not debit HubCenter itself:
// only the hub that will hold the grant is allowed to move the credits, and
// it has to name the service group the grant will fund.

type hubSecretVerifier interface {
	VerifyHubSecret(ctx context.Context, hubID, rawSecret string) error
}

// tokenBankHubLedger is the withdrawal slice the hub pull needs beyond the
// session view. The concrete repository implements it; a narrow fake does not,
// and the hub routes then report unavailable instead of panicking.
type tokenBankHubLedger interface {
	Withdraw(ctx context.Context, req sqlite.TokenBankWithdrawRequest) (*sqlite.TokenBankWithdrawal, bool, error)
	BindGrantID(ctx context.Context, requestID, hubID, grantID string) error
	ReissueGrantID(ctx context.Context, requestID, hubID, grantID string) error
	SumWithdrawalMicro(ctx context.Context, userID, hubID string) (int64, error)
}

// tokenBankHubMember is the account/hub binding the pull must check before it
// moves credits. It is the same hub_user_links join that sizes the 1/N cap.
type tokenBankHubMember interface {
	HubBelongsToUser(ctx context.Context, hubID, userID string) (bool, error)
}

// tokenBankGroupChecker is optional on the publisher. Share tests that stub
// only the publish methods leave it nil, and the hub pull then reports that
// the group cannot be validated instead of debiting blindly.
type tokenBankGroupChecker interface {
	ServiceGroupHostsTokenBank(ctx context.Context, groupID string) (bool, error)
}

// tokenBankHubWithdrawBody is what a hub sends when it pulls credits.
//
// There is deliberately no `manual` field. Manual is the flag that lifts the
// 1/N cap (E6) and lets one request take the whole balance, so it may only ever
// come from an authenticated user session — the user endpoint carries it, this
// one does not. A hub is self-hosted and speaks for itself: accepting the flag
// here would let any hub opt out of the cap by simply setting it, which is the
// same as not having the cap. An older hub that still sends the field is
// ignored rather than rejected, and gets the automatic 1/N allowance.
type tokenBankHubWithdrawBody struct {
	Email          string `json:"email"`
	RequestID      string `json:"request_id"`
	ServiceGroupID string `json:"service_group_id"`
	AmountMicro    int64  `json:"amount_micro"`
	Kind           string `json:"kind"`
	LinkID         string `json:"link_id"`
}

type tokenBankHubGrantBody struct {
	RequestID string `json:"request_id"`
	GrantID   string `json:"grant_id"`
	Reissue   bool   `json:"reissue"`
}

type tokenBankHubReconcileBody struct {
	Email string `json:"email"`
}

func (h *SkillMarketHandlers) TokenBankHubWithdraw(auth hubSecretVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID, ok := h.tokenBankHubCaller(w, r, auth)
		if !ok {
			return
		}
		repo := h.tokenBankHubLedger()
		if repo == nil {
			tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
			return
		}
		// After authentication and before the body is decoded: the peer
		// re-authenticates the forwarded request, so the hub's credentials have
		// to be read here and travel with it. A hub that pulls through a replica
		// must still be decided by the clearing node — that is the node whose
		// ledger view is the one that counts.
		if h.tokenBankRouteToClearing(w, r) {
			return
		}
		var req tokenBankHubWithdrawBody
		if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
			return
		}
		groupID := strings.TrimSpace(req.ServiceGroupID)
		requestID := strings.TrimSpace(req.RequestID)
		if requestID == "" {
			tbError(w, http.StatusBadRequest, "missing_request_id", "request_id is required: it is the idempotency key for the debit")
			return
		}
		if groupID == "" {
			tbError(w, http.StatusBadRequest, "missing_service_group", "service_group_id is required")
			return
		}
		if req.AmountMicro < 0 {
			tbError(w, http.StatusBadRequest, "invalid_amount", "amount_micro must not be negative")
			return
		}
		user, ok := h.tokenBankUserByEmail(w, r, req.Email)
		if !ok {
			return
		}
		if !h.tokenBankHubMayActFor(w, r, hubID, user.ID) {
			return
		}
		checker := h.tokenBankGroupChecker()
		if checker == nil {
			tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank service groups are not available on this node")
			return
		}
		hosts, err := checker.ServiceGroupHostsTokenBank(r.Context(), groupID)
		if err != nil {
			tbError(w, http.StatusInternalServerError, "service_group_lookup_failed", err.Error())
			return
		}
		if !hosts {
			tbError(w, http.StatusBadRequest, "service_group_not_token_bank", "service group does not include a token bank array or member")
			return
		}
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
		if kind == "gift" && writeTokenBankGiftSettleError(w, h.settleGiftForWithdraw(r.Context(), user.ID, strings.TrimSpace(req.LinkID), time.Now().UTC())) {
			return
		}
		withdrawal, created, err := repo.Withdraw(r.Context(), sqlite.TokenBankWithdrawRequest{
			RequestID:   requestID,
			UserID:      user.ID,
			HubID:       hubID,
			AmountMicro: req.AmountMicro,
			// Never req.Manual: see tokenBankHubWithdrawBody. This endpoint is
			// reached by a hub acting on its own, so every pull through it is
			// automatic and capped at 1/N of what is available.
			Manual: false,
			Kind:   kind,
			LinkID: strings.TrimSpace(req.LinkID),
			Now:    time.Now().UTC(),
		})
		if err != nil {
			writeTokenBankWithdrawError(w, err)
			return
		}
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
}

func (h *SkillMarketHandlers) TokenBankHubBindGrant(auth hubSecretVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID, ok := h.tokenBankHubCaller(w, r, auth)
		if !ok {
			return
		}
		repo := h.tokenBankHubLedger()
		if repo == nil {
			tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
			return
		}
		var req tokenBankHubGrantBody
		if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
			return
		}
		// The withdrawal row this writes to only exists on the clearing node.
		// Bound anywhere else it answers not-found and the grant id is lost for
		// good, which is exactly what makes a re-issue impossible later.
		if h.tokenBankRouteToClearing(w, r) {
			return
		}
		requestID := strings.TrimSpace(req.RequestID)
		grantID := strings.TrimSpace(req.GrantID)
		if requestID == "" || grantID == "" {
			tbError(w, http.StatusBadRequest, "missing_grant", "request_id and grant_id are required")
			return
		}
		var err error
		if req.Reissue {
			err = repo.ReissueGrantID(r.Context(), requestID, hubID, grantID)
		} else {
			err = repo.BindGrantID(r.Context(), requestID, hubID, grantID)
		}
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				tbError(w, http.StatusNotFound, "withdrawal_not_found", err.Error())
				return
			}
			tbError(w, http.StatusConflict, "grant_bind_failed", err.Error())
			return
		}
		state := sqlite.TokenBankWithdrawStatusBound
		if req.Reissue {
			state = sqlite.TokenBankWithdrawStatusReissued
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "request_id": requestID, "grant_id": grantID, "state": state})
	}
}

func (h *SkillMarketHandlers) TokenBankHubReconcile(auth hubSecretVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID, ok := h.tokenBankHubCaller(w, r, auth)
		if !ok {
			return
		}
		repo := h.tokenBankHubLedger()
		if repo == nil {
			tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
			return
		}
		var req tokenBankHubReconcileBody
		if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
			return
		}
		// Same rows as above: summed anywhere but the clearing node this is
		// always zero, and the hub concludes it never withdrew anything.
		if h.tokenBankRouteToClearing(w, r) {
			return
		}
		user, ok := h.tokenBankUserByEmail(w, r, req.Email)
		if !ok {
			return
		}
		if !h.tokenBankHubMayActFor(w, r, hubID, user.ID) {
			return
		}
		sum, err := repo.SumWithdrawalMicro(r.Context(), user.ID, hubID)
		if err != nil {
			tbError(w, http.StatusInternalServerError, "reconcile_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "hub_id": hubID, "withdrawn_micro": sum})
	}
}

func (h *SkillMarketHandlers) tokenBankHubCaller(w http.ResponseWriter, r *http.Request, auth hubSecretVerifier) (string, bool) {
	if auth == nil {
		tbError(w, http.StatusServiceUnavailable, "hub_auth_unavailable", "hub authentication is not available")
		return "", false
	}
	hubID := strings.TrimSpace(r.PathValue("id"))
	if hubID == "" {
		tbError(w, http.StatusBadRequest, "invalid_hub_id", "hub id is required")
		return "", false
	}
	secret := hubCredentialSecret(r)
	if err := auth.VerifyHubSecret(r.Context(), hubID, secret); err != nil {
		if errors.Is(err, hubs.ErrHubUnauthorized) {
			tbError(w, http.StatusUnauthorized, "hub_unauthorized", "hub secret was rejected")
			return "", false
		}
		tbError(w, http.StatusInternalServerError, "hub_auth_failed", err.Error())
		return "", false
	}
	return hubID, true
}

func (h *SkillMarketHandlers) tokenBankUserByEmail(w http.ResponseWriter, r *http.Request, email string) (*skillmarket.SkillMarketUser, bool) {
	email = strings.TrimSpace(email)
	if email == "" {
		tbError(w, http.StatusBadRequest, "missing_email", "email is required")
		return nil, false
	}
	if h == nil || h.store == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank users are not available on this node")
		return nil, false
	}
	user, err := h.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, skillmarket.ErrNotFound) {
			tbError(w, http.StatusNotFound, "user_not_found", "no token bank account for that email")
			return nil, false
		}
		tbError(w, http.StatusInternalServerError, "user_lookup_failed", err.Error())
		return nil, false
	}
	if user == nil {
		tbError(w, http.StatusNotFound, "user_not_found", "no token bank account for that email")
		return nil, false
	}
	return user, true
}

func (h *SkillMarketHandlers) tokenBankHubMayActFor(w http.ResponseWriter, r *http.Request, hubID, userID string) bool {
	var member tokenBankHubMember
	if h != nil {
		member, _ = h.tokenBank.(tokenBankHubMember)
	}
	if member == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank hub links are not available on this node")
		return false
	}
	belongs, err := member.HubBelongsToUser(r.Context(), hubID, userID)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "hub_link_lookup_failed", err.Error())
		return false
	}
	if !belongs {
		tbError(w, http.StatusForbidden, "hub_not_linked", "this hub is not linked to that account")
		return false
	}
	return true
}

func (h *SkillMarketHandlers) tokenBankHubLedger() tokenBankHubLedger {
	if h == nil || h.tokenBank == nil {
		return nil
	}
	ledger, _ := h.tokenBank.(tokenBankHubLedger)
	return ledger
}

func (h *SkillMarketHandlers) tokenBankGroupChecker() tokenBankGroupChecker {
	if h == nil {
		return nil
	}
	if h.tokenBankGroups != nil {
		return h.tokenBankGroups
	}
	checker, _ := h.tokenBankLLM.(tokenBankGroupChecker)
	return checker
}

func writeTokenBankWithdrawError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sqlite.ErrTokenBankInsufficient):
		tbError(w, http.StatusPaymentRequired, "insufficient_credits", err.Error())
	case errors.Is(err, sqlite.ErrTokenBankNothingToWithdraw):
		tbError(w, http.StatusPaymentRequired, "insufficient_credits", err.Error())
	case errors.Is(err, sqlite.ErrTokenBankWithdrawalMismatch):
		tbError(w, http.StatusConflict, "withdrawal_mismatch", "request id is already used by another hub or account")
	case errors.Is(err, sqlite.ErrTokenBankGiftWithdrawn):
		tbError(w, http.StatusConflict, "gift_already_withdrawn", "this gift was already withdrawn")
	default:
		tbError(w, http.StatusInternalServerError, "withdraw_failed", err.Error())
	}
}
