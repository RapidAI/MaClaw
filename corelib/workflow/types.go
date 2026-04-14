package workflow

import "time"

// WorkflowType identifies the kind of workflow template.
type WorkflowType string

const (
	WorkflowCoding          WorkflowType = "coding"
	WorkflowProductDesign   WorkflowType = "product_design"
	WorkflowInnovation      WorkflowType = "innovation"
	WorkflowBusinessPlan    WorkflowType = "business_plan"
	WorkflowTesting         WorkflowType = "testing"
	WorkflowLiteratureReview WorkflowType = "literature_review"
	WorkflowResearchReport  WorkflowType = "research_report"
	WorkflowExperimentDesign   WorkflowType = "experiment_design"
	WorkflowGrantProposal      WorkflowType = "grant_proposal"
	WorkflowPaperWriting       WorkflowType = "paper_writing"
	WorkflowProjectProposal    WorkflowType = "project_proposal"
	WorkflowEventPlanning      WorkflowType = "event_planning"
	WorkflowCompetitiveAnalysis WorkflowType = "competitive_analysis"
	WorkflowPresentationDesign  WorkflowType = "presentation_design"
)

// StructuredIntent is the output of the intent understanding phase.
type StructuredIntent struct {
	Category      WorkflowType `json:"category"`
	Summary       string       `json:"summary"`
	Goals         []string     `json:"goals"`
	Constraints   []string     `json:"constraints"`
	OpenQuestions []string     `json:"open_questions"`
	Confidence    float64      `json:"confidence"`
	Ready         bool         `json:"ready"`
}

// ToolFilterPolicy controls which tools are available during a phase.
type ToolFilterPolicy string

const (
	ToolFilterNone    ToolFilterPolicy = "none"     // no tool restrictions
	ToolFilterDocOnly ToolFilterPolicy = "doc_only"  // documentation tools only
	ToolFilterFull    ToolFilterPolicy = "full"      // full tool list
)

// PhaseTemplate defines a single phase within a workflow template.
type PhaseTemplate struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Prompt       string           `json:"prompt"`
	Deliverable  string           `json:"deliverable"`
	Checklist    []string         `json:"checklist"`
	NeedsConfirm bool            `json:"needs_confirm"`
	CanSkip      bool             `json:"can_skip"`
	ToolPolicy   ToolFilterPolicy `json:"tool_policy"`
}

// WorkflowTemplate defines a complete workflow with ordered phases.
type WorkflowTemplate struct {
	Type        WorkflowType    `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Keywords    []string        `json:"keywords"`
	Phases      []PhaseTemplate `json:"phases"`
}

// WorkflowResponse is the engine's response to a user input during a workflow.
type WorkflowResponse struct {
	Text         string             // reply text for the user
	PhasePrompt  string             // system prompt to inject into runAgentLoop
	ToolFilter   ToolFilterPolicy   // tool filtering policy for this response
	RunAgentLoop bool               // whether to invoke runAgentLoop
	Advance      bool               // whether to advance to the next phase
	Complete     bool               // whether the workflow is complete
	DocContent   string             // document content for frontend preview
	GateResult   *QualityGateResult // quality gate check result, if any
}

// WorkflowStatus tracks the lifecycle state of a workflow.
type WorkflowStatus string

const (
	WorkflowActive    WorkflowStatus = "active"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowCancelled WorkflowStatus = "cancelled"
)

// WorkflowState holds the runtime state of an active workflow.
type WorkflowState struct {
	ID           string                       `json:"id"`
	UserID       string                       `json:"user_id"`
	Type         WorkflowType                 `json:"type"`
	Intent       StructuredIntent             `json:"intent"`
	CurrentPhase string                       `json:"current_phase"`
	PhaseIndex   int                          `json:"phase_index"`
	PhaseOutputs map[string]string            `json:"phase_outputs"`
	GateResults  map[string]*QualityGateResult `json:"gate_results"`
	Status       WorkflowStatus               `json:"status"`
	CreatedAt    time.Time                    `json:"created_at"`
	UpdatedAt    time.Time                    `json:"updated_at"`
}

// QualityGateResult records the outcome of a phase quality gate check.
type QualityGateResult struct {
	PhaseID   string          `json:"phase_id"`
	Passed    bool            `json:"passed"`
	Items     []GateCheckItem `json:"items"`
	CheckedAt time.Time       `json:"checked_at"`
}

// GateCheckItem is a single item in a quality gate checklist.
type GateCheckItem struct {
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Note        string `json:"note,omitempty"`
}

// FilterResult classifies an incoming user message.
type FilterResult string

const (
	FilterSmallTalk           FilterResult = "small_talk"
	FilterSimpleDirective     FilterResult = "simple_directive"
	FilterActiveWorkflow      FilterResult = "active_workflow"
	FilterActiveUnderstanding FilterResult = "active_understanding"
	FilterNeedsUnderstanding  FilterResult = "needs_understanding"
)

// UnderstandingState tracks the lifecycle of an intent understanding session.
type UnderstandingState string

const (
	UnderstandingActive    UnderstandingState = "active"
	UnderstandingConfirmed UnderstandingState = "confirmed"
	UnderstandingCancelled UnderstandingState = "cancelled"
	UnderstandingExpired   UnderstandingState = "expired"
)

// UnderstandingSession holds the state of a multi-round intent clarification.
type UnderstandingSession struct {
	ID        string             `json:"id"`
	UserID    string             `json:"user_id"`
	Intent    StructuredIntent   `json:"intent"`
	Rounds    []UnderstandingRound `json:"rounds"`
	State     UnderstandingState `json:"state"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// UnderstandingRound records one exchange in an intent understanding session.
type UnderstandingRound struct {
	UserText      string    `json:"user_text"`
	AssistantText string    `json:"assistant_text"`
	Timestamp     time.Time `json:"timestamp"`
}

// EngineCallbacks defines the adapter interface that GUI/TUI must implement
// to receive notifications from the workflow engine.
type EngineCallbacks interface {
	// SendTextToUser sends a text message to the user.
	SendTextToUser(userID, text string) error
	// EmitPhaseUpdate notifies the frontend of a phase change.
	EmitPhaseUpdate(userID string, state *WorkflowState) error
	// EmitDocUpdate notifies the frontend of document content changes.
	EmitDocUpdate(userID, phaseID, content string) error
	// EmitGateResult notifies the frontend of a quality gate result.
	EmitGateResult(userID, phaseID string, result *QualityGateResult) error
}

// LLMCaller abstracts LLM invocation for testability.
type LLMCaller interface {
	DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error)
}

// PersistenceStore abstracts workflow state persistence.
type PersistenceStore interface {
	// Understanding sessions
	SaveUnderstandingSession(session *UnderstandingSession) error
	LoadUnderstandingSession(userID string) (*UnderstandingSession, error)
	DeleteUnderstandingSession(userID string) error

	// Workflow states
	SaveWorkflowState(state *WorkflowState) error
	LoadWorkflowState(userID string) (*WorkflowState, error)
	DeleteWorkflowState(id string) error
	ListActiveWorkflows() ([]*WorkflowState, error)

	// Cleanup
	CleanupExpired(olderThan time.Duration) error
}
