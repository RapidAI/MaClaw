package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestIMAndReviewedIntentRulesShareCapabilityIDsForCommonFamilies(t *testing.T) {
	im := IMSemanticIntentCapabilityNeedRules()
	reviewed := ReviewedDynamicIntentCapabilityNeedRules()
	catalogSpecific := map[intent.IntentLabel]bool{
		intent.LabelOffice:       true,
		intent.LabelDocumentOpen: true,
		intent.LabelAppLaunch:    true,
	}
	for label, reviewedTemplates := range reviewed {
		if catalogSpecific[label] {
			continue
		}
		imTemplates, ok := im[label]
		if !ok {
			t.Errorf("IM rules dropped reviewed family %q", label)
			continue
		}
		if len(imTemplates) < 1 || imTemplates[0].Capability != reviewedTemplates[0].Capability {
			t.Errorf("family %q IM capability %q != reviewed %q", label, imTemplates[0].Capability, reviewedTemplates[0].Capability)
		}
	}
}

func TestIMSemanticIntentRulesCoverCodingAndSearchWeb(t *testing.T) {
	im := IMSemanticIntentCapabilityNeedRules()
	if len(im[intent.LabelCoding]) == 0 || im[intent.LabelCoding][0].Capability != ReviewedCodingCapabilityNeedRule()[0].Capability {
		t.Fatalf("coding family missing from IM rules: %#v", im[intent.LabelCoding])
	}
	search := im[intent.LabelSearch]
	if len(search) != 1 || search[0].Capability != CapabilityInformationSearchWeb || search[0].MaxInvocations != 5 {
		t.Fatalf("IM search rule=%#v", search)
	}
	office := im[intent.LabelOffice]
	if len(office) < 1 || office[0].Capability != coretool.CapabilityDocumentWriteOffice || office[0].MaxInvocations != 8 {
		t.Fatalf("IM office write budget=%#v, want 8 iterative invocations", office)
	}
	reviewed := ReviewedDynamicIntentCapabilityNeedRules()[intent.LabelSearch]
	if len(reviewed) != 1 || reviewed[0].Capability != CapabilityInformationSearchWeb || reviewed[0].Qualifiers[QualifierSearchFreshness] != SearchFreshnessReference || reviewed[0].MaxInvocations != 5 {
		t.Fatalf("reviewed search must share information.search.web budget 5, got %#v", reviewed)
	}
}

func TestIMAndReviewedIntentRulesShareRepeatBudgets(t *testing.T) {
	im := IMSemanticIntentCapabilityNeedRules()
	reviewed := ReviewedDynamicIntentCapabilityNeedRules()
	for label, reviewedTemplates := range reviewed {
		imTemplates, ok := im[label]
		if !ok {
			continue
		}
		imByKey := make(map[string]IntentCapabilityNeedTemplate, len(imTemplates))
		for _, template := range imTemplates {
			imByKey[NeedTemplateIdentityKey(template)] = template
		}
		for _, template := range reviewedTemplates {
			peer, ok := imByKey[NeedTemplateIdentityKey(template)]
			if !ok {
				continue
			}
			if coretool.RepeatSiblingBudget(peer.MaxInvocations) != coretool.RepeatSiblingBudget(template.MaxInvocations) {
				t.Errorf("family %q capability %q budget IM=%d reviewed=%d", label, template.Capability, peer.MaxInvocations, template.MaxInvocations)
			}
		}
	}
	if coretool.RepeatSiblingBudget(im[intent.LabelOffice][0].MaxInvocations) != coretool.RepeatSiblingBudget(reviewed[intent.LabelOffice][0].MaxInvocations) {
		t.Errorf("office write budget drifted: IM=%d reviewed=%d", im[intent.LabelOffice][0].MaxInvocations, reviewed[intent.LabelOffice][0].MaxInvocations)
	}
	if coretool.RepeatSiblingBudget(im[intent.LabelShellCommand][0].MaxInvocations) != coretool.RepeatSiblingBudget(reviewed[intent.LabelShellCommand][0].MaxInvocations) {
		t.Errorf("shell budget drifted: IM=%d reviewed=%d", im[intent.LabelShellCommand][0].MaxInvocations, reviewed[intent.LabelShellCommand][0].MaxInvocations)
	}
}

func TestSemanticArchetypeBundlesArePinnedToRules(t *testing.T) {
	im := IMSemanticIntentCapabilityNeedRules()
	for primary, companions := range SemanticArchetypeBundles() {
		if len(im[primary]) == 0 {
			t.Errorf("bundle primary %q has no IM rule", primary)
		}
		for _, companion := range companions {
			if len(im[companion]) == 0 {
				t.Errorf("bundle %q companion %q has no IM rule", primary, companion)
			}
		}
	}
}
