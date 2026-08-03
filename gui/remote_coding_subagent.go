package main

// remote_coding_subagent.go implements a RemoteCodingSubAgent that executes
// coding tasks on a remote server via SSH. It mirrors the local CodingSubAgent
// architecture — clean context, minimal tools, independent conversation — but
// all file/shell operations target a remote server.
//
// Tool set (5 SSH-wrapped tools):
//   ssh_read_file   → cat remote file
//   ssh_write_file  → write content to remote file (python pathlib)
//   ssh_edit_file   → python string replace in remote file
//   ssh_bash        → execute command on remote server
//   ssh_list_dir    → ls remote directory
//
// The SubAgent reuses corelib/agent.RunLoop and delegates SSH execution to the
// host IMMessageHandler's existing SSH infrastructure (ensureSSHManager, sshExec).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	pathpkg "path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// RemoteCodingSubAgent executes coding tasks on a remote server via SSH.
type RemoteCodingSubAgent struct {
	handler    *IMMessageHandler
	cfg        corelib.MaclawLLMConfig
	httpClient *http.Client

	// Remote server context
	sessionID  string // SSH session ID (already connected)
	workDir    string // remote working directory
	projectDir string // remote project directory (within workDir)

	// Callbacks
	onToken    func(string)
	onProgress func(string)

	// Cancellation
	loopCtx *LoopContext

	// Knowledge stores (optional, nil = gracefully skipped)
	codingKB  *knowledge.CodingKnowledgeStore
	generalKB *knowledge.SQLiteStore

	// highRiskApproval lets the user override remote bash safety guardrails.
	// Nil preserves the default hard-reject behavior.
	highRiskApproval         *remoteHighRiskApprovalState
	highRiskApprovalExplicit bool

	// sourcePreviewEnabled is opt-in because this agent is also used by remote
	// execution paths that are not the user-facing remote coding workflow.
	sourcePreviewEnabled   bool
	sourcePreviewSessionID string

	// nestDepth is 0 for the pure-coding root turn. Nested spawn_coding_agent
	// children increment this; spawn is disabled at codingSubAgentMaxNestDepth.
	nestDepth int
	// role specializes the SSH tool surface for nested agents (explorer/worker/reviewer).
	// Empty means worker (full remote coding surface).
	role codingSubAgentRole

	// readOnlyInquiry narrows the root tool surface for repository questions.
	// It is set per turn so an existing remote coding session can still perform
	// normal implementation work on the next user request.
	readOnlyInquiry bool
	// requestKind is supplied by the workbench before it expands a plan step.
	// It preserves the classification of the user's original short request.
	requestKind codingRequestKind
	// operationalRequest marks a run/build/demo turn. It deliberately gets a
	// smaller, source-safe tool surface so "run it" does not accidentally turn
	// into an implementation workflow just because it happens over SSH. Build
	// output is allowed because it is a normal consequence of executing a build.
	operationalRequest bool
	// maintenance changes presentation only. The underlying SSH tool and safety
	// policies remain the remote-coding engine shared with development tasks.
	maintenance bool
}

var remoteSourcePreviewSessionSeq atomic.Uint64

// RemoteCodingSubAgentResult is the outcome of a remote task execution.
type RemoteCodingSubAgentResult struct {
	Status        string // "success", "failed", "cancelled"
	Summary       string
	Error         string
	Iterations    int
	ToolCalls     int
	InputTokens   int
	OutputTokens  int
	EstCostRMB    float64
	RouteModel    string
	RouteSource   string
	RouteTask     string
	RouteReason   string
	FilesModified []string
	FilesCreated  []string
	Localization  *CodingSubAgentLocalizationEvidence
}

// NewRemoteCodingSubAgent creates a SubAgent bound to an existing SSH session.
func NewRemoteCodingSubAgent(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	sessionID, workDir, projectDir string,
	loopCtx *LoopContext,
) *RemoteCodingSubAgent {
	return &RemoteCodingSubAgent{
		handler:    handler,
		cfg:        cfg,
		httpClient: httpClient,
		sessionID:  sessionID,
		workDir:    workDir,
		projectDir: projectDir,
		loopCtx:    loopCtx,
	}
}

// SetCallbacks configures optional streaming and progress callbacks.
func (r *RemoteCodingSubAgent) SetCallbacks(onToken func(string), onProgress func(string)) {
	if r == nil {
		return
	}
	r.onToken = onToken
	r.onProgress = onProgress
	if !r.highRiskApprovalExplicit && r.handler != nil && r.handler.app != nil {
		r.setHighRiskApprovalCallback(buildRemoteHighRiskApprovalCallback(r.handler, r.loopCtx, onProgress, r.maintenance), false, false, true)
	}
}

// SetSourcePreviewEnabled enables source events for the remote coding workflow.
func (r *RemoteCodingSubAgent) SetSourcePreviewEnabled(enabled bool) {
	if r != nil {
		r.sourcePreviewEnabled = enabled
		if enabled && r.sourcePreviewSessionID == "" {
			r.sourcePreviewSessionID = fmt.Sprintf("remote:%s:%d:%d", r.sessionID, time.Now().UnixNano(), remoteSourcePreviewSessionSeq.Add(1))
		}
		if !enabled {
			r.sourcePreviewSessionID = ""
		}
	}
}

// SetHighRiskApprovalCallback configures interactive user confirmation for
// remote bash commands blocked by coding guardrails.
func (r *RemoteCodingSubAgent) SetHighRiskApprovalCallback(callback ScopeApprovalCallback, fullAccess bool) {
	r.setHighRiskApprovalCallback(callback, fullAccess, true, false)
}

func (r *RemoteCodingSubAgent) setHighRiskApprovalCallback(callback ScopeApprovalCallback, fullAccess bool, explicit bool, preserveFullAccess bool) {
	if r == nil {
		return
	}
	if r.highRiskApproval == nil {
		r.highRiskApproval = newRemoteHighRiskApprovalState(callback, fullAccess)
	} else {
		r.highRiskApproval.configure(callback, fullAccess, preserveFullAccess)
	}
	if r.handler != nil && r.handler.app != nil {
		r.highRiskApproval.setAuditCallback(func(req ScopeApprovalRequest, decision ScopeApprovalDecision, source string) {
			recordScopeApprovalAudit(r.handler, "", req, decision, source)
		})
	}
	r.highRiskApprovalExplicit = explicit
}

func (s *remoteHighRiskApprovalState) setAuditCallback(callback func(ScopeApprovalRequest, ScopeApprovalDecision, string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.auditApproval = callback
	s.mu.Unlock()
}

// SetKnowledgeStores configures the coding experience store and general knowledge store.
// Both are optional — nil stores are gracefully skipped.
func (r *RemoteCodingSubAgent) SetKnowledgeStores(codingKB *knowledge.CodingKnowledgeStore, generalKB *knowledge.SQLiteStore) {
	if r == nil {
		return
	}
	r.codingKB = codingKB
	r.generalKB = generalKB
}

// SaveExperience saves an experiment experience to the coding knowledge store.
// Called by the orchestrator after each experiment round completes.
// This accumulates experimental knowledge (what worked, what didn't, why).
func (r *RemoteCodingSubAgent) SaveExperience(exp knowledge.CodingExperience) error {
	if r == nil || r.codingKB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.codingKB.SaveExperience(ctx, exp)
	if err != nil {
		log.Printf("[remote-subagent] failed to save experience: %v", err)
	}
	return err
}

// ExecuteTask runs a single task on the remote server in a clean context.
func (r *RemoteCodingSubAgent) ExecuteTask(taskDescription, taskContext string) *RemoteCodingSubAgentResult {
	if r == nil {
		return &RemoteCodingSubAgentResult{Status: "failed", Error: "remote coding subagent is nil"}
	}
	// Keep turn-specific routing state on an execution-local copy. A caller may
	// retain and reuse this agent across turns, and concurrent calls must not let
	// one repository question narrow another implementation task's tool surface.
	execution := *r
	execution.readOnlyInquiry, execution.operationalRequest = resolveRemoteCodingRequestFlags(r.requestKind, taskDescription)
	return execution.executeTask(taskDescription, taskContext)
}

func resolveRemoteCodingRequestFlags(kind codingRequestKind, taskDescription string) (readOnlyInquiry, operationalRequest bool) {
	switch kind {
	case codingRequestInquiry:
		return true, false
	case codingRequestOperational:
		return false, true
	default:
		// A missing propagated decision must not loosen the remote tool surface.
		// Treat it as implementation rather than guessing from task wording.
		return false, false
	}
}

func (r *RemoteCodingSubAgent) executeTask(taskDescription, taskContext string) *RemoteCodingSubAgentResult {
	// Root remote coding run owns one source-preview session. Nested spawn
	// children reuse the parent session ID and must not open/close sessions.
	// Do not attach the remote project path: it is not a local tab path.
	if r != nil && r.sourcePreviewEnabled && r.nestDepth == 0 && r.handler != nil && r.handler.app != nil && r.handler.app.codeEventEmitter != nil {
		previewSessionID := r.sourcePreviewSessionID
		r.handler.app.codeEventEmitter.EmitPreviewSessionStart(previewSessionID)
		defer r.handler.app.codeEventEmitter.EmitSessionEnd(previewSessionID)
	}
	// A new project directory cannot be created through ssh_bash alone because
	// that tool first changes into workDir. Bootstrap it before the agent loop
	// (root only — nested agents inherit an already-bootstrapped project).
	if r.handler == nil || strings.TrimSpace(r.sessionID) == "" || strings.TrimSpace(r.projectDir) == "" {
		return &RemoteCodingSubAgentResult{Status: "failed", Error: "remote coding project context is incomplete"}
	}
	if r.nestDepth == 0 && !r.readOnlyInquiry && !r.operationalRequest {
		bootstrapResult := r.handler.sshExec(map[string]interface{}{
			"session_id":   r.sessionID,
			"command":      "mkdir -p -- " + remoteShellQuote(r.projectDir),
			"wait_seconds": float64(15),
		})
		if remoteCodingToolOutcome(bootstrapResult) != "success" {
			log.Printf("[remote-source-preview] project bootstrap failed session=%q project=%q result=%q", r.sessionID, r.projectDir, truncateRunesV2(bootstrapResult, 300))
			return &RemoteCodingSubAgentResult{Status: "failed", Error: "无法创建或访问远程项目目录", Summary: bootstrapResult}
		}
		log.Printf("[remote-source-preview] project bootstrap ready session=%q project=%q preview=%v", r.sessionID, r.projectDir, r.sourcePreviewEnabled)
	}
	cb := &remoteCodingCallbacks{
		agent:       r,
		task:        taskDescription,
		taskContext: taskContext,
	}

	userText := taskDescription
	userContent := remoteCodingUserContent(r, userText)
	parentSessionID := ""
	userID := ""
	if r.loopCtx != nil {
		parentSessionID = r.loopCtx.ID
		userID = strings.TrimSpace(r.loopCtx.UserID)
	}
	sessionID := fmt.Sprintf("remote-coding-subagent-%d", time.Now().UnixNano())
	traj := startSubAgentTrajectory(
		r.handler,
		"remote_coding_subagent",
		sessionID,
		userID,
		"remote_coding_subagent",
		parentSessionID,
		r.cfg,
		cb.BuildSystemPrompt(userText, true),
		cb.BuildTools(userText),
	)
	if traj != nil {
		defer flushSubAgentTrajectory(traj)
	}
	hooks := r.buildRemoteCodingLoopHooks()
	var result agent.LoopResult
	if userContent != nil && userContent != userText {
		result = agent.RunLoopWithUserContent(cb, userText, userContent, nil, r.httpClient, hooks)
	} else {
		result = agent.RunLoop(cb, userText, nil, r.httpClient, hooks)
	}
	finishSubAgentTrajectory(traj, result)
	if r.handler != nil {
		accumulateLoopResultUsage(r.handler.app, r.cfg, result)
	}
	// Explorer/reviewer nested agents do not mutate files — skip write-oriented
	// post-edit audits (diff re-read / git stat) that would only add noise.
	if r.role == "" || r.role == codingRoleWorker {
		cb.completeRemotePostEditAudit()
	}
	out := cb.applyRemoteVerificationOutcome(remoteCodingSubAgentResultFromLoopResult(result))
	r.normalizeUserFacingTaskOutcome(out)
	if out != nil && out.Status == "success" {
		r.persistLocalizationExperience(taskDescription, cb.localization.snapshot(), cb.commandsRun)
	}
	if out != nil {
		out.Summary = appendCodingAgentTodoTurnNote(out.Summary, cb.todos.snapshot())
	}
	return out
}

// normalizeUserFacingTaskOutcome keeps the implementation engine name out of
// maintenance task results. The task intentionally reuses the remote coding
// engine, but users should see the maintenance intent they selected.
func (r *RemoteCodingSubAgent) normalizeUserFacingTaskOutcome(out *RemoteCodingSubAgentResult) {
	if r == nil || !r.maintenance || out == nil {
		return
	}
	out.Error = strings.NewReplacer(
		"RemoteCodingSubAgent", "远程维护",
		"remoteCodingSubAgent", "远程维护",
		"Remote coding SubAgent", "远程维护",
		"remote coding subagent", "远程维护",
		"Remote coding", "远程维护",
		"remote coding", "远程维护",
	).Replace(out.Error)
	out.Summary = strings.NewReplacer(
		"RemoteCodingSubAgent", "远程维护",
		"remoteCodingSubAgent", "远程维护",
		"Remote coding SubAgent", "远程维护",
		"remote coding subagent", "远程维护",
		"Remote coding", "远程维护",
		"remote coding", "远程维护",
	).Replace(out.Summary)
}

func remoteCodingUserContent(r *RemoteCodingSubAgent, userText string) interface{} {
	if r == nil || r.loopCtx == nil || len(r.loopCtx.CodingAttachments) == 0 {
		return userText
	}
	cfg := r.cfg
	if hasCodingImageAttachment(r.loopCtx.CodingAttachments) && r.handler != nil {
		cfg = r.handler.routeLLMConfigForCodingVision(cfg)
		r.cfg = cfg
	}
	protocol := strings.TrimSpace(cfg.Protocol)
	if protocol == "" {
		protocol = "openai"
	}
	return agent.BuildUserContent(userText, r.loopCtx.CodingAttachments, protocol, cfg.SupportsVision, nil)
}

func remoteCodingSubAgentResultFromLoopResult(result agent.LoopResult) *RemoteCodingSubAgentResult {
	inTok, outTok, cost := codingLoopUsageFields(result.Usage)
	base := func(status, errText string) *RemoteCodingSubAgentResult {
		return &RemoteCodingSubAgentResult{
			Status:       status,
			Error:        errText,
			Summary:      result.Text,
			Iterations:   result.Iterations,
			ToolCalls:    result.ToolCalls,
			InputTokens:  inTok,
			OutputTokens: outTok,
			EstCostRMB:   cost,
			RouteModel:   result.Route.Model,
			RouteSource:  result.Route.Source,
			RouteTask:    result.Route.TaskType,
			RouteReason:  result.Route.Reason,
		}
	}
	if result.Error != "" {
		status := "failed"
		if remoteCodingSubAgentLoopErrorIsCancelled(result.Error) {
			status = "cancelled"
		}
		return base(status, result.Error)
	}
	if result.HardExit {
		return base("failed", "remote coding subagent hard exit")
	}
	if result.AskUser != nil {
		return base("failed", "remote coding subagent requires user input")
	}
	if strings.TrimSpace(result.Text) == "" {
		return base("failed", "remote coding subagent returned empty summary")
	}
	if result.ToolCalls == 0 {
		return base("failed", "remote coding subagent completed without using tools")
	}

	return base("success", "")
}

func remoteCodingSubAgentLoopErrorIsCancelled(errText string) bool {
	lower := strings.ToLower(strings.TrimSpace(errText))
	return lower == "cancelled" || strings.HasPrefix(lower, "cancelled ")
}

func (c *remoteCodingCallbacks) applyRemoteVerificationOutcome(result *RemoteCodingSubAgentResult) *RemoteCodingSubAgentResult {
	if result == nil {
		return result
	}
	filesModified, filesCreated, filesRead, searchesRun, exploredBeforeFirstEdit, commandsRun, lastEditSeq := c.remoteAuditSnapshot()
	// Surface audit paths for sticky multi-turn memory / UI continuity.
	result.FilesModified = uniqueSortedSubAgentStrings(filesModified)
	result.FilesCreated = uniqueSortedSubAgentStrings(filesCreated)
	result.Localization = c.localization.snapshot()

	// Nested explorer/reviewer are inspection-only by tool policy. Do not fail them
	// for missing post-edit confirmation / git diff / implementation "no change".
	role := codingRoleWorker
	if c != nil && c.agent != nil && c.agent.role != "" {
		role = c.agent.role
	}
	if role == codingRoleExplorer || role == codingRoleReviewer {
		return c.applyRemoteInspectionRoleOutcome(result, filesRead, searchesRun, commandsRun, role)
	}
	if c != nil && c.agent != nil && c.agent.readOnlyInquiry {
		if result.Status == "success" && (result.ToolCalls == 0 || (len(filesRead) == 0 && len(searchesRun) == 0 && len(commandsRun) == 0)) {
			result.Status = "failed"
			result.Error = "repository inquiry completed without inspection evidence"
			return result
		}
		if result.Status == "success" {
			result.Summary = strings.TrimSpace(result.Summary) + "\n\n[repository inquiry] read-only inspection evidence gathered; no files changed."
		}
		return result
	}
	if c != nil && c.agent != nil && c.agent.operationalRequest {
		if result.Status == "success" {
			if status, summary, _ := summarizeRemoteOperationalQuality(commandsRun, result.ToolCalls); status != codingSubAgentQualityPassed {
				result.Status = "failed"
				result.Error = compactSubAgentErrorSummary(summary)
				return result
			}
			result.Summary = strings.TrimSpace(result.Summary) + "\n\n[operational request] launch/build command evidence gathered; no source changes were requested. Build output, if any, may have been generated by the command."
		}
		return result
	}

	existingModified := existingSubAgentModifiedFiles(filesModified, filesCreated)
	explorationStatus, explorationSummary := summarizeSubAgentExploration(existingModified, filesRead, searchesRun, exploredBeforeFirstEdit)
	confirmationStatus, confirmationSummary := c.summarizeRemotePostEditConfirmation(filesModified)
	// Documentation-only wrap-up edits (for example, recording the already
	// observed build results in README) do not change the built artifact. They
	// must not invalidate a successful build/run performed earlier in this same
	// plan step. Code and build-config edits still require fresh verification.
	verificationFiles, verificationLastEditSeq := c.remoteVerificationRelevantEdits(filesModified)
	verificationStatus, verificationSummary := summarizeSubAgentVerification(verificationFiles, commandsRun, verificationLastEditSeq)
	// Multi-step plan scaffold/init steps create incomplete skeletons and must
	// not be failed for missing build/test verification. Only relax MISSING when
	// post-edit re-read confirmation already passed (structure evidence exists).
	if verificationStatus == codingSubAgentQualityMissing && confirmationStatus == codingSubAgentQualityPassed {
		taskText := ""
		if c != nil {
			taskText = c.task
		}
		stepTitle, stepDesc := resolveCodingPlanStepFocus("", "", taskText)
		verificationStatus, verificationSummary = maybeRelaxDeferredPlanStepVerification(stepTitle, stepDesc, taskText, verificationStatus, verificationSummary)
	}
	diffStatus, diffSummary := summarizeRemoteDiffSelfCheck(filesModified, commandsRun, lastEditSeq)
	noChangeSummary := summarizeSubAgentNoChangeEvidence(filesModified, filesCreated, filesRead, searchesRun, commandsRun, nil)
	failedCommands := unresolvedFailedSubAgentCommands(filterPostEditSubAgentCommands(commandsRun, lastEditSeq))
	commandSummary := ""
	if len(failedCommands) > 0 && verificationStatus != codingSubAgentQualityFailed {
		commandSummary = summarizeFailedSubAgentCommandWarning(failedCommands)
		if strings.TrimSpace(commandSummary) == "" {
			commandSummary = "remote coding subagent left failed post-edit commands unresolved"
		}
	}
	result.Summary = appendSubAgentExplorationSummary(result.Summary, explorationStatus, explorationSummary)
	result.Summary = appendRemoteConfirmationSummary(result.Summary, confirmationStatus, confirmationSummary)
	result.Summary = appendSubAgentVerificationSummary(result.Summary, verificationStatus, verificationSummary)
	result.Summary = appendRemoteDiffSelfCheckSummary(result.Summary, diffStatus, diffSummary)
	result.Summary = appendRemoteNoChangeEvidenceSummary(result.Summary, noChangeSummary)
	result.Summary = appendRemoteCommandFailureSummary(result.Summary, commandSummary)
	if result.Status != "success" {
		// A task can be satisfied without a write when the requested artifact
		// already exists and a remote compile/run check proves it works. Do not
		// let a later agent-loop closing error turn that verified outcome into a
		// misleading failure in the final task banner. This deliberately does
		// not recover cancellations, edits, or unverified inspection-only runs.
		if remoteCodingCanRecoverVerifiedCompletion(result, filesModified, filesCreated, commandsRun, commandSummary, confirmationStatus, verificationStatus, diffStatus) {
			// Keep the underlying loop issue in the server audit trail. The result
			// itself is intentionally successful because the remote acceptance
			// evidence is stronger than a late, non-task failure from the agent.
			log.Printf("[remote-coding] accepting verified no-change completion despite terminal loop failure: %s", compactSubAgentErrorSummary(result.Error))
			result.Status = "success"
			result.Error = ""
			return result
		}
		return result
	}
	if strings.TrimSpace(noChangeSummary) != "" {
		result.Status = "failed"
		result.Error = compactSubAgentErrorSummary(noChangeSummary)
		return result
	}
	if explorationStatus == codingSubAgentQualityMissing {
		result.Status = "failed"
		if strings.TrimSpace(explorationSummary) == "" {
			explorationSummary = "remote coding subagent edited existing files without reading or searching first"
		}
		result.Error = compactSubAgentErrorSummary(explorationSummary)
		return result
	}
	if issue := summarizeLocalizationQuality(c.task+"\n"+c.taskContext, existingModified, c.localization.snapshot(), c.searchesRun); issue != "" {
		result.Status = "failed"
		result.Error = compactSubAgentErrorSummary(issue)
		return result
	}
	if confirmationStatus == codingSubAgentQualityMissing {
		result.Status = "failed"
		if strings.TrimSpace(confirmationSummary) == "" {
			confirmationSummary = "remote coding subagent did not re-read modified files after editing"
		}
		result.Error = compactSubAgentErrorSummary(confirmationSummary)
		return result
	}
	switch verificationStatus {
	case codingSubAgentQualityFailed, codingSubAgentQualityMissing:
		result.Status = "failed"
		if strings.TrimSpace(verificationSummary) == "" {
			verificationSummary = "remote coding subagent verification did not pass after file changes"
		}
		result.Error = compactSubAgentErrorSummary(verificationSummary)
		return result
	}
	switch diffStatus {
	case codingSubAgentQualityFailed, codingSubAgentQualityMissing:
		result.Status = "failed"
		if strings.TrimSpace(diffSummary) == "" {
			diffSummary = "remote coding subagent did not run a usable git diff/status self-check after file changes"
		}
		result.Error = compactSubAgentErrorSummary(diffSummary)
		return result
	}
	if strings.TrimSpace(commandSummary) != "" {
		result.Status = "failed"
		result.Error = compactSubAgentErrorSummary(commandSummary)
	}
	return result
}

// applyRemoteInspectionRoleOutcome evaluates nested explorer/reviewer results:
// require tool use + a non-empty report; do not require file mutations.
func (c *remoteCodingCallbacks) applyRemoteInspectionRoleOutcome(
	result *RemoteCodingSubAgentResult,
	filesRead []string,
	searchesRun []CodingSubAgentSearchResult,
	commandsRun []CodingSubAgentCommandResult,
	role codingSubAgentRole,
) *RemoteCodingSubAgentResult {
	if result == nil {
		return result
	}
	if result.Status != "success" {
		return result
	}
	hasInspection := len(uniqueSortedSubAgentStrings(filesRead)) > 0 ||
		countSuccessfulSubAgentSearches(searchesRun) > 0 ||
		len(commandsRun) > 0
	if result.ToolCalls == 0 || !hasInspection {
		result.Status = "failed"
		result.Error = fmt.Sprintf("remote %s subagent completed without inspection evidence (ssh_read_file/ssh_bash/ssh_list_dir)", role)
		return result
	}
	if strings.TrimSpace(result.Summary) == "" {
		result.Status = "failed"
		result.Error = fmt.Sprintf("remote %s subagent returned empty summary", role)
		return result
	}
	// Soft-append a note so the parent spawn aggregator knows this was inspection-only.
	note := fmt.Sprintf("\n[%s] inspection-only role: skipped write-oriented verification gates", role)
	if !strings.Contains(result.Summary, note) {
		result.Summary = strings.TrimSpace(result.Summary) + note
	}
	return result
}

func remoteCodingCanRecoverVerifiedCompletion(
	result *RemoteCodingSubAgentResult,
	filesModified, filesCreated []string,
	commandsRun []CodingSubAgentCommandResult,
	commandSummary string,
	confirmationStatus, verificationStatus, diffStatus codingSubAgentQualityStatus,
) bool {
	if result == nil || result.Status != "failed" || !remoteCodingMaxIterationsReached(result.Error) || strings.TrimSpace(commandSummary) != "" {
		return false
	}
	if len(filesModified) == 0 && len(filesCreated) == 0 {
		return countSuccessfulSubAgentVerificationCommands(commandsRun) > 0
	}
	return remoteCodingPostEditEvidenceIsConclusive(confirmationStatus, verificationStatus, diffStatus)
}

func remoteCodingMaxIterationsReached(errText string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(errText)), "max iterations reached")
}

func remoteCodingPostEditEvidenceIsConclusive(confirmationStatus, verificationStatus, diffStatus codingSubAgentQualityStatus) bool {
	if confirmationStatus != codingSubAgentQualityPassed {
		return false
	}
	// A plan step with an explicitly later compile/build/test task has already
	// been narrowed to NOT_NEEDED by maybeRelaxDeferredPlanStepVerification.
	// Its post-edit reads plus diff/non-git audit are conclusive for this step,
	// even if the model reaches its iteration limit while composing the final
	// summary.  Ordinary unverified edits remain MISSING and cannot recover.
	if verificationStatus != codingSubAgentQualityPassed && verificationStatus != codingSubAgentQualityNotNeeded {
		return false
	}
	return diffStatus == codingSubAgentQualityPassed || diffStatus == codingSubAgentQualityNotNeeded
}

func summarizeRemoteDiffSelfCheck(filesModified []string, commands []CodingSubAgentCommandResult, lastEditSeq uint64) (codingSubAgentQualityStatus, string) {
	if len(filesModified) == 0 {
		return codingSubAgentQualityNotNeeded, "未检测到远程文件修改，跳过远程 diff/status 自检要求。"
	}
	var checks []CodingSubAgentCommandResult
	for _, cmd := range commands {
		if lastEditSeq > 0 && cmd.seq > 0 && cmd.seq < lastEditSeq {
			continue
		}
		if isAuditableRemoteDiffSelfCheckCommand(cmd.Command) {
			checks = append(checks, cmd)
			continue
		}
		// Older remote prompts commonly chained status and diff with `;`. Such a
		// command cannot prove a successful diff check because a later command may
		// mask an earlier failure. It can, however, conclusively prove that the
		// directory is not a Git work tree when the captured output retains Git's
		// explicit fatal diagnostic. Keep only that narrow negative evidence so a
		// real self-check is not incorrectly reported as entirely missing.
		if !cmd.Succeeded && isLegacyRemoteDiffSelfCheckProbeCommand(cmd.Command) &&
			remoteDiffSelfCheckHasExplicitNonGitFatal(cmd.Summary) {
			checks = append(checks, cmd)
		}
	}
	if len(checks) == 0 {
		return codingSubAgentQualityMissing, "远程文件已修改，但未记录编辑后的 git diff/status 自检。"
	}

	// Only status/diff successes count as content evidence. A bare successful
	// `git rev-parse --is-inside-work-tree` must not satisfy the gate.
	var contentSuccess []CodingSubAgentCommandResult
	var lastContentSuccessSeq uint64
	for _, cmd := range checks {
		if cmd.Succeeded && isSubAgentDiffSelfCheckContentCommand(cmd.Command) {
			contentSuccess = append(contentSuccess, cmd)
			if cmd.seq > lastContentSuccessSeq {
				lastContentSuccessSeq = cmd.seq
			}
		}
	}

	// Soft non-git / policy-rejection are never hard. Earlier hard failures are
	// superseded by a later content success; hard failures after that success
	// still fail the gate.
	failed := failedSubAgentCommands(checks)
	nonGit, other := splitRemoteNonGitDiffSelfCheckFailures(failed)
	hard := make([]CodingSubAgentCommandResult, 0, len(other))
	for _, cmd := range other {
		if subAgentCommandIsSoftSilencedGitSelfCheckRejection(cmd) {
			continue
		}
		// Recovery: ignore hard failures that happened before the latest
		// successful status/diff. seq==0 entries are only treated as hard when
		// there is no content success (tests and unsequenced audits).
		if len(contentSuccess) > 0 && (cmd.seq == 0 || cmd.seq <= lastContentSuccessSeq) {
			continue
		}
		hard = append(hard, cmd)
	}

	if len(contentSuccess) == 0 {
		if len(hard) > 0 {
			return codingSubAgentQualityFailed, fmt.Sprintf("远程 git diff/status 自检失败：%s", compactFailedVerificationCommandResults(hard))
		}
		if len(nonGit) > 0 {
			return codingSubAgentQualityNotNeeded, fmt.Sprintf("远程目录不是 Git 仓库，跳过 git diff/status 自检；已使用文件审计记录作为改动证据：%s", compactSubAgentVerificationCommandList(nonGit))
		}
		// e.g. only policy rejections, or only a work-tree probe — still missing
		// a real status/diff self-check.
		return codingSubAgentQualityMissing, "远程文件已修改，但未记录可用的 git diff/status 自检（请去掉 2>/dev/null 后重跑 status/diff，或确认非 Git 仓库）。"
	}

	if len(hard) > 0 {
		return codingSubAgentQualityFailed, fmt.Sprintf("远程 git diff/status 自检失败：%s", compactFailedVerificationCommandResults(hard))
	}

	clean := remoteCleanDiffSelfChecks(contentSuccess)
	if len(clean) == len(contentSuccess) {
		return codingSubAgentQualityFailed, fmt.Sprintf("远程 git diff/status 自检显示工作区干净，但审计记录已有远程文件修改：%s", compactSubAgentVerificationCommandList(clean))
	}
	changeEvidence := remoteDiffSelfChecksWithChangeEvidence(contentSuccess)
	if len(changeEvidence) == 0 {
		return codingSubAgentQualityFailed, fmt.Sprintf("远程 git diff/status 自检未返回可识别的文件改动（可能只有 warning 或包装输出）：%s", compactSubAgentVerificationCommandList(contentSuccess))
	}
	return codingSubAgentQualityPassed, fmt.Sprintf("已运行 %d 条远程 diff/status 自检命令：%s", len(contentSuccess), compactSubAgentVerificationCommandList(contentSuccess))
}

func isLegacyRemoteDiffSelfCheckProbeCommand(command string) bool {
	if !subAgentCommandIsPureGitSelfCheckProbes(command) {
		return false
	}
	hasContentProbe := false
	for _, segment := range shellCommandSegments(command) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		switch commandNameBase(segment[0]) {
		case "echo":
			if len(segment) != 2 || !subAgentIsDashSeparatorLine(strings.Trim(normalizeShellExecutableToken(segment[1]), "'\"")) {
				return false
			}
		case "printf", "true", ":":
			// These are accepted by the general soft-failure classifier, but are
			// unnecessary in the legacy command shape and can manufacture output or
			// mask a Git exit status. Do not trust them as audit evidence.
			return false
		case "git":
			if isAuditableGitDiffSelfCheckSegment(segment) {
				hasContentProbe = true
			}
		}
	}
	return hasContentProbe
}

func remoteDiffSelfCheckHasExplicitNonGitFatal(summary string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(summary, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if !(strings.HasPrefix(lower, "fatal:") || strings.HasPrefix(lower, "fatal：")) {
			continue
		}
		if subAgentGitDiffUnavailableBecauseNonGit(line) {
			return true
		}
	}
	return false
}

func splitRemoteNonGitDiffSelfCheckFailures(commands []CodingSubAgentCommandResult) ([]CodingSubAgentCommandResult, []CodingSubAgentCommandResult) {
	nonGit := make([]CodingSubAgentCommandResult, 0)
	other := make([]CodingSubAgentCommandResult, 0)
	for _, cmd := range commands {
		// The shared classifier intentionally treats ambiguous/silenced EXIT:128
		// as soft for unresolved-command reporting. The diff quality gate needs
		// stronger evidence: otherwise a valid repo with no commits (git log 128)
		// can be mislabeled as non-Git and edits pass without a real diff/status.
		if !cmd.Succeeded && remoteDiffSelfCheckHasExplicitNonGitFatal(cmd.Summary) {
			nonGit = append(nonGit, cmd)
			continue
		}
		other = append(other, cmd)
	}
	return nonGit, other
}

func appendRemoteDiffSelfCheckSummary(summary string, status codingSubAgentQualityStatus, diffSummary string) string {
	if strings.TrimSpace(diffSummary) == "" {
		return summary
	}
	label := status.String()
	switch status {
	case codingSubAgentQualityPassed:
		label = "CHECKED"
	case codingSubAgentQualityFailed:
		label = "FAILED"
	case codingSubAgentQualityMissing:
		label = "MISSING"
	case codingSubAgentQualityNotNeeded:
		label = "NOT_NEEDED"
	}
	return strings.TrimSpace(summary) + "\n\n## 远程 Diff 自检\n\n" + label + ": " + diffSummary
}

func appendRemoteCommandFailureSummary(summary string, commandSummary string) string {
	if strings.TrimSpace(commandSummary) == "" {
		return summary
	}
	return strings.TrimSpace(summary) + "\n\n## 命令状态\n\nFAILED: " + commandSummary
}

func appendRemoteNoChangeEvidenceSummary(summary string, noChangeSummary string) string {
	if strings.TrimSpace(noChangeSummary) == "" {
		return summary
	}
	return strings.TrimSpace(summary) + "\n\n## 无改动证据\n\nFAILED: " + noChangeSummary
}

func remoteCleanDiffSelfChecks(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	clean := make([]CodingSubAgentCommandResult, 0)
	for _, cmd := range commands {
		if remoteDiffSelfCheckLooksClean(cmd) {
			clean = append(clean, cmd)
		}
	}
	return clean
}

func remoteDiffSelfChecksWithChangeEvidence(commands []CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	evidence := make([]CodingSubAgentCommandResult, 0)
	for _, cmd := range commands {
		if remoteDiffSelfCheckHasChangeEvidence(cmd) {
			evidence = append(evidence, cmd)
		}
	}
	return evidence
}

func remoteDiffSelfCheckHasChangeEvidence(cmd CodingSubAgentCommandResult) bool {
	lines := remoteDiffSelfCheckMeaningfulLines(cmd.Summary)
	if len(lines) == 0 {
		return false
	}
	hasStatus, hasDiff := remoteDiffSelfCheckProbeKinds(cmd.Command)
	if hasStatus {
		for i, line := range lines {
			if remoteGitStatusPorcelainLine(line, i == 0) {
				return true
			}
		}
		// Keep compatibility with deliberately requested long-format status.
		joined := strings.ToLower(strings.Join(lines, "\n"))
		return strings.Contains(joined, "changes not staged for commit") ||
			strings.Contains(joined, "changes to be committed") ||
			strings.Contains(joined, "untracked files:")
	}
	if hasDiff {
		for _, line := range lines {
			text := strings.TrimSpace(line)
			if strings.HasPrefix(text, "diff --git ") || strings.HasPrefix(text, "@@ ") ||
				strings.HasPrefix(text, "--- a/") || strings.HasPrefix(text, "+++ b/") {
				return true
			}
			// `git diff --stat` entries use a `path | count +/-` shape.
			if bar := strings.LastIndex(text, "|"); bar > 0 {
				right := strings.TrimSpace(text[bar+1:])
				if strings.ContainsAny(right, "+-") {
					return true
				}
			}
		}
	}
	return false
}

func remoteDiffSelfCheckProbeKinds(command string) (hasStatus, hasDiff bool) {
	for _, segment := range shellCommandSegments(strings.ToLower(strings.Join(strings.Fields(command), " "))) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 || commandNameBase(segment[0]) != "git" {
			continue
		}
		for _, arg := range segment[1:] {
			arg = normalizeShellExecutableToken(arg)
			if strings.HasPrefix(arg, "-") {
				continue
			}
			switch arg {
			case "status":
				hasStatus = true
			case "diff":
				hasDiff = true
			}
			break
		}
	}
	return hasStatus, hasDiff
}

func remoteGitStatusPorcelainLine(line string, allowTrimmedLeadingColumn bool) bool {
	line = strings.TrimRight(line, "\r\n")
	valid := func(b byte) bool {
		return strings.ContainsRune(" MADRCU?!T", rune(b))
	}
	if len(line) >= 4 && line[2] == ' ' && valid(line[0]) && valid(line[1]) &&
		(line[0] != ' ' || line[1] != ' ') {
		return true
	}
	// compactCommandResult trims the whole captured result. When the first real
	// output line is an unstaged porcelain entry (" M path"), that outer trim
	// removes its significant leading column and leaves "M path". Accept only
	// this exact one-column-loss shape; later lines retain both columns above.
	return allowTrimmedLeadingColumn && len(line) >= 3 && line[1] == ' ' && valid(line[0]) && line[0] != ' '
}

func remoteDiffSelfCheckMeaningfulLines(summary string) []string {
	lines := strings.Split(strings.ReplaceAll(summary, "\r\n", "\n"), "\n")
	meaningful := make([]string, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimRight(remoteCodingStripSimpleANSI(line), " \t\r")
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || subAgentIsExitMarkerLine(trimmed) || trimmed == "(无输出)" ||
			remoteDiffSelfCheckShellNoiseLine(text, len(meaningful) == 0) || subAgentIsDashSeparatorLine(trimmed) ||
			subAgentLooksLikeBase64BlobLine(trimmed) {
			continue
		}
		meaningful = append(meaningful, text)
	}
	return meaningful
}

func remoteDiffSelfCheckShellNoiseLine(line string, firstMeaningfulLine bool) bool {
	// Porcelain status deliberately uses its first two columns; entries such as
	// " M file" and "?? file" must be recognized before generic PTY cleanup,
	// whose TrimSpace-based `$ ` rule would otherwise discard a filename `$ x`.
	if remoteGitStatusPorcelainLine(line, firstMeaningfulLine) {
		return false
	}
	return subAgentRemoteShellNoiseLine(line)
}

func remoteDiffSelfCheckLooksClean(cmd CodingSubAgentCommandResult) bool {
	output := strings.TrimSpace(cmd.Summary)
	if output == "" {
		return true
	}
	meaningful := remoteDiffSelfCheckMeaningfulLines(output)
	if len(meaningful) == 0 {
		return true
	}
	joined := strings.ToLower(strings.Join(meaningful, "\n"))
	if strings.Contains(joined, "nothing to commit") &&
		(strings.Contains(joined, "working tree clean") || strings.Contains(joined, "working tree is clean")) {
		return true
	}
	return len(meaningful) == 1 && strings.EqualFold(strings.TrimSpace(meaningful[0]), "no changes")
}

func isRemoteDiffSelfCheckCommand(command string) bool {
	return isSubAgentDiffSelfCheckCommand(command)
}

// isAuditableRemoteDiffSelfCheckCommand accepts only a transparent sequence of
// directory changes and git status/diff probes joined by `&&`. A trailing
// `echo $?` or a semicolon chain can make ssh_bash report success even when
// an earlier git probe failed, so neither can be diff evidence.
func isAuditableRemoteDiffSelfCheckCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" || !isRemoteDiffSelfCheckCommand(normalized) {
		return false
	}
	fields := shellCommandFields(normalized)
	// Only `&&` propagates every probe's status to the shell result. Other
	// operators can hide failures or detach a probe from the command outcome.
	for _, field := range fields {
		switch normalizeShellCommandToken(field) {
		case "||", "|", "&", ";":
			return false
		}
	}
	for _, field := range shellCommandFields(normalized) {
		token := normalizeShellCommandToken(field)
		if token == "2>&1" || token == "1>&2" || isShellVerificationOutputRedirectionToken(token) {
			return false
		}
	}
	for _, segment := range shellCommandSegments(normalized) {
		if len(segment) == 0 {
			continue
		}
		if isAuditableGitDiffSelfCheckSegment(segment) {
			continue
		}
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		switch commandNameBase(segment[0]) {
		case "cd", "pushd", "popd":
			continue
		default:
			return false
		}
	}
	return true
}

func isAuditableGitDiffSelfCheckSegment(segment []string) bool {
	segment = stripVerificationCommandPrefixes(segment)
	if len(segment) == 0 || commandNameBase(segment[0]) != "git" {
		return false
	}
	for _, arg := range segment[1:] {
		arg = normalizeShellExecutableToken(arg)
		if strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "status", "diff":
			return true
		default:
			return false
		}
	}
	return false
}

func appendRemoteConfirmationSummary(summary string, status codingSubAgentQualityStatus, confirmationSummary string) string {
	if strings.TrimSpace(confirmationSummary) == "" {
		return summary
	}
	label := status.String()
	switch status {
	case codingSubAgentQualityPassed:
		label = "PASS"
	case codingSubAgentQualityMissing:
		label = "MISSING"
	case codingSubAgentQualityNotNeeded:
		label = "NOT_NEEDED"
	}
	return strings.TrimSpace(summary) + "\n\n## 确认状态\n\n" + label + ": " + confirmationSummary
}

func (c *remoteCodingCallbacks) summarizeRemotePostEditConfirmation(filesModified []string) (codingSubAgentQualityStatus, string) {
	if len(filesModified) == 0 {
		return codingSubAgentQualityNotNeeded, "未检测到远程文件修改，跳过修改后读取确认要求。"
	}
	missing := c.remoteFilesMissingPostEditRead()
	if len(missing) == 0 {
		return codingSubAgentQualityPassed, fmt.Sprintf("已在最终编辑后重新读取 %d 个远程修改文件。", len(uniqueSortedSubAgentStrings(filesModified)))
	}
	return codingSubAgentQualityMissing, fmt.Sprintf("远程文件修改后缺少 ssh_read_file 确认：%s", strings.Join(missing, ", "))
}

func (c *remoteCodingCallbacks) remoteFilesMissingPostEditRead() []string {
	if c == nil || len(c.fileEdits) == 0 {
		return nil
	}
	lastEditByPath := make(map[string]remoteCodingFileAuditEvent)
	for _, edit := range c.fileEdits {
		key := subAgentPathEvidenceKey(edit.Path)
		if key == "" {
			continue
		}
		if prev, ok := lastEditByPath[key]; !ok || edit.Seq > prev.Seq {
			lastEditByPath[key] = edit
		}
	}
	confirmed := make(map[string]bool, len(lastEditByPath))
	for _, read := range c.fileReads {
		key := subAgentPathEvidenceKey(read.Path)
		edit, ok := lastEditByPath[key]
		if ok && read.Seq > edit.Seq {
			confirmed[key] = true
		}
	}
	missing := make([]string, 0)
	for key, edit := range lastEditByPath {
		if !confirmed[key] {
			missing = append(missing, edit.Path)
		}
	}
	return uniqueSortedSubAgentStrings(missing)
}

func (c *remoteCodingCallbacks) remoteVerificationRelevantEdits(filesModified []string) ([]string, uint64) {
	if c == nil {
		return nil, 0
	}
	paths := make([]string, 0, len(filesModified))
	for _, path := range filesModified {
		if !remoteCodingDocumentationPath(path) {
			paths = append(paths, path)
		}
	}
	var last uint64
	for _, edit := range c.fileEdits {
		if edit.Seq > last && !remoteCodingDocumentationPath(edit.Path) {
			last = edit.Seq
		}
	}
	return uniqueSortedSubAgentStrings(paths), last
}

func remoteCodingDocumentationPath(path string) bool {
	base := strings.ToLower(strings.TrimSpace(pathpkg.Base(strings.ReplaceAll(path, "\\", "/"))))
	// CMakeLists.txt is build configuration, not documentation.
	if base == "cmakelists.txt" {
		return false
	}
	if base == "readme" || strings.HasPrefix(base, "readme.") || base == "changelog" || strings.HasPrefix(base, "changelog.") {
		return true
	}
	return strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".mdx") || strings.HasSuffix(base, ".rst")
}

func (c *remoteCodingCallbacks) remoteAuditSnapshot() ([]string, []string, []string, []CodingSubAgentSearchResult, bool, []CodingSubAgentCommandResult, uint64) {
	if c == nil {
		return nil, nil, nil, nil, true, nil, 0
	}
	files := append([]string(nil), c.filesModified...)
	created := append([]string(nil), c.filesCreated...)
	read := append([]string(nil), c.filesRead...)
	searches := append([]CodingSubAgentSearchResult(nil), c.searchesRun...)
	commands := append([]CodingSubAgentCommandResult(nil), c.commandsRun...)
	return files, created, read, searches, c.remoteExploredBeforeFirstEdit(), commands, c.lastEditSeq
}

func (c *remoteCodingCallbacks) completeRemotePostEditAudit() {
	if c == nil || c.ShouldStop() || len(c.filesModified) == 0 {
		return
	}
	c.completeRemotePostEditReads()
	c.completeRemoteDiffSelfCheck()
}

func (c *remoteCodingCallbacks) completeRemotePostEditReads() {
	for _, path := range c.remoteFilesMissingPostEditRead() {
		if c.ShouldStop() {
			return
		}
		result := c.sshReadFile(map[string]interface{}{
			"path":  path,
			"limit": float64(200),
		})
		if remoteCodingToolOutcome(result) != "success" {
			log.Printf("[remote-subagent] post-edit read audit failed: path=%q result_tail=%q", compactCodingSubAgentLogText(path, 300), compactRemoteCodingResultTail(result, 800))
		}
	}
}

func compactRemoteCodingResultTail(result string, maxRunes int) string {
	result = compactCodingSubAgentLogText(result, 0)
	if maxRunes <= 0 {
		return result
	}
	runes := []rune(result)
	if len(runes) <= maxRunes {
		return result
	}
	return "..." + string(runes[len(runes)-maxRunes:])
}

func (c *remoteCodingCallbacks) completeRemoteDiffSelfCheck() {
	status, _ := summarizeRemoteDiffSelfCheck(c.filesModified, c.commandsRun, c.lastEditSeq)
	if status != codingSubAgentQualityMissing || c.ShouldStop() {
		return
	}
	workDir := c.defaultRemoteWorkingDir()
	// One dedicated status probe is sufficient and preserves an auditable exit:
	// dirty/untracked Git files produce content, a clean tree is detected as
	// suspicious against the file audit, and a non-Git directory retains Git's
	// explicit fatal diagnostic for the NOT_NEEDED path. A preliminary rev-parse
	// used to return early in non-Git directories, leaving no status/diff evidence.
	result := c.sshBash(map[string]interface{}{
		"command":     remotePostEditDiffSelfCheckCommand,
		"working_dir": workDir,
	})
	if remoteCodingToolOutcome(result) != "success" {
		log.Printf("[remote-subagent] status audit command did not pass (often non-git): result=%q", compactCodingSubAgentLogText(result, 800))
	}
}

const remotePostEditDiffSelfCheckCommand = "git status --porcelain=v1 --untracked-files=all"

func (c *remoteCodingCallbacks) nextRemoteAuditSeq() uint64 {
	if c == nil {
		return 0
	}
	c.eventSeq++
	return c.eventSeq
}

func (c *remoteCodingCallbacks) trackRemoteFileChanged(path string, created bool) {
	if c == nil || strings.TrimSpace(path) == "" {
		return
	}
	seq := c.nextRemoteAuditSeq()
	c.filesModified = append(c.filesModified, path)
	c.fileEdits = append(c.fileEdits, remoteCodingFileAuditEvent{Path: path, Seq: seq})
	if created {
		c.filesCreated = append(c.filesCreated, path)
	}
	if c.firstEditSeq == 0 {
		c.firstEditSeq = seq
	}
	c.lastEditSeq = seq
}

func (c *remoteCodingCallbacks) trackRemoteFileRead(path string) {
	if c == nil || strings.TrimSpace(path) == "" {
		return
	}
	seq := c.nextRemoteAuditSeq()
	c.filesRead = append(c.filesRead, path)
	c.fileReads = append(c.fileReads, remoteCodingFileAuditEvent{Path: path, Seq: seq})
	if c.firstReadSeq == 0 {
		c.firstReadSeq = seq
	}
}

func (c *remoteCodingCallbacks) remoteExploredBeforeFirstEdit() bool {
	if c == nil || c.firstEditSeq == 0 {
		return true
	}
	return (c.firstReadSeq > 0 && c.firstReadSeq < c.firstEditSeq) || (c.firstSearchSeq > 0 && c.firstSearchSeq < c.firstEditSeq)
}

func (c *remoteCodingCallbacks) trackRemoteCommand(command, workingDir, result string, succeeded bool) {
	if c == nil || strings.TrimSpace(command) == "" {
		return
	}
	seq := c.nextRemoteAuditSeq()
	c.commandsRun = append(c.commandsRun, CodingSubAgentCommandResult{
		Command:    command,
		WorkingDir: workingDir,
		Succeeded:  succeeded,
		Summary:    compactCommandResult(result),
		seq:        seq,
	})
	if !succeeded {
		if !remoteCodingCommandFailureIsDiagnostic(command, result) {
			c.logRemoteCommandFailure(seq, command, workingDir, result)
		}
	}
	if succeeded && isRemoteCodingExplorationCommand(command) {
		search := CodingSubAgentSearchResult{
			Tool:      remoteCodingExplorationToolName(command),
			Query:     compactSubAgentSearchText(command),
			Path:      compactSubAgentPathText(workingDir),
			Succeeded: true,
			Summary:   compactSearchResult(result),
			seq:       seq,
		}
		c.searchesRun = append(c.searchesRun, search)
		if c.firstSearchSeq == 0 && subAgentSearchProvidesExplorationEvidence(search) {
			c.firstSearchSeq = seq
		}
	}
}

func remoteCodingCommandFailureIsDiagnostic(command, result string) bool {
	entry := CodingSubAgentCommandResult{
		Command: command,
		Summary: result,
	}
	// A diff/status probe in a non-Git directory is expected during remote
	// exploration. It remains visible as skipped audit evidence, not an error.
	if subAgentCommandIsSoftNonGitDiffSelfCheckFailure(entry) {
		return true
	}
	// Missing workdir/path on env-check probes (ls/test, or any probe that
	// never ran because cd into working_dir failed) is a valid negative
	// finding, not a hard task failure.
	if subAgentCommandIsSoftInspectionProbeFailure(entry) {
		return true
	}
	return subAgentCommandFailureCanBeResolvedByLaterVerification(entry)
}

func (c *remoteCodingCallbacks) logRemoteCommandFailure(seq uint64, command, workingDir, result string) {
	sessionID := ""
	projectDir := ""
	task := ""
	if c != nil && c.agent != nil {
		sessionID = c.agent.sessionID
		projectDir = c.agent.projectDir
	}
	if c != nil {
		task = compactCodingSubAgentLogText(c.task, 300)
	}
	log.Printf("[remote-subagent] shell command failed: tool=ssh_bash outcome=failed seq=%d session=%q project=%q task=%q workdir=%q command=%q result=%q",
		seq,
		sessionID,
		projectDir,
		task,
		compactCodingSubAgentLogText(workingDir, 300),
		compactCodingSubAgentLogText(command, 500),
		compactCodingSubAgentLogText(result, 800),
	)
}

func (c *remoteCodingCallbacks) trackRemoteSearch(tool, query, path, result string, succeeded bool, toolArgs ...map[string]interface{}) {
	if c == nil || strings.TrimSpace(tool) == "" {
		return
	}
	seq := c.nextRemoteAuditSeq()
	summary := compactSearchResult(result)
	if strings.EqualFold(strings.TrimSpace(tool), "web_search") || strings.EqualFold(strings.TrimSpace(tool), "web_fetch") {
		summary = truncateLocalizationWebAudit(result)
	}
	var args map[string]interface{}
	if len(toolArgs) > 0 {
		args = toolArgs[0]
	}
	fetchOffset, fetchNextOffset, fetchTotalChars, fetchHasMore, fetchRangeKnown := localizationWebFetchPagination(tool, args, result)
	search := CodingSubAgentSearchResult{
		Tool:             strings.TrimSpace(tool),
		Query:            compactSubAgentSearchText(query),
		Path:             compactSubAgentPathText(path),
		Succeeded:        succeeded,
		Summary:          summary,
		FetchOffset:      fetchOffset,
		FetchNextOffset:  fetchNextOffset,
		FetchTotalChars:  fetchTotalChars,
		FetchHasMore:     fetchHasMore,
		FetchRangeKnown:  fetchRangeKnown,
		FetchAuditKnown:  strings.EqualFold(strings.TrimSpace(tool), "web_fetch"),
		FetchResolvedURL: localizationWebFetchResolvedURL(result),
		seq:              seq,
	}
	c.searchesRun = append(c.searchesRun, search)
	if strings.EqualFold(strings.TrimSpace(tool), "web_search") || strings.EqualFold(strings.TrimSpace(tool), "web_fetch") {
		log.Printf("[remote-research] project=%q %s", remoteLocalizationLogProject(c), localizationResearchToolDebugSummary(search))
	}
	if succeeded && c.firstSearchSeq == 0 && subAgentSearchProvidesExplorationEvidence(search) {
		c.firstSearchSeq = seq
	}
}

func isRemoteCodingExplorationCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if normalized == "" {
		return false
	}
	for _, segment := range shellCommandSegments(normalized) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		name := commandNameBase(segment[0])
		switch name {
		case "codegraph":
			if len(segment) >= 2 && (segment[1] == "explore" || segment[1] == "node") {
				return true
			}
		case "rg", "ripgrep", "grep":
			return true
		case "git":
			if len(segment) >= 2 && segment[1] == "grep" {
				return true
			}
		}
	}
	return false
}

func remoteCodingExplorationToolName(command string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	for _, segment := range shellCommandSegments(normalized) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		name := commandNameBase(segment[0])
		if name == "git" && len(segment) >= 2 && segment[1] == "grep" {
			return "git grep"
		}
		switch name {
		case "codegraph", "rg", "ripgrep", "grep":
			return name
		}
	}
	return "ssh_bash"
}

func (c *remoteCodingCallbacks) trackRemoteTaskCheckResult(result string) {
	command, workingDir := remoteCodingTaskStatusCommandAndWorkingDir(result)
	if command == "" {
		return
	}
	if !remoteCodingTaskStatusCompleted(result) && remoteCodingToolOutcome(result) == "success" {
		return
	}
	c.trackRemoteCommand(command, workingDir, result, remoteCodingToolOutcome(result) == "success" && remoteCodingTaskStatusCompletedSuccessfully(result))
}

func remoteCodingTaskStatusCommand(result string) string {
	for _, line := range remoteCodingTaskHeaderLines(result) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "command:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, line[:len("command:")]))
	}
	return ""
}

func remoteCodingTaskStatusCommandAndWorkingDir(result string) (string, string) {
	return remoteCodingCommandAndWorkingDir(remoteCodingTaskStatusCommand(result))
}

func remoteCodingCommandAndWorkingDir(command string) (string, string) {
	command = strings.TrimSpace(command)
	fields := shellCommandFields(command)
	if len(fields) < 4 || normalizeShellCommandToken(fields[0]) != "cd" {
		return command, ""
	}
	dirIndex := 1
	if fields[dirIndex] == "--" {
		dirIndex++
	}
	if dirIndex >= len(fields)-2 || fields[dirIndex] == "" || normalizeShellCommandToken(fields[dirIndex+1]) != "&&" {
		return command, ""
	}
	rest := strings.TrimSpace(strings.Join(fields[dirIndex+2:], " "))
	if rest == "" {
		return command, ""
	}
	return rest, fields[dirIndex]
}

func remoteCodingTaskStatusCompleted(result string) bool {
	for _, line := range remoteCodingTaskHeaderLines(strings.ToLower(result)) {
		status, ok := remoteCodingToolResultLineFieldValue(strings.TrimSpace(line), "status")
		if ok && (status == "completed" || status == "failed" || status == "killed" || status == "cancelled" || status == "error") {
			return true
		}
	}
	return false
}

func remoteCodingTaskStatusCompletedSuccessfully(result string) bool {
	lower := strings.ToLower(result)
	hasCompleted := false
	hasExitZero := false
	for _, line := range remoteCodingTaskHeaderLines(lower) {
		line = strings.TrimSpace(line)
		if status, ok := remoteCodingToolResultLineFieldValue(line, "status"); ok && status == "completed" {
			hasCompleted = true
		}
		if value, ok := remoteCodingToolResultLineFieldValue(line, "exit_code", "exit"); ok && remoteCodingExitCodeValueIsZero(value) {
			hasExitZero = true
		}
	}
	return hasCompleted && hasExitZero
}

func remoteCodingTaskHeaderLines(result string) []string {
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.ToLower(line))
		if strings.HasPrefix(trimmed, "--- latest log") || strings.HasPrefix(trimmed, "--- log") {
			return lines[:i]
		}
	}
	return lines
}

// --- LoopCallbacks Implementation ---

type remoteCodingCallbacks struct {
	agent       *RemoteCodingSubAgent
	task        string
	taskContext string

	eventSeq       uint64
	firstReadSeq   uint64
	firstSearchSeq uint64
	firstEditSeq   uint64
	lastEditSeq    uint64
	filesModified  []string
	filesCreated   []string
	filesRead      []string
	fileEdits      []remoteCodingFileAuditEvent
	fileReads      []remoteCodingFileAuditEvent
	knownExisting  map[string]bool
	commandsRun    []CodingSubAgentCommandResult
	searchesRun    []CodingSubAgentSearchResult
	localization   codingSubAgentLocalizationState

	// Local workbench extensions (skills / MCP) for full remote coding env.
	localExtSelected bool
	localExtSkills   []codingSubAgentSkillMatch
	localExtMCP      []codingSubAgentMCPToolMatch

	// Agent-internal Claude Code / Codex-style step checklist for this turn.
	todos codingAgentTodoState
}

type remoteCodingFileAuditEvent struct {
	Path string
	Seq  uint64
}

func (c *remoteCodingCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	if c == nil || c.agent == nil {
		return corelib.MaclawLLMConfig{}
	}
	return c.agent.cfg
}

func (c *remoteCodingCallbacks) GetMaxIterations() int {
	if c != nil && c.agent != nil && c.agent.nestDepth > 0 {
		role := c.agent.role
		if role == "" {
			role = codingRoleWorker
		}
		return config.EffectiveMaxIterations(codingSpawnRoleMaxIterations(role))
	}
	// The root remote coding task is a user-facing agent turn. Honor the same
	// configurable budget as the other agent surfaces instead of hard-capping it
	// at 50: the workflow creates loopCtx from "Agent 最大推理轮数".
	if c != nil && c.agent != nil && c.agent.loopCtx != nil && c.agent.loopCtx.MaxIterations() > 0 {
		return config.EffectiveMaxIterations(c.agent.loopCtx.MaxIterations())
	}
	// No root loop context is normally only possible in lightweight tests or
	// bootstrap error paths. Keep the fallback consistent with the global agent
	// setting's default rather than reintroducing a hidden remote-only cap.
	return config.EffectiveMaxIterations(0)
}

func (c *remoteCodingCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	projectDir, workDir, taskContext := "", "", ""
	nestDepth := 0
	role := codingRoleWorker
	if c != nil {
		taskContext = c.taskContext
		if c.agent != nil {
			projectDir = c.agent.projectDir
			workDir = c.agent.workDir
			nestDepth = c.agent.nestDepth
			if c.agent.role != "" {
				role = c.agent.role
			}
		}
	}
	var prompt string
	inspectionRole := nestDepth > 0 && (role == codingRoleExplorer || role == codingRoleReviewer)
	if nestDepth > 0 {
		prompt = "## Nested remote coding subagent\n" + codingSpawnRolePromptHint(role) + "\n\n"
		if inspectionRole {
			prompt += buildRemoteInspectionRoleSystemPrompt(projectDir, workDir, role, taskContext)
		} else {
			prompt += buildNestedFullCodingEnvironmentPromptPreamble()
			prompt += buildRemoteCodingSystemPrompt(projectDir, workDir, taskContext)
		}
	} else if c != nil && c.agent != nil && c.agent.readOnlyInquiry {
		prompt = buildRemoteInquirySystemPrompt(projectDir, workDir)
	} else if c != nil && c.agent != nil && c.agent.operationalRequest {
		prompt = buildRemoteOperationalSystemPrompt(projectDir, workDir)
	} else {
		prompt = buildFullCodingEnvironmentPromptPreamble() + buildRemoteCodingSystemPrompt(projectDir, workDir, taskContext)
	}
	// In-agent plan/checklist (not workbench multi-task orchestration).
	if !inspectionRole && !(c != nil && c.agent != nil && (c.agent.readOnlyInquiry || c.agent.operationalRequest)) {
		prompt += codingAgentTodoPromptSection
	}

	// Inject knowledge from coding experience store + general knowledge store.
	if c != nil {
		if sections := c.buildRemoteKnowledgePromptSections(); sections != "" {
			prompt += sections
		}
	}
	// Skills/MCP for root + nested workers only (inspection roles stay lean).
	if !inspectionRole && c != nil && !(c.agent != nil && (c.agent.readOnlyInquiry || c.agent.operationalRequest)) {
		prompt += "\n## 本地扩展能力\n远程改码通过 SSH 工具完成；本机 Skill / MCP 仍可调用（manage_skill / call_mcp_tool），用于文档、浏览器自动化等辅助能力。\n"
		c.ensureLocalWorkbenchExtensions()
		if section := buildCodingSubAgentSkillSection(c.localExtSkills); section != "" {
			prompt += section
		}
		if section := buildCodingSubAgentMCPSection(c.localExtMCP); section != "" {
			prompt += section
		}
	}
	return prompt
}

func (c *remoteCodingCallbacks) ensureLocalWorkbenchExtensions() {
	if c == nil || c.localExtSelected {
		return
	}
	c.localExtSelected = true
	if c.agent == nil || c.agent.handler == nil {
		return
	}
	// Reuse local CodingSubAgent skill/MCP selection in full-environment mode.
	sa := &CodingSubAgent{handler: c.agent.handler, fullEnvironment: true}
	cb := &codingSubAgentCallbacks{
		subagent: sa,
		task:     &TaskItem{Index: 1, Title: c.task, Description: c.task},
	}
	c.localExtSkills = cb.selectRelevantSkillsForTask(c.task)
	c.localExtMCP = cb.selectRelevantMCPToolsForTask(c.task)
}

func (c *remoteCodingCallbacks) localWorkbenchCallbacks() *codingSubAgentCallbacks {
	if c == nil || c.agent == nil {
		return nil
	}
	c.ensureLocalWorkbenchExtensions()
	sa := &CodingSubAgent{handler: c.agent.handler, fullEnvironment: true}
	return &codingSubAgentCallbacks{
		subagent:        sa,
		task:            &TaskItem{Index: 1, Title: c.task, Description: c.task},
		matchedSkills:   c.localExtSkills,
		matchedMCPTools: c.localExtMCP,
	}
}

func (c *remoteCodingCallbacks) BuildTools(userText string) []map[string]interface{} {
	tools := remoteCodingToolDefinitions()
	tools = append(tools, buildCodeNavigationToolDefinition(), buildReportLocalizationToolDefinition())
	if c == nil {
		// Keep the callback contract nil-safe. This is used by lightweight
		// prompt/tool-surface checks before a concrete remote agent is bound.
		return append(tools, buildCodingFullEnvExtraToolDefinitions()...)
	}
	// Append knowledge search tools when stores are available.
	if c != nil && c.agent != nil && c.agent.codingKB != nil {
		tools = append(tools, codingKnowledgeSearchToolDef())
	}
	if c != nil && c.agent != nil && c.agent.generalKB != nil {
		tools = append(tools, knowledgeSearchToolDef())
	}
	if c.agent != nil && c.agent.readOnlyInquiry {
		return filterRemoteCodingInquiryTools(tools)
	}
	if c.agent != nil && c.agent.operationalRequest {
		return filterRemoteCodingOperationalTools(tools)
	}
	// Full workbench extras (local research helpers) available during remote coding too.
	tools = append(tools, buildCodingFullEnvExtraToolDefinitions()...)
	// /goal lifecycle on pure remote coding root (not nested inspection agents).
	if c != nil && c.agent != nil && c.agent.nestDepth == 0 {
		tools = append(tools, buildCodingGoalToolDefinition())
	}

	role := codingRoleWorker
	if c != nil && c.agent != nil && c.agent.role != "" {
		role = c.agent.role
	}
	inspectionRole := c != nil && c.agent != nil && c.agent.nestDepth > 0 &&
		(role == codingRoleExplorer || role == codingRoleReviewer)
	if !inspectionRole {
		c.ensureLocalWorkbenchExtensions()
		if len(c.localExtSkills) > 0 {
			tools = append(tools, buildManageSkillToolDefinition())
		}
		if len(c.localExtMCP) > 0 {
			tools = append(tools, buildCallMCPToolDefinition())
		}
	}
	// Codex-style nested subagents on pure remote coding workbench root.
	if c != nil && c.agent != nil && c.agent.canSpawnRemoteCodingAgent() {
		tools = append(tools, buildSpawnCodingAgentToolDefinition())
	}
	// In-agent requirement breakdown + step checklist (workers only).
	if !inspectionRole {
		tools = append(tools, buildCodingAgentTodoToolDefinition())
	}
	if c != nil && c.agent != nil {
		tools = filterRemoteCodingToolsForRole(tools, c.agent)
	}
	return tools
}

func (c *remoteCodingCallbacks) ExecuteTool(name, argsJSON string) string {
	return c.executeRemoteTool(name, argsJSON)
}

func (c *remoteCodingCallbacks) OnToken(delta string) {
	if c != nil && c.agent != nil && c.agent.onToken != nil {
		c.agent.onToken(delta)
	}
}

func (c *remoteCodingCallbacks) OnProgress(text string) {
	if c != nil && c.agent != nil && c.agent.onProgress != nil {
		c.agent.onProgress(text)
	}
}

func (c *remoteCodingCallbacks) OnToolCall(name string) {
	if c != nil && c.agent != nil && c.agent.onProgress != nil {
		// Emit a structured CodingAgentEvent so the frontend renders the same
		// tool activity panel as the local CodingSubAgent.
		event := CodingAgentEvent{
			Version: 1,
			Agent:   codingAgentNameCoding.String(),
			Event:   codingAgentEventKindToolStarted.String(),
			Phase:   codingAgentEventPhaseRunning.String(),
			Detail:  strings.TrimSpace(name),
			Title:   c.userFacingActivityTitle(),
		}
		emitCodingAgentEvent(c.agent.onProgress, event)
	}
}

func (c *remoteCodingCallbacks) OnToolResult(name string) {}

func (c *remoteCodingCallbacks) userFacingActivityTitle() string {
	if c != nil && c.agent != nil && c.agent.maintenance {
		return "远程维护"
	}
	return "远程编码"
}

func (c *remoteCodingCallbacks) ShouldStop() bool {
	if c != nil && c.agent != nil && c.agent.loopCtx != nil {
		return c.agent.loopCtx.IsCancelled()
	}
	return false
}

// LLMRequestContext implements LLMRequestContextProvider for cancellation,
// scheduler leases, and request tracing (same posture as local CodingSubAgent).
func (c *remoteCodingCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	var loopCtx *LoopContext
	if c != nil && c.agent != nil {
		loopCtx = c.agent.loopCtx
	}
	return codingLoopLLMRequestContext(loopCtx, "remote-coding-subagent", iteration)
}

// --- Tool Execution ---

func (c *remoteCodingCallbacks) executeRemoteTool(name, argsJSON string) string {
	startedAt := time.Now()
	canonicalName := strings.ToLower(strings.TrimSpace(name))
	var normalizedArgsJSON string

	// Defer tool_finished event emission — guarantees pairing with tool_started
	// regardless of early returns (parse errors, nil handler, etc.)
	var result string
	defer func() {
		if c != nil && c.agent != nil && c.agent.onProgress != nil {
			duration := time.Since(startedAt)
			outcome := remoteCodingToolOutcome(result)
			event := CodingAgentEvent{
				Version:    1,
				Agent:      codingAgentNameCoding.String(),
				Event:      codingAgentEventKindToolFinished.String(),
				Phase:      codingAgentEventPhaseRunning.String(),
				Detail:     strings.TrimSpace(name),
				Title:      c.userFacingActivityTitle(),
				Command:    remoteCodingToolEventCommand(canonicalName, normalizedArgsJSON),
				Outcome:    outcome,
				DurationMS: duration.Milliseconds(),
			}
			if outcome != "success" {
				summary := result
				if len([]rune(summary)) > 180 {
					summary = string([]rune(summary)[:180]) + "..."
				}
				event.Summary = summary
				if remoteCodingToolFailureIsDiagnostic(canonicalName, argsJSON, result, outcome) {
					event.Severity = "diagnostic"
				}
			}
			emitCodingAgentEvent(c.agent.onProgress, event)
		}
	}()

	if c == nil || c.agent == nil {
		return "remote coding subagent: agent unavailable"
	}

	normalizedArgsJSON = normalizeCodingSubAgentToolArguments(argsJSON)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(normalizedArgsJSON), &args); err != nil {
		result = fmt.Sprintf("参数解析失败: %v", err)
		return result
	}
	if applyRemoteCodingSubAgentToolArgumentAliases(canonicalName, args) {
		if data, err := json.Marshal(args); err == nil {
			normalizedArgsJSON = string(data)
		}
	}

	// Nested role policy (explorer/reviewer) enforced at execution time too.
	toolCheckName := canonicalName
	if toolCheckName == "" {
		toolCheckName = name
	}
	if !c.agent.remoteToolAllowedForRole(toolCheckName) {
		result = fmt.Sprintf("tool %s is not available for nested role %q", name, c.agent.role)
		return result
	}
	if c.agent.readOnlyInquiry && !isRemoteCodingInquiryTool(canonicalName) {
		result = fmt.Sprintf("tool %s is unavailable for a read-only repository inquiry", name)
		return result
	}
	if c.agent.operationalRequest && !isRemoteCodingOperationalTool(canonicalName) {
		result = fmt.Sprintf("tool %s is unavailable for a run/build/demo request", name)
		return result
	}

	switch canonicalName {
	case "coding_knowledge_search":
		result = c.executeRemoteCodingKnowledgeSearch(normalizedArgsJSON)
		return result
	case "knowledge_search":
		result = c.executeRemoteKnowledgeSearch(normalizedArgsJSON)
		return result
	case "manage_skill":
		if local := c.localWorkbenchCallbacks(); local != nil {
			result = local.executeManageSkill(args).Text
			return result
		}
		result = "manage_skill unavailable"
		return result
	case "call_mcp_tool":
		if local := c.localWorkbenchCallbacks(); local != nil {
			result = local.executeCallMCPTool(args).Text
			return result
		}
		result = "call_mcp_tool unavailable"
		return result
	case "web_search":
		if c.agent.handler != nil {
			result = c.agent.handler.toolWebSearch(args)
			c.trackRemoteSearch("web_search", remoteArgStr(args, "query"), "web", result, !codingWebResearchResultLooksFailed(result))
			return result
		}
		result = "错误: web_search unavailable: host handler missing"
		c.trackRemoteSearch("web_search", remoteArgStr(args, "query"), "web", result, false)
		return result
	case "web_fetch":
		if c.agent.handler != nil {
			result = c.agent.handler.toolWebFetch(args)
			c.trackRemoteSearch("web_fetch", remoteArgStr(args, "url"), "web", result, !codingWebFetchResultLooksFailed(result), args)
			return result
		}
		result = "web_fetch unavailable"
		c.trackRemoteSearch("web_fetch", remoteArgStr(args, "url"), "web", result, false, args)
		return result
	case "current_datetime":
		result = formatBtwCurrentDateTime()
		return result
	case codeNavigationToolName:
		result = c.executeRemoteCodeNavigation(args)
		return result
	case reportLocalizationToolName:
		result = c.executeRemoteReportLocalization(args)
		return result
	case "goal":
		if c.agent != nil && c.agent.handler != nil {
			userID := ""
			if c.agent.loopCtx != nil {
				userID = strings.TrimSpace(c.agent.loopCtx.UserID)
			}
			if userID == "" {
				result = c.userFacingRemoteAgentMessage("goal unavailable: remote coding session owner is missing (loopCtx.UserID empty)")
				return result
			}
			result = c.agent.handler.toolGoalForUser(userID, args)
			return result
		}
		result = "goal unavailable"
		return result
	case codingSubAgentSpawnToolName:
		result = c.executeSpawnRemoteCodingAgent(args)
		return result
	case codingAgentTodoToolName:
		result = c.executeRemoteTodoWrite(normalizedArgsJSON)
		return result
	}

	if !remoteCodingToolRequiresSSHHandler(canonicalName) {
		result = fmt.Sprintf("unknown tool: %s (supports: ssh_*, spawn_coding_agent, goal, todo_write, manage_skill, call_mcp_tool, knowledge search)", name)
		return result
	}
	if c.agent.handler == nil {
		result = c.userFacingRemoteAgentMessage("remote coding subagent: handler unavailable")
		return result
	}

	switch canonicalName {
	case "ssh_read_file":
		result = c.sshReadFile(args)
	case "ssh_write_file":
		if msg := c.requireRemoteLocalizationBeforeBugEdit(args, false); msg != "" {
			result = msg
			break
		}
		result = c.sshWriteFile(args)
	case "ssh_edit_file":
		if msg := c.requireRemoteLocalizationBeforeBugEdit(args, true); msg != "" {
			result = msg
			break
		}
		result = c.sshEditFile(args)
	case "ssh_bash":
		result = c.sshBash(args)
	case "ssh_list_dir":
		result = c.sshListDir(args)
	case "ssh_check_task":
		result = c.sshCheckTask(args)
	}

	return result
}

func (c *remoteCodingCallbacks) executeRemoteTodoWrite(argsJSON string) string {
	if c == nil {
		return "todo_write unavailable"
	}
	var onProgress func(string)
	var userID string
	if c.agent != nil {
		onProgress = c.agent.onProgress
		if c.agent.loopCtx != nil {
			userID = strings.TrimSpace(c.agent.loopCtx.UserID)
		}
	}
	var handler *IMMessageHandler
	if c.agent != nil {
		handler = c.agent.handler
	}
	text, outcome := executeCodingAgentTodoWrite(&c.todos, argsJSON, wrapTodoProgressForOrchestratedPlan(handler, userID, onProgress), func(items []codingAgentTodoItem) {
		if handler != nil && userID != "" {
			publishCodingAgentTodosToUI(handler, userID, items)
		}
	})
	if outcome == codingToolOutcomeSuccess {
		text = annotateTodoChecklistForOrchestratedPlan(handler, userID, text)
	}
	return text
}

func remoteCodingToolRequiresSSHHandler(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_read_file", "ssh_write_file", "ssh_edit_file", "ssh_bash", "ssh_list_dir", "ssh_check_task":
		return true
	default:
		return false
	}
}

func remoteCodingToolEventCommand(name, argsJSON string) string {
	if strings.ToLower(strings.TrimSpace(name)) != remoteSSHBashToolName {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	applyRemoteCodingSubAgentToolArgumentAliases(remoteSSHBashToolName, args)
	return strings.TrimSpace(redactCodingSubAgentFreeformLogText(remoteArgStr(args, "command")))
}
func remoteCodingToolOutcome(result string) string {
	if remoteCodingToolResultLooksBlocked(result) {
		return "blocked"
	}
	// Instrumented remote bash prints "EXIT: N". That marker is authoritative:
	// a successful grep/tail of error logs may contain "error:" / "panic:" / "失败"
	// in the log body while the command itself exited 0.
	if code, ok := remoteCodingParseAuthoritativeExitMarker(result); ok {
		if code != 0 {
			return "failed"
		}
		return "success"
	}
	if remoteCodingToolResultLooksFailed(result) {
		return "failed"
	}
	return "success"
}

// remoteCodingParseAuthoritativeExitMarker accepts a marker only when nothing
// except SSH/PTTY wrapper noise follows it. A program may legitimately print a
// standalone line such as "EXIT: 0" before later failing; that line must not
// override the actual trailing failure text.
func remoteCodingParseAuthoritativeExitMarker(result string) (int, bool) {
	lines := strings.Split(result, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(remoteCodingStripSimpleANSI(strings.TrimRight(lines[i], "\r")))
		if code, ok := remoteCodingParseExitMarker(line); ok {
			return code, true
		}
		if line == "" || subAgentRemoteShellNoiseLine(line) {
			continue
		}
		return 0, false
	}
	return 0, false
}

// remoteCodingParseExitMarker extracts the last instrumented exit marker from
// tool output. Supports both remote bash ("EXIT: N") and ACP-style
// ("---EXIT_CODE:N") markers. Returns (code, true) when a numeric exit marker
// is present. Scans from the end so a successful command's marker wins over
// earlier echo noise.
func remoteCodingParseExitMarker(result string) (int, bool) {
	lines := strings.Split(result, "\n")
	// Prefer tail: instrumented EXIT is near the end; avoid scanning multi-MB dumps fully
	// more than once from the back.
	for i := len(lines) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		// Strip common CSI color codes so colored EXIT markers still parse.
		raw = remoteCodingStripSimpleANSI(raw)
		rest := ""
		switch {
		case len(raw) >= 5 && strings.EqualFold(raw[:5], "EXIT:"):
			rest = strings.TrimSpace(raw[5:])
		case strings.HasPrefix(raw, "---EXIT_CODE:"):
			rest = strings.TrimSpace(raw[len("---EXIT_CODE:"):])
		default:
			continue
		}
		if rest == "" {
			continue
		}
		// Leading integer token only ("0", "127"); reject "0xdead"
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		if end < len(rest) {
			c := rest[end]
			if c != ' ' && c != '\t' {
				continue
			}
		}
		n, err := strconv.Atoi(rest[:end])
		if err != nil {
			continue
		}
		return n, true
	}
	return 0, false
}

// remoteCodingStripSimpleANSI removes common CSI sequences (e.g. \x1b[0m) so
// EXIT markers remain parseable when a remote shell colors output.
func remoteCodingStripSimpleANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\x1b' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if s[i] == '[' {
			i++
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7E) {
				i++
			}
			// skip terminator if present
			continue
		}
		// skip single-char escape
	}
	return b.String()
}

func remoteCodingToolResultLooksBlocked(result string) bool {
	normalized := strings.ToLower(strings.TrimSpace(result))
	return strings.Contains(result, "\u62d2\u7edd\u6267\u884c\u9ad8\u98ce\u9669\u547d\u4ee4") ||
		strings.HasPrefix(normalized, "refusing remote directory access outside the project:") ||
		strings.HasPrefix(normalized, "refusing to modify remote path outside the project:") ||
		strings.HasPrefix(normalized, "refusing to read remote path outside the project:")
}

func remoteCodingToolFailureIsDiagnostic(name, argsJSON, result, outcome string) bool {
	if outcome != "failed" {
		return false
	}
	canonical := strings.ToLower(strings.TrimSpace(name))
	// Exploratory path probes (does this file/dir exist?) are expected misses,
	// not hard failures — match local CodingSubAgent's neutral diagnostic tone.
	if remoteCodingExploratoryLookupTool(canonical) && remoteCodingPathLookupLooksUnsuccessful(result) {
		return true
	}
	if canonical != "ssh_bash" {
		return false
	}
	normalizedArgsJSON := normalizeCodingSubAgentToolArguments(argsJSON)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(normalizedArgsJSON), &args); err != nil {
		return false
	}
	applyRemoteCodingSubAgentToolArgumentAliases("ssh_bash", args)
	command := remoteArgStr(args, "command")
	return remoteCodingCommandFailureIsDiagnostic(command, result)
}

func remoteCodingExploratoryLookupTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_read_file", "ssh_list_dir":
		return true
	default:
		return false
	}
}

func remoteCodingPathLookupLooksUnsuccessful(result string) bool {
	text := strings.ToLower(strings.TrimSpace(result))
	if text == "" {
		return false
	}
	// Hard failures should stay red even if the message also mentions a path.
	for _, marker := range []string{
		"access denied",
		"permission denied",
		"fatal error",
		"traceback",
		"panic:",
	} {
		if strings.Contains(text, marker) {
			return false
		}
	}
	for _, marker := range []string{
		"file not found",
		"no such file",
		"path does not exist",
		"path not found",
		"cannot find the path",
		"could not find the path",
		"cannot access",
		"not a directory",
		"is not a directory",
		"not found:",
		"文件不存在",
		"路径不存在",
		"找不到文件",
		"找不到路径",
		"没有那个文件",
		"不是目录",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func remoteCodingToolResultLooksFailed(result string) bool {
	text := strings.TrimSpace(result)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)

	// Framework / transport failures (structural Chinese phrases — not bare "失败",
	// which appears constantly inside application logs being grepped).
	if strings.HasPrefix(text, "错误") ||
		strings.Contains(text, "参数解析失败") ||
		remoteCodingToolResultHasStructuralChineseFailure(text) {
		return true
	}
	// Explicit maclaw SSH timeout / hang recovery messages
	if strings.Contains(text, "[maclaw] 命令执行超时") ||
		strings.Contains(text, "连续") && strings.Contains(text, "无响应") ||
		strings.Contains(lower, "handler unavailable") {
		return true
	}
	if remoteCodingToolResultHasFailedTaskStatus(lower) || remoteCodingToolResultHasFailedExitCode(lower) {
		return true
	}
	if remoteCodingToolResultHasFailedExitPhrase(lower) {
		return true
	}
	// Content heuristics: only apply to the "command/tool diagnostic" region,
	// not the full body of a log dump. Prefer the status header + first lines
	// and the tail (where EXIT / shell errors usually appear).
	diagnostic := remoteCodingToolResultDiagnosticSlice(lower)
	for _, pattern := range []string{
		"error:",
		"traceback",
		"exception",
		"panic:",
		"no such file or directory",
		"command not found",
		"file not found",
		"permission denied",
		"unavailable",
		"unknown tool",
		"ninja: build stopped: subcommand failed",
	} {
		if strings.Contains(diagnostic, pattern) {
			return true
		}
	}
	return false
}

// remoteCodingToolResultHasStructuralChineseFailure matches MaClaw/SSH framework
// failure phrases without treating arbitrary log lines that merely contain "失败".
func remoteCodingToolResultHasStructuralChineseFailure(text string) bool {
	// Keep this list MaClaw/SSH-tool specific. Generic phrases like "连接失败"
	// / "执行失败" appear constantly inside application logs under diagnosis.
	phrases := []string{
		"写入失败",
		"发送命令失败",
		"自动重连失败",
		"上传失败",
		"下载失败",
		"检查任务失败",
		"终止任务失败",
		"提交后台任务失败",
		"SSH 连接失败",
		"ssh 连接失败",
		"SSH会话已断开，自动重连失败",
		"SSH 会话已断开，自动重连失败",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// remoteCodingToolResultDiagnosticSlice returns a shortened view of tool output
// used for keyword failure heuristics. Full log bodies (e.g. grepping nginx/hub
// error logs) would otherwise trip on "error:" / "exception" in content.
func remoteCodingToolResultDiagnosticSlice(lower string) string {
	lines := strings.Split(lower, "\n")
	if len(lines) <= 24 {
		return lower
	}
	const headN = 12
	const tailN = 12
	var b strings.Builder
	for i := 0; i < headN && i < len(lines); i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	start := len(lines) - tailN
	if start < headN {
		start = headN
	}
	for i := start; i < len(lines); i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	return b.String()
}

func remoteCodingToolResultHasFailedExitPhrase(lower string) bool {
	for _, phrase := range []string{
		"command exited with code",
		"process exited with code",
		"exit status",
	} {
		remaining := lower
		for {
			idx := strings.Index(remaining, phrase)
			if idx < 0 {
				break
			}
			tail := strings.TrimSpace(remaining[idx+len(phrase):])
			fields := strings.FieldsFunc(tail, func(r rune) bool {
				return r < '0' || r > '9'
			})
			if len(fields) == 0 {
				return true
			}
			code, err := strconv.Atoi(fields[0])
			if err != nil || code != 0 {
				return true
			}
			remaining = tail
		}
	}
	return false
}

func remoteCodingToolResultHasFailedTaskStatus(lower string) bool {
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[failed]") || strings.HasPrefix(line, "[killed]") {
			return true
		}
		if status, ok := remoteCodingToolResultJSONFieldValue(line, "status", "state"); ok {
			if status == "failed" || status == "killed" || status == "error" || status == "cancelled" {
				return true
			}
		}
		if status, ok := remoteCodingToolResultLineFieldValue(line, "status", "state"); ok {
			if status == "failed" || status == "killed" || status == "error" || status == "cancelled" {
				return true
			}
		}
	}
	return false
}

func remoteCodingToolResultHasFailedExitCode(lower string) bool {
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if value, ok := remoteCodingToolResultJSONFieldValue(line, "exit_code", "exit", "exit code", "returncode", "return_code"); ok {
			if remoteCodingExitCodeValueLooksFailed(value) {
				return true
			}
		}
		if value, ok := remoteCodingToolResultLineFieldValue(line, "exit_code", "exit", "exit code", "returncode", "return_code"); ok {
			if remoteCodingExitCodeValueLooksFailed(value) {
				return true
			}
		}
	}
	return false
}

func remoteCodingToolResultJSONFieldValue(line string, fields ...string) (string, bool) {
	if !strings.HasPrefix(line, "{") {
		return "", false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return "", false
	}
	for _, field := range fields {
		value, ok := payload[field]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case nil:
			return "null", true
		case string:
			return strings.ToLower(strings.TrimSpace(v)), true
		default:
			return strings.ToLower(strings.TrimSpace(fmt.Sprint(v))), true
		}
	}
	return "", false
}

func remoteCodingToolResultLineFieldValue(line string, fields ...string) (string, bool) {
	line = strings.Trim(strings.TrimSpace(line), "{}[],")
	if line == "" {
		return "", false
	}
	for _, sep := range []string{":", "="} {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.Trim(strings.TrimSpace(parts[0]), `"'`)
		value := strings.Trim(strings.TrimSpace(parts[1]), `"',`)
		for _, field := range fields {
			if key == field {
				return value, true
			}
		}
	}
	return "", false
}

func remoteCodingExitCodeValueLooksFailed(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), `"',`)
	if value == "" || value == "unknown" || value == "none" || value == "null" {
		return false
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if len(fields) == 0 {
		return true
	}
	code, err := strconv.Atoi(fields[0])
	if err != nil {
		return true
	}
	return code != 0
}

func remoteCodingExitCodeValueIsZero(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), `"',`)
	if value == "" || value == "unknown" || value == "none" || value == "null" {
		return false
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if len(fields) == 0 {
		return false
	}
	code, err := strconv.Atoi(fields[0])
	return err == nil && code == 0
}

type remoteHighRiskApprovalState struct {
	mu                 sync.Mutex
	highRiskFullAccess bool
	pathFullAccess     bool
	approvedDirs       map[string]bool
	callback           ScopeApprovalCallback
	auditApproval      func(ScopeApprovalRequest, ScopeApprovalDecision, string)
}

const (
	remoteHighRiskApprovalKind = "remote_high_risk_bash"
	remoteDirectoryWriteKind   = "remote_shell_directory_write"
	remotePathAccessKind       = "remote_path_access"
	remoteSSHBashToolName      = "ssh_bash"
)

func newRemoteHighRiskApprovalState(callback ScopeApprovalCallback, fullAccess bool) *remoteHighRiskApprovalState {
	return &remoteHighRiskApprovalState{
		highRiskFullAccess: fullAccess,
		pathFullAccess:     fullAccess,
		approvedDirs:       make(map[string]bool),
		callback:           callback,
	}
}

func (s *remoteHighRiskApprovalState) configure(callback ScopeApprovalCallback, fullAccess bool, preserveFullAccess bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callback = callback
	if preserveFullAccess && (s.highRiskFullAccess || s.pathFullAccess) {
		return
	}
	s.highRiskFullAccess = fullAccess
	s.pathFullAccess = fullAccess
}

// grantHighRiskFullAccess enables high-risk bash auto-allow without changing path trust.
func (s *remoteHighRiskApprovalState) grantHighRiskFullAccess() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.highRiskFullAccess = true
	s.mu.Unlock()
}

// grantPathFullAccess enables path auto-allow (no out-of-scope prompts).
func (s *remoteHighRiskApprovalState) grantPathFullAccess() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.pathFullAccess = true
	s.mu.Unlock()
}

func rememberRemoteScopeStickyDecision(handler *IMMessageHandler, loopCtx *LoopContext, req ScopeApprovalRequest, decision ScopeApprovalDecision) {
	if handler == nil || decision != ScopeApprovalFullAccess {
		return
	}
	userID := ""
	if loopCtx != nil {
		userID = strings.TrimSpace(loopCtx.UserID)
	}
	if userID == "" {
		return
	}
	switch req.Kind {
	case remoteHighRiskApprovalKind, localHighRiskApprovalKind:
		// Session-scoped high-risk trust for pure coding multi-turn continuity.
		handler.markStickyCodingSessionHighRiskAccess(userID)
	default:
		// Path/dir full access for remote path prompts → session path trust.
		handler.markStickyCodingSessionFullAccess(userID, "remote", req.ProjectPath)
		if dir := strings.TrimSpace(req.Directory); dir != "" {
			handler.rememberStickyApprovedDir(userID, dir)
		}
	}
	// If path + high-risk are both set, upgrade sticky mode so UI shows 完全控制.
	handler.maybeUpgradeStickyPermissionModeToFull(userID)
}

func buildRemoteHighRiskApprovalCallback(handler *IMMessageHandler, loopCtx *LoopContext, onProgress func(string), maintenance bool) ScopeApprovalCallback {
	return func(req ScopeApprovalRequest) ScopeApprovalDecision {
		req.Maintenance = maintenance
		if loopCtx != nil && loopCtx.IsCancelled() {
			recordScopeApprovalAudit(handler, "", req, ScopeApprovalDeny, "cancelled")
			return ScopeApprovalDeny
		}
		if onProgress != nil {
			onProgress(remoteScopeApprovalProgressMessage(req))
		}
		responseCh := make(chan ScopeApprovalDecision, 1)
		approvalID := storePendingScopeApproval(handler, req, responseCh)
		if handler != nil && handler.app != nil {
			emitScopeApprovalEvent(handler.app, approvalID, req)
		}
		timeout := time.NewTimer(scopeApprovalTimeout)
		defer timeout.Stop()
		if loopCtx != nil {
			select {
			case decision := <-responseCh:
				recordScopeApprovalAudit(handler, approvalID, req, decision, "user")
				rememberRemoteScopeStickyDecision(handler, loopCtx, req, decision)
				if shouldPersistRemoteScopeFullAccess(req, decision) && handler != nil && handler.app != nil {
					handler.app.persistSubAgentFullAccess()
				}
				return decision
			case <-loopCtx.CancelC:
				pendingScopeApprovals.Delete(approvalID)
				recordScopeApprovalAudit(handler, approvalID, req, ScopeApprovalDeny, "cancelled")
				return ScopeApprovalDeny
			case <-timeout.C:
				pendingScopeApprovals.Delete(approvalID)
				decision := remoteScopeApprovalTimeoutDecision(req)
				if onProgress != nil {
					onProgress(remoteScopeApprovalTimeoutProgress(req, decision))
				}
				recordScopeApprovalAudit(handler, approvalID, req, decision, "timeout")
				return decision
			}
		}
		select {
		case decision := <-responseCh:
			recordScopeApprovalAudit(handler, approvalID, req, decision, "user")
			rememberRemoteScopeStickyDecision(handler, loopCtx, req, decision)
			if shouldPersistRemoteScopeFullAccess(req, decision) && handler != nil && handler.app != nil {
				handler.app.persistSubAgentFullAccess()
			}
			return decision
		case <-timeout.C:
			pendingScopeApprovals.Delete(approvalID)
			decision := remoteScopeApprovalTimeoutDecision(req)
			recordScopeApprovalAudit(handler, approvalID, req, decision, "timeout")
			return decision
		}
	}
}

func shouldPersistRemoteScopeFullAccess(req ScopeApprovalRequest, decision ScopeApprovalDecision) bool {
	return decision == ScopeApprovalFullAccess && req.Kind != remoteHighRiskApprovalKind
}

func remoteScopeApprovalProgressMessage(req ScopeApprovalRequest) string {
	message := strings.TrimSpace(req.Message)
	remoteActor := "远程编码 SubAgent"
	if req.Maintenance {
		remoteActor = "远程维护"
	}
	switch req.Kind {
	case localHighRiskApprovalKind:
		return fmt.Sprintf("编码 SubAgent 请求执行高风险命令，等待确认...\n命令: %s\n工作目录: %s", req.Path, req.ProjectPath)
	case remoteHighRiskApprovalKind:
		return fmt.Sprintf("%s 请求执行高风险命令，等待确认...\n命令: %s\n工作目录: %s", remoteActor, req.Path, req.ProjectPath)
	case remoteDirectoryWriteKind:
		if message != "" {
			return fmt.Sprintf("%s 请求创建/写入目录，等待确认...\n目录: %s\n项目范围: %s\n%s", remoteActor, req.Directory, req.ProjectPath, message)
		}
		return fmt.Sprintf("%s 请求创建/写入目录，等待确认...\n目录: %s\n项目范围: %s", remoteActor, req.Directory, req.ProjectPath)
	default:
		if message != "" {
			return fmt.Sprintf("%s 请求访问项目目录外路径，等待确认...\n路径: %s\n项目范围: %s\n%s", remoteActor, req.Path, req.ProjectPath, message)
		}
		return fmt.Sprintf("%s 请求访问项目目录外路径，等待确认...\n路径: %s\n项目范围: %s", remoteActor, req.Path, req.ProjectPath)
	}
}

func remoteScopeApprovalTimeoutDecision(req ScopeApprovalRequest) ScopeApprovalDecision {
	return ScopeApprovalDeny
}

func remoteScopeApprovalTimeoutProgress(req ScopeApprovalRequest, decision ScopeApprovalDecision) string {
	if req.Kind == localHighRiskApprovalKind {
		return fmt.Sprintf("本地高风险命令确认超时，已拒绝执行: %s", req.Path)
	}
	if req.Kind == remoteDirectoryWriteKind || req.Kind == remotePathAccessKind || req.Kind == "" {
		return fmt.Sprintf("目录/路径确认超时，已拒绝访问: %s", req.Path)
	}
	return fmt.Sprintf("远程高风险命令确认超时，已拒绝执行: %s", req.Path)
}

func (s *remoteHighRiskApprovalState) check(command, workingDir, rejection string) string {
	if s == nil {
		return rejection
	}
	s.mu.Lock()
	if s.highRiskFullAccess {
		audit := s.auditApproval
		s.mu.Unlock()
		if audit != nil {
			audit(ScopeApprovalRequest{ToolName: remoteSSHBashToolName, Path: command, ProjectPath: workingDir, Directory: workingDir, Kind: remoteHighRiskApprovalKind}, ScopeApprovalFullAccess, "automatic")
		}
		return ""
	}
	callback := s.callback
	s.mu.Unlock()
	if callback == nil {
		return rejection
	}
	decision := callback(ScopeApprovalRequest{
		ToolName:    remoteSSHBashToolName,
		Path:        command,
		ProjectPath: workingDir,
		Directory:   workingDir,
		Kind:        remoteHighRiskApprovalKind,
		Message:     rejection,
		AutoAllow:   false,
	})
	switch decision {
	case ScopeApprovalAllowOnce:
		return ""
	case ScopeApprovalFullAccess:
		s.mu.Lock()
		s.highRiskFullAccess = true
		s.mu.Unlock()
		return ""
	default:
		return rejection
	}
}

func (s *remoteHighRiskApprovalState) checkRemotePath(toolName, path, projectPath, kind, message string, allowDirDecision bool) string {
	if s == nil {
		return message
	}
	s.mu.Lock()
	if s.pathFullAccess {
		audit := s.auditApproval
		s.mu.Unlock()
		if audit != nil {
			audit(ScopeApprovalRequest{ToolName: toolName, Path: path, ProjectPath: projectPath, Directory: remotePathDir(path), Kind: kind}, ScopeApprovalFullAccess, "automatic")
		}
		return ""
	}
	if s.isApprovedLocked(path) {
		audit := s.auditApproval
		s.mu.Unlock()
		if audit != nil {
			audit(ScopeApprovalRequest{ToolName: toolName, Path: path, ProjectPath: projectPath, Directory: remotePathDir(path), Kind: kind}, ScopeApprovalAllowDir, "automatic")
		}
		return ""
	}
	callback := s.callback
	s.mu.Unlock()
	if callback == nil {
		return message
	}
	dir := remotePathDir(path)
	if kind == remoteDirectoryWriteKind {
		dir = remoteCleanPath(path)
	}
	decision := callback(ScopeApprovalRequest{
		ToolName:    toolName,
		Path:        path,
		ProjectPath: projectPath,
		Directory:   dir,
		Kind:        kind,
		Message:     message,
		AutoAllow:   false,
	})
	switch decision {
	case ScopeApprovalAllowOnce:
		return ""
	case ScopeApprovalAllowDir:
		if !allowDirDecision {
			return message
		}
		s.approveDir(dir)
		return ""
	case ScopeApprovalFullAccess:
		s.mu.Lock()
		s.pathFullAccess = true
		s.mu.Unlock()
		return ""
	default:
		return message
	}
}

func (s *remoteHighRiskApprovalState) isApprovedLocked(path string) bool {
	if s == nil || len(s.approvedDirs) == 0 {
		return false
	}
	clean := remoteCleanPath(path)
	for dir := range s.approvedDirs {
		dir = strings.TrimRight(dir, "/")
		if clean == dir || strings.HasPrefix(clean, dir+"/") {
			return true
		}
	}
	return false
}

func (s *remoteHighRiskApprovalState) approveDir(dir string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.approvedDirs == nil {
		s.approvedDirs = make(map[string]bool)
	}
	s.approvedDirs[remoteCleanPath(dir)] = true
}

func (c *remoteCodingCallbacks) sshReadFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	if path == "" {
		return "错误: 需要 path 参数"
	}
	path = c.resolvePath(path)
	if msg := c.requireRemoteProjectScope("ssh_read_file", path); msg != "" {
		return msg
	}
	offset := remoteArgInt(args, 0, 0, 1000000, "offset", "start_line", "start", "startLine")
	limit := remoteArgInt(args, 0, 0, 2000, "limit", "lines", "num_lines", "line_count")
	result := c.execSSH(remoteReadFileRangePythonCommand(path, offset, limit), 10)
	if remoteCodingToolOutcome(result) == "success" && remoteReadFileResultHasUsefulEvidence(result) {
		c.trackRemoteFileRead(path)
		if c.knownExisting == nil {
			c.knownExisting = make(map[string]bool)
		}
		c.knownExisting[remoteCleanPath(path)] = true
		// A range beginning after line one is useful to the agent but is not a
		// faithful source preview of the file; keep the user's current preview.
		if remoteReadCanUpdatePreview(offset) && !remotePreviewOutputIsTransportTruncated(result) {
			c.emitRemoteCodePreview(path, extractRemoteReadPreviewContent(result), "", "read", remotePreviewOutputIsTruncated(result), false)
		}
	}
	return result
}

func remoteReadCanUpdatePreview(offset int) bool { return offset <= 1 }

func remoteReadFileResultHasUsefulEvidence(result string) bool {
	if strings.TrimSpace(result) == "" || strings.Contains(result, "[remote read_file binary/non-UTF8:") {
		return false
	}
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		if _, err := strconv.Atoi(line[:tab]); err == nil {
			return true
		}
	}
	return strings.Contains(result, "[remote read_file EOF: offset 1 is beyond scanned file length 0]")
}

func remotePythonCommand(script string) string {
	// The SSH/PTTY command layer can normalize literal newlines. Python's
	// indentation then becomes invalid, which previously made post-edit reads
	// fail and left the source-preview panel empty. Transfer the complete script
	// as one shell-safe token and decode it only on the remote host.
	encoded := base64EncodeString(script)
	return "python3 -c \"$(printf '%s' " + remoteShellQuote(encoded) + " | base64 -d)\""
}

func remoteReadFileRangePythonCommand(path string, offset, limit int) string {
	pathB64 := base64EncodeString(path)
	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64, sys",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"start = %d",
		"limit = %d",
		"if start < 1:",
		"    start = 1",
		"shown = 0",
		"last_lineno = 0",
		"try:",
		"    with p.open('r', encoding='utf-8', errors='strict') as f:",
		"        for lineno, line in enumerate(f, start=1):",
		"            last_lineno = lineno",
		"            if lineno < start:",
		"                continue",
		"            if shown >= limit:",
		"                sys.stdout.write('\\n[remote read_file truncated: showing lines %%d-%%d; call again with offset=%%d]\\n' %% (start, lineno - 1, lineno))",
		"                break",
		"            sys.stdout.write(f'{lineno}\\t{line}')",
		"            shown += 1",
		"except UnicodeDecodeError:",
		"    size = p.stat().st_size",
		"    sys.stdout.write('[remote read_file binary/non-UTF8: %%d bytes; text line range unavailable for offset=%%d limit=%%d]\\n' %% (size, start, limit))",
		"    sys.exit(0)",
		"if shown == 0 and start > last_lineno:",
		"    sys.stdout.write('[remote read_file EOF: offset %%d is beyond scanned file length %%d]\\n' %% (start, last_lineno))",
	}, "\n"), pathB64, offset, limit)
	return remotePythonCommand(script)
}

func (c *remoteCodingCallbacks) sshWriteFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	content, hasContent := remoteArgRawStr(args, "content")
	if path == "" || !hasContent {
		return "错误: 需要 path 和 content 参数"
	}
	path = c.resolvePath(path)
	if msg := c.requireRemoteProjectScope("ssh_write_file", path); msg != "" {
		return msg
	}

	// Capture the existing remote content before mutation so the preview can show a diff.
	original, originalAvailable, originalTruncated := c.readRemotePreviewContent(path)

	// For large content (>32KB), write in chunks to avoid PTY buffer overflow.
	if len(content) > 32*1024 {
		result := c.sshWriteFileLarge(path, content)
		if remoteCodingToolOutcome(result) == "success" {
			created := remoteWriteFileResultCreated(result)
			c.trackRemoteFileChanged(path, created)
			if updated, ok, truncated := c.readRemotePreviewContent(path); ok {
				opType := "modify"
				if created {
					opType = "create"
				}
				c.emitRemoteCodePreview(path, updated, original, opType, originalTruncated || truncated, !created && !originalAvailable)
			}
		}
		return result
	}

	// Use base64 encoding embedded directly in Python code — no pipes needed.
	// This is PTY-safe since the entire command is a single python3 -c invocation.
	pyScript := remoteWriteFilePythonCommand(path, content)

	result := c.execSSH(pyScript, 15)
	formatted := remoteWriteFileResult(path, len(content), result, false)
	if remoteCodingToolOutcome(formatted) == "success" {
		created := remoteWriteFileResultCreated(result)
		c.trackRemoteFileChanged(path, created)
		if updated, ok, truncated := c.readRemotePreviewContent(path); ok {
			opType := "modify"
			if created {
				opType = "create"
			}
			c.emitRemoteCodePreview(path, updated, original, opType, originalTruncated || truncated, !created && !originalAvailable)
		}
	}
	return formatted
}

func remoteWriteFileResult(path string, contentLen int, commandResult string, chunked bool) string {
	if remoteCodingToolResultLooksFailed(commandResult) || !remoteWriteFileResultHasOK(commandResult) {
		return fmt.Sprintf("写入失败: %s", commandResult)
	}
	createdText := "created=false"
	if remoteWriteFileResultCreated(commandResult) {
		createdText = "created=true"
	}
	if chunked {
		return fmt.Sprintf("已写入 %s (%d bytes, chunked, %s)", path, contentLen, createdText)
	}
	return fmt.Sprintf("已写入 %s (%d bytes, %s)", path, contentLen, createdText)
}

func remoteWriteFileResultCreated(commandResult string) bool {
	for _, line := range strings.Split(commandResult, "\n") {
		if strings.TrimSpace(strings.ToLower(line)) == "ok created=true" {
			return true
		}
	}
	return false
}

func remoteWriteFileResultHasOK(commandResult string) bool {
	for _, line := range strings.Split(commandResult, "\n") {
		line = strings.TrimSpace(line)
		if line == "OK" || line == "OK created=true" || line == "OK created=false" {
			return true
		}
	}
	return false
}

func remoteWriteFilePythonCommand(path, content string) string {
	pathB64 := base64EncodeString(path)
	contentB64 := base64EncodeString(content)
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"created = not p.exists()",
		"p.parent.mkdir(parents=True, exist_ok=True)",
		"p.write_bytes(base64.b64decode('%s'))",
		"print('OK created=' + str(created).lower())",
	}, "\n"), pathB64, contentB64)
	return remotePythonCommand(script)
}

// sshWriteFileLarge handles files >32KB by writing in base64 chunks.
func (c *remoteCodingCallbacks) sshWriteFileLarge(path, content string) string {
	// Write full base64 to a temp file, then decode it.
	b64 := base64EncodeString(content)
	tmpPath := "/tmp/maclaw_write_" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Write base64 data in chunks of 48KB using append mode.
	chunkSize := 48 * 1024
	for i := 0; i < len(b64); i += chunkSize {
		end := i + chunkSize
		if end > len(b64) {
			end = len(b64)
		}
		chunk := b64[i:end]
		cmd := remoteWriteFileLargeChunkCommand(tmpPath, chunk, i > 0)
		result := c.execSSH(cmd, 10)
		if remoteCodingToolResultLooksFailed(result) {
			return fmt.Sprintf("写入失败（分块传输）: %s", result)
		}
	}

	// Decode and move to target path.
	decodeCmd := remoteWriteFileLargeDecodeCommand(path, tmpPath)

	result := c.execSSH(decodeCmd, 15)
	return remoteWriteFileResult(path, len(content), result, true)
}

func remoteWriteFileLargeChunkCommand(tmpPath, chunk string, appendMode bool) string {
	op := ">"
	if appendMode {
		op = ">>"
	}
	return fmt.Sprintf("printf %%s %s %s %s", remoteShellQuote(chunk), op, remoteShellQuote(tmpPath))
}

func remoteWriteFileLargeDecodeCommand(path, tmpPath string) string {
	pathB64 := base64EncodeString(path)
	tmpPathB64 := base64EncodeString(tmpPath)
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"created = not p.exists()",
		"p.parent.mkdir(parents=True, exist_ok=True)",
		"tmp = base64.b64decode('%s').decode('utf-8')",
		"data = base64.b64decode(open(tmp).read())",
		"p.write_bytes(data)",
		"print('OK created=' + str(created).lower())",
	}, "\n"), pathB64, tmpPathB64)
	return fmt.Sprintf("%s && rm -f %s", remotePythonCommand(script), remoteShellQuote(tmpPath))
}

func (c *remoteCodingCallbacks) sshEditFile(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	oldStr, hasOldStr := remoteArgRawStr(args, "old_str")
	newStr, hasNewStr := remoteArgRawStr(args, "new_str")
	if path == "" || !hasOldStr || oldStr == "" || !hasNewStr {
		return "错误: 需要 path、old_str 和 new_str 参数"
	}
	path = c.resolvePath(path)
	if msg := c.requireRemoteProjectScope("ssh_edit_file", path); msg != "" {
		return msg
	}

	// Capture the existing remote content before mutation so the preview can show a diff.
	original, originalAvailable, originalTruncated := c.readRemotePreviewContent(path)

	// Use base64 to safely transfer old/new strings without heredoc terminator conflicts.
	pyScript := remoteEditFilePythonCommand(path, oldStr, newStr)

	result := c.execSSH(pyScript, 15)
	formatted := remoteEditFileResult(path, result)
	if remoteCodingToolOutcome(formatted) == "success" {
		c.trackRemoteFileChanged(path, false)
		if updated, ok, truncated := c.readRemotePreviewContent(path); ok {
			c.emitRemoteCodePreview(path, updated, original, "modify", originalTruncated || truncated, !originalAvailable)
		}
	}
	return formatted
}

// readRemotePreviewContent retrieves a whole remote text file for the local preview.
// It deliberately uses the existing SSH read command rather than the desktop filesystem.
func (c *remoteCodingCallbacks) readRemotePreviewContent(path string) (string, bool, bool) {
	// Keep preview reads under the SSH transport's output cap. A preview that
	// cannot be transferred intact is omitted rather than showing a misleading
	// middle-truncated source file.
	content := c.execSSH(remoteReadFileRangePythonCommand(path, 0, 100), 10)
	if remoteCodingToolOutcome(content) != "success" || !remoteReadFileResultHasUsefulEvidence(content) {
		return "", false, false
	}
	if remotePreviewOutputIsTransportTruncated(content) {
		return "", false, false
	}
	if len(content) > maxCodeFileSize || !isCodePreviewTextContent([]byte(content)) {
		return "", false, false
	}
	return extractRemoteReadPreviewContent(content), true, remotePreviewOutputIsTruncated(content)
}

func remotePreviewOutputIsTruncated(result string) bool {
	return remotePreviewOutputIsTransportTruncated(result) || strings.Contains(result, "[remote read_file truncated:")
}

func remotePreviewOutputIsTransportTruncated(result string) bool {
	return strings.Contains(result, "... (truncated) ...")
}

// extractRemoteReadPreviewContent removes the SSH command envelope and the
// line-number prefixes deliberately returned to the agent by ssh_read_file.
func extractRemoteReadPreviewContent(result string) string {
	lines := strings.Split(result, "\n")
	content := make([]string, 0, len(lines))
	for _, line := range lines {
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		if _, err := strconv.Atoi(line[:tab]); err != nil {
			continue
		}
		content = append(content, line[tab+1:])
	}
	return strings.Join(content, "\n")
}

// emitRemoteCodePreview bridges SSH tool output to the existing source preview.
// Remote paths are intentionally not used for tab routing: they do not exist locally.
func (c *remoteCodingCallbacks) emitRemoteCodePreview(path, content, original, opType string, previewTruncated, originalMissing bool) {
	if c == nil || c.agent == nil || !c.agent.sourcePreviewEnabled || c.agent.handler == nil || c.agent.handler.app == nil || c.agent.handler.app.codeEventEmitter == nil {
		return
	}
	log.Printf("[remote-source-preview] emit file session=%q path=%q op=%s content_len=%d truncated=%v original_missing=%v", c.agent.sourcePreviewSessionID, path, opType, len(content), previewTruncated, originalMissing)
	c.agent.handler.app.codeEventEmitter.EmitCodeFileEvent(buildRemoteCodingCodeFileEvent(
		c.agent.sourcePreviewSessionID,
		path,
		content,
		original,
		opType,
		previewTruncated,
		originalMissing,
	))
}

// buildRemoteCodingCodeFileEvent builds a code:file_update payload for remote coding.
//
// ForceOpen is always set for the remote coding workflow (sourcePreview is opt-in
// there). Local CodingSubAgent only force-opens writes; remote also force-opens
// reads so the right-hand pane actually appears — otherwise a leftover active
// session blocks non-force updates, and auto_open alone never takes over.
func buildRemoteCodingCodeFileEvent(sessionID, path, content, original, opType string, previewTruncated, originalMissing bool) CodeFileEvent {
	op := strings.ToLower(strings.TrimSpace(opType))
	if op == "" {
		op = "modify"
	}
	return CodeFileEvent{
		SessionID:        sessionID,
		FilePath:         path,
		FileName:         pathpkg.Base(path),
		AbsPath:          path,
		Content:          content,
		Original:         original,
		OpType:           op,
		Language:         detectLanguageFromExt(path),
		ForceOpen:        true,
		AutoOpenPreview:  true,
		PreviewTruncated: previewTruncated,
		OriginalMissing:  originalMissing,
	}
}

func remoteEditFileResult(path string, commandResult string) string {
	if remoteCodingToolResultLooksFailed(commandResult) || !remoteEditFileResultHasOK(commandResult) {
		return fmt.Sprintf("编辑失败: %s", commandResult)
	}
	return fmt.Sprintf("已编辑 %s", path)
}

func remoteEditFileResultHasOK(commandResult string) bool {
	for _, line := range strings.Split(commandResult, "\n") {
		if strings.TrimSpace(line) == "OK: replaced 1 occurrence" {
			return true
		}
	}
	return false
}

func remoteEditFilePythonCommand(path, oldStr, newStr string) string {
	pathB64 := base64EncodeString(path)
	oldB64 := base64EncodeString(oldStr)
	newB64 := base64EncodeString(newStr)
	script := fmt.Sprintf(strings.Join([]string{
		"import pathlib, base64, sys",
		"p = pathlib.Path(base64.b64decode('%s').decode('utf-8'))",
		"if not p.exists():",
		"    print('ERROR: file not found: ' + str(p))",
		"    sys.exit(0)",
		"text = p.read_text(encoding='utf-8')",
		"old = base64.b64decode('%s').decode('utf-8')",
		"new = base64.b64decode('%s').decode('utf-8')",
		"if old not in text:",
		"    print('ERROR: old_str not found in file')",
		"elif text.count(old) > 1:",
		"    print('ERROR: old_str matches ' + str(text.count(old)) + ' locations (must be unique)')",
		"else:",
		"    p.write_text(text.replace(old, new, 1), encoding='utf-8')",
		"    print('OK: replaced 1 occurrence')",
	}, "\n"), pathB64, oldB64, newB64)
	return remotePythonCommand(script)
}

func (c *remoteCodingCallbacks) sshBash(args map[string]interface{}) string {
	command := remoteArgStr(args, "command")
	if command == "" {
		return "错误: 需要 command 参数"
	}
	workDir := remoteArgStr(args, "working_dir")
	if workDir == "" {
		workDir = c.defaultRemoteWorkingDir()
	} else {
		workDir = c.resolvePath(workDir)
	}
	if c != nil && c.agent != nil && c.agent.readOnlyInquiry {
		if msg := rejectCodingInquiryShellCommand(command); msg != "" {
			c.trackRemoteCommand(command, workDir, msg, false)
			return msg
		}
	}
	if c != nil && c.agent != nil && c.agent.operationalRequest {
		if msg := rejectCodingOperationalShellCommand(command); msg != "" {
			c.trackRemoteCommand(command, workDir, msg, false)
			return msg
		}
	}
	// Hard block silenced git self-checks (no high-risk approval bypass).
	if msg := rejectSilencedGitSelfCheckCommand(command); msg != "" {
		msg = c.userFacingSafetyActorMessage(msg)
		c.trackRemoteCommand(command, workDir, msg, false)
		return msg
	}
	if msg := rejectDisallowedCodingBashCommand(command); msg != "" {
		msg = c.userFacingSafetyActorMessage(msg)
		if c == nil || c.agent == nil {
			return msg
		}
		if hasOnlyDirectoryCreationShellMutation(strings.ToLower(strings.Join(strings.Fields(command), " "))) {
			if approvalMsg := c.requireRemoteShellDirectoryWriteApproval(command, workDir, msg); approvalMsg != "" {
				return approvalMsg
			}
		} else if approvalMsg := c.agent.highRiskApproval.check(command, workDir, msg); approvalMsg != "" {
			return approvalMsg
		}
	}

	fullCmd := fmt.Sprintf("cd %s && %s", remoteShellQuote(workDir), command)

	// Long commands → use SSH background task
	if isLongRemoteCommand(command) {
		log.Printf("[remote-subagent] long command detected, using background task: %.80s", command)
		result := c.execSSHBackground(fullCmd)
		if remoteCodingToolOutcome(result) != "success" {
			c.trackRemoteCommand(command, workDir, result, false)
		}
		return result
	}

	result := c.execSSH(remoteBashCommandWithExitMarker(workDir, command), 60)
	c.trackRemoteCommand(command, workDir, result, remoteCodingToolOutcome(result) == "success")
	return result
}

func (c *remoteCodingCallbacks) userFacingSafetyActorMessage(message string) string {
	return strings.Replace(message, "编码 SubAgent", c.userFacingRemoteActor(), 1)
}

func (c *remoteCodingCallbacks) userFacingRemoteActor() string {
	if c != nil && c.agent != nil && c.agent.maintenance {
		return "远程维护"
	}
	return "远程编码 SubAgent"
}

func (c *remoteCodingCallbacks) userFacingRemoteAgentMessage(message string) string {
	return strings.NewReplacer(
		"RemoteCodingSubAgent", c.userFacingRemoteActor(),
		"remoteCodingSubAgent", c.userFacingRemoteActor(),
		"Remote coding SubAgent", c.userFacingRemoteActor(),
		"remote coding subagent", c.userFacingRemoteActor(),
	).Replace(message)
}

func remoteBashCommandWithExitMarker(workDir, command string) string {
	return fmt.Sprintf(strings.Join([]string{
		"cd %s",
		"__maclaw_cd_status=$?",
		"if [ $__maclaw_cd_status -ne 0 ]; then",
		"  printf '\\nEXIT: %%s\\n' \"$__maclaw_cd_status\"",
		"else",
		"  sh -lc %s",
		"  __maclaw_cmd_status=$?",
		"  printf '\\nEXIT: %%s\\n' \"$__maclaw_cmd_status\"",
		"fi",
	}, "\n"), remoteShellQuote(workDir), remoteShellQuote(command))
}

func (c *remoteCodingCallbacks) sshListDir(args map[string]interface{}) string {
	path := remoteArgStr(args, "path")
	if path == "" {
		path = c.defaultRemoteWorkingDir()
	} else {
		path = c.resolvePath(path)
	}
	if msg := c.requireRemoteProjectScope("ssh_list_dir", path); msg != "" {
		return msg
	}
	result := c.execSSH(fmt.Sprintf("ls -la %s", remoteShellQuote(path)), 10)
	c.trackRemoteSearch("ssh_list_dir", "ls -la "+path, path, result, remoteCodingToolOutcome(result) == "success")
	return result
}

func (c *remoteCodingCallbacks) defaultRemoteWorkingDir() string {
	if c == nil || c.agent == nil {
		return "."
	}
	if projectDir := strings.TrimSpace(c.agent.projectDir); projectDir != "" {
		return projectDir
	}
	if workDir := strings.TrimSpace(c.agent.workDir); workDir != "" {
		return workDir
	}
	return "."
}

func (c *remoteCodingCallbacks) sshCheckTask(args map[string]interface{}) string {
	taskID := remoteArgStr(args, "task_id")
	if taskID == "" {
		return "错误: 需要 task_id 参数"
	}
	if c == nil || c.agent == nil || c.agent.handler == nil {
		return c.userFacingRemoteAgentMessage("remote coding subagent: handler unavailable")
	}
	// Delegate to the main SSH tool's check_task action.
	h := c.agent.handler
	result := h.toolSSH(map[string]interface{}{
		"action":     "check_task",
		"session_id": c.agent.sessionID,
		"task_id":    taskID,
		"tail_lines": float64(remoteArgInt(args, 50, 1, 1000, "tail_lines", "tail", "lines", "limit")),
	})
	c.trackRemoteTaskCheckResult(result)
	return result
}

// --- SSH Execution Helpers ---

func (c *remoteCodingCallbacks) execSSH(command string, waitSec int) string {
	if c == nil || c.agent == nil || c.agent.handler == nil {
		return c.userFacingRemoteAgentMessage("remote coding subagent: handler unavailable")
	}
	h := c.agent.handler
	return h.sshExec(map[string]interface{}{
		"session_id":   c.agent.sessionID,
		"command":      command,
		"wait_seconds": float64(waitSec),
	})
}

func (c *remoteCodingCallbacks) execSSHBackground(command string) string {
	if c == nil || c.agent == nil || c.agent.handler == nil {
		return c.userFacingRemoteAgentMessage("remote coding subagent: handler unavailable")
	}
	h := c.agent.handler
	return h.toolSSH(map[string]interface{}{
		"action":     "exec_background",
		"session_id": c.agent.sessionID,
		"command":    command,
	})
}

// resolvePath makes relative paths relative to the project directory.
func (c *remoteCodingCallbacks) resolvePath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	if c == nil || c.agent == nil || strings.TrimSpace(c.agent.projectDir) == "" {
		return path
	}
	return strings.TrimRight(c.agent.projectDir, "/") + "/" + path
}

func (c *remoteCodingCallbacks) requireRemoteProjectScope(toolName, targetPath string) string {
	projectPath := c.defaultRemoteWorkingDir()
	if c != nil && c.agent != nil && strings.TrimSpace(c.agent.projectDir) != "" {
		projectPath = c.agent.projectDir
	}
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" || projectPath == "." {
		return ""
	}
	if remotePathWithinDir(targetPath, projectPath) {
		return ""
	}
	msg := c.userFacingRemoteScopeRejection(toolName, targetPath, projectPath)
	if c != nil && c.agent != nil && c.agent.highRiskApproval != nil {
		return c.agent.highRiskApproval.checkRemotePath(toolName, targetPath, projectPath, remotePathAccessKind, msg, true)
	}
	return msg
}

func (c *remoteCodingCallbacks) requireRemoteShellDirectoryWriteApproval(command, workDir, fallback string) string {
	projectPath := ""
	if c != nil && c.agent != nil {
		projectPath = strings.TrimSpace(c.agent.projectDir)
	}
	if projectPath == "" {
		projectPath = c.defaultRemoteWorkingDir()
	}
	if strings.TrimSpace(projectPath) == "" || projectPath == "." {
		return fallback
	}
	if !remotePathWithinDir(workDir, projectPath) {
		if msg := c.requireRemoteProjectScope(remoteSSHBashToolName, workDir); msg != "" {
			return msg
		}
	}
	targets, ok := remoteShellDirectoryCreationTargets(command, workDir)
	if !ok {
		return fallback
	}
	for _, target := range targets {
		if remotePathWithinDir(target, projectPath) {
			continue
		}
		if c != nil && c.agent != nil && c.agent.highRiskApproval != nil {
			msg := c.userFacingRemoteScopeRejection(remoteSSHBashToolName, target, projectPath)
			if approvalMsg := c.agent.highRiskApproval.checkRemotePath(remoteSSHBashToolName, target, projectPath, remotePathAccessKind, msg, true); approvalMsg != "" {
				return approvalMsg
			}
			continue
		}
		return c.userFacingRemoteScopeRejection(remoteSSHBashToolName, target, projectPath)
	}
	if c == nil || c.agent == nil || c.agent.highRiskApproval == nil {
		return fallback
	}
	msg := c.userFacingRemoteAgentMessage(fmt.Sprintf("remote coding subagent requests directory creation via ssh_bash: %s", strings.Join(targets, ", ")))
	return c.agent.highRiskApproval.checkRemotePath(remoteSSHBashToolName, workDir, projectPath, remoteDirectoryWriteKind, msg, true)
}

func remoteShellDirectoryCreationTargets(command, workDir string) ([]string, bool) {
	fields := shellCommandFields(command)
	var targets []string
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
			switch commandNameBase(normalizeShellExecutableToken(token)) {
			case "mkdir", "md":
				segmentArgs := commandSegmentFields(fields[i+1:])
				for j := 0; j < len(segmentArgs); j++ {
					arg := normalizeShellCommandToken(segmentArgs[j])
					if arg == "" {
						continue
					}
					if arg == "--" {
						for _, literal := range segmentArgs[j+1:] {
							literal = normalizeShellCommandToken(literal)
							if literal == "" || shellDirectoryCreationTargetLooksDynamic(literal) {
								return nil, false
							}
							targets = append(targets, remoteResolvePath(workDir, literal))
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
						return nil, false
					}
					targets = append(targets, remoteResolvePath(workDir, arg))
				}
			case "echo", "write-output", "printf":
			default:
				return nil, false
			}
		}
		commandPosition = false
	}
	return targets, len(targets) > 0
}

func remoteResolvePath(workDir, p string) string {
	if strings.HasPrefix(p, "/") {
		return remoteCleanPath(p)
	}
	return remoteCleanPath(strings.TrimRight(workDir, "/") + "/" + p)
}

func remoteCleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "."
	}
	return pathpkg.Clean(p)
}

func remotePathDir(p string) string {
	return pathpkg.Dir(remoteCleanPath(p))
}

func remotePathWithinDir(p, dir string) bool {
	p = remoteCleanPath(p)
	dir = strings.TrimRight(remoteCleanPath(dir), "/")
	return p == dir || strings.HasPrefix(p, dir+"/")
}

func remoteScopeRejection(toolName, path, projectPath string) string {
	switch toolName {
	case "ssh_read_file", "ssh_list_dir":
		return fmt.Sprintf("refusing to read remote path outside the project: %s. Remote coding SubAgent may only use %s inside %s.", path, toolName, projectPath)
	case remoteSSHBashToolName:
		return fmt.Sprintf("refusing remote directory access outside the project: %s. Remote coding SubAgent ssh_bash working_dir and directory targets must stay inside %s.", path, projectPath)
	default:
		return fmt.Sprintf("refusing to modify remote path outside the project: %s. Remote coding SubAgent may only modify files inside %s.", path, projectPath)
	}
}

func (c *remoteCodingCallbacks) userFacingRemoteScopeRejection(toolName, path, projectPath string) string {
	return c.userFacingRemoteAgentMessage(remoteScopeRejection(toolName, path, projectPath))
}

// --- System Prompt ---

// buildRemoteInspectionRoleSystemPrompt is for nested explorer/reviewer agents:
// read/diagnose only — no write-oriented quality workflow.
func buildRemoteInspectionRoleSystemPrompt(projectDir, workDir string, role codingSubAgentRole, taskContext string) string {
	var sb strings.Builder
	sb.WriteString("# Remote Inspection SubAgent\n\n")
	if role == codingRoleReviewer {
		sb.WriteString("你是远程审查/验证子代理：只读探查 + shell 检查，禁止写文件。\n\n")
	} else {
		sb.WriteString("你是远程探索子代理：只读探查远程代码与环境，禁止写文件。\n\n")
	}
	sb.WriteString(fmt.Sprintf("## 环境信息\n- 远程项目目录: %s\n- 工作目录: %s\n\n", projectDir, workDir))
	sb.WriteString(`## 可用工具
- ssh_read_file / ssh_list_dir：读取与列目录
- ssh_bash：探索/诊断/验证命令（不要用 shell 改写文件或 Git 工作区）
`)
	if role == codingRoleReviewer {
		sb.WriteString("- ssh_check_task：跟进后台长任务\n")
	}
	sb.WriteString(`
## 工作规范
1. 先定位相关路径与符号，再深入阅读关键文件
1a. 故障定位优先 code_navigation；输出必须包含症状、Top 候选、根因文件/符号、因果路径、复现证据、反证/排除假设与建议的 focused test，并用 report_localization 提交结构化结果
1b. 遇到陌生概念/精确报错、第三方依赖/API/协议、版本或兼容性事实，必须 web_search 搜索精确错误与组件版本，优先核对官方文档并记录来源；若只是“无结果”，换一条保留组件/版本/错误码的查询再试一次，provider/网络/配置明确失败则不要重复空转；纯仓内问题也要明确说明为何无需联网
2. 用 ssh_bash 做只读探查（find/rg/ls/git status/diff/test 等）
3. git status/diff/log 自检不要加 2>/dev/null 或 >/dev/null；若目录不是 Git 仓库，直接在结论中说明即可，不要用重定向掩盖 fatal 信息
4. 完成后给出结构化发现：关键路径、结论、风险/建议
5. 禁止写文件或改仓库状态；只输出发现与建议
`)
	if strings.TrimSpace(taskContext) != "" {
		sb.WriteString("\n## 任务上下文\n\n")
		sb.WriteString(taskContext)
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildRemoteCodingSystemPrompt(projectDir, workDir, taskContext string) string {
	var sb strings.Builder
	sb.WriteString("# Remote Coding SubAgent\n\n")
	sb.WriteString("你是一个在远程服务器上执行代码修改和实验的编程 Agent。\n\n")
	sb.WriteString(fmt.Sprintf("## 环境信息\n- 远程项目目录: %s\n- 工作目录: %s\n\n", projectDir, workDir))
	sb.WriteString(`## 可用工具

- ssh_read_file(path, offset?, limit?): 读取远程文件内容；默认读取前 200 行，大文件用 offset/limit 分片读取；也接受 start/start_line/startLine 和 lines/num_lines/line_count
- ssh_write_file(path, content): 写入/创建远程文件（自动创建父目录）
- ssh_edit_file(path, old_str, new_str): 精确替换远程文件中的文本（old_str 必须唯一匹配）
- ssh_bash(command, working_dir?): 在远程服务器执行命令（长时间命令自动转后台任务，返回 task_id）
- ssh_check_task(task_id, tail_lines?): 查询后台任务状态、exit_code 和日志尾部
- ssh_list_dir(path?): 列出远程目录内容

参数兼容: 路径可用 file/file_path/filename/target_path 代替 path；ssh_edit_file 也接受 old_string/old_content/find/search -> old_str 和 new_string/new_content/replace/replacement -> new_str；ssh_bash 接受 cwd/work_dir -> working_dir；ssh_check_task 接受 id/task -> task_id。

## 工作规范

0. For a toolchain inventory, never put an optional tool in an "&&" chain with required tools: one missing optional tool (such as clang++) short-circuits the entire check. Check required tools first, then probe optional tools separately with a non-blocking command (for example, "command -v clang++ || true") and report the result.

0a. Do not use "tree" as a verification dependency: it is often absent on minimal servers. For a portable project-structure display use "find . -maxdepth 3 -print | sort"; run build/syntax checks as separate commands so a display-tool absence cannot block them.

以下第 4、5、6 步是完成任务的质量门禁；只要修改或创建了文件，就必须执行并在最终回复中报告。唯一例外是第 5 步：当前多步骤计划明确把编译/build/test 交给后续独立步骤时，不要提前构建，但第 4、6 步仍为必做项。

1. 修改文件前先 ssh_read_file 确认当前内容
1a. 修复 bug 时先提取错误文本/堆栈/入口与期望-实际差异；优先调用 code_navigation（远端 .codegraph + codegraph，自动回退 rg/grep）定位定义、引用、调用者/被调用者。形成候选与反证，复现或说明无法复现的原因。对陌生/版本敏感/第三方事实必须调用本地 web_search（精确错误 + 组件/版本）并优先阅读官方来源；若搜索只是“无结果”，换一条保留组件/版本/错误码的查询再试一次，provider/网络/配置明确失败则不要重复空转；纯仓内逻辑则记录无需搜索的理由。随后调用 report_localization；根因文件与证据不匹配时禁止修改。
2. 优先做最小、聚焦的修改；不要顺手重构无关代码
3. 使用 ssh_edit_file 做精确修改（小改动）或 ssh_write_file 重写文件（大改动）
4. 修改后再次 ssh_read_file 读取关键片段，确认远程文件确实变成预期内容
	5. 修改后用 ssh_bash 运行匹配任务的验证命令（如 "g++ -o hello hello.cpp"、"python3 -m py_compile file.py"、pytest/go test/npm test 等）。例外仅适用于：当前是多步骤计划的实现步骤，且计划后续明确有独立的编译/build/test 步骤时，不要提前执行完整构建；完成本步骤的修改后回读确认，后续步骤负责可执行验证。没有这一明确的后续步骤时，必须在本步骤验证。
6. 修改后运行并查看只读自检命令（优先 git status --short 与 git diff --stat；不要附带 git log，除非任务明确要求）
7. git 自检命令不要使用 2>/dev/null、>/dev/null、&>/dev/null 等重定向；保留 fatal 原文。若不是 Git 仓库，在最终回复写明“非 Git 仓库，跳过 diff 自检”即可，不要反复重试
8. ssh_bash 只用于探索、诊断、格式化和验证；文件改写必须使用 ssh_edit_file/ssh_write_file
9. 路径可以是相对路径（相对于项目目录）或绝对路径
10. ssh_read_file 默认只返回前 200 行；继续读取时用返回提示里的 offset 分片查看
11. 长时间训练命令会自动作为后台任务运行，返回 task_id；必须用 ssh_check_task 跟进直到得到明确状态/exit_code
12. 如确实需要运行被安全策略拦截的 ssh_bash 命令，等待用户确认；用户可选择本次放行或本任务放行
13. 最终回复必须说明：修改/创建的文件、实际运行的验证命令及结果、diff/status 自检结果、剩余风险或未验证项

## 严禁行为
- 不要删除项目根目录或关键系统文件
- 不要修改 /etc、/usr 等系统目录
- 不要在未读取文件的情况下盲目覆盖
- 不要用 ssh_bash 执行 git reset/checkout/restore/switch/merge/rebase/stash/add/commit/apply/clean -f 等会改写工作区或历史的命令
- 不要用 ssh_bash 执行 rm -r/rm -rf、shell 重定向写文件、sed -i、perl -pi、touch/mkdir/cp/mv、脚本内写文件等绕过审计的文件改写
- 不要对 git status/diff/log 自检命令做 2>/dev/null 或 >/dev/null（会掩盖“不是 Git 仓库”等关键信息并触发质量门误判）
`)
	if taskContext != "" {
		sb.WriteString("\n## 任务上下文\n\n")
		sb.WriteString(taskContext)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildRemoteInquirySystemPrompt is the remote counterpart of the local
// read-only repository-question path.  It keeps SSH inspection useful without
// making a harmless question pay the cost (or risk) of an implementation flow.
func buildRemoteInquirySystemPrompt(projectDir, workDir string) string {
	return fmt.Sprintf(`# Remote repository inquiry

Answer the user's repository/code question using read-only evidence from the remote workspace.

## Environment
- Remote project directory: %s
- Working directory: %s

## Rules
1. Use only ssh_list_dir, ssh_read_file, code_navigation, available knowledge search, or ssh_bash for targeted read-only inspection.
2. If the project contains .codegraph/, prefer code_navigation / CodeGraph before broad shell searching.
3. Do not modify files, create files, run builds/tests, create a plan, or present TDD/task-number progress.
4. Lead with a concise answer. Then list the most relevant inspected paths as evidence. State clearly that no files were changed.
`, projectDir, workDir)
}

// buildRemoteOperationalSystemPrompt is the remote counterpart of the local
// run/build/demo path.  It preserves useful SSH inspection and execution while
// making the no-code-change boundary explicit to the model and tool policy.
func buildRemoteOperationalSystemPrompt(projectDir, workDir string) string {
	return fmt.Sprintf(`# Remote operational task

Execute the user's run/build/demo request against the existing remote project. This is not an implementation task.

Remote project directory: %s
Working directory: %s

Rules:
1. Use ssh_list_dir, ssh_read_file, code_navigation, or targeted ssh_bash only as needed to locate the existing executable, script, or build command.
2. Actually run the launch/build command with ssh_bash; do not merely say it was run.
3. Do not edit source files or dependency configuration, generate a plan, create tests, or start a TDD/task-number workflow. Normal build output created by the launch/build command is allowed.
4. For a long-running process, use an appropriate timeout/background task and report whether it started successfully.
5. Lead with the result, then report the command and exit code/key output. State that no source changes were requested; mention any generated build output when observed.
`, projectDir, workDir)
}

// summarizeRemoteOperationalQuality mirrors the local operational acceptance
// gate: locating files alone is insufficient, while one successful run/build
// command is enough and does not require implementation verification or diff.
func summarizeRemoteOperationalQuality(commands []CodingSubAgentCommandResult, toolCalls int) (codingSubAgentQualityStatus, string, int) {
	successfulLaunch, failedLaunch, otherBashSuccess := 0, 0, 0
	for _, cmd := range commands {
		switch classifyOperationalShellCommand(cmd.Command) {
		case operationalShellLaunchBuild:
			if cmd.Succeeded {
				successfulLaunch++
			} else {
				failedLaunch++
			}
		case operationalShellInspection:
			// Inspection locates the artifact but is not execution evidence.
		default:
			if cmd.Succeeded {
				otherBashSuccess++
			}
		}
	}
	if successfulLaunch > 0 {
		return codingSubAgentQualityPassed, "operational run: launch/build command evidence present", 0
	}
	if toolCalls == 0 {
		return codingSubAgentQualityFailed, "operational task ran no tools (need ssh_bash to launch/build)", 1
	}
	if failedLaunch > 0 {
		return codingSubAgentQualityFailed, "operational task: launch/build command(s) failed", 1
	}
	if otherBashSuccess > 0 {
		return codingSubAgentQualityFailed, "operational task: ran shell commands but none looked like launch/build", 1
	}
	return codingSubAgentQualityFailed, "operational task: no launch/build command executed", 1
}

// --- Tool Definitions ---

func remoteCodingToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		buildRemoteToolDef("ssh_read_file", "读取远程服务器上的文件内容",
			map[string]interface{}{
				"path":   map[string]interface{}{"type": "string", "description": "文件路径（相对于项目目录或绝对路径；也接受 file/file_path/filename/target_path）"},
				"offset": map[string]interface{}{"type": "number", "description": "可选，1-based 起始行；也接受 start/start_line/startLine"},
				"limit":  map[string]interface{}{"type": "number", "description": "可选，最多读取的行数；默认 200，也接受 lines/num_lines/line_count，最大 2000"},
			}, []string{"path"}),
		buildRemoteToolDef("ssh_write_file", "写入内容到远程文件（自动创建父目录）",
			map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "文件路径（也接受 file/file_path/filename/target_path）"},
				"content": map[string]interface{}{"type": "string", "description": "文件内容"},
			}, []string{"path", "content"}),
		buildRemoteToolDef("ssh_edit_file", "精确替换远程文件中的文本（old_str 必须在文件中唯一匹配；也接受 old_string/old_content/find/search 和 new_string/new_content/replace/replacement）",
			map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "文件路径（也接受 file/file_path/filename/target_path）"},
				"old_str": map[string]interface{}{"type": "string", "description": "要被替换的原始文本（也接受 old_string/old_content/find/search）"},
				"new_str": map[string]interface{}{"type": "string", "description": "替换后的新文本（也接受 new_string/new_content/replace/replacement）"},
			}, []string{"path", "old_str", "new_str"}),
		buildRemoteToolDef("ssh_bash", "在远程服务器上执行探索、诊断、格式化或验证命令（长时间命令自动转后台任务；拒绝 git 工作区改写、递归删除和通过 shell 直接改写文件）",
			map[string]interface{}{
				"command":     map[string]interface{}{"type": "string", "description": "要执行的命令；用于探索/诊断/格式化/验证，不要用它改写文件或 Git 工作区"},
				"working_dir": map[string]interface{}{"type": "string", "description": "工作目录（默认项目目录；也接受 cwd/work_dir）"},
			}, []string{"command"}),
		buildRemoteToolDef("ssh_list_dir", "列出远程目录内容",
			map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "目录路径（默认项目目录；也接受 dir/directory/root/file/file_path/filename/target_path）"},
			}, nil),
		buildRemoteToolDef("ssh_check_task", "查询后台任务状态（训练/下载等长时间任务）。返回运行状态和日志尾部",
			map[string]interface{}{
				"task_id":    map[string]interface{}{"type": "string", "description": "后台任务 ID（由 ssh_bash 长命令自动返回；也接受 id/task）"},
				"tail_lines": map[string]interface{}{"type": "number", "description": "可选，返回的日志尾部行数；默认 50，也接受 tail/lines/limit，范围 1-1000"},
			}, []string{"task_id"}),
	}
}

func buildRemoteToolDef(name, description string, properties map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  params,
		},
	}
}

// --- Utility ---

func remoteArgStr(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func remoteArgRawStr(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key].(string)
	return v, ok
}

func remoteArgInt(args map[string]interface{}, defaultValue, minValue, maxValue int, keys ...string) int {
	value := defaultValue
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case int:
			value = v
		case int64:
			value = int(v)
		case float64:
			value = int(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				value = int(n)
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				value = n
			}
		}
		break
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func applyRemoteCodingSubAgentToolArgumentAliases(name string, args map[string]interface{}) bool {
	if len(args) == 0 {
		return false
	}
	changed := false
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_read_file", "ssh_write_file":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
	case "ssh_edit_file":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "old_string", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "old_content", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "find", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "search", "old_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "new_string", "new_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "new_content", "new_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "replace", "new_str") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "replacement", "new_str") || changed
	case "ssh_bash":
		changed = applyCodingSubAgentToolArgumentAlias(args, "work_dir", "working_dir") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "cwd", "working_dir") || changed
	case "ssh_list_dir":
		changed = applyCodingSubAgentPathArgumentAliases(args) || changed
		changed = applyCodingSubAgentDirectoryArgumentAliases(args) || changed
	case "ssh_check_task":
		changed = applyCodingSubAgentToolArgumentAlias(args, "task", "task_id") || changed
		changed = applyCodingSubAgentToolArgumentAlias(args, "id", "task_id") || changed
	case "coding_knowledge_search", "knowledge_search":
		changed = applyCodingSubAgentQueryArgumentAliases(args) || changed
	}
	return changed
}

func remoteShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func base64EncodeString(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func isLongRemoteCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" || remoteCommandIsPythonOneLiner(lower) {
		return false
	}
	for _, pattern := range []string{
		"nohup ",
		"screen ",
		"tmux ",
		"pip install",
		"conda install",
		"apt install",
		"apt-get install",
		"git clone ",
		"wget ",
		"curl -o",
		"curl -l",
		"docker build",
		"docker pull",
	} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	if strings.Contains(lower, "git pull") {
		return true
	}
	// Only treat make as long when the make *target* is long-running.
	// Do NOT match incidental paths like `mkdir -p build && make` via bare " build".
	if remoteCommandInvokesMake(lower) {
		return remoteCommandHasLongTrainingIntent(lower) || remoteMakeCommandHasLongTarget(lower)
	}
	if strings.Contains(lower, "cmake ") {
		return strings.Contains(lower, "--build") || strings.Contains(lower, " --install")
	}
	if (strings.Contains(lower, "python") || strings.Contains(lower, "bash ") || strings.Contains(lower, "sh ")) &&
		(strings.Contains(lower, ".py") || strings.Contains(lower, ".sh")) {
		return remoteCommandHasLongTrainingIntent(lower)
	}
	return remoteCommandHasLongTrainingIntent(lower) && remoteCommandLooksExplicitlyLongRunning(lower)
}

func remoteCommandIsPythonOneLiner(lower string) bool {
	return strings.Contains(lower, "python3 -c") ||
		strings.Contains(lower, "python -c") ||
		strings.Contains(lower, "python3 - <<") ||
		strings.Contains(lower, "python - <<")
}

func remoteCommandInvokesMake(lower string) bool {
	lower = strings.TrimSpace(lower)
	if lower == "" {
		return false
	}
	// Normalize common chain separators so "&&make" still tokenizes.
	normalized := lower
	for _, sep := range []string{"&&", ";", "|"} {
		normalized = strings.ReplaceAll(normalized, sep, " ")
	}
	for _, field := range strings.Fields(normalized) {
		base := commandNameBase(field)
		if base == "make" || base == "gmake" || base == "mingw32-make" {
			return true
		}
	}
	return false
}

// remoteMakeCommandHasLongTarget reports make invocations whose first positional
// target is a long-running one (build/install), ignoring flags and redirections.
// `mkdir -p build` does not invoke make, so it never matches.
func remoteMakeCommandHasLongTarget(lower string) bool {
	normalized := lower
	for _, sep := range []string{"&&", ";", "|"} {
		normalized = strings.ReplaceAll(normalized, sep, " ")
	}
	fields := strings.Fields(normalized)
	for i := 0; i < len(fields); i++ {
		base := commandNameBase(fields[i])
		if base != "make" && base != "gmake" && base != "mingw32-make" {
			continue
		}
		for j := i + 1; j < len(fields); j++ {
			tok := strings.TrimSpace(fields[j])
			if tok == "" || tok == "2>&1" || tok == "1>&2" {
				continue
			}
			if strings.HasPrefix(tok, "2>") || strings.HasPrefix(tok, "1>") ||
				strings.HasPrefix(tok, ">") || strings.HasPrefix(tok, "&>") {
				continue
			}
			if strings.HasPrefix(tok, "-") {
				// Common value-taking flags: -j 8, -C dir, -f file
				switch tok {
				case "-j", "-C", "-f", "--file", "--makefile", "--directory", "-I", "--include-dir":
					j++
				}
				continue
			}
			// First positional target only (switch-break would not exit this loop).
			return tok == "build" || tok == "install"
		}
	}
	return false
}

func remoteCommandLooksExplicitlyLongRunning(lower string) bool {
	return strings.Contains(lower, "train ") ||
		strings.Contains(lower, " train") ||
		strings.Contains(lower, "epoch") ||
		strings.Contains(lower, "--epochs") ||
		strings.Contains(lower, "--max-steps")
}

func remoteCommandHasLongTrainingIntent(lower string) bool {
	tokens := remoteCommandTokens(lower)
	for i, token := range tokens {
		if !remoteCommandTokenIsTrainingIntent(token) {
			continue
		}
		if i > 0 && remoteCommandTokenSuppressesTrainingIntent(tokens[i-1]) {
			continue
		}
		return true
	}
	return false
}

func remoteCommandTokenIsTrainingIntent(token string) bool {
	switch token {
	case "train", "training", "fit", "epoch", "epochs", "finetune", "finetuning":
		return true
	default:
		return false
	}
}

func remoteCommandTokenSuppressesTrainingIntent(token string) bool {
	switch token {
	case "check", "test", "tests", "validate", "validation", "lint", "verify":
		return true
	default:
		return false
	}
}

func remoteCommandTokens(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

// --- Knowledge Store Integration ---

// buildRemoteKnowledgePromptSections generates knowledge-related system prompt
// sections. Returns empty string if no relevant knowledge found.
func (c *remoteCodingCallbacks) buildRemoteKnowledgePromptSections() string {
	if c == nil || c.agent == nil {
		return ""
	}
	if c.ShouldStop() {
		return ""
	}

	var b strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	taskQuery := c.task
	if taskQuery == "" {
		return ""
	}

	// 1. Coding knowledge (experiences)
	if c.agent.codingKB != nil {
		pack, err := c.agent.codingKB.ContextPackForTask(ctx, knowledge.CodingContextPackOptions{
			Query:    taskQuery,
			Language: "python", // paper reproduction is predominantly Python
			MaxItems: 4,
			MaxChars: 1500,
		})
		if err == nil && len(pack.Items) > 0 {
			b.WriteString("\n## 相关编码经验（来自编程知识库）\n")
			b.WriteString("以下经验来自历史编码任务积累，供参考：\n")
			for _, item := range pack.Items {
				text := item.Text
				if len([]rune(text)) > 300 {
					text = string([]rune(text)[:300]) + "..."
				}
				b.WriteString(fmt.Sprintf("- **%s**: %s\n", item.Title, text))
			}
		}
	}

	// 2. General knowledge (project docs)
	if c.agent.generalKB != nil {
		searchOpts := knowledge.SearchOptions{
			Query: taskQuery,
			Limit: 10,
		}
		pack, err := c.agent.generalKB.ContextPack(ctx, knowledge.ContextPackOptions{
			SearchOptions: searchOpts,
			MaxItems:      3,
			MaxChars:      2000,
		})
		if err == nil && len(pack.Items) > 0 {
			b.WriteString("\n## 项目参考资料（来自通用知识库）\n")
			b.WriteString("以下是与当前任务相关的项目文档：\n")
			b.WriteString(knowledge.FormatContextPackForLLM(pack))
		}
	}

	return b.String()
}

// executeRemoteCodingKnowledgeSearch handles coding_knowledge_search tool call.
func (c *remoteCodingCallbacks) executeRemoteCodingKnowledgeSearch(argsJSON string) string {
	if c.agent.codingKB == nil {
		return "编程知识库未配置。暂无可用的编码经验。"
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}
	query, _ := args["query"].(string)
	if query == "" {
		return "Error: query parameter is required"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	experiences, err := c.agent.codingKB.SearchExperiences(ctx, knowledge.CodingSearchOptions{
		Query:    query,
		Language: "python",
		Status:   []string{knowledge.CodingStatusActive, knowledge.CodingStatusVerified},
		Limit:    5,
	})
	if err != nil {
		return fmt.Sprintf("编程知识库搜索失败: %v", err)
	}
	if len(experiences) == 0 {
		return fmt.Sprintf("未找到与 %q 相关的编码经验。", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条相关编码经验：\n\n", len(experiences)))
	for i, exp := range experiences {
		b.WriteString(fmt.Sprintf("%d. **%s** [%s/%s] (置信度: %.1f)\n", i+1, exp.Title, exp.Scope, exp.Category, exp.Confidence))
		if exp.TriggerCondition != "" {
			b.WriteString(fmt.Sprintf("   触发条件: %s\n", exp.TriggerCondition))
		}
		if exp.Content != "" {
			content := exp.Content
			if len([]rune(content)) > 400 {
				content = string([]rune(content)[:400]) + "..."
			}
			b.WriteString(fmt.Sprintf("   %s\n", content))
		}
		if exp.CodeSnippet != "" {
			snippet := exp.CodeSnippet
			if len([]rune(snippet)) > 300 {
				snippet = string([]rune(snippet)[:300]) + "..."
			}
			b.WriteString(fmt.Sprintf("   代码片段:\n   ```\n   %s\n   ```\n", snippet))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// executeRemoteKnowledgeSearch handles knowledge_search tool call (general knowledge).
func (c *remoteCodingCallbacks) executeRemoteKnowledgeSearch(argsJSON string) string {
	if c.agent.generalKB == nil {
		return "项目知识库未配置。请使用 ssh_read_file 直接查看项目文件获取信息。"
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}
	query, _ := args["query"].(string)
	if query == "" {
		return "Error: query parameter is required"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	results, err := c.agent.generalKB.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: 5,
	})
	if err != nil {
		return fmt.Sprintf("项目知识库搜索失败: %v", err)
	}
	if len(results) == 0 {
		return fmt.Sprintf("未找到与 %q 相关的项目资料。", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条相关项目资料：\n\n", len(results)))
	for i, r := range results {
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := remoteKnowledgeSnippet(r)
		b.WriteString(fmt.Sprintf("%d. [%.1f] **%s**\n   %s\n\n", i+1, r.Score, source, text))
	}
	return b.String()
}

func remoteKnowledgeSnippet(r knowledge.SearchResult) string {
	text := knowledge.BestContentText(r)
	if text == "" {
		return "(no content)"
	}
	if len([]rune(text)) > 300 {
		text = string([]rune(text)[:300]) + "..."
	}
	return text
}
