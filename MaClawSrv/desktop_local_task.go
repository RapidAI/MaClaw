package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
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
	"mode coding_dev with working_dir is a task in a folder on that computer. mode remote_coding_dev is a task on a machine that computer can SSH to. location cloud creates a cloud agent. cloud_workspace_id is only a working directory for a chat task on that computer, not a location. location remote_device with grant_id creates the task on an authorized computer. The call returns when the task has started; poll with get_task. " +
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
			"tool=create_task starts a task. location=local is that computer. location=cloud creates a cloud agent (chat or coding_dev) and does not take grant_id, cloud_workspace_id, provider, or model. cloud_workspace_id is only the working directory of a chat task on that computer, not a location and not a cloud agent. location=remote_device with grant_id creates the task on an authorized computer; mode is chat or coding_dev, and working_dir is a folder on that computer. Omit location and a grant_id means remote_device, otherwise local. mode=coding_dev with working_dir is a programming task in a folder on that computer. mode=remote_coding_dev with ssh_host, ssh_user and remote_work_dir is a programming task on a machine that computer can reach by SSH. mode=chat is a conversation. prompt is the instruction. Omit mode when a working_dir is set and location is local and it is treated as a programming task. Do not pass provider or model for a remote device or a cloud agent. " +
			"The call returns project_path and run_id as soon as the task has started, even when wait is set; then tool=get_task with that project_path (run_id optional). A timed-out call may still have started the task: use get_request with the same client_request_id when one was sent, otherwise list_tasks, before creating another. tool=list_tasks lists them. tool=send_command sends a follow-up or a confirmation, and must pass confirmation_id from get_task when commands are present. tool=stop_task stops the current round. tool=list_models lists models on that computer. tool=get_request checks a client_request_id after a timeout. tool=retry_steps retries failed steps and needs step_ids (T5, 5, or 步骤 5) plus confirmation_id from get_task. tool=read_log reads one page of a round; pass cursor from the previous next_cursor for the next page. expected_status_version and dry_run apply to send_command and retry_steps. " +
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
					"description": "Arguments for that MCP tool. The same fields may be set at the top level; a value inside arguments wins.",
				},
				"name":                    map[string]any{"type": "string", "description": "create_task: task name. Empty uses the first line of prompt."},
				"prompt":                  map[string]any{"type": "string", "description": "create_task: what to do. Empty creates the task and does not start it. send_command: the follow-up text."},
				"mode":                    map[string]any{"type": "string", "description": "create_task: chat, coding_dev, or remote_coding_dev. A cloud agent accepts chat or coding_dev. A remote device accepts chat or coding_dev."},
				"location":                map[string]any{"type": "string", "description": "create_task: local, cloud, or remote_device. cloud is a cloud agent. Omit to infer remote_device from grant_id, otherwise local. cloud_workspace_id does not select a location."},
				"cloud_workspace_id":      map[string]any{"type": "string", "description": "create_task: working directory of a chat task on that computer, as a cloud workspace id. Not a location. Do not pass it with location=cloud."},
				"grant_id":                map[string]any{"type": "string", "description": "create_task: remote-control grant id. Required when location is remote_device. A cloud agent opens its own grant."},
				"working_dir":             map[string]any{"type": "string", "description": "create_task: absolute folder on the person's computer for a local programming task. For a remote device, a folder on that computer."},
				"remote_work_dir":         map[string]any{"type": "string", "description": "create_task: remote directory for remote_coding_dev."},
				"ssh_host":                map[string]any{"type": "string", "description": "create_task: SSH host for a remote programming task."},
				"ssh_user":                map[string]any{"type": "string", "description": "create_task: SSH user."},
				"ssh_password":            map[string]any{"type": "string", "description": "create_task: SSH password for this connection only. It is not written into the task result."},
				"ssh_port":                map[string]any{"type": "integer", "description": "create_task: SSH port. Omit for 22."},
				"provider":                map[string]any{"type": "string", "description": "create_task: provider id or name on that computer. send_command: a different value changes the model for the new round. The same value as provider_effective keeps the current model."},
				"model":                   map[string]any{"type": "string", "description": "create_task or send_command: model id. Naming a model requires provider. The same value as model_effective keeps the current model."},
				"project_path":            map[string]any{"type": "string", "description": "get_task, send_command, stop_task, read_log, retry_steps: project_path from create_task. One entry in tasks, the list_tasks field, is used when this is empty."},
				"run_id":                  map[string]any{"type": "string", "description": "get_task or read_log: one round. Omit for the current round."},
				"command":                 map[string]any{"type": "string", "description": "send_command: the command text. prompt is a follow-up you write. When command and prompt are empty, one entry in commands is used."},
				"confirmation_id":         map[string]any{"type": "string", "description": "send_command or retry_steps: confirmation_id from the latest get_task."},
				"client_request_id":       map[string]any{"type": "string", "description": "create_task, send_command, retry_steps: caller id. The same id is not run twice. get_request looks it up."},
				"wait":                    map[string]any{"type": "boolean", "description": "Ignored. The Hub link cannot wait out a coding run. The call returns when the task has started; poll get_task."},
				"step_ids":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "retry_steps: step ids such as T5, 5, or 步骤 5. A get_task steps list keeps failed, unfinished, and blocked steps. A JSON array string is accepted."},
				"cursor":                  map[string]any{"type": "integer", "description": "read_log: next_cursor from the previous page. Omit on the first page."},
				"expected_status_version": map[string]any{"type": "integer", "description": "send_command or retry_steps: status_version from get_task. A mismatch does not run."},
				"dry_run":                 map[string]any{"type": "boolean", "description": "send_command or retry_steps: check only. The round does not start."},
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
	tool, payload, secrets, err := localTaskPayload(args)
	if err != nil {
		return "", err
	}
	if botPhaseFromTurn(request) == "plan" && localTaskMutating(tool) {
		return "", fmt.Errorf("plan step: local_task can list and read tasks now; %s runs in the execute step", tool)
	}
	if tool == "" {
		return localTaskCatalog(), nil
	}
	return localTaskCall(ctx, scope, tool, payload, secrets)
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

func localTaskPayload(args map[string]any) (string, string, []string, error) {
	tool, err := localTaskCanonical(desktopArg(args, "tool"))
	if err != nil {
		return "", "", nil, err
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
					return "", "", nil, fmt.Errorf("arguments must be a JSON object")
				}
				for k, v := range obj {
					merged[k] = v
				}
			}
		default:
			return "", "", nil, fmt.Errorf("arguments must be a JSON object")
		}
	}
	// A tool name inside arguments wins over the top-level name. Fold
	// aliases before copying top-level fields, so a later copy cannot
	// cover the canonical field with another spelling.
	if nested, ok := merged["tool"].(string); ok && strings.TrimSpace(nested) != "" {
		tool, err = localTaskCanonical(nested)
		if err != nil {
			return "", "", nil, err
		}
	}
	delete(merged, "tool")
	dropped, err := localTaskFold(merged, tool)
	if err != nil {
		return "", "", nil, err
	}
	dropped = append(dropped, localTaskCoveredPasswords(merged, args)...)
	// Every top-level field is an MCP argument. An allowlist dropped step_ids
	// and the log cursor, so retry and the next log page never ran.
	for key := range args {
		if key == "tool" || key == "arguments" {
			continue
		}
		localTaskCopyArg(merged, args, key)
	}
	more, err := localTaskFold(merged, tool)
	if err != nil {
		return "", "", nil, err
	}
	dropped = append(dropped, more...)
	// commands that survived the fold are several choices, or a list that
	// arrived after command was chosen. One entry was already taken.
	if err := localTaskUseListedCommands(merged, tool); err != nil {
		return "", "", nil, err
	}
	if err := localTaskRejectAmbiguousTasks(merged, tool); err != nil {
		return "", "", nil, err
	}
	localTaskStringifyNumbers(merged, "client_request_id", "confirmation_id", "run_id")
	if tool == "" && len(merged) > 0 {
		return "", "", nil, fmt.Errorf("tool must be one of %s", strings.Join(localTaskToolNames, ", "))
	}
	// The password is only for creating a remote task. Other calls must not
	// carry it across Hub. wait is cleared because the relay stream ends at
	// 10 minutes while a coding run keeps going; waiting would look like a
	// failure and invite a second create.
	secrets := localTaskMergeSecrets(localTaskSecrets(merged, tool == "create_task"), dropped)
	localTaskScrubTaskText(merged, secrets)
	switch tool {
	case "create_task", "send_command":
		merged["wait"] = false
	default:
		delete(merged, "wait")
	}
	localTaskExpandStepIDs(merged)
	if err := localTaskRejectEmptyRetry(merged, tool); err != nil {
		return "", "", nil, err
	}
	localTaskDropEchoedModel(merged, tool)
	localTaskDropResultNoise(merged)
	raw, err := json.Marshal(merged)
	if err != nil {
		return "", "", nil, err
	}
	return tool, string(raw), secrets, nil
}

func localTaskSecrets(merged map[string]any, keep bool) []string {
	var secrets []string
	seen := map[string]bool{}
	for _, key := range []string{"ssh_password", "sshPassword", "password"} {
		text, ok := merged[key].(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		secrets = append(secrets, text)
	}
	delete(merged, "sshPassword")
	delete(merged, "password")
	if !keep || len(secrets) == 0 {
		delete(merged, "ssh_password")
		return secrets
	}
	// ssh_password is collected first, so a real value wins over the aliases.
	merged["ssh_password"] = secrets[0]
	return secrets
}

// localTaskFold keeps one canonical name for each GUI alias group.
// A password that loses to another spelling is returned so it can still be
// removed from the task text.
func localTaskFold(merged map[string]any, tool string) ([]string, error) {
	if merged == nil {
		return nil, nil
	}
	localTaskLiftEnvelope(merged, tool)
	dropped := localTaskAdopt(merged, "ssh_password", "sshPassword", "password")
	localTaskAdopt(merged, "client_request_id", "clientRequestId")
	localTaskAdopt(merged, "working_dir", "workingDir", "work_dir")
	localTaskAdopt(merged, "location")
	localTaskAdopt(merged, "cloud_workspace_id", "cloudWorkspaceId", "workspace_id")
	localTaskAdopt(merged, "grant_id", "grantId")
	localTaskAdopt(merged, "ssh_host", "sshHost")
	localTaskAdopt(merged, "ssh_user", "sshUser")
	localTaskAdopt(merged, "ssh_port", "sshPort")
	localTaskAdopt(merged, "remote_work_dir", "remoteWorkDir", "ssh_work_dir")
	// get_task returns task_id as the same path, and current_run_id as that round's run_id.
	localTaskAdopt(merged, "project_path", "projectPath", "task_id", "taskId")
	localTaskAdopt(merged, "run_id", "runId", "current_run_id", "currentRunId")
	localTaskAdopt(merged, "confirmation_id", "confirmationId")
	localTaskAdopt(merged, "prompt", "instruction")
	localTaskAdopt(merged, "step_ids", "stepIds", "steps")
	// get_task returns status_version. send, retry, and stop compare
	// expected_status_version. Copying the result field forward keeps that
	// check. create_task leaves status_version alone so a copied number does
	// not fence a new task.
	versionAliases := []string{"expectedStatusVersion"}
	switch tool {
	case "send_command", "retry_steps", "stop_task":
		versionAliases = append(versionAliases, "status_version", "statusVersion")
	}
	localTaskAdopt(merged, "expected_status_version", versionAliases...)
	// read_log's next page is next_cursor in the result and cursor in the call.
	localTaskAdopt(merged, "cursor", "next_cursor", "nextCursor")
	localTaskAdopt(merged, "dry_run", "dryRun")
	// send_command: command, then prompt, then one commands entry, then text.
	// get_task's text is the previous answer, so a pasted view must not send
	// that answer when a confirmation or a written follow-up is present.
	// create_task reads prompt on its own. An array in command is one choice
	// or a request to pick one, before text can be dropped.
	if tool == "send_command" {
		if err := localTaskFoldSendCommand(merged); err != nil {
			return nil, err
		}
	}
	// A project_path chosen in this same body retires the task list.
	if localTaskFilled(merged, "project_path") {
		delete(merged, "tasks")
	}
	return dropped, nil
}

// localTaskLiftEnvelope reads a get_request body. scope.task_id and the nested
// task carry the path and the round. The nested answer text stays out of command.
func localTaskLiftEnvelope(merged map[string]any, tool string) {
	if merged == nil {
		return
	}
	if scope := localTaskObject(merged["scope"]); scope != nil {
		delete(merged, "scope")
		localTaskFillFrom(merged, "project_path", scope, "task_id", "taskId", "project_path", "projectPath")
	}
	if task := localTaskObject(merged["task"]); task != nil {
		delete(merged, "task")
		localTaskApplyTask(merged, tool, task)
	}
	localTaskTakeSingleTaskList(merged, tool)
}

func localTaskApplyTask(merged map[string]any, tool string, task map[string]any) {
	localTaskFillFrom(merged, "project_path", task, "project_path", "projectPath", "task_id", "taskId")
	localTaskFillFrom(merged, "run_id", task, "run_id", "runId", "current_run_id", "currentRunId")
	localTaskFillFrom(merged, "confirmation_id", task, "confirmation_id", "confirmationId")
	switch tool {
	case "send_command", "retry_steps", "stop_task":
		if !localTaskFilled(merged, "expected_status_version") && !localTaskFilled(merged, "status_version") {
			localTaskFillFrom(merged, "status_version", task, "status_version", "statusVersion")
		}
	}
	if tool == "send_command" && !localTaskFilled(merged, "commands") {
		if value, ok := task["commands"]; ok && value != nil {
			merged["commands"] = value
		}
	}
	if tool == "retry_steps" && !localTaskFilled(merged, "step_ids") && !localTaskFilled(merged, "steps") && !localTaskFilled(merged, "stepIds") {
		if value, ok := task["steps"]; ok && value != nil {
			merged["steps"] = value
		}
	}
}

// localTaskTakeSingleTaskList reads list_tasks. One entry fills an empty
// project_path. Several entries stay in place until the call names a path.
func localTaskTakeSingleTaskList(merged map[string]any, tool string) {
	raw, ok := merged["tasks"]
	if !ok || raw == nil {
		return
	}
	items, recognized := localTaskTaskItems(raw)
	if !recognized {
		return
	}
	if len(items) == 0 {
		delete(merged, "tasks")
		return
	}
	if len(items) > 1 {
		if localTaskFilled(merged, "project_path") {
			delete(merged, "tasks")
		}
		return
	}
	switch items[0].(type) {
	case map[string]any, string:
		delete(merged, "tasks")
		localTaskApplyListedTask(merged, tool, items[0])
	}
}

func localTaskApplyListedTask(merged map[string]any, tool string, item any) {
	switch typed := item.(type) {
	case map[string]any:
		localTaskApplyTask(merged, tool, typed)
	case string:
		if obj := localTaskObject(typed); obj != nil {
			localTaskApplyTask(merged, tool, obj)
			return
		}
		text := strings.TrimSpace(typed)
		if text == "" || localTaskFilled(merged, "project_path") {
			return
		}
		merged["project_path"] = text
	}
}

func localTaskTaskItems(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case string:
		inner, _ := localTaskUnwrapFence(typed)
		if items, ok := localTaskJSONArray(inner); ok {
			return items, true
		}
		if obj := localTaskObject(inner); obj != nil {
			return []any{obj}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func localTaskNeedsProject(tool string) bool {
	switch tool {
	case "get_task", "send_command", "stop_task", "retry_steps", "read_log":
		return true
	}
	return false
}

func localTaskRejectAmbiguousTasks(merged map[string]any, tool string) error {
	raw, ok := merged["tasks"]
	if !ok || raw == nil {
		return nil
	}
	items, recognized := localTaskTaskItems(raw)
	delete(merged, "tasks")
	if !recognized || len(items) <= 1 {
		return nil
	}
	if localTaskFilled(merged, "project_path") || !localTaskNeedsProject(tool) {
		return nil
	}
	return fmt.Errorf("tasks lists more than one task; set project_path to the one to use")
}

// localTaskDropEchoedModel leaves a send on the task's current model.
// get_task reports that model twice: provider with provider_effective, and
// model with model_effective. send_command treats any provider or model as
// a change, and a live confirmation is refused while a change is attached.
// Matching pairs are the current model. A different provider or model is
// the caller's choice, and both values stay so the pair is complete.
func localTaskDropEchoedModel(merged map[string]any, tool string) {
	if tool != "send_command" || merged == nil {
		return
	}
	provider, hasProvider := localTaskFieldString(merged, "provider")
	model, hasModel := localTaskFieldString(merged, "model")
	effProvider, hasEffProvider := localTaskFieldString(merged, "provider_effective")
	effModel, hasEffModel := localTaskFieldString(merged, "model_effective")
	delete(merged, "provider_effective")
	delete(merged, "model_effective")
	delete(merged, "provider_requested")
	delete(merged, "model_requested")
	if !hasEffProvider && !hasEffModel {
		return
	}
	providerSame := !hasProvider || (hasEffProvider && strings.EqualFold(provider, effProvider))
	modelSame := !hasModel || (hasEffModel && strings.EqualFold(model, effModel))
	if providerSame && modelSame {
		delete(merged, "provider")
		delete(merged, "model")
		return
	}
	if !hasProvider && hasEffProvider {
		merged["provider"] = effProvider
	}
}

func localTaskFieldString(merged map[string]any, key string) (string, bool) {
	text, ok := merged[key].(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

// localTaskDropResultNoise removes pages and nested lists the GUI does not
// read as arguments. A copied read_log page can be larger than the Hub body.
func localTaskDropResultNoise(merged map[string]any) {
	if merged == nil {
		return
	}
	for _, key := range []string{"events", "checks", "children", "conflicts", "text", "message", "error", "end_detail", "end_reason", "outcome", "dirty_note"} {
		delete(merged, key)
	}
}

func localTaskObject(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		text, _ := localTaskUnwrapFence(typed)
		text = strings.TrimSpace(text)
		if text == "" || !strings.HasPrefix(text, "{") {
			return nil
		}
		if obj, ok := localTaskParseObject(text); ok {
			return obj
		}
		return nil
	default:
		return nil
	}
}

func localTaskFillFrom(dst map[string]any, canonical string, src map[string]any, keys ...string) {
	if localTaskFilled(dst, canonical) || src == nil {
		return
	}
	for _, key := range keys {
		value, ok := src[key]
		if !ok || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			dst[canonical] = text
			return
		}
		dst[canonical] = value
		return
	}
}

// localTaskFoldSendCommand picks the text send_command will run.
// prompt is a follow-up written for this call. One commands entry is the
// confirmation from get_task. text is used when neither of those is present,
// so a pasted answer does not replace the confirmation.
func localTaskFoldSendCommand(merged map[string]any) error {
	if err := localTaskCoerceCommand(merged); err != nil {
		return err
	}
	if strings.TrimSpace(localTaskCommandText(merged["command"])) == "" {
		localTaskAdopt(merged, "command", "prompt")
	}
	if strings.TrimSpace(localTaskCommandText(merged["command"])) == "" {
		switch localTaskListedCommandCount(merged["commands"]) {
		case 1:
			if err := localTaskUseListedCommands(merged, "send_command"); err != nil {
				return err
			}
		default:
			if localTaskListedCommandCount(merged["commands"]) > 1 {
				return nil
			}
			localTaskAdopt(merged, "command", "text", "message")
		}
	}
	if strings.TrimSpace(localTaskCommandText(merged["command"])) != "" {
		delete(merged, "text")
		delete(merged, "message")
	}
	return nil
}

func localTaskListedCommandCount(value any) int {
	if value == nil {
		return 0
	}
	choices, recognized := localTaskCommandChoices(value)
	if !recognized {
		return 0
	}
	return len(choices)
}

func localTaskCoerceCommand(merged map[string]any) error {
	raw, ok := merged["command"]
	if !ok || raw == nil {
		return nil
	}
	if text, ok := raw.(string); ok {
		if strings.TrimSpace(text) == "" {
			delete(merged, "command")
		}
		return nil
	}
	choices, recognized := localTaskCommandChoices(raw)
	if !recognized || len(choices) == 0 {
		delete(merged, "command")
		return nil
	}
	if len(choices) > 1 {
		return fmt.Errorf("command lists more than one choice; set command to the one to send")
	}
	merged["command"] = choices[0]
	return nil
}

// localTaskUseListedCommands turns get_task's commands into command when the
// call did not already name one. Several entries stay a choice for the model.
func localTaskUseListedCommands(merged map[string]any, tool string) error {
	if tool != "send_command" || merged == nil {
		return nil
	}
	raw, ok := merged["commands"]
	if !ok || raw == nil {
		return nil
	}
	delete(merged, "commands")
	if strings.TrimSpace(localTaskCommandText(merged["command"])) != "" {
		return nil
	}
	choices, recognized := localTaskCommandChoices(raw)
	if !recognized || len(choices) == 0 {
		return nil
	}
	if len(choices) > 1 {
		return fmt.Errorf("commands lists more than one choice; set command to the one to send")
	}
	merged["command"] = choices[0]
	return nil
}

func localTaskCommandText(value any) string {
	text, _ := value.(string)
	return text
}

func localTaskCommandChoices(value any) ([]string, bool) {
	switch typed := value.(type) {
	case string:
		inner, _ := localTaskUnwrapFence(typed)
		if items, ok := localTaskJSONArray(inner); ok {
			return localTaskChoiceStrings(items), true
		}
		text := strings.TrimSpace(inner)
		if text == "" {
			return nil, true
		}
		return []string{text}, true
	case []any:
		return localTaskChoiceStrings(typed), true
	case []string:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = item
		}
		return localTaskChoiceStrings(items), true
	default:
		return nil, false
	}
}

func localTaskChoiceStrings(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func localTaskAdopt(merged map[string]any, canonical string, aliases ...string) []string {
	if !localTaskFilled(merged, canonical) {
		for _, alias := range aliases {
			if localTaskFilled(merged, alias) {
				merged[canonical] = merged[alias]
				break
			}
		}
	}
	winner, _ := merged[canonical].(string)
	winner = strings.TrimSpace(winner)
	var dropped []string
	for _, alias := range aliases {
		if text, ok := merged[alias].(string); ok {
			text = strings.TrimSpace(text)
			if text != "" && text != winner {
				dropped = append(dropped, text)
			}
		}
		delete(merged, alias)
	}
	return dropped
}

func localTaskFilled(merged map[string]any, key string) bool {
	value, ok := merged[key]
	if !ok || value == nil {
		return false
	}
	text, ok := value.(string)
	if ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

// localTaskCoveredPasswords keeps a top-level password that lost to the
// value already taken from arguments. The task text is still scrubbed with it.
func localTaskCoveredPasswords(dst, src map[string]any) []string {
	if !localTaskFilled(dst, "ssh_password") {
		return nil
	}
	winner, _ := dst["ssh_password"].(string)
	winner = strings.TrimSpace(winner)
	var dropped []string
	seen := map[string]bool{winner: true}
	for _, key := range []string{"ssh_password", "sshPassword", "password"} {
		text, ok := src[key].(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		dropped = append(dropped, text)
	}
	return dropped
}

func localTaskMergeSecrets(base, extra []string) []string {
	seen := map[string]bool{}
	for _, secret := range base {
		seen[secret] = true
	}
	for _, secret := range extra {
		if secret == "" || seen[secret] {
			continue
		}
		seen[secret] = true
		base = append(base, secret)
	}
	return base
}

// localTaskScrubTaskText removes the SSH password from text the task keeps.
// The connection still receives ssh_password. A title or a follow-up field
// is not a second copy of that secret.
func localTaskScrubTaskText(merged map[string]any, secrets []string) {
	for _, key := range []string{"prompt", "instruction", "name", "command", "text", "message"} {
		raw, ok := merged[key].(string)
		if !ok || raw == "" {
			continue
		}
		for _, secret := range secrets {
			raw = localrelay.RedactSecret(raw, secret)
		}
		merged[key] = raw
	}
}

// localTaskExpandStepIDs turns a JSON array string into an array. A weak
// model often sends step_ids as one string, and the app then rejects it
// as a single unrecognized id.
func localTaskExpandStepIDs(merged map[string]any) {
	for _, key := range []string{"step_ids", "stepIds", "steps"} {
		switch typed := merged[key].(type) {
		case string:
			inner, fenced := localTaskUnwrapFence(typed)
			if items, ok := localTaskJSONArray(inner); ok {
				merged[key] = localTaskStepItems(items)
				continue
			}
			// A fenced comma list is still a step list. Leave it for the app splitter.
			if fenced && strings.TrimSpace(inner) != "" {
				merged[key] = strings.TrimSpace(inner)
			}
		case []any:
			merged[key] = localTaskStepItems(typed)
		}
	}
}

// localTaskStepItems reads id from a get_task step object. The app accepts
// id strings, and an object would be rejected as an unrecognized step.
// A passed step in that list makes the app refuse the whole retry, so those
// ids stay out. An id with no status is an explicit choice and stays.
func localTaskStepItems(items []any) []any {
	out := make([]any, 0, len(items))
	changed := false
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		id := localTaskStepID(obj)
		if id == "" {
			out = append(out, item)
			continue
		}
		changed = true
		if !localTaskStepCanRetry(obj) {
			continue
		}
		out = append(out, id)
	}
	if !changed {
		return items
	}
	return out
}

// localTaskStepCanRetry reports whether a step object can be retried.
// passed always stays out: the app rejects the call if one is included.
// retryable is the app's own flag. Without that flag, failed, unfinished,
// and blocked steps are kept, and skipped or waiting steps stay out.
func localTaskStepCanRetry(obj map[string]any) bool {
	if status, ok := obj["status"].(string); ok && strings.EqualFold(strings.TrimSpace(status), "passed") {
		return false
	}
	if value, ok := obj["retryable"].(bool); ok {
		return value
	}
	status, ok := obj["status"].(string)
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "skipped", "waiting", "needs_confirmation":
		return false
	default:
		return true
	}
}

func localTaskRejectEmptyRetry(merged map[string]any, tool string) error {
	if tool != "retry_steps" || merged == nil {
		return nil
	}
	raw, ok := merged["step_ids"]
	if !ok || raw == nil {
		return nil
	}
	empty := false
	switch typed := raw.(type) {
	case []any:
		empty = len(typed) == 0
	case []string:
		empty = len(typed) == 0
	case string:
		empty = strings.TrimSpace(typed) == ""
	}
	if !empty {
		return nil
	}
	return fmt.Errorf("retry_steps needs a failed, unfinished, or blocked step")
}

func localTaskStepID(obj map[string]any) string {
	for _, key := range []string{"id", "step_id", "stepId"} {
		switch typed := obj[key].(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case float64:
			if typed > 0 && typed == float64(int64(typed)) {
				return strconv.FormatInt(int64(typed), 10)
			}
		case int:
			if typed > 0 {
				return strconv.Itoa(typed)
			}
		}
	}
	return ""
}

// localTaskStringifyNumbers turns a numeric id into the string the app reads.
// A JSON number is otherwise ignored, so the call loses its idempotency key.
func localTaskStringifyNumbers(merged map[string]any, keys ...string) {
	for _, key := range keys {
		switch typed := merged[key].(type) {
		case float64:
			if typed > 0 && typed == float64(int64(typed)) {
				merged[key] = strconv.FormatInt(int64(typed), 10)
			}
		case int:
			if typed > 0 {
				merged[key] = strconv.Itoa(typed)
			}
		}
	}
}

// localTaskUnwrapFence removes one ``` fence. A language tag is dropped.
// The step text on the first line stays.
func localTaskUnwrapFence(text string) (string, bool) {
	raw := strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF"))
	if !strings.HasPrefix(raw, "```") {
		return raw, false
	}
	body := strings.TrimSpace(raw[3:])
	if strings.HasSuffix(body, "```") {
		body = strings.TrimSpace(body[:len(body)-3])
	}
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		head := strings.TrimSpace(body[:nl])
		if localTaskLanguageTag(head) {
			body = strings.TrimSpace(body[nl+1:])
		}
	} else if i := strings.IndexByte(body, '['); i > 0 && localTaskLanguageTag(body[:i]) {
		body = strings.TrimSpace(body[i:])
	}
	return strings.TrimSpace(body), true
}

func localTaskLanguageTag(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	for _, r := range line {
		switch r {
		case '[', ']', '"', ',', '，', ';', '；':
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
	}
	return !strings.Contains(line, "步骤") && !strings.Contains(line, "步驟")
}

// localTaskJSONArray accepts a JSON array or a quoted JSON array.
// Other strings stay as the app's comma-separated ids.
func localTaskJSONArray(text string) ([]any, bool) {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF"))
	if strings.HasPrefix(text, `"`) {
		var inner string
		if json.Unmarshal([]byte(text), &inner) == nil {
			text = strings.TrimSpace(inner)
		}
	}
	if !strings.HasPrefix(text, "[") {
		return nil, false
	}
	if items, ok := localTaskParseJSONArray(text); ok {
		return items, true
	}
	// A weak model often leaves the comma after the last id.
	repaired := localTaskDropTrailingComma(text, ']')
	if repaired == text {
		return nil, false
	}
	return localTaskParseJSONArray(repaired)
}

func localTaskParseObject(text string) (map[string]any, bool) {
	var obj map[string]any
	if json.Unmarshal([]byte(text), &obj) == nil && obj != nil {
		return obj, true
	}
	repaired := localTaskDropTrailingComma(text, '}')
	if repaired == text {
		return nil, false
	}
	var again map[string]any
	if json.Unmarshal([]byte(repaired), &again) != nil || again == nil {
		return nil, false
	}
	return again, true
}

func localTaskParseJSONArray(text string) ([]any, bool) {
	var items []any
	if json.Unmarshal([]byte(text), &items) != nil {
		return nil, false
	}
	return items, true
}

func localTaskDropTrailingComma(text string, closer byte) string {
	end := strings.LastIndexByte(text, closer)
	if end < 0 {
		return text
	}
	i := end - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\n' || text[i] == '\r' || text[i] == '\t') {
		i--
	}
	if i < 0 || text[i] != ',' {
		return text
	}
	return text[:i] + text[i+1:]
}

func localTaskCatalog() string {
	var b strings.Builder
	b.WriteString("MCP tools on this computer:\n")
	for _, name := range localTaskToolNames {
		fmt.Fprintf(&b, "- %s\n", name)
	}
	return b.String()
}

func localTaskCopyArg(dst map[string]any, args map[string]any, key string) {
	if localTaskFilled(dst, key) {
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

// localTaskBodyLimit is Hub's relay body cap. Tests shrink it.
var localTaskBodyLimit = 256 << 10

func localTaskCall(ctx context.Context, scope agentruntime.Scope, tool, payload string, secrets []string) (string, error) {
	body := localRelayBase(scope, "mcp_call")
	body["server"] = mcpHostRelayServerName
	body["tool"] = tool
	body["args"] = payload
	// Measure the JSON Hub will read. Quoting a prompt full of \ or " can
	// push the body past the cap even when the prompt itself fits.
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	if len(raw) > localTaskBodyLimit {
		return "", fmt.Errorf("the task request is too large to send over Hub. Shorten the prompt and try again")
	}
	reply, err := localRelayPost(ctx, body)
	if err != nil {
		return "", localTaskRedactErr(localTaskUnanswered(tool, payload, err), secrets)
	}
	if !reply.OK {
		msg := strings.TrimSpace(reply.Error)
		if msg == "" {
			msg = "the person's computer refused the request"
		}
		err = &localrelay.Err{Code: reply.Code, Message: msg}
		return "", localTaskRedactErr(localTaskUnanswered(tool, payload, err), secrets)
	}
	return localTaskRedact(strings.TrimSpace(reply.Text), secrets), nil
}

// localTaskUnanswered tells the model a mutating call may have started
// when the Hub stream or the HTTP client gave up. A read that times out
// did not start work, and a dial failure never reached the computer.
// create_task has no project_path until the reply arrives, so that timeout
// points at get_request when a client_request_id was sent, and at list_tasks
// when it was not.
func localTaskUnanswered(tool, payload string, err error) error {
	if err == nil || !localTaskMutating(tool) || !localTaskInFlight(err) {
		return err
	}
	switch tool {
	case "stop_task", "cancel_task":
		return fmt.Errorf("%w. The stop may already have reached the computer; use get_task to see the task", err)
	case "create_task", "send_command", "retry_steps":
		if id := localTaskArgString(payload, "client_request_id"); id != "" {
			return fmt.Errorf("%w. The work may already be running on the computer; use get_request with client_request_id %s before starting another", err, id)
		}
		if tool == "create_task" {
			return fmt.Errorf("%w. The work may already be running on the computer; use list_tasks before starting another", err)
		}
		return fmt.Errorf("%w. The work may already be running on the computer; use get_task before starting another", err)
	default:
		return fmt.Errorf("%w. The work may already be running on the computer; use get_task before starting another", err)
	}
}

func localTaskArgString(payload, key string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(payload), &obj) != nil {
		return ""
	}
	text, _ := obj[key].(string)
	return strings.TrimSpace(text)
}

func localTaskInFlight(err error) bool {
	if localrelay.CodeOf(err) == localrelay.CodeTimeout {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Client.Timeout") || strings.Contains(msg, "deadline exceeded")
}

func localTaskRedact(text string, secrets []string) string {
	for _, secret := range secrets {
		text = localrelay.RedactSecret(text, secret)
	}
	return text
}

func localTaskRedactErr(err error, secrets []string) error {
	if err == nil {
		return nil
	}
	msg := localTaskRedact(err.Error(), secrets)
	if msg == err.Error() {
		return err
	}
	if code := localrelay.CodeOf(err); code != "" && code != localrelay.CodeFailed {
		return &localrelay.Err{Code: code, Message: msg}
	}
	return errors.New(msg)
}

// mcpHostRelayServerName is the server id the GUI treats as its own MCP
// task service. It is not a stdio server and not a URL.
const mcpHostRelayServerName = "maclaw"
