package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
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
		previewRoutePath := codePreviewRouteProjectPath(ownerID, taskOrch.ProjectPath)
		emitCodingSubAgentCodeSessionStart(h.app, codeSessionID, previewRoutePath)
		defer emitCodingSubAgentCodeSessionEnd(h.app, codeSessionID, previewRoutePath)

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

		// Legacy workflow engine was removed during the V2 migration.
		// All workflow state management is now in corelib/workflow/v2.

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
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return true, ""
	}
	_, policy, apply := h.workflowToolFilterOwnerPolicyAndDecision(ownerID, nil)
	if !apply {
		return true, ""
	}
	if policy == v2.ToolFilterNone {
		return false, "current workflow phase is blocked"
	}
	if !v2.IsToolAllowedByPolicy(policy, "delegate_task") {
		return false, "delegate_task is not allowed by the current workflow tool policy"
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
