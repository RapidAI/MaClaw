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

	// suggestMaximizeSent tracks whether the fullscreen suggestion banner
	// has already been emitted for each user in the current app session.
	// Key: userID, Value: true. This prevents the banner from firing on
	// every single message while a workflow is active.
	suggestMaximizeSent sync.Map
}

type frontendWorkflowPhase struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Index           int    `json:"index"`
	ExpectsDocument bool   `json:"expects_document"`
}

type frontendWorkflowState struct {
	*workflow.WorkflowState
	Phases []frontendWorkflowPhase `json:"phases,omitempty"`
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
		var registry *workflow.WorkflowRegistry
		if a.engine != nil {
			registry = a.engine.GetRegistry()
		}
		runtime.EventsEmit(a.app.ctx, "workflow:phase_update", normalizeWorkflowStateForFrontendWithRegistry(state, registry))
	}
	return nil
}

func canonicalWorkflowPhaseID(phaseID string) string {
	if canonical := normalizeWorkflowPhaseID(phaseID); canonical != "" {
		return canonical
	}
	return phaseID
}

func normalizeWorkflowStateForFrontend(state *workflow.WorkflowState) *frontendWorkflowState {
	return normalizeWorkflowStateForFrontendWithRegistry(state, nil)
}

func normalizeWorkflowStateForFrontendWithRegistry(state *workflow.WorkflowState, registry *workflow.WorkflowRegistry) *frontendWorkflowState {
	if state == nil {
		return nil
	}
	cp := *state
	cp.CurrentPhase = canonicalWorkflowPhaseID(cp.CurrentPhase)
	cp.PendingReviewPhaseID = canonicalWorkflowPhaseID(cp.PendingReviewPhaseID)
	cp.PhaseOutputs = normalizeWorkflowPhaseOutputs(state.PhaseOutputs)
	cp.GateResults = normalizeWorkflowGateResults(state.GateResults)
	return &frontendWorkflowState{
		WorkflowState: &cp,
		Phases:        normalizeWorkflowPhasesForFrontend(state.Type, registry),
	}
}

func normalizeWorkflowPhasesForFrontend(workflowType workflow.WorkflowType, registry *workflow.WorkflowRegistry) []frontendWorkflowPhase {
	if registry == nil {
		return nil
	}
	tmpl := registry.Match(workflowType)
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return nil
	}
	phases := make([]frontendWorkflowPhase, 0, len(tmpl.Phases))
	seen := make(map[string]bool, len(tmpl.Phases))
	for _, phase := range tmpl.Phases {
		id := canonicalWorkflowPhaseID(phase.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		phases = append(phases, frontendWorkflowPhase{
			ID:              id,
			Name:            phase.Name,
			Index:           len(phases),
			ExpectsDocument: phase.ToolPolicy != workflow.ToolFilterFull && phase.ToolPolicy != workflow.ToolFilterOpsControlled,
		})
	}
	return phases
}

func normalizeWorkflowPhaseOutputs(outputs map[string]string) map[string]string {
	if outputs == nil {
		return nil
	}
	normalized := make(map[string]string, len(outputs))
	for phaseID, content := range outputs {
		canonical := canonicalWorkflowPhaseID(phaseID)
		if _, exists := normalized[canonical]; exists && phaseID != canonical {
			continue
		}
		normalized[canonical] = content
	}
	return normalized
}

func normalizeWorkflowGateResults(results map[string]*workflow.QualityGateResult) map[string]*workflow.QualityGateResult {
	if results == nil {
		return nil
	}
	normalized := make(map[string]*workflow.QualityGateResult, len(results))
	for phaseID, result := range results {
		canonical := canonicalWorkflowPhaseID(phaseID)
		if _, exists := normalized[canonical]; exists && phaseID != canonical {
			continue
		}
		if result == nil {
			normalized[canonical] = nil
			continue
		}
		cp := *result
		cp.PhaseID = canonicalWorkflowPhaseID(cp.PhaseID)
		normalized[canonical] = &cp
	}
	return normalized
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
	// Read back the persisted file — this is the single source of truth
	// for the preview panel. If the file can't be read, fall back to
	// the in-memory content.
	if fileContent := a.readPersistedDoc(phaseID); fileContent != "" {
		content = fileContent
	}
	if a.app.ctx != nil {
		runtime.EventsEmit(a.app.ctx, "workflow:doc_update", map[string]string{
			"user_id":  userID,
			"phase_id": phaseID,
			"content":  content,
		})
	}
	return nil
}

// readPersistedDoc reads the persisted markdown file for a phase.
// Returns empty string if the file doesn't exist or can't be read.
func (a *GUIWorkflowAdapter) readPersistedDoc(phaseID string) string {
	if a.app == nil {
		return ""
	}
	a.mu.RLock()
	projectPath := a.workingDir
	a.mu.RUnlock()
	if projectPath == "" {
		projectPath = strings.TrimSpace(a.app.GetCurrentProjectPath())
	}
	if projectPath == "" {
		return ""
	}
	fileName := phaseFileName[phaseID]
	if fileName == "" {
		fileName = phaseID + ".md"
	}
	filePath := filepath.Join(projectPath, ".maclaw", "workflow", fileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return string(data)
}

// stripDocPreamble removes conversational text before the first Markdown
// heading (#). LLM output often starts with a sentence like "好的，以下是
// 需求文档：" before the actual document heading.
func stripDocPreamble(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			stripped := strings.TrimSpace(strings.Join(lines[i:], "\n"))
			if len(stripped) > 100 {
				return stripped
			}
			break
		}
	}
	return text
}

// phaseFileName maps a phase ID to a human-readable file name.
var phaseFileName = map[string]string{
	workflowPhaseRequirements: "01-需求文档.md",
	workflowPhaseDesign:       "02-技术设计.md",
	workflowPhaseTasks:        "03-任务拆分.md",
	"ops_intake":              "01-ops-intake.md",
	"readonly_collection":     "02-readonly-collection.md",
	"artifact_plan":           "03-maintenance-artifacts.md",
	"risk_policy":             "04-risk-policy.md",
	"controlled_execution":    "05-controlled-execution.md",
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
			"user_id":  userID,
			"phase_id": phaseID,
			"result":   result,
		})
	}
	return nil
}
