package httpapi

import (
	"context"
	"database/sql"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// UserDataPurger provides a single entry point for removing all user data from
// the system. Most cleanup steps are best-effort, but local identity cleanup is
// required because stale identities would make admin user cards and login lookup
// appear bound after deletion.
//
// Adding a new user-linked table? Add a step to PurgeAll. This is the single
// source of truth for "what data does a user leave behind".
type UserDataPurger struct {
	Identity      *auth.IdentityService
	DeviceSvc     *device.Service
	InvitationSvc *invitation.Service
	FeishuNotify  *feishu.Notifier
	IMCleaners    []IMBindingCleaner
	SecuritySvc   *security.SecurityService
	System        store.SystemSettingsRepository
	DB            *sql.DB
	RouteDeleter  BoundUserRouteDeleter
}

// PurgeResult reports what was cleaned up (for response/logging).
type PurgeResult struct {
	DeletedMachines        int64
	DeletedInvitationCodes int64
	RouteDeleteWarning     string
}

// PurgeAll removes all data associated with the user from the system.
// The user record itself is deleted last. If user record deletion fails, an error is returned.
// All other steps are best-effort.
func (p *UserDataPurger) PurgeAll(ctx context.Context, user *store.User) (*PurgeResult, error) {
	if user == nil {
		return &PurgeResult{}, nil
	}
	result := &PurgeResult{}
	tenantID := store.NormalizeTenantID(user.TenantID)
	email := strings.TrimSpace(strings.ToLower(user.Email))
	userID := user.ID
	cleanupEmails, routeIdentities := collectUserPurgeIdentities(ctx, p.Identity, tenantID, user)

	logErr := func(area string, err error) {
		if err != nil {
			log.Printf("[user-purge] %s for %s (%s): %v", area, email, userID, err)
		}
	}

	// ─── Phase 1: Service-level cleanup (typed interfaces) ───────────────

	// 1. Machines (requires user ID for lookup).
	if p.DeviceSvc != nil && userID != "" {
		deleted, err := p.DeviceSvc.ForceDeleteMachinesByTenantUser(ctx, tenantID, userID)
		logErr("machines", err)
		result.DeletedMachines = deleted
	}

	// 2. Invitation codes bound to email.
	if p.InvitationSvc != nil && len(cleanupEmails) > 0 {
		for _, cleanupEmail := range cleanupEmails {
			deleted, err := p.InvitationSvc.DeleteCodeByTenantEmail(ctx, tenantID, cleanupEmail)
			logErr("invitation_codes", err)
			result.DeletedInvitationCodes += deleted
		}
	}

	// 3. Feishu IM binding.
	if p.FeishuNotify != nil && len(cleanupEmails) > 0 {
		for _, cleanupEmail := range cleanupEmails {
			p.FeishuNotify.RemoveOpenIDForTenant(tenantID, cleanupEmail)
		}
	}

	// 4. Other IM bindings (QQ, WeChat, DingTalk).
	for _, cleanupEmail := range cleanupEmails {
		removeIMBindingsForTenant(p.IMCleaners, tenantID, cleanupEmail)
	}

	// 5. Enrollment records.
	if p.Identity != nil {
		if repo := p.Identity.EnrollmentsRepo(); repo != nil && len(cleanupEmails) > 0 {
			for _, cleanupEmail := range cleanupEmails {
				_, err := repo.DeleteByTenantEmail(ctx, tenantID, cleanupEmail)
				logErr("enrollments", err)
			}
		}
	}

	// 6. Viewer tokens.
	if p.Identity != nil && userID != "" {
		if repo := p.Identity.ViewerTokensRepo(); repo != nil {
			_, err := repo.DeleteByUserID(ctx, userID)
			logErr("viewer_tokens", err)
		}
	}

	// 7. Security group membership (department).
	if p.SecuritySvc != nil && len(cleanupEmails) > 0 {
		tenantCtx := security.WithTenant(ctx, tenantID)
		for _, cleanupEmail := range cleanupEmails {
			err := p.SecuritySvc.RemoveUser(tenantCtx, "", cleanupEmail)
			logErr("security_group", err)
		}
	}

	// 8. LLM service: user bindings, grants, redeemed cards.
	if p.System != nil && (userID != "" || email != "") {
		tenantSystem := ScopedSystemSettingsForTenant(tenantID, p.System)
		err := llmservice.PurgeUserFromRegistryForUser(ctx, tenantSystem, userID, email)
		logErr("llm_service_registry", err)
		for _, cleanupEmail := range cleanupEmails {
			if cleanupEmail == email {
				continue
			}
			err := llmservice.PurgeUserFromRegistryForUser(ctx, tenantSystem, "", cleanupEmail)
			logErr("llm_service_registry", err)
		}
	}

	// ─── Phase 2: Direct SQL cleanup (tables without repo delete methods) ──

	if p.DB != nil {
		// 9. Login tokens.
		if len(cleanupEmails) > 0 {
			for _, cleanupEmail := range cleanupEmails {
				_, err := p.DB.ExecContext(ctx, `DELETE FROM login_tokens WHERE tenant_id = ? AND lower(email) = lower(?)`, tenantID, cleanupEmail)
				logErr("login_tokens", err)
			}
		}
		// 10. MCP secret bindings.
		if userID != "" {
			_, err := p.DB.ExecContext(ctx, `DELETE FROM mcp_secret_bindings WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
			logErr("mcp_secret_bindings", err)
		}
		// 11. MCP hub-managed secrets.
		if userID != "" {
			_, err := p.DB.ExecContext(ctx, `DELETE FROM mcp_hub_secrets WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
			logErr("mcp_hub_secrets", err)
		}
		// 12. User capability inventory.
		if len(cleanupEmails) > 0 {
			for _, cleanupEmail := range cleanupEmails {
				_, err := p.DB.ExecContext(ctx, `DELETE FROM user_capability_inventory WHERE tenant_id = ? AND lower(user_email) = lower(?)`, tenantID, cleanupEmail)
				logErr("user_capability_inventory", err)
			}
		}
		// 13. Workflow intent sessions.
		if userID != "" {
			_, err := p.DB.ExecContext(ctx, `DELETE FROM understanding_sessions WHERE user_id = ?`, userID)
			logErr("understanding_sessions", err)
		}
		// 14. Workflow states.
		if userID != "" {
			_, err := p.DB.ExecContext(ctx, `DELETE FROM workflow_states WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
			logErr("workflow_states", err)
		}
		// 15. User identities. These are the local email/phone bindings shown on
		// admin user cards, and the users table does not cascade-delete them.
		if userID != "" {
			if _, err := p.DB.ExecContext(ctx, `DELETE FROM user_identities WHERE tenant_id = ? AND user_id = ?`, tenantID, userID); err != nil {
				logErr("user_identities", err)
				return result, err
			}
		}
	}

	// ─── Phase 3: User record deletion ───────────────────────────────────

	if p.DB != nil && userID != "" {
		res, err := p.DB.ExecContext(ctx, `DELETE FROM users WHERE tenant_id = ? AND id = ?`, tenantID, userID)
		if err != nil {
			return result, err
		}
		if affected, err := res.RowsAffected(); err == nil && affected == 0 {
			return result, sql.ErrNoRows
		}
	} else if p.Identity != nil && p.Identity.UsersRepo() != nil {
		if err := p.Identity.UsersRepo().DeleteByTenantEmail(ctx, tenantID, email); err != nil {
			return result, err
		}
	}

	// ─── Phase 4: External system notification (post-delete) ─────────────

	// 16. Hub Center route removal.
	if p.RouteDeleter != nil {
		var warnings []string
		for _, routeIdentity := range routeIdentities {
			if err := p.RouteDeleter.DeleteUserRoute(ctx, routeIdentity, tenantID); err != nil {
				warnings = append(warnings, routeIdentity+": "+err.Error())
			}
		}
		if len(warnings) > 0 {
			result.RouteDeleteWarning = strings.Join(warnings, "; ")
		}
	}

	return result, nil
}

func collectUserPurgeIdentities(ctx context.Context, identity *auth.IdentityService, tenantID string, user *store.User) ([]string, []string) {
	var emails []string
	var routeIdentities []string
	seenEmails := map[string]struct{}{}
	seenRoutes := map[string]struct{}{}

	addEmail := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" || strings.HasPrefix(value, "phone:") {
			return
		}
		if _, ok := seenEmails[value]; !ok {
			seenEmails[value] = struct{}{}
			emails = append(emails, value)
		}
		if _, ok := seenRoutes[value]; !ok {
			seenRoutes[value] = struct{}{}
			routeIdentities = append(routeIdentities, value)
		}
	}
	addPhone := func(value string) {
		value = normalizePurgePhoneIdentity(value)
		if value == "" {
			return
		}
		routeIdentity := "phone:" + value
		if _, ok := seenRoutes[routeIdentity]; ok {
			return
		}
		seenRoutes[routeIdentity] = struct{}{}
		routeIdentities = append(routeIdentities, routeIdentity)
	}

	account := strings.TrimSpace(user.Email)
	if strings.HasPrefix(strings.ToLower(account), "phone:") {
		addPhone(account)
	} else {
		addEmail(account)
	}

	if identity == nil || identity.UsersRepo() == nil || strings.TrimSpace(user.ID) == "" {
		return emails, routeIdentities
	}
	rows, err := identity.UsersRepo().ListIdentitiesByUser(ctx, tenantID, user.ID)
	if err != nil {
		log.Printf("[user-purge] list user identities for %s (%s): %v", user.Email, user.ID, err)
		return emails, routeIdentities
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(row.Type)) {
		case "email":
			addEmail(row.Value)
		case "phone":
			addPhone(row.Value)
		}
	}
	return emails, routeIdentities
}

func normalizePurgePhoneIdentity(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "phone:")
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
