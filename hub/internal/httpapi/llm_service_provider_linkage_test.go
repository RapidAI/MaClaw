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

func TestCollectLLMServiceProviderReferenceIssues_SkipsBuiltinProvider(t *testing.T) {
	// maclaw_official is a built-in virtual provider that routes through HubCenter.
	// It should NOT trigger "references unknown providers" even when it's absent
	// from the user-configured provider registry.
	serviceReg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:   llmservice.MaClawOfficialServiceGroupID,
			Name: llmservice.MaClawOfficialServiceGroupName,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
			}},
		}},
	}
	// Provider registry does NOT contain maclaw_official — that's normal.
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "deepseek"}}}
	issues := collectLLMServiceProviderReferenceIssues(serviceReg, providerReg)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for built-in provider, got %#v", issues)
	}

	// Case-insensitive: provider ID stored with different casing should still be recognized.
	serviceReg2 := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "test-group",
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"MaClaw_Official"},
			}},
		}},
	}
	issues2 := collectLLMServiceProviderReferenceIssues(serviceReg2, providerReg)
	if len(issues2) != 0 {
		t.Fatalf("expected 0 issues for case-insensitive built-in provider, got %#v", issues2)
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

func TestFilterAuthorizedModels_BuiltinProviderNotDropped(t *testing.T) {
	// maclaw_official should be retained even when it's not in the provider registry.
	status := &llmservice.ServiceStatus{
		Active:          true,
		SkipLLMConfig:   true,
		AvailableModels: []string{"auto"},
		DefaultModel:    "auto",
	}
	models := []llmservice.AuthorizedModel{{
		Name:            "auto",
		ProviderIDs:     []string{llmservice.MaClawOfficialProviderID},
		ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID},
		ProviderServiceGroups: map[string][]string{
			llmservice.MaClawOfficialProviderID: {llmservice.MaClawOfficialServiceGroupID},
		},
		ProviderCreditMultipliers: map[string]float64{
			llmservice.MaClawOfficialProviderID: 1,
		},
		CreditMultiplier: 1,
	}}
	// Provider registry has NO maclaw_official entry (normal scenario).
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "deepseek"}}}
	filteredStatus, filteredModels := filterAuthorizedModelsByProviderRegistry(status, models, providerReg)
	if len(filteredModels) != 1 {
		t.Fatalf("expected built-in provider model to be retained, got %d models: %#v", len(filteredModels), filteredModels)
	}
	if filteredModels[0].Name != "auto" {
		t.Fatalf("expected auto, got %q", filteredModels[0].Name)
	}
	if !filteredStatus.Active {
		t.Fatalf("status should remain active with built-in provider")
	}
}

func TestFilterAuthorizedModelsByProviderRegistryPreservesBillingInactiveStatus(t *testing.T) {
	status := &llmservice.ServiceStatus{
		Active:          false,
		SkipLLMConfig:   false,
		AvailableModels: []string{"auto"},
		DefaultModel:    "auto",
		CreditGrants: []llmservice.ActiveGrant{{
			ServiceGroupID: "group-a",
			Status:         "period_limited",
			RetryAfterAt:   "2026-05-05T10:00:00Z",
		}},
	}
	models := []llmservice.AuthorizedModel{{
		Name:            "auto",
		ProviderIDs:     []string{"provider-a"},
		ServiceGroupIDs: []string{"group-a"},
		ProviderServiceGroups: map[string][]string{
			"provider-a": {"group-a"},
		},
	}}
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a"}}}

	filteredStatus, filteredModels := filterAuthorizedModelsByProviderRegistry(status, models, providerReg)
	if len(filteredModels) != 1 {
		t.Fatalf("expected model route to remain explainable, got %#v", filteredModels)
	}
	if filteredStatus.Active || filteredStatus.SkipLLMConfig {
		t.Fatalf("provider filtering must not re-activate period-limited status: %#v", filteredStatus)
	}
	if len(filteredStatus.AvailableModels) != 1 || filteredStatus.AvailableModels[0] != "auto" {
		t.Fatalf("available models should remain visible for diagnostics, got %#v", filteredStatus.AvailableModels)
	}
}

func TestExplainFilteredServiceStatusIssues(t *testing.T) {
	providerReg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a"}}}
	original := &llmservice.ServiceStatus{
		ServiceGroupIDs: []string{"group-a"},
		AuthorizedModels: []llmservice.AuthorizedModel{{
			Name:        "auto",
			ProviderIDs: []string{"provider-missing"},
		}},
		ActiveGrants: []llmservice.ActiveGrant{{ServiceGroupID: "group-a"}},
	}
	reasons := explainFilteredServiceStatusIssues(original, nil, providerReg)
	if !containsString(reasons, "authorized service groups exist, but none route to a live LLM provider") {
		t.Fatalf("expected live-provider reason in %#v", reasons)
	}
	if !containsString(reasons, "active grants exist, but they currently expose no live model routes") {
		t.Fatalf("expected active-grant reason in %#v", reasons)
	}

	periodLimited := &llmservice.ServiceStatus{CreditGrants: []llmservice.ActiveGrant{{
		ServiceGroupID: "group-a",
		Status:         "period_limited",
		RetryAfterAt:   "2026-05-05T10:00:00Z",
	}}}
	reasons = explainFilteredServiceStatusIssues(periodLimited, nil, providerReg)
	if !containsString(reasons, "current period credit limit is exhausted; retry after 2026-05-05T10:00:00Z") {
		t.Fatalf("expected period-limit reason in %#v", reasons)
	}
	if containsString(reasons, "no service-group entitlement is active for this user") {
		t.Fatalf("period limit should not be reported as missing entitlement: %#v", reasons)
	}

	noEntitlement := &llmservice.ServiceStatus{}
	reasons = explainFilteredServiceStatusIssues(noEntitlement, nil, nil)
	if !containsString(reasons, "no service-group entitlement is active for this user") {
		t.Fatalf("expected entitlement reason in %#v", reasons)
	}
	if !containsString(reasons, "no LLM providers are currently configured") {
		t.Fatalf("expected provider reason in %#v", reasons)
	}
}

func TestValidateLLMServiceGroupReferences(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups:          []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		GlobalServiceGroupIDs:       []string{"missing-global"},
		GroupBindings:               []llmservice.GroupBinding{{GroupID: "engineering", ServiceGroupIDs: []string{"coding-basic", "missing-group"}}},
		UserBindings:                []llmservice.UserBinding{{Email: "user@example.com", ServiceGroupIDs: []string{"missing-user-group"}}},
		DefaultNewUserServiceGroups: []string{"coding-basic", "missing-default"},
		Cards:                       []llmservice.RechargeCard{{ID: "card_1", Label: "April", ServiceGroupIDs: []string{"missing-card"}}},
		Grants:                      []llmservice.Grant{{ID: "grant_1", Email: "user@example.com", ServiceGroupID: "missing-grant"}},
	}
	reg.Normalize()
	issues := validateLLMServiceGroupReferences(reg)
	want := []string{
		"global service groups references unknown service group: missing-global",
		"security group binding \"engineering\" references unknown service group: missing-group",
		"user binding \"user@example.com\" references unknown service group: missing-user-group",
		"new-user default grants references unknown service group: missing-default",
		"service exchange card \"April\" references unknown service group: missing-card",
		"grant \"grant_1\" references unknown service group: missing-grant",
	}
	for _, expected := range want {
		if !containsString(issues, expected) {
			t.Fatalf("expected issue %q in %#v", expected, issues)
		}
	}
}

func TestValidateLLMServiceSecurityGroupReferences(t *testing.T) {
	reg := &llmservice.Registry{
		GroupBindings: []llmservice.GroupBinding{
			{GroupID: "engineering", ServiceGroupIDs: []string{"coding-basic"}},
			{GroupID: "missing-security-group", ServiceGroupIDs: []string{"coding-pro"}},
		},
	}
	issues := validateLLMServiceSecurityGroupReferences(reg, map[string]struct{}{
		"engineering": {},
		"root":        {},
	})
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %#v", issues)
	}
	if issues[0] != "service group binding references unknown security group: missing-security-group" {
		t.Fatalf("unexpected issue: %q", issues[0])
	}

	if got := validateLLMServiceSecurityGroupReferences(reg, nil); len(got) != 0 {
		t.Fatalf("expected no issues without known security groups, got %#v", got)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
