package httpapi

import (
	"crypto/rand"
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
	Count        int  `json:"count"`
	ValidityDays int  `json:"validity_days"`
	VIP          bool `json:"vip"`
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
	Exported     bool    `json:"exported"`
	VIP          bool    `json:"vip"`
	TenantID     string  `json:"tenant_id"`
	CreatedAt    string  `json:"created_at"`
}

func GenerateInvitationCodesHandler(svc *invitation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req generateCodesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		codes, err := svc.GenerateCodesForTenant(r.Context(), RequestTenantID(r), req.Count, req.ValidityDays, req.VIP)
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

		codes, total, err := svc.ListCodesPagedForTenant(r.Context(), RequestTenantID(r), status, search, page, pageSize)
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
		// exported filter: "unexported" (default), "exported", "all"
		exportedFilter := r.URL.Query().Get("exported")
		if exportedFilter == "" {
			exportedFilter = "unexported"
		}

		vipOnly := r.URL.Query().Get("vip") == "true"

		codes, err := svc.ExportUnusedCodesForTenant(r.Context(), RequestTenantID(r), exportedFilter, vipOnly)
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
		w.Header().Set("X-Export-Count", fmt.Sprintf("%d", len(codes)))
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
				user, lookupErr := identity.UsersRepo().GetByTenantEmail(r.Context(), code.TenantID, email)
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
			codesDeleted, delErr := svc.DeleteCodeByTenantEmail(r.Context(), code.TenantID, email)
			if delErr != nil {
				log.Printf("[admin-unbind] delete codes for %s failed: %v", email, delErr)
			} else if codesDeleted > 0 {
				log.Printf("[admin-unbind] deleted %d invitation code(s) for %s", codesDeleted, email)
			}

			// Remove IM bindings.
			if feishuNotifier != nil {
				feishuNotifier.RemoveOpenIDForTenant(code.TenantID, email)
			}
			removeIMBindingsForTenant(imCleaners, code.TenantID, email)

			// Delete the user record so bind-query returns unbound.
			if identity != nil {
				if repo := identity.UsersRepo(); repo != nil {
					if delErr := repo.DeleteByTenantEmail(r.Context(), code.TenantID, email); delErr != nil {
						log.Printf("[admin-unbind] delete user record for %s failed: %v", email, delErr)
					}
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
		TenantID:     c.TenantID,
		Code:         c.Code,
		Status:       c.Status,
		UsedByEmail:  c.UsedByEmail,
		ValidityDays: c.ValidityDays,
		Exported:     c.Exported,
		VIP:          c.VIP,
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

// --- Email Invite (restored) ---

type emailInviteResponse struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toEmailInviteResponse(item *store.EmailInvite) emailInviteResponse {
	return emailInviteResponse{
		ID:        item.ID,
		TenantID:  item.TenantID,
		Email:     item.Email,
		Role:      item.Role,
		Status:    item.Status,
		CreatedAt: item.CreatedAt.Format(time.RFC3339),
		UpdatedAt: item.UpdatedAt.Format(time.RFC3339),
	}
}

func emailInviteID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("ei_%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("ei_%x", buf)
}

func CreateEmailInviteHandler(repo store.EmailInviteRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		switch req.Role {
		case "viewer", "member", "admin":
			// valid
		case "":
			req.Role = "viewer"
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "role must be viewer, member, or admin")
			return
		}
		now := time.Now()
		item := &store.EmailInvite{
			ID:        emailInviteID(),
			TenantID:  RequestTenantID(r),
			Email:     req.Email,
			Role:      req.Role,
			Status:    "pending",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := repo.Create(r.Context(), item); err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toEmailInviteResponse(item))
	}
}

func ListEmailInvitesHandler(repo store.EmailInviteRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			items []*store.EmailInvite
			err   error
		)
		if isTenantScopedAdminRequest(r) {
			items, err = repo.ListByTenant(r.Context(), RequestTenantID(r))
		} else {
			items, err = repo.List(r.Context())
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		resp := make([]emailInviteResponse, len(items))
		for i, item := range items {
			resp[i] = toEmailInviteResponse(item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"invites": resp})
	}
}

func DeleteEmailInviteHandler(repo store.EmailInviteRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		existing, err := repo.GetByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "invite not found")
			return
		}
		if isTenantScopedAdminRequest(r) && strings.TrimSpace(existing.TenantID) != RequestTenantID(r) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "invite not found")
			return
		}
		if err := repo.DeleteByID(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
