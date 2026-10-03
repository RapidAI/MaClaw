package llmservice

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestProviderInventoryRedactsKeyAndListsNodes(t *testing.T) {
	resetMemberRuntime()
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-a-1", Name: "Primary", APIURL: "https://api.example/v1", APIKey: "sk-live-secret-ABCD",
			Protocol: "openai", Models: []string{"free-llama-70b"}, ArrayID: "pool-a",
			AllowedNodes: "hc-1, hc-2,hc-3", DispatchWeight: 3, RequestsPerDay: 200,
			ModelMap: map[string]string{"free-llama-70b": "meta-llama/Llama-3.3-70B"},
		}},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	inventory, err := svc.ListProviderArrayInventory(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	raw, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-live-secret-ABCD") || strings.Contains(string(raw), `"api_key"`) {
		t.Fatalf("inventory leaked the upstream key: %s", raw)
	}
	if len(inventory.Arrays) != 1 || len(inventory.Arrays[0].Members) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	member := inventory.Arrays[0].Members[0]
	if !member.APIKeyConfigured || member.APIKeyLast4 != "ABCD" || !member.Enabled {
		t.Fatalf("member disclosure = %+v", member)
	}
	if len(member.AllowedNodeIDs) != 3 || member.AllowedNodeIDs[0] != "hc-1" || member.AllowedNodeIDs[2] != "hc-3" {
		t.Fatalf("nodes = %#v", member.AllowedNodeIDs)
	}
	if member.ModelMap["free-llama-70b"] != "meta-llama/Llama-3.3-70B" || member.DispatchWeight != 3 {
		t.Fatalf("policy = %+v", member)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(reg.Providers[0].AllowedNodes) != "" {
		t.Fatalf("allowed_nodes was stored: %q", reg.Providers[0].AllowedNodes)
	}
}

func TestPreviewProviderArraysDoesNotWrite(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	preview, err := svc.PreviewProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-1", Name: "One", APIURL: "https://api.example/v1", APIKey: "secret",
		}},
	}})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Arrays) != 1 || len(preview.Arrays[0].Created) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Providers) != 0 {
		t.Fatalf("dry-run wrote providers: %+v", reg.Providers)
	}
}

func TestPatchProviderMemberNodesAndDisable(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "a", Name: "A", APIURL: "https://a.example/v1", APIKey: "secret-a"}},
	}); err != nil {
		t.Fatal(err)
	}
	enabled := false
	if err := svc.PatchProviderMember(ctx, "a", ProviderMemberPatch{
		Enabled:           &enabled,
		AllowedNodeIDsSet: true,
		AllowedNodeIDs:    []string{"hc-2", "hc-1"},
	}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Providers[0]
	if !got.Paused || len(got.AllowedNodeIDs) != 2 || got.APIKey != "secret-a" {
		t.Fatalf("patched = %+v", got)
	}
}

func TestProviderCapabilityTagsRoundTrip(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if _, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-1", Name: "One", APIURL: "https://api.example/v1", APIKey: "secret",
			CapabilityTags: []string{"Tools", "vision", "tools"},
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-1", Name: "One", APIURL: "https://api.example/v1",
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(reg.Providers[0].CapabilityTags, ",") != "tools,vision" {
		t.Fatalf("kept tags = %#v", reg.Providers[0].CapabilityTags)
	}
	if err := svc.PatchProviderMember(ctx, "pool-1", ProviderMemberPatch{CapabilityTagsSet: true}); err != nil {
		t.Fatal(err)
	}
	inventory, err := svc.ListProviderArrayInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tags := inventory.Arrays[0].Members[0].CapabilityTags; len(tags) != 0 {
		t.Fatalf("cleared tags = %#v", tags)
	}
	if _, err := NormalizeCapabilityTags([]string{"bad tag"}); err == nil {
		t.Fatal("expected invalid tag")
	}
	for _, tag := range ProviderCapabilityPresets {
		if _, err := NormalizeCapabilityTags([]string{tag}); err != nil {
			t.Fatalf("preset %s: %v", tag, err)
		}
	}
	if _, err := NormalizeCapabilityTags([]string{"---"}); err == nil {
		t.Fatal("expected separator-only tag to be rejected")
	}
}

func TestProviderCapabilityTagsFeedDispatch(t *testing.T) {
	reg := &Registry{Providers: []llmpool.ProviderConfig{{
		ID: "p1", CapabilityTags: []string{"tools", "vision"},
	}}}
	model := &llmpool.ModelConfig{
		Name:           "auto",
		CapabilityTags: []string{"chat"},
		ProviderConfigs: []llmpool.ModelProviderConfig{{
			ProviderID: "p1",
		}},
	}
	dm := buildDispatchModel(reg, model)
	if strings.Join(dm.ProviderRoutes[0].CapabilityTags, ",") != "chat,tools,vision" {
		t.Fatalf("route tags = %#v", dm.ProviderRoutes[0].CapabilityTags)
	}
	scored := llmpool.OrderScoredProviderRoutes(map[string]any{"tools": []any{map[string]any{"type": "function"}}}, dm)
	if len(scored) != 1 || scored[0].Score == 0 {
		t.Fatalf("tools score = %#v", scored)
	}
	model.ProviderConfigs[0].CapabilityTags = []string{"reasoning"}
	dm = buildDispatchModel(reg, model)
	if strings.Join(dm.ProviderRoutes[0].CapabilityTags, ",") != "reasoning" {
		t.Fatalf("route override = %#v", dm.ProviderRoutes[0].CapabilityTags)
	}
	model.ProviderConfigs[0].CapabilityTags = nil
	reg.Providers[0].CapabilityTags = nil
	dm = buildDispatchModel(reg, model)
	if len(dm.ProviderRoutes[0].CapabilityTags) != 0 || strings.Join(dm.CapabilityTags, ",") != "chat" {
		t.Fatalf("fallback route=%#v model=%#v", dm.ProviderRoutes[0].CapabilityTags, dm.CapabilityTags)
	}
}

func TestGroupWithMemberCapabilityTagsLeavesStoredGroup(t *testing.T) {
	reg := &Registry{Providers: []llmpool.ProviderConfig{{
		ID: "p1", CapabilityTags: []string{"vision"},
	}}}
	group := &llmpool.ServiceGroup{
		Kind: llmpool.ServiceGroupKindDynamic,
		Models: []llmpool.ModelConfig{
			{
				Name: "writer",
				ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "p1"},
					{ProviderID: "p2", CapabilityTags: []string{"reasoning"}},
				},
			},
			{Name: "coder"},
		},
		Routes: []llmpool.WorkloadRoute{
			{Model: "writer", Quality: llmpool.QualityMid},
			{Model: "coder", Quality: llmpool.QualityMid},
		},
	}
	scored := groupWithMemberCapabilityTags(reg, group)
	if scored == group {
		t.Fatal("expected a scoring copy")
	}
	if len(group.Models[0].ProviderConfigs[0].CapabilityTags) != 0 {
		t.Fatal("stored route tags were changed")
	}
	if strings.Join(scored.Models[0].ProviderConfigs[0].CapabilityTags, ",") != "vision" {
		t.Fatalf("scoring tags = %#v", scored.Models[0].ProviderConfigs[0].CapabilityTags)
	}
	if strings.Join(scored.Models[0].ProviderConfigs[1].CapabilityTags, ",") != "reasoning" {
		t.Fatalf("route override = %#v", scored.Models[0].ProviderConfigs[1].CapabilityTags)
	}
	plain := &llmpool.ServiceGroup{Kind: llmpool.ServiceGroupKindDynamic, Models: group.Models}
	if groupWithMemberCapabilityTags(&Registry{}, plain) != plain {
		t.Fatal("expected the original group when members have no tags")
	}
	official := &llmpool.ServiceGroup{
		Kind: llmpool.ServiceGroupKindDynamic,
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierMid, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
		},
	}
	if groupWithMemberCapabilityTags(reg, official) != official {
		t.Fatal("one model per quality band does not need a scoring copy")
	}
}

func TestArrayMemberCapabilityTagsFeedDispatch(t *testing.T) {
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "m1", ArrayID: "pool", CapabilityTags: []string{"chat"}, Paused: true},
			{ID: "m2", ArrayID: "pool", CapabilityTags: []string{"vision"}},
		},
		ProviderArrays: []llmpool.ProviderArray{{ID: "pool", MemberIDs: []string{"m1", "m2"}}},
	}
	model := &llmpool.ModelConfig{
		Name:            "writer",
		CapabilityTags:  []string{"chat"},
		ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "pool"}},
	}
	dm := buildDispatchModel(reg, model)
	if strings.Join(dm.ProviderRoutes[0].CapabilityTags, ",") != "chat,vision" {
		t.Fatalf("route tags = %#v", dm.ProviderRoutes[0].CapabilityTags)
	}
	group := &llmpool.ServiceGroup{
		Kind: llmpool.ServiceGroupKindDynamic,
		Models: []llmpool.ModelConfig{
			*model,
			{Name: "coder"},
		},
		Routes: []llmpool.WorkloadRoute{
			{Model: "writer", Quality: llmpool.QualityMid},
			{Model: "coder", Quality: llmpool.QualityMid},
		},
	}
	scored := groupWithMemberCapabilityTags(reg, group)
	if scored == group {
		t.Fatal("expected a scoring copy")
	}
	if strings.Join(scored.Models[0].ProviderConfigs[0].CapabilityTags, ",") != "vision" {
		t.Fatalf("scoring tags = %#v", scored.Models[0].ProviderConfigs[0].CapabilityTags)
	}
	if len(group.Models[0].ProviderConfigs[0].CapabilityTags) != 0 {
		t.Fatal("stored route tags were changed")
	}
	left := &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "pool", ArrayID: "other", CapabilityTags: []string{"audio"}},
		{ID: "m2", ArrayID: "pool", CapabilityTags: []string{"vision"}},
	}}
	leftModel := &llmpool.ModelConfig{
		ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "pool"}},
	}
	leftDispatch := buildDispatchModel(left, leftModel)
	if strings.Join(leftDispatch.ProviderRoutes[0].CapabilityTags, ",") != "vision" {
		t.Fatalf("left member tags = %#v", leftDispatch.ProviderRoutes[0].CapabilityTags)
	}
	stale := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "pool", ArrayID: "other", CapabilityTags: []string{"audio"}},
			{ID: "m2", ArrayID: "pool", CapabilityTags: []string{"vision"}},
		},
		ProviderArrays: []llmpool.ProviderArray{{ID: "pool"}},
	}
	staleDispatch := buildDispatchModel(stale, &llmpool.ModelConfig{
		ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "pool"}},
	})
	if strings.Join(staleDispatch.ProviderRoutes[0].CapabilityTags, ",") != "vision" {
		t.Fatalf("stale member list tags = %#v", staleDispatch.ProviderRoutes[0].CapabilityTags)
	}
	if ids := providerArrayMemberIDs(stale, "pool"); len(ids) != 1 || ids[0] != "m2" {
		t.Fatalf("member ids = %#v", ids)
	}
	cased := &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "pool", ArrayID: "other", CapabilityTags: []string{"audio"}},
		{ID: "m2", ArrayID: "pool", CapabilityTags: []string{"vision"}},
	}}
	logical, _, members := lookupProviderArray(cased, "Pool", acceptLiveProvider)
	if logical != "pool" || len(members) != 1 || members[0].ID != "m2" {
		t.Fatalf("logical=%q members=%v", logical, members)
	}
	plain := &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "m2", ArrayID: "pool", CapabilityTags: []string{"vision"}},
	}}
	logical, _, members = lookupProviderArray(plain, "Pool", acceptLiveProvider)
	if logical != "pool" || len(members) != 1 || members[0].ID != "m2" {
		t.Fatalf("plain logical=%q members=%v", logical, members)
	}
	empty := &Registry{
		Providers:      []llmpool.ProviderConfig{{ID: "pool", ArrayID: "pool#solo", CapabilityTags: []string{"audio"}}},
		ProviderArrays: []llmpool.ProviderArray{{ID: "pool", Manual: true}},
	}
	if _, _, members = lookupProviderArray(empty, "pool", acceptLiveProvider); len(members) != 0 {
		t.Fatalf("empty array members = %#v", members)
	}
	paused := &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "a", ArrayID: "pool", CapabilityTags: []string{"vision"}, Paused: true},
		{ID: "b", ArrayID: "pool", CapabilityTags: []string{"tools"}, Paused: true},
	}}
	pausedModel := &llmpool.ModelConfig{
		CapabilityTags:  []string{"chat"},
		ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "pool"}},
	}
	pausedDispatch := buildDispatchModel(paused, pausedModel)
	if len(pausedDispatch.ProviderRoutes[0].CapabilityTags) != 0 {
		t.Fatalf("paused array tags = %#v", pausedDispatch.ProviderRoutes[0].CapabilityTags)
	}
	pauseArrayMember("m-cool", time.Minute)
	t.Cleanup(func() { clearArrayMemberPause("m-cool") })
	cooling := &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "m-cool", ArrayID: "pool", CapabilityTags: []string{"vision"}},
		{ID: "m-ready", ArrayID: "pool", CapabilityTags: []string{"chat"}},
	}}
	coolingDispatch := buildDispatchModel(cooling, &llmpool.ModelConfig{
		ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "pool"}},
	})
	if strings.Join(coolingDispatch.ProviderRoutes[0].CapabilityTags, ",") != "chat" {
		t.Fatalf("cooling member tags = %#v", coolingDispatch.ProviderRoutes[0].CapabilityTags)
	}
}

func TestMemberQuotaSkipsExhaustedMember(t *testing.T) {
	resetMemberRuntime()
	member := &llmpool.ProviderConfig{ID: "quota-a", RequestsPerMinute: 1, Timezone: "UTC"}
	if _, _, blocked := admitMemberQuota(member, time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC)); blocked {
		t.Fatal("first admit blocked")
	}
	until, blocked := memberQuotaBlocked(member, time.Date(2026, 9, 30, 1, 2, 40, 0, time.UTC))
	want := time.Date(2026, 9, 30, 1, 3, 0, 0, time.UTC)
	if !blocked || !until.Equal(want) {
		t.Fatalf("second minute check blocked=%v until=%s", blocked, until)
	}
	if _, blocked := memberQuotaBlocked(member, time.Date(2026, 9, 30, 1, 3, 0, 0, time.UTC)); blocked {
		t.Fatal("next minute still blocked")
	}
}

func TestReleaseMemberQuotaLetsCanceledDialRetry(t *testing.T) {
	resetMemberRuntime()
	now := time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC)
	member := &llmpool.ProviderConfig{ID: "quota-release", RequestsPerDay: 1, Timezone: "UTC"}
	lease, _, blocked := admitMemberQuota(member, now)
	if blocked {
		t.Fatal("first admit blocked")
	}
	releaseMemberQuota(lease)
	if _, _, blocked := admitMemberQuota(member, now.Add(time.Second)); blocked {
		t.Fatal("released admit still consumed the daily cap")
	}
}

func TestReleaseMemberQuotaDoesNotStealNextWindow(t *testing.T) {
	resetMemberRuntime()
	member := &llmpool.ProviderConfig{ID: "quota-lease", RequestsPerMinute: 1, Timezone: "UTC"}
	lease, _, blocked := admitMemberQuota(member, time.Date(2026, 9, 30, 1, 2, 40, 0, time.UTC))
	if blocked {
		t.Fatal("first admit blocked")
	}
	next := time.Date(2026, 9, 30, 1, 3, 1, 0, time.UTC)
	if _, _, blocked := admitMemberQuota(member, next); blocked {
		t.Fatal("next minute should be open")
	}
	releaseMemberQuota(lease)
	if _, blocked := memberQuotaBlocked(member, next); !blocked {
		t.Fatal("releasing the previous minute freed the current one")
	}
}

func TestMemberQuotaDeadlineUsesNextLocalMidnight(t *testing.T) {
	resetMemberRuntime()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 8, 1, 30, 0, 0, loc)
	member := &llmpool.ProviderConfig{ID: "quota-dst", RequestsPerDay: 1, Timezone: "America/New_York"}
	if _, _, blocked := admitMemberQuota(member, now); blocked {
		t.Fatal("first admit blocked")
	}
	until, blocked := memberQuotaBlocked(member, now)
	want := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)
	if !blocked || !until.Equal(want) {
		t.Fatalf("until=%s want=%s blocked=%v", until, want, blocked)
	}
}

func TestMemberQuotaReportsLaterDeadline(t *testing.T) {
	resetMemberRuntime()
	now := time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC)
	member := &llmpool.ProviderConfig{ID: "quota-both", RequestsPerMinute: 1, RequestsPerDay: 1, Timezone: "UTC"}
	if _, _, blocked := admitMemberQuota(member, now); blocked {
		t.Fatal("first admit blocked")
	}
	until, blocked := memberQuotaBlocked(member, now.Add(time.Second))
	want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if !blocked || !until.Equal(want) {
		t.Fatalf("until=%s blocked=%v", until, blocked)
	}
	health := memberHealthSnapshot(member, now.Add(time.Second))
	if !health.QuotaUntil.Equal(want.UTC()) {
		t.Fatalf("health quota_until=%s", health.QuotaUntil)
	}
}

func TestModelMapRewritesPublicName(t *testing.T) {
	member := &llmpool.ProviderConfig{ModelMap: map[string]string{"Free-Llama-70B": "vendor-llama"}}
	if got := memberUpstreamModel(member, "chat", "free-llama-70b", "chat"); got != "vendor-llama" {
		t.Fatalf("mapped model = %q", got)
	}
	if got := UpstreamModelForMember(member, "free-llama-70b"); got != "vendor-llama" {
		t.Fatalf("public model = %q", got)
	}
}

func TestMemberUpstreamModelUsesOwnModelWhenRouteNameIsForeign(t *testing.T) {
	openrouter := &llmpool.ProviderConfig{Models: []string{"openrouter/free"}}
	agnes := &llmpool.ProviderConfig{Models: []string{"agnes-3.0-flash"}}
	if got := memberUpstreamModel(openrouter, "DeepSeek-V4-Flash-0731", "official-low", "DeepSeek-V4-Flash-0731"); got != "openrouter/free" {
		t.Fatalf("hardcoded foreign model = %q", got)
	}
	if got := memberUpstreamModel(agnes, "", "official-low", "openrouter/free"); got != "agnes-3.0-flash" {
		t.Fatalf("empty route inherited first member = %q", got)
	}
	listed := &llmpool.ProviderConfig{Models: []string{"DeepSeek-V4-Flash-0731", "other"}}
	if got := memberUpstreamModel(listed, "DeepSeek-V4-Flash-0731", "official-low", ""); got != "DeepSeek-V4-Flash-0731" {
		t.Fatalf("member that offers the route model = %q", got)
	}
	mapped := &llmpool.ProviderConfig{
		Models:   []string{"openrouter/free"},
		ModelMap: map[string]string{"DeepSeek-V4-Flash-0731": "openrouter/free"},
	}
	if got := memberUpstreamModel(mapped, "DeepSeek-V4-Flash-0731", "official-low", ""); got != "openrouter/free" {
		t.Fatalf("mapped route model = %q", got)
	}
	bare := &llmpool.ProviderConfig{}
	if got := memberUpstreamModel(bare, "qwen-plus", "deepseek-chat", "qwen-plus"); got != "qwen-plus" {
		t.Fatalf("provider without a catalog = %q", got)
	}
	for _, band := range []string{"auto", "low", "mid", "high", "official-low"} {
		got := memberUpstreamModel(agnes, "DeepSeek-V4-Flash-0731", band, "official-low")
		if got != "agnes-3.0-flash" || isBillingBandName(got) {
			t.Fatalf("band %s upstream = %q, want the provider model", band, got)
		}
	}
	mappedBand := &llmpool.ProviderConfig{
		Models:   []string{"agnes-3.0-flash"},
		ModelMap: map[string]string{"low": "agnes-2.0-flash"},
	}
	if got := memberUpstreamModel(mappedBand, "", "official-low", ""); got != "agnes-2.0-flash" {
		t.Fatalf("band map = %q", got)
	}
	bandMapped := &llmpool.ProviderConfig{
		Models:   []string{"agnes-3.0-flash"},
		ModelMap: map[string]string{"official-low": "low"},
	}
	if got := memberUpstreamModel(bandMapped, "", "official-low", ""); got != "agnes-3.0-flash" || isBillingBandName(got) {
		t.Fatalf("band map target = %q, want the configured model", got)
	}
	aliasAfterBand := &llmpool.ProviderConfig{
		Models: []string{"agnes-3.0-flash"},
		ModelMap: map[string]string{
			"official-low": "low",
			"low":          "agnes-2.0-flash",
		},
	}
	if got := memberUpstreamModel(aliasAfterBand, "", "official-low", ""); got != "agnes-2.0-flash" {
		t.Fatalf("alias after band map target = %q", got)
	}
	if got := memberUpstreamModel(&llmpool.ProviderConfig{}, "official-low", "low", "auto"); got != "low" {
		t.Fatalf("unconfigured band upstream = %q, want the logical model", got)
	}
	// Providers that never filled a catalog keep the concrete name on the route.
	if got := memberUpstreamModel(&llmpool.ProviderConfig{}, "chat", "auto", "openrouter/free"); got != "chat" {
		t.Fatalf("legacy route model = %q", got)
	}
	if got := memberUpstreamModel(&llmpool.ProviderConfig{}, "", "auto", ""); got != "auto" {
		t.Fatalf("empty route legacy model = %q", got)
	}
	several := &llmpool.ProviderConfig{Models: []string{"agnes-3.0-flash", "agnes-2.0-flash"}}
	if got := memberUpstreamModel(several, "DeepSeek-V4-Flash-0731", "qwen-plus", ""); got != "agnes-3.0-flash" {
		t.Fatalf("foreign pin with several models = %q", got)
	}
}

func TestModelMapRejectsCaseDuplicate(t *testing.T) {
	provider := &llmpool.ProviderConfig{ModelMap: map[string]string{"Llama": "a", " llama ": "b"}}
	if err := validateMemberPolicy(provider); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("error = %v", err)
	}
}

func TestOrderArrayMembersUsesWeight(t *testing.T) {
	members := []*llmpool.ProviderConfig{
		{ID: "light", DispatchWeight: 1},
		{ID: "heavy", DispatchWeight: 3},
	}
	counts := map[string]int{}
	for i := 0; i < 40; i++ {
		ordered := orderArrayMembers("weight-pool", members)
		if len(ordered) != 2 {
			t.Fatalf("order = %#v", ordered)
		}
		counts[ordered[0].ID]++
	}
	if counts["heavy"] <= counts["light"] {
		t.Fatalf("weight picks = %#v, want heavy more often", counts)
	}
}

func TestAdminAPIKeyScopeAndExpiry(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	created, err := svc.CreateAdminAPIKeySpec(ctx, AdminAPIKeySpec{
		Name:      "limited",
		Scopes:    []string{"read", "test"},
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.AuthorizeAdminAPIKey(ctx, created.APIKey, AdminAPIScopeRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := svc.AuthenticateAdminAPIKey(ctx, created.APIKey); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if _, err := svc.AuthorizeAdminAPIKey(ctx, created.APIKey, AdminAPIScopeWrite); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("write error = %v", err)
	}
	raw := svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey]
	var file adminAPIKeyFile
	if err := json.Unmarshal([]byte(raw), &file); err != nil {
		t.Fatal(err)
	}
	file.Keys[0].ExpiresAt = time.Now().Add(-time.Minute).UTC()
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey] = string(encoded)
	if err := svc.AuthenticateAdminAPIKey(ctx, created.APIKey); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired error = %v", err)
	}
}
