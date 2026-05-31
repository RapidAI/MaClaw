package agentservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMCPReadinessManager_EnsureReady_LocalAutoStart(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789abcdef",
	}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_ = store.SaveTenant(Tenant{ID: "t1", Name: "test"})
	_ = store.SaveUser(User{TenantID: "t1", ID: "u1", Name: "user1"})

	cfg := UserConfig{
		TenantID: "t1",
		UserID:   "u1",
		AppConfig: corelib.AppConfig{
			LocalMCPServers: []corelib.LocalMCPServerEntry{
				{
					ID:        "mcp_local_test1",
					Name:      "test-server",
					Command:   "nonexistent-command-for-test",
					AutoStart: true,
					Disabled:  false,
				},
			},
		},
	}
	if err := store.SaveUserConfig(cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	mgr := NewMCPReadinessManager(svc)
	p := Principal{TenantID: "t1", UserID: "u1"}

	// EnsureReady should attempt to start the local server (will fail).
	mgr.EnsureReady(context.Background(), p)

	// Verify the attempt was recorded.
	mgr.mu.Lock()
	state := mgr.users[composite("t1", "u1")]
	mgr.mu.Unlock()

	if state == nil {
		t.Fatal("expected user readiness state to be created")
	}
	state.mu.Lock()
	_, ok := state.localAttempts["mcp_local_test1"]
	state.mu.Unlock()
	if !ok {
		t.Fatal("expected local server start attempt to be recorded")
	}
}

func TestMCPReadinessManager_EnsureReady_LocalDisabled_Skipped(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789abcdef",
	}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_ = store.SaveTenant(Tenant{ID: "t1", Name: "test"})
	_ = store.SaveUser(User{TenantID: "t1", ID: "u1", Name: "user1"})

	cfg := UserConfig{
		TenantID: "t1",
		UserID:   "u1",
		AppConfig: corelib.AppConfig{
			LocalMCPServers: []corelib.LocalMCPServerEntry{
				{
					ID:        "mcp_local_disabled",
					Name:      "disabled-server",
					Command:   "nonexistent",
					AutoStart: true,
					Disabled:  true,
				},
				{
					ID:        "mcp_local_noauto",
					Name:      "no-autostart",
					Command:   "nonexistent",
					AutoStart: false,
					Disabled:  false,
				},
			},
		},
	}
	if err := store.SaveUserConfig(cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	mgr := NewMCPReadinessManager(svc)
	mgr.EnsureReady(context.Background(), Principal{TenantID: "t1", UserID: "u1"})

	mgr.mu.Lock()
	state := mgr.users[composite("t1", "u1")]
	mgr.mu.Unlock()

	if state == nil {
		return // no state = no attempts = correct
	}
	state.mu.Lock()
	_, disabledAttempted := state.localAttempts["mcp_local_disabled"]
	_, noautoAttempted := state.localAttempts["mcp_local_noauto"]
	state.mu.Unlock()

	if disabledAttempted {
		t.Fatal("disabled server should not be started")
	}
	if noautoAttempted {
		t.Fatal("non-autostart server should not be started")
	}
}

func TestMCPReadinessManager_EnsureReady_LocalCooldown(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789abcdef",
	}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_ = store.SaveTenant(Tenant{ID: "t1", Name: "test"})
	_ = store.SaveUser(User{TenantID: "t1", ID: "u1", Name: "user1"})

	cfg := UserConfig{
		TenantID: "t1",
		UserID:   "u1",
		AppConfig: corelib.AppConfig{
			LocalMCPServers: []corelib.LocalMCPServerEntry{
				{
					ID:        "mcp_local_crashy",
					Name:      "crashy-server",
					Command:   "nonexistent",
					AutoStart: true,
					Disabled:  false,
				},
			},
		},
	}
	if err := store.SaveUserConfig(cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	mgr := NewMCPReadinessManager(svc)
	p := Principal{TenantID: "t1", UserID: "u1"}

	// First call — attempts start (fails).
	mgr.EnsureReady(context.Background(), p)

	mgr.mu.Lock()
	state := mgr.users[composite("t1", "u1")]
	mgr.mu.Unlock()

	state.mu.Lock()
	firstAttempt := state.localAttempts["mcp_local_crashy"]
	state.mu.Unlock()

	// Second call immediately — cooldown should prevent retry.
	mgr.EnsureReady(context.Background(), p)

	state.mu.Lock()
	secondAttempt := state.localAttempts["mcp_local_crashy"]
	state.mu.Unlock()

	if !secondAttempt.Equal(firstAttempt) {
		t.Fatal("expected cooldown to prevent second attempt")
	}
}

func TestMCPReadinessManager_EnsureReady_NoServers_FastPath(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789abcdef",
	}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_ = store.SaveTenant(Tenant{ID: "t1", Name: "test"})
	_ = store.SaveUser(User{TenantID: "t1", ID: "u1", Name: "user1"})

	// Empty config — no MCP servers.
	if err := store.SaveUserConfig(UserConfig{
		TenantID: "t1", UserID: "u1", AppConfig: corelib.AppConfig{},
	}); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	mgr := NewMCPReadinessManager(svc)
	mgr.EnsureReady(context.Background(), Principal{TenantID: "t1", UserID: "u1"})

	// No user state should be created (fast path).
	mgr.mu.Lock()
	_, exists := mgr.users[composite("t1", "u1")]
	mgr.mu.Unlock()

	if exists {
		t.Fatal("expected fast path to skip user state creation when no MCP servers configured")
	}
}

func TestMCPReadinessManager_EnsureReady_ReturnsEffectiveLLMFlatConfig(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789abcdef",
	}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_ = store.SaveTenant(Tenant{ID: "t1", Name: "test"})
	_ = store.SaveUser(User{TenantID: "t1", ID: "u1", Name: "user1"})

	if err := store.SaveUserConfig(UserConfig{
		TenantID: "t1",
		UserID:   "u1",
		AppConfig: corelib.AppConfig{
			MaclawLLMUrl:             "https://stale.example.test/v1",
			MaclawLLMKey:             "stale-key",
			MaclawLLMModel:           "stale-model",
			MaclawLLMCurrentProvider: "hub",
			MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub", URL: "https://hub.example.test/api/llm/v1", Key: "hub-key", Model: "auto"}},
		},
	}); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	mgr := NewMCPReadinessManager(svc)
	app, ok := mgr.EnsureReady(context.Background(), Principal{TenantID: "t1", UserID: "u1"})
	if !ok {
		t.Fatal("EnsureReady returned not ok")
	}
	if app.MaclawLLMUrl != "https://hub.example.test/api/llm/v1" || app.MaclawLLMKey != "hub-key" || app.MaclawLLMModel != "auto" {
		t.Fatalf("EnsureReady should return effective LLM flat config, got %#v", app)
	}
}

func TestMCPReadinessManager_Reset(t *testing.T) {
	svc, _ := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789abcdef",
	}, NewMemoryStore(), EchoExecutor{})

	mgr := NewMCPReadinessManager(svc)

	// Manually populate state.
	mgr.mu.Lock()
	mgr.users[composite("t1", "u1")] = &userReadinessState{
		localAttempts:  map[string]time.Time{"srv1": time.Now()},
		remoteAttempts: map[string]time.Time{"srv2": time.Now()},
	}
	mgr.mu.Unlock()

	mgr.Reset("t1", "u1")

	mgr.mu.Lock()
	_, exists := mgr.users[composite("t1", "u1")]
	mgr.mu.Unlock()

	if exists {
		t.Fatal("expected Reset to clear user state")
	}
}

func TestMCPToolBridge_ListAvailableTools_EnsuresReadiness(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789abcdef",
	}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_ = store.SaveTenant(Tenant{ID: "t1", Name: "test"})
	_ = store.SaveUser(User{TenantID: "t1", ID: "u1", Name: "user1"})

	if err := store.SaveUserConfig(UserConfig{
		TenantID: "t1",
		UserID:   "u1",
		AppConfig: corelib.AppConfig{
			LocalMCPServers: []corelib.LocalMCPServerEntry{
				{
					ID:        "mcp_local_x",
					Name:      "x-server",
					Command:   "nonexistent-x",
					AutoStart: true,
					Disabled:  false,
				},
			},
		},
	}); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	bridge := NewMCPToolBridge(svc)
	p := Principal{TenantID: "t1", UserID: "u1"}

	// Should not panic even when server fails to start.
	tools := bridge.ListAvailableTools(context.Background(), p)
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools from failed server, got %d", len(tools))
	}

	// Verify readiness manager recorded the attempt.
	bridge.readiness.mu.Lock()
	state := bridge.readiness.users[composite("t1", "u1")]
	bridge.readiness.mu.Unlock()

	if state == nil {
		t.Fatal("expected readiness state to be created by ListAvailableTools")
	}
	state.mu.Lock()
	_, ok := state.localAttempts["mcp_local_x"]
	state.mu.Unlock()
	if !ok {
		t.Fatal("expected ListAvailableTools to trigger EnsureReady for local server")
	}
}
