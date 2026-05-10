package main

import (
	"log"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) routeSubAgentExecution(msg IMUserMessage, httpClient *http.Client, loopCtx *LoopContext, history []agent.ConversationEntry, onProgress tool.ProgressCallback, onToken llm.TokenCallback) (*IMAgentResponse, []agent.ConversationEntry, bool) {
	if ShouldUseSubAgent(h.getTaskOrchestratorReadOnly(msg.UserID)) {
		log.Printf("[subagent-intercept] routing to SubAgent for user=%s", msg.UserID)
		cfg := h.getMaclawLLMConfig()
		taskOrch := h.getTaskOrchestratorReadOnly(msg.UserID)

		if onProgress != nil {
			onProgress("Starting SubAgent tasks...")
		}

		runner := NewSubAgentTaskRunner(h, cfg, httpClient, taskOrch, loopCtx)
		report := runner.RunAllTasks(onToken, func(text string) {
			if onProgress != nil {
				onProgress(text)
			}
		})

		// Deactivate orchestrator after all tasks are done.
		// NOTE: Deactivate is called AFTER integration and AdvancePhase
		// because those operations need the orchestrator's task state
		// (HasPassedTasks, BuildIntegrationPrompt, ProjectPath, etc.).
		defer taskOrch.Deactivate()

		// --- Fix #1: Save implementation phase output and advance workflow ---
		// Previously, SubAgent completion only deactivated the orchestrator
		// without updating the workflow engine. This left the workflow stuck
		// in the "implementation" phase forever 鈥?the integration and review
		// phases were never reached.
		//
		// Now we:
		// 1. Save the execution report as the implementation phase output
		// 2. Run the integration phase (BuildIntegrationPrompt) if all tasks passed
		// 3. Advance the workflow to the review phase
		if engine := h.getWorkflowEngine(); engine != nil {
			// Inject OwnerID for multi-tenant artifact saving.
			if h.app != nil && h.app.workflowArtifactSaver != nil {
				h.app.workflowArtifactSaver.SetCurrentUserID(msg.UserID)
			}
			// Save implementation phase output so the engine knows it's done.
			engine.SavePhaseOutput(msg.UserID, report)

			// Run integration phase if there are completed tasks to integrate.
			if taskOrch.HasPassedTasks() && !loopCtx.IsCancelled() {
				integrationPrompt := taskOrch.BuildIntegrationPrompt()
				if integrationPrompt != "" {
					if onProgress != nil {
						onProgress("馃敆 鍚姩闆嗘垚鑱旇皟闃舵...")
					}
					integrationResult := RunTaskWithSubAgent(
						h, cfg, httpClient,
						&TaskItem{
							Title:       "闆嗘垚鑱旇皟",
							Description: integrationPrompt,
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
					report += "\n\n## 闆嗘垚鑱旇皟\n\n" + integrationResult.Summary
				}
			}

			// Advance from implementation to review phase.
			// Skip if the user cancelled 鈥?don't advance to review when
			// the implementation was interrupted.
			if !loopCtx.IsCancelled() {
				advResp, advErr := engine.AdvancePhase(msg.UserID)
				if advErr != nil {
					log.Printf("[subagent-intercept] AdvancePhase error after SubAgent: %v", advErr)
				} else if advResp != nil {
					if advResp.Text != "" {
						report += "\n\n---\n" + advResp.Text
					}
					// If the review phase needs the agent loop, stash the prompt
					// so the next user message triggers it.
					if advResp.RunAgentLoop && advResp.PhasePrompt != "" {
						h.stashedPhasePrompt.Store(msg.UserID, advResp.PhasePrompt)
						h.workflowAgentLoopMarker.Store(msg.UserID, true)
					}
					if advResp.Complete {
						if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
							adapter.ResetSuggestMaximize(msg.UserID)
						}
					}
				}
			}
		}

		// Preserve SubAgent execution context in conversation history so the
		// LLM has context for follow-up messages ("鏀逛竴涓?Player 鐨勮烦璺冮€昏緫").
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
