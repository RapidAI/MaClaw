package v2

import (
	"encoding/json"
	"fmt"
	"log"
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
	mu                 sync.Mutex // serializes all state mutations per operation
	store              WorkflowStore
	templates          *TemplateRegistry
	confirmClassifier  ConfirmClassifier // LLM-based intent classification
	allowTempTestPaths bool
}

func NewStateMachine(store WorkflowStore, templates *TemplateRegistry) *StateMachine {
	return &StateMachine{store: store, templates: templates}
}

// SetConfirmClassifier sets the LLM-based confirm intent classifier.
// If nil, falls back to conservative local confirmation handling.
func (m *StateMachine) SetConfirmClassifier(fn ConfirmClassifier) {
	m.confirmClassifier = fn
}

func (m *StateMachine) SetAllowTempTestPaths(allow bool) {
	m.allowTempTestPaths = allow
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
	if !m.allowTempTestPaths && looksLikeTempTestPath(projectPath) {
		return nil, fmt.Errorf("project path cannot be a temp/test directory: %s", projectPath)
	}

	tmpl := m.templates.Get(workflowType)
	if tmpl == nil {
		return nil, fmt.Errorf("unknown workflow type: %s", workflowType)
	}

	// Build phases from template
	phases := make([]Phase, len(tmpl.Phases))
	for i, pt := range tmpl.Phases {
		kind, mutationScope, _ := phaseMetadataSemantics(WorkflowType(workflowType), CanonicalPhaseID(pt.ID))
		status := PhasePending
		if i == 0 {
			status = PhaseRunning
		}
		phases[i] = Phase{
			ID:            pt.ID,
			Name:          pt.Name,
			NeedsConfirm:  pt.NeedsConfirm,
			ToolPolicy:    pt.ToolPolicy,
			ExecMode:      pt.ExecMode,
			Kind:          firstPhaseKind(pt.Kind, kind),
			MutationScope: firstMutationScope(pt.MutationScope, mutationScope),
			Status:        status,
			InputSchema:   pt.InputSchema,
			DependsOnFull: pt.DependsOnFull,
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

	// Suspended workflows require explicit user confirmation before resuming.
	// This prevents stale workflows from hijacking messages after app restart.
	if state.Suspended {
		m.mu.Unlock()
		intent := m.classifyIntent(state, text)
		m.mu.Lock()
		// Re-load in case state changed during LLM call
		state, err = m.store.Load(userID)
		if err != nil || state == nil || state.Status != StatusActive {
			m.mu.Unlock()
			return &HandleResult{Action: ActionPassThrough}, nil
		}
		phase = state.ActivePhase()
		if phase == nil {
			m.mu.Unlock()
			return &HandleResult{Action: ActionPassThrough}, nil
		}

		switch intent {
		case "confirm":
			// User wants to resume — clear suspended flag and run the phase
			state.Suspended = false
			phase.Status = PhaseRunning
			m.store.Save(state)
			m.mu.Unlock()
			return &HandleResult{Action: ActionRunPhase, Phase: phase, State: state}, nil
		case "cancel":
			state.Status = StatusCancelled
			state.Suspended = false
			m.store.Save(state)
			m.mu.Unlock()
			return &HandleResult{Action: ActionCancelled, State: state}, nil
		default:
			// "modify", "unrelated", "" — pass through to normal agent loop
			m.mu.Unlock()
			return &HandleResult{Action: ActionPassThrough}, nil
		}
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
			previous := snapshotWorkflowState(state)
			phase.Status = PhaseRunning
			phase.Output = "" // clear previous output
			if err := m.store.Save(state); err != nil {
				*state = previous
				m.mu.Unlock()
				return nil, fmt.Errorf("save revised workflow state: %w", err)
			}
			m.mu.Unlock()
			return &HandleResult{Action: ActionModify, Phase: phase, ModifyHint: text, State: state}, nil
		case "cancel":
			previous := snapshotWorkflowState(state)
			state.Status = StatusCancelled
			if err := m.store.Save(state); err != nil {
				*state = previous
				m.mu.Unlock()
				return nil, fmt.Errorf("save cancelled workflow state: %w", err)
			}
			m.mu.Unlock()
			return &HandleResult{Action: ActionCancelled, State: state}, nil
		case "cancel_execute":
			previous := snapshotWorkflowState(state)
			state.Status = StatusCancelled
			if err := m.store.Save(state); err != nil {
				*state = previous
				m.mu.Unlock()
				return nil, fmt.Errorf("save cancelled workflow state: %w", err)
			}
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
// Validates that all required fields have non-empty values and submitted
// select/multiselect values match the schema options.
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
		val, exists := formData[f.Name]
		if f.Required && (!exists || val == nil) {
			label := f.Label
			if label == "" {
				label = f.Name
			}
			return fmt.Errorf("必填字段「%s」未填写", label)
		}
		// Check for empty string values
		if f.Required && isEmptyFormValue(val) {
			label := f.Label
			if label == "" {
				label = f.Name
			}
			return fmt.Errorf("必填字段「%s」不能为空", label)
		}
		if !exists || val == nil || isEmptyFormValue(val) {
			continue
		}
		if err := validateFormFieldOptions(f, val); err != nil {
			return err
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

func validateFormFieldOptions(field PhaseInputField, value interface{}) error {
	if field.Type != "select" && field.Type != "multiselect" {
		return nil
	}
	if len(field.Options) == 0 {
		return nil
	}

	allowed := phaseInputOptionSet(field.Options)
	label := field.Label
	if label == "" {
		label = field.Name
	}

	switch field.Type {
	case "select":
		selected, ok := value.(string)
		if !ok {
			return fmt.Errorf("字段「%s」必须从下拉选项中选择", label)
		}
		selected = strings.TrimSpace(selected)
		if !allowed[selected] {
			return fmt.Errorf("字段「%s」的值「%s」不在可选范围内", label, selected)
		}
	case "multiselect":
		selected, ok := formValueStringSlice(value)
		if !ok {
			return fmt.Errorf("字段「%s」必须从多选选项中选择", label)
		}
		for _, item := range selected {
			if !allowed[item] {
				return fmt.Errorf("字段「%s」的值「%s」不在可选范围内", label, item)
			}
		}
	}
	return nil
}

func phaseInputOptionSet(options []PhaseInputOption) map[string]bool {
	allowed := make(map[string]bool, len(options))
	for _, option := range options {
		allowed[option.Value] = true
	}
	return allowed
}

func formValueStringSlice(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		result := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
		return result, true
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			s = strings.TrimSpace(s)
			if s != "" {
				result = append(result, s)
			}
		}
		return result, true
	default:
		return nil, false
	}
}

func isEmptyFormValue(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []string:
		selected, _ := formValueStringSlice(v)
		return len(selected) == 0
	case []interface{}:
		selected, ok := formValueStringSlice(v)
		return ok && len(selected) == 0
	default:
		return false
	}
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
	previous := *state
	previous.Phases = append([]Phase(nil), state.Phases...)
	sanitizedOutput := SanitizePhaseOutputWithKind(phase.ID, phase.Kind, output)
	if err := validatePhaseOutputForCompletion(state.Type, phase.ID, sanitizedOutput); err != nil {
		if phase.NeedsConfirm {
			// For NeedsConfirm phases, validation failure is advisory — record the
			// output and let the user decide whether to accept it. Blocking here
			// creates a dead loop: phase stays Running → next message re-triggers
			// agent loop → same output → same validation failure → infinite cycle.
			log.Printf("[workflow-v2] RecordOutput: validation advisory (phase=%s NeedsConfirm=true): %v", phase.ID, err)
		} else {
			return err
		}
	}
	phase.Output = sanitizedOutput
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
	if err := m.store.Save(state); err != nil {
		*state = previous
		return err
	}
	return nil
}

// SaveExecutionProgress stores intermediate execution output without completing
// or advancing the phase. Used when coding implementation is cancelled or still
// has failed tasks so the user can resume / 重试失败.
func (m *StateMachine) SaveExecutionProgress(userID, output string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil {
		return fmt.Errorf("no active workflow for user %s", userID)
	}
	if state.Status != StatusActive {
		return fmt.Errorf("workflow for user %s is no longer active (status=%s)", userID, state.Status)
	}
	phase := state.ActivePhase()
	if phase == nil {
		return fmt.Errorf("no active phase")
	}
	previous := *state
	previous.Phases = append([]Phase(nil), state.Phases...)
	sanitized := SanitizePhaseOutputWithKind(phase.ID, phase.Kind, output)
	if strings.TrimSpace(sanitized) == "" {
		sanitized = strings.TrimSpace(output)
	}
	phase.Output = sanitized
	// Keep phase open for resume/retry. Executing signals "implementation in progress/paused".
	phase.Status = PhaseExecuting
	state.UpdatedAt = time.Now()
	if err := m.store.Save(state); err != nil {
		*state = previous
		return err
	}
	return nil
}

// MarkPhaseExecuting sets the active phase status to executing without changing
// output. Prefer this over writing the store without the machine lock.
func (m *StateMachine) MarkPhaseExecuting(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(userID)
	if err != nil || state == nil {
		return fmt.Errorf("no active workflow for user %s", userID)
	}
	if state.Status != StatusActive {
		return fmt.Errorf("workflow for user %s is no longer active (status=%s)", userID, state.Status)
	}
	phase := state.ActivePhase()
	if phase == nil {
		return fmt.Errorf("no active phase")
	}
	previous := *state
	previous.Phases = append([]Phase(nil), state.Phases...)
	phase.Status = PhaseExecuting
	state.UpdatedAt = time.Now()
	if err := m.store.Save(state); err != nil {
		*state = previous
		return err
	}
	return nil
}

func validatePhaseOutputForCompletion(workflowType, phaseID, output string) error {
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("phase %s produced empty output", phaseID)
	}
	if workflowType != string(WorkflowGaokaoApplication) || phaseID != GaokaoPhaseFinalPlan {
		return nil
	}
	requiredGroups := [][]string{
		{"总排清单", "志愿填报表", "志愿序号"},
		{"冲"},
		{"稳"},
		{"保"},
		{"学校", "院校名称", "院校代号"},
		{"专业", "专业组"},
		{"办学地点"},
		{"类型", "档位"},
		{"往年最低位次", "最低位次", "等效位次差"},
		{"推荐理由", "推荐分析"},
		{"数据来源", "依据来源"},
	}
	var missing []string
	for _, group := range requiredGroups {
		found := false
		for _, marker := range group {
			if strings.Contains(output, marker) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, strings.Join(group, "/"))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("gaokao final plan output is incomplete; missing required markers: %s", strings.Join(missing, ", "))
	}
	if err := validateGaokaoFinalPlanSourceEvidence(output); err != nil {
		return err
	}
	return nil
}

func validateGaokaoFinalPlanSourceEvidence(output string) error {
	if !containsHTTPURL(output) {
		return fmt.Errorf("gaokao final plan output is incomplete; missing source URLs")
	}
	// Only check source URL requirement within the "推荐分析表" section or
	// legacy "总排清单/冲/稳/保" sections. Skip "志愿填报表" section rows
	// which contain enrollment codes, not source URLs.
	inRecommendationSection := false
	inVolunteerFormSection := false
	var badRows []string
	for _, line := range strings.Split(output, "\n") {
		row := strings.TrimSpace(line)
		// Track section boundaries
		if strings.Contains(row, "志愿填报表") || strings.Contains(row, "志愿序号") && strings.Contains(row, "院校代号") {
			inVolunteerFormSection = true
			inRecommendationSection = false
			continue
		}
		if strings.Contains(row, "推荐分析表") || strings.Contains(row, "总排清单") {
			inRecommendationSection = true
			inVolunteerFormSection = false
			continue
		}
		if (strings.HasPrefix(row, "## ") || strings.HasPrefix(row, "# ")) && !strings.Contains(row, "冲") && !strings.Contains(row, "稳") && !strings.Contains(row, "保") {
			if !strings.Contains(row, "推荐") && !strings.Contains(row, "总排") {
				inRecommendationSection = false
				inVolunteerFormSection = false
			}
		}
		// Only validate source URLs in recommendation sections
		if inVolunteerFormSection || !inRecommendationSection {
			continue
		}
		if !strings.HasPrefix(row, "|") || !strings.HasSuffix(row, "|") {
			continue
		}
		cells := markdownTableCells(row)
		if len(cells) < 6 || isMarkdownSeparatorCells(cells) || isGaokaoFinalPlanHeaderRow(cells) {
			continue
		}
		rowText := strings.Join(cells, " ")
		if strings.Contains(rowText, "待核验") ||
			strings.Contains(rowText, "无法核验") ||
			strings.Contains(rowText, "来源URL") ||
			strings.Contains(rowText, "无来源") ||
			!containsHTTPURL(rowText) {
			badRows = append(badRows, summarizeGaokaoRecommendationRow(cells))
		}
	}
	if len(badRows) > 0 {
		return fmt.Errorf("gaokao final plan output is incomplete; recommendation rows missing verified source URLs: %s", strings.Join(badRows, ", "))
	}
	if err := validateGaokaoFinalPlanSectionRows(output); err != nil {
		return err
	}
	return nil
}

func validateGaokaoFinalPlanSectionRows(output string) error {
	counts := map[string]int{
		"总排清单": 0,
		"冲":    0,
		"稳":    0,
		"保":    0,
	}
	currentSection := ""
	for _, line := range strings.Split(output, "\n") {
		row := strings.TrimSpace(line)
		if row == "" {
			continue
		}
		if !strings.HasPrefix(row, "|") {
			if section := gaokaoFinalPlanSectionName(row); section != "" {
				currentSection = section
			}
			continue
		}
		if !strings.HasSuffix(row, "|") {
			continue
		}
		cells := markdownTableCells(row)
		if isMarkdownSeparatorCells(cells) || isGaokaoFinalPlanHeaderRow(cells) {
			continue
		}
		// New format: "档位" column in table cells (志愿填报表 or 推荐分析表)
		rowText := strings.Join(cells, " ")
		if strings.Contains(rowText, "冲") {
			counts["冲"]++
		}
		if strings.Contains(rowText, "稳") {
			counts["稳"]++
		}
		if strings.Contains(rowText, "保") {
			counts["保"]++
		}
		// Legacy format: section-based counting
		if currentSection != "" {
			if len(cells) >= 6 {
				if _, ok := counts[currentSection]; ok {
					counts[currentSection]++
				}
			}
		}
		// 总排清单: any table data row in that section counts, or row with "志愿序号"
		if currentSection == "总排清单" || strings.Contains(rowText, "志愿") {
			counts["总排清单"]++
		}
	}
	var missing []string
	for _, section := range []string{"冲", "稳", "保"} {
		if counts[section] == 0 {
			missing = append(missing, section)
		}
	}
	// 总排清单 OR 志愿填报表 — at least one must have rows
	if counts["总排清单"] == 0 && counts["冲"]+counts["稳"]+counts["保"] == 0 {
		missing = append(missing, "总排清单/志愿填报表")
	}
	if len(missing) > 0 {
		return fmt.Errorf("gaokao final plan output is incomplete; missing recommendation rows in sections: %s", strings.Join(missing, ", "))
	}
	return nil
}

func gaokaoFinalPlanSectionName(line string) string {
	heading := strings.TrimSpace(strings.TrimLeft(line, "# "))
	heading = strings.Trim(heading, " ：:、-")
	if strings.Contains(heading, "总排清单") {
		return "总排清单"
	}
	if strings.HasPrefix(heading, "冲") {
		return "冲"
	}
	if strings.HasPrefix(heading, "稳") {
		return "稳"
	}
	if strings.HasPrefix(heading, "保") {
		return "保"
	}
	return ""
}

func markdownTableCells(row string) []string {
	parts := strings.Split(strings.Trim(row, "|"), "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func isMarkdownSeparatorCells(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		trimmed := strings.TrimSpace(cell)
		if trimmed == "" {
			return false
		}
		for _, r := range trimmed {
			if r != '-' && r != ':' && r != ' ' {
				return false
			}
		}
	}
	return true
}

func isGaokaoFinalPlanHeaderRow(cells []string) bool {
	rowText := strings.Join(cells, " ")
	return strings.Contains(rowText, "学校") &&
		strings.Contains(rowText, "专业") &&
		strings.Contains(rowText, "推荐理由")
}

func summarizeGaokaoRecommendationRow(cells []string) string {
	if len(cells) >= 2 {
		return strings.TrimSpace(cells[0] + "/" + cells[1])
	}
	if len(cells) == 1 {
		return cells[0]
	}
	return "unknown row"
}

func containsHTTPURL(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
}

func SanitizePhaseOutput(phaseID, output string) string {
	return SanitizePhaseOutputWithKind(phaseID, PhaseKindUnknown, output)
}

// SanitizePhaseOutputWithKind sanitizes phase output using PhaseKind semantics.
// When kind is known (non-Unknown), it drives the decision; otherwise falls
// back to phaseID string matching for backward compatibility.
func SanitizePhaseOutputWithKind(phaseID string, kind PhaseKind, output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	if calls, malformed := llm.ParseContentToolCallsDetailed(output); len(calls) > 0 {
		// Kind-based decision takes priority over phaseID string matching.
		if kind != PhaseKindUnknown && !ShouldExtractDocFromToolCalls(kind) {
			return llm.StripAllExtra(output)
		}
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

// ShouldExtractDocFromToolCalls returns true if a phase's tool calls may
// contain document content worth capturing (e.g. write_file producing a
// requirements .md). Execution/artifact/review phases use tool calls for
// actions (code generation, file generation, testing), not document authoring.
func ShouldExtractDocFromToolCalls(kind PhaseKind) bool {
	switch kind {
	case PhaseKindDocumentPlanning, PhaseKindCodePlanning, PhaseKindUnknown:
		return true
	default:
		// PhaseKindArtifactGeneration, PhaseKindExecution, PhaseKindReview,
		// PhaseKindOpsExecution, PhaseKindOpsRiskPolicy — tool calls are actions.
		return false
	}
}

func SanitizePhaseOutputFromToolCalls(phaseID string, calls []llm.ToolCall) string {
	// Legacy hardcoded list kept as fallback for phases that don't yet have
	// Kind metadata propagated through the RecordOutput call path.
	// Once all callers pass Kind, this switch can be removed.
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

// ListAllStoredUserIDs returns all user IDs that have workflow state in the store.
// Used by startup cleanup to discover and cancel stale workflows across all
// users/tabs (desktop-user, desktop-user:{path}, etc.).
func (m *StateMachine) ListAllStoredUserIDs() ([]string, error) {
	return m.store.ListAllUserIDs()
}

// DeleteState removes workflow state for a user from the store entirely.
// Used as a fallback when Cancel (which does Load + mutate + Save) fails due to
// store write errors. Deleting the row is simpler than updating it.
func (m *StateMachine) DeleteState(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Delete(userID)
}

// SuspendWorkflow marks an active workflow as suspended. Suspended workflows
// remain Active (preserving phase outputs) but HandleInput returns PassThrough
// for unrelated messages instead of auto-executing the current phase.
// The user can resume by sending a "continue" intent.
func (m *StateMachine) SuspendWorkflow(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(userID)
	if err != nil || state == nil || state.Status != StatusActive {
		return nil
	}
	state.Suspended = true
	state.UpdatedAt = time.Now()
	return m.store.Save(state)
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
		previous := *state
		state.Status = StatusCancelled
		state.UpdatedAt = time.Now()
		if err := m.store.Save(state); err != nil {
			*state = previous
			return nil, fmt.Errorf("save cancelled workflow state: %w", err)
		}
		return &HandleResult{Action: ActionCancelled, State: state}, nil

	case "switch_task":
		// Cancel workflow so caller can re-route the message.
		previous := *state
		state.Status = StatusCancelled
		state.UpdatedAt = time.Now()
		if err := m.store.Save(state); err != nil {
			*state = previous
			return nil, fmt.Errorf("save cancelled workflow state: %w", err)
		}
		return &HandleResult{Action: ActionCancelAndExecute, State: state}, nil

	default:
		// "supplement", "other", or anything else — reopen phase for revision.
		previous := *state
		previous.Phases = append([]Phase(nil), state.Phases...)
		phase.Status = PhaseRunning
		phase.Output = "" // clear previous output for re-generation
		state.UpdatedAt = time.Now()
		if err := m.store.Save(state); err != nil {
			*state = previous
			return nil, fmt.Errorf("save revised workflow state: %w", err)
		}
		return &HandleResult{Action: ActionModify, Phase: phase, ModifyHint: feedback, State: state}, nil
	}
}

// advanceLocked moves to the next phase. Caller must hold m.mu.
// Preserves PhaseSkipped status (set by ApplyReviewIntent "skip") rather than
// unconditionally overwriting to PhaseCompleted.
// Rolls back in-memory mutations if store.Save fails, so the returned
// HandleResult always references persisted state.
func (m *StateMachine) advanceLocked(state *WorkflowState) (*HandleResult, error) {
	previous := snapshotWorkflowState(state)
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
			*state = previous
			return nil, fmt.Errorf("save workflow state: %w", err)
		}
		return &HandleResult{Action: ActionConfirmed, State: state}, nil
	}

	// Start next phase
	next := state.ActivePhase()
	next.Status = PhaseRunning
	state.UpdatedAt = time.Now()
	if err := m.store.Save(state); err != nil {
		*state = previous
		return nil, fmt.Errorf("save workflow state: %w", err)
	}
	return &HandleResult{Action: ActionRunPhase, Phase: next, State: state}, nil
}

// snapshotWorkflowState copies the fields mutated during a state transition.
// WorkflowStore implementations used in tests may retain pointers, so rollback
// must restore both the state header and the phase slice after a failed write.
func snapshotWorkflowState(state *WorkflowState) WorkflowState {
	snapshot := *state
	snapshot.Phases = append([]Phase(nil), state.Phases...)
	return snapshot
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
