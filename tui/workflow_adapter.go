package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// TUIWorkflowAdapter implements workflow.EngineCallbacks for the TUI layer.
// EmitPhaseUpdate, EmitDocUpdate, and EmitGateResult are no-ops since TUI
// has no split-pane document preview UI.
type TUIWorkflowAdapter struct {
	handler *TUIAgentHandler
	engine  *workflow.WorkflowEngine
}

// NewTUIWorkflowAdapter creates a new adapter wiring the handler and engine.
func NewTUIWorkflowAdapter(handler *TUIAgentHandler, engine *workflow.WorkflowEngine) *TUIWorkflowAdapter {
	return &TUIWorkflowAdapter{handler: handler, engine: engine}
}

// SendTextToUser prints a text message to the TUI output.
func (a *TUIWorkflowAdapter) SendTextToUser(userID, text string) error {
	fmt.Println(text)
	return nil
}

// EmitPhaseUpdate is a no-op in TUI (no split-pane UI).
func (a *TUIWorkflowAdapter) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error {
	return nil
}

// EmitDocUpdate is a no-op in TUI (no split-pane UI).
func (a *TUIWorkflowAdapter) EmitDocUpdate(userID, phaseID, content string) error {
	return nil
}

// EmitGateResult is a no-op in TUI (no split-pane UI).
func (a *TUIWorkflowAdapter) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	return nil
}
