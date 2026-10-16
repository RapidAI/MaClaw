package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func testPetitionSelection(needID string, capability coretool.CapabilityID, qualifiers map[string]string) coretool.PlannedSelection {
	return coretool.PlannedSelection{
		NeedID:                 needID,
		Phase:                  coretool.PlanPhase("execution"),
		ParameterAuthorization: coretool.ParameterAuthorization{CanonicalizerVer: "semantic-parameters-v1"},
		FitProof:               coretool.FitProof{MatchedCapability: capability, QualifierBindings: qualifiers},
	}
}

func TestValidatePetitionExpansionRejectsOutsideLabel(t *testing.T) {
	parent := coretool.ToolPlan{
		RootTaskID: "root-validator",
		Selections: []coretool.PlannedSelection{testPetitionSelection("need:document.generate.file:x", "document.generate.file", nil)},
	}
	child := coretool.ToolPlan{
		RootTaskID: "root-validator",
		Selections: append(append([]coretool.PlannedSelection(nil), parent.Selections...), testPetitionSelection("need:shell.execute.local:y", coretool.CapabilityShellExecuteLocal, nil)),
	}
	err := ValidatePetitionExpansion(parent, child, IMSemanticIntentCapabilityNeedRules()[intent.LabelSearch])
	if err == nil || !strings.Contains(err.Error(), "outside the petitioned label") {
		t.Fatalf("a shell leg added by a search petition must stay rejected, err=%v", err)
	}
}

func TestValidatePetitionExpansionAcceptsTemplateNeed(t *testing.T) {
	parent := coretool.ToolPlan{
		RootTaskID: "root-validator",
		Selections: []coretool.PlannedSelection{testPetitionSelection("need:document.generate.file:x", "document.generate.file", nil)},
	}
	child := coretool.ToolPlan{
		RootTaskID: "root-validator",
		Selections: append(append([]coretool.PlannedSelection(nil), parent.Selections...), testPetitionSelection(
			"need:information.search.web:y", CapabilityInformationSearchWeb, map[string]string{QualifierSearchFreshness: SearchFreshnessReference},
		)),
	}
	if err := ValidatePetitionExpansion(parent, child, IMSemanticIntentCapabilityNeedRules()[intent.LabelSearch]); err != nil {
		t.Fatalf("search template need must be admitted, err=%v", err)
	}
}

// TestValidatePetitionExpansionRejectsUnmetChild guards the availability
// condition the planner records instead of silently dropping: a petition whose
// child revision still carries Unmet must never be published. This is the split
// that makes full semantic routing safe to attempt -- a missing capability has
// to stay an observable, recoverable Unmet rather than becoming a silently
// absent tool when the legacy wide-surface fallback is removed.
func TestValidatePetitionExpansionRejectsUnmetChild(t *testing.T) {
	templates := IMSemanticIntentCapabilityNeedRules()[intent.LabelSearch]
	parent := coretool.ToolPlan{
		RootTaskID: "root-unmet",
		Selections: []coretool.PlannedSelection{testPetitionSelection("need:document.generate.file:x", "document.generate.file", nil)},
	}
	child := coretool.ToolPlan{
		RootTaskID: "root-unmet",
		Selections: append(append([]coretool.PlannedSelection(nil), parent.Selections...), testPetitionSelection(
			"need:information.search.web:y", CapabilityInformationSearchWeb, map[string]string{QualifierSearchFreshness: SearchFreshnessReference},
		)),
	}
	if err := ValidatePetitionExpansion(parent, child, templates); err != nil {
		t.Fatalf("baseline: template-only expansion must be admitted, err=%v", err)
	}

	child.Unmet = []coretool.UnmetNeed{{NeedID: "need:information.search.web:y", ReasonCode: "no_feasible_provider"}}
	err := ValidatePetitionExpansion(parent, child, templates)
	if err == nil || !strings.Contains(err.Error(), "unmet needs") {
		t.Fatalf("a child with unmet needs must stay rejected, err=%v", err)
	}

	// Omitted is a policy-driven drop rather than an availability condition, so
	// it must not trip the Unmet gate.
	child.Unmet = nil
	child.Omitted = []coretool.UnmetNeed{{NeedID: "need:information.search.web:y", ReasonCode: "policy_denied"}}
	if err := ValidatePetitionExpansion(parent, child, templates); err != nil {
		t.Fatalf("omitted needs must not block petition expansion, err=%v", err)
	}
}

func TestValidatePetitionExpansionRejectsRootMismatchAndNoAdd(t *testing.T) {
	parent := coretool.ToolPlan{
		RootTaskID: "root-a",
		Selections: []coretool.PlannedSelection{testPetitionSelection("need:document.generate.file:x", "document.generate.file", nil)},
	}
	child := parent
	child.RootTaskID = "root-b"
	if err := ValidatePetitionExpansion(parent, child, IMSemanticIntentCapabilityNeedRules()[intent.LabelSearch]); err == nil || !strings.Contains(err.Error(), "root task mismatch") {
		t.Fatalf("root mismatch err=%v", err)
	}
	child.RootTaskID = parent.RootTaskID
	if err := ValidatePetitionExpansion(parent, child, IMSemanticIntentCapabilityNeedRules()[intent.LabelSearch]); err == nil || !strings.Contains(err.Error(), "added no governed need") {
		t.Fatalf("identity child err=%v", err)
	}
}

func TestPetitionLabelForCapabilityIsDeterministic(t *testing.T) {
	rules := IMSemanticIntentCapabilityNeedRules()
	cases := []struct {
		capability coretool.CapabilityID
		want       intent.IntentLabel
	}{
		{CapabilityInformationSearchWeb, intent.LabelSearch},
		{coretool.CapabilityFSReadLocal, intent.LabelFileRead},
		{coretool.CapabilitySystemLaunchLocal, intent.LabelAppLaunch},
		{CapabilityDocumentGenerate, intent.LabelDocumentGenerate},
		{CapabilityArtifactDeliverCurrent, intent.LabelAttachmentDelivery},
		{coretool.CapabilityBusinessDataRead, intent.LabelDatabase},
		{coretool.CapabilityBusinessDataMIS, intent.LabelBusinessData},
	}
	for _, tc := range cases {
		for i := 0; i < 8; i++ {
			got, ok := PetitionLabelForCapability(tc.capability, rules)
			if !ok || got != tc.want {
				t.Fatalf("capability %s: label=%q ok=%v, want %q", tc.capability, got, ok, tc.want)
			}
		}
	}
	if _, ok := PetitionLabelForCapability(coretool.CapabilityMessageSendIM, rules); ok {
		t.Fatal("quarantined capability without a rule label must not resolve")
	}
	if _, ok := PetitionLabelForCapability(coretool.CapabilityMemoryRecallAgent, rules); ok {
		t.Fatal("ambient-only capability without a rule label must not resolve")
	}
}
