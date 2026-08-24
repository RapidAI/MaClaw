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

const reviewedHostImageDeliverMaxBytes = 10 << 20

type reviewedHostImageInput struct {
	Payload  coretool.ArtifactPayload
	FileName string
	MIMEType string
}

// ReviewedHostTrustedImageMIME reports whether a host attachment is in the
// closed current-channel image allowlist and returns the canonical MIME.
func ReviewedHostTrustedImageMIME(fileName, mimeType string) (string, bool) {
	return reviewedHostImageFormat(fileName, mimeType)
}

func reviewedHostImageFormat(fileName, mimeType string) (canonicalMIME string, ok bool) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])) {
	case "image/png":
		return "image/png", true
	case "image/jpeg", "image/jpg":
		return "image/jpeg", true
	case "image/gif":
		return "image/gif", true
	case "image/webp":
		return "image/webp", true
	default:
		return "", false
	}
}

func reviewedHostSniffTrustedImage(raw []byte) (mime, name string, ok bool) {
	if len(raw) >= 8 && raw[0] == 0x89 && string(raw[1:4]) == "PNG" {
		return "image/png", "photo.png", true
	}
	if len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF {
		return "image/jpeg", "photo.jpg", true
	}
	if len(raw) >= 6 && (string(raw[:6]) == "GIF87a" || string(raw[:6]) == "GIF89a") {
		return "image/gif", "photo.gif", true
	}
	if len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		return "image/webp", "photo.webp", true
	}
	return "", "", false
}

func reviewedHostDeliverableImage(fileName, mimeType string) (name, canonicalMIME string, ok bool) {
	name = filepath.Base(strings.TrimSpace(fileName))
	canonicalMIME, ok = reviewedHostImageFormat(name, mimeType)
	if !ok {
		return "", "", false
	}
	if name == "" || name == "." {
		name = reviewedHostImageFallbackName(canonicalMIME)
	}
	return name, canonicalMIME, true
}

func reviewedHostImageInputsForTurn(rootTaskID, turnID, principalID string, attachments []agent.MessageAttachment) ([]reviewedHostImageInput, error) {
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
	inputs := make([]reviewedHostImageInput, 0, len(attachments))
	for index, attachment := range attachments {
		if _, _, ok := reviewedHostDocumentFormat(attachment.FileName, attachment.MimeType); ok {
			continue
		}
		mimeType, ok := reviewedHostImageFormat(attachment.FileName, attachment.MimeType)
		if !ok {
			continue
		}
		raw, err := decodeReviewedHostAttachmentBytes(attachment.Data)
		if err != nil {
			return nil, fmt.Errorf("trusted_image_attachment_content_missing")
		}
		if len(raw) == 0 || int64(len(raw)) > reviewedHostImageDeliverMaxBytes {
			return nil, fmt.Errorf("trusted_image_attachment_too_large")
		}
		encoded := base64.StdEncoding.EncodeToString(raw)
		sourceID := strings.TrimSpace(attachment.SourceMediaID)
		if sourceID == "" {
			sourceID = fmt.Sprintf("attachment:%d:%s:%s", index, filepath.Base(attachment.FileName), mimeType)
		}
		producer := "trusted-input:host-image:" + coretool.SchemaDigest([]byte(sourceID))[:24]
		payload, err := coretool.NewArtifactPayload(inputScope, producer, "image", mimeType, encoded, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("trusted_image_attachment_invalid: %w", err)
		}
		fileName := filepath.Base(strings.TrimSpace(attachment.FileName))
		if fileName == "" || fileName == "." {
			fileName = reviewedHostImageFallbackName(mimeType)
		}
		inputs = append(inputs, reviewedHostImageInput{Payload: payload, FileName: fileName, MIMEType: mimeType})
	}
	return inputs, nil
}

type reviewedHostDeliverableTurnInputs struct {
	Documents   []reviewedHostDocumentInput
	DocumentErr error
	Images      []reviewedHostImageInput
	ImageErr    error
	Voices      []reviewedHostVoiceInput
	VoiceErr    error
}

func bindReviewedHostDeliverableTurn(needs []coretool.CapabilityNeed, turn reviewedHostDeliverableTurnInputs) ([]coretool.CapabilityNeed, *reviewedHostDocumentInput, *reviewedHostImageInput, *reviewedHostVoiceInput, error) {
	if !reviewedHostDocumentNeedPresent(needs) {
		return needs, nil, nil, nil, nil
	}
	hasDocumentRead := false
	hasAttachment := false
	for _, need := range needs {
		if need.Capability == CapabilityDocumentRead {
			hasDocumentRead = true
		}
		if reviewedHostAttachmentDeliverNeed(need) {
			hasAttachment = true
		}
	}
	if hasDocumentRead {
		resolved, err := bindReviewedHostDocumentTurn(needs, turn.Documents, turn.DocumentErr)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if len(turn.Documents) != 1 {
			return resolved, nil, nil, nil, nil
		}
		return resolved, &turn.Documents[0], nil, nil, nil
	}
	if (reviewedHostGenerateNeedPresent(needs) || reviewedHostAudioRenderNeedPresent(needs) || reviewedHostVisualCaptureNeedPresent(needs)) && hasAttachment {
		return needs, nil, nil, nil, nil
	}
	if !hasAttachment {
		return needs, nil, nil, nil, nil
	}
	if turn.DocumentErr != nil {
		return nil, nil, nil, nil, turn.DocumentErr
	}
	if turn.ImageErr != nil {
		return nil, nil, nil, nil, turn.ImageErr
	}
	if turn.VoiceErr != nil {
		return nil, nil, nil, nil, turn.VoiceErr
	}
	docN, imgN, voiceN := len(turn.Documents), len(turn.Images), len(turn.Voices)
	if docN == 1 && imgN == 0 && voiceN == 0 {
		resolved, err := applyReviewedHostDocumentInputs(needs, turn.Documents)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return resolved, &turn.Documents[0], nil, nil, nil
	}
	if docN == 0 && imgN == 1 && voiceN == 0 {
		resolved, err := applyReviewedHostImageDeliverInputs(needs, turn.Images)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return resolved, nil, &turn.Images[0], nil, nil
	}
	if docN == 0 && imgN == 0 && voiceN == 1 {
		resolved, err := applyReviewedHostVoiceDeliverInputs(needs, turn.Voices)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return resolved, nil, nil, &turn.Voices[0], nil
	}
	if docN+imgN+voiceN == 0 {
		return nil, nil, nil, nil, fmt.Errorf("trusted_document_input_missing")
	}
	return nil, nil, nil, nil, fmt.Errorf("trusted_document_input_ambiguous")
}

func applyReviewedHostImageDeliverInputs(needs []coretool.CapabilityNeed, inputs []reviewedHostImageInput) ([]coretool.CapabilityNeed, error) {
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
		resolved[index].Qualifiers = map[string]string{QualifierArtifactFormat: ArtifactFormatImage}
	}
	return resolved, nil
}
