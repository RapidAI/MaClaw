package app

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	colleagueHandler "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/handler"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	colleagueService "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/capabilities"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/memories"
	roleHandler "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/handler"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	roleService "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/workflow"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

// Center holds all initialized services and the HTTP mux.
type Center struct {
	DB  *db.Provider
	Mux *http.ServeMux

	Roles      *roleService.RoleService
	Colleagues *colleagueService.ColleagueService
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

	// --- wire capabilities module ---
	capHandler := capabilities.NewHandler(provider.Write, provider.Read)

	// --- wire collaboration module ---
	collabRepo := collaboration.NewRepo(provider.Write, provider.Read)
	collabSvc := collaboration.NewService(collabRepo)
	collabHandler := collaboration.NewHandler(collabSvc)

	// --- wire workflow module (depends on collaboration + colleagues) ---
	wfRepo := workflow.NewRepo(provider.Write, provider.Read)
	wfSvc := workflow.NewService(wfRepo, provider, collabRepo, colRepo)
	wfHandler := workflow.NewHandler(wfSvc)

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
	capHandler.RegisterAdminRoutes(mux)
	capHandler.RegisterClientRoutes(mux)
	collabHandler.RegisterAdminRoutes(mux)
	collabHandler.RegisterClientRoutes(mux)
	wfHandler.RegisterAdminRoutes(mux)
	wfHandler.RegisterRuntimeRoutes(mux)
	wfHandler.RegisterClientRoutes(mux)

	log.Printf("[iWorkerCenter] bootstrap complete, dsn=%s", dsn)

	return &Center{
		DB:         provider,
		Mux:        mux,
		Roles:      rSvc,
		Colleagues: colSvc,
	}, nil
}

// Close releases all resources.
func (c *Center) Close() {
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
