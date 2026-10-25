package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
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

// semanticSSHPublicationMode is the one catalog mode for this principal.
// A runtime hook forces exec. Otherwise the count of live sessions this
// principal opened decides the mode: zero publishes connect, one publishes
// exec, and any other count publishes neither. Sessions opened by another
// principal are not part of the count.
func semanticSSHPublicationMode(bound int, runtimeHook bool) string {
	if runtimeHook {
		return "exec"
	}
	switch {
	case bound <= 0:
		return "connect"
	case bound == 1:
		return "exec"
	default:
		return "none"
	}
}

// semanticSSHCatalogMode is one catalog decision for this principal.
// A runtime hook forces exec without listing sessions. Otherwise the live
// sessions this principal opened are read once: zero publishes connect, one
// publishes exec, and any other count publishes neither.
func semanticSSHCatalogMode(h *IMMessageHandler, principalID string) string {
	if h == nil {
		return "none"
	}
	if h.semanticTrustedSSH != nil {
		return "exec"
	}
	return semanticSSHPublicationMode(len(trustedSSHBoundSessions(h, principalID)), false)
}

// semanticTrustedSSHAnyPublished reports whether either mode is published for
// this principal's catalog build. Exactly one mode publishes at a time (or
// none, when this principal has several live sessions).
func semanticTrustedSSHAnyPublished(h *IMMessageHandler, principalID string) bool {
	mode := semanticSSHCatalogMode(h, principalID)
	return mode == "exec" || mode == "connect"
}

// sshAvailabilityMode names the published adapter mode for observability.
func sshAvailabilityMode(h *IMMessageHandler, principalID string) string {
	return semanticSSHCatalogMode(h, principalID)
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
// bound to the one live session this principal owns (or a runtime binding).
func semanticTrustedSSHPublished(h *IMMessageHandler, principalID string) bool {
	return semanticSSHCatalogMode(h, principalID) == "exec"
}

// semanticTrustedSSHConnectPublished reports the connect mode: no runtime
// binding and no live session owned by this principal, so the only useful
// ssh surface is one that can OPEN a session. Another principal's session
// leaves this turn session-less. Without connect, a session-less turn
// (e.g. the user top-up turn "密码是 …" after a classifier degradation) has
// no connect path at all: the managed planner cannot plan an unbound exec
// adapter, leftover drops the builtin ssh tool on degraded classifications,
// and petitions die on the missing provider (production 2026-09-18 08:26).
func semanticTrustedSSHConnectPublished(h *IMMessageHandler, principalID string) bool {
	return semanticSSHCatalogMode(h, principalID) == "connect"
}

func trustedSSHBoundSessions(h *IMMessageHandler, principalID string) []*remote.SSHManagedSession {
	if h == nil || h.sshMgr == nil {
		return nil
	}
	return sshSessionsBoundToPrincipal(h.sshMgr.List(), principalID, guiRuntimeSSHSessionAlive)
}

// sshSessionsBoundToPrincipal keeps live sessions opened by principalID.
// Liveness is injected so the ownership rule can be tested without a PTY.
func sshSessionsBoundToPrincipal(sessions []*remote.SSHManagedSession, principalID string, alive func(*remote.SSHManagedSession) bool) []*remote.SSHManagedSession {
	if alive == nil {
		return nil
	}
	var out []*remote.SSHManagedSession
	for _, session := range sessions {
		if session == nil || !remote.SSHSessionOwnerMatches(session.OwnerID, principalID) {
			continue
		}
		if alive(session) {
			out = append(out, session)
		}
	}
	return out
}

func trustedSSHSingleBoundSession(h *IMMessageHandler, principalID string) *remote.SSHManagedSession {
	sessions := trustedSSHBoundSessions(h, principalID)
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
			"description": "Run one command on the host-bound remote session, or copy one remote file or directory onto this computer with remote_path and local_path. remote_path may be absolute or start with ~/. Host, credentials, and session id are not model fields.",
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
					"Seconds to wait for the remote shell prompt before the host sends Ctrl+C. Default %d. Maximum %d. Used only with command.",
					semanticTrustedSSHTimeoutSeconds(), semanticTrustedSSHTimeoutSeconds(),
				),
			},
			"remote_path": semanticSSHRemotePathProperty(),
			"local_path":  semanticSSHLocalPathProperty(),
		},
		// command and the path pair are alternatives. The wash rejects an
		// empty call and a one-sided path before the grant is spent.
		"additionalProperties": false,
	}
}

func semanticSSHRemotePathProperty() map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": "File or directory on the connected remote host, including a host reached through ProxyJump such as gpu84. An absolute path or ~/path (the login home) is copied the way scp would copy it. Pair with local_path. Do not cat, base64, or scp the file through command. source_path is not a field.",
	}
}

func semanticSSHLocalPathProperty() map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": "Destination on this computer. An absolute path is used as given. A relative path resolves in the working directory. An existing directory keeps the remote file's name inside it. Pair with remote_path.",
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
			"description": "Open or reuse an SSH session. Pass host, or host with remote_path and local_path to copy a file onto this computer in the same call. An OpenSSH alias such as gpu84 keeps its IdentityFile and ProxyJump.",
			"parameters":  semanticTrustedSSHConnectSchema(),
		},
	}
}

// semanticTrustedSSHConnectSchema is the session-less surface: it admits the
// connection fields the exec schema forbids. The catalog renderer rebuilds
// the top-level description from the mode-agnostic capability summary, then
// semanticRemoteHostSchemaCue appends whether host and the credential fields
// in this schema are required. Field descriptions still carry the rest.
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
			"host":        strProp("Server hostname, IP, or an OpenSSH config alias such as gpu84. An alias uses ~/.ssh/config, including its IdentityFile and ProxyJump."),
			"user":        strProp("SSH login user. Omit it when host is an OpenSSH config alias that already sets User."),
			"password":    strProp("SSH password (or omit when using key auth configured host-side)."),
			"port":        map[string]interface{}{"type": "integer", "description": "SSH port, default 22."},
			"label":       strProp("Optional label for the saved host entry."),
			"command":     strProp("Optional command to run on the session right after connecting (the session is reused when one is already connected). Do not combine with remote_path or local_path."),
			"remote_path": semanticSSHRemotePathProperty(),
			"local_path":  semanticSSHLocalPathProperty(),
		},
		"required":             []string{"host"},
		"additionalProperties": false,
	}
}

// semanticSSHSchemaIsConnectMode reports whether a rendered parameter schema
// is the connect-mode surface (admits host) rather than the exec-mode surface
// (command, or remote_path with local_path). The admission-time legacy-shape wash uses it to pick its
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
	if strings.TrimSpace(host) == "" {
		return nil, fmt.Errorf("trusted_ssh_connect_host_required")
	}
	if user, _ := args["user"].(string); strings.TrimSpace(user) == "" {
		delete(args, "user")
	}
	if err := semanticSSHDownloadShapeError(args, true); err != nil {
		return nil, err
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
//   - a file copy is remote_path plus local_path. Legacy names such as
//     source_path fold onto those fields. A one-sided path or a mix with
//     command is refused before execution so the same ssh tool stays listed.
func semanticSSHInvocationArgs(argsJSON string, connectMode bool) (string, error) {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON, nil
	}
	changed, err := foldSemanticSSHPathAliases(parsed)
	if err != nil {
		return "", err
	}
	action, _ := parsed["action"].(string)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "upload":
		return "", &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_upload_unavailable: this ssh tool copies from the remote host onto this computer with remote_path and local_path, or runs command. It does not upload. The call was refused before execution, so the tool remains available."}
	}
	if connectMode {
		for _, key := range []string{"action", "session_id"} {
			if _, ok := parsed[key]; ok {
				delete(parsed, key)
				changed = true
			}
		}
		if err := semanticSSHDownloadShapeError(parsed, true); err != nil {
			return "", err
		}
	} else {
		command := strings.TrimSpace(sshStringArg(parsed, "command"))
		remote := strings.TrimSpace(sshStringArg(parsed, "remote_path"))
		local := strings.TrimSpace(sshStringArg(parsed, "local_path"))
		if command == "" && remote == "" && local == "" {
			switch strings.ToLower(strings.TrimSpace(action)) {
			case "connect", "login", "open":
				return "", &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_session_already_bound: a remote session is already bound host-side for this turn; do not connect or reconnect. Call ssh with {\"command\": \"...\"} to run a command, or {\"remote_path\": \"...\", \"local_path\": \"...\"} to copy a file onto this computer."}
			}
		}
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
			if key == "command" || key == "timeout_seconds" || key == "remote_path" || key == "local_path" {
				continue
			}
			if hostBoundSSHDecoration[key] {
				delete(parsed, key)
				changed = true
			}
		}
		if err := semanticSSHDownloadShapeError(parsed, false); err != nil {
			return "", err
		}
	}
	normalized, err := normalizeSemanticSSHDownloadPaths(parsed)
	if err != nil {
		return "", err
	}
	changed = changed || normalized
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

func foldSemanticSSHPathAliases(parsed map[string]interface{}) (bool, error) {
	changed := false
	for _, alias := range []string{"source_path", "src_path", "remote_file", "from_path"} {
		folded, err := foldSemanticSSHPathAlias(parsed, alias, "remote_path")
		if err != nil {
			return false, err
		}
		changed = changed || folded
	}
	for _, alias := range []string{"local_file", "dest_path", "destination_path", "save_path", "output_path", "target_path"} {
		folded, err := foldSemanticSSHPathAlias(parsed, alias, "local_path")
		if err != nil {
			return false, err
		}
		changed = changed || folded
	}
	return changed, nil
}

func foldSemanticSSHPathAlias(parsed map[string]interface{}, alias, canonical string) (bool, error) {
	raw, ok := parsed[alias]
	if !ok {
		return false, nil
	}
	aliasText, isString := raw.(string)
	if !isString {
		return false, nil
	}
	if existing, exists := parsed[canonical]; exists {
		existingText, _ := existing.(string)
		if strings.TrimSpace(existingText) != strings.TrimSpace(aliasText) {
			return false, &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_download_paths_required: " + alias + " and " + canonical + " disagree. Pass remote_path and local_path. The call was refused before execution, so the tool remains available."}
		}
		delete(parsed, alias)
		return true, nil
	}
	parsed[canonical] = strings.TrimSpace(aliasText)
	delete(parsed, alias)
	return true, nil
}

func semanticSSHDownloadShapeError(parsed map[string]interface{}, connectMode bool) error {
	command := strings.TrimSpace(sshStringArg(parsed, "command"))
	remote := strings.TrimSpace(sshStringArg(parsed, "remote_path"))
	local := strings.TrimSpace(sshStringArg(parsed, "local_path"))
	_, hasRemote := parsed["remote_path"]
	_, hasLocal := parsed["local_path"]
	if command != "" && (hasRemote || hasLocal) {
		return &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_conflicting_fields: pass either command, or remote_path together with local_path. The call was refused before execution, so the tool remains available."}
	}
	if hasRemote != hasLocal || (hasRemote && (remote == "" || local == "")) {
		return &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_download_paths_required: copy a file with both remote_path (on the server) and local_path (on this computer). source_path is not a field. The call was refused before execution, so the tool remains available."}
	}
	if !connectMode && command == "" && remote == "" && local == "" {
		return &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_call_shape: pass command to run a shell command, or remote_path and local_path to copy a file onto this computer. session_id is not a field. The call was refused before execution, so the tool remains available."}
	}
	return nil
}

// normalizeSemanticSSHDownloadPaths cleans a copy's paths before the grant is
// spent. filepath.Clean would drop a trailing separator, which is the signal
// that a not-yet-created local path is a directory, so local_path is only
// trimmed and checked here.
func normalizeSemanticSSHDownloadPaths(parsed map[string]interface{}) (bool, error) {
	changed := false
	if _, ok := parsed["remote_path"]; ok {
		raw := sshStringArg(parsed, "remote_path")
		cleaned, err := semanticSSHRemotePath(raw)
		if err != nil {
			if errors.Is(err, errSemanticSSHRemoteHomeUser) {
				return false, &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_remote_path_required: ~otheruser is not expanded. Pass ~, ~/path, or an absolute remote path. The call was refused before execution, so the tool remains available."}
			}
			return false, &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_remote_path_required: pass one remote file or directory path, with no line breaks. The call was refused before execution, so the tool remains available."}
		}
		if cleaned != raw {
			parsed["remote_path"] = cleaned
			changed = true
		}
	}
	if _, ok := parsed["local_path"]; ok {
		raw := sshStringArg(parsed, "local_path")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.ContainsAny(trimmed, "\r\n\x00") {
			return false, &semanticCanonicalDetailedRejection{text: "[system rejected] trusted_ssh_local_path_required: pass one local destination path, with no line breaks. The call was refused before execution, so the tool remains available."}
		}
		if trimmed != raw {
			parsed["local_path"] = trimmed
			changed = true
		}
	}
	return changed, nil
}

func semanticTrustedSSHArgsAllowed(args map[string]interface{}) (command string, timeout time.Duration, remotePath, localPath string, err error) {
	timeout = semanticTrustedSSHCommandTimeout
	hasCommand := false
	hasRemote := false
	hasLocal := false
	for key, raw := range args {
		switch key {
		case "command":
			value, ok := raw.(string)
			if !ok {
				return "", 0, "", "", fmt.Errorf("trusted_ssh_arguments_rejected")
			}
			command, hasCommand = value, true
		case "timeout_seconds":
			seconds, ok := semanticIntArg(raw)
			if !ok || seconds < 1 {
				return "", 0, "", "", fmt.Errorf("trusted_ssh_arguments_rejected")
			}
			timeout = time.Duration(seconds) * time.Second
			if timeout > semanticTrustedShellMaxTimeout {
				timeout = semanticTrustedShellMaxTimeout
			}
		case "remote_path":
			value, ok := raw.(string)
			if !ok {
				return "", 0, "", "", fmt.Errorf("trusted_ssh_arguments_rejected")
			}
			remotePath, hasRemote = value, true
		case "local_path":
			value, ok := raw.(string)
			if !ok {
				return "", 0, "", "", fmt.Errorf("trusted_ssh_arguments_rejected")
			}
			localPath, hasLocal = value, true
		default:
			return "", 0, "", "", fmt.Errorf("trusted_ssh_arguments_rejected")
		}
	}
	command = strings.TrimSpace(command)
	remotePath = strings.TrimSpace(remotePath)
	localPath = strings.TrimSpace(localPath)
	if (hasCommand && command != "") && (hasRemote || hasLocal) {
		return "", 0, "", "", fmt.Errorf("trusted_ssh_conflicting_fields")
	}
	if hasRemote || hasLocal {
		if !hasRemote || remotePath == "" || !hasLocal || localPath == "" {
			return "", 0, "", "", fmt.Errorf("trusted_ssh_download_paths_required")
		}
		return "", timeout, remotePath, localPath, nil
	}
	if !hasCommand || command == "" {
		return "", 0, "", "", fmt.Errorf("trusted_ssh_command_required")
	}
	return command, timeout, "", "", nil
}

func semanticPrefixedRejection(err error) string {
	if err == nil {
		return "[system rejected]"
	}
	msg := strings.TrimSpace(err.Error())
	if strings.HasPrefix(msg, "[system rejected]") || strings.HasPrefix(msg, "[system unknown]") {
		return msg
	}
	return "[system rejected] " + msg
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
	session := trustedSSHSingleBoundSession(h, principalID)
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

// semanticSSHConnectDownloadResult puts the copy first. The outcome classifier
// reads only the first line, and the connect banner would make a failed copy
// look like a successful dial. The session header stays after the copy so the
// model still has the session id, without the login preview.
func semanticSSHConnectDownloadResult(connectResult, downloadResult string) string {
	header := sshConnectResultForFollowUpCommand(connectResult)
	downloadResult = strings.TrimSpace(downloadResult)
	if downloadResult == "" {
		return header
	}
	if header == "" {
		return downloadResult
	}
	return downloadResult + "\n\n" + header
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

// sshSessionIDFromConnectResult reads the session id sshConnect prints.
func sshSessionIDFromConnectResult(result string) string {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "会话 ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "会话 ID:"))
		}
	}
	return ""
}

// errSemanticSSHRemoteHomeUser is "~otheruser". SFTP and this tool expand only
// the login home ("~" and "~/"). The wash rejects the other form before the
// grant is spent.
var errSemanticSSHRemoteHomeUser = errors.New("trusted_ssh_remote_home_user")

func semanticSSHRemotePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\x00") {
		return "", fmt.Errorf("trusted_ssh_remote_path_required")
	}
	slash := filepath.ToSlash(raw)
	if slash == "~" || strings.HasPrefix(slash, "~/") {
		cleaned := cleanHomeRelativeRemotePath(slash)
		if cleaned == "" {
			return "", fmt.Errorf("trusted_ssh_remote_path_required")
		}
		return cleaned, nil
	}
	if strings.HasPrefix(slash, "~") {
		return "", errSemanticSSHRemoteHomeUser
	}
	cleaned := path.Clean(slash)
	// "/" is the remote filesystem, not one document or one output directory.
	if cleaned == "." || cleaned == "" || cleaned == "/" {
		return "", fmt.Errorf("trusted_ssh_remote_path_required")
	}
	return cleaned, nil
}

// cleanHomeRelativeRemotePath keeps the leading "~/" so a later ".." is
// resolved against the login home. path.Clean("~/../x") would drop the tilde
// and leave a path relative to the SFTP working directory.
func cleanHomeRelativeRemotePath(slash string) string {
	if slash == "~" || slash == "~/" {
		return "~"
	}
	// "~//foo" leaves a leading slash on the remainder. Cleaning that as a
	// rooted path would resolve ".." against "/" instead of the login home.
	rest := path.Clean(strings.TrimLeft(strings.TrimPrefix(slash, "~/"), "/"))
	if rest == "." || rest == "" {
		return "~"
	}
	return "~/" + rest
}

// resolveTrustedSSHLocalPath accepts an absolute destination, or a relative
// path under the caller's working directory. The copy is the user's own
// computer, so an absolute path is not confined to the project workspace.
// A trailing separator is restored after Clean: Clean would otherwise turn a
// new directory path into a file path before the destination join.
func (h *IMMessageHandler) resolveTrustedSSHLocalPath(userID, localPath string) (string, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" || strings.ContainsAny(localPath, "\r\n\x00") {
		return "", fmt.Errorf("trusted_ssh_local_path_required")
	}
	dirIntent := strings.HasSuffix(localPath, "/") || strings.HasSuffix(localPath, `\`)
	var resolved string
	if filepath.IsAbs(localPath) {
		resolved = filepath.Clean(localPath)
	} else {
		base := ""
		if h != nil {
			base = strings.TrimSpace(h.effectiveWorkingDirForUser(userID))
		}
		if base == "" {
			return "", fmt.Errorf("trusted_ssh_local_path_required: pass an absolute local_path")
		}
		resolved = filepath.Clean(filepath.Join(base, localPath))
	}
	if dirIntent && !strings.HasSuffix(resolved, string(os.PathSeparator)) {
		resolved += string(os.PathSeparator)
	}
	return resolved, nil
}

// semanticSSHLocalDestination matches scp: an existing directory, or a path
// that ends in a separator, receives the remote base name.
func semanticSSHLocalDestination(remotePath, localPath string) string {
	// A previous attempt created this directory and stopped before the copy
	// finished. Keep writing into it. The mark is filed under the cleaned
	// path, so the retry has to use that same path. Another remote path still
	// lands inside.
	markLocal := filepath.Clean(strings.TrimRight(localPath, `/\`))
	if remote.SameIncompleteDownload(markLocal, remotePath) {
		return markLocal
	}
	slashEnded := strings.HasSuffix(localPath, "/") || strings.HasSuffix(localPath, `\`)
	info, err := os.Stat(localPath)
	if slashEnded || (err == nil && info.IsDir()) {
		base := path.Base(path.Clean(filepath.ToSlash(remotePath)))
		// "~" is the login home. Its real directory name is not known until
		// the session expands it, so do not create a local file named "~".
		if base == "." || base == "/" || base == "" || base == "~" {
			return localPath
		}
		return filepath.Join(strings.TrimRight(localPath, `/\`), base)
	}
	return localPath
}

func (h *IMMessageHandler) downloadTrustedSSH(sessionID, remotePath, localPath string) string {
	if h == nil {
		return "下载失败: trusted_ssh_session_unavailable"
	}
	mgr := h.ensureSSHManager()
	resolved, finalLocal, result, err := mgr.SFTPDownloadForSession(sessionID, localPath, remotePath, semanticSSHLocalDestination)
	if resolved != "" {
		remotePath = resolved
	}
	if finalLocal != "" {
		localPath = finalLocal
	}
	if err != nil || result == nil {
		reason := "trusted_ssh_session_unavailable"
		if err != nil {
			reason = err.Error()
		}
		return fmt.Sprintf("下载失败: %s\nremote_path=%s\nlocal_path=%s", reason, remotePath, localPath)
	}
	return semanticSSHDownloadReceiptTransfer(remotePath, localPath, result)
}

const (
	semanticSSHHashMaxBytes int64 = 512 << 20
	semanticSSHHashMaxFiles       = 32
)

func semanticSSHDownloadReceipt(remotePath, localPath, transfer string) string {
	return semanticSSHDownloadReceiptBytes(remotePath, localPath, transfer, -1)
}

// semanticSSHDownloadReceiptBytes reports a finished copy. transferred < 0
// skips the size check (callers that only have the human-readable summary).
// The first line stays a fixed marker so a path cannot change the outcome.
func semanticSSHDownloadReceiptBytes(remotePath, localPath, transfer string, transferred int64) string {
	info, err := os.Stat(strings.TrimRight(localPath, `/\`))
	if err != nil {
		if transferred >= 0 {
			return semanticSSHDownloadFailure(fmt.Sprintf("local path is missing after the copy: %s", err.Error()), remotePath, localPath)
		}
		return semanticSSHDownloadHeader(remotePath, localPath, transfer)
	}
	if info.Mode().IsRegular() {
		if transferred >= 0 && info.Size() != transferred {
			return semanticSSHDownloadFailure(fmt.Sprintf("transferred %d bytes but local file is %d bytes", transferred, info.Size()), remotePath, localPath)
		}
		var b strings.Builder
		b.WriteString(semanticSSHDownloadHeader(remotePath, localPath, transfer))
		if info.Size() > semanticSSHHashMaxBytes {
			fmt.Fprintf(&b, "\nlocal_bytes=%d sha256=omitted exceeds %d bytes", info.Size(), semanticSSHHashMaxBytes)
			return b.String()
		}
		sum, hashErr := semanticSSHFileSHA256(localPath)
		if hashErr != nil {
			fmt.Fprintf(&b, "\nlocal_bytes=%d sha256=omitted %s", info.Size(), hashErr.Error())
			return b.String()
		}
		fmt.Fprintf(&b, "\nlocal_bytes=%d sha256=%s", info.Size(), sum)
		return b.String()
	}
	return semanticSSHDownloadHeader(remotePath, localPath, transfer)
}

// semanticSSHDownloadReceiptTransfer checks the files this transfer wrote.
// Summing the destination directory treats files that were already there as
// part of the copy and reports a finished download as failed.
func semanticSSHDownloadReceiptTransfer(remotePath, localPath string, result *remote.SFTPTransferResult) string {
	if result == nil {
		return semanticSSHDownloadFailure("empty transfer", remotePath, localPath)
	}
	extra, err := semanticSSHVerifyWritten(localPath, result.Written, result.Bytes)
	if err != nil {
		return semanticSSHDownloadFailure(err.Error(), remotePath, localPath)
	}
	return semanticSSHDownloadHeader(remotePath, localPath, result.String()) + extra
}

func semanticSSHVerifyWritten(localPath string, written []remote.SFTPWrittenFile, transferred int64) (string, error) {
	var total int64
	var extra strings.Builder
	var hashed int
	var omitted int
	for _, file := range written {
		info, err := os.Stat(file.Path)
		if err != nil {
			return "", fmt.Errorf("local file missing after copy: %s", file.Path)
		}
		if !info.Mode().IsRegular() || info.Size() != file.Bytes {
			return "", fmt.Errorf("transferred %d bytes but %s is %d bytes", file.Bytes, file.Path, info.Size())
		}
		total += file.Bytes
		label, err := semanticSSHWrittenLabel(localPath, file.Path)
		if err != nil {
			return "", err
		}
		if info.Size() > semanticSSHHashMaxBytes || hashed >= semanticSSHHashMaxFiles {
			omitted++
			if info.Size() > semanticSSHHashMaxBytes {
				if label == "" {
					fmt.Fprintf(&extra, "\nlocal_bytes=%d sha256=omitted exceeds %d bytes", info.Size(), semanticSSHHashMaxBytes)
				} else {
					fmt.Fprintf(&extra, "\nfile=%s local_bytes=%d sha256=omitted exceeds %d bytes", label, info.Size(), semanticSSHHashMaxBytes)
				}
			}
			continue
		}
		sum, hashErr := semanticSSHFileSHA256(file.Path)
		if hashErr != nil {
			omitted++
			if label == "" {
				fmt.Fprintf(&extra, "\nlocal_bytes=%d sha256=omitted %s", info.Size(), hashErr.Error())
			} else {
				fmt.Fprintf(&extra, "\nfile=%s local_bytes=%d sha256=omitted %s", label, info.Size(), hashErr.Error())
			}
			continue
		}
		hashed++
		if label == "" {
			fmt.Fprintf(&extra, "\nlocal_bytes=%d sha256=%s", info.Size(), sum)
		} else {
			fmt.Fprintf(&extra, "\nfile=%s local_bytes=%d sha256=%s", label, info.Size(), sum)
		}
	}
	if total != transferred {
		return "", fmt.Errorf("transferred %d bytes but written files total %d bytes", transferred, total)
	}
	if omitted > 0 {
		fmt.Fprintf(&extra, "\nsha256_omitted=%d", omitted)
	}
	return extra.String(), nil
}

// semanticSSHWrittenLabel returns a path relative to the destination directory.
// An empty label means the written path is the destination file itself.
func semanticSSHWrittenLabel(localPath, writtenPath string) (string, error) {
	localPath = filepath.Clean(strings.TrimRight(localPath, `/\`))
	writtenPath = filepath.Clean(writtenPath)
	if writtenPath == localPath {
		return "", nil
	}
	rel, err := filepath.Rel(localPath, writtenPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("written file is outside the destination")
	}
	return filepath.ToSlash(rel), nil
}

func semanticSSHDownloadHeader(remotePath, localPath, transfer string) string {
	return fmt.Sprintf("下载完成:\nremote_path=%s\nlocal_path=%s\n%s", remotePath, localPath, transfer)
}

func semanticSSHDownloadFailure(reason, remotePath, localPath string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "copy failed"
	}
	return fmt.Sprintf("下载失败: %s\nremote_path=%s\nlocal_path=%s", reason, remotePath, localPath)
}

func semanticSSHFileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
