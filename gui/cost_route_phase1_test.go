package main

import (
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
