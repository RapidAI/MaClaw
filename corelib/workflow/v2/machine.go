package v2

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HandleAction indicates what the caller should do after HandleInput.
type HandleAction string

const (
	ActionRunPhase         HandleAction = "run_phase"          // execute the current phase
	ActionConfirmed        HandleAction = "confirmed"          // user confirmed, advanced to next (or completed)
	ActionModify           HandleAction = "modify"             // user wants to modify, re-run current phase
	ActionPassThrough      HandleAction = "pass_through"       // not relevant to workflow, use normal agent loop
	ActionCancelled        HandleAction = "cancelled"          // workflow cancelled
	ActionCancelAndExecute HandleAction = "cancel_and_execute" // cancel workflow but execute original task directly
)

// HandleResult is returned by StateMachine.HandleInput.
type HandleResult struct {
	Action     HandleAction
	Phase      *Phase // current phase to execute (RunPhase/Modify)
	ModifyHint string // user's modification request (Modify)
	State      *WorkflowState
}

// ConfirmClassifier is a function that uses LLM to classify user intent
// in the context of a pending confirmation. Returns one of:
// "confirm", "modify", "cancel", "unrelated"
type ConfirmClassifier func(phaseContext, userText string) string

// StateMachine manages workflow lifecycle and phase transitions.
type StateMachine struct {
	mu                sync.Mutex // serializes all state mutations per operation
	store             WorkflowStore
	templates         *TemplateRegistry
	confirmClassifier ConfirmClassifier // LLM-based intent classification
}

func NewStateMachine(store WorkflowStore, templates *TemplateRegistry) *StateMachine {
	return &StateMachine{store: store, templates: templates}
}

// SetConfirmClassifier sets the LLM-based confirm intent classifier.
// If nil, falls back to conservative local confirmation handling.
func (m *StateMachine) SetConfirmClassifier(fn ConfirmClassifier) {
	m.confirmClassifier = fn
}

// Create starts a new workflow for the given user.
// projectPath is set once and never changes.
func (m *StateMachine) Create(userID, workflowType, projectPath, summary string) (*WorkflowState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, fmt.Errorf("projectPath is required")
	}
	if looksLikeTempTestPath(projectPath) {
		return nil, fmt.Errorf("project path cannot be a temp/test directory: %s", projectPath)
	}

	tmpl := m.templates.Get(workflowType)
	if tmpl == nil {
		return nil, fmt.Errorf("unknown workflow type: %s", workflowType)
	}

	// Build phases from template
	phases := make([]Phase, len(tmpl.Phases))
	for i, pt := range tmpl.Phases {
		status := PhasePending
		if i == 0 {
			status = PhaseRunning
		}
		phases[i] = Phase{
			ID:           pt.ID,
			Name:         pt.Name,
			NeedsConfirm: pt.NeedsConfirm,
			ToolPolicy:   pt.ToolPolicy,
			ExecMode:     pt.ExecMode,
			Status:       status,
		}
	}

	state := &WorkflowState{
		ID:           GenerateID(userID),
		UserID:       userID,
		Type:         workflowType,
		ProjectPath:  projectPath,
		Summary:      summary,
		Phases:       phases,
		CurrentPhase: 0,
		Status:       StatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := m.store.Save(state); err != nil {
		return nil, fmt.Errorf("save workflow: %w", err)
	}
	return state, nil
}

// HandleInput processes a user message in the context of an active workflow.
func (m *StateMachine) HandleInput(userID, text string) (*HandleResult, error) {
	m.mu.Lock()
	state, err := m.store.Load(userID)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if state == nil || state.Status != StatusActive {
		m.mu.Unlock()
		return &HandleResult{Action: ActionPassThrough}, nil
	}

	phase := state.ActivePhase()
	if phase == nil {
		m.mu.Unlock()
		return &HandleResult{Action: ActionPassThrough}, nil
	}

	switch phase.Status {
	case PhasePending, PhaseRunning:
		// Phase is being executed — tell caller to run agent loop for it
		phase.Status = PhaseRunning
		m.store.Save(state)
		m.mu.Unlock()
		return &HandleResult{Action: ActionRunPhase, Phase: phase, State: state}, nil

	case PhaseExecuting:
		// Background execution in progress — don't interfere, pass through
		m.mu.Unlock()
		return &HandleResult{Action: ActionPassThrough}, nil

	case PhaseWaitingConfirm:
		// Release lock before LLM call (may take 12s)
		m.mu.Unlock()
		intent := m.classifyIntent(state, text)
		// Re-acquire lock for state mutation
		m.mu.Lock()

		// If classifier failed (empty intent), don't advance.
		// Return a special action so the caller can inform the user.
		if intent == "" {
			m.mu.Unlock()
			return &HandleResult{Action: ActionPassThrough, State: state}, nil
		}

		// Re-load state in case it changed during LLM call
		state, err = m.store.Load(userID)
		if err != nil || state == nil || state.Status != StatusActive {
			m.mu.Unlock()
			return &HandleResult{Action: ActionPassThrough}, nil
		}
		phase = state.ActivePhase()
		if phase == nil || phase.Status != PhaseWaitingConfirm {
			m.mu.Unlock()
			return &HandleResult{Action: ActionPassThrough}, nil
		}

		switch intent {
		case "confirm":
			result, err := m.advanceLocked(state)
			m.mu.Unlock()
			return result, err
		case "modify":
			phase.Status = PhaseRunning
			phase.Output = "" // clear previous output
			m.store.Save(state)
			m.mu.Unlock()
			return &HandleResult{Action: ActionModify, Phase: phase, ModifyHint: text, State: state}, nil
		case "cancel":
			state.Status = StatusCancelled
			m.store.Save(state)
			m.mu.Unlock()
			return &HandleResult{Action: ActionCancelled, State: state}, nil
		case "cancel_execute":
			state.Status = StatusCancelled
			m.store.Save(state)
			m.mu.Unlock()
			return &HandleResult{Action: ActionCancelAndExecute, State: state}, nil
		default:
			// Unrelated message — let it go to normal agent loop
			m.mu.Unlock()
			return &HandleResult{Action: ActionPassThrough}, nil
		}

	case PhaseCompleted, PhaseSkipped:
		// Shouldn't be here — advance
		result, err := m.advanceLocked(state)
		m.mu.Unlock()
		return result, err
	}

	m.mu.Unlock()
	return &HandleResult{Action: ActionPassThrough}, nil
}

// RecordOutput saves the phase output and transitions to waiting_confirm.
func (m *StateMachine) RecordOutput(userID, output string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil {
		return fmt.Errorf("no active workflow for user %s", userID)
	}
	// Only record output if workflow is still active.
	// Prevents race condition: old SubAgent goroutine finishing after workflow was cancelled/replaced.
	if state.Status != StatusActive {
		return fmt.Errorf("workflow for user %s is no longer active (status=%s)", userID, state.Status)
	}
	phase := state.ActivePhase()
	if phase == nil {
		return fmt.Errorf("no active phase")
	}
	phase.Output = output
	if phase.NeedsConfirm {
		phase.Status = PhaseWaitingConfirm
	} else {
		// No confirmation needed — mark complete and auto-advance.
		phase.Status = PhaseCompleted
		state.CurrentPhase++
		if state.CurrentPhase >= len(state.Phases) {
			state.Status = StatusCompleted
		} else {
			state.Phases[state.CurrentPhase].Status = PhaseRunning
		}
	}
	state.UpdatedAt = time.Now()
	return m.store.Save(state)
}

// Cancel terminates the workflow.
func (m *StateMachine) Cancel(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil {
		return nil
	}
	state.Status = StatusCancelled
	state.UpdatedAt = time.Now()
	return m.store.Save(state)
}

// GetActive returns the active workflow state for a user, or nil.
func (m *StateMachine) GetActive(userID string) *WorkflowState {
	state, _ := m.store.Load(userID)
	if state == nil || state.Status != StatusActive {
		return nil
	}
	return state
}

// advanceLocked moves to the next phase. Caller must hold m.mu.
func (m *StateMachine) advanceLocked(state *WorkflowState) (*HandleResult, error) {
	phase := state.ActivePhase()
	if phase != nil {
		phase.Status = PhaseCompleted
	}

	state.CurrentPhase++
	if state.CurrentPhase >= len(state.Phases) {
		// All phases done
		state.Status = StatusCompleted
		state.UpdatedAt = time.Now()
		m.store.Save(state)
		return &HandleResult{Action: ActionConfirmed, State: state}, nil
	}

	// Start next phase
	next := state.ActivePhase()
	next.Status = PhaseRunning
	state.UpdatedAt = time.Now()
	m.store.Save(state)
	return &HandleResult{Action: ActionRunPhase, Phase: next, State: state}, nil
}

// --- Helpers ---

// --- Intent Classification ---

// classifyIntent determines user intent using LLM semantic classification.
// When LLM is unavailable, returns "unrelated" (conservative — never auto-advance without understanding).
func (m *StateMachine) classifyIntent(state *WorkflowState, text string) string {
	if m.confirmClassifier != nil {
		phase := state.ActivePhase()
		phaseContext := ""
		if phase != nil {
			phaseContext = fmt.Sprintf("工作流类型: %s, 当前阶段: %s, 阶段产出摘要: %s",
				state.Type, phase.Name, truncateForContext(phase.Output, 200))
		}
		result := m.confirmClassifier(phaseContext, text)
		if result != "" {
			return result
		}
	}
	// LLM unavailable — cannot determine intent.
	// Return empty string to signal "classifier failed" to the caller.
	return ""
}

func truncateForContext(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// ClassifyConfirmIntentKeyword is the fallback keyword-based classifier.
// Used when LLM is unavailable or fails.
func ClassifyConfirmIntentKeyword(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return "unrelated"
	}

	// Cancel takes priority (user might say "取消，不做了")
	cancelWords := []string{"取消", "cancel", "放弃", "不做了", "算了", "停止"}
	for _, w := range cancelWords {
		if strings.Contains(lower, w) {
			// Check if user also wants direct execution after cancel
			// e.g. "取消，直接处理" / "取消工作流，直接做" / "cancel and just do it"
			// We require "直接" as the primary signal — bare "处理"/"做" alone
			// might appear in pure cancellation phrases like "不做了".
			cancelExecutePatterns := []string{
				"直接处理", "直接做", "直接执行", "直接搞", "直接干",
				"直接帮我", "直接来", "直接开始",
				"just do", "directly", "do it directly",
				"跳过流程", "跳过步骤", "跳过确认",
				"不要流程", "不要工作流", "不走流程",
			}
			for _, ep := range cancelExecutePatterns {
				if strings.Contains(lower, ep) {
					return "cancel_execute"
				}
			}
			return "cancel"
		}
	}

	// Confirm: only if the message is SHORT (≤8 runes) and matches a confirm word.
	// This prevents "继续完善需求，加一个登录功能" from being classified as confirm.
	runes := []rune(lower)
	if len(runes) <= 8 {
		confirmWords := []string{
			"确认", "ok", "好的", "可以", "没问题", "通过", "确定",
			"继续", "confirm", "yes", "lgtm", "好", "行", "对",
		}
		for _, w := range confirmWords {
			if lower == w || strings.HasPrefix(lower, w) {
				return "confirm"
			}
		}
	}

	// If the message has substantive content (>4 runes), treat as modification
	if len(runes) > 4 {
		return "modify"
	}

	return "unrelated"
}

func looksLikeTempTestPath(path string) bool {
	lower := strings.ToLower(filepath.Clean(path))
	sep := string(os.PathSeparator)
	// Reject paths that are clearly from Go test temp directories
	if strings.Contains(lower, sep+"temp"+sep) && strings.Contains(lower, "test") {
		return true
	}
	if strings.Contains(lower, "t.tempdir") {
		return true
	}
	return false
}
