package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func planHasCapability(plan tool.ToolPlan, capability tool.CapabilityID) bool {
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == capability {
			return true
		}
	}
	return false
}

func TestIMSemanticAmbientRetrievalFollowsPrimaryPolicy(t *testing.T) {
	cases := []struct {
		name    string
		primary intent.IntentLabel
		want    bool
	}{
		{name: "file_read", primary: intent.LabelFileRead, want: true},
		{name: "coding", primary: intent.LabelCoding, want: true},
		{name: "document_open", primary: intent.LabelDocumentOpen, want: false},
		{name: "current_time", primary: intent.LabelCurrentTime, want: false},
		{name: "live_data", primary: intent.LabelLiveData, want: false},
		{name: "audio_deliver", primary: intent.LabelAudioDeliver, want: false},
		{name: "document_generate", primary: intent.LabelDocumentGenerate, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, tc.primary)}
			registerBuiltinTools(h.registry, h)
			registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
			prepared, handled, err := h.semanticPlanForTurnWithClassification(
				"user-1", "ambient policy", "lansenger", "root-ambient-"+tc.name, "turn-ambient-"+tc.name,
				&intent.ClassificationResult{Primary: tc.primary, Confidence: .98},
			)
			if !handled || prepared == nil {
				t.Fatalf("handled=%v prepared=%#v err=%v", handled, prepared, err)
			}
			if tc.want && err != nil {
				t.Fatalf("yes-ambient primary failed: %v", err)
			}
			if tc.primary == intent.LabelAudioDeliver && err == nil {
				t.Fatal("audio_deliver without voice channel should fail closed")
			}
			hasKnowledge := planHasCapability(prepared.plan, tool.CapabilityKnowledgeReadLocal)
			if hasKnowledge != tc.want {
				t.Fatalf("knowledge on %s = %v, want %v selections=%+v", tc.primary, hasKnowledge, tc.want, prepared.plan.Selections)
			}
		})
	}
}

func TestIMSemanticWeatherPDFDoesNotSelectKnowledge(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "查询南京天气，并生成pdf报告", "desktop", "root-weather-kb", "turn-weather-kb", liveDataGenerateClassification(),
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if planHasCapability(prepared.plan, tool.CapabilityKnowledgeReadLocal) {
		t.Fatalf("lookup+generate must not grow knowledge: %+v", prepared.plan.Selections)
	}
}

func TestIMSemanticGroupDenyKnowledgeOmitsOnFileRead(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileRead)}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	ctx := withSemanticGroupPermissions(context.Background(), &lansengerGroupPermissionPolicy{AllowAllDirectories: true})
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "看看 notes.txt", "lansenger", "root-group-kb", "turn-group-kb", fileReadClassification(), nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("group deny knowledge must not HostReject file_read: handled=%v err=%v", handled, err)
	}
	if planHasCapability(prepared.plan, tool.CapabilityKnowledgeReadLocal) {
		t.Fatalf("denied knowledge was selected: %+v", prepared.plan.Selections)
	}
	if !planHasCapability(prepared.plan, tool.CapabilityFSReadLocal) {
		t.Fatalf("file_read missing: %+v", prepared.plan.Selections)
	}
	if !planHasCapability(prepared.plan, tool.CapabilityMemoryRecallAgent) {
		t.Fatalf("group policy must not deny read-only memory recall: %+v", prepared.plan.Selections)
	}
}

func TestIMSemanticLightCloseDropsMemoryKeepsKnowledge(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileRead)}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看 notes.txt", "lansenger", "root-light-kb", "turn-light-kb", fileReadClassification(),
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if !planHasCapability(surface.plan, tool.CapabilityKnowledgeReadLocal) || !planHasCapability(surface.plan, tool.CapabilityMemoryRecallAgent) {
		t.Fatalf("plan should hold both warehouse tools before light close: %+v", surface.plan.Selections)
	}
	if planHasCapability(surface.plan, tool.CapabilityMemoryManageAgent) {
		t.Fatalf("ambient must select recall, not manage: %+v", surface.plan.Selections)
	}
	light := closedManagedSemanticDefinitionsForTurn(defs, surface, true)
	var hasKnowledge, hasRecall bool
	for _, def := range light {
		grant := surface.grants[extractToolName(def)]
		selection, ok := semanticSelectionByID(surface.plan, grant.SelectionID)
		if !ok {
			continue
		}
		switch selection.FitProof.MatchedCapability {
		case tool.CapabilityKnowledgeReadLocal:
			hasKnowledge = true
		case tool.CapabilityMemoryRecallAgent:
			hasRecall = true
		}
	}
	if !hasKnowledge {
		t.Fatalf("light close dropped read-only knowledge: %#v", light)
	}
	if !hasRecall {
		t.Fatal("light close must keep read-only memory recall")
	}
}
