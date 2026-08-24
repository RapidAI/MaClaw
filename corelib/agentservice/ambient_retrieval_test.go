package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestAppendAmbientRetrievalNeedsFromEmptyIsUnmanagedNeedPath(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got := AppendAmbientRetrievalNeeds(registry, nil)
	if len(got) != 2 {
		t.Fatalf("empty start must grow both warehouse needs: %#v", got)
	}
	if got[0].Capability != CapabilityKnowledgeRead || got[0].Required {
		t.Fatalf("knowledge=%#v", got[0])
	}
	if got[1].Capability != CapabilityMemoryRecall || got[1].Required {
		t.Fatalf("recall=%#v", got[1])
	}
}

func TestAppendAmbientRetrievalNeedsSkipsWhenCapabilityAlreadyRequired(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	needs := []coretool.CapabilityNeed{{
		ID: "need:knowledge.read.local:intent", Capability: CapabilityKnowledgeRead,
		Polarity: coretool.NeedRequire, Required: true,
	}}
	got := AppendAmbientRetrievalNeeds(registry, needs)
	if len(got) != 2 {
		t.Fatalf("needs=%#v", got)
	}
	if got[0].Capability != CapabilityKnowledgeRead || got[0].Required != true {
		t.Fatalf("intent knowledge need must stay first and required: %#v", got[0])
	}
	if got[1].Capability != CapabilityMemoryRecall || got[1].Required || got[1].EvidenceIDs[0] != ambientRetrievalEvidence {
		t.Fatalf("ambient memory=%#v", got[1])
	}
}

func TestApplyAmbientRetrievalStaysOffWhenDisabled(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelFileRead, Confidence: .99,
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "read this file"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityFileRead {
		t.Fatalf("need=%#v", resolution.Needs[0])
	}
}

func TestApplyAmbientRetrievalAddsOptionalWarehouseTools(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelFileRead, Confidence: .99,
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
		AmbientRetrieval: true,
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "read this file"})
	if err != nil || !resolution.Managed {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	var haveFile, haveKnowledge, haveMemory bool
	for _, need := range resolution.Needs {
		switch need.Capability {
		case CapabilityFileRead:
			haveFile = need.Required
		case CapabilityKnowledgeRead:
			haveKnowledge = !need.Required
		case CapabilityMemoryRecall:
			haveMemory = !need.Required
		}
	}
	if !haveFile || !haveKnowledge || !haveMemory {
		t.Fatalf("ambient retrieval missing: %#v", resolution.Needs)
	}
}

func TestApplyAmbientRetrievalSkipsLookupPrimaryEvenWhenEnabled(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: .99,
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
		AmbientRetrieval: true,
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "南京天气"})
	if err != nil || !resolution.Managed {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	for _, need := range resolution.Needs {
		if need.Capability == CapabilityKnowledgeRead && !need.Required {
			t.Fatalf("lookup primary must not grow ambient knowledge: %#v", resolution.Needs)
		}
	}
}
