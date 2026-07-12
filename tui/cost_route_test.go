package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func TestTUIApp_RouteTurn_CostRouteOnUsesAux(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	llm.ResetCostRouteStatsForTest()
	t.Setenv(llm.CostRouteEnvKey, "on")

	app := &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{URL: "http://p", Model: "primary-m", Key: "k"},
		appConfig: corelib.AppConfig{
			AuxiliaryLLM: corelib.AuxiliaryLLMConfig{URL: "http://a", Model: "aux-m", Key: "k"},
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
