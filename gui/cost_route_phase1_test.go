package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	cllm "github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestApplyTurnModelRoute_AttachesCostTierShadow(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "shadow")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{
		URL: "https://example.invalid/v1", Model: "primary-model", ProviderName: "p1",
	}
	_, d := h.applyTurnModelRoute(primary, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if d.CostRouteMode != "shadow" {
		t.Fatalf("mode=%s", d.CostRouteMode)
	}
	if d.CostRouteApplied {
		t.Fatal("shadow must not apply")
	}
	if d.Model != primary.Model {
		t.Fatalf("shadow must keep model: got %s", d.Model)
	}
	if d.Task == string(cllm.TaskFast) && d.CostTier != "c0" {
		t.Fatalf("fast should map c0, got %s", d.CostTier)
	}
}

func TestApplyTurnModelRoute_CostOnAppliesAuxForGreeting(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	app := &App{}
	app.ohModules.cachedAuxLLM = corelib.AuxiliaryLLMConfig{
		URL: "https://aux.example", Key: "ak", Model: "aux-flash",
	}
	h := &IMMessageHandler{app: app}
	primary := corelib.MaclawLLMConfig{URL: "https://p.example", Key: "pk", Model: "primary-m"}
	cfg, d := h.applyTurnModelRoute(primary, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if d.CostRouteMode != "on" || !d.CostRouteApplied {
		t.Fatalf("decision=%+v", d)
	}
	if d.CostTier != "c0" {
		t.Fatalf("greeting tier=%s want c0", d.CostTier)
	}
	if cfg.Model != "aux-flash" || d.Model != "aux-flash" {
		t.Fatalf("expected aux model, cfg=%s decision=%s", cfg.Model, d.Model)
	}
	if d.Source != "aux" {
		t.Fatalf("source=%s", d.Source)
	}
	// Phase 3: c0 → thinking off
	if d.ThinkingPolicy != "off" {
		t.Fatalf("think=%s", d.ThinkingPolicy)
	}
	if cfg.ThinkingMode != "disabled" || cfg.ReasoningEffort != "none" {
		t.Fatalf("cfg thinking=%+v effort=%s", cfg.ThinkingMode, cfg.ReasoningEffort)
	}
}

func TestApplyTurnModelRoute_HubManagedSkipsCostApply(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	app := &App{}
	app.ohModules.cachedAuxLLM = corelib.AuxiliaryLLMConfig{
		URL: "https://aux.example", Key: "ak", Model: "aux-flash",
	}
	h := &IMMessageHandler{app: app}
	primary := corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto", ProviderName: "hub"}
	cfg, d := h.applyTurnModelRoute(primary, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if d.CostRouteApplied {
		t.Fatalf("hub-managed must not apply desktop cost-route: %+v", d)
	}
	if cfg.Model != "auto" || cfg.URL != primary.URL {
		t.Fatalf("hub-managed must keep auto: cfg=%+v", cfg)
	}
	if !strings.Contains(d.Reason, "hub-managed") {
		t.Fatalf("reason=%q", d.Reason)
	}
	if !cfg.HubManaged || cfg.TaskTypeHint == "" {
		t.Fatalf("hub-managed must attach task hint: %+v", cfg)
	}
	if cfg.WorkloadClassHint != "" {
		t.Fatalf("desktop must not invent P0 class: %+v", cfg)
	}
}

func TestApplyTurnModelRoute_HubManagedSendsWorkflowHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"}
	cfg, _ := h.applyTurnModelRoute(primary, "实现登录接口", &LoopContext{
		Kind:              LoopKindChat,
		WorkflowAgentLoop: true,
		WorkflowType:      "coding",
		WorkflowPhaseKind: "execution",
		WorkflowPhaseID:   "implementation",
	}, nil)
	if cfg.Model != "auto" {
		t.Fatalf("must keep auto, got %s", cfg.Model)
	}
	if cfg.WorkflowTypeHint != "coding" || cfg.PhaseKindHint != "execution" {
		t.Fatalf("workflow hints: type=%q phase=%q", cfg.WorkflowTypeHint, cfg.PhaseKindHint)
	}
	if cfg.WorkloadClassHint != "" {
		t.Fatalf("must not send P0 class, got %q", cfg.WorkloadClassHint)
	}
}

func TestBtwRouteTurn_HubManagedPersistsHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	h := &IMMessageHandler{}
	btw := NewBtwSubAgent(h, corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"}, nil, "u1")
	cb := &btwCallbacks{subagent: btw}
	cfg, d, ok := cb.RouteTurn("你好")
	if !ok || d.CostRouteApplied || cfg.Model != "auto" || cfg.TaskTypeHint == "" {
		t.Fatalf("btw hub hints: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
	if cb.GetLLMConfig().TaskTypeHint != cfg.TaskTypeHint {
		t.Fatalf("GetLLMConfig dropped btw hints: %+v", cb.GetLLMConfig())
	}
}

func TestVERouteTurn_HubManagedPersistsHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	cb := &veAgentCallbacks{
		app:    &App{},
		llmCfg: corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"},
	}
	cfg, d, ok := cb.RouteTurn("你好")
	if !ok || d.CostRouteApplied || cfg.Model != "auto" || cfg.TaskTypeHint == "" {
		t.Fatalf("ve hub hints: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
	if cb.GetLLMConfig().TaskTypeHint != cfg.TaskTypeHint {
		t.Fatalf("GetLLMConfig dropped ve hints: %+v", cb.GetLLMConfig())
	}
}

func TestLoopCycleRouteTurn_HubManagedPersistsHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	cb := &loopCycleCallbacks{parent: &guiLoopCommandCallbacks{
		handler: &IMMessageHandler{},
		llmCfg:  corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"},
	}}
	cfg, d, ok := cb.RouteTurn("fix the failing tests")
	if !ok || d.CostRouteApplied || cfg.Model != "auto" || cfg.TaskTypeHint == "" {
		t.Fatalf("loop hub hints: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
	if cb.GetLLMConfig().TaskTypeHint != cfg.TaskTypeHint {
		t.Fatalf("GetLLMConfig dropped loop hints: %+v", cb.GetLLMConfig())
	}
}

func TestApplyTurnModelRoute_ThirdPartyHasNoHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "off")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-4o"}
	cfg, _ := h.applyTurnModelRoute(primary, "你好", &LoopContext{
		Kind:              LoopKindChat,
		WorkflowAgentLoop: true,
		WorkflowType:      "coding",
	}, nil)
	if cfg.HubManaged || cfg.TaskTypeHint != "" || cfg.WorkflowTypeHint != "" {
		t.Fatalf("third-party must not carry Hub hints: %+v", cfg)
	}
}

func TestApplyTurnModelRoute_CostOffNoApply(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "off")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://x", Model: "m1"}
	_, d := h.applyTurnModelRoute(primary, "你好", nil, nil)
	if d.CostRouteMode != "off" || d.CostRouteApplied {
		t.Fatalf("%+v", d)
	}
	if d.CostTier == "" {
		t.Fatal("tier still computed when off")
	}
}

func TestApplyTurnModelRoute_HostImageNotesDoNotForceVision(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "off")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://x", Model: "m1"}
	text := "北京天气\n[Host note: selected image \"missing.jpg\" could not be read: file does not exist]"
	_, d := h.applyTurnModelRoute(primary, text, &LoopContext{Kind: LoopKindChat}, nil)
	if d.Task == string(cllm.TaskVision) {
		t.Fatalf("failed picker notes must not route weather as vision: %+v", d)
	}
	_, vision := h.applyTurnModelRoute(primary, "北京天气", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"},
	})
	if vision.Task != string(cllm.TaskVision) {
		t.Fatalf("loaded image bytes must still route vision: %+v", vision)
	}
	_, fileTurn := h.applyTurnModelRoute(primary, "请看这个", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "file", FileName: "notes.pdf", MimeType: "application/pdf", Data: "AAAA"},
	})
	if fileTurn.Task == string(cllm.TaskVision) {
		t.Fatalf("a PDF attachment must not route as vision: %+v", fileTurn)
	}
	if fileTurn.Task != string(cllm.TaskReasoning) {
		t.Fatalf("a PDF attachment should prefer a heavy model, got %+v", fileTurn)
	}
}
