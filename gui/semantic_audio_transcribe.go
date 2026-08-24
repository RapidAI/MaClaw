package main

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const semanticTrustedAudioTranscribeAdapter = "semantic_transcribe_trusted_audio"

// semanticAudioInputsForTurn normalizes the current turn's audio bytes into a
// host-scoped artifact payload. SessionID/principalID must match the
// InvocationScope used for the plan so that artifact projection can succeed
// (see semanticDocumentInputsForTurn for the same invariant).
func semanticAudioInputsForTurn(rootTaskID, turnID, sessionID, principalID string, attachments []MessageAttachment) ([]semanticTrustedArtifactInput, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(principalID) == "" {
		return nil, fmt.Errorf("trusted_audio_input_identity_required")
	}
	inputScope := tool.InvocationScope{
		RootTaskID:  rootTaskID,
		PlanID:      "input:" + strings.TrimSpace(turnID),
		SessionID:   sessionID,
		TurnID:      turnID,
		PrincipalID: principalID,
	}
	inputs := make([]semanticTrustedArtifactInput, 0, len(attachments))
	for index, attachment := range attachments {
		mimeType, ok := agentservice.ReviewedHostTrustedAudioMIME(attachment)
		if !ok {
			continue
		}
		raw, err := decodeSemanticAttachmentBytes(attachment.Data)
		if err != nil {
			return nil, fmt.Errorf("trusted_audio_attachment_content_missing")
		}
		if len(raw) == 0 || len(raw) > agentservice.ReviewedHostAudioTranscribeMaxBytes {
			return nil, fmt.Errorf("trusted_audio_attachment_too_large")
		}
		sourceID := strings.TrimSpace(attachment.SourceMediaID)
		if sourceID == "" {
			sourceID = fmt.Sprintf("attachment:%d:%s:%s", index, filepath.Base(attachment.FileName), mimeType)
		}
		producer := "trusted-input:channel-attachment:" + tool.SchemaDigest([]byte(sourceID))[:24]
		payload, err := tool.NewArtifactPayload(inputScope, producer, "audio", mimeType, base64.StdEncoding.EncodeToString(raw), time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("trusted_audio_attachment_invalid: %w", err)
		}
		inputs = append(inputs, semanticTrustedArtifactInput{Payload: payload})
	}
	return inputs, nil
}

func semanticNeedsForTrustedAudioInputs(h *IMMessageHandler, needs []tool.CapabilityNeed, inputs []semanticTrustedArtifactInput) ([]tool.CapabilityNeed, error) {
	if !semanticAudioTranscribeNeedPresent(needs) {
		return needs, nil
	}
	if !h.semanticTrustedAudioReady() {
		return nil, fmt.Errorf("trusted_audio_transcribe_unavailable")
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("trusted_audio_input_missing")
	}
	if len(inputs) != 1 {
		return nil, fmt.Errorf("trusted_audio_input_ambiguous")
	}
	return needs, nil
}

func semanticAudioTranscribeNeedPresent(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if need.Capability == tool.CapabilityAudioTranscribeSpeech {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) semanticTrustedAudioReady() bool {
	if h == nil {
		return false
	}
	if h.semanticTrustedAudioTranscribe != nil {
		return true
	}
	return h.app != nil && h.app.GetASREnabled() && h.app.IsASRReady()
}

func semanticUnpublishedLegacyASRProvider(registered RegisteredTool) bool {
	return registered.Name == "asr"
}

func semanticTrustedAudioTranscribeDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedAudioTranscribeAdapter,
			"description": "Transcribe the approved current-turn audio attachment. The audio identity is host-bound; no path, format, language, or minutes fields are accepted.",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
}

func semanticTrustedAudioFacts(inputs []semanticTrustedArtifactInput) []tool.RoutingFact {
	facts := make([]tool.RoutingFact, 0, len(inputs))
	for _, input := range inputs {
		binding := tool.ArtifactBindingFromRef(input.Payload.Ref)
		facts = append(facts, tool.RoutingFact{
			ID:   "trusted-audio:" + input.Payload.Ref.ID,
			Kind: "artifact_available",
			Attributes: map[string]string{
				"artifact_id": input.Payload.Ref.ID,
				"kind":        input.Payload.Ref.Kind,
				"mime_type":   input.Payload.Ref.MIMEType,
			},
			Artifact:  &binding,
			Authority: tool.AuthorityChannel,
		})
	}
	return facts
}

func decodeSemanticAttachmentBytes(data string) ([]byte, error) {
	encoded := strings.TrimSpace(data)
	if encoded == "" {
		return nil, fmt.Errorf("trusted_attachment_content_missing")
	}
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding}
	if strings.ContainsAny(encoded, "-_") {
		encodings = append(encodings, base64.URLEncoding, base64.RawURLEncoding)
	}
	for _, enc := range encodings {
		raw, err := enc.DecodeString(encoded)
		if err == nil && len(raw) > 0 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("trusted_attachment_content_missing")
}

func (h *IMMessageHandler) transcribeTrustedAudioBytes(mime string, data []byte) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_audio_transcribe_unavailable")
	}
	if h.semanticTrustedAudioTranscribe != nil {
		return h.semanticTrustedAudioTranscribe(mime, data)
	}
	if h.app == nil {
		return "", fmt.Errorf("trusted_audio_transcribe_unavailable")
	}
	wav, err := audioconv.ToWAV(data, mime)
	if err != nil {
		return "", err
	}
	return h.app.transcribeWAVBytes(wav, asrTranscribeOpts{})
}

func semanticTrustedAudioResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_audio_transcribe_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_audio_transcribe_empty")
	}
	return text, nil
}

func semanticTrustedAudioArgsAllowed(args map[string]interface{}) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("trusted_audio_transcribe_arguments_rejected")
}
