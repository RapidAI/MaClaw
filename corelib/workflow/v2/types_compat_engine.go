package v2

// types_compat_engine.go — V1 engine/registry/understanding compat stubs.
//
// These types exist solely for backward compatibility with consumers that
// reference workflow.WorkflowEngine, workflow.WorkflowRegistry, etc.
// All runtime behavior has been migrated to V2 StateMachine.
// These are placeholder stubs that satisfy type references and compilation.

import (
	"errors"
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
// WorkflowStatus aliases (V1 used WorkflowActive/Completed/Cancelled)
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
	return &StartResult{Rejected: true}, nil
}
func (m *IntentUnderstandingManager) HandleInput(userID, text string) (string, bool, bool, *StructuredIntent, error) {
	return "", false, false, nil, nil
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

// WorkflowRegistry holds registered workflow templates (stubbed).
type WorkflowRegistry struct {
	mu        sync.RWMutex
	templates map[WorkflowType]*V1WorkflowTemplate
}

// NewWorkflowRegistry creates a WorkflowRegistry.
func NewWorkflowRegistry() *WorkflowRegistry {
	return &WorkflowRegistry{
		templates: make(map[WorkflowType]*V1WorkflowTemplate),
	}
}

func (r *WorkflowRegistry) Register(tmpl *V1WorkflowTemplate) error {
	if tmpl == nil || tmpl.Type == "" {
		return errors.New("invalid template")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[tmpl.Type] = tmpl
	return nil
}

func (r *WorkflowRegistry) MustRegister(tmpl *V1WorkflowTemplate) {
	if err := r.Register(tmpl); err != nil {
		panic(err)
	}
}

func (r *WorkflowRegistry) Match(wt WorkflowType) *V1WorkflowTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates[wt]
}

func (r *WorkflowRegistry) All() []*V1WorkflowTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*V1WorkflowTemplate, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}
	return result
}

func (r *WorkflowRegistry) AllDescriptions() string                             { return "" }
func (r *WorkflowRegistry) BestTemplateScore(text string) float64               { return 0 }
func (r *WorkflowRegistry) BestTemplateType(text string) WorkflowType           { return "" }
func (r *WorkflowRegistry) RankedTemplateScores(text string) []TemplateScore    { return nil }
func (r *WorkflowRegistry) MatchesAnyTemplate(text string) bool                 { return false }

// ---------------------------------------------------------------------------
// WorkflowEngine (stub)
// ---------------------------------------------------------------------------

// WorkflowEngine is the V1 engine type retained for backward compatibility.
// All runtime behavior has been migrated to V2 StateMachine.
type WorkflowEngine struct {
	mu            sync.RWMutex
	workflows     map[string]*V1WorkflowState
	registry      *WorkflowRegistry
	understanding *IntentUnderstandingManager
	store         PersistenceStore
	callbacks     EngineCallbacks
	filter        *QuickFilter
	artifactSaver ArtifactSaver
}

// NewWorkflowEngine creates a WorkflowEngine stub.
func NewWorkflowEngine(
	registry *WorkflowRegistry,
	understanding *IntentUnderstandingManager,
	store PersistenceStore,
	callbacks EngineCallbacks,
) *WorkflowEngine {
	e := &WorkflowEngine{
		workflows:     make(map[string]*V1WorkflowState),
		registry:      registry,
		understanding: understanding,
		store:         store,
		callbacks:     callbacks,
	}
	e.filter = NewQuickFilter(e)
	return e
}

func (e *WorkflowEngine) SetArtifactSaver(s ArtifactSaver) { e.artifactSaver = s }
func (e *WorkflowEngine) HasActiveWorkflow(userID string) bool {
	return e.GetActiveWorkflow(userID) != nil
}
func (e *WorkflowEngine) HasActiveUnderstanding(userID string) bool { return false }
func (e *WorkflowEngine) StartWorkflow(userID string, intent StructuredIntent) (*V1WorkflowState, error) {
	return e.StartWorkflowWithOptions(userID, intent, WorkflowStartOptions{})
}
func (e *WorkflowEngine) StartWorkflowWithOptions(userID string, intent StructuredIntent, options WorkflowStartOptions) (*V1WorkflowState, error) {
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
	state := &V1WorkflowState{
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
	return state, nil
}
func (e *WorkflowEngine) SetProjectPath(userID, projectPath string) error { return nil }
func (e *WorkflowEngine) HandleInput(userID, text string) (*WorkflowResponse, error) {
	return nil, nil
}
func (e *WorkflowEngine) SubmitPhaseForm(userID string, formData map[string]interface{}) (*WorkflowResponse, error) {
	return nil, nil
}
func (e *WorkflowEngine) SubmitInputPayload(userID string, payload *WorkflowInputPayload) (*WorkflowResponse, error) {
	return nil, nil
}
func (e *WorkflowEngine) SkipPhaseForm(userID string) error                          { return nil }
func (e *WorkflowEngine) AdvancePhase(userID string) (*WorkflowResponse, error)      { return nil, nil }
func (e *WorkflowEngine) CancelWorkflow(userID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ws, ok := e.workflows[userID]; ok {
		ws.Status = WorkflowCancelled
		delete(e.workflows, userID)
	}
	return nil
}
func (e *WorkflowEngine) ApplyReviewIntent(userID string, intent ReviewIntent, feedback string) (*WorkflowResponse, error) {
	return nil, nil
}
func (e *WorkflowEngine) ReopenPhaseForRevision(userID, phaseID, feedback string) (*WorkflowResponse, error) {
	return nil, nil
}
func (e *WorkflowEngine) GetActiveWorkflow(userID string) *V1WorkflowState {
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
	for uid, ws := range e.workflows {
		if ws.Status == WorkflowActive && ws.CurrentPhase == phaseID {
			return uid, true
		}
	}
	return "", false
}
func (e *WorkflowEngine) SingleActiveWorkflowUserID() (string, bool)                  { return "", false }
func (e *WorkflowEngine) BuildPhasePrompt(userID string) string                       { return "" }
func (e *WorkflowEngine) GetPhaseToolFilter(userID string) ToolFilterPolicy            { return ToolFilterNone }
func (e *WorkflowEngine) GetActivePhaseToolFilter(userID string) ToolFilterPolicy      { return ToolFilterNone }
func (e *WorkflowEngine) GetActivePhaseContract(userID string) (PhaseContract, bool)   { return PhaseContract{}, false }
func (e *WorkflowEngine) GetPhaseRuntimeGate(userID string) (PhaseRuntimeGate, bool)   { return PhaseRuntimeGate{}, false }
func (e *WorkflowEngine) IsActivePhaseExecutionOrchestrator(userID string) bool        { return false }
func (e *WorkflowEngine) IsPhaseExecutionBlocked(userID string) bool                   { return false }
func (e *WorkflowEngine) GetOpsApprovedCommands(userID string) []OpsApprovedCommand    { return nil }
func (e *WorkflowEngine) HasPhaseOutput(userID string) bool                            { return false }
func (e *WorkflowEngine) IsPhaseNeedsConfirm(userID string) bool                       { return false }
func (e *WorkflowEngine) IsAwaitingReview(userID string) bool                          { return false }
func (e *WorkflowEngine) RestoreFromStore() error                                      { return nil }
func (e *WorkflowEngine) CleanupExpired() error                                        { return nil }
func (e *WorkflowEngine) SetCallbacks(cb EngineCallbacks)                              { e.callbacks = cb }
func (e *WorkflowEngine) GetCallbacks() EngineCallbacks                                { return e.callbacks }
func (e *WorkflowEngine) GetFilter() *QuickFilter                                      { return e.filter }
func (e *WorkflowEngine) GetUnderstanding() *IntentUnderstandingManager                { return e.understanding }
func (e *WorkflowEngine) GetRegistry() *WorkflowRegistry                               { return e.registry }
func (e *WorkflowEngine) SavePhaseOutput(userID, content string) (string, error)       { return "", nil }
func (e *WorkflowEngine) SavePhaseOutputAndMaybeAdvance(userID, content string) (string, *WorkflowResponse, error) {
	return "", nil, nil
}
func (e *WorkflowEngine) GetInputRequirement(userID string) *InputRequirement { return nil }

// ---------------------------------------------------------------------------
// PhaseContract / PhaseRuntimeGate
// ---------------------------------------------------------------------------

// PhaseContract is the static, template-derived capability contract for one phase.
type PhaseContract struct {
	PhaseID                  string
	Kind                     PhaseKind
	ToolPolicy               ToolFilterPolicy
	ExpectsDocument          bool
	RequiresReview           bool
	RequiresStructuredForm   bool
	MutationScope            MutationScope
	AllowsRepoInspection     bool
	AllowsProjectMutation    bool
	AllowsDelegation         bool
	UsesSystemDocPersistence bool
	ActivatesOrchestrator    bool
}

// PhaseRuntimeGate combines the static phase contract with current WorkflowState gates.
type PhaseRuntimeGate struct {
	Contract                PhaseContract
	WaitingForWorkflowInput bool
	WaitingForPhaseForm     bool
	AwaitingReview          bool
	BlocksAgentLoop         bool
}

// PhaseContractFromPolicy constructs a PhaseContract from a policy (stub).
func PhaseContractFromPolicy(_ ToolFilterPolicy, _ MutationScope) PhaseContract {
	return PhaseContract{}
}

// IsToolAllowedByContract returns true (stub — all tools allowed).
func IsToolAllowedByContract(_ PhaseContract, _ string) bool { return true }

// FilterToolDefinitionsByContract returns the input tools unchanged (stub).
func FilterToolDefinitionsByContract(_ PhaseContract, tools []map[string]interface{}) []map[string]interface{} {
	return tools
}

// RequiredToolNamesForContract returns nil (stub).
func RequiredToolNamesForContract(_ PhaseContract) []string { return nil }

// ValidateToolCallByContract returns nil (stub — all calls valid).
func ValidateToolCallByContract(_ PhaseContract, _ string, _ map[string]interface{}) error {
	return nil
}

// ValidateToolCallByContractWithApproval returns nil (stub).
func ValidateToolCallByContractWithApproval(_ PhaseContract, _ string, _ map[string]interface{}, _ []OpsApprovedCommand) error {
	return nil
}

// DerivePhaseContract returns an empty PhaseContract (stub).
func DerivePhaseContract(_ *V1WorkflowTemplate, _ V1PhaseTemplate) PhaseContract {
	return PhaseContract{}
}

// DeriveWorkflowContracts returns nil (stub).
func DeriveWorkflowContracts(_ *V1WorkflowTemplate) []PhaseContract { return nil }

// DerivePhaseRuntimeGate returns an empty PhaseRuntimeGate (stub).
func DerivePhaseRuntimeGate(_ *V1WorkflowTemplate, _ *V1WorkflowState) PhaseRuntimeGate {
	return PhaseRuntimeGate{}
}

// ValidateWorkflowTemplateContract returns nil (stub).
func ValidateWorkflowTemplateContract(_ *V1WorkflowTemplate) []error { return nil }

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

// PhaseMetadata returns the phase metadata for a V1 workflow template.
func PhaseMetadata(tmpl *V1WorkflowTemplate) []PhaseMeta {
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
		metas = append(metas, PhaseMeta{
			ID:            id,
			Name:          phase.Name,
			Index:         len(metas),
			NeedsConfirm:  phase.NeedsConfirm,
			CanSkip:       phase.CanSkip,
			ToolPolicy:    phase.ToolPolicy,
			MutationScope: phase.MutationScope,
		})
	}
	if len(metas) == 0 {
		return nil
	}
	return metas
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
func IsTemplatePhaseExecutionOrchestrator(_ *V1WorkflowTemplate, _ V1PhaseTemplate) bool {
	return false
}

// IsExecutionOrchestratorPhase returns false (stub).
func IsExecutionOrchestratorPhase(_ V1PhaseTemplate) bool { return false }

// PhaseExpectsDocument returns false (stub).
func PhaseExpectsDocument(_ V1PhaseTemplate) bool { return false }

// ---------------------------------------------------------------------------
// Misc functions
// ---------------------------------------------------------------------------

// GetToolFilterForPhase returns the ToolPolicy from a V1PhaseTemplate.
func GetToolFilterForPhase(phase *V1PhaseTemplate) ToolFilterPolicy {
	if phase == nil {
		return ToolFilterNone
	}
	return phase.ToolPolicy
}

// BuildPhaseSystemPrompt returns empty string (stub).
func BuildPhaseSystemPrompt(_ *V1WorkflowState, _ *V1PhaseTemplate, _ *WorkflowRegistry) string {
	return ""
}

// BuildQualityGatePrompt returns empty string (stub).
func BuildQualityGatePrompt(_ *V1PhaseTemplate, _ string) string { return "" }

// RunQualityGate returns a passing result (stub).
func RunQualityGate(phase *V1PhaseTemplate, output string) *QualityGateResult {
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

// DefaultMutationScopeForPolicy returns MutationScopeNone (stub).
func DefaultMutationScopeForPolicy(_ ToolFilterPolicy) MutationScope { return MutationScopeNone }

// NullStore is an alias for V1NullStore for backward compat.
type NullStore = V1NullStore
