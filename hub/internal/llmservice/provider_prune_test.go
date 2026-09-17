package llmservice

import (
	"context"
	"testing"
)

type pruneMemorySettingsRepo struct {
	data map[string]string
	sets int
}

func (m *pruneMemorySettingsRepo) Get(_ context.Context, key string) (string, error) {
	return m.data[key], nil
}

func (m *pruneMemorySettingsRepo) Set(_ context.Context, key, value string) error {
	m.sets++
	if m.data == nil {
		m.data = map[string]string{}
	}
	m.data[key] = value
	return nil
}

func pruneTestRegistry() *Registry {
	return &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:   "group-a",
		Name: "Group A",
		Models: []ModelServiceModel{{
			Name:        "model-x",
			ProviderIDs: []string{"provider-a", "provider-b"},
			ProviderConfigs: []ModelServiceProviderConfig{{
				ProviderID: "provider-a",
				Model:      "gpt-x",
			}, {
				ProviderID:       "provider-b",
				Model:            "gpt-x-b",
				CreditMultiplier: 2,
			}},
		}},
	}, {
		ID:   "group-b",
		Name: "Group B",
		Models: []ModelServiceModel{{
			Name:        "model-y",
			ProviderIDs: []string{"provider-c"},
			ProviderConfigs: []ModelServiceProviderConfig{{
				ProviderID: "provider-c",
				Model:      "gpt-y",
			}},
		}},
	}}}
}

func TestPruneProvidersRemovesOnlyDisappearedProviders(t *testing.T) {
	reg := pruneTestRegistry()
	if !reg.PruneProviders([]string{"provider-b"}) {
		t.Fatal("expected change")
	}
	groupA := reg.FindModelServiceGroup("group-a")
	if groupA == nil || len(groupA.Models) != 1 {
		t.Fatalf("group-a models = %#v", groupA)
	}
	modelX := groupA.Models[0]
	if len(modelX.ProviderIDs) != 1 || modelX.ProviderIDs[0] != "provider-a" {
		t.Fatalf("model-x provider ids = %#v", modelX.ProviderIDs)
	}
	if len(modelX.ProviderConfigs) != 1 || modelX.ProviderConfigs[0].ProviderID != "provider-a" {
		t.Fatalf("model-x provider configs = %#v", modelX.ProviderConfigs)
	}
	groupB := reg.FindModelServiceGroup("group-b")
	if groupB == nil || len(groupB.Models) != 1 {
		t.Fatalf("group-b models = %#v", groupB)
	}
	if len(groupB.Models[0].ProviderIDs) != 1 || groupB.Models[0].ProviderIDs[0] != "provider-c" {
		t.Fatalf("model-y provider ids = %#v", groupB.Models[0].ProviderIDs)
	}
	// Case-insensitive matching on the removal set.
	if !reg.PruneProviders([]string{"PROVIDER-A", " Provider-C "}) {
		t.Fatal("expected change for case-insensitive removal")
	}
	groupA = reg.FindModelServiceGroup("group-a")
	groupB = reg.FindModelServiceGroup("group-b")
	if len(groupA.Models[0].ProviderIDs) != 0 || len(groupA.Models[0].ProviderConfigs) != 0 {
		t.Fatalf("model-x should be empty after pruning all providers: %#v", groupA.Models[0])
	}
	if len(groupB.Models[0].ProviderIDs) != 0 || len(groupB.Models[0].ProviderConfigs) != 0 {
		t.Fatalf("model-y should be empty after pruning all providers: %#v", groupB.Models[0])
	}
}

func TestPruneProvidersKeepsEmptyModelsAndGroups(t *testing.T) {
	reg := pruneTestRegistry()
	if !reg.PruneProviders([]string{"provider-a", "provider-b", "provider-c"}) {
		t.Fatal("expected change")
	}
	if len(reg.ModelServiceGroups) != 2 {
		t.Fatalf("groups must be retained: %#v", reg.ModelServiceGroups)
	}
	for _, group := range reg.ModelServiceGroups {
		if len(group.Models) != 1 {
			t.Fatalf("group %q models must be retained: %#v", group.ID, group.Models)
		}
	}
	// Pruning against a registry with no references is a no-op.
	if reg.PruneProviders([]string{"provider-a", "provider-b", "provider-c"}) {
		t.Fatal("second prune should not change anything")
	}
	if (&Registry{}).PruneProviders(nil) {
		t.Fatal("nil provider list should be a no-op")
	}
}

func TestPruneProvidersFromGroupsPersistsOnlyWhenChanged(t *testing.T) {
	ctx := context.Background()
	repo := &pruneMemorySettingsRepo{data: map[string]string{}}
	changed, err := PruneProvidersFromGroups(ctx, repo, []string{"provider-b"})
	if err != nil {
		t.Fatalf("PruneProvidersFromGroups on empty store: %v", err)
	}
	if changed {
		t.Fatal("empty store should not change")
	}
	if repo.sets != 0 {
		t.Fatalf("no-op must not persist, sets = %d", repo.sets)
	}

	reg := pruneTestRegistry()
	if err := SaveRegistry(ctx, repo, reg); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	setsAfterSeed := repo.sets

	changed, err = PruneProvidersFromGroups(ctx, repo, []string{"provider-b"})
	if err != nil {
		t.Fatalf("PruneProvidersFromGroups: %v", err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	if repo.sets != setsAfterSeed+1 {
		t.Fatalf("prune must persist exactly once, sets = %d want %d", repo.sets, setsAfterSeed+1)
	}

	stored, err := LoadRegistry(ctx, repo)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	groupA := stored.FindModelServiceGroup("group-a")
	if groupA == nil || len(groupA.Models[0].ProviderIDs) != 1 || groupA.Models[0].ProviderIDs[0] != "provider-a" {
		t.Fatalf("stored group-a = %#v", groupA)
	}
	groupB := stored.FindModelServiceGroup("group-b")
	if groupB == nil || len(groupB.Models[0].ProviderIDs) != 1 || groupB.Models[0].ProviderIDs[0] != "provider-c" {
		t.Fatalf("stored group-b = %#v", groupB)
	}

	// A removal that no group references must not write.
	changed, err = PruneProvidersFromGroups(ctx, repo, []string{"provider-zzz"})
	if err != nil {
		t.Fatalf("PruneProvidersFromGroups unknown provider: %v", err)
	}
	if changed {
		t.Fatal("unknown provider should not change anything")
	}
	if repo.sets != setsAfterSeed+1 {
		t.Fatalf("unknown provider must not persist, sets = %d want %d", repo.sets, setsAfterSeed+1)
	}
}
