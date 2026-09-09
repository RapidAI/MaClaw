package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestClassificationFromGrantedNeedsPinsCompositeFamilies(t *testing.T) {
	rules := IMSemanticIntentCapabilityNeedRules()
	has := func(result intent.ClassificationResult, label intent.IntentLabel) bool {
		return result.HasLabel(label)
	}

	empty := ClassificationFromGrantedNeeds(nil, rules)
	if empty.Primary != intent.LabelUnknown || empty.Layer != 3 {
		t.Fatalf("empty needs=%+v", empty)
	}

	generate := ClassificationFromGrantedNeeds([]coretool.CapabilityNeed{
		{Capability: CapabilityDocumentGenerate},
		{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile}},
	}, rules)
	if generate.Primary != intent.LabelDocumentGenerate || has(generate, intent.LabelAttachmentDelivery) {
		t.Fatalf("generate+file deliver must not relabel as attachment: %+v", generate)
	}

	fileOnly := ClassificationFromGrantedNeeds([]coretool.CapabilityNeed{
		{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile}},
	}, rules)
	if !has(fileOnly, intent.LabelAttachmentDelivery) {
		t.Fatalf("file deliver without generate=%+v", fileOnly)
	}

	search := ClassificationFromGrantedNeeds([]coretool.CapabilityNeed{
		{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}},
	}, rules)
	if search.Primary != intent.LabelSearch || has(search, intent.LabelLiveData) {
		t.Fatalf("reference search=%+v", search)
	}
	live := ClassificationFromGrantedNeeds([]coretool.CapabilityNeed{
		{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessCurrent}},
	}, rules)
	if live.Primary != intent.LabelLiveData || has(live, intent.LabelSearch) {
		t.Fatalf("current search=%+v", live)
	}

	image := ClassificationFromGrantedNeeds([]coretool.CapabilityNeed{
		{Capability: CapabilityArtifactDeliverCurrent, Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage}},
	}, rules)
	if !has(image, intent.LabelScreenshot) {
		t.Fatalf("image deliver=%+v", image)
	}

	launch := ClassificationFromGrantedNeeds([]coretool.CapabilityNeed{
		{Capability: coretool.CapabilitySystemLaunchLocal},
	}, rules)
	if launch.Primary != intent.LabelAppLaunch || has(launch, intent.LabelDocumentOpen) {
		t.Fatalf("shared launch capability must replay as app_launch: %+v", launch)
	}
}
