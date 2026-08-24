package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func documentOpenClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelDocumentOpen, Confidence: .98}
}

func TestIMSemanticDocumentOpenIsManaged(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(*documentOpenClassification())
	if !managed || unmapped != "" {
		t.Fatalf("document_open must be managed, managed=%v unmapped=%q", managed, unmapped)
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, resolved, err := semanticIntentNeedsFromClassification(registry, *documentOpenClassification())
	if err != nil || !resolved || len(needs) != 1 {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, resolved, err)
	}
	if needs[0].Capability != tool.CapabilitySystemLaunchLocal || !needs[0].Required {
		t.Fatalf("need=%#v", needs[0])
	}
}

func TestIMSemanticDocumentOpenMaterializesOpen(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "open the PDF document on my desktop", "desktop", "root-doc-open", "turn-doc-open", documentOpenClassification(),
	)
	if err != nil || !handled || prepared == nil || len(prepared.plan.Unmet) != 0 {
		t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	if !planHasCapabilities(prepared.plan, tool.CapabilitySystemLaunchLocal) {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	for _, selection := range prepared.plan.Selections {
		if selection.AdapterName != "open" {
			t.Fatalf("document_open selected %s", selection.AdapterName)
		}
		if selection.FitProof.MatchedCapability != tool.CapabilitySystemLaunchLocal {
			t.Fatalf("capability=%q", selection.FitProof.MatchedCapability)
		}
	}
	schema, ok := prepared.schemas["open"]
	if !ok {
		t.Fatal("open schema missing")
	}
	properties, _ := schema["properties"].(map[string]interface{})
	for _, blocked := range []string{"channel", "destination", "group_name"} {
		if _, found := properties[blocked]; found {
			t.Fatalf("open schema still exposes %s", blocked)
		}
	}
}

func TestIMSemanticDocumentDeliveryStillUnmapped(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: .98})
	if !managed || unmapped != "" {
		t.Fatalf("document_delivery is specified-target deliver, managed=%v unmapped=%q", managed, unmapped)
	}
	if _, ok := imSemanticIntentRuleSet[intent.LabelDocumentOpen]; !ok {
		t.Fatal("GUI rule set must map document_open")
	}
}

