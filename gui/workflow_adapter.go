package main

import (
	"log"
	"strings"
	"sync"
	"time"

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

	// activeWorkflowID is the ID of the currently active workflow instance.
	// Used to namespace persisted documents under {workingDir}/.maclaw/workflow/{id}/
	// so that different workflow instances (even of the same type) never share files.
	// Set by EmitPhaseUpdate when a new active workflow is detected; cleared on
	// workflow completion/cancellation.
	activeWorkflowID string

	// activeWorkflowType is the workflow type for the current active workflow.
	// Used to determine the Project_Storage subdirectory path (kebab-case).
	// Protected by mu.
	activeWorkflowType workflow.WorkflowType

	// workflowStartDate is the start time of the current workflow instance.
	// Used to generate the date subdirectory (YYYY-MM-DD) in Project_Storage.
	// Protected by mu.
	workflowStartDate time.Time

	// projectStorageDir is the cached resolved Project_Storage date directory path.
	// Lazily resolved on first publish call and reused for the lifetime of the workflow.
	// Reset to empty string when a new workflow starts (forcing re-resolution).
	// Protected by mu.
	projectStorageDir string

	// suggestMaximizeSent tracks whether the fullscreen suggestion banner
	// has already been emitted for each user in the current app session.
	// Key: userID, Value: true. This prevents the banner from firing on
	// every single message while a workflow is active.
	suggestMaximizeSent sync.Map
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

// GetLang returns the current user-facing language from the app config.
// This is the single source of truth for workflow message localization.
func (a *GUIWorkflowAdapter) GetLang() string {
	if a.app != nil {
		return a.app.CurrentLanguage
	}
	return ""
}

// EmitPhaseUpdate notifies the frontend of a phase change.
func (a *GUIWorkflowAdapter) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error {
	if state != nil {
		stateProjectPath := ""
		validStateProjectPath := ""
		workflowStateHasPath := state.ID != "" && (state.Status == workflow.WorkflowActive || state.Status == workflow.WorkflowCompleted || state.Status == workflow.WorkflowCancelled)
		if workflowStateHasPath {
			stateProjectPath = strings.TrimSpace(state.ProjectPath)
			if stateProjectPath != "" {
				normalized, _, err := normalizeWorkflowProjectPath(stateProjectPath)
				if err != nil {
					log.Printf("[WorkflowAdapter] invalid workflow project path %s: %v", stateProjectPath, err)
				} else {
					validStateProjectPath = normalized
				}
			}
		}
		a.mu.Lock()
		if state.Status == workflow.WorkflowActive && state.ID != "" {
			// Only set workflowStartDate when a new workflow instance is detected
			// (different ID from current). This prevents resetting the date on
			// every phase transition within the same workflow.
			newWorkflow := a.activeWorkflowID != state.ID
			if newWorkflow {
				a.workflowStartDate = time.Now()
				a.projectStorageDir = "" // force re-resolution for new workflow
			}
			if validStateProjectPath != "" {
				if a.workingDir != validStateProjectPath {
					a.projectStorageDir = ""
				}
				a.workingDir = validStateProjectPath
			} else {
				a.projectStorageDir = ""
				a.workingDir = ""
			}
			a.activeWorkflowID = state.ID
			a.activeWorkflowType = state.Type
		} else {
			// Workflow completed or cancelled — write manifest before clearing fields,
			// because writeWorkflowManifest depends on activeWorkflowType and workflowStartDate
			// (via resolveProjectStorageDir). We snapshot the needed fields under lock,
			// then release the lock before calling writeManifestOnCompletion to avoid
			// holding the lock during I/O.
			if state.Status == workflow.WorkflowCompleted || state.Status == workflow.WorkflowCancelled {
				if validStateProjectPath != "" {
					if a.workingDir != validStateProjectPath {
						a.projectStorageDir = ""
					}
					a.workingDir = validStateProjectPath
				} else if stateProjectPath != "" {
					a.projectStorageDir = ""
					a.workingDir = ""
				}
				a.mu.Unlock()
				a.writeManifestOnCompletion(state)
				// Clean Internal_Storage only on successful completion (Req 3.3).
				// Must be called before activeWorkflowID is cleared below.
				if state.Status == workflow.WorkflowCompleted {
					a.cleanInternalStorageOnCompletion()
				}
				a.mu.Lock()
			}
			// Clear the instance namespace and all Project_Storage related fields
			// so the next workflow starts fresh.
			a.activeWorkflowID = ""
			a.activeWorkflowType = ""
			a.workflowStartDate = time.Time{}
			a.projectStorageDir = ""
		}
		a.mu.Unlock()
	}
	if a.app.ctx != nil {
		var registry *workflow.WorkflowRegistry
		if a.engine != nil {
			registry = a.engine.GetRegistry()
		}
		runtime.EventsEmit(a.app.ctx, "workflow:phase_update", normalizeWorkflowStateForFrontendWithRegistry(state, registry))
	}
	return nil
}

// EmitDocUpdate notifies the frontend of document content changes and
// persists the document to the project's .maclaw/workflow/ directory.
// The content sent to the frontend is read back from the persisted file
// to ensure the preview panel always shows the clean document.
func (a *GUIWorkflowAdapter) EmitDocUpdate(userID, phaseID, content string) error {
	phaseID = canonicalWorkflowPhaseID(phaseID)
	// Strip conversational preamble before persisting.
	content = stripDocPreamble(content)
	// Persist first.
	a.persistWorkflowDoc(phaseID, content)
	// Publish to Project_Storage (best-effort, non-blocking).
	// This is called after persistWorkflowDoc succeeds so we know the content is valid.
	a.publishToProjectStorage(phaseID, content)
	// Read back the persisted file — this is the single source of truth
	// for the preview panel. If the file can't be read, fall back to
	// the in-memory content.
	if fileContent := a.readPersistedDoc(phaseID); fileContent != "" {
		content = fileContent
	}
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:doc_update", map[string]string{
			"user_id":      userID,
			"phase_id":     phaseID,
			"content":      content,
			"project_path": a.GetWorkingDir(),
		})
	}
	return nil
}

// SetWorkingDir locks the working directory for the current workflow session.
// Documents will be persisted under {workingDir}/.maclaw/workflow/.
// Also emits a frontend event so the UI can display the path.
func (a *GUIWorkflowAdapter) SetWorkingDir(userID, dir string) {
	trimmed, _, err := normalizeWorkflowProjectPath(dir)
	if err != nil {
		log.Printf("[WorkflowAdapter] invalid workflow working directory %s: %v", strings.TrimSpace(dir), err)
		return
	}
	a.mu.Lock()
	if a.workingDir != trimmed {
		a.projectStorageDir = ""
	}
	a.workingDir = trimmed
	a.mu.Unlock()
	if a.engine != nil {
		if err := a.engine.SetProjectPath(userID, trimmed); err != nil {
			log.Printf("[WorkflowAdapter] failed to persist workflow project path: %v", err)
		}
	}
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
// Deduplicates per user: only emits once per app session per user.
// Call ResetSuggestMaximize when a workflow is cancelled or completed
// so the next workflow can trigger the banner again.
func (a *GUIWorkflowAdapter) EmitSuggestMaximize(userID, workflowType string) {
	// Only emit once per user per app session.
	if _, already := a.suggestMaximizeSent.LoadOrStore(userID, true); already {
		return
	}
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:suggest_maximize", map[string]string{
			"user_id":       userID,
			"workflow_type": workflowType,
		})
	}
}

// ResetSuggestMaximize clears the dedup flag so the next workflow for this
// user can trigger the fullscreen suggestion banner again.
// Also notifies the frontend to dismiss the banner.
func (a *GUIWorkflowAdapter) ResetSuggestMaximize(userID string) {
	a.suggestMaximizeSent.Delete(userID)
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:suggest_maximize_dismiss", map[string]string{
			"user_id": userID,
		})
	}
}

// EmitGateResult notifies the frontend of a quality gate result.
func (a *GUIWorkflowAdapter) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	phaseID = canonicalWorkflowPhaseID(phaseID)
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:gate_result", map[string]interface{}{
			"user_id":      userID,
			"phase_id":     phaseID,
			"result":       result,
			"project_path": a.GetWorkingDir(),
		})
	}
	return nil
}
