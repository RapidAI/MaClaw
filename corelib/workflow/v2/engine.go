package v2

// engine.go — WorkflowEngine adapter + supporting types.
//
// WorkflowEngine is a thin adapter over an in-memory map[string]*EngineState.
// It provides the primary API surface for GUI/TUI consumers to interact with
// workflow state (start, handle input, get active workflow, check phase policy).
//
// The StateMachine (machine.go) is the durable backend; WorkflowEngine is the
// in-process query layer that GUI code calls synchronously.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// ErrIntentUnderstandingContractBreach
// ---------------------------------------------------------------------------

// ErrIntentUnderstandingContractBreach is returned when the IUM LLM completely
// violates the structured JSON contract (e.g. produces capability denial text).
var ErrIntentUnderstandingContractBreach = errors.New("intent understanding LLM contract breach")

// ---------------------------------------------------------------------------
// WorkflowStartOptions
// ---------------------------------------------------------------------------

// WorkflowStartOptions carries durable context for workflow creation.
type WorkflowStartOptions struct {
	ProjectPath string
}

// ---------------------------------------------------------------------------
// WorkflowStatus convenience aliases
// ---------------------------------------------------------------------------

const (
	WorkflowActive    WorkflowStatus = "active"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowCancelled WorkflowStatus = "cancelled"
)

// ---------------------------------------------------------------------------
// WorkflowChecker interface
// ---------------------------------------------------------------------------

// WorkflowChecker is the minimal interface QuickFilter needs from the engine.
type WorkflowChecker interface {
	HasActiveWorkflow(userID string) bool
	HasActiveUnderstanding(userID string) bool
}

// ---------------------------------------------------------------------------
// QuickFilter (stub)
// ---------------------------------------------------------------------------

// QuickFilter routes deterministic workflow state (stubbed).
type QuickFilter struct {
	engine WorkflowChecker
}

// NewQuickFilter creates a QuickFilter stub.
func NewQuickFilter(engine WorkflowChecker) *QuickFilter {
	return &QuickFilter{engine: engine}
}

// Classify returns simple_directive for all messages (stub).
func (f *QuickFilter) Classify(userID, text string) FilterResult {
	if f.engine != nil {
		if f.engine.HasActiveWorkflow(userID) {
			return FilterActiveWorkflow
		}
		if f.engine.HasActiveUnderstanding(userID) {
			return FilterActiveUnderstanding
		}
	}
	return FilterSimpleDirective
}

// ---------------------------------------------------------------------------
// StartResult
// ---------------------------------------------------------------------------

// StartResult holds the outcome of IntentUnderstandingManager.Start().
type StartResult struct {
	Reply    string
	Rejected bool
	Ready    bool
	Intent   *StructuredIntent
}

// ---------------------------------------------------------------------------
// IntentUnderstandingManager (stub)
// ---------------------------------------------------------------------------

// IntentUnderstandingManager handles multi-round intent clarification (stubbed).
type IntentUnderstandingManager struct {
	mu       sync.RWMutex
	sessions map[string]*UnderstandingSession
	store    PersistenceStore
	llm      LLMCaller
	registry *WorkflowRegistry
}

// NewIntentUnderstandingManager creates a stubbed IntentUnderstandingManager.
func NewIntentUnderstandingManager(store PersistenceStore, llm LLMCaller, registry *WorkflowRegistry) *IntentUnderstandingManager {
	return &IntentUnderstandingManager{
		sessions: make(map[string]*UnderstandingSession),
		store:    store,
		llm:      llm,
		registry: registry,
	}
}

func (m *IntentUnderstandingManager) SetLanguage(lang string)             {}
func (m *IntentUnderstandingManager) SetUserLanguage(userID, lang string) {}
func (m *IntentUnderstandingManager) Start(userID, text string) (*StartResult, error) {
	if m == nil || m.llm == nil {
		return &StartResult{Rejected: true}, nil
	}
	result, err := m.callUnderstandingLLM(userID, text, nil)
	if err != nil {
		return nil, err
	}
	if result.Rejected || result.Ready {
		return result, nil
	}
	if result.Intent == nil || result.Intent.Category == "" || result.Intent.Category == WorkflowNone {
		result.Rejected = true
		return result, nil
	}
	now := time.Now()
	session := &UnderstandingSession{
		ID:        fmt.Sprintf("understanding-%d", now.UnixNano()),
		UserID:    userID,
		Intent:    *result.Intent,
		State:     UnderstandingActive,
		CreatedAt: now,
		UpdatedAt: now,
		Rounds: []UnderstandingRound{{
			UserText:      text,
			AssistantText: result.Reply,
			Timestamp:     now,
		}},
	}
	m.mu.Lock()
	m.sessions[userID] = session
	m.mu.Unlock()
	if m.store != nil {
		_ = m.store.SaveUnderstandingSession(session)
	}
	return result, nil
}
func (m *IntentUnderstandingManager) HandleInput(userID, text string) (string, bool, bool, *StructuredIntent, error) {
	if m == nil {
		return "", false, false, nil, nil
	}
	session := m.GetSession(userID)
	if session == nil || session.State != UnderstandingActive {
		return "", false, false, nil, nil
	}
	result, err := m.callUnderstandingLLM(userID, text, session)
	if err != nil {
		return "", false, false, nil, err
	}
	now := time.Now()
	session.Rounds = append(session.Rounds, UnderstandingRound{
		UserText:      text,
		AssistantText: result.Reply,
		Timestamp:     now,
	})
	session.UpdatedAt = now
	if result.Intent != nil {
		session.Intent = *result.Intent
	}
	if result.Ready || result.Rejected {
		session.State = UnderstandingConfirmed
		m.mu.Lock()
		delete(m.sessions, userID)
		m.mu.Unlock()
		if m.store != nil {
			_ = m.store.DeleteUnderstandingSession(userID)
		}
		return result.Reply, result.Ready, false, result.Intent, nil
	}
	m.mu.Lock()
	m.sessions[userID] = session
	m.mu.Unlock()
	if m.store != nil {
		_ = m.store.SaveUnderstandingSession(session)
	}
	return result.Reply, false, false, result.Intent, nil
}

type intentUnderstandingLLMResponse struct {
	Intent    *StructuredIntent `json:"intent"`
	Reply     string            `json:"reply"`
	Ready     bool              `json:"ready"`
	Rejected  bool              `json:"rejected"`
	Cancel    bool              `json:"cancel"`
	Cancelled bool              `json:"cancelled"`
}

func (m *IntentUnderstandingManager) callUnderstandingLLM(userID, text string, session *UnderstandingSession) (*StartResult, error) {
	messages := []interface{}{
		map[string]string{
			"role":    "system",
			"content": "Classify whether the user request should start or continue a workflow. Reply as JSON with intent, reply, ready, and rejected fields.",
		},
	}
	if session != nil {
		messages = append(messages, map[string]string{"role": "system", "content": fmt.Sprintf("Current workflow intent: %s\nSummary: %s", session.Intent.Category, session.Intent.Summary)})
		for _, round := range session.Rounds {
			messages = append(messages,
				map[string]string{"role": "user", "content": round.UserText},
				map[string]string{"role": "assistant", "content": round.AssistantText},
			)
		}
	}
	messages = append(messages, map[string]string{"role": "user", "content": text})
	raw, err := m.llm.DoSimpleLLMRequest(messages, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var parsed intentUnderstandingLLMResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil {
		return nil, ErrIntentUnderstandingContractBreach
	}
	ready := parsed.Ready
	if parsed.Intent != nil && parsed.Intent.Ready {
		ready = true
	}
	rejected := parsed.Rejected || parsed.Cancel || parsed.Cancelled
	if parsed.Intent == nil && !ready {
		rejected = true
	}
	return &StartResult{
		Reply:    strings.TrimSpace(parsed.Reply),
		Rejected: rejected,
		Ready:    ready,
		Intent:   parsed.Intent,
	}, nil
}
func (m *IntentUnderstandingManager) GetSession(userID string) *UnderstandingSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[userID]
}
func (m *IntentUnderstandingManager) HasActiveSession(userID string) bool {
	s := m.GetSession(userID)
	return s != nil && s.State == UnderstandingActive
}
func (m *IntentUnderstandingManager) CleanupExpired()                    {}
func (m *IntentUnderstandingManager) RestoreSession(userID string) error { return nil }
func (m *IntentUnderstandingManager) CancelSession(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[userID]; ok {
		s.State = UnderstandingCancelled
	}
}

// ---------------------------------------------------------------------------
// WorkflowRegistry (stub)
// ---------------------------------------------------------------------------

// WorkflowRegistry holds registered workflow templates (stubbed BM25 methods).
// The templates map is populated at startup from V2 TemplateRegistry and does
// not grow at runtime — no memory pressure concern.
type WorkflowRegistry struct {
	mu        sync.RWMutex
	templates map[WorkflowType]*TemplateSpec
}

// NewWorkflowRegistry creates a WorkflowRegistry.
func NewWorkflowRegistry() *WorkflowRegistry {
	registry := &WorkflowRegistry{
		templates: make(map[WorkflowType]*TemplateSpec),
	}
	registry.registerBuiltinTemplates()
	return registry
}

func (r *WorkflowRegistry) registerBuiltinTemplates() {
	if r == nil {
		return
	}
	v2Registry := NewTemplateRegistry()
	RegisterBuiltinTemplates(v2Registry)
	for _, tmplType := range v2Registry.AllTypes() {
		v2Tmpl := v2Registry.Get(string(tmplType))
		if v2Tmpl == nil {
			continue
		}
		phases := make([]PhaseSpec, 0, len(v2Tmpl.Phases))
		for _, phase := range v2Tmpl.Phases {
			phases = append(phases, phaseTemplateToSpec(WorkflowType(v2Tmpl.Type), phase))
		}
		var requiresInput *InputRequirement
		if len(v2Tmpl.Phases) > 0 && v2Tmpl.Phases[0].InputSchema != nil && v2Tmpl.Phases[0].InputSchema.Title != "" {
			requiresInput = &InputRequirement{
				Description: v2Tmpl.Phases[0].InputSchema.Title,
				AcceptText:  true,
			}
		}
		r.templates[WorkflowType(v2Tmpl.Type)] = &TemplateSpec{
			Type:          WorkflowType(v2Tmpl.Type),
			Name:          v2Tmpl.Name,
			Description:   v2Tmpl.Description,
			Keywords:      append([]string(nil), v2Tmpl.Keywords...),
			Phases:        phases,
			RequiresInput: requiresInput,
		}
	}
}

func clonePhaseInputOptions(options []PhaseInputOption) []PhaseInputOptionSpec {
	if len(options) == 0 {
		return nil
	}
	out := make([]PhaseInputOptionSpec, 0, len(options))
	for _, option := range options {
		out = append(out, PhaseInputOptionSpec{
			Label: option.Label,
			Value: option.Value,
		})
	}
	return out
}

func phaseTemplateToSpec(workflowType WorkflowType, phase PhaseTemplate) PhaseSpec {
	kind, mutationScope, _ := phaseMetadataSemantics(workflowType, CanonicalPhaseID(phase.ID))
	spec := PhaseSpec{
		ID:            phase.ID,
		Name:          phase.Name,
		NeedsConfirm:  phase.NeedsConfirm,
		ToolPolicy:    ToolFilterPolicy(phase.ToolPolicy),
		Kind:          firstPhaseKind(phase.Kind, kind),
		MutationScope: firstMutationScope(phase.MutationScope, mutationScope),
		DependsOnFull: append([]string(nil), phase.DependsOnFull...),
	}
	spec.InputSchema = phaseInputSchemaToSpec(phase.InputSchema)
	return spec
}

func phaseSpecToTemplate(workflowType WorkflowType, spec PhaseSpec) PhaseTemplate {
	kind, mutationScope, _ := phaseMetadataSemantics(workflowType, CanonicalPhaseID(spec.ID))
	tmpl := PhaseTemplate{
		ID:            spec.ID,
		Name:          spec.Name,
		NeedsConfirm:  spec.NeedsConfirm,
		ToolPolicy:    ToolPolicy(spec.ToolPolicy),
		Kind:          firstPhaseKind(spec.Kind, kind),
		MutationScope: firstMutationScope(spec.MutationScope, mutationScope),
		DependsOnFull: append([]string(nil), spec.DependsOnFull...),
	}
	tmpl.InputSchema = phaseInputSchemaFromSpec(spec.InputSchema)
	return tmpl
}

func clonePhaseInputOptionsBack(options []PhaseInputOptionSpec) []PhaseInputOption {
	if len(options) == 0 {
		return nil
	}
	out := make([]PhaseInputOption, 0, len(options))
	for _, option := range options {
		out = append(out, PhaseInputOption{
			Label: option.Label,
			Value: option.Value,
		})
	}
	return out
}

func phaseInputFieldToSpec(field PhaseInputField) PhaseInputFieldSpec {
	return PhaseInputFieldSpec{
		Name:        field.Name,
		Label:       field.Label,
		Type:        field.Type,
		Required:    field.Required,
		Sensitive:   field.Sensitive,
		Description: field.Description,
		Placeholder: field.Placeholder,
		Options:     clonePhaseInputOptions(field.Options),
		Default:     field.Default,
		Reusable:    field.Reusable,
	}
}

func phaseInputFieldFromSpec(field PhaseInputFieldSpec) PhaseInputField {
	return PhaseInputField{
		Name:        field.Name,
		Label:       field.Label,
		Type:        field.Type,
		Required:    field.Required,
		Sensitive:   field.Sensitive,
		Description: field.Description,
		Placeholder: field.Placeholder,
		Options:     clonePhaseInputOptionsBack(field.Options),
		Default:     field.Default,
		Reusable:    field.Reusable,
	}
}

func phaseInputSchemaToSpec(schema *PhaseInputSchema) *PhaseInputSchemaSpec {
	if schema == nil {
		return nil
	}
	spec := &PhaseInputSchemaSpec{
		Title:         schema.Title,
		Description:   schema.Description,
		Fields:        make([]PhaseInputFieldSpec, 0, len(schema.Fields)),
		Variants:      make([]PhaseInputVariantSpec, 0, len(schema.Variants)),
		AcceptsResume: schema.AcceptsResume,
	}
	for _, field := range schema.Fields {
		spec.Fields = append(spec.Fields, phaseInputFieldToSpec(field))
	}
	for _, variant := range schema.Variants {
		vs := PhaseInputVariantSpec{
			ID:     variant.ID,
			Label:  variant.Label,
			Fields: make([]PhaseInputFieldSpec, 0, len(variant.Fields)),
		}
		for _, field := range variant.Fields {
			vs.Fields = append(vs.Fields, phaseInputFieldToSpec(field))
		}
		spec.Variants = append(spec.Variants, vs)
	}
	if schema.AcceptsSupplementary != nil {
		spec.AcceptsSupplementary = &SupplementaryDocConfigSpec{
			Label:         schema.AcceptsSupplementary.Label,
			Description:   schema.AcceptsSupplementary.Description,
			MaxFiles:      schema.AcceptsSupplementary.MaxFiles,
			AcceptedTypes: append([]string(nil), schema.AcceptsSupplementary.AcceptedTypes...),
		}
	}
	return spec
}

func (r *WorkflowRegistry) Register(tmpl *TemplateSpec) error {
	if tmpl == nil || tmpl.Type == "" {
		return errors.New("invalid template")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[tmpl.Type] = tmpl
	return nil
}

func (r *WorkflowRegistry) MustRegister(tmpl *TemplateSpec) {
	if err := r.Register(tmpl); err != nil {
		panic(err)
	}
}

func (r *WorkflowRegistry) Match(wt WorkflowType) *TemplateSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates[wt]
}

func (r *WorkflowRegistry) All() []*TemplateSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*TemplateSpec, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}
	return result
}

func (r *WorkflowRegistry) AllDescriptions() string                          { return "" }
func (r *WorkflowRegistry) BestTemplateScore(text string) float64            { return 0 }
func (r *WorkflowRegistry) BestTemplateType(text string) WorkflowType        { return "" }
func (r *WorkflowRegistry) RankedTemplateScores(text string) []TemplateScore { return nil }
func (r *WorkflowRegistry) MatchesAnyTemplate(text string) bool              { return false }

// ---------------------------------------------------------------------------
// WorkflowEngine (stub)
// ---------------------------------------------------------------------------

// WorkflowEngine is the engine type retained for backward compatibility.
// All runtime behavior has been migrated to V2 StateMachine.
//
// Memory management: The workflows map grows with each StartWorkflow call.
// CancelWorkflow removes entries, but completed workflows remain (invisible
// to GetActiveWorkflow which checks Status==Active, but occupying memory).
// For long-running servers (maclawsrv), call CleanupExpired periodically to
// remove stale entries. Currently CleanupExpired is a no-op stub — implement
// it if memory pressure is observed in production.
type WorkflowEngine struct {
	mu            sync.RWMutex
	workflows     map[string]*EngineState
	registry      *WorkflowRegistry
	understanding *IntentUnderstandingManager
	store         PersistenceStore
	callbacks     EngineCallbacks
	filter        *QuickFilter
	artifactSaver ArtifactSaver
	machine       *StateMachine // V2 machine for bidirectional sync (test infrastructure)
}

// NewWorkflowEngine creates a WorkflowEngine stub.
func NewWorkflowEngine(
	registry *WorkflowRegistry,
	understanding *IntentUnderstandingManager,
	store PersistenceStore,
	callbacks EngineCallbacks,
) *WorkflowEngine {
	e := &WorkflowEngine{
		workflows:     make(map[string]*EngineState),
		registry:      registry,
		understanding: understanding,
		store:         store,
		callbacks:     callbacks,
	}
	e.filter = NewQuickFilter(e)
	return e
}

func (e *WorkflowEngine) SetArtifactSaver(s ArtifactSaver) { e.artifactSaver = s }
func (e *WorkflowEngine) SetMachine(m *StateMachine)       { e.machine = m }
func (e *WorkflowEngine) GetMachine() *StateMachine        { return e.machine }
func (e *WorkflowEngine) HasActiveWorkflow(userID string) bool {
	return e.GetActiveWorkflow(userID) != nil
}
func (e *WorkflowEngine) HasActiveUnderstanding(userID string) bool { return false }
func (e *WorkflowEngine) StartWorkflow(userID string, intent StructuredIntent) (*EngineState, error) {
	return e.StartWorkflowWithOptions(userID, intent, WorkflowStartOptions{})
}
func (e *WorkflowEngine) StartWorkflowWithOptions(userID string, intent StructuredIntent, options WorkflowStartOptions) (*EngineState, error) {
	if e == nil {
		return nil, errors.New("workflow engine is nil")
	}
	tmpl := e.registry.Match(intent.Category)
	if tmpl == nil {
		return nil, errors.New("unknown workflow type: " + string(intent.Category))
	}
	now := time.Now()
	currentPhase := ""
	if len(tmpl.Phases) > 0 {
		currentPhase = tmpl.Phases[0].ID
	}
	state := &EngineState{
		ID:           "wf_" + userID + "_" + now.Format("20060102150405"),
		UserID:       userID,
		Type:         intent.Category,
		Intent:       intent,
		CurrentPhase: currentPhase,
		PhaseIndex:   0,
		PhaseOutputs: map[string]string{},
		GateResults:  map[string]*QualityGateResult{},
		Status:       WorkflowActive,
		CreatedAt:    now,
		UpdatedAt:    now,
		ProjectPath:  strings.TrimSpace(options.ProjectPath),
	}
	if state.ProjectPath == "" {
		state.ProjectPath = "."
	}
	e.mu.Lock()
	e.workflows[userID] = state
	e.mu.Unlock()
	if e.store != nil {
		_ = e.store.SaveWorkflowState(state)
	}
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, state)
	}
	// Sync to V2 StateMachine if available (enables V2 production paths in tests).
	if e.machine != nil {
		if _, err := e.machine.Create(userID, string(intent.Category), state.ProjectPath, intent.Summary); err != nil {
			// V2 registry doesn't have this template type — create a minimal V2
			// WorkflowState directly in the store for test compatibility.
			if e.machine.GetRegistry() != nil && e.machine.GetRegistry().Get(string(intent.Category)) == nil && tmpl != nil {
				// Register the template in V2 registry from V1 TemplateSpec.
				v2Phases := make([]PhaseTemplate, 0, len(tmpl.Phases))
				for _, p := range tmpl.Phases {
					v2Phases = append(v2Phases, phaseSpecToTemplate(tmpl.Type, p))
				}
				e.machine.GetRegistry().Register(&WorkflowTemplate{
					Type:        string(intent.Category),
					Name:        tmpl.Name,
					Description: tmpl.Description,
					Keywords:    tmpl.Keywords,
					Phases:      v2Phases,
				})
				// Retry create after registration.
				e.machine.Create(userID, string(intent.Category), state.ProjectPath, intent.Summary)
			}
		}
	}
	return state, nil
}
func (e *WorkflowEngine) SetProjectPath(userID, projectPath string) error {
	if e == nil {
		return errors.New("workflow engine is nil")
	}
	userID = strings.TrimSpace(userID)
	projectPath = strings.TrimSpace(projectPath)
	if userID == "" {
		return errors.New("userID is required")
	}
	if projectPath == "" {
		return errors.New("project path is required")
	}
	e.mu.Lock()
	ws, ok := e.workflows[userID]
	if !ok || ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return errors.New("no active workflow for user")
	}
	ws.ProjectPath = projectPath
	ws.UpdatedAt = time.Now()
	snapshot := *ws
	e.mu.Unlock()
	if e.store != nil {
		_ = e.store.SaveWorkflowState(&snapshot)
	}
	if e.machine != nil {
		if state := e.machine.GetActive(userID); state != nil {
			state.ProjectPath = projectPath
			state.UpdatedAt = snapshot.UpdatedAt
			if e.machine.store != nil {
				_ = e.machine.store.Save(state)
			}
		}
	}
	return nil
}

// StoreActiveState stores a pre-built EngineState into the engine's active
// workflows map. Used when the workflow is created via V2 machine and needs to
// be accessible through engine methods (HandleInput, GetActiveWorkflow, etc.).
func (e *WorkflowEngine) StoreActiveState(userID string, state *EngineState) {
	if e == nil || state == nil {
		return
	}
	e.mu.Lock()
	e.workflows[userID] = state
	e.mu.Unlock()
}

func (e *WorkflowEngine) HandleInput(userID, text string) (*WorkflowResponse, error) {
	if e == nil {
		return nil, nil
	}
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return nil, nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return nil, nil
	}
	if ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, nil
	}
	phase := &tmpl.Phases[ws.PhaseIndex]

	// If the phase has a form gate and the form hasn't been submitted/skipped yet,
	// show the form.
	if phase.InputSchema != nil && !ws.PhaseFormSubmitted && !ws.PhaseFormSkipped {
		return &WorkflowResponse{
			ShowForm:   true,
			FormSchema: phase.InputSchema,
			Text:       phase.Name,
		}, nil
	}

	// If NeedsConfirm and there's already output, it's a review/confirm scenario.
	if phase.NeedsConfirm && ws.PhaseOutputs[ws.CurrentPhase] != "" {
		return &WorkflowResponse{
			PendingConfirm: true,
			Text:           ws.PhaseOutputs[ws.CurrentPhase],
		}, nil
	}

	// Default: run the agent loop with the phase prompt.
	prompt := BuildPhaseSystemPrompt(ws, phase, e.registry)
	if prompt == "" {
		// Fallback: use phase Prompt field, or phase Name.
		prompt = phase.Prompt
		if prompt == "" {
			prompt = phase.Name
		}
	}

	log.Printf("[WorkflowEngine] HandleInput: RunAgentLoop user=%s phase=%s", userID, ws.CurrentPhase)
	return &WorkflowResponse{
		PhasePrompt:  prompt,
		RunAgentLoop: true,
		DefaultInput: true,
	}, nil
}
func (e *WorkflowEngine) SubmitPhaseForm(userID string, formData map[string]interface{}) (*WorkflowResponse, error) {
	if e == nil {
		return nil, errors.New("workflow engine is nil")
	}
	e.mu.Lock()
	ws, ok := e.workflows[userID]
	if !ok || ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return nil, errors.New("no active workflow for user")
	}

	originalProjectPath := ws.ProjectPath
	originalFormData := ws.PhaseFormData
	originalSubmitted := ws.PhaseFormSubmitted
	originalUpdatedAt := ws.UpdatedAt
	// Normalize project_path / output_dir → update workflow ProjectPath.
	// Priority: project_path > output_dir. This ensures later phases see the
	// output directory in their prompt header ("项目路径").
	if pp := formDataString(formData, "project_path"); pp != "" {
		ws.ProjectPath = pp
		formData["project_path"] = pp
	} else if od := formDataString(formData, "output_dir"); od != "" {
		ws.ProjectPath = od
	}
	ws.PhaseFormData = formData
	ws.PhaseFormSubmitted = true
	ws.UpdatedAt = time.Now()
	snapshot := *ws
	e.mu.Unlock()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(&snapshot); err != nil {
			e.mu.Lock()
			if current := e.workflows[userID]; current != nil {
				current.ProjectPath = originalProjectPath
				current.PhaseFormData = originalFormData
				current.PhaseFormSubmitted = originalSubmitted
				current.UpdatedAt = originalUpdatedAt
			}
			e.mu.Unlock()
			return nil, err
		}
	}

	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return &WorkflowResponse{RunAgentLoop: true}, nil
	}
	if ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return &WorkflowResponse{RunAgentLoop: true}, nil
	}
	phase := &tmpl.Phases[ws.PhaseIndex]
	prompt := phase.Prompt
	if prompt == "" {
		prompt = phase.Name
	}

	return &WorkflowResponse{
		PhasePrompt:  prompt,
		RunAgentLoop: true,
	}, nil
}
func (e *WorkflowEngine) SubmitInputPayload(userID string, payload *WorkflowInputPayload) (*WorkflowResponse, error) {
	if e == nil {
		return nil, errors.New("workflow engine is nil")
	}
	e.mu.Lock()
	ws, ok := e.workflows[userID]
	if !ok || ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return nil, errors.New("no active workflow for user")
	}
	ws.InputReceived = true
	ws.InputPayload = payload
	ws.UpdatedAt = time.Now()
	e.mu.Unlock()
	// Check if the current phase has a form gate (InputSchema).
	tmpl := e.registry.Match(ws.Type)
	if tmpl != nil && ws.PhaseIndex >= 0 && ws.PhaseIndex < len(tmpl.Phases) {
		phase := &tmpl.Phases[ws.PhaseIndex]
		if phase.InputSchema != nil {
			return &WorkflowResponse{
				ShowForm:   true,
				FormSchema: phase.InputSchema,
			}, nil
		}
	}
	// No form gate — build phase prompt with input evidence.
	prompt := ""
	if tmpl != nil && ws.PhaseIndex >= 0 && ws.PhaseIndex < len(tmpl.Phases) {
		phase := &tmpl.Phases[ws.PhaseIndex]
		prompt = phase.Name
		if payload != nil {
			if payload.Text != "" {
				prompt += "\n\nUser input: " + payload.Text
			}
			for _, att := range payload.Attachments {
				prompt += "\n\nAttachment: " + att.FileName
			}
		}
	}
	return &WorkflowResponse{
		PhasePrompt:  prompt,
		RunAgentLoop: true,
	}, nil
}
func (e *WorkflowEngine) SkipPhaseForm(userID string) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	ws, ok := e.workflows[userID]
	if !ok || ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return nil
	}
	originalSkipped := ws.PhaseFormSkipped
	originalUpdatedAt := ws.UpdatedAt
	ws.PhaseFormSkipped = true
	ws.UpdatedAt = time.Now()
	snapshot := *ws
	e.mu.Unlock()
	if e.store != nil {
		if err := e.store.SaveWorkflowState(&snapshot); err != nil {
			e.mu.Lock()
			if current := e.workflows[userID]; current != nil {
				current.PhaseFormSkipped = originalSkipped
				current.UpdatedAt = originalUpdatedAt
			}
			e.mu.Unlock()
			return err
		}
	}
	if e.machine != nil && e.machine.GetActive(userID) != nil {
		if err := e.machine.SkipPhaseForm(userID); err != nil {
			e.mu.Lock()
			if current := e.workflows[userID]; current != nil {
				current.PhaseFormSkipped = originalSkipped
				current.UpdatedAt = originalUpdatedAt
			}
			e.mu.Unlock()
			if e.store != nil {
				rollback := snapshot
				rollback.PhaseFormSkipped = originalSkipped
				rollback.UpdatedAt = originalUpdatedAt
				_ = e.store.SaveWorkflowState(&rollback)
			}
			return err
		}
	}
	return nil
}
func (e *WorkflowEngine) AdvancePhase(userID string) (*WorkflowResponse, error) {
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return nil, nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		e.mu.Unlock()
		return nil, nil
	}
	ws.PhaseIndex++
	if ws.PhaseIndex >= len(tmpl.Phases) {
		ws.Status = WorkflowCompleted
		ws.UpdatedAt = time.Now()
		e.mu.Unlock()
		return &WorkflowResponse{Complete: true}, nil
	}
	ws.CurrentPhase = tmpl.Phases[ws.PhaseIndex].ID
	ws.PhaseFormSubmitted = false
	ws.PhaseFormSkipped = false
	ws.UpdatedAt = time.Now()
	nextPhase := tmpl.Phases[ws.PhaseIndex]
	e.mu.Unlock()
	// Sync to V2 machine.
	if e.machine != nil {
		e.machine.SetActivePhaseForTest(userID, ws.PhaseIndex)
	}
	if nextPhase.InputSchema != nil {
		return &WorkflowResponse{
			ShowForm:   true,
			FormSchema: nextPhase.InputSchema.Clone(),
		}, nil
	}
	return &WorkflowResponse{
		Advance:      true,
		RunAgentLoop: true,
		PhasePrompt:  e.BuildPhasePrompt(userID),
	}, nil
}
func (e *WorkflowEngine) CancelWorkflow(userID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ws, ok := e.workflows[userID]; ok {
		ws.Status = WorkflowCancelled
		delete(e.workflows, userID)
	}
	if e.machine != nil {
		e.machine.Cancel(userID)
	}
	return nil
}
func (e *WorkflowEngine) ApplyReviewIntent(userID string, intent ReviewIntent, feedback string) (*WorkflowResponse, error) {
	if e == nil {
		return nil, nil
	}
	// Delegate to V2 machine if available.
	if e.machine != nil && e.machine.GetActive(userID) != nil {
		hr, err := e.machine.ApplyReviewIntent(userID, string(intent), feedback)
		if err != nil {
			return nil, err
		}
		if hr == nil {
			return nil, nil
		}
		e.syncEngineStateFromHandleResult(userID, hr)
		resp := &WorkflowResponse{}
		switch hr.Action {
		case ActionRunPhase, ActionModify:
			resp.RunAgentLoop = true
			resp.PhasePrompt = BuildPhasePrompt(hr.State)
			if hr.ModifyHint != "" {
				resp.PhasePrompt += "\n\nUser modification request: " + hr.ModifyHint
			}
		case ActionConfirmed:
			if hr.State != nil && hr.State.Status == StatusCompleted {
				resp.Complete = true
			} else {
				resp.Advance = true
				resp.RunAgentLoop = true
				resp.PhasePrompt = BuildPhasePrompt(hr.State)
			}
		case ActionCancelled, ActionCancelAndExecute:
			// handled by caller
		}
		return resp, nil
	}
	// Fallback: basic engine-level implementation.
	e.mu.Lock()
	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return nil, nil
	}
	switch intent {
	case ReviewIntentConfirm:
		ws.PendingReviewPhaseID = ""
		e.mu.Unlock()
		return e.AdvancePhase(userID)
	case ReviewIntentSkip:
		ws.PendingReviewPhaseID = ""
		e.mu.Unlock()
		return e.AdvancePhase(userID)
	case ReviewIntentCancel:
		ws.Status = WorkflowCancelled
		delete(e.workflows, userID)
		e.mu.Unlock()
		return &WorkflowResponse{}, nil
	case ReviewIntentSupplement:
		ws.PendingReviewRevisionRequested = true
		ws.UpdatedAt = time.Now()
		e.mu.Unlock()
		tmpl := e.registry.Match(ws.Type)
		if tmpl != nil && ws.PhaseIndex < len(tmpl.Phases) {
			phase := &tmpl.Phases[ws.PhaseIndex]
			prompt := phase.Prompt
			if prompt == "" {
				prompt = phase.Name
			}
			return &WorkflowResponse{PhasePrompt: prompt + "\n\nUser feedback: " + feedback, RunAgentLoop: true}, nil
		}
		return &WorkflowResponse{RunAgentLoop: true}, nil
	default:
		e.mu.Unlock()
		return nil, nil
	}
}
func (e *WorkflowEngine) ReopenPhaseForRevision(userID, phaseID, feedback string) (*WorkflowResponse, error) {
	if e == nil {
		return nil, errors.New("workflow engine is nil")
	}
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return nil, errors.New("workflow not active")
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return nil, errors.New("workflow template not found")
	}
	phaseID = CanonicalPhaseID(phaseID)
	phaseIndex := -1
	for i, phase := range tmpl.Phases {
		if CanonicalPhaseID(phase.ID) == phaseID {
			phaseIndex = i
			phaseID = phase.ID
			break
		}
	}
	if phaseIndex < 0 {
		return nil, errors.New("workflow phase not found: " + phaseID)
	}
	e.mu.Lock()
	ws.PhaseIndex = phaseIndex
	ws.CurrentPhase = phaseID
	ws.PendingReviewPhaseID = phaseID
	ws.PendingReviewRevisionRequested = true
	ws.UpdatedAt = time.Now()
	if ws.GateResults == nil {
		ws.GateResults = map[string]*QualityGateResult{}
	}
	if strings.TrimSpace(feedback) != "" {
		ws.GateResults[phaseID] = &QualityGateResult{PhaseID: phaseID, Passed: false, Items: []GateCheckItem{{Description: "revision requested", Passed: false, Note: feedback}}, CheckedAt: ws.UpdatedAt}
	}
	e.mu.Unlock()
	if e.store != nil {
		_ = e.store.SaveWorkflowState(ws)
	}
	if e.callbacks != nil {
		_ = e.callbacks.EmitPhaseUpdate(userID, ws)
	}
	return &WorkflowResponse{RunAgentLoop: true, PhasePrompt: e.BuildPhasePrompt(userID)}, nil
}

func (e *WorkflowEngine) syncEngineStateFromHandleResult(userID string, hr *HandleResult) {
	if e == nil || hr == nil || hr.State == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	ws := e.workflows[userID]
	if ws == nil {
		return
	}
	switch hr.State.Status {
	case StatusCompleted:
		ws.Status = WorkflowCompleted
		ws.PendingReviewPhaseID = ""
		ws.UpdatedAt = time.Now()
		return
	case StatusCancelled:
		ws.Status = WorkflowCancelled
		delete(e.workflows, userID)
		return
	}
	if hr.State.Status != StatusActive {
		return
	}
	ws.Status = WorkflowActive
	ws.PhaseIndex = hr.State.CurrentPhase
	if hr.State.CurrentPhase >= 0 && hr.State.CurrentPhase < len(hr.State.Phases) {
		ws.CurrentPhase = hr.State.Phases[hr.State.CurrentPhase].ID
	}
	ws.PendingReviewPhaseID = ""
	ws.UpdatedAt = time.Now()
	switch hr.Action {
	case ActionModify:
		ws.PendingReviewRevisionRequested = true
		if ws.PhaseOutputs != nil && ws.CurrentPhase != "" {
			delete(ws.PhaseOutputs, ws.CurrentPhase)
		}
	case ActionRunPhase, ActionConfirmed:
		ws.PendingReviewRevisionRequested = false
		ws.PhaseFormSubmitted = false
		ws.PhaseFormSkipped = false
	}
}

func (e *WorkflowEngine) SyncActiveStateFromHandleResult(userID string, hr *HandleResult) {
	e.syncEngineStateFromHandleResult(userID, hr)
}

func (e *WorkflowEngine) MarkPhasePendingReview(userID, phaseID string, revisionRequested bool) {
	if e == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(phaseID) == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive || ws.CurrentPhase != phaseID {
		return
	}
	ws.PendingReviewPhaseID = phaseID
	ws.PendingReviewRevisionRequested = revisionRequested
	ws.UpdatedAt = time.Now()
}

func (e *WorkflowEngine) GetActiveWorkflow(userID string) *EngineState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ws := e.workflows[userID]
	if ws != nil && ws.Status == WorkflowActive {
		return ws
	}
	return nil
}
func (e *WorkflowEngine) ActiveWorkflowUserIDForPhase(phaseID string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	matched := ""
	for uid, ws := range e.workflows {
		if ws.Status == WorkflowActive && ws.CurrentPhase == phaseID {
			if matched != "" {
				return "", false
			}
			matched = uid
		}
	}
	return matched, matched != ""
}
func (e *WorkflowEngine) SingleActiveWorkflowUserID() (string, bool) { return "", false }
func (e *WorkflowEngine) BuildPhasePrompt(userID string) string {
	if e == nil {
		return ""
	}
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return ""
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return ""
	}
	state := &WorkflowState{
		ID:           ws.ID,
		UserID:       ws.UserID,
		Type:         string(ws.Type),
		ProjectPath:  ws.ProjectPath,
		Summary:      ws.Intent.Summary,
		CurrentPhase: ws.PhaseIndex,
		Status:       ws.Status,
		CreatedAt:    ws.CreatedAt,
		UpdatedAt:    ws.UpdatedAt,
		Phases:       make([]Phase, 0, len(tmpl.Phases)),
	}
	for i, spec := range tmpl.Phases {
		kind, mutationScope, _ := phaseMetadataSemantics(tmpl.Type, CanonicalPhaseID(spec.ID))
		phase := Phase{
			ID:            spec.ID,
			Name:          spec.Name,
			NeedsConfirm:  spec.NeedsConfirm,
			ToolPolicy:    spec.ToolPolicy,
			Kind:          firstPhaseKind(spec.Kind, kind),
			MutationScope: firstMutationScope(spec.MutationScope, mutationScope),
			InputSchema:   phaseInputSchemaFromSpec(spec.InputSchema),
			DependsOnFull: spec.DependsOnFull,
			Output:        ws.PhaseOutputs[spec.ID],
		}
		switch {
		case i < ws.PhaseIndex:
			phase.Status = PhaseCompleted
		case i == ws.PhaseIndex:
			switch {
			case ws.PendingReviewPhaseID == spec.ID:
				phase.Status = PhaseWaitingConfirm
			case spec.InputSchema != nil && !ws.PhaseFormSubmitted && !ws.PhaseFormSkipped:
				phase.Status = PhaseRunning
			default:
				phase.Status = PhaseRunning
			}
			if spec.InputSchema != nil && ws.PhaseFormData != nil && ws.CurrentPhase == spec.ID {
				phase.FormData = ws.PhaseFormData
			}
		default:
			phase.Status = PhasePending
		}
		state.Phases = append(state.Phases, phase)
	}
	return BuildPhasePrompt(state)
}

func phaseInputSchemaFromSpec(spec *PhaseInputSchemaSpec) *PhaseInputSchema {
	if spec == nil {
		return nil
	}
	schema := &PhaseInputSchema{
		Title:         spec.Title,
		Description:   spec.Description,
		Fields:        make([]PhaseInputField, 0, len(spec.Fields)),
		Variants:      make([]PhaseInputVariant, 0, len(spec.Variants)),
		AcceptsResume: spec.AcceptsResume,
	}
	for _, field := range spec.Fields {
		schema.Fields = append(schema.Fields, phaseInputFieldFromSpec(field))
	}
	for _, variant := range spec.Variants {
		cloned := PhaseInputVariant{
			ID:     variant.ID,
			Label:  variant.Label,
			Fields: make([]PhaseInputField, 0, len(variant.Fields)),
		}
		for _, field := range variant.Fields {
			cloned.Fields = append(cloned.Fields, phaseInputFieldFromSpec(field))
		}
		schema.Variants = append(schema.Variants, cloned)
	}
	if spec.AcceptsSupplementary != nil {
		schema.AcceptsSupplementary = &SupplementaryDocConfig{
			Label:         spec.AcceptsSupplementary.Label,
			Description:   spec.AcceptsSupplementary.Description,
			MaxFiles:      spec.AcceptsSupplementary.MaxFiles,
			AcceptedTypes: append([]string(nil), spec.AcceptsSupplementary.AcceptedTypes...),
		}
	}
	return schema
}
func (e *WorkflowEngine) GetPhaseToolFilter(userID string) ToolFilterPolicy {
	return e.GetActivePhaseToolFilter(userID)
}
func (e *WorkflowEngine) GetActivePhaseToolFilter(userID string) ToolFilterPolicy {
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return ToolFilterNone
	}
	if ws.PendingReviewPhaseID != "" && ws.PendingReviewPhaseID == ws.CurrentPhase {
		return ToolFilterNone
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return ToolFilterNone
	}
	for _, phase := range tmpl.Phases {
		if phase.ID == ws.CurrentPhase {
			if ws.Type == WorkflowCoding && phase.ID == PhaseCodingTaskBreakdown {
				return ToolFilterPlanning
			}
			return phase.ToolPolicy
		}
	}
	return ToolFilterNone
}
func (e *WorkflowEngine) IsActivePhaseExecutionOrchestrator(userID string) bool {
	if e == nil {
		return false
	}
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return false
	}
	registry := e.GetRegistry()
	if registry == nil {
		return false
	}
	tmpl := registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return false
	}
	phase := tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase {
		return false
	}
	return IsTemplatePhaseExecutionOrchestrator(tmpl, phase)
}
func (e *WorkflowEngine) IsPhaseExecutionBlocked(userID string) bool {
	if e == nil {
		return false
	}
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return false
	}
	if ws.PendingReviewPhaseID != "" && ws.PendingReviewPhaseID == ws.CurrentPhase {
		return true
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return false
	}
	phase := tmpl.Phases[ws.PhaseIndex]
	return phase.InputSchema != nil && !ws.PhaseFormSubmitted && !ws.PhaseFormSkipped
}
func (e *WorkflowEngine) GetOpsApprovedCommands(userID string) []OpsApprovedCommand { return nil }
func (e *WorkflowEngine) HasPhaseOutput(userID string) bool {
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return false
	}
	return ws.PhaseOutputs[ws.CurrentPhase] != ""
}
func (e *WorkflowEngine) IsPhaseNeedsConfirm(userID string) bool {
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return false
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return false
	}
	for _, p := range tmpl.Phases {
		if p.ID == ws.CurrentPhase {
			return p.NeedsConfirm
		}
	}
	return false
}
func (e *WorkflowEngine) IsAwaitingReview(userID string) bool {
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return false
	}
	return ws.PendingReviewPhaseID != ""
}
func (e *WorkflowEngine) RestoreFromStore() error                       { return nil }
func (e *WorkflowEngine) CleanupExpired() error                         { return nil }
func (e *WorkflowEngine) SetCallbacks(cb EngineCallbacks)               { e.callbacks = cb }
func (e *WorkflowEngine) GetCallbacks() EngineCallbacks                 { return e.callbacks }
func (e *WorkflowEngine) GetFilter() *QuickFilter                       { return e.filter }
func (e *WorkflowEngine) GetUnderstanding() *IntentUnderstandingManager { return e.understanding }
func (e *WorkflowEngine) GetRegistry() *WorkflowRegistry                { return e.registry }
func (e *WorkflowEngine) SavePhaseOutput(userID, content string) (string, error) {
	if e == nil {
		return "", nil
	}
	e.mu.Lock()
	ws := e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return "", nil
	}
	phaseID := ws.CurrentPhase
	workflowType := ws.Type
	e.mu.Unlock()

	// Also record in V2 machine before mutating the legacy engine state. This
	// keeps invalid phase outputs from becoming pending-confirm through the
	// engine compatibility path.
	var phaseKind PhaseKind
	if e.machine != nil {
		state := e.machine.GetActive(userID)
		if state != nil {
			activePhase := state.ActivePhase()
			if state.Type != string(workflowType) || activePhase == nil || activePhase.ID != phaseID {
				machinePhaseID := ""
				if activePhase != nil {
					machinePhaseID = activePhase.ID
				}
				return phaseID, fmt.Errorf("workflow state mismatch: engine type=%s phase=%s, v2 type=%s phase=%s", workflowType, phaseID, state.Type, machinePhaseID)
			}
			phaseKind = activePhase.Kind
		}
	}

	// Use Kind-aware sanitization so validation sees the same result as
	// RecordOutput's storage path. This prevents new artifact phases from
	// failing validation due to legacy phaseID list not covering them.
	sanitizedContent := SanitizePhaseOutputWithKind(phaseID, phaseKind, content)

	// When V2 machine exists, delegate validation entirely to RecordOutput
	// (which is the authoritative state owner). This avoids double-validation
	// and double-logging. RecordOutput already treats validation failure as
	// advisory for NeedsConfirm phases.
	if e.machine != nil && e.machine.GetActive(userID) != nil {
		if err := e.machine.RecordOutput(userID, content); err != nil {
			return phaseID, err
		}
		if state := e.machine.GetActive(userID); state != nil {
			for i := range state.Phases {
				if state.Phases[i].ID == phaseID && state.Phases[i].Output != "" {
					sanitizedContent = state.Phases[i].Output
					break
				}
			}
		}
	} else {
		// No V2 machine — validate here (legacy path).
		if err := validatePhaseOutputForCompletion(string(workflowType), phaseID, sanitizedContent); err != nil {
			phaseNeedsConfirm := false
			if tmpl := e.registry.Match(workflowType); tmpl != nil {
				for _, phase := range tmpl.Phases {
					if phase.ID == phaseID {
						phaseNeedsConfirm = phase.NeedsConfirm
						break
					}
				}
			}
			if !phaseNeedsConfirm {
				return phaseID, err
			}
			log.Printf("[workflow-engine] SavePhaseOutput: validation advisory (phase=%s NeedsConfirm=true): %v", phaseID, err)
		}
	}

	phaseNeedsConfirm := false
	if tmpl := e.registry.Match(workflowType); tmpl != nil {
		for _, phase := range tmpl.Phases {
			if phase.ID == phaseID {
				phaseNeedsConfirm = phase.NeedsConfirm
				break
			}
		}
	}

	e.mu.Lock()
	ws = e.workflows[userID]
	if ws == nil || ws.Status != WorkflowActive {
		e.mu.Unlock()
		return phaseID, nil
	}
	if ws.PhaseOutputs == nil {
		ws.PhaseOutputs = make(map[string]string)
	}
	ws.PhaseOutputs[phaseID] = sanitizedContent
	if phaseNeedsConfirm {
		ws.PendingReviewPhaseID = phaseID
	} else {
		ws.PendingReviewPhaseID = ""
	}
	ws.UpdatedAt = time.Now()
	e.mu.Unlock()
	return phaseID, nil
}
func (e *WorkflowEngine) SavePhaseOutputAndMaybeAdvance(userID, content string) (string, *WorkflowResponse, error) {
	phaseID, err := e.SavePhaseOutput(userID, content)
	if err != nil {
		return phaseID, nil, err
	}
	// If the phase needs confirm, set pending review state instead of auto-advancing.
	if e.IsPhaseNeedsConfirm(userID) {
		e.mu.Lock()
		if ws := e.workflows[userID]; ws != nil {
			ws.PendingReviewPhaseID = ws.CurrentPhase
		}
		e.mu.Unlock()
		// Sync WaitingConfirm status to V2 machine.
		if e.machine != nil {
			if state := e.machine.GetActive(userID); state != nil {
				if p := state.ActivePhase(); p != nil {
					p.Status = PhaseWaitingConfirm
					e.machine.GetStore().Save(state)
				}
			}
		}
		return phaseID, &WorkflowResponse{PendingConfirm: true}, nil
	}
	// Auto-advance for non-confirm phases.
	resp, advErr := e.AdvancePhase(userID)
	return phaseID, resp, advErr
}
func (e *WorkflowEngine) GetInputRequirement(userID string) *InputRequirement {
	if e == nil || e.registry == nil {
		return nil
	}
	ws := e.GetActiveWorkflow(userID)
	if ws == nil {
		return nil
	}
	tmpl := e.registry.Match(ws.Type)
	if tmpl == nil {
		return nil
	}
	return tmpl.RequiresInput
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// PhaseMeta / PhaseMetadata
// ---------------------------------------------------------------------------

// PhaseMeta is the dashboard-facing projection of a PhaseTemplate.
type PhaseMeta struct {
	ID                    string           `json:"id"`
	Name                  string           `json:"name"`
	Index                 int              `json:"index"`
	ExpectsDocument       bool             `json:"expects_document"`
	CanSkip               bool             `json:"can_skip"`
	NeedsConfirm          bool             `json:"needs_confirm"`
	Kind                  PhaseKind        `json:"kind,omitempty"`
	ToolPolicy            ToolFilterPolicy `json:"tool_policy,omitempty"`
	MutationScope         MutationScope    `json:"mutation_scope"`
	ActivatesOrchestrator bool             `json:"activates_orchestrator"`
}

// PhaseMetadata returns the phase metadata for a workflow template spec.
func PhaseMetadata(tmpl *TemplateSpec) []PhaseMeta {
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tmpl.Phases))
	metas := make([]PhaseMeta, 0, len(tmpl.Phases))
	for _, phase := range tmpl.Phases {
		id := CanonicalPhaseID(phase.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		derivedKind, derivedMutationScope, activatesOrchestrator := phaseMetadataSemantics(tmpl.Type, id)
		toolPolicy := phase.ToolPolicy
		needsConfirm := phase.NeedsConfirm
		canSkip := phase.CanSkip
		expectsDocument := phase.NeedsConfirm
		mutationScope := firstMutationScope(phase.MutationScope, derivedMutationScope)
		if tmpl.Type == WorkflowCoding {
			switch id {
			case "tasks":
				if toolPolicy == "" || toolPolicy == ToolPolicyDocOnly {
					toolPolicy = ToolPolicyPlanning
				}
				needsConfirm = true
				canSkip = true
				expectsDocument = true
			case "review":
				toolPolicy = ToolPolicyDocOnly
				needsConfirm = true
				canSkip = true
				expectsDocument = true
				mutationScope = MutationScopeWorkflowDoc
			}
		}
		metas = append(metas, PhaseMeta{
			ID:                    id,
			Name:                  phase.Name,
			Index:                 len(metas),
			ExpectsDocument:       expectsDocument,
			NeedsConfirm:          needsConfirm,
			CanSkip:               canSkip,
			Kind:                  firstPhaseKind(phase.Kind, derivedKind),
			ToolPolicy:            toolPolicy,
			MutationScope:         mutationScope,
			ActivatesOrchestrator: activatesOrchestrator,
		})
	}
	if len(metas) == 0 {
		return nil
	}
	return metas
}

func phaseMetadataSemantics(workflowType WorkflowType, phaseID string) (PhaseKind, MutationScope, bool) {
	switch workflowType {
	case WorkflowCoding:
		switch phaseID {
		case "requirements", "design":
			return PhaseKindDocumentPlanning, MutationScopeWorkflowDoc, false
		case "tasks":
			return PhaseKindCodePlanning, MutationScopeWorkflowDoc, false
		case "implementation":
			return PhaseKindExecution, MutationScopeProject, true
		case "review":
			return PhaseKindReview, MutationScopeWorkflowDoc, false
		}
	case WorkflowPresentationDesign:
		if phaseID == "ppt_generation" {
			return PhaseKindArtifactGeneration, MutationScopeArtifact, false
		}
	}
	return PhaseKindUnknown, MutationScopeUnknown, false
}

func firstPhaseKind(values ...PhaseKind) PhaseKind {
	for _, value := range values {
		if value != PhaseKindUnknown {
			return value
		}
	}
	return PhaseKindUnknown
}

func firstMutationScope(values ...MutationScope) MutationScope {
	for _, value := range values {
		if value != MutationScopeUnknown {
			return value
		}
	}
	return MutationScopeUnknown
}

// ---------------------------------------------------------------------------
// CanonicalPhaseID / ParseReviewIntent / IsTemplatePhaseExecutionOrchestrator
// ---------------------------------------------------------------------------

// CanonicalPhaseID normalizes a phase ID string.
func CanonicalPhaseID(phaseID string) string {
	switch strings.ToLower(strings.TrimSpace(phaseID)) {
	case "requirements", "requirement", "req":
		return "requirements"
	case "design", "tech_design", "technical_design":
		return "design"
	case "tasks", "task", "task_plan", "task_breakdown":
		return "tasks"
	case "verification", "verify", "review", "acceptance":
		return "review"
	default:
		return phaseID
	}
}

// ParseReviewIntent converts a raw string to a typed ReviewIntent.
func ParseReviewIntent(raw string) ReviewIntent {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "confirm", "confirmed", "yes", "ok", "确认", "好":
		return ReviewIntentConfirm
	case "supplement", "modify", "revise", "修改", "补充":
		return ReviewIntentSupplement
	case "skip", "跳过":
		return ReviewIntentSkip
	case "cancel", "取消":
		return ReviewIntentCancel
	case "switch_task", "switch", "换任务", "新任务":
		return ReviewIntentSwitchTask
	default:
		return ReviewIntentOther
	}
}

// IsTemplatePhaseExecutionOrchestrator returns false (stub).
func IsTemplatePhaseExecutionOrchestrator(tmpl *TemplateSpec, phase PhaseSpec) bool {
	if tmpl == nil || phase.DisableOrchestrator {
		return false
	}
	kind, _, activatesOrchestrator := phaseMetadataSemantics(tmpl.Type, CanonicalPhaseID(phase.ID))
	phaseKind := firstPhaseKind(phase.Kind, kind)
	return activatesOrchestrator || phaseKind == PhaseKindExecution || phaseKind == PhaseKindOpsExecution
}

// IsExecutionOrchestratorPhase returns false (stub).
// Deprecated: only retained for interface compat. Callers should use
// IsTemplatePhaseExecutionOrchestrator instead.
func IsExecutionOrchestratorPhase(_ PhaseSpec) bool { return false }

// ---------------------------------------------------------------------------
// Misc functions
// ---------------------------------------------------------------------------

// BuildPhaseSystemPrompt is deprecated — use BuildPhasePrompt(state) in phase_prompt.go.
func BuildPhaseSystemPrompt(_ *EngineState, _ *PhaseSpec, _ *WorkflowRegistry) string {
	return ""
}

// BuildQualityGatePrompt returns empty string (stub).
func BuildQualityGatePrompt(_ *PhaseSpec, _ string) string { return "" }

// RunQualityGate returns a passing result (stub).
func RunQualityGate(phase *PhaseSpec, output string) *QualityGateResult {
	if phase == nil || len(phase.Checklist) == 0 {
		return &QualityGateResult{Passed: true, CheckedAt: time.Now()}
	}
	items := make([]GateCheckItem, len(phase.Checklist))
	for i, desc := range phase.Checklist {
		items[i] = GateCheckItem{Description: desc, Passed: true}
	}
	return &QualityGateResult{
		PhaseID:   phase.ID,
		Passed:    true,
		Items:     items,
		CheckedAt: time.Now(),
	}
}

// NullStore is an alias for NullPersistenceStore for backward compat.
type NullStore = NullPersistenceStore
