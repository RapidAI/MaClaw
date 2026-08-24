package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestTUIApp_RouteTurn_CostRouteOnUsesAux(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	llm.ResetCostRouteStatsForTest()
	t.Setenv(llm.CostRouteEnvKey, "on")

	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "http://p", Model: "primary-m", Key: "k", ContextLength: 400_000, ThinkingMode: "disabled", ReasoningEffort: "minimal"},
		appConfig: corelib.AppConfig{
			AuxiliaryLLM: corelib.AuxiliaryLLMConfig{URL: "http://a", Model: "aux-m", Key: "k", ContextLength: 200_000},
		},
	}
	cfg, d, ok := app.routeTurn("hi", llm.ClassifyHints{})
	if !ok {
		t.Fatal("expected route")
	}
	if d.CostRouteMode != "on" || !d.CostRouteApplied {
		t.Fatalf("decision=%+v", d)
	}
	// fast/intent → c0 → aux when no model_routes
	if cfg.Model != "aux-m" {
		t.Fatalf("model=%s want aux-m decision=%+v", cfg.Model, d)
	}
	if d.CostTier != "c0" {
		t.Fatalf("tier=%s", d.CostTier)
	}
	if cfg.ThinkingMode != "disabled" || cfg.ReasoningEffort != "minimal" {
		t.Fatalf("global thinking mode was overwritten by cost route: %+v", cfg)
	}
	if cfg.ContextLength != 200_000 {
		t.Fatalf("aux routed context length = %d, want 200000", cfg.ContextLength)
	}
}

func TestTUIAppRouteTurnUsesModelRouteContextLength(t *testing.T) {
	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "http://p", Model: "primary-m", Key: "k", ContextLength: 32_000},
		appConfig: corelib.AppConfig{ModelRoutes: map[string]corelib.ModelRouteConfig{
			"reasoning": {Model: "reasoning-m", ContextLength: 400_000},
		}},
	}
	cfg, decision, ok := app.routeTurn("fix the bug in this stack trace", llm.ClassifyHints{})
	if !ok || decision.Source != "route" || cfg.Model != "reasoning-m" || cfg.ContextLength != 400_000 {
		t.Fatalf("routed config=%+v decision=%+v", cfg, decision)
	}
}

func TestTUIApp_RouteTurn_HubManagedAttachesTaskHint(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	t.Setenv(llm.CostRouteEnvKey, "on")

	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto", Key: "k"},
		appConfig: corelib.AppConfig{
			AuxiliaryLLM: corelib.AuxiliaryLLMConfig{URL: "http://a", Model: "aux-m", Key: "k"},
		},
	}
	cfg, d, ok := app.routeTurn("hi", llm.ClassifyHints{})
	if !ok {
		t.Fatal("expected route")
	}
	if d.CostRouteApplied {
		t.Fatalf("hub-managed must not apply cost-route: %+v", d)
	}
	if cfg.Model != "auto" || !cfg.HubManaged || cfg.TaskTypeHint == "" {
		t.Fatalf("hub-managed hints missing: %+v", cfg)
	}
	if cfg.WorkloadClassHint != "" {
		t.Fatalf("desktop must not invent P0 class: %+v", cfg)
	}
}

func TestTUIApp_RouteTurn_HubManagedSendsWorkflowHints(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	t.Setenv(llm.CostRouteEnvKey, "on")

	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto", Key: "k"},
	}
	app.setHubWorkflowHints(&v2.WorkflowState{
		Type:         "coding",
		CurrentPhase: 0,
		Phases: []v2.Phase{{
			ID:   "implementation",
			Kind: v2.PhaseKindExecution,
		}},
	})
	cfg, d, ok := app.routeTurn("implement login", llm.ClassifyHints{ToolHeavy: true})
	if !ok || d.CostRouteApplied {
		t.Fatalf("hub-managed workflow route failed: ok=%v decision=%+v", ok, d)
	}
	if cfg.WorkflowTypeHint != "coding" || cfg.PhaseKindHint != "execution" {
		t.Fatalf("workflow hints: type=%q phase=%q", cfg.WorkflowTypeHint, cfg.PhaseKindHint)
	}
	if cfg.Model != "auto" {
		t.Fatalf("must keep auto, got %s", cfg.Model)
	}
}

func TestTUIApp_RouteTurn_HubServiceProviderSkipsCostApply(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	t.Setenv(llm.CostRouteEnvKey, "on")

	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "https://tenant.example/custom-llm", Model: "auto", Key: "k"},
		appConfig: corelib.AppConfig{
			AuxiliaryLLM: corelib.AuxiliaryLLMConfig{URL: "http://a", Model: "aux-m", Key: "k"},
			MaclawLLMProviders: []corelib.MaclawLLMProvider{{
				Name: "hub", URL: "https://tenant.example/custom-llm", Model: "auto", IsHubService: true,
			}},
		},
	}
	cfg, d, ok := app.routeTurn("hi", llm.ClassifyHints{})
	if !ok || d.CostRouteApplied || cfg.Model != "auto" || !cfg.HubManaged {
		t.Fatalf("IsHubService must skip cost-route: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
}

func TestTUIActiveLLM_GetKeepsRouted(t *testing.T) {
	var a tuiActiveLLM
	fb := corelib.MaclawLLMConfig{URL: "http://p", Model: "primary", Key: "k"}
	if got := a.get(fb); got.Model != "primary" {
		t.Fatalf("%+v", got)
	}
	a.set(corelib.MaclawLLMConfig{URL: "http://a", Model: "aux", Key: "k"})
	if got := a.get(fb); got.Model != "aux" {
		t.Fatalf("%+v", got)
	}
}

func TestTUIActiveLLMEscalatesContextOnlyReasoningRouteAfterTool(t *testing.T) {
	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "http://p", Model: "same-model", Key: "k", ContextLength: 32_000},
		appConfig: corelib.AppConfig{ModelRoutes: map[string]corelib.ModelRouteConfig{
			"reasoning": {Model: "same-model", ContextLength: 400_000},
		}},
	}
	var active tuiActiveLLM
	active.setRoute(app.llmConfig, agent.RouteDecision{TaskType: string(llm.TaskFast), Source: "route"})
	escalateTUIActiveLLMAfterTool(app, &active)
	got := active.get(app.llmConfig)
	if got.ContextLength != 400_000 || got.EffectiveContextTokens() != 320_000 {
		t.Fatalf("escalated config = %+v, want 400K context / 320K effective", got)
	}
}

func TestTUIActiveLLMDoesNotEscalateReasoningRouteAfterTool(t *testing.T) {
	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "http://p", Model: "same-model", Key: "k", ContextLength: 32_000},
		appConfig: corelib.AppConfig{ModelRoutes: map[string]corelib.ModelRouteConfig{
			"reasoning": {Model: "same-model", ContextLength: 400_000},
		}},
	}
	var active tuiActiveLLM
	active.setRoute(app.llmConfig, agent.RouteDecision{TaskType: string(llm.TaskReasoning), Source: "route"})
	escalateTUIActiveLLMAfterTool(app, &active)
	if got := active.get(app.llmConfig); got.ContextLength != 32_000 {
		t.Fatalf("reasoning route must not be overridden after a tool: %+v", got)
	}
}
