package llmservice

import "testing"

func TestOrderProvidersForRequestUsesProviderScopedParams(t *testing.T) {
	model := &AuthorizedModel{
		Name:             "auto",
		ProviderIDs:      []string{"provider-doc", "provider-tools"},
		CapabilityTags:   []string{"document", "tools"},
		Priority:         10,
		ResolutionTier:   1,
		CreditMultiplier: 1,
		ProviderCapabilityTags: map[string][]string{
			"provider-doc":   {"document"},
			"provider-tools": {"tools"},
		},
		ProviderPriorities: map[string]int{
			"provider-doc":   20,
			"provider-tools": 60,
		},
		ProviderResolutionTiers: map[string]int{
			"provider-doc":   2,
			"provider-tools": 1,
		},
		ProviderCreditMultipliers: map[string]float64{
			"provider-doc":   1.2,
			"provider-tools": 1,
		},
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Use tools to search and fetch the answer."}},
		"tools":    []any{map[string]any{"type": "function"}},
	}
	ordered := OrderProvidersForRequest(body, model)
	if len(ordered) != 2 {
		t.Fatalf("ordered providers = %#v", ordered)
	}
	if ordered[0] != "provider-tools" || ordered[1] != "provider-doc" {
		t.Fatalf("unexpected provider order: %#v", ordered)
	}
}
