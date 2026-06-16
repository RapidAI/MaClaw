package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

func TestAppCloseCancelsBackgroundWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{ctx: ctx, cancel: cancel}
	stopped := make(chan struct{})

	a.goBackground(func(ctx context.Context) {
		<-ctx.Done()
		close(stopped)
	})

	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("background worker was not canceled")
	}
}

func TestAppCloseIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{ctx: ctx, cancel: cancel}
	stopped := make(chan struct{})

	a.goBackground(func(ctx context.Context) {
		<-ctx.Done()
		close(stopped)
	})

	if err := a.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("background worker was not canceled")
	}
}

func TestInitLLMModuleSeedsDefaultComputeCardTypes(t *testing.T) {
	ctx := context.Background()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-llm-init.db"),
		WAL:               false,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	registrySvc := llmservice.NewService(st.System)
	if err := registrySvc.SaveRegistry(ctx, &llmservice.Registry{
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official-pro",
			Name:         "MaClaw Official Pro",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	module := InitLLMModule(provider, st.System, "node-a", nil, nil)
	if module == nil {
		t.Fatal("InitLLMModule returned nil")
	}
	types, err := module.CardStoreSvc.ListEnabledCardTypes(ctx)
	if err != nil {
		t.Fatalf("ListEnabledCardTypes: %v", err)
	}
	if len(types) != 3 {
		t.Fatalf("enabled card types = %d, want 3", len(types))
	}
	for _, ct := range types {
		if ct.ServiceGroupID != "maclaw-official-pro" {
			t.Fatalf("seeded card type service_group_id = %q, want maclaw-official-pro", ct.ServiceGroupID)
		}
		if !ct.Enabled || ct.PriceRMB <= 0 || ct.Credits <= 0 {
			t.Fatalf("invalid seeded card type: %+v", ct)
		}
	}
}

func TestInitLLMModuleDoesNotSeedCardsForFreeServiceGroup(t *testing.T) {
	ctx := context.Background()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-llm-init-free.db"),
		WAL:               false,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	registrySvc := llmservice.NewService(st.System)
	if err := registrySvc.SaveRegistry(ctx, &llmservice.Registry{
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "free-group",
			Name:         "Free Group",
			AccessPolicy: llmservice.AccessPolicyFree,
		}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	module := InitLLMModule(provider, st.System, "node-a", nil, nil)
	if module == nil {
		t.Fatal("InitLLMModule returned nil")
	}
	types, err := module.CardStoreSvc.ListEnabledCardTypes(ctx)
	if err != nil {
		t.Fatalf("ListEnabledCardTypes: %v", err)
	}
	if len(types) != 0 {
		t.Fatalf("enabled card types = %d, want 0", len(types))
	}
}
