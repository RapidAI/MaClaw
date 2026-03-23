package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/entry"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/voiceprint"
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
	hubLLMStatusFn func() string,
	convStatsFn func() (int, int),
	chatStore *chat.Store,
	chatChannelSvc *chat.ChannelService,
	chatMessageSvc *chat.MessageService,
	chatFileSvc *chat.FileService,
	chatReadReceiptSvc *chat.ReadReceiptService,
	chatPresenceSvc *chat.PresenceService,
	chatVoiceSignaling *chat.VoiceSignaling,
	chatNotifier *chat.Notifier,
	voiceprintSvc *voiceprint.Service,
	hubCfg *config.Config,
	configPath string,
	ensureTLSCert func(certFile, keyFile string) error,
	staticDir string,
	routePrefix string,
	bridgeDir string,
) http.Handler {
	var invChecker entry.InvitationCodeChecker
	if invitationSvc != nil {
		invChecker = invitationSvc
	}
	var feishuChecker entry.FeishuAutoEnrollChecker
	if feishuNotifier != nil {
		if ae := feishuNotifier.AutoEnroller(); ae != nil {
			feishuChecker = ae
		}
	}
	entrySvc := entry.NewService(identity, invChecker, feishuChecker)
	var userLookup machineUserLookup
	if identity != nil {
		userLookup = identity.UsersRepo()
	}
	var imCleaners []IMBindingCleaner
	if qqbotPlugin != nil {
		imCleaners = append(imCleaners, qqbotPlugin)
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
	mux.HandleFunc("GET /api/admin/sessions/all", RequireAdmin(admins, AdminListAllSessionsHandler(sessionSvc)))
	mux.HandleFunc("POST /api/admin/users/manual-bind", RequireAdmin(admins, ManualBindHandler(identity)))
	mux.HandleFunc("GET /api/admin/users", RequireAdmin(admins, ListUsersHandler(identity)))
	mux.HandleFunc("GET /api/admin/users/lookup", RequireAdmin(admins, LookupUserHandler(identity)))
	mux.HandleFunc("GET /api/admin/blocklist", RequireAdmin(admins, ListBlockedEmailsHandler(identity)))
	mux.HandleFunc("POST /api/admin/blocklist", RequireAdmin(admins, AddBlockedEmailHandler(identity)))
	mux.HandleFunc("DELETE /api/admin/blocklist/{email}", RequireAdmin(admins, RemoveBlockedEmailHandler(identity)))
	// Deprecated Email invite routes — return 410 Gone
	mux.HandleFunc("POST /api/admin/invites", RequireAdmin(admins, DeprecatedEmailInviteHandler()))
	mux.HandleFunc("GET /api/admin/invites", RequireAdmin(admins, DeprecatedEmailInviteHandler()))
	mux.HandleFunc("POST /api/admin/invitation-codes/generate", RequireAdmin(admins, GenerateInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes", RequireAdmin(admins, ListInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("POST /api/admin/invitation-codes/toggle", RequireAdmin(admins, ToggleInvitationCodeHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/status", RequireAdmin(admins, InvitationCodeStatusHandler(invitationSvc)))
	mux.HandleFunc("GET /api/admin/invitation-codes/export", RequireAdmin(admins, ExportInvitationCodesHandler(invitationSvc)))
	mux.HandleFunc("POST /api/admin/invitation-codes/unbind", RequireAdmin(admins, UnbindInvitationCodeHandler(invitationSvc, identity, deviceSvc, feishuNotifier, imCleaners)))
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
	// Hub LLM configuration
	mux.HandleFunc("GET /api/admin/hub_llm_config", RequireAdmin(admins, GetHubLLMConfigHandler(system)))
	mux.HandleFunc("PUT /api/admin/hub_llm_config", RequireAdmin(admins, UpdateHubLLMConfigHandler(system)))
	mux.HandleFunc("POST /api/admin/hub_llm_test", RequireAdmin(admins, TestHubLLMHandler(system)))
	mux.HandleFunc("GET /api/admin/hub_llm_status", RequireAdmin(admins, HubLLMStatusHandler(hubLLMStatusFn)))
	// TLS configuration
	mux.HandleFunc("GET /api/admin/tls_config", RequireAdmin(admins, GetTLSConfigHandler(hubCfg)))
	mux.HandleFunc("POST /api/admin/tls_config", RequireAdmin(admins, UpdateTLSConfigHandler(hubCfg, configPath, ensureTLSCert, centerSvc)))
	// Smart route permission
	mux.HandleFunc("POST /api/admin/users/smart_route", RequireAdmin(admins, UpdateUserSmartRouteHandler(identity.UsersRepo())))
	mux.HandleFunc("GET /api/admin/smart_route_all", RequireAdmin(admins, GetSmartRouteAllHandler(system)))
	mux.HandleFunc("PUT /api/admin/smart_route_all", RequireAdmin(admins, UpdateSmartRouteAllHandler(system)))
	// Voiceprint management
	if voiceprintSvc != nil {
		mux.HandleFunc("GET /api/admin/voiceprint/config", RequireAdmin(admins, GetVoiceprintConfigHandler(voiceprintSvc)))
		mux.HandleFunc("PUT /api/admin/voiceprint/config", RequireAdmin(admins, UpdateVoiceprintConfigHandler(voiceprintSvc)))
		mux.HandleFunc("POST /api/admin/voiceprint/enroll", RequireAdmin(admins, VoiceprintEnrollHandler(voiceprintSvc, identity.UsersRepo())))
		mux.HandleFunc("POST /api/admin/voiceprint/identify", RequireAdmin(admins, VoiceprintIdentifyHandler(voiceprintSvc)))
		mux.HandleFunc("GET /api/admin/voiceprints", RequireAdmin(admins, ListVoiceprintsHandler(voiceprintSvc)))
		mux.HandleFunc("DELETE /api/admin/voiceprints", RequireAdmin(admins, DeleteVoiceprintHandler(voiceprintSvc)))
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
	mux.HandleFunc("/api/feishu/webhook", feishu.WebhookHandler(feishuNotifier))
	if feishuPlugin != nil {
		mux.HandleFunc("GET /api/feishu/tempfile/{token}", feishuPlugin.ServeTempFile)
	}
	// Public binding page API (no auth required) — allow cross-origin for iframe embedding
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
	mux.HandleFunc("GET /api/debug/machines", RequireAdmin(admins, DebugListMachinesHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/debug/machine-events", RequireAdmin(admins, DebugListMachineEventsHandler(deviceSvc)))
	mux.HandleFunc("GET /api/debug/sessions", RequireAdmin(admins, DebugListSessionsHandler(sessionSvc)))
	mux.HandleFunc("GET /api/debug/session", RequireAdmin(admins, DebugGetSessionHandler(sessionSvc)))
	mux.HandleFunc("/ws", gateway.HandleWS)
	mux.HandleFunc("GET /api/shortcuts", GetShortcutsHandler(identity, system))
	mux.HandleFunc("PUT /api/shortcuts", PutShortcutsHandler(identity, system))
	// Webhook session endpoint (Bearer token auth handled internally)
	mux.HandleFunc("POST /api/webhook/session", WebhookCreateSessionHandler(deviceSvc, sessionSvc))

	// ClawNet identity key online backup/restore (no auth — protected by user password)
	mux.HandleFunc("POST /api/clawnet/key/backup", ClawNetKeyBackupHandler())
	mux.HandleFunc("POST /api/clawnet/key/restore", ClawNetKeyRestoreHandler())

	// ClawNet task bulletin board — Hub-relayed P2P task discovery
	mux.HandleFunc("POST /api/clawnet/tasks/publish", ClawNetTaskPublishHandler())
	mux.HandleFunc("GET /api/clawnet/tasks/browse", ClawNetTaskBrowseHandler())

	// ── User-facing voiceprint self-enrollment ──────────────
	if voiceprintSvc != nil {
		mux.HandleFunc("POST /api/chat/voiceprint/enroll", UserVoiceprintEnrollHandler(identity, voiceprintSvc))
		mux.HandleFunc("GET /api/chat/voiceprint/list", UserVoiceprintListHandler(identity, voiceprintSvc))
		mux.HandleFunc("DELETE /api/chat/voiceprint", UserVoiceprintDeleteHandler(identity, voiceprintSvc))
	}

	// ── Chat Module ─────────────────────────────────────────
	if chatChannelSvc != nil {
		mux.HandleFunc("POST /api/chat/channels", ChatCreateChannelHandler(identity, chatChannelSvc))
		mux.HandleFunc("GET /api/chat/channels", ChatListChannelsHandler(identity, chatChannelSvc))
		mux.HandleFunc("POST /api/chat/channels/{id}/messages", ChatSendMessageHandler(identity, chatChannelSvc, chatMessageSvc))
		mux.HandleFunc("GET /api/chat/channels/{id}/messages", ChatGetMessagesHandler(identity, chatChannelSvc, chatMessageSvc))
		mux.HandleFunc("POST /api/chat/read-receipts", ChatReadReceiptsHandler(identity, chatReadReceiptSvc))
		mux.HandleFunc("POST /api/chat/files/upload", ChatFileUploadHandler(identity, chatChannelSvc, chatFileSvc))
		mux.HandleFunc("GET /api/chat/files/{id}", ChatFileDownloadHandler(identity, chatFileSvc))
		mux.HandleFunc("GET /api/chat/users/{id}/presence", ChatPresenceHandler(identity, chatPresenceSvc))
		mux.HandleFunc("POST /api/chat/voice/call", ChatVoiceCallHandler(identity, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/voice/answer", ChatVoiceAnswerHandler(identity, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/voice/ice", ChatVoiceICEHandler(identity, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/voice/hangup", ChatVoiceHangupHandler(identity, chatVoiceSignaling))
		mux.HandleFunc("POST /api/chat/push/register", ChatPushRegisterHandler(identity, chatStore))
		mux.HandleFunc("POST /api/chat/typing", ChatTypingHandler(identity, chatNotifier))
		mux.HandleFunc("/api/chat/ws", ChatWSHandler(identity, chatNotifier))
	}

	registerPWAStaticRoutes(mux, staticDir, routePrefix)
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	registerBindStaticRoutes(mux, "./web/bind", "/bind")
	return mux
}
