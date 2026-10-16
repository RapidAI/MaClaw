package tool

import "testing"

func TestSelectionReceiptPolicies(t *testing.T) {
	provider := ProviderBinding{Kind: "host"}
	local := PlannedSelection{Provider: provider, Effects: []EffectClass{EffectLocalMutation}}
	if !SelectionRequiresReceipt(local) {
		t.Fatal("local mutation must require the generic receipt policy")
	}
	if SelectionRequiresExternalReceipt(local) {
		t.Fatal("dynamic external policy must not require a local mutation receipt")
	}

	external := PlannedSelection{Provider: provider, AdapterName: "ssh", Effects: []EffectClass{EffectExternalEffect}}
	if !SelectionRequiresExternalReceipt(external) {
		t.Fatal("external effect must require dynamic receipt")
	}
	if !HostObservedExternalSelection(external, "host", "ssh") {
		t.Fatal("allowlisted host adapter should provide observation receipt")
	}
	if HostObservedExternalSelection(external, "mcp", "ssh") {
		t.Fatal("provider kind must fence observation receipt")
	}
}

func TestHostLocalMutationSelectionRejectsMixedExternalEffects(t *testing.T) {
	selection := PlannedSelection{Provider: ProviderBinding{Kind: "builtin"}, Effects: []EffectClass{EffectSensitive, EffectExternalEffect}}
	if HostLocalMutationSelection(selection, "builtin") {
		t.Fatal("mixed external effect must not claim local receipt")
	}
}

func TestPlanHasCapability(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{{FitProof: FitProof{MatchedCapability: "workspace.file.read"}}}}
	if !PlanHasCapability(plan, "workspace.file.read") {
		t.Fatal("expected capability in plan")
	}
	if PlanHasCapability(plan, "") || PlanHasCapability(plan, "missing") {
		t.Fatal("unexpected capability match")
	}
}

func TestCapabilityNeedHasSideEffectUsesRegistryThenReadOnlyFallback(t *testing.T) {
	if CapabilityNeedHasSideEffect(nil, CapabilityNeed{}) {
		t.Fatal("empty capability is not a side effect")
	}
	if CapabilityNeedHasSideEffect(nil, CapabilityNeed{Capability: "information.search.web"}) {
		t.Fatal("search must be read-only without a registry")
	}
	if CapabilityNeedHasSideEffect(nil, CapabilityNeed{Capability: "information.lookup"}) {
		t.Fatal("information.lookup must be read-only without a registry")
	}
	if CapabilityNeedHasSideEffect(nil, CapabilityNeed{Capability: CapabilityInteractionAskUser}) {
		t.Fatal("ask-user must be read-only without a registry")
	}
	if !CapabilityNeedHasSideEffect(nil, CapabilityNeed{Capability: "document.generate.file"}) {
		t.Fatal("generate must be a side effect")
	}
	if !CapabilityNeedHasSideEffect(nil, CapabilityNeed{Capability: "visual.capture.desktop"}) {
		t.Fatal("unregistered capture defaults to side effect")
	}
	if !CapabilityNeedsHaveSideEffect(nil, []CapabilityNeed{
		{Capability: "information.search.web"},
		{Capability: "document.generate.file"},
	}) {
		t.Fatal("mixed needs must report a side effect")
	}
	registry := NewCapabilityRegistry("side-effect-test")
	if err := registry.Register(CapabilityDescriptor{ID: "visual.capture.desktop", Version: "v1", Owner: "test", Summary: "capture", Effects: []EffectClass{EffectReadOnly}}); err != nil {
		t.Fatal(err)
	}
	if CapabilityNeedHasSideEffect(registry, CapabilityNeed{Capability: "visual.capture.desktop"}) {
		t.Fatal("registry read-only capture must not be a side effect")
	}
	if err := registry.Register(CapabilityDescriptor{ID: "fs.write.local", Version: "v1", Owner: "test", Summary: "write", Effects: []EffectClass{EffectLocalMutation}}); err != nil {
		t.Fatal(err)
	}
	if !CapabilityNeedHasSideEffect(registry, CapabilityNeed{Capability: "fs.write.local"}) {
		t.Fatal("registry mutation must be a side effect")
	}
}

func TestIsLookupSelectionUsesCapabilityFamily(t *testing.T) {
	for _, capability := range []CapabilityID{"information.search.web", CapabilityInformationFetchWeb, "information.current_time"} {
		if !IsLookupSelection(PlannedSelection{FitProof: FitProof{MatchedCapability: capability}}) {
			t.Fatalf("capability %q should be recognized as lookup selection", capability)
		}
	}
	if IsLookupSelection(PlannedSelection{FitProof: FitProof{MatchedCapability: "document.generate.file"}}) {
		t.Fatal("document generation must not be recognized as a lookup selection")
	}
}

func TestPlanWithSelectionsRetainsPlanMetadataAndOrder(t *testing.T) {
	plan := ToolPlan{
		ID: "plan-1",
		Selections: []PlannedSelection{
			{ID: "first"}, {ID: "second"}, {ID: "third"},
		},
		Unmet: []UnmetNeed{{NeedID: "optional"}},
	}
	filtered := PlanWithSelections(plan, map[string]bool{"third": true, "first": true})
	if filtered.ID != plan.ID || len(filtered.Unmet) != 1 || len(filtered.Selections) != 2 ||
		filtered.Selections[0].ID != "first" || filtered.Selections[1].ID != "third" {
		t.Fatalf("filtered plan=%#v", filtered)
	}
}

func TestPlanSelectionByID(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{{ID: "s1", NeedID: "n1"}}}
	selection, ok := PlanSelectionByID(plan, "s1")
	if !ok || selection.NeedID != "n1" {
		t.Fatalf("selection=%#v ok=%v", selection, ok)
	}
	if _, ok := PlanSelectionByID(plan, "missing"); ok {
		t.Fatal("missing selection unexpectedly found")
	}
}
