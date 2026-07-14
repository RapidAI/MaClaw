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
	// V2 workflow: skip execution confirmation gate — V2 has its own three-phase
	// document review mechanism. The confirmation panel is a legacy artifact.
	if !opts.WorkflowAgentLoop {
		if resp, handled := h.handleExecutionConfirmationGate(opts.FreshTask, msg, opts.Trimmed, opts.HTTPClient); handled {
			return resp
		}
	}
	if resp, handled := h.maybeReturnUnfinishedSlotHint(msg, opts.Trimmed, opts.FreshTask, opts.Decision, opts.UnfinishedSlot); handled {
		return resp
	}
	gatesDone := time.Since(execStart)

	// Immediate UI feedback before any potentially multi-ms pre-loop work.
	if opts.OnProgress != nil {
		opts.OnProgress(imEarlyProgressText)
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
	loopCtx.WorkflowAgentLoop = opts.WorkflowAgentLoop
	loopCtx.WorkflowDocPhase = opts.WorkflowDocPhase
	loopCtx.WorkflowPhaseID = opts.WorkflowPhaseID
	// When WorkflowAgentLoop is set via the confirmation gate path
	// (ConfirmedWorkflowAgentLoop), the DocPhase/PhaseID fields may not
	// be propagated through the routing result. Derive from V2 state.
	if loopCtx.WorkflowAgentLoop && loopCtx.WorkflowPhaseID == "" {
		if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
			if state := wf.machine.GetActive(msg.UserID); state != nil {
				if phase := state.ActivePhase(); phase != nil {
					loopCtx.WorkflowPhaseID = phase.ID
					loopCtx.WorkflowDocPhase = phase.NeedsConfirm
				}
			}
		}
	}
	executionProfile, semanticIntent := h.classifyIMExecutionProfileAndSemantic(msg, opts.WorkflowAgentLoop, opts.AskUserContext != "" || opts.PendingUserReplyContext != "")
	loopCtx.Runtime.Execution = executionProfile
	loopCtx.Runtime.SemanticIntent = semanticIntent
	loopCtxElapsed := time.Since(loopCtxStart)

	history, historyElapsed := drainHistory()
	agentLoopUserText := h.agentLoopUserTextForWorkflow(msg, opts.WorkflowAgentLoop)

	// Coding templates wait for the user's next message after their form is
	// submitted. Consume them before generic direct-execution or SubAgent
	// routing can reinterpret that message.
	if opts.WorkflowAgentLoop && !opts.WorkflowDocPhase && h.hasPendingTemplateSubAgentExecution(msg.UserID) {
		if execResp, handled := h.consumePendingTemplateSubAgentExecution(msg, agentLoopUserText, loopCtx, requestID, opts.OnProgress, opts.OnToken); handled {
			if execResp != nil {
				loopCtx.SkipWorkflowDocCapture = true
				return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
			}
			log.Printf("[workflow-v2] template SubAgent execution returned nil, falling back to agent loop")
		}
	}

	if resp, handled := h.tryDirectExecutionProfile(msg, loopCtx, history); handled {
		imPerfLog("im_pre_loop", execStart, requestID, msg.UserID, "gates", gatesDone, "history_load", historyElapsed, "loop_ctx", loopCtxElapsed, "system_prompt", 0, "history_len", len(history), "prompt_len", 0, "exec_layer", loopCtx.Runtime.Execution.Layer, "exec_task", loopCtx.Runtime.Execution.TaskType)
		return resp
	}

	promptStart := time.Now()
	systemPrompt := h.buildIMEntrySystemPrompt(msg, history, loopCtx, opts.WorkflowAgentLoop, opts.PhasePrompt, opts.AskUserContext, opts.PendingUserReplyContext, opts.CapabilityGapContext)
	promptElapsed := time.Since(promptStart)

	if resp, updatedHistory, handled := h.routeSubAgentExecution(msg, opts.HTTPClient, loopCtx, history, opts.OnProgress, opts.OnToken); handled {
		return resp
	} else {
		history = updatedHistory
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
			// Workflow SubAgent (implementation phase with parsed tasks)
			log.Printf("[workflow-v2] SubAgent execution triggered in agent loop context, user=%s request_id=%s", msg.UserID, requestID)
			wf := h.getWorkflowV2()
			if wf != nil {
				if state := wf.machine.GetActive(msg.UserID); state != nil {
					execResp := h.handleWorkflowV2ExecutionPhaseWithProgress(msg.UserID, state, opts.OnProgress, opts.OnToken)
					if execResp != nil {
						return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
					}
				}
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
	if remoteRaw, isRemoteCodingTemplate := h.pendingTemplateRemoteCoding.LoadAndDelete(msg.UserID); isRemoteCodingTemplate {
		h.pendingV2SubAgentExecution.Delete(msg.UserID)
		remoteCtx, _ := remoteRaw.(remoteCodingTemplateContext)
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
		projectPath, _ := projectPathRaw.(string)
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

// rearmStickyLocalCodingEnvironment keeps pure local coding sessions multi-turn
// (Claude Code–style continuous coding chat) after each SubAgent completion.
func (h *IMMessageHandler) rearmStickyLocalCodingEnvironment(userID, projectPath string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	projectPath = normalizeProjectSessionPath(projectPath)
	if userID == "" || projectPath == "" {
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
