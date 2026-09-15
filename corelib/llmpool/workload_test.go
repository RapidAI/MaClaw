package llmpool

import (
	"net/http"
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
	if len(got) != 1 || got[0] != "auto" {
		t.Fatalf("default catalog = %#v, want [auto]", got)
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
