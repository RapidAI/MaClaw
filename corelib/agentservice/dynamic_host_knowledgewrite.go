package agentservice

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostKnowledgeWriteProviderID     = "core-knowledge-ingest"
	reviewedHostKnowledgeWriteImplementation = "local"
	reviewedHostKnowledgeWriteAdapterName    = "host_knowledge_ingest_local"
	reviewedHostKnowledgeIngestMaxRunes      = 50000
)

type reviewedHostKnowledgeIngester interface {
	IngestReviewedHostKnowledge(ctx context.Context, principal Principal, text, url, path string) (string, error)
}

func reviewedHostKnowledgeWriteInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
			"url":  map[string]interface{}{"type": "string"},
			"path": map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostKnowledgeWriteContractDigest() string {
	return coretool.SchemaDigest([]byte("knowledge.ingest.local:v2:host-knowledge-ingest"))
}

func reviewedHostKnowledgeIngestExclusive(text, url, path string) bool {
	n := 0
	if text != "" {
		n++
	}
	if url != "" {
		n++
	}
	if path != "" {
		n++
	}
	return n == 1
}

// ProjectReviewedHostKnowledgeWriteProvider projects the host-owned local
// knowledge-store ingest. It is not a Skill/MCP discovery entry and must not
// import GUI knowledge_save_* / knowledge_import_*. The closed schema accepts
// text XOR url XOR path; field presence decides, not user keywords. File vs
// directory import is decided by the filesystem type. Channel, destination,
// file_path, query, save_path, and admin fields are rejected. This is not
// knowledge.read.local, fs.write.local, or artifact.acquire.remote. The host
// process observes SaveText/SaveURL/Import*, so the handler result is the
// local completion receipt.
func ProjectReviewedHostKnowledgeWriteProvider(ingester reviewedHostKnowledgeIngester) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if ingester == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host knowledge ingester is unavailable")
	}
	parameters := reviewedHostKnowledgeWriteInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host knowledge ingest schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostKnowledgeWriteContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-knowledge-ingest-text-xor-url-xor-path-v2", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostKnowledgeWriteAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostKnowledgeWriteProviderID,
			ImplementationID: reviewedHostKnowledgeWriteImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityKnowledgeWrite,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostKnowledgeWrite(ingester)}, nil
}

func AttachReviewedHostKnowledgeWriteProvider(catalog DynamicSemanticCatalog, ingester reviewedHostKnowledgeIngester) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostKnowledgeWriteProvider(ingester)
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

func executeReviewedHostKnowledgeWrite(ingester reviewedHostKnowledgeIngester) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if ingester == nil {
			return "", fmt.Errorf("host_knowledge_ingest_unavailable")
		}
		if len(args) > 3 {
			return "", fmt.Errorf("host_knowledge_ingest_arguments_rejected")
		}
		text, url, path := "", "", ""
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_knowledge_ingest_arguments_rejected")
			}
			switch key {
			case "text":
				text = value
			case "url":
				url = value
			case "path":
				path = value
			default:
				return "", fmt.Errorf("host_knowledge_ingest_arguments_rejected")
			}
		}
		text, url, path = strings.TrimSpace(text), strings.TrimSpace(url), strings.TrimSpace(path)
		if !reviewedHostKnowledgeIngestExclusive(text, url, path) {
			return "", fmt.Errorf("host_knowledge_ingest_text_xor_url_xor_path_required")
		}
		return ingester.IngestReviewedHostKnowledge(ctx, principal, text, url, path)
	}
}

func (c *coreAgentCallbacks) IngestReviewedHostKnowledge(ctx context.Context, principal Principal, text, url, path string) (string, error) {
	if c == nil || c.knowledgeStore == nil {
		return "", fmt.Errorf("host_knowledge_ingest_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_knowledge_ingest_principal_mismatch")
	}
	text, url, path = strings.TrimSpace(text), strings.TrimSpace(url), strings.TrimSpace(path)
	if !reviewedHostKnowledgeIngestExclusive(text, url, path) {
		return "", fmt.Errorf("host_knowledge_ingest_text_xor_url_xor_path_required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		timeout := 10 * time.Second
		if url != "" {
			timeout = 30 * time.Second
		}
		if path != "" {
			timeout = 2 * time.Minute
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if path != "" {
		return c.ingestReviewedHostKnowledgePath(ctx, principal, path)
	}
	if url != "" {
		source, err := c.knowledgeStore.SaveURL(ctx, knowledge.URLSaveRequest{
			URL:      url,
			OwnerID:  principal.UserID,
			TenantID: principal.TenantID,
		})
		if err != nil {
			return "", err
		}
		return reviewedHostKnowledgeIngestProjection("URL", source), nil
	}
	if utf8.RuneCountInString(text) > reviewedHostKnowledgeIngestMaxRunes {
		return "", fmt.Errorf("host_knowledge_ingest_text_too_large")
	}
	source, err := c.knowledgeStore.SaveText(ctx, knowledge.TextSaveRequest{
		Text:     text,
		OwnerID:  principal.UserID,
		TenantID: principal.TenantID,
	})
	if err != nil {
		return "", err
	}
	return reviewedHostKnowledgeIngestProjection("Text", source), nil
}

func (c *coreAgentCallbacks) ingestReviewedHostKnowledgePath(ctx context.Context, principal Principal, path string) (string, error) {
	if strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_knowledge_ingest_path_unavailable")
	}
	absPath, err := c.resolveWorkspacePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	req := knowledge.DirectoryImportRequest{
		OwnerID:          principal.UserID,
		TenantID:         principal.TenantID,
		Recursive:        true,
		OfficeReadConfig: officeReadConfigPtrFromAppConfig(c.appCfg),
	}
	display := reviewedHostWorkspaceRelative(c.workspace, absPath, path)
	if info.IsDir() {
		req.RootPath = absPath
		result, err := c.knowledgeStore.ImportDirectory(ctx, req)
		if err != nil {
			return "", err
		}
		return reviewedHostKnowledgeImportProjection("Directory", display, result), nil
	}
	root, err := c.resolveKnowledgeWorkspaceRoot()
	if err != nil {
		return "", err
	}
	req.RootPath = root
	result, err := c.knowledgeStore.ImportFiles(ctx, req, []string{absPath})
	if err != nil {
		return "", err
	}
	return reviewedHostKnowledgeImportProjection("File", display, result), nil
}

func reviewedHostKnowledgeIngestProjection(kind string, source knowledge.Source) string {
	result := kind + " saved to knowledge base."
	if id := strings.TrimSpace(source.ID); id != "" {
		result += " Source ID: " + id
	}
	if title := strings.TrimSpace(source.Title); title != "" {
		result += ", Title: " + title
	}
	return result
}

func reviewedHostKnowledgeImportProjection(kind, relPath string, result knowledge.DirectoryImportResult) string {
	return fmt.Sprintf("%s imported to knowledge base (%s). Batch ID: %s, total=%d, imported=%d, skipped=%d, duplicates=%d, failed=%d",
		kind, relPath, strings.TrimSpace(result.BatchID), result.TotalFiles, result.ImportedFiles, result.SkippedFiles, result.DuplicateFiles, result.FailedFiles)
}
