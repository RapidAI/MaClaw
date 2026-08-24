package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func audioTranscribeClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelAudioTranscribe, Confidence: .98, ToolNames: []string{"asr", "office"}}
}

func testTrustedAudioAttachment() MessageAttachment {
	return MessageAttachment{
		Type:          "audio",
		FileName:      "clip.wav",
		MimeType:      "audio/wav",
		SourceMediaID: "weixin-media:voice-1",
		Data:          base64.StdEncoding.EncodeToString([]byte("RIFF....WAVE")),
	}
}

func TestIMSemanticAudioTranscribeUsesTrustedAttachmentNotASR(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelAudioTranscribe)}
	h.semanticTrustedAudioTranscribe = func(mime string, data []byte) (string, error) {
		if mime != "audio/wav" || string(data) != "RIFF....WAVE" {
			t.Fatalf("transcriber input mime=%q data=%q", mime, data)
		}
		return "recognized speech", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "转写一下这段录音", "lansenger", "root-asr", "turn-asr",
		audioTranscribeClassification(), []MessageAttachment{testTrustedAudioAttachment()},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedAudioTranscribeAdapter || selection.FitProof.MatchedCapability != tool.CapabilityAudioTranscribeSpeech {
		t.Fatalf("selection=%+v", selection)
	}
	if len(selection.ArtifactDependencies) != 1 || selection.ArtifactDependencies[0].ProducerSelection != "" {
		t.Fatalf("audio dependency=%#v", selection.ArtifactDependencies)
	}
	definition := defs[0]["function"].(map[string]interface{})
	if extractToolName(defs[0]) != "asr" || definition["name"] != "asr" {
		t.Fatalf("managed transcribe name=%q, want asr", extractToolName(defs[0]))
	}
	if selection.AdapterName == "asr" {
		t.Fatal("adapter leaked soup name")
	}
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	for _, forbidden := range []string{"path", "url", "file_path", "format", "for_minutes", "minutes", "language", "action", "channel", "destination"} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing audio schema exposed %q: %#v", forbidden, properties)
		}
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedAudioTranscribeAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"path":"C:/secret.wav"}`); !strings.Contains(got, "parameter_unknown_field") {
		t.Fatalf("forged path result=%q", got)
	}
}

func TestIMSemanticAudioTranscribeExecutesHostBytes(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelAudioTranscribe)}
	h.semanticTrustedAudioTranscribe = func(mime string, data []byte) (string, error) {
		if mime != "audio/wav" || string(data) != "RIFF....WAVE" {
			t.Fatalf("transcriber input mime=%q data=%q", mime, data)
		}
		return "recognized speech", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "转写一下这段录音", "lansenger", "root-asr-exec", "turn-asr-exec",
		audioTranscribeClassification(), []MessageAttachment{testTrustedAudioAttachment()},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); got != "recognized speech" {
		t.Fatalf("bound transcribe=%q", got)
	}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", got)
	}
}

func TestIMSemanticAudioTranscribeFailClosedWithoutAttachmentOrEngine(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedAudioTranscribe = func(string, []byte) (string, error) {
		t.Fatal("missing attachment must not reach the transcriber")
		return "", nil
	}
	classification := audioTranscribeClassification()
	_, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"user-1", "转写一下这段录音", "lansenger", "root-missing", "turn-missing", classification, nil,
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_audio_input_missing") {
		t.Fatalf("missing audio handled=%v err=%v", handled, err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("RIFF....WAVE"))
	_, handled, err = h.semanticPlanForTurnWithClassificationAndAttachments(
		"user-1", "转写两段", "lansenger", "root-two", "turn-two", classification, []MessageAttachment{
			{Type: "audio", FileName: "a.wav", MimeType: "audio/wav", Data: encoded},
			{Type: "audio", FileName: "b.wav", MimeType: "audio/wav", Data: encoded},
		},
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_audio_input_ambiguous") {
		t.Fatalf("ambiguous audio handled=%v err=%v", handled, err)
	}

	unready := &IMMessageHandler{registry: NewToolRegistry()}
	_, handled, err = unready.semanticPlanForTurnWithClassificationAndAttachments(
		"user-1", "转写一下这段录音", "lansenger", "root-unready", "turn-unready", classification,
		[]MessageAttachment{testTrustedAudioAttachment()},
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_audio_transcribe_unavailable") {
		t.Fatalf("unready engine handled=%v err=%v", handled, err)
	}
}

func TestIMSemanticAudioTranscribeSniffsUnnamedWAVAndRejectsDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelAudioTranscribe)}
	h.semanticTrustedAudioTranscribe = func(mime string, _ []byte) (string, error) {
		if mime != "audio/wav" {
			t.Fatalf("sniffed mime=%q", mime)
		}
		return "[voice_base64|audio/wav]AAAA", nil
	}
	wav := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "转写一下这段录音", "weixin", "root-sniff", "turn-sniff",
		audioTranscribeClassification(), []MessageAttachment{{
			Type:     "file",
			FileName: "file",
			MimeType: "application/octet-stream",
			Data:     base64.StdEncoding.EncodeToString(wav),
		}},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if surface.plan.Selections[0].AdapterName != semanticTrustedAudioTranscribeAdapter {
		t.Fatalf("selection=%+v", surface.plan.Selections[0])
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_audio_transcribe_delivery_token") {
		t.Fatalf("delivery token result=%q", got)
	}
}

func TestIMSemanticDocumentReadSniffsUnnamedPDF(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	classification := &intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}
	prepared, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"user-1", "文件里有什么", "weixin", "root-pdf", "turn-pdf", classification, []MessageAttachment{{
			Type:     "file",
			FileName: "file",
			MimeType: "application/octet-stream",
			Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n")),
		}},
	)
	if err != nil || !handled || prepared == nil || len(prepared.plan.Unmet) != 0 {
		t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	if !planHasCapabilities(prepared.plan, "document.read.local") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
}
