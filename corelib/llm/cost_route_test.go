package llm

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func isolateCostRouteStats(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	ResetCostRouteStatsForTest()
}

func TestRecommendCostTier_ByTask(t *testing.T) {
	cases := []struct {
		task TaskType
		want CostTier
	}{
		{TaskFast, CostTierC0},
		{TaskIntent, CostTierC0},
		{TaskSummary, CostTierC1},
		{TaskDefault, CostTierC2},
		{TaskReasoning, CostTierC3},
		{TaskVision, CostTierC3},
	}
	for _, tc := range cases {
		if got := RecommendCostTier(tc.task, ClassifyHints{}); got != tc.want {
			t.Fatalf("task=%s got %s want %s", tc.task, got, tc.want)
		}
	}
}

func TestRecommendCostTier_HintsBump(t *testing.T) {
	if got := RecommendCostTier(TaskFast, ClassifyHints{ToolHeavy: true}); got != CostTierC2 {
		t.Fatalf("tool heavy fast → %s", got)
	}
	if got := RecommendCostTier(TaskFast, ClassifyHints{HasAttachments: true}); got != CostTierC3 {
		t.Fatalf("attachments → %s", got)
	}
	if got := RecommendCostTier(TaskSummary, ClassifyHints{ForceReasoning: true}); got != CostTierC3 {
		t.Fatalf("force reasoning → %s", got)
	}
}

func TestResolveCostRouteMode(t *testing.T) {
	t.Setenv(CostRouteEnvKey, "")
	if ResolveCostRouteMode() != CostRouteOff {
		t.Fatal("default off")
	}
	t.Setenv(CostRouteEnvKey, "shadow")
	if ResolveCostRouteMode() != CostRouteShadow {
		t.Fatal("shadow")
	}
	t.Setenv(CostRouteEnvKey, "on")
	if ResolveCostRouteMode() != CostRouteOn {
		t.Fatal("on")
	}
	if !CostRouteSurfaces(CostRouteShadow) || CostRouteSurfaces(CostRouteOff) {
		t.Fatal("surfaces")
	}
}

func TestDecideCostRoute_RecommendOnly(t *testing.T) {
	isolateCostRouteStats(t)
	t.Setenv(CostRouteEnvKey, "on")
	d := DecideCostRoute(TaskFast, ClassifyHints{}, "short greeting")
	if d.Tier != CostTierC0 {
		t.Fatalf("tier=%s", d.Tier)
	}
	if d.Applied {
		t.Fatal("DecideCostRoute alone never applies")
	}
	if d.Mode != CostRouteOn {
		t.Fatalf("mode=%s", d.Mode)
	}
}

func TestApplyCostTierConfig_OnUsesAuxForC0(t *testing.T) {
	isolateCostRouteStats(t)
	primary := corelib.MaclawLLMConfig{URL: "https://p", Key: "pk", Model: "primary-m"}
	aux := corelib.AuxiliaryLLMConfig{URL: "https://a", Key: "ak", Model: "aux-m"}
	cfg, applied, source, detail := ApplyCostTierConfig(nil, primary, aux, CostTierC0, CostRouteOn)
	if !applied || source != "aux" || cfg.Model != "aux-m" {
		t.Fatalf("cfg=%+v applied=%v source=%s detail=%s", cfg, applied, source, detail)
	}
	// shadow must not apply
	cfg2, applied2, _, _ := ApplyCostTierConfig(nil, primary, aux, CostTierC0, CostRouteShadow)
	if applied2 || cfg2.Model != primary.Model {
		t.Fatalf("shadow must keep primary: applied=%v model=%s", applied2, cfg2.Model)
	}
}

func TestApplyCostTierConfig_C3PrefersReasoningRoute(t *testing.T) {
	isolateCostRouteStats(t)
	primary := corelib.MaclawLLMConfig{URL: "https://p", Key: "pk", Model: "primary-m"}
	router := NewModelRouter(map[string]ModelRoute{
		"reasoning": {Model: "strong-m", URL: "https://s", Key: "sk"},
	})
	cfg, applied, source, detail := ApplyCostTierConfig(router, primary, corelib.AuxiliaryLLMConfig{}, CostTierC3, CostRouteOn)
	if !applied || source != "route" || cfg.Model != "strong-m" {
		t.Fatalf("cfg=%+v applied=%v source=%s detail=%s", cfg, applied, source, detail)
	}
}

func TestApplyCostTierConfig_C2StaysPrimary(t *testing.T) {
	isolateCostRouteStats(t)
	primary := corelib.MaclawLLMConfig{URL: "https://p", Key: "pk", Model: "primary-m"}
	cfg, applied, source, _ := ApplyCostTierConfig(nil, primary, corelib.AuxiliaryLLMConfig{}, CostTierC2, CostRouteOn)
	if !applied || source != "primary" || cfg.Model != "primary-m" {
		t.Fatalf("c2 should primary: model=%s applied=%v source=%s", cfg.Model, applied, source)
	}
}

func TestRecommendThinkingPolicy(t *testing.T) {
	if RecommendThinkingPolicy(CostTierC0) != ThinkingOff || RecommendThinkingPolicy(CostTierC1) != ThinkingOff {
		t.Fatal("c0/c1 should think off")
	}
	if RecommendThinkingPolicy(CostTierC2) != ThinkingLow {
		t.Fatal("c2 low")
	}
	if RecommendThinkingPolicy(CostTierC3) != ThinkingHigh {
		t.Fatal("c3 high")
	}
}

func TestApplyThinkingPolicy(t *testing.T) {
	cfg := ApplyThinkingPolicy(corelib.MaclawLLMConfig{Model: "m"}, ThinkingOff)
	if cfg.ThinkingMode != "disabled" || cfg.ReasoningEffort != "none" {
		t.Fatalf("%+v", cfg)
	}
	cfg = ApplyThinkingPolicy(corelib.MaclawLLMConfig{Model: "m"}, ThinkingHigh)
	if cfg.ThinkingMode != "enabled" || cfg.ReasoningEffort != "high" {
		t.Fatalf("%+v", cfg)
	}
}

func TestDecideCostRoute_IncludesThinking(t *testing.T) {
	isolateCostRouteStats(t)
	t.Setenv(CostRouteEnvKey, "shadow")
	d := DecideCostRoute(TaskFast, ClassifyHints{}, "")
	if d.Thinking != ThinkingOff {
		t.Fatalf("think=%s", d.Thinking)
	}
	d = DecideCostRoute(TaskReasoning, ClassifyHints{}, "")
	if d.Thinking != ThinkingHigh {
		t.Fatalf("think=%s", d.Thinking)
	}
}
