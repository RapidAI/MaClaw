package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
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
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/wecom"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func Bootstrap(cfg *config.Config, configPath string) (*App, error) {
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:                   cfg.Database.DSN,
		WAL:                   cfg.Database.WAL,
		BusyTimeoutMS:         cfg.Database.BusyTimeoutMS,
		MaxReadOpenConns:      cfg.Database.MaxReadOpenConns,
		MaxReadIdleConns:      cfg.Database.MaxReadIdleConns,
		MaxWriteOpenConns:     cfg.Database.MaxWriteOpenConns,
		MaxWriteIdleConns:     cfg.Database.MaxWriteIdleConns,
		BatchFlushMS:          cfg.Database.BatchFlushMS,
		BatchMaxSize:          cfg.Database.BatchMaxSize,
		BatchQueueSize:        cfg.Database.BatchQueueSize,
		CacheSizeKB:           cfg.Database.CacheSizeKB,
		MmapSizeBytes:         cfg.Database.MmapSizeBytes,
		CheckpointIntervalSec: cfg.Database.CheckpointIntervalSec,
		CoalesceFlushMS:       cfg.Database.CoalesceFlushMS,
		CoalesceMaxBatch:      cfg.Database.CoalesceMaxBatch,
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
	identityService.SetTenantRepository(st.Tenants)
	centerService := center.NewService(cfg, st.System)
	centerService.SetConfigPath(configPath)
	failureRecorder := diagnostics.NewFailureEventRecorder(st.FailureLogs)
	centerService.SetFailureEventRecorder(failureRecorder)
	invitationService.SetCenterSyncer(centerService)
	if status, err := centerService.Status(context.Background()); err == nil {
		log.Printf("[hub] center registration bootstrap: visibility=%s enrollment=%s corporate_domains=%v accept_public_signup=%t registered=%t pending=%t disabled=%t", status.Visibility, status.EnrollmentMode, status.CorporateEmailDomains, status.AcceptPublicSignup, status.Registered, status.PendingConfirmation, status.Disabled)
	} else {
		log.Printf("[hub] center registration bootstrap status unavailable: %v", err)
	}
	deviceRuntime := device.NewRuntime()
	deviceService := device.NewService(st.Machines, deviceRuntime)
	centerService.SetStatsProviders(identityService, deviceService)
	centerService.SetTenantRepository(st.Tenants)
	centerService.SetTenantAdminProvider(adminService)
	deviceService.ResetStaleOnlineStatus(context.Background())
	sessionCache := session.NewCache()
	sessionService := session.NewService(sessionCache, st.Sessions)
	centerService.SetStatsProviders(identityService, deviceService, sessionService)
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
	startMenuRoot := filepath.Join(hubRuntimeDataDir(cfg, configPath), "welcome-sync")
	imAdapter.SetStartMenuTemplateStore(im.NewStartMenuTemplateStore(startMenuRoot, func(ctx context.Context, tenantID, userID string) (string, error) {
		user, err := st.Users.GetByID(ctx, userID)
		if err != nil {
			return "", err
		}
		if user == nil || (tenantID != "" && user.TenantID != tenantID) {
			return "", fmt.Errorf("未找到当前用户")
		}
		return user.Email, nil
	}))

	// Wire the PluginIdentityResolver now that the adapter exists.
	pluginIdentity := im.NewPluginIdentityResolver(imAdapter)
	imAdapter.SetIdentityResolver(pluginIdentity)

	// Server-side IM LLM uses the reserved system-free service group.
	// MaClaw official client is attached after InitMaClawModule below.
	systemLLMResolver := &im.SystemLLMResolver{
		System: st.System,
		Scope: func(tenantID string, base store.SystemSettingsRepository) store.SystemSettingsRepository {
			return scopedSystemSettingsForTenant(tenantID, base)
		},
	}
	im.SetSystemLLMResolver(systemLLMResolver)
	// Hub LLM Coordinator sits between Adapter and MessageRouter.
	// Provides seamless smart mode when system-free (or legacy hub_llm_config) is available.
	llmConfigProvider := systemLLMResolver.ConfigProvider(func(ctx context.Context) *im.HubLLMConfig {
		return loadGlobalHubLLMConfig(ctx, st.System)
	})
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
	openclawIMPlugin := im.NewWebhookIMPlugin("openclaw", func(ctx context.Context) im.WebhookConfig {
		tenantID := im.TenantIDFromContext(ctx)
		system := st.System
		configTenantID := ""
		if tenantID != "" && tenantID != store.DefaultTenantID {
			system = httpapi.ScopedSystemSettingsForTenant(tenantID, st.System)
			configTenantID = tenantID
		}
		raw, err := system.Get(context.Background(), "openclaw_im_config")
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
		return im.WebhookConfig{TenantID: configTenantID, WebhookURL: cfg.WebhookURL, Secret: cfg.Secret}
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
	thirdPartyPlugin := im.NewRemoteGatewayPlugin("thirdparty", deviceService, st.Users, st.System)
	if err := imAdapter.RegisterPlugin(thirdPartyPlugin); err != nil {
		log.Printf("[bootstrap] failed to register thirdparty plugin: %v", err)
	}
	gateway.RegisterIMGatewayPlugin(qqRemotePlugin)
	gateway.RegisterIMGatewayPlugin(telegramPlugin)
	gateway.RegisterIMGatewayPlugin(weixinPlugin)
	gateway.RegisterIMGatewayPlugin(lansengerPlugin)
	gateway.RegisterIMGatewayPlugin(thirdPartyPlugin)
	deviceGateway := im.NewPersistentDeviceGateway(thirdPartyPlugin, st.System)
	// Hardware bearer credentials are owned by a GUI machine identity. Keep the
	// minimal tenant/user/machine records in the same recovery snapshot so a Hub
	// SQLite reinstall does not force the already-installed GUI to re-enroll
	// before it can control its recovered hardware.
	deviceGateway.SetCredentialIdentityRepositories(st.Tenants, st.Users, st.Machines)
	credentialRestorer := sqlite.NewDeviceCredentialIdentityRestorer(provider)
	deviceGateway.SetCredentialIdentityRestorer(credentialRestorer)
	deviceGateway.SetCredentialSnapshotRestorer(credentialRestorer)
	deviceGateway.SetCredentialBackup(centerService.BackupDeviceCredentials)
	centerService.SetDeviceCredentialRecovery(func(ctx context.Context) {
		recoveryResolved, err := deviceGateway.RestoreMissingCredentialsResult(ctx, centerService.RestoreDeviceCredentials)
		if err != nil {
			// Recovery is best-effort. A first install has no snapshot and an
			// unavailable Hub Center must never block the local Hub from starting.
			log.Printf("[bootstrap] hardware credential recovery unavailable: %v", err)
		}
		if !recoveryResolved {
			// The local store is empty because this may be a reinstall. Never
			// overwrite the remote binding backup with that empty state after a
			// transient Hub Center error.
			return
		}
		// A successful (re-)registration also publishes the current local
		// snapshot. This seeds existing deployments when they upgrade, while a
		// freshly reinstalled Hub first restores the snapshot above and then
		// writes it back under its renewed Hub credential.
		if snapshot, err := deviceGateway.ExportPersistedCredentials(); err != nil {
			log.Printf("[bootstrap] hardware credential backup snapshot failed: %v", err)
		} else if err := centerService.BackupDeviceCredentials(ctx, snapshot); err != nil {
			log.Printf("[bootstrap] hardware credential backup unavailable: %v", err)
		}
	})
	// StartBackgroundSync can complete an automatic registration before the
	// device gateway is constructed. Run recovery once after wiring the hook so
	// a freshly reinstalled Hub cannot miss its only startup restore attempt.
	centerService.RecoverDeviceCredentialsNow(context.Background())
	deviceGateway.SetMachineMessageSender(deviceService)
	deviceGateway.SetVoicePairTranscriber(httpapi.TranscribeHardwarePairingWAV)
	httpapi.SetHardwareMeetingResultNotifier(deviceGateway)
	deviceGateway.SetMeetingRecordingHandler(httpapi.HardwareMeetingRecordingsHandler(deviceGateway))
	meetingTranscript, meetingMinutes := httpapi.HardwareMeetingRecordingWorkerAvailability()
	deviceGateway.SetMeetingRecordingModes(meetingTranscript, meetingMinutes)
	gateway.SetDeviceGateway(deviceGateway)

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
	publicBaseURL := centerService.GetPublicBaseURL(context.Background())
	if publicBaseURL != "" {
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
	if publicBaseURL != "" {
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
	if publicBaseURL != "" {
		dingtalkPlugin.SetPublicBaseURL(publicBaseURL)
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
	tenantNativeIMRuntimes := newTenantNativeIMRuntimeManager(imAdapter, st.System, st.Users, mailer, broadcaster, publicBaseURL)
	tenantNativeIMRuntimes.ReloadAll(context.Background(), st.Tenants)

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
	startVerifiedPhoneRouteBackfillLoop(identityService)

	// Register session event listener 闂?routes through IM Adapter when available,
	// falls back to legacy notifier path.
	sessionService.RegisterListener(feishuNotifier.HandleEvent)

	chatStore, err := chat.NewStore(provider.Write)
	if err != nil {
		return nil, fmt.Errorf("chat store: %w", err)
	}

	// Push dispatcher: look up tokens from chat store.
	pushDispatcher := chatpush.NewTenantDispatcher(func(tenantID, userID string) ([]chatpush.TokenInfo, error) {
		tokens, err := chatStore.GetPushTokensForTenant(tenantID, userID)
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

	securityStore := security.NewSecurityStore(provider.Write)
	if err := securityStore.InitSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("security schema: %w", err)
	}
	if err := securityStore.InitRootGroup(context.Background()); err != nil {
		return nil, fmt.Errorf("security root group: %w", err)
	}
	securitySvc := security.NewSecurityService(securityStore, st.System, st.AdminAudit, st.Users)
	smartRouteChecker.SetSecurityPolicyProvider(&smartRouteSecurityPolicyAdapter{svc: securitySvc})

	// Inject SecurityProvider into ws.Gateway for heartbeat policy delivery.
	gateway.SecurityProvider = securitySvc
	gateway.ConfigProvider = heartbeatConfigProvider{settings: st.System}

	// Wire OutboundInterceptor into im.Adapter for file/image outbound checks.
	outboundInterceptor := im.NewOutboundInterceptor(securitySvc, nil)
	imAdapter.SetOutboundInterceptor(outboundInterceptor)

	// Wire ContentAuditor into im.Adapter for outbound content compliance checks.
	contentAuditLogStore := im.NewSQLiteAuditLogStore(provider.Write)
	contentAuditConfigProvider := func(ctx context.Context) *im.ContentAuditDynamicConfig {
		tenantID := im.TenantIDFromContext(ctx)
		system := scopedSystemSettingsForTenant(tenantID, st.System)
		raw, err := system.Get(context.Background(), "content_audit_config")
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

	// --- MaClaw Official LLM Provider module ---
	hubCenterURL := strings.TrimSpace(cfg.Center.BaseURL)
	hubID := ""
	hubSecret := ""
	if regState, err := centerService.Status(context.Background()); err == nil && regState != nil {
		hubID = regState.HubID
		// The explicit Hub configuration is the routing source of truth.  The
		// registration state is replicated/runtime data and can temporarily point
		// at an old HA member after a topology change; letting it override the
		// configured URL sends LLM traffic to a node that may not own the current
		// tenant binding.  Only use it to recover a missing configuration.
		if hubCenterURL == "" {
			hubCenterURL = firstNonEmpty(regState.ActiveBaseURL, regState.BaseURL)
		}
	}
	// hub_secret is stored in center_registration system setting
	hubSecret = loadCenterHubSecret(context.Background(), st.System)
	maclawMod := llmservice.InitMaClawModule(hubCenterURL, hubID, hubSecret, func() []string {
		tenants, _ := st.Tenants.List(context.Background())
		ids := make([]string, 0, len(tenants))
		for _, t := range tenants {
			ids = append(ids, t.ID)
		}
		return ids
	})
	if maclawMod != nil {
		// Keep every configured or registered remote HubCenter node available to
		// the LLM path. Hub and HubCenter may be deployed independently.
		candidates := append([]string{cfg.Center.BaseURL}, cfg.Center.BaseURLs...)
		if strings.TrimSpace(cfg.Center.BaseURL) == "" && len(cfg.Center.BaseURLs) == 0 {
			if regState, err := centerService.Status(context.Background()); err == nil && regState != nil {
				candidates = append(candidates, regState.ActiveBaseURL)
				candidates = append(candidates, regState.BaseURLs...)
			}
		}
		maclawMod.Client.SetHubCenterCandidates(candidates)
		refreshHubCenterCandidates := func() {
			// Probe remote cluster members proactively. This is intentionally
			// independent of deployment topology: Hub may run on a different host.
			urls := append([]string{cfg.Center.BaseURL}, cfg.Center.BaseURLs...)
			if strings.TrimSpace(cfg.Center.BaseURL) == "" && len(cfg.Center.BaseURLs) == 0 {
				if state, err := centerService.Status(context.Background()); err == nil && state != nil {
					urls = append(urls, state.ActiveBaseURL)
					urls = append(urls, state.BaseURLs...)
				}
			}
			current := maclawMod.Client.CurrentHubCenterURL()
			ordered := remote.SelectBestCenter(context.Background(), &http.Client{Timeout: 4 * time.Second}, urls, current)
			if len(ordered) == 0 {
				return
			}
			maclawMod.Client.SetHubCenterCandidates(ordered)
			if ordered[0] != current {
				log.Printf("[maclaw-provider] HubCenter health probe selected %s (previous=%s)", ordered[0], current)
				maclawMod.Client.SetBoundURL(ordered[0])
			}
		}
		refreshHubCenterCandidates()
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				refreshHubCenterCandidates()
			}
		}()
		maclawMod.Client.SetRefreshCredentials(func() (string, string) {
			hubID := ""
			if regState, err := centerService.Status(context.Background()); err == nil && regState != nil {
				hubID = strings.TrimSpace(regState.HubID)
			}
			return hubID, loadCenterHubSecret(context.Background(), st.System)
		})
		httpapi.SetMaClawModule(maclawMod)
		// Attach official client so IM system-free can forward when no local provider.
		systemLLMResolver.MaClawClient = maclawMod.Client
	}
	ensureLLMRegistryBuiltinsForAllTenants(context.Background(), st.System, st.Tenants)

	// Mark Hub as ready for traffic. This must be called AFTER:
	// - MaClawModule initialized (LLM provider client ready)
	// - LLM registry builtins ensured (provider registry loadable)
	// The /healthz/ready endpoint returns 200 only after this call,
	// allowing nginx to route traffic only to fully initialized instances.
	httpapi.MarkHubReady()

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
		securitySvc,
		cfg,
		configPath,
		EnsureSelfSignedCert,
		cfg.PWA.StaticDir,
		cfg.PWA.RoutePrefix,
		cfg.Bridge.Dir,
		tenantNativeIMRuntimes,
		st.KnowledgeShares,
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
		KnowledgeShares: st.KnowledgeShares,

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

func hubRuntimeDataDir(cfg *config.Config, configPath string) string {
	if cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.Database.Driver), "sqlite") {
		dsn := strings.TrimSpace(cfg.Database.DSN)
		if idx := strings.IndexByte(dsn, '?'); idx >= 0 {
			dsn = dsn[:idx]
		}
		dsn = strings.TrimPrefix(dsn, "file:")
		if dsn != "" && dsn != ":memory:" && !strings.HasPrefix(dsn, ":memory:") {
			if dir := filepath.Dir(dsn); dir != "." && dir != "" {
				return dir
			}
		}
	}
	if configPath != "" {
		return filepath.Join(filepath.Dir(configPath), "data")
	}
	return "./data"
}

// userEmailLookup adapts store.UserRepository to im.UserLookup.
type userEmailLookup struct {
	users interface {
		GetByID(ctx context.Context, id string) (*store.User, error)
	}
}

func loadGlobalHubLLMConfig(ctx context.Context, system store.SystemSettingsRepository) *im.HubLLMConfig {
	if system == nil {
		return nil
	}
	raw, err := system.Get(ctx, "hub_llm_config")
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

type smartRouteSecurityPolicyAdapter struct {
	svc *security.SecurityService
}

func (a *smartRouteSecurityPolicyAdapter) IsSmartRouteAllowedBySecurityPolicy(ctx context.Context, userID string) (bool, bool) {
	if a == nil || a.svc == nil {
		return true, false
	}
	ctx = security.WithTenant(ctx, im.TenantIDFromContext(ctx))
	enabled, err := a.svc.IsCentralizedEnabled(ctx)
	if err != nil {
		log.Printf("[SmartRouteChecker] security settings lookup error for %s: %v", userID, err)
		return false, true
	}
	if !enabled {
		return true, false
	}
	policy, err := a.svc.GetEffectivePolicyByUserID(ctx, userID)
	if err != nil {
		log.Printf("[SmartRouteChecker] security policy lookup error for %s: %v", userID, err)
		return false, true
	}
	if policy == nil {
		return false, true
	}
	return policy.SmartRouteEnabled, true
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

func ensureLLMRegistryBuiltinsForAllTenants(ctx context.Context, system store.SystemSettingsRepository, tenants store.TenantRepository) {
	llmservice.EnsureRegistryBuiltins(ctx, system)
	if tenants == nil {
		return
	}
	items, err := tenants.List(ctx)
	if err != nil {
		log.Printf("[maclaw-init] failed to list tenants for LLM registry builtins: %v", err)
		return
	}
	for _, tenant := range items {
		tenantID := strings.TrimSpace(tenant.ID)
		if tenantID == "" || tenantID == store.DefaultTenantID {
			continue
		}
		llmservice.EnsureRegistryBuiltins(ctx, scopedSystemSettingsForTenant(tenantID, system))
	}
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

type nativeIMMailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
}

type tenantNativeIMRuntimeManager struct {
	mu            sync.Mutex
	adapter       *im.Adapter
	system        store.SystemSettingsRepository
	users         store.UserRepository
	mailer        nativeIMMailer
	broadcaster   *im.NotifyBroadcaster
	publicBaseURL string
	runtimes      map[string]map[string]im.IMPlugin
}

func newTenantNativeIMRuntimeManager(adapter *im.Adapter, system store.SystemSettingsRepository, users store.UserRepository, mailer nativeIMMailer, broadcaster *im.NotifyBroadcaster, publicBaseURL string) *tenantNativeIMRuntimeManager {
	return &tenantNativeIMRuntimeManager{adapter: adapter, system: system, users: users, mailer: mailer, broadcaster: broadcaster, publicBaseURL: publicBaseURL, runtimes: make(map[string]map[string]im.IMPlugin)}
}

func (m *tenantNativeIMRuntimeManager) ReloadAll(ctx context.Context, tenants store.TenantRepository) {
	if m == nil || tenants == nil || m.adapter == nil || m.system == nil || m.users == nil {
		return
	}
	items, err := tenants.List(ctx)
	if err != nil {
		log.Printf("[bootstrap] tenant IM runtimes unavailable: list tenants failed: %v", err)
		return
	}
	activeTenants := make(map[string]struct{})
	for _, tenant := range items {
		if tenant == nil {
			continue
		}
		tenantID := strings.TrimSpace(tenant.ID)
		if tenantID == "" {
			continue
		}
		if tenantID == store.DefaultTenantID || tenant.DeletedAt != nil || !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
			m.StopTenantIMs(ctx, tenantID)
			continue
		}
		activeTenants[tenantID] = struct{}{}
		for _, platform := range []string{"qqbot", "wecom", "dingtalk"} {
			if err := m.ReloadTenantIM(ctx, tenantID, platform); err != nil {
				log.Printf("[bootstrap] failed to reload tenant IM runtime: tenant=%s platform=%s err=%v", tenantID, platform, err)
			}
		}
	}
	for _, tenantID := range m.tenantRuntimeIDs() {
		if _, ok := activeTenants[tenantID]; !ok {
			m.StopTenantIMs(ctx, tenantID)
		}
	}
}

func (m *tenantNativeIMRuntimeManager) ReloadTenantIM(ctx context.Context, tenantID, platform string) error {
	if m == nil || m.adapter == nil || m.system == nil || m.users == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	platform = strings.ToLower(strings.TrimSpace(platform))
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(ctx, tenantID, platform)
	tenantSystem := httpapi.ScopedSystemSettingsForTenant(tenantID, m.system)
	var plugin im.IMPlugin
	switch platform {
	case "qqbot":
		plugin = buildTenantQQBotPlugin(ctx, tenantSystem, m.users, m.mailer, m.broadcaster, m.publicBaseURL)
	case "wecom":
		plugin = buildTenantWeComPlugin(ctx, tenantSystem, m.users, m.mailer, m.broadcaster, m.publicBaseURL)
	case "dingtalk":
		plugin = buildTenantDingTalkPlugin(ctx, tenantSystem, m.users, m.mailer, m.broadcaster, m.publicBaseURL)
	default:
		return nil
	}
	if plugin == nil {
		return nil
	}
	return m.registerAndStartTenantPluginLocked(ctx, tenantID, platform, plugin)
}

func (m *tenantNativeIMRuntimeManager) registerAndStartTenantPluginLocked(ctx context.Context, tenantID, platform string, plugin im.IMPlugin) error {
	if err := m.adapter.RegisterTenantPlugin(tenantID, plugin); err != nil {
		return err
	}
	if err := plugin.Start(ctx); err != nil {
		removed := m.adapter.UnregisterTenantPlugin(tenantID, platform)
		if removed != nil {
			_ = removed.Stop(ctx)
		}
		return err
	}
	if m.runtimes[tenantID] == nil {
		m.runtimes[tenantID] = make(map[string]im.IMPlugin)
	}
	m.runtimes[tenantID][platform] = plugin
	return nil
}

func (m *tenantNativeIMRuntimeManager) stopLocked(ctx context.Context, tenantID, platform string) {
	removed := m.adapter.UnregisterTenantPlugin(tenantID, platform)
	if removed != nil {
		_ = removed.Stop(ctx)
	}
	if m.runtimes[tenantID] != nil {
		if existing := m.runtimes[tenantID][platform]; existing != nil {
			if existing != removed {
				_ = existing.Stop(ctx)
			}
		}
		delete(m.runtimes[tenantID], platform)
		if len(m.runtimes[tenantID]) == 0 {
			delete(m.runtimes, tenantID)
		}
	}
}

func (m *tenantNativeIMRuntimeManager) StopTenantIMs(ctx context.Context, tenantID string) {
	if m == nil || m.adapter == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, platform := range []string{"qqbot", "wecom", "dingtalk"} {
		m.stopLocked(ctx, tenantID, platform)
	}
}

func (m *tenantNativeIMRuntimeManager) tenantRuntimeIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.runtimes))
	for tenantID := range m.runtimes {
		out = append(out, tenantID)
	}
	return out
}

func buildTenantQQBotPlugin(ctx context.Context, system store.SystemSettingsRepository, users store.UserRepository, mailer nativeIMMailer, broadcaster *im.NotifyBroadcaster, publicBaseURL string) im.IMPlugin {
	if cfg := loadTenantQQBotConfig(ctx, system); !cfg.Enabled || cfg.AppID == "" || cfg.AppSecret == "" {
		return nil
	}
	plugin := qqbot.New(func() qqbot.Config { return loadTenantQQBotConfig(context.Background(), system) }, users, system, mailer)
	if publicBaseURL != "" {
		plugin.SetPublicBaseURL(publicBaseURL)
	}
	plugin.SetBroadcaster(broadcaster)
	return plugin
}

func buildTenantWeComPlugin(ctx context.Context, system store.SystemSettingsRepository, users store.UserRepository, mailer nativeIMMailer, broadcaster *im.NotifyBroadcaster, publicBaseURL string) im.IMPlugin {
	if cfg := loadTenantWeComConfig(ctx, system); !cfg.Enabled || cfg.BotID == "" || cfg.Secret == "" {
		return nil
	}
	plugin := wecom.New(func() wecom.Config { return loadTenantWeComConfig(context.Background(), system) }, users, system, mailer)
	if publicBaseURL != "" {
		plugin.SetPublicBaseURL(publicBaseURL)
	}
	plugin.SetBroadcaster(broadcaster)
	return plugin
}

func buildTenantDingTalkPlugin(ctx context.Context, system store.SystemSettingsRepository, users store.UserRepository, mailer nativeIMMailer, broadcaster *im.NotifyBroadcaster, publicBaseURL string) im.IMPlugin {
	if cfg := loadTenantDingTalkConfig(ctx, system); !cfg.Enabled || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil
	}
	plugin := dingtalk.New(func() dingtalk.Config { return loadTenantDingTalkConfig(context.Background(), system) }, users, system, mailer)
	if publicBaseURL != "" {
		plugin.SetPublicBaseURL(publicBaseURL)
	}
	plugin.SetBroadcaster(broadcaster)
	return plugin
}

func loadTenantQQBotConfig(ctx context.Context, system store.SystemSettingsRepository) qqbot.Config {
	raw, err := system.Get(ctx, "qqbot_config")
	if err != nil || raw == "" {
		return qqbot.Config{}
	}
	var cfg qqbot.Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return qqbot.Config{}
	}
	return cfg
}

func loadTenantWeComConfig(ctx context.Context, system store.SystemSettingsRepository) wecom.Config {
	raw, err := system.Get(ctx, "wecom_config")
	if err != nil || raw == "" {
		return wecom.Config{}
	}
	var cfg wecom.Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return wecom.Config{}
	}
	return cfg
}

func loadTenantDingTalkConfig(ctx context.Context, system store.SystemSettingsRepository) dingtalk.Config {
	raw, err := system.Get(ctx, "dingtalk_config")
	if err != nil || raw == "" {
		return dingtalk.Config{}
	}
	var cfg dingtalk.Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return dingtalk.Config{}
	}
	return cfg
}

func startVerifiedPhoneRouteBackfillLoop(identity *auth.IdentityService) {
	if identity == nil {
		return
	}
	go func() {
		delays := []time.Duration{0, 30 * time.Second, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute}
		for attempt, delay := range delays {
			if delay > 0 {
				timer := time.NewTimer(delay)
				<-timer.C
			}
			count, err := identity.SyncVerifiedPhoneRoutes(context.Background())
			if err != nil {
				log.Printf("[bootstrap] verified phone route backfill attempt %d/%d failed after syncing %d route(s): %v", attempt+1, len(delays), count, err)
				continue
			}
			if count > 0 {
				log.Printf("[bootstrap] synced %d verified phone route(s) to hub center", count)
			}
			break
		}
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			count, err := identity.SyncVerifiedPhoneRoutes(context.Background())
			if err != nil {
				log.Printf("[bootstrap] verified phone route reconciliation failed after syncing %d route(s): %v", count, err)
				continue
			}
			if count > 0 {
				log.Printf("[bootstrap] reconciled %d verified phone route(s) to hub center", count)
			}
		}
	}()
}

// startTenantAdminRouteReconciliationLoop repairs routes from tenant
// administrators created before their email routes were synced to HubCenter.
// SyncUserRoute is idempotent, so periodically replaying all active tenant
// admin routes also closes any gap caused by temporary HubCenter outages.
func startTenantAdminRouteReconciliationLoop(admins *auth.AdminService, centerSvc *center.Service) {
	if admins == nil || centerSvc == nil {
		return
	}
	go func() {
		// HubCenter registration can still be in progress when background tasks
		// start. Retry promptly until a route sync succeeds, then fall back to a
		// low-frequency repair sweep to avoid needlessly replaying every admin.
		reconcile := func() (retrySoon bool) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if !tenantAdminRouteSyncReady(ctx, centerSvc) {
				return true
			}
			synced, failed, err := reconcileTenantAdminRoutes(ctx, admins, centerSvc)
			if err != nil {
				log.Printf("[bootstrap] tenant admin HubCenter route reconciliation failed: %v", err)
				return true
			}
			if failed > 0 {
				log.Printf("[bootstrap] tenant admin HubCenter route reconciliation synced %d route(s), %d failed", synced, failed)
				return true
			}
			if synced > 0 {
				log.Printf("[bootstrap] reconciled %d tenant admin HubCenter route(s)", synced)
			}
			return false
		}

		const repairInterval = 30 * time.Minute
		// Startup registration normally settles within a few heartbeat attempts.
		// Keep the recovery path responsive without making sustained failures
		// generate a high-rate request stream.
		retryInterval := 15 * time.Second
		retrySoon := reconcile()
		for {
			interval := repairInterval
			if retrySoon {
				interval = retryInterval
			}
			time.Sleep(interval)
			retrySoon = reconcile()
			if retrySoon && retryInterval < time.Minute {
				retryInterval *= 2
				if retryInterval > time.Minute {
					retryInterval = time.Minute
				}
			} else if !retrySoon {
				retryInterval = 15 * time.Second
			}
		}
	}()
}

func tenantAdminRouteSyncReady(ctx context.Context, centerSvc *center.Service) bool {
	if centerSvc == nil {
		return false
	}
	status, err := centerSvc.Status(ctx)
	if err != nil || status == nil || strings.TrimSpace(status.HubID) == "" {
		return false
	}
	// Pending and disabled registrations reject user-link writes in HubCenter.
	// Treating them as ready only creates predictable failed requests and noisy
	// logs; a confirmed registration will be picked up by the retry loop.
	return status.Registered && !status.Disabled
}

func reconcileTenantAdminRoutes(ctx context.Context, admins *auth.AdminService, centerSvc interface {
	SyncUserRoute(context.Context, string, ...string) error
}) (synced, failed int, err error) {
	if admins == nil || centerSvc == nil {
		return 0, 0, nil
	}
	items, err := admins.ListAllTenantAdmins(ctx)
	if err != nil {
		return 0, 0, err
	}
	const maxConcurrentRouteSyncs = 8
	sem := make(chan struct{}, maxConcurrentRouteSyncs)
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	seen := make(map[string]struct{})
	for _, admin := range items {
		if admin == nil || !strings.EqualFold(strings.TrimSpace(admin.Status), "active") {
			continue
		}
		tenantID := strings.TrimSpace(admin.TenantID)
		email := strings.TrimSpace(strings.ToLower(admin.Email))
		if tenantID == "" || email == "" {
			continue
		}
		key := tenantID + "\x00" + email
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sem <- struct{}{}
		wg.Add(1)
		go func(tenantID, email string) {
			defer wg.Done()
			defer func() { <-sem }()
			if syncErr := centerSvc.SyncUserRoute(ctx, email, tenantID); syncErr != nil {
				resultMu.Lock()
				failed++
				resultMu.Unlock()
				log.Printf("[bootstrap] tenant admin HubCenter route reconciliation failed for tenant=%s email=%s: %v", tenantID, email, syncErr)
				return
			}
			resultMu.Lock()
			synced++
			resultMu.Unlock()
		}(tenantID, email)
	}
	wg.Wait()
	return synced, failed, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func loadCenterHubSecret(ctx context.Context, system store.SystemSettingsRepository) string {
	raw, err := system.Get(ctx, "center_registration")
	if err != nil || raw == "" {
		return ""
	}
	var regRec struct {
		HubSecret string `json:"hub_secret"`
	}
	if json.Unmarshal([]byte(raw), &regRec) != nil {
		return ""
	}
	return strings.TrimSpace(regRec.HubSecret)
}
