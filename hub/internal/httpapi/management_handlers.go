package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type ManualBindRequest struct {
	Email string `json:"email"`
}

type LookupUserRequest struct {
	Email  string `json:"email"`
	Mobile string `json:"mobile"`
}

type DeleteBoundUserRequest struct {
	Email    string `json:"email"`
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Phone    string `json:"phone"`
}

type ForceDeleteVirtualBoundUserRequest struct {
	Email         string `json:"email"`
	TenantID      string `json:"tenant_id"`
	AdminPassword string `json:"admin_password"`
}

type BlockEmailRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type CenterConfigRequest struct {
	BaseURL               string   `json:"base_url"`
	PublicBaseURL         string   `json:"public_base_url"`
	Visibility            string   `json:"visibility"`
	EnrollmentMode        string   `json:"enrollment_mode"`
	CorporateEmailDomain  *string  `json:"corporate_email_domain,omitempty"`
	CorporateEmailDomains []string `json:"corporate_email_domains,omitempty"`
	AcceptPublicSignup    *bool    `json:"accept_public_signup,omitempty"`
}

type FailureLogView struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Category  string         `json:"category"`
	EventCode string         `json:"event_code"`
	Message   string         `json:"message"`
	EntityID  string         `json:"entity_id"`
	Email     string         `json:"email"`
	ClientIP  string         `json:"client_ip"`
	Details   map[string]any `json:"details"`
	CreatedAt string         `json:"created_at"`
}

func ListFailureLogsHandler(repo store.FailureEventLogRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "FAILURE_LOGS_UNAVAILABLE", "Failure logs are unavailable")
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		filter := store.FailureEventLogFilter{
			Keyword:  strings.TrimSpace(r.URL.Query().Get("keyword")),
			Category: strings.TrimSpace(r.URL.Query().Get("category")),
			Offset:   offset,
			Limit:    limit,
		}
		if admin := AdminFromContext(r.Context()); admin != nil && strings.TrimSpace(admin.Scope) == "tenant" {
			filter.TenantID = AdminTenantID(r.Context())
			filter.TenantScoped = true
		} else if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
			filter.TenantID = tenantID
			filter.TenantScoped = true
		}
		items, total, err := repo.List(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILURE_LOGS_FAILED", err.Error())
			return
		}
		logs := make([]FailureLogView, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			details := map[string]any{}
			if strings.TrimSpace(item.DetailsJSON) != "" {
				_ = json.Unmarshal([]byte(item.DetailsJSON), &details)
			}
			logs = append(logs, FailureLogView{
				ID:        item.ID,
				TenantID:  item.TenantID,
				Category:  item.Category,
				EventCode: item.EventCode,
				Message:   item.Message,
				EntityID:  item.EntityID,
				Email:     item.Email,
				ClientIP:  item.ClientIP,
				Details:   details,
				CreatedAt: item.CreatedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "total": total, "offset": offset, "limit": limit})
	}
}

type BoundUserView struct {
	ID                string                    `json:"id"`
	TenantID          string                    `json:"tenant_id"`
	Email             string                    `json:"email"`
	Emails            []string                  `json:"emails,omitempty"`
	Phone             string                    `json:"phone,omitempty"`
	Phones            []string                  `json:"phones,omitempty"`
	Identities        []BoundUserIdentityView   `json:"identities,omitempty"`
	SN                string                    `json:"sn"`
	Status            string                    `json:"status"`
	EnrollmentStatus  string                    `json:"enrollment_status"`
	AccountType       string                    `json:"account_type,omitempty"`
	IsVirtualEmployee bool                      `json:"is_virtual_employee,omitempty"`
	SmartRoute        bool                      `json:"smart_route"`
	EmailVerified     bool                      `json:"email_verified"`
	HasServiceAccess  bool                      `json:"has_service_access,omitempty"`
	ServiceStatus     *llmservice.ServiceStatus `json:"service_status,omitempty"`
}

type BoundUserIdentityView struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Verified bool   `json:"verified"`
}

type boundUserIdentityBatchLister interface {
	ListIdentitiesByUsers(ctx context.Context, tenantID string, userIDs []string) (map[string][]*store.UserIdentity, error)
}

func boundUserIdentityKey(tenantID, userID string) string {
	return store.NormalizeTenantID(tenantID) + "\x00" + strings.TrimSpace(userID)
}

func preloadBoundUserIdentities(ctx context.Context, repo store.UserRepository, users []*store.User) map[string][]*store.UserIdentity {
	out := map[string][]*store.UserIdentity{}
	if repo == nil {
		return out
	}
	tenantUsers := map[string][]string{}
	seen := map[string]struct{}{}
	for _, user := range users {
		if user == nil || strings.TrimSpace(user.ID) == "" {
			continue
		}
		tenantID := store.NormalizeTenantID(user.TenantID)
		key := boundUserIdentityKey(tenantID, user.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tenantUsers[tenantID] = append(tenantUsers[tenantID], strings.TrimSpace(user.ID))
	}
	if batch, ok := repo.(boundUserIdentityBatchLister); ok {
		for tenantID, userIDs := range tenantUsers {
			rowsByUser, err := batch.ListIdentitiesByUsers(ctx, tenantID, userIDs)
			if err != nil {
				log.Printf("[admin/users] ListIdentitiesByUsers failed for tenant=%s: %v", tenantID, err)
				preloadBoundUserIdentitiesIndividually(ctx, repo, out, tenantID, userIDs)
				continue
			}
			for userID, rows := range rowsByUser {
				out[boundUserIdentityKey(tenantID, userID)] = rows
			}
		}
		return out
	}
	for tenantID, userIDs := range tenantUsers {
		preloadBoundUserIdentitiesIndividually(ctx, repo, out, tenantID, userIDs)
	}
	return out
}

func preloadBoundUserIdentitiesIndividually(ctx context.Context, repo store.UserRepository, out map[string][]*store.UserIdentity, tenantID string, userIDs []string) {
	for _, userID := range userIDs {
		rows, err := repo.ListIdentitiesByUser(ctx, tenantID, userID)
		if err != nil {
			log.Printf("[admin/users] ListIdentitiesByUser failed for tenant=%s user=%s: %v", tenantID, userID, err)
			continue
		}
		out[boundUserIdentityKey(tenantID, userID)] = rows
	}
}

func boundUserContactFields(user *store.User, identityRows []*store.UserIdentity) ([]string, string, []string, []BoundUserIdentityView) {
	emailSeen := map[string]struct{}{}
	phoneSeen := map[string]struct{}{}
	identitySeen := map[string]struct{}{}
	emails := make([]string, 0, 1)
	phones := make([]string, 0, 1)
	identityViews := make([]BoundUserIdentityView, 0, 2)

	addEmail := func(value string, verified bool) {
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(strings.ToLower(value), "phone:") || !strings.Contains(value, "@") {
			return
		}
		key := strings.ToLower(value)
		if _, ok := emailSeen[key]; !ok {
			emailSeen[key] = struct{}{}
			emails = append(emails, value)
		}
		identityKey := "email\x00" + key
		if _, ok := identitySeen[identityKey]; !ok {
			identitySeen[identityKey] = struct{}{}
			identityViews = append(identityViews, BoundUserIdentityView{Type: "email", Value: value, Verified: verified})
		}
	}
	addPhone := func(value string, verified bool) {
		value = strings.TrimSpace(value)
		value = strings.TrimPrefix(strings.ToLower(value), "phone:")
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := phoneSeen[key]; !ok {
			phoneSeen[key] = struct{}{}
			phones = append(phones, value)
		}
		identityKey := "phone\x00" + key
		if _, ok := identitySeen[identityKey]; !ok {
			identitySeen[identityKey] = struct{}{}
			identityViews = append(identityViews, BoundUserIdentityView{Type: "phone", Value: value, Verified: verified})
		}
	}

	if user != nil {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(user.Email)), "phone:") {
			addPhone(user.Email, true)
		} else {
			addEmail(user.Email, user.EmailVerified)
		}
	}
	for _, row := range identityRows {
		if row == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(row.Type)) {
		case "email":
			addEmail(row.Value, row.Verified)
		case "phone":
			addPhone(row.Value, row.Verified)
		}
	}

	primaryPhone := ""
	if len(phones) > 0 {
		primaryPhone = phones[0]
	}
	return emails, primaryPhone, phones, identityViews
}

func boundUserEmailVerified(user *store.User, identities []BoundUserIdentityView) bool {
	if user != nil && user.EmailVerified {
		return true
	}
	for _, identity := range identities {
		if !identity.Verified {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(identity.Type), "email") && strings.Contains(strings.TrimSpace(identity.Value), "@") {
			return true
		}
	}
	return false
}

type BoundUserRouteDeleter interface {
	DeleteUserRoute(ctx context.Context, email string, tenantIDOpt ...string) error
}

func ManualBindHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ManualBindRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		user, err := identity.ManualBindForTenant(r.Context(), RequestTenantID(r), req.Email)
		if err != nil {
			if errors.Is(err, auth.ErrRoutedToAnotherHub) {
				writeError(w, http.StatusConflict, "EMAIL_ROUTED_TO_ANOTHER_HUB", err.Error())
				return
			}
			if errors.Is(err, auth.ErrEmailDomainNotAllowed) {
				writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "MANUAL_BIND_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"user": map[string]any{
				"id":    user.ID,
				"email": user.Email,
				"sn":    user.SN,
			},
		})
	}
}

func DeleteBoundUserHandler(identity *auth.IdentityService, purger *UserDataPurger, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		phone := strings.TrimSpace(r.URL.Query().Get("phone"))
		if r.Body != nil {
			var req DeleteBoundUserRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				if email == "" {
					email = strings.TrimSpace(req.Email)
				}
				if tenantID == "" {
					tenantID = strings.TrimSpace(req.TenantID)
				}
				if userID == "" {
					userID = strings.TrimSpace(req.UserID)
				}
				if phone == "" {
					phone = strings.TrimSpace(req.Phone)
				}
			}
		}
		if email == "" && userID == "" && phone == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email, user_id, or phone is required")
			return
		}
		email = strings.TrimSpace(strings.ToLower(email))
		if identity == nil || identity.UsersRepo() == nil {
			writeError(w, http.StatusInternalServerError, "USER_DELETE_UNAVAILABLE", "User repository is unavailable")
			return
		}

		user, err := resolveBoundUserForDelete(r, identity.UsersRepo(), tenantID, email, userID, phone)
		if err != nil {
			if err == errAmbiguousTenantEmail || err == errAmbiguousTenantIdentity {
				writeError(w, http.StatusBadRequest, "TENANT_ID_REQUIRED", "tenant_id is required when the identity exists in multiple tenants")
				return
			}
			writeError(w, http.StatusInternalServerError, "LOOKUP_USER_FAILED", err.Error())
			return
		}
		if user == nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}
		if boundUserIsVirtualEmployee(r.Context(), system, user) {
			writeError(w, http.StatusConflict, "VIRTUAL_USER_FORCE_DELETE_REQUIRED", "virtual employee accounts cannot be removed directly; use force delete with admin password")
			return
		}

		result, err := purger.PurgeAll(r.Context(), user)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "USER_NOT_DELETED", "User was not deleted; tenant_id and email did not match a stored user")
				return
			}
			writeError(w, http.StatusInternalServerError, "DELETE_USER_FAILED", err.Error())
			return
		}
		resp := map[string]any{
			"ok":                       true,
			"tenant_id":                user.TenantID,
			"email":                    user.Email,
			"deleted_machines":         result.DeletedMachines,
			"deleted_invitation_codes": result.DeletedInvitationCodes,
		}
		if result.RouteDeleteWarning != "" {
			resp["route_delete_warning"] = result.RouteDeleteWarning
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func ForceDeleteVirtualBoundUserHandler(admins *auth.AdminService, identity *auth.IdentityService, purger *UserDataPurger, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if admins == nil {
			writeError(w, http.StatusServiceUnavailable, "ADMIN_AUTH_UNAVAILABLE", "admin authentication is unavailable")
			return
		}
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		var req ForceDeleteVirtualBoundUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if email == "" {
			email = strings.TrimSpace(req.Email)
		}
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		if strings.TrimSpace(req.AdminPassword) == "" {
			writeError(w, http.StatusBadRequest, "ADMIN_PASSWORD_REQUIRED", "admin_password is required")
			return
		}
		admin := AdminFromContext(r.Context())
		if admin == nil || strings.TrimSpace(admin.Username) == "" {
			writeError(w, http.StatusForbidden, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}
		scopeTenantID := auth.ExplicitGlobalAdminTenantScope
		if adminHasTenantScope(admin) {
			scopeTenantID = AdminTenantID(r.Context())
		}
		if _, err := admins.VerifyScopedCredentials(r.Context(), admin.Username, req.AdminPassword, scopeTenantID); err != nil {
			writeError(w, http.StatusUnauthorized, "INVALID_ADMIN_PASSWORD", "admin password is incorrect")
			return
		}
		if identity == nil || identity.UsersRepo() == nil {
			writeError(w, http.StatusInternalServerError, "USER_DELETE_UNAVAILABLE", "User repository is unavailable")
			return
		}
		user, err := resolveBoundUserForDelete(r, identity.UsersRepo(), tenantID, strings.ToLower(strings.TrimSpace(email)), "", "")
		if err != nil {
			if err == errAmbiguousTenantEmail || err == errAmbiguousTenantIdentity {
				writeError(w, http.StatusBadRequest, "TENANT_ID_REQUIRED", "tenant_id is required when the identity exists in multiple tenants")
				return
			}
			writeError(w, http.StatusInternalServerError, "LOOKUP_USER_FAILED", err.Error())
			return
		}
		if user == nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}
		if !boundUserIsVirtualEmployee(r.Context(), system, user) {
			writeError(w, http.StatusBadRequest, "NOT_VIRTUAL_USER", "force delete is only available for virtual employee accounts")
			return
		}
		result, err := purger.PurgeAll(r.Context(), user)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "USER_NOT_DELETED", "User was not deleted; tenant_id and email did not match a stored user")
				return
			}
			writeError(w, http.StatusInternalServerError, "DELETE_USER_FAILED", err.Error())
			return
		}
		resp := map[string]any{
			"ok":                       true,
			"tenant_id":                user.TenantID,
			"email":                    user.Email,
			"deleted_machines":         result.DeletedMachines,
			"deleted_invitation_codes": result.DeletedInvitationCodes,
			"forced":                   true,
		}
		if result.RouteDeleteWarning != "" {
			resp["route_delete_warning"] = result.RouteDeleteWarning
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func boundUserIsVirtualEmployee(ctx context.Context, system store.SystemSettingsRepository, user *store.User) bool {
	if system == nil || user == nil {
		return false
	}
	tenantID := store.NormalizeTenantID(user.TenantID)
	_, excludedEmails := platformEmployeeAccountExclusions(ctx, system, tenantID)
	_, ok := excludedEmails[strings.ToLower(strings.TrimSpace(user.Email))]
	return ok
}

var errAmbiguousTenantEmail = errors.New("email exists in multiple tenants")
var errAmbiguousTenantIdentity = errors.New("identity exists in multiple tenants")

func resolveBoundUserForDelete(r *http.Request, users store.UserRepository, tenantID, email, userID, phone string) (*store.User, error) {
	if r == nil || users == nil {
		return nil, nil
	}
	if userID = strings.TrimSpace(userID); userID != "" {
		user, err := users.GetByID(r.Context(), userID)
		if err != nil || user == nil {
			return user, err
		}
		if admin := AdminFromContext(r.Context()); adminHasTenantScope(admin) && store.NormalizeTenantID(user.TenantID) != AdminTenantID(r.Context()) {
			return nil, nil
		}
		if tenantID != "" && store.NormalizeTenantID(user.TenantID) != store.NormalizeTenantID(tenantID) {
			return nil, nil
		}
		if !boundUserDeleteUserMatchesFilters(r.Context(), users, user, email, phone) {
			return nil, nil
		}
		return user, nil
	}
	if admin := AdminFromContext(r.Context()); adminHasTenantScope(admin) {
		tenantID := AdminTenantID(r.Context())
		if email != "" {
			return users.GetByTenantEmail(r.Context(), tenantID, email)
		}
		return users.GetByTenantIdentity(r.Context(), tenantID, "phone", phone)
	}
	if tenantID != "" {
		if email != "" {
			return users.GetByTenantEmail(r.Context(), tenantID, email)
		}
		return users.GetByTenantIdentity(r.Context(), tenantID, "phone", phone)
	}
	items, err := users.List(r.Context())
	if err != nil {
		return nil, err
	}
	var matched *store.User
	for _, item := range items {
		if item == nil || !boundUserDeleteCandidateMatches(r.Context(), users, item, email, phone) {
			continue
		}
		if matched != nil && store.NormalizeTenantID(matched.TenantID) != store.NormalizeTenantID(item.TenantID) {
			if email != "" {
				return nil, errAmbiguousTenantEmail
			}
			return nil, errAmbiguousTenantIdentity
		}
		matched = item
	}
	return matched, nil
}

func boundUserDeleteCandidateMatches(ctx context.Context, users store.UserRepository, user *store.User, email, phone string) bool {
	if user == nil {
		return false
	}
	if email != "" && strings.EqualFold(strings.TrimSpace(user.Email), strings.TrimSpace(email)) {
		return true
	}
	phone = normalizePurgePhoneIdentity(phone)
	if phone == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(user.Email), "phone:"+phone) {
		return true
	}
	rows, err := users.ListIdentitiesByUser(ctx, store.NormalizeTenantID(user.TenantID), user.ID)
	if err != nil {
		log.Printf("[admin/users] ListIdentitiesByUser failed while resolving delete for tenant=%s user=%s: %v", user.TenantID, user.ID, err)
		return false
	}
	for _, row := range rows {
		if row != nil && strings.EqualFold(strings.TrimSpace(row.Type), "phone") && normalizePurgePhoneIdentity(row.Value) == phone {
			return true
		}
	}
	return false
}

func boundUserDeleteUserMatchesFilters(ctx context.Context, users store.UserRepository, user *store.User, email, phone string) bool {
	email = strings.TrimSpace(email)
	phone = normalizePurgePhoneIdentity(phone)
	if email == "" && phone == "" {
		return true
	}
	emailMatched := email == ""
	phoneMatched := phone == ""
	emailMatched = emailMatched || strings.EqualFold(strings.TrimSpace(user.Email), email)
	phoneMatched = phoneMatched || strings.EqualFold(strings.TrimSpace(user.Email), "phone:"+phone)
	if emailMatched && phoneMatched {
		return true
	}
	rows, err := users.ListIdentitiesByUser(ctx, store.NormalizeTenantID(user.TenantID), user.ID)
	if err != nil {
		log.Printf("[admin/users] ListIdentitiesByUser failed while validating delete for tenant=%s user=%s: %v", user.TenantID, user.ID, err)
		return false
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(row.Type)) {
		case "email":
			if email != "" && strings.EqualFold(strings.TrimSpace(row.Value), email) {
				emailMatched = true
			}
		case "phone":
			if phone != "" && normalizePurgePhoneIdentity(row.Value) == phone {
				phoneMatched = true
			}
		}
		if emailMatched && phoneMatched {
			return true
		}
	}
	return emailMatched && phoneMatched
}
func ListBlockedEmailsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListBlockedEmails(auth.WithTenant(r.Context(), RequestTenantID(r)))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_BLOCKED_EMAILS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocked_emails": items})
	}
}

func AddBlockedEmailHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BlockEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		if err := identity.AddBlockedEmail(auth.WithTenant(r.Context(), RequestTenantID(r)), req.Email, req.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "ADD_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func RemoveBlockedEmailHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.PathValue("email")
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		if err := identity.RemoveBlockedEmail(auth.WithTenant(r.Context(), RequestTenantID(r)), email); err != nil {
			writeError(w, http.StatusInternalServerError, "REMOVE_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func LookupUserHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		mobile := r.URL.Query().Get("mobile")
		if email == "" && mobile == "" {
			var req LookupUserRequest
			if r.Method != http.MethodGet {
				if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
					email = req.Email
					mobile = req.Mobile
				}
			}
		}

		var (
			user *store.User
			err  error
		)

		switch {
		case mobile != "":
			user, err = identity.LookupUserByMobile(auth.WithTenant(r.Context(), RequestTenantID(r)), mobile)
		case email != "":
			user, err = identity.LookupUserByEmail(auth.WithTenant(r.Context(), RequestTenantID(r)), email)
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email or mobile is required")
			return
		}

		if err != nil {
			if err == auth.ErrInvalidEmail {
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "LOOKUP_USER_FAILED", err.Error())
			return
		}
		if user == nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user": map[string]any{
				"id":        user.ID,
				"tenant_id": user.TenantID,
				"email":     user.Email,
				"sn":        user.SN,
				"status":    user.Status,
			},
		})
	}
}

func ListUsersHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			items []*store.User
			err   error
		)
		if IsGlobalAdmin(r.Context()) && strings.TrimSpace(r.URL.Query().Get("tenant_id")) == "" {
			items, err = identity.ListUsers(r.Context())
		} else {
			items, err = identity.ListUsersForTenant(r.Context(), RequestTenantID(r))
		}
		if err != nil {
			log.Printf("[admin/users] ListUsers failed: %v", err)
			writeError(w, http.StatusInternalServerError, "LIST_USERS_FAILED", err.Error())
			return
		}
		identityRows := preloadBoundUserIdentities(r.Context(), identity.UsersRepo(), items)
		out := make([]BoundUserView, 0, len(items))
		seenUsers := make(map[string]struct{}, len(items))
		virtualEmailCache := map[string]map[string]struct{}{}
		for _, user := range items {
			if user == nil {
				continue
			}
			emailKey := strings.TrimSpace(strings.ToLower(user.Email))
			tenantID := store.NormalizeTenantID(user.TenantID)
			userIdentityRows := identityRows[boundUserIdentityKey(tenantID, user.ID)]
			emails, primaryPhone, phones, identities := boundUserContactFields(user, userIdentityRows)
			if emailKey == "" && len(emails) == 0 && len(phones) == 0 {
				continue
			}
			seenKey := tenantID + "\x00" + strings.TrimSpace(user.ID)
			if strings.TrimSpace(user.ID) == "" {
				seenKey = tenantID + "\x00" + emailKey
			}
			if _, exists := seenUsers[seenKey]; exists {
				continue
			}
			seenUsers[seenKey] = struct{}{}
			accountType := "physical_employee"
			isVirtualEmployee := false
			if system != nil {
				excludedEmails, ok := virtualEmailCache[tenantID]
				if !ok {
					_, excludedEmails = platformEmployeeAccountExclusions(r.Context(), system, tenantID)
					virtualEmailCache[tenantID] = excludedEmails
				}
				if _, ok := excludedEmails[emailKey]; ok {
					accountType = "virtual_employee"
					isVirtualEmployee = true
				}
			}
			var serviceStatus *llmservice.ServiceStatus
			if system != nil {
				tenantSystem := ScopedSystemSettingsForTenant(tenantID, system)
				serviceStatus, _ = llmservice.ResolveServiceStatusForUserID(r.Context(), tenantSystem, securitySvc, user.ID, user.Email, externalLLMBaseURL(r))
			}
			out = append(out, BoundUserView{
				ID:                user.ID,
				TenantID:          tenantID,
				Email:             user.Email,
				Emails:            emails,
				Phone:             primaryPhone,
				Phones:            phones,
				Identities:        identities,
				SN:                user.SN,
				Status:            user.Status,
				EnrollmentStatus:  user.EnrollmentStatus,
				AccountType:       accountType,
				IsVirtualEmployee: isVirtualEmployee,
				SmartRoute:        user.SmartRoute,
				EmailVerified:     boundUserEmailVerified(user, identities),
				HasServiceAccess:  serviceStatus != nil && serviceStatus.Active,
				ServiceStatus:     serviceStatus,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": out})
	}
}

func GetCenterStatusHandler(centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := centerSvc.RefreshStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_STATUS_FAILED", err.Error())
			return
		}
		filterCenterStatusForTenantAdmin(r, status)
		writeJSON(w, http.StatusOK, status)
	}
}

func UpdateCenterConfigHandler(centerSvc *center.Service, identity *auth.IdentityService, onPublicBaseURLChanged ...func(string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGlobalAdminForHubCenter(w, r) {
			return
		}
		var req CenterConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.BaseURL == "" && req.PublicBaseURL == "" && req.Visibility == "" && req.EnrollmentMode == "" && req.CorporateEmailDomain == nil && len(req.CorporateEmailDomains) == 0 && req.AcceptPublicSignup == nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Base URL, public base URL, visibility, enrollment mode, corporate email domains, or public signup setting is required")
			return
		}
		var (
			status *center.RegistrationState
			err    error
		)
		if req.BaseURL != "" {
			status, err = centerSvc.SetBaseURL(r.Context(), req.BaseURL)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.PublicBaseURL != "" {
			status, err = centerSvc.SetPublicBaseURL(r.Context(), req.PublicBaseURL)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
			// Notify IM plugins so temp-file download URLs use the new domain.
			for _, fn := range onPublicBaseURLChanged {
				fn(status.PublicBaseURL)
			}
		}
		if req.Visibility != "" {
			status, err = centerSvc.SetVisibility(r.Context(), req.Visibility)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.EnrollmentMode != "" {
			status, err = centerSvc.SetEnrollmentMode(r.Context(), req.EnrollmentMode)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
			if err := identity.SetEnrollmentMode(r.Context(), req.EnrollmentMode); err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.CorporateEmailDomain != nil {
			status, err = centerSvc.SetCorporateEmailDomain(r.Context(), *req.CorporateEmailDomain)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if len(req.CorporateEmailDomains) > 0 {
			status, err = centerSvc.SetCorporateEmailDomains(r.Context(), req.CorporateEmailDomains)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.AcceptPublicSignup != nil {
			status, err = centerSvc.SetAcceptPublicSignup(r.Context(), *req.AcceptPublicSignup)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		status, err = centerSvc.RefreshStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func RegisterCenterHandler(centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGlobalAdminForHubCenter(w, r) {
			return
		}
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		status, err := centerSvc.Register(r.Context(), admin.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func filterCenterStatusForTenantAdmin(r *http.Request, status *center.RegistrationState) {
	if r == nil || status == nil || AdminFromContext(r.Context()) == nil || IsGlobalAdmin(r.Context()) {
		if r == nil || status == nil || AdminFromContext(r.Context()) == nil || !IsGlobalAdmin(r.Context()) {
			return
		}
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if tenantID == "" || status.DigitalEmployeeAuthorizations == nil {
			return
		}
		status.DigitalEmployeeAuthorization = centerStatusAuthorizationForTenant(status, tenantID)
		return
	}
	tenantID := AdminTenantID(r.Context())
	status.DigitalEmployeeAuthorization = centerStatusAuthorizationForTenant(status, tenantID)
	status.DigitalEmployeeAuthorizations = nil
}

func centerStatusAuthorizationForTenant(status *center.RegistrationState, tenantID string) *corelib.DigitalEmployeeAuthorization {
	tenantID = strings.TrimSpace(tenantID)
	if status == nil || tenantID == "" {
		return nil
	}
	if status.DigitalEmployeeAuthorizations != nil {
		if authz := status.DigitalEmployeeAuthorizations[tenantID]; authz != nil {
			return authz
		}
	}
	if tenantID == store.DefaultTenantID {
		return status.DigitalEmployeeAuthorization
	}
	return nil
}

func requireGlobalAdminForHubCenter(w http.ResponseWriter, r *http.Request) bool {
	if r == nil || AdminFromContext(r.Context()) == nil || IsGlobalAdmin(r.Context()) {
		return true
	}
	writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Hub Center registration is managed by the Hub global administrator")
	return false
}

// --- Smart Route permission handlers ---

const smartRouteAllKey = "smart_route_all"

// UpdateUserSmartRouteHandler toggles the smart_route flag for a single user.
func UpdateUserSmartRouteHandler(users store.UserRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID  string `json:"user_id"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.UserID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "user_id is required")
			return
		}
		if !IsGlobalAdmin(r.Context()) || strings.TrimSpace(r.URL.Query().Get("tenant_id")) != "" {
			user, err := users.GetByID(r.Context(), req.UserID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LOOKUP_USER_FAILED", err.Error())
				return
			}
			if user == nil || strings.TrimSpace(user.TenantID) != RequestTenantID(r) {
				writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
				return
			}
		}
		if err := users.UpdateSmartRoute(r.Context(), req.UserID, req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_SMART_ROUTE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// GetSmartRouteAllHandler returns the global smart_route_all toggle.
func GetSmartRouteAllHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		raw, _ := system.Get(r.Context(), smartRouteAllKey)
		enabled := raw == "true"
		writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
	}
}

// UpdateSmartRouteAllHandler sets the global smart_route_all toggle.
func UpdateSmartRouteAllHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		val := "false"
		if req.Enabled {
			val = "true"
		}
		if err := system.Set(r.Context(), smartRouteAllKey, val); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_SMART_ROUTE_ALL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": req.Enabled})
	}
}
