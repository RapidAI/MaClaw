package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/auth"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/centers"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/compute"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/config"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/httpapi"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store/sqlite"
)

type App struct {
	Handler http.Handler
	db      *sqlite.Provider
}

func Bootstrap(cfg *config.Config) (*App, error) {
	provider, err := sqlite.NewProvider(cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := sqlite.RunMigrations(provider.Write); err != nil {
		provider.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	st := sqlite.NewStore(provider)

	// License key pair
	dataDir := filepath.Dir(cfg.Database.DSN)
	privKey, err := license.EnsureKeyPair(dataDir)
	if err != nil {
		provider.Close()
		return nil, fmt.Errorf("ensure key pair: %w", err)
	}
	log.Printf("[iWorkerCloud] RSA key pair ready in %s", dataDir)

	// Services
	authSvc := auth.NewService(st.Admins)
	licenseSvc := license.NewService(st.Licenses, privKey)
	centerSvc := centers.NewService(st.Centers, licenseSvc)
	centerSvc.SetPrivateKey(privKey)
	skillMarketSvc := skillmarket.NewService(st.Skills)
	if err := skillMarketSvc.EnsureDefaults(context.Background()); err != nil {
		provider.Close()
		return nil, fmt.Errorf("skill market defaults: %w", err)
	}

	// Compute power management
	computeEncKey, err := compute.LoadOrGenerateKey(dataDir)
	if err != nil {
		provider.Close()
		return nil, fmt.Errorf("compute encryption key: %w", err)
	}
	computeStore := compute.NewProviderStore(provider.Write, computeEncKey)
	if err := computeStore.CreateTable(context.Background()); err != nil {
		provider.Close()
		return nil, fmt.Errorf("compute tables: %w", err)
	}
	computeHandler := httpapi.NewComputeHandler(computeStore, centerSvc)

	handler := httpapi.NewRouter(authSvc, centerSvc, licenseSvc, dataDir, computeHandler, skillMarketSvc)

	return &App{Handler: handler, db: provider}, nil
}

func (a *App) Close() {
	if a.db != nil {
		a.db.Close()
	}
}
