package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
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

	module, err := InitLLMModule(provider, st.System, "node-a", nil, nil, t.TempDir())
	if err != nil {
		t.Fatalf("InitLLMModule: %v", err)
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

func TestInitLLMModulePublishesDefaultComputeCardTypesToHA(t *testing.T) {
	ctx := context.Background()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "hubcenter-llm-init-ha.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	registrySvc := llmservice.NewService(st.System)
	if err := registrySvc.SaveRegistry(ctx, &llmservice.Registry{ServiceGroups: []llmpool.ServiceGroup{{
		ID:           "compute-group",
		Name:         "Compute Group",
		AccessPolicy: llmservice.AccessPolicyGrantRequired,
	}}}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	haSvc := ha.NewService("hc-1", "HubCenter 1", "https://hc-1.example.com", "secret", nil)
	haSvc.AttachStore(st)

	if _, err := InitLLMModule(provider, st.System, "hc-1", nil, haSvc, t.TempDir()); err != nil {
		t.Fatalf("InitLLMModule: %v", err)
	}
	ops, err := st.HASyncOps.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq: %v", err)
	}
	count := 0
	for _, op := range ops {
		if op.EntityType == ha.EntityLLMCardType {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("published default card type ops = %d, want 3", count)
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

	module, err := InitLLMModule(provider, st.System, "node-a", nil, nil, t.TempDir())
	if err != nil {
		t.Fatalf("InitLLMModule: %v", err)
	}
	types, err := module.CardStoreSvc.ListEnabledCardTypes(ctx)
	if err != nil {
		t.Fatalf("ListEnabledCardTypes: %v", err)
	}
	if len(types) != 0 {
		t.Fatalf("enabled card types = %d, want 0", len(types))
	}
}

func TestInitLLMModuleRepairsCardBackedFreeServiceGroup(t *testing.T) {
	ctx := context.Background()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-llm-init-card-backed.db"),
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
	if err := sqlite.EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables: %v", err)
	}
	st := sqlite.NewStore(provider)
	registrySvc := llmservice.NewService(st.System)
	if err := registrySvc.SaveRegistry(ctx, &llmservice.Registry{
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: llmservice.AccessPolicyFree,
		}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	cardRepo := sqlite.NewLLMCardTypeRepo(provider)
	if err := cardRepo.Create(ctx, &cardstore.CardType{
		ID:             "redeem-card",
		ServiceGroupID: "redeem",
		Label:          "Redeem Card",
		Credits:        1000,
		Period:         "month",
		PriceRMB:       10,
		Template:       "enterprise_monthly_blue",
		Enabled:        true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create card type: %v", err)
	}

	module, err := InitLLMModule(provider, st.System, "node-a", nil, nil, t.TempDir())
	if err != nil {
		t.Fatalf("InitLLMModule: %v", err)
	}
	reg, err := module.Service.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(reg.ServiceGroups) != 1 {
		t.Fatalf("service groups = %d, want 1", len(reg.ServiceGroups))
	}
	if got := reg.ServiceGroups[0].AccessPolicy; got != llmservice.AccessPolicyGrantRequired {
		t.Fatalf("access policy = %q, want %q", got, llmservice.AccessPolicyGrantRequired)
	}
	types, err := module.CardStoreSvc.ListEnabledCardTypes(ctx)
	if err != nil {
		t.Fatalf("ListEnabledCardTypes: %v", err)
	}
	if len(types) != 1 || types[0].ID != "redeem-card" {
		t.Fatalf("enabled card types = %+v, want only existing redeem-card", types)
	}
}

func TestInitLLMModuleInvalidatesLLMRegistryCacheOnHAApply(t *testing.T) {
	ctx := context.Background()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-llm-ha-pause.db"),
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
	haSvc := ha.NewService("hc-1", "HubCenter 1", "https://hc-1.example.com", "secret", nil)
	haSvc.AttachStore(st)
	module, err := InitLLMModule(provider, st.System, "hc-1", nil, haSvc, t.TempDir())
	if err != nil {
		t.Fatalf("InitLLMModule: %v", err)
	}
	if err := module.Service.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	got, err := module.Service.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Paused {
		t.Fatalf("fresh provider = %#v err=%v", got, err)
	}

	raw, err := st.System.Get(ctx, llmservice.RegistrySettingKey)
	if err != nil || raw == "" {
		t.Fatalf("load stored registry: %q err=%v", raw, err)
	}
	var stored llmservice.Registry
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("parse stored registry: %v", err)
	}
	if len(stored.Providers) == 0 {
		t.Fatal("stored registry missing provider")
	}
	stored.Providers[0].Paused = true
	pausedJSON, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal paused registry: %v", err)
	}
	payload, err := json.Marshal(map[string]string{
		"key":        llmservice.RegistrySettingKey,
		"value_json": string(pausedJSON),
	})
	if err != nil {
		t.Fatalf("marshal HA payload: %v", err)
	}
	sum := sha256.Sum256(payload)
	op := &store.HASyncOp{
		OpID:          "op-pause-registry",
		SourceNodeID:  "hc-2",
		EntityType:    ha.EntitySystemSetting,
		EntityID:      llmservice.RegistrySettingKey,
		OpType:        ha.OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   string(payload),
		PayloadHash:   hex.EncodeToString(sum[:]),
	}
	if err := haSvc.ApplyRemoteOp(ctx, op); err != nil {
		t.Fatalf("ApplyRemoteOp: %v", err)
	}
	got, err = module.Service.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || !got.Paused {
		t.Fatalf("replica cache after HA apply = %#v err=%v", got, err)
	}
}
