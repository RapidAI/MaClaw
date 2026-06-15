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
