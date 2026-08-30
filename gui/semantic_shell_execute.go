package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedShellAdapter        = "semantic_execute_trusted_shell"
	semanticTrustedShellImplementation = "trusted-shell-execute-v1"
	semanticTrustedShellDefaultTimeout = 30 * time.Second
	semanticTrustedShellMaxTimeout     = 10 * time.Minute
)

func semanticUnpublishedLegacyShellProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityShellExecuteLocal {
			return true
		}
	}
	return false
}

func semanticTrustedShellDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedShellAdapter,
			"description": "Run one local command in the bound workspace. Working directory is host-fixed.",
			"parameters":  semanticTrustedShellInvocationSchema(),
		},
	}
}

func semanticTrustedShellInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command":         map[string]interface{}{"type": "string"},
			"timeout_seconds": map[string]interface{}{"type": "integer"},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

// semanticShellInvocationArgs washes model-supplied shell arguments before
// canonical schema validation, the same boundary role as
// semanticOfficeWriteInvocationArgs. Two correctable shapes show up in
// production: timeout as a decimal string ("60") and the alias key
// "timeout" for timeout_seconds. Canonicalization would reject both as
// parameter_schema_invalid and burn the one-shot grant on a typo. Unknown
// keys with real values (workdir, shell) pass through untouched: the working
// directory and interpreter are host-fixed, and dropping such a key would
// silently change what the model asked for.
func semanticShellInvocationArgs(argsJSON string) string {
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
	if _, ok := parsed["timeout_seconds"]; !ok {
		if alias, ok := parsed["timeout"]; ok {
			delete(parsed, "timeout")
			parsed["timeout_seconds"] = alias
			changed = true
		}
	}
	if raw, ok := parsed["timeout_seconds"]; ok {
		if text, isString := raw.(string); isString {
			if seconds, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
				parsed["timeout_seconds"] = seconds
				changed = true
			}
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

func semanticTrustedShellArgsAllowed(args map[string]interface{}) (command string, timeout time.Duration, err error) {
	if len(args) > 2 {
		return "", 0, fmt.Errorf("trusted_shell_arguments_rejected")
	}
	timeout = semanticTrustedShellDefaultTimeout
	hasCommand := false
	for key, raw := range args {
		switch key {
		case "command":
			value, ok := raw.(string)
			if !ok {
				return "", 0, fmt.Errorf("trusted_shell_arguments_rejected")
			}
			command, hasCommand = value, true
		case "timeout_seconds":
			seconds, ok := semanticIntArg(raw)
			if !ok || seconds < 1 {
				return "", 0, fmt.Errorf("trusted_shell_timeout_rejected")
			}
			timeout = time.Duration(seconds) * time.Second
			if timeout > semanticTrustedShellMaxTimeout {
				timeout = semanticTrustedShellMaxTimeout
			}
		default:
			return "", 0, fmt.Errorf("trusted_shell_arguments_rejected")
		}
	}
	command = strings.TrimSpace(command)
	if !hasCommand || command == "" {
		return "", 0, fmt.Errorf("trusted_shell_command_required")
	}
	return command, timeout, nil
}

func semanticIntArg(raw interface{}) (int, bool) {
	switch n := raw.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func (h *IMMessageHandler) executeTrustedShell(principalID, command string, timeout time.Duration) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_shell_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_shell_principal_required")
	}
	// These guards keep one capability from carrying another: a local shell
	// grant must not reach a remote host, the user's whole browser process
	// tree, an authenticated non-idempotent HTTP call, or a second browser
	// control plane. Managed turns have distinct trusted adapters for remote
	// execution and browser control, so a shell selection that could do those
	// things would hand the model more than the plan granted.
	//
	// They run ahead of the executor seam on purpose. A boundary a test double
	// can step around is not a boundary, and every other local shell path in
	// the tree applies this set before running anything.
	for _, guard := range []func(string) (string, bool){
		tool.RejectRawSSHCommand,
		tool.RejectBroadBrowserKillCommand,
		tool.RejectBrowserSideEffectHTTPCommand,
		tool.RejectShellBrowserAutomationCommand,
	} {
		if rejection, rejected := guard(command); rejected {
			return "", fmt.Errorf("%s", rejection)
		}
	}
	if h.semanticTrustedShell != nil {
		return h.semanticTrustedShell(principalID, command, timeout)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("trusted_shell_workspace_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// cmd /c via Go argument escaping eats inner quotes and mangles CJK
		// paths; NewWindowsShellCommand passes the line verbatim. UTF-8 env
		// keeps child (e.g. Python) stdio out of the GBK console codepage.
		cmd = tool.NewWindowsShellCommand(ctx, command, workspace)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
		cmd.Dir = workspace
	}
	cmd.Env = tool.AppendUTF8Env(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("trusted_shell_timeout")
	}
	if err != nil {
		if out == "" {
			return "", err
		}
		return fmt.Sprintf("%s\n%s", out, err.Error()), nil
	}
	if out == "" {
		out = "exit 0"
	}
	return out, nil
}

func semanticTrustedShellResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_shell_delivery_token")
	}
	if strings.Contains(text, "toolBash") || strings.Contains(text, "\"project_path\"") {
		return "", fmt.Errorf("trusted_shell_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_shell_empty")
	}
	return text, nil
}
