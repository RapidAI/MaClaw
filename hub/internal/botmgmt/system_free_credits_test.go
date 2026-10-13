package botmgmt

import (
	"context"
	"encoding/json"
	"math"
	"testing"
)

func TestSystemFreeCreditsLimitSaveAndUsage(t *testing.T) {
	ctx := context.Background()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		Principals: []ownerPrincipal{{HubUserID: "alice", LLMViewerToken: "bot-token"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)

	limit := 10.5
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, &limit); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, nil); err != nil {
		t.Fatal(err)
	}
	match, ok := svc.LookupBotLLMToken(ctx, "tenant-a", "bot-token")
	if !ok || match.HubUserID != "alice" || match.Limit != 10.5 || match.Used != 0 {
		t.Fatalf("lookup = %+v ok=%v", match, ok)
	}
	if _, ok := svc.LookupBotLLMToken(ctx, "tenant-a", "other-token"); ok {
		t.Fatal("unrelated token matched a bot viewer token")
	}

	if err := svc.AddSystemFreeCreditsUsed(ctx, "tenant-a", "alice", "req-1", 4); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddSystemFreeCreditsUsed(ctx, "tenant-a", "alice", "req-1", 4); err != nil {
		t.Fatal(err)
	}
	view, err := svc.SystemFreeCreditsForOwner(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Applies || view.Unlimited || view.Limit != 10.5 || view.Used != 4 || view.Remaining != 6.5 {
		t.Fatalf("view = %+v", view)
	}

	zero := 0.0
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, &zero); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddSystemFreeCreditsUsed(ctx, "tenant-a", "alice", "req-2", 3); err != nil {
		t.Fatal(err)
	}
	view, err = svc.SystemFreeCreditsForOwner(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Applies || !view.Unlimited || view.Limit != 0 || view.Used != 4 {
		t.Fatalf("unlimited view = %+v", view)
	}

	negative := -1.0
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, &negative); err == nil {
		t.Fatal("negative limit accepted")
	}
	bad := math.NaN()
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, &bad); err == nil {
		t.Fatal("NaN limit accepted")
	}
}

func TestSystemFreeCreditsViewAppliesOnlyOnSystemFree(t *testing.T) {
	ctx := context.Background()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		LLM:        llmSettings{Current: "lp_other", SystemFreeCreditsLimit: 8},
		Principals: []ownerPrincipal{{HubUserID: "alice", LLMViewerToken: "bot-token"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	view, err := svc.SystemFreeCreditsForOwner(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Applies || view.Unlimited || view.Limit != 8 {
		t.Fatalf("non-system-free view = %+v", view)
	}

	raw, err = json.Marshal(record{
		LLM:        llmSettings{Current: "system-free", SystemFreeCreditsLimit: 8},
		Principals: []ownerPrincipal{{HubUserID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	view, err = svc.SystemFreeCreditsForOwner(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Applies || view.Limit != 8 {
		t.Fatalf("legacy system-free still presents a bot cap: %+v", view)
	}

	raw, err = json.Marshal(record{
		LLM: llmSettings{Current: "system-free", SystemFreeCreditsLimit: 8},
		OwnerLLMOverrides: map[string]*ownerLLMOverride{
			"alice": {Name: "own", URL: "https://llm.example/v1", Key: "sk", Model: "m"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	view, err = svc.SystemFreeCreditsForOwner(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Applies {
		t.Fatalf("owner override still applies system-free budget: %+v", view)
	}
}
