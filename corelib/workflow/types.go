package workflow

import "time"

// WorkflowType identifies the kind of workflow template.
type WorkflowType string

const (
	// WorkflowNone indicates the task is NOT a workflow (e.g., content processing).
	WorkflowNone WorkflowType = "none"

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
	WorkflowBidResponse         WorkflowType = "bid_response"
	WorkflowContractReview      WorkflowType = "contract_review"
	WorkflowDueDiligence        WorkflowType = "due_diligence"
	WorkflowComplianceAudit     WorkflowType = "compliance_audit"
	WorkflowPatentAnalysis      WorkflowType = "patent_analysis"
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

// InputRequirement describes a document/file that the user must provide
// before a workflow can begin its first phase. This is a template-level
// declaration — the engine uses it to prompt the user for upload and to
// gate phase execution until the input is received.
//
// Workflow types that need input documents (bid response, contract review,
// compliance audit, patent analysis, etc.) declare this field. The engine
// handles the upload prompt, waiting state, and transition uniformly.
type InputRequirement struct {
	// Description is a user-facing explanation of what document is needed.
	// Example: "请上传发包方的招标文件（PDF/Word/图片均可）"
	Description string `json:"description"`

	// FileTypes lists accepted file extensions for display purposes.
	// Example: ["pdf", "docx", "doc", "png", "jpg"]
	FileTypes []string `json:"file_types,omitempty"`

	// AcceptText indicates the user can also paste text content directly
	// instead of uploading a file.
	AcceptText bool `json:"accept_text,omitempty"`

	// AnalysisHint is an optional instruction appended to the first phase
	// prompt, guiding the LLM on how to analyze the uploaded document.
	// If empty, the first phase's Prompt is used as-is after the document
	// is received.
	AnalysisHint string `json:"analysis_hint,omitempty"`
}

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

	// RequiresInput declares that this workflow needs the user to provide
	// an input document before the first phase can execute. When set, the
	// engine will prompt the user for upload and wait until the document
	// is received. Nil means no input document is required (the default
	// for most workflow types).
	RequiresInput *InputRequirement `json:"requires_input,omitempty"`
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

	// DefaultInput is true when the engine's HandleInput fell through to
	// the default branch (no confirm/skip/modify match). The caller can
	// use this to decide whether to capture the agent loop output as
	// workflow phase content. When true, the message may be unrelated to
	// the workflow (e.g. a weather query while a coding workflow is active).
	DefaultInput bool
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

	// InputReceived is true when the user has provided the required input
	// document (for workflows with RequiresInput). The engine sets this
	// when it detects a document upload or substantial text input during
	// the waiting-for-input state.
	InputReceived bool `json:"input_received,omitempty"`
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

// NeedsInputDocument returns true if the workflow template requires the user
// to provide an input document before the first phase can execute.
func (t *WorkflowTemplate) NeedsInputDocument() bool {
	return t != nil && t.RequiresInput != nil && t.RequiresInput.Description != ""
}

// IsWaitingForInput returns true if the workflow is in the "waiting for user
// to upload input document" state: the template requires input, but the user
// hasn't provided it yet, and we're still on the first phase.
func (ws *WorkflowState) IsWaitingForInput(tmpl *WorkflowTemplate) bool {
	if ws == nil || tmpl == nil {
		return false
	}
	return tmpl.NeedsInputDocument() && !ws.InputReceived && ws.PhaseIndex == 0
}
