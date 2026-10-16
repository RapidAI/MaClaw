package guiapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/buildverify"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedBuildVerifyAdapter        = "semantic_run_trusted_build_verify"
	semanticTrustedBuildVerifyImplementation = "trusted-build-verify-v1"
)

func semanticTrustedBuildVerifyDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedBuildVerifyAdapter,
			"description": "Run one reviewed verification task in the bound workspace. The host picks the program. build in a LaTeX directory compiles each document there, standalone figures before the file that includes them. Optionally give target as the workspace subdirectory that contains the .tex files.",
			"parameters":  semanticTrustedBuildVerifyInvocationSchema(),
		},
	}
}

func semanticTrustedBuildVerifyInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{"type": "string", "enum": tool.BuildVerifyTasks()},
			// A subdirectory is the one narrowing that works the same way for
			// every project kind. Per-tool package selectors would each need
			// their own argument syntax, and that syntax is where a command
			// line creeps back into the model's hands.
			"target": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	}
}

func semanticTrustedBuildVerifyArgsAllowed(args map[string]interface{}) (task, target string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("trusted_build_verify_arguments_rejected")
	}
	hasTask := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("trusted_build_verify_arguments_rejected")
		}
		switch key {
		case "task":
			task, hasTask = strings.TrimSpace(value), true
		case "target":
			target = strings.TrimSpace(value)
		default:
			return "", "", fmt.Errorf("trusted_build_verify_arguments_rejected")
		}
	}
	if !hasTask {
		return "", "", fmt.Errorf("trusted_build_verify_task_required")
	}
	// The enum is re-checked here rather than trusted from the schema. A
	// boundary that only exists in a document the model is shown is not a
	// boundary.
	if !semanticBuildVerifyTaskAllowed(task) {
		return "", "", fmt.Errorf("trusted_build_verify_task_rejected")
	}
	return task, target, nil
}

func semanticBuildVerifyTaskAllowed(task string) bool {
	return tool.BuildVerifyTaskAllowed(task)
}

func semanticBuildVerifyResolveDir(workspace, target string) (string, error) {
	dir, escaped, notDir := tool.BuildVerifyWorkspaceSubdir(workspace, target)
	if escaped {
		return "", fmt.Errorf("trusted_build_verify_target_rejected")
	}
	if notDir {
		return "", fmt.Errorf("trusted_build_verify_target_not_a_directory")
	}
	return dir, nil
}

func (h *IMMessageHandler) runTrustedBuildVerify(principalID, task, target string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_build_verify_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_build_verify_principal_required")
	}
	if !semanticBuildVerifyTaskAllowed(task) {
		return "", fmt.Errorf("trusted_build_verify_task_rejected")
	}
	if h.semanticTrustedBuildVerify != nil {
		return h.semanticTrustedBuildVerify(principalID, task, target)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("trusted_build_verify_workspace_unavailable")
	}
	runDir, err := semanticBuildVerifyResolveDir(workspace, target)
	if err != nil {
		return "", err
	}
	out, err := buildverify.Run(context.Background(), workspace, runDir, task)
	if err != nil {
		switch {
		case errors.Is(err, buildverify.ErrUnrecognised):
			return "", fmt.Errorf("trusted_build_verify_project_unrecognised")
		case errors.Is(err, buildverify.ErrUnsupported):
			return "", fmt.Errorf("trusted_build_verify_task_unsupported")
		case errors.Is(err, buildverify.ErrTimeout):
			return "", buildVerifyTimeoutError("trusted_build_verify_timeout", out)
		default:
			return "", err
		}
	}
	return out, nil
}

func buildVerifyTimeoutError(prefix, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return errors.New(prefix)
	}
	return errors.New(prefix + "\n" + detail)
}

func semanticTrustedBuildVerifyResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_build_verify_delivery_token")
	}
	if strings.Contains(text, "toolBash") || strings.Contains(text, "\"project_path\"") {
		return "", fmt.Errorf("trusted_build_verify_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_build_verify_empty")
	}
	return text, nil
}
