package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedFileWriteAdapter        = "semantic_write_trusted_file"
	semanticTrustedFileWriteImplementation = "trusted-fs-write-v1"
)

func semanticUnpublishedLegacyFileWriteProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityFSWriteLocal {
			return true
		}
	}
	return false
}

func semanticTrustedFileWriteDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedFileWriteAdapter,
			"description": "Change a workspace file. Give content to write the whole file, with optional mode overwrite or append. Give old_string and new_string instead to replace one exact passage in an existing file; old_string must match exactly once.",
			"parameters":  semanticTrustedFileWriteInvocationSchema(),
		},
	}
}

func semanticTrustedFileWriteInvocationSchema() map[string]interface{} {
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

// semanticFileWriteInvocationArgs washes model-supplied file-write arguments
// before canonical schema validation, the same boundary role as
// semanticOfficeWriteInvocationArgs. Models trained on legacy tool soup
// habitually write file_path for path and text for content; strict admission
// burns the one-shot grant on those aliases. Aliases fold only when the
// canonical key is absent — a real conflict (both path and file_path) passes
// through unchanged so admission still fails closed. Null values carry no
// intent and are dropped.
func semanticFileWriteInvocationArgs(argsJSON string) string {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON
	}
	changed := false
	for key, raw := range parsed {
		if raw == nil {
			delete(parsed, key)
			changed = true
		}
	}
	fold := func(alias, canonical string) {
		if _, ok := parsed[canonical]; ok {
			return
		}
		if value, ok := parsed[alias]; ok {
			delete(parsed, alias)
			parsed[canonical] = value
			changed = true
		}
	}
	fold("file_path", "path")
	fold("text", "content")
	if !changed {
		return argsJSON
	}
	body, err := json.Marshal(parsed)
	if err != nil {
		return argsJSON
	}
	return string(body)
}

// trustedFileWriteRequest is the decoded shape of one managed mutation. edit
// distinguishes the two outcomes; the caller never has to re-derive it from
// which fields happen to be empty.
type trustedFileWriteRequest struct {
	path      string
	content   string
	mode      string
	oldString string
	newString string
	edit      bool
}

func semanticTrustedFileWriteArgsAllowed(args map[string]interface{}) (trustedFileWriteRequest, error) {
	if len(args) > 5 {
		return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_write_arguments_rejected")
	}
	var req trustedFileWriteRequest
	hasPath, hasContent, hasMode, hasOld, hasNew := false, false, false, false, false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_write_arguments_rejected")
		}
		switch key {
		case "path":
			req.path, hasPath = value, true
		case "content":
			req.content, hasContent = value, true
		case "mode":
			req.mode, hasMode = value, true
		case "old_string":
			req.oldString, hasOld = value, true
		case "new_string":
			req.newString, hasNew = value, true
		default:
			return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_write_arguments_rejected")
		}
	}
	if !hasPath {
		return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_write_arguments_rejected")
	}
	// Only path and mode are trimmed. Leading and trailing whitespace is
	// meaningful in the text being matched and inserted, and trimming it would
	// silently edit something other than what was asked for.
	req.path, req.mode = strings.TrimSpace(req.path), strings.TrimSpace(req.mode)
	if req.path == "" {
		return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_write_path_required")
	}
	req.edit = hasOld || hasNew
	if req.edit {
		if !hasOld || !hasNew {
			return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_edit_pair_required")
		}
		// Mixing the two shapes is refused rather than resolved by precedence:
		// a request carrying both says two different things about the file, and
		// picking one silently discards the other.
		if hasContent || hasMode {
			return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_edit_conflicting_fields")
		}
		if req.oldString == "" {
			return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_edit_old_string_required")
		}
		return req, nil
	}
	if !hasContent {
		return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_write_arguments_rejected")
	}
	if _, err := tool.NormalizeWriteModeKind(req.mode); err != nil {
		return trustedFileWriteRequest{}, fmt.Errorf("trusted_file_write_mode_rejected")
	}
	return req, nil
}

func (h *IMMessageHandler) writeTrustedFile(principalID, path, content, mode string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_file_write_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_file_write_principal_required")
	}
	path, mode = strings.TrimSpace(path), strings.TrimSpace(mode)
	if path == "" {
		return "", fmt.Errorf("trusted_file_write_path_required")
	}
	resolvedMode, err := tool.NormalizeWriteModeKind(mode)
	if err != nil {
		return "", fmt.Errorf("trusted_file_write_mode_rejected")
	}
	if len(content) > writeFileMaxSize {
		return "", fmt.Errorf("trusted_file_write_content_too_large")
	}
	if h.semanticTrustedFileWrite != nil {
		return h.semanticTrustedFileWrite(principalID, path, content, mode)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	absPath, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("trusted_file_write_path_is_directory")
	}
	size, err := tool.WriteTextFile(absPath, content, string(resolvedMode))
	if err != nil {
		return "", err
	}
	display := trustedFileWriteDisplayPath(workspace, absPath, path)
	if resolvedMode == tool.WriteModeAppend {
		return fmt.Sprintf("Appended to %s (%d bytes total)", display, size), nil
	}
	return fmt.Sprintf("Written to %s (%d bytes)", display, size), nil
}

// editTrustedFile replaces one exact passage in an existing workspace file.
//
// It is deliberately separate from writeTrustedFile: a surgical edit and a
// whole-file write have different preconditions (the file must already exist)
// and different failure modes, and folding them into one function would mean a
// mode argument deciding which half of the body runs.
//
// The match must be unique. The legacy tool offers a replace_all switch, but a
// managed turn gets no such knob: an old_string that appears several times is
// ambiguous about which occurrence was meant, and guessing is how an edit lands
// in the wrong place. The model is told to be more specific instead.
func (h *IMMessageHandler) editTrustedFile(principalID, path, oldString, newString string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_file_write_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_file_write_principal_required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("trusted_file_write_path_required")
	}
	if oldString == "" {
		return "", fmt.Errorf("trusted_file_edit_old_string_required")
	}
	if len(newString) > writeFileMaxSize {
		return "", fmt.Errorf("trusted_file_write_content_too_large")
	}
	if h.semanticTrustedFileEdit != nil {
		return h.semanticTrustedFileEdit(principalID, path, oldString, newString)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	absPath, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("trusted_file_edit_not_found")
	}
	if info.IsDir() {
		return "", fmt.Errorf("trusted_file_write_path_is_directory")
	}
	if info.Size() > int64(writeFileMaxSize) {
		return "", fmt.Errorf("trusted_file_edit_content_too_large")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	original := string(data)
	switch strings.Count(original, oldString) {
	case 0:
		return "", fmt.Errorf("trusted_file_edit_no_match")
	case 1:
	default:
		return "", fmt.Errorf("trusted_file_edit_ambiguous_match")
	}
	updated := strings.Replace(original, oldString, newString, 1)
	if len(updated) > writeFileMaxSize {
		return "", fmt.Errorf("trusted_file_write_content_too_large")
	}
	size, err := tool.WriteTextFile(absPath, updated, string(tool.WriteModeOverwrite))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited %s (%d bytes)", trustedFileWriteDisplayPath(workspace, absPath, path), size), nil
}

func trustedFileWriteResolvePath(workspace, path string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	path = strings.TrimSpace(path)
	if workspace == "" {
		return "", fmt.Errorf("trusted_file_write_path_unavailable")
	}
	if path == "" {
		return "", fmt.Errorf("trusted_file_write_path_rejected")
	}
	base, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("trusted_file_write_path_unavailable")
	}
	candidate := path
	if !filepath.IsAbs(path) {
		candidate = filepath.Join(base, path)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("trusted_file_write_path_rejected")
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("trusted_file_write_path_rejected")
	}
	return abs, nil
}

func trustedFileWriteDisplayPath(workspace, absPath, raw string) string {
	if rel, err := filepath.Rel(workspace, absPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return strings.TrimSpace(raw)
}

func semanticTrustedFileWriteResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_file_write_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_file_write_empty")
	}
	return text, nil
}
