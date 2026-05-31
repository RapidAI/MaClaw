package workflow

import "time"

// WorkflowType identifies the kind of workflow template.
type WorkflowType string

const (
	// WorkflowNone indicates the task is NOT a workflow (e.g., content processing).
	WorkflowNone WorkflowType = "none"

	WorkflowCoding                  WorkflowType = "coding"
	WorkflowProductDesign           WorkflowType = "product_design"
	WorkflowInnovation              WorkflowType = "innovation"
	WorkflowBusinessPlan            WorkflowType = "business_plan"
	WorkflowTesting                 WorkflowType = "testing"
	WorkflowLiteratureReview        WorkflowType = "literature_review"
	WorkflowResearchReport          WorkflowType = "research_report"
	WorkflowExperimentDesign        WorkflowType = "experiment_design"
	WorkflowGrantProposal           WorkflowType = "grant_proposal"
	WorkflowPaperWriting            WorkflowType = "paper_writing"
	WorkflowProjectProposal         WorkflowType = "project_proposal"
	WorkflowEventPlanning           WorkflowType = "event_planning"
	WorkflowCompetitiveAnalysis     WorkflowType = "competitive_analysis"
	WorkflowPresentationDesign      WorkflowType = "presentation_design"
	WorkflowBidResponse             WorkflowType = "bid_response"
	WorkflowContractReview          WorkflowType = "contract_review"
	WorkflowDueDiligence            WorkflowType = "due_diligence"
	WorkflowComplianceAudit         WorkflowType = "compliance_audit"
	WorkflowPatentAnalysis          WorkflowType = "patent_analysis"
	WorkflowOpsMaintenance          WorkflowType = "ops_maintenance"
	WorkflowChangjiangScholar       WorkflowType = "changjiang_scholar"
	WorkflowChangjiangScholarReview WorkflowType = "changjiang_scholar_review"
)

// Phase IDs for the coding workflow. These are the canonical identifiers
// used in codingTemplate().Phases[].ID. Other code should reference these
// constants instead of hardcoding string literals.
const (
	PhaseCodingRequirements   = "requirements"
	PhaseCodingTechDesign     = "tech_design"
	PhaseCodingTaskBreakdown  = "task_breakdown"
	PhaseCodingImplementation = "implementation"
	PhaseCodingReview         = "review"
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
	ToolFilterNone          ToolFilterPolicy = "none"           // no tool restrictions
	ToolFilterDocOnly       ToolFilterPolicy = "doc_only"       // documentation tools only
	ToolFilterFull          ToolFilterPolicy = "full"           // full tool list
	ToolFilterOpsControlled ToolFilterPolicy = "ops_controlled" // controlled operational execution tools
)

// DocOnlyAllowedTools is the canonical set of tool names permitted during
// doc_only workflow phases. A doc-only phase may gather context and deliver the
// phase document, but it must not mutate the workspace, run shell commands, or
// delegate execution. Both GUI and TUI should reference this set instead of
// maintaining separate copies.
var DocOnlyAllowedTools = map[string]bool{
	"read_file":      true,
	"memory":         true,
	"generate_pdf":   true,
	"office":         true,
	"send_file":      true,
	"web_search":     true,
	"web_fetch":      true,
	"open":           true,
	"set_nickname":   true,
	"list_directory": true,
}

// OpsControlledAllowedTools is the canonical tool set for controlled server
// operations phases. It intentionally excludes generic subagent/task/session
// orchestration tools so operational execution stays inside the risk-gated
// workflow phase.
var OpsControlledAllowedTools = map[string]bool{
	"bash":           true,
	"ssh":            true,
	"read_file":      true,
	"list_directory": true,
	"memory":         true,
	"async_wait":     true,
	"send_file":      true,
	"web_search":     true,
	"web_fetch":      true,
	"set_nickname":   true,
}

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

func (r *InputRequirement) Clone() *InputRequirement {
	if r == nil {
		return nil
	}
	cp := *r
	cp.FileTypes = append([]string(nil), r.FileTypes...)
	return &cp
}

// WorkflowInputAttachment records a user-provided file/image/audio attachment
// that satisfies a template-level input requirement. The engine stores metadata
// instead of raw bytes so workflow state remains small and serializable; the
// agent loop still receives the original attachment payload for analysis.
type WorkflowInputAttachment struct {
	Type     string `json:"type,omitempty"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// WorkflowInputPayload is the durable input contract for document-driven
// workflows. It makes "input received" an explicit state transition with
// inspectable evidence, rather than a boolean inferred from a non-empty chat
// message.
type WorkflowInputPayload struct {
	Text        string                    `json:"text,omitempty"`
	Attachments []WorkflowInputAttachment `json:"attachments,omitempty"`
	ReceivedAt  time.Time                 `json:"received_at,omitempty"`
}

func (p *WorkflowInputPayload) Clone() *WorkflowInputPayload {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Attachments = append([]WorkflowInputAttachment(nil), p.Attachments...)
	return &cp
}

// PhaseInputOption defines a selectable option for select/multiselect fields.
type PhaseInputOption struct {
	Label     string            `json:"label"`
	Value     string            `json:"value"`
	LabelI18N map[string]string `json:"label_i18n,omitempty"`
}

// PhaseInputField defines a single form field for structured information collection.
// It maps directly to AgentViewField on the frontend — same field names, same semantics.
type PhaseInputField struct {
	Name            string             `json:"name"`
	Label           string             `json:"label"`
	Type            string             `json:"type"` // text|textarea|number|date|select|multiselect|boolean|file
	Required        bool               `json:"required,omitempty"`
	Description     string             `json:"description,omitempty"`
	Placeholder     string             `json:"placeholder,omitempty"`
	LabelI18N       map[string]string  `json:"label_i18n,omitempty"`
	DescriptionI18N map[string]string  `json:"description_i18n,omitempty"`
	PlaceholderI18N map[string]string  `json:"placeholder_i18n,omitempty"`
	Options         []PhaseInputOption `json:"options,omitempty"`
	Default         interface{}        `json:"default,omitempty"`
	Min             *float64           `json:"min,omitempty"`
	Max             *float64           `json:"max,omitempty"`
	MinLength       *int               `json:"min_length,omitempty"`
	MaxLength       *int               `json:"max_length,omitempty"`
	Pattern         string             `json:"pattern,omitempty"`
}

// PhaseInputSchema declares a structured form for a phase's information collection.
// When non-nil, the engine emits an AG UI form instead of running the agent loop
// directly. The user's form submission is injected into the PhasePrompt as
// structured context before the LLM generates the phase deliverable.
type PhaseInputSchema struct {
	Title           string            `json:"title"`
	Description     string            `json:"description,omitempty"`
	TitleI18N       map[string]string `json:"title_i18n,omitempty"`
	DescriptionI18N map[string]string `json:"description_i18n,omitempty"`
	Fields          []PhaseInputField `json:"fields"`
}

func (s *PhaseInputSchema) Clone() *PhaseInputSchema {
	if s == nil {
		return nil
	}
	cp := *s
	cp.TitleI18N = cloneStringMap(s.TitleI18N)
	cp.DescriptionI18N = cloneStringMap(s.DescriptionI18N)
	cp.Fields = make([]PhaseInputField, len(s.Fields))
	for i, field := range s.Fields {
		cp.Fields[i] = field
		cp.Fields[i].LabelI18N = cloneStringMap(field.LabelI18N)
		cp.Fields[i].DescriptionI18N = cloneStringMap(field.DescriptionI18N)
		cp.Fields[i].PlaceholderI18N = cloneStringMap(field.PlaceholderI18N)
		cp.Fields[i].Options = append([]PhaseInputOption(nil), field.Options...)
		for j := range cp.Fields[i].Options {
			cp.Fields[i].Options[j].LabelI18N = cloneStringMap(field.Options[j].LabelI18N)
		}
		cp.Fields[i].Default = cloneWorkflowValue(field.Default)
		if field.Min != nil {
			v := *field.Min
			cp.Fields[i].Min = &v
		}
		if field.Max != nil {
			v := *field.Max
			cp.Fields[i].Max = &v
		}
		if field.MinLength != nil {
			v := *field.MinLength
			cp.Fields[i].MinLength = &v
		}
		if field.MaxLength != nil {
			v := *field.MaxLength
			cp.Fields[i].MaxLength = &v
		}
	}
	return &cp
}

// PhaseTemplate defines a single phase within a workflow template.
type PhaseTemplate struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Prompt       string           `json:"prompt"`
	Deliverable  string           `json:"deliverable"`
	Checklist    []string         `json:"checklist"`
	NeedsConfirm bool             `json:"needs_confirm"`
	CanSkip      bool             `json:"can_skip"`
	ToolPolicy   ToolFilterPolicy `json:"tool_policy"`

	// InputSchema declares a structured form for this phase's information
	// collection. When set, the engine signals ShowForm=true on first entry
	// (before PhaseFormData is populated). The GUI emits an AG UI form; the
	// user fills it and submits. The form data is then injected into the
	// PhasePrompt as structured context. Nil means the phase uses natural
	// language interaction (current behavior).
	InputSchema *PhaseInputSchema `json:"input_schema,omitempty"`

	// DisableOrchestrator prevents the generic task orchestrator from trying
	// to parse the previous phase output as a coding task list when this phase
	// enters full-tool execution. Operational workflows use this so the risk
	// policy gate remains the execution boundary.
	DisableOrchestrator bool `json:"disable_orchestrator,omitempty"`
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

	// ShowForm is true when the engine wants the caller to emit an AG UI form
	// for structured information collection. The caller should build the form
	// from FormSchema and emit it via emitAgentView. The engine will not run
	// the agent loop until the form is submitted via SubmitPhaseForm.
	ShowForm bool

	// FormSchema is the phase's InputSchema, provided when ShowForm=true.
	// The caller converts this to an AgentView map and emits it.
	FormSchema *PhaseInputSchema

	// DefaultInput is true when the engine's HandleInput fell through to
	// the default branch (no confirm/skip/modify match). This is a
	// diagnostic signal — it indicates the user's message was not
	// recognized as a workflow control command. The caller (handleActive-
	// Workflow) still sets the workflow marker and stashes the phase
	// prompt, because the phase prompt guides the LLM to produce the
	// deliverable regardless of the user's message content.
	DefaultInput bool

	// PendingReview is true when the phase has output and the next user
	// message must be classified into a ReviewIntent before the workflow can
	// advance, regenerate, skip, cancel, or switch tasks.
	PendingReview bool

	// PendingConfirm is kept for older GUI/TUI callers and mirrors
	// PendingReview. New code should use PendingReview and ApplyReviewIntent.
	PendingConfirm bool

	// ActivateOrchestrator is true when the workflow has advanced into an
	// execution phase (see IsExecutionOrchestratorPhase). The caller should
	// attempt to parse the TaskBreakdownText as a task list and activate
	// the orchestrator if parsing succeeds. If parsing fails (e.g. PPT
	// workflow's slide_scripting output is not a task list), the caller
	// falls through to the normal agent loop for execution.
	ActivateOrchestrator bool
	TaskBreakdownText    string // output from the phase preceding the execution phase
	RequirementsContext  string // truncated first-phase output (requirements/goals)
	DesignContext        string // truncated middle-phase outputs (design/specification)
}

// ReviewIntent is the classified user intent while a NeedsConfirm phase is
// waiting for user review. The engine accepts only this typed intent for review
// transitions; callers are responsible for classifying free-form text before
// invoking ApplyReviewIntent.
type ReviewIntent string

const (
	ReviewIntentConfirm    ReviewIntent = "confirm"
	ReviewIntentSupplement ReviewIntent = "supplement"
	ReviewIntentSkip       ReviewIntent = "skip"
	ReviewIntentCancel     ReviewIntent = "cancel"
	ReviewIntentSwitchTask ReviewIntent = "switch_task"
	ReviewIntentOther      ReviewIntent = "other"
)

// WorkflowStatus tracks the lifecycle state of a workflow.
type WorkflowStatus string

const (
	WorkflowActive    WorkflowStatus = "active"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowCancelled WorkflowStatus = "cancelled"
)

// WorkflowState holds the runtime state of an active workflow.
type WorkflowState struct {
	ID           string                        `json:"id"`
	UserID       string                        `json:"user_id"`
	Type         WorkflowType                  `json:"type"`
	Intent       StructuredIntent              `json:"intent"`
	CurrentPhase string                        `json:"current_phase"`
	PhaseIndex   int                           `json:"phase_index"`
	PhaseOutputs map[string]string             `json:"phase_outputs"`
	GateResults  map[string]*QualityGateResult `json:"gate_results"`
	Status       WorkflowStatus                `json:"status"`
	CreatedAt    time.Time                     `json:"created_at"`
	UpdatedAt    time.Time                     `json:"updated_at"`

	// PhaseFormData stores the user's structured form submission for the
	// current phase. Populated when the user submits a PhaseInputSchema form
	// via SubmitPhaseForm. Cleared when the phase advances.
	PhaseFormData map[string]interface{} `json:"phase_form_data,omitempty"`

	// PhaseFormSubmitted records that the current phase's form gate has been
	// satisfied even when the submitted form payload is empty because all fields
	// are optional. Without this bit, an empty but valid form submission is
	// indistinguishable from no submission after persistence/restore.
	PhaseFormSubmitted bool `json:"phase_form_submitted,omitempty"`

	// PhaseFormSkipped records that the user dismissed the structured form for
	// the current phase, allowing natural-language input without re-showing it.
	PhaseFormSkipped bool `json:"phase_form_skipped,omitempty"`

	// InputReceived is true when the user has provided the required input
	// document (for workflows with RequiresInput). The engine sets this
	// when it detects a document upload or substantial text input during
	// the waiting-for-input state.
	InputReceived bool `json:"input_received,omitempty"`

	// InputPayload records the evidence that satisfied RequiresInput. It is
	// injected into phase prompts so the input remains available after the
	// original chat turn and across process restarts.
	InputPayload *WorkflowInputPayload `json:"input_payload,omitempty"`

	// ProjectPath is the working directory associated with this workflow
	// (e.g., the project root for coding workflows). Used for artifact
	// tagging and context scoping.
	ProjectPath string `json:"project_path,omitempty"`

	// PendingReviewPhaseID is set after a NeedsConfirm phase output has been
	// saved and cleared only when the user explicitly confirms, modifies, or
	// cancels the review. This makes the confirmation/supplement step an engine
	// state instead of an agent-loop heuristic.
	PendingReviewPhaseID string `json:"pending_review_phase_id,omitempty"`

	// PendingReviewRevisionRequested is true only after a classified supplement
	// intent asks the agent loop to regenerate the pending review deliverable.
	// It prevents stray agent-loop output from overwriting a phase document while
	// the workflow is simply waiting for user review.
	PendingReviewRevisionRequested bool `json:"pending_review_revision_requested,omitempty"`
}

func cloneWorkflowMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(src))
	for k, v := range src {
		cp[k] = cloneWorkflowValue(v)
	}
	return cp
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	cp := make(map[string]string, len(src))
	for k, v := range src {
		cp[k] = v
	}
	return cp
}

func cloneWorkflowValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return cloneWorkflowMap(x)
	case []interface{}:
		cp := make([]interface{}, len(x))
		for i, item := range x {
			cp[i] = cloneWorkflowValue(item)
		}
		return cp
	case []string:
		return append([]string(nil), x...)
	case []float64:
		return append([]float64(nil), x...)
	case []int:
		return append([]int(nil), x...)
	case []bool:
		return append([]bool(nil), x...)
	default:
		return x
	}
}

func (ws *WorkflowState) Clone() *WorkflowState {
	if ws == nil {
		return nil
	}
	cp := *ws
	cp.Intent.Goals = append([]string(nil), ws.Intent.Goals...)
	cp.Intent.Constraints = append([]string(nil), ws.Intent.Constraints...)
	cp.Intent.OpenQuestions = append([]string(nil), ws.Intent.OpenQuestions...)
	if ws.PhaseOutputs != nil {
		cp.PhaseOutputs = make(map[string]string, len(ws.PhaseOutputs))
		for k, v := range ws.PhaseOutputs {
			cp.PhaseOutputs[k] = v
		}
	}
	if ws.GateResults != nil {
		cp.GateResults = make(map[string]*QualityGateResult, len(ws.GateResults))
		for k, v := range ws.GateResults {
			if v == nil {
				cp.GateResults[k] = nil
				continue
			}
			gate := *v
			gate.Items = append([]GateCheckItem(nil), v.Items...)
			cp.GateResults[k] = &gate
		}
	}
	cp.PhaseFormData = cloneWorkflowMap(ws.PhaseFormData)
	cp.InputPayload = ws.InputPayload.Clone()
	return &cp
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
	ID        string               `json:"id"`
	UserID    string               `json:"user_id"`
	Intent    StructuredIntent     `json:"intent"`
	Rounds    []UnderstandingRound `json:"rounds"`
	State     UnderstandingState   `json:"state"`
	CreatedAt time.Time            `json:"created_at"`
	UpdatedAt time.Time            `json:"updated_at"`
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
	// GetLang returns the current user-facing language ("zh", "en").
	// The engine calls this each time it needs to produce localized text,
	// ensuring the language is always read from the single source of truth
	// (app config) without requiring push-based synchronization.
	GetLang() string
}

// LLMCaller abstracts LLM invocation for testability.
type LLMCaller interface {
	DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error)
}

// ArtifactSaver abstracts saving workflow phase outputs to long-term memory.
// Implemented by memory.Store (via a thin adapter in gui/tui) to avoid
// corelib/workflow importing corelib/memory.
type ArtifactSaver interface {
	// SaveArtifact persists a workflow phase output summary to long-term memory.
	// title is a short human-readable label for the task list display.
	// tags should include phaseID and workflowType.
	SaveArtifact(title, content string, tags []string, sourceURL string) error
}

// FullArtifactSaver is implemented by artifact savers that can keep a compact
// memory preview while storing the full phase output as separately addressable evidence.
type FullArtifactSaver interface {
	ArtifactSaver
	SaveArtifactFull(title, summary, fullContent string, tags []string, sourceURL string) error
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
