package v2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// HandleAction indicates what the caller should do after HandleInput.
type HandleAction string

const (
	ActionRunPhase         HandleAction = "run_phase"          // execute the current phase
	ActionShowForm         HandleAction = "show_form"          // emit AG UI form for data collection
	ActionConfirmed        HandleAction = "confirmed"          // user confirmed, advanced to next (or completed)
	ActionModify           HandleAction = "modify"             // user wants to modify, re-run current phase
	ActionPassThrough      HandleAction = "pass_through"       // not relevant to workflow, use normal agent loop
	ActionCancelled        HandleAction = "cancelled"          // workflow cancelled
	ActionCancelAndExecute HandleAction = "cancel_and_execute" // cancel workflow but execute original task directly
)

// HandleResult is returned by StateMachine.HandleInput.
type HandleResult struct {
	Action     HandleAction
	Phase      *Phase // current phase to execute (RunPhase/Modify/ShowForm)
	ModifyHint string // user's modification request (Modify)
	State      *WorkflowState

	// PrefilledData contains auto-collected default values for form fields.
	// Populated by the consumer layer (GUI) when Action == ActionShowForm.
	// The state machine itself does not populate this — it is set externally
	// after HandleInput returns, before the form event is sent to the frontend.
	// Each entry has a verifiable source (memory/context/web); no LLM inference.
	PrefilledData map[string]*PrefilledValue `json:"prefilled_data,omitempty"`
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
			InputSchema:  pt.InputSchema,
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
		// If this phase has an InputSchema and form data hasn't been submitted yet,
		// signal the caller to show the AG UI form instead of running the agent loop.
		if phase.InputSchema != nil && phase.FormData == nil {
			phase.Status = PhaseRunning
			m.store.Save(state)
			m.mu.Unlock()
			return &HandleResult{Action: ActionShowForm, Phase: phase, State: state}, nil
		}
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

// SubmitForm stores the user's AG UI form submission for the current phase.
// After submission, the next HandleInput call will return ActionRunPhase
// (since FormData is now populated, the InputSchema check passes through).
// Validates that all required fields have non-empty values.
func (m *StateMachine) SubmitForm(userID string, formData map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil {
		return fmt.Errorf("no active workflow for user %s", userID)
	}
	if state.Status != StatusActive {
		return fmt.Errorf("workflow for user %s is not active (status=%s)", userID, state.Status)
	}
	phase := state.ActivePhase()
	if phase == nil {
		return fmt.Errorf("no active phase")
	}
	if phase.InputSchema == nil {
		return fmt.Errorf("phase %s does not have an input schema", phase.ID)
	}

	// Validate required fields (top-level Fields + active variant's fields)
	fieldsToValidate := phase.InputSchema.Fields
	if variantID, ok := formData["_agent_view_variant"].(string); ok && variantID != "" {
		for _, v := range phase.InputSchema.Variants {
			if v.ID == variantID {
				fieldsToValidate = append(fieldsToValidate, v.Fields...)
				break
			}
		}
	}
	for _, f := range fieldsToValidate {
		if !f.Required {
			continue
		}
		val, exists := formData[f.Name]
		if !exists || val == nil {
			label := f.Label
			if label == "" {
				label = f.Name
			}
			return fmt.Errorf("必填字段「%s」未填写", label)
		}
		// Check for empty string values
		if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
			label := f.Label
			if label == "" {
				label = f.Name
			}
			return fmt.Errorf("必填字段「%s」不能为空", label)
		}
	}

	phase.FormData = formData

	// Propagate path fields to state.ProjectPath so later phases see it in
	// their prompt header ("项目路径"). Priority: project_path > output_dir.
	if pp := formDataString(formData, "project_path"); pp != "" {
		state.ProjectPath = pp
	} else if od := formDataString(formData, "output_dir"); od != "" {
		state.ProjectPath = od
	}

	state.UpdatedAt = time.Now()
	return m.store.Save(state)
}

// formDataString extracts a trimmed string value from form data, or "".
func formDataString(data map[string]interface{}, key string) string {
	v, ok := data[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// resolveOutputDirFromState scans all phases' FormData for an output_dir or
// project_path field, returning the first non-empty value found.
// This ensures later phases can find the user's specified output directory
// even if state.ProjectPath was set to a useless default at creation time.
func resolveOutputDirFromState(state *WorkflowState) string {
	if state == nil {
		return ""
	}
	for i := range state.Phases {
		fd := state.Phases[i].FormData
		if fd == nil {
			continue
		}
		if pp := formDataString(fd, "project_path"); pp != "" {
			return pp
		}
		if od := formDataString(fd, "output_dir"); od != "" {
			return od
		}
	}
	return ""
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
	phase.Output = SanitizePhaseOutput(phase.ID, output)
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

func SanitizePhaseOutput(phaseID, output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	if calls, malformed := llm.ParseContentToolCallsDetailed(output); len(calls) > 0 {
		if extracted := SanitizePhaseOutputFromToolCalls(phaseID, calls); extracted != "" {
			return llm.StripAllExtra(extracted)
		}
		if malformed {
			return llm.StripAllExtra(output)
		}
		return llm.StripAllExtra(output)
	}
	return llm.StripAllExtra(output)
}

func SanitizePhaseOutputFromToolCalls(phaseID string, calls []llm.ToolCall) string {
	switch strings.ToLower(strings.TrimSpace(phaseID)) {
	case "implementation", "verification", "ppt_generation", "test_execution", "defect_report":
		return ""
	}
	best := ""
	for _, call := range calls {
		switch strings.ToLower(strings.TrimSpace(call.Function.Name)) {
		case "write_file", "write":
		default:
			continue
		}
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			continue
		}
		// Skip script/tool files — only capture document content (e.g. .md, .txt, .docx source).
		// Script files written by phase prompts (e.g. md2docx.py) must be executed, not captured.
		if path, _ := args["path"].(string); isScriptFilePath(path) {
			continue
		}
		content, _ := args["content"].(string)
		content = strings.TrimSpace(content)
		if len([]rune(content)) > len([]rune(best)) {
			best = content
		}
	}
	return llm.StripAllExtra(best)
}

// isScriptFilePath returns true if the file path looks like an executable script
// rather than a document. Such files should be written to disk and executed,
// not captured as workflow phase document output.
func isScriptFilePath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	// Check common script/executable extensions.
	scriptExtensions := []string{
		".py", ".ps1", ".sh", ".bat", ".cmd", ".js", ".ts",
		".rb", ".pl", ".lua", ".vbs", ".wsf", ".r",
	}
	for _, ext := range scriptExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
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

// GetRegistry returns the template registry.
func (m *StateMachine) GetRegistry() *TemplateRegistry {
	return m.templates
}

// GetStore returns the underlying WorkflowStore.
func (m *StateMachine) GetStore() WorkflowStore {
	return m.store
}

// SupplementaryDocEntry is used by AddSupplementaryDocs to pass extracted text.
type SupplementaryDocEntry struct {
	FileName string
	Text     string
}

// AddSupplementaryDocs atomically adds supplementary documents to the workflow state.
// Holds the mutex during the read-modify-write cycle to prevent race conditions
// with concurrent SubmitForm calls.
func (m *StateMachine) AddSupplementaryDocs(userID string, docs []SupplementaryDocEntry) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil || state.Status != StatusActive {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}

	if state.SupplementaryDocs == nil {
		state.SupplementaryDocs = make(map[string]string)
	}

	var processedFiles []string
	for _, doc := range docs {
		fileName := doc.FileName
		// Disambiguate same-name files
		if _, exists := state.SupplementaryDocs[fileName]; exists {
			ext := ""
			if dotIdx := strings.LastIndex(fileName, "."); dotIdx >= 0 {
				ext = fileName[dotIdx:]
				fileName = fileName[:dotIdx]
			}
			for i := 2; ; i++ {
				candidate := fmt.Sprintf("%s_%d%s", fileName, i, ext)
				if _, exists := state.SupplementaryDocs[candidate]; !exists {
					fileName = candidate
					break
				}
			}
		}
		state.SupplementaryDocs[fileName] = doc.Text
		processedFiles = append(processedFiles, fileName)
	}

	state.UpdatedAt = time.Now()
	if err := m.store.Save(state); err != nil {
		return nil, fmt.Errorf("save failed: %w", err)
	}
	return processedFiles, nil
}

// RemoveSupplementaryDoc atomically removes a supplementary document from the workflow state.
func (m *StateMachine) RemoveSupplementaryDoc(userID, fileName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil || state.Status != StatusActive {
		return fmt.Errorf("no active workflow for user %s", userID)
	}

	if len(state.SupplementaryDocs) == 0 {
		return fmt.Errorf("没有已上传的补充材料")
	}
	if _, exists := state.SupplementaryDocs[fileName]; !exists {
		return fmt.Errorf("文件不存在: %s", fileName)
	}

	delete(state.SupplementaryDocs, fileName)
	state.UpdatedAt = time.Now()
	return m.store.Save(state)
}

// SetActivePhaseForTest advances the stored workflow state to the phase at
// the given index. This is a test helper — production code uses HandleInput
// and advanceLocked for proper phase transitions.
func (m *StateMachine) SetActivePhaseForTest(userID string, phaseIndex int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, _ := m.store.Load(userID)
	if state == nil || phaseIndex < 0 || phaseIndex >= len(state.Phases) {
		return
	}
	state.CurrentPhase = phaseIndex
	for i := 0; i < phaseIndex; i++ {
		state.Phases[i].Status = PhaseCompleted
	}
	state.Phases[phaseIndex].Status = PhaseRunning
	state.UpdatedAt = time.Now()
	m.store.Save(state)
}

// SkipPhaseForm marks the current phase's form gate as skipped.
// In V2, this sets FormData to an empty map (non-nil) so the InputSchema
// check in HandleInput passes through, allowing the phase to proceed to
// execution without actual form data.
// Idempotent: no-op if FormData is already populated.
func (m *StateMachine) SkipPhaseForm(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil || state.Status != StatusActive {
		return fmt.Errorf("no active workflow for user %s", userID)
	}
	phase := state.ActivePhase()
	if phase == nil {
		return fmt.Errorf("no active phase")
	}
	// Set FormData to non-nil empty map so HandleInput's InputSchema check
	// (InputSchema != nil && FormData == nil) passes through to ActionRunPhase.
	if phase.FormData == nil {
		phase.FormData = make(map[string]interface{})
	}
	if phase.Status == PhasePending {
		phase.Status = PhaseRunning
	}
	state.UpdatedAt = time.Now()
	return m.store.Save(state)
}

// ApplyReviewIntent handles a user's response to a phase review gate.
// intent can be: "confirm" (advance), "skip" (skip phase), "supplement"/"other" (reopen with feedback)
// Returns a HandleResult indicating the action taken.
func (m *StateMachine) ApplyReviewIntent(userID string, intent string, feedback string) (*HandleResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil || state.Status != StatusActive {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}
	phase := state.ActivePhase()
	if phase == nil {
		return nil, fmt.Errorf("no active phase")
	}

	switch strings.ToLower(strings.TrimSpace(intent)) {
	case "confirm":
		// User approves — advance to next phase.
		result, err := m.advanceLocked(state)
		return result, err

	case "skip":
		// User wants to skip this phase.
		phase.Status = PhaseSkipped
		result, err := m.advanceLocked(state)
		return result, err

	case "cancel":
		// User cancels the workflow.
		state.Status = StatusCancelled
		state.UpdatedAt = time.Now()
		m.store.Save(state)
		return &HandleResult{Action: ActionCancelled, State: state}, nil

	case "switch_task":
		// Cancel workflow so caller can re-route the message.
		state.Status = StatusCancelled
		state.UpdatedAt = time.Now()
		m.store.Save(state)
		return &HandleResult{Action: ActionCancelAndExecute, State: state}, nil

	default:
		// "supplement", "other", or anything else — reopen phase for revision.
		phase.Status = PhaseRunning
		phase.Output = "" // clear previous output for re-generation
		state.UpdatedAt = time.Now()
		m.store.Save(state)
		return &HandleResult{Action: ActionModify, Phase: phase, ModifyHint: feedback, State: state}, nil
	}
}

// advanceLocked moves to the next phase. Caller must hold m.mu.
// Preserves PhaseSkipped status (set by ApplyReviewIntent "skip") rather than
// unconditionally overwriting to PhaseCompleted.
// Rolls back in-memory mutations if store.Save fails, so the returned
// HandleResult always references persisted state.
func (m *StateMachine) advanceLocked(state *WorkflowState) (*HandleResult, error) {
	phase := state.ActivePhase()
	if phase != nil && phase.Status != PhaseSkipped {
		phase.Status = PhaseCompleted
	}

	state.CurrentPhase++
	if state.CurrentPhase >= len(state.Phases) {
		// All phases done
		state.Status = StatusCompleted
		state.UpdatedAt = time.Now()
		if err := m.store.Save(state); err != nil {
			state.CurrentPhase--
			state.Status = StatusActive
			if phase != nil && phase.Status != PhaseSkipped {
				phase.Status = PhaseWaitingConfirm
			}
			return nil, fmt.Errorf("save workflow state: %w", err)
		}
		return &HandleResult{Action: ActionConfirmed, State: state}, nil
	}

	// Start next phase
	next := state.ActivePhase()
	next.Status = PhaseRunning
	state.UpdatedAt = time.Now()
	if err := m.store.Save(state); err != nil {
		state.CurrentPhase--
		next.Status = PhasePending
		if phase != nil && phase.Status != PhaseSkipped {
			phase.Status = PhaseWaitingConfirm
		}
		return nil, fmt.Errorf("save workflow state: %w", err)
	}
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
			// Strip base64 data URLs before truncation — they are binary noise
			// that wastes classifier context tokens.
			cleanOutput := stripBase64DataURLs(phase.Output)
			phaseContext = fmt.Sprintf("工作流类型: %s, 当前阶段: %s, 阶段产出摘要: %s",
				state.Type, phase.Name, truncateForContext(cleanOutput, 200))
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
