package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func audioSynthesizeClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelAudioSynthesize, Confidence: .98}
}

func TestIMSemanticAudioSynthesizeLocalIsManagedOnDesktop(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(*audioSynthesizeClassification())
	if !managed || unmapped != "" {
		t.Fatalf("local speech must be managed, managed=%v unmapped=%q", managed, unmapped)
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, resolved, err := semanticIntentNeedsFromClassification(registry, *audioSynthesizeClassification())
	if err != nil || !resolved || len(needs) != 1 || needs[0].Capability != tool.CapabilityAudioSynthesizeLocal {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, resolved, err)
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "read this paragraph aloud", "desktop", "root-tts-local", "turn-tts-local", audioSynthesizeClassification(),
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("desktop speech must plan, handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, tool.CapabilityAudioSynthesizeLocal) {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	for _, selection := range prepared.plan.Selections {
		if selection.AdapterName == "tts" {
			t.Fatal("managed local speech selected the merged tts IM handler")
		}
		if selection.FitProof.MatchedCapability == tool.CapabilityAudioSynthesizeLocal && selection.AdapterName != "tts_local" {
			t.Fatalf("local adapter=%q", selection.AdapterName)
		}
	}
}

func TestIMSemanticAudioSynthesizeLocalUnmetOnLansenger(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "read this paragraph aloud", "lansenger", "root-tts-im", "turn-tts-im", audioSynthesizeClassification(),
	)
	if !handled {
		t.Fatal("IM speech must stay on the managed path")
	}
	if err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("IM speech must fail closed as unmet, err=%v", err)
	}
	if prepared != nil {
		if len(prepared.plan.Selections) != 0 {
			t.Fatalf("IM speech must not materialize tts or tts_local: %#v", prepared.plan.Selections)
		}
		for _, item := range prepared.plan.Unmet {
			switch item.ReasonCode {
			case "policy_denied", "no_feasible_provider", "catalog_incomplete":
				return
			}
		}
		t.Fatalf("unmet=%#v", prepared.plan.Unmet)
	}
}

func TestIMSemanticAudioSynthesizeLocalSchemaRejectsChannel(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "read this paragraph aloud", "desktop", "root-tts-schema", "turn-tts-schema", audioSynthesizeClassification(),
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	schema, ok := prepared.schemas["tts_local"]
	if !ok {
		t.Fatalf("tts_local schema missing: %#v", prepared.schemas)
	}
	if _, err := tool.CanonicalizeInvocationArguments(`{"channel":"lansenger","destination":"group:ops"}`, schema); err == nil {
		t.Fatal("channel/destination must be rejected")
	}
	if _, err := tool.CanonicalizeInvocationArguments(`{"text":"hello"}`, schema); err != nil {
		t.Fatalf("text-only args must be accepted: %v", err)
	}
}

func TestToolTTSLocalNeverReturnsVoiceBase64(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.toolTTSLocal(map[string]interface{}{"text": "hello", "channel": "lansenger"})
	if strings.Contains(got, "voice_base64") || strings.Contains(got, "[voice_base64") {
		t.Fatalf("channel arg leaked a voice payload: %q", got)
	}
	if !strings.Contains(got, "does not accept channel") {
		t.Fatalf("channel arg=%q", got)
	}
	got = h.toolTTSLocal(map[string]interface{}{"text": "hello"})
	if strings.Contains(got, "voice_base64") || strings.Contains(got, "[voice_base64") {
		t.Fatalf("empty app leaked a voice payload: %q", got)
	}
}

func TestReviewedDynamicRulesStayClearOfAudioSynthesize(t *testing.T) {
	if _, ok := imSemanticIntentRuleSet[intent.LabelAudioSynthesize]; !ok {
		t.Fatal("GUI rule set must map audio_synthesize")
	}
	for _, templates := range imSemanticIntentRuleSet {
		for _, template := range templates {
			if template.Capability == tool.CapabilityAudioSynthesizeSpeech {
				t.Fatal("managed rules must not map the merged IM speech capability")
			}
		}
	}
}
