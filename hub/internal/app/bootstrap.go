package app

import (
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func Bootstrap(cfg *config.Config) (*App, error) {
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               cfg.Database.DSN,
		WAL:               cfg.Database.WAL,
		BusyTimeoutMS:     cfg.Database.BusyTimeoutMS,
		MaxReadOpenConns:  cfg.Database.MaxReadOpenConns,
		MaxReadIdleConns:  cfg.Database.MaxReadIdleConns,
		MaxWriteOpenConns: cfg.Database.MaxWriteOpenConns,
		MaxWriteIdleConns: cfg.Database.MaxWriteIdleConns,
		BatchFlushMS:      cfg.Database.BatchFlushMS,
		BatchMaxSize:      cfg.Database.BatchMaxSize,
		BatchQueueSize:    cfg.Database.BatchQueueSize,
	})
	if err != nil {
		return nil, err
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		return nil, err
	}

	st := sqlite.NewStore(provider)
	adminService := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	mailer := mail.New(*cfg, st.System)
	identityService := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.EmailInvites, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, cfg.Identity.EnrollmentMode, cfg.Identity.AllowSelfEnroll, mailer, cfg.Server.PublicBaseURL)
	centerService := center.NewService(cfg, st.System)
	deviceRuntime := device.NewRuntime()
	deviceService := device.NewService(st.Machines, deviceRuntime)
	sessionCache := session.NewCache()
	sessionService := session.NewService(sessionCache, st.Sessions)
	gateway := ws.NewGateway(identityService, deviceService, sessionService)
	sessionService.RegisterListener(gateway.HandleSessionEvent)

	router := httpapi.NewRouter(
		adminService,
		identityService,
		centerService,
		mailer,
		gateway,
		deviceService,
		sessionService,
		cfg.PWA.StaticDir,
		cfg.PWA.RoutePrefix,
	)

	return &App{
		Config:          cfg,
		Provider:        provider,
		AdminService:    adminService,
		IdentityService: identityService,
		CenterService:   centerService,
		DeviceService:   deviceService,
		SessionService:  sessionService,
		Mailer:          mailer,
		WSGateway:       gateway,
		HTTPHandler:     router,
	}, nil
}
