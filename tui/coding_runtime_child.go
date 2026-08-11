package main

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
)

const tuiReadOnlyChildSpawnToolName = "spawn_coding_agent"

const tuiReadOnlyChildMaxFanout = 3

var tuiReadOnlyChildTools = map[string]bool{
	"read_file":      true,
	"list_directory": true,
	"web_search":     true,
	"web_fetch":      true,
}

type tuiReadOnlyChildSpec struct {
	Role string
	Task string
}

func tuiReadOnlyChildToolAllowed(name string) bool {
	return tuiReadOnlyChildTools[strings.TrimSpace(name)]
}

func tuiReadOnlyChildToolCallAllowed(name string, args map[string]interface{}) (bool, string) {
	return (codingagent.ToolPolicy{Role: codingagent.RoleExplorer, Allowed: tuiReadOnlyChildTools}).IsToolCallAllowed(name, args)
}

func tuiFilterReadOnlyChildToolDefinitions(defs []map[string]interface{}) []map[string]interface{} {
	return (codingagent.ToolPolicy{Role: codingagent.RoleExplorer, Allowed: tuiReadOnlyChildTools}).FilterToolDefinitions(defs)
}

func tuiReadOnlyChildSystemPrompt(requestedWork string) string {
	requestedWork = strings.TrimSpace(requestedWork)
	if requestedWork == "" {
		requestedWork = "Inspect the assigned workspace and report bounded findings."
	}
	return "# Read-only coding child\n\n" +
		"You are an explorer/reviewer child for a coding task. Inspect only; do not edit files, run commands, access SSH, invoke skills/MCP, send messages, or create durable records. " +
		"Use only the supplied read/list/web tools. Return concise findings, relevant paths, risks, and verification gaps.\n\n" +
		"## Assigned work\n" + requestedWork
}

func tuiReadOnlyChildSpawnToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        tuiReadOnlyChildSpawnToolName,
			"description": "Admit independent read-only coding explorer/reviewer children. The parent attempt releases its lease immediately; children run asynchronously and return only bounded durable summaries for a later explicit parent attempt. worker is not supported.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"role": map[string]interface{}{"type": "string", "enum": []string{"explorer", "reviewer"}},
					"task": map[string]interface{}{"type": "string"},
					"agents": map[string]interface{}{
						"type": "array", "maxItems": tuiReadOnlyChildMaxFanout,
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

func parseTUIReadOnlyChildSpawn(argsJSON string) ([]tuiReadOnlyChildSpec, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(argsJSON)), &raw); err != nil {
		return nil, fmt.Errorf("invalid read-only child arguments: %v", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("read-only child arguments are required")
	}
	if value, ok := raw["agents"]; ok && value != nil {
		items, ok := value.([]interface{})
		if !ok || len(items) == 0 || len(items) > tuiReadOnlyChildMaxFanout {
			return nil, fmt.Errorf("agents must contain 1-%d read-only children", tuiReadOnlyChildMaxFanout)
		}
		out := make([]tuiReadOnlyChildSpec, 0, len(items))
		for index, value := range items {
			item, ok := value.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("agents[%d] must be an object", index)
			}
			spec, err := parseOneTUIReadOnlyChildSpec(item)
			if err != nil {
				return nil, fmt.Errorf("agents[%d]: %w", index, err)
			}
			out = append(out, spec)
		}
		return out, nil
	}
	spec, err := parseOneTUIReadOnlyChildSpec(raw)
	if err != nil {
		return nil, err
	}
	return []tuiReadOnlyChildSpec{spec}, nil
}

func parseOneTUIReadOnlyChildSpec(raw map[string]interface{}) (tuiReadOnlyChildSpec, error) {
	role, _ := raw["role"].(string)
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = string(codingagent.RoleExplorer)
	}
	parsed, err := codingagent.ParseRole(role)
	if err != nil || !parsed.ReadOnly() {
		return tuiReadOnlyChildSpec{}, fmt.Errorf("role must be explorer or reviewer")
	}
	task, _ := raw["task"].(string)
	task = strings.TrimSpace(task)
	if task == "" {
		return tuiReadOnlyChildSpec{}, fmt.Errorf("task is required")
	}
	return tuiReadOnlyChildSpec{Role: string(parsed), Task: task}, nil
}

func (c *tuiCallbacks) executeTUIReadOnlyChildSpawn(argsJSON string) string {
	if c == nil {
		return "Error: read-only child delegation is unavailable"
	}
	c.runtimeMu.Lock()
	store, attempt, childApp, readOnly, childExecutions := c.runtimeStore, c.runtimeAttempt, c.runtimeChildApp, c.runtimeReadOnlyChild, c.childExecutions
	c.runtimeMu.Unlock()
	if store == nil || attempt == nil || readOnly || childApp == nil || childExecutions == nil {
		return "Error: read-only child delegation is unavailable"
	}
	specs, err := parseTUIReadOnlyChildSpawn(argsJSON)
	if err != nil {
		return "Error: " + err.Error()
	}
	projectPath := strings.TrimSpace(attempt.Policy.ProjectRoot)
	policy := codingruntime.PolicySnapshot{ProjectRoot: projectPath, Mode: "local", ReadOnly: true}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		return "Error: cannot freeze read-only child policy"
	}
	policy.Digest = digest
	children := make([]codingruntime.ChildTaskSpec, 0, len(specs))
	for _, spec := range specs {
		children = append(children, codingruntime.ChildTaskSpec{Name: spec.Role, RequestedWork: spec.Task, ProjectRef: projectPath, Mode: "local"})
	}
	handles, err := (codingruntime.ChildTaskService{Store: store}).AdmitReadOnlyChildren(attempt.AttemptID, attempt.LeaseOwner, children, policy)
	if err != nil {
		return "Error: read-only child admission failed"
	}
	for _, handle := range handles {
		handle := handle
		child := tuiReadOnlyChildExecutor{app: childApp}
		go func() {
			ctx, release := childExecutions.Begin(attempt.TaskID, handle.TaskID)
			defer release()
			runner := codingruntime.Runner{Store: store, LeaseOwner: "tui:child:" + handle.TaskID, LeaseDuration: 15 * time.Minute}
			_, _, _, _ = (codingruntime.ChildTaskService{Store: store}).RunReadOnlyChild(ctx, runner, handle.TaskID, policy, child)
		}()
	}
	return formatTUIReadOnlyChildHandles(handles)
}

func formatTUIReadOnlyChildHandles(handles []codingruntime.ChildTaskHandle) string {
	lines := []string{"Read-only coding child task(s) admitted; the parent attempt released its lease:"}
	for _, handle := range handles {
		lines = append(lines, fmt.Sprintf("- task_id=%s role=%s status=%s", handle.TaskID, handle.Name, handle.Status))
	}
	lines = append(lines, "Children run independently. Only bounded summaries/evidence are retained; a fresh parent attempt must explicitly review them.")
	return strings.Join(lines, "\n")
}

type tuiReadOnlyChildExecutor struct{ app *TUIApp }

func (e tuiReadOnlyChildExecutor) ExecuteReadOnlyChild(ctx context.Context, run codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
	if e.app == nil || !run.Attempt.Policy.ReadOnly || strings.TrimSpace(run.Task.ProjectRef) != strings.TrimSpace(run.Attempt.Policy.ProjectRoot) {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "TUI read-only child policy is unavailable"}
	}
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCancelled, Summary: "TUI read-only child cancelled before execution"}
	}
	cb := newTuiCallbacks(e.app, nil)
	cb.runtimeReadOnlyChild = true
	cb.executionCtx = ctx
	result := codingagent.Run(cb, run.Task.RequestedWork, run.Task.RequestedWork, nil, nil, nil)
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCancelled, Summary: "TUI read-only child cancelled during execution"}
	}
	if result.HardExit || strings.TrimSpace(result.Error) != "" {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "TUI read-only child failed", EvidenceDigest: tuiReadOnlyChildDigest(result)}
	}
	if result.AskUser != nil || result.RecordAudio != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskBlocked, Summary: "TUI read-only child requires explicit user input", EvidenceDigest: tuiReadOnlyChildDigest(result)}
	}
	return codingruntime.ChildTaskResult{Status: codingruntime.TaskCompleted, Summary: boundedTUIReadOnlyChildSummary(result.Text), EvidenceDigest: tuiReadOnlyChildDigest(result)}
}

func boundedTUIReadOnlyChildSummary(value string) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= 4096 {
		return value
	}
	return string([]rune(value)[:4096])
}

func tuiReadOnlyChildDigest(result agent.LoopResult) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("iterations=%d|tools=%d|hard_exit=%t|usage=%d:%d", result.Iterations, result.ToolCalls, result.HardExit, result.Usage.InputTokens, result.Usage.OutputTokens)))
	return fmt.Sprintf("sha256:%x", sum[:])
}
