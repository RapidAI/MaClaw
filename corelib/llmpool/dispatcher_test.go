package llmpool

import (
	"testing"
)

func TestOrderProviders_NilModel(t *testing.T) {
	result := OrderProviders(nil, nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestOrderProviders_SingleProvider(t *testing.T) {
	model := &DispatchModel{
		Name:        "gpt-4",
		ProviderIDs: []string{"openai"},
	}
	result := OrderProviders(nil, model)
	if len(result) != 1 || result[0] != "openai" {
		t.Fatalf("expected [openai], got %v", result)
	}
}

func TestOrderProviders_CapabilityMatching(t *testing.T) {
	model := &DispatchModel{
		Name:        "auto",
		ProviderIDs: []string{"basic", "tools_capable"},
		ProviderCapabilityTags: map[string][]string{
			"basic":         {},
			"tools_capable": {"tools"},
		},
	}
	body := map[string]any{
		"tools": []any{map[string]any{"type": "function"}},
	}
	result := OrderProviders(body, model)
	if len(result) != 2 {
		t.Fatalf("expected 2 providers, got %v", result)
	}
	if result[0] != "tools_capable" {
		t.Fatalf("expected tools_capable first, got %v", result)
	}
}

func TestOrderProviders_PriorityBreaksTie(t *testing.T) {
	model := &DispatchModel{
		Name:        "auto",
		ProviderIDs: []string{"low", "high"},
		ProviderPriorities: map[string]int{
			"low":  1,
			"high": 10,
		},
	}
	result := OrderProviders(nil, model)
	if len(result) != 2 || result[0] != "high" {
		t.Fatalf("expected high first, got %v", result)
	}
}

func TestOrderProviders_ResolutionTierPrefersCheaper(t *testing.T) {
	model := &DispatchModel{
		Name:        "auto",
		ProviderIDs: []string{"expensive", "cheap"},
		ProviderResolutionTiers: map[string]int{
			"expensive": 3,
			"cheap":     1,
		},
	}
	result := OrderProviders(nil, model)
	if len(result) != 2 || result[0] != "cheap" {
		t.Fatalf("expected cheap first, got %v", result)
	}
}

func TestOrderProviders_CreditMultiplierPrefersCheaper(t *testing.T) {
	model := &DispatchModel{
		Name:        "auto",
		ProviderIDs: []string{"double_cost", "normal_cost"},
		ProviderCreditMultipliers: map[string]float64{
			"double_cost": 2.0,
			"normal_cost": 1.0,
		},
	}
	result := OrderProviders(nil, model)
	if len(result) != 2 || result[0] != "normal_cost" {
		t.Fatalf("expected normal_cost first, got %v", result)
	}
}

func TestDetectCapabilityNeeds_Tools(t *testing.T) {
	body := map[string]any{
		"tools": []any{map[string]any{"type": "function"}},
	}
	needs := DetectCapabilityNeeds(body)
	if needs["tools"] == 0 {
		t.Fatal("expected tools capability need")
	}
}

func TestDetectCapabilityNeeds_Empty(t *testing.T) {
	needs := DetectCapabilityNeeds(nil)
	if len(needs) != 0 {
		t.Fatalf("expected empty needs, got %v", needs)
	}
}
