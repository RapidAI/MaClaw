package workflow

// engine_compat.go — V1 workflow engine compatibility layer.
//
// V2 (corelib/workflow/v2/) is the sole active workflow engine at runtime.
// This file retains V1 type definitions, constructors, and method signatures
// so that GUI production code (which references *WorkflowEngine in function
// signatures and nil-guards) and 50+ test files continue to compile.
//
// At runtime, the GUI's workflowEngine field is NEVER initialized (the
// initWorkflowEngine call is commented out). All code paths that reference
// workflowEngine check for nil first. TUI no longer initializes or uses V1.
//
// Do NOT add new functionality here. New workflow features belong in V2.
//
// This file was formerly named engine_stub.go (deleted per task 17e).

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrIntentUnderstandingContractBreach is returned when the IUM LLM completely
// violates the structured JSON contract (e.g. produces capability denial text).
var ErrIntentUnderstandingContractBreach = errors.New("intent understanding LLM contract breach")

// ---------------------------------------------------------------------------
// WorkflowEngine
// ---------------------------------------------------------------------------

// WorkflowEngine is the V1 workflow state-machine engine (stubbed out).
type WorkflowEngine struct {
	mu            sync.RWMutex
	workflows     map[string]*WorkflowState
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
		workflows:     make(map[string]*WorkflowState),
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
func (e *WorkflowEngine) StartWorkflow(userID string, intent StructuredIntent) (*WorkflowState, error) {
	return e.StartWorkflowWithOptions(userID, intent, WorkflowStartOptions{})
}
func (e *WorkflowEngine) StartWorkflowWithOptions(userID string, intent StructuredIntent, options WorkflowStartOptions) (*WorkflowState, error) {
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
	state := &WorkflowState{
		ID:           "wf_" + sanitizeWorkflowIDPart(userID) + "_" + now.Format("20060102150405"),
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
	if e.workflows == nil {
		e.workflows = make(map[string]*WorkflowState)
	}
	e.workflows[userID] = state
	e.mu.Unlock()
	if e.store != nil {
		_ = e.store.SaveWorkflowState(state)
	}
	return state, nil
}
func (e *WorkflowEngine) SetProjectPath(userID, projectPath string) error { return nil }
func (e *WorkflowEngine) HandleInput(userID, text string) (*WorkflowResponse, error) {
	state := e.GetActiveWorkflow(userID)
	if state == nil {
		return nil, nil
	}
	phase := e.activePhase(userID)
	if phase == nil {
		return nil, nil
	}
	if phase.InputSchema != nil && !state.PhaseFormSubmitted && !state.PhaseFormSkipped {
		return &WorkflowResponse{Text: phase.Description, ShowForm: true, FormSchema: phase.InputSchema}, nil
	}
	return &WorkflowResponse{
		Text:         phase.Description,
		PhasePrompt:  phase.Prompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
		DefaultInput: true,
	}, nil
}
func (e *WorkflowEngine) SubmitPhaseForm(userID string, formData map[string]interface{}) (*WorkflowResponse, error) {
	state := e.GetActiveWorkflow(userID)
	if state == nil {
		return nil, errors.New("no active workflow")
	}
	state.PhaseFormData = formData
	state.PhaseFormSubmitted = true
	phase := e.activePhase(userID)
	if phase == nil {
		return nil, nil
	}
	return &WorkflowResponse{
		Text:         phase.Description,
		PhasePrompt:  phase.Prompt,
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
	}, nil
}
func (e *WorkflowEngine) SubmitInputPayload(userID string, payload *WorkflowInputPayload) (*WorkflowResponse, error) {
	state := e.GetActiveWorkflow(userID)
	if state == nil {
		return nil, errors.New("no active workflow")
	}
	state.InputReceived = true
	state.InputPayload = payload.Clone()
	phase := e.activePhase(userID)
	if phase == nil {
		return nil, nil
	}
	if phase.InputSchema != nil && !state.PhaseFormSubmitted && !state.PhaseFormSkipped {
		return &WorkflowResponse{Text: phase.Description, ShowForm: true, FormSchema: phase.InputSchema}, nil
	}
	return &WorkflowResponse{
		Text:         phase.Description,
		PhasePrompt:  phase.Prompt + "\n\nInput evidence: " + workflowInputPayloadSummary(payload),
		ToolFilter:   phase.ToolPolicy,
		RunAgentLoop: true,
	}, nil
}
func (e *WorkflowEngine) SkipPhaseForm(userID string) error {
	if state := e.GetActiveWorkflow(userID); state != nil {
		state.PhaseFormSkipped = true
	}
	return nil
}
func (e *WorkflowEngine) AdvancePhase(userID string) (*WorkflowResponse, error) { return nil, nil }
func (e *WorkflowEngine) CancelWorkflow(userID string) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	delete(e.workflows, userID)
	e.mu.Unlock()
	return nil
}
func (e *WorkflowEngine) ApplyReviewIntent(userID string, intent ReviewIntent, feedback string) (*WorkflowResponse, error) {
	return nil, nil
}
func (e *WorkflowEngine) ReopenPhaseForRevision(userID, phaseID, feedback string) (*WorkflowResponse, error) {
	return nil, nil
}
func (e *WorkflowEngine) GetActiveWorkflow(userID string) *WorkflowState {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	state := e.workflows[userID]
	e.mu.RUnlock()
	if state == nil || state.Status != WorkflowActive {
		return nil
	}
	return state
}
func (e *WorkflowEngine) ActiveWorkflowUserIDForPhase(phaseID string) (string, bool) {
	return "", false
}
func (e *WorkflowEngine) SingleActiveWorkflowUserID() (string, bool) { return "", false }
func (e *WorkflowEngine) BuildPhasePrompt(userID string) string      { return "" }
func (e *WorkflowEngine) GetPhaseToolFilter(userID string) ToolFilterPolicy {
	return e.GetActivePhaseToolFilter(userID)
}
func (e *WorkflowEngine) GetActivePhaseToolFilter(userID string) ToolFilterPolicy {
	phase := e.activePhase(userID)
	if phase == nil {
		return ToolFilterNone
	}
	return GetToolFilterForPhase(phase)
}
func (e *WorkflowEngine) GetActivePhaseContract(userID string) (PhaseContract, bool) {
	phase := e.activePhase(userID)
	if phase == nil {
		return PhaseContract{}, false
	}
	return PhaseContractFromPolicy(GetToolFilterForPhase(phase), phase.MutationScope), true
}
func (e *WorkflowEngine) GetPhaseRuntimeGate(userID string) (PhaseRuntimeGate, bool) {
	return PhaseRuntimeGate{}, false
}
func (e *WorkflowEngine) IsActivePhaseExecutionOrchestrator(userID string) bool { return false }
func (e *WorkflowEngine) IsPhaseExecutionBlocked(userID string) bool            { return false }
func (e *WorkflowEngine) GetOpsApprovedCommands(userID string) []OpsApprovedCommand {
	state := e.GetActiveWorkflow(userID)
	if state == nil {
		return nil
	}
	return ExtractOpsApprovedCommands(state.PhaseOutputs["risk_policy"])
}
func (e *WorkflowEngine) HasPhaseOutput(userID string) bool { return false }
func (e *WorkflowEngine) IsPhaseNeedsConfirm(userID string) bool {
	phase := e.activePhase(userID)
	return phase != nil && phase.NeedsConfirm
}
func (e *WorkflowEngine) IsAwaitingReview(userID string) bool           { return false }
func (e *WorkflowEngine) RestoreFromStore() error                       { return nil }
func (e *WorkflowEngine) CleanupExpired() error                         { return nil }
func (e *WorkflowEngine) SetCallbacks(cb EngineCallbacks)               { e.callbacks = cb }
func (e *WorkflowEngine) GetCallbacks() EngineCallbacks                 { return e.callbacks }
func (e *WorkflowEngine) GetFilter() *QuickFilter                       { return e.filter }
func (e *WorkflowEngine) GetUnderstanding() *IntentUnderstandingManager { return e.understanding }
func (e *WorkflowEngine) GetRegistry() *WorkflowRegistry                { return e.registry }
func (e *WorkflowEngine) SavePhaseOutput(userID, content string) (string, error) {
	return "", nil
}
func (e *WorkflowEngine) SavePhaseOutputAndMaybeAdvance(userID, content string) (string, *WorkflowResponse, error) {
	return "", nil, nil
}
func (e *WorkflowEngine) GetInputRequirement(userID string) *InputRequirement { return nil }

func (e *WorkflowEngine) activePhase(userID string) *PhaseTemplate {
	state := e.GetActiveWorkflow(userID)
	if state == nil || e == nil || e.registry == nil {
		return nil
	}
	tmpl := e.registry.Match(state.Type)
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return nil
	}
	idx := state.PhaseIndex
	if idx < 0 || idx >= len(tmpl.Phases) {
		for i := range tmpl.Phases {
			if tmpl.Phases[i].ID == state.CurrentPhase {
				idx = i
				break
			}
		}
	}
	if idx < 0 || idx >= len(tmpl.Phases) {
		return nil
	}
	return &tmpl.Phases[idx]
}

func sanitizeWorkflowIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "user"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 32 {
		return out[:32]
	}
	return out
}

// WorkflowStartOptions carries durable context for workflow creation.
type WorkflowStartOptions struct {
	ProjectPath string
}

// ---------------------------------------------------------------------------
// WorkflowRegistry
// ---------------------------------------------------------------------------

// WorkflowRegistry holds registered workflow templates (stubbed).
type WorkflowRegistry struct {
	mu        sync.RWMutex
	templates map[WorkflowType]*WorkflowTemplate
}

// TemplateScore is a ranked advisory score for a template document.
type TemplateScore struct {
	Type  WorkflowType
	Score float64
}

func NewWorkflowRegistry() *WorkflowRegistry {
	r := &WorkflowRegistry{templates: make(map[WorkflowType]*WorkflowTemplate)}
	RegisterBuiltinTemplates(r)
	return r
}

func (r *WorkflowRegistry) Register(tmpl *WorkflowTemplate) error {
	if r == nil || tmpl == nil {
		return nil
	}
	r.mu.Lock()
	if r.templates == nil {
		r.templates = make(map[WorkflowType]*WorkflowTemplate)
	}
	r.templates[tmpl.Type] = tmpl
	r.mu.Unlock()
	return nil
}

func (r *WorkflowRegistry) MustRegister(tmpl *WorkflowTemplate) {
	_ = r.Register(tmpl)
}

func (r *WorkflowRegistry) Match(wt WorkflowType) *WorkflowTemplate {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	tmpl := r.templates[wt]
	r.mu.RUnlock()
	return tmpl
}

func (r *WorkflowRegistry) All() []*WorkflowTemplate {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*WorkflowTemplate, 0, len(r.templates))
	for _, tmpl := range r.templates {
		if tmpl != nil {
			out = append(out, tmpl)
		}
	}
	return out
}

func (r *WorkflowRegistry) AllDescriptions() string { return "" }

func (r *WorkflowRegistry) BestTemplateScore(text string) float64            { return 0 }
func (r *WorkflowRegistry) BestTemplateType(text string) WorkflowType        { return "" }
func (r *WorkflowRegistry) RankedTemplateScores(text string) []TemplateScore { return nil }

// ---------------------------------------------------------------------------
// RegisterBuiltinTemplates
// ---------------------------------------------------------------------------

// RegisterBuiltinTemplates is a no-op stub. All workflow templates are now
// registered exclusively in V2's templates.go (corelib/workflow/v2/templates.go).
// This function signature is retained so that NewWorkflowRegistry() and any
// other callers continue to compile. Template data should be sourced from V2.
func RegisterBuiltinTemplates(r *WorkflowRegistry) {
	// Intentionally empty — V2 is the single source of truth for templates.
}

// ---------------------------------------------------------------------------
// IntentUnderstandingManager
// ---------------------------------------------------------------------------

// IntentUnderstandingManager handles multi-round intent clarification (stubbed).
type IntentUnderstandingManager struct {
	mu       sync.RWMutex
	sessions map[string]*UnderstandingSession
	store    PersistenceStore
	llm      LLMCaller
	registry *WorkflowRegistry
	lang     string
	userLang map[string]string
}

// StartResult holds the outcome of Start().
type StartResult struct {
	Reply    string
	Rejected bool
	Ready    bool
	Intent   *StructuredIntent
}

func NewIntentUnderstandingManager(store PersistenceStore, llm LLMCaller, registry *WorkflowRegistry) *IntentUnderstandingManager {
	return &IntentUnderstandingManager{
		sessions: make(map[string]*UnderstandingSession),
		userLang: make(map[string]string),
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
	raw, err := m.llm.DoSimpleLLMRequest([]interface{}{map[string]interface{}{"role": "user", "content": text}}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	result, err := parseStartResult(raw)
	if err != nil {
		return nil, err
	}
	if !result.Rejected && !result.Ready {
		m.mu.Lock()
		if m.sessions == nil {
			m.sessions = make(map[string]*UnderstandingSession)
		}
		now := time.Now()
		m.sessions[userID] = &UnderstandingSession{
			ID:        "ium_" + sanitizeWorkflowIDPart(userID) + "_" + now.Format("20060102150405"),
			UserID:    userID,
			State:     UnderstandingActive,
			Rounds:    []UnderstandingRound{{UserText: text, AssistantText: result.Reply, Timestamp: now}},
			CreatedAt: now,
			UpdatedAt: now,
		}
		m.mu.Unlock()
	}
	return result, nil
}
func (m *IntentUnderstandingManager) HandleInput(userID, text string) (string, bool, bool, *StructuredIntent, error) {
	if m == nil || m.llm == nil {
		return "", false, true, nil, nil
	}
	raw, err := m.llm.DoSimpleLLMRequest([]interface{}{map[string]interface{}{"role": "user", "content": text}}, 30*time.Second)
	if err != nil {
		return "", false, false, nil, err
	}
	result, err := parseStartResult(raw)
	if err != nil {
		return "", false, false, nil, err
	}
	if result.Ready || result.Rejected {
		m.CancelSession(userID)
	}
	return result.Reply, result.Ready, result.Rejected, result.Intent, nil
}
func (m *IntentUnderstandingManager) GetSession(userID string) *UnderstandingSession {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[userID]
}
func (m *IntentUnderstandingManager) HasActiveSession(userID string) bool {
	return m.GetSession(userID) != nil
}
func (m *IntentUnderstandingManager) CleanupExpired()                    {}
func (m *IntentUnderstandingManager) RestoreSession(userID string) error { return nil }
func (m *IntentUnderstandingManager) CancelSession(userID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.sessions, userID)
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// QuickFilter
// ---------------------------------------------------------------------------

// WorkflowChecker is the minimal interface QuickFilter needs from the engine.
type WorkflowChecker interface {
	HasActiveWorkflow(userID string) bool
	HasActiveUnderstanding(userID string) bool
}

// QuickFilter routes deterministic workflow state (stubbed).
type QuickFilter struct {
	engine WorkflowChecker
}

func NewQuickFilter(engine WorkflowChecker) *QuickFilter {
	return &QuickFilter{engine: engine}
}

func (f *QuickFilter) Classify(userID, text string) FilterResult {
	if f.engine != nil {
		if f.engine.HasActiveWorkflow(userID) {
			return FilterActiveWorkflow
		}
		if f.engine.HasActiveUnderstanding(userID) {
			return FilterActiveUnderstanding
		}
	}
	if strings.TrimSpace(text) == "" {
		return FilterSimpleDirective
	}
	return FilterNeedsUnderstanding
}

// ---------------------------------------------------------------------------
// Prompt builder
// ---------------------------------------------------------------------------

func BuildPhaseSystemPrompt(_ *WorkflowState, _ *PhaseTemplate, _ *WorkflowRegistry) string {
	return ""
}

func BuildQualityGatePrompt(_ *PhaseTemplate, _ string) string { return "" }

func GetToolFilterForPhase(phase *PhaseTemplate) ToolFilterPolicy {
	if phase == nil {
		return ToolFilterNone
	}
	return phase.ToolPolicy
}

// ---------------------------------------------------------------------------
// Execution phase
// ---------------------------------------------------------------------------

func IsExecutionOrchestratorPhase(_ PhaseTemplate) bool                              { return false }
func IsTemplatePhaseExecutionOrchestrator(_ *WorkflowTemplate, _ PhaseTemplate) bool { return false }

// ---------------------------------------------------------------------------
// Review intent
// ---------------------------------------------------------------------------

func ParseReviewIntent(raw string) ReviewIntent {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ReviewIntentConfirm):
		return ReviewIntentConfirm
	case string(ReviewIntentSupplement), "modify":
		return ReviewIntentSupplement
	case string(ReviewIntentSkip):
		return ReviewIntentSkip
	case string(ReviewIntentCancel):
		return ReviewIntentCancel
	case string(ReviewIntentSwitchTask):
		return ReviewIntentSwitchTask
	default:
		return ReviewIntentOther
	}
}

// ---------------------------------------------------------------------------
// Quality gate
// ---------------------------------------------------------------------------

func RunQualityGate(phase *PhaseTemplate, output string) *QualityGateResult {
	if phase == nil || len(phase.Checklist) == 0 || strings.TrimSpace(output) == "" {
		return nil
	}
	items := make([]GateCheckItem, 0, len(phase.Checklist))
	for _, desc := range phase.Checklist {
		items = append(items, GateCheckItem{Description: desc, Passed: false, Note: "requires review confirmation"})
	}
	return &QualityGateResult{PhaseID: phase.ID, Passed: false, Items: items, CheckedAt: time.Now()}
}

// ---------------------------------------------------------------------------
// Tool policy
// ---------------------------------------------------------------------------

func IsToolAllowedByPolicy(policy ToolFilterPolicy, name string) bool {
	name = strings.TrimSpace(name)
	switch policy {
	case ToolFilterDocOnly:
		return DocOnlyAllowedTools[name]
	case ToolFilterPlanning:
		return PlanningAllowedTools[name]
	case ToolFilterOpsControlled:
		return OpsControlledAllowedTools[name]
	default:
		return true
	}
}

func FilterToolDefinitions(policy ToolFilterPolicy, tools []map[string]interface{}) []map[string]interface{} {
	if policy == ToolFilterNone || policy == ToolFilterFull || len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		nameVal := workflowToolDefinitionName(def)
		if IsToolAllowedByPolicy(policy, nameVal) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func workflowToolDefinitionName(def map[string]interface{}) string {
	if def == nil {
		return ""
	}
	if name, _ := def["name"].(string); strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if fn, _ := def["function"].(map[string]interface{}); fn != nil {
		name, _ := fn["name"].(string)
		return strings.TrimSpace(name)
	}
	return ""
}

func RequiredToolNamesForPolicy(policy ToolFilterPolicy) []string {
	switch policy {
	case ToolFilterFull:
		return []string{"bash", "read_file", "list_directory", "write_file", "edit_file"}
	case ToolFilterDocOnly:
		return []string{"read_file", "list_directory", "send_file"}
	case ToolFilterPlanning:
		return []string{"read_file", "list_directory", "send_file"}
	case ToolFilterOpsControlled:
		return []string{"bash", "ssh", "read_file", "list_directory"}
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// Tool contract policy
// ---------------------------------------------------------------------------

func DefaultMutationScopeForPolicy(_ ToolFilterPolicy) MutationScope { return MutationScopeNone }
func PhaseContractFromPolicy(_ ToolFilterPolicy, _ MutationScope) PhaseContract {
	return PhaseContract{}
}
func IsToolAllowedByContract(_ PhaseContract, _ string) bool { return true }
func FilterToolDefinitionsByContract(_ PhaseContract, tools []map[string]interface{}) []map[string]interface{} {
	return tools
}
func RequiredToolNamesForContract(_ PhaseContract) []string { return nil }
func ValidateToolCallByContract(_ PhaseContract, _ string, _ map[string]interface{}) error {
	return nil
}
func ValidateToolCallByContractWithApproval(_ PhaseContract, _ string, _ map[string]interface{}, _ []OpsApprovedCommand) error {
	return nil
}

// ---------------------------------------------------------------------------
// Phase contract
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

func DerivePhaseContract(_ *WorkflowTemplate, _ PhaseTemplate) PhaseContract { return PhaseContract{} }
func DeriveWorkflowContracts(_ *WorkflowTemplate) []PhaseContract            { return nil }
func DerivePhaseRuntimeGate(_ *WorkflowTemplate, _ *WorkflowState) PhaseRuntimeGate {
	return PhaseRuntimeGate{}
}
func ValidateWorkflowTemplateContract(_ *WorkflowTemplate) []error { return nil }

// ---------------------------------------------------------------------------
// Phase metadata
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

func PhaseExpectsDocument(_ PhaseTemplate) bool { return false }

func PhaseMetadata(tmpl *WorkflowTemplate) []PhaseMeta {
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
// Ops command policy
// ---------------------------------------------------------------------------

type OpsCommandRisk string

const (
	OpsCommandRiskReadOnly OpsCommandRisk = "read_only"
	OpsCommandRiskMutating OpsCommandRisk = "mutating"
	OpsCommandRiskHigh     OpsCommandRisk = "high_risk"
)

type OpsCommandAssessment struct {
	Command string
	Risk    OpsCommandRisk
	Reason  string
}

type OpsApprovedCommand struct {
	Tool                string                 `json:"tool"`
	Action              string                 `json:"action,omitempty"`
	Target              string                 `json:"target,omitempty"`
	Command             string                 `json:"command"`
	RiskLevel           OpsRiskLevel           `json:"risk_level,omitempty"`
	ApprovalRequirement OpsApprovalRequirement `json:"approval_required,omitempty"`
}

type OpsRiskDecision string

const (
	OpsRiskDecisionUnknown          OpsRiskDecision = ""
	OpsRiskDecisionDocumentOnly     OpsRiskDecision = "document_only"
	OpsRiskDecisionPropose          OpsRiskDecision = "propose"
	OpsRiskDecisionApprovalRequired OpsRiskDecision = "approval_required"
	OpsRiskDecisionAutoExecute      OpsRiskDecision = "auto_execute"
	OpsRiskDecisionDeny             OpsRiskDecision = "deny"
)

type OpsRiskLevel string

const (
	OpsRiskLevelUnknown OpsRiskLevel = ""
	OpsRiskLevelL0      OpsRiskLevel = "L0"
	OpsRiskLevelL1      OpsRiskLevel = "L1"
	OpsRiskLevelL2      OpsRiskLevel = "L2"
	OpsRiskLevelL3      OpsRiskLevel = "L3"
	OpsRiskLevelL4      OpsRiskLevel = "L4"
)

type OpsApprovalRequirement string

const (
	OpsApprovalRequirementUnknown OpsApprovalRequirement = ""
	OpsApprovalRequirementNone    OpsApprovalRequirement = "none"
	OpsApprovalRequirementSingle  OpsApprovalRequirement = "single"
	OpsApprovalRequirementDouble  OpsApprovalRequirement = "double"
)

func AssessOpsCommand(_ string) OpsCommandAssessment {
	return OpsCommandAssessment{Risk: OpsCommandRiskReadOnly}
}
func ValidateToolCallByPolicy(policy ToolFilterPolicy, name string, args map[string]interface{}) error {
	return ValidateToolCallByPolicyWithApproval(policy, name, args, nil)
}
func ValidateToolCallByPolicyWithApproval(policy ToolFilterPolicy, name string, args map[string]interface{}, approved []OpsApprovedCommand) error {
	name = strings.TrimSpace(name)
	if !IsToolAllowedByPolicy(policy, name) {
		return fmt.Errorf("%s is not allowed in current workflow phase", name)
	}
	if policy != ToolFilterOpsControlled || (name != "bash" && name != "ssh") {
		return nil
	}
	desc := opsCommandDescriptor(name, args)
	if isHighRiskOpsCommand(desc) {
		return fmt.Errorf("%s requires a reviewed runbook before execution", name)
	}
	if !isMutatingOpsCommand(name, args, desc) {
		return nil
	}
	if len(approved) == 0 {
		return fmt.Errorf("%s requires allowed_commands in the approved risk-policy before execution", name)
	}
	for _, item := range approved {
		if opsApprovedCommandMatches(item, name, args, desc) {
			return nil
		}
	}
	return fmt.Errorf("%s command is outside the approved risk-policy allowed_commands manifest", name)
}

func ExtractOpsApprovedCommands(text string) []OpsApprovedCommand {
	var out []OpsApprovedCommand
	var current *OpsApprovedCommand
	flush := func() {
		if current != nil && (current.Tool != "" || current.Command != "") {
			out = append(out, *current)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			current = &OpsApprovedCommand{}
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		}
		if current == nil || !strings.Contains(trimmed, ":") {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch key {
		case "tool":
			current.Tool = val
		case "action":
			current.Action = val
		case "target":
			current.Target = val
		case "command":
			current.Command = val
		}
	}
	flush()
	return out
}
func ExtractOpsRiskDecision(_ string) OpsRiskDecision { return OpsRiskDecisionUnknown }
func ExtractOpsRiskLevel(_ string) OpsRiskLevel       { return OpsRiskLevelUnknown }
func ExtractOpsApprovalRequirement(_ string) OpsApprovalRequirement {
	return OpsApprovalRequirementUnknown
}
func OpsRiskDecisionAllowsExecution(_ OpsRiskDecision, _ OpsRiskLevel, _ OpsApprovalRequirement) bool {
	return false
}
func OpsApprovalDigest(_ string) string { return "" }

func parseStartResult(raw string) (*StartResult, error) {
	var payload struct {
		Reply    string            `json:"reply"`
		Rejected bool              `json:"rejected"`
		Ready    bool              `json:"ready"`
		Intent   *StructuredIntent `json:"intent"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return nil, ErrIntentUnderstandingContractBreach
	}
	if payload.Intent != nil {
		payload.Ready = payload.Ready || payload.Intent.Ready
		if payload.Intent.Category == "" || payload.Intent.Category == WorkflowNone {
			payload.Rejected = true
		}
	}
	return &StartResult{Reply: payload.Reply, Rejected: payload.Rejected, Ready: payload.Ready, Intent: payload.Intent}, nil
}

func workflowInputPayloadSummary(payload *WorkflowInputPayload) string {
	if payload == nil {
		return ""
	}
	parts := []string{}
	if strings.TrimSpace(payload.Text) != "" {
		parts = append(parts, strings.TrimSpace(payload.Text))
	}
	for _, att := range payload.Attachments {
		if strings.TrimSpace(att.FileName) != "" {
			parts = append(parts, att.FileName)
		}
	}
	return strings.Join(parts, "\n")
}

func opsCommandDescriptor(name string, args map[string]interface{}) string {
	switch strings.TrimSpace(name) {
	case "bash":
		return strings.TrimSpace(fmt.Sprint(args["command"]))
	case "ssh":
		action := strings.TrimSpace(fmt.Sprint(args["action"]))
		switch action {
		case "upload":
			return strings.TrimSpace(fmt.Sprintf("%s -> %s", args["local_path"], args["remote_path"]))
		case "command", "exec", "run":
			return strings.TrimSpace(fmt.Sprint(args["command"]))
		default:
			if cmd := strings.TrimSpace(fmt.Sprint(args["command"])); cmd != "" && cmd != "<nil>" {
				return cmd
			}
			return action
		}
	default:
		return ""
	}
}

func isHighRiskOpsCommand(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "rm -rf /") ||
		strings.Contains(lower, "--no-preserve-root") ||
		strings.Contains(lower, "mkfs") ||
		strings.Contains(lower, "dd if=")
}

func isMutatingOpsCommand(name string, args map[string]interface{}, desc string) bool {
	if strings.TrimSpace(name) == "ssh" {
		action := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["action"])))
		return action == "upload" || action == "command" || action == "exec" || action == "run"
	}
	lower := strings.ToLower(desc)
	for _, marker := range []string{"restart", "start ", "stop ", "reload", "rm ", "mv ", "cp ", "chmod", "chown", "docker", "kubectl apply", "systemctl"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func opsApprovedCommandMatches(item OpsApprovedCommand, name string, args map[string]interface{}, desc string) bool {
	if strings.TrimSpace(item.Tool) != "" && strings.TrimSpace(item.Tool) != strings.TrimSpace(name) {
		return false
	}
	if action := strings.TrimSpace(item.Action); action != "" && action != strings.TrimSpace(fmt.Sprint(args["action"])) {
		return false
	}
	if target := strings.TrimSpace(item.Target); target != "" &&
		target != strings.TrimSpace(fmt.Sprint(args["session_id"])) &&
		target != strings.TrimSpace(fmt.Sprint(args["target"])) {
		return false
	}
	cmd := strings.TrimSpace(item.Command)
	return cmd != "" && cmd == strings.TrimSpace(desc)
}

// ---------------------------------------------------------------------------
// Persistence stubs
// ---------------------------------------------------------------------------

// NullStore is a no-op PersistenceStore.
type NullStore struct{}

var _ PersistenceStore = (*NullStore)(nil)

func (NullStore) SaveUnderstandingSession(_ *UnderstandingSession) error           { return nil }
func (NullStore) LoadUnderstandingSession(_ string) (*UnderstandingSession, error) { return nil, nil }
func (NullStore) DeleteUnderstandingSession(_ string) error                        { return nil }
func (NullStore) SaveWorkflowState(_ *WorkflowState) error                         { return nil }
func (NullStore) LoadWorkflowState(_ string) (*WorkflowState, error)               { return nil, nil }
func (NullStore) DeleteWorkflowState(_ string) error                               { return nil }
func (NullStore) ListActiveWorkflows() ([]*WorkflowState, error)                   { return nil, nil }
func (NullStore) CleanupExpired(_ time.Duration) error                             { return nil }

// SQLiteStore implements PersistenceStore using SQLite (stubbed).
type SQLiteStore struct{}

func NewSQLiteStore(_ string) (*SQLiteStore, error)                           { return &SQLiteStore{}, nil }
func (s *SQLiteStore) Close() error                                           { return nil }
func (s *SQLiteStore) SaveUnderstandingSession(_ *UnderstandingSession) error { return nil }
func (s *SQLiteStore) LoadUnderstandingSession(_ string) (*UnderstandingSession, error) {
	return nil, nil
}
func (s *SQLiteStore) DeleteUnderstandingSession(_ string) error          { return nil }
func (s *SQLiteStore) SaveWorkflowState(_ *WorkflowState) error           { return nil }
func (s *SQLiteStore) LoadWorkflowState(_ string) (*WorkflowState, error) { return nil, nil }
func (s *SQLiteStore) DeleteWorkflowState(_ string) error                 { return nil }
func (s *SQLiteStore) ListActiveWorkflows() ([]*WorkflowState, error)     { return nil, nil }
func (s *SQLiteStore) CleanupExpired(_ time.Duration) error               { return nil }

// ---------------------------------------------------------------------------
// ExperienceProvider
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// phase_input_field_type helpers (used in engine validation)
// ---------------------------------------------------------------------------

func normalizePhaseInputFieldType(fieldType string) string {
	return strings.ToLower(strings.TrimSpace(fieldType))
}

func isStringPhaseInputFieldType(fieldType string) bool {
	switch normalizePhaseInputFieldType(fieldType) {
	case "", "text", "textarea", "date", "datetime", "file", "directory", "hidden",
		"user_ref", "department_ref", "business_ref":
		return true
	default:
		return false
	}
}

func isSupportedPhaseInputFieldType(fieldType string) bool {
	switch normalizePhaseInputFieldType(fieldType) {
	case "", "text", "textarea", "number", "date", "datetime",
		"select", "multiselect", "boolean", "file", "directory", "hidden",
		"user_ref", "department_ref", "business_ref",
		"object_form", "array_table":
		return true
	default:
		return false
	}
}
