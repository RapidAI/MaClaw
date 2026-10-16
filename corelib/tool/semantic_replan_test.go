package tool

import (
	"fmt"
	"strings"
	"testing"
)

func TestReplanAndPetitionTurnIDsAreStableAndDistinct(t *testing.T) {
	parent := "turn-abc"
	got := ReplanTurnID(parent, 1)
	want := "replan:" + SchemaDigest([]byte(strings.TrimSpace(parent) + fmt.Sprintf(":%d", uint8(1))))[:24]
	if got != want {
		t.Fatalf("ReplanTurnID=%q want=%q", got, want)
	}
	petition := PetitionTurnID(parent, "search")
	wantPetition := "petition:" + SchemaDigest([]byte(strings.TrimSpace(parent) + ":search"))[:24]
	if petition != wantPetition {
		t.Fatalf("PetitionTurnID=%q want=%q", petition, wantPetition)
	}
	if ReplanTurnID(parent, 1) == ReplanTurnID(parent, 2) {
		t.Fatal("replan attempts must not collide")
	}
	if strings.HasPrefix(petition, "replan:") || strings.HasPrefix(got, "petition:") {
		t.Fatal("kind prefixes must stay distinct")
	}
}

func TestEffectsEqualComparesMultisets(t *testing.T) {
	if !EffectsEqual(nil, nil) || !EffectsEqual([]EffectClass{EffectReadOnly}, []EffectClass{EffectReadOnly}) {
		t.Fatal("identical effect lists must match")
	}
	if EffectsEqual([]EffectClass{EffectReadOnly}, []EffectClass{EffectSensitive}) {
		t.Fatal("different effects must not match")
	}
}

func TestArtifactContractsEqualComparesKindMIMEAndRequired(t *testing.T) {
	left := []ArtifactContract{{Kind: "file", MIMEType: "text/plain", Required: true}}
	if !ArtifactContractsEqual(left, []ArtifactContract{{Kind: "file", MIMEType: "text/plain", Required: true}}) {
		t.Fatal("identical contracts must match")
	}
	if ArtifactContractsEqual(left, []ArtifactContract{{Kind: "file", MIMEType: "text/plain", Required: false}}) {
		t.Fatal("required flag must participate")
	}
}

func TestQualifiersEqualComparesKeysAndValues(t *testing.T) {
	if !QualifiersEqual(nil, map[string]string{}) {
		t.Fatal("empty maps must match")
	}
	if !QualifiersEqual(map[string]string{"freshness": "current"}, map[string]string{"freshness": "current"}) {
		t.Fatal("identical maps must match")
	}
	if QualifiersEqual(map[string]string{"freshness": "current"}, map[string]string{"freshness": "reference"}) {
		t.Fatal("different values must not match")
	}
	if QualifiersEqual(map[string]string{"freshness": "current"}, map[string]string{"freshness": "current", "scope": "current"}) {
		t.Fatal("different lengths must not match")
	}
}

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
