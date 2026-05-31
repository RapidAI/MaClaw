package workflow

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

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
	workflows     map[string]*WorkflowState // userID -> active workflow
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

// getLang returns the current user-facing language by pulling from callbacks.
// Falls back to "zh" when callbacks are not set.
func (e *WorkflowEngine) getLang() string {
	if e.callbacks != nil {
		if lang := e.callbacks.GetLang(); lang != "" {
			return i18n.NormalizeLang(lang)
		}
	}
	return "zh"
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

// WorkflowStartOptions carries durable context that belongs to the workflow
// state from its first persisted snapshot.
type WorkflowStartOptions struct {
	ProjectPath string
}

// StartWorkflow creates and starts a new workflow for the user based on the
// given StructuredIntent. It validates that the user has no active workflow,
// matches a template by intent.Category, creates a WorkflowState at phase 0,
// persists it, and notifies callbacks.
func (e *WorkflowEngine) StartWorkflow(userID string, intent StructuredIntent) (*WorkflowState, error) {
	return e.StartWorkflowWithOptions(userID, intent, WorkflowStartOptions{})
}

// StartWorkflowWithOptions is the authoritative workflow-start transition when
// the caller has durable execution context such as the project root.
func (e *WorkflowEngine) StartWorkflowWithOptions(userID string, intent StructuredIntent, options WorkflowStartOptions) (*WorkflowState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Enforce single active workflow per user.
	if ws, ok := e.workflows[userID]; ok && ws != nil && ws.Status == WorkflowActive {
		return nil, fmt.Errorf("user already has an active workflow (%s); complete or cancel it first", ws.Type)
	}

	// Match template by intent category.
	if e.registry == nil {
		return nil, fmt.Errorf("workflow registry not initialized")
	}
	tmpl := e.registry.Match(intent.Category)
	if tmpl == nil {
		return nil, fmt.Errorf("workflow template not found for category %s", intent.Category)
	}
	if len(tmpl.Phases) == 0 {
		return nil, fmt.Errorf("workflow template %s has no phases", intent.Category)
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
		ProjectPath:  strings.TrimSpace(options.ProjectPath),
	}

	if e.store != nil {
		if err := e.store.SaveWorkflowState(state); err != nil {
			return nil, fmt.Errorf("save started workflow state: %w", err)
		}
	}

	e.workflows[userID] = state

	// Notify callbacks (best-effort, outside lock would be ideal but
	// callbacks are expected to be non-blocking).
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, state)
	}

	return state, nil
}

// SetProjectPath updates the durable project context for an active workflow.
// It is used when the user changes the workflow working directory after start;
// the value is persisted before the in-memory state is considered updated.
func (e *WorkflowEngine) SetProjectPath(userID, projectPath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return nil
	}
	trimmed := strings.TrimSpace(projectPath)
	if ws.ProjectPath == trimmed {
		return nil
	}

	previousProjectPath := ws.ProjectPath
	previousUpdatedAt := ws.UpdatedAt
	ws.ProjectPath = trimmed
	ws.UpdatedAt = time.Now()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			ws.ProjectPath = previousProjectPath
			ws.UpdatedAt = previousUpdatedAt
			return fmt.Errorf("save workflow project path: %w", err)
		}
	}
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, ws)
	}
	return nil
}

// HandleInput processes user input within an active workflow.
// Free-form review decisions are intentionally not parsed here. When a phase is
// awaiting review, the engine returns PendingConfirm so the caller can classify
// the user's intent and feed the typed ReviewIntent to ApplyReviewIntent.
func (e *WorkflowEngine) HandleInput(userID, text string) (*WorkflowResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws, ok := e.workflows[userID]
	if !ok || ws == nil || ws.Status != WorkflowActive {
		return nil, fmt.Errorf("user has no active workflow")
	}

	// Look up the current phase template.
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, fmt.Errorf("workflow template or phase index is invalid")
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase {
		return nil, fmt.Errorf("workflow current phase is inconsistent with template")
	}

	trimmed := strings.TrimSpace(text)
	if isWorkflowCancelCommand(trimmed) {
		return e.cancelWorkflowLocked(userID, ws, i18n.T(i18n.MsgWorkflowCancelled, e.getLang()))
	}

	// 0. Handle document-required workflows waiting for user input.
	// When the template declares RequiresInput and the user hasn't provided
	// the document yet, we gate phase execution until we detect that the
	// user has supplied content (file attachment or substantial text).
	if ws.IsWaitingForInput(tmpl) {
		if trimmed != "" {
			// User has provided document text: record it as durable workflow input.
			if err := e.receiveInputLocked(ws, &WorkflowInputPayload{Text: trimmed, ReceivedAt: time.Now()}); err != nil {
				return nil, err
			}
			// Fall through to normal phase execution below.
		} else {
			// Still waiting; remind the user to upload the document.
			req := tmpl.RequiresInput
			hint := req.Description
			if len(req.FileTypes) > 0 {
				hint += i18n.Tf(i18n.MsgWorkflowInputFormats, e.getLang(), strings.Join(req.FileTypes, ", "))
			}
			if req.AcceptText {
				hint += i18n.T(i18n.MsgWorkflowInputPasteAlt, e.getLang())
			}
			return &WorkflowResponse{
				Text:         i18n.Tf(i18n.MsgWorkflowInputWaiting, e.getLang(), hint),
				RunAgentLoop: false,
			}, nil
		}
	}

	_, hasOutput := ws.PhaseOutputs[ws.CurrentPhase]
	if phase.NeedsConfirm && hasOutput {
		log.Printf("[WorkflowEngine] pending review: user=%s phase=%s msg=%q",
			userID, ws.CurrentPhase, truncateForLog(trimmed, 50))
		return &WorkflowResponse{
			PendingReview:  true,
			PendingConfirm: true,
		}, nil
	}

	// Default: normal phase input; run agent loop with phase prompt.

	// Check if this phase requires structured form input and the user
	// has not satisfied that gate yet. A submitted or explicitly skipped
	// form falls through to normal phase execution.
	if phase.InputSchema != nil && !ws.phaseFormGateSatisfied() {
		return &WorkflowResponse{
			ShowForm:   true,
			FormSchema: phase.InputSchema.Clone(),
		}, nil
	}

	phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)

	// When the phase has no output yet, this is the first execution request
	// (e.g. the user confirms and the system needs to generate the document).
	return &WorkflowResponse{
		Text:         "",
		PhasePrompt:  phasePrompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
		DefaultInput: true,
	}, nil
}

// SubmitPhaseForm receives the user's structured form submission for the current
// phase. It stores the form data in WorkflowState and triggers the agent loop
// with the form context injected into the PhasePrompt.
func (e *WorkflowEngine) SubmitPhaseForm(userID string, formData map[string]interface{}) (*WorkflowResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, fmt.Errorf("workflow template or phase index is invalid")
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase {
		return nil, fmt.Errorf("workflow current phase is inconsistent with template")
	}
	if ws.IsWaitingForInput(tmpl) {
		return nil, fmt.Errorf("workflow is still waiting for required input")
	}
	if ws.PendingReviewPhaseID != "" {
		return nil, fmt.Errorf("workflow phase is awaiting review")
	}
	if phase.InputSchema == nil {
		return nil, fmt.Errorf("current workflow phase does not accept structured form input")
	}
	if ws.phaseFormGateSatisfied() {
		return nil, fmt.Errorf("workflow phase form has already been submitted or skipped")
	}
	if missing := missingRequiredPhaseInputFields(phase.InputSchema, formData); len(missing) > 0 {
		return nil, fmt.Errorf("missing required workflow form fields: %s", strings.Join(missing, ", "))
	}
	if invalid := invalidPhaseInputFields(phase.InputSchema, formData); len(invalid) > 0 {
		return nil, fmt.Errorf("invalid workflow form fields: %s", strings.Join(invalid, ", "))
	}

	previousFormData := ws.PhaseFormData
	previousPhaseFormSubmitted := ws.PhaseFormSubmitted
	previousPhaseFormSkipped := ws.PhaseFormSkipped
	previousUpdatedAt := ws.UpdatedAt
	ws.PhaseFormData = cloneWorkflowMap(formData)
	ws.PhaseFormSubmitted = true
	ws.PhaseFormSkipped = false
	ws.UpdatedAt = time.Now()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			ws.PhaseFormData = previousFormData
			ws.PhaseFormSubmitted = previousPhaseFormSubmitted
			ws.PhaseFormSkipped = previousPhaseFormSkipped
			ws.UpdatedAt = previousUpdatedAt
			return nil, fmt.Errorf("save workflow form state: %w", err)
		}
	}

	phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)
	return &WorkflowResponse{
		PhasePrompt:  phasePrompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
	}, nil
}

// SubmitInputPayload records the document/file input required by the current
// workflow and returns the first phase response. This is the authoritative input
// transition for document-driven workflows; GUI/TUI callers should use it when
// the user uploads files or provides pasted source material.
func (e *WorkflowEngine) SubmitInputPayload(userID string, payload *WorkflowInputPayload) (*WorkflowResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, fmt.Errorf("workflow template or phase index is invalid")
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase {
		return nil, fmt.Errorf("workflow current phase is inconsistent with template")
	}
	if !ws.IsWaitingForInput(tmpl) {
		return nil, fmt.Errorf("workflow is not waiting for input")
	}
	if payload == nil || (strings.TrimSpace(payload.Text) == "" && len(payload.Attachments) == 0) {
		return &WorkflowResponse{Text: i18n.Tf(i18n.MsgWorkflowInputWaiting, e.getLang(), tmpl.RequiresInput.Description), RunAgentLoop: false}, nil
	}

	if err := e.receiveInputLocked(ws, payload); err != nil {
		return nil, err
	}
	if phase.InputSchema != nil && !ws.phaseFormGateSatisfied() {
		return &WorkflowResponse{
			ShowForm:   true,
			FormSchema: phase.InputSchema.Clone(),
		}, nil
	}
	return &WorkflowResponse{
		PhasePrompt:  BuildPhaseSystemPrompt(ws, phase, e.registry),
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
	}, nil
}

// receiveInputLocked records input evidence. Must be called with e.mu held.
func (e *WorkflowEngine) receiveInputLocked(ws *WorkflowState, payload *WorkflowInputPayload) error {
	if payload == nil {
		payload = &WorkflowInputPayload{}
	}
	if payload.ReceivedAt.IsZero() {
		payload.ReceivedAt = time.Now()
	}
	previousInputReceived := ws.InputReceived
	previousInputPayload := ws.InputPayload
	previousUpdatedAt := ws.UpdatedAt
	ws.InputReceived = true
	ws.InputPayload = payload.Clone()
	ws.UpdatedAt = time.Now()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			ws.InputReceived = previousInputReceived
			ws.InputPayload = previousInputPayload
			ws.UpdatedAt = previousUpdatedAt
			return fmt.Errorf("save workflow input state: %w", err)
		}
	}
	return nil
}

func missingRequiredPhaseInputFields(schema *PhaseInputSchema, formData map[string]interface{}) []string {
	if schema == nil {
		return nil
	}
	var missing []string
	for _, field := range schema.Fields {
		if !field.Required {
			continue
		}
		value, ok := formData[field.Name]
		if !ok || isEmptyPhaseInputValue(value) {
			label := strings.TrimSpace(field.Label)
			if label == "" {
				label = field.Name
			}
			missing = append(missing, label)
		}
	}
	return missing
}

func invalidPhaseInputFields(schema *PhaseInputSchema, formData map[string]interface{}) []string {
	if schema == nil {
		return nil
	}
	allowed := make(map[string]PhaseInputField, len(schema.Fields))
	for _, field := range schema.Fields {
		allowed[field.Name] = field
	}

	var invalid []string
	for name, value := range formData {
		field, ok := allowed[name]
		if !ok {
			invalid = append(invalid, fmt.Sprintf("%s (unknown field)", name))
			continue
		}
		if isEmptyPhaseInputValue(value) {
			continue
		}
		label := strings.TrimSpace(field.Label)
		if label == "" {
			label = field.Name
		}
		if err := validatePhaseInputField(field, value); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (%v)", label, err))
		}
	}
	return invalid
}

func validatePhaseInputField(field PhaseInputField, value interface{}) error {
	typ := strings.ToLower(strings.TrimSpace(field.Type))
	switch typ {
	case "", "text", "textarea", "date", "file":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be text")
		}
		return validateStringPhaseInput(field, s)
	case "select":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be a selected value")
		}
		if err := validateStringPhaseInput(field, s); err != nil {
			return err
		}
		if len(field.Options) > 0 && !phaseInputOptionAllowed(field.Options, s) {
			return fmt.Errorf("must be one of the allowed options")
		}
	case "multiselect":
		values, ok := phaseInputStringSlice(value)
		if !ok {
			return fmt.Errorf("must be a list of selected values")
		}
		if len(field.Options) > 0 {
			for _, item := range values {
				if !phaseInputOptionAllowed(field.Options, item) {
					return fmt.Errorf("%q is not an allowed option", item)
				}
			}
		}
	case "number":
		n, ok := phaseInputNumber(value)
		if !ok {
			return fmt.Errorf("must be a number")
		}
		if field.Min != nil && n < *field.Min {
			return fmt.Errorf("must be at least %g", *field.Min)
		}
		if field.Max != nil && n > *field.Max {
			return fmt.Errorf("must be at most %g", *field.Max)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("must be true or false")
		}
	default:
		return nil
	}
	return nil
}

func validateStringPhaseInput(field PhaseInputField, value string) error {
	if field.MinLength != nil && len([]rune(value)) < *field.MinLength {
		return fmt.Errorf("must be at least %d characters", *field.MinLength)
	}
	if field.MaxLength != nil && len([]rune(value)) > *field.MaxLength {
		return fmt.Errorf("must be at most %d characters", *field.MaxLength)
	}
	if strings.TrimSpace(field.Pattern) != "" {
		re, err := regexp.Compile(field.Pattern)
		if err != nil {
			return fmt.Errorf("has invalid validation pattern")
		}
		if !re.MatchString(value) {
			return fmt.Errorf("does not match the required format")
		}
	}
	return nil
}

func phaseInputOptionAllowed(options []PhaseInputOption, value string) bool {
	for _, option := range options {
		if value == option.Value {
			return true
		}
	}
	return false
}

func phaseInputStringSlice(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...), true
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			items = append(items, s)
		}
		return items, true
	default:
		return nil, false
	}
}

func phaseInputNumber(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}
func isEmptyPhaseInputValue(value interface{}) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []string:
		return len(v) == 0
	case []interface{}:
		return len(v) == 0
	default:
		return false
	}
}

func (ws *WorkflowState) phaseFormGateSatisfied() bool {
	return ws != nil && (ws.PhaseFormSubmitted || ws.PhaseFormSkipped || len(ws.PhaseFormData) != 0)
}

// SkipPhaseForm marks the current phase's form as skipped (user dismissed the
// AG UI form). Subsequent HandleInput calls will fall through to the normal
// agent loop (natural language interaction) instead of re-showing the form.
func (e *WorkflowEngine) SkipPhaseForm(userID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if ws.IsWaitingForInput(tmpl) || ws.PendingReviewPhaseID != "" || phase.InputSchema == nil || ws.phaseFormGateSatisfied() {
		return nil
	}
	previousFormData := ws.PhaseFormData
	previousPhaseFormSubmitted := ws.PhaseFormSubmitted
	previousPhaseFormSkipped := ws.PhaseFormSkipped
	previousUpdatedAt := ws.UpdatedAt

	// Mark the form gate as explicitly skipped. PhaseFormData remains reserved
	// for user-submitted values, so prompts and persistence do not need to
	// special-case a synthetic control key.
	ws.PhaseFormData = nil
	ws.PhaseFormSubmitted = false
	ws.PhaseFormSkipped = true
	ws.UpdatedAt = time.Now()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			ws.PhaseFormData = previousFormData
			ws.PhaseFormSubmitted = previousPhaseFormSubmitted
			ws.PhaseFormSkipped = previousPhaseFormSkipped
			ws.UpdatedAt = previousUpdatedAt
			return fmt.Errorf("save skipped phase form state: %w", err)
		}
	}
	return nil
}

// AdvancePhase is the public entry point for advancing the workflow to the
// next phase. Review-state transitions should normally use ApplyReviewIntent so
// user free-form text is classified before it can affect the state machine.
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
	if ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) || tmpl.Phases[ws.PhaseIndex].ID != ws.CurrentPhase {
		return nil, fmt.Errorf("workflow current phase is inconsistent with template")
	}
	if ws.PendingReviewPhaseID != "" {
		return nil, fmt.Errorf("workflow phase is awaiting review; apply a classified review intent instead of advancing directly")
	}
	return e.advancePhase(userID, ws, tmpl)
}

func (e *WorkflowEngine) cancelWorkflowLocked(userID string, ws *WorkflowState, text string) (*WorkflowResponse, error) {
	if ws == nil || ws.Status != WorkflowActive {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}
	previousStatus := ws.Status
	previousPendingReviewPhaseID := ws.PendingReviewPhaseID
	previousPendingReviewRevisionRequested := ws.PendingReviewRevisionRequested
	previousUpdatedAt := ws.UpdatedAt
	ws.Status = WorkflowCancelled
	ws.PendingReviewPhaseID = ""
	ws.PendingReviewRevisionRequested = false
	ws.UpdatedAt = time.Now()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			ws.Status = previousStatus
			ws.PendingReviewPhaseID = previousPendingReviewPhaseID
			ws.PendingReviewRevisionRequested = previousPendingReviewRevisionRequested
			ws.UpdatedAt = previousUpdatedAt
			return nil, fmt.Errorf("save cancelled workflow state: %w", err)
		}
	}
	delete(e.workflows, userID)
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, ws)
	}
	return &WorkflowResponse{
		Text:         text,
		RunAgentLoop: false,
		Complete:     true,
	}, nil
}

func isWorkflowCancelCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "/cancel", "cancel", "abort", "stop", "quit", "\u53d6\u6d88", "\u505c\u6b62", "\u653e\u5f03", "\u9000\u51fa":
		return true
	default:
		return false
	}
}

// ApplyReviewIntent applies a classified review intent to the active workflow.
// This is the only engine entry point that may advance, regenerate, skip, or
// cancel a NeedsConfirm review state. Free-form user text must be classified by
// the caller before this method is invoked.
func (e *WorkflowEngine) ApplyReviewIntent(userID string, intent ReviewIntent, feedback string) (*WorkflowResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, fmt.Errorf("workflow template or phase index is invalid")
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase {
		return nil, fmt.Errorf("workflow current phase is inconsistent with template")
	}
	if ws.PendingReviewPhaseID == "" || ws.PendingReviewPhaseID != ws.CurrentPhase {
		return nil, fmt.Errorf("workflow is not awaiting review")
	}

	switch intent {
	case ReviewIntentConfirm:
		if output := ws.PhaseOutputs[ws.CurrentPhase]; strings.TrimSpace(output) == "" {
			return e.requestReviewRegenerationLocked(ws, phase, feedback)
		}
		return e.advancePhase(userID, ws, tmpl)

	case ReviewIntentSupplement:
		return e.requestReviewRegenerationLocked(ws, phase, feedback)

	case ReviewIntentSkip:
		if !phase.CanSkip {
			return &WorkflowResponse{
				Text:         i18n.Tf(i18n.MsgWorkflowPhaseCannotSkip, e.getLang(), phase.Name),
				RunAgentLoop: false,
			}, nil
		}
		return e.advancePhase(userID, ws, tmpl)

	case ReviewIntentCancel:
		return e.cancelWorkflowLocked(userID, ws, i18n.T(i18n.MsgWorkflowCancelled, e.getLang()))

	case ReviewIntentSwitchTask:
		return e.cancelWorkflowLocked(userID, ws, "")

	case ReviewIntentOther:
		return &WorkflowResponse{
			Text:         i18n.Tf(i18n.MsgWorkflowAwaitingReview, e.getLang(), phase.Name),
			RunAgentLoop: false,
		}, nil

	default:
		return nil, fmt.Errorf("unknown review intent %q", intent)
	}
}

func (e *WorkflowEngine) requestReviewRegenerationLocked(ws *WorkflowState, phase *PhaseTemplate, feedback string) (*WorkflowResponse, error) {
	previousRequested := ws.PendingReviewRevisionRequested
	previousUpdatedAt := ws.UpdatedAt
	ws.PendingReviewRevisionRequested = true
	ws.UpdatedAt = time.Now()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			ws.PendingReviewRevisionRequested = previousRequested
			ws.UpdatedAt = previousUpdatedAt
			return nil, fmt.Errorf("save review regeneration request: %w", err)
		}
	}
	return e.regenerateCurrentPhaseResponse(ws, phase, feedback), nil
}

func cloneQualityGateResults(src map[string]*QualityGateResult) map[string]*QualityGateResult {
	if src == nil {
		return nil
	}
	cp := make(map[string]*QualityGateResult, len(src))
	for k, v := range src {
		if v == nil {
			cp[k] = nil
			continue
		}
		gate := *v
		gate.Items = append([]GateCheckItem(nil), v.Items...)
		cp[k] = &gate
	}
	return cp
}

// ReopenPhaseForRevision rewinds an active workflow to a previous NeedsConfirm
// phase and requests regeneration of that phase's deliverable. It is used when
// a later execution boundary discovers that an earlier reviewed artifact is not
// structurally executable and must be repaired before continuing.
func (e *WorkflowEngine) ReopenPhaseForRevision(userID, phaseID, feedback string) (*WorkflowResponse, error) {
	e.mu.Lock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("workflow template not found for type %s", ws.Type)
	}
	targetIndex, ok := workflowTemplatePhaseIndex(tmpl, phaseID)
	if !ok || targetIndex < 0 || targetIndex >= len(tmpl.Phases) {
		e.mu.Unlock()
		return nil, fmt.Errorf("workflow phase %s not found", phaseID)
	}
	phase := &tmpl.Phases[targetIndex]
	if !phase.NeedsConfirm {
		e.mu.Unlock()
		return nil, fmt.Errorf("workflow phase %s is not reviewable", phaseID)
	}
	if targetIndex > ws.PhaseIndex {
		e.mu.Unlock()
		return nil, fmt.Errorf("workflow phase %s is ahead of current phase %s", phaseID, ws.CurrentPhase)
	}

	previousPhaseIndex := ws.PhaseIndex
	previousCurrentPhase := ws.CurrentPhase
	previousPendingReviewPhaseID := ws.PendingReviewPhaseID
	previousPendingReviewRevisionRequested := ws.PendingReviewRevisionRequested
	previousPhaseFormData := ws.PhaseFormData
	previousPhaseFormSubmitted := ws.PhaseFormSubmitted
	previousPhaseFormSkipped := ws.PhaseFormSkipped
	previousOutputs := cloneStringMap(ws.PhaseOutputs)
	previousGates := cloneQualityGateResults(ws.GateResults)
	previousUpdatedAt := ws.UpdatedAt

	rollback := func() {
		ws.PhaseIndex = previousPhaseIndex
		ws.CurrentPhase = previousCurrentPhase
		ws.PendingReviewPhaseID = previousPendingReviewPhaseID
		ws.PendingReviewRevisionRequested = previousPendingReviewRevisionRequested
		ws.PhaseFormData = previousPhaseFormData
		ws.PhaseFormSubmitted = previousPhaseFormSubmitted
		ws.PhaseFormSkipped = previousPhaseFormSkipped
		ws.PhaseOutputs = previousOutputs
		ws.GateResults = previousGates
		ws.UpdatedAt = previousUpdatedAt
	}

	for i := targetIndex + 1; i < len(tmpl.Phases); i++ {
		delete(ws.PhaseOutputs, tmpl.Phases[i].ID)
		delete(ws.GateResults, tmpl.Phases[i].ID)
	}
	ws.PhaseIndex = targetIndex
	ws.CurrentPhase = phase.ID
	ws.PendingReviewPhaseID = phase.ID
	ws.PendingReviewRevisionRequested = true
	ws.PhaseFormData = nil
	ws.PhaseFormSubmitted = false
	ws.PhaseFormSkipped = false
	ws.UpdatedAt = time.Now()

	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			rollback()
			e.mu.Unlock()
			return nil, fmt.Errorf("save reopened workflow phase: %w", err)
		}
	}
	stateForCallback := ws.Clone()
	callbacks := e.callbacks
	resp := e.regenerateCurrentPhaseResponse(ws, phase, feedback)
	e.mu.Unlock()

	if callbacks != nil {
		_ = callbacks.EmitPhaseUpdate(userID, stateForCallback)
	}
	return resp, nil
}

func (e *WorkflowEngine) regenerateCurrentPhaseResponse(ws *WorkflowState, phase *PhaseTemplate, feedback string) *WorkflowResponse {
	phasePrompt := BuildPhaseSystemPrompt(ws, phase, e.registry)
	if strings.TrimSpace(feedback) != "" {
		phasePrompt += fmt.Sprintf("\n\nUser supplement/change request for this review round:\n%s\n\nRegenerate the current phase deliverable incorporating this feedback. Do not advance to the next phase.", feedback)
	}
	return &WorkflowResponse{
		Text:         i18n.Tf(i18n.MsgWorkflowSupplementAck, e.getLang(), phase.Name),
		PhasePrompt:  phasePrompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
	}
}

// advancePhase moves the workflow to the next phase, or marks it completed
// if the current phase is the last one. Must be called with e.mu held.
func (e *WorkflowEngine) advancePhase(userID string, ws *WorkflowState, tmpl *WorkflowTemplate) (*WorkflowResponse, error) {
	nextIndex := ws.PhaseIndex + 1
	now := time.Now()
	previousStatus := ws.Status
	previousPhaseIndex := ws.PhaseIndex
	previousCurrentPhase := ws.CurrentPhase
	previousPendingReviewPhaseID := ws.PendingReviewPhaseID
	previousPendingReviewRevisionRequested := ws.PendingReviewRevisionRequested
	previousPhaseFormData := ws.PhaseFormData
	previousPhaseFormSubmitted := ws.PhaseFormSubmitted
	previousPhaseFormSkipped := ws.PhaseFormSkipped
	previousUpdatedAt := ws.UpdatedAt

	rollback := func() {
		ws.Status = previousStatus
		ws.PhaseIndex = previousPhaseIndex
		ws.CurrentPhase = previousCurrentPhase
		ws.PendingReviewPhaseID = previousPendingReviewPhaseID
		ws.PendingReviewRevisionRequested = previousPendingReviewRevisionRequested
		ws.PhaseFormData = previousPhaseFormData
		ws.PhaseFormSubmitted = previousPhaseFormSubmitted
		ws.PhaseFormSkipped = previousPhaseFormSkipped
		ws.UpdatedAt = previousUpdatedAt
		e.workflows[userID] = ws
	}

	ws.PendingReviewPhaseID = ""
	ws.PendingReviewRevisionRequested = false
	ws.PhaseFormData = nil
	ws.PhaseFormSubmitted = false
	ws.PhaseFormSkipped = false

	if nextIndex >= len(tmpl.Phases) {
		// Last phase; mark workflow completed.
		ws.Status = WorkflowCompleted
		ws.UpdatedAt = now

		if e.store != nil {
			if err := e.store.SaveWorkflowState(ws); err != nil {
				rollback()
				return nil, fmt.Errorf("save completed workflow state: %w", err)
			}
		}
		delete(e.workflows, userID)
		if e.callbacks != nil {
			_ = e.callbacks.EmitPhaseUpdate(userID, ws)
		}

		return &WorkflowResponse{
			Text:     i18n.Tf(i18n.MsgWorkflowCompleted, e.getLang(), len(tmpl.Phases)),
			Complete: true,
			Advance:  true,
		}, nil
	}

	// Advance to next phase.
	ws.PhaseIndex = nextIndex
	ws.CurrentPhase = tmpl.Phases[nextIndex].ID
	ws.UpdatedAt = now

	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			rollback()
			return nil, fmt.Errorf("save advanced workflow state: %w", err)
		}
	}
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, ws)
	}

	nextPhase := &tmpl.Phases[nextIndex]
	advanceText := i18n.Tf(i18n.MsgWorkflowPhaseAdvance, e.getLang(), nextIndex+1, len(tmpl.Phases), nextPhase.Name)
	if nextPhase.InputSchema != nil && !ws.phaseFormGateSatisfied() {
		return &WorkflowResponse{
			Text:       advanceText,
			ShowForm:   true,
			FormSchema: nextPhase.InputSchema.Clone(),
			Advance:    true,
		}, nil
	}
	phasePrompt := BuildPhaseSystemPrompt(ws, nextPhase, e.registry)

	resp := &WorkflowResponse{
		Text:         advanceText,
		PhasePrompt:  phasePrompt,
		ToolFilter:   nextPhase.ToolPolicy,
		RunAgentLoop: true,
		Advance:      true,
	}

	// When advancing to an orchestrator-backed execution phase,
	// signal the caller to activate the task orchestrator. This is the
	// workflow engine's declaration that "planning phases are done, execute."
	// The caller decides HOW to execute (SubAgent vs main loop vs external).
	//
	// This decouples orchestrator activation from specific phase IDs; coding's
	// "ppt_generation" all satisfy ToolFilterFull && !NeedsConfirm.
	// Templates can opt out when full-tool execution must stay inside the
	// phase prompt itself (for example controlled ops execution).
	if IsExecutionOrchestratorPhase(*nextPhase) {
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

	return resp, nil
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
		return ws.Clone()
	}
	return nil
}

// ActiveWorkflowUserIDForPhase returns the user ID for the single active
// workflow currently on phaseID. If no workflow or multiple workflows match,
// it returns false so callers do not guess across sessions.
func (e *WorkflowEngine) ActiveWorkflowUserIDForPhase(phaseID string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	phaseID = strings.TrimSpace(phaseID)
	if phaseID == "" {
		return "", false
	}
	matchedUserID := ""
	for userID, ws := range e.workflows {
		if ws == nil || ws.Status != WorkflowActive || ws.CurrentPhase != phaseID {
			continue
		}
		if matchedUserID != "" {
			return "", false
		}
		matchedUserID = userID
	}
	return matchedUserID, matchedUserID != ""
}

// SingleActiveWorkflowUserID returns the user ID when exactly one active
// workflow exists. If there are none or more than one, it returns false so
// callers do not guess across sessions.
func (e *WorkflowEngine) SingleActiveWorkflowUserID() (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	matchedUserID := ""
	for userID, ws := range e.workflows {
		if ws == nil || ws.Status != WorkflowActive {
			continue
		}
		if matchedUserID != "" {
			return "", false
		}
		matchedUserID = userID
	}
	return matchedUserID, matchedUserID != ""
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
		return fmt.Errorf("user has no active workflow")
	}
	_, err := e.cancelWorkflowLocked(userID, ws, i18n.T(i18n.MsgWorkflowCancelled, e.getLang()))
	return err
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
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return ""
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase || e.isPhaseExecutionBlockedLocked(ws, tmpl, phase) {
		return ""
	}

	return BuildPhaseSystemPrompt(ws, phase, e.registry)
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
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return ToolFilterNone
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase || e.isPhaseExecutionBlockedLocked(ws, tmpl, phase) {
		return ToolFilterNone
	}

	return GetToolFilterForPhase(phase)
}

// GetActivePhaseToolFilter returns the template tool policy for the current
// active phase even when the phase is temporarily blocked by a form, input, or
// review gate. Use this for defensive LLM-facing filtering; use
// GetPhaseToolFilter when the caller needs to know whether phase execution is
// currently allowed.
func (e *WorkflowEngine) GetActivePhaseToolFilter(userID string) ToolFilterPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return ToolFilterNone
	}

	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return ToolFilterNone
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase {
		return ToolFilterNone
	}

	return GetToolFilterForPhase(phase)
}

// IsActivePhaseExecutionOrchestrator reports whether the active phase is an
// implementation phase that may launch the task/CodingSubAgent orchestrator.
func (e *WorkflowEngine) IsActivePhaseExecutionOrchestrator(userID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return false
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return false
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase || e.isPhaseExecutionBlockedLocked(ws, tmpl, phase) {
		return false
	}
	return IsExecutionOrchestratorPhase(*phase)
}

// IsPhaseExecutionBlocked reports whether the active workflow phase is waiting
// for form/input/review completion. A missing workflow is not blocked; corrupt
// workflow state is treated as blocked.
func (e *WorkflowEngine) IsPhaseExecutionBlocked(userID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		return false
	}

	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return true
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase {
		return true
	}

	return e.isPhaseExecutionBlockedLocked(ws, tmpl, phase)
}

func (e *WorkflowEngine) isPhaseExecutionBlockedLocked(ws *WorkflowState, tmpl *WorkflowTemplate, phase *PhaseTemplate) bool {
	if ws == nil || tmpl == nil || phase == nil || ws.Status != WorkflowActive {
		return true
	}
	if ws.IsWaitingForInput(tmpl) {
		return true
	}
	if ws.PendingReviewPhaseID != "" {
		return true
	}
	if phase.InputSchema != nil && !ws.phaseFormGateSatisfied() {
		return true
	}
	return false
}

// GetOpsApprovedCommands returns the confirmed risk-policy command manifest for
// an active ops workflow. It is used by execution hosts as a hard boundary for
// controlled_execution.
func (e *WorkflowEngine) GetOpsApprovedCommands(userID string) []OpsApprovedCommand {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive || ws.Type != WorkflowOpsMaintenance {
		return nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if ws.CurrentPhase != "controlled_execution" || phase.ID != ws.CurrentPhase {
		return nil
	}
	if e.isPhaseExecutionBlockedLocked(ws, tmpl, phase) {
		return nil
	}
	if ws.PhaseIndex == 0 || tmpl.Phases[ws.PhaseIndex-1].ID != "risk_policy" {
		return nil
	}
	riskPolicy := strings.TrimSpace(ws.PhaseOutputs["risk_policy"])
	if riskPolicy == "" {
		return nil
	}
	// In the workflow state-machine path, reaching controlled_execution with a
	// completed risk_policy output means the risk gate was reviewed and
	// confirmed. Detached service/API callers do not have this state proof and
	// must provide a separate approval marker before approval_required manifests
	// are honored.
	return ExtractOpsApprovedCommands(riskPolicy)
}

// HasPhaseOutput returns true if the user's active workflow has
// output stored for the current phase. The agent loop uses this
// to distinguish first execution (no output yet; let the loop
// continue) from post-output confirmation (output exists; gate
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
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return false
	}

	return tmpl.Phases[ws.PhaseIndex].NeedsConfirm
}

// IsAwaitingReview returns true when a NeedsConfirm phase has produced output
// and the workflow is waiting for explicit user confirmation or modification.
func (e *WorkflowEngine) IsAwaitingReview(userID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ws := e.workflows[userID]
	return ws != nil &&
		ws.Status == WorkflowActive &&
		ws.PendingReviewPhaseID != "" &&
		ws.PendingReviewPhaseID == ws.CurrentPhase
}

// ---------------------------------------------------------------------------
// Persistence restore / cleanup
// ---------------------------------------------------------------------------

func workflowTemplatePhaseIndex(tmpl *WorkflowTemplate, phaseID string) (int, bool) {
	if tmpl == nil {
		return 0, false
	}
	for i := range tmpl.Phases {
		if tmpl.Phases[i].ID == phaseID {
			return i, true
		}
	}
	return 0, false
}

func (e *WorkflowEngine) cancelRestoredWorkflowLocked(ws *WorkflowState, reason string) (bool, error) {
	previousStatus := ws.Status
	previousPendingReviewPhaseID := ws.PendingReviewPhaseID
	previousPendingReviewRevisionRequested := ws.PendingReviewRevisionRequested
	previousUpdatedAt := ws.UpdatedAt
	ws.Status = WorkflowCancelled
	ws.PendingReviewPhaseID = ""
	ws.PendingReviewRevisionRequested = false
	ws.UpdatedAt = time.Now()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			ws.Status = previousStatus
			ws.PendingReviewPhaseID = previousPendingReviewPhaseID
			ws.PendingReviewRevisionRequested = previousPendingReviewRevisionRequested
			ws.UpdatedAt = previousUpdatedAt
			return false, fmt.Errorf("save restored workflow cancellation: %w", err)
		}
	}
	log.Printf("[WorkflowEngine] cancelled restored workflow %s for user %s: %s", ws.ID, ws.UserID, reason)
	return false, nil
}

func (e *WorkflowEngine) repairRestoredWorkflowStateLocked(ws *WorkflowState) (bool, error) {
	if time.Since(ws.UpdatedAt) > workflowStaleTimeout {
		log.Printf("[WorkflowEngine] cancelling stale workflow %s for user %s "+
			"(type=%s, phase=%s, last_updated=%s, age=%s)",
			ws.ID, ws.UserID, ws.Type, ws.CurrentPhase,
			ws.UpdatedAt.Format("2006-01-02 15:04:05"),
			time.Since(ws.UpdatedAt).Round(time.Minute))
		return e.cancelRestoredWorkflowLocked(ws, "stale active workflow")
	}

	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return e.cancelRestoredWorkflowLocked(ws, "missing workflow template")
	}

	previousPhaseIndex := ws.PhaseIndex
	previousCurrentPhase := ws.CurrentPhase
	previousPendingReviewPhaseID := ws.PendingReviewPhaseID
	previousPendingReviewRevisionRequested := ws.PendingReviewRevisionRequested
	previousPhaseFormData := ws.PhaseFormData
	previousPhaseFormSubmitted := ws.PhaseFormSubmitted
	previousPhaseFormSkipped := ws.PhaseFormSkipped
	previousPhaseOutputs := ws.PhaseOutputs
	previousGateResults := ws.GateResults
	previousUpdatedAt := ws.UpdatedAt
	repaired := false
	rollback := func() {
		ws.PhaseIndex = previousPhaseIndex
		ws.CurrentPhase = previousCurrentPhase
		ws.PendingReviewPhaseID = previousPendingReviewPhaseID
		ws.PendingReviewRevisionRequested = previousPendingReviewRevisionRequested
		ws.PhaseFormData = previousPhaseFormData
		ws.PhaseFormSubmitted = previousPhaseFormSubmitted
		ws.PhaseFormSkipped = previousPhaseFormSkipped
		ws.PhaseOutputs = previousPhaseOutputs
		ws.GateResults = previousGateResults
		ws.UpdatedAt = previousUpdatedAt
	}

	if ws.PhaseOutputs == nil {
		ws.PhaseOutputs = make(map[string]string)
		repaired = true
	}
	if ws.GateResults == nil {
		ws.GateResults = make(map[string]*QualityGateResult)
		repaired = true
	}

	if tmpl.NeedsInputDocument() && !ws.InputReceived {
		if ws.PhaseIndex != 0 || ws.CurrentPhase != tmpl.Phases[0].ID || len(ws.PhaseOutputs) != 0 || len(ws.GateResults) != 0 || ws.PendingReviewPhaseID != "" || ws.PendingReviewRevisionRequested || ws.phaseFormGateSatisfied() {
			ws.PhaseIndex = 0
			ws.CurrentPhase = tmpl.Phases[0].ID
			ws.PhaseOutputs = make(map[string]string)
			ws.GateResults = make(map[string]*QualityGateResult)
			ws.PendingReviewPhaseID = ""
			ws.PendingReviewRevisionRequested = false
			ws.PhaseFormData = nil
			ws.PhaseFormSubmitted = false
			ws.PhaseFormSkipped = false
			repaired = true
		}
	}

	if ws.PhaseIndex > 0 && len(ws.PhaseOutputs) == 0 {
		ws.PhaseIndex = 0
		ws.CurrentPhase = tmpl.Phases[0].ID
		ws.PendingReviewPhaseID = ""
		ws.PendingReviewRevisionRequested = false
		ws.PhaseFormData = nil
		ws.PhaseFormSubmitted = false
		ws.PhaseFormSkipped = false
		repaired = true
	}

	if ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) || tmpl.Phases[ws.PhaseIndex].ID != ws.CurrentPhase {
		if idx, ok := workflowTemplatePhaseIndex(tmpl, ws.CurrentPhase); ok {
			ws.PhaseIndex = idx
			repaired = true
		} else if ws.PhaseIndex >= 0 && ws.PhaseIndex < len(tmpl.Phases) {
			ws.CurrentPhase = tmpl.Phases[ws.PhaseIndex].ID
			repaired = true
		} else {
			return e.cancelRestoredWorkflowLocked(ws, "unrepairable phase pointer")
		}
	}

	phase := &tmpl.Phases[ws.PhaseIndex]
	if ws.PendingReviewPhaseID != "" && (ws.PendingReviewPhaseID != ws.CurrentPhase || !phase.NeedsConfirm) {
		ws.PendingReviewPhaseID = ""
		ws.PendingReviewRevisionRequested = false
		repaired = true
	}
	if ws.PendingReviewPhaseID == "" && phase.NeedsConfirm && strings.TrimSpace(ws.PhaseOutputs[ws.CurrentPhase]) != "" {
		ws.PendingReviewPhaseID = ws.CurrentPhase
		ws.PendingReviewRevisionRequested = false
		repaired = true
	}
	if ws.PendingReviewPhaseID == "" && ws.PendingReviewRevisionRequested {
		ws.PendingReviewRevisionRequested = false
		repaired = true
	}
	if phase.InputSchema == nil && ws.phaseFormGateSatisfied() {
		ws.PhaseFormData = nil
		ws.PhaseFormSubmitted = false
		ws.PhaseFormSkipped = false
		repaired = true
	}
	if phase.InputSchema != nil {
		if _, legacySkipped := ws.PhaseFormData["_skipped"]; legacySkipped {
			ws.PhaseFormData = nil
			ws.PhaseFormSubmitted = false
			ws.PhaseFormSkipped = true
			repaired = true
		} else if ws.phaseFormGateSatisfied() && !ws.PhaseFormSubmitted && !ws.PhaseFormSkipped {
			ws.PhaseFormSubmitted = true
			repaired = true
		}
	}

	if repaired {
		ws.UpdatedAt = time.Now()
		if e.store != nil {
			if err := e.store.SaveWorkflowState(ws); err != nil {
				rollback()
				return false, fmt.Errorf("save repaired workflow state: %w", err)
			}
		}
	}
	return true, nil
}

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

		publish, err := e.repairRestoredWorkflowStateLocked(ws)
		if err != nil {
			e.mu.Unlock()
			return err
		}
		if publish {
			e.workflows[ws.UserID] = ws
		}
	}
	e.mu.Unlock()

	return nil
}

// CleanupExpired removes completed/cancelled workflow records older than 7 days
// from the persistence store.
func (e *WorkflowEngine) CleanupExpired() error {
	if e.store == nil {
		return nil
	}
	return e.store.CleanupExpired(workflowExpiry)
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
func (e *WorkflowEngine) SavePhaseOutput(userID, content string) (string, error) {
	e.mu.Lock()

	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return "", nil
	}

	phaseID := ws.CurrentPhase
	if phaseID == "" {
		e.mu.Unlock()
		return "", nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) || tmpl.Phases[ws.PhaseIndex].ID != phaseID {
		e.mu.Unlock()
		return "", nil
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	if ws.IsWaitingForInput(tmpl) || (phase.InputSchema != nil && !ws.phaseFormGateSatisfied()) {
		log.Printf("[WorkflowEngine] SavePhaseOutput ignored: phase execution is blocked by user input gate for phase=%s user=%s", phaseID, userID)
		e.mu.Unlock()
		return "", nil
	}
	if ws.PendingReviewPhaseID != "" {
		if ws.PendingReviewPhaseID != phaseID {
			log.Printf("[WorkflowEngine] SavePhaseOutput ignored: workflow is awaiting review for phase=%s, got output for phase=%s user=%s", ws.PendingReviewPhaseID, phaseID, userID)
			e.mu.Unlock()
			return "", nil
		}
		if !ws.PendingReviewRevisionRequested {
			log.Printf("[WorkflowEngine] SavePhaseOutput ignored: phase=%s user=%s is awaiting review without a regeneration request", phaseID, userID)
			e.mu.Unlock()
			return "", nil
		}
		previousUpdatedAt := ws.UpdatedAt
		ws.PendingReviewRevisionRequested = false
		ws.UpdatedAt = time.Now()
		if e.store != nil {
			if err := e.store.SaveWorkflowState(ws); err != nil {
				ws.PendingReviewRevisionRequested = true
				ws.UpdatedAt = previousUpdatedAt
				e.mu.Unlock()
				return "", fmt.Errorf("consume review regeneration request: %w", err)
			}
		}
	}
	// Minimum quality gate.
	// Reject content that is clearly not a phase deliverable. This catches
	// cases where the LLM ignored the phase prompt and produced unrelated
	// output (e.g., answering a previous task's question instead of
	// generating the phase document).
	//
	// The gate checks structural properties that ANY phase deliverable
	// should have; it does not use phase-specific keywords (which would
	// be a workaround that breaks when templates change).
	if !passesMinimumQualityGate(content) {
		log.Printf("[WorkflowEngine] SavePhaseOutput rejected: content does not pass minimum quality gate for phase=%s user=%s len=%d lines=%d",
			phaseID, userID, len([]rune(content)), strings.Count(content, "\n")+1)
		e.mu.Unlock()
		return "", nil
	}

	previousOutput, hadPreviousOutput := ws.PhaseOutputs[phaseID]
	previousGate, hadPreviousGate := ws.GateResults[phaseID]
	previousPendingReviewPhaseID := ws.PendingReviewPhaseID
	previousPendingReviewRevisionRequested := ws.PendingReviewRevisionRequested
	previousUpdatedAt := ws.UpdatedAt
	previousOutputsNil := ws.PhaseOutputs == nil
	previousGatesNil := ws.GateResults == nil

	rollback := func() {
		if previousOutputsNil {
			ws.PhaseOutputs = nil
		} else if hadPreviousOutput {
			ws.PhaseOutputs[phaseID] = previousOutput
		} else {
			delete(ws.PhaseOutputs, phaseID)
		}
		if previousGatesNil {
			ws.GateResults = nil
		} else if hadPreviousGate {
			ws.GateResults[phaseID] = previousGate
		} else {
			delete(ws.GateResults, phaseID)
		}
		ws.PendingReviewPhaseID = previousPendingReviewPhaseID
		ws.PendingReviewRevisionRequested = previousPendingReviewRevisionRequested
		ws.UpdatedAt = previousUpdatedAt
	}

	if ws.PhaseOutputs == nil {
		ws.PhaseOutputs = make(map[string]string)
	}
	if ws.GateResults == nil {
		ws.GateResults = make(map[string]*QualityGateResult)
	}
	ws.PhaseOutputs[phaseID] = content
	ws.UpdatedAt = time.Now()

	// Run quality gate check against the phase's checklist.
	var gateResult *QualityGateResult
	if phase.NeedsConfirm {
		ws.PendingReviewPhaseID = phaseID
		ws.PendingReviewRevisionRequested = false
	}
	if gateResult = RunQualityGate(phase, content); gateResult != nil {
		ws.GateResults[phaseID] = gateResult
	}

	if e.store != nil {
		if err := e.store.SaveWorkflowState(ws); err != nil {
			rollback()
			e.mu.Unlock()
			return "", fmt.Errorf("save phase output state: %w", err)
		}
	}
	// Capture values needed for artifact sinking before releasing the lock.
	saver := e.artifactSaver
	wsType := string(ws.Type)
	projectPath := ws.ProjectPath
	stateForCallback := ws.Clone()
	callbacks := e.callbacks

	e.mu.Unlock()

	if gateResult != nil && callbacks != nil {
		_ = callbacks.EmitGateResult(userID, phaseID, gateResult)
	}
	if callbacks != nil {
		_ = callbacks.EmitPhaseUpdate(userID, stateForCallback)
	}

	// Sink phase output summary to long-term memory OUTSIDE the engine lock.
	// This avoids WorkflowEngine.mu -> memory.Store.mu lock nesting.
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
			summary = string(runes[:cutoff]) + "\n...(truncated)"
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
			title = wsType + " / " + phaseID
		}
		if fullSaver, ok := saver.(FullArtifactSaver); ok {
			_ = fullSaver.SaveArtifactFull(title, summary, content, tags, "")
		} else {
			_ = saver.SaveArtifact(title, summary, tags, "")
		}
	}

	return phaseID, nil
}

// SavePhaseOutputAndMaybeAdvance captures a generated phase deliverable and
// applies the workflow's phase-completion contract. NeedsConfirm phases enter
// review state and wait for ApplyReviewIntent; non-confirm phases advance as
// soon as their deliverable is captured.
func (e *WorkflowEngine) SavePhaseOutputAndMaybeAdvance(userID, content string) (string, *WorkflowResponse, error) {
	phaseID, err := e.SavePhaseOutput(userID, content)
	if err != nil || phaseID == "" {
		return phaseID, nil, err
	}

	e.mu.RLock()
	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive || ws.CurrentPhase != phaseID {
		e.mu.RUnlock()
		return phaseID, nil, nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) || tmpl.Phases[ws.PhaseIndex].ID != phaseID {
		e.mu.RUnlock()
		return phaseID, nil, nil
	}
	needsConfirm := tmpl.Phases[ws.PhaseIndex].NeedsConfirm
	e.mu.RUnlock()

	if needsConfirm {
		return phaseID, nil, nil
	}
	resp, err := e.AdvancePhase(userID)
	if err != nil {
		log.Printf("[WorkflowEngine] auto-advance after phase output failed: user=%s phase=%s err=%v", userID, phaseID, err)
		return phaseID, nil, err
	}
	return phaseID, resp, nil
}

// passesMinimumQualityGate performs a lightweight structural check to reject
// content that is clearly not a valid phase deliverable. Returns true if the
// content should be stored, false if it should be rejected.
//
// This is NOT a comprehensive quality check; it only catches obvious
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
	// Require at least 100 runes. A short acknowledgement is not a phase deliverable.
	if len(runes) < 100 {
		return false
	}

	// Gate 2: Structural complexity. A phase deliverable should have some
	// structure: multiple lines, headers, or list items. A single paragraph
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
	return tmpl.RequiresInput.Clone()
}

// truncateForLog truncates a string to maxRunes for log output.
func truncateForLog(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
