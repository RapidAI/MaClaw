package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
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

		// Keep incomplete runs active so a later user turn can resume after transient
		// provider failures such as LLM rate limits.
		defer func() {
			if shouldDeactivateSubAgentOrchestratorAfterRun(runCompleted, runCancelled) {
				taskOrch.Deactivate()
			}
		}()

		// V1 workflow engine code removed — engine is always nil since
		// im_workflow_engine_stub.go was deleted.

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

	return nil, history, false
}

func (h *IMMessageHandler) workflowAllowsSubAgentExecutionForOwner(ownerID string) (bool, string) {
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
