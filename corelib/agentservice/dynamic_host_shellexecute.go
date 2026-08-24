package agentservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostShellProviderID     = "core-shellexecute"
	reviewedHostShellImplementation = "local"
	reviewedHostShellAdapterName    = "host_shell_execute_local"
	reviewedHostShellDefaultTimeout = 30 * time.Second
	reviewedHostShellMaxTimeout     = 10 * time.Minute
)

type reviewedHostShellExecutor interface {
	ExecuteReviewedHostShell(ctx context.Context, principal Principal, command string, timeout time.Duration) (string, error)
}

func reviewedHostShellInvocationSchema() map[string]interface{} {
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

func reviewedHostShellContractDigest() string {
	return coretool.SchemaDigest([]byte("shell.execute.local:v1:host-shellexecute"))
}

// ProjectReviewedHostShellProvider projects the host-owned workspace command.
// It is not a Skill/MCP discovery entry and must not import GUI bash. The
// closed schema accepts command and optional timeout_seconds. project_path,
// working_dir, channel, and destination are rejected. cwd is the bound
// workspace. The host process waits for exit, so the handler result is the
// local completion receipt.
func ProjectReviewedHostShellProvider(executor reviewedHostShellExecutor) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if executor == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host shell executor is unavailable")
	}
	parameters := reviewedHostShellInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host shell schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostShellContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-shellexecute-command-timeout-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostShellAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostShellProviderID,
			ImplementationID: reviewedHostShellImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityShellExecute,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostShell(executor)}, nil
}

func AttachReviewedHostShellProvider(catalog DynamicSemanticCatalog, executor reviewedHostShellExecutor) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostShellProvider(executor)
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

func executeReviewedHostShell(executor reviewedHostShellExecutor) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if executor == nil {
			return "", fmt.Errorf("host_shell_unavailable")
		}
		command, timeout, err := reviewedHostShellArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return executor.ExecuteReviewedHostShell(ctx, principal, command, timeout)
	}
}

func reviewedHostShellArgsAllowed(args map[string]interface{}) (string, time.Duration, error) {
	if len(args) > 2 {
		return "", 0, fmt.Errorf("host_shell_arguments_rejected")
	}
	command := ""
	timeout := reviewedHostShellDefaultTimeout
	hasCommand := false
	for key, raw := range args {
		switch key {
		case "command":
			value, ok := raw.(string)
			if !ok {
				return "", 0, fmt.Errorf("host_shell_arguments_rejected")
			}
			command, hasCommand = value, true
		case "timeout_seconds":
			seconds, ok := reviewedHostIntArg(raw)
			if !ok || seconds < 1 {
				return "", 0, fmt.Errorf("host_shell_timeout_rejected")
			}
			timeout = time.Duration(seconds) * time.Second
			if timeout > reviewedHostShellMaxTimeout {
				timeout = reviewedHostShellMaxTimeout
			}
		default:
			return "", 0, fmt.Errorf("host_shell_arguments_rejected")
		}
	}
	command = strings.TrimSpace(command)
	if !hasCommand || command == "" {
		return "", 0, fmt.Errorf("host_shell_command_required")
	}
	return command, timeout, nil
}

func reviewedHostIntArg(raw interface{}) (int, bool) {
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
	case json.Number:
		parsed, err := n.Int64()
		if err != nil || parsed < 1 {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func (c *coreAgentCallbacks) ExecuteReviewedHostShell(ctx context.Context, principal Principal, command string, timeout time.Duration) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" || !c.canUseLocalBash() {
		return "", fmt.Errorf("host_shell_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_shell_principal_mismatch")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("host_shell_command_required")
	}
	// Same boundary as the GUI trusted shell adapter: a managed local shell
	// grant must not carry remote execution or browser control, both of which
	// have their own trusted adapters on this host.
	for _, guard := range []func(string) (string, bool){
		coretool.RejectRawSSHCommand,
		coretool.RejectBroadBrowserKillCommand,
		coretool.RejectBrowserSideEffectHTTPCommand,
		coretool.RejectShellBrowserAutomationCommand,
	} {
		if rejection, rejected := guard(command); rejected {
			return "", fmt.Errorf("%s", rejection)
		}
	}
	if timeout <= 0 {
		timeout = reviewedHostShellDefaultTimeout
	}
	if timeout > reviewedHostShellMaxTimeout {
		timeout = reviewedHostShellMaxTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(runCtx, "bash", "-lc", command)
	}
	cmd.Dir = c.workspace
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("host_shell_timeout")
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
