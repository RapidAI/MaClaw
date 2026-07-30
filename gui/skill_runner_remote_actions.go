package main

import (
	"context"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// remoteSkillRunOwner resolves the project-owned remote coding context shared
// by all remote Skill actions.  Checklist updates need this context for UI
// ownership, but intentionally do not require a live SSH session: they should
// still be able to report a reconnect-needed failure to the user.
func (r *SkillRunner) remoteSkillRunOwner(runID string) (*IMMessageHandler, string, error) {
	if r == nil || r.executor == nil || r.executor.app == nil {
		return nil, "", fmt.Errorf("remote skill actions require the GUI app and an active remote-coding task")
	}
	r.mu.RLock()
	run, ok := r.runs[runID]
	ownerID := ""
	if ok && run != nil {
		ownerID = strings.TrimSpace(run.status.OwnerID)
	}
	r.mu.RUnlock()
	if ownerID == "" {
		return nil, "", fmt.Errorf("remote skill actions require a project-scoped run owner")
	}
	hub := r.executor.app.ensureHubClient()
	if hub == nil {
		return nil, "", fmt.Errorf("remote skill actions require the AI assistant connection")
	}
	handler := hub.ensureIMHandler()
	if handler == nil {
		return nil, "", fmt.Errorf("remote skill actions require the message handler")
	}
	mem := handler.getStickyCodingWorkbenchMemory(ownerID)
	if strings.TrimSpace(mem.Kind) != "remote" {
		return nil, "", fmt.Errorf("remote skill actions require an active remote-coding task for this project")
	}
	return handler, ownerID, nil
}

// remoteSkillRunBinding resolves a skill run to exactly the SSH connection
// already selected by its project-scoped remote-coding task.  In particular,
// do not accept a session_id from a learned skill: that would allow a skill to
// cross project/session boundaries merely by naming another live connection.
func (r *SkillRunner) remoteSkillRunBinding(runID string) (*IMMessageHandler, string, string, error) {
	handler, ownerID, err := r.remoteSkillRunOwner(runID)
	if err != nil {
		return nil, "", "", err
	}
	mem := handler.getStickyCodingWorkbenchMemory(ownerID)
	sessionID := strings.TrimSpace(mem.RemoteSessionID)
	if sessionID == "" || !handler.sshSessionAlive(sessionID) {
		return nil, "", "", fmt.Errorf("remote skill actions require the current project's SSH session; reconnect the remote-coding task and retry")
	}
	workDir := strings.TrimSpace(mem.RemoteWorkDir)
	if workDir == "" {
		workDir = strings.TrimSpace(mem.RemoteProjectDir)
	}
	if workDir == "" {
		return nil, "", "", fmt.Errorf("remote skill actions require the current project's remote work directory")
	}
	return handler, sessionID, workDir, nil
}

func (r *SkillRunner) executeRemoteSkillStep(ctx context.Context, runID string, step corelib.NLSkillStep) (string, error) {
	// Unlike local bash, SSH transport waits cannot currently be interrupted by a
	// context once a command has been submitted.  Still reject a step which has
	// already timed out or been cancelled, so a queued/retried step cannot start
	// a new remote command after its Skill run has stopped.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if classifySkillStepAction(step.Action) == skillStepActionTodoWrite {
		return r.executeRemoteSkillTodoWrite(runID, step)
	}
	handler, sessionID, workDir, err := r.remoteSkillRunBinding(runID)
	if err != nil {
		return "", err
	}
	switch classifySkillStepAction(step.Action) {
	case skillStepActionSSHBash:
		command, _ := step.Params["command"].(string)
		command = strings.TrimSpace(command)
		if command == "" {
			return "", fmt.Errorf("ssh_bash requires command parameter")
		}
		raw := handler.sshExec(map[string]interface{}{
			"session_id":   sessionID,
			"command":      remoteSkillCommandInWorkDir(workDir, command),
			"wait_seconds": remoteSkillBashWaitSeconds(step.Params),
		})
		if remoteCodingToolOutcome(raw) != "success" {
			return "", fmt.Errorf("ssh_bash failed: %s", compactRemoteSSHError(raw))
		}
		return raw, nil

	case skillStepActionSSHListDir:
		path, _ := step.Params["path"].(string)
		path = remoteSkillResolvePath(workDir, path)
		// Keep the command's exit status even though sshExec itself treats a
		// completed PTY command as transport success.  Use a child shell so an
		// unusual path/name cannot influence the marker wrapper.
		raw := handler.sshExec(map[string]interface{}{
			"session_id":   sessionID,
			"command":      remoteSkillCommandInWorkDir(workDir, fmt.Sprintf("ls -la -- %s 2>&1", remoteShellQuote(path))),
			"wait_seconds": remoteSkillWaitSeconds(step.Params, 20),
		})
		if remoteCodingToolOutcome(raw) != "success" {
			return "", fmt.Errorf("ssh_list_dir failed: %s", compactRemoteSSHError(raw))
		}
		return raw, nil

	case skillStepActionSSHReadFile:
		path, _ := step.Params["path"].(string)
		path = remoteSkillResolvePath(workDir, path)
		if path == "" {
			return "", fmt.Errorf("ssh_read_file requires path parameter")
		}
		offset := remoteSkillPositiveInt(step.Params["offset"], 1, 1, 1_000_000)
		limit := remoteSkillPositiveInt(step.Params["limit"], 500, 1, 2_000)
		// The reader reports an EOF/binary result as ordinary output. Wrap it
		// too, otherwise a missing file can look like a successful read merely
		// because the PTY returned a prompt.
		raw := handler.sshExec(map[string]interface{}{
			"session_id":   sessionID,
			"command":      remoteSkillCommandInWorkDir(workDir, remoteReadFileRangePythonCommand(path, offset, limit)),
			"wait_seconds": remoteSkillWaitSeconds(step.Params, 20),
		})
		if remoteCodingToolOutcome(raw) != "success" {
			return "", fmt.Errorf("ssh_read_file failed: %s", compactRemoteSSHError(raw))
		}
		content := extractRemoteReadPreviewContent(raw)
		// A line limit alone does not protect the desktop process from a file
		// with a few exceptionally long lines. Keep the same practical payload
		// ceiling as the remote editor bridge.
		if utf8.RuneCountInString(content) > 400000 {
			content = string([]rune(content)[:400000]) + "\n... (remote read truncated)"
		}
		return content, nil

	default:
		return "", fmt.Errorf("unknown remote skill action: %s", step.Action)
	}
}

func (r *SkillRunner) executeRemoteSkillTodoWrite(runID string, step corelib.NLSkillStep) (string, error) {
	handler, ownerID, err := r.remoteSkillRunOwner(runID)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(step.Params)
	if err != nil {
		return "", fmt.Errorf("encode todo_write parameters: %w", err)
	}
	r.mu.RLock()
	run := r.runs[runID]
	r.mu.RUnlock()
	if run == nil {
		return "", fmt.Errorf("skill run %q not found", runID)
	}
	result, outcome := executeCodingAgentTodoWrite(&run.todos, string(payload), nil, func(items []codingAgentTodoItem) {
		publishCodingAgentTodosToUI(handler, ownerID, items)
	})
	if outcome != codingToolOutcomeSuccess {
		return "", fmt.Errorf("%s", result)
	}
	return result, nil
}

func remoteSkillWaitSeconds(params map[string]interface{}, fallback int) float64 {
	// Do not allow inspection actions to use the SSH transport's default short
	// wait when a learned Skill explicitly supplies an invalid/zero value.
	// Unlike ssh_bash these commands are normally quick, so 20 seconds remains
	// a reasonable synchronous lower bound while avoiding an accidental zero.
	return float64(remoteSkillWaitSecondsAtLeast(params, fallback, 1))
}

// sshExec promotes commands it recognizes as long-running to a background
// task when wait_seconds is 30 or lower. A Skill step needs the final exit
// marker synchronously, so retain the caller's longer wait but lift shorter
// values just above that transport threshold.
func remoteSkillBashWaitSeconds(params map[string]interface{}) float64 {
	return float64(remoteSkillWaitSecondsAtLeast(params, 31, 31))
}

func remoteSkillWaitSecondsAtLeast(params map[string]interface{}, fallback, min int) int {
	wait := remoteSkillPositiveInt(params["wait_seconds"], fallback, 1, 600)
	if wait < min {
		return min
	}
	return wait
}

// remoteSkillCommandInWorkDir preserves the user command's exit status even
// when the interactive SSH transport itself reports output normally. Without
// the marker, a failed `make` or test command could be recorded as a successful
// skill step simply because it printed a useful error message.
func remoteSkillCommandInWorkDir(workDir, command string) string {
	// Reuse the remote coding wrapper rather than interpolating the skill's
	// command into an outer shell script. It passes the complete command as one
	// shell-quoted argument to a child `sh -lc`, so malformed quotes, `exit`,
	// `exec`, and shell-option changes cannot bypass the outer exit marker.
	return remoteBashCommandWithExitMarker(workDir, command)
}

// remoteSkillResolvePath uses POSIX path semantics because the destination is
// always an SSH host, even when the desktop GUI itself is running on Windows.
// Absolute paths remain supported for diagnostic skills; relative paths are
// consistently rooted at the active remote work directory.
func remoteSkillResolvePath(workDir, rawPath string) string {
	workDir = strings.TrimSpace(workDir)
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return workDir
	}
	if strings.HasPrefix(rawPath, "/") {
		return pathpkg.Clean(rawPath)
	}
	if workDir == "" {
		return pathpkg.Clean(rawPath)
	}
	return pathpkg.Join(workDir, rawPath)
}

func remoteSkillPositiveInt(value interface{}, fallback, min, max int) int {
	n := fallback
	switch v := value.(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64:
		n = int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			n = int(parsed)
		}
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
