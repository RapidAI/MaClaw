package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
)

// local_task is how a desktop bot creates programming tasks on the person's
// own MaClaw app. The call is that app's MCP tools. It travels over the
// app's outbound Hub link, so it works from a public server and from an
// intranet MaclawSrv without dialing the computer's MCP port.

const localTaskToolName = "local_task"

const localTaskPrompt = "local_task creates and follows programming tasks on the person's own MaClaw app, using that app's MCP service. " +
	"The call rides the app's outbound connection to Hub. It works from the public server and from an intranet MaclawSrv; do not dial the computer's MCP address or open a tunnel to its port. " +
	"The app must be signed in to this Hub, and the person must turn MCP on in the app. If it is offline or MCP is off, say so. " +
	"mode coding_dev with working_dir is a task in a folder on that computer. mode remote_coding_dev is a task on a machine that computer can SSH to. The call returns when the task has started; poll with get_task. " +
	"Work that should run in this cloud desktop stays on the desktop and repo tools."

var localTaskToolNames = []string{
	"list_models", "create_task", "get_task", "list_tasks", "send_command",
	"stop_task", "get_request", "retry_steps", "read_log",
}

func localTaskAvailable(request agentruntime.TurnRequest) bool {
	if strings.TrimSpace(os.Getenv("MACLAW_LOCAL_RELAY")) == "off" {
		return false
	}
	if !desktopHubConfigured() && !localRelayTestEnabled {
		return false
	}
	// A Hub bot can schedule the person's computer even with no cloud desktop.
	// A digital-employee turn is not that bot.
	if desktopHubBotTurn(request) {
		return true
	}
	switch botPhaseFromTurn(request) {
	case "plan", "execute":
		return true
	default:
		return false
	}
}

func (localRelayModule) ContributePrompt(_ context.Context, request agentruntime.TurnRequest) (string, error) {
	if !localTaskAvailable(request) {
		return "", nil
	}
	return localTaskPrompt, nil
}

func localTaskTool() agentruntime.ToolDefinition {
	return agentruntime.ToolDefinition{
		Name: localTaskToolName,
		Description: "Create and follow programming tasks on the person's own MaClaw app (the computer where that app is open). " +
			"This calls that app's MCP tools over the app's outbound connection to Hub. It works when this bot runs on the public server and when MaclawSrv is installed on the person's intranet: never dial the computer's MCP address, and do not open a tunnel to its port. Loopback, a private IP, and a firewall do not matter. " +
			"The app must be signed in to the same Hub, and the person must turn the MCP service on in the app. If the app is offline or MCP is off, the result says so and nothing is created. " +
			"tool=create_task starts a task. mode=coding_dev with working_dir is a programming task in a folder on that computer. mode=remote_coding_dev with ssh_host, ssh_user and remote_work_dir is a programming task on a machine that computer can reach. mode=chat is a conversation. prompt is the instruction. Omit mode when a working_dir is set and it is treated as a programming task. " +
			"The call returns project_path and run_id as soon as the task has started, even when wait is set; then tool=get_task with that project_path (run_id optional). A timed-out call may still have started the task, so check get_task before creating another. tool=list_tasks lists them. tool=send_command sends a follow-up or a confirmation, and must pass confirmation_id from get_task when commands are present. tool=stop_task stops the current round. tool=list_models lists models on that computer. tool=get_request checks a client_request_id after a timeout. tool=retry_steps retries failed steps. tool=read_log reads one round's log. " +
			"Pass client_request_id on create_task so a retry does not create a second task. " +
			"Use this when the work should run on the person's computer or on a machine only that computer can reach. Work that belongs in this cloud desktop stays on the desktop and repo tools.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool": map[string]any{
					"type":        "string",
					"enum":        localTaskToolNames,
					"description": "MCP tool on the person's MaClaw app. Empty lists the tools.",
				},
				"arguments": map[string]any{
					"type":        "object",
					"description": "Arguments for that MCP tool. create_task fields may also be set at the top level.",
				},
				"name":              map[string]any{"type": "string", "description": "create_task: task name. Empty uses the first line of prompt."},
				"prompt":            map[string]any{"type": "string", "description": "create_task: what to do. Empty creates the task and does not start it. send_command: the follow-up text."},
				"mode":              map[string]any{"type": "string", "description": "create_task: chat, coding_dev, or remote_coding_dev."},
				"working_dir":       map[string]any{"type": "string", "description": "create_task: absolute folder on the person's computer for a local programming task."},
				"remote_work_dir":   map[string]any{"type": "string", "description": "create_task: remote directory for remote_coding_dev."},
				"ssh_host":          map[string]any{"type": "string", "description": "create_task: SSH host for a remote programming task."},
				"ssh_user":          map[string]any{"type": "string", "description": "create_task: SSH user."},
				"ssh_password":      map[string]any{"type": "string", "description": "create_task: SSH password for this connection only. It is not written into the task result."},
				"ssh_port":          map[string]any{"type": "integer", "description": "create_task: SSH port. Omit for 22."},
				"provider":          map[string]any{"type": "string", "description": "create_task or send_command: provider id or name on that computer."},
				"model":             map[string]any{"type": "string", "description": "create_task or send_command: model id. Naming a model requires provider."},
				"project_path":      map[string]any{"type": "string", "description": "get_task, send_command, stop_task, read_log, retry_steps: project_path from create_task."},
				"run_id":            map[string]any{"type": "string", "description": "get_task or read_log: one round. Omit for the current round."},
				"command":           map[string]any{"type": "string", "description": "send_command: the command text, including a confirmation from get_task."},
				"confirmation_id":   map[string]any{"type": "string", "description": "send_command or retry_steps: confirmation_id from the latest get_task."},
				"client_request_id": map[string]any{"type": "string", "description": "create_task, send_command, retry_steps: caller id. The same id is not run twice. get_request looks it up."},
				"wait":              map[string]any{"type": "boolean", "description": "Ignored. The Hub link cannot wait out a coding run. The call returns when the task has started; poll get_task."},
			},
		},
	}
}

func (m localRelayModule) invokeLocalTask(ctx context.Context, request agentruntime.TurnRequest, args map[string]any) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	scope, _, err := desktopScopeForTurn(request)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(scope.RunID) == "" {
		scope.RunID = strings.TrimSpace(request.Scope.RunID)
	}
	tool, payload, secret, err := localTaskPayload(args)
	if err != nil {
		return "", err
	}
	if botPhaseFromTurn(request) == "plan" && localTaskMutating(tool) {
		return "", fmt.Errorf("plan step: local_task can list and read tasks now; %s runs in the execute step", tool)
	}
	text, err := localTaskCall(ctx, scope, tool, payload, secret)
	if err != nil {
		return "", err
	}
	return localTaskRedact(text, secret), nil
}

func localTaskMutating(tool string) bool {
	switch tool {
	case "create_task", "send_command", "stop_task", "cancel_task", "retry_steps":
		return true
	}
	return false
}

func localTaskCanonical(tool string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "", "tools":
		return "", nil
	case "list_models", "models":
		return "list_models", nil
	case "create_task", "create":
		return "create_task", nil
	case "get_task", "get":
		return "get_task", nil
	case "list_tasks":
		return "list_tasks", nil
	case "send_command", "send":
		return "send_command", nil
	case "stop_task", "stop", "cancel_task", "cancel":
		return "stop_task", nil
	case "get_request":
		return "get_request", nil
	case "retry_steps", "retry":
		return "retry_steps", nil
	case "read_log", "log":
		return "read_log", nil
	default:
		return "", fmt.Errorf("tool must be one of %s", strings.Join(localTaskToolNames, ", "))
	}
}

func localTaskPayload(args map[string]any) (string, string, string, error) {
	tool, err := localTaskCanonical(desktopArg(args, "tool"))
	if err != nil {
		return "", "", "", err
	}
	merged := map[string]any{}
	if raw, ok := args["arguments"]; ok && raw != nil {
		switch typed := raw.(type) {
		case map[string]any:
			for k, v := range typed {
				merged[k] = v
			}
		case string:
			text := strings.TrimSpace(typed)
			if text != "" && text != "null" {
				var obj map[string]any
				if err := json.Unmarshal([]byte(text), &obj); err != nil {
					return "", "", "", fmt.Errorf("arguments must be a JSON object")
				}
				for k, v := range obj {
					merged[k] = v
				}
			}
		default:
			return "", "", "", fmt.Errorf("arguments must be a JSON object")
		}
	}
	for _, key := range []string{
		"name", "prompt", "instruction", "mode", "working_dir", "work_dir", "remote_work_dir",
		"ssh_host", "ssh_user", "ssh_password", "ssh_port", "provider", "model",
		"project_path", "run_id", "command", "text", "confirmation_id", "client_request_id", "wait",
	} {
		localTaskCopyArg(merged, args, key)
	}
	if tool == "" && len(merged) > 0 {
		return "", "", "", fmt.Errorf("tool must be one of %s", strings.Join(localTaskToolNames, ", "))
	}
	// The password is only for creating a remote task. Other calls must not
	// carry it across Hub. wait is cleared because the relay stream ends at
	// 10 minutes while a coding run keeps going; waiting would look like a
	// failure and invite a second create.
	secret, _ := merged["ssh_password"].(string)
	secret = strings.TrimSpace(secret)
	if tool != "create_task" {
		delete(merged, "ssh_password")
	}
	switch tool {
	case "create_task", "send_command":
		merged["wait"] = false
	default:
		delete(merged, "wait")
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return "", "", "", err
	}
	return tool, string(raw), secret, nil
}

func localTaskCopyArg(dst map[string]any, args map[string]any, key string) {
	if _, exists := dst[key]; exists {
		return
	}
	value, ok := args[key]
	if !ok || value == nil {
		return
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
		return
	}
	dst[key] = value
}

func localTaskCall(ctx context.Context, scope agentruntime.Scope, tool, payload, secret string) (string, error) {
	// Hub rejects the relay body past 256KB. Fail here so the model hears
	// why, instead of a generic rejection after the password has been posted.
	if len(payload) > 200000 {
		return "", fmt.Errorf("the task request is too large to send over Hub. Shorten the prompt and try again")
	}
	reply, err := localRelayCall(ctx, scope, "mcp_call", map[string]any{
		"server": mcpHostRelayServerName,
		"tool":   tool,
		"args":   payload,
	})
	if err != nil {
		if localrelay.CodeOf(err) == localrelay.CodeTimeout {
			return "", fmt.Errorf("%w. The work may already be running on the computer; use get_task before starting another", err)
		}
		return "", err
	}
	return localTaskRedact(strings.TrimSpace(reply.Text), secret), nil
}

// localTaskRedact hides a long secret that a result echoed back. A short
// secret is left alone: it can sit inside an ordinary word such as a status.
func localTaskRedact(text, secret string) string {
	if len([]rune(secret)) < 8 || text == "" || !strings.Contains(text, secret) {
		return text
	}
	return strings.ReplaceAll(text, secret, "••••")
}

// mcpHostRelayServerName is the server id the GUI treats as its own MCP
// task service. It is not a stdio server and not a URL.
const mcpHostRelayServerName = "maclaw"
