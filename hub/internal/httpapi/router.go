package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/entry"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/skill"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func NewRouter(
	admins *auth.AdminService,
	identity *auth.IdentityService,
	centerSvc *center.Service,
	mailer *mail.Service,
	gateway *ws.Gateway,
	deviceSvc *device.Service,
	sessionSvc *session.Service,
	invitationSvc *invitation.Service,
	system store.SystemSettingsRepository,
	feishuNotifier *feishu.Notifier,
	feishuPlugin *feishu.FeishuPlugin,
	openclawIMPlugin *im.WebhookIMPlugin,
	qqbotPlugin *qqbot.Plugin,
	skillStore *skill.SkillStore,
	staticDir string,
	routePrefix string,
	bridgeDir string,
) http.Handler {
	var invChecker entry.InvitationCodeChecker
	if invitationSvc != nil {
		invChecker = invitationSvc
	}
	entrySvc := entry.NewService(identity, invChecker)
	var userLookup machineUserLookup
	if identity != nil {
		userLookup = identity.UsersRepo()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler("maclaw-hub"))
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(admins))
	mux.HandleFunc("POST /api/admin/setup", SetupAdminHandler(admins))
	mux.HandleFunc("POST /api/admin/login", AdminLoginHandler(admins))
	mux.HandleFunc("POST /api/admin/password", RequireAdmin(admins, AdminChangePasswordHandler(admins)))
	mux.HandleFunc("POST /api/admin/profile", RequireAdmin(admins, AdminUpdateProfileHandler(admins)))
	mux.HandleFunc("GET /api/admin/debug/machines", RequireAdmin(admins, DebugListMachinesHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/machine-events", RequireAdmin(admins, DebugListMachineEventsHandler(deviceSvc)))
	mux.HandleFunc("DELETE /api/admin/machines", RequireAdmin(admins, DeleteMachineHandler(deviceSvc)))
	mux.HandleFunc("POST /api/admin/machines/rename", RequireAdmin(admins, RenameMachineHandler(deviceSvc)))
	mux.HandleFunc("POST /api/admin/machines/clear-offline", RequireAdmin(admins, ClearOfflineMachinesHandler(deviceSvc)))
	mux.HandleFunc("DELETE /api/admin/machines/by-email", RequireAdmin(admins, DeleteMachinesByEmailHandler(deviceSvc, userLookup)))
	mux.HandleFunc("DELETE /api/admin/machines/force-by-email", RequireAdmin(admins, ForceDeleteMachinesByEmailHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/sessions", RequireAdmin(admins, DebugListSessionsHandler(sessionSvc)))
	mux.HandleFunc("GET /api/admin/debug/session", RequireAdmin(admins, DebugGetSessionHandler(sessionSvc)))
	mux.HandleFunc("POST /api/admin/users/manual-bind", RequireAdmin(admins, ManualBindHandler(identity)))
	mux.HandleFunc("GET /api/admin/users", RequireAdmin(admins, ListUsersHandler(identity)))
	mux.HandleFunc("GET /api/admin/users/lookup", RequireAdmin(admins, LookupUserHandler(identity)))
	mux.HandleFunc("GET /api/admin/blocklist", RequireAdmin(admins, ListBlockedEmailsHandler(identity)))
	mux.HandleFunc("POST /api/admin/blocklist", RequireAdmin(admins, AddBlockedEmailHandler(identity)))
	mux.HandleFunc("DELETE /api/admin/blocklist/{email}", RequireAdmin(admins, RemoveBlockedEmailHandler(identity)))
	mux.HandleFunc("POST /api/admin/invitation-codes/generate", RequireAdmin(admins, GenerateInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes", RequireAdmin(admins, ListInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("POST /api/admin/invitation-codes/toggle", RequireAdmin(admins, ToggleInvitationCodeHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/status", RequireAdmin(admins, InvitationCodeStatusHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/export", RequireAdmin(admins, ExportInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/enrollments/pending", RequireAdmin(admins, ListPendingEnrollmentsHandler(identity)))
	mux.HandleFunc("GET /api/admin/enrollments/all", RequireAdmin(admins, ListAllEnrollmentsHandler(identity)))
	mux.HandleFunc("POST /api/admin/enrollments/approve", RequireAdmin(admins, ApproveEnrollmentHandler(identity, feishuNotifier)))
	mux.HandleFunc("POST /api/admin/enrollments/reject", RequireAdmin(admins, RejectEnrollmentHandler(identity)))
	mux.HandleFunc("GET /api/admin/pending-logins", RequireAdmin(admins, ListPendingLoginsHandler(identity)))
	mux.HandleFunc("POST /api/admin/pending-logins/confirm", RequireAdmin(admins, AdminConfirmLoginHandler(identity)))
	mux.HandleFunc("GET /api/admin/center/status", RequireAdmin(admins, GetCenterStatusHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/center/config", RequireAdmin(admins, UpdateCenterConfigHandler(centerSvc, identity, func(url string) {
		if qqbotPlugin != nil {
			qqbotPlugin.SetPublicBaseURL(url)
		}
		if feishuPlugin != nil {
			feishuPlugin.SetPublicBaseURL(url)
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
	mux.HandleFunc("GET /api/admin/settings/qqbot", RequireAdmin(admins, GetQQBotConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/settings/qqbot", RequireAdmin(admins, UpdateQQBotConfigHandler(system, qqbotPlugin)))
	mux.HandleFunc("GET /api/admin/qqbot/bindings", RequireAdmin(admins, GetQQBotBindingsHandler(qqbotPlugin)))
	mux.HandleFunc("DELETE /api/admin/qqbot/bindings", RequireAdmin(admins, DeleteQQBotBindingHandler(qqbotPlugin)))
	mux.HandleFunc("POST /api/qqbot/webhook", QQBotWebhookHandler(qqbotPlugin))
	mux.HandleFunc("GET /api/qqbot/tempfile/{token}", qqbotPlugin.ServeTempFile)
	mux.HandleFunc("/api/feishu/webhook", feishu.WebhookHandler(feishuNotifier))
	if feishuPlugin != nil {
		mux.HandleFunc("GET /api/feishu/tempfile/{token}", feishuPlugin.ServeTempFile)
	}
	mux.HandleFunc("POST /api/enroll/start", EnrollStartHandler(identity, feishuNotifier))
	mux.HandleFunc("POST /api/auth/email-request", EmailRequestLoginHandler(identity))
	mux.HandleFunc("POST /api/auth/email-confirm", EmailConfirmLoginHandler(identity))
	mux.HandleFunc("POST /api/auth/email-poll", EmailPollLoginHandler(identity))
	mux.HandleFunc("POST /api/entry/probe", EntryProbeHandler(entrySvc))
	mux.HandleFunc("GET /api/machines", ListMachinesHandler(identity, deviceSvc))
	mux.HandleFunc("GET /api/sessions", ListSessionsHandler(identity, sessionSvc))
	mux.HandleFunc("GET /api/session", GetSessionHandler(identity, sessionSvc))
	mux.HandleFunc("POST /api/session/start", SessionStartHandler(identity, deviceSvc))
	mux.HandleFunc("POST /api/session/input", SessionInputHandler(identity, sessionSvc, deviceSvc))
	mux.HandleFunc("POST /api/session/interrupt", SessionInterruptHandler(identity, sessionSvc, deviceSvc))
	mux.HandleFunc("POST /api/session/kill", SessionKillHandler(identity, sessionSvc, deviceSvc))
	mux.HandleFunc("GET /api/debug/machines", DebugListMachinesHandler(deviceSvc, userLookup))
	mux.HandleFunc("GET /api/debug/machine-events", DebugListMachineEventsHandler(deviceSvc))
	mux.HandleFunc("GET /api/debug/sessions", DebugListSessionsHandler(sessionSvc))
	mux.HandleFunc("GET /api/debug/session", DebugGetSessionHandler(sessionSvc))
	mux.HandleFunc("/ws", gateway.HandleWS)
	mux.HandleFunc("GET /api/shortcuts", GetShortcutsHandler(identity, system))
	mux.HandleFunc("PUT /api/shortcuts", PutShortcutsHandler(identity, system))
	// Skill Catalog API
	skillHandlers := NewSkillHandlers(skillStore)
	mux.HandleFunc("GET /api/v1/skills/search", skillHandlers.SearchSkills)
	mux.HandleFunc("GET /api/v1/skills/{id}", skillHandlers.GetSkill)
	mux.HandleFunc("GET /api/v1/skills/{id}/download", skillHandlers.DownloadSkill)
	mux.HandleFunc("GET /api/v1/skills/popular", skillHandlers.PopularSkills)
	mux.HandleFunc("POST /api/v1/skills", skillHandlers.PublishSkill)

	// Webhook session endpoint (Bearer token auth handled internally)
	mux.HandleFunc("POST /api/webhook/session", WebhookCreateSessionHandler(deviceSvc, sessionSvc))

	registerPWAStaticRoutes(mux, staticDir, routePrefix)
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	return mux
}
