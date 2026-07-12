package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestTurnUsageAdd(t *testing.T) {
	var u TurnUsage
	u.Add(TurnUsage{Model: "a", InputTokens: 10, OutputTokens: 2, Requests: 1})
	u.Add(TurnUsage{Model: "b", Provider: "p", InputTokens: 5, CachedTokens: 3, EstCostRMB: 0.1, Requests: 1})
	if u.Model != "a" {
		t.Fatalf("model should keep first non-empty, got %q", u.Model)
	}
	if u.Provider != "p" {
		t.Fatalf("provider=%q", u.Provider)
	}
	if u.InputTokens != 15 || u.OutputTokens != 2 || u.CachedTokens != 3 || u.Requests != 2 {
		t.Fatalf("usage=%+v", u)
	}
	if u.TotalTokens() != 17 {
		t.Fatalf("total=%d", u.TotalTokens())
	}
	if u.EstCostRMB != 0.1 {
		t.Fatalf("cost=%v", u.EstCostRMB)
	}
}

func TestTurnUsageFromLLM(t *testing.T) {
	u := TurnUsageFromLLM(corelib.MaclawLLMConfig{Model: "m1", ProviderName: "p1"}, &llm.Usage{
		PromptTokens:      1000,
		CompletionTokens:  200,
		CachedInputTokens: 50,
	})
	if u.Model != "m1" || u.Provider != "p1" {
		t.Fatalf("labels=%+v", u)
	}
	if u.InputTokens != 1000 || u.OutputTokens != 200 || u.CachedTokens != 50 || u.Requests != 1 {
		t.Fatalf("tokens=%+v", u)
	}
	if u.EstCostRMB <= 0 {
		t.Fatalf("expected default cost estimate, got %v", u.EstCostRMB)
	}
	if TurnUsageFromLLM(corelib.MaclawLLMConfig{}, nil).Requests != 0 {
		t.Fatal("nil usage should be empty")
	}
}

func TestTurnUsageSummary(t *testing.T) {
	if (TurnUsage{}).Summary() != "" {
		t.Fatal("empty summary")
	}
	s := TurnUsage{
		Model:        "m1",
		InputTokens:  1000,
		OutputTokens: 200,
		CachedTokens: 50,
		Requests:     2,
		EstCostRMB:   0.0123,
	}.Summary()
	for _, part := range []string{"in=1000", "out=200", "total=1200", "cache_read=50", "req=2", "~¥0.0123", "model m1"} {
		if !strings.Contains(s, part) {
			t.Fatalf("summary missing %q: %q", part, s)
		}
	}
}

func TestFormatTurnMeta(t *testing.T) {
	if FormatTurnMeta(RouteDecision{}, TurnUsage{}) != "" {
		t.Fatal("empty")
	}
	s := FormatTurnMeta(
		RouteDecision{TaskType: "fast", Source: "aux", Model: "m-flash"},
		TurnUsage{InputTokens: 1200, OutputTokens: 340, EstCostRMB: 0.0123},
	)
	for _, part := range []string{"fast", "aux", "m-flash", "in=1.2k", "out=340", "~¥0.0123"} {
		if !strings.Contains(s, part) {
			t.Fatalf("meta missing %q: %q", part, s)
		}
	}
	withPrompt := FormatTurnMetaWithPrompt(
		RouteDecision{TaskType: "fast", Source: "aux", Model: "m-flash"},
		TurnUsage{InputTokens: 100, OutputTokens: 20},
		string(PromptProfileLight),
	)
	if !strings.Contains(withPrompt, "prompt=light") {
		t.Fatalf("expected prompt=light in %q", withPrompt)
	}
	withSaved := FormatTurnMetaOpts(TurnMetaOptions{
		Route:             RouteDecision{TaskType: "fast", Source: "aux", Model: "m-flash"},
		Usage:             TurnUsage{InputTokens: 100, OutputTokens: 20},
		PromptProfile:     string(PromptProfileLight),
		PromptSavedTokens: 3800,
	})
	if !strings.Contains(withSaved, "prompt=light(-3.8k)") {
		t.Fatalf("expected savings tag in %q", withSaved)
	}
	upgraded := FormatTurnMetaOpts(TurnMetaOptions{
		Route:          RouteDecision{TaskType: "reasoning", Source: "primary", Model: "m1"},
		Usage:          TurnUsage{InputTokens: 2000, OutputTokens: 400},
		PromptProfile:  string(PromptProfileFull),
		PromptUpgraded: true,
	})
	if !strings.Contains(upgraded, "prompt=full(upgraded)") {
		t.Fatalf("expected upgraded tag in %q", upgraded)
	}
	// Upgraded wins over light savings label.
	upgradedFromLight := FormatTurnMetaOpts(TurnMetaOptions{
		Route:             RouteDecision{TaskType: "fast", Source: "aux", Model: "m-flash"},
		Usage:             TurnUsage{InputTokens: 100, OutputTokens: 20},
		PromptProfile:     string(PromptProfileLight),
		PromptSavedTokens: 3800,
		PromptUpgraded:    true,
	})
	if !strings.Contains(upgradedFromLight, "prompt=full(upgraded)") || strings.Contains(upgradedFromLight, "prompt=light") {
		t.Fatalf("upgraded should replace light tag: %q", upgradedFromLight)
	}
	ab := FormatTurnMetaOpts(TurnMetaOptions{
		Route:          RouteDecision{TaskType: "fast", Source: "aux", Model: "m-flash"},
		Usage:          TurnUsage{InputTokens: 100, OutputTokens: 20},
		PromptProfile:  string(PromptProfileFull),
		PromptABSample: true,
	})
	if !strings.Contains(ab, "prompt=full(ab)") {
		t.Fatalf("expected ab tag in %q", ab)
	}
	// Upgraded wins over A/B.
	both := FormatTurnMetaOpts(TurnMetaOptions{
		Route:          RouteDecision{TaskType: "fast", Source: "aux", Model: "m1"},
		Usage:          TurnUsage{InputTokens: 10, OutputTokens: 5},
		PromptUpgraded: true,
		PromptABSample: true,
	})
	if !strings.Contains(both, "prompt=full(upgraded)") || strings.Contains(both, "prompt=full(ab)") {
		t.Fatalf("upgraded should win over ab: %q", both)
	}
	soft := FormatTurnMetaOpts(TurnMetaOptions{
		Route:          RouteDecision{TaskType: "fast", Source: "aux", Model: "m1"},
		Usage:          TurnUsage{InputTokens: 10, OutputTokens: 5},
		PromptProfile:  string(PromptProfileFull),
		PromptSoftFull: true,
	})
	if !strings.Contains(soft, "prompt=full(soft)") {
		t.Fatalf("expected soft tag in %q", soft)
	}
	// Cost-route Phase 1: shadow tier surfaces on chip; off mode omits.
	withTier := FormatTurnMetaOpts(TurnMetaOptions{
		Route: RouteDecision{
			TaskType:      "fast",
			Source:        "aux",
			Model:         "m-flash",
			CostTier:      "c0",
			CostRouteMode: "shadow",
		},
		Usage: TurnUsage{InputTokens: 10, OutputTokens: 5},
	})
	if !strings.Contains(withTier, "tier=c0(shadow)") {
		t.Fatalf("expected tier shadow tag in %q", withTier)
	}
	offTier := FormatTurnMetaOpts(TurnMetaOptions{
		Route: RouteDecision{
			TaskType:      "fast",
			Model:         "m1",
			CostTier:      "c0",
			CostRouteMode: "off",
		},
		Usage: TurnUsage{InputTokens: 10, OutputTokens: 5},
	})
	if strings.Contains(offTier, "tier=") {
		t.Fatalf("off mode should omit tier: %q", offTier)
	}
	appliedTier := FormatTurnMetaOpts(TurnMetaOptions{
		Route: RouteDecision{
			TaskType:         "fast",
			Model:            "aux-m",
			CostTier:         "c0",
			CostRouteMode:    "on",
			CostRouteApplied: true,
			ThinkingPolicy:   "off",
		},
		Usage: TurnUsage{InputTokens: 10, OutputTokens: 5},
	})
	if !strings.Contains(appliedTier, "tier=c0") || strings.Contains(appliedTier, "tier=c0(shadow)") {
		t.Fatalf("applied on should show bare tier: %q", appliedTier)
	}
	if !strings.Contains(appliedTier, "think=off") || strings.Contains(appliedTier, "think=off(shadow)") {
		t.Fatalf("applied think tag: %q", appliedTier)
	}
	shadowThink := FormatTurnMetaOpts(TurnMetaOptions{
		Route: RouteDecision{
			TaskType:       "fast",
			Model:          "m1",
			CostTier:       "c0",
			CostRouteMode:  "shadow",
			ThinkingPolicy: "off",
		},
		Usage: TurnUsage{InputTokens: 10, OutputTokens: 5},
	})
	if !strings.Contains(shadowThink, "think=off(shadow)") {
		t.Fatalf("shadow think: %q", shadowThink)
	}
}

func TestFilterToolDefsForLightTurn(t *testing.T) {
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "bash"}},
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "read_file"}},
		{"type": "function", "function": map[string]interface{}{"name": "web_fetch"}},
	}
	got := FilterToolDefsForLightTurn(defs)
	if len(got) != 2 {
		t.Fatalf("got %d tools: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, d := range got {
		names[toolDefName(d)] = true
	}
	if !names["web_search"] || !names["web_fetch"] {
		t.Fatalf("names=%v", names)
	}
}
