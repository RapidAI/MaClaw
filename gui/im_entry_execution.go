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

	historyStart := time.Now()
	history := h.memory.Load(msg.UserID)
	historyElapsed := time.Since(historyStart)

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
	agentLoopUserText := h.agentLoopUserTextForWorkflow(msg, opts.WorkflowAgentLoop)

	// Direct coding modes entered from the workflow panel wait for the user's
	// next message. Consume them before generic direct-execution or SubAgent
	// routing can reinterpret that message.
	if opts.WorkflowAgentLoop && !opts.WorkflowDocPhase && h.hasPendingDirectSubAgentExecution(msg.UserID) {
		if _, pending := h.pendingV2SubAgentExecution.LoadAndDelete(msg.UserID); pending {
			if remoteRaw, isDirectRemoteCoding := h.pendingDirectRemoteCoding.LoadAndDelete(msg.UserID); isDirectRemoteCoding {
				remoteCtx, _ := remoteRaw.(directRemoteCodingContext)
				log.Printf("[workflow-v2] Direct remote coding SubAgent: user=%s session=%s project=%s request_id=%s", msg.UserID, remoteCtx.SessionID, remoteCtx.ProjectDir, requestID)
				execResp := h.runDirectRemoteCodingSubAgent(msg.UserID, agentLoopUserText, remoteCtx, loopCtx, opts.OnProgress, opts.OnToken)
				if execResp != nil {
					return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
				}
			}
			if projectPathRaw, isDirectCoding := h.pendingDirectCodingProjectPath.LoadAndDelete(msg.UserID); isDirectCoding {
				projectPath := projectPathRaw.(string)
				log.Printf("[workflow-v2] Direct coding SubAgent: user=%s project=%s request_id=%s", msg.UserID, projectPath, requestID)
				execResp := h.runDirectCodingSubAgent(msg.UserID, agentLoopUserText, projectPath, loopCtx, opts.OnProgress, opts.OnToken)
				if execResp != nil {
					return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
				}
			}
			log.Printf("[workflow-v2] Direct SubAgent execution returned nil, falling back to agent loop")
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
		if _, pending := h.pendingV2SubAgentExecution.LoadAndDelete(msg.UserID); pending {
			if remoteRaw, isDirectRemoteCoding := h.pendingDirectRemoteCoding.LoadAndDelete(msg.UserID); isDirectRemoteCoding {
				remoteCtx, _ := remoteRaw.(directRemoteCodingContext)
				log.Printf("[workflow-v2] Direct remote coding SubAgent: user=%s session=%s project=%s request_id=%s", msg.UserID, remoteCtx.SessionID, remoteCtx.ProjectDir, requestID)
				execResp := h.runDirectRemoteCodingSubAgent(msg.UserID, agentLoopUserText, remoteCtx, loopCtx, opts.OnProgress, opts.OnToken)
				if execResp != nil {
					return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
				}
			}
			// Check if this is a direct coding request (no workflow, single task from user text)
			if projectPathRaw, isDirectCoding := h.pendingDirectCodingProjectPath.LoadAndDelete(msg.UserID); isDirectCoding {
				projectPath := projectPathRaw.(string)
				log.Printf("[workflow-v2] Direct coding SubAgent: user=%s project=%s request_id=%s", msg.UserID, projectPath, requestID)
				execResp := h.runDirectCodingSubAgent(msg.UserID, agentLoopUserText, projectPath, loopCtx, opts.OnProgress, opts.OnToken)
				if execResp != nil {
					return h.finalizeIMAgentLoopResponse(msg, loopCtx, execResp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
				}
			} else {
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
			hint := "\n\n---\n📋 请确认以上「" + phaseName + "」文档是否符合预期，或提出修改意见。"
			resp.Text += hint
			// Also send hint via onToken so streaming UI shows it immediately
			if opts.OnToken != nil {
				opts.OnToken(hint)
			}
		}
	}

	return h.finalizeIMAgentLoopResponse(msg, loopCtx, resp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
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
