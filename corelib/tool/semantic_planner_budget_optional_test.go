package tool

import "testing"

func warehouseBudgetRegistry(t *testing.T) *CapabilityRegistry {
	t.Helper()
	registry := semanticRegistry(t)
	for _, id := range []CapabilityID{CapabilityKnowledgeReadLocal, CapabilityMemoryManageAgent} {
		if err := registry.Register(CapabilityDescriptor{
			ID: id, Version: "v1", Effects: []EffectClass{EffectReadOnly},
		}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	return registry
}

func warehouseBudgetNeeds() []CapabilityNeed {
	return []CapabilityNeed{
		{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
		{ID: "need:~ambient:knowledge.read.local", Capability: CapabilityKnowledgeReadLocal, Required: false},
		{ID: "need:~ambient:memory.manage.agent", Capability: CapabilityMemoryManageAgent, Required: false},
	}
}

func warehouseBudgetProviders() []ProviderSpec {
	return []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
		semanticProvider("knowledge_adapter", CapabilityKnowledgeReadLocal, nil, EffectReadOnly),
		semanticProvider("memory_adapter", CapabilityMemoryManageAgent, nil, EffectReadOnly),
	}
}

func omittedNeedIDs(plan ToolPlan) map[string]string {
	out := make(map[string]string, len(plan.Omitted))
	for _, item := range plan.Omitted {
		out[item.NeedID] = item.ReasonCode
	}
	return out
}

func TestPlanningBudgetKeepsRequiredAndOmitsOptionalOnTightSelectionBudget(t *testing.T) {
	registry := warehouseBudgetRegistry(t)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1",
		Snapshot: semanticSnapshot(t, registry, warehouseBudgetProviders()),
		Needs:    warehouseBudgetNeeds(),
		Budget:   PlanningBudget{MaxSelections: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("optional overflow must not unmet the required intent: %+v", plan.Unmet)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "capture" {
		t.Fatalf("MaxSelections=1 must keep required only: %+v", plan.Selections)
	}
	omitted := omittedNeedIDs(plan)
	if omitted["need:~ambient:knowledge.read.local"] != reasonOptionalBudgetOmitted {
		t.Fatalf("knowledge omit=%q plan=%+v", omitted["need:~ambient:knowledge.read.local"], plan.Omitted)
	}
	if omitted["need:~ambient:memory.manage.agent"] != reasonOptionalBudgetOmitted {
		t.Fatalf("memory omit=%q plan=%+v", omitted["need:~ambient:memory.manage.agent"], plan.Omitted)
	}
}

func TestPlanningBudgetUnlimitedPartitionsRequiredBeforeOptional(t *testing.T) {
	registry := warehouseBudgetRegistry(t)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1",
		Snapshot: semanticSnapshot(t, registry, warehouseBudgetProviders()),
		Needs:    warehouseBudgetNeeds(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 3 || plan.Selections[0].NeedID != "capture" {
		t.Fatalf("unlimited must start with required: %+v", plan.Selections)
	}
	if plan.Selections[1].NeedID != "need:~ambient:knowledge.read.local" || plan.Selections[2].NeedID != "need:~ambient:memory.manage.agent" {
		t.Fatalf("optional must follow required: %+v", plan.Selections)
	}
	if len(plan.Unmet) != 0 || len(plan.Omitted) != 0 {
		t.Fatalf("unlimited must keep both optional: unmet=%+v omitted=%+v", plan.Unmet, plan.Omitted)
	}
}

func TestPlanningBudgetOmitsOptionalWhenSchemaTokensFitRequiredOnly(t *testing.T) {
	registry := warehouseBudgetRegistry(t)
	providers := warehouseBudgetProviders()
	requiredCost := selectionSchemaTokenCost([]PlannedSelection{{ParameterAuthorization: providers[0].ParameterAuthorization}})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1",
		Snapshot: semanticSnapshot(t, registry, providers),
		Needs:    warehouseBudgetNeeds(),
		Budget:   PlanningBudget{MaxSchemaTokens: requiredCost + 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "capture" {
		t.Fatalf("schema budget must keep required only: %+v", plan.Selections)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("schema overflow on optional must not unmet: %+v", plan.Unmet)
	}
	if len(omittedNeedIDs(plan)) != 2 {
		t.Fatalf("both optional must be omitted: %+v", plan.Omitted)
	}
}

func TestPlanningBudgetTreatsMissingNeedIDAsRequired(t *testing.T) {
	plan := ToolPlan{
		Selections: []PlannedSelection{
			{ID: "selection:ghost", NeedID: "ghost"},
			{ID: "selection:need:~ambient:knowledge.read.local", NeedID: "need:~ambient:knowledge.read.local"},
		},
	}
	applyPlanningBudget(&plan, PlanningBudget{MaxSelections: 1}, []CapabilityNeed{
		{ID: "need:~ambient:knowledge.read.local", Required: false},
	})
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "ghost" {
		t.Fatalf("missing NeedID must stay required: %+v", plan.Selections)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("optional must not unmet: %+v", plan.Unmet)
	}
	if len(plan.Omitted) != 1 || plan.Omitted[0].NeedID != "need:~ambient:knowledge.read.local" || plan.Omitted[0].ReasonCode != reasonOptionalBudgetOmitted {
		t.Fatalf("optional omit=%+v", plan.Omitted)
	}
}

func TestPlanningBudgetLeftoverOneKeepsKnowledgeOmitsMemory(t *testing.T) {
	registry := warehouseBudgetRegistry(t)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1",
		Snapshot: semanticSnapshot(t, registry, warehouseBudgetProviders()),
		Needs:    warehouseBudgetNeeds(),
		Budget:   PlanningBudget{MaxSelections: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 2 || plan.Selections[0].NeedID != "capture" || plan.Selections[1].NeedID != "need:~ambient:knowledge.read.local" {
		t.Fatalf("leftover=1 must keep knowledge: %+v", plan.Selections)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("memory omit must not unmet: %+v", plan.Unmet)
	}
	if len(plan.Omitted) != 1 || plan.Omitted[0].NeedID != "need:~ambient:memory.manage.agent" {
		t.Fatalf("memory must be omitted one-at-a-time: %+v", plan.Omitted)
	}
}

func TestPlanningBudgetRequiredFirstWaveExceedDoesNotFillOptional(t *testing.T) {
	registry := warehouseBudgetRegistry(t)
	if err := registry.Register(CapabilityDescriptor{
		ID: "information.current_time", Version: "v1", Effects: []EffectClass{EffectReadOnly},
	}); err != nil {
		t.Fatal(err)
	}
	needs := append(warehouseBudgetNeeds(), CapabilityNeed{ID: "clock", Capability: "information.current_time", Required: true})
	providers := append(warehouseBudgetProviders(), semanticProvider("clock_adapter", "information.current_time", nil, EffectReadOnly))
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1",
		Snapshot: semanticSnapshot(t, registry, providers),
		Needs:    needs,
		Budget:   PlanningBudget{MaxSelections: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 0 {
		t.Fatalf("first required wave exceed must keep none: %+v", plan.Selections)
	}
	for _, item := range plan.Unmet {
		if item.ReasonCode != "planning_budget_exceeded" {
			t.Fatalf("required unmet=%+v", plan.Unmet)
		}
		if item.NeedID == "need:~ambient:knowledge.read.local" || item.NeedID == "need:~ambient:memory.manage.agent" {
			t.Fatalf("optional must not be unmet: %+v", plan.Unmet)
		}
	}
	omitted := omittedNeedIDs(plan)
	if omitted["need:~ambient:knowledge.read.local"] != reasonOptionalBudgetOmitted || omitted["need:~ambient:memory.manage.agent"] != reasonOptionalBudgetOmitted {
		t.Fatalf("optional must be omitted, not selected: %+v", plan.Omitted)
	}
}

func TestPlanningBudgetEmptyRequiredIsNotFirstWaveExceed(t *testing.T) {
	plan := ToolPlan{
		Selections: []PlannedSelection{
			{ID: "selection:need:~ambient:knowledge.read.local", NeedID: "need:~ambient:knowledge.read.local"},
			{ID: "selection:need:~ambient:memory.manage.agent", NeedID: "need:~ambient:memory.manage.agent"},
		},
	}
	applyPlanningBudget(&plan, PlanningBudget{MaxSelections: 1}, []CapabilityNeed{
		{ID: "need:~ambient:knowledge.read.local", Required: false},
		{ID: "need:~ambient:memory.manage.agent", Required: false},
	})
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "need:~ambient:knowledge.read.local" {
		t.Fatalf("empty required must fill optional one-at-a-time: %+v", plan.Selections)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("empty required must not planning_budget_exceeded: %+v", plan.Unmet)
	}
	if len(plan.Omitted) != 1 || plan.Omitted[0].NeedID != "need:~ambient:memory.manage.agent" {
		t.Fatalf("second optional omit=%+v", plan.Omitted)
	}
}

func TestPlanningBudgetOmitsOptionalWhoseRequiredParentWasDropped(t *testing.T) {
	plan := ToolPlan{
		Selections: []PlannedSelection{
			{ID: "selection:search", NeedID: "search"},
			{ID: "selection:generate", NeedID: "generate", Requires: []string{"selection:search"}},
			{ID: "selection:extra", NeedID: "extra", Requires: []string{"selection:generate"}},
		},
	}
	applyPlanningBudget(&plan, PlanningBudget{MaxSelections: 1}, []CapabilityNeed{
		{ID: "search", Required: true},
		{ID: "generate", Required: true},
		{ID: "extra", Required: false},
	})
	if len(plan.Selections) != 1 || plan.Selections[0].NeedID != "search" {
		t.Fatalf("must keep search prefix: %+v", plan.Selections)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].NeedID != "generate" || plan.Unmet[0].ReasonCode != "budget_exceeded" {
		t.Fatalf("generate unmet=%+v", plan.Unmet)
	}
	if len(plan.Omitted) != 1 || plan.Omitted[0].NeedID != "extra" || plan.Omitted[0].ReasonCode != reasonOptionalBudgetOmitted {
		t.Fatalf("optional whose parent dropped must omit: %+v", plan.Omitted)
	}
}
