package agentservice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// The first service-hosted child surface is deliberately narrower than the
// ordinary planning policy.  In particular, it omits bash, SSH, skills, MCP,
// memory, IM and task-management tools: each can mutate external or durable
// host state even when it appears read-oriented.  Additions require a separate
// host-level safety review and execution-time enforcement below.
var serviceReadOnlyChildTools = map[string]bool{
	"read_file":      true,
	"list_directory": true,
	"web_search":     true,
	"web_fetch":      true,
}

const serviceReadOnlyChildSpawnToolName = "spawn_coding_agent"

const serviceReadOnlyChildMaxFanout = 3

type serviceReadOnlyChildSpec struct {
	Role string
	Task string
}

func serviceReadOnlyChildSpawnToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        serviceReadOnlyChildSpawnToolName,
			"description": "Admit independent read-only coding explorer/reviewer children. The parent attempt releases its lease immediately; children run asynchronously and return only bounded durable summaries for a later explicit parent attempt. worker is not supported.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"role": map[string]interface{}{"type": "string", "enum": []string{"explorer", "reviewer"}},
					"task": map[string]interface{}{"type": "string"},
					"agents": map[string]interface{}{
						"type":     "array",
						"maxItems": serviceReadOnlyChildMaxFanout,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"role": map[string]interface{}{"type": "string", "enum": []string{"explorer", "reviewer"}},
								"task": map[string]interface{}{"type": "string"},
							},
							"required": []string{"task"},
						},
					},
				},
			},
		},
	}
}

func parseServiceReadOnlyChildSpawn(argsJSON string) ([]serviceReadOnlyChildSpec, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(argsJSON)), &raw); err != nil {
		return nil, fmt.Errorf("invalid read-only child arguments: %v", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("read-only child arguments are required")
	}
	if agents, hasAgents := raw["agents"]; hasAgents && agents != nil {
		values, ok := agents.([]interface{})
		if !ok || len(values) == 0 || len(values) > serviceReadOnlyChildMaxFanout {
			return nil, fmt.Errorf("agents must contain 1-%d read-only children", serviceReadOnlyChildMaxFanout)
		}
		out := make([]serviceReadOnlyChildSpec, 0, len(values))
		for index, value := range values {
			item, ok := value.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("agents[%d] must be an object", index)
			}
			spec, err := parseOneServiceReadOnlyChildSpec(item)
			if err != nil {
				return nil, fmt.Errorf("agents[%d]: %w", index, err)
			}
			out = append(out, spec)
		}
		return out, nil
	}
	spec, err := parseOneServiceReadOnlyChildSpec(raw)
	if err != nil {
		return nil, err
	}
	return []serviceReadOnlyChildSpec{spec}, nil
}

func parseOneServiceReadOnlyChildSpec(raw map[string]interface{}) (serviceReadOnlyChildSpec, error) {
	role, _ := raw["role"].(string)
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = string(codingagent.RoleExplorer)
	}
	parsedRole, err := codingagent.ParseRole(role)
	if err != nil || !parsedRole.ReadOnly() {
		return serviceReadOnlyChildSpec{}, fmt.Errorf("role must be explorer or reviewer")
	}
	task, _ := raw["task"].(string)
	task = strings.TrimSpace(task)
	if task == "" {
		return serviceReadOnlyChildSpec{}, fmt.Errorf("task is required")
	}
	return serviceReadOnlyChildSpec{Role: string(parsedRole), Task: task}, nil
}

func (c *coreAgentCallbacks) executeServiceReadOnlyChildSpawn(argsJSON string) agent.ToolExecutionResult {
	if c == nil || c.runtimeStore == nil || c.runtimeAttempt == nil || c.runtimeReadOnlyChild {
		return agent.ToolExecutionResult{Result: "Error: read-only child delegation is unavailable", Outcome: agent.ToolExecutionOutcomeError}
	}
	specs, err := parseServiceReadOnlyChildSpawn(argsJSON)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	policy := codingruntime.PolicySnapshot{
		ProjectRoot: strings.TrimSpace(c.workspace),
		Mode:        "local",
		ReadOnly:    true,
	}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: cannot freeze read-only child policy", Outcome: agent.ToolExecutionOutcomeError}
	}
	policy.Digest = digest
	children := make([]codingruntime.ChildTaskSpec, 0, len(specs))
	for _, spec := range specs {
		children = append(children, codingruntime.ChildTaskSpec{
			Name:          spec.Role,
			RequestedWork: spec.Task,
			ProjectRef:    c.workspace,
			Mode:          "local",
		})
	}
	handles, err := (codingruntime.ChildTaskService{Store: c.runtimeStore}).AdmitReadOnlyChildren(c.runtimeAttempt.AttemptID, c.runtimeAttempt.LeaseOwner, children, policy)
	if err != nil {
		return agent.ToolExecutionResult{Result: "Error: read-only child admission failed", Outcome: agent.ToolExecutionOutcomeError}
	}
	// Admission is the parent tool boundary.  RunLoop observes waiting_child
	// through ShouldStop and returns; each child gets a fresh callback/context.
	for _, handle := range handles {
		handle := handle
		childExecutor := c.runtimeChildExecutor()
		parentTaskID := c.runtimeAttempt.TaskID
		parent := c.runtimeParentExecutor
		go func() {
			ctx := context.Background()
			release := func() {}
			if parent != nil {
				ctx, release = parent.childExecutions.Begin(parentTaskID, handle.TaskID)
			}
			defer release()
			runner := codingruntime.Runner{
				Store:         c.runtimeStore,
				LeaseOwner:    "srv:child:" + handle.TaskID,
				LeaseDuration: 15 * time.Minute,
			}
			_, _, _, _ = (codingruntime.ChildTaskService{Store: c.runtimeStore}).RunReadOnlyChild(ctx, runner, handle.TaskID, policy, childExecutor)
		}()
	}
	return agent.ToolExecutionResult{Result: formatServiceReadOnlyChildHandles(handles), Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) runtimeChildExecutor() codingruntime.ReadOnlyChildExecutor {
	return serviceReadOnlyChildExecutor{parent: c.runtimeParentExecutor, request: c.runtimeRequest}
}

func formatServiceReadOnlyChildHandles(handles []codingruntime.ChildTaskHandle) string {
	lines := []string{"Read-only coding child task(s) admitted; the parent attempt released its lease:"}
	for _, handle := range handles {
		lines = append(lines, fmt.Sprintf("- task_id=%s role=%s status=%s", handle.TaskID, handle.Name, handle.Status))
	}
	lines = append(lines, "Children run independently. Only bounded summaries/evidence are retained; a fresh parent attempt must explicitly review them.")
	return strings.Join(lines, "\n")
}

func serviceReadOnlyChildToolAllowed(name string) bool {
	return serviceReadOnlyChildTools[strings.TrimSpace(name)]
}

func serviceReadOnlyChildToolCallAllowed(name string, args map[string]interface{}) (bool, string) {
	return (codingagent.ToolPolicy{Role: codingagent.RoleExplorer, Allowed: serviceReadOnlyChildTools}).IsToolCallAllowed(name, args)
}

func filterServiceReadOnlyChildToolDefinitions(tools []map[string]interface{}) []map[string]interface{} {
	return (codingagent.ToolPolicy{
		Role:    codingagent.RoleExplorer,
		Allowed: serviceReadOnlyChildTools,
	}).FilterToolDefinitions(tools)
}

func serviceReadOnlyChildSystemPrompt(requestedWork string) string {
	requestedWork = strings.TrimSpace(requestedWork)
	if requestedWork == "" {
		requestedWork = "Inspect the assigned workspace and report bounded findings."
	}
	return "# Read-only coding child\n\n" +
		"You are an explorer/reviewer child for a coding task. Inspect only; do not edit files, run commands, access SSH, invoke skills/MCP, send messages, or create durable records. " +
		"Use only the supplied read/list/web tools. Return concise findings, relevant paths, risks, and verification gaps.\n\n" +
		"## Assigned work\n" + requestedWork
}

// serviceReadOnlyChildExecutor runs an admitted runtime child using a new
// service callback and a fresh model conversation.  It intentionally does not
// call executeDirect: that path permits the full host tool surface and would
// make the child contract advisory rather than enforced.
type serviceReadOnlyChildExecutor struct {
	parent  *CoreAgentExecutor
	request ExecuteRequest
}

func (e serviceReadOnlyChildExecutor) ExecuteReadOnlyChild(ctx context.Context, run codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
	if e.parent == nil || !run.Attempt.Policy.ReadOnly {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "service read-only child policy is unavailable"}
	}
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCancelled, Summary: "service read-only child cancelled before execution"}
	}
	if err := validateServiceReadOnlyChildRequest(e.request, run); err != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "service read-only child rejected"}
	}

	llmCfg, err := ResolveLLMConfig(e.request.Config)
	if err != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "service read-only child LLM configuration unavailable"}
	}
	resources, err := e.parent.resourcesForUser(e.request.Principal.TenantID, e.request.Principal.UserID, e.request.DataDir)
	if err != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "service read-only child resources unavailable"}
	}
	cb := &coreAgentCallbacks{
		ctx:                  ctx,
		appCfg:               e.request.Config,
		llmCfg:               llmCfg,
		principal:            e.request.Principal,
		tenant:               e.request.Tenant,
		user:                 e.request.User,
		instance:             e.request.Instance,
		userText:             run.Task.RequestedWork,
		workspace:            e.request.Instance.Workspace,
		dataDir:              e.request.DataDir,
		memory:               resources,
		httpClient:           e.parent.clientFor(llmCfg),
		toolPolicy:           v2.ToolPolicyFull,
		mutationScope:        v2.MutationScopeNone,
		loopID:               "srv:child:" + run.Task.TaskID,
		runtimeReadOnlyChild: true,
	}
	result := codingagent.Run(cb, run.Task.RequestedWork, run.Task.RequestedWork, nil, cb.httpClient, nil)
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCancelled, Summary: "service read-only child cancelled during execution"}
	}
	if result.HardExit || strings.TrimSpace(result.Error) != "" {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "service read-only child failed", EvidenceDigest: serviceReadOnlyChildDigest(result)}
	}
	if result.AskUser != nil || result.RecordAudio != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskBlocked, Summary: "service read-only child requires explicit user input", EvidenceDigest: serviceReadOnlyChildDigest(result)}
	}
	return codingruntime.ChildTaskResult{Status: codingruntime.TaskCompleted, Summary: boundedServiceReadOnlyChildSummary(result.Text), EvidenceDigest: serviceReadOnlyChildDigest(result)}
}

func validateServiceReadOnlyChildRequest(req ExecuteRequest, run codingruntime.ExecutionRequest) error {
	if !run.Attempt.Policy.ReadOnly {
		return fmt.Errorf("read-only policy required")
	}
	if strings.TrimSpace(req.Instance.Workspace) == "" || strings.TrimSpace(run.Task.ProjectRef) != strings.TrimSpace(req.Instance.Workspace) {
		return fmt.Errorf("instance workspace mismatch")
	}
	if strings.TrimSpace(run.Task.OwnerID) != serviceCodingRuntimeOwner(req) {
		return fmt.Errorf("task owner mismatch")
	}
	if run.Task.Mode != "local" || run.Attempt.Policy.Mode != "local" || strings.TrimSpace(run.Attempt.Policy.ProjectRoot) != strings.TrimSpace(req.Instance.Workspace) {
		return fmt.Errorf("only local service child execution is supported")
	}
	return nil
}

func boundedServiceReadOnlyChildSummary(value string) string {
	value = strings.TrimSpace(value)
	const limit = 4096
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func serviceReadOnlyChildDigest(result agent.LoopResult) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("iterations=%d|tools=%d|hard_exit=%t|usage=%d:%d", result.Iterations, result.ToolCalls, result.HardExit, result.Usage.InputTokens, result.Usage.OutputTokens)))
	return fmt.Sprintf("sha256:%x", sum[:])
}
