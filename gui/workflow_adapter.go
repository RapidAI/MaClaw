package main

import (
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GUIWorkflowAdapter implements workflow.EngineCallbacks for the GUI layer.
// It bridges the workflow engine to the Wails frontend via event emission.
type GUIWorkflowAdapter struct {
	app    *App
	engine *workflow.WorkflowEngine
}

// NewGUIWorkflowAdapter creates a new adapter wiring the App and WorkflowEngine.
func NewGUIWorkflowAdapter(app *App, engine *workflow.WorkflowEngine) *GUIWorkflowAdapter {
	return &GUIWorkflowAdapter{app: app, engine: engine}
}

// SendTextToUser sends a text message to the user via Wails event.
func (a *GUIWorkflowAdapter) SendTextToUser(userID, text string) error {
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:text", map[string]string{
			"user_id": userID,
			"text":    text,
		})
	}
	return nil
}

// EmitPhaseUpdate notifies the frontend of a phase change.
func (a *GUIWorkflowAdapter) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error {
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:phase_update", state)
	}
	return nil
}

// EmitDocUpdate notifies the frontend of document content changes.
func (a *GUIWorkflowAdapter) EmitDocUpdate(userID, phaseID, content string) error {
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:doc_update", map[string]string{
			"user_id":  userID,
			"phase_id": phaseID,
			"content":  content,
		})
	}
	return nil
}

// EmitSuggestMaximize notifies the frontend that a workflow is starting
// and suggests maximizing the AI panel for a better experience.
func (a *GUIWorkflowAdapter) EmitSuggestMaximize(userID, workflowType string) {
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:suggest_maximize", map[string]string{
			"user_id":       userID,
			"workflow_type": workflowType,
		})
	}
}

// EmitGateResult notifies the frontend of a quality gate result.
func (a *GUIWorkflowAdapter) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:gate_result", map[string]interface{}{
			"user_id":  userID,
			"phase_id": phaseID,
			"result":   result,
		})
	}
	return nil
}
