package workflow

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Confirm words — user wants to advance to the next phase.
var confirmWords = []string{"下一步", "确认", "继续", "没问题", "可以", "好的", "通过"}

// Skip words — user wants to skip the current phase.
var skipWords = []string{"跳过", "skip"}

// Modify indicators — user wants to modify the current phase output.
var modifyIndicators = []string{"改一下", "修改", "调整", "更新"}

// workflowExpiry is the duration after which completed/cancelled workflows
// are eligible for cleanup.
const workflowExpiry = 7 * 24 * time.Hour

// WorkflowEngine is the core state-machine engine that manages workflow
// lifecycle: creation, phase advancement, cancellation, and persistence.
// It is safe for concurrent use.
type WorkflowEngine struct {
	mu            sync.RWMutex
	workflows     map[string]*WorkflowState // userID → active workflow
	registry      *WorkflowRegistry
	understanding *IntentUnderstandingManager
	store         PersistenceStore
	callbacks     EngineCallbacks
	filter        *QuickFilter
}

// NewWorkflowEngine creates a WorkflowEngine with all dependencies wired.
// It creates a QuickFilter that references this engine as WorkflowChecker.
func NewWorkflowEngine(
	registry *WorkflowRegistry,
	understanding *IntentUnderstandingManager,
	store PersistenceStore,
	callbacks EngineCallbacks,
) *WorkflowEngine {
	e := &WorkflowEngine{
		workflows:     make(map[string]*WorkflowState),
		registry:      registry,
		understanding: understanding,
		store:         store,
		callbacks:     callbacks,
	}
	e.filter = NewQuickFilter(e)
	return e
}

// ---------------------------------------------------------------------------
// WorkflowChecker interface implementation (used by QuickFilter)
// ---------------------------------------------------------------------------

// HasActiveWorkflow returns true if the user has an active workflow.
func (e *WorkflowEngine) HasActiveWorkflow(userID string) bool {
	e.mu.RLock()
	ws, ok := e.workflows[userID]
	e.mu.RUnlock()
	return ok && ws != nil && ws.Status == WorkflowActive
}

// HasActiveUnderstanding delegates to IntentUnderstandingManager.HasActiveSession.
func (e *WorkflowEngine) HasActiveUnderstanding(userID string) bool {
	if e.understanding == nil {
		return false
	}
	return e.understanding.HasActiveSession(userID)
}

// ---------------------------------------------------------------------------
// Workflow lifecycle
// ---------------------------------------------------------------------------

// StartWorkflow creates and starts a new workflow for the user based on the
// given StructuredIntent. It validates that the user has no active workflow,
// matches a template by intent.Category, creates a WorkflowState at phase 0,
// persists it, and notifies callbacks.
func (e *WorkflowEngine) StartWorkflow(userID string, intent StructuredIntent) (*WorkflowState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Enforce single active workflow per user.
	if ws, ok := e.workflows[userID]; ok && ws != nil && ws.Status == WorkflowActive {
		return nil, fmt.Errorf("用户已有活跃工作流 (%s)，请先完成或取消当前工作流", ws.Type)
	}

	// Match template by intent category.
	if e.registry == nil {
		return nil, fmt.Errorf("workflow registry not initialized")
	}
	tmpl := e.registry.Match(intent.Category)
	if tmpl == nil {
		return nil, fmt.Errorf("未找到匹配的工作流模板: %s", intent.Category)
	}
	if len(tmpl.Phases) == 0 {
		return nil, fmt.Errorf("工作流模板 %s 没有定义阶段", intent.Category)
	}

	now := time.Now()
	state := &WorkflowState{
		ID:           fmt.Sprintf("wf-%s-%d", userID, now.UnixMilli()),
		UserID:       userID,
		Type:         tmpl.Type,
		Intent:       intent,
		CurrentPhase: tmpl.Phases[0].ID,
		PhaseIndex:   0,
		PhaseOutputs: make(map[string]string),
		GateResults:  make(map[string]*QualityGateResult),
		Status:       WorkflowActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	e.workflows[userID] = state

	// Persist (best-effort).
	if e.store != nil {
		_ = e.store.SaveWorkflowState(state)
	}

	// Notify callbacks (best-effort, outside lock would be ideal but
	// callbacks are expected to be non-blocking).
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, state)
	}

	return state, nil
}

// HandleInput processes user input within an active workflow.
// It parses confirm/skip/modify requests and controls phase advancement.
func (e *WorkflowEngine) HandleInput(userID, text string) (*WorkflowResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws, ok := e.workflows[userID]
	if !ok || ws == nil || ws.Status != WorkflowActive {
		return nil, fmt.Errorf("用户没有活跃的工作流")
	}

	// Look up the current phase template.
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, fmt.Errorf("工作流模板或阶段索引无效")
	}
	phase := &tmpl.Phases[ws.PhaseIndex]

	trimmed := strings.TrimSpace(text)

	// 1. Check for skip words.
	if containsAny(trimmed, skipWords) {
		if phase.CanSkip {
			resp := e.advancePhase(userID, ws, tmpl)
			return resp, nil
		}
		return &WorkflowResponse{
			Text:         fmt.Sprintf("该阶段（%s）不可跳过，请完成后再继续。", phase.Name),
			RunAgentLoop: false,
		}, nil
	}

	// 2. Check for confirm words (only meaningful when NeedsConfirm=true).
	if phase.NeedsConfirm && containsAny(trimmed, confirmWords) {
		resp := e.advancePhase(userID, ws, tmpl)
		return resp, nil
	}

	// 3. Check for modify indicators.
	if containsAny(trimmed, modifyIndicators) {
		phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)
		modifyPrompt := fmt.Sprintf("%s\n\n## 用户修改请求\n\n用户要求修改当前阶段产出物：%s\n请根据修改意见更新产出物。", phasePrompt, trimmed)
		return &WorkflowResponse{
			Text:         "",
			PhasePrompt:  modifyPrompt,
			ToolFilter:   phase.ToolPolicy,
			RunAgentLoop: true,
		}, nil
	}

	// 4. Default: normal phase input — run agent loop with phase prompt.
	phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)
	return &WorkflowResponse{
		Text:         "",
		PhasePrompt:  phasePrompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
	}, nil
}

// advancePhase moves the workflow to the next phase, or marks it completed
// if the current phase is the last one. Must be called with e.mu held.
func (e *WorkflowEngine) advancePhase(userID string, ws *WorkflowState, tmpl *WorkflowTemplate) *WorkflowResponse {
	nextIndex := ws.PhaseIndex + 1
	now := time.Now()

	if nextIndex >= len(tmpl.Phases) {
		// Last phase — mark workflow completed.
		ws.Status = WorkflowCompleted
		ws.UpdatedAt = now
		delete(e.workflows, userID)

		if e.store != nil {
			_ = e.store.SaveWorkflowState(ws)
		}
		if e.callbacks != nil {
			_ = e.callbacks.EmitPhaseUpdate(userID, ws)
		}

		return &WorkflowResponse{
			Text:     fmt.Sprintf("🎉 工作流已完成！所有 %d 个阶段均已完成。", len(tmpl.Phases)),
			Complete: true,
			Advance:  true,
		}
	}

	// Advance to next phase.
	ws.PhaseIndex = nextIndex
	ws.CurrentPhase = tmpl.Phases[nextIndex].ID
	ws.UpdatedAt = now

	if e.store != nil {
		_ = e.store.SaveWorkflowState(ws)
	}
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, ws)
	}

	nextPhase := &tmpl.Phases[nextIndex]
	phasePrompt := BuildPhaseSystemPrompt(ws, nextPhase, e.registry)

	return &WorkflowResponse{
		Text:         fmt.Sprintf("✅ 进入阶段 %d/%d：%s", nextIndex+1, len(tmpl.Phases), nextPhase.Name),
		PhasePrompt:  phasePrompt,
		ToolFilter:   nextPhase.ToolPolicy,
		RunAgentLoop: true,
		Advance:      true,
	}
}

// ---------------------------------------------------------------------------
// Query methods
// ---------------------------------------------------------------------------

// GetActiveWorkflow returns the active workflow for the user, or nil.
func (e *WorkflowEngine) GetActiveWorkflow(userID string) *WorkflowState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ws := e.workflows[userID]
	if ws != nil && ws.Status == WorkflowActive {
		return ws
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

// CancelWorkflow cancels the active workflow for the user.
// The workflow status is set to cancelled and PhaseOutputs are preserved.
func (e *WorkflowEngine) CancelWorkflow(userID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws, ok := e.workflows[userID]
	if !ok || ws == nil || ws.Status != WorkflowActive {
		return fmt.Errorf("用户没有活跃的工作流")
	}

	ws.Status = WorkflowCancelled
	ws.UpdatedAt = time.Now()

	// Remove from active map but preserve the state for persistence.
	delete(e.workflows, userID)

	if e.store != nil {
		_ = e.store.SaveWorkflowState(ws)
	}
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, ws)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Prompt / Tool filter delegation
// ---------------------------------------------------------------------------

// BuildPhasePrompt builds the system prompt for the user's current workflow phase.
// Returns empty string if no active workflow.
func (e *WorkflowEngine) BuildPhasePrompt(userID string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return ""
	}

	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex >= len(tmpl.Phases) {
		return ""
	}

	return BuildPhaseSystemPrompt(ws, &tmpl.Phases[ws.PhaseIndex], e.registry)
}

// GetPhaseToolFilter returns the tool filter policy for the user's current
// workflow phase. Returns ToolFilterNone if no active workflow.
func (e *WorkflowEngine) GetPhaseToolFilter(userID string) ToolFilterPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return ToolFilterNone
	}

	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex >= len(tmpl.Phases) {
		return ToolFilterNone
	}

	return GetToolFilterForPhase(&tmpl.Phases[ws.PhaseIndex])
}

// IsPhaseNeedsConfirm returns true if the user has an active workflow whose
// current phase requires user confirmation before advancing. The agent loop
// uses this to hard-stop after the LLM produces its deliverable, preventing
// stall-detection heuristics from forcing another round that re-outputs the
// same content.
func (e *WorkflowEngine) IsPhaseNeedsConfirm(userID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return false
	}

	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex >= len(tmpl.Phases) {
		return false
	}

	return tmpl.Phases[ws.PhaseIndex].NeedsConfirm
}

// ---------------------------------------------------------------------------
// Persistence restore / cleanup
// ---------------------------------------------------------------------------

// RestoreFromStore loads all active workflows from the persistence store
// into the in-memory map. Called during application startup.
func (e *WorkflowEngine) RestoreFromStore() error {
	if e.store == nil {
		return nil
	}

	states, err := e.store.ListActiveWorkflows()
	if err != nil {
		return fmt.Errorf("restore workflows from store: %w", err)
	}

	e.mu.Lock()
	for _, ws := range states {
		if ws != nil && ws.Status == WorkflowActive && ws.UserID != "" {
			e.workflows[ws.UserID] = ws
		}
	}
	e.mu.Unlock()

	return nil
}

// CleanupExpired removes completed/cancelled workflow records older than 7 days
// from the persistence store.
func (e *WorkflowEngine) CleanupExpired() {
	if e.store == nil {
		return
	}
	_ = e.store.CleanupExpired(workflowExpiry)
}

// ---------------------------------------------------------------------------
// Filter accessor
// ---------------------------------------------------------------------------

// SetCallbacks sets the EngineCallbacks after construction.
// This is useful when the adapter needs a reference to the engine itself
// (chicken-and-egg: engine needs adapter, adapter needs engine).
func (e *WorkflowEngine) SetCallbacks(cb EngineCallbacks) {
	e.mu.Lock()
	e.callbacks = cb
	e.mu.Unlock()
}

// GetCallbacks returns the current EngineCallbacks instance.
func (e *WorkflowEngine) GetCallbacks() EngineCallbacks {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.callbacks
}

// GetFilter returns the QuickFilter instance owned by this engine.
func (e *WorkflowEngine) GetFilter() *QuickFilter {
	return e.filter
}

// GetUnderstanding returns the IntentUnderstandingManager owned by this engine.
// Returns nil if no understanding manager was provided at construction time.
func (e *WorkflowEngine) GetUnderstanding() *IntentUnderstandingManager {
	return e.understanding
}

// ---------------------------------------------------------------------------
// Phase output capture
// ---------------------------------------------------------------------------

// SavePhaseOutput stores the LLM-generated content for the current phase
// and returns the phase ID it was saved under. Returns empty string if
// no active workflow exists. Also runs the quality gate check if the phase
// has checklist items.
func (e *WorkflowEngine) SavePhaseOutput(userID, content string) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return ""
	}

	phaseID := ws.CurrentPhase
	if phaseID == "" {
		return ""
	}

	ws.PhaseOutputs[phaseID] = content
	ws.UpdatedAt = time.Now()

	// Run quality gate check against the phase's checklist.
	tmpl := e.registry.Match(ws.Type)
	if tmpl != nil && ws.PhaseIndex < len(tmpl.Phases) {
		phase := &tmpl.Phases[ws.PhaseIndex]
		if gateResult := RunQualityGate(phase, content); gateResult != nil {
			ws.GateResults[phaseID] = gateResult
			// Emit gate result to frontend (best-effort, non-blocking).
			if e.callbacks != nil {
				_ = e.callbacks.EmitGateResult(userID, phaseID, gateResult)
			}
		}
	}

	if e.store != nil {
		_ = e.store.SaveWorkflowState(ws)
	}

	return phaseID
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------


