package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type generateCodesRequest struct {
	Count        int `json:"count"`
	ValidityDays int `json:"validity_days"`
}

type toggleInvitationCodeRequest struct {
	Required bool `json:"required"`
}

type invitationCodeResponse struct {
	ID           string  `json:"id"`
	Code         string  `json:"code"`
	Status       string  `json:"status"`
	UsedByEmail  string  `json:"used_by_email"`
	UsedAt       *string `json:"used_at"`
	ValidityDays int     `json:"validity_days"`
	BoundAt      *string `json:"bound_at"`
	CreatedAt    string  `json:"created_at"`
}

func GenerateInvitationCodesHandler(svc *invitation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req generateCodesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		codes, err := svc.GenerateCodes(r.Context(), req.Count, req.ValidityDays)
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
		pageStr := r.URL.Query().Get("page")
		pageSizeStr := r.URL.Query().Get("page_size")

		page, _ := strconv.Atoi(pageStr)
		pageSize, _ := strconv.Atoi(pageSizeStr)
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 200 {
			pageSize = 20
		}

		codes, total, err := svc.ListCodesPaged(r.Context(), status, search, page, pageSize)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}

		resp := make([]invitationCodeResponse, len(codes))
		for i, c := range codes {
			resp[i] = toInvitationCodeResponse(c)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"codes":     resp,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		})
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

func ExportInvitationCodesHandler(svc *invitation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		search := r.URL.Query().Get("search")

		codes, err := svc.ListCodes(r.Context(), status, search)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "EXPORT_FAILED", err.Error())
			return
		}

		var sb strings.Builder
		for _, c := range codes {
			sb.WriteString(c.Code)
			sb.WriteByte('\n')
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=invitation_codes_%s.txt", time.Now().Format("20060102_150405")))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sb.String()))
	}
}

func UnbindInvitationCodeHandler(svc *invitation.Service, identity *auth.IdentityService, deviceSvc *device.Service, feishuNotifier *feishu.Notifier, imCleaners []IMBindingCleaner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}

		// Look up the invitation code to get the bound email before cleanup.
		code, err := svc.GetCodeByID(r.Context(), req.ID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "invitation code not found")
			return
		}

		email := code.UsedByEmail
		var deletedMachines int64

		// If the code was bound to an email, clean up all associated data.
		if email != "" {
			if identity != nil {
				user, lookupErr := identity.LookupUserByEmail(r.Context(), email)
				if lookupErr != nil {
					log.Printf("[admin-unbind] lookup user %s failed: %v", email, lookupErr)
				}
				if user != nil && deviceSvc != nil {
					deleted, delErr := deviceSvc.ForceDeleteMachinesByUser(r.Context(), user.ID)
					if delErr != nil {
						log.Printf("[admin-unbind] delete machines for user %s failed: %v", user.ID, delErr)
					} else {
						deletedMachines = deleted
					}
				}
			}

			// Remove all invitation codes bound to this email.
			codesDeleted, delErr := svc.DeleteCodeByEmail(r.Context(), email)
			if delErr != nil {
				log.Printf("[admin-unbind] delete codes for %s failed: %v", email, delErr)
			} else if codesDeleted > 0 {
				log.Printf("[admin-unbind] deleted %d invitation code(s) for %s", codesDeleted, email)
			}

			// Remove IM bindings.
			if feishuNotifier != nil {
				feishuNotifier.RemoveOpenID(email)
			}
			for _, cleaner := range imCleaners {
				if cleaner != nil {
					cleaner.RemoveBindingByEmail(email)
				}
			}
		}

		// Delete the invitation code itself (if not already deleted by DeleteCodeByEmail above).
		if delErr := svc.DeleteCode(r.Context(), req.ID); delErr != nil {
			log.Printf("[admin-unbind] delete code %s: %v (may already be deleted)", req.ID, delErr)
		}

		log.Printf("[admin-unbind] code=%s email=%s machines_deleted=%d", code.Code, email, deletedMachines)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               true,
			"email":            email,
			"deleted_machines": deletedMachines,
		})
	}
}

func toInvitationCodeResponse(c *store.InvitationCode) invitationCodeResponse {
	resp := invitationCodeResponse{
		ID:           c.ID,
		Code:         c.Code,
		Status:       c.Status,
		UsedByEmail:  c.UsedByEmail,
		ValidityDays: c.ValidityDays,
		CreatedAt:    c.CreatedAt.Format(time.RFC3339),
	}
	if c.UsedAt != nil {
		t := c.UsedAt.Format(time.RFC3339)
		resp.UsedAt = &t
		resp.BoundAt = &t
	}
	return resp
}

// DeprecatedEmailInviteHandler returns HTTP 410 Gone for the removed Email invite API.
// The Email invite feature has been removed. Use invitation codes instead.
func DeprecatedEmailInviteHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusGone, "FEATURE_REMOVED", "Email invite feature has been removed. Use invitation codes instead.")
	}
}
