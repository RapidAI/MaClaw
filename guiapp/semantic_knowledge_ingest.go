package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedKnowledgeIngestAdapter        = "semantic_ingest_trusted_knowledge"
	semanticTrustedKnowledgeIngestImplementation = "trusted-knowledge-ingest-v1"
	semanticTrustedKnowledgeIngestMaxRunes       = 50000
	semanticTrustedKnowledgeIngestTextTimeout    = 10 * time.Second
	semanticTrustedKnowledgeIngestURLTimeout     = 30 * time.Second
	semanticTrustedKnowledgeIngestPathTimeout    = 2 * time.Minute
)

func semanticUnpublishedLegacyKnowledgeIngestProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityKnowledgeIngestLocal {
			return true
		}
	}
	return false
}

func semanticTrustedKnowledgeIngestDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedKnowledgeIngestAdapter,
			"description": "Save exactly one source into the current principal's knowledge store. Pass text to save the note itself, url to save a public page, or path to import an existing file or directory in the bound workspace. Do not combine those fields.",
			"parameters":  semanticTrustedKnowledgeIngestInvocationSchema(),
		},
	}
}

func semanticTrustedKnowledgeIngestInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string", "description": "Note body to save. Mutually exclusive with url and path."},
			"url":  map[string]interface{}{"type": "string", "description": "Public page to save. Mutually exclusive with text and path."},
			"path": map[string]interface{}{"type": "string", "description": "Existing workspace file or directory to import. Mutually exclusive with text and url."},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedKnowledgeIngestArgsAllowed(args map[string]interface{}) (text, url, path string, err error) {
	if len(args) > 3 {
		return "", "", "", fmt.Errorf("trusted_knowledge_ingest_arguments_rejected")
	}
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", "", fmt.Errorf("trusted_knowledge_ingest_arguments_rejected")
		}
		switch key {
		case "text":
			text = strings.TrimSpace(value)
		case "url":
			url = strings.TrimSpace(value)
		case "path":
			path = strings.TrimSpace(value)
		default:
			return "", "", "", fmt.Errorf("trusted_knowledge_ingest_arguments_rejected")
		}
	}
	if !semanticTrustedKnowledgeIngestExclusive(text, url, path) {
		return "", "", "", fmt.Errorf("trusted_knowledge_ingest_text_xor_url_xor_path_required")
	}
	return text, url, path, nil
}

// semanticKnowledgeIngestExclusiveCanonical rejects a combined source before
// the one-shot grant is admitted. text plus path is the natural "save this
// note under this name" call, but path means import an existing file, so
// both sources together are ambiguous. Production 2026-09-26: that rejection
// ran inside the adapter, retired knowledge_save_text, and the corrected
// text-only retry was told the tool was gone.
func semanticKnowledgeIngestExclusiveCanonical(canonicalJSON []byte) error {
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalJSON, &args); err != nil || args == nil {
		return semanticKnowledgeIngestExclusiveParameterError()
	}
	if _, _, _, err := semanticTrustedKnowledgeIngestArgsAllowed(args); err != nil {
		if err.Error() != "trusted_knowledge_ingest_text_xor_url_xor_path_required" {
			return err
		}
		return semanticKnowledgeIngestExclusiveParameterError()
	}
	return nil
}

func semanticKnowledgeIngestExclusiveParameterError() error {
	// Not parameter_schema_invalid: that guidance tells the model to match the
	// rendered schema, and the schema still lists text, url, and path together.
	// The exclusive rule is enforced here, before the grant is spent.
	return &semanticCanonicalDetailedRejection{
		text: "[system rejected] trusted_knowledge_ingest_text_xor_url_xor_path_required: pass exactly one of text, url, or path. path imports an existing workspace file and is not a title. The call was refused before execution, so the tool remains available; call it again with only one of those fields.",
	}
}

func semanticTrustedKnowledgeIngestExclusive(text, url, path string) bool {
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

func (h *IMMessageHandler) ingestTrustedKnowledge(principalID, text, url, path string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_knowledge_ingest_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_knowledge_ingest_principal_required")
	}
	text, url, path = strings.TrimSpace(text), strings.TrimSpace(url), strings.TrimSpace(path)
	if !semanticTrustedKnowledgeIngestExclusive(text, url, path) {
		return "", fmt.Errorf("trusted_knowledge_ingest_text_xor_url_xor_path_required")
	}
	if h.semanticTrustedKnowledgeIngest != nil {
		return h.semanticTrustedKnowledgeIngest(principalID, text, url, path)
	}
	if h.app == nil {
		return "", fmt.Errorf("trusted_knowledge_ingest_unavailable")
	}
	store, err := h.app.openKnowledgeStore()
	if err != nil {
		return "", fmt.Errorf("trusted_knowledge_ingest_unavailable")
	}
	defer store.Close()
	ctx, cancel := trustedKnowledgeIngestContext(h.app.knowledgeContext(), url, path)
	defer cancel()
	if path != "" {
		return h.ingestTrustedKnowledgePath(ctx, store, principalID, path)
	}
	storeOwner, _, _ := knowledgeOwnerScope(principalID)
	if url != "" {
		source, err := store.SaveURL(ctx, knowledge.URLSaveRequest{
			URL:     url,
			OwnerID: storeOwner,
		})
		if err != nil {
			return "", err
		}
		return semanticTrustedKnowledgeIngestProjection("URL", source), nil
	}
	if utf8.RuneCountInString(text) > semanticTrustedKnowledgeIngestMaxRunes {
		return "", fmt.Errorf("trusted_knowledge_ingest_text_too_large")
	}
	source, err := store.SaveText(ctx, knowledge.TextSaveRequest{
		Text:    text,
		OwnerID: storeOwner,
	})
	if err != nil {
		return "", err
	}
	return semanticTrustedKnowledgeIngestProjection("Text", source), nil
}

func (h *IMMessageHandler) ingestTrustedKnowledgePath(ctx context.Context, store *knowledge.SQLiteStore, principalID, path string) (string, error) {
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	absPath, err := trustedKnowledgeIngestResolvePath(workspace, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	storeOwner, _, _ := knowledgeOwnerScope(principalID)
	req := knowledge.DirectoryImportRequest{
		OwnerID:   storeOwner,
		Recursive: true,
	}
	display := trustedKnowledgeIngestDisplayPath(workspace, absPath, path)
	if info.IsDir() {
		req.RootPath = absPath
		result, err := store.ImportDirectory(ctx, req)
		if err != nil {
			return "", err
		}
		return semanticTrustedKnowledgeImportProjection("Directory", display, result), nil
	}
	req.RootPath = workspace
	result, err := store.ImportFiles(ctx, req, []string{absPath})
	if err != nil {
		return "", err
	}
	return semanticTrustedKnowledgeImportProjection("File", display, result), nil
}

func trustedKnowledgeIngestContext(parent context.Context, url, path string) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if _, hasDeadline := parent.Deadline(); hasDeadline {
		return parent, func() {}
	}
	timeout := semanticTrustedKnowledgeIngestTextTimeout
	if url != "" {
		timeout = semanticTrustedKnowledgeIngestURLTimeout
	}
	if path != "" {
		timeout = semanticTrustedKnowledgeIngestPathTimeout
	}
	return context.WithTimeout(parent, timeout)
}

func trustedKnowledgeIngestResolvePath(workspace, path string) (string, error) {
	abs, _, kind := resolvePathInsideWorkspace(workspace, path)
	switch kind {
	case trustedPathOK:
		return abs, nil
	case trustedPathNoWorkspace:
		return "", fmt.Errorf("trusted_knowledge_ingest_path_unavailable")
	default:
		return "", fmt.Errorf("trusted_knowledge_ingest_path_rejected")
	}
}

func trustedKnowledgeIngestDisplayPath(workspace, absPath, raw string) string {
	return trustedRelDisplayPath(workspace, absPath, raw)
}

func semanticTrustedKnowledgeIngestProjection(kind string, source knowledge.Source) string {
	result := kind + " saved to knowledge base."
	if id := strings.TrimSpace(source.ID); id != "" {
		result += " Source ID: " + id
	}
	if title := strings.TrimSpace(source.Title); title != "" {
		result += ", Title: " + title
	}
	return result
}

func semanticTrustedKnowledgeImportProjection(kind, relPath string, result knowledge.DirectoryImportResult) string {
	return fmt.Sprintf("%s imported to knowledge base (%s). Batch ID: %s, total=%d, imported=%d, skipped=%d, duplicates=%d, failed=%d",
		kind, relPath, strings.TrimSpace(result.BatchID), result.TotalFiles, result.ImportedFiles, result.SkippedFiles, result.DuplicateFiles, result.FailedFiles)
}

func semanticTrustedKnowledgeIngestResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_knowledge_ingest_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_knowledge_ingest_empty")
	}
	return text, nil
}
