package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestDebugToolSurfaceForTrialLoop(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "pro"
	cfg.TrialReflectEnabled = true
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           "http://127.0.0.1:1",
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 5
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := app.SaveMaclawLLMProviders(cfg.MaclawLLMProviders, cfg.MaclawLLMCurrentProvider); err != nil {
		t.Fatalf("SaveMaclawLLMProviders: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "test bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress coretool.ProgressCallback) string {
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}

	all := h.getTools()
	t.Logf("getTools count=%d names=%v", len(all), agentLoopToolNamesForLog(all))

	ctx := NewLoopContext("chat-trial-retry", 5, nil)
	phase := h.initialAgentLoopPhase("run retry loop", ctx)
	toolSet := h.prepareAgentLoopTools("u1", "run retry loop", ctx, phase)
	t.Logf("Tools count=%d names=%v", len(toolSet.Tools), agentLoopToolNamesForLog(toolSet.Tools))
	t.Logf("BaseTools count=%d names=%v", len(toolSet.BaseTools), agentLoopToolNamesForLog(toolSet.BaseTools))
	t.Logf("LegacyPlanBacked=%v ClientToolNames=%v", toolSet.LegacyPlanBacked, toolSet.ClientToolNames)

	ctx3 := NewLoopContext("chat-trial-retry", 5, nil)
	_, _, handled, serr := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx3, "u1", "run retry loop", "desktop", nil)
	t.Logf("semantic handled=%v err=%v", handled, serr)
	applySemanticChatProjection(ctx3)
	applySemanticRoutingMissFallback(ctx3)
	if ctx3.Runtime.SemanticIntent != nil {
		t.Logf("SemanticIntent primary=%v conf=%.2f degraded=%v managed=%v", ctx3.Runtime.SemanticIntent.Primary, ctx3.Runtime.SemanticIntent.Confidence, ctx3.Runtime.SemanticIntent.Degraded, imSemanticIntentIsManagedForLoop(ctx3.WorkflowAgentLoop, *ctx3.Runtime.SemanticIntent))
	}
	phase3 := h.initialAgentLoopPhase("run retry loop", ctx3)
	ts3 := h.prepareAgentLoopTools("u1", "run retry loop", ctx3, phase3)
	t.Logf("after-miss-fallback Tools count=%d names=%v", len(ts3.Tools), agentLoopToolNamesForLog(ts3.Tools))
	ctx2 := NewLoopContext("chat-trial-retry2", 5, nil)
	ss := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context: ctx2, UserID: "u1", UserText: "run retry loop", SystemPrompt: "system", Platform: "desktop",
	})
	if ss.Cleanup != nil {
		defer ss.Cleanup()
	}
	t.Logf("startState Tools count=%d names=%v HostReject=%v semantic=%v", len(ss.Tools), agentLoopToolNamesForLog(ss.Tools), ss.HostReject != nil, ss.SemanticSurface != nil)
}
