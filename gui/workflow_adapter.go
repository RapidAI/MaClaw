package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GUIWorkflowAdapter implements workflow.EngineCallbacks for the GUI layer.
// It bridges the workflow engine to the Wails frontend via event emission.
type GUIWorkflowAdapter struct {
	app        *App
	engine     *workflow.WorkflowEngine
	mu         sync.RWMutex
	workingDir string // locked working directory for the current workflow session
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
		// Normalize the current phase ID for the frontend.
		emitState := state
		if state != nil {
			if canonical, ok := normalizePhaseIDMap[state.CurrentPhase]; ok {
				// Shallow copy to avoid mutating the engine's state.
				cp := *state
				cp.CurrentPhase = canonical
				emitState = &cp
			}
		}
		runtime.EventsEmit(a.app.ctx, "workflow:phase_update", emitState)
	}
	return nil
}

// normalizePhaseID maps engine-internal phase IDs to the canonical IDs
// used by the frontend. The workflow engine templates use IDs like
// "tech_design" and "task_breakdown", while the frontend expects
// "design" and "tasks".
var normalizePhaseIDMap = map[string]string{
	"tech_design":    "design",
	"task_breakdown": "tasks",
}

// EmitDocUpdate notifies the frontend of document content changes and
// persists the document to the project's .maclaw/workflow/ directory.
func (a *GUIWorkflowAdapter) EmitDocUpdate(userID, phaseID, content string) error {
	if canonical, ok := normalizePhaseIDMap[phaseID]; ok {
		phaseID = canonical
	}
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:doc_update", map[string]string{
			"user_id":  userID,
			"phase_id": phaseID,
			"content":  content,
		})
	}
	// Persist to project directory: {projectPath}/.maclaw/workflow/{phaseID}.md
	a.persistWorkflowDoc(phaseID, content)
	return nil
}

// phaseFileName maps a phase ID to a human-readable file name.
var phaseFileName = map[string]string{
	"requirements": "01-需求文档.md",
	"design":       "02-技术设计.md",
	"tasks":        "03-任务拆分.md",
}

// persistWorkflowDoc writes the phase document to the workflow working
// directory's .maclaw/workflow/ subdirectory. Uses the locked workingDir
// if set, otherwise falls back to the current project path.
// Errors are logged but not propagated since file persistence is
// best-effort and should not block the UI.
func (a *GUIWorkflowAdapter) persistWorkflowDoc(phaseID, content string) {
	if a.app == nil || strings.TrimSpace(content) == "" {
		return
	}
	a.mu.RLock()
	projectPath := a.workingDir
	a.mu.RUnlock()
	if projectPath == "" {
		projectPath = strings.TrimSpace(a.app.GetCurrentProjectPath())
	}
	if projectPath == "" {
		return
	}
	dir := filepath.Join(projectPath, ".maclaw", "workflow")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[WorkflowAdapter] failed to create workflow dir %s: %v", dir, err)
		return
	}
	fileName := phaseFileName[phaseID]
	if fileName == "" {
		fileName = phaseID + ".md"
	}
	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		log.Printf("[WorkflowAdapter] failed to write workflow doc %s: %v", filePath, err)
	} else {
		log.Printf("[WorkflowAdapter] persisted workflow doc: %s (%d bytes)", filePath, len(content))
	}
}

// SetWorkingDir locks the working directory for the current workflow session.
// Documents will be persisted under {workingDir}/.maclaw/workflow/.
// Also emits a frontend event so the UI can display the path.
func (a *GUIWorkflowAdapter) SetWorkingDir(userID, dir string) {
	trimmed := strings.TrimSpace(dir)
	a.mu.Lock()
	a.workingDir = trimmed
	a.mu.Unlock()
	if trimmed != "" && a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:workdir_set", map[string]string{
			"user_id": userID,
			"path":    trimmed,
		})
		log.Printf("[WorkflowAdapter] working dir set: %s", trimmed)
	}
}

// GetWorkingDir returns the locked working directory, or empty if not set.
func (a *GUIWorkflowAdapter) GetWorkingDir() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.workingDir
}

// ResetWorkingDir clears the working directory when the workflow ends.
func (a *GUIWorkflowAdapter) ResetWorkingDir() {
	a.mu.Lock()
	a.workingDir = ""
	a.mu.Unlock()
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
	if canonical, ok := normalizePhaseIDMap[phaseID]; ok {
		phaseID = canonical
	}
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:gate_result", map[string]interface{}{
			"user_id":  userID,
			"phase_id": phaseID,
			"result":   result,
		})
	}
	return nil
}
