package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostSSHProviderID     = "core-ssh"
	reviewedHostSSHImplementation = "bound-session"
	reviewedHostSSHAdapterName    = "host_shell_execute_remote_host"
	reviewedHostSSHDefaultTimeout = 30 * time.Second
)

type reviewedHostSSHExecutor interface {
	ExecuteReviewedHostSSH(ctx context.Context, principal Principal, command string) (string, error)
}

func reviewedHostSSHInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func reviewedHostSSHContractDigest() string {
	return coretool.SchemaDigest([]byte("shell.execute.remote_host:v1:host-ssh"))
}

// ProjectReviewedHostSSHProvider projects the host-owned remote command.
// It is not a Skill/MCP discovery entry and must not import GUI ssh. The
// closed schema accepts command only. host, label, session_id, channel, and
// destination are rejected. No live session means the provider is not
// attached. Timeout or disconnect is unknown. This is not a channel send.
func ProjectReviewedHostSSHProvider(executor reviewedHostSSHExecutor) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if executor == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host ssh executor is unavailable")
	}
	parameters := reviewedHostSSHInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host ssh schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostSSHContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-ssh-command-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostSSHAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostSSHProviderID,
			ImplementationID: reviewedHostSSHImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilitySSHExecute,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostSSH(executor)}, nil
}

func AttachReviewedHostSSHProvider(catalog DynamicSemanticCatalog, executor reviewedHostSSHExecutor) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostSSHProvider(executor)
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

func executeReviewedHostSSH(executor reviewedHostSSHExecutor) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if executor == nil {
			return "", fmt.Errorf("host_ssh_session_unavailable")
		}
		command, err := reviewedHostSSHArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return executor.ExecuteReviewedHostSSH(ctx, principal, command)
	}
}

func reviewedHostSSHArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("host_ssh_arguments_rejected")
	}
	command := ""
	hasCommand := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("host_ssh_arguments_rejected")
		}
		switch key {
		case "command":
			command, hasCommand = value, true
		default:
			return "", fmt.Errorf("host_ssh_arguments_rejected")
		}
	}
	command = strings.TrimSpace(command)
	if !hasCommand || command == "" {
		return "", fmt.Errorf("host_ssh_command_required")
	}
	return command, nil
}

func reviewedHostSSHSessionAlive(session *remote.SSHManagedSession) bool {
	if session == nil || session.Handle == nil || !session.Handle.IsAlive() {
		return false
	}
	return remote.SessionStatus(session.GetSummary().Status).IsRunning()
}

func reviewedHostSingleBoundSSHSession(c *coreAgentCallbacks) *remote.SSHManagedSession {
	if c == nil || c.sshDeps.Manager == nil {
		return nil
	}
	var alive []*remote.SSHManagedSession
	for _, session := range c.sshDeps.Manager.List() {
		if reviewedHostSSHSessionAlive(session) {
			alive = append(alive, session)
		}
	}
	if len(alive) != 1 {
		return nil
	}
	return alive[0]
}

func reviewedHostSSHResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("host_ssh_delivery_token")
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "reconnecting") || strings.Contains(lower, "auto-retry") {
		return "", fmt.Errorf("host_ssh_reconnect_forbidden")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_ssh_empty")
	}
	return text, nil
}

func executeReviewedHostBoundSSH(ctx context.Context, mgr *remote.SSHSessionManager, session *remote.SSHManagedSession, command string, timeout time.Duration) (string, error) {
	if mgr == nil || session == nil {
		return "", fmt.Errorf("host_ssh_session_unavailable")
	}
	if !reviewedHostSSHSessionAlive(session) {
		return "", fmt.Errorf("host_ssh_session_disconnected")
	}
	if timeout <= 0 {
		timeout = reviewedHostSSHDefaultTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	before := session.LineCount()
	if err := mgr.WriteInput(session.ID, command); err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "disconnect") || strings.Contains(lower, "not found") {
			return "", fmt.Errorf("host_ssh_session_disconnected")
		}
		return "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lines, status := mgr.WaitForOutputContext(runCtx, session.ID, before, timeout)
	if runCtx.Err() != nil {
		return "", fmt.Errorf("host_ssh_timeout")
	}
	if status == remote.SessionExited || status == remote.SessionError {
		return "", fmt.Errorf("host_ssh_session_disconnected")
	}
	return reviewedHostSSHResultProjection(strings.Join(lines, "\n"))
}

func (c *coreAgentCallbacks) ExecuteReviewedHostSSH(ctx context.Context, principal Principal, command string) (string, error) {
	if c == nil || c.delegateChild || c.runtimeReadOnlyChild {
		return "", fmt.Errorf("host_ssh_session_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_ssh_principal_mismatch")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("host_ssh_command_required")
	}
	if rejection, rejected := coretool.RejectRawSSHCommand(command); rejected {
		return "", fmt.Errorf("%s", rejection)
	}
	if c.trustedSSH != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		runCtx, cancel := context.WithTimeout(ctx, reviewedHostSSHDefaultTimeout)
		defer cancel()
		out, err := c.trustedSSH(runCtx, principal, command)
		if runCtx.Err() != nil {
			return "", fmt.Errorf("host_ssh_timeout")
		}
		if err != nil {
			code := strings.TrimSpace(err.Error())
			if dynamicHostObservedExternalUnknown(code) {
				return "", err
			}
			lower := strings.ToLower(code)
			// A driver that already separated "the command may have run" from
			// "it never left" must not have that distinction discarded here:
			// none of the rungs below match an unobserved name, so it would
			// fall through and be reported as a definite failure.
			if strings.Contains(lower, "unobserved") {
				return "", fmt.Errorf("host_ssh_outcome_unobserved")
			}
			if strings.Contains(lower, "timeout") {
				return "", fmt.Errorf("host_ssh_timeout")
			}
			if strings.Contains(lower, "disconnect") {
				return "", fmt.Errorf("host_ssh_session_disconnected")
			}
			if strings.Contains(lower, "unavailable") {
				return "", fmt.Errorf("host_ssh_session_unavailable")
			}
			return "", err
		}
		return reviewedHostSSHResultProjection(out)
	}
	session := reviewedHostSingleBoundSSHSession(c)
	if session == nil {
		return "", fmt.Errorf("host_ssh_session_unavailable")
	}
	return executeReviewedHostBoundSSH(ctx, c.sshDeps.Manager, session, command, reviewedHostSSHDefaultTimeout)
}
