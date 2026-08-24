package agentservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostFileWriteProviderID     = "core-filewrite"
	reviewedHostFileWriteImplementation = "local"
	reviewedHostFileWriteAdapterName    = "host_fs_write_local"
)

type reviewedHostFileWriter interface {
	WriteReviewedHostFile(ctx context.Context, principal Principal, path, content, mode string) (string, error)
	EditReviewedHostFile(ctx context.Context, principal Principal, path, oldString, newString string) (string, error)
}

func reviewedHostFileWriteInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":    map[string]interface{}{"type": "string"},
			"content": map[string]interface{}{"type": "string"},
			"mode":    map[string]interface{}{"type": "string"},
			// Replacing one exact passage is an outcome whole-file content
			// cannot express. Which pair is present decides the outcome, so the
			// schema needs no action or operation field, and it carries none of
			// the legacy edit tool's other knobs.
			"old_string": map[string]interface{}{"type": "string"},
			"new_string": map[string]interface{}{"type": "string"},
		},
		// content is required for a whole-file write but meaningless for a
		// replacement, so the pairing rule lives in the argument check rather
		// than in a schema that cannot express "one of these two shapes".
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func reviewedHostFileWriteContractDigest() string {
	return coretool.SchemaDigest([]byte("fs.write.local:v2:host-filewrite"))
}

// ProjectReviewedHostFileWriteProvider projects the host-owned workspace
// filesystem mutation. It is not a Skill/MCP discovery entry and must not
// import GUI write_file / edit_file. The closed schema accepts path plus
// either content with optional mode (overwrite/append) for a whole-file write,
// or old_string with new_string to replace one exact passage. The two shapes
// are mutually exclusive and old_string must match exactly once. Channel,
// destination, group_name, file_path, query, save_path, replace_all, and
// line numbers are rejected. This is not fs.read.local,
// knowledge.ingest.local, or document.generate.file. The host process
// observes the write, so the handler result is the local completion receipt.
func ProjectReviewedHostFileWriteProvider(writer reviewedHostFileWriter) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if writer == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host file writer is unavailable")
	}
	parameters := reviewedHostFileWriteInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host file write schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostFileWriteContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-filewrite-path-content-mode-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostFileWriteAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostFileWriteProviderID,
			ImplementationID: reviewedHostFileWriteImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityFileWrite,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostFileWrite(writer)}, nil
}

func AttachReviewedHostFileWriteProvider(catalog DynamicSemanticCatalog, writer reviewedHostFileWriter) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostFileWriteProvider(writer)
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

func executeReviewedHostFileWrite(writer reviewedHostFileWriter) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if writer == nil {
			return "", fmt.Errorf("host_file_write_unavailable")
		}
		if len(args) > 5 {
			return "", fmt.Errorf("host_file_write_arguments_rejected")
		}
		path, content, mode, oldString, newString := "", "", "", "", ""
		hasPath, hasContent, hasMode, hasOld, hasNew := false, false, false, false, false
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_file_write_arguments_rejected")
			}
			switch key {
			case "path":
				path, hasPath = value, true
			case "content":
				content, hasContent = value, true
			case "mode":
				mode, hasMode = value, true
			case "old_string":
				oldString, hasOld = value, true
			case "new_string":
				newString, hasNew = value, true
			default:
				return "", fmt.Errorf("host_file_write_arguments_rejected")
			}
		}
		if !hasPath {
			return "", fmt.Errorf("host_file_write_arguments_rejected")
		}
		// Only path and mode are trimmed. Leading and trailing whitespace is
		// meaningful in the text being matched and inserted.
		if hasOld || hasNew {
			if !hasOld || !hasNew {
				return "", fmt.Errorf("host_file_edit_pair_required")
			}
			// Mixing the two shapes is refused rather than resolved by
			// precedence: a request carrying both says two different things
			// about the file, and picking one silently discards the other.
			if hasContent || hasMode {
				return "", fmt.Errorf("host_file_edit_conflicting_fields")
			}
			return writer.EditReviewedHostFile(ctx, principal, strings.TrimSpace(path), oldString, newString)
		}
		if !hasContent {
			return "", fmt.Errorf("host_file_write_arguments_rejected")
		}
		return writer.WriteReviewedHostFile(ctx, principal, strings.TrimSpace(path), content, strings.TrimSpace(mode))
	}
}

func (c *coreAgentCallbacks) WriteReviewedHostFile(ctx context.Context, principal Principal, path, content, mode string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_file_write_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_file_write_principal_mismatch")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("host_file_write_path_required")
	}
	if len(content) > srvWriteFileMaxSize {
		return "", fmt.Errorf("host_file_write_content_too_large")
	}
	resolvedMode, err := coretool.NormalizeWriteModeKind(mode)
	if err != nil {
		return "", fmt.Errorf("host_file_write_mode_rejected")
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
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("host_file_write_path_is_directory")
	}
	size, err := coretool.WriteTextFile(absPath, content, string(resolvedMode))
	if err != nil {
		return "", err
	}
	display := reviewedHostWorkspaceRelative(c.workspace, absPath, path)
	if resolvedMode == coretool.WriteModeAppend {
		return fmt.Sprintf("Appended to %s (%d bytes total)", display, size), nil
	}
	return fmt.Sprintf("Written to %s (%d bytes)", display, size), nil
}

// EditReviewedHostFile replaces one exact passage in an existing workspace
// file.
//
// It is deliberately separate from WriteReviewedHostFile: a surgical edit and a
// whole-file write have different preconditions (the file must already exist)
// and different failure modes, and folding them into one function would mean a
// mode argument deciding which half of the body runs.
//
// The match must be unique. The legacy tool offers a replace_all switch, but a
// managed turn gets no such knob: an old_string that appears several times is
// ambiguous about which occurrence was meant, and guessing is how an edit lands
// in the wrong place.
func (c *coreAgentCallbacks) EditReviewedHostFile(ctx context.Context, principal Principal, path, oldString, newString string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_file_write_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_file_write_principal_mismatch")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("host_file_write_path_required")
	}
	if oldString == "" {
		return "", fmt.Errorf("host_file_edit_old_string_required")
	}
	if len(newString) > srvWriteFileMaxSize {
		return "", fmt.Errorf("host_file_write_content_too_large")
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
		return "", fmt.Errorf("host_file_edit_not_found")
	}
	if info.IsDir() {
		return "", fmt.Errorf("host_file_write_path_is_directory")
	}
	if info.Size() > int64(srvWriteFileMaxSize) {
		return "", fmt.Errorf("host_file_edit_content_too_large")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	original := string(data)
	switch strings.Count(original, oldString) {
	case 0:
		return "", fmt.Errorf("host_file_edit_no_match")
	case 1:
	default:
		return "", fmt.Errorf("host_file_edit_ambiguous_match")
	}
	updated := strings.Replace(original, oldString, newString, 1)
	if len(updated) > srvWriteFileMaxSize {
		return "", fmt.Errorf("host_file_write_content_too_large")
	}
	size, err := coretool.WriteTextFile(absPath, updated, string(coretool.WriteModeOverwrite))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited %s (%d bytes)", reviewedHostWorkspaceRelative(c.workspace, absPath, path), size), nil
}

func reviewedHostWorkspaceRelative(workspace, absPath, fallback string) string {
	base := strings.TrimSpace(workspace)
	if base == "" {
		return filepath.ToSlash(strings.TrimSpace(fallback))
	}
	rel, err := filepath.Rel(base, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(strings.TrimSpace(fallback))
	}
	return filepath.ToSlash(rel)
}
