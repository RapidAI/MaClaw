package llmservice

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestApplyProxyWorkloadRoutingPinUsesWorkflowClass(t *testing.T) {
	group := officialDynamicFixtureGroup()
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{group}}
	header := http.Header{}
	header.Set(llmpool.WorkflowTypeHeader, "business_plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": llmpool.OfficialTierHigh}}
	dispatch := matchProxyGroupModel(reg, &reg.ServiceGroups[0], llmpool.OfficialTierHigh)
	model, _, _ := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], dispatch, llmpool.OfficialTierHigh)
	if model != llmpool.OfficialTierHigh {
		t.Fatalf("model = %q, want pinned official-high", model)
	}
	if req.WorkloadClass != llmpool.WorkloadClassPlan {
		t.Fatalf("class = %q, want plan from workflow", req.WorkloadClass)
	}
	got := appendOfficialTierAvailabilityRoutes(reg, nil, &reg.ServiceGroups[0], model, req.WorkloadClass, acceptLiveProvider)
	for _, route := range got {
		if route.Model == llmpool.OfficialTierLow {
			t.Fatalf("plan pin extras = %#v, must not include official-low", got)
		}
	}
}

func TestApplyProxyWorkloadRoutingSkipsL1ForConcreteModel(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID:     llmpool.OfficialGroupID,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
		},
	}
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{*group}}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": llmpool.OfficialTierHigh}}
	dispatch := matchProxyGroupModel(reg, group, llmpool.OfficialTierHigh)
	model, matched, _ := applyProxyWorkloadRouting(req, nil, reg, group, dispatch, llmpool.OfficialTierHigh)
	if model != llmpool.OfficialTierHigh {
		t.Fatalf("model = %q, want official-high", model)
	}
	if matched == nil || matched.ID != llmpool.OfficialGroupID {
		t.Fatalf("matched = %#v", matched)
	}
	if req.WorkloadClass != llmpool.WorkloadClassPlan {
		t.Fatalf("class = %q", req.WorkloadClass)
	}
}

func TestApplyProxyWorkloadRoutingResolvesAuto(t *testing.T) {
	group := officialDynamicFixtureGroup()
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{group}}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": "auto"}}
	dispatch := matchProxyGroupModel(reg, &group, "auto")
	model, _, _ := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], dispatch, "auto")
	if model != llmpool.OfficialTierHigh {
		t.Fatalf("model = %q, want official-high", model)
	}
}

func TestApplyProxyWorkloadRoutingDoesNotDispatchFirstModelForAuto(t *testing.T) {
	group := llmpool.ServiceGroup{
		ID:   llmpool.OfficialGroupID,
		Kind: llmpool.ServiceGroupKindDynamic,
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-low"}, {ID: "p-mid"}}, ServiceGroups: []llmpool.ServiceGroup{group}}
	placeholder := matchProxyGroupModel(reg, &reg.ServiceGroups[0], "auto")
	if placeholder == nil || placeholder.Name != llmpool.OfficialTierLow {
		t.Fatalf("placeholder = %#v, want official-low first model", placeholder)
	}
	req := &ProxyRequest{Body: map[string]any{"model": "auto"}}
	model, _, dispatch := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], placeholder, "auto")
	if dispatch != nil && dispatch.Name == llmpool.OfficialTierLow {
		t.Fatalf("auto dispatch = %#v model=%s, must not ride official-low", dispatch, model)
	}
}

func TestApplyProxyWorkloadRoutingFallsBackWhenHighUnavailable(t *testing.T) {
	group := llmpool.ServiceGroup{
		ID:     llmpool.OfficialGroupID,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid"}}},
			{Name: llmpool.OfficialTierHigh},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-mid"}, {ID: "p-low"}},
		ServiceGroups: []llmpool.ServiceGroup{group},
	}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": "auto"}}
	dispatch := matchProxyGroupModel(reg, &reg.ServiceGroups[0], "auto")
	model, _, _ := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], dispatch, "auto")
	if model != llmpool.OfficialTierMid {
		t.Fatalf("model = %q, want official-mid", model)
	}
}

func TestMatchProxyServiceGroupModelKeepsGroupWhenOfficialHighMissing(t *testing.T) {
	group := llmpool.ServiceGroup{
		ID:   "maclaw-official",
		Kind: llmpool.ServiceGroupKindDynamic,
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-mid"}, {ID: "p-low"}}, ServiceGroups: []llmpool.ServiceGroup{group}}
	matched, dispatch := matchProxyServiceGroupModel(reg, group.ID, llmpool.OfficialTierHigh)
	if matched == nil || matched.ID != group.ID {
		t.Fatalf("matched = %#v, want requested group", matched)
	}
	if dispatch == nil || dispatch.Name != "auto" {
		t.Fatalf("dispatch = %#v, want auto placeholder instead of plan→low", dispatch)
	}
}

func TestApplyProxyWorkloadRoutingRejectsAutoPlaceholderForOfficialHigh(t *testing.T) {
	group := llmpool.ServiceGroup{
		ID:     "maclaw-official",
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-low"}}, ServiceGroups: []llmpool.ServiceGroup{group}}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": llmpool.OfficialTierHigh}}
	placeholder := matchProxyGroupModel(reg, &reg.ServiceGroups[0], "auto")
	model, _, dispatch := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], placeholder, llmpool.OfficialTierHigh)
	if dispatch != nil {
		t.Fatalf("dispatch = %#v model=%s, auto placeholder must not serve official-high", dispatch, model)
	}
}

func TestMatchProxyServiceGroupModelDoesNotUseFirstModelAsOfficialPlaceholder(t *testing.T) {
	group := llmpool.ServiceGroup{
		ID:   "maclaw-official",
		Kind: llmpool.ServiceGroupKindDynamic,
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-low"}, {ID: "p-mid"}}, ServiceGroups: []llmpool.ServiceGroup{group}}
	matched, dispatch := matchProxyServiceGroupModel(reg, group.ID, llmpool.OfficialTierHigh)
	if matched == nil || matched.ID != group.ID {
		t.Fatalf("matched = %#v, want requested group", matched)
	}
	if dispatch != nil {
		t.Fatalf("dispatch = %#v, want nil placeholder instead of official-low", dispatch)
	}
}

func TestApplyProxyWorkloadRoutingRematchesNilDispatchToMid(t *testing.T) {
	group := llmpool.ServiceGroup{
		ID:     llmpool.OfficialGroupID,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-mid"}, {ID: "p-low"}}, ServiceGroups: []llmpool.ServiceGroup{group}}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": llmpool.OfficialTierHigh}}
	model, _, dispatch := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], nil, llmpool.OfficialTierHigh)
	if model != llmpool.OfficialTierMid {
		t.Fatalf("model = %q, want official-mid", model)
	}
	if dispatch == nil || dispatch.Name != llmpool.OfficialTierMid {
		t.Fatalf("dispatch = %#v, want official-mid", dispatch)
	}
}

func TestApplyProxyWorkloadRoutingPinnedHighUsesMidNotLow(t *testing.T) {
	group := llmpool.ServiceGroup{
		ID:     llmpool.OfficialGroupID,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid"}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-mid"}, {ID: "p-low"}}, ServiceGroups: []llmpool.ServiceGroup{group}}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": llmpool.OfficialTierHigh}}
	placeholder := matchProxyGroupModel(reg, &reg.ServiceGroups[0], "auto")
	model, _, dispatch := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], placeholder, llmpool.OfficialTierHigh)
	if model != llmpool.OfficialTierMid {
		t.Fatalf("model = %q, want official-mid", model)
	}
	if dispatch == nil || dispatch.Name != llmpool.OfficialTierMid {
		t.Fatalf("dispatch = %#v, want official-mid", dispatch)
	}
}

func TestAppendOfficialTierAvailabilityRoutesAddsMidAfterHigh(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-low"}}}
	got := appendOfficialTierAvailabilityRoutes(reg, []llmpool.DispatchProviderRoute{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}, group, llmpool.OfficialTierHigh, "", acceptLiveProvider)
	if len(got) < 3 {
		t.Fatalf("routes = %#v, want high then mid then low", got)
	}
	if got[1].Model != llmpool.OfficialTierMid || got[1].ProviderID != "p-high" {
		t.Fatalf("first extra = %#v, want p-high/official-mid", got[1])
	}
	if got[2].Model != llmpool.OfficialTierLow || got[2].ProviderID != "p-low" {
		t.Fatalf("second extra = %#v, want p-low/official-low", got[2])
	}
	planOnly := appendOfficialTierAvailabilityRoutes(reg, nil, group, llmpool.OfficialTierHigh, llmpool.WorkloadClassPlan, acceptLiveProvider)
	if len(planOnly) != 1 || planOnly[0].Model != llmpool.OfficialTierMid {
		t.Fatalf("plan extras = %#v, want only official-mid", planOnly)
	}
}

func TestAppendOfficialTierAvailabilityRoutesPlanPinOfLowQualityConcreteAddsMid(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
			{Name: "gpt-4o-mini", ProviderIDs: []string{"p-mini"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mini", Model: "gpt-4o-mini"}}},
		},
		Routes: []llmpool.WorkloadRoute{{Class: llmpool.WorkloadClassPlan, Model: "gpt-4o-mini", Quality: llmpool.QualityLow}},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-mid"}, {ID: "p-mini"}}}
	got := appendOfficialTierAvailabilityRoutes(reg, []llmpool.DispatchProviderRoute{{ProviderID: "p-mini", Model: "gpt-4o-mini"}}, group, "gpt-4o-mini", llmpool.WorkloadClassPlan, acceptLiveProvider)
	hasMid, hasLow := false, false
	for _, route := range got {
		if route.Model == llmpool.OfficialTierMid {
			hasMid = true
		}
		if route.Model == llmpool.OfficialTierLow || route.Model == "gpt-4o-mini" && route.ProviderID != "p-mini" {
			hasLow = true
		}
	}
	if !hasMid {
		t.Fatalf("routes = %#v, plan pin of gpt-4o-mini must add official-mid", got)
	}
	if hasLow {
		t.Fatalf("routes = %#v, plan extras must not add another low", got)
	}
}

func TestOrderProxyDispatchRoutesOfficialHighStaysInRequestedGroup(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
		},
	}
	other := llmpool.ServiceGroup{
		ID: "other-group",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-other"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-other", Model: "gpt-4o-mini"}}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-mid"}, {ID: "p-other"}},
		ServiceGroups: []llmpool.ServiceGroup{*group, other},
	}
	scored := []llmpool.ScoredProviderRoute{{
		Route: llmpool.DispatchProviderRoute{ProviderID: "p-high", Model: llmpool.OfficialTierHigh},
	}}
	got := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierHigh, llmpool.WorkloadClassPlan, scored, acceptLiveProvider, time.Time{}, false)
	for _, route := range got {
		if route.ProviderID == "p-other" || route.Model == "gpt-4o-mini" {
			t.Fatalf("routes = %#v, official extras must stay in requested group", got)
		}
	}
}

func TestOrderProxyDispatchRoutesPlanConcreteLowStaysInRequestedGroup(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
			{Name: "gpt-4o-mini", ProviderIDs: []string{"p-mini"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mini", Model: "gpt-4o-mini"}}},
		},
		Routes: []llmpool.WorkloadRoute{{Class: llmpool.WorkloadClassPlan, Model: "gpt-4o-mini", Quality: llmpool.QualityLow}},
	}
	other := llmpool.ServiceGroup{
		ID: "other-group",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-other"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-other", Model: "gpt-4o-mini"}}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-mid"}, {ID: "p-mini"}, {ID: "p-other"}},
		ServiceGroups: []llmpool.ServiceGroup{*group, other},
	}
	scored := []llmpool.ScoredProviderRoute{{
		Route: llmpool.DispatchProviderRoute{ProviderID: "p-mini", Model: "gpt-4o-mini"},
	}}
	got := orderProxyDispatchRoutes(nil, reg, group, "gpt-4o-mini", llmpool.WorkloadClassPlan, scored, acceptLiveProvider, time.Time{}, false)
	for _, route := range got {
		if route.ProviderID == "p-other" {
			t.Fatalf("routes = %#v, plan extras for gpt-4o-mini must stay in requested group", got)
		}
	}
	midAt, otherAt := -1, -1
	for i, route := range got {
		if route.Model == llmpool.OfficialTierMid && midAt < 0 {
			midAt = i
		}
		if route.ProviderID == "p-other" {
			otherAt = i
		}
	}
	if midAt < 0 {
		t.Fatalf("routes = %#v, want official-mid extra", got)
	}
	if otherAt >= 0 && otherAt < midAt {
		t.Fatalf("routes = %#v, other-group low must not precede official-mid", got)
	}
}

func TestOrderProxyDispatchRoutesKeepsOfficialMidBeforeLow(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-low"}},
		ServiceGroups: []llmpool.ServiceGroup{*group},
	}
	scored := []llmpool.ScoredProviderRoute{{
		Route: llmpool.DispatchProviderRoute{ProviderID: "p-high", Model: llmpool.OfficialTierHigh},
	}}
	got := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierHigh, "", scored, acceptLiveProvider, time.Time{}, false)
	var models []string
	for _, route := range got {
		models = append(models, route.Model)
	}
	midAt, lowAt := -1, -1
	for i, model := range models {
		if model == llmpool.OfficialTierMid && midAt < 0 {
			midAt = i
		}
		if model == llmpool.OfficialTierLow && lowAt < 0 {
			lowAt = i
		}
	}
	if midAt < 0 || lowAt < 0 || midAt > lowAt {
		t.Fatalf("route models = %#v, want official-mid before official-low", models)
	}
}

func TestOrderProxyDispatchRoutesStripsConcreteMidUpstreamFromWRRExtras(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: "gpt-4o"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-mid"}, {ID: "p-low"}},
		ServiceGroups: []llmpool.ServiceGroup{*group},
	}
	scored := []llmpool.ScoredProviderRoute{{
		Route: llmpool.DispatchProviderRoute{ProviderID: "p-high", Model: llmpool.OfficialTierHigh},
	}}
	got := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierHigh, "", scored, acceptLiveProvider, time.Time{}, false)
	gptAt, lowAt := -1, -1
	for i, route := range got {
		if route.Model == "gpt-4o" && gptAt < 0 {
			gptAt = i
		}
		if route.Model == llmpool.OfficialTierLow && lowAt < 0 {
			lowAt = i
		}
	}
	if gptAt < 0 || lowAt < 0 || gptAt > lowAt {
		t.Fatalf("routes = %#v, want gpt-4o (mid) before official-low", got)
	}
}

func TestOrderProxyDispatchRoutesStripsInferredMidUpstreamFromWRRExtras(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "p-high", Models: []string{llmpool.OfficialTierHigh}},
			{ID: "p-mid", Models: []string{"gpt-4o-mini"}},
			{ID: "p-low", Models: []string{llmpool.OfficialTierLow}},
		},
		ServiceGroups: []llmpool.ServiceGroup{*group},
	}
	scored := []llmpool.ScoredProviderRoute{{
		Route: llmpool.DispatchProviderRoute{ProviderID: "p-high", Model: llmpool.OfficialTierHigh},
	}}
	got := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierHigh, "", scored, acceptLiveProvider, time.Time{}, false)
	midAt, lowAt := -1, -1
	for i, route := range got {
		if route.ProviderID == "p-mid" && route.Model == "gpt-4o-mini" && midAt < 0 {
			midAt = i
		}
		if route.Model == llmpool.OfficialTierLow && lowAt < 0 {
			lowAt = i
		}
	}
	if midAt < 0 || lowAt < 0 || midAt > lowAt {
		t.Fatalf("routes = %#v, want gpt-4o-mini (inferred mid) before official-low", got)
	}
}

func TestOrderProxyDispatchRoutesDoesNotServeHighOnMidOnlyProvider(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: "gpt-4o"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-mid"}, {ID: "p-low"}},
		ServiceGroups: []llmpool.ServiceGroup{*group},
	}
	scored := []llmpool.ScoredProviderRoute{{
		Route: llmpool.DispatchProviderRoute{ProviderID: "p-high", Model: llmpool.OfficialTierHigh},
	}}
	got := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierHigh, "", scored, acceptLiveProvider, time.Time{}, false)
	for _, route := range got {
		if route.ProviderID == "p-mid" && route.Model == llmpool.OfficialTierHigh {
			t.Fatalf("routes = %#v, mid-only provider must not serve official-high", got)
		}
	}
}

func TestOrderProxyDispatchRoutesKeepsSharedProviderMidInQualityChain(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high", "p-extra"}, ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "p-high", Model: llmpool.OfficialTierHigh},
				{ProviderID: "p-extra", Model: llmpool.OfficialTierHigh},
			}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-extra"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-extra", Model: "gpt-4o"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low", Model: llmpool.OfficialTierLow}}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-extra"}, {ID: "p-low"}},
		ServiceGroups: []llmpool.ServiceGroup{*group},
	}
	scored := []llmpool.ScoredProviderRoute{{
		Route: llmpool.DispatchProviderRoute{ProviderID: "p-high", Model: llmpool.OfficialTierHigh},
	}}
	got := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierHigh, "", scored, acceptLiveProvider, time.Time{}, false)
	lastHigh, gptAt := -1, -1
	for i, route := range got {
		if route.Model == llmpool.OfficialTierHigh {
			lastHigh = i
		}
		if route.ProviderID == "p-extra" && route.Model == "gpt-4o" && gptAt < 0 {
			gptAt = i
		}
	}
	if gptAt < 0 || lastHigh < 0 || gptAt < lastHigh {
		t.Fatalf("routes = %#v, shared-provider mid must stay after official-high extras", got)
	}
}

func TestOfficialLogicalModelForRouteMapsSiblingUpstream(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: "gpt-4o"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-low"}}},
		},
	}
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "p-high", Models: []string{llmpool.OfficialTierHigh}},
			{ID: "p-mid", Models: []string{"gpt-4o"}},
			{ID: "p-low", Models: []string{"gpt-4o-mini"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{*group},
	}
	if got := officialLogicalModelForRoute(reg, group, llmpool.DispatchProviderRoute{ProviderID: "p-mid", Model: "gpt-4o"}, llmpool.OfficialTierHigh); got != llmpool.OfficialTierMid {
		t.Fatalf("concrete mid upstream = %q, want official-mid", got)
	}
	if got := officialLogicalModelForRoute(reg, group, llmpool.DispatchProviderRoute{ProviderID: "p-low", Model: "gpt-4o-mini"}, llmpool.OfficialTierHigh); got != llmpool.OfficialTierLow {
		t.Fatalf("inferred low upstream = %q, want official-low", got)
	}
	if got := officialLogicalModelForRoute(reg, group, llmpool.DispatchProviderRoute{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}, llmpool.OfficialTierHigh); got != llmpool.OfficialTierHigh {
		t.Fatalf("official-high route = %q", got)
	}
}

func TestProxyRouteDispatchRematchesOfficialMidExtra(t *testing.T) {
	group := officialDynamicFixtureGroup()
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{group}, Providers: []llmpool.ProviderConfig{{ID: "p1"}}}
	high := matchProxyGroupModel(reg, &reg.ServiceGroups[0], llmpool.OfficialTierHigh)
	logical, dispatch := proxyRouteDispatch(reg, &reg.ServiceGroups[0], llmpool.DispatchProviderRoute{ProviderID: "p1", Model: llmpool.OfficialTierMid}, llmpool.OfficialTierHigh, high)
	if logical != llmpool.OfficialTierMid {
		t.Fatalf("logical = %q, want official-mid", logical)
	}
	if dispatch == nil || dispatch.Name != llmpool.OfficialTierMid {
		t.Fatalf("dispatch = %#v, want official-mid", dispatch)
	}
	if high != nil && dispatch == high {
		t.Fatal("mid extra must not keep the official-high dispatch model")
	}
}

func TestOfficialLogicalModelForRoutePrefersFallbackWhenSharedUpstream(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"shared"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "shared", Model: "gpt-4o"}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"shared"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "shared", Model: "gpt-4o"}}},
		},
	}
	reg := &Registry{Providers: []llmpool.ProviderConfig{{ID: "shared", Models: []string{"gpt-4o"}}}}
	route := llmpool.DispatchProviderRoute{ProviderID: "shared", Model: "gpt-4o"}
	if got := officialLogicalModelForRoute(reg, group, route, llmpool.OfficialTierMid); got != llmpool.OfficialTierMid {
		t.Fatalf("fallback mid = %q, want official-mid", got)
	}
	if got := officialLogicalModelForRoute(reg, group, route, llmpool.OfficialTierHigh); got != llmpool.OfficialTierHigh {
		t.Fatalf("fallback high = %q, want official-high", got)
	}
}

func TestProxyQuotePricingRequiresMatchingUpstream(t *testing.T) {
	req := &ProxyRequest{Quote: &ProxyQuote{
		ProviderID:    "p-high",
		UpstreamModel: llmpool.OfficialTierHigh,
		Pricing:       llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 9}},
	}}
	if got := proxyQuotePricingForRequest(req, "p-high", llmpool.OfficialTierHigh); got == nil || got.InputCreditsPer10K != 9 {
		t.Fatalf("matching quote = %#v", got)
	}
	if got := proxyQuotePricingForRequest(req, "p-high", llmpool.OfficialTierMid); got != nil {
		t.Fatalf("429 mid must not inherit high quote pricing, got %#v", got)
	}
	if got := proxyQuotePricingForRequest(req, "p-mid", llmpool.OfficialTierHigh); got != nil {
		t.Fatalf("other provider must not inherit quote pricing, got %#v", got)
	}
}

func TestProxyAppendSameGroupRateLimitRoutesKeepsOfficialMidBeforeLow(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID: "maclaw-official",
		Models: []llmpool.ModelConfig{
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p-high"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p-mid"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p-mid", Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p-low", "p-new"}, ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "p-low", Model: llmpool.OfficialTierLow},
				{ProviderID: "p-new", Model: llmpool.OfficialTierLow},
			}},
		},
	}
	reg := &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p-high"}, {ID: "p-mid"}, {ID: "p-low"}, {ID: "p-new"}},
		ServiceGroups: []llmpool.ServiceGroup{*group},
	}
	ordered := []llmpool.DispatchProviderRoute{{ProviderID: "p-high", Model: llmpool.OfficialTierHigh}}
	got := proxyAppendSameGroupRateLimitRoutes(ordered, reg, group, llmpool.OfficialTierHigh, "", acceptLiveProvider)
	var models []string
	for _, route := range got {
		if route.ProviderID == "p-new" || route.Model == llmpool.OfficialTierMid || route.Model == llmpool.OfficialTierLow {
			models = append(models, route.ProviderID+":"+route.Model)
		}
	}
	midAt, lowAt := -1, -1
	for i, item := range models {
		if strings.HasSuffix(item, ":"+llmpool.OfficialTierMid) && midAt < 0 {
			midAt = i
		}
		if strings.HasSuffix(item, ":"+llmpool.OfficialTierLow) && lowAt < 0 {
			lowAt = i
		}
	}
	if midAt < 0 || lowAt < 0 || midAt > lowAt {
		t.Fatalf("429 extras = %#v, want official-mid before official-low", models)
	}
	for _, route := range got {
		if route.ProviderID == "p-new" && route.Model == llmpool.OfficialTierHigh {
			t.Fatalf("429 extras = %#v, low-only provider must not serve official-high", got)
		}
	}
}

func TestOrderProxyDispatchRoutesLoadBalancesOfficialQualityBands(t *testing.T) {
	for _, tier := range []string{llmpool.OfficialTierHigh, llmpool.OfficialTierMid, llmpool.OfficialTierLow} {
		t.Run(tier, func(t *testing.T) {
			proxyDispatchWRR.Reset()
			firstID, secondID := tier+"-a", tier+"-b"
			reg := officialQualityWRRRegistry(tier, firstID, secondID, nil, nil)
			group := &reg.ServiceGroups[0]
			model := buildDispatchModel(reg, findGroupModelConfig(group, tier))
			scored := llmpool.OrderScoredProviderRoutes(nil, model)
			first := orderProxyDispatchRoutes(nil, reg, group, tier, "", scored, acceptLiveProvider, time.Time{}, false)
			second := orderProxyDispatchRoutes(nil, reg, group, tier, "", scored, acceptLiveProvider, time.Time{}, false)
			if len(first) < 2 || first[0].ProviderID != firstID {
				t.Fatalf("first pick = %#v, want %s", first, firstID)
			}
			if second[0].ProviderID != secondID {
				t.Fatalf("second pick = %s, want %s so concurrent %s requests split vendors", second[0].ProviderID, secondID, tier)
			}
		})
	}
}

func TestOrderProxyDispatchRoutesFindsProviderIDsCaseInsensitively(t *testing.T) {
	proxyDispatchWRR.Reset()
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "mid-a", Sequence: 1, MaxConcurrency: 10},
			{ID: "mid-b", Sequence: 2, MaxConcurrency: 10},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: llmpool.OfficialGroupID,
			Models: []llmpool.ModelConfig{{
				Name: llmpool.OfficialTierMid,
				ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "Mid-A"},
					{ProviderID: "Mid-B"},
				},
			}},
		}},
	}
	group := &reg.ServiceGroups[0]
	model := buildDispatchModel(reg, findGroupModelConfig(group, llmpool.OfficialTierMid))
	scored := llmpool.OrderScoredProviderRoutes(nil, model)
	first := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierMid, "", scored, acceptLiveProvider, time.Time{}, false)
	second := orderProxyDispatchRoutes(nil, reg, group, llmpool.OfficialTierMid, "", scored, acceptLiveProvider, time.Time{}, false)
	if len(first) < 2 || first[0].ProviderID != "Mid-A" {
		t.Fatalf("first pick = %#v, want Mid-A after case-insensitive registry lookup", first)
	}
	if second[0].ProviderID != "Mid-B" {
		t.Fatalf("second pick = %s, want Mid-B in the WRR pool", second[0].ProviderID)
	}
}

func TestOrderProxyDispatchRoutesLoadBalancesAutoSameCapabilityTags(t *testing.T) {
	proxyDispatchWRR.Reset()
	reg := officialQualityWRRRegistry("auto", "tools-a", "tools-b", []string{"tools"}, map[string]int{"tools-a": 10, "tools-b": 90})
	group := &reg.ServiceGroups[0]
	model := buildDispatchModel(reg, findGroupModelConfig(group, "auto"))
	body := map[string]any{"tools": []any{map[string]any{"type": "function"}}}
	scored := llmpool.OrderScoredProviderRoutes(body, model)
	if len(scored) != 2 || scored[0].Score != scored[1].Score {
		t.Fatalf("auto same-tag scores = %#v, want one WRR band", scored)
	}
	first := orderProxyDispatchRoutes(nil, reg, group, "auto", "", scored, acceptLiveProvider, time.Time{}, false)
	second := orderProxyDispatchRoutes(nil, reg, group, "auto", "", scored, acceptLiveProvider, time.Time{}, false)
	if len(first) < 2 || first[0].ProviderID != "tools-a" {
		t.Fatalf("first pick = %#v, want tools-a", first)
	}
	if second[0].ProviderID != "tools-b" {
		t.Fatalf("second pick = %s, want tools-b so same-tag auto providers share load", second[0].ProviderID)
	}
}

func officialQualityWRRRegistry(modelName, firstID, secondID string, tags []string, priorities map[string]int) *Registry {
	priority := func(id string, fallback int) int {
		if priorities == nil {
			return fallback
		}
		if p, ok := priorities[id]; ok {
			return p
		}
		return fallback
	}
	return &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: firstID, Sequence: 1, MaxConcurrency: 10},
			{ID: secondID, Sequence: 2, MaxConcurrency: 10},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:   llmpool.OfficialGroupID,
			Kind: llmpool.ServiceGroupKindDynamic,
			Models: []llmpool.ModelConfig{{
				Name: modelName,
				ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: firstID, CapabilityTags: append([]string(nil), tags...), Priority: priority(firstID, 10)},
					{ProviderID: secondID, CapabilityTags: append([]string(nil), tags...), Priority: priority(secondID, 90)},
				},
			}},
		}},
	}
}

func officialDynamicFixtureGroup() llmpool.ServiceGroup {
	return llmpool.ServiceGroup{
		ID:     llmpool.OfficialGroupID,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
		},
	}
}
