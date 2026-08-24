package tool

import "testing"

// An optional need is one a turn takes if the host has it and proceeds without
// if the host does not.
//
// Before this existed, Required:false meant the planner skipped the need
// outright, so the only expressible rule was "every capability must be present
// on every host". That is what kept conditionally-provisioned hosts from
// taking families whose providers are attached per turn: one withheld provider
// turned the whole turn into an empty tool surface, which is strictly worse
// than serving the part that is available.

func optionalPlanRequest(t *testing.T, providers []ProviderSpec, needs []CapabilityNeed) (ToolPlan, error) {
	t.Helper()
	registry := semanticRegistry(t)
	return NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1",
		TurnID:     "turn-1",
		Snapshot:   semanticSnapshot(t, registry, providers),
		Needs:      needs,
	})
}

func selectionNeedIDs(plan ToolPlan) map[string]bool {
	out := make(map[string]bool, len(plan.Selections))
	for _, selection := range plan.Selections {
		out[selection.NeedID] = true
	}
	return out
}

func TestAnAbsentOptionalCapabilityDoesNotCostTheTurnItsOtherTools(t *testing.T) {
	// The host publishes capture but not delivery, which is the shape of a
	// workspace-bound host that withholds one provider from this turn.
	plan, err := optionalPlanRequest(t,
		[]ProviderSpec{semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)},
		[]CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: false},
		})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("an absent optional capability failed the plan: %+v", plan.Unmet)
	}
	if !selectionNeedIDs(plan)["capture"] {
		t.Fatal("the required capability was not selected, so the turn lost tools it could have had")
	}
	if selectionNeedIDs(plan)["deliver"] {
		t.Fatal("a capability with no provider was selected")
	}
}

func TestAnOmittedOptionalCapabilityIsRecordedRatherThanDropped(t *testing.T) {
	plan, err := optionalPlanRequest(t,
		[]ProviderSpec{semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)},
		[]CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: false},
		})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Omitted) != 1 || plan.Omitted[0].NeedID != "deliver" {
		t.Fatalf("the omission was not recorded: %+v", plan.Omitted)
	}
	if plan.Omitted[0].ReasonCode != "no_feasible_provider" {
		t.Fatalf("omitted reason = %q, want no_feasible_provider", plan.Omitted[0].ReasonCode)
	}
	// Why the model never saw the capability has to be answerable from the
	// trace, not by re-deriving the host's wiring afterwards.
	found := false
	for _, event := range plan.Trace.Events {
		if event.Stage == TraceStageFeasibility && event.Event == "omitted" && event.ReasonCode == "no_feasible_provider" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no feasibility row explains the omission: %+v", plan.Trace.Events)
	}
}

func TestAnOptionalCapabilityIsSelectedWhenTheHostHasIt(t *testing.T) {
	plan, err := optionalPlanRequest(t,
		[]ProviderSpec{
			semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
			semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect),
		},
		[]CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: false},
		})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Omitted) != 0 {
		t.Fatalf("a servable optional capability was omitted: %+v", plan.Omitted)
	}
	if !selectionNeedIDs(plan)["deliver"] {
		t.Fatal("an optional need with a ready provider was not selected")
	}
}

// Optionality is about the host, not about the rule. A capability the registry
// never declared could not be served anywhere, so Required:false must not turn
// it into a silent omission.
func TestAnOptionalNeedStillFailsWhenTheRuleNamesNothingReal(t *testing.T) {
	plan, err := optionalPlanRequest(t, nil, []CapabilityNeed{
		{ID: "typo", Capability: "visual.capture.destkop", Required: false},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Omitted) != 0 {
		t.Fatalf("an undeclared capability was hidden as an omission: %+v", plan.Omitted)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "unknown_capability" {
		t.Fatalf("unmet = %+v, want one unknown_capability", plan.Unmet)
	}
}

func TestAnOptionalNeedStillFailsWhenItViolatesTheDescriptor(t *testing.T) {
	// The capture descriptor requires a display qualifier from a closed set.
	plan, err := optionalPlanRequest(t, nil, []CapabilityNeed{
		{ID: "bad", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "sideways"}, Required: false},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "invalid_capability_need" {
		t.Fatalf("unmet = %+v, want one invalid_capability_need", plan.Unmet)
	}
}

// The change is only safe to land because every existing rule is all-required,
// and a required need must behave exactly as it did before.
func TestARequiredCapabilityWithNoProviderStillFailsTheTurn(t *testing.T) {
	plan, err := optionalPlanRequest(t,
		[]ProviderSpec{semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)},
		[]CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
		})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Omitted) != 0 {
		t.Fatalf("a required need was demoted to an omission: %+v", plan.Omitted)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].NeedID != "deliver" {
		t.Fatalf("unmet = %+v, want the required delivery need", plan.Unmet)
	}
}
