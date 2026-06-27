package app

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

func Bootstrap(cfg *config.Config) (*App, error) {
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
	})
	if err != nil {
		return nil, err
	}
	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{Config: cfg, Provider: provider, ctx: appCtx, cancel: appCancel}
	bootOK := false
	defer func() {
		if !bootOK {
			appCancel()
			_ = provider.Close()
		}
	}()

	if err := sqlite.RunMigrations(provider.Write); err != nil {
		return nil, err
	}
	log.Printf("[hubcenter] sqlite migrations complete for %s", cfg.Database.DSN)

	st := sqlite.NewStore(provider)
	failureRecorder := diagnostics.NewFailureEventRecorder(st.FailureLogs)
	dataDir := filepath.Dir(cfg.Database.DSN)
	haConfigSvc := ha.NewConfigService(cfg.HA, st.System)
	effectiveHA, err := haConfigSvc.CurrentConfig(context.Background())
	if err != nil {
		return nil, err
	}
	cfg.HA = effectiveHA

	var haSvc *ha.Service
	if cfg.HA.Enabled {
		keyMaterial, err := ha.EnsureNodeKeyPair(dataDir, &cfg.HA)
		if err != nil {
			return nil, err
		}
		peers := make([]ha.StaticPeer, 0, len(cfg.HA.Peers))
		for _, peer := range cfg.HA.Peers {
			if !peer.Enabled || strings.TrimSpace(peer.NodeID) == "" || strings.TrimSpace(peer.BaseURL) == "" {
				continue
			}
			peers = append(peers, ha.StaticPeer{
				NodeID:       peer.NodeID,
				NodeName:     peer.Name,
				BaseURL:      peer.BaseURL,
				PublicKeyPEM: peer.PublicKeyPEM,
			})
		}
		haSvc = ha.NewService(cfg.HA.NodeID, cfg.HA.NodeName, cfg.HA.AdvertiseURL, cfg.HA.ClusterSecret, peers)
		haSvc.SetNodeKeyMaterial(keyMaterial)
		haSvc.AttachStore(st)
		haSvc.SetPushDebounceInterval(time.Duration(cfg.HA.PushDebounceSeconds) * time.Second)
		haSvc.SetHeartbeatSyncMinInterval(time.Duration(cfg.HA.HeartbeatSyncMinIntervalSeconds) * time.Second)
		haSvc.SetFailureEventRecorder(failureRecorder)
	}

	systemSettings := st.System
	gossipRepo := st.Gossip
	if haSvc != nil {
		systemSettings = &haSystemSettings{inner: st.System, sync: haSvc}
		gossipRepo = &haGossipRepo{inner: st.Gossip, sync: haSvc}
	}

	adminService := auth.NewAdminService(st.Admins, systemSettings, st.AdminAudit)
	mailer := mail.New(*cfg, systemSettings)
	hubService := hubs.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, systemSettings, mailer, cfg.Server.PublicBaseURL)
	hubService.SetFailureEventRecorder(failureRecorder)
	hubService.SetInvitationCodeRoutes(st.InvitationCodeRoutes)
	entryService := entry.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, systemSettings)
	entryService.SetInvitationCodeRoutes(st.InvitationCodeRoutes)
	hubService.SetRouteSnapshotRefresher(entryService)
	if err := entryService.Rebuild(context.Background()); err != nil {
		return nil, err
	}
	routeStats := entryService.SnapshotStats()
	log.Printf("[hubcenter] route snapshot rebuilt: defaults=%d email_routes=%d domain_routes=%d public_hubs=%d blocked_emails=%d blocked_ips=%d", routeStats.DefaultHubIDs, routeStats.EmailRoutes, routeStats.DomainRoutes, routeStats.PublicHubs, routeStats.BlockedEmails, routeStats.BlockedIPs)

	skillStoreDir := filepath.Join(dataDir, "skills")
	skillStore := skill.NewSkillStore(skillStoreDir)

	gossipCachePath := filepath.Join(dataDir, "gossip_cache.json.gz")
	gossipCache := httpapi.NewGossipCache(st.Gossip, gossipCachePath)
	gossipCache.EnsureExists(context.Background())

	smStore, err := skillmarket.NewStore(provider.Write, provider.Read)
	if err != nil {
		return nil, err
	}
	if haSvc != nil {
		skillStore.SetSyncRecorder(haSvc)
		smStore.SetSyncRecorder(haSvc)
		haSvc.AttachSkillStore(skillStore)
		haSvc.AttachSkillMarket(smStore)
	}
	userSvc := skillmarket.NewUserService(smStore, mailer)
	creditsSvc := skillmarket.NewCreditsService(smStore)
	ratingSvc := skillmarket.NewRatingService(smStore)
	trialMgr := skillmarket.NewTrialManager(smStore, skillStore, ratingSvc)
	versionMgr := skillmarket.NewVersionManager(smStore)
	pendingDir := filepath.Join(dataDir, "sm_pending")
	sandboxDir := filepath.Join(dataDir, "sm_sandbox")
	processor := skillmarket.NewProcessor(pendingDir, sandboxDir, smStore, skillStore, mailer, trialMgr, versionMgr)

	rsaPrivKey, err := skillmarket.EnsureRSAKeyPair(dataDir)
	if err != nil {
		return nil, err
	}

	searchSvc, err := skillmarket.NewSearchService(smStore, skillStore)
	if err != nil {
		return nil, err
	}
	if err := searchSvc.RebuildIndex(context.Background()); err != nil {
		log.Printf("[hubcenter] rebuild search index: %v", err)
	}
	processor.SetSearchService(searchSvc)
	leaderboardSvc := skillmarket.NewLeaderboardService(skillStore)

	apiKeySvc, err := skillmarket.NewAPIKeyPoolService(smStore, rsaPrivKey.D.Bytes())
	if err != nil {
		return nil, err
	}

	notifSvc, err := skillmarket.NewNotificationService(smStore, mailer)
	if err != nil {
		return nil, err
	}

	refundSvc := skillmarket.NewRefundService(smStore, creditsSvc, mailer)

	tierSvc := skillmarket.NewTierService(smStore)
	rateLimiter := skillmarket.NewRateLimiter(smStore, tierSvc)

	authSvc := skillmarket.NewAuthService(smStore, mailer, cfg.Server.PublicBaseURL)
	authSvc.SetSessionSigningSecret(cfg.HA.ClusterSecret)
	authSvc.SetPublicBaseURLProvider(hubService)

	smHandlers := httpapi.NewSkillMarketHandlers(httpapi.SkillMarketConfig{
		Store:          smStore,
		SkillStore:     skillStore,
		UserSvc:        userSvc,
		CreditsSvc:     creditsSvc,
		Processor:      processor,
		RatingSvc:      ratingSvc,
		TrialMgr:       trialMgr,
		SearchSvc:      searchSvc,
		LeaderboardSvc: leaderboardSvc,
		APIKeySvc:      apiKeySvc,
		RefundSvc:      refundSvc,
		RateLimiter:    rateLimiter,
		AuthSvc:        authSvc,
		Settings:       systemSettings,
		RSAPrivKey:     rsaPrivKey,
		PendingDir:     pendingDir,
		DataDir:        dataDir,
	})

	app.goBackground(processor.Run)

	app.goBackground(func(ctx context.Context) {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			_ = notifSvc.ProcessPendingNotifications(ctx)
			trialMgr.ProcessExpiredTrials(ctx)
			_ = smStore.DeleteExpiredAuthTokens(ctx)
			_ = smStore.DeleteExpiredSessions(ctx)
		}
	})

	if haSvc != nil {
		hubService.SetSyncRecorder(haSvc)
		haSvc.SetRouteSnapshotRefresher(entryService)
		app.goBackground(func(ctx context.Context) {
			seedInitialHASnapshots(ctx, haSvc, entryService, st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.System, st.Gossip, st.News, skillStore, smStore)
		})
		app.goBackground(func(ctx context.Context) {
			runHAHistoryPruner(ctx, haSvc, cfg.HA.HistoryRetentionDays, cfg.HA.HistoryMaxRetainedOps, cfg.HA.HistoryPruneIntervalMinutes, cfg.HA.HistoryPruneBatchSize)
		})
		app.goBackground(ha.NewProber(haSvc, time.Duration(cfg.HA.SyncIntervalSeconds)*time.Second).Run)
		app.goBackground(ha.NewSyncer(haSvc, time.Duration(cfg.HA.SyncIntervalSeconds)*time.Second, cfg.HA.PullBatchSize).Run)
	}

	// --- LLM Service Module ---
	nodeID := ""
	if cfg.HA.Enabled && cfg.HA.NodeID != "" {
		nodeID = cfg.HA.NodeID
	} else {
		nodeID = "single"
	}
	_ = InitLLMModule(provider, systemSettings, nodeID, entryService, haSvc)

	router := httpapi.NewRouter(adminService, hubService, entryService, mailer, skillStore, st.FailureLogs, gossipRepo, gossipCache, smHandlers, systemSettings, st.News, haConfigSvc, haSvc, st.HubUserUsage)

	app.Store = st
	app.AdminService = adminService
	app.HubService = hubService
	app.EntryService = entryService
	app.Mailer = mailer
	app.HTTPHandler = router
	bootOK = true
	return app, nil
}

func runHAHistoryPruner(ctx context.Context, haSvc *ha.Service, retentionDays float64, maxRetainedOps, intervalMinutes, batchSize int) {
	if haSvc == nil {
		return
	}
	if retentionDays <= 0 {
		retentionDays = 0.5
	}
	if maxRetainedOps <= 0 {
		maxRetainedOps = 50000
	}
	if intervalMinutes <= 0 {
		intervalMinutes = 10
	}
	if batchSize <= 0 {
		batchSize = 20000
	}
	retention := time.Duration(retentionDays * float64(24*time.Hour))
	prune := func() {
		result, err := haSvc.PruneHistory(ctx, retention, int64(maxRetainedOps), int64(batchSize))
		if err != nil {
			log.Printf("[hubcenter][ha] prune history: %v", err)
			return
		}
		if result != nil && (result.DeletedOps > 0 || result.DeletedAppliedOps > 0) {
			log.Printf("[hubcenter][ha] pruned history: ops=%d applied_ops=%d remaining_ops=%d max_seq=%d", result.DeletedOps, result.DeletedAppliedOps, result.RemainingOps, result.MaxSeq)
		}
	}
	prune()
	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}
