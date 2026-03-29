package app

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
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

	// Skill store: derive directory from database DSN path.
	skillStoreDir := filepath.Join(filepath.Dir(cfg.Database.DSN), "skills")
	skillStore := skill.NewSkillStore(skillStoreDir)

	// Gossip snapshot cache: static gzip file for zero-CPU client polling.
	gossipCachePath := filepath.Join(filepath.Dir(cfg.Database.DSN), "gossip_cache.json.gz")
	gossipCache := httpapi.NewGossipCache(st.Gossip, gossipCachePath)
	gossipCache.EnsureExists(context.Background())

	// SkillMarket: data models, services, processor.
	dataDir := filepath.Dir(cfg.Database.DSN)
	smStore, err := skillmarket.NewStore(provider.Write, provider.Read)
	if err != nil {
		return nil, err
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
	// 启动时全量重建 FTS 搜索索引
	if err := searchSvc.RebuildIndex(context.Background()); err != nil {
		log.Printf("[hubcenter] rebuild search index: %v", err)
	}
	// 将 searchSvc 注入 processor，发布时增量更新索引
	processor.SetSearchService(searchSvc)
	leaderboardSvc := skillmarket.NewLeaderboardService(skillStore)

	// API Key pool: 使用 RSA 私钥的原始字节作为加密密钥种子
	apiKeySvc, err := skillmarket.NewAPIKeyPoolService(smStore, rsaPrivKey.D.Bytes())
	if err != nil {
		return nil, err
	}

	// 通知服务
	notifSvc, err := skillmarket.NewNotificationService(smStore, mailer)
	if err != nil {
		return nil, err
	}

	// 退款服务
	refundSvc := skillmarket.NewRefundService(smStore, creditsSvc, mailer)

	// 频率限制
	tierSvc := skillmarket.NewTierService(smStore)
	rateLimiter := skillmarket.NewRateLimiter(smStore, tierSvc)

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
		RSAPrivKey:     rsaPrivKey,
		PendingDir:     pendingDir,
		DataDir:        dataDir,
	})

	// 启动异步处理器后台 goroutine
	go processor.Run(context.Background())

	// 启动通知服务后台 goroutine（与试用期到期扫描复用）
	go func() {
		ctx := context.Background()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			<-ticker.C
			_ = notifSvc.ProcessPendingNotifications(ctx)
			trialMgr.ProcessExpiredTrials(ctx)
		}
	}()

	router := httpapi.NewRouter(adminService, hubService, entryService, mailer, skillStore, st.Gossip, gossipCache, smHandlers, st.System)

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
