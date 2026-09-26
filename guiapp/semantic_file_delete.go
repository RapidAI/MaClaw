package guiapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	semanticTrustedFileDeleteAdapter        = "semantic_delete_trusted_file"
	semanticTrustedFileDeleteImplementation = "trusted-fs-delete-v1"
)

func semanticTrustedFileDeleteDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedFileDeleteAdapter,
			"description": "Remove one file inside the bound workspace. Pass path. This deletes the file. It does not rewrite the file and it does not run a shell command.",
			"parameters":  semanticTrustedFileDeleteInvocationSchema(),
		},
	}
}

func semanticTrustedFileDeleteInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

// semanticFileDeleteInvocationArgs folds the legacy file_path alias onto path
// before the closed schema rejects it. A one-shot delete grant must not be
// spent on a field name the model learned from another tool.
func semanticFileDeleteInvocationArgs(argsJSON string) string {
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
	if _, ok := parsed["path"]; !ok {
		if value, ok := parsed["file_path"]; ok {
			delete(parsed, "file_path")
			parsed["path"] = value
			changed = true
		}
	}
	if !changed {
		return argsJSON
	}
	body, err := json.Marshal(parsed)
	if err != nil {
		return argsJSON
	}
	return string(body)
}

func semanticTrustedFileDeleteArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("trusted_file_delete_arguments_rejected")
	}
	raw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("trusted_file_delete_arguments_rejected")
	}
	path, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("trusted_file_delete_arguments_rejected")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("trusted_file_delete_path_required")
	}
	return path, nil
}

func (h *IMMessageHandler) deleteTrustedFile(principalID, path string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_file_delete_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_file_delete_principal_required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("trusted_file_delete_path_required")
	}
	if h.semanticTrustedFileDelete != nil {
		return h.semanticTrustedFileDelete(principalID, path)
	}
	return deleteTrustedWorkspaceFile(trustedPrincipalBoundWorkspace(h, principalID), path)
}

// deleteTrustedWorkspaceFile removes one regular file inside workspace.
// A directory is refused: removing a tree is a different effect from removing
// the file the user named. An ancestor symlink that resolves outside the
// workspace is refused before anything is unlinked.
func deleteTrustedWorkspaceFile(workspace, path string) (string, error) {
	absPath, err := fileDeleteTargetReady(workspace, path)
	if err != nil {
		return "", err
	}
	if err := os.Remove(absPath); err != nil {
		return "", err
	}
	return fmt.Sprintf("Removed %s", trustedFileWriteDisplayPath(workspace, absPath, path)), nil
}

// fileDeleteTargetReady checks that path names one regular file inside
// workspace and returns that absolute path. It does not remove anything, so
// intake can reject a correctable path before the one-shot grant is admitted.
func fileDeleteTargetReady(workspace, path string) (string, error) {
	absPath, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		if strings.Contains(err.Error(), "unavailable") {
			return "", fmt.Errorf("trusted_file_delete_path_unavailable")
		}
		return "", fmt.Errorf("trusted_file_delete_path_rejected")
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("trusted_file_delete_not_found")
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("trusted_file_delete_path_is_directory")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return absPath, nil
	}
	real, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("trusted_file_delete_path_rejected")
	}
	base, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil || !pathContainedInBase(normalizeProjectSessionPath(real), normalizeProjectSessionPath(base)) {
		return "", fmt.Errorf("trusted_file_delete_path_rejected")
	}
	return absPath, nil
}
