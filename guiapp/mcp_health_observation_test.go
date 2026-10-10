package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestSemanticMCPInventorySeparatesSuccessfulSlowFromFailedProbe(t *testing.T) {
	cases := []struct {
		name    string
		state   mcpHealthStatus
		cache   bool
		ready   bool
		blocked bool
	}{
		{name: "slow with cache", state: mcpHealthStatusSlow, cache: true, ready: true},
		{name: "healthy with cache", state: mcpHealthStatusHealthy, cache: true, ready: true},
		// A slow status without a tools/list cache is not a finished observation.
		{name: "slow without cache", state: mcpHealthStatusSlow, cache: false, ready: false},
		// A finished negative probe keeps the known tool and closes the family,
		// so a healthy sibling can still be selected.
		{name: "degraded with stale cache", state: mcpHealthStatusDegraded, cache: true, ready: true, blocked: true},
		{name: "unknown with cache", state: mcpHealthStatusUnknown, cache: true, ready: false},
		{name: "unavailable with cache", state: mcpHealthStatusUnavailable, cache: true, ready: true, blocked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &App{testHomeDir: t.TempDir()}
			cfg, err := app.LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			cfg.MCPServers = []corelib.MCPServerEntry{{ID: "remote", Name: "remote", EndpointURL: "https://example.invalid/mcp"}}
			if err := app.SaveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			app.mcpRegistry = NewMCPRegistry(app)
			defer app.closeSemanticInvocationStore()
			app.mcpRegistry.health["remote"] = &mcpHealthState{Status: tc.state}
			if tc.cache {
				app.mcpRegistry.toolsCache["remote"] = []MCPToolView{{Name: "work_log_query"}}
			}
			h := &IMMessageHandler{app: app}
			inventory, err := h.semanticDynamicInventory(context.Background(), "desktop-user")
			if err != nil {
				t.Fatal(err)
			}
			coverage := inventory.coverage.ForProviderKind("mcp")
			ready := coverage.State == tool.CatalogCoverageComplete && coverage.ReasonCode == ""
			if ready != tc.ready {
				t.Fatalf("coverage=%+v want ready=%v", coverage, tc.ready)
			}
			if !tc.ready && coverage.ReasonCode != tool.CatalogCoverageReasonNotReady {
				t.Fatalf("coverage=%+v", coverage)
			}
			if !tc.ready {
				if len(inventory.mcpEntries) != 0 {
					t.Fatalf("open observation published tools: %#v", inventory.mcpEntries)
				}
				return
			}
			if !tc.blocked {
				if len(inventory.mcpEntries) != 1 || inventory.mcpEntries[0].RuntimeBlocked || inventory.mcpEntries[0].ToolName != "work_log_query" {
					t.Fatalf("entries=%#v", inventory.mcpEntries)
				}
				return
			}
			if len(inventory.mcpEntries) != 1 || !inventory.mcpEntries[0].RuntimeBlocked || inventory.mcpEntries[0].ToolName != "work_log_query" {
				t.Fatalf("closed negative entries=%#v", inventory.mcpEntries)
			}
		})
	}
}

func TestBoundMCPCallRejectsFailedProbeBeforeTransport(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{
		ID: "remote", Name: "remote", EndpointURL: "http://127.0.0.1:1/mcp",
	}}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	app.mcpRegistry.client.Timeout = time.Second
	app.mcpRegistry.health["remote"] = &mcpHealthState{Status: mcpHealthStatusSlow}
	app.mcpRegistry.toolsCache["remote"] = []MCPToolView{{Name: "work_log_query"}}
	bridge := guiSemanticMCPBridge{handler: &IMMessageHandler{app: app}}
	binding := agentservice.MCPToolBinding{ServerID: "remote", ToolName: "work_log_query"}
	_, err := bridge.CallBoundTool(context.Background(), agentservice.Principal{UserID: "desktop-user"}, binding, map[string]interface{}{})
	if err == nil || strings.Contains(err.Error(), "mcp_binding_stale") {
		t.Fatalf("successful slow observation err=%v", err)
	}
	if app.mcpRegistry.health["remote"].Status != mcpHealthStatusDegraded {
		t.Fatalf("transport failure status=%s", app.mcpRegistry.health["remote"].Status)
	}
	_, err = bridge.CallBoundTool(context.Background(), agentservice.Principal{UserID: "desktop-user"}, binding, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "mcp_binding_stale") {
		t.Fatalf("failed probe err=%v", err)
	}
}

func TestSuccessfulSlowMCPObservationPlansReviewedWorklogRead(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "work_log_query", Description: "query work log", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "tengyun", tools)
	app.mcpRegistry.health["tengyun"].Status = mcpHealthStatusSlow
	if err := app.publishReviewedMCPServerTools("tengyun", tools); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", "查一下今天上午的工作日志", "desktop", "root-slow-worklog", "turn-slow-worklog", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityRecordReadWorklog)
	if !ok || selection.Provider.Kind != "mcp" {
		t.Fatalf("read selection=%+v ok=%v", selection, ok)
	}
}

func TestDegradedMCPObservationFailClosesReviewedWorklogRead(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "work_log_query", Description: "query work log", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "tengyun", tools)
	app.mcpRegistry.health["tengyun"].Status = mcpHealthStatusDegraded
	if err := app.publishReviewedMCPServerTools("tengyun", tools); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", "查一下今天上午的工作日志", "desktop", "root-degraded-worklog", "turn-degraded-worklog", stored,
	)
	if !handled || err == nil || surface != nil || !strings.Contains(err.Error(), "record.read.worklog") || !strings.Contains(err.Error(), tool.CatalogCoverageReasonNotReady) {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
}

func TestDegradedMCPSiblingLeavesHealthyWorklogSelectable(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	readTools := []MCPToolView{{Name: "work_log_query", Description: "query work log", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "tengyun", readTools)
	if err := app.publishReviewedMCPServerTools("tengyun", readTools); err != nil {
		t.Fatal(err)
	}
	const updateTool = "fixture_worklog_append"
	updateTools := []MCPToolView{{Name: updateTool, Description: "append a work record", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "fixture-mcp", updateTools)
	app.mcpRegistry.health["fixture-mcp"].Status = mcpHealthStatusDegraded
	contracts, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.PublishMCPContract(semanticDesktopContractPrincipal(), "fixture-mcp", updateTool, agentservice.DynamicCapabilityContract{
		Provisions:            []tool.CapabilityProvision{{Capability: tool.CapabilityRecordUpdateWorklog, Quality: 3}},
		Effects:               []tool.EffectClass{tool.EffectExternalEffect},
		ObservedBindingDigest: agentservice.DynamicMCPObservedBindingDigest("fixture-mcp", updateTool, schema),
	}); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", "查一下今天上午的工作日志", "desktop", "root-sibling-read", "turn-sibling-read", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("healthy read handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityRecordReadWorklog)
	if !ok || selection.Provider.Kind != "mcp" || selection.Provider.ProviderID != "tengyun" {
		t.Fatalf("read selection=%+v ok=%v", selection, ok)
	}
	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", worklogProductionUtterance, "desktop", "root-sibling-update", "turn-sibling-update", stored,
	)
	if !handled || err == nil || surface != nil || !strings.Contains(err.Error(), "record.update.worklog") || !strings.Contains(err.Error(), tool.CatalogCoverageReasonNotReady) {
		t.Fatalf("degraded update handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
}

func TestAutoStartFalseLocalMCPDoesNotOpenFamily(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	t.Run("manual remote stays complete", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		defer app.closeSemanticInvocationStore()
		if err := app.SaveConfig(corelib.AppConfig{
			MCPServers: []corelib.MCPServerEntry{{ID: "tengyun", Name: "tengyun", EndpointURL: "https://example.invalid/mcp"}},
			LocalMCPServers: []corelib.LocalMCPServerEntry{
				{ID: "offline", Name: "offline", Command: "unused", AutoStart: false},
				{ID: "disabled", Name: "disabled", Command: "unused", AutoStart: true, Disabled: true},
			},
		}); err != nil {
			t.Fatal(err)
		}
		app.mcpRegistry = NewMCPRegistry(app)
		app.mcpRegistry.health["tengyun"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
		app.mcpRegistry.toolsCache["tengyun"] = []MCPToolView{{Name: "work_log_query", InputSchema: schema}}
		inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
		if err != nil {
			t.Fatal(err)
		}
		coverage := inventory.coverage.ForProviderKind("mcp")
		if coverage.State != tool.CatalogCoverageComplete || coverage.ReasonCode != "" {
			t.Fatalf("coverage=%+v", coverage)
		}
		if len(inventory.mcpEntries) != 1 || inventory.mcpEntries[0].ServerID != "tengyun" || inventory.mcpEntries[0].RuntimeBlocked {
			t.Fatalf("entries=%#v", inventory.mcpEntries)
		}
	})
	t.Run("launch set not running is closed", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		defer app.closeSemanticInvocationStore()
		if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{
			{ID: "manual-off", Name: "manual-off", Command: "unused", AutoStart: false},
			{ID: "launch-down", Name: "launch-down", Command: "unused", AutoStart: true},
		}}); err != nil {
			t.Fatal(err)
		}
		app.mcpRegistry = NewMCPRegistry(app)
		app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
		t.Cleanup(func() { app.localMCPManager.cancel() })
		// A sync has already returned and left the launch server down.
		app.localMCPManager.syncFinished.Store(true)
		inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
		if err != nil {
			t.Fatal(err)
		}
		coverage := inventory.coverage.ForProviderKind("mcp")
		if coverage.State != tool.CatalogCoverageComplete || len(inventory.mcpEntries) != 0 {
			t.Fatalf("finished local failure stayed open: coverage=%+v entries=%#v", coverage, inventory.mcpEntries)
		}
	})
	t.Run("canceled sync leaves the launch set open", func(t *testing.T) {
		base := withIsolatedMaclawBase(t)
		app := &App{testHomeDir: base}
		defer app.closeSemanticInvocationStore()
		local := corelib.LocalMCPServerEntry{
			ID: "market-local", Name: "market-local", Command: "unused", AutoStart: true,
			Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap-local"},
		}
		if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{local}}); err != nil {
			t.Fatal(err)
		}
		app.mcpRegistry = NewMCPRegistry(app)
		if err := app.markMCPRuntimeSyncReady(local.ID); err != nil {
			t.Fatal(err)
		}
		app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
		t.Cleanup(func() { app.localMCPManager.cancel() })
		parent, cancel := context.WithCancel(context.Background())
		cancel()
		if err := app.localMCPManager.syncFromConfig(parent); err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("sync err=%v", err)
		}
		state, ok := loadMCPRuntimeSyncState(local.ID)
		if !ok || state.Status != "ready" || state.Attempts != 0 {
			t.Fatalf("canceled sync consumed the durable budget: %+v ok=%v", state, ok)
		}
		if app.localMCPManager.configSyncFinished() {
			t.Fatal("canceled sync marked the launch set finished")
		}
		inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
		if err != nil {
			t.Fatal(err)
		}
		coverage := inventory.coverage.ForProviderKind("mcp")
		if coverage.State != tool.CatalogCoverageIncomplete || coverage.ReasonCode != tool.CatalogCoverageReasonNotReady || len(inventory.mcpEntries) != 0 {
			t.Fatalf("canceled sync closed the launch set: coverage=%+v entries=%#v", coverage, inventory.mcpEntries)
		}
	})
	t.Run("manager created before sync starts is open", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		defer app.closeSemanticInvocationStore()
		if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{
			{ID: "launch-unstarted", Name: "launch-unstarted", Command: "unused", AutoStart: true},
		}}); err != nil {
			t.Fatal(err)
		}
		app.mcpRegistry = NewMCPRegistry(app)
		app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
		t.Cleanup(func() { app.localMCPManager.cancel() })
		inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
		if err != nil {
			t.Fatal(err)
		}
		coverage := inventory.coverage.ForProviderKind("mcp")
		if coverage.State != tool.CatalogCoverageIncomplete || coverage.ReasonCode != tool.CatalogCoverageReasonNotReady || len(inventory.mcpEntries) != 0 {
			t.Fatalf("unsynced manager closed the launch set: coverage=%+v entries=%#v", coverage, inventory.mcpEntries)
		}
	})
	t.Run("launch set before manager exists is open", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		defer app.closeSemanticInvocationStore()
		if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{
			{ID: "launch-pending", Name: "launch-pending", Command: "unused", AutoStart: true},
		}}); err != nil {
			t.Fatal(err)
		}
		app.mcpRegistry = NewMCPRegistry(app)
		inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
		if err != nil {
			t.Fatal(err)
		}
		coverage := inventory.coverage.ForProviderKind("mcp")
		if coverage.State != tool.CatalogCoverageIncomplete || coverage.ReasonCode != tool.CatalogCoverageReasonNotReady {
			t.Fatalf("coverage=%+v", coverage)
		}
	})
	t.Run("launch set sync in progress is open", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		defer app.closeSemanticInvocationStore()
		if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{
			{ID: "launch-sync", Name: "launch-sync", Command: "unused", AutoStart: true},
		}}); err != nil {
			t.Fatal(err)
		}
		app.mcpRegistry = NewMCPRegistry(app)
		app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
		t.Cleanup(func() { app.localMCPManager.cancel() })
		app.localMCPManager.syncFinished.Store(true)
		app.localMCPManager.syncInFlight.Add(1)
		inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
		if err != nil {
			t.Fatal(err)
		}
		coverage := inventory.coverage.ForProviderKind("mcp")
		if coverage.State != tool.CatalogCoverageIncomplete || coverage.ReasonCode != tool.CatalogCoverageReasonNotReady {
			t.Fatalf("coverage=%+v", coverage)
		}
	})
	t.Run("disabled running server is omitted", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		defer app.closeSemanticInvocationStore()
		schema := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"additionalProperties": false,
		}
		disabled := corelib.LocalMCPServerEntry{ID: "disabled-up", Name: "disabled-up", Command: "unused", AutoStart: true, Disabled: true}
		if err := app.SaveConfig(corelib.AppConfig{
			MCPServers:      []corelib.MCPServerEntry{{ID: "tengyun", Name: "tengyun", EndpointURL: "https://example.invalid/mcp"}},
			LocalMCPServers: []corelib.LocalMCPServerEntry{disabled},
		}); err != nil {
			t.Fatal(err)
		}
		app.mcpRegistry = NewMCPRegistry(app)
		app.mcpRegistry.health["tengyun"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
		app.mcpRegistry.toolsCache["tengyun"] = []MCPToolView{{Name: "work_log_query", InputSchema: schema}}
		app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
		t.Cleanup(func() { app.localMCPManager.cancel() })
		app.localMCPManager.syncFinished.Store(true)
		client := &LocalMCPClient{entry: disabled}
		client.stateMu.Lock()
		client.running = true
		client.tools = []MCPToolView{{Name: "disabled_tool", InputSchema: schema}}
		client.stateMu.Unlock()
		app.localMCPManager.clients[disabled.ID] = client
		inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
		if err != nil {
			t.Fatal(err)
		}
		coverage := inventory.coverage.ForProviderKind("mcp")
		if coverage.State != tool.CatalogCoverageComplete || coverage.ReasonCode != "" {
			t.Fatalf("coverage=%+v", coverage)
		}
		if len(inventory.mcpEntries) != 1 || inventory.mcpEntries[0].ServerID != "tengyun" || inventory.mcpEntries[0].RuntimeBlocked {
			t.Fatalf("entries=%#v", inventory.mcpEntries)
		}
	})
}

func TestLocalMCPConfigSyncCompletionIsObservable(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{
		{ID: "disabled-only", Name: "disabled-only", Command: "unused", AutoStart: true, Disabled: true},
	}}); err != nil {
		t.Fatal(err)
	}
	manager := NewLocalMCPManager(NewMCPRegistry(app))
	t.Cleanup(func() { manager.cancel() })
	if manager.configSyncFinished() || manager.configSyncInProgress() {
		t.Fatal("a new manager already looked synced")
	}
	if err := manager.syncFromConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.configSyncInProgress() || !manager.configSyncFinished() {
		t.Fatalf("inFlight=%d finished=%v", manager.syncInFlight.Load(), manager.configSyncFinished())
	}
}

func TestRemoteMCPMemberOpen(t *testing.T) {
	cases := []struct {
		name     string
		status   mcpHealthStatus
		observed bool
		sync     string
		retryDue bool
		open     bool
	}{
		{name: "healthy cache", status: mcpHealthStatusHealthy, observed: true, open: false},
		{name: "healthy without cache", status: mcpHealthStatusHealthy, open: true},
		{name: "slow cache", status: mcpHealthStatusSlow, observed: true, open: false},
		{name: "slow without cache", status: mcpHealthStatusSlow, open: true},
		{name: "degraded cache", status: mcpHealthStatusDegraded, observed: true, sync: "pending", retryDue: true, open: false},
		{name: "unavailable", status: mcpHealthStatusUnavailable, observed: true, open: false},
		{name: "unknown cache", status: mcpHealthStatusUnknown, observed: true, open: true},
		{name: "needs review", status: mcpHealthStatusUnknown, sync: "needs_review", retryDue: true, open: false},
		{name: "pending not due", status: mcpHealthStatusUnknown, sync: "pending", open: false},
		{name: "pending due", status: mcpHealthStatusUnknown, sync: "pending", retryDue: true, open: true},
		{name: "ready marker is not a tool list", status: mcpHealthStatusUnknown, sync: "ready", open: true},
		{name: "manual unknown", status: mcpHealthStatusUnknown, open: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteMCPMemberOpen(tc.status, tc.observed, tc.sync, tc.retryDue); got != tc.open {
				t.Fatalf("open=%v want %v", got, tc.open)
			}
		})
	}
}

func withIsolatedMaclawBase(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	old := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(old) })
	return base
}

func setMCPRuntimeSyncPersistenceFence(t *testing.T, serverID string) {
	t.Helper()
	key := mcpRuntimeSyncPersistenceKey(serverID)
	mcpRuntimeSyncStateMu.Lock()
	mcpRuntimeSyncPersistenceFailures[key] = true
	mcpRuntimeSyncStateMu.Unlock()
	t.Cleanup(func() {
		mcpRuntimeSyncStateMu.Lock()
		delete(mcpRuntimeSyncPersistenceFailures, key)
		mcpRuntimeSyncStateMu.Unlock()
	})
}

func TestDurableSyncBlockerKeepsStaleHealthyObservationUnselectable(t *testing.T) {
	base := withIsolatedMaclawBase(t)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{
		{ID: "stale", Name: "stale", EndpointURL: "https://example.invalid/stale"},
		{ID: "fresh", Name: "fresh", EndpointURL: "https://example.invalid/fresh"},
	}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.recordMCPRuntimeSyncFailure("stale", "http", "", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry.health["stale"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry.toolsCache["stale"] = []MCPToolView{{Name: "stale_tool", InputSchema: schema}}
	app.mcpRegistry.health["fresh"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry.toolsCache["fresh"] = []MCPToolView{{Name: "fresh_tool", InputSchema: schema}}
	inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageComplete || coverage.ReasonCode != "" {
		t.Fatalf("coverage=%+v", coverage)
	}
	var stale, fresh *agentservice.MCPToolEntry
	for i := range inventory.mcpEntries {
		switch inventory.mcpEntries[i].ServerID {
		case "stale":
			stale = &inventory.mcpEntries[i]
		case "fresh":
			fresh = &inventory.mcpEntries[i]
		}
	}
	if stale == nil || !stale.RuntimeBlocked || fresh == nil || fresh.RuntimeBlocked {
		t.Fatalf("entries=%#v", inventory.mcpEntries)
	}
}

func TestNeedsReviewLocalAutoStartDoesNotOpenMCPFamily(t *testing.T) {
	base := withIsolatedMaclawBase(t)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	local := corelib.LocalMCPServerEntry{
		ID: "market-local", Name: "market-local", Command: "unused", AutoStart: true,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap-local"},
	}
	if err := app.SaveConfig(corelib.AppConfig{
		MCPServers:      []corelib.MCPServerEntry{{ID: "tengyun", Name: "tengyun", EndpointURL: "https://example.invalid/mcp"}},
		LocalMCPServers: []corelib.LocalMCPServerEntry{local},
	}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	for i := 0; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure(local.ID, "stdio", "", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	app.mcpRegistry.health["tengyun"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry.toolsCache["tengyun"] = []MCPToolView{{Name: "work_log_query", InputSchema: schema}}
	inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageComplete || coverage.ReasonCode != "" {
		t.Fatalf("coverage=%+v", coverage)
	}
	if len(inventory.mcpEntries) != 1 || inventory.mcpEntries[0].ServerID != "tengyun" || inventory.mcpEntries[0].RuntimeBlocked {
		t.Fatalf("entries=%#v", inventory.mcpEntries)
	}
}

func TestRunningLocalUnderNeedsReviewStaysBlocked(t *testing.T) {
	base := withIsolatedMaclawBase(t)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	localEntry := corelib.LocalMCPServerEntry{
		ID: "market-local", Name: "market-local", Command: "unused", AutoStart: true,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap-local"},
	}
	if err := app.SaveConfig(corelib.AppConfig{
		MCPServers:      []corelib.MCPServerEntry{{ID: "tengyun", Name: "tengyun", EndpointURL: "https://example.invalid/mcp"}},
		LocalMCPServers: []corelib.LocalMCPServerEntry{localEntry},
	}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	for i := 0; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure(localEntry.ID, "stdio", "", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	app.mcpRegistry.health["tengyun"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry.toolsCache["tengyun"] = []MCPToolView{{Name: "work_log_query", InputSchema: schema}}
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	t.Cleanup(func() { app.localMCPManager.cancel() })
	client := &LocalMCPClient{entry: localEntry}
	client.stateMu.Lock()
	client.running = true
	client.tools = []MCPToolView{{Name: "local_tool", InputSchema: schema}}
	client.stateMu.Unlock()
	app.localMCPManager.clients[localEntry.ID] = client
	inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageComplete || coverage.ReasonCode != "" {
		t.Fatalf("coverage=%+v", coverage)
	}
	var localBlocked, remoteBlocked bool
	var sawLocal, sawRemote bool
	for _, entry := range inventory.mcpEntries {
		switch entry.ServerID {
		case localEntry.ID:
			sawLocal = true
			localBlocked = entry.RuntimeBlocked
		case "tengyun":
			sawRemote = true
			remoteBlocked = entry.RuntimeBlocked
		}
	}
	if !sawLocal || !localBlocked || !sawRemote || remoteBlocked {
		t.Fatalf("entries=%#v", inventory.mcpEntries)
	}
}

func TestPersistenceFenceClosesReadyMarkerWithoutToolList(t *testing.T) {
	base := withIsolatedMaclawBase(t)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	stale := corelib.MCPServerEntry{
		ID: "market-ready", Name: "market", EndpointURL: "https://example.invalid/market",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{
		stale,
		{ID: "fresh", Name: "fresh", EndpointURL: "https://example.invalid/fresh"},
	}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.markMCPRuntimeSyncReady(stale.ID); err != nil {
		t.Fatal(err)
	}
	setMCPRuntimeSyncPersistenceFence(t, stale.ID)
	app.mcpRegistry.health["fresh"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry.toolsCache["fresh"] = []MCPToolView{{Name: "fresh_tool", InputSchema: schema}}
	inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageComplete || coverage.ReasonCode != "" {
		t.Fatalf("coverage=%+v", coverage)
	}
	if len(inventory.mcpEntries) != 1 || inventory.mcpEntries[0].ServerID != "fresh" || inventory.mcpEntries[0].RuntimeBlocked {
		t.Fatalf("entries=%#v", inventory.mcpEntries)
	}
}
