package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	chatpush "github.com/RapidAI/CodeClaw/hub/internal/chat/push"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/voiceprint"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func Bootstrap(cfg *config.Config, configPath string) (*App, error) {
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

	// Hub LLM Coordinator — sits between Adapter and MessageRouter.
	// Provides seamless smart mode when Hub LLM is configured.
	llmConfigProvider := func() *im.HubLLMConfig {
		raw, err := st.System.Get(context.Background(), "hub_llm_config")
		if err != nil || raw == "" {
			return nil
		}
		var cfg im.HubLLMConfig
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			return nil
		}
		if !cfg.Enabled {
			return nil
		}
		return &cfg
	}
	coordinator := im.NewCoordinator(messageRouter, deviceFinder, llmConfigProvider)
	imAdapter.SetCoordinator(coordinator)

	// Wire smart route permission checker so only authorized users get LLM features.
	smartRouteChecker := im.NewDBSmartRouteChecker(
		&smartRouteUserAdapter{users: st.Users},
		st.System,
	)
	coordinator.SetSmartRouteChecker(smartRouteChecker)

	// Wire the DiscussionConductor into the MessageRouter so /discuss
	// can delegate to LLM-orchestrated discussions when available.
	discussionConductor := im.NewDiscussionConductor(llmConfigProvider, coordinator.Breaker(), messageRouter)
	messageRouter.SetConductor(discussionConductor)

	// Device notifier — sends online/offline notifications to active IM users.
	deviceNotifier := im.NewDeviceNotifier(imAdapter, coordinator)
	imAdapter.SetDeviceNotifier(deviceNotifier)

	// Wire device profile updates and connect/disconnect hooks into the gateway.
	gateway.SetDeviceProfileUpdater(coordinator.HandleDeviceProfileUpdate)
	gateway.SetDeviceNotifyHook(ws.DeviceNotifyHook{
		OnConnect:    deviceNotifier.NotifyDeviceOnline,
		OnDisconnect: deviceNotifier.NotifyDeviceOffline,
	})

	// 3. Wire MessageRouter's agent response handler into the WebSocket Gateway
	//    so im.agent_response messages from MaClaw clients are routed back.
	wsResponder := &im.WSAgentResponder{Router: messageRouter}
	gateway.SetIMResponder(wsResponder)

	// 3b. Wire progress delivery so the MessageRouter can send intermediate
	//     status updates to users via IM during long-running agent tasks.
	messageRouter.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		imAdapter.DeliverProgress(ctx, platformName, userID, platformUID, text)
	})

	// 3c. Wire response delivery so the MessageRouter can deliver full
	//     GenericResponse messages (images, files) individually in broadcast mode.
	messageRouter.SetResponseDelivery(func(ctx context.Context, userID, platformName, platformUID string, resp *im.GenericResponse) {
		imAdapter.DeliverResponse(ctx, platformName, userID, platformUID, resp)
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

	// 8a. Remote Gateway Plugins — client-side IM gateways (QQ Bot, Telegram)
	//     forwarded through the existing Hub↔Client WebSocket.
	qqRemotePlugin := im.NewRemoteGatewayPlugin("qqbot_remote", deviceService, st.Users, st.System)
	if err := imAdapter.RegisterPlugin(qqRemotePlugin); err != nil {
		log.Printf("[bootstrap] failed to register qqbot_remote plugin: %v", err)
	}
	telegramPlugin := im.NewRemoteGatewayPlugin("telegram", deviceService, st.Users, st.System)
	if err := imAdapter.RegisterPlugin(telegramPlugin); err != nil {
		log.Printf("[bootstrap] failed to register telegram plugin: %v", err)
	}
	weixinPlugin := im.NewRemoteGatewayPlugin("weixin", deviceService, st.Users, st.System)
	if err := imAdapter.RegisterPlugin(weixinPlugin); err != nil {
		log.Printf("[bootstrap] failed to register weixin plugin: %v", err)
	}
	gateway.RegisterIMGatewayPlugin(qqRemotePlugin)
	gateway.RegisterIMGatewayPlugin(telegramPlugin)
	gateway.RegisterIMGatewayPlugin(weixinPlugin)

	// 8b. QQBot Plugin — connects to QQ Bot via WebSocket gateway (Hub-native)
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
	broadcaster.SetActiveUserProvider(deviceNotifier)
	qqbotPlugin.SetBroadcaster(broadcaster)
	feishuNotifier.SetBroadcaster(broadcaster)

	// 10. Proactive message sender — allows MaClaw clients to push
	//     non-request-based messages (e.g. scheduled task results) to users.
	proactiveSender := im.NewProactiveSender(broadcaster, &userEmailLookup{users: st.Users})
	gateway.SetIMProactiveSender(proactiveSender)

	// When a user's second device comes online, push a multi-device usage
	// guide to their IM so they know how to switch between machines.
	deviceService.OnMultiDeviceOnline = func(userID string, machineNames []string) {
		names := strings.Join(machineNames, "、")
		guide := fmt.Sprintf("🖥️ 您有 %d 台设备同时在线：%s\n\n"+
			"使用方式：\n"+
			"• /call <昵称> — 切换到指定设备\n"+
			"• /call all — 进入群聊模式（所有设备同时回复）\n"+
			"• /machines — 查看在线设备列表\n"+
			"• /discuss <话题> — 让多台设备 AI 讨论\n\n"+
			"当前默认与第一台上线的设备交流，发送 /call <昵称> 切换。",
			len(machineNames), names)
		if err := proactiveSender.SendProactiveMessage(context.Background(), userID, guide); err != nil {
			log.Printf("[bootstrap] multi-device guide send failed for user=%s: %v", userID, err)
		}
	}

	// Wire login link broadcaster into identity service so PWA login
	// confirmation links are also sent to bound IM channels.
	identityService.SetLoginNotifier(broadcaster)

	// Register session event listener — routes through IM Adapter when available,
	// falls back to legacy notifier path.
	sessionService.RegisterListener(feishuNotifier.HandleEvent)

	// ── Chat Module ─────────────────────────────────────────
	chatStore, err := chat.NewStore(provider.Write)
	if err != nil {
		return nil, fmt.Errorf("chat store: %w", err)
	}

	// Push dispatcher: look up tokens from chat store.
	pushDispatcher := chatpush.NewDispatcher(func(userID string) ([]chatpush.TokenInfo, error) {
		tokens, err := chatStore.GetPushTokens(userID)
		if err != nil {
			return nil, err
		}
		var infos []chatpush.TokenInfo
		for _, t := range tokens {
			infos = append(infos, chatpush.TokenInfo{Platform: t.Platform, Token: t.Token})
		}
		return infos, nil
	})

	chatNotifier := chat.NewNotifier(chatStore, pushDispatcher)
	chatChannelSvc := chat.NewChannelService(chatStore)
	chatMessageSvc := chat.NewMessageService(chatStore, chatNotifier)
	chatFileSvc := chat.NewFileService(chatStore, "./data/chat_files")
	chatReadReceiptSvc := chat.NewReadReceiptService(chatStore)
	chatPresenceSvc := chat.NewPresenceService(chatStore, chatNotifier)
	chatVoiceSignaling := chat.NewVoiceSignaling(chatStore, chatNotifier)

	voiceprintSvc := voiceprint.NewService(st.Voiceprints, st.System)

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
		coordinator.GetLLMStatus,
		coordinator.ConvContextStats,
		chatStore,
		chatChannelSvc,
		chatMessageSvc,
		chatFileSvc,
		chatReadReceiptSvc,
		chatPresenceSvc,
		chatVoiceSignaling,
		chatNotifier,
		voiceprintSvc,
		cfg,
		configPath,
		EnsureSelfSignedCert,
		cfg.PWA.StaticDir,
		cfg.PWA.RoutePrefix,
		cfg.Bridge.Dir,
	)

	return &App{
		Config:          cfg,
		ConfigPath:      configPath,
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
		QQRemotePlugin:   qqRemotePlugin,
		TelegramPlugin:   telegramPlugin,
		ChatNotifier:     chatNotifier,
	}, nil
}

// userEmailLookup adapts store.UserRepository to im.UserLookup.
type userEmailLookup struct {
	users interface {
		GetByID(ctx context.Context, id string) (*store.User, error)
	}
}

func (u *userEmailLookup) GetEmail(ctx context.Context, userID string) (string, error) {
	user, err := u.users.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", fmt.Errorf("user not found: %s", userID)
	}
	return user.Email, nil
}

// smartRouteUserAdapter adapts store.UserRepository to im.SmartRouteStore.
type smartRouteUserAdapter struct {
	users interface {
		GetByID(ctx context.Context, id string) (*store.User, error)
	}
}

func (a *smartRouteUserAdapter) GetSmartRouteByUserID(ctx context.Context, userID string) (bool, error) {
	user, err := a.users.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	return user.SmartRoute, nil
}
