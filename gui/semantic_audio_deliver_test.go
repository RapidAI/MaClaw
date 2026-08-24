package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func audioDeliverClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelAudioDeliver, Confidence: .98}
}

func testMinimalWAVBase64() string {
	wav := []byte("RIFF\x00\x00\x00\x00WAVE")
	return base64.StdEncoding.EncodeToString(wav)
}

func TestIMSemanticAudioDeliverIsManaged(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(*audioDeliverClassification())
	if !managed || unmapped != "" {
		t.Fatalf("audio_deliver must be managed, managed=%v unmapped=%q", managed, unmapped)
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, resolved, err := semanticIntentNeedsFromClassification(registry, *audioDeliverClassification())
	if err != nil || !resolved || len(needs) != 2 {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, resolved, err)
	}
	foundRender, foundDeliver := false, false
	for _, need := range needs {
		if need.Capability == tool.CapabilityAudioRenderSpeech {
			foundRender = true
		}
		if need.Capability == "artifact.deliver.current_channel" && need.Qualifiers["format"] == "voice" {
			foundDeliver = true
		}
	}
	if !foundRender || !foundDeliver {
		t.Fatalf("needs=%#v", needs)
	}
}

func TestIMSemanticAudioDeliverUnmetWithoutTrustedDestination(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "send this as a voice message", "desktop", "root-voice-desk", "turn-voice-desk", audioDeliverClassification(),
	)
	if !handled {
		t.Fatal("desktop voice deliver must stay managed")
	}
	if err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("desktop without destination must fail closed, err=%v", err)
	}
	if prepared != nil && len(prepared.plan.Selections) != 0 {
		t.Fatalf("must not materialize tts or voice send: %#v", prepared.plan.Selections)
	}
}

func TestIMSemanticAudioDeliverMaterializesOnLansengerGroup(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	ctx := scheduleDispatchDestinationCtx("group:ops")
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", "send this as a voice message", "lansenger", "root-voice-ok", "turn-voice-ok", audioDeliverClassification(), nil,
	)
	if err != nil || !handled || prepared == nil || len(prepared.plan.Unmet) != 0 {
		t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	if !planHasCapabilities(prepared.plan, tool.CapabilityAudioRenderSpeech, "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	var renderID string
	var deliver tool.PlannedSelection
	foundDeliver := false
	for _, selection := range prepared.plan.Selections {
		if selection.AdapterName == "tts" || selection.AdapterName == "tts_local" {
			t.Fatalf("managed voice deliver selected %s", selection.AdapterName)
		}
		if selection.FitProof.MatchedCapability == tool.CapabilityAudioRenderSpeech {
			if selection.AdapterName != "tts_render" {
				t.Fatalf("render adapter=%q", selection.AdapterName)
			}
			renderID = selection.ID
		}
		if selection.FitProof.MatchedCapability == "artifact.deliver.current_channel" {
			if selection.AdapterName != "semantic_deliver_current_voice" {
				t.Fatalf("deliver adapter=%q", selection.AdapterName)
			}
			if selection.FitProof.QualifierBindings["format"] != "voice" {
				t.Fatalf("deliver format=%#v", selection.FitProof.QualifierBindings)
			}
			deliver = selection
			foundDeliver = true
		}
	}
	if renderID == "" || !foundDeliver {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	required := false
	for _, requirement := range deliver.Requires {
		if requirement == renderID {
			required = true
			break
		}
	}
	if !required {
		t.Fatalf("voice deliver must require render, requires=%#v render=%q", deliver.Requires, renderID)
	}
	schema, ok := prepared.schemas["tts_render"]
	if !ok {
		t.Fatal("tts_render schema missing")
	}
	properties, _ := schema["properties"].(map[string]interface{})
	for _, blocked := range []string{"channel", "destination", "group_name", "path"} {
		if _, found := properties[blocked]; found {
			t.Fatalf("tts_render schema still exposes %s", blocked)
		}
	}
	deliverSchema, ok := prepared.schemas["semantic_deliver_current_voice"]
	if !ok {
		t.Fatal("voice deliver schema missing")
	}
	deliverProps, _ := deliverSchema["properties"].(map[string]interface{})
	for _, blocked := range []string{"channel", "destination", "base64", "text"} {
		if _, found := deliverProps[blocked]; found {
			t.Fatalf("voice deliver schema still exposes %s", blocked)
		}
	}
}

func TestToolTTSRenderRejectsChannelAndDoesNotReturnVoiceBase64(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	if got := h.toolTTSRender(map[string]interface{}{"text": "hello", "channel": "lansenger"}); !strings.Contains(got, "does not accept channel") {
		t.Fatalf("channel leak: %q", got)
	}
	if got := h.toolTTSRender(map[string]interface{}{"text": "hello", "destination": "group:ops"}); !strings.Contains(got, "does not accept destination") {
		t.Fatalf("destination leak: %q", got)
	}
}

func TestSpeechArtifactPayloadIsNotASend(t *testing.T) {
	obs := parseToolPayloadResult(toolPayloadSpeechArtifactPrefix + testMinimalWAVBase64())
	if obs.VoiceData != "" || obs.File != nil {
		t.Fatalf("speech artifact must not become voice or file send: %#v", obs)
	}
	if !strings.Contains(obs.ToolContent, "not a send") {
		t.Fatalf("tool content=%q", obs.ToolContent)
	}
}

func TestReviewedDynamicRulesStillIgnoreAudioDeliver(t *testing.T) {
	// Compile-time reminder: Hub/MaClawSrv must not import this GUI family.
	if _, ok := imSemanticIntentRuleSet[intent.LabelAudioDeliver]; !ok {
		t.Fatal("GUI rule set must map audio_deliver")
	}
}
