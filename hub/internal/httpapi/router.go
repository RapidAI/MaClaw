package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/entry"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
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
	staticDir string,
	routePrefix string,
) http.Handler {
	entrySvc := entry.NewService(identity)
	var userLookup machineUserLookup
	if identity != nil {
		userLookup = identity.UsersRepo()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler("codeclaw-hub"))
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(admins))
	mux.HandleFunc("POST /api/admin/setup", SetupAdminHandler(admins))
	mux.HandleFunc("POST /api/admin/login", AdminLoginHandler(admins))
	mux.HandleFunc("POST /api/admin/password", RequireAdmin(admins, AdminChangePasswordHandler(admins)))
	mux.HandleFunc("POST /api/admin/profile", RequireAdmin(admins, AdminUpdateProfileHandler(admins)))
	mux.HandleFunc("GET /api/admin/debug/machines", RequireAdmin(admins, DebugListMachinesHandler(deviceSvc, userLookup)))
	mux.HandleFunc("GET /api/admin/debug/machine-events", RequireAdmin(admins, DebugListMachineEventsHandler(deviceSvc)))
	mux.HandleFunc("GET /api/admin/debug/sessions", RequireAdmin(admins, DebugListSessionsHandler(sessionSvc)))
	mux.HandleFunc("GET /api/admin/debug/session", RequireAdmin(admins, DebugGetSessionHandler(sessionSvc)))
	mux.HandleFunc("POST /api/admin/users/manual-bind", RequireAdmin(admins, ManualBindHandler(identity)))
	mux.HandleFunc("GET /api/admin/users", RequireAdmin(admins, ListUsersHandler(identity)))
	mux.HandleFunc("GET /api/admin/users/lookup", RequireAdmin(admins, LookupUserHandler(identity)))
	mux.HandleFunc("GET /api/admin/blocklist", RequireAdmin(admins, ListBlockedEmailsHandler(identity)))
	mux.HandleFunc("POST /api/admin/blocklist", RequireAdmin(admins, AddBlockedEmailHandler(identity)))
	mux.HandleFunc("DELETE /api/admin/blocklist/{email}", RequireAdmin(admins, RemoveBlockedEmailHandler(identity)))
	mux.HandleFunc("GET /api/admin/invites", RequireAdmin(admins, ListInvitesHandler(identity)))
	mux.HandleFunc("POST /api/admin/invites", RequireAdmin(admins, AddInviteHandler(identity)))
	mux.HandleFunc("GET /api/admin/center/status", RequireAdmin(admins, GetCenterStatusHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/center/config", RequireAdmin(admins, UpdateCenterConfigHandler(centerSvc, identity)))
	mux.HandleFunc("GET /api/admin/mail/config", RequireAdmin(admins, GetMailConfigHandler(mailer)))
	mux.HandleFunc("POST /api/admin/mail/config", RequireAdmin(admins, UpdateMailConfigHandler(mailer)))
	mux.HandleFunc("POST /api/admin/center/register", RequireAdmin(admins, RegisterCenterHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/mail/test", RequireAdmin(admins, AdminSendTestMailHandler(mailer)))
	mux.HandleFunc("POST /api/enroll/start", EnrollStartHandler(identity))
	mux.HandleFunc("POST /api/auth/email-request", EmailRequestLoginHandler(identity))
	mux.HandleFunc("POST /api/auth/email-confirm", EmailConfirmLoginHandler(identity))
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
	registerPWAStaticRoutes(mux, staticDir, routePrefix)
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	return mux
}
