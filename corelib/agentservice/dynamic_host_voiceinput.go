package agentservice

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type reviewedHostVoiceInput struct {
	Payload  coretool.ArtifactPayload
	FileName string
	MIMEType string
}

func reviewedHostDeliverableVoice(fileName, mimeType string) (name, canonicalMIME string, ok bool) {
	name = filepath.Base(strings.TrimSpace(fileName))
	canonicalMIME, ok = reviewedHostAudioMIME(agent.MessageAttachment{FileName: name, MimeType: mimeType})
	if !ok {
		return "", "", false
	}
	if name == "" || name == "." {
		name = reviewedHostAudioFallbackName(canonicalMIME)
	}
	return name, canonicalMIME, true
}

func reviewedHostVoiceInputsForTurn(rootTaskID, turnID, principalID string, attachments []agent.MessageAttachment) ([]reviewedHostVoiceInput, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	inputScope := coretool.InvocationScope{
		RootTaskID:  strings.TrimSpace(rootTaskID),
		PlanID:      "input:" + strings.TrimSpace(turnID),
		SessionID:   strings.TrimSpace(principalID),
		TurnID:      strings.TrimSpace(turnID),
		PrincipalID: strings.TrimSpace(principalID),
	}
	attachments = CanonicalizeReviewedHostMessageAttachments(attachments)
	inputs := make([]reviewedHostVoiceInput, 0, len(attachments))
	for index, attachment := range attachments {
		if _, _, ok := reviewedHostDocumentFormat(attachment.FileName, attachment.MimeType); ok {
			continue
		}
		if _, ok := reviewedHostImageFormat(attachment.FileName, attachment.MimeType); ok {
			continue
		}
		mimeType, ok := reviewedHostAudioMIME(attachment)
		if !ok {
			continue
		}
		raw, err := decodeReviewedHostAttachmentBytes(attachment.Data)
		if err != nil {
			return nil, fmt.Errorf("trusted_audio_attachment_content_missing")
		}
		if len(raw) == 0 || len(raw) > ReviewedHostAudioTranscribeMaxBytes {
			return nil, fmt.Errorf("trusted_audio_attachment_too_large")
		}
		encoded := base64.StdEncoding.EncodeToString(raw)
		sourceID := strings.TrimSpace(attachment.SourceMediaID)
		if sourceID == "" {
			sourceID = fmt.Sprintf("attachment:%d:%s:%s", index, filepath.Base(attachment.FileName), mimeType)
		}
		producer := "trusted-input:host-voice:" + coretool.SchemaDigest([]byte(sourceID))[:24]
		payload, err := coretool.NewArtifactPayload(inputScope, producer, "voice", mimeType, encoded, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("trusted_audio_attachment_invalid: %w", err)
		}
		fileName := filepath.Base(strings.TrimSpace(attachment.FileName))
		if fileName == "" || fileName == "." {
			fileName = reviewedHostAudioFallbackName(mimeType)
		}
		inputs = append(inputs, reviewedHostVoiceInput{Payload: payload, FileName: fileName, MIMEType: mimeType})
	}
	return inputs, nil
}

func applyReviewedHostVoiceDeliverInputs(needs []coretool.CapabilityNeed, inputs []reviewedHostVoiceInput) ([]coretool.CapabilityNeed, error) {
	if len(inputs) != 1 {
		if len(inputs) == 0 {
			return nil, fmt.Errorf("trusted_document_input_missing")
		}
		return nil, fmt.Errorf("trusted_document_input_ambiguous")
	}
	resolved := append([]coretool.CapabilityNeed(nil), needs...)
	for index := range resolved {
		if !reviewedHostAttachmentDeliverNeed(resolved[index]) {
			continue
		}
		resolved[index].Qualifiers = map[string]string{QualifierArtifactFormat: ArtifactFormatVoice}
	}
	return resolved, nil
}
