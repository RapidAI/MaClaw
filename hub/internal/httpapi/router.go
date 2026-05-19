package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/dingtalk"
	"github.com/RapidAI/CodeClaw/hub/internal/entry"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	skillpkg "github.com/RapidAI/CodeClaw/hub/internal/skill"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/ve"
	"github.com/RapidAI/CodeClaw/hub/internal/wecom"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func resolveHubRuntimeDataDir(hubCfg *config.Config, configPath string) string {
	if hubCfg != nil && strings.EqualFold(strings.TrimSpace(hubCfg.Database.Driver), "sqlite") {
		dsn := strings.TrimSpace(hubCfg.Database.DSN)
		if idx := strings.IndexByte(dsn, '?'); idx >= 0 {
			dsn = dsn[:idx]
		}
		dsn = strings.TrimPrefix(dsn, "file:")
		if dsn != "" && dsn != ":memory:" && !strings.HasPrefix(dsn, ":memory:") {
			dir := filepath.Dir(dsn)
			if dir != "." && dir != "" {
				return dir
			}
		}
	}
	return resolveHubDataDir(configPath)
}

func NewRouter(
	admins *auth.AdminService,
	identity *auth.IdentityService,
	centerSvc *center.Service,
	mailer *mail.Service,
	gateway *ws.Gateway,
	deviceSvc *device.Service,
	sessionSvc *session.Service,
	invitationSvc *invitation.Service,
	emailInviteRepo store.EmailInviteRepository,
	system store.SystemSettingsRepository,
	hubDB *sql.DB,
	llmPromptCache *llmcache.Cache,
	adminAudit store.AdminAuditRepository,
	failureLogs store.FailureEventLogRepository,
	feishuNotifier *feishu.Notifier,
	feishuPlugin *feishu.FeishuPlugin,
	openclawIMPlugin *im.WebhookIMPlugin,
	qqbotPlugin *qqbot.Plugin,
	wecomPlugin *wecom.Plugin,
	dingtalkPlugin *dingtalk.Plugin,
	hubLLMStatusFn func(context.Context) string,
	convStatsFn func() (int, int),
	chatStore *chat.Store,
	chatChannelSvc *chat.ChannelService,
	chatMessageSvc *chat.MessageService,
	chatFileSvc *chat.FileService,
	chatReadReceiptSvc *chat.ReadReceiptService,
	chatPresenceSvc *chat.PresenceService,
	chatVoiceSignaling *chat.VoiceSignaling,
	chatNotifier *chat.Notifier,
	securitySvc *security.SecurityService,
	hubCfg *config.Config,
	configPath string,
	ensureTLSCert func(certFile, keyFile string) error,
	staticDir string,
	routePrefix string,
	bridgeDir string,
	tenantRepoOpt ...store.TenantRepository,
) http.Handler {
	var tenantRepo store.TenantRepository
	if len(tenantRepoOpt) > 0 {
		tenantRepo = tenantRepoOpt[0]
	}
	var invChecker entry.InvitationCodeChecker
	if invitationSvc != nil {
		invChecker = invitationSvc
	}
	entrySvc := entry.NewService(identity, invChecker)
	var userLookup machineUserLookup
	if identity != nil {
		userLookup = identity.UsersRepo()
	}
	var imCleaners []IMBindingCleaner
	if qqbotPlugin != nil {
		imCleaners = append(imCleaners, qqbotPlugin)
	}
	if wecomPlugin != nil {
		imCleaners = append(imCleaners, wecomPlugin)
	}
	if dingtalkPlugin != nil {
		imCleaners = append(imCleaners, dingtalkPlugin)
	}
	mux := http.NewServeMux()
	groupDiscussionSvc := NewGroupDiscussionService(hubDB)
	groupDiscussionHandler := NewGroupDiscussionHandler(groupDiscussionSvc, deviceSvc)
	fileRelay := ve.NewFileRelay(filepath.Join(resolveHubRuntimeDataDir(hubCfg, configPath), "ve-files"))
	fileRelay.SetParticipantValidator(groupDiscussionSvc)
	fileRelay.Start(context.Background())
	capabilitySvc := capability.NewService(hubDB)
	mux.HandleFunc("GET /healthz", HealthHandler("maclaw-hub"))
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(admins))
	groupDiscussionAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			principal, ok := authenticateVEMachine(w, r, identity)
			if !ok {
				return
			}
			participantID := r.URL.Query().Get("participant_id")
			if participantID != "" && !strings.EqualFold(participantID, principal.MachineID) {
				writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "participant_id must match authenticated machine")
				return
			}
			r.Header.Set("X-Authenticated-Machine-ID", principal.MachineID)
			r.Header.Set("X-Hub-Tenant-ID", principal.TenantID)
			h(w, r)
		}
	}
	groupDiscussionHandler.RegisterHubRoutesWithMiddleware(mux, groupDiscussionAuth)
	fileRelayAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			principal, ok := authenticateVEMachine(w, r, identity)
			if !ok {
				return
			}
			participantID := r.URL.Query().Get("participant_id")
			if participantID != "" && !strings.EqualFold(participantID, principal.MachineID) {
				writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "participant_id must match authenticated machine")
				return
			}
			r.Header.Set("X-Participant-ID", principal.MachineID)
			h(w, r)
		}
	}
	mux.HandleFunc("POST /api/ve/files/upload", fileRelayAuth(fileRelay.HandleUpload))
	mux.HandleFunc("GET /api/ve/files/download/{id}", fileRelayAuth(fileRelay.HandleDownload))
	mux.HandleFunc("GET /api/ve/history/{discussionID}/attachments/{fileID}", RequireAdmin(admins, fileRelay.HandleAdminDownload))
	mux.HandleFunc("GET /api/admin/a2a/group-discussions", RequireAdmin(admins, groupDiscussionHandler.handleAdminGroupDiscussions))
	mux.HandleFunc("POST /api/ve/register", VERegisterHandler(system, identity, userLookup))
	mux.HandleFunc("PUT /api/ve/settings", VESettingsHandler(system, identity, userLookup))
	mux.HandleFunc("GET /api/ve/status", VEStatusHandler(system, identity))
	mux.HandleFunc("GET /api/ve/discoverable", VEDiscoverableHandler(system, identity))
	mux.HandleFunc("POST /api/ve/{id}/initiate", VEInitiateHandler(system, groupDiscussionSvc, identity))
	mux.HandleFunc("GET /api/ve/list", RequireAdmin(admins, VEAdminListHandler(system, userLookup)))
	mux.HandleFunc("GET /api/ve/{id}/history", RequireAdmin(admins, VEHistoryHandler(system, groupDiscussionSvc, userLookup)))
	mux.HandleFunc("GET /api/ve/history/search", RequireAdmin(admins, VEHistorySearchHandler(system, groupDiscussionSvc, userLookup)))
	mux.HandleFunc("GET /api/ve/history/{id}/detail", RequireAdmin(admins, VEHistoryDetailHandler(groupDiscussionSvc)))
	mux.HandleFunc("PUT /api/ve/config", RequireAdmin(admins, VEAdminConfigHandler(system)))
	mux.HandleFunc("GET /api/ve/config", RequireAdmin(admins, VEAdminConfigHandler(system)))
	mux.HandleFunc("POST /api/ve/{id}/approve", RequireAdmin(admins, VEAdminActionHandler(system, "approve", deviceSvc)))
	mux.HandleFunc("POST /api/ve/{id}/reject", RequireAdmin(admins, VEAdminActionHandler(system, "reject", deviceSvc)))
	mux.HandleFunc("POST /api/ve/{id}/disable", RequireAdmin(admins, VEAdminActionHandler(system, "disable", deviceSvc)))
	mux.HandleFunc("POST /api/admin/setup", SetupAdminHandler(admins))
	mux.HandleFunc("POST /api/admin/login", AdminLoginHandler(admins))
	mux.HandleFunc("POST /api/admin/password", RequireAdmin(admins, AdminChangePasswordHandler(admins)))
	mux.HandleFunc("POST /api/admin/profile", RequireAdmin(admins, AdminUpdateProfileHandler(admins)))
	if tenantRepo != nil {
		mux.HandleFunc("GET /api/admin/tenants", RequireAdmin(admins, AdminTenantsListHandler(tenantRepo)))
		mux.HandleFunc("POST /api/admin/tenants", RequireAdmin(admins, AdminTenantCreateHandler(tenantRepo, admins, adminAudit)))
		mux.HandleFunc("GET /api/admin/tenants/{tenantId}", RequireAdmin(admins, AdminTenantDetailHandler(tenantRepo)))
		mux.HandleFunc("POST /api/admin/tenants/{tenantId}/admins", RequireAdmin(admins, AdminTenantAdminCreateHandler(tenantRepo, admins, adminAudit)))
	}
	mux.HandleFunc("GET /api/admin/debug/machines", RequireAdmin(admins, DebugListMachinesHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/machine-events", RequireAdmin(admins, DebugListMachineEventsHandler(deviceSvc)))
	mux.HandleFunc("DELETE /api/admin/machines", RequireAdmin(admins, DeleteMachineHandler(deviceSvc)))
	mux.HandleFunc("POST /api/admin/machines/rename", RequireAdmin(admins, RenameMachineHandler(deviceSvc)))
	mux.HandleFunc("POST /api/admin/machines/clear-offline", RequireAdmin(admins, ClearOfflineMachinesHandler(deviceSvc)))
	mux.HandleFunc("DELETE /api/admin/machines/by-email", RequireAdmin(admins, DeleteMachinesByEmailHandler(deviceSvc, userLookup)))
	mux.HandleFunc("DELETE /api/admin/machines/force-by-email", RequireAdmin(admins, ForceDeleteMachinesByEmailHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/sessions", RequireAdmin(admins, DebugListSessionsHandler(sessionSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/session", RequireAdmin(admins, DebugGetSessionHandler(sessionSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/failure-logs", RequireAdmin(admins, ListFailureLogsHandler(failureLogs)))
	mux.HandleFunc("GET /api/admin/sessions/all", RequireAdmin(admins, AdminListAllSessionsHandler(sessionSvc, userLookup)))
	mux.HandleFunc("POST /api/admin/users/manual-bind", RequireAdmin(admins, ManualBindHandler(identity)))
	mux.HandleFunc("GET /api/admin/users", RequireAdmin(admins, ListUsersHandler(identity, system, securitySvc)))
	mux.HandleFunc("DELETE /api/admin/users", RequireAdmin(admins, DeleteBoundUserHandler(identity, deviceSvc, invitationSvc, feishuNotifier, imCleaners)))
	mux.HandleFunc("GET /api/admin/users/lookup", RequireAdmin(admins, LookupUserHandler(identity)))
	mux.HandleFunc("GET /api/admin/blocklist", RequireAdmin(admins, ListBlockedEmailsHandler(identity)))
	mux.HandleFunc("POST /api/admin/blocklist", RequireAdmin(admins, AddBlockedEmailHandler(identity)))
	mux.HandleFunc("DELETE /api/admin/blocklist/{email}", RequireAdmin(admins, RemoveBlockedEmailHandler(identity)))
	// Email invite routes (restored)
	mux.HandleFunc("POST /api/admin/invites", RequireAdmin(admins, CreateEmailInviteHandler(emailInviteRepo)))
	mux.HandleFunc("GET /api/admin/invites", RequireAdmin(admins, ListEmailInvitesHandler(emailInviteRepo)))
	mux.HandleFunc("DELETE /api/admin/invites/{id}", RequireAdmin(admins, DeleteEmailInviteHandler(emailInviteRepo)))
	mux.HandleFunc("POST /api/admin/invitation-codes/generate", RequireAdmin(admins, GenerateInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes", RequireAdmin(admins, ListInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("POST /api/admin/invitation-codes/toggle", RequireAdmin(admins, ToggleInvitationCodeHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/status", RequireAdmin(admins, InvitationCodeStatusHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/export", RequireAdmin(admins, ExportInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("POST /api/admin/invitation-codes/unbind", RequireAdmin(admins, UnbindInvitationCodeHandler(invitationSvc, identity, deviceSvc, feishuNotifier, imCleaners)))
	mux.HandleFunc("GET /api/admin/enrollments/pending", RequireAdmin(admins, ListPendingEnrollmentsHandler(identity)))
	mux.HandleFunc("GET /api/admin/enrollments/all", RequireAdmin(admins, ListAllEnrollmentsHandler(identity)))
	mux.HandleFunc("POST /api/admin/enrollments/approve", RequireAdmin(admins, ApproveEnrollmentHandler(identity, securitySvc)))
	mux.HandleFunc("POST /api/admin/enrollments/reject", RequireAdmin(admins, RejectEnrollmentHandler(identity)))
	mux.HandleFunc("GET /api/admin/pending-logins", RequireAdmin(admins, ListPendingLoginsHandler(identity)))
	mux.HandleFunc("POST /api/admin/pending-logins/confirm", RequireAdmin(admins, AdminConfirmLoginHandler(identity)))
	mux.HandleFunc("GET /api/admin/center/status", RequireAdmin(admins, GetCenterStatusHandler(centerSvc)))
	mux.HandleFunc("POST /api/center/user-migration/export", CenterUserMigrationExportHandler(centerSvc, identity, deviceSvc))
	mux.HandleFunc("POST /api/center/user-migration/import", CenterUserMigrationImportHandler(centerSvc, identity, deviceSvc))
	mux.HandleFunc("POST /api/center/user-migration/delete", CenterUserMigrationDeleteHandler(centerSvc, identity, deviceSvc, invitationSvc, feishuNotifier, imCleaners))
	mux.HandleFunc("POST /api/admin/center/config", RequireAdmin(admins, UpdateCenterConfigHandler(centerSvc, identity, func(url string) {
		if qqbotPlugin != nil {
			qqbotPlugin.SetPublicBaseURL(url)
		}
		if feishuPlugin != nil {
			feishuPlugin.SetPublicBaseURL(url)
		}
		if dingtalkPlugin != nil {
			dingtalkPlugin.SetPublicBaseURL(url)
		}
	})))
	mux.HandleFunc("GET /api/admin/mail/config", RequireAdmin(admins, GetMailConfigHandler(mailer)))
	mux.HandleFunc("POST /api/admin/mail/config", RequireAdmin(admins, UpdateMailConfigHandler(mailer)))
	mux.HandleFunc("POST /api/admin/center/register", RequireAdmin(admins, RegisterCenterHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/mail/test", RequireAdmin(admins, AdminSendTestMailHandler(mailer)))
	mux.HandleFunc("GET /api/admin/feishu/config", RequireAdmin(admins, GetFeishuConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/feishu/config", RequireAdmin(admins, UpdateFeishuConfigHandler(system, feishuNotifier)))
	mux.HandleFunc("GET /api/admin/feishu/bindings", RequireAdmin(admins, GetFeishuBindingsHandler(feishuNotifier)))
	mux.HandleFunc("DELETE /api/admin/feishu/bindings", RequireAdmin(admins, DeleteFeishuBindingHandler(feishuNotifier)))
	mux.HandleFunc("GET /api/admin/feishu/auto-enroll", RequireAdmin(admins, GetFeishuAutoEnrollHandler(system)))
	mux.HandleFunc("POST /api/admin/feishu/auto-enroll", RequireAdmin(admins, UpdateFeishuAutoEnrollHandler(system, feishuNotifier)))
	mux.HandleFunc("GET /api/admin/settings/openclaw_im", RequireAdmin(admins, GetOpenclawIMConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/openclaw_im", RequireAdmin(admins, UpdateOpenclawIMConfigHandler(system, bridgeDir)))
	mux.HandleFunc("POST /api/admin/settings/openclaw_im/test", RequireAdmin(admins, TestOpenclawIMWebhookHandler(system)))
	mux.HandleFunc("POST /api/openclaw_im/webhook", OpenclawIMWebhookHandler(system, openclawIMPlugin))
	// Bridge channel management
	mux.HandleFunc("GET /api/admin/bridge/channels", RequireAdmin(admins, GetBridgeChannelsHandler(system, bridgeDir)))
	mux.HandleFunc("POST /api/admin/bridge/channels", RequireAdmin(admins, SaveBridgeChannelHandler(system, bridgeDir)))
	mux.HandleFunc("GET /api/admin/bridge/status", RequireAdmin(admins, BridgeStatusHandler(system)))
	mux.HandleFunc("POST /api/admin/bridge/install", RequireAdmin(admins, InstallBridgeDepsHandler(bridgeDir)))
	// Hub LLM configuration
	mux.HandleFunc("GET /api/admin/hub_llm_config", RequireAdmin(admins, GetHubLLMConfigHandler(system)))
	mux.HandleFunc("PUT /api/admin/hub_llm_config", RequireAdmin(admins, UpdateHubLLMConfigHandler(system)))
	mux.HandleFunc("GET /api/admin/hub_llm_prompt_cache_config", RequireAdmin(admins, GetHubLLMPromptCacheConfigHandler(system, llmPromptCache)))
	mux.HandleFunc("PUT /api/admin/hub_llm_prompt_cache_config", RequireAdmin(admins, UpdateHubLLMPromptCacheConfigHandler(system, llmPromptCache)))
	mux.HandleFunc("POST /api/admin/hub_llm_prompt_cache_clear", RequireAdmin(admins, ClearHubLLMPromptCacheHandler(llmPromptCache)))
	mux.HandleFunc("GET /api/admin/hub_llm_prompt_cache_entries", RequireAdmin(admins, GetHubLLMPromptCacheEntriesHandler(llmPromptCache)))
	mux.HandleFunc("GET /api/admin/hub_llm_prompt_cache_entry", RequireAdmin(admins, GetHubLLMPromptCacheEntryHandler(llmPromptCache)))
	mux.HandleFunc("DELETE /api/admin/hub_llm_prompt_cache_entry", RequireAdmin(admins, DeleteHubLLMPromptCacheEntryHandler(llmPromptCache)))
	mux.HandleFunc("POST /api/admin/hub_llm_test", RequireAdmin(admins, TestHubLLMHandler(system)))
	mux.HandleFunc("GET /api/admin/hub_llm_status", RequireAdmin(admins, HubLLMStatusHandler(hubLLMStatusFn, system, llmPromptCache)))
	mux.HandleFunc("GET /api/admin/llm/services/diagnose", RequireAdmin(admins, GetLLMServiceEntitlementDiagnosticHandler(system, securitySvc)))
	mux.HandleFunc("GET /api/admin/llm/providers", RequireAdmin(admins, GetLLMProvidersHandler(system)))
	mux.HandleFunc("PUT /api/admin/llm/providers", RequireAdmin(admins, UpdateLLMProvidersHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/providers/test", RequireAdmin(admins, TestLLMProviderHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/providers/test-key", RequireAdmin(admins, GenerateLLMProviderTestKeyHandler(identity)))
	mux.HandleFunc("GET /api/admin/llm/services", RequireAdmin(admins, GetLLMServicesAdminHandler(system)))
	mux.HandleFunc("PUT /api/admin/llm/services", RequireAdmin(admins, UpdateLLMServicesAdminHandler(system, securitySvc, adminAudit)))
	mux.HandleFunc("POST /api/admin/llm/service-cards", RequireAdmin(admins, CreateLLMServiceCardHandler(system, adminAudit)))
	mux.HandleFunc("GET /api/admin/llm/service-cards", RequireAdmin(admins, ListLLMServiceCardsHandler(system)))
	mux.HandleFunc("GET /api/admin/llm/service-cards/export", RequireAdmin(admins, ExportLLMServiceCardsHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/service-cards/export-selected", RequireAdmin(admins, ExportSelectedLLMServiceCardsHandler(system)))
	mux.HandleFunc("DELETE /api/admin/llm/service-cards/{id}", RequireAdmin(admins, DeleteLLMServiceCardHandler(system, adminAudit)))
	mux.HandleFunc("POST /api/admin/llm/service-cards/delete-batch", RequireAdmin(admins, DeleteLLMServiceCardsBatchHandler(system, adminAudit)))
	mux.HandleFunc("DELETE /api/admin/llm/service-grants/{id}", RequireAdmin(admins, DeleteLLMServiceGrantHandler(system, adminAudit)))
	mux.HandleFunc("GET /api/admin/llm/usage-report", RequireAdmin(admins, GetLLMUsageReportHandler(system, securitySvc)))
	mux.HandleFunc("GET /api/admin/llm/access-logs", RequireAdmin(admins, GetLLMEndpointAccessLogsHandler(system)))
	mux.HandleFunc("GET /api/admin/model_download/status", RequireAdmin(admins, GetAdminModelDownloadStatusHandler(configPath)))
	mux.HandleFunc("POST /api/admin/model_download/trigger", RequireAdmin(admins, TriggerAdminModelDownloadHandler(configPath)))
	mux.HandleFunc("GET /api/llm/service/status", GetLLMServiceStatusHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/llm/service/account", GetLLMServiceAccountHandler(identity, system, securitySvc))
	mux.HandleFunc("POST /api/llm/service/redeem", RedeemLLMServiceCardHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/llm/v1/models", LLMV1ModelsHandler(identity, system, securitySvc))
	mux.HandleFunc("POST /api/llm/v1/chat/completions", LLMV1ChatCompletionsHandler(identity, system, securitySvc, llmPromptCache))
	// Content audit configuration
	mux.HandleFunc("GET /api/admin/content_audit/config", RequireAdmin(admins, GetContentAuditConfigHandler(system)))
	mux.HandleFunc("PUT /api/admin/content_audit/config", RequireAdmin(admins, UpdateContentAuditConfigHandler(system)))
	// TLS configuration
	mux.HandleFunc("GET /api/admin/tls_config", RequireAdmin(admins, GetTLSConfigHandler(hubCfg)))
	mux.HandleFunc("POST /api/admin/tls_config", RequireAdmin(admins, UpdateTLSConfigHandler(hubCfg, configPath, ensureTLSCert, centerSvc)))
	// Smart route permission
	mux.HandleFunc("POST /api/admin/users/smart_route", RequireAdmin(admins, UpdateUserSmartRouteHandler(identity.UsersRepo())))
	mux.HandleFunc("GET /api/admin/smart_route_all", RequireAdmin(admins, GetSmartRouteAllHandler(system)))
	mux.HandleFunc("PUT /api/admin/smart_route_all", RequireAdmin(admins, UpdateSmartRouteAllHandler(system)))
	// Security management
	if securitySvc != nil {
		mux.HandleFunc("GET /api/admin/security/groups", RequireAdmin(admins, SecurityGroupsHandler(securitySvc)))
		mux.HandleFunc("GET /api/admin/security/groups/root", RequireAdmin(admins, SecurityGroupsRootHandler(securitySvc)))
		mux.HandleFunc("POST /api/admin/security/groups", RequireAdmin(admins, CreateSecurityGroupHandler(securitySvc, adminAudit)))
		mux.HandleFunc("PUT /api/admin/security/groups/{id}", RequireAdmin(admins, UpdateSecurityGroupHandler(securitySvc, adminAudit)))
		mux.HandleFunc("DELETE /api/admin/security/groups/{id}", RequireAdmin(admins, DeleteSecurityGroupHandler(securitySvc, adminAudit)))
		mux.HandleFunc("GET /api/admin/security/groups/{id}/members", RequireAdmin(admins, ListGroupMembersHandler(securitySvc)))
		mux.HandleFunc("POST /api/admin/security/groups/{id}/members", RequireAdmin(admins, AddGroupMemberHandler(securitySvc, adminAudit)))
		mux.HandleFunc("DELETE /api/admin/security/groups/{id}/members/{email}", RequireAdmin(admins, RemoveGroupMemberHandler(securitySvc, adminAudit)))
		mux.HandleFunc("GET /api/admin/security/groups/{id}/policy", RequireAdmin(admins, GetGroupPolicyHandler(securitySvc)))
		mux.HandleFunc("PUT /api/admin/security/groups/{id}/policy", RequireAdmin(admins, UpdateGroupPolicyHandler(securitySvc, adminAudit)))
		mux.HandleFunc("GET /api/admin/security/users/{email}/effective-policy", RequireAdmin(admins, GetUserEffectivePolicyHandler(securitySvc)))
		mux.HandleFunc("GET /api/admin/security/settings", RequireAdmin(admins, GetSecuritySettingsHandler(securitySvc)))
		mux.HandleFunc("PUT /api/admin/security/settings", RequireAdmin(admins, UpdateSecuritySettingsHandler(securitySvc)))
		mux.HandleFunc("PUT /api/admin/security/settings/default-group", RequireAdmin(admins, SetDefaultGroupHandler(securitySvc, adminAudit)))
		// Public endpoint for enrollment group tree
		mux.HandleFunc("GET /api/enroll/group-tree", EnrollGroupTreeHandler(securitySvc))
	}

	// Skill source control API (independent of security group policy).
	// Supports global / tenant / user level control.
	{
		skillSourceSvc := skillpkg.NewSourceControlService(system)
		adminWrap := func(h http.HandlerFunc) http.HandlerFunc {
			return RequireAdmin(admins, h)
		}
		skillpkg.RegisterRoutes(mux, skillSourceSvc, adminWrap, func(r *http.Request) (string, bool) {
			return RequestTenantID(r), IsGlobalAdmin(r.Context())
		})
		// Wire into SecurityService so GetHeartbeatPolicy merges the result.
		if securitySvc != nil {
			securitySvc.SetSkillSourcesProvider(skillSourceSvc)
		}
	}

	// Conversation stats
	if convStatsFn != nil {
		mux.HandleFunc("GET /api/admin/conversation_stats", RequireAdmin(admins, func(w http.ResponseWriter, r *http.Request) {
			contexts, rounds := convStatsFn()
			writeJSON(w, http.StatusOK, map[string]any{
				"active_contexts": contexts,
				"total_rounds":    rounds,
			})
		}))
	}

	mux.HandleFunc("GET /api/admin/settings/qqbot", RequireAdmin(admins, GetQQBotConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/qqbot", RequireAdmin(admins, UpdateQQBotConfigHandler(system, qqbotPlugin)))
	mux.HandleFunc("GET /api/admin/qqbot/bindings", RequireAdmin(admins, GetQQBotBindingsHandler(qqbotPlugin)))
	mux.HandleFunc("DELETE /api/admin/qqbot/bindings", RequireAdmin(admins, DeleteQQBotBindingHandler(qqbotPlugin)))
	mux.HandleFunc("POST /api/qqbot/webhook", QQBotWebhookHandler(qqbotPlugin))
	mux.HandleFunc("GET /api/qqbot/tempfile/{token}", qqbotPlugin.ServeTempFile)

	// WeCom Bot
	mux.HandleFunc("GET /api/admin/settings/wecom", RequireAdmin(admins, GetWeComConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/wecom", RequireAdmin(admins, UpdateWeComConfigHandler(system, wecomPlugin)))
	mux.HandleFunc("GET /api/admin/wecom/bindings", RequireAdmin(admins, GetWeComBindingsHandler(wecomPlugin)))
	mux.HandleFunc("DELETE /api/admin/wecom/bindings", RequireAdmin(admins, DeleteWeComBindingHandler(wecomPlugin)))

	// DingTalk Bot
	mux.HandleFunc("GET /api/admin/settings/dingtalk", RequireAdmin(admins, GetDingTalkConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/dingtalk", RequireAdmin(admins, UpdateDingTalkConfigHandler(system, dingtalkPlugin)))
	mux.HandleFunc("GET /api/admin/dingtalk/bindings", RequireAdmin(admins, GetDingTalkBindingsHandler(dingtalkPlugin)))
	mux.HandleFunc("DELETE /api/admin/dingtalk/bindings", RequireAdmin(admins, DeleteDingTalkBindingHandler(dingtalkPlugin)))

	mux.HandleFunc("/api/feishu/webhook", feishu.WebhookHandler(feishuNotifier))
	if feishuPlugin != nil {
		mux.HandleFunc("GET /api/feishu/tempfile/{token}", feishuPlugin.ServeTempFile)
	}
	// Public binding page API (no auth required); allow cross-origin for iframe embedding
	bindCORS := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/bind/config", bindCORS(BindConfigHandler(invitationSvc)))
	mux.HandleFunc("POST /api/bind/query", bindCORS(BindQueryHandler(identity)))
	mux.HandleFunc("POST /api/bind/send-code", bindCORS(BindSendCodeHandler(identity, mailer, feishuNotifier)))
	mux.HandleFunc("POST /api/bind/unbind", bindCORS(BindUnbindHandler(identity, deviceSvc, invitationSvc, feishuNotifier, imCleaners)))

	mux.HandleFunc("POST /api/enroll/start", EnrollStartHandler(identity, invitationSvc, securitySvc))
	mux.HandleFunc("POST /api/auth/email-request", EmailRequestLoginHandler(identity))
	mux.HandleFunc("POST /api/auth/email-confirm", EmailConfirmLoginHandler(identity))
	mux.HandleFunc("POST /api/auth/email-poll", EmailPollLoginHandler(identity))
	mux.HandleFunc("POST /api/entry/probe", EntryProbeHandler(entrySvc))
	mux.HandleFunc("GET /api/machines", ListMachinesHandler(identity, deviceSvc))
	mux.HandleFunc("POST /api/machines/clear-offline", ClearOfflineMachinesForViewerHandler(identity, deviceSvc))
	mux.HandleFunc("GET /api/sessions", ListSessionsHandler(identity, sessionSvc))
	mux.HandleFunc("GET /api/session", GetSessionHandler(identity, sessionSvc))
	mux.HandleFunc("POST /api/session/start", SessionStartHandler(identity, deviceSvc))
	mux.HandleFunc("POST /api/session/input", SessionInputHandler(identity, sessionSvc, deviceSvc))
	mux.HandleFunc("POST /api/session/interrupt", SessionInterruptHandler(identity, sessionSvc, deviceSvc))
	mux.HandleFunc("POST /api/session/kill", SessionKillHandler(identity, sessionSvc, deviceSvc))
	mux.HandleFunc("GET /api/debug/machines", RequireAdmin(admins, DebugListMachinesHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/debug/machine-events", RequireAdmin(admins, DebugListMachineEventsHandler(deviceSvc)))
	mux.HandleFunc("GET /api/debug/sessions", RequireAdmin(admins, DebugListSessionsHandler(sessionSvc, userLookup)))
	mux.HandleFunc("GET /api/debug/session", RequireAdmin(admins, DebugGetSessionHandler(sessionSvc, userLookup)))
	mux.HandleFunc("/ws", gateway.HandleWS)
	mux.HandleFunc("GET /api/shortcuts", GetShortcutsHandler(identity, system))
	mux.HandleFunc("GET /marketplace", MarketplacePageHandler("hub"))
	mux.HandleFunc("GET /api/capabilities", CapabilityListHandler(capabilitySvc))
	mux.HandleFunc("POST /api/admin/capabilities", RequireAdmin(admins, AdminCapabilityUpsertHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/capabilities/{id}", CapabilityDetailHandler(capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/{id}/versions", CapabilityVersionsHandler(capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/{id}/mcp-secret-requirements", MCPSecretRequirementsHandler(capabilitySvc))
	mux.HandleFunc("POST /api/capabilities/{id}/install-intent", CapabilityInstallIntentHandler(capabilitySvc, system, centerSvc))
	mux.HandleFunc("GET /api/capabilities/managed-deployments", CapabilityManagedDeploymentsHandler(capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/recommended", CapabilityRecommendationsHandler(capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/inventory", UserCapabilityInventoryHandler(identity, capabilitySvc))
	mux.HandleFunc("PUT /api/capabilities/inventory", UserCapabilityInventoryUpsertHandler(identity, capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/mcp-secret-bindings", MCPSecretBindingsHandler(identity, capabilitySvc))
	mux.HandleFunc("PUT /api/capabilities/mcp-secret-bindings", MCPSecretBindingUpsertHandler(identity, capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/mcp-hub-secrets", MCPHubSecretsHandler(identity, capabilitySvc))
	mux.HandleFunc("PUT /api/capabilities/mcp-hub-secrets", MCPHubSecretUpsertHandler(identity, capabilitySvc))
	mux.HandleFunc("GET /api/admin/billing/customer-account", RequireAdmin(admins, AdminBillingCustomerAccountHandler(system, centerSvc)))
	mux.HandleFunc("GET /api/admin/billing/licenses", RequireAdmin(admins, AdminBillingLicensesHandler(system, centerSvc)))
	mux.HandleFunc("GET /api/admin/capability-market/policy", RequireAdmin(admins, AdminCapabilityMarketPolicyGetHandler(system)))
	mux.HandleFunc("PUT /api/admin/capability-market/policy", RequireAdmin(admins, AdminCapabilityMarketPolicyUpdateHandler(system)))
	mux.HandleFunc("GET /api/admin/capability-market/acquisition-requests", RequireAdmin(admins, AdminCapabilityAcquisitionRequestsHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/acquisition-requests/{id}", RequireAdmin(admins, AdminCapabilityAcquisitionRequestDetailHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/acquisition-requests/{id}/approve", RequireAdmin(admins, AdminCapabilityApproveAcquisitionHandler(capabilitySvc, system, centerSvc)))
	mux.HandleFunc("POST /api/admin/capability-market/acquisition-requests/{id}/reject", RequireAdmin(admins, AdminCapabilityRejectAcquisitionHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/acquisition-requests/{id}/complete", RequireAdmin(admins, AdminCapabilityCompleteAcquisitionHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/managed-deployments", RequireAdmin(admins, AdminCapabilityManagedDeploymentCreateHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("DELETE /api/admin/capability-market/managed-deployments/{id}", RequireAdmin(admins, AdminCapabilityManagedDeploymentDeleteHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("POST /api/admin/capability-market/recommendations", RequireAdmin(admins, AdminCapabilityRecommendationCreateHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("DELETE /api/admin/capability-market/recommendations/{id}", RequireAdmin(admins, AdminCapabilityRecommendationDeleteHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("GET /api/admin/audit-logs", RequireAdmin(admins, AdminAuditLogsHandler(adminAudit)))
	mux.HandleFunc("GET /api/admin/capability-market/groups/{id}/effective-policies", RequireAdmin(admins, AdminGroupCapabilityEffectivePoliciesHandler(capabilitySvc, securitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/users/{email}/inventory", RequireAdmin(admins, AdminUserCapabilityInventoryHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/users/{email}/effective-policies", RequireAdmin(admins, AdminUserCapabilityEffectivePoliciesHandler(capabilitySvc, securitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/users/{email}/compliance", RequireAdmin(admins, AdminUserCapabilityComplianceHandler(capabilitySvc, securitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/mcp", RequireAdmin(admins, AdminMCPMarketplaceUpsertHandler(capabilitySvc)))
	mux.HandleFunc("PUT /api/admin/capability-market/mcp", RequireAdmin(admins, AdminMCPMarketplaceUpsertHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/mcp/test", RequireAdmin(admins, AdminMCPTestConnectionHandler()))
	mux.HandleFunc("POST /api/admin/capability-market/mcp-secret-requirements", RequireAdmin(admins, AdminMCPSecretRequirementUpsertHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/admin/capabilities/external-search", RequireAdmin(admins, AdminCapabilityExternalSearchHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/capabilities/mcp/validate", RequireAdmin(admins, AdminMCPValidateHandler()))
	mux.HandleFunc("POST /api/admin/capabilities/import-intent", RequireAdmin(admins, AdminCapabilityImportIntentHandler(capabilitySvc, system, centerSvc)))

	// Workflow admin review API
	{
		workflowReviewSvc := workflow.NewAdminReviewService(workflow.NewPGWorkflowStore(hubDB), capabilitySvc)
		mux.HandleFunc("GET /api/v1/admin/reviews", RequireAdmin(admins, WorkflowAdminReviewListHandler(workflowReviewSvc)))
		mux.HandleFunc("GET /api/v1/admin/reviews/{id}", RequireAdmin(admins, WorkflowAdminReviewDetailHandler(workflowReviewSvc)))
		mux.HandleFunc("POST /api/v1/admin/reviews/{id}/approve", RequireAdmin(admins, WorkflowAdminReviewApproveHandler(workflowReviewSvc)))
		mux.HandleFunc("POST /api/v1/admin/reviews/{id}/reject", RequireAdmin(admins, WorkflowAdminReviewRejectHandler(workflowReviewSvc)))
		mux.HandleFunc("POST /api/v1/admin/reviews/{id}/unpublish", RequireAdmin(admins, WorkflowAdminReviewUnpublishHandler(workflowReviewSvc)))
	}

	// Workflow user-facing auth middleware: authenticates VE machine and sets X-Owner-ID.
	workflowUserAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			principal, ok := authenticateVEMachine(w, r, identity)
			if !ok {
				return
			}
			r.Header.Set("X-Owner-ID", principal.MachineID)
			r = r.WithContext(store.WithTenant(r.Context(), principal.TenantID))
			h(w, r)
		}
	}

	// Workflow CRUD API (user-facing)
	{
		wfStore := workflow.NewPGWorkflowStore(hubDB)
		vm := workflow.NewVersionManager(wfStore)
		wfAPI := workflow.NewWorkflowAPI(wfStore, vm)
		wfAPI.RegisterRoutes(mux, workflowUserAuth)
	}

	// Workflow Instance API
	{
		wfStore := workflow.NewPGWorkflowStore(hubDB)
		instStore := workflow.NewPgInstanceStore(hubDB)
		auditStore := workflow.NewPgAuditStore(hubDB)
		dispatcher := &noopApprovalDispatcher{} // placeholder until A2A dispatch is wired
		executor := workflow.NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)
		instanceAPI := workflow.NewInstanceAPI(executor, instStore, auditStore)
		instanceAPI.RegisterRoutes(mux, workflowUserAuth)

		// Start background services for approval workflow
		escalationMgr := workflow.NewEscalationManager(dispatcher, auditStore, &noopAvailabilityChecker{})
		escalationMgr.Start()

		timeoutTicker := workflow.NewTimeoutTicker(executor, instStore)
		timeoutTicker.Start()
	}

	// Workflow audit trail query API
	{
		auditStore := workflow.NewPgAuditStore(hubDB)
		auditAPI := workflow.NewAuditAPI(auditStore)
		auditAPI.RegisterRoutes(mux, workflowUserAuth)
	}

	// Review notification background service
	{
		wfStore := workflow.NewPGWorkflowStore(hubDB)
		// Use Hub's notification sender if available, otherwise graceful degradation (log only).
		notifier := workflow.NewHubReviewNotifier(nil)
		reviewNotifSvc := workflow.NewReviewNotificationService(notifier, wfStore, workflow.ReviewNotificationConfig{
			ReminderInterval: 7 * 24 * time.Hour,
			CheckInterval:    1 * time.Hour,
		})
		reviewNotifSvc.Start()
	}

	// Workflow market listing API
	{
		marketSvc := workflow.NewMarketService(capabilitySvc)
		mux.HandleFunc("GET /api/v1/market/workflows", func(w http.ResponseWriter, r *http.Request) {
			filter := workflow.MarketListingFilter{
				SubCategory: workflow.WorkflowSubCategory(r.URL.Query().Get("sub_category")),
				Author:      r.URL.Query().Get("author"),
				Keyword:     r.URL.Query().Get("keyword"),
			}
			if cat := r.URL.Query().Get("category"); cat != "" {
				filter.Category = workflow.MarketCategory(cat)
			}
			page, err := marketSvc.ListWorkflows(r.Context(), filter)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, page)
		})
	}

	mux.HandleFunc("PUT /api/shortcuts", PutShortcutsHandler(identity, system))
	// Webhook session endpoint (Bearer token auth handled internally)
	mux.HandleFunc("POST /api/webhook/session", WebhookCreateSessionHandler(deviceSvc, sessionSvc))

	// Chat Module
	if chatChannelSvc != nil {
		mux.HandleFunc("POST /api/chat/channels", ChatCreateChannelHandler(identity, chatChannelSvc))
		mux.HandleFunc("GET /api/chat/channels", ChatListChannelsHandler(identity, chatChannelSvc))
		mux.HandleFunc("POST /api/chat/channels/{id}/messages", ChatSendMessageHandler(identity, chatChannelSvc, chatMessageSvc))
		mux.HandleFunc("GET /api/chat/channels/{id}/messages", ChatGetMessagesHandler(identity, chatChannelSvc, chatMessageSvc))
		mux.HandleFunc("POST /api/chat/read-receipts", ChatReadReceiptsHandler(identity, chatChannelSvc, chatReadReceiptSvc))
		mux.HandleFunc("POST /api/chat/files/upload", ChatFileUploadHandler(identity, chatChannelSvc, chatFileSvc))
		mux.HandleFunc("GET /api/chat/files/{id}", ChatFileDownloadHandler(identity, chatChannelSvc, chatFileSvc))
		mux.HandleFunc("GET /api/chat/users/{id}/presence", ChatPresenceHandler(identity, chatPresenceSvc))
		mux.HandleFunc("POST /api/chat/voice/call", ChatVoiceCallHandler(identity, chatChannelSvc, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/voice/answer", ChatVoiceAnswerHandler(identity, chatChannelSvc, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/voice/ice", ChatVoiceICEHandler(identity, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/voice/hangup", ChatVoiceHangupHandler(identity, chatChannelSvc, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/push/register", ChatPushRegisterHandler(identity, chatStore))
		mux.HandleFunc("POST /api/chat/typing", ChatTypingHandler(identity, chatChannelSvc, chatNotifier))
		mux.HandleFunc("/api/chat/ws", ChatWSHandler(identity, chatChannelSvc, chatNotifier))
	}

	// Model file download (embedding models etc.); public, no auth
	mux.HandleFunc("GET /api/v1/models/{filename}", ModelDownloadHandler(configPath))
	mux.HandleFunc("GET /api/public/model_download/status", PublicModelDownloadStatusHandler(configPath))

	registerPWAStaticRoutes(mux, staticDir, routePrefix)
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	registerBindStaticRoutes(mux, "./web/bind", "/bind")
	registerGetCreditsStaticRoutes(mux, "./web/get-credits", "/get-credits")
	registerStaticRoutes(mux, "./web/connector", "/connector")
	registerStaticRoutes(mux, "./web/approval_workflow", "/approval_workflow")
	return mux
}
