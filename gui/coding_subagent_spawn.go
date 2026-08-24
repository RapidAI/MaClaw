package main

// coding_subagent_spawn.go — Codex-style nested subagents for pure coding workbench.
//
// The root full-environment CodingSubAgent (create-task coding_dev) can spawn
// specialized child agents with a clean context. Children cannot spawn further
// (nest depth hard cap), matching Codex depth control.

import (
	"fmt"
	"log"
	"path/filepath"
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
	// Isolated writers may share one spawn only when write-sets are disjoint.
	codingSubAgentMaxParallelWorkers = 2
)

// codingSubAgentRole is retained as a GUI-facing alias while the canonical
// role vocabulary lives in corelib for TUI and MaClawSrv reuse.
type codingSubAgentRole = codingagent.Role

const (
	codingRoleWorker   = codingagent.RoleWorker   // isolated worktree implementation child
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
	// Only the full-environment root may spawn. Inspection children stay
	// read-only; local workers run in isolated worktrees and merge back.
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
				"Use explorer/reviewer for independent repository inspection, or worker for an isolated implementation child. " +
				"worker requires files[] (exact write-set) and runs in a git worktree isolate. " +
				"Two local workers may run in parallel only when their files do not overlap; merge stays sequential. " +
				"Remote workers always run sequentially. Children cannot spawn further subagents. " +
				"agents[] max 3; inspection-only batches may run in parallel.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"role": map[string]interface{}{
						"type":        "string",
						"description": "Single-agent role: explorer | reviewer | worker (default explorer). Ignored when agents[] is set.",
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Single-agent task description (required unless agents[] is set).",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Optional extra context for the single agent (paths, constraints, acceptance).",
					},
					"files": map[string]interface{}{
						"type":        "array",
						"description": "Required for worker: exact files or directories this child may change. No wildcards.",
						"items":       map[string]interface{}{"type": "string"},
					},
					"agents": map[string]interface{}{
						"type":        "array",
						"description": "Optional fan-out (max 3). Each item: {role, task, context?, files?}. Two local workers run in parallel only when files do not overlap; remote workers stay sequential.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"role":    map[string]interface{}{"type": "string", "description": "explorer | reviewer | worker"},
								"task":    map[string]interface{}{"type": "string", "description": "What this agent should do"},
								"context": map[string]interface{}{"type": "string", "description": "Optional context"},
								"files":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Required for worker"},
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
	Role        codingSubAgentRole
	Task        string
	Context     string
	Files       []string
	projectPath string // required isolate root for a worker; empty is fail-closed
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
			role, err := parseCodingSpawnRole(codingSpawnStringArg(m["role"]))
			if err != nil {
				return nil, fmt.Errorf("agents[%d]: %w", i, err)
			}
			files, err := parseCodingSpawnFiles(m["files"])
			if err != nil {
				return nil, fmt.Errorf("agents[%d]: %w", i, err)
			}
			if err := requireWorkerSpawnFiles(role, files); err != nil {
				return nil, fmt.Errorf("agents[%d]: %w", i, err)
			}
			out = append(out, codingSpawnSpec{
				Role:    role,
				Task:    task,
				Context: codingSpawnStringArg(m["context"]),
				Files:   files,
			})
		}
		return out, nil
	}
	task := codingSpawnStringArg(args["task"])
	if task == "" {
		return nil, fmt.Errorf("task is required (or pass agents[])")
	}
	role, err := parseCodingSpawnRole(codingSpawnStringArg(args["role"]))
	if err != nil {
		return nil, err
	}
	files, err := parseCodingSpawnFiles(args["files"])
	if err != nil {
		return nil, err
	}
	if err := requireWorkerSpawnFiles(role, files); err != nil {
		return nil, err
	}
	return []codingSpawnSpec{{
		Role:    role,
		Task:    task,
		Context: codingSpawnStringArg(args["context"]),
		Files:   files,
	}}, nil
}

func parseCodingSpawnRole(raw string) (codingSubAgentRole, error) {
	if strings.TrimSpace(raw) == "" {
		return codingRoleExplorer, nil
	}
	return parseCodingSubAgentRole(raw)
}

func parseCodingSpawnFiles(raw interface{}) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(typed))
		for i, item := range typed {
			path := codingSpawnStringArg(item)
			if path == "" {
				return nil, fmt.Errorf("files[%d] must be a non-empty path", i)
			}
			out = append(out, path)
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(typed))
		for i, item := range typed {
			path := strings.TrimSpace(item)
			if path == "" {
				return nil, fmt.Errorf("files[%d] must be a non-empty path", i)
			}
			out = append(out, path)
		}
		return out, nil
	case string:
		path := strings.TrimSpace(typed)
		if path == "" {
			return nil, nil
		}
		return []string{path}, nil
	default:
		return nil, fmt.Errorf("files must be an array of paths")
	}
}

func requireWorkerSpawnFiles(role codingSubAgentRole, files []string) error {
	if role != codingRoleWorker {
		return nil
	}
	if len(files) == 0 {
		return fmt.Errorf("worker requires files write-set")
	}
	return nil
}

func codingSpawnHasWorker(specs []codingSpawnSpec) bool {
	for _, spec := range specs {
		if spec.Role == codingRoleWorker {
			return true
		}
	}
	return false
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
		return "你是实现子代理（worker）：在隔离 git worktree 中完成指定实现/修复，只改声明过的 files，改完后自行验证。你不能再 spawn 更深层子代理。"
	}
}

func isolatedWorkerWriteSets(projectPath string, specs []codingSpawnSpec) ([]codingruntime.WriteSet, error) {
	scope := codingruntime.WriteScope{Mode: "local", ProjectRef: projectPath}
	out := make([]codingruntime.WriteSet, 0, len(specs))
	for i, spec := range specs {
		if spec.Role != codingRoleWorker {
			continue
		}
		set, err := codingruntime.NormalizeWriteSet(scope, spec.Files)
		if err != nil {
			return nil, fmt.Errorf("agent[%d]: %w", i, err)
		}
		if set.Unknown || len(set.Claims) == 0 {
			return nil, fmt.Errorf("agent[%d]: worker requires a concrete files write-set", i)
		}
		out = append(out, set)
	}
	return out, nil
}

func allCodingSpawnWorkers(specs []codingSpawnSpec) bool {
	if len(specs) == 0 {
		return false
	}
	for _, spec := range specs {
		if spec.Role != codingRoleWorker {
			return false
		}
	}
	return true
}

func canParallelizeIsolatedWriteSets(sets []codingruntime.WriteSet) bool {
	if len(sets) != codingSubAgentMaxParallelWorkers {
		return false
	}
	return !codingruntime.CanAdmitParallelWriters(sets[0], sets[1], true, true, true).Conflicts
}

func canParallelizeIsolatedWorkers(projectPath string, specs []codingSpawnSpec) bool {
	if len(specs) != codingSubAgentMaxParallelWorkers || !allCodingSpawnWorkers(specs) {
		return false
	}
	sets, err := isolatedWorkerWriteSets(projectPath, specs)
	if err != nil {
		return false
	}
	return canParallelizeIsolatedWriteSets(sets)
}

func validateIsolatedWorkerSpecs(projectPath string, specs []codingSpawnSpec) error {
	_, err := isolatedWorkerWriteSets(projectPath, specs)
	return err
}

func newCodingSpawnProgress(onProgress func(string)) (progress, child func(string)) {
	var mu sync.Mutex
	progress = func(msg string) {
		mu.Lock()
		defer mu.Unlock()
		emitCodingSubAgentProgress(onProgress, msg)
	}
	child = func(text string) {
		if onProgress == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		onProgress(text)
	}
	return progress, child
}

func attachSpawnChildError(existing, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return extra
	}
	return compactSubAgentErrorSummary(existing) + "; " + extra
}

func codingSpawnWorkerIsolatePath(spec codingSpawnSpec) (string, error) {
	if spec.Role != codingRoleWorker {
		return strings.TrimSpace(spec.projectPath), nil
	}
	projectPath := strings.TrimSpace(spec.projectPath)
	if projectPath == "" {
		return "", fmt.Errorf("worker spawn requires an isolated workspace")
	}
	return projectPath, nil
}

func codingSpawnLocalPathsEqual(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	absLeft, errLeft := filepath.Abs(left)
	absRight, errRight := filepath.Abs(right)
	if errLeft != nil || errRight != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(absLeft) == filepath.Clean(absRight)
}

func isolatedWorkerShouldKeepWorktree(wt *codingWorkbenchWorktree, res *CodingSubAgentResult) bool {
	if res != nil && (len(res.FilesModified) > 0 || len(res.FilesCreated) > 0) {
		return true
	}
	dirty, probed := wt.hasLocalChanges()
	if !probed {
		return true
	}
	return dirty
}

func validateRemoteIsolatedWorkerSpecs(projectDir string, specs []codingSpawnSpec) error {
	if err := validateIsolatedWorkerSpecs(projectDir, specs); err != nil {
		return err
	}
	for i, spec := range specs {
		if spec.Role != codingRoleWorker {
			continue
		}
		if err := validateRemoteIsolateWriteClaims(spec.Files); err != nil {
			return fmt.Errorf("agent[%d]: %w", i, err)
		}
	}
	return nil
}

func codingSpawnBatchHeader(label string, count, passed, failed int, mode string) string {
	if failed > 0 {
		return fmt.Sprintf("错误: %s 有子代理失败 passed=%d failed=%d mode=%s\n", label, passed, failed, mode)
	}
	return fmt.Sprintf("%s completed: %d agent(s) mode=%s\n", label, count, mode)
}

// codingSpawnRemoteFailure prefixes a remote spawn error so
// remoteCodingToolOutcome classifies it as failed. Local spawn already
// returns codingToolOutcomeFailed; remote only has the result string.
func codingSpawnRemoteFailure(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "错误: spawn_coding_agent failed"
	}
	if strings.HasPrefix(msg, "错误") {
		return msg
	}
	return "错误: " + msg
}

func appendCodingSpawnChildReport(b *strings.Builder, idx int, role codingSubAgentRole, task, status string, iterations, toolCalls int, mergeNote, summary, errText string, modified, created []string) {
	if b == nil {
		return
	}
	b.WriteString(fmt.Sprintf("\n### agent[%d] role=%s task=%q\n", idx, role, truncateRunesForSubAgent(task, 120)))
	b.WriteString(fmt.Sprintf("status=%s iterations=%d tools=%d\n", status, iterations, toolCalls))
	if mergeNote != "" {
		b.WriteString("merge: ")
		b.WriteString(mergeNote)
		b.WriteString("\n")
	}
	if summary != "" {
		b.WriteString("summary:\n")
		b.WriteString(truncateRunesForSubAgent(summary, 4000))
		b.WriteString("\n")
	}
	if errText != "" {
		b.WriteString("error: ")
		b.WriteString(compactSubAgentErrorSummary(errText))
		b.WriteString("\n")
	}
	if len(modified) > 0 {
		b.WriteString("files_modified: ")
		b.WriteString(strings.Join(modified, ", "))
		b.WriteString("\n")
	}
	if len(created) > 0 {
		b.WriteString("files_created: ")
		b.WriteString(strings.Join(created, ", "))
		b.WriteString("\n")
	}
}

func markLocalInspectionSpawnWriteFailure(role codingSubAgentRole, res *CodingSubAgentResult) {
	if res == nil || role == codingRoleWorker {
		return
	}
	if len(res.FilesModified) == 0 && len(res.FilesCreated) == 0 {
		return
	}
	res.Status = TaskExecFailed
	msg := "inspection child reported workspace writes"
	if strings.TrimSpace(res.Error) == "" {
		res.Error = msg
		return
	}
	res.Error = compactSubAgentErrorSummary(res.Error) + "; " + msg
}

func markRemoteInspectionSpawnWriteFailure(role codingSubAgentRole, res *RemoteCodingSubAgentResult) {
	if res == nil || role == codingRoleWorker {
		return
	}
	if len(res.FilesModified) == 0 && len(res.FilesCreated) == 0 {
		return
	}
	res.Status = "failed"
	msg := "inspection child reported workspace writes"
	if strings.TrimSpace(res.Error) == "" {
		res.Error = msg
		return
	}
	res.Error = compactSubAgentErrorSummary(res.Error) + "; " + msg
}

func (c *codingSubAgentCallbacks) executeIsolatedWorkerSpawn(specs []codingSpawnSpec) codingToolExecutionResult {
	if c == nil || c.subagent == nil {
		return codingToolExecutionResult{Text: "coding subagent is unavailable", Outcome: codingToolOutcomeFailed}
	}
	parent := c.subagent
	sets, err := isolatedWorkerWriteSets(parent.projectPath, specs)
	if err != nil {
		return codingToolExecutionResult{Text: "spawn_coding_agent: " + err.Error(), Outcome: codingToolOutcomeFailed}
	}
	if allCodingSpawnWorkers(specs) && canParallelizeIsolatedWriteSets(sets) {
		return c.executeParallelIsolatedWorkerSpawn(specs)
	}

	progress, childProgress := newCodingSpawnProgress(parent.onProgress)
	progress(fmt.Sprintf("spawn_coding_agent: launching %d nested agent(s) (sequential isolated worker)", len(specs)))
	var body strings.Builder
	passed := 0
	for i, spec := range specs {
		if parent.loopCtx != nil && parent.loopCtx.IsCancelled() {
			appendCodingSpawnChildReport(&body, i, spec.Role, spec.Task, string(TaskExecFailed), 0, 0, "", "", "coding subagent cancelled before nested agent start", nil, nil)
			continue
		}
		progress(fmt.Sprintf("nested agent [%d/%d] role=%s starting", i+1, len(specs), spec.Role))
		res, mergeNote, err := c.runIsolatedOrInspectionSpawn(spec, i, childProgress)
		if res == nil {
			res = &CodingSubAgentResult{Status: TaskExecFailed, Error: "nested coding agent returned nil"}
		}
		if err != nil {
			res.Status = TaskExecFailed
			res.Error = attachSpawnChildError(res.Error, err.Error())
		}
		markLocalInspectionSpawnWriteFailure(spec.Role, res)
		if res.Status == TaskExecPassed {
			passed++
		}
		progress(fmt.Sprintf("nested agent [%d/%d] role=%s finished status=%s", i+1, len(specs), spec.Role, res.Status))
		appendCodingSpawnChildReport(&body, i, spec.Role, spec.Task, string(res.Status), res.Iterations, res.ToolCalls, mergeNote, res.Summary, res.Error, res.FilesModified, res.FilesCreated)
	}
	failed := len(specs) - passed
	out := strings.TrimSpace(codingSpawnBatchHeader("spawn_coding_agent", len(specs), passed, failed, "sequential") + body.String() + fmt.Sprintf("\npassed=%d failed=%d\n", passed, failed))
	if failed > 0 {
		return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeFailed}
	}
	return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeSuccess}
}

func (c *codingSubAgentCallbacks) executeParallelIsolatedWorkerSpawn(specs []codingSpawnSpec) codingToolExecutionResult {
	if c == nil || c.subagent == nil {
		return codingToolExecutionResult{Text: "coding subagent is unavailable", Outcome: codingToolOutcomeFailed}
	}
	parent := c.subagent
	progress, childProgress := newCodingSpawnProgress(parent.onProgress)

	type workerItem struct {
		spec      codingSpawnSpec
		wt        *codingWorkbenchWorktree
		result    *CodingSubAgentResult
		mergeNote string
	}
	items := make([]workerItem, len(specs))
	created := make([]*codingWorkbenchWorktree, 0, len(specs))
	for i, spec := range specs {
		items[i].spec = spec
		wt, err := createCodingWorkbenchWorktree(parent.projectPath, i+1, "spawn-worker")
		if err != nil {
			cleanupCodingWorkbenchWorktrees(created, false)
			return codingToolExecutionResult{Text: "spawn_coding_agent: create isolated worktree: " + err.Error(), Outcome: codingToolOutcomeFailed}
		}
		if wt == nil {
			cleanupCodingWorkbenchWorktrees(created, false)
			return codingToolExecutionResult{Text: "spawn_coding_agent: worker spawn requires a git repository with at least one commit", Outcome: codingToolOutcomeFailed}
		}
		if strings.TrimSpace(wt.ProjectPath) == "" {
			wt.cleanup(false)
			cleanupCodingWorkbenchWorktrees(created, false)
			return codingToolExecutionResult{Text: "spawn_coding_agent: worker spawn requires an isolated workspace", Outcome: codingToolOutcomeFailed}
		}
		items[i].wt = wt
		items[i].spec.projectPath = wt.ProjectPath
		created = append(created, wt)
	}

	progress(fmt.Sprintf("spawn_coding_agent: launching %d nested agent(s) (parallel isolated worker)", len(specs)))
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if parent.loopCtx != nil && parent.loopCtx.IsCancelled() {
				items[i].result = &CodingSubAgentResult{Status: TaskExecFailed, Error: "coding subagent cancelled before nested agent start"}
				return
			}
			progress(fmt.Sprintf("nested agent [%d/%d] role=%s starting", i+1, len(items), items[i].spec.Role))
			items[i].result = parent.runNestedCodingAgent(items[i].spec, c, childProgress)
			status := "unknown"
			if items[i].result != nil {
				status = string(items[i].result.Status)
			}
			progress(fmt.Sprintf("nested agent [%d/%d] role=%s finished status=%s", i+1, len(items), items[i].spec.Role, status))
		}(i)
	}
	wg.Wait()

	mergeBlocked := false
	var body strings.Builder
	passed := 0
	for i := range items {
		res := items[i].result
		if res == nil {
			res = &CodingSubAgentResult{Status: TaskExecFailed, Error: "nested coding agent returned nil"}
			items[i].result = res
		}
		if items[i].wt != nil {
			if res.Status != TaskExecPassed || mergeBlocked {
				keep := isolatedWorkerShouldKeepWorktree(items[i].wt, res)
				if mergeBlocked && res.Status == TaskExecPassed {
					res.Status = TaskExecFailed
					res.Error = attachSpawnChildError(res.Error, "merge skipped after earlier isolated-worker merge failure")
					keep = true
				}
				items[i].wt.cleanup(keep)
			} else if merged, note, err := items[i].wt.mergeBack(parent.projectPath, items[i].spec.Files); err != nil {
				items[i].mergeNote = note
				res.Status = TaskExecFailed
				res.Error = attachSpawnChildError(res.Error, err.Error())
				items[i].wt.cleanup(true)
				mergeBlocked = true
			} else {
				items[i].mergeNote = note
				items[i].wt.cleanup(false)
				if res.FilesModified != nil {
					res.FilesModified = remapWorktreePaths(res.FilesModified, items[i].spec.projectPath, parent.projectPath)
				}
				if res.FilesCreated != nil {
					res.FilesCreated = remapWorktreePaths(res.FilesCreated, items[i].spec.projectPath, parent.projectPath)
				}
				c.mergeSpawnedFileAudit(res.FilesModified, res.FilesCreated)
				if !merged && strings.TrimSpace(items[i].mergeNote) == "" {
					items[i].mergeNote = "worktree produced no mergeable file changes"
				}
			}
		}
		if res.Status == TaskExecPassed {
			passed++
		}
		appendCodingSpawnChildReport(&body, i, items[i].spec.Role, items[i].spec.Task, string(res.Status), res.Iterations, res.ToolCalls, items[i].mergeNote, res.Summary, res.Error, res.FilesModified, res.FilesCreated)
	}
	failed := len(items) - passed
	out := strings.TrimSpace(codingSpawnBatchHeader("spawn_coding_agent", len(items), passed, failed, "parallel") + body.String() + fmt.Sprintf("\npassed=%d failed=%d\n", passed, failed))
	if failed > 0 {
		return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeFailed}
	}
	return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeSuccess}
}

func cleanupCodingWorkbenchWorktrees(trees []*codingWorkbenchWorktree, keep bool) {
	for _, wt := range trees {
		if wt != nil {
			wt.cleanup(keep)
		}
	}
}

func (c *codingSubAgentCallbacks) runIsolatedOrInspectionSpawn(spec codingSpawnSpec, index int, onProgress func(string)) (*CodingSubAgentResult, string, error) {
	parent := c.subagent
	if spec.Role != codingRoleWorker {
		return parent.runNestedCodingAgent(spec, c, onProgress), "", nil
	}
	wt, err := createCodingWorkbenchWorktree(parent.projectPath, index+1, "spawn-worker")
	if err != nil {
		return nil, "", fmt.Errorf("create isolated worktree: %w", err)
	}
	if wt == nil {
		return nil, "", fmt.Errorf("worker spawn requires a git repository with at least one commit")
	}
	if strings.TrimSpace(wt.ProjectPath) == "" {
		wt.cleanup(false)
		return nil, "", fmt.Errorf("worker spawn requires an isolated workspace")
	}
	spec.projectPath = wt.ProjectPath
	result := parent.runNestedCodingAgent(spec, c, onProgress)
	if result == nil {
		wt.cleanup(isolatedWorkerShouldKeepWorktree(wt, nil))
		return nil, "", fmt.Errorf("nested coding agent returned nil")
	}
	if result.Status != TaskExecPassed {
		wt.cleanup(isolatedWorkerShouldKeepWorktree(wt, result))
		return result, "", nil
	}
	merged, mergeNote, mergeErr := wt.mergeBack(parent.projectPath, spec.Files)
	if mergeErr != nil {
		wt.cleanup(true)
		return result, mergeNote, mergeErr
	}
	wt.cleanup(false)
	if result.FilesModified != nil {
		result.FilesModified = remapWorktreePaths(result.FilesModified, spec.projectPath, parent.projectPath)
	}
	if result.FilesCreated != nil {
		result.FilesCreated = remapWorktreePaths(result.FilesCreated, spec.projectPath, parent.projectPath)
	}
	c.mergeSpawnedFileAudit(result.FilesModified, result.FilesCreated)
	if !merged && strings.TrimSpace(mergeNote) == "" {
		mergeNote = "worktree produced no mergeable file changes"
	}
	return result, mergeNote, nil
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
	// Isolated workers stay in-process: the parent waits, keeps its lease, and
	// merges the worktree before returning. Inspection children with a Runtime
	// Attempt still use durable Ledger admission.
	if codingSpawnHasWorker(specs) {
		return c.executeIsolatedWorkerSpawn(specs)
	}
	if parent.runtimeAttempt != nil {
		return c.executeLedgerReadOnlySpawn(specs)
	}

	progress, childProgress := newCodingSpawnProgress(parent.onProgress)
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
	var body strings.Builder
	passed := 0
	for _, o := range outcomes {
		res := o.result
		if res == nil {
			appendCodingSpawnChildReport(&body, o.idx, o.spec.Role, o.spec.Task, string(TaskExecFailed), 0, 0, "", "", "nil result", nil, nil)
			continue
		}
		markLocalInspectionSpawnWriteFailure(o.spec.Role, res)
		if res.Status == TaskExecPassed {
			passed++
		}
		appendCodingSpawnChildReport(&body, o.idx, o.spec.Role, o.spec.Task, string(res.Status), res.Iterations, res.ToolCalls, "", res.Summary, res.Error, res.FilesModified, res.FilesCreated)
	}
	failed := len(outcomes) - passed
	out := strings.TrimSpace(codingSpawnBatchHeader("spawn_coding_agent", len(outcomes), passed, failed, mode) + body.String() + fmt.Sprintf("\npassed=%d failed=%d\n", passed, failed))
	if failed > 0 {
		return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeFailed}
	}
	return codingToolExecutionResult{Text: out, Outcome: codingToolOutcomeSuccess}
}

func (c *codingSubAgentCallbacks) executeLedgerReadOnlySpawn(specs []codingSpawnSpec) codingToolExecutionResult {
	if c == nil || c.subagent == nil || c.subagent.runtimeAttempt == nil || c.subagent.runtimeStore == nil {
		return codingToolExecutionResult{Text: "runtime child admission is unavailable", Outcome: codingToolOutcomeFailed}
	}
	if codingSpawnHasWorker(specs) {
		return codingToolExecutionResult{Text: "spawn_coding_agent: inspection ledger admission cannot run worker children", Outcome: codingToolOutcomeFailed}
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
	childExecution := newCodingChildExecutionContext(parent.loopCtx, parent.httpClient, true)
	child := NewCodingSubAgent(parent.handler, parent.cfg, parent.httpClient, parent.projectPath, childExecution.loopCtx)
	child.nestedLoopRelease = childExecution.release
	// Child execution needs a fresh trusted turn anchor. Do not copy the
	// parent's identity here: child runtime admission must resolve its own
	// durable attempt mapping, otherwise a stale parent identity could cross an
	// explicit child/review boundary.
	child.nestDepth, child.role = parent.nestDepth+1, spec.Role
	// The detached child receives its own Attempt in ExecuteReadOnlyChild; the
	// shared Store is only for observing that fresh Attempt's cancellation.
	child.runtimeStore = parent.runtimeStore
	child.codingKB, child.generalKB = parent.codingKB, parent.generalKB
	// The actual prompt stays host-local; corelib stores only ChildTaskSpec.
	// Progress is explicitly passed and has no authority over the child scope.
	child.SetCallbacks(nil, parent.onProgress)
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
	// A synchronous nested execution replaces the parent's live model turn.
	// If a future qualified S1-C relay owns a reservation, retire it before the
	// child receives its fresh runtime attempt. This does not transfer identity
	// or create a new relay; the child resolves its own verified ingress later.
	parent.closeCodingSubAgentDynamicLifecycle(codingBoundDynamicRequestNestedExit)
	projectPath := strings.TrimSpace(spec.projectPath)
	if spec.Role == codingRoleWorker {
		isolate, err := codingSpawnWorkerIsolatePath(spec)
		if err != nil {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: err.Error()}
		}
		if codingSpawnLocalPathsEqual(isolate, parent.projectPath) {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: "worker isolate must not be the primary project"}
		}
		projectPath = isolate
	} else if projectPath == "" {
		projectPath = parent.projectPath
	}
	childExecution := newCodingChildExecutionContext(parent.loopCtx, parent.httpClient, false)
	child := NewCodingSubAgent(parent.handler, parent.cfg, parent.httpClient, projectPath, childExecution.loopCtx)
	defer childExecution.release()
	// See newReadOnlyNestedCodingAgent: identity is resolved only after the
	// child has its own ledger Attempt and anchor registration.
	child.nestDepth = parent.nestDepth + 1
	child.role = spec.Role
	if spec.Role == codingRoleWorker {
		child.SetFullEnvironment(true)
		child.setNestedWorkerScopeApproval(nil)
	}
	// Inspection children stay lean and role-filtered. Workers write only inside
	// an isolated worktree supplied by the parent spawn path.
	// Progress only — avoid flooding parent stream with child token deltas.
	// onProgress is expected to be concurrency-safe when parallel explorers run.
	if onProgress == nil {
		onProgress = parent.onProgress
	}
	child.SetCallbacks(nil, onProgress)
	child.codingKB = parent.codingKB
	child.generalKB = parent.generalKB

	taskTitle := fmt.Sprintf("[%s] %s", spec.Role, truncateRunesForSubAgent(spec.Task, 80))
	task := &TaskItem{
		Index:       1,
		Title:       taskTitle,
		Description: spec.Task,
		Files:       append([]string(nil), spec.Files...),
		Status:      TaskExecPending,
	}

	reqCtx := codingSpawnRolePromptHint(spec.Role)
	if parentCB != nil && parentCB.reqCtx != "" {
		reqCtx += "\n\n## Parent request context\n" + truncateRunesForSubAgent(parentCB.reqCtx, 1500)
	}
	if spec.Context != "" {
		reqCtx += "\n\n## Spawn context\n" + truncateRunesForSubAgent(spec.Context, 2000)
	}
	if spec.Role == codingRoleWorker && len(spec.Files) > 0 {
		reqCtx += "\n\n## Declared write-set\n" + strings.Join(spec.Files, "\n")
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
		spec.Role, child.nestDepth, truncateRunesForSubAgent(spec.Task, 80), projectPath)
	result := child.ExecuteTask(task, reqCtx, designCtx, prev)
	if result == nil {
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: "nested coding agent returned nil"}
	}
	log.Printf("[coding-subagent-spawn] done role=%s status=%s iters=%d tools=%d err=%q",
		spec.Role, result.Status, result.Iterations, result.ToolCalls, compactSubAgentErrorSummary(result.Error))
	return result
}
