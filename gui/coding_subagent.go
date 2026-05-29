package main

// coding_subagent.go implements a lightweight coding SubAgent that runs in a
// clean context — only coding-related system prompt, only file/shell tools,
// and an independent conversation history. This eliminates the context
// pollution problem where IM rules, 40+ tool definitions, memory recall,
// and steering rules consume 40K+ tokens before any coding work begins.
//
// The SubAgent reuses corelib/agent.RunLoop for the LLM loop and delegates
// tool execution to the host's existing tool implementations (toolReadFile,
// toolWriteFile, etc.) via the IMMessageHandler reference.
//
// Architecture:
//
//   Main Agent (orchestrator)          Coding SubAgent (executor)
//   ┌──────────────────────┐          ┌──────────────────────┐
//   │ System Prompt  12K   │          │ Coding Prompt   2K   │
//   │ 40+ Tools     15K   │          │ 9 Tools         2K   │
//   │ Memory/Steering 5K  │   task   │ Task Context    3K   │
//   │ History       20K   │ ───────→ │ Coding History  ~95K │
//   │                      │          │                      │
//   │ Duties: workflow,    │ ←─────── │ Duties: read/write   │
//   │ IM, memory, routing  │  result  │ files, compile, test │
//   └──────────────────────┘          └──────────────────────┘

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
)

// CodingSubAgent executes a single coding task in a clean context.
type CodingSubAgent struct {
	handler     *IMMessageHandler // host — provides tool implementations
	cfg         corelib.MaclawLLMConfig
	httpClient  *http.Client
	projectPath string

	// onToken streams text deltas to the UI (optional).
	onToken func(delta string)
	// onProgress reports status updates (optional).
	onProgress func(text string)

	// loopCtx is the parent LoopContext from the main agent loop.
	// Used to propagate cancellation: when the user clicks cancel,
	// CancelCurrentSession calls loopCtx.Cancel(), and the SubAgent's
	// ShouldStop checks loopCtx.IsCancelled().
	loopCtx *LoopContext
}

// CodingSubAgentResult is the outcome of a single task execution.
type CodingSubAgentResult struct {
	Status     TaskExecStatus // passed, failed, skipped
	Summary    string         // human-readable summary of what was done
	Error      string         // error message if failed
	Iterations int
	ToolCalls  int

	// FilesModified lists files that were written/edited during execution.
	// Extracted from tool call history for context preservation.
	FilesModified []string

	// FilesCreated lists files newly created by write_file during execution.
	FilesCreated []string

	// FilesRead lists files successfully read before or during execution.
	FilesRead []string

	// GitDiffChecked indicates whether the SubAgent inspected the final diff.
	// GitDiffSummary contains a compact, user-facing excerpt of that diff.
	GitDiffChecked bool
	GitDiffSummary string

	// CommandsRun records bash commands executed during the coding task.
	CommandsRun []CodingSubAgentCommandResult

	// SearchesRun records Glob/ripgrep exploration calls.
	SearchesRun []CodingSubAgentSearchResult

	// GuardrailViolations records safety/scope rejections encountered during tool execution.
	GuardrailViolations []CodingSubAgentGuardrailViolation

	// ExplorationStatus summarizes whether the agent explored before editing:
	// explored, read_only, missing, or not_needed.
	ExplorationStatus  codingSubAgentQualityStatus
	ExplorationSummary string

	// VerificationStatus summarizes command-based verification:
	// passed, failed, missing, or not_needed.
	VerificationStatus  codingSubAgentQualityStatus
	VerificationSummary string
}

// CodingSubAgentCommandResult is a compact audit record for a bash command.
type CodingSubAgentCommandResult struct {
	Command    string
	WorkingDir string
	Succeeded  bool
	Summary    string
}

// CodingSubAgentSearchResult is a compact audit record for code exploration.
type CodingSubAgentSearchResult struct {
	Tool      string
	Query     string
	Path      string
	Succeeded bool
	Summary   string
}

// CodingSubAgentGuardrailViolation is a compact audit record for blocked tool use.
type CodingSubAgentGuardrailViolation struct {
	Tool     string
	Category CodingSubAgentGuardrailCategory
	Path     string
	Command  string
	Summary  string
}

type codingToolOutcome string

const (
	codingToolOutcomeSuccess codingToolOutcome = "success"
	codingToolOutcomeFailed  codingToolOutcome = "failed"
	codingToolOutcomeBlocked codingToolOutcome = "blocked"
	codingToolOutcomeTimeout codingToolOutcome = "timeout"
)

type codingToolExecutionResult struct {
	Text    string
	Outcome codingToolOutcome
}

// NewCodingSubAgent creates a SubAgent bound to the host's tool implementations.
// loopCtx propagates cancellation from the main agent loop.
func NewCodingSubAgent(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, projectPath string, loopCtx *LoopContext) *CodingSubAgent {
	return &CodingSubAgent{
		handler:     handler,
		cfg:         cfg,
		httpClient:  httpClient,
		projectPath: projectPath,
		loopCtx:     loopCtx,
	}
}

// SetCallbacks configures optional streaming and progress callbacks.
func (s *CodingSubAgent) SetCallbacks(onToken func(string), onProgress func(string)) {
	s.onToken = onToken
	s.onProgress = onProgress
}

// ExecuteTask runs a single task in a clean coding context.
// The conversation is independent — no IM rules, no memory, no 40+ tools.
func (s *CodingSubAgent) ExecuteTask(task *TaskItem, reqCtx, designCtx string, prevOutputs []string) *CodingSubAgentResult {
	if s == nil {
		log.Printf("[coding-subagent] task start failed: subagent is nil")
		return &CodingSubAgentResult{
			Status: TaskExecFailed,
			Error:  "coding subagent is nil",
		}
	}
	if task == nil {
		log.Printf("[coding-subagent] task start failed: task is nil (project=%s)", s.projectPath)
		return &CodingSubAgentResult{
			Status: TaskExecFailed,
			Error:  "coding subagent task is nil",
		}
	}

	taskTitle := compactSubAgentTaskTitle(task.Title)
	log.Printf("[coding-subagent] starting task T%d: %s (project=%s)", task.Index, taskTitle, s.projectPath)

	if s.onProgress != nil {
		emitCodingAgentEvent(s.onProgress, newCodingAgentTaskEvent(codingAgentEventPhaseRunning, task, taskTitle, ""))
	}

	cb := &codingSubAgentCallbacks{
		subagent:    s,
		task:        task,
		reqCtx:      reqCtx,
		designCtx:   designCtx,
		prevOutputs: prevOutputs,
	}

	result := agent.RunLoop(cb, cb.buildTaskUserMessage(), nil, s.httpClient)

	status := TaskExecPassed
	errMsg := ""
	if result.Error != "" {
		status = TaskExecFailed
		errMsg = compactSubAgentErrorSummary(result.Error)
		log.Printf("[coding-subagent] task T%d agent loop failed: %s", task.Index, errMsg)
	}
	if result.HardExit {
		status = TaskExecFailed
		errMsg = "模型连续返回空响应，任务中断"
	}

	summary := compactSubAgentModelSummary(result.Text)
	if summary == "" {
		summary = fmt.Sprintf("任务 T%d 执行完成，%d 轮迭代，%d 次工具调用", task.Index, result.Iterations, result.ToolCalls)
	}

	filesModified := limitSubAgentStringSlice(cb.getFilesModified(), codingSubAgentResultFilesMax)
	filesCreated := limitSubAgentStringSlice(cb.getFilesCreated(), codingSubAgentResultFilesMax)
	filesRead := limitSubAgentStringSlice(cb.getFilesRead(), codingSubAgentResultFilesMax)
	commandsRun := limitSubAgentCommandResults(cb.getCommandsRun(), codingSubAgentResultAuditMax)
	searchesRun := limitSubAgentSearchResults(cb.getSearchesRun(), codingSubAgentResultAuditMax)
	guardrailViolations := limitSubAgentGuardrailViolations(cb.getGuardrailViolations(), codingSubAgentResultAuditMax)
	explorationStatus, explorationSummary := summarizeSubAgentExploration(filesModified, filesRead, searchesRun)
	verificationStatus, verificationSummary := summarizeSubAgentVerification(filesModified, commandsRun)
	status, errMsg = applySubAgentVerificationOutcome(status, errMsg, verificationStatus, verificationSummary)
	summary = appendSubAgentFileChangeSummary(summary, filesModified, filesCreated)
	cb.emitFileActivitySummaryEvent(filesRead, filesModified, filesCreated)
	if len(guardrailViolations) > 0 {
		summary = appendSubAgentGuardrailSummary(summary, guardrailViolations)
	}
	cb.emitGuardrailSummaryEvent(guardrailViolations)
	if explorationSummary != "" {
		summary = appendSubAgentExplorationSummary(summary, explorationStatus, explorationSummary)
	}
	cb.emitExplorationSummaryEvent(explorationStatus, explorationSummary, countSuccessfulSubAgentSearches(searchesRun))
	if len(commandsRun) > 0 {
		summary = appendSubAgentCommandSummary(summary, commandsRun)
	}
	cb.emitCommandSummaryEvent(commandsRun)
	if verificationSummary != "" {
		summary = appendSubAgentVerificationSummary(summary, verificationStatus, verificationSummary)
	}
	cb.emitVerificationSummaryEvent(verificationStatus, verificationSummary, len(filterSubAgentVerificationCommands(commandsRun)))
	diffChecked, diffSummary := cb.ensureFinalGitDiff(filesModified)
	if diffSummary != "" {
		summary = appendSubAgentDiffSummary(summary, diffSummary)
	}
	cb.emitDiffCheckEvent(diffChecked, diffSummary, len(filesModified))
	cb.emitQualitySummaryEvent(explorationStatus, verificationStatus, diffChecked, filesModified, commandsRun, guardrailViolations)
	cb.emitDiffSummaryEvent(filesModified, filesCreated, diffSummary)

	log.Printf("[coding-subagent] task T%d finished: status=%s iterations=%d tools=%d err=%q",
		task.Index, status, result.Iterations, result.ToolCalls, errMsg)
	if s.onProgress != nil {
		event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, task, taskTitle, "")
		event.Detail = string(status)
		emitCodingAgentEvent(s.onProgress, event)
	}

	return &CodingSubAgentResult{
		Status:              status,
		Summary:             summary,
		Error:               errMsg,
		Iterations:          result.Iterations,
		ToolCalls:           result.ToolCalls,
		FilesModified:       filesModified,
		FilesCreated:        filesCreated,
		FilesRead:           filesRead,
		GitDiffChecked:      diffChecked,
		GitDiffSummary:      diffSummary,
		CommandsRun:         commandsRun,
		SearchesRun:         searchesRun,
		GuardrailViolations: guardrailViolations,
		ExplorationStatus:   explorationStatus,
		ExplorationSummary:  explorationSummary,
		VerificationStatus:  verificationStatus,
		VerificationSummary: verificationSummary,
	}
}

// ---------------------------------------------------------------------------
// codingSubAgentCallbacks implements agent.LoopCallbacks with a minimal
// coding-only configuration.
// ---------------------------------------------------------------------------

type codingSubAgentCallbacks struct {
	subagent    *CodingSubAgent
	task        *TaskItem
	reqCtx      string
	designCtx   string
	prevOutputs []string

	// cachedTools is built once on first call to BuildTools.
	cachedTools []map[string]interface{}

	// filesModified tracks files written/edited during execution.
	mu             sync.Mutex
	filesModified  map[string]bool
	filesCreated   map[string]bool
	filesRead      map[string]bool
	fileSnapshots  map[string]codingFileSnapshot
	gitDiffChecked bool
	lastGitDiff    string
	commandsRun    []CodingSubAgentCommandResult
	searchesRun    []CodingSubAgentSearchResult
	guardrails     []CodingSubAgentGuardrailViolation
}

type codingFileSnapshot struct {
	Size int64
	Hash string
}

func (c *codingSubAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.subagent.cfg
}

func (c *codingSubAgentCallbacks) GetMaxIterations() int {
	// Single task: 50 iterations is generous. This keeps context bounded.
	return config.EffectiveMaxIterations(50)
}

func (c *codingSubAgentCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	return buildCodingSubAgentSystemPrompt(c.task, c.subagent.projectPath, c.reqCtx, c.designCtx, c.prevOutputs)
}

func (c *codingSubAgentCallbacks) BuildTools(userText string) []map[string]interface{} {
	if c.cachedTools == nil {
		c.cachedTools = buildCodingToolDefinitionsFromRegistry(c.subagent.handler)
	}
	return c.cachedTools
}

func (c *codingSubAgentCallbacks) ExecuteTool(name, argsJSON string) string {
	return c.ExecuteToolStructured(name, argsJSON).Result
}

func (c *codingSubAgentCallbacks) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
	result := c.executeToolWithOutcome(name, argsJSON)
	outcome := agent.ToolExecutionOutcomeOK
	switch result.Outcome {
	case codingToolOutcomeTimeout:
		outcome = agent.ToolExecutionOutcomeTimeout
	case codingToolOutcomeSuccess:
		outcome = agent.ToolExecutionOutcomeOK
	default:
		outcome = agent.ToolExecutionOutcomeError
	}
	return agent.ToolExecutionResult{Result: result.Text, Outcome: outcome}
}

func (c *codingSubAgentCallbacks) executeToolWithOutcome(name, argsJSON string) (toolResult codingToolExecutionResult) {
	if c.ShouldStop() {
		return codingToolExecutionResult{Text: "coding subagent cancelled before tool execution", Outcome: codingToolOutcomeFailed}
	}
	toolStartedAt := time.Now()
	c.emitToolStartedEvent(name)
	defer func() {
		c.emitToolFinishedEvent(name, toolResult.Text, toolResult.Outcome, time.Since(toolStartedAt))
	}()

	if !codingSubAgentToolNames[name] {
		return codingToolExecutionResult{Text: fmt.Sprintf("unknown tool: %s (coding SubAgent supports %v)", name, codingSubAgentToolNameList()), Outcome: codingToolOutcomeFailed}
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return codingToolExecutionResult{Text: fmt.Sprintf("argument parse failed: %v", err), Outcome: codingToolOutcomeFailed}
	}

	if c == nil || c.subagent == nil {
		return codingToolExecutionResult{Text: "coding subagent is unavailable", Outcome: codingToolOutcomeFailed}
	}

	h := c.subagent.handler
	switch name {
	case "Glob":
		searchArgs := c.withDefaultProjectPath(args)
		if p, _ := searchArgs["path"].(string); p != "" {
			if msg := c.requireProjectReadScope(p, "Glob"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("Glob", searchArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		result := agent.ToolGlobDetailed(searchArgs)
		c.trackSearchResult("Glob", searchArgs, result.Text, result.Outcome == agent.SearchToolOutcomeMatched)
		return codingToolExecutionResult{Text: result.Text, Outcome: codingOutcomeFromSearchOutcome(result.Outcome)}
	case "ripgrep":
		searchArgs := c.withDefaultProjectPath(args)
		if p, _ := searchArgs["path"].(string); p != "" {
			if msg := c.requireProjectReadScope(p, "ripgrep"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("ripgrep", searchArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		result := agent.ToolRipgrepDetailed(searchArgs)
		c.trackSearchResult("ripgrep", searchArgs, result.Text, result.Outcome == agent.SearchToolOutcomeMatched)
		return codingToolExecutionResult{Text: result.Text, Outcome: codingOutcomeFromSearchOutcome(result.Outcome)}
	case "read_file":
		if h == nil {
			return codingToolExecutionResult{Text: c.rejectToolCall("read_file", args, "coding subagent host tool handler is unavailable"), Outcome: codingToolOutcomeBlocked}
		}
		fileArgs := c.withProjectRelativePath(args, false)
		if p, _ := fileArgs["path"].(string); p != "" {
			if msg := c.requireProjectReadScope(p, "read_file"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("read_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		result := executeCodingReadFile(fileArgs)
		if p, _ := fileArgs["path"].(string); p != "" && result.Outcome == codingToolOutcomeSuccess {
			c.trackReadFile(p)
		}
		return result
	case "write_file":
		if h == nil {
			return codingToolExecutionResult{Text: c.rejectToolCall("write_file", args, "coding subagent host tool handler is unavailable"), Outcome: codingToolOutcomeBlocked}
		}
		fileArgs := c.withProjectRelativePath(args, false)
		if p, _ := fileArgs["path"].(string); p != "" {
			if msg := c.requireProjectWriteScope(p); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("write_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			created := !codingFileExists(p)
			if msg := c.requireReadBeforeWriteExisting(p); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("write_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			result := executeCodingWriteFile(fileArgs)
			if result.Outcome == codingToolOutcomeSuccess {
				c.trackFile(p)
				if created {
					c.trackCreatedFile(p)
				}
				c.refreshFileSnapshot(p)
			}
			return result
		}
		return executeCodingWriteFile(fileArgs)
	case "edit_file":
		if h == nil {
			return codingToolExecutionResult{Text: c.rejectToolCall("edit_file", args, "coding subagent host tool handler is unavailable"), Outcome: codingToolOutcomeBlocked}
		}
		fileArgs := c.withProjectRelativePath(args, false)
		if p, _ := fileArgs["path"].(string); p != "" {
			if msg := c.requireProjectWriteScope(p); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("edit_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			if msg := c.requireReadBeforeModify(p, "edit_file"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("edit_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			result := executeCodingEditFile(fileArgs)
			if result.Outcome == codingToolOutcomeSuccess {
				c.trackFile(p)
				c.refreshFileSnapshot(p)
			}
			return result
		}
		return executeCodingEditFile(fileArgs)
	case "edit_lines":
		if h == nil {
			return codingToolExecutionResult{Text: c.rejectToolCall("edit_lines", args, "coding subagent host tool handler is unavailable"), Outcome: codingToolOutcomeBlocked}
		}
		fileArgs := c.withProjectRelativePath(args, false)
		if p, _ := fileArgs["path"].(string); p != "" {
			if msg := c.requireProjectWriteScope(p); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("edit_lines", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			if msg := c.requireReadBeforeModify(p, "edit_lines"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("edit_lines", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			result := executeCodingEditLines(fileArgs)
			if result.Outcome == codingToolOutcomeSuccess {
				c.trackFile(p)
				c.refreshFileSnapshot(p)
			}
			return result
		}
		return executeCodingEditLines(fileArgs)
	case "bash":
		bashArgs := c.withDefaultWorkingDir(args)
		if command, _ := bashArgs["command"].(string); command != "" {
			if msg := rejectDisallowedCodingBashCommand(command); msg != "" {
				c.trackCommandResult(bashArgs, msg, false)
				return codingToolExecutionResult{Text: c.rejectToolCall("bash", bashArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		if wd, _ := bashArgs["working_dir"].(string); wd != "" {
			if msg := c.requireProjectWorkingDirScope(wd); msg != "" {
				c.trackCommandResult(bashArgs, msg, false)
				return codingToolExecutionResult{Text: c.rejectToolCall("bash", bashArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		// Avoid raw bash heartbeat rows in chat; tool_started/tool_finished events
		// already keep the AI assistant panel updated while the command runs.
		commandResult := executeCodingBash(bashArgs, nil)
		c.trackCommandResult(bashArgs, commandResult.Text, commandResult.Kind == codingCommandResultOK)
		return commandResult.toolResult()
	case "list_directory":
		if h == nil {
			return codingToolExecutionResult{Text: c.rejectToolCall("list_directory", args, "coding subagent host tool handler is unavailable"), Outcome: codingToolOutcomeBlocked}
		}
		listArgs := c.withProjectRelativePath(args, true)
		if p, _ := listArgs["path"].(string); p != "" {
			if msg := c.requireProjectReadScope(p, "list_directory"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("list_directory", listArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		return codingToolExecutionResult{Text: h.toolListDirectory(listArgs), Outcome: codingToolOutcomeSuccess}
	case "git_diff":
		diffArgs := c.withProjectRelativePath(args, true)
		if p, _ := diffArgs["path"].(string); p != "" {
			if msg := c.requireProjectDiffScope(p); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("git_diff", diffArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		result := c.toolGitDiff(diffArgs)
		c.trackGitDiff(result)
		return codingToolExecutionResult{Text: result, Outcome: codingToolOutcomeSuccess}
	default:
		return codingToolExecutionResult{Text: fmt.Sprintf("unknown tool: %s", name), Outcome: codingToolOutcomeFailed}
	}
}

func codingOutcomeFromSuccess(success bool) codingToolOutcome {
	if success {
		return codingToolOutcomeSuccess
	}
	return codingToolOutcomeFailed
}

func codingOutcomeFromSearchOutcome(outcome agent.SearchToolOutcome) codingToolOutcome {
	if outcome == agent.SearchToolOutcomeMatched {
		return codingToolOutcomeSuccess
	}
	return codingToolOutcomeFailed
}

func (c *codingSubAgentCallbacks) withDefaultProjectPath(args map[string]interface{}) map[string]interface{} {
	copied := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		copied[k] = v
	}
	projectPath := c.projectPath()
	if projectPath != "" {
		if raw, ok := copied["path"].(string); !ok || strings.TrimSpace(raw) == "" {
			copied["path"] = projectPath
		} else if raw = strings.TrimSpace(raw); !filepath.IsAbs(raw) {
			copied["path"] = filepath.Join(projectPath, raw)
		} else {
			copied["path"] = raw
		}
	}
	return copied
}

func (c *codingSubAgentCallbacks) withProjectRelativePath(args map[string]interface{}, defaultWhenEmpty bool) map[string]interface{} {
	copied := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		copied[k] = v
	}
	raw, _ := copied["path"].(string)
	raw = strings.TrimSpace(raw)
	projectPath := c.projectPath()
	if raw == "" {
		if defaultWhenEmpty && projectPath != "" {
			copied["path"] = projectPath
		}
		return copied
	}
	if filepath.IsAbs(raw) || projectPath == "" {
		return copied
	}
	copied["path"] = filepath.Join(projectPath, raw)
	return copied
}

func (c *codingSubAgentCallbacks) withDefaultWorkingDir(args map[string]interface{}) map[string]interface{} {
	copied := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		copied[k] = v
	}
	copied["timeout"] = normalizeCodingBashTimeout(copied["timeout"])
	wd, _ := copied["working_dir"].(string)
	wd = strings.TrimSpace(wd)
	projectPath := c.projectPath()
	if wd == "" && projectPath != "" {
		copied["working_dir"] = projectPath
		return copied
	}
	if wd != "" && !filepath.IsAbs(wd) && projectPath != "" {
		copied["working_dir"] = filepath.Join(projectPath, wd)
	}
	return copied
}

func normalizeCodingBashTimeout(raw interface{}) float64 {
	timeout := corelib.DefaultAgentTimeoutSec
	switch v := raw.(type) {
	case float64:
		timeout = int(v)
	case float32:
		timeout = int(v)
	case int:
		timeout = v
	case int64:
		timeout = int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			timeout = int(i)
		}
	}
	return float64(corelib.NormalizeAgentTimeoutSec(timeout))
}

func rejectDisallowedCodingBashCommand(command string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return ""
	}
	disallowed := false
	switch {
	case hasDisallowedGitCommand(normalized):
		disallowed = true
	case hasDisallowedShellFileMutation(normalized):
		disallowed = true
	case hasDisallowedRecursiveDeleteCommand(normalized):
		disallowed = true
	}
	if !disallowed {
		return ""
	}
	return fmt.Sprintf("拒绝执行高风险命令：%s。编码 SubAgent 不允许自动执行破坏性删除、清理、Git 工作区改写或 shell 文件写入命令；请改用更小范围的文件编辑工具。", command)
}

func hasDisallowedRecursiveDeleteCommand(normalizedCommand string) bool {
	fields := shellCommandFields(normalizedCommand)
	commandPosition := true
	for i, field := range fields {
		token := normalizeShellCommandToken(field)
		if token == "" {
			continue
		}
		if isShellCommandStartMarker(token) {
			commandPosition = true
			continue
		}
		if commandPosition {
			if consumed, ok := shellCommandPrefixLength(fields[i:]); ok {
				i += consumed - 1
				commandPosition = true
				continue
			}
			switch normalizeShellExecutableToken(token) {
			case "rm", "remove-item", "ri", "del", "erase", "rd", "rmdir":
				if hasRecursiveDeleteFlag(strings.Join(commandSegmentFields(fields[i+1:]), " ")) {
					return true
				}
			}
		}
		commandPosition = false
	}
	return false
}

func hasDisallowedShellFileMutation(normalizedCommand string) bool {
	redirectionCheck := strings.ReplaceAll(normalizedCommand, "2>&1", "")
	if hasShellCommandToken(normalizedCommand, map[string]bool{
		"set-content": true, "add-content": true, "out-file": true, "new-item": true,
		"copy-item": true, "move-item": true, "rename-item": true, "tee-object": true,
		"export-csv": true, "start-transcript": true,
		"sc": true, "ac": true, "ni": true, "cp": true, "copy": true, "mv": true,
		"move": true, "ren": true, "rename": true, "touch": true, "mkdir": true,
		"md": true, "truncate": true, "xcopy": true, "robocopy": true,
	}) {
		return true
	}
	if strings.Contains(normalizedCommand, "sed -i") || strings.Contains(normalizedCommand, "perl -pi") {
		return true
	}
	if strings.Contains(normalizedCommand, "writefilesync") || strings.Contains(normalizedCommand, "promises.writefile") || strings.Contains(normalizedCommand, ".writefile(") || strings.Contains(normalizedCommand, ".appendfile(") {
		return true
	}
	if strings.Contains(normalizedCommand, ".write_text(") || strings.Contains(normalizedCommand, ".write_bytes(") {
		return true
	}
	if strings.Contains(normalizedCommand, "open(") && (strings.Contains(normalizedCommand, "'w'") || strings.Contains(normalizedCommand, "\"w\"") || strings.Contains(normalizedCommand, "'a'") || strings.Contains(normalizedCommand, "\"a\"")) {
		return true
	}
	if strings.Contains(normalizedCommand, " of=") || strings.Contains(normalizedCommand, " of ") {
		return true
	}
	if strings.Contains(redirectionCheck, " >") || strings.Contains(redirectionCheck, "> ") || strings.Contains(redirectionCheck, ">>") {
		return true
	}
	if strings.Contains(normalizedCommand, "| tee") || strings.Contains(normalizedCommand, "|tee") || strings.Contains(normalizedCommand, "| tee-object") || strings.Contains(normalizedCommand, "|tee-object") {
		return true
	}
	return false
}

func hasShellCommandToken(normalizedCommand string, disallowed map[string]bool) bool {
	fields := shellCommandFields(normalizedCommand)
	commandPosition := true
	for i := 0; i < len(fields); i++ {
		token := normalizeShellCommandToken(fields[i])
		if token == "" {
			continue
		}
		if isShellCommandStartMarker(token) {
			commandPosition = true
			continue
		}
		if commandPosition {
			if consumed, ok := shellCommandPrefixLength(fields[i:]); ok {
				i += consumed - 1
				commandPosition = true
				continue
			}
			if disallowed[normalizeShellExecutableToken(token)] {
				return true
			}
		}
		commandPosition = false
	}
	return false
}

func normalizeShellCommandToken(field string) string {
	return strings.Trim(field, " \t\r\n'\"`(){}[]")
}

func normalizeShellExecutableToken(token string) string {
	for _, suffix := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		if strings.HasSuffix(token, suffix) {
			return strings.TrimSuffix(token, suffix)
		}
	}
	return token
}

func isShellCommandBoundary(token string) bool {
	switch token {
	case "|", ";", "&&", "||", "&", "(", ")":
		return true
	}
	return false
}

func isShellCommandStartMarker(token string) bool {
	return isShellCommandBoundary(token) || token == "-command" || token == "/command" || token == "-c" || token == "/c"
}

func commandSegmentFields(fields []string) []string {
	segment := make([]string, 0, len(fields))
	for _, field := range fields {
		token := normalizeShellCommandToken(field)
		if isShellCommandBoundary(token) {
			break
		}
		segment = append(segment, token)
	}
	return segment
}

func shellCommandFields(command string) []string {
	spaced := strings.NewReplacer(
		"&&", " && ",
		"||", " || ",
		";", " ; ",
		"|", " | ",
		"(", " ( ",
		")", " ) ",
	).Replace(command)
	return strings.Fields(spaced)
}

func hasDisallowedGitCommand(normalizedCommand string) bool {
	fields := shellCommandFields(normalizedCommand)
	commandPosition := true
	for i := 0; i < len(fields); i++ {
		token := normalizeShellExecutableToken(normalizeShellCommandToken(fields[i]))
		if token == "" {
			continue
		}
		if isShellCommandStartMarker(token) {
			commandPosition = true
			continue
		}
		if commandPosition {
			if consumed, ok := shellCommandPrefixLength(fields[i:]); ok {
				i += consumed - 1
				commandPosition = true
				continue
			}
		}
		if token != "git" {
			commandPosition = false
			continue
		}
		if !commandPosition {
			commandPosition = false
			continue
		}
		for j := i + 1; j < len(fields); j++ {
			arg := normalizeShellCommandToken(fields[j])
			if isShellCommandBoundary(arg) {
				break
			}
			if arg == "-c" || arg == "--git-dir" || arg == "--work-tree" || arg == "--namespace" {
				j++
				continue
			}
			if strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--git-dir=") || strings.HasPrefix(arg, "--work-tree=") || strings.HasPrefix(arg, "--namespace=") {
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
			switch arg {
			case "reset", "checkout", "restore", "switch", "merge", "rebase", "stash":
				return true
			case "clean":
				return hasGitCleanForceFlag(commandSegmentFields(fields[j+1:]))
			}
			break
		}
		commandPosition = false
	}
	return false
}

func hasGitCleanForceFlag(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-f", "--force":
			return true
		case "-n", "--dry-run":
			continue
		}
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "f") {
			return true
		}
	}
	return false
}

func shellCommandPrefixLength(fields []string) (int, bool) {
	if len(fields) == 0 {
		return 0, false
	}
	token := normalizeShellCommandToken(fields[0])
	cmd := normalizeShellExecutableToken(token)
	switch {
	case isShellEnvAssignment(cmd):
		return 1, true
	case cmd == "cross-env" || cmd == "cross-env-shell" || cmd == "time":
		return 1, true
	case cmd == "env":
		consumed := 1
		for consumed < len(fields) {
			next := normalizeShellCommandToken(fields[consumed])
			if isShellEnvAssignment(next) {
				consumed++
				continue
			}
			if envOptionConsumesValue(next) {
				consumed++
				if consumed < len(fields) {
					consumed++
				}
				continue
			}
			if strings.HasPrefix(next, "-") {
				consumed++
				continue
			}
			break
		}
		return consumed, true
	}
	return 0, false
}

func envOptionConsumesValue(option string) bool {
	switch option {
	case "-u", "--unset", "-C", "--chdir", "-S", "--split-string":
		return true
	}
	return false
}

func hasRecursiveDeleteFlag(normalizedCommand string) bool {
	fields := strings.Fields(normalizedCommand)
	for _, field := range fields {
		switch field {
		case "-r", "-recurse", "-recursive", "/s":
			return true
		}
		if strings.HasPrefix(field, "-") && strings.Contains(field, "r") && strings.Contains(field, "f") {
			return true
		}
	}
	return false
}

func (c *codingSubAgentCallbacks) toolGitDiff(args map[string]interface{}) string {
	workDir, _ := args["path"].(string)
	if workDir == "" {
		workDir = c.projectPath()
	}
	if workDir == "" {
		workDir = "."
	}

	command := "git diff -- ."
	if staged, _ := args["staged"].(bool); staged {
		command = "git diff --staged -- ."
	}
	return agent.ToolBash(map[string]interface{}{
		"command":     command,
		"working_dir": workDir,
		"timeout":     float64(30),
	}, nil)
}

func (c *codingSubAgentCallbacks) projectPath() string {
	if c == nil || c.subagent == nil {
		return ""
	}
	return strings.TrimSpace(c.subagent.projectPath)
}

func codingSubAgentToolNameList() []string {
	names := make([]string, 0, len(codingSubAgentToolNames))
	for n := range codingSubAgentToolNames {
		names = append(names, n)
	}
	return names
}

func (c *codingSubAgentCallbacks) trackFile(path string) {
	displayPath := c.displayProjectPath(path)
	c.mu.Lock()
	if c.filesModified == nil {
		c.filesModified = make(map[string]bool)
	}
	c.filesModified[displayPath] = true
	count := len(c.filesModified)
	c.mu.Unlock()
	c.emitDiffUpdatedEvent(displayPath, count)
}

func (c *codingSubAgentCallbacks) trackCreatedFile(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.filesCreated == nil {
		c.filesCreated = make(map[string]bool)
	}
	c.filesCreated[c.displayProjectPath(path)] = true
}

func (c *codingSubAgentCallbacks) trackReadFile(path string) {
	snap, err := snapshotCodingFile(path)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.filesRead == nil {
		c.filesRead = make(map[string]bool)
	}
	if c.fileSnapshots == nil {
		c.fileSnapshots = make(map[string]codingFileSnapshot)
	}
	key := canonicalCodingPath(path)
	c.filesRead[key] = true
	c.fileSnapshots[key] = snap
}

func (c *codingSubAgentCallbacks) getFilesModified() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	files := make([]string, 0, len(c.filesModified))
	for f := range c.filesModified {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

func (c *codingSubAgentCallbacks) getFilesCreated() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	files := make([]string, 0, len(c.filesCreated))
	for f := range c.filesCreated {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

func (c *codingSubAgentCallbacks) getFilesRead() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	files := make([]string, 0, len(c.filesRead))
	for f := range c.filesRead {
		files = append(files, c.displayProjectPath(f))
	}
	sort.Strings(files)
	return files
}

func (c *codingSubAgentCallbacks) displayProjectPath(path string) string {
	if c.subagent == nil || strings.TrimSpace(c.subagent.projectPath) == "" {
		return filepath.Clean(path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	absProject, err := filepath.Abs(c.subagent.projectPath)
	if err != nil {
		return filepath.Clean(path)
	}
	if rel, err := filepath.Rel(absProject, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return filepath.ToSlash(rel)
	}
	return filepath.Clean(path)
}

func (c *codingSubAgentCallbacks) trackGitDiff(result string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gitDiffChecked = true
	c.lastGitDiff = compactSubAgentDiff(result)
}

func (c *codingSubAgentCallbacks) trackCommandResult(args map[string]interface{}, result string, succeeded bool) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return
	}
	workDir, _ := args["working_dir"].(string)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commandsRun = append(c.commandsRun, CodingSubAgentCommandResult{
		Command:    command,
		WorkingDir: workDir,
		Succeeded:  succeeded,
		Summary:    compactCommandResult(result),
	})
}

func (c *codingSubAgentCallbacks) rejectToolCall(toolName string, args map[string]interface{}, result string) string {
	c.trackGuardrailViolation(toolName, args, result)
	return result
}

func (c *codingSubAgentCallbacks) trackGuardrailViolation(toolName string, args map[string]interface{}, result string) {
	path, _ := args["path"].(string)
	if path == "" {
		path, _ = args["working_dir"].(string)
	}
	command, _ := args["command"].(string)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.guardrails = append(c.guardrails, CodingSubAgentGuardrailViolation{
		Tool:     toolName,
		Category: classifyCodingGuardrailCategory(toolName, path, command, result),
		Path:     c.displayProjectPath(path),
		Command:  command,
		Summary:  compactGuardrailSummary(result),
	})
}

func (c *codingSubAgentCallbacks) getGuardrailViolations() []CodingSubAgentGuardrailViolation {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.guardrails) == 0 {
		return nil
	}
	out := make([]CodingSubAgentGuardrailViolation, len(c.guardrails))
	copy(out, c.guardrails)
	return out
}

func (c *codingSubAgentCallbacks) getCommandsRun() []CodingSubAgentCommandResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.commandsRun) == 0 {
		return nil
	}
	out := make([]CodingSubAgentCommandResult, len(c.commandsRun))
	copy(out, c.commandsRun)
	return out
}

func (c *codingSubAgentCallbacks) trackSearchResult(toolName string, args map[string]interface{}, result string, succeeded bool) {
	query, _ := args["pattern"].(string)
	path, _ := args["path"].(string)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.searchesRun = append(c.searchesRun, CodingSubAgentSearchResult{
		Tool:      toolName,
		Query:     compactSubAgentSearchText(query),
		Path:      compactSubAgentPathText(c.displayProjectPath(path)),
		Succeeded: succeeded,
		Summary:   compactSearchResult(result),
	})
}

func (c *codingSubAgentCallbacks) getSearchesRun() []CodingSubAgentSearchResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.searchesRun) == 0 {
		return nil
	}
	out := make([]CodingSubAgentSearchResult, len(c.searchesRun))
	copy(out, c.searchesRun)
	return out
}

func limitSubAgentStringSlice(values []string, maxItems int) []string {
	if maxItems <= 0 || len(values) <= maxItems {
		return values
	}
	out := make([]string, maxItems)
	copy(out, values[:maxItems])
	return out
}

func limitSubAgentCommandResults(values []CodingSubAgentCommandResult, maxItems int) []CodingSubAgentCommandResult {
	if maxItems <= 0 || len(values) <= maxItems {
		return values
	}
	out := make([]CodingSubAgentCommandResult, maxItems)
	copy(out, values[:maxItems])
	return out
}

func limitSubAgentSearchResults(values []CodingSubAgentSearchResult, maxItems int) []CodingSubAgentSearchResult {
	if maxItems <= 0 || len(values) <= maxItems {
		return values
	}
	out := make([]CodingSubAgentSearchResult, maxItems)
	copy(out, values[:maxItems])
	return out
}

func limitSubAgentGuardrailViolations(values []CodingSubAgentGuardrailViolation, maxItems int) []CodingSubAgentGuardrailViolation {
	if maxItems <= 0 || len(values) <= maxItems {
		return values
	}
	out := make([]CodingSubAgentGuardrailViolation, maxItems)
	copy(out, values[:maxItems])
	return out
}

func (c *codingSubAgentCallbacks) ensureFinalGitDiff(filesModified []string) (bool, string) {
	c.mu.Lock()
	alreadyChecked := c.gitDiffChecked
	lastDiff := c.lastGitDiff
	c.mu.Unlock()

	if alreadyChecked {
		return true, lastDiff
	}
	if len(filesModified) == 0 {
		return false, ""
	}

	result := c.toolGitDiff(map[string]interface{}{})
	c.trackGitDiff(result)
	return true, compactSubAgentDiff(result)
}

func (c *codingSubAgentCallbacks) requireProjectWriteScope(path string) string {
	projectPath := c.projectPath()
	if projectPath == "" {
		return ""
	}
	ok, err := isPathWithinDir(path, projectPath)
	if err != nil {
		return fmt.Sprintf("无法确认写入路径是否位于项目目录内：%s", err.Error())
	}
	if ok {
		return ""
	}
	return fmt.Sprintf("拒绝修改项目目录外的文件：%s。编码 SubAgent 只能修改项目路径 %s 内的文件。", path, projectPath)
}

func (c *codingSubAgentCallbacks) requireProjectReadScope(path, toolName string) string {
	projectPath := c.projectPath()
	if projectPath == "" {
		return ""
	}
	ok, err := isPathWithinDir(path, projectPath)
	if err != nil {
		return fmt.Sprintf("无法确认 %s 读取路径是否位于项目目录内：%s", toolName, err.Error())
	}
	if ok {
		return ""
	}
	return fmt.Sprintf("拒绝读取项目目录外的路径：%s。编码 SubAgent 的 %s 只能访问项目路径 %s 内的内容。", path, toolName, projectPath)
}

func (c *codingSubAgentCallbacks) requireProjectWorkingDirScope(path string) string {
	projectPath := c.projectPath()
	if projectPath == "" {
		return ""
	}
	ok, err := isPathWithinDir(path, projectPath)
	if err != nil {
		return fmt.Sprintf("无法确认命令工作目录是否位于项目目录内：%s", err.Error())
	}
	if ok {
		return ""
	}
	return fmt.Sprintf("拒绝在项目目录外执行命令：%s。编码 SubAgent 只能在项目路径 %s 内运行命令。", path, projectPath)
}

func (c *codingSubAgentCallbacks) requireProjectDiffScope(path string) string {
	projectPath := c.projectPath()
	if projectPath == "" {
		return ""
	}
	ok, err := isPathWithinDir(path, projectPath)
	if err != nil {
		return fmt.Sprintf("无法确认 git_diff 路径是否位于项目目录内：%s", err.Error())
	}
	if ok {
		return ""
	}
	return fmt.Sprintf("拒绝查看项目目录外的 diff：%s。编码 SubAgent 只能检查项目路径 %s 内的 diff。", path, projectPath)
}

func (c *codingSubAgentCallbacks) requireReadBeforeModify(path, toolName string) string {
	if ok, msg := c.validateReadSnapshot(path); !ok {
		if msg != "" {
			return msg
		}
		return fmt.Sprintf("请先调用 read_file(path=%q) 查看当前内容，再使用 %s 修改。编码 SubAgent 不允许未读文件就修改已有内容。", path, toolName)
	}
	return ""
}

func (c *codingSubAgentCallbacks) requireReadBeforeWriteExisting(path string) string {
	abs, err := resolveFileToolPath(path)
	if err != nil {
		return err.Error()
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return fmt.Sprintf("无法检查目标文件状态: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，不能用 write_file 写入", abs)
	}
	if ok, msg := c.validateReadSnapshot(abs); !ok {
		if msg != "" {
			return msg
		}
		return fmt.Sprintf("目标文件已存在，请先调用 read_file(path=%q) 查看当前内容；只有创建全新文件时才能直接 write_file。", path)
	}
	return ""
}

func (c *codingSubAgentCallbacks) validateReadSnapshot(path string) (bool, string) {
	key := canonicalCodingPath(path)
	c.mu.Lock()
	read := c.filesRead != nil && c.filesRead[key]
	snap, hasSnap := c.fileSnapshots[key]
	c.mu.Unlock()
	if !read {
		return false, ""
	}
	if !hasSnap {
		return false, fmt.Sprintf("文件 %s 缺少 read_file 快照，请重新调用 read_file 后再修改。", path)
	}
	current, err := snapshotCodingFile(path)
	if err != nil {
		return false, fmt.Sprintf("无法确认文件 %s 是否仍与 read_file 时一致：%s。请重新 read_file 后再修改。", path, err.Error())
	}
	if current != snap {
		return false, fmt.Sprintf("文件 %s 自上次 read_file 后已变化，请重新调用 read_file 获取最新内容后再修改。", path)
	}
	return true, ""
}

func (c *codingSubAgentCallbacks) refreshFileSnapshot(path string) {
	snap, err := snapshotCodingFile(path)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.filesRead == nil {
		c.filesRead = make(map[string]bool)
	}
	if c.fileSnapshots == nil {
		c.fileSnapshots = make(map[string]codingFileSnapshot)
	}
	key := canonicalCodingPath(path)
	c.filesRead[key] = true
	c.fileSnapshots[key] = snap
}

func canonicalCodingPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := resolveFileToolPath(path); err == nil {
		path = abs
	}
	if clean, err := filepath.Abs(path); err == nil {
		path = clean
	}
	if realPath, err := evalCodingScopePath(path); err == nil {
		path = realPath
	}
	return filepath.Clean(path)
}

func isPathWithinDir(path, dir string) (bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false, err
	}
	absPath = filepath.Clean(absPath)
	absDir = filepath.Clean(absDir)
	if ok, err := pathWithinCleanDir(absPath, absDir); err != nil || !ok {
		return ok, err
	}

	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return false, err
	}
	realPath, err := evalCodingScopePath(absPath)
	if err != nil {
		return false, err
	}
	return pathWithinCleanDir(realPath, realDir)
}

func pathWithinCleanDir(path, dir string) (bool, error) {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

func evalCodingScopePath(path string) (string, error) {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	var missing []string
	current := clean
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing parent for %s", path)
		}
		missing = append(missing, filepath.Base(current))
		if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
			resolved := resolvedParent
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		current = parent
	}
}

func snapshotCodingFile(path string) (codingFileSnapshot, error) {
	abs, err := resolveFileToolPath(path)
	if err != nil {
		return codingFileSnapshot{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return codingFileSnapshot{}, err
	}
	if info.IsDir() {
		return codingFileSnapshot{}, fmt.Errorf("%s is a directory", abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return codingFileSnapshot{}, err
	}
	sum := sha256.Sum256(data)
	return codingFileSnapshot{
		Size: info.Size(),
		Hash: fmt.Sprintf("%x", sum),
	}, nil
}

func codingFileExists(path string) bool {
	abs, err := resolveFileToolPath(path)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && !info.IsDir()
}

func compactSubAgentDiff(diff string) string {
	diff = strings.TrimSpace(diff)
	if diff == "" || diff == "(命令执行完成，无输出)" {
		return "git diff 无输出"
	}
	return truncateRunesForSubAgent(diff, 2000)
}

func appendSubAgentDiffSummary(summary, diffSummary string) string {
	if strings.TrimSpace(diffSummary) == "" {
		return summary
	}
	return strings.TrimSpace(summary) + "\n\n## Diff 自检\n\n" + diffSummary
}

func compactSubAgentModelSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	return truncateRunesForSubAgent(summary, codingSubAgentModelSummaryMaxRunes)
}

func compactSubAgentErrorSummary(errMsg string) string {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return ""
	}
	return truncateRunesForSubAgent(errMsg, codingSubAgentErrorSummaryMaxRunes)
}

func appendSubAgentFileChangeSummary(summary string, filesModified, filesCreated []string) string {
	filesModified = uniqueSortedSubAgentStrings(filesModified)
	filesCreated = uniqueSortedSubAgentStrings(filesCreated)
	if len(filesModified) == 0 && len(filesCreated) == 0 {
		return summary
	}
	created := make(map[string]bool, len(filesCreated))
	for _, f := range filesCreated {
		created[f] = true
	}
	var changedExisting []string
	for _, f := range filesModified {
		if !created[f] {
			changedExisting = append(changedExisting, f)
		}
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n\n## 文件变更\n\n")
	shownCreated := len(filesCreated)
	if shownCreated > codingSubAgentFileChangeSummaryMax {
		shownCreated = codingSubAgentFileChangeSummaryMax
	}
	for _, f := range filesCreated[:shownCreated] {
		b.WriteString("- created: `")
		b.WriteString(strings.ReplaceAll(f, "`", "'"))
		b.WriteString("`\n")
	}
	if remaining := len(filesCreated) - shownCreated; remaining > 0 {
		b.WriteString(fmt.Sprintf("- ... 还有 %d 个新建文件未展开\n", remaining))
	}
	shownModified := len(changedExisting)
	if shownModified > codingSubAgentFileChangeSummaryMax {
		shownModified = codingSubAgentFileChangeSummaryMax
	}
	for _, f := range changedExisting[:shownModified] {
		b.WriteString("- modified: `")
		b.WriteString(strings.ReplaceAll(f, "`", "'"))
		b.WriteString("`\n")
	}
	if remaining := len(changedExisting) - shownModified; remaining > 0 {
		b.WriteString(fmt.Sprintf("- ... 还有 %d 个修改文件未展开\n", remaining))
	}
	return strings.TrimRight(b.String(), "\n")
}

func uniqueSortedSubAgentStrings(items []string) []string {
	out := uniqueSubAgentStrings(items)
	sort.Strings(out)
	return out
}

func uniqueSubAgentStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func nonEmptySubAgentStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func appendSubAgentGuardrailSummary(summary string, violations []CodingSubAgentGuardrailViolation) string {
	if len(violations) == 0 {
		return summary
	}
	entries := aggregateGuardrailViolations(violations)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n\n## 安全边界\n\n")
	shown := len(entries)
	if shown > codingSubAgentGuardrailSummaryMax {
		shown = codingSubAgentGuardrailSummaryMax
	}
	for _, entry := range entries[:shown] {
		v := entry.Violation
		b.WriteString("- blocked `")
		b.WriteString(escapeSubAgentInlineCode(v.Tool))
		b.WriteString("`")
		if entry.Count > 1 {
			b.WriteString(fmt.Sprintf(" x%d", entry.Count))
		}
		if v.Category != "" {
			b.WriteString(" category: `")
			b.WriteString(escapeSubAgentInlineCode(v.Category.String()))
			b.WriteString("`")
		}
		if v.Path != "" {
			b.WriteString(" path: `")
			b.WriteString(escapeSubAgentInlineCode(compactSubAgentPathText(v.Path)))
			b.WriteString("`")
		}
		if v.Command != "" {
			b.WriteString(" command: `")
			b.WriteString(escapeSubAgentInlineCode(compactSubAgentCommandText(v.Command)))
			b.WriteString("`")
		}
		if v.Summary != "" {
			b.WriteString("\n  ")
			b.WriteString(truncateRunesForSubAgent(firstLine(v.Summary), codingSubAgentGuardrailDetailMaxRunes))
		}
		b.WriteString("\n")
	}
	if remaining := len(entries) - shown; remaining > 0 {
		b.WriteString(fmt.Sprintf("- ... 还有 %d 条安全边界拦截未展开\n", remaining))
	}
	return strings.TrimRight(b.String(), "\n")
}

type aggregatedGuardrailViolation struct {
	Violation CodingSubAgentGuardrailViolation
	Count     int
}

func aggregateGuardrailViolations(violations []CodingSubAgentGuardrailViolation) []aggregatedGuardrailViolation {
	entries := make([]aggregatedGuardrailViolation, 0, len(violations))
	seen := make(map[string]int, len(violations))
	for _, v := range violations {
		key := strings.Join([]string{v.Tool, v.Category.String(), v.Path, v.Command, v.Summary}, "\x00")
		if idx, ok := seen[key]; ok {
			entries[idx].Count++
			continue
		}
		seen[key] = len(entries)
		entries = append(entries, aggregatedGuardrailViolation{Violation: v, Count: 1})
	}
	return entries
}

func compactCommandResult(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return "(无输出)"
	}
	return truncateRunesForSubAgent(result, 1000)
}

func compactGuardrailSummary(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return "(无详情)"
	}
	return truncateRunesForSubAgent(result, 500)
}

func classifyCodingGuardrailCategory(toolName, path, command, result string) CodingSubAgentGuardrailCategory {
	normalizedCommand := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if classifyCodingSubAgentTool(toolName) == codingSubAgentToolBash {
		switch {
		case hasDisallowedGitCommand(normalizedCommand):
			return codingSubAgentGuardrailCategoryGit
		case hasDisallowedRecursiveDeleteCommand(normalizedCommand):
			return codingSubAgentGuardrailCategoryDelete
		case hasDisallowedShellFileMutation(normalizedCommand):
			return codingSubAgentGuardrailCategoryShellWrite
		default:
			return codingSubAgentGuardrailCategoryCommand
		}
	}
	switch classifyCodingGuardrailResultMarker(result) {
	case codingGuardrailResultMarkerHostUnavailable:
		return codingSubAgentGuardrailCategoryHost
	case codingGuardrailResultMarkerProjectScope:
		return codingSubAgentGuardrailCategoryScope
	}
	if path != "" {
		return codingSubAgentGuardrailCategoryScope
	}
	return codingSubAgentGuardrailCategoryPolicy
}

func compactSearchResult(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return "(无输出)"
	}
	return truncateRunesForSubAgent(result, 1000)
}

func appendSubAgentCommandSummary(summary string, commands []CodingSubAgentCommandResult) string {
	if len(commands) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n\n## 命令验证\n\n")
	shown := len(commands)
	if shown > codingSubAgentCommandSummaryMax {
		shown = codingSubAgentCommandSummaryMax
	}
	for _, cmd := range commands[:shown] {
		status := "PASS"
		if !cmd.Succeeded {
			status = "FAIL"
		}
		b.WriteString("- ")
		b.WriteString(status)
		b.WriteString(": `")
		b.WriteString(escapeSubAgentInlineCode(compactSubAgentCommandText(cmd.Command)))
		b.WriteString("`")
		if cmd.WorkingDir != "" {
			b.WriteString(" (cwd: `")
			b.WriteString(escapeSubAgentInlineCode(compactSubAgentPathText(cmd.WorkingDir)))
			b.WriteString("`)")
		}
		if cmd.Summary != "" {
			b.WriteString("\n  ")
			b.WriteString(truncateRunesForSubAgent(firstLine(cmd.Summary), codingSubAgentCommandOutputLineMaxRunes))
		}
		b.WriteString("\n")
	}
	if remaining := len(commands) - shown; remaining > 0 {
		b.WriteString(fmt.Sprintf("- ... 还有 %d 条命令记录未展开\n", remaining))
	}
	return b.String()
}

func summarizeSubAgentCommands(commands []CodingSubAgentCommandResult) (codingSubAgentQualityStatus, string) {
	if len(commands) == 0 {
		return codingSubAgentQualityNone, "no bash commands run"
	}
	var failed []string
	for _, cmd := range commands {
		if !cmd.Succeeded {
			failed = append(failed, cmd.Command)
		}
	}
	if len(failed) == 0 {
		if len(commands) == 1 {
			return codingSubAgentQualityPassed, "1 bash command run, no failures"
		}
		return codingSubAgentQualityPassed, fmt.Sprintf("%d bash commands run, no failures", len(commands))
	}
	if len(failed) == 1 {
		return codingSubAgentQualityFailed, fmt.Sprintf("%d bash commands run, 1 failed: %s", len(commands), compactFailedVerificationCommands(failed))
	}
	return codingSubAgentQualityFailed, fmt.Sprintf("%d bash commands run, %d failed: %s", len(commands), len(failed), compactFailedVerificationCommands(failed))
}

func summarizeSubAgentQuality(explorationStatus, verificationStatus codingSubAgentQualityStatus, diffChecked bool, filesModified []string, commands []CodingSubAgentCommandResult, guardrails []CodingSubAgentGuardrailViolation) (codingSubAgentQualityStatus, string, int) {
	filesModified = uniqueSortedSubAgentStrings(filesModified)
	var failed []string
	var warnings []string
	if len(guardrails) > 0 {
		failed = append(failed, fmt.Sprintf("%d guardrail block(s)", len(guardrails)))
	}
	if verificationStatus == codingSubAgentQualityFailed {
		failed = append(failed, "verification failed")
	}
	failedCommands := countFailedSubAgentCommands(commands)
	if failedCommands > 0 && verificationStatus != codingSubAgentQualityFailed {
		warnings = append(warnings, fmt.Sprintf("%d command(s) failed", failedCommands))
	}
	if len(filesModified) > 0 {
		if explorationStatus == codingSubAgentQualityMissing {
			warnings = append(warnings, "no exploration before edits")
		}
		if verificationStatus == codingSubAgentQualityMissing {
			warnings = append(warnings, "verification not run")
		}
		if !diffChecked {
			warnings = append(warnings, "diff not checked")
		}
	}
	if len(failed) > 0 {
		issues := append(failed, warnings...)
		return codingSubAgentQualityFailed, strings.Join(issues, "; "), len(issues)
	}
	if len(warnings) > 0 {
		return codingSubAgentQualityWarning, strings.Join(warnings, "; "), len(warnings)
	}
	if len(filesModified) == 0 {
		return codingSubAgentQualityPassed, "no file changes; quality gates not needed", 0
	}
	return codingSubAgentQualityPassed, "exploration, verification, and diff check passed", 0
}

func countFailedSubAgentCommands(commands []CodingSubAgentCommandResult) int {
	count := 0
	for _, cmd := range commands {
		if !cmd.Succeeded {
			count++
		}
	}
	return count
}

func compactSubAgentCommandText(command string) string {
	return truncateRunesForSubAgent(strings.TrimSpace(command), codingSubAgentCommandTextMaxRunes)
}

func compactSubAgentPathText(path string) string {
	return truncateRunesForSubAgent(strings.TrimSpace(path), codingSubAgentPathTextMaxRunes)
}

func compactSubAgentFileList(files []string, maxItems int) string {
	files = uniqueSortedSubAgentStrings(files)
	if len(files) == 0 {
		return ""
	}
	shown := len(files)
	if maxItems > 0 && shown > maxItems {
		shown = maxItems
	}
	parts := make([]string, 0, shown+1)
	for _, file := range files[:shown] {
		parts = append(parts, compactSubAgentPathText(file))
	}
	if remaining := len(files) - shown; remaining > 0 {
		parts = append(parts, fmt.Sprintf("还有 %d 个文件未展开", remaining))
	}
	return strings.Join(parts, ", ")
}

func compactSubAgentSearchText(query string) string {
	return truncateRunesForSubAgent(strings.TrimSpace(query), codingSubAgentSearchTextMaxRunes)
}

func escapeSubAgentInlineCode(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}

func summarizeSubAgentVerification(filesModified []string, commands []CodingSubAgentCommandResult) (codingSubAgentQualityStatus, string) {
	if len(filesModified) == 0 {
		return codingSubAgentQualityNotNeeded, "未检测到文件修改，跳过命令验证要求。"
	}
	verificationCommands := filterSubAgentVerificationCommands(commands)
	if len(verificationCommands) == 0 {
		if len(commands) == 0 {
			return codingSubAgentQualityMissing, "检测到文件修改，但没有运行 bash 验证命令。"
		}
		return codingSubAgentQualityMissing, fmt.Sprintf("检测到文件修改，并运行了 %d 条 bash 命令，但没有发现 test/build/lint/typecheck 等验证命令。", len(commands))
	}
	var failed []string
	for _, cmd := range verificationCommands {
		if !cmd.Succeeded {
			failed = append(failed, cmd.Command)
		}
	}
	if len(failed) > 0 {
		if len(failed) == 1 {
			return codingSubAgentQualityFailed, fmt.Sprintf("有 1 条验证命令失败：%s", compactFailedVerificationCommands(failed))
		}
		return codingSubAgentQualityFailed, fmt.Sprintf("有 %d 条验证命令失败：%s", len(failed), compactFailedVerificationCommands(failed))
	}
	return codingSubAgentQualityPassed, fmt.Sprintf("已运行 %d 条 bash 验证命令，未检测到失败。", len(verificationCommands))
}

func filterSubAgentVerificationCommands(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, cmd := range commands {
		if isSubAgentVerificationCommand(cmd.Command) {
			filtered = append(filtered, cmd)
		}
	}
	return filtered
}

func isSubAgentVerificationCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return false
	}
	for _, segment := range shellCommandSegments(normalized) {
		if isSubAgentVerificationCommandSegment(segment) {
			return true
		}
	}
	return false
}

func shellCommandSegments(command string) [][]string {
	fields := shellCommandFields(command)
	segments := make([][]string, 0, 1)
	current := make([]string, 0, len(fields))
	for _, field := range fields {
		token := normalizeShellCommandToken(field)
		if token == "" {
			continue
		}
		if isShellCommandStartMarker(token) {
			if len(current) > 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, token)
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	return segments
}

func isSubAgentVerificationCommandSegment(segment []string) bool {
	if len(segment) == 0 {
		return false
	}
	segment = stripVerificationCommandPrefixes(segment)
	if len(segment) == 0 {
		return false
	}
	cmd := commandNameBase(segment[0])
	args := segment[1:]
	switch cmd {
	case "go":
		return firstArgIn(args, "test", "vet", "build", "fmt")
	case "cargo":
		return firstArgIn(args, "test", "check", "clippy", "build", "fmt")
	case "npm", "pnpm", "yarn":
		return packageManagerRunsVerification(args)
	case "npx", "pnpx", "yarnx":
		return firstArgIn(args, "tsc", "eslint", "prettier", "biome", "jest", "vitest")
	case "corepack":
		return corepackRunsVerification(args)
	case "node":
		return hasArg(args, "--test")
	case "bun", "deno":
		return firstArgIn(args, "test")
	case "python", "python3", "py":
		return len(args) >= 2 && args[0] == "-m" && isVerificationRunner(args[1])
	case "pytest", "phpunit", "rspec", "rubocop", "jest", "vitest", "eslint", "prettier", "biome", "tsc":
		return true
	case "make":
		return firstArgIn(args, "test", "check", "build", "lint", "fmt", "format", "typecheck", "type-check")
	case "mvn", "mvnw":
		return firstArgIn(args, "test", "verify")
	case "gradle", "gradlew":
		return hasArg(args, "test") || hasArg(args, "build") || hasArg(args, "check")
	case "dotnet":
		return firstArgIn(args, "test", "build")
	case "composer":
		return firstArgIn(args, "test")
	case "uv", "uvx":
		return uvRunsVerification(args)
	case "poetry", "pipenv", "hatch", "pdm", "rye":
		return pythonProjectToolRunsVerification(cmd, args)
	case "bundle", "bundler":
		return bundleRunsVerification(args)
	}
	return cmd == "test" || isVerificationRunner(cmd)
}

func commandNameBase(token string) string {
	normalized := normalizeShellExecutableToken(token)
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	normalized = strings.TrimPrefix(normalized, "./")
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		normalized = normalized[idx+1:]
	}
	return normalized
}

func stripVerificationCommandPrefixes(segment []string) []string {
	for len(segment) > 0 {
		cmd := normalizeShellExecutableToken(segment[0])
		switch {
		case isShellEnvAssignment(cmd):
			segment = segment[1:]
			continue
		case cmd == "env":
			segment = stripEnvCommandPrefix(segment[1:])
			continue
		case cmd == "cross-env" || cmd == "cross-env-shell" || cmd == "time":
			segment = segment[1:]
			continue
		}
		break
	}
	return segment
}

func stripEnvCommandPrefix(args []string) []string {
	for len(args) > 0 {
		arg := args[0]
		switch {
		case isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case envOptionConsumesValue(arg):
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "-"):
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func isShellEnvAssignment(token string) bool {
	if strings.HasPrefix(token, "$") {
		return false
	}
	name, _, ok := strings.Cut(token, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9' && i > 0) || r == '_' {
			continue
		}
		return false
	}
	return true
}

func packageManagerRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if isVerificationScriptName(args[0]) {
		return true
	}
	if args[0] == "run" && len(args) > 1 {
		return isVerificationScriptName(args[1])
	}
	if (args[0] == "exec" || args[0] == "dlx") && len(args) > 1 {
		return isVerificationRunner(args[1])
	}
	return false
}

func corepackRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "npm", "pnpm", "yarn":
		return packageManagerRunsVerification(args[1:])
	case "npx", "pnpx", "yarnx":
		return len(args) > 1 && isVerificationRunner(args[1])
	}
	return false
}

func uvRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if isVerificationRunner(args[0]) {
		return true
	}
	if args[0] == "run" && len(args) > 1 {
		return isVerificationRunner(args[1])
	}
	return false
}

func pythonProjectToolRunsVerification(cmd string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch cmd {
	case "rye":
		if isVerificationScriptName(args[0]) {
			return true
		}
	}
	if args[0] == "run" && len(args) > 1 {
		return isVerificationRunner(args[1]) || isVerificationScriptName(args[1])
	}
	return false
}

func bundleRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if isVerificationRunner(args[0]) || isVerificationScriptName(args[0]) {
		return true
	}
	if args[0] == "exec" && len(args) > 1 {
		return isVerificationRunner(args[1]) || isVerificationScriptName(args[1])
	}
	return false
}

func isVerificationScriptName(name string) bool {
	switch name {
	case "test", "tests", "check", "checks", "build", "lint", "vet", "fmt", "format", "typecheck", "type-check":
		return true
	}
	return strings.HasPrefix(name, "test:") ||
		strings.HasPrefix(name, "check:") ||
		strings.HasPrefix(name, "build:") ||
		strings.HasPrefix(name, "lint:") ||
		strings.HasPrefix(name, "typecheck:") ||
		strings.HasPrefix(name, "type-check:")
}

func isVerificationRunner(name string) bool {
	switch commandNameBase(name) {
	case "pytest", "unittest", "tox", "nox", "jest", "vitest", "eslint", "prettier", "biome", "tsc", "phpunit", "rspec", "rubocop":
		return true
	}
	return false
}

func firstArgIn(args []string, names ...string) bool {
	if len(args) == 0 {
		return false
	}
	first := normalizeShellExecutableToken(args[0])
	for _, name := range names {
		if first == name {
			return true
		}
	}
	return false
}

func hasArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func compactFailedVerificationCommands(commands []string) string {
	shown := len(commands)
	if shown > codingSubAgentFailedVerificationSummaryMax {
		shown = codingSubAgentFailedVerificationSummaryMax
	}
	parts := make([]string, 0, shown+1)
	for _, command := range commands[:shown] {
		parts = append(parts, truncateRunesForSubAgent(command, 160))
	}
	if remaining := len(commands) - shown; remaining > 0 {
		parts = append(parts, fmt.Sprintf("还有 %d 条失败命令未展开", remaining))
	}
	return strings.Join(parts, "; ")
}

func summarizeSubAgentExploration(filesModified, filesRead []string, searches []CodingSubAgentSearchResult) (codingSubAgentQualityStatus, string) {
	if len(filesModified) == 0 {
		return codingSubAgentQualityNotNeeded, "未检测到文件修改，跳过探索要求。"
	}
	successfulSearches := countSuccessfulSubAgentSearches(searches)
	if successfulSearches > 0 {
		return codingSubAgentQualityExplored, fmt.Sprintf("修改前/过程中运行了 %d 次成功搜索，并读取了 %d 个文件。", successfulSearches, len(filesRead))
	}
	if len(filesRead) > 0 {
		return codingSubAgentQualityReadOnly, fmt.Sprintf("未记录成功搜索，但读取了 %d 个文件后修改。", len(filesRead))
	}
	return codingSubAgentQualityMissing, "检测到文件修改，但没有记录成功搜索或文件读取。"
}

func countSuccessfulSubAgentSearches(searches []CodingSubAgentSearchResult) int {
	successfulSearches := 0
	for _, s := range searches {
		if s.Succeeded {
			successfulSearches++
		}
	}
	return successfulSearches
}

func appendSubAgentExplorationSummary(summary string, status codingSubAgentQualityStatus, explorationSummary string) string {
	if strings.TrimSpace(explorationSummary) == "" {
		return summary
	}
	label := status.String()
	switch status {
	case codingSubAgentQualityExplored:
		label = "EXPLORED"
	case codingSubAgentQualityReadOnly:
		label = "READ_ONLY"
	case codingSubAgentQualityMissing:
		label = "MISSING"
	case codingSubAgentQualityNotNeeded:
		label = "NOT_NEEDED"
	}
	return strings.TrimSpace(summary) + "\n\n## 探索状态\n\n" + label + ": " + explorationSummary
}

func appendSubAgentVerificationSummary(summary string, status codingSubAgentQualityStatus, verificationSummary string) string {
	if strings.TrimSpace(verificationSummary) == "" {
		return summary
	}
	label := status.String()
	switch status {
	case codingSubAgentQualityPassed:
		label = "PASS"
	case codingSubAgentQualityFailed:
		label = "FAIL"
	case codingSubAgentQualityMissing:
		label = "MISSING"
	case codingSubAgentQualityNotNeeded:
		label = "NOT_NEEDED"
	}
	return strings.TrimSpace(summary) + "\n\n## 验证状态\n\n" + label + ": " + verificationSummary
}

func applySubAgentVerificationOutcome(status TaskExecStatus, errMsg string, verificationStatus codingSubAgentQualityStatus, verificationSummary string) (TaskExecStatus, string) {
	if status != TaskExecPassed || verificationStatus != codingSubAgentQualityFailed {
		return status, errMsg
	}
	if strings.TrimSpace(verificationSummary) == "" {
		verificationSummary = "验证命令失败"
	}
	return TaskExecFailed, compactSubAgentErrorSummary(verificationSummary)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}

func (c *codingSubAgentCallbacks) OnToken(delta string) {
	if c.subagent.onToken != nil {
		c.subagent.onToken(delta)
	}
}

func (c *codingSubAgentCallbacks) OnProgress(text string) {
	if c.subagent.onProgress != nil {
		c.subagent.onProgress(text)
	}
}

func (c *codingSubAgentCallbacks) OnToolCall(name string) {
	// Tool start/finish progress is emitted as structured Coding Agent events in
	// executeToolWithOutcome, which the UI can compact into one live status row.
}

func (c *codingSubAgentCallbacks) emitToolStartedEvent(name string) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseRunning, c.task, title, "")
	event.Event = codingAgentEventKindToolStarted.String()
	event.Detail = strings.TrimSpace(name)
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitToolFinishedEvent(name, result string, outcome codingToolOutcome, duration time.Duration) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseRunning, c.task, title, "")
	event.Event = codingAgentEventKindToolFinished.String()
	event.Detail = strings.TrimSpace(name)
	event.Outcome = string(outcome)
	if outcome != codingToolOutcomeSuccess {
		event.Summary = compactCodingToolResultSummary(result)
	}
	durationMS := duration.Milliseconds()
	if durationMS == 0 {
		durationMS = 1
	}
	event.DurationMS = durationMS
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func compactCodingToolResultSummary(result string) string {
	result = firstLine(result)
	if result == "" {
		return ""
	}
	return truncateRunesForSubAgent(result, 180)
}

func (c *codingSubAgentCallbacks) emitDiffUpdatedEvent(path string, count int) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseRunning, c.task, title, "")
	event.Event = codingAgentEventKindDiffUpdated.String()
	path = strings.TrimSpace(path)
	if path != "" {
		event.Detail = fmt.Sprintf("%s (%d)", compactSubAgentPathText(path), count)
	} else {
		event.Detail = fmt.Sprintf("%d files", count)
	}
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitDiffSummaryEvent(filesModified, filesCreated []string, diffSummary string) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	snapshot := newCodingDiffSnapshot(filesModified, filesCreated, diffSummary)
	if snapshot.Count() == 0 {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	emitCodingAgentEvent(c.subagent.onProgress, newCodingAgentDiffSummaryEvent(c.task, title, snapshot))
}

func (c *codingSubAgentCallbacks) emitFileActivitySummaryEvent(filesRead, filesModified, filesCreated []string) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	filesRead = uniqueSortedSubAgentStrings(filesRead)
	filesModified = uniqueSortedSubAgentStrings(filesModified)
	filesCreated = uniqueSortedSubAgentStrings(filesCreated)
	outcome := "none"
	if len(filesModified) > 0 || len(filesCreated) > 0 {
		outcome = "changed"
	} else if len(filesRead) > 0 {
		outcome = "read_only"
	}
	detail := fmt.Sprintf("read %d / modified %d / created %d", len(filesRead), len(filesModified), len(filesCreated))
	summary := detail
	if len(filesCreated) > 0 || len(filesModified) > 0 {
		summary = fmt.Sprintf("%s; changed: %s", detail, compactSubAgentFileList(append(filesCreated, filesModified...), 5))
	} else if len(filesRead) > 0 {
		summary = fmt.Sprintf("%s; read: %s", detail, compactSubAgentFileList(filesRead, 5))
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindFileActivitySummary.String()
	event.Outcome = outcome
	event.Detail = detail
	event.Summary = truncateRunesForSubAgent(summary, 240)
	event.Count = len(filesRead) + len(filesModified) + len(filesCreated)
	event.Files = limitSubAgentStringSlice(uniqueSortedSubAgentStrings(append(filesCreated, filesModified...)), codingSubAgentResultFilesMax)
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitDiffCheckEvent(checked bool, diffSummary string, modifiedCount int) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindDiffCheck.String()
	event.Count = modifiedCount
	switch {
	case checked && strings.TrimSpace(diffSummary) != "":
		event.Outcome = "checked"
		event.Summary = truncateRunesForSubAgent(firstLine(diffSummary), 240)
	case checked:
		event.Outcome = "checked"
		event.Summary = "git diff checked"
	default:
		event.Outcome = "skipped"
		event.Summary = "no modified files"
	}
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitQualitySummaryEvent(explorationStatus, verificationStatus codingSubAgentQualityStatus, diffChecked bool, filesModified []string, commands []CodingSubAgentCommandResult, guardrails []CodingSubAgentGuardrailViolation) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	outcome, summary, count := summarizeSubAgentQuality(explorationStatus, verificationStatus, diffChecked, filesModified, commands, guardrails)
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindQualitySummary.String()
	event.Outcome = outcome.String()
	event.Summary = truncateRunesForSubAgent(firstLine(summary), 240)
	event.Count = count
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitExplorationSummaryEvent(status codingSubAgentQualityStatus, summary string, count int) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	statusText := strings.TrimSpace(status.String())
	summary = strings.TrimSpace(summary)
	if statusText == "" || summary == "" {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindExplorationSummary.String()
	event.Outcome = statusText
	event.Summary = truncateRunesForSubAgent(firstLine(summary), 240)
	event.Count = count
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitGuardrailSummaryEvent(violations []CodingSubAgentGuardrailViolation) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil || len(violations) == 0 {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	entries := aggregateGuardrailViolations(violations)
	summary := "guardrail blocked tool use"
	if len(entries) > 0 {
		v := entries[0].Violation
		parts := []string{"blocked", strings.TrimSpace(v.Tool)}
		if strings.TrimSpace(v.Category.String()) != "" {
			parts = append(parts, "category:"+strings.TrimSpace(v.Category.String()))
		}
		if strings.TrimSpace(v.Summary) != "" {
			parts = append(parts, firstLine(v.Summary))
		}
		summary = strings.Join(nonEmptySubAgentStrings(parts), " | ")
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindGuardrailSummary.String()
	event.Outcome = "blocked"
	event.Summary = truncateRunesForSubAgent(summary, 240)
	event.Count = len(violations)
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitCommandSummaryEvent(commands []CodingSubAgentCommandResult) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	outcome, summary := summarizeSubAgentCommands(commands)
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindCommandSummary.String()
	event.Outcome = outcome.String()
	event.Summary = truncateRunesForSubAgent(firstLine(summary), 240)
	event.Count = len(commands)
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitVerificationSummaryEvent(status codingSubAgentQualityStatus, summary string, count int) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	statusText := strings.TrimSpace(status.String())
	summary = strings.TrimSpace(summary)
	if statusText == "" || summary == "" {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindVerificationSummary.String()
	event.Outcome = statusText
	event.Summary = truncateRunesForSubAgent(firstLine(summary), 240)
	event.Count = count
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) OnToolResult(name string) {}

func (c *codingSubAgentCallbacks) ShouldStop() bool {
	if c != nil && c.subagent != nil && c.subagent.loopCtx != nil {
		return c.subagent.loopCtx.IsCancelled()
	}
	return false
}

func (c *codingSubAgentCallbacks) buildTaskUserMessage() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("请执行以下编码任务：\n\n## T%d: %s\n\n", c.task.Index, compactSubAgentTaskTitle(c.task.Title)))
	if c.task.Description != "" {
		b.WriteString(compactSubAgentTaskDescription(c.task.Description))
		b.WriteString("\n\n")
	}
	if len(c.task.Files) > 0 {
		b.WriteString("**涉及文件**：\n")
		appendSubAgentBulletList(&b, c.task.Files, codingSubAgentTaskFilesMax, codingSubAgentPromptBulletMaxRunes)
		b.WriteString("\n")
	}
	if len(c.task.AcceptanceCriteria) > 0 {
		b.WriteString("**验收标准**：\n")
		appendSubAgentBulletList(&b, c.task.AcceptanceCriteria, codingSubAgentAcceptanceCriteriaMax, codingSubAgentPromptBulletMaxRunes)
	}
	return b.String()
}

func compactSubAgentTaskDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}
	return truncateRunesForSubAgent(description, codingSubAgentTaskDescriptionMaxRunes)
}

// ---------------------------------------------------------------------------
// System prompt — minimal, coding-only. ~1500-2000 tokens.
// ---------------------------------------------------------------------------

func buildCodingSubAgentSystemPrompt(task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string) string {
	var b strings.Builder

	b.WriteString(`你是一个专注的编码执行器。你的唯一职责是完成分配给你的编码任务。

## 工具使用策略（严格遵守）

### 读取：先理解再动手
- 优先用 Glob 查找相关文件，用 ripgrep 搜索函数、类型、配置项或错误信息。
- 所有读取、搜索、列目录都必须限定在项目路径内；不要读取项目外文件。
- 修改已有文件前，必须先 read_file 查看当前内容。不要凭记忆修改。
- 大文件用 read_file(start_line=N, lines=50) 只读相关段落，不要读整个文件。

### 修改已有文件：edit_file / edit_lines（patch 模式）
- 修改已有文件时，必须优先使用 edit_file 或 edit_lines，不要用 write_file 重写整个文件。
- **edit_file**（搜索替换）：提供 old_string 和 new_string。适合改动内容明确、上下文唯一的场景。
- **edit_lines**（行号编辑）：提供行号范围和新内容。适合改动位置明确（先 read_file 看到行号）或文件中有重复内容的场景。
  - replace: edit_lines(path, operation="replace", start_line=10, end_line=12, content="新内容")
  - insert:  edit_lines(path, operation="insert", start_line=5, content="插入的行") — 在第 5 行后插入
  - delete:  edit_lines(path, operation="delete", start_line=10, end_line=15)
- 一个文件需要改多处时，对每处分别调用，不要试图一次替换完。
- edit_file 失败（"未找到要替换的内容"）时，用 read_file 确认当前内容，改用 edit_lines 按行号编辑。

### 创建新文件：write_file
- 只有创建全新文件时才用 write_file(mode=overwrite)。
- 大文件（>3000 字符）分块写入：先 overwrite 第一部分，再 append 后续部分。

### 禁止行为
- 禁止用 write_file 重写已有文件来做小修改——这浪费 token 且容易丢失原文件中的其他内容。
- 禁止不读文件就直接修改——你不知道文件当前的确切内容，edit_file 的 old_string 会匹配失败。
- 禁止执行破坏性删除、清理或 Git 回滚命令，例如 git reset --hard、git checkout --、git checkout .、git restore、git clean -f、rm -rf、Remove-Item -Recurse、rmdir /s、del /s。
- bash 的 working_dir 必须在项目路径内；相对路径会按项目路径解析。

## 编码规范
- 每次修改后运行编译/构建命令验证，确保代码可编译。
- 完成前调用 git_diff 自检，确认改动范围符合任务要求。
- write_file 始终 UTF-8 编码，直接写中文即可。
- 完成后简要总结：列出修改的文件、每个文件改了什么、运行过哪些验证命令、是否还有残余风险。
- 遇到无法解决的问题，说明具体原因，不要反复重试相同的失败操作。
`)

	b.WriteString(`
## Single-task contract
- Work only on the assigned task. Avoid broad refactors, unrelated formatting, dependency churn, or speculative feature work.
- Keep edits small and reviewable. Prefer targeted patches over whole-file rewrites.
- If verification fails because of unrelated pre-existing errors, report the exact blocker with file/line when available and do not rewrite unrelated areas unless they block this task directly.
- Before the final answer, inspect the diff, summarize created/modified files, list verification commands, and call out remaining risk.

## Command guardrails
- Do not run Git commands that rewrite or move worktree state: reset, checkout, restore, switch, merge, rebase, stash, or clean -f. Read-only Git commands such as status, diff, and log are allowed.
- Do not run recursive or forceful delete commands such as rm -r/-rf, Remove-Item -Recurse/-r/-rf, ri -r, rd/rmdir /s, del /s, or erase /s. Use edit_file/edit_lines/write_file for scoped file changes.
- Do not mutate files through bash redirection or shell helpers: >, >>, tee/Tee-Object, Set-Content/Add-Content/Out-File, touch/mkdir, Copy-Item/Move-Item/Rename-Item, sed -i, perl -pi, node fs.writeFileSync/promises.writeFile, Python open(..., "w")/Path.write_text, or dd of=. Use the file editing tools instead.
`)

	b.WriteString(fmt.Sprintf("\n## 项目路径\n%s\n", projectPath))

	// Platform hint so the LLM generates correct shell commands.
	b.WriteString(fmt.Sprintf("平台: %s\n", normalizedRemotePlatform()))
	if normalizedRemotePlatform() == "windows" {
		b.WriteString("注意: bash 工具通过 PowerShell 执行。使用 PowerShell 语法（如 `;` 分隔命令，`Remove-Item` 删除文件）。\n")
	}

	if reqCtx != "" {
		b.WriteString("\n## 需求摘要\n")
		b.WriteString(truncateRunesForSubAgent(reqCtx, 800))
		b.WriteString("\n")
	}

	if designCtx != "" {
		b.WriteString("\n## 设计摘要\n")
		b.WriteString(truncateRunesForSubAgent(designCtx, 800))
		b.WriteString("\n")
	}

	if len(prevOutputs) > 0 {
		b.WriteString("\n## 前置任务产出\n")
		appendSubAgentBulletList(&b, prevOutputs, codingSubAgentPrevOutputsMax, codingSubAgentPromptBulletMaxRunes)
	}

	now := time.Now()
	b.WriteString(fmt.Sprintf("\n当前时间: %s\n", now.Format("2006-01-02 15:04")))

	return b.String()
}

func appendSubAgentBulletList(b *strings.Builder, items []string, maxItems, maxRunes int) {
	if maxItems <= 0 {
		maxItems = len(items)
	}
	shown := len(items)
	if shown > maxItems {
		shown = maxItems
	}
	for _, item := range items[:shown] {
		b.WriteString("- ")
		b.WriteString(truncateRunesForSubAgent(item, maxRunes))
		b.WriteString("\n")
	}
	if remaining := len(items) - shown; remaining > 0 {
		b.WriteString(fmt.Sprintf("- ... 还有 %d 项未展开\n", remaining))
	}
}

// truncateRunesForSubAgent truncates a string to maxRunes, preferring paragraph boundaries.
func truncateRunesForSubAgent(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	// Try to cut at a paragraph boundary.
	cutoff := string(runes[:maxRunes])
	if idx := strings.LastIndex(cutoff, "\n\n"); idx > len(cutoff)/2 {
		return cutoff[:idx] + "\n…（已截断）"
	}
	if idx := strings.LastIndex(cutoff, "\n"); idx > len(cutoff)/2 {
		return cutoff[:idx] + "\n…（已截断）"
	}
	return cutoff + "…（已截断）"
}

// ---------------------------------------------------------------------------
// Tool definitions — compact coding tool belt.
// Extracted from the host's tool registry to avoid duplicate definitions.
// If the registry is not available (e.g. in tests), falls back to minimal
// inline definitions.
// ---------------------------------------------------------------------------

// codingSubAgentToolOrder is the single source of truth for the compact tool
// belt. The membership map is derived from it below.
const (
	codingSubAgentDefaultBashTimeout           = corelib.DefaultAgentTimeoutSec
	codingSubAgentMaxBashTimeout               = corelib.MaxAgentTimeoutSec
	codingSubAgentGuardrailSummaryMax          = 5
	codingSubAgentGuardrailDetailMaxRunes      = 240
	codingSubAgentCommandSummaryMax            = 10
	codingSubAgentCommandTextMaxRunes          = 240
	codingSubAgentCommandOutputLineMaxRunes    = 240
	codingSubAgentPathTextMaxRunes             = 180
	codingSubAgentSearchTextMaxRunes           = 180
	codingSubAgentFailedVerificationSummaryMax = 5
	codingSubAgentFileChangeSummaryMax         = 20
	codingSubAgentResultFilesMax               = 80
	codingSubAgentResultAuditMax               = 50
	codingSubAgentModelSummaryMaxRunes         = 4000
	codingSubAgentErrorSummaryMaxRunes         = 1000
	codingSubAgentReportSummaryMaxRunes        = 8000
	codingSubAgentRunReportMaxItems            = 30
	codingSubAgentRunReportMaxRunes            = 6000
	codingSubAgentTaskTitleMaxRunes            = 160
	codingSubAgentTaskDescriptionMaxRunes      = 2000
	codingSubAgentTaskListSummaryMax           = 50
	codingSubAgentTaskFilesMax                 = 30
	codingSubAgentDependencySummaryMax         = 20
	codingSubAgentAcceptanceCriteriaMax        = 20
	codingSubAgentPrevOutputsMax               = 20
	codingSubAgentPromptBulletMaxRunes         = 160
)

var codingSubAgentToolOrder = []string{
	"Glob",
	"ripgrep",
	"read_file",
	"edit_file",
	"edit_lines",
	"write_file",
	"bash",
	"list_directory",
	"git_diff",
}

var codingSubAgentToolNames = makeCodingSubAgentToolNameSet(codingSubAgentToolOrder)

func makeCodingSubAgentToolNameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// buildCodingToolDefinitionsFromRegistry extracts tool definitions from the
// host's registry, filtered to only coding tools. This ensures parameter
// changes (e.g. adding offset to read_file) are automatically reflected
// in the SubAgent without maintaining a separate copy.
func buildCodingToolDefinitionsFromRegistry(handler *IMMessageHandler) []map[string]interface{} {
	if handler == nil {
		return buildCodingToolDefinitionsFallback()
	}
	allTools := handler.getTools()
	if len(allTools) == 0 {
		return buildCodingToolDefinitionsFallback()
	}

	fallbacks := buildCodingToolDefinitionsFallback()
	byName := make(map[string]map[string]interface{}, len(codingSubAgentToolNames))
	for _, t := range allTools {
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if codingSubAgentToolNames[name] {
			byName[name] = t
		}
	}

	// The GUI registry does not currently expose every coding-agent-only tool.
	// Keep registry definitions when present, then fill gaps from fallback defs.
	for _, t := range fallbacks {
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if codingSubAgentToolNames[name] {
			if _, exists := byName[name]; !exists {
				byName[name] = t
			}
		}
	}

	ordered := make([]map[string]interface{}, 0, len(codingSubAgentToolNames))
	for _, name := range codingSubAgentToolOrder {
		if t, ok := byName[name]; ok {
			ordered = append(ordered, t)
		}
	}
	if len(ordered) == 0 {
		return fallbacks
	}
	return ordered
}

// buildCodingToolDefinitionsFallback provides minimal inline definitions
// for testing or when the registry is unavailable.
func buildCodingToolDefinitionsFallback() []map[string]interface{} {
	core := agent.NewCoreToolRegistry()
	agent.RegisterCoreTools(core, agent.CoreToolDeps{})
	byName := make(map[string]map[string]interface{})
	for _, t := range core.BuildDefinitions() {
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if codingSubAgentToolNames[name] {
			byName[name] = t
		}
	}

	for _, t := range codingSubAgentExtraToolDefinitions() {
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if codingSubAgentToolNames[name] {
			byName[name] = t
		}
	}

	tools := make([]map[string]interface{}, 0, len(codingSubAgentToolOrder))
	for _, name := range codingSubAgentToolOrder {
		if t, ok := byName[name]; ok {
			tools = append(tools, compactCodingSubAgentToolDefinition(t))
		}
	}
	return tools
}

func compactCodingSubAgentToolDefinition(tool map[string]interface{}) map[string]interface{} {
	fn, ok := tool["function"].(map[string]interface{})
	if !ok {
		return tool
	}
	params, ok := fn["parameters"].(map[string]interface{})
	if !ok {
		return tool
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		return tool
	}
	compactProps := make(map[string]interface{}, len(props))
	for name, raw := range props {
		switch prop := raw.(type) {
		case map[string]interface{}:
			copyProp := make(map[string]interface{}, len(prop))
			for key, value := range prop {
				if key == "description" {
					continue
				}
				copyProp[key] = value
			}
			compactProps[name] = copyProp
		case map[string]string:
			copyProp := make(map[string]string, len(prop))
			for key, value := range prop {
				if key == "description" {
					continue
				}
				copyProp[key] = value
			}
			compactProps[name] = copyProp
		default:
			compactProps[name] = raw
		}
	}
	compactParams := make(map[string]interface{}, len(params))
	for key, value := range params {
		if key == "properties" {
			compactParams[key] = compactProps
			continue
		}
		compactParams[key] = value
	}
	compactFn := make(map[string]interface{}, len(fn))
	for key, value := range fn {
		if key == "parameters" {
			compactFn[key] = compactParams
			continue
		}
		compactFn[key] = value
	}
	compactTool := make(map[string]interface{}, len(tool))
	for key, value := range tool {
		if key == "function" {
			compactTool[key] = compactFn
			continue
		}
		compactTool[key] = value
	}
	return compactTool
}

func codingSubAgentExtraToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		buildToolDef("edit_lines", "按行号精确编辑文件（替换/插入/删除指定行）",
			map[string]interface{}{
				"path":       map[string]string{"type": "string", "description": "文件路径"},
				"operation":  map[string]string{"type": "string", "description": "操作: replace/insert/delete"},
				"start_line": map[string]string{"type": "integer", "description": "起始行号（1-indexed，insert 时 0=文件开头）"},
				"end_line":   map[string]string{"type": "integer", "description": "结束行号（replace/delete 时必填）"},
				"content":    map[string]string{"type": "string", "description": "新内容（replace/insert 时必填）"},
			}, []string{"path", "operation", "start_line"}),
		buildToolDef("git_diff", "查看当前 Git 工作区 diff，用于完成前自检改动范围。只读，不会修改文件。",
			map[string]interface{}{
				"path":   map[string]string{"type": "string", "description": "Git 仓库路径（可选，默认项目路径）"},
				"staged": map[string]string{"type": "boolean", "description": "是否查看已暂存 diff（可选）"},
			}, nil),
	}
}

func buildToolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	return agent.ToolDef(name, desc, props, required)
}

// ---------------------------------------------------------------------------
// Convenience: run a task through the SubAgent from the orchestrator.
// ---------------------------------------------------------------------------

// RunTaskWithSubAgent creates a SubAgent and executes a single task.
// This is the entry point called by the main agent loop when the orchestrator
// delegates a task to the SubAgent instead of the external coding tool.
func RunTaskWithSubAgent(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	task *TaskItem,
	projectPath, reqCtx, designCtx string,
	prevOutputs []string,
	loopCtx *LoopContext,
	onToken func(string),
	onProgress func(string),
) *CodingSubAgentResult {
	// OpenHuman-inspired: route to reasoning model for coding tasks
	if handler != nil && handler.app != nil && handler.app.ohModules.modelRouter != nil {
		if handler.app.ohModules.modelRouter.HasRoute("reasoning") {
			cfg = handler.routeLLMConfig("reasoning")
		}
	}
	sa := NewCodingSubAgent(handler, cfg, httpClient, projectPath, loopCtx)
	sa.SetCallbacks(onToken, onProgress)
	return sa.ExecuteTask(task, reqCtx, designCtx, prevOutputs)
}
