package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func (h *IMMessageHandler) routeSubAgentExecution(msg IMUserMessage, httpClient *http.Client, loopCtx *LoopContext, history []agent.ConversationEntry, onProgress tool.ProgressCallback, onToken llm.TokenCallback) (*IMAgentResponse, []agent.ConversationEntry, bool) {
	ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if ShouldUseSubAgent(h.getTaskOrchestratorReadOnly(ownerID)) {
		if allowed, reason := h.workflowAllowsSubAgentExecutionForOwner(ownerID); !allowed {
			log.Printf("[subagent-intercept] blocked SubAgent route by workflow policy user=%s owner=%s reason=%s", msg.UserID, ownerID, reason)
			h.deactivateTaskOrchestratorForWorkflowPolicyBlock(ownerID, reason)
			return nil, history, false
		}
		log.Printf("[subagent-intercept] routing to SubAgent for user=%s owner=%s", msg.UserID, ownerID)
		cfg := h.getMaclawLLMConfig()
		taskOrch := h.getTaskOrchestratorReadOnly(ownerID)

		if onProgress != nil {
			onProgress("Starting SubAgent tasks...")
		}
		codeSessionID := newCodingSubAgentCodeSessionID("subagent-workflow", ownerID)
		emitCodingSubAgentCodeSessionStart(h.app, codeSessionID)
		defer emitCodingSubAgentCodeSessionEnd(h.app, codeSessionID)

		runner := NewSubAgentTaskRunner(h, cfg, httpClient, taskOrch, loopCtx)
		runner.codeSessionID = codeSessionID
		report := runner.RunAllTasks(onToken, func(text string) {
			if onProgress != nil {
				onProgress(text)
			}
		})
		runCompleted := taskOrch.AllDone()
		runCancelled := loopCtx != nil && loopCtx.IsCancelled()
		runHasPassedTasks := taskOrch.HasPassedTasks()

		// Keep incomplete runs active so a later user turn can resume after transient
		// provider failures such as LLM rate limits.
		defer func() {
			if shouldDeactivateSubAgentOrchestratorAfterRun(runCompleted, runCancelled) {
				taskOrch.Deactivate()
			}
		}()

		workflowSaveAllowed := shouldSaveSubAgentWorkflowOutput(runCompleted, runCancelled, runHasPassedTasks)
		integrationFailed := false
		if engine := h.getWorkflowEngine(); engine != nil {
			if h.app != nil && h.app.workflowArtifactSaver != nil {
				h.app.workflowArtifactSaver.SetCurrentUserID(ownerID)
			}

			// Run integration before saving the implementation deliverable. The engine
			// should persist the final phase artifact, not an intermediate report that
			// is missing integration output.
			if workflowSaveAllowed {
				integrationPrompt := taskOrch.BuildIntegrationPrompt()
				if integrationPrompt != "" {
					integrationIndex := taskOrch.TaskCount()
					if onProgress != nil {
						onProgress("Starting integration phase...")
					}
					integrationResult := RunTaskWithSubAgent(
						h, cfg, httpClient,
						&TaskItem{
							Index:         integrationIndex,
							DisplayNumber: integrationIndex + 1,
							Title:         "Integration",
							Description:   integrationPrompt,
						},
						taskOrch.ProjectPath,
						taskOrch.RequirementsContext,
						taskOrch.DesignContext,
						runner.collectPreviousOutputs(),
						loopCtx, onToken,
						func(text string) {
							if onProgress != nil {
								onProgress(text)
							}
						},
					)
					if !subAgentIntegrationPassed(integrationResult) {
						workflowSaveAllowed = false
						integrationFailed = true
					}
					if integrationResult != nil {
						emitCodingSubAgentCodeFileEvents(h.app, codeSessionID, taskOrch.ProjectPath, integrationResult.FilesModified, integrationResult.FilesCreated)
						report += "\n\n## Integration\n\n" + integrationResult.Summary
					}
				}
			}

			// Use the same phase-completion transition as the main agent loop. A
			// cancelled SubAgent run is conversation evidence, not a durable phase
			// deliverable, so it must not mutate workflow output/review state.
			if runCancelled {
				log.Printf("[WorkflowEngine] subagent phase output not saved after cancellation: user=%s owner=%s", msg.UserID, ownerID)
			} else if !workflowSaveAllowed {
				logSubAgentWorkflowSaveBlocked(ownerID, runCompleted, runHasPassedTasks, integrationFailed)
			} else {
				_, advResp, err := engine.SavePhaseOutputAndMaybeAdvance(ownerID, report)
				if err != nil {
					log.Printf("[WorkflowEngine] subagent phase output save failed: user=%s owner=%s err=%v", msg.UserID, ownerID, err)
				}
				h.applyWorkflowAutoAdvanceResponse(ownerID, advResp, msg.Platform)
				if advResp != nil && advResp.Text != "" {
					report += "\n\n---\n" + advResp.Text
				}
			}
		}

		// Preserve SubAgent execution context in conversation history so the LLM has
		// context for follow-up messages about the implementation.
		history = append(history, agent.ConversationEntry{
			Role:    "assistant",
			Content: report,
		})

		resp := &IMAgentResponse{Text: report}
		h.saveConversationHistoryTimed(msg.UserID, history, resp)
		return resp, history, true
	}

	if h.shouldRouteBugFixToDirectCodingSubAgent(msg, loopCtx) {
		return h.routeDirectCodingSubAgentExecution(msg, httpClient, loopCtx, history, onProgress, onToken)
	}

	return nil, history, false
}

func (h *IMMessageHandler) shouldRouteBugFixToDirectCodingSubAgent(msg IMUserMessage, loopCtx *LoopContext) bool {
	if h == nil || msg.IsBackground || strings.TrimSpace(msg.Text) == "" {
		return false
	}
	ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if allowed, reason := h.workflowAllowsSubAgentExecutionForOwner(ownerID); !allowed {
		log.Printf("[subagent-intercept] blocked direct CodingSubAgent route by workflow policy user=%s owner=%s reason=%s", msg.UserID, ownerID, reason)
		return false
	}
	if loopCtx != nil {
		if loopCtx.WorkflowAgentLoop || loopCtx.Kind == LoopKindBackground {
			return false
		}
	}
	if blocked, reason := h.directCodingSubAgentExecutionContextBlockReason(msg, loopCtx, false); blocked {
		log.Printf("[subagent-intercept] blocked direct CodingSubAgent route by execution context user=%s reason=%s", msg.UserID, reason)
		return false
	}
	result, ok := h.classifyDirectCodingSubAgentGateIntent(msg)
	if !ok {
		return false
	}
	shouldRoute := shouldRouteGateResultToDirectCodingSubAgent(result, msg, loopCtx)
	if shouldRoute {
		if blocked, reason := h.directCodingSubAgentExecutionContextBlockReason(msg, loopCtx, true); blocked {
			log.Printf("[subagent-intercept] blocked direct CodingSubAgent route by execution context user=%s reason=%s", msg.UserID, reason)
			return false
		}
	}
	if shouldRoute {
		log.Printf("[subagent-intercept] routing semantic bug_fix to CodingSubAgent user=%s conf=%.2f layer=%d degraded=%v reason=%q",
			msg.UserID, result.Confidence, result.Layer, result.Degraded, result.Reason)
	}
	return shouldRoute
}

func (h *IMMessageHandler) classifyDirectCodingSubAgentGateIntent(msg IMUserMessage) (GateIntentResult, bool) {
	uic := h.getUnifiedClassifier()
	if uic == nil {
		uic = unifiedClassifierPtr.Load()
	}
	if uic == nil {
		return GateIntentResult{}, false
	}
	return classifyUnifiedGateIntent(uic, msg.Text, msg.UserID)
}

func shouldRouteGateResultToDirectCodingSubAgent(result GateIntentResult, msg IMUserMessage, loopCtx *LoopContext) bool {
	if msg.IsBackground || strings.TrimSpace(msg.Text) == "" {
		return false
	}
	if loopCtx != nil && (loopCtx.WorkflowAgentLoop || loopCtx.Kind == LoopKindBackground) {
		return false
	}
	if !isSemanticGateResultForDirectCodingSubAgent(result) {
		return false
	}
	return result.Intent == GateIntentBugFix
}

func (h *IMMessageHandler) directCodingSubAgentExecutionContextBlockReason(msg IMUserMessage, loopCtx *LoopContext, requireExistingCodeEvidence bool) (bool, string) {
	ownerID := strings.TrimSpace(msg.UserID)
	interactionContinuation := false
	if loopCtx != nil {
		ownerID = strings.TrimSpace(h.workflowPolicyOwnerID(msg.UserID, loopCtx))
		interactionContinuation = loopCtx.IsAskUserResponse
	}
	reason := h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
		Text:                        msg.Text,
		OwnerID:                     ownerID,
		ProjectPath:                 h.workflowStartProjectPath(),
		InteractionContinuation:     interactionContinuation,
		RequireExistingCodeEvidence: requireExistingCodeEvidence,
	})
	if reason == "" && ownerID != strings.TrimSpace(msg.UserID) {
		reason = h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
			Text:                        msg.Text,
			OwnerID:                     strings.TrimSpace(msg.UserID),
			ProjectPath:                 h.workflowStartProjectPath(),
			InteractionContinuation:     interactionContinuation,
			RequireExistingCodeEvidence: requireExistingCodeEvidence,
		})
	}
	if reason != "" {
		return true, reason
	}
	return false, ""
}

func isSemanticGateResultForDirectCodingSubAgent(result GateIntentResult) bool {
	return isTrustedSemanticGateResult(result)
}

func (h *IMMessageHandler) routeDirectCodingSubAgentExecution(msg IMUserMessage, httpClient *http.Client, loopCtx *LoopContext, history []agent.ConversationEntry, onProgress tool.ProgressCallback, onToken llm.TokenCallback) (*IMAgentResponse, []agent.ConversationEntry, bool) {
	ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if allowed, reason := h.workflowAllowsSubAgentExecutionForOwner(ownerID); !allowed {
		log.Printf("[subagent-intercept] refused direct CodingSubAgent execution by workflow policy user=%s owner=%s reason=%s", msg.UserID, ownerID, reason)
		return nil, history, false
	}
	if onProgress != nil {
		onProgress("Starting CodingSubAgent...")
	}
	if loopCtx != nil && loopCtx.HTTPClient == nil {
		loopCtx.HTTPClient = httpClient
	}
	args := map[string]interface{}{
		"agent":   "coding_workflow",
		"request": msg.Text,
	}
	if projectPath := h.workflowStartProjectPath(); projectPath != "" {
		args["project_path"] = projectPath
	}
	result, handled := h.executeCodingWorkflowDelegateArgs(args, agentLoopToolExecutionOptions{
		Context:    loopCtx,
		UserID:     msg.UserID,
		UserText:   msg.Text,
		OnProgress: onProgress,
		OnToken:    onToken,
	})
	if !handled {
		return nil, history, false
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		text = "CodingSubAgent finished."
	}
	history = append(history, agent.ConversationEntry{Role: "assistant", Content: text})
	resp := &IMAgentResponse{Text: text}
	h.saveConversationHistoryTimed(msg.UserID, history, resp)
	return resp, history, true
}

func (h *IMMessageHandler) workflowAllowsSubAgentExecution(userID string) (bool, string) {
	if h == nil {
		return true, ""
	}
	return h.workflowAllowsSubAgentExecutionForOwner(h.workflowPolicyUserID(userID))
}

func (h *IMMessageHandler) workflowAllowsSubAgentExecutionForOwner(ownerID string) (bool, string) {
	if h == nil {
		return true, ""
	}
	engine := h.getWorkflowEngine()
	ownerID = strings.TrimSpace(ownerID)
	if engine == nil || ownerID == "" || engine.GetActiveWorkflow(ownerID) == nil {
		return true, ""
	}
	if engine.IsPhaseExecutionBlocked(ownerID) {
		return false, "current workflow phase is waiting for required input or review"
	}
	if !engine.IsActivePhaseExecutionOrchestrator(ownerID) {
		policy := engine.GetActivePhaseToolFilter(ownerID)
		if policy != workflow.ToolFilterFull {
			return false, "current workflow phase policy is " + string(policy)
		}
		return false, "current workflow phase is not an orchestrated execution phase"
	}
	return true, ""
}

func (h *IMMessageHandler) deactivateTaskOrchestratorForWorkflowPolicyBlock(userID, reason string) {
	if h == nil || h.taskOrchestratorRegistry == nil || strings.TrimSpace(userID) == "" {
		return
	}
	orch := h.taskOrchestratorRegistry.Get(userID)
	if orch == nil || !orch.IsActive() {
		return
	}
	orch.Deactivate()
	log.Printf("[subagent-intercept] deactivated task orchestrator outside workflow execution phase user=%s reason=%s", userID, reason)
}

func shouldDeactivateSubAgentOrchestratorAfterRun(runCompleted, runCancelled bool) bool {
	return runCompleted || runCancelled
}

func shouldSaveSubAgentWorkflowOutput(runCompleted, runCancelled, hasPassedTasks bool) bool {
	return runCompleted && !runCancelled && hasPassedTasks
}

func subAgentIntegrationPassed(result *CodingSubAgentResult) bool {
	return result != nil && result.Status == TaskExecPassed
}

func logSubAgentWorkflowSaveBlocked(userID string, runCompleted, hasPassedTasks, integrationFailed bool) {
	switch {
	case !runCompleted:
		log.Printf("[WorkflowEngine] subagent phase output not saved before all tasks complete: user=%s", userID)
	case !hasPassedTasks:
		log.Printf("[WorkflowEngine] subagent phase output not saved because no tasks passed: user=%s", userID)
	case integrationFailed:
		log.Printf("[WorkflowEngine] subagent phase output not saved because integration failed: user=%s", userID)
	default:
		log.Printf("[WorkflowEngine] subagent phase output not saved: user=%s", userID)
	}
}
