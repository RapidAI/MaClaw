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

	// 0. Handle document-required workflows waiting for user input.
	// When the template declares RequiresInput and the user hasn't provided
	// the document yet, we gate phase execution until we detect that the
	// user has supplied content (file attachment or substantial text).
	if ws.IsWaitingForInput(tmpl) {
		if isSubstantialInput(trimmed) {
			// User has provided the document content — mark as received and
			// proceed to run the first phase with the content as context.
			ws.InputReceived = true
			ws.UpdatedAt = time.Now()
			if err := e.store.SaveWorkflowState(ws); err != nil {
				return nil, fmt.Errorf("保存工作流状态失败: %w", err)
			}
			// Fall through to normal phase execution below.
		} else {
			// Still waiting — remind the user to upload the document.
			req := tmpl.RequiresInput
			hint := req.Description
			if len(req.FileTypes) > 0 {
				hint += fmt.Sprintf("（支持格式：%s）", strings.Join(req.FileTypes, "、"))
			}
			if req.AcceptText {
				hint += "\n\n也可以直接将文档内容粘贴到对话框中，或提供网址由系统自动抓取。"
			}
			return &WorkflowResponse{
				Text:         "📎 " + hint,
				RunAgentLoop: false,
			}, nil
		}
	}

	// 1. Check for skip words.
	if containsAny(trimmed, skipWords) {
		if phase.CanSkip {
			// Only allow skip if the phase has output or is explicitly skippable
			// without output (CanSkip is already checked above).
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
		// Only allow confirmation if the current phase has produced output.
		// Without this guard, a user saying "好的" (acknowledging the workflow
		// start message) would prematurely advance past the requirements phase
		// before any document has been generated.
		if _, hasOutput := ws.PhaseOutputs[ws.CurrentPhase]; hasOutput {
			resp := e.advancePhase(userID, ws, tmpl)
			return resp, nil
		}
		// No output yet — treat as normal input to generate the phase document.
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

	// Determine if this is likely an unrelated message (e.g. "查询天气"
	// while a coding workflow is active). When the current phase already
	// has output AND the message didn't match confirm/skip/modify, the
	// user is probably asking something unrelated to the workflow.
	// When the phase has NO output yet, this is the first execution
	// request (e.g. user said "开工" and the system needs to generate
	// the requirements document) — treat it as genuine workflow input.
	_, hasOutput := ws.PhaseOutputs[ws.CurrentPhase]
	isDefault := hasOutput // only mark as default when phase already has output

	return &WorkflowResponse{
		Text:         "",
		PhasePrompt:  phasePrompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
		DefaultInput: isDefault,
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
		if ws == nil || ws.Status != WorkflowActive || ws.UserID == "" {
			continue
		}
		// Validate phase consistency: if the workflow has advanced past
		// requirements but has no output for earlier phases, it was
		// corrupted by the premature-advance bug. Reset to phase 0.
		if ws.PhaseIndex > 0 && len(ws.PhaseOutputs) == 0 {
			tmpl := e.registry.Match(ws.Type)
			if tmpl != nil && len(tmpl.Phases) > 0 {
				ws.PhaseIndex = 0
				ws.CurrentPhase = tmpl.Phases[0].ID
				if e.store != nil {
					_ = e.store.SaveWorkflowState(ws)
				}
			}
		}
		e.workflows[ws.UserID] = ws
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

// GetRegistry returns the WorkflowRegistry owned by this engine.
func (e *WorkflowEngine) GetRegistry() *WorkflowRegistry {
	return e.registry
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
	// Look up the phase by ID (not by index) to ensure the correct
	// checklist is used even when PhaseIndex and CurrentPhase diverge.
	tmpl := e.registry.Match(ws.Type)
	if tmpl != nil {
		var phase *PhaseTemplate
		for i := range tmpl.Phases {
			if tmpl.Phases[i].ID == phaseID {
				phase = &tmpl.Phases[i]
				break
			}
		}
		if phase != nil {
			if gateResult := RunQualityGate(phase, content); gateResult != nil {
				ws.GateResults[phaseID] = gateResult
				// Emit gate result to frontend (best-effort, non-blocking).
				if e.callbacks != nil {
					_ = e.callbacks.EmitGateResult(userID, phaseID, gateResult)
				}
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

// GetInputRequirement returns the InputRequirement for the given user's
// active workflow, or nil if the workflow doesn't require input documents.
// Used by callers (e.g., handleActiveUnderstanding) to append the upload
// prompt to the workflow startup message.
func (e *WorkflowEngine) GetInputRequirement(userID string) *InputRequirement {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil {
		return nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return nil
	}
	if !tmpl.NeedsInputDocument() {
		return nil
	}
	return tmpl.RequiresInput
}
