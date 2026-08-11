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

func TestOrderProviderRoutes_AllowsDuplicateProviderWithDifferentModels(t *testing.T) {
	model := &DispatchModel{
		Name: "auto",
		ProviderRoutes: []DispatchProviderRoute{
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", Priority: 10, CreditMultiplier: 1, OriginalIndex: 0},
			{ProviderID: "deepseek", Model: "deepseek-v4-pro", Priority: 50, CreditMultiplier: 2, OriginalIndex: 1},
		},
	}
	result := OrderProviderRoutes(nil, model)
	if len(result) != 2 {
		t.Fatalf("expected 2 routes, got %v", result)
	}
	if result[0].ProviderID != "deepseek" || result[0].Model != "deepseek-v4-pro" {
		t.Fatalf("first route = %+v, want deepseek-v4-pro", result[0])
	}
	if result[1].Model != "deepseek-v4-flash" {
		t.Fatalf("second route = %+v, want deepseek-v4-flash", result[1])
	}
}

func TestOrderProviderRoutes_ZeroRoutePriorityDoesNotFallBackToProviderMap(t *testing.T) {
	model := &DispatchModel{
		Name: "auto",
		ProviderRoutes: []DispatchProviderRoute{
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", Priority: 0, CreditMultiplier: 1, OriginalIndex: 0},
			{ProviderID: "deepseek", Model: "deepseek-v4-pro", Priority: 10, CreditMultiplier: 1, OriginalIndex: 1},
		},
		ProviderPriorities: map[string]int{"deepseek": 99},
	}
	result := OrderProviderRoutes(nil, model)
	if len(result) != 2 {
		t.Fatalf("expected 2 routes, got %v", result)
	}
	if result[0].Model != "deepseek-v4-pro" || result[0].Priority != 10 {
		t.Fatalf("first route = %+v, want pro priority 10", result[0])
	}
	if result[1].Model != "deepseek-v4-flash" || result[1].Priority != 0 {
		t.Fatalf("second route = %+v, want flash priority 0", result[1])
	}
}

func TestOrderProviderRoutes_RouteModeCreditMultiplierUsesModelDefaultNotProviderMap(t *testing.T) {
	model := &DispatchModel{
		Name:             "auto",
		CreditMultiplier: 1.5,
		ProviderRoutes: []DispatchProviderRoute{
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", CreditMultiplier: 0, OriginalIndex: 0},
			{ProviderID: "deepseek", Model: "deepseek-v4-pro", CreditMultiplier: 2, OriginalIndex: 1},
		},
		ProviderCreditMultipliers: map[string]float64{"deepseek": 9},
	}
	result := OrderProviderRoutes(nil, model)
	if len(result) != 2 {
		t.Fatalf("expected 2 routes, got %v", result)
	}
	if result[0].Model != "deepseek-v4-flash" || result[0].CreditMultiplier != 1.5 {
		t.Fatalf("first route = %+v, want flash with model default multiplier 1.5", result[0])
	}
	if result[1].Model != "deepseek-v4-pro" || result[1].CreditMultiplier != 2 {
		t.Fatalf("second route = %+v, want pro with route multiplier 2", result[1])
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

func TestDetectCapabilityNeeds_RecognizesAllOfficeFormats(t *testing.T) {
	for _, format := range []string{"doc", "docx", "xls", "xlsx", "ppt", "pptx"} {
		t.Run(format, func(t *testing.T) {
			needs := DetectCapabilityNeeds(map[string]any{
				"messages": []any{map[string]any{"role": "user", "content": "summarize this report." + format}},
			})
			if needs["document"] == 0 {
				t.Fatalf("%q did not request document capability: %#v", format, needs)
			}
		})
	}
}
