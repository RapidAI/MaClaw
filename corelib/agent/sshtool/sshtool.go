// Package sshtool provides the core SSH tool dispatch logic shared between
// GUI and TUI. Both GUI's im_ssh_tools.go and TUI's agent_tools_ssh.go
// implement the same SSH actions by delegating to remote.SSHSessionManager.
// The difference is GUI has background-loop registration and config loading.
// Those platform-specific behaviours are injected via SSHToolDeps callbacks.
package sshtool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// SSHToolDeps holds the dependencies needed by SSH tool handlers.
type SSHToolDeps struct {
	Manager   *remote.SSHSessionManager
	BGTaskMgr *remote.SSHBackgroundTaskManager

	// HostLoader returns the list of pre-configured SSH hosts from config.
	HostLoader func() []corelib.SSHHostEntry

	// OnConnected is called after a new SSH session is successfully created.
	// GUI uses this to register a background-loop entry; TUI may leave it nil.
	OnConnected func(session *remote.SSHManagedSession, cfg remote.SSHHostConfig)

	// OnClosed is called when an SSH session is closed or removed.
	// GUI uses this to complete the background-loop entry; TUI may leave it nil.
	OnClosed func(sessionID string)

	// OnExecIteration is called after each exec / exec_background to bump
	// the background-loop iteration counter. May be nil.
	OnExecIteration func(sessionID string)
}

// ToolSSH dispatches SSH tool calls by the "action" parameter.
func ToolSSH(deps SSHToolDeps, args map[string]interface{}) string {
	action := strArg(args, "action")
	switch action {
	case "connect":
		return SSHConnect(deps, args)
	case "exec":
		return SSHExec(deps, args)
	case "exec_background":
		return SSHExecBackground(deps, args)
	case "check_task":
		return SSHCheckTask(deps, args)
	case "list_tasks":
		return SSHListTasks(deps)
	case "kill_task":
		return SSHKillTask(deps, args)
	case "sudo_prepare":
		return SSHSudoPrepare(deps, args)
	case "upload":
		return SSHUpload(deps, args)
	case "download":
		return SSHDownload(deps, args)
	case "list":
		return SSHList(deps)
	case "close":
		return SSHClose(deps, args)
	case "close_all":
		return SSHCloseAll(deps)
	default:
		return fmt.Sprintf("未知 SSH 操作: %s（支持: connect/exec/exec_background/check_task/list_tasks/kill_task/sudo_prepare/upload/download/list/close/close_all）", action)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]interface{}, key string, defaultVal int) int {
	if args == nil {
		return defaultVal
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return defaultVal
}

// ResolveSSHHostByLabel looks up a pre-configured SSH host by label.
func ResolveSSHHostByLabel(hosts []corelib.SSHHostEntry, label string) *corelib.SSHHostEntry {
	label = strings.ToLower(strings.TrimSpace(label))
	for i := range hosts {
		if strings.ToLower(hosts[i].Label) == label {
			return &hosts[i]
		}
	}
	// Fuzzy fallback: label contains keyword.
	for i := range hosts {
		if strings.Contains(strings.ToLower(hosts[i].Label), label) {
			return &hosts[i]
		}
	}
	return nil
}

// FindRunningSSHSession looks for an existing SSH session matching the
// given hostID (user@host:port) or label that is still running.
func FindRunningSSHSession(mgr *remote.SSHSessionManager, hostID, label string) *remote.SSHManagedSession {
	for _, s := range mgr.List() {
		summary := s.GetSummary()
		if summary.Status != string(remote.SessionRunning) {
			continue
		}
		if summary.HostID == hostID || (label != "" && summary.HostLabel == label) {
			return s
		}
	}
	return nil
}
