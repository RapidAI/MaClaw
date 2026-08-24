package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/swarm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostDocumentGenerateProviderID     = "core-docgenerate"
	reviewedHostDocumentGenerateImplementation = "local"
	reviewedHostDocumentGenerateAdapterName    = "host_document_generate_file"
	reviewedHostDocumentGenerateMaxBytes       = 180 * 1024
)

type reviewedHostGeneratedDocument struct {
	Payload  coretool.ArtifactPayload
	FileName string
	MIMEType string
	Data     []byte
}

type reviewedHostDocumentGenerator interface {
	GenerateReviewedHostDocument(ctx context.Context, principal Principal, content string) (string, error)
}

func reviewedHostDocumentGenerateInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"content"},
		"additionalProperties": false,
	}
}

func reviewedHostDocumentGenerateContractDigest() string {
	return coretool.SchemaDigest([]byte("document.generate.file:v1:host-docgenerate-pdf"))
}

// ProjectReviewedHostDocumentGenerateProvider projects the host-owned PDF
// renderer. It is not a Skill/MCP discovery entry and must not import GUI
// generate_pdf / office. The closed schema accepts content only. Path,
// channel, destination, and file_name stay out. This is not a send.
func ProjectReviewedHostDocumentGenerateProvider(generator reviewedHostDocumentGenerator) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if generator == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host document generator is unavailable")
	}
	parameters := reviewedHostDocumentGenerateInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host document generate schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostDocumentGenerateContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-docgenerate-content-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostDocumentGenerateAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostDocumentGenerateProviderID,
			ImplementationID: reviewedHostDocumentGenerateImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityDocumentGenerate,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF},
			Quality:    1,
		}},
		Produces: []coretool.ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}},
		Effects:  []coretool.EffectClass{coretool.EffectLocalMutation},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostDocumentGenerate(generator)}, nil
}

func AttachReviewedHostDocumentGenerateProvider(catalog DynamicSemanticCatalog, generator reviewedHostDocumentGenerator) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostDocumentGenerateProvider(generator)
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

func executeReviewedHostDocumentGenerate(generator reviewedHostDocumentGenerator) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if generator == nil {
			return "", fmt.Errorf("host_document_generate_unavailable")
		}
		content, err := reviewedHostDocumentGenerateArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return generator.GenerateReviewedHostDocument(ctx, principal, content)
	}
}

func reviewedHostDocumentGenerateArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("host_document_generate_arguments_rejected")
	}
	raw, ok := args["content"]
	if !ok {
		return "", fmt.Errorf("host_document_generate_arguments_rejected")
	}
	content, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("host_document_generate_arguments_rejected")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("host_document_generate_content_required")
	}
	if len(content) > reviewedHostDocumentGenerateMaxBytes {
		return "", fmt.Errorf("host_document_generate_content_too_large")
	}
	if strings.Contains(content, "[file_base64") || strings.Contains(content, "[voice_base64") {
		return "", fmt.Errorf("host_document_generate_delivery_bypass")
	}
	return content, nil
}

func reviewedHostGenerateNeedPresent(needs []coretool.CapabilityNeed) bool {
	for _, need := range needs {
		if need.Capability == CapabilityDocumentGenerate {
			return true
		}
	}
	return false
}

func (c *coreAgentCallbacks) GenerateReviewedHostDocument(ctx context.Context, principal Principal, content string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_document_generate_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_document_generate_principal_mismatch")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("host_document_generate_content_required")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	pdf, err := reviewedHostRenderGeneratedPDF(c.reviewedHostPDFRenderer, content)
	if err != nil || len(pdf) == 0 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("host_document_generate_unavailable")
	}
	if _, _, ok := reviewedHostDeliverableDocument("document.pdf", "application/pdf"); !ok {
		return "", fmt.Errorf("host_document_generate_document_required")
	}
	generated, err := reviewedHostGeneratedDocumentFromPDF(content, pdf)
	if err != nil {
		return "", err
	}
	c.reviewedHostGeneratedDocument = generated
	return "PDF artifact published; deliver it through the current-channel file adapter. This is not a send.", nil
}

func reviewedHostGeneratedDocumentFromPDF(content string, pdf []byte) (*reviewedHostGeneratedDocument, error) {
	scope := coretool.InvocationScope{
		RootTaskID:  "document-generate",
		PlanID:      "generate",
		SessionID:   "host",
		TurnID:      "generate",
		PrincipalID: "host",
	}
	producer := "selection:document-generate"
	payload, err := coretool.NewArtifactPayload(scope, producer, "document", "application/pdf", base64.StdEncoding.EncodeToString(pdf), time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("host_document_generate_artifact_invalid: %w", err)
	}
	_ = content
	return &reviewedHostGeneratedDocument{Payload: payload, FileName: "document.pdf", MIMEType: "application/pdf", Data: pdf}, nil
}

func reviewedHostRenderGeneratedPDF(renderer func(string) ([]byte, error), content string) ([]byte, error) {
	if renderer != nil {
		return renderer(content)
	}
	if err := swarm.ValidatePDFContent(content); err != nil {
		return nil, fmt.Errorf("host_document_generate_content_rejected: %w", err)
	}
	generator := swarm.NewSwarmDocGenerator()
	if generator == nil || !generator.HasFont() {
		return nil, fmt.Errorf("host_document_generate_unavailable")
	}
	pdf, err := generator.GenerateSpecDoc("", "文档", content)
	if err != nil || len(pdf) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("host_document_generate_unavailable")
	}
	return pdf, nil
}
