package llmservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// GetProvider and FindServiceGroupForModel hand results to HTTP handlers and
// test helpers. A shallow copy would share the cached registry's slice and
// map backing arrays, so one careless caller mutation would corrupt the
// cache every later reader sees (and the next persist would write the
// corruption back). These tests pin the deep-copy contract that
// ProviderReferences already documents.
func TestGetProviderReturnsDeepCopy(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:             "deep",
		Name:           "deep",
		APIURL:         "https://api.deep.example/v1",
		Models:         []string{"model-a", "model-b"},
		CapabilityTags: []string{"tag-a"},
		AllowedNodeIDs: []string{"node-1"},
		ModelMap:       map[string]string{"k": "v"},
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	got, err := svc.GetProvider(ctx, "deep")
	if err != nil || got == nil {
		t.Fatalf("get provider: %v", err)
	}
	got.Models[0] = "mutated"
	got.CapabilityTags[0] = "mutated"
	got.AllowedNodeIDs[0] = "mutated"
	got.ModelMap["k"] = "mutated"

	fresh, err := svc.GetProvider(ctx, "deep")
	if err != nil || fresh == nil {
		t.Fatalf("reload: %v", err)
	}
	if fresh.Models[0] != "model-a" || fresh.Models[1] != "model-b" {
		t.Fatalf("cached Models mutated through returned copy: %v", fresh.Models)
	}
	if fresh.CapabilityTags[0] != "tag-a" {
		t.Fatalf("cached CapabilityTags mutated: %v", fresh.CapabilityTags)
	}
	if fresh.AllowedNodeIDs[0] != "node-1" {
		t.Fatalf("cached AllowedNodeIDs mutated: %v", fresh.AllowedNodeIDs)
	}
	if fresh.ModelMap["k"] != "v" {
		t.Fatalf("cached ModelMap mutated: %v", fresh.ModelMap)
	}
}

func TestFindServiceGroupForModelReturnsDeepCopy(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deep",
		Name:   "deep",
		APIURL: "https://api.deep.example/v1",
		Models: []string{"model-a"},
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	if err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID:   "group-deep",
		Name: "group-deep",
		Models: []llmpool.ModelConfig{{
			Name:           "model-a",
			ProviderIDs:    []string{"deep"},
			CapabilityTags: []string{"tag-a"},
		}},
	}); err != nil {
		t.Fatalf("add service group: %v", err)
	}

	group, dm, err := svc.FindServiceGroupForModel(ctx, "group-deep", "model-a")
	if err != nil || group == nil || dm == nil {
		t.Fatalf("find: %v", err)
	}
	group.Models[0].ProviderIDs[0] = "mutated"
	group.Models[0].CapabilityTags[0] = "mutated"
	dm.CapabilityTags[0] = "mutated"

	fresh, _, err := svc.FindServiceGroupForModel(ctx, "group-deep", "model-a")
	if err != nil || fresh == nil {
		t.Fatalf("reload: %v", err)
	}
	if fresh.Models[0].ProviderIDs[0] != "deep" {
		t.Fatalf("cached ProviderIDs mutated through returned copy: %v", fresh.Models[0].ProviderIDs)
	}
	if fresh.Models[0].CapabilityTags[0] != "tag-a" {
		t.Fatalf("cached model CapabilityTags mutated: %v", fresh.Models[0].CapabilityTags)
	}
}
