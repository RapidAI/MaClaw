package im

import "time"

// ---------------------------------------------------------------------------
// Workflow type enum
// ---------------------------------------------------------------------------

// WorkflowType identifies the category of a workflow template.
type WorkflowType string

const (
	WorkflowCoding        WorkflowType = "coding"
	WorkflowProductDesign WorkflowType = "product_design"
	WorkflowInnovation    WorkflowType = "innovation"
	WorkflowBusinessPlan  WorkflowType = "business_plan"
)

// ---------------------------------------------------------------------------
// Template structures
// ---------------------------------------------------------------------------

// PhaseTemplate describes a single phase inside a workflow template.
type PhaseTemplate struct {
	ID           string
	Name         string
	Description  string
	Prompt       string
	Deliverable  string
	Checklist    []string
	NeedsConfirm bool
	NeedsDevice  bool
	CanSkip      bool
}

// WorkflowTemplate is the full definition of a workflow type, including its
// ordered list of phases.
type WorkflowTemplate struct {
	Type        WorkflowType
	Name        string
	Description string
	Keywords    []string
	Phases      []PhaseTemplate
}

// ---------------------------------------------------------------------------
// Structured intent (JSON-tagged for LLM parsing)
// ---------------------------------------------------------------------------

// StructuredIntent is the accumulated understanding of the user's request,
// built up over multiple rounds of LLM-assisted clarification.
type StructuredIntent struct {
	Category      WorkflowType `json:"category"`
	Summary       string       `json:"summary"`
	Goals         []string     `json:"goals"`
	Constraints   []string     `json:"constraints"`
	OpenQuestions []string     `json:"open_questions"`
	Confidence    float64      `json:"confidence"`
	Ready         bool         `json:"ready"`
}

// ---------------------------------------------------------------------------
// Understanding session
// ---------------------------------------------------------------------------

// UnderstandingRound records one exchange between the user and the assistant
// during the intent-understanding phase.
type UnderstandingRound struct {
	UserText      string    `json:"user_text"`
	AssistantText string    `json:"assistant_text"`
	Timestamp     time.Time `json:"timestamp"`
}

// UnderstandingState tracks the lifecycle of an understanding session.
type UnderstandingState string

const (
	UnderstandingActive    UnderstandingState = "active"
	UnderstandingConfirmed UnderstandingState = "confirmed"
	UnderstandingCancelled UnderstandingState = "cancelled"
)

// UnderstandingSession holds the full state of a multi-round intent
// clarification conversation.
type UnderstandingSession struct {
	ID        string
	UserID    string
	Intent    StructuredIntent
	Rounds    []UnderstandingRound
	State     UnderstandingState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ---------------------------------------------------------------------------
// Workflow state
// ---------------------------------------------------------------------------

// WorkflowState tracks the runtime progress of an active workflow instance.
type WorkflowState struct {
	ID           string
	UserID       string
	Type         WorkflowType
	TemplateRef  WorkflowType
	Intent       StructuredIntent
	CurrentPhase string
	PhaseOutputs map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ---------------------------------------------------------------------------
// Workflow response
// ---------------------------------------------------------------------------

// WorkflowRouteAction describes a device-routing action to be performed by
// the workflow engine (e.g. sending a task to a specific device).
type WorkflowRouteAction struct {
	Action string
	Target string
}

// WorkflowResponse is the return value from workflow engine operations,
// carrying the text to show the user plus control flags.
type WorkflowResponse struct {
	Text        string
	Advance     bool
	Complete    bool
	RouteAction *WorkflowRouteAction
}
