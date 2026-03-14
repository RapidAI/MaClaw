package app

import (
	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
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
	hubService := hubs.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, cfg.Server.PublicBaseURL)
	entryService := entry.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)
	router := httpapi.NewRouter(adminService, hubService, entryService, mailer)

	return &App{
		Config:       cfg,
		Provider:     provider,
		Store:        st,
		AdminService: adminService,
		HubService:   hubService,
		EntryService: entryService,
		Mailer:       mailer,
		HTTPHandler:  router,
	}, nil
}
