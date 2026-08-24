package agentservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostFileDeliverProviderID     = "core-filedeliver"
	reviewedHostFileDeliverImplementation = "channel"
	reviewedHostFileDeliverAdapterName    = "host_artifact_deliver_specified"
	reviewedHostFileDeliverMaxBytes       = 20 << 20
)

type reviewedHostFileDeliverer interface {
	PrepareReviewedHostFileDeliver(ctx context.Context, principal Principal, destinationID, path string) (string, error)
}

func reviewedHostFileDeliverInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func reviewedHostFileDeliverContractDigest() string {
	return coretool.SchemaDigest([]byte("artifact.deliver.specified_target:v1:host-filedeliver-document-image-voice"))
}

// ProjectReviewedHostFileDeliverProvider projects a specified-target
// trusted document, image, or voice prepare. It is not a Skill/MCP
// discovery entry and must not import GUI send_file / send_to_im. The
// closed schema accepts path only. Channel, group_name, destination, and
// file_name are rejected. Destinations come only from inbound transport.
// Prepare is not a send. Zip and other files outside the current-channel
// document, image, and voice allowlists stay unpublished.
func ProjectReviewedHostFileDeliverProvider(deliverer reviewedHostFileDeliverer, destinationID string) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if deliverer == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host file deliverer is unavailable")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("trusted_delivery_target_missing")
	}
	parameters := reviewedHostFileDeliverInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host file deliver schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostFileDeliverContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-filedeliver-document-image-voice-v1", contractDigest, invocationDigest, destinationID,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostFileDeliverAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostFileDeliverProviderID,
			ImplementationID: reviewedHostFileDeliverImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityArtifactDeliverSpecified,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
			Quality:    1,
		}, {
			Capability: CapabilityArtifactDeliverSpecified,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage},
			Quality:    1,
		}, {
			Capability: CapabilityArtifactDeliverSpecified,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostFileDeliver(deliverer, destinationID)}, nil
}

func AttachReviewedHostFileDeliverProvider(catalog DynamicSemanticCatalog, deliverer reviewedHostFileDeliverer, destinationID string) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostFileDeliverProvider(deliverer, destinationID)
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

func executeReviewedHostFileDeliver(deliverer reviewedHostFileDeliverer, destinationID string) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if deliverer == nil {
			return "", fmt.Errorf("host_file_deliver_unavailable")
		}
		path, err := reviewedHostFileDeliverArgsAllowed(args)
		if err != nil {
			return "", err
		}
		if !reviewedHostTrustedDestination(destinationID) {
			return "", fmt.Errorf("trusted_delivery_target_missing")
		}
		return deliverer.PrepareReviewedHostFileDeliver(ctx, principal, destinationID, path)
	}
}

func reviewedHostFileDeliverArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("host_file_deliver_arguments_rejected")
	}
	path := ""
	hasPath := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("host_file_deliver_arguments_rejected")
		}
		switch key {
		case "path":
			path, hasPath = value, true
		default:
			return "", fmt.Errorf("host_file_deliver_arguments_rejected")
		}
	}
	path = strings.TrimSpace(path)
	if !hasPath || path == "" {
		return "", fmt.Errorf("host_file_deliver_path_required")
	}
	return path, nil
}

func reviewedHostSpreadsheetFile(path string) (fileName, mimeType string, ok bool) {
	fileName = filepath.Base(strings.TrimSpace(path))
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".xlsx":
		return fileName, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true
	case ".xls":
		return fileName, "application/vnd.ms-excel", true
	case ".csv":
		return fileName, "text/csv", true
	default:
		return "", "", false
	}
}

func reviewedHostDeliverableMedia(fileName, mimeType string) (name, canonicalMIME, kind string, ok bool) {
	if n, m, ok := reviewedHostDeliverableImage(fileName, mimeType); ok {
		return n, m, "image", true
	}
	if n, m, ok := reviewedHostDeliverableVoice(fileName, mimeType); ok {
		return n, m, "voice", true
	}
	if n, m, ok := reviewedHostDeliverableDocument(fileName, mimeType); ok {
		return n, m, "document", true
	}
	return "", "", "", false
}

func reviewedHostDeliverableDocument(fileName, mimeType string) (name, canonicalMIME string, ok bool) {
	name = filepath.Base(strings.TrimSpace(fileName))
	format, canonicalMIME, ok := reviewedHostDocumentFormat(name, mimeType)
	if !ok {
		return reviewedHostSpreadsheetFile(name)
	}
	if name == "" || name == "." {
		name = "document" + reviewedHostDocumentTempSuffix("", format)
	}
	return name, canonicalMIME, true
}

func (c *coreAgentCallbacks) PrepareReviewedHostFileDeliver(ctx context.Context, principal Principal, destinationID, path string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_file_deliver_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_file_deliver_principal_mismatch")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return "", fmt.Errorf("trusted_delivery_target_missing")
	}
	if destinationID != strings.TrimSpace(c.trustedDestinationID) {
		return "", fmt.Errorf("trusted_delivery_target_mismatch")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("host_file_deliver_path_required")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	absPath, err := c.resolveWorkspacePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("host_file_deliver_path_missing")
	}
	if info.IsDir() {
		return "", fmt.Errorf("host_file_deliver_path_is_directory")
	}
	fileName, mimeType, kind, ok := reviewedHostDeliverableMedia(filepath.Base(absPath), "")
	if !ok {
		return "", fmt.Errorf("host_file_deliver_document_required")
	}
	if info.Size() <= 0 || info.Size() > reviewedHostFileDeliverSizeLimit(kind) {
		return "", fmt.Errorf("host_file_deliver_size_rejected")
	}
	if deps, armed := c.reviewedHostFileDeliverFireDeps(destinationID, absPath, fileName, mimeType); armed {
		if err := FireReviewedHostFileDeliver(ctx, deps); err != nil {
			return "", err
		}
		return "", fmt.Errorf("host_file_deliver_unknown")
	}
	display := reviewedHostWorkspaceRelative(c.workspace, absPath, path)
	return reviewedHostFileDeliverPrepareReceipt(kind, destinationID, display), nil
}

func reviewedHostFileDeliverSizeLimit(kind string) int64 {
	switch kind {
	case "image":
		return reviewedHostImageDeliverMaxBytes
	case "voice":
		return ReviewedHostAudioTranscribeMaxBytes
	default:
		return reviewedHostFileDeliverMaxBytes
	}
}

func reviewedHostFileDeliverPrepareReceipt(kind, destinationID, display string) string {
	label := "Document"
	switch kind {
	case "image":
		label = "Image"
	case "voice":
		label = "Voice"
	}
	return label + " prepared for " + destinationID + " (" + display + "). This is not a send."
}

func (c *coreAgentCallbacks) reviewedHostFileDeliverFireDeps(destinationID, absPath, fileName, mimeType string) (reviewedHostFileDeliverFireDeps, bool) {
	if c == nil || c.executor == nil || c.executor.ScheduleDispatchFileSend == nil {
		return reviewedHostFileDeliverFireDeps{}, false
	}
	coordinator := c.executor.reviewedHostScheduleDispatchCoordinator()
	if coordinator == nil {
		return reviewedHostFileDeliverFireDeps{}, false
	}
	if _, ok := reviewedHostScheduleDispatchFireChannel(c.inboundChannelScope); !ok {
		return reviewedHostFileDeliverFireDeps{}, false
	}
	data, err := os.ReadFile(absPath)
	if err != nil || len(data) == 0 {
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
