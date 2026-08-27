package httpapi

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/digitalasset"
	"github.com/RapidAI/CodeClaw/hub/internal/dingtalk"
	"github.com/RapidAI/CodeClaw/hub/internal/entry"
	"github.com/RapidAI/CodeClaw/hub/internal/expert"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/industryexpert"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	skillpkg "github.com/RapidAI/CodeClaw/hub/internal/skill"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/survey"
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
	tenantIMRuntimeReloader TenantIMRuntimeReloader,
	knowledgeShares store.KnowledgeShareRepository,
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
	var platformUsers store.UserRepository
	var platformMachines platformMachineLister
	var userReferralRepo store.UserReferralRepository
	if hubDB != nil && identity != nil && tenantRepo != nil {
		userReferralRepo = storesqlite.NewUserReferralRepository(hubDB, hubDB)
		system = userReferralMetricSystemSettings{SystemSettingsRepository: system, repo: userReferralRepo, usage: storesqlite.NewLLMUsageRepository(hubDB, hubDB), billing: storesqlite.NewLLMBillingLedgerRepository(hubDB, hubDB)}
	}
	if identity != nil {
		userLookup = identity.UsersRepo()
		platformUsers = identity.UsersRepo()
		platformMachines = identity.MachinesRepo()
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
	var tenantIMRuntimeStopper TenantIMRuntimeStopper
	if stopper, ok := tenantIMRuntimeReloader.(TenantIMRuntimeStopper); ok {
		tenantIMRuntimeStopper = stopper
	}
	mux := http.NewServeMux()
	ConfigureMobileLLMAuthorizationPersistence(system)
	requireAdmin := func(h http.HandlerFunc) http.HandlerFunc {
		return RequireAdmin(admins, h, tenantRepo)
	}
	requireGlobalAdmin := func(h http.HandlerFunc) http.HandlerFunc {
		return requireAdmin(RequireGlobalAdmin(h))
	}
	requireGlobalAdminAllowTenantQuery := func(h http.HandlerFunc) http.HandlerFunc {
		return requireAdmin(RequireGlobalAdminAllowTenantQuery(h))
	}
	requireCascadeAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return RequireCascadeAuth(admins, system, h, tenantRepo)
	}
	requireTenantAdmin := func(h http.HandlerFunc) http.HandlerFunc {
		return requireAdmin(RequireTenantAdmin(h))
	}
	groupDiscussionSvc := NewGroupDiscussionService(hubDB)
	groupDiscussionSender := platformAwareMachineSender{fallback: deviceSvc, system: system, tenants: tenantRepo, groupSvc: groupDiscussionSvc}
	groupDiscussionHandler := NewGroupDiscussionHandler(groupDiscussionSvc, groupDiscussionSender)
	runtimeDataDir := resolveHubRuntimeDataDir(hubCfg, configPath)
	// Mobile documents / employees / push devices+pending (default path under runtime).
	InitMobileStatePersistence(runtimeDataDir)
	// Mobile full agent (skills/MCP/memory) shares Hub runtime disk under mobile-agent/.
	InitMobileCoreAgent(runtimeDataDir)
	// Document write paths resolve free/paid quota from the same service grants as bootstrap.
	ConfigureMobileDocumentQuota(system, securitySvc)
	// Desktop push: wake online GUI machines when mobile queues digital-employee tasks.
	if deviceSvc != nil {
		ConfigureMobileMachinePush(mobileDevicePushAdapter{svc: deviceSvc})
	}
	knowledgeSharePackageDir := filepath.Join(runtimeDataDir, "knowledge-packages")
	knowledgeSyncPackageDir := filepath.Join(runtimeDataDir, "knowledge-sync")
	welcomeSyncPackageDir := filepath.Join(runtimeDataDir, "welcome-sync")
	virtualRepositorySyncDir := filepath.Join(runtimeDataDir, "virtual-repository-sync")
	StartKnowledgeSyncCleanup(knowledgeSyncPackageDir)

	// Enterprise digital assets.
	// Process config/env only seeds defaults; tenant admins toggle enabled in System Settings (persisted).
	var digitalAssetSvc *digitalasset.Service
	if hubDB != nil {
		settings := digitalasset.DefaultTenantSettings()
		enabled := hubCfg != nil && hubCfg.DigitalAssets.Enabled
		if v := strings.TrimSpace(os.Getenv("MACLAW_DIGITAL_ASSETS_ENABLED")); v == "1" || strings.EqualFold(v, "true") {
			enabled = true
		}
		settings.Enabled = enabled
		host := digitalasset.NewKnowledgeHost(runtimeDataDir, settings.MaxOpenLibraries)
		digitalAssetSvc = &digitalasset.Service{
			Repo:     storesqlite.NewDigitalAssetRepository(hubDB, hubDB),
			Host:     host,
			ACL:      &digitalasset.Evaluator{Groups: digitalasset.SecurityGroupLookup{Service: securitySvc}, AncestorMatch: true},
			Limiter:  digitalasset.NewSyncLimiter(settings.PerUserPullRPM, settings.PerTenantConcurrentPulls),
			System:   system,
			Settings: settings,
			Enabled:  enabled,
		}
	}
	if hubDB != nil && identity != nil {
		NewMigrationAPI(hubDB, runtimeDataDir, identity, identity.MachinesRepo(), system).RegisterRoutes(mux, requireTenantAdmin)
	}
	fileRelay := ve.NewFileRelay(filepath.Join(runtimeDataDir, "ve-files"))
	fileRelay.SetParticipantValidator(groupDiscussionSvc)
	fileRelay.Start(context.Background())
	capabilitySvc := capability.NewService(hubDB)

	// Pre-computed user ranking cache — refreshes every 5 minutes per tenant.
	// Shared by public, admin, and my-ranking handlers for instant response.
	var rankingCache *RankingCache
	if sessionSvc != nil {
		rankingCache = NewRankingCache(sessionSvc, platformUsers, tenantRepo, 5*time.Minute)
		rankingCache.Start()
	}

	mux.HandleFunc("GET /healthz", HealthHandler("maclaw-hub"))
	mux.HandleFunc("GET /healthz/ready", ReadinessHandler("maclaw-hub"))
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(admins))
	groupDiscussionAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			principal, ok := authenticateVEMachine(w, r, identity)
			if !ok {
				return
			}
			participantID := r.URL.Query().Get("participant_id")
			if participantID != "" && !groupDiscussionParticipantIdentityMatches(participantID, principal.MachineID) {
				writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "participant_id must match authenticated machine")
				return
			}
			r.Header.Set("X-Authenticated-Machine-ID", principal.MachineID)
			r = r.WithContext(WithRequestTenant(r.Context(), principal.TenantID))
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
			if participantID != "" && !groupDiscussionParticipantIdentityMatches(participantID, principal.MachineID) {
				writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "participant_id must match authenticated machine")
				return
			}
			r.Header.Set("X-Participant-ID", principal.MachineID)
			h(w, r)
		}
	}
	mux.HandleFunc("POST /api/ve/files/upload", fileRelayAuth(fileRelay.HandleUpload))
	mux.HandleFunc("GET /api/ve/files/download/{id}", fileRelayAuth(fileRelay.HandleDownload))
	mux.HandleFunc("GET /api/ve/history/{discussionID}/attachments/{fileID}", requireTenantAdmin(fileRelay.HandleAdminDownload))
	mux.HandleFunc("GET /api/admin/a2a/group-discussions", requireTenantAdmin(groupDiscussionHandler.handleAdminGroupDiscussions))
	mux.HandleFunc("POST /api/ve/register", VERegisterHandler(system, identity, userLookup))
	mux.HandleFunc("PUT /api/ve/settings", VESettingsHandler(system, identity, userLookup))
	mux.HandleFunc("PUT /api/ve/settings/approval_capability", VEApprovalCapabilityHandler(system, identity))
	mux.HandleFunc("GET /api/ve/status", VEStatusHandler(system, identity))
	mux.HandleFunc("GET /api/ve/reclaimable", VEReclaimableHandler(system, identity, deviceSvc))
	mux.HandleFunc("POST /api/ve/reclaim", VEReclaimHandler(system, identity, userLookup))
	mux.HandleFunc("GET /api/ve/discoverable", VEDiscoverableHandler(system, identity, deviceSvc, veSecurityVisibilityResolver{securitySvc: securitySvc, users: userLookup}))
	mux.HandleFunc("POST /api/ve/{id}/initiate", VEInitiateHandler(system, groupDiscussionSvc, identity, deviceSvc))
	mux.HandleFunc("POST /api/ve/auth/respond", VEAuthRespondHandler(system, identity, deviceSvc))
	mux.HandleFunc("GET /api/ve/list", requireTenantAdmin(VEAdminListHandler(system, deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/ve/metrics", requireTenantAdmin(VEMetricsHandler()))
	mux.HandleFunc("GET /api/admin/adaptive-prompt/metrics", requireTenantAdmin(AdaptivePromptMetricsHandler(deviceSvc)))
	mux.HandleFunc("GET /api/admin/cost-ops/metrics", requireTenantAdmin(CostOpsMetricsHandler(deviceSvc)))
	mux.HandleFunc("GET /api/ve/{id}/history", requireTenantAdmin(VEHistoryHandler(system, groupDiscussionSvc, userLookup, identity.MachinesRepo())))
	mux.HandleFunc("GET /api/ve/history/search", requireTenantAdmin(VEHistorySearchHandler(system, groupDiscussionSvc, userLookup, identity.MachinesRepo())))
	mux.HandleFunc("GET /api/ve/history/{id}/detail", requireTenantAdmin(VEHistoryDetailHandler(system, groupDiscussionSvc, userLookup, identity.MachinesRepo())))
	mux.HandleFunc("PUT /api/ve/config", requireTenantAdmin(VEAdminConfigHandler(system, deviceSvc)))
	mux.HandleFunc("GET /api/ve/config", requireTenantAdmin(VEAdminConfigHandler(system, deviceSvc)))
	mux.HandleFunc("POST /api/ve/{id}/approve", requireTenantAdmin(VEAdminActionHandler(system, "approve", deviceSvc)))
	mux.HandleFunc("POST /api/ve/{id}/reject", requireTenantAdmin(VEAdminActionHandler(system, "reject", deviceSvc)))
	mux.HandleFunc("POST /api/ve/{id}/disable", requireTenantAdmin(VEAdminActionHandler(system, "disable", deviceSvc)))
	mux.HandleFunc("DELETE /api/ve/{id}", requireTenantAdmin(VEAdminActionHandler(system, "delete", deviceSvc)))
	mux.HandleFunc("POST /api/ve/{id}/force-delete", requireTenantAdmin(VEAdminForceDeleteHandler(system, groupDiscussionSvc, admins, deviceSvc)))
	mux.HandleFunc("PUT /api/ve/{id}/resident", requireTenantAdmin(VEAdminResidentHandler(system)))
	mux.HandleFunc("PUT /api/ve/{id}/visibility", requireTenantAdmin(VEAdminVisibilityHandler(system, securitySvc)))
	mux.HandleFunc("POST /api/platform/providers/register", PlatformProviderRegisterHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/providers/diagnose", PlatformProviderDiagnoseHandler(system))
	mux.HandleFunc("POST /api/platform/providers/tenant-domains", PlatformTenantDomainsHandler(system, tenantRepo))
	mux.HandleFunc("GET /api/platform/tenants", PlatformTenantsListHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/tenants", PlatformTenantsListHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/tenants/list", PlatformTenantsListHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/llm/options", PlatformLLMOptionsHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/tenant-admins/list", PlatformTenantAdminsListHandler(system, tenantRepo, admins))
	mux.HandleFunc("POST /api/platform/tenant-admins/authenticate", PlatformTenantAdminAuthenticateHandler(system, tenantRepo, admins))
	mux.HandleFunc("POST /api/platform/source-users/sync", PlatformSourceUsersSyncHandler(system, platformUsers, tenantRepo, platformMachines))
	mux.HandleFunc("POST /api/platform/source-users/{id}/viewer-token", PlatformSourceUserViewerTokenHandler(system, tenantRepo, platformUsers, identity))
	mux.HandleFunc("POST /api/platform/employees", PlatformEmployeeRegisterHandler(system, tenantRepo, platformUsers, identity))
	mux.HandleFunc("POST /api/platform/employees/{id}/viewer-token", PlatformEmployeeViewerTokenHandler(system, tenantRepo, platformUsers, identity))
	mux.HandleFunc("POST /api/platform/employees/{id}/status", PlatformEmployeeStatusHandler(system, tenantRepo))
	mux.HandleFunc("DELETE /api/platform/employees/{id}", PlatformEmployeeDeleteHandler(system, tenantRepo, platformUsers))
	mux.HandleFunc("POST /api/platform/migrations", PlatformMigrationSubmitHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/migrations/{id}/cancel", PlatformMigrationCancelHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/knowledge/imports", PlatformKnowledgeImportHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/knowledge/imports/{id}/cancel", PlatformKnowledgeImportCancelHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/platform/sync/jobs/{id}/run", PlatformSyncJobRunHandler(system, tenantRepo))
	mux.HandleFunc("POST /api/admin/setup", SetupAdminHandler(admins))
	mux.HandleFunc("POST /api/admin/login", AdminLoginHandler(admins, tenantRepo))
	if tenantRepo != nil {
		mux.HandleFunc("GET /api/admin/login/tenants", AdminLoginTenantsHandler(tenantRepo))
	}
	mux.HandleFunc("POST /api/admin/password", requireAdmin(AdminChangePasswordHandler(admins)))
	mux.HandleFunc("POST /api/admin/profile", requireAdmin(AdminUpdateProfileHandler(admins, centerSvc)))
	if tenantRepo != nil {
		mux.HandleFunc("GET /api/admin/tenants", requireGlobalAdmin(AdminTenantsListWithAuthHandler(tenantRepo, centerSvc, nil, hubDB)))
		mux.HandleFunc("POST /api/admin/tenants", requireGlobalAdmin(AdminTenantCreateWithPlatformCallbackHandler(system, tenantRepo, admins, adminAudit, centerSvc)))
		mux.HandleFunc("GET /api/admin/tenants/{tenantId}", requireAdmin(AdminTenantDetailHandler(tenantRepo)))
		mux.HandleFunc("PATCH /api/admin/tenants/{tenantId}/domains", requireAdmin(AdminTenantDomainsUpdateWithPlatformCallbackHandler(system, tenantRepo, adminAudit)))
		mux.HandleFunc("PATCH /api/admin/tenants/{tenantId}/status", requireGlobalAdmin(AdminTenantStatusUpdateWithPlatformCallbackHandler(system, adminAudit, tenantRepo, tenantIMRuntimeStopper)))
		mux.HandleFunc("POST /api/admin/tenants/{tenantId}/merge", requireGlobalAdmin(AdminTenantMergeHandler(hubDB, tenantRepo, adminAudit, tenantIMRuntimeStopper)))
		mux.HandleFunc("DELETE /api/admin/tenants/{tenantId}", requireGlobalAdmin(AdminTenantDeleteWithPlatformCallbackHandler(system, adminAudit, admins, hubDB, centerSvc, tenantRepo, tenantIMRuntimeStopper)))
		mux.HandleFunc("POST /api/admin/tenants/{tenantId}/admins", requireAdmin(AdminTenantAdminCreateHandler(tenantRepo, admins, adminAudit, centerSvc)))
		// Semantic tool routing (mobile core agent): owner-only dynamic capability
		// contract publication for observed MCP/Skill bindings, aligned with the
		// MaClawSrv admin routes. Hub "owner" maps to the global admin scope.
		mux.HandleFunc("POST /api/admin/tenants/{tenantId}/users/{userId}/dynamic-capabilities/mcp/{serverId}/{toolName}", requireGlobalAdmin(mobilePublishDynamicMCPContractHandler(adminAudit)))
		mux.HandleFunc("POST /api/admin/tenants/{tenantId}/users/{userId}/dynamic-capabilities/skills/{stableId}", requireGlobalAdmin(mobilePublishDynamicSkillContractHandler(adminAudit)))
		// The out-of-band exit for an operation that ended unknown. The
		// operation ledger supplies tenant, user and binding, so the path
		// carries only the operation it is aimed at.
		mux.HandleFunc("POST /api/admin/dynamic-effects/{operationId}/resolve", requireGlobalAdmin(mobileResolveUnknownDynamicEffectHandler(adminAudit)))
	}
	mux.HandleFunc("GET /api/admin/debug/machines", requireAdmin(DebugListMachinesHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/machine-events", requireAdmin(DebugListMachineEventsHandler(deviceSvc)))
	mux.HandleFunc("DELETE /api/admin/machines", requireAdmin(DeleteMachineHandler(deviceSvc)))
	mux.HandleFunc("POST /api/admin/machines/rename", requireAdmin(RenameMachineHandler(deviceSvc)))
	mux.HandleFunc("POST /api/admin/machines/clear-offline", requireAdmin(ClearOfflineMachinesHandler(deviceSvc)))
	mux.HandleFunc("DELETE /api/admin/machines/by-email", requireAdmin(DeleteMachinesByEmailHandler(deviceSvc, userLookup)))
	mux.HandleFunc("DELETE /api/admin/machines/force-by-email", requireAdmin(ForceDeleteMachinesByEmailHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/sessions", requireAdmin(DebugListSessionsHandler(sessionSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/session", requireAdmin(DebugGetSessionHandler(sessionSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/failure-logs", requireAdmin(ListFailureLogsHandler(failureLogs)))
	mux.HandleFunc("GET /api/admin/knowledge/shares", requireAdmin(ListKnowledgeSharesAdminHandler(knowledgeShares)))
	mux.HandleFunc("DELETE /api/admin/knowledge/shares/{knowledgeID}", requireAdmin(ForceDeleteKnowledgeShareAdminHandler(knowledgeShares, adminAudit)))
	// Digital assets (enterprise knowledge libraries). Routes always registered; handlers 404 when disabled.
	if digitalAssetSvc != nil {
		shareLoader := digitalasset.KnowledgeShareFileLoader{
			Repo: knowledgeShares, PackageDir: knowledgeSharePackageDir,
		}
		mux.HandleFunc("GET /api/admin/digital-assets/libraries", requireTenantAdmin(ListDigitalAssetLibrariesAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries", requireTenantAdmin(CreateDigitalAssetLibraryAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("PATCH /api/admin/digital-assets/libraries/{id}", requireTenantAdmin(PatchDigitalAssetLibraryAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("DELETE /api/admin/digital-assets/libraries/{id}", requireTenantAdmin(DeleteDigitalAssetLibraryAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/libraries/{id}/search", requireTenantAdmin(SearchDigitalAssetLibraryAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/libraries/{id}/sources", requireTenantAdmin(ListDigitalAssetLibrarySourcesAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("DELETE /api/admin/digital-assets/libraries/{id}/sources/{source_id}", requireTenantAdmin(DeleteDigitalAssetLibrarySourceAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries/{id}/sources/delete", requireTenantAdmin(DeleteDigitalAssetLibrarySourcesBatchAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/libraries/{id}/import-jobs", requireTenantAdmin(ListDigitalAssetLibraryImportJobsAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries/{id}/import/upload", requireTenantAdmin(ImportDigitalAssetUploadAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries/{id}/import/archive", requireTenantAdmin(ImportDigitalAssetArchiveAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries/{id}/import/local-dir", requireTenantAdmin(ImportDigitalAssetLocalDirAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries/{id}/import/browser-dir", requireTenantAdmin(ImportDigitalAssetBrowserDirAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries/{id}/import/knowledge-share", requireTenantAdmin(ImportDigitalAssetKnowledgeShareAdminHandler(digitalAssetSvc, shareLoader)))
		mux.HandleFunc("POST /api/admin/digital-assets/libraries/merge", requireTenantAdmin(MergeDigitalAssetLibrariesAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/export", requireTenantAdmin(ExportDigitalAssetBackupAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/export/jobs/{job_id}/download", requireTenantAdmin(DownloadDigitalAssetBackupAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/import/backup", requireTenantAdmin(ImportDigitalAssetBackupAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/import/jobs/{job_id}", requireTenantAdmin(GetDigitalAssetImportJobAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/settings", requireTenantAdmin(GetDigitalAssetSettingsAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("PUT /api/admin/digital-assets/settings", requireTenantAdmin(PutDigitalAssetSettingsAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/submissions", requireTenantAdmin(ListDigitalAssetSubmissionsAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/admin/digital-assets/submissions/{id}", requireTenantAdmin(GetDigitalAssetSubmissionAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/submissions/{id}/approve", requireTenantAdmin(ApproveDigitalAssetSubmissionAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("POST /api/admin/digital-assets/submissions/{id}/reject", requireTenantAdmin(RejectDigitalAssetSubmissionAdminHandler(digitalAssetSvc)))
		mux.HandleFunc("GET /api/digital-assets/libraries", ListDigitalAssetLibrariesUserHandler(digitalAssetSvc, identity))
		mux.HandleFunc("GET /api/digital-assets/libraries/contributable", ListDigitalAssetContributableLibrariesHandler(digitalAssetSvc, identity))
		mux.HandleFunc("POST /api/digital-assets/submissions", CreateDigitalAssetSubmissionHandler(digitalAssetSvc, identity))
		mux.HandleFunc("GET /api/digital-assets/submissions", ListMyDigitalAssetSubmissionsHandler(digitalAssetSvc, identity))
		mux.HandleFunc("POST /api/digital-assets/submissions/{id}/withdraw", WithdrawDigitalAssetSubmissionHandler(digitalAssetSvc, identity))
		mux.HandleFunc("GET /api/digital-assets/sync/manifest", DigitalAssetSyncManifestHandler(digitalAssetSvc, identity))
		mux.HandleFunc("POST /api/digital-assets/sync/pull", DigitalAssetSyncPullHandler(digitalAssetSvc, identity))
		mux.HandleFunc("GET /api/digital-assets/libraries/{id}/sync/packages/{rev}", DigitalAssetSyncPackageHandler(digitalAssetSvc, identity))
	}
	cloudWorkspaceSvc := cloudworkspace.NewService(system, platformUsers, securitySvc)
	if hubDB != nil {
		cloudWorkspaceSvc.Workspaces = cloudworkspace.NewStore(hubDB)
		blobRoot := filepath.Join(runtimeDataDir, "cloud-workspaces")
		cloudWorkspaceSvc.Blobs = &cloudworkspace.BlobStore{Root: blobRoot, KeyDir: blobRoot, DB: hubDB}
	}
	mux.HandleFunc("GET /api/admin/cloud-workspaces/settings", requireTenantAdmin(GetCloudWorkspaceSettingsAdminHandler(cloudWorkspaceSvc)))
	mux.HandleFunc("PUT /api/admin/cloud-workspaces/settings", requireTenantAdmin(PutCloudWorkspaceSettingsAdminHandler(cloudWorkspaceSvc, adminAudit)))
	mux.HandleFunc("GET /api/v1/cloud-workspaces/entitlement", CloudWorkspaceEntitlementHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("POST /api/v1/cloud-workspaces", CloudWorkspaceCreateHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("PATCH /api/v1/cloud-workspaces/{id}", CloudWorkspaceRenameHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("DELETE /api/v1/cloud-workspaces/{id}", CloudWorkspaceDeleteHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/restore", CloudWorkspaceRestoreHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/leases", CloudWorkspaceAcquireLeaseHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/leases/{lease_id}/heartbeat", CloudWorkspaceHeartbeatLeaseHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("DELETE /api/v1/cloud-workspaces/{id}/leases/{lease_id}", CloudWorkspaceReleaseLeaseHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("GET /api/v1/cloud-workspaces/{id}/manifest", CloudWorkspaceGetManifestHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("PUT /api/v1/cloud-workspaces/{id}/manifest", CloudWorkspacePutManifestHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("GET /api/v1/cloud-workspaces/{id}/objects/{sha256}", CloudWorkspaceGetObjectHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("PUT /api/v1/cloud-workspaces/{id}/objects/{sha256}", CloudWorkspacePutObjectHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("PUT /api/v1/cloud-workspaces/{id}/objects/{sha256}/chunks/{index}", CloudWorkspacePutObjectChunkHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/objects/{sha256}/complete", CloudWorkspaceCompleteObjectHandler(cloudWorkspaceSvc, identity))
	mux.HandleFunc("GET /api/knowledge/shares/mine", ListMyKnowledgeSharesHandler(knowledgeShares, identity))
	mux.HandleFunc("POST /api/knowledge/shares", CreateKnowledgeShareHandler(knowledgeShares, identity, knowledgeSharePackageDir))
	mux.HandleFunc("GET /api/knowledge/shares/{knowledgeID}", GetKnowledgeSharePublicHandler(knowledgeShares, identity))
	mux.HandleFunc("PATCH /api/knowledge/shares/{knowledgeID}", UpdateMyKnowledgeShareHandler(knowledgeShares, identity, knowledgeSharePackageDir))
	mux.HandleFunc("DELETE /api/knowledge/shares/{knowledgeID}", DeleteMyKnowledgeShareHandler(knowledgeShares, identity))
	mux.HandleFunc("GET /api/knowledge/shares/{knowledgeID}/package", DownloadKnowledgeSharePackageHandler(knowledgeShares, identity, knowledgeSharePackageDir))
	mux.HandleFunc("GET /hub/knowledge/shares/{knowledgeID}", KnowledgeSharePublicPageHandler(knowledgeShares, identity))
	mux.HandleFunc("GET /api/knowledge/sync/status", KnowledgeSyncStatusHandler(identity, knowledgeSyncPackageDir, centerSvc))
	mux.HandleFunc("PUT /api/knowledge/sync/package", UploadKnowledgeSyncPackageHandler(identity, knowledgeSyncPackageDir, centerSvc))
	mux.HandleFunc("GET /api/knowledge/sync/package", DownloadKnowledgeSyncPackageHandler(identity, knowledgeSyncPackageDir, centerSvc))
	mux.HandleFunc("DELETE /api/knowledge/sync/package", DeleteKnowledgeSyncPackageHandler(identity, knowledgeSyncPackageDir))
	// Welcome templates / role / recent — small JSON blob per signed-in Hub user.
	mux.HandleFunc("GET /api/welcome/sync/status", WelcomeSyncStatusHandler(identity, welcomeSyncPackageDir))
	mux.HandleFunc("PUT /api/welcome/sync", UploadWelcomeSyncHandler(identity, welcomeSyncPackageDir))
	mux.HandleFunc("GET /api/welcome/sync", DownloadWelcomeSyncHandler(identity, welcomeSyncPackageDir))
	mux.HandleFunc("DELETE /api/welcome/sync", DeleteWelcomeSyncHandler(identity, welcomeSyncPackageDir))
	// Per-user encrypted virtual repository definitions and credentials.
	mux.HandleFunc("GET /api/virtual-repositories/sync", VirtualRepositorySyncHandler(identity, virtualRepositorySyncDir))
	mux.HandleFunc("PUT /api/virtual-repositories/sync", VirtualRepositorySyncHandler(identity, virtualRepositorySyncDir))
	mux.HandleFunc("GET /api/admin/sessions/all", requireAdmin(AdminListAllSessionsHandler(sessionSvc, userLookup)))
	mux.HandleFunc("POST /api/admin/users/manual-bind", requireAdmin(ManualBindHandler(identity)))
	mux.HandleFunc("GET /api/admin/users", requireAdmin(ListUsersHandler(identity, system, securitySvc, userReferralRepo)))
	// Shared purger: single source of truth for "what data does a user leave behind".
	userPurger := &UserDataPurger{
		Identity:                 identity,
		DeviceSvc:                deviceSvc,
		InvitationSvc:            invitationSvc,
		FeishuNotify:             feishuNotifier,
		IMCleaners:               imCleaners,
		SecuritySvc:              securitySvc,
		System:                   system,
		DB:                       hubDB,
		RouteDeleter:             centerSvc,
		GroupDiscussionSvc:       groupDiscussionSvc,
		KnowledgeSharePackageDir: knowledgeSharePackageDir,
		KnowledgeSyncDir:         knowledgeSyncPackageDir,
		WelcomeSyncDir:           welcomeSyncPackageDir,
		VirtualRepositorySyncDir: virtualRepositorySyncDir,
		UserDataMigrationDir:     filepath.Join(runtimeDataDir, "user-data-migrations"),
	}
	if chatFileSvc != nil {
		userPurger.ChatFileDir = chatFileSvc.DataDir()
	}
	mux.HandleFunc("DELETE /api/admin/users", requireAdmin(DeleteBoundUserHandler(identity, userPurger, system)))
	mux.HandleFunc("POST /api/admin/users/force-delete-virtual", requireAdmin(ForceDeleteVirtualBoundUserHandler(admins, identity, userPurger, system)))
	mux.HandleFunc("GET /api/admin/users/lookup", requireAdmin(LookupUserHandler(identity)))
	mux.HandleFunc("GET /api/admin/blocklist", requireAdmin(ListBlockedEmailsHandler(identity)))
	mux.HandleFunc("POST /api/admin/blocklist", requireAdmin(AddBlockedEmailHandler(identity)))
	mux.HandleFunc("DELETE /api/admin/blocklist/{email}", requireAdmin(RemoveBlockedEmailHandler(identity)))
	// Email invite routes (restored)
	mux.HandleFunc("POST /api/admin/invites", requireAdmin(CreateEmailInviteHandler(emailInviteRepo)))
	mux.HandleFunc("GET /api/admin/invites", requireAdmin(ListEmailInvitesHandler(emailInviteRepo)))
	mux.HandleFunc("DELETE /api/admin/invites/{id}", requireAdmin(DeleteEmailInviteHandler(emailInviteRepo)))
	mux.HandleFunc("POST /api/admin/invitation-codes/generate", requireAdmin(GenerateInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes", requireAdmin(ListInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("POST /api/admin/invitation-codes/toggle", requireAdmin(ToggleInvitationCodeHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/status", requireAdmin(InvitationCodeStatusHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/export", requireAdmin(ExportInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("POST /api/admin/invitation-codes/unbind", requireAdmin(UnbindInvitationCodeHandlerWithPurger(invitationSvc, identity, userPurger)))
	mux.HandleFunc("GET /api/admin/enrollments/pending", requireAdmin(ListPendingEnrollmentsHandler(identity)))
	mux.HandleFunc("GET /api/admin/enrollments/all", requireAdmin(ListAllEnrollmentsHandler(identity)))
	mux.HandleFunc("POST /api/admin/enrollments/approve", requireAdmin(ApproveEnrollmentHandler(identity, securitySvc)))
	mux.HandleFunc("POST /api/admin/enrollments/reject", requireAdmin(RejectEnrollmentHandler(identity)))
	mux.HandleFunc("GET /api/admin/pending-logins", requireAdmin(ListPendingLoginsHandler(identity)))
	mux.HandleFunc("POST /api/admin/pending-logins/confirm", requireAdmin(AdminConfirmLoginHandler(identity)))
	mux.HandleFunc("GET /api/admin/center/status", requireGlobalAdminAllowTenantQuery(GetCenterStatusHandler(centerSvc)))
	mux.HandleFunc("POST /api/center/user-migration/export", CenterUserMigrationExportHandler(centerSvc, identity, deviceSvc))
	mux.HandleFunc("POST /api/center/user-migration/import", CenterUserMigrationImportHandler(centerSvc, identity, deviceSvc))
	mux.HandleFunc("POST /api/center/user-migration/delete", CenterUserMigrationDeleteHandler(centerSvc, identity, deviceSvc, invitationSvc, feishuNotifier, imCleaners, userPurger))
	mux.HandleFunc("POST /api/center/skillmarket-authenticate", CenterSkillMarketAuthenticateHandler(centerSvc, identity))
	mux.HandleFunc("POST /api/admin/center/config", requireGlobalAdmin(UpdateCenterConfigHandler(centerSvc, identity, func(url string) {
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
	mux.HandleFunc("GET /api/admin/mail/config", requireGlobalAdmin(GetMailConfigHandler(mailer)))
	mux.HandleFunc("POST /api/admin/mail/config", requireGlobalAdmin(UpdateMailConfigHandler(mailer)))
	mux.HandleFunc("GET /api/admin/mail/sender-name", requireTenantAdmin(GetTenantMailSenderNameHandler(system)))
	mux.HandleFunc("POST /api/admin/mail/sender-name", requireTenantAdmin(UpdateTenantMailSenderNameHandler(system)))
	// Registration verification is tenant policy. Global admins must not read or
	// modify a tenant's registration method or SMS credentials.
	mux.HandleFunc("GET /api/admin/settings/registration-auth", requireTenantAdmin(GetRegistrationAuthConfigHandler(system)))
	mux.HandleFunc("PUT /api/admin/settings/registration-auth", requireTenantAdmin(UpdateRegistrationAuthConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/center/register", requireGlobalAdmin(RegisterCenterHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/mail/test", requireGlobalAdmin(AdminSendTestMailHandler(mailer)))
	mux.HandleFunc("GET /api/admin/feishu/config", requireTenantAdmin(GetFeishuConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/feishu/config", requireTenantAdmin(UpdateFeishuConfigHandler(system, feishuNotifier)))
	mux.HandleFunc("GET /api/admin/feishu/bindings", requireTenantAdmin(GetFeishuBindingsHandler(feishuNotifier)))
	mux.HandleFunc("DELETE /api/admin/feishu/bindings", requireTenantAdmin(DeleteFeishuBindingHandler(feishuNotifier)))
	mux.HandleFunc("GET /api/admin/feishu/auto-enroll", requireTenantAdmin(GetFeishuAutoEnrollHandler(system)))
	mux.HandleFunc("POST /api/admin/feishu/auto-enroll", requireTenantAdmin(UpdateFeishuAutoEnrollHandler(system, feishuNotifier)))
	mux.HandleFunc("GET /api/admin/settings/openclaw_im", requireTenantAdmin(GetOpenclawIMConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/openclaw_im", requireTenantAdmin(UpdateOpenclawIMConfigHandler(system, bridgeDir)))
	mux.HandleFunc("POST /api/admin/settings/openclaw_im/test", requireTenantAdmin(TestOpenclawIMWebhookHandler(system)))
	mux.HandleFunc("POST /api/openclaw_im/webhook", OpenclawIMWebhookHandler(system, openclawIMPlugin))
	// Bridge channel management
	mux.HandleFunc("GET /api/admin/bridge/channels", requireTenantAdmin(GetBridgeChannelsHandler(system, bridgeDir)))
	mux.HandleFunc("POST /api/admin/bridge/channels", requireTenantAdmin(SaveBridgeChannelHandler(system, bridgeDir)))
	mux.HandleFunc("GET /api/admin/bridge/status", requireTenantAdmin(BridgeStatusHandler(system)))
	mux.HandleFunc("POST /api/admin/bridge/install", requireGlobalAdmin(InstallBridgeDepsHandler(bridgeDir)))
	// LLM prompt cache compatibility/operations APIs. There is no standalone
	// global-admin UI; model endpoints are tenant-scoped under /api/admin/llm.
	mux.HandleFunc("GET /api/admin/hub_llm_prompt_cache_config", requireGlobalAdmin(GetHubLLMPromptCacheConfigHandler(system, llmPromptCache)))
	mux.HandleFunc("PUT /api/admin/hub_llm_prompt_cache_config", requireGlobalAdmin(UpdateHubLLMPromptCacheConfigHandler(system, llmPromptCache)))
	mux.HandleFunc("POST /api/admin/hub_llm_prompt_cache_clear", requireGlobalAdmin(ClearHubLLMPromptCacheHandler(llmPromptCache)))
	mux.HandleFunc("GET /api/admin/hub_llm_prompt_cache_entries", requireGlobalAdmin(GetHubLLMPromptCacheEntriesHandler(llmPromptCache)))
	mux.HandleFunc("GET /api/admin/hub_llm_prompt_cache_entry", requireGlobalAdmin(GetHubLLMPromptCacheEntryHandler(llmPromptCache)))
	mux.HandleFunc("DELETE /api/admin/hub_llm_prompt_cache_entry", requireGlobalAdmin(DeleteHubLLMPromptCacheEntryHandler(llmPromptCache)))
	mux.HandleFunc("GET /api/admin/hub_llm_status", requireGlobalAdmin(HubLLMStatusHandler(hubLLMStatusFn, system, llmPromptCache)))
	mux.HandleFunc("GET /api/admin/llm/services/diagnose", requireTenantAdmin(GetLLMServiceEntitlementDiagnosticHandler(system, securitySvc)))
	mux.HandleFunc("GET /api/admin/llm/providers", requireTenantAdmin(GetLLMProvidersHandler(system, GetMaClawAccessControl())))
	mux.HandleFunc("PUT /api/admin/llm/providers", requireTenantAdmin(UpdateLLMProvidersHandler(system, GetMaClawAccessControl())))
	mux.HandleFunc("POST /api/admin/llm/providers/test", requireTenantAdmin(TestLLMProviderHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/providers/test-key", requireTenantAdmin(GenerateLLMProviderTestKeyHandler(identity)))
	mux.HandleFunc("GET /api/admin/llm/maclaw-compute-status", requireTenantAdmin(MaClawComputeStatusHandler(centerSvc, nil)))
	mux.HandleFunc("GET /api/admin/llm/services", requireTenantAdmin(GetLLMServicesAdminHandler(system)))
	mux.HandleFunc("PUT /api/admin/llm/services", requireTenantAdmin(UpdateLLMServicesAdminHandler(system, securitySvc, adminAudit)))
	mux.HandleFunc("GET /api/admin/llm/system-free", requireTenantAdmin(GetSystemFreeLLMHandler(system)))
	mux.HandleFunc("PUT /api/admin/llm/system-free", requireTenantAdmin(UpdateSystemFreeLLMHandler(system, adminAudit)))
	mux.HandleFunc("POST /api/admin/llm/system-free/test", requireTenantAdmin(TestSystemFreeLLMHandler(system)))
	configAgentDeps := ConfigAgentDeps{
		System:        system,
		Audit:         adminAudit,
		Invites:       emailInviteRepo,
		Identity:      identity,
		Codes:         invitationSvc,
		Security:      securitySvc,
		Feishu:        feishuNotifier,
		WeCom:         wecomPlugin,
		DingTalk:      dingtalkPlugin,
		QQBot:         qqbotPlugin,
		IMRuntime:     tenantIMRuntimeReloader,
		BridgeDir:     bridgeDir,
		DigitalAssets: digitalAssetSvc,
	}
	mux.HandleFunc("POST /api/admin/config-agent/plan", requireTenantAdmin(ConfigAgentPlanHandler(configAgentDeps)))
	mux.HandleFunc("POST /api/admin/config-agent/execute", requireTenantAdmin(ConfigAgentExecuteHandler(configAgentDeps)))
	mux.HandleFunc("GET /api/admin/config-agent/history", requireTenantAdmin(ConfigAgentHistoryHandler(adminAudit)))
	mux.HandleFunc("GET /api/admin/config-agent/catalog", requireTenantAdmin(ConfigAgentCatalogHandler()))
	mux.HandleFunc("POST /api/admin/llm/service-cards", requireTenantAdmin(CreateLLMServiceCardHandler(system, adminAudit)))
	mux.HandleFunc("GET /api/admin/llm/service-cards", requireTenantAdmin(ListLLMServiceCardsHandler(system)))
	mux.HandleFunc("GET /api/admin/llm/service-cards/export", requireTenantAdmin(ExportLLMServiceCardsHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/service-cards/export-selected", requireTenantAdmin(ExportSelectedLLMServiceCardsHandler(system)))
	mux.HandleFunc("DELETE /api/admin/llm/service-cards/{id}", requireTenantAdmin(DeleteLLMServiceCardHandler(system, adminAudit)))
	mux.HandleFunc("POST /api/admin/llm/service-cards/delete-batch", requireTenantAdmin(DeleteLLMServiceCardsBatchHandler(system, adminAudit)))
	mux.HandleFunc("DELETE /api/admin/llm/service-grants/{id}", requireTenantAdmin(DeleteLLMServiceGrantHandler(system, adminAudit)))
	mux.HandleFunc("GET /api/admin/llm/usage-report", requireTenantAdmin(GetLLMUsageReportHandler(system, securitySvc)))
	mux.HandleFunc("GET /api/admin/llm/class-traffic", requireTenantAdmin(GetLLMClassTrafficHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/classify-preview", requireTenantAdmin(PostLLMClassifyPreviewHandler(system)))
	bindClassHeadSettings(system)
	if hubCfg != nil {
		peers := make([]string, 0, len(hubCfg.Replica.Peers))
		urls := map[string]string{}
		for _, peer := range hubCfg.Replica.Peers {
			id := strings.TrimSpace(peer.ID)
			if id == "" {
				continue
			}
			peers = append(peers, id)
			if u := strings.TrimSpace(peer.URL); u != "" {
				urls[id] = u
			}
		}
		local := strings.TrimSpace(hubCfg.Replica.NodeID)
		if local == "" {
			local = "local"
		}
		bindClassHeadRoster(local, peers, urls, hubCfg.Replica.SharedSecret)
	}
	mux.HandleFunc("GET /api/admin/llm/class-head", requireTenantAdmin(GetLLMClassHeadHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/train", requireTenantAdmin(PostLLMClassHeadTrainHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/pipeline", requireTenantAdmin(PostLLMClassHeadPipelineHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/review", requireTenantAdmin(PostLLMClassHeadReviewHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/rollback", requireTenantAdmin(PostLLMClassHeadRollbackHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/pull-official", requireTenantAdmin(PostLLMClassHeadPullOfficialHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/trainer", requireTenantAdmin(PostLLMClassHeadTrainerHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/score", requireTenantAdmin(PostLLMClassHeadScoreHandler(system)))
	mux.HandleFunc("POST /api/admin/llm/class-head/distribute", requireTenantAdmin(PostLLMClassHeadDistributeHandler(system)))
	mux.HandleFunc("POST /api/internal/llm/class-head/apply", PostInternalClassHeadApplyHandler(system))
	mux.HandleFunc("GET /api/admin/llm/access-logs", requireTenantAdmin(GetLLMEndpointAccessLogsHandler(system)))
	mux.HandleFunc("GET /api/admin/card-store/config", requireTenantAdmin(GetCardStoreConfigHandler(system)))
	mux.HandleFunc("PUT /api/admin/card-store/config", requireTenantAdmin(UpdateCardStoreConfigHandler(system, adminAudit)))
	cardStoreQRDir := filepath.Join(resolveHubRuntimeDataDir(hubCfg, configPath), "card-store", "payment-qr")
	mux.HandleFunc("POST /api/admin/card-store/payment-qr/upload", requireTenantAdmin(AdminCardStorePaymentQRUploadHandler(cardStoreQRDir)))
	mux.HandleFunc("GET /api/admin/card-store/sales", requireTenantAdmin(GetCardStoreSalesStatsHandler(system)))
	mux.HandleFunc("POST /api/admin/card-store/orders/{orderNo}/complete", requireTenantAdmin(AdminCompleteCardStoreOrderHandler(system, mailer, identity, adminAudit)))
	mux.HandleFunc("DELETE /api/admin/card-store/orders/{orderNo}", requireTenantAdmin(AdminDeleteCardStoreOrderHandler(system, adminAudit)))
	mux.HandleFunc("POST /api/admin/card-store/orders/{orderNo}/approve", requireTenantAdmin(AdminApproveCardStorePersonalOrderHandler(system, mailer, identity, adminAudit)))
	mux.HandleFunc("POST /api/admin/card-store/orders/{orderNo}/reject", requireTenantAdmin(AdminRejectCardStorePersonalOrderHandler(system, adminAudit)))
	mux.HandleFunc("GET /api/admin/model_download/status", requireGlobalAdmin(GetAdminModelDownloadStatusHandler(configPath)))
	mux.HandleFunc("POST /api/admin/model_download/trigger", requireGlobalAdmin(TriggerAdminModelDownloadHandler(configPath)))
	mux.HandleFunc("GET /api/llm/service/status", GetLLMServiceStatusHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/llm/service/account", GetLLMServiceAccountHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/mobile/bootstrap", MobileBootstrapHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/mobile/entitlements/caps", MobileEntitlementsCapsHandler(identity, system, securitySvc))
	mux.HandleFunc("PUT /api/mobile/entitlements/caps", MobileEntitlementsCapsHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/mobile/realtime", MobileRealtimeHandler(identity))
	// Mobile push: device registry + offline pending completion sync (+ optional webhook/FCM).
	mux.HandleFunc("GET /api/mobile/push/devices", MobilePushDevicesHandler(identity))
	mux.HandleFunc("POST /api/mobile/push/devices", MobilePushDevicesHandler(identity))
	mux.HandleFunc("DELETE /api/mobile/push/devices", MobilePushDevicesHandler(identity))
	mux.HandleFunc("GET /api/mobile/push/pending", MobilePushPendingHandler(identity))
	mux.HandleFunc("POST /api/mobile/push/pending/ack", MobilePushPendingAckHandler(identity))
	mux.HandleFunc("POST /api/mobile/llm/desktop-qr-sessions", MobileLLMDesktopQRSessionHandler(identity))
	mux.HandleFunc("POST /api/mobile/llm/desktop-qr-authorizations", MobileLLMDesktopQRAuthorizationHandler(identity))
	mux.HandleFunc("DELETE /api/mobile/llm/desktop-qr-authorizations", MobileLLMDesktopQRAuthorizationRevokeHandler(identity))
	mux.HandleFunc("POST /api/mobile/auth/desktop-qr-sessions", MobileDesktopAuthQRSessionHandler(identity))
	mux.HandleFunc("POST /api/mobile/search", MobileSearchHandler(identity, LLMV1ChatCompletionsHandler(identity, system, securitySvc, llmPromptCache)))
	mux.HandleFunc("GET /api/mobile/jobs", MobileJobsHandler(identity))
	// Hub SSH credential vault (hub_exec path).
	mux.HandleFunc("GET /api/mobile/ssh/vault", MobileSSHVaultListHandler(identity))
	mux.HandleFunc("GET /api/mobile/ssh/vault/{profileId}", MobileSSHVaultHandler(identity))
	mux.HandleFunc("PUT /api/mobile/ssh/vault/{profileId}", MobileSSHVaultHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/vault/{profileId}", MobileSSHVaultHandler(identity))
	mux.HandleFunc("DELETE /api/mobile/ssh/vault/{profileId}", MobileSSHVaultHandler(identity))
	// Zero-management path: host + username + password → AI assistant ssh ready.
	mux.HandleFunc("POST /api/mobile/ssh/quick-connect", MobileSSHQuickConnectHandler(identity))
	// Long-running official assistant jobs (async upgrade path).
	mux.HandleFunc("GET /api/mobile/agent/jobs", MobileAgentJobsHandler(identity, LLMV1ChatCompletionsHandler(identity, system, securitySvc, llmPromptCache)))
	mux.HandleFunc("POST /api/mobile/agent/jobs", MobileAgentJobsHandler(identity, LLMV1ChatCompletionsHandler(identity, system, securitySvc, llmPromptCache)))
	mux.HandleFunc("GET /api/mobile/agent/jobs/{jobId}", MobileAgentJobsHandler(identity, LLMV1ChatCompletionsHandler(identity, system, securitySvc, llmPromptCache)))
	// Meeting recordings use a resumable audio protocol, separate from the
	// 25 MiB document-import path.
	mux.HandleFunc("POST /api/mobile/meeting-recordings", MobileMeetingRecordingsHandler(identity))
	mux.HandleFunc("GET /api/mobile/meeting-recordings/capabilities", MobileMeetingRecordingCapabilitiesHandler(identity))
	mux.HandleFunc("GET /api/mobile/meeting-recordings/{recordingId}", MobileMeetingRecordingsHandler(identity))
	mux.HandleFunc("GET /api/mobile/meeting-recordings/{recordingId}/audio", MobileMeetingRecordingAudioHandler(identity))
	mux.HandleFunc("DELETE /api/mobile/meeting-recordings/{recordingId}", MobileMeetingRecordingsHandler(identity))
	mux.HandleFunc("DELETE /api/mobile/meeting-recordings/{recordingId}/audio", MobileMeetingRecordingsHandler(identity))
	mux.HandleFunc("PUT /api/mobile/meeting-recordings/{recordingId}/chunks/{chunkIndex}", MobileMeetingRecordingsHandler(identity))
	mux.HandleFunc("POST /api/mobile/meeting-recordings/{recordingId}/complete", MobileMeetingRecordingsHandler(identity))
	mux.HandleFunc("POST /api/mobile/meeting-recordings/{recordingId}/process", MobileMeetingRecordingsHandler(identity))
	mux.HandleFunc("GET /api/mobile/agent/mcp", MobileAgentMCPHandler(identity))
	mux.HandleFunc("PUT /api/mobile/agent/mcp", MobileAgentMCPHandler(identity))
	mux.HandleFunc("GET /api/mobile/agent/mcp/health", MobileAgentMCPHealthHandler(identity))
	mux.HandleFunc("POST /api/mobile/agent/mcp/health", MobileAgentMCPHealthHandler(identity))
	mux.HandleFunc("GET /api/mobile/agent/skills", MobileAgentSkillsHandler(identity))
	mux.HandleFunc("POST /api/mobile/agent/skills/reseed", MobileAgentSkillsHandler(identity))
	mux.HandleFunc("GET /api/mobile/agent/knowledge/status", MobileAgentKnowledgeStatusHandler(identity))
	mux.HandleFunc("POST /api/mobile/agent/knowledge/ingest", MobileAgentKnowledgeIngestHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/drafts", MobileDocumentDraftsListHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/drafts/{draftId}", MobileDocumentDraftsListHandler(identity))
	mux.HandleFunc("GET /api/mobile/library/items", MobileLibraryItemsHandler(identity))
	mux.HandleFunc("GET /api/mobile/library/items/{itemId}", MobileLibraryItemsHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/drafts/{draftId}/source", MobileDocumentDraftSourceHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/drafts/{draftId}/images/{imageId}", MobileDocumentDraftImageHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/quota", MobileDocumentQuotaHandler(identity))
	mux.HandleFunc("POST /api/mobile/documents/drafts", MobileDocumentDraftHandler(identity))
	mux.HandleFunc("PATCH /api/mobile/documents/drafts/{draftId}", MobileDocumentDraftUpdateHandler(identity))
	mux.HandleFunc("DELETE /api/mobile/documents/drafts/{draftId}", MobileDocumentDraftUpdateHandler(identity))
	mux.HandleFunc("POST /api/mobile/documents/drafts/{draftId}/process", MobileDocumentProcessHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/process-jobs/{jobId}", MobileDocumentProcessJobStatusHandler(identity))
	mux.HandleFunc("POST /api/mobile/documents/upload", MobileDocumentUploadHandler(identity))
	mux.HandleFunc("POST /api/mobile/documents/upload/claim", MobileDocumentUploadClaimHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/upload/{taskId}", MobileDocumentUploadStatusHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/upload/{taskId}/source", MobileDocumentUploadSourceHandler(identity))
	mux.HandleFunc("PATCH /api/mobile/documents/upload/{taskId}/result", MobileDocumentUploadResultHandler(identity))
	mux.HandleFunc("POST /api/mobile/documents/export", MobileDocumentExportHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/export/{jobId}", MobileDocumentExportStatusHandler(identity))
	mux.HandleFunc("GET /api/mobile/documents/export/{jobId}/download", MobileDocumentExportDownloadHandler(identity))
	mux.HandleFunc("GET /api/mobile/server-profiles", MobileServerProfilesHandler(identity))
	mux.HandleFunc("POST /api/mobile/server-profiles", MobileServerProfilesHandler(identity))
	mux.HandleFunc("PUT /api/mobile/server-profiles", MobileServerProfilesHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/analyze", MobileSSHAnalyzeHandler(identity))
	mux.HandleFunc("GET /api/mobile/ssh/sessions", MobileBackendSSHSessionsHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions", MobileBackendSSHSessionsHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/claim", MobileBackendSSHSessionClaimHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/attach", MobileBackendSSHSessionAttachHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/input", MobileBackendSSHSessionInputHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/interrupt", MobileBackendSSHSessionInterruptHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/reconnect", MobileBackendSSHSessionReconnectHandler(identity))
	mux.HandleFunc("PATCH /api/mobile/ssh/sessions/{sessionId}/worker", MobileBackendSSHSessionUpdateHandler(identity))
	mux.HandleFunc("GET /api/mobile/ssh/sessions/{sessionId}/tasks", MobileBackendSSHSessionTasksHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/tasks", MobileBackendSSHSessionTasksHandler(identity))
	mux.HandleFunc("GET /api/mobile/ssh/sessions/{sessionId}/tasks/{taskId}", MobileBackendSSHSessionTaskStatusHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/tasks/{taskId}/wait", MobileBackendSSHSessionTaskWaitHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/tasks/{taskId}/kill", MobileBackendSSHSessionTaskKillHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/sessions/{sessionId}/files", MobileBackendSSHSessionFilesHandler(identity))
	mux.HandleFunc("GET /api/mobile/ssh/files/download/{token}", MobileHubSSHFileDownloadHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/tasks/claim", MobileBackendSSHTaskClaimHandler(identity))
	mux.HandleFunc("PATCH /api/mobile/ssh/tasks/{taskId}/worker", MobileBackendSSHTaskUpdateHandler(identity))
	mux.HandleFunc("POST /api/mobile/ssh/files/claim", MobileBackendSSHFileOperationClaimHandler(identity))
	mux.HandleFunc("PATCH /api/mobile/ssh/files/{operationId}/worker", MobileBackendSSHFileOperationUpdateHandler(identity))
	mux.HandleFunc("DELETE /api/mobile/ssh/sessions/{sessionId}", MobileBackendSSHSessionCloseHandler(identity))
	mux.HandleFunc("GET /api/mobile/digital-employees", MobileDigitalEmployeesHandler(identity, system, securitySvc, deviceSvc))
	// Bulk claim must register before the {employeeId} path so "tasks" is not captured as an id.
	mux.HandleFunc("POST /api/mobile/digital-employees/tasks/claim", MobileDigitalEmployeeTaskClaimAnyHandler(identity))
	mux.HandleFunc("POST /api/mobile/digital-employees/{employeeId}/tasks", MobileDigitalEmployeeTaskHandler(identity))
	mux.HandleFunc("POST /api/mobile/digital-employees/{employeeId}/tasks/claim", MobileDigitalEmployeeTaskClaimHandler(identity))
	mux.HandleFunc("GET /api/mobile/digital-employees/tasks/{taskId}", MobileDigitalEmployeeTaskStatusHandler(identity))
	mux.HandleFunc("PATCH /api/mobile/digital-employees/tasks/{taskId}", MobileDigitalEmployeeTaskUpdateHandler(identity))
	mux.HandleFunc("GET /api/my-ranking", GetMyRankingHandler(identity, sessionSvc, rankingCache))
	// Personal invitation links and the tenant-scoped invitation operations UI.
	// Referral links deliberately use a separate path from legacy admission
	// invitation codes: they never bypass allow_user_registration.
	if hubDB != nil && identity != nil && tenantRepo != nil {
		mux.HandleFunc("GET /api/me/invitations/status", GetMyUserInvitationStatusHandler(identity, system))
		mux.HandleFunc("GET /api/me/invitations", GetMyUserInvitationsHandler(identity, userReferralRepo, system))
		mux.HandleFunc("POST /api/me/invitations/rotate", RotateMyUserInvitationHandler(identity, userReferralRepo, system))
		mux.HandleFunc("GET /api/admin/user-referrals/config", requireTenantAdmin(GetUserReferralConfigHandler(system)))
		mux.HandleFunc("PUT /api/admin/user-referrals/config", requireTenantAdmin(UpdateUserReferralConfigHandler(system, adminAudit)))
		mux.HandleFunc("GET /api/admin/user-referrals", requireTenantAdmin(ListUserReferralInvitersHandler(userReferralRepo, system)))
		mux.HandleFunc("GET /api/admin/user-referrals/metrics", requireTenantAdmin(GetUserReferralMetricsHandler(userReferralRepo, system)))
		mux.HandleFunc("GET /api/admin/user-referrals/review-queue", requireTenantAdmin(ListReservedUserReferralsHandler(userReferralRepo)))
		mux.HandleFunc("GET /api/admin/user-referrals/{inviter_id}", requireTenantAdmin(ListUserReferralInviteesHandler(userReferralRepo, system)))
		mux.HandleFunc("POST /api/admin/user-referrals/{referral_id}/retry", requireTenantAdmin(RetryUserReferralRewardHandler(identity, userReferralRepo, system, adminAudit, failureLogs)))
		mux.HandleFunc("POST /api/admin/user-referrals/{referral_id}/{action}", requireTenantAdmin(ModerateUserReferralHandler(identity, userReferralRepo, system, adminAudit, failureLogs)))
		mux.HandleFunc("GET /invite/{code}", PublicUserReferralLandingHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("GET /invite/{code}/registration/status", PublicUserReferralRegistrationStatusHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /invite/{code}/registration/account-check", PublicUserReferralAccountCheckHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /invite/{code}/handoff", PublicUserReferralHandoffHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /invite/{code}/email/send-code", PublicUserReferralEmailSendCodeHandler(identity, userReferralRepo, system, tenantRepo, mailer))
		mux.HandleFunc("POST /invite/{code}/phone/send-code", PublicUserReferralPhoneSendCodeHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /invite/{code}/phone/register", PublicUserReferralPhoneRegisterHandler(identity, userReferralRepo, system, tenantRepo, failureLogs))
		mux.HandleFunc("POST /invite/{code}/register", PublicUserReferralRegisterHandler(identity, userReferralRepo, system, tenantRepo, failureLogs))
		mux.HandleFunc("POST /api/public/referral-handoffs/claim", PublicUserReferralHandoffClaimHandler(identity, userReferralRepo, system, tenantRepo))
		// Desktop registration follows the same handlers as the browser path,
		// but resolves attribution from the claimed opaque handoff session sent
		// in X-MaClaw-Referral-* headers instead of an invitation-code URL.
		mux.HandleFunc("POST /api/public/referral-registration/email/send-code", PublicUserReferralEmailSendCodeHandler(identity, userReferralRepo, system, tenantRepo, mailer))
		mux.HandleFunc("GET /api/public/referral-registration/status", PublicUserReferralDesktopRegistrationStatusHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /api/public/referral-registration/account-check", PublicUserReferralAccountCheckHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /api/public/referral-registration/email/enroll", PublicUserReferralEmailEnrollHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /api/public/referral-registration/phone/send-code", PublicUserReferralPhoneSendCodeHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /api/public/referral-registration/phone/register", PublicUserReferralPhoneRegisterHandler(identity, userReferralRepo, system, tenantRepo, failureLogs))
		mux.HandleFunc("POST /api/public/referral-registration/phone/enroll", PublicUserReferralPhoneEnrollHandler(identity, userReferralRepo, system, tenantRepo))
		mux.HandleFunc("POST /api/public/referral-registration/register", PublicUserReferralRegisterHandler(identity, userReferralRepo, system, tenantRepo, failureLogs))
	}
	mux.HandleFunc("POST /api/llm/service/redeem", RedeemLLMServiceCardHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/llm/v1/models", LLMV1ModelsHandler(identity, system, securitySvc))
	mux.HandleFunc("GET /api/llm/v1/models/{model...}", LLMV1ModelHandler(identity, system, securitySvc))
	mux.HandleFunc("POST /api/llm/v1/chat/completions", LLMV1ChatCompletionsHandler(identity, system, securitySvc, llmPromptCache))
	mux.HandleFunc("POST /api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, securitySvc, llmPromptCache))
	mux.HandleFunc("GET /api/card-store/products", GetCardStoreProductsHandler(system, tenantRepo))
	mux.HandleFunc("GET /api/card-store/payment-qr/{tenantID}/{filename}", CardStorePaymentQRImageHandler(cardStoreQRDir))
	mux.HandleFunc("GET /api/card-store/me", GetCardStoreMeHandler(identity))
	mux.HandleFunc("POST /api/card-store/orders", CreateCardStoreOrderHandler(identity, system, mailer, nil))
	mux.HandleFunc("GET /api/card-store/orders/{orderNo}/alipay/pay", CardStoreAlipayPayPageHandler(system))
	mux.HandleFunc("GET /api/card-store/orders/{orderNo}/alipay/return", CardStoreAlipayReturnHandler(system, mailer, identity))
	mux.HandleFunc("GET /api/card-store/orders/{orderNo}", GetCardStoreOrderHandler(system))
	mux.HandleFunc("POST /api/card-store/orders/{orderNo}/payment-opened", CardStorePaymentOpenedHandler(system, mailer))
	mux.HandleFunc("GET /card_store/admin/confirm", CardStorePersonalPaymentConfirmPageHandler(system, "approve"))
	mux.HandleFunc("GET /card_store/admin/delete", CardStorePersonalPaymentConfirmPageHandler(system, "reject"))
	mux.HandleFunc("POST /api/card-store/personal-payment/confirm", CardStorePersonalPaymentTokenActionHandler(system, mailer, identity, "approve"))
	mux.HandleFunc("POST /api/card-store/personal-payment/reject", CardStorePersonalPaymentTokenActionHandler(system, mailer, identity, "reject"))
	mux.HandleFunc("POST /api/card-store/recover", RecoverCardStoreCodesHandler(system, mailer))
	mux.HandleFunc("GET /api/zhifuxpay/notify", CardStorePaymentNotifyHandler(system, mailer, identity))
	mux.HandleFunc("POST /api/zhifuxpay/notify", CardStorePaymentNotifyHandler(system, mailer, identity))
	mux.HandleFunc("GET /api/card-store/payment/notify", CardStorePaymentNotifyHandler(system, mailer, identity))
	mux.HandleFunc("POST /api/card-store/payment/notify", CardStorePaymentNotifyHandler(system, mailer, identity))
	// Content audit configuration
	mux.HandleFunc("GET /api/admin/content_audit/config", requireTenantAdmin(GetContentAuditConfigHandler(system)))
	mux.HandleFunc("PUT /api/admin/content_audit/config", requireTenantAdmin(UpdateContentAuditConfigHandler(system)))
	// TLS configuration
	mux.HandleFunc("GET /api/admin/tls_config", requireGlobalAdmin(GetTLSConfigHandler(hubCfg)))
	mux.HandleFunc("POST /api/admin/tls_config", requireGlobalAdmin(UpdateTLSConfigHandler(hubCfg, configPath, ensureTLSCert, centerSvc)))
	// Smart route permission
	mux.HandleFunc("POST /api/admin/users/smart_route", requireAdmin(UpdateUserSmartRouteHandler(identity.UsersRepo())))
	mux.HandleFunc("GET /api/admin/smart_route_all", requireAdmin(GetSmartRouteAllHandler(system)))
	mux.HandleFunc("PUT /api/admin/smart_route_all", requireAdmin(UpdateSmartRouteAllHandler(system)))
	// HTTP threat class-head (not the LLM routing class-head).
	mountHTTPThreatAdmin(mux, requireTenantAdmin, runtimeDataDir, identity, adminAudit)
	// Security management
	if securitySvc != nil {
		mux.HandleFunc("GET /api/admin/security/groups", requireTenantAdmin(SecurityGroupsHandler(securitySvc)))
		mux.HandleFunc("GET /api/admin/security/groups/root", requireTenantAdmin(SecurityGroupsRootHandler(securitySvc)))
		mux.HandleFunc("POST /api/admin/security/groups", requireTenantAdmin(CreateSecurityGroupHandler(securitySvc, adminAudit)))
		mux.HandleFunc("PUT /api/admin/security/groups/{id}", requireTenantAdmin(UpdateSecurityGroupHandler(securitySvc, adminAudit)))
		mux.HandleFunc("DELETE /api/admin/security/groups/{id}", requireTenantAdmin(DeleteSecurityGroupHandler(securitySvc, adminAudit)))
		mux.HandleFunc("GET /api/admin/security/groups/{id}/members", requireTenantAdmin(ListGroupMembersHandler(securitySvc)))
		mux.HandleFunc("POST /api/admin/security/groups/{id}/members", requireTenantAdmin(AddGroupMemberHandler(securitySvc, adminAudit)))
		mux.HandleFunc("DELETE /api/admin/security/groups/{id}/members/{email}", requireTenantAdmin(RemoveGroupMemberHandler(securitySvc, adminAudit)))
		mux.HandleFunc("GET /api/admin/security/groups/{id}/policy", requireTenantAdmin(GetGroupPolicyHandler(securitySvc)))
		mux.HandleFunc("PUT /api/admin/security/groups/{id}/policy", requireTenantAdmin(UpdateGroupPolicyHandler(securitySvc, adminAudit)))
		mux.HandleFunc("GET /api/admin/security/users/{email}/effective-policy", requireTenantAdmin(GetUserEffectivePolicyHandler(securitySvc)))
		mux.HandleFunc("GET /api/admin/security/settings", requireTenantAdmin(GetSecuritySettingsHandler(securitySvc)))
		mux.HandleFunc("PUT /api/admin/security/settings", requireTenantAdmin(UpdateSecuritySettingsHandler(securitySvc)))
		mux.HandleFunc("PUT /api/admin/security/settings/default-group", requireTenantAdmin(SetDefaultGroupHandler(securitySvc, adminAudit)))
		mux.HandleFunc("GET /api/admin/security/approval-roles", requireTenantAdmin(GetApprovalRolesHandler(system)))
		mux.HandleFunc("PUT /api/admin/security/approval-roles", requireTenantAdmin(UpdateApprovalRolesHandler(system, adminAudit)))
		// Public endpoint for enrollment group tree
		mux.HandleFunc("GET /api/enroll/group-tree", EnrollGroupTreeHandler(securitySvc))
	}

	// Skill source control API (independent of security group policy).
	// Supports global / tenant / user level control.
	{
		skillSourceSvc := skillpkg.NewSourceControlService(system)
		adminWrap := func(h http.HandlerFunc) http.HandlerFunc {
			return requireAdmin(h)
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
		mux.HandleFunc("GET /api/admin/conversation_stats", requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			contexts, rounds := convStatsFn()
			writeJSON(w, http.StatusOK, map[string]any{
				"active_contexts": contexts,
				"total_rounds":    rounds,
			})
		}))
	}

	mux.HandleFunc("GET /api/admin/settings/qqbot", requireTenantAdmin(GetQQBotConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/qqbot", requireTenantAdmin(UpdateQQBotConfigHandler(system, qqbotPlugin, tenantIMRuntimeReloader)))
	mux.HandleFunc("GET /api/admin/qqbot/bindings", requireTenantAdmin(GetQQBotBindingsHandler(qqbotPlugin)))
	mux.HandleFunc("DELETE /api/admin/qqbot/bindings", requireTenantAdmin(DeleteQQBotBindingHandler(qqbotPlugin)))
	mux.HandleFunc("POST /api/qqbot/webhook", QQBotWebhookHandler(qqbotPlugin))
	mux.HandleFunc("GET /api/qqbot/tempfile/{token}", qqbotPlugin.ServeTempFile)

	// WeCom Bot
	mux.HandleFunc("GET /api/admin/settings/wecom", requireTenantAdmin(GetWeComConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/wecom", requireTenantAdmin(UpdateWeComConfigHandler(system, wecomPlugin, tenantIMRuntimeReloader)))
	mux.HandleFunc("GET /api/admin/wecom/bindings", requireTenantAdmin(GetWeComBindingsHandler(wecomPlugin)))
	mux.HandleFunc("DELETE /api/admin/wecom/bindings", requireTenantAdmin(DeleteWeComBindingHandler(wecomPlugin)))

	// DingTalk Bot
	mux.HandleFunc("GET /api/admin/settings/dingtalk", requireTenantAdmin(GetDingTalkConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/dingtalk", requireTenantAdmin(UpdateDingTalkConfigHandler(system, dingtalkPlugin, tenantIMRuntimeReloader)))
	mux.HandleFunc("GET /api/admin/dingtalk/bindings", requireTenantAdmin(GetDingTalkBindingsHandler(dingtalkPlugin)))
	mux.HandleFunc("DELETE /api/admin/dingtalk/bindings", requireTenantAdmin(DeleteDingTalkBindingHandler(dingtalkPlugin)))

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
	mux.HandleFunc("POST /api/bind/unbind", bindCORS(BindUnbindHandler(identity, deviceSvc, invitationSvc, feishuNotifier, imCleaners, userPurger)))

	mux.HandleFunc("POST /api/enroll/start", EnrollStartHandler(identity, invitationSvc, securitySvc))
	mux.HandleFunc("POST /api/enroll/email/send-code", RegistrationEmailSendCodeHandler(identity, mailer, system))
	mux.HandleFunc("POST /api/enroll/email/verify-and-start", RegistrationEmailVerifyAndStartHandler(identity, invitationSvc, securitySvc, system))
	mux.HandleFunc("POST /api/enroll/email/start-with-invitation", RegistrationEmailStartWithInvitationHandler(identity, invitationSvc, securitySvc, system))
	mux.HandleFunc("GET /api/enroll/registration-auth", PublicRegistrationAuthConfigHandler(system, identity))
	mux.HandleFunc("POST /api/enroll/sms/send-code", RegistrationSMSSendCodeHandler(identity, system, nil))
	mux.HandleFunc("POST /api/enroll/sms/verify-and-start", RegistrationSMSVerifyAndStartHandler(identity, system, nil))
	mux.HandleFunc("POST /api/mobile/auth/phone/send-code", MobileRegistrationSMSSendCodeHandler(identity, system, nil))
	mux.HandleFunc("POST /api/mobile/auth/phone/verify-and-start", MobileRegistrationSMSVerifyAndStartHandler(identity, system, nil))
	mux.HandleFunc("POST /api/enroll/profile/send-code", RegistrationContactSendCodeHandler(identity, mailer, system, nil))
	mux.HandleFunc("POST /api/enroll/profile/verify", RegistrationContactVerifyHandler(identity, system, nil))
	mux.HandleFunc("GET /api/enroll/profile/current", RegistrationCurrentProfileHandler(identity))
	mux.HandleFunc("POST /api/center/user-exists", CenterUserExistsHandler(identity, centerSvc))
	mux.HandleFunc("POST /api/auth/email-request", EmailRequestLoginHandler(identity))
	mux.HandleFunc("POST /api/auth/email-confirm", EmailConfirmLoginHandler(identity))
	mux.HandleFunc("GET /api/auth/verify-email", VerifyEmailHandler(identity))
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
	mux.HandleFunc("GET /api/debug/machines", requireAdmin(DebugListMachinesHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/debug/machine-events", requireAdmin(DebugListMachineEventsHandler(deviceSvc)))
	mux.HandleFunc("GET /api/debug/sessions", requireAdmin(DebugListSessionsHandler(sessionSvc, userLookup)))
	mux.HandleFunc("GET /api/debug/session", requireAdmin(DebugGetSessionHandler(sessionSvc, userLookup)))
	mux.HandleFunc("POST /api/admin/routing/sync-verified-phone-routes", requireAdmin(AdminSyncVerifiedPhoneRoutesHandler(identity)))
	mux.HandleFunc("/ws", gateway.HandleWS)
	if gateway != nil && gateway.DeviceGateway != nil {
		mux.Handle("/api/device-gateway/v1/", gateway.DeviceGateway)
		mux.Handle("/api/im-gateway/v1/", gateway.DeviceGateway)
	}
	mux.HandleFunc("GET /api/shortcuts", GetShortcutsHandler(identity, system))
	mux.HandleFunc("GET /marketplace", MarketplacePageHandler("hub"))
	mux.HandleFunc("GET /api/capabilities", CapabilityListHandler(capabilitySvc, identity))
	mux.HandleFunc("GET /api/admin/capabilities", requireTenantAdmin(AdminCapabilityListHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capabilities", requireTenantAdmin(AdminCapabilityUpsertHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capabilities/backfill-display-names", requireTenantAdmin(AdminCapabilityBackfillDisplayNamesHandler(capabilitySvc, runtimeDataDir)))
	mux.HandleFunc("DELETE /api/admin/capabilities/{id}", requireTenantAdmin(AdminCapabilityDeleteHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capabilities/{id}/upload-to-market", requireTenantAdmin(AdminCapabilityUploadToMarketHandler(capabilitySvc, centerSvc, runtimeDataDir)))
	mux.HandleFunc("GET /api/admin/capability-market/submissions", requireTenantAdmin(AdminCapabilityMarketSubmissionsHandler(capabilitySvc, centerSvc)))
	mux.HandleFunc("POST /api/admin/capabilities/maclaw-apps/{id}/approve", requireTenantAdmin(AdminCapabilityMaclawAppReviewHandler(capabilitySvc, "approve")))
	mux.HandleFunc("POST /api/admin/capabilities/maclaw-apps/{id}/publish", requireTenantAdmin(AdminCapabilityMaclawAppPublishHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capabilities/maclaw-apps/{id}/reject", requireTenantAdmin(AdminCapabilityMaclawAppReviewHandler(capabilitySvc, "reject")))
	mux.HandleFunc("GET /api/capabilities/{id}", CapabilityDetailHandler(capabilitySvc, identity))
	mux.HandleFunc("POST /api/capabilities/skills/submit", CapabilitySkillSubmitHandler(capabilitySvc, identity, runtimeDataDir))
	mux.HandleFunc("POST /api/capabilities/maclaw-apps/submit", CapabilityMaclawAppSubmitHandler(capabilitySvc, identity))
	mux.HandleFunc("GET /api/capabilities/maclaw-apps/{id}/package", CapabilityMaclawAppPackageHandler(capabilitySvc, identity))
	mux.HandleFunc("GET /api/v1/skills/{id}/download", CapabilitySkillDownloadHandler(capabilitySvc, identity, runtimeDataDir))
	mux.HandleFunc("GET /api/capabilities/{id}/versions", CapabilityVersionsHandler(capabilitySvc, identity))
	mux.HandleFunc("GET /api/capabilities/{id}/mcp-secret-requirements", MCPSecretRequirementsHandler(capabilitySvc, identity))
	mux.HandleFunc("POST /api/capabilities/{id}/install-intent", CapabilityInstallIntentHandler(capabilitySvc, system, identity, centerSvc))
	mux.HandleFunc("GET /api/capabilities/managed-deployments", CapabilityManagedDeploymentsHandler(capabilitySvc, identity, securitySvc))
	mux.HandleFunc("GET /api/capabilities/recommended", CapabilityRecommendationsHandler(capabilitySvc, identity, securitySvc))
	mux.HandleFunc("GET /api/capabilities/inventory", UserCapabilityInventoryHandler(identity, capabilitySvc))
	mux.HandleFunc("PUT /api/capabilities/inventory", UserCapabilityInventoryUpsertHandler(identity, capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/mcp-secret-bindings", MCPSecretBindingsHandler(identity, capabilitySvc))
	mux.HandleFunc("PUT /api/capabilities/mcp-secret-bindings", MCPSecretBindingUpsertHandler(identity, capabilitySvc))
	mux.HandleFunc("GET /api/capabilities/mcp-hub-secrets", MCPHubSecretsHandler(identity, capabilitySvc))
	mux.HandleFunc("PUT /api/capabilities/mcp-hub-secrets", MCPHubSecretUpsertHandler(identity, capabilitySvc))
	mux.HandleFunc("GET /api/admin/billing/customer-account", requireTenantAdmin(AdminBillingCustomerAccountHandler(system, centerSvc)))
	mux.HandleFunc("GET /api/admin/billing/licenses", requireTenantAdmin(AdminBillingLicensesHandler(system, centerSvc)))
	mux.HandleFunc("GET /api/admin/user-rankings", requireTenantAdmin(GetUserRankingsHandler(sessionSvc, platformUsers, rankingCache)))
	mux.HandleFunc("GET /api/admin/capability-market/policy", requireTenantAdmin(AdminCapabilityMarketPolicyGetHandler(system)))
	mux.HandleFunc("PUT /api/admin/capability-market/policy", requireTenantAdmin(AdminCapabilityMarketPolicyUpdateHandler(system)))
	mux.HandleFunc("GET /api/admin/capability-market/acquisition-requests", requireTenantAdmin(AdminCapabilityAcquisitionRequestsHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/acquisition-requests/{id}", requireTenantAdmin(AdminCapabilityAcquisitionRequestDetailHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/acquisition-requests/{id}/approve", requireTenantAdmin(AdminCapabilityApproveAcquisitionHandler(capabilitySvc, system, centerSvc)))
	mux.HandleFunc("POST /api/admin/capability-market/acquisition-requests/{id}/reject", requireTenantAdmin(AdminCapabilityRejectAcquisitionHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/acquisition-requests/{id}/complete", requireTenantAdmin(AdminCapabilityCompleteAcquisitionHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/managed-deployments", requireTenantAdmin(AdminCapabilityManagedDeploymentListHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/managed-deployments", requireTenantAdmin(AdminCapabilityManagedDeploymentCreateHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("DELETE /api/admin/capability-market/managed-deployments/{id}", requireTenantAdmin(AdminCapabilityManagedDeploymentDeleteHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("GET /api/admin/capability-market/recommendations", requireTenantAdmin(AdminCapabilityRecommendationListHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/recommendations", requireTenantAdmin(AdminCapabilityRecommendationCreateHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("DELETE /api/admin/capability-market/recommendations/{id}", requireTenantAdmin(AdminCapabilityRecommendationDeleteHandler(capabilitySvc, adminAudit)))
	mux.HandleFunc("GET /api/admin/audit-logs", requireAdmin(AdminAuditLogsHandler(adminAudit)))
	mux.HandleFunc("GET /api/admin/capability-market/groups/{id}/effective-policies", requireTenantAdmin(AdminGroupCapabilityEffectivePoliciesHandler(capabilitySvc, securitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/users/{email}/inventory", requireTenantAdmin(AdminUserCapabilityInventoryHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/users/{email}/effective-policies", requireTenantAdmin(AdminUserCapabilityEffectivePoliciesHandler(capabilitySvc, securitySvc)))
	mux.HandleFunc("GET /api/admin/capability-market/users/{email}/compliance", requireTenantAdmin(AdminUserCapabilityComplianceHandler(capabilitySvc, securitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/mcp", requireTenantAdmin(AdminMCPMarketplaceUpsertHandler(capabilitySvc)))
	mux.HandleFunc("PUT /api/admin/capability-market/mcp", requireTenantAdmin(AdminMCPMarketplaceUpsertHandler(capabilitySvc)))
	mux.HandleFunc("POST /api/admin/capability-market/mcp/test", requireTenantAdmin(AdminMCPTestConnectionHandler()))
	mux.HandleFunc("POST /api/admin/capability-market/mcp-secret-requirements", requireTenantAdmin(AdminMCPSecretRequirementUpsertHandler(capabilitySvc)))
	mux.HandleFunc("GET /api/admin/capabilities/external-search", requireTenantAdmin(AdminCapabilityExternalSearchHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/capabilities/mcp/validate", requireTenantAdmin(AdminMCPValidateHandler()))
	mux.HandleFunc("POST /api/admin/capabilities/import-intent", requireTenantAdmin(AdminCapabilityImportIntentHandler(capabilitySvc, system, centerSvc)))

	// Workflow admin review API
	{
		workflowReviewSvc := workflow.NewAdminReviewService(storesqlite.NewWorkflowStore(hubDB), capabilitySvc)
		mux.HandleFunc("GET /api/v1/admin/reviews", requireTenantAdmin(WorkflowAdminReviewListHandler(workflowReviewSvc)))
		mux.HandleFunc("GET /api/v1/admin/reviews/{id}", requireTenantAdmin(WorkflowAdminReviewDetailHandler(workflowReviewSvc)))
		mux.HandleFunc("POST /api/v1/admin/reviews/{id}/approve", requireTenantAdmin(WorkflowAdminReviewApproveHandler(workflowReviewSvc)))
		mux.HandleFunc("POST /api/v1/admin/reviews/{id}/reject", requireTenantAdmin(WorkflowAdminReviewRejectHandler(workflowReviewSvc)))
		mux.HandleFunc("POST /api/v1/admin/reviews/{id}/unpublish", requireTenantAdmin(WorkflowAdminReviewUnpublishHandler(workflowReviewSvc)))
	}

	// Workflow user-facing auth middleware: authenticates VE machine and sets X-Owner-ID.
	workflowUserAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			principal, ok := authenticateVEMachine(w, r, identity)
			if !ok {
				return
			}
			r.Header.Set("X-Owner-ID", principal.MachineID)
			// Establish the authenticated identity in BOTH conventions:
			// header (read by InstanceAPI / DecisionAPI / WorkflowAPI) and
			// context (read by RuntimeAPI's handleInitiateWorkflow / handleConfirm
			// / directory views via getUserIDFromContext). Populating the context
			// is purely additive and lets the RuntimeAPI routes registered in the
			// runtime wiring below see the caller instead of returning 401.
			ctx := workflow.WithUserID(r.Context(), principal.MachineID)
			ctx = store.WithTenant(ctx, principal.TenantID)
			ctx = WithRequestTenant(ctx, principal.TenantID)
			r = r.WithContext(ctx)
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/workflow-directory/approvers", workflowUserAuth(WorkflowApproverDirectoryHandler(securitySvc, identity, deviceSvc, system)))
	mux.HandleFunc("POST /api/v1/workflow-drafts/generate", WorkflowDraftLLMHandler(identity, system, securitySvc))

	// Workflow CRUD API (user-facing)
	{
		wfStore := storesqlite.NewWorkflowStore(hubDB)
		vm := workflow.NewVersionManager(wfStore)
		wfAPI := workflow.NewWorkflowAPI(wfStore, vm)
		wfAPI.RegisterRoutes(mux, workflowUserAuth)
	}

	// Workflow Instance API
	{
		// Hub bootstraps a SQLite database.  These stores must therefore use
		// the SQLite schema (not the legacy PostgreSQL table names such as
		// workflow_node_executions), otherwise the timeout ticker logs an error
		// every five minutes and approvals cannot be inspected.
		wfStore := storesqlite.NewWorkflowStore(hubDB)
		instStore := storesqlite.NewInstanceStore(hubDB)
		auditStore := storesqlite.NewAuditStore(hubDB)
		confirmStore := workflow.NewPgConfirmationStore(hubDB)

		// ConfirmationTracker: the post-completion confirmation subsystem
		// (terminal-node StartTracking, reminder loop, orphan reconciliation).
		// It is wired with the SAME confirmStore instance the RuntimeAPI
		// directory/confirm routes use below, so the tracker and those routes
		// share one store (Fix wiring item 5 / ordering note 3).
		//
		// The NotificationDispatcher is now wired with a real HubInAppNotifier
		// (HubNotifier, below) in place of the former first nil, so reminder and
		// completion *delivery* actually reaches recipient machines.
		// StartTracking and ReconcileOrphanedInstances, which touch only
		// confirmStore + auditStore, continue to work fully. SetWorkflowStore
		// injects the WorkflowStore so ReconcileOrphanedInstances can re-derive a
		// completed instance's terminal-node TerminalNodeConfig from its
		// published version graph. The NotificationDispatcher and
		// ConfirmationTracker public APIs are unchanged.
		// Real HubInAppNotifier backed by the Hub machine sender
		// (device.Service.SendToMachine), with presence sourced from
		// device.Service.IsMachineOnline, the same sources HubApprovalDispatcher
		// and HubAvailabilityChecker use. Replaces the first nil notifier so
		// terminal-node completion notifications and confirmation reminders are
		// actually delivered to recipient machines. imPusher (arg 2) and
		// notifStore (arg 4) stay nil (out of scope); auditStore (arg 3) is
		// unchanged so delivery-failure audit recording remains active. The
		// HubInAppNotifier interface and the NewNotificationDispatcher signature
		// are unchanged.
		hubNotifier := NewHubNotifier(deviceSvc).WithPresence(deviceSvc)
		notifDispatcher := workflow.NewNotificationDispatcher(hubNotifier, nil, auditStore, nil)
		confirmTracker := workflow.NewConfirmationTracker(confirmStore, instStore, notifDispatcher, auditStore).
			SetWorkflowStore(wfStore)

		// Real ApprovalDispatcher backed by the Hub machine sender
		// (device.Service.SendToMachine), the same mechanism VE/group messaging
		// uses. Replaces the noop dispatcher so approval requests are actually
		// delivered to approver machines (Finding 1.2 / 2.2). The
		// ApprovalDispatcher interface and the executor call sites are unchanged.
		dispatcher := NewHubApprovalDispatcher(deviceSvc)
		// Pass the tracker into the executor so a terminal node creates
		// confirmation records at runtime (executeTerminalNode -> StartTracking).
		// Without WithConfirmationTracker the executor's tracker is nil and
		// StartTracking is skipped, so no confirmation records are ever created
		// in production (Finding 1.5 / design Fix Implementation item 5).
		// Participant notifier pushes blocked/attention events to initiator machines
		// (ve:workflow_status) so desktop projections update without waiting for reconcile.
		participantNotifier := NewHubWorkflowParticipantNotifier(deviceSvc, instStore, deviceSvc, identity)
		// EscalationManager retries fallback human delivery; max-fail notifies + blocks.
		// Created before executor so WithEscalationManager can register the failed hook.
		escalationMgr := workflow.NewEscalationManager(dispatcher, auditStore, NewHubAvailabilityChecker(deviceSvc)).
			SetNotifier(participantNotifier)
		executor := workflow.NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher,
			workflow.WithConfirmationTracker(confirmTracker),
			workflow.WithApprovalApproverResolver(newWorkflowApprovalRoleResolver(system, identity, deviceSvc, securitySvc)),
			workflow.WithNotifier(participantNotifier),
			workflow.WithEscalationManager(escalationMgr),
		)
		instanceAPI := workflow.NewInstanceAPI(executor, instStore, auditStore)
		instanceAPI.RegisterRoutes(mux, workflowUserAuth)

		// Decision entry point: routes an approver's decision into
		// WorkflowExecutor.ResumeInstance (registers POST
		// /api/v1/instances/{id}/nodes/{nodeID}/decision). Without this an
		// instance reaching an approval node has no caller for ResumeInstance
		// and blocks forever.
		decisionAPI := workflow.NewDecisionAPI(executor, instStore, wfStore)
		decisionAPI.RegisterRoutes(mux, workflowUserAuth)

		// Runtime API: validated initiation (/initiate), withdrawal, confirmation,
		// and directory routes (Finding 1.3 / 2.3). The 5-arg RuntimeExecutor
		// signature is bridged to the executor's 2-arg StartInstance via
		// hubRuntimeExecutorAdapter, which marshals {form_data, initiator_id,
		// channel, submission_timestamp} into trigger data. handleInitiateWorkflow
		// already validates form_data against the published version's schema with
		// FormValidator, so validator semantics are unchanged. The existing
		// /trigger route + owner isolation registered by instanceAPI above remain
		// unchanged (Preservation 3.5).
		runtimeExec := newHubRuntimeExecutorAdapter(executor)
		runtimeAPI := workflow.NewRuntimeAPI(runtimeExec, instStore, auditStore, &workflow.FormValidator{}, wfStore)
		runtimeAPI.SetWithdrawalHandler(
			workflow.NewWithdrawalHandler(instStore, auditStore, nil, nil).
				SetEscalationManager(escalationMgr),
		)
		runtimeAPI.SetDirectoryService(workflow.NewDirectoryService(instStore, confirmStore, newHubNodeExecStoreAdapter(instStore)))
		runtimeAPI.RegisterRoutes(mux, workflowUserAuth)

		// Start background services for approval workflow.
		// Real HumanApproverChecker backed by device.Service.IsMachineOnline so
		// availability mirrors real approver presence and unavailable/queue-full/
		// timeout conditions can route to fallback/escalation (Finding 1.4 / 2.4).
		// EscalationManager and HandleUnavailable/HandleTimeout/HandleQueueFull are
		// unchanged; only the availability source changes (Preservation 3.6).
		// Rebuild in-memory escalation queue from durable instance_data markers
		// left by a previous Hub process (restart recovery). Then start the ticker.
		if n := executor.ReconcileEscalations(context.Background()); n > 0 {
			log.Printf("[hub-router] escalation reconcile restored %d peer(s)", n)
		}
		// Background retry loop for EscalationManager (wired on executor above).
		escalationMgr.Start()

		timeoutTicker := workflow.NewTimeoutTicker(executor, instStore)
		timeoutTicker.Start()

		// Confirmation reconciliation + reminder loops. These complete the
		// runtime-half wiring: RunReconcileLoop repairs orphaned completed
		// instances (those marked completed before StartTracking ran in the
		// crash window in executeTerminalNode), and RunReminderLoop drives
		// pending-confirmation reminders/escalation. Both take a context and
		// stop when it is cancelled.
		//
		// NewRouter threads no context.Context and no shutdown hook, and the
		// established pattern for the other workflow background loops in this
		// block (EscalationManager.Start, TimeoutTicker.Start) is a
		// process-lifetime goroutine driven by context.Background() with no
		// caller-side stop. We match that established pattern: each loop owns a
		// context.Background() and runs for the process lifetime. (ConfirmationTracker
		// also exposes Stop() and the loops honor ctx cancellation, so when a
		// shutdown context is threaded through NewRouter in the future these can
		// adopt it without touching the loop implementations.)
		go confirmTracker.RunReconcileLoop(context.Background())
		go confirmTracker.RunReminderLoop(context.Background())
	}

	// Workflow audit trail query API
	{
		auditStore := storesqlite.NewAuditStore(hubDB)
		auditAPI := workflow.NewAuditAPI(auditStore)
		auditAPI.RegisterRoutes(mux, workflowUserAuth)
	}

	// Review notification background service
	{
		wfStore := storesqlite.NewWorkflowStore(hubDB)
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

	// Public user ranking leaderboard (no auth, masked emails)
	// Uses pre-computed ranking cache for instant response.
	mux.HandleFunc("GET /api/public/user-rankings", GetPublicUserRankingsHandler(sessionSvc, platformUsers, rankingCache))

	// ---------------------------------------------------------------------------
	// Dynamic Notification System endpoints
	// ---------------------------------------------------------------------------
	if hubDB != nil {
		notifStore := notification.NewStore(hubDB)
		_ = notifStore.InitSchema(context.Background())
		var notifPusher notification.WSBroadcaster
		if deviceSvc != nil {
			notifPusher = notification.NewPusher(deviceSvc, deviceSvc)
		}
		notifSvc := notification.NewService(notifStore, notifPusher, nil)
		notifHandler := NewNotificationHandler(notifSvc)

		// Admin routes (requireAdmin)
		mux.HandleFunc("POST /api/v1/admin/notifications", requireAdmin(notifHandler.HandleCreateNotification))
		mux.HandleFunc("GET /api/v1/admin/notifications", requireAdmin(notifHandler.HandleListNotifications))
		mux.HandleFunc("GET /api/v1/admin/notifications/{id}", requireAdmin(notifHandler.HandleGetNotification))
		mux.HandleFunc("POST /api/v1/admin/notifications/{id}/revoke", requireAdmin(notifHandler.HandleRevokeNotification))
		mux.HandleFunc("DELETE /api/v1/admin/notifications/{id}", requireAdmin(notifHandler.HandleDeleteNotification))
		mux.HandleFunc("POST /api/admin/notifications", requireAdmin(notifHandler.HandleCreateNotification))
		mux.HandleFunc("GET /api/admin/notifications", requireAdmin(notifHandler.HandleListNotifications))
		mux.HandleFunc("GET /api/admin/notifications/{id}", requireAdmin(notifHandler.HandleGetNotification))
		mux.HandleFunc("POST /api/admin/notifications/{id}/revoke", requireAdmin(notifHandler.HandleRevokeNotification))
		mux.HandleFunc("DELETE /api/admin/notifications/{id}", requireAdmin(notifHandler.HandleDeleteNotification))

		// Client routes (machine auth)
		machineAuth := requireMachineAuth(identity)
		mux.HandleFunc("GET /api/v1/notifications/unread", machineAuth(notifHandler.HandleUnread))
		mux.HandleFunc("POST /api/v1/notifications/{id}/read", machineAuth(notifHandler.HandleMarkRead))
		mux.HandleFunc("POST /api/v1/notifications/read-all", machineAuth(notifHandler.HandleMarkAllRead))

		// Cascade route: HubCenter uses the registered installation ID as a pre-shared token.
		mux.HandleFunc("POST /api/v1/notifications/cascade", requireCascadeAuth(notifHandler.HandleCascade))
	}

	// Surveys (Hub-first authority): machine-authenticated tenant APIs.
	if hubDB != nil {
		surveyStore := survey.NewStore(hubDB)
		if err := surveyStore.InitSchema(context.Background()); err != nil {
			log.Printf("[hub] survey schema init failed: %v", err)
		} else {
			surveyHandler := NewSurveyHandler(surveyStore)
			surveyHandler.Register(mux, identity)
		}
	}

	// Experts (Hub-first authority): machine-authenticated tenant APIs.
	if hubDB != nil {
		expertStore := expert.NewStore(hubDB)
		if err := expertStore.InitSchema(context.Background()); err != nil {
			log.Printf("[hub] expert schema init failed: %v", err)
		} else {
			expertHandler := NewExpertHandler(expertStore)
			expertHandler.Register(mux, identity)
		}
		managedIndustryExpertStore := industryexpert.NewStore(hubDB)
		if err := managedIndustryExpertStore.InitSchema(context.Background()); err != nil {
			log.Printf("[hub] managed industry expert schema init failed: %v", err)
		} else {
			NewManagedIndustryExpertHandler(managedIndustryExpertStore).Register(mux, identity)
		}
	}

	registerPWAStaticRoutes(mux, staticDir, routePrefix)
	registerStaticRoutes(mux, "./web/knowledge_shares", "/hub/knowledge/shares/mine")
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	registerBindStaticRoutes(mux, "./web/bind", "/bind")
	registerGetCreditsStaticRoutes(mux, "./web/get-credits", "/get-credits")
	registerCardStoreStaticRoutes(mux, "./web/card_store", "/card_store")
	registerStaticRoutes(mux, "./web/connector", "/connector")
	registerStaticRoutes(mux, "./web/maclaw-app-manual", "/maclaw-app-manual")
	registerStaticRoutes(mux, "./web/pet-pack-help", "/pet-pack-help")
	registerStaticRoutes(mux, "./web/approval_workflow", "/approval_workflow")
	registerStaticRoutes(mux, "./web/user-ranking", "/user-ranking")
	return mux
}
