package llmservice

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func newProviderPruneTestService(t *testing.T) *Service {
	t.Helper()
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "prov_a", Name: "Provider A", APIURL: "https://a.example.com"},
			{ID: "prov_b", Name: "Provider B", APIURL: "https://b.example.com"},
			{ID: "prov_unused", Name: "Provider Unused", APIURL: "https://u.example.com"},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:   "group_one",
				Name: "Group One",
				Models: []llmpool.ModelConfig{{
					Name:            "model-x",
					ProviderIDs:     []string{"prov_a", "prov_b"},
					ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "prov_a"}, {ProviderID: "prov_b"}},
				}},
			},
			{
				ID:   "group_two",
				Name: "Group Two",
				Models: []llmpool.ModelConfig{{
					Name:            "model-y",
					ProviderIDs:     []string{"prov_a"},
					ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "prov_a"}},
				}},
			},
			{
				ID:   "group_unrelated",
				Name: "Group Unrelated",
				Models: []llmpool.ModelConfig{{
					Name:            "model-z",
					ProviderIDs:     []string{"prov_b"},
					ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "prov_b"}},
				}},
			},
		},
	}
	if err := svc.SaveRegistry(context.Background(), reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	return svc
}

func TestDeleteProviderInUseWithoutPrune(t *testing.T) {
	svc := newProviderPruneTestService(t)

	pruned, err := svc.DeleteProvider(context.Background(), "prov_a", false)
	if !errors.Is(err, ErrProviderInUse) {
		t.Fatalf("err = %v, want ErrProviderInUse", err)
	}
	if len(pruned) != 2 || !slices.Contains(pruned, "Group One") || !slices.Contains(pruned, "Group Two") {
		t.Fatalf("pruned groups = %v, want [Group One Group Two]", pruned)
	}

	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.Providers) != 3 {
		t.Fatalf("provider must not be deleted on ErrProviderInUse: %#v", reg.Providers)
	}
}

func TestDeleteProviderPruneRemovesReferences(t *testing.T) {
	svc := newProviderPruneTestService(t)

	pruned, err := svc.DeleteProvider(context.Background(), "prov_a", true)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(pruned) != 2 || !slices.Contains(pruned, "Group One") || !slices.Contains(pruned, "Group Two") {
		t.Fatalf("pruned groups = %v, want [Group One Group Two]", pruned)
	}

	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.Providers) != 2 || providerIDPresent(reg.Providers, "prov_a") {
		t.Fatalf("providers = %#v, want prov_b and prov_unused", reg.Providers)
	}
	if len(reg.ServiceGroups) != 3 {
		t.Fatalf("service groups must all be kept: %#v", reg.ServiceGroups)
	}
	byID := map[string]llmpool.ServiceGroup{}
	for _, g := range reg.ServiceGroups {
		byID[g.ID] = g
	}
	one := byID["group_one"].Models[0]
	if len(one.ProviderIDs) != 1 || one.ProviderIDs[0] != "prov_b" {
		t.Fatalf("group_one provider_ids = %v, want [prov_b]", one.ProviderIDs)
	}
	if len(one.ProviderConfigs) != 1 || one.ProviderConfigs[0].ProviderID != "prov_b" {
		t.Fatalf("group_one provider_configs = %#v, want only prov_b", one.ProviderConfigs)
	}
	two := byID["group_two"].Models[0]
	if len(two.ProviderIDs) != 0 || len(two.ProviderConfigs) != 0 {
		t.Fatalf("group_two model must be left empty for admin to fix: %#v", two)
	}
	unrelated := byID["group_unrelated"].Models[0]
	if len(unrelated.ProviderIDs) != 1 || unrelated.ProviderIDs[0] != "prov_b" {
		t.Fatalf("unrelated group must be untouched: %#v", unrelated)
	}
}

func TestDeleteProviderWithoutReferences(t *testing.T) {
	svc := newProviderPruneTestService(t)

	pruned, err := svc.DeleteProvider(context.Background(), "prov_unused", false)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned groups = %v, want empty", pruned)
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.Providers) != 2 {
		t.Fatalf("providers = %#v, want prov_a and prov_b", reg.Providers)
	}
}

func TestDeleteProviderNotFoundStillReported(t *testing.T) {
	svc := newProviderPruneTestService(t)

	for _, prune := range []bool{false, true} {
		if _, err := svc.DeleteProvider(context.Background(), "prov_missing", prune); !errors.Is(err, ErrProviderNotFound) {
			t.Fatalf("prune=%v err = %v, want ErrProviderNotFound", prune, err)
		}
	}
}

func providerIDPresent(providers []llmpool.ProviderConfig, id string) bool {
	for _, p := range providers {
		if p.ID == id {
			return true
		}
	}
	return false
}

func TestProviderReferences(t *testing.T) {
	svc := newProviderPruneTestService(t)

	groups, err := svc.ProviderReferences(context.Background(), "prov_a")
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	if len(groups) != 2 || groups[0].ID == groups[1].ID {
		t.Fatalf("groups = %#v, want group_one and group_two", groups)
	}
	// Returned groups must be copies, not aliases into the cached registry.
	groups[0].Models[0].ProviderIDs[0] = "tampered"
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if reg.ServiceGroups[0].Models[0].ProviderIDs[0] == "tampered" {
		t.Fatal("ProviderReferences leaked a mutable reference into the cached registry")
	}

	groups, err = svc.ProviderReferences(context.Background(), "prov_missing")
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %#v, want empty", groups)
	}
}
