package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/survey"
)

// UserDataPurger provides a single entry point for removing all user data from
// the system. Most cleanup steps are best-effort, but local identity cleanup is
// required because stale identities would make admin user cards and login lookup
// appear bound after deletion.
//
// Adding a new user-linked table? Add a step to PurgeAll. This is the single
// source of truth for "what data does a user leave behind".
type UserDataPurger struct {
	Identity           *auth.IdentityService
	DeviceSvc          *device.Service
	InvitationSvc      *invitation.Service
	FeishuNotify       *feishu.Notifier
	IMCleaners         []IMBindingCleaner
	SecuritySvc        *security.SecurityService
	System             store.SystemSettingsRepository
	DB                 *sql.DB
	RouteDeleter       BoundUserRouteDeleter
	GroupDiscussionSvc *GroupDiscussionService

	// Runtime data roots are kept here so every unlink path (self-service,
	// invitation-code and administrator deletion) removes the same on-disk data.
	KnowledgeSharePackageDir string
	KnowledgeSyncDir         string
	WelcomeSyncDir           string
	VirtualRepositorySyncDir string
	UserDataMigrationDir     string
	ChatFileDir              string
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

	// Mobile keeps several user-owned records outside SQLite so that document
	// uploads, SSH sessions and offline work can survive a Hub restart. Purge
	// those before deleting the identity; otherwise state.json can restore the
	// account's private data on the next restart.
	p.purgeMobileUserData(ctx, tenantID, userID, cleanupEmails, logErr)

	// 1. A2A collaboration state refers to machine IDs in both indexed columns
	// and session JSON. Clear it before removing the user's machines.
	p.deleteUserA2AData(ctx, tenantID, userID, logErr)

	// 2. Machines (requires user ID for lookup).
	if p.DeviceSvc != nil && userID != "" {
		deleted, err := p.DeviceSvc.ForceDeleteMachinesByTenantUser(ctx, tenantID, userID)
		logErr("machines", err)
		result.DeletedMachines = deleted
	} else if p.DB != nil && userID != "" {
		// Keep the purger complete for focused deployments/tests that do not wire
		// the device service. The normal router uses DeviceSvc above, which also
		// performs its runtime cleanup; this is a durable-data fallback.
		deleted, err := p.DB.ExecContext(ctx, `DELETE FROM machines WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		logErr("machines", err)
		if err == nil {
			result.DeletedMachines, _ = deleted.RowsAffected()
		}
	}

	// 3. Invitation codes bound to email.
	if p.InvitationSvc != nil && len(cleanupEmails) > 0 {
		for _, cleanupEmail := range cleanupEmails {
			deleted, err := p.InvitationSvc.DeleteCodeByTenantEmail(ctx, tenantID, cleanupEmail)
			logErr("invitation_codes", err)
			result.DeletedInvitationCodes += deleted
		}
	}

	// 4. Feishu IM binding.
	if p.FeishuNotify != nil && len(cleanupEmails) > 0 {
		for _, cleanupEmail := range cleanupEmails {
			p.FeishuNotify.RemoveOpenIDForTenant(tenantID, cleanupEmail)
		}
	}

	// 5. Other IM bindings (QQ, WeChat, DingTalk).
	for _, cleanupEmail := range cleanupEmails {
		removeIMBindingsForTenant(p.IMCleaners, tenantID, cleanupEmail)
	}

	// 6. Enrollment records.
	if p.Identity != nil {
		if repo := p.Identity.EnrollmentsRepo(); repo != nil && len(cleanupEmails) > 0 {
			for _, cleanupEmail := range cleanupEmails {
				_, err := repo.DeleteByTenantEmail(ctx, tenantID, cleanupEmail)
				logErr("enrollments", err)
			}
		}
	}

	// 7. Viewer tokens.
	if p.Identity != nil && userID != "" {
		if repo := p.Identity.ViewerTokensRepo(); repo != nil {
			_, err := repo.DeleteByUserID(ctx, userID)
			logErr("viewer_tokens", err)
		}
	}

	// 8. Security group membership (department).
	if p.SecuritySvc != nil && len(cleanupEmails) > 0 {
		tenantCtx := security.WithTenant(ctx, tenantID)
		for _, cleanupEmail := range cleanupEmails {
			err := p.SecuritySvc.RemoveUser(tenantCtx, "", cleanupEmail)
			logErr("security_group", err)
		}
	}

	// 9. Referral audit ledger. Freeze and revoke referral benefits before any
	// registry cleanup: the referral grants must be retained (frozen) for
	// audit, not silently deleted with the account's ordinary entitlements.
	// This must use the affected tenant's settings namespace; p.System is the
	// shared/global repository in normal router wiring.
	p.deleteUserReferrals(ctx, tenantID, userID, logErr)

	// 10. LLM service: ordinary user bindings, grants and redeemed cards.
	// Referral grants were frozen above and intentionally remain in the ledger.
	if p.System != nil && (userID != "" || email != "") {
		tenantSystem := ScopedSystemSettingsForTenant(tenantID, p.System)
		err := llmservice.PurgeUserFromRegistryExceptReferralBenefitsForUser(ctx, tenantSystem, userID, email)
		logErr("llm_service_registry", err)
		for _, cleanupEmail := range cleanupEmails {
			if cleanupEmail == email {
				continue
			}
			err := llmservice.PurgeUserFromRegistryExceptReferralBenefitsForUser(ctx, tenantSystem, "", cleanupEmail)
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
		// 15. Historical sessions, usage, content and operational records. These
		// records contain project paths, session previews and usage attribution, so
		// retaining them after an account deletion would leave user data behind.
		p.deleteUserSQL(ctx, tenantID, userID, cleanupEmails, logErr)
	}
	// Files use the canonical owner email/user ID paths, so clean them before
	// removing the identity record that lets callers resolve those paths.
	p.deleteUserFiles(tenantID, userID, cleanupEmails, logErr)

	// User identities are the final dependent records before the account. They
	// must be removed after all alias-aware cleanup has completed.
	if p.DB != nil && userID != "" {
		if _, err := p.DB.ExecContext(ctx, `DELETE FROM user_identities WHERE tenant_id = ? AND user_id = ?`, tenantID, userID); err != nil {
			logErr("user_identities", err)
			return result, err
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

// deleteUserSQL removes user-owned rows from tables which intentionally do not
// have repository delete methods. All statements are tenant-scoped whenever the
// table supports it. A failed optional cleanup is logged but does not prevent
// deleting the account, matching the established purger contract.
func (p *UserDataPurger) deleteUserSQL(ctx context.Context, tenantID, userID string, emails []string, logErr func(string, error)) {
	if p.DB == nil {
		return
	}
	exec := func(area, query string, args ...any) {
		_, err := p.DB.ExecContext(ctx, query, args...)
		logErr(area, err)
	}
	if userID != "" {
		exec("session_token_usage_snapshots", `DELETE FROM session_token_usage_snapshots WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		exec("sessions", `DELETE FROM sessions WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		exec("machine_heartbeat_log", `DELETE FROM machine_heartbeat_log WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		exec("digital_asset_sync_cursors", `DELETE FROM digital_asset_sync_cursors WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		exec("content_audit_logs", `DELETE FROM content_audit_logs WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		exec("audit_logs", `DELETE FROM audit_logs WHERE user_id = ?`, userID)
		exec("capability_acquisition_requests", `DELETE FROM capability_acquisition_requests WHERE tenant_id = ? AND requester_user_id = ?`, tenantID, userID)

		p.deleteUserSurveyData(ctx, tenantID, userID, logErr)

		// A workflow definition owns all its versions, instances and execution
		// records. Delete children explicitly because SQLite deployments may not
		// enable foreign-key cascades.
		workflowIDs := `SELECT id FROM workflow_definitions WHERE tenant_id = ? AND owner_id = ?`
		instanceIDs := `SELECT id FROM workflow_instances WHERE tenant_id = ? AND workflow_id IN (` + workflowIDs + `)`
		workflowArgs := []any{tenantID, tenantID, userID}
		instanceArgs := []any{tenantID, userID}
		exec("confirmations", `DELETE FROM confirmations WHERE tenant_id = ? AND instance_id IN (`+instanceIDs+`)`, append([]any{tenantID}, workflowArgs...)...)
		// approval_audit_trail is intentionally immutable (the database has a
		// no-delete trigger). It is retained as a system audit record, consistent
		// with the account-deletion audit retention policy.
		exec("node_executions", `DELETE FROM node_executions WHERE instance_id IN (`+instanceIDs+`)`, workflowArgs...)
		exec("workflow_instances", `DELETE FROM workflow_instances WHERE tenant_id = ? AND workflow_id IN (`+workflowIDs+`)`, append([]any{tenantID}, instanceArgs...)...)
		exec("workflow_versions", `DELETE FROM workflow_versions WHERE workflow_id IN (`+workflowIDs+`)`, instanceArgs...)
		exec("workflow_definitions", `DELETE FROM workflow_definitions WHERE tenant_id = ? AND owner_id = ?`, tenantID, userID)

		// Migration chunks must precede their export metadata; the corresponding
		// encrypted files are removed below.
		exec("user_data_migration_chunks", `DELETE FROM user_data_migration_chunks WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		exec("user_data_migration_exports", `DELETE FROM user_data_migration_exports WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)

	}
	// Chat content is stored in tables owned by the chat module rather than the
	// main migrations. The chat schema is optional in lightweight deployments,
	// therefore tolerate an unavailable table while retaining diagnostics.
	p.deleteUserChatData(ctx, tenantID, userID, logErr)
	for _, email := range emails {
		exec("failure_event_logs", `DELETE FROM failure_event_logs WHERE tenant_id = ? AND lower(email) = lower(?)`, tenantID, email)
		exec("email_invites", `DELETE FROM email_invites WHERE tenant_id = ? AND lower(email) = lower(?)`, tenantID, email)
		exec("user_usage_daily", `DELETE FROM user_usage_daily WHERE tenant_id = ? AND lower(user_email) = lower(?)`, tenantID, email)
		exec("gossip_comments", `DELETE FROM gossip_comments WHERE lower(user_email) = lower(?)`, email)
		// Remove comments on posts owned by this user as well. The schema has no
		// foreign-key cascade, so merely deleting the posts would orphan comments.
		exec("gossip_comments_for_posts", `DELETE FROM gossip_comments WHERE post_id IN (SELECT id FROM gossip_posts WHERE lower(user_email) = lower(?))`, email)
		exec("gossip_posts", `DELETE FROM gossip_posts WHERE lower(user_email) = lower(?)`, email)
	}
	if userID != "" {
		exec("user_usage_daily_by_id", `DELETE FROM user_usage_daily WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
		exec("user_capability_inventory_by_id", `DELETE FROM user_capability_inventory WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
	}
	for _, email := range emails {
		exec("user_capability_inventory", `DELETE FROM user_capability_inventory WHERE tenant_id = ? AND lower(user_email) = lower(?)`, tenantID, email)
	}

	// Knowledge shares are private user data. Unlike a user-initiated "delete"
	// action, account deletion physically removes both the metadata and package.
	p.deleteKnowledgeShares(ctx, tenantID, userID, emails, logErr)
}

func (p *UserDataPurger) deleteUserChatData(ctx context.Context, tenantID, userID string, logErr func(string, error)) {
	if p.DB == nil || userID == "" {
		return
	}
	p.deleteUserChatFiles(ctx, tenantID, userID, logErr)
	exec := func(area, query string, args ...any) {
		_, err := p.DB.ExecContext(ctx, query, args...)
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return
		}
		logErr(area, err)
	}
	ownedChannels := `SELECT id FROM chat_channels WHERE tenant_id = ? AND created_by = ?`
	ownedCalls := `SELECT id FROM chat_voice_calls WHERE tenant_id = ? AND caller_id = ?`
	exec("chat_voice_participants_owned_calls", `DELETE FROM chat_voice_participants WHERE tenant_id = ? AND call_id IN (`+ownedCalls+`)`, tenantID, tenantID, userID)
	exec("chat_voice_participants", `DELETE FROM chat_voice_participants WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
	exec("chat_voice_calls", `DELETE FROM chat_voice_calls WHERE tenant_id = ? AND caller_id = ?`, tenantID, userID)
	exec("chat_files_owned_channels", `DELETE FROM chat_files WHERE tenant_id = ? AND channel_id IN (`+ownedChannels+`)`, tenantID, tenantID, userID)
	exec("chat_messages_owned_channels", `DELETE FROM chat_messages WHERE tenant_id = ? AND channel_id IN (`+ownedChannels+`)`, tenantID, tenantID, userID)
	exec("chat_members_owned_channels", `DELETE FROM chat_members WHERE tenant_id = ? AND channel_id IN (`+ownedChannels+`)`, tenantID, tenantID, userID)
	exec("chat_channels", `DELETE FROM chat_channels WHERE tenant_id = ? AND created_by = ?`, tenantID, userID)
	exec("chat_files", `DELETE FROM chat_files WHERE tenant_id = ? AND uploader_id = ?`, tenantID, userID)
	exec("chat_messages", `DELETE FROM chat_messages WHERE tenant_id = ? AND sender_id = ?`, tenantID, userID)
	exec("chat_members", `DELETE FROM chat_members WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
	exec("chat_push_tokens", `DELETE FROM chat_push_tokens WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
	exec("chat_presence", `DELETE FROM chat_presence WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
}

// deleteUserSurveyData removes the user's in-progress answers, submitted
// responses, and surveys they created. Anonymous response keys are deterministic
// HMACs of the user ID, so they can be derived without retaining an identity map.
func (p *UserDataPurger) deleteUserSurveyData(ctx context.Context, tenantID, userID string, logErr func(string, error)) {
	if p.DB == nil || userID == "" {
		return
	}
	optional := func(area string, err error) bool {
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return true
		}
		logErr(area, err)
		return err != nil
	}
	ownedSurveys := `SELECT id FROM surveys WHERE tenant_id = ? AND created_by = ?`
	if _, err := p.DB.ExecContext(ctx, `DELETE FROM survey_sessions WHERE tenant_id = ? AND (user_id = ? OR survey_id IN (`+ownedSurveys+`))`, tenantID, userID, tenantID, userID); optional("survey_sessions", err) {
		return
	}

	rows, err := p.DB.QueryContext(ctx, `SELECT id, settings_json FROM surveys WHERE tenant_id = ?`, tenantID)
	if optional("surveys_list", err) {
		return
	}
	type surveyRow struct {
		id       string
		settings survey.Settings
	}
	var surveys []surveyRow
	for rows.Next() {
		var surveyID, settingsJSON string
		if err := rows.Scan(&surveyID, &settingsJSON); err != nil {
			logErr("surveys_scan", err)
			continue
		}
		var settings survey.Settings
		if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
			logErr("surveys_settings", err)
			continue
		}
		surveys = append(surveys, surveyRow{id: surveyID, settings: settings})
	}
	if err := rows.Close(); err != nil {
		logErr("surveys_close", err)
	}
	for _, row := range surveys {
		respondentKey, err := survey.ComputeRespondentKey(row.settings.Anonymous, row.settings.AnonymitySalt, userID)
		if err != nil {
			logErr("survey_respondent_key", err)
			continue
		}
		if _, err := p.DB.ExecContext(ctx, `DELETE FROM survey_responses WHERE tenant_id = ? AND survey_id = ? AND respondent_key = ?`, tenantID, row.id, respondentKey); optional("survey_responses", err) {
			return
		}
	}

	queries := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM survey_responses WHERE tenant_id = ? AND survey_id IN (` + ownedSurveys + `)`, []any{tenantID, tenantID, userID}},
		{`DELETE FROM survey_sessions WHERE tenant_id = ? AND survey_id IN (` + ownedSurveys + `)`, []any{tenantID, tenantID, userID}},
		{`DELETE FROM survey_bindings WHERE survey_id IN (` + ownedSurveys + `)`, []any{tenantID, userID}},
		{`DELETE FROM survey_questions WHERE survey_id IN (` + ownedSurveys + `)`, []any{tenantID, userID}},
		{`DELETE FROM surveys WHERE tenant_id = ? AND created_by = ?`, []any{tenantID, userID}},
	}
	for _, item := range queries {
		if _, err := p.DB.ExecContext(ctx, item.query, item.args...); optional("user_surveys", err) {
			return
		}
	}
}

// deleteUserChatFiles removes attachments before their metadata rows are
// deleted. Chat file names are generated as <file-id><extension>, so matching
// the exact ID or its dot-delimited extension cannot affect another upload.
func (p *UserDataPurger) deleteUserChatFiles(ctx context.Context, tenantID, userID string, logErr func(string, error)) {
	if p.DB == nil || strings.TrimSpace(p.ChatFileDir) == "" || userID == "" {
		return
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT id FROM chat_files
		WHERE tenant_id = ? AND (
			uploader_id = ? OR channel_id IN (
				SELECT id FROM chat_channels WHERE tenant_id = ? AND created_by = ?
			)
		)`, tenantID, userID, tenantID, userID)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logErr("chat_files_list", err)
		}
		return
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			logErr("chat_files_scan", err)
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		logErr("chat_files_rows", err)
		return
	}
	entries, err := os.ReadDir(p.ChatFileDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logErr("chat_files_readdir", err)
		}
		return
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || filepath.Base(id) != id || id == "." {
			logErr("chat_file_id", &os.PathError{Op: "validate", Path: id, Err: os.ErrInvalid})
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || (entry.Name() != id && !strings.HasPrefix(entry.Name(), id+".")) {
				continue
			}
			if err := os.Remove(filepath.Join(p.ChatFileDir, entry.Name())); err != nil && !os.IsNotExist(err) {
				logErr("chat_file", err)
			}
		}
	}
}

// deleteUserReferrals clears only short-lived invitation artifacts. Durable
// attribution, grant references, referral codes and their status history are
// an audit ledger, not disposable profile data. On user deletion we revoke the
// affected links and freeze every issued referral benefit before recording the
// terminal transition. This makes a deleted account unable to receive further
// invitations without erasing the evidence required to investigate abuse or
// account deletion later.
func (p *UserDataPurger) deleteUserReferrals(ctx context.Context, tenantID, userID string, logErr func(string, error)) {
	if p.DB == nil || userID == "" {
		return
	}
	type referralForPurge struct {
		id             string
		status         string
		inviterGrantID string
		inviteeGrantID string
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT id, status, inviter_grant_id, invitee_grant_id
		FROM user_referrals WHERE tenant_id = ? AND (inviter_user_id = ? OR invitee_user_id = ?)
		ORDER BY id ASC`, tenantID, userID, userID)
	if err != nil {
		logErr("user_referrals_list", err)
		return
	}
	var referrals []referralForPurge
	for rows.Next() {
		var referral referralForPurge
		if err := rows.Scan(&referral.id, &referral.status, &referral.inviterGrantID, &referral.inviteeGrantID); err != nil {
			_ = rows.Close()
			logErr("user_referrals_scan", err)
			return
		}
		referrals = append(referrals, referral)
	}
	if err := rows.Close(); err != nil {
		logErr("user_referrals_close", err)
		return
	}
	if err := rows.Err(); err != nil {
		logErr("user_referrals_list", err)
		return
	}

	// Do not mark a referral revoked until its Credits are safely frozen. A
	// non-empty grant ID without a settings repository is exceptional, but
	// retaining the prior status is safer than presenting an audit record as
	// revoked while spendable Credits remain in the registry.
	revokable := make([]referralForPurge, 0, len(referrals))
	for _, referral := range referrals {
		hasGrant := strings.TrimSpace(referral.inviterGrantID) != "" || strings.TrimSpace(referral.inviteeGrantID) != ""
		if hasGrant {
			if p.System == nil {
				logErr("user_referral_freeze", fmt.Errorf("cannot freeze referral %s: system settings unavailable", referral.id))
				continue
			}
			// The router stores a shared settings repository on the purger. Scope
			// it here so referral IDs can never freeze a same-named grant in the
			// default tenant's registry.
			if err := llmservice.FreezeUserReferralBenefits(ctx, ScopedSystemSettingsForTenant(tenantID, p.System), referral.id); err != nil {
				logErr("user_referral_freeze", err)
				continue
			}
		}
		revokable = append(revokable, referral)
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		logErr("user_referrals_begin", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	queries := []struct {
		area  string
		query string
		args  []any
	}{
		{"user_referral_identity_reservations", `DELETE FROM user_referral_identity_reservations WHERE tenant_id = ? AND code_hash IN (SELECT code_hash FROM user_referral_codes WHERE tenant_id = ? AND inviter_user_id = ?)`, []any{tenantID, tenantID, userID}},
		// A user may be either the inviter or the invitee. Registration sessions
		// retain the latter in invitee_user_id after successful attribution.
		{"user_referral_registration_sessions", `DELETE FROM user_referral_registration_sessions WHERE tenant_id = ? AND (invitee_user_id = ? OR code_hash IN (SELECT code_hash FROM user_referral_codes WHERE tenant_id = ? AND inviter_user_id = ?))`, []any{tenantID, userID, tenantID, userID}},
		{"user_referral_handoffs", `DELETE FROM user_referral_handoffs WHERE tenant_id = ? AND (inviter_user_id = ? OR code_hash IN (SELECT code_hash FROM user_referral_codes WHERE tenant_id = ? AND inviter_user_id = ?))`, []any{tenantID, userID, tenantID, userID}},
	}
	for _, item := range queries {
		if _, err := tx.ExecContext(ctx, item.query, item.args...); err != nil {
			logErr(item.area, err)
			return
		}
	}
	// An inviter's encrypted code is retained solely as historical evidence,
	// but no longer resolves because lookup accepts only active codes.
	if _, err := tx.ExecContext(ctx, `UPDATE user_referral_codes SET status = 'revoked', rotated_at = COALESCE(rotated_at, ?)
		WHERE tenant_id = ? AND inviter_user_id = ? AND status = 'active'`, time.Now().UTC().Format(time.RFC3339), tenantID, userID); err != nil {
		logErr("user_referral_codes_revoke", err)
		return
	}
	now := time.Now().UTC()
	for _, referral := range revokable {
		if strings.EqualFold(strings.TrimSpace(referral.status), "revoked") {
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE user_referrals SET status = 'revoked', updated_at = ?
			WHERE tenant_id = ? AND id = ? AND status = ?`, now.Format(time.RFC3339), tenantID, referral.id, referral.status)
		if err != nil {
			logErr("user_referral_revoke", err)
			return
		}
		changed, err := result.RowsAffected()
		if err != nil || changed == 0 {
			if err != nil {
				logErr("user_referral_revoke", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_referral_status_history (id, tenant_id, referral_id, from_status, to_status, reason, actor_user_id, created_at)
			VALUES (?, ?, ?, ?, 'revoked', ?, 'system', ?)`, llmservice.NewID("refhist"), tenantID, referral.id, referral.status, "account deleted; referral benefits frozen", now.Format(time.RFC3339)); err != nil {
			logErr("user_referral_history", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		logErr("user_referrals_commit", err)
	}
}

func (p *UserDataPurger) deleteKnowledgeShares(ctx context.Context, tenantID, userID string, emails []string, logErr func(string, error)) {
	if p.DB == nil {
		return
	}
	cleanup := func(where string, args ...any) {
		rows, err := p.DB.QueryContext(ctx, `SELECT storage_ref FROM knowledge_shares WHERE `+where, args...)
		if err != nil {
			logErr("knowledge_shares_list", err)
			return
		}
		var refs []string
		for rows.Next() {
			var ref string
			if err := rows.Scan(&ref); err != nil {
				logErr("knowledge_shares_scan", err)
				continue
			}
			refs = append(refs, ref)
		}
		if err := rows.Close(); err != nil {
			logErr("knowledge_shares_close", err)
		}
		for _, ref := range refs {
			if path, ok := knowledgeSharePackagePath(p.KnowledgeSharePackageDir, ref); ok {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					logErr("knowledge_share_package", err)
				}
			}
		}
		_, err = p.DB.ExecContext(ctx, `DELETE FROM knowledge_shares WHERE `+where, args...)
		logErr("knowledge_shares", err)
	}
	if userID != "" {
		cleanup("tenant_id = ? AND owner_user_id = ?", tenantID, userID)
	}
	for _, email := range emails {
		cleanup("tenant_id = ? AND lower(owner_user_email) = lower(?)", tenantID, email)
	}
}

func (p *UserDataPurger) deleteUserFiles(tenantID, userID string, emails []string, logErr func(string, error)) {
	if userID == "" && len(emails) == 0 {
		return
	}
	for _, email := range emails {
		principal := &auth.ViewerPrincipal{TenantID: tenantID, UserID: userID, Email: email}
		if p.KnowledgeSyncDir != "" {
			logErr("knowledge_sync_files", removeKnowledgeSyncUserDirs(p.KnowledgeSyncDir, principal))
		}
		if p.WelcomeSyncDir != "" {
			logErr("welcome_sync_files", os.RemoveAll(welcomeSyncUserDir(p.WelcomeSyncDir, principal)))
		}
	}
	if p.VirtualRepositorySyncDir != "" && userID != "" {
		sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)))
		logErr("virtual_repository_sync_files", os.RemoveAll(filepath.Join(p.VirtualRepositorySyncDir, "users", hex.EncodeToString(sum[:]))))
	}
	if p.UserDataMigrationDir != "" && userID != "" {
		logErr("user_data_migration_files", os.RemoveAll(filepath.Join(p.UserDataMigrationDir, safeMigrationSegment(tenantID), safeMigrationSegment(userID))))
	}
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
