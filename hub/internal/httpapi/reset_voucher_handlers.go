package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// IssueResetVouchersHandler issues one-time quota reset vouchers to users who
// currently hold a live new-user limit card. An empty group_ids list considers
// every active tenant user; users without a live welcome card are skipped.
func IssueResetVouchersHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			GroupIDs      []string `json:"group_ids"`
			DepartmentIDs []string `json:"department_ids"`
			ExpiresDays   int      `json:"expires_days"`
			ValidityDays  int      `json:"validity_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.ExpiresDays == 0 {
			req.ExpiresDays = req.ValidityDays
		}
		if req.ExpiresDays <= 0 || req.ExpiresDays > 3650 {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "expires_days must be between 1 and 3650")
			return
		}
		allGroupIDs := append(append([]string{}, req.GroupIDs...), req.DepartmentIDs...)
		targets, err := resolveLLMUserTargets(r, identity, securitySvc, allGroupIDs)
		if err != nil {
			code := "SECURITY_GROUP_LOAD_FAILED"
			if !hasLLMTargetGroups(allGroupIDs) {
				code = "USER_LOAD_FAILED"
			}
			writeError(w, http.StatusInternalServerError, code, err.Error())
			return
		}
		tenantSystem := scopedSystemSettingsForRequest(r, system)
		reg, err := llmservice.LoadRegistry(r.Context(), tenantSystem)
		if err != nil {
			writeError(w, 500, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		issued := llmservice.IssueResetVouchers(reg, targets, req.ExpiresDays, time.Now().UTC())
		if issued > 0 {
			if err := llmservice.SaveRegistry(r.Context(), tenantSystem, reg); err != nil {
				writeError(w, 500, "LLM_SERVICE_SAVE_FAILED", err.Error())
				return
			}
			invalidateLLMRuntimeCaches(tenantSystem)
		}
		writeLLMServiceCardAdminAudit(r.Context(), firstAdminAuditRepo(audits...), RequestTenantID(r), "llm.reset_voucher.issue", map[string]any{"issued": issued, "group_ids": allGroupIDs, "expires_days": req.ExpiresDays})
		writeJSON(w, http.StatusOK, map[string]any{"issued": issued})
	}
}

func hasLLMTargetGroups(groupIDs []string) bool {
	for _, id := range groupIDs {
		if strings.TrimSpace(id) != "" {
			return true
		}
	}
	return false
}

// resolveLLMUserTargets returns active tenant users, optionally narrowed to
// selected security groups (including descendants).
func resolveLLMUserTargets(r *http.Request, identity *auth.IdentityService, securitySvc *security.SecurityService, groupIDs []string) ([]llmservice.VoucherUser, error) {
	if identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	users, err := identity.ListUsersForTenant(r.Context(), RequestTenantID(r))
	if err != nil {
		return nil, err
	}
	usersByKey := map[string]llmservice.VoucherUser{}
	for _, u := range users {
		if u == nil || (strings.TrimSpace(u.Status) != "" && !strings.EqualFold(strings.TrimSpace(u.Status), "active")) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(u.Email))
		if key == "" {
			key = "id:" + strings.TrimSpace(u.ID)
		}
		if key != "id:" {
			usersByKey[key] = llmservice.VoucherUser{ID: u.ID, Email: u.Email}
		}
	}
	selected := map[string]bool{}
	for _, id := range groupIDs {
		if id = strings.TrimSpace(id); id != "" {
			selected[id] = true
		}
	}
	if len(selected) == 0 {
		out := make([]llmservice.VoucherUser, 0, len(usersByKey))
		for _, u := range usersByKey {
			out = append(out, u)
		}
		return out, nil
	}
	if securitySvc == nil {
		return nil, fmt.Errorf("organization service unavailable")
	}
	allowed := map[string]bool{}
	var walk func(string) error
	walk = func(id string) error {
		if allowed[id] {
			return nil
		}
		allowed[id] = true
		children, e := securitySvc.GetGroupChildren(security.WithTenant(r.Context(), RequestTenantID(r)), id)
		if e != nil {
			return e
		}
		for _, c := range children {
			if c != nil {
				if e := walk(c.ID); e != nil {
					return e
				}
			}
		}
		return nil
	}
	for id := range selected {
		if err := walk(id); err != nil {
			return nil, err
		}
	}
	keep := map[string]llmservice.VoucherUser{}
	for id := range allowed {
		members, e := securitySvc.ListGroupMembers(security.WithTenant(r.Context(), RequestTenantID(r)), id)
		if e != nil {
			return nil, e
		}
		for _, email := range members {
			if v, ok := usersByKey[strings.ToLower(strings.TrimSpace(email))]; ok {
				key := strings.ToLower(strings.TrimSpace(v.Email))
				if key == "" {
					key = "id:" + strings.TrimSpace(v.ID)
				}
				keep[key] = v
			}
		}
	}
	out := make([]llmservice.VoucherUser, 0, len(keep))
	for _, u := range keep {
		out = append(out, u)
	}
	return out, nil
}

// ManageNewUserBenefitHandler issues or revokes configured new-user limit cards
// for the selected users. Existing cards are never duplicated.
func ManageNewUserBenefitHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action        string   `json:"action"`
			GroupIDs      []string `json:"group_ids"`
			DepartmentIDs []string `json:"department_ids"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			writeError(w, 400, "INVALID_JSON", "Invalid request body")
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		if action == "grant" || action == "give" {
			action = "issue"
		} else if action == "withdraw" || action == "remove" {
			action = "revoke"
		}
		// Explicit action endpoints are authoritative; this prevents a body
		// value from turning /issue into a revoke (or vice versa).
		pathAction := ""
		if strings.HasSuffix(strings.TrimSpace(r.URL.Path), "/revoke") {
			pathAction = "revoke"
		} else if strings.HasSuffix(strings.TrimSpace(r.URL.Path), "/issue") {
			pathAction = "issue"
		}
		if pathAction != "" {
			action = pathAction
		}
		if action != "issue" && action != "revoke" {
			writeError(w, 400, "INVALID_INPUT", "action must be issue or revoke")
			return
		}
		targets, err := resolveLLMUserTargets(r, identity, securitySvc, append(append([]string{}, req.GroupIDs...), req.DepartmentIDs...))
		if err != nil {
			writeError(w, 500, "USER_TARGET_LOAD_FAILED", err.Error())
			return
		}
		tenantSystem := scopedSystemSettingsForRequest(r, system)
		reg, err := llmservice.LoadRegistry(r.Context(), tenantSystem)
		if err != nil {
			writeError(w, 500, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		count := 0
		if action == "issue" {
			count = llmservice.IssueNewUserLimitCards(reg, targets, time.Now().UTC())
		} else {
			count = llmservice.RevokeNewUserLimitCards(reg, targets)
		}
		if count > 0 {
			if err := llmservice.SaveRegistry(r.Context(), tenantSystem, reg); err != nil {
				writeError(w, 500, "LLM_SERVICE_SAVE_FAILED", err.Error())
				return
			}
			invalidateLLMRuntimeCaches(tenantSystem)
		}
		writeLLMServiceCardAdminAudit(r.Context(), firstAdminAuditRepo(audits...), RequestTenantID(r), "llm.new_user_limit_card."+action, map[string]any{"count": count, "group_ids": append(append([]string{}, req.GroupIDs...), req.DepartmentIDs...)})
		result := map[string]any{"issued": 0, "revoked": 0}
		if action == "issue" {
			result["issued"] = count
		} else {
			result["revoked"] = count
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func RedeemResetVoucherHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, 401, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			VoucherID string `json:"voucher_id"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			writeError(w, 400, "INVALID_JSON", "Invalid request body")
			return
		}
		if strings.TrimSpace(req.VoucherID) == "" {
			writeError(w, 400, "INVALID_INPUT", "voucher_id is required")
			return
		}
		tenantSystem := scopedSystemSettingsForTenant(principal.TenantID, system)
		reg, err := llmservice.LoadRegistry(r.Context(), tenantSystem)
		if err != nil {
			writeError(w, 500, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		ok, err := llmservice.RedeemResetVoucher(reg, principal.UserID, principal.Email, req.VoucherID, time.Now().UTC())
		if err != nil || !ok {
			writeError(w, 400, "RESET_VOUCHER_REDEEM_FAILED", voucherErrString(err, "voucher redemption failed"))
			return
		}
		if err := llmservice.SaveRegistry(r.Context(), tenantSystem, reg); err != nil {
			writeError(w, 500, "LLM_SERVICE_SAVE_FAILED", err.Error())
			return
		}
		invalidateLLMRuntimeCaches(tenantSystem)
		writeLLMServiceCardAdminAudit(r.Context(), firstAdminAuditRepo(audits...), principal.TenantID, "llm.reset_voucher.redeem", map[string]any{"voucher_id": req.VoucherID, "user_id": principal.UserID})
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	}
}

func voucherErrString(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}
