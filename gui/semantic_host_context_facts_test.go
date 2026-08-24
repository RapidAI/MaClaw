package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestSemanticHostContextFactsAndConstraints(t *testing.T) {
	host := semanticHostContext{
		Channel: "lansenger", Destination: "group:ops", ExecutionLayer: string(executionLayerLight),
		ExpertSession: true, ComputerUseActive: true, GroupPolicyPresent: true,
		GroupWebSearchDenied: true, GroupKnowledgeDenied: true, GroupFileReadDenied: true,
	}
	facts := semanticHostContextFacts(host)
	if factAttr(facts, "destination_kind", "kind") != "group" {
		t.Fatalf("destination fact=%#v", facts)
	}
	if factAttr(facts, "schedule_dispatch_published", "published") != "true" {
		t.Fatalf("trusted group destination must publish schedule dispatch: %#v", facts)
	}
	if factAttr(facts, "audio_synthesize_local_published", "published") != "false" {
		t.Fatalf("lansenger must not publish local speech playback: %#v", facts)
	}
	if factAttr(facts, "voice_delivery_published", "published") != "true" {
		t.Fatalf("trusted lansenger group must publish voice delivery: %#v", facts)
	}
	if factAttr(facts, "execution_layer", "layer") != string(executionLayerLight) {
		t.Fatalf("execution layer fact=%#v", facts)
	}
	if factAttr(facts, "expert_session", "active") != "true" || factAttr(facts, "computer_use_active", "active") != "true" {
		t.Fatalf("session facts=%#v", facts)
	}
	if factAttr(facts, "group_permission", "web_search") != "false" || factAttr(facts, "group_permission", "knowledge") != "false" || factAttr(facts, "group_permission", "file_read") != "false" {
		t.Fatalf("group permission fact=%#v", facts)
	}
	constraints := semanticHostContextConstraints(host)
	if !hostConstraintDenies(constraints, "document.generate.file", nil) {
		t.Fatalf("light profile must deny generate: %#v", constraints)
	}
	if !hostConstraintDenies(constraints, "information.search.web", nil) {
		t.Fatalf("group policy must deny web search: %#v", constraints)
	}
	if !hostConstraintDenies(constraints, string(tool.CapabilityInformationFetchWeb), nil) {
		t.Fatalf("group policy must deny web fetch: %#v", constraints)
	}
	if !hostConstraintDenies(constraints, "knowledge.read.local", nil) {
		t.Fatalf("group policy must deny knowledge: %#v", constraints)
	}
	if !hostConstraintDenies(constraints, string(tool.CapabilityFSReadLocal), nil) {
		t.Fatalf("group policy must deny file read without a directory grant: %#v", constraints)
	}
	if !hostConstraintDenies(constraints, string(tool.CapabilityFSWriteLocal), nil) || !hostConstraintDenies(constraints, "visual.capture.desktop", nil) {
		t.Fatalf("group policy must deny local admin and desktop capture: %#v", constraints)
	}
	if hostConstraintDenies(constraints, string(tool.CapabilityScheduleDispatchChannel), nil) {
		t.Fatalf("trusted lansenger group must not deny schedule dispatch: %#v", constraints)
	}
	if !hostConstraintDenies(constraints, string(tool.CapabilityAudioSynthesizeLocal), nil) {
		t.Fatalf("lansenger must deny local speech playback: %#v", constraints)
	}
	if hostConstraintDenies(constraints, string(tool.CapabilityAudioRenderSpeech), nil) {
		t.Fatalf("trusted lansenger group must not deny speech render: %#v", constraints)
	}
	if hostConstraintDenies(constraints, "artifact.deliver.current_channel", map[string]string{"format": "voice"}) {
		t.Fatalf("trusted lansenger group must not deny voice delivery: %#v", constraints)
	}
}

func TestSemanticGroupPermissionDeniesSearchOnManagedPath(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	ctx := withSemanticGroupPermissions(context.Background(), &lansengerGroupPermissionPolicy{})
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "查天气", "lansenger", "root-group-deny", "turn-group-deny",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98}, nil,
	)
	if !handled {
		t.Fatal("group-denied search must stay on the managed path")
	}
	if err == nil || !strings.Contains(err.Error(), "policy_denied") {
		t.Fatalf("want policy_denied on managed path, handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
}

func TestSemanticGroupPermissionDeniesFetchOnManagedPath(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	ctx := withSemanticGroupPermissions(context.Background(), &lansengerGroupPermissionPolicy{})
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "抓取这个链接", "lansenger", "root-group-fetch-deny", "turn-group-fetch-deny",
		&intent.ClassificationResult{Primary: intent.LabelWebFetch, Confidence: .98}, nil,
	)
	if !handled {
		t.Fatal("group-denied fetch must stay on the managed path")
	}
	if err == nil || !strings.Contains(err.Error(), "policy_denied") {
		t.Fatalf("want fetch policy_denied, handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
}

func TestSemanticGroupPermissionDeniesFileReadWithoutDirectoryGrant(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	ctx := withSemanticGroupPermissions(context.Background(), &lansengerGroupPermissionPolicy{})
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "读一下这个文件", "lansenger", "root-group-file-deny", "turn-group-file-deny",
		&intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: .98}, nil,
	)
	if !handled {
		t.Fatal("group-denied file read must stay on the managed path")
	}
	if err == nil || !strings.Contains(err.Error(), "policy_denied") {
		t.Fatalf("want file-read policy_denied, handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
}

func TestSemanticLightLayerDeniesDocumentGenerate(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	ctx := withSemanticExecutionLayer(context.Background(), string(executionLayerLight))
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", "生成pdf报告", "desktop", "root-light-gen", "turn-light-gen",
		documentGenerateClassification(), nil,
	)
	if !handled {
		t.Fatal("light generate must stay on the managed path")
	}
	if err == nil || !strings.Contains(err.Error(), "policy_denied") {
		t.Fatalf("want generate policy_denied, handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
}

func hostConstraintDenies(constraints []tool.RoutingConstraint, capability string, qualifiers map[string]string) bool {
	for _, constraint := range constraints {
		if string(constraint.Capability) != capability || constraint.Effect != "deny" {
			continue
		}
		if len(constraint.Attributes) == 0 {
			return true
		}
		match := true
		for key, value := range constraint.Attributes {
			if qualifiers[key] != value {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
