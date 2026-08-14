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
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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

	// scopeApproval handles interactive user confirmation when the SubAgent
	// attempts to access paths outside the declared projectPath. When nil,
	// out-of-scope access is hard-rejected (legacy behavior).
	scopeApproval *scopeApprovalState

	// fullEnvironment enables Claude Code / Codex–aligned posture: broader
	// skill/MCP surface, project workspace probe, and full-workbench prompts.
	// Used by create-task pure coding mode (coding_dev / remote_coding_dev).
	fullEnvironment bool

	// nestDepth is 0 for the pure-coding root turn. Nested spawn_coding_agent
	// children increment this; spawn is disabled at codingSubAgentMaxNestDepth.
	nestDepth int
	// role specializes the tool surface for nested agents (explorer/worker/reviewer).
	// Empty means worker (full coding surface).
	role codingSubAgentRole

	// Knowledge stores (both optional, nil = gracefully skipped)
	codingKB  *knowledge.CodingKnowledgeStore // coding experiences (coding_knowledge.db)
	generalKB *knowledge.SQLiteStore          // project docs (knowledge.db)

	// attachments are optional user images/files for the first user turn
	// (pure-coding vision / screenshot-to-code). Nested spawn children inherit none.
	attachments []agent.MessageAttachment

	// runtimeStore/runtimeAttempt are ephemeral bindings for a currently
	// executing ledger-backed parent. They are never persisted or reused for
	// recovery; they exist solely to admit a read-only child and release the
	// parent lease through corelib while the parent loop is still alive.
	runtimeStore   codingruntime.Store
	runtimeAttempt *codingruntime.Attempt
	// executionCtx is set only on an admitted detached child. It is distinct
	// from loopCtx: normal parent handoff must not cancel the child, while an
	// explicit Runtime cancellation must interrupt its current model/tool wait.
	executionCtx context.Context
}

// ExecuteReadOnlyChild implements codingruntime.ReadOnlyChildExecutor. The
// corelib runner owns the child Attempt; this GUI adapter supplies only the
// existing read-only explorer/reviewer loop and a bounded evidence digest.
func (s *CodingSubAgent) ExecuteReadOnlyChild(ctx context.Context, request codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
	if s == nil || (s.role != codingRoleExplorer && s.role != codingRoleReviewer) {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "GUI read-only child adapter requires explorer or reviewer role"}
	}
	if !request.Attempt.Policy.ReadOnly {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "read-only child policy missing"}
	}
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCancelled, Summary: "read-only child cancelled before execution"}
	}
	child := *s
	// RunReadOnlyChild owns a fresh child Attempt. Binding it here gives the
	// local callback the same durable cancellation/lease boundary as remote
	// children, without ever reusing the parent's released Attempt.
	child.runtimeAttempt = &request.Attempt
	child.executionCtx = ctx
	result := child.ExecuteTask(&TaskItem{Index: 1, Title: request.Task.RequestedWork, Description: request.Task.RequestedWork, Status: TaskExecPending}, codingSpawnRolePromptHint(child.role), "", nil)
	if result == nil {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskFailed, Summary: "GUI read-only child returned no result"}
	}
	status := codingruntime.TaskFailed
	switch result.Status {
	case TaskExecPassed:
		status = codingruntime.TaskCompleted
	case TaskExecInterrupted:
		status = codingruntime.TaskInterrupted
	}
	digest := codingRuntimeDigest(result.Summary + "\n" + result.Error + "\n" + strings.Join(result.FilesRead, "\n"))
	return codingruntime.ChildTaskResult{TaskID: request.Task.TaskID, AttemptID: request.Attempt.AttemptID, Status: status, Summary: result.Summary, EvidenceDigest: digest}
}

// CodingSubAgentResult is the outcome of a single task execution.
type CodingSubAgentResult struct {
	Status  TaskExecStatus // passed, failed, skipped
	Summary string         // human-readable summary of what was done
	Error   string         // error message if failed
	// RuntimeTaskID is the opaque durable ledger task reference for this
	// execution. It is used for recovery/projection only, never replay.
	RuntimeTaskID string
	// RuntimeHandoff is true only when this result represents a durable
	// waiting_child parent handoff. Workflow V2 preserves it separately from a
	// generic skipped status so users can explicitly review child results.
	RuntimeHandoff bool
	Iterations     int
	ToolCalls      int

	// Token / cost accounting for this SubAgent loop (when provider reports usage).
	InputTokens  int
	OutputTokens int
	EstCostRMB   float64

	// RouteModel / RouteSource / RouteTask describe model routing for this loop.
	RouteModel  string
	RouteSource string
	RouteTask   string
	RouteReason string

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

	// DiffStat holds structured diff statistics parsed from `git diff --stat`.
	// Nil when diff was not checked or the project is not a git repo.
	DiffStat *SubAgentDiffStat

	// CommandsRun records bash commands executed during the coding task.
	CommandsRun []CodingSubAgentCommandResult

	// SearchesRun records Glob/ripgrep exploration calls.
	SearchesRun []CodingSubAgentSearchResult

	// GuardrailViolations records safety/scope rejections encountered during tool execution.
	GuardrailViolations []CodingSubAgentGuardrailViolation

	// DynamicToolsRun records manage_skill/call_mcp_tool host executions.
	DynamicToolsRun []CodingSubAgentDynamicToolResult

	// ExplorationStatus summarizes whether the agent explored before editing:
	// explored, read_only, missing, or not_needed.
	ExplorationStatus  codingSubAgentQualityStatus
	ExplorationSummary string

	// VerificationStatus summarizes command-based verification:
	// passed, failed, missing, or not_needed.
	VerificationStatus  codingSubAgentQualityStatus
	VerificationSummary string

	// QualityStatus is the combined audit outcome across exploration,
	// verification, diff, guardrails, command failures, and dynamic tool failures.
	QualityStatus     codingSubAgentQualityStatus
	QualitySummary    string
	QualityIssueCount int

	// Localization records structured root-cause evidence for bug-fix tasks.
	// It is nil for non-debugging work and for legacy callers that did not need
	// the bug-localization workflow.
	Localization *CodingSubAgentLocalizationEvidence
}

// CodingSubAgentCommandResult is a compact audit record for a bash command.
type CodingSubAgentCommandResult struct {
	Command    string
	WorkingDir string
	Succeeded  bool
	Summary    string
	seq        uint64
}

// CodingSubAgentSearchResult is a compact audit record for code exploration.
type CodingSubAgentSearchResult struct {
	Tool             string
	Query            string
	Path             string
	Succeeded        bool
	Summary          string
	FetchOffset      int
	FetchNextOffset  int
	FetchTotalChars  int
	FetchHasMore     bool
	FetchRangeKnown  bool
	FetchAuditKnown  bool
	FetchResolvedURL string
	seq              uint64
}

// CodingSubAgentGuardrailViolation is a compact audit record for blocked tool use.
type CodingSubAgentGuardrailViolation struct {
	Tool     string
	Category CodingSubAgentGuardrailCategory
	Path     string
	Command  string
	Summary  string
	seq      uint64
}

// CodingSubAgentDynamicToolResult is a compact audit record for host-backed dynamic tools.
type CodingSubAgentDynamicToolResult struct {
	Tool      string
	Name      string
	Succeeded bool
	Summary   string
	seq       uint64
}

type codingToolOutcome string

const (
	codingToolOutcomeSuccess codingToolOutcome = "success"
	codingToolOutcomeFailed  codingToolOutcome = "failed"
	codingToolOutcomeBlocked codingToolOutcome = "blocked"
	codingToolOutcomeTimeout codingToolOutcome = "timeout"
)

const codingSubAgentInlineContentLimit = 1800

type codingToolExecutionResult struct {
	Text                          string
	Outcome                       codingToolOutcome
	SkipRejectedDynamicToolRecord bool
}

var codingSubAgentFreeformSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)((?:"|')?authorization(?:"|')?\s*[:=]\s*(?:"|')?(?:bearer|basic)\s+)[^\s,"';]+`),
	regexp.MustCompile(`(?i)((?:"|')?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)(?:"|')?\s*[:=]\s*(?:"|')?)[^\s,"';]+`),
	regexp.MustCompile(`(?i)((?:--|/)(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)\s+")[^"\r\n]*`),
	regexp.MustCompile(`(?i)((?:--|/)(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)\s+')[^'\r\n]*`),
	regexp.MustCompile(`(?i)((?:--|/)(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)\s+(?:"|')?)[^\s,"';]+`),
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

// SetFullEnvironment enables the full coding workbench posture (skill/MCP
// breadth, workspace probe, Claude Code–aligned prompt). Safe to call on nil.
func (s *CodingSubAgent) SetFullEnvironment(enabled bool) {
	if s != nil {
		s.fullEnvironment = enabled
	}
}

// SetAttachments attaches user media (images) for multimodal pure-coding turns.
func (s *CodingSubAgent) SetAttachments(atts []agent.MessageAttachment) {
	if s == nil {
		return
	}
	if len(atts) == 0 {
		s.attachments = nil
		return
	}
	s.attachments = append([]agent.MessageAttachment(nil), atts...)
}

func (s *CodingSubAgent) isFullEnvironment() bool {
	return s != nil && s.fullEnvironment
}

// seedFullEnvironmentWorkspaceApprovals pre-approves the bound project root
// and its parent directory for full-env sessions so monorepo/sibling reads
// do not stop the agent on the first out-of-boundary path (Claude Code–style
// workspace trust for the chosen coding directory).
func (s *CodingSubAgent) seedFullEnvironmentWorkspaceApprovals() {
	if s == nil || !s.fullEnvironment || s.scopeApproval == nil {
		return
	}
	root := strings.TrimSpace(s.projectPath)
	if root == "" {
		return
	}
	s.scopeApproval.approveDir(root)
}

// SetScopeApprovalCallback configures interactive user confirmation for
// out-of-scope file access. When set, the SubAgent will pause and ask the
// user before rejecting operations on paths outside projectPath.
// If not set (nil), out-of-scope access is hard-rejected (legacy behavior).
// fullAccess: if true, all scope checks are bypassed (user previously granted permanent access).
func (s *CodingSubAgent) SetScopeApprovalCallback(callback ScopeApprovalCallback, fullAccess bool) {
	s.scopeApproval = newScopeApprovalState(callback, fullAccess)
}

// prepareTaskScopeApproval resolves declared absolute paths before model
// execution. Tool-level checks remain in place for paths discovered later.
func (s *CodingSubAgent) prepareTaskScopeApproval(task *TaskItem) string {
	if s == nil || task == nil || strings.TrimSpace(s.projectPath) == "" {
		return ""
	}
	for _, path := range collectTaskAbsolutePaths(task) {
		withinProject, err := isPathWithinDir(path, s.projectPath)
		if err == nil && withinProject {
			continue
		}
		if s.scopeApproval == nil {
			return formatScopeRejection("task_scope", path, s.projectPath)
		}
		if rejection := s.scopeApproval.check("task_scope", path, s.projectPath); rejection != "" {
			return rejection
		}
	}
	return ""
}

func scopeApprovalRequiredCodingSubAgentResult(errMsg string) *CodingSubAgentResult {
	errMsg = compactSubAgentErrorSummary(errMsg)
	return &CodingSubAgentResult{
		Status:         TaskExecWaitingApproval,
		Summary:        "Coding task was not started because required scope approval was not granted.",
		Error:          "scope_approval_required: " + errMsg,
		QualityStatus:  codingSubAgentQualityMissing,
		QualitySummary: "scope approval was not granted before model execution",
	}
}

func failedCodingSubAgentStartResult(errMsg string) *CodingSubAgentResult {
	errMsg = compactSubAgentErrorSummary(errMsg)
	if errMsg == "" {
		errMsg = "coding subagent failed to start"
	}
	result := &CodingSubAgentResult{
		Status:            TaskExecFailed,
		Summary:           "任务运行错误：" + errMsg,
		Error:             errMsg,
		QualityStatus:     codingSubAgentQualityFailed,
		QualitySummary:    errMsg,
		QualityIssueCount: 1,
		Localization:      nil,
	}
	result.Summary = appendSubAgentQualityReportSummary(result.Summary, result)
	return result
}

// ExecuteTask runs a single task in a clean coding context.
// The conversation is independent — no IM rules, no memory, no 40+ tools.
func (s *CodingSubAgent) ExecuteTask(task *TaskItem, reqCtx, designCtx string, prevOutputs []string) *CodingSubAgentResult {
	if s == nil {
		log.Printf("[coding-subagent] task start failed: subagent is nil")
		return failedCodingSubAgentStartResult("coding subagent is nil")
	}
	if task == nil {
		log.Printf("[coding-subagent] task start failed: task is nil (project=%s)", s.projectPath)
		return failedCodingSubAgentStartResult("coding subagent task is nil")
	}

	taskTitle := compactSubAgentTaskTitle(task.Title)
	log.Printf("[coding-subagent] starting task T%d: %s (project=%s)", taskDisplayNumber(task), taskTitle, s.projectPath)

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

	userText := cb.buildTaskUserMessage()
	userContent := codingSubAgentUserContent(s, userText)

	parentSessionID := ""
	userID := ""
	if s.loopCtx != nil {
		parentSessionID = s.loopCtx.ID
		userID = strings.TrimSpace(s.loopCtx.UserID)
	}
	sessionID := fmt.Sprintf("coding-subagent-T%d-%d", taskDisplayNumber(task), time.Now().UnixNano())
	traj := startSubAgentTrajectory(
		s.handler,
		"coding_subagent",
		sessionID,
		userID,
		"coding_subagent",
		parentSessionID,
		s.cfg,
		cb.BuildSystemPrompt(userText, true),
		cb.BuildTools(userText),
	)
	if traj != nil {
		defer flushSubAgentTrajectory(traj)
	}

	result := codingagent.Run(cb, userText, userContent, nil, s.httpClient, s.buildLoopHooks(cb))
	// Record main loop turns first; seal outcome after optional post-loop verify/fix.
	appendSubAgentLoopResult(traj, result, false)

	// Nested explorer/reviewer are inspection-only — do not run write-oriented
	// post-loop verify/fix cycles or the full implementation quality matrix.
	if s.nestDepth > 0 && (s.role == codingRoleExplorer || s.role == codingRoleReviewer) {
		sealSubAgentTrajectory(traj, result)
		if s.handler != nil {
			accumulateLoopResultUsage(s.handler.app, s.cfg, result)
		}
		return s.finishInspectionRoleTask(cb, task, taskTitle, result)
	}

	// Run/build/demo style follow-ups must not enter implement-oriented post-loop
	// verify+fix (that is for code-change tasks).
	operational := codingTaskLooksOperational(task)
	inquiry := codingTaskLooksInquiry(task)

	// Codex-inspired post-loop verification: if the model completed without
	// errors but didn't run verification itself, automatically verify + fix.
	if !operational && !inquiry && result.Error == "" && !result.HardExit && !cb.ShouldStop() {
		if verifyCmd := detectProjectVerifyCommand(s.projectPath); verifyCmd != "" {
			// Check if model already ran a verification command during the loop
			if !hasSubAgentSelfVerified(cb) {
				s.runPostLoopVerifyFixCycle(cb, &result, verifyCmd, traj)
			}
		}
	}
	sealSubAgentTrajectory(traj, result)
	// Accumulate after post-loop so fix-loop token usage is not dropped.
	if s.handler != nil {
		accumulateLoopResultUsage(s.handler.app, s.cfg, result)
	}

	status := TaskExecPassed
	errMsg := ""
	if result.Error != "" {
		status = TaskExecFailed
		errMsg = compactSubAgentErrorSummary(result.Error)
		log.Printf("[coding-subagent] task T%d agent loop failed: %s", taskDisplayNumber(task), errMsg)
	}
	if result.HardExit {
		status = TaskExecFailed
		errMsg = "模型连续返回空响应，任务中断"
	}

	modelSummary := compactSubAgentModelSummary(result.Text)
	summary := modelSummary
	if summary == "" {
		summary = fallbackSubAgentTaskSummary(status, task, result.Iterations, result.ToolCalls)
	}

	audit := collectSubAgentAudit(cb)
	allFilesModified := audit.AllFilesModified
	allFilesCreated := audit.AllFilesCreated
	allFilesRead := audit.AllFilesRead
	allCommandsRun := audit.AllCommandsRun
	allSearchesRun := audit.AllSearchesRun
	allGuardrailViolations := audit.AllGuardrailViolations
	allDynamicToolsRun := audit.AllDynamicToolsRun
	filesModified := audit.FilesModified
	filesCreated := audit.FilesCreated
	filesRead := audit.FilesRead
	commandsRun := audit.CommandsRun
	searchesRun := audit.SearchesRun
	guardrailViolations := audit.GuardrailViolations
	dynamicToolsRun := audit.DynamicToolsRun

	var (
		explorationStatus, verificationStatus   codingSubAgentQualityStatus
		explorationSummary, verificationSummary string
		diffChecked                             bool
		diffSummary                             string
		qualityStatus                           codingSubAgentQualityStatus
		qualitySummary                          string
		qualityIssueCount                       int
	)

	if inquiry {
		// Repository questions are evidence-gathering turns. They must never be
		// forced through TDD, edit verification, or diff gates just because the
		// conversation happens to be inside a coding workbench.
		explorationStatus, explorationSummary = codingSubAgentQualityNotNeeded, "repository inquiry: read-only evidence gathering"
		verificationStatus, verificationSummary = codingSubAgentQualityNotNeeded, "repository inquiry: no code change requested"
		diffChecked = false
		diffSummary = ""
		cb.emitExplorationSummaryEvent(explorationStatus, explorationSummary, 0)
		cb.emitVerificationSummaryEvent(verificationStatus, verificationSummary, 0)
		if len(allCommandsRun) > 0 {
			summary = appendSubAgentCommandSummary(summary, allCommandsRun)
		}
		cb.emitCommandSummaryEvent(allCommandsRun)
		if result.ToolCalls == 0 || (len(allFilesRead) == 0 && len(allSearchesRun) == 0 && len(allCommandsRun) == 0) {
			status = TaskExecFailed
			errMsg = "repository inquiry completed without inspection evidence"
			qualityStatus, qualitySummary, qualityIssueCount = codingSubAgentQualityFailed, errMsg, 1
		} else {
			qualityStatus, qualitySummary, qualityIssueCount = codingSubAgentQualityPassed, "repository inquiry: inspection evidence gathered", 0
		}
	} else if operational {
		// Lightweight path: require successful launch/build bash evidence, not
		// file edits / git_diff / acceptance-criteria matrix.
		explorationStatus, explorationSummary = codingSubAgentQualityNotNeeded, "operational request: exploration not required"
		verificationStatus, verificationSummary = codingSubAgentQualityNotNeeded, "operational request: implement verification gates not required"
		// Do not claim git_diff was checked — that is implement-path only.
		diffChecked = false
		diffSummary = ""
		// Keep UI banners consistent with implement path (NOT_NEEDED, not blank).
		cb.emitExplorationSummaryEvent(explorationStatus, explorationSummary, 0)
		cb.emitVerificationSummaryEvent(verificationStatus, verificationSummary, 0)
		unresolvedGuardrailViolations := unresolvedSubAgentGuardrailViolations(allGuardrailViolations, filterPostEditSubAgentCommands(allCommandsRun, audit.LastEditSeq))
		status, errMsg = applySubAgentGuardrailOutcome(status, errMsg, unresolvedGuardrailViolations)
		if len(unresolvedGuardrailViolations) > 0 {
			summary = appendSubAgentGuardrailSummary(summary, unresolvedGuardrailViolations)
		}
		cb.emitGuardrailSummaryEvent(unresolvedGuardrailViolations)
		if len(allCommandsRun) > 0 {
			summary = appendSubAgentCommandSummary(summary, allCommandsRun)
		}
		cb.emitCommandSummaryEvent(allCommandsRun)
		if modelSummary == "" {
			summary = rebaseFallbackSubAgentTaskSummary(summary, status, task, result.Iterations, result.ToolCalls)
		}
		// Prefer an ops-specific empty-loop diagnostic over the generic hard-exit text.
		if result.HardExit && result.ToolCalls == 0 && status == TaskExecFailed {
			errMsg = "模型未调用工具就结束了；运行/演示类任务需要 bash 启动或构建程序"
		}
		qualityStatus, qualitySummary, qualityIssueCount = summarizeOperationalSubAgentQuality(audit, result)
		status, errMsg = applySubAgentQualityOutcome(status, errMsg, qualityStatus, qualitySummary, qualityIssueCount)
	} else {
		existingFilesModified := existingSubAgentModifiedFiles(allFilesModified, allFilesCreated)
		explorationStatus, explorationSummary = summarizeSubAgentExploration(existingFilesModified, allFilesRead, allSearchesRun, audit.ExploredBeforeFirstEdit)
		verificationStatus, verificationSummary = summarizeSubAgentVerification(allFilesModified, allCommandsRun, audit.LastEditSeq)
		// Scaffold/init plan steps create incomplete skeletons; defer build/test gates.
		if task != nil {
			stepTitle, stepDesc := resolveCodingPlanStepFocus(task.Title, task.Description, "")
			verificationStatus, verificationSummary = maybeRelaxScaffoldVerification(stepTitle, stepDesc, verificationStatus, verificationSummary)
		}
		unresolvedGuardrailViolations := unresolvedSubAgentGuardrailViolations(allGuardrailViolations, filterPostEditSubAgentCommands(allCommandsRun, audit.LastEditSeq))
		status, errMsg = applySubAgentExplorationOutcome(status, errMsg, explorationStatus, explorationSummary, len(existingFilesModified))
		status, errMsg = applySubAgentVerificationOutcome(status, errMsg, verificationStatus, verificationSummary)
		status, errMsg = applySubAgentGuardrailOutcome(status, errMsg, unresolvedGuardrailViolations)
		summary = appendSubAgentFileChangeSummary(summary, filesModified, filesCreated)
		cb.emitFileActivitySummaryEvent(filesRead, filesModified, filesCreated)
		if len(unresolvedGuardrailViolations) > 0 {
			summary = appendSubAgentGuardrailSummary(summary, unresolvedGuardrailViolations)
		}
		cb.emitGuardrailSummaryEvent(unresolvedGuardrailViolations)
		if explorationSummary != "" {
			summary = appendSubAgentExplorationSummary(summary, explorationStatus, explorationSummary)
		}
		cb.emitExplorationSummaryEvent(explorationStatus, explorationSummary, countSuccessfulSubAgentSearches(allSearchesRun))
		if len(allCommandsRun) > 0 {
			summary = appendSubAgentCommandSummary(summary, allCommandsRun)
		}
		if len(allDynamicToolsRun) > 0 {
			summary = appendSubAgentDynamicToolSummary(summary, allDynamicToolsRun)
		}
		cb.emitCommandSummaryEvent(allCommandsRun)
		if verificationSummary != "" {
			summary = appendSubAgentVerificationSummary(summary, verificationStatus, verificationSummary)
		}
		cb.emitVerificationSummaryEvent(verificationStatus, verificationSummary, countFreshSubAgentVerificationAttempts(allCommandsRun, audit.LastEditSeq))
		diffChecked, diffSummary = cb.ensureFinalGitDiff(allFilesModified, allFilesCreated)
		status, errMsg = applySubAgentDiffOutcome(status, errMsg, diffChecked, diffSummary, len(allFilesModified))
		if modelSummary == "" {
			summary = rebaseFallbackSubAgentTaskSummary(summary, status, task, result.Iterations, result.ToolCalls)
		}
		if diffSummary != "" {
			summary = appendSubAgentDiffSummary(summary, diffSummary)
		}
		qualityStatus, qualitySummary, qualityIssueCount = summarizeSubAgentQuality(explorationStatus, verificationStatus, diffChecked, allFilesModified, allFilesCreated, allCommandsRun, audit.LastEditSeq, allGuardrailViolations, allDynamicToolsRun)
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentNoChangeEvidence(allFilesModified, allFilesCreated, allFilesRead, allSearchesRun, allCommandsRun, allDynamicToolsRun))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentCreatedFileContextEvidence(allFilesCreated, allFilesRead, allSearchesRun, allDynamicToolsRun))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentAcceptanceCriteriaEvidence(task, modelSummary, allFilesModified, allFilesCreated))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentScopeEvidence(task, modelSummary, allFilesModified, allFilesCreated))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentChangedFileSummaryEvidence(modelSummary, allFilesModified, allFilesCreated))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentRiskSummaryEvidence(modelSummary, allFilesModified, allFilesCreated))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentVerificationCommandSummaryEvidence(modelSummary, allFilesModified, allFilesCreated, allCommandsRun, audit.LastEditSeq))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentClaimedVerificationEvidence(modelSummary, allCommandsRun))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeSubAgentClaimedVerificationFailureEvidence(modelSummary, allCommandsRun))
		qualityStatus, qualitySummary, qualityIssueCount = appendSubAgentQualityFailure(qualityStatus, qualitySummary, qualityIssueCount, summarizeLocalizationQuality(task.Title+"\n"+task.Description, existingFilesModified, cb.localization.snapshot(), allSearchesRun))
		status, errMsg = applySubAgentQualityOutcome(status, errMsg, qualityStatus, qualitySummary, qualityIssueCount)
	}
	if modelSummary == "" {
		summary = rebaseFallbackSubAgentTaskSummary(summary, status, task, result.Iterations, result.ToolCalls)
	}
	summary = appendSubAgentQualityReportSummary(summary, &CodingSubAgentResult{QualityStatus: qualityStatus, QualitySummary: qualitySummary, QualityIssueCount: qualityIssueCount})
	summary = appendCodingAgentTodoTurnNote(summary, cb.todos.snapshot())
	cb.emitDiffCheckEvent(diffChecked, diffSummary, len(allFilesModified))
	cb.emitQualitySummaryEventWithAudit(qualityStatus, qualitySummary, qualityIssueCount)
	cb.emitDiffSummaryEvent(filesModified, filesCreated, diffSummary)

	log.Printf("[coding-subagent] task T%d finished: status=%s iterations=%d tools=%d err=%q",
		taskDisplayNumber(task), status, result.Iterations, result.ToolCalls, errMsg)
	if s.onProgress != nil {
		event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, task, taskTitle, "")
		event.Detail = string(status)
		emitCodingAgentEvent(s.onProgress, event)
	}

	inTok, outTok, cost := codingLoopUsageFields(result.Usage)
	return &CodingSubAgentResult{
		Status:              status,
		Summary:             summary,
		Error:               errMsg,
		Iterations:          result.Iterations,
		ToolCalls:           result.ToolCalls,
		InputTokens:         inTok,
		OutputTokens:        outTok,
		EstCostRMB:          cost,
		RouteModel:          result.Route.Model,
		RouteSource:         result.Route.Source,
		RouteTask:           result.Route.TaskType,
		RouteReason:         result.Route.Reason,
		FilesModified:       filesModified,
		FilesCreated:        filesCreated,
		FilesRead:           filesRead,
		GitDiffChecked:      diffChecked,
		GitDiffSummary:      diffSummary,
		DiffStat:            cb.getDiffStat(),
		CommandsRun:         commandsRun,
		SearchesRun:         searchesRun,
		GuardrailViolations: guardrailViolations,
		DynamicToolsRun:     dynamicToolsRun,
		ExplorationStatus:   explorationStatus,
		ExplorationSummary:  explorationSummary,
		VerificationStatus:  verificationStatus,
		VerificationSummary: verificationSummary,
		QualityStatus:       qualityStatus,
		QualitySummary:      qualitySummary,
		QualityIssueCount:   qualityIssueCount,
		Localization:        cb.localization.snapshot(),
	}
}

// ---------------------------------------------------------------------------
// codingSubAgentCallbacks implements agent.LoopCallbacks with a minimal
// coding-only configuration.
// ---------------------------------------------------------------------------

type codingSubAgentAudit struct {
	AllFilesModified        []string
	AllFilesCreated         []string
	AllFilesRead            []string
	AllCommandsRun          []CodingSubAgentCommandResult
	AllSearchesRun          []CodingSubAgentSearchResult
	AllGuardrailViolations  []CodingSubAgentGuardrailViolation
	AllDynamicToolsRun      []CodingSubAgentDynamicToolResult
	LastEditSeq             uint64
	FilesModified           []string
	FilesCreated            []string
	FilesRead               []string
	CommandsRun             []CodingSubAgentCommandResult
	SearchesRun             []CodingSubAgentSearchResult
	GuardrailViolations     []CodingSubAgentGuardrailViolation
	DynamicToolsRun         []CodingSubAgentDynamicToolResult
	ExploredBeforeFirstEdit bool
}

func collectSubAgentAudit(cb *codingSubAgentCallbacks) codingSubAgentAudit {
	if cb == nil {
		return codingSubAgentAudit{}
	}
	audit := codingSubAgentAudit{
		AllFilesModified:        cb.getFilesModified(),
		AllFilesCreated:         cb.getFilesCreated(),
		AllFilesRead:            cb.getFilesRead(),
		AllCommandsRun:          cb.getCommandsRun(),
		AllSearchesRun:          cb.getSearchesRun(),
		AllGuardrailViolations:  cb.getGuardrailViolations(),
		AllDynamicToolsRun:      cb.getDynamicToolsRun(),
		LastEditSeq:             cb.lastEditSequence(),
		ExploredBeforeFirstEdit: cb.exploredBeforeFirstEdit(),
	}
	audit.FilesModified = limitSubAgentStringSlice(audit.AllFilesModified, codingSubAgentResultFilesMax)
	audit.FilesCreated = limitSubAgentStringSlice(audit.AllFilesCreated, codingSubAgentResultFilesMax)
	audit.FilesRead = limitSubAgentStringSlice(audit.AllFilesRead, codingSubAgentResultFilesMax)
	audit.CommandsRun = limitSubAgentCommandResults(audit.AllCommandsRun, codingSubAgentResultAuditMax)
	audit.SearchesRun = limitSubAgentSearchResults(audit.AllSearchesRun, codingSubAgentResultAuditMax)
	audit.GuardrailViolations = limitSubAgentGuardrailViolations(audit.AllGuardrailViolations, codingSubAgentResultAuditMax)
	audit.DynamicToolsRun = limitSubAgentDynamicToolResults(audit.AllDynamicToolsRun, codingSubAgentResultAuditMax)
	return audit
}

type codingSubAgentCallbacks struct {
	subagent    *CodingSubAgent
	task        *TaskItem
	reqCtx      string
	designCtx   string
	prevOutputs []string

	// cachedSystemPrompt is built once per task to avoid repeated knowledge and
	// dynamic-tool prompt assembly on every LLM turn.
	cachedSystemPrompt string

	// cachedTools is built once on first call to BuildTools.
	cachedTools []map[string]interface{}

	// matchedSkills holds skills selected for this task via BM25 matching.
	matchedSkills         []codingSubAgentSkillMatch
	matchedSkillsSelected bool

	// matchedMCPTools holds MCP tools selected for this task.
	matchedMCPTools         []codingSubAgentMCPToolMatch
	matchedMCPToolsSelected bool

	// cachedDynamicSelectionText is the stable task text used to select dynamic
	// skills and MCP tools once per task.
	cachedDynamicSelectionText string
	dynamicSelectionTextBuilt  bool

	// filesModified tracks files written/edited during execution.
	mu             sync.Mutex
	filesModified  map[string]bool
	filesCreated   map[string]bool
	filesRead      map[string]bool
	fileSnapshots  map[string]codingFileSnapshot
	gitDiffChecked bool
	lastGitDiff    string
	diffStat       *SubAgentDiffStat
	commandsRun    []CodingSubAgentCommandResult
	searchesRun    []CodingSubAgentSearchResult
	guardrails     []CodingSubAgentGuardrailViolation
	dynamicTools   []CodingSubAgentDynamicToolResult
	localization   codingSubAgentLocalizationState
	eventSeq       uint64
	firstEditSeq   uint64
	lastEditSeq    uint64
	firstReadSeq   uint64
	firstSearchSeq uint64
	lastDiffSeq    uint64

	// Agent-internal Claude Code / Codex-style step checklist for this turn.
	todos codingAgentTodoState
}

type codingFileSnapshot struct {
	Size int64
	Hash string
}

func (c *codingSubAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	cfg := c.subagent.cfg
	// Codex-inspired: SubAgent system prompt never changes across iterations.
	// Enable prompt caching so providers (Anthropic, DeepSeek) can cache the
	// system prompt's KV state, reducing latency and cost by ~90% for iters 2-80.
	cfg.EnablePromptCache = true
	return cfg
}

// RouteTurn forces the reasoning path for coding subagent loops when a
// model router / aux config is available on the host app.
func (c *codingSubAgentCallbacks) RouteTurn(userText string) (corelib.MaclawLLMConfig, agent.RouteDecision, bool) {
	cfg := c.GetLLMConfig()
	decision := agent.RouteDecision{
		TaskType: string(llm.TaskReasoning),
		Model:    cfg.Model,
		Provider: cfg.ProviderName,
		Source:   "primary",
		Reason:   "coding subagent",
		Applied:  true,
	}
	if c == nil || c.subagent == nil || c.subagent.handler == nil {
		return cfg, decision, true
	}
	h := c.subagent.handler
	if h.app == nil || h.app.ohModules.modelRouter == nil {
		return cfg, decision, true
	}
	routed := h.routeCodingLLMConfig(llm.TaskReasoning, cfg)
	if routed.Model != "" {
		cfg = routed
		cfg.EnablePromptCache = true
		decision.Model = cfg.Model
		decision.Provider = cfg.ProviderName
		if h.app.ohModules.modelRouter.HasRoute(llm.TaskReasoning) {
			decision.Source = "route"
			decision.Reason = "coding subagent → reasoning route"
		}
	}
	return cfg, decision, true
}

func (c *codingSubAgentCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	var loopCtx *LoopContext
	var executionCtx context.Context
	if c != nil && c.subagent != nil {
		loopCtx = c.subagent.loopCtx
		executionCtx = c.subagent.executionCtx
	}
	return codingLoopLLMRequestContext(executionCtx, loopCtx, "coding-subagent", iteration)
}

// codingLoopLLMRequestContext builds a per-round LLM context with cancel linkage,
// request tracing, and scheduler lease — shared by local and remote coding SubAgents.
func codingLoopLLMRequestContext(executionCtx context.Context, loopCtx *LoopContext, caller string, iteration int) (context.Context, func(error), error) {
	baseCtx := executionCtx
	baseCancel := func() {}
	trace := llm.RequestTrace{Caller: caller, Iteration: iteration}
	if loopCtx != nil {
		if loopCtx.IsCancelled() || (baseCtx != nil && baseCtx.Err() != nil) {
			return nil, nil, fmt.Errorf("cancelled")
		}
		if baseCtx == nil {
			baseCtx, baseCancel = loopCtx.Context()
		}
		trace.OwnerID = strings.TrimSpace(loopCtx.Runtime.PolicyOwnerID)
		if trace.OwnerID == "" {
			trace.OwnerID = strings.TrimSpace(loopCtx.UserID)
		}
		trace.RequestID = loopCtx.Runtime.RequestID
		trace.LoopID = loopCtx.ID
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if err := baseCtx.Err(); err != nil {
		return nil, nil, err
	}
	ctx := llm.WithRequestTrace(baseCtx, trace)
	lease, scheduledTrace, err := acquireLLMSchedulerLease(ctx)
	if err != nil {
		baseCancel()
		return nil, nil, err
	}
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	return scheduledCtx, func(err error) {
		globalLLMScheduler.ObserveResult(scheduledTrace, err)
		scheduledCancel()
		lease.Release()
		baseCancel()
	}, nil
}

func (c *codingSubAgentCallbacks) toolContext() (context.Context, context.CancelFunc) {
	if c != nil && c.subagent != nil && c.subagent.executionCtx != nil {
		return c.subagent.executionCtx, func() {}
	}
	if c != nil && c.subagent != nil && c.subagent.loopCtx != nil {
		return c.subagent.loopCtx.Context()
	}
	return context.Background(), func() {}
}

func (c *codingSubAgentCallbacks) GetMaxIterations() int {
	if c != nil && c.subagent != nil {
		if c.subagent.loopCtx != nil && c.subagent.loopCtx.MaxIterations() > 0 {
			return config.EffectiveMaxIterations(c.subagent.loopCtx.MaxIterations())
		}
		// Nested spawn roles use tighter budgets than the pure-coding root turn.
		if c.subagent.nestDepth > 0 && c.subagent.role != "" && c.subagent.role != codingRoleWorker {
			return codingSpawnRoleMaxIterations(c.subagent.role)
		}
		if c.subagent.nestDepth > 0 {
			return codingSpawnRoleMaxIterations(codingRoleWorker)
		}
		if c.subagent.isFullEnvironment() {
			return codingSubAgentFullEnvMaxIterations
		}
	}
	// Per-task hard cap: SubAgent executes a single focused task, not an
	// open-ended conversation. 80 iterations is generous for any single task
	// (typical tasks complete in 10-30 iterations). Without this cap the
	// fallback is MaxAgentIterationsCap=300, which allows the SubAgent to
	// run for 20+ minutes if it gets stuck in a non-repeating failure loop
	// (e.g. repeatedly editing a script with different content then running
	// it — each iteration has unique args so drift detection won't trigger).
	return codingSubAgentPerTaskMaxIterations
}

func (c *codingSubAgentCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	if c.cachedSystemPrompt != "" {
		c.logCacheEvent("system_prompt", "hit", "prompt_chars", len(c.cachedSystemPrompt))
		return c.cachedSystemPrompt
	}
	fullEnv := c != nil && c.subagent != nil && c.subagent.isFullEnvironment()
	operational := c != nil && codingTaskLooksOperational(c.task)
	prompt := buildCodingSubAgentSystemPrompt(c.task, c.subagent.projectPath, c.reqCtx, c.designCtx, c.prevOutputs)
	inspectionRole := c != nil && c.subagent != nil && c.subagent.nestDepth > 0 &&
		(c.subagent.role == codingRoleExplorer || c.subagent.role == codingRoleReviewer)
	if c != nil && c.subagent != nil && c.subagent.nestDepth > 0 {
		role := c.subagent.role
		if role == "" {
			role = codingRoleWorker
		}
		prompt = "## Nested coding subagent\n" + codingSpawnRolePromptHint(role) + "\n\n" + prompt
		if inspectionRole {
			// Replace write-oriented base prompt with a lean inspection brief.
			prompt = "## Nested coding subagent\n" + codingSpawnRolePromptHint(role) + "\n\n" +
				buildLocalInspectionRoleSystemPrompt(c.subagent.projectPath, role, c.reqCtx)
		}
	}
	if fullEnv && !inspectionRole && !operational {
		// Nested workers get full-env tools/skills but must not be told to spawn again.
		if c.subagent != nil && c.subagent.nestDepth > 0 {
			prompt = buildNestedFullCodingEnvironmentPromptPreamble() + prompt
		} else {
			prompt = buildFullCodingEnvironmentPromptPreamble() + prompt
		}
		if probe := probeCodingWorkspace(c.subagent.projectPath); probe != "" {
			prompt += "\n## 工作区概览（进门自动探查）\n" + probe + "\n"
		}
	}
	if inspectionRole && c.subagent != nil {
		if probe := probeCodingWorkspace(c.subagent.projectPath); probe != "" {
			prompt += "\n## 工作区概览（进门自动探查）\n" + probe + "\n"
		}
	}

	// Inject knowledge from coding experience store + general knowledge store.
	if knowledgeSections := c.buildKnowledgePromptSections(); knowledgeSections != "" {
		prompt += knowledgeSections
	}

	// Skills/MCP for root + nested workers only (inspection roles stay lean).
	if !inspectionRole && !operational {
		// Eagerly select relevant skills so both BuildSystemPrompt and BuildTools
		// have access to the same matchedSkills list.
		c.ensureMatchedSkillsSelected()

		if section := buildCodingSubAgentSkillSection(c.matchedSkills); section != "" {
			prompt += section
		}

		if section := c.buildCodingSubAgentMCPSection(); section != "" {
			prompt += section
		}
	}

	c.cachedSystemPrompt = prompt
	c.logCacheEvent("system_prompt", "build", "prompt_chars", len(prompt), "full_env", fullEnv, "operational", operational)
	return prompt
}

func (c *codingSubAgentCallbacks) ensureMatchedSkillsSelected() {
	if c == nil || c.matchedSkillsSelected {
		return
	}
	if len(c.matchedSkills) > 0 {
		c.matchedSkillsSelected = true
		return
	}
	c.matchedSkills = c.selectRelevantSkillsForTask(c.dynamicSelectionText())
	c.matchedSkillsSelected = true
}

func (c *codingSubAgentCallbacks) ensureMatchedMCPToolsSelected() {
	if c == nil || c.matchedMCPToolsSelected {
		return
	}
	if len(c.matchedMCPTools) > 0 {
		c.matchedMCPToolsSelected = true
		return
	}
	c.matchedMCPTools = c.selectRelevantMCPToolsForTask(c.dynamicSelectionText())
	c.matchedMCPToolsSelected = true
}

func (c *codingSubAgentCallbacks) dynamicSelectionText() string {
	if c == nil {
		return ""
	}
	if c.dynamicSelectionTextBuilt {
		return c.cachedDynamicSelectionText
	}
	c.cachedDynamicSelectionText = codingSubAgentDynamicSelectionTextWithContext(c.task, c.reqCtx, c.designCtx, c.prevOutputs)
	c.dynamicSelectionTextBuilt = true
	return c.cachedDynamicSelectionText
}

func (c *codingSubAgentCallbacks) BuildTools(userText string) []map[string]interface{} {
	if c.cachedTools == nil {
		tools := buildCodingToolDefinitionsFromRegistry(c.subagent.handler)
		tools = append(tools, buildCodeNavigationToolDefinition(), buildReportLocalizationToolDefinition())
		if c.task != nil && codingTaskLooksInquiry(c.task) {
			c.cachedTools = filterCodingInquiryTools(tools)
			c.logCacheEvent("tools", "build-read-only", "tool_count", len(c.cachedTools))
			return cloneCodingSubAgentToolDefinitions(c.cachedTools)
		}
		if c.task != nil && codingTaskLooksOperational(c.task) {
			c.cachedTools = filterCodingOperationalTools(tools)
			c.logCacheEvent("tools", "build-operational", "tool_count", len(c.cachedTools))
			return cloneCodingSubAgentToolDefinitions(c.cachedTools)
		}

		// Full workbench extras (web research / clock) — always on for full env.
		if c.subagent != nil && c.subagent.isFullEnvironment() {
			tools = append(tools, buildCodingFullEnvExtraToolDefinitions()...)
		}
		// Nested explorer/reviewer still need research helpers even without full env.
		if c.subagent != nil && !c.subagent.isFullEnvironment() && c.subagent.nestDepth > 0 {
			tools = append(tools, buildCodingFullEnvExtraToolDefinitions()...)
		}
		// A lean local CodingSubAgent normally omits network helpers. Bug tasks keep
		// the research pair available even when the original report looks purely
		// local: code navigation may discover a third-party/version-sensitive cause
		// after this tool list has been cached. Without this, the localization gate
		// could require research that the current agent can no longer perform.
		if c.subagent != nil && !c.subagent.isFullEnvironment() && c.subagent.nestDepth == 0 &&
			(codingTaskNeedsLocalization(c.dynamicSelectionText()) || codingTaskNeedsExternalResearch(c.dynamicSelectionText())) {
			tools = append(tools, buildCodingExternalResearchToolDefinitions()...)
		}

		inspectionRole := c.subagent != nil && c.subagent.nestDepth > 0 &&
			(c.subagent.role == codingRoleExplorer || c.subagent.role == codingRoleReviewer)
		// /goal long-running objective tool — root pure-coding workbench only.
		// Nested explorers/reviewers stay lean; workers inherit via root continuation.
		if c.subagent != nil && c.subagent.isFullEnvironment() && c.subagent.nestDepth == 0 {
			tools = append(tools, buildCodingGoalToolDefinition())
		}
		// Skills/MCP are for root + nested workers; keep inspection agents lean.
		if !inspectionRole {
			// Append manage_skill if relevant skills were found for this task.
			// matchedSkills may already be populated by BuildSystemPrompt.
			c.ensureMatchedSkillsSelected()
			if len(c.matchedSkills) > 0 {
				tools = append(tools, buildManageSkillToolDefinition())
			}

			// Append call_mcp_tool if relevant MCP tools were found for this task.
			c.ensureMatchedMCPToolsSelected()
			if len(c.matchedMCPTools) > 0 {
				tools = append(tools, buildCallMCPToolDefinition())
			}
		}

		// Append knowledge search tools (read-only) when stores are available.
		if c.subagent.codingKB != nil {
			tools = append(tools, codingKnowledgeSearchToolDef())
		}
		if c.subagent.generalKB != nil {
			tools = append(tools, knowledgeSearchToolDef(), knowledgeImageSearchToolDef())
		}

		// Codex-style nested subagents (pure coding workbench root only).
		if c.subagent != nil && c.subagent.canSpawnCodingAgent() {
			tools = append(tools, buildSpawnCodingAgentToolDefinition())
		}

		// In-agent requirement breakdown + step checklist (workers only).
		if !inspectionRole {
			tools = append(tools, buildCodingAgentTodoToolDefinition())
		}

		// Role-based tool surface for nested explorer/reviewer agents.
		if c.subagent != nil {
			tools = filterCodingToolsForRole(tools, c.subagent)
		}

		c.cachedTools = tools
		c.logCacheEvent("tools", "build", "tool_count", len(tools))
	} else {
		c.logCacheEvent("tools", "hit", "tool_count", len(c.cachedTools))
	}
	return cloneCodingSubAgentToolDefinitions(c.cachedTools)
}

func filterCodingToolsForRole(tools []map[string]interface{}, sa *CodingSubAgent) []map[string]interface{} {
	if sa == nil || len(tools) == 0 {
		return tools
	}
	role := sa.role
	if role == "" || role == codingRoleWorker {
		if sa.canSpawnCodingAgent() {
			return tools
		}
		out := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			fn, _ := t["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			if name == codingSubAgentSpawnToolName {
				continue
			}
			out = append(out, t)
		}
		return out
	}
	return (codingagent.ToolPolicy{Role: role, Allowed: codingSubAgentSpawnRoleTools[role], Normalize: canonicalCodingSubAgentToolName}).FilterToolDefinitions(tools)
}

func cloneCodingSubAgentToolDefinitions(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		out[i], _ = cloneCodingSubAgentToolValue(tool).(map[string]interface{})
	}
	return out
}

func cloneCodingSubAgentToolValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[key] = cloneCodingSubAgentToolValue(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = cloneCodingSubAgentToolValue(item)
		}
		return out
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(v))
		for i, item := range v {
			out[i], _ = cloneCodingSubAgentToolValue(item).(map[string]interface{})
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	case map[string][]string:
		out := make(map[string][]string, len(v))
		for key, items := range v {
			copied := make([]string, len(items))
			copy(copied, items)
			out[key] = copied
		}
		return out
	default:
		return v
	}
}

func (c *codingSubAgentCallbacks) logCacheEvent(cacheName, event string, kv ...interface{}) {
	if os.Getenv("MACLAW_DEBUG_CODING_SUBAGENT_CACHE") != "1" {
		return
	}
	taskIndex := 0
	skillCount := 0
	mcpToolCount := 0
	if c != nil && c.task != nil {
		taskIndex = c.task.Index
	}
	if c != nil {
		skillCount = len(c.matchedSkills)
		mcpToolCount = len(c.matchedMCPTools)
	}
	parts := []string{
		fmt.Sprintf("cache=%s", cacheName),
		fmt.Sprintf("event=%s", event),
		fmt.Sprintf("task_index=%d", taskIndex),
		fmt.Sprintf("skills=%d", skillCount),
		fmt.Sprintf("mcp_tools=%d", mcpToolCount),
	}
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, fmt.Sprintf("%v=%v", kv[i], kv[i+1]))
	}
	log.Printf("[coding-subagent-cache] %s", strings.Join(parts, " "))
}

func (c *codingSubAgentCallbacks) ExecuteTool(name, argsJSON string) string {
	return c.ExecuteToolStructured(name, argsJSON).Result
}

func (c *codingSubAgentCallbacks) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
	if c != nil && codingTaskLooksInquiry(c.task) && !isCodingInquiryTool(name) {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("tool %s is unavailable for a read-only repository inquiry", name), Outcome: agent.ToolExecutionOutcomeError}
	}
	if c != nil && codingTaskLooksOperational(c.task) && !isCodingOperationalTool(name) {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("tool %s is unavailable for a run/build/demo request", name), Outcome: agent.ToolExecutionOutcomeError}
	}
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

// ProjectToolResult implements agent.ToolResultProjector. RunLoop calls it only
// after hooks and diagnostics have observed the complete execution result.
func (c *codingSubAgentCallbacks) ProjectToolResult(name string, result agent.ToolExecutionResult) string {
	if result.Outcome != agent.ToolExecutionOutcomeOK {
		return result.Result
	}
	toolName := canonicalCodingSubAgentToolName(name)
	preview := truncateToolResultForSubAgent(toolName, result.Result)
	sessionKey := ""
	if c != nil && c.subagent != nil && c.subagent.handler != nil {
		sessionKey = c.subagent.handler.currentRuntimeOrLegacyPolicyOwnerID()
	}
	return projectToolResultHandle(toolName, sessionKey, result.Result, preview, maxToolResultLen)
}

func (c *codingSubAgentCallbacks) executeToolWithOutcome(name, argsJSON string) (toolResult codingToolExecutionResult) {
	name = canonicalCodingSubAgentToolName(name)
	dynamicToolInitialCount := -1
	if c != nil && codingSubAgentDynamicToolNames[name] {
		dynamicToolInitialCount = c.dynamicToolResultCount()
	}
	if c.ShouldStop() {
		toolResult = codingToolExecutionResult{Text: "coding subagent cancelled before tool execution", Outcome: codingToolOutcomeFailed}
		logCodingSubAgentOperationFailure(c, name, argsJSON, toolResult, 0)
		return toolResult
	}
	toolStartedAt := time.Now()
	c.emitToolStartedEvent(name)
	defer func() {
		duration := time.Since(toolStartedAt)
		c.emitToolFinishedEvent(name, argsJSON, toolResult.Text, toolResult.Outcome, duration)
		if toolResult.Outcome != codingToolOutcomeSuccess {
			logCodingSubAgentOperationFailure(c, name, argsJSON, toolResult, duration)
			if !toolResult.SkipRejectedDynamicToolRecord {
				c.trackRejectedDynamicToolResult(name, argsJSON, toolResult.Text, dynamicToolInitialCount)
			}
		}
	}()

	if !codingSubAgentToolNames[name] && !codingSubAgentDynamicToolNames[name] {
		return codingToolExecutionResult{Text: fmt.Sprintf("unknown tool: %s (coding SubAgent supports %v)", name, codingSubAgentToolNameList()), Outcome: codingToolOutcomeFailed}
	}

	if result, rejected := rejectInvalidCodingSubAgentToolArguments(name, argsJSON); rejected {
		return result
	}
	argsJSON = normalizeCodingSubAgentToolArgumentsForTool(name, argsJSON)

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return invalidCodingSubAgentToolArgumentsResult(name, argsJSON, err)
	}
	if result, rejected := rejectInvalidCodingSubAgentToolArgumentTypes(name, args); rejected {
		return result
	}

	if c == nil || c.subagent == nil {
		return codingToolExecutionResult{Text: "coding subagent is unavailable", Outcome: codingToolOutcomeFailed}
	}
	// Nested role policy (explorer/reviewer) is enforced at execution time too,
	// including write-capable argument variants of otherwise observational tools.
	if allowed, reason := c.subagent.toolCallAllowedForRole(name, args); !allowed {
		return codingToolExecutionResult{
			Text:    reason,
			Outcome: codingToolOutcomeBlocked,
		}
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
		ctx, cancel := c.toolContext()
		defer cancel()
		result := agent.ToolGlobDetailedCtx(ctx, searchArgs)
		c.trackSearchResult("Glob", searchArgs, result.Text, searchOutcomeCountsAsExploration(result.Outcome))
		return codingToolExecutionResult{Text: result.Text, Outcome: codingOutcomeFromSearchOutcome(result.Outcome)}
	case "ripgrep":
		searchArgs := c.withDefaultProjectPath(args)
		if p, _ := searchArgs["path"].(string); p != "" {
			if msg := c.requireProjectReadScope(p, "ripgrep"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("ripgrep", searchArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		ctx, cancel := c.toolContext()
		defer cancel()
		result := agent.ToolRipgrepDetailedCtx(ctx, searchArgs)
		c.trackSearchResult("ripgrep", searchArgs, result.Text, searchOutcomeCountsAsExploration(result.Outcome))
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
			// Emit code:file_update so the code preview panel shows the file being read.
			c.emitReadFilePreview(p)
		}
		return result
	case "write_file":
		if h == nil {
			return codingToolExecutionResult{Text: c.rejectToolCall("write_file", args, "coding subagent host tool handler is unavailable"), Outcome: codingToolOutcomeBlocked}
		}
		fileArgs := c.withProjectRelativePath(args, false)
		if p, _ := fileArgs["path"].(string); p != "" {
			if msg := c.requireLocalizationBeforeExistingBugEdit(p, !codingFileExists(p)); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("write_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			if msg := c.requireProjectWriteScope(p); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("write_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			created := !codingFileExists(p)
			if msg := c.requireReadBeforeWriteExisting(p, fileArgs); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("write_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			result := executeCodingWriteFile(fileArgs)
			if result.Outcome == codingToolOutcomeSuccess {
				c.trackFile(p)
				if created {
					c.trackCreatedFile(p)
				}
				// refreshFileSnapshot also sets filesRead[key]=true, which allows
				// subsequent write_file to the same file without read_file first.
				c.refreshFileSnapshot(p)
				// Surface successful file changes immediately. Waiting until the whole
				// subagent task completes leaves the source preview blank while the
				// agent is still running its remaining checks.
				emitCodeFilePreviewForPath(h.app, c.codeSessionID(), c.projectPath(), c.previewRouteProjectPath(), p, created, true)
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
			if msg := c.requireLocalizationBeforeExistingBugEdit(p, false); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("edit_file", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
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
				emitCodeFilePreviewForPath(h.app, c.codeSessionID(), c.projectPath(), c.previewRouteProjectPath(), p, false, true)
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
			if msg := c.requireLocalizationBeforeExistingBugEdit(p, false); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("edit_lines", fileArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
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
				emitCodeFilePreviewForPath(h.app, c.codeSessionID(), c.projectPath(), c.previewRouteProjectPath(), p, false, true)
			}
			return result
		}
		return executeCodingEditLines(fileArgs)
	case "bash":
		bashArgs := c.withDefaultWorkingDir(args)
		if command, _ := bashArgs["command"].(string); command != "" {
			if codingTaskLooksInquiry(c.task) {
				if msg := rejectCodingInquiryShellCommand(command); msg != "" {
					c.trackCommandResult(bashArgs, msg, false)
					return codingToolExecutionResult{Text: c.rejectToolCall("bash", bashArgs, msg), Outcome: codingToolOutcomeBlocked}
				}
			}
			if codingTaskLooksOperational(c.task) {
				if msg := rejectCodingOperationalShellCommand(command); msg != "" {
					c.trackCommandResult(bashArgs, msg, false)
					return codingToolExecutionResult{Text: c.rejectToolCall("bash", bashArgs, msg), Outcome: codingToolOutcomeBlocked}
				}
			}
			// Hard block: never offer high-risk approval for silenced git self-checks.
			if msg := rejectSilencedGitSelfCheckCommand(command); msg != "" {
				c.trackCommandResult(bashArgs, msg, false)
				return codingToolExecutionResult{Text: c.rejectToolCall("bash", bashArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
			if msg := rejectDisallowedCodingBashCommand(command); msg != "" {
				workingDir, _ := bashArgs["working_dir"].(string)
				if c.subagent != nil && c.subagent.scopeApproval != nil {
					msg = c.subagent.scopeApproval.checkHighRisk("bash", command, c.projectPath(), workingDir, msg)
				}
				if msg == "" {
					// The user approved this guarded command; continue to the executor.
				} else {
					c.trackCommandResult(bashArgs, msg, false)
					return codingToolExecutionResult{Text: c.rejectToolCall("bash", bashArgs, msg), Outcome: codingToolOutcomeBlocked}
				}
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
		ctx, cancel := c.toolContext()
		defer cancel()
		commandResult := executeCodingBashWithContext(ctx, bashArgs, nil)
		command, _ := bashArgs["command"].(string)
		c.trackCommandResult(bashArgs, commandResult.Text, commandResult.succeededForCommand(command))
		return commandResult.toolResultForCommand(command)
	case "list_directory":
		listArgs := c.withProjectRelativePath(args, true)
		if p, _ := listArgs["path"].(string); p != "" {
			if msg := c.requireProjectReadScope(p, "list_directory"); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("list_directory", listArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		result := executeCodingListDirectory(listArgs)
		c.trackSearchResult("list_directory", listArgs, result.Text, result.Outcome == codingToolOutcomeSuccess)
		return result
	case "git_diff":
		diffArgs := c.withProjectRelativePath(args, true)
		if p, _ := diffArgs["path"].(string); p != "" {
			if msg := c.requireProjectDiffScope(p); msg != "" {
				return codingToolExecutionResult{Text: c.rejectToolCall("git_diff", diffArgs, msg), Outcome: codingToolOutcomeBlocked}
			}
		}
		result := c.toolGitDiffResult(diffArgs)
		if result.Outcome == codingToolOutcomeSuccess {
			c.trackGitDiff(result.Text)
		}
		return result
	case codeNavigationToolName:
		result := c.executeLocalCodeNavigation(args)
		if result.Outcome == codingToolOutcomeSuccess {
			c.trackSearchResult(codeNavigationToolName, args, result.Text, true)
		}
		return result
	case reportLocalizationToolName:
		return c.executeReportLocalization(args)
	case "manage_skill":
		return c.executeManageSkill(args)
	case "call_mcp_tool":
		return c.executeCallMCPTool(args)
	case "coding_knowledge_search":
		return c.executeCodingKnowledgeSearch(argsJSON)
	case "knowledge_search":
		return c.executeKnowledgeSearch(argsJSON)
	case "knowledge_image_search":
		return c.executeKnowledgeImageSearch(argsJSON)
	case "web_search":
		if h == nil {
			text := "web_search unavailable: host handler missing"
			c.trackSearchResult("web_search", args, text, false)
			return codingToolExecutionResult{Text: text, Outcome: codingToolOutcomeFailed}
		}
		text := h.toolWebSearch(args)
		succeeded := !codingWebResearchResultLooksFailed(text)
		c.trackSearchResult("web_search", args, text, succeeded)
		return codingToolExecutionResult{Text: text, Outcome: codingOutcomeFromSuccess(succeeded)}
	case "web_fetch":
		if h == nil {
			text := "web_fetch unavailable: host handler missing"
			c.trackSearchResult("web_fetch", args, text, false)
			return codingToolExecutionResult{Text: text, Outcome: codingToolOutcomeFailed}
		}
		text := h.toolWebFetch(args)
		succeeded := !codingWebFetchResultLooksFailed(text)
		c.trackSearchResult("web_fetch", args, text, succeeded)
		return codingToolExecutionResult{Text: text, Outcome: codingOutcomeFromSuccess(succeeded)}
	case "download_file":
		if h == nil {
			return codingToolExecutionResult{Text: "download_file unavailable: host handler missing", Outcome: codingToolOutcomeFailed}
		}
		return codingToolExecutionResult{Text: h.toolDownloadFile(args), Outcome: codingToolOutcomeSuccess}
	case "current_datetime":
		return codingToolExecutionResult{Text: formatBtwCurrentDateTime(), Outcome: codingToolOutcomeSuccess}
	case "goal":
		return c.executeGoalTool(args)
	case codingSubAgentSpawnToolName:
		return c.executeSpawnCodingAgent(args)
	case codingAgentTodoToolName:
		return c.executeTodoWrite(argsJSON)
	default:
		return codingToolExecutionResult{Text: fmt.Sprintf("unknown tool: %s", name), Outcome: codingToolOutcomeFailed}
	}
}

func (c *codingSubAgentCallbacks) executeTodoWrite(argsJSON string) codingToolExecutionResult {
	if c == nil {
		return codingToolExecutionResult{Text: "todo_write unavailable", Outcome: codingToolOutcomeFailed}
	}
	var onProgress func(string)
	var userID string
	if c.subagent != nil {
		onProgress = c.subagent.onProgress
		if c.subagent.loopCtx != nil {
			userID = strings.TrimSpace(c.subagent.loopCtx.UserID)
		}
	}
	var handler *IMMessageHandler
	if c.subagent != nil {
		handler = c.subagent.handler
	}
	text, outcome := executeCodingAgentTodoWrite(&c.todos, argsJSON, wrapTodoProgressForOrchestratedPlan(handler, userID, onProgress), func(items []codingAgentTodoItem) {
		if handler != nil && userID != "" {
			publishCodingAgentTodosToUI(handler, userID, items)
		}
	})
	if outcome == codingToolOutcomeSuccess {
		text = annotateTodoChecklistForOrchestratedPlan(handler, userID, text)
	}
	return codingToolExecutionResult{Text: text, Outcome: outcome}
}

// executeGoalTool routes goal(action=...) through the host goal store using the
// pure-coding session owner (loopCtx.UserID / project tab key).
func (c *codingSubAgentCallbacks) executeGoalTool(args map[string]interface{}) codingToolExecutionResult {
	if c == nil || c.subagent == nil || c.subagent.handler == nil {
		return codingToolExecutionResult{Text: "goal unavailable: host handler missing", Outcome: codingToolOutcomeFailed}
	}
	userID := ""
	if c.subagent.loopCtx != nil {
		userID = strings.TrimSpace(c.subagent.loopCtx.UserID)
	}
	if userID == "" {
		// Refuse silent fallback to lastUserID — multi-tab pure coding would
		// otherwise attach goals to the wrong project session.
		return codingToolExecutionResult{
			Text:    "goal unavailable: coding session owner is missing (loopCtx.UserID empty)",
			Outcome: codingToolOutcomeFailed,
		}
	}
	text := c.subagent.handler.toolGoalForUser(userID, args)
	return codingToolExecutionResult{Text: text, Outcome: codingToolOutcomeSuccess}
}

func rejectInvalidCodingSubAgentToolArgumentTypes(name string, args map[string]interface{}) (codingToolExecutionResult, bool) {
	for _, field := range codingSubAgentRequiredArgumentFields(name) {
		value, ok := args[field]
		if !ok || value == nil {
			return missingCodingSubAgentRequiredArgumentResult(name, field), true
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" && !codingSubAgentRequiredArgumentAllowsEmptyString(name, field) {
			return missingCodingSubAgentRequiredArgumentResult(name, field), true
		}
	}
	for _, field := range codingSubAgentStringArgumentFields(name) {
		value, ok := args[field]
		if !ok || value == nil {
			continue
		}
		if _, ok := value.(string); ok {
			continue
		}
		return invalidCodingSubAgentArgumentTypeResult(name, field, "string", codingSubAgentArgumentTypeName(value), "a JSON string"), true
	}
	for _, field := range codingSubAgentNumberArgumentFields(name) {
		value, ok := args[field]
		if !ok || value == nil {
			continue
		}
		if codingSubAgentArgumentIsIntegerNumber(value) {
			if min, ok := codingSubAgentNumberArgumentMin(name, field); ok {
				actual, _ := codingSubAgentArgumentIntegerValue(value)
				if actual < min {
					return invalidCodingSubAgentArgumentRangeResult(name, field, min, actual), true
				}
			}
			continue
		}
		if codingSubAgentArgumentIsNumber(value) {
			return invalidCodingSubAgentArgumentTypeResult(name, field, "integer", "fractional number", "a JSON integer"), true
		}
		return invalidCodingSubAgentArgumentTypeResult(name, field, "integer", codingSubAgentArgumentTypeName(value), "a JSON integer"), true
	}
	for _, field := range codingSubAgentBoolArgumentFields(name) {
		value, ok := args[field]
		if !ok || value == nil {
			continue
		}
		if _, ok := value.(bool); ok {
			continue
		}
		return invalidCodingSubAgentArgumentTypeResult(name, field, "boolean", codingSubAgentArgumentTypeName(value), "a JSON boolean"), true
	}
	for _, field := range codingSubAgentObjectArgumentFields(name) {
		value, ok := args[field]
		if !ok || value == nil {
			continue
		}
		if _, ok := value.(map[string]interface{}); ok {
			continue
		}
		return invalidCodingSubAgentArgumentTypeResult(name, field, "object", codingSubAgentArgumentTypeName(value), "a JSON object"), true
	}
	if result, rejected := rejectInvalidCodingSubAgentToolArgumentValues(name, args); rejected {
		return result, true
	}
	return codingToolExecutionResult{}, false
}

func invalidCodingSubAgentArgumentTypeResult(name, field, expected, got, jsonType string) codingToolExecutionResult {
	return codingToolExecutionResult{
		Text:    appendCodingSubAgentArgumentExample(fmt.Sprintf("Error: tool call %q has invalid argument type for %q: expected %s, got %s. The tool was not executed. Regenerate the same tool call with %q as %s.", name, field, expected, got, field, jsonType), name),
		Outcome: codingToolOutcomeFailed,
	}
}

func invalidCodingSubAgentArgumentRangeResult(name, field string, min, actual int64) codingToolExecutionResult {
	return codingToolExecutionResult{
		Text:    appendCodingSubAgentArgumentExample(fmt.Sprintf("Error: tool call %q has invalid argument value for %q: expected integer >= %d, got %d. The tool was not executed. Regenerate the same tool call with %q in the allowed range.", name, field, min, actual, field), name),
		Outcome: codingToolOutcomeFailed,
	}
}

func invalidCodingSubAgentArgumentAllowedValuesResult(name, field, actual string, allowed []string) codingToolExecutionResult {
	return codingToolExecutionResult{
		Text:    appendCodingSubAgentArgumentExample(fmt.Sprintf("Error: tool call %q has invalid argument value for %q: expected one of %s, got %q. The tool was not executed. Regenerate the same tool call with %q set to an allowed value.", name, field, strings.Join(allowed, "/"), actual, field), name),
		Outcome: codingToolOutcomeFailed,
	}
}

func invalidCodingSubAgentArgumentOrderResult(name, field, minField string, actual, min int64) codingToolExecutionResult {
	return codingToolExecutionResult{
		Text:    appendCodingSubAgentArgumentExample(fmt.Sprintf("Error: tool call %q has invalid argument value for %q: expected integer >= %q (%d), got %d. The tool was not executed. Regenerate the same tool call with a valid line range.", name, field, minField, min, actual), name),
		Outcome: codingToolOutcomeFailed,
	}
}

func missingCodingSubAgentRequiredArgumentResult(name, field string) codingToolExecutionResult {
	return codingToolExecutionResult{
		Text:    appendCodingSubAgentArgumentExample(fmt.Sprintf("Error: tool call %q is missing required argument %q. The tool was not executed. Regenerate the same tool call with %q set.", name, field, field), name),
		Outcome: codingToolOutcomeFailed,
	}
}

func codingSubAgentRequiredArgumentFields(name string) []string {
	switch canonicalCodingSubAgentToolName(name) {
	case "Glob", "ripgrep":
		return []string{"pattern"}
	case "read_file":
		return []string{"path"}
	case "write_file":
		return []string{"path", "content"}
	case "edit_file":
		return []string{"path", "old_string", "new_string"}
	case "edit_lines":
		return []string{"path", "operation", "start_line"}
	case "bash":
		return []string{"command"}
	case "manage_skill":
		return []string{"action"}
	case "call_mcp_tool":
		return []string{"server_id", "tool_name"}
	case "coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return []string{"query"}
	default:
		return nil
	}
}

func codingSubAgentRequiredArgumentAllowsEmptyString(name, field string) bool {
	switch canonicalCodingSubAgentToolName(name) {
	case "write_file":
		return field == "content"
	case "edit_file":
		return field == "new_string"
	case "edit_lines":
		return field == "content"
	}
	return false
}

func rejectInvalidCodingSubAgentToolArgumentValues(name string, args map[string]interface{}) (codingToolExecutionResult, bool) {
	switch canonicalCodingSubAgentToolName(name) {
	case "write_file":
		mode, _ := args["mode"].(string)
		mode = strings.TrimSpace(mode)
		switch mode {
		case "", "overwrite", "append":
			return codingToolExecutionResult{}, false
		default:
			return invalidCodingSubAgentArgumentAllowedValuesResult(name, "mode", mode, []string{"overwrite", "append"}), true
		}
	case "edit_lines":
		return rejectInvalidCodingSubAgentEditLinesArgumentValues(name, args)
	default:
		return codingToolExecutionResult{}, false
	}
}

func rejectInvalidCodingSubAgentEditLinesArgumentValues(name string, args map[string]interface{}) (codingToolExecutionResult, bool) {
	operation, _ := args["operation"].(string)
	switch operation {
	case "replace":
		if missingCodingSubAgentArgumentValue(args, "end_line") {
			return missingCodingSubAgentRequiredArgumentResult(name, "end_line"), true
		}
		if missingCodingSubAgentArgumentValue(args, "content") {
			return missingCodingSubAgentRequiredArgumentResult(name, "content"), true
		}
		if result, rejected := rejectInvalidCodingSubAgentEditLinesRange(name, args); rejected {
			return result, true
		}
	case "insert":
		if missingCodingSubAgentArgumentValue(args, "content") {
			return missingCodingSubAgentRequiredArgumentResult(name, "content"), true
		}
		if content, _ := args["content"].(string); strings.TrimSpace(content) == "" {
			return missingCodingSubAgentRequiredArgumentResult(name, "content"), true
		}
	case "delete":
		if missingCodingSubAgentArgumentValue(args, "end_line") {
			return missingCodingSubAgentRequiredArgumentResult(name, "end_line"), true
		}
		if result, rejected := rejectInvalidCodingSubAgentEditLinesRange(name, args); rejected {
			return result, true
		}
	default:
		return invalidCodingSubAgentArgumentAllowedValuesResult(name, "operation", operation, []string{"replace", "insert", "delete"}), true
	}
	return codingToolExecutionResult{}, false
}

func rejectInvalidCodingSubAgentEditLinesRange(name string, args map[string]interface{}) (codingToolExecutionResult, bool) {
	startLine, startOK := codingSubAgentArgumentIntegerValue(args["start_line"])
	if startOK && startLine < 1 {
		return invalidCodingSubAgentArgumentRangeResult(name, "start_line", 1, startLine), true
	}
	endLine, endOK := codingSubAgentArgumentIntegerValue(args["end_line"])
	if endOK && endLine < 1 {
		return invalidCodingSubAgentArgumentRangeResult(name, "end_line", 1, endLine), true
	}
	if startOK && endOK && endLine < startLine {
		return invalidCodingSubAgentArgumentOrderResult(name, "end_line", "start_line", endLine, startLine), true
	}
	return codingToolExecutionResult{}, false
}

func missingCodingSubAgentArgumentValue(args map[string]interface{}, field string) bool {
	value, ok := args[field]
	return !ok || value == nil
}

func codingSubAgentStringArgumentFields(name string) []string {
	switch canonicalCodingSubAgentToolName(name) {
	case "Glob", "ripgrep":
		return []string{"path", "pattern"}
	case "read_file", "list_directory", "git_diff":
		return []string{"path"}
	case "write_file":
		return []string{"path", "content", "mode"}
	case "edit_file":
		return []string{"path", "old_string", "new_string"}
	case "edit_lines":
		return []string{"path", "operation", "content"}
	case "bash":
		return []string{"command", "working_dir"}
	case "manage_skill":
		return []string{"action", "name"}
	case "call_mcp_tool":
		return []string{"server_id", "tool_name"}
	case "coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return []string{"query"}
	default:
		return nil
	}
}

func codingSubAgentNumberArgumentFields(name string) []string {
	switch canonicalCodingSubAgentToolName(name) {
	case "read_file":
		return []string{"lines", "start_line", "offset"}
	case "edit_lines":
		return []string{"start_line", "end_line"}
	case "bash":
		return []string{"timeout"}
	default:
		return nil
	}
}

func codingSubAgentNumberArgumentMin(name, field string) (int64, bool) {
	switch canonicalCodingSubAgentToolName(name) {
	case "read_file":
		switch field {
		case "lines", "start_line", "offset":
			return 1, true
		}
	case "edit_lines":
		switch field {
		case "start_line", "end_line":
			return 0, true
		}
	case "bash":
		if field == "timeout" {
			return 1, true
		}
	}
	return 0, false
}
func codingSubAgentBoolArgumentFields(name string) []string {
	switch canonicalCodingSubAgentToolName(name) {
	case "edit_file":
		return []string{"replace_all"}
	case "git_diff":
		return []string{"staged"}
	default:
		return nil
	}
}

func codingSubAgentObjectArgumentFields(name string) []string {
	switch canonicalCodingSubAgentToolName(name) {
	case "manage_skill":
		return []string{"args"}
	case "call_mcp_tool":
		return []string{"arguments"}
	default:
		return nil
	}
}

func codingSubAgentArgumentIsNumber(value interface{}) bool {
	switch value.(type) {
	case float64, float32, int, int64, json.Number:
		return true
	default:
		return false
	}
}

func codingSubAgentArgumentIsIntegerNumber(value interface{}) bool {
	switch v := value.(type) {
	case int, int64:
		return true
	case float64:
		return math.Trunc(v) == v
	case float32:
		return math.Trunc(float64(v)) == float64(v)
	case json.Number:
		_, err := v.Int64()
		return err == nil
	default:
		return false
	}
}

func codingSubAgentArgumentIntegerValue(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		if math.Trunc(v) == v {
			return int64(v), true
		}
	case float32:
		f := float64(v)
		if math.Trunc(f) == f {
			return int64(f), true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}
func codingSubAgentArgumentTypeName(value interface{}) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int64, json.Number:
		return "number"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}
func rejectInvalidCodingSubAgentToolArguments(name, argsJSON string) (codingToolExecutionResult, bool) {
	args := strings.TrimSpace(argsJSON)
	if args == "" || codingSubAgentToolArgumentsAreObject(args) {
		return codingToolExecutionResult{}, false
	}
	err := json.Unmarshal([]byte(args), &map[string]interface{}{})
	result := invalidCodingSubAgentToolArgumentsResult(name, args, err)
	log.Printf("[coding-subagent] rejected invalid tool arguments tool=%q args_len=%d hint=%q", strings.TrimSpace(name), len(args), codingSubAgentToolArgumentHint(err, args))
	return result, true
}

func codingSubAgentToolArgumentsAreObject(args string) bool {
	var parsed map[string]interface{}
	return json.Unmarshal([]byte(args), &parsed) == nil && parsed != nil
}

func invalidCodingSubAgentToolArgumentsResult(name, argsJSON string, err error) codingToolExecutionResult {
	args := strings.TrimSpace(argsJSON)
	hint := codingSubAgentToolArgumentHint(err, args)
	text := fmt.Sprintf("Error: tool call %q has invalid JSON object arguments. The tool was not executed. Regenerate the same tool call with complete valid JSON object arguments.", name)
	if err != nil {
		text += fmt.Sprintf(" Parse error: %s.", err.Error())
	}
	if hint != "" {
		text += " " + hint
	}
	text += fmt.Sprintf(" If the content is large, split it into smaller chunks: write_file uses mode=\"overwrite\" for the first chunk and mode=\"append\" for later chunks; edit_file/edit_lines content fields must stay under %d characters per call.", codingSubAgentInlineContentLimit)
	text = appendCodingSubAgentArgumentExample(text, name)
	return codingToolExecutionResult{Text: text, Outcome: codingToolOutcomeFailed}
}

func appendCodingSubAgentArgumentExample(text, name string) string {
	example := codingSubAgentToolArgumentExample(name)
	if example == "" {
		return text
	}
	text = strings.TrimSpace(text) + " Example valid arguments: " + example + "."
	if hint := codingSubAgentToolArgumentAliasHint(name); hint != "" {
		text += " " + hint
	}
	return text
}

func codingSubAgentToolArgumentExample(name string) string {
	switch canonicalCodingSubAgentToolName(name) {
	case "Glob":
		return `{"pattern":"**/*.go","path":"."}`
	case "ripgrep":
		return `{"pattern":"TODO","path":"."}`
	case "read_file":
		return `{"path":"src/main.go"}`
	case "write_file":
		return `{"path":"src/new_file.go","content":"package main\n"}`
	case "edit_file":
		return `{"path":"src/main.go","old_string":"old text","new_string":"new text"}`
	case "edit_lines":
		return `{"path":"src/main.go","operation":"replace","start_line":10,"end_line":12,"content":"replacement text"}`
	case "bash":
		return `{"command":"go test ./gui","timeout":120}`
	case "list_directory":
		return `{"path":"."}`
	case "git_diff":
		return `{"path":"."}`
	case "manage_skill":
		return `{"action":"run","name":"skill-name","args":{"input":"task-specific instructions"}}`
	case "call_mcp_tool":
		return `{"server_id":"server","tool_name":"tool","arguments":{}}`
	case "coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return `{"query":"search terms"}`
	default:
		return ""
	}
}

func codingSubAgentToolArgumentAliasHint(name string) string {
	switch canonicalCodingSubAgentToolName(name) {
	case "Glob":
		return `Accepted aliases: glob/query -> pattern; file/file_path/filepath/filename/target_path and dir/directory/root -> path.`
	case "ripgrep":
		return `Accepted aliases: regex/query -> pattern; file/file_path/filepath/filename/target_path and dir/directory/root -> path.`
	case "list_directory", "git_diff":
		return `Accepted aliases: file/file_path/filepath/filename/target_path and dir/directory/root -> path.`
	case "read_file", "write_file":
		return `Accepted aliases: file/file_path/filepath/filename/target_path -> path.`
	case "edit_file":
		return `Accepted aliases: file/file_path/filepath/filename/target_path -> path; old_content/find/search -> old_string; new_content/replace/replacement -> new_string.`
	case "edit_lines":
		return `Accepted aliases: file/file_path/filepath/filename/target_path -> path; action/op -> operation; start/startLine -> start_line; end/endLine -> end_line; add/update/remove-style operations are normalized.`
	case "bash":
		return `Accepted aliases: work_dir/cwd -> working_dir.`
	case "call_mcp_tool":
		return `Accepted aliases: server/server_name -> server_id; tool/name -> tool_name; args/params/input -> arguments.`
	case "coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return `Accepted aliases: text/question/keyword/keywords/term/terms/search_terms/q -> query.`
	default:
		return ""
	}
}

func codingSubAgentToolArgumentHint(err error, args string) string {
	hint := ""
	if err != nil {
		hint = classifyToolArgumentError(err, args).Hint()
	}
	return hint
}

func normalizeCodingSubAgentToolArguments(argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return "{}"
	}
	return argsJSON
}

func normalizeCodingSubAgentToolArgumentsForTool(name, argsJSON string) string {
	normalized := normalizeCodingSubAgentToolArguments(argsJSON)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &args); err != nil || args == nil {
		return normalized
	}
	if !applyCodingSubAgentToolArgumentAliases(name, args) {
		return normalized
	}
	data, err := json.Marshal(args)
	if err != nil {
		return normalized
	}
	return string(data)
}

func applyCodingSubAgentToolArgumentAliases(name string, args map[string]interface{}) bool {
	if len(args) == 0 {
		return false
	}
	changed := false
	switch canonicalCodingSubAgentToolName(name) {
	case "Glob":
		changed = applyCodingSubAgentToolArgumentAlias(args, "glob", "pattern") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "query", "pattern") || changed
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentDirectoryArgumentAliases(args) || changed
	case "ripgrep":
		changed = applyCodingSubAgentToolArgumentAlias(args, "regex", "pattern") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "query", "pattern") || changed
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentDirectoryArgumentAliases(args) || changed
	case "read_file", "write_file":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		if canonicalCodingSubAgentToolName(name) == "read_file" {
			changed = applyCodingSubAgentToolArgumentAlias(args, "limit", "lines") || changed
			changed = applyCodingSubAgentToolArgumentAlias(args, "num_lines", "lines") || changed
			changed = applyCodingSubAgentToolArgumentAlias(args, "line_count", "lines") || changed
			changed = applyCodingSubAgentToolArgumentAlias(args, "start", "start_line") || changed
			changed = applyCodingSubAgentToolArgumentAlias(args, "startLine", "start_line") || changed
		}
	case "list_directory", "git_diff":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentDirectoryArgumentAliases(args) || changed
	case "edit_lines":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "action", "operation") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "op", "operation") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "start", "start_line") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "startLine", "start_line") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "end", "end_line") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "endLine", "end_line") || changed
		changed = applyCodingSubAgentEditLinesOperationAliases(args) || changed
	case "bash":
		changed = applyCodingSubAgentToolArgumentAlias(args, "work_dir", "working_dir") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "cwd", "working_dir") || changed
	case "edit_file":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "old_content", "old_string") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "find", "old_string") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "search", "old_string") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "new_content", "new_string") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "replace", "new_string") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "replacement", "new_string") || changed
	case "call_mcp_tool":
		changed = applyCodingSubAgentToolArgumentAlias(args, "server", "server_id") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "server_name", "server_id") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "name", "tool_name") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "tool", "tool_name") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "args", "arguments") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "params", "arguments") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "input", "arguments") || changed
	case "coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		changed = applyCodingSubAgentQueryArgumentAliases(args) || changed
	}
	return changed
}

func applyCodingSubAgentQueryArgumentAliases(args map[string]interface{}) bool {
	changed := false
	for _, alias := range []string{"text", "question", "keyword", "keywords", "term", "terms", "search_terms", "q"} {
		changed = applyCodingSubAgentToolArgumentAlias(args, alias, "query") || changed
	}
	return changed
}

func applyCodingSubAgentEditLinesOperationAliases(args map[string]interface{}) bool {
	operation, ok := args["operation"].(string)
	if !ok {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(operation))
	switch normalized {
	case "insert", "replace", "delete":
		if operation == normalized {
			return false
		}
		args["operation"] = normalized
	case "add", "append", "insert_line", "insert_lines":
		args["operation"] = "insert"
	case "update", "modify", "replace_line", "replace_lines":
		args["operation"] = "replace"
	case "remove", "rm", "delete_line", "delete_lines":
		args["operation"] = "delete"
	default:
		return false
	}
	return true
}

func applyCodingSubAgentPathArgumentAliases(args map[string]interface{}) bool {
	changed := false
	changed = applyCodingSubAgentToolArgumentAlias(args, "file", "path") || changed
	changed = applyCodingSubAgentToolArgumentAlias(args, "file_path", "path") || changed
	changed = applyCodingSubAgentToolArgumentAlias(args, "filepath", "path") || changed
	changed = applyCodingSubAgentToolArgumentAlias(args, "filename", "path") || changed
	changed = applyCodingSubAgentToolArgumentAlias(args, "target_path", "path") || changed
	return changed
}

func applyCodingSubAgentDirectoryArgumentAliases(args map[string]interface{}) bool {
	changed := false
	changed = applyCodingSubAgentToolArgumentAlias(args, "dir", "path") || changed
	changed = applyCodingSubAgentToolArgumentAlias(args, "directory", "path") || changed
	changed = applyCodingSubAgentToolArgumentAlias(args, "root", "path") || changed
	return changed
}

func applyCodingSubAgentToolArgumentAlias(args map[string]interface{}, alias, canonical string) bool {
	value, ok := args[alias]
	if !ok {
		return false
	}
	if _, exists := args[canonical]; !exists {
		args[canonical] = value
	}
	delete(args, alias)
	return true
}

func codingOutcomeFromSuccess(success bool) codingToolOutcome {
	if success {
		return codingToolOutcomeSuccess
	}
	return codingToolOutcomeFailed
}

func logCodingSubAgentOperationFailure(c *codingSubAgentCallbacks, name, argsJSON string, result codingToolExecutionResult, duration time.Duration) {
	projectPath := ""
	taskIndex := -1
	taskTitle := ""
	if c != nil {
		if c.subagent != nil {
			projectPath = c.subagent.projectPath
		}
		if c.task != nil {
			taskIndex = c.task.Index
			taskTitle = compactSubAgentTaskTitle(c.task.Title)
		}
	}
	if codingSubAgentFailedToolLogIsDiagnostic(name, argsJSON, result) {
		return
	}
	log.Printf("[coding-subagent] %s: tool=%s outcome=%s duration=%s task=%d title=%q project=%q args=%s result=%s",
		"operation failed",
		name,
		result.Outcome,
		duration,
		taskIndex,
		taskTitle,
		projectPath,
		compactCodingSubAgentArgsLogText(argsJSON, 500),
		compactCodingSubAgentLogText(result.Text, 500),
	)
}

func codingSubAgentFailedToolLogIsDiagnostic(name, argsJSON string, result codingToolExecutionResult) bool {
	if canonicalCodingSubAgentToolName(name) != "bash" || result.Outcome != codingToolOutcomeFailed {
		return false
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return false
	}
	command, _ := args["command"].(string)
	return subAgentCommandFailureCanBeResolvedByLaterVerification(CodingSubAgentCommandResult{Command: command, Summary: result.Text})
}

func compactCodingSubAgentArgsLogText(argsJSON string, maxRunes int) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		if summary := redactInvalidCodingSubAgentArgsLogText(argsJSON); summary != "" {
			return summary
		}
		return compactCodingSubAgentLogText(argsJSON, maxRunes)
	}
	redactCodingSubAgentLogArgs(args)
	data, err := json.Marshal(args)
	if err != nil {
		return compactCodingSubAgentLogText(argsJSON, maxRunes)
	}
	return compactCodingSubAgentLogText(string(data), maxRunes)
}

func redactInvalidCodingSubAgentArgsLogText(argsJSON string) string {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	containsContent := false
	for _, key := range []string{`"content"`, `"old_string"`, `"new_string"`, `"replacement"`, `"text"`} {
		if strings.Contains(lower, key) {
			containsContent = true
			break
		}
	}
	containsSecret := false
	for _, key := range []string{`"password"`, `"token"`, `"api_key"`, `"apikey"`, `"secret"`, `"authorization"`} {
		if strings.Contains(lower, key) {
			containsSecret = true
			break
		}
	}
	if !containsContent && !containsSecret {
		return ""
	}
	prefixSource := trimmed
	if idx := firstCodingSubAgentSensitiveLogKeyIndex(lower); idx >= 0 {
		prefixSource = strings.TrimSpace(trimmed[:idx]) + "..."
	}
	prefix := compactCodingSubAgentLogText(prefixSource, 160)
	return fmt.Sprintf("[invalid JSON redacted bytes=%d content_field=%t secret_field=%t prefix=%q]", len(argsJSON), containsContent, containsSecret, prefix)
}

func firstCodingSubAgentSensitiveLogKeyIndex(lower string) int {
	first := -1
	for _, key := range []string{`"content"`, `"old_string"`, `"new_string"`, `"replacement"`, `"text"`, `"password"`, `"token"`, `"api_key"`, `"apikey"`, `"secret"`, `"authorization"`} {
		if idx := strings.Index(lower, key); idx >= 0 && (first < 0 || idx < first) {
			first = idx
		}
	}
	return first
}

func redactCodingSubAgentLogArgs(args map[string]interface{}) {
	for key, value := range args {
		switch lower := strings.ToLower(strings.TrimSpace(key)); {
		case isCodingSubAgentContentLogKey(lower):
			if s, ok := value.(string); ok {
				args[key] = fmt.Sprintf("[redacted %d runes]", len([]rune(s)))
			} else {
				args[key] = "[redacted]"
			}
		case isCodingSubAgentSecretLogKey(lower):
			args[key] = "[redacted]"
		default:
			redactCodingSubAgentLogValue(value)
		}
	}
}

func isCodingSubAgentContentLogKey(key string) bool {
	normalized := normalizeCodingSubAgentLogKey(key)
	switch normalized {
	case "content", "oldstring", "newstring", "replacement", "text":
		return true
	}
	return strings.Contains(normalized, "content") || strings.HasSuffix(normalized, "text")
}

func isCodingSubAgentSecretLogKey(key string) bool {
	normalized := normalizeCodingSubAgentLogKey(key)
	switch normalized {
	case "password", "passwd", "token", "accesstoken", "refreshtoken", "apikey", "secret", "authorization":
		return true
	}
	return strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "authorization")
}

func normalizeCodingSubAgentLogKey(key string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
}

func redactCodingSubAgentLogValue(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		redactCodingSubAgentLogArgs(typed)
	case []interface{}:
		for _, item := range typed {
			redactCodingSubAgentLogValue(item)
		}
	}
}

func hasOnlyDirectoryCreationShellMutation(command string) bool {
	fields := shellCommandFields(command)
	sawTarget := false
	commandPosition := true
	for i := 0; i < len(fields); i++ {
		token := strings.ToLower(normalizeShellCommandToken(fields[i]))
		if token == "" {
			continue
		}
		if isShellCommandStartMarker(token) {
			commandPosition = true
			continue
		}
		if !commandPosition {
			continue
		}
		if consumed, ok := shellCommandPrefixLength(fields[i:]); ok {
			i += consumed - 1
			commandPosition = true
			continue
		}
		switch commandNameBase(normalizeShellExecutableToken(token)) {
		case "mkdir", "md":
			args := commandSegmentFields(fields[i+1:])
			foundInSegment := false
			for j := 0; j < len(args); j++ {
				arg := normalizeShellCommandToken(args[j])
				if arg == "" {
					continue
				}
				if arg == "--" {
					for _, literal := range args[j+1:] {
						literal = normalizeShellCommandToken(literal)
						if literal == "" || shellDirectoryCreationTargetLooksDynamic(literal) {
							return false
						}
						foundInSegment = true
					}
					break
				}
				if shellMkdirOptionConsumesValue(arg) {
					j++
					continue
				}
				if strings.HasPrefix(arg, "-") {
					continue
				}
				if shellDirectoryCreationTargetLooksDynamic(arg) {
					return false
				}
				foundInSegment = true
			}
			if !foundInSegment {
				return false
			}
			sawTarget = true
		default:
			return false
		}
		commandPosition = false
	}
	return sawTarget
}

func shellDirectoryCreationTargetLooksDynamic(target string) bool {
	return strings.ContainsAny(target, "$*?`|&;<>") || strings.Contains(target, "${")
}

func shellMkdirOptionConsumesValue(option string) bool {
	option = strings.ToLower(strings.TrimSpace(option))
	switch option {
	case "-m", "--mode", "--context":
		return true
	}
	return false
}
func compactCodingSubAgentLogText(text string, maxRunes int) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return ""
	}
	text = redactCodingSubAgentFreeformLogText(text)
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func redactCodingSubAgentFreeformLogText(text string) string {
	for _, pattern := range codingSubAgentFreeformSecretPatterns {
		text = pattern.ReplaceAllString(text, `${1}[redacted]`)
	}
	return text
}

func searchOutcomeCountsAsExploration(outcome agent.SearchToolOutcome) bool {
	switch outcome {
	case agent.SearchToolOutcomeMatched, agent.SearchToolOutcomeNoMatch:
		return true
	default:
		return false
	}
}

func codingOutcomeFromSearchOutcome(outcome agent.SearchToolOutcome) codingToolOutcome {
	switch outcome {
	case agent.SearchToolOutcomeMatched:
		return codingToolOutcomeSuccess
	case agent.SearchToolOutcomeNoMatch:
		// "No matches" is a normal search result, not an error.
		// The SubAgent is exploring — it shouldn't count as a failure.
		return codingToolOutcomeSuccess
	default:
		return codingToolOutcomeFailed
	}
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
	if command, ok := copied["command"].(string); ok {
		copied["command"] = strings.TrimSpace(command)
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
	if msg := rejectWindowsShellCompatibilityCommand(command); msg != "" {
		return msg
	}
	// Note: silenced git self-checks are rejected separately via
	// rejectSilencedGitSelfCheckCommand (hard block, no high-risk approval bypass).
	disallowed := false
	switch {
	case hasOpaqueShellWrapperCommand(normalized):
		disallowed = true
	case hasDisallowedGitCommand(normalized):
		disallowed = true
	case hasDisallowedRecursiveDeleteCommand(normalized):
		disallowed = true
	case hasDisallowedShellFileMutation(normalized):
		disallowed = true
	}
	if !disallowed {
		return ""
	}
	return fmt.Sprintf("拒绝执行高风险命令：%s。编码 SubAgent 不允许自动执行 Git 工作区改写、递归删除或通过 shell 直接改写文件；请改用更小范围的操作或文件编辑工具。", command)
}

// rejectSilencedGitSelfCheckCommand blocks git status/diff self-checks that
// redirect stderr/stdout to /dev/null. These redirections discard fatal text
// the quality gate needs for soft non-git classification. Callers must hard-
// return this rejection (do not offer high-risk approval bypass).
func rejectSilencedGitSelfCheckCommand(command string) string {
	if !isSubAgentDiffSelfCheckCommand(command) || !subAgentCommandSilencesGitStderr(command) {
		return ""
	}
	// Avoid the phrase "不是 git 仓库" here: soft non-git classifiers match that
	// marker and would treat a policy rejection as non-git evidence.
	return fmt.Sprintf("拒绝执行被重定向静默的 git 自检：%s。请去掉 2>/dev/null、>/dev/null、&>/dev/null 等重定向后重试（保留 fatal 原文）。若尚未初始化版本控制，直接在结论中说明即可。", command)
}

func rejectWindowsShellCompatibilityCommand(command string) string {
	if normalizedRemotePlatform() != "windows" {
		return ""
	}
	if !hasWindowsShellCompatibilitySyntax(command) {
		return ""
	}
	return fmt.Sprintf("PowerShell command compatibility: %s uses bash-only syntax such as `mkdir -p`. Use PowerShell syntax and set working_dir to the command directory.", command)
}

func hasWindowsShellCompatibilitySyntax(command string) bool {
	fields := shellCommandFields(command)
	commandPosition := true
	for i := 0; i < len(fields); i++ {
		token := strings.ToLower(normalizeShellCommandToken(fields[i]))
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
			if commandNameBase(normalizeShellExecutableToken(token)) == "mkdir" && shellCommandSegmentHasArg(fields[i+1:], "-p") {
				return true
			}
		}
		commandPosition = false
	}
	return false
}

func shellCommandSegmentHasArg(fields []string, want string) bool {
	want = strings.ToLower(want)
	for _, field := range fields {
		token := strings.ToLower(normalizeShellCommandToken(field))
		if token == "" {
			continue
		}
		if isShellCommandBoundary(token) {
			return false
		}
		if token == want {
			return true
		}
	}
	return false
}

func hasOpaqueShellWrapperCommand(normalizedCommand string) bool {
	fields := shellCommandFields(normalizedCommand)
	for i := 0; i < len(fields); i++ {
		cmd := commandNameBase(normalizeShellCommandToken(fields[i]))
		if cmd != "powershell" && cmd != "pwsh" {
			continue
		}
		for j := i + 1; j < len(fields); j++ {
			token := normalizeShellCommandToken(fields[j])
			if isShellCommandBoundary(token) {
				break
			}
			if isPowerShellEncodedCommandOption(token) {
				return true
			}
		}
	}
	return false
}

func isPowerShellEncodedCommandOption(token string) bool {
	switch normalizeShellCommandToken(token) {
	case "-encodedcommand", "/encodedcommand", "-enc", "/enc", "-encodedarguments", "/encodedarguments":
		return true
	}
	return false
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
	if hasDisallowedInlineFileMutationScript(normalizedCommand) {
		return true
	}
	if strings.Contains(normalizedCommand, " of=") || strings.Contains(normalizedCommand, " of ") {
		return true
	}
	fields := shellCommandFields(normalizedCommand)
	if hasShellOutputRedirection(fields) || hasShellPipeToTee(fields) {
		return true
	}
	return false
}

func hasDisallowedInlineFileMutationScript(normalizedCommand string) bool {
	fields := shellCommandFields(normalizedCommand)
	for i := 0; i < len(fields); i++ {
		cmd := commandNameBase(normalizeShellCommandToken(fields[i]))
		switch cmd {
		case "node", "nodejs":
			if script, ok := nodeInlineScriptArgument(fields[i+1:]); ok && inlineJavaScriptWritesFile(script) {
				return true
			}
		case "bun":
			if script, ok := bunInlineScriptArgument(fields[i+1:]); ok && inlineBunJavaScriptWritesFile(script) {
				return true
			}
		case "deno":
			if script, ok := denoInlineScriptArgument(fields[i+1:]); ok && inlineDenoJavaScriptWritesFile(script) {
				return true
			}
		case "python", "python3", "py":
			if script, ok := pythonInlineScriptArgument(fields[i+1:]); ok && inlinePythonWritesFile(script) {
				return true
			}
		}
	}
	return false
}

func nodeInlineScriptArgument(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := normalizeShellCommandToken(args[i])
		switch {
		case arg == "-e" || arg == "--eval" || arg == "--print":
			if i+1 >= len(args) {
				return "", false
			}
			return args[i+1], true
		case strings.HasPrefix(arg, "--eval=") || strings.HasPrefix(arg, "--print="):
			return nodeInlineScriptArgumentRemainder(args, i, strings.TrimPrefix(strings.TrimPrefix(arg, "--eval="), "--print=")), true
		case nodeOptionConsumesValue(arg):
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return "", false
		}
	}
	return "", false
}

func inlineScriptArgumentRemainder(args []string, index int, initial string) string {
	parts := []string{initial}
	for i := index + 1; i < len(args); i++ {
		if token := normalizeShellCommandToken(args[i]); isShellCommandBoundary(token) {
			break
		}
		parts = append(parts, args[i])
	}
	return strings.Join(parts, " ")
}

func nodeInlineScriptArgumentRemainder(args []string, index int, initial string) string {
	compact := compactShellInlineCode(strings.ToLower(initial))
	if strings.Contains(compact, "require") {
		return inlineScriptArgumentRemainder(args, index, initial)
	}
	return initial
}

func pythonInlineScriptArgument(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := normalizeShellCommandToken(args[i])
		switch {
		case arg == "-c":
			if i+1 >= len(args) {
				return "", false
			}
			return args[i+1], true
		case strings.HasPrefix(arg, "-c") && len(arg) > 2:
			return inlineScriptArgumentRemainder(args, i, strings.TrimPrefix(arg, "-c")), true
		case pythonOptionConsumesValue(arg):
			i++
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return "", false
		}
	}
	return "", false
}

func bunInlineScriptArgument(args []string) (string, bool) {
	return nodeInlineScriptArgument(args)
}

func denoInlineScriptArgument(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := normalizeShellCommandToken(args[i])
		if arg != "eval" {
			if denoOptionConsumesValue(arg) {
				i++
			}
			continue
		}
		for j := i + 1; j < len(args); j++ {
			next := normalizeShellCommandToken(args[j])
			switch {
			case next == "":
				continue
			case denoOptionConsumesValue(next):
				j++
				continue
			case strings.HasPrefix(next, "-"):
				continue
			default:
				return args[j], true
			}
		}
		return "", false
	}
	return "", false
}

func nodeOptionConsumesValue(arg string) bool {
	switch arg {
	case "--require", "-r", "--import", "--loader", "--experimental-loader", "--input-type":
		return true
	}
	return false
}

func pythonOptionConsumesValue(arg string) bool {
	switch arg {
	case "-m", "-W", "-X", "--check-hash-based-pycs":
		return true
	}
	return false
}

func denoOptionConsumesValue(arg string) bool {
	switch arg {
	case "--config", "-c", "--import-map", "--cert", "--location", "--ext", "--node-modules-dir", "--v8-flags":
		return true
	}
	return false
}

func inlineJavaScriptWritesFile(script string) bool {
	code := compactShellInlineCode(stripShellInlineQuotedLiterals(strings.ToLower(script)))
	return strings.Contains(code, "writefilesync") ||
		strings.Contains(code, "promises.writefile") ||
		strings.Contains(code, ".writefile(") ||
		strings.Contains(code, ".appendfile(") ||
		strings.Contains(code, ".copyfile(") ||
		strings.Contains(code, ".copyfilesync(") ||
		strings.Contains(code, ".cpsync(") ||
		strings.Contains(code, ".renamesync(") ||
		strings.Contains(code, ".unlinksync(") ||
		strings.Contains(code, ".rmsync(") ||
		strings.Contains(code, ".mkdirsync(") ||
		inlineJavaScriptUsesMutatingFSAPI(code)
}

func inlineJavaScriptUsesMutatingFSAPI(code string) bool {
	for _, api := range []string{"writefile", "appendfile", "copyfile", "cp", "rename", "unlink", "rm", "mkdir"} {
		if strings.Contains(code, "promises."+api+"(") ||
			strings.Contains(code, "fs."+api+"(") ||
			strings.Contains(code, "require()."+api+"(") {
			return true
		}
	}
	return false
}

func inlineBunJavaScriptWritesFile(script string) bool {
	code := compactShellInlineCode(stripShellInlineQuotedLiterals(strings.ToLower(script)))
	return inlineJavaScriptWritesFile(script) ||
		strings.Contains(code, "bun.write(") ||
		strings.Contains(code, "bun.file().writer(")
}

func inlineDenoJavaScriptWritesFile(script string) bool {
	code := compactShellInlineCode(stripShellInlineQuotedLiterals(strings.ToLower(script)))
	for _, api := range []string{
		"writetextfile", "writetextfilesync", "writefile", "writefilesync",
		"copyfile", "copyfilesync", "rename", "renamesync",
		"remove", "removesync", "mkdir", "mkdirsync",
	} {
		if strings.Contains(code, "deno."+api+"(") {
			return true
		}
	}
	return inlineJavaScriptWritesFile(script)
}

func inlinePythonWritesFile(script string) bool {
	lower := strings.ToLower(script)
	code := compactShellInlineCode(stripShellInlineQuotedLiterals(lower))
	if strings.Contains(code, ".write_text(") || strings.Contains(code, ".write_bytes(") ||
		strings.Contains(code, ".touch(") || strings.Contains(code, ".mkdir(") || strings.Contains(code, ".unlink(") ||
		strings.Contains(code, ".rename(") || strings.Contains(code, ".replace(") || strings.Contains(code, ".rmdir(") ||
		strings.Contains(code, "os.remove(") || strings.Contains(code, "os.unlink(") || strings.Contains(code, "os.rmdir(") ||
		strings.Contains(code, "os.removedirs(") || strings.Contains(code, "os.rename(") || strings.Contains(code, "os.replace(") ||
		strings.Contains(code, "os.makedirs(") || strings.Contains(code, "shutil.copy") || strings.Contains(code, "shutil.move(") ||
		strings.Contains(code, "shutil.rmtree(") {
		return true
	}
	if !strings.Contains(code, "open(") {
		return false
	}
	return pythonInlineOpenUsesWriteMode(lower) ||
		strings.Contains(code, ".write(") || strings.Contains(code, ".writelines(")
}

func pythonInlineOpenUsesWriteMode(lowerScript string) bool {
	compact := compactShellInlineCode(lowerScript)
	for start := 0; start < len(compact); {
		idx := strings.Index(compact[start:], "open(")
		if idx < 0 {
			break
		}
		callStart := start + idx
		args, ok := shellInlineCallArguments(compact, callStart+len("open"))
		if ok && pythonOpenCallArgumentsUseWriteMode(args, callStart > 0 && compact[callStart-1] == '.') {
			return true
		}
		start = callStart + len("open(")
	}
	return false
}

func shellInlineCallArguments(compact string, openParen int) (string, bool) {
	if openParen < 0 || openParen >= len(compact) || compact[openParen] != '(' {
		return "", false
	}
	depth := 0
	quote := byte(0)
	for i := openParen; i < len(compact); i++ {
		ch := compact[i]
		if quote != 0 {
			if ch == '\\' && i+1 < len(compact) {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return compact[openParen+1 : i], true
			}
		}
	}
	return "", false
}

func pythonOpenCallArgumentsUseWriteMode(args string, methodOpen bool) bool {
	for _, literal := range shellInlineStringLiterals(args) {
		if !pythonFileModeMayWrite(literal.Value) {
			continue
		}
		if pythonLiteralIsKeywordArgument(args, literal.Start, "mode") {
			return true
		}
		if methodOpen || pythonLiteralFollowsArgumentSeparator(args, literal.Start) {
			return true
		}
	}
	return false
}

type shellInlineStringLiteral struct {
	Value string
	Start int
}

func shellInlineStringLiterals(text string) []shellInlineStringLiteral {
	literals := make([]shellInlineStringLiteral, 0)
	for start := 0; start < len(text); {
		idxSingle := strings.IndexByte(text[start:], '\'')
		idxDouble := strings.IndexByte(text[start:], '"')
		idx := idxSingle
		quote := byte('\'')
		if idx < 0 || (idxDouble >= 0 && idxDouble < idx) {
			idx = idxDouble
			quote = '"'
		}
		if idx < 0 {
			break
		}
		literalStart := start + idx
		valueStart := literalStart + 1
		valueEnd := valueStart
		for valueEnd < len(text) {
			if text[valueEnd] == '\\' && valueEnd+1 < len(text) {
				valueEnd += 2
				continue
			}
			if text[valueEnd] == quote {
				break
			}
			valueEnd++
		}
		if valueEnd >= len(text) {
			break
		}
		literals = append(literals, shellInlineStringLiteral{Value: text[valueStart:valueEnd], Start: literalStart})
		start = valueEnd + 1
	}
	return literals
}

func pythonLiteralIsKeywordArgument(args string, literalStart int, keyword string) bool {
	prefix := keyword + "="
	return literalStart >= len(prefix) && args[literalStart-len(prefix):literalStart] == prefix
}

func pythonLiteralFollowsArgumentSeparator(args string, literalStart int) bool {
	return literalStart > 0 && args[literalStart-1] == ','
}

func pythonFileModeMayWrite(mode string) bool {
	if mode == "" {
		return false
	}
	for _, ch := range mode {
		if !strings.ContainsRune("rwaxbt+", ch) {
			return false
		}
	}
	if strings.ContainsAny(mode, "wax") {
		return true
	}
	return strings.Contains(mode, "r") && strings.Contains(mode, "+")
}

func compactShellInlineCode(text string) string {
	return strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(text)
}

func stripShellInlineQuotedLiterals(text string) string {
	var out strings.Builder
	var quote rune
	for _, ch := range text {
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		out.WriteRune(ch)
	}
	return out.String()
}

func hasShellOutputRedirection(fields []string) bool {
	for _, field := range fields {
		token := normalizeShellCommandToken(field)
		if token == "2>&1" {
			continue
		}
		if isShellVerificationOutputRedirectionToken(token) {
			return true
		}
		if !strings.Contains(token, " ") && strings.Contains(token, ">") {
			return true
		}
	}
	return false
}

func hasShellPipeToTee(fields []string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if normalizeShellCommandToken(fields[i]) != "|" {
			continue
		}
		next := commandNameBase(normalizeShellCommandToken(fields[i+1]))
		if next == "tee" || next == "tee-object" {
			return true
		}
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
	lower := strings.ToLower(token)
	for _, suffix := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		if strings.HasSuffix(lower, suffix) {
			return token[:len(token)-len(suffix)]
		}
	}
	return token
}

func isShellVerificationOutputRedirectionToken(token string) bool {
	if token == "" || token == "2>&1" {
		return false
	}
	return token == ">" || token == ">>" || token == "1>" || token == "1>>" ||
		token == "2>" || token == "2>>" || token == "&>" || token == "&>>" ||
		token == "*>" || token == "*>>" || strings.HasPrefix(token, ">") ||
		strings.HasPrefix(token, "1>") || strings.HasPrefix(token, "2>") ||
		strings.HasPrefix(token, "&>") || strings.HasPrefix(token, "*>")
}

func isShellCommandBoundary(token string) bool {
	switch token {
	case "|", ";", "&&", "||", "&", "(", ")":
		return true
	}
	return false
}

func isShellCommandStartMarker(token string) bool {
	return isShellCommandBoundary(token) || isShellCommandOptionStartMarker(token)
}

func isShellCommandOptionStartMarker(token string) bool {
	token = strings.ToLower(token)
	return token == "-command" || token == "/command" || token == "-c" || token == "/c"
}

func currentSegmentStartsWithShellWrapper(segment []string) bool {
	return len(segment) > 0 && isShellWrapperCommand(commandNameBase(segment[0]))
}

func isShellWrapperCommand(cmd string) bool {
	switch strings.ToLower(cmd) {
	case "bash", "sh", "zsh", "fish", "cmd", "powershell", "pwsh":
		return true
	}
	return false
}

func isShellWrapperCommandOptionForCommand(cmd, token string) bool {
	cmd = strings.ToLower(cmd)
	token = strings.ToLower(token)
	if !isShellWrapperCommand(cmd) {
		return false
	}
	if isShellCommandOptionStartMarker(token) {
		return true
	}
	switch cmd {
	case "bash", "sh", "zsh", "fish":
		return strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "--") && strings.Contains(token[1:], "c")
	}
	return false
}

func shellCommandStartsAfterToken(token string, segment []string) bool {
	if isShellCommandBoundary(token) {
		return true
	}
	if len(segment) == 0 {
		return false
	}
	return isShellWrapperCommandOptionForCommand(commandNameBase(segment[0]), token)
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
	var fields []string
	var current strings.Builder
	var quote rune
	flush := func(fromQuote bool) {
		field := strings.TrimSpace(current.String())
		current.Reset()
		if field == "" {
			return
		}
		if fromQuote && shouldExpandQuotedShellCommand(field, fields) {
			fields = append(fields, shellCommandFields(field)...)
			return
		}
		fields = append(fields, field)
	}
	for i := 0; i < len(command); i++ {
		ch := rune(command[i])
		if quote != 0 {
			if ch == quote {
				quote = 0
				flush(true)
				continue
			}
			current.WriteRune(ch)
			continue
		}
		switch ch {
		case '\'', '"':
			flush(false)
			quote = ch
		case ' ', '\t', '\r', '\n':
			flush(false)
		case ';', '(', ')':
			flush(false)
			fields = append(fields, string(ch))
		case '&':
			if current.Len() > 0 && strings.Contains(current.String(), ">") {
				current.WriteRune(ch)
				continue
			}
			flush(false)
			if i+1 < len(command) && command[i+1] == '&' {
				fields = append(fields, "&&")
				i++
			} else {
				fields = append(fields, "&")
			}
		case '|':
			flush(false)
			if i+1 < len(command) && command[i+1] == '|' {
				fields = append(fields, "||")
				i++
			} else {
				fields = append(fields, "|")
			}
		default:
			current.WriteRune(ch)
		}
	}
	flush(quote != 0)
	return fields
}

func shouldExpandQuotedShellCommand(field string, currentFields []string) bool {
	if quotedFieldIsShellWrapperCommandArgument(currentFields) {
		return true
	}
	raw := strings.Fields(strings.ToLower(strings.TrimSpace(field)))
	if len(raw) == 0 {
		return false
	}
	segment := make([]string, 0, len(raw))
	for _, token := range raw {
		if normalized := normalizeShellCommandToken(token); normalized != "" {
			segment = append(segment, normalized)
		}
	}
	return isSubAgentVerificationCommandSegment(segment) || currentSegmentStartsWithShellWrapper(segment)
}

func quotedFieldIsShellWrapperCommandArgument(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	for i := len(fields) - 1; i >= 0; i-- {
		token := normalizeShellCommandToken(fields[i])
		if isShellCommandBoundary(token) {
			return false
		}
		if len(fields[:i+1]) > 0 && shellWrapperFieldsEndAtCommandOption(fields[:i+1]) {
			return true
		}
	}
	return false
}

func shellWrapperFieldsEndAtCommandOption(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	cmd := commandNameBase(normalizeShellCommandToken(fields[0]))
	if !isShellWrapperCommand(cmd) {
		return false
	}
	for i := 1; i < len(fields); i++ {
		token := normalizeShellCommandToken(fields[i])
		if shellWrapperOptionConsumesValue(token) {
			i++
			continue
		}
		if isShellWrapperCommandOptionForCommand(cmd, token) {
			return i == len(fields)-1
		}
		if strings.HasPrefix(token, "-") || strings.HasPrefix(token, "/") {
			continue
		}
		return false
	}
	return false
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
			case "reset", "checkout", "restore", "switch", "merge", "rebase", "stash", "add", "commit", "apply", "am", "cherry-pick", "revert", "rm", "mv", "update-index", "read-tree":
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
	cmd := strings.ToLower(normalizeShellExecutableToken(token))
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
	case isShellWrapperCommand(cmd):
		if consumed, ok := shellWrapperCommandPrefixLength(fields); ok {
			return consumed, true
		}
	}
	return 0, false
}

func shellWrapperCommandPrefixLength(fields []string) (int, bool) {
	if len(fields) == 0 {
		return 0, false
	}
	cmd := commandNameBase(fields[0])
	consumed := 1
	for consumed < len(fields) {
		next := normalizeShellCommandToken(fields[consumed])
		if shellWrapperOptionConsumesValue(next) {
			consumed++
			if consumed < len(fields) {
				consumed++
			}
			continue
		}
		if isShellWrapperCommandOptionForCommand(cmd, next) {
			return consumed + 1, true
		}
		if strings.HasPrefix(next, "-") || strings.HasPrefix(next, "/") {
			consumed++
			continue
		}
		break
	}
	return 0, false
}

func shellWrapperOptionConsumesValue(option string) bool {
	switch option {
	case "-executionpolicy", "/executionpolicy", "-inputformat", "/inputformat", "-outputformat", "/outputformat", "-configurationname", "/configurationname":
		return true
	}
	return false
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
	return c.toolGitDiffResult(args).Text
}

func (c *codingSubAgentCallbacks) toolGitDiffResult(args map[string]interface{}) codingToolExecutionResult {
	workDir, _ := args["path"].(string)
	if workDir == "" {
		workDir = c.projectPath()
	}
	if workDir == "" {
		workDir = "."
	}

	if result := c.ensureGitWorkTree(workDir); result.Outcome != codingToolOutcomeSuccess {
		if subAgentGitDiffUnavailableBecauseNonGit(result.Text) {
			return codingToolExecutionResult{
				Text:    subAgentGitDiffUnavailableNonGitSummary(workDir),
				Outcome: codingToolOutcomeSuccess,
			}
		}
		return result
	}

	staged, _ := args["staged"].(bool)
	command := "git diff -- ."
	if staged {
		command = "git diff --staged -- ."
	}
	ctx, cancel := c.toolContext()
	defer cancel()
	result := executeCodingBashWithContext(ctx, map[string]interface{}{
		"command":     command,
		"working_dir": workDir,
		"timeout":     float64(30),
	}, nil).toolResult()
	if result.Outcome == codingToolOutcomeSuccess && !staged {
		result.Text = appendSubAgentUntrackedGitFiles(ctx, workDir, result.Text)
	}
	return result
}

func appendSubAgentUntrackedGitFiles(ctx context.Context, workDir, diffText string) string {
	cmd := exec.CommandContext(ctx, passthroughRuntimeProgram("git"), "-C", workDir, "ls-files", "--others", "--exclude-standard", "--", ".")
	hideCommandWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return diffText
	}
	files := uniqueSortedSubAgentStrings(strings.Split(strings.TrimSpace(string(output)), "\n"))
	if len(files) == 0 {
		return diffText
	}
	if isEmptySubAgentDiffOutput(diffText) {
		diffText = ""
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(diffText))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("Untracked files:\n")
	for _, file := range files {
		b.WriteString("- ")
		b.WriteString(file)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func (c *codingSubAgentCallbacks) ensureGitWorkTree(workDir string) codingToolExecutionResult {
	info, err := os.Stat(workDir)
	if err != nil {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("git diff unavailable: cannot inspect %s: %v", workDir, err),
			Outcome: codingToolOutcomeFailed,
		}
	}
	if !info.IsDir() {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("git diff unavailable: %s is not a directory", workDir),
			Outcome: codingToolOutcomeFailed,
		}
	}

	ctx, cancel := c.toolContext()
	defer cancel()
	cmd := exec.CommandContext(ctx, passthroughRuntimeProgram("git"), "-C", workDir, "rev-parse", "--is-inside-work-tree")
	hideCommandWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("git diff unavailable: %s is not a git repository (%s)", workDir, strings.TrimSpace(string(output))),
			Outcome: codingToolOutcomeFailed,
		}
	}
	if strings.TrimSpace(string(output)) != "true" {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("git diff unavailable: %s is not a git work tree", workDir),
			Outcome: codingToolOutcomeFailed,
		}
	}
	return codingToolExecutionResult{Text: "git work tree detected", Outcome: codingToolOutcomeSuccess}
}

func subAgentGitDiffUnavailableBecauseNonGit(text string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	if normalized == "" {
		return false
	}
	nonGitMarkers := []string{
		"not a git repository",
		"not a git work tree",
		"not a git worktree",
		"outside a work tree",
		"must be run in a work tree",
		"不是 git 仓库",
		"不是一个 git 仓库",
		"不是 git 工作树",
		"不在工作树中",
	}
	for _, marker := range nonGitMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

// subAgentIsDashSeparatorLine reports agent-inserted probe separators such as
// echo "---" between git status/diff/log probes.
func subAgentIsDashSeparatorLine(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r != '-' {
			return false
		}
	}
	// Require at least "--" so a lone "-" cannot hide real one-character output.
	n := utf8.RuneCountInString(text)
	return n >= 2 && n <= 40
}

// subAgentIsExitMarkerLine reports instrumented remote bash / ACP exit markers
// with a numeric code (e.g. "EXIT: 128", "---EXIT_CODE:1"). Non-numeric lines
// such as printf format strings are not treated as markers.
func subAgentIsExitMarkerLine(text string) bool {
	_, ok := remoteCodingParseExitMarker(text)
	return ok
}

// subAgentLooksLikeBase64BlobLine reports wrapped base64 payload lines from the
// remote bash transport (`eval "$(echo '....' | base64 -d)"`).
// Threshold stays above 40-char git object ids so a lone SHA is not treated as
// transport noise and soft-skipped away.
func subAgentLooksLikeBase64BlobLine(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	// Keep the trailing quote/paren crumbs from wrapped eval payloads.
	text = strings.Trim(text, "`'\"| )")
	if len(text) < 48 {
		return false
	}
	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '/', r == '=':
			continue
		default:
			return false
		}
	}
	return true
}

// subAgentLastExitMarkerLineIndex returns the index of the last instrumented
// exit marker line, or -1 when none is present.
func subAgentLastExitMarkerLineIndex(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if subAgentIsExitMarkerLine(lines[i]) {
			return i
		}
	}
	return -1
}

// subAgentRemoteShellNoiseLine reports PTY/SSH envelope and remote bash wrapper
// scaffolding that must not count as git self-check diagnostic body.
func subAgentRemoteShellNoiseLine(line string) bool {
	text := strings.TrimSpace(remoteCodingStripSimpleANSI(line))
	if text == "" {
		return true
	}
	// OSC title sequences often precede the real prompt on the same line.
	if i := strings.IndexByte(text, '\x07'); i >= 0 {
		text = strings.TrimSpace(text[i+1:])
		if text == "" {
			return true
		}
	}
	if subAgentIsExitMarkerLine(text) || subAgentIsDashSeparatorLine(text) {
		return true
	}
	if strings.HasPrefix(text, "[ssh_") || strings.Contains(text, "状态:") {
		return true
	}
	if strings.HasPrefix(text, "$ ") {
		return true
	}
	if strings.Contains(text, "base64 -d") || strings.Contains(text, "__maclaw_") {
		return true
	}
	if strings.HasPrefix(text, "eval ") || strings.HasPrefix(text, "sh -lc ") {
		return true
	}
	if subAgentLooksLikeBase64BlobLine(text) {
		return true
	}
	// remoteBashCommandWithExitMarker scaffolding echoed by the PTY.
	switch {
	case strings.HasPrefix(text, "if ["),
		text == "else",
		text == "fi",
		strings.HasPrefix(text, "printf "),
		strings.HasPrefix(text, "cd '"),
		strings.HasPrefix(text, "cd \""):
		return true
	}
	// Shell prompts after the command finishes.
	if strings.Contains(text, "@") && (strings.HasSuffix(text, "#") || strings.HasSuffix(text, "$")) {
		return true
	}
	if strings.HasPrefix(text, "(base)") && strings.Contains(text, "@") {
		return true
	}
	return false
}

// subAgentDiffSelfCheckMeaningfulBodyEmpty reports whether a failed git
// diff/status self-check produced no real diagnostic body — only exit markers,
// agent separators (echo "---"), and remote SSH/PTY wrapper noise. This is the
// common shape when agents run `git ... 2>/dev/null` outside a repo.
func subAgentDiffSelfCheckMeaningfulBodyEmpty(summary string) bool {
	lines := strings.Split(strings.ReplaceAll(summary, "\r\n", "\n"), "\n")
	// Ignore everything after the last EXIT marker (prompt / shell chrome).
	end := len(lines)
	if idx := subAgentLastExitMarkerLineIndex(lines); idx >= 0 {
		end = idx
	}
	for i := 0; i < end; i++ {
		if subAgentRemoteShellNoiseLine(lines[i]) {
			continue
		}
		return false
	}
	return true
}

// subAgentCommandSilencesGitStderr detects agent habits that hide
// "fatal: not a git repository" from quality-gate text matching.
func subAgentCommandSilencesGitStderr(command string) bool {
	lower := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if lower == "" || !strings.Contains(lower, "git") {
		return false
	}
	// Common redirections that drop git diagnostics from the captured body.
	switch {
	case strings.Contains(lower, "2>/dev/null"),
		strings.Contains(lower, "2> /dev/null"),
		strings.Contains(lower, "2>&-"),
		strings.Contains(lower, "&>/dev/null"),
		strings.Contains(lower, "&> /dev/null"),
		strings.Contains(lower, ">&/dev/null"),
		strings.Contains(lower, ">& /dev/null"):
		return true
	}
	// Full-discard forms: >/dev/null 2>&1  and  2>&1 >/dev/null
	hasStdoutNull := strings.Contains(lower, ">/dev/null") || strings.Contains(lower, "> /dev/null")
	hasStderrMerge := strings.Contains(lower, "2>&1")
	return hasStdoutNull && hasStderrMerge
}

// subAgentCommandIsPureGitSelfCheckProbes reports commands that only run git
// self-check probes (plus cd/echo separators). Mixed pipelines that also run
// make/test/etc. must not soft-skip via the "stderr silenced + empty body"
// fallback when the trailing EXIT marker was truncated.
func subAgentCommandIsPureGitSelfCheckProbes(command string) bool {
	if !isSubAgentDiffSelfCheckCommand(command) {
		return false
	}
	segments := shellCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "cd", "echo", "printf", "true", ":":
			continue
		case "git":
			if len(segment) < 2 {
				return false
			}
			switch segment[1] {
			case "diff", "status", "log", "show", "branch", "ls-files", "describe":
				continue
			case "rev-parse":
				// Only the work-tree probe is a soft self-check companion.
				ok := false
				for _, arg := range segment[2:] {
					if normalizeShellExecutableToken(arg) == "--is-inside-work-tree" {
						ok = true
						break
					}
				}
				if !ok {
					return false
				}
				continue
			default:
				// Mutating / unrelated git subcommands are not soft self-checks.
				return false
			}
		default:
			return false
		}
	}
	return true
}

// subAgentDiffSelfCheckLooksLikeSilencedNonGitFailure detects non-repo git
// self-checks whose stderr was redirected away (2>/dev/null), leaving only
// EXIT: 128 (or a truncated summary without the exit line) plus empty /
// separator / SSH-wrapper body. Real git fatals that still surface text
// (bad revision, lock, permission, …) keep a non-empty body and remain hard.
func subAgentDiffSelfCheckLooksLikeSilencedNonGitFailure(cmd CodingSubAgentCommandResult) bool {
	if !subAgentDiffSelfCheckMeaningfulBodyEmpty(cmd.Summary) {
		return false
	}
	pure := subAgentCommandIsPureGitSelfCheckProbes(cmd.Command)
	silenced := subAgentCommandSilencesGitStderr(cmd.Command)
	if code, ok := remoteCodingParseExitMarker(cmd.Summary); ok {
		if code != 128 {
			return false
		}
		// EXIT 128 + empty body:
		// - pure git probes → soft (classic non-repo / missing git metadata)
		// - stderr silenced → soft (diagnostic text was discarded)
		// - mixed non-silenced pipelines stay hard so we do not mask other fatals
		return pure || silenced
	}
	// No EXIT marker (often truncated audit text): only soft-skip pure git
	// probe commands that also silenced stderr. Mixed commands like
	// `git status 2>/dev/null; make` must stay hard when EXIT is unknown.
	return pure && silenced
}

func subAgentGitDiffUnavailableNonGitSummary(workDir string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		workDir = "."
	}
	return fmt.Sprintf("Git diff self-check unavailable: %s is not a Git repository. File changes were tracked by the coding audit instead.", workDir)
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
	sort.Strings(names)
	return names
}

func canonicalCodingSubAgentToolName(name string) string {
	trimmed := strings.TrimSpace(name)
	if codingSubAgentToolNames[trimmed] {
		return trimmed
	}
	// Check dynamic tools (case-sensitive first).
	if codingSubAgentDynamicToolNames[trimmed] {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "grep_search":
		return "ripgrep"
	case "search_files":
		return "Glob"
	}
	for _, candidate := range codingSubAgentToolOrder {
		if strings.ToLower(candidate) == lower {
			return candidate
		}
	}
	// Case-insensitive check for dynamic tools.
	for dyn := range codingSubAgentDynamicToolNames {
		if strings.ToLower(dyn) == lower {
			return dyn
		}
	}
	return trimmed
}

func (c *codingSubAgentCallbacks) trackFile(path string) {
	displayPath := c.displayProjectPath(path)
	c.mu.Lock()
	if c.filesModified == nil {
		c.filesModified = make(map[string]bool)
	}
	c.filesModified[displayPath] = true
	seq := c.nextEventSeqLocked()
	if c.firstEditSeq == 0 {
		c.firstEditSeq = seq
	}
	c.lastEditSeq = seq
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
	seq := c.nextEventSeqLocked()
	if c.firstReadSeq == 0 {
		c.firstReadSeq = seq
	}
}

func (c *codingSubAgentCallbacks) nextEventSeqLocked() uint64 {
	c.eventSeq++
	return c.eventSeq
}

func (c *codingSubAgentCallbacks) exploredBeforeFirstEdit() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.firstEditSeq == 0 {
		return true
	}
	return (c.firstReadSeq > 0 && c.firstReadSeq < c.firstEditSeq) || (c.firstSearchSeq > 0 && c.firstSearchSeq < c.firstEditSeq)
}

func (c *codingSubAgentCallbacks) lastEditSequence() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastEditSeq
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
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
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
	absProject = filepath.Clean(absProject)
	realProject, err := filepath.EvalSymlinks(absProject)
	if err != nil {
		return filepath.Clean(path)
	}
	realPath, err := evalCodingScopePath(absPath)
	if err != nil {
		return filepath.Clean(path)
	}
	if ok, err := pathWithinCleanDir(realPath, realProject); err == nil && ok {
		if rel, err := filepath.Rel(realProject, realPath); err == nil && rel != "." {
			return filepath.ToSlash(rel)
		}
	}
	if ok, err := pathWithinCleanDir(absPath, absProject); err == nil && ok {
		return filepath.Clean(realPath)
	}
	return filepath.Clean(path)
}

func (c *codingSubAgentCallbacks) trackGitDiff(result string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	seq := c.nextEventSeqLocked()
	c.gitDiffChecked = true
	c.lastGitDiff = compactSubAgentDiff(result)
	c.lastDiffSeq = seq
}

func (c *codingSubAgentCallbacks) rejectEmptyFinalGitDiff() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gitDiffChecked = false
}

func (c *codingSubAgentCallbacks) trackCommandResult(args map[string]interface{}, result string, succeeded bool) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return
	}
	workDir, _ := args["working_dir"].(string)
	c.mu.Lock()
	defer c.mu.Unlock()
	seq := c.nextEventSeqLocked()
	c.commandsRun = append(c.commandsRun, CodingSubAgentCommandResult{
		Command:    command,
		WorkingDir: workDir,
		Succeeded:  succeeded,
		Summary:    compactCommandResult(result),
		seq:        seq,
	})
	if succeeded && isCodeGraphExplorationCommand(command) {
		search := CodingSubAgentSearchResult{
			Tool:      "codegraph",
			Query:     compactSubAgentSearchText(codeGraphExplorationQuery(command)),
			Path:      compactSubAgentPathText(c.displayProjectPath(workDir)),
			Succeeded: true,
			Summary:   compactSearchResult(result),
			seq:       seq,
		}
		c.searchesRun = append(c.searchesRun, search)
		if c.firstSearchSeq == 0 && !subAgentSearchSuccessLooksEmpty(search) {
			c.firstSearchSeq = seq
		}
	}
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
	seq := c.nextEventSeqLocked()
	c.guardrails = append(c.guardrails, CodingSubAgentGuardrailViolation{
		Tool:     toolName,
		Category: classifyCodingGuardrailCategory(toolName, path, command, result),
		Path:     c.displayProjectPath(path),
		Command:  command,
		Summary:  compactGuardrailSummary(result),
		seq:      seq,
	})
}

func isCodeGraphExplorationCommand(command string) bool {
	for _, segment := range shellCommandSegments(strings.ToLower(strings.Join(strings.Fields(command), " "))) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) >= 2 && commandNameBase(segment[0]) == "codegraph" {
			switch segment[1] {
			case "explore", "node":
				return true
			}
		}
	}
	return false
}

func codeGraphExplorationQuery(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "codegraph"
	}
	return command
}

func (c *codingSubAgentCallbacks) trackDynamicToolResult(toolName, name, result string, succeeded bool) {
	toolName = strings.TrimSpace(toolName)
	name = strings.TrimSpace(name)
	if toolName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	seq := c.nextEventSeqLocked()
	c.dynamicTools = append(c.dynamicTools, CodingSubAgentDynamicToolResult{
		Tool:      toolName,
		Name:      compactSubAgentSearchText(name),
		Succeeded: succeeded,
		Summary:   compactCommandResult(result),
		seq:       seq,
	})
}

func (c *codingSubAgentCallbacks) dynamicToolResultCount() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.dynamicTools)
}

func (c *codingSubAgentCallbacks) trackRejectedDynamicToolResult(toolName, argsJSON, result string, initialCount int) {
	if c == nil || initialCount < 0 || !codingSubAgentDynamicToolNames[toolName] {
		return
	}
	if c.dynamicToolResultCount() != initialCount {
		return
	}
	c.trackDynamicToolResult(toolName, dynamicToolEvidenceName(toolName, argsJSON), result, false)
}

func dynamicToolEvidenceName(toolName, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(normalizeCodingSubAgentToolArguments(argsJSON)), &args); err != nil {
		return ""
	}
	switch canonicalCodingSubAgentToolName(toolName) {
	case "manage_skill":
		name, _ := args["name"].(string)
		return strings.TrimSpace(name)
	case "call_mcp_tool":
		serverID, _ := args["server_id"].(string)
		tool, _ := args["tool_name"].(string)
		serverID = strings.TrimSpace(serverID)
		tool = strings.TrimSpace(tool)
		if serverID == "" || tool == "" {
			return ""
		}
		return serverID + "/" + tool
	default:
		return ""
	}
}

func (c *codingSubAgentCallbacks) getDynamicToolsRun() []CodingSubAgentDynamicToolResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.dynamicTools) == 0 {
		return nil
	}
	out := make([]CodingSubAgentDynamicToolResult, len(c.dynamicTools))
	copy(out, c.dynamicTools)
	return out
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
	if query == "" {
		query, _ = args["query"].(string)
	}
	if query == "" && strings.EqualFold(strings.TrimSpace(toolName), "web_fetch") {
		query, _ = args["url"].(string)
	}
	path, _ := args["path"].(string)
	fetchOffset, fetchNextOffset, fetchTotalChars, fetchHasMore, fetchRangeKnown := localizationWebFetchPagination(toolName, args, result)
	c.mu.Lock()
	defer c.mu.Unlock()
	seq := c.nextEventSeqLocked()
	summary := compactSearchResult(result)
	if strings.EqualFold(strings.TrimSpace(toolName), "web_search") || strings.EqualFold(strings.TrimSpace(toolName), "web_fetch") {
		summary = truncateLocalizationWebAudit(result)
	}
	c.searchesRun = append(c.searchesRun, CodingSubAgentSearchResult{
		Tool:             toolName,
		Query:            compactSubAgentSearchText(query),
		Path:             compactSubAgentPathText(c.displayProjectPath(path)),
		Succeeded:        succeeded,
		Summary:          summary,
		FetchOffset:      fetchOffset,
		FetchNextOffset:  fetchNextOffset,
		FetchTotalChars:  fetchTotalChars,
		FetchHasMore:     fetchHasMore,
		FetchRangeKnown:  fetchRangeKnown,
		FetchAuditKnown:  strings.EqualFold(strings.TrimSpace(toolName), "web_fetch"),
		FetchResolvedURL: localizationWebFetchResolvedURL(result),
		seq:              seq,
	})
	if strings.EqualFold(strings.TrimSpace(toolName), "web_search") || strings.EqualFold(strings.TrimSpace(toolName), "web_fetch") {
		log.Printf("[coding-research] %s", localizationResearchToolDebugSummary(c.searchesRun[len(c.searchesRun)-1]))
	}
	if succeeded && c.firstSearchSeq == 0 && subAgentSearchProvidesExplorationEvidence(c.searchesRun[len(c.searchesRun)-1]) {
		c.firstSearchSeq = seq
	}
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
	return selectSubAgentCommandAuditEntries(values, maxItems)
}

func limitSubAgentSearchResults(values []CodingSubAgentSearchResult, maxItems int) []CodingSubAgentSearchResult {
	return selectSubAgentSearchAuditEntries(values, maxItems)
}

func selectSubAgentSearchAuditEntries(values []CodingSubAgentSearchResult, maxItems int) []CodingSubAgentSearchResult {
	if maxItems <= 0 || len(values) <= maxItems {
		return values
	}
	out := append([]CodingSubAgentSearchResult(nil), values...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Succeeded != out[j].Succeeded {
			return !out[i].Succeeded && out[j].Succeeded
		}
		if out[i].seq != 0 && out[j].seq != 0 && out[i].seq != out[j].seq {
			return out[i].seq > out[j].seq
		}
		return false
	})
	return out[:maxItems]
}

func limitSubAgentGuardrailViolations(values []CodingSubAgentGuardrailViolation, maxItems int) []CodingSubAgentGuardrailViolation {
	return selectSubAgentGuardrailAuditEntries(values, maxItems)
}

func limitSubAgentDynamicToolResults(values []CodingSubAgentDynamicToolResult, maxItems int) []CodingSubAgentDynamicToolResult {
	return selectSubAgentDynamicToolAuditEntries(values, maxItems)
}

func (c *codingSubAgentCallbacks) ensureFinalGitDiff(filesModified, filesCreated []string) (bool, string) {
	c.mu.Lock()
	alreadyChecked := c.gitDiffChecked && c.lastDiffSeq > 0 && c.lastDiffSeq >= c.lastEditSeq
	lastDiff := c.lastGitDiff
	c.mu.Unlock()

	if alreadyChecked && !isEmptySubAgentDiffOutput(lastDiff) {
		// Also collect --stat if not already done
		c.ensureDiffStat()
		return c.validateFinalGitDiffSummary(lastDiff, filesCreated)
	}
	if len(filesModified) == 0 {
		return false, ""
	}

	result := c.executeToolWithOutcome("git_diff", `{}`)
	if result.Outcome != codingToolOutcomeSuccess {
		return false, compactSubAgentDiff(result.Text)
	}
	diffSummary := compactSubAgentDiff(result.Text)
	if isEmptySubAgentDiffOutput(diffSummary) {
		c.rejectEmptyFinalGitDiff()
		return false, "git diff 无输出：已记录文件修改，但最终 diff 为空。请重新检查改动是否被还原，必要时重新编辑并再次运行 git_diff。"
	}
	// Collect structured --stat
	c.ensureDiffStat()
	return c.validateFinalGitDiffSummary(diffSummary, filesCreated)
}

func (c *codingSubAgentCallbacks) validateFinalGitDiffSummary(diffSummary string, filesCreated []string) (bool, string) {
	if subAgentGitDiffUnavailableBecauseNonGit(diffSummary) {
		return true, diffSummary
	}
	if subAgentDiffOnlyHasUntrackedFiles(diffSummary) && !subAgentFileListsIntersect(untrackedSubAgentDiffFiles(diffSummary), filesCreated) {
		c.rejectEmptyFinalGitDiff()
		return false, "git diff 只包含与本任务新建文件无关的未跟踪文件：已记录文件修改，但最终 diff 缺少本任务改动证据。请重新检查改动是否被还原，必要时重新编辑并再次运行 git_diff。"
	}
	return true, diffSummary
}

// ensureDiffStat runs `git diff --stat` and parses the output into structured
// SubAgentDiffStat. Called after the main diff check to collect metrics without
// blocking the quality gate (best-effort).
func (c *codingSubAgentCallbacks) ensureDiffStat() {
	c.mu.Lock()
	if c.diffStat != nil {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	workDir := c.projectPath()
	if workDir == "" {
		return
	}

	ctx, cancel := c.toolContext()
	defer cancel()
	result := executeCodingBashWithContext(ctx, map[string]interface{}{
		"command":     "git diff --stat -- .",
		"working_dir": workDir,
		"timeout":     float64(15),
	}, nil).toolResult()

	if result.Outcome != codingToolOutcomeSuccess {
		return
	}

	stat := parseGitDiffStat(result.Text)
	if stat != nil {
		c.mu.Lock()
		c.diffStat = stat
		c.mu.Unlock()
	}
}

// getDiffStat returns the parsed diff stat (may be nil if not a git repo or no changes).
func (c *codingSubAgentCallbacks) getDiffStat() *SubAgentDiffStat {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.diffStat
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
	// Out of scope — try interactive approval before rejecting.
	if c.subagent != nil && c.subagent.scopeApproval != nil {
		return c.subagent.scopeApproval.check("write_file", path, projectPath)
	}
	return formatScopeRejection("write_file", path, projectPath)
}

func (c *codingSubAgentCallbacks) requireProjectReadScope(path, toolName string) string {
	projectPath := c.projectPath()
	if projectPath == "" {
		return ""
	}
	ok, err := isPathWithinDir(path, projectPath)
	if err != nil {
		return fmt.Sprintf("无法确认 %s 路径是否位于项目目录内：%s", toolName, err.Error())
	}
	if ok {
		return ""
	}
	// Out of scope — try interactive approval before rejecting.
	if c.subagent != nil && c.subagent.scopeApproval != nil {
		return c.subagent.scopeApproval.check(toolName, path, projectPath)
	}
	return formatScopeRejection(toolName, path, projectPath)
}

func (c *codingSubAgentCallbacks) requireProjectWorkingDirScope(path string) string {
	projectPath := c.projectPath()
	if projectPath == "" || strings.TrimSpace(path) == "" {
		return ""
	}
	ok, err := isPathWithinDir(path, projectPath)
	if err != nil {
		return fmt.Sprintf("无法确认命令 working_dir 是否位于项目目录内：%s", err.Error())
	}
	if ok {
		return ""
	}
	// Out of scope — try interactive approval before rejecting.
	if c.subagent != nil && c.subagent.scopeApproval != nil {
		return c.subagent.scopeApproval.check("bash", path, projectPath)
	}
	return formatScopeRejection("bash", path, projectPath)
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
	// Out of scope — try interactive approval before rejecting.
	if c.subagent != nil && c.subagent.scopeApproval != nil {
		return c.subagent.scopeApproval.check("git_diff", path, projectPath)
	}
	return formatScopeRejection("git_diff", path, projectPath)
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

func (c *codingSubAgentCallbacks) requireReadBeforeWriteExisting(path string, args map[string]interface{}) string {
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
	if isCodingSubAgentReportAppend(abs, args) {
		return ""
	}
	if ok, msg := c.validateReadSnapshot(abs); !ok {
		if msg != "" {
			return msg
		}
		return fmt.Sprintf("目标文件已存在，请先调用 read_file(path=%q) 查看当前内容；只有创建全新文件时才能直接 write_file。", path)
	}
	return ""
}

func isCodingSubAgentReportAppend(path string, args map[string]interface{}) bool {
	mode := strings.TrimSpace(stringVal(args, "mode"))
	if !strings.EqualFold(mode, "append") {
		return false
	}
	return strings.EqualFold(filepath.Base(path), "TEST_REPORT.md")
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
		return false, fmt.Sprintf("文件 %s 自上次 read_file 后已变化（read_file 时 %s，当前 %s）。请先重新调用 read_file(path=%q) 获取最新内容，再重新应用最小编辑。", path, codingFileSnapshotSummary(snap), codingFileSnapshotSummary(current), path)
	}
	return true, ""
}

func codingFileSnapshotSummary(snap codingFileSnapshot) string {
	hash := snap.Hash
	if len(hash) > 12 {
		hash = hash[:12]
	}
	if hash == "" {
		hash = "unknown"
	}
	return fmt.Sprintf("size=%d sha256=%s", snap.Size, hash)
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
	return canonicalCodingPathKey(path)
}

func canonicalCodingPathKey(path string) string {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if subAgentWorkingDirLooksWindowsAbsolute(clean) {
		return strings.ToLower(clean)
	}
	return clean
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
	if isEmptySubAgentDiffOutput(diff) {
		return "git diff 无输出"
	}
	return truncateRunesForSubAgent(diff, 2000)
}

func isEmptySubAgentDiffOutput(diff string) bool {
	diff = strings.TrimSpace(diff)
	return diff == "" || diff == "(command completed with no output)" || diff == "(命令执行完成，无输出)" || diff == "git diff 无输出"
}

func untrackedSubAgentDiffFiles(diff string) []string {
	lines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	inSection := false
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "Untracked files:" {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			break
		}
		file := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if file != "" {
			files = append(files, filepath.ToSlash(file))
		}
	}
	return uniqueSortedSubAgentStrings(files)
}

func subAgentDiffOnlyHasUntrackedFiles(diff string) bool {
	diff = strings.TrimSpace(diff)
	return len(untrackedSubAgentDiffFiles(diff)) > 0 && !strings.Contains(diff, "diff --git ")
}

func subAgentFileListsIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, item := range a {
		item = filepath.ToSlash(strings.TrimSpace(item))
		if item != "" {
			seen[item] = true
		}
	}
	for _, item := range b {
		item = filepath.ToSlash(strings.TrimSpace(item))
		if item != "" && seen[item] {
			return true
		}
	}
	return false
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

func fallbackSubAgentTaskSummary(status TaskExecStatus, task *TaskItem, iterations, toolCalls int) string {
	prefix := "任务"
	switch status {
	case TaskExecFailed:
		prefix = "任务运行错误"
	case TaskExecSkipped:
		prefix = "任务已跳过"
	default:
		prefix = "任务执行完成"
	}
	return fmt.Sprintf("%s T%d，%d 轮迭代，%d 次工具调用", prefix, taskDisplayNumber(task), iterations, toolCalls)
}

func rebaseFallbackSubAgentTaskSummary(summary string, status TaskExecStatus, task *TaskItem, iterations, toolCalls int) string {
	replacement := fallbackSubAgentTaskSummary(status, task, iterations, toolCalls)
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return replacement
	}
	if idx := strings.Index(summary, "\n\n## "); idx >= 0 {
		return replacement + summary[idx:]
	}
	return replacement
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
	sort.SliceStable(entries, func(i, j int) bool {
		left := guardrailViolationSummaryPriority(entries[i].Violation)
		right := guardrailViolationSummaryPriority(entries[j].Violation)
		if left != right {
			return left > right
		}
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return false
	})
	return entries
}

func selectSubAgentGuardrailAuditEntries(values []CodingSubAgentGuardrailViolation, maxItems int) []CodingSubAgentGuardrailViolation {
	if maxItems <= 0 || len(values) <= maxItems {
		return values
	}
	out := append([]CodingSubAgentGuardrailViolation(nil), values...)
	sort.SliceStable(out, func(i, j int) bool {
		left := guardrailViolationSummaryPriority(out[i])
		right := guardrailViolationSummaryPriority(out[j])
		if left != right {
			return left > right
		}
		if out[i].seq != 0 && out[j].seq != 0 && out[i].seq != out[j].seq {
			return out[i].seq > out[j].seq
		}
		return false
	})
	return out[:maxItems]
}

func guardrailViolationSummaryPriority(v CodingSubAgentGuardrailViolation) int {
	switch v.Category {
	case codingSubAgentGuardrailCategoryGit, codingSubAgentGuardrailCategoryDelete, codingSubAgentGuardrailCategoryShellWrite:
		return 5
	case codingSubAgentGuardrailCategoryCommand:
		return 4
	case codingSubAgentGuardrailCategoryScope:
		return 3
	case codingSubAgentGuardrailCategoryHost:
		return 2
	case codingSubAgentGuardrailCategoryPolicy:
		return 1
	default:
		return 0
	}
}

func compactCommandResult(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return "(无输出)"
	}
	const maxRunes = 1000
	if utf8.RuneCountInString(result) <= maxRunes {
		return result
	}
	// Prefer head+tail when an instrumented EXIT marker is present so quality
	// gates keep exit codes and late diagnostics instead of only SSH prelude.
	if _, ok := remoteCodingParseExitMarker(result); ok {
		// Keep enough head for context and enough tail for EXIT + last output.
		head, tail := 280, 680
		if head+tail > maxRunes {
			tail = maxRunes - head
		}
		return truncateRunesMiddle(result, head, tail)
	}
	return truncateRunesForSubAgent(result, maxRunes)
}

// subAgentResultLooksLikeRemotePTYDump reports SSH/remote-bash envelope chrome
// that should not be preferred over EXIT markers in failure banners.
func subAgentResultLooksLikeRemotePTYDump(result string) bool {
	if result == "" {
		return false
	}
	return strings.Contains(result, "[ssh_") ||
		strings.Contains(result, "__maclaw_") ||
		strings.Contains(result, "base64 -d") ||
		strings.Contains(result, "状态:")
}

func commandResultDiagnosticLine(result string) string {
	line := strings.TrimSpace(firstDiagnosticCodingToolResultLine(result))
	// Only apply remote PTY noise filtering when the result looks like an SSH
	// dump. Local tool failures must keep their first actionable diagnostic
	// (e.g. "printf: not found") without git-self-check heuristics.
	if !subAgentResultLooksLikeRemotePTYDump(result) {
		return line
	}
	// Remote PTY dumps often put SSH chrome first; prefer a real diagnostic or
	// the instrumented EXIT marker so failure banners are actionable.
	if line != "" && !subAgentRemoteShellNoiseLine(line) && !subAgentIsDashSeparatorLine(strings.TrimSpace(line)) {
		return line
	}
	if code, ok := remoteCodingParseExitMarker(result); ok {
		return fmt.Sprintf("EXIT: %d", code)
	}
	// Last non-noise body line before EXIT (if any).
	lines := strings.Split(strings.ReplaceAll(result, "\r\n", "\n"), "\n")
	end := len(lines)
	if idx := subAgentLastExitMarkerLineIndex(lines); idx >= 0 {
		end = idx
	}
	for i := end - 1; i >= 0; i-- {
		text := strings.TrimSpace(lines[i])
		if text == "" || subAgentRemoteShellNoiseLine(text) {
			continue
		}
		return text
	}
	return line
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
	summaryCommands := filterResolvedSubAgentCommandSummaryEntries(commands)
	if len(summaryCommands) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n\n## 命令验证\n\n")
	shownCommands := selectSubAgentCommandSummaryEntries(summaryCommands, codingSubAgentCommandSummaryMax)
	for _, cmd := range shownCommands {
		status := subAgentCommandSummaryStatus(cmd)
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
			if summary := commandResultDiagnosticLine(cmd.Summary); summary != "" {
				b.WriteString("\n  ")
				b.WriteString(truncateRunesForSubAgent(summary, codingSubAgentCommandOutputLineMaxRunes))
			}
		}
		b.WriteString("\n")
	}
	if remaining := len(summaryCommands) - len(shownCommands); remaining > 0 {
		b.WriteString(fmt.Sprintf("- ... 还有 %d 条命令记录未展开\n", remaining))
	}
	return b.String()
}

func filterResolvedSubAgentCommandSummaryEntries(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	laterSucceeded := make(map[string]bool, len(commands))
	laterVerificationSucceeded := false
	reversed := make([]CodingSubAgentCommandResult, 0, len(commands))
	for i := len(commands) - 1; i >= 0; i-- {
		cmd := commands[i]
		key := subAgentCommandFailureResolutionKey(cmd)
		if cmd.Succeeded && !subAgentCommandSuccessLooksEmpty(cmd) {
			if key != "" {
				laterSucceeded[key] = true
			}
			if isSubAgentVerificationCommand(cmd.Command) {
				laterVerificationSucceeded = true
			}
			reversed = append(reversed, cmd)
			continue
		}
		if !cmd.Succeeded && key != "" && laterSucceeded[key] {
			continue
		}
		if !cmd.Succeeded && laterVerificationSucceeded && subAgentCommandFailureCanBeResolvedByLaterVerification(cmd) {
			continue
		}
		// Soft failures stay in the list as SKIP (see subAgentCommandSummaryStatus);
		// they are excluded from blocking failure counts separately.
		reversed = append(reversed, cmd)
	}
	filtered := make([]CodingSubAgentCommandResult, len(reversed))
	for i := range reversed {
		filtered[len(reversed)-1-i] = reversed[i]
	}
	return filtered
}

func selectSubAgentCommandSummaryEntries(commands []CodingSubAgentCommandResult, maxItems int) []CodingSubAgentCommandResult {
	if maxItems <= 0 || len(commands) <= maxItems {
		return commands
	}
	selectedIndexes := make([]int, 0, maxItems)
	used := make(map[int]bool, maxItems)
	for i := len(commands) - 1; i >= 0; i-- {
		cmd := commands[i]
		if !subAgentCommandSummaryHasProblem(cmd) {
			continue
		}
		selectedIndexes = append(selectedIndexes, i)
		used[i] = true
		if len(selectedIndexes) == maxItems {
			return subAgentCommandResultsAtIndexes(commands, selectedIndexes)
		}
	}
	for i := len(commands) - 1; i >= 0; i-- {
		if used[i] {
			continue
		}
		selectedIndexes = append(selectedIndexes, i)
		if len(selectedIndexes) == maxItems {
			return subAgentCommandResultsAtIndexes(commands, selectedIndexes)
		}
	}
	return subAgentCommandResultsAtIndexes(commands, selectedIndexes)
}

func selectSubAgentCommandAuditEntries(commands []CodingSubAgentCommandResult, maxItems int) []CodingSubAgentCommandResult {
	if maxItems <= 0 || len(commands) <= maxItems {
		return commands
	}
	hasProblem := false
	for _, cmd := range commands {
		if subAgentCommandSummaryHasProblem(cmd) {
			hasProblem = true
			break
		}
	}
	if !hasProblem {
		out := make([]CodingSubAgentCommandResult, maxItems)
		copy(out, commands[len(commands)-maxItems:])
		return out
	}
	indexes := make([]int, len(commands))
	for i := range commands {
		indexes[i] = i
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		left := commands[indexes[i]]
		right := commands[indexes[j]]
		leftProblem := subAgentCommandSummaryHasProblem(left)
		rightProblem := subAgentCommandSummaryHasProblem(right)
		if leftProblem != rightProblem {
			return leftProblem
		}
		return subAgentAuditSeq(left.seq, indexes[i]) > subAgentAuditSeq(right.seq, indexes[j])
	})
	out := make([]CodingSubAgentCommandResult, 0, maxItems)
	for _, idx := range indexes[:maxItems] {
		out = append(out, commands[idx])
	}
	return out
}

func subAgentCommandResultsAtIndexes(commands []CodingSubAgentCommandResult, indexes []int) []CodingSubAgentCommandResult {
	sort.Ints(indexes)
	selected := make([]CodingSubAgentCommandResult, 0, len(indexes))
	for _, i := range indexes {
		if i >= 0 && i < len(commands) {
			selected = append(selected, commands[i])
		}
	}
	return selected
}

func subAgentCommandSummaryHasProblem(cmd CodingSubAgentCommandResult) bool {
	if subAgentCommandIsSoftFailure(cmd) {
		return false
	}
	return !cmd.Succeeded || subAgentCommandSuccessLooksEmpty(cmd)
}

func subAgentCommandSummaryStatus(cmd CodingSubAgentCommandResult) string {
	if subAgentCommandIsSoftFailure(cmd) {
		return "SKIP"
	}
	if !cmd.Succeeded {
		return "FAIL"
	}
	if subAgentCommandSuccessLooksEmpty(cmd) {
		return "EMPTY"
	}
	return "PASS"
}

func subAgentAuditSeq(seq uint64, index int) uint64 {
	if seq != 0 {
		return seq
	}
	return uint64(index + 1)
}

func appendSubAgentDynamicToolSummary(summary string, tools []CodingSubAgentDynamicToolResult) string {
	if len(tools) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n\n## 动态工具\n\n")
	shownTools := selectSubAgentDynamicToolSummaryEntries(tools, codingSubAgentCommandSummaryMax)
	for _, tool := range shownTools {
		status := "PASS"
		if !tool.Succeeded {
			status = "FAIL"
		} else if subAgentDynamicToolSuccessLooksEmpty(tool) {
			status = "EMPTY"
		}
		b.WriteString("- ")
		b.WriteString(status)
		b.WriteString(": `")
		b.WriteString(escapeSubAgentInlineCode(compactSubAgentSearchText(tool.Tool)))
		b.WriteString("`")
		if strings.TrimSpace(tool.Name) != "" {
			b.WriteString(" `")
			b.WriteString(escapeSubAgentInlineCode(compactSubAgentSearchText(tool.Name)))
			b.WriteString("`")
		}
		if tool.Summary != "" {
			b.WriteString("\n  ")
			b.WriteString(truncateRunesForSubAgent(commandResultDiagnosticLine(tool.Summary), codingSubAgentCommandOutputLineMaxRunes))
		}
		b.WriteString("\n")
	}
	if remaining := len(tools) - len(shownTools); remaining > 0 {
		b.WriteString(fmt.Sprintf("- ... 还有 %d 条动态工具记录未展开\n", remaining))
	}
	return b.String()
}
func selectSubAgentDynamicToolSummaryEntries(tools []CodingSubAgentDynamicToolResult, maxItems int) []CodingSubAgentDynamicToolResult {
	if maxItems <= 0 || len(tools) <= maxItems {
		return tools
	}
	selectedIndexes := make([]int, 0, maxItems)
	used := make(map[int]bool, maxItems)
	for i := len(tools) - 1; i >= 0; i-- {
		tool := tools[i]
		if !subAgentDynamicToolSummaryHasProblem(tool) {
			continue
		}
		selectedIndexes = append(selectedIndexes, i)
		used[i] = true
		if len(selectedIndexes) == maxItems {
			return subAgentDynamicToolResultsAtIndexes(tools, selectedIndexes)
		}
	}
	for i := len(tools) - 1; i >= 0; i-- {
		if used[i] {
			continue
		}
		selectedIndexes = append(selectedIndexes, i)
		if len(selectedIndexes) == maxItems {
			return subAgentDynamicToolResultsAtIndexes(tools, selectedIndexes)
		}
	}
	return subAgentDynamicToolResultsAtIndexes(tools, selectedIndexes)
}

func selectSubAgentDynamicToolAuditEntries(tools []CodingSubAgentDynamicToolResult, maxItems int) []CodingSubAgentDynamicToolResult {
	if maxItems <= 0 || len(tools) <= maxItems {
		return tools
	}
	hasProblem := false
	for _, tool := range tools {
		if subAgentDynamicToolSummaryHasProblem(tool) {
			hasProblem = true
			break
		}
	}
	if !hasProblem {
		out := make([]CodingSubAgentDynamicToolResult, maxItems)
		copy(out, tools[len(tools)-maxItems:])
		return out
	}
	indexes := make([]int, len(tools))
	for i := range tools {
		indexes[i] = i
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		left := tools[indexes[i]]
		right := tools[indexes[j]]
		leftProblem := subAgentDynamicToolSummaryHasProblem(left)
		rightProblem := subAgentDynamicToolSummaryHasProblem(right)
		if leftProblem != rightProblem {
			return leftProblem
		}
		return subAgentAuditSeq(left.seq, indexes[i]) > subAgentAuditSeq(right.seq, indexes[j])
	})
	out := make([]CodingSubAgentDynamicToolResult, 0, maxItems)
	for _, idx := range indexes[:maxItems] {
		out = append(out, tools[idx])
	}
	return out
}

func subAgentDynamicToolSummaryHasProblem(tool CodingSubAgentDynamicToolResult) bool {
	return !tool.Succeeded || subAgentDynamicToolSuccessLooksEmpty(tool)
}

func subAgentDynamicToolResultsAtIndexes(tools []CodingSubAgentDynamicToolResult, indexes []int) []CodingSubAgentDynamicToolResult {
	sort.Ints(indexes)
	selected := make([]CodingSubAgentDynamicToolResult, 0, len(indexes))
	for _, i := range indexes {
		if i >= 0 && i < len(tools) {
			selected = append(selected, tools[i])
		}
	}
	return selected
}

func summarizeSubAgentCommands(commands []CodingSubAgentCommandResult) (codingSubAgentQualityStatus, string) {
	if len(commands) == 0 {
		return codingSubAgentQualityNone, "no bash commands run"
	}
	summaryCommands := filterResolvedSubAgentCommandSummaryEntries(commands)
	skippedDiffChecks := countSoftNonGitDiffSelfCheckFailures(summaryCommands)
	skippedPathProbes := countSoftInspectionProbeFailures(summaryCommands)
	failed := filterSoftFailures(failedSubAgentCommands(summaryCommands))
	emptySuccesses := emptySuccessSubAgentCommands(summaryCommands)
	problems := append(append([]CodingSubAgentCommandResult{}, failed...), emptySuccesses...)
	if len(problems) == 0 {
		if skippedDiffChecks > 0 {
			checkWord := "self-checks"
			if skippedDiffChecks == 1 {
				checkWord = "self-check"
			}
			return codingSubAgentQualityPassed, fmt.Sprintf("%d bash command(s) run, %d skipped diff %s, no blocking failures", len(commands), skippedDiffChecks, checkWord)
		}
		if skippedPathProbes > 0 {
			if skippedPathProbes == 1 {
				return codingSubAgentQualityPassed, fmt.Sprintf("%d bash command(s) run, 1 soft missing-path probe, no blocking failures", len(commands))
			}
			return codingSubAgentQualityPassed, fmt.Sprintf("%d bash command(s) run, %d soft missing-path probes, no blocking failures", len(commands), skippedPathProbes)
		}
		if len(commands) == 1 {
			return codingSubAgentQualityPassed, "1 bash command run, no failures"
		}
		return codingSubAgentQualityPassed, fmt.Sprintf("%d bash commands run, no failures", len(commands))
	}
	return codingSubAgentQualityFailed, fmt.Sprintf("%d bash commands run, %s: %s", len(commands), summarizeSubAgentCommandProblemCounts(len(failed), len(emptySuccesses)), compactFailedVerificationCommandResults(problems))
}

func emptySuccessSubAgentCommands(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	empty := make([]CodingSubAgentCommandResult, 0)
	for _, cmd := range commands {
		if subAgentCommandSuccessLooksEmpty(cmd) {
			empty = append(empty, cmd)
		}
	}
	return empty
}

func summarizeSubAgentCommandProblemCounts(failedCount, emptySuccessCount int) string {
	var parts []string
	if failedCount == 1 {
		parts = append(parts, "1 failed")
	} else if failedCount > 1 {
		parts = append(parts, fmt.Sprintf("%d failed", failedCount))
	}
	if emptySuccessCount == 1 {
		parts = append(parts, "1 empty success")
	} else if emptySuccessCount > 1 {
		parts = append(parts, fmt.Sprintf("%d empty successes", emptySuccessCount))
	}
	return strings.Join(parts, ", ")
}

func summarizeSubAgentQuality(explorationStatus, verificationStatus codingSubAgentQualityStatus, diffChecked bool, filesModified, filesCreated []string, commands []CodingSubAgentCommandResult, lastEditSeq uint64, guardrails []CodingSubAgentGuardrailViolation, dynamicTools []CodingSubAgentDynamicToolResult) (codingSubAgentQualityStatus, string, int) {
	filesModified = uniqueSortedSubAgentStrings(filesModified)
	var failed []string
	var warnings []string
	postEditCommandsWithBlocked := filterPostEditSubAgentCommands(commands, lastEditSeq)
	unresolvedGuardrails := unresolvedSubAgentGuardrailViolations(guardrails, postEditCommandsWithBlocked)
	if len(unresolvedGuardrails) > 0 {
		failed = append(failed, fmt.Sprintf("%d guardrail block(s)", len(unresolvedGuardrails)))
	}
	postEditCommands := filterGuardrailBlockedSubAgentCommands(postEditCommandsWithBlocked, guardrails)
	failedCommands := unresolvedFailedSubAgentCommands(postEditCommands)
	failedCommandSummary := ""
	if len(failedCommands) > 0 {
		failedCommandSummary = summarizeFailedSubAgentCommandWarning(failedCommands)
	}
	if len(failedCommands) > 0 && verificationStatus != codingSubAgentQualityFailed {
		failed = append(failed, failedCommandSummary)
	}
	failedDynamicTools := unresolvedFailedSubAgentDynamicTools(filterPostEditSubAgentDynamicTools(dynamicTools, lastEditSeq))
	if len(failedDynamicTools) > 0 {
		failed = append(failed, summarizeFailedSubAgentDynamicToolWarning(failedDynamicTools))
	}
	if len(filesModified) > 0 {
		if explorationStatus == codingSubAgentQualityMissing && countExistingSubAgentModifiedFiles(filesModified, filesCreated) > 0 {
			failed = append(failed, "no exploration before existing-file edits")
		}
		if verificationStatus == codingSubAgentQualityMissing {
			failed = append(failed, "verification not run")
		} else if verificationStatus == codingSubAgentQualityFailed {
			if failedCommandSummary != "" {
				failed = append(failed, "verification failed: "+failedCommandSummary)
			} else {
				failed = append(failed, "verification failed")
			}
		}
		if !diffChecked {
			failed = append(failed, "diff not checked")
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

func summarizeSubAgentNoChangeEvidence(filesModified, filesCreated, filesRead []string, searches []CodingSubAgentSearchResult, commands []CodingSubAgentCommandResult, dynamicTools []CodingSubAgentDynamicToolResult) string {
	if len(uniqueSortedSubAgentStrings(append(append([]string{}, filesModified...), filesCreated...))) > 0 {
		return ""
	}
	if len(uniqueSortedSubAgentStrings(filesRead)) > 0 ||
		countSuccessfulSubAgentSearches(searches) > 0 ||
		countSuccessfulSubAgentVerificationCommands(commands) > 0 ||
		countSuccessfulSubAgentInspectionDynamicTools(dynamicTools) > 0 ||
		countSubAgentInspectionProbeEvidence(commands) > 0 {
		return ""
	}
	return "no file changes and no inspection or verification evidence"
}

// countSubAgentInspectionProbeEvidence counts shell probes that prove the agent
// inspected the environment (version checks, ls/find/which, and soft "path does
// not exist" probes). These satisfy no-change quality gates for env-check steps
// that intentionally make no file edits.
func countSubAgentInspectionProbeEvidence(commands []CodingSubAgentCommandResult) int {
	count := 0
	for _, cmd := range commands {
		if cmd.Succeeded {
			if subAgentInspectionProbeCommand(cmd.Command) && !subAgentCommandSuccessLooksEmpty(cmd) {
				count++
			}
			continue
		}
		// Negative existence checks (missing dir/file) are still useful evidence
		// for "check remote environment / workdir" plan steps.
		if subAgentCommandIsSoftInspectionProbeFailure(cmd) {
			count++
		}
	}
	return count
}

// subAgentInspectionProbeCommand reports whether a shell command is a read-only
// environment / path inspection probe (not a build/test/write).
func subAgentInspectionProbeCommand(command string) bool {
	normalized, segments := normalizeShellCommandSegments(command)
	if normalized == "" {
		return false
	}
	if subAgentDiagnosticProbeCommand(normalized) {
		return true
	}
	if len(segments) == 0 {
		return false
	}
	hasRealProbe := false
	for _, segment := range segments {
		if !subAgentInspectionProbeCommandSegment(segment) {
			return false
		}
		if len(segment) == 0 {
			continue
		}
		switch commandNameBase(segment[0]) {
		case "echo", "printf", "true", "false":
			// Separators / no-ops alone are not inspection evidence.
		default:
			hasRealProbe = true
		}
	}
	return hasRealProbe
}

func subAgentInspectionProbeCommandSegment(segment []string) bool {
	segment = stripVerificationCommandPrefixes(segment)
	if len(segment) == 0 {
		return false
	}
	if subAgentDiagnosticProbeCommandSegment(segment) {
		return true
	}
	cmd := commandNameBase(segment[0])
	switch cmd {
	case "ls", "find", "which", "type", "test", "stat", "uname", "pwd", "id",
		"whoami", "file", "basename", "dirname", "realpath", "readlink",
		"hostname", "nproc", "getconf", "arch", "tree", "du",
		"true", "false", "printf", "echo":
		return true
	case "command":
		// `command -v g++` / `command -V cmake`
		for _, arg := range segment[1:] {
			a := strings.TrimSpace(normalizeShellCommandToken(arg))
			if a == "-v" || a == "-V" {
				return true
			}
		}
	}
	return false
}

// subAgentMissingPathProbeMarkers are result phrases that mean a path probe
// answered "does not exist" (valid env-check evidence, not a hard task failure).
var subAgentMissingPathProbeMarkers = []string{
	"no such file or directory",
	"cannot access",
	"not a directory",
	"does not exist",
	"no such file",
	"cannot open",
	"can't cd to",
	"cannot cd",
}

// subAgentCommandIsSoftInspectionProbeFailure treats missing-path probe failures
// as non-blocking diagnostic outcomes (the probe answered "does not exist").
//
// Soft cases:
//  1. Path probes (ls/stat/find/test/…) with a missing-path error message.
//  2. Pure `test`/`[` existence probes (any non-hard failure; exit-code only).
//  3. Inspection probes that never ran because ssh_bash could not cd into
//     working_dir (common on brand-new remote project paths).
//  4. Multi-tool version inventory probes (e.g. g++/gcc/clang++ --version)
//     where some tools report versions and others are "not found" — documenting
//     an optional missing compiler is a valid T1 env finding, not a hard fail.
//  5. Multi-tool `which`/`command -v` inventory: GNU which exits 1 when any
//     named tool is missing and omits that name from stdout (no "not found"
//     line). Agents commonly chain `which g++ gcc clang++ cmake make && …`;
//     when required tools (g++/cmake/make) are listed as found paths and only
//     optional ones (clang++) are absent, that is valid T1 env evidence.
//
// Not soft: single required toolchain fully missing (no version output),
// permission denied, compile/link errors.
func subAgentCommandIsSoftInspectionProbeFailure(cmd CodingSubAgentCommandResult) bool {
	if cmd.Succeeded {
		return false
	}
	if subAgentDiagnosticProbeFailureResultLooksHard(cmd.Summary) {
		return false
	}
	summary := strings.ToLower(strings.Join(strings.Fields(cmd.Summary), " "))

	// `rg`, `grep`, and `git grep` use exit code 1 for the ordinary negative
	// result "no matches".  This is inspection evidence, not a failed build or
	// task action.  Keep exit 2 (bad pattern/I/O), 127 (tool missing), permission
	// errors, and compounds containing non-inspection commands hard.
	if subAgentReadOnlySearchNoMatchFailure(cmd.Command, cmd.Summary) {
		return true
	}

	// (1) Explicit path existence probes with a missing-path message.
	if subAgentPathExistenceProbeCommand(cmd.Command) && subAgentSummaryHasMissingPathMarker(summary) {
		return true
	}

	// (2) Pure `test`/`[` existence probes only communicate via exit code.
	// Remote ssh_bash often wraps them in large script dumps, so do not require
	// empty/silent output — any non-hard failure is a valid negative finding.
	if subAgentIsExistenceTestCommand(cmd.Command) {
		return true
	}

	// (3) Workdir/cd gate failed before a read-only probe body ran.
	// Remote ssh_bash wraps every call as `cd workdir` then the command; when the
	// project dir does not exist yet, even `g++ --version` fails with a cd error.
	// subAgentInspectionProbeCommand already covers diagnostic version probes.
	if subAgentMissingWorkdirCdFailure(summary) && subAgentInspectionProbeCommand(cmd.Command) {
		return true
	}

	// (4) Partial toolchain inventory: some tools printed versions and others
	// were not found (common: g++ present, clang++ absent).
	// Also soft when the summary was truncated (PTY dump) and only shows
	// not-found for a subset of tools named in a multi-tool --version probe —
	// we must not require the successful banner text still be present.
	// Accept inventory-only compounds (which + --version | head) in addition to
	// pure diagnostic probes — agents often mix both in one T1 shell line.
	if (subAgentDiagnosticProbeCommand(cmd.Command) || subAgentToolchainInventoryOnlyCommand(cmd.Command)) &&
		subAgentSummaryLooksLikeCommandNotFound(summary) &&
		(subAgentSummaryLooksLikeSuccessfulToolchainVersion(summary) ||
			subAgentPartialVersionInventoryMissingOnlySomeTools(cmd.Command, summary)) {
		return true
	}

	// (5) Multi-tool which/command -v inventory with optional tools absent.
	// GNU which does not print "clang++: not found"; it just exits 1 after
	// listing the tools that exist. Version probes after `&&` never run.
	if subAgentPartialWhichInventorySoftFailure(cmd) {
		return true
	}

	// `tree` is a convenience-only directory renderer, not a project build
	// dependency. Minimal remote images often omit it. Agents sometimes place
	// it before CMake/compiler checks in an `&&` chain, so its absence aborts
	// otherwise useful verification. Treat only this exact missing-command case
	// as soft; failures from find/ls/cmake/the compiler remain actionable.
	if subAgentOptionalTreeDisplayCommandMissing(cmd.Command, summary) {
		return true
	}
	return false
}

func subAgentReadOnlySearchNoMatchFailure(command, summary string) bool {
	if !subAgentSummaryLooksLikeSearchNoMatch(summary) {
		return false
	}
	segments := shellCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	hasSearch := false
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "rg", "ripgrep", "grep":
			hasSearch = true
		case "git":
			if len(segment) < 2 || normalizeShellExecutableToken(segment[1]) != "grep" {
				return false
			}
			hasSearch = true
		case "cd", "pushd", "popd":
			continue
		default:
			return false
		}
	}
	return hasSearch
}

func subAgentSummaryLooksLikeSearchNoMatch(summary string) bool {
	if exitCode, found := subAgentSummaryExitCode(summary); found {
		// rg/grep/git-grep reserve 1 for a clean no-match result. An explicit
		// operational exit code (normally 2) wins over possibly misleading text.
		return exitCode == 1
	}
	summary = strings.ToLower(strings.Join(strings.Fields(summary), " "))
	for _, phrase := range []string{
		"no match", "no matches", "0 matches", "found 0 matches",
		"no results", "0 results", "found 0 results",
		"未找到匹配", "没有匹配",
	} {
		if strings.Contains(summary, phrase) {
			return true
		}
	}
	return false
}

func subAgentSummaryExitCode(summary string) (int, bool) {
	// Dedicated shell instrumentation is authoritative only when the marker is
	// a standalone output line. This avoids treating command output such as
	// `echo "EXIT: 1"` as the shell's actual status.
	if code, found := remoteCodingParseAuthoritativeExitMarker(summary); found {
		return code, true
	}
	lastCode := 0
	found := false
	for _, field := range strings.FieldsFunc(summary, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ';'
	}) {
		field = strings.ToLower(strings.TrimSpace(field))
		value, ok := remoteCodingToolResultLineFieldValue(field, "exit_code", "exit code")
		if !ok {
			continue
		}
		if remoteCodingExitCodeValueIsZero(value) {
			lastCode, found = 0, true
			continue
		}
		if remoteCodingExitCodeValueLooksFailed(value) {
			// Search only distinguishes the conventional no-match code (1) from
			// every operational failure, so preserve 1 and collapse the rest.
			digits := strings.FieldsFunc(value, func(r rune) bool {
				return r < '0' || r > '9'
			})
			if len(digits) > 0 && digits[0] == "1" {
				lastCode = 1
			} else {
				lastCode = 2
			}
			found = true
		}
	}
	return lastCode, found
}

func subAgentOptionalTreeDisplayCommandMissing(command, summary string) bool {
	if !subAgentSummaryReportsToolNotFound(summary, "tree") {
		return false
	}
	segments := shellCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	first := stripVerificationCommandPrefixes(segments[0])
	return len(first) > 0 && commandNameBase(first[0]) == "tree"
}

// subAgentPartialWhichInventorySoftFailure reports multi-name which/type/
// command -v inventories where required build tools appear as found paths and
// only optional tools are missing (exit non-zero with no hard-error markers).
func subAgentPartialWhichInventorySoftFailure(cmd CodingSubAgentCommandResult) bool {
	if cmd.Succeeded {
		return false
	}
	if subAgentDiagnosticProbeFailureResultLooksHard(cmd.Summary) {
		return false
	}
	if !subAgentToolchainInventoryOnlyCommand(cmd.Command) {
		return false
	}
	tools := subAgentWhichInventoryToolsInCommand(cmd.Command)
	if len(tools) < 2 {
		return false
	}
	foundRequired := 0
	missingRequired := 0
	missingOptional := 0
	for _, tool := range tools {
		found := subAgentSummaryShowsWhichFoundTool(cmd.Summary, tool)
		if subAgentToolchainToolIsRequiredBuildProbe(tool) {
			if found {
				foundRequired++
			} else {
				missingRequired++
			}
		} else if !found {
			missingOptional++
		}
	}
	// Soft only when every required tool named in the inventory is present and
	// at least one explicitly-requested optional tool is absent. This prevents a
	// failed inventory containing only required tools from hiding an unrelated
	// shell or transport failure.
	return foundRequired > 0 && missingRequired == 0 && missingOptional > 0
}

// subAgentToolchainInventoryOnlyCommand is true when every shell segment is a
// read-only env inventory probe (which/version/uname/…) or a pipe viewer
// (head/tail) used to trim version output — never a build/test/write command.
func subAgentToolchainInventoryOnlyCommand(command string) bool {
	segments := shellCommandSegments(strings.ToLower(command))
	if len(segments) == 0 {
		return false
	}
	hasInventory := false
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "which", "where", "where.exe", "type", "whatis":
			hasInventory = true
			continue
		case "command":
			if !subAgentCommandLookupProbe(segment[1:]) {
				return false
			}
			hasInventory = true
			continue
		case "echo", "printf":
			// Only decorative inventory banners ("=== g++ ===", "---"), not prose.
			if !subAgentDiagnosticEchoSeparator(segment[1:]) {
				return false
			}
			continue
		case "false":
			// A deliberate failing shell command is not environment inventory.
			return false
		case "true", "head", "tail",
			"uname", "nproc", "arch", "hostname", "id", "whoami", "pwd",
			"getconf", "free", "df", "ls", "stat", "file":
			// Viewers / env facts allowed only alongside real inventory segments.
			continue
		}
		if subAgentDiagnosticProbeCommandSegment(segment) {
			hasInventory = true
			continue
		}
		if subAgentInspectionProbeCommandSegment(segment) {
			continue
		}
		return false
	}
	return hasInventory
}

// subAgentCommandLookupProbe accepts only the non-executing `command -v/-V`
// forms. `command <program>` would execute that program and must never make a
// failed shell line eligible for the inventory soft-failure path.
func subAgentCommandLookupProbe(args []string) bool {
	hasLookupFlag := false
	hasToolName := false
	for _, arg := range args {
		tok := strings.TrimSpace(normalizeShellCommandToken(arg))
		if tok == "" {
			continue
		}
		if tok == "-v" || tok == "-V" {
			hasLookupFlag = true
			continue
		}
		if strings.HasPrefix(tok, "-") {
			return false
		}
		hasToolName = true
	}
	return hasLookupFlag && hasToolName
}

// subAgentWhichInventoryToolsInCommand lists tool names passed to which/type/
// command -v (or where on Windows) in a compound inventory command.
func subAgentWhichInventoryToolsInCommand(command string) []string {
	seen := make(map[string]bool)
	var tools []string
	for _, segment := range shellCommandSegments(strings.ToLower(command)) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		args := segment[1:]
		switch cmd {
		case "which", "where", "where.exe", "type":
			// which g++ gcc clang++  /  type -a g++  /  where g++
		case "command":
			// command -v g++ / command -V cmake
			if !subAgentCommandLookupProbe(args) {
				continue
			}
			cleaned := make([]string, 0, len(args))
			for _, a := range args {
				tok := strings.TrimSpace(normalizeShellCommandToken(a))
				if tok == "-v" || tok == "-V" {
					continue
				}
				if tok == "" || strings.HasPrefix(tok, "-") {
					continue
				}
				cleaned = append(cleaned, tok)
			}
			args = cleaned
		default:
			continue
		}
		for _, a := range args {
			tok := strings.TrimSpace(normalizeShellCommandToken(a))
			if tok == "" || strings.HasPrefix(tok, "-") {
				continue
			}
			name := commandNameBase(tok)
			if name == "cl.exe" {
				name = "cl"
			}
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			tools = append(tools, name)
		}
	}
	return tools
}

// subAgentSummaryShowsWhichFoundTool reports that which/type output lists an
// absolute (or path-like) location for tool — e.g. "/usr/bin/g++".
func subAgentSummaryShowsWhichFoundTool(summary, tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	if tool == "" || summary == "" {
		return false
	}
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		// Skip PTY dumps / wrapper echoes that mention the tool name in the command.
		if strings.HasPrefix(lower, "$") || strings.HasPrefix(lower, "sh -lc") ||
			strings.HasPrefix(lower, "bash -lc") || strings.Contains(lower, "which ") ||
			strings.Contains(lower, "command -v") || strings.HasPrefix(lower, "eval ") ||
			strings.HasPrefix(lower, "[ssh_") || strings.HasPrefix(lower, "状态:") {
			continue
		}
		// Normalize Windows separators then take basename.
		norm := strings.ReplaceAll(lower, "\\", "/")
		base := pathpkg.Base(norm)
		base = strings.TrimSuffix(base, ".exe")
		if base == tool {
			return true
		}
	}
	return false
}

func subAgentSummaryLooksLikeCommandNotFound(summary string) bool {
	// Keep markers tight: bare "not found" / "no such file or directory" also
	// appear in path probes and unrelated errors, and would over-soft case (4).
	for _, marker := range []string{
		"command not found",
		"is not recognized",
		"不是内部或外部命令",
		"无法将", // PowerShell: 无法将“xxx”项识别为…
	} {
		if strings.Contains(summary, marker) {
			return true
		}
	}
	// sh/bash style: "sh: 1: clang++: not found" / "bash: foo: not found"
	if strings.Contains(summary, ": not found") {
		return true
	}
	return false
}

// subAgentSummaryLooksLikeSuccessfulToolchainVersion reports that a diagnostic
// probe body contains at least one real tool version banner (not just errors).
//
// Markers must come from tool *output*, not from the probe command text that
// remote PTY dumps echo (e.g. `sh -lc 'g++ --version'`). Matching on bare
// "--version" + tool name would false-soft a fully missing compiler.
func subAgentSummaryLooksLikeSuccessfulToolchainVersion(summary string) bool {
	if summary == "" {
		return false
	}
	for _, marker := range []string{
		"free software foundation",
		"copyright (c)",
		"cmake version", // cmake stdout, not a common flag form
		"gnu make",
		"clang version",
		"apple clang",
		"go version go", // "go version go1.22.x" stdout; avoids matching only the words in a command dump
		"rustc 1.",
		"rustc 0.",
	} {
		if strings.Contains(summary, marker) {
			return true
		}
	}
	// Compiler identity banners: "g++ (Ubuntu …)", "g++.exe (Rev…, Built by MSYS2…)"
	for _, tool := range []string{"g++", "gcc", "c++", "clang", "clang++"} {
		if subAgentSummaryHasToolchainParenBanner(summary, tool) {
			return true
		}
	}
	return false
}

// subAgentSummaryHasToolchainParenBanner matches "<tool> (" or "<tool>.exe (" in
// version stdout (not "tool: not found" / flag-only command echoes).
func subAgentSummaryHasToolchainParenBanner(summary, tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	if tool == "" {
		return false
	}
	for _, form := range []string{tool + " (", tool + ".exe ("} {
		if strings.Contains(summary, form) {
			return true
		}
	}
	return false
}

// subAgentPartialVersionInventoryMissingOnlySomeTools reports multi-tool
// --version inventories where the summary only marks optional tools as
// not-found (e.g. clang++), with no required build tool (g++/cmake/make)
// reported missing. Used when compactCommandResult truncates away successful
// version banners but keeps the optional-tool error line.
//
// If a required tool is reported missing, stay hard — that is a real env gap.
func subAgentPartialVersionInventoryMissingOnlySomeTools(command, summary string) bool {
	tools := subAgentVersionProbeToolsInCommand(command)
	if len(tools) < 2 {
		return false
	}
	missingOptional := false
	for _, tool := range tools {
		if !subAgentSummaryReportsToolNotFound(summary, tool) {
			continue
		}
		if subAgentToolchainToolIsRequiredBuildProbe(tool) {
			return false
		}
		missingOptional = true
	}
	return missingOptional
}

func subAgentToolchainToolIsRequiredBuildProbe(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "g++", "gcc", "c++", "cmake", "make", "gmake", "mingw32-make", "cl", "cl.exe":
		return true
	default:
		// clang++/pkg-config/boost probes are treated as optional inventory.
		return false
	}
}

// subAgentVersionProbeToolsInCommand lists tools invoked with a version/probe
// flag in a diagnostic inventory (g++, clang++, cmake, make, pkg-config, …).
func subAgentVersionProbeToolsInCommand(command string) []string {
	seen := make(map[string]bool)
	var tools []string
	// Pass the raw command (lowercased) into shellCommandSegments so ";" / "&&"
	// stay as boundaries. Do NOT strings.Fields-join first — that glues "2>&1;"
	// into one token and drops tools from the inventory.
	for _, segment := range shellCommandSegments(strings.ToLower(command)) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "echo", "printf", "true", "false", "cd":
			continue
		}
		args := stripShellRedirectionOnlyArgs(segment[1:])
		if !subAgentDiagnosticCompilerProbeArgs(args) && !subAgentDiagnosticVersionProbeArgs(args) &&
			!(subAgentDiagnosticBareProbeTool(cmd) && subAgentDiagnosticProbeArgsAreOnlyRedirection(args)) {
			continue
		}
		if seen[cmd] {
			continue
		}
		seen[cmd] = true
		tools = append(tools, cmd)
	}
	return tools
}

func subAgentSummaryReportsToolNotFound(summary, tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	summary = strings.ToLower(summary)
	if tool == "" || summary == "" {
		return false
	}
	// Prefix forms already include a delimiter before the tool name.
	for _, form := range []string{
		"bash: " + tool + ":",
		"sh: 1: " + tool + ":",
		"sh: " + tool + ":",
	} {
		if strings.Contains(summary, form) {
			return true
		}
	}
	// "g++: not found" must not match inside "clang++: not found".
	for _, suffix := range []string{": not found", ": command not found"} {
		needle := tool + suffix
		for i := 0; i+len(needle) <= len(summary); i++ {
			if summary[i:i+len(needle)] != needle {
				continue
			}
			if i > 0 {
				prev := summary[i-1]
				// '+' is part of g++/c++/clang++ names — treat as inside a longer tool.
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') || prev == '+' || prev == '_' || prev == '-' {
					continue
				}
			}
			return true
		}
	}
	return false
}

func subAgentSummaryHasMissingPathMarker(summary string) bool {
	for _, marker := range subAgentMissingPathProbeMarkers {
		if strings.Contains(summary, marker) {
			return true
		}
	}
	return false
}

func subAgentIsExistenceTestCommand(command string) bool {
	_, segments := normalizeShellCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	sawProbe := false
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			return false
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "echo", "printf", "true", "false":
			continue
		case "test", "[":
			// Normal forms: test -d path  /  [ -d path ]
			if !subAgentTestSegmentIsExistenceProbe(segment[1:]) {
				return false
			}
			sawProbe = true
		case "-d", "-e", "-f", "-L", "-h", "-b", "-c", "-p", "-S":
			// `[` / `]` are stripped by normalizeShellCommandToken, so
			// `[ -f /path ]` is often tokenized as ["-f", "/path"].
			if !subAgentTestSegmentIsExistenceProbe(segment) {
				return false
			}
			sawProbe = true
		default:
			return false
		}
	}
	return sawProbe
}

func subAgentTestSegmentIsExistenceProbe(args []string) bool {
	// Accept: test -d path | test -e path | test -f path | test -L path
	// and flag-first form after `[` stripping: -f /path
	cleaned := make([]string, 0, len(args))
	for _, a := range args {
		tok := strings.TrimSpace(normalizeShellCommandToken(a))
		if tok == "" || tok == "]" || tok == "[" || tok == "2>&1" {
			continue
		}
		cleaned = append(cleaned, tok)
	}
	if len(cleaned) < 2 {
		return false
	}
	switch cleaned[0] {
	case "-d", "-e", "-f", "-L", "-h", "-b", "-c", "-p", "-S":
		return true
	case "!", "-n", "-z":
		// Negated / string tests are not pure path-existence probes.
		return false
	default:
		return false
	}
}

// subAgentMissingWorkdirCdFailure detects remote/local shell wrappers that fail
// only because the configured working directory does not exist.
func subAgentMissingWorkdirCdFailure(summary string) bool {
	if summary == "" {
		return false
	}
	// Require a missing-path signal AND a cd token so we do not soft-match
	// unrelated "cannot cd" / permission issues (those are hard-filtered above
	// when markers like permission denied are present).
	hasMissing := strings.Contains(summary, "no such file or directory") ||
		strings.Contains(summary, "does not exist") ||
		strings.Contains(summary, "not a directory")
	if !hasMissing {
		return false
	}
	return strings.Contains(summary, "can't cd to") ||
		strings.Contains(summary, "cannot cd") ||
		strings.Contains(summary, "cd:") ||
		strings.Contains(summary, " cd ")
}

// subAgentPathExistenceProbeCommand is true for read-only probes whose failure
// commonly means "path missing" rather than "task broken".
func subAgentPathExistenceProbeCommand(command string) bool {
	_, segments := normalizeShellCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	hasPathProbe := false
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			return false
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "ls", "find", "stat", "test", "realpath", "readlink", "file":
			hasPathProbe = true
		case "echo", "printf", "true", "false":
			// Allowed as separators in compound probes.
		default:
			return false
		}
	}
	return hasPathProbe
}

// normalizeShellCommandSegments lowercases/collapses whitespace and returns
// shell segments once, shared by inspection/path probe classifiers.
func normalizeShellCommandSegments(command string) (string, [][]string) {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return "", nil
	}
	return normalized, shellCommandSegments(normalized)
}

func summarizeSubAgentCreatedFileContextEvidence(filesCreated, filesRead []string, searches []CodingSubAgentSearchResult, dynamicTools []CodingSubAgentDynamicToolResult) string {
	filesCreated = uniqueSortedSubAgentStrings(filesCreated)
	if len(filesCreated) == 0 {
		return ""
	}
	contextReads := existingSubAgentModifiedFiles(filesRead, filesCreated)
	if len(contextReads) > 0 || countSuccessfulSubAgentSearches(searches) > 0 || countSuccessfulSubAgentInspectionDynamicTools(dynamicTools) > 0 {
		return ""
	}
	return "created files without inspection or project-context evidence"
}
func countSuccessfulSubAgentInspectionDynamicTools(tools []CodingSubAgentDynamicToolResult) int {
	count := 0
	for _, tool := range tools {
		if tool.Succeeded && subAgentDynamicToolProvidesInspectionEvidence(tool) && !subAgentDynamicToolInspectionOutputLooksEmpty(tool) {
			count++
		}
	}
	return count
}

func subAgentDynamicToolInspectionOutputLooksEmpty(tool CodingSubAgentDynamicToolResult) bool {
	summary := strings.TrimSpace(tool.Summary)
	if summary == "" || summary == "(无输出)" {
		return true
	}
	if subAgentDynamicToolIsKnowledgeSearch(tool) {
		normalized := strings.ToLower(strings.Join(strings.Fields(summary), " "))
		for _, phrase := range []string{
			"no results",
			"no results found",
			"0 results",
			"found 0 results",
			"no matches",
			"0 matches",
			"found 0 matches",
			"no documents found",
			"no memories found",
			"no records found",
		} {
			if subAgentSummaryContainsEmptyVerificationPhrase(normalized, phrase) {
				return true
			}
		}
	}
	return false
}

func subAgentDynamicToolIsKnowledgeSearch(tool CodingSubAgentDynamicToolResult) bool {
	switch strings.ToLower(strings.TrimSpace(tool.Tool)) {
	case "coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return true
	default:
		return false
	}
}

func subAgentDynamicToolProvidesInspectionEvidence(tool CodingSubAgentDynamicToolResult) bool {
	if subAgentDynamicToolIsKnowledgeSearch(tool) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(tool.Tool)) {
	case "call_mcp_tool":
		return subAgentMCPToolCallProvidesInspectionEvidence(tool.Name)
	default:
		return false
	}
}

func subAgentMCPToolCallProvidesInspectionEvidence(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '\\' || r == ':' || r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	}) {
		if subAgentMCPToolNamePartLooksMutating(part) {
			return false
		}
	}
	return true
}

func subAgentMCPToolNamePartLooksMutating(part string) bool {
	part = strings.TrimSpace(part)
	if part == "" {
		return false
	}
	lower := strings.ToLower(part)
	switch lower {
	case "write", "create", "add", "insert", "upsert", "update", "delete", "remove", "replace", "rename", "move", "save", "merge", "archive", "restore", "approve", "reject", "enable", "disable", "send", "submit", "publish", "post", "put", "patch", "upload", "apply", "edit", "set", "run", "execute", "exec", "install", "deploy", "start", "stop", "restart":
		return true
	}
	for _, prefix := range []string{"write", "create", "add", "insert", "upsert", "update", "delete", "remove", "replace", "rename", "move", "save", "merge", "archive", "restore", "approve", "reject", "enable", "disable", "send", "submit", "publish", "post", "put", "patch", "upload", "apply", "edit", "execute", "exec", "install", "deploy", "start", "stop", "restart"} {
		if subAgentMCPToolNamePartHasMutatingPrefix(part, lower, prefix) {
			return true
		}
	}
	return false
}

func subAgentMCPToolNamePartHasMutatingPrefix(part, lower, prefix string) bool {
	if !strings.HasPrefix(lower, prefix) || len(lower) <= len(prefix) {
		return false
	}
	suffix := part[len(prefix):]
	if suffix == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(suffix)
	return unicode.IsUpper(first) || unicode.IsDigit(first)
}

func countSuccessfulSubAgentVerificationCommands(commands []CodingSubAgentCommandResult) int {
	count := 0
	for _, cmd := range commands {
		if cmd.Succeeded && isSubAgentVerificationCommand(cmd.Command) && !subAgentVerificationOutputLooksEmpty(cmd) {
			count++
		}
	}
	return count
}

func subAgentVerificationOutputLooksEmpty(cmd CodingSubAgentCommandResult) bool {
	if !isSubAgentVerificationCommand(cmd.Command) {
		return false
	}
	// Compilers like `python -m py_compile` emit no stdout on success; empty
	// output must not be treated as "no tests ran".
	if cmd.Succeeded && subAgentVerificationAllowsSilentSuccess(cmd.Command) {
		return false
	}
	summary := strings.ToLower(strings.Join(strings.Fields(cmd.Summary), " "))
	if summary == "" || summary == "(无输出)" {
		return true
	}
	emptyPhrases := []string{
		"[no test files]",
		"no test files",
		"no tests collected",
		"collected 0 items",
		"collected 0 tests",
		"no tests found",
		"no tests matching",
		"no tests matched",
		"no test files found",
		"no test files were found",
		"no test suite found",
		"no test suites found",
		"no tests were found",
		"no tests to run",
		"matched no packages",
		"0 passing",
		"0 examples",
		"0 tests found",
		"0 tests passed",
		"0 tests run",
		"0 tests total",
		"0 tests completed",
		"0 tests successful",
		"0 tests executed",
		"0 selected",
		"running 0 tests",
		"selected 0",
		"total tests: 0",
		"tests run: 0",
		"tests: 0 total",
		"tests 0 passed",
		"test suites: 0 total",
		"test suites: 0 passed",
		"test files 0 passed",
		"test files: 0 passed",
		"ran 0 tests",
		"executed 0 tests",
		"found 0 tests",
		"# tests 0",
		"0 test cases",
		"0 assertions",
		"0 specs",
		"0 scenarios",
		"0 features",
		"checked 0 files",
		"0 files checked",
		"processed 0 files",
		"0 files processed",
		"analyzed 0 files",
		"0 files analyzed",
		"matched 0 files",
		"found 0 files",
		"0 files found",
		"0 source files",
		"no source files",
		"no tests executed",
		"no specs found",
		"no files matching",
		"no files matched",
		"no matching files",
		"no files to check",
		"no files to lint",
		"no projects matched",
		"no projects found",
		"0 projects",
	}
	for _, phrase := range emptyPhrases {
		if subAgentSummaryContainsEmptyVerificationPhrase(summary, phrase) {
			return true
		}
	}
	return false
}

func subAgentSummaryContainsEmptyVerificationPhrase(summary, phrase string) bool {
	if phrase == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(summary[start:], phrase)
		if idx < 0 {
			return false
		}
		absolute := start + idx
		if subAgentSummaryPhraseHasBoundary(summary, phrase, absolute) {
			return true
		}
		start = absolute + len(phrase)
		if start >= len(summary) {
			return false
		}
	}
}

func subAgentSummaryPhraseHasBoundary(summary, phrase string, absolute int) bool {
	if strings.HasPrefix(phrase, "0 ") && absolute > 0 && isASCIIDigit(summary[absolute-1]) {
		return false
	}
	beforeOK := absolute == 0 || !isASCIIAlphaNumeric(summary[absolute-1])
	after := absolute + len(phrase)
	afterOK := after >= len(summary) || !isASCIIAlphaNumeric(summary[after])
	return beforeOK && afterOK
}

func isASCIIAlphaNumeric(b byte) bool {
	return isASCIIDigit(b) || (b >= 'a' && b <= 'z')
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func appendSubAgentQualityFailure(status codingSubAgentQualityStatus, summary string, count int, failure string) (codingSubAgentQualityStatus, string, int) {
	failure = strings.TrimSpace(failure)
	if failure == "" {
		return status, summary, count
	}
	if strings.TrimSpace(summary) == "" {
		summary = failure
	} else {
		summary += "; " + failure
	}
	return codingSubAgentQualityFailed, summary, count + 1
}

func summarizeSubAgentAcceptanceCriteriaEvidence(task *TaskItem, modelSummary string, filesModified, filesCreated []string) string {
	if task == nil || len(task.AcceptanceCriteria) == 0 {
		return ""
	}
	changedFiles := uniqueSortedSubAgentStrings(append(append([]string{}, filesModified...), filesCreated...))
	if len(changedFiles) == 0 {
		return ""
	}
	if !subAgentSummaryMentionsAcceptanceVerification(modelSummary) {
		return "acceptance criteria verification not summarized"
	}
	if !subAgentSummaryReferencesAcceptanceCriterion(modelSummary, task.AcceptanceCriteria) {
		return "acceptance criteria verification does not reference each listed criterion"
	}
	return ""
}

func summarizeSubAgentScopeEvidence(task *TaskItem, modelSummary string, filesModified, filesCreated []string) string {
	if task == nil || len(task.Files) == 0 {
		return ""
	}
	changedFiles := uniqueSortedSubAgentStrings(append(append([]string{}, filesModified...), filesCreated...))
	if len(changedFiles) == 0 {
		return ""
	}
	outside := subAgentFilesOutsidePlannedScope(changedFiles, task.Files)
	if len(outside) == 0 || subAgentSummaryExplainsScopeExpansion(modelSummary, outside) {
		return ""
	}
	return fmt.Sprintf("changed files outside listed task scope without summary rationale: %s", compactSubAgentFileList(outside, 5))
}

func subAgentFilesOutsidePlannedScope(changedFiles, plannedFiles []string) []string {
	plannedFiles = uniqueSortedSubAgentStrings(plannedFiles)
	if len(changedFiles) == 0 || len(plannedFiles) == 0 {
		return nil
	}
	var outside []string
	for _, changed := range uniqueSortedSubAgentStrings(changedFiles) {
		if subAgentFileWithinPlannedScope(changed, plannedFiles) {
			continue
		}
		outside = append(outside, changed)
	}
	return outside
}

func subAgentFileWithinPlannedScope(changed string, plannedFiles []string) bool {
	changedKey := strings.Trim(subAgentPathEvidenceKey(changed), "/")
	if changedKey == "" {
		return false
	}
	for _, planned := range plannedFiles {
		plannedKey := strings.Trim(subAgentPathEvidenceKey(planned), "/")
		if plannedKey == "" {
			continue
		}
		if changedKey == plannedKey || strings.HasPrefix(changedKey, plannedKey+"/") {
			return true
		}
	}
	return false
}

func subAgentSummaryExplainsScopeExpansion(summary string, outsideFiles []string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" || len(outsideFiles) == 0 {
		return false
	}
	slashSummary := filepath.ToSlash(summary)
	if subAgentSummaryHasScopeExpansionRationale(summary) {
		return true
	}
	for _, file := range outsideFiles {
		key := subAgentPathEvidenceKey(file)
		if subAgentSummaryContainsPathEvidence(slashSummary, key) {
			return true
		}
	}
	return false
}

func subAgentSummaryHasScopeExpansionRationale(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false
	}
	lower := strings.ToLower(summary)
	for _, token := range []string{
		"scope expansion", "scope expanded", "expanded scope",
		"outside listed", "outside planned", "outside scope", "out of scope",
		"additional file", "additional files", "extra file", "extra files",
		"also changed", "had to update", "needed to update", "required update",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	for _, token := range []string{
		"范围外", "超出范围", "范围扩展", "扩展范围", "涉及文件外",
		"额外修改", "额外文件", "同时修改", "还修改", "需要修改", "必须修改",
	} {
		if strings.Contains(summary, token) {
			return true
		}
	}
	return false
}

func summarizeSubAgentChangedFileSummaryEvidence(modelSummary string, filesModified, filesCreated []string) string {
	modelSummary = strings.TrimSpace(modelSummary)
	if modelSummary == "" {
		return ""
	}
	changedFiles := uniqueSortedSubAgentStrings(append(append([]string{}, filesModified...), filesCreated...))
	if len(changedFiles) == 0 {
		return ""
	}
	missing := subAgentSummaryMissingChangedFiles(modelSummary, changedFiles)
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("changed files not referenced in final summary: %s", compactSubAgentFileList(missing, 5))
}

func subAgentSummaryMissingChangedFiles(summary string, changedFiles []string) []string {
	slashSummary := strings.ToLower(filepath.ToSlash(strings.TrimSpace(summary)))
	if slashSummary == "" {
		return uniqueSortedSubAgentStrings(changedFiles)
	}
	var missing []string
	for _, file := range changedFiles {
		key := subAgentPathEvidenceKey(file)
		if !subAgentSummaryContainsPathEvidence(slashSummary, key) {
			missing = append(missing, file)
		}
	}
	return uniqueSortedSubAgentStrings(missing)
}

func subAgentSummaryContainsPathEvidence(slashSummary, pathKey string) bool {
	slashSummary = strings.ToLower(filepath.ToSlash(strings.TrimSpace(slashSummary)))
	pathKey = strings.ToLower(strings.Trim(subAgentPathEvidenceKey(pathKey), "/"))
	if slashSummary == "" || pathKey == "" {
		return false
	}
	for _, candidate := range []string{pathKey, "./" + pathKey} {
		if subAgentSummaryContainsPathEvidenceCandidate(slashSummary, candidate) {
			return true
		}
	}
	return false
}

func subAgentSummaryContainsPathEvidenceCandidate(summary, candidate string) bool {
	start := 0
	for {
		idx := strings.Index(summary[start:], candidate)
		if idx < 0 {
			return false
		}
		absolute := start + idx
		after := absolute + len(candidate)
		if subAgentPathEvidenceBoundaryBefore(summary, absolute) && subAgentPathEvidenceBoundaryAfter(summary, after) {
			return true
		}
		start = absolute + len(candidate)
	}
}

func subAgentPathEvidenceBoundaryBefore(summary string, absolute int) bool {
	if absolute <= 0 {
		return true
	}
	return !isSubAgentPathEvidenceContinuationByte(summary[absolute-1])
}

func subAgentPathEvidenceBoundaryAfter(summary string, after int) bool {
	if after >= len(summary) {
		return true
	}
	return !isSubAgentPathEvidenceContinuationByte(summary[after])
}

func isSubAgentPathEvidenceContinuationByte(b byte) bool {
	return isASCIIAlphaNumeric(b) || b == '_' || b == '-' || b == '.' || b == '/'
}

func summarizeSubAgentRiskSummaryEvidence(modelSummary string, filesModified, filesCreated []string) string {
	modelSummary = strings.TrimSpace(modelSummary)
	if modelSummary == "" {
		return ""
	}
	changedFiles := uniqueSortedSubAgentStrings(append(append([]string{}, filesModified...), filesCreated...))
	if len(changedFiles) == 0 || subAgentSummaryMentionsRisk(modelSummary) {
		return ""
	}
	return "remaining risk not called out in final summary"
}

func subAgentSummaryMentionsRisk(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false
	}
	lower := strings.ToLower(summary)
	for _, token := range []string{
		"risk:", "risks:", "risk：", "risks：",
		"remaining risk", "remaining risks", "residual risk", "residual risks",
		"known risk", "known risks", "known issue", "known issues",
		"no known risk", "no known risks", "no known remaining risk", "no known remaining risks",
		"blocker", "blockers", "blocked by",
		"not covered", "not automatically verified", "manual verification", "manual test", "manual testing",
	} {
		if subAgentSummaryContainsRiskPhrase(lower, token) {
			return true
		}
	}
	for _, token := range []string{
		"风险", "剩余风险", "残留风险", "已知风险",
		"阻塞", "已知问题", "未覆盖", "无法自动验证", "人工验证", "手动验证", "人工测试", "手动测试",
	} {
		if strings.Contains(summary, token) {
			return true
		}
	}
	return false
}

func subAgentSummaryContainsRiskPhrase(lowerSummary, phrase string) bool {
	lowerSummary = strings.ToLower(strings.TrimSpace(lowerSummary))
	phrase = strings.ToLower(strings.TrimSpace(phrase))
	if lowerSummary == "" || phrase == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(lowerSummary[start:], phrase)
		if idx < 0 {
			return false
		}
		absolute := start + idx
		if subAgentSummaryPhraseHasBoundary(lowerSummary, phrase, absolute) {
			return true
		}
		start = absolute + len(phrase)
		if start >= len(lowerSummary) {
			return false
		}
	}
}

func summarizeSubAgentVerificationCommandSummaryEvidence(modelSummary string, filesModified, filesCreated []string, commands []CodingSubAgentCommandResult, lastEditSeq uint64) string {
	modelSummary = strings.TrimSpace(modelSummary)
	if modelSummary == "" {
		return ""
	}
	changedFiles := uniqueSortedSubAgentStrings(append(append([]string{}, filesModified...), filesCreated...))
	if len(changedFiles) == 0 {
		return ""
	}
	freshVerification := filterFreshSubAgentVerificationCommands(commands, lastEditSeq)
	if len(freshVerification) == 0 {
		return ""
	}
	freshVerification = filterSubAgentVerificationCommandsWithExecutionEvidence(freshVerification)
	if len(freshVerification) == 0 {
		return "fresh verification command did not produce execution evidence"
	}
	claimed := subAgentClaimedVerificationCommands(modelSummary)
	if len(claimed) == 0 {
		return "verification command not referenced in final summary"
	}
	includesFresh, includesOutcome := subAgentClaimedVerificationIncludesFreshCommandOutcome(claimed, freshVerification)
	if !includesFresh {
		return "fresh verification command not referenced in final summary"
	}
	if !includesOutcome {
		return "fresh verification command outcome not referenced in final summary"
	}
	return ""
}

func filterSubAgentVerificationCommandsWithExecutionEvidence(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, cmd := range commands {
		if cmd.Succeeded && subAgentVerificationOutputLooksEmpty(cmd) {
			continue
		}
		filtered = append(filtered, cmd)
	}
	return filtered
}

func subAgentClaimedVerificationIncludesFreshCommand(claimed []subAgentClaimedVerificationCommand, fresh []CodingSubAgentCommandResult) bool {
	includesFresh, _ := subAgentClaimedVerificationIncludesFreshCommandOutcome(claimed, fresh)
	return includesFresh
}

func subAgentClaimedVerificationIncludesFreshCommandOutcome(claimed []subAgentClaimedVerificationCommand, fresh []CodingSubAgentCommandResult) (bool, bool) {
	if len(claimed) == 0 || len(fresh) == 0 {
		return false, false
	}
	freshCommands := make(map[string]bool, len(fresh))
	for _, cmd := range fresh {
		freshCommands[normalizeSubAgentCommandForEvidence(cmd.Command)] = true
	}
	claimedCommands := make(map[string]subAgentClaimedVerificationCommand, len(claimed))
	for _, command := range claimed {
		if key := normalizeSubAgentCommandForEvidence(command.Command); key != "" {
			claimedCommands[key] = command
		}
	}
	includesFresh := false
	for _, command := range claimed {
		if freshCommands[normalizeSubAgentCommandForEvidence(command.Command)] {
			includesFresh = true
			if command.ClaimedPassed || command.ClaimedFailed {
				return true, true
			}
		}
	}
	for _, cmd := range fresh {
		covered, includesOutcome := subAgentFreshCompoundVerificationCoveredByClaimed(cmd.Command, claimedCommands)
		if !covered {
			continue
		}
		includesFresh = true
		if includesOutcome {
			return true, true
		}
	}
	return includesFresh, false
}

func subAgentFreshCompoundVerificationCoveredByClaimed(command string, claimed map[string]subAgentClaimedVerificationCommand) (bool, bool) {
	if len(claimed) == 0 || !subAgentCommandContainsReliableVerificationChainBoundary(command) {
		return false, false
	}
	segments := shellCommandSegments(command)
	if len(segments) < 2 {
		return false, false
	}
	verificationSegments := 0
	coveredSegments := 0
	includesOutcome := false
	for _, segment := range segments {
		if !isSubAgentVerificationCommandSegment(segment) {
			continue
		}
		verificationSegments++
		claimedCommand, ok := claimed[normalizeSubAgentCommandFieldsForEvidence(segment)]
		if !ok {
			continue
		}
		coveredSegments++
		includesOutcome = includesOutcome || claimedCommand.ClaimedPassed || claimedCommand.ClaimedFailed
	}
	return verificationSegments > 1 && coveredSegments == verificationSegments, includesOutcome
}

func summarizeSubAgentClaimedVerificationEvidence(modelSummary string, commands []CodingSubAgentCommandResult) string {
	missing, _ := collectSubAgentClaimedVerificationEvidence(modelSummary, commands)
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("claimed verification command not found in audit log: %s", compactSubAgentClaimedCommandList(missing, 3))
}

func summarizeSubAgentClaimedVerificationFailureEvidence(modelSummary string, commands []CodingSubAgentCommandResult) string {
	_, claimedPassedButFailed := collectSubAgentClaimedVerificationEvidence(modelSummary, commands)
	if len(claimedPassedButFailed) == 0 {
		return ""
	}
	return fmt.Sprintf("claimed verification command passed but audit log recorded failure or empty success: %s", compactSubAgentClaimedCommandList(claimedPassedButFailed, 3))
}

func collectSubAgentClaimedVerificationEvidence(modelSummary string, commands []CodingSubAgentCommandResult) ([]string, []string) {
	claimed := subAgentClaimedVerificationCommands(modelSummary)
	if len(claimed) == 0 {
		return nil, nil
	}
	ran := make(map[string][]CodingSubAgentCommandResult, len(commands))
	for _, cmd := range commands {
		if !isSubAgentVerificationCommand(cmd.Command) {
			continue
		}
		key := normalizeSubAgentCommandForEvidence(cmd.Command)
		if key == "" {
			continue
		}
		ran[key] = append(ran[key], cmd)
	}
	var missing []string
	var claimedPassedButFailed []string
	for _, command := range claimed {
		audited := ran[normalizeSubAgentCommandForEvidence(command.Command)]
		if len(audited) == 0 {
			audited = subAgentClaimedVerificationCoveredByAuditedCompound(command.Command, commands)
		}
		if len(audited) == 0 {
			missing = append(missing, command.Command)
			continue
		}
		if command.ClaimedPassed && len(unresolvedFailedSubAgentCommands(audited)) > 0 {
			claimedPassedButFailed = append(claimedPassedButFailed, command.Command)
		}
	}
	return missing, claimedPassedButFailed
}

func subAgentClaimedVerificationCoveredByAuditedCompound(claimed string, audited []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	claimedKey := normalizeSubAgentCommandForEvidence(claimed)
	if claimedKey == "" {
		return nil
	}
	var covered []CodingSubAgentCommandResult
	for _, cmd := range audited {
		if !subAgentCommandContainsReliableVerificationChainBoundary(cmd.Command) {
			continue
		}
		segments := shellCommandSegments(cmd.Command)
		if len(segments) < 2 {
			continue
		}
		for _, segment := range segments {
			if normalizeSubAgentCommandFieldsForEvidence(segment) == claimedKey {
				covered = append(covered, cmd)
				break
			}
		}
	}
	return covered
}

func subAgentCommandContainsReliableVerificationChainBoundary(command string) bool {
	for _, field := range shellCommandFields(command) {
		if normalizeShellCommandToken(field) == "&&" {
			return true
		}
	}
	return false
}

func subAgentCommandContainsShellBoundary(command string) bool {
	for _, field := range shellCommandFields(command) {
		if isShellCommandBoundary(normalizeShellCommandToken(field)) {
			return true
		}
	}
	return false
}

type subAgentClaimedVerificationCommand struct {
	Command       string
	ClaimedPassed bool
	ClaimedFailed bool
}

func subAgentClaimedVerificationCommands(summary string) []subAgentClaimedVerificationCommand {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}
	var commands []subAgentClaimedVerificationCommand
	parts := strings.Split(summary, "`")
	for i := 1; i < len(parts); i += 2 {
		candidate := strings.TrimSpace(parts[i])
		if candidate == "" || strings.Contains(candidate, "\n") {
			continue
		}
		if len([]rune(candidate)) > 200 {
			continue
		}
		if isSubAgentVerificationCommand(candidate) {
			commands = append(commands, subAgentClaimedVerificationCommand{
				Command:       candidate,
				ClaimedPassed: subAgentInlineVerificationClaimedPassed(parts, i),
				ClaimedFailed: subAgentInlineVerificationClaimedFailed(parts, i),
			})
		}
	}
	commands = append(commands, subAgentClaimedVerificationLineCommands(summary)...)
	return uniqueSubAgentClaimedVerificationCommands(commands)
}

func subAgentClaimedVerificationLineCommands(summary string) []subAgentClaimedVerificationCommand {
	var commands []subAgentClaimedVerificationCommand
	for _, line := range strings.Split(summary, "\n") {
		commands = append(commands, subAgentVerificationLineCommandCandidates(line)...)
	}
	return commands
}

func subAgentVerificationLineCommandCandidates(line string) []subAgentClaimedVerificationCommand {
	candidate, claimedPassed, claimedFailed, ok := subAgentVerificationLineCommandCandidate(line)
	if !ok {
		return nil
	}
	_, rawClaimedPassed, rawClaimedFailed := trimSubAgentClaimedCommandTail(candidate)
	compoundCandidate, compoundClaimedPassed, compoundClaimedFailed := trimSubAgentClaimedCompoundCommandTail(candidate)
	parts := splitSubAgentVerificationCommandCandidates(candidate)
	if len(parts) == 0 {
		parts = []string{candidate}
	}
	commands := make([]subAgentClaimedVerificationCommand, 0, len(parts)+1)
	lineClaimsPassed := claimedPassed || rawClaimedPassed
	lineClaimsFailed := claimedFailed || rawClaimedFailed
	compoundClaimsPassed := lineClaimsPassed || compoundClaimedPassed
	compoundClaimsFailed := lineClaimsFailed || compoundClaimedFailed
	if subAgentClaimedVerificationCandidateHasMultipleVerificationSegments(compoundCandidate) {
		commands = append(commands, subAgentClaimedVerificationCommand{
			Command:       compoundCandidate,
			ClaimedPassed: compoundClaimsPassed,
			ClaimedFailed: compoundClaimsFailed,
		})
	}
	for _, part := range parts {
		part, partClaimedPassed, partClaimedFailed := trimSubAgentClaimedCommandTail(part)
		if !isSubAgentVerificationCommand(part) {
			continue
		}
		lineClaimsPassed = lineClaimsPassed || partClaimedPassed
		lineClaimsFailed = lineClaimsFailed || partClaimedFailed
		commands = append(commands, subAgentClaimedVerificationCommand{
			Command:       part,
			ClaimedPassed: partClaimedPassed,
			ClaimedFailed: partClaimedFailed,
		})
	}
	if len(commands) > 0 && (lineClaimsPassed || lineClaimsFailed) {
		for i := range commands {
			if !commands[i].ClaimedPassed && !commands[i].ClaimedFailed {
				commands[i].ClaimedPassed = lineClaimsPassed
				commands[i].ClaimedFailed = lineClaimsFailed
			}
		}
	}
	return commands
}

func subAgentClaimedVerificationCandidateHasMultipleVerificationSegments(candidate string) bool {
	verificationSegments := 0
	for _, segment := range shellCommandSegments(candidate) {
		if isSubAgentVerificationCommandSegment(segment) {
			verificationSegments++
			if verificationSegments > 1 {
				return true
			}
		}
	}
	return false
}

func trimSubAgentClaimedCompoundCommandTail(candidate string) (string, bool, bool) {
	candidate = strings.TrimSpace(candidate)
	claimedPassed := false
	claimedFailed := false
	for _, sep := range []string{"。", "，", ",", " - ", " — ", " -> ", " => "} {
		if idx := strings.Index(candidate, sep); idx >= 0 {
			tail := candidate[idx+len(sep):]
			if subAgentClaimTailClaimsPassed(tail) {
				claimedPassed = true
			}
			if subAgentClaimTailClaimsFailed(tail) {
				claimedFailed = true
			}
			candidate = candidate[:idx]
		}
	}
	return trimSubAgentClaimedCommandOutcomeSuffix(candidate, claimedPassed, claimedFailed)
}

func subAgentVerificationLineCommandCandidate(line string) (string, bool, bool, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false, false, false
	}
	line = strings.TrimLeft(line, "-*0123456789.）) \t")
	lower := strings.ToLower(line)
	prefixes := []string{
		"verification:", "verification：",
		"verification command:", "verification command：",
		"verified with:", "verified with：",
		"validated with:", "validated with：",
		"tests:", "tests：",
		"test command:", "test command：",
		"check command:", "check command：",
		"ran:", "ran：",
		"run:", "run：",
		"验证:", "验证：",
		"验证命令:", "验证命令：",
		"测试:", "测试：",
		"测试命令:", "测试命令：",
		"检查:", "检查：",
		"检查命令:", "检查命令：",
		"已运行:", "已运行：",
		"运行:", "运行：",
	}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		candidate := strings.TrimSpace(line[len(prefix):])
		candidate = strings.ReplaceAll(strings.Trim(candidate, "` "), "`", "")
		return candidate, false, false, candidate != ""
	}
	return "", false, false, false
}

func splitSubAgentVerificationCommandCandidates(candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return nil
	}
	parts := []string{candidate}
	for _, sep := range []string{"；", ";", " and ", " && "} {
		parts = splitSubAgentVerificationCommandCandidateParts(parts, sep)
	}
	parts = splitSubAgentVerificationCommandCandidatePartsOnComma(parts)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, "` "))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitSubAgentVerificationCommandCandidateParts(parts []string, sep string) []string {
	if len(parts) == 0 || sep == "" {
		return parts
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.Split(part, sep)...)
	}
	return out
}

func splitSubAgentVerificationCommandCandidatePartsOnComma(parts []string) []string {
	if len(parts) == 0 {
		return parts
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		commaParts := strings.Split(part, ",")
		if len(commaParts) == 1 {
			out = append(out, part)
			continue
		}
		allVerification := true
		for _, commaPart := range commaParts {
			candidate, _, _ := trimSubAgentClaimedCommandTail(commaPart)
			if !isSubAgentVerificationCommand(candidate) {
				allVerification = false
				break
			}
		}
		if allVerification {
			out = append(out, commaParts...)
		} else {
			out = append(out, part)
		}
	}
	return out
}

func trimSubAgentClaimedCommandTail(candidate string) (string, bool, bool) {
	claimedPassed := false
	claimedFailed := false
	for _, sep := range []string{"；", ";", "。", "，", ",", " - ", " — ", " -> ", " => "} {
		if idx := strings.Index(candidate, sep); idx >= 0 {
			tail := candidate[idx+len(sep):]
			if subAgentClaimTailClaimsPassed(tail) {
				claimedPassed = true
			}
			if subAgentClaimTailClaimsFailed(tail) {
				claimedFailed = true
			}
			candidate = candidate[:idx]
		}
	}
	return trimSubAgentClaimedCommandOutcomeSuffix(candidate, claimedPassed, claimedFailed)
}

func trimSubAgentClaimedCommandOutcomeSuffix(candidate string, claimedPassed, claimedFailed bool) (string, bool, bool) {
	candidate = strings.TrimSpace(candidate)
	lower := strings.ToLower(candidate)
	for _, suffix := range []string{
		" all tests passed", " all checks passed", " green", " clean",
		" all tests passed.", " all checks passed.", " green.", " clean.",
		" completed successfully", " completed successfully.",
		" returned exit code 0", " returned exit code 0.",
		" exit code 0", " exit code 0.",
		" exit 0", " exit 0.",
		" passed", " succeeded", " successful", " ok",
		" passed.", " succeeded.", " successful.", " ok.",
		" pass", " passes",
		" pass.", " passes.",
		" (passed)", " (succeeded)", " (ok)",
		" [passed]", " [succeeded]", " [ok]",
		"（通过）", "（成功）", " 全部通过", " 已通过", " 执行成功",
		" 全部通过。", " 已通过。", " 执行成功。",
	} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSpace(candidate[:len(candidate)-len(suffix)]), true, claimedFailed
		}
	}
	for _, suffix := range []string{
		" failed", " failing", " errored",
		" failed.", " failing.", " errored.",
		" (failed)", " (failing)", " (errored)",
		" [failed]", " [failing]", " [errored]",
		"（失败）", "（未通过）",
	} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSpace(candidate[:len(candidate)-len(suffix)]), claimedPassed, true
		}
	}
	candidate = strings.TrimSpace(strings.TrimRight(candidate, ".。"))
	return candidate, claimedPassed, claimedFailed
}

func subAgentInlineVerificationClaimedPassed(parts []string, commandPartIndex int) bool {
	if commandPartIndex+1 >= len(parts) {
		return false
	}
	return subAgentClaimTailClaimsPassed(parts[commandPartIndex+1])
}

func subAgentInlineVerificationClaimedFailed(parts []string, commandPartIndex int) bool {
	if commandPartIndex+1 >= len(parts) {
		return false
	}
	return subAgentClaimTailClaimsFailed(parts[commandPartIndex+1])
}

func subAgentClaimTailClaimsPassed(tail string) bool {
	return subAgentClaimTailHasOutcome(tail, []string{
		"passed", "pass", "passes",
		"succeeded", "successful", "completed successfully",
		"ok", "green", "clean",
		"exit 0", "exit code 0", "returned exit code 0",
		"all tests passed", "all checks passed",
		"通过", "已通过", "全部通过", "成功", "执行成功",
	})
}

func subAgentClaimTailClaimsFailed(tail string) bool {
	return subAgentClaimTailHasOutcome(tail, []string{"failed", "failing", "errored", "failure", "失败", "未通过"})
}

func subAgentClaimTailHasOutcome(tail string, prefixes []string) bool {
	tail = strings.TrimLeft(tail, " \t:：;；,，.-—>()[]（）【】")
	if idx := strings.IndexByte(tail, '\n'); idx >= 0 {
		tail = tail[:idx]
	}
	tail = strings.TrimSpace(tail)
	tail = strings.Trim(tail, "。.,，；;:：!！()[]（）【】")
	lower := strings.ToLower(tail)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func uniqueSubAgentClaimedVerificationCommands(items []subAgentClaimedVerificationCommand) []subAgentClaimedVerificationCommand {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]int, len(items))
	out := make([]subAgentClaimedVerificationCommand, 0, len(items))
	for _, item := range items {
		item.Command = strings.TrimSpace(item.Command)
		if item.Command == "" {
			continue
		}
		key := normalizeSubAgentCommandForEvidence(item.Command)
		if idx, ok := seen[key]; ok {
			out[idx].ClaimedPassed = out[idx].ClaimedPassed || item.ClaimedPassed
			out[idx].ClaimedFailed = out[idx].ClaimedFailed || item.ClaimedFailed
			continue
		}
		seen[key] = len(out)
		out = append(out, item)
	}
	return out
}

func compactSubAgentClaimedCommandList(commands []string, maxItems int) string {
	if len(commands) == 0 {
		return ""
	}
	if maxItems <= 0 || maxItems > len(commands) {
		maxItems = len(commands)
	}
	parts := make([]string, 0, maxItems+1)
	for _, command := range commands[:maxItems] {
		text := compactSubAgentCommandText(command)
		if text == "" {
			text = "<empty command>"
		}
		parts = append(parts, "`"+escapeSubAgentInlineCode(text)+"`")
	}
	if remaining := len(commands) - maxItems; remaining > 0 {
		parts = append(parts, fmt.Sprintf("还有 %d 条未展开", remaining))
	}
	return strings.Join(parts, ", ")
}

func normalizeSubAgentCommandForEvidence(command string) string {
	fields := shellCommandFields(strings.TrimSpace(command))
	return normalizeSubAgentCommandFieldsForEvidence(fields)
}

func normalizeSubAgentCommandForFailureResolution(command string) string {
	command = strings.TrimSpace(command)
	segments := shellCommandSegments(command)
	for i, segment := range segments {
		if len(segments) > 1 && !subAgentPriorSegmentsAreShellWrappers(segments, i) {
			continue
		}
		stripped := stripVerificationCommandPrefixes(segment)
		if len(stripped) == 0 || !isSubAgentVerificationCommandSegment(stripped) {
			continue
		}
		if fields := normalizeSubAgentCommandFieldsForEvidence(stripped); fields != "" {
			return fields
		}
	}
	fields := shellCommandFields(command)
	return normalizeSubAgentCommandFieldsForEvidence(fields)
}

func subAgentPriorSegmentsAreShellWrappers(segments [][]string, index int) bool {
	if index <= 0 {
		return true
	}
	for _, segment := range segments[:index] {
		if !subAgentSegmentIsShellWrapperPrefix(segment) && !subAgentSegmentIsDirectoryChangePrefix(segment) {
			return false
		}
	}
	return true
}

func subAgentSegmentIsDirectoryChangePrefix(segment []string) bool {
	if len(segment) == 0 {
		return false
	}
	return commandNameBase(segment[0]) == "cd"
}

func subAgentSegmentIsShellWrapperPrefix(segment []string) bool {
	if len(segment) == 0 {
		return true
	}
	if !isShellWrapperCommand(commandNameBase(segment[0])) {
		return false
	}
	for _, arg := range segment[1:] {
		arg = strings.ToLower(normalizeShellCommandToken(arg))
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "/") {
			continue
		}
		return false
	}
	return true
}

func normalizeSubAgentCommandFieldsForEvidence(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(fields))
	commandPosition := true
	for _, field := range fields {
		field = normalizeShellCommandToken(field)
		if field == "" {
			continue
		}
		if field == "&" && commandPosition {
			continue
		}
		if isShellCommandBoundary(field) {
			normalized = append(normalized, field)
			commandPosition = true
			continue
		}
		field = normalizeShellExecutableToken(field)
		if commandPosition {
			field = commandNameBase(field)
			commandPosition = false
		}
		if field != "" {
			normalized = append(normalized, field)
		}
	}
	return strings.ToLower(strings.Join(normalized, " "))
}

func subAgentSummaryMentionsAcceptanceVerification(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false
	}
	lower := strings.ToLower(summary)
	hasAcceptance := strings.Contains(lower, "acceptance") || strings.Contains(lower, "criteria") || strings.Contains(summary, "验收") || strings.Contains(summary, "标准")
	hasVerification := strings.Contains(lower, "verif") || strings.Contains(lower, "test") || strings.Contains(lower, "check") || strings.Contains(summary, "验证") || strings.Contains(summary, "检查") || strings.Contains(summary, "测试")
	return hasAcceptance && hasVerification
}

func subAgentSummaryReferencesAcceptanceCriterion(summary string, criteria []string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" || len(criteria) == 0 {
		return false
	}
	lowerSummary := strings.ToLower(summary)
	for i, criterion := range criteria {
		if strings.TrimSpace(criterion) == "" {
			continue
		}
		if !subAgentSummaryReferencesOneAcceptanceCriterion(lowerSummary, criterion, i+1) {
			return false
		}
	}
	return true
}

func subAgentSummaryReferencesOneAcceptanceCriterion(lowerSummary, criterion string, index int) bool {
	if subAgentSummaryReferencesAcceptanceIndex(lowerSummary, index) {
		return true
	}
	tokens := subAgentAcceptanceCriterionTokens(criterion)
	if len(tokens) == 0 {
		return false
	}
	matches := 0
	for _, token := range tokens {
		if subAgentSummaryContainsAcceptanceToken(lowerSummary, token) {
			matches++
		}
	}
	needed := 2
	if len(tokens) < needed {
		needed = len(tokens)
	}
	return matches >= needed
}

func subAgentSummaryContainsAcceptanceToken(lowerSummary, token string) bool {
	lowerSummary = strings.ToLower(strings.TrimSpace(lowerSummary))
	token = strings.ToLower(strings.TrimSpace(token))
	if lowerSummary == "" || token == "" {
		return false
	}
	if !subAgentAcceptanceTokenNeedsBoundary(token) {
		return strings.Contains(lowerSummary, token)
	}
	start := 0
	for {
		idx := strings.Index(lowerSummary[start:], token)
		if idx < 0 {
			return false
		}
		absolute := start + idx
		if subAgentSummaryPhraseHasBoundary(lowerSummary, token, absolute) {
			return true
		}
		start = absolute + len(token)
		if start >= len(lowerSummary) {
			return false
		}
	}
}

func subAgentAcceptanceTokenNeedsBoundary(token string) bool {
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func subAgentSummaryReferencesAcceptanceIndex(lowerSummary string, index int) bool {
	if index <= 0 {
		return false
	}
	idx := fmt.Sprint(index)
	chineseIdx := subAgentChineseIndex(index)
	markers := []string{
		"ac" + idx, "ac " + idx, "ac-" + idx, "ac#" + idx, "ac #" + idx,
		"criterion " + idx, "criterion #" + idx, "criteria " + idx, "criteria #" + idx,
		"acceptance " + idx, "acceptance #" + idx, "acceptance criterion " + idx, "acceptance criterion #" + idx,
		"标准" + idx, "标准 " + idx, "标准第" + idx, "标准第 " + idx, "标准第" + idx + "条", "标准第 " + idx + " 条",
		"验收" + idx, "验收 " + idx, "验收第" + idx, "验收第 " + idx, "验收第" + idx + "条", "验收第 " + idx + " 条",
		"第" + idx + "条", "第 " + idx + " 条",
		"(" + idx + ")", "（" + idx + "）", "#" + idx,
	}
	if chineseIdx != "" {
		markers = append(markers,
			"标准"+chineseIdx, "标准第"+chineseIdx, "标准第"+chineseIdx+"条",
			"验收"+chineseIdx, "验收第"+chineseIdx, "验收第"+chineseIdx+"条",
			"第"+chineseIdx+"条",
			"("+chineseIdx+")", "（"+chineseIdx+"）",
		)
	}
	for _, marker := range markers {
		if subAgentSummaryContainsAcceptanceIndexMarker(lowerSummary, marker) {
			return true
		}
	}
	return false
}

func subAgentSummaryContainsAcceptanceIndexMarker(lowerSummary, marker string) bool {
	lowerSummary = strings.ToLower(strings.TrimSpace(lowerSummary))
	marker = strings.ToLower(strings.TrimSpace(marker))
	if lowerSummary == "" || marker == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(lowerSummary[start:], marker)
		if idx < 0 {
			return false
		}
		absolute := start + idx
		after := absolute + len(marker)
		if (absolute == 0 || !isASCIIAlphaNumeric(lowerSummary[absolute-1])) &&
			(after >= len(lowerSummary) || !isASCIIAlphaNumeric(lowerSummary[after])) {
			return true
		}
		start = after
		if start >= len(lowerSummary) {
			return false
		}
	}
}

func subAgentChineseIndex(index int) string {
	switch index {
	case 1:
		return "一"
	case 2:
		return "二"
	case 3:
		return "三"
	case 4:
		return "四"
	case 5:
		return "五"
	case 6:
		return "六"
	case 7:
		return "七"
	case 8:
		return "八"
	case 9:
		return "九"
	case 10:
		return "十"
	default:
		return ""
	}
}

func subAgentAcceptanceCriterionTokens(criterion string) []string {
	criterion = strings.ToLower(strings.TrimSpace(criterion))
	if criterion == "" {
		return nil
	}
	parts := strings.FieldsFunc(criterion, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	var tokens []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || isSubAgentAcceptanceCriterionStopWord(part) {
			continue
		}
		if len([]rune(part)) < 2 && !subAgentTokenHasDigit(part) {
			continue
		}
		tokens = append(tokens, part)
	}
	return uniqueSubAgentStrings(tokens)
}

func isSubAgentAcceptanceCriterionStopWord(token string) bool {
	switch token {
	case "a", "an", "the", "and", "or", "to", "for", "of", "in", "on", "with", "by", "is", "are", "be", "can", "should", "must", "will", "when", "then", "that", "this", "it", "its":
		return true
	default:
		return false
	}
}

func subAgentTokenHasDigit(token string) bool {
	for _, r := range token {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func filterPostEditSubAgentDynamicTools(tools []CodingSubAgentDynamicToolResult, lastEditSeq uint64) []CodingSubAgentDynamicToolResult {
	if lastEditSeq == 0 || len(tools) == 0 {
		return tools
	}
	filtered := make([]CodingSubAgentDynamicToolResult, 0, len(tools))
	for _, tool := range tools {
		if tool.seq == 0 || tool.seq >= lastEditSeq {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func failedSubAgentDynamicTools(tools []CodingSubAgentDynamicToolResult) []CodingSubAgentDynamicToolResult {
	if len(tools) == 0 {
		return nil
	}
	failed := make([]CodingSubAgentDynamicToolResult, 0, len(tools))
	for _, tool := range tools {
		if !tool.Succeeded {
			failed = append(failed, tool)
		}
	}
	return failed
}

func unresolvedFailedSubAgentDynamicTools(tools []CodingSubAgentDynamicToolResult) []CodingSubAgentDynamicToolResult {
	if len(tools) == 0 {
		return nil
	}
	laterSucceeded := make(map[string]bool, len(tools))
	unresolvedReversed := make([]CodingSubAgentDynamicToolResult, 0)
	for i := len(tools) - 1; i >= 0; i-- {
		tool := tools[i]
		if subAgentDynamicToolIsSoftFailure(tool) {
			continue
		}
		key := normalizeSubAgentDynamicToolForEvidence(tool)
		if key == "" {
			if !tool.Succeeded {
				unresolvedReversed = append(unresolvedReversed, tool)
			}
			continue
		}
		if tool.Succeeded && !subAgentDynamicToolSuccessLooksEmpty(tool) {
			laterSucceeded[key] = true
			continue
		}
		if !laterSucceeded[key] {
			unresolvedReversed = append(unresolvedReversed, tool)
		}
	}
	unresolved := make([]CodingSubAgentDynamicToolResult, len(unresolvedReversed))
	for i := range unresolvedReversed {
		unresolved[len(unresolvedReversed)-1-i] = unresolvedReversed[i]
	}
	return unresolved
}

// Knowledge search is an optional, read-only context aid. Its backend may be
// unavailable or simply have no indexed material; that must not invalidate
// edits that have independent exploration, verification, and diff evidence.
// MCP and skill failures remain hard because they may represent the requested
// operation itself rather than an optional lookup.
func subAgentDynamicToolIsSoftFailure(tool CodingSubAgentDynamicToolResult) bool {
	return !tool.Succeeded && subAgentDynamicToolIsKnowledgeSearch(tool)
}

func normalizeSubAgentDynamicToolForEvidence(tool CodingSubAgentDynamicToolResult) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(tool.Tool+" "+tool.Name)), " "))
}

func subAgentDynamicToolSuccessLooksEmpty(tool CodingSubAgentDynamicToolResult) bool {
	return tool.Succeeded && subAgentDynamicToolInspectionOutputLooksEmpty(tool)
}

func summarizeFailedSubAgentDynamicToolWarning(tools []CodingSubAgentDynamicToolResult) string {
	if len(tools) == 0 {
		return "0 dynamic tool(s) failed"
	}
	detail := compactFailedSubAgentDynamicToolResults(tools)
	if len(tools) == 1 {
		return fmt.Sprintf("1 dynamic tool failed: %s", detail)
	}
	return fmt.Sprintf("%d dynamic tools failed: %s", len(tools), detail)
}

func compactFailedSubAgentDynamicToolResults(tools []CodingSubAgentDynamicToolResult) string {
	if len(tools) == 0 {
		return ""
	}
	tools = dedupeSubAgentDynamicToolFailuresForSummary(tools)
	limit := codingSubAgentFailedVerificationSummaryMax
	if len(tools) < limit {
		limit = len(tools)
	}
	selected := selectSubAgentDynamicToolFailuresForSummary(tools, limit)
	parts := make([]string, 0, limit+1)
	for _, tool := range selected {
		label := strings.TrimSpace(tool.Tool)
		if strings.TrimSpace(tool.Name) != "" {
			label += " " + strings.TrimSpace(tool.Name)
		}
		summary := commandResultDiagnosticLine(tool.Summary)
		if summary != "" {
			label += " -> " + summary
		}
		parts = append(parts, truncateRunesForSubAgent(label, codingSubAgentCommandTextMaxRunes))
	}
	if remaining := len(tools) - len(selected); remaining > 0 {
		parts = append(parts, fmt.Sprintf("... %d more", remaining))
	}
	return strings.Join(parts, "; ")
}

func dedupeSubAgentDynamicToolFailuresForSummary(tools []CodingSubAgentDynamicToolResult) []CodingSubAgentDynamicToolResult {
	if len(tools) < 2 {
		return tools
	}
	latestByKey := make(map[string]int, len(tools))
	for i, tool := range tools {
		latestByKey[subAgentDynamicToolFailureSummaryDedupeKey(tool, i)] = i
	}
	out := make([]CodingSubAgentDynamicToolResult, 0, len(latestByKey))
	for i, tool := range tools {
		key := subAgentDynamicToolFailureSummaryDedupeKey(tool, i)
		if latestByKey[key] == i {
			out = append(out, tool)
		}
	}
	return out
}

func subAgentDynamicToolFailureSummaryDedupeKey(tool CodingSubAgentDynamicToolResult, index int) string {
	target := normalizeSubAgentDynamicToolForEvidence(tool)
	if target == "" {
		target = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(tool.Tool)), " "))
	}
	diagnostic := strings.ToLower(strings.Join(strings.Fields(commandResultDiagnosticLine(tool.Summary)), " "))
	if diagnostic == "" {
		diagnostic = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(tool.Summary)), " "))
	}
	if target == "" && diagnostic == "" {
		return fmt.Sprintf("idx:%d", index)
	}
	return target + "\x00" + diagnostic
}

func selectSubAgentDynamicToolFailuresForSummary(tools []CodingSubAgentDynamicToolResult, limit int) []CodingSubAgentDynamicToolResult {
	if limit <= 0 || len(tools) == 0 {
		return nil
	}
	if len(tools) <= limit {
		out := make([]CodingSubAgentDynamicToolResult, len(tools))
		copy(out, tools)
		return out
	}
	selected := make(map[int]bool, limit)
	for i := len(tools) - 1; i >= 0 && len(selected) < limit; i-- {
		if isActionableCommandDiagnosticLine(commandResultDiagnosticLine(tools[i].Summary)) {
			selected[i] = true
		}
	}
	for i := len(tools) - 1; i >= 0 && len(selected) < limit; i-- {
		selected[i] = true
	}
	indices := make([]int, 0, len(selected))
	for i := range selected {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	out := make([]CodingSubAgentDynamicToolResult, 0, len(indices))
	for _, i := range indices {
		out = append(out, tools[i])
	}
	return out
}

func filterPostEditSubAgentCommands(commands []CodingSubAgentCommandResult, lastEditSeq uint64) []CodingSubAgentCommandResult {
	if lastEditSeq == 0 || len(commands) == 0 {
		return commands
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, cmd := range commands {
		if cmd.seq == 0 || cmd.seq >= lastEditSeq {
			filtered = append(filtered, cmd)
		}
	}
	return filtered
}
func filterGuardrailBlockedSubAgentCommands(commands []CodingSubAgentCommandResult, guardrails []CodingSubAgentGuardrailViolation) []CodingSubAgentCommandResult {
	if len(commands) == 0 || len(guardrails) == 0 {
		return commands
	}
	blocked := make(map[string]bool, len(guardrails))
	for _, violation := range guardrails {
		key := subAgentGuardrailBlockedCommandKey(violation.Command)
		if key != "" {
			blocked[key] = true
		}
	}
	if len(blocked) == 0 {
		return commands
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, command := range commands {
		if !blocked[subAgentGuardrailBlockedCommandKey(command.Command)] {
			filtered = append(filtered, command)
		}
	}
	return filtered
}

func unresolvedSubAgentGuardrailViolations(guardrails []CodingSubAgentGuardrailViolation, commands []CodingSubAgentCommandResult) []CodingSubAgentGuardrailViolation {
	if len(guardrails) == 0 {
		return nil
	}
	unresolved := make([]CodingSubAgentGuardrailViolation, 0, len(guardrails))
	for _, violation := range guardrails {
		if subAgentGuardrailViolationResolvedByCommandSuccess(violation, commands) {
			continue
		}
		unresolved = append(unresolved, violation)
	}
	return unresolved
}

func subAgentGuardrailViolationResolvedByCommandSuccess(violation CodingSubAgentGuardrailViolation, commands []CodingSubAgentCommandResult) bool {
	if len(commands) == 0 || !subAgentGuardrailViolationCanBeResolvedByCommandSuccess(violation) {
		return false
	}
	key := normalizeSubAgentCommandForFailureResolution(violation.Command)
	for _, command := range commands {
		if !command.Succeeded || subAgentCommandSuccessLooksEmpty(command) || !subAgentCommandHappenedAfterGuardrail(command, violation) {
			continue
		}
		commandKey := normalizeSubAgentCommandForFailureResolution(command.Command)
		if key != "" && commandKey == key {
			return true
		}
		if subAgentGuardrailViolationCanBeResolvedByLaterVerification(violation) && isSubAgentVerificationCommand(command.Command) {
			return true
		}
	}
	return false
}

func subAgentCommandHappenedAfterGuardrail(command CodingSubAgentCommandResult, violation CodingSubAgentGuardrailViolation) bool {
	return command.seq == 0 || violation.seq == 0 || command.seq > violation.seq
}

func subAgentGuardrailViolationCanBeResolvedByCommandSuccess(violation CodingSubAgentGuardrailViolation) bool {
	if violation.Category != codingSubAgentGuardrailCategoryCommand {
		return false
	}
	summary := strings.ToLower(violation.Summary)
	return strings.Contains(summary, "powershell command compatibility") ||
		strings.Contains(summary, "bash-only syntax") ||
		strings.Contains(summary, "powershell exception") ||
		strings.Contains(summary, "about_execution_policies") ||
		strings.Contains(summary, "running scripts is disabled") ||
		strings.Contains(summary, "无法加载文件") ||
		strings.Contains(summary, "禁止运行脚本")
}

func subAgentGuardrailViolationCanBeResolvedByLaterVerification(violation CodingSubAgentGuardrailViolation) bool {
	summary := strings.ToLower(violation.Summary)
	return strings.Contains(summary, "powershell exception") ||
		strings.Contains(summary, "about_execution_policies") ||
		strings.Contains(summary, "running scripts is disabled") ||
		strings.Contains(summary, "无法加载文件") ||
		strings.Contains(summary, "禁止运行脚本")
}

func subAgentGuardrailBlockedCommandKey(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if key := normalizeSubAgentCommandForFailureResolution(command); key != "" {
		return key
	}
	return command
}

func failedSubAgentCommands(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	failed := make([]CodingSubAgentCommandResult, 0)
	for _, cmd := range commands {
		if !cmd.Succeeded {
			failed = append(failed, cmd)
		}
	}
	return failed
}

// filterSoftFailures drops soft non-git diff and soft missing-path probes from
// a failed-command list so they never block quality status.
func filterSoftFailures(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return commands
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, cmd := range commands {
		if subAgentCommandIsSoftFailure(cmd) {
			continue
		}
		filtered = append(filtered, cmd)
	}
	return filtered
}

// filterSoftNonGitDiffSelfCheckFailures is kept for older call sites / tests.
func filterSoftNonGitDiffSelfCheckFailures(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	return filterSoftFailures(commands)
}

func countSoftNonGitDiffSelfCheckFailures(commands []CodingSubAgentCommandResult) int {
	count := 0
	for _, cmd := range commands {
		if subAgentCommandIsSoftNonGitDiffSelfCheckFailure(cmd) {
			count++
		}
	}
	return count
}

func countSoftInspectionProbeFailures(commands []CodingSubAgentCommandResult) int {
	count := 0
	for _, cmd := range commands {
		if subAgentCommandIsSoftInspectionProbeFailure(cmd) {
			count++
		}
	}
	return count
}

func subAgentCommandIsSoftNonGitDiffSelfCheckFailure(cmd CodingSubAgentCommandResult) bool {
	if cmd.Succeeded || !isSubAgentDiffSelfCheckCommand(cmd.Command) {
		return false
	}
	// Policy rejections are soft for unresolved-command accounting, but they are
	// not evidence that the remote tree is non-git.
	if subAgentCommandIsSoftSilencedGitSelfCheckRejection(cmd) {
		return false
	}
	text := strings.TrimSpace(cmd.Command + "\n" + cmd.Summary)
	if subAgentGitDiffUnavailableBecauseNonGit(text) {
		return true
	}
	// Agents often redirect stderr (2>/dev/null) on git self-checks, so the
	// "not a git repository" marker is lost and only EXIT: 128 / SSH wrapper
	// noise remains. Treat that as a soft non-git skip so it does not hard-fail
	// an otherwise successful compile/run step.
	return subAgentDiffSelfCheckLooksLikeSilencedNonGitFailure(cmd)
}

// subAgentCommandIsSoftSilencedGitSelfCheckRejection reports policy blocks that
// told the agent to re-run git status/diff without /dev/null. These must not
// remain as unresolved hard failures after a correct unsilenced retry.
func subAgentCommandIsSoftSilencedGitSelfCheckRejection(cmd CodingSubAgentCommandResult) bool {
	if cmd.Succeeded || !isSubAgentDiffSelfCheckCommand(cmd.Command) {
		return false
	}
	text := strings.ToLower(cmd.Summary)
	return strings.Contains(text, "拒绝执行被重定向静默") ||
		strings.Contains(text, "被重定向静默的 git 自检") ||
		(strings.Contains(text, "2>/dev/null") && strings.Contains(text, "保留 fatal"))
}

func subAgentCommandIsSoftFailure(cmd CodingSubAgentCommandResult) bool {
	return subAgentCommandIsSoftNonGitDiffSelfCheckFailure(cmd) ||
		subAgentCommandIsSoftInspectionProbeFailure(cmd) ||
		subAgentCommandIsSoftSilencedGitSelfCheckRejection(cmd)
}

func unresolvedFailedSubAgentCommands(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	laterSucceeded := make(map[string]bool, len(commands))
	laterVerificationSucceeded := false
	unresolvedReversed := make([]CodingSubAgentCommandResult, 0)
	for i := len(commands) - 1; i >= 0; i-- {
		cmd := commands[i]
		key := subAgentCommandFailureResolutionKey(cmd)
		if key == "" {
			if !cmd.Succeeded && !subAgentCommandIsSoftFailure(cmd) && !(laterVerificationSucceeded && subAgentCommandFailureCanBeResolvedByLaterVerification(cmd)) {
				unresolvedReversed = append(unresolvedReversed, cmd)
			}
			continue
		}
		if cmd.Succeeded && !subAgentCommandSuccessLooksEmpty(cmd) {
			laterSucceeded[key] = true
			if isSubAgentVerificationCommand(cmd.Command) {
				laterVerificationSucceeded = true
			}
			continue
		}
		if !cmd.Succeeded && laterVerificationSucceeded && subAgentCommandFailureCanBeResolvedByLaterVerification(cmd) {
			continue
		}
		if !laterSucceeded[key] && !subAgentCommandIsSoftFailure(cmd) {
			unresolvedReversed = append(unresolvedReversed, cmd)
		}
	}
	unresolved := make([]CodingSubAgentCommandResult, len(unresolvedReversed))
	for i := range unresolvedReversed {
		unresolved[len(unresolvedReversed)-1-i] = unresolvedReversed[i]
	}
	return unresolved
}

func subAgentCommandFailureResolutionKey(cmd CodingSubAgentCommandResult) string {
	key := normalizeSubAgentCommandForFailureResolution(cmd.Command)
	if key == "" {
		return ""
	}
	workingDir := normalizeSubAgentWorkingDirForEvidence(cmd.WorkingDir)
	if workingDir == "" {
		return key
	}
	return key + "\x00" + workingDir
}

func subAgentCommandFailureCanBeResolvedByLaterVerification(cmd CodingSubAgentCommandResult) bool {
	if cmd.Succeeded || isSubAgentVerificationCommand(cmd.Command) {
		return false
	}
	if subAgentDiagnosticProbeFailureResultLooksHard(cmd.Summary) {
		return false
	}
	command := strings.ToLower(strings.Join(strings.Fields(cmd.Command), " "))
	if command == "" {
		return false
	}
	return subAgentDiagnosticProbeCommand(command)
}

func subAgentDiagnosticProbeFailureResultLooksHard(result string) bool {
	lower := strings.ToLower(strings.Join(strings.Fields(result), " "))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"assert",
		"access denied",
		"build stopped",
		"check(",
		"check (",
		"expected",
		"fatal error",
		"fail at",
		"failed test",
		"linker command failed",
		"lnk",
		"ninja:",
		"panic:",
		"permission denied",
		"pytest",
		"test failed",
		"traceback",
		"undefined reference",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func subAgentDiagnosticProbeCommand(command string) bool {
	segments := shellCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		if !subAgentDiagnosticProbeCommandSegment(segment) {
			return false
		}
	}
	return true
}

func subAgentDiagnosticProbeCommandSegment(segment []string) bool {
	segment = stripVerificationCommandPrefixes(segment)
	if len(segment) == 0 {
		return false
	}
	cmd := commandNameBase(segment[0])
	args := segment[1:]
	if cmd == "echo" {
		return subAgentDiagnosticEchoSeparator(args)
	}
	if subAgentDiagnosticBareProbeTool(cmd) && subAgentDiagnosticProbeArgsAreOnlyRedirection(args) {
		return true
	}
	if subAgentDiagnosticBareProbeTool(cmd) && subAgentDiagnosticCompilerProbeArgs(args) {
		return true
	}
	if cmd == "dir" {
		return subAgentDiagnosticProbeArgsContain(args, "/s") && subAgentDiagnosticProbeArgsContain(args, "/b")
	}
	switch cmd {
	case "get-command", "where", "where.exe", "vswhere", "get-childitem", "test-path", "select-object":
		return true
	}
	return subAgentDiagnosticVersionProbeArgs(args)
}

func subAgentDiagnosticVersionProbeArgs(args []string) bool {
	sawVersionArg := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		arg = strings.TrimSpace(normalizeShellCommandToken(arg))
		if arg == "" || arg == "2>&1" {
			continue
		}
		if consumes := subAgentDiagnosticProbeSecretArgConsumesValue(arg); consumes >= 0 {
			i += consumes
			continue
		}
		switch arg {
		case "--version", "-version", "/?":
			sawVersionArg = true
			continue
		}
		return false
	}
	return sawVersionArg
}

func subAgentDiagnosticCompilerProbeArgs(args []string) bool {
	sawProbeArg := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		arg = strings.TrimSpace(normalizeShellCommandToken(arg))
		if arg == "" || arg == "2>&1" {
			continue
		}
		if consumes := subAgentDiagnosticProbeSecretArgConsumesValue(arg); consumes >= 0 {
			i += consumes
			continue
		}
		switch arg {
		case "--version", "-v", "-print-search-dirs":
			sawProbeArg = true
			continue
		}
		return false
	}
	return sawProbeArg
}

func subAgentDiagnosticProbeSecretArgConsumesValue(arg string) int {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		return -1
	}
	for _, prefix := range []string{"--", "/"} {
		if !strings.HasPrefix(arg, prefix) {
			continue
		}
		flag := strings.TrimPrefix(arg, prefix)
		for _, name := range []string{"api-key", "api_key", "access-token", "access_token", "refresh-token", "refresh_token", "token", "password", "passwd", "secret"} {
			if flag == name {
				return 1
			}
			if strings.HasPrefix(flag, name+"=") || strings.HasPrefix(flag, name+":") {
				return 0
			}
		}
	}
	return -1
}

func subAgentDiagnosticBareProbeTool(cmd string) bool {
	switch cmd {
	case "cc", "gcc", "g++", "c++", "clang", "clang++", "cl":
		return true
	}
	return false
}

func subAgentDiagnosticProbeArgsAreOnlyRedirection(args []string) bool {
	for _, arg := range args {
		token := strings.TrimSpace(normalizeShellCommandToken(arg))
		if token == "" || token == "2>&1" {
			continue
		}
		return false
	}
	return true
}

func subAgentDiagnosticEchoSeparator(args []string) bool {
	if len(args) == 0 {
		return true
	}
	text := strings.Trim(strings.Join(args, " "), `"'`)
	if text == "" {
		return true
	}
	// Decorative inventory banners only: "=== g++ ===", "---versions---", "***".
	// Plain prose like "not-a-separator" or "done" must NOT count as a diagnostic
	// separator, or agents could hide non-probe work inside version inventory chains.
	if !(strings.Contains(text, "===") || strings.Contains(text, "---") || strings.Contains(text, "***")) {
		// Pure punctuation separators: "---", "===", "...."
		onlyDecor := false
		for _, r := range text {
			switch r {
			case '-', '=', '*', '.', ' ', '\t':
				onlyDecor = true
			default:
				return false
			}
		}
		return onlyDecor
	}
	// Reject shell metacharacters so echo is not used to smuggle real work.
	for _, r := range text {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsSpace(r):
			continue
		case r == '-' || r == '=' || r == '_' || r == '.' || r == '+' || r == ':' || r == '/' || r == '\\' || r == '*':
			continue
		default:
			return false
		}
	}
	return true
}

func subAgentDiagnosticProbeArgsContain(args []string, target string) bool {
	for _, arg := range args {
		if strings.TrimSpace(normalizeShellCommandToken(arg)) == target {
			return true
		}
	}
	return false
}

func normalizeSubAgentWorkingDirForEvidence(workingDir string) string {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		return ""
	}
	normalized := filepath.ToSlash(filepath.Clean(workingDir))
	if subAgentWorkingDirLooksWindowsAbsolute(normalized) {
		return strings.ToLower(normalized)
	}
	return normalized
}

func subAgentWorkingDirLooksWindowsAbsolute(path string) bool {
	if strings.HasPrefix(path, "//") {
		return true
	}
	return len(path) >= 3 && path[1] == ':' && path[2] == '/' && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z'))
}

func subAgentCommandSuccessLooksEmpty(cmd CodingSubAgentCommandResult) bool {
	return cmd.Succeeded && subAgentVerificationOutputLooksEmpty(cmd)
}

func summarizeFailedSubAgentCommandWarning(commands []CodingSubAgentCommandResult) string {
	if len(commands) == 0 {
		return ""
	}
	command := failedSubAgentCommandWithBestDiagnostic(commands)
	text := fmt.Sprintf("%d command(s) failed", len(commands))
	first := strings.TrimSpace(command.Command)
	if first != "" {
		text += ": " + truncateRunesForSubAgent(first, 160)
	}
	if summary := commandResultDiagnosticLine(command.Summary); summary != "" {
		text += " -> " + truncateRunesForSubAgent(summary, codingSubAgentCommandOutputLineMaxRunes)
	}
	return text
}

func failedSubAgentCommandWithBestDiagnostic(commands []CodingSubAgentCommandResult) CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return CodingSubAgentCommandResult{}
	}
	for i := len(commands) - 1; i >= 0; i-- {
		command := commands[i]
		if isActionableCommandDiagnosticLine(commandResultDiagnosticLine(command.Summary)) {
			return command
		}
	}
	return commands[len(commands)-1]
}

func isActionableCommandDiagnosticLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || isCodingToolCommandStatusLine(line) {
		return false
	}
	return true
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

func compactSubAgentCommandList(commands []string, maxItems int) string {
	commands = uniqueSortedSubAgentStrings(commands)
	if len(commands) == 0 {
		return ""
	}
	shown := len(commands)
	if maxItems > 0 && shown > maxItems {
		shown = maxItems
	}
	parts := make([]string, 0, shown+1)
	for _, command := range commands[:shown] {
		parts = append(parts, compactSubAgentCommandText(command))
	}
	if remaining := len(commands) - shown; remaining > 0 {
		parts = append(parts, fmt.Sprintf("还有 %d 条命令未展开", remaining))
	}
	return strings.Join(parts, ", ")
}

func compactSubAgentSearchText(query string) string {
	return truncateRunesForSubAgent(strings.TrimSpace(query), codingSubAgentSearchTextMaxRunes)
}

func isSubAgentDiffSelfCheckCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return false
	}
	for _, segment := range shellCommandSegments(normalized) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) < 2 || commandNameBase(segment[0]) != "git" {
			continue
		}
		switch segment[1] {
		case "diff", "status":
			return true
		case "rev-parse":
			// Used as a non-destructive "is this a git work tree?" probe before
			// status/diff self-check on remote workspaces.
			for _, arg := range segment[2:] {
				if normalizeShellExecutableToken(arg) == "--is-inside-work-tree" {
					return true
				}
			}
		}
	}
	return false
}

// isSubAgentDiffSelfCheckContentCommand reports status/diff probes that actually
// inspect the working tree. git rev-parse --is-inside-work-tree is only a
// repository probe and must not alone satisfy the remote diff gate.
func isSubAgentDiffSelfCheckContentCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return false
	}
	for _, segment := range shellCommandSegments(normalized) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) < 2 || commandNameBase(segment[0]) != "git" {
			continue
		}
		if segment[1] == "diff" || segment[1] == "status" {
			return true
		}
	}
	return false
}

func escapeSubAgentInlineCode(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}

// Scaffold / implement keyword tables for plan-step verification relaxation.
// Kept package-level so tests and both remote/local gates share one source of truth.
var (
	// codingPlanStrongScaffoldKeywords alone are enough to mark structure-only work.
	codingPlanStrongScaffoldKeywords = []string{
		"项目结构", "目录结构", "项目骨架", "创建项目结构", "创建目录",
		"scaffold", "skeleton", "project structure", "init structure",
		"create structure", "project layout", "目录布局", "骨架",
	}
	// codingPlanWeakScaffoldKeywords need a structure hint in the same focus text
	// (avoids relaxing "初始化数据库" / "搭建服务器" as scaffold).
	codingPlanWeakScaffoldKeywords = []string{
		"初始化项目", "初始化", "搭建项目", "搭建",
		"create project", "set up project", "setup project", "bootstrap",
	}
	// codingPlanStructureHints disambiguate soft build / weak scaffold words.
	codingPlanStructureHints = []string{
		"结构", "目录", "scaffold", "skeleton", "骨架", "structure",
		"layout", "src/", "include/", "cmake", "makefile",
	}
	// codingPlanHardImplementKeywords always keep full verification gates when
	// present in the step title (or non-scaffold-title description).
	// Prefer multi-char phrases so "文件编码" / "编码规范" do not false-reject scaffold.
	codingPlanHardImplementKeywords = []string{
		"implement", "实现", "编码实现", "编写代码",
		"编译", "cmake --build", "go build", "npm run build", "cargo build",
		"验收", "typecheck",
		"运行并", "run and", "运行验证",
		"修复", "fix bug", "fix the", "bugfix",
	}
	// codingPlanSoftVerifyKeywords: "验证"/"lint" appear in structure checks;
	// only reject when the title is not clearly structure-scaffold.
	codingPlanSoftVerifyKeywords = []string{
		"verify", "验证", "lint",
	}
	// codingPlanSoftBuildKeywords are ambiguous (e.g. 构建目录 vs 构建可执行文件).
	codingPlanSoftBuildKeywords = []string{
		"build", "构建", "make ", "compile",
	}
	// codingPlanSoftTestKeywords: "test" alone is too broad; require clear intent.
	codingPlanSoftTestKeywords = []string{
		"测试", "unit test", "run tests", "pytest", "go test", "npm test", "jest",
	}
)

func codingPlanTextContainsAny(blob string, keywords []string) bool {
	for _, kw := range keywords {
		if kw != "" && strings.Contains(blob, kw) {
			return true
		}
	}
	return false
}

func codingPlanFocusHasScaffoldSignal(focus string) bool {
	if codingPlanTextContainsAny(focus, codingPlanStrongScaffoldKeywords) {
		return true
	}
	// Weak words only count when paired with structure hints.
	return codingPlanTextContainsAny(focus, codingPlanWeakScaffoldKeywords) &&
		codingPlanTextContainsAny(focus, codingPlanStructureHints)
}

func codingPlanTitleBlocksScaffoldRelaxation(title string) bool {
	if title == "" {
		return false
	}
	if codingPlanTextContainsAny(title, codingPlanHardImplementKeywords) {
		return true
	}
	if codingPlanTextContainsAny(title, codingPlanSoftTestKeywords) {
		return true
	}
	// "验证"/"lint" keep full gates unless the title is clearly structure work.
	if codingPlanTextContainsAny(title, codingPlanSoftVerifyKeywords) &&
		!codingPlanFocusHasScaffoldSignal(title) {
		return true
	}
	// Soft build words: allow "构建项目目录结构", reject bare "构建项目".
	if codingPlanTextContainsAny(title, codingPlanSoftBuildKeywords) &&
		!codingPlanTextContainsAny(title, codingPlanStructureHints) {
		return true
	}
	return false
}

// codingPlanStepIsStructureScaffold reports whether a multi-step plan step is
// structure/scaffold/init only. Those steps intentionally create incomplete
// project skeletons and must not be failed for missing build/test/lint
// verification — implementation and compile belong to later plan steps.
//
// Only title + description of the CURRENT step are considered (not the full
// prompt that may list later-step titles in an outline).
func codingPlanStepIsStructureScaffold(title, description string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	description = strings.ToLower(strings.TrimSpace(description))
	if title == "" && description == "" {
		return false
	}
	focus := title
	if description != "" {
		if focus != "" {
			focus += "\n"
		}
		focus += description
	}
	if !codingPlanFocusHasScaffoldSignal(focus) {
		return false
	}
	if codingPlanTitleBlocksScaffoldRelaxation(title) {
		return false
	}
	// Title itself is scaffold — description may casually mention later work.
	if codingPlanFocusHasScaffoldSignal(title) {
		return true
	}
	// Description-only scaffold: apply the same reject rules on description.
	if codingPlanTitleBlocksScaffoldRelaxation(description) {
		return false
	}
	return true
}

// resolveCodingPlanStepFocus picks title/description for scaffold detection.
// Prefers an embedded [Plan step Tn/N] header in fullTask (remote prompts),
// then falls back to the explicit title/description fields (local TaskItem).
func resolveCodingPlanStepFocus(title, description, fullTask string) (stepTitle, stepDesc string) {
	stepTitle = strings.TrimSpace(title)
	stepDesc = strings.TrimSpace(description)
	for _, candidate := range []string{fullTask, description, title} {
		if t, d, ok := extractPlanStepFocusFromTaskText(candidate); ok {
			return t, d
		}
	}
	return stepTitle, stepDesc
}

// extractPlanStepFocusFromTaskText pulls the current plan-step title and
// description from a remote/local plan-step user prompt. Returns ok=false when
// the text is not a multi-step plan step (free-form tasks keep full gates).
func extractPlanStepFocusFromTaskText(taskText string) (title, description string, ok bool) {
	taskText = strings.TrimSpace(taskText)
	if taskText == "" {
		return "", "", false
	}
	lower := strings.ToLower(taskText)
	const marker = "[plan step "
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return "", "", false
	}
	// Align to the original string (case may differ for the marker).
	rest := taskText[idx:]
	// Header: [Plan step T2/6] Title...
	lineEnd := strings.IndexAny(rest, "\r\n")
	header := rest
	body := ""
	if lineEnd >= 0 {
		header = rest[:lineEnd]
		body = strings.TrimLeft(rest[lineEnd:], "\r\n")
	}
	// Title after closing bracket.
	br := strings.Index(header, "]")
	if br < 0 || br+1 >= len(header) {
		return "", "", false
	}
	title = strings.TrimSpace(header[br+1:])
	if title == "" {
		return "", "", false
	}
	// Description ends at the standard plan-step instruction block or next section.
	bodyLower := strings.ToLower(body)
	best := -1
	for _, m := range []string{
		"you are executing plan step ",
		"## overall user request",
		"plan outline ",
		"[session continuity",
	} {
		if i := strings.Index(bodyLower, m); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if best >= 0 {
		description = strings.TrimSpace(body[:best])
	} else {
		description = strings.TrimSpace(body)
	}
	return title, description, true
}

// maybeRelaxScaffoldVerification downgrades a MISSING build/test verification
// status to NOT_NEEDED for structure-scaffold plan steps. FAILED verification
// (actual build/test errors) is never relaxed.
func maybeRelaxScaffoldVerification(
	title, description string,
	verificationStatus codingSubAgentQualityStatus,
	verificationSummary string,
) (codingSubAgentQualityStatus, string) {
	if verificationStatus != codingSubAgentQualityMissing {
		return verificationStatus, verificationSummary
	}
	if !codingPlanStepIsStructureScaffold(title, description) {
		return verificationStatus, verificationSummary
	}
	return codingSubAgentQualityNotNeeded,
		"plan structure/scaffold step: build/test/lint verification deferred to later implement/build steps; structure confirmed without requiring compile of incomplete stubs"
}

// maybeRelaxDeferredPlanStepVerification permits an implementation-only plan
// step to defer executable verification when the same plan explicitly assigns
// that work to a later build/test step.  This keeps the quality gate aligned
// with the per-step instruction without weakening ordinary implementation
// tasks: post-edit confirmation is still required by the caller, and an
// actual failed verification command is never relaxed.
func maybeRelaxDeferredPlanStepVerification(
	title, description, fullTask string,
	verificationStatus codingSubAgentQualityStatus,
	verificationSummary string,
) (codingSubAgentQualityStatus, string) {
	verificationStatus, verificationSummary = maybeRelaxScaffoldVerification(title, description, verificationStatus, verificationSummary)
	if verificationStatus != codingSubAgentQualityMissing {
		return verificationStatus, verificationSummary
	}
	focus := strings.ToLower(strings.TrimSpace(title + "\n" + description))
	if !codingPlanTextContainsAny(focus, []string{"implement", "实现", "编码实现", "编写代码"}) {
		return verificationStatus, verificationSummary
	}
	if !codingPlanHasLaterBuildOrTestStep(fullTask) {
		return verificationStatus, verificationSummary
	}
	return codingSubAgentQualityNotNeeded,
		"plan implementation step: build/test verification explicitly deferred to a later compile/build/test step; post-edit file confirmation passed"
}

// codingPlanHasLaterBuildOrTestStep recognizes the compact `- T4: 编译项目`
// lines included in a remote plan-step prompt.  It deliberately requires a
// numbered current header and a later numbered outline entry, so free-form
// tasks and plans without a dedicated verifier keep the normal full gate.
func codingPlanHasLaterBuildOrTestStep(fullTask string) bool {
	current, ok := codingPlanStepNumberFromHeader(fullTask)
	if !ok {
		return false
	}
	for _, line := range strings.Split(strings.ToLower(fullTask), "\n") {
		step, title, ok := codingPlanStepNumberFromOutlineLine(line)
		if !ok || step <= current {
			continue
		}
		if codingPlanTextContainsAny(title, []string{
			"build", "compile", "编译", "构建", "test", "测试", "lint", "typecheck",
		}) {
			return true
		}
	}
	return false
}

func codingPlanStepNumberFromHeader(fullTask string) (int, bool) {
	lower := strings.ToLower(fullTask)
	idx := strings.Index(lower, "[plan step t")
	if idx < 0 {
		return 0, false
	}
	return codingPlanLeadingStepNumber(lower[idx+len("[plan step t"):])
}

func codingPlanStepNumberFromOutlineLine(line string) (int, string, bool) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
	if !strings.HasPrefix(line, "t") {
		return 0, "", false
	}
	step, ok := codingPlanLeadingStepNumber(line[1:])
	if !ok {
		return 0, "", false
	}
	colon := strings.Index(line, ":")
	if colon < 0 {
		return 0, "", false
	}
	return step, strings.TrimSpace(line[colon+1:]), true
}

func codingPlanLeadingStepNumber(text string) (int, bool) {
	n := 0
	found := false
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			break
		}
		n = n*10 + int(ch-'0')
		found = true
	}
	return n, found
}

func summarizeSubAgentVerification(filesModified []string, commands []CodingSubAgentCommandResult, lastEditSeq uint64) (codingSubAgentQualityStatus, string) {
	if len(filesModified) == 0 {
		return codingSubAgentQualityNotNeeded, "未检测到文件修改，跳过命令验证要求。"
	}
	allVerificationCommands := filterSubAgentVerificationCommands(commands)
	unsafeVerificationCommands := filterUnsafeSubAgentVerificationCommands(commands)
	freshUnsafeVerificationCommands := filterFreshUnsafeSubAgentVerificationCommands(commands, lastEditSeq)
	verificationCommands := filterFreshSubAgentVerificationCommands(commands, lastEditSeq)
	if len(freshUnsafeVerificationCommands) > 0 {
		return codingSubAgentQualityMissing, fmt.Sprintf("verification command used failure-suppressing shell syntax (%d command(s)); rerun test/build/lint/typecheck without || fallback, pipe filters, output redirection, or extra commands after the verifier", len(freshUnsafeVerificationCommands))
	}
	if len(verificationCommands) == 0 {
		if len(commands) == 0 {
			return codingSubAgentQualityMissing, "file changes detected but no bash verification command ran"
		}
		if len(allVerificationCommands) > 0 {
			return codingSubAgentQualityMissing, fmt.Sprintf("verification ran before the final edit (%d command(s)); rerun test/build/lint/typecheck after editing", len(allVerificationCommands))
		}
		if len(unsafeVerificationCommands) > 0 {
			return codingSubAgentQualityMissing, fmt.Sprintf("verification command used failure-suppressing shell syntax (%d command(s)); rerun test/build/lint/typecheck without || fallback, pipe filters, output redirection, or extra commands after the verifier", len(unsafeVerificationCommands))
		}
		return codingSubAgentQualityMissing, fmt.Sprintf("file changes detected; ran %d bash command(s), but none were test/build/lint/typecheck verification", len(commands))
	}
	var failed []CodingSubAgentCommandResult
	for _, cmd := range unresolvedFailedSubAgentCommands(verificationCommands) {
		if !cmd.Succeeded {
			failed = append(failed, cmd)
		}
	}
	if len(failed) > 0 {
		if len(failed) == 1 {
			return codingSubAgentQualityFailed, fmt.Sprintf("有 1 条验证命令未通过：%s", compactFailedVerificationCommandResults(failed))
		}
		return codingSubAgentQualityFailed, fmt.Sprintf("有 %d 条验证命令未通过：%s", len(failed), compactFailedVerificationCommandResults(failed))
	}
	// A failed command can be non-blocking for command accounting (for example,
	// `tree` missing before a chained build).  It still is not evidence that the
	// build/test portion ran: `&&` short-circuits the rest of that shell line.
	// Keep scaffold steps eligible for their existing MISSING -> NOT_NEEDED
	// relaxation, while implementation/build steps must rerun verification as a
	// standalone command.
	if !hasSuccessfulSubAgentVerificationCommand(verificationCommands) {
		return codingSubAgentQualityMissing, "verification command did not reach a successful build/test check; rerun the verifier separately without optional display/probe commands in the same && chain"
	}
	var empty []CodingSubAgentCommandResult
	for _, cmd := range verificationCommands {
		if cmd.Succeeded && subAgentVerificationOutputLooksEmpty(cmd) {
			empty = append(empty, cmd)
		}
	}
	if len(empty) > 0 {
		if len(empty) == 1 {
			return codingSubAgentQualityFailed, fmt.Sprintf("有 1 条验证命令未实际执行测试或检查：%s", compactSubAgentVerificationCommandList(empty))
		}
		return codingSubAgentQualityFailed, fmt.Sprintf("有 %d 条验证命令未实际执行测试或检查：%s", len(empty), compactSubAgentVerificationCommandList(empty))
	}
	var successful []CodingSubAgentCommandResult
	for _, cmd := range verificationCommands {
		if cmd.Succeeded {
			successful = append(successful, cmd)
		}
	}
	return codingSubAgentQualityPassed, fmt.Sprintf("已运行 %d 条有效 bash 验证命令，未检测到未解决错误：%s", len(successful), compactSubAgentVerificationCommandList(successful))
}

func hasSuccessfulSubAgentVerificationCommand(commands []CodingSubAgentCommandResult) bool {
	for _, cmd := range commands {
		if cmd.Succeeded {
			return true
		}
	}
	return false
}

func compactSubAgentVerificationCommandList(commands []CodingSubAgentCommandResult) string {
	if len(commands) == 0 {
		return ""
	}
	limit := codingSubAgentFailedVerificationSummaryMax
	if len(commands) < limit {
		limit = len(commands)
	}
	parts := make([]string, 0, limit+1)
	for _, command := range commands[:limit] {
		text := compactSubAgentCommandText(command.Command)
		if text == "" {
			text = "<empty command>"
		}
		parts = append(parts, "`"+escapeSubAgentInlineCode(text)+"`")
	}
	if remaining := len(commands) - limit; remaining > 0 {
		parts = append(parts, fmt.Sprintf("还有 %d 条未展开", remaining))
	}
	return strings.Join(parts, ", ")
}

func countFreshSubAgentVerificationAttempts(commands []CodingSubAgentCommandResult, lastEditSeq uint64) int {
	freshClean := filterFreshSubAgentVerificationCommands(commands, lastEditSeq)
	freshUnsafe := filterFreshUnsafeSubAgentVerificationCommands(commands, lastEditSeq)
	return len(freshClean) + len(freshUnsafe)
}

func filterFreshSubAgentVerificationCommands(commands []CodingSubAgentCommandResult, lastEditSeq uint64) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, cmd := range commands {
		if !isSubAgentVerificationCommand(cmd.Command) {
			continue
		}
		if lastEditSeq > 0 && cmd.seq > 0 && cmd.seq < lastEditSeq {
			continue
		}
		filtered = append(filtered, cmd)
	}
	return filtered
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

func filterFreshUnsafeSubAgentVerificationCommands(commands []CodingSubAgentCommandResult, lastEditSeq uint64) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, cmd := range commands {
		if !isUnsafeSubAgentVerificationCommand(cmd.Command) {
			continue
		}
		if lastEditSeq > 0 && cmd.seq > 0 && cmd.seq < lastEditSeq {
			continue
		}
		filtered = append(filtered, cmd)
	}
	return filtered
}

func filterUnsafeSubAgentVerificationCommands(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) == 0 {
		return nil
	}
	filtered := make([]CodingSubAgentCommandResult, 0, len(commands))
	for _, cmd := range commands {
		if isUnsafeSubAgentVerificationCommand(cmd.Command) {
			filtered = append(filtered, cmd)
		}
	}
	return filtered
}

func isUnsafeSubAgentVerificationCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	// Shell safety rules apply to actual build/test/lint/typecheck (or project
	// acceptance-run) commands.  A git status/diff self-check may legitimately
	// use a semicolon to run its two read-only probes, and must be handled by the
	// dedicated diff-self-check gate instead of poisoning verification evidence.
	return normalized != "" && commandContainsSubAgentVerificationSegment(normalized) && suppressesVerificationFailure(normalized)
}

func commandContainsSubAgentVerificationSegment(command string) bool {
	for _, segment := range shellCommandSegments(command) {
		if isSubAgentVerificationCommandSegment(segment) {
			return true
		}
	}
	return false
}

func isSubAgentVerificationCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" || suppressesVerificationFailure(normalized) {
		return false
	}
	for _, segment := range shellCommandSegments(normalized) {
		if isSubAgentVerificationCommandSegment(segment) {
			return true
		}
	}
	return false
}

func suppressesVerificationFailure(normalizedCommand string) bool {
	fields := shellCommandFields(normalizedCommand)
	var segment []string
	sawVerification := false
	flushSegment := func() {
		if isSubAgentVerificationCommandSegment(segment) {
			sawVerification = true
		}
		segment = nil
	}
	for i := 0; i < len(fields); i++ {
		token := normalizeShellCommandToken(fields[i])
		if token == "" {
			continue
		}
		if shellCommandStartsAfterToken(token, segment) {
			flushSegment()
			if sawVerification && token == "||" && i+1 < len(fields) {
				return true
			}
			if sawVerification && token == "|" && i+1 < len(fields) {
				return true
			}
			if sawVerification && token == "&&" && i+1 < len(fields) {
				nextSegment := commandSegmentFields(fields[i+1:])
				if !isSubAgentVerificationCommandSegment(nextSegment) {
					return true
				}
			}
			if sawVerification && token == "&" && i+1 < len(fields) {
				return true
			}
			if sawVerification && token == ";" && i+1 < len(fields) {
				return true
			}
			continue
		}
		if isShellVerificationOutputRedirectionToken(token) {
			if sawVerification || isSubAgentVerificationCommandSegment(segment) {
				return true
			}
		}
		segment = append(segment, token)
	}
	flushSegment()
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
		if shellCommandStartsAfterToken(token, current) {
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
	// Ignore pure shell redirections such as 2>&1 so `make 2>&1` / `g++ ... 2>&1`
	// still count as build/compile verification (common on remote ssh_bash).
	args := stripShellRedirectionOnlyArgs(segment[1:])
	if hasInteractiveVerificationArg(args) {
		return false
	}
	if cmd == "test" && subAgentCommandTokenLooksLikePath(segment[0]) {
		return false
	}
	switch cmd {
	case "go":
		return goRunsVerification(args)
	case "cargo":
		return cargoRunsVerification(args)
	case "swift":
		return swiftRunsVerification(args)
	case "zig":
		return zigRunsVerification(args)
	case "stack":
		return stackRunsVerification(args)
	case "cabal":
		return cabalRunsVerification(args)
	case "lein":
		return leinRunsVerification(args)
	case "clojure", "clj":
		return clojureRunsVerification(args)
	case "bb":
		return babashkaRunsVerification(args)
	case "sbt":
		return sbtRunsVerification(args)
	case "mill":
		return millRunsVerification(args)
	case "dune":
		return duneRunsVerification(args)
	case "opam":
		return opamRunsVerification(args)
	case "npm", "pnpm", "yarn":
		return packageManagerRunsVerification(args)
	case "npx", "pnpx", "yarnx":
		return verificationRunnerCommandFromArgs(args)
	case "corepack":
		return corepackRunsVerification(args)
	case "node":
		// node --test / node -e suites, or running a project script for acceptance.
		if nodeRunsVerification(args) {
			return true
		}
		return len(args) >= 1 && subAgentCommandLooksLikeRunnableScript(args[0], ".js", ".mjs", ".cjs", ".ts")
	case "bun":
		return bunRunsVerification(args)
	case "deno":
		return denoRunsVerification(args)
	case "dart":
		return dartRunsVerification(args)
	case "flutter":
		return flutterRunsVerification(args)
	case "mix":
		return mixRunsVerification(args)
	case "python", "python3", "py":
		// -m pytest/unittest… is verification; so is running a project script
		// (acceptance steps like `python3 main.py`).
		if len(args) >= 2 && args[0] == "-m" && isVerificationRunnerCommand(args[1], args[2:]) {
			return true
		}
		return len(args) >= 1 && subAgentCommandLooksLikeRunnableScript(args[0], ".py")
	case "pytest", "phpunit", "pest", "phpstan", "psalm", "rspec", "rubocop", "jest", "vitest", "eslint", "tsc":
		return isVerificationRunnerCommand(cmd, args)
	case "make", "mingw32-make", "gmake":
		return makeRunsVerification(args)
	case "just":
		return justRunsVerification(args)
	case "task", "go-task":
		return taskRunnerRunsVerification(args)
	case "mage":
		return mageRunsVerification(args)
	case "bazel", "bazelisk":
		return bazelRunsVerification(args)
	case "pants":
		return pantsRunsVerification(args)
	case "buck2":
		return buckRunsVerification(args)
	case "mvn", "mvnw":
		return mavenRunsVerification(args)
	case "gradle", "gradlew":
		return gradleRunsVerification(args)
	case "cmake":
		return cmakeRunsVerification(args)
	case "ctest":
		return ctestRunsVerification(args)
	case "ninja", "ninja-build":
		return ninjaRunsVerification(args)
	case "cc", "gcc", "g++", "c++", "clang", "clang++", "cl", "cl.exe":
		return cCompilerRunsVerification(args)
	case "dotnet":
		return dotnetRunsVerification(args)
	case "composer":
		return composerRunsVerification(args)
	case "uv", "uvx":
		return uvRunsVerification(args)
	case "poetry", "pipenv", "hatch", "pdm", "rye":
		return pythonProjectToolRunsVerification(cmd, args)
	case "bundle", "bundler":
		return bundleRunsVerification(args)
	case "rails":
		return railsRunsVerification(args)
	case "rake":
		return rakeRunsVerification(args)
	}
	// Project-relative binary runs are acceptance verification (e.g. `./sysinfo`).
	// Path-installed tools (./vendor/bin/phpunit) are recognized by basename.
	// Mutating path tools (php-cs-fixer fix) and system binaries (/bin/rm) must not count.
	if tok := strings.TrimSpace(normalizeShellCommandToken(segment[0])); subAgentCommandLooksLikeExecutableRun(tok) {
		return pathExecutableRunsVerification(tok, commandNameBase(tok), args)
	}
	return cmd == "test" || isVerificationRunnerCommand(cmd, args)
}

func subAgentCommandTokenLooksLikePath(token string) bool {
	token = strings.TrimSpace(normalizeShellCommandToken(token))
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") || strings.HasPrefix(token, "/") || strings.HasPrefix(token, `.\`) || strings.HasPrefix(token, `..\`) {
		return true
	}
	return len(token) >= 3 && token[1] == ':' && (token[2] == '\\' || token[2] == '/')
}

// subAgentCommandIsProjectRelativeExecutable reports ./foo or ../foo style tokens
// (including Windows .\ / ..\). These are the safe acceptance-run form used by
// plan steps like `cd project && ./sysinfo`.
func subAgentCommandIsProjectRelativeExecutable(token string) bool {
	token = strings.TrimSpace(normalizeShellCommandToken(token))
	if token == "" || strings.HasSuffix(token, "/") || strings.HasSuffix(token, `\`) {
		return false
	}
	return strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") ||
		strings.HasPrefix(token, `.\`) || strings.HasPrefix(token, `..\`)
}

// subAgentCommandLooksLikeExecutableRun reports path-like shell tokens that may
// be treated as runnable programs. Bare names like "make" are handled by the
// named switch cases, not here.
func subAgentCommandLooksLikeExecutableRun(token string) bool {
	token = strings.TrimSpace(normalizeShellCommandToken(token))
	if token == "" || strings.ContainsAny(token, " \t") {
		return false
	}
	if strings.HasSuffix(token, "/") || strings.HasSuffix(token, `\`) {
		return false
	}
	return subAgentCommandTokenLooksLikePath(token)
}

// pathExecutableRunsVerification decides whether a path-like binary invocation
// counts as verification/acceptance evidence.
//
// Order matters:
//  1. Known runners by basename (phpunit, psalm, eslint…) with their arg rules
//  2. Reject help/list/mutating invocations
//  3. Accept project-relative bare binaries (./sysinfo)
//  4. Accept absolute project build binaries (/home/.../build/sysinfo) — common
//     on remote coding hosts — but never system paths like /bin/rm.
func pathExecutableRunsVerification(token, base string, args []string) bool {
	base = strings.TrimSpace(strings.ToLower(base))
	if base == "" {
		return false
	}
	// ./vendor/bin/phpunit, ./node_modules/.bin/eslint, /path/to/phpunit, etc.
	if isVerificationRunnerCommand(base, args) {
		return true
	}
	if hasNonExecutingVerificationArg(base, args) || hasMutatingVerificationArg(args) {
		return false
	}
	if pathExecutableArgsLookMutating(args) {
		return false
	}
	// Bare acceptance runs: ./sysinfo or absolute project build outputs.
	// System tools must never satisfy the verification gate by accident.
	return subAgentCommandIsProjectRelativeExecutable(token) ||
		subAgentCommandLooksLikeProjectBuildBinary(token, args)
}

// subAgentCommandLooksLikeProjectBuildBinary reports paths to a built project
// binary, e.g. /home/sysinfo12/build/sysinfo12 or D:\proj\build\app.exe.
//
// Tight on purpose: only build/dist/out (and project-local bin/) trees count.
// Arbitrary /home/foo, /tmp/bar, or C:\Windows\...\.exe must never pass.
func subAgentCommandLooksLikeProjectBuildBinary(token string, args []string) bool {
	token = strings.TrimSpace(normalizeShellCommandToken(token))
	if token == "" || strings.ContainsAny(token, " \t") {
		return false
	}
	// Acceptance runs are bare invocations (no subcommands). Flags like --help
	// are filtered earlier; any remaining args are treated as non-verify.
	if len(stripShellRedirectionOnlyArgs(args)) > 0 {
		return false
	}
	lower := strings.ToLower(token)
	base := pathpkg.Base(token)
	if base == "" || base == "/" || base == "." || base == ".." ||
		base == "\\" || strings.EqualFold(base, token) && !strings.ContainsAny(token, `/\`) {
		return false
	}

	// Windows drive path: only project build trees, never System32 etc.
	if len(token) >= 3 && token[1] == ':' && (token[2] == '\\' || token[2] == '/') {
		if strings.Contains(lower, `\windows\`) || strings.Contains(lower, `/windows/`) ||
			strings.Contains(lower, `\system32\`) || strings.Contains(lower, `\syswow64\`) ||
			strings.Contains(lower, `\program files`) {
			return false
		}
		return strings.Contains(lower, `\build\`) || strings.Contains(lower, "/build/") ||
			strings.Contains(lower, `\dist\`) || strings.Contains(lower, "/dist/") ||
			strings.Contains(lower, `\out\`) || strings.Contains(lower, "/out/") ||
			strings.Contains(lower, `\bin\`) || strings.Contains(lower, "/bin/")
	}

	if !strings.HasPrefix(token, "/") {
		return false
	}
	// System locations never count (even if they contain "bin").
	for _, prefix := range []string{
		"/bin/", "/sbin/", "/usr/bin/", "/usr/sbin/", "/usr/local/bin/",
		"/lib/", "/lib64/", "/usr/lib/", "/dev/", "/proc/", "/sys/", "/etc/",
	} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	// Project build/output trees only (remote CMake/Make layout).
	return strings.Contains(lower, "/build/") ||
		strings.Contains(lower, "/dist/") ||
		strings.Contains(lower, "/out/") ||
		// project-local bin, not /usr/bin (already rejected by prefix list)
		(strings.Contains(lower, "/bin/") && (strings.HasPrefix(lower, "/home/") ||
			strings.HasPrefix(lower, "/tmp/") || strings.HasPrefix(lower, "/var/tmp/") ||
			strings.HasPrefix(lower, "/opt/")))
}

// pathExecutableArgsLookMutating catches subcommands that rewrite the tree when
// invoked via a path (./vendor/bin/php-cs-fixer fix). Flag forms like --fix are
// handled by hasMutatingVerificationArg.
func pathExecutableArgsLookMutating(args []string) bool {
	if len(args) == 0 {
		return false
	}
	first := strings.Trim(strings.ToLower(normalizeShellExecutableToken(args[0])), `"'`)
	if first == "" || strings.HasPrefix(first, "-") {
		return false
	}
	switch first {
	case "fix", "format", "fmt", "install", "update", "init", "alter",
		"add", "remove", "require", "write", "apply", "migrate":
		return true
	default:
		return false
	}
}

// subAgentCommandLooksLikeRunnableScript reports script file arguments passed to
// interpreters (main.py, app.js). Extensions are matched case-insensitively.
func subAgentCommandLooksLikeRunnableScript(token string, exts ...string) bool {
	token = strings.TrimSpace(normalizeShellCommandToken(token))
	if token == "" {
		return false
	}
	lower := strings.ToLower(token)
	for _, ext := range exts {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext != "" && strings.HasSuffix(lower, ext) {
			return true
		}
	}
	// Path-like tokens without known extension still count (python path/to/tool).
	return subAgentCommandTokenLooksLikePath(token)
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
		case cmd == "timeout" || cmd == "gtimeout":
			segment = stripTimeoutCommandPrefix(segment[1:])
			continue
		case cmd == "cmd" || cmd == "cmd.exe":
			segment = stripCmdCommandPrefix(segment[1:])
			continue
		}
		break
	}
	return segment
}

func stripCmdCommandPrefix(args []string) []string {
	for i := 0; i < len(args); i++ {
		arg := normalizeShellExecutableToken(args[i])
		switch arg {
		case "/c", "-c":
			return shellCommandFields(strings.Join(args[i+1:], " "))
		case "/k", "-k":
			return nil
		}
		if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "-") {
			continue
		}
		return args[i:]
	}
	return nil
}

func stripTimeoutCommandPrefix(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--":
			args = args[1:]
			break
		case timeoutOptionConsumesValue(arg):
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
	if len(args) == 0 {
		return args
	}
	// GNU timeout syntax is: timeout [OPTION] DURATION COMMAND [ARG]...
	return args[1:]
}

func timeoutOptionConsumesValue(arg string) bool {
	switch arg {
	case "-k", "--kill-after", "-s", "--signal":
		return true
	}
	return false
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
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9' && i > 0) || r == '_' {
			continue
		}
		return false
	}
	return true
}

func packageManagerRunsVerification(args []string) bool {
	args = stripPackageManagerOptions(args)
	if len(args) == 0 {
		return false
	}
	if args[0] == "workspace" && len(args) > 2 {
		args = args[2:]
	}
	if args[0] == "workspaces" && len(args) > 2 && args[1] == "foreach" {
		return yarnWorkspacesForeachRunsVerification(args[2:])
	}
	if len(args) == 0 {
		return false
	}
	if hasMutatingVerificationArg(args) {
		return false
	}
	if isVerificationScriptName(args[0]) {
		return true
	}
	if args[0] == "run" && len(args) > 1 {
		scriptArgs := stripPackageManagerOptions(args[1:])
		return len(scriptArgs) > 0 && isVerificationScriptName(scriptArgs[0])
	}
	if (args[0] == "exec" || args[0] == "dlx") && len(args) > 1 {
		return verificationRunnerCommandFromArgs(args[1:])
	}
	if verificationRunnerCommandFromArgs(args) {
		return true
	}
	return false
}

func yarnWorkspacesForeachRunsVerification(args []string) bool {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case yarnWorkspacesForeachOptionConsumesValue(arg):
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
	if len(args) == 0 {
		return false
	}
	if args[0] == "run" && len(args) > 1 {
		scriptArgs := stripPackageManagerOptions(args[1:])
		return len(scriptArgs) > 0 && isVerificationScriptName(scriptArgs[0]) && !hasMutatingVerificationArg(scriptArgs)
	}
	if args[0] == "exec" && len(args) > 1 {
		return verificationRunnerCommandFromArgs(args[1:])
	}
	return false
}

func yarnWorkspacesForeachOptionConsumesValue(arg string) bool {
	switch arg {
	case "--from", "--include", "--exclude", "--since", "--jobs", "-j":
		return true
	}
	return false
}
func stripPackageManagerOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case packageManagerOptionConsumesValue(arg):
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

func packageManagerOptionConsumesValue(arg string) bool {
	switch arg {
	case "-w", "--workspace", "--filter", "-f", "-c", "--cwd", "--dir", "--prefix", "--registry", "--cache", "--config", "--userconfig":
		return true
	}
	return false
}

func corepackRunsVerification(args []string) bool {
	args = stripPackageManagerOptions(args)
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "npm", "pnpm", "yarn":
		return packageManagerRunsVerification(args[1:])
	case "npx", "pnpx", "yarnx":
		return verificationRunnerCommandFromArgs(args[1:])
	}
	return false
}

func uvRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if verificationRunnerCommandFromArgs(args) {
		return true
	}
	if args[0] == "run" && len(args) > 1 {
		runArgs := args[1:]
		return verificationRunnerCommandFromArgs(runArgs) || verificationScriptInvocationRunsVerification(runArgs)
	}
	return false
}

func pythonProjectToolRunsVerification(cmd string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	args = stripVerificationRunnerOptions(args)
	if len(args) == 0 {
		return false
	}
	switch cmd {
	case "poetry", "pdm":
		if firstArgIn(args, "check") {
			return true
		}
	case "hatch":
		if firstArgIn(args, "test") {
			return true
		}
	case "rye":
		if verificationScriptInvocationRunsVerification(args) {
			return true
		}
	}
	if args[0] == "run" && len(args) > 1 {
		runArgs := args[1:]
		return verificationRunnerCommandFromArgs(runArgs) || verificationScriptInvocationRunsVerification(runArgs)
	}
	return false
}

func bundleRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if verificationRunnerCommandFromArgs(args) || verificationScriptInvocationRunsVerification(args) {
		return true
	}
	if args[0] == "exec" && len(args) > 1 {
		execArgs := args[1:]
		return verificationRunnerCommandFromArgs(execArgs) || verificationScriptInvocationRunsVerification(execArgs)
	}
	return false
}

func composerRunsVerification(args []string) bool {
	args = stripComposerOptions(args)
	if len(args) == 0 {
		return false
	}
	if verificationRunnerCommandFromArgs(args) || verificationScriptInvocationRunsVerification(args) {
		return true
	}
	switch normalizeShellExecutableToken(args[0]) {
	case "run", "run-script":
		if len(args) <= 1 {
			return false
		}
		scriptArgs := stripComposerOptions(args[1:])
		return verificationRunnerCommandFromArgs(scriptArgs) || verificationScriptInvocationRunsVerification(scriptArgs)
	case "exec":
		return len(args) > 1 && verificationRunnerCommandFromArgs(args[1:])
	}
	return false
}

func stripComposerOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case composerOptionConsumesValue(arg):
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "--working-dir="), strings.HasPrefix(arg, "--ignore-platform-req="):
			args = args[1:]
			continue
		case strings.HasPrefix(arg, "-"):
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func composerOptionConsumesValue(arg string) bool {
	switch arg {
	case "-d", "--working-dir", "--ignore-platform-req":
		return true
	}
	return false
}

func isVerificationScriptName(name string) bool {
	if isWatchVerificationScriptName(name) {
		return false
	}
	switch name {
	case "test", "tests", "unit", "units", "integration", "e2e", "ci", "verify", "validate", "validation", "check", "checks", "build", "lint", "vet", "typecheck", "type-check":
		return true
	}
	return strings.HasPrefix(name, "test:") ||
		strings.HasPrefix(name, "unit:") ||
		strings.HasPrefix(name, "integration:") ||
		strings.HasPrefix(name, "e2e:") ||
		strings.HasPrefix(name, "ci:") ||
		strings.HasPrefix(name, "verify:") ||
		strings.HasPrefix(name, "validate:") ||
		strings.HasPrefix(name, "validation:") ||
		strings.HasPrefix(name, "check:") ||
		strings.HasPrefix(name, "build:") ||
		strings.HasPrefix(name, "lint:") ||
		strings.HasPrefix(name, "typecheck:") ||
		strings.HasPrefix(name, "type-check:")
}

func verificationScriptInvocationRunsVerification(args []string) bool {
	return len(args) > 0 && isVerificationScriptName(args[0]) && !hasMutatingVerificationArg(args)
}

func isWatchVerificationScriptName(name string) bool {
	for _, part := range strings.FieldsFunc(name, func(r rune) bool {
		return r == ':' || r == '-' || r == '_' || r == '.'
	}) {
		if part == "watch" {
			return true
		}
	}
	return false
}

func verificationRunnerCommandFromArgs(args []string) bool {
	args = stripVerificationRunnerOptions(args)
	if len(args) == 0 {
		return false
	}
	return isVerificationRunnerCommand(args[0], args[1:])
}

func stripVerificationRunnerOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case verificationRunnerOptionConsumesValue(arg):
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

func verificationRunnerOptionConsumesValue(arg string) bool {
	switch arg {
	case "-p", "--package", "--with", "--from", "--python", "--project", "--directory", "--cwd", "--env-file", "--config-file":
		return true
	}
	return false
}

func isVerificationRunnerCommand(name string, args []string) bool {
	cmd := commandNameBase(name)
	if hasNonExecutingVerificationArg(cmd, args) || hasMutatingVerificationArg(args) || (cmd == "rubocop" && hasRubocopAutoCorrectArg(args)) {
		return false
	}
	switch cmd {
	case "rails":
		return railsRunsVerification(args)
	case "rake":
		return rakeRunsVerification(args)
	case "ruff":
		return firstArgIn(args, "check")
	case "golangci-lint":
		return len(args) == 0 || firstArgIn(args, "run")
	case "pyre":
		return len(args) == 0 || firstArgIn(args, "check")
	case "prettier":
		return prettierRunsVerification(args)
	case "biome":
		return biomeRunsVerification(args)
	case "turbo", "turbo.exe":
		return turboRunsVerification(args)
	case "nx":
		return nxRunsVerification(args)
	case "mypy", "pyright", "basedpyright", "staticcheck", "revive":
		return true
	}
	return isVerificationRunner(name)
}

func railsRunsVerification(args []string) bool {
	return firstArgIn(args, "test")
}

func rakeRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		if strings.HasPrefix(normalized, "-") || isShellEnvAssignment(normalized) {
			continue
		}
		return isRakeVerificationTask(normalized)
	}
	return false
}

func isRakeVerificationTask(task string) bool {
	task = strings.TrimSpace(task)
	if task == "" {
		return false
	}
	return task == "test" || task == "spec" || task == "cucumber" ||
		strings.HasPrefix(task, "test:") ||
		strings.HasPrefix(task, "spec:") ||
		strings.HasPrefix(task, "cucumber:")
}

func swiftRunsVerification(args []string) bool {
	return firstArgIn(args, "test", "build")
}

func cCompilerRunsVerification(args []string) bool {
	for _, arg := range args {
		normalized := strings.Trim(strings.TrimSpace(normalizeShellExecutableToken(arg)), `"'`)
		if normalized == "" || strings.HasPrefix(normalized, "-") || isShellEnvAssignment(normalized) {
			continue
		}
		if cCompilerArgLooksLikeSourceFile(normalized) {
			return true
		}
	}
	return false
}

func cCompilerArgLooksLikeSourceFile(arg string) bool {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		return false
	}
	if idx := strings.LastIndexAny(arg, `/\`); idx >= 0 {
		arg = arg[idx+1:]
	}
	for _, ext := range []string{".c", ".cc", ".cpp", ".cxx", ".c++", ".m", ".mm"} {
		if strings.HasSuffix(arg, ext) {
			return true
		}
	}
	return false
}

func zigRunsVerification(args []string) bool {
	return firstArgIn(args, "test", "build")
}

func stackRunsVerification(args []string) bool {
	args = stripHaskellToolOptions(args)
	return firstArgIn(args, "test", "build", "bench", "haddock")
}

func cabalRunsVerification(args []string) bool {
	args = stripHaskellToolOptions(args)
	return firstArgIn(args, "test", "v2-test", "new-test", "build", "v2-build", "new-build", "bench", "v2-bench", "new-bench", "haddock", "v2-haddock", "new-haddock")
}

func stripHaskellToolOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		if arg == "--" || isShellEnvAssignment(arg) {
			args = args[1:]
			continue
		}
		switch arg {
		case "--stack-yaml", "--resolver", "--compiler", "--project-file", "--store-dir", "-w", "--with-compiler":
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		}
		if strings.HasPrefix(arg, "--stack-yaml=") ||
			strings.HasPrefix(arg, "--resolver=") ||
			strings.HasPrefix(arg, "--compiler=") ||
			strings.HasPrefix(arg, "--project-file=") ||
			strings.HasPrefix(arg, "--store-dir=") ||
			strings.HasPrefix(arg, "--with-compiler=") {
			args = args[1:]
			continue
		}
		if strings.HasPrefix(arg, "-") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func leinRunsVerification(args []string) bool {
	args = stripClojureToolOptions(args)
	return firstArgIn(args, "test", "check", "eastwood", "kibit", "clj-kondo")
}

func clojureRunsVerification(args []string) bool {
	args = stripClojureToolOptions(args)
	if len(args) == 0 {
		return false
	}
	for i, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		if clojureAliasLooksVerification(normalized) {
			return true
		}
		if normalized == "-m" && i+1 < len(args) && !strings.HasPrefix(normalizeShellExecutableToken(args[i+1]), "-") {
			return clojureMainLooksVerification(args[i+1])
		}
	}
	return false
}

func babashkaRunsVerification(args []string) bool {
	args = stripClojureToolOptions(args)
	if firstArgIn(args, "test", "check") {
		return true
	}
	if len(args) > 1 && normalizeShellExecutableToken(args[0]) == "run" {
		return isVerificationScriptName(normalizeShellExecutableToken(args[1]))
	}
	return false
}

func stripClojureToolOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		if arg == "--" || isShellEnvAssignment(arg) {
			args = args[1:]
			continue
		}
		switch arg {
		case "-sdeps", "-jvm-opts", "-config", "-deps-root", "-main":
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		}
		if strings.HasPrefix(arg, "-sdeps=") ||
			strings.HasPrefix(arg, "-jvm-opts=") ||
			strings.HasPrefix(arg, "-config=") ||
			strings.HasPrefix(arg, "-deps-root=") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func clojureAliasLooksVerification(arg string) bool {
	if arg == "" {
		return false
	}
	for _, prefix := range []string{"-m:", "-x:", "-a:", "-a"} {
		if strings.HasPrefix(arg, prefix) {
			arg = strings.TrimPrefix(arg, prefix)
			break
		}
	}
	for _, part := range strings.Split(arg, ":") {
		switch part {
		case "test", "tests", "kaocha", "eftest", "cognitect-test-runner", "runner", "check", "lint", "clj-kondo":
			return true
		}
	}
	return false
}

func clojureMainLooksVerification(main string) bool {
	main = normalizeShellExecutableToken(main)
	return strings.Contains(main, "kaocha") ||
		strings.Contains(main, "eftest") ||
		strings.Contains(main, "cognitect.test-runner") ||
		strings.Contains(main, "clj-kondo")
}

func sbtRunsVerification(args []string) bool {
	args = stripScalaToolOptions(args)
	for _, arg := range args {
		for _, task := range scalaTaskTokens(arg) {
			if scalaTaskLooksVerification(task) {
				return true
			}
			if scalaTaskLooksNonVerification(task) {
				return false
			}
		}
	}
	return false
}

func millRunsVerification(args []string) bool {
	args = stripScalaToolOptions(args)
	for _, arg := range args {
		for _, task := range scalaTaskTokens(arg) {
			if millTaskLooksVerification(task) {
				return true
			}
			if scalaTaskLooksNonVerification(task) {
				return false
			}
		}
	}
	return false
}

func stripScalaToolOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		if arg == "--" || isShellEnvAssignment(arg) {
			args = args[1:]
			continue
		}
		switch arg {
		case "-d", "--debug", "-v", "--verbose", "-batch", "--batch", "-no-colors", "--no-colors":
			args = args[1:]
			continue
		case "-sbt-dir", "-sbt-boot", "-ivy", "-java-home", "-jvm-debug", "--meta-level":
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		}
		if strings.HasPrefix(arg, "-sbt-dir=") ||
			strings.HasPrefix(arg, "-sbt-boot=") ||
			strings.HasPrefix(arg, "-ivy=") ||
			strings.HasPrefix(arg, "-java-home=") ||
			strings.HasPrefix(arg, "-jvm-debug=") ||
			strings.HasPrefix(arg, "--meta-level=") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func normalizeScalaTaskToken(task string) string {
	task = strings.TrimSpace(normalizeShellExecutableToken(task))
	task = strings.Trim(task, "'\"")
	for strings.HasPrefix(task, ";") {
		task = strings.TrimPrefix(task, ";")
	}
	return task
}

func scalaTaskTokens(arg string) []string {
	normalized := normalizeScalaTaskToken(arg)
	if normalized == "" {
		return nil
	}
	parts := strings.Split(normalized, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = normalizeScalaTaskToken(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func scalaTaskLooksVerification(task string) bool {
	task = strings.TrimPrefix(task, "+")
	task = strings.TrimPrefix(task, "~")
	task = strings.TrimPrefix(task, "!")
	task = strings.TrimSpace(task)
	if idx := strings.LastIndex(task, "/"); idx >= 0 {
		task = task[idx+1:]
	}
	switch task {
	case "test", "testonly", "testquick", "compile", "test:compile", "it:test", "integrationtest/test",
		"scalafmtcheck", "scalafmtcheckall", "scalafmtsbtcheck", "scalafmttestcheck",
		"scalafix", "scalafixall", "scalastyle", "coverage", "coverageoff", "coverageaggregate":
		return true
	}
	return strings.HasSuffix(task, ":test") ||
		strings.HasSuffix(task, "/test") ||
		strings.HasSuffix(task, ".test") ||
		strings.HasSuffix(task, ".compile") ||
		strings.HasSuffix(task, ".scalafmtcheck") ||
		strings.HasSuffix(task, ".scalafmtcheckall")
}

func millTaskLooksVerification(task string) bool {
	return scalaTaskLooksVerification(task)
}

func scalaTaskLooksNonVerification(task string) bool {
	task = strings.TrimPrefix(task, "+")
	task = strings.TrimPrefix(task, "~")
	task = strings.TrimSpace(task)
	if idx := strings.LastIndex(task, "/"); idx >= 0 {
		task = task[idx+1:]
	}
	switch task {
	case "run", "console", "repl", "update", "reload", "publishlocal", "publish", "assembly":
		return true
	}
	return strings.HasSuffix(task, ".run") ||
		strings.HasSuffix(task, "/run") ||
		strings.HasSuffix(task, ":run")
}

func duneRunsVerification(args []string) bool {
	args = stripDuneOptions(args)
	if len(args) == 0 {
		return false
	}
	cmd := normalizeShellExecutableToken(args[0])
	switch cmd {
	case "runtest", "test":
		return true
	case "build":
		if len(args) == 1 {
			return true
		}
		for _, arg := range args[1:] {
			target := normalizeShellExecutableToken(arg)
			if target == "@runtest" || target == "@check" || target == "@fmt" || strings.HasSuffix(target, "/runtest") || strings.HasSuffix(target, "/check") {
				return true
			}
		}
		return true
	case "exec", "utop", "promote", "clean", "install", "subst":
		return false
	}
	return false
}

func stripDuneOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		if arg == "--" || isShellEnvAssignment(arg) {
			args = args[1:]
			continue
		}
		switch arg {
		case "--root", "--workspace", "--profile", "--build-dir", "-j":
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		}
		if strings.HasPrefix(arg, "--root=") ||
			strings.HasPrefix(arg, "--workspace=") ||
			strings.HasPrefix(arg, "--profile=") ||
			strings.HasPrefix(arg, "--build-dir=") ||
			strings.HasPrefix(arg, "-j") {
			args = args[1:]
			continue
		}
		if strings.HasPrefix(arg, "-") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func opamRunsVerification(args []string) bool {
	args = stripOpamOptions(args)
	if len(args) == 0 {
		return false
	}
	if normalizeShellExecutableToken(args[0]) != "exec" {
		return false
	}
	args = stripOpamExecOptions(args[1:])
	if len(args) == 0 {
		return false
	}
	if commandNameBase(args[0]) == "dune" {
		return duneRunsVerification(args[1:])
	}
	return isVerificationRunnerCommand(args[0], args[1:])
}

func stripOpamOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		if arg == "--" || isShellEnvAssignment(arg) {
			args = args[1:]
			continue
		}
		switch arg {
		case "--switch", "--root", "--cli":
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		}
		if strings.HasPrefix(arg, "--switch=") || strings.HasPrefix(arg, "--root=") || strings.HasPrefix(arg, "--cli=") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func stripOpamExecOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		if arg == "--" || isShellEnvAssignment(arg) {
			args = args[1:]
			continue
		}
		if strings.HasPrefix(arg, "-") {
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func hasNonExecutingVerificationArg(cmd string, args []string) bool {
	if hasNormalizedArgOrTruthyAssignment(args, "--help", "-h", "help", "--version") {
		return true
	}
	switch cmd {
	case "pytest":
		return hasNormalizedArgOrTruthyAssignment(args, "--collect-only", "--co", "--fixtures", "--fixtures-per-test", "--setup-only", "--setup-plan", "--markers", "--trace-config")
	case "jest":
		return hasNormalizedArgOrTruthyAssignment(args, "--listtests", "--showconfig", "--clearcache")
	case "vitest":
		return len(args) > 0 && hasNormalizedArgOrTruthyAssignment(args[:1], "list", "--list")
	case "eslint":
		return hasNormalizedArgOrTruthyAssignment(args, "--print-config", "--env-info")
	case "tsc":
		return hasNormalizedArgOrTruthyAssignment(args, "--showconfig", "--init")
	case "tox":
		return hasNormalizedArgOrTruthyAssignment(args, "--listenvs", "--listenvs-all", "-l", "--showconfig")
	case "nox":
		return hasNormalizedArgOrTruthyAssignment(args, "--list", "-l", "--list-sessions", "--json")
	case "rspec", "cucumber":
		return hasNormalizedArgOrTruthyAssignment(args, "--dry-run")
	case "rubocop":
		return hasNormalizedArgOrTruthyAssignment(args, "--show-cops", "--init")
	case "phpunit", "pest":
		return hasNormalizedArgOrTruthyAssignment(args, "--list-tests", "--list-groups", "--list-suites", "--generate-configuration", "--migrate-configuration")
	case "phpstan":
		return hasNormalizedArgOrTruthyAssignment(args, "clear-result-cache", "dump-parameters")
	case "psalm":
		return hasNormalizedArgOrTruthyAssignment(args, "--init", "--alter", "--set-baseline", "--clear-cache", "--clear-global-cache") ||
			hasNormalizedArgPrefix(args, "--set-baseline=")
	case "mypy":
		return hasNormalizedArgOrTruthyAssignment(args, "--install-types")
	case "pyright", "basedpyright":
		return hasNormalizedArgOrTruthyAssignment(args, "--createstub")
	}
	return false
}

func hasInteractiveVerificationArg(args []string) bool {
	for _, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		switch normalized {
		case "watch", "--watch", "--watchall", "--interactive", "--ui":
			return true
		}
		if normalizedArgMatchesTruthyAssignment(normalized, "--watch", "--watchall", "--interactive", "--ui") {
			return true
		}
	}
	return false
}

func nodeRunsVerification(args []string) bool {
	return hasArg(args, "--test") && !hasNodeTestNoRunArg(args)
}

func hasNodeTestNoRunArg(args []string) bool {
	if hasNormalizedArg(args, "--help", "-h", "--version", "-v") {
		return true
	}
	for _, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		if normalized == "--test-only" {
			return true
		}
		if strings.HasPrefix(normalized, "--test-only=") && !isFalseShellFlagValue(strings.TrimPrefix(normalized, "--test-only=")) {
			return true
		}
	}
	return false
}

func isFalseShellFlagValue(value string) bool {
	switch strings.Trim(strings.ToLower(strings.TrimSpace(value)), "'\"") {
	case "", "0", "false", "no", "off":
		return true
	default:
		return false
	}
}
func hasMutatingVerificationArg(args []string) bool {
	for _, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		if normalizedArgMatchesOrTruthyAssignment(normalized, "--fix", "--fix-dry-run", "--write", "--auto-correct", "--autocorrect", "--auto-correct-all", "--autocorrect-all") {
			return true
		}
	}
	return false
}

func hasRubocopAutoCorrectArg(args []string) bool {
	return hasNormalizedArgOrTruthyAssignment(args, "-a", "-A")
}

func turboRunsVerification(args []string) bool {
	args = stripMonorepoRunnerOptions(args)
	if len(args) == 0 || args[0] != "run" {
		return false
	}
	args = stripMonorepoRunnerOptions(args[1:])
	for _, arg := range args {
		arg = normalizeShellExecutableToken(arg)
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			break
		}
		if isVerificationScriptName(arg) {
			return true
		}
	}
	return false
}

func nxRunsVerification(args []string) bool {
	args = stripMonorepoRunnerOptions(args)
	if len(args) == 0 {
		return false
	}
	subcommand := normalizeShellExecutableToken(args[0])
	switch subcommand {
	case "test", "build", "lint", "typecheck", "type-check":
		return true
	case "run":
		return len(args) > 1 && nxTargetIsVerification(args[1])
	case "affected", "run-many":
		return nxArgsContainVerificationTarget(args[1:])
	}
	return false
}

func stripMonorepoRunnerOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case monorepoRunnerOptionConsumesValue(arg):
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

func monorepoRunnerOptionConsumesValue(arg string) bool {
	switch arg {
	case "--filter", "--scope", "--since", "--only", "--project", "--configuration", "--parallel", "--output-style", "--runner", "--exclude", "-c", "-p":
		return true
	}
	return false
}

func nxArgsContainVerificationTarget(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := normalizeShellExecutableToken(args[i])
		switch {
		case arg == "-t" || arg == "--target" || arg == "--targets":
			if i+1 < len(args) && nxTargetListContainsVerification(args[i+1]) {
				return true
			}
			i++
		case strings.HasPrefix(arg, "-t="):
			if nxTargetListContainsVerification(strings.TrimPrefix(arg, "-t=")) {
				return true
			}
		case strings.HasPrefix(arg, "--target="):
			if nxTargetListContainsVerification(strings.TrimPrefix(arg, "--target=")) {
				return true
			}
		case strings.HasPrefix(arg, "--targets="):
			if nxTargetListContainsVerification(strings.TrimPrefix(arg, "--targets=")) {
				return true
			}
		case nxTargetIsVerification(arg):
			return true
		}
	}
	return false
}

func nxTargetListContainsVerification(value string) bool {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
		if nxTargetIsVerification(part) {
			return true
		}
	}
	return false
}

func nxTargetIsVerification(value string) bool {
	value = normalizeShellExecutableToken(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	if isVerificationScriptName(value) {
		return true
	}
	if idx := strings.LastIndex(value, ":"); idx >= 0 {
		return isVerificationScriptName(value[idx+1:])
	}
	return false
}
func isVerificationRunner(name string) bool {
	switch commandNameBase(name) {
	case "pytest", "unittest", "tox", "nox", "jest", "vitest", "eslint", "tsc", "phpunit", "pest", "phpstan", "psalm", "rspec", "rubocop", "cucumber",
		// Syntax/type compile checks commonly used for small CLI/script projects
		// without a formal test suite (remote pure-coding workbench).
		"py_compile", "compileall":
		return true
	}
	return false
}

// subAgentVerificationAllowsSilentSuccess reports verifiers that intentionally
// produce no stdout when they succeed (exit code is the signal).
func subAgentVerificationAllowsSilentSuccess(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return false
	}
	for _, segment := range shellCommandSegments(normalized) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		args := segment[1:]
		switch cmd {
		case "python", "python3", "py":
			if len(args) >= 2 && args[0] == "-m" {
				mod := commandNameBase(args[1])
				if mod == "py_compile" || mod == "compileall" {
					return true
				}
			}
		case "py_compile", "compileall":
			return true
		}
	}
	return false
}

func prettierRunsVerification(args []string) bool {
	return hasNormalizedArg(args, "--check", "-c", "--list-different", "-l") && !hasNormalizedArgOrTruthyAssignment(args, "--write", "-w")
}

func biomeRunsVerification(args []string) bool {
	if len(args) == 0 || hasNormalizedArgOrTruthyAssignment(args, "--write", "--apply", "--apply-unsafe", "--suppress") {
		return false
	}
	return firstArgIn(args, "check", "ci", "lint")
}

func hasNormalizedArg(args []string, targets ...string) bool {
	for _, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		for _, target := range targets {
			if normalized == target {
				return true
			}
		}
	}
	return false
}

func hasNormalizedArgOrTruthyAssignment(args []string, targets ...string) bool {
	for _, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		if normalizedArgMatchesOrTruthyAssignment(normalized, targets...) {
			return true
		}
	}
	return false
}

func normalizedArgMatchesOrTruthyAssignment(normalized string, targets ...string) bool {
	for _, target := range targets {
		if normalized == target {
			return true
		}
		if strings.HasPrefix(normalized, target+"=") && !isFalseShellFlagValue(strings.TrimPrefix(normalized, target+"=")) {
			return true
		}
	}
	return false
}

func normalizedArgMatchesTruthyAssignment(normalized string, targets ...string) bool {
	for _, target := range targets {
		if strings.HasPrefix(normalized, target+"=") && !isFalseShellFlagValue(strings.TrimPrefix(normalized, target+"=")) {
			return true
		}
	}
	return false
}

func hasNormalizedArgPrefix(args []string, prefixes ...string) bool {
	for _, arg := range args {
		normalized := normalizeShellExecutableToken(arg)
		for _, prefix := range prefixes {
			if strings.HasPrefix(normalized, prefix) {
				return true
			}
		}
	}
	return false
}

func cmakeRunsVerification(args []string) bool {
	build := false
	for i, arg := range args {
		arg = normalizeShellExecutableToken(arg)
		if arg == "--build" {
			build = true
			continue
		}
		if (arg == "--target" || arg == "-t") && i+1 < len(args) && isNonVerificationBuildTarget(args[i+1]) {
			return false
		}
		if strings.HasPrefix(arg, "--target=") && isNonVerificationBuildTarget(strings.TrimPrefix(arg, "--target=")) {
			return false
		}
	}
	return build
}

func isNonVerificationBuildTarget(target string) bool {
	switch normalizeShellExecutableToken(strings.TrimSpace(target)) {
	case "clean", "install", "uninstall", "package":
		return true
	}
	return false
}

func ctestRunsVerification(args []string) bool {
	for _, arg := range args {
		arg = normalizeShellExecutableToken(arg)
		if arg == "-n" || arg == "--show-only" || strings.HasPrefix(arg, "--show-only=") || arg == "-h" || arg == "--help" {
			return false
		}
	}
	return true
}

func ninjaRunsVerification(args []string) bool {
	for _, arg := range args {
		arg = normalizeShellExecutableToken(arg)
		if arg == "-t" || strings.HasPrefix(arg, "-t=") || (strings.HasPrefix(arg, "-t") && len(arg) > len("-t")) {
			return false
		}
	}
	args = stripNinjaOptions(args)
	if len(args) == 0 {
		return true
	}
	return firstArgIn(args, "test", "check", "build", "all")
}

func stripNinjaOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case ninjaOptionConsumesValue(arg):
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

func ninjaOptionConsumesValue(arg string) bool {
	switch arg {
	case "-C", "-c", "-f", "-j", "-k", "-l", "-t", "-w":
		return true
	}
	return false
}

func makeRunsVerification(args []string) bool {
	args = stripShellRedirectionOnlyArgs(stripMakeOptions(stripShellRedirectionOnlyArgs(args)))
	if len(args) == 0 {
		return true
	}
	return firstArgIn(args, "test", "check", "build", "all", "lint", "typecheck", "type-check")
}

// stripShellRedirectionOnlyArgs drops tokens that only redirect streams
// (2>&1, >/dev/null, 2>/dev/null, …) so they do not look like make/cmake targets.
func stripShellRedirectionOnlyArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		tok := strings.TrimSpace(normalizeShellExecutableToken(arg))
		if tok == "" {
			continue
		}
		if tok == "2>&1" || tok == "1>&2" || tok == "&>/dev/null" || tok == ">&/dev/null" {
			continue
		}
		if strings.HasPrefix(tok, ">/") || strings.HasPrefix(tok, ">>/") ||
			strings.HasPrefix(tok, "1>/") || strings.HasPrefix(tok, "1>>/") ||
			strings.HasPrefix(tok, "2>/") || strings.HasPrefix(tok, "2>>/") ||
			strings.HasPrefix(tok, "&>/") || strings.HasPrefix(tok, "&>>/") {
			continue
		}
		// Bare operators without a glued path are also redirections.
		if tok == ">" || tok == ">>" || tok == "1>" || tok == "1>>" ||
			tok == "2>" || tok == "2>>" || tok == "&>" || tok == "&>>" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func stripMakeOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case makeOptionConsumesValue(arg):
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

func makeOptionConsumesValue(arg string) bool {
	switch arg {
	case "-C", "-c", "-f", "--file", "--makefile", "-i", "--include-dir", "-j", "--jobs", "-l", "--load-average", "-o", "--old-file", "--assume-old", "--directory":
		return true
	}
	return false
}

func justRunsVerification(args []string) bool {
	if hasTaskRunnerNonExecutingArg(args) {
		return false
	}
	args = stripJustOptions(args)
	if len(args) == 0 {
		return false
	}
	return isVerificationScriptName(args[0])
}

func stripJustOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case justOptionConsumesTwoValues(arg):
			if len(args) > 2 {
				args = args[3:]
			} else {
				args = nil
			}
			continue
		case justOptionConsumesValue(arg):
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

func justOptionConsumesValue(arg string) bool {
	switch arg {
	case "-f", "--justfile", "-d", "--working-directory", "--shell", "--shell-arg", "--chooser", "--color", "--dotenv-path", "--command-color":
		return true
	}
	return false
}

func justOptionConsumesTwoValues(arg string) bool {
	return arg == "--set"
}

func taskRunnerRunsVerification(args []string) bool {
	if hasTaskRunnerNonExecutingArg(args) {
		return false
	}
	args = stripTaskRunnerOptions(args)
	if len(args) == 0 {
		return false
	}
	return isVerificationScriptName(args[0])
}

func stripTaskRunnerOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case taskRunnerOptionConsumesValue(arg):
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

func taskRunnerOptionConsumesValue(arg string) bool {
	switch arg {
	case "-t", "--taskfile", "-d", "--dir", "-o", "--output", "--output-style", "-c", "--concurrency", "--sort", "--color":
		return true
	}
	return false
}

func mageRunsVerification(args []string) bool {
	if hasTaskRunnerNonExecutingArg(args) {
		return false
	}
	args = stripMageOptions(args)
	if len(args) == 0 {
		return false
	}
	return isVerificationScriptName(args[0])
}

func hasTaskRunnerNonExecutingArg(args []string) bool {
	for _, arg := range args {
		arg = normalizeShellExecutableToken(arg)
		switch arg {
		case "--help", "-h", "help", "--version", "version",
			"--list", "-l", "list", "--list-all", "-a",
			"--summary", "--dump", "--dry-run", "--dry", "-n":
			return true
		}
	}
	return false
}

func stripMageOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case mageOptionConsumesValue(arg):
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

func mageOptionConsumesValue(arg string) bool {
	switch arg {
	case "-d", "--dir", "-f", "--file", "-t", "--timeout":
		return true
	}
	return false
}

func bazelRunsVerification(args []string) bool {
	args = stripBazelStartupOptions(args)
	if len(args) == 0 {
		return false
	}
	switch normalizeShellExecutableToken(args[0]) {
	case "test", "build":
		return true
	}
	return false
}

func stripBazelStartupOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case bazelStartupOptionConsumesValue(arg):
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "--host_jvm_args="), strings.HasPrefix(arg, "--output_base="), strings.HasPrefix(arg, "--output_user_root="), strings.HasPrefix(arg, "--bazelrc="), strings.HasPrefix(arg, "--max_idle_secs="):
			args = args[1:]
			continue
		case strings.HasPrefix(arg, "-"):
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func bazelStartupOptionConsumesValue(arg string) bool {
	switch arg {
	case "--host_jvm_args", "--output_base", "--output_user_root", "--bazelrc", "--max_idle_secs":
		return true
	}
	return false
}

func pantsRunsVerification(args []string) bool {
	args = stripPantsGlobalOptions(args)
	if len(args) == 0 {
		return false
	}
	return firstArgIn(args, "test", "check", "lint")
}

func stripPantsGlobalOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case pantsGlobalOptionConsumesValue(arg):
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "--pants-config-files="), strings.HasPrefix(arg, "--pants-workdir="), strings.HasPrefix(arg, "--level="):
			args = args[1:]
			continue
		case strings.HasPrefix(arg, "-"):
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func pantsGlobalOptionConsumesValue(arg string) bool {
	switch arg {
	case "--pants-config-files", "--pants-workdir", "--level":
		return true
	}
	return false
}

func buckRunsVerification(args []string) bool {
	args = stripBuckGlobalOptions(args)
	if len(args) == 0 {
		return false
	}
	return firstArgIn(args, "test", "build")
}

func stripBuckGlobalOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case buckGlobalOptionConsumesValue(arg):
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "--isolation-dir="), strings.HasPrefix(arg, "--config="):
			args = args[1:]
			continue
		case strings.HasPrefix(arg, "-"):
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func buckGlobalOptionConsumesValue(arg string) bool {
	switch arg {
	case "--isolation-dir", "--config":
		return true
	}
	return false
}

func dotnetRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch normalizeShellExecutableToken(args[0]) {
	case "test", "vstest":
		return !hasDotnetListOnlyArg(args[1:])
	case "build":
		return true
	case "format":
		return hasArg(args[1:], "--verify-no-changes") || hasArg(args[1:], "--check")
	case "msbuild":
		return dotnetMSBuildRunsVerification(args[1:])
	}
	return false
}

func hasDotnetListOnlyArg(args []string) bool {
	return hasNormalizedArgOrTruthyAssignment(args, "--list-tests", "-t", "--listtests", "/listtests", "/lt")
}

func dotnetMSBuildRunsVerification(args []string) bool {
	for _, arg := range args {
		arg = normalizeShellExecutableToken(arg)
		if arg == "test" || arg == "build" {
			return true
		}
		if strings.HasPrefix(arg, "-t:") || strings.HasPrefix(arg, "/t:") || strings.HasPrefix(arg, "-target:") || strings.HasPrefix(arg, "/target:") {
			_, targetText, _ := strings.Cut(arg, ":")
			for _, target := range strings.FieldsFunc(targetText, func(r rune) bool { return r == ';' || r == ',' }) {
				target = strings.ToLower(strings.TrimSpace(target))
				if target == "test" || target == "build" {
					return true
				}
			}
		}
	}
	return false
}

func mavenRunsVerification(args []string) bool {
	if hasMavenSkipTestsArg(args) {
		return false
	}
	args = stripMavenOptions(args)
	return firstArgIn(args, "test", "verify")
}

func hasMavenSkipTestsArg(args []string) bool {
	for _, arg := range args {
		arg = strings.ToLower(normalizeShellExecutableToken(arg))
		for _, key := range []string{"-dskiptests", "-dmaven.test.skip"} {
			if arg == key {
				return true
			}
			if strings.HasPrefix(arg, key+"=") {
				value := strings.TrimSpace(strings.TrimPrefix(arg, key+"="))
				if value != "" && value != "false" && value != "0" {
					return true
				}
			}
		}
	}
	return false
}

func cargoRunsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	subcommand := normalizeShellExecutableToken(args[0])
	switch subcommand {
	case "test":
		return !hasCargoTestNoRunArg(args[1:])
	case "clippy":
		return !hasMutatingVerificationArg(args[1:])
	case "check", "build":
		return true
	}
	return false
}

func hasCargoTestNoRunArg(args []string) bool {
	for _, arg := range args {
		arg = normalizeShellExecutableToken(arg)
		switch arg {
		case "--no-run", "--help", "-h", "--list":
			return true
		}
	}
	return false
}

func stripMavenOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case mavenOptionConsumesValue(arg):
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

func mavenOptionConsumesValue(arg string) bool {
	switch arg {
	case "-f", "--file", "-pl", "--projects", "-rf", "--resume-from", "-gs", "--global-settings", "-s", "--settings", "-t", "--toolchains", "-p", "--activate-profiles":
		return true
	}
	return false
}

func gradleRunsVerification(args []string) bool {
	if hasNormalizedArgOrTruthyAssignment(args, "--dry-run", "-m") || hasGradleExcludedVerificationTask(args) {
		return false
	}
	for _, arg := range args {
		if isGradleVerificationTask(arg) {
			return true
		}
	}
	return false
}

func hasGradleExcludedVerificationTask(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := normalizeShellExecutableToken(args[i])
		switch {
		case arg == "-x" || arg == "--exclude-task":
			if i+1 < len(args) && isGradleVerificationTask(args[i+1]) {
				return true
			}
		case strings.HasPrefix(arg, "--exclude-task="):
			if isGradleVerificationTask(strings.TrimPrefix(arg, "--exclude-task=")) {
				return true
			}
		case strings.HasPrefix(arg, "-x") && len(arg) > len("-x"):
			value := strings.TrimPrefix(arg, "-x")
			value = strings.TrimPrefix(value, "=")
			if isGradleVerificationTask(value) {
				return true
			}
		}
	}
	return false
}

func isGradleVerificationTask(arg string) bool {
	arg = normalizeShellExecutableToken(arg)
	if arg == "" || strings.HasPrefix(arg, "-") || isShellEnvAssignment(arg) {
		return false
	}
	arg = strings.Trim(arg, ":")
	if idx := strings.LastIndex(arg, ":"); idx >= 0 {
		arg = arg[idx+1:]
	}
	switch arg {
	case "test", "check", "build":
		return true
	}
	return false
}

func bunRunsVerification(args []string) bool {
	if firstArgIn(args, "test") {
		return true
	}
	if len(args) > 1 && normalizeShellExecutableToken(args[0]) == "run" {
		scriptArgs := stripPackageManagerOptions(args[1:])
		return len(scriptArgs) > 0 && isVerificationScriptName(scriptArgs[0]) && !hasMutatingVerificationArg(scriptArgs)
	}
	return false
}

func denoRunsVerification(args []string) bool {
	if firstArgIn(args, "test", "lint", "check") {
		return true
	}
	if len(args) > 1 && normalizeShellExecutableToken(args[0]) == "task" {
		taskArgs := stripPackageManagerOptions(args[1:])
		return len(taskArgs) > 0 && isVerificationScriptName(taskArgs[0]) && !hasMutatingVerificationArg(taskArgs)
	}
	return false
}

func dartRunsVerification(args []string) bool {
	args = stripDartOptions(args)
	if len(args) == 0 {
		return false
	}
	return firstArgIn(args, "test", "analyze", "compile")
}

func flutterRunsVerification(args []string) bool {
	args = stripDartOptions(args)
	if len(args) == 0 {
		return false
	}
	return firstArgIn(args, "test", "analyze", "build")
}

func stripDartOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case dartOptionConsumesValue(arg):
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "--define="), strings.HasPrefix(arg, "--device-id="):
			args = args[1:]
			continue
		case strings.HasPrefix(arg, "-"):
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func dartOptionConsumesValue(arg string) bool {
	switch arg {
	case "-d", "--device-id", "--define", "--dart-define", "--target", "-t", "--flavor":
		return true
	}
	return false
}

func mixRunsVerification(args []string) bool {
	args = stripMixOptions(args)
	if len(args) == 0 {
		return false
	}
	switch normalizeShellExecutableToken(args[0]) {
	case "test", "compile":
		return true
	case "credo", "dialyzer":
		return true
	}
	return false
}

func stripMixOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case mixOptionConsumesValue(arg):
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "--profile="):
			args = args[1:]
			continue
		case strings.HasPrefix(arg, "-"):
			args = args[1:]
			continue
		}
		break
	}
	return args
}

func mixOptionConsumesValue(arg string) bool {
	switch arg {
	case "--profile":
		return true
	}
	return false
}

func goRunsVerification(args []string) bool {
	args = stripGoGlobalOptions(args)
	if len(args) == 0 {
		return false
	}
	if normalizeShellExecutableToken(args[0]) == "test" {
		return !hasGoTestNoRunArg(args[1:])
	}
	return firstArgIn(args, "vet", "build")
}

func hasGoTestNoRunArg(args []string) bool {
	runPatternRunsNoTests := false
	hasExplicitWorkload := false
	for i := 0; i < len(args); i++ {
		arg := normalizeShellExecutableToken(args[i])
		if arg == "-c" || strings.HasPrefix(arg, "-c=") {
			return true
		}
		if arg == "-count" && i+1 < len(args) && goTestCountRunsNoTests(args[i+1]) {
			return true
		}
		if strings.HasPrefix(arg, "-count=") && goTestCountRunsNoTests(strings.TrimPrefix(arg, "-count=")) {
			return true
		}
		if arg == "-list" || strings.HasPrefix(arg, "-list=") {
			return true
		}
		if goTestArgNamesWorkload(arg, "-bench", "-fuzz") {
			hasExplicitWorkload = true
			continue
		}
		if arg == "-run" && i+1 < len(args) && goTestRunPatternRunsNoTests(args[i+1]) {
			runPatternRunsNoTests = true
		}
		if strings.HasPrefix(arg, "-run=") && goTestRunPatternRunsNoTests(strings.TrimPrefix(arg, "-run=")) {
			runPatternRunsNoTests = true
		}
	}
	return runPatternRunsNoTests && !hasExplicitWorkload
}

func goTestArgNamesWorkload(arg string, names ...string) bool {
	for _, name := range names {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func goTestRunPatternRunsNoTests(pattern string) bool {
	pattern = strings.Trim(strings.ToLower(strings.TrimSpace(pattern)), "'\"")
	return pattern == "^$" || pattern == "^$/" || pattern == "$^"
}

func goTestCountRunsNoTests(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), "'\"")
	return value == "0"
}

func stripGoGlobalOptions(args []string) []string {
	for len(args) > 0 {
		arg := normalizeShellExecutableToken(args[0])
		switch {
		case arg == "--" || isShellEnvAssignment(arg):
			args = args[1:]
			continue
		case arg == "-c":
			if len(args) > 1 {
				args = args[2:]
			} else {
				args = args[1:]
			}
			continue
		case strings.HasPrefix(arg, "-c="):
			args = args[1:]
			continue
		}
		break
	}
	return args
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
		parts = append(parts, fmt.Sprintf("还有 %d 条未通过命令未展开", remaining))
	}
	return strings.Join(parts, "; ")
}

func compactFailedVerificationCommandResults(commands []CodingSubAgentCommandResult) string {
	commands = dedupeSubAgentCommandFailuresForSummary(commands)
	entries := selectFailedVerificationSummaryEntries(commands, codingSubAgentFailedVerificationSummaryMax)
	parts := make([]string, 0, len(entries)+1)
	for _, command := range entries {
		text := truncateRunesForSubAgent(command.Command, 160)
		if summary := commandResultDiagnosticLine(command.Summary); summary != "" {
			text += ": " + truncateRunesForSubAgent(summary, codingSubAgentCommandOutputLineMaxRunes)
		}
		parts = append(parts, text)
	}
	if remaining := len(commands) - len(entries); remaining > 0 {
		parts = append(parts, fmt.Sprintf("还有 %d 条未通过命令未展开", remaining))
	}
	return strings.Join(parts, "; ")
}

func dedupeSubAgentCommandFailuresForSummary(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	if len(commands) < 2 {
		return commands
	}
	latestByKey := make(map[string]int, len(commands))
	for i, cmd := range commands {
		latestByKey[subAgentCommandFailureSummaryDedupeKey(cmd, i)] = i
	}
	out := make([]CodingSubAgentCommandResult, 0, len(latestByKey))
	for i, cmd := range commands {
		key := subAgentCommandFailureSummaryDedupeKey(cmd, i)
		if latestByKey[key] == i {
			out = append(out, cmd)
		}
	}
	return out
}

func subAgentCommandFailureSummaryDedupeKey(cmd CodingSubAgentCommandResult, index int) string {
	target := subAgentCommandFailureResolutionKey(cmd)
	diagnostic := strings.ToLower(strings.Join(strings.Fields(commandResultDiagnosticLine(cmd.Summary)), " "))
	if diagnostic == "" {
		diagnostic = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(cmd.Summary)), " "))
	}
	if target == "" && diagnostic == "" {
		return fmt.Sprintf("idx:%d", index)
	}
	return target + "\x00" + diagnostic
}

func selectFailedVerificationSummaryEntries(commands []CodingSubAgentCommandResult, maxItems int) []CodingSubAgentCommandResult {
	if maxItems <= 0 || len(commands) <= maxItems {
		return commands
	}
	out := append([]CodingSubAgentCommandResult(nil), commands...)
	sort.SliceStable(out, func(i, j int) bool {
		leftActionable := isActionableCommandDiagnosticLine(commandResultDiagnosticLine(out[i].Summary))
		rightActionable := isActionableCommandDiagnosticLine(commandResultDiagnosticLine(out[j].Summary))
		if leftActionable != rightActionable {
			return leftActionable
		}
		if out[i].seq != 0 && out[j].seq != 0 && out[i].seq != out[j].seq {
			return out[i].seq > out[j].seq
		}
		return false
	})
	return out[:maxItems]
}

func summarizeSubAgentExploration(filesModified, filesRead []string, searches []CodingSubAgentSearchResult, exploredBeforeFirstEdit bool) (codingSubAgentQualityStatus, string) {
	if len(filesModified) == 0 {
		return codingSubAgentQualityNotNeeded, "未检测到既有文件修改，跳过探索要求。"
	}
	successfulSearches := countSuccessfulSubAgentExplorationSearches(searches)
	if !exploredBeforeFirstEdit {
		return codingSubAgentQualityMissing, "检测到既有文件修改，但首次修改前没有记录成功搜索或文件读取。"
	}
	if successfulSearches > 0 {
		return codingSubAgentQualityExplored, fmt.Sprintf("首次修改前已探索；过程中运行了 %d 次成功搜索，并读取了 %d 个文件。", successfulSearches, len(filesRead))
	}
	if len(filesRead) > 0 {
		return codingSubAgentQualityReadOnly, fmt.Sprintf("首次修改前已读取文件；未记录成功搜索，但共读取了 %d 个文件。", len(filesRead))
	}
	return codingSubAgentQualityMissing, "检测到既有文件修改，但没有记录成功搜索或文件读取。"
}

func existingSubAgentModifiedFiles(filesModified, filesCreated []string) []string {
	created := make(map[string]bool, len(filesCreated))
	for _, file := range uniqueSortedSubAgentStrings(filesCreated) {
		if key := subAgentPathEvidenceKey(file); key != "" {
			created[key] = true
		}
	}
	var existing []string
	for _, file := range uniqueSortedSubAgentStrings(filesModified) {
		if key := subAgentPathEvidenceKey(file); key != "" && !created[key] {
			existing = append(existing, file)
		}
	}
	return existing
}

func subAgentPathEvidenceKey(path string) string {
	key := filepath.ToSlash(strings.TrimSpace(path))
	if key == "" {
		return ""
	}
	cleaned := pathpkg.Clean(key)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func countExistingSubAgentModifiedFiles(filesModified, filesCreated []string) int {
	return len(existingSubAgentModifiedFiles(filesModified, filesCreated))
}

func countSuccessfulSubAgentSearches(searches []CodingSubAgentSearchResult) int {
	successfulSearches := 0
	for _, s := range searches {
		if s.Succeeded && !subAgentSearchSuccessLooksEmpty(s) {
			successfulSearches++
		}
	}
	return successfulSearches
}

func countSuccessfulSubAgentExplorationSearches(searches []CodingSubAgentSearchResult) int {
	successfulSearches := 0
	for _, s := range searches {
		if subAgentSearchProvidesExplorationEvidence(s) {
			successfulSearches++
		}
	}
	return successfulSearches
}

func subAgentSearchProvidesExplorationEvidence(search CodingSubAgentSearchResult) bool {
	if !search.Succeeded || subAgentSearchSuccessLooksEmpty(search) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(search.Tool)) {
	case "list_directory", "ssh_list_dir":
		return false
	}
	return true
}

func subAgentSearchSuccessLooksEmpty(search CodingSubAgentSearchResult) bool {
	if !search.Succeeded {
		return false
	}
	summary := strings.TrimSpace(search.Summary)
	if summary == "" || summary == "(无输出)" {
		return true
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(summary), " "))
	for _, phrase := range []string{
		"no results",
		"no results found",
		"0 results",
		"found 0 results",
		"no matches",
		"0 matches",
		"found 0 matches",
		"no files matched",
		"no files found",
		"matched 0 files",
		"found 0 files",
		"0 files found",
	} {
		if subAgentSummaryContainsEmptyVerificationPhrase(normalized, phrase) {
			return true
		}
	}
	return false
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
	if status != TaskExecPassed {
		return status, errMsg
	}
	switch verificationStatus {
	case codingSubAgentQualityFailed:
		if strings.TrimSpace(verificationSummary) == "" {
			verificationSummary = "验证命令未通过"
		}
		return TaskExecFailed, compactSubAgentErrorSummary(verificationSummary)
	case codingSubAgentQualityMissing:
		if strings.TrimSpace(verificationSummary) == "" {
			verificationSummary = "no verification command ran after file changes"
		}
		return TaskExecFailed, compactSubAgentErrorSummary(verificationSummary)
	}
	return status, errMsg
}

func applySubAgentExplorationOutcome(status TaskExecStatus, errMsg string, explorationStatus codingSubAgentQualityStatus, explorationSummary string, modifiedExistingCount int) (TaskExecStatus, string) {
	if status != TaskExecPassed || explorationStatus != codingSubAgentQualityMissing || modifiedExistingCount == 0 {
		return status, errMsg
	}
	if strings.TrimSpace(explorationSummary) == "" {
		explorationSummary = "no exploration before editing existing files"
	}
	return TaskExecFailed, compactSubAgentErrorSummary(explorationSummary)
}

func applySubAgentGuardrailOutcome(status TaskExecStatus, errMsg string, violations []CodingSubAgentGuardrailViolation) (TaskExecStatus, string) {
	if len(violations) == 0 {
		return status, errMsg
	}
	summary := fmt.Sprintf("%d guardrail block(s)", len(violations))
	if entries := aggregateGuardrailViolations(violations); len(entries) > 0 {
		v := entries[0].Violation
		detail := firstNonEmptySubAgentString(firstLine(v.Summary), strings.TrimSpace(v.Command), strings.TrimSpace(v.Path), strings.TrimSpace(v.Tool))
		if detail != "" {
			summary = fmt.Sprintf("%s: %s", summary, detail)
		}
	}
	summary = compactSubAgentErrorSummary(summary)
	if status != TaskExecPassed {
		return status, appendSubAgentFailureDiagnostic(errMsg, summary)
	}
	return TaskExecFailed, summary
}

func appendSubAgentFailureDiagnostic(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	if existing == "" {
		return compactSubAgentErrorSummary(addition)
	}
	if addition == "" || strings.Contains(existing, addition) {
		return compactSubAgentErrorSummary(existing)
	}
	return compactSubAgentErrorSummary(existing + "; " + addition)
}

func firstNonEmptySubAgentString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func applySubAgentDiffOutcome(status TaskExecStatus, errMsg string, diffChecked bool, diffSummary string, modifiedCount int) (TaskExecStatus, string) {
	if status != TaskExecPassed || diffChecked || modifiedCount == 0 {
		return status, errMsg
	}
	if strings.TrimSpace(diffSummary) == "" {
		diffSummary = "git diff self-check did not complete"
	}
	return TaskExecFailed, compactSubAgentErrorSummary(diffSummary)
}

func applySubAgentQualityOutcome(status TaskExecStatus, errMsg string, qualityStatus codingSubAgentQualityStatus, qualitySummary string, qualityIssueCount int) (TaskExecStatus, string) {
	if qualityStatus != codingSubAgentQualityFailed {
		return status, errMsg
	}
	qualitySummary = subAgentQualityFailureDiagnostic(qualitySummary, qualityIssueCount)
	if status != TaskExecPassed {
		return status, appendSubAgentFailureDiagnostic(errMsg, qualitySummary)
	}
	return TaskExecFailed, compactSubAgentErrorSummary(qualitySummary)
}

func subAgentQualityFailureDiagnostic(qualitySummary string, qualityIssueCount int) string {
	qualitySummary = strings.TrimSpace(qualitySummary)
	if qualitySummary == "" {
		qualitySummary = "coding SubAgent quality audit failed"
	} else if qualityIssueCount > 0 {
		qualitySummary = fmt.Sprintf("coding SubAgent quality audit failed: %s (%d issue(s))", qualitySummary, qualityIssueCount)
	} else {
		qualitySummary = "coding SubAgent quality audit failed: " + qualitySummary
	}
	return compactSubAgentErrorSummary(qualitySummary)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}

func subAgentEventSummaryLine(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	fallback := firstLine(summary)
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || subAgentEventSummaryLineLooksLikeHeader(line) {
			continue
		}
		return line
	}
	return fallback
}

func subAgentEventSummaryLineLooksLikeHeader(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	if strings.HasPrefix(line, "#") {
		return true
	}
	trimmed := strings.TrimSpace(strings.TrimRight(line, ":："))
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case "summary", "audit", "quality audit", "quality summary", "verification summary", "exploration summary", "command summary", "guardrail summary",
		"质量审计", "质量摘要", "验证摘要", "探索摘要", "命令摘要", "护栏摘要":
		return true
	default:
		return false
	}
}

func subAgentVerificationOutcomeStatus(status codingSubAgentQualityStatus) codingSubAgentQualityStatus {
	if status == codingSubAgentQualityMissing {
		return codingSubAgentQualityFailed
	}
	return status
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

// emitReadFilePreview sends a code:file_update event for a file that was
// successfully read, so the code preview panel shows it during execution.
// It reads the raw file content from disk (not the formatted tool output).
func (c *codingSubAgentCallbacks) emitReadFilePreview(filePath string) {
	if c == nil || c.subagent == nil || c.subagent.handler == nil || !codingTaskShouldEnableSourcePreview(c.task) {
		return
	}
	app := c.subagent.handler.app
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	projectPath := c.projectPath()
	normalized := normalizeSubAgentCodeEventPath(filePath, projectPath)
	if normalized.displayPath == "" || normalized.absPath == "" {
		return
	}
	data, err := os.ReadFile(normalized.absPath)
	if err != nil {
		return
	}
	// Skip large or binary files.
	if len(data) > 256*1024 || !isCodePreviewTextContent(data) {
		return
	}
	fileName := filepath.Base(normalized.displayPath)
	// Pure-coding full environment: force-open so exploration populates the
	// right-hand panel even when the agent only reads existing sources.
	forceOpen := c.subagent != nil && c.subagent.isFullEnvironment()
	app.codeEventEmitter.EmitCodeFileEvent(CodeFileEvent{
		SessionID:       c.codeSessionID(),
		FilePath:        normalized.displayPath,
		FileName:        fileName,
		AbsPath:         normalized.absPath,
		Content:         string(data),
		OpType:          "read",
		Language:        detectLanguageFromExt(fileName),
		ForceOpen:       forceOpen,
		AutoOpenPreview: forceOpen,
		// Route with tab project path (managed task dir), not exec/working_dir.
		ProjectPath: c.previewRouteProjectPath(),
	})
}

// codeSessionID returns the active code session ID for preview routing.
func (c *codingSubAgentCallbacks) codeSessionID() string {
	if c != nil && c.subagent != nil && c.subagent.loopCtx != nil {
		if sessionID := strings.TrimSpace(c.subagent.loopCtx.codeSessionID); sessionID != "" {
			return sessionID
		}
	}
	return "subagent-workflow"
}

// previewRouteProjectPath is the frontend tab project path for code events.
// Pure-coding tabs use a managed task dir as identity while tools run under
// working_dir; routing with execDir causes shouldAcceptCodeEventForProject to drop events.
func (c *codingSubAgentCallbacks) previewRouteProjectPath() string {
	if c != nil && c.subagent != nil && c.subagent.loopCtx != nil {
		if tab := projectPathFromSessionOwnerID(c.subagent.loopCtx.UserID); tab != "" {
			return tab
		}
	}
	return c.projectPath()
}

func (c *codingSubAgentCallbacks) emitToolFinishedEvent(name, argsJSON, result string, outcome codingToolOutcome, duration time.Duration) {
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
	event.Command = codingToolEventCommand(name, argsJSON)
	event.Outcome = string(outcome)
	if outcome != codingToolOutcomeSuccess {
		event.Summary = compactCodingToolResultSummary(result)
		if codingSubAgentFailedToolLogIsDiagnostic(name, argsJSON, codingToolExecutionResult{Text: result, Outcome: outcome}) {
			event.Severity = "diagnostic"
		}
	}
	durationMS := duration.Milliseconds()
	if durationMS == 0 {
		durationMS = 1
	}
	event.DurationMS = durationMS
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func codingToolEventCommand(name, argsJSON string) string {
	name = canonicalCodingSubAgentToolName(name)
	if name != "bash" && name != remoteSSHBashToolName {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	command, _ := args["command"].(string)
	return strings.TrimSpace(redactCodingSubAgentFreeformLogText(command))
}

func compactCodingToolResultSummary(result string) string {
	result = firstDiagnosticCodingToolResultLine(result)
	if result == "" {
		return ""
	}
	return truncateRunesForSubAgent(result, 180)
}

func firstDiagnosticCodingToolResultLine(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(result, "\r\n", "\n"), "\n")
	firstStderr := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if detail, ok := strings.CutPrefix(line, "[stderr]"); ok {
			detail = strings.TrimSpace(detail)
			if detail == "" {
				continue
			}
			if firstStderr == "" {
				firstStderr = detail
			}
			if isLikelyCodingToolFailureDiagnostic(detail) {
				return detail
			}
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || isCodingToolCommandStatusLine(line) {
			continue
		}
		if isLikelyCodingToolFailureDiagnostic(line) {
			return line
		}
	}
	if firstStderr != "" {
		return firstStderr
	}
	return firstLine(result)
}

func isCodingToolCommandStatusLine(line string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(line, "command exited with code") ||
		strings.HasPrefix(line, "command timed out") ||
		strings.HasPrefix(line, "command cancelled") ||
		line == "fail" ||
		strings.HasPrefix(line, "fail\t") ||
		isCodingToolExitStatusOnlyLine(line)
}

func isCodingToolExitStatusOnlyLine(line string) bool {
	line = strings.Trim(strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(line))), " "), ".")
	for _, prefix := range []string{"error:", "error "} {
		if strings.HasPrefix(line, prefix) {
			line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			break
		}
	}
	for _, prefix := range []string{"command failed:", "process failed:", "script failed:", "task failed:"} {
		if strings.HasPrefix(line, prefix) {
			line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			break
		}
	}
	for _, exact := range []string{"command failed", "process failed", "script failed", "task failed"} {
		if line == exact {
			return true
		}
	}
	for _, prefix := range []string{
		"exit status ",
		"exit code ",
		"exited with code ",
		"process exited with code ",
		"process completed with exit code ",
		"command exited with code ",
		"command failed with exit code ",
		"command failed with code ",
		"script failed with exit code ",
		"task failed with exit code ",
	} {
		if strings.HasPrefix(line, prefix) && codingToolExitStatusSuffixIsCode(strings.TrimSpace(strings.TrimPrefix(line, prefix))) {
			return true
		}
	}
	return false
}

func codingToolExitStatusSuffixIsCode(suffix string) bool {
	suffix = strings.Trim(suffix, " .:()[]")
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isLikelyCodingToolFailureDiagnostic(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	if isNonFailureDiagnosticNoise(lower) {
		return false
	}
	for _, marker := range []string{
		"error:",
		"error ",
		"error[",
		"errors",
		"fatal:",
		"panic:",
		"fail:",
		"fail ",
		"failed",
		"failure",
		"build failed",
		"compilation failed",
		"typecheck failed",
		"traceback",
		"segmentation fault",
		"signal: killed",
		"exit status",
		"permission denied",
		"attributeerror:",
		"importerror:",
		"modulenotfounderror:",
		"nameerror:",
		"referenceerror:",
		"syntaxerror:",
		"typeerror:",
		"undefined:",
		"cannot find",
		"not found",
		"does not exist",
		"no such file",
		"syntax error",
		"type error",
		"assert ",
		"assertion",
		"expected",
		"received",
		"want ",
		"want:",
		"exception",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isNonFailureDiagnosticNoise(lower string) bool {
	for _, marker := range []string{
		"0 error",
		"0 errors",
		"no error",
		"no errors",
		"0 assertion",
		"0 assertions",
		"no assertion",
		"no assertions",
		"without error",
		"without errors",
	} {
		if subAgentSummaryContainsEmptyVerificationPhrase(lower, marker) {
			return true
		}
	}
	return false
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
	case modifiedCount > 0:
		event.Outcome = "failed"
		if strings.TrimSpace(diffSummary) != "" {
			event.Summary = truncateRunesForSubAgent(firstLine(diffSummary), 240)
		} else {
			event.Summary = "git diff self-check did not complete"
		}
	default:
		event.Outcome = "skipped"
		event.Summary = "no modified files"
	}
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) emitQualitySummaryEvent(explorationStatus, verificationStatus codingSubAgentQualityStatus, diffChecked bool, filesModified, filesCreated []string, commands []CodingSubAgentCommandResult, lastEditSeq uint64, guardrails []CodingSubAgentGuardrailViolation, dynamicTools []CodingSubAgentDynamicToolResult) {
	outcome, summary, count := summarizeSubAgentQuality(explorationStatus, verificationStatus, diffChecked, filesModified, filesCreated, commands, lastEditSeq, guardrails, dynamicTools)
	c.emitQualitySummaryEventWithAudit(outcome, summary, count)
}

func (c *codingSubAgentCallbacks) emitQualitySummaryEventWithAudit(outcome codingSubAgentQualityStatus, summary string, count int) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, c.task, title, "")
	event.Event = codingAgentEventKindQualitySummary.String()
	event.Outcome = outcome.String()
	event.Summary = truncateRunesForSubAgent(subAgentEventSummaryLine(summary), 240)
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
	event.Summary = truncateRunesForSubAgent(subAgentEventSummaryLine(summary), 240)
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
			parts = append(parts, subAgentEventSummaryLine(v.Summary))
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
	event.Summary = truncateRunesForSubAgent(subAgentEventSummaryLine(summary), 240)
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
	event.Summary = truncateRunesForSubAgent(subAgentEventSummaryLine(summary), 240)
	event.Count = count
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

func (c *codingSubAgentCallbacks) OnToolResult(name string) {}

func (c *codingSubAgentCallbacks) ShouldStop() bool {
	if c != nil && c.subagent != nil && c.subagent.runtimeAttempt != nil && c.subagent.runtimeStore != nil {
		current, err := c.subagent.runtimeStore.GetAttempt(c.subagent.runtimeAttempt.AttemptID)
		// Runtime state is authoritative once the agent is ledger-bound. A
		// cancelled, interrupted, waiting-child, or terminal Attempt must not
		// admit another model/tool turn from a stale callback.
		if err != nil || current.Status != codingruntime.TaskRunning {
			return true
		}
	}
	if c != nil && c.subagent != nil && c.subagent.executionCtx != nil && c.subagent.executionCtx.Err() != nil {
		return true
	}
	if c != nil && c.subagent != nil && c.subagent.loopCtx != nil {
		return c.subagent.loopCtx.IsCancelled()
	}
	return false
}

func (c *codingSubAgentCallbacks) buildTaskUserMessage() string {
	var b strings.Builder
	if codingTaskLooksInquiry(c.task) {
		b.WriteString(fmt.Sprintf("请回答下面的仓库/代码问题。先做必要的只读检索，再给出简洁、可核查的结论；不要修改文件、不要写测试、不要创建实现任务。\\n\\n## 问题\\n%s\\n\\n", compactSubAgentTaskDescription(c.task.Description)))
		b.WriteString("**只读工作要求**：\\n")
		b.WriteString("1. 项目根目录存在 .codegraph/ 时，优先使用 codegraph explore / codegraph node；否则使用 list_directory、glob、read_file 或搜索。\\n")
		b.WriteString("2. 最终先回答用户问题，再简要列出查看过的关键目录或文件；不要输出 TDD、任务编号或完整工具流水。\\n")
		b.WriteString("3. 未明确要求修改时，绝不写入文件、运行构建或测试。\\n")
		return compactCodingSubAgentTaskUserMessage(b.String())
	}
	if codingTaskLooksOperational(c.task) {
		b.WriteString(fmt.Sprintf("请执行以下操作任务（运行/构建/演示，不要改代码）：\n\n## T%d: %s\n\n", taskDisplayNumber(c.task), compactSubAgentTaskTitle(c.task.Title)))
		if c.task.Description != "" {
			b.WriteString(compactSubAgentTaskDescription(c.task.Description))
			b.WriteString("\n\n")
		}
		appendCodingSubAgentOperationalChecklist(&b)
		if len(c.prevOutputs) > 0 {
			b.WriteString("**前置任务上下文**（可用来定位已生成的可执行文件）：\n")
			appendSubAgentBulletList(&b, c.prevOutputs, codingSubAgentPrevOutputsMax, codingSubAgentPromptBulletMaxRunes)
			b.WriteString("\n")
		}
		return compactCodingSubAgentTaskUserMessage(b.String())
	}
	b.WriteString(fmt.Sprintf("请执行以下编码任务：\n\n## T%d: %s\n\n", taskDisplayNumber(c.task), compactSubAgentTaskTitle(c.task.Title)))
	if c.task.Description != "" {
		b.WriteString(compactSubAgentTaskDescription(c.task.Description))
		b.WriteString("\n\n")
	}
	appendCodingSubAgentPreflightChecklist(&b)
	if len(c.prevOutputs) > 0 {
		b.WriteString("**前置任务上下文**：\n")
		appendSubAgentBulletList(&b, c.prevOutputs, codingSubAgentPrevOutputsMax, codingSubAgentPromptBulletMaxRunes)
		b.WriteString("\n")
	}
	if len(c.task.Files) > 0 {
		b.WriteString("**涉及文件**：\n")
		appendSubAgentBulletList(&b, c.task.Files, codingSubAgentTaskFilesMax, codingSubAgentPromptBulletMaxRunes)
		b.WriteString("\n")
	}
	if len(c.task.AcceptanceCriteria) > 0 {
		b.WriteString("**验收标准**：\n")
		appendSubAgentAcceptanceCriteriaList(&b, c.task.AcceptanceCriteria, codingSubAgentAcceptanceCriteriaMax, codingSubAgentPromptBulletMaxRunes)
		b.WriteString("\n**验收验证要求**：将最终验证命令或检查结果对应到上述验收标准；无法自动验证的标准必须说明原因，不要只笼统声称完成。\n")
	}
	return compactCodingSubAgentTaskUserMessage(b.String())
}

func compactCodingSubAgentTaskUserMessage(message string) string {
	const overheadBudget = codingSubAgentTaskTitleMaxRunes + 1000
	return truncateRunesForSubAgent(message, codingSubAgentTaskDescriptionMaxRunes+overheadBudget)
}

func appendCodingSubAgentPreflightChecklist(b *strings.Builder) {
	if b == nil {
		return
	}
	b.WriteString("**Before editing**:\n")
	b.WriteString("1. If project root has .codegraph/, locate relevant code with `codegraph explore` / `codegraph node`; otherwise use Glob/ripgrep/read_file.\n")
	b.WriteString("2. State likely files and risk/impact.\n")
	b.WriteString("3. Choose the minimal edit approach.\n")
	b.WriteString("4. If this is a retry, use retry context and avoid repeating the failed approach.\n")
	b.WriteString("5. For bug fixes, call code_navigation, reproduce or explain why not, reject a plausible alternative, and make an explicit research decision. Unknown/current/third-party facts require web_search of the exact error plus component/version; then submit report_localization before editing existing code.\n")
	b.WriteString("\n**Before finalizing**:\n")
	b.WriteString("1. After the last edit, run matching verification command(s): test/build/lint/typecheck. Do not present pre-edit verification as final verification.\n")
	b.WriteString("2. Make the final summary match audit evidence: name actual modified/created file paths, list only verification commands you really ran after editing, map acceptance criteria when present, explain any scope expansion, and include remaining risk or say no known remaining risk.\n\n")
}

func appendCodingSubAgentOperationalChecklist(b *strings.Builder) {
	if b == nil {
		return
	}
	b.WriteString("**操作任务要求**：\n")
	b.WriteString("1. 先在项目目录定位可执行文件/脚本/构建产物（list_directory / glob / read 少量文件即可）。\n")
	b.WriteString("2. 必须用 bash 实际执行启动/构建/演示命令；禁止只文字回复「已运行」。\n")
	b.WriteString("3. GUI/游戏若是阻塞进程：用合适 timeout 启动并回报是否成功拉起；不要为「通过质量门禁」去改代码或造测试。\n")
	b.WriteString("4. 最终摘要写清：执行了什么命令、退出码/关键输出、是否成功。\n\n")
}

// codingTaskRequestKind is propagated from the root workbench classifier. A
// task not launched by that workbench safely retains implementation behavior.
func codingTaskRequestKind(task *TaskItem) codingRequestKind {
	if task != nil {
		switch task.RequestKind {
		case codingRequestInquiry, codingRequestOperational, codingRequestImplementation:
			return task.RequestKind
		}
	}
	return codingRequestImplementation
}

func codingTaskLooksOperational(task *TaskItem) bool {
	return codingTaskRequestKind(task) == codingRequestOperational
}

func codingTaskLooksInquiry(task *TaskItem) bool {
	return codingTaskRequestKind(task) == codingRequestInquiry
}

// codingTaskShouldEnableSourcePreview keeps the source/diff affordance scoped
// to implementation work. The decision is made once by the root workbench and
// carried with the task; subagents never re-infer intent from task wording.
func codingTaskShouldEnableSourcePreview(task *TaskItem) bool {
	return codingTaskRequestKind(task) == codingRequestImplementation
}

// summarizeOperationalSubAgentQuality requires a successful launch/build-style
// bash command. Pure listing (dir/ls/pwd), mkdir, or read-only tools do not count.
func summarizeOperationalSubAgentQuality(audit codingSubAgentAudit, result agent.LoopResult) (codingSubAgentQualityStatus, string, int) {
	successfulLaunch := 0
	failedLaunch := 0
	otherBashSuccess := 0
	for _, cmd := range audit.AllCommandsRun {
		switch classifyOperationalShellCommand(cmd.Command) {
		case operationalShellLaunchBuild:
			if cmd.Succeeded {
				successfulLaunch++
			} else {
				failedLaunch++
			}
		case operationalShellInspection:
			// ignore for pass/fail primary evidence
		default:
			if cmd.Succeeded {
				otherBashSuccess++
			}
		}
	}
	if successfulLaunch > 0 {
		return codingSubAgentQualityPassed, "operational run: launch/build command evidence present", 0
	}
	if result.ToolCalls == 0 {
		return codingSubAgentQualityFailed, "operational task ran no tools (need bash to launch/build)", 1
	}
	if failedLaunch > 0 {
		return codingSubAgentQualityFailed, "operational task: launch/build command(s) failed", 1
	}
	if otherBashSuccess > 0 {
		return codingSubAgentQualityFailed, "operational task: ran shell commands but none looked like launch/build", 1
	}
	// Tools ran (list_directory / read_file / dir) but never launched/built.
	return codingSubAgentQualityFailed, "operational task: no launch/build command executed", 1
}

type operationalShellClass int

const (
	operationalShellUnknown operationalShellClass = iota
	operationalShellInspection
	operationalShellLaunchBuild
)

// classifyOperationalShellCommand classifies a bash command for ops evidence.
// Compound commands (dir && .\snake.exe) are launch/build if any segment is.
func classifyOperationalShellCommand(command string) operationalShellClass {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return operationalShellInspection
	}
	segments := shellCommandSegments(normalized)
	if len(segments) == 0 {
		return operationalShellInspection
	}
	sawLaunch := false
	sawNonInspection := false
	sawSegment := false
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		sawSegment = true
		if isOperationalLaunchOrBuildSegment(segment) {
			sawLaunch = true
			continue
		}
		if !isOperationalInspectionOnlySegment(segment) {
			sawNonInspection = true
		}
	}
	if !sawSegment {
		return operationalShellInspection
	}
	if sawLaunch {
		return operationalShellLaunchBuild
	}
	if sawNonInspection {
		return operationalShellUnknown
	}
	return operationalShellInspection
}

// isOperationalInspectionOnlyCommand is kept for tests and callers that only
// need the denylist view of pure listing/env commands.
func isOperationalInspectionOnlyCommand(command string) bool {
	return classifyOperationalShellCommand(command) == operationalShellInspection
}

func isOperationalInspectionOnlySegment(segment []string) bool {
	if len(segment) == 0 {
		return true
	}
	base := commandNameBase(segment[0])
	switch base {
	case "ls", "dir", "pwd", "cd", "echo", "type", "cat", "head", "tail",
		"where", "which", "get-childitem", "gci", "get-location", "gl",
		"test-path", "resolve-path", "get-item", "gi", "get-content", "gc",
		"tree", "find", "stat", "file", "wc", "du", "df",
		"env", "printenv", "set", "whoami",
		"mkdir", "md", "touch", "new-item", "ni":
		return true
	default:
		return false
	}
}

// isOperationalLaunchOrBuildSegment reports segments that count as run/build evidence.
func isOperationalLaunchOrBuildSegment(segment []string) bool {
	if len(segment) == 0 {
		return false
	}
	raw0 := strings.TrimSpace(segment[0])
	if raw0 == "" {
		return false
	}
	base := commandNameBase(raw0)
	args := segment[1:]

	// Path-like invocation: .\game.exe, ./a.out, build\Release\app.exe
	// Do not treat bare "." / ".." (or empty relative prefixes) as launch.
	if isOperationalPathLaunchToken(raw0) {
		return true
	}
	// Extension-based binaries/scripts (snake.exe, build_and_run.bat).
	for _, ext := range []string{".exe", ".bat", ".cmd", ".com", ".ps1", ".sh", ".msi"} {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}

	switch base {
	case "start", "start-process", "invoke-item", "ii", "open", "xdg-open":
		return true
	case "make", "mingw32-make", "gmake", "ninja", "msbuild", "cmake",
		"cl", "g++", "gcc", "clang", "clang++", "rustc", "javac", "mvn", "gradle", "flutter":
		return true
	case "go":
		return len(args) > 0 && (args[0] == "run" || args[0] == "build" || args[0] == "test" || args[0] == "install")
	case "cargo":
		return len(args) > 0 && (args[0] == "run" || args[0] == "build" || args[0] == "test" || args[0] == "bench")
	case "npm", "pnpm", "yarn", "bun":
		if len(args) == 0 {
			return false
		}
		switch args[0] {
		case "start", "run", "test", "build", "exec", "serve", "dev":
			return true
		default:
			return false
		}
	case "python", "python3", "py", "node", "deno", "ruby", "perl", "php", "lua", "dotnet":
		// Bare interpreter with no args often just opens a REPL / prints help —
		// require a script/module argument to count as launch evidence.
		if len(args) == 0 {
			return false
		}
		switch args[0] {
		case "-m", "-c", "-e", "run":
			return true
		default:
			return !strings.HasPrefix(args[0], "-")
		}
	case "powershell", "pwsh":
		// After stripVerificationCommandPrefixes, powershell wrappers are usually
		// peeled; if still present, look at remaining args for a launch token.
		return segmentLooksLikeOperationalLaunchArgs(args)
	case "bash", "sh", "zsh":
		return segmentLooksLikeOperationalLaunchArgs(args)
	default:
		return false
	}
}

// isOperationalPathLaunchToken reports path-form tokens used to invoke a program
// (.\app.exe, ./game, build\Release\app). Bare "." / ".." are excluded.
func isOperationalPathLaunchToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || token == "." || token == ".." {
		return false
	}
	// ".\" / "./" alone are not an invocation target.
	if token == ".\\" || token == "./" || token == ".\\\\" {
		return false
	}
	if strings.HasPrefix(token, ".") || strings.ContainsAny(token, `/\`) {
		return true
	}
	return false
}

func segmentLooksLikeOperationalLaunchArgs(args []string) bool {
	for _, a := range args {
		al := strings.ToLower(strings.Trim(a, `"'`))
		if al == "" || al == "-c" || al == "/c" || al == "-command" || al == "-file" || al == "-f" {
			continue
		}
		if isOperationalLaunchOrBuildSegment([]string{al}) {
			return true
		}
		// Nested script body sometimes arrives as one token; cheap markers.
		if strings.Contains(al, ".exe") || strings.Contains(al, ".bat") || strings.Contains(al, ".cmd") ||
			strings.Contains(al, "go run") || strings.Contains(al, "npm start") || strings.Contains(al, "npm run") {
			return true
		}
	}
	return false
}

func compactSubAgentTaskDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}
	return truncateRunesForSubAgent(description, codingSubAgentTaskDescriptionMaxRunes)
}

func compactSubAgentDynamicSelectionDescription(task *TaskItem) string {
	if task == nil {
		return ""
	}
	description := strings.TrimSpace(task.Description)
	if description == "" {
		return ""
	}
	maxRunes := codingSubAgentTaskDescriptionMaxRunes
	if len(task.Files) > 0 || len(task.AcceptanceCriteria) > 0 {
		maxRunes = codingSubAgentDynamicSelectionDescriptionMaxRunes
	}
	return truncateRunesForSubAgent(description, maxRunes)
}

func codingSubAgentDynamicSelectionText(task *TaskItem) string {
	return codingSubAgentDynamicSelectionTextWithContext(task, "", "", nil)
}

func codingSubAgentDynamicSelectionTextWithContext(task *TaskItem, reqCtx, designCtx string, prevOutputs []string) string {
	if task == nil {
		return ""
	}
	var b strings.Builder
	if title := compactSubAgentTaskTitle(task.Title); title != "" {
		b.WriteString(title)
		b.WriteString("\n")
	}
	if description := compactSubAgentDynamicSelectionDescription(task); description != "" {
		b.WriteString(description)
		b.WriteString("\n")
	}
	if len(task.Files) > 0 {
		b.WriteString("Files:\n")
		appendSubAgentBulletList(&b, uniqueSubAgentStrings(task.Files), codingSubAgentTaskFilesMax, codingSubAgentPromptBulletMaxRunes)
	}
	if len(task.AcceptanceCriteria) > 0 {
		b.WriteString("Acceptance criteria:\n")
		appendSubAgentBulletList(&b, uniqueSubAgentStrings(task.AcceptanceCriteria), codingSubAgentAcceptanceCriteriaMax, codingSubAgentPromptBulletMaxRunes)
	}
	if reqCtx = strings.TrimSpace(reqCtx); reqCtx != "" {
		b.WriteString("Requirement context:\n")
		b.WriteString(truncateRunesForSubAgent(reqCtx, codingSubAgentDynamicSelectionContextMaxRunes))
		b.WriteString("\n")
	}
	if designCtx = strings.TrimSpace(designCtx); designCtx != "" {
		b.WriteString("Design context:\n")
		b.WriteString(truncateRunesForSubAgent(designCtx, codingSubAgentDynamicSelectionContextMaxRunes))
		b.WriteString("\n")
	}
	if len(prevOutputs) > 0 {
		b.WriteString("Previous task outputs:\n")
		appendSubAgentBulletList(&b, prevOutputs, codingSubAgentDynamicSelectionPrevOutputsMax, codingSubAgentPromptBulletMaxRunes)
	}
	return truncateRunesForSubAgent(strings.TrimSpace(b.String()), codingSubAgentDynamicSelectionTextMaxRunes)
}

// finishInspectionRoleTask finalizes nested explorer/reviewer results without
// write-oriented quality gates (post-edit verify, git diff, acceptance matrix).
func (s *CodingSubAgent) finishInspectionRoleTask(
	cb *codingSubAgentCallbacks,
	task *TaskItem,
	taskTitle string,
	result agent.LoopResult,
) *CodingSubAgentResult {
	status := TaskExecPassed
	errMsg := ""
	if result.Error != "" {
		status = TaskExecFailed
		errMsg = compactSubAgentErrorSummary(result.Error)
	}
	if result.HardExit {
		status = TaskExecFailed
		errMsg = "模型连续返回空响应，任务中断"
	}
	summary := compactSubAgentModelSummary(result.Text)
	if summary == "" {
		summary = fallbackSubAgentTaskSummary(status, task, result.Iterations, result.ToolCalls)
	}
	audit := collectSubAgentAudit(cb)
	filesRead := audit.FilesRead
	searchesRun := audit.SearchesRun
	commandsRun := audit.CommandsRun
	hasInspection := len(uniqueSortedSubAgentStrings(filesRead)) > 0 ||
		countSuccessfulSubAgentSearches(searchesRun) > 0 ||
		len(commandsRun) > 0
	if status == TaskExecPassed {
		if result.ToolCalls == 0 || !hasInspection {
			status = TaskExecFailed
			errMsg = fmt.Sprintf("nested %s subagent completed without inspection evidence (read/search/bash)", s.role)
		} else if strings.TrimSpace(summary) == "" {
			status = TaskExecFailed
			errMsg = fmt.Sprintf("nested %s subagent returned empty summary", s.role)
		}
	}
	if status == TaskExecPassed {
		note := fmt.Sprintf("\n[%s] inspection-only role: skipped write-oriented verification gates", s.role)
		if !strings.Contains(summary, note) {
			summary = strings.TrimSpace(summary) + note
		}
	}
	log.Printf("[coding-subagent] inspection task T%d finished: role=%s status=%s iterations=%d tools=%d err=%q",
		taskDisplayNumber(task), s.role, status, result.Iterations, result.ToolCalls, errMsg)
	if s.onProgress != nil {
		event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, task, taskTitle, "")
		event.Detail = string(status)
		emitCodingAgentEvent(s.onProgress, event)
	}
	inTok, outTok, cost := codingLoopUsageFields(result.Usage)
	return &CodingSubAgentResult{
		Status:         status,
		Summary:        summary,
		Error:          errMsg,
		Iterations:     result.Iterations,
		ToolCalls:      result.ToolCalls,
		InputTokens:    inTok,
		OutputTokens:   outTok,
		EstCostRMB:     cost,
		RouteModel:     result.Route.Model,
		RouteSource:    result.Route.Source,
		RouteTask:      result.Route.TaskType,
		RouteReason:    result.Route.Reason,
		FilesModified:  nil,
		FilesCreated:   nil,
		FilesRead:      filesRead,
		CommandsRun:    commandsRun,
		SearchesRun:    searchesRun,
		QualityStatus:  codingSubAgentQualityPassed,
		QualitySummary: "inspection-only nested role",
		Localization:   cb.localization.snapshot(),
	}
}

// codingSubAgentUserContent builds multimodal user content when attachments exist.
func codingSubAgentUserContent(s *CodingSubAgent, userText string) interface{} {
	if s == nil || len(s.attachments) == 0 {
		return userText
	}
	// Prefer vision route when images are present and a vision model is configured.
	cfg := s.cfg
	if hasCodingImageAttachment(s.attachments) {
		if s.handler != nil {
			if routed := s.handler.routeLLMConfigForCodingVision(cfg); routed.Model != "" {
				s.cfg = routed
				cfg = routed
			}
		}
	}
	protocol := strings.TrimSpace(cfg.Protocol)
	if protocol == "" {
		protocol = "openai"
	}
	return agent.BuildUserContent(userText, s.attachments, protocol, cfg.SupportsVision, imageTextFromAttachment, nil)
}

func hasCodingImageAttachment(atts []agent.MessageAttachment) bool {
	for _, a := range atts {
		if a.Type == "image" || agent.IsImageMime(a.MimeType) {
			return true
		}
	}
	return false
}

// buildLocalInspectionRoleSystemPrompt is the lean brief for nested explorer/reviewer.
func buildLocalInspectionRoleSystemPrompt(projectPath string, role codingSubAgentRole, reqCtx string) string {
	var b strings.Builder
	b.WriteString("# Local Inspection SubAgent\n\n")
	if role == codingRoleReviewer {
		b.WriteString("你是审查/验证子代理：可读代码、跑 shell 与 git_diff，禁止写文件。\n\n")
	} else {
		b.WriteString("你是探索子代理：只读探查代码库，禁止写文件。\n\n")
	}
	if strings.TrimSpace(projectPath) != "" {
		b.WriteString(fmt.Sprintf("## 项目路径\n%s\n\n", projectPath))
	}
	b.WriteString(`## 可用工具
- code_navigation：优先用于符号、定义、引用、调用链与候选定位
- report_localization：提交结构化根因证据
- Glob / ripgrep / list_directory / read_file：文本定位与阅读
- git_diff：只读 diff/status（如可用）
`)
	if role == codingRoleReviewer {
		b.WriteString("- bash：诊断与验证命令（不要用 shell 改写文件或 Git 工作区）\n")
	}
	b.WriteString(`
## 工作规范
	1. 先搜索再阅读关键文件
	2. 故障定位必须输出症状、Top 候选、根因文件/符号、因果路径、复现证据、反证/排除假设、focused test，并调用 report_localization
	3. 遇到陌生概念/精确报错、第三方依赖/API/协议、版本或兼容性事实，必须 web_search 搜索精确错误和官方文档；纯仓内问题也要在 report_localization 说明为何无需联网
	4. 完成后给出结构化发现：关键路径、结论、外部来源、风险/建议
	5. 禁止 write/edit 改文件；不要改仓库状态
`)
	if strings.TrimSpace(reqCtx) != "" {
		b.WriteString("\n## 任务上下文\n\n")
		b.WriteString(reqCtx)
		b.WriteString("\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// System prompt — minimal, coding-only. ~1500-2000 tokens.
// ---------------------------------------------------------------------------

func buildFullCodingEnvironmentPromptPreamble() string {
	return `## 全功能编程环境（Full Coding Workbench）
你运行在与 Claude Code / Codex 同级目标的完整编程工作台中（同一模型下追求同等工程能力）：
- 可使用文件读写、搜索、shell、git_diff、web_search/web_fetch、current_datetime、goal（长时目标 complete/fail/get），以及任务相关的 Skill（manage_skill）与 MCP（call_mcp_tool）。
- 工作区根目录已绑定；下方「工作区概览」是进门自动探查结果，请先消化再深入探索。
- 默认自主探索与实现：不要等待用户再发「去了解项目」；需要时主动 list/Glob/ripgrep/read。
- 本会话支持多轮续写：用户后续消息仍在同一编程工作台中执行，可继续改码、验证、补测。
- 复杂任务：系统可能已给出多步「自动规划」；若有规划，严格按步骤推进并在每步验证。若无规划，先短计划（explore → implement → verify）再动手。
- 多步任务自行拆解、实现、验证；工具失败时换策略，不要空转重试。
- 外部知识判定是故障定位的必做步骤：遇到陌生概念/报错、第三方依赖/API/协议、版本或兼容性问题时，必须先 web_search 搜索精确错误与官方文档，再用 web_fetch 阅读最相关来源；不要靠记忆猜测。若搜索只是“无结果”，换一条保留组件/版本/错误码的查询再试一次；provider/网络/配置明确失败则不要重复空转。纯仓内逻辑问题可以不联网，但必须在 report_localization 中说明理由。
- 子代理（Codex 风格）：复杂/可并行工作时用 spawn_coding_agent 派生子代理。
  - explorer：只读探查（搜索/阅读），返回结构化发现
  - worker：干净上下文实现/修复（不可再嵌套 spawn）
  - reviewer：只读+shell+git_diff 审查验证，不写文件
  - 可 agents[] 最多 3 路：仅全部为 explorer 时并行；含 worker/reviewer 时顺序执行（避免并发写冲突）
- 保持工程师纪律：最小必要改动、验证优先、完成后 diff 与风险说明。

`
}

// buildNestedFullCodingEnvironmentPromptPreamble is for nested worker agents:
// same tool posture as full env, but no spawn guidance (depth capped).
func buildNestedFullCodingEnvironmentPromptPreamble() string {
	return `## 嵌套实现子代理（Nested Worker）
你是全功能编程工作台派发的实现子代理（干净上下文）：
- 可使用文件读写、搜索、shell、git_diff、web_search/web_fetch、current_datetime，以及任务相关的 Skill / MCP。
- 禁止再派生子代理；请在本上下文中直接完成指派任务并验证。
- 最小必要改动；工具失败时换策略；完成后说明改动与验证结果。

`
}

// probeCodingWorkspace builds a compact on-disk overview of the project root
// so the first turn already has structure context (Claude Code–style entry).
func probeCodingWorkspace(projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return ""
	}
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return ""
	}

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return ""
	}

	const maxEntries = 48
	var dirs, files []string
	stackHints := make([]string, 0, 8)
	stackFiles := map[string]string{
		"go.mod": "Go", "package.json": "Node/JS", "pnpm-lock.yaml": "pnpm",
		"yarn.lock": "Yarn", "package-lock.json": "npm", "pyproject.toml": "Python",
		"requirements.txt": "Python", "Cargo.toml": "Rust", "pom.xml": "Java/Maven",
		"build.gradle": "Java/Gradle", "build.gradle.kts": "Java/Gradle",
		"CMakeLists.txt": "C/C++", "Makefile": "Make", "Dockerfile": "Docker",
		".codegraph": "CodeGraph index", "README.md": "README", "README.MD": "README",
	}

	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if e.IsDir() {
			if len(dirs) < maxEntries {
				dirs = append(dirs, name+"/")
			}
			if name == ".codegraph" {
				stackHints = append(stackHints, "CodeGraph index")
			}
			continue
		}
		if len(files) < maxEntries {
			files = append(files, name)
		}
		if hint, ok := stackFiles[name]; ok {
			stackHints = append(stackHints, hint)
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("- 根路径: %s\n", projectPath))
	if len(stackHints) > 0 {
		// de-dup hints
		seen := map[string]bool{}
		var uniq []string
		for _, h := range stackHints {
			if !seen[h] {
				seen[h] = true
				uniq = append(uniq, h)
			}
		}
		b.WriteString("- 技术栈线索: " + strings.Join(uniq, ", ") + "\n")
	}
	if len(dirs) > 0 {
		b.WriteString("- 顶层目录: " + strings.Join(dirs, " ") + "\n")
	}
	if len(files) > 0 {
		shown := files
		if len(shown) > 24 {
			shown = shown[:24]
		}
		b.WriteString("- 顶层文件: " + strings.Join(shown, " ") + "\n")
	}
	if len(dirs)+len(files) >= maxEntries {
		b.WriteString("- （条目已截断；需要时用 list_directory / Glob 继续探查）\n")
	}
	return strings.TrimSpace(b.String())
}

func buildCodingSubAgentSystemPrompt(task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string) string {
	var b strings.Builder

	if codingTaskLooksOperational(task) {
		b.WriteString(`你是编程工作台里的操作执行器。当前任务是运行/构建/演示已有产物，不是实现新功能。

## 目标
- 在项目目录内找到可执行文件、脚本或构建命令，用 bash 真正执行。
- 不要修改源码、不要写测试、不要重构、不要调用 git_diff 来「凑」质量门禁。
- GUI/游戏可能阻塞：设置合理 timeout 启动并回报是否成功；把命令输出与退出码写进最终摘要。

## 允许
- list_directory / glob / read_file / bash（以及只读搜索）定位并启动程序。
- 仅当没有可执行产物时，用已有 build 脚本编译一次再运行。

## 禁止
- 为通过审计而伪造文件改动或添加无关测试。
- 破坏性删除/Git 改写命令（reset --hard、clean -f、rm -rf 等）。
- 只回复文字而不调用工具。

## 完成标准
- 至少成功执行过相关启动/构建命令，或明确报告可执行文件缺失及排查结果。
`)
		b.WriteString(fmt.Sprintf("\n## 项目路径\n%s\n", projectPath))
		if normalizedRemotePlatform() == "windows" {
			b.WriteString(fmt.Sprintf("平台: %s\n", normalizedRemotePlatform()))
			b.WriteString("Windows shell contract: bash 工具默认经 PowerShell 执行；普通命令用 `;` 分隔；需要 cmd 语义时用 `cmd /c \"...\"`，并用 working_dir 指定目录。\n")
			b.WriteString(formatWindowsMSVCToolchainHint(detectWindowsMSVCToolchain()))
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("平台: %s\n", normalizedRemotePlatform()))
		}
		if reqCtx != "" {
			b.WriteString("\n## 需求摘要\n")
			b.WriteString(truncateRunesForSubAgent(reqCtx, 280))
			b.WriteString("\n")
		}
		if len(prevOutputs) > 0 {
			b.WriteString("\n## 前置轮次摘要\n")
			appendSubAgentBulletList(&b, prevOutputs, 4, 200)
		}
		now := time.Now()
		b.WriteString(fmt.Sprintf("\n当前时间: %s\n", now.Format("2006-01-02 15:04")))
		return b.String()
	}

	b.WriteString(`你是一个专注的编码执行器。目标是像资深工程师一样：先定位和理解，再做最小改动，最后验证并说明风险。
`)
	b.WriteString(codingAgentTodoPromptSection)
	b.WriteString(`
## 工作流
- 如果项目根目录存在 .codegraph/，先用 bash 运行 codegraph explore / codegraph node 定位相关符号、调用链和文件；如果没有索引，再用 Glob / ripgrep 定位相关代码，并用 read_file 阅读当前内容。所有读取、搜索、列目录都必须限定在项目路径内；不要读取项目外文件。
- 修复 bug 时，优先复现或确认错误，再沿调用链追踪输入、状态变化和影响范围。不要基于猜测修改。
- 修改已有文件时优先使用 edit_file 或 edit_lines；禁止用 write_file 重写已有文件来做小修改。edit_file 失败时先 read_file 确认当前内容，再改用 edit_lines。
- write_file 只用于创建新文件，或在用户/仓库流程明确要求时追加 TEST_REPORT.md。
- bash 用于测试、构建、lint、typecheck、调试命令；长命令必须设置 timeout，working_dir 必须在项目路径内。
- 需要辅助能力时使用 manage_skill / call_mcp_tool（若已提供）；不要假设用户会手动再安装工具。

## 验证优先流程
1. 能自动化覆盖的行为变更，应添加或更新聚焦测试；无法合理自动化时，在总结中说明原因。
2. 修改后运行匹配的验证命令（test/build/lint/typecheck），失败时分析错误后再修复。
3. 完成前调用 git_diff 自检，确认改动范围符合任务要求。若项目不是 Git 仓库，说明该情况并依赖文件审计列表，不要用 bash 反复跑 git status/diff/log，也不要对 git 自检加 2>/dev/null。
4. 只在用户明确要求或仓库已有流程要求时，才追加 TEST_REPORT.md；不要默认制造报告文件。

## 禁止行为
- 禁止执行破坏性删除、清理或 Git 工作区/索引/历史改写命令，例如 git reset --hard、git checkout --、git checkout .、git restore、git switch、git merge/rebase/stash、git add/commit/apply/cherry-pick/revert/rm/mv/update-index/read-tree、git clean -f、rm -rf、Remove-Item -Recurse、rmdir /s、del /s。
- 禁止对 git status/diff/log 自检使用 2>/dev/null 或 >/dev/null 掩盖错误输出。
- 禁止不读文件就直接修改；禁止无关重构、无关格式化、依赖 churn 或 speculative feature work。
- 遇到无法解决的问题，说明具体原因，不要反复重试相同的失败操作。
`)

	b.WriteString(fmt.Sprintf(`
## Single-task contract
- Work only on the assigned task. Avoid broad refactors, unrelated formatting, dependency churn, or speculative feature work; keep edits small and reviewable.
- If verification fails because of unrelated pre-existing errors, report the exact blocker with file/line when available.
- Before the final answer, inspect the diff, summarize created/modified files, list verification commands, and call out remaining risk.

## Quality audit gates
- Enforced hard gates: explore before existing-file edits, verify changed tasks, run git_diff, and give inspection/verification evidence for no-change tasks or project-context evidence for new files.
	- Bug fixes: localize with code_navigation, reproduction and alternatives, make an explicit research decision, then report_localization before editing. Unknown/current/third-party facts require web_search (exact error + component/version) and authoritative sources.
- Verification evidence must be fresh after the final edit, include real execution output, and be named in the final summary with pass/fail outcome.
- Empty/weak evidence does not count: blank, "(无输出)", "no tests found", "no tests collected", "[no test files]", "0 tests", "0 examples", list/help/collect-only/dry-run.
- Final summary: actual modified/created file paths, only verification commands really run/passed, map every acceptance criterion, scope expansion, remaining risk/no known remaining risk.

## Tool-call JSON reliability
- Keep every tool_call arguments JSON complete and valid. If write_file content exceeds about 6000 chars, split it into chunks: first mode="overwrite", then mode="append".
- Prefer edit_file or edit_lines for existing files. Use write_file only for new files, or TEST_REPORT.md append entries when the user or repo workflow explicitly asks for a report.
- If write_file JSON was invalid/incomplete, retry smaller chunks. Treat tool error text as authoritative recovery guidance; do not repeat an identical failed tool call or command. If the same target fails, change the approach before retrying.

## Command guardrails
- Do not run Git commands that rewrite or move worktree/index/history state: reset, checkout, restore, switch, merge, rebase, stash, add, commit, apply, am, cherry-pick, revert, rm, mv, update-index, read-tree, or clean -f. Read-only status/diff/log are allowed.
- Do not run recursive or forceful delete commands such as rm -r/-rf, Remove-Item -Recurse/-r/-rf, ri -r, rd/rmdir /s, del /s, or erase /s. Use edit_file/edit_lines/write_file for scoped file changes.
- Do not mutate files through bash redirection or shell helpers: >, >>, tee/Tee-Object, Set-Content/Add-Content/Out-File, touch/mkdir, Copy-Item/Move-Item/Rename-Item, sed -i, perl -pi, Node fs write/copy/rename/rm/mkdir APIs, Python open(..., "w")/Path write/touch/rename/remove APIs, or dd of=. Use the file editing tools instead.
- Verification wrappers timeout/gtimeout, env/cross-env/time, cmd /c, powershell -Command, bash -lc are OK only when the wrapped command runs tests/build/lint/typecheck.
- Do not use failure-suppressing or non-auditable verification shells: no || true, pipes, output redirection, help/list/collect-only flags, watch/UI modes, mutating flags such as --fix/--write, or chained post-verification commands.
`))

	b.WriteString(fmt.Sprintf("\n## 项目路径\n%s\n", projectPath))

	// Platform hint so the LLM generates correct shell commands.
	b.WriteString(fmt.Sprintf("平台: %s\n", normalizedRemotePlatform()))
	if normalizedRemotePlatform() == "windows" {
		b.WriteString("Windows shell contract: bash 工具默认经 PowerShell 执行；普通命令用 `;` 分隔，避免 bash-only 语法如 `mkdir -p`；需要 cmd 语义（vcvars+cl、`||`、`2>nul`）时用 `cmd /c \"...\"`，并用 working_dir 指定目录；不要在既有 build 目录中切换 CMake generators。\n")
		// Host-side vswhere detection — stop the agent from claiming VS is missing
		// when only cl.exe is absent from the default PATH (normal for MSVC).
		b.WriteString(formatWindowsMSVCToolchainHint(detectWindowsMSVCToolchain()))
		b.WriteString("\n")
	}

	if reqCtx != "" {
		b.WriteString("\n## 需求摘要\n")
		b.WriteString(truncateRunesForSubAgent(reqCtx, 280))
		b.WriteString("\n")
	}

	if designCtx != "" {
		b.WriteString("\n## 设计摘要\n")
		b.WriteString(truncateRunesForSubAgent(designCtx, 280))
		b.WriteString("\n")
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

func appendSubAgentAcceptanceCriteriaList(b *strings.Builder, items []string, maxItems, maxRunes int) {
	if maxItems <= 0 {
		maxItems = len(items)
	}
	shown := len(items)
	if shown > maxItems {
		shown = maxItems
	}
	for i, item := range items[:shown] {
		b.WriteString(fmt.Sprintf("- AC%d/标准%d: ", i+1, i+1))
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
	codingSubAgentDefaultBashTimeout                  = corelib.DefaultAgentTimeoutSec
	codingSubAgentMaxBashTimeout                      = corelib.MaxAgentTimeoutSec
	codingSubAgentGuardrailSummaryMax                 = 5
	codingSubAgentGuardrailDetailMaxRunes             = 240
	codingSubAgentCommandSummaryMax                   = 10
	codingSubAgentCommandTextMaxRunes                 = 240
	codingSubAgentCommandOutputLineMaxRunes           = 240
	codingSubAgentPathTextMaxRunes                    = 180
	codingSubAgentSearchTextMaxRunes                  = 180
	codingSubAgentFailedVerificationSummaryMax        = 5
	codingSubAgentFileChangeSummaryMax                = 20
	codingSubAgentResultFilesMax                      = 80
	codingSubAgentResultAuditMax                      = 50
	codingSubAgentModelSummaryMaxRunes                = 4000
	codingSubAgentErrorSummaryMaxRunes                = 1000
	codingSubAgentReportSummaryMaxRunes               = 8000
	codingSubAgentRunReportMaxItems                   = 30
	codingSubAgentRunReportMaxRunes                   = 6000
	codingSubAgentTaskTitleMaxRunes                   = 160
	codingSubAgentTaskDescriptionMaxRunes             = 2000
	codingSubAgentDynamicSelectionTextMaxRunes        = 4000
	codingSubAgentDynamicSelectionDescriptionMaxRunes = 900
	codingSubAgentTaskListSummaryMax                  = 50
	codingSubAgentTaskFilesMax                        = 30
	codingSubAgentDependencySummaryMax                = 20
	codingSubAgentAcceptanceCriteriaMax               = 20
	codingSubAgentPrevOutputsMax                      = 20
	codingSubAgentDynamicSelectionPrevOutputsMax      = 5
	codingSubAgentDynamicSelectionContextMaxRunes     = 240
	codingSubAgentPromptBulletMaxRunes                = 160

	// codingSubAgentPerTaskMaxIterations is the hard iteration cap for a
	// single SubAgent task. This prevents the SubAgent from blocking the
	// main UI thread indefinitely when it gets stuck in a non-repeating
	// failure loop (e.g. repeatedly trying different workarounds for a
	// platform-specific issue where each attempt has unique args/results,
	// so the built-in drift detector in RunLoop never triggers).
	//
	// 80 iterations is generous for focused single-task execution (typical
	// tasks complete in 10-30 iterations). Beyond 80, the task is almost
	// certainly stuck and should be reported as failed.
	codingSubAgentPerTaskMaxIterations = 80

	// Full coding workbench (create-task pure coding) allows longer multi-step
	// exploration + implement + verify cycles, closer to Claude Code sessions.
	codingSubAgentFullEnvMaxIterations = 120
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

// codingSubAgentFullEnvExtraToolOrder is always available in fullEnvironment
// mode (Claude Code / Codex–aligned research helpers).
var codingSubAgentFullEnvExtraToolOrder = []string{
	"web_search",
	"web_fetch",
	"download_file",
	"current_datetime",
}

var codingSubAgentToolNames = makeCodingSubAgentToolNameSet(codingSubAgentToolOrder)

var (
	codingSubAgentFallbackToolsOnce sync.Once
	codingSubAgentFallbackTools     []map[string]interface{}
)

// codingSubAgentDynamicToolNames lists tools that are conditionally available
// in the SubAgent (injected based on task context, not always present).
// These bypass the static tool name check in executeToolWithOutcome.
var codingSubAgentDynamicToolNames = map[string]bool{
	"manage_skill":              true,
	"call_mcp_tool":             true,
	"coding_knowledge_search":   true,
	"knowledge_search":          true,
	"knowledge_image_search":    true,
	"web_search":                true,
	"web_fetch":                 true,
	"download_file":             true,
	"current_datetime":          true,
	"goal":                      true,
	codingSubAgentSpawnToolName: true,
	codingAgentTodoToolName:     true,
	codeNavigationToolName:      true,
	reportLocalizationToolName:  true,
}

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

	byName := make(map[string]map[string]interface{}, len(codingSubAgentToolNames))
	for _, t := range allTools {
		fn, ok := t["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if codingSubAgentToolNames[name] {
			byName[name] = compactCodingSubAgentToolDefinition(t)
		}
	}

	// The GUI registry does not currently expose every coding-agent-only tool.
	// Keep registry definitions when present, then fill gaps from fallback defs.
	var fallbacks []map[string]interface{}
	if len(byName) < len(codingSubAgentToolNames) {
		fallbacks = buildCodingToolDefinitionsFallback()
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
	}

	ordered := make([]map[string]interface{}, 0, len(codingSubAgentToolNames))
	for _, name := range codingSubAgentToolOrder {
		if t, ok := byName[name]; ok {
			ordered = append(ordered, t)
		}
	}
	if len(ordered) == 0 {
		if fallbacks == nil {
			fallbacks = buildCodingToolDefinitionsFallback()
		}
		return fallbacks
	}
	return ordered
}

// buildCodingFullEnvExtraToolDefinitions returns research helpers for full
// coding workbench mode (always appended when fullEnvironment is on).
func buildCodingFullEnvExtraToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "web_search",
				"description": "Search the web for unfamiliar concepts, exact error messages, official documentation, APIs, versions, compatibility, and current library usage. For unknown/current/third-party bug facts, use this before guessing or editing; search the quoted error plus component/version.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query":       map[string]interface{}{"type": "string", "description": "Search query"},
						"max_results": map[string]interface{}{"type": "integer", "description": "Max results (optional)"},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "web_fetch",
				"description": "Fetch and extract text content from a URL (docs, GitHub, RFCs). Use save_path to download binary/PDF files.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url":       map[string]interface{}{"type": "string", "description": "URL to fetch"},
						"save_path": map[string]interface{}{"type": "string", "description": "Optional path under workdir to save file"},
						"max_chars": map[string]interface{}{"type": "integer", "description": "Max characters (optional)"},
					},
					"required": []string{"url"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "download_file",
				"description": "Download HTTP/HTTPS URL into the working directory (preferred for PDFs). Returns absolute path.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url":       map[string]interface{}{"type": "string", "description": "URL to download"},
						"save_path": map[string]interface{}{"type": "string", "description": "Optional relative path under workdir"},
					},
					"required": []string{"url"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "current_datetime",
				"description": "Get current local date and time. Prefer this over guessing the clock.",
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
	}
}

func buildCodingExternalResearchToolDefinitions() []map[string]interface{} {
	all := buildCodingFullEnvExtraToolDefinitions()
	out := make([]map[string]interface{}, 0, 2)
	for _, def := range all {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name == "web_search" || name == "web_fetch" {
			out = append(out, def)
		}
	}
	return out
}

// buildCodingGoalToolDefinition exposes persistent /goal lifecycle actions to
// the pure coding workbench so continuation turns can complete/fail/get status.
func buildCodingGoalToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "goal",
			"description": "Manage the persistent long-running goal for this coding workbench " +
				"(action: create/complete/fail/get). Use complete when acceptance criteria are met; " +
				"use fail when the goal cannot continue. Prefer get before create.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":              map[string]interface{}{"type": "string", "description": "create | complete | fail | get"},
					"objective":           map[string]interface{}{"type": "string", "description": "Goal description (create)"},
					"summary":             map[string]interface{}{"type": "string", "description": "Completion summary (complete)"},
					"reason":              map[string]interface{}{"type": "string", "description": "Failure reason (fail)"},
					"token_budget":        map[string]interface{}{"type": "integer", "description": "Optional token budget (create)"},
					"max_turns":           map[string]interface{}{"type": "integer", "description": "Optional max continuation turns (create)"},
					"acceptance_criteria": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional acceptance criteria (create)"},
					"project_path":        map[string]interface{}{"type": "string", "description": "Optional project path metadata (create)"},
				},
				"required": []string{"action"},
			},
		},
	}
}

// buildCodingToolDefinitionsFallback provides minimal inline definitions
// for testing or when the registry is unavailable.
func buildCodingToolDefinitionsFallback() []map[string]interface{} {
	codingSubAgentFallbackToolsOnce.Do(func() {
		codingSubAgentFallbackTools = buildCodingToolDefinitionsFallbackUncached()
	})
	return cloneCodingSubAgentToolDefinitions(codingSubAgentFallbackTools)
}

func buildCodingToolDefinitionsFallbackUncached() []map[string]interface{} {
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
	toolName, _ := fn["name"].(string)
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
				if key == "description" && !keepCodingSubAgentToolPropertyDescription(toolName, name) {
					continue
				}
				copyProp[key] = value
			}
			compactProps[name] = copyProp
		case map[string]string:
			copyProp := make(map[string]string, len(prop))
			for key, value := range prop {
				if key == "description" && !keepCodingSubAgentToolPropertyDescription(toolName, name) {
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
	applyCodingSubAgentToolHints(compactFn)
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

func applyCodingSubAgentToolHints(fn map[string]interface{}) {
	name, _ := fn["name"].(string)
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	switch name {
	case "bash":
		appendCodingSubAgentToolDescription(fn, "Coding SubAgent bash is for read-only diagnostics and verification commands (test/build/lint/typecheck). Do not use bash to edit files, create/delete/move files, rewrite Git state, stage/commit/apply patches, or hide verifier failures with pipes/redirection/extra shell commands; use read_file/edit_file/edit_lines/write_file/git_diff instead.")
	case "read_file":
		ensureCodingSubAgentToolIntegerProp(props, "lines", "Max lines to read (optional, default 200, maximum 2000). Also accepts limit/num_lines/line_count.")
		ensureCodingSubAgentToolIntegerProp(props, "start_line", "Starting line number, 1-based (optional). Also accepts start/startLine.")
		ensureCodingSubAgentToolIntegerProp(props, "offset", "Read the last N lines of the file. Use this after adaptive output says earlier content was skipped or when inspecting logs/tails.")
	case "write_file":
		setCodingSubAgentToolPropDescription(props, "content", "File content. No length limit; you can write complete scripts or documents in a single call. For very large files (>6000 chars), consider splitting into overwrite + append chunks.")
		// Do NOT set maxLength for write_file — it causes models to avoid calling the tool for long content.
		setCodingSubAgentToolPropDescription(props, "mode", "Write mode: overwrite for first chunk, append for later chunks.")
	case "edit_file":
		setCodingSubAgentToolPropDescription(props, "old_string", fmt.Sprintf("Exact text to replace. Keep under %d characters; use edit_lines for large edits.", codingSubAgentInlineContentLimit))
		setCodingSubAgentToolPropMaxLength(props, "old_string", codingSubAgentInlineContentLimit)
		setCodingSubAgentToolPropDescription(props, "new_string", fmt.Sprintf("Replacement text. Keep under %d characters; split large edits into multiple small calls.", codingSubAgentInlineContentLimit))
		setCodingSubAgentToolPropMaxLength(props, "new_string", codingSubAgentInlineContentLimit)
	case "edit_lines":
		setCodingSubAgentToolPropDescription(props, "content", fmt.Sprintf("New content for replace/insert. Keep under %d characters; split large edits into multiple small calls.", codingSubAgentInlineContentLimit))
		setCodingSubAgentToolPropMaxLength(props, "content", codingSubAgentInlineContentLimit)
	}
}

func appendCodingSubAgentToolDescription(fn map[string]interface{}, hint string) {
	if fn == nil || strings.TrimSpace(hint) == "" {
		return
	}
	desc, _ := fn["description"].(string)
	desc = strings.TrimSpace(desc)
	if strings.Contains(desc, hint) {
		return
	}
	if desc == "" {
		fn["description"] = hint
		return
	}
	fn["description"] = desc + " " + hint
}
func ensureCodingSubAgentToolIntegerProp(props map[string]interface{}, propName, desc string) {
	if props == nil {
		return
	}
	if _, ok := props[propName]; ok {
		setCodingSubAgentToolPropDescription(props, propName, desc)
		return
	}
	props[propName] = map[string]string{"type": "integer", "description": desc}
}

func setCodingSubAgentToolPropDescription(props map[string]interface{}, propName, desc string) {
	if props == nil {
		return
	}
	switch prop := props[propName].(type) {
	case map[string]interface{}:
		prop["description"] = desc
	case map[string]string:
		prop["description"] = desc
	}
}

func setCodingSubAgentToolPropMaxLength(props map[string]interface{}, propName string, maxLength int) {
	if props == nil {
		return
	}
	switch prop := props[propName].(type) {
	case map[string]interface{}:
		prop["maxLength"] = maxLength
	case map[string]string:
		next := make(map[string]interface{}, len(prop)+1)
		for key, value := range prop {
			next[key] = value
		}
		next["maxLength"] = maxLength
		props[propName] = next
	}
}

func keepCodingSubAgentToolPropertyDescription(toolName, propName string) bool {
	switch toolName {
	case "write_file":
		return propName == "content" || propName == "mode"
	case "edit_file":
		return propName == "old_string" || propName == "new_string"
	case "edit_lines":
		return propName == "content"
	}
	return false
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
	return runTaskWithSubAgentRuntimeOptions(handler, cfg, httpClient, task, projectPath, reqCtx, designCtx, prevOutputs, loopCtx, onToken, onProgress, nil)
}

// runTaskWithSubAgentRuntimeOptions is the internal implementation boundary
// for isolated worktree execution. Public callers retain the existing API;
// the workbench can supply a final merge gate that is evaluated before the
// shared runtime records a successful Attempt.
func runTaskWithSubAgentRuntimeOptions(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	task *TaskItem,
	projectPath, reqCtx, designCtx string,
	prevOutputs []string,
	loopCtx *LoopContext,
	onToken func(string),
	onProgress func(string),
	runtimeOptions *guiCodingRuntimeOptions,
) *CodingSubAgentResult {
	// OpenHuman-inspired + sticky RoutePref (auto/primary/reasoning/vision).
	userID := ""
	if loopCtx != nil {
		userID = loopCtx.UserID
	}
	hasImages := loopCtx != nil && hasCodingImageAttachment(loopCtx.CodingAttachments)
	if handler != nil {
		cfg = handler.applyCodingRoutePreference(userID, cfg, hasImages)
	}
	sa := NewCodingSubAgent(handler, cfg, httpClient, projectPath, loopCtx)
	sa.SetCallbacks(onToken, onProgress)
	if loopCtx != nil && len(loopCtx.CodingAttachments) > 0 {
		// Only the first root step of a turn should consume attachments (avoid
		// re-sending images on every multi-step plan task). Caller clears after turn.
		sa.SetAttachments(loopCtx.CodingAttachments)
	}
	// Create-task pure coding: full workbench posture
	// (broader skill/MCP, workspace probe) aligned with Claude Code / Codex intent.
	sa.SetFullEnvironment(true)

	// Wire interactive scope approval: when the SubAgent tries to access paths
	// outside projectPath, pause and ask the user instead of hard-rejecting.
	// The approval callback uses onProgress to send the prompt and blocks on
	// loopCtx's approval channel.
	// Input-box "完全控制" (full) → skip all path and high-risk prompts.
	if handler != nil && handler.app != nil {
		globalFull := handler.app.isSubAgentFullAccessGranted()
		userID := ""
		if loopCtx != nil {
			userID = loopCtx.UserID
		}
		fullAccess := handler.stickyCodingEffectiveFullAccess(userID, globalFull)
		sa.SetScopeApprovalCallback(buildSubAgentScopeApprovalCallback(handler, loopCtx, onProgress), fullAccess)
		sa.scopeApproval.setAuditCallback(func(req ScopeApprovalRequest, decision ScopeApprovalDecision, source string) {
			recordScopeApprovalAudit(handler, "", req, decision, source)
			// Multi-turn continuity: remember allow_dir / path trust / high-risk trust
			// without requiring a global config write when the user already chose session trust.
			if loopCtx != nil {
				switch decision {
				case ScopeApprovalAllowDir:
					handler.rememberStickyApprovedDir(loopCtx.UserID, req.Directory)
				case ScopeApprovalFullAccess:
					if req.Kind == localHighRiskApprovalKind {
						handler.markStickyCodingSessionHighRiskAccess(loopCtx.UserID)
					} else {
						handler.markStickyCodingSessionFullAccess(loopCtx.UserID, "", sa.projectPath)
					}
					// Path + high-risk both granted → upgrade UI mode to full.
					handler.maybeUpgradeStickyPermissionModeToFull(loopCtx.UserID)
				}
			}
		})
		// Trust the user-selected coding workspace + parent (monorepo common case).
		sa.seedFullEnvironmentWorkspaceApprovals()
		// Create-task pure coding: apply session-scoped full-access + prior allow_dir grants.
		// When already fullAccess, sticky overlay is redundant but harmless.
		if loopCtx != nil {
			handler.applyStickyCodingPermissions(loopCtx.UserID, sa)
		}
	}
	// Wire knowledge stores for experience recall and project doc lookup.
	if handler != nil && handler.app != nil {
		codingKB := handler.app.ensureCodingKnowledgeStore()
		generalKB := getAutoRecallStoreForApp(handler.app, false)
		sa.SetKnowledgeStores(codingKB, generalKB)
	}

	store, closeStore, err := openGUICodingRuntimeStore(handler)
	if err != nil {
		return failedCodingSubAgentStartResult("unable to open coding execution ledger: " + err.Error())
	}
	defer closeStore()
	ctx := context.Background()
	var cancel context.CancelFunc
	if loopCtx != nil {
		ctx, cancel = loopCtx.Context()
		defer cancel()
	}
	ownerID := "gui:local"
	workflowID, phaseID := "", ""
	if loopCtx != nil {
		ownerID = "gui:" + loopCtx.ID
		workflowID = loopCtx.WorkflowID
		phaseID = loopCtx.WorkflowPhaseID
	}
	approvalGate := codingRuntimeApprovalGate(func() string { return sa.prepareTaskScopeApproval(task) })
	var unregisterRuntimeCancellation func()
	result, attempt, ledgerErr := runGUICodingTaskWithLedgerWithOptions(ctx, store, ownerID, workflowID, phaseID, projectPath, task.Title+"\n"+task.Description, approvalGate, func(request codingruntime.ExecutionRequest) {
		sa.runtimeStore = store
		attempt := request.Attempt
		sa.runtimeAttempt = &attempt
		if loopCtx != nil {
			unregisterRuntimeCancellation = loopCtx.RegisterCancelHookForContext(ctx, func() {
				// LoopContext already stops the in-process agent. This second,
				// durable boundary prevents admitted children or a late callback
				// from continuing after the user has cancelled the parent turn.
				_, _ = store.CancelTask(request.Task.TaskID, time.Now().UTC())
				cancelGUIAdmittedChildExecutions(request.Task.TaskID)
			})
		}
	}, runtimeOptions, func() *CodingSubAgentResult {
		return sa.ExecuteTask(task, reqCtx, designCtx, prevOutputs)
	})
	if unregisterRuntimeCancellation != nil {
		unregisterRuntimeCancellation()
	}
	sa.runtimeStore, sa.runtimeAttempt = nil, nil
	if ledgerErr != nil {
		return failedCodingSubAgentStartResult("coding execution ledger failed: " + ledgerErr.Error())
	}
	if result != nil && result.Status == TaskExecWaitingChild {
		result.RuntimeHandoff = true
	}
	if result != nil && attempt != nil {
		result.Summary = strings.TrimSpace(result.Summary + "\n\nExecution attempt: " + attempt.AttemptID)
	}
	if result != nil && result.Status == TaskExecPassed && handler != nil {
		persistLocalizationExperience(handler.app, sa.codingKB, projectPath, task.Title, result.Localization, result.CommandsRun, result.RuntimeTaskID)
	}
	return result
}
