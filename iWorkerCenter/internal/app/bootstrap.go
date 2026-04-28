package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	a2amodule "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/a2a"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/adminauth"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/agentruntime"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/capabilities"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	colleagueHandler "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/handler"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	colleagueService "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/compute"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/delivery"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/diworkerauth"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/executive"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/experience"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/goalwatch"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/imconfig"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/memories"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/modelrouting"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/recommend"
	roleHandler "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/handler"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	roleService "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/security"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/workermemory"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/workflow"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
	"gopkg.in/yaml.v3"
)

// Center holds all initialized services and the HTTP mux.
type Center struct {
	DB              *db.Provider
	Mux             *http.ServeMux
	Roles           *roleService.RoleService
	Colleagues      *colleagueService.ColleagueService
	AuditRepo       *audit.Repo
	SecurityChecker *security.Checker
	Deduplicator    *experience.Deduplicator
	Auth            *adminauth.Handler
	TenantService   *tenant.TenantService
	WorkerMemory    *corememory.Store
	A2A             *a2amodule.Service
	GoalWatch       *goalwatch.Service
	AgentRuntime    *agentruntime.Service
	GoalMonitor     *goalwatch.Monitor
}

// Bootstrap initializes the database, runs migrations, wires all modules,
// and returns a ready-to-use Center.
func Bootstrap() (*Center, error) {
	dsn, err := defaultDSN()
	if err != nil {
		return nil, fmt.Errorf("resolve dsn: %w", err)
	}

	provider, err := db.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Migrate(provider.Write); err != nil {
		_ = provider.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// --- wire roles module ---
	rRepo := roleRepo.New(provider.Write, provider.Read)
	rSvc := roleService.New(rRepo)
	rHandler := roleHandler.New(rSvc)

	// --- wire colleagues module (depends on roles) ---
	colRepo := colleagueRepo.New(provider.Write, provider.Read)
	colSvc := colleagueService.New(colRepo, rRepo, rSvc)
	colHandler := colleagueHandler.New(colSvc, rRepo, rSvc)

	// --- wire memories module ---
	memHandler := memories.NewHandler(provider.Write, provider.Read)

	// --- wire iWorker-owned memory module ---
	workerMemoryStore, err := corememory.NewStore(defaultWorkerMemoryStorePath())
	if err != nil {
		_ = provider.Close()
		return nil, fmt.Errorf("init worker memory store: %w", err)
	}
	workerMemHandler := workermemory.NewHandler(workerMemoryStore)

	// --- wire capabilities module ---
	capHandler := capabilities.NewHandler(provider.Write, provider.Read)

	// --- wire audit module ---
	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	auditHandler := audit.NewHandler(auditRepo)

	// --- wire collaboration module ---
	collabRepo := collaboration.NewRepo(provider.Write, provider.Read)
	collabSvc := collaboration.NewService(collabRepo, colRepo)
	collabHandler := collaboration.NewHandler(collabSvc, auditRepo)

	// --- wire a2a collaboration protocol module ---
	a2aRepo := a2amodule.NewRepo(provider.Write, provider.Read)
	a2aSvc := a2amodule.NewService(a2aRepo)
	a2aHandler := a2amodule.NewHandler(a2aSvc)

	// --- wire iWorker multi-agent runtime module ---
	agentRuntimeRepo := agentruntime.NewRepo(provider.Write, provider.Read)
	agentRuntimeSvc := agentruntime.NewService(agentRuntimeRepo)
	agentRuntimeHandler := agentruntime.NewHandler(agentRuntimeSvc)

	// --- wire workflow module (depends on collaboration + colleagues) ---
	wfRepo := workflow.NewRepo(provider.Write, provider.Read)
	wfSvc := workflow.NewService(wfRepo, provider, collabRepo, colRepo)
	wfHandler := workflow.NewHandler(wfSvc)

	// --- wire delivery module ---
	deliveryHandler := delivery.NewHandler(provider.Write, provider.Read)

	// --- wire recommend module ---
	recHandler := recommend.NewHandler(colRepo, rRepo)

	// --- wire executive module ---
	execHandler := executive.NewHandler(provider.Read, auditRepo)
	execHandler.SetWriteDB(provider.Write)
	execHandler.SetWorkflowService(wfSvc)

	// --- wire security module ---
	secRepo := security.NewRepo(provider.Write, provider.Read)
	secChecker := security.NewChecker(secRepo)
	secHandler := security.NewHandler(secRepo, secChecker)

	// --- wire security group module ---
	secGroupRepo := security.NewGroupRepo(provider.Write, provider.Read)
	secGroupHandler := security.NewGroupHandler(secGroupRepo)

	// --- wire model routing module ---
	mrHandler := modelrouting.NewHandler(provider.Write, provider.Read)

	// --- wire experience dedup ---
	dedup := experience.NewDeduplicator(provider.Write, provider.Read)

	// --- wire IM config ---
	imcHandler := imconfig.NewHandler()

	// --- wire admin auth ---
	authHandler := adminauth.NewHandler(provider.Write, provider.Read)

	// --- wire diworker auth ---
	dwAuthHandler := diworkerauth.NewHandler(provider.Write, provider.Read)

	// --- wire tenant module ---
	cloudCfg := loadCloudConfig()
	cloudClient := tenant.NewCloudClient(cloudCfg)
	tenantRepo := tenant.NewTenantRepo(provider.Write, provider.Read)
	nonceRepo := tenant.NewNonceRepo(provider.Write)
	var pubKeyCache *tenant.PublicKeyCache
	if cloudCfg.BaseURL != "" {
		pubKeyCache = tenant.NewPublicKeyCache(cloudCfg.PublicKeyCacheHours, cloudClient.FetchPublicKey)
	}
	tenantSvc := tenant.NewTenantService(tenantRepo, nonceRepo, provider.Write, provider.Write, cloudClient, pubKeyCache)
	tenantHandler := tenant.NewHandler(tenantSvc)

	// --- wire goal watchdog / push module ---
	goalWatchSvc := goalwatch.NewService(collabRepo, goalwatch.Config{})
	goalWatchSvc.SetAgentRuntime(agentRuntimeSvc)
	goalWatchHandler := goalwatch.NewHandler(goalWatchSvc)
	goalMonitor := goalwatch.NewMonitor(goalWatchSvc, tenantSvc)
	goalWatchHandler.SetMonitor(goalMonitor)
	goalMonitor.Start()

	// Start nonce cleanup goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			_ = nonceRepo.Cleanup(context.Background())
		}
	}()

	// Warmup public key cache
	if pubKeyCache != nil {
		go pubKeyCache.Warmup(context.Background())
	}

	// --- wire compute module ---
	computeSyncMgr, _, _, computeHandler := wireCompute(cloudCfg, tenantSvc)

	// Start compute sync background loop (only if cloud URL is configured)
	if computeSyncMgr != nil && computeSyncMgr.IsConfigured() {
		computeSyncMgr.Start()
	}

	// --- build mux ---
	mux := http.NewServeMux()

	// health
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// register module routes
	rHandler.RegisterAdminRoutes(mux)
	rHandler.RegisterClientRoutes(mux)
	colHandler.RegisterAdminRoutes(mux)
	colHandler.RegisterClientRoutes(mux)
	memHandler.RegisterAdminRoutes(mux)
	memHandler.RegisterClientRoutes(mux)
	workerMemHandler.RegisterClientRoutes(mux)
	capHandler.RegisterAdminRoutes(mux)
	capHandler.RegisterClientRoutes(mux)
	collabHandler.RegisterAdminRoutes(mux)
	collabHandler.RegisterClientRoutes(mux)
	goalWatchHandler.RegisterAdminRoutes(mux)
	goalWatchHandler.RegisterRuntimeRoutes(mux)
	goalWatchHandler.RegisterClientRoutes(mux)
	agentRuntimeHandler.RegisterRuntimeRoutes(mux)
	agentRuntimeHandler.RegisterClientRoutes(mux)
	a2aHandler.RegisterRuntimeRoutes(mux)
	wfHandler.RegisterAdminRoutes(mux)
	wfHandler.RegisterRuntimeRoutes(mux)
	wfHandler.RegisterClientRoutes(mux)
	auditHandler.RegisterAdminRoutes(mux)
	deliveryHandler.RegisterAdminRoutes(mux)
	deliveryHandler.RegisterClientRoutes(mux)
	recHandler.RegisterClientRoutes(mux)
	execHandler.RegisterAdminRoutes(mux)
	secHandler.RegisterAdminRoutes(mux)
	secGroupHandler.RegisterAdminRoutes(mux)
	mrHandler.RegisterAdminRoutes(mux)
	imcHandler.RegisterAdminRoutes(mux)
	authHandler.RegisterRoutes(mux)
	authHandler.RegisterAdminRoutes(mux)
	dwAuthHandler.RegisterAdminRoutes(mux)
	dwAuthHandler.RegisterAuthRoutes(mux)
	tenantHandler.RegisterRoutes(mux)
	tenantHandler.RegisterAdminRoutes(mux)
	computeHandler.RegisterAdminRoutes(mux)

	// dedup/expiry endpoint
	mux.HandleFunc("/admin/memories/dedup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		result := dedup.RunDedup()
		expired := dedup.RunExpiry(90)
		result.Expired = expired
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})

	log.Printf("[iWorkerCenter] bootstrap complete, dsn=%s", dsn)

	return &Center{
		DB:              provider,
		Mux:             mux,
		Roles:           rSvc,
		Colleagues:      colSvc,
		AuditRepo:       auditRepo,
		SecurityChecker: secChecker,
		Deduplicator:    dedup,
		Auth:            authHandler,
		TenantService:   tenantSvc,
		WorkerMemory:    workerMemoryStore,
		A2A:             a2aSvc,
		GoalWatch:       goalWatchSvc,
		AgentRuntime:    agentRuntimeSvc,
		GoalMonitor:     goalMonitor,
	}, nil
}

// Close releases all resources.
func (c *Center) Close() {
	if c.GoalMonitor != nil {
		c.GoalMonitor.Stop()
	}
	if c.WorkerMemory != nil {
		_ = c.WorkerMemory.Flush()
	}
	if c.DB != nil {
		_ = c.DB.Close()
	}
}

func defaultDSN() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".iworkercenter", "center.db"), nil
}

// loadCloudConfig reads iWorkerCloud connection settings from config.yaml
// or environment variables.
func loadCloudConfig() tenant.CloudConfig {
	cfg := tenant.CloudConfig{
		PublicKeyCacheHours: 24,
	}

	// Try config file first
	home, err := os.UserHomeDir()
	if err == nil {
		cfgPath := filepath.Join(home, ".iworkercenter", "config.yaml")
		if data, err := os.ReadFile(cfgPath); err == nil {
			// Simple YAML-like parsing for cloud section
			parseCloudConfig(data, &cfg)
		}
	}

	// Environment variables override
	if v := os.Getenv("IWORKERCENTER_CLOUD_URL"); v != "" {
		cfg.BaseURL = v
	}

	return cfg
}

// parseCloudConfig does minimal parsing of the cloud section from YAML.
func parseCloudConfig(data []byte, cfg *tenant.CloudConfig) {
	// Use gopkg.in/yaml.v3 if available, otherwise simple line parsing
	type yamlCfg struct {
		Cloud struct {
			BaseURL             string `yaml:"base_url"`
			PublicKeyCacheHours int    `yaml:"public_key_cache_hours"`
		} `yaml:"cloud"`
	}
	// Try to import yaml; for now use a simple approach
	// since the project already has gopkg.in/yaml.v3
	var parsed yamlCfg
	if err := yamlUnmarshal(data, &parsed); err == nil {
		if parsed.Cloud.BaseURL != "" {
			cfg.BaseURL = parsed.Cloud.BaseURL
		}
		if parsed.Cloud.PublicKeyCacheHours > 0 {
			cfg.PublicKeyCacheHours = parsed.Cloud.PublicKeyCacheHours
		}
	}
}

// yamlUnmarshal wraps yaml.Unmarshal. We import it at the top.
func yamlUnmarshal(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

// wireCompute initialises the compute module components.
// It reads cloud URL, center ID, and center secret from config/tenant,
// creates SyncManager (nil if cloud URL is not configured), SourceManager,
// LocalStore, and Handler.
func wireCompute(cloudCfg tenant.CloudConfig, tenantSvc *tenant.TenantService) (*compute.SyncManager, *compute.SourceManager, *compute.LocalStore, *compute.Handler) {
	// Resolve local store path
	localStorePath := defaultLocalStorePath()

	localStore := compute.NewLocalStore(localStorePath)

	// Try to get cloud connection info from the active tenant
	var cloudURL, centerID, centerSecret string
	cloudURL = cloudCfg.BaseURL

	if cloudURL != "" && tenantSvc != nil {
		tenants, err := tenantSvc.ListActiveTenants(context.Background())
		if err == nil && len(tenants) > 0 {
			centerID = tenants[0].CloudCenterID
			centerSecret = tenants[0].CloudSecret
		}
	}

	var syncMgr *compute.SyncManager
	if cloudURL != "" && centerID != "" {
		syncMgr = compute.NewSyncManager(cloudURL, centerID, centerSecret)
	} else {
		// Create a no-op sync manager (no cloud URL) so SourceManager works
		syncMgr = compute.NewSyncManager("", "", "")
	}

	sourceMgr := compute.NewSourceManager(syncMgr)
	handler := compute.NewHandler(syncMgr, sourceMgr, localStore)

	return syncMgr, sourceMgr, localStore, handler
}

// defaultLocalStorePath returns the path for the local compute providers JSON file.
func defaultWorkerMemoryStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "iworker_memory.json"
	}
	return filepath.Join(home, ".iworkercenter", "iworker_memory.json")
}

func defaultLocalStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "compute_providers.json"
	}
	return filepath.Join(home, ".iworkercenter", "compute_providers.json")
}
