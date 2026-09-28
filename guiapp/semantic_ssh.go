package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedSSHAdapter        = "semantic_execute_trusted_ssh"
	semanticTrustedSSHImplementation = "trusted-ssh-execute-v1"
	// semanticTrustedSSHConnectImplementation is the session-less variant of the
	// same adapter: the schema admits host/credentials so the model can open a
	// session (or reuse the live one) instead of executing a command.
	semanticTrustedSSHConnectImplementation = "trusted-ssh-connect-v1"
	// semanticTrustedSSHCommandTimeout is how long one dispatched remote
	// command may run. The shell prompt ends the wait early, so a short
	// command still returns immediately. This is the safety cap, not the
	// 30s local-shell default: that shorter timer is a stuck-session probe,
	// and using it here sent Ctrl+C into apt and into a sleep that was
	// only waiting to read a log.
	semanticTrustedSSHCommandTimeout = semanticTrustedShellMaxTimeout
)

// semanticTrustedSSHAnyPublished reports whether either mode is published for
// the current catalog build. Exactly one mode publishes at a time (or none,
// when several ambiguous sessions are live): exec with a bound session,
// connect when session-less.
func semanticTrustedSSHAnyPublished(h *IMMessageHandler) bool {
	return semanticTrustedSSHPublished(h) || semanticTrustedSSHConnectPublished(h)
}

// sshAvailabilityMode names the published adapter mode for observability.
func sshAvailabilityMode(h *IMMessageHandler) string {
	if semanticTrustedSSHConnectPublished(h) {
		return "connect"
	}
	if semanticTrustedSSHPublished(h) {
		return "exec"
	}
	return "none"
}

// logSSHAvailabilityEvent emits the single observable event for the ssh
// availability stream (improvement plan §B4/§D1). ONE event source: maclaw.log
// counting and trajectory machine-reading both derive from this line and the
// model-visible rescue messages (which carry [ssh-rescue:<layer>] tokens).
// turn_total is the denominator for rescue-rate metrics; rescue is the
// numerator. Coverage notes: (1) turn_total counts MANAGED (semantic) turns
// only; legacy-router ssh turns never produce rescues and are excluded by
// design. (2) Petition/replan child revisions re-run the planner for the SAME
// user turn — sshAvailabilityCountEligible excludes their synthetic turn IDs,
// otherwise the denominator double-counts and dilutes the rescue rate.
func logSSHAvailabilityEvent(event string, fields ...string) {
	parts := append([]string{"event=" + event}, fields...)
	log.Printf("[ssh-availability] %s", strings.Join(parts, " "))
}

// sshAvailabilityCountEligible reports whether a planning call counts toward
// the turn_total denominator. Child-revision turn IDs (petition/replan) name
// planner re-runs of the same user turn, not new turns.
func sshAvailabilityCountEligible(turnID string) bool {
	turnID = strings.TrimSpace(turnID)
	return !strings.HasPrefix(turnID, "petition:") && !strings.HasPrefix(turnID, "replan:")
}

func semanticUnpublishedLegacySSHProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityShellExecuteRemoteHost {
			return true
		}
	}
	return false
}

// semanticTrustedSSHPublished reports the exec mode: a command-only surface
// bound host-side to the single live session (or a runtime binding).
func semanticTrustedSSHPublished(h *IMMessageHandler) bool {
	return h != nil && (h.semanticTrustedSSH != nil || trustedSSHSingleBoundSession(h) != nil)
}

// semanticTrustedSSHConnectPublished reports the connect mode: no runtime
// binding and no live session, so the only useful ssh surface is one that can
// OPEN a session. Without it a session-less turn (e.g. the user top-up turn
// "密码是 …" after a classifier degradation) has no connect path at all: the
// managed planner cannot plan an unbound exec adapter, leftover drops the
// builtin ssh tool on degraded classifications, and petitions die on the
// missing provider (production 2026-09-18 08:26).
func semanticTrustedSSHConnectPublished(h *IMMessageHandler) bool {
	if h == nil || h.semanticTrustedSSH != nil {
		return false
	}
	return len(trustedSSHBoundSessions(h)) == 0
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
			"command": map[string]interface{}{
				"type": "string",
				"description": fmt.Sprintf(
					"Command to run on the already-connected remote session. The host waits for the shell prompt, then sends Ctrl+C. Default and maximum are %d seconds; the prompt ends the wait early. Pass timeout_seconds only to give the session back sooner. A sleep longer than the wait is interrupted.",
					semanticTrustedSSHTimeoutSeconds(),
				),
			},
			"timeout_seconds": map[string]interface{}{
				"type": "integer",
				"description": fmt.Sprintf(
					"Seconds to wait for the remote shell prompt before the host sends Ctrl+C. Default %d. Maximum %d.",
					semanticTrustedSSHTimeoutSeconds(), semanticTrustedSSHTimeoutSeconds(),
				),
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

// hostBoundSSHDecoration lists legacy full-ssh-tool argument keys that are
// pinned host-side on the semantic surface: dropping them cannot change what
// executes because the session identity is bound by the host, not the model.
var hostBoundSSHDecoration = map[string]bool{
	"action": true, "session_id": true, "host": true, "user": true,
	"port": true, "label": true, "password": true, "key_path": true,
	"auth_method": true, "host_key_fingerprint": true, "capture_host_key": true,
	"force_new": true,
}

func semanticTrustedSSHConnectDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedSSHAdapter,
			"description": "Open (or reuse) an SSH session to a remote host. No session is currently bound.",
			"parameters":  semanticTrustedSSHConnectSchema(),
		},
	}
}

// semanticTrustedSSHConnectSchema is the session-less surface: it admits the
// connection fields the exec schema forbids. Field-level descriptions carry
// the mode-specific guidance because the catalog renderer rebuilds the
// top-level description from the capability registry, which is mode-agnostic.
//
// command is optional: the catalog is bound for the whole turn, so after a
// mid-turn connect the surface cannot switch to the exec schema — without
// this field the model's natural next call ({command: ...}) is rejected as an
// unknown field and the turn stalls (production 2026-09-18 14:08: connect
// succeeded, the exec-shaped follow-up was refused, and the model told the
// user to run commands in SecureCRT instead).
func semanticTrustedSSHConnectSchema() map[string]interface{} {
	strProp := func(description string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": description}
	}
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"host":     strProp("Server hostname or IP to connect to."),
			"user":     strProp("SSH login user."),
			"password": strProp("SSH password (or omit when using key auth configured host-side)."),
			"port":     map[string]interface{}{"type": "integer", "description": "SSH port, default 22."},
			"label":    strProp("Optional label for the saved host entry."),
			"command":  strProp("Optional command to run on the session right after connecting (the session is reused when one is already connected)."),
		},
		"required":             []string{"host", "user"},
		"additionalProperties": false,
	}
}

// semanticSSHSchemaIsConnectMode reports whether a rendered parameter schema
// is the connect-mode surface (admits host) rather than the exec-mode surface
// (command only). The admission-time legacy-shape wash uses it to pick its
// mode-specific behavior: exec mode strips host-bound decoration and rejects
// connect shapes as already bound; connect mode only drops pure legacy
// decoration (action/session_id) and keeps the declared connection fields.
func semanticSSHSchemaIsConnectMode(schema map[string]interface{}) bool {
	props, _ := schema["properties"].(map[string]interface{})
	_, ok := props["host"]
	return ok
}

// semanticTrustedSSHConnectArgsAllowed is the second-layer argument check for
// the connect surface, mirroring semanticTrustedSSHArgsAllowed on the exec
// side. Canonicalization has already enforced the closed schema.
func semanticTrustedSSHConnectArgsAllowed(args map[string]interface{}) (map[string]interface{}, error) {
	host, _ := args["host"].(string)
	user, _ := args["user"].(string)
	if strings.TrimSpace(host) == "" || strings.TrimSpace(user) == "" {
		return nil, fmt.Errorf("trusted_ssh_connect_host_user_required")
	}
	return args, nil
}

// semanticSSHInvocationArgs washes model-supplied ssh arguments before
// canonical schema validation, the same boundary role as
// semanticShellInvocationArgs. It runs against BOTH published modes; the
// caller passes the mode (derived from the rendered schema via
// semanticSSHSchemaIsConnectMode). The conversation history still shows the
// legacy ssh tool's action/session_id/host/password shapes, so the model
// repeats them (production 2026-09-18 "查看磁盘使用情况" turn: session_id and
// connect-shaped retries rejected, several iterations burned before a bare
// {command} call succeeded).
//
// Washing rules, all pre-execution so the one-shot grant stays live:
//   - exec mode (bound session, {command} schema): exec-shaped calls drop
//     host-bound decoration (session_id/host/user/port/label/password/key
//     material/action); connect-shaped calls (action=connect, no command)
//     get a dedicated rejection telling the model a session is already
//     bound — mapping them to a no-op command would fake a connection.
//   - connect mode (session-less, host/user/password schema): only pure
//     legacy decoration washes away (action, session_id — the action IS
//     connect on this surface and session_id cannot steer the host-bound
//     dial). host/user/password/port/label stay untouched; missing or
//     unknown fields fail closed in canonicalization so nothing the model
//     asked for is silently dropped.
//   - exec mode folds the local-shell aliases "timeout" and a decimal-string
//     timeout_seconds into timeout_seconds before validation. wait_seconds
//     and any other key that would change execution without a schema field
//     still pass through untouched so canonicalization rejects them.
func semanticSSHInvocationArgs(argsJSON string, connectMode bool) (string, error) {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON, nil
	}
	if connectMode {
		changed := false
		for _, key := range []string{"action", "session_id"} {
			if _, ok := parsed[key]; ok {
				delete(parsed, key)
				changed = true
			}
		}
		if !changed {
			return argsJSON, nil
		}
		body, err := json.Marshal(parsed)
		if err != nil {
			return argsJSON, nil
		}
		return string(body), nil
	}
	command, hasCommand := parsed["command"].(string)
	if !hasCommand || strings.TrimSpace(command) == "" {
		action, _ := parsed["action"].(string)
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "connect", "login", "open":
			return "", &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_session_already_bound: a remote session is already bound host-side for this turn; do not connect or reconnect. Call ssh with a bare {\"command\": \"...\"} argument to run the next command on the bound session."}
		}
		return argsJSON, nil
	}
	changed := false
	// Same correctable shapes as the local shell: "timeout" and "60" must not
	// burn the grant. A real timeout_seconds wins over the alias.
	if _, ok := parsed["timeout_seconds"]; !ok {
		if alias, ok := parsed["timeout"]; ok {
			delete(parsed, "timeout")
			parsed["timeout_seconds"] = alias
			changed = true
		}
	}
	if raw, ok := parsed["timeout_seconds"]; ok {
		if text, isString := raw.(string); isString {
			if seconds, convErr := strconv.Atoi(strings.TrimSpace(text)); convErr == nil {
				parsed["timeout_seconds"] = seconds
				changed = true
			}
		}
	}
	for key := range parsed {
		if key == "command" || key == "timeout_seconds" {
			continue
		}
		if hostBoundSSHDecoration[key] {
			delete(parsed, key)
			changed = true
		}
	}
	if !changed {
		return argsJSON, nil
	}
	// Execution-affecting or unknown keys (if any) stay in the washed map so
	// canonicalization rejects them: silently dropping them would run something
	// different from what the model asked for.
	body, err := json.Marshal(parsed)
	if err != nil {
		return argsJSON, nil
	}
	return string(body), nil
}

func semanticTrustedSSHArgsAllowed(args map[string]interface{}) (command string, timeout time.Duration, err error) {
	if len(args) > 2 {
		return "", 0, fmt.Errorf("trusted_ssh_arguments_rejected")
	}
	timeout = semanticTrustedSSHCommandTimeout
	hasCommand := false
	for key, raw := range args {
		switch key {
		case "command":
			value, ok := raw.(string)
			if !ok {
				return "", 0, fmt.Errorf("trusted_ssh_arguments_rejected")
			}
			command, hasCommand = value, true
		case "timeout_seconds":
			seconds, ok := semanticIntArg(raw)
			if !ok || seconds < 1 {
				return "", 0, fmt.Errorf("trusted_ssh_arguments_rejected")
			}
			timeout = time.Duration(seconds) * time.Second
			if timeout > semanticTrustedShellMaxTimeout {
				timeout = semanticTrustedShellMaxTimeout
			}
		default:
			return "", 0, fmt.Errorf("trusted_ssh_arguments_rejected")
		}
	}
	command = strings.TrimSpace(command)
	if !hasCommand || command == "" {
		return "", 0, fmt.Errorf("trusted_ssh_command_required")
	}
	return command, timeout, nil
}

func semanticTrustedSSHTimeoutSeconds() int {
	seconds := int(semanticTrustedSSHCommandTimeout / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// remoteSSHTimeoutHint tells the model why the host interrupted the command.
// A wait below the safety cap can be raised. The cap itself cannot.
func remoteSSHTimeoutHint(waited time.Duration) string {
	if waited <= 0 {
		waited = semanticTrustedSSHCommandTimeout
	}
	waitedSeconds := int(waited / time.Second)
	maxSeconds := semanticTrustedSSHTimeoutSeconds()
	if waited >= semanticTrustedShellMaxTimeout {
		return fmt.Sprintf("\n[system] Host waited %d seconds for the shell prompt, then sent Ctrl+C. That is the safety cap. Start a longer job in the background and poll it with a short command; do not sleep inside the remote command.", waitedSeconds)
	}
	return fmt.Sprintf("\n[system] Host waited %d seconds for the shell prompt, then sent Ctrl+C. Pass a larger timeout_seconds (maximum %d) to wait longer. sleep longer than that wait is interrupted the same way.", waitedSeconds, maxSeconds)
}

func (h *IMMessageHandler) executeTrustedSSH(principalID, command string, timeout time.Duration) (string, error) {
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
	return executeTrustedBoundSSH(h.sshMgr, session, command, timeout)
}

func executeTrustedBoundSSH(mgr *remote.SSHSessionManager, session *remote.SSHManagedSession, command string, timeout time.Duration) (string, error) {
	if mgr == nil || session == nil {
		return "", fmt.Errorf("trusted_ssh_session_unavailable")
	}
	if !guiRuntimeSSHSessionAlive(session) {
		return "", fmt.Errorf("trusted_ssh_session_disconnected")
	}
	if timeout <= 0 {
		timeout = semanticTrustedSSHCommandTimeout
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
	output, err := trustedSSHWaitResult(lines, status, ctx.Err(), command)
	if err != nil {
		return "", err
	}
	// The PTY echoes the command before its output. A long echo sits in front
	// of uptime/disk/memory and is what the preview keeps when the capture
	// exceeds the terminal budget.
	return stripLeadingCommandEcho(output, command), nil
}

// trustedSSHWaitResult classifies a finished wait. A deadline alone is not a
// failure: the command may have printed its prompt as the context expired.
// Only an explicit timeout notice means the wait gave up. A still-busy shell
// stays unobserved so another command is not written into it.
func trustedSSHWaitResult(lines []string, status remote.SessionStatus, waitErr error, command string) (string, error) {
	output := strings.TrimSpace(strings.Join(lines, "\n"))
	// The command was already written when the session ended, so whether it ran
	// is no longer observable. A deadline with empty output must not win: that
	// would look like a settled timeout and the next command would be sent to
	// a session that is gone.
	if status == remote.SessionExited || status == remote.SessionError {
		return "", fmt.Errorf("trusted_ssh_outcome_unobserved")
	}
	if strings.Contains(output, remote.SSHWaitTimeoutShellBusyNotice) {
		return "", fmt.Errorf("trusted_ssh_outcome_unobserved")
	}
	if strings.Contains(output, "[maclaw] 命令执行超时") || (waitErr == context.DeadlineExceeded && output == "") {
		if output == "" {
			return "", fmt.Errorf("trusted_ssh_timeout")
		}
		output = stripLeadingCommandEcho(compactSSHPtyOutput(output), command)
		if output == "" {
			return "", fmt.Errorf("trusted_ssh_timeout")
		}
		return "", fmt.Errorf("trusted_ssh_timeout: %s", output)
	}
	if output == "" {
		return "", fmt.Errorf("trusted_ssh_empty")
	}
	// PTY capture pads each line out to the terminal width and leaves bracketed
	// paste / title sequences in the text. That padding, not the metrics, is
	// what pushed a status command over the preview budget.
	output = compactSSHPtyOutput(output)
	if output == "" {
		return "", fmt.Errorf("trusted_ssh_empty")
	}
	return output, nil
}

// stripLeadingCommandEcho removes the PTY echo of command from the front of
// output. Wrapped echoes span lines at the terminal width. A line that mixes
// the tail of the echo with real output is left in place.
func stripLeadingCommandEcho(output, command string) string {
	return remote.StripLeadingCommandEcho(output, command)
}

// compactSSHPtyOutput strips terminal controls and trailing padding from one
// captured command. Internal spacing is kept so df/free columns stay readable.
func compactSSHPtyOutput(s string) string {
	return remote.CompactPtyOutput(s)
}

// sshConnectResultForFollowUpCommand drops the login preview that sshConnect
// appends. A command issued in the same turn already returns its own output;
// the MOTD and the orphan-task scan echo only compete with that output for
// the model-facing preview budget.
func sshConnectResultForFollowUpCommand(result string) string {
	for _, marker := range []string{"\n\n--- 初始输出 ---\n", "\n\n最近输出: "} {
		if i := strings.Index(result, marker); i >= 0 {
			result = result[:i]
		}
	}
	return result
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
