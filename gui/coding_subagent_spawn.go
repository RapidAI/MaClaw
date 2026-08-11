package main

// coding_subagent_spawn.go — Codex-style nested subagents for pure coding workbench.
//
// The root full-environment CodingSubAgent (create-task coding_dev) can spawn
// specialized child agents with a clean context. Children cannot spawn further
// (nest depth hard cap), matching Codex depth control.

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingagent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

const (
	codingSubAgentSpawnToolName = "spawn_coding_agent"
	// Root pure-coding turn is depth 0; children are 1. Depth >= max cannot spawn.
	codingSubAgentMaxNestDepth = 1
	// Cap parallel fan-out per spawn call (Codex-style concurrency).
	codingSubAgentMaxParallelSpawn = 3
)

// codingSubAgentRole is retained as a GUI-facing alias while the canonical
// role vocabulary lives in corelib for TUI and MaClawSrv reuse.
type codingSubAgentRole = codingagent.Role

const (
	codingRoleWorker   = codingagent.RoleWorker   // reserved for a future isolated write-child runtime
	codingRoleExplorer = codingagent.RoleExplorer // read-only map/search
	codingRoleReviewer = codingagent.RoleReviewer // strict read-only review
)

var codingSubAgentSpawnRoleTools = map[codingSubAgentRole]map[string]bool{
	codingRoleExplorer: {
		"Glob": true, "ripgrep": true, "read_file": true, "list_directory": true,
		codeNavigationToolName: true, reportLocalizationToolName: true,
		"git_diff": true, "web_search": true, "web_fetch": true, "current_datetime": true,
		"coding_knowledge_search": true, "knowledge_search": true,
	},
	codingRoleReviewer: {
		"Glob": true, "ripgrep": true, "read_file": true, "list_directory": true,
		codeNavigationToolName: true, reportLocalizationToolName: true,
		"git_diff":   true,
		"web_search": true, "web_fetch": true, "current_datetime": true,
		"coding_knowledge_search": true, "knowledge_search": true,
	},
	// worker: nil map means "all standard coding tools except spawn"
}

func parseCodingSubAgentRole(raw string) (codingSubAgentRole, error) {
	return codingagent.ParseRole(raw)
}

func (s *CodingSubAgent) canSpawnCodingAgent() bool {
	if s == nil || !s.fullEnvironment {
		return false
	}
	if s.nestDepth >= codingSubAgentMaxNestDepth {
		return false
	}
	// Only root (or worker-role full env at depth 0) may admit inspection
	// children. Write-capable child workers are intentionally not admitted until
	// they have isolated workspaces and conflict detection in corelib.
	role := s.role
	if role == "" {
		role = codingRoleWorker
	}
	return role == codingRoleWorker
}

func (s *CodingSubAgent) toolAllowedForRole(name string) bool {
	if s == nil {
		return true
	}
	name = canonicalCodingSubAgentToolName(name)
	if name == codingSubAgentSpawnToolName {
		return s.canSpawnCodingAgent()
	}
	role := s.role
	if role == "" || role == codingRoleWorker {
		// Worker uses the full static/dynamic coding surface.
		return true
	}
	allowed, ok := codingSubAgentSpawnRoleTools[role]
	if !ok {
		return false
	}
	return (codingagent.ToolPolicy{Role: role, Allowed: allowed}).Allows(name)
}

// toolCallAllowedForRole is the execution-time companion to
// toolAllowedForRole. Inspection roles may expose observational web_fetch,
// but destination aliases turn that tool into a write and must be rejected
// before the host handler sees them.
func (s *CodingSubAgent) toolCallAllowedForRole(name string, args map[string]interface{}) (bool, string) {
	if s == nil {
		return false, "coding subagent is unavailable"
	}
	name = canonicalCodingSubAgentToolName(name)
	role := s.role
	if role == "" || role == codingRoleWorker {
		if !s.toolAllowedForRole(name) {
			return false, fmt.Sprintf("tool %s is not available for nested role %q", name, s.role)
		}
		return true, ""
	}
	allowed, ok := codingSubAgentSpawnRoleTools[role]
	if !ok {
		return false, fmt.Sprintf("tool %s is not available for nested role %q", name, s.role)
	}
	return (codingagent.ToolPolicy{Role: role, Allowed: allowed, Normalize: canonicalCodingSubAgentToolName}).IsToolCallAllowed(name, args)
}

func buildSpawnCodingAgentToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": codingSubAgentSpawnToolName,
			"description": "Spawn a nested coding subagent with a clean context (Codex-style). " +
				"Use for independent repository exploration so the parent context stays focused. " +
				"Roles: explorer (read-only search) or reviewer (read-only code review); worker is not available for nested execution. " +
				"Children cannot spawn further subagents. agents[] max 3; inspection children may run in parallel.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"role": map[string]interface{}{
						"type":        "string",
						"description": "Single-agent role: explorer | reviewer (default explorer). worker is reserved and rejected. Ignored when agents[] is set.",
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Single-agent task description (required unless agents[] is set).",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Optional extra context for the single agent (paths, constraints, acceptance).",
					},
					"agents": map[string]interface{}{
						"type":        "array",
						"description": "Optional parallel fan-out (max 3). Each item: {role, task, context?}.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"role":    map[string]interface{}{"type": "string", "description": "explorer | reviewer (worker is reserved)"},
								"task":    map[string]interface{}{"type": "string", "description": "What this agent should do"},
								"context": map[string]interface{}{"type": "string", "description": "Optional context"},
							},
							"required": []string{"task"},
						},
					},
				},
			},
		},
	}
}

type codingSpawnSpec struct {
	Role    codingSubAgentRole
	Task    string
	Context string
}

func codingSpawnStringArg(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	case float64:
		// JSON numbers sometimes leak through loose tool arg decoding.
		if t == float64(int64(t)) {
			return strings.TrimSpace(fmt.Sprintf("%d", int64(t)))
		}
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case bool:
		return fmt.Sprintf("%v", t)
	default:
		return ""
	}
}

func parseCodingSpawnSpecs(args map[string]interface{}) ([]codingSpawnSpec, error) {
	if args == nil {
		return nil, fmt.Errorf("missing arguments")
	}
	if raw, ok := args["agents"]; ok && raw != nil {
		list, ok := raw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("agents must be an array")
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("agents array is empty")
		}
		if len(list) > codingSubAgentMaxParallelSpawn {
			return nil, fmt.Errorf("too many agents (%d); max parallel is %d", len(list), codingSubAgentMaxParallelSpawn)
		}
		out := make([]codingSpawnSpec, 0, len(list))
		for i, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("agents[%d] must be an object", i)
			}
			task := codingSpawnStringArg(m["task"])
			if task == "" {
				return nil, fmt.Errorf("agents[%d].task is required", i)
			}
			role, err := parseCodingReadOnlySpawnRole(codingSpawnStringArg(m["role"]))
			if err != nil {
				return nil, fmt.Errorf("agents[%d]: %w", i, err)
			}
			out = append(out, codingSpawnSpec{
				Role:    role,
				Task:    task,
				Context: codingSpawnStringArg(m["context"]),
			})
		}
		return out, nil
	}
	task := codingSpawnStringArg(args["task"])
	if task == "" {
		return nil, fmt.Errorf("task is required (or pass agents[])")
	}
	role, err := parseCodingReadOnlySpawnRole(codingSpawnStringArg(args["role"]))
	if err != nil {
		return nil, err
	}
	return []codingSpawnSpec{{
		Role:    role,
		Task:    task,
		Context: codingSpawnStringArg(args["context"]),
	}}, nil
}

func parseCodingReadOnlySpawnRole(raw string) (codingSubAgentRole, error) {
	role, err := parseCodingSubAgentRole(raw)
	if err != nil {
		return "", err
	}
	if role == codingRoleWorker {
		return "", fmt.Errorf("worker nested subagents require an isolated write workspace and are not enabled")
	}
	return role, nil
}

// shouldParallelizeCodingSpawn may parallelize only inspection children. Their
// corelib admission policy is read-only, so they cannot write the workspace.
func shouldParallelizeCodingSpawn(specs []codingSpawnSpec) bool {
	if len(specs) <= 1 {
		return false
	}
	for _, s := range specs {
		if s.Role != codingRoleExplorer && s.Role != codingRoleReviewer {
			return false
		}
	}
	return true
}

func codingSpawnRoleMaxIterations(role codingSubAgentRole) int {
	switch role {
	case codingRoleExplorer:
		return 40
	case codingRoleReviewer:
		return 50
	default:
		return codingSubAgentPerTaskMaxIterations
	}
}

func codingSpawnRolePromptHint(role codingSubAgentRole) string {
	switch role {
	case codingRoleExplorer:
		return "你是只读探索子代理（explorer）：只搜索/阅读代码与文档，禁止写文件或改仓库。遇到陌生概念、精确报错、第三方依赖/API/协议或版本兼容性问题时，必须用 web_search 搜索并优先核对官方来源，不能凭记忆猜测。完成后给出结构化发现（关键路径、符号、外部来源、风险点）。"
	case codingRoleReviewer:
		return "你是审查/验证子代理（reviewer）：可读代码、跑 shell 检查与 git_diff，禁止 write/edit 改文件。涉及陌生或版本敏感的第三方事实时，必须用 web_search 核对；完成后给出问题清单、外部来源、验证结果与建议。"
	default:
		return "你是实现子代理（worker）：在干净上下文中完成指定实现/修复，改完后自行验证。你不能再 spawn 更深层子代理。"
	}
}

func (c *codingSubAgentCallbacks) executeSpawnCodingAgent(args map[string]interface{}) codingToolExecutionResult {
	if c == nil || c.subagent == nil {
		return codingToolExecutionResult{Text: "coding subagent is unavailable", Outcome: codingToolOutcomeFailed}
	}
	parent := c.subagent
	if !parent.canSpawnCodingAgent() {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("%s unavailable: only full coding workbench root can spawn (depth=%d full_env=%v role=%q)", codingSubAgentSpawnToolName, parent.nestDepth, parent.fullEnvironment, parent.role),
			Outcome: codingToolOutcomeBlocked,
		}
	}
	specs, err := parseCodingSpawnSpecs(args)
	if err != nil {
		return codingToolExecutionResult{Text: "spawn_coding_agent: " + err.Error(), Outcome: codingToolOutcomeFailed}
	}
	if parent.loopCtx != nil && parent.loopCtx.IsCancelled() {
		return codingToolExecutionResult{Text: "coding subagent cancelled before spawn", Outcome: codingToolOutcomeFailed}
	}
	if parent.runtimeAttempt != nil && parent.runtimeStore != nil {
		current, getErr := parent.runtimeStore.GetAttempt(parent.runtimeAttempt.AttemptID)
		if getErr != nil || current.Status != codingruntime.TaskRunning {
			return codingToolExecutionResult{Text: "spawn_coding_agent: runtime parent attempt is no longer running", Outcome: codingToolOutcomeFailed}
		}
	}
	// A nested child must be admitted through the corelib ledger whenever the
	// parent itself is a runtime attempt. The ledger closes the parent attempt
	// as waiting_child and releases its write lease before any child starts.
	// Legacy callers without a runtime attempt keep their existing safe,
	// in-process read-only behavior until their parent path is ledger-backed.
	if parent.runtimeAttempt != nil {
		return c.executeLedgerReadOnlySpawn(specs)
	}

	// Serialize progress/UI callbacks across nested agents (esp. parallel explorers).
	var progressMu sync.Mutex
	progress := func(msg string) {
		progressMu.Lock()
		defer progressMu.Unlock()
		emitCodingSubAgentProgress(parent.onProgress, msg)
	}
	childProgress := func(text string) {
		if parent.onProgress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		parent.onProgress(text)
	}

	parallel := shouldParallelizeCodingSpawn(specs)
	mode := "sequential"
	if parallel {
		mode = "parallel"
	}
	progress(fmt.Sprintf("spawn_coding_agent: launching %d nested agent(s) (%s)", len(specs), mode))

	type spawnOutcome struct {
		idx    int
		spec   codingSpawnSpec
		result *CodingSubAgentResult
	}
	outcomes := make([]spawnOutcome, len(specs))
	runOne := func(i int, spec codingSpawnSpec) {
		if parent.loopCtx != nil && parent.loopCtx.IsCancelled() {
			outcomes[i] = spawnOutcome{
				idx:  i,
				spec: spec,
				result: &CodingSubAgentResult{
					Status: TaskExecFailed,
					Error:  "coding subagent cancelled before nested agent start",
				},
			}
			return
		}
		progress(fmt.Sprintf("nested agent [%d/%d] role=%s starting", i+1, len(specs), spec.Role))
		res := parent.runNestedCodingAgent(spec, c, childProgress)
		outcomes[i] = spawnOutcome{idx: i, spec: spec, result: res}
		status := "unknown"
		if res != nil {
			status = string(res.Status)
		}
		progress(fmt.Sprintf("nested agent [%d/%d] role=%s finished status=%s", i+1, len(specs), spec.Role, status))
	}

	if parallel {
		var wg sync.WaitGroup
		for i, spec := range specs {
			wg.Add(1)
			go func(i int, spec codingSpawnSpec) {
				defer wg.Done()
				runOne(i, spec)
			}(i, spec)
		}
		wg.Wait()
	} else {
		for i, spec := range specs {
			runOne(i, spec)
		}
	}

	// Only pass the bounded child reports to the parent. Inspection children are
	// not allowed to produce file audits and their full transcript stays out of
	// the parent conversation.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("spawn_coding_agent completed: %d agent(s) mode=%s\n", len(outcomes), mode))
	anyFail := false
	passed := 0
	for _, o := range outcomes {
		res := o.result
		if res == nil {
			anyFail = true
			b.WriteString(fmt.Sprintf("\n### agent[%d] role=%s\nstatus=failed\nerror=nil result\n", o.idx, o.spec.Role))
			continue
		}
		if res.Status != TaskExecPassed {
			anyFail = true
		} else {
			passed++
		}
		b.WriteString(fmt.Sprintf("\n### agent[%d] role=%s task=%q\n", o.idx, o.spec.Role, truncateRunesForSubAgent(o.spec.Task, 120)))
		b.WriteString(fmt.Sprintf("status=%s iterations=%d tools=%d\n", res.Status, res.Iterations, res.ToolCalls))
		if res.Summary != "" {
			b.WriteString("summary:\n")
			b.WriteString(truncateRunesForSubAgent(res.Summary, 4000))
			b.WriteString("\n")
		}
		if res.Error != "" {
			b.WriteString("error: ")
			b.WriteString(compactSubAgentErrorSummary(res.Error))
			b.WriteString("\n")
		}
		if len(res.FilesModified) > 0 || len(res.FilesCreated) > 0 {
			anyFail = true
			b.WriteString("error: inspection child reported workspace writes\n")
		}
	}
	b.WriteString(fmt.Sprintf("\npassed=%d failed=%d\n", passed, len(outcomes)-passed))
	out := strings.TrimSpace(b.String())
	if anyFail {
		return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeFailed}
	}
	return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeSuccess}
}

func (c *codingSubAgentCallbacks) executeLedgerReadOnlySpawn(specs []codingSpawnSpec) codingToolExecutionResult {
	if c == nil || c.subagent == nil || c.subagent.runtimeAttempt == nil || c.subagent.runtimeStore == nil {
		return codingToolExecutionResult{Text: "runtime child admission is unavailable", Outcome: codingToolOutcomeFailed}
	}
	parent := c.subagent
	policy := codingruntime.PolicySnapshot{
		Digest:      codingRuntimeDigest(parent.projectPath + "\nreadonly-child"),
		ProjectRoot: parent.projectPath,
		Mode:        "local",
		ReadOnly:    true,
	}
	childSpecs := make([]codingruntime.ChildTaskSpec, 0, len(specs))
	for _, spec := range specs {
		childSpecs = append(childSpecs, codingruntime.ChildTaskSpec{Name: string(spec.Role), RequestedWork: spec.Task, ProjectRef: parent.projectPath, Mode: "local"})
	}
	service := codingruntime.ChildTaskService{Store: parent.runtimeStore}
	handles, err := service.AdmitReadOnlyChildren(parent.runtimeAttempt.AttemptID, parent.runtimeAttempt.LeaseOwner, childSpecs, policy)
	if err != nil {
		return codingToolExecutionResult{Text: "spawn_coding_agent: runtime admission failed: " + err.Error(), Outcome: codingToolOutcomeFailed}
	}
	// Admission is the completion boundary for the parent tool call. Child
	// execution happens independently, and only its bounded ledger result is
	// available to a later parent Attempt. Do not wait here: waiting would keep
	// the old parent loop alive and collapse the durable handoff back into an
	// in-process nested conversation.
	store := parent.runtimeStore
	for i, handle := range handles {
		spec := specs[i]
		child := parent.newReadOnlyNestedCodingAgent(spec, c)
		go runAdmittedLocalReadOnlyChild(store, parent.runtimeAttempt.TaskID, handle, policy, child, parent.onProgress)
	}
	return codingToolExecutionResult{Text: formatAdmittedReadOnlyChildHandles(handles), Outcome: codingToolOutcomeSuccess}
}

// runAdmittedLocalReadOnlyChild is intentionally detached from the parent
// LoopContext. A parent loop ends immediately after admission; binding child
// execution to that stack would turn normal handoff into an accidental cancel.
// Explicit cancellation/recovery remains ledger-driven and starts a new
// Attempt rather than replaying this child.
func runAdmittedLocalReadOnlyChild(store codingruntime.Store, parentTaskID string, handle codingruntime.ChildTaskHandle, policy codingruntime.PolicySnapshot, child *CodingSubAgent, onProgress func(string)) {
	if store == nil || child == nil {
		return
	}
	ctx, release := guiAdmittedChildExecutions.Begin(parentTaskID, handle.TaskID)
	defer release()
	if onProgress != nil {
		emitCodingSubAgentProgress(onProgress, fmt.Sprintf("read-only child admitted: %s (%s)", handle.TaskID, handle.Name))
	}
	service := codingruntime.ChildTaskService{Store: store}
	runner := codingruntime.Runner{Store: store, LeaseOwner: "gui:child:" + handle.TaskID, LeaseDuration: 15 * time.Minute}
	outcome := <-service.StartReadOnlyChild(ctx, runner, handle.TaskID, policy, child)
	if onProgress == nil {
		return
	}
	if outcome.Err != nil {
		emitCodingSubAgentProgress(onProgress, fmt.Sprintf("read-only child %s failed: %s", handle.TaskID, compactSubAgentErrorSummary(outcome.Err.Error())))
		return
	}
	status := codingruntime.TaskFailed
	if outcome.Attempt != nil {
		status = outcome.Attempt.Status
	}
	emitCodingSubAgentProgress(onProgress, fmt.Sprintf("read-only child %s completed: %s", handle.TaskID, status))
}

func formatAdmittedReadOnlyChildHandles(handles []codingruntime.ChildTaskHandle) string {
	lines := make([]string, 0, len(handles)+1)
	lines = append(lines, "spawn_coding_agent admitted read-only child task(s); parent attempt released its lease:")
	for _, handle := range handles {
		lines = append(lines, fmt.Sprintf("- task_id=%s name=%s status=%s", handle.TaskID, handle.Name, handle.Status))
	}
	lines = append(lines, "Child results will be persisted as bounded summaries/evidence digests; a fresh parent attempt must explicitly review them.")
	return strings.Join(lines, "\n")
}

func (parent *CodingSubAgent) newReadOnlyNestedCodingAgent(spec codingSpawnSpec, parentCB *codingSubAgentCallbacks) *CodingSubAgent {
	child := NewCodingSubAgent(parent.handler, parent.cfg, parent.httpClient, parent.projectPath, parent.loopCtx)
	child.nestDepth, child.role = parent.nestDepth+1, spec.Role
	// The detached child receives its own Attempt in ExecuteReadOnlyChild; the
	// shared Store is only for observing that fresh Attempt's cancellation.
	child.runtimeStore = parent.runtimeStore
	child.codingKB, child.generalKB = parent.codingKB, parent.generalKB
	if parent.scopeApproval != nil {
		child.scopeApproval = parent.scopeApproval
	}
	if parentCB != nil && parentCB.designCtx != "" {
		// The actual prompt stays host-local; corelib stores only ChildTaskSpec.
		child.SetCallbacks(nil, parent.onProgress)
	}
	return child
}

func (c *codingSubAgentCallbacks) mergeSpawnedFileAudit(modified, created []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.filesModified == nil {
		c.filesModified = make(map[string]bool)
	}
	if c.filesCreated == nil {
		c.filesCreated = make(map[string]bool)
	}
	for _, p := range modified {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		c.filesModified[c.displayProjectPath(p)] = true
	}
	for _, p := range created {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		c.filesCreated[c.displayProjectPath(p)] = true
	}
}

func (parent *CodingSubAgent) runNestedCodingAgent(spec codingSpawnSpec, parentCB *codingSubAgentCallbacks, onProgress func(string)) *CodingSubAgentResult {
	if parent == nil {
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: "parent coding subagent is nil"}
	}
	child := NewCodingSubAgent(parent.handler, parent.cfg, parent.httpClient, parent.projectPath, parent.loopCtx)
	child.nestDepth = parent.nestDepth + 1
	child.role = spec.Role
	// Inspection children deliberately stay lean and role-filtered. Worker is
	// rejected before this point until an isolated write-child design exists.
	// Progress only — avoid flooding parent stream with child token deltas.
	// onProgress is expected to be concurrency-safe when parallel explorers run.
	if onProgress == nil {
		onProgress = parent.onProgress
	}
	child.SetCallbacks(nil, onProgress)
	if parent.scopeApproval != nil {
		// Shared, mutex-protected approval state (scopeApprovalState.mu).
		child.scopeApproval = parent.scopeApproval
	}
	child.codingKB = parent.codingKB
	child.generalKB = parent.generalKB

	taskTitle := fmt.Sprintf("[%s] %s", spec.Role, truncateRunesForSubAgent(spec.Task, 80))
	task := &TaskItem{
		Index:       1,
		Title:       taskTitle,
		Description: spec.Task,
		Status:      TaskExecPending,
	}

	reqCtx := codingSpawnRolePromptHint(spec.Role)
	if parentCB != nil && parentCB.reqCtx != "" {
		reqCtx += "\n\n## Parent request context\n" + truncateRunesForSubAgent(parentCB.reqCtx, 1500)
	}
	if spec.Context != "" {
		reqCtx += "\n\n## Spawn context\n" + truncateRunesForSubAgent(spec.Context, 2000)
	}
	designCtx := ""
	if parentCB != nil {
		designCtx = parentCB.designCtx
	}
	var prev []string
	if parentCB != nil && len(parentCB.prevOutputs) > 0 {
		// Copy a short tail so parallel children never share a mutated slice header.
		src := parentCB.prevOutputs
		if len(src) > 3 {
			src = src[len(src)-3:]
		}
		prev = append([]string(nil), src...)
	}

	log.Printf("[coding-subagent-spawn] start role=%s depth=%d task=%q project=%s",
		spec.Role, child.nestDepth, truncateRunesForSubAgent(spec.Task, 80), parent.projectPath)
	result := child.ExecuteTask(task, reqCtx, designCtx, prev)
	if result == nil {
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: "nested coding agent returned nil"}
	}
	log.Printf("[coding-subagent-spawn] done role=%s status=%s iters=%d tools=%d err=%q",
		spec.Role, result.Status, result.Iterations, result.ToolCalls, compactSubAgentErrorSummary(result.Error))
	return result
}
