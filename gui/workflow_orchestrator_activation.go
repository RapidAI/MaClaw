package main

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func (h *IMMessageHandler) backfillExecutionOrchestratorActivation(engine *workflow.WorkflowEngine, userID string, resp *workflow.WorkflowResponse) {
	if engine == nil || resp == nil || resp.ActivateOrchestrator || !resp.RunAgentLoop {
		return
	}
	if h.taskOrchestratorRegistry == nil {
		return
	}
	if orch := h.taskOrchestratorRegistry.Get(userID); orch != nil && orch.IsActive() {
		return
	}

	ws, tmpl, ok := activeWorkflowExecutionPhase(engine, userID)
	if !ok {
		return
	}

	resp.ActivateOrchestrator = true
	if ws.PhaseIndex > 0 {
		prevPhaseID := tmpl.Phases[ws.PhaseIndex-1].ID
		resp.TaskBreakdownText = ws.PhaseOutputs[prevPhaseID]
	}
	var reqParts, designParts []string
	for i := 0; i < ws.PhaseIndex; i++ {
		output := ws.PhaseOutputs[tmpl.Phases[i].ID]
		if output == "" {
			continue
		}
		runes := []rune(output)
		if len(runes) > 500 {
			output = string(runes[:500])
		}
		if i == 0 {
			reqParts = append(reqParts, output)
		} else {
			designParts = append(designParts, output)
		}
	}
	resp.RequirementsContext = strings.Join(reqParts, "\n")
	resp.DesignContext = strings.Join(designParts, "\n")
	log.Printf("[WorkflowInterception] backfilled orchestrator activation for active execution phase: user=%s phase=%s", userID, ws.CurrentPhase)
}

func activeWorkflowExecutionPhase(engine *workflow.WorkflowEngine, userID string) (*workflow.WorkflowState, *workflow.WorkflowTemplate, bool) {
	if engine == nil {
		return nil, nil, false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		return nil, nil, false
	}
	registry := engine.GetRegistry()
	if registry == nil {
		return nil, nil, false
	}
	tmpl := registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, nil, false
	}
	phase := tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase || !workflow.IsExecutionOrchestratorPhase(phase) {
		return nil, nil, false
	}
	return ws, tmpl, true
}
