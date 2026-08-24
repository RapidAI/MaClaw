package llmservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestServiceGroupAccessPolicyPersists(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})

	if err := svc.SaveRegistry(ctx, &Registry{
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw_official_group",
			Name:         "MaClaw Official Group",
			AccessPolicy: AccessPolicyGrantRequired,
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if got := reg.ServiceGroups[0].AccessPolicy; got != AccessPolicyGrantRequired {
		t.Fatalf("access policy = %q, want %q", got, AccessPolicyGrantRequired)
	}

	group := reg.ServiceGroups[0]
	group.AccessPolicy = AccessPolicyFree
	if err := svc.UpdateServiceGroup(ctx, group); err != nil {
		t.Fatalf("update service group: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if got := reg.ServiceGroups[0].AccessPolicy; got != AccessPolicyFree {
		t.Fatalf("updated access policy = %q, want %q", got, AccessPolicyFree)
	}
}

func TestServiceGroupAccessPolicyDefaultsToFree(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})

	if err := svc.SaveRegistry(ctx, &Registry{
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", AccessPolicy: "bad"}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if got := reg.ServiceGroups[0].AccessPolicy; got != AccessPolicyFree {
		t.Fatalf("default access policy = %q, want %q", got, AccessPolicyFree)
	}
}

func TestServiceGroupReservedExternalComputePermissionID(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})

	err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID:      ExternalComputePermissionServiceGroupID,
		Name:    "reserved",
		AgentID: DefaultComputeAgentID,
	})
	if err == nil {
		t.Fatalf("AddServiceGroup with reserved id succeeded")
	}

	err = svc.UpdateServiceGroup(ctx, llmpool.ServiceGroup{
		ID:      ExternalComputePermissionServiceGroupID,
		Name:    "reserved",
		AgentID: DefaultComputeAgentID,
	})
	if err == nil {
		t.Fatalf("UpdateServiceGroup with reserved id succeeded")
	}
}

func TestServiceGroupAllowsSameProviderDifferentModels(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})

	err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID:      "g1",
		Name:    "G1",
		AgentID: DefaultComputeAgentID,
		Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", Priority: 10},
			{ProviderID: "deepseek", Model: "deepseek-v4-pro", Priority: 50},
		}}},
	})
	if err != nil {
		t.Fatalf("AddServiceGroup should allow same provider with different models: %v", err)
	}
}

func TestServiceGroupRejectsDuplicateProviderModelRoute(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})

	err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID:      "g1",
		Name:    "G1",
		AgentID: DefaultComputeAgentID,
		Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", Priority: 10},
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", Priority: 50},
		}}},
	})
	if err == nil {
		t.Fatal("AddServiceGroup accepted duplicate provider/model route")
	}
	if !contains(err.Error(), "duplicate provider route") {
		t.Fatalf("error = %v, want duplicate provider route", err)
	}
}

func TestServiceGroupRejectsDefaultAndExplicitSingleProviderModelDuplicate(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", Models: []string{"deepseek-v4-flash"}}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID:      "g1",
		Name:    "G1",
		AgentID: DefaultComputeAgentID,
		Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
			{ProviderID: "deepseek"},
			{ProviderID: "deepseek", Model: "deepseek-v4-flash"},
		}}},
	})
	if err == nil {
		t.Fatal("AddServiceGroup accepted default and explicit single-model duplicate")
	}
	if !contains(err.Error(), "duplicate provider route") {
		t.Fatalf("error = %v, want duplicate provider route", err)
	}
}

func TestServiceGroupRejectsDuplicateLegacyProviderIDs(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})

	err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID:      "g1",
		Name:    "G1",
		AgentID: DefaultComputeAgentID,
		Models:  []llmpool.ModelConfig{{Name: "auto", ProviderIDs: []string{"deepseek", "deepseek"}}},
	})
	if err == nil {
		t.Fatal("AddServiceGroup accepted duplicate legacy provider_ids")
	}
	if !contains(err.Error(), "duplicate provider route") {
		t.Fatalf("error = %v, want duplicate provider route", err)
	}
}

func TestRegistryNormalizesLegacyProviderIDsToProviderConfigs(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(ctx, &Registry{
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:     "g1",
			Name:   "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderIDs: []string{" p1 ", "p2", "p1"}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	model := reg.ServiceGroups[0].Models[0]
	if len(model.ProviderConfigs) != 3 {
		t.Fatalf("provider_configs = %+v, want 3 legacy routes preserved", model.ProviderConfigs)
	}
	if model.ProviderConfigs[0].ProviderID != "p1" || model.ProviderConfigs[1].ProviderID != "p2" || model.ProviderConfigs[2].ProviderID != "p1" {
		t.Fatalf("provider_configs = %+v, want trimmed legacy order", model.ProviderConfigs)
	}
	if len(model.ProviderIDs) != 2 || model.ProviderIDs[0] != "p1" || model.ProviderIDs[1] != "p2" {
		t.Fatalf("provider_ids = %+v, want unique provider ids", model.ProviderIDs)
	}
}

func TestRequiresGrantAccessPolicyMatchesProxyNormalization(t *testing.T) {
	if !RequiresGrantAccessPolicy(AccessPolicyGrantRequired) || !RequiresGrantAccessPolicy("GRANT_REQUIRED") {
		t.Fatal("grant_required should require grant")
	}
	if RequiresGrantAccessPolicy("") || RequiresGrantAccessPolicy(AccessPolicyFree) || RequiresGrantAccessPolicy("unknown") {
		t.Fatal("empty, free, and unknown policies should not require grant")
	}
}
