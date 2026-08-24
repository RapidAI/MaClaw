package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedSSHAdapter        = "semantic_execute_trusted_ssh"
	semanticTrustedSSHImplementation = "trusted-ssh-execute-v1"
)

func semanticUnpublishedLegacySSHProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityShellExecuteRemoteHost {
			return true
		}
	}
	return false
}

func semanticTrustedSSHPublished(h *IMMessageHandler) bool {
	return h != nil && (h.semanticTrustedSSH != nil || trustedSSHSingleBoundSession(h) != nil)
}

func trustedSSHBoundSessions(h *IMMessageHandler) []*remote.SSHManagedSession {
	if h == nil || h.sshMgr == nil {
		return nil
	}
	var out []*remote.SSHManagedSession
	for _, session := range h.sshMgr.List() {
		if guiRuntimeSSHSessionAlive(session) {
			out = append(out, session)
		}
	}
	return out
}

func trustedSSHSingleBoundSession(h *IMMessageHandler) *remote.SSHManagedSession {
	sessions := trustedSSHBoundSessions(h)
	if len(sessions) != 1 {
		return nil
	}
	return sessions[0]
}

func semanticTrustedSSHDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedSSHAdapter,
			"description": "Run one command on the host-bound remote session. Host and credentials are not model fields.",
			"parameters":  semanticTrustedSSHInvocationSchema(),
		},
	}
}

func semanticTrustedSSHInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func semanticTrustedSSHArgsAllowed(args map[string]interface{}) (command string, err error) {
	if len(args) > 1 {
		return "", fmt.Errorf("trusted_ssh_arguments_rejected")
	}
	hasCommand := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("trusted_ssh_arguments_rejected")
		}
		switch key {
		case "command":
			command, hasCommand = value, true
		default:
			return "", fmt.Errorf("trusted_ssh_arguments_rejected")
		}
	}
	command = strings.TrimSpace(command)
	if !hasCommand || command == "" {
		return "", fmt.Errorf("trusted_ssh_command_required")
	}
	return command, nil
}

func (h *IMMessageHandler) executeTrustedSSH(principalID, command string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_ssh_session_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_ssh_principal_required")
	}
	if h.semanticTrustedSSH != nil {
		return h.semanticTrustedSSH(principalID, command)
	}
	if rejection, rejected := tool.RejectRawSSHCommand(command); rejected {
		return "", fmt.Errorf("%s", rejection)
	}
	session := trustedSSHSingleBoundSession(h)
	if session == nil {
		return "", fmt.Errorf("trusted_ssh_session_unavailable")
	}
	return executeTrustedBoundSSH(h.sshMgr, session, command, semanticTrustedShellDefaultTimeout)
}

func executeTrustedBoundSSH(mgr *remote.SSHSessionManager, session *remote.SSHManagedSession, command string, timeout time.Duration) (string, error) {
	if mgr == nil || session == nil {
		return "", fmt.Errorf("trusted_ssh_session_unavailable")
	}
	if !guiRuntimeSSHSessionAlive(session) {
		return "", fmt.Errorf("trusted_ssh_session_disconnected")
	}
	if timeout <= 0 {
		timeout = semanticTrustedShellDefaultTimeout
	}
	before := session.LineCount()
	if err := mgr.WriteInput(session.ID, command); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "disconnect") || strings.Contains(strings.ToLower(err.Error()), "not found") {
			return "", fmt.Errorf("trusted_ssh_session_disconnected")
		}
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	lines, status := mgr.WaitForOutputContext(ctx, session.ID, before, timeout)
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("trusted_ssh_timeout")
	}
	// The command was already written when the session ended, so whether it ran
	// is no longer observable. The two checks above use the disconnected name
	// for the opposite fact -- a session that never carried the command -- and
	// keeping one name for both would leave the classification below correct
	// only by coincidence, since it happens to treat every one of them as
	// unknown. Anyone tightening that list needs the distinction to exist.
	if status == remote.SessionExited || status == remote.SessionError {
		return "", fmt.Errorf("trusted_ssh_outcome_unobserved")
	}
	output := strings.TrimSpace(strings.Join(lines, "\n"))
	if output == "" {
		return "", fmt.Errorf("trusted_ssh_empty")
	}
	return output, nil
}

func semanticTrustedSSHResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_ssh_delivery_token")
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "reconnecting") || strings.Contains(lower, "auto-retry") {
		return "", fmt.Errorf("trusted_ssh_reconnect_forbidden")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_ssh_empty")
	}
	return text, nil
}
