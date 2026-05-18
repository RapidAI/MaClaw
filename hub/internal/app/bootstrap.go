package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	chatpush "github.com/RapidAI/CodeClaw/hub/internal/chat/push"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hub/internal/dingtalk"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/voiceprint"
	"github.com/RapidAI/CodeClaw/hub/internal/wecom"
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
	log.Printf("[hub] sqlite migrations complete for %s", cfg.Database.DSN)

	st := sqlite.NewStore(provider)
	promptCacheCfg := httpapi.LoadHubLLMPromptCacheConfig(context.Background(), st.System)
	promptCache := llmcache.New(st.LLMPromptCache, llmcache.Config{MemoryMaxEntries: promptCacheCfg.MemoryMaxEntries, MemoryMaxBytes: promptCacheCfg.MemoryMaxBytes})
	adminService := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	mailer := mail.New(*cfg, st.System)
	invitationService := invitation.NewService(st.InvitationCodes, st.System)

	identityService := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, invitationService, cfg.Identity.EnrollmentMode, cfg.Identity.AllowSelfEnroll, mailer, cfg.Server.PublicBaseURL)
	centerService := center.NewService(cfg, st.System)
	failureRecorder := diagnostics.NewFailureEventRecorder(st.FailureLogs)
	centerService.SetFailureEventRecorder(failureRecorder)
	if status, err := centerService.Status(context.Background()); err == nil {
		log.Printf("[hub] center registration bootstrap: visibility=%s enrollment=%s corporate_domains=%v accept_public_signup=%t registered=%t pending=%t disabled=%t", status.Visibility, status.EnrollmentMode, status.CorporateEmailDomains, status.AcceptPublicSignup, status.Registered, status.PendingConfirmation, status.Disabled)
	} else {
		log.Printf("[hub] center registration bootstrap status unavailable: %v", err)
	}
	deviceRuntime := device.NewRuntime()
	deviceService := device.NewService(st.Machines, deviceRuntime)
	centerService.SetStatsProviders(identityService, deviceService)
	deviceService.ResetStaleOnlineStatus(context.Background())
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

	// 1. MessageRouter 闂?routes IM messages to MaClaw Agent via WebSocket
	deviceFinder := &im.DeviceServiceFinder{Svc: deviceService}
	messageRouter := im.NewMessageRouter(deviceFinder)

	// 2. IM_Adapter 闂?create with a temporary nil identity resolver; we wire
	//    the real one (PluginIdentityResolver) after plugin registration.
	imAdapter := im.NewAdapter(messageRouter, nil)

	// Wire the PluginIdentityResolver now that the adapter exists.
	pluginIdentity := im.NewPluginIdentityResolver(imAdapter)
	imAdapter.SetIdentityResolver(pluginIdentity)

	// Hub LLM Coordinator 闂?sits between Adapter and MessageRouter.
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

	// 闂佸啿鍘滈崑鎾绘煃閸忓浜?Workflow Engine (removed) 闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑鎾绘煃閸忓浜鹃梺鍐插帨閸嬫捇鏌嶉崗澶婁壕闂佸啿鍘滈崑?	// Workflow logic is now handled by the device-side agent (corelib/workflow).
	// Hub no longer creates WorkflowEngine, WorkflowRegistry, or UnderstandingManager.
	// The /workflow command is forwarded to the device via RouteToAgent.

	// Start background goroutine for periodic cleanup.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if st.WorkflowRepo != nil {
				_ = st.WorkflowRepo.CleanupExpired(context.Background(), 7*24*time.Hour)
			}
			if promptCache != nil {
				cacheCfg := httpapi.LoadHubLLMPromptCacheConfig(context.Background(), st.System)
				promptCache.UpdateConfig(llmcache.Config{MemoryMaxEntries: cacheCfg.MemoryMaxEntries, MemoryMaxBytes: cacheCfg.MemoryMaxBytes})
				_, _ = promptCache.DeleteExpired(context.Background(), time.Now().UTC())
				_, _ = promptCache.TrimDiskToBytes(context.Background(), cacheCfg.DiskMaxBytes)
			}
		}
	}()

	// Initialize background task dispatcher 闂?enables non-blocking IM:
	// simple queries (direct_answer) are answered immediately, while
	// device-bound tasks are queued and results pushed asynchronously.
	imAdapter.InitTaskDispatcher(5)

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

	// Device notifier 闂?sends online/offline notifications to active IM users.
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

	// 7. OpenClaw IM Webhook Plugin 闂?enables external IM adapters to
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

	// 8a. Remote Gateway Plugins 闂?client-side IM gateways (QQ Bot, Telegram)
	//     forwarded through the existing Hub闂備焦鍓氶崑鍛村箺閺勭ent WebSocket.
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
	lansengerPlugin := im.NewRemoteGatewayPlugin("lansenger", deviceService, st.Users, st.System)
	if err := imAdapter.RegisterPlugin(lansengerPlugin); err != nil {
		log.Printf("[bootstrap] failed to register lansenger plugin: %v", err)
	}
	gateway.RegisterIMGatewayPlugin(qqRemotePlugin)
	gateway.RegisterIMGatewayPlugin(telegramPlugin)
	gateway.RegisterIMGatewayPlugin(weixinPlugin)
	gateway.RegisterIMGatewayPlugin(lansengerPlugin)

	// 8b. QQBot Plugin 闂?connects to QQ Bot via WebSocket gateway (Hub-native)
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

	// 8c. WeCom Plugin 闂?connects to WeCom Bot via WebSocket gateway (Hub-native)
	wecomPlugin := wecom.New(func() wecom.Config {
		raw, err := st.System.Get(context.Background(), "wecom_config")
		if err != nil || raw == "" {
			return wecom.Config{}
		}
		var cfg wecom.Config
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			return wecom.Config{}
		}
		return cfg
	}, st.Users, st.System, mailer)
	if err := imAdapter.RegisterPlugin(wecomPlugin); err != nil {
		log.Printf("[bootstrap] failed to register wecom plugin: %v", err)
	}
	if publicBaseURL := centerService.GetPublicBaseURL(context.Background()); publicBaseURL != "" {
		wecomPlugin.SetPublicBaseURL(publicBaseURL)
	}
	if err := wecomPlugin.Start(context.Background()); err != nil {
		log.Printf("[bootstrap] failed to start wecom plugin: %v", err)
	}

	// 8d. DingTalk Plugin 闂?connects to DingTalk Bot via Stream Mode (Hub-native)
	dingtalkPlugin := dingtalk.New(func() dingtalk.Config {
		raw, err := st.System.Get(context.Background(), "dingtalk_config")
		if err != nil || raw == "" {
			return dingtalk.Config{}
		}
		var cfg dingtalk.Config
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			return dingtalk.Config{}
		}
		return cfg
	}, st.Users, st.System, mailer)
	if err := imAdapter.RegisterPlugin(dingtalkPlugin); err != nil {
		log.Printf("[bootstrap] failed to register dingtalk plugin: %v", err)
	}
	if err := dingtalkPlugin.Start(context.Background()); err != nil {
		log.Printf("[bootstrap] failed to start dingtalk plugin: %v", err)
	}

	// 9. Cross-IM NotifyBroadcaster 闂?sends verification codes to all
	//    reachable channels (email + any already-bound IM platforms).
	broadcaster := im.NewNotifyBroadcaster(imAdapter, mailer)
	broadcaster.SetActiveUserProvider(deviceNotifier)
	qqbotPlugin.SetBroadcaster(broadcaster)
	feishuNotifier.SetBroadcaster(broadcaster)
	wecomPlugin.SetBroadcaster(broadcaster)
	dingtalkPlugin.SetBroadcaster(broadcaster)

	// 10. Proactive message sender 闂?allows MaClaw clients to push
	//     non-request-based messages (e.g. scheduled task results) to users.
	proactiveSender := im.NewProactiveSender(broadcaster, &userEmailLookup{users: st.Users})
	gateway.SetIMProactiveSender(proactiveSender)

	// When a user's second device comes online, push a quick multi-device guide.
	deviceService.OnMultiDeviceOnline = func(tenantID, userID string, machineNames []string) {
		names := strings.Join(machineNames, ", ")
		guide := fmt.Sprintf("You now have %d devices online: %s\n\nUse /call <name> to switch devices, /call all for group mode, /machines to view online devices, and /discuss <topic> for multi-device AI discussion.", len(machineNames), names)
		if err := proactiveSender.SendProactiveMessage(context.Background(), tenantID, userID, guide); err != nil {
			log.Printf("[bootstrap] multi-device guide send failed for user=%s: %v", userID, err)
		}
	}

	// Wire login link broadcaster into identity service so PWA login
	// confirmation links are also sent to bound IM channels.
	identityService.SetLoginNotifier(broadcaster)
	identityService.SetUserRouteSyncer(centerService)

	// Register session event listener 闂?routes through IM Adapter when available,
	// falls back to legacy notifier path.
	sessionService.RegisterListener(feishuNotifier.HandleEvent)

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

	securityStore := security.NewSecurityStore(provider.Write)
	if err := securityStore.InitSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("security schema: %w", err)
	}
	if err := securityStore.InitRootGroup(context.Background()); err != nil {
		return nil, fmt.Errorf("security root group: %w", err)
	}
	securitySvc := security.NewSecurityService(securityStore, st.System, st.AdminAudit, st.Users)

	// Inject SecurityProvider into ws.Gateway for heartbeat policy delivery.
	gateway.SecurityProvider = securitySvc
	gateway.ConfigProvider = heartbeatConfigProvider{settings: st.System}

	// Wire OutboundInterceptor into im.Adapter for file/image outbound checks.
	outboundInterceptor := im.NewOutboundInterceptor(securitySvc, nil)
	imAdapter.SetOutboundInterceptor(outboundInterceptor)

	// Wire ContentAuditor into im.Adapter for outbound content compliance checks.
	contentAuditLogStore := im.NewSQLiteAuditLogStore(provider.Write)
	contentAuditConfigProvider := func() *im.ContentAuditDynamicConfig {
		raw, err := st.System.Get(context.Background(), "content_audit_config")
		if err != nil || raw == "" {
			return nil
		}
		var dynCfg im.ContentAuditDynamicConfig
		if err := json.Unmarshal([]byte(raw), &dynCfg); err != nil {
			return nil
		}
		return &dynCfg
	}
	contentAuditor := im.NewContentAuditor(
		cfg.ContentAudit.ProgramPath,
		cfg.ContentAudit.TimeoutSeconds,
		cfg.ContentAudit.TimeoutPolicy,
		contentAuditLogStore,
		contentAuditConfigProvider,
	)
	imAdapter.SetContentAuditor(contentAuditor)

	router := httpapi.NewRouter(
		adminService,
		identityService,
		centerService,
		mailer,
		gateway,
		deviceService,
		sessionService,
		invitationService,
		st.EmailInvites,
		st.System,
		provider.Write,
		promptCache,
		st.AdminAudit,
		st.FailureLogs,
		feishuNotifier,
		feishuPlugin,
		openclawIMPlugin,
		qqbotPlugin,
		wecomPlugin,
		dingtalkPlugin,
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
		securitySvc,
		cfg,
		configPath,
		EnsureSelfSignedCert,
		cfg.PWA.StaticDir,
		cfg.PWA.RoutePrefix,
		cfg.Bridge.Dir,
		st.Tenants,
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
		WecomPlugin:      wecomPlugin,
		DingTalkPlugin:   dingtalkPlugin,
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

func (u *userEmailLookup) GetEmail(ctx context.Context, tenantID, userID string) (string, error) {
	user, err := u.users.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", fmt.Errorf("user not found: %s", userID)
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	if strings.TrimSpace(user.TenantID) != "" && strings.TrimSpace(user.TenantID) != tenantID {
		return "", fmt.Errorf("user %s is not in tenant %s", userID, tenantID)
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

type heartbeatConfigProvider struct {
	settings store.SystemSettingsRepository
}

func (p heartbeatConfigProvider) GetHeartbeatConfig(ctx context.Context, userID string, tenantID string) (*ws.HeartbeatConfigPayload, error) {
	settings := scopedSystemSettingsForTenant(tenantID, p.settings)
	policy := corelib.DefaultCapabilityMarketPolicy()
	if settings != nil {
		raw, err := settings.Get(ctx, "capability_market_policy")
		if err != nil {
			log.Printf("[hub-config] failed to read capability_market_policy: %v", err)
			// Don't return error — capability_market_policy failure should not
			// prevent digital_employee_authorization from being delivered.
		} else if strings.TrimSpace(raw) != "" {
			if err := json.Unmarshal([]byte(raw), &policy); err != nil {
				log.Printf("[hub-config] failed to parse capability_market_policy: %v", err)
			} else {
				policy = policy.WithDefaults()
			}
		}
	}
	return &ws.HeartbeatConfigPayload{CapabilityMarketPolicy: policy, DigitalEmployeeAuthorization: center.LoadDigitalEmployeeAuthorizationForTenant(ctx, p.settings, tenantID)}, nil
}

type tenantScopedSystemSettings struct {
	tenantID string
	base     store.SystemSettingsRepository
}

func scopedSystemSettingsForTenant(tenantID string, base store.SystemSettingsRepository) store.SystemSettingsRepository {
	tenantID = strings.TrimSpace(tenantID)
	if base == nil || tenantID == "" || tenantID == store.DefaultTenantID {
		return base
	}
	return tenantScopedSystemSettings{tenantID: tenantID, base: base}
}

func (s tenantScopedSystemSettings) Set(ctx context.Context, key, valueJSON string) error {
	return s.base.Set(ctx, s.key(key), valueJSON)
}

func (s tenantScopedSystemSettings) Get(ctx context.Context, key string) (string, error) {
	return s.base.Get(ctx, s.key(key))
}

func (s tenantScopedSystemSettings) TenantID() string {
	return s.tenantID
}

func (s tenantScopedSystemSettings) key(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "tenant:") || isGlobalHeartbeatSettingKey(key) {
		return key
	}
	return "tenant:" + s.tenantID + ":" + key
}

func isGlobalHeartbeatSettingKey(key string) bool {
	switch key {
	case "center_registration", "center_base_url", "admin_email", "hub_installation_id", "server_public_base_url":
		return true
	default:
		return false
	}
}
