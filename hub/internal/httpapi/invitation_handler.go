package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type generateCodesRequest struct {
	Count int `json:"count"`
}

type toggleInvitationCodeRequest struct {
	Required bool `json:"required"`
}

type invitationCodeResponse struct {
	ID          string  `json:"id"`
	Code        string  `json:"code"`
	Status      string  `json:"status"`
	UsedByEmail string  `json:"used_by_email"`
	UsedAt      *string `json:"used_at"`
	CreatedAt   string  `json:"created_at"`
}

func GenerateInvitationCodesHandler(svc *invitation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req generateCodesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		codes, err := svc.GenerateCodes(r.Context(), req.Count)
		if err != nil {
			if errors.Is(err, invitation.ErrInvalidCount) {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "GENERATE_FAILED", err.Error())
			return
		}

		resp := make([]invitationCodeResponse, len(codes))
		for i, c := range codes {
			resp[i] = toInvitationCodeResponse(c)
		}
		writeJSON(w, http.StatusOK, map[string]any{"codes": resp})
	}
}

func ListInvitationCodesHandler(svc *invitation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		search := r.URL.Query().Get("search")

		codes, err := svc.ListCodes(r.Context(), status, search)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}

		resp := make([]invitationCodeResponse, len(codes))
		for i, c := range codes {
			resp[i] = toInvitationCodeResponse(c)
		}
		writeJSON(w, http.StatusOK, map[string]any{"codes": resp})
	}
}

func ToggleInvitationCodeHandler(svc *invitation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req toggleInvitationCodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if err := svc.SetRequired(r.Context(), req.Required); err != nil {
			writeError(w, http.StatusInternalServerError, "TOGGLE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                       true,
			"invitation_code_required": req.Required,
		})
	}
}

func InvitationCodeStatusHandler(svc *invitation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		required, err := svc.IsRequired(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STATUS_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"invitation_code_required": required,
		})
	}
}

func toInvitationCodeResponse(c *store.InvitationCode) invitationCodeResponse {
	resp := invitationCodeResponse{
		ID:          c.ID,
		Code:        c.Code,
		Status:      c.Status,
		UsedByEmail: c.UsedByEmail,
		CreatedAt:   c.CreatedAt.Format(time.RFC3339),
	}
	if c.UsedAt != nil {
		t := c.UsedAt.Format(time.RFC3339)
		resp.UsedAt = &t
	}
	return resp
}
