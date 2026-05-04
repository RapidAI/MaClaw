package workflow

import (
	"fmt"
	"log"
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

// workflowStaleTimeout is the maximum age of an active workflow before it
// is considered abandoned and automatically cancelled during RestoreFromStore.
// Active workflows are updated on every phase transition, so a 24-hour gap
// strongly indicates the user has moved on.
const workflowStaleTimeout = 24 * time.Hour

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
	artifactSaver ArtifactSaver // optional: saves phase outputs to long-term memory
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

// SetArtifactSaver wires an optional long-term memory saver for phase outputs.
// When set, SavePhaseOutput will persist a summary of each phase's output
// to long-term memory so it survives conversation history truncation.
func (e *WorkflowEngine) SetArtifactSaver(saver ArtifactSaver) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.artifactSaver = saver
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
				hint += "\n\n也可以直接将文档内容粘贴到对话框中，提供本地文件路径，或提供网址由系统自动抓取。"
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
	//
	// IMPORTANT: Only use the keyword shortcut when the message is purely
	// a confirm word (no substantial additional content). When the user
	// says "好的，但是把技术栈改成React", the "好的" matches a confirmWord
	// but the message also contains a modification request. In this case,
	// delegate to the LLM classifier (PendingConfirm path) which can
	// distinguish "pure confirm" from "confirm + modify".
	_, hasOutput := ws.PhaseOutputs[ws.CurrentPhase]
	if phase.NeedsConfirm && hasOutput && isOnlyConfirmWord(trimmed, confirmWords) {
		resp := e.advancePhase(userID, ws, tmpl)
		return resp, nil
	}
	// When NeedsConfirm but no output yet, confirm words are treated as
	// "start generating" signals — fall through to the default branch.

	// 3. LLM-delegated confirm/modify (Kiro-style).
	//
	// When the phase requires confirmation and already has output, we do NOT
	// try to classify the user's intent with keyword matching. Instead, we
	// return PendingConfirm=true so the caller can make a lightweight LLM
	// call (~200 tokens) to classify the intent as confirm/modify/other.
	//
	// The caller (handleActiveWorkflow) uses LLMClassify() with a minimal
	// system prompt — no tools, no conversation history, no streaming.
	// Based on the result:
	//   - "confirm" → caller calls AdvancePhase()
	//   - "modify"  → caller runs agent loop with modify prompt
	//   - "other"   → caller lets the message fall through to normal handling
	if phase.NeedsConfirm && hasOutput {
		log.Printf("[WorkflowEngine] pending confirm: user=%s phase=%s msg=%q",
			userID, ws.CurrentPhase, truncateForLog(trimmed, 50))
		return &WorkflowResponse{
			PendingConfirm: true,
		}, nil
	}

	// 4. Default: normal phase input — run agent loop with phase prompt.
	phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)

	// When the phase has no output yet, this is the first execution request
	// (e.g. user said "开工" and the system needs to generate the document).
	return &WorkflowResponse{
		Text:         "",
		PhasePrompt:  phasePrompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
		DefaultInput: true,
	}, nil
}

// AdvancePhase is the public entry point for advancing the workflow to the
// next phase. Called by the GUI layer after the agent loop completes when
// PendingConfirm was set (LLM-delegated confirm: no new doc = confirm).
func (e *WorkflowEngine) AdvancePhase(userID string) (*WorkflowResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return nil, fmt.Errorf("workflow template not found for type %s", ws.Type)
	}
	return e.advancePhase(userID, ws, tmpl), nil
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

	resp := &WorkflowResponse{
		Text:         fmt.Sprintf("✅ 进入阶段 %d/%d：%s", nextIndex+1, len(tmpl.Phases), nextPhase.Name),
		PhasePrompt:  phasePrompt,
		ToolFilter:   nextPhase.ToolPolicy,
		RunAgentLoop: true,
		Advance:      true,
	}

	// When advancing to an execution phase (ToolFilterFull + !NeedsConfirm),
	// signal the caller to activate the task orchestrator. This is the
	// workflow engine's declaration that "planning phases are done, execute."
	// The caller decides HOW to execute (SubAgent vs main loop vs external).
	//
	// This decouples orchestrator activation from specific phase IDs —
	// coding's "implementation", testing's "test_execution", and PPT's
	// "ppt_generation" all satisfy ToolFilterFull && !NeedsConfirm.
	if nextPhase.ToolPolicy == ToolFilterFull && !nextPhase.NeedsConfirm {
		resp.ActivateOrchestrator = true
		// The task list is in the phase immediately before the execution phase.
		if nextIndex > 0 {
			prevPhaseID := tmpl.Phases[nextIndex-1].ID
			resp.TaskBreakdownText = ws.PhaseOutputs[prevPhaseID]
		}
		// Collect context from all completed planning phases before the
		// execution phase. The first phase's output is the requirements/
		// goals context; the rest form the design context. Derived from
		// template phase order, not hardcoded phase IDs.
		var reqParts, designParts []string
		for i := 0; i < nextIndex; i++ {
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
	}

	return resp
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

// HasPhaseOutput returns true if the user's active workflow has
// output stored for the current phase. The agent loop uses this
// to distinguish first execution (no output yet — let the loop
// continue) from post-output confirmation (output exists — gate
// should force-return).
func (e *WorkflowEngine) HasPhaseOutput(userID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return false
	}
	output, ok := ws.PhaseOutputs[ws.CurrentPhase]
	return ok && output != ""
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

		// Stale workflow cleanup: workflows not updated in 24 hours are
		// likely abandoned. Cancel them to prevent zombie workflows from
		// blocking new workflow creation across application restarts.
		//
		// 24 hours is conservative — covers "user left for the day and
		// came back next morning". Active workflows are updated on every
		// phase transition, so a 24-hour gap strongly indicates abandonment.
		if time.Since(ws.UpdatedAt) > workflowStaleTimeout {
			log.Printf("[WorkflowEngine] cancelling stale workflow %s for user %s "+
				"(type=%s, phase=%s, last_updated=%s, age=%s)",
				ws.ID, ws.UserID, ws.Type, ws.CurrentPhase,
				ws.UpdatedAt.Format("2006-01-02 15:04:05"),
				time.Since(ws.UpdatedAt).Round(time.Minute))
			ws.Status = WorkflowCancelled
			ws.UpdatedAt = time.Now()
			if e.store != nil {
				_ = e.store.SaveWorkflowState(ws)
			}
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

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return ""
	}

	phaseID := ws.CurrentPhase
	if phaseID == "" {
		e.mu.Unlock()
		return ""
	}

	// ── Minimum quality gate ──
	//
	// Reject content that is clearly not a phase deliverable. This catches
	// cases where the LLM ignored the phase prompt and produced unrelated
	// output (e.g., answering a previous task's question instead of
	// generating the phase document).
	//
	// The gate checks structural properties that ANY phase deliverable
	// should have — it does not use phase-specific keywords (which would
	// be a workaround that breaks when templates change).
	if !passesMinimumQualityGate(content) {
		log.Printf("[WorkflowEngine] SavePhaseOutput rejected: content does not pass minimum quality gate for phase=%s user=%s len=%d lines=%d",
			phaseID, userID, len([]rune(content)), strings.Count(content, "\n")+1)
		e.mu.Unlock()
		return ""
	}

	ws.PhaseOutputs[phaseID] = content
	ws.UpdatedAt = time.Now()

	// Run quality gate check against the phase's checklist.
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
				if e.callbacks != nil {
					_ = e.callbacks.EmitGateResult(userID, phaseID, gateResult)
				}
			}
		}
	}

	if e.store != nil {
		_ = e.store.SaveWorkflowState(ws)
	}

	// Capture values needed for artifact sinking before releasing the lock.
	saver := e.artifactSaver
	wsType := string(ws.Type)
	projectPath := ws.ProjectPath

	e.mu.Unlock()

	// Sink phase output summary to long-term memory OUTSIDE the engine lock.
	// This avoids WorkflowEngine.mu → memory.Store.mu lock nesting.
	if saver != nil && len([]rune(content)) > 200 {
		summary := content
		runes := []rune(summary)
		if len(runes) > 800 {
			cutoff := 800
			for i := cutoff; i > 600; i-- {
				if runes[i] == '\n' {
					cutoff = i
					break
				}
			}
			summary = string(runes[:cutoff]) + "\n…(摘要截断)"
		}
		tags := []string{"workflow", phaseID, wsType}
		if projectPath != "" {
			tags = append(tags, projectPath)
		}
		// Derive a human-readable title for the task list.
		// Use the first markdown heading if present, otherwise workflow type + phase.
		title := ""
		for _, line := range strings.SplitN(summary, "\n", 10) {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				break
			}
			if strings.HasPrefix(line, "## ") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "## "))
				break
			}
		}
		if title == "" {
			title = wsType + " — " + phaseID
		}
		_ = saver.SaveArtifact(title, summary, tags, "")
	}

	return phaseID
}

// passesMinimumQualityGate performs a lightweight structural check to reject
// content that is clearly not a valid phase deliverable. Returns true if the
// content should be stored, false if it should be rejected.
//
// This is NOT a comprehensive quality check — it only catches obvious
// failures like a single short sentence that the LLM produced when it
// ignored the phase prompt. The detailed quality assessment is handled
// by RunQualityGate (checklist-based).
//
// The check is phase-type-agnostic: it does not use phase-specific keywords
// (which would be a workaround that breaks when templates change). It only
// checks structural properties that ANY phase deliverable should have.
func passesMinimumQualityGate(content string) bool {
	runes := []rune(content)

	// Gate 1: Minimum length. Any meaningful phase document should be at
	// least 100 runes. A single sentence like "已记录 ✅ ..." (76 bytes)
	// or "开工做什么呢伯伯？" (98 bytes) is not a phase deliverable.
	if len(runes) < 100 {
		return false
	}

	// Gate 2: Structural complexity. A phase deliverable should have some
	// structure — multiple lines, headers, or list items. A single paragraph
	// (fewer than 3 lines) is almost certainly not a phase document.
	lineCount := strings.Count(content, "\n") + 1
	if lineCount < 3 {
		return false
	}

	return true
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

// truncateForLog truncates a string to maxRunes for log output.
func truncateForLog(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
