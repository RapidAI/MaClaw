package llmpool

import (
	"net/http"
	"strings"
	"testing"
)

func TestClassifyWorkloadP0Hint(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	class, source := ClassifyWorkload(ClassifyInput{
		Header: header,
		Body:   map[string]any{BodyWorkloadClassKey: "code", "messages": []any{map[string]any{"content": "写商业计划"}}},
	})
	if class != WorkloadClassPlan || source != ClassSourceHint {
		t.Fatalf("got class=%s source=%s, want plan/hint", class, source)
	}
}

func TestClassifyWorkloadP0IgnoresInvalidHint(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "vision")
	class, source := ClassifyWorkload(ClassifyInput{
		Header: header,
		Body:   map[string]any{"messages": []any{map[string]any{"content": "hello"}}},
	})
	if class != WorkloadClassChat || source != ClassSourceHeuristic {
		t.Fatalf("got class=%s source=%s, want chat/heuristic", class, source)
	}
}

func TestClassifyWorkloadP1PhaseWinsOverWorkflow(t *testing.T) {
	header := http.Header{}
	header.Set(PhaseKindHeader, "artifact_generation")
	header.Set(WorkflowTypeHeader, "business_plan")
	class, source := ClassifyWorkload(ClassifyInput{Header: header})
	if class != WorkloadClassDocWrite || source != ClassSourceWorkflow {
		t.Fatalf("got class=%s source=%s, want doc_write/workflow", class, source)
	}
}

func TestClassifyWorkloadP1NSFCAndCodingPhases(t *testing.T) {
	header := http.Header{}
	header.Set(WorkflowTypeHeader, "nsfc_youth")
	class, source := ClassifyWorkload(ClassifyInput{Header: header})
	if class != WorkloadClassPlan || source != ClassSourceWorkflow {
		t.Fatalf("nsfc = %s/%s, want plan/workflow", class, source)
	}
	header = http.Header{}
	header.Set(PhaseKindHeader, "implementation")
	class, source = ClassifyWorkload(ClassifyInput{Header: header})
	if class != WorkloadClassCode || source != ClassSourceWorkflow {
		t.Fatalf("implementation = %s/%s, want code/workflow", class, source)
	}
}

func TestClassifyWorkloadP2FastIsChat(t *testing.T) {
	header := http.Header{}
	header.Set(TaskTypeHeader, "fast")
	class, source := ClassifyWorkload(ClassifyInput{Header: header})
	if class != WorkloadClassChat || source != ClassSourceTaskType {
		t.Fatalf("got class=%s source=%s, want chat/task_type", class, source)
	}
}

func TestClassifyWorkloadP2TaskTypeCannotDecidePlan(t *testing.T) {
	header := http.Header{}
	header.Set(TaskTypeHeader, "reasoning")
	class, source := ClassifyWorkload(ClassifyInput{Header: header})
	if class != WorkloadClassCode || source != ClassSourceTaskType {
		t.Fatalf("got class=%s source=%s, want code/task_type", class, source)
	}
}

func TestClassifyWorkloadP3BusinessPlanWriteIsDocWrite(t *testing.T) {
	class, source := ClassifyWorkload(ClassifyInput{
		Body: map[string]any{"messages": []any{map[string]any{"content": "请写商业计划书全文"}}},
	})
	if class != WorkloadClassDocWrite || source != ClassSourceHeuristic {
		t.Fatalf("got class=%s source=%s, want doc_write/heuristic", class, source)
	}
}

func TestClassifyWorkloadP4Balanced(t *testing.T) {
	class, source := ClassifyWorkload(ClassifyInput{Body: map[string]any{"messages": []any{map[string]any{"content": "xyzzy"}}}})
	if class != WorkloadFallbackBalanced || source != ClassSourceFallback {
		t.Fatalf("got class=%s source=%s, want balanced/fallback", class, source)
	}
}

func TestRouteWorkloadClassOptionalFallsBackToBalanced(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Routes: []WorkloadRoute{
			{Class: WorkloadClassPlan, Model: OfficialTierHigh, Quality: QualityHigh},
			{Class: WorkloadClassDesign, Model: OfficialTierHigh, Quality: QualityHigh},
			{Class: WorkloadClassDocWrite, Model: OfficialTierMid, Quality: QualityMid},
			{Class: WorkloadClassCode, Model: OfficialTierMid, Quality: QualityMid},
			{Class: WorkloadFallbackBalanced, Model: OfficialTierMid, Quality: QualityMid},
		},
	}
	routed, model, quality := RouteWorkloadClass(group, WorkloadClassReview)
	if routed != WorkloadFallbackBalanced || model != OfficialTierMid || quality != QualityMid {
		t.Fatalf("got routed=%s model=%s quality=%s", routed, model, quality)
	}
}

func TestClassifyAndRouteSkipsL1ForConcreteModel(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	group := &ServiceGroup{Kind: ServiceGroupKindDynamic, Routes: DefaultOfficialAutoRoutes()}
	dec := ClassifyAndRouteModel(header, map[string]any{"model": OfficialTierHigh}, group, OfficialTierHigh)
	if !dec.Passthrough || dec.ResolvedModel != OfficialTierHigh || dec.Class != WorkloadClassPlan {
		t.Fatalf("unexpected decision %#v", dec)
	}
}

func TestClassifyAndRoutePinUsesWorkflowForAvailability(t *testing.T) {
	header := http.Header{}
	header.Set(WorkflowTypeHeader, "business_plan")
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	dec := ClassifyAndRouteModel(header, map[string]any{"model": OfficialTierHigh}, group, OfficialTierHigh)
	if !dec.Passthrough || dec.Class != WorkloadClassPlan || dec.Source != ClassSourceWorkflow {
		t.Fatalf("decision = %#v, want pin passthrough plan/workflow", dec)
	}
	if dec.ResolvedModel != OfficialTierMid || !dec.AvailabilityFallback {
		t.Fatalf("decision = %#v, want official-mid, not low", dec)
	}
}

func TestClassifyAndRoutePinPlanDoesNotLandOnLow(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh},
			{Name: OfficialTierMid},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	dec := ClassifyAndRouteModel(nil, map[string]any{"model": OfficialTierHigh}, group, OfficialTierHigh)
	if dec.ResolvedModel != OfficialTierLow || !dec.AvailabilityFallback {
		t.Fatalf("unclassified pin fallback = %#v, want official-low", dec)
	}
	header := http.Header{}
	header.Set(WorkflowTypeHeader, "business_plan")
	dec = ClassifyAndRouteModel(header, map[string]any{"model": OfficialTierHigh}, group, OfficialTierHigh)
	if dec.AvailabilityFallback || dec.ResolvedModel != OfficialTierHigh {
		t.Fatalf("plan pin must not land on low, got %#v", dec)
	}
}

func TestClassifyAndRoutePlanDoesNotUseLowRouteTarget(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh, ProviderIDs: []string{"p-high"}},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: []WorkloadRoute{{Class: WorkloadClassPlan, Model: OfficialTierLow, Quality: QualityLow}},
	}
	dec := ClassifyAndRouteModel(header, map[string]any{"model": "auto"}, group, "auto")
	if dec.ResolvedModel == OfficialTierLow {
		t.Fatalf("plan must not resolve to official-low, got %#v", dec)
	}
	if dec.ResolvedModel != OfficialTierMid && dec.ResolvedModel != OfficialTierHigh {
		t.Fatalf("plan = %#v, want official-mid or official-high", dec)
	}
}

func TestClassifyAndRoutePlanDoesNotUseLowQualityConcreteModel(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh, ProviderIDs: []string{"p-high"}},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: "gpt-4o-mini", ProviderIDs: []string{"p-mini"}},
		},
		Routes: []WorkloadRoute{{Class: WorkloadClassPlan, Model: "gpt-4o-mini", Quality: QualityLow}},
	}
	dec := ClassifyAndRouteModel(header, map[string]any{"model": "auto"}, group, "auto")
	if dec.ResolvedModel == "gpt-4o-mini" || dec.ResolvedModel == OfficialTierLow {
		t.Fatalf("plan must not resolve to low-quality model, got %#v", dec)
	}
	if dec.ResolvedModel != OfficialTierMid && dec.ResolvedModel != OfficialTierHigh {
		t.Fatalf("plan = %#v, want official-mid or official-high", dec)
	}
}

func TestClassifyAndRoutePinLowStaysOnLowForPlan(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh, ProviderIDs: []string{"p-high"}},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	dec := ClassifyAndRouteModel(header, map[string]any{"model": OfficialTierLow}, group, OfficialTierLow)
	if !dec.Passthrough || dec.ResolvedModel != OfficialTierLow {
		t.Fatalf("pin of official-low must stay, got %#v", dec)
	}
}

func TestClassifyAndRoutePinFindsPublishedLowAlias(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, WorkloadClassPlan)
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: "low", ProviderIDs: []string{"p-low"}},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	dec := ClassifyAndRouteModel(header, map[string]any{"model": OfficialTierLow}, group, OfficialTierLow)
	if !dec.Passthrough || dec.ResolvedModel != OfficialTierLow || dec.AvailabilityFallback {
		t.Fatalf("published low row must satisfy an official-low pin, got %#v", dec)
	}
}

func TestValidateDynamicServiceGroupRequiredRoutes(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh},
			{Name: OfficialTierMid},
		},
		Routes: []WorkloadRoute{
			{Class: WorkloadClassPlan, Model: OfficialTierHigh, Quality: QualityHigh},
		},
	}
	if err := ValidateDynamicServiceGroup(group); err == nil {
		t.Fatal("expected missing required routes")
	}
}

func TestValidateDynamicServiceGroupRejectsPlanToLow(t *testing.T) {
	group := officialDynamicFixture()
	for i := range group.Routes {
		if group.Routes[i].Class == WorkloadClassPlan {
			group.Routes[i].Model = OfficialTierLow
			group.Routes[i].Quality = QualityLow
		}
	}
	if err := ValidateDynamicServiceGroup(group); err == nil {
		t.Fatal("expected plan/low to be rejected")
	}
}

func TestValidateDynamicServiceGroupOK(t *testing.T) {
	if err := ValidateDynamicServiceGroup(officialDynamicFixture()); err != nil {
		t.Fatalf("ValidateDynamicServiceGroup() error = %v", err)
	}
}

func TestUpgradeModelInBandDoesNotLeaveQuality(t *testing.T) {
	group := officialDynamicFixture()
	group.Models = []ModelConfig{
		{Name: OfficialTierMid, CapabilityTags: []string{"chat"}},
		{Name: OfficialTierHigh, CapabilityTags: []string{"tools", "vision"}},
		{Name: "mid-tools", CapabilityTags: []string{"tools"}},
	}
	group.Routes = append(group.Routes, WorkloadRoute{Class: "extra", Model: "mid-tools", Quality: QualityMid})
	upgraded, ok := upgradeModelInBand(group, OfficialTierMid, QualityMid, map[string]int{"tools": 8})
	if !ok || upgraded != "mid-tools" {
		t.Fatalf("got upgraded=%s ok=%v, want mid-tools", upgraded, ok)
	}
}

func TestPublicCatalogModelsDynamicDefaultsToAuto(t *testing.T) {
	got := PublicCatalogModels(&ServiceGroup{
		Kind:   ServiceGroupKindDynamic,
		Models: []ModelConfig{{Name: OfficialTierHigh}, {Name: OfficialTierMid}},
	})
	want := DefaultCapabilityCatalog()
	if len(got) != len(want) {
		t.Fatalf("default catalog = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default catalog = %#v, want %#v", got, want)
		}
	}
	got = PublicCatalogModels(&ServiceGroup{
		Kind:          ServiceGroupKindDynamic,
		ExposedModels: []string{" auto ", "official-high", ""},
	})
	if len(got) != 2 || got[0] != "auto" || got[1] != "official-high" {
		t.Fatalf("exposed catalog = %#v", got)
	}
	got = PublicCatalogModels(&ServiceGroup{
		Kind:   ServiceGroupKindStatic,
		Models: []ModelConfig{{Name: "gpt-4o"}, {Name: "auto"}},
	})
	if len(got) != 2 || got[0] != "gpt-4o" || got[1] != "auto" {
		t.Fatalf("static catalog = %#v", got)
	}
}

func officialDynamicFixture() *ServiceGroup {
	return &ServiceGroup{
		ID:     OfficialGroupID,
		Kind:   ServiceGroupKindDynamic,
		Models: []ModelConfig{{Name: OfficialTierHigh}, {Name: OfficialTierMid}, {Name: OfficialTierLow}},
		Routes: DefaultOfficialAutoRoutes(),
	}
}

func TestFallbackAvailableModelDowngradesHighToMid(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	got, ok := FallbackAvailableModel(group, OfficialTierHigh, WorkloadClassPlan)
	if !ok || got != OfficialTierMid {
		t.Fatalf("plan high fallback = %s ok=%v, want official-mid", got, ok)
	}
	got, ok = FallbackAvailableModel(group, OfficialTierHigh, WorkloadClassChat)
	if !ok || got != OfficialTierMid {
		t.Fatalf("chat high fallback = %s ok=%v, want official-mid first", got, ok)
	}
}

func TestFallbackAvailableModelDoesNotSendPlanToLow(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh},
			{Name: OfficialTierMid},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	got, ok := FallbackAvailableModel(group, OfficialTierHigh, WorkloadClassPlan)
	if ok || got != OfficialTierHigh {
		t.Fatalf("plan must not fall through to low, got %s ok=%v", got, ok)
	}
	got, ok = FallbackAvailableModel(group, OfficialTierHigh, WorkloadClassChat)
	if !ok || got != OfficialTierLow {
		t.Fatalf("chat high→low = %s ok=%v", got, ok)
	}
}

func TestFallbackAvailableModelUpgradesLowToMid(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh, ProviderIDs: []string{"p-high"}},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: OfficialTierLow},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	got, ok := FallbackAvailableModel(group, OfficialTierLow, WorkloadClassChat)
	if !ok || got != OfficialTierMid {
		t.Fatalf("low upgrade = %s ok=%v, want official-mid", got, ok)
	}
}

func TestRouteWorkloadClassFallsBackWhenHighMissing(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	routed, model, quality := RouteWorkloadClass(group, WorkloadClassPlan)
	if routed != WorkloadClassPlan || model != OfficialTierMid || quality != QualityMid {
		t.Fatalf("got routed=%s model=%s quality=%s, want plan/official-mid/mid", routed, model, quality)
	}
}

func TestClassifyAndRouteAutoFallsBackWhenHighUnavailable(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	group := &ServiceGroup{
		ID:   OfficialGroupID,
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh},
			{Name: OfficialTierMid, ProviderIDs: []string{"p-mid"}},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	dec := ClassifyAndRoute(header, map[string]any{"model": "auto"}, group)
	if dec.ResolvedModel != OfficialTierMid || !dec.AvailabilityFallback {
		t.Fatalf("decision = %#v, want official-mid with availability fallback", dec)
	}
	if dec.Attribution.SelectionReason != "official tier availability fallback" {
		t.Fatalf("reason = %q", dec.Attribution.SelectionReason)
	}
}

func TestClassifyAndRouteFillsAttribution(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	group := officialDynamicFixture()
	dec := ClassifyAndRoute(header, map[string]any{"model": "auto"}, group)
	attr := dec.Attribution
	if attr.RequestedGroup != OfficialGroupID || attr.RequestedModel != "auto" {
		t.Fatalf("request = %#v", attr)
	}
	if attr.WorkloadClass != WorkloadClassPlan || attr.ClassSource != ClassSourceHint {
		t.Fatalf("class = %#v", attr)
	}
	if attr.ResolvedModel != OfficialTierHigh || attr.UpstreamModel != OfficialTierHigh || attr.OfficialProviderPool != OfficialGroupID {
		t.Fatalf("resolved = %#v", attr)
	}
	if attr.SelectionReason != "dynamic workload route" {
		t.Fatalf("reason = %q", attr.SelectionReason)
	}
}

func TestHeuristicUsesLatestUserText(t *testing.T) {
	chatBody := map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": "你好"},
		map[string]any{"role": "tool", "content": "部署 kubernetes rollback"},
	}}
	class, source := ClassifyWorkload(ClassifyInput{Body: chatBody})
	if class != WorkloadClassChat || source != ClassSourceHeuristic {
		t.Fatalf("chat continuation class=%s source=%s", class, source)
	}
	codeBody := map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": "实现这个函数"},
		map[string]any{"role": "tool", "content": "你好"},
	}}
	class, source = ClassifyWorkload(ClassifyInput{Body: codeBody})
	if class != WorkloadClassCode || source != ClassSourceHeuristic {
		t.Fatalf("code continuation class=%s source=%s", class, source)
	}
}

func TestDegreeGateLowersSimpleFallback(t *testing.T) {
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "加个注释"}},
	}, officialDynamicFixture(), "auto")
	if dec.Class != WorkloadFallbackBalanced || dec.Source != ClassSourceFallback {
		t.Fatalf("class = %s/%s", dec.Class, dec.Source)
	}
	if dec.ResolvedModel != OfficialTierLow || dec.Degree != DegreeSimple || !dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
	if dec.Attribution.SelectionReason != "workload degree gate" {
		t.Fatalf("reason = %q", dec.Attribution.SelectionReason)
	}
}

func TestDegreeGateKeepsReasoningOnMid(t *testing.T) {
	header := http.Header{}
	header.Set(TaskTypeHeader, "reasoning")
	dec := ClassifyAndRouteModel(header, map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "加个注释"}},
	}, officialDynamicFixture(), "auto")
	if dec.Class != WorkloadClassCode || dec.Source != ClassSourceTaskType {
		t.Fatalf("class = %s/%s", dec.Class, dec.Source)
	}
	if dec.ResolvedModel != OfficialTierMid || dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestDegreeGateRaisesHardChat(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, WorkloadClassChat)
	dec := ClassifyAndRouteModel(header, map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "为什么这个锁会丢"}},
	}, officialDynamicFixture(), "auto")
	if dec.Class != WorkloadClassChat || dec.ResolvedModel != OfficialTierMid || dec.Degree != DegreeComplex || !dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestDegreeGateDoesNotLowerPlanOrPin(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, WorkloadClassPlan)
	dec := ClassifyAndRouteModel(header, map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "加个注释"}},
	}, officialDynamicFixture(), "auto")
	if dec.ResolvedModel != OfficialTierHigh || dec.DegreeApplied {
		t.Fatalf("plan = %#v", dec)
	}
	pinned := ClassifyAndRouteModel(nil, map[string]any{
		"model":    OfficialTierMid,
		"messages": []any{map[string]any{"role": "user", "content": "加个注释"}},
	}, officialDynamicFixture(), OfficialTierMid)
	if !pinned.Passthrough || pinned.ResolvedModel != OfficialTierMid || pinned.DegreeApplied {
		t.Fatalf("pin = %#v", pinned)
	}
}

func TestDegreeGateKeepsShortCodeOnMid(t *testing.T) {
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "fix bug"}},
	}, officialDynamicFixture(), "auto")
	if dec.Class != WorkloadClassCode || dec.Source != ClassSourceHeuristic {
		t.Fatalf("class = %s/%s", dec.Class, dec.Source)
	}
	if dec.ResolvedModel != OfficialTierMid || dec.Degree == DegreeSimple || dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestDegreeGateKeepsVisionOnMid(t *testing.T) {
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model": "auto",
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "加个注释"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
		}}},
	}, officialDynamicFixture(), "auto")
	if dec.Class != WorkloadFallbackBalanced || dec.Source != ClassSourceFallback {
		t.Fatalf("class = %s/%s", dec.Class, dec.Source)
	}
	if dec.ResolvedModel != OfficialTierMid || dec.Degree != DegreeSimple || dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestVisionClimbUsesTheNextBandThatCanSee(t *testing.T) {
	image := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "你好"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
	}}}}
	blindLow := officialDynamicFixture()
	blindLow.Models[1].CapabilityTags = []string{"vision"}
	dec := ClassifyAndRouteModel(nil, image, blindLow, "auto")
	if dec.Class != WorkloadClassChat || dec.ResolvedModel != OfficialTierMid || !dec.Upgraded || dec.DegreeApplied {
		t.Fatalf("blind low = %#v", dec)
	}
	seeingLow := officialDynamicFixture()
	seeingLow.Models[2].CapabilityTags = []string{"vision"}
	dec = ClassifyAndRouteModel(nil, image, seeingLow, "auto")
	if dec.ResolvedModel != OfficialTierLow || dec.Upgraded {
		t.Fatalf("seeing low = %#v", dec)
	}
	onlyHigh := officialDynamicFixture()
	onlyHigh.Models[0].CapabilityTags = []string{"vision"}
	dec = ClassifyAndRouteModel(nil, map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "加个注释"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
	}}}}, onlyHigh, "auto")
	if dec.Class != WorkloadFallbackBalanced || dec.ResolvedModel != OfficialTierHigh || !dec.Upgraded {
		t.Fatalf("only high = %#v", dec)
	}
	untagged := ClassifyAndRouteModel(nil, image, officialDynamicFixture(), "auto")
	if untagged.ResolvedModel != OfficialTierLow || untagged.Upgraded {
		t.Fatalf("untagged = %#v", untagged)
	}
}

func TestVisionClimbReturnsToTheBandThatCanSee(t *testing.T) {
	header := http.Header{}
	header.Set(WorkloadClassHeader, WorkloadClassChat)
	onlyLow := officialDynamicFixture()
	onlyLow.Models[2].CapabilityTags = []string{"vision"}
	dec := ClassifyAndRouteModel(header, map[string]any{
		"model": "auto",
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "为什么看不清"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
		}}},
	}, onlyLow, "auto")
	if dec.Class != WorkloadClassChat || dec.ResolvedModel != OfficialTierLow || !dec.Upgraded || dec.DegreeApplied {
		t.Fatalf("seeing low = %#v", dec)
	}
	plain := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "加个注释"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
	}}}}
	dec = ClassifyAndRouteModel(nil, plain, onlyLow, "auto")
	if dec.Class != WorkloadFallbackBalanced || dec.ResolvedModel != OfficialTierLow || !dec.Upgraded {
		t.Fatalf("simple image = %#v", dec)
	}
	longBody := map[string]any{"model": "auto", "messages": []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "加个注释"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
		}},
		map[string]any{"role": "tool", "content": strings.Repeat("x", capabilityLongTextBytes)},
	}}
	dec = ClassifyAndRouteModel(nil, longBody, onlyLow, "auto")
	if dec.ResolvedModel != OfficialTierMid || dec.Upgraded {
		t.Fatalf("long image = %#v", dec)
	}
	planHeader := http.Header{}
	planHeader.Set(WorkloadClassHeader, WorkloadClassPlan)
	dec = ClassifyAndRouteModel(planHeader, plain, onlyLow, "auto")
	if dec.ResolvedModel != OfficialTierHigh || dec.Upgraded {
		t.Fatalf("plan = %#v", dec)
	}
	onlyMid := officialDynamicFixture()
	onlyMid.Models[1].CapabilityTags = []string{"vision"}
	dec = ClassifyAndRouteModel(planHeader, plain, onlyMid, "auto")
	if dec.ResolvedModel != OfficialTierMid || !dec.Upgraded {
		t.Fatalf("plan mid = %#v", dec)
	}
}

func TestVisionUpgradePrefersSeeingModelInBand(t *testing.T) {
	group := officialDynamicFixture()
	group.Models[1].CapabilityTags = []string{"tools"}
	group.Models = append(group.Models, ModelConfig{Name: "mid-vision", CapabilityTags: []string{"vision"}})
	group.Routes = append(group.Routes, WorkloadRoute{Class: "extra", Model: "mid-vision", Quality: QualityMid})
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model": "auto",
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "edit"}}},
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "加个注释"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
		}}},
	}, group, "auto")
	if dec.ResolvedModel != "mid-vision" || !dec.Upgraded {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestVisionUpgradeSkipsUndialableSibling(t *testing.T) {
	group := officialDynamicFixture()
	group.Models[0].ProviderIDs = []string{"p-high"}
	group.Models[0].CapabilityTags = []string{"vision"}
	group.Models[1].ProviderIDs = []string{"p-mid"}
	group.Models[2].ProviderIDs = []string{"p-low"}
	group.Models = append(group.Models, ModelConfig{Name: "mid-vision", CapabilityTags: []string{"vision"}})
	group.Routes = append(group.Routes, WorkloadRoute{Class: "extra", Model: "mid-vision", Quality: QualityMid})
	needs := map[string]int{"vision": 8}
	if got, ok := upgradeModelInBand(group, OfficialTierMid, QualityMid, needs); ok || got != OfficialTierMid {
		t.Fatalf("in-band = %s ok=%v, want usable official-mid", got, ok)
	}
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model": "auto",
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "加个注释"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
		}}},
	}, group, "auto")
	if dec.ResolvedModel != OfficialTierHigh || !dec.Upgraded {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestAutoClientRewriteClimbsOffABlindOfficialTier(t *testing.T) {
	group := officialDynamicFixture()
	group.Models[1].CapabilityTags = []string{"vision"}
	image := map[string]any{"model": OfficialTierLow, "messages": []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "你好"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
	}}}}
	header := http.Header{}
	header.Set(ClientModelHeader, "auto")
	header.Set(WorkloadClassHeader, WorkloadClassChat)
	dec := ClassifyAndRouteModel(header, image, group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierMid || !dec.Upgraded || dec.Attribution.SelectionReason != "dynamic workload route" {
		t.Fatalf("auto rewrite = %#v", dec)
	}
	pin := http.Header{}
	pin.Set(ClientModelHeader, "low")
	dec = ClassifyAndRouteModel(pin, image, group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierLow || dec.Upgraded || dec.Attribution.SelectionReason != "pinned model" {
		t.Fatalf("pin = %#v", dec)
	}
	dec = ClassifyAndRouteModel(nil, image, group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierLow || dec.Upgraded {
		t.Fatalf("direct pin = %#v", dec)
	}
	textHeader := http.Header{}
	textHeader.Set(ClientModelHeader, "auto")
	dec = ClassifyAndRouteModel(textHeader, map[string]any{
		"model":    OfficialTierLow,
		"messages": []any{map[string]any{"role": "user", "content": "你好"}},
	}, group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierLow || dec.Upgraded {
		t.Fatalf("text rewrite = %#v", dec)
	}
	plan := http.Header{}
	plan.Set(ClientModelHeader, "auto")
	plan.Set(WorkloadClassHeader, WorkloadClassPlan)
	onlyLow := officialDynamicFixture()
	onlyLow.Models[2].CapabilityTags = []string{"vision"}
	dec = ClassifyAndRouteModel(plan, image, onlyLow, OfficialTierHigh)
	if dec.ResolvedModel != OfficialTierHigh || dec.Upgraded {
		t.Fatalf("plan rewrite = %#v", dec)
	}
}

func TestAutoRewriteLiftsLongImageOffLow(t *testing.T) {
	group := officialDynamicFixture()
	group.Models[2].CapabilityTags = []string{"vision"}
	longImage := func(text, model string) map[string]any {
		return map[string]any{
			"model": model,
			"messages": []any{
				map[string]any{"role": "user", "content": []any{
					map[string]any{"type": "text", "text": text},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
				}},
				map[string]any{"role": "tool", "content": strings.Repeat("x", capabilityLongTextBytes)},
			},
		}
	}
	code := http.Header{}
	code.Set(ClientModelHeader, "auto")
	code.Set(WorkloadClassHeader, WorkloadClassCode)
	dec := ClassifyAndRouteModel(code, longImage("加个注释", OfficialTierLow), group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierMid || !dec.Upgraded {
		t.Fatalf("code retry = %#v, want official-mid", dec)
	}
	chat := http.Header{}
	chat.Set(ClientModelHeader, "auto")
	chat.Set(WorkloadClassHeader, WorkloadClassChat)
	dec = ClassifyAndRouteModel(chat, longImage("你好", OfficialTierLow), group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierLow || dec.Upgraded {
		t.Fatalf("chat = %#v, want official-low", dec)
	}
	dec = ClassifyAndRouteModel(chat, longImage("为什么看不清", OfficialTierLow), group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierMid || !dec.Upgraded {
		t.Fatalf("complex chat = %#v, want official-mid", dec)
	}
	pin := http.Header{}
	pin.Set(ClientModelHeader, "low")
	pin.Set(WorkloadClassHeader, WorkloadClassCode)
	dec = ClassifyAndRouteModel(pin, longImage("加个注释", OfficialTierLow), group, OfficialTierLow)
	if dec.ResolvedModel != OfficialTierLow || dec.Upgraded {
		t.Fatalf("pin = %#v, want official-low", dec)
	}
	plan := http.Header{}
	plan.Set(ClientModelHeader, "auto")
	plan.Set(WorkloadClassHeader, WorkloadClassPlan)
	dec = ClassifyAndRouteModel(plan, map[string]any{
		"model": OfficialTierLow,
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "架构怎么拆"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
		}}},
	}, group, OfficialTierLow)
	if dec.ResolvedModel == OfficialTierLow {
		t.Fatalf("plan = %#v, want a higher band", dec)
	}
}

func TestVisionClimbThenUpgradesInsideTheBandThatCanSee(t *testing.T) {
	group := officialDynamicFixture()
	for i := range group.Models {
		group.Models[i].ProviderIDs = []string{"p"}
	}
	group.Models[1].CapabilityTags = []string{"vision"}
	group.Models = append(group.Models, ModelConfig{
		Name:           "mid-both",
		ProviderIDs:    []string{"p-both"},
		CapabilityTags: []string{"vision", "tools"},
	})
	group.Routes = append(group.Routes, WorkloadRoute{Class: "extra", Model: "mid-both", Quality: QualityMid})
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model": "auto",
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "edit"}}},
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "你好"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aaaa"}},
		}}},
	}, group, "auto")
	if dec.Class != WorkloadClassChat || dec.ResolvedModel != "mid-both" || !dec.Upgraded {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestToolsDoNotClimbWhenLowLacksTools(t *testing.T) {
	group := officialDynamicFixture()
	group.Models[1].CapabilityTags = []string{"tools"}
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model":    "auto",
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "edit"}}},
		"messages": []any{map[string]any{"role": "user", "content": "加个注释"}},
	}, group, "auto")
	if dec.ResolvedModel != OfficialTierLow || !dec.DegreeApplied || dec.Upgraded {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestDegreeGateKeepsLongTranscriptOnMid(t *testing.T) {
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model": "auto",
		"messages": []any{
			map[string]any{"role": "user", "content": "加个注释"},
			map[string]any{"role": "tool", "content": strings.Repeat("x", capabilityLongTextBytes)},
		},
	}, officialDynamicFixture(), "auto")
	if dec.Class != WorkloadFallbackBalanced || dec.ResolvedModel != OfficialTierMid || dec.Degree != DegreeSimple || dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestDegreeGateLowersAtExactWindowLimit(t *testing.T) {
	user := "加个注释"
	// extractRequestText joins message texts with one space. The window tag
	// is strict greater-than, so a transcript of exactly that many bytes
	// still counts as a short edit.
	toolLen := capabilityLongTextBytes - len(user) - 1
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model": "auto",
		"messages": []any{
			map[string]any{"role": "user", "content": user},
			map[string]any{"role": "tool", "content": strings.Repeat("x", toolLen)},
		},
	}, officialDynamicFixture(), "auto")
	if dec.ResolvedModel != OfficialTierLow || dec.Degree != DegreeSimple || !dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestToolsDoNotBlockSimpleLower(t *testing.T) {
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model":    "auto",
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "edit"}}},
		"messages": []any{map[string]any{"role": "user", "content": "加个注释"}},
	}, officialDynamicFixture(), "auto")
	if dec.ResolvedModel != OfficialTierLow || !dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestToolWrappedTurnKeepsEarlierUserText(t *testing.T) {
	cases := []struct {
		name string
		last any
	}{
		{
			name: "tool_result part",
			last: map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "call_1", "content": "部署 kubernetes rollback"},
			}},
		},
		{
			name: "tool_call_id item",
			last: map[string]any{"tool_call_id": "call_1", "content": "部署 kubernetes rollback"},
		},
		{
			name: "function_call_output",
			last: map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "部署 kubernetes rollback"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": "你好"},
				tc.last,
			}}
			class, source := ClassifyWorkload(ClassifyInput{Body: body})
			if class != WorkloadClassChat || source != ClassSourceHeuristic {
				t.Fatalf("class=%s source=%s", class, source)
			}
			if got := LatestUserPreview(body, 400); got != "你好" {
				t.Fatalf("preview=%q", got)
			}
		})
	}
}

func TestBusinessPlanWriteStaysOnMid(t *testing.T) {
	dec := ClassifyAndRouteModel(nil, map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"content": "请写商业计划书全文"}},
	}, officialDynamicFixture(), "auto")
	if dec.Class != WorkloadClassDocWrite || dec.ResolvedModel != OfficialTierMid || dec.DegreeApplied {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestQualityAvailabilityChainMidClimbsBeforeFalling(t *testing.T) {
	got := QualityAvailabilityChain(QualityMid, WorkloadClassCode)
	want := []string{QualityMid, QualityHigh, QualityLow}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("chain = %#v, want %#v", got, want)
	}
	plan := QualityAvailabilityChain(QualityMid, WorkloadClassPlan)
	if len(plan) != 2 || plan[0] != QualityMid || plan[1] != QualityHigh {
		t.Fatalf("plan chain = %#v", plan)
	}
}

func TestFallbackAvailableModelMidPrefersHigh(t *testing.T) {
	group := &ServiceGroup{
		Kind: ServiceGroupKindDynamic,
		Models: []ModelConfig{
			{Name: OfficialTierHigh, ProviderIDs: []string{"p-high"}},
			{Name: OfficialTierMid},
			{Name: OfficialTierLow, ProviderIDs: []string{"p-low"}},
		},
		Routes: DefaultOfficialAutoRoutes(),
	}
	got, ok := FallbackAvailableModel(group, OfficialTierMid, WorkloadClassCode)
	if !ok || got != OfficialTierHigh {
		t.Fatalf("mid fallback = %s ok=%v, want official-high", got, ok)
	}
}
