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
		name  string
		state mcpHealthStatus
		cache bool
		ready bool
	}{
		{name: "slow with cache", state: mcpHealthStatusSlow, cache: true, ready: true},
		{name: "healthy with cache", state: mcpHealthStatusHealthy, cache: true, ready: true},
		{name: "slow without cache", state: mcpHealthStatusSlow, cache: false, ready: false},
		{name: "degraded with stale cache", state: mcpHealthStatusDegraded, cache: true, ready: false},
		{name: "unknown with cache", state: mcpHealthStatusUnknown, cache: true, ready: false},
		{name: "unavailable with cache", state: mcpHealthStatusUnavailable, cache: true, ready: false},
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
