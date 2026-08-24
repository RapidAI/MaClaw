package main

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// imEarlyProgressText is the first local progress nudge after gates, so the UI
// shows feedback before history load / profile classify / system prompt.
const imEarlyProgressText = "收到，正在处理"

type preparedIMEntryExecutionOptions struct {
	Message                   IMUserMessage
	Trimmed                   string
	ProvidedLoopContext       *LoopContext
	HTTPClient                *http.Client
	FreshTask                 bool
	Decision                  explicitTaskSlotDecision
	UnfinishedSlot            *agent.UnfinishedTaskSlot
	WorkflowAgentLoop         bool
	WorkflowDocPhase          bool
	WorkflowPhaseID           string
	PhasePrompt               string // Synchronously passed from runWorkflowV2Phase
	SkipNeedsConfirmGate      bool
	AskUserContext            string
	ResumeWorkingState        *agent.WorkingState
	PendingUserReplyContext   string
	CapabilityGapContext      string
	ClearUIAfterContextSwitch bool
	ConfirmedResume           bool
	OnProgress                tool.ProgressCallback
	OnToken                   llm.TokenCallback
	OnNewRound                NewRoundCallback
	OnStreamDone              StreamDoneCallback
}

func (h *IMMessageHandler) executePreparedIMEntry(opts preparedIMEntryExecutionOptions) *IMAgentResponse {
	msg := opts.Message
	execStart := time.Now()
	requestID := imRequestID(msg)

	if resp, handled := h.handleBackgroundIMRoute(msg, opts.ProvidedLoopContext, opts.HTTPClient, opts.OnProgress); handled {
		return resp
	}
	// Dedicated local/remote coding is already an armed workbench. Do not
	// open the legacy confirmation card, unfinished-slot recover prompt, or
	// chat [Status] milestone — those belong to the shared IM loop.
	// Re-arm before this check: /clear drops pending + sticky Kind, and the
	// project-index lookaside can miss on a cold start.
	if !opts.WorkflowDocPhase {
		h.ensurePureCodingArmedForIncomingMessage(msg.UserID)
	}
	codingWorkbench := h.isPureCodingWorkbenchSession(msg.UserID)
	// V2 workflow: skip execution confirmation gate — V2 has its own three-phase
	// document review mechanism. The confirmation panel is a legacy artifact.
	if !opts.WorkflowAgentLoop && !codingWorkbench {
		if resp, handled := h.handleExecutionConfirmationGate(opts.FreshTask, msg, opts.Trimmed, opts.HTTPClient); handled {
			return resp
		}
	}
	if !codingWorkbench {
		if resp, handled := h.maybeReturnUnfinishedSlotHint(msg, opts.Trimmed, opts.FreshTask, opts.Decision, opts.UnfinishedSlot); handled {
			return resp
		}
	}
	gatesDone := time.Since(execStart)

	// Immediate UI feedback before history/prompt preparation. These are safe
	// execution milestones, not model chain-of-thought.
	if opts.OnProgress != nil && !codingWorkbench {
		opts.OnProgress("[Status] " + imEarlyProgressText)
	}

	// Load conversation history in parallel with loop-context + profile classify.
	// History is pure memory/disk; classify does not depend on history contents.
	// Always drain historyCh before return so the loader goroutine cannot leak.
	type historyLoadResult struct {
		entries []agent.ConversationEntry
		elapsed time.Duration
	}
	historyCh := make(chan historyLoadResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[executePreparedIMEntry] history load panic: %v", r)
				historyCh <- historyLoadResult{}
			}
		}()
		start := time.Now()
		var entries []agent.ConversationEntry
		if h != nil && h.memory != nil {
			entries = h.memory.Load(msg.UserID)
		}
		historyCh <- historyLoadResult{entries: entries, elapsed: time.Since(start)}
	}()
	// Join history after the parallel branch; use a named drain so any early
	// return path after this point cannot leave the goroutine blocked on send.
	// (Channel is buffered size 1 so send never blocks; receive is for ordering.)
	drainHistory := func() (entries []agent.ConversationEntry, elapsed time.Duration) {
		hist := <-historyCh
		return hist.entries, hist.elapsed
	}

	loopCtxStart := time.Now()
	loopCtx := h.prepareIMLoopContext(
		opts.ProvidedLoopContext,
		msg,
		opts.HTTPClient,
		opts.SkipNeedsConfirmGate,
		opts.AskUserContext != "" || opts.PendingUserReplyContext != "",
	)
	turnGeneration := loopCtx.SemanticTurnGeneration()
	turnCtx, cleanupTurnCtx, turnCurrent := loopCtx.SemanticTurnContext(turnGeneration)
	defer cleanupTurnCtx()
	if !turnCurrent {
		return &IMAgentResponse{Error: "semantic_turn_replaced", ResponseSource: "ingress_replacement"}
	}
	bindLoopResumeWorkingState(loopCtx, opts.ResumeWorkingState, opts.AskUserContext)
	loopCtx.WorkflowAgentLoop = opts.WorkflowAgentLoop
	loopCtx.WorkflowDocPhase = opts.WorkflowDocPhase
	loopCtx.WorkflowPhaseID = opts.WorkflowPhaseID
	// When WorkflowAgentLoop is set via the confirmation gate path
	// (ConfirmedWorkflowAgentLoop), the DocPhase/PhaseID/type fields may not
	// be propagated through the routing result. Derive from V2 state.
	if loopCtx.WorkflowAgentLoop {
		if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
			if state := wf.machine.GetActive(msg.UserID); state != nil {
				if loopCtx.WorkflowID == "" {
					loopCtx.WorkflowID = state.ID
				}
				if loopCtx.WorkflowType == "" {
					loopCtx.WorkflowType = strings.TrimSpace(state.Type)
				}
				if phase := state.ActivePhase(); phase != nil {
					if loopCtx.WorkflowPhaseID == "" {
						loopCtx.WorkflowPhaseID = phase.ID
						loopCtx.WorkflowDocPhase = phase.NeedsConfirm
					}
					if loopCtx.WorkflowPhaseKind == "" && phase.Kind != "" {
						loopCtx.WorkflowPhaseKind = string(phase.Kind)
					}
				}
			}
		}
	}
	agentLoopUserText := h.agentLoopUserTextForWorkflow(msg, opts.WorkflowAgentLoop)

	// Dedicated local/remote coding workbench is CodingSubAgent /
	// RemoteCodingSubAgent. Consume it before UIC and
	// semanticCallSurfaceForSharedTurn — that catalog is only for the
	// shared IM loop. Drain history on this return so the loader cannot leak.
	var history []agent.ConversationEntry
	var historyElapsed time.Duration
	historyDrained := false
	// Dedicated local/remote coding does not wait for WorkflowAgentLoop.
	// SkipWorkflowRouting or a missing marker used to skip this consume,
	// then UIC HostRejected a real coding-tab follow-up. Re-arm once more
	// here so a race after entry-context cannot fall into the shared loop.
	if !opts.WorkflowDocPhase && codingWorkbench && !h.hasPendingTemplateSubAgentExecution(msg.UserID) {
		h.ensurePureCodingArmedForIncomingMessage(msg.UserID)
	}
	if !opts.WorkflowDocPhase && h.hasPendingTemplateSubAgentExecution(msg.UserID) {
		if execResp, handled := h.consumePendingTemplateSubAgentExecution(msg, agentLoopUserText, loopCtx, requestID, opts.OnProgress, opts.OnToken); handled {
			history, historyElapsed = drainHistory()
			historyDrained = true
			// Already consumed (and re-armed). Do not fall through to UIC /
			// semanticCallSurfaceForSharedTurn — that can HostReject or run
			// the SubAgent a second time.
			if execResp == nil {
				log.Printf("[coding-env] dedicated coding consume returned nil; skipping semantic routing user=%s request_id=%s", msg.UserID, requestID)
				execResp = &IMAgentResponse{}
			} else {
				log.Printf("[coding-env] dedicated coding workbench skipped semantic routing user=%s request_id=%s", msg.UserID, requestID)
			}
			loopCtx.SkipWorkflowDocCapture = true
			imPerfLog("im_pre_loop", execStart, requestID, msg.UserID, "gates", gatesDone, "history_load", historyElapsed, "loop_ctx", time.Since(loopCtxStart), "system_prompt", 0, "history_len", len(history), "prompt_len", 0, "exec_layer", "coding_subagent", "exec_task", "coding")
			return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, true, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
		}
	}
	if !historyDrained {
		history, historyElapsed = drainHistory()
	}
	// A workbench session that missed consume (retry, workflow, unarmed race)
	// must not pay UIC / semanticCallSurfaceForSharedTurn. That catalog
	// HostRejects coding follow-ups. Workflow execution still runs below.
	if !codingWorkbench {
		executionProfile, semanticIntent := h.classifyIMExecutionProfileAndSemanticContext(turnCtx, msg, opts.WorkflowAgentLoop, opts.AskUserContext != "" || opts.PendingUserReplyContext != "", recentHistoryTexts(history, 6))
		if err := semanticRoutingRequestErr(turnCtx); err != nil || !loopCtx.SemanticTurnCurrent(turnGeneration) {
			return &IMAgentResponse{Error: "semantic_turn_replaced", ResponseSource: "ingress_replacement"}
		}
		if semanticIntent != nil {
			if replayed, ok := h.applySessionGovernedContinuation(msg.UserID, msg.Platform, sessionGovernedDestination(loopCtx), opts.WorkflowAgentLoop, *semanticIntent, msg.Text, msg.Attachments); ok {
				copied := replayed
				semanticIntent = &copied
				executionProfile = executionProfileFromSemanticIntent(semanticIntent, h.executionContractForRegisteredToolName)
			}
		}
		loopCtx.Runtime.Execution = executionProfile
		bindLoopSemanticIntent(loopCtx, semanticIntent)
	}
	applyStagedImageUnderstandRuntime(loopCtx, msg.Text, msg.Attachments)
	loopCtxElapsed := time.Since(loopCtxStart)
	if opts.OnProgress != nil && !codingWorkbench {
		if loopCtx.Runtime.Execution.IsDirect() {
			opts.OnProgress("[Status] 已匹配直接执行能力，正在处理")
		} else if loopCtx.Runtime.Execution.IsLight() {
			opts.OnProgress("[Status] 已选择快速执行路径，正在构建请求")
		} else {
			opts.OnProgress("[Status] 已选择完整执行路径，正在构建请求")
		}
	}

	if !codingWorkbench {
		if resp, handled := h.tryDirectExecutionProfile(msg, loopCtx, history); handled {
			imPerfLog("im_pre_loop", execStart, requestID, msg.UserID, "gates", gatesDone, "history_load", historyElapsed, "loop_ctx", loopCtxElapsed, "system_prompt", 0, "history_len", len(history), "prompt_len", 0, "exec_layer", loopCtx.Runtime.Execution.Layer, "exec_task", loopCtx.Runtime.Execution.TaskType)
			return resp
		}
	}

	promptStart := time.Now()
	if opts.OnProgress != nil && !codingWorkbench {
		opts.OnProgress("[Status] 正在整理上下文并准备模型请求")
	}
	systemPrompt := h.buildIMEntrySystemPrompt(msg, history, loopCtx, opts.WorkflowAgentLoop, opts.PhasePrompt, opts.AskUserContext, opts.PendingUserReplyContext, opts.CapabilityGapContext)
	promptElapsed := time.Since(promptStart)

	if !codingWorkbench || opts.WorkflowAgentLoop {
		if resp, updatedHistory, handled := h.routeSubAgentExecution(msg, opts.HTTPClient, loopCtx, history, opts.OnProgress, opts.OnToken); handled {
			return resp
		} else {
			history = updatedHistory
		}
	}

	totalPreLoop := time.Since(execStart)
	if totalPreLoop > 500*time.Millisecond {
		log.Printf("[executePreparedIMEntry] slow pre-loop: gates=%v history_load=%v system_prompt=%v loop_ctx=%v total=%v user=%s",
			gatesDone, historyElapsed, promptElapsed, loopCtxElapsed, totalPreLoop, msg.UserID)
	}
	imPerfLog("im_pre_loop", execStart, requestID, msg.UserID, "gates", gatesDone, "history_load", historyElapsed, "loop_ctx", loopCtxElapsed, "system_prompt", promptElapsed, "history_len", len(history), "prompt_len", len(systemPrompt), "exec_layer", loopCtx.Runtime.Execution.Layer, "exec_task", loopCtx.Runtime.Execution.TaskType)

	// V2 SubAgent execution: check the dedicated marker (not stashedPhasePrompt
	// which gets consumed by system prompt builder via LoadAndDelete).
	if opts.WorkflowAgentLoop && !opts.WorkflowDocPhase {
		if execResp, handled := h.consumePendingTemplateSubAgentExecution(msg, agentLoopUserText, loopCtx, requestID, opts.OnProgress, opts.OnToken); handled {
			if execResp != nil {
				loopCtx.SkipWorkflowDocCapture = true
				return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
			}
			log.Printf("[workflow-v2] template SubAgent execution returned nil, falling back to workflow execution")
		}
		if _, pending := h.pendingV2SubAgentExecution.LoadAndDelete(msg.UserID); pending {
			// Workflow SubAgent (implementation phase with parsed tasks).
			// Always invoke the handler: a queued 重试失败 / 继续 with no
			// active phase must return its own message, not fall into the
			// shared agent loop (nil panic / HostReject).
			log.Printf("[workflow-v2] SubAgent execution triggered in agent loop context, user=%s request_id=%s", msg.UserID, requestID)
			var state *v2.WorkflowState
			if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
				state = wf.machine.GetActive(msg.UserID)
			}
			execResp := h.handleWorkflowV2ExecutionPhaseWithProgress(msg.UserID, state, opts.OnProgress, opts.OnToken, loopCtx)
			if execResp != nil {
				return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
			}
			log.Printf("[workflow-v2] SubAgent execution returned nil, falling back to agent loop")
		}
	}

	resp := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, agentLoopUserText, msg.Attachments, opts.OnProgress, opts.OnToken, opts.OnNewRound, opts.OnStreamDone, msg.MinIterations, msg.Platform)

	// V2 workflow doc phase: append confirmation hint to response text.
	// The phase prompt forbids the LLM from outputting confirmation prompts
	// (to prevent self-confirmation loops). The hint is added by the system instead.
	// Check both resp.Text (finalize path) and WorkflowDocBuffer (multi-iteration accumulation).
	if opts.WorkflowDocPhase && resp != nil && resp.Error == "" {
		hasContent := sanitizeWorkflowDocPhaseResponseText(resp, loopCtx, opts.WorkflowPhaseID)
		if hasContent {
			phaseName := ""
			if wf := h.getWorkflowV2(); wf != nil {
				if state := wf.machine.GetActive(msg.UserID); state != nil {
					if p := state.ActivePhase(); p != nil {
						phaseName = p.Name
					}
				}
			}
			if phaseName == "" {
				phaseName = "当前阶段"
			}
			hint := "\n\n---\n请确认以上「" + phaseName + "」文档是否符合预期，或提出修改意见。"
			resp.Text += hint
			// Also send hint via onToken so streaming UI shows it immediately
			if opts.OnToken != nil {
				opts.OnToken(hint)
			}
		}
	}

	return h.finalizeIMAgentLoopResponse(msg, loopCtx, resp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
}

func (h *IMMessageHandler) consumePendingTemplateSubAgentExecution(msg IMUserMessage, agentLoopUserText string, loopCtx *LoopContext, requestID string, onProgress tool.ProgressCallback, onToken llm.TokenCallback) (*IMAgentResponse, bool) {
	if h == nil {
		return nil, false
	}
	if _, pending := h.pendingV2SubAgentExecution.Load(msg.UserID); !pending {
		return nil, false
	}
	// Coding-workflow implementation / checkpoint retry must win over pure sticky
	// coding. Otherwise multi-turn remote/local pure coding steals "继续" / "重试失败".
	if _, retryPending := h.pendingCodingExecRetryAction.Load(msg.UserID); retryPending {
		return nil, false
	}
	if wf := h.getWorkflowV2(); wf != nil {
		if state := wf.machine.GetActive(msg.UserID); state != nil &&
			state.IsExecutionPhase() && strings.EqualFold(strings.TrimSpace(state.Type), "coding") {
			// Active coding-workflow implementation owns this SubAgent turn.
			return nil, false
		}
	}
	if remoteRaw, isRemoteCodingTemplate := h.pendingTemplateRemoteCoding.LoadAndDelete(msg.UserID); isRemoteCodingTemplate {
		h.pendingV2SubAgentExecution.Delete(msg.UserID)
		remoteCtx, _ := remoteRaw.(remoteCodingTemplateContext)
		// Request intent normally belongs to this message, not to the sticky SSH
		// session. The incident-diagnosis entry point is the exception: it locks
		// only its first auto-sent turn to evidence-only inspection, preventing a
		// classifier outage from turning a diagnosis into remote writes.
		if remoteCtx.ForceInitialInquiry {
			// ForceInitialInquiry is the durable marker used by the diagnosis entry
			// point. Infer the matching presentation intent as well so sticky
			// contexts saved before Maintenance was introduced cannot regress to a
			// misleading "remote coding" label after reopen or reconnect.
			remoteCtx.Maintenance = true
			remoteCtx.RequestKind = codingRequestInquiry
			remoteCtx.RequestNeedsPlan = false
			remoteCtx.ForceInitialInquiry = false
		} else {
			decision := h.resolveCodingRequestDecision(agentLoopUserText)
			remoteCtx.RequestKind = decision.Kind
			remoteCtx.RequestNeedsPlan = decision.NeedsPlan
		}
		// Attach images/files for this pure-coding turn (vision-capable models).
		if loopCtx != nil && len(msg.Attachments) > 0 {
			loopCtx.CodingAttachments = append([]MessageAttachment(nil), msg.Attachments...)
		}
		log.Printf("[workflow-v2] pure remote coding execution: user=%s session=%s project=%s request_id=%s attachments=%d", msg.UserID, remoteCtx.SessionID, remoteCtx.ProjectDir, requestID, len(msg.Attachments))
		resp := h.runRemoteCodingTemplateSubAgent(msg.UserID, agentLoopUserText, remoteCtx, loopCtx, onProgress, onToken)
		if loopCtx != nil {
			loopCtx.CodingAttachments = nil
		}
		// Multi-turn full workbench: re-arm so follow-up messages stay in RemoteCodingSubAgent.
		h.rearmStickyRemoteCodingEnvironment(msg.UserID, remoteCtx)
		return resp, true
	}
	if projectPathRaw, isCodingTemplate := h.pendingTemplateCodingProjectPath.LoadAndDelete(msg.UserID); isCodingTemplate {
		h.pendingV2SubAgentExecution.Delete(msg.UserID)
		if h.codingSessionIsRemote(msg.UserID, h.getStickyCodingWorkbenchMemory(msg.UserID)) {
			// A remote-tagged task must never execute against the local cwd,
			// even if a leftover local pending path leaked into this owner.
			return nil, false
		}
		stored, _ := projectPathRaw.(string)
		projectPath := h.liveLocalCodingExecDir(msg.UserID, stored)
		if projectPath == "" {
			log.Printf("[workflow-v2] pure coding skipped: empty work root user=%s", msg.UserID)
			return nil, false
		}
		if loopCtx != nil && len(msg.Attachments) > 0 {
			loopCtx.CodingAttachments = append([]MessageAttachment(nil), msg.Attachments...)
		}
		log.Printf("[workflow-v2] pure coding execution: user=%s project=%s request_id=%s attachments=%d", msg.UserID, projectPath, requestID, len(msg.Attachments))
		resp := h.runCodingTemplateSubAgent(msg.UserID, agentLoopUserText, projectPath, loopCtx, onProgress, onToken)
		if loopCtx != nil {
			loopCtx.CodingAttachments = nil
		}
		// Multi-turn full workbench: re-arm so follow-up messages stay in CodingSubAgent.
		h.rearmStickyLocalCodingEnvironment(msg.UserID, projectPath)
		return resp, true
	}
	return nil, false
}

// liveLocalCodingExecDir is the work root for this turn. The pending map and
// sticky ProjectPath are caches; they must not override the current working
// directory (otherwise a stale taskDir/workspace survives a directory change).
func (h *IMMessageHandler) liveLocalCodingExecDir(userID, stored string) string {
	if h != nil && h.app != nil {
		if dir := strings.TrimSpace(h.app.liveWorkingDirForCodingOwner(userID)); dir != "" {
			return dir
		}
	}
	stored = normalizeProjectSessionPath(stored)
	if isLocalCodingIdentityOrSandbox(projectPathFromSessionOwnerID(userID), stored) {
		return ""
	}
	return stored
}

func (h *IMMessageHandler) codingSessionIsRemote(userID string, mem stickyCodingWorkbenchMemory) bool {
	if strings.EqualFold(strings.TrimSpace(mem.Kind), "remote") {
		return true
	}
	if h == nil || h.app == nil {
		return false
	}
	identity := projectPathFromSessionOwnerID(userID)
	return identity != "" && h.app.projectPathIsCodingWorkbench(identity) && !h.app.projectPathIsLocalCodingWorkbench(identity)
}

// codingSessionWorkRoot is the directory slash commands and checkpoints use.
// Local sessions follow the live current working directory; remote sessions
// keep the stored remote path and never inherit the desktop cwd.
func (h *IMMessageHandler) codingSessionWorkRoot(userID string, mem stickyCodingWorkbenchMemory) string {
	if h.codingSessionIsRemote(userID, mem) {
		if p := strings.TrimSpace(mem.ProjectPath); p != "" {
			return p
		}
		if p := strings.TrimSpace(mem.RemoteProjectDir); p != "" {
			return p
		}
		return strings.TrimSpace(mem.RemoteWorkDir)
	}
	return h.liveLocalCodingExecDir(userID, mem.ProjectPath)
}

// rearmStickyLocalCodingEnvironment keeps pure local coding sessions multi-turn
// (Claude Code–style continuous coding chat) after each SubAgent completion.
func (h *IMMessageHandler) rearmStickyLocalCodingEnvironment(userID, projectPath string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	projectPath = normalizeProjectSessionPath(projectPath)
	if userID == "" {
		return
	}
	if identity := projectPathFromSessionOwnerID(userID); h.app != nil &&
		h.app.projectPathIsLocalCodingWorkbench(identity) &&
		isLocalCodingIdentityOrSandbox(identity, projectPath) {
		projectPath = h.liveLocalCodingExecDir(userID, "")
	}
	if projectPath == "" {
		return
	}
	h.pendingTemplateRemoteCoding.Delete(userID)
	h.pendingV2SubAgentExecution.Store(userID, true)
	h.pendingTemplateCodingProjectPath.Store(userID, projectPath)
	h.workflowAgentLoopMarker.Store(userID, true)
	// Keep durable memory in sync for reopen-after-restart (skip disk when hot-path unchanged).
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.Kind != "local" || normalizeProjectSessionPath(mem.ProjectPath) != projectPath {
		mem.Kind = "local"
		mem.ProjectPath = projectPath
		h.storeStickyCodingWorkbenchMemory(userID, mem)
	}
	log.Printf("[coding-env] re-armed sticky local coding session user=%s project=%s", userID, projectPath)
}

// rearmStickyRemoteCodingEnvironment keeps pure remote coding sessions multi-turn.
func (h *IMMessageHandler) rearmStickyRemoteCodingEnvironment(userID string, remoteCtx remoteCodingTemplateContext) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || strings.TrimSpace(remoteCtx.SessionID) == "" {
		return
	}
	// A continuation may have recreated an expired SSH session. Prefer the
	// latest durable remote context so this re-arm never puts the removed ID
	// back into the next turn's pending template.
	if mem := h.getStickyCodingWorkbenchMemory(userID); mem.Kind == "remote" && strings.TrimSpace(mem.RemoteSessionID) != "" {
		remoteCtx.SessionID = strings.TrimSpace(mem.RemoteSessionID)
		if strings.TrimSpace(mem.RemoteWorkDir) != "" {
			remoteCtx.WorkDir = strings.TrimSpace(mem.RemoteWorkDir)
		}
		if strings.TrimSpace(mem.RemoteProjectDir) != "" {
			remoteCtx.ProjectDir = strings.TrimSpace(mem.RemoteProjectDir)
		}
	}
	// RequestKind is per turn. Never persist it with a reusable connection
	// context: the next user message is classified independently above. Keep
	// ForceInitialInquiry when restoring an untouched diagnosis task; the first
	// consuming turn clears it before calling this re-arm helper.
	remoteCtx.RequestKind = ""
	remoteCtx.RequestNeedsPlan = false
	h.pendingTemplateCodingProjectPath.Delete(userID)
	h.pendingV2SubAgentExecution.Store(userID, true)
	h.pendingTemplateRemoteCoding.Store(userID, remoteCtx)
	h.workflowAgentLoopMarker.Store(userID, true)
	h.bindStickyRemoteCodingContext(userID, remoteCtx, "", "", 0)
	log.Printf("[coding-env] re-armed sticky remote coding session user=%s session=%s project=%s", userID, remoteCtx.SessionID, remoteCtx.ProjectDir)
}

// clearStickyCodingEnvironment drops multi-turn coding bindings for a user session.
func (h *IMMessageHandler) clearStickyCodingEnvironment(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.pendingV2SubAgentExecution.Delete(userID)
	h.pendingTemplateCodingProjectPath.Delete(userID)
	h.pendingTemplateRemoteCoding.Delete(userID)
	h.clearStickyCodingWorkbenchMemory(userID)
}

// clearPendingPureCodingTemplateExecution drops pure local/remote coding template
// markers so a coding-workflow SubAgent turn is not stolen. Durable sticky memory
// is left intact for reopen after the workflow finishes.
func (h *IMMessageHandler) clearPendingPureCodingTemplateExecution(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.pendingTemplateCodingProjectPath.Delete(userID)
	h.pendingTemplateRemoteCoding.Delete(userID)
}

func sanitizeWorkflowDocPhaseResponseText(resp *IMAgentResponse, loopCtx *LoopContext, phaseID string) bool {
	if resp == nil {
		return false
	}
	if loopCtx != nil && loopCtx.WorkflowDocBuffer.Len() > 0 {
		if t := strings.TrimSpace(loopCtx.WorkflowDocBuffer.String()); t != "" {
			cleaned := v2.SanitizePhaseOutput(phaseID, t)
			resp.Text = cleaned
			return strings.TrimSpace(cleaned) != ""
		}
	}
	cleaned := v2.SanitizePhaseOutput(phaseID, resp.Text)
	resp.Text = cleaned
	return strings.TrimSpace(cleaned) != ""
}

func recentHistoryTexts(entries []agent.ConversationEntry, limit int) []string {
	if limit <= 0 {
		limit = 6
	}
	var texts []string
	for i := len(entries) - 1; i >= 0 && len(texts) < limit; i-- {
		role := strings.ToLower(strings.TrimSpace(entries[i].Role))
		if role != "user" && role != "assistant" {
			continue
		}
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		texts = append(texts, text)
	}
	for i, j := 0, len(texts)-1; i < j; i, j = i+1, j-1 {
		texts[i], texts[j] = texts[j], texts[i]
	}
	return texts
}
