package agentservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostDocumentReadProviderID     = "core-docread"
	reviewedHostDocumentReadImplementation = "local"
	reviewedHostDocumentReadAdapterName    = "host_document_read_local"
	QualifierDocumentFormat                = "format"
	DocumentFormatPDF                      = agent.DocumentAttachmentFormatPDF
	DocumentFormatWord                     = agent.DocumentAttachmentFormatWord
	DocumentFormatSpreadsheet              = agent.DocumentAttachmentFormatSpreadsheet
	DocumentFormatPresentation             = agent.DocumentAttachmentFormatPresentation
	DocumentFormatText                     = agent.DocumentAttachmentFormatText
)

type reviewedHostDocumentInput struct {
	Payload  coretool.ArtifactPayload
	Format   string
	Suffix   string
	FileName string
}

func reviewedHostDocumentReadInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"max_chars":    map[string]interface{}{"type": "integer"},
			"offset":       map[string]interface{}{"type": "integer"},
			"line_numbers": map[string]interface{}{"type": "boolean"},
			"sheet":        map[string]interface{}{"type": "string"},
			"range":        map[string]interface{}{"type": "string"},
			"max_rows":     map[string]interface{}{"type": "integer"},
			"max_slides":   map[string]interface{}{"type": "integer"},
			"slide_offset": map[string]interface{}{"type": "integer"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostDocumentReadContractDigest() string {
	return coretool.SchemaDigest([]byte("document.read.local:v1:host-docread"))
}

// ProjectReviewedHostDocumentReadProvider projects the host-owned trusted
// attachment reader. It is not a Skill/MCP discovery entry and must not
// import GUI office / read_file. The closed schema accepts only paging
// fields; path, file_path, artifact ID, channel, and destination are
// rejected. The bytes come from a host-published ArtifactRef, never from
// model text.
func ProjectReviewedHostDocumentReadProvider(reader reviewedHostDocumentReader) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if reader == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host document reader is unavailable")
	}
	parameters := reviewedHostDocumentReadInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host document read schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostDocumentReadContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-docread-paging-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostDocumentReadAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostDocumentReadProviderID,
			ImplementationID: reviewedHostDocumentReadImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{
			{Capability: CapabilityDocumentRead, Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF}, Quality: 1},
			{Capability: CapabilityDocumentRead, Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatWord}, Quality: 1},
			{Capability: CapabilityDocumentRead, Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatSpreadsheet}, Quality: 1},
			{Capability: CapabilityDocumentRead, Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPresentation}, Quality: 1},
			{Capability: CapabilityDocumentRead, Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatText}, Quality: 1},
		},
		Consumes: []coretool.ArtifactContract{{Kind: "document", Required: true}},
		Effects:  []coretool.EffectClass{coretool.EffectReadOnly},
		Ready:    true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostDocumentRead(reader)}, nil
}

func AttachReviewedHostDocumentReadProvider(catalog DynamicSemanticCatalog, reader reviewedHostDocumentReader) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostDocumentReadProvider(reader)
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

func executeReviewedHostDocumentRead(reader reviewedHostDocumentReader) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if reader == nil {
			return "", fmt.Errorf("host_document_read_unavailable")
		}
		if err := reviewedHostDocumentReadArgsAllowed(args); err != nil {
			return "", err
		}
		return reader.ReadReviewedHostDocument(ctx, principal, args)
	}
}

func reviewedHostDocumentReadArgsAllowed(args map[string]interface{}) error {
	if len(args) > 8 {
		return fmt.Errorf("host_document_read_arguments_rejected")
	}
	for key, raw := range args {
		switch key {
		case "max_chars", "offset", "max_rows", "max_slides", "slide_offset":
			switch raw.(type) {
			case float64, float32, int, int32, int64, json.Number:
			default:
				return fmt.Errorf("host_document_read_arguments_rejected")
			}
		case "line_numbers":
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("host_document_read_arguments_rejected")
			}
		case "sheet", "range":
			if _, ok := raw.(string); !ok {
				return fmt.Errorf("host_document_read_arguments_rejected")
			}
		default:
			return fmt.Errorf("host_document_read_arguments_rejected")
		}
	}
	return nil
}

func reviewedHostDocumentInputsForTurn(rootTaskID, turnID, principalID string, attachments []agent.MessageAttachment) ([]reviewedHostDocumentInput, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	inputScope := coretool.InvocationScope{
		RootTaskID:  strings.TrimSpace(rootTaskID),
		PlanID:      coretool.TrustedInputPlanID(turnID),
		SessionID:   strings.TrimSpace(principalID),
		TurnID:      strings.TrimSpace(turnID),
		PrincipalID: strings.TrimSpace(principalID),
	}
	attachments = CanonicalizeReviewedHostMessageAttachments(attachments)
	inputs := make([]reviewedHostDocumentInput, 0, len(attachments))
	for index, attachment := range attachments {
		format, mimeType, ok := agent.DocumentAttachmentFormat(attachment.FileName, attachment.MimeType)
		if !ok {
			continue
		}
		raw, err := decodeReviewedHostAttachmentBytes(attachment.Data)
		if err != nil {
			return nil, fmt.Errorf("trusted_document_attachment_content_missing")
		}
		if int64(len(raw)) > agent.MaxOfficeReadFileBytes {
			return nil, fmt.Errorf("trusted_document_attachment_too_large")
		}
		encoded := base64.StdEncoding.EncodeToString(raw)
		sourceID := coretool.TrustedAttachmentSourceID(index, attachment.FileName, mimeType, attachment.SourceMediaID)
		producer := "trusted-input:host-attachment:" + coretool.SchemaDigest([]byte(sourceID))[:24]
		payload, err := coretool.NewArtifactPayload(inputScope, producer, "document", mimeType, encoded, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("trusted_document_attachment_invalid: %w", err)
		}
		inputs = append(inputs, reviewedHostDocumentInput{
			Payload:  payload,
			Format:   format,
			Suffix:   agent.DocumentAttachmentTempSuffix(attachment.FileName, format),
			FileName: filepath.Base(strings.TrimSpace(attachment.FileName)),
		})
	}
	return inputs, nil
}

func bindReviewedHostDocumentTurn(needs []coretool.CapabilityNeed, inputs []reviewedHostDocumentInput, inputErr error) ([]coretool.CapabilityNeed, error) {
	if !reviewedHostDocumentNeedPresent(needs) {
		return needs, nil
	}
	if inputErr != nil {
		return nil, inputErr
	}
	return applyReviewedHostDocumentInputs(needs, inputs)
}

func applyReviewedHostDocumentInputs(needs []coretool.CapabilityNeed, inputs []reviewedHostDocumentInput) ([]coretool.CapabilityNeed, error) {
	if len(needs) == 0 {
		return nil, nil
	}
	resolved := append([]coretool.CapabilityNeed(nil), needs...)
	requiresExact := false
	for _, need := range resolved {
		if coretool.IsDocumentRead(need.Capability) || reviewedHostAttachmentDeliverNeed(need) {
			requiresExact = true
			break
		}
	}
	if requiresExact {
		if err := coretool.UniqueTrustedInputCount(len(inputs)); err != nil {
			return nil, err
		}
		for index := range resolved {
			resolved[index] = coretool.BindDocumentReadFormat(resolved[index], inputs[0].Format)
		}
	}
	return resolved, nil
}

func reviewedHostDocumentFacts(inputs []reviewedHostDocumentInput) []coretool.RoutingFact {
	facts := make([]coretool.RoutingFact, 0, len(inputs))
	for _, input := range inputs {
		binding := coretool.ArtifactBindingFromRef(input.Payload.Ref)
		facts = append(facts, coretool.RoutingFact{
			ID:   "trusted-document:" + input.Payload.Ref.ID,
			Kind: "artifact_available",
			Attributes: map[string]string{
				"artifact_id": input.Payload.Ref.ID,
				"kind":        input.Payload.Ref.Kind,
				"mime_type":   input.Payload.Ref.MIMEType,
			},
			Artifact:  &binding,
			Authority: coretool.AuthorityChannel,
		})
	}
	return facts
}

func reviewedHostDocumentNeedPresent(needs []coretool.CapabilityNeed) bool {
	for _, need := range needs {
		if coretool.IsDocumentRead(need.Capability) || reviewedHostAttachmentDeliverNeed(need) {
			return true
		}
	}
	return false
}

func reviewedHostAttachmentDeliverNeed(need coretool.CapabilityNeed) bool {
	return coretool.CurrentChannelDeliverAccepts(need, ArtifactFormatFile, ArtifactFormatImage, ArtifactFormatVoice, "")
}

// ReviewedHostTrustedDocumentMIME reports whether a host attachment is in the
// closed document-read allowlist and returns the canonical MIME.
func ReviewedHostTrustedDocumentMIME(fileName, mimeType string) (string, bool) {
	_, mime, ok := agent.DocumentAttachmentFormat(fileName, mimeType)
	return mime, ok
}

func (c *coreAgentCallbacks) ReadReviewedHostDocument(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
	if c == nil || c.reviewedHostDocument == nil {
		return "", fmt.Errorf("host_document_read_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_document_read_principal_mismatch")
	}
	if err := reviewedHostDocumentReadArgsAllowed(args); err != nil {
		return "", err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	path, cleanup, err := materializeReviewedHostDocument(c.reviewedHostDocument.Payload, c.reviewedHostDocument.Suffix)
	if err != nil {
		return "", err
	}
	defer cleanup()
	toolArgs := map[string]interface{}{"file_path": path}
	for key, value := range args {
		toolArgs[key] = value
	}
	contextTokens := 0
	if c != nil {
		contextTokens = c.llmCfg.EffectiveContextTokens()
	}
	result := agent.ToolReadDocumentWithOfficeReadConfigAndContext(toolArgs, officeReadConfigFromAppConfig(c.appCfg), contextTokens)
	if class, failed := agent.DocumentReadFailure(result); failed {
		return "", fmt.Errorf("host_document_read_failed_%s", class)
	}
	return reviewedHostDocumentReadResultProjection(result), nil
}

func materializeReviewedHostDocument(payload coretool.ArtifactPayload, suffix string) (string, func(), error) {
	bytes, err := base64.StdEncoding.DecodeString(payload.Base64)
	if err != nil || len(bytes) == 0 {
		return "", nil, fmt.Errorf("trusted_document_payload_invalid")
	}
	file, err := os.CreateTemp("", "semantic-document-*"+suffix)
	if err != nil {
		return "", nil, fmt.Errorf("trusted_document_temp_create_failed")
	}
	path := filepath.Clean(file.Name())
	if _, err := file.Write(bytes); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("trusted_document_temp_write_failed")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("trusted_document_temp_close_failed")
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func reviewedHostDocumentReadResultProjection(result string) string {
	lines := strings.Split(result, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# path:") || strings.HasPrefix(trimmed, "# continue:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}
