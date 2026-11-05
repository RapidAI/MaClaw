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

func TestPreservePetitionParentAuthorityKeepsContinuationAndTemplate(t *testing.T) {
	templates := IMSemanticIntentCapabilityNeedRules()[intent.LabelFileDownload]
	qualifiers := map[string]string{QualifierSearchFreshness: SearchFreshnessReference}
	clock := testPetitionSelection("need:information.current_time:clock", CapabilityCurrentTime, nil)
	search := testPetitionSelection("need:information.search.web:abc", CapabilityInformationSearchWeb, qualifiers)
	search.ID = "selection:" + search.NeedID
	search.Provider.ImplementationID = "original"
	search.EvidenceIDs = []string{"intent:search"}
	continuation := testPetitionSelection("need:information.search.web:abc#06", CapabilityInformationSearchWeb, qualifiers)
	continuation.ID = "selection:" + continuation.NeedID
	continuation.AdapterName = "continuation"
	continuation.Optional = true
	continuation.EvidenceIDs = []string{"intent:search"}
	parent := coretool.ToolPlan{RootTaskID: "root-preserve", Selections: []coretool.PlannedSelection{clock, search, continuation}}

	refreshed := search
	refreshed.ID = "selection:refreshed-search"
	refreshed.Provider.ImplementationID = "refreshed"
	widened := clock
	widened.ParameterAuthorization.AllowedFields = []string{"now"}
	rewrittenContinuation := continuation
	rewrittenContinuation.ID = "selection:rewritten-continuation"
	rewrittenContinuation.AdapterName = "rewritten"
	rewrittenContinuation.Optional = false
	rewrittenContinuation.Requires = []string{"selection:other"}
	rewrittenContinuation.EvidenceIDs = []string{"intent:rewritten"}
	rewrittenContinuation.ArtifactDependencies = []coretool.ArtifactDependency{{
		ProducerSelection: "selection:other",
		Contract:          coretool.ArtifactContract{Kind: "page", MIMEType: "text/html", Required: true},
	}}
	rewritten := testPetitionSelection("need:information.search.web:other", CapabilityInformationSearchWeb, qualifiers)
	rewritten.ParameterAuthorization.AllowedFields = []string{"query"}
	download := testPetitionSelection("need:artifact.acquire.remote:new", coretool.CapabilityArtifactAcquireRemote, nil)
	shell := testPetitionSelection("need:shell.execute.local:nope", coretool.CapabilityShellExecuteLocal, nil)
	child := coretool.ToolPlan{
		RootTaskID: parent.RootTaskID,
		Selections: []coretool.PlannedSelection{widened, refreshed, rewrittenContinuation, rewritten, download, shell},
		Unmet: []coretool.UnmetNeed{
			{NeedID: continuation.NeedID, ReasonCode: "catalog_incomplete"},
			{NeedID: "need:browser.control.web:missing", ReasonCode: "catalog_incomplete"},
		},
	}
	preserved := PreservePetitionParentAuthority(parent, child, templates)
	seen := map[string]coretool.PlannedSelection{}
	for _, selection := range preserved.Selections {
		if _, ok := seen[selection.NeedID]; ok {
			t.Fatalf("duplicate need %s", selection.NeedID)
		}
		seen[selection.NeedID] = selection
	}
	if got := seen[search.NeedID]; got.Provider.ImplementationID != "original" || got.ID != search.ID {
		t.Fatalf("parent search = %+v", got)
	}
	seen[search.NeedID].EvidenceIDs[0] = "mutated"
	if parent.Selections[1].EvidenceIDs[0] != "intent:search" {
		t.Fatal("preserved search shares the parent's evidence slice")
	}
	if fields := seen[clock.NeedID].ParameterAuthorization.AllowedFields; len(fields) != 0 {
		t.Fatalf("widened clock kept fields %v", fields)
	}
	gotContinuation := seen[continuation.NeedID]
	if gotContinuation.AdapterName != "continuation" || !gotContinuation.Optional || gotContinuation.ID != continuation.ID || len(gotContinuation.ArtifactDependencies) != 0 || len(gotContinuation.Requires) != 0 || len(gotContinuation.EvidenceIDs) != 1 || gotContinuation.EvidenceIDs[0] != "intent:search" {
		t.Fatalf("continuation = %+v", gotContinuation)
	}
	if _, ok := seen[download.NeedID]; !ok {
		t.Fatal("download template was dropped")
	}
	if _, ok := seen[rewritten.NeedID]; ok {
		t.Fatal("rewritten search id survived a download petition")
	}
	if _, ok := seen[shell.NeedID]; ok {
		t.Fatal("shell survived a download petition")
	}
	if len(preserved.Unmet) != 1 || preserved.Unmet[0].NeedID != "need:browser.control.web:missing" {
		t.Fatalf("unmet = %+v", preserved.Unmet)
	}
	if err := ValidatePetitionExpansion(parent, preserved, templates); err == nil || !strings.Contains(err.Error(), "unmet needs") {
		t.Fatalf("unmet browser must still reject the expansion, err=%v", err)
	}
	preserved.Unmet = nil
	if err := ValidatePetitionExpansion(parent, preserved, templates); err != nil {
		t.Fatalf("parent continuation plus download template = %v", err)
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
