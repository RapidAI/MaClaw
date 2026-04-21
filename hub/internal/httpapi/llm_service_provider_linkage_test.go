package httpapi

import (
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestCollectLLMServiceProviderReferenceIssues(t *testing.T) {
	serviceReg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a", "provider-missing"},
			}},
		}},
	}
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a"}}}
	issues := collectLLMServiceProviderReferenceIssues(serviceReg, providerReg)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %#v", issues)
	}
	if issues[0] != `service group "coding-basic" model "auto" references unknown providers: provider-missing` {
		t.Fatalf("unexpected issue: %q", issues[0])
	}
}

func TestFilterAuthorizedModelsByProviderRegistry(t *testing.T) {
	status := &llmservice.ServiceStatus{
		Active:          true,
		SkipLLMConfig:   true,
		AvailableModels: []string{"auto", "doc"},
		DefaultModel:    "auto",
	}
	models := []llmservice.AuthorizedModel{
		{
			Name:             "auto",
			ProviderIDs:      []string{"provider-a", "provider-missing"},
			ServiceGroupIDs:  []string{"group-a", "group-b"},
			CreditMultiplier: 2,
			ProviderServiceGroups: map[string][]string{
				"provider-a":       {"group-a"},
				"provider-missing": {"group-b"},
			},
			ProviderCreditMultipliers: map[string]float64{
				"provider-a":       1,
				"provider-missing": 2,
			},
		},
		{
			Name:             "doc",
			ProviderIDs:      []string{"provider-missing"},
			ServiceGroupIDs:  []string{"group-doc"},
			CreditMultiplier: 3,
			ProviderServiceGroups: map[string][]string{
				"provider-missing": {"group-doc"},
			},
			ProviderCreditMultipliers: map[string]float64{
				"provider-missing": 3,
			},
		},
	}
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a"}}}
	filteredStatus, filteredModels := filterAuthorizedModelsByProviderRegistry(status, models, providerReg)
	if len(filteredModels) != 1 {
		t.Fatalf("expected 1 model after filtering, got %#v", filteredModels)
	}
	if filteredModels[0].Name != "auto" {
		t.Fatalf("expected auto to remain, got %#v", filteredModels[0])
	}
	if len(filteredModels[0].ProviderIDs) != 1 || filteredModels[0].ProviderIDs[0] != "provider-a" {
		t.Fatalf("provider ids = %#v", filteredModels[0].ProviderIDs)
	}
	if len(filteredModels[0].ServiceGroupIDs) != 1 || filteredModels[0].ServiceGroupIDs[0] != "group-a" {
		t.Fatalf("service groups = %#v", filteredModels[0].ServiceGroupIDs)
	}
	if filteredModels[0].CreditMultiplier != 1 {
		t.Fatalf("credit multiplier = %v, want 1", filteredModels[0].CreditMultiplier)
	}
	if !filteredStatus.Active || !filteredStatus.SkipLLMConfig {
		t.Fatalf("filtered status should stay active: %#v", filteredStatus)
	}
	if len(filteredStatus.AvailableModels) != 1 || filteredStatus.AvailableModels[0] != "auto" {
		t.Fatalf("available models = %#v", filteredStatus.AvailableModels)
	}
}
