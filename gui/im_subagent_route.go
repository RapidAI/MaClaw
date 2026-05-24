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
				h.app.workflowArtifactSaver.SetCurrentUserID(msg.UserID)
			}

			// Run integration before saving the implementation deliverable. The engine
			// should persist the final phase artifact, not an intermediate report that
			// is missing integration output.
			if workflowSaveAllowed {
				integrationPrompt := taskOrch.BuildIntegrationPrompt()
				if integrationPrompt != "" {
					if onProgress != nil {
						onProgress("Starting integration phase...")
					}
					integrationResult := RunTaskWithSubAgent(
						h, cfg, httpClient,
						&TaskItem{
							Title:       "Integration",
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
					if !subAgentIntegrationPassed(integrationResult) {
						workflowSaveAllowed = false
						integrationFailed = true
					}
					if integrationResult != nil {
						report += "\n\n## Integration\n\n" + integrationResult.Summary
					}
				}
			}

			// Use the same phase-completion transition as the main agent loop. A
			// cancelled SubAgent run is conversation evidence, not a durable phase
			// deliverable, so it must not mutate workflow output/review state.
			if runCancelled {
				log.Printf("[WorkflowEngine] subagent phase output not saved after cancellation: user=%s", msg.UserID)
			} else if !workflowSaveAllowed {
				logSubAgentWorkflowSaveBlocked(msg.UserID, runCompleted, runHasPassedTasks, integrationFailed)
			} else {
				_, advResp, err := engine.SavePhaseOutputAndMaybeAdvance(msg.UserID, report)
				if err != nil {
					log.Printf("[WorkflowEngine] subagent phase output save failed: user=%s err=%v", msg.UserID, err)
				}
				h.applyWorkflowAutoAdvanceResponse(msg.UserID, advResp, msg.Platform)
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

	return nil, history, false
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
