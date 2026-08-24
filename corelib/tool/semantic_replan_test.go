package tool

import "testing"

func TestValidateReplanSubsetRequiresOneToOneNeedCorrespondence(t *testing.T) {
	parent := ToolPlan{RootTaskID: "root", Selections: []PlannedSelection{
		testReplanSelection("lookup", "need:lookup", "information.search.web"),
		testReplanSelection("generate", "need:generate", "document.generate.file"),
	}}
	child := ToolPlan{RootTaskID: "root", Selections: []PlannedSelection{
		testReplanSelection("lookup-a", "need:lookup", "information.search.web"),
		testReplanSelection("lookup-b", "need:lookup", "information.search.web"),
	}}
	if err := ValidateReplanSubset(parent, child); err == nil {
		t.Fatal("child that duplicates a need and drops another was accepted")
	}
}

func TestReplanBindingOnlyReplacementRequiresOneToOneNeedCorrespondence(t *testing.T) {
	parent := ToolPlan{RootTaskID: "root", Selections: []PlannedSelection{
		testDynamicReplanSelection("lookup", "need:lookup", "information.search.web"),
		testDynamicReplanSelection("generate", "need:generate", "document.generate.file"),
	}}
	child := ToolPlan{RootTaskID: "root", Selections: []PlannedSelection{
		testDynamicReplanSelection("lookup-a", "need:lookup", "information.search.web"),
		testDynamicReplanSelection("lookup-b", "need:lookup", "information.search.web"),
	}}
	if ReplanIsBindingOnlyReplacement(parent, child) {
		t.Fatal("binding-only replan accepted duplicate need correspondence")
	}
}

func TestReplanBindingReplacementCannotExpandReferenceAuthority(t *testing.T) {
	parent := ToolPlan{RootTaskID: "root", Selections: []PlannedSelection{
		testDynamicReplanSelection("lookup", "need:lookup", "information.search.web"),
	}}
	parent.Selections[0].ParameterAuthorization.AllowedTargets = []string{"target:approved"}
	parent.Selections[0].ParameterAuthorization.AllowedArtifactIDs = []string{"artifact:approved"}
	child := parent
	child.Selections = append([]PlannedSelection(nil), parent.Selections...)
	child.Selections[0].ParameterAuthorization.AllowedTargets = []string{"target:approved", "target:expanded"}
	if ReplanIsBindingOnlyReplacement(parent, child) {
		t.Fatal("binding-only replan expanded target authority")
	}
	child.Selections[0].ParameterAuthorization.AllowedTargets = []string{"target:approved"}
	child.Selections[0].ParameterAuthorization.AllowedArtifactIDs = []string{"artifact:approved", "artifact:expanded"}
	if ReplanIsBindingOnlyReplacement(parent, child) {
		t.Fatal("binding-only replan expanded artifact authority")
	}
	child.Selections[0].ParameterAuthorization.AllowedArtifactIDs = []string{"artifact:approved"}
	if !ReplanIsBindingOnlyReplacement(parent, child) {
		t.Fatal("binding-only replan with identical reference authority was rejected")
	}
}

func TestValidateReplanSubsetRejectsReferenceAuthorityChange(t *testing.T) {
	parent := ToolPlan{RootTaskID: "root", Selections: []PlannedSelection{
		testReplanSelection("lookup", "need:lookup", "information.search.web"),
	}}
	parent.Selections[0].ParameterAuthorization.AllowedTargets = []string{"target:approved"}
	child := parent
	child.Selections = append([]PlannedSelection(nil), parent.Selections...)
	child.Selections[0].ParameterAuthorization.AllowedTargets = []string{"target:expanded"}
	if err := ValidateReplanSubset(parent, child); err == nil {
		t.Fatal("replan changed target authority")
	}
}

func testReplanSelection(id, needID string, capability CapabilityID) PlannedSelection {
	return PlannedSelection{
		ID: id, NeedID: needID, Phase: PlanPhaseExecution,
		ParameterAuthorization: ParameterAuthorization{Digest: id + "-params", CanonicalizerVer: semanticCanonicalizerVersion},
		FitProof:               FitProof{MatchedCapability: capability},
		Effects:                []EffectClass{EffectReadOnly},
	}
}

func testDynamicReplanSelection(id, needID string, capability CapabilityID) PlannedSelection {
	selection := testReplanSelection(id, needID, capability)
	selection.Provider = ProviderBinding{Kind: "mcp", ProviderID: "server", ImplementationID: id}
	return selection
}
