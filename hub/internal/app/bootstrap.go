package app

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/skill"
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
	invitationService := invitation.NewService(st.InvitationCodes, st.System)

	identityService := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, invitationService, cfg.Identity.EnrollmentMode, cfg.Identity.AllowSelfEnroll, mailer, cfg.Server.PublicBaseURL)
	centerService := center.NewService(cfg, st.System)
	deviceRuntime := device.NewRuntime()
	deviceService := device.NewService(st.Machines, deviceRuntime)
	sessionCache := session.NewCache()
	sessionService := session.NewService(sessionCache, st.Sessions)
	gateway := ws.NewGateway(identityService, deviceService, sessionService)
	sessionService.RegisterListener(gateway.HandleSessionEvent)

	// Feishu notifier: push session events to users via Feishu cards.
	feishuAppID, feishuAppSecret := cfg.Feishu.AppID, cfg.Feishu.AppSecret
	if raw, err := st.System.Get(context.Background(), "feishu_config"); err == nil && raw != "" {
		var dbCfg struct {
			Enabled   bool   `json:"enabled"`
			AppID     string `json:"app_id"`
			AppSecret string `json:"app_secret"`
		}
		if json.Unmarshal([]byte(raw), &dbCfg) == nil && dbCfg.Enabled && dbCfg.AppID != "" && dbCfg.AppSecret != "" {
			feishuAppID = dbCfg.AppID
			feishuAppSecret = dbCfg.AppSecret
		}
	}
	feishuNotifier := feishu.New(feishuAppID, feishuAppSecret, st.Users, st.System, mailer)
	feishuNotifier.SetServices(&feishu.DeviceServiceAdapter{Svc: deviceService}, sessionService)

	// Feishu auto-enrollment: when users register on the desktop client,
	// automatically add them to the Feishu organization so they can discover
	// and use the bot (requires contact:user write scope).
	autoEnroller := feishu.NewAutoEnroller(feishuNotifier.Bot, feishuNotifier.BindOpenID)
	autoEnroller.SetConfig(feishu.LoadAutoEnrollSetting(context.Background(), st.System))
	feishuNotifier.SetAutoEnroller(autoEnroller)

	// -----------------------------------------------------------------------
	// Agent Passthrough IM modules
	// -----------------------------------------------------------------------

	// 1. MessageRouter — routes IM messages to MaClaw Agent via WebSocket
	deviceFinder := &im.DeviceServiceFinder{Svc: deviceService}
	messageRouter := im.NewMessageRouter(deviceFinder)

	// 2. IM_Adapter — create with a temporary nil identity resolver; we wire
	//    the real one (PluginIdentityResolver) after plugin registration.
	imAdapter := im.NewAdapter(messageRouter, nil)

	// Wire the PluginIdentityResolver now that the adapter exists.
	pluginIdentity := im.NewPluginIdentityResolver(imAdapter)
	imAdapter.SetIdentityResolver(pluginIdentity)

	// 3. Wire MessageRouter's agent response handler into the WebSocket Gateway
	//    so im.agent_response messages from MaClaw clients are routed back.
	wsResponder := &im.WSAgentResponder{Router: messageRouter}
	gateway.SetIMResponder(wsResponder)

	// 3b. Wire progress delivery so the MessageRouter can send intermediate
	//     status updates to users via IM during long-running agent tasks.
	messageRouter.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		imAdapter.DeliverProgress(ctx, platformName, userID, platformUID, text)
	})

	// 4. Feishu_Plugin
	feishuPlugin := feishu.NewPlugin(feishuNotifier)

	// 5. Register Feishu_Plugin with IM_Adapter
	if err := imAdapter.RegisterPlugin(feishuPlugin); err != nil {
		log.Printf("[bootstrap] failed to register feishu plugin: %v", err)
	}

	// 6. Wire the plugin back to the notifier so handleBotMessage routes
	//     through the IM Adapter pipeline.
	feishuNotifier.SetPlugin(feishuPlugin)
	feishuPlugin.SetAdapter(imAdapter)

	// 7. OpenClaw IM Webhook Plugin — enables external IM adapters to
	//     communicate with Hub via the OpenClaw IM protocol.
	openclawIMPlugin := im.NewWebhookIMPlugin("openclaw", func() im.WebhookConfig {
		raw, err := st.System.Get(context.Background(), "openclaw_im_config")
		if err != nil || raw == "" {
			return im.WebhookConfig{}
		}
		var cfg struct {
			Enabled    bool   `json:"enabled"`
			WebhookURL string `json:"webhook_url"`
			Secret     string `json:"secret"`
		}
		if json.Unmarshal([]byte(raw), &cfg) != nil || !cfg.Enabled {
			return im.WebhookConfig{}
		}
		return im.WebhookConfig{WebhookURL: cfg.WebhookURL, Secret: cfg.Secret}
	})
	if err := imAdapter.RegisterPlugin(openclawIMPlugin); err != nil {
		log.Printf("[bootstrap] failed to register openclaw IM plugin: %v", err)
	}

	// 8. QQBot Plugin — connects to QQ Bot via WebSocket gateway
	qqbotPlugin := qqbot.New(func() qqbot.Config {
		raw, err := st.System.Get(context.Background(), "qqbot_config")
		if err != nil || raw == "" {
			return qqbot.Config{}
		}
		var cfg qqbot.Config
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			return qqbot.Config{}
		}
		return cfg
	}, st.Users, st.System, mailer)
	if err := imAdapter.RegisterPlugin(qqbotPlugin); err != nil {
		log.Printf("[bootstrap] failed to register qqbot plugin: %v", err)
	}
	// Provide the hub's public URL so IM plugins can serve temp files for large uploads.
	// GetPublicBaseURL prefers the database value (set via admin panel) over the config file.
	if publicBaseURL := centerService.GetPublicBaseURL(context.Background()); publicBaseURL != "" {
		qqbotPlugin.SetPublicBaseURL(publicBaseURL)
		feishuPlugin.SetPublicBaseURL(publicBaseURL)
	}
	// Start QQBot WebSocket gateway if configured
	if err := qqbotPlugin.Start(context.Background()); err != nil {
		log.Printf("[bootstrap] failed to start qqbot plugin: %v", err)
	}

	// 9. Cross-IM NotifyBroadcaster — sends verification codes to all
	//    reachable channels (email + any already-bound IM platforms).
	broadcaster := im.NewNotifyBroadcaster(imAdapter, mailer)
	qqbotPlugin.SetBroadcaster(broadcaster)
	feishuNotifier.SetBroadcaster(broadcaster)

	// Wire login link broadcaster into identity service so PWA login
	// confirmation links are also sent to bound IM channels.
	identityService.SetLoginNotifier(broadcaster)

	// Register session event listener — routes through IM Adapter when available,
	// falls back to legacy notifier path.
	sessionService.RegisterListener(feishuNotifier.HandleEvent)

	// Skill store: derive directory from database DSN path.
	skillStoreDir := filepath.Join(filepath.Dir(cfg.Database.DSN), "skills")
	skillStore := skill.NewSkillStore(skillStoreDir)

	router := httpapi.NewRouter(
		adminService,
		identityService,
		centerService,
		mailer,
		gateway,
		deviceService,
		sessionService,
		invitationService,
		st.System,
		feishuNotifier,
		feishuPlugin,
		openclawIMPlugin,
		qqbotPlugin,
		skillStore,
		cfg.PWA.StaticDir,
		cfg.PWA.RoutePrefix,
		cfg.Bridge.Dir,
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

		// Agent Passthrough IM modules
		MessageRouter:    messageRouter,
		IMAdapter:        imAdapter,
		FeishuPlugin:     feishuPlugin,
		OpenclawIMPlugin: openclawIMPlugin,
		QQBotPlugin:      qqbotPlugin,

		// Skill store
		SkillStore: skillStore,
	}, nil
}
