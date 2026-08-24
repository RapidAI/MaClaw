package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostAttachmentDeliverProviderID     = "core-attachmentdeliver"
	reviewedHostAttachmentDeliverImplementation = "channel"
	reviewedHostAttachmentDeliverAdapterName    = "host_artifact_deliver_current"
)

type reviewedHostAttachmentDeliverer interface {
	PrepareReviewedHostAttachmentDeliver(ctx context.Context, principal Principal, destinationID string) (string, error)
}

func reviewedHostAttachmentDeliverInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostAttachmentDeliverContractDigest() string {
	return coretool.SchemaDigest([]byte("artifact.deliver.current_channel:v1:host-attachmentdeliver-file-image-voice"))
}

// ProjectReviewedHostAttachmentDeliverProvider projects a current-channel
// echo of the turn's one trusted document, image, or audio attachment. It is not a
// Skill/MCP discovery entry and must not import GUI send_file / send_to_im.
// The closed schema is empty. Path, artifact ID, channel, and destination
// are rejected. Destinations come only from inbound transport. Prepare is
// not a send.
func ProjectReviewedHostAttachmentDeliverProvider(deliverer reviewedHostAttachmentDeliverer, destinationID string) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if deliverer == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host attachment deliverer is unavailable")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("trusted_delivery_target_missing")
	}
	parameters := reviewedHostAttachmentDeliverInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host attachment deliver schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostAttachmentDeliverContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-attachmentdeliver-file-image-voice-v1", contractDigest, invocationDigest, destinationID,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostAttachmentDeliverAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostAttachmentDeliverProviderID,
			ImplementationID: reviewedHostAttachmentDeliverImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityArtifactDeliverCurrent,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
			Quality:    1,
		}, {
			Capability: CapabilityArtifactDeliverCurrent,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage},
			Quality:    1,
		}, {
			Capability: CapabilityArtifactDeliverCurrent,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatVoice},
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostAttachmentDeliver(deliverer, destinationID)}, nil
}

func AttachReviewedHostAttachmentDeliverProvider(catalog DynamicSemanticCatalog, deliverer reviewedHostAttachmentDeliverer, destinationID string) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostAttachmentDeliverProvider(deliverer, destinationID)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostAttachmentDeliver(deliverer reviewedHostAttachmentDeliverer, destinationID string) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if deliverer == nil {
			return "", fmt.Errorf("host_attachment_deliver_unavailable")
		}
		if err := reviewedHostAttachmentDeliverArgsAllowed(args); err != nil {
			return "", err
		}
		if !reviewedHostTrustedDestination(destinationID) {
			return "", fmt.Errorf("trusted_delivery_target_missing")
		}
		return deliverer.PrepareReviewedHostAttachmentDeliver(ctx, principal, destinationID)
	}
}

func reviewedHostAttachmentDeliverArgsAllowed(args map[string]interface{}) error {
	if len(args) > 0 {
		return fmt.Errorf("host_attachment_deliver_arguments_rejected")
	}
	return nil
}

func (c *coreAgentCallbacks) PrepareReviewedHostAttachmentDeliver(ctx context.Context, principal Principal, destinationID string) (string, error) {
	if c == nil || (c.reviewedHostDocument == nil && c.reviewedHostImage == nil && c.reviewedHostVoice == nil && c.reviewedHostGeneratedDocument == nil && c.reviewedHostGeneratedSpeech == nil && c.reviewedHostGeneratedImage == nil) {
		return "", fmt.Errorf("host_attachment_deliver_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_attachment_deliver_principal_mismatch")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return "", fmt.Errorf("trusted_delivery_target_missing")
	}
	if destinationID != strings.TrimSpace(c.trustedDestinationID) {
		return "", fmt.Errorf("trusted_delivery_target_mismatch")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	data, fileName, mimeType, err := c.reviewedHostAttachmentDeliverPayload()
	if err != nil {
		return "", err
	}
	if deps, armed := c.reviewedHostAttachmentDeliverFireDeps(destinationID, data, fileName, mimeType); armed {
		if err := FireReviewedHostFileDeliver(ctx, deps); err != nil {
			return "", err
		}
		return "", fmt.Errorf("host_file_deliver_unknown")
	}
	return "Trusted attachment prepared for " + destinationID + ". This is not a send.", nil
}

func (c *coreAgentCallbacks) reviewedHostAttachmentDeliverPayload() (data []byte, fileName, mimeType string, err error) {
	if c.reviewedHostGeneratedDocument != nil {
		data = append([]byte(nil), c.reviewedHostGeneratedDocument.Data...)
		if len(data) == 0 {
			decoded, decodeErr := base64.StdEncoding.DecodeString(c.reviewedHostGeneratedDocument.Payload.Base64)
			if decodeErr != nil || len(decoded) == 0 {
				return nil, "", "", fmt.Errorf("trusted_document_attachment_content_missing")
			}
			data = decoded
		}
		fileName = strings.TrimSpace(c.reviewedHostGeneratedDocument.FileName)
		if fileName == "" {
			fileName = "document.pdf"
		}
		mimeType = strings.TrimSpace(c.reviewedHostGeneratedDocument.MIMEType)
		if mimeType == "" {
			mimeType = "application/pdf"
		}
		if _, _, ok := reviewedHostDeliverableDocument(fileName, mimeType); !ok {
			return nil, "", "", fmt.Errorf("host_file_deliver_document_required")
		}
		return data, fileName, mimeType, nil
	}
	if c.reviewedHostGeneratedSpeech != nil {
		data = append([]byte(nil), c.reviewedHostGeneratedSpeech.Data...)
		if len(data) == 0 {
			decoded, decodeErr := base64.StdEncoding.DecodeString(c.reviewedHostGeneratedSpeech.Payload.Base64)
			if decodeErr != nil || len(decoded) == 0 {
				return nil, "", "", fmt.Errorf("trusted_audio_attachment_content_missing")
			}
			data = decoded
		}
		fileName = strings.TrimSpace(c.reviewedHostGeneratedSpeech.FileName)
		if fileName == "" {
			fileName = "speech.wav"
		}
		mimeType = strings.TrimSpace(c.reviewedHostGeneratedSpeech.MIMEType)
		if mimeType == "" {
			mimeType = "audio/wav"
		}
		if _, _, ok := reviewedHostDeliverableVoice(fileName, mimeType); !ok {
			return nil, "", "", fmt.Errorf("host_file_deliver_voice_required")
		}
		return data, fileName, mimeType, nil
	}
	if c.reviewedHostGeneratedImage != nil {
		data = append([]byte(nil), c.reviewedHostGeneratedImage.Data...)
		if len(data) == 0 {
			decoded, decodeErr := base64.StdEncoding.DecodeString(c.reviewedHostGeneratedImage.Payload.Base64)
			if decodeErr != nil || len(decoded) == 0 {
				return nil, "", "", fmt.Errorf("trusted_image_attachment_content_missing")
			}
			data = decoded
		}
		fileName = strings.TrimSpace(c.reviewedHostGeneratedImage.FileName)
		if fileName == "" {
			fileName = "screenshot.png"
		}
		mimeType = strings.TrimSpace(c.reviewedHostGeneratedImage.MIMEType)
		if mimeType == "" {
			mimeType = "image/png"
		}
		if _, _, ok := reviewedHostDeliverableImage(fileName, mimeType); !ok {
			return nil, "", "", fmt.Errorf("host_file_deliver_image_required")
		}
		return data, fileName, mimeType, nil
	}
	if c.reviewedHostVoice != nil {
		data, err = base64.StdEncoding.DecodeString(c.reviewedHostVoice.Payload.Base64)
		if err != nil || len(data) == 0 {
			return nil, "", "", fmt.Errorf("trusted_audio_attachment_content_missing")
		}
		fileName = strings.TrimSpace(c.reviewedHostVoice.FileName)
		mimeType = strings.TrimSpace(c.reviewedHostVoice.MIMEType)
		if fileName == "" {
			fileName = reviewedHostAudioFallbackName(mimeType)
		}
		if _, _, ok := reviewedHostDeliverableVoice(fileName, mimeType); !ok {
			return nil, "", "", fmt.Errorf("host_file_deliver_voice_required")
		}
		return data, fileName, mimeType, nil
	}
	if c.reviewedHostImage != nil {
		data, err = base64.StdEncoding.DecodeString(c.reviewedHostImage.Payload.Base64)
		if err != nil || len(data) == 0 {
			return nil, "", "", fmt.Errorf("trusted_image_attachment_content_missing")
		}
		fileName = strings.TrimSpace(c.reviewedHostImage.FileName)
		mimeType = strings.TrimSpace(c.reviewedHostImage.MIMEType)
		if fileName == "" {
			fileName = reviewedHostImageFallbackName(mimeType)
		}
		if _, _, ok := reviewedHostDeliverableImage(fileName, mimeType); !ok {
			return nil, "", "", fmt.Errorf("host_file_deliver_image_required")
		}
		return data, fileName, mimeType, nil
	}
	data, err = base64.StdEncoding.DecodeString(c.reviewedHostDocument.Payload.Base64)
	if err != nil || len(data) == 0 {
		return nil, "", "", fmt.Errorf("trusted_document_attachment_content_missing")
	}
	fileName = strings.TrimSpace(c.reviewedHostDocument.FileName)
	if fileName == "" {
		fileName = "document" + strings.TrimSpace(c.reviewedHostDocument.Suffix)
	}
	mimeType = strings.TrimSpace(c.reviewedHostDocument.Payload.Ref.MIMEType)
	if _, _, ok := reviewedHostDeliverableDocument(fileName, mimeType); !ok {
		return nil, "", "", fmt.Errorf("host_file_deliver_document_required")
	}
	return data, fileName, mimeType, nil
}

func (c *coreAgentCallbacks) reviewedHostAttachmentDeliverFireDeps(destinationID string, data []byte, fileName, mimeType string) (reviewedHostFileDeliverFireDeps, bool) {
	if c == nil || c.executor == nil || c.executor.ScheduleDispatchFileSend == nil || len(data) == 0 {
		return reviewedHostFileDeliverFireDeps{}, false
	}
	coordinator := c.executor.reviewedHostScheduleDispatchCoordinator()
	if coordinator == nil {
		return reviewedHostFileDeliverFireDeps{}, false
	}
	if _, ok := reviewedHostScheduleDispatchFireChannel(c.inboundChannelScope); !ok {
		return reviewedHostFileDeliverFireDeps{}, false
	}
	principalID := strings.TrimSpace(c.principal.UserID)
	if principalID == "" {
		principalID = memoryOwnerIDForPrincipal(c.principal)
	}
	send := c.executor.ScheduleDispatchFileSend
	principal := c.principal
	return reviewedHostFileDeliverFireDeps{
		Coordinator:   coordinator,
		ChannelScope:  c.inboundChannelScope,
		DestinationID: destinationID,
		PrincipalID:   principalID,
		FileName:      fileName,
		MIMEType:      mimeType,
		Data:          data,
		Send: func(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, body []byte, name, mime string) error {
			return send(ctx, principal, channel, targets, body, name, mime)
		},
	}, true
}
