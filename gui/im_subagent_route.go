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

		// Deactivate orchestrator after all workflow integration and state updates
		// because these operations still need task state and collected outputs.
		defer taskOrch.Deactivate()

		if engine := h.getWorkflowEngine(); engine != nil {
			if h.app != nil && h.app.workflowArtifactSaver != nil {
				h.app.workflowArtifactSaver.SetCurrentUserID(msg.UserID)
			}

			// Run integration before saving the implementation deliverable. The engine
			// should persist the final phase artifact, not an intermediate report that
			// is missing integration output.
			if taskOrch.HasPassedTasks() && !loopCtx.IsCancelled() {
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
					report += "\n\n## Integration\n\n" + integrationResult.Summary
				}
			}

			// Use the same phase-completion transition as the main agent loop. A
			// cancelled SubAgent run is conversation evidence, not a durable phase
			// deliverable, so it must not mutate workflow output/review state.
			if loopCtx.IsCancelled() {
				log.Printf("[WorkflowEngine] subagent phase output not saved after cancellation: user=%s", msg.UserID)
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
