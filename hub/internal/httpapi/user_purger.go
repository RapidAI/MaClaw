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
// the system. All cleanup steps are best-effort — failures are logged but do not
// block subsequent steps or the final user record deletion.
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
	if p.InvitationSvc != nil && email != "" {
		deleted, err := p.InvitationSvc.DeleteCodeByTenantEmail(ctx, tenantID, email)
		logErr("invitation_codes", err)
		result.DeletedInvitationCodes = deleted
	}

	// 3. Feishu IM binding.
	if p.FeishuNotify != nil && email != "" {
		p.FeishuNotify.RemoveOpenIDForTenant(tenantID, email)
	}

	// 4. Other IM bindings (QQ, WeChat, DingTalk).
	removeIMBindingsForTenant(p.IMCleaners, tenantID, email)

	// 5. Enrollment records.
	if p.Identity != nil {
		if repo := p.Identity.EnrollmentsRepo(); repo != nil && email != "" {
			_, err := repo.DeleteByTenantEmail(ctx, tenantID, email)
			logErr("enrollments", err)
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
	if p.SecuritySvc != nil && email != "" {
		tenantCtx := security.WithTenant(ctx, tenantID)
		err := p.SecuritySvc.RemoveUser(tenantCtx, "", email)
		logErr("security_group", err)
	}

	// 8. LLM service: user bindings, grants, redeemed cards.
	if p.System != nil && (userID != "" || email != "") {
		tenantSystem := ScopedSystemSettingsForTenant(tenantID, p.System)
		err := llmservice.PurgeUserFromRegistryForUser(ctx, tenantSystem, userID, email)
		logErr("llm_service_registry", err)
	}

	// ─── Phase 2: Direct SQL cleanup (tables without repo delete methods) ──

	if p.DB != nil {
		// 9. Login tokens.
		if email != "" {
			_, err := p.DB.ExecContext(ctx, `DELETE FROM login_tokens WHERE tenant_id = ? AND lower(email) = lower(?)`, tenantID, email)
			logErr("login_tokens", err)
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
		if email != "" {
			_, err := p.DB.ExecContext(ctx, `DELETE FROM user_capability_inventory WHERE tenant_id = ? AND lower(user_email) = lower(?)`, tenantID, email)
			logErr("user_capability_inventory", err)
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
	}

	// ─── Phase 3: User record deletion ───────────────────────────────────

	if p.Identity != nil && p.Identity.UsersRepo() != nil {
		if err := p.Identity.UsersRepo().DeleteByTenantEmail(ctx, tenantID, email); err != nil {
			return result, err
		}
	}

	// ─── Phase 4: External system notification (post-delete) ─────────────

	// 15. Hub Center route removal.
	if p.RouteDeleter != nil && email != "" {
		if err := p.RouteDeleter.DeleteUserRoute(ctx, email, tenantID); err != nil {
			result.RouteDeleteWarning = err.Error()
		}
	}

	return result, nil
}
