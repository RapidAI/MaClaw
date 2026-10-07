package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Token Bank pull. The viewer is authenticated here, the service group is
// resolved here, and only then does the hub debit HubCenter and write a
// permanent grant. A missing hub registration returns before any debit.

type tokenBankWithdrawBody struct {
	RequestID      string `json:"request_id"`
	ServiceGroupID string `json:"service_group_id"`
	AmountMicro    int64  `json:"amount_micro"`
	Manual         bool   `json:"manual"`
	Kind           string `json:"kind"`
	LinkID         string `json:"link_id"`
}

// TokenBankHubIDHandler tells the signed-in desktop which HubCenter hub id
// this machine is. "提取到本机" has to debit with that id on the user session,
// then replay the same request through the hub pull. The registration secret
// is not part of the response.
func TokenBankHubIDHandler(identity *auth.IdentityService, centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET is required")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil || strings.TrimSpace(principal.Email) == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if centerSvc == nil {
			writeError(w, http.StatusServiceUnavailable, "TOKEN_BANK_UNAVAILABLE", "Token Bank withdraw is not available on this hub")
			return
		}
		status, err := centerSvc.Status(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_STATUS", err.Error())
			return
		}
		hubID := ""
		if status != nil {
			hubID = strings.TrimSpace(status.HubID)
		}
		if hubID == "" {
			writeError(w, http.StatusConflict, "HUB_NOT_REGISTERED", "this Hub is not registered with HubCenter")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hub_id": hubID})
	}
}

func TokenBankWithdrawHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST is required")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil || strings.TrimSpace(principal.Email) == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if centerSvc == nil || system == nil {
			writeError(w, http.StatusServiceUnavailable, "TOKEN_BANK_UNAVAILABLE", "Token Bank withdraw is not available on this hub")
			return
		}
		var req tokenBankWithdrawBody
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		requestID := strings.TrimSpace(req.RequestID)
		if requestID == "" {
			writeError(w, http.StatusBadRequest, "MISSING_REQUEST_ID", "request id is required")
			return
		}
		if req.AmountMicro < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_AMOUNT", "amount must not be negative")
			return
		}
		groupID := strings.TrimSpace(req.ServiceGroupID)
		if groupID == "" {
			cfg, cfgErr := llmservice.LoadTokenBankAutoSettings(r.Context(), system)
			if cfgErr != nil {
				writeError(w, http.StatusInternalServerError, "TOKEN_BANK_SETTINGS", cfgErr.Error())
				return
			}
			groupID = cfg.ServiceGroupID
		}
		resolved, err := llmservice.ResolveTokenBankServiceGroup(r.Context(), system, groupID)
		if err != nil {
			if errors.Is(err, llmservice.ErrTokenBankServiceGroupMissing) {
				writeError(w, http.StatusBadRequest, "SERVICE_GROUP_MISSING", "choose a service group that includes the Token Bank before withdrawing")
				return
			}
			writeError(w, http.StatusInternalServerError, "SERVICE_GROUP_LOOKUP", err.Error())
			return
		}
		if resolved == "" {
			writeError(w, http.StatusBadRequest, "SERVICE_GROUP_REQUIRED", "choose a service group that includes the Token Bank before withdrawing")
			return
		}
		kind := strings.TrimSpace(req.Kind)
		if kind == "" {
			kind = "self"
		}
		result, err := llmservice.PullTokenBankGrant(r.Context(), system, centerSvc, principal.UserID, principal.Email, resolved, requestID, req.AmountMicro, req.Manual, kind, req.LinkID)
		if err != nil {
			status := http.StatusBadGateway
			code := "TOKEN_BANK_WITHDRAW_FAILED"
			if errors.Is(err, llmservice.ErrTokenBankServiceGroupMissing) {
				status = http.StatusBadRequest
				code = "SERVICE_GROUP_MISSING"
			} else if errors.Is(err, llmservice.ErrTokenBankNothingToWithdraw) {
				status = http.StatusPaymentRequired
				code = "NOTHING_TO_WITHDRAW"
			} else if errors.Is(err, llmservice.ErrTokenBankInsufficient) {
				status = http.StatusPaymentRequired
				code = "INSUFFICIENT_CREDITS"
			} else if errors.Is(err, llmservice.ErrTokenBankGrantPending) {
				status = http.StatusBadGateway
				code = "GRANT_PENDING"
			} else if errors.Is(err, llmservice.ErrTokenBankGrantGroupMismatch) {
				status = http.StatusConflict
				code = "GRANT_GROUP_MISMATCH"
			}
			writeError(w, status, code, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "ok",
			"grant_id":      result.GrantID,
			"request_id":    result.RequestID,
			"amount_micro":  result.AmountMicro,
			"created":       result.Created,
			"service_group": resolved,
		})
	}
}

type tokenBankAutoSettingsBody struct {
	MaxPerWithdrawMicro *int64 `json:"max_per_withdraw_micro"`
}

func writeTokenBankAutoSettings(w http.ResponseWriter, cfg llmservice.TokenBankAutoSettings) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                cfg.Enabled,
		"max_per_withdraw_micro": cfg.MaxPerWithdrawMicro,
		"service_group_id":       cfg.ServiceGroupID,
	})
}

// TokenBankAutoSettingsHandler reads and stores this machine's ceiling for
// one automatic Token Bank pull. Zero means no local ceiling. The 1/N share
// on HubCenter still applies.
func TokenBankAutoSettingsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET or PUT is required")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil || strings.TrimSpace(principal.Email) == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if system == nil {
			writeError(w, http.StatusServiceUnavailable, "TOKEN_BANK_UNAVAILABLE", "Token Bank withdraw is not available on this hub")
			return
		}
		if r.Method == http.MethodGet {
			cfg, err := llmservice.LoadTokenBankAutoSettings(r.Context(), system)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "TOKEN_BANK_SETTINGS", err.Error())
				return
			}
			writeTokenBankAutoSettings(w, cfg)
			return
		}
		var req tokenBankAutoSettingsBody
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.MaxPerWithdrawMicro == nil {
			writeError(w, http.StatusBadRequest, "MISSING_CAP", "max_per_withdraw_micro is required")
			return
		}
		if *req.MaxPerWithdrawMicro < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_AMOUNT", "automatic withdrawal cap must not be negative")
			return
		}
		if err := llmservice.SaveTokenBankAutoMaxPerWithdraw(r.Context(), system, *req.MaxPerWithdrawMicro); err != nil {
			writeError(w, http.StatusInternalServerError, "TOKEN_BANK_SETTINGS", err.Error())
			return
		}
		cfg, err := llmservice.LoadTokenBankAutoSettings(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TOKEN_BANK_SETTINGS", err.Error())
			return
		}
		writeTokenBankAutoSettings(w, cfg)
	}
}
